package ingestion

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"contractpro/document-management/internal/application/tenant"
	"contractpro/document-management/internal/domain/model"
	"contractpro/document-management/internal/domain/port"
	"contractpro/document-management/internal/egress/outbox"
	"contractpro/document-management/internal/infra/objectstorage"
)

// defaultSchemaVersion is the schema version recorded for ingested artifacts.
// Events do not currently carry schema information; this constant serves as
// the initial version marker. Future event versions may override it.
const defaultSchemaVersion = "1.0"

// Logger is the minimal structured logging interface.
type Logger interface {
	Info(msg string, keysAndValues ...any)
	Warn(msg string, keysAndValues ...any)
	Error(msg string, keysAndValues ...any)
}

// FallbackMetrics provides metrics for defensive fallback operations (REV-001/REV-002).
type FallbackMetrics interface {
	IncMissingVersionID()
}

// ArtifactIngestionService receives artifact payloads from producer domains
// (DP, LIC, RE) and persists them into object storage and the metadata store.
// It transitions the version's artifact_status through the state machine and
// publishes confirmation + notification events via the transactional outbox.
type ArtifactIngestionService struct {
	transactor       port.Transactor
	versionRepo      port.VersionRepository
	artifactRepo     port.ArtifactRepository
	auditRepo        port.AuditRepository
	objectStorage    port.ObjectStoragePort
	outboxWriter     *outbox.OutboxWriter
	fallbackResolver port.DocumentFallbackResolver
	fallbackMetrics  FallbackMetrics
	docRepo          tenant.DocumentExistenceChecker
	tenantMetrics    tenant.Metrics
	logger           Logger
	newUUID          func() string
}

// Compile-time interface check.
var _ port.ArtifactIngestionHandler = (*ArtifactIngestionService)(nil)

// NewArtifactIngestionService creates a new service with the given dependencies.
// Panics if any required dependency is nil (programmer error at startup wiring).
func NewArtifactIngestionService(
	transactor port.Transactor,
	versionRepo port.VersionRepository,
	artifactRepo port.ArtifactRepository,
	auditRepo port.AuditRepository,
	objectStorage port.ObjectStoragePort,
	outboxWriter *outbox.OutboxWriter,
	fallbackResolver port.DocumentFallbackResolver,
	fallbackMetrics FallbackMetrics,
	docRepo tenant.DocumentExistenceChecker,
	tenantMetrics tenant.Metrics,
	logger Logger,
) *ArtifactIngestionService {
	if transactor == nil {
		panic("ingestion: transactor must not be nil")
	}
	if versionRepo == nil {
		panic("ingestion: versionRepo must not be nil")
	}
	if artifactRepo == nil {
		panic("ingestion: artifactRepo must not be nil")
	}
	if auditRepo == nil {
		panic("ingestion: auditRepo must not be nil")
	}
	if objectStorage == nil {
		panic("ingestion: objectStorage must not be nil")
	}
	if outboxWriter == nil {
		panic("ingestion: outboxWriter must not be nil")
	}
	if fallbackResolver == nil {
		panic("ingestion: fallbackResolver must not be nil")
	}
	if fallbackMetrics == nil {
		panic("ingestion: fallbackMetrics must not be nil")
	}
	if docRepo == nil {
		panic("ingestion: docRepo must not be nil")
	}
	if tenantMetrics == nil {
		panic("ingestion: tenantMetrics must not be nil")
	}
	if logger == nil {
		panic("ingestion: logger must not be nil")
	}
	return &ArtifactIngestionService{
		transactor:       transactor,
		versionRepo:      versionRepo,
		artifactRepo:     artifactRepo,
		auditRepo:        auditRepo,
		objectStorage:    objectStorage,
		outboxWriter:     outboxWriter,
		fallbackResolver: fallbackResolver,
		fallbackMetrics:  fallbackMetrics,
		docRepo:          docRepo,
		tenantMetrics:    tenantMetrics,
		logger:           logger,
		newUUID:          generateUUID,
	}
}

// ---------------------------------------------------------------------------
// Public handlers — one per producer domain.
// ---------------------------------------------------------------------------

// HandleDPArtifacts processes a DP artifacts-ready event: validates the
// version, stores each artifact blob in object storage, creates artifact
// descriptors in the metadata store, transitions artifact_status to
// PROCESSING_ARTIFACTS_RECEIVED, and publishes confirmation/notification
// events via the transactional outbox.
//
// Defensive fallback (REV-001/REV-002): if DP omits version_id or
// organization_id, the service resolves them from the documents table.
func (s *ArtifactIngestionService) HandleDPArtifacts(ctx context.Context, event model.DocumentProcessingArtifactsReady) error {
	// REV-001/REV-002: resolve missing fields with a single DB lookup when possible.
	if event.OrgID == "" || event.VersionID == "" {
		if err := s.resolveDPEventFields(ctx, event.DocumentID, &event.OrgID, &event.VersionID); err != nil {
			return err
		}
	}

	// BRE-015: verify document belongs to claimed organization.
	if err := tenant.VerifyTenantOwnership(ctx, s.docRepo, s.tenantMetrics, s.logger, event.OrgID, event.DocumentID); err != nil {
		return err
	}

	if err := validateRequired(event.OrgID, event.JobID, event.DocumentID, event.VersionID); err != nil {
		return err
	}

	artifacts := extractDPArtifacts(event)
	if len(artifacts) == 0 {
		return port.NewValidationError("no artifacts in DP event")
	}

	meta := model.EventMeta{CorrelationID: event.CorrelationID, Timestamp: time.Now().UTC()}
	savedTypes := itemTypes(artifacts)

	return s.processIngestion(ctx, ingestionParams{
		orgID:         event.OrgID,
		docID:         event.DocumentID,
		versionID:     event.VersionID,
		jobID:         event.JobID,
		correlationID: event.CorrelationID,
		producer:      model.ProducerDomainDP,
		targetStatus:  model.ArtifactStatusProcessingArtifactsReceived,
		artifacts:     artifacts,
		outboxEvents: []outbox.TopicEvent{
			{
				Topic: model.TopicDMResponsesArtifactsPersisted,
				Event: model.DocumentProcessingArtifactsPersisted{
					EventMeta:  meta,
					JobID:      event.JobID,
					DocumentID: event.DocumentID,
				},
			},
			{
				Topic: model.TopicDMEventsVersionArtifactsReady,
				Event: model.VersionProcessingArtifactsReady{
					EventMeta:     meta,
					DocumentID:    event.DocumentID,
					VersionID:     event.VersionID,
					OrgID:         event.OrgID,
					ArtifactTypes: savedTypes,
				},
			},
		},
	})
}

// HandleLICArtifacts processes a LIC analysis artifacts-ready event.
// Stores 8 analysis artifact blobs and transitions artifact_status to
// ANALYSIS_ARTIFACTS_RECEIVED.
func (s *ArtifactIngestionService) HandleLICArtifacts(ctx context.Context, event model.LegalAnalysisArtifactsReady) error {
	// REV-002: resolve organization_id if missing.
	if event.OrgID == "" {
		if err := s.resolveOrgID(ctx, event.DocumentID, &event.OrgID); err != nil {
			return err
		}
	}

	// BRE-015: verify document belongs to claimed organization.
	if err := tenant.VerifyTenantOwnership(ctx, s.docRepo, s.tenantMetrics, s.logger, event.OrgID, event.DocumentID); err != nil {
		return err
	}

	if err := validateRequired(event.OrgID, event.JobID, event.DocumentID, event.VersionID); err != nil {
		return err
	}

	artifacts := extractLICArtifacts(event)
	if len(artifacts) == 0 {
		return port.NewValidationError("no artifacts in LIC event")
	}

	meta := model.EventMeta{CorrelationID: event.CorrelationID, Timestamp: time.Now().UTC()}
	savedTypes := itemTypes(artifacts)

	return s.processIngestion(ctx, ingestionParams{
		orgID:         event.OrgID,
		docID:         event.DocumentID,
		versionID:     event.VersionID,
		jobID:         event.JobID,
		correlationID: event.CorrelationID,
		producer:      model.ProducerDomainLIC,
		targetStatus:  model.ArtifactStatusAnalysisArtifactsReceived,
		artifacts:     artifacts,
		outboxEvents: []outbox.TopicEvent{
			{
				Topic: model.TopicDMResponsesLICArtifactsPersisted,
				Event: model.LegalAnalysisArtifactsPersisted{
					EventMeta:  meta,
					JobID:      event.JobID,
					DocumentID: event.DocumentID,
				},
			},
			{
				Topic: model.TopicDMEventsVersionAnalysisReady,
				Event: model.VersionAnalysisArtifactsReady{
					EventMeta:     meta,
					DocumentID:    event.DocumentID,
					VersionID:     event.VersionID,
					OrgID:         event.OrgID,
					ArtifactTypes: savedTypes,
				},
			},
		},
	})
}

// HandleREArtifacts processes a RE reports artifacts-ready event.
// Uses the claim-check pattern: binary blobs are already in object storage;
// DM verifies their existence, records artifact descriptors, and transitions
// artifact_status to FULLY_READY.
func (s *ArtifactIngestionService) HandleREArtifacts(ctx context.Context, event model.ReportsArtifactsReady) error {
	// REV-002: resolve organization_id if missing.
	if event.OrgID == "" {
		if err := s.resolveOrgID(ctx, event.DocumentID, &event.OrgID); err != nil {
			return err
		}
	}

	// BRE-015: verify document belongs to claimed organization.
	if err := tenant.VerifyTenantOwnership(ctx, s.docRepo, s.tenantMetrics, s.logger, event.OrgID, event.DocumentID); err != nil {
		return err
	}

	if err := validateRequired(event.OrgID, event.JobID, event.DocumentID, event.VersionID); err != nil {
		return err
	}

	artifacts := extractREArtifacts(event)
	if len(artifacts) == 0 {
		return port.NewValidationError("no exports in RE event: at least one of export_pdf or export_docx required")
	}

	meta := model.EventMeta{CorrelationID: event.CorrelationID, Timestamp: time.Now().UTC()}
	savedTypes := itemTypes(artifacts)

	return s.processIngestion(ctx, ingestionParams{
		orgID:         event.OrgID,
		docID:         event.DocumentID,
		versionID:     event.VersionID,
		jobID:         event.JobID,
		correlationID: event.CorrelationID,
		producer:      model.ProducerDomainRE,
		targetStatus:  model.ArtifactStatusFullyReady,
		artifacts:     artifacts,
		outboxEvents: []outbox.TopicEvent{
			{
				Topic: model.TopicDMResponsesREReportsPersisted,
				Event: model.ReportsArtifactsPersisted{
					EventMeta:  meta,
					JobID:      event.JobID,
					DocumentID: event.DocumentID,
				},
			},
			{
				Topic: model.TopicDMEventsVersionReportsReady,
				Event: model.VersionReportsReady{
					EventMeta:     meta,
					DocumentID:    event.DocumentID,
					VersionID:     event.VersionID,
					OrgID:         event.OrgID,
					ArtifactTypes: savedTypes,
				},
			},
		},
	})
}

// ---------------------------------------------------------------------------
// Internal: common ingestion flow.
// ---------------------------------------------------------------------------

// ingestionParams holds all parameters for the shared ingestion logic.
type ingestionParams struct {
	orgID         string
	docID         string
	versionID     string
	jobID         string
	correlationID string
	producer      model.ProducerDomain
	targetStatus  model.ArtifactStatus
	artifacts     []artifactItem
	outboxEvents  []outbox.TopicEvent
}

// artifactItem represents a single artifact to be ingested.
type artifactItem struct {
	artifactType model.ArtifactType
	data         json.RawMessage      // non-nil for DP/LIC: blob content to upload
	blobRef      *model.BlobReference // non-nil for RE: already-uploaded blob reference
}

// savedBlob tracks the result of saving a single artifact to object storage.
type savedBlob struct {
	storageKey  string // S3 key (empty for claim-check that shouldn't be compensated)
	sizeBytes   int64
	contentHash string
	uploaded    bool // true if DM uploaded this blob (needs compensation on failure)
}

// processIngestion is the common flow shared by all three handlers:
//  1. Save blobs to Object Storage (or verify refs for RE claim-check).
//  2. DB transaction: insert descriptors, transition status, audit, outbox events.
func (s *ArtifactIngestionService) processIngestion(ctx context.Context, p ingestionParams) error {
	if err := ctx.Err(); err != nil {
		return port.NewTimeoutError("context cancelled before ingestion", err)
	}

	s.logger.Info("ingestion started",
		"producer", p.producer, "document_id", p.docID,
		"version_id", p.versionID, "job_id", p.jobID,
		"artifact_count", len(p.artifacts),
	)

	// Step 1: Save blobs to Object Storage (or verify refs for RE).
	blobs, err := s.saveBlobs(ctx, p)
	if err != nil {
		s.compensate(blobs)
		return err
	}

	// Step 2: DB transaction — insert descriptors, transition status, audit, outbox.
	if err := s.transactor.WithTransaction(ctx, func(txCtx context.Context) error {
		// 2a. Lock version row (FOR UPDATE) and validate status transition.
		// SELECT ... FOR UPDATE serializes concurrent artifact_status updates,
		// preventing race conditions when DP, LIC, and RE events arrive
		// simultaneously for the same version (BRE-001).
		version, findErr := s.versionRepo.FindByIDForUpdate(txCtx, p.orgID, p.docID, p.versionID)
		if findErr != nil {
			return findErr
		}

		oldStatus := version.ArtifactStatus
		if err := version.TransitionArtifactStatus(p.targetStatus); err != nil {
			// Retryable: true — the transition may become valid after a prior
			// stage completes (e.g., LIC arrives before DP commits). Currently the
			// consumer drops the message (always returns nil), but the retryable flag
			// enables future NACK-with-requeue + bounded retry logic (DM-TASK-023).
			return &port.DomainError{
				Code:      port.ErrCodeStatusTransition,
				Message:   fmt.Sprintf("invalid status transition from %s to %s", oldStatus, p.targetStatus),
				Retryable: true,
				Cause:     err,
			}
		}

		// 2b. Insert artifact descriptors.
		for i, item := range p.artifacts {
			storageKey := blobs[i].storageKey
			if item.blobRef != nil {
				storageKey = item.blobRef.StorageKey
			}

			descriptor := model.NewArtifactDescriptor(
				s.newUUID(), p.versionID, p.docID, p.orgID,
				item.artifactType, p.producer,
				storageKey, blobs[i].sizeBytes, blobs[i].contentHash,
				defaultSchemaVersion, p.jobID, p.correlationID,
			)
			if err := s.artifactRepo.Insert(txCtx, descriptor); err != nil {
				return err
			}
		}

		// 2c. Update version artifact_status.
		if err := s.versionRepo.Update(txCtx, version); err != nil {
			return err
		}

		// 2d. Insert audit records.
		savedDetails, marshalErr := json.Marshal(map[string]any{
			"producer":       string(p.producer),
			"artifact_types": itemTypes(p.artifacts),
			"artifact_count": len(p.artifacts),
		})
		if marshalErr != nil {
			s.logger.Warn("failed to marshal audit saved details", "error", marshalErr)
		}
		auditSaved := model.NewAuditRecord(
			s.newUUID(), p.orgID, model.AuditActionArtifactSaved,
			model.ActorTypeDomain, string(p.producer),
		).WithDocument(p.docID).
			WithVersion(p.versionID).
			WithJob(p.jobID, p.correlationID).
			WithDetails(savedDetails)
		if err := s.auditRepo.Insert(txCtx, auditSaved); err != nil {
			return err
		}

		statusDetails, marshalErr2 := json.Marshal(map[string]any{
			"from": string(oldStatus),
			"to":   string(p.targetStatus),
		})
		if marshalErr2 != nil {
			s.logger.Warn("failed to marshal audit status details", "error", marshalErr2)
		}
		auditStatus := model.NewAuditRecord(
			s.newUUID(), p.orgID, model.AuditActionArtifactStatusChanged,
			model.ActorTypeDomain, string(p.producer),
		).WithDocument(p.docID).
			WithVersion(p.versionID).
			WithJob(p.jobID, p.correlationID).
			WithDetails(statusDetails)
		if err := s.auditRepo.Insert(txCtx, auditStatus); err != nil {
			return err
		}

		// 2e. Write outbox events (confirmation + notification).
		return s.outboxWriter.WriteMultiple(txCtx, p.versionID, p.outboxEvents)
	}); err != nil {
		s.logger.Error("ingestion transaction failed",
			"producer", p.producer, "document_id", p.docID,
			"version_id", p.versionID, "error", err,
		)
		// Compensate uploaded blobs: DB rolled back but blobs persist in S3.
		s.compensate(blobs)
		return err
	}

	s.logger.Info("ingestion completed",
		"producer", p.producer, "document_id", p.docID,
		"version_id", p.versionID, "status", p.targetStatus,
	)
	return nil
}

// saveBlobs uploads artifact data to Object Storage and returns metadata
// for each saved blob. For claim-check (RE) items, it verifies the blob
// exists via HeadObject instead of uploading.
func (s *ArtifactIngestionService) saveBlobs(ctx context.Context, p ingestionParams) ([]savedBlob, error) {
	blobs := make([]savedBlob, 0, len(p.artifacts))

	for _, item := range p.artifacts {
		if item.blobRef != nil {
			// Claim-check: verify the pre-uploaded blob exists.
			_, exists, err := s.objectStorage.HeadObject(ctx, item.blobRef.StorageKey)
			if err != nil {
				return blobs, port.NewStorageError(
					fmt.Sprintf("head object %s for %s", item.blobRef.StorageKey, item.artifactType), err)
			}
			if !exists {
				return blobs, port.NewStorageError(
					fmt.Sprintf("claim-check blob not found at %s for %s", item.blobRef.StorageKey, item.artifactType), nil)
			}
			blobs = append(blobs, savedBlob{
				storageKey:  item.blobRef.StorageKey,
				sizeBytes:   item.blobRef.SizeBytes,
				contentHash: item.blobRef.ContentHash,
				uploaded:    false,
			})
			continue
		}

		key := objectstorage.ArtifactKey(p.orgID, p.docID, p.versionID, item.artifactType)
		contentType := objectstorage.ContentTypeForArtifact(item.artifactType)

		if err := s.objectStorage.PutObject(ctx, key, bytes.NewReader(item.data), contentType); err != nil {
			return blobs, port.NewStorageError(
				fmt.Sprintf("put object %s for %s", key, item.artifactType), err)
		}

		blobs = append(blobs, savedBlob{
			storageKey:  key,
			sizeBytes:   int64(len(item.data)),
			contentHash: sha256Hex(item.data),
			uploaded:    true,
		})
	}

	return blobs, nil
}

// compensate deletes already-uploaded blobs from Object Storage on failure.
// Uses context.Background() because the original request context may have
// been cancelled. Best-effort: errors are logged but not propagated.
func (s *ArtifactIngestionService) compensate(blobs []savedBlob) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	for _, blob := range blobs {
		if !blob.uploaded {
			continue
		}
		if err := s.objectStorage.DeleteObject(ctx, blob.storageKey); err != nil {
			s.logger.Warn("compensation: failed to delete blob",
				"key", blob.storageKey, "error", err,
			)
		}
	}
}

// ---------------------------------------------------------------------------
// Artifact extraction helpers: event fields → []artifactItem.
// ---------------------------------------------------------------------------

func extractDPArtifacts(event model.DocumentProcessingArtifactsReady) []artifactItem {
	items := make([]artifactItem, 0, 5)
	items = appendIfNonEmpty(items, model.ArtifactTypeOCRRaw, event.OCRRaw)
	items = appendIfNonEmpty(items, model.ArtifactTypeExtractedText, event.Text)
	items = appendIfNonEmpty(items, model.ArtifactTypeDocumentStructure, event.Structure)
	items = appendIfNonEmpty(items, model.ArtifactTypeSemanticTree, event.SemanticTree)
	items = appendIfNonEmpty(items, model.ArtifactTypeProcessingWarnings, event.Warnings)
	return items
}

func extractLICArtifacts(event model.LegalAnalysisArtifactsReady) []artifactItem {
	items := make([]artifactItem, 0, 8)
	items = appendIfNonEmpty(items, model.ArtifactTypeClassificationResult, event.ClassificationResult)
	items = appendIfNonEmpty(items, model.ArtifactTypeKeyParameters, event.KeyParameters)
	items = appendIfNonEmpty(items, model.ArtifactTypeRiskAnalysis, event.RiskAnalysis)
	items = appendIfNonEmpty(items, model.ArtifactTypeRiskProfile, event.RiskProfile)
	items = appendIfNonEmpty(items, model.ArtifactTypeRecommendations, event.Recommendations)
	items = appendIfNonEmpty(items, model.ArtifactTypeSummary, event.Summary)
	items = appendIfNonEmpty(items, model.ArtifactTypeDetailedReport, event.DetailedReport)
	items = appendIfNonEmpty(items, model.ArtifactTypeAggregateScore, event.AggregateScore)
	return items
}

func extractREArtifacts(event model.ReportsArtifactsReady) []artifactItem {
	items := make([]artifactItem, 0, 2)
	if event.ExportPDF != nil {
		items = append(items, artifactItem{
			artifactType: model.ArtifactTypeExportPDF,
			blobRef:      event.ExportPDF,
		})
	}
	if event.ExportDOCX != nil {
		items = append(items, artifactItem{
			artifactType: model.ArtifactTypeExportDOCX,
			blobRef:      event.ExportDOCX,
		})
	}
	return items
}

func appendIfNonEmpty(items []artifactItem, at model.ArtifactType, data json.RawMessage) []artifactItem {
	if len(data) > 0 {
		items = append(items, artifactItem{artifactType: at, data: data})
	}
	return items
}

// ---------------------------------------------------------------------------
// Defensive fallback resolvers (REV-001/REV-002).
// TEMPORARY: remove when DP TASK-056 and TASK-057 are completed.
// ---------------------------------------------------------------------------

// resolveOrgID looks up the organization_id for a document when the incoming
// event omits it (REV-002). Mutates *target in place and logs a warning.
func (s *ArtifactIngestionService) resolveOrgID(ctx context.Context, documentID string, target *string) error {
	orgID, _, err := s.fallbackResolver.ResolveByDocumentID(ctx, documentID)
	if err != nil {
		s.logger.Error("REV-002 fallback: failed to resolve organization_id",
			"document_id", documentID, "error", err)
		return err
	}
	s.logger.Warn("REV-002 fallback: resolved organization_id from DB (event field was empty)",
		"document_id", documentID, "organization_id", orgID)
	*target = orgID
	return nil
}

// resolveDPEventFields resolves both organization_id and version_id for DP
// events using a single DB lookup. Used by HandleDPArtifacts to avoid two
// identical queries when both fields are empty (REV-001/REV-002).
func (s *ArtifactIngestionService) resolveDPEventFields(
	ctx context.Context, documentID string,
	orgTarget, versionTarget *string,
) error {
	orgID, versionID, err := s.fallbackResolver.ResolveByDocumentID(ctx, documentID)
	if err != nil {
		s.logger.Error("REV-001/REV-002 fallback: failed to resolve document fields",
			"document_id", documentID, "error", err)
		return err
	}

	if *orgTarget == "" {
		s.logger.Warn("REV-002 fallback: resolved organization_id from DB (event field was empty)",
			"document_id", documentID, "organization_id", orgID)
		*orgTarget = orgID
	}

	if *versionTarget == "" {
		if versionID == "" {
			return port.NewValidationError("REV-001 fallback: document " + documentID + " has no current_version_id")
		}
		s.logger.Warn("REV-001 fallback: resolved version_id from DB (event field was empty)",
			"document_id", documentID, "version_id", versionID)
		s.fallbackMetrics.IncMissingVersionID()
		*versionTarget = versionID
	}

	return nil
}

// ---------------------------------------------------------------------------
// Utility functions.
// ---------------------------------------------------------------------------

func itemTypes(items []artifactItem) []model.ArtifactType {
	types := make([]model.ArtifactType, len(items))
	for i, item := range items {
		types[i] = item.artifactType
	}
	return types
}

func validateRequired(orgID, jobID, documentID, versionID string) error {
	if orgID == "" {
		return port.NewValidationError("organization_id is required")
	}
	if jobID == "" {
		return port.NewValidationError("job_id is required")
	}
	if documentID == "" {
		return port.NewValidationError("document_id is required")
	}
	if versionID == "" {
		return port.NewValidationError("version_id is required")
	}
	return nil
}

func sha256Hex(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

// generateUUID produces a UUID v4 using crypto/rand.
// Panics if crypto/rand fails (broken system CSPRNG — fatal condition).
func generateUUID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("ingestion: crypto/rand failed: " + err.Error())
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
