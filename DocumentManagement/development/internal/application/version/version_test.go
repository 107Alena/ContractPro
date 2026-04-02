package version

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

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
	findByIDFn func(ctx context.Context, orgID, docID string) (*model.Document, error)
	updateFn   func(ctx context.Context, doc *model.Document) error

	updatedDocs []*model.Document
}

func (m *mockDocumentRepo) Insert(context.Context, *model.Document) error {
	panic("not used in version")
}
func (m *mockDocumentRepo) FindByID(ctx context.Context, orgID, docID string) (*model.Document, error) {
	if m.findByIDFn != nil {
		return m.findByIDFn(ctx, orgID, docID)
	}
	return nil, port.NewDocumentNotFoundError(orgID, docID)
}
func (m *mockDocumentRepo) List(context.Context, string, *model.DocumentStatus, int, int) ([]*model.Document, int, error) {
	panic("not used in version")
}
func (m *mockDocumentRepo) Update(ctx context.Context, doc *model.Document) error {
	m.updatedDocs = append(m.updatedDocs, doc)
	if m.updateFn != nil {
		return m.updateFn(ctx, doc)
	}
	return nil
}
func (m *mockDocumentRepo) ExistsByID(context.Context, string, string) (bool, error) {
	panic("not used in version")
}

type mockVersionRepo struct {
	insertFn            func(ctx context.Context, version *model.DocumentVersion) error
	findByIDFn          func(ctx context.Context, orgID, docID, versionID string) (*model.DocumentVersion, error)
	listFn              func(ctx context.Context, orgID, docID string, page, pageSize int) ([]*model.DocumentVersion, int, error)
	nextVersionNumberFn func(ctx context.Context, orgID, docID string) (int, error)

	insertedVersions []*model.DocumentVersion
}

func (m *mockVersionRepo) Insert(ctx context.Context, v *model.DocumentVersion) error {
	m.insertedVersions = append(m.insertedVersions, v)
	if m.insertFn != nil {
		return m.insertFn(ctx, v)
	}
	return nil
}
func (m *mockVersionRepo) FindByID(ctx context.Context, orgID, docID, versionID string) (*model.DocumentVersion, error) {
	if m.findByIDFn != nil {
		return m.findByIDFn(ctx, orgID, docID, versionID)
	}
	return nil, port.NewVersionNotFoundError(versionID)
}
func (m *mockVersionRepo) FindByIDForUpdate(ctx context.Context, orgID, docID, versionID string) (*model.DocumentVersion, error) {
	return m.FindByID(ctx, orgID, docID, versionID) // version management does not use FOR UPDATE
}
func (m *mockVersionRepo) List(ctx context.Context, orgID, docID string, page, pageSize int) ([]*model.DocumentVersion, int, error) {
	if m.listFn != nil {
		return m.listFn(ctx, orgID, docID, page, pageSize)
	}
	return nil, 0, nil
}
func (m *mockVersionRepo) Update(context.Context, *model.DocumentVersion) error {
	panic("not used in version management")
}
func (m *mockVersionRepo) NextVersionNumber(ctx context.Context, orgID, docID string) (int, error) {
	if m.nextVersionNumberFn != nil {
		return m.nextVersionNumberFn(ctx, orgID, docID)
	}
	return 1, nil
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
	panic("not used in version")
}

type mockOutboxWriter struct {
	written []outboxCall
	err     error
}

type outboxCall struct {
	AggregateID string
	Topic       string
	Event       any
}

func (m *mockOutboxWriter) Write(_ context.Context, aggregateID, topic string, event any) error {
	m.written = append(m.written, outboxCall{AggregateID: aggregateID, Topic: topic, Event: event})
	if m.err != nil {
		return m.err
	}
	return nil
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
	transactor  *mockTransactor
	docRepo     *mockDocumentRepo
	versionRepo *mockVersionRepo
	auditRepo   *mockAuditRepo
	outbox      *mockOutboxWriter
	logger      *mockLogger
}

func newTestDeps() *testDeps {
	return &testDeps{
		transactor:  &mockTransactor{},
		docRepo:     &mockDocumentRepo{},
		versionRepo: &mockVersionRepo{},
		auditRepo:   &mockAuditRepo{},
		outbox:      &mockOutboxWriter{},
		logger:      &mockLogger{},
	}
}

func (d *testDeps) withActiveDoc(orgID, docID string) {
	d.docRepo.findByIDFn = func(_ context.Context, org, doc string) (*model.Document, error) {
		if org == orgID && doc == docID {
			return model.NewDocument(docID, orgID, "Test Document", "user-1"), nil
		}
		return nil, port.NewDocumentNotFoundError(org, doc)
	}
}

func (d *testDeps) newService() *VersionManagementService {
	svc := NewVersionManagementService(
		d.transactor, d.docRepo, d.versionRepo, d.auditRepo, d.outbox, d.logger,
	)
	counter := 0
	svc.newUUID = func() string {
		counter++
		return fmt.Sprintf("test-uuid-%03d", counter)
	}
	fixedTime := time.Date(2026, 4, 2, 12, 0, 0, 0, time.UTC)
	svc.nowFunc = func() time.Time { return fixedTime }
	return svc
}

func defaultCreateParams() port.CreateVersionParams {
	return port.CreateVersionParams{
		OrganizationID:     "org-1",
		DocumentID:         "doc-1",
		OriginType:         model.OriginTypeUpload,
		SourceFileKey:      "org-1/doc-1/uploads/file.pdf",
		SourceFileName:     "contract.pdf",
		SourceFileSize:     1024,
		SourceFileChecksum: "abc123",
		CreatedByUserID:    "user-1",
	}
}

// ---------------------------------------------------------------------------
// Constructor tests.
// ---------------------------------------------------------------------------

func TestNewVersionManagementService_PanicsOnNilDeps(t *testing.T) {
	d := newTestDeps()

	tests := []struct {
		name        string
		transactor  port.Transactor
		docRepo     port.DocumentRepository
		versionRepo port.VersionRepository
		auditRepo   port.AuditRepository
		outbox      OutboxWriter
		logger      Logger
		panicMsg    string
	}{
		{"nil transactor", nil, d.docRepo, d.versionRepo, d.auditRepo, d.outbox, d.logger, "transactor must not be nil"},
		{"nil docRepo", d.transactor, nil, d.versionRepo, d.auditRepo, d.outbox, d.logger, "docRepo must not be nil"},
		{"nil versionRepo", d.transactor, d.docRepo, nil, d.auditRepo, d.outbox, d.logger, "versionRepo must not be nil"},
		{"nil auditRepo", d.transactor, d.docRepo, d.versionRepo, nil, d.outbox, d.logger, "auditRepo must not be nil"},
		{"nil outbox", d.transactor, d.docRepo, d.versionRepo, d.auditRepo, nil, d.logger, "outbox must not be nil"},
		{"nil logger", d.transactor, d.docRepo, d.versionRepo, d.auditRepo, d.outbox, nil, "logger must not be nil"},
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
			NewVersionManagementService(tt.transactor, tt.docRepo, tt.versionRepo, tt.auditRepo, tt.outbox, tt.logger)
		})
	}
}

func TestNewVersionManagementService_Success(t *testing.T) {
	d := newTestDeps()
	svc := NewVersionManagementService(
		d.transactor, d.docRepo, d.versionRepo, d.auditRepo, d.outbox, d.logger,
	)
	if svc == nil {
		t.Fatal("expected non-nil service")
	}
}

// ---------------------------------------------------------------------------
// CreateVersion tests.
// ---------------------------------------------------------------------------

func TestCreateVersion_HappyPath_Upload(t *testing.T) {
	d := newTestDeps()
	d.withActiveDoc("org-1", "doc-1")
	svc := d.newService()

	params := defaultCreateParams()
	v, err := svc.CreateVersion(context.Background(), params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Version assertions.
	if v.VersionID != "test-uuid-001" {
		t.Errorf("version_id = %q, want test-uuid-001", v.VersionID)
	}
	if v.DocumentID != "doc-1" {
		t.Errorf("document_id = %q, want doc-1", v.DocumentID)
	}
	if v.OrganizationID != "org-1" {
		t.Errorf("organization_id = %q, want org-1", v.OrganizationID)
	}
	if v.VersionNumber != 1 {
		t.Errorf("version_number = %d, want 1", v.VersionNumber)
	}
	if v.OriginType != model.OriginTypeUpload {
		t.Errorf("origin_type = %q, want UPLOAD", v.OriginType)
	}
	if v.SourceFileKey != "org-1/doc-1/uploads/file.pdf" {
		t.Errorf("source_file_key = %q, want org-1/doc-1/uploads/file.pdf", v.SourceFileKey)
	}
	if v.SourceFileName != "contract.pdf" {
		t.Errorf("source_file_name = %q, want contract.pdf", v.SourceFileName)
	}
	if v.SourceFileSize != 1024 {
		t.Errorf("source_file_size = %d, want 1024", v.SourceFileSize)
	}
	if v.SourceFileChecksum != "abc123" {
		t.Errorf("source_file_checksum = %q, want abc123", v.SourceFileChecksum)
	}
	if v.ArtifactStatus != model.ArtifactStatusPending {
		t.Errorf("artifact_status = %q, want PENDING", v.ArtifactStatus)
	}
	if v.CreatedByUserID != "user-1" {
		t.Errorf("created_by_user_id = %q, want user-1", v.CreatedByUserID)
	}

	// Version was inserted.
	if len(d.versionRepo.insertedVersions) != 1 {
		t.Fatalf("expected 1 inserted version, got %d", len(d.versionRepo.insertedVersions))
	}

	// Document current_version_id updated.
	if len(d.docRepo.updatedDocs) != 1 {
		t.Fatalf("expected 1 updated doc, got %d", len(d.docRepo.updatedDocs))
	}
	if d.docRepo.updatedDocs[0].CurrentVersionID != "test-uuid-001" {
		t.Errorf("doc.current_version_id = %q, want test-uuid-001", d.docRepo.updatedDocs[0].CurrentVersionID)
	}

	// Audit assertions.
	if len(d.auditRepo.inserted) != 1 {
		t.Fatalf("expected 1 audit record, got %d", len(d.auditRepo.inserted))
	}
	audit := d.auditRepo.inserted[0]
	if audit.AuditID != "test-uuid-002" {
		t.Errorf("audit_id = %q, want test-uuid-002", audit.AuditID)
	}
	if audit.Action != model.AuditActionVersionCreated {
		t.Errorf("action = %q, want VERSION_CREATED", audit.Action)
	}
	if audit.ActorType != model.ActorTypeUser {
		t.Errorf("actor_type = %q, want USER", audit.ActorType)
	}
	if audit.ActorID != "user-1" {
		t.Errorf("actor_id = %q, want user-1", audit.ActorID)
	}
	if audit.DocumentID != "doc-1" {
		t.Errorf("audit.document_id = %q, want doc-1", audit.DocumentID)
	}
	if audit.VersionID != "test-uuid-001" {
		t.Errorf("audit.version_id = %q, want test-uuid-001", audit.VersionID)
	}

	// Audit details.
	var details map[string]any
	if err := json.Unmarshal(audit.Details, &details); err != nil {
		t.Fatalf("failed to unmarshal audit details: %v", err)
	}
	if details["origin_type"] != "UPLOAD" {
		t.Errorf("audit details origin_type = %v, want UPLOAD", details["origin_type"])
	}
	if details["version_number"] != float64(1) {
		t.Errorf("audit details version_number = %v, want 1", details["version_number"])
	}

	// Outbox assertions.
	if len(d.outbox.written) != 1 {
		t.Fatalf("expected 1 outbox write, got %d", len(d.outbox.written))
	}
	outboxEntry := d.outbox.written[0]
	if outboxEntry.AggregateID != "test-uuid-001" {
		t.Errorf("outbox aggregate_id = %q, want test-uuid-001", outboxEntry.AggregateID)
	}
	if outboxEntry.Topic != model.TopicDMEventsVersionCreated {
		t.Errorf("outbox topic = %q, want %s", outboxEntry.Topic, model.TopicDMEventsVersionCreated)
	}
	notif, ok := outboxEntry.Event.(model.VersionCreated)
	if !ok {
		t.Fatalf("outbox event is not VersionCreated: %T", outboxEntry.Event)
	}
	if notif.DocumentID != "doc-1" {
		t.Errorf("notification document_id = %q, want doc-1", notif.DocumentID)
	}
	if notif.VersionID != "test-uuid-001" {
		t.Errorf("notification version_id = %q, want test-uuid-001", notif.VersionID)
	}
	if notif.VersionNumber != 1 {
		t.Errorf("notification version_number = %d, want 1", notif.VersionNumber)
	}
	if notif.OrgID != "org-1" {
		t.Errorf("notification org_id = %q, want org-1", notif.OrgID)
	}
	if notif.OriginType != model.OriginTypeUpload {
		t.Errorf("notification origin_type = %q, want UPLOAD", notif.OriginType)
	}
	if notif.CreatedByUserID != "user-1" {
		t.Errorf("notification created_by_user_id = %q, want user-1", notif.CreatedByUserID)
	}
	if notif.CorrelationID == "" {
		t.Error("notification correlation_id should not be empty")
	}
	if notif.Timestamp.IsZero() {
		t.Error("notification timestamp should not be zero")
	}

	// Logger.
	if len(d.logger.infos) != 1 {
		t.Errorf("expected 1 info log, got %d", len(d.logger.infos))
	}
}

func TestCreateVersion_HappyPath_WithParentVersion(t *testing.T) {
	d := newTestDeps()
	d.withActiveDoc("org-1", "doc-1")
	d.versionRepo.nextVersionNumberFn = func(_ context.Context, _, _ string) (int, error) {
		return 2, nil
	}
	svc := d.newService()

	params := defaultCreateParams()
	params.OriginType = model.OriginTypeReUpload
	params.ParentVersionID = "version-1"

	v, err := svc.CreateVersion(context.Background(), params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if v.VersionNumber != 2 {
		t.Errorf("version_number = %d, want 2", v.VersionNumber)
	}
	if v.ParentVersionID != "version-1" {
		t.Errorf("parent_version_id = %q, want version-1", v.ParentVersionID)
	}
	if v.OriginType != model.OriginTypeReUpload {
		t.Errorf("origin_type = %q, want RE_UPLOAD", v.OriginType)
	}

	// Check outbox event includes parent_version_id.
	notif := d.outbox.written[0].Event.(model.VersionCreated)
	if notif.ParentVersionID != "version-1" {
		t.Errorf("notification parent_version_id = %q, want version-1", notif.ParentVersionID)
	}
}

func TestCreateVersion_HappyPath_WithOriginDescription(t *testing.T) {
	d := newTestDeps()
	d.withActiveDoc("org-1", "doc-1")
	svc := d.newService()

	params := defaultCreateParams()
	params.OriginDescription = "Applied clause 3.2 recommendation"

	v, err := svc.CreateVersion(context.Background(), params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if v.OriginDescription != "Applied clause 3.2 recommendation" {
		t.Errorf("origin_description = %q, want 'Applied clause 3.2 recommendation'", v.OriginDescription)
	}
}

func TestCreateVersion_HappyPath_ReCheck_CopiesSourceFileKey(t *testing.T) {
	d := newTestDeps()
	d.withActiveDoc("org-1", "doc-1")
	parentVersion := &model.DocumentVersion{
		VersionID:      "parent-v1",
		DocumentID:     "doc-1",
		OrganizationID: "org-1",
		VersionNumber:  1,
		OriginType:     model.OriginTypeUpload,
		SourceFileKey:  "org-1/doc-1/parent-v1/SOURCE_FILE",
		SourceFileName: "original.pdf",
	}
	d.versionRepo.findByIDFn = func(_ context.Context, orgID, docID, versionID string) (*model.DocumentVersion, error) {
		if versionID == "parent-v1" {
			return parentVersion, nil
		}
		return nil, port.NewVersionNotFoundError(versionID)
	}
	d.versionRepo.nextVersionNumberFn = func(_ context.Context, _, _ string) (int, error) {
		return 2, nil
	}
	svc := d.newService()

	params := port.CreateVersionParams{
		OrganizationID:  "org-1",
		DocumentID:      "doc-1",
		ParentVersionID: "parent-v1",
		OriginType:      model.OriginTypeReCheck,
		SourceFileName:  "original.pdf",
		SourceFileSize:  2048,
		CreatedByUserID: "user-1",
	}

	v, err := svc.CreateVersion(context.Background(), params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Source file key should be copied from parent.
	if v.SourceFileKey != "org-1/doc-1/parent-v1/SOURCE_FILE" {
		t.Errorf("source_file_key = %q, want org-1/doc-1/parent-v1/SOURCE_FILE", v.SourceFileKey)
	}
	if v.ParentVersionID != "parent-v1" {
		t.Errorf("parent_version_id = %q, want parent-v1", v.ParentVersionID)
	}
	if v.OriginType != model.OriginTypeReCheck {
		t.Errorf("origin_type = %q, want RE_CHECK", v.OriginType)
	}
}

func TestCreateVersion_AllOriginTypes(t *testing.T) {
	for _, ot := range model.AllOriginTypes {
		t.Run(string(ot), func(t *testing.T) {
			d := newTestDeps()
			d.withActiveDoc("org-1", "doc-1")
			if ot == model.OriginTypeReCheck {
				d.versionRepo.findByIDFn = func(_ context.Context, _, _, _ string) (*model.DocumentVersion, error) {
					return &model.DocumentVersion{
						VersionID:     "parent-v1",
						SourceFileKey: "org-1/doc-1/parent-v1/SOURCE_FILE",
					}, nil
				}
			}
			svc := d.newService()

			params := defaultCreateParams()
			params.OriginType = ot
			if ot == model.OriginTypeReCheck {
				params.ParentVersionID = "parent-v1"
				params.SourceFileKey = "" // should be copied from parent
			}

			v, err := svc.CreateVersion(context.Background(), params)
			if err != nil {
				t.Fatalf("unexpected error for origin %s: %v", ot, err)
			}
			if v.OriginType != ot {
				t.Errorf("origin_type = %q, want %q", v.OriginType, ot)
			}
		})
	}
}

// --- Validation error tests ---

func TestCreateVersion_EmptyOrganizationID(t *testing.T) {
	d := newTestDeps()
	svc := d.newService()

	params := defaultCreateParams()
	params.OrganizationID = ""
	_, err := svc.CreateVersion(context.Background(), params)
	if port.ErrorCode(err) != port.ErrCodeValidation {
		t.Fatalf("expected VALIDATION_ERROR, got %v", err)
	}
}

func TestCreateVersion_EmptyDocumentID(t *testing.T) {
	d := newTestDeps()
	svc := d.newService()

	params := defaultCreateParams()
	params.DocumentID = ""
	_, err := svc.CreateVersion(context.Background(), params)
	if port.ErrorCode(err) != port.ErrCodeValidation {
		t.Fatalf("expected VALIDATION_ERROR, got %v", err)
	}
}

func TestCreateVersion_InvalidOriginType(t *testing.T) {
	d := newTestDeps()
	svc := d.newService()

	params := defaultCreateParams()
	params.OriginType = "INVALID_ORIGIN"
	_, err := svc.CreateVersion(context.Background(), params)
	if port.ErrorCode(err) != port.ErrCodeValidation {
		t.Fatalf("expected VALIDATION_ERROR, got %v", err)
	}
	if !strings.Contains(err.Error(), "origin_type") {
		t.Errorf("error should mention origin_type: %v", err)
	}
}

func TestCreateVersion_EmptyOriginType(t *testing.T) {
	d := newTestDeps()
	svc := d.newService()

	params := defaultCreateParams()
	params.OriginType = ""
	_, err := svc.CreateVersion(context.Background(), params)
	if port.ErrorCode(err) != port.ErrCodeValidation {
		t.Fatalf("expected VALIDATION_ERROR, got %v", err)
	}
}

func TestCreateVersion_EmptySourceFileName(t *testing.T) {
	d := newTestDeps()
	svc := d.newService()

	params := defaultCreateParams()
	params.SourceFileName = ""
	_, err := svc.CreateVersion(context.Background(), params)
	if port.ErrorCode(err) != port.ErrCodeValidation {
		t.Fatalf("expected VALIDATION_ERROR, got %v", err)
	}
}

func TestCreateVersion_ZeroSourceFileSize(t *testing.T) {
	d := newTestDeps()
	svc := d.newService()

	params := defaultCreateParams()
	params.SourceFileSize = 0
	_, err := svc.CreateVersion(context.Background(), params)
	if port.ErrorCode(err) != port.ErrCodeValidation {
		t.Fatalf("expected VALIDATION_ERROR, got %v", err)
	}
	if !strings.Contains(err.Error(), "source_file_size") {
		t.Errorf("error should mention source_file_size: %v", err)
	}
}

func TestCreateVersion_NegativeSourceFileSize(t *testing.T) {
	d := newTestDeps()
	svc := d.newService()

	params := defaultCreateParams()
	params.SourceFileSize = -1
	_, err := svc.CreateVersion(context.Background(), params)
	if port.ErrorCode(err) != port.ErrCodeValidation {
		t.Fatalf("expected VALIDATION_ERROR, got %v", err)
	}
}

func TestCreateVersion_EmptyCreatedByUserID(t *testing.T) {
	d := newTestDeps()
	svc := d.newService()

	params := defaultCreateParams()
	params.CreatedByUserID = ""
	_, err := svc.CreateVersion(context.Background(), params)
	if port.ErrorCode(err) != port.ErrCodeValidation {
		t.Fatalf("expected VALIDATION_ERROR, got %v", err)
	}
}

func TestCreateVersion_EmptySourceFileKey_NonReCheck(t *testing.T) {
	d := newTestDeps()
	svc := d.newService()

	params := defaultCreateParams()
	params.SourceFileKey = ""
	_, err := svc.CreateVersion(context.Background(), params)
	if port.ErrorCode(err) != port.ErrCodeValidation {
		t.Fatalf("expected VALIDATION_ERROR, got %v", err)
	}
	if !strings.Contains(err.Error(), "source_file_key") {
		t.Errorf("error should mention source_file_key: %v", err)
	}
}

func TestCreateVersion_ReCheck_EmptyParentVersionID(t *testing.T) {
	d := newTestDeps()
	svc := d.newService()

	params := defaultCreateParams()
	params.OriginType = model.OriginTypeReCheck
	params.SourceFileKey = "" // OK for RE_CHECK
	params.ParentVersionID = ""
	_, err := svc.CreateVersion(context.Background(), params)
	if port.ErrorCode(err) != port.ErrCodeValidation {
		t.Fatalf("expected VALIDATION_ERROR, got %v", err)
	}
	if !strings.Contains(err.Error(), "parent_version_id") {
		t.Errorf("error should mention parent_version_id: %v", err)
	}
}

func TestCreateVersion_ReCheck_ParentVersionNotFound(t *testing.T) {
	d := newTestDeps()
	svc := d.newService()

	params := defaultCreateParams()
	params.OriginType = model.OriginTypeReCheck
	params.SourceFileKey = ""
	params.ParentVersionID = "nonexistent-v"
	_, err := svc.CreateVersion(context.Background(), params)
	if port.ErrorCode(err) != port.ErrCodeVersionNotFound {
		t.Fatalf("expected VERSION_NOT_FOUND, got %v", err)
	}
}

// --- Document error tests (checked inside transaction) ---

func TestCreateVersion_DocumentNotFound(t *testing.T) {
	d := newTestDeps()
	// docRepo default returns NotFound
	svc := d.newService()

	_, err := svc.CreateVersion(context.Background(), defaultCreateParams())
	if port.ErrorCode(err) != port.ErrCodeDocumentNotFound {
		t.Fatalf("expected DOCUMENT_NOT_FOUND, got %v", err)
	}
}

func TestCreateVersion_DocumentArchived(t *testing.T) {
	d := newTestDeps()
	d.docRepo.findByIDFn = func(_ context.Context, orgID, docID string) (*model.Document, error) {
		doc := model.NewDocument(docID, orgID, "Test", "user-1")
		doc.TransitionTo(model.DocumentStatusArchived)
		return doc, nil
	}
	svc := d.newService()

	_, err := svc.CreateVersion(context.Background(), defaultCreateParams())
	if port.ErrorCode(err) != port.ErrCodeInvalidStatus {
		t.Fatalf("expected INVALID_STATUS, got %v", err)
	}
	if !strings.Contains(err.Error(), "ARCHIVED") {
		t.Errorf("error should mention ARCHIVED: %v", err)
	}
}

func TestCreateVersion_DocumentDeleted(t *testing.T) {
	d := newTestDeps()
	d.docRepo.findByIDFn = func(_ context.Context, orgID, docID string) (*model.Document, error) {
		doc := model.NewDocument(docID, orgID, "Test", "user-1")
		doc.TransitionTo(model.DocumentStatusDeleted)
		return doc, nil
	}
	svc := d.newService()

	_, err := svc.CreateVersion(context.Background(), defaultCreateParams())
	if port.ErrorCode(err) != port.ErrCodeInvalidStatus {
		t.Fatalf("expected INVALID_STATUS, got %v", err)
	}
	if !strings.Contains(err.Error(), "DELETED") {
		t.Errorf("error should mention DELETED: %v", err)
	}
}

// Document status is now checked inside the transaction, so INVALID_STATUS
// does NOT trigger the retry loop (it is not VersionAlreadyExists).
func TestCreateVersion_DocumentArchived_NoRetry(t *testing.T) {
	d := newTestDeps()
	findCount := 0
	d.docRepo.findByIDFn = func(_ context.Context, orgID, docID string) (*model.Document, error) {
		findCount++
		doc := model.NewDocument(docID, orgID, "Test", "user-1")
		doc.TransitionTo(model.DocumentStatusArchived)
		return doc, nil
	}
	svc := d.newService()

	_, err := svc.CreateVersion(context.Background(), defaultCreateParams())
	if port.ErrorCode(err) != port.ErrCodeInvalidStatus {
		t.Fatalf("expected INVALID_STATUS, got %v", err)
	}
	// Should only fetch once (no retry for non-conflict errors).
	if findCount != 1 {
		t.Errorf("expected 1 FindByID call, got %d", findCount)
	}
}

// --- Transaction error tests ---

func TestCreateVersion_NextVersionNumberFails(t *testing.T) {
	d := newTestDeps()
	d.withActiveDoc("org-1", "doc-1")
	d.versionRepo.nextVersionNumberFn = func(context.Context, string, string) (int, error) {
		return 0, port.NewDatabaseError("next version failed", errors.New("connection refused"))
	}
	svc := d.newService()

	_, err := svc.CreateVersion(context.Background(), defaultCreateParams())
	if port.ErrorCode(err) != port.ErrCodeDatabaseFailed {
		t.Fatalf("expected DATABASE_FAILED, got %v", err)
	}
}

func TestCreateVersion_VersionInsertFails(t *testing.T) {
	d := newTestDeps()
	d.withActiveDoc("org-1", "doc-1")
	d.versionRepo.insertFn = func(context.Context, *model.DocumentVersion) error {
		return port.NewDatabaseError("insert failed", errors.New("connection refused"))
	}
	svc := d.newService()

	_, err := svc.CreateVersion(context.Background(), defaultCreateParams())
	if port.ErrorCode(err) != port.ErrCodeDatabaseFailed {
		t.Fatalf("expected DATABASE_FAILED, got %v", err)
	}
}

func TestCreateVersion_DocUpdateFails(t *testing.T) {
	d := newTestDeps()
	d.withActiveDoc("org-1", "doc-1")
	d.docRepo.updateFn = func(context.Context, *model.Document) error {
		return port.NewDatabaseError("update failed", errors.New("connection refused"))
	}
	svc := d.newService()

	_, err := svc.CreateVersion(context.Background(), defaultCreateParams())
	if port.ErrorCode(err) != port.ErrCodeDatabaseFailed {
		t.Fatalf("expected DATABASE_FAILED, got %v", err)
	}
	// Audit should not be inserted since doc update failed inside tx.
	if len(d.auditRepo.inserted) != 0 {
		t.Error("audit should not be inserted when doc update fails")
	}
}

func TestCreateVersion_AuditInsertFails(t *testing.T) {
	d := newTestDeps()
	d.withActiveDoc("org-1", "doc-1")
	d.auditRepo.insertErr = port.NewDatabaseError("audit failed", errors.New("disk full"))
	svc := d.newService()

	_, err := svc.CreateVersion(context.Background(), defaultCreateParams())
	if port.ErrorCode(err) != port.ErrCodeDatabaseFailed {
		t.Fatalf("expected DATABASE_FAILED, got %v", err)
	}
}

func TestCreateVersion_OutboxWriteFails(t *testing.T) {
	d := newTestDeps()
	d.withActiveDoc("org-1", "doc-1")
	d.outbox.err = port.NewDatabaseError("outbox write failed", errors.New("disk full"))
	svc := d.newService()

	_, err := svc.CreateVersion(context.Background(), defaultCreateParams())
	if port.ErrorCode(err) != port.ErrCodeDatabaseFailed {
		t.Fatalf("expected DATABASE_FAILED, got %v", err)
	}
}

// --- Optimistic locking / retry tests ---

func TestCreateVersion_OptimisticLocking_SucceedsOnRetry(t *testing.T) {
	d := newTestDeps()
	d.withActiveDoc("org-1", "doc-1")

	insertAttempt := 0
	d.versionRepo.insertFn = func(_ context.Context, v *model.DocumentVersion) error {
		insertAttempt++
		if insertAttempt == 1 {
			return port.NewVersionAlreadyExistsError(v.VersionID)
		}
		return nil
	}
	versionNum := 0
	d.versionRepo.nextVersionNumberFn = func(_ context.Context, _, _ string) (int, error) {
		versionNum++
		return versionNum, nil
	}
	svc := d.newService()

	v, err := svc.CreateVersion(context.Background(), defaultCreateParams())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v.VersionNumber != 2 {
		t.Errorf("version_number = %d, want 2 (after retry)", v.VersionNumber)
	}
	if insertAttempt != 2 {
		t.Errorf("expected 2 insert attempts, got %d", insertAttempt)
	}
	// Should have logged a warning for the retry.
	if len(d.logger.warns) != 1 {
		t.Errorf("expected 1 warning log for retry, got %d", len(d.logger.warns))
	}
}

func TestCreateVersion_OptimisticLocking_ExhaustsRetries(t *testing.T) {
	d := newTestDeps()
	d.withActiveDoc("org-1", "doc-1")
	d.versionRepo.insertFn = func(_ context.Context, v *model.DocumentVersion) error {
		return port.NewVersionAlreadyExistsError(v.VersionID)
	}
	svc := d.newService()

	_, err := svc.CreateVersion(context.Background(), defaultCreateParams())
	if port.ErrorCode(err) != port.ErrCodeVersionAlreadyExists {
		t.Fatalf("expected VERSION_ALREADY_EXISTS, got %v", err)
	}
	if len(d.logger.warns) != maxRetries {
		t.Errorf("expected %d warning logs, got %d", maxRetries, len(d.logger.warns))
	}
	if len(d.logger.errors) != 1 {
		t.Errorf("expected 1 error log, got %d", len(d.logger.errors))
	}
}

func TestCreateVersion_NonConflictError_NoRetry(t *testing.T) {
	d := newTestDeps()
	d.withActiveDoc("org-1", "doc-1")
	d.versionRepo.insertFn = func(context.Context, *model.DocumentVersion) error {
		return port.NewDatabaseError("insert failed", errors.New("connection refused"))
	}
	svc := d.newService()

	_, err := svc.CreateVersion(context.Background(), defaultCreateParams())
	if port.ErrorCode(err) != port.ErrCodeDatabaseFailed {
		t.Fatalf("expected DATABASE_FAILED, got %v", err)
	}
	// Non-conflict errors should not trigger retries.
	if len(d.versionRepo.insertedVersions) != 1 {
		t.Errorf("expected exactly 1 insert attempt, got %d", len(d.versionRepo.insertedVersions))
	}
	if len(d.logger.warns) != 0 {
		t.Errorf("expected 0 warning logs, got %d", len(d.logger.warns))
	}
}

func TestCreateVersion_ContextCancelled_NoRetry(t *testing.T) {
	d := newTestDeps()
	d.withActiveDoc("org-1", "doc-1")
	svc := d.newService()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, err := svc.CreateVersion(ctx, defaultCreateParams())
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
	// Should fail fast without attempting any transaction.
	if len(d.versionRepo.insertedVersions) != 0 {
		t.Errorf("expected 0 insert attempts, got %d", len(d.versionRepo.insertedVersions))
	}
}

// --- Document re-fetched inside transaction on retry ---

func TestCreateVersion_DocRefetchedInsideTxOnRetry(t *testing.T) {
	d := newTestDeps()

	findCount := 0
	d.docRepo.findByIDFn = func(_ context.Context, orgID, docID string) (*model.Document, error) {
		findCount++
		return model.NewDocument(docID, orgID, "Test Document", "user-1"), nil
	}

	insertAttempt := 0
	d.versionRepo.insertFn = func(_ context.Context, v *model.DocumentVersion) error {
		insertAttempt++
		if insertAttempt == 1 {
			return port.NewVersionAlreadyExistsError(v.VersionID)
		}
		return nil
	}
	versionNum := 0
	d.versionRepo.nextVersionNumberFn = func(_ context.Context, _, _ string) (int, error) {
		versionNum++
		return versionNum, nil
	}
	svc := d.newService()

	_, err := svc.CreateVersion(context.Background(), defaultCreateParams())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Document should be fetched inside each transaction attempt.
	if findCount != 2 {
		t.Errorf("expected 2 FindByID calls (one per attempt), got %d", findCount)
	}
}

// ---------------------------------------------------------------------------
// GetVersion tests.
// ---------------------------------------------------------------------------

func TestGetVersion_HappyPath(t *testing.T) {
	d := newTestDeps()
	expected := &model.DocumentVersion{
		VersionID:      "v-1",
		DocumentID:     "doc-1",
		OrganizationID: "org-1",
		VersionNumber:  1,
	}
	d.versionRepo.findByIDFn = func(_ context.Context, orgID, docID, versionID string) (*model.DocumentVersion, error) {
		if orgID == "org-1" && docID == "doc-1" && versionID == "v-1" {
			return expected, nil
		}
		return nil, port.NewVersionNotFoundError(versionID)
	}
	svc := d.newService()

	v, err := svc.GetVersion(context.Background(), "org-1", "doc-1", "v-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v.VersionID != "v-1" {
		t.Errorf("version_id = %q, want v-1", v.VersionID)
	}
}

func TestGetVersion_EmptyOrganizationID(t *testing.T) {
	d := newTestDeps()
	svc := d.newService()

	_, err := svc.GetVersion(context.Background(), "", "doc-1", "v-1")
	if port.ErrorCode(err) != port.ErrCodeValidation {
		t.Fatalf("expected VALIDATION_ERROR, got %v", err)
	}
}

func TestGetVersion_EmptyDocumentID(t *testing.T) {
	d := newTestDeps()
	svc := d.newService()

	_, err := svc.GetVersion(context.Background(), "org-1", "", "v-1")
	if port.ErrorCode(err) != port.ErrCodeValidation {
		t.Fatalf("expected VALIDATION_ERROR, got %v", err)
	}
}

func TestGetVersion_EmptyVersionID(t *testing.T) {
	d := newTestDeps()
	svc := d.newService()

	_, err := svc.GetVersion(context.Background(), "org-1", "doc-1", "")
	if port.ErrorCode(err) != port.ErrCodeValidation {
		t.Fatalf("expected VALIDATION_ERROR, got %v", err)
	}
}

func TestGetVersion_NotFound(t *testing.T) {
	d := newTestDeps()
	svc := d.newService()

	_, err := svc.GetVersion(context.Background(), "org-1", "doc-1", "v-1")
	if port.ErrorCode(err) != port.ErrCodeVersionNotFound {
		t.Fatalf("expected VERSION_NOT_FOUND, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// ListVersions tests.
// ---------------------------------------------------------------------------

func TestListVersions_HappyPath(t *testing.T) {
	d := newTestDeps()
	versions := []*model.DocumentVersion{
		{VersionID: "v-1", DocumentID: "doc-1", VersionNumber: 1},
		{VersionID: "v-2", DocumentID: "doc-1", VersionNumber: 2},
	}
	d.versionRepo.listFn = func(_ context.Context, _, _ string, _, _ int) ([]*model.DocumentVersion, int, error) {
		return versions, 2, nil
	}
	svc := d.newService()

	result, err := svc.ListVersions(context.Background(), port.ListVersionsParams{
		OrganizationID: "org-1",
		DocumentID:     "doc-1",
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

func TestListVersions_EmptyOrganizationID(t *testing.T) {
	d := newTestDeps()
	svc := d.newService()

	_, err := svc.ListVersions(context.Background(), port.ListVersionsParams{
		DocumentID: "doc-1",
		Page:       1,
		PageSize:   10,
	})
	if port.ErrorCode(err) != port.ErrCodeValidation {
		t.Fatalf("expected VALIDATION_ERROR, got %v", err)
	}
}

func TestListVersions_EmptyDocumentID(t *testing.T) {
	d := newTestDeps()
	svc := d.newService()

	_, err := svc.ListVersions(context.Background(), port.ListVersionsParams{
		OrganizationID: "org-1",
		Page:           1,
		PageSize:       10,
	})
	if port.ErrorCode(err) != port.ErrCodeValidation {
		t.Fatalf("expected VALIDATION_ERROR, got %v", err)
	}
}

func TestListVersions_InvalidPage(t *testing.T) {
	d := newTestDeps()
	svc := d.newService()

	_, err := svc.ListVersions(context.Background(), port.ListVersionsParams{
		OrganizationID: "org-1",
		DocumentID:     "doc-1",
		Page:           0,
		PageSize:       10,
	})
	if port.ErrorCode(err) != port.ErrCodeValidation {
		t.Fatalf("expected VALIDATION_ERROR, got %v", err)
	}
}

func TestListVersions_InvalidPageSize(t *testing.T) {
	d := newTestDeps()
	svc := d.newService()

	_, err := svc.ListVersions(context.Background(), port.ListVersionsParams{
		OrganizationID: "org-1",
		DocumentID:     "doc-1",
		Page:           1,
		PageSize:       0,
	})
	if port.ErrorCode(err) != port.ErrCodeValidation {
		t.Fatalf("expected VALIDATION_ERROR, got %v", err)
	}
}

func TestListVersions_NilItemsNormalized(t *testing.T) {
	d := newTestDeps()
	d.versionRepo.listFn = func(context.Context, string, string, int, int) ([]*model.DocumentVersion, int, error) {
		return nil, 0, nil
	}
	svc := d.newService()

	result, err := svc.ListVersions(context.Background(), port.ListVersionsParams{
		OrganizationID: "org-1",
		DocumentID:     "doc-1",
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

func TestListVersions_PageSizeClamped(t *testing.T) {
	d := newTestDeps()
	var capturedPageSize int
	d.versionRepo.listFn = func(_ context.Context, _, _ string, _, pageSize int) ([]*model.DocumentVersion, int, error) {
		capturedPageSize = pageSize
		return []*model.DocumentVersion{}, 0, nil
	}
	svc := d.newService()

	result, err := svc.ListVersions(context.Background(), port.ListVersionsParams{
		OrganizationID: "org-1",
		DocumentID:     "doc-1",
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

func TestListVersions_RepoError(t *testing.T) {
	d := newTestDeps()
	d.versionRepo.listFn = func(context.Context, string, string, int, int) ([]*model.DocumentVersion, int, error) {
		return nil, 0, port.NewDatabaseError("list failed", errors.New("timeout"))
	}
	svc := d.newService()

	_, err := svc.ListVersions(context.Background(), port.ListVersionsParams{
		OrganizationID: "org-1",
		DocumentID:     "doc-1",
		Page:           1,
		PageSize:       10,
	})
	if port.ErrorCode(err) != port.ErrCodeDatabaseFailed {
		t.Fatalf("expected DATABASE_FAILED, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// isValidOriginType tests.
// ---------------------------------------------------------------------------

func TestIsValidOriginType(t *testing.T) {
	for _, ot := range model.AllOriginTypes {
		if !isValidOriginType(ot) {
			t.Errorf("expected %q to be valid", ot)
		}
	}
	if isValidOriginType("INVALID") {
		t.Error("expected INVALID to be invalid")
	}
	if isValidOriginType("") {
		t.Error("expected empty string to be invalid")
	}
}
