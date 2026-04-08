package api

import (
	"fmt"
	"net/http"
	"runtime/debug"

	"contractpro/api-orchestrator/internal/domain/model"
	"contractpro/api-orchestrator/internal/infra/observability/logger"
)

// RecoveryMiddleware returns a middleware that catches panics in downstream
// handlers and converts them into 500 INTERNAL_ERROR JSON responses.
//
// The panic value and full stack trace are logged at ERROR level. The stack
// trace is never included in the HTTP response to avoid leaking internal
// details to clients (error-handling.md section 1.3).
//
// This middleware must be the first in the chain (before CORS, auth, etc.)
// so that panics in any middleware or handler are caught.
func RecoveryMiddleware(log *logger.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					// Let http.ErrAbortHandler propagate — the standard
					// server uses it to silently abort the response (e.g.
					// when a client disconnects during SSE streaming).
					if rec == http.ErrAbortHandler {
						panic(rec)
					}

					stack := debug.Stack()

					log.Error(r.Context(), "panic recovered in HTTP handler",
						"panic", fmt.Sprintf("%v", rec),
						"stack", string(stack),
						"method", r.Method,
						"path", r.URL.Path,
					)

					model.WriteError(w, r, model.ErrInternalError, nil)
				}
			}()

			next.ServeHTTP(w, r)
		})
	}
}
