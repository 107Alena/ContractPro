package lifecycle

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"

	"contractpro/document-management/internal/domain/model"
	"contractpro/document-management/internal/domain/port"
)

// maxPageSize is the upper bound for pagination to prevent full-table scans.
const maxPageSize = 100

// Logger is the minimal structured logging interface.
type Logger interface {
	Info(msg string, keysAndValues ...any)
	Warn(msg string, keysAndValues ...any)
	Error(msg string, keysAndValues ...any)
}

// DocumentLifecycleService manages the lifecycle of documents: creation,
// retrieval, listing, archiving, and deletion.
type DocumentLifecycleService struct {
	transactor port.Transactor
	docRepo    port.DocumentRepository
	auditRepo  port.AuditRepository
	logger     Logger
	newUUID    func() string
}

// Compile-time interface check.
var _ port.DocumentLifecycleHandler = (*DocumentLifecycleService)(nil)

// NewDocumentLifecycleService creates a new service with the given dependencies.
// Panics if any required dependency is nil (programmer error at startup wiring).
func NewDocumentLifecycleService(
	transactor port.Transactor,
	docRepo port.DocumentRepository,
	auditRepo port.AuditRepository,
	logger Logger,
) *DocumentLifecycleService {
	if transactor == nil {
		panic("lifecycle: transactor must not be nil")
	}
	if docRepo == nil {
		panic("lifecycle: docRepo must not be nil")
	}
	if auditRepo == nil {
		panic("lifecycle: auditRepo must not be nil")
	}
	if logger == nil {
		panic("lifecycle: logger must not be nil")
	}
	return &DocumentLifecycleService{
		transactor: transactor,
		docRepo:    docRepo,
		auditRepo:  auditRepo,
		logger:     logger,
		newUUID:    generateUUID,
	}
}

// ---------------------------------------------------------------------------
// Public handlers — implements port.DocumentLifecycleHandler.
// ---------------------------------------------------------------------------

// CreateDocument creates a new document in ACTIVE status within a single
// transaction that also records an audit entry.
func (s *DocumentLifecycleService) CreateDocument(ctx context.Context, params port.CreateDocumentParams) (*model.Document, error) {
	if params.OrganizationID == "" {
		return nil, port.NewValidationError("organization_id is required")
	}
	if params.Title == "" {
		return nil, port.NewValidationError("title is required")
	}
	if params.CreatedByUserID == "" {
		return nil, port.NewValidationError("created_by_user_id is required")
	}

	doc := model.NewDocument(s.newUUID(), params.OrganizationID, params.Title, params.CreatedByUserID)

	details, err := json.Marshal(map[string]string{"title": params.Title})
	if err != nil {
		s.logger.Warn("failed to marshal create audit details", "error", err)
	}
	audit := model.NewAuditRecord(s.newUUID(), params.OrganizationID,
		model.AuditActionDocumentCreated, model.ActorTypeUser, params.CreatedByUserID).
		WithDocument(doc.DocumentID).
		WithDetails(details)

	if err := s.transactor.WithTransaction(ctx, func(txCtx context.Context) error {
		if err := s.docRepo.Insert(txCtx, doc); err != nil {
			return err
		}
		return s.auditRepo.Insert(txCtx, audit)
	}); err != nil {
		s.logger.Error("create document failed",
			"organization_id", params.OrganizationID,
			"error", err,
		)
		return nil, err
	}

	s.logger.Info("document created",
		"document_id", doc.DocumentID,
		"organization_id", params.OrganizationID,
	)
	return doc, nil
}

// GetDocument returns a document by ID within the given organization.
// Returns ErrCodeDocumentNotFound if the document does not exist or belongs
// to a different organization (tenant isolation).
func (s *DocumentLifecycleService) GetDocument(ctx context.Context, organizationID, documentID string) (*model.Document, error) {
	if organizationID == "" {
		return nil, port.NewValidationError("organization_id is required")
	}
	if documentID == "" {
		return nil, port.NewValidationError("document_id is required")
	}
	return s.docRepo.FindByID(ctx, organizationID, documentID)
}

// ListDocuments returns a paginated list of documents for an organization,
// optionally filtered by status.
func (s *DocumentLifecycleService) ListDocuments(ctx context.Context, params port.ListDocumentsParams) (*port.PageResult[*model.Document], error) {
	if params.OrganizationID == "" {
		return nil, port.NewValidationError("organization_id is required")
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

	docs, totalCount, err := s.docRepo.List(ctx, params.OrganizationID, params.StatusFilter, params.Page, pageSize)
	if err != nil {
		return nil, err
	}

	// Normalize nil slice to empty for deterministic JSON serialization ([] not null).
	if docs == nil {
		docs = []*model.Document{}
	}

	return &port.PageResult[*model.Document]{
		Items:      docs,
		TotalCount: totalCount,
		Page:       params.Page,
		PageSize:   pageSize,
	}, nil
}

// ArchiveDocument transitions a document from ACTIVE to ARCHIVED within a
// single transaction that also records an audit entry.
// Returns ErrCodeDocumentNotFound or ErrCodeStatusTransition on failure.
func (s *DocumentLifecycleService) ArchiveDocument(ctx context.Context, organizationID, documentID string) error {
	return s.transitionDocument(ctx, organizationID, documentID,
		model.DocumentStatusArchived, model.AuditActionDocumentArchived, "archive")
}

// DeleteDocument transitions a document to DELETED (soft delete) within a
// single transaction that also records an audit entry.
// Returns ErrCodeDocumentNotFound or ErrCodeStatusTransition on failure.
func (s *DocumentLifecycleService) DeleteDocument(ctx context.Context, organizationID, documentID string) error {
	return s.transitionDocument(ctx, organizationID, documentID,
		model.DocumentStatusDeleted, model.AuditActionDocumentDeleted, "delete")
}

// ---------------------------------------------------------------------------
// Internal helpers.
// ---------------------------------------------------------------------------

// transitionDocument is the shared implementation for ArchiveDocument and
// DeleteDocument. It validates input, transitions the document status within
// a transaction, records an audit entry, and logs the outcome.
// The actor is recorded as ActorTypeSystem/"system" because the port interface
// does not carry user identity; the API handler layer (DM-TASK-022) will add
// user context when available.
func (s *DocumentLifecycleService) transitionDocument(
	ctx context.Context,
	organizationID, documentID string,
	targetStatus model.DocumentStatus,
	auditAction model.AuditAction,
	verb string,
) error {
	if organizationID == "" {
		return port.NewValidationError("organization_id is required")
	}
	if documentID == "" {
		return port.NewValidationError("document_id is required")
	}

	if err := s.transactor.WithTransaction(ctx, func(txCtx context.Context) error {
		doc, err := s.docRepo.FindByID(txCtx, organizationID, documentID)
		if err != nil {
			return err
		}

		oldStatus := doc.Status
		if !doc.TransitionTo(targetStatus) {
			return port.NewStatusTransitionError(string(oldStatus), string(targetStatus))
		}

		if err := s.docRepo.Update(txCtx, doc); err != nil {
			return err
		}

		details, err := json.Marshal(map[string]string{
			"from": string(oldStatus),
			"to":   string(targetStatus),
		})
		if err != nil {
			s.logger.Warn("failed to marshal transition audit details", "error", err)
		}
		audit := model.NewAuditRecord(s.newUUID(), organizationID,
			auditAction, model.ActorTypeSystem, "system").
			WithDocument(documentID).
			WithDetails(details)

		return s.auditRepo.Insert(txCtx, audit)
	}); err != nil {
		s.logger.Error(verb+" document failed",
			"document_id", documentID,
			"organization_id", organizationID,
			"error", err,
		)
		return err
	}

	s.logger.Info("document "+verb+"d",
		"document_id", documentID,
		"organization_id", organizationID,
	)
	return nil
}

// generateUUID produces a UUID v4 using crypto/rand.
// Panics if crypto/rand fails (broken system CSPRNG — fatal condition).
func generateUUID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("lifecycle: crypto/rand failed: " + err.Error())
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
