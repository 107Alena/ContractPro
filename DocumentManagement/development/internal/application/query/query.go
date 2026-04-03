package query

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"contractpro/document-management/internal/application/tenant"
	"contractpro/document-management/internal/domain/model"
	"contractpro/document-management/internal/domain/port"
	"contractpro/document-management/internal/infra/objectstorage"
)

// maxArtifactReadBytes is the safety limit for reading artifact content
// from object storage. Prevents OOM on corrupted or oversized objects.
const maxArtifactReadBytes = 50 * 1024 * 1024 // 50 MB

// Logger is the minimal structured logging interface.
type Logger interface {
	Info(msg string, keysAndValues ...any)
	Warn(msg string, keysAndValues ...any)
	Error(msg string, keysAndValues ...any)
}

// ArtifactQueryService serves artifact retrieval requests from other domains
// (async via events) and from the REST API (sync).
type ArtifactQueryService struct {
	artifactRepo     port.ArtifactRepository
	objectStorage    port.ObjectStoragePort
	confirmation     port.ConfirmationPublisherPort
	auditRepo        port.AuditRepository
	fallbackResolver port.DocumentFallbackResolver
	docRepo          tenant.DocumentExistenceChecker
	tenantMetrics    tenant.Metrics
	logger           Logger
	newUUID          func() string
}

// Compile-time interface check.
var _ port.ArtifactQueryHandler = (*ArtifactQueryService)(nil)

// NewArtifactQueryService creates a new service with the given dependencies.
// Panics if any required dependency is nil (programmer error at startup wiring).
func NewArtifactQueryService(
	artifactRepo port.ArtifactRepository,
	objectStorage port.ObjectStoragePort,
	confirmation port.ConfirmationPublisherPort,
	auditRepo port.AuditRepository,
	fallbackResolver port.DocumentFallbackResolver,
	docRepo tenant.DocumentExistenceChecker,
	tenantMetrics tenant.Metrics,
	logger Logger,
) *ArtifactQueryService {
	if artifactRepo == nil {
		panic("query: artifactRepo must not be nil")
	}
	if objectStorage == nil {
		panic("query: objectStorage must not be nil")
	}
	if confirmation == nil {
		panic("query: confirmation must not be nil")
	}
	if auditRepo == nil {
		panic("query: auditRepo must not be nil")
	}
	if fallbackResolver == nil {
		panic("query: fallbackResolver must not be nil")
	}
	if docRepo == nil {
		panic("query: docRepo must not be nil")
	}
	if tenantMetrics == nil {
		panic("query: tenantMetrics must not be nil")
	}
	if logger == nil {
		panic("query: logger must not be nil")
	}
	return &ArtifactQueryService{
		artifactRepo:     artifactRepo,
		objectStorage:    objectStorage,
		confirmation:     confirmation,
		auditRepo:        auditRepo,
		fallbackResolver: fallbackResolver,
		docRepo:          docRepo,
		tenantMetrics:    tenantMetrics,
		logger:           logger,
		newUUID:          generateUUID,
	}
}

// ---------------------------------------------------------------------------
// Async handlers — called by event consumers.
// ---------------------------------------------------------------------------

// HandleGetSemanticTree processes a GetSemanticTreeRequest from DP.
// Retrieves the semantic tree artifact and publishes a SemanticTreeProvided
// response. On "not found", publishes a response with error fields.
// On infrastructure failure, returns an error for retry.
func (s *ArtifactQueryService) HandleGetSemanticTree(ctx context.Context, event model.GetSemanticTreeRequest) error {
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

	if err := validateQueryRequired(event.OrgID, event.JobID, event.DocumentID, event.VersionID); err != nil {
		return err
	}

	meta := model.EventMeta{CorrelationID: event.CorrelationID, Timestamp: time.Now().UTC()}

	// Find the SEMANTIC_TREE artifact descriptor.
	descriptor, err := s.artifactRepo.FindByVersionAndType(
		ctx, event.OrgID, event.DocumentID, event.VersionID, model.ArtifactTypeSemanticTree,
	)
	if err != nil {
		if port.IsNotFound(err) {
			// Artifact not found — publish response with error fields.
			s.logger.Warn("semantic tree not found",
				"document_id", event.DocumentID, "version_id", event.VersionID,
				"correlation_id", event.CorrelationID,
			)
			return s.confirmation.PublishSemanticTreeProvided(ctx, model.SemanticTreeProvided{
				EventMeta:    meta,
				JobID:        event.JobID,
				DocumentID:   event.DocumentID,
				VersionID:    event.VersionID,
				ErrorCode:    port.ErrCodeArtifactNotFound,
				ErrorMessage: fmt.Sprintf("semantic tree not found for version %s", event.VersionID),
			})
		}
		// Infrastructure failure — return error for retry.
		return err
	}

	// Read artifact content from object storage.
	data, err := s.readArtifact(ctx, descriptor.StorageKey)
	if err != nil {
		return err
	}

	// Record audit (best-effort, non-blocking for async handler).
	s.recordAuditAsync(event.OrgID, event.DocumentID, event.VersionID,
		event.JobID, event.CorrelationID,
		"DP", []model.ArtifactType{model.ArtifactTypeSemanticTree})

	s.logger.Info("semantic tree provided",
		"document_id", event.DocumentID, "version_id", event.VersionID,
		"correlation_id", event.CorrelationID, "size_bytes", len(data),
	)

	return s.confirmation.PublishSemanticTreeProvided(ctx, model.SemanticTreeProvided{
		EventMeta:    meta,
		JobID:        event.JobID,
		DocumentID:   event.DocumentID,
		VersionID:    event.VersionID,
		SemanticTree: json.RawMessage(data),
	})
}

// HandleGetArtifacts processes a GetArtifactsRequest from LIC or RE.
// Retrieves the requested artifacts and publishes an ArtifactsProvided
// response with the found artifacts and any missing types.
func (s *ArtifactQueryService) HandleGetArtifacts(ctx context.Context, event model.GetArtifactsRequest) error {
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

	if err := validateQueryRequired(event.OrgID, event.JobID, event.DocumentID, event.VersionID); err != nil {
		return err
	}
	if len(event.ArtifactTypes) == 0 {
		return port.NewValidationError("artifact_types must not be empty")
	}

	meta := model.EventMeta{CorrelationID: event.CorrelationID, Timestamp: time.Now().UTC()}

	// Find descriptors for the requested artifact types.
	descriptors, err := s.artifactRepo.ListByVersionAndTypes(
		ctx, event.OrgID, event.DocumentID, event.VersionID, event.ArtifactTypes,
	)
	if err != nil {
		// Infrastructure failure — return error for retry.
		return err
	}

	// Build a set of found types for missing detection.
	foundTypes := make(map[model.ArtifactType]struct{}, len(descriptors))
	for _, d := range descriptors {
		foundTypes[d.ArtifactType] = struct{}{}
	}

	// Determine missing types.
	var missingTypes []model.ArtifactType
	for _, reqType := range event.ArtifactTypes {
		if _, ok := foundTypes[reqType]; !ok {
			missingTypes = append(missingTypes, reqType)
		}
	}

	// Read content for each found descriptor.
	artifacts := make(map[model.ArtifactType]json.RawMessage, len(descriptors))
	for _, d := range descriptors {
		data, readErr := s.readArtifact(ctx, d.StorageKey)
		if readErr != nil {
			// Infrastructure failure reading artifact content.
			return readErr
		}
		artifacts[d.ArtifactType] = json.RawMessage(data)
	}

	// Record audit (best-effort, non-blocking for async handler).
	readTypes := make([]model.ArtifactType, 0, len(descriptors))
	for _, d := range descriptors {
		readTypes = append(readTypes, d.ArtifactType)
	}
	requester := inferRequesterDomain(event.ArtifactTypes)
	s.recordAuditAsync(event.OrgID, event.DocumentID, event.VersionID,
		event.JobID, event.CorrelationID,
		requester, readTypes)

	s.logger.Info("artifacts provided",
		"document_id", event.DocumentID, "version_id", event.VersionID,
		"correlation_id", event.CorrelationID,
		"found", len(artifacts), "missing", len(missingTypes),
	)

	return s.confirmation.PublishArtifactsProvided(ctx, model.ArtifactsProvided{
		EventMeta:    meta,
		JobID:        event.JobID,
		DocumentID:   event.DocumentID,
		VersionID:    event.VersionID,
		Artifacts:    artifacts,
		MissingTypes: missingTypes,
	})
}

// ---------------------------------------------------------------------------
// Sync handlers — called by REST API.
// ---------------------------------------------------------------------------

// GetArtifact retrieves a single artifact's content by type (sync API).
func (s *ArtifactQueryService) GetArtifact(ctx context.Context, params port.GetArtifactParams) (*port.ArtifactContent, error) {
	if err := validateGetArtifactParams(params); err != nil {
		return nil, err
	}

	descriptor, err := s.artifactRepo.FindByVersionAndType(
		ctx, params.OrganizationID, params.DocumentID, params.VersionID, params.ArtifactType,
	)
	if err != nil {
		return nil, err
	}

	data, err := s.readArtifact(ctx, descriptor.StorageKey)
	if err != nil {
		return nil, err
	}

	contentType := objectstorage.ContentTypeForArtifact(params.ArtifactType)

	return &port.ArtifactContent{
		Content:     data,
		ContentType: contentType,
	}, nil
}

// ListArtifacts returns all artifact descriptors for a document version.
func (s *ArtifactQueryService) ListArtifacts(ctx context.Context, organizationID, documentID, versionID string) ([]*model.ArtifactDescriptor, error) {
	if organizationID == "" {
		return nil, port.NewValidationError("organization_id is required")
	}
	if documentID == "" {
		return nil, port.NewValidationError("document_id is required")
	}
	if versionID == "" {
		return nil, port.NewValidationError("version_id is required")
	}

	descriptors, err := s.artifactRepo.ListByVersion(ctx, organizationID, documentID, versionID)
	if err != nil {
		return nil, err
	}

	// Normalize nil to empty slice for consistent JSON serialization.
	if descriptors == nil {
		descriptors = []*model.ArtifactDescriptor{}
	}

	return descriptors, nil
}

// ---------------------------------------------------------------------------
// Internal helpers.
// ---------------------------------------------------------------------------

// readArtifact reads artifact content from object storage with a safety size limit.
func (s *ArtifactQueryService) readArtifact(ctx context.Context, storageKey string) ([]byte, error) {
	reader, err := s.objectStorage.GetObject(ctx, storageKey)
	if err != nil {
		return nil, err
	}
	defer reader.Close()

	limited := io.LimitReader(reader, maxArtifactReadBytes+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, port.NewStorageError(fmt.Sprintf("read object %s", storageKey), err)
	}
	if int64(len(data)) > maxArtifactReadBytes {
		return nil, &port.DomainError{
			Code:      port.ErrCodeValidation,
			Message:   fmt.Sprintf("artifact %s exceeds size limit (%d bytes)", storageKey, maxArtifactReadBytes),
			Retryable: false,
		}
	}
	return data, nil
}

// recordAuditAsync records an ARTIFACT_READ audit record in a separate goroutine.
// Uses context.Background() with a short timeout to avoid blocking the response path.
func (s *ArtifactQueryService) recordAuditAsync(
	orgID, docID, versionID, jobID, correlationID string,
	requesterDomain string, readTypes []model.ArtifactType,
) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		details, err := json.Marshal(map[string]any{
			"requester":      requesterDomain,
			"artifact_types": readTypes,
			"artifact_count": len(readTypes),
		})
		if err != nil {
			s.logger.Warn("failed to marshal audit details", "error", err)
			details = []byte("{}")
		}

		record := model.NewAuditRecord(
			s.newUUID(), orgID, model.AuditActionArtifactRead,
			model.ActorTypeDomain, requesterDomain,
		).WithDocument(docID).
			WithVersion(versionID).
			WithJob(jobID, correlationID).
			WithDetails(details)

		if err := s.auditRepo.Insert(ctx, record); err != nil {
			s.logger.Warn("failed to record audit for artifact read",
				"document_id", docID, "version_id", versionID,
				"requester", requesterDomain, "error", err,
			)
		}
	}()
}

// inferRequesterDomain guesses the requester domain from the requested artifact types.
// LIC typically requests DP artifacts; RE typically requests LIC artifacts.
func inferRequesterDomain(requestedTypes []model.ArtifactType) string {
	if len(requestedTypes) == 0 {
		return "UNKNOWN"
	}
	for _, t := range requestedTypes {
		for _, licType := range model.ArtifactTypesByProducer[model.ProducerDomainLIC] {
			if t == licType {
				return "RE"
			}
		}
	}
	for _, t := range requestedTypes {
		for _, dpType := range model.ArtifactTypesByProducer[model.ProducerDomainDP] {
			if t == dpType {
				return "LIC"
			}
		}
	}
	return "UNKNOWN"
}

// ---------------------------------------------------------------------------
// Defensive fallback resolver (REV-002).
// TEMPORARY: remove when DP TASK-056 and TASK-057 are completed.
// ---------------------------------------------------------------------------

// resolveOrgID looks up the organization_id for a document when the incoming
// event omits it (REV-002). Mutates *target in place and logs a warning.
func (s *ArtifactQueryService) resolveOrgID(ctx context.Context, documentID string, target *string) error {
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

// validateQueryRequired validates common required fields for async query handlers.
func validateQueryRequired(orgID, jobID, documentID, versionID string) error {
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

// validateGetArtifactParams validates params for the sync GetArtifact endpoint.
func validateGetArtifactParams(p port.GetArtifactParams) error {
	if p.OrganizationID == "" {
		return port.NewValidationError("organization_id is required")
	}
	if p.DocumentID == "" {
		return port.NewValidationError("document_id is required")
	}
	if p.VersionID == "" {
		return port.NewValidationError("version_id is required")
	}
	if p.ArtifactType == "" {
		return port.NewValidationError("artifact_type is required")
	}
	return nil
}

// generateUUID produces a UUID v4 using crypto/rand.
// Panics if crypto/rand fails (broken system CSPRNG — fatal condition).
func generateUUID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("query: crypto/rand failed: " + err.Error())
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
