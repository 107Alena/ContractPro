package api

import (
	"net/http"

	"contractpro/api-orchestrator/internal/application/authproxy"
	"contractpro/api-orchestrator/internal/application/comparison"
	"contractpro/api-orchestrator/internal/application/contracts"
	"contractpro/api-orchestrator/internal/application/results"
	"contractpro/api-orchestrator/internal/application/versions"
	"contractpro/api-orchestrator/internal/ingress/sse"

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
// Handler parameters:
//   - authMW: JWT authentication middleware (nil → no-op pass-through)
//   - rbacMW: RBAC middleware (nil → no-op pass-through)
//   - uploadH: contract upload handler (nil → 501 Not Implemented)
//   - contractH: contract CRUD handler (nil → 501 Not Implemented stubs)
//   - versionH: version management handler (nil → 501 Not Implemented stubs)
func registerRoutes(r chi.Router, authMW, rbacMW func(http.Handler) http.Handler, uploadH http.HandlerFunc, authH *authproxy.Handler, contractH *contracts.Handler, versionH *versions.Handler, resultsH *results.Handler, comparisonH *comparison.Handler, sseH *sse.Handler) {
	r.Route("/api/v1", func(r chi.Router) {
		// --- Public routes (no auth required) ---
		r.Group(func(r chi.Router) {
			if authH != nil {
				r.Post("/auth/login", authH.HandleLogin())
				r.Post("/auth/refresh", authH.HandleRefresh())
				r.Post("/auth/logout", authH.HandleLogout())
			} else {
				r.Post("/auth/login", notImplemented)
				r.Post("/auth/refresh", notImplemented)
				r.Post("/auth/logout", notImplemented)
			}
		})

		// --- Protected routes (auth + RBAC + rate limiting) ---
		r.Group(func(r chi.Router) {
			// Auth middleware: real JWT validation from ORCH-TASK-010.
			r.Use(authMW)
			// RBAC middleware: role-based access control from ORCH-TASK-011.
			r.Use(rbacMW)
			r.Use(rateLimitMiddleware)

			// User profile.
			if authH != nil {
				r.Get("/users/me", authH.HandleGetMe())
			} else {
				r.Get("/users/me", notImplemented)
			}

			// Contracts.
			r.Post("/contracts/upload", uploadH)
			if contractH != nil {
				r.Get("/contracts", contractH.HandleList())
				r.Get("/contracts/{contract_id}", contractH.HandleGet())
				r.Delete("/contracts/{contract_id}", contractH.HandleDelete())
				r.Post("/contracts/{contract_id}/archive", contractH.HandleArchive())
			} else {
				r.Get("/contracts", notImplemented)
				r.Get("/contracts/{contract_id}", notImplemented)
				r.Delete("/contracts/{contract_id}", notImplemented)
				r.Post("/contracts/{contract_id}/archive", notImplemented)
			}

			// Versions.
			if versionH != nil {
				r.Get("/contracts/{contract_id}/versions", versionH.HandleList())
				r.Post("/contracts/{contract_id}/versions/upload", versionH.HandleUpload())
				r.Get("/contracts/{contract_id}/versions/{version_id}", versionH.HandleGet())
				r.Get("/contracts/{contract_id}/versions/{version_id}/status", versionH.HandleStatus())
				r.Post("/contracts/{contract_id}/versions/{version_id}/recheck", versionH.HandleRecheck())
			} else {
				r.Get("/contracts/{contract_id}/versions", notImplemented)
				r.Post("/contracts/{contract_id}/versions/upload", notImplemented)
				r.Get("/contracts/{contract_id}/versions/{version_id}", notImplemented)
				r.Get("/contracts/{contract_id}/versions/{version_id}/status", notImplemented)
				r.Post("/contracts/{contract_id}/versions/{version_id}/recheck", notImplemented)
			}

			// Results.
			if resultsH != nil {
				r.Get("/contracts/{contract_id}/versions/{version_id}/results", resultsH.HandleResults())
				r.Get("/contracts/{contract_id}/versions/{version_id}/risks", resultsH.HandleRisks())
				r.Get("/contracts/{contract_id}/versions/{version_id}/summary", resultsH.HandleSummary())
				r.Get("/contracts/{contract_id}/versions/{version_id}/recommendations", resultsH.HandleRecommendations())
			} else {
				r.Get("/contracts/{contract_id}/versions/{version_id}/results", notImplemented)
				r.Get("/contracts/{contract_id}/versions/{version_id}/risks", notImplemented)
				r.Get("/contracts/{contract_id}/versions/{version_id}/summary", notImplemented)
				r.Get("/contracts/{contract_id}/versions/{version_id}/recommendations", notImplemented)
			}

			// Comparison.
			if comparisonH != nil {
				r.Post("/contracts/{contract_id}/compare", comparisonH.HandleCompare())
				r.Get("/contracts/{contract_id}/versions/{base_version_id}/diff/{target_version_id}", comparisonH.HandleGetDiff())
			} else {
				r.Post("/contracts/{contract_id}/compare", notImplemented)
				r.Get("/contracts/{contract_id}/versions/{base_version_id}/diff/{target_version_id}", notImplemented)
			}

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
		// middleware group. Auth is handled inside the SSE handler itself
		// via the "token" query parameter.
		if sseH != nil {
			r.Get("/events/stream", sseH.Handle())
		} else {
			r.Get("/events/stream", sseStub)
		}
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
