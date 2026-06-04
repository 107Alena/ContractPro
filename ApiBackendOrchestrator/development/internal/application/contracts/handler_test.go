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
	listFn         func(ctx context.Context, params dmclient.ListDocumentsParams) (*dmclient.DocumentList, error)
	listAnalysisFn func(ctx context.Context, params dmclient.ListDocumentsParams) (*dmclient.DocumentAnalysisList, error)
	statsFn        func(ctx context.Context, params dmclient.DocumentStatsParams) (*dmclient.DocumentStats, error)
	getFn          func(ctx context.Context, documentID string) (*dmclient.DocumentWithCurrentVersion, error)
	deleteFn       func(ctx context.Context, documentID string) (*dmclient.Document, error)
	archiveFn      func(ctx context.Context, documentID string) (*dmclient.Document, error)

	listCalls         []dmclient.ListDocumentsParams
	listAnalysisCalls []dmclient.ListDocumentsParams
	statsCalls        []dmclient.DocumentStatsParams
	getCalls          []string
	deleteCalls       []string
	archiveCalls      []string
}

func (m *mockDMClient) ListDocuments(ctx context.Context, params dmclient.ListDocumentsParams) (*dmclient.DocumentList, error) {
	m.listCalls = append(m.listCalls, params)
	if m.listFn != nil {
		return m.listFn(ctx, params)
	}
	return &dmclient.DocumentList{Items: []dmclient.Document{}, Total: 0, Page: params.Page, Size: params.Size}, nil
}

func (m *mockDMClient) ListDocumentsWithAnalysis(ctx context.Context, params dmclient.ListDocumentsParams) (*dmclient.DocumentAnalysisList, error) {
	m.listAnalysisCalls = append(m.listAnalysisCalls, params)
	if m.listAnalysisFn != nil {
		return m.listAnalysisFn(ctx, params)
	}
	return &dmclient.DocumentAnalysisList{Items: []dmclient.DocumentWithAnalysis{}, Total: 0, Page: params.Page, Size: params.Size}, nil
}

func (m *mockDMClient) GetDocumentStats(ctx context.Context, params dmclient.DocumentStatsParams) (*dmclient.DocumentStats, error) {
	m.statsCalls = append(m.statsCalls, params)
	if m.statsFn != nil {
		return m.statsFn(ctx, params)
	}
	return &dmclient.DocumentStats{ByArtifactStatus: map[string]int{}, NotStarted: 0, Total: 0}, nil
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
	return NewHandler(dm, log, false, false)
}

// newAnalysisHandler builds a handler with list-aggregation enabled
// (ORCH-TASK-056), exercising the DM include=analysis read-contract path.
func newAnalysisHandler(dm *mockDMClient) *Handler {
	log := logger.NewLogger("error")
	return NewHandler(dm, log, true, false)
}

// newStatsHandler builds a handler with the stats aggregate enabled
// (ORCH-TASK-057), exercising the DM count-by-artifact_status read-contract.
func newStatsHandler(dm *mockDMClient) *Handler {
	log := logger.NewLogger("error")
	return NewHandler(dm, log, false, true)
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

// stubDocWithAnalysis builds a DM DocumentWithAnalysis for list-aggregation
// tests. artifactStatus drives the current version's status; analysis (may be
// nil) carries the aggregate. A nil currentVersion is produced when
// artifactStatus is empty.
func stubDocWithAnalysis(id, artifactStatus string, analysis *dmclient.DocumentAnalysisAggregate) dmclient.DocumentWithAnalysis {
	doc := stubDocument()
	doc.DocumentID = id
	d := dmclient.DocumentWithAnalysis{Document: doc, Analysis: analysis}
	if artifactStatus != "" {
		d.CurrentVersion = &dmclient.DocumentVersion{
			VersionID:      "ver-" + id,
			DocumentID:     id,
			VersionNumber:  1,
			ArtifactStatus: artifactStatus,
			CreatedAt:      time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC),
		}
	}
	return d
}

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

// =========================================================================
// HandleList — list-aggregation (ORCH-TASK-056)
// =========================================================================

func aggList(items ...dmclient.DocumentWithAnalysis) *dmclient.DocumentAnalysisList {
	return &dmclient.DocumentAnalysisList{Items: items, Total: len(items), Page: 1, Size: 20}
}

func TestHandleList_Analysis_PopulatesFields(t *testing.T) {
	analysis := &dmclient.DocumentAnalysisAggregate{
		ContractType: strPtr("LEASE"),
		RiskLevel:    strPtr("high"),
		RiskCounts:   &dmclient.RiskCounts{High: 3, Medium: 2, Low: 1},
	}
	dm := &mockDMClient{
		listAnalysisFn: func(_ context.Context, _ dmclient.ListDocumentsParams) (*dmclient.DocumentAnalysisList, error) {
			return aggList(stubDocWithAnalysis(testContractID, "FULLY_READY", analysis)), nil
		},
	}
	h := newAnalysisHandler(dm)

	rr := httptest.NewRecorder()
	r := withAuthContext(httptest.NewRequest(http.MethodGet, "/api/v1/contracts", nil))
	h.HandleList().ServeHTTP(rr, r)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	item := parseJSON(t, rr)["items"].([]any)[0].(map[string]any)
	if item["contract_type"] != "LEASE" {
		t.Errorf("contract_type = %v, want LEASE", item["contract_type"])
	}
	if item["risk_level"] != "high" {
		t.Errorf("risk_level = %v, want high", item["risk_level"])
	}
	if item["processing_status"] != "READY" {
		t.Errorf("processing_status = %v, want READY", item["processing_status"])
	}
	if item["current_version_number"].(float64) != 1 {
		t.Errorf("current_version_number = %v, want 1", item["current_version_number"])
	}
	rc, ok := item["risk_counts"].(map[string]any)
	if !ok {
		t.Fatalf("risk_counts missing/null: %v", item["risk_counts"])
	}
	if rc["high"].(float64) != 3 || rc["medium"].(float64) != 2 || rc["low"].(float64) != 1 {
		t.Errorf("risk_counts = %v", rc)
	}
	// no N+1: enriched method called once, plain method never.
	if len(dm.listAnalysisCalls) != 1 {
		t.Errorf("ListDocumentsWithAnalysis calls = %d, want 1", len(dm.listAnalysisCalls))
	}
	if len(dm.listCalls) != 0 {
		t.Errorf("ListDocuments should not be called, got %d", len(dm.listCalls))
	}
	if !dm.listAnalysisCalls[0].IncludeAnalysis {
		t.Error("IncludeAnalysis should be true")
	}
}

func TestHandleList_Analysis_NullWhenNoAnalysis(t *testing.T) {
	dm := &mockDMClient{
		listAnalysisFn: func(_ context.Context, _ dmclient.ListDocumentsParams) (*dmclient.DocumentAnalysisList, error) {
			return aggList(stubDocWithAnalysis(testContractID, "PROCESSING_IN_PROGRESS", nil)), nil
		},
	}
	h := newAnalysisHandler(dm)

	rr := httptest.NewRecorder()
	r := withAuthContext(httptest.NewRequest(http.MethodGet, "/api/v1/contracts", nil))
	h.HandleList().ServeHTTP(rr, r)

	item := parseJSON(t, rr)["items"].([]any)[0].(map[string]any)
	for _, f := range []string{"contract_type", "risk_level", "risk_counts"} {
		if item[f] != nil {
			t.Errorf("%s should be null, got %v", f, item[f])
		}
	}
	if item["processing_status"] != "PROCESSING" {
		t.Errorf("processing_status = %v, want PROCESSING", item["processing_status"])
	}
}

func TestHandleList_Analysis_RiskNulledWhenNotReady(t *testing.T) {
	// Analysis carries risk, but the current version is not READY: the
	// orchestrator must null risk_level/risk_counts, keeping contract_type.
	analysis := &dmclient.DocumentAnalysisAggregate{
		ContractType: strPtr("SERVICES"),
		RiskLevel:    strPtr("medium"),
		RiskCounts:   &dmclient.RiskCounts{High: 0, Medium: 4, Low: 2},
	}
	dm := &mockDMClient{
		listAnalysisFn: func(_ context.Context, _ dmclient.ListDocumentsParams) (*dmclient.DocumentAnalysisList, error) {
			return aggList(stubDocWithAnalysis(testContractID, "ANALYSIS_IN_PROGRESS", analysis)), nil
		},
	}
	h := newAnalysisHandler(dm)

	rr := httptest.NewRecorder()
	r := withAuthContext(httptest.NewRequest(http.MethodGet, "/api/v1/contracts", nil))
	h.HandleList().ServeHTTP(rr, r)

	item := parseJSON(t, rr)["items"].([]any)[0].(map[string]any)
	if item["contract_type"] != "SERVICES" {
		t.Errorf("contract_type = %v, want SERVICES (known pre-READY)", item["contract_type"])
	}
	if item["risk_level"] != nil {
		t.Errorf("risk_level should be null when not READY, got %v", item["risk_level"])
	}
	if item["risk_counts"] != nil {
		t.Errorf("risk_counts should be null when not READY, got %v", item["risk_counts"])
	}
}

func TestHandleList_Analysis_NoCurrentVersion(t *testing.T) {
	analysis := &dmclient.DocumentAnalysisAggregate{ContractType: strPtr("OTHER")}
	dm := &mockDMClient{
		listAnalysisFn: func(_ context.Context, _ dmclient.ListDocumentsParams) (*dmclient.DocumentAnalysisList, error) {
			return aggList(stubDocWithAnalysis(testContractID, "", analysis)), nil
		},
	}
	h := newAnalysisHandler(dm)

	rr := httptest.NewRecorder()
	r := withAuthContext(httptest.NewRequest(http.MethodGet, "/api/v1/contracts", nil))
	h.HandleList().ServeHTTP(rr, r)

	item := parseJSON(t, rr)["items"].([]any)[0].(map[string]any)
	if item["current_version_number"] != nil {
		t.Errorf("current_version_number should be null, got %v", item["current_version_number"])
	}
	if item["processing_status"] != nil {
		t.Errorf("processing_status should be null, got %v", item["processing_status"])
	}
	if item["risk_level"] != nil {
		t.Errorf("risk_level should be null without current version, got %v", item["risk_level"])
	}
	if item["contract_type"] != "OTHER" {
		t.Errorf("contract_type = %v, want OTHER", item["contract_type"])
	}
}

func TestHandleList_Analysis_SingleBatchCall_NoNPlus1(t *testing.T) {
	items := make([]dmclient.DocumentWithAnalysis, 25)
	for i := range items {
		items[i] = stubDocWithAnalysis(testContractID, "FULLY_READY",
			&dmclient.DocumentAnalysisAggregate{RiskLevel: strPtr("low")})
	}
	dm := &mockDMClient{
		listAnalysisFn: func(_ context.Context, _ dmclient.ListDocumentsParams) (*dmclient.DocumentAnalysisList, error) {
			return &dmclient.DocumentAnalysisList{Items: items, Total: 25, Page: 1, Size: 100}, nil
		},
	}
	h := newAnalysisHandler(dm)

	rr := httptest.NewRecorder()
	r := withAuthContext(httptest.NewRequest(http.MethodGet, "/api/v1/contracts?size=100", nil))
	h.HandleList().ServeHTTP(rr, r)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	// Exactly one DM call regardless of 25 documents.
	if len(dm.listAnalysisCalls) != 1 {
		t.Errorf("expected 1 batch DM call, got %d", len(dm.listAnalysisCalls))
	}
	if len(dm.getCalls) != 0 {
		t.Errorf("expected 0 per-document GetDocument calls, got %d", len(dm.getCalls))
	}
}

func TestHandleList_Analysis_FiltersAndSortForwarded(t *testing.T) {
	dm := &mockDMClient{}
	h := newAnalysisHandler(dm)

	url := "/api/v1/contracts?risk_level=high&contract_type=LEASE&contract_type=SALE" +
		"&processing_status=ANALYZING&date_from=2026-01-01&date_to=2026-03-31&sort=risk&order=desc"
	rr := httptest.NewRecorder()
	r := withAuthContext(httptest.NewRequest(http.MethodGet, url, nil))
	h.HandleList().ServeHTTP(rr, r)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if len(dm.listAnalysisCalls) != 1 {
		t.Fatalf("expected 1 DM call, got %d", len(dm.listAnalysisCalls))
	}
	p := dm.listAnalysisCalls[0]
	if p.RiskLevel != "high" {
		t.Errorf("RiskLevel = %q, want high", p.RiskLevel)
	}
	if len(p.ContractTypes) != 2 || p.ContractTypes[0] != "LEASE" || p.ContractTypes[1] != "SALE" {
		t.Errorf("ContractTypes = %v", p.ContractTypes)
	}
	// ANALYZING expands to ARTIFACTS_READY + ANALYSIS_IN_PROGRESS.
	wantStatuses := map[string]bool{"ARTIFACTS_READY": false, "ANALYSIS_IN_PROGRESS": false}
	for _, s := range p.ArtifactStatuses {
		if _, ok := wantStatuses[s]; ok {
			wantStatuses[s] = true
		} else {
			t.Errorf("unexpected artifact_status %q", s)
		}
	}
	for s, seen := range wantStatuses {
		if !seen {
			t.Errorf("missing expanded artifact_status %q", s)
		}
	}
	if p.DateFrom != "2026-01-01" || p.DateTo != "2026-03-31" {
		t.Errorf("date range = %q..%q", p.DateFrom, p.DateTo)
	}
	if p.Sort != "risk" || p.Order != "desc" {
		t.Errorf("sort/order = %q/%q", p.Sort, p.Order)
	}
}

func TestHandleList_Analysis_TotalReflectsFiltered(t *testing.T) {
	dm := &mockDMClient{
		listAnalysisFn: func(_ context.Context, _ dmclient.ListDocumentsParams) (*dmclient.DocumentAnalysisList, error) {
			return &dmclient.DocumentAnalysisList{
				Items: []dmclient.DocumentWithAnalysis{stubDocWithAnalysis(testContractID, "FULLY_READY",
					&dmclient.DocumentAnalysisAggregate{RiskLevel: strPtr("high")})},
				Total: 7, Page: 1, Size: 20,
			}, nil
		},
	}
	h := newAnalysisHandler(dm)
	rr := httptest.NewRecorder()
	r := withAuthContext(httptest.NewRequest(http.MethodGet, "/api/v1/contracts?risk_level=high", nil))
	h.HandleList().ServeHTTP(rr, r)

	if got := parseJSON(t, rr)["total"].(float64); got != 7 {
		t.Errorf("total = %v, want 7 (DM-provided filtered count)", got)
	}
}

func TestHandleList_Analysis_InvalidParams(t *testing.T) {
	cases := []struct {
		name string
		url  string
	}{
		{"risk_level", "/api/v1/contracts?risk_level=critical"},
		{"contract_type", "/api/v1/contracts?contract_type=NOPE"},
		{"processing_status_unknown", "/api/v1/contracts?processing_status=BOGUS"},
		{"processing_status_awaiting", "/api/v1/contracts?processing_status=AWAITING_USER_INPUT"},
		{"sort", "/api/v1/contracts?sort=color"},
		{"order", "/api/v1/contracts?order=sideways"},
		{"date_from_bad", "/api/v1/contracts?date_from=not-a-date"},
		{"date_inverted", "/api/v1/contracts?date_from=2026-05-01&date_to=2026-01-01"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dm := &mockDMClient{}
			h := newAnalysisHandler(dm)
			rr := httptest.NewRecorder()
			r := withAuthContext(httptest.NewRequest(http.MethodGet, tc.url, nil))
			h.HandleList().ServeHTTP(rr, r)

			if rr.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d", rr.Code)
			}
			if parseJSON(t, rr)["error_code"] != "VALIDATION_ERROR" {
				t.Errorf("error_code = %v", parseJSON(t, rr)["error_code"])
			}
			if len(dm.listAnalysisCalls) != 0 {
				t.Error("DM should not be called on invalid params")
			}
		})
	}
}

func TestHandleList_Analysis_DateRangeSameDayMixedFormat(t *testing.T) {
	// date_from as RFC3339 later in the day + date_to as date-only of the same
	// calendar day must NOT be rejected (day-granularity comparison).
	dm := &mockDMClient{}
	h := newAnalysisHandler(dm)
	url := "/api/v1/contracts?date_from=2026-01-02T20:00:00Z&date_to=2026-01-02"
	rr := httptest.NewRecorder()
	r := withAuthContext(httptest.NewRequest(http.MethodGet, url, nil))
	h.HandleList().ServeHTTP(rr, r)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 for same-day mixed-format range, got %d", rr.Code)
	}
	if len(dm.listAnalysisCalls) != 1 {
		t.Errorf("expected DM call, got %d", len(dm.listAnalysisCalls))
	}
}

func TestHandleList_Analysis_DMError(t *testing.T) {
	dm := &mockDMClient{
		listAnalysisFn: func(_ context.Context, _ dmclient.ListDocumentsParams) (*dmclient.DocumentAnalysisList, error) {
			return nil, dmHTTPError("ListDocumentsWithAnalysis", 503, "unavailable")
		},
	}
	h := newAnalysisHandler(dm)
	rr := httptest.NewRecorder()
	r := withAuthContext(httptest.NewRequest(http.MethodGet, "/api/v1/contracts", nil))
	h.HandleList().ServeHTTP(rr, r)

	if rr.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d", rr.Code)
	}
}

// =========================================================================
// HandleList — feature flag OFF (fail-safe behavior)
// =========================================================================

func TestHandleList_FlagOff_PlainPath(t *testing.T) {
	dm := &mockDMClient{
		listFn: func(_ context.Context, _ dmclient.ListDocumentsParams) (*dmclient.DocumentList, error) {
			return stubDocumentList(2), nil
		},
	}
	h := newTestHandler(dm) // flag OFF
	rr := httptest.NewRecorder()
	r := withAuthContext(httptest.NewRequest(http.MethodGet, "/api/v1/contracts", nil))
	h.HandleList().ServeHTTP(rr, r)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if len(dm.listCalls) != 1 {
		t.Errorf("expected plain ListDocuments call, got %d", len(dm.listCalls))
	}
	if len(dm.listAnalysisCalls) != 0 {
		t.Errorf("ListDocumentsWithAnalysis must not be called when flag off, got %d", len(dm.listAnalysisCalls))
	}
	// New fields present but null in the plain path.
	item := parseJSON(t, rr)["items"].([]any)[0].(map[string]any)
	if _, ok := item["contract_type"]; !ok {
		t.Error("contract_type key should be present (null)")
	}
	if item["risk_level"] != nil {
		t.Errorf("risk_level should be null in plain path, got %v", item["risk_level"])
	}
}

func TestHandleList_FlagOff_RejectsAggParams(t *testing.T) {
	cases := []string{
		"/api/v1/contracts?risk_level=high",
		"/api/v1/contracts?contract_type=LEASE",
		"/api/v1/contracts?processing_status=READY",
		"/api/v1/contracts?sort=risk",
		"/api/v1/contracts?date_from=2026-01-01",
	}
	for _, url := range cases {
		t.Run(url, func(t *testing.T) {
			dm := &mockDMClient{}
			h := newTestHandler(dm) // flag OFF
			rr := httptest.NewRecorder()
			r := withAuthContext(httptest.NewRequest(http.MethodGet, url, nil))
			h.HandleList().ServeHTTP(rr, r)

			if rr.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d", rr.Code)
			}
			if parseJSON(t, rr)["error_code"] != "VALIDATION_ERROR" {
				t.Errorf("error_code = %v", parseJSON(t, rr)["error_code"])
			}
			if len(dm.listCalls) != 0 || len(dm.listAnalysisCalls) != 0 {
				t.Error("DM must not be called when rejecting unsupported params")
			}
		})
	}
}

// =========================================================================
// Backward-compat: archive/delete responses carry null aggregate fields
// =========================================================================

func TestHandleArchive_NewFieldsNull(t *testing.T) {
	dm := &mockDMClient{}
	h := newTestHandler(dm)
	rr := httptest.NewRecorder()
	r := withAuthContext(httptest.NewRequest(http.MethodPost, "/api/v1/contracts/"+testContractID+"/archive", nil))
	r = withChiParam(r, "contract_id", testContractID)
	h.HandleArchive().ServeHTTP(rr, r)

	body := parseJSON(t, rr)
	for _, f := range []string{"contract_type", "risk_level", "risk_counts"} {
		if v, ok := body[f]; !ok || v != nil {
			t.Errorf("%s should be present and null, got ok=%v v=%v", f, ok, v)
		}
	}
}

// =========================================================================
// Reverse processing-status map (ORCH-TASK-056)
// =========================================================================

func TestReverseProcessingStatusMap_RoundTrip(t *testing.T) {
	// Every artifact_status in the forward map must reverse-expand from its
	// user status; and every expanded artifact_status must forward-map back to
	// the same user status (no drift).
	for artifactStatus, userStatus := range processingStatusMap {
		expanded, ok := artifactStatusesForUserStatus(userStatus)
		if !ok {
			t.Errorf("user status %q not in reverse map", userStatus)
			continue
		}
		found := false
		for _, a := range expanded {
			if a == artifactStatus {
				found = true
			}
			if mapProcessingStatus(a) != userStatus {
				t.Errorf("artifact_status %q forward-maps to %q, want %q", a, mapProcessingStatus(a), userStatus)
			}
		}
		if !found {
			t.Errorf("artifact_status %q missing from reverse expansion of %q", artifactStatus, userStatus)
		}
	}
}

func TestReverseProcessingStatusMap_AwaitingUserInputUnsupported(t *testing.T) {
	if _, ok := artifactStatusesForUserStatus("AWAITING_USER_INPUT"); ok {
		t.Error("AWAITING_USER_INPUT must be unsupported for DM-side filtering")
	}
}

func TestMapDocumentWithAnalysis_Mapper(t *testing.T) {
	doc := stubDocWithAnalysis(testContractID, "FULLY_READY", &dmclient.DocumentAnalysisAggregate{
		ContractType: strPtr("SUPPLY"),
		RiskLevel:    strPtr("low"),
		RiskCounts:   &dmclient.RiskCounts{High: 1, Medium: 0, Low: 5},
	})
	cs := mapDocumentWithAnalysisToContractSummary(doc)
	if cs.ContractType == nil || *cs.ContractType != "SUPPLY" {
		t.Errorf("ContractType = %v", cs.ContractType)
	}
	if cs.RiskLevel == nil || *cs.RiskLevel != "low" {
		t.Errorf("RiskLevel = %v", cs.RiskLevel)
	}
	if cs.RiskCounts == nil || cs.RiskCounts.Low != 5 {
		t.Errorf("RiskCounts = %v", cs.RiskCounts)
	}
	if cs.ProcessingStatus == nil || *cs.ProcessingStatus != "READY" {
		t.Errorf("ProcessingStatus = %v", cs.ProcessingStatus)
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
