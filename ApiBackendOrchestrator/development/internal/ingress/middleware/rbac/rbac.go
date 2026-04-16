// Package rbac provides role-based access control middleware for the
// API/Backend Orchestrator.
//
// The middleware runs after JWT authentication and reads the AuthContext
// injected by the auth middleware to check the user's role against the
// endpoint's allowed roles. If the role is not permitted, it returns
// 403 PERMISSION_DENIED via the unified error format.
//
// Access rules are defined as a map keyed by "METHOD /api/v1/..." route
// pattern. A companion chi.Mux is built at construction time and used
// solely for pattern matching via Mux.Match(). This avoids the limitation
// where chi's RouteContext().RoutePattern() is not yet fully resolved
// inside middleware registered on a sub-router created by Router.Route().
//
// Routes with no explicit rule are allowed (fail-open) to support forward
// compatibility when new endpoints are added before RBAC rules are updated.
//
// Thread safety: both the access policy map and the matching mux are built
// once at construction time and are never mutated, so concurrent reads
// from multiple goroutines are safe.
package rbac

import (
	"net/http"

	"contractpro/api-orchestrator/internal/domain/model"
	"contractpro/api-orchestrator/internal/infra/observability/logger"
	"contractpro/api-orchestrator/internal/ingress/middleware/auth"

	"github.com/go-chi/chi/v5"
)

// roleSet is an immutable set of roles allowed for a specific endpoint.
// Using a map[auth.Role]struct{} for O(1) lookup instead of a slice.
type roleSet map[auth.Role]struct{}

// newRoleSet builds a roleSet from the given roles.
func newRoleSet(roles ...auth.Role) roleSet {
	s := make(roleSet, len(roles))
	for _, r := range roles {
		s[r] = struct{}{}
	}
	return s
}

// contains returns true if the set contains the given role.
func (s roleSet) contains(r auth.Role) bool {
	_, ok := s[r]
	return ok
}

// allRoles is a convenience set containing every recognized role.
// Used for endpoints accessible to all authenticated users.
var allRoles = newRoleSet(auth.RoleLawyer, auth.RoleBusinessUser, auth.RoleOrgAdmin)

// lawyerAndAdmin is a convenience set for endpoints restricted to
// LAWYER and ORG_ADMIN roles.
var lawyerAndAdmin = newRoleSet(auth.RoleLawyer, auth.RoleOrgAdmin)

// adminOnly is a convenience set for endpoints restricted to ORG_ADMIN.
var adminOnly = newRoleSet(auth.RoleOrgAdmin)

// policyRule defines an access rule for a single endpoint.
type policyRule struct {
	method  string
	pattern string
	allowed roleSet
}

// accessRules is the list of all RBAC rules derived from the access matrix
// in security.md. Each rule specifies an HTTP method, route pattern (matching
// chi's pattern syntax), and the set of roles allowed to call that endpoint.
//
// The route patterns use the same parameter names as routes.go:
//   - {contract_id}, {version_id}, {base_version_id}, {target_version_id}
//   - {format}, {policy_id}, {checklist_id}
//
// The /api/v1 prefix is included because chi's Route("/api/v1", ...) nesting
// produces patterns with the full path.
var accessRules = []policyRule{
	// User profile.
	{http.MethodGet, "/api/v1/users/me", allRoles},

	// Contracts.
	{http.MethodPost, "/api/v1/contracts/upload", allRoles},
	{http.MethodGet, "/api/v1/contracts", allRoles},
	{http.MethodGet, "/api/v1/contracts/{contract_id}", allRoles},
	{http.MethodDelete, "/api/v1/contracts/{contract_id}", lawyerAndAdmin},
	{http.MethodPost, "/api/v1/contracts/{contract_id}/archive", lawyerAndAdmin},

	// Versions.
	{http.MethodGet, "/api/v1/contracts/{contract_id}/versions", allRoles},
	{http.MethodPost, "/api/v1/contracts/{contract_id}/versions/upload", allRoles},
	{http.MethodGet, "/api/v1/contracts/{contract_id}/versions/{version_id}", allRoles},
	{http.MethodGet, "/api/v1/contracts/{contract_id}/versions/{version_id}/status", allRoles},
	{http.MethodPost, "/api/v1/contracts/{contract_id}/versions/{version_id}/recheck", lawyerAndAdmin},
	{http.MethodPost, "/api/v1/contracts/{contract_id}/versions/{version_id}/confirm-type", lawyerAndAdmin},

	// Results.
	{http.MethodGet, "/api/v1/contracts/{contract_id}/versions/{version_id}/results", lawyerAndAdmin},
	{http.MethodGet, "/api/v1/contracts/{contract_id}/versions/{version_id}/risks", lawyerAndAdmin},
	{http.MethodGet, "/api/v1/contracts/{contract_id}/versions/{version_id}/summary", allRoles},
	{http.MethodGet, "/api/v1/contracts/{contract_id}/versions/{version_id}/recommendations", lawyerAndAdmin},

	// Comparison.
	{http.MethodPost, "/api/v1/contracts/{contract_id}/compare", lawyerAndAdmin},
	{http.MethodGet, "/api/v1/contracts/{contract_id}/versions/{base_version_id}/diff/{target_version_id}", lawyerAndAdmin},

	// Export: all roles allowed at the RBAC level. BUSINESS_USER access is
	// further restricted by OPM policy check inside the handler (security.md §2.2).
	{http.MethodGet, "/api/v1/contracts/{contract_id}/versions/{version_id}/export/{format}", allRoles},

	// Feedback.
	{http.MethodPost, "/api/v1/contracts/{contract_id}/versions/{version_id}/feedback", allRoles},

	// Admin.
	{http.MethodGet, "/api/v1/admin/policies", adminOnly},
	{http.MethodPut, "/api/v1/admin/policies/{policy_id}", adminOnly},
	{http.MethodGet, "/api/v1/admin/checklists", adminOnly},
	{http.MethodPut, "/api/v1/admin/checklists/{checklist_id}", adminOnly},
}

// Middleware implements RBAC authorization for the API/Backend Orchestrator.
// It checks the authenticated user's role (from AuthContext in context.Context)
// against the access policy for the matched route pattern.
type Middleware struct {
	// policy maps "METHOD /pattern" to the set of allowed roles.
	policy map[string]roleSet

	// matcher is a chi.Mux used solely for route pattern matching.
	// It is built once at construction time from the accessRules and
	// never used for actual HTTP serving. Mux.Match() resolves the
	// full route pattern for a given method+path, which we then use
	// as the key into the policy map.
	matcher *chi.Mux

	log *logger.Logger
}

// noopHandler is a placeholder handler registered on the matcher mux.
// It is never called; the mux is only used for Match().
var noopHandler = http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})

// NewMiddleware constructs an RBAC Middleware.
//
// The logger is used to emit WARN-level messages when access is denied,
// including the user_id, role, HTTP method, and request path for audit
// purposes.
//
// At construction time, the middleware:
//  1. Builds a policy map from accessRules (immutable after construction).
//  2. Builds a chi.Mux with all known route patterns for path matching.
//
// Both structures are safe for concurrent use from multiple goroutines.
func NewMiddleware(log *logger.Logger) *Middleware {
	policy := make(map[string]roleSet, len(accessRules))
	matcher := chi.NewMux()

	for _, rule := range accessRules {
		key := rule.method + " " + rule.pattern
		policy[key] = rule.allowed

		// Register the route on the matcher mux. Since chi.Mux does not
		// have a generic MethodFunc that avoids duplicate registration for
		// the same pattern with different methods, we register per-method.
		matcher.MethodFunc(rule.method, rule.pattern, noopHandler)
	}

	return &Middleware{
		policy:  policy,
		matcher: matcher,
		log:     log.With("component", "rbac-middleware"),
	}
}

// Handler returns a chi-compatible middleware function.
//
// The middleware:
//  1. Extracts AuthContext from context (injected by auth middleware).
//  2. Uses the internal matcher mux to resolve the full route pattern
//     for the request's method and path.
//  3. Looks up the access policy for "METHOD /pattern".
//  4. If no match or no rule exists, allows the request (fail-open).
//  5. If the user's role is not in the allowed set, returns 403.
//  6. Otherwise, passes the request to the next handler.
//
// Note on route pattern resolution: chi's RouteContext().RoutePattern()
// is not fully resolved inside middleware registered on sub-routers
// (e.g., via Router.Route("/api/v1", ...)). The internal matcher mux
// sidesteps this by performing an independent route match using
// chi.Mux.Match(), which always returns the full pattern.
func (m *Middleware) Handler() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Step 1: Extract AuthContext. If missing, this is a programming
			// error -- the auth middleware should always run before RBAC.
			ac, ok := auth.AuthContextFrom(r.Context())
			if !ok {
				m.log.Error(r.Context(), "RBAC: AuthContext not found in context; auth middleware may not be configured",
					"method", r.Method,
					"path", r.URL.Path,
				)
				model.WriteError(w, r, model.ErrInternalError, nil)
				return
			}

			// Step 2: Resolve the route pattern using the matcher mux.
			// chi.NewRouteContext() creates a fresh context for matching
			// without affecting the request's existing route context.
			rctx := chi.NewRouteContext()
			if !m.matcher.Match(rctx, r.Method, r.URL.Path) {
				// No matching route in our policy rules. Fail-open:
				// allow the request through. This supports forward
				// compatibility -- new routes can be added before RBAC
				// rules are updated. The endpoint is still protected
				// by authentication (JWT) even without an explicit
				// RBAC rule.
				m.log.Debug(r.Context(), "RBAC: no matching policy rule, allowing (fail-open)",
					"method", r.Method,
					"path", r.URL.Path,
				)
				next.ServeHTTP(w, r)
				return
			}

			routePattern := rctx.RoutePattern()

			// Step 3: Build the policy key and look up access rules.
			policyKey := r.Method + " " + routePattern

			allowed, exists := m.policy[policyKey]
			if !exists {
				// Matched the route pattern but no policy entry. This
				// should not happen if accessRules and the matcher are
				// in sync, but fail-open as a safety net.
				next.ServeHTTP(w, r)
				return
			}

			// Step 4: Check if the user's role is permitted.
			if !allowed.contains(ac.Role) {
				m.log.Warn(r.Context(), "RBAC: access denied",
					"user_id", ac.UserID,
					"role", string(ac.Role),
					"method", r.Method,
					"path", r.URL.Path,
					"route_pattern", routePattern,
				)
				model.WriteError(w, r, model.ErrPermissionDenied, nil)
				return
			}

			// Step 5: Role is permitted -- proceed to the next handler.
			next.ServeHTTP(w, r)
		})
	}
}

// policyKeys returns the list of all policy keys ("METHOD /pattern") in
// the middleware's access policy. Used by tests to verify that all
// registered routes have corresponding RBAC rules.
func (m *Middleware) policyKeys() []string {
	keys := make([]string, 0, len(m.policy))
	for k := range m.policy {
		keys = append(keys, k)
	}
	return keys
}
