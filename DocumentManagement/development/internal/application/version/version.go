package version

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"time"

	"contractpro/document-management/internal/domain/model"
	"contractpro/document-management/internal/domain/port"
)

// maxPageSize is the upper bound for pagination to prevent full-table scans.
const maxPageSize = 100

// maxRetries is the number of attempts for optimistic locking on version creation.
const maxRetries = 3

// Logger is the minimal structured logging interface.
type Logger interface {
	Info(msg string, keysAndValues ...any)
	Warn(msg string, keysAndValues ...any)
	Error(msg string, keysAndValues ...any)
}

// OutboxWriter writes events to the transactional outbox within the current
// database transaction.
type OutboxWriter interface {
	Write(ctx context.Context, aggregateID, topic string, event any) error
}

// VersionManagementService manages document version creation and retrieval.
type VersionManagementService struct {
	transactor  port.Transactor
	docRepo     port.DocumentRepository
	versionRepo port.VersionRepository
	auditRepo   port.AuditRepository
	outbox      OutboxWriter
	logger      Logger
	newUUID     func() string
	nowFunc     func() time.Time
}

// Compile-time interface check.
var _ port.VersionManagementHandler = (*VersionManagementService)(nil)

// NewVersionManagementService creates a new service with the given dependencies.
// Panics if any required dependency is nil (programmer error at startup wiring).
func NewVersionManagementService(
	transactor port.Transactor,
	docRepo port.DocumentRepository,
	versionRepo port.VersionRepository,
	auditRepo port.AuditRepository,
	outbox OutboxWriter,
	logger Logger,
) *VersionManagementService {
	if transactor == nil {
		panic("version: transactor must not be nil")
	}
	if docRepo == nil {
		panic("version: docRepo must not be nil")
	}
	if versionRepo == nil {
		panic("version: versionRepo must not be nil")
	}
	if auditRepo == nil {
		panic("version: auditRepo must not be nil")
	}
	if outbox == nil {
		panic("version: outbox must not be nil")
	}
	if logger == nil {
		panic("version: logger must not be nil")
	}
	return &VersionManagementService{
		transactor:  transactor,
		docRepo:     docRepo,
		versionRepo: versionRepo,
		auditRepo:   auditRepo,
		outbox:      outbox,
		logger:      logger,
		newUUID:     generateUUID,
		nowFunc:     func() time.Time { return time.Now().UTC() },
	}
}

// ---------------------------------------------------------------------------
// Public handlers — implements port.VersionManagementHandler.
// ---------------------------------------------------------------------------

// CreateVersion creates a new document version with optimistic locking.
// Validates that the document exists and is ACTIVE (inside the transaction to
// prevent TOCTOU race conditions).
// For RE_CHECK origin, copies source_file_key from the parent version.
// Publishes a VersionCreated notification via the outbox.
// Retries up to 3 times on unique constraint violation (concurrent version creation).
func (s *VersionManagementService) CreateVersion(ctx context.Context, params port.CreateVersionParams) (*model.DocumentVersion, error) {
	if err := s.validateCreateParams(params); err != nil {
		return nil, err
	}

	// Resolve source_file_key for RE_CHECK: copy from parent version.
	// Parent version lookup is outside the transaction because versions are
	// immutable — once created they cannot be modified or deleted.
	sourceFileKey := params.SourceFileKey
	if params.OriginType == model.OriginTypeReCheck {
		if params.ParentVersionID == "" {
			return nil, port.NewValidationError("parent_version_id is required for RE_CHECK origin")
		}
		parentVersion, err := s.versionRepo.FindByID(ctx, params.OrganizationID, params.DocumentID, params.ParentVersionID)
		if err != nil {
			return nil, err
		}
		sourceFileKey = parentVersion.SourceFileKey
	}

	// Retry loop for optimistic locking on version_number unique constraint.
	// The document status check happens inside the transaction to prevent TOCTOU.
	var (
		version *model.DocumentVersion
		err     error
	)
	for attempt := 0; attempt < maxRetries; attempt++ {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		version, err = s.createVersionInTx(ctx, params, sourceFileKey)
		if err == nil {
			break
		}
		if port.ErrorCode(err) != port.ErrCodeVersionAlreadyExists {
			return nil, err
		}
		s.logger.Warn("version creation conflict, retrying",
			"document_id", params.DocumentID,
			"attempt", attempt+1,
			"error", err,
		)
	}
	if err != nil {
		s.logger.Error("version creation failed after retries",
			"document_id", params.DocumentID,
			"organization_id", params.OrganizationID,
			"error", err,
		)
		return nil, err
	}

	s.logger.Info("version created",
		"version_id", version.VersionID,
		"document_id", version.DocumentID,
		"organization_id", version.OrganizationID,
		"version_number", version.VersionNumber,
		"origin_type", version.OriginType,
	)
	return version, nil
}

// GetVersion returns a specific version by ID within the given organization and document.
func (s *VersionManagementService) GetVersion(ctx context.Context, organizationID, documentID, versionID string) (*model.DocumentVersion, error) {
	if organizationID == "" {
		return nil, port.NewValidationError("organization_id is required")
	}
	if documentID == "" {
		return nil, port.NewValidationError("document_id is required")
	}
	if versionID == "" {
		return nil, port.NewValidationError("version_id is required")
	}
	return s.versionRepo.FindByID(ctx, organizationID, documentID, versionID)
}

// ListVersions returns a paginated list of versions for a document.
func (s *VersionManagementService) ListVersions(ctx context.Context, params port.ListVersionsParams) (*port.PageResult[*model.DocumentVersion], error) {
	if params.OrganizationID == "" {
		return nil, port.NewValidationError("organization_id is required")
	}
	if params.DocumentID == "" {
		return nil, port.NewValidationError("document_id is required")
	}
	if params.Page < 1 {
		return nil, port.NewValidationError("page must be >= 1")
	}
	if params.PageSize < 1 {
		return nil, port.NewValidationError("page_size must be >= 1")
	}
	pageSize := params.PageSize
	if pageSize > maxPageSize {
		pageSize = maxPageSize
	}

	versions, totalCount, err := s.versionRepo.List(ctx, params.OrganizationID, params.DocumentID, params.Page, pageSize)
	if err != nil {
		return nil, err
	}

	// Normalize nil slice to empty for deterministic JSON serialization ([] not null).
	if versions == nil {
		versions = []*model.DocumentVersion{}
	}

	return &port.PageResult[*model.DocumentVersion]{
		Items:      versions,
		TotalCount: totalCount,
		Page:       params.Page,
		PageSize:   pageSize,
	}, nil
}

// ---------------------------------------------------------------------------
// Internal helpers.
// ---------------------------------------------------------------------------

// createVersionInTx executes the version creation within a single DB transaction:
// FindByID (status check) → NextVersionNumber → Insert version →
// Update document.current_version_id → Audit → Outbox.
//
// The document is fetched inside the transaction to prevent TOCTOU race
// conditions (another request could archive/delete the document between the
// check and the update). A fresh fetch on each retry also avoids leaking
// mutations from a rolled-back attempt.
func (s *VersionManagementService) createVersionInTx(
	ctx context.Context,
	params port.CreateVersionParams,
	sourceFileKey string,
) (*model.DocumentVersion, error) {
	var version *model.DocumentVersion

	err := s.transactor.WithTransaction(ctx, func(txCtx context.Context) error {
		// Fetch document inside transaction for atomicity.
		doc, err := s.docRepo.FindByID(txCtx, params.OrganizationID, params.DocumentID)
		if err != nil {
			return err
		}
		if doc.Status != model.DocumentStatusActive {
			return port.NewInvalidStatusError(
				fmt.Sprintf("document %s is %s, must be ACTIVE to create a version", params.DocumentID, doc.Status),
			)
		}

		versionNumber, err := s.versionRepo.NextVersionNumber(txCtx, params.OrganizationID, params.DocumentID)
		if err != nil {
			return err
		}

		versionID := s.newUUID()
		version = model.NewDocumentVersion(
			versionID,
			params.DocumentID,
			params.OrganizationID,
			versionNumber,
			params.OriginType,
			sourceFileKey,
			params.SourceFileName,
			params.SourceFileSize,
			params.SourceFileChecksum,
			params.CreatedByUserID,
		)
		version.ParentVersionID = params.ParentVersionID
		version.OriginDescription = params.OriginDescription

		if err := s.versionRepo.Insert(txCtx, version); err != nil {
			return err
		}

		// Update document's current version pointer.
		doc.CurrentVersionID = versionID
		doc.UpdatedAt = s.nowFunc()
		if err := s.docRepo.Update(txCtx, doc); err != nil {
			return err
		}

		// Audit record: VERSION_CREATED.
		details, jsonErr := json.Marshal(map[string]any{
			"version_number": versionNumber,
			"origin_type":    string(params.OriginType),
		})
		if jsonErr != nil {
			s.logger.Warn("failed to marshal version created audit details", "error", jsonErr)
		}
		audit := model.NewAuditRecord(s.newUUID(), params.OrganizationID,
			model.AuditActionVersionCreated, model.ActorTypeUser, params.CreatedByUserID).
			WithDocument(params.DocumentID).
			WithVersion(versionID).
			WithDetails(details)

		if err := s.auditRepo.Insert(txCtx, audit); err != nil {
			return err
		}

		// Outbox: VersionCreated notification event.
		notificationEvent := model.VersionCreated{
			EventMeta: model.EventMeta{
				CorrelationID: s.newUUID(),
				Timestamp:     s.nowFunc(),
			},
			DocumentID:      params.DocumentID,
			VersionID:       versionID,
			VersionNumber:   versionNumber,
			OrgID:           params.OrganizationID,
			OriginType:      params.OriginType,
			ParentVersionID: params.ParentVersionID,
			CreatedByUserID: params.CreatedByUserID,
		}
		return s.outbox.Write(txCtx, versionID, model.TopicDMEventsVersionCreated, notificationEvent)
	})
	if err != nil {
		return nil, err
	}

	return version, nil
}

// validateCreateParams validates the required fields for version creation.
func (s *VersionManagementService) validateCreateParams(params port.CreateVersionParams) error {
	if params.OrganizationID == "" {
		return port.NewValidationError("organization_id is required")
	}
	if params.DocumentID == "" {
		return port.NewValidationError("document_id is required")
	}
	if !isValidOriginType(params.OriginType) {
		return port.NewValidationError(fmt.Sprintf("invalid origin_type: %s", params.OriginType))
	}
	if params.SourceFileName == "" {
		return port.NewValidationError("source_file_name is required")
	}
	if params.SourceFileSize <= 0 {
		return port.NewValidationError("source_file_size must be positive")
	}
	if params.CreatedByUserID == "" {
		return port.NewValidationError("created_by_user_id is required")
	}
	// For non-RE_CHECK origins, source_file_key is required (for RE_CHECK it is copied from parent).
	if params.OriginType != model.OriginTypeReCheck && params.SourceFileKey == "" {
		return port.NewValidationError("source_file_key is required")
	}
	return nil
}

// isValidOriginType checks whether the given origin type is a recognized value.
func isValidOriginType(ot model.OriginType) bool {
	for _, v := range model.AllOriginTypes {
		if v == ot {
			return true
		}
	}
	return false
}

// generateUUID produces a UUID v4 using crypto/rand.
// Panics if crypto/rand fails (broken system CSPRNG — fatal condition).
func generateUUID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("version: crypto/rand failed: " + err.Error())
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
