package query

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"contractpro/document-management/internal/application/tenant"
	"contractpro/document-management/internal/domain/model"
	"contractpro/document-management/internal/domain/port"
)

// ---------------------------------------------------------------------------
// Mock implementations.
// ---------------------------------------------------------------------------

type mockArtifactRepo struct {
	findByVersionAndType func(ctx context.Context, orgID, docID, versionID string, at model.ArtifactType) (*model.ArtifactDescriptor, error)
	listByVersion        func(ctx context.Context, orgID, docID, versionID string) ([]*model.ArtifactDescriptor, error)
	listByVersionAndTypes func(ctx context.Context, orgID, docID, versionID string, types []model.ArtifactType) ([]*model.ArtifactDescriptor, error)
}

func (m *mockArtifactRepo) FindByVersionAndType(ctx context.Context, orgID, docID, versionID string, at model.ArtifactType) (*model.ArtifactDescriptor, error) {
	if m.findByVersionAndType != nil {
		return m.findByVersionAndType(ctx, orgID, docID, versionID, at)
	}
	return nil, port.NewArtifactNotFoundError(versionID, string(at))
}

func (m *mockArtifactRepo) ListByVersion(ctx context.Context, orgID, docID, versionID string) ([]*model.ArtifactDescriptor, error) {
	if m.listByVersion != nil {
		return m.listByVersion(ctx, orgID, docID, versionID)
	}
	return nil, nil
}

func (m *mockArtifactRepo) ListByVersionAndTypes(ctx context.Context, orgID, docID, versionID string, types []model.ArtifactType) ([]*model.ArtifactDescriptor, error) {
	if m.listByVersionAndTypes != nil {
		return m.listByVersionAndTypes(ctx, orgID, docID, versionID, types)
	}
	return nil, nil
}

func (m *mockArtifactRepo) Insert(context.Context, *model.ArtifactDescriptor) error {
	panic("not used in query")
}
func (m *mockArtifactRepo) DeleteByVersion(context.Context, string, string, string) error {
	panic("not used in query")
}

type mockObjectStorage struct {
	getObjectFn         func(ctx context.Context, key string) (io.ReadCloser, error)
	generatePresignedFn func(ctx context.Context, key string, expiry time.Duration) (string, error)
}

func (m *mockObjectStorage) GetObject(ctx context.Context, key string) (io.ReadCloser, error) {
	if m.getObjectFn != nil {
		return m.getObjectFn(ctx, key)
	}
	return io.NopCloser(strings.NewReader("{}")), nil
}

func (m *mockObjectStorage) GeneratePresignedURL(ctx context.Context, key string, expiry time.Duration) (string, error) {
	if m.generatePresignedFn != nil {
		return m.generatePresignedFn(ctx, key, expiry)
	}
	return "https://presigned-url", nil
}

func (m *mockObjectStorage) PutObject(context.Context, string, io.Reader, string) error {
	panic("not used in query")
}
func (m *mockObjectStorage) DeleteObject(context.Context, string) error {
	panic("not used in query")
}
func (m *mockObjectStorage) HeadObject(context.Context, string) (int64, bool, error) {
	panic("not used in query")
}
func (m *mockObjectStorage) DeleteByPrefix(context.Context, string) error {
	panic("not used in query")
}

type publishedSemanticTree struct {
	event model.SemanticTreeProvided
}

type publishedArtifacts struct {
	event model.ArtifactsProvided
}

type mockConfirmation struct {
	semanticTrees []publishedSemanticTree
	artifacts     []publishedArtifacts
	publishErr    error
}

func (m *mockConfirmation) PublishSemanticTreeProvided(ctx context.Context, event model.SemanticTreeProvided) error {
	if m.publishErr != nil {
		return m.publishErr
	}
	m.semanticTrees = append(m.semanticTrees, publishedSemanticTree{event: event})
	return nil
}

func (m *mockConfirmation) PublishArtifactsProvided(ctx context.Context, event model.ArtifactsProvided) error {
	if m.publishErr != nil {
		return m.publishErr
	}
	m.artifacts = append(m.artifacts, publishedArtifacts{event: event})
	return nil
}

func (m *mockConfirmation) PublishDPArtifactsPersisted(context.Context, model.DocumentProcessingArtifactsPersisted) error {
	return nil
}
func (m *mockConfirmation) PublishDPArtifactsPersistFailed(context.Context, model.DocumentProcessingArtifactsPersistFailed) error {
	return nil
}
func (m *mockConfirmation) PublishDiffPersisted(context.Context, model.DocumentVersionDiffPersisted) error {
	return nil
}
func (m *mockConfirmation) PublishDiffPersistFailed(context.Context, model.DocumentVersionDiffPersistFailed) error {
	return nil
}
func (m *mockConfirmation) PublishLICArtifactsPersisted(context.Context, model.LegalAnalysisArtifactsPersisted) error {
	return nil
}
func (m *mockConfirmation) PublishLICArtifactsPersistFailed(context.Context, model.LegalAnalysisArtifactsPersistFailed) error {
	return nil
}
func (m *mockConfirmation) PublishREReportsPersisted(context.Context, model.ReportsArtifactsPersisted) error {
	return nil
}
func (m *mockConfirmation) PublishREReportsPersistFailed(context.Context, model.ReportsArtifactsPersistFailed) error {
	return nil
}

type mockAuditRepo struct {
	mu        sync.Mutex
	inserted  []*model.AuditRecord
	insertErr error
}

func (m *mockAuditRepo) Insert(_ context.Context, r *model.AuditRecord) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.insertErr != nil {
		return m.insertErr
	}
	m.inserted = append(m.inserted, r)
	return nil
}

func (m *mockAuditRepo) getInserted() []*model.AuditRecord {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]*model.AuditRecord, len(m.inserted))
	copy(cp, m.inserted)
	return cp
}

func (m *mockAuditRepo) List(context.Context, port.AuditListParams) ([]*model.AuditRecord, int, error) {
	panic("not used in query")
}

type mockDocExistence struct {
	exists bool
	err    error
}

func (m *mockDocExistence) ExistsByID(_ context.Context, _, _ string) (bool, error) {
	return m.exists, m.err
}

var _ tenant.DocumentExistenceChecker = (*mockDocExistence)(nil)

type noopTenantMetrics struct{}

func (n *noopTenantMetrics) IncTenantMismatch() {}

var _ tenant.Metrics = (*noopTenantMetrics)(nil)

type mockQueryMetrics struct {
	integrityFailures atomic.Int64
}

func (m *mockQueryMetrics) IncIntegrityCheckFailures() {
	m.integrityFailures.Add(1)
}

var _ Metrics = (*mockQueryMetrics)(nil)

type mockLogger struct {
	mu       sync.Mutex
	messages []logMsg
}

type logMsg struct {
	level string
	msg   string
}

func (m *mockLogger) Info(msg string, _ ...any) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.messages = append(m.messages, logMsg{"INFO", msg})
}
func (m *mockLogger) Warn(msg string, _ ...any) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.messages = append(m.messages, logMsg{"WARN", msg})
}
func (m *mockLogger) Error(msg string, _ ...any) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.messages = append(m.messages, logMsg{"ERROR", msg})
}

func (m *mockLogger) getMessages() []logMsg {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]logMsg, len(m.messages))
	copy(cp, m.messages)
	return cp
}

// ---------------------------------------------------------------------------
// Test helpers.
// ---------------------------------------------------------------------------

type testDeps struct {
	artifactRepo     *mockArtifactRepo
	objectStorage    *mockObjectStorage
	confirmation     *mockConfirmation
	auditRepo        *mockAuditRepo
	fallbackResolver *mockFallbackResolver
	docExistence     *mockDocExistence
	tenantMetrics    *noopTenantMetrics
	queryMetrics     *mockQueryMetrics
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
	return &testDeps{
		artifactRepo:     &mockArtifactRepo{},
		objectStorage:    &mockObjectStorage{},
		confirmation:     &mockConfirmation{},
		auditRepo:        &mockAuditRepo{},
		fallbackResolver: &mockFallbackResolver{orgID: "org-1", versionID: "ver-1"},
		docExistence:     &mockDocExistence{exists: true},
		tenantMetrics:    &noopTenantMetrics{},
		queryMetrics:     &mockQueryMetrics{},
		logger:           &mockLogger{},
	}
}

func (d *testDeps) buildService() *ArtifactQueryService {
	svc := NewArtifactQueryService(
		d.artifactRepo,
		d.objectStorage,
		d.confirmation,
		d.auditRepo,
		d.fallbackResolver,
		d.docExistence,
		d.tenantMetrics,
		d.queryMetrics,
		d.logger,
	)
	svc.newUUID = func() string { return "test-uuid" }
	return svc
}

// waitForAudit polls the audit repo until the expected number of records appears
// or the deadline is exceeded.
func waitForAudit(t *testing.T, repo *mockAuditRepo, wantCount int) []*model.AuditRecord {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		records := repo.getInserted()
		if len(records) >= wantCount {
			return records
		}
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for %d audit records, got %d", wantCount, len(records))
			return nil
		case <-time.After(10 * time.Millisecond):
		}
	}
}

// waitForLogs polls the logger until the expected number of messages appears
// or the deadline is exceeded.
func waitForLogs(t *testing.T, logger *mockLogger, wantCount int) []logMsg {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		msgs := logger.getMessages()
		if len(msgs) >= wantCount {
			return msgs
		}
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for %d log messages, got %d", wantCount, len(msgs))
			return nil
		case <-time.After(10 * time.Millisecond):
		}
	}
}

// testSHA256 computes the hex-encoded SHA-256 hash of s.
func testSHA256(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// defaultContent is the content returned by the default mockObjectStorage.
const defaultContent = "{}"

// defaultContentHash is the SHA-256 of defaultContent.
var defaultContentHash = testSHA256(defaultContent)

func makeDescriptor(versionID string, at model.ArtifactType, storageKey string) *model.ArtifactDescriptor {
	return &model.ArtifactDescriptor{
		ArtifactID:     "art-" + string(at),
		VersionID:      versionID,
		DocumentID:     "doc-1",
		OrganizationID: "org-1",
		ArtifactType:   at,
		ProducerDomain: model.ProducerDomainDP,
		StorageKey:     storageKey,
		SizeBytes:      42,
		ContentHash:    defaultContentHash,
		SchemaVersion:  "1.0",
		JobID:          "job-1",
		CorrelationID:  "corr-1",
		CreatedAt:      time.Now().UTC(),
	}
}

// makeDescriptorForContent creates a descriptor with ContentHash matching the given content.
func makeDescriptorForContent(versionID string, at model.ArtifactType, storageKey, content string) *model.ArtifactDescriptor {
	d := makeDescriptor(versionID, at, storageKey)
	d.ContentHash = testSHA256(content)
	return d
}

func makeSemanticTreeEvent() model.GetSemanticTreeRequest {
	return model.GetSemanticTreeRequest{
		EventMeta:  model.EventMeta{CorrelationID: "corr-1", Timestamp: time.Now().UTC()},
		JobID:      "job-1",
		DocumentID: "doc-1",
		VersionID:  "ver-1",
		OrgID:      "org-1",
	}
}

func makeGetArtifactsEvent(types ...model.ArtifactType) model.GetArtifactsRequest {
	return model.GetArtifactsRequest{
		EventMeta:     model.EventMeta{CorrelationID: "corr-1", Timestamp: time.Now().UTC()},
		JobID:         "job-1",
		DocumentID:    "doc-1",
		VersionID:     "ver-1",
		OrgID:         "org-1",
		ArtifactTypes: types,
	}
}

// ---------------------------------------------------------------------------
// Constructor tests.
// ---------------------------------------------------------------------------

func TestNewArtifactQueryService_PanicsOnNilDeps(t *testing.T) {
	d := newTestDeps()

	tests := []struct {
		name      string
		build     func()
		wantPanic string
	}{
		{"nil artifactRepo", func() {
			NewArtifactQueryService(nil, d.objectStorage, d.confirmation, d.auditRepo, d.fallbackResolver, d.docExistence, d.tenantMetrics, d.queryMetrics, d.logger)
		}, "artifactRepo"},
		{"nil objectStorage", func() {
			NewArtifactQueryService(d.artifactRepo, nil, d.confirmation, d.auditRepo, d.fallbackResolver, d.docExistence, d.tenantMetrics, d.queryMetrics, d.logger)
		}, "objectStorage"},
		{"nil confirmation", func() {
			NewArtifactQueryService(d.artifactRepo, d.objectStorage, nil, d.auditRepo, d.fallbackResolver, d.docExistence, d.tenantMetrics, d.queryMetrics, d.logger)
		}, "confirmation"},
		{"nil auditRepo", func() {
			NewArtifactQueryService(d.artifactRepo, d.objectStorage, d.confirmation, nil, d.fallbackResolver, d.docExistence, d.tenantMetrics, d.queryMetrics, d.logger)
		}, "auditRepo"},
		{"nil fallbackResolver", func() {
			NewArtifactQueryService(d.artifactRepo, d.objectStorage, d.confirmation, d.auditRepo, nil, d.docExistence, d.tenantMetrics, d.queryMetrics, d.logger)
		}, "fallbackResolver"},
		{"nil docRepo", func() {
			NewArtifactQueryService(d.artifactRepo, d.objectStorage, d.confirmation, d.auditRepo, d.fallbackResolver, nil, d.tenantMetrics, d.queryMetrics, d.logger)
		}, "docRepo"},
		{"nil tenantMetrics", func() {
			NewArtifactQueryService(d.artifactRepo, d.objectStorage, d.confirmation, d.auditRepo, d.fallbackResolver, d.docExistence, nil, d.queryMetrics, d.logger)
		}, "tenantMetrics"},
		{"nil metrics", func() {
			NewArtifactQueryService(d.artifactRepo, d.objectStorage, d.confirmation, d.auditRepo, d.fallbackResolver, d.docExistence, d.tenantMetrics, nil, d.logger)
		}, "metrics"},
		{"nil logger", func() {
			NewArtifactQueryService(d.artifactRepo, d.objectStorage, d.confirmation, d.auditRepo, d.fallbackResolver, d.docExistence, d.tenantMetrics, d.queryMetrics, nil)
		}, "logger"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				r := recover()
				if r == nil {
					t.Fatal("expected panic")
				}
				msg, ok := r.(string)
				if !ok {
					t.Fatalf("expected string panic, got %T", r)
				}
				if !strings.Contains(msg, tt.wantPanic) {
					t.Fatalf("panic %q does not contain %q", msg, tt.wantPanic)
				}
			}()
			tt.build()
		})
	}
}

func TestNewArtifactQueryService_Success(t *testing.T) {
	d := newTestDeps()
	svc := NewArtifactQueryService(d.artifactRepo, d.objectStorage, d.confirmation, d.auditRepo, d.fallbackResolver, d.docExistence, d.tenantMetrics, d.queryMetrics, d.logger)
	if svc == nil {
		t.Fatal("expected non-nil service")
	}
}

// ---------------------------------------------------------------------------
// HandleGetSemanticTree tests.
// ---------------------------------------------------------------------------

func TestHandleGetSemanticTree_HappyPath(t *testing.T) {
	d := newTestDeps()
	treeData := json.RawMessage(`{"nodes":[{"id":"root","text":"Contract"}]}`)

	d.artifactRepo.findByVersionAndType = func(_ context.Context, orgID, docID, versionID string, at model.ArtifactType) (*model.ArtifactDescriptor, error) {
		if orgID != "org-1" || docID != "doc-1" || versionID != "ver-1" || at != model.ArtifactTypeSemanticTree {
			t.Fatalf("unexpected args: %s %s %s %s", orgID, docID, versionID, at)
		}
		return makeDescriptorForContent("ver-1", model.ArtifactTypeSemanticTree, "org-1/doc-1/ver-1/SEMANTIC_TREE", string(treeData)), nil
	}

	d.objectStorage.getObjectFn = func(_ context.Context, key string) (io.ReadCloser, error) {
		if key != "org-1/doc-1/ver-1/SEMANTIC_TREE" {
			t.Fatalf("unexpected key: %s", key)
		}
		return io.NopCloser(strings.NewReader(string(treeData))), nil
	}

	svc := d.buildService()
	err := svc.HandleGetSemanticTree(context.Background(), makeSemanticTreeEvent())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(d.confirmation.semanticTrees) != 1 {
		t.Fatalf("expected 1 published event, got %d", len(d.confirmation.semanticTrees))
	}

	pub := d.confirmation.semanticTrees[0].event
	if pub.JobID != "job-1" {
		t.Errorf("job_id: got %q, want %q", pub.JobID, "job-1")
	}
	if pub.DocumentID != "doc-1" {
		t.Errorf("document_id: got %q, want %q", pub.DocumentID, "doc-1")
	}
	if pub.VersionID != "ver-1" {
		t.Errorf("version_id: got %q, want %q", pub.VersionID, "ver-1")
	}
	if pub.ErrorCode != "" {
		t.Errorf("error_code should be empty, got %q", pub.ErrorCode)
	}
	if string(pub.SemanticTree) != string(treeData) {
		t.Errorf("semantic_tree mismatch: got %s", pub.SemanticTree)
	}
	if pub.CorrelationID != "corr-1" {
		t.Errorf("correlation_id: got %q, want %q", pub.CorrelationID, "corr-1")
	}

	// Wait for async audit goroutine to complete.
	records := waitForAudit(t, d.auditRepo, 1)
	audit := records[0]
	if audit.Action != model.AuditActionArtifactRead {
		t.Errorf("audit action: got %q, want %q", audit.Action, model.AuditActionArtifactRead)
	}
	if audit.ActorType != model.ActorTypeDomain {
		t.Errorf("audit actor_type: got %q, want %q", audit.ActorType, model.ActorTypeDomain)
	}
	if audit.ActorID != "DP" {
		t.Errorf("audit actor_id: got %q, want %q", audit.ActorID, "DP")
	}
}

func TestHandleGetSemanticTree_NotFound_PublishesErrorResponse(t *testing.T) {
	d := newTestDeps()

	d.artifactRepo.findByVersionAndType = func(_ context.Context, _, _, versionID string, _ model.ArtifactType) (*model.ArtifactDescriptor, error) {
		return nil, port.NewArtifactNotFoundError(versionID, "SEMANTIC_TREE")
	}

	svc := d.buildService()
	err := svc.HandleGetSemanticTree(context.Background(), makeSemanticTreeEvent())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(d.confirmation.semanticTrees) != 1 {
		t.Fatalf("expected 1 published event, got %d", len(d.confirmation.semanticTrees))
	}

	pub := d.confirmation.semanticTrees[0].event
	if pub.ErrorCode != port.ErrCodeArtifactNotFound {
		t.Errorf("error_code: got %q, want %q", pub.ErrorCode, port.ErrCodeArtifactNotFound)
	}
	if pub.ErrorMessage == "" {
		t.Error("error_message should not be empty")
	}
	if pub.SemanticTree != nil {
		t.Errorf("semantic_tree should be nil on not-found, got %s", pub.SemanticTree)
	}
}

func TestHandleGetSemanticTree_InfraError_ReturnsErrorForRetry(t *testing.T) {
	d := newTestDeps()

	dbErr := port.NewDatabaseError("connection timeout", errors.New("timeout"))
	d.artifactRepo.findByVersionAndType = func(context.Context, string, string, string, model.ArtifactType) (*model.ArtifactDescriptor, error) {
		return nil, dbErr
	}

	svc := d.buildService()
	err := svc.HandleGetSemanticTree(context.Background(), makeSemanticTreeEvent())
	if err == nil {
		t.Fatal("expected error for retry")
	}
	if !port.IsRetryable(err) {
		t.Errorf("error should be retryable, got: %v", err)
	}
	if len(d.confirmation.semanticTrees) != 0 {
		t.Error("should not publish on infra error")
	}
}

func TestHandleGetSemanticTree_StorageReadError(t *testing.T) {
	d := newTestDeps()

	d.artifactRepo.findByVersionAndType = func(context.Context, string, string, string, model.ArtifactType) (*model.ArtifactDescriptor, error) {
		return makeDescriptor("ver-1", model.ArtifactTypeSemanticTree, "org-1/doc-1/ver-1/SEMANTIC_TREE"), nil
	}
	d.objectStorage.getObjectFn = func(context.Context, string) (io.ReadCloser, error) {
		return nil, port.NewStorageError("S3 unavailable", errors.New("timeout"))
	}

	svc := d.buildService()
	err := svc.HandleGetSemanticTree(context.Background(), makeSemanticTreeEvent())
	if err == nil {
		t.Fatal("expected error")
	}
	if !port.IsRetryable(err) {
		t.Errorf("storage error should be retryable: %v", err)
	}
}

func TestHandleGetSemanticTree_PublishError(t *testing.T) {
	d := newTestDeps()

	d.artifactRepo.findByVersionAndType = func(context.Context, string, string, string, model.ArtifactType) (*model.ArtifactDescriptor, error) {
		return makeDescriptor("ver-1", model.ArtifactTypeSemanticTree, "key"), nil
	}
	d.confirmation.publishErr = port.NewBrokerError("broker down", errors.New("conn refused"))

	svc := d.buildService()
	err := svc.HandleGetSemanticTree(context.Background(), makeSemanticTreeEvent())
	if err == nil {
		t.Fatal("expected error on publish failure")
	}
}

func TestHandleGetSemanticTree_ValidationErrors(t *testing.T) {
	d := newTestDeps()
	svc := d.buildService()

	tests := []struct {
		name  string
		event model.GetSemanticTreeRequest
		want  string
	}{
		{"empty job_id", model.GetSemanticTreeRequest{
			EventMeta: model.EventMeta{CorrelationID: "c"}, OrgID: "o", DocumentID: "d", VersionID: "v",
		}, "job_id"},
		{"empty document_id", model.GetSemanticTreeRequest{
			EventMeta: model.EventMeta{CorrelationID: "c"}, OrgID: "o", JobID: "j", VersionID: "v",
		}, "document_id"},
		{"empty version_id", model.GetSemanticTreeRequest{
			EventMeta: model.EventMeta{CorrelationID: "c"}, OrgID: "o", JobID: "j", DocumentID: "d",
		}, "version_id"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := svc.HandleGetSemanticTree(context.Background(), tt.event)
			if err == nil {
				t.Fatal("expected validation error")
			}
			if port.ErrorCode(err) != port.ErrCodeValidation {
				t.Errorf("error code: got %q, want %q", port.ErrorCode(err), port.ErrCodeValidation)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error %q should mention %q", err.Error(), tt.want)
			}
		})
	}
}

func TestHandleGetSemanticTree_ContextCancelled(t *testing.T) {
	d := newTestDeps()

	d.artifactRepo.findByVersionAndType = func(ctx context.Context, _, _, _ string, _ model.ArtifactType) (*model.ArtifactDescriptor, error) {
		return makeDescriptor("ver-1", model.ArtifactTypeSemanticTree, "key"), nil
	}
	d.objectStorage.getObjectFn = func(ctx context.Context, _ string) (io.ReadCloser, error) {
		return nil, ctx.Err()
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	svc := d.buildService()
	err := svc.HandleGetSemanticTree(ctx, makeSemanticTreeEvent())
	if err == nil {
		t.Fatal("expected error on cancelled context")
	}
}

// ---------------------------------------------------------------------------
// HandleGetArtifacts tests.
// ---------------------------------------------------------------------------

func TestHandleGetArtifacts_HappyPath_AllFound(t *testing.T) {
	d := newTestDeps()

	treeContent := `{"nodes":[]}`
	structContent := `{"sections":[]}`

	contentByType := map[model.ArtifactType]string{
		model.ArtifactTypeSemanticTree:      treeContent,
		model.ArtifactTypeDocumentStructure: structContent,
	}

	d.artifactRepo.listByVersionAndTypes = func(_ context.Context, _, _, _ string, types []model.ArtifactType) ([]*model.ArtifactDescriptor, error) {
		result := make([]*model.ArtifactDescriptor, 0, len(types))
		for _, at := range types {
			c := contentByType[at]
			result = append(result, makeDescriptorForContent("ver-1", at, "org-1/doc-1/ver-1/"+string(at), c))
		}
		return result, nil
	}

	d.objectStorage.getObjectFn = func(_ context.Context, key string) (io.ReadCloser, error) {
		switch {
		case strings.HasSuffix(key, "SEMANTIC_TREE"):
			return io.NopCloser(strings.NewReader(treeContent)), nil
		case strings.HasSuffix(key, "DOCUMENT_STRUCTURE"):
			return io.NopCloser(strings.NewReader(structContent)), nil
		default:
			return io.NopCloser(strings.NewReader("{}")), nil
		}
	}

	svc := d.buildService()
	event := makeGetArtifactsEvent(model.ArtifactTypeSemanticTree, model.ArtifactTypeDocumentStructure)
	err := svc.HandleGetArtifacts(context.Background(), event)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(d.confirmation.artifacts) != 1 {
		t.Fatalf("expected 1 published event, got %d", len(d.confirmation.artifacts))
	}

	pub := d.confirmation.artifacts[0].event
	if pub.JobID != "job-1" {
		t.Errorf("job_id: got %q", pub.JobID)
	}
	if len(pub.Artifacts) != 2 {
		t.Fatalf("expected 2 artifacts, got %d", len(pub.Artifacts))
	}
	if string(pub.Artifacts[model.ArtifactTypeSemanticTree]) != treeContent {
		t.Errorf("semantic tree content mismatch")
	}
	if string(pub.Artifacts[model.ArtifactTypeDocumentStructure]) != structContent {
		t.Errorf("structure content mismatch")
	}
	if len(pub.MissingTypes) != 0 {
		t.Errorf("expected no missing types, got %v", pub.MissingTypes)
	}
	if pub.CorrelationID != "corr-1" {
		t.Errorf("correlation_id: got %q, want %q", pub.CorrelationID, "corr-1")
	}
}

func TestHandleGetArtifacts_PartiallyFound_WithMissingTypes(t *testing.T) {
	d := newTestDeps()

	// Only return SEMANTIC_TREE, not DOCUMENT_STRUCTURE.
	d.artifactRepo.listByVersionAndTypes = func(_ context.Context, _, _, _ string, _ []model.ArtifactType) ([]*model.ArtifactDescriptor, error) {
		return []*model.ArtifactDescriptor{
			makeDescriptor("ver-1", model.ArtifactTypeSemanticTree, "org-1/doc-1/ver-1/SEMANTIC_TREE"),
		}, nil
	}

	svc := d.buildService()
	event := makeGetArtifactsEvent(model.ArtifactTypeSemanticTree, model.ArtifactTypeDocumentStructure)
	err := svc.HandleGetArtifacts(context.Background(), event)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	pub := d.confirmation.artifacts[0].event
	if len(pub.Artifacts) != 1 {
		t.Fatalf("expected 1 artifact, got %d", len(pub.Artifacts))
	}
	if len(pub.MissingTypes) != 1 {
		t.Fatalf("expected 1 missing type, got %d", len(pub.MissingTypes))
	}
	if pub.MissingTypes[0] != model.ArtifactTypeDocumentStructure {
		t.Errorf("missing type: got %q, want %q", pub.MissingTypes[0], model.ArtifactTypeDocumentStructure)
	}
}

func TestHandleGetArtifacts_NoneFound_AllMissing(t *testing.T) {
	d := newTestDeps()

	d.artifactRepo.listByVersionAndTypes = func(context.Context, string, string, string, []model.ArtifactType) ([]*model.ArtifactDescriptor, error) {
		return nil, nil
	}

	svc := d.buildService()
	event := makeGetArtifactsEvent(model.ArtifactTypeSemanticTree, model.ArtifactTypeRiskAnalysis)
	err := svc.HandleGetArtifacts(context.Background(), event)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	pub := d.confirmation.artifacts[0].event
	if len(pub.Artifacts) != 0 {
		t.Errorf("expected empty artifacts, got %d", len(pub.Artifacts))
	}
	if len(pub.MissingTypes) != 2 {
		t.Errorf("expected 2 missing types, got %d", len(pub.MissingTypes))
	}
}

func TestHandleGetArtifacts_InfraError_ReturnsForRetry(t *testing.T) {
	d := newTestDeps()

	d.artifactRepo.listByVersionAndTypes = func(context.Context, string, string, string, []model.ArtifactType) ([]*model.ArtifactDescriptor, error) {
		return nil, port.NewDatabaseError("DB down", errors.New("conn refused"))
	}

	svc := d.buildService()
	event := makeGetArtifactsEvent(model.ArtifactTypeSemanticTree)
	err := svc.HandleGetArtifacts(context.Background(), event)
	if err == nil {
		t.Fatal("expected error for retry")
	}
	if !port.IsRetryable(err) {
		t.Errorf("should be retryable: %v", err)
	}
}

func TestHandleGetArtifacts_StorageReadError(t *testing.T) {
	d := newTestDeps()

	d.artifactRepo.listByVersionAndTypes = func(context.Context, string, string, string, []model.ArtifactType) ([]*model.ArtifactDescriptor, error) {
		return []*model.ArtifactDescriptor{
			makeDescriptor("ver-1", model.ArtifactTypeSemanticTree, "key"),
		}, nil
	}
	d.objectStorage.getObjectFn = func(context.Context, string) (io.ReadCloser, error) {
		return nil, port.NewStorageError("S3 fail", errors.New("timeout"))
	}

	svc := d.buildService()
	err := svc.HandleGetArtifacts(context.Background(), makeGetArtifactsEvent(model.ArtifactTypeSemanticTree))
	if err == nil {
		t.Fatal("expected error")
	}
	if !port.IsRetryable(err) {
		t.Errorf("storage error should be retryable: %v", err)
	}
}

func TestHandleGetArtifacts_EmptyTypes_ValidationError(t *testing.T) {
	d := newTestDeps()
	svc := d.buildService()

	event := makeGetArtifactsEvent() // no types
	err := svc.HandleGetArtifacts(context.Background(), event)
	if err == nil {
		t.Fatal("expected validation error")
	}
	if port.ErrorCode(err) != port.ErrCodeValidation {
		t.Errorf("error code: got %q, want VALIDATION_ERROR", port.ErrorCode(err))
	}
}

func TestHandleGetArtifacts_ValidationErrors(t *testing.T) {
	d := newTestDeps()
	svc := d.buildService()

	tests := []struct {
		name  string
		event model.GetArtifactsRequest
		want  string
	}{
		{"empty job_id", model.GetArtifactsRequest{
			EventMeta: model.EventMeta{CorrelationID: "c"}, OrgID: "o", DocumentID: "d", VersionID: "v",
			ArtifactTypes: []model.ArtifactType{model.ArtifactTypeSemanticTree},
		}, "job_id"},
		{"empty document_id", model.GetArtifactsRequest{
			EventMeta: model.EventMeta{CorrelationID: "c"}, OrgID: "o", JobID: "j", VersionID: "v",
			ArtifactTypes: []model.ArtifactType{model.ArtifactTypeSemanticTree},
		}, "document_id"},
		{"empty version_id", model.GetArtifactsRequest{
			EventMeta: model.EventMeta{CorrelationID: "c"}, OrgID: "o", JobID: "j", DocumentID: "d",
			ArtifactTypes: []model.ArtifactType{model.ArtifactTypeSemanticTree},
		}, "version_id"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := svc.HandleGetArtifacts(context.Background(), tt.event)
			if err == nil {
				t.Fatal("expected validation error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error %q should mention %q", err.Error(), tt.want)
			}
		})
	}
}

func TestHandleGetArtifacts_CorrelationIDPreserved(t *testing.T) {
	d := newTestDeps()

	d.artifactRepo.listByVersionAndTypes = func(context.Context, string, string, string, []model.ArtifactType) ([]*model.ArtifactDescriptor, error) {
		return nil, nil
	}

	svc := d.buildService()
	event := makeGetArtifactsEvent(model.ArtifactTypeSemanticTree)
	event.CorrelationID = "my-corr-id"
	err := svc.HandleGetArtifacts(context.Background(), event)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	pub := d.confirmation.artifacts[0].event
	if pub.CorrelationID != "my-corr-id" {
		t.Errorf("correlation_id: got %q, want %q", pub.CorrelationID, "my-corr-id")
	}
}

func TestHandleGetArtifacts_PublishError(t *testing.T) {
	d := newTestDeps()

	d.artifactRepo.listByVersionAndTypes = func(context.Context, string, string, string, []model.ArtifactType) ([]*model.ArtifactDescriptor, error) {
		return nil, nil
	}
	d.confirmation.publishErr = port.NewBrokerError("broker down", errors.New("err"))

	svc := d.buildService()
	err := svc.HandleGetArtifacts(context.Background(), makeGetArtifactsEvent(model.ArtifactTypeSemanticTree))
	if err == nil {
		t.Fatal("expected error on publish failure")
	}
}

// ---------------------------------------------------------------------------
// GetArtifact (sync) tests.
// ---------------------------------------------------------------------------

func TestGetArtifact_HappyPath(t *testing.T) {
	d := newTestDeps()

	content := `{"nodes":[]}`
	d.artifactRepo.findByVersionAndType = func(_ context.Context, orgID, docID, versionID string, at model.ArtifactType) (*model.ArtifactDescriptor, error) {
		return makeDescriptorForContent(versionID, at, "org-1/doc-1/ver-1/SEMANTIC_TREE", content), nil
	}
	d.objectStorage.getObjectFn = func(_ context.Context, _ string) (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader(content)), nil
	}

	svc := d.buildService()
	result, err := svc.GetArtifact(context.Background(), port.GetArtifactParams{
		OrganizationID: "org-1",
		DocumentID:     "doc-1",
		VersionID:      "ver-1",
		ArtifactType:   model.ArtifactTypeSemanticTree,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(result.Content) != content {
		t.Errorf("content: got %q, want %q", result.Content, content)
	}
	if result.ContentType != "application/json" {
		t.Errorf("content_type: got %q, want %q", result.ContentType, "application/json")
	}
}

func TestGetArtifact_PDF_ContentType(t *testing.T) {
	d := newTestDeps()

	d.artifactRepo.findByVersionAndType = func(_ context.Context, _, _, versionID string, at model.ArtifactType) (*model.ArtifactDescriptor, error) {
		return makeDescriptorForContent(versionID, at, "key", "pdf-data"), nil
	}
	d.objectStorage.getObjectFn = func(context.Context, string) (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader("pdf-data")), nil
	}

	svc := d.buildService()
	result, err := svc.GetArtifact(context.Background(), port.GetArtifactParams{
		OrganizationID: "org-1",
		DocumentID:     "doc-1",
		VersionID:      "ver-1",
		ArtifactType:   model.ArtifactTypeExportPDF,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ContentType != "application/pdf" {
		t.Errorf("content_type: got %q, want %q", result.ContentType, "application/pdf")
	}
}

func TestGetArtifact_DOCX_ContentType(t *testing.T) {
	d := newTestDeps()

	d.artifactRepo.findByVersionAndType = func(_ context.Context, _, _, versionID string, at model.ArtifactType) (*model.ArtifactDescriptor, error) {
		return makeDescriptorForContent(versionID, at, "key", "docx-data"), nil
	}
	d.objectStorage.getObjectFn = func(context.Context, string) (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader("docx-data")), nil
	}

	svc := d.buildService()
	result, err := svc.GetArtifact(context.Background(), port.GetArtifactParams{
		OrganizationID: "org-1",
		DocumentID:     "doc-1",
		VersionID:      "ver-1",
		ArtifactType:   model.ArtifactTypeExportDOCX,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantCT := "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	if result.ContentType != wantCT {
		t.Errorf("content_type: got %q, want %q", result.ContentType, wantCT)
	}
}

func TestGetArtifact_NotFound(t *testing.T) {
	d := newTestDeps()

	d.artifactRepo.findByVersionAndType = func(_ context.Context, _, _, versionID string, at model.ArtifactType) (*model.ArtifactDescriptor, error) {
		return nil, port.NewArtifactNotFoundError(versionID, string(at))
	}

	svc := d.buildService()
	_, err := svc.GetArtifact(context.Background(), port.GetArtifactParams{
		OrganizationID: "org-1",
		DocumentID:     "doc-1",
		VersionID:      "ver-1",
		ArtifactType:   model.ArtifactTypeSemanticTree,
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if port.ErrorCode(err) != port.ErrCodeArtifactNotFound {
		t.Errorf("error code: got %q, want %q", port.ErrorCode(err), port.ErrCodeArtifactNotFound)
	}
}

func TestGetArtifact_StorageError(t *testing.T) {
	d := newTestDeps()

	d.artifactRepo.findByVersionAndType = func(_ context.Context, _, _, versionID string, at model.ArtifactType) (*model.ArtifactDescriptor, error) {
		return makeDescriptor(versionID, at, "key"), nil
	}
	d.objectStorage.getObjectFn = func(context.Context, string) (io.ReadCloser, error) {
		return nil, port.NewStorageError("S3 down", errors.New("timeout"))
	}

	svc := d.buildService()
	_, err := svc.GetArtifact(context.Background(), port.GetArtifactParams{
		OrganizationID: "org-1",
		DocumentID:     "doc-1",
		VersionID:      "ver-1",
		ArtifactType:   model.ArtifactTypeSemanticTree,
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !port.IsRetryable(err) {
		t.Errorf("storage error should be retryable: %v", err)
	}
}

func TestGetArtifact_ValidationErrors(t *testing.T) {
	d := newTestDeps()
	svc := d.buildService()

	tests := []struct {
		name   string
		params port.GetArtifactParams
		want   string
	}{
		{"empty org_id", port.GetArtifactParams{DocumentID: "d", VersionID: "v", ArtifactType: "A"}, "organization_id"},
		{"empty doc_id", port.GetArtifactParams{OrganizationID: "o", VersionID: "v", ArtifactType: "A"}, "document_id"},
		{"empty version_id", port.GetArtifactParams{OrganizationID: "o", DocumentID: "d", ArtifactType: "A"}, "version_id"},
		{"empty artifact_type", port.GetArtifactParams{OrganizationID: "o", DocumentID: "d", VersionID: "v"}, "artifact_type"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := svc.GetArtifact(context.Background(), tt.params)
			if err == nil {
				t.Fatal("expected validation error")
			}
			if port.ErrorCode(err) != port.ErrCodeValidation {
				t.Errorf("error code: got %q, want VALIDATION_ERROR", port.ErrorCode(err))
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error %q should mention %q", err.Error(), tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// ListArtifacts tests.
// ---------------------------------------------------------------------------

func TestListArtifacts_HappyPath(t *testing.T) {
	d := newTestDeps()

	d.artifactRepo.listByVersion = func(_ context.Context, orgID, docID, versionID string) ([]*model.ArtifactDescriptor, error) {
		return []*model.ArtifactDescriptor{
			makeDescriptor(versionID, model.ArtifactTypeSemanticTree, "key1"),
			makeDescriptor(versionID, model.ArtifactTypeOCRRaw, "key2"),
		}, nil
	}

	svc := d.buildService()
	result, err := svc.ListArtifacts(context.Background(), "org-1", "doc-1", "ver-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 descriptors, got %d", len(result))
	}
}

func TestListArtifacts_Empty_ReturnsEmptySlice(t *testing.T) {
	d := newTestDeps()

	d.artifactRepo.listByVersion = func(context.Context, string, string, string) ([]*model.ArtifactDescriptor, error) {
		return nil, nil // simulates no artifacts
	}

	svc := d.buildService()
	result, err := svc.ListArtifacts(context.Background(), "org-1", "doc-1", "ver-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil empty slice, got nil")
	}
	if len(result) != 0 {
		t.Errorf("expected 0 descriptors, got %d", len(result))
	}
}

func TestListArtifacts_DatabaseError(t *testing.T) {
	d := newTestDeps()

	d.artifactRepo.listByVersion = func(context.Context, string, string, string) ([]*model.ArtifactDescriptor, error) {
		return nil, port.NewDatabaseError("DB down", errors.New("conn"))
	}

	svc := d.buildService()
	_, err := svc.ListArtifacts(context.Background(), "org-1", "doc-1", "ver-1")
	if err == nil {
		t.Fatal("expected error")
	}
	if !port.IsRetryable(err) {
		t.Errorf("database error should be retryable: %v", err)
	}
}

func TestListArtifacts_ValidationErrors(t *testing.T) {
	d := newTestDeps()
	svc := d.buildService()

	tests := []struct {
		name    string
		orgID   string
		docID   string
		verID   string
		want    string
	}{
		{"empty org_id", "", "doc-1", "ver-1", "organization_id"},
		{"empty doc_id", "org-1", "", "ver-1", "document_id"},
		{"empty ver_id", "org-1", "doc-1", "", "version_id"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := svc.ListArtifacts(context.Background(), tt.orgID, tt.docID, tt.verID)
			if err == nil {
				t.Fatal("expected validation error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error %q should mention %q", err.Error(), tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// readArtifact tests.
// ---------------------------------------------------------------------------

func TestReadArtifact_SizeLimitExceeded(t *testing.T) {
	d := newTestDeps()

	// Create data slightly over the limit.
	oversized := strings.Repeat("x", maxArtifactReadBytes+1)
	d.objectStorage.getObjectFn = func(context.Context, string) (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader(oversized)), nil
	}

	svc := d.buildService()
	_, err := svc.readArtifact(context.Background(), "key", "")
	if err == nil {
		t.Fatal("expected error for oversized artifact")
	}
	if port.ErrorCode(err) != port.ErrCodeValidation {
		t.Errorf("error code: got %q, want VALIDATION_ERROR", port.ErrorCode(err))
	}
	if port.IsRetryable(err) {
		t.Error("oversized artifact error should not be retryable")
	}
}

func TestReadArtifact_ReadError(t *testing.T) {
	d := newTestDeps()

	d.objectStorage.getObjectFn = func(context.Context, string) (io.ReadCloser, error) {
		return io.NopCloser(&failReader{}), nil
	}

	svc := d.buildService()
	_, err := svc.readArtifact(context.Background(), "key", "")
	if err == nil {
		t.Fatal("expected error on read failure")
	}
}

// failReader always returns an error on Read.
type failReader struct{}

func (f *failReader) Read([]byte) (int, error) {
	return 0, errors.New("read failure")
}

// ---------------------------------------------------------------------------
// BRE-027: Content hash verification tests.
// ---------------------------------------------------------------------------

func TestReadArtifact_IntegrityMatch(t *testing.T) {
	d := newTestDeps()

	content := `{"verified":"data"}`
	d.objectStorage.getObjectFn = func(context.Context, string) (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader(content)), nil
	}

	svc := d.buildService()
	data, err := svc.readArtifact(context.Background(), "key", testSHA256(content))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(data) != content {
		t.Errorf("content mismatch: got %q, want %q", data, content)
	}
	if d.queryMetrics.integrityFailures.Load() != 0 {
		t.Error("integrity failure metric should not be incremented on match")
	}
}

func TestReadArtifact_IntegrityMismatch(t *testing.T) {
	d := newTestDeps()

	content := `{"actual":"data"}`
	d.objectStorage.getObjectFn = func(context.Context, string) (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader(content)), nil
	}

	svc := d.buildService()
	_, err := svc.readArtifact(context.Background(), "key", "wrong-hash-value")
	if err == nil {
		t.Fatal("expected error on hash mismatch")
	}
	if port.ErrorCode(err) != port.ErrCodeIntegrityCheckFailed {
		t.Errorf("error code: got %q, want %q", port.ErrorCode(err), port.ErrCodeIntegrityCheckFailed)
	}
	if port.IsRetryable(err) {
		t.Error("integrity check error should not be retryable")
	}
	if !strings.Contains(err.Error(), "wrong-hash-value") {
		t.Errorf("error should mention expected hash: %v", err)
	}
	if !strings.Contains(err.Error(), testSHA256(content)) {
		t.Errorf("error should mention actual hash: %v", err)
	}

	// Verify metric was incremented.
	if d.queryMetrics.integrityFailures.Load() != 1 {
		t.Errorf("integrity failures: got %d, want 1", d.queryMetrics.integrityFailures.Load())
	}

	// Verify ERROR log was emitted.
	msgs := d.logger.getMessages()
	found := false
	for _, msg := range msgs {
		if msg.level == "ERROR" && strings.Contains(msg.msg, "content hash mismatch") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected ERROR log about content hash mismatch")
	}
}

func TestReadArtifact_EmptyHash_SkipsVerification(t *testing.T) {
	d := newTestDeps()

	content := `{"legacy":"artifact"}`
	d.objectStorage.getObjectFn = func(context.Context, string) (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader(content)), nil
	}

	svc := d.buildService()
	data, err := svc.readArtifact(context.Background(), "key", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(data) != content {
		t.Errorf("content: got %q, want %q", data, content)
	}

	// Verify WARN log about skipping verification.
	msgs := d.logger.getMessages()
	found := false
	for _, msg := range msgs {
		if msg.level == "WARN" && strings.Contains(msg.msg, "skipping integrity check") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected WARN log about skipping integrity check")
	}

	// No metric increment.
	if d.queryMetrics.integrityFailures.Load() != 0 {
		t.Error("integrity failure metric should not be incremented when hash is empty")
	}
}

func TestGetArtifact_IntegrityMismatch_ReturnsError(t *testing.T) {
	d := newTestDeps()

	d.artifactRepo.findByVersionAndType = func(_ context.Context, _, _, versionID string, at model.ArtifactType) (*model.ArtifactDescriptor, error) {
		desc := makeDescriptor(versionID, at, "key")
		desc.ContentHash = "stale-hash-from-db"
		return desc, nil
	}
	d.objectStorage.getObjectFn = func(context.Context, string) (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader(`{"corrupted":"data"}`)), nil
	}

	svc := d.buildService()
	_, err := svc.GetArtifact(context.Background(), port.GetArtifactParams{
		OrganizationID: "org-1",
		DocumentID:     "doc-1",
		VersionID:      "ver-1",
		ArtifactType:   model.ArtifactTypeSemanticTree,
	})
	if err == nil {
		t.Fatal("expected integrity check error")
	}
	if port.ErrorCode(err) != port.ErrCodeIntegrityCheckFailed {
		t.Errorf("error code: got %q, want %q", port.ErrorCode(err), port.ErrCodeIntegrityCheckFailed)
	}
}

func TestHandleGetSemanticTree_IntegrityMismatch_ReturnsError(t *testing.T) {
	d := newTestDeps()

	d.artifactRepo.findByVersionAndType = func(context.Context, string, string, string, model.ArtifactType) (*model.ArtifactDescriptor, error) {
		desc := makeDescriptor("ver-1", model.ArtifactTypeSemanticTree, "key")
		desc.ContentHash = "bad-hash"
		return desc, nil
	}
	d.objectStorage.getObjectFn = func(context.Context, string) (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader(`{"tree":"data"}`)), nil
	}

	svc := d.buildService()
	err := svc.HandleGetSemanticTree(context.Background(), makeSemanticTreeEvent())
	if err == nil {
		t.Fatal("expected integrity check error")
	}
	if port.ErrorCode(err) != port.ErrCodeIntegrityCheckFailed {
		t.Errorf("error code: got %q, want %q", port.ErrorCode(err), port.ErrCodeIntegrityCheckFailed)
	}
	if len(d.confirmation.semanticTrees) != 0 {
		t.Error("should not publish on integrity failure")
	}
}

func TestHandleGetArtifacts_IntegrityMismatch_ShortCircuits(t *testing.T) {
	d := newTestDeps()

	d.artifactRepo.listByVersionAndTypes = func(_ context.Context, _, _, _ string, types []model.ArtifactType) ([]*model.ArtifactDescriptor, error) {
		result := make([]*model.ArtifactDescriptor, 0, len(types))
		for _, at := range types {
			desc := makeDescriptor("ver-1", at, "org-1/doc-1/ver-1/"+string(at))
			desc.ContentHash = "stale-hash"
			result = append(result, desc)
		}
		return result, nil
	}
	d.objectStorage.getObjectFn = func(context.Context, string) (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader(`{"data":"value"}`)), nil
	}

	svc := d.buildService()
	event := makeGetArtifactsEvent(model.ArtifactTypeSemanticTree, model.ArtifactTypeDocumentStructure)
	err := svc.HandleGetArtifacts(context.Background(), event)
	if err == nil {
		t.Fatal("expected integrity check error")
	}
	if port.ErrorCode(err) != port.ErrCodeIntegrityCheckFailed {
		t.Errorf("error code: got %q, want %q", port.ErrorCode(err), port.ErrCodeIntegrityCheckFailed)
	}
	// Should short-circuit — no ArtifactsProvided event published.
	if len(d.confirmation.artifacts) != 0 {
		t.Error("should not publish partial ArtifactsProvided on integrity failure")
	}
}

// ---------------------------------------------------------------------------
// inferRequesterDomain tests.
// ---------------------------------------------------------------------------

func TestInferRequesterDomain(t *testing.T) {
	tests := []struct {
		name  string
		types []model.ArtifactType
		want  string
	}{
		{"empty", nil, "UNKNOWN"},
		{"DP types → LIC", []model.ArtifactType{model.ArtifactTypeSemanticTree, model.ArtifactTypeExtractedText}, "LIC"},
		{"LIC types → RE", []model.ArtifactType{model.ArtifactTypeRiskAnalysis, model.ArtifactTypeSummary}, "RE"},
		{"RE types → UNKNOWN", []model.ArtifactType{model.ArtifactTypeExportPDF}, "UNKNOWN"},
		{"mixed DP+LIC → RE", []model.ArtifactType{model.ArtifactTypeSemanticTree, model.ArtifactTypeRiskAnalysis}, "RE"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := inferRequesterDomain(tt.types)
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Async audit tests.
// ---------------------------------------------------------------------------

func TestRecordAuditAsync_ErrorLogged(t *testing.T) {
	d := newTestDeps()
	d.auditRepo.insertErr = errors.New("DB down")

	svc := d.buildService()
	svc.recordAuditAsync("org-1", "doc-1", "ver-1", "job-1", "corr-1", "DP",
		[]model.ArtifactType{model.ArtifactTypeSemanticTree})

	// Wait for async goroutine to log the error.
	msgs := waitForLogs(t, d.logger, 1)
	found := false
	for _, msg := range msgs {
		if msg.level == "WARN" && strings.Contains(msg.msg, "failed to record audit") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected WARN log about failed audit")
	}
}

func TestRecordAuditAsync_AuditDetails(t *testing.T) {
	d := newTestDeps()
	svc := d.buildService()

	svc.recordAuditAsync("org-1", "doc-1", "ver-1", "job-1", "corr-1", "DP",
		[]model.ArtifactType{model.ArtifactTypeSemanticTree})

	records := waitForAudit(t, d.auditRepo, 1)
	record := records[0]
	if record.OrganizationID != "org-1" {
		t.Errorf("org_id: got %q", record.OrganizationID)
	}
	if record.DocumentID != "doc-1" {
		t.Errorf("doc_id: got %q", record.DocumentID)
	}
	if record.VersionID != "ver-1" {
		t.Errorf("version_id: got %q", record.VersionID)
	}
	if record.Action != model.AuditActionArtifactRead {
		t.Errorf("action: got %q", record.Action)
	}
	if record.ActorType != model.ActorTypeDomain {
		t.Errorf("actor_type: got %q", record.ActorType)
	}
	if record.ActorID != "DP" {
		t.Errorf("actor_id: got %q", record.ActorID)
	}

	var details map[string]any
	if err := json.Unmarshal(record.Details, &details); err != nil {
		t.Fatalf("unmarshal details: %v", err)
	}
	if details["requester"] != "DP" {
		t.Errorf("details.requester: got %q", details["requester"])
	}
	if details["artifact_count"].(float64) != 1 {
		t.Errorf("details.artifact_count: got %v", details["artifact_count"])
	}
}

// ---------------------------------------------------------------------------
// Interface compliance.
// ---------------------------------------------------------------------------

func TestInterfaceCompliance(t *testing.T) {
	var _ port.ArtifactQueryHandler = (*ArtifactQueryService)(nil)
}

// ---------------------------------------------------------------------------
// Fallback tests (REV-002).
// ---------------------------------------------------------------------------

func TestHandleGetSemanticTree_FallbackOrgID(t *testing.T) {
	d := newTestDeps()
	d.fallbackResolver.orgID = "org-1"
	treeData := json.RawMessage(`{"nodes":[{"id":"root"}]}`)

	d.artifactRepo.findByVersionAndType = func(_ context.Context, orgID, docID, versionID string, at model.ArtifactType) (*model.ArtifactDescriptor, error) {
		if orgID != "org-1" {
			t.Fatalf("expected resolved org_id=org-1, got %q", orgID)
		}
		return makeDescriptorForContent("ver-1", model.ArtifactTypeSemanticTree, "org-1/doc-1/ver-1/SEMANTIC_TREE", string(treeData)), nil
	}
	d.objectStorage.getObjectFn = func(_ context.Context, key string) (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader(string(treeData))), nil
	}

	svc := d.buildService()
	event := makeSemanticTreeEvent()
	event.OrgID = "" // empty — trigger REV-002 fallback

	err := svc.HandleGetSemanticTree(context.Background(), event)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if d.fallbackResolver.callCount != 1 {
		t.Errorf("fallback resolver call count = %d, want 1", d.fallbackResolver.callCount)
	}
	// Verify response was published with tree content.
	if len(d.confirmation.semanticTrees) != 1 {
		t.Fatalf("expected 1 confirmation, got %d", len(d.confirmation.semanticTrees))
	}
}

func TestHandleGetArtifacts_FallbackOrgID(t *testing.T) {
	d := newTestDeps()
	d.fallbackResolver.orgID = "org-1"

	artifactContent := `{"data":"test"}`
	d.artifactRepo.listByVersionAndTypes = func(_ context.Context, orgID, docID, versionID string, types []model.ArtifactType) ([]*model.ArtifactDescriptor, error) {
		if orgID != "org-1" {
			t.Fatalf("expected resolved org_id=org-1, got %q", orgID)
		}
		return []*model.ArtifactDescriptor{
			makeDescriptorForContent("ver-1", model.ArtifactTypeSemanticTree, "key1", artifactContent),
		}, nil
	}
	d.objectStorage.getObjectFn = func(_ context.Context, key string) (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader(artifactContent)), nil
	}

	svc := d.buildService()
	event := makeGetArtifactsEvent(model.ArtifactTypeSemanticTree)
	event.OrgID = "" // empty — trigger REV-002 fallback

	err := svc.HandleGetArtifacts(context.Background(), event)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if d.fallbackResolver.callCount != 1 {
		t.Errorf("fallback resolver call count = %d, want 1", d.fallbackResolver.callCount)
	}
}

func TestHandleGetSemanticTree_FallbackResolverError(t *testing.T) {
	d := newTestDeps()
	d.fallbackResolver.err = port.NewDatabaseError("DB unreachable", nil)

	svc := d.buildService()
	event := makeSemanticTreeEvent()
	event.OrgID = ""

	err := svc.HandleGetSemanticTree(context.Background(), event)
	if err == nil {
		t.Fatal("expected error from fallback resolver")
	}
	if !port.IsRetryable(err) {
		t.Errorf("expected retryable error, got %v", err)
	}
}

func TestHandleGetSemanticTree_NoFallbackWhenOrgPresent(t *testing.T) {
	d := newTestDeps()
	d.artifactRepo.findByVersionAndType = func(_ context.Context, _, _, _ string, _ model.ArtifactType) (*model.ArtifactDescriptor, error) {
		return makeDescriptor("ver-1", model.ArtifactTypeSemanticTree, "key1"), nil
	}
	d.objectStorage.getObjectFn = func(_ context.Context, _ string) (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader(`{}`)), nil
	}

	svc := d.buildService()
	err := svc.HandleGetSemanticTree(context.Background(), makeSemanticTreeEvent())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d.fallbackResolver.callCount != 0 {
		t.Errorf("fallback resolver should not be called, got %d calls", d.fallbackResolver.callCount)
	}
}
