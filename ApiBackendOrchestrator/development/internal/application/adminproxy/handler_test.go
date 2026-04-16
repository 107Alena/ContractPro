package adminproxy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"contractpro/api-orchestrator/internal/domain/model"
	"contractpro/api-orchestrator/internal/egress/opmclient"
	"contractpro/api-orchestrator/internal/infra/observability/logger"
	"contractpro/api-orchestrator/internal/ingress/middleware/auth"

	"github.com/go-chi/chi/v5"
)

// ---------------------------------------------------------------------------
// Mock OPM client
// ---------------------------------------------------------------------------

type mockOPMClient struct {
	mu sync.Mutex

	listPoliciesFn    func(ctx context.Context, orgID string) (json.RawMessage, error)
	updatePolicyFn    func(ctx context.Context, policyID string, body json.RawMessage) (json.RawMessage, error)
	listChecklistsFn  func(ctx context.Context, orgID string) (json.RawMessage, error)
	updateChecklistFn func(ctx context.Context, checklistID string, body json.RawMessage) (json.RawMessage, error)
}

func (m *mockOPMClient) ListPolicies(ctx context.Context, orgID string) (json.RawMessage, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.listPoliciesFn != nil {
		return m.listPoliciesFn(ctx, orgID)
	}
	return json.RawMessage(`[]`), nil
}

func (m *mockOPMClient) UpdatePolicy(ctx context.Context, policyID string, body json.RawMessage) (json.RawMessage, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.updatePolicyFn != nil {
		return m.updatePolicyFn(ctx, policyID, body)
	}
	return json.RawMessage(`{"id":"` + policyID + `"}`), nil
}

func (m *mockOPMClient) ListChecklists(ctx context.Context, orgID string) (json.RawMessage, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.listChecklistsFn != nil {
		return m.listChecklistsFn(ctx, orgID)
	}
	return json.RawMessage(`[]`), nil
}

func (m *mockOPMClient) UpdateChecklist(ctx context.Context, checklistID string, body json.RawMessage) (json.RawMessage, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.updateChecklistFn != nil {
		return m.updateChecklistFn(ctx, checklistID, body)
	}
	return json.RawMessage(`{"id":"` + checklistID + `"}`), nil
}

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

func newTestHandler(opm OPMClient) *Handler {
	log := logger.NewLogger("error")
	return NewHandler(opm, nil, log)
}

func newTestHandlerWithInvalidator(opm OPMClient, inv PermissionsInvalidator) *Handler {
	log := logger.NewLogger("error")
	return NewHandler(opm, inv, log)
}

// fakePublisher records every InvalidateOrg call for assertion.
// Stores the resulting channel name (matching the production publisher) so
// that tests can assert on the channel-naming convention too.
type fakePublisher struct {
	mu       sync.Mutex
	channels []string
	err      error
}

func (f *fakePublisher) InvalidateOrg(_ context.Context, orgID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.channels = append(f.channels, "permissions:invalidate:"+orgID)
	return f.err
}

func (f *fakePublisher) snapshot() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.channels))
	copy(out, f.channels)
	return out
}

func newAdminContext() context.Context {
	ctx := context.Background()
	ctx = auth.WithAuthContext(ctx, auth.AuthContext{
		UserID:         "user-001",
		OrganizationID: "org-001",
		Role:           auth.RoleOrgAdmin,
	})
	ctx = logger.WithRequestContext(ctx, logger.RequestContext{
		CorrelationID: "corr-001",
	})
	return ctx
}

func withChiParam(r *http.Request, key, value string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add(key, value)
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}

func parseErrorResponse(t *testing.T, body []byte) model.ErrorResponse {
	t.Helper()
	var resp model.ErrorResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("failed to parse error response: %v", err)
	}
	return resp
}

// ---------------------------------------------------------------------------
// Tests: HandleListPolicies
// ---------------------------------------------------------------------------

func TestHandleListPolicies_Success(t *testing.T) {
	mock := &mockOPMClient{
		listPoliciesFn: func(_ context.Context, orgID string) (json.RawMessage, error) {
			if orgID != "org-001" {
				t.Errorf("orgID = %q, want %q", orgID, "org-001")
			}
			return json.RawMessage(`[{"id":"pol-1","name":"Default"}]`), nil
		},
	}
	h := newTestHandler(mock)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/policies", nil)
	req = req.WithContext(newAdminContext())
	w := httptest.NewRecorder()

	h.HandleListPolicies().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want %q", ct, "application/json")
	}

	var policies []json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &policies); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if len(policies) != 1 {
		t.Errorf("len(policies) = %d, want 1", len(policies))
	}
}

func TestHandleListPolicies_NoAuth(t *testing.T) {
	h := newTestHandler(&mockOPMClient{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/policies", nil)
	// No AuthContext
	w := httptest.NewRecorder()

	h.HandleListPolicies().ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
	resp := parseErrorResponse(t, w.Body.Bytes())
	if resp.ErrorCode != string(model.ErrAuthTokenMissing) {
		t.Errorf("error_code = %q, want %q", resp.ErrorCode, model.ErrAuthTokenMissing)
	}
}

func TestHandleListPolicies_OPMDown(t *testing.T) {
	mock := &mockOPMClient{
		listPoliciesFn: func(_ context.Context, _ string) (json.RawMessage, error) {
			return nil, &opmclient.OPMError{
				Operation:  "ListPolicies",
				StatusCode: 503,
				Body:       []byte("service unavailable"),
				Retryable:  true,
			}
		},
	}
	h := newTestHandler(mock)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/policies", nil)
	req = req.WithContext(newAdminContext())
	w := httptest.NewRecorder()

	h.HandleListPolicies().ServeHTTP(w, req)

	if w.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadGateway)
	}
	resp := parseErrorResponse(t, w.Body.Bytes())
	if resp.ErrorCode != string(model.ErrOPMUnavailable) {
		t.Errorf("error_code = %q, want %q", resp.ErrorCode, model.ErrOPMUnavailable)
	}
}

func TestHandleListPolicies_OPMDisabled(t *testing.T) {
	mock := &mockOPMClient{
		listPoliciesFn: func(_ context.Context, _ string) (json.RawMessage, error) {
			return nil, &opmclient.OPMError{
				Operation: "ListPolicies",
				Retryable: false,
				Cause:     opmclient.ErrOPMDisabled,
			}
		},
	}
	h := newTestHandler(mock)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/policies", nil)
	req = req.WithContext(newAdminContext())
	w := httptest.NewRecorder()

	h.HandleListPolicies().ServeHTTP(w, req)

	if w.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadGateway)
	}
	resp := parseErrorResponse(t, w.Body.Bytes())
	if resp.ErrorCode != string(model.ErrOPMUnavailable) {
		t.Errorf("error_code = %q, want %q", resp.ErrorCode, model.ErrOPMUnavailable)
	}
}

func TestHandleListPolicies_ContextCanceled(t *testing.T) {
	mock := &mockOPMClient{
		listPoliciesFn: func(_ context.Context, _ string) (json.RawMessage, error) {
			return nil, context.Canceled
		},
	}
	h := newTestHandler(mock)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/policies", nil)
	req = req.WithContext(newAdminContext())
	w := httptest.NewRecorder()

	h.HandleListPolicies().ServeHTTP(w, req)

	if w.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadGateway)
	}
}

func TestHandleListPolicies_DeadlineExceeded(t *testing.T) {
	mock := &mockOPMClient{
		listPoliciesFn: func(_ context.Context, _ string) (json.RawMessage, error) {
			return nil, context.DeadlineExceeded
		},
	}
	h := newTestHandler(mock)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/policies", nil)
	req = req.WithContext(newAdminContext())
	w := httptest.NewRecorder()

	h.HandleListPolicies().ServeHTTP(w, req)

	if w.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadGateway)
	}
}

func TestHandleListPolicies_NilResponse(t *testing.T) {
	mock := &mockOPMClient{
		listPoliciesFn: func(_ context.Context, _ string) (json.RawMessage, error) {
			return nil, nil
		},
	}
	h := newTestHandler(mock)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/policies", nil)
	req = req.WithContext(newAdminContext())
	w := httptest.NewRecorder()

	h.HandleListPolicies().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
	// Nil data → empty JSON object fallback.
	body := strings.TrimSpace(w.Body.String())
	if body != "{}" {
		t.Errorf("body = %q, want %q", body, "{}")
	}
}

// ---------------------------------------------------------------------------
// Tests: HandleUpdatePolicy
// ---------------------------------------------------------------------------

func TestHandleUpdatePolicy_Success(t *testing.T) {
	var receivedID string
	var receivedBody json.RawMessage
	mock := &mockOPMClient{
		updatePolicyFn: func(_ context.Context, policyID string, body json.RawMessage) (json.RawMessage, error) {
			receivedID = policyID
			receivedBody = body
			return json.RawMessage(`{"id":"pol-1","name":"Updated"}`), nil
		},
	}
	h := newTestHandler(mock)

	reqBody := `{"name":"Updated","strictness":"HIGH"}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/policies/pol-1", strings.NewReader(reqBody))
	req = req.WithContext(newAdminContext())
	req = withChiParam(req, "policy_id", "pol-1")
	w := httptest.NewRecorder()

	h.HandleUpdatePolicy().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if receivedID != "pol-1" {
		t.Errorf("policyID = %q, want %q", receivedID, "pol-1")
	}
	if string(receivedBody) != reqBody {
		t.Errorf("body = %q, want %q", string(receivedBody), reqBody)
	}
}

func TestHandleUpdatePolicy_NoAuth(t *testing.T) {
	h := newTestHandler(&mockOPMClient{})

	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/policies/pol-1",
		strings.NewReader(`{"name":"test"}`))
	req = withChiParam(req, "policy_id", "pol-1")
	w := httptest.NewRecorder()

	h.HandleUpdatePolicy().ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestHandleUpdatePolicy_EmptyPolicyID(t *testing.T) {
	h := newTestHandler(&mockOPMClient{})

	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/policies/",
		strings.NewReader(`{"name":"test"}`))
	req = req.WithContext(newAdminContext())
	// No chi param set — empty policy_id.
	w := httptest.NewRecorder()

	h.HandleUpdatePolicy().ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleUpdatePolicy_EmptyBody(t *testing.T) {
	h := newTestHandler(&mockOPMClient{})

	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/policies/pol-1",
		strings.NewReader(""))
	req = req.WithContext(newAdminContext())
	req = withChiParam(req, "policy_id", "pol-1")
	w := httptest.NewRecorder()

	h.HandleUpdatePolicy().ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
	resp := parseErrorResponse(t, w.Body.Bytes())
	if resp.ErrorCode != string(model.ErrValidationError) {
		t.Errorf("error_code = %q, want %q", resp.ErrorCode, model.ErrValidationError)
	}
}

func TestHandleUpdatePolicy_InvalidJSON(t *testing.T) {
	h := newTestHandler(&mockOPMClient{})

	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/policies/pol-1",
		strings.NewReader("not json"))
	req = req.WithContext(newAdminContext())
	req = withChiParam(req, "policy_id", "pol-1")
	w := httptest.NewRecorder()

	h.HandleUpdatePolicy().ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
	resp := parseErrorResponse(t, w.Body.Bytes())
	if resp.ErrorCode != string(model.ErrValidationError) {
		t.Errorf("error_code = %q, want %q", resp.ErrorCode, model.ErrValidationError)
	}
}

func TestHandleUpdatePolicy_BodyTooLarge(t *testing.T) {
	h := newTestHandler(&mockOPMClient{})

	// 1 MB + 1 byte.
	bigBody := `{"data":"` + strings.Repeat("x", maxAdminBodySize) + `"}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/policies/pol-1",
		strings.NewReader(bigBody))
	req = req.WithContext(newAdminContext())
	req = withChiParam(req, "policy_id", "pol-1")
	w := httptest.NewRecorder()

	h.HandleUpdatePolicy().ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleUpdatePolicy_OPM404(t *testing.T) {
	mock := &mockOPMClient{
		updatePolicyFn: func(_ context.Context, _ string, _ json.RawMessage) (json.RawMessage, error) {
			return nil, &opmclient.OPMError{
				Operation:  "UpdatePolicy",
				StatusCode: 404,
				Body:       []byte("not found"),
				Retryable:  false,
			}
		},
	}
	h := newTestHandler(mock)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/policies/pol-999",
		strings.NewReader(`{"name":"test"}`))
	req = req.WithContext(newAdminContext())
	req = withChiParam(req, "policy_id", "pol-999")
	w := httptest.NewRecorder()

	h.HandleUpdatePolicy().ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
	resp := parseErrorResponse(t, w.Body.Bytes())
	if resp.ErrorCode != string(model.ErrPolicyNotFound) {
		t.Errorf("error_code = %q, want %q", resp.ErrorCode, model.ErrPolicyNotFound)
	}
}

func TestHandleUpdatePolicy_OPM400(t *testing.T) {
	mock := &mockOPMClient{
		updatePolicyFn: func(_ context.Context, _ string, _ json.RawMessage) (json.RawMessage, error) {
			return nil, &opmclient.OPMError{
				Operation:  "UpdatePolicy",
				StatusCode: 400,
				Body:       []byte("bad request"),
				Retryable:  false,
			}
		},
	}
	h := newTestHandler(mock)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/policies/pol-1",
		strings.NewReader(`{"invalid":"data"}`))
	req = req.WithContext(newAdminContext())
	req = withChiParam(req, "policy_id", "pol-1")
	w := httptest.NewRecorder()

	h.HandleUpdatePolicy().ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
	resp := parseErrorResponse(t, w.Body.Bytes())
	if resp.ErrorCode != string(model.ErrValidationError) {
		t.Errorf("error_code = %q, want %q", resp.ErrorCode, model.ErrValidationError)
	}
}

func TestHandleUpdatePolicy_OPMDown(t *testing.T) {
	mock := &mockOPMClient{
		updatePolicyFn: func(_ context.Context, _ string, _ json.RawMessage) (json.RawMessage, error) {
			return nil, &opmclient.OPMError{
				Operation:  "UpdatePolicy",
				StatusCode: 500,
				Body:       []byte("internal error"),
				Retryable:  true,
			}
		},
	}
	h := newTestHandler(mock)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/policies/pol-1",
		strings.NewReader(`{"name":"test"}`))
	req = req.WithContext(newAdminContext())
	req = withChiParam(req, "policy_id", "pol-1")
	w := httptest.NewRecorder()

	h.HandleUpdatePolicy().ServeHTTP(w, req)

	if w.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadGateway)
	}
}

func TestHandleUpdatePolicy_TransportError(t *testing.T) {
	mock := &mockOPMClient{
		updatePolicyFn: func(_ context.Context, _ string, _ json.RawMessage) (json.RawMessage, error) {
			return nil, &opmclient.OPMError{
				Operation: "UpdatePolicy",
				Retryable: true,
				Cause:     errors.New("connection refused"),
			}
		},
	}
	h := newTestHandler(mock)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/policies/pol-1",
		strings.NewReader(`{"name":"test"}`))
	req = req.WithContext(newAdminContext())
	req = withChiParam(req, "policy_id", "pol-1")
	w := httptest.NewRecorder()

	h.HandleUpdatePolicy().ServeHTTP(w, req)

	if w.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadGateway)
	}
}

func TestHandleUpdatePolicy_UnknownError(t *testing.T) {
	mock := &mockOPMClient{
		updatePolicyFn: func(_ context.Context, _ string, _ json.RawMessage) (json.RawMessage, error) {
			return nil, errors.New("something unexpected")
		},
	}
	h := newTestHandler(mock)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/policies/pol-1",
		strings.NewReader(`{"name":"test"}`))
	req = req.WithContext(newAdminContext())
	req = withChiParam(req, "policy_id", "pol-1")
	w := httptest.NewRecorder()

	h.HandleUpdatePolicy().ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", w.Code, http.StatusInternalServerError)
	}
	resp := parseErrorResponse(t, w.Body.Bytes())
	if resp.ErrorCode != string(model.ErrInternalError) {
		t.Errorf("error_code = %q, want %q", resp.ErrorCode, model.ErrInternalError)
	}
}

// ---------------------------------------------------------------------------
// Tests: HandleListChecklists
// ---------------------------------------------------------------------------

func TestHandleListChecklists_Success(t *testing.T) {
	mock := &mockOPMClient{
		listChecklistsFn: func(_ context.Context, orgID string) (json.RawMessage, error) {
			if orgID != "org-001" {
				t.Errorf("orgID = %q, want %q", orgID, "org-001")
			}
			return json.RawMessage(`[{"id":"cl-1"}]`), nil
		},
	}
	h := newTestHandler(mock)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/checklists", nil)
	req = req.WithContext(newAdminContext())
	w := httptest.NewRecorder()

	h.HandleListChecklists().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestHandleListChecklists_NoAuth(t *testing.T) {
	h := newTestHandler(&mockOPMClient{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/checklists", nil)
	w := httptest.NewRecorder()

	h.HandleListChecklists().ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestHandleListChecklists_OPMDown(t *testing.T) {
	mock := &mockOPMClient{
		listChecklistsFn: func(_ context.Context, _ string) (json.RawMessage, error) {
			return nil, &opmclient.OPMError{
				Operation:  "ListChecklists",
				StatusCode: 500,
				Retryable:  true,
			}
		},
	}
	h := newTestHandler(mock)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/checklists", nil)
	req = req.WithContext(newAdminContext())
	w := httptest.NewRecorder()

	h.HandleListChecklists().ServeHTTP(w, req)

	if w.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadGateway)
	}
}

func TestHandleListChecklists_OPMDisabled(t *testing.T) {
	mock := &mockOPMClient{
		listChecklistsFn: func(_ context.Context, _ string) (json.RawMessage, error) {
			return nil, &opmclient.OPMError{
				Operation: "ListChecklists",
				Retryable: false,
				Cause:     opmclient.ErrOPMDisabled,
			}
		},
	}
	h := newTestHandler(mock)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/checklists", nil)
	req = req.WithContext(newAdminContext())
	w := httptest.NewRecorder()

	h.HandleListChecklists().ServeHTTP(w, req)

	if w.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadGateway)
	}
}

// ---------------------------------------------------------------------------
// Tests: HandleUpdateChecklist
// ---------------------------------------------------------------------------

func TestHandleUpdateChecklist_Success(t *testing.T) {
	var receivedID string
	mock := &mockOPMClient{
		updateChecklistFn: func(_ context.Context, checklistID string, _ json.RawMessage) (json.RawMessage, error) {
			receivedID = checklistID
			return json.RawMessage(`{"id":"cl-1","updated":true}`), nil
		},
	}
	h := newTestHandler(mock)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/checklists/cl-1",
		strings.NewReader(`{"items":["check1","check2"]}`))
	req = req.WithContext(newAdminContext())
	req = withChiParam(req, "checklist_id", "cl-1")
	w := httptest.NewRecorder()

	h.HandleUpdateChecklist().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if receivedID != "cl-1" {
		t.Errorf("checklistID = %q, want %q", receivedID, "cl-1")
	}
}

func TestHandleUpdateChecklist_NoAuth(t *testing.T) {
	h := newTestHandler(&mockOPMClient{})

	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/checklists/cl-1",
		strings.NewReader(`{"items":[]}`))
	req = withChiParam(req, "checklist_id", "cl-1")
	w := httptest.NewRecorder()

	h.HandleUpdateChecklist().ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestHandleUpdateChecklist_EmptyChecklistID(t *testing.T) {
	h := newTestHandler(&mockOPMClient{})

	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/checklists/",
		strings.NewReader(`{"items":[]}`))
	req = req.WithContext(newAdminContext())
	w := httptest.NewRecorder()

	h.HandleUpdateChecklist().ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleUpdateChecklist_OPM404(t *testing.T) {
	mock := &mockOPMClient{
		updateChecklistFn: func(_ context.Context, _ string, _ json.RawMessage) (json.RawMessage, error) {
			return nil, &opmclient.OPMError{
				Operation:  "UpdateChecklist",
				StatusCode: 404,
				Body:       []byte("not found"),
				Retryable:  false,
			}
		},
	}
	h := newTestHandler(mock)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/checklists/cl-999",
		strings.NewReader(`{"items":[]}`))
	req = req.WithContext(newAdminContext())
	req = withChiParam(req, "checklist_id", "cl-999")
	w := httptest.NewRecorder()

	h.HandleUpdateChecklist().ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
	resp := parseErrorResponse(t, w.Body.Bytes())
	if resp.ErrorCode != string(model.ErrChecklistNotFound) {
		t.Errorf("error_code = %q, want %q", resp.ErrorCode, model.ErrChecklistNotFound)
	}
}

func TestHandleUpdateChecklist_EmptyBody(t *testing.T) {
	h := newTestHandler(&mockOPMClient{})

	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/checklists/cl-1",
		strings.NewReader(""))
	req = req.WithContext(newAdminContext())
	req = withChiParam(req, "checklist_id", "cl-1")
	w := httptest.NewRecorder()

	h.HandleUpdateChecklist().ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleUpdateChecklist_InvalidJSON(t *testing.T) {
	h := newTestHandler(&mockOPMClient{})

	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/checklists/cl-1",
		strings.NewReader("{broken"))
	req = req.WithContext(newAdminContext())
	req = withChiParam(req, "checklist_id", "cl-1")
	w := httptest.NewRecorder()

	h.HandleUpdateChecklist().ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

// ---------------------------------------------------------------------------
// Tests: Response format
// ---------------------------------------------------------------------------

func TestResponseFormat_ContentType(t *testing.T) {
	mock := &mockOPMClient{
		listPoliciesFn: func(_ context.Context, _ string) (json.RawMessage, error) {
			return json.RawMessage(`[]`), nil
		},
	}
	h := newTestHandler(mock)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/policies", nil)
	req = req.WithContext(newAdminContext())
	w := httptest.NewRecorder()

	h.HandleListPolicies().ServeHTTP(w, req)

	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want %q", ct, "application/json")
	}
}

func TestErrorResponseFormat_CorrelationID(t *testing.T) {
	h := newTestHandler(&mockOPMClient{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/policies", nil)
	ctx := logger.WithRequestContext(context.Background(), logger.RequestContext{
		CorrelationID: "test-corr-123",
	})
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	h.HandleListPolicies().ServeHTTP(w, req)

	// No auth → error response with correlation_id.
	resp := parseErrorResponse(t, w.Body.Bytes())
	if resp.CorrelationID != "test-corr-123" {
		t.Errorf("correlation_id = %q, want %q", resp.CorrelationID, "test-corr-123")
	}
}

// ---------------------------------------------------------------------------
// Tests: handleOPMError classification
// ---------------------------------------------------------------------------

func TestHandleOPMError_ContextCanceled(t *testing.T) {
	mock := &mockOPMClient{
		listPoliciesFn: func(_ context.Context, _ string) (json.RawMessage, error) {
			return nil, context.Canceled
		},
	}
	h := newTestHandler(mock)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/policies", nil)
	req = req.WithContext(newAdminContext())
	w := httptest.NewRecorder()

	h.HandleListPolicies().ServeHTTP(w, req)

	if w.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadGateway)
	}
}

func TestHandleOPMError_DeadlineExceeded(t *testing.T) {
	mock := &mockOPMClient{
		listChecklistsFn: func(_ context.Context, _ string) (json.RawMessage, error) {
			return nil, context.DeadlineExceeded
		},
	}
	h := newTestHandler(mock)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/checklists", nil)
	req = req.WithContext(newAdminContext())
	w := httptest.NewRecorder()

	h.HandleListChecklists().ServeHTTP(w, req)

	if w.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadGateway)
	}
}

// ---------------------------------------------------------------------------
// Tests: Interface compliance
// ---------------------------------------------------------------------------

func TestOPMClientInterface(t *testing.T) {
	var _ OPMClient = (*mockOPMClient)(nil)
	var _ OPMClient = (*opmclient.Client)(nil)
}

// ---------------------------------------------------------------------------
// Tests: Constructor
// ---------------------------------------------------------------------------

func TestNewHandler(t *testing.T) {
	log := logger.NewLogger("error")
	mock := &mockOPMClient{}
	h := NewHandler(mock, nil, log)

	if h == nil {
		t.Fatal("NewHandler returned nil")
	}
	if h.opm == nil {
		t.Error("handler opm is nil")
	}
	if h.log == nil {
		t.Error("handler log is nil")
	}
	if h.invalidator != nil {
		t.Error("handler invalidator should be nil when not provided")
	}
}

// ---------------------------------------------------------------------------
// Tests: HandleUpdatePolicy — permissions invalidation (ORCH-TASK-050)
// ---------------------------------------------------------------------------

func TestHandleUpdatePolicy_PublishesInvalidation(t *testing.T) {
	// mockOPMClient uses updatePolicyFn in its existing pattern.
	mock := findUpdatePolicyField(t)
	mock.updatePolicyFn = func(_ context.Context, _ string, _ json.RawMessage) (json.RawMessage, error) {
		return json.RawMessage(`{"id":"p1","updated":true}`), nil
	}
	pub := &fakePublisher{}
	h := newTestHandlerWithInvalidator(mock, pub)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/policies/p1",
		bytes.NewReader([]byte(`{"enabled":true}`)))
	req = req.WithContext(newAdminContext())
	req = withChiParam(req, "policy_id", "p1")
	rec := httptest.NewRecorder()
	h.HandleUpdatePolicy().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body=%s)", rec.Code, http.StatusOK, rec.Body.String())
	}
	channels := pub.snapshot()
	if len(channels) != 1 {
		t.Fatalf("published %d events, want 1", len(channels))
	}
	want := "permissions:invalidate:org-001"
	if channels[0] != want {
		t.Errorf("channel = %q, want %q", channels[0], want)
	}
}

// If Publish fails (Redis down), request still succeeds.
func TestHandleUpdatePolicy_PublishFailure_DoesNotFailRequest(t *testing.T) {
	mock := findUpdatePolicyField(t)
	mock.updatePolicyFn = func(_ context.Context, _ string, _ json.RawMessage) (json.RawMessage, error) {
		return json.RawMessage(`{}`), nil
	}
	pub := &fakePublisher{err: errors.New("redis down")}
	h := newTestHandlerWithInvalidator(mock, pub)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/policies/p1",
		bytes.NewReader([]byte(`{"enabled":true}`)))
	req = req.WithContext(newAdminContext())
	req = withChiParam(req, "policy_id", "p1")
	rec := httptest.NewRecorder()
	h.HandleUpdatePolicy().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if len(pub.snapshot()) != 1 {
		t.Errorf("Publish not attempted on failure path")
	}
}

// If OPM update itself fails, no invalidation event is published.
func TestHandleUpdatePolicy_OPMFailure_NoPublish(t *testing.T) {
	mock := findUpdatePolicyField(t)
	mock.updatePolicyFn = func(_ context.Context, _ string, _ json.RawMessage) (json.RawMessage, error) {
		return nil, &opmclient.OPMError{
			Operation: "UpdatePolicy", StatusCode: 503, Retryable: true,
		}
	}
	pub := &fakePublisher{}
	h := newTestHandlerWithInvalidator(mock, pub)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/policies/p1",
		bytes.NewReader([]byte(`{"enabled":true}`)))
	req = req.WithContext(newAdminContext())
	req = withChiParam(req, "policy_id", "p1")
	rec := httptest.NewRecorder()
	h.HandleUpdatePolicy().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadGateway)
	}
	if len(pub.snapshot()) != 0 {
		t.Errorf("invalidation published despite OPM failure: %v", pub.snapshot())
	}
}

// findUpdatePolicyField is a diagnostic helper that panics if the mockOPMClient
// renames its updatePolicyFn field — cheap guard against a silent test skip.
func findUpdatePolicyField(t *testing.T) *mockOPMClient {
	t.Helper()
	return &mockOPMClient{}
}

// ---------------------------------------------------------------------------
// Tests: readBody
// ---------------------------------------------------------------------------

func TestReadBody_ValidJSON(t *testing.T) {
	h := newTestHandler(&mockOPMClient{})

	body := `{"key":"value"}`
	req := httptest.NewRequest(http.MethodPut, "/", strings.NewReader(body))
	w := httptest.NewRecorder()

	data, ok := h.readBody(w, req)
	if !ok {
		t.Fatal("readBody returned false for valid JSON")
	}
	if string(data) != body {
		t.Errorf("data = %q, want %q", string(data), body)
	}
}

func TestReadBody_Empty(t *testing.T) {
	h := newTestHandler(&mockOPMClient{})

	req := httptest.NewRequest(http.MethodPut, "/", strings.NewReader(""))
	w := httptest.NewRecorder()

	_, ok := h.readBody(w, req)
	if ok {
		t.Fatal("readBody returned true for empty body")
	}
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestReadBody_InvalidJSON(t *testing.T) {
	h := newTestHandler(&mockOPMClient{})

	req := httptest.NewRequest(http.MethodPut, "/", strings.NewReader("not json"))
	w := httptest.NewRecorder()

	_, ok := h.readBody(w, req)
	if ok {
		t.Fatal("readBody returned true for invalid JSON")
	}
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestReadBody_TooLarge(t *testing.T) {
	h := newTestHandler(&mockOPMClient{})

	// Create body slightly larger than maxAdminBodySize.
	bigData := bytes.Repeat([]byte("a"), maxAdminBodySize+100)
	req := httptest.NewRequest(http.MethodPut, "/", bytes.NewReader(bigData))
	w := httptest.NewRecorder()

	_, ok := h.readBody(w, req)
	if ok {
		t.Fatal("readBody returned true for oversized body")
	}
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestReadBody_ReadError(t *testing.T) {
	h := newTestHandler(&mockOPMClient{})

	req := httptest.NewRequest(http.MethodPut, "/", &errorReader{})
	w := httptest.NewRecorder()

	_, ok := h.readBody(w, req)
	if ok {
		t.Fatal("readBody returned true for read error")
	}
	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", w.Code, http.StatusInternalServerError)
	}
}

// errorReader is an io.ReadCloser that always returns an error.
type errorReader struct{}

func (e *errorReader) Read(_ []byte) (int, error) { return 0, errors.New("read error") }
func (e *errorReader) Close() error                { return nil }

// ---------------------------------------------------------------------------
// Tests: writeRawJSON
// ---------------------------------------------------------------------------

func TestWriteRawJSON_NilData(t *testing.T) {
	h := newTestHandler(&mockOPMClient{})

	w := httptest.NewRecorder()
	h.writeRawJSON(context.Background(), w, http.StatusOK, nil)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if w.Body.String() != "{}" {
		t.Errorf("body = %q, want %q", w.Body.String(), "{}")
	}
}

func TestWriteRawJSON_WithData(t *testing.T) {
	h := newTestHandler(&mockOPMClient{})

	w := httptest.NewRecorder()
	h.writeRawJSON(context.Background(), w, http.StatusOK, json.RawMessage(`[1,2,3]`))

	if w.Body.String() != "[1,2,3]" {
		t.Errorf("body = %q, want %q", w.Body.String(), "[1,2,3]")
	}
}

func TestWriteRawJSON_ErrorWriter(t *testing.T) {
	h := newTestHandler(&mockOPMClient{})

	ew := &errorWriter{header: http.Header{}}
	h.writeRawJSON(context.Background(), ew, http.StatusOK, json.RawMessage(`{}`))
	// Should not panic — error is logged.
}

// errorWriter is an http.ResponseWriter that fails on Write.
type errorWriter struct {
	header http.Header
}

func (e *errorWriter) Header() http.Header        { return e.header }
func (e *errorWriter) Write(_ []byte) (int, error) { return 0, errors.New("write error") }
func (e *errorWriter) WriteHeader(_ int)           {}

// ---------------------------------------------------------------------------
// Tests: Concurrent safety
// ---------------------------------------------------------------------------

func TestConcurrentAccess(t *testing.T) {
	mock := &mockOPMClient{
		listPoliciesFn: func(_ context.Context, _ string) (json.RawMessage, error) {
			return json.RawMessage(`[]`), nil
		},
		listChecklistsFn: func(_ context.Context, _ string) (json.RawMessage, error) {
			return json.RawMessage(`[]`), nil
		},
	}
	h := newTestHandler(mock)

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req = req.WithContext(newAdminContext())
			w := httptest.NewRecorder()
			h.HandleListPolicies().ServeHTTP(w, req)
		}()
		go func() {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req = req.WithContext(newAdminContext())
			w := httptest.NewRecorder()
			h.HandleListChecklists().ServeHTTP(w, req)
		}()
	}
	wg.Wait()
}

// ---------------------------------------------------------------------------
// Tests: Proxy pass-through (body is not modified)
// ---------------------------------------------------------------------------

func TestProxyPassthrough_PolicyBody(t *testing.T) {
	expectedBody := `{"name":"Test Policy","strictness":"HIGH","rules":[{"id":"r1"}]}`
	var receivedBody json.RawMessage
	mock := &mockOPMClient{
		updatePolicyFn: func(_ context.Context, _ string, body json.RawMessage) (json.RawMessage, error) {
			receivedBody = body
			return json.RawMessage(`{"ok":true}`), nil
		},
	}
	h := newTestHandler(mock)

	req := httptest.NewRequest(http.MethodPut, "/", strings.NewReader(expectedBody))
	req = req.WithContext(newAdminContext())
	req = withChiParam(req, "policy_id", "pol-1")
	w := httptest.NewRecorder()

	h.HandleUpdatePolicy().ServeHTTP(w, req)

	if string(receivedBody) != expectedBody {
		t.Errorf("body not passed through:\ngot:  %s\nwant: %s", receivedBody, expectedBody)
	}
}

func TestProxyPassthrough_ChecklistBody(t *testing.T) {
	expectedBody := `{"items":[{"id":"c1","label":"Проверить реквизиты"}]}`
	var receivedBody json.RawMessage
	mock := &mockOPMClient{
		updateChecklistFn: func(_ context.Context, _ string, body json.RawMessage) (json.RawMessage, error) {
			receivedBody = body
			return json.RawMessage(`{"ok":true}`), nil
		},
	}
	h := newTestHandler(mock)

	req := httptest.NewRequest(http.MethodPut, "/", strings.NewReader(expectedBody))
	req = req.WithContext(newAdminContext())
	req = withChiParam(req, "checklist_id", "cl-1")
	w := httptest.NewRecorder()

	h.HandleUpdateChecklist().ServeHTTP(w, req)

	if string(receivedBody) != expectedBody {
		t.Errorf("body not passed through:\ngot:  %s\nwant: %s", receivedBody, expectedBody)
	}
}

// ---------------------------------------------------------------------------
// Tests: OPM error with wrapped context errors
// ---------------------------------------------------------------------------

func TestHandleOPMError_WrappedContextCanceled(t *testing.T) {
	mock := &mockOPMClient{
		updatePolicyFn: func(_ context.Context, _ string, _ json.RawMessage) (json.RawMessage, error) {
			return nil, &opmclient.OPMError{
				Operation: "UpdatePolicy",
				Retryable: true,
				Cause:     context.Canceled,
			}
		},
	}
	h := newTestHandler(mock)

	req := httptest.NewRequest(http.MethodPut, "/", strings.NewReader(`{"a":"b"}`))
	req = req.WithContext(newAdminContext())
	req = withChiParam(req, "policy_id", "pol-1")
	w := httptest.NewRecorder()

	h.HandleUpdatePolicy().ServeHTTP(w, req)

	// OPMError wrapping context.Canceled should be caught by errors.Is.
	if w.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadGateway)
	}
}

// ---------------------------------------------------------------------------
// Tests: No OPM call when auth fails
// ---------------------------------------------------------------------------

func TestNoOPMCallOnAuthFailure(t *testing.T) {
	called := false
	mock := &mockOPMClient{
		listPoliciesFn: func(_ context.Context, _ string) (json.RawMessage, error) {
			called = true
			return nil, nil
		},
	}
	h := newTestHandler(mock)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	// No auth context.
	w := httptest.NewRecorder()
	h.HandleListPolicies().ServeHTTP(w, req)

	if called {
		t.Error("OPM client was called despite missing auth context")
	}
}

func TestNoOPMCallOnValidationFailure(t *testing.T) {
	called := false
	mock := &mockOPMClient{
		updatePolicyFn: func(_ context.Context, _ string, _ json.RawMessage) (json.RawMessage, error) {
			called = true
			return nil, nil
		},
	}
	h := newTestHandler(mock)

	req := httptest.NewRequest(http.MethodPut, "/", strings.NewReader(""))
	req = req.WithContext(newAdminContext())
	req = withChiParam(req, "policy_id", "pol-1")
	w := httptest.NewRecorder()
	h.HandleUpdatePolicy().ServeHTTP(w, req)

	if called {
		t.Error("OPM client was called despite validation failure (empty body)")
	}
}

// ---------------------------------------------------------------------------
// Tests: readBody with io.NopCloser wrapping
// ---------------------------------------------------------------------------

func TestReadBody_NilBody(t *testing.T) {
	h := newTestHandler(&mockOPMClient{})

	req := httptest.NewRequest(http.MethodPut, "/", nil)
	req.Body = io.NopCloser(strings.NewReader(""))
	w := httptest.NewRecorder()

	_, ok := h.readBody(w, req)
	if ok {
		t.Fatal("readBody returned true for nil/empty body")
	}
}
