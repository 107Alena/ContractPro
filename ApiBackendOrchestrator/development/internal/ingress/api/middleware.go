package api

import "net/http"

// Middleware stubs for the HTTP server.
//
// Each middleware is a no-op pass-through that will be replaced with a
// real implementation in a subsequent task:
//
//   - corsMiddleware      -> ORCH-TASK-009
//   - authMiddleware      -> ORCH-TASK-010
//   - rbacMiddleware      -> ORCH-TASK-011
//   - rateLimitMiddleware -> ORCH-TASK-013

// corsMiddleware is a stub for CORS handling.
// Real implementation (ORCH-TASK-009) will read CORSConfig and set
// Access-Control-Allow-Origin, Access-Control-Allow-Methods,
// Access-Control-Allow-Headers, Access-Control-Max-Age headers.
func corsMiddleware(next http.Handler) http.Handler {
	return next
}

// authMiddleware is a stub for JWT authentication.
// Real implementation (ORCH-TASK-010) will extract the Bearer token,
// validate the JWT signature and claims, and inject AuthContext into
// the request context.
func authMiddleware(next http.Handler) http.Handler {
	return next
}

// rbacMiddleware is a stub for role-based access control.
// Real implementation (ORCH-TASK-011) will check the user's role
// against the endpoint's required permissions and return 403 Forbidden
// if insufficient.
func rbacMiddleware(next http.Handler) http.Handler {
	return next
}

// rateLimitMiddleware is a stub for per-organization rate limiting.
// Real implementation (ORCH-TASK-013) will use Redis-backed token
// buckets with separate read/write RPS limits and return 429 Too Many
// Requests with a Retry-After header when exceeded.
func rateLimitMiddleware(next http.Handler) http.Handler {
	return next
}
