package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

// registerRoutes sets up all API routes on the given chi router.
//
// Routes are organized into two groups:
//
//  1. Public routes: accessible without authentication. Only CORS
//     middleware is applied (inherited from the root router).
//
//  2. Protected routes: require authentication. The full middleware
//     chain (Auth -> RBAC -> RateLimit) is applied via Group().
//
// All handlers are stubs returning 501 Not Implemented.
// Real handlers will be registered in ORCH-TASK-018+.
//
// The authMW parameter is the JWT authentication middleware. When nil,
// a no-op pass-through is used (for tests that don't need auth).
func registerRoutes(r chi.Router, authMW func(http.Handler) http.Handler) {
	r.Route("/api/v1", func(r chi.Router) {
		// --- Public routes (no auth required) ---
		r.Group(func(r chi.Router) {
			r.Post("/auth/login", notImplemented)
			r.Post("/auth/refresh", notImplemented)
			r.Post("/auth/logout", notImplemented)
		})

		// --- Protected routes (auth + RBAC + rate limiting) ---
		r.Group(func(r chi.Router) {
			// Auth middleware: real JWT validation from ORCH-TASK-010.
			r.Use(authMW)
			r.Use(rbacMiddleware)
			r.Use(rateLimitMiddleware)

			// User profile.
			r.Get("/users/me", notImplemented)

			// Contracts.
			r.Post("/contracts/upload", notImplemented)
			r.Get("/contracts", notImplemented)
			r.Get("/contracts/{contract_id}", notImplemented)
			r.Delete("/contracts/{contract_id}", notImplemented)
			r.Post("/contracts/{contract_id}/archive", notImplemented)

			// Versions.
			r.Get("/contracts/{contract_id}/versions", notImplemented)
			r.Post("/contracts/{contract_id}/versions/upload", notImplemented)
			r.Get("/contracts/{contract_id}/versions/{version_id}", notImplemented)
			r.Get("/contracts/{contract_id}/versions/{version_id}/status", notImplemented)
			r.Post("/contracts/{contract_id}/versions/{version_id}/recheck", notImplemented)

			// Results.
			r.Get("/contracts/{contract_id}/versions/{version_id}/results", notImplemented)
			r.Get("/contracts/{contract_id}/versions/{version_id}/risks", notImplemented)
			r.Get("/contracts/{contract_id}/versions/{version_id}/summary", notImplemented)
			r.Get("/contracts/{contract_id}/versions/{version_id}/recommendations", notImplemented)

			// Comparison.
			r.Post("/contracts/{contract_id}/compare", notImplemented)
			r.Get("/contracts/{contract_id}/versions/{base_version_id}/diff/{target_version_id}", notImplemented)

			// Export.
			r.Get("/contracts/{contract_id}/versions/{version_id}/export/{format}", notImplemented)

			// Feedback.
			r.Post("/contracts/{contract_id}/versions/{version_id}/feedback", notImplemented)

			// Admin.
			r.Get("/admin/policies", notImplemented)
			r.Put("/admin/policies/{policy_id}", notImplemented)
			r.Get("/admin/checklists", notImplemented)
			r.Put("/admin/checklists/{checklist_id}", notImplemented)
		})

		// --- SSE endpoint ---
		// SSE uses query-param authentication (EventSource API does not
		// support custom headers), so it sits outside the standard auth
		// middleware group. Auth will be handled inside the SSE handler
		// itself (ORCH-TASK-029).
		r.Get("/events/stream", sseStub)
	})
}

// sseStub is a placeholder for the SSE endpoint.
// Real implementation will be added in ORCH-TASK-029.
func sseStub(w http.ResponseWriter, _ *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)

	// Send a single comment (heartbeat-style) and close.
	// Real implementation will hold the connection open.
	_, _ = w.Write([]byte(": connected\n\n"))
	flusher.Flush()
}
