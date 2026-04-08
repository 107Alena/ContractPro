package api

import "net/http"

// Middleware stubs and wiring for the HTTP server.
//
// Middleware lifecycle:
//
//   - corsMiddleware      -> ORCH-TASK-009 (stub)
//   - authMiddleware      -> ORCH-TASK-010 (implemented in internal/ingress/middleware/auth)
//   - rbacMiddleware      -> ORCH-TASK-011 (implemented in internal/ingress/middleware/rbac)
//   - rateLimitMiddleware -> ORCH-TASK-013 (stub)
//
// authMiddleware is wired at Server construction time via Deps.AuthMiddleware.
// rbacMiddleware is wired at Server construction time via Deps.RBACMiddleware.
// When nil (e.g., in tests that don't need auth/RBAC), they fall back to a
// no-op pass-through.

// corsMiddleware is a stub for CORS handling.
// Real implementation (ORCH-TASK-009) will read CORSConfig and set
// Access-Control-Allow-Origin, Access-Control-Allow-Methods,
// Access-Control-Allow-Headers, Access-Control-Max-Age headers.
func corsMiddleware(next http.Handler) http.Handler {
	return next
}

// rateLimitMiddleware is a stub for per-organization rate limiting.
// Real implementation (ORCH-TASK-013) will use Redis-backed token
// buckets with separate read/write RPS limits and return 429 Too Many
// Requests with a Retry-After header when exceeded.
func rateLimitMiddleware(next http.Handler) http.Handler {
	return next
}

// noopMiddleware is a pass-through middleware used as a fallback when the
// real auth middleware is not injected (e.g., in tests).
func noopMiddleware(next http.Handler) http.Handler {
	return next
}
