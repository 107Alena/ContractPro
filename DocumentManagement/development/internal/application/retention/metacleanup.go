package retention

import (
	"context"
	"log/slog"
	"time"

	"contractpro/document-management/internal/config"
	"contractpro/document-management/internal/domain/model"
)

// MetaCleanupMetrics is the consumer-side interface for metadata cleanup metrics.
// Implemented by observability.Metrics.
type MetaCleanupMetrics interface {
	IncRetentionMetaDeletedTotal(count int)
	SetRetentionMetaScanDocsCount(count float64)
}

// MetaCleanupTransactor executes a function within a database transaction.
type MetaCleanupTransactor interface {
	WithTransaction(ctx context.Context, fn func(ctx context.Context) error) error
}

// MetaCleanupDocumentRepo provides the document operations needed for
// metadata cleanup.
type MetaCleanupDocumentRepo interface {
	FindDeletedOlderThan(ctx context.Context, cutoff time.Time, limit int) ([]*model.Document, error)
	DeleteByID(ctx context.Context, documentID string) error
	// Update clears current_version_id before version deletion (FK constraint).
	Update(ctx context.Context, doc *model.Document) error
}

// MetaCleanupVersionRepo provides version operations for metadata cleanup.
type MetaCleanupVersionRepo interface {
	ListByDocument(ctx context.Context, documentID string) ([]*model.DocumentVersion, error)
	DeleteByDocument(ctx context.Context, documentID string) error
}

// MetaCleanupArtifactRepo provides artifact operations for metadata cleanup.
type MetaCleanupArtifactRepo interface {
	DeleteByVersion(ctx context.Context, organizationID, documentID, versionID string) error
}

// MetaCleanupDiffRepo provides diff operations for metadata cleanup.
type MetaCleanupDiffRepo interface {
	DeleteByDocument(ctx context.Context, organizationID, documentID string) error
}

// MetaCleanupAuditRepo provides audit operations for metadata cleanup.
type MetaCleanupAuditRepo interface {
	DeleteByDocument(ctx context.Context, documentID string) error
}

// DeletedMetaCleanupJob is a background job that hard-deletes metadata
// (DB rows) for documents with status=DELETED whose deleted_at is older
// than DM_RETENTION_DELETED_META_DAYS.
//
// Processing flow per document (within a single transaction):
//  1. Clear current_version_id on document (to break circular FK)
//  2. For each version: delete artifact_descriptors
//  3. Delete version_diff_references for the document
//  4. Delete audit_records for the document
//  5. Delete document_versions for the document
//  6. Delete the document row itself
//
// FK deletion order is critical: artifacts → diffs → audit → versions → document.
type DeletedMetaCleanupJob struct {
	transactor   MetaCleanupTransactor
	docRepo      MetaCleanupDocumentRepo
	versionRepo  MetaCleanupVersionRepo
	artifactRepo MetaCleanupArtifactRepo
	diffRepo     MetaCleanupDiffRepo
	auditRepo    MetaCleanupAuditRepo
	metrics      MetaCleanupMetrics
	logger       *slog.Logger
	cfg          config.RetentionConfig

	stop chan struct{}
	done chan struct{}
}

// NewDeletedMetaCleanupJob creates a new metadata cleanup job.
// Panics if any required dependency is nil (programmer error at startup).
func NewDeletedMetaCleanupJob(
	transactor MetaCleanupTransactor,
	docRepo MetaCleanupDocumentRepo,
	versionRepo MetaCleanupVersionRepo,
	artifactRepo MetaCleanupArtifactRepo,
	diffRepo MetaCleanupDiffRepo,
	auditRepo MetaCleanupAuditRepo,
	metrics MetaCleanupMetrics,
	logger *slog.Logger,
	cfg config.RetentionConfig,
) *DeletedMetaCleanupJob {
	if transactor == nil {
		panic("retention: transactor must not be nil")
	}
	if docRepo == nil {
		panic("retention: document repository must not be nil")
	}
	if versionRepo == nil {
		panic("retention: version repository must not be nil")
	}
	if artifactRepo == nil {
		panic("retention: artifact repository must not be nil")
	}
	if diffRepo == nil {
		panic("retention: diff repository must not be nil")
	}
	if auditRepo == nil {
		panic("retention: audit repository must not be nil")
	}
	if metrics == nil {
		panic("retention: metrics must not be nil")
	}
	if logger == nil {
		panic("retention: logger must not be nil")
	}

	return &DeletedMetaCleanupJob{
		transactor:   transactor,
		docRepo:      docRepo,
		versionRepo:  versionRepo,
		artifactRepo: artifactRepo,
		diffRepo:     diffRepo,
		auditRepo:    auditRepo,
		metrics:      metrics,
		logger:       logger,
		cfg:          cfg,
		stop:         make(chan struct{}),
		done:         make(chan struct{}),
	}
}

// Start launches the scanning loop in a background goroutine.
func (j *DeletedMetaCleanupJob) Start() {
	go j.run()
}

// Stop signals the scanning loop to stop. Safe to call multiple times.
func (j *DeletedMetaCleanupJob) Stop() {
	select {
	case <-j.stop:
	default:
		close(j.stop)
	}
}

// Done returns a channel that is closed when the job goroutine has exited.
func (j *DeletedMetaCleanupJob) Done() <-chan struct{} {
	return j.done
}

func (j *DeletedMetaCleanupJob) run() {
	defer close(j.done)

	ticker := time.NewTicker(j.cfg.MetaScanInterval)
	defer ticker.Stop()

	for {
		select {
		case <-j.stop:
			j.logger.Info("retention meta cleanup: shutting down")
			return
		case <-ticker.C:
			j.scan()
		}
	}
}

func (j *DeletedMetaCleanupJob) scan() {
	ctx, cancel := context.WithTimeout(context.Background(), j.cfg.ScanTimeout)
	defer cancel()

	cutoff := time.Now().UTC().AddDate(0, 0, -j.cfg.DeletedMetaDays)

	docs, err := j.docRepo.FindDeletedOlderThan(ctx, cutoff, j.cfg.BatchSize)
	if err != nil {
		j.logger.Error("retention meta cleanup: find deleted docs failed", "error", err)
		return
	}

	j.metrics.SetRetentionMetaScanDocsCount(float64(len(docs)))

	if len(docs) == 0 {
		return
	}

	var deletedCount, failedCount int

	for _, doc := range docs {
		if ctx.Err() != nil {
			j.logger.Warn("retention meta cleanup: scan context cancelled", "error", ctx.Err())
			break
		}

		if err := j.deleteDocument(ctx, doc); err != nil {
			j.logger.Warn("retention meta cleanup: delete document failed",
				"document_id", doc.DocumentID,
				"organization_id", doc.OrganizationID,
				"error", err,
			)
			failedCount++
			continue
		}
		deletedCount++
	}

	if deletedCount > 0 {
		j.metrics.IncRetentionMetaDeletedTotal(deletedCount)
	}

	j.logger.Info("retention meta cleanup: scan completed",
		"found", len(docs),
		"deleted", deletedCount,
		"failed", failedCount,
	)
}

// deleteDocument removes all metadata for a single document within a transaction.
// FK deletion order: clear current_version_id → artifacts → diffs → audit → versions → document.
func (j *DeletedMetaCleanupJob) deleteDocument(ctx context.Context, doc *model.Document) error {
	return j.transactor.WithTransaction(ctx, func(txCtx context.Context) error {
		// 1. Clear current_version_id to break circular FK.
		doc.CurrentVersionID = ""
		if err := j.docRepo.Update(txCtx, doc); err != nil {
			return err
		}

		// 2. List versions to delete artifacts per version.
		versions, err := j.versionRepo.ListByDocument(txCtx, doc.DocumentID)
		if err != nil {
			return err
		}

		// 3. Delete artifacts for each version.
		for _, v := range versions {
			if err := j.artifactRepo.DeleteByVersion(txCtx, v.OrganizationID, v.DocumentID, v.VersionID); err != nil {
				return err
			}
		}

		// 4. Delete diffs for the document.
		if err := j.diffRepo.DeleteByDocument(txCtx, doc.OrganizationID, doc.DocumentID); err != nil {
			return err
		}

		// 5. Delete audit records for the document.
		if err := j.auditRepo.DeleteByDocument(txCtx, doc.DocumentID); err != nil {
			return err
		}

		// 6. Delete versions for the document.
		if err := j.versionRepo.DeleteByDocument(txCtx, doc.DocumentID); err != nil {
			return err
		}

		// 7. Delete the document row itself.
		return j.docRepo.DeleteByID(txCtx, doc.DocumentID)
	})
}
