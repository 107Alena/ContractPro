package retention

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"contractpro/document-management/internal/config"
	"contractpro/document-management/internal/domain/model"
)

// BlobCleanupMetrics is the consumer-side interface for blob cleanup metrics.
// Implemented by observability.Metrics.
type BlobCleanupMetrics interface {
	IncRetentionBlobDeletedTotal(count int)
	SetRetentionBlobScanDocsCount(count float64)
}

// DeletedDocumentFinder retrieves soft-deleted documents for cleanup.
// Implemented by DocumentRepository (via pool-injecting wrapper).
type DeletedDocumentFinder interface {
	FindDeletedOlderThan(ctx context.Context, cutoff time.Time, limit int) ([]*model.Document, error)
}

// BlobDeleter deletes S3 blobs by prefix (all objects under a document).
// Implemented by ObjectStoragePort (or circuit breaker wrapper).
type BlobDeleter interface {
	DeleteByPrefix(ctx context.Context, prefix string) error
}

// DeletedBlobCleanupJob is a background job that deletes S3 blobs for
// documents with status=DELETED whose deleted_at is older than
// DM_RETENTION_DELETED_BLOB_DAYS.
//
// Processing flow per scan:
//  1. Find DELETED documents older than retention threshold
//  2. For each document: DeleteByPrefix({org_id}/{doc_id}/)
//  3. Update metrics and log summary
//
// No DB mutations — blob deletion only. Metadata cleanup is handled
// separately by DeletedMetaCleanupJob.
type DeletedBlobCleanupJob struct {
	docFinder DeletedDocumentFinder
	storage   BlobDeleter
	metrics   BlobCleanupMetrics
	logger    *slog.Logger
	cfg       config.RetentionConfig

	stop chan struct{}
	done chan struct{}
}

// NewDeletedBlobCleanupJob creates a new blob cleanup job.
// Panics if any required dependency is nil (programmer error at startup).
func NewDeletedBlobCleanupJob(
	docFinder DeletedDocumentFinder,
	storage BlobDeleter,
	metrics BlobCleanupMetrics,
	logger *slog.Logger,
	cfg config.RetentionConfig,
) *DeletedBlobCleanupJob {
	if docFinder == nil {
		panic("retention: document finder must not be nil")
	}
	if storage == nil {
		panic("retention: object storage must not be nil")
	}
	if metrics == nil {
		panic("retention: metrics must not be nil")
	}
	if logger == nil {
		panic("retention: logger must not be nil")
	}

	return &DeletedBlobCleanupJob{
		docFinder: docFinder,
		storage:   storage,
		metrics:   metrics,
		logger:    logger,
		cfg:       cfg,
		stop:      make(chan struct{}),
		done:      make(chan struct{}),
	}
}

// Start launches the scanning loop in a background goroutine.
func (j *DeletedBlobCleanupJob) Start() {
	go j.run()
}

// Stop signals the scanning loop to stop. Safe to call multiple times.
func (j *DeletedBlobCleanupJob) Stop() {
	select {
	case <-j.stop:
	default:
		close(j.stop)
	}
}

// Done returns a channel that is closed when the job goroutine has exited.
func (j *DeletedBlobCleanupJob) Done() <-chan struct{} {
	return j.done
}

func (j *DeletedBlobCleanupJob) run() {
	defer close(j.done)

	ticker := time.NewTicker(j.cfg.BlobScanInterval)
	defer ticker.Stop()

	for {
		select {
		case <-j.stop:
			j.logger.Info("retention blob cleanup: shutting down")
			return
		case <-ticker.C:
			j.scan()
		}
	}
}

func (j *DeletedBlobCleanupJob) scan() {
	ctx, cancel := context.WithTimeout(context.Background(), j.cfg.ScanTimeout)
	defer cancel()

	cutoff := time.Now().UTC().AddDate(0, 0, -j.cfg.DeletedBlobDays)

	docs, err := j.docFinder.FindDeletedOlderThan(ctx, cutoff, j.cfg.BatchSize)
	if err != nil {
		j.logger.Error("retention blob cleanup: find deleted docs failed", "error", err)
		return
	}

	j.metrics.SetRetentionBlobScanDocsCount(float64(len(docs)))

	if len(docs) == 0 {
		return
	}

	var deletedCount, skippedCount int

	for _, doc := range docs {
		if ctx.Err() != nil {
			j.logger.Warn("retention blob cleanup: scan context cancelled", "error", ctx.Err())
			break
		}

		prefix := fmt.Sprintf("%s/%s/", doc.OrganizationID, doc.DocumentID)
		if err := j.storage.DeleteByPrefix(ctx, prefix); err != nil {
			j.logger.Warn("retention blob cleanup: delete prefix failed",
				"document_id", doc.DocumentID,
				"organization_id", doc.OrganizationID,
				"prefix", prefix,
				"error", err,
			)
			skippedCount++
			continue
		}
		deletedCount++
	}

	if deletedCount > 0 {
		j.metrics.IncRetentionBlobDeletedTotal(deletedCount)
	}

	j.logger.Info("retention blob cleanup: scan completed",
		"found", len(docs),
		"deleted", deletedCount,
		"skipped", skippedCount,
	)
}
