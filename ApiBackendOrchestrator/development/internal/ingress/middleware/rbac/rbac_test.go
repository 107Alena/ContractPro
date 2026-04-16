package rbac

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"

	"contractpro/api-orchestrator/internal/domain/model"
	"contractpro/api-orchestrator/internal/infra/observability/logger"
	"contractpro/api-orchestrator/internal/ingress/middleware/auth"

	"github.com/go-chi/chi/v5"
)

// --- test helpers ---

func testLogger() *logger.Logger {
	return logger.NewLogger("error")
}

// okHandler is a simple handler that returns 200 OK with body "ok".
// Used to verify that the middleware passes through on success.
var okHandler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
})

// serveWithChi sets up a minimal chi router that mounts the RBAC middleware
// and a single route matching the given method+pattern, then dispatches the
// request. This ensures chi.RouteContext is populated exactly as in production.
func serveWithChi(t *testing.T, mw *Middleware, method, pattern, requestPath string, ctx context.Context) *httptest.ResponseRecorder {
	t.Helper()

	r := chi.NewRouter()
	r.Route("/api/v1", func(r chi.Router) {
		r.Use(mw.Handler())
		switch method {
		case http.MethodGet:
			r.Get(pattern, okHandler)
		case http.MethodPost:
			r.Post(pattern, okHandler)
		case http.MethodPut:
			r.Put(pattern, okHandler)
		case http.MethodDelete:
			r.Delete(pattern, okHandler)
		default:
			t.Fatalf("unsupported method %s", method)
		}
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(method, requestPath, nil)
	if ctx != nil {
		req = req.WithContext(ctx)
	}

	r.ServeHTTP(w, req)
	return w
}

// ctxWithAuth returns a context.Context carrying an AuthContext with the given role.
func ctxWithAuth(role auth.Role) context.Context {
	ac := auth.AuthContext{
		UserID:         "user-test-123",
		OrganizationID: "org-test-456",
		Role:           role,
		TokenID:        "jti-test-789",
	}
	return auth.WithAuthContext(context.Background(), ac)
}

// decodeErrorResponse decodes the response body into a model.ErrorResponse.
func decodeErrorResponse(t *testing.T, w *httptest.ResponseRecorder) model.ErrorResponse {
	t.Helper()
	var resp model.ErrorResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode error response: %v", err)
	}
	return resp
}

// --- Constructor tests ---

func TestNewMiddleware_ReturnsNonNil(t *testing.T) {
	mw := NewMiddleware(testLogger())
	if mw == nil {
		t.Fatal("NewMiddleware returned nil")
	}
}

func TestNewMiddleware_CopiesPolicy(t *testing.T) {
	mw := NewMiddleware(testLogger())
	// Verify the policy has the same number of entries as accessRules.
	if len(mw.policy) != len(accessRules) {
		t.Errorf("policy size = %d, want %d", len(mw.policy), len(accessRules))
	}
}

func TestNewMiddleware_policyKeysMatchAccessRules(t *testing.T) {
	mw := NewMiddleware(testLogger())
	keys := mw.policyKeys()
	if len(keys) != len(accessRules) {
		t.Errorf("policyKeys() returned %d keys, want %d", len(keys), len(accessRules))
	}

	keySet := make(map[string]struct{}, len(keys))
	for _, k := range keys {
		keySet[k] = struct{}{}
	}
	for _, rule := range accessRules {
		key := rule.method + " " + rule.pattern
		if _, ok := keySet[key]; !ok {
			t.Errorf("policyKeys() missing key %q", key)
		}
	}
}

// --- Missing AuthContext (programming error) ---

func TestHandler_MissingAuthContext_Returns500(t *testing.T) {
	mw := NewMiddleware(testLogger())

	// Use a context WITHOUT AuthContext to simulate a misconfigured
	// middleware chain where auth runs after RBAC (should never happen).
	w := serveWithChi(t, mw, http.MethodGet, "/users/me", "/api/v1/users/me", context.Background())

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", w.Code, http.StatusInternalServerError)
	}
	resp := decodeErrorResponse(t, w)
	if resp.ErrorCode != string(model.ErrInternalError) {
		t.Errorf("error_code = %q, want %q", resp.ErrorCode, model.ErrInternalError)
	}
}

// --- Fail-open for unknown routes ---

func TestHandler_UnknownRoute_AllowsThrough(t *testing.T) {
	mw := NewMiddleware(testLogger())

	// Register a route that has no corresponding entry in accessPolicy.
	r := chi.NewRouter()
	r.Route("/api/v1", func(r chi.Router) {
		r.Use(mw.Handler())
		r.Get("/unknown/route", okHandler)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/unknown/route", nil)
	req = req.WithContext(ctxWithAuth(auth.RoleBusinessUser))
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d (fail-open for unknown routes)", w.Code, http.StatusOK)
	}
}

// --- Permission denied tests ---

func TestHandler_BusinessUser_DeniedOnDeleteContract(t *testing.T) {
	mw := NewMiddleware(testLogger())
	w := serveWithChi(t, mw, http.MethodDelete, "/contracts/{contract_id}", "/api/v1/contracts/abc-123", ctxWithAuth(auth.RoleBusinessUser))

	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d", w.Code, http.StatusForbidden)
	}
	resp := decodeErrorResponse(t, w)
	if resp.ErrorCode != string(model.ErrPermissionDenied) {
		t.Errorf("error_code = %q, want %q", resp.ErrorCode, model.ErrPermissionDenied)
	}
}

func TestHandler_BusinessUser_DeniedOnArchive(t *testing.T) {
	mw := NewMiddleware(testLogger())
	w := serveWithChi(t, mw, http.MethodPost, "/contracts/{contract_id}/archive", "/api/v1/contracts/abc-123/archive", ctxWithAuth(auth.RoleBusinessUser))

	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d", w.Code, http.StatusForbidden)
	}
}

func TestHandler_BusinessUser_DeniedOnRecheck(t *testing.T) {
	mw := NewMiddleware(testLogger())
	w := serveWithChi(t, mw, http.MethodPost, "/contracts/{contract_id}/versions/{version_id}/recheck", "/api/v1/contracts/abc/versions/v1/recheck", ctxWithAuth(auth.RoleBusinessUser))

	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d", w.Code, http.StatusForbidden)
	}
}

func TestHandler_BusinessUser_DeniedOnResults(t *testing.T) {
	mw := NewMiddleware(testLogger())
	w := serveWithChi(t, mw, http.MethodGet, "/contracts/{contract_id}/versions/{version_id}/results", "/api/v1/contracts/abc/versions/v1/results", ctxWithAuth(auth.RoleBusinessUser))

	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d", w.Code, http.StatusForbidden)
	}
}

func TestHandler_BusinessUser_DeniedOnRisks(t *testing.T) {
	mw := NewMiddleware(testLogger())
	w := serveWithChi(t, mw, http.MethodGet, "/contracts/{contract_id}/versions/{version_id}/risks", "/api/v1/contracts/abc/versions/v1/risks", ctxWithAuth(auth.RoleBusinessUser))

	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d", w.Code, http.StatusForbidden)
	}
}

func TestHandler_BusinessUser_DeniedOnRecommendations(t *testing.T) {
	mw := NewMiddleware(testLogger())
	w := serveWithChi(t, mw, http.MethodGet, "/contracts/{contract_id}/versions/{version_id}/recommendations", "/api/v1/contracts/abc/versions/v1/recommendations", ctxWithAuth(auth.RoleBusinessUser))

	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d", w.Code, http.StatusForbidden)
	}
}

func TestHandler_BusinessUser_DeniedOnCompare(t *testing.T) {
	mw := NewMiddleware(testLogger())
	w := serveWithChi(t, mw, http.MethodPost, "/contracts/{contract_id}/compare", "/api/v1/contracts/abc/compare", ctxWithAuth(auth.RoleBusinessUser))

	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d", w.Code, http.StatusForbidden)
	}
}

func TestHandler_BusinessUser_DeniedOnDiff(t *testing.T) {
	mw := NewMiddleware(testLogger())
	w := serveWithChi(t, mw, http.MethodGet, "/contracts/{contract_id}/versions/{base_version_id}/diff/{target_version_id}", "/api/v1/contracts/abc/versions/v1/diff/v2", ctxWithAuth(auth.RoleBusinessUser))

	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d", w.Code, http.StatusForbidden)
	}
}

func TestHandler_Lawyer_DeniedOnAdminPolicies(t *testing.T) {
	mw := NewMiddleware(testLogger())
	w := serveWithChi(t, mw, http.MethodGet, "/admin/policies", "/api/v1/admin/policies", ctxWithAuth(auth.RoleLawyer))

	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d", w.Code, http.StatusForbidden)
	}
}

func TestHandler_BusinessUser_DeniedOnAdminPolicies(t *testing.T) {
	mw := NewMiddleware(testLogger())
	w := serveWithChi(t, mw, http.MethodGet, "/admin/policies", "/api/v1/admin/policies", ctxWithAuth(auth.RoleBusinessUser))

	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d", w.Code, http.StatusForbidden)
	}
}

func TestHandler_Lawyer_DeniedOnAdminPoliciesUpdate(t *testing.T) {
	mw := NewMiddleware(testLogger())
	w := serveWithChi(t, mw, http.MethodPut, "/admin/policies/{policy_id}", "/api/v1/admin/policies/pol-1", ctxWithAuth(auth.RoleLawyer))

	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d", w.Code, http.StatusForbidden)
	}
}

func TestHandler_Lawyer_DeniedOnAdminChecklists(t *testing.T) {
	mw := NewMiddleware(testLogger())
	w := serveWithChi(t, mw, http.MethodGet, "/admin/checklists", "/api/v1/admin/checklists", ctxWithAuth(auth.RoleLawyer))

	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d", w.Code, http.StatusForbidden)
	}
}

func TestHandler_Lawyer_DeniedOnAdminChecklistsUpdate(t *testing.T) {
	mw := NewMiddleware(testLogger())
	w := serveWithChi(t, mw, http.MethodPut, "/admin/checklists/{checklist_id}", "/api/v1/admin/checklists/cl-1", ctxWithAuth(auth.RoleLawyer))

	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d", w.Code, http.StatusForbidden)
	}
}

// --- Allowed access tests ---

func TestHandler_AllRoles_AllowedOnUsersMe(t *testing.T) {
	mw := NewMiddleware(testLogger())

	for _, role := range []auth.Role{auth.RoleLawyer, auth.RoleBusinessUser, auth.RoleOrgAdmin} {
		t.Run(string(role), func(t *testing.T) {
			w := serveWithChi(t, mw, http.MethodGet, "/users/me", "/api/v1/users/me", ctxWithAuth(role))
			if w.Code != http.StatusOK {
				t.Errorf("status = %d, want %d for role %s", w.Code, http.StatusOK, role)
			}
		})
	}
}

func TestHandler_AllRoles_AllowedOnUpload(t *testing.T) {
	mw := NewMiddleware(testLogger())

	for _, role := range []auth.Role{auth.RoleLawyer, auth.RoleBusinessUser, auth.RoleOrgAdmin} {
		t.Run(string(role), func(t *testing.T) {
			w := serveWithChi(t, mw, http.MethodPost, "/contracts/upload", "/api/v1/contracts/upload", ctxWithAuth(role))
			if w.Code != http.StatusOK {
				t.Errorf("status = %d, want %d for role %s", w.Code, http.StatusOK, role)
			}
		})
	}
}

func TestHandler_AllRoles_AllowedOnListContracts(t *testing.T) {
	mw := NewMiddleware(testLogger())

	for _, role := range []auth.Role{auth.RoleLawyer, auth.RoleBusinessUser, auth.RoleOrgAdmin} {
		t.Run(string(role), func(t *testing.T) {
			w := serveWithChi(t, mw, http.MethodGet, "/contracts", "/api/v1/contracts", ctxWithAuth(role))
			if w.Code != http.StatusOK {
				t.Errorf("status = %d, want %d for role %s", w.Code, http.StatusOK, role)
			}
		})
	}
}

func TestHandler_AllRoles_AllowedOnSummary(t *testing.T) {
	mw := NewMiddleware(testLogger())

	for _, role := range []auth.Role{auth.RoleLawyer, auth.RoleBusinessUser, auth.RoleOrgAdmin} {
		t.Run(string(role), func(t *testing.T) {
			w := serveWithChi(t, mw, http.MethodGet, "/contracts/{contract_id}/versions/{version_id}/summary", "/api/v1/contracts/abc/versions/v1/summary", ctxWithAuth(role))
			if w.Code != http.StatusOK {
				t.Errorf("status = %d, want %d for role %s", w.Code, http.StatusOK, role)
			}
		})
	}
}

func TestHandler_AllRoles_AllowedOnExport(t *testing.T) {
	mw := NewMiddleware(testLogger())

	for _, role := range []auth.Role{auth.RoleLawyer, auth.RoleBusinessUser, auth.RoleOrgAdmin} {
		t.Run(string(role), func(t *testing.T) {
			w := serveWithChi(t, mw, http.MethodGet, "/contracts/{contract_id}/versions/{version_id}/export/{format}", "/api/v1/contracts/abc/versions/v1/export/pdf", ctxWithAuth(role))
			if w.Code != http.StatusOK {
				t.Errorf("status = %d, want %d for role %s", w.Code, http.StatusOK, role)
			}
		})
	}
}

func TestHandler_AllRoles_AllowedOnFeedback(t *testing.T) {
	mw := NewMiddleware(testLogger())

	for _, role := range []auth.Role{auth.RoleLawyer, auth.RoleBusinessUser, auth.RoleOrgAdmin} {
		t.Run(string(role), func(t *testing.T) {
			w := serveWithChi(t, mw, http.MethodPost, "/contracts/{contract_id}/versions/{version_id}/feedback", "/api/v1/contracts/abc/versions/v1/feedback", ctxWithAuth(role))
			if w.Code != http.StatusOK {
				t.Errorf("status = %d, want %d for role %s", w.Code, http.StatusOK, role)
			}
		})
	}
}

func TestHandler_Lawyer_AllowedOnDeleteContract(t *testing.T) {
	mw := NewMiddleware(testLogger())
	w := serveWithChi(t, mw, http.MethodDelete, "/contracts/{contract_id}", "/api/v1/contracts/abc-123", ctxWithAuth(auth.RoleLawyer))

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestHandler_OrgAdmin_AllowedOnDeleteContract(t *testing.T) {
	mw := NewMiddleware(testLogger())
	w := serveWithChi(t, mw, http.MethodDelete, "/contracts/{contract_id}", "/api/v1/contracts/abc-123", ctxWithAuth(auth.RoleOrgAdmin))

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestHandler_OrgAdmin_AllowedOnAdminPolicies(t *testing.T) {
	mw := NewMiddleware(testLogger())
	w := serveWithChi(t, mw, http.MethodGet, "/admin/policies", "/api/v1/admin/policies", ctxWithAuth(auth.RoleOrgAdmin))

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestHandler_OrgAdmin_AllowedOnAdminPoliciesUpdate(t *testing.T) {
	mw := NewMiddleware(testLogger())
	w := serveWithChi(t, mw, http.MethodPut, "/admin/policies/{policy_id}", "/api/v1/admin/policies/pol-1", ctxWithAuth(auth.RoleOrgAdmin))

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestHandler_OrgAdmin_AllowedOnAdminChecklists(t *testing.T) {
	mw := NewMiddleware(testLogger())
	w := serveWithChi(t, mw, http.MethodGet, "/admin/checklists", "/api/v1/admin/checklists", ctxWithAuth(auth.RoleOrgAdmin))

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestHandler_OrgAdmin_AllowedOnAdminChecklistsUpdate(t *testing.T) {
	mw := NewMiddleware(testLogger())
	w := serveWithChi(t, mw, http.MethodPut, "/admin/checklists/{checklist_id}", "/api/v1/admin/checklists/cl-1", ctxWithAuth(auth.RoleOrgAdmin))

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestHandler_Lawyer_AllowedOnResults(t *testing.T) {
	mw := NewMiddleware(testLogger())
	w := serveWithChi(t, mw, http.MethodGet, "/contracts/{contract_id}/versions/{version_id}/results", "/api/v1/contracts/abc/versions/v1/results", ctxWithAuth(auth.RoleLawyer))

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestHandler_OrgAdmin_AllowedOnResults(t *testing.T) {
	mw := NewMiddleware(testLogger())
	w := serveWithChi(t, mw, http.MethodGet, "/contracts/{contract_id}/versions/{version_id}/results", "/api/v1/contracts/abc/versions/v1/results", ctxWithAuth(auth.RoleOrgAdmin))

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestHandler_Lawyer_AllowedOnCompare(t *testing.T) {
	mw := NewMiddleware(testLogger())
	w := serveWithChi(t, mw, http.MethodPost, "/contracts/{contract_id}/compare", "/api/v1/contracts/abc/compare", ctxWithAuth(auth.RoleLawyer))

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestHandler_OrgAdmin_AllowedOnCompare(t *testing.T) {
	mw := NewMiddleware(testLogger())
	w := serveWithChi(t, mw, http.MethodPost, "/contracts/{contract_id}/compare", "/api/v1/contracts/abc/compare", ctxWithAuth(auth.RoleOrgAdmin))

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
}

// --- Error response format tests ---

func TestHandler_DeniedResponse_HasCorrectFormat(t *testing.T) {
	mw := NewMiddleware(testLogger())
	w := serveWithChi(t, mw, http.MethodGet, "/admin/policies", "/api/v1/admin/policies", ctxWithAuth(auth.RoleLawyer))

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusForbidden)
	}

	resp := decodeErrorResponse(t, w)
	if resp.ErrorCode != string(model.ErrPermissionDenied) {
		t.Errorf("error_code = %q, want %q", resp.ErrorCode, model.ErrPermissionDenied)
	}
	if resp.Message == "" {
		t.Error("message should not be empty")
	}
	if resp.Suggestion == "" {
		t.Error("suggestion should not be empty for PERMISSION_DENIED")
	}
}

func TestHandler_DeniedResponse_ContentTypeJSON(t *testing.T) {
	mw := NewMiddleware(testLogger())
	w := serveWithChi(t, mw, http.MethodGet, "/admin/policies", "/api/v1/admin/policies", ctxWithAuth(auth.RoleLawyer))

	ct := w.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
}

// --- roleSet unit tests ---

func TestRoleSet_Contains(t *testing.T) {
	s := newRoleSet(auth.RoleLawyer, auth.RoleOrgAdmin)

	if !s.contains(auth.RoleLawyer) {
		t.Error("should contain LAWYER")
	}
	if !s.contains(auth.RoleOrgAdmin) {
		t.Error("should contain ORG_ADMIN")
	}
	if s.contains(auth.RoleBusinessUser) {
		t.Error("should not contain BUSINESS_USER")
	}
}

func TestRoleSet_Empty(t *testing.T) {
	s := newRoleSet()
	if s.contains(auth.RoleLawyer) {
		t.Error("empty set should not contain any role")
	}
}

// --- Access policy completeness test ---

// TestAccessPolicy_AllRoutesCovered verifies that every protected route
// defined in routes.go has a corresponding entry in the access policy.
// This is a documentation-level test; it checks the policy map keys.
func TestAccessPolicy_AllRoutesCovered(t *testing.T) {
	// Expected policy keys derived from the access matrix in the task spec.
	// If a new route is added to routes.go, this test should be updated.
	expected := []string{
		"GET /api/v1/users/me",
		"POST /api/v1/contracts/upload",
		"GET /api/v1/contracts",
		"GET /api/v1/contracts/{contract_id}",
		"DELETE /api/v1/contracts/{contract_id}",
		"POST /api/v1/contracts/{contract_id}/archive",
		"GET /api/v1/contracts/{contract_id}/versions",
		"POST /api/v1/contracts/{contract_id}/versions/upload",
		"GET /api/v1/contracts/{contract_id}/versions/{version_id}",
		"GET /api/v1/contracts/{contract_id}/versions/{version_id}/status",
		"POST /api/v1/contracts/{contract_id}/versions/{version_id}/recheck",
		"POST /api/v1/contracts/{contract_id}/versions/{version_id}/confirm-type",
		"GET /api/v1/contracts/{contract_id}/versions/{version_id}/results",
		"GET /api/v1/contracts/{contract_id}/versions/{version_id}/risks",
		"GET /api/v1/contracts/{contract_id}/versions/{version_id}/summary",
		"GET /api/v1/contracts/{contract_id}/versions/{version_id}/recommendations",
		"POST /api/v1/contracts/{contract_id}/compare",
		"GET /api/v1/contracts/{contract_id}/versions/{base_version_id}/diff/{target_version_id}",
		"GET /api/v1/contracts/{contract_id}/versions/{version_id}/export/{format}",
		"POST /api/v1/contracts/{contract_id}/versions/{version_id}/feedback",
		"GET /api/v1/admin/policies",
		"PUT /api/v1/admin/policies/{policy_id}",
		"GET /api/v1/admin/checklists",
		"PUT /api/v1/admin/checklists/{checklist_id}",
	}

	sort.Strings(expected)

	mw := NewMiddleware(testLogger())
	actual := mw.policyKeys()
	sort.Strings(actual)

	if len(actual) != len(expected) {
		t.Errorf("policy has %d entries, want %d", len(actual), len(expected))
	}

	for i, key := range expected {
		if i >= len(actual) {
			t.Errorf("missing policy key: %q", key)
			continue
		}
		if actual[i] != key {
			t.Errorf("policy key [%d] = %q, want %q", i, actual[i], key)
		}
	}
}

// --- Comprehensive access matrix test ---

// TestAccessMatrix_FullCoverage tests every combination of role x endpoint
// from the access matrix to ensure correctness.
func TestAccessMatrix_FullCoverage(t *testing.T) {
	mw := NewMiddleware(testLogger())

	type testCase struct {
		name        string
		method      string
		chiPattern  string
		requestPath string
		role        auth.Role
		wantAllowed bool
	}

	tests := []testCase{
		// /users/me - all roles allowed
		{"LAWYER can GET /users/me", http.MethodGet, "/users/me", "/api/v1/users/me", auth.RoleLawyer, true},
		{"BUSINESS_USER can GET /users/me", http.MethodGet, "/users/me", "/api/v1/users/me", auth.RoleBusinessUser, true},
		{"ORG_ADMIN can GET /users/me", http.MethodGet, "/users/me", "/api/v1/users/me", auth.RoleOrgAdmin, true},

		// /contracts/upload - all roles allowed
		{"LAWYER can POST /contracts/upload", http.MethodPost, "/contracts/upload", "/api/v1/contracts/upload", auth.RoleLawyer, true},
		{"BUSINESS_USER can POST /contracts/upload", http.MethodPost, "/contracts/upload", "/api/v1/contracts/upload", auth.RoleBusinessUser, true},
		{"ORG_ADMIN can POST /contracts/upload", http.MethodPost, "/contracts/upload", "/api/v1/contracts/upload", auth.RoleOrgAdmin, true},

		// GET /contracts - all roles allowed
		{"LAWYER can GET /contracts", http.MethodGet, "/contracts", "/api/v1/contracts", auth.RoleLawyer, true},
		{"BUSINESS_USER can GET /contracts", http.MethodGet, "/contracts", "/api/v1/contracts", auth.RoleBusinessUser, true},
		{"ORG_ADMIN can GET /contracts", http.MethodGet, "/contracts", "/api/v1/contracts", auth.RoleOrgAdmin, true},

		// GET /contracts/{contract_id} - all roles allowed
		{"LAWYER can GET /contracts/{contract_id}", http.MethodGet, "/contracts/{contract_id}", "/api/v1/contracts/c1", auth.RoleLawyer, true},
		{"BUSINESS_USER can GET /contracts/{contract_id}", http.MethodGet, "/contracts/{contract_id}", "/api/v1/contracts/c1", auth.RoleBusinessUser, true},
		{"ORG_ADMIN can GET /contracts/{contract_id}", http.MethodGet, "/contracts/{contract_id}", "/api/v1/contracts/c1", auth.RoleOrgAdmin, true},

		// DELETE /contracts/{contract_id} - LAWYER+ORG_ADMIN only
		{"LAWYER can DELETE /contracts/{contract_id}", http.MethodDelete, "/contracts/{contract_id}", "/api/v1/contracts/c1", auth.RoleLawyer, true},
		{"BUSINESS_USER cannot DELETE /contracts/{contract_id}", http.MethodDelete, "/contracts/{contract_id}", "/api/v1/contracts/c1", auth.RoleBusinessUser, false},
		{"ORG_ADMIN can DELETE /contracts/{contract_id}", http.MethodDelete, "/contracts/{contract_id}", "/api/v1/contracts/c1", auth.RoleOrgAdmin, true},

		// POST /contracts/{contract_id}/archive - LAWYER+ORG_ADMIN only
		{"LAWYER can POST archive", http.MethodPost, "/contracts/{contract_id}/archive", "/api/v1/contracts/c1/archive", auth.RoleLawyer, true},
		{"BUSINESS_USER cannot POST archive", http.MethodPost, "/contracts/{contract_id}/archive", "/api/v1/contracts/c1/archive", auth.RoleBusinessUser, false},
		{"ORG_ADMIN can POST archive", http.MethodPost, "/contracts/{contract_id}/archive", "/api/v1/contracts/c1/archive", auth.RoleOrgAdmin, true},

		// GET /contracts/{contract_id}/versions - all roles allowed
		{"LAWYER can GET versions", http.MethodGet, "/contracts/{contract_id}/versions", "/api/v1/contracts/c1/versions", auth.RoleLawyer, true},
		{"BUSINESS_USER can GET versions", http.MethodGet, "/contracts/{contract_id}/versions", "/api/v1/contracts/c1/versions", auth.RoleBusinessUser, true},
		{"ORG_ADMIN can GET versions", http.MethodGet, "/contracts/{contract_id}/versions", "/api/v1/contracts/c1/versions", auth.RoleOrgAdmin, true},

		// POST /contracts/{contract_id}/versions/upload - all roles allowed
		{"LAWYER can POST versions/upload", http.MethodPost, "/contracts/{contract_id}/versions/upload", "/api/v1/contracts/c1/versions/upload", auth.RoleLawyer, true},
		{"BUSINESS_USER can POST versions/upload", http.MethodPost, "/contracts/{contract_id}/versions/upload", "/api/v1/contracts/c1/versions/upload", auth.RoleBusinessUser, true},
		{"ORG_ADMIN can POST versions/upload", http.MethodPost, "/contracts/{contract_id}/versions/upload", "/api/v1/contracts/c1/versions/upload", auth.RoleOrgAdmin, true},

		// GET /contracts/{contract_id}/versions/{version_id} - all roles allowed
		{"LAWYER can GET version detail", http.MethodGet, "/contracts/{contract_id}/versions/{version_id}", "/api/v1/contracts/c1/versions/v1", auth.RoleLawyer, true},
		{"BUSINESS_USER can GET version detail", http.MethodGet, "/contracts/{contract_id}/versions/{version_id}", "/api/v1/contracts/c1/versions/v1", auth.RoleBusinessUser, true},
		{"ORG_ADMIN can GET version detail", http.MethodGet, "/contracts/{contract_id}/versions/{version_id}", "/api/v1/contracts/c1/versions/v1", auth.RoleOrgAdmin, true},

		// GET /contracts/{contract_id}/versions/{version_id}/status - all roles allowed
		{"LAWYER can GET version status", http.MethodGet, "/contracts/{contract_id}/versions/{version_id}/status", "/api/v1/contracts/c1/versions/v1/status", auth.RoleLawyer, true},
		{"BUSINESS_USER can GET version status", http.MethodGet, "/contracts/{contract_id}/versions/{version_id}/status", "/api/v1/contracts/c1/versions/v1/status", auth.RoleBusinessUser, true},
		{"ORG_ADMIN can GET version status", http.MethodGet, "/contracts/{contract_id}/versions/{version_id}/status", "/api/v1/contracts/c1/versions/v1/status", auth.RoleOrgAdmin, true},

		// POST recheck - LAWYER+ORG_ADMIN only
		{"LAWYER can POST recheck", http.MethodPost, "/contracts/{contract_id}/versions/{version_id}/recheck", "/api/v1/contracts/c1/versions/v1/recheck", auth.RoleLawyer, true},
		{"BUSINESS_USER cannot POST recheck", http.MethodPost, "/contracts/{contract_id}/versions/{version_id}/recheck", "/api/v1/contracts/c1/versions/v1/recheck", auth.RoleBusinessUser, false},
		{"ORG_ADMIN can POST recheck", http.MethodPost, "/contracts/{contract_id}/versions/{version_id}/recheck", "/api/v1/contracts/c1/versions/v1/recheck", auth.RoleOrgAdmin, true},

		// GET results - LAWYER+ORG_ADMIN only
		{"LAWYER can GET results", http.MethodGet, "/contracts/{contract_id}/versions/{version_id}/results", "/api/v1/contracts/c1/versions/v1/results", auth.RoleLawyer, true},
		{"BUSINESS_USER cannot GET results", http.MethodGet, "/contracts/{contract_id}/versions/{version_id}/results", "/api/v1/contracts/c1/versions/v1/results", auth.RoleBusinessUser, false},
		{"ORG_ADMIN can GET results", http.MethodGet, "/contracts/{contract_id}/versions/{version_id}/results", "/api/v1/contracts/c1/versions/v1/results", auth.RoleOrgAdmin, true},

		// GET risks - LAWYER+ORG_ADMIN only
		{"LAWYER can GET risks", http.MethodGet, "/contracts/{contract_id}/versions/{version_id}/risks", "/api/v1/contracts/c1/versions/v1/risks", auth.RoleLawyer, true},
		{"BUSINESS_USER cannot GET risks", http.MethodGet, "/contracts/{contract_id}/versions/{version_id}/risks", "/api/v1/contracts/c1/versions/v1/risks", auth.RoleBusinessUser, false},
		{"ORG_ADMIN can GET risks", http.MethodGet, "/contracts/{contract_id}/versions/{version_id}/risks", "/api/v1/contracts/c1/versions/v1/risks", auth.RoleOrgAdmin, true},

		// GET recommendations - LAWYER+ORG_ADMIN only
		{"LAWYER can GET recommendations", http.MethodGet, "/contracts/{contract_id}/versions/{version_id}/recommendations", "/api/v1/contracts/c1/versions/v1/recommendations", auth.RoleLawyer, true},
		{"BUSINESS_USER cannot GET recommendations", http.MethodGet, "/contracts/{contract_id}/versions/{version_id}/recommendations", "/api/v1/contracts/c1/versions/v1/recommendations", auth.RoleBusinessUser, false},
		{"ORG_ADMIN can GET recommendations", http.MethodGet, "/contracts/{contract_id}/versions/{version_id}/recommendations", "/api/v1/contracts/c1/versions/v1/recommendations", auth.RoleOrgAdmin, true},

		// POST compare - LAWYER+ORG_ADMIN only
		{"LAWYER can POST compare", http.MethodPost, "/contracts/{contract_id}/compare", "/api/v1/contracts/c1/compare", auth.RoleLawyer, true},
		{"BUSINESS_USER cannot POST compare", http.MethodPost, "/contracts/{contract_id}/compare", "/api/v1/contracts/c1/compare", auth.RoleBusinessUser, false},
		{"ORG_ADMIN can POST compare", http.MethodPost, "/contracts/{contract_id}/compare", "/api/v1/contracts/c1/compare", auth.RoleOrgAdmin, true},

		// GET diff - LAWYER+ORG_ADMIN only
		{"LAWYER can GET diff", http.MethodGet, "/contracts/{contract_id}/versions/{base_version_id}/diff/{target_version_id}", "/api/v1/contracts/c1/versions/v1/diff/v2", auth.RoleLawyer, true},
		{"BUSINESS_USER cannot GET diff", http.MethodGet, "/contracts/{contract_id}/versions/{base_version_id}/diff/{target_version_id}", "/api/v1/contracts/c1/versions/v1/diff/v2", auth.RoleBusinessUser, false},
		{"ORG_ADMIN can GET diff", http.MethodGet, "/contracts/{contract_id}/versions/{base_version_id}/diff/{target_version_id}", "/api/v1/contracts/c1/versions/v1/diff/v2", auth.RoleOrgAdmin, true},

		// Admin endpoints - ORG_ADMIN only
		{"LAWYER cannot GET admin/policies", http.MethodGet, "/admin/policies", "/api/v1/admin/policies", auth.RoleLawyer, false},
		{"BUSINESS_USER cannot GET admin/policies", http.MethodGet, "/admin/policies", "/api/v1/admin/policies", auth.RoleBusinessUser, false},
		{"ORG_ADMIN can GET admin/policies", http.MethodGet, "/admin/policies", "/api/v1/admin/policies", auth.RoleOrgAdmin, true},

		{"LAWYER cannot PUT admin/policies/{policy_id}", http.MethodPut, "/admin/policies/{policy_id}", "/api/v1/admin/policies/p1", auth.RoleLawyer, false},
		{"BUSINESS_USER cannot PUT admin/policies/{policy_id}", http.MethodPut, "/admin/policies/{policy_id}", "/api/v1/admin/policies/p1", auth.RoleBusinessUser, false},
		{"ORG_ADMIN can PUT admin/policies/{policy_id}", http.MethodPut, "/admin/policies/{policy_id}", "/api/v1/admin/policies/p1", auth.RoleOrgAdmin, true},

		{"LAWYER cannot GET admin/checklists", http.MethodGet, "/admin/checklists", "/api/v1/admin/checklists", auth.RoleLawyer, false},
		{"BUSINESS_USER cannot GET admin/checklists", http.MethodGet, "/admin/checklists", "/api/v1/admin/checklists", auth.RoleBusinessUser, false},
		{"ORG_ADMIN can GET admin/checklists", http.MethodGet, "/admin/checklists", "/api/v1/admin/checklists", auth.RoleOrgAdmin, true},

		{"LAWYER cannot PUT admin/checklists/{checklist_id}", http.MethodPut, "/admin/checklists/{checklist_id}", "/api/v1/admin/checklists/c1", auth.RoleLawyer, false},
		{"BUSINESS_USER cannot PUT admin/checklists/{checklist_id}", http.MethodPut, "/admin/checklists/{checklist_id}", "/api/v1/admin/checklists/c1", auth.RoleBusinessUser, false},
		{"ORG_ADMIN can PUT admin/checklists/{checklist_id}", http.MethodPut, "/admin/checklists/{checklist_id}", "/api/v1/admin/checklists/c1", auth.RoleOrgAdmin, true},

		// GET summary - all roles
		{"LAWYER can GET summary", http.MethodGet, "/contracts/{contract_id}/versions/{version_id}/summary", "/api/v1/contracts/c1/versions/v1/summary", auth.RoleLawyer, true},
		{"BUSINESS_USER can GET summary", http.MethodGet, "/contracts/{contract_id}/versions/{version_id}/summary", "/api/v1/contracts/c1/versions/v1/summary", auth.RoleBusinessUser, true},
		{"ORG_ADMIN can GET summary", http.MethodGet, "/contracts/{contract_id}/versions/{version_id}/summary", "/api/v1/contracts/c1/versions/v1/summary", auth.RoleOrgAdmin, true},

		// GET export - all roles (policy check in handler)
		{"LAWYER can GET export", http.MethodGet, "/contracts/{contract_id}/versions/{version_id}/export/{format}", "/api/v1/contracts/c1/versions/v1/export/pdf", auth.RoleLawyer, true},
		{"BUSINESS_USER can GET export", http.MethodGet, "/contracts/{contract_id}/versions/{version_id}/export/{format}", "/api/v1/contracts/c1/versions/v1/export/pdf", auth.RoleBusinessUser, true},
		{"ORG_ADMIN can GET export", http.MethodGet, "/contracts/{contract_id}/versions/{version_id}/export/{format}", "/api/v1/contracts/c1/versions/v1/export/pdf", auth.RoleOrgAdmin, true},

		// POST feedback - all roles
		{"LAWYER can POST feedback", http.MethodPost, "/contracts/{contract_id}/versions/{version_id}/feedback", "/api/v1/contracts/c1/versions/v1/feedback", auth.RoleLawyer, true},
		{"BUSINESS_USER can POST feedback", http.MethodPost, "/contracts/{contract_id}/versions/{version_id}/feedback", "/api/v1/contracts/c1/versions/v1/feedback", auth.RoleBusinessUser, true},
		{"ORG_ADMIN can POST feedback", http.MethodPost, "/contracts/{contract_id}/versions/{version_id}/feedback", "/api/v1/contracts/c1/versions/v1/feedback", auth.RoleOrgAdmin, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := serveWithChi(t, mw, tt.method, tt.chiPattern, tt.requestPath, ctxWithAuth(tt.role))

			if tt.wantAllowed {
				if w.Code != http.StatusOK {
					t.Errorf("status = %d, want %d (should be allowed)", w.Code, http.StatusOK)
				}
			} else {
				if w.Code != http.StatusForbidden {
					t.Errorf("status = %d, want %d (should be denied)", w.Code, http.StatusForbidden)
				}
			}
		})
	}
}

// --- Next handler is NOT called on denial ---

func TestHandler_DeniedRequest_DoesNotCallNextHandler(t *testing.T) {
	mw := NewMiddleware(testLogger())

	called := false
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	r := chi.NewRouter()
	r.Route("/api/v1", func(r chi.Router) {
		r.Use(mw.Handler())
		r.Get("/admin/policies", handler)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/policies", nil)
	req = req.WithContext(ctxWithAuth(auth.RoleLawyer))
	r.ServeHTTP(w, req)

	if called {
		t.Error("next handler should not be called when access is denied")
	}
}

// --- Next handler IS called on allow ---

func TestHandler_AllowedRequest_CallsNextHandler(t *testing.T) {
	mw := NewMiddleware(testLogger())

	called := false
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	r := chi.NewRouter()
	r.Route("/api/v1", func(r chi.Router) {
		r.Use(mw.Handler())
		r.Get("/admin/policies", handler)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/policies", nil)
	req = req.WithContext(ctxWithAuth(auth.RoleOrgAdmin))
	r.ServeHTTP(w, req)

	if !called {
		t.Error("next handler should be called when access is allowed")
	}
}

// --- Unknown / forged role ---

func TestHandler_UnknownRole_DeniedOnProtectedRoute(t *testing.T) {
	mw := NewMiddleware(testLogger())
	// A forged or unrecognized role value must be denied on all restricted endpoints.
	w := serveWithChi(t, mw, http.MethodGet, "/admin/policies", "/api/v1/admin/policies", ctxWithAuth(auth.Role("SUPER_ADMIN")))

	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d for unknown role", w.Code, http.StatusForbidden)
	}
}

func TestHandler_EmptyRole_DeniedOnProtectedRoute(t *testing.T) {
	mw := NewMiddleware(testLogger())
	w := serveWithChi(t, mw, http.MethodDelete, "/contracts/{contract_id}", "/api/v1/contracts/c1", ctxWithAuth(auth.Role("")))

	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d for empty role", w.Code, http.StatusForbidden)
	}
}
