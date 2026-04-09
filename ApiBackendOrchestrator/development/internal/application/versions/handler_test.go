package versions

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"contractpro/api-orchestrator/internal/egress/commandpub"
	"contractpro/api-orchestrator/internal/egress/dmclient"
	"contractpro/api-orchestrator/internal/infra/observability/logger"
	"contractpro/api-orchestrator/internal/ingress/middleware/auth"
)

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

type mockDMClient struct {
	getDocFn     func(ctx context.Context, documentID string) (*dmclient.DocumentWithCurrentVersion, error)
	listVerFn    func(ctx context.Context, documentID string, params dmclient.ListVersionsParams) (*dmclient.VersionList, error)
	getVerFn     func(ctx context.Context, documentID, versionID string) (*dmclient.DocumentVersionWithArtifacts, error)
	createVerFn  func(ctx context.Context, documentID string, req dmclient.CreateVersionRequest) (*dmclient.DocumentVersion, error)
	getDocCalls  []string
	listVerCalls []listVerCall
	getVerCalls  []getVerCall
	createCalls  []createCall
}

type listVerCall struct {
	DocumentID string
	Params     dmclient.ListVersionsParams
}

type getVerCall struct {
	DocumentID string
	VersionID  string
}

type createCall struct {
	DocumentID string
	Req        dmclient.CreateVersionRequest
}

func (m *mockDMClient) GetDocument(ctx context.Context, documentID string) (*dmclient.DocumentWithCurrentVersion, error) {
	m.getDocCalls = append(m.getDocCalls, documentID)
	if m.getDocFn != nil {
		return m.getDocFn(ctx, documentID)
	}
	return stubDocWithVersion("FULLY_READY"), nil
}

func (m *mockDMClient) ListVersions(ctx context.Context, documentID string, params dmclient.ListVersionsParams) (*dmclient.VersionList, error) {
	m.listVerCalls = append(m.listVerCalls, listVerCall{DocumentID: documentID, Params: params})
	if m.listVerFn != nil {
		return m.listVerFn(ctx, documentID, params)
	}
	return &dmclient.VersionList{Items: []dmclient.DocumentVersion{}, Total: 0, Page: params.Page, Size: params.Size}, nil
}

func (m *mockDMClient) GetVersion(ctx context.Context, documentID, versionID string) (*dmclient.DocumentVersionWithArtifacts, error) {
	m.getVerCalls = append(m.getVerCalls, getVerCall{DocumentID: documentID, VersionID: versionID})
	if m.getVerFn != nil {
		return m.getVerFn(ctx, documentID, versionID)
	}
	return stubVersionWithArtifacts("FULLY_READY"), nil
}

func (m *mockDMClient) CreateVersion(ctx context.Context, documentID string, req dmclient.CreateVersionRequest) (*dmclient.DocumentVersion, error) {
	m.createCalls = append(m.createCalls, createCall{DocumentID: documentID, Req: req})
	if m.createVerFn != nil {
		return m.createVerFn(ctx, documentID, req)
	}
	return &dmclient.DocumentVersion{VersionID: "ver-002", VersionNumber: 2}, nil
}

type mockStorage struct {
	putFn       func(ctx context.Context, key string, data io.ReadSeeker, contentType string) error
	deleteFn    func(ctx context.Context, key string) error
	putCalls    []putCall
	deleteCalls []string
}

type putCall struct {
	Key         string
	ContentType string
	Data        []byte
}

func (m *mockStorage) PutObject(ctx context.Context, key string, data io.ReadSeeker, contentType string) error {
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, data); err != nil {
		return err
	}
	m.putCalls = append(m.putCalls, putCall{Key: key, ContentType: contentType, Data: buf.Bytes()})
	if m.putFn != nil {
		return m.putFn(ctx, key, data, contentType)
	}
	return nil
}

func (m *mockStorage) DeleteObject(ctx context.Context, key string) error {
	m.deleteCalls = append(m.deleteCalls, key)
	if m.deleteFn != nil {
		return m.deleteFn(ctx, key)
	}
	return nil
}

type mockPublisher struct {
	publishFn func(ctx context.Context, cmd commandpub.ProcessDocumentCommand) error
	calls     []commandpub.ProcessDocumentCommand
}

func (m *mockPublisher) PublishProcessDocument(ctx context.Context, cmd commandpub.ProcessDocumentCommand) error {
	m.calls = append(m.calls, cmd)
	if m.publishFn != nil {
		return m.publishFn(ctx, cmd)
	}
	return nil
}

type mockKVStore struct {
	setFn func(ctx context.Context, key string, value string, ttl time.Duration) error
	calls []kvSetCall
}

type kvSetCall struct {
	Key   string
	Value string
	TTL   time.Duration
}

func (m *mockKVStore) Set(ctx context.Context, key string, value string, ttl time.Duration) error {
	m.calls = append(m.calls, kvSetCall{Key: key, Value: value, TTL: ttl})
	if m.setFn != nil {
		return m.setFn(ctx, key, value, ttl)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

const (
	testContractID = "550e8400-e29b-41d4-a716-446655440000"
	testVersionID  = "660e8400-e29b-41d4-a716-446655440000"
)

func newTestHandler(dm *mockDMClient) *Handler {
	return newTestHandlerFull(dm, &mockStorage{}, &mockPublisher{}, &mockKVStore{})
}

func newTestHandlerFull(dm *mockDMClient, storage *mockStorage, pub *mockPublisher, kv *mockKVStore) *Handler {
	h := NewHandler(dm, storage, pub, kv, logger.NewLogger("error"), 20<<20)
	counter := 0
	h.uuidGen = func() string {
		counter++
		return fmt.Sprintf("uuid-%03d", counter)
	}
	return h
}

func withAuthContext(r *http.Request) *http.Request {
	ctx := auth.WithAuthContext(r.Context(), auth.AuthContext{
		UserID:         "user-001",
		OrganizationID: "org-001",
		Role:           auth.RoleLawyer,
		TokenID:        "token-001",
	})
	return r.WithContext(ctx)
}

func withChiParams(r *http.Request, params map[string]string) *http.Request {
	rctx := chi.NewRouteContext()
	for k, v := range params {
		rctx.URLParams.Add(k, v)
	}
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}

func parseJSON(t *testing.T, rr *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var result map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &result); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}
	return result
}

func strPtr(s string) *string { return &s }

func stubVersion(artifactStatus string) dmclient.DocumentVersion {
	return dmclient.DocumentVersion{
		VersionID:          testVersionID,
		DocumentID:         testContractID,
		VersionNumber:      1,
		ParentVersionID:    nil,
		OriginType:         "UPLOAD",
		OriginDescription:  nil,
		SourceFileKey:      "uploads/org-001/key.pdf",
		SourceFileName:     "contract.pdf",
		SourceFileSize:     1024000,
		SourceFileChecksum: "abc123",
		ArtifactStatus:     artifactStatus,
		CreatedByUserID:    "user-001",
		CreatedAt:          time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC),
	}
}

func stubVersionWithArtifacts(artifactStatus string) *dmclient.DocumentVersionWithArtifacts {
	return &dmclient.DocumentVersionWithArtifacts{
		DocumentVersion: stubVersion(artifactStatus),
		Artifacts:       []dmclient.ArtifactDescriptor{},
	}
}

func stubDocWithVersion(artifactStatus string) *dmclient.DocumentWithCurrentVersion {
	v := stubVersion(artifactStatus)
	return &dmclient.DocumentWithCurrentVersion{
		Document: dmclient.Document{
			DocumentID:       testContractID,
			OrganizationID:   "org-001",
			Title:            "Договор поставки №42",
			CurrentVersionID: strPtr(testVersionID),
			Status:           "ACTIVE",
			CreatedByUserID:  "user-001",
			CreatedAt:        time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC),
			UpdatedAt:        time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC),
		},
		CurrentVersion: &v,
	}
}

func dmHTTPError(op string, code int, body string) *dmclient.DMError {
	return &dmclient.DMError{
		Operation:  op,
		StatusCode: code,
		Body:       []byte(body),
		Retryable:  code >= 500,
	}
}

func validPDFContent() []byte {
	content := make([]byte, 100)
	copy(content, []byte("%PDF-1.7 test content"))
	return content
}

func createMultipartRequest(t *testing.T, fileName string, fileContent []byte) *http.Request {
	t.Helper()
	var body bytes.Buffer
	w := multipart.NewWriter(&body)

	if fileContent != nil {
		partHeader := make(map[string][]string)
		partHeader["Content-Disposition"] = []string{
			fmt.Sprintf(`form-data; name="file"; filename="%s"`, fileName),
		}
		partHeader["Content-Type"] = []string{"application/pdf"}
		part, err := w.CreatePart(partHeader)
		if err != nil {
			t.Fatalf("create form file: %v", err)
		}
		if _, err := part.Write(fileContent); err != nil {
			t.Fatalf("write file content: %v", err)
		}
	}

	if err := w.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/contracts/"+testContractID+"/versions/upload", &body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	return req
}

// =========================================================================
// HandleList tests
// =========================================================================

func TestHandleList_Success(t *testing.T) {
	dm := &mockDMClient{
		listVerFn: func(_ context.Context, _ string, _ dmclient.ListVersionsParams) (*dmclient.VersionList, error) {
			return &dmclient.VersionList{
				Items: []dmclient.DocumentVersion{stubVersion("FULLY_READY"), stubVersion("PENDING_PROCESSING")},
				Total: 2,
				Page:  1,
				Size:  20,
			}, nil
		},
	}
	h := newTestHandler(dm)

	rr := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/contracts/"+testContractID+"/versions", nil)
	r = withAuthContext(r)
	r = withChiParams(r, map[string]string{"contract_id": testContractID})

	h.HandleList().ServeHTTP(rr, r)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	body := parseJSON(t, rr)
	items := body["items"].([]any)
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}

	item := items[0].(map[string]any)
	if item["contract_id"] != testContractID {
		t.Errorf("contract_id = %v, want %s", item["contract_id"], testContractID)
	}
	if item["processing_status"] != "READY" {
		t.Errorf("processing_status = %v, want READY", item["processing_status"])
	}
	if item["processing_status_message"] != "Результаты готовы" {
		t.Errorf("processing_status_message = %v", item["processing_status_message"])
	}

	// Second item should map PENDING_PROCESSING → QUEUED.
	item2 := items[1].(map[string]any)
	if item2["processing_status"] != "QUEUED" {
		t.Errorf("second item processing_status = %v, want QUEUED", item2["processing_status"])
	}
}

func TestHandleList_Empty(t *testing.T) {
	dm := &mockDMClient{}
	h := newTestHandler(dm)

	rr := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/contracts/"+testContractID+"/versions", nil)
	r = withAuthContext(r)
	r = withChiParams(r, map[string]string{"contract_id": testContractID})

	h.HandleList().ServeHTTP(rr, r)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	body := parseJSON(t, rr)
	items := body["items"].([]any)
	if len(items) != 0 {
		t.Fatalf("expected 0 items, got %d", len(items))
	}
	if body["total"].(float64) != 0 {
		t.Errorf("total = %v, want 0", body["total"])
	}
}

func TestHandleList_CustomPageSize(t *testing.T) {
	dm := &mockDMClient{}
	h := newTestHandler(dm)

	rr := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/contracts/"+testContractID+"/versions?page=3&size=10", nil)
	r = withAuthContext(r)
	r = withChiParams(r, map[string]string{"contract_id": testContractID})

	h.HandleList().ServeHTTP(rr, r)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	if len(dm.listVerCalls) != 1 {
		t.Fatalf("expected 1 DM call, got %d", len(dm.listVerCalls))
	}
	if dm.listVerCalls[0].Params.Page != 3 || dm.listVerCalls[0].Params.Size != 10 {
		t.Errorf("DM called with page=%d size=%d, want page=3 size=10",
			dm.listVerCalls[0].Params.Page, dm.listVerCalls[0].Params.Size)
	}
}

func TestHandleList_InvalidPage(t *testing.T) {
	h := newTestHandler(&mockDMClient{})

	rr := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/contracts/"+testContractID+"/versions?page=0", nil)
	r = withAuthContext(r)
	r = withChiParams(r, map[string]string{"contract_id": testContractID})

	h.HandleList().ServeHTTP(rr, r)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}

	body := parseJSON(t, rr)
	if body["error_code"] != "VALIDATION_ERROR" {
		t.Errorf("error_code = %v", body["error_code"])
	}
}

func TestHandleList_InvalidSize(t *testing.T) {
	h := newTestHandler(&mockDMClient{})

	rr := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/contracts/"+testContractID+"/versions?size=200", nil)
	r = withAuthContext(r)
	r = withChiParams(r, map[string]string{"contract_id": testContractID})

	h.HandleList().ServeHTTP(rr, r)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestHandleList_InvalidContractID(t *testing.T) {
	h := newTestHandler(&mockDMClient{})

	rr := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/contracts/not-a-uuid/versions", nil)
	r = withAuthContext(r)
	r = withChiParams(r, map[string]string{"contract_id": "not-a-uuid"})

	h.HandleList().ServeHTTP(rr, r)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestHandleList_DM5xx(t *testing.T) {
	dm := &mockDMClient{
		listVerFn: func(_ context.Context, _ string, _ dmclient.ListVersionsParams) (*dmclient.VersionList, error) {
			return nil, dmHTTPError("ListVersions", 500, "")
		},
	}
	h := newTestHandler(dm)

	rr := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/contracts/"+testContractID+"/versions", nil)
	r = withAuthContext(r)
	r = withChiParams(r, map[string]string{"contract_id": testContractID})

	h.HandleList().ServeHTTP(rr, r)

	if rr.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d", rr.Code)
	}
}

func TestHandleList_CircuitOpen(t *testing.T) {
	dm := &mockDMClient{
		listVerFn: func(_ context.Context, _ string, _ dmclient.ListVersionsParams) (*dmclient.VersionList, error) {
			return nil, fmt.Errorf("wrapped: %w", dmclient.ErrCircuitOpen)
		},
	}
	h := newTestHandler(dm)

	rr := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/contracts/"+testContractID+"/versions", nil)
	r = withAuthContext(r)
	r = withChiParams(r, map[string]string{"contract_id": testContractID})

	h.HandleList().ServeHTTP(rr, r)

	if rr.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d", rr.Code)
	}
}

func TestHandleList_NoAuth(t *testing.T) {
	h := newTestHandler(&mockDMClient{})

	rr := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/contracts/"+testContractID+"/versions", nil)
	r = withChiParams(r, map[string]string{"contract_id": testContractID})

	h.HandleList().ServeHTTP(rr, r)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
}

func TestHandleList_DM404(t *testing.T) {
	dm := &mockDMClient{
		listVerFn: func(_ context.Context, _ string, _ dmclient.ListVersionsParams) (*dmclient.VersionList, error) {
			return nil, dmHTTPError("ListVersions", 404, `{"error_code":"NOT_FOUND"}`)
		},
	}
	h := newTestHandler(dm)

	rr := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/contracts/"+testContractID+"/versions", nil)
	r = withAuthContext(r)
	r = withChiParams(r, map[string]string{"contract_id": testContractID})

	h.HandleList().ServeHTTP(rr, r)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestHandleList_SourceFileKeyNotExposed(t *testing.T) {
	dm := &mockDMClient{
		listVerFn: func(_ context.Context, _ string, _ dmclient.ListVersionsParams) (*dmclient.VersionList, error) {
			return &dmclient.VersionList{
				Items: []dmclient.DocumentVersion{stubVersion("FULLY_READY")},
				Total: 1, Page: 1, Size: 20,
			}, nil
		},
	}
	h := newTestHandler(dm)

	rr := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/contracts/"+testContractID+"/versions", nil)
	r = withAuthContext(r)
	r = withChiParams(r, map[string]string{"contract_id": testContractID})

	h.HandleList().ServeHTTP(rr, r)

	respBody := rr.Body.String()
	if strings.Contains(respBody, "source_file_key") {
		t.Error("response should not contain source_file_key")
	}
	if strings.Contains(respBody, "artifact_status") {
		t.Error("response should not contain artifact_status")
	}
}

// =========================================================================
// HandleGet tests
// =========================================================================

func TestHandleGet_Success(t *testing.T) {
	dm := &mockDMClient{
		getVerFn: func(_ context.Context, _, _ string) (*dmclient.DocumentVersionWithArtifacts, error) {
			return stubVersionWithArtifacts("FULLY_READY"), nil
		},
	}
	h := newTestHandler(dm)

	rr := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/contracts/"+testContractID+"/versions/"+testVersionID, nil)
	r = withAuthContext(r)
	r = withChiParams(r, map[string]string{"contract_id": testContractID, "version_id": testVersionID})

	h.HandleGet().ServeHTTP(rr, r)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	body := parseJSON(t, rr)
	if body["version_id"] != testVersionID {
		t.Errorf("version_id = %v, want %s", body["version_id"], testVersionID)
	}
	if body["contract_id"] != testContractID {
		t.Errorf("contract_id = %v, want %s", body["contract_id"], testContractID)
	}
	if body["processing_status"] != "READY" {
		t.Errorf("processing_status = %v, want READY", body["processing_status"])
	}
	if body["processing_status_message"] != "Результаты готовы" {
		t.Errorf("processing_status_message = %v", body["processing_status_message"])
	}
	if body["source_file_name"] != "contract.pdf" {
		t.Errorf("source_file_name = %v", body["source_file_name"])
	}
}

func TestHandleGet_InvalidContractID(t *testing.T) {
	h := newTestHandler(&mockDMClient{})

	rr := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/contracts/bad/versions/"+testVersionID, nil)
	r = withAuthContext(r)
	r = withChiParams(r, map[string]string{"contract_id": "bad", "version_id": testVersionID})

	h.HandleGet().ServeHTTP(rr, r)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestHandleGet_InvalidVersionID(t *testing.T) {
	h := newTestHandler(&mockDMClient{})

	rr := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/contracts/"+testContractID+"/versions/bad", nil)
	r = withAuthContext(r)
	r = withChiParams(r, map[string]string{"contract_id": testContractID, "version_id": "bad"})

	h.HandleGet().ServeHTTP(rr, r)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestHandleGet_DM404(t *testing.T) {
	dm := &mockDMClient{
		getVerFn: func(_ context.Context, _, _ string) (*dmclient.DocumentVersionWithArtifacts, error) {
			return nil, dmHTTPError("GetVersion", 404, `{"error_code":"NOT_FOUND"}`)
		},
	}
	h := newTestHandler(dm)

	rr := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r = withAuthContext(r)
	r = withChiParams(r, map[string]string{"contract_id": testContractID, "version_id": testVersionID})

	h.HandleGet().ServeHTTP(rr, r)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestHandleGet_DM5xx(t *testing.T) {
	dm := &mockDMClient{
		getVerFn: func(_ context.Context, _, _ string) (*dmclient.DocumentVersionWithArtifacts, error) {
			return nil, dmHTTPError("GetVersion", 503, "")
		},
	}
	h := newTestHandler(dm)

	rr := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r = withAuthContext(r)
	r = withChiParams(r, map[string]string{"contract_id": testContractID, "version_id": testVersionID})

	h.HandleGet().ServeHTTP(rr, r)

	if rr.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d", rr.Code)
	}
}

func TestHandleGet_NoAuth(t *testing.T) {
	h := newTestHandler(&mockDMClient{})

	rr := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r = withChiParams(r, map[string]string{"contract_id": testContractID, "version_id": testVersionID})

	h.HandleGet().ServeHTTP(rr, r)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
}

func TestHandleGet_SourceFileKeyNotExposed(t *testing.T) {
	h := newTestHandler(&mockDMClient{})

	rr := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r = withAuthContext(r)
	r = withChiParams(r, map[string]string{"contract_id": testContractID, "version_id": testVersionID})

	h.HandleGet().ServeHTTP(rr, r)

	if strings.Contains(rr.Body.String(), "source_file_key") {
		t.Error("response should not contain source_file_key")
	}
}

// =========================================================================
// HandleStatus tests
// =========================================================================

func TestHandleStatus_Success(t *testing.T) {
	dm := &mockDMClient{
		getVerFn: func(_ context.Context, _, _ string) (*dmclient.DocumentVersionWithArtifacts, error) {
			return stubVersionWithArtifacts("PROCESSING_IN_PROGRESS"), nil
		},
	}
	h := newTestHandler(dm)

	before := time.Now().UTC().Truncate(time.Second)
	rr := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r = withAuthContext(r)
	r = withChiParams(r, map[string]string{"contract_id": testContractID, "version_id": testVersionID})

	h.HandleStatus().ServeHTTP(rr, r)
	after := time.Now().UTC().Add(time.Second).Truncate(time.Second)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	body := parseJSON(t, rr)
	if body["version_id"] != testVersionID {
		t.Errorf("version_id = %v", body["version_id"])
	}
	if body["status"] != "PROCESSING" {
		t.Errorf("status = %v, want PROCESSING", body["status"])
	}
	if body["message"] != "Извлечение текста и структуры" {
		t.Errorf("message = %v", body["message"])
	}

	// updated_at should be approximately now (server time).
	updatedAt, err := time.Parse(time.RFC3339, body["updated_at"].(string))
	if err != nil {
		t.Fatalf("failed to parse updated_at: %v", err)
	}
	if updatedAt.Before(before) || updatedAt.After(after.Add(time.Second)) {
		t.Errorf("updated_at %v not between %v and %v", updatedAt, before, after)
	}
}

func TestHandleStatus_InvalidVersionID(t *testing.T) {
	h := newTestHandler(&mockDMClient{})

	rr := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r = withAuthContext(r)
	r = withChiParams(r, map[string]string{"contract_id": testContractID, "version_id": "bad"})

	h.HandleStatus().ServeHTTP(rr, r)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestHandleStatus_DM404(t *testing.T) {
	dm := &mockDMClient{
		getVerFn: func(_ context.Context, _, _ string) (*dmclient.DocumentVersionWithArtifacts, error) {
			return nil, dmHTTPError("GetVersion", 404, `{"error_code":"NOT_FOUND"}`)
		},
	}
	h := newTestHandler(dm)

	rr := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r = withAuthContext(r)
	r = withChiParams(r, map[string]string{"contract_id": testContractID, "version_id": testVersionID})

	h.HandleStatus().ServeHTTP(rr, r)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestHandleStatus_NoAuth(t *testing.T) {
	h := newTestHandler(&mockDMClient{})

	rr := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r = withChiParams(r, map[string]string{"contract_id": testContractID, "version_id": testVersionID})

	h.HandleStatus().ServeHTTP(rr, r)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
}

// =========================================================================
// Processing status mapping tests
// =========================================================================

func TestProcessingStatusMapping_AllValues(t *testing.T) {
	tests := []struct {
		artifactStatus string
		wantStatus     string
		wantMessage    string
	}{
		{"PENDING_UPLOAD", "UPLOADED", "Договор загружен"},
		{"PENDING_PROCESSING", "QUEUED", "В очереди на обработку"},
		{"PROCESSING_IN_PROGRESS", "PROCESSING", "Извлечение текста и структуры"},
		{"ARTIFACTS_READY", "ANALYZING", "Юридический анализ"},
		{"ANALYSIS_IN_PROGRESS", "ANALYZING", "Юридический анализ"},
		{"ANALYSIS_READY", "GENERATING_REPORTS", "Формирование отчётов"},
		{"REPORTS_IN_PROGRESS", "GENERATING_REPORTS", "Формирование отчётов"},
		{"FULLY_READY", "READY", "Результаты готовы"},
		{"PARTIALLY_AVAILABLE", "PARTIALLY_FAILED", "Частично доступно (есть ошибки)"},
		{"PROCESSING_FAILED", "FAILED", "Ошибка обработки"},
		{"REJECTED", "REJECTED", "Файл отклонён (формат/размер)"},
	}

	for _, tt := range tests {
		t.Run(tt.artifactStatus, func(t *testing.T) {
			status := mapProcessingStatus(tt.artifactStatus)
			if status != tt.wantStatus {
				t.Errorf("mapProcessingStatus(%s) = %s, want %s", tt.artifactStatus, status, tt.wantStatus)
			}
			msg := mapProcessingStatusMessage(status)
			if msg != tt.wantMessage {
				t.Errorf("message = %s, want %s", msg, tt.wantMessage)
			}
		})
	}
}

func TestProcessingStatusMapping_Unknown(t *testing.T) {
	status := mapProcessingStatus("SOME_FUTURE_STATUS")
	if status != "UNKNOWN" {
		t.Errorf("expected UNKNOWN for unrecognized status, got %s", status)
	}
	msg := mapProcessingStatusMessage(status)
	if msg != "Статус неизвестен" {
		t.Errorf("expected unknown message, got %s", msg)
	}
}

// =========================================================================
// HandleUpload tests
// =========================================================================

func TestHandleUpload_Success(t *testing.T) {
	dm := &mockDMClient{}
	storage := &mockStorage{}
	pub := &mockPublisher{}
	kv := &mockKVStore{}
	h := newTestHandlerFull(dm, storage, pub, kv)

	rr := httptest.NewRecorder()
	r := createMultipartRequest(t, "newversion.pdf", validPDFContent())
	r = withAuthContext(r)
	r = withChiParams(r, map[string]string{"contract_id": testContractID})

	h.HandleUpload().ServeHTTP(rr, r)

	if rr.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp VersionUploadResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if resp.ContractID != testContractID {
		t.Errorf("contract_id = %s, want %s", resp.ContractID, testContractID)
	}
	if resp.VersionID != "ver-002" {
		t.Errorf("version_id = %s, want ver-002", resp.VersionID)
	}
	if resp.VersionNumber != 2 {
		t.Errorf("version_number = %d, want 2", resp.VersionNumber)
	}
	if resp.JobID != "uuid-002" { // second UUID generated (first is correlation_id)
		t.Errorf("job_id = %s, want uuid-002", resp.JobID)
	}
	if resp.Status != "UPLOADED" {
		t.Errorf("status = %s, want UPLOADED", resp.Status)
	}

	// Verify X-Correlation-Id header.
	if rr.Header().Get("X-Correlation-Id") != "uuid-001" {
		t.Errorf("X-Correlation-Id = %s, want uuid-001", rr.Header().Get("X-Correlation-Id"))
	}

	// Verify DM CreateVersion was called with correct params.
	if len(dm.createCalls) != 1 {
		t.Fatalf("expected 1 CreateVersion call, got %d", len(dm.createCalls))
	}
	if dm.createCalls[0].Req.OriginType != "RE_UPLOAD" {
		t.Errorf("origin_type = %s, want RE_UPLOAD", dm.createCalls[0].Req.OriginType)
	}
	if dm.createCalls[0].Req.ParentVersionID != testVersionID {
		t.Errorf("parent_version_id = %s, want %s", dm.createCalls[0].Req.ParentVersionID, testVersionID)
	}

	// Verify S3 upload.
	if len(storage.putCalls) != 1 {
		t.Fatalf("expected 1 S3 PutObject call, got %d", len(storage.putCalls))
	}

	// Verify command published.
	if len(pub.calls) != 1 {
		t.Fatalf("expected 1 PublishProcessDocument call, got %d", len(pub.calls))
	}
	if pub.calls[0].SourceFileMIMEType != "application/pdf" {
		t.Errorf("mime_type = %s", pub.calls[0].SourceFileMIMEType)
	}
	if pub.calls[0].DocumentID != testContractID {
		t.Errorf("document_id = %s, want %s", pub.calls[0].DocumentID, testContractID)
	}

	// Verify Redis tracking.
	if len(kv.calls) != 1 {
		t.Fatalf("expected 1 Redis Set call, got %d", len(kv.calls))
	}
	if !strings.Contains(kv.calls[0].Key, "upload:org-001:uuid-002") {
		t.Errorf("redis key = %s", kv.calls[0].Key)
	}
}

func TestHandleUpload_NoAuth(t *testing.T) {
	h := newTestHandlerFull(&mockDMClient{}, &mockStorage{}, &mockPublisher{}, &mockKVStore{})

	rr := httptest.NewRecorder()
	r := createMultipartRequest(t, "test.pdf", validPDFContent())
	r = withChiParams(r, map[string]string{"contract_id": testContractID})

	h.HandleUpload().ServeHTTP(rr, r)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
}

func TestHandleUpload_InvalidContractID(t *testing.T) {
	h := newTestHandlerFull(&mockDMClient{}, &mockStorage{}, &mockPublisher{}, &mockKVStore{})

	rr := httptest.NewRecorder()
	r := createMultipartRequest(t, "test.pdf", validPDFContent())
	r = withAuthContext(r)
	r = withChiParams(r, map[string]string{"contract_id": "not-uuid"})

	h.HandleUpload().ServeHTTP(rr, r)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestHandleUpload_MissingFile(t *testing.T) {
	h := newTestHandlerFull(&mockDMClient{}, &mockStorage{}, &mockPublisher{}, &mockKVStore{})

	// Multipart request with no file.
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	_ = w.Close()

	rr := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/", &body)
	r.Header.Set("Content-Type", w.FormDataContentType())
	r = withAuthContext(r)
	r = withChiParams(r, map[string]string{"contract_id": testContractID})

	h.HandleUpload().ServeHTTP(rr, r)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestHandleUpload_FileTooLarge(t *testing.T) {
	dm := &mockDMClient{}
	storage := &mockStorage{}
	pub := &mockPublisher{}
	kv := &mockKVStore{}
	h := NewHandler(dm, storage, pub, kv, logger.NewLogger("error"), 50) // 50 byte limit
	h.uuidGen = defaultUUIDGenerator

	rr := httptest.NewRecorder()
	r := createMultipartRequest(t, "test.pdf", validPDFContent()) // 100 bytes
	r = withAuthContext(r)
	r = withChiParams(r, map[string]string{"contract_id": testContractID})

	h.HandleUpload().ServeHTTP(rr, r)

	if rr.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestHandleUpload_UnsupportedMIME(t *testing.T) {
	h := newTestHandlerFull(&mockDMClient{}, &mockStorage{}, &mockPublisher{}, &mockKVStore{})

	// Create multipart with wrong Content-Type.
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	partHeader := make(map[string][]string)
	partHeader["Content-Disposition"] = []string{`form-data; name="file"; filename="test.docx"`}
	partHeader["Content-Type"] = []string{"application/vnd.openxmlformats-officedocument.wordprocessingml.document"}
	part, _ := w.CreatePart(partHeader)
	_, _ = part.Write(validPDFContent())
	_ = w.Close()

	rr := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/", &body)
	r.Header.Set("Content-Type", w.FormDataContentType())
	r = withAuthContext(r)
	r = withChiParams(r, map[string]string{"contract_id": testContractID})

	h.HandleUpload().ServeHTTP(rr, r)

	if rr.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("expected 415, got %d", rr.Code)
	}
}

func TestHandleUpload_InvalidMagicBytes(t *testing.T) {
	h := newTestHandlerFull(&mockDMClient{}, &mockStorage{}, &mockPublisher{}, &mockKVStore{})

	rr := httptest.NewRecorder()
	content := []byte("NOT-A-PDF-FILE-at-all-just-random-content")
	r := createMultipartRequest(t, "fake.pdf", content)
	r = withAuthContext(r)
	r = withChiParams(r, map[string]string{"contract_id": testContractID})

	h.HandleUpload().ServeHTTP(rr, r)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestHandleUpload_EmptyFile(t *testing.T) {
	h := newTestHandlerFull(&mockDMClient{}, &mockStorage{}, &mockPublisher{}, &mockKVStore{})

	rr := httptest.NewRecorder()
	r := createMultipartRequest(t, "empty.pdf", []byte{})
	r = withAuthContext(r)
	r = withChiParams(r, map[string]string{"contract_id": testContractID})

	h.HandleUpload().ServeHTTP(rr, r)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestHandleUpload_DocumentNotFound(t *testing.T) {
	dm := &mockDMClient{
		getDocFn: func(_ context.Context, _ string) (*dmclient.DocumentWithCurrentVersion, error) {
			return nil, dmHTTPError("GetDocument", 404, `{"error_code":"NOT_FOUND"}`)
		},
	}
	storage := &mockStorage{}
	h := newTestHandlerFull(dm, storage, &mockPublisher{}, &mockKVStore{})

	rr := httptest.NewRecorder()
	r := createMultipartRequest(t, "test.pdf", validPDFContent())
	r = withAuthContext(r)
	r = withChiParams(r, map[string]string{"contract_id": testContractID})

	h.HandleUpload().ServeHTTP(rr, r)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}

	// S3 should NOT have been called.
	if len(storage.putCalls) != 0 {
		t.Error("S3 PutObject should not be called when document not found")
	}
}

func TestHandleUpload_DocumentArchived(t *testing.T) {
	dm := &mockDMClient{
		getDocFn: func(_ context.Context, _ string) (*dmclient.DocumentWithCurrentVersion, error) {
			doc := stubDocWithVersion("FULLY_READY")
			doc.Status = "ARCHIVED"
			return doc, nil
		},
	}
	storage := &mockStorage{}
	h := newTestHandlerFull(dm, storage, &mockPublisher{}, &mockKVStore{})

	rr := httptest.NewRecorder()
	r := createMultipartRequest(t, "test.pdf", validPDFContent())
	r = withAuthContext(r)
	r = withChiParams(r, map[string]string{"contract_id": testContractID})

	h.HandleUpload().ServeHTTP(rr, r)

	if rr.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", rr.Code, rr.Body.String())
	}

	// No S3 upload.
	if len(storage.putCalls) != 0 {
		t.Error("S3 PutObject should not be called for archived document")
	}
}

func TestHandleUpload_DocumentDeleted(t *testing.T) {
	dm := &mockDMClient{
		getDocFn: func(_ context.Context, _ string) (*dmclient.DocumentWithCurrentVersion, error) {
			doc := stubDocWithVersion("FULLY_READY")
			doc.Status = "DELETED"
			return doc, nil
		},
	}
	h := newTestHandlerFull(dm, &mockStorage{}, &mockPublisher{}, &mockKVStore{})

	rr := httptest.NewRecorder()
	r := createMultipartRequest(t, "test.pdf", validPDFContent())
	r = withAuthContext(r)
	r = withChiParams(r, map[string]string{"contract_id": testContractID})

	h.HandleUpload().ServeHTTP(rr, r)

	if rr.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d", rr.Code)
	}
}

func TestHandleUpload_VersionStillProcessing(t *testing.T) {
	statuses := []string{"PENDING_UPLOAD", "PENDING_PROCESSING", "PROCESSING_IN_PROGRESS"}
	for _, as := range statuses {
		t.Run(as, func(t *testing.T) {
			dm := &mockDMClient{
				getDocFn: func(_ context.Context, _ string) (*dmclient.DocumentWithCurrentVersion, error) {
					return stubDocWithVersion(as), nil
				},
			}
			storage := &mockStorage{}
			h := newTestHandlerFull(dm, storage, &mockPublisher{}, &mockKVStore{})

			rr := httptest.NewRecorder()
			r := createMultipartRequest(t, "test.pdf", validPDFContent())
			r = withAuthContext(r)
			r = withChiParams(r, map[string]string{"contract_id": testContractID})

			h.HandleUpload().ServeHTTP(rr, r)

			if rr.Code != http.StatusConflict {
				t.Fatalf("expected 409 for %s, got %d: %s", as, rr.Code, rr.Body.String())
			}

			// No S3 upload should occur.
			if len(storage.putCalls) != 0 {
				t.Error("S3 PutObject should not be called when version still processing")
			}
		})
	}
}

func TestHandleUpload_S3Failure(t *testing.T) {
	dm := &mockDMClient{}
	storage := &mockStorage{
		putFn: func(_ context.Context, _ string, _ io.ReadSeeker, _ string) error {
			return errors.New("S3 connection refused")
		},
	}
	h := newTestHandlerFull(dm, storage, &mockPublisher{}, &mockKVStore{})

	rr := httptest.NewRecorder()
	r := createMultipartRequest(t, "test.pdf", validPDFContent())
	r = withAuthContext(r)
	r = withChiParams(r, map[string]string{"contract_id": testContractID})

	h.HandleUpload().ServeHTTP(rr, r)

	if rr.Code != http.StatusBadGateway {
		t.Fatalf("expected 502 (STORAGE_UNAVAILABLE), got %d: %s", rr.Code, rr.Body.String())
	}

	// No DM CreateVersion call.
	if len(dm.createCalls) != 0 {
		t.Error("DM CreateVersion should not be called after S3 failure")
	}
}

func TestHandleUpload_DMCreateVersionFailure_S3Cleanup(t *testing.T) {
	dm := &mockDMClient{
		createVerFn: func(_ context.Context, _ string, _ dmclient.CreateVersionRequest) (*dmclient.DocumentVersion, error) {
			return nil, dmHTTPError("CreateVersion", 500, "")
		},
	}
	storage := &mockStorage{}
	h := newTestHandlerFull(dm, storage, &mockPublisher{}, &mockKVStore{})

	rr := httptest.NewRecorder()
	r := createMultipartRequest(t, "test.pdf", validPDFContent())
	r = withAuthContext(r)
	r = withChiParams(r, map[string]string{"contract_id": testContractID})

	h.HandleUpload().ServeHTTP(rr, r)

	if rr.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d", rr.Code)
	}

	// S3 cleanup should have been called.
	if len(storage.deleteCalls) != 1 {
		t.Fatalf("expected 1 S3 DeleteObject call for cleanup, got %d", len(storage.deleteCalls))
	}
}

func TestHandleUpload_BrokerFailure_NoS3Cleanup(t *testing.T) {
	dm := &mockDMClient{}
	storage := &mockStorage{}
	pub := &mockPublisher{
		publishFn: func(_ context.Context, _ commandpub.ProcessDocumentCommand) error {
			return errors.New("broker down")
		},
	}
	h := newTestHandlerFull(dm, storage, pub, &mockKVStore{})

	rr := httptest.NewRecorder()
	r := createMultipartRequest(t, "test.pdf", validPDFContent())
	r = withAuthContext(r)
	r = withChiParams(r, map[string]string{"contract_id": testContractID})

	h.HandleUpload().ServeHTTP(rr, r)

	if rr.Code != http.StatusBadGateway {
		t.Fatalf("expected 502 (BROKER_UNAVAILABLE), got %d: %s", rr.Code, rr.Body.String())
	}

	// S3 cleanup should NOT happen — version exists in DM.
	if len(storage.deleteCalls) != 0 {
		t.Error("S3 should not be cleaned up after broker failure (version exists in DM)")
	}
}

func TestHandleUpload_RedisFailure_NonCritical(t *testing.T) {
	dm := &mockDMClient{}
	storage := &mockStorage{}
	pub := &mockPublisher{}
	kv := &mockKVStore{
		setFn: func(_ context.Context, _ string, _ string, _ time.Duration) error {
			return errors.New("Redis down")
		},
	}
	h := newTestHandlerFull(dm, storage, pub, kv)

	rr := httptest.NewRecorder()
	r := createMultipartRequest(t, "test.pdf", validPDFContent())
	r = withAuthContext(r)
	r = withChiParams(r, map[string]string{"contract_id": testContractID})

	h.HandleUpload().ServeHTTP(rr, r)

	// Should still succeed — Redis tracking is non-critical.
	if rr.Code != http.StatusAccepted {
		t.Fatalf("expected 202 even with Redis failure, got %d", rr.Code)
	}
}

func TestHandleUpload_DocumentWithNoVersions(t *testing.T) {
	dm := &mockDMClient{
		getDocFn: func(_ context.Context, _ string) (*dmclient.DocumentWithCurrentVersion, error) {
			return &dmclient.DocumentWithCurrentVersion{
				Document: dmclient.Document{
					DocumentID:       testContractID,
					OrganizationID:   "org-001",
					Title:            "Orphan doc",
					CurrentVersionID: nil,
					Status:           "ACTIVE",
					CreatedByUserID:  "user-001",
					CreatedAt:        time.Now(),
					UpdatedAt:        time.Now(),
				},
				CurrentVersion: nil, // no versions
			}, nil
		},
	}
	storage := &mockStorage{}
	h := newTestHandlerFull(dm, storage, &mockPublisher{}, &mockKVStore{})

	rr := httptest.NewRecorder()
	r := createMultipartRequest(t, "test.pdf", validPDFContent())
	r = withAuthContext(r)
	r = withChiParams(r, map[string]string{"contract_id": testContractID})

	h.HandleUpload().ServeHTTP(rr, r)

	if rr.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", rr.Code, rr.Body.String())
	}

	// ParentVersionID should be empty (no parent).
	if len(dm.createCalls) != 1 {
		t.Fatalf("expected 1 CreateVersion call, got %d", len(dm.createCalls))
	}
	if dm.createCalls[0].Req.ParentVersionID != "" {
		t.Errorf("parent_version_id = %s, want empty", dm.createCalls[0].Req.ParentVersionID)
	}
}

func TestHandleUpload_PublishedCommandFields(t *testing.T) {
	dm := &mockDMClient{}
	storage := &mockStorage{}
	pub := &mockPublisher{}
	h := newTestHandlerFull(dm, storage, pub, &mockKVStore{})

	rr := httptest.NewRecorder()
	r := createMultipartRequest(t, "test.pdf", validPDFContent())
	r = withAuthContext(r)
	r = withChiParams(r, map[string]string{"contract_id": testContractID})

	h.HandleUpload().ServeHTTP(rr, r)

	if rr.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d", rr.Code)
	}

	if len(pub.calls) != 1 {
		t.Fatalf("expected 1 publish call, got %d", len(pub.calls))
	}

	cmd := pub.calls[0]
	if cmd.JobID != "uuid-002" {
		t.Errorf("job_id = %s, want uuid-002", cmd.JobID)
	}
	if cmd.DocumentID != testContractID {
		t.Errorf("document_id = %s", cmd.DocumentID)
	}
	if cmd.VersionID != "ver-002" {
		t.Errorf("version_id = %s", cmd.VersionID)
	}
	if cmd.OrganizationID != "org-001" {
		t.Errorf("organization_id = %s", cmd.OrganizationID)
	}
	if cmd.RequestedByUserID != "user-001" {
		t.Errorf("requested_by_user_id = %s", cmd.RequestedByUserID)
	}
	if cmd.SourceFileName != "test.pdf" {
		t.Errorf("source_file_name = %s", cmd.SourceFileName)
	}
	if cmd.SourceFileSize != 100 { // validPDFContent() is 100 bytes
		t.Errorf("source_file_size = %d, want 100", cmd.SourceFileSize)
	}
	if cmd.SourceFileMIMEType != "application/pdf" {
		t.Errorf("source_file_mime_type = %s", cmd.SourceFileMIMEType)
	}
	if cmd.SourceFileChecksum == "" {
		t.Error("source_file_checksum should not be empty")
	}
}

func TestHandleUpload_S3KeyFormat(t *testing.T) {
	storage := &mockStorage{}
	h := newTestHandlerFull(&mockDMClient{}, storage, &mockPublisher{}, &mockKVStore{})

	rr := httptest.NewRecorder()
	r := createMultipartRequest(t, "test.pdf", validPDFContent())
	r = withAuthContext(r)
	r = withChiParams(r, map[string]string{"contract_id": testContractID})

	h.HandleUpload().ServeHTTP(rr, r)

	if rr.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d", rr.Code)
	}

	if len(storage.putCalls) != 1 {
		t.Fatalf("expected 1 PutObject call")
	}

	// Key format: uploads/{org_id}/{job_id}/{uuid}
	key := storage.putCalls[0].Key
	if !strings.HasPrefix(key, "uploads/org-001/uuid-002/") {
		t.Errorf("S3 key = %s, want prefix uploads/org-001/uuid-002/", key)
	}
}

// =========================================================================
// sanitizeFilename tests
// =========================================================================

func TestSanitizeFilename(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"normal", "contract.pdf", "contract.pdf"},
		{"path_traversal_fwd", "../../../etc/passwd", "passwd"},
		{"path_traversal_bwd", "..\\..\\windows\\system32", "system32"},
		{"null_chars", "file\x00name.pdf", "filename.pdf"},
		{"control_chars", "file\x01\x02name.pdf", "filename.pdf"},
		{"del_char", "file\x7Fname.pdf", "filename.pdf"},
		{"empty", "", "upload.pdf"},
		{"dot", ".", "upload.pdf"},
		{"dotdot", "..", "upload.pdf"},
		{"embedded_traversal", "foo/../bar.pdf", "bar.pdf"},
		{"spaces", "  spaces  .pdf  ", "spaces  .pdf"},
		{"unicode", "договор.pdf", "договор.pdf"},
		{"absolute_path", "/etc/file.pdf", "file.pdf"},
		{"windows_path", "C:\\Users\\file.pdf", "file.pdf"},
		{"long_truncation", strings.Repeat("a", 300), strings.Repeat("a", 255)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeFilename(tt.input)
			if got != tt.want {
				t.Errorf("sanitizeFilename(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// =========================================================================
// checksumReadSeeker test
// =========================================================================

func TestChecksumReadSeeker(t *testing.T) {
	data := []byte("hello world")
	rs := bytes.NewReader(data)
	hasher := &mockHasher{}
	crs := &checksumReadSeeker{rs: rs, hasher: hasher}

	buf := make([]byte, 5)
	n, _ := crs.Read(buf)
	if n != 5 || string(buf[:n]) != "hello" {
		t.Errorf("first read: %q", buf[:n])
	}

	// Seek back and verify hash reset.
	_, _ = crs.Seek(0, io.SeekStart)
	if !hasher.resetCalled {
		t.Error("hasher should be reset on Seek(0)")
	}
}

type mockHasher struct {
	buf          bytes.Buffer
	resetCalled  bool
	writeCalls   int
}

func (h *mockHasher) Write(p []byte) (int, error) {
	h.writeCalls++
	return h.buf.Write(p)
}

func (h *mockHasher) Sum(b []byte) []byte {
	return append(b, h.buf.Bytes()...)
}

func (h *mockHasher) Reset() {
	h.resetCalled = true
	h.buf.Reset()
}

// =========================================================================
// Response format tests
// =========================================================================

func TestResponseFormat_ContentType(t *testing.T) {
	h := newTestHandler(&mockDMClient{})

	rr := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r = withAuthContext(r)
	r = withChiParams(r, map[string]string{"contract_id": testContractID})

	h.HandleList().ServeHTTP(rr, r)

	ct := rr.Header().Get("Content-Type")
	if !strings.HasPrefix(ct, "application/json") {
		t.Errorf("Content-Type = %s, want application/json", ct)
	}
}

func TestResponseFormat_RFC3339Time(t *testing.T) {
	dm := &mockDMClient{
		getVerFn: func(_ context.Context, _, _ string) (*dmclient.DocumentVersionWithArtifacts, error) {
			return stubVersionWithArtifacts("FULLY_READY"), nil
		},
	}
	h := newTestHandler(dm)

	rr := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r = withAuthContext(r)
	r = withChiParams(r, map[string]string{"contract_id": testContractID, "version_id": testVersionID})

	h.HandleGet().ServeHTTP(rr, r)

	body := parseJSON(t, rr)
	createdAt := body["created_at"].(string)
	if _, err := time.Parse(time.RFC3339, createdAt); err != nil {
		t.Errorf("created_at is not valid RFC3339: %s", createdAt)
	}
}

// =========================================================================
// handleDMError tests
// =========================================================================

func TestHandleDMError_TransportError(t *testing.T) {
	h := newTestHandler(&mockDMClient{})

	rr := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", nil)

	transportErr := &dmclient.DMError{
		Operation: "GetVersion",
		Retryable: true,
	}
	h.handleDMError(r.Context(), rr, r, transportErr, "GetVersion", "version")

	if rr.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d", rr.Code)
	}
}

func TestHandleDMError_UnknownError(t *testing.T) {
	h := newTestHandler(&mockDMClient{})

	rr := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", nil)

	h.handleDMError(r.Context(), rr, r, errors.New("something unexpected"), "GetVersion", "version")

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rr.Code)
	}
}

// =========================================================================
// HandleRecheck tests
// =========================================================================

func TestHandleRecheck_Success(t *testing.T) {
	dm := &mockDMClient{
		getVerFn: func(_ context.Context, _, _ string) (*dmclient.DocumentVersionWithArtifacts, error) {
			return stubVersionWithArtifacts("FULLY_READY"), nil
		},
		createVerFn: func(_ context.Context, _ string, _ dmclient.CreateVersionRequest) (*dmclient.DocumentVersion, error) {
			return &dmclient.DocumentVersion{
				VersionID:     "ver-new-001",
				VersionNumber: 2,
			}, nil
		},
	}
	pub := &mockPublisher{}
	kv := &mockKVStore{}
	h := newTestHandlerFull(dm, &mockStorage{}, pub, kv)

	rr := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/v1/contracts/"+testContractID+"/versions/"+testVersionID+"/recheck", nil)
	r = withAuthContext(r)
	r = withChiParams(r, map[string]string{"contract_id": testContractID, "version_id": testVersionID})

	h.HandleRecheck().ServeHTTP(rr, r)

	if rr.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", rr.Code, rr.Body.String())
	}

	result := parseJSON(t, rr)
	if result["contract_id"] != testContractID {
		t.Errorf("contract_id = %v, want %s", result["contract_id"], testContractID)
	}
	if result["version_id"] != "ver-new-001" {
		t.Errorf("version_id = %v, want ver-new-001", result["version_id"])
	}
	if result["status"] != "UPLOADED" {
		t.Errorf("status = %v, want UPLOADED", result["status"])
	}
	if result["job_id"] == nil || result["job_id"] == "" {
		t.Error("job_id should not be empty")
	}
	if result["message"] == nil || result["message"] == "" {
		t.Error("message should not be empty")
	}

	// Verify X-Correlation-Id header is set.
	if rr.Header().Get("X-Correlation-Id") == "" {
		t.Error("X-Correlation-Id header should be set")
	}
}

func TestHandleRecheck_NoAuth(t *testing.T) {
	h := newTestHandler(&mockDMClient{})

	rr := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/v1/contracts/"+testContractID+"/versions/"+testVersionID+"/recheck", nil)
	r = withChiParams(r, map[string]string{"contract_id": testContractID, "version_id": testVersionID})

	h.HandleRecheck().ServeHTTP(rr, r)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
}

func TestHandleRecheck_InvalidContractID(t *testing.T) {
	h := newTestHandler(&mockDMClient{})

	rr := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/v1/contracts/not-uuid/versions/"+testVersionID+"/recheck", nil)
	r = withAuthContext(r)
	r = withChiParams(r, map[string]string{"contract_id": "not-uuid", "version_id": testVersionID})

	h.HandleRecheck().ServeHTTP(rr, r)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestHandleRecheck_InvalidVersionID(t *testing.T) {
	h := newTestHandler(&mockDMClient{})

	rr := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/v1/contracts/"+testContractID+"/versions/not-uuid/recheck", nil)
	r = withAuthContext(r)
	r = withChiParams(r, map[string]string{"contract_id": testContractID, "version_id": "not-uuid"})

	h.HandleRecheck().ServeHTTP(rr, r)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestHandleRecheck_VersionNotFound(t *testing.T) {
	dm := &mockDMClient{
		getVerFn: func(_ context.Context, _, _ string) (*dmclient.DocumentVersionWithArtifacts, error) {
			return nil, dmHTTPError("GetVersion", 404, `{"error":"not found"}`)
		},
	}
	h := newTestHandler(dm)

	rr := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/v1/contracts/"+testContractID+"/versions/"+testVersionID+"/recheck", nil)
	r = withAuthContext(r)
	r = withChiParams(r, map[string]string{"contract_id": testContractID, "version_id": testVersionID})

	h.HandleRecheck().ServeHTTP(rr, r)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestHandleRecheck_VersionStillProcessing(t *testing.T) {
	processingStatuses := []string{
		"PENDING_UPLOAD",
		"PENDING_PROCESSING",
		"PROCESSING_IN_PROGRESS",
		"ARTIFACTS_READY",
		"ANALYSIS_IN_PROGRESS",
		"ANALYSIS_READY",
		"REPORTS_IN_PROGRESS",
	}

	for _, status := range processingStatuses {
		t.Run(status, func(t *testing.T) {
			dm := &mockDMClient{
				getVerFn: func(_ context.Context, _, _ string) (*dmclient.DocumentVersionWithArtifacts, error) {
					return stubVersionWithArtifacts(status), nil
				},
			}
			h := newTestHandler(dm)

			rr := httptest.NewRecorder()
			r := httptest.NewRequest(http.MethodPost, "/api/v1/contracts/"+testContractID+"/versions/"+testVersionID+"/recheck", nil)
			r = withAuthContext(r)
			r = withChiParams(r, map[string]string{"contract_id": testContractID, "version_id": testVersionID})

			h.HandleRecheck().ServeHTTP(rr, r)

			if rr.Code != http.StatusConflict {
				t.Fatalf("expected 409 for status %s, got %d: %s", status, rr.Code, rr.Body.String())
			}

			result := parseJSON(t, rr)
			if result["error_code"] != "VERSION_STILL_PROCESSING" {
				t.Errorf("error_code = %v, want VERSION_STILL_PROCESSING", result["error_code"])
			}
		})
	}
}

func TestHandleRecheck_TerminalStatuses_Allowed(t *testing.T) {
	terminalStatuses := []string{
		"FULLY_READY",
		"PARTIALLY_AVAILABLE",
		"PROCESSING_FAILED",
		"REJECTED",
	}

	for _, status := range terminalStatuses {
		t.Run(status, func(t *testing.T) {
			dm := &mockDMClient{
				getVerFn: func(_ context.Context, _, _ string) (*dmclient.DocumentVersionWithArtifacts, error) {
					return stubVersionWithArtifacts(status), nil
				},
			}
			h := newTestHandlerFull(dm, &mockStorage{}, &mockPublisher{}, &mockKVStore{})

			rr := httptest.NewRecorder()
			r := httptest.NewRequest(http.MethodPost, "/api/v1/contracts/"+testContractID+"/versions/"+testVersionID+"/recheck", nil)
			r = withAuthContext(r)
			r = withChiParams(r, map[string]string{"contract_id": testContractID, "version_id": testVersionID})

			h.HandleRecheck().ServeHTTP(rr, r)

			if rr.Code != http.StatusAccepted {
				t.Fatalf("expected 202 for terminal status %s, got %d: %s", status, rr.Code, rr.Body.String())
			}
		})
	}
}

func TestHandleRecheck_CreateVersionRequest(t *testing.T) {
	dm := &mockDMClient{
		getVerFn: func(_ context.Context, _, _ string) (*dmclient.DocumentVersionWithArtifacts, error) {
			return stubVersionWithArtifacts("FULLY_READY"), nil
		},
	}
	pub := &mockPublisher{}
	h := newTestHandlerFull(dm, &mockStorage{}, pub, &mockKVStore{})

	rr := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/v1/contracts/"+testContractID+"/versions/"+testVersionID+"/recheck", nil)
	r = withAuthContext(r)
	r = withChiParams(r, map[string]string{"contract_id": testContractID, "version_id": testVersionID})

	h.HandleRecheck().ServeHTTP(rr, r)

	if rr.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", rr.Code, rr.Body.String())
	}

	// Verify DM CreateVersion was called with correct request.
	if len(dm.createCalls) != 1 {
		t.Fatalf("expected 1 CreateVersion call, got %d", len(dm.createCalls))
	}
	call := dm.createCalls[0]
	if call.DocumentID != testContractID {
		t.Errorf("CreateVersion documentID = %s, want %s", call.DocumentID, testContractID)
	}
	if call.Req.OriginType != "RE_CHECK" {
		t.Errorf("OriginType = %s, want RE_CHECK", call.Req.OriginType)
	}
	if call.Req.ParentVersionID != testVersionID {
		t.Errorf("ParentVersionID = %s, want %s", call.Req.ParentVersionID, testVersionID)
	}
	if call.Req.SourceFileKey != "uploads/org-001/key.pdf" {
		t.Errorf("SourceFileKey = %s, want uploads/org-001/key.pdf", call.Req.SourceFileKey)
	}
	if call.Req.SourceFileName != "contract.pdf" {
		t.Errorf("SourceFileName = %s, want contract.pdf", call.Req.SourceFileName)
	}
	if call.Req.SourceFileSize != 1024000 {
		t.Errorf("SourceFileSize = %d, want 1024000", call.Req.SourceFileSize)
	}
	if call.Req.SourceFileChecksum != "abc123" {
		t.Errorf("SourceFileChecksum = %s, want abc123", call.Req.SourceFileChecksum)
	}

	// Verify ProcessDocumentRequested was published with correct fields.
	if len(pub.calls) != 1 {
		t.Fatalf("expected 1 publish call, got %d", len(pub.calls))
	}
	cmd := pub.calls[0]
	if cmd.DocumentID != testContractID {
		t.Errorf("cmd.DocumentID = %s, want %s", cmd.DocumentID, testContractID)
	}
	if cmd.OrganizationID != "org-001" {
		t.Errorf("cmd.OrganizationID = %s, want org-001", cmd.OrganizationID)
	}
	if cmd.RequestedByUserID != "user-001" {
		t.Errorf("cmd.RequestedByUserID = %s, want user-001", cmd.RequestedByUserID)
	}
	if cmd.SourceFileKey != "uploads/org-001/key.pdf" {
		t.Errorf("cmd.SourceFileKey = %s, want uploads/org-001/key.pdf", cmd.SourceFileKey)
	}
	if cmd.SourceFileMIMEType != "application/pdf" {
		t.Errorf("cmd.SourceFileMIMEType = %s, want application/pdf", cmd.SourceFileMIMEType)
	}
}

func TestHandleRecheck_DM5xx(t *testing.T) {
	dm := &mockDMClient{
		getVerFn: func(_ context.Context, _, _ string) (*dmclient.DocumentVersionWithArtifacts, error) {
			return nil, dmHTTPError("GetVersion", 500, `{"error":"internal"}`)
		},
	}
	h := newTestHandler(dm)

	rr := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/v1/contracts/"+testContractID+"/versions/"+testVersionID+"/recheck", nil)
	r = withAuthContext(r)
	r = withChiParams(r, map[string]string{"contract_id": testContractID, "version_id": testVersionID})

	h.HandleRecheck().ServeHTTP(rr, r)

	if rr.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestHandleRecheck_DMCircuitOpen(t *testing.T) {
	dm := &mockDMClient{
		getVerFn: func(_ context.Context, _, _ string) (*dmclient.DocumentVersionWithArtifacts, error) {
			return nil, dmclient.ErrCircuitOpen
		},
	}
	h := newTestHandler(dm)

	rr := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/v1/contracts/"+testContractID+"/versions/"+testVersionID+"/recheck", nil)
	r = withAuthContext(r)
	r = withChiParams(r, map[string]string{"contract_id": testContractID, "version_id": testVersionID})

	h.HandleRecheck().ServeHTTP(rr, r)

	if rr.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestHandleRecheck_CreateVersionFailure(t *testing.T) {
	dm := &mockDMClient{
		getVerFn: func(_ context.Context, _, _ string) (*dmclient.DocumentVersionWithArtifacts, error) {
			return stubVersionWithArtifacts("FULLY_READY"), nil
		},
		createVerFn: func(_ context.Context, _ string, _ dmclient.CreateVersionRequest) (*dmclient.DocumentVersion, error) {
			return nil, dmHTTPError("CreateVersion", 500, `{"error":"internal"}`)
		},
	}
	h := newTestHandlerFull(dm, &mockStorage{}, &mockPublisher{}, &mockKVStore{})

	rr := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/v1/contracts/"+testContractID+"/versions/"+testVersionID+"/recheck", nil)
	r = withAuthContext(r)
	r = withChiParams(r, map[string]string{"contract_id": testContractID, "version_id": testVersionID})

	h.HandleRecheck().ServeHTTP(rr, r)

	if rr.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestHandleRecheck_BrokerFailure(t *testing.T) {
	dm := &mockDMClient{
		getVerFn: func(_ context.Context, _, _ string) (*dmclient.DocumentVersionWithArtifacts, error) {
			return stubVersionWithArtifacts("FULLY_READY"), nil
		},
	}
	pub := &mockPublisher{
		publishFn: func(_ context.Context, _ commandpub.ProcessDocumentCommand) error {
			return errors.New("broker connection lost")
		},
	}
	h := newTestHandlerFull(dm, &mockStorage{}, pub, &mockKVStore{})

	rr := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/v1/contracts/"+testContractID+"/versions/"+testVersionID+"/recheck", nil)
	r = withAuthContext(r)
	r = withChiParams(r, map[string]string{"contract_id": testContractID, "version_id": testVersionID})

	h.HandleRecheck().ServeHTTP(rr, r)

	if rr.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d: %s", rr.Code, rr.Body.String())
	}

	result := parseJSON(t, rr)
	if result["error_code"] != "BROKER_UNAVAILABLE" {
		t.Errorf("error_code = %v, want BROKER_UNAVAILABLE", result["error_code"])
	}

	// Version was already created — no rollback possible.
	if len(dm.createCalls) != 1 {
		t.Errorf("expected version to have been created before broker failure, got %d calls", len(dm.createCalls))
	}
}

func TestHandleRecheck_RedisNonCritical(t *testing.T) {
	dm := &mockDMClient{
		getVerFn: func(_ context.Context, _, _ string) (*dmclient.DocumentVersionWithArtifacts, error) {
			return stubVersionWithArtifacts("FULLY_READY"), nil
		},
	}
	kv := &mockKVStore{
		setFn: func(_ context.Context, _ string, _ string, _ time.Duration) error {
			return errors.New("redis down")
		},
	}
	h := newTestHandlerFull(dm, &mockStorage{}, &mockPublisher{}, kv)

	rr := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/v1/contracts/"+testContractID+"/versions/"+testVersionID+"/recheck", nil)
	r = withAuthContext(r)
	r = withChiParams(r, map[string]string{"contract_id": testContractID, "version_id": testVersionID})

	h.HandleRecheck().ServeHTTP(rr, r)

	// Should still return 202 — Redis failure is non-critical.
	if rr.Code != http.StatusAccepted {
		t.Fatalf("expected 202 (Redis failure is non-critical), got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestHandleRecheck_RedisTracking(t *testing.T) {
	dm := &mockDMClient{
		getVerFn: func(_ context.Context, _, _ string) (*dmclient.DocumentVersionWithArtifacts, error) {
			return stubVersionWithArtifacts("FULLY_READY"), nil
		},
		createVerFn: func(_ context.Context, _ string, _ dmclient.CreateVersionRequest) (*dmclient.DocumentVersion, error) {
			return &dmclient.DocumentVersion{
				VersionID:     "ver-new-001",
				VersionNumber: 2,
			}, nil
		},
	}
	kv := &mockKVStore{}
	h := newTestHandlerFull(dm, &mockStorage{}, &mockPublisher{}, kv)

	rr := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/v1/contracts/"+testContractID+"/versions/"+testVersionID+"/recheck", nil)
	r = withAuthContext(r)
	r = withChiParams(r, map[string]string{"contract_id": testContractID, "version_id": testVersionID})

	h.HandleRecheck().ServeHTTP(rr, r)

	if rr.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d", rr.Code)
	}

	// Verify Redis tracking was saved.
	if len(kv.calls) != 1 {
		t.Fatalf("expected 1 Redis Set call, got %d", len(kv.calls))
	}
	if !strings.HasPrefix(kv.calls[0].Key, "upload:org-001:") {
		t.Errorf("Redis key = %s, want prefix upload:org-001:", kv.calls[0].Key)
	}
	if kv.calls[0].TTL != uploadTrackingTTL {
		t.Errorf("Redis TTL = %v, want %v", kv.calls[0].TTL, uploadTrackingTTL)
	}
}

func TestHandleRecheck_EmptySourceFileKey(t *testing.T) {
	dm := &mockDMClient{
		getVerFn: func(_ context.Context, _, _ string) (*dmclient.DocumentVersionWithArtifacts, error) {
			v := stubVersionWithArtifacts("FULLY_READY")
			v.SourceFileKey = ""
			return v, nil
		},
	}
	h := newTestHandlerFull(dm, &mockStorage{}, &mockPublisher{}, &mockKVStore{})

	rr := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/v1/contracts/"+testContractID+"/versions/"+testVersionID+"/recheck", nil)
	r = withAuthContext(r)
	r = withChiParams(r, map[string]string{"contract_id": testContractID, "version_id": testVersionID})

	h.HandleRecheck().ServeHTTP(rr, r)

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 for empty source_file_key, got %d: %s", rr.Code, rr.Body.String())
	}

	result := parseJSON(t, rr)
	if result["error_code"] != "INTERNAL_ERROR" {
		t.Errorf("error_code = %v, want INTERNAL_ERROR", result["error_code"])
	}
}

func TestHandleRecheck_NoS3Upload(t *testing.T) {
	// Recheck should not touch S3 — it reuses the parent version's source file.
	storage := &mockStorage{}
	dm := &mockDMClient{
		getVerFn: func(_ context.Context, _, _ string) (*dmclient.DocumentVersionWithArtifacts, error) {
			return stubVersionWithArtifacts("FULLY_READY"), nil
		},
	}
	h := newTestHandlerFull(dm, storage, &mockPublisher{}, &mockKVStore{})

	rr := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/v1/contracts/"+testContractID+"/versions/"+testVersionID+"/recheck", nil)
	r = withAuthContext(r)
	r = withChiParams(r, map[string]string{"contract_id": testContractID, "version_id": testVersionID})

	h.HandleRecheck().ServeHTTP(rr, r)

	if rr.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d", rr.Code)
	}

	if len(storage.putCalls) != 0 {
		t.Errorf("S3 PutObject should not be called for recheck, got %d calls", len(storage.putCalls))
	}
	if len(storage.deleteCalls) != 0 {
		t.Errorf("S3 DeleteObject should not be called for recheck, got %d calls", len(storage.deleteCalls))
	}
}

// =========================================================================
// isStillProcessing tests
// =========================================================================

func TestIsStillProcessing(t *testing.T) {
	tests := []struct {
		status string
		want   bool
	}{
		{"PENDING_UPLOAD", true},
		{"PENDING_PROCESSING", true},
		{"PROCESSING_IN_PROGRESS", true},
		{"ARTIFACTS_READY", true},
		{"ANALYSIS_IN_PROGRESS", true},
		{"ANALYSIS_READY", true},
		{"REPORTS_IN_PROGRESS", true},
		{"FULLY_READY", false},
		{"PARTIALLY_AVAILABLE", false},
		{"PROCESSING_FAILED", false},
		{"REJECTED", false},
		{"UNKNOWN_STATUS", true},
	}

	for _, tt := range tests {
		t.Run(tt.status, func(t *testing.T) {
			got := isStillProcessing(tt.status)
			if got != tt.want {
				t.Errorf("isStillProcessing(%q) = %v, want %v", tt.status, got, tt.want)
			}
		})
	}
}

// =========================================================================
// Interface compliance
// =========================================================================

func TestInterfaceCompliance(t *testing.T) {
	// Verify that dmclient.Client satisfies DMClient.
	var _ DMClient = (*dmclient.Client)(nil)

	// Verify that commandpub.Publisher satisfies CommandPublisher.
	var _ CommandPublisher = (*commandpub.Publisher)(nil)
}
