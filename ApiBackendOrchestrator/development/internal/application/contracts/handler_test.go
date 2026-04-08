package contracts

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"contractpro/api-orchestrator/internal/egress/dmclient"
	"contractpro/api-orchestrator/internal/infra/observability/logger"
	"contractpro/api-orchestrator/internal/ingress/middleware/auth"
)

// ---------------------------------------------------------------------------
// Mock DM client
// ---------------------------------------------------------------------------

type mockDMClient struct {
	listFn    func(ctx context.Context, params dmclient.ListDocumentsParams) (*dmclient.DocumentList, error)
	getFn     func(ctx context.Context, documentID string) (*dmclient.DocumentWithCurrentVersion, error)
	deleteFn  func(ctx context.Context, documentID string) (*dmclient.Document, error)
	archiveFn func(ctx context.Context, documentID string) (*dmclient.Document, error)

	listCalls    []dmclient.ListDocumentsParams
	getCalls     []string
	deleteCalls  []string
	archiveCalls []string
}

func (m *mockDMClient) ListDocuments(ctx context.Context, params dmclient.ListDocumentsParams) (*dmclient.DocumentList, error) {
	m.listCalls = append(m.listCalls, params)
	if m.listFn != nil {
		return m.listFn(ctx, params)
	}
	return &dmclient.DocumentList{Items: []dmclient.Document{}, Total: 0, Page: params.Page, Size: params.Size}, nil
}

func (m *mockDMClient) GetDocument(ctx context.Context, documentID string) (*dmclient.DocumentWithCurrentVersion, error) {
	m.getCalls = append(m.getCalls, documentID)
	if m.getFn != nil {
		return m.getFn(ctx, documentID)
	}
	return &dmclient.DocumentWithCurrentVersion{Document: stubDocument()}, nil
}

func (m *mockDMClient) DeleteDocument(ctx context.Context, documentID string) (*dmclient.Document, error) {
	m.deleteCalls = append(m.deleteCalls, documentID)
	if m.deleteFn != nil {
		return m.deleteFn(ctx, documentID)
	}
	doc := stubDocument()
	doc.Status = "DELETED"
	return &doc, nil
}

func (m *mockDMClient) ArchiveDocument(ctx context.Context, documentID string) (*dmclient.Document, error) {
	m.archiveCalls = append(m.archiveCalls, documentID)
	if m.archiveFn != nil {
		return m.archiveFn(ctx, documentID)
	}
	doc := stubDocument()
	doc.Status = "ARCHIVED"
	return &doc, nil
}

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

const testContractID = "550e8400-e29b-41d4-a716-446655440000"

func newTestHandler(dm *mockDMClient) *Handler {
	log := logger.NewLogger("error")
	return NewHandler(dm, log)
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

func withChiParam(r *http.Request, key, value string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add(key, value)
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}

func parseJSON(t *testing.T, rr *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var result map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &result); err != nil {
		t.Fatalf("failed to parse JSON response: %v", err)
	}
	return result
}

func stubDocument() dmclient.Document {
	return dmclient.Document{
		DocumentID:       testContractID,
		OrganizationID:   "org-001",
		Title:            "Договор поставки №42",
		CurrentVersionID: strPtr("ver-001"),
		Status:           "ACTIVE",
		CreatedByUserID:  "user-001",
		CreatedAt:        time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC),
		UpdatedAt:        time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC),
	}
}

func stubDocumentWithVersion(artifactStatus string) dmclient.DocumentWithCurrentVersion {
	return dmclient.DocumentWithCurrentVersion{
		Document: stubDocument(),
		CurrentVersion: &dmclient.DocumentVersion{
			VersionID:          "ver-001",
			DocumentID:         testContractID,
			VersionNumber:      1,
			ParentVersionID:    nil,
			OriginType:         "UPLOAD",
			OriginDescription:  nil,
			SourceFileKey:      "uploads/org-001/secret-key.pdf",
			SourceFileName:     "contract.pdf",
			SourceFileSize:     1024000,
			SourceFileChecksum: "abc123",
			ArtifactStatus:     artifactStatus,
			CreatedByUserID:    "user-001",
			CreatedAt:          time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC),
		},
	}
}

func stubDocumentList(n int) *dmclient.DocumentList {
	items := make([]dmclient.Document, n)
	for i := range items {
		items[i] = stubDocument()
	}
	return &dmclient.DocumentList{
		Items: items,
		Total: n,
		Page:  1,
		Size:  20,
	}
}

func strPtr(s string) *string { return &s }

func dmHTTPError(operation string, statusCode int, body string) *dmclient.DMError {
	return &dmclient.DMError{
		Operation:  operation,
		StatusCode: statusCode,
		Body:       []byte(body),
		Retryable:  statusCode >= 500,
	}
}

// =========================================================================
// HandleList tests
// =========================================================================

func TestHandleList_Success(t *testing.T) {
	dm := &mockDMClient{
		listFn: func(_ context.Context, params dmclient.ListDocumentsParams) (*dmclient.DocumentList, error) {
			return stubDocumentList(2), nil
		},
	}
	h := newTestHandler(dm)

	rr := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/contracts", nil)
	r = withAuthContext(r)

	h.HandleList().ServeHTTP(rr, r)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	body := parseJSON(t, rr)
	items, ok := body["items"].([]any)
	if !ok || len(items) != 2 {
		t.Fatalf("expected 2 items, got %v", body["items"])
	}

	item := items[0].(map[string]any)
	if item["contract_id"] != testContractID {
		t.Errorf("contract_id = %v, want %s", item["contract_id"], testContractID)
	}
	if item["title"] != "Договор поставки №42" {
		t.Errorf("title = %v", item["title"])
	}
	if item["status"] != "ACTIVE" {
		t.Errorf("status = %v", item["status"])
	}

	if body["total"].(float64) != 2 {
		t.Errorf("total = %v", body["total"])
	}
}

func TestHandleList_EmptyList(t *testing.T) {
	dm := &mockDMClient{}
	h := newTestHandler(dm)

	rr := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/contracts", nil)
	r = withAuthContext(r)

	h.HandleList().ServeHTTP(rr, r)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	body := parseJSON(t, rr)
	items := body["items"].([]any)
	if len(items) != 0 {
		t.Errorf("expected empty items, got %d", len(items))
	}

	// Verify items is [] not null.
	if !strings.Contains(rr.Body.String(), `"items":[]`) {
		t.Error("items should be [] not null")
	}
}

func TestHandleList_CustomPageSize(t *testing.T) {
	dm := &mockDMClient{}
	h := newTestHandler(dm)

	rr := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/contracts?page=3&size=50", nil)
	r = withAuthContext(r)

	h.HandleList().ServeHTTP(rr, r)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	if len(dm.listCalls) != 1 {
		t.Fatal("expected 1 list call")
	}
	if dm.listCalls[0].Page != 3 {
		t.Errorf("page = %d, want 3", dm.listCalls[0].Page)
	}
	if dm.listCalls[0].Size != 50 {
		t.Errorf("size = %d, want 50", dm.listCalls[0].Size)
	}
}

func TestHandleList_StatusFilter(t *testing.T) {
	dm := &mockDMClient{}
	h := newTestHandler(dm)

	rr := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/contracts?status=ARCHIVED", nil)
	r = withAuthContext(r)

	h.HandleList().ServeHTTP(rr, r)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	if dm.listCalls[0].Status != "ARCHIVED" {
		t.Errorf("status = %q, want ARCHIVED", dm.listCalls[0].Status)
	}
}

func TestHandleList_InvalidPage_NotNumber(t *testing.T) {
	dm := &mockDMClient{}
	h := newTestHandler(dm)

	rr := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/contracts?page=abc", nil)
	r = withAuthContext(r)

	h.HandleList().ServeHTTP(rr, r)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}

	body := parseJSON(t, rr)
	if body["error_code"] != "VALIDATION_ERROR" {
		t.Errorf("error_code = %v", body["error_code"])
	}
}

func TestHandleList_InvalidPage_Zero(t *testing.T) {
	dm := &mockDMClient{}
	h := newTestHandler(dm)

	rr := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/contracts?page=0", nil)
	r = withAuthContext(r)

	h.HandleList().ServeHTTP(rr, r)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestHandleList_InvalidPage_Negative(t *testing.T) {
	dm := &mockDMClient{}
	h := newTestHandler(dm)

	rr := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/contracts?page=-1", nil)
	r = withAuthContext(r)

	h.HandleList().ServeHTTP(rr, r)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestHandleList_InvalidSize_TooLarge(t *testing.T) {
	dm := &mockDMClient{}
	h := newTestHandler(dm)

	rr := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/contracts?size=101", nil)
	r = withAuthContext(r)

	h.HandleList().ServeHTTP(rr, r)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestHandleList_InvalidSize_Zero(t *testing.T) {
	dm := &mockDMClient{}
	h := newTestHandler(dm)

	rr := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/contracts?size=0", nil)
	r = withAuthContext(r)

	h.HandleList().ServeHTTP(rr, r)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestHandleList_InvalidStatus(t *testing.T) {
	dm := &mockDMClient{}
	h := newTestHandler(dm)

	rr := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/contracts?status=INVALID", nil)
	r = withAuthContext(r)

	h.HandleList().ServeHTTP(rr, r)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}

	body := parseJSON(t, rr)
	if body["error_code"] != "VALIDATION_ERROR" {
		t.Errorf("error_code = %v", body["error_code"])
	}
}

func TestHandleList_SearchTooLong(t *testing.T) {
	dm := &mockDMClient{}
	h := newTestHandler(dm)

	longSearch := strings.Repeat("а", 201) // 201 Cyrillic runes
	rr := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/contracts?search="+longSearch, nil)
	r = withAuthContext(r)

	h.HandleList().ServeHTTP(rr, r)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestHandleList_SearchAccepted(t *testing.T) {
	dm := &mockDMClient{}
	h := newTestHandler(dm)

	rr := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/contracts?search=договор", nil)
	r = withAuthContext(r)

	h.HandleList().ServeHTTP(rr, r)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

func TestHandleList_DMError_5xx(t *testing.T) {
	dm := &mockDMClient{
		listFn: func(_ context.Context, _ dmclient.ListDocumentsParams) (*dmclient.DocumentList, error) {
			return nil, dmHTTPError("ListDocuments", 500, "internal server error")
		},
	}
	h := newTestHandler(dm)

	rr := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/contracts", nil)
	r = withAuthContext(r)

	h.HandleList().ServeHTTP(rr, r)

	if rr.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d", rr.Code)
	}

	body := parseJSON(t, rr)
	if body["error_code"] != "DM_UNAVAILABLE" {
		t.Errorf("error_code = %v", body["error_code"])
	}
}

func TestHandleList_DMError_CircuitOpen(t *testing.T) {
	dm := &mockDMClient{
		listFn: func(_ context.Context, _ dmclient.ListDocumentsParams) (*dmclient.DocumentList, error) {
			return nil, dmclient.ErrCircuitOpen
		},
	}
	h := newTestHandler(dm)

	rr := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/contracts", nil)
	r = withAuthContext(r)

	h.HandleList().ServeHTTP(rr, r)

	if rr.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d", rr.Code)
	}

	body := parseJSON(t, rr)
	if body["error_code"] != "DM_UNAVAILABLE" {
		t.Errorf("error_code = %v", body["error_code"])
	}
}

func TestHandleList_NoAuthContext(t *testing.T) {
	dm := &mockDMClient{}
	h := newTestHandler(dm)

	rr := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/contracts", nil)
	// No auth context.

	h.HandleList().ServeHTTP(rr, r)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
}

// =========================================================================
// HandleGet tests
// =========================================================================

func TestHandleGet_Success_WithVersion(t *testing.T) {
	dm := &mockDMClient{
		getFn: func(_ context.Context, _ string) (*dmclient.DocumentWithCurrentVersion, error) {
			doc := stubDocumentWithVersion("FULLY_READY")
			return &doc, nil
		},
	}
	h := newTestHandler(dm)

	rr := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/contracts/"+testContractID, nil)
	r = withAuthContext(r)
	r = withChiParam(r, "contract_id", testContractID)

	h.HandleGet().ServeHTTP(rr, r)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	body := parseJSON(t, rr)
	if body["contract_id"] != testContractID {
		t.Errorf("contract_id = %v", body["contract_id"])
	}
	if body["processing_status"] != "READY" {
		t.Errorf("processing_status = %v, want READY", body["processing_status"])
	}

	cv := body["current_version"].(map[string]any)
	if cv["version_id"] != "ver-001" {
		t.Errorf("version_id = %v", cv["version_id"])
	}
	if cv["contract_id"] != testContractID {
		t.Errorf("current_version.contract_id = %v", cv["contract_id"])
	}
}

func TestHandleGet_Success_WithoutVersion(t *testing.T) {
	dm := &mockDMClient{
		getFn: func(_ context.Context, _ string) (*dmclient.DocumentWithCurrentVersion, error) {
			doc := stubDocument()
			return &dmclient.DocumentWithCurrentVersion{Document: doc, CurrentVersion: nil}, nil
		},
	}
	h := newTestHandler(dm)

	rr := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/contracts/"+testContractID, nil)
	r = withAuthContext(r)
	r = withChiParam(r, "contract_id", testContractID)

	h.HandleGet().ServeHTTP(rr, r)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	body := parseJSON(t, rr)
	if body["current_version"] != nil {
		t.Errorf("current_version should be null, got %v", body["current_version"])
	}
	if body["processing_status"] != nil {
		t.Errorf("processing_status should be null, got %v", body["processing_status"])
	}
}

func TestHandleGet_InvalidContractID_NotUUID(t *testing.T) {
	dm := &mockDMClient{}
	h := newTestHandler(dm)

	rr := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/contracts/not-a-uuid", nil)
	r = withAuthContext(r)
	r = withChiParam(r, "contract_id", "not-a-uuid")

	h.HandleGet().ServeHTTP(rr, r)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}

	body := parseJSON(t, rr)
	if body["error_code"] != "VALIDATION_ERROR" {
		t.Errorf("error_code = %v", body["error_code"])
	}
}

func TestHandleGet_InvalidContractID_Empty(t *testing.T) {
	dm := &mockDMClient{}
	h := newTestHandler(dm)

	rr := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/contracts/", nil)
	r = withAuthContext(r)
	r = withChiParam(r, "contract_id", "")

	h.HandleGet().ServeHTTP(rr, r)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestHandleGet_DMError_NotFound(t *testing.T) {
	dm := &mockDMClient{
		getFn: func(_ context.Context, _ string) (*dmclient.DocumentWithCurrentVersion, error) {
			return nil, dmHTTPError("GetDocument", 404, `{"code":"NOT_FOUND"}`)
		},
	}
	h := newTestHandler(dm)

	rr := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/contracts/"+testContractID, nil)
	r = withAuthContext(r)
	r = withChiParam(r, "contract_id", testContractID)

	h.HandleGet().ServeHTTP(rr, r)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}

	body := parseJSON(t, rr)
	if body["error_code"] != "DOCUMENT_NOT_FOUND" {
		t.Errorf("error_code = %v", body["error_code"])
	}
}

func TestHandleGet_DMError_5xx(t *testing.T) {
	dm := &mockDMClient{
		getFn: func(_ context.Context, _ string) (*dmclient.DocumentWithCurrentVersion, error) {
			return nil, dmHTTPError("GetDocument", 503, "service unavailable")
		},
	}
	h := newTestHandler(dm)

	rr := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/contracts/"+testContractID, nil)
	r = withAuthContext(r)
	r = withChiParam(r, "contract_id", testContractID)

	h.HandleGet().ServeHTTP(rr, r)

	if rr.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d", rr.Code)
	}
}

func TestHandleGet_ProcessingStatus_AllMappings(t *testing.T) {
	tests := []struct {
		artifactStatus string
		wantStatus     string
	}{
		{"PENDING_UPLOAD", "UPLOADED"},
		{"PENDING_PROCESSING", "QUEUED"},
		{"PROCESSING_IN_PROGRESS", "PROCESSING"},
		{"ARTIFACTS_READY", "ANALYZING"},
		{"ANALYSIS_IN_PROGRESS", "ANALYZING"},
		{"ANALYSIS_READY", "GENERATING_REPORTS"},
		{"REPORTS_IN_PROGRESS", "GENERATING_REPORTS"},
		{"FULLY_READY", "READY"},
		{"PARTIALLY_AVAILABLE", "PARTIALLY_FAILED"},
		{"PROCESSING_FAILED", "FAILED"},
		{"REJECTED", "REJECTED"},
	}

	for _, tt := range tests {
		t.Run(tt.artifactStatus, func(t *testing.T) {
			dm := &mockDMClient{
				getFn: func(_ context.Context, _ string) (*dmclient.DocumentWithCurrentVersion, error) {
					doc := stubDocumentWithVersion(tt.artifactStatus)
					return &doc, nil
				},
			}
			h := newTestHandler(dm)

			rr := httptest.NewRecorder()
			r := httptest.NewRequest(http.MethodGet, "/api/v1/contracts/"+testContractID, nil)
			r = withAuthContext(r)
			r = withChiParam(r, "contract_id", testContractID)

			h.HandleGet().ServeHTTP(rr, r)

			if rr.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d", rr.Code)
			}

			body := parseJSON(t, rr)
			if body["processing_status"] != tt.wantStatus {
				t.Errorf("processing_status = %v, want %s", body["processing_status"], tt.wantStatus)
			}
		})
	}
}

func TestHandleGet_SourceFileKey_NotExposed(t *testing.T) {
	dm := &mockDMClient{
		getFn: func(_ context.Context, _ string) (*dmclient.DocumentWithCurrentVersion, error) {
			doc := stubDocumentWithVersion("FULLY_READY")
			return &doc, nil
		},
	}
	h := newTestHandler(dm)

	rr := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/contracts/"+testContractID, nil)
	r = withAuthContext(r)
	r = withChiParam(r, "contract_id", testContractID)

	h.HandleGet().ServeHTTP(rr, r)

	if strings.Contains(rr.Body.String(), "source_file_key") {
		t.Error("response should not contain source_file_key")
	}
	if strings.Contains(rr.Body.String(), "artifact_status") {
		t.Error("response should not contain artifact_status")
	}
}

func TestHandleGet_NoAuthContext(t *testing.T) {
	dm := &mockDMClient{}
	h := newTestHandler(dm)

	rr := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/contracts/"+testContractID, nil)
	r = withChiParam(r, "contract_id", testContractID)
	// No auth context.

	h.HandleGet().ServeHTTP(rr, r)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
}

// =========================================================================
// HandleArchive tests
// =========================================================================

func TestHandleArchive_Success(t *testing.T) {
	dm := &mockDMClient{}
	h := newTestHandler(dm)

	rr := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/v1/contracts/"+testContractID+"/archive", nil)
	r = withAuthContext(r)
	r = withChiParam(r, "contract_id", testContractID)

	h.HandleArchive().ServeHTTP(rr, r)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	body := parseJSON(t, rr)
	if body["status"] != "ARCHIVED" {
		t.Errorf("status = %v, want ARCHIVED", body["status"])
	}

	if len(dm.archiveCalls) != 1 || dm.archiveCalls[0] != testContractID {
		t.Errorf("ArchiveDocument called with %v", dm.archiveCalls)
	}
}

func TestHandleArchive_InvalidContractID(t *testing.T) {
	dm := &mockDMClient{}
	h := newTestHandler(dm)

	rr := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/v1/contracts/bad-id/archive", nil)
	r = withAuthContext(r)
	r = withChiParam(r, "contract_id", "bad-id")

	h.HandleArchive().ServeHTTP(rr, r)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestHandleArchive_DMError_NotFound(t *testing.T) {
	dm := &mockDMClient{
		archiveFn: func(_ context.Context, _ string) (*dmclient.Document, error) {
			return nil, dmHTTPError("ArchiveDocument", 404, `{"code":"NOT_FOUND"}`)
		},
	}
	h := newTestHandler(dm)

	rr := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/v1/contracts/"+testContractID+"/archive", nil)
	r = withAuthContext(r)
	r = withChiParam(r, "contract_id", testContractID)

	h.HandleArchive().ServeHTTP(rr, r)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestHandleArchive_DMError_AlreadyArchived(t *testing.T) {
	dm := &mockDMClient{
		archiveFn: func(_ context.Context, _ string) (*dmclient.Document, error) {
			return nil, dmHTTPError("ArchiveDocument", 409, `{"code":"DOCUMENT_ARCHIVED"}`)
		},
	}
	h := newTestHandler(dm)

	rr := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/v1/contracts/"+testContractID+"/archive", nil)
	r = withAuthContext(r)
	r = withChiParam(r, "contract_id", testContractID)

	h.HandleArchive().ServeHTTP(rr, r)

	if rr.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d", rr.Code)
	}

	body := parseJSON(t, rr)
	if body["error_code"] != "DOCUMENT_ARCHIVED" {
		t.Errorf("error_code = %v", body["error_code"])
	}
}

func TestHandleArchive_DMError_Deleted(t *testing.T) {
	dm := &mockDMClient{
		archiveFn: func(_ context.Context, _ string) (*dmclient.Document, error) {
			return nil, dmHTTPError("ArchiveDocument", 409, `{"code":"DOCUMENT_DELETED"}`)
		},
	}
	h := newTestHandler(dm)

	rr := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/v1/contracts/"+testContractID+"/archive", nil)
	r = withAuthContext(r)
	r = withChiParam(r, "contract_id", testContractID)

	h.HandleArchive().ServeHTTP(rr, r)

	if rr.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d", rr.Code)
	}

	body := parseJSON(t, rr)
	if body["error_code"] != "DOCUMENT_DELETED" {
		t.Errorf("error_code = %v", body["error_code"])
	}
}

func TestHandleArchive_DMError_5xx(t *testing.T) {
	dm := &mockDMClient{
		archiveFn: func(_ context.Context, _ string) (*dmclient.Document, error) {
			return nil, dmHTTPError("ArchiveDocument", 500, "error")
		},
	}
	h := newTestHandler(dm)

	rr := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/v1/contracts/"+testContractID+"/archive", nil)
	r = withAuthContext(r)
	r = withChiParam(r, "contract_id", testContractID)

	h.HandleArchive().ServeHTTP(rr, r)

	if rr.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d", rr.Code)
	}
}

func TestHandleArchive_NoAuthContext(t *testing.T) {
	dm := &mockDMClient{}
	h := newTestHandler(dm)

	rr := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/v1/contracts/"+testContractID+"/archive", nil)
	r = withChiParam(r, "contract_id", testContractID)

	h.HandleArchive().ServeHTTP(rr, r)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
}

// =========================================================================
// HandleDelete tests
// =========================================================================

func TestHandleDelete_Success(t *testing.T) {
	dm := &mockDMClient{}
	h := newTestHandler(dm)

	rr := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodDelete, "/api/v1/contracts/"+testContractID, nil)
	r = withAuthContext(r)
	r = withChiParam(r, "contract_id", testContractID)

	h.HandleDelete().ServeHTTP(rr, r)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	body := parseJSON(t, rr)
	if body["status"] != "DELETED" {
		t.Errorf("status = %v, want DELETED", body["status"])
	}

	if len(dm.deleteCalls) != 1 || dm.deleteCalls[0] != testContractID {
		t.Errorf("DeleteDocument called with %v", dm.deleteCalls)
	}
}

func TestHandleDelete_InvalidContractID(t *testing.T) {
	dm := &mockDMClient{}
	h := newTestHandler(dm)

	rr := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodDelete, "/api/v1/contracts/bad-id", nil)
	r = withAuthContext(r)
	r = withChiParam(r, "contract_id", "bad-id")

	h.HandleDelete().ServeHTTP(rr, r)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestHandleDelete_DMError_NotFound(t *testing.T) {
	dm := &mockDMClient{
		deleteFn: func(_ context.Context, _ string) (*dmclient.Document, error) {
			return nil, dmHTTPError("DeleteDocument", 404, `{"code":"NOT_FOUND"}`)
		},
	}
	h := newTestHandler(dm)

	rr := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodDelete, "/api/v1/contracts/"+testContractID, nil)
	r = withAuthContext(r)
	r = withChiParam(r, "contract_id", testContractID)

	h.HandleDelete().ServeHTTP(rr, r)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestHandleDelete_DMError_AlreadyDeleted(t *testing.T) {
	dm := &mockDMClient{
		deleteFn: func(_ context.Context, _ string) (*dmclient.Document, error) {
			return nil, dmHTTPError("DeleteDocument", 409, `{"code":"DOCUMENT_DELETED"}`)
		},
	}
	h := newTestHandler(dm)

	rr := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodDelete, "/api/v1/contracts/"+testContractID, nil)
	r = withAuthContext(r)
	r = withChiParam(r, "contract_id", testContractID)

	h.HandleDelete().ServeHTTP(rr, r)

	if rr.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d", rr.Code)
	}

	body := parseJSON(t, rr)
	if body["error_code"] != "DOCUMENT_DELETED" {
		t.Errorf("error_code = %v", body["error_code"])
	}
}

func TestHandleDelete_DMError_Archived(t *testing.T) {
	dm := &mockDMClient{
		deleteFn: func(_ context.Context, _ string) (*dmclient.Document, error) {
			return nil, dmHTTPError("DeleteDocument", 409, `{"code":"DOCUMENT_ARCHIVED"}`)
		},
	}
	h := newTestHandler(dm)

	rr := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodDelete, "/api/v1/contracts/"+testContractID, nil)
	r = withAuthContext(r)
	r = withChiParam(r, "contract_id", testContractID)

	h.HandleDelete().ServeHTTP(rr, r)

	if rr.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d", rr.Code)
	}

	body := parseJSON(t, rr)
	if body["error_code"] != "DOCUMENT_ARCHIVED" {
		t.Errorf("error_code = %v", body["error_code"])
	}
}

func TestHandleDelete_DMError_5xx(t *testing.T) {
	dm := &mockDMClient{
		deleteFn: func(_ context.Context, _ string) (*dmclient.Document, error) {
			return nil, dmHTTPError("DeleteDocument", 500, "error")
		},
	}
	h := newTestHandler(dm)

	rr := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodDelete, "/api/v1/contracts/"+testContractID, nil)
	r = withAuthContext(r)
	r = withChiParam(r, "contract_id", testContractID)

	h.HandleDelete().ServeHTTP(rr, r)

	if rr.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d", rr.Code)
	}
}

func TestHandleDelete_NoAuthContext(t *testing.T) {
	dm := &mockDMClient{}
	h := newTestHandler(dm)

	rr := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodDelete, "/api/v1/contracts/"+testContractID, nil)
	r = withChiParam(r, "contract_id", testContractID)

	h.HandleDelete().ServeHTTP(rr, r)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
}

// =========================================================================
// Mapping function tests
// =========================================================================

func TestMapProcessingStatus_AllValues(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"PENDING_UPLOAD", "UPLOADED"},
		{"PENDING_PROCESSING", "QUEUED"},
		{"PROCESSING_IN_PROGRESS", "PROCESSING"},
		{"ARTIFACTS_READY", "ANALYZING"},
		{"ANALYSIS_IN_PROGRESS", "ANALYZING"},
		{"ANALYSIS_READY", "GENERATING_REPORTS"},
		{"REPORTS_IN_PROGRESS", "GENERATING_REPORTS"},
		{"FULLY_READY", "READY"},
		{"PARTIALLY_AVAILABLE", "PARTIALLY_FAILED"},
		{"PROCESSING_FAILED", "FAILED"},
		{"REJECTED", "REJECTED"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := mapProcessingStatus(tt.input)
			if got != tt.want {
				t.Errorf("mapProcessingStatus(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestMapProcessingStatus_Unknown(t *testing.T) {
	got := mapProcessingStatus("SOMETHING_NEW")
	if got != "UNKNOWN" {
		t.Errorf("mapProcessingStatus(unknown) = %q, want UNKNOWN", got)
	}
}

func TestMapDocumentToContractSummary(t *testing.T) {
	doc := stubDocument()
	cs := mapDocumentToContractSummary(doc)

	if cs.ContractID != doc.DocumentID {
		t.Errorf("ContractID = %v, want %v", cs.ContractID, doc.DocumentID)
	}
	if cs.Title != doc.Title {
		t.Errorf("Title = %v", cs.Title)
	}
	if cs.Status != doc.Status {
		t.Errorf("Status = %v", cs.Status)
	}
	if cs.CurrentVersionNumber != nil {
		t.Error("CurrentVersionNumber should be nil")
	}
	if cs.ProcessingStatus != nil {
		t.Error("ProcessingStatus should be nil")
	}
	if cs.CreatedAt != "2026-01-15T10:00:00Z" {
		t.Errorf("CreatedAt = %v", cs.CreatedAt)
	}
	if cs.UpdatedAt != "2026-01-15T12:00:00Z" {
		t.Errorf("UpdatedAt = %v", cs.UpdatedAt)
	}
}

func TestMapDocumentWithVersion_WithVersion(t *testing.T) {
	doc := stubDocumentWithVersion("FULLY_READY")
	cd := mapDocumentWithVersionToContractDetails(doc)

	if cd.ContractID != doc.DocumentID {
		t.Errorf("ContractID = %v", cd.ContractID)
	}
	if cd.CurrentVersion == nil {
		t.Fatal("CurrentVersion should not be nil")
	}
	if cd.CurrentVersion.ContractID != doc.DocumentID {
		t.Errorf("CurrentVersion.ContractID = %v, want %v", cd.CurrentVersion.ContractID, doc.DocumentID)
	}
	if cd.ProcessingStatus == nil || *cd.ProcessingStatus != "READY" {
		t.Errorf("ProcessingStatus = %v, want READY", cd.ProcessingStatus)
	}
	if cd.CreatedByUserID != doc.CreatedByUserID {
		t.Errorf("CreatedByUserID = %v", cd.CreatedByUserID)
	}
}

func TestMapDocumentWithVersion_NilVersion(t *testing.T) {
	doc := dmclient.DocumentWithCurrentVersion{
		Document:       stubDocument(),
		CurrentVersion: nil,
	}
	cd := mapDocumentWithVersionToContractDetails(doc)

	if cd.CurrentVersion != nil {
		t.Error("CurrentVersion should be nil")
	}
	if cd.ProcessingStatus != nil {
		t.Error("ProcessingStatus should be nil")
	}
}

// =========================================================================
// handleDMError tests
// =========================================================================

func TestHandleGet_DMError_400(t *testing.T) {
	dm := &mockDMClient{
		getFn: func(_ context.Context, _ string) (*dmclient.DocumentWithCurrentVersion, error) {
			return nil, dmHTTPError("GetDocument", 400, `{"message":"bad request"}`)
		},
	}
	h := newTestHandler(dm)

	rr := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/contracts/"+testContractID, nil)
	r = withAuthContext(r)
	r = withChiParam(r, "contract_id", testContractID)

	h.HandleGet().ServeHTTP(rr, r)

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rr.Code)
	}

	body := parseJSON(t, rr)
	if body["error_code"] != "INTERNAL_ERROR" {
		t.Errorf("error_code = %v, want INTERNAL_ERROR", body["error_code"])
	}
}

func TestHandleDMError_TransportError(t *testing.T) {
	dm := &mockDMClient{}
	h := newTestHandler(dm)

	rr := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r = withAuthContext(r)

	transportErr := &dmclient.DMError{
		Operation: "GetDocument",
		Retryable: true,
		Cause:     errors.New("connection refused"),
	}

	h.handleDMError(r.Context(), rr, r, transportErr, "GetDocument", "document")

	if rr.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d", rr.Code)
	}
}

func TestHandleDMError_UnknownError(t *testing.T) {
	dm := &mockDMClient{}
	h := newTestHandler(dm)

	rr := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r = withAuthContext(r)

	h.handleDMError(r.Context(), rr, r, errors.New("something weird"), "GetDocument", "document")

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rr.Code)
	}
}

// =========================================================================
// Interface check
// =========================================================================

func TestDMClientInterface(t *testing.T) {
	// Compile-time check is in handler.go, this test documents intent.
	var _ DMClient = (*mockDMClient)(nil)
}

// =========================================================================
// Response JSON format tests
// =========================================================================

func TestHandleList_ContentType(t *testing.T) {
	dm := &mockDMClient{}
	h := newTestHandler(dm)

	rr := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/contracts", nil)
	r = withAuthContext(r)

	h.HandleList().ServeHTTP(rr, r)

	ct := rr.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
}

func TestHandleGet_TimeFormat_RFC3339(t *testing.T) {
	dm := &mockDMClient{
		getFn: func(_ context.Context, _ string) (*dmclient.DocumentWithCurrentVersion, error) {
			doc := stubDocumentWithVersion("FULLY_READY")
			return &doc, nil
		},
	}
	h := newTestHandler(dm)

	rr := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/contracts/"+testContractID, nil)
	r = withAuthContext(r)
	r = withChiParam(r, "contract_id", testContractID)

	h.HandleGet().ServeHTTP(rr, r)

	body := parseJSON(t, rr)
	createdAt, ok := body["created_at"].(string)
	if !ok {
		t.Fatal("created_at should be string")
	}
	if _, err := time.Parse(time.RFC3339, createdAt); err != nil {
		t.Errorf("created_at is not RFC3339: %v", createdAt)
	}
}
