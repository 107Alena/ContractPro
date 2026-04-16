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
// Implemented by observability.Metrics. DM-TASK-053: each method takes a stage
// label so the underlying Prometheus gauge/counter is partitioned by pipeline
// stage (processing/analysis/reports/finalization).
type WatchdogMetrics interface {
	IncStuckVersionsTotal(stage string, count int)
	SetStuckVersionsCount(stage string, count float64)
}

// Stage labels used for metrics and logs. Each value corresponds to one
// intermediate ArtifactStatus (DM-TASK-053).
const (
	stageProcessing   = "processing"
	stageAnalysis     = "analysis"
	stageReports      = "reports"
	stageFinalization = "finalization"
)

// allStages is the canonical list of stage labels. Used to reset per-stage
// gauges to zero before each scan so a resolved backlog does not leave a
// stale non-zero value on one of the labels.
var allStages = []string{stageProcessing, stageAnalysis, stageReports, stageFinalization}

// stageFromStatus maps an intermediate ArtifactStatus to its stage label.
// Returns ("unknown", false) for terminal or unexpected statuses.
func stageFromStatus(status model.ArtifactStatus) (string, bool) {
	switch status {
	case model.ArtifactStatusPending:
		return stageProcessing, true
	case model.ArtifactStatusProcessingArtifactsReceived:
		return stageAnalysis, true
	case model.ArtifactStatusAnalysisArtifactsReceived:
		return stageReports, true
	case model.ArtifactStatusReportsReady:
		return stageFinalization, true
	default:
		return "unknown", false
	}
}

// StaleVersionWatchdog is a background job that periodically scans for
// document versions stuck in intermediate artifact_status states beyond
// the configured per-stage timeout, transitions them to PARTIALLY_AVAILABLE,
// records an audit trail, and publishes a notification event via the
// transactional outbox (REV-008/BRE-010/DM-TASK-053).
type StaleVersionWatchdog struct {
	transactor   port.Transactor
	versionRepo  port.VersionRepository
	artifactRepo port.ArtifactRepository
	auditRepo    port.AuditRepository
	outboxWriter OutboxWriter
	metrics      WatchdogMetrics
	logger       *slog.Logger

	watchdogCfg  config.WatchdogConfig
	partialTopic string

	stop chan struct{}
	done chan struct{}
}

// NewStaleVersionWatchdog creates a watchdog with the given dependencies.
// Panics if any required dependency is nil (programmer error at startup).
//
// DM-TASK-053: per-stage timeouts are read from watchdogCfg (StaleTimeoutProcessing,
// StaleTimeoutAnalysis, StaleTimeoutReports, StaleTimeoutFinalization). The
// previous single staleTimeout parameter is removed — callers must pre-resolve
// any fallback behaviour into the WatchdogConfig fields.
func NewStaleVersionWatchdog(
	transactor port.Transactor,
	versionRepo port.VersionRepository,
	artifactRepo port.ArtifactRepository,
	auditRepo port.AuditRepository,
	outboxWriter OutboxWriter,
	metrics WatchdogMetrics,
	logger *slog.Logger,
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

// buildCutoffs returns the per-status cutoff timestamps based on now() and
// the per-stage timeouts from WatchdogConfig. Only statuses with a positive
// timeout are included — a zero timeout effectively disables the stage.
func (w *StaleVersionWatchdog) buildCutoffs(now time.Time) map[model.ArtifactStatus]time.Time {
	cutoffs := make(map[model.ArtifactStatus]time.Time, 4)
	addIfPositive := func(status model.ArtifactStatus, d time.Duration) {
		if d > 0 {
			cutoffs[status] = now.Add(-d)
		}
	}
	addIfPositive(model.ArtifactStatusPending, w.watchdogCfg.StaleTimeoutProcessing)
	addIfPositive(model.ArtifactStatusProcessingArtifactsReceived, w.watchdogCfg.StaleTimeoutAnalysis)
	addIfPositive(model.ArtifactStatusAnalysisArtifactsReceived, w.watchdogCfg.StaleTimeoutReports)
	addIfPositive(model.ArtifactStatusReportsReady, w.watchdogCfg.StaleTimeoutFinalization)
	return cutoffs
}

// timeoutForStatus returns the configured timeout for the given intermediate
// status (used for audit/event payload annotation).
func (w *StaleVersionWatchdog) timeoutForStatus(status model.ArtifactStatus) time.Duration {
	switch status {
	case model.ArtifactStatusPending:
		return w.watchdogCfg.StaleTimeoutProcessing
	case model.ArtifactStatusProcessingArtifactsReceived:
		return w.watchdogCfg.StaleTimeoutAnalysis
	case model.ArtifactStatusAnalysisArtifactsReceived:
		return w.watchdogCfg.StaleTimeoutReports
	case model.ArtifactStatusReportsReady:
		return w.watchdogCfg.StaleTimeoutFinalization
	default:
		return 0
	}
}

// scan performs one sweep: finds stale versions per-stage, transitions each one.
func (w *StaleVersionWatchdog) scan() {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	cutoffs := w.buildCutoffs(time.Now().UTC())

	staleVersions, err := w.versionRepo.FindStaleInIntermediateStatus(ctx, cutoffs, w.watchdogCfg.BatchSize)
	if err != nil {
		w.logger.Error("stale version watchdog: find stale versions failed", "error", err)
		return
	}

	// Group found versions by stage so the gauge reflects the per-stage
	// backlog in this scan. Stages with no stale versions get a zero value
	// so Prometheus clears a previous non-zero reading on the next scrape.
	perStageFound := make(map[string]int, len(allStages))
	for _, stage := range allStages {
		perStageFound[stage] = 0
	}
	for _, v := range staleVersions {
		stage, ok := stageFromStatus(v.ArtifactStatus)
		if !ok {
			continue
		}
		perStageFound[stage]++
	}
	for stage, count := range perStageFound {
		w.metrics.SetStuckVersionsCount(stage, float64(count))
	}

	if len(staleVersions) == 0 {
		return
	}

	perStageSuccess := make(map[string]int, len(allStages))
	var successCount, failCount int

	for _, v := range staleVersions {
		if err := ctx.Err(); err != nil {
			w.logger.Warn("stale version watchdog: scan context cancelled", "error", err)
			break
		}

		stage, _ := stageFromStatus(v.ArtifactStatus)

		if err := w.transitionVersion(ctx, v); err != nil {
			w.logger.Warn("stale version watchdog: failed to transition version",
				"version_id", v.VersionID,
				"document_id", v.DocumentID,
				"artifact_status", v.ArtifactStatus,
				"stage", stage,
				"error", err,
			)
			failCount++
		} else {
			w.logger.Info("stale version watchdog: transitioned to PARTIALLY_AVAILABLE",
				"version_id", v.VersionID,
				"document_id", v.DocumentID,
				"artifact_status", v.ArtifactStatus,
				"stage", stage,
			)
			perStageSuccess[stage]++
			successCount++
		}
	}

	for stage, count := range perStageSuccess {
		if count > 0 {
			w.metrics.IncStuckVersionsTotal(stage, count)
		}
	}

	w.logger.Info("stale version watchdog: scan completed",
		"found", len(staleVersions),
		"transitioned", successCount,
		"failed", failCount,
		"per_stage_found", perStageFound,
		"per_stage_success", perStageSuccess,
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
		stageTimeout := w.timeoutForStatus(oldStatus)

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
		stage, _ := stageFromStatus(oldStatus)
		details, _ := json.Marshal(map[string]any{
			"from":    string(oldStatus),
			"to":      string(model.ArtifactStatusPartiallyAvailable),
			"reason":  "stale_version_timeout",
			"stage":   stage,
			"timeout": stageTimeout.String(),
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
			ErrorMessage:   "version timed out in " + string(oldStatus) + " after " + stageTimeout.String(),
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
// that failed to complete. The label set here is the human-readable pipeline
// name published in the outbox event (different from the short metric label
// returned by stageFromStatus).
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
