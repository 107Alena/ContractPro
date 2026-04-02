package diff

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"contractpro/document-management/internal/domain/model"
	"contractpro/document-management/internal/domain/port"
	"contractpro/document-management/internal/egress/outbox"
	"contractpro/document-management/internal/infra/objectstorage"
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
	findByIDCallCount int
}

func (m *mockVersionRepo) FindByID(ctx context.Context, orgID, docID, versionID string) (*model.DocumentVersion, error) {
	m.findByIDCallCount++
	if m.findByID != nil {
		return m.findByID(ctx, orgID, docID, versionID)
	}
	return &model.DocumentVersion{
		VersionID:      versionID,
		DocumentID:     docID,
		OrganizationID: orgID,
		VersionNumber:  1,
		ArtifactStatus: model.ArtifactStatusPending,
	}, nil
}

func (m *mockVersionRepo) Insert(context.Context, *model.DocumentVersion) error {
	panic("not used in diff")
}
func (m *mockVersionRepo) List(context.Context, string, string, int, int) ([]*model.DocumentVersion, int, error) {
	panic("not used in diff")
}
func (m *mockVersionRepo) Update(context.Context, *model.DocumentVersion) error {
	panic("not used in diff")
}
func (m *mockVersionRepo) NextVersionNumber(context.Context, string, string) (int, error) {
	panic("not used in diff")
}

type mockDiffRepo struct {
	inserted  []*model.VersionDiffReference
	insertErr error
	findFn    func(ctx context.Context, orgID, docID, baseVersionID, targetVersionID string) (*model.VersionDiffReference, error)
}

func (m *mockDiffRepo) Insert(ctx context.Context, ref *model.VersionDiffReference) error {
	if m.insertErr != nil {
		return m.insertErr
	}
	m.inserted = append(m.inserted, ref)
	return nil
}

func (m *mockDiffRepo) FindByVersionPair(ctx context.Context, orgID, docID, baseVersionID, targetVersionID string) (*model.VersionDiffReference, error) {
	if m.findFn != nil {
		return m.findFn(ctx, orgID, docID, baseVersionID, targetVersionID)
	}
	return nil, port.NewDiffNotFoundError(baseVersionID, targetVersionID)
}

func (m *mockDiffRepo) ListByDocument(context.Context, string, string) ([]*model.VersionDiffReference, error) {
	panic("not used in diff service")
}
func (m *mockDiffRepo) DeleteByDocument(context.Context, string, string) error {
	panic("not used in diff service")
}

type mockAuditRepo struct {
	inserted []*model.AuditRecord
	err      error
}

func (m *mockAuditRepo) Insert(ctx context.Context, r *model.AuditRecord) error {
	if m.err != nil {
		return m.err
	}
	m.inserted = append(m.inserted, r)
	return nil
}

func (m *mockAuditRepo) List(context.Context, port.AuditListParams) ([]*model.AuditRecord, int, error) {
	panic("not used in diff service")
}

type mockObjectStorage struct {
	putCalls    []putCall
	putErr      error
	getCalls    []string
	getContent  map[string][]byte
	getErr      error
	deleteCalls []string
}

type putCall struct {
	key         string
	contentType string
	data        []byte
}

func newMockObjectStorage() *mockObjectStorage {
	return &mockObjectStorage{
		getContent: make(map[string][]byte),
	}
}

func (m *mockObjectStorage) PutObject(ctx context.Context, key string, data io.Reader, contentType string) error {
	if m.putErr != nil {
		return m.putErr
	}
	b, _ := io.ReadAll(data)
	m.putCalls = append(m.putCalls, putCall{key: key, contentType: contentType, data: b})
	return nil
}

func (m *mockObjectStorage) GetObject(ctx context.Context, key string) (io.ReadCloser, error) {
	m.getCalls = append(m.getCalls, key)
	if m.getErr != nil {
		return nil, m.getErr
	}
	content, ok := m.getContent[key]
	if !ok {
		return nil, port.NewStorageError("not found", nil)
	}
	return io.NopCloser(strings.NewReader(string(content))), nil
}

func (m *mockObjectStorage) DeleteObject(ctx context.Context, key string) error {
	m.deleteCalls = append(m.deleteCalls, key)
	return nil
}

func (m *mockObjectStorage) HeadObject(context.Context, string) (int64, bool, error) {
	panic("not used in diff service")
}
func (m *mockObjectStorage) GeneratePresignedURL(context.Context, string, time.Duration) (string, error) {
	panic("not used in diff service")
}
func (m *mockObjectStorage) DeleteByPrefix(context.Context, string) error {
	panic("not used in diff service")
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
	diffRepo      *mockDiffRepo
	auditRepo     *mockAuditRepo
	objectStorage *mockObjectStorage
	outboxRepo       *mockOutboxRepo
	outboxWriter     *outbox.OutboxWriter
	fallbackResolver *mockFallbackResolver
	logger           *mockLogger
}

type mockFallbackResolver struct {
	orgID     string
	versionID string
	err       error
	callCount int
}

func (m *mockFallbackResolver) ResolveByDocumentID(_ context.Context, _ string) (string, string, error) {
	m.callCount++
	return m.orgID, m.versionID, m.err
}

func newTestDeps() *testDeps {
	outboxRepo := &mockOutboxRepo{}
	return &testDeps{
		transactor:       &mockTransactor{},
		versionRepo:      &mockVersionRepo{},
		diffRepo:         &mockDiffRepo{},
		auditRepo:        &mockAuditRepo{},
		objectStorage:    newMockObjectStorage(),
		outboxRepo:       outboxRepo,
		outboxWriter:     outbox.NewOutboxWriter(outboxRepo),
		fallbackResolver: &mockFallbackResolver{orgID: "org-1", versionID: "ver-1"},
		logger:           &mockLogger{},
	}
}

func (d *testDeps) newService() *DiffStorageService {
	svc := NewDiffStorageService(
		d.transactor, d.versionRepo, d.diffRepo, d.auditRepo,
		d.objectStorage, d.outboxWriter, d.fallbackResolver, d.logger,
	)
	svc.newUUID = func() string { return "test-uuid" }
	return svc
}

func validDiffEvent() model.DocumentVersionDiffReady {
	return model.DocumentVersionDiffReady{
		EventMeta: model.EventMeta{
			CorrelationID: "corr-123",
			Timestamp:     time.Now().UTC(),
		},
		JobID:               "job-1",
		DocumentID:          "doc-1",
		BaseVersionID:       "ver-1",
		TargetVersionID:     "ver-2",
		OrgID:               "org-1",
		TextDiffs:           json.RawMessage(`[{"from":"a","to":"b"}]`),
		StructuralDiffs:     json.RawMessage(`[{"type":"added"}]`),
		TextDiffCount:       1,
		StructuralDiffCount: 1,
	}
}

// ---------------------------------------------------------------------------
// Constructor tests.
// ---------------------------------------------------------------------------

func TestNewDiffStorageService_PanicsOnNilDeps(t *testing.T) {
	t.Parallel()

	deps := newTestDeps()

	tests := []struct {
		name    string
		setup   func() *DiffStorageService
		wantMsg string
	}{
		{
			name:    "nil transactor",
			setup:   func() *DiffStorageService { return NewDiffStorageService(nil, deps.versionRepo, deps.diffRepo, deps.auditRepo, deps.objectStorage, deps.outboxWriter, deps.fallbackResolver, deps.logger) },
			wantMsg: "transactor must not be nil",
		},
		{
			name:    "nil versionRepo",
			setup:   func() *DiffStorageService { return NewDiffStorageService(deps.transactor, nil, deps.diffRepo, deps.auditRepo, deps.objectStorage, deps.outboxWriter, deps.fallbackResolver, deps.logger) },
			wantMsg: "versionRepo must not be nil",
		},
		{
			name:    "nil diffRepo",
			setup:   func() *DiffStorageService { return NewDiffStorageService(deps.transactor, deps.versionRepo, nil, deps.auditRepo, deps.objectStorage, deps.outboxWriter, deps.fallbackResolver, deps.logger) },
			wantMsg: "diffRepo must not be nil",
		},
		{
			name:    "nil auditRepo",
			setup:   func() *DiffStorageService { return NewDiffStorageService(deps.transactor, deps.versionRepo, deps.diffRepo, nil, deps.objectStorage, deps.outboxWriter, deps.fallbackResolver, deps.logger) },
			wantMsg: "auditRepo must not be nil",
		},
		{
			name:    "nil objectStorage",
			setup:   func() *DiffStorageService { return NewDiffStorageService(deps.transactor, deps.versionRepo, deps.diffRepo, deps.auditRepo, nil, deps.outboxWriter, deps.fallbackResolver, deps.logger) },
			wantMsg: "objectStorage must not be nil",
		},
		{
			name:    "nil outboxWriter",
			setup:   func() *DiffStorageService { return NewDiffStorageService(deps.transactor, deps.versionRepo, deps.diffRepo, deps.auditRepo, deps.objectStorage, nil, deps.fallbackResolver, deps.logger) },
			wantMsg: "outboxWriter must not be nil",
		},
		{
			name:    "nil fallbackResolver",
			setup:   func() *DiffStorageService { return NewDiffStorageService(deps.transactor, deps.versionRepo, deps.diffRepo, deps.auditRepo, deps.objectStorage, deps.outboxWriter, nil, deps.logger) },
			wantMsg: "fallbackResolver must not be nil",
		},
		{
			name:    "nil logger",
			setup:   func() *DiffStorageService { return NewDiffStorageService(deps.transactor, deps.versionRepo, deps.diffRepo, deps.auditRepo, deps.objectStorage, deps.outboxWriter, deps.fallbackResolver, nil) },
			wantMsg: "logger must not be nil",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			defer func() {
				r := recover()
				if r == nil {
					t.Fatal("expected panic")
				}
				msg, ok := r.(string)
				if !ok || !strings.Contains(msg, tt.wantMsg) {
					t.Fatalf("unexpected panic message: %v", r)
				}
			}()
			tt.setup()
		})
	}
}

// ---------------------------------------------------------------------------
// HandleDiffReady tests.
// ---------------------------------------------------------------------------

func TestHandleDiffReady_HappyPath(t *testing.T) {
	t.Parallel()
	deps := newTestDeps()
	svc := deps.newService()

	event := validDiffEvent()
	err := svc.HandleDiffReady(context.Background(), event)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify blob uploaded to correct key.
	expectedKey := objectstorage.DiffKey("org-1", "doc-1", "ver-1", "ver-2")
	if len(deps.objectStorage.putCalls) != 1 {
		t.Fatalf("expected 1 put call, got %d", len(deps.objectStorage.putCalls))
	}
	put := deps.objectStorage.putCalls[0]
	if put.key != expectedKey {
		t.Errorf("put key = %q, want %q", put.key, expectedKey)
	}
	if put.contentType != objectstorage.ContentTypeJSON {
		t.Errorf("content type = %q, want %q", put.contentType, objectstorage.ContentTypeJSON)
	}

	// Verify blob content is merged diffs.
	var blob diffBlob
	if err := json.Unmarshal(put.data, &blob); err != nil {
		t.Fatalf("unmarshal blob: %v", err)
	}
	if string(blob.TextDiffs) != `[{"from":"a","to":"b"}]` {
		t.Errorf("text_diffs = %s", blob.TextDiffs)
	}
	if string(blob.StructuralDiffs) != `[{"type":"added"}]` {
		t.Errorf("structural_diffs = %s", blob.StructuralDiffs)
	}

	// Verify diff reference inserted.
	if len(deps.diffRepo.inserted) != 1 {
		t.Fatalf("expected 1 diff reference, got %d", len(deps.diffRepo.inserted))
	}
	ref := deps.diffRepo.inserted[0]
	if ref.DocumentID != "doc-1" {
		t.Errorf("ref.DocumentID = %q", ref.DocumentID)
	}
	if ref.BaseVersionID != "ver-1" {
		t.Errorf("ref.BaseVersionID = %q", ref.BaseVersionID)
	}
	if ref.TargetVersionID != "ver-2" {
		t.Errorf("ref.TargetVersionID = %q", ref.TargetVersionID)
	}
	if ref.StorageKey != expectedKey {
		t.Errorf("ref.StorageKey = %q", ref.StorageKey)
	}
	if ref.TextDiffCount != 1 {
		t.Errorf("ref.TextDiffCount = %d", ref.TextDiffCount)
	}
	if ref.StructuralDiffCount != 1 {
		t.Errorf("ref.StructuralDiffCount = %d", ref.StructuralDiffCount)
	}
	if ref.JobID != "job-1" {
		t.Errorf("ref.JobID = %q", ref.JobID)
	}

	// Verify audit record.
	if len(deps.auditRepo.inserted) != 1 {
		t.Fatalf("expected 1 audit record, got %d", len(deps.auditRepo.inserted))
	}
	audit := deps.auditRepo.inserted[0]
	if audit.Action != model.AuditActionDiffSaved {
		t.Errorf("audit.Action = %q", audit.Action)
	}
	if audit.ActorType != model.ActorTypeDomain {
		t.Errorf("audit.ActorType = %q", audit.ActorType)
	}
	if audit.ActorID != string(model.ProducerDomainDP) {
		t.Errorf("audit.ActorID = %q", audit.ActorID)
	}
	if audit.DocumentID != "doc-1" {
		t.Errorf("audit.DocumentID = %q", audit.DocumentID)
	}
	if audit.VersionID != "ver-2" {
		t.Errorf("audit.VersionID = %q (should be target version)", audit.VersionID)
	}

	// Verify outbox entry (DiffPersisted).
	if len(deps.outboxRepo.entries) != 1 {
		t.Fatalf("expected 1 outbox entry, got %d", len(deps.outboxRepo.entries))
	}
	entry := deps.outboxRepo.entries[0]
	if entry.Topic != model.TopicDMResponsesDiffPersisted {
		t.Errorf("outbox topic = %q", entry.Topic)
	}
	if entry.AggregateID != "ver-2" {
		t.Errorf("outbox aggregate_id = %q, want ver-2", entry.AggregateID)
	}

	// Verify payload has correct job_id and document_id.
	var persisted model.DocumentVersionDiffPersisted
	if err := json.Unmarshal(entry.Payload, &persisted); err != nil {
		t.Fatalf("unmarshal outbox payload: %v", err)
	}
	if persisted.JobID != "job-1" {
		t.Errorf("persisted.JobID = %q", persisted.JobID)
	}
	if persisted.DocumentID != "doc-1" {
		t.Errorf("persisted.DocumentID = %q", persisted.DocumentID)
	}
	if persisted.CorrelationID != "corr-123" {
		t.Errorf("persisted.CorrelationID = %q", persisted.CorrelationID)
	}

	// Verify both versions validated (2 FindByID calls).
	if deps.versionRepo.findByIDCallCount != 2 {
		t.Errorf("FindByID calls = %d, want 2", deps.versionRepo.findByIDCallCount)
	}

	// Verify no compensation.
	if len(deps.objectStorage.deleteCalls) != 0 {
		t.Errorf("unexpected delete calls: %v", deps.objectStorage.deleteCalls)
	}
}

func TestHandleDiffReady_NilDiffs(t *testing.T) {
	t.Parallel()
	deps := newTestDeps()
	svc := deps.newService()

	event := validDiffEvent()
	event.TextDiffs = nil
	event.StructuralDiffs = nil

	err := svc.HandleDiffReady(context.Background(), event)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify ensureJSONArray produced empty arrays.
	var blob diffBlob
	if err := json.Unmarshal(deps.objectStorage.putCalls[0].data, &blob); err != nil {
		t.Fatalf("unmarshal blob: %v", err)
	}
	if string(blob.TextDiffs) != "[]" {
		t.Errorf("text_diffs = %s, want []", blob.TextDiffs)
	}
	if string(blob.StructuralDiffs) != "[]" {
		t.Errorf("structural_diffs = %s, want []", blob.StructuralDiffs)
	}
}

func TestHandleDiffReady_ValidationErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		mutate  func(*model.DocumentVersionDiffReady)
		wantMsg string
	}{
		{"empty job_id", func(e *model.DocumentVersionDiffReady) { e.JobID = "" }, "job_id is required"},
		{"empty document_id", func(e *model.DocumentVersionDiffReady) { e.DocumentID = "" }, "document_id is required"},
		{"empty base_version_id", func(e *model.DocumentVersionDiffReady) { e.BaseVersionID = "" }, "base_version_id is required"},
		{"empty target_version_id", func(e *model.DocumentVersionDiffReady) { e.TargetVersionID = "" }, "target_version_id is required"},
		{"same base and target", func(e *model.DocumentVersionDiffReady) { e.BaseVersionID = "ver-1"; e.TargetVersionID = "ver-1" }, "must differ"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			deps := newTestDeps()
			svc := deps.newService()

			event := validDiffEvent()
			tt.mutate(&event)

			err := svc.HandleDiffReady(context.Background(), event)
			if err == nil {
				t.Fatal("expected error")
			}
			if port.ErrorCode(err) != port.ErrCodeValidation {
				t.Errorf("error code = %q, want %q", port.ErrorCode(err), port.ErrCodeValidation)
			}
			if !strings.Contains(err.Error(), tt.wantMsg) {
				t.Errorf("error = %q, want to contain %q", err.Error(), tt.wantMsg)
			}
		})
	}
}

func TestHandleDiffReady_BaseVersionNotFound(t *testing.T) {
	t.Parallel()
	deps := newTestDeps()
	deps.versionRepo.findByID = func(ctx context.Context, orgID, docID, versionID string) (*model.DocumentVersion, error) {
		if versionID == "ver-1" {
			return nil, port.NewVersionNotFoundError("ver-1")
		}
		return &model.DocumentVersion{VersionID: versionID}, nil
	}
	svc := deps.newService()

	err := svc.HandleDiffReady(context.Background(), validDiffEvent())
	if err == nil {
		t.Fatal("expected error")
	}
	if port.ErrorCode(err) != port.ErrCodeVersionNotFound {
		t.Errorf("error code = %q, want VERSION_NOT_FOUND", port.ErrorCode(err))
	}
	// No blob should be uploaded.
	if len(deps.objectStorage.putCalls) != 0 {
		t.Errorf("unexpected put calls: %d", len(deps.objectStorage.putCalls))
	}
}

func TestHandleDiffReady_TargetVersionNotFound(t *testing.T) {
	t.Parallel()
	deps := newTestDeps()
	deps.versionRepo.findByID = func(ctx context.Context, orgID, docID, versionID string) (*model.DocumentVersion, error) {
		if versionID == "ver-2" {
			return nil, port.NewVersionNotFoundError("ver-2")
		}
		return &model.DocumentVersion{VersionID: versionID}, nil
	}
	svc := deps.newService()

	err := svc.HandleDiffReady(context.Background(), validDiffEvent())
	if err == nil {
		t.Fatal("expected error")
	}
	if port.ErrorCode(err) != port.ErrCodeVersionNotFound {
		t.Errorf("error code = %q, want VERSION_NOT_FOUND", port.ErrorCode(err))
	}
	// No blob should be uploaded.
	if len(deps.objectStorage.putCalls) != 0 {
		t.Errorf("unexpected put calls: %d", len(deps.objectStorage.putCalls))
	}
}

func TestHandleDiffReady_PutObjectFailure(t *testing.T) {
	t.Parallel()
	deps := newTestDeps()
	deps.objectStorage.putErr = errors.New("s3 down")
	svc := deps.newService()

	err := svc.HandleDiffReady(context.Background(), validDiffEvent())
	if err == nil {
		t.Fatal("expected error")
	}
	if port.ErrorCode(err) != port.ErrCodeStorageFailed {
		t.Errorf("error code = %q, want STORAGE_FAILED", port.ErrorCode(err))
	}
	// No diff reference or audit should be inserted.
	if len(deps.diffRepo.inserted) != 0 {
		t.Errorf("unexpected diff inserts: %d", len(deps.diffRepo.inserted))
	}
}

func TestHandleDiffReady_TransactionFailure_Compensation(t *testing.T) {
	t.Parallel()
	deps := newTestDeps()
	deps.diffRepo.insertErr = port.NewDatabaseError("db error", errors.New("connection reset"))
	svc := deps.newService()

	err := svc.HandleDiffReady(context.Background(), validDiffEvent())
	if err == nil {
		t.Fatal("expected error")
	}

	// Blob should have been uploaded.
	if len(deps.objectStorage.putCalls) != 1 {
		t.Fatalf("expected 1 put call, got %d", len(deps.objectStorage.putCalls))
	}

	// Compensation: blob should be deleted.
	expectedKey := objectstorage.DiffKey("org-1", "doc-1", "ver-1", "ver-2")
	if len(deps.objectStorage.deleteCalls) != 1 {
		t.Fatalf("expected 1 delete call, got %d", len(deps.objectStorage.deleteCalls))
	}
	if deps.objectStorage.deleteCalls[0] != expectedKey {
		t.Errorf("delete key = %q, want %q", deps.objectStorage.deleteCalls[0], expectedKey)
	}

	// Error log should exist.
	hasErrorLog := false
	for _, msg := range deps.logger.messages {
		if msg.level == "ERROR" && strings.Contains(msg.msg, "transaction failed") {
			hasErrorLog = true
			break
		}
	}
	if !hasErrorLog {
		t.Error("expected ERROR log about transaction failure")
	}
}

func TestHandleDiffReady_Idempotency_DiffAlreadyExists(t *testing.T) {
	t.Parallel()
	deps := newTestDeps()
	// Pre-check finds existing diff → skip blob upload.
	deps.diffRepo.findFn = func(ctx context.Context, orgID, docID, baseVersionID, targetVersionID string) (*model.VersionDiffReference, error) {
		return &model.VersionDiffReference{
			DiffID:          "existing-diff",
			DocumentID:      docID,
			OrganizationID:  orgID,
			BaseVersionID:   baseVersionID,
			TargetVersionID: targetVersionID,
			StorageKey:      "org-1/doc-1/diffs/ver-1_ver-2",
			JobID:           "old-job",
		}, nil
	}
	svc := deps.newService()

	event := validDiffEvent()
	err := svc.HandleDiffReady(context.Background(), event)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// No blob should be uploaded (pre-check short-circuits).
	if len(deps.objectStorage.putCalls) != 0 {
		t.Errorf("unexpected put calls: %d (blob upload should be skipped)", len(deps.objectStorage.putCalls))
	}

	// No audit record should be inserted (skip audit for duplicate).
	if len(deps.auditRepo.inserted) != 0 {
		t.Errorf("unexpected audit inserts: %d", len(deps.auditRepo.inserted))
	}

	// No diff reference should be inserted.
	if len(deps.diffRepo.inserted) != 0 {
		t.Errorf("unexpected diff inserts: %d", len(deps.diffRepo.inserted))
	}

	// DiffPersisted should still be written to outbox for current job_id.
	if len(deps.outboxRepo.entries) != 1 {
		t.Fatalf("expected 1 outbox entry, got %d", len(deps.outboxRepo.entries))
	}
	entry := deps.outboxRepo.entries[0]
	if entry.Topic != model.TopicDMResponsesDiffPersisted {
		t.Errorf("outbox topic = %q", entry.Topic)
	}

	var persisted model.DocumentVersionDiffPersisted
	if err := json.Unmarshal(entry.Payload, &persisted); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if persisted.JobID != "job-1" {
		t.Errorf("persisted.JobID = %q, want job-1", persisted.JobID)
	}
	if persisted.CorrelationID != "corr-123" {
		t.Errorf("persisted.CorrelationID = %q, want corr-123", persisted.CorrelationID)
	}

	// Info log about duplicate.
	hasDupLog := false
	for _, msg := range deps.logger.messages {
		if msg.level == "INFO" && strings.Contains(msg.msg, "already exists") {
			hasDupLog = true
			break
		}
	}
	if !hasDupLog {
		t.Error("expected INFO log about existing diff")
	}

	// No compensation needed.
	if len(deps.objectStorage.deleteCalls) != 0 {
		t.Errorf("unexpected delete calls: %v", deps.objectStorage.deleteCalls)
	}
}

func TestHandleDiffReady_AuditInsertFailure(t *testing.T) {
	t.Parallel()
	deps := newTestDeps()
	deps.auditRepo.err = port.NewDatabaseError("audit failed", errors.New("disk full"))
	svc := deps.newService()

	err := svc.HandleDiffReady(context.Background(), validDiffEvent())
	if err == nil {
		t.Fatal("expected error")
	}
	if port.ErrorCode(err) != port.ErrCodeDatabaseFailed {
		t.Errorf("error code = %q, want DATABASE_FAILED", port.ErrorCode(err))
	}

	// Compensation should occur.
	if len(deps.objectStorage.deleteCalls) != 1 {
		t.Errorf("expected 1 compensation delete, got %d", len(deps.objectStorage.deleteCalls))
	}
}

func TestHandleDiffReady_OutboxWriteFailure(t *testing.T) {
	t.Parallel()
	deps := newTestDeps()
	deps.outboxRepo.insertErr = port.NewDatabaseError("outbox failed", errors.New("constraint"))
	svc := deps.newService()

	err := svc.HandleDiffReady(context.Background(), validDiffEvent())
	if err == nil {
		t.Fatal("expected error")
	}

	// Compensation should occur.
	if len(deps.objectStorage.deleteCalls) != 1 {
		t.Errorf("expected 1 compensation delete, got %d", len(deps.objectStorage.deleteCalls))
	}
}

func TestHandleDiffReady_ContextCancelled(t *testing.T) {
	t.Parallel()
	deps := newTestDeps()
	svc := deps.newService()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := svc.HandleDiffReady(ctx, validDiffEvent())
	if err == nil {
		t.Fatal("expected error")
	}
	if port.ErrorCode(err) != port.ErrCodeTimeout {
		t.Errorf("error code = %q, want TIMEOUT", port.ErrorCode(err))
	}

	// No interactions.
	if deps.versionRepo.findByIDCallCount != 0 {
		t.Errorf("unexpected FindByID calls: %d", deps.versionRepo.findByIDCallCount)
	}
}

func TestHandleDiffReady_OutboxAggregateID(t *testing.T) {
	t.Parallel()
	deps := newTestDeps()
	svc := deps.newService()

	event := validDiffEvent()
	event.TargetVersionID = "target-ver-custom"

	if err := svc.HandleDiffReady(context.Background(), event); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(deps.outboxRepo.entries) != 1 {
		t.Fatalf("expected 1 outbox entry, got %d", len(deps.outboxRepo.entries))
	}
	if deps.outboxRepo.entries[0].AggregateID != "target-ver-custom" {
		t.Errorf("aggregate_id = %q, want target-ver-custom", deps.outboxRepo.entries[0].AggregateID)
	}
}

func TestHandleDiffReady_AuditDetailsContent(t *testing.T) {
	t.Parallel()
	deps := newTestDeps()
	svc := deps.newService()

	event := validDiffEvent()
	event.TextDiffCount = 5
	event.StructuralDiffCount = 3

	if err := svc.HandleDiffReady(context.Background(), event); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(deps.auditRepo.inserted) != 1 {
		t.Fatalf("expected 1 audit record, got %d", len(deps.auditRepo.inserted))
	}
	audit := deps.auditRepo.inserted[0]

	var details map[string]any
	if err := json.Unmarshal(audit.Details, &details); err != nil {
		t.Fatalf("unmarshal details: %v", err)
	}
	if int(details["text_diff_count"].(float64)) != 5 {
		t.Errorf("text_diff_count = %v", details["text_diff_count"])
	}
	if int(details["structural_diff_count"].(float64)) != 3 {
		t.Errorf("structural_diff_count = %v", details["structural_diff_count"])
	}
	if _, ok := details["content_hash"]; !ok {
		t.Error("missing content_hash in details")
	}
	if _, ok := details["storage_key"]; !ok {
		t.Error("missing storage_key in details")
	}
}

func TestHandleDiffReady_StorageKeyFormat(t *testing.T) {
	t.Parallel()
	deps := newTestDeps()
	svc := deps.newService()

	if err := svc.HandleDiffReady(context.Background(), validDiffEvent()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expectedKey := "org-1/doc-1/diffs/ver-1_ver-2"
	if deps.objectStorage.putCalls[0].key != expectedKey {
		t.Errorf("storage key = %q, want %q", deps.objectStorage.putCalls[0].key, expectedKey)
	}
}

func TestHandleDiffReady_DiffInsertNonConflictError(t *testing.T) {
	t.Parallel()
	deps := newTestDeps()
	deps.diffRepo.insertErr = port.NewDatabaseError("unexpected error", errors.New("connection lost"))
	svc := deps.newService()

	err := svc.HandleDiffReady(context.Background(), validDiffEvent())
	if err == nil {
		t.Fatal("expected error")
	}
	if port.ErrorCode(err) != port.ErrCodeDatabaseFailed {
		t.Errorf("error code = %q, want DATABASE_FAILED", port.ErrorCode(err))
	}

	// Compensation should occur.
	if len(deps.objectStorage.deleteCalls) != 1 {
		t.Errorf("expected compensation, got %d deletes", len(deps.objectStorage.deleteCalls))
	}
}

// ---------------------------------------------------------------------------
// GetDiff tests.
// ---------------------------------------------------------------------------

func TestGetDiff_HappyPath(t *testing.T) {
	t.Parallel()
	deps := newTestDeps()

	expectedRef := &model.VersionDiffReference{
		DiffID:              "diff-1",
		DocumentID:          "doc-1",
		OrganizationID:      "org-1",
		BaseVersionID:       "ver-1",
		TargetVersionID:     "ver-2",
		StorageKey:          "org-1/doc-1/diffs/ver-1_ver-2",
		TextDiffCount:       3,
		StructuralDiffCount: 2,
		JobID:               "job-1",
	}
	deps.diffRepo.findFn = func(ctx context.Context, orgID, docID, baseVersionID, targetVersionID string) (*model.VersionDiffReference, error) {
		return expectedRef, nil
	}

	blobContent := []byte(`{"text_diffs":[],"structural_diffs":[]}`)
	deps.objectStorage.getContent[expectedRef.StorageKey] = blobContent

	svc := deps.newService()

	ref, data, err := svc.GetDiff(context.Background(), port.GetDiffParams{
		OrganizationID:  "org-1",
		DocumentID:      "doc-1",
		BaseVersionID:   "ver-1",
		TargetVersionID: "ver-2",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if ref.DiffID != "diff-1" {
		t.Errorf("ref.DiffID = %q", ref.DiffID)
	}
	if ref.TextDiffCount != 3 {
		t.Errorf("ref.TextDiffCount = %d", ref.TextDiffCount)
	}
	if string(data) != string(blobContent) {
		t.Errorf("data = %q, want %q", data, blobContent)
	}

	// Verify GetObject was called with correct key.
	if len(deps.objectStorage.getCalls) != 1 {
		t.Fatalf("expected 1 get call, got %d", len(deps.objectStorage.getCalls))
	}
	if deps.objectStorage.getCalls[0] != expectedRef.StorageKey {
		t.Errorf("get key = %q", deps.objectStorage.getCalls[0])
	}
}

func TestGetDiff_ValidationErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		params  port.GetDiffParams
		wantMsg string
	}{
		{"empty org_id", port.GetDiffParams{DocumentID: "d", BaseVersionID: "b", TargetVersionID: "t"}, "organization_id is required"},
		{"empty doc_id", port.GetDiffParams{OrganizationID: "o", BaseVersionID: "b", TargetVersionID: "t"}, "document_id is required"},
		{"empty base_version_id", port.GetDiffParams{OrganizationID: "o", DocumentID: "d", TargetVersionID: "t"}, "base_version_id is required"},
		{"empty target_version_id", port.GetDiffParams{OrganizationID: "o", DocumentID: "d", BaseVersionID: "b"}, "target_version_id is required"},
		{"same base and target", port.GetDiffParams{OrganizationID: "o", DocumentID: "d", BaseVersionID: "v", TargetVersionID: "v"}, "must differ"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			deps := newTestDeps()
			svc := deps.newService()

			_, _, err := svc.GetDiff(context.Background(), tt.params)
			if err == nil {
				t.Fatal("expected error")
			}
			if port.ErrorCode(err) != port.ErrCodeValidation {
				t.Errorf("error code = %q, want VALIDATION_ERROR", port.ErrorCode(err))
			}
			if !strings.Contains(err.Error(), tt.wantMsg) {
				t.Errorf("error = %q, want to contain %q", err.Error(), tt.wantMsg)
			}
		})
	}
}

func TestGetDiff_DiffNotFound(t *testing.T) {
	t.Parallel()
	deps := newTestDeps()
	// diffRepo default returns DiffNotFound.
	svc := deps.newService()

	_, _, err := svc.GetDiff(context.Background(), port.GetDiffParams{
		OrganizationID:  "org-1",
		DocumentID:      "doc-1",
		BaseVersionID:   "ver-1",
		TargetVersionID: "ver-2",
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if port.ErrorCode(err) != port.ErrCodeDiffNotFound {
		t.Errorf("error code = %q, want DIFF_NOT_FOUND", port.ErrorCode(err))
	}
}

func TestGetDiff_StorageGetFailure(t *testing.T) {
	t.Parallel()
	deps := newTestDeps()
	deps.diffRepo.findFn = func(ctx context.Context, orgID, docID, baseVersionID, targetVersionID string) (*model.VersionDiffReference, error) {
		return &model.VersionDiffReference{
			StorageKey: "some-key",
		}, nil
	}
	deps.objectStorage.getErr = errors.New("s3 timeout")
	svc := deps.newService()

	_, _, err := svc.GetDiff(context.Background(), port.GetDiffParams{
		OrganizationID:  "org-1",
		DocumentID:      "doc-1",
		BaseVersionID:   "ver-1",
		TargetVersionID: "ver-2",
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if port.ErrorCode(err) != port.ErrCodeStorageFailed {
		t.Errorf("error code = %q, want STORAGE_FAILED", port.ErrorCode(err))
	}
}

// ---------------------------------------------------------------------------
// Helper tests.
// ---------------------------------------------------------------------------

func TestEnsureJSONArray(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input json.RawMessage
		want  string
	}{
		{"nil", nil, "[]"},
		{"empty", json.RawMessage{}, "[]"},
		{"non-empty", json.RawMessage(`[1,2,3]`), "[1,2,3]"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := ensureJSONArray(tt.input)
			if string(got) != tt.want {
				t.Errorf("ensureJSONArray = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSha256Hex(t *testing.T) {
	t.Parallel()
	hash := sha256Hex([]byte("hello"))
	if len(hash) != 64 {
		t.Errorf("hash length = %d, want 64", len(hash))
	}
	// Known SHA-256 of "hello".
	expected := "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"
	if hash != expected {
		t.Errorf("hash = %q, want %q", hash, expected)
	}
}

func TestHandleDiffReady_CorrelationIDPreserved(t *testing.T) {
	t.Parallel()
	deps := newTestDeps()
	svc := deps.newService()

	event := validDiffEvent()
	event.CorrelationID = "trace-abc-123"

	if err := svc.HandleDiffReady(context.Background(), event); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Check outbox payload carries the correlation_id.
	var persisted model.DocumentVersionDiffPersisted
	if err := json.Unmarshal(deps.outboxRepo.entries[0].Payload, &persisted); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if persisted.CorrelationID != "trace-abc-123" {
		t.Errorf("correlation_id = %q, want trace-abc-123", persisted.CorrelationID)
	}

	// Diff reference should also have the correlation_id.
	if deps.diffRepo.inserted[0].CorrelationID != "trace-abc-123" {
		t.Errorf("ref.CorrelationID = %q", deps.diffRepo.inserted[0].CorrelationID)
	}

	// Audit record too.
	if deps.auditRepo.inserted[0].CorrelationID != "trace-abc-123" {
		t.Errorf("audit.CorrelationID = %q", deps.auditRepo.inserted[0].CorrelationID)
	}
}

func TestHandleDiffReady_TransactorFailure_Compensation(t *testing.T) {
	t.Parallel()
	deps := newTestDeps()
	// Transactor wraps fn, runs it successfully, then returns an error
	// (simulating a commit failure).
	deps.transactor.fn = func(ctx context.Context, fn func(ctx context.Context) error) error {
		if err := fn(ctx); err != nil {
			return err
		}
		return port.NewDatabaseError("commit failed", errors.New("connection reset"))
	}
	svc := deps.newService()

	err := svc.HandleDiffReady(context.Background(), validDiffEvent())
	if err == nil {
		t.Fatal("expected error")
	}
	if port.ErrorCode(err) != port.ErrCodeDatabaseFailed {
		t.Errorf("error code = %q, want DATABASE_FAILED", port.ErrorCode(err))
	}

	// Blob was uploaded (inner fn succeeded).
	if len(deps.objectStorage.putCalls) != 1 {
		t.Fatalf("expected 1 put call, got %d", len(deps.objectStorage.putCalls))
	}

	// Compensation must still fire since commit failed.
	if len(deps.objectStorage.deleteCalls) != 1 {
		t.Fatalf("expected 1 compensation delete, got %d", len(deps.objectStorage.deleteCalls))
	}
}

func TestHandleDiffReady_FindByVersionPairDBError(t *testing.T) {
	t.Parallel()
	deps := newTestDeps()
	// Pre-check for existing diff fails with a DB error (not DiffNotFound).
	deps.diffRepo.findFn = func(ctx context.Context, orgID, docID, baseVersionID, targetVersionID string) (*model.VersionDiffReference, error) {
		return nil, port.NewDatabaseError("pre-check failed", errors.New("timeout"))
	}
	svc := deps.newService()

	err := svc.HandleDiffReady(context.Background(), validDiffEvent())
	if err == nil {
		t.Fatal("expected error")
	}
	if port.ErrorCode(err) != port.ErrCodeDatabaseFailed {
		t.Errorf("error code = %q, want DATABASE_FAILED", port.ErrorCode(err))
	}

	// No blob should be uploaded.
	if len(deps.objectStorage.putCalls) != 0 {
		t.Errorf("unexpected put calls: %d", len(deps.objectStorage.putCalls))
	}
}

func TestHandleDiffReady_InterfaceCompliance(t *testing.T) {
	t.Parallel()
	// Verify compile-time interface check is enforced.
	var _ port.DiffStorageHandler = (*DiffStorageService)(nil)
}

// ---------------------------------------------------------------------------
// Fallback tests (REV-002).
// ---------------------------------------------------------------------------

func TestHandleDiffReady_FallbackOrgID(t *testing.T) {
	t.Parallel()
	deps := newTestDeps()
	deps.fallbackResolver.orgID = "org-1"
	svc := deps.newService()

	event := validDiffEvent()
	event.OrgID = "" // empty — trigger REV-002 fallback

	err := svc.HandleDiffReady(context.Background(), event)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify fallback was called.
	if deps.fallbackResolver.callCount != 1 {
		t.Errorf("fallback resolver call count = %d, want 1", deps.fallbackResolver.callCount)
	}
	// Verify WARN log for REV-002.
	foundWarn := false
	for _, m := range deps.logger.messages {
		if m.level == "WARN" && strings.Contains(m.msg, "REV-002") {
			foundWarn = true
			break
		}
	}
	if !foundWarn {
		t.Error("expected WARN log with REV-002 message")
	}
	// Verify diff was stored (pipeline completed).
	if len(deps.objectStorage.putCalls) != 1 {
		t.Errorf("expected 1 put call, got %d", len(deps.objectStorage.putCalls))
	}
}

func TestHandleDiffReady_FallbackResolverError(t *testing.T) {
	t.Parallel()
	deps := newTestDeps()
	deps.fallbackResolver.err = port.NewDatabaseError("DB down", nil)

	svc := deps.newService()
	event := validDiffEvent()
	event.OrgID = ""

	err := svc.HandleDiffReady(context.Background(), event)
	if err == nil {
		t.Fatal("expected error from fallback resolver")
	}
	if !port.IsRetryable(err) {
		t.Errorf("expected retryable error, got %v", err)
	}
}

func TestHandleDiffReady_NoFallbackWhenOrgPresent(t *testing.T) {
	t.Parallel()
	deps := newTestDeps()
	svc := deps.newService()

	// Event with org_id present — no fallback.
	err := svc.HandleDiffReady(context.Background(), validDiffEvent())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if deps.fallbackResolver.callCount != 0 {
		t.Errorf("fallback resolver should not be called, got %d calls", deps.fallbackResolver.callCount)
	}
}
