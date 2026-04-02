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
