package export

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/go-chi/chi/v5"

	"contractpro/api-orchestrator/internal/egress/dmclient"
	"contractpro/api-orchestrator/internal/infra/observability/logger"
	"contractpro/api-orchestrator/internal/ingress/middleware/auth"
)

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

type mockDMClient struct {
	mu          sync.Mutex
	getArtFn    func(ctx context.Context, documentID, versionID, artifactType string) (*dmclient.ArtifactResponse, error)
	getArtCalls []getArtCall
}

type getArtCall struct {
	DocumentID   string
	VersionID    string
	ArtifactType string
}

func (m *mockDMClient) GetArtifact(ctx context.Context, documentID, versionID, artifactType string) (*dmclient.ArtifactResponse, error) {
	m.mu.Lock()
	m.getArtCalls = append(m.getArtCalls, getArtCall{
		DocumentID:   documentID,
		VersionID:    versionID,
		ArtifactType: artifactType,
	})
	m.mu.Unlock()
	if m.getArtFn != nil {
		return m.getArtFn(ctx, documentID, versionID, artifactType)
	}
	return &dmclient.ArtifactResponse{
		RedirectURL: "https://s3.example.com/presigned-url?token=abc",
	}, nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func newTestHandler(dm DMClient) *Handler {
	log := logger.NewLogger("error")
	return NewHandler(dm, log)
}

func makeRequest(t *testing.T, contractID, versionID, format string) *http.Request {
	t.Helper()
	path := fmt.Sprintf("/api/v1/contracts/%s/versions/%s/export/%s", contractID, versionID, format)
	r := httptest.NewRequest(http.MethodGet, path, nil)

	// Set auth context.
	ctx := auth.WithAuthContext(r.Context(), auth.AuthContext{
		UserID:         "user-001",
		OrganizationID: "org-001",
		Role:           auth.RoleLawyer,
		TokenID:        "token-001",
	})

	// Set chi URL params.
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("contract_id", contractID)
	rctx.URLParams.Add("version_id", versionID)
	rctx.URLParams.Add("format", format)
	ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)

	return r.WithContext(ctx)
}

func makeRequestWithRole(t *testing.T, contractID, versionID, format string, role auth.Role) *http.Request {
	t.Helper()
	path := fmt.Sprintf("/api/v1/contracts/%s/versions/%s/export/%s", contractID, versionID, format)
	r := httptest.NewRequest(http.MethodGet, path, nil)

	ctx := auth.WithAuthContext(r.Context(), auth.AuthContext{
		UserID:         "user-001",
		OrganizationID: "org-001",
		Role:           role,
		TokenID:        "token-001",
	})

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("contract_id", contractID)
	rctx.URLParams.Add("version_id", versionID)
	rctx.URLParams.Add("format", format)
	ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)

	return r.WithContext(ctx)
}

func makeRequestNoAuth(t *testing.T, contractID, versionID, format string) *http.Request {
	t.Helper()
	path := fmt.Sprintf("/api/v1/contracts/%s/versions/%s/export/%s", contractID, versionID, format)
	r := httptest.NewRequest(http.MethodGet, path, nil)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("contract_id", contractID)
	rctx.URLParams.Add("version_id", versionID)
	rctx.URLParams.Add("format", format)
	ctx := context.WithValue(r.Context(), chi.RouteCtxKey, rctx)

	return r.WithContext(ctx)
}

func decodeJSON(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var result map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}
	return result
}

// ---------------------------------------------------------------------------
// Test constants
// ---------------------------------------------------------------------------

const (
	validContractID = "a1b2c3d4-e5f6-7890-abcd-ef1234567890"
	validVersionID  = "b2c3d4e5-f6a7-8901-bcde-f12345678901"
)

// ---------------------------------------------------------------------------
// HandleExport — happy path
// ---------------------------------------------------------------------------

func TestHandleExport_PDF_Returns302(t *testing.T) {
	dm := &mockDMClient{}
	h := newTestHandler(dm)

	w := httptest.NewRecorder()
	r := makeRequest(t, validContractID, validVersionID, "pdf")

	h.HandleExport().ServeHTTP(w, r)

	if w.Code != http.StatusFound {
		t.Errorf("expected 302, got %d", w.Code)
	}
	loc := w.Header().Get("Location")
	if loc != "https://s3.example.com/presigned-url?token=abc" {
		t.Errorf("unexpected Location header: %s", loc)
	}

	// Verify DM was called with correct artifact type.
	dm.mu.Lock()
	calls := dm.getArtCalls
	dm.mu.Unlock()
	if len(calls) != 1 {
		t.Fatalf("expected 1 DM call, got %d", len(calls))
	}
	call := calls[0]
	if call.DocumentID != validContractID {
		t.Errorf("expected documentID=%s, got %s", validContractID, call.DocumentID)
	}
	if call.VersionID != validVersionID {
		t.Errorf("expected versionID=%s, got %s", validVersionID, call.VersionID)
	}
	if call.ArtifactType != "EXPORT_PDF" {
		t.Errorf("expected artifactType=EXPORT_PDF, got %s", call.ArtifactType)
	}
}

func TestHandleExport_DOCX_Returns302(t *testing.T) {
	dm := &mockDMClient{
		getArtFn: func(_ context.Context, _, _, _ string) (*dmclient.ArtifactResponse, error) {
			return &dmclient.ArtifactResponse{
				RedirectURL: "https://s3.example.com/docx-presigned",
			}, nil
		},
	}
	h := newTestHandler(dm)

	w := httptest.NewRecorder()
	r := makeRequest(t, validContractID, validVersionID, "docx")

	h.HandleExport().ServeHTTP(w, r)

	if w.Code != http.StatusFound {
		t.Errorf("expected 302, got %d", w.Code)
	}
	if loc := w.Header().Get("Location"); loc != "https://s3.example.com/docx-presigned" {
		t.Errorf("unexpected Location: %s", loc)
	}

	// Verify DM was called with EXPORT_DOCX.
	dm.mu.Lock()
	calls := dm.getArtCalls
	dm.mu.Unlock()
	if len(calls) != 1 {
		t.Fatalf("expected 1 DM call, got %d", len(calls))
	}
	if calls[0].ArtifactType != "EXPORT_DOCX" {
		t.Errorf("expected EXPORT_DOCX, got %s", calls[0].ArtifactType)
	}
}

func TestHandleExport_CorrelationID_InResponse(t *testing.T) {
	dm := &mockDMClient{}
	h := newTestHandler(dm)

	w := httptest.NewRecorder()
	r := makeRequest(t, validContractID, validVersionID, "pdf")

	// Inject a known correlation ID via logger.RequestContext.
	ctx := logger.WithRequestContext(r.Context(), logger.RequestContext{
		CorrelationID:  "test-corr-id",
		OrganizationID: "org-001",
		UserID:         "user-001",
	})
	r = r.WithContext(ctx)

	h.HandleExport().ServeHTTP(w, r)

	if w.Code != http.StatusFound {
		t.Errorf("expected 302, got %d", w.Code)
	}
	if cid := w.Header().Get("X-Correlation-Id"); cid != "test-corr-id" {
		t.Errorf("expected X-Correlation-Id=test-corr-id, got %s", cid)
	}
}

// ---------------------------------------------------------------------------
// HandleExport — defense-in-depth auth checks
// ---------------------------------------------------------------------------

func TestHandleExport_NoAuthContext_Returns401(t *testing.T) {
	dm := &mockDMClient{}
	h := newTestHandler(dm)

	w := httptest.NewRecorder()
	r := makeRequestNoAuth(t, validContractID, validVersionID, "pdf")

	h.HandleExport().ServeHTTP(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
	body := decodeJSON(t, w)
	if body["error_code"] != "AUTH_TOKEN_MISSING" {
		t.Errorf("expected AUTH_TOKEN_MISSING, got %s", body["error_code"])
	}
}

func TestHandleExport_BusinessUser_Returns403(t *testing.T) {
	dm := &mockDMClient{}
	h := newTestHandler(dm)

	w := httptest.NewRecorder()
	r := makeRequestWithRole(t, validContractID, validVersionID, "pdf", auth.RoleBusinessUser)

	h.HandleExport().ServeHTTP(w, r)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", w.Code)
	}
	body := decodeJSON(t, w)
	if body["error_code"] != "PERMISSION_DENIED" {
		t.Errorf("expected PERMISSION_DENIED, got %s", body["error_code"])
	}
}

// ---------------------------------------------------------------------------
// HandleExport — format validation
// ---------------------------------------------------------------------------

func TestHandleExport_InvalidFormat_Returns400(t *testing.T) {
	formats := []string{"xml", "txt", "xlsx", "PDF", "DOCX", "", "html"}
	dm := &mockDMClient{}
	h := newTestHandler(dm)

	for _, format := range formats {
		t.Run("format="+format, func(t *testing.T) {
			w := httptest.NewRecorder()
			r := makeRequest(t, validContractID, validVersionID, format)

			h.HandleExport().ServeHTTP(w, r)

			if w.Code != http.StatusBadRequest {
				t.Errorf("expected 400 for format=%q, got %d", format, w.Code)
			}
			body := decodeJSON(t, w)
			if body["error_code"] != "VALIDATION_ERROR" {
				t.Errorf("expected VALIDATION_ERROR, got %s", body["error_code"])
			}
		})
	}

	// No DM calls for invalid format.
	dm.mu.Lock()
	calls := len(dm.getArtCalls)
	dm.mu.Unlock()
	if calls != 0 {
		t.Errorf("expected 0 DM calls for invalid formats, got %d", calls)
	}
}

// ---------------------------------------------------------------------------
// HandleExport — UUID validation
// ---------------------------------------------------------------------------

func TestHandleExport_InvalidContractID_Returns400(t *testing.T) {
	dm := &mockDMClient{}
	h := newTestHandler(dm)

	w := httptest.NewRecorder()
	r := makeRequest(t, "not-a-uuid", validVersionID, "pdf")

	h.HandleExport().ServeHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
	body := decodeJSON(t, w)
	if body["error_code"] != "VALIDATION_ERROR" {
		t.Errorf("expected VALIDATION_ERROR, got %s", body["error_code"])
	}
}

func TestHandleExport_InvalidVersionID_Returns400(t *testing.T) {
	dm := &mockDMClient{}
	h := newTestHandler(dm)

	w := httptest.NewRecorder()
	r := makeRequest(t, validContractID, "not-a-uuid", "pdf")

	h.HandleExport().ServeHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
	body := decodeJSON(t, w)
	if body["error_code"] != "VALIDATION_ERROR" {
		t.Errorf("expected VALIDATION_ERROR, got %s", body["error_code"])
	}
}

func TestHandleExport_EmptyContractID_Returns400(t *testing.T) {
	dm := &mockDMClient{}
	h := newTestHandler(dm)

	w := httptest.NewRecorder()
	r := makeRequest(t, "", validVersionID, "pdf")

	h.HandleExport().ServeHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandleExport_EmptyVersionID_Returns400(t *testing.T) {
	dm := &mockDMClient{}
	h := newTestHandler(dm)

	w := httptest.NewRecorder()
	r := makeRequest(t, validContractID, "", "pdf")

	h.HandleExport().ServeHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

// ---------------------------------------------------------------------------
// HandleExport — DM errors
// ---------------------------------------------------------------------------

func TestHandleExport_DM404_Returns404(t *testing.T) {
	dm := &mockDMClient{
		getArtFn: func(_ context.Context, _, _, _ string) (*dmclient.ArtifactResponse, error) {
			return nil, &dmclient.DMError{
				Operation:  "GetArtifact",
				StatusCode: 404,
				Body:       []byte(`{"error":"not found"}`),
			}
		},
	}
	h := newTestHandler(dm)

	w := httptest.NewRecorder()
	r := makeRequest(t, validContractID, validVersionID, "pdf")

	h.HandleExport().ServeHTTP(w, r)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
	body := decodeJSON(t, w)
	if body["error_code"] != "ARTIFACT_NOT_FOUND" {
		t.Errorf("expected ARTIFACT_NOT_FOUND, got %s", body["error_code"])
	}
}

func TestHandleExport_DM5xx_Returns502(t *testing.T) {
	dm := &mockDMClient{
		getArtFn: func(_ context.Context, _, _, _ string) (*dmclient.ArtifactResponse, error) {
			return nil, &dmclient.DMError{
				Operation:  "GetArtifact",
				StatusCode: 500,
				Body:       []byte(`internal error`),
				Retryable:  true,
			}
		},
	}
	h := newTestHandler(dm)

	w := httptest.NewRecorder()
	r := makeRequest(t, validContractID, validVersionID, "pdf")

	h.HandleExport().ServeHTTP(w, r)

	if w.Code != http.StatusBadGateway {
		t.Errorf("expected 502, got %d", w.Code)
	}
	body := decodeJSON(t, w)
	if body["error_code"] != "DM_UNAVAILABLE" {
		t.Errorf("expected DM_UNAVAILABLE, got %s", body["error_code"])
	}
}

func TestHandleExport_CircuitOpen_Returns502(t *testing.T) {
	dm := &mockDMClient{
		getArtFn: func(_ context.Context, _, _, _ string) (*dmclient.ArtifactResponse, error) {
			return nil, dmclient.ErrCircuitOpen
		},
	}
	h := newTestHandler(dm)

	w := httptest.NewRecorder()
	r := makeRequest(t, validContractID, validVersionID, "pdf")

	h.HandleExport().ServeHTTP(w, r)

	if w.Code != http.StatusBadGateway {
		t.Errorf("expected 502, got %d", w.Code)
	}
	body := decodeJSON(t, w)
	if body["error_code"] != "DM_UNAVAILABLE" {
		t.Errorf("expected DM_UNAVAILABLE, got %s", body["error_code"])
	}
}

func TestHandleExport_TransportError_Returns502(t *testing.T) {
	dm := &mockDMClient{
		getArtFn: func(_ context.Context, _, _, _ string) (*dmclient.ArtifactResponse, error) {
			return nil, &dmclient.DMError{
				Operation: "GetArtifact",
				Retryable: true,
				Cause:     errors.New("connection refused"),
			}
		},
	}
	h := newTestHandler(dm)

	w := httptest.NewRecorder()
	r := makeRequest(t, validContractID, validVersionID, "pdf")

	h.HandleExport().ServeHTTP(w, r)

	if w.Code != http.StatusBadGateway {
		t.Errorf("expected 502, got %d", w.Code)
	}
	body := decodeJSON(t, w)
	if body["error_code"] != "DM_UNAVAILABLE" {
		t.Errorf("expected DM_UNAVAILABLE, got %s", body["error_code"])
	}
}

func TestHandleExport_UnknownError_Returns500(t *testing.T) {
	dm := &mockDMClient{
		getArtFn: func(_ context.Context, _, _, _ string) (*dmclient.ArtifactResponse, error) {
			return nil, errors.New("something unexpected")
		},
	}
	h := newTestHandler(dm)

	w := httptest.NewRecorder()
	r := makeRequest(t, validContractID, validVersionID, "pdf")

	h.HandleExport().ServeHTTP(w, r)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", w.Code)
	}
	body := decodeJSON(t, w)
	if body["error_code"] != "INTERNAL_ERROR" {
		t.Errorf("expected INTERNAL_ERROR, got %s", body["error_code"])
	}
}

func TestHandleExport_DM400_Returns500(t *testing.T) {
	dm := &mockDMClient{
		getArtFn: func(_ context.Context, _, _, _ string) (*dmclient.ArtifactResponse, error) {
			return nil, &dmclient.DMError{
				Operation:  "GetArtifact",
				StatusCode: 400,
				Body:       []byte(`bad request`),
			}
		},
	}
	h := newTestHandler(dm)

	w := httptest.NewRecorder()
	r := makeRequest(t, validContractID, validVersionID, "pdf")

	h.HandleExport().ServeHTTP(w, r)

	// DM 400 maps to INTERNAL_ERROR (unexpected DM response).
	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", w.Code)
	}
}

func TestHandleExport_DM403_Returns500(t *testing.T) {
	dm := &mockDMClient{
		getArtFn: func(_ context.Context, _, _, _ string) (*dmclient.ArtifactResponse, error) {
			return nil, &dmclient.DMError{
				Operation:  "GetArtifact",
				StatusCode: 403,
				Body:       []byte(`forbidden`),
			}
		},
	}
	h := newTestHandler(dm)

	w := httptest.NewRecorder()
	r := makeRequest(t, validContractID, validVersionID, "pdf")

	h.HandleExport().ServeHTTP(w, r)

	// DM 403 is unexpected (orchestrator sets org/user headers internally) → 500.
	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", w.Code)
	}
}

// ---------------------------------------------------------------------------
// HandleExport — context cancellation (raw, not wrapped in DMError)
// ---------------------------------------------------------------------------

func TestHandleExport_RawContextCanceled_Returns502(t *testing.T) {
	dm := &mockDMClient{
		getArtFn: func(_ context.Context, _, _, _ string) (*dmclient.ArtifactResponse, error) {
			return nil, context.Canceled
		},
	}
	h := newTestHandler(dm)

	w := httptest.NewRecorder()
	r := makeRequest(t, validContractID, validVersionID, "pdf")

	h.HandleExport().ServeHTTP(w, r)

	if w.Code != http.StatusBadGateway {
		t.Errorf("expected 502, got %d", w.Code)
	}
	body := decodeJSON(t, w)
	if body["error_code"] != "DM_UNAVAILABLE" {
		t.Errorf("expected DM_UNAVAILABLE, got %s", body["error_code"])
	}
}

func TestHandleExport_RawContextDeadlineExceeded_Returns502(t *testing.T) {
	dm := &mockDMClient{
		getArtFn: func(_ context.Context, _, _, _ string) (*dmclient.ArtifactResponse, error) {
			return nil, context.DeadlineExceeded
		},
	}
	h := newTestHandler(dm)

	w := httptest.NewRecorder()
	r := makeRequest(t, validContractID, validVersionID, "pdf")

	h.HandleExport().ServeHTTP(w, r)

	if w.Code != http.StatusBadGateway {
		t.Errorf("expected 502, got %d", w.Code)
	}
}

func TestHandleExport_WrappedContextCanceled_Returns502(t *testing.T) {
	dm := &mockDMClient{
		getArtFn: func(_ context.Context, _, _, _ string) (*dmclient.ArtifactResponse, error) {
			return nil, &dmclient.DMError{
				Operation: "GetArtifact",
				Cause:     context.Canceled,
			}
		},
	}
	h := newTestHandler(dm)

	w := httptest.NewRecorder()
	r := makeRequest(t, validContractID, validVersionID, "pdf")

	h.HandleExport().ServeHTTP(w, r)

	if w.Code != http.StatusBadGateway {
		t.Errorf("expected 502, got %d", w.Code)
	}
}

// ---------------------------------------------------------------------------
// HandleExport — DM response edge cases
// ---------------------------------------------------------------------------

func TestHandleExport_DMReturnsContent_Returns200(t *testing.T) {
	dm := &mockDMClient{
		getArtFn: func(_ context.Context, _, _, _ string) (*dmclient.ArtifactResponse, error) {
			return &dmclient.ArtifactResponse{
				Content: json.RawMessage(`{"data":"some-content"}`),
			}, nil
		},
	}
	h := newTestHandler(dm)

	w := httptest.NewRecorder()
	r := makeRequest(t, validContractID, validVersionID, "pdf")

	h.HandleExport().ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("expected Content-Type=application/json, got %s", ct)
	}
}

func TestHandleExport_DMReturnsEmptyResponse_Returns500(t *testing.T) {
	dm := &mockDMClient{
		getArtFn: func(_ context.Context, _, _, _ string) (*dmclient.ArtifactResponse, error) {
			return &dmclient.ArtifactResponse{}, nil
		},
	}
	h := newTestHandler(dm)

	w := httptest.NewRecorder()
	r := makeRequest(t, validContractID, validVersionID, "pdf")

	h.HandleExport().ServeHTTP(w, r)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", w.Code)
	}
	body := decodeJSON(t, w)
	if body["error_code"] != "INTERNAL_ERROR" {
		t.Errorf("expected INTERNAL_ERROR, got %s", body["error_code"])
	}
}

// ---------------------------------------------------------------------------
// HandleExport — redirect URL validation (open redirect prevention)
// ---------------------------------------------------------------------------

func TestHandleExport_InvalidRedirectURL_Returns500(t *testing.T) {
	tests := []struct {
		name string
		url  string
	}{
		{"javascript_scheme", "javascript:alert(1)"},
		{"data_scheme", "data:text/html,<h1>evil</h1>"},
		{"empty_host", "https:///path"},
		{"relative_path", "/some/path"},
		{"unparseable", "://invalid"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dm := &mockDMClient{
				getArtFn: func(_ context.Context, _, _, _ string) (*dmclient.ArtifactResponse, error) {
					return &dmclient.ArtifactResponse{RedirectURL: tt.url}, nil
				},
			}
			h := newTestHandler(dm)

			w := httptest.NewRecorder()
			r := makeRequest(t, validContractID, validVersionID, "pdf")

			h.HandleExport().ServeHTTP(w, r)

			if w.Code != http.StatusInternalServerError {
				t.Errorf("expected 500 for URL %q, got %d", tt.url, w.Code)
			}
		})
	}
}

func TestHandleExport_ValidRedirectURLSchemes(t *testing.T) {
	tests := []struct {
		name string
		url  string
	}{
		{"https", "https://s3.example.com/bucket/key?token=abc"},
		{"http_local", "http://localhost:9000/bucket/key?token=abc"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dm := &mockDMClient{
				getArtFn: func(_ context.Context, _, _, _ string) (*dmclient.ArtifactResponse, error) {
					return &dmclient.ArtifactResponse{RedirectURL: tt.url}, nil
				},
			}
			h := newTestHandler(dm)

			w := httptest.NewRecorder()
			r := makeRequest(t, validContractID, validVersionID, "pdf")

			h.HandleExport().ServeHTTP(w, r)

			if w.Code != http.StatusFound {
				t.Errorf("expected 302 for URL %q, got %d", tt.url, w.Code)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// HandleExport — all allowed roles
// ---------------------------------------------------------------------------

func TestHandleExport_AllowedRoles(t *testing.T) {
	tests := []struct {
		role       auth.Role
		wantStatus int
	}{
		{auth.RoleLawyer, http.StatusFound},
		{auth.RoleOrgAdmin, http.StatusFound},
	}

	for _, tt := range tests {
		t.Run(string(tt.role), func(t *testing.T) {
			dm := &mockDMClient{}
			h := newTestHandler(dm)

			w := httptest.NewRecorder()
			r := makeRequestWithRole(t, validContractID, validVersionID, "pdf", tt.role)

			h.HandleExport().ServeHTTP(w, r)

			if w.Code != tt.wantStatus {
				t.Errorf("expected %d for role %s, got %d", tt.wantStatus, tt.role, w.Code)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// HandleExport — response format
// ---------------------------------------------------------------------------

func TestHandleExport_ErrorResponseFormat(t *testing.T) {
	dm := &mockDMClient{}
	h := newTestHandler(dm)

	w := httptest.NewRecorder()
	r := makeRequest(t, "invalid", validVersionID, "pdf")

	h.HandleExport().ServeHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}

	// Check Content-Type.
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("expected Content-Type=application/json, got %s", ct)
	}

	body := decodeJSON(t, w)

	// All error responses must have these fields.
	for _, field := range []string{"error_code", "message"} {
		if _, ok := body[field]; !ok {
			t.Errorf("missing field %q in error response", field)
		}
	}
}

// ---------------------------------------------------------------------------
// Interface compliance
// ---------------------------------------------------------------------------

func TestDMClientInterfaceCompliance(t *testing.T) {
	var _ DMClient = (*mockDMClient)(nil)
	var _ DMClient = (*dmclient.Client)(nil)
}

// ---------------------------------------------------------------------------
// Format mapping
// ---------------------------------------------------------------------------

func TestValidFormats(t *testing.T) {
	tests := []struct {
		format       string
		wantArtifact string
		wantOK       bool
	}{
		{"pdf", "EXPORT_PDF", true},
		{"docx", "EXPORT_DOCX", true},
		{"xml", "", false},
		{"txt", "", false},
		{"PDF", "", false},
		{"DOCX", "", false},
		{"", "", false},
	}

	for _, tt := range tests {
		t.Run("format="+tt.format, func(t *testing.T) {
			artifact, ok := validFormats[tt.format]
			if ok != tt.wantOK {
				t.Errorf("validFormats[%q]: got ok=%v, want ok=%v", tt.format, ok, tt.wantOK)
			}
			if ok && artifact != tt.wantArtifact {
				t.Errorf("validFormats[%q] = %q, want %q", tt.format, artifact, tt.wantArtifact)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// isValidRedirectURL
// ---------------------------------------------------------------------------

func TestIsValidRedirectURL(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want bool
	}{
		{"https_valid", "https://s3.amazonaws.com/bucket/key", true},
		{"http_valid", "http://localhost:9000/bucket/key", true},
		{"javascript", "javascript:alert(1)", false},
		{"data_uri", "data:text/html,evil", false},
		{"ftp", "ftp://server/file", false},
		{"empty_host", "https:///path", false},
		{"relative", "/path/to/file", false},
		{"empty", "", false},
		{"just_scheme", "https://", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isValidRedirectURL(tt.url)
			if got != tt.want {
				t.Errorf("isValidRedirectURL(%q) = %v, want %v", tt.url, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Concurrent safety (go test -race)
// ---------------------------------------------------------------------------

func TestHandleExport_ConcurrentSafety(t *testing.T) {
	dm := &mockDMClient{}
	h := newTestHandler(dm)

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w := httptest.NewRecorder()
			r := makeRequest(t, validContractID, validVersionID, "pdf")
			h.HandleExport().ServeHTTP(w, r)
			if w.Code != http.StatusFound {
				t.Errorf("expected 302, got %d", w.Code)
			}
		}()
	}
	wg.Wait()
}
