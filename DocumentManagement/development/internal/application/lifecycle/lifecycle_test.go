package lifecycle

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"contractpro/document-management/internal/domain/model"
	"contractpro/document-management/internal/domain/port"
)

// ---------------------------------------------------------------------------
// Mock implementations.
// ---------------------------------------------------------------------------

type mockTransactor struct {
	fn func(ctx context.Context, fn func(ctx context.Context) error) error
}

func (m *mockTransactor) WithTransaction(ctx context.Context, fn func(ctx context.Context) error) error {
	if m.fn != nil {
		return m.fn(ctx, fn)
	}
	return fn(ctx)
}

type mockDocumentRepo struct {
	insertFn  func(ctx context.Context, doc *model.Document) error
	findByIDFn func(ctx context.Context, orgID, docID string) (*model.Document, error)
	listFn    func(ctx context.Context, orgID string, statusFilter *model.DocumentStatus, page, pageSize int) ([]*model.Document, int, error)
	updateFn  func(ctx context.Context, doc *model.Document) error

	insertedDocs []*model.Document
	updatedDocs  []*model.Document
}

func (m *mockDocumentRepo) Insert(ctx context.Context, doc *model.Document) error {
	m.insertedDocs = append(m.insertedDocs, doc)
	if m.insertFn != nil {
		return m.insertFn(ctx, doc)
	}
	return nil
}

func (m *mockDocumentRepo) FindByID(ctx context.Context, orgID, docID string) (*model.Document, error) {
	if m.findByIDFn != nil {
		return m.findByIDFn(ctx, orgID, docID)
	}
	return nil, port.NewDocumentNotFoundError(orgID, docID)
}

func (m *mockDocumentRepo) List(ctx context.Context, orgID string, statusFilter *model.DocumentStatus, page, pageSize int) ([]*model.Document, int, error) {
	if m.listFn != nil {
		return m.listFn(ctx, orgID, statusFilter, page, pageSize)
	}
	return nil, 0, nil
}

func (m *mockDocumentRepo) Update(ctx context.Context, doc *model.Document) error {
	m.updatedDocs = append(m.updatedDocs, doc)
	if m.updateFn != nil {
		return m.updateFn(ctx, doc)
	}
	return nil
}

func (m *mockDocumentRepo) FindByIDForUpdate(ctx context.Context, orgID, docID string) (*model.Document, error) {
	return m.FindByID(ctx, orgID, docID) // lifecycle does not use FOR UPDATE
}

func (m *mockDocumentRepo) ExistsByID(context.Context, string, string) (bool, error) {
	panic("not used in lifecycle")
}

type mockAuditRepo struct {
	inserted  []*model.AuditRecord
	insertErr error
}

func (m *mockAuditRepo) Insert(_ context.Context, r *model.AuditRecord) error {
	if m.insertErr != nil {
		return m.insertErr
	}
	m.inserted = append(m.inserted, r)
	return nil
}

func (m *mockAuditRepo) List(context.Context, port.AuditListParams) ([]*model.AuditRecord, int, error) {
	panic("not used in lifecycle")
}

type mockLogger struct {
	infos  []string
	warns  []string
	errors []string
}

func (m *mockLogger) Info(msg string, _ ...any)  { m.infos = append(m.infos, msg) }
func (m *mockLogger) Warn(msg string, _ ...any)  { m.warns = append(m.warns, msg) }
func (m *mockLogger) Error(msg string, _ ...any) { m.errors = append(m.errors, msg) }

// ---------------------------------------------------------------------------
// Test helpers.
// ---------------------------------------------------------------------------

type testDeps struct {
	transactor *mockTransactor
	docRepo    *mockDocumentRepo
	auditRepo  *mockAuditRepo
	logger     *mockLogger
}

func newTestDeps() *testDeps {
	return &testDeps{
		transactor: &mockTransactor{},
		docRepo:    &mockDocumentRepo{},
		auditRepo:  &mockAuditRepo{},
		logger:     &mockLogger{},
	}
}

func (d *testDeps) newService() *DocumentLifecycleService {
	svc := NewDocumentLifecycleService(d.transactor, d.docRepo, d.auditRepo, d.logger)
	counter := 0
	svc.newUUID = func() string {
		counter++
		return fmt.Sprintf("test-uuid-%03d", counter)
	}
	return svc
}

func activeDoc(orgID, docID string) *model.Document {
	return model.NewDocument(docID, orgID, "Test Document", "user-1")
}

func archivedDoc(orgID, docID string) *model.Document {
	doc := model.NewDocument(docID, orgID, "Test Document", "user-1")
	doc.TransitionTo(model.DocumentStatusArchived)
	return doc
}

func deletedDoc(orgID, docID string) *model.Document {
	doc := model.NewDocument(docID, orgID, "Test Document", "user-1")
	doc.TransitionTo(model.DocumentStatusDeleted)
	return doc
}

// ---------------------------------------------------------------------------
// Constructor tests.
// ---------------------------------------------------------------------------

func TestNewDocumentLifecycleService_PanicsOnNilDeps(t *testing.T) {
	d := newTestDeps()

	tests := []struct {
		name       string
		transactor port.Transactor
		docRepo    port.DocumentRepository
		auditRepo  port.AuditRepository
		logger     Logger
		panicMsg   string
	}{
		{"nil transactor", nil, d.docRepo, d.auditRepo, d.logger, "transactor must not be nil"},
		{"nil docRepo", d.transactor, nil, d.auditRepo, d.logger, "docRepo must not be nil"},
		{"nil auditRepo", d.transactor, d.docRepo, nil, d.logger, "auditRepo must not be nil"},
		{"nil logger", d.transactor, d.docRepo, d.auditRepo, nil, "logger must not be nil"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				r := recover()
				if r == nil {
					t.Fatal("expected panic")
				}
				msg := fmt.Sprint(r)
				if !strings.Contains(msg, tt.panicMsg) {
					t.Fatalf("panic message %q does not contain %q", msg, tt.panicMsg)
				}
			}()
			NewDocumentLifecycleService(tt.transactor, tt.docRepo, tt.auditRepo, tt.logger)
		})
	}
}

func TestNewDocumentLifecycleService_Success(t *testing.T) {
	d := newTestDeps()
	svc := NewDocumentLifecycleService(d.transactor, d.docRepo, d.auditRepo, d.logger)
	if svc == nil {
		t.Fatal("expected non-nil service")
	}
}

// ---------------------------------------------------------------------------
// CreateDocument tests.
// ---------------------------------------------------------------------------

func TestCreateDocument_HappyPath(t *testing.T) {
	d := newTestDeps()
	svc := d.newService()

	doc, err := svc.CreateDocument(context.Background(), port.CreateDocumentParams{
		OrganizationID:  "org-1",
		Title:           "Contract A",
		CreatedByUserID: "user-1",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Document assertions.
	if doc.DocumentID != "test-uuid-001" {
		t.Errorf("document_id = %q, want test-uuid-001", doc.DocumentID)
	}
	if doc.OrganizationID != "org-1" {
		t.Errorf("organization_id = %q, want org-1", doc.OrganizationID)
	}
	if doc.Title != "Contract A" {
		t.Errorf("title = %q, want Contract A", doc.Title)
	}
	if doc.Status != model.DocumentStatusActive {
		t.Errorf("status = %q, want ACTIVE", doc.Status)
	}
	if doc.CreatedByUserID != "user-1" {
		t.Errorf("created_by_user_id = %q, want user-1", doc.CreatedByUserID)
	}

	// Repository assertions.
	if len(d.docRepo.insertedDocs) != 1 {
		t.Fatalf("expected 1 inserted doc, got %d", len(d.docRepo.insertedDocs))
	}

	// Audit assertions.
	if len(d.auditRepo.inserted) != 1 {
		t.Fatalf("expected 1 audit record, got %d", len(d.auditRepo.inserted))
	}
	audit := d.auditRepo.inserted[0]
	if audit.AuditID != "test-uuid-002" {
		t.Errorf("audit_id = %q, want test-uuid-002", audit.AuditID)
	}
	if audit.Action != model.AuditActionDocumentCreated {
		t.Errorf("action = %q, want DOCUMENT_CREATED", audit.Action)
	}
	if audit.ActorType != model.ActorTypeUser {
		t.Errorf("actor_type = %q, want USER", audit.ActorType)
	}
	if audit.ActorID != "user-1" {
		t.Errorf("actor_id = %q, want user-1", audit.ActorID)
	}
	if audit.DocumentID != "test-uuid-001" {
		t.Errorf("audit.document_id = %q, want test-uuid-001", audit.DocumentID)
	}

	// Audit details.
	var details map[string]string
	if err := json.Unmarshal(audit.Details, &details); err != nil {
		t.Fatalf("failed to unmarshal audit details: %v", err)
	}
	if details["title"] != "Contract A" {
		t.Errorf("audit details title = %q, want Contract A", details["title"])
	}

	// Logger.
	if len(d.logger.infos) != 1 {
		t.Errorf("expected 1 info log, got %d", len(d.logger.infos))
	}
}

func TestCreateDocument_EmptyOrganizationID(t *testing.T) {
	d := newTestDeps()
	svc := d.newService()

	_, err := svc.CreateDocument(context.Background(), port.CreateDocumentParams{
		Title:           "Contract A",
		CreatedByUserID: "user-1",
	})
	if port.ErrorCode(err) != port.ErrCodeValidation {
		t.Fatalf("expected VALIDATION_ERROR, got %v", err)
	}
}

func TestCreateDocument_EmptyTitle(t *testing.T) {
	d := newTestDeps()
	svc := d.newService()

	_, err := svc.CreateDocument(context.Background(), port.CreateDocumentParams{
		OrganizationID:  "org-1",
		CreatedByUserID: "user-1",
	})
	if port.ErrorCode(err) != port.ErrCodeValidation {
		t.Fatalf("expected VALIDATION_ERROR, got %v", err)
	}
}

func TestCreateDocument_EmptyCreatedByUserID(t *testing.T) {
	d := newTestDeps()
	svc := d.newService()

	_, err := svc.CreateDocument(context.Background(), port.CreateDocumentParams{
		OrganizationID: "org-1",
		Title:          "Contract A",
	})
	if port.ErrorCode(err) != port.ErrCodeValidation {
		t.Fatalf("expected VALIDATION_ERROR, got %v", err)
	}
}

func TestCreateDocument_DocRepoInsertFails(t *testing.T) {
	d := newTestDeps()
	d.docRepo.insertFn = func(context.Context, *model.Document) error {
		return port.NewDatabaseError("insert failed", errors.New("connection refused"))
	}
	svc := d.newService()

	_, err := svc.CreateDocument(context.Background(), port.CreateDocumentParams{
		OrganizationID:  "org-1",
		Title:           "Contract A",
		CreatedByUserID: "user-1",
	})
	if port.ErrorCode(err) != port.ErrCodeDatabaseFailed {
		t.Fatalf("expected DATABASE_FAILED, got %v", err)
	}
	if len(d.auditRepo.inserted) != 0 {
		t.Error("audit should not be inserted when doc insert fails")
	}
}

func TestCreateDocument_AuditInsertFails(t *testing.T) {
	d := newTestDeps()
	d.auditRepo.insertErr = port.NewDatabaseError("audit failed", errors.New("disk full"))
	svc := d.newService()

	_, err := svc.CreateDocument(context.Background(), port.CreateDocumentParams{
		OrganizationID:  "org-1",
		Title:           "Contract A",
		CreatedByUserID: "user-1",
	})
	if port.ErrorCode(err) != port.ErrCodeDatabaseFailed {
		t.Fatalf("expected DATABASE_FAILED, got %v", err)
	}
}

func TestCreateDocument_DocumentAlreadyExists(t *testing.T) {
	d := newTestDeps()
	d.docRepo.insertFn = func(context.Context, *model.Document) error {
		return port.NewDocumentAlreadyExistsError("doc-1")
	}
	svc := d.newService()

	_, err := svc.CreateDocument(context.Background(), port.CreateDocumentParams{
		OrganizationID:  "org-1",
		Title:           "Contract A",
		CreatedByUserID: "user-1",
	})
	if port.ErrorCode(err) != port.ErrCodeDocumentAlreadyExists {
		t.Fatalf("expected DOCUMENT_ALREADY_EXISTS, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// GetDocument tests.
// ---------------------------------------------------------------------------

func TestGetDocument_HappyPath(t *testing.T) {
	d := newTestDeps()
	expected := activeDoc("org-1", "doc-1")
	d.docRepo.findByIDFn = func(_ context.Context, orgID, docID string) (*model.Document, error) {
		if orgID == "org-1" && docID == "doc-1" {
			return expected, nil
		}
		return nil, port.NewDocumentNotFoundError(orgID, docID)
	}
	svc := d.newService()

	doc, err := svc.GetDocument(context.Background(), "org-1", "doc-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if doc.DocumentID != "doc-1" {
		t.Errorf("document_id = %q, want doc-1", doc.DocumentID)
	}
}

func TestGetDocument_EmptyOrganizationID(t *testing.T) {
	d := newTestDeps()
	svc := d.newService()

	_, err := svc.GetDocument(context.Background(), "", "doc-1")
	if port.ErrorCode(err) != port.ErrCodeValidation {
		t.Fatalf("expected VALIDATION_ERROR, got %v", err)
	}
}

func TestGetDocument_EmptyDocumentID(t *testing.T) {
	d := newTestDeps()
	svc := d.newService()

	_, err := svc.GetDocument(context.Background(), "org-1", "")
	if port.ErrorCode(err) != port.ErrCodeValidation {
		t.Fatalf("expected VALIDATION_ERROR, got %v", err)
	}
}

func TestGetDocument_NotFound(t *testing.T) {
	d := newTestDeps()
	svc := d.newService()

	_, err := svc.GetDocument(context.Background(), "org-1", "doc-1")
	if port.ErrorCode(err) != port.ErrCodeDocumentNotFound {
		t.Fatalf("expected DOCUMENT_NOT_FOUND, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// ListDocuments tests.
// ---------------------------------------------------------------------------

func TestListDocuments_HappyPath(t *testing.T) {
	d := newTestDeps()
	docs := []*model.Document{activeDoc("org-1", "doc-1"), activeDoc("org-1", "doc-2")}
	d.docRepo.listFn = func(_ context.Context, orgID string, _ *model.DocumentStatus, page, pageSize int) ([]*model.Document, int, error) {
		return docs, 2, nil
	}
	svc := d.newService()

	result, err := svc.ListDocuments(context.Background(), port.ListDocumentsParams{
		OrganizationID: "org-1",
		Page:           1,
		PageSize:       10,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Items) != 2 {
		t.Errorf("items = %d, want 2", len(result.Items))
	}
	if result.TotalCount != 2 {
		t.Errorf("total_count = %d, want 2", result.TotalCount)
	}
	if result.Page != 1 {
		t.Errorf("page = %d, want 1", result.Page)
	}
	if result.PageSize != 10 {
		t.Errorf("page_size = %d, want 10", result.PageSize)
	}
}

func TestListDocuments_WithStatusFilter(t *testing.T) {
	d := newTestDeps()
	var capturedFilter *model.DocumentStatus
	d.docRepo.listFn = func(_ context.Context, _ string, statusFilter *model.DocumentStatus, _, _ int) ([]*model.Document, int, error) {
		capturedFilter = statusFilter
		return []*model.Document{}, 0, nil
	}
	svc := d.newService()

	filter := model.DocumentStatusArchived
	_, err := svc.ListDocuments(context.Background(), port.ListDocumentsParams{
		OrganizationID: "org-1",
		StatusFilter:   &filter,
		Page:           1,
		PageSize:       10,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedFilter == nil || *capturedFilter != model.DocumentStatusArchived {
		t.Errorf("status filter not passed to repo correctly")
	}
}

func TestListDocuments_EmptyOrganizationID(t *testing.T) {
	d := newTestDeps()
	svc := d.newService()

	_, err := svc.ListDocuments(context.Background(), port.ListDocumentsParams{
		Page:     1,
		PageSize: 10,
	})
	if port.ErrorCode(err) != port.ErrCodeValidation {
		t.Fatalf("expected VALIDATION_ERROR, got %v", err)
	}
}

func TestListDocuments_InvalidPage(t *testing.T) {
	d := newTestDeps()
	svc := d.newService()

	_, err := svc.ListDocuments(context.Background(), port.ListDocumentsParams{
		OrganizationID: "org-1",
		Page:           0,
		PageSize:       10,
	})
	if port.ErrorCode(err) != port.ErrCodeValidation {
		t.Fatalf("expected VALIDATION_ERROR, got %v", err)
	}
}

func TestListDocuments_InvalidPageSize(t *testing.T) {
	d := newTestDeps()
	svc := d.newService()

	_, err := svc.ListDocuments(context.Background(), port.ListDocumentsParams{
		OrganizationID: "org-1",
		Page:           1,
		PageSize:       0,
	})
	if port.ErrorCode(err) != port.ErrCodeValidation {
		t.Fatalf("expected VALIDATION_ERROR, got %v", err)
	}
}

func TestListDocuments_NilItemsNormalized(t *testing.T) {
	d := newTestDeps()
	d.docRepo.listFn = func(context.Context, string, *model.DocumentStatus, int, int) ([]*model.Document, int, error) {
		return nil, 0, nil
	}
	svc := d.newService()

	result, err := svc.ListDocuments(context.Background(), port.ListDocumentsParams{
		OrganizationID: "org-1",
		Page:           1,
		PageSize:       10,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Items == nil {
		t.Error("items should not be nil, expected empty slice")
	}
	if len(result.Items) != 0 {
		t.Errorf("items = %d, want 0", len(result.Items))
	}
}

func TestListDocuments_PageSizeClamped(t *testing.T) {
	d := newTestDeps()
	var capturedPageSize int
	d.docRepo.listFn = func(_ context.Context, _ string, _ *model.DocumentStatus, _, pageSize int) ([]*model.Document, int, error) {
		capturedPageSize = pageSize
		return []*model.Document{}, 0, nil
	}
	svc := d.newService()

	result, err := svc.ListDocuments(context.Background(), port.ListDocumentsParams{
		OrganizationID: "org-1",
		Page:           1,
		PageSize:       500,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedPageSize != maxPageSize {
		t.Errorf("repo received pageSize = %d, want %d", capturedPageSize, maxPageSize)
	}
	if result.PageSize != maxPageSize {
		t.Errorf("result.PageSize = %d, want %d", result.PageSize, maxPageSize)
	}
}

func TestListDocuments_RepoError(t *testing.T) {
	d := newTestDeps()
	d.docRepo.listFn = func(context.Context, string, *model.DocumentStatus, int, int) ([]*model.Document, int, error) {
		return nil, 0, port.NewDatabaseError("list failed", errors.New("timeout"))
	}
	svc := d.newService()

	_, err := svc.ListDocuments(context.Background(), port.ListDocumentsParams{
		OrganizationID: "org-1",
		Page:           1,
		PageSize:       10,
	})
	if port.ErrorCode(err) != port.ErrCodeDatabaseFailed {
		t.Fatalf("expected DATABASE_FAILED, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// ArchiveDocument tests.
// ---------------------------------------------------------------------------

func TestArchiveDocument_HappyPath_FromActive(t *testing.T) {
	d := newTestDeps()
	d.docRepo.findByIDFn = func(_ context.Context, orgID, docID string) (*model.Document, error) {
		return activeDoc(orgID, docID), nil
	}
	svc := d.newService()

	err := svc.ArchiveDocument(context.Background(), "org-1", "doc-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Document was updated.
	if len(d.docRepo.updatedDocs) != 1 {
		t.Fatalf("expected 1 updated doc, got %d", len(d.docRepo.updatedDocs))
	}
	updated := d.docRepo.updatedDocs[0]
	if updated.Status != model.DocumentStatusArchived {
		t.Errorf("status = %q, want ARCHIVED", updated.Status)
	}

	// Audit record.
	if len(d.auditRepo.inserted) != 1 {
		t.Fatalf("expected 1 audit record, got %d", len(d.auditRepo.inserted))
	}
	audit := d.auditRepo.inserted[0]
	if audit.Action != model.AuditActionDocumentArchived {
		t.Errorf("action = %q, want DOCUMENT_ARCHIVED", audit.Action)
	}
	if audit.ActorType != model.ActorTypeSystem {
		t.Errorf("actor_type = %q, want SYSTEM", audit.ActorType)
	}
	if audit.ActorID != "system" {
		t.Errorf("actor_id = %q, want system", audit.ActorID)
	}
	if audit.DocumentID != "doc-1" {
		t.Errorf("audit.document_id = %q, want doc-1", audit.DocumentID)
	}

	// Audit details.
	var details map[string]string
	if err := json.Unmarshal(audit.Details, &details); err != nil {
		t.Fatalf("failed to unmarshal audit details: %v", err)
	}
	if details["from"] != "ACTIVE" || details["to"] != "ARCHIVED" {
		t.Errorf("audit details = %v, want from=ACTIVE to=ARCHIVED", details)
	}
}

func TestArchiveDocument_EmptyOrganizationID(t *testing.T) {
	d := newTestDeps()
	svc := d.newService()

	err := svc.ArchiveDocument(context.Background(), "", "doc-1")
	if port.ErrorCode(err) != port.ErrCodeValidation {
		t.Fatalf("expected VALIDATION_ERROR, got %v", err)
	}
}

func TestArchiveDocument_EmptyDocumentID(t *testing.T) {
	d := newTestDeps()
	svc := d.newService()

	err := svc.ArchiveDocument(context.Background(), "org-1", "")
	if port.ErrorCode(err) != port.ErrCodeValidation {
		t.Fatalf("expected VALIDATION_ERROR, got %v", err)
	}
}

func TestArchiveDocument_NotFound(t *testing.T) {
	d := newTestDeps()
	svc := d.newService()

	err := svc.ArchiveDocument(context.Background(), "org-1", "doc-1")
	if port.ErrorCode(err) != port.ErrCodeDocumentNotFound {
		t.Fatalf("expected DOCUMENT_NOT_FOUND, got %v", err)
	}
}

func TestArchiveDocument_InvalidTransition_FromArchived(t *testing.T) {
	d := newTestDeps()
	d.docRepo.findByIDFn = func(_ context.Context, orgID, docID string) (*model.Document, error) {
		return archivedDoc(orgID, docID), nil
	}
	svc := d.newService()

	err := svc.ArchiveDocument(context.Background(), "org-1", "doc-1")
	if port.ErrorCode(err) != port.ErrCodeStatusTransition {
		t.Fatalf("expected INVALID_STATUS_TRANSITION, got %v", err)
	}
	if !strings.Contains(err.Error(), "ARCHIVED") {
		t.Errorf("error message should mention ARCHIVED: %v", err)
	}
}

func TestArchiveDocument_InvalidTransition_FromDeleted(t *testing.T) {
	d := newTestDeps()
	d.docRepo.findByIDFn = func(_ context.Context, orgID, docID string) (*model.Document, error) {
		return deletedDoc(orgID, docID), nil
	}
	svc := d.newService()

	err := svc.ArchiveDocument(context.Background(), "org-1", "doc-1")
	if port.ErrorCode(err) != port.ErrCodeStatusTransition {
		t.Fatalf("expected INVALID_STATUS_TRANSITION, got %v", err)
	}
}

func TestArchiveDocument_UpdateFails(t *testing.T) {
	d := newTestDeps()
	d.docRepo.findByIDFn = func(_ context.Context, orgID, docID string) (*model.Document, error) {
		return activeDoc(orgID, docID), nil
	}
	d.docRepo.updateFn = func(context.Context, *model.Document) error {
		return port.NewDatabaseError("update failed", errors.New("connection refused"))
	}
	svc := d.newService()

	err := svc.ArchiveDocument(context.Background(), "org-1", "doc-1")
	if port.ErrorCode(err) != port.ErrCodeDatabaseFailed {
		t.Fatalf("expected DATABASE_FAILED, got %v", err)
	}
	if len(d.auditRepo.inserted) != 0 {
		t.Error("audit should not be inserted when update fails (tx rolled back)")
	}
}

func TestArchiveDocument_AuditFails(t *testing.T) {
	d := newTestDeps()
	d.docRepo.findByIDFn = func(_ context.Context, orgID, docID string) (*model.Document, error) {
		return activeDoc(orgID, docID), nil
	}
	d.auditRepo.insertErr = port.NewDatabaseError("audit failed", errors.New("disk full"))
	svc := d.newService()

	err := svc.ArchiveDocument(context.Background(), "org-1", "doc-1")
	if port.ErrorCode(err) != port.ErrCodeDatabaseFailed {
		t.Fatalf("expected DATABASE_FAILED, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// DeleteDocument tests.
// ---------------------------------------------------------------------------

func TestDeleteDocument_HappyPath_FromActive(t *testing.T) {
	d := newTestDeps()
	d.docRepo.findByIDFn = func(_ context.Context, orgID, docID string) (*model.Document, error) {
		return activeDoc(orgID, docID), nil
	}
	svc := d.newService()

	err := svc.DeleteDocument(context.Background(), "org-1", "doc-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Document was updated.
	if len(d.docRepo.updatedDocs) != 1 {
		t.Fatalf("expected 1 updated doc, got %d", len(d.docRepo.updatedDocs))
	}
	updated := d.docRepo.updatedDocs[0]
	if updated.Status != model.DocumentStatusDeleted {
		t.Errorf("status = %q, want DELETED", updated.Status)
	}
	if updated.DeletedAt == nil {
		t.Error("deleted_at should be set")
	}

	// Audit record.
	if len(d.auditRepo.inserted) != 1 {
		t.Fatalf("expected 1 audit record, got %d", len(d.auditRepo.inserted))
	}
	audit := d.auditRepo.inserted[0]
	if audit.Action != model.AuditActionDocumentDeleted {
		t.Errorf("action = %q, want DOCUMENT_DELETED", audit.Action)
	}
	if audit.ActorType != model.ActorTypeSystem {
		t.Errorf("actor_type = %q, want SYSTEM", audit.ActorType)
	}

	// Audit details.
	var details map[string]string
	if err := json.Unmarshal(audit.Details, &details); err != nil {
		t.Fatalf("failed to unmarshal audit details: %v", err)
	}
	if details["from"] != "ACTIVE" || details["to"] != "DELETED" {
		t.Errorf("audit details = %v, want from=ACTIVE to=DELETED", details)
	}
}

func TestDeleteDocument_HappyPath_FromArchived(t *testing.T) {
	d := newTestDeps()
	d.docRepo.findByIDFn = func(_ context.Context, orgID, docID string) (*model.Document, error) {
		return archivedDoc(orgID, docID), nil
	}
	svc := d.newService()

	err := svc.DeleteDocument(context.Background(), "org-1", "doc-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	updated := d.docRepo.updatedDocs[0]
	if updated.Status != model.DocumentStatusDeleted {
		t.Errorf("status = %q, want DELETED", updated.Status)
	}

	// Audit details.
	var details map[string]string
	if err := json.Unmarshal(d.auditRepo.inserted[0].Details, &details); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if details["from"] != "ARCHIVED" {
		t.Errorf("from = %q, want ARCHIVED", details["from"])
	}
}

func TestDeleteDocument_EmptyOrganizationID(t *testing.T) {
	d := newTestDeps()
	svc := d.newService()

	err := svc.DeleteDocument(context.Background(), "", "doc-1")
	if port.ErrorCode(err) != port.ErrCodeValidation {
		t.Fatalf("expected VALIDATION_ERROR, got %v", err)
	}
}

func TestDeleteDocument_EmptyDocumentID(t *testing.T) {
	d := newTestDeps()
	svc := d.newService()

	err := svc.DeleteDocument(context.Background(), "org-1", "")
	if port.ErrorCode(err) != port.ErrCodeValidation {
		t.Fatalf("expected VALIDATION_ERROR, got %v", err)
	}
}

func TestDeleteDocument_NotFound(t *testing.T) {
	d := newTestDeps()
	svc := d.newService()

	err := svc.DeleteDocument(context.Background(), "org-1", "doc-1")
	if port.ErrorCode(err) != port.ErrCodeDocumentNotFound {
		t.Fatalf("expected DOCUMENT_NOT_FOUND, got %v", err)
	}
}

func TestDeleteDocument_InvalidTransition_FromDeleted(t *testing.T) {
	d := newTestDeps()
	d.docRepo.findByIDFn = func(_ context.Context, orgID, docID string) (*model.Document, error) {
		return deletedDoc(orgID, docID), nil
	}
	svc := d.newService()

	err := svc.DeleteDocument(context.Background(), "org-1", "doc-1")
	if port.ErrorCode(err) != port.ErrCodeStatusTransition {
		t.Fatalf("expected INVALID_STATUS_TRANSITION, got %v", err)
	}
}

func TestDeleteDocument_UpdateFails(t *testing.T) {
	d := newTestDeps()
	d.docRepo.findByIDFn = func(_ context.Context, orgID, docID string) (*model.Document, error) {
		return activeDoc(orgID, docID), nil
	}
	d.docRepo.updateFn = func(context.Context, *model.Document) error {
		return port.NewDatabaseError("update failed", errors.New("connection refused"))
	}
	svc := d.newService()

	err := svc.DeleteDocument(context.Background(), "org-1", "doc-1")
	if port.ErrorCode(err) != port.ErrCodeDatabaseFailed {
		t.Fatalf("expected DATABASE_FAILED, got %v", err)
	}
}

func TestDeleteDocument_AuditFails(t *testing.T) {
	d := newTestDeps()
	d.docRepo.findByIDFn = func(_ context.Context, orgID, docID string) (*model.Document, error) {
		return activeDoc(orgID, docID), nil
	}
	d.auditRepo.insertErr = port.NewDatabaseError("audit failed", errors.New("disk full"))
	svc := d.newService()

	err := svc.DeleteDocument(context.Background(), "org-1", "doc-1")
	if port.ErrorCode(err) != port.ErrCodeDatabaseFailed {
		t.Fatalf("expected DATABASE_FAILED, got %v", err)
	}
}
