package api

import (
	"context"
	"net/http"
	"regexp"
)

// Header names for auth context extraction.
// In production, the API Gateway validates the JWT and forwards these headers.
const (
	HeaderOrganizationID = "X-Organization-ID"
	HeaderUserID         = "X-User-ID"
	HeaderUserRole       = "X-User-Role"
)

// authContextKey is an unexported type for context keys to prevent collisions.
type authContextKey struct{}

// AuthContext holds the authenticated caller identity extracted from request headers.
type AuthContext struct {
	OrganizationID string
	UserID         string
	Role           string
}

// authFromContext retrieves the AuthContext from the request context.
// Returns nil if no auth context is present.
func authFromContext(ctx context.Context) *AuthContext {
	ac, _ := ctx.Value(authContextKey{}).(*AuthContext)
	return ac
}

// identifierRe validates that a header value looks like a safe identifier:
// alphanumeric, hyphens, underscores, dots; max 128 chars.
var identifierRe = regexp.MustCompile(`^[a-zA-Z0-9._-]{1,128}$`)

// authMiddleware extracts organization_id and user_id from request headers
// and injects them into the request context. Returns 401 if required headers
// are missing. Returns 400 if header values are malformed (defense-in-depth).
func authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		orgID := r.Header.Get(HeaderOrganizationID)
		userID := r.Header.Get(HeaderUserID)

		if orgID == "" || userID == "" {
			writeErrorJSON(w, http.StatusUnauthorized, "UNAUTHORIZED", "missing required auth headers")
			return
		}

		if !identifierRe.MatchString(orgID) || !identifierRe.MatchString(userID) {
			writeErrorJSON(w, http.StatusBadRequest, "INVALID_AUTH", "malformed auth header value")
			return
		}

		role := r.Header.Get(HeaderUserRole)
		if role != "" && !identifierRe.MatchString(role) {
			writeErrorJSON(w, http.StatusBadRequest, "INVALID_AUTH", "malformed role header value")
			return
		}

		ac := &AuthContext{
			OrganizationID: orgID,
			UserID:         userID,
			Role:           role,
		}
		ctx := context.WithValue(r.Context(), authContextKey{}, ac)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// requireRole returns middleware that checks if the AuthContext contains one of
// the allowed roles. Returns 403 if the role is missing or not in the allowed
// set. This is a preparation for future UOM integration — currently the role is
// read from the X-User-Role header set by the API Gateway.
//
// Role comparison is case-sensitive by design: the API Gateway normalizes role
// values to lowercase before forwarding them via X-User-Role header.
func requireRole(allowedRoles ...string) func(http.Handler) http.Handler {
	allowed := make(map[string]struct{}, len(allowedRoles))
	for _, r := range allowedRoles {
		allowed[r] = struct{}{}
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ac := authFromContext(r.Context())
			if ac == nil || ac.Role == "" {
				writeErrorJSON(w, http.StatusForbidden, "FORBIDDEN", "insufficient permissions")
				return
			}
			if _, ok := allowed[ac.Role]; !ok {
				writeErrorJSON(w, http.StatusForbidden, "FORBIDDEN", "insufficient permissions")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
