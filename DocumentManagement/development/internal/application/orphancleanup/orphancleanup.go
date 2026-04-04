package orphancleanup

import (
	"context"
	"log/slog"
	"time"

	"contractpro/document-management/internal/config"
	"contractpro/document-management/internal/domain/port"
)

// OrphanCleanupMetrics is the consumer-side interface for orphan cleanup
// metrics. Implemented by observability.Metrics.
type OrphanCleanupMetrics interface {
	IncOrphansDeletedTotal(count int)
	SetOrphanCandidatesCount(count float64)
}

// ObjectStorageDeleter is the consumer-side interface for deleting orphan
// blobs from S3. Implemented by objectstorage.Client (or circuit breaker
// wrapper).
type ObjectStorageDeleter interface {
	DeleteObject(ctx context.Context, key string) error
}

// OrphanCleanupJob is a background job that periodically scans the
// orphan_candidates table for suspected orphan blobs, verifies each one
// against artifact_descriptors, and deletes confirmed orphans from S3
// (BRE-008).
//
// Processing flow per scan:
//  1. SELECT orphan candidates older than GracePeriod
//  2. For each candidate: check if storage_key exists in artifact_descriptors
//  3. If exists (false positive): remove from orphan_candidates
//  4. If not exists (confirmed orphan): DeleteObject from S3, remove from orphan_candidates
//  5. Update metrics and log summary
type OrphanCleanupJob struct {
	candidateRepo port.OrphanCandidateRepository
	storage       ObjectStorageDeleter
	metrics       OrphanCleanupMetrics
	logger        *slog.Logger
	cfg           config.OrphanCleanupConfig

	stop chan struct{}
	done chan struct{}
}

// NewOrphanCleanupJob creates a new orphan cleanup job with the given
// dependencies. Panics if any required dependency is nil (programmer error
// at startup).
func NewOrphanCleanupJob(
	candidateRepo port.OrphanCandidateRepository,
	storage ObjectStorageDeleter,
	metrics OrphanCleanupMetrics,
	logger *slog.Logger,
	cfg config.OrphanCleanupConfig,
) *OrphanCleanupJob {
	if candidateRepo == nil {
		panic("orphancleanup: candidate repository must not be nil")
	}
	if storage == nil {
		panic("orphancleanup: object storage must not be nil")
	}
	if metrics == nil {
		panic("orphancleanup: metrics must not be nil")
	}
	if logger == nil {
		panic("orphancleanup: logger must not be nil")
	}

	return &OrphanCleanupJob{
		candidateRepo: candidateRepo,
		storage:       storage,
		metrics:       metrics,
		logger:        logger,
		cfg:           cfg,
		stop:          make(chan struct{}),
		done:          make(chan struct{}),
	}
}

// Start launches the scanning loop in a background goroutine.
func (j *OrphanCleanupJob) Start() {
	go j.run()
}

// Stop signals the scanning loop to stop. Safe to call multiple times.
func (j *OrphanCleanupJob) Stop() {
	select {
	case <-j.stop:
	default:
		close(j.stop)
	}
}

// Done returns a channel that is closed when the job goroutine has exited.
func (j *OrphanCleanupJob) Done() <-chan struct{} {
	return j.done
}

// run is the main loop.
func (j *OrphanCleanupJob) run() {
	defer close(j.done)

	ticker := time.NewTicker(j.cfg.ScanInterval)
	defer ticker.Stop()

	for {
		select {
		case <-j.stop:
			j.logger.Info("orphan cleanup: shutting down")
			return
		case <-ticker.C:
			j.scan()
		}
	}
}

// scan performs one sweep: finds orphan candidates, verifies each, deletes
// confirmed orphans.
func (j *OrphanCleanupJob) scan() {
	ctx, cancel := context.WithTimeout(context.Background(), j.cfg.ScanTimeout)
	defer cancel()

	cutoff := time.Now().UTC().Add(-j.cfg.GracePeriod)

	candidates, err := j.candidateRepo.FindOlderThan(ctx, cutoff, j.cfg.BatchSize)
	if err != nil {
		j.logger.Error("orphan cleanup: find candidates failed", "error", err)
		return
	}

	j.metrics.SetOrphanCandidatesCount(float64(len(candidates)))

	if len(candidates) == 0 {
		return
	}

	var (
		deletedKeys       []string
		orphanDeletedCount int
		skippedCount       int
	)

	// Process each candidate independently. The GracePeriod (default 1h)
	// prevents a TOCTOU race: between ExistsByStorageKey (no row) and
	// DeleteObject, a concurrent ingestion could create an ArtifactDescriptor
	// pointing to this key. The grace period ensures we only process
	// candidates old enough that no in-flight transaction could still commit.
	// Multi-replica safety: S3 DeleteObject and DeleteByKeys are both
	// idempotent, so concurrent scans on different replicas are safe.
	for _, c := range candidates {
		if ctx.Err() != nil {
			j.logger.Warn("orphan cleanup: scan context cancelled", "error", ctx.Err())
			break
		}

		exists, err := j.candidateRepo.ExistsByStorageKey(ctx, c.StorageKey)
		if err != nil {
			j.logger.Warn("orphan cleanup: check storage key failed",
				"storage_key", c.StorageKey,
				"error", err,
			)
			skippedCount++
			continue
		}

		if exists {
			// False positive — ArtifactDescriptor exists, blob is not orphaned.
			deletedKeys = append(deletedKeys, c.StorageKey)
			continue
		}

		// Confirmed orphan — delete blob from S3.
		if err := j.storage.DeleteObject(ctx, c.StorageKey); err != nil {
			j.logger.Warn("orphan cleanup: delete blob failed",
				"storage_key", c.StorageKey,
				"error", err,
			)
			skippedCount++
			continue
		}

		deletedKeys = append(deletedKeys, c.StorageKey)
		orphanDeletedCount++
	}

	// Batch-remove processed candidates from the table.
	if len(deletedKeys) > 0 {
		if err := j.candidateRepo.DeleteByKeys(ctx, deletedKeys); err != nil {
			j.logger.Warn("orphan cleanup: delete candidate rows failed",
				"count", len(deletedKeys),
				"error", err,
			)
		}
	}

	if orphanDeletedCount > 0 {
		j.metrics.IncOrphansDeletedTotal(orphanDeletedCount)
	}

	falsePositives := len(deletedKeys) - orphanDeletedCount

	j.logger.Info("orphan cleanup: scan completed",
		"found", len(candidates),
		"deleted", orphanDeletedCount,
		"false_positives", falsePositives,
		"skipped", skippedCount,
	)
}
