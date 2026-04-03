package watchdog

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"contractpro/document-management/internal/config"
	"contractpro/document-management/internal/domain/model"
	"contractpro/document-management/internal/domain/port"
)

// OutboxWriter is a consumer-side interface for writing events to the
// transactional outbox. Implemented by outbox.OutboxWriter.
type OutboxWriter interface {
	Write(ctx context.Context, aggregateID, topic string, event any) error
}

// WatchdogMetrics is the consumer-side interface for watchdog-specific metrics.
// Implemented by observability.Metrics.
type WatchdogMetrics interface {
	IncStuckVersionsTotal(count int)
	SetStuckVersionsCount(count float64)
}

// StaleVersionWatchdog is a background job that periodically scans for
// document versions stuck in intermediate artifact_status states beyond
// the configured timeout, transitions them to PARTIALLY_AVAILABLE,
// records an audit trail, and publishes a notification event via the
// transactional outbox (REV-008/BRE-010).
type StaleVersionWatchdog struct {
	transactor   port.Transactor
	versionRepo  port.VersionRepository
	artifactRepo port.ArtifactRepository
	auditRepo    port.AuditRepository
	outboxWriter OutboxWriter
	metrics      WatchdogMetrics
	logger       *slog.Logger

	staleTimeout time.Duration
	watchdogCfg  config.WatchdogConfig
	partialTopic string

	stop chan struct{}
	done chan struct{}
}

// NewStaleVersionWatchdog creates a watchdog with the given dependencies.
// Panics if any required dependency is nil (programmer error at startup).
func NewStaleVersionWatchdog(
	transactor port.Transactor,
	versionRepo port.VersionRepository,
	artifactRepo port.ArtifactRepository,
	auditRepo port.AuditRepository,
	outboxWriter OutboxWriter,
	metrics WatchdogMetrics,
	logger *slog.Logger,
	staleTimeout time.Duration,
	watchdogCfg config.WatchdogConfig,
	partialTopic string,
) *StaleVersionWatchdog {
	if transactor == nil {
		panic("watchdog: transactor must not be nil")
	}
	if versionRepo == nil {
		panic("watchdog: version repository must not be nil")
	}
	if artifactRepo == nil {
		panic("watchdog: artifact repository must not be nil")
	}
	if auditRepo == nil {
		panic("watchdog: audit repository must not be nil")
	}
	if outboxWriter == nil {
		panic("watchdog: outbox writer must not be nil")
	}
	if metrics == nil {
		panic("watchdog: metrics must not be nil")
	}
	if logger == nil {
		panic("watchdog: logger must not be nil")
	}

	return &StaleVersionWatchdog{
		transactor:   transactor,
		versionRepo:  versionRepo,
		artifactRepo: artifactRepo,
		auditRepo:    auditRepo,
		outboxWriter: outboxWriter,
		metrics:      metrics,
		logger:       logger,
		staleTimeout: staleTimeout,
		watchdogCfg:  watchdogCfg,
		partialTopic: partialTopic,
		stop:         make(chan struct{}),
		done:         make(chan struct{}),
	}
}

// Start launches the scanning loop in a background goroutine.
func (w *StaleVersionWatchdog) Start() {
	go w.run()
}

// Stop signals the scanning loop to stop. Safe to call multiple times.
func (w *StaleVersionWatchdog) Stop() {
	select {
	case <-w.stop:
	default:
		close(w.stop)
	}
}

// Done returns a channel that is closed when the watchdog goroutine has exited.
func (w *StaleVersionWatchdog) Done() <-chan struct{} {
	return w.done
}

// run is the main loop (mirrors OutboxPoller.run pattern).
func (w *StaleVersionWatchdog) run() {
	defer close(w.done)

	ticker := time.NewTicker(w.watchdogCfg.ScanInterval)
	defer ticker.Stop()

	for {
		select {
		case <-w.stop:
			w.logger.Info("stale version watchdog: shutting down")
			return
		case <-ticker.C:
			w.scan()
		}
	}
}

// scan performs one sweep: finds stale versions, transitions each one.
func (w *StaleVersionWatchdog) scan() {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	cutoff := time.Now().UTC().Add(-w.staleTimeout)

	staleVersions, err := w.versionRepo.FindStaleInIntermediateStatus(ctx, cutoff, w.watchdogCfg.BatchSize)
	if err != nil {
		w.logger.Error("stale version watchdog: find stale versions failed", "error", err)
		return
	}

	// Update gauge with the number of stale versions found before transitioning.
	w.metrics.SetStuckVersionsCount(float64(len(staleVersions)))

	if len(staleVersions) == 0 {
		return
	}

	var successCount, failCount int

	for _, v := range staleVersions {
		if err := ctx.Err(); err != nil {
			w.logger.Warn("stale version watchdog: scan context cancelled", "error", err)
			break
		}

		if err := w.transitionVersion(ctx, v); err != nil {
			w.logger.Warn("stale version watchdog: failed to transition version",
				"version_id", v.VersionID,
				"document_id", v.DocumentID,
				"artifact_status", v.ArtifactStatus,
				"error", err,
			)
			failCount++
		} else {
			successCount++
		}
	}

	if successCount > 0 {
		w.metrics.IncStuckVersionsTotal(successCount)
	}

	w.logger.Info("stale version watchdog: scan completed",
		"found", len(staleVersions),
		"transitioned", successCount,
		"failed", failCount,
	)
}

// transitionVersion handles a single stale version within a transaction:
//  1. Lock version FOR UPDATE (re-check status in case another replica handled it)
//  2. Transition artifact_status → PARTIALLY_AVAILABLE
//  3. Update version row
//  4. Fetch available artifact types for the notification event
//  5. Insert audit record (actor_type=SYSTEM, actor_id="stale-version-watchdog")
//  6. Write outbox event (VersionPartiallyAvailable)
func (w *StaleVersionWatchdog) transitionVersion(ctx context.Context, version *model.DocumentVersion) error {
	return w.transactor.WithTransaction(ctx, func(txCtx context.Context) error {
		// Re-read with exclusive lock to get fresh status.
		fresh, err := w.versionRepo.FindByIDForUpdate(
			txCtx, version.OrganizationID, version.DocumentID, version.VersionID,
		)
		if err != nil {
			return err
		}

		// Re-validate: status might have changed since the unlocked read.
		if fresh.ArtifactStatus.IsTerminal() {
			return nil
		}

		oldStatus := fresh.ArtifactStatus
		if err := fresh.TransitionArtifactStatus(model.ArtifactStatusPartiallyAvailable); err != nil {
			return err
		}

		if err := w.versionRepo.Update(txCtx, fresh); err != nil {
			return err
		}

		// Fetch available artifacts for the notification event.
		artifacts, err := w.artifactRepo.ListByVersion(
			txCtx, fresh.OrganizationID, fresh.DocumentID, fresh.VersionID,
		)
		if err != nil {
			return err
		}
		availableTypes := extractArtifactTypes(artifacts)

		// Audit record.
		details, _ := json.Marshal(map[string]any{
			"from":    string(oldStatus),
			"to":      string(model.ArtifactStatusPartiallyAvailable),
			"reason":  "stale_version_timeout",
			"timeout": w.staleTimeout.String(),
		})
		auditRecord := model.NewAuditRecord(
			generateUUID(), fresh.OrganizationID,
			model.AuditActionArtifactStatusChanged,
			model.ActorTypeSystem, "stale-version-watchdog",
		).WithDocument(fresh.DocumentID).
			WithVersion(fresh.VersionID).
			WithDetails(details)

		if err := w.auditRepo.Insert(txCtx, auditRecord); err != nil {
			return err
		}

		// Outbox event.
		event := model.VersionPartiallyAvailable{
			EventMeta: model.EventMeta{
				CorrelationID: generateUUID(),
				Timestamp:     time.Now().UTC(),
			},
			DocumentID:     fresh.DocumentID,
			VersionID:      fresh.VersionID,
			OrgID:          fresh.OrganizationID,
			ArtifactStatus: oldStatus,
			AvailableTypes: availableTypes,
			FailedStage:    failedStageFromStatus(oldStatus),
			ErrorMessage:   "version timed out in " + string(oldStatus) + " after " + w.staleTimeout.String(),
		}

		return w.outboxWriter.Write(txCtx, fresh.VersionID, w.partialTopic, event)
	})
}

// extractArtifactTypes returns the list of artifact types from descriptors.
func extractArtifactTypes(artifacts []*model.ArtifactDescriptor) []model.ArtifactType {
	types := make([]model.ArtifactType, 0, len(artifacts))
	for _, a := range artifacts {
		types = append(types, a.ArtifactType)
	}
	return types
}

// failedStageFromStatus maps the intermediate status to the pipeline stage
// that failed to complete.
func failedStageFromStatus(status model.ArtifactStatus) string {
	switch status {
	case model.ArtifactStatusPending:
		return "document_processing"
	case model.ArtifactStatusProcessingArtifactsReceived:
		return "legal_analysis"
	case model.ArtifactStatusAnalysisArtifactsReceived:
		return "report_generation"
	case model.ArtifactStatusReportsReady:
		return "finalization"
	default:
		return "unknown"
	}
}

// generateUUID generates a UUID v4 string using crypto/rand.
func generateUUID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("watchdog: crypto/rand failed: " + err.Error())
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
