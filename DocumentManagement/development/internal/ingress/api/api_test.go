package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"contractpro/document-management/internal/domain/model"
	"contractpro/document-management/internal/domain/port"
)

// ---------------------------------------------------------------------------
// Mock implementations
// ---------------------------------------------------------------------------

type mockLogger struct{ msgs []string }

func (m *mockLogger) Info(msg string, args ...any)  { m.msgs = append(m.msgs, msg) }
func (m *mockLogger) Warn(msg string, args ...any)  { m.msgs = append(m.msgs, msg) }
func (m *mockLogger) Error(msg string, args ...any) { m.msgs = append(m.msgs, msg) }

type mockLifecycle struct {
	createDoc  func(ctx context.Context, params port.CreateDocumentParams) (*model.Document, error)
	getDoc     func(ctx context.Context, orgID, docID string) (*model.Document, error)
	listDocs   func(ctx context.Context, params port.ListDocumentsParams) (*port.PageResult[*model.Document], error)
	archiveDoc func(ctx context.Context, orgID, docID string) error
	deleteDoc  func(ctx context.Context, orgID, docID string) error
}

func (m *mockLifecycle) CreateDocument(ctx context.Context, params port.CreateDocumentParams) (*model.Document, error) {
	if m.createDoc != nil {
		return m.createDoc(ctx, params)
	}
	return nil, nil
}
func (m *mockLifecycle) GetDocument(ctx context.Context, orgID, docID string) (*model.Document, error) {
	if m.getDoc != nil {
		return m.getDoc(ctx, orgID, docID)
	}
	return nil, nil
}
func (m *mockLifecycle) ListDocuments(ctx context.Context, params port.ListDocumentsParams) (*port.PageResult[*model.Document], error) {
	if m.listDocs != nil {
		return m.listDocs(ctx, params)
	}
	return &port.PageResult[*model.Document]{Items: []*model.Document{}}, nil
}
func (m *mockLifecycle) ArchiveDocument(ctx context.Context, orgID, docID string) error {
	if m.archiveDoc != nil {
		return m.archiveDoc(ctx, orgID, docID)
	}
	return nil
}
func (m *mockLifecycle) DeleteDocument(ctx context.Context, orgID, docID string) error {
	if m.deleteDoc != nil {
		return m.deleteDoc(ctx, orgID, docID)
	}
	return nil
}

type mockVersions struct {
	createVersion func(ctx context.Context, params port.CreateVersionParams) (*model.DocumentVersion, error)
	getVersion    func(ctx context.Context, orgID, docID, versionID string) (*model.DocumentVersion, error)
	listVersions  func(ctx context.Context, params port.ListVersionsParams) (*port.PageResult[*model.DocumentVersion], error)
}

func (m *mockVersions) CreateVersion(ctx context.Context, params port.CreateVersionParams) (*model.DocumentVersion, error) {
	if m.createVersion != nil {
		return m.createVersion(ctx, params)
	}
	return nil, nil
}
func (m *mockVersions) GetVersion(ctx context.Context, orgID, docID, versionID string) (*model.DocumentVersion, error) {
	if m.getVersion != nil {
		return m.getVersion(ctx, orgID, docID, versionID)
	}
	return nil, nil
}
func (m *mockVersions) ListVersions(ctx context.Context, params port.ListVersionsParams) (*port.PageResult[*model.DocumentVersion], error) {
	if m.listVersions != nil {
		return m.listVersions(ctx, params)
	}
	return &port.PageResult[*model.DocumentVersion]{Items: []*model.DocumentVersion{}}, nil
}

type mockQueries struct {
	getArtifact           func(ctx context.Context, params port.GetArtifactParams) (*port.ArtifactContent, error)
	listArtifacts         func(ctx context.Context, orgID, docID, versionID string) ([]*model.ArtifactDescriptor, error)
	handleGetSemanticTree func(ctx context.Context, event model.GetSemanticTreeRequest) error
	handleGetArtifacts    func(ctx context.Context, event model.GetArtifactsRequest) error
}

func (m *mockQueries) GetArtifact(ctx context.Context, params port.GetArtifactParams) (*port.ArtifactContent, error) {
	if m.getArtifact != nil {
		return m.getArtifact(ctx, params)
	}
	return nil, nil
}
func (m *mockQueries) ListArtifacts(ctx context.Context, orgID, docID, versionID string) ([]*model.ArtifactDescriptor, error) {
	if m.listArtifacts != nil {
		return m.listArtifacts(ctx, orgID, docID, versionID)
	}
	return []*model.ArtifactDescriptor{}, nil
}
func (m *mockQueries) HandleGetSemanticTree(ctx context.Context, event model.GetSemanticTreeRequest) error {
	if m.handleGetSemanticTree != nil {
		return m.handleGetSemanticTree(ctx, event)
	}
	return nil
}
func (m *mockQueries) HandleGetArtifacts(ctx context.Context, event model.GetArtifactsRequest) error {
	if m.handleGetArtifacts != nil {
		return m.handleGetArtifacts(ctx, event)
	}
	return nil
}

type mockDiffs struct {
	getDiff    func(ctx context.Context, params port.GetDiffParams) (*model.VersionDiffReference, []byte, error)
	handleDiff func(ctx context.Context, event model.DocumentVersionDiffReady) error
}

func (m *mockDiffs) GetDiff(ctx context.Context, params port.GetDiffParams) (*model.VersionDiffReference, []byte, error) {
	if m.getDiff != nil {
		return m.getDiff(ctx, params)
	}
	return nil, nil, nil
}
func (m *mockDiffs) HandleDiffReady(ctx context.Context, event model.DocumentVersionDiffReady) error {
	if m.handleDiff != nil {
		return m.handleDiff(ctx, event)
	}
	return nil
}

type mockAudit struct {
	record func(ctx context.Context, rec *model.AuditRecord) error
	list   func(ctx context.Context, params port.AuditListParams) ([]*model.AuditRecord, int, error)
}

func (m *mockAudit) Record(ctx context.Context, rec *model.AuditRecord) error {
	if m.record != nil {
		return m.record(ctx, rec)
	}
	return nil
}
func (m *mockAudit) List(ctx context.Context, params port.AuditListParams) ([]*model.AuditRecord, int, error) {
	if m.list != nil {
		return m.list(ctx, params)
	}
	return []*model.AuditRecord{}, 0, nil
}

type mockStorage struct {
	putObject         func(ctx context.Context, key string, data io.Reader, contentType string) error
	getObject         func(ctx context.Context, key string) (io.ReadCloser, error)
	deleteObject      func(ctx context.Context, key string) error
	headObject        func(ctx context.Context, key string) (int64, bool, error)
	generatePresigned func(ctx context.Context, key string, expiry time.Duration) (string, error)
	deleteByPrefix    func(ctx context.Context, prefix string) error
}

func (m *mockStorage) PutObject(ctx context.Context, key string, data io.Reader, contentType string) error {
	if m.putObject != nil {
		return m.putObject(ctx, key, data, contentType)
	}
	return nil
}
func (m *mockStorage) GetObject(ctx context.Context, key string) (io.ReadCloser, error) {
	if m.getObject != nil {
		return m.getObject(ctx, key)
	}
	return io.NopCloser(strings.NewReader("")), nil
}
func (m *mockStorage) DeleteObject(ctx context.Context, key string) error {
	if m.deleteObject != nil {
		return m.deleteObject(ctx, key)
	}
	return nil
}
func (m *mockStorage) HeadObject(ctx context.Context, key string) (int64, bool, error) {
	if m.headObject != nil {
		return m.headObject(ctx, key)
	}
	return 0, false, nil
}
func (m *mockStorage) GeneratePresignedURL(ctx context.Context, key string, expiry time.Duration) (string, error) {
	if m.generatePresigned != nil {
		return m.generatePresigned(ctx, key, expiry)
	}
	return "https://s3.example.com/presigned", nil
}
func (m *mockStorage) DeleteByPrefix(ctx context.Context, prefix string) error {
	if m.deleteByPrefix != nil {
		return m.deleteByPrefix(ctx, prefix)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

type testDeps struct {
	lifecycle *mockLifecycle
	versions  *mockVersions
	queries   *mockQueries
	diffs     *mockDiffs
	audit     *mockAudit
	storage   *mockStorage
	logger    *mockLogger
	handler   http.Handler
}

func newTestDeps() *testDeps {
	d := &testDeps{
		lifecycle: &mockLifecycle{},
		versions:  &mockVersions{},
		queries:   &mockQueries{},
		diffs:     &mockDiffs{},
		audit:     &mockAudit{},
		storage:   &mockStorage{},
		logger:    &mockLogger{},
	}
	h := NewHandler(d.lifecycle, d.versions, d.queries, d.diffs, d.audit, d.storage, d.logger)
	d.handler = h.Mux(nil, nil)
	return d
}

func newTestDepsWithMetrics() (*testDeps, *prometheus.CounterVec, *prometheus.HistogramVec) {
	reg := prometheus.NewRegistry()
	apiReqs := prometheus.NewCounterVec(prometheus.CounterOpts{Name: "test_api_requests_total"}, []string{"method", "path", "status_code"})
	apiDur := prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "test_api_request_duration_seconds"}, []string{"method", "path"})
	reg.MustRegister(apiReqs, apiDur)

	d := &testDeps{
		lifecycle: &mockLifecycle{},
		versions:  &mockVersions{},
		queries:   &mockQueries{},
		diffs:     &mockDiffs{},
		audit:     &mockAudit{},
		storage:   &mockStorage{},
		logger:    &mockLogger{},
	}
	h := NewHandler(d.lifecycle, d.versions, d.queries, d.diffs, d.audit, d.storage, d.logger)
	d.handler = h.Mux(apiReqs, apiDur)
	return d, apiReqs, apiDur
}

func doRequest(handler http.Handler, method, path string, body any) *httptest.ResponseRecorder {
	return doRequestWithHeaders(handler, method, path, body, map[string]string{
		"X-Organization-ID": "org-1",
		"X-User-ID":         "user-1",
		"X-User-Role":       "admin",
	})
}

func doRequestWithHeaders(handler http.Handler, method, path string, body any, headers map[string]string) *httptest.ResponseRecorder {
	var reqBody io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		reqBody = bytes.NewReader(b)
	}

	req := httptest.NewRequest(method, path, reqBody)
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	return rr
}

// ---------------------------------------------------------------------------
// Constructor tests
// ---------------------------------------------------------------------------

func TestNewHandler_Panics(t *testing.T) {
	t.Parallel()

	deps := []struct {
		name string
		fn   func()
	}{
		{"nil lifecycle", func() { NewHandler(nil, &mockVersions{}, &mockQueries{}, &mockDiffs{}, &mockAudit{}, &mockStorage{}, &mockLogger{}) }},
		{"nil versions", func() { NewHandler(&mockLifecycle{}, nil, &mockQueries{}, &mockDiffs{}, &mockAudit{}, &mockStorage{}, &mockLogger{}) }},
		{"nil queries", func() { NewHandler(&mockLifecycle{}, &mockVersions{}, nil, &mockDiffs{}, &mockAudit{}, &mockStorage{}, &mockLogger{}) }},
		{"nil diffs", func() { NewHandler(&mockLifecycle{}, &mockVersions{}, &mockQueries{}, nil, &mockAudit{}, &mockStorage{}, &mockLogger{}) }},
		{"nil audit", func() { NewHandler(&mockLifecycle{}, &mockVersions{}, &mockQueries{}, &mockDiffs{}, nil, &mockStorage{}, &mockLogger{}) }},
		{"nil storage", func() { NewHandler(&mockLifecycle{}, &mockVersions{}, &mockQueries{}, &mockDiffs{}, &mockAudit{}, nil, &mockLogger{}) }},
		{"nil logger", func() { NewHandler(&mockLifecycle{}, &mockVersions{}, &mockQueries{}, &mockDiffs{}, &mockAudit{}, &mockStorage{}, nil) }},
	}

	for _, tc := range deps {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			defer func() {
				if r := recover(); r == nil {
					t.Errorf("expected panic for %s", tc.name)
				}
			}()
			tc.fn()
		})
	}
}

func TestNewHandler_Success(t *testing.T) {
	t.Parallel()
	h := NewHandler(&mockLifecycle{}, &mockVersions{}, &mockQueries{}, &mockDiffs{}, &mockAudit{}, &mockStorage{}, &mockLogger{})
	if h == nil {
		t.Fatal("expected non-nil handler")
	}
}

// ---------------------------------------------------------------------------
// Auth middleware tests
// ---------------------------------------------------------------------------

func TestAuth_MissingOrgID(t *testing.T) {
	t.Parallel()
	d := newTestDeps()
	rr := doRequestWithHeaders(d.handler, "GET", "/api/v1/documents", nil, map[string]string{
		"X-User-ID": "user-1",
	})
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rr.Code)
	}
}

func TestAuth_MissingUserID(t *testing.T) {
	t.Parallel()
	d := newTestDeps()
	rr := doRequestWithHeaders(d.handler, "GET", "/api/v1/documents", nil, map[string]string{
		"X-Organization-ID": "org-1",
	})
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rr.Code)
	}
}

func TestAuth_MissingBothHeaders(t *testing.T) {
	t.Parallel()
	d := newTestDeps()
	rr := doRequestWithHeaders(d.handler, "GET", "/api/v1/documents", nil, map[string]string{})
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rr.Code)
	}
}

func TestAuth_MalformedOrgID(t *testing.T) {
	t.Parallel()
	d := newTestDeps()
	rr := doRequestWithHeaders(d.handler, "GET", "/api/v1/documents", nil, map[string]string{
		"X-Organization-ID": "org/../evil",
		"X-User-ID":         "user-1",
	})
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for malformed org_id", rr.Code)
	}
}

func TestAuth_MalformedUserID(t *testing.T) {
	t.Parallel()
	d := newTestDeps()
	rr := doRequestWithHeaders(d.handler, "GET", "/api/v1/documents", nil, map[string]string{
		"X-Organization-ID": "org-1",
		"X-User-ID":         "user\ninjection",
	})
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for malformed user_id", rr.Code)
	}
}

func TestAuth_HeadersExtracted(t *testing.T) {
	t.Parallel()
	d := newTestDeps()

	var capturedOrgID, capturedUserID string
	d.lifecycle.createDoc = func(ctx context.Context, params port.CreateDocumentParams) (*model.Document, error) {
		capturedOrgID = params.OrganizationID
		capturedUserID = params.CreatedByUserID
		return model.NewDocument("doc-1", params.OrganizationID, params.Title, params.CreatedByUserID), nil
	}

	doRequest(d.handler, "POST", "/api/v1/documents", map[string]string{"title": "Test"})

	if capturedOrgID != "org-1" {
		t.Errorf("org_id = %q, want org-1", capturedOrgID)
	}
	if capturedUserID != "user-1" {
		t.Errorf("user_id = %q, want user-1", capturedUserID)
	}
}

// ---------------------------------------------------------------------------
// Document endpoint tests
// ---------------------------------------------------------------------------

func TestCreateDocument_Happy(t *testing.T) {
	t.Parallel()
	d := newTestDeps()
	d.lifecycle.createDoc = func(ctx context.Context, params port.CreateDocumentParams) (*model.Document, error) {
		return model.NewDocument("doc-1", params.OrganizationID, params.Title, params.CreatedByUserID), nil
	}

	rr := doRequest(d.handler, "POST", "/api/v1/documents", map[string]string{"title": "Contract A"})

	if rr.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201", rr.Code)
	}
	var doc model.Document
	if err := json.NewDecoder(rr.Body).Decode(&doc); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if doc.Title != "Contract A" {
		t.Errorf("title = %q, want 'Contract A'", doc.Title)
	}
	if doc.Status != model.DocumentStatusActive {
		t.Errorf("status = %q, want ACTIVE", doc.Status)
	}
}

func TestCreateDocument_EmptyTitle(t *testing.T) {
	t.Parallel()
	d := newTestDeps()
	rr := doRequest(d.handler, "POST", "/api/v1/documents", map[string]string{"title": ""})
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rr.Code)
	}
}

func TestCreateDocument_InvalidJSON(t *testing.T) {
	t.Parallel()
	d := newTestDeps()
	rr := doRequestWithHeaders(d.handler, "POST", "/api/v1/documents", nil, map[string]string{
		"X-Organization-ID": "org-1",
		"X-User-ID":         "user-1",
	})
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rr.Code)
	}
}

func TestCreateDocument_ServiceError(t *testing.T) {
	t.Parallel()
	d := newTestDeps()
	d.lifecycle.createDoc = func(ctx context.Context, params port.CreateDocumentParams) (*model.Document, error) {
		return nil, port.NewDatabaseError("db failed", nil)
	}

	rr := doRequest(d.handler, "POST", "/api/v1/documents", map[string]string{"title": "Test"})
	if rr.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rr.Code)
	}
}

func TestListDocuments_Happy(t *testing.T) {
	t.Parallel()
	d := newTestDeps()
	d.lifecycle.listDocs = func(ctx context.Context, params port.ListDocumentsParams) (*port.PageResult[*model.Document], error) {
		return &port.PageResult[*model.Document]{
			Items:      []*model.Document{model.NewDocument("doc-1", "org-1", "A", "u-1")},
			TotalCount: 1,
			Page:       1,
			PageSize:   20,
		}, nil
	}

	rr := doRequest(d.handler, "GET", "/api/v1/documents", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}

	var resp PaginatedResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Total != 1 {
		t.Errorf("total = %d, want 1", resp.Total)
	}
}

func TestListDocuments_StatusFilter(t *testing.T) {
	t.Parallel()
	d := newTestDeps()

	var capturedFilter *model.DocumentStatus
	d.lifecycle.listDocs = func(ctx context.Context, params port.ListDocumentsParams) (*port.PageResult[*model.Document], error) {
		capturedFilter = params.StatusFilter
		return &port.PageResult[*model.Document]{Items: []*model.Document{}}, nil
	}

	doRequest(d.handler, "GET", "/api/v1/documents?status=ARCHIVED", nil)

	if capturedFilter == nil || *capturedFilter != model.DocumentStatusArchived {
		t.Errorf("status filter = %v, want ARCHIVED", capturedFilter)
	}
}

func TestListDocuments_InvalidStatusFilter(t *testing.T) {
	t.Parallel()
	d := newTestDeps()
	rr := doRequest(d.handler, "GET", "/api/v1/documents?status=FOOBAR", nil)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for invalid status filter", rr.Code)
	}
}

func TestListDocuments_Pagination(t *testing.T) {
	t.Parallel()
	d := newTestDeps()

	var capturedPage, capturedSize int
	d.lifecycle.listDocs = func(ctx context.Context, params port.ListDocumentsParams) (*port.PageResult[*model.Document], error) {
		capturedPage = params.Page
		capturedSize = params.PageSize
		return &port.PageResult[*model.Document]{Items: []*model.Document{}, Page: params.Page, PageSize: params.PageSize}, nil
	}

	doRequest(d.handler, "GET", "/api/v1/documents?page=3&size=50", nil)

	if capturedPage != 3 {
		t.Errorf("page = %d, want 3", capturedPage)
	}
	if capturedSize != 50 {
		t.Errorf("size = %d, want 50", capturedSize)
	}
}

func TestListDocuments_SizeClampedAt100(t *testing.T) {
	t.Parallel()
	d := newTestDeps()

	var capturedSize int
	d.lifecycle.listDocs = func(ctx context.Context, params port.ListDocumentsParams) (*port.PageResult[*model.Document], error) {
		capturedSize = params.PageSize
		return &port.PageResult[*model.Document]{Items: []*model.Document{}}, nil
	}

	doRequest(d.handler, "GET", "/api/v1/documents?size=200", nil)

	if capturedSize != 100 {
		t.Errorf("size = %d, want 100 (clamped)", capturedSize)
	}
}

func TestGetDocument_Happy(t *testing.T) {
	t.Parallel()
	d := newTestDeps()
	d.lifecycle.getDoc = func(ctx context.Context, orgID, docID string) (*model.Document, error) {
		return model.NewDocument(docID, orgID, "Contract", "u-1"), nil
	}

	rr := doRequest(d.handler, "GET", "/api/v1/documents/doc-1", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
}

func TestGetDocument_NotFound(t *testing.T) {
	t.Parallel()
	d := newTestDeps()
	d.lifecycle.getDoc = func(ctx context.Context, orgID, docID string) (*model.Document, error) {
		return nil, port.NewDocumentNotFoundError(orgID, docID)
	}

	rr := doRequest(d.handler, "GET", "/api/v1/documents/doc-1", nil)
	if rr.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rr.Code)
	}
}

func TestDeleteDocument_Happy(t *testing.T) {
	t.Parallel()
	d := newTestDeps()
	d.lifecycle.deleteDoc = func(ctx context.Context, orgID, docID string) error {
		return nil
	}
	d.lifecycle.getDoc = func(ctx context.Context, orgID, docID string) (*model.Document, error) {
		doc := model.NewDocument(docID, orgID, "Contract", "u-1")
		doc.Status = model.DocumentStatusDeleted
		return doc, nil
	}

	rr := doRequest(d.handler, "DELETE", "/api/v1/documents/doc-1", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}

	var doc model.Document
	if err := json.NewDecoder(rr.Body).Decode(&doc); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if doc.Status != model.DocumentStatusDeleted {
		t.Errorf("status = %q, want DELETED", doc.Status)
	}
}

func TestDeleteDocument_Conflict(t *testing.T) {
	t.Parallel()
	d := newTestDeps()
	d.lifecycle.deleteDoc = func(ctx context.Context, orgID, docID string) error {
		return port.NewStatusTransitionError("DELETED", "DELETED")
	}

	rr := doRequest(d.handler, "DELETE", "/api/v1/documents/doc-1", nil)
	if rr.Code != http.StatusConflict {
		t.Errorf("status = %d, want 409", rr.Code)
	}
}

func TestArchiveDocument_Happy(t *testing.T) {
	t.Parallel()
	d := newTestDeps()
	d.lifecycle.archiveDoc = func(ctx context.Context, orgID, docID string) error {
		return nil
	}
	d.lifecycle.getDoc = func(ctx context.Context, orgID, docID string) (*model.Document, error) {
		doc := model.NewDocument(docID, orgID, "Contract", "u-1")
		doc.Status = model.DocumentStatusArchived
		return doc, nil
	}

	rr := doRequest(d.handler, "POST", "/api/v1/documents/doc-1/archive", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
}

func TestArchiveDocument_Conflict(t *testing.T) {
	t.Parallel()
	d := newTestDeps()
	d.lifecycle.archiveDoc = func(ctx context.Context, orgID, docID string) error {
		return port.NewStatusTransitionError("ARCHIVED", "ARCHIVED")
	}

	rr := doRequest(d.handler, "POST", "/api/v1/documents/doc-1/archive", nil)
	if rr.Code != http.StatusConflict {
		t.Errorf("status = %d, want 409", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// Version endpoint tests
// ---------------------------------------------------------------------------

func TestCreateVersion_Happy(t *testing.T) {
	t.Parallel()
	d := newTestDeps()
	d.versions.createVersion = func(ctx context.Context, params port.CreateVersionParams) (*model.DocumentVersion, error) {
		return model.NewDocumentVersion(
			"v-1", params.DocumentID, params.OrganizationID, 1,
			params.OriginType, params.SourceFileKey, params.SourceFileName,
			params.SourceFileSize, params.SourceFileChecksum, params.CreatedByUserID,
		), nil
	}

	body := createVersionRequest{
		SourceFileKey:      "files/test.pdf",
		SourceFileName:     "test.pdf",
		SourceFileSize:     1024,
		SourceFileChecksum: "abc123",
		OriginType:         "UPLOAD",
	}

	rr := doRequest(d.handler, "POST", "/api/v1/documents/doc-1/versions", body)
	if rr.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201", rr.Code)
	}

	var v model.DocumentVersion
	if err := json.NewDecoder(rr.Body).Decode(&v); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if v.VersionNumber != 1 {
		t.Errorf("version_number = %d, want 1", v.VersionNumber)
	}
}

func TestCreateVersion_InvalidOriginType(t *testing.T) {
	t.Parallel()
	d := newTestDeps()

	body := createVersionRequest{
		SourceFileKey:      "files/test.pdf",
		SourceFileName:     "test.pdf",
		SourceFileSize:     1024,
		SourceFileChecksum: "abc123",
		OriginType:         "INVALID_TYPE",
	}

	rr := doRequest(d.handler, "POST", "/api/v1/documents/doc-1/versions", body)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for invalid origin_type", rr.Code)
	}
}

func TestCreateVersion_InvalidJSON(t *testing.T) {
	t.Parallel()
	d := newTestDeps()
	rr := doRequestWithHeaders(d.handler, "POST", "/api/v1/documents/doc-1/versions", nil, map[string]string{
		"X-Organization-ID": "org-1",
		"X-User-ID":         "user-1",
	})
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rr.Code)
	}
}

func TestCreateVersion_DocNotFound(t *testing.T) {
	t.Parallel()
	d := newTestDeps()
	d.versions.createVersion = func(ctx context.Context, params port.CreateVersionParams) (*model.DocumentVersion, error) {
		return nil, port.NewDocumentNotFoundError(params.OrganizationID, params.DocumentID)
	}

	body := createVersionRequest{
		SourceFileKey:      "files/test.pdf",
		SourceFileName:     "test.pdf",
		SourceFileSize:     1024,
		SourceFileChecksum: "abc123",
		OriginType:         "UPLOAD",
	}

	rr := doRequest(d.handler, "POST", "/api/v1/documents/doc-1/versions", body)
	if rr.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rr.Code)
	}
}

func TestListVersions_Happy(t *testing.T) {
	t.Parallel()
	d := newTestDeps()
	d.versions.listVersions = func(ctx context.Context, params port.ListVersionsParams) (*port.PageResult[*model.DocumentVersion], error) {
		return &port.PageResult[*model.DocumentVersion]{
			Items:      []*model.DocumentVersion{},
			TotalCount: 0,
			Page:       1,
			PageSize:   20,
		}, nil
	}

	rr := doRequest(d.handler, "GET", "/api/v1/documents/doc-1/versions", nil)
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rr.Code)
	}
}

func TestGetVersion_Happy(t *testing.T) {
	t.Parallel()
	d := newTestDeps()
	d.versions.getVersion = func(ctx context.Context, orgID, docID, versionID string) (*model.DocumentVersion, error) {
		return model.NewDocumentVersion(
			versionID, docID, orgID, 1,
			model.OriginTypeUpload, "key", "file.pdf", 1024, "sha", "u-1",
		), nil
	}
	d.queries.listArtifacts = func(ctx context.Context, orgID, docID, versionID string) ([]*model.ArtifactDescriptor, error) {
		return []*model.ArtifactDescriptor{}, nil
	}

	rr := doRequest(d.handler, "GET", "/api/v1/documents/doc-1/versions/v-1", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}

	var resp map[string]json.RawMessage
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, ok := resp["artifacts"]; !ok {
		t.Error("response missing 'artifacts' field")
	}
}

func TestGetVersion_NotFound(t *testing.T) {
	t.Parallel()
	d := newTestDeps()
	d.versions.getVersion = func(ctx context.Context, orgID, docID, versionID string) (*model.DocumentVersion, error) {
		return nil, port.NewVersionNotFoundError(versionID)
	}

	rr := doRequest(d.handler, "GET", "/api/v1/documents/doc-1/versions/v-1", nil)
	if rr.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rr.Code)
	}
}

func TestGetVersion_ArtifactListError(t *testing.T) {
	t.Parallel()
	d := newTestDeps()
	d.versions.getVersion = func(ctx context.Context, orgID, docID, versionID string) (*model.DocumentVersion, error) {
		return model.NewDocumentVersion(versionID, docID, orgID, 1, model.OriginTypeUpload, "key", "file.pdf", 1024, "sha", "u-1"), nil
	}
	d.queries.listArtifacts = func(ctx context.Context, orgID, docID, versionID string) ([]*model.ArtifactDescriptor, error) {
		return nil, port.NewDatabaseError("db fail", nil)
	}

	rr := doRequest(d.handler, "GET", "/api/v1/documents/doc-1/versions/v-1", nil)
	if rr.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// Artifact endpoint tests
// ---------------------------------------------------------------------------

func TestListArtifacts_Happy(t *testing.T) {
	t.Parallel()
	d := newTestDeps()
	d.queries.listArtifacts = func(ctx context.Context, orgID, docID, versionID string) ([]*model.ArtifactDescriptor, error) {
		return []*model.ArtifactDescriptor{
			{ArtifactID: "a-1", ArtifactType: model.ArtifactTypeSemanticTree, ProducerDomain: model.ProducerDomainDP},
			{ArtifactID: "a-2", ArtifactType: model.ArtifactTypeRiskAnalysis, ProducerDomain: model.ProducerDomainLIC},
		}, nil
	}

	rr := doRequest(d.handler, "GET", "/api/v1/documents/doc-1/versions/v-1/artifacts", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}

	var resp struct {
		Items []model.ArtifactDescriptor `json:"items"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Items) != 2 {
		t.Errorf("items count = %d, want 2", len(resp.Items))
	}
}

func TestListArtifacts_FilterByType(t *testing.T) {
	t.Parallel()
	d := newTestDeps()
	d.queries.listArtifacts = func(ctx context.Context, orgID, docID, versionID string) ([]*model.ArtifactDescriptor, error) {
		return []*model.ArtifactDescriptor{
			{ArtifactID: "a-1", ArtifactType: model.ArtifactTypeSemanticTree, ProducerDomain: model.ProducerDomainDP},
			{ArtifactID: "a-2", ArtifactType: model.ArtifactTypeRiskAnalysis, ProducerDomain: model.ProducerDomainLIC},
		}, nil
	}

	rr := doRequest(d.handler, "GET", "/api/v1/documents/doc-1/versions/v-1/artifacts?artifact_type=SEMANTIC_TREE", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}

	var resp struct {
		Items []model.ArtifactDescriptor `json:"items"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Items) != 1 {
		t.Errorf("items count = %d, want 1", len(resp.Items))
	}
}

func TestListArtifacts_FilterByProducer(t *testing.T) {
	t.Parallel()
	d := newTestDeps()
	d.queries.listArtifacts = func(ctx context.Context, orgID, docID, versionID string) ([]*model.ArtifactDescriptor, error) {
		return []*model.ArtifactDescriptor{
			{ArtifactID: "a-1", ArtifactType: model.ArtifactTypeSemanticTree, ProducerDomain: model.ProducerDomainDP},
			{ArtifactID: "a-2", ArtifactType: model.ArtifactTypeRiskAnalysis, ProducerDomain: model.ProducerDomainLIC},
		}, nil
	}

	rr := doRequest(d.handler, "GET", "/api/v1/documents/doc-1/versions/v-1/artifacts?producer_domain=LIC", nil)

	var resp struct {
		Items []model.ArtifactDescriptor `json:"items"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Items) != 1 {
		t.Errorf("items count = %d, want 1 (filtered by LIC)", len(resp.Items))
	}
	if resp.Items[0].ArtifactType != model.ArtifactTypeRiskAnalysis {
		t.Errorf("artifact_type = %s, want RISK_ANALYSIS", resp.Items[0].ArtifactType)
	}
}

func TestGetArtifact_JSON_Happy(t *testing.T) {
	t.Parallel()
	d := newTestDeps()
	d.queries.getArtifact = func(ctx context.Context, params port.GetArtifactParams) (*port.ArtifactContent, error) {
		return &port.ArtifactContent{
			Content:     []byte(`{"tree": "data"}`),
			ContentType: "application/json",
		}, nil
	}

	rr := doRequest(d.handler, "GET", "/api/v1/documents/doc-1/versions/v-1/artifacts/SEMANTIC_TREE", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	if ct := rr.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	if nosniff := rr.Header().Get("X-Content-Type-Options"); nosniff != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want nosniff", nosniff)
	}
}

func TestGetArtifact_Blob_Redirect(t *testing.T) {
	t.Parallel()
	d := newTestDeps()
	d.queries.listArtifacts = func(ctx context.Context, orgID, docID, versionID string) ([]*model.ArtifactDescriptor, error) {
		return []*model.ArtifactDescriptor{
			{ArtifactID: "a-1", ArtifactType: model.ArtifactTypeExportPDF, StorageKey: "org-1/doc-1/v-1/EXPORT_PDF"},
		}, nil
	}
	d.storage.generatePresigned = func(ctx context.Context, key string, expiry time.Duration) (string, error) {
		if key != "org-1/doc-1/v-1/EXPORT_PDF" {
			t.Errorf("presigned key = %q, want org-1/doc-1/v-1/EXPORT_PDF", key)
		}
		return "https://s3.example.com/presigned-pdf", nil
	}

	rr := doRequest(d.handler, "GET", "/api/v1/documents/doc-1/versions/v-1/artifacts/EXPORT_PDF", nil)
	if rr.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", rr.Code)
	}
	loc := rr.Header().Get("Location")
	if loc != "https://s3.example.com/presigned-pdf" {
		t.Errorf("Location = %q, want presigned URL", loc)
	}
}

func TestGetArtifact_Blob_NotFound(t *testing.T) {
	t.Parallel()
	d := newTestDeps()
	d.queries.listArtifacts = func(ctx context.Context, orgID, docID, versionID string) ([]*model.ArtifactDescriptor, error) {
		return []*model.ArtifactDescriptor{}, nil // No EXPORT_PDF
	}

	rr := doRequest(d.handler, "GET", "/api/v1/documents/doc-1/versions/v-1/artifacts/EXPORT_PDF", nil)
	if rr.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rr.Code)
	}
}

func TestGetArtifact_PresignedURLError(t *testing.T) {
	t.Parallel()
	d := newTestDeps()
	d.queries.listArtifacts = func(ctx context.Context, orgID, docID, versionID string) ([]*model.ArtifactDescriptor, error) {
		return []*model.ArtifactDescriptor{
			{ArtifactID: "a-1", ArtifactType: model.ArtifactTypeExportPDF, StorageKey: "key"},
		}, nil
	}
	d.storage.generatePresigned = func(ctx context.Context, key string, expiry time.Duration) (string, error) {
		return "", port.NewStorageError("s3 fail", nil)
	}

	rr := doRequest(d.handler, "GET", "/api/v1/documents/doc-1/versions/v-1/artifacts/EXPORT_PDF", nil)
	if rr.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rr.Code)
	}
}

func TestGetArtifact_UnknownType(t *testing.T) {
	t.Parallel()
	d := newTestDeps()
	rr := doRequest(d.handler, "GET", "/api/v1/documents/doc-1/versions/v-1/artifacts/UNKNOWN_TYPE", nil)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for unknown artifact type", rr.Code)
	}
}

func TestGetArtifact_NotFound(t *testing.T) {
	t.Parallel()
	d := newTestDeps()
	d.queries.getArtifact = func(ctx context.Context, params port.GetArtifactParams) (*port.ArtifactContent, error) {
		return nil, port.NewArtifactNotFoundError(params.VersionID, string(params.ArtifactType))
	}

	rr := doRequest(d.handler, "GET", "/api/v1/documents/doc-1/versions/v-1/artifacts/SEMANTIC_TREE", nil)
	if rr.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// Diff endpoint tests
// ---------------------------------------------------------------------------

func TestGetDiff_Happy(t *testing.T) {
	t.Parallel()
	d := newTestDeps()
	d.diffs.getDiff = func(ctx context.Context, params port.GetDiffParams) (*model.VersionDiffReference, []byte, error) {
		ref := &model.VersionDiffReference{
			DiffID:              "diff-1",
			DocumentID:          params.DocumentID,
			BaseVersionID:       params.BaseVersionID,
			TargetVersionID:     params.TargetVersionID,
			TextDiffCount:       2,
			StructuralDiffCount: 1,
			CreatedAt:           time.Now().UTC(),
		}
		blob := `{"text_diffs":[{"type":"added","path":"1.1"}],"structural_diffs":[{"type":"removed","node_id":"n1"}]}`
		return ref, []byte(blob), nil
	}

	rr := doRequest(d.handler, "GET", "/api/v1/documents/doc-1/diffs/base-v/target-v", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}

	var resp diffResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.TextDiffCount != 2 {
		t.Errorf("text_diff_count = %d, want 2", resp.TextDiffCount)
	}
	if resp.StructuralDiffCount != 1 {
		t.Errorf("structural_diff_count = %d, want 1", resp.StructuralDiffCount)
	}
}

func TestGetDiff_NotFound(t *testing.T) {
	t.Parallel()
	d := newTestDeps()
	d.diffs.getDiff = func(ctx context.Context, params port.GetDiffParams) (*model.VersionDiffReference, []byte, error) {
		return nil, nil, port.NewDiffNotFoundError(params.BaseVersionID, params.TargetVersionID)
	}

	rr := doRequest(d.handler, "GET", "/api/v1/documents/doc-1/diffs/base-v/target-v", nil)
	if rr.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rr.Code)
	}
}

func TestGetDiff_MalformedBlob(t *testing.T) {
	t.Parallel()
	d := newTestDeps()
	d.diffs.getDiff = func(ctx context.Context, params port.GetDiffParams) (*model.VersionDiffReference, []byte, error) {
		return &model.VersionDiffReference{DiffID: "d-1"}, []byte("not-json"), nil
	}

	rr := doRequest(d.handler, "GET", "/api/v1/documents/doc-1/diffs/base-v/target-v", nil)
	if rr.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500 for malformed blob", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// Audit endpoint tests
// ---------------------------------------------------------------------------

func TestListAuditRecords_Happy(t *testing.T) {
	t.Parallel()
	d := newTestDeps()
	d.audit.list = func(ctx context.Context, params port.AuditListParams) ([]*model.AuditRecord, int, error) {
		return []*model.AuditRecord{
			model.NewAuditRecord("a-1", params.OrganizationID, model.AuditActionDocumentCreated, model.ActorTypeUser, "user-1"),
		}, 1, nil
	}

	rr := doRequest(d.handler, "GET", "/api/v1/audit", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}

	var resp PaginatedResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Total != 1 {
		t.Errorf("total = %d, want 1", resp.Total)
	}
}

func TestListAuditRecords_Filters(t *testing.T) {
	t.Parallel()
	d := newTestDeps()

	var capturedParams port.AuditListParams
	d.audit.list = func(ctx context.Context, params port.AuditListParams) ([]*model.AuditRecord, int, error) {
		capturedParams = params
		return []*model.AuditRecord{}, 0, nil
	}

	doRequest(d.handler, "GET", "/api/v1/audit?document_id=doc-1&action=ARTIFACT_SAVED&actor_id=DP&from=2026-01-01T00:00:00Z&to=2026-12-31T23:59:59Z", nil)

	if capturedParams.DocumentID != "doc-1" {
		t.Errorf("document_id = %q, want doc-1", capturedParams.DocumentID)
	}
	if capturedParams.Action == nil || *capturedParams.Action != model.AuditActionArtifactSaved {
		t.Errorf("action = %v, want ARTIFACT_SAVED", capturedParams.Action)
	}
	if capturedParams.ActorID != "DP" {
		t.Errorf("actor_id = %q, want DP", capturedParams.ActorID)
	}
	if capturedParams.Since == nil {
		t.Error("from filter not parsed")
	}
	if capturedParams.Until == nil {
		t.Error("to filter not parsed")
	}
}

func TestListAuditRecords_InvalidFromDate(t *testing.T) {
	t.Parallel()
	d := newTestDeps()
	rr := doRequest(d.handler, "GET", "/api/v1/audit?from=not-a-date", nil)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for invalid from date", rr.Code)
	}
}

func TestListAuditRecords_InvalidToDate(t *testing.T) {
	t.Parallel()
	d := newTestDeps()
	rr := doRequest(d.handler, "GET", "/api/v1/audit?to=not-a-date", nil)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for invalid to date", rr.Code)
	}
}

func TestListAuditRecords_ServiceError(t *testing.T) {
	t.Parallel()
	d := newTestDeps()
	d.audit.list = func(ctx context.Context, params port.AuditListParams) ([]*model.AuditRecord, int, error) {
		return nil, 0, port.NewDatabaseError("db fail", nil)
	}

	rr := doRequest(d.handler, "GET", "/api/v1/audit", nil)
	if rr.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// Error mapping tests
// ---------------------------------------------------------------------------

func TestErrorMapping_TenantMismatch_Returns404(t *testing.T) {
	t.Parallel()
	d := newTestDeps()
	d.lifecycle.getDoc = func(ctx context.Context, orgID, docID string) (*model.Document, error) {
		return nil, port.NewTenantMismatchError(docID, "org-2", orgID)
	}

	rr := doRequest(d.handler, "GET", "/api/v1/documents/doc-1", nil)
	if rr.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404 (tenant mismatch hidden)", rr.Code)
	}
}

func TestErrorMapping_Validation_Returns400(t *testing.T) {
	t.Parallel()
	d := newTestDeps()
	d.lifecycle.createDoc = func(ctx context.Context, params port.CreateDocumentParams) (*model.Document, error) {
		return nil, port.NewValidationError("title too long")
	}

	rr := doRequest(d.handler, "POST", "/api/v1/documents", map[string]string{"title": "x"})
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// Middleware tests
// ---------------------------------------------------------------------------

func TestMiddleware_Metrics(t *testing.T) {
	t.Parallel()
	d, _, _ := newTestDepsWithMetrics()
	d.lifecycle.listDocs = func(ctx context.Context, params port.ListDocumentsParams) (*port.PageResult[*model.Document], error) {
		return &port.PageResult[*model.Document]{Items: []*model.Document{}}, nil
	}

	rr := doRequest(d.handler, "GET", "/api/v1/documents", nil)
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rr.Code)
	}
}

func TestMiddleware_Logging(t *testing.T) {
	t.Parallel()
	d := newTestDeps()
	d.lifecycle.listDocs = func(ctx context.Context, params port.ListDocumentsParams) (*port.PageResult[*model.Document], error) {
		return &port.PageResult[*model.Document]{Items: []*model.Document{}}, nil
	}

	doRequest(d.handler, "GET", "/api/v1/documents", nil)

	if len(d.logger.msgs) == 0 {
		t.Error("expected at least one log message from middleware")
	}
	found := false
	for _, msg := range d.logger.msgs {
		if msg == "api request" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected 'api request' log, got: %v", d.logger.msgs)
	}
}

// ---------------------------------------------------------------------------
// Content-Type / nosniff tests
// ---------------------------------------------------------------------------

func TestResponse_ContentType_JSON(t *testing.T) {
	t.Parallel()
	d := newTestDeps()
	d.lifecycle.listDocs = func(ctx context.Context, params port.ListDocumentsParams) (*port.PageResult[*model.Document], error) {
		return &port.PageResult[*model.Document]{Items: []*model.Document{}}, nil
	}

	rr := doRequest(d.handler, "GET", "/api/v1/documents", nil)
	ct := rr.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	nosniff := rr.Header().Get("X-Content-Type-Options")
	if nosniff != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want nosniff", nosniff)
	}
}

// ---------------------------------------------------------------------------
// Pagination defaults
// ---------------------------------------------------------------------------

func TestParsePagination_Defaults(t *testing.T) {
	t.Parallel()
	req := httptest.NewRequest("GET", "/test", nil)
	page, size := parsePagination(req)
	if page != 1 {
		t.Errorf("page = %d, want 1", page)
	}
	if size != 20 {
		t.Errorf("size = %d, want 20", size)
	}
}

func TestParsePagination_InvalidValues(t *testing.T) {
	t.Parallel()
	req := httptest.NewRequest("GET", "/test?page=-1&size=abc", nil)
	page, size := parsePagination(req)
	if page != 1 {
		t.Errorf("page = %d, want 1 (default for invalid)", page)
	}
	if size != 20 {
		t.Errorf("size = %d, want 20 (default for invalid)", size)
	}
}

func TestParsePagination_PageZero(t *testing.T) {
	t.Parallel()
	req := httptest.NewRequest("GET", "/test?page=0", nil)
	page, _ := parsePagination(req)
	if page != 1 {
		t.Errorf("page = %d, want 1 (default for page=0)", page)
	}
}

// ---------------------------------------------------------------------------
// Validation helpers
// ---------------------------------------------------------------------------

func TestIsValidDocumentStatus(t *testing.T) {
	t.Parallel()
	if !isValidDocumentStatus(model.DocumentStatusActive) {
		t.Error("ACTIVE should be valid")
	}
	if isValidDocumentStatus("FOOBAR") {
		t.Error("FOOBAR should not be valid")
	}
}

func TestIsValidOriginType(t *testing.T) {
	t.Parallel()
	if !isValidOriginType(model.OriginTypeUpload) {
		t.Error("UPLOAD should be valid")
	}
	if isValidOriginType("BAD_TYPE") {
		t.Error("BAD_TYPE should not be valid")
	}
}

func TestIsValidArtifactType(t *testing.T) {
	t.Parallel()
	if !isValidArtifactType(model.ArtifactTypeSemanticTree) {
		t.Error("SEMANTIC_TREE should be valid")
	}
	if isValidArtifactType("UNKNOWN") {
		t.Error("UNKNOWN should not be valid")
	}
}

// ---------------------------------------------------------------------------
// filterArtifacts tests
// ---------------------------------------------------------------------------

func TestFilterArtifacts_NoFilter(t *testing.T) {
	t.Parallel()
	arts := []*model.ArtifactDescriptor{
		{ArtifactID: "a-1", ArtifactType: "A", ProducerDomain: "X"},
		{ArtifactID: "a-2", ArtifactType: "B", ProducerDomain: "Y"},
	}
	result := filterArtifacts(arts, "", "")
	if len(result) != 2 {
		t.Errorf("len = %d, want 2", len(result))
	}
}

func TestFilterArtifacts_ByType(t *testing.T) {
	t.Parallel()
	arts := []*model.ArtifactDescriptor{
		{ArtifactID: "a-1", ArtifactType: "A", ProducerDomain: "X"},
		{ArtifactID: "a-2", ArtifactType: "B", ProducerDomain: "Y"},
	}
	result := filterArtifacts(arts, "A", "")
	if len(result) != 1 {
		t.Errorf("len = %d, want 1", len(result))
	}
}

func TestFilterArtifacts_ByProducer(t *testing.T) {
	t.Parallel()
	arts := []*model.ArtifactDescriptor{
		{ArtifactID: "a-1", ArtifactType: "A", ProducerDomain: "X"},
		{ArtifactID: "a-2", ArtifactType: "B", ProducerDomain: "Y"},
	}
	result := filterArtifacts(arts, "", "Y")
	if len(result) != 1 {
		t.Errorf("len = %d, want 1", len(result))
	}
}

func TestFilterArtifacts_BothFilters(t *testing.T) {
	t.Parallel()
	arts := []*model.ArtifactDescriptor{
		{ArtifactID: "a-1", ArtifactType: "A", ProducerDomain: "X"},
		{ArtifactID: "a-2", ArtifactType: "A", ProducerDomain: "Y"},
	}
	result := filterArtifacts(arts, "A", "X")
	if len(result) != 1 || result[0].ArtifactID != "a-1" {
		t.Errorf("expected a-1, got %v", result)
	}
}

// ---------------------------------------------------------------------------
// Mux returns non-nil
// ---------------------------------------------------------------------------

func TestMux_NonNil(t *testing.T) {
	t.Parallel()
	h := NewHandler(&mockLifecycle{}, &mockVersions{}, &mockQueries{}, &mockDiffs{}, &mockAudit{}, &mockStorage{}, &mockLogger{})
	mux := h.Mux(nil, nil)
	if mux == nil {
		t.Fatal("Mux() returned nil")
	}
}

// ---------------------------------------------------------------------------
// responseWriter tests
// ---------------------------------------------------------------------------

func TestResponseWriter_Flush(t *testing.T) {
	t.Parallel()
	rr := httptest.NewRecorder()
	rw := &responseWriter{ResponseWriter: rr, statusCode: http.StatusOK}
	// Should not panic even if underlying writer supports Flush.
	rw.Flush()
}

func TestResponseWriter_Unwrap(t *testing.T) {
	t.Parallel()
	rr := httptest.NewRecorder()
	rw := &responseWriter{ResponseWriter: rr, statusCode: http.StatusOK}
	if rw.Unwrap() != rr {
		t.Error("Unwrap should return the underlying writer")
	}
}

func TestResponseWriter_DoubleWriteHeader(t *testing.T) {
	t.Parallel()
	rr := httptest.NewRecorder()
	rw := &responseWriter{ResponseWriter: rr, statusCode: http.StatusOK}
	rw.WriteHeader(http.StatusCreated)
	rw.WriteHeader(http.StatusNotFound) // should be ignored
	if rw.statusCode != http.StatusCreated {
		t.Errorf("statusCode = %d, want 201 (first call wins)", rw.statusCode)
	}
}
