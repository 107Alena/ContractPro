package results

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"contractpro/api-orchestrator/internal/egress/dmclient"
	"contractpro/api-orchestrator/internal/infra/observability/logger"
	"contractpro/api-orchestrator/internal/ingress/middleware/auth"
)

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

type mockDMClient struct {
	getVerFn     func(ctx context.Context, documentID, versionID string) (*dmclient.DocumentVersionWithArtifacts, error)
	getArtFn     func(ctx context.Context, documentID, versionID, artifactType string) (*dmclient.ArtifactResponse, error)
	getVerCalls  []getVerCall
	getArtCalls  atomic.Int64 // atomic because called from concurrent goroutines
}

type getVerCall struct {
	DocumentID string
	VersionID  string
}

func (m *mockDMClient) GetVersion(ctx context.Context, documentID, versionID string) (*dmclient.DocumentVersionWithArtifacts, error) {
	m.getVerCalls = append(m.getVerCalls, getVerCall{DocumentID: documentID, VersionID: versionID})
	if m.getVerFn != nil {
		return m.getVerFn(ctx, documentID, versionID)
	}
	return stubVersionWithArtifacts("FULLY_READY"), nil
}

func (m *mockDMClient) GetArtifact(ctx context.Context, documentID, versionID, artifactType string) (*dmclient.ArtifactResponse, error) {
	m.getArtCalls.Add(1)
	if m.getArtFn != nil {
		return m.getArtFn(ctx, documentID, versionID, artifactType)
	}
	return &dmclient.ArtifactResponse{
		Content: json.RawMessage(`{"test":"` + artifactType + `"}`),
	}, nil
}

// ---------------------------------------------------------------------------
// Stubs
// ---------------------------------------------------------------------------

func stubVersionWithArtifacts(artifactStatus string) *dmclient.DocumentVersionWithArtifacts {
	return &dmclient.DocumentVersionWithArtifacts{
		DocumentVersion: dmclient.DocumentVersion{
			VersionID:      "ver-001",
			DocumentID:     "doc-001",
			VersionNumber:  1,
			ArtifactStatus: artifactStatus,
			OriginType:     "INITIAL_UPLOAD",
			SourceFileName: "contract.pdf",
			SourceFileSize: 1024,
			CreatedByUserID: "user-001",
			CreatedAt:      time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		},
		Artifacts: []dmclient.ArtifactDescriptor{},
	}
}

func newTestHandler(dm DMClient) *Handler {
	log := logger.NewLogger("error")
	return NewHandler(dm, log)
}

// makeRequest builds an http.Request with chi URL params and auth context.
func makeRequest(t *testing.T, method, path string, role auth.Role, contractID, versionID string) *http.Request {
	t.Helper()
	r := httptest.NewRequest(method, path, nil)

	// Set auth context.
	ctx := auth.WithAuthContext(r.Context(), auth.AuthContext{
		UserID:         "user-001",
		OrganizationID: "org-001",
		Role:           role,
		TokenID:        "token-001",
	})

	// Set chi URL params.
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("contract_id", contractID)
	rctx.URLParams.Add("version_id", versionID)
	ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)

	return r.WithContext(ctx)
}

// decodeJSON decodes the response body into a generic map.
func decodeJSON(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var result map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}
	return result
}

// ---------------------------------------------------------------------------
// HandleResults tests
// ---------------------------------------------------------------------------

func TestHandleResults_BUSINESS_USER_Returns403(t *testing.T) {
	dm := &mockDMClient{}
	h := newTestHandler(dm)

	r := makeRequest(t, http.MethodGet, "/results", auth.RoleBusinessUser,
		"11111111-1111-1111-1111-111111111111", "22222222-2222-2222-2222-222222222222")
	w := httptest.NewRecorder()

	h.HandleResults().ServeHTTP(w, r)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", w.Code)
	}
	body := decodeJSON(t, w)
	if body["error_code"] != "PERMISSION_DENIED" {
		t.Errorf("expected PERMISSION_DENIED, got %v", body["error_code"])
	}
}

func TestHandleResults_NoAuth_Returns401(t *testing.T) {
	dm := &mockDMClient{}
	h := newTestHandler(dm)

	// No auth context set.
	r := httptest.NewRequest(http.MethodGet, "/results", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("contract_id", "11111111-1111-1111-1111-111111111111")
	rctx.URLParams.Add("version_id", "22222222-2222-2222-2222-222222222222")
	r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	h.HandleResults().ServeHTTP(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestHandleResults_InvalidContractID_Returns400(t *testing.T) {
	dm := &mockDMClient{}
	h := newTestHandler(dm)

	r := makeRequest(t, http.MethodGet, "/results", auth.RoleLawyer,
		"not-a-uuid", "22222222-2222-2222-2222-222222222222")
	w := httptest.NewRecorder()

	h.HandleResults().ServeHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandleResults_InvalidVersionID_Returns400(t *testing.T) {
	dm := &mockDMClient{}
	h := newTestHandler(dm)

	r := makeRequest(t, http.MethodGet, "/results", auth.RoleLawyer,
		"11111111-1111-1111-1111-111111111111", "bad")
	w := httptest.NewRecorder()

	h.HandleResults().ServeHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandleResults_ProcessingInProgress_Returns200WithNulls(t *testing.T) {
	dm := &mockDMClient{
		getVerFn: func(_ context.Context, _, _ string) (*dmclient.DocumentVersionWithArtifacts, error) {
			return stubVersionWithArtifacts("PROCESSING_IN_PROGRESS"), nil
		},
	}
	h := newTestHandler(dm)

	r := makeRequest(t, http.MethodGet, "/results", auth.RoleLawyer,
		"11111111-1111-1111-1111-111111111111", "22222222-2222-2222-2222-222222222222")
	w := httptest.NewRecorder()

	h.HandleResults().ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	body := decodeJSON(t, w)
	if body["status"] != "PROCESSING" {
		t.Errorf("expected status PROCESSING, got %v", body["status"])
	}
	// Artifact fields should be null.
	if body["risks"] != nil {
		t.Errorf("expected risks to be null, got %v", body["risks"])
	}
	if body["summary"] != nil {
		t.Errorf("expected summary to be null, got %v", body["summary"])
	}

	// GetArtifact should NOT have been called.
	if dm.getArtCalls.Load() != 0 {
		t.Errorf("expected 0 GetArtifact calls, got %d", dm.getArtCalls.Load())
	}
}

func TestHandleResults_FullyReady_FetchesAllArtifacts(t *testing.T) {
	dm := &mockDMClient{}
	h := newTestHandler(dm)

	r := makeRequest(t, http.MethodGet, "/results", auth.RoleLawyer,
		"11111111-1111-1111-1111-111111111111", "22222222-2222-2222-2222-222222222222")
	w := httptest.NewRecorder()

	h.HandleResults().ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	body := decodeJSON(t, w)
	if body["status"] != "READY" {
		t.Errorf("expected status READY, got %v", body["status"])
	}
	if body["status_message"] != "Анализ завершён" {
		t.Errorf("expected status_message 'Анализ завершён', got %v", body["status_message"])
	}

	// All 7 artifact types should have been fetched.
	if dm.getArtCalls.Load() != 7 {
		t.Errorf("expected 7 GetArtifact calls, got %d", dm.getArtCalls.Load())
	}

	// Check that artifacts are present (non-null).
	for _, field := range []string{"risks", "risk_profile", "summary", "recommendations", "key_parameters", "contract_type", "aggregate_score"} {
		if body[field] == nil {
			t.Errorf("expected %s to be non-null", field)
		}
	}
}

func TestHandleResults_PartiallyAvailable_FetchesArtifacts(t *testing.T) {
	dm := &mockDMClient{
		getVerFn: func(_ context.Context, _, _ string) (*dmclient.DocumentVersionWithArtifacts, error) {
			return stubVersionWithArtifacts("PARTIALLY_AVAILABLE"), nil
		},
	}
	h := newTestHandler(dm)

	r := makeRequest(t, http.MethodGet, "/results", auth.RoleLawyer,
		"11111111-1111-1111-1111-111111111111", "22222222-2222-2222-2222-222222222222")
	w := httptest.NewRecorder()

	h.HandleResults().ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	body := decodeJSON(t, w)
	if body["status"] != "PARTIALLY_FAILED" {
		t.Errorf("expected status PARTIALLY_FAILED, got %v", body["status"])
	}
	// Artifacts should have been fetched.
	if dm.getArtCalls.Load() != 7 {
		t.Errorf("expected 7 GetArtifact calls, got %d", dm.getArtCalls.Load())
	}
}

func TestHandleResults_Artifact404_ReturnsNullForThat(t *testing.T) {
	dm := &mockDMClient{
		getArtFn: func(_ context.Context, _, _, artifactType string) (*dmclient.ArtifactResponse, error) {
			if artifactType == ArtifactRiskAnalysis {
				return nil, &dmclient.DMError{
					Operation:  "GetArtifact",
					StatusCode: http.StatusNotFound,
					Body:       []byte(`{"code":"NOT_FOUND"}`),
				}
			}
			return &dmclient.ArtifactResponse{
				Content: json.RawMessage(`{"data":"ok"}`),
			}, nil
		},
	}
	h := newTestHandler(dm)

	r := makeRequest(t, http.MethodGet, "/results", auth.RoleLawyer,
		"11111111-1111-1111-1111-111111111111", "22222222-2222-2222-2222-222222222222")
	w := httptest.NewRecorder()

	h.HandleResults().ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	body := decodeJSON(t, w)
	// risks (RISK_ANALYSIS) should be null due to 404.
	if body["risks"] != nil {
		t.Errorf("expected risks to be null (404 from DM), got %v", body["risks"])
	}
	// Other artifacts should be present.
	if body["summary"] == nil {
		t.Errorf("expected summary to be non-null")
	}
}

func TestHandleResults_ArtifactTransportError_ReturnsNullGracefully(t *testing.T) {
	dm := &mockDMClient{
		getArtFn: func(_ context.Context, _, _, artifactType string) (*dmclient.ArtifactResponse, error) {
			if artifactType == ArtifactSummary {
				return nil, &dmclient.DMError{
					Operation: "GetArtifact",
					Retryable: true,
					Cause:     fmt.Errorf("connection refused"),
				}
			}
			return &dmclient.ArtifactResponse{
				Content: json.RawMessage(`{"data":"ok"}`),
			}, nil
		},
	}
	h := newTestHandler(dm)

	r := makeRequest(t, http.MethodGet, "/results", auth.RoleLawyer,
		"11111111-1111-1111-1111-111111111111", "22222222-2222-2222-2222-222222222222")
	w := httptest.NewRecorder()

	h.HandleResults().ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	body := decodeJSON(t, w)
	// summary should be null due to transport error.
	if body["summary"] != nil {
		t.Errorf("expected summary to be null (transport error), got %v", body["summary"])
	}
	// Other artifacts should be present.
	if body["risks"] == nil {
		t.Errorf("expected risks to be non-null")
	}
}

func TestHandleResults_GetVersionError_Returns502(t *testing.T) {
	dm := &mockDMClient{
		getVerFn: func(_ context.Context, _, _ string) (*dmclient.DocumentVersionWithArtifacts, error) {
			return nil, dmclient.ErrCircuitOpen
		},
	}
	h := newTestHandler(dm)

	r := makeRequest(t, http.MethodGet, "/results", auth.RoleLawyer,
		"11111111-1111-1111-1111-111111111111", "22222222-2222-2222-2222-222222222222")
	w := httptest.NewRecorder()

	h.HandleResults().ServeHTTP(w, r)

	if w.Code != http.StatusBadGateway {
		t.Errorf("expected 502, got %d", w.Code)
	}
}

func TestHandleResults_GetVersionDM404_Returns404(t *testing.T) {
	dm := &mockDMClient{
		getVerFn: func(_ context.Context, _, _ string) (*dmclient.DocumentVersionWithArtifacts, error) {
			return nil, &dmclient.DMError{
				Operation:  "GetVersion",
				StatusCode: http.StatusNotFound,
				Body:       []byte(`{"code":"NOT_FOUND"}`),
			}
		},
	}
	h := newTestHandler(dm)

	r := makeRequest(t, http.MethodGet, "/results", auth.RoleLawyer,
		"11111111-1111-1111-1111-111111111111", "22222222-2222-2222-2222-222222222222")
	w := httptest.NewRecorder()

	h.HandleResults().ServeHTTP(w, r)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

func TestHandleResults_OrgAdmin_Allowed(t *testing.T) {
	dm := &mockDMClient{}
	h := newTestHandler(dm)

	r := makeRequest(t, http.MethodGet, "/results", auth.RoleOrgAdmin,
		"11111111-1111-1111-1111-111111111111", "22222222-2222-2222-2222-222222222222")
	w := httptest.NewRecorder()

	h.HandleResults().ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

// ---------------------------------------------------------------------------
// HandleRisks tests
// ---------------------------------------------------------------------------

func TestHandleRisks_BUSINESS_USER_Returns403(t *testing.T) {
	dm := &mockDMClient{}
	h := newTestHandler(dm)

	r := makeRequest(t, http.MethodGet, "/risks", auth.RoleBusinessUser,
		"11111111-1111-1111-1111-111111111111", "22222222-2222-2222-2222-222222222222")
	w := httptest.NewRecorder()

	h.HandleRisks().ServeHTTP(w, r)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", w.Code)
	}
}

func TestHandleRisks_FullyReady_Fetches2Artifacts(t *testing.T) {
	dm := &mockDMClient{}
	h := newTestHandler(dm)

	r := makeRequest(t, http.MethodGet, "/risks", auth.RoleLawyer,
		"11111111-1111-1111-1111-111111111111", "22222222-2222-2222-2222-222222222222")
	w := httptest.NewRecorder()

	h.HandleRisks().ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	body := decodeJSON(t, w)
	if body["status"] != "READY" {
		t.Errorf("expected status READY, got %v", body["status"])
	}
	if dm.getArtCalls.Load() != 2 {
		t.Errorf("expected 2 GetArtifact calls, got %d", dm.getArtCalls.Load())
	}
	if body["risks"] == nil {
		t.Errorf("expected risks to be non-null")
	}
	if body["risk_profile"] == nil {
		t.Errorf("expected risk_profile to be non-null")
	}
}

func TestHandleRisks_ProcessingInProgress_Returns200WithNulls(t *testing.T) {
	dm := &mockDMClient{
		getVerFn: func(_ context.Context, _, _ string) (*dmclient.DocumentVersionWithArtifacts, error) {
			return stubVersionWithArtifacts("ANALYSIS_IN_PROGRESS"), nil
		},
	}
	h := newTestHandler(dm)

	r := makeRequest(t, http.MethodGet, "/risks", auth.RoleLawyer,
		"11111111-1111-1111-1111-111111111111", "22222222-2222-2222-2222-222222222222")
	w := httptest.NewRecorder()

	h.HandleRisks().ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	body := decodeJSON(t, w)
	if body["status"] != "ANALYZING" {
		t.Errorf("expected status ANALYZING, got %v", body["status"])
	}
	if body["risks"] != nil {
		t.Errorf("expected risks to be null")
	}
	if dm.getArtCalls.Load() != 0 {
		t.Errorf("expected 0 GetArtifact calls, got %d", dm.getArtCalls.Load())
	}
}

// ---------------------------------------------------------------------------
// HandleSummary tests
// ---------------------------------------------------------------------------

func TestHandleSummary_AllRolesAllowed(t *testing.T) {
	for _, role := range []auth.Role{auth.RoleLawyer, auth.RoleBusinessUser, auth.RoleOrgAdmin} {
		t.Run(string(role), func(t *testing.T) {
			dm := &mockDMClient{}
			h := newTestHandler(dm)

			r := makeRequest(t, http.MethodGet, "/summary", role,
				"11111111-1111-1111-1111-111111111111", "22222222-2222-2222-2222-222222222222")
			w := httptest.NewRecorder()

			h.HandleSummary().ServeHTTP(w, r)

			if w.Code != http.StatusOK {
				t.Errorf("expected 200 for role %s, got %d", role, w.Code)
			}
		})
	}
}

func TestHandleSummary_FullyReady_Fetches3Artifacts(t *testing.T) {
	dm := &mockDMClient{}
	h := newTestHandler(dm)

	r := makeRequest(t, http.MethodGet, "/summary", auth.RoleLawyer,
		"11111111-1111-1111-1111-111111111111", "22222222-2222-2222-2222-222222222222")
	w := httptest.NewRecorder()

	h.HandleSummary().ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	body := decodeJSON(t, w)
	if dm.getArtCalls.Load() != 3 {
		t.Errorf("expected 3 GetArtifact calls, got %d", dm.getArtCalls.Load())
	}
	if body["summary"] == nil {
		t.Errorf("expected summary to be non-null")
	}
	if body["aggregate_score"] == nil {
		t.Errorf("expected aggregate_score to be non-null")
	}
	if body["key_parameters"] == nil {
		t.Errorf("expected key_parameters to be non-null")
	}
}

func TestHandleSummary_PendingProcessing_Returns200WithNulls(t *testing.T) {
	dm := &mockDMClient{
		getVerFn: func(_ context.Context, _, _ string) (*dmclient.DocumentVersionWithArtifacts, error) {
			return stubVersionWithArtifacts("PENDING_PROCESSING"), nil
		},
	}
	h := newTestHandler(dm)

	r := makeRequest(t, http.MethodGet, "/summary", auth.RoleBusinessUser,
		"11111111-1111-1111-1111-111111111111", "22222222-2222-2222-2222-222222222222")
	w := httptest.NewRecorder()

	h.HandleSummary().ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	body := decodeJSON(t, w)
	if body["status"] != "QUEUED" {
		t.Errorf("expected status QUEUED, got %v", body["status"])
	}
	if body["summary"] != nil {
		t.Errorf("expected summary to be null")
	}
}

// ---------------------------------------------------------------------------
// HandleRecommendations tests
// ---------------------------------------------------------------------------

func TestHandleRecommendations_BUSINESS_USER_Returns403(t *testing.T) {
	dm := &mockDMClient{}
	h := newTestHandler(dm)

	r := makeRequest(t, http.MethodGet, "/recommendations", auth.RoleBusinessUser,
		"11111111-1111-1111-1111-111111111111", "22222222-2222-2222-2222-222222222222")
	w := httptest.NewRecorder()

	h.HandleRecommendations().ServeHTTP(w, r)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", w.Code)
	}
}

func TestHandleRecommendations_FullyReady_Fetches1Artifact(t *testing.T) {
	dm := &mockDMClient{}
	h := newTestHandler(dm)

	r := makeRequest(t, http.MethodGet, "/recommendations", auth.RoleLawyer,
		"11111111-1111-1111-1111-111111111111", "22222222-2222-2222-2222-222222222222")
	w := httptest.NewRecorder()

	h.HandleRecommendations().ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	body := decodeJSON(t, w)
	if dm.getArtCalls.Load() != 1 {
		t.Errorf("expected 1 GetArtifact call, got %d", dm.getArtCalls.Load())
	}
	if body["recommendations"] == nil {
		t.Errorf("expected recommendations to be non-null")
	}
}

func TestHandleRecommendations_ProcessingFailed_Returns200WithNulls(t *testing.T) {
	dm := &mockDMClient{
		getVerFn: func(_ context.Context, _, _ string) (*dmclient.DocumentVersionWithArtifacts, error) {
			return stubVersionWithArtifacts("PROCESSING_FAILED"), nil
		},
	}
	h := newTestHandler(dm)

	r := makeRequest(t, http.MethodGet, "/recommendations", auth.RoleLawyer,
		"11111111-1111-1111-1111-111111111111", "22222222-2222-2222-2222-222222222222")
	w := httptest.NewRecorder()

	h.HandleRecommendations().ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	body := decodeJSON(t, w)
	if body["status"] != "FAILED" {
		t.Errorf("expected status FAILED, got %v", body["status"])
	}
	if body["recommendations"] != nil {
		t.Errorf("expected recommendations to be null")
	}
}

// ---------------------------------------------------------------------------
// Edge case: redirect response from DM (N-2)
// ---------------------------------------------------------------------------

func TestHandleResults_ArtifactRedirectResponse_ReturnsNull(t *testing.T) {
	dm := &mockDMClient{
		getArtFn: func(_ context.Context, _, _, _ string) (*dmclient.ArtifactResponse, error) {
			return &dmclient.ArtifactResponse{RedirectURL: "https://s3.example.com/presigned"}, nil
		},
	}
	h := newTestHandler(dm)

	r := makeRequest(t, http.MethodGet, "/results", auth.RoleLawyer,
		"11111111-1111-1111-1111-111111111111", "22222222-2222-2222-2222-222222222222")
	w := httptest.NewRecorder()

	h.HandleResults().ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	body := decodeJSON(t, w)
	// All artifact fields should be null because redirect is unexpected for analysis artifacts.
	for _, field := range []string{"risks", "risk_profile", "summary", "recommendations", "key_parameters", "contract_type", "aggregate_score"} {
		if body[field] != nil {
			t.Errorf("expected %s to be null (redirect response), got %v", field, body[field])
		}
	}
}

// ---------------------------------------------------------------------------
// Edge case: context cancellation during parallel fetch (S-4)
// ---------------------------------------------------------------------------

func TestHandleResults_ContextCancelled_ReturnsGracefully(t *testing.T) {
	dm := &mockDMClient{
		getArtFn: func(ctx context.Context, _, _, _ string) (*dmclient.ArtifactResponse, error) {
			return nil, ctx.Err()
		},
	}
	h := newTestHandler(dm)

	r := makeRequest(t, http.MethodGet, "/results", auth.RoleLawyer,
		"11111111-1111-1111-1111-111111111111", "22222222-2222-2222-2222-222222222222")

	// Cancel the context before artifact fetching.
	ctx, cancel := context.WithCancel(r.Context())
	cancel()
	r = r.WithContext(ctx)

	w := httptest.NewRecorder()

	h.HandleResults().ServeHTTP(w, r)

	// GetVersion will fail with context error → DM error handling.
	// The handler should not hang.
	if w.Code == 0 {
		t.Fatal("expected a response, got none")
	}
}

// ---------------------------------------------------------------------------
// Status mapping unit tests
// ---------------------------------------------------------------------------

func TestMapProcessingStatus(t *testing.T) {
	tests := []struct {
		input    string
		expected string
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
		{"UNKNOWN_STATUS", "UNKNOWN"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := mapProcessingStatus(tt.input)
			if got != tt.expected {
				t.Errorf("mapProcessingStatus(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestIsDataAvailable(t *testing.T) {
	available := []string{"FULLY_READY", "PARTIALLY_AVAILABLE"}
	for _, s := range available {
		if !isDataAvailable(s) {
			t.Errorf("expected isDataAvailable(%q) = true", s)
		}
	}

	notAvailable := []string{"PENDING_UPLOAD", "PENDING_PROCESSING", "PROCESSING_IN_PROGRESS",
		"ARTIFACTS_READY", "ANALYSIS_IN_PROGRESS", "ANALYSIS_READY", "REPORTS_IN_PROGRESS",
		"PROCESSING_FAILED", "REJECTED"}
	for _, s := range notAvailable {
		if isDataAvailable(s) {
			t.Errorf("expected isDataAvailable(%q) = false", s)
		}
	}
}

func TestMapProcessingStatusMessage(t *testing.T) {
	// Known statuses return Russian messages.
	if msg := mapProcessingStatusMessage("READY"); msg != "Анализ завершён" {
		t.Errorf("expected 'Анализ завершён', got %q", msg)
	}
	// Unknown statuses return the fallback.
	if msg := mapProcessingStatusMessage("UNKNOWN"); msg != "Статус неизвестен" {
		t.Errorf("expected 'Статус неизвестен', got %q", msg)
	}
	if msg := mapProcessingStatusMessage("TOTALLY_BOGUS"); msg != "Статус неизвестен" {
		t.Errorf("expected 'Статус неизвестен' for unknown, got %q", msg)
	}
}

func TestIsDMNotFound(t *testing.T) {
	if !isDMNotFound(&dmclient.DMError{StatusCode: 404}) {
		t.Error("expected true for 404 DMError")
	}
	if isDMNotFound(&dmclient.DMError{StatusCode: 500}) {
		t.Error("expected false for 500 DMError")
	}
	if isDMNotFound(fmt.Errorf("some other error")) {
		t.Error("expected false for non-DMError")
	}
}

// ---------------------------------------------------------------------------
// All statuses early-return test (no artifact fetching)
// ---------------------------------------------------------------------------

func TestHandleResults_AllNonDataStatuses_SkipArtifactFetch(t *testing.T) {
	nonDataStatuses := []string{
		"PENDING_UPLOAD", "PENDING_PROCESSING", "PROCESSING_IN_PROGRESS",
		"ARTIFACTS_READY", "ANALYSIS_IN_PROGRESS", "ANALYSIS_READY",
		"REPORTS_IN_PROGRESS", "PROCESSING_FAILED", "REJECTED",
	}

	for _, dmStatus := range nonDataStatuses {
		t.Run(dmStatus, func(t *testing.T) {
			dm := &mockDMClient{
				getVerFn: func(_ context.Context, _, _ string) (*dmclient.DocumentVersionWithArtifacts, error) {
					return stubVersionWithArtifacts(dmStatus), nil
				},
			}
			h := newTestHandler(dm)

			r := makeRequest(t, http.MethodGet, "/results", auth.RoleLawyer,
				"11111111-1111-1111-1111-111111111111", "22222222-2222-2222-2222-222222222222")
			w := httptest.NewRecorder()

			h.HandleResults().ServeHTTP(w, r)

			if w.Code != http.StatusOK {
				t.Errorf("expected 200 for status %s, got %d", dmStatus, w.Code)
			}
			if dm.getArtCalls.Load() != 0 {
				t.Errorf("expected 0 GetArtifact calls for status %s, got %d", dmStatus, dm.getArtCalls.Load())
			}
		})
	}
}
