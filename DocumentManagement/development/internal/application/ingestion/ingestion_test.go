package ingestion

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
	"contractpro/document-management/internal/egress/outbox"
	"contractpro/document-management/internal/infra/objectstorage"
	"io"
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

type mockVersionRepo struct {
	findByID          func(ctx context.Context, orgID, docID, versionID string) (*model.DocumentVersion, error)
	update            func(ctx context.Context, version *model.DocumentVersion) error
	updatedVersions   []*model.DocumentVersion
	findByIDCallCount int
}

func (m *mockVersionRepo) FindByID(ctx context.Context, orgID, docID, versionID string) (*model.DocumentVersion, error) {
	m.findByIDCallCount++
	if m.findByID != nil {
		return m.findByID(ctx, orgID, docID, versionID)
	}
	return nil, port.NewVersionNotFoundError(versionID)
}

func (m *mockVersionRepo) Update(ctx context.Context, version *model.DocumentVersion) error {
	m.updatedVersions = append(m.updatedVersions, version)
	if m.update != nil {
		return m.update(ctx, version)
	}
	return nil
}

func (m *mockVersionRepo) Insert(context.Context, *model.DocumentVersion) error {
	panic("not used in ingestion")
}
func (m *mockVersionRepo) List(context.Context, string, string, int, int) ([]*model.DocumentVersion, int, error) {
	panic("not used in ingestion")
}
func (m *mockVersionRepo) NextVersionNumber(context.Context, string, string) (int, error) {
	panic("not used in ingestion")
}

type mockArtifactRepo struct {
	inserted  []*model.ArtifactDescriptor
	insertErr error
}

func (m *mockArtifactRepo) Insert(ctx context.Context, d *model.ArtifactDescriptor) error {
	if m.insertErr != nil {
		return m.insertErr
	}
	m.inserted = append(m.inserted, d)
	return nil
}

func (m *mockArtifactRepo) FindByVersionAndType(context.Context, string, string, string, model.ArtifactType) (*model.ArtifactDescriptor, error) {
	panic("not used in ingestion")
}
func (m *mockArtifactRepo) ListByVersion(context.Context, string, string, string) ([]*model.ArtifactDescriptor, error) {
	panic("not used in ingestion")
}
func (m *mockArtifactRepo) ListByVersionAndTypes(context.Context, string, string, string, []model.ArtifactType) ([]*model.ArtifactDescriptor, error) {
	panic("not used in ingestion")
}
func (m *mockArtifactRepo) DeleteByVersion(context.Context, string, string, string) error {
	panic("not used in ingestion")
}

type mockAuditRepo struct {
	inserted []*model.AuditRecord
}

func (m *mockAuditRepo) Insert(ctx context.Context, r *model.AuditRecord) error {
	m.inserted = append(m.inserted, r)
	return nil
}

func (m *mockAuditRepo) List(context.Context, port.AuditListParams) ([]*model.AuditRecord, int, error) {
	panic("not used in ingestion")
}

type mockAuditRepoWithErr struct {
	failAfter int
	err       error
	callCount int
}

func (m *mockAuditRepoWithErr) Insert(ctx context.Context, r *model.AuditRecord) error {
	if m.callCount >= m.failAfter {
		return m.err
	}
	m.callCount++
	return nil
}

func (m *mockAuditRepoWithErr) List(context.Context, port.AuditListParams) ([]*model.AuditRecord, int, error) {
	panic("not used in ingestion")
}

type mockObjectStorage struct {
	putCalls    []putCall
	putErr      error     // returned on next PutObject call
	putErrAfter int       // fail after N successful puts (-1 = never)
	putCount    int       // internal counter
	headResults map[string]headResult
	deleteCalls []string
}

type putCall struct {
	key         string
	contentType string
	data        []byte
}

type headResult struct {
	size   int64
	exists bool
	err    error
}

func newMockObjectStorage() *mockObjectStorage {
	return &mockObjectStorage{
		putErrAfter: -1,
		headResults: make(map[string]headResult),
	}
}

func (m *mockObjectStorage) PutObject(ctx context.Context, key string, data io.Reader, contentType string) error {
	if m.putErrAfter >= 0 && m.putCount >= m.putErrAfter {
		return m.putErr
	}
	m.putCount++
	b, _ := io.ReadAll(data)
	m.putCalls = append(m.putCalls, putCall{key: key, contentType: contentType, data: b})
	return nil
}

func (m *mockObjectStorage) HeadObject(ctx context.Context, key string) (int64, bool, error) {
	r, ok := m.headResults[key]
	if !ok {
		return 0, false, nil
	}
	return r.size, r.exists, r.err
}

func (m *mockObjectStorage) DeleteObject(ctx context.Context, key string) error {
	m.deleteCalls = append(m.deleteCalls, key)
	return nil
}

func (m *mockObjectStorage) GetObject(context.Context, string) (io.ReadCloser, error) {
	panic("not used in ingestion")
}
func (m *mockObjectStorage) GeneratePresignedURL(context.Context, string, time.Duration) (string, error) {
	panic("not used in ingestion")
}
func (m *mockObjectStorage) DeleteByPrefix(context.Context, string) error {
	panic("not used in ingestion")
}

type mockOutboxRepo struct {
	entries   []port.OutboxEntry
	insertErr error
}

func (m *mockOutboxRepo) Insert(ctx context.Context, entries ...port.OutboxEntry) error {
	if m.insertErr != nil {
		return m.insertErr
	}
	m.entries = append(m.entries, entries...)
	return nil
}
func (m *mockOutboxRepo) FetchUnpublished(context.Context, int) ([]port.OutboxEntry, error) {
	return nil, nil
}
func (m *mockOutboxRepo) MarkPublished(context.Context, []string) error { return nil }
func (m *mockOutboxRepo) DeletePublished(context.Context, time.Time, int) (int64, error) {
	return 0, nil
}
func (m *mockOutboxRepo) PendingStats(context.Context) (int64, float64, error) { return 0, 0, nil }

type mockLogger struct {
	messages []logMsg
}
type logMsg struct {
	level string
	msg   string
}

func (m *mockLogger) Info(msg string, _ ...any)  { m.messages = append(m.messages, logMsg{"INFO", msg}) }
func (m *mockLogger) Warn(msg string, _ ...any)  { m.messages = append(m.messages, logMsg{"WARN", msg}) }
func (m *mockLogger) Error(msg string, _ ...any) { m.messages = append(m.messages, logMsg{"ERROR", msg}) }

// ---------------------------------------------------------------------------
// Test helpers.
// ---------------------------------------------------------------------------

type testDeps struct {
	transactor    *mockTransactor
	versionRepo   *mockVersionRepo
	artifactRepo  *mockArtifactRepo
	auditRepo     *mockAuditRepo
	objectStorage *mockObjectStorage
	outboxRepo    *mockOutboxRepo
	outboxWriter  *outbox.OutboxWriter
	logger        *mockLogger
}

func newTestDeps() *testDeps {
	outboxRepo := &mockOutboxRepo{}
	return &testDeps{
		transactor:    &mockTransactor{},
		versionRepo:   &mockVersionRepo{},
		artifactRepo:  &mockArtifactRepo{},
		auditRepo:     &mockAuditRepo{},
		objectStorage: newMockObjectStorage(),
		outboxRepo:    outboxRepo,
		outboxWriter:  outbox.NewOutboxWriter(outboxRepo),
		logger:        &mockLogger{},
	}
}

func (d *testDeps) newService() *ArtifactIngestionService {
	svc := NewArtifactIngestionService(
		d.transactor, d.versionRepo, d.artifactRepo,
		d.auditRepo, d.objectStorage, d.outboxWriter, d.logger,
	)
	uuidCounter := 0
	svc.newUUID = func() string {
		uuidCounter++
		return fmt.Sprintf("test-uuid-%03d", uuidCounter)
	}
	return svc
}

func newTestVersion(orgID, docID, versionID string, status model.ArtifactStatus) *model.DocumentVersion {
	return &model.DocumentVersion{
		VersionID:      versionID,
		DocumentID:     docID,
		OrganizationID: orgID,
		VersionNumber:  1,
		ArtifactStatus: status,
		CreatedAt:      time.Now().UTC(),
	}
}

func validDPEvent() model.DocumentProcessingArtifactsReady {
	return model.DocumentProcessingArtifactsReady{
		EventMeta: model.EventMeta{
			CorrelationID: "corr-001",
			Timestamp:     time.Now().UTC(),
		},
		JobID:        "job-001",
		DocumentID:   "doc-001",
		VersionID:    "ver-001",
		OrgID:        "org-001",
		OCRRaw:       json.RawMessage(`{"pages":[]}`),
		Text:         json.RawMessage(`{"content":"hello"}`),
		Structure:    json.RawMessage(`{"sections":[]}`),
		SemanticTree: json.RawMessage(`{"root":{}}`),
		Warnings:     json.RawMessage(`[{"code":"W1"}]`),
	}
}

func validLICEvent() model.LegalAnalysisArtifactsReady {
	return model.LegalAnalysisArtifactsReady{
		EventMeta: model.EventMeta{
			CorrelationID: "corr-002",
			Timestamp:     time.Now().UTC(),
		},
		JobID:                "job-002",
		DocumentID:           "doc-001",
		VersionID:            "ver-001",
		OrgID:                "org-001",
		ClassificationResult: json.RawMessage(`{"type":"supply"}`),
		KeyParameters:        json.RawMessage(`{"params":[]}`),
		RiskAnalysis:         json.RawMessage(`{"risks":[]}`),
		RiskProfile:          json.RawMessage(`{"score":0.7}`),
		Recommendations:      json.RawMessage(`{"items":[]}`),
		Summary:              json.RawMessage(`{"text":"ok"}`),
		DetailedReport:       json.RawMessage(`{"sections":[]}`),
		AggregateScore:       json.RawMessage(`{"value":85}`),
	}
}

func validREEvent() model.ReportsArtifactsReady {
	return model.ReportsArtifactsReady{
		EventMeta: model.EventMeta{
			CorrelationID: "corr-003",
			Timestamp:     time.Now().UTC(),
		},
		JobID:      "job-003",
		DocumentID: "doc-001",
		VersionID:  "ver-001",
		OrgID:      "org-001",
		ExportPDF: &model.BlobReference{
			StorageKey:  "re/exports/report.pdf",
			FileName:    "report.pdf",
			SizeBytes:   1024,
			ContentHash: "abc123",
		},
		ExportDOCX: &model.BlobReference{
			StorageKey:  "re/exports/report.docx",
			FileName:    "report.docx",
			SizeBytes:   2048,
			ContentHash: "def456",
		},
	}
}

func setupVersionFind(d *testDeps, version *model.DocumentVersion) {
	d.versionRepo.findByID = func(ctx context.Context, orgID, docID, versionID string) (*model.DocumentVersion, error) {
		if orgID == version.OrganizationID && docID == version.DocumentID && versionID == version.VersionID {
			return version, nil
		}
		return nil, port.NewVersionNotFoundError(versionID)
	}
}

// ---------------------------------------------------------------------------
// Tests: Constructor.
// ---------------------------------------------------------------------------

func TestNewArtifactIngestionService_PanicsOnNilDeps(t *testing.T) {
	d := newTestDeps()

	tests := []struct {
		name string
		fn   func()
	}{
		{"nil transactor", func() {
			NewArtifactIngestionService(nil, d.versionRepo, d.artifactRepo, d.auditRepo, d.objectStorage, d.outboxWriter, d.logger)
		}},
		{"nil versionRepo", func() {
			NewArtifactIngestionService(d.transactor, nil, d.artifactRepo, d.auditRepo, d.objectStorage, d.outboxWriter, d.logger)
		}},
		{"nil artifactRepo", func() {
			NewArtifactIngestionService(d.transactor, d.versionRepo, nil, d.auditRepo, d.objectStorage, d.outboxWriter, d.logger)
		}},
		{"nil auditRepo", func() {
			NewArtifactIngestionService(d.transactor, d.versionRepo, d.artifactRepo, nil, d.objectStorage, d.outboxWriter, d.logger)
		}},
		{"nil objectStorage", func() {
			NewArtifactIngestionService(d.transactor, d.versionRepo, d.artifactRepo, d.auditRepo, nil, d.outboxWriter, d.logger)
		}},
		{"nil outboxWriter", func() {
			NewArtifactIngestionService(d.transactor, d.versionRepo, d.artifactRepo, d.auditRepo, d.objectStorage, nil, d.logger)
		}},
		{"nil logger", func() {
			NewArtifactIngestionService(d.transactor, d.versionRepo, d.artifactRepo, d.auditRepo, d.objectStorage, d.outboxWriter, nil)
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r == nil {
					t.Fatal("expected panic")
				}
			}()
			tt.fn()
		})
	}
}

// ---------------------------------------------------------------------------
// Tests: HandleDPArtifacts.
// ---------------------------------------------------------------------------

func TestHandleDPArtifacts_HappyPath(t *testing.T) {
	d := newTestDeps()
	version := newTestVersion("org-001", "doc-001", "ver-001", model.ArtifactStatusPending)
	setupVersionFind(d, version)

	svc := d.newService()
	err := svc.HandleDPArtifacts(context.Background(), validDPEvent())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify 5 blobs uploaded.
	if got := len(d.objectStorage.putCalls); got != 5 {
		t.Fatalf("expected 5 put calls, got %d", got)
	}

	// Verify storage keys follow naming convention.
	expectedKey := objectstorage.ArtifactKey("org-001", "doc-001", "ver-001", model.ArtifactTypeOCRRaw)
	if d.objectStorage.putCalls[0].key != expectedKey {
		t.Errorf("first key = %q, want %q", d.objectStorage.putCalls[0].key, expectedKey)
	}

	// Verify all content types are JSON.
	for _, pc := range d.objectStorage.putCalls {
		if pc.contentType != objectstorage.ContentTypeJSON {
			t.Errorf("content type = %q, want %q", pc.contentType, objectstorage.ContentTypeJSON)
		}
	}

	// Verify 5 artifact descriptors inserted.
	if got := len(d.artifactRepo.inserted); got != 5 {
		t.Fatalf("expected 5 descriptors, got %d", got)
	}
	for _, desc := range d.artifactRepo.inserted {
		if desc.ProducerDomain != model.ProducerDomainDP {
			t.Errorf("producer = %q, want DP", desc.ProducerDomain)
		}
		if desc.JobID != "job-001" {
			t.Errorf("job_id = %q, want job-001", desc.JobID)
		}
		if desc.CorrelationID != "corr-001" {
			t.Errorf("correlation_id = %q, want corr-001", desc.CorrelationID)
		}
		if desc.SchemaVersion != defaultSchemaVersion {
			t.Errorf("schema_version = %q, want %q", desc.SchemaVersion, defaultSchemaVersion)
		}
		if desc.ContentHash == "" {
			t.Error("content_hash is empty")
		}
	}

	// Verify version status transitioned.
	if len(d.versionRepo.updatedVersions) != 1 {
		t.Fatalf("expected 1 version update, got %d", len(d.versionRepo.updatedVersions))
	}
	if d.versionRepo.updatedVersions[0].ArtifactStatus != model.ArtifactStatusProcessingArtifactsReceived {
		t.Errorf("status = %q, want PROCESSING_ARTIFACTS_RECEIVED",
			d.versionRepo.updatedVersions[0].ArtifactStatus)
	}

	// Verify 2 audit records: ARTIFACT_SAVED + ARTIFACT_STATUS_CHANGED.
	if got := len(d.auditRepo.inserted); got != 2 {
		t.Fatalf("expected 2 audit records, got %d", got)
	}
	if d.auditRepo.inserted[0].Action != model.AuditActionArtifactSaved {
		t.Errorf("audit[0].action = %q, want ARTIFACT_SAVED", d.auditRepo.inserted[0].Action)
	}
	if d.auditRepo.inserted[1].Action != model.AuditActionArtifactStatusChanged {
		t.Errorf("audit[1].action = %q, want ARTIFACT_STATUS_CHANGED", d.auditRepo.inserted[1].Action)
	}

	// Verify 2 outbox events: confirmation + notification.
	if got := len(d.outboxRepo.entries); got != 2 {
		t.Fatalf("expected 2 outbox entries, got %d", got)
	}
	if d.outboxRepo.entries[0].Topic != model.TopicDMResponsesArtifactsPersisted {
		t.Errorf("outbox[0].topic = %q, want %q",
			d.outboxRepo.entries[0].Topic, model.TopicDMResponsesArtifactsPersisted)
	}
	if d.outboxRepo.entries[1].Topic != model.TopicDMEventsVersionArtifactsReady {
		t.Errorf("outbox[1].topic = %q, want %q",
			d.outboxRepo.entries[1].Topic, model.TopicDMEventsVersionArtifactsReady)
	}

	// Verify correlation_id preserved in confirmation event.
	var confirmation model.DocumentProcessingArtifactsPersisted
	if err := json.Unmarshal(d.outboxRepo.entries[0].Payload, &confirmation); err != nil {
		t.Fatalf("unmarshal confirmation: %v", err)
	}
	if confirmation.CorrelationID != "corr-001" {
		t.Errorf("confirmation.correlation_id = %q, want corr-001", confirmation.CorrelationID)
	}

	// Verify notification carries artifact types.
	var notification model.VersionProcessingArtifactsReady
	if err := json.Unmarshal(d.outboxRepo.entries[1].Payload, &notification); err != nil {
		t.Fatalf("unmarshal notification: %v", err)
	}
	if len(notification.ArtifactTypes) != 5 {
		t.Errorf("notification.artifact_types length = %d, want 5", len(notification.ArtifactTypes))
	}
}

func TestHandleDPArtifacts_NoWarnings(t *testing.T) {
	d := newTestDeps()
	version := newTestVersion("org-001", "doc-001", "ver-001", model.ArtifactStatusPending)
	setupVersionFind(d, version)

	svc := d.newService()
	event := validDPEvent()
	event.Warnings = nil

	err := svc.HandleDPArtifacts(context.Background(), event)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 4 artifacts (no warnings).
	if got := len(d.objectStorage.putCalls); got != 4 {
		t.Fatalf("expected 4 put calls, got %d", got)
	}
	if got := len(d.artifactRepo.inserted); got != 4 {
		t.Fatalf("expected 4 descriptors, got %d", got)
	}
}

func TestHandleDPArtifacts_ValidationErrors(t *testing.T) {
	d := newTestDeps()
	svc := d.newService()

	tests := []struct {
		name  string
		event model.DocumentProcessingArtifactsReady
	}{
		{"empty org_id", func() model.DocumentProcessingArtifactsReady {
			e := validDPEvent()
			e.OrgID = ""
			return e
		}()},
		{"empty job_id", func() model.DocumentProcessingArtifactsReady {
			e := validDPEvent()
			e.JobID = ""
			return e
		}()},
		{"empty document_id", func() model.DocumentProcessingArtifactsReady {
			e := validDPEvent()
			e.DocumentID = ""
			return e
		}()},
		{"empty version_id", func() model.DocumentProcessingArtifactsReady {
			e := validDPEvent()
			e.VersionID = ""
			return e
		}()},
		{"no artifacts", func() model.DocumentProcessingArtifactsReady {
			e := validDPEvent()
			e.OCRRaw = nil
			e.Text = nil
			e.Structure = nil
			e.SemanticTree = nil
			e.Warnings = nil
			return e
		}()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := svc.HandleDPArtifacts(context.Background(), tt.event)
			if err == nil {
				t.Fatal("expected error")
			}
			if port.ErrorCode(err) != port.ErrCodeValidation {
				t.Errorf("error code = %q, want VALIDATION_ERROR", port.ErrorCode(err))
			}
		})
	}
}

func TestHandleDPArtifacts_VersionNotFound(t *testing.T) {
	d := newTestDeps()
	// Default mock returns not-found.
	svc := d.newService()

	err := svc.HandleDPArtifacts(context.Background(), validDPEvent())
	if err == nil {
		t.Fatal("expected error")
	}
	if port.ErrorCode(err) != port.ErrCodeVersionNotFound {
		t.Errorf("error code = %q, want VERSION_NOT_FOUND", port.ErrorCode(err))
	}

	// Blobs were uploaded before version check, then compensated after tx failure.
	if got := len(d.objectStorage.putCalls); got != 5 {
		t.Fatalf("expected 5 put calls before version check, got %d", got)
	}
	if got := len(d.objectStorage.deleteCalls); got != 5 {
		t.Fatalf("expected 5 delete calls for compensation, got %d", got)
	}
}

func TestHandleDPArtifacts_InvalidStatusTransition(t *testing.T) {
	d := newTestDeps()
	// Version already in FULLY_READY (terminal).
	version := newTestVersion("org-001", "doc-001", "ver-001", model.ArtifactStatusFullyReady)
	setupVersionFind(d, version)

	svc := d.newService()
	err := svc.HandleDPArtifacts(context.Background(), validDPEvent())
	if err == nil {
		t.Fatal("expected error")
	}
	if port.ErrorCode(err) != port.ErrCodeStatusTransition {
		t.Errorf("error code = %q, want INVALID_STATUS_TRANSITION", port.ErrorCode(err))
	}
}

func TestHandleDPArtifacts_ObjectStorageFailure_Compensation(t *testing.T) {
	d := newTestDeps()

	// Fail after 2 successful puts.
	storageErr := errors.New("S3 unavailable")
	d.objectStorage.putErrAfter = 2
	d.objectStorage.putErr = storageErr

	svc := d.newService()
	err := svc.HandleDPArtifacts(context.Background(), validDPEvent())
	if err == nil {
		t.Fatal("expected error")
	}
	if !port.IsRetryable(err) {
		t.Error("expected retryable error")
	}

	// 2 blobs were saved before failure.
	if got := len(d.objectStorage.putCalls); got != 2 {
		t.Fatalf("expected 2 put calls, got %d", got)
	}

	// Compensation: 2 blobs deleted.
	if got := len(d.objectStorage.deleteCalls); got != 2 {
		t.Fatalf("expected 2 delete calls for compensation, got %d", got)
	}
}

func TestHandleDPArtifacts_TransactionFailure(t *testing.T) {
	d := newTestDeps()
	version := newTestVersion("org-001", "doc-001", "ver-001", model.ArtifactStatusPending)
	setupVersionFind(d, version)

	dbErr := port.NewDatabaseError("connection lost", errors.New("EOF"))
	d.transactor.fn = func(ctx context.Context, fn func(ctx context.Context) error) error {
		return dbErr
	}

	svc := d.newService()
	err := svc.HandleDPArtifacts(context.Background(), validDPEvent())
	if err == nil {
		t.Fatal("expected error")
	}
	if port.ErrorCode(err) != port.ErrCodeDatabaseFailed {
		t.Errorf("error code = %q, want DATABASE_FAILED", port.ErrorCode(err))
	}

	// Blobs saved, then compensated after DB failure.
	if got := len(d.objectStorage.putCalls); got != 5 {
		t.Fatalf("expected 5 put calls, got %d", got)
	}
	if got := len(d.objectStorage.deleteCalls); got != 5 {
		t.Fatalf("expected 5 delete calls for compensation after DB error, got %d", got)
	}
}

func TestHandleDPArtifacts_ContextCancelled(t *testing.T) {
	d := newTestDeps()
	svc := d.newService()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := svc.HandleDPArtifacts(ctx, validDPEvent())
	if err == nil {
		t.Fatal("expected error")
	}
	if port.ErrorCode(err) != port.ErrCodeTimeout {
		t.Errorf("error code = %q, want TIMEOUT", port.ErrorCode(err))
	}
}

func TestHandleDPArtifacts_ContentHash(t *testing.T) {
	d := newTestDeps()
	version := newTestVersion("org-001", "doc-001", "ver-001", model.ArtifactStatusPending)
	setupVersionFind(d, version)

	svc := d.newService()
	event := validDPEvent()
	err := svc.HandleDPArtifacts(context.Background(), event)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify content hash of first artifact (OCR_RAW).
	expectedHash := sha256Hex([]byte(`{"pages":[]}`))
	if d.artifactRepo.inserted[0].ContentHash != expectedHash {
		t.Errorf("content_hash = %q, want %q", d.artifactRepo.inserted[0].ContentHash, expectedHash)
	}
}

func TestHandleDPArtifacts_ArtifactInsertError(t *testing.T) {
	d := newTestDeps()
	version := newTestVersion("org-001", "doc-001", "ver-001", model.ArtifactStatusPending)
	setupVersionFind(d, version)
	d.artifactRepo.insertErr = port.NewArtifactAlreadyExistsError("ver-001", "OCR_RAW")

	svc := d.newService()
	err := svc.HandleDPArtifacts(context.Background(), validDPEvent())
	if err == nil {
		t.Fatal("expected error")
	}
	if port.ErrorCode(err) != port.ErrCodeArtifactAlreadyExists {
		t.Errorf("error code = %q, want ARTIFACT_ALREADY_EXISTS", port.ErrorCode(err))
	}
}

func TestHandleDPArtifacts_OutboxWriteFailure(t *testing.T) {
	d := newTestDeps()
	version := newTestVersion("org-001", "doc-001", "ver-001", model.ArtifactStatusPending)
	setupVersionFind(d, version)
	d.outboxRepo.insertErr = port.NewDatabaseError("outbox insert failed", errors.New("pg: connection lost"))

	svc := d.newService()
	err := svc.HandleDPArtifacts(context.Background(), validDPEvent())
	if err == nil {
		t.Fatal("expected error")
	}
	if port.ErrorCode(err) != port.ErrCodeDatabaseFailed {
		t.Errorf("error code = %q, want DATABASE_FAILED", port.ErrorCode(err))
	}
}

func TestHandleDPArtifacts_AuditRepoFailure(t *testing.T) {
	d := newTestDeps()
	version := newTestVersion("org-001", "doc-001", "ver-001", model.ArtifactStatusPending)
	setupVersionFind(d, version)

	auditRepo := &mockAuditRepoWithErr{
		failAfter: 0, // fail on first insert
		err:       port.NewDatabaseError("audit insert failed", errors.New("connection reset")),
	}

	svc := NewArtifactIngestionService(
		d.transactor, d.versionRepo, d.artifactRepo,
		auditRepo, d.objectStorage, d.outboxWriter, d.logger,
	)
	uuidCounter := 0
	svc.newUUID = func() string {
		uuidCounter++
		return fmt.Sprintf("test-uuid-%03d", uuidCounter)
	}

	err := svc.HandleDPArtifacts(context.Background(), validDPEvent())
	if err == nil {
		t.Fatal("expected error")
	}
	if port.ErrorCode(err) != port.ErrCodeDatabaseFailed {
		t.Errorf("error code = %q, want DATABASE_FAILED", port.ErrorCode(err))
	}
}

func TestHandleDPArtifacts_VersionUpdateFailure(t *testing.T) {
	d := newTestDeps()
	version := newTestVersion("org-001", "doc-001", "ver-001", model.ArtifactStatusPending)
	setupVersionFind(d, version)
	d.versionRepo.update = func(ctx context.Context, v *model.DocumentVersion) error {
		return port.NewDatabaseError("version update failed", errors.New("deadlock"))
	}

	svc := d.newService()
	err := svc.HandleDPArtifacts(context.Background(), validDPEvent())
	if err == nil {
		t.Fatal("expected error")
	}
	if port.ErrorCode(err) != port.ErrCodeDatabaseFailed {
		t.Errorf("error code = %q, want DATABASE_FAILED", port.ErrorCode(err))
	}
}

// ---------------------------------------------------------------------------
// Tests: HandleLICArtifacts.
// ---------------------------------------------------------------------------

func TestHandleLICArtifacts_HappyPath(t *testing.T) {
	d := newTestDeps()
	version := newTestVersion("org-001", "doc-001", "ver-001", model.ArtifactStatusProcessingArtifactsReceived)
	setupVersionFind(d, version)

	svc := d.newService()
	err := svc.HandleLICArtifacts(context.Background(), validLICEvent())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify 8 blobs uploaded.
	if got := len(d.objectStorage.putCalls); got != 8 {
		t.Fatalf("expected 8 put calls, got %d", got)
	}

	// Verify 8 descriptors.
	if got := len(d.artifactRepo.inserted); got != 8 {
		t.Fatalf("expected 8 descriptors, got %d", got)
	}
	for _, desc := range d.artifactRepo.inserted {
		if desc.ProducerDomain != model.ProducerDomainLIC {
			t.Errorf("producer = %q, want LIC", desc.ProducerDomain)
		}
	}

	// Verify status → ANALYSIS_ARTIFACTS_RECEIVED.
	if d.versionRepo.updatedVersions[0].ArtifactStatus != model.ArtifactStatusAnalysisArtifactsReceived {
		t.Errorf("status = %q, want ANALYSIS_ARTIFACTS_RECEIVED",
			d.versionRepo.updatedVersions[0].ArtifactStatus)
	}

	// Verify outbox: confirmation + notification.
	if got := len(d.outboxRepo.entries); got != 2 {
		t.Fatalf("expected 2 outbox entries, got %d", got)
	}
	if d.outboxRepo.entries[0].Topic != model.TopicDMResponsesLICArtifactsPersisted {
		t.Errorf("outbox[0].topic = %q, want %q",
			d.outboxRepo.entries[0].Topic, model.TopicDMResponsesLICArtifactsPersisted)
	}
	if d.outboxRepo.entries[1].Topic != model.TopicDMEventsVersionAnalysisReady {
		t.Errorf("outbox[1].topic = %q, want %q",
			d.outboxRepo.entries[1].Topic, model.TopicDMEventsVersionAnalysisReady)
	}
}

func TestHandleLICArtifacts_InvalidStatusTransition(t *testing.T) {
	d := newTestDeps()
	// PENDING → ANALYSIS_ARTIFACTS_RECEIVED is not allowed.
	version := newTestVersion("org-001", "doc-001", "ver-001", model.ArtifactStatusPending)
	setupVersionFind(d, version)

	svc := d.newService()
	err := svc.HandleLICArtifacts(context.Background(), validLICEvent())
	if err == nil {
		t.Fatal("expected error")
	}
	if port.ErrorCode(err) != port.ErrCodeStatusTransition {
		t.Errorf("error code = %q, want INVALID_STATUS_TRANSITION", port.ErrorCode(err))
	}
}

func TestHandleLICArtifacts_ValidationError(t *testing.T) {
	d := newTestDeps()
	svc := d.newService()

	event := validLICEvent()
	event.JobID = ""

	err := svc.HandleLICArtifacts(context.Background(), event)
	if err == nil {
		t.Fatal("expected error")
	}
	if port.ErrorCode(err) != port.ErrCodeValidation {
		t.Errorf("error code = %q, want VALIDATION_ERROR", port.ErrorCode(err))
	}
}

// ---------------------------------------------------------------------------
// Tests: HandleREArtifacts.
// ---------------------------------------------------------------------------

func TestHandleREArtifacts_HappyPath_BothExports(t *testing.T) {
	d := newTestDeps()
	version := newTestVersion("org-001", "doc-001", "ver-001", model.ArtifactStatusAnalysisArtifactsReceived)
	setupVersionFind(d, version)

	event := validREEvent()
	// Register both blobs as existing.
	d.objectStorage.headResults[event.ExportPDF.StorageKey] = headResult{size: 1024, exists: true}
	d.objectStorage.headResults[event.ExportDOCX.StorageKey] = headResult{size: 2048, exists: true}

	svc := d.newService()
	err := svc.HandleREArtifacts(context.Background(), event)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// No PutObject calls (claim-check pattern).
	if got := len(d.objectStorage.putCalls); got != 0 {
		t.Fatalf("expected 0 put calls (claim-check), got %d", got)
	}

	// Verify 2 descriptors with correct storage keys.
	if got := len(d.artifactRepo.inserted); got != 2 {
		t.Fatalf("expected 2 descriptors, got %d", got)
	}
	if d.artifactRepo.inserted[0].StorageKey != "re/exports/report.pdf" {
		t.Errorf("descriptor[0].storage_key = %q, want re/exports/report.pdf", d.artifactRepo.inserted[0].StorageKey)
	}
	if d.artifactRepo.inserted[0].SizeBytes != 1024 {
		t.Errorf("descriptor[0].size_bytes = %d, want 1024", d.artifactRepo.inserted[0].SizeBytes)
	}
	if d.artifactRepo.inserted[0].ContentHash != "abc123" {
		t.Errorf("descriptor[0].content_hash = %q, want abc123", d.artifactRepo.inserted[0].ContentHash)
	}

	// Verify status → FULLY_READY.
	if d.versionRepo.updatedVersions[0].ArtifactStatus != model.ArtifactStatusFullyReady {
		t.Errorf("status = %q, want FULLY_READY",
			d.versionRepo.updatedVersions[0].ArtifactStatus)
	}

	// Verify outbox: confirmation + notification.
	if d.outboxRepo.entries[0].Topic != model.TopicDMResponsesREReportsPersisted {
		t.Errorf("outbox[0].topic = %q, want %q",
			d.outboxRepo.entries[0].Topic, model.TopicDMResponsesREReportsPersisted)
	}
	if d.outboxRepo.entries[1].Topic != model.TopicDMEventsVersionReportsReady {
		t.Errorf("outbox[1].topic = %q, want %q",
			d.outboxRepo.entries[1].Topic, model.TopicDMEventsVersionReportsReady)
	}
}

func TestHandleREArtifacts_HappyPath_OnlyPDF(t *testing.T) {
	d := newTestDeps()
	version := newTestVersion("org-001", "doc-001", "ver-001", model.ArtifactStatusAnalysisArtifactsReceived)
	setupVersionFind(d, version)

	event := validREEvent()
	event.ExportDOCX = nil
	d.objectStorage.headResults[event.ExportPDF.StorageKey] = headResult{size: 1024, exists: true}

	svc := d.newService()
	err := svc.HandleREArtifacts(context.Background(), event)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got := len(d.artifactRepo.inserted); got != 1 {
		t.Fatalf("expected 1 descriptor, got %d", got)
	}
	if d.artifactRepo.inserted[0].ArtifactType != model.ArtifactTypeExportPDF {
		t.Errorf("type = %q, want EXPORT_PDF", d.artifactRepo.inserted[0].ArtifactType)
	}
}

func TestHandleREArtifacts_NoExports(t *testing.T) {
	d := newTestDeps()
	svc := d.newService()

	event := validREEvent()
	event.ExportPDF = nil
	event.ExportDOCX = nil

	err := svc.HandleREArtifacts(context.Background(), event)
	if err == nil {
		t.Fatal("expected error")
	}
	if port.ErrorCode(err) != port.ErrCodeValidation {
		t.Errorf("error code = %q, want VALIDATION_ERROR", port.ErrorCode(err))
	}
}

func TestHandleREArtifacts_BlobNotFound(t *testing.T) {
	d := newTestDeps()

	event := validREEvent()
	event.ExportDOCX = nil
	// Don't register the PDF blob → HeadObject returns exists=false.

	svc := d.newService()
	err := svc.HandleREArtifacts(context.Background(), event)
	if err == nil {
		t.Fatal("expected error")
	}
	if port.ErrorCode(err) != port.ErrCodeStorageFailed {
		t.Errorf("error code = %q, want STORAGE_FAILED", port.ErrorCode(err))
	}
	if !strings.Contains(err.Error(), "claim-check blob not found") {
		t.Errorf("error message doesn't mention claim-check: %v", err)
	}
}

func TestHandleREArtifacts_HeadObjectError(t *testing.T) {
	d := newTestDeps()

	event := validREEvent()
	event.ExportDOCX = nil
	d.objectStorage.headResults[event.ExportPDF.StorageKey] = headResult{
		err: errors.New("network timeout"),
	}

	svc := d.newService()
	err := svc.HandleREArtifacts(context.Background(), event)
	if err == nil {
		t.Fatal("expected error")
	}
	if !port.IsRetryable(err) {
		t.Error("expected retryable error for HeadObject failure")
	}
}

func TestHandleREArtifacts_VersionNotFound(t *testing.T) {
	d := newTestDeps()

	event := validREEvent()
	d.objectStorage.headResults[event.ExportPDF.StorageKey] = headResult{size: 1024, exists: true}
	d.objectStorage.headResults[event.ExportDOCX.StorageKey] = headResult{size: 2048, exists: true}
	// Default mock returns version not found.

	svc := d.newService()
	err := svc.HandleREArtifacts(context.Background(), event)
	if err == nil {
		t.Fatal("expected error")
	}
	if port.ErrorCode(err) != port.ErrCodeVersionNotFound {
		t.Errorf("error code = %q, want VERSION_NOT_FOUND", port.ErrorCode(err))
	}

	// No compensation needed for claim-check (DM didn't upload anything).
	if got := len(d.objectStorage.deleteCalls); got != 0 {
		t.Errorf("expected 0 delete calls for RE claim-check, got %d", got)
	}
}

func TestHandleREArtifacts_InvalidStatusTransition(t *testing.T) {
	d := newTestDeps()
	// PENDING → FULLY_READY is not valid (must go through intermediate states).
	version := newTestVersion("org-001", "doc-001", "ver-001", model.ArtifactStatusPending)
	setupVersionFind(d, version)

	event := validREEvent()
	d.objectStorage.headResults[event.ExportPDF.StorageKey] = headResult{size: 1024, exists: true}
	d.objectStorage.headResults[event.ExportDOCX.StorageKey] = headResult{size: 2048, exists: true}

	svc := d.newService()
	err := svc.HandleREArtifacts(context.Background(), event)
	if err == nil {
		t.Fatal("expected error")
	}
	if port.ErrorCode(err) != port.ErrCodeStatusTransition {
		t.Errorf("error code = %q, want INVALID_STATUS_TRANSITION", port.ErrorCode(err))
	}
}

// ---------------------------------------------------------------------------
// Tests: Outbox aggregate_id.
// ---------------------------------------------------------------------------

func TestOutboxAggregateID_IsVersionID(t *testing.T) {
	d := newTestDeps()
	version := newTestVersion("org-001", "doc-001", "ver-001", model.ArtifactStatusPending)
	setupVersionFind(d, version)

	svc := d.newService()
	err := svc.HandleDPArtifacts(context.Background(), validDPEvent())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, entry := range d.outboxRepo.entries {
		if entry.AggregateID != "ver-001" {
			t.Errorf("outbox aggregate_id = %q, want ver-001 (REV-010)", entry.AggregateID)
		}
	}
}

// ---------------------------------------------------------------------------
// Tests: Audit record details.
// ---------------------------------------------------------------------------

func TestAuditRecordDetails(t *testing.T) {
	d := newTestDeps()
	version := newTestVersion("org-001", "doc-001", "ver-001", model.ArtifactStatusPending)
	setupVersionFind(d, version)

	svc := d.newService()
	err := svc.HandleDPArtifacts(context.Background(), validDPEvent())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Check ARTIFACT_SAVED audit details.
	var savedDetails map[string]any
	if err := json.Unmarshal(d.auditRepo.inserted[0].Details, &savedDetails); err != nil {
		t.Fatalf("unmarshal saved details: %v", err)
	}
	if savedDetails["producer"] != "DP" {
		t.Errorf("producer = %v, want DP", savedDetails["producer"])
	}
	count, ok := savedDetails["artifact_count"].(float64)
	if !ok || int(count) != 5 {
		t.Errorf("artifact_count = %v, want 5", savedDetails["artifact_count"])
	}

	// Check ARTIFACT_STATUS_CHANGED audit details.
	var statusDetails map[string]any
	if err := json.Unmarshal(d.auditRepo.inserted[1].Details, &statusDetails); err != nil {
		t.Fatalf("unmarshal status details: %v", err)
	}
	if statusDetails["from"] != "PENDING" {
		t.Errorf("from = %v, want PENDING", statusDetails["from"])
	}
	if statusDetails["to"] != "PROCESSING_ARTIFACTS_RECEIVED" {
		t.Errorf("to = %v, want PROCESSING_ARTIFACTS_RECEIVED", statusDetails["to"])
	}

	// Verify actor is domain (DP).
	if d.auditRepo.inserted[0].ActorType != model.ActorTypeDomain {
		t.Errorf("actor_type = %q, want DOMAIN", d.auditRepo.inserted[0].ActorType)
	}
	if d.auditRepo.inserted[0].ActorID != "DP" {
		t.Errorf("actor_id = %q, want DP", d.auditRepo.inserted[0].ActorID)
	}
}

// ---------------------------------------------------------------------------
// Tests: Extract helpers.
// ---------------------------------------------------------------------------

func TestExtractDPArtifacts_AllPresent(t *testing.T) {
	event := validDPEvent()
	items := extractDPArtifacts(event)
	if len(items) != 5 {
		t.Fatalf("expected 5, got %d", len(items))
	}

	expected := []model.ArtifactType{
		model.ArtifactTypeOCRRaw,
		model.ArtifactTypeExtractedText,
		model.ArtifactTypeDocumentStructure,
		model.ArtifactTypeSemanticTree,
		model.ArtifactTypeProcessingWarnings,
	}
	for i, item := range items {
		if item.artifactType != expected[i] {
			t.Errorf("item[%d].type = %q, want %q", i, item.artifactType, expected[i])
		}
	}
}

func TestExtractLICArtifacts_AllPresent(t *testing.T) {
	event := validLICEvent()
	items := extractLICArtifacts(event)
	if len(items) != 8 {
		t.Fatalf("expected 8, got %d", len(items))
	}
}

func TestExtractREArtifacts_BothPresent(t *testing.T) {
	event := validREEvent()
	items := extractREArtifacts(event)
	if len(items) != 2 {
		t.Fatalf("expected 2, got %d", len(items))
	}
	if items[0].blobRef == nil || items[1].blobRef == nil {
		t.Error("expected blobRef to be set for RE artifacts")
	}
}

func TestExtractREArtifacts_NilEvent(t *testing.T) {
	event := model.ReportsArtifactsReady{}
	items := extractREArtifacts(event)
	if len(items) != 0 {
		t.Fatalf("expected 0, got %d", len(items))
	}
}

// ---------------------------------------------------------------------------
// Tests: SHA-256 helper.
// ---------------------------------------------------------------------------

func TestSha256Hex(t *testing.T) {
	hash := sha256Hex([]byte("hello"))
	// SHA-256 of "hello" = 2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824
	expected := "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"
	if hash != expected {
		t.Errorf("sha256Hex(hello) = %q, want %q", hash, expected)
	}
}
