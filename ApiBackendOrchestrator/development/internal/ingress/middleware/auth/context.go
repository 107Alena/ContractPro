// Package auth provides JWT authentication middleware for the API/Backend
// Orchestrator.
//
// The middleware extracts the JWT from the Authorization header, validates
// signature and claims against a pre-loaded RSA or ECDSA public key, and
// injects AuthContext into the request context for downstream handlers and
// the RBAC middleware.
//
// This package does NOT handle SSE query-param authentication — that is the
// responsibility of the SSE handler itself (ORCH-TASK-029), which can reuse
// ValidateToken from this package.
package auth

import "context"

// Role represents a user role within the ContractPro system.
// Values are extracted from the JWT "role" claim.
type Role string

const (
	// RoleLawyer is the primary user role with full access to analysis results.
	RoleLawyer Role = "LAWYER"

	// RoleBusinessUser has limited access — only summaries and aggregate scores.
	RoleBusinessUser Role = "BUSINESS_USER"

	// RoleOrgAdmin has all LAWYER permissions plus organization administration.
	RoleOrgAdmin Role = "ORG_ADMIN"
)

// validRoles is the set of roles accepted by the JWT validation logic.
// Unexported to prevent runtime mutation; use IsValidRole for queries.
var validRoles = map[Role]struct{}{
	RoleLawyer:       {},
	RoleBusinessUser: {},
	RoleOrgAdmin:     {},
}

// IsValidRole returns true if r is one of the recognized roles.
func IsValidRole(r Role) bool {
	_, ok := validRoles[r]
	return ok
}

// AuthContext carries authenticated user information extracted from a
// validated JWT. It is stored in context.Context separately from
// logger.RequestContext so that the RBAC middleware can read the Role
// without depending on the logger package.
type AuthContext struct {
	// UserID is the subject claim (sub) — UUID of the authenticated user.
	UserID string

	// OrganizationID is the org claim — UUID of the user's organization.
	OrganizationID string

	// Role is the role claim — one of LAWYER, BUSINESS_USER, ORG_ADMIN.
	Role Role

	// TokenID is the jti claim — UUID of the token for audit logging.
	TokenID string
}

// authContextKey is an unexported type used as a context key to prevent
// collisions with keys from other packages.
type authContextKey struct{}

// WithAuthContext returns a new context carrying the given AuthContext.
func WithAuthContext(ctx context.Context, ac AuthContext) context.Context {
	return context.WithValue(ctx, authContextKey{}, ac)
}

// AuthContextFrom extracts the AuthContext from ctx.
// Returns a zero-value AuthContext and ok=false if none is present.
func AuthContextFrom(ctx context.Context) (AuthContext, bool) {
	ac, ok := ctx.Value(authContextKey{}).(AuthContext)
	return ac, ok
}
