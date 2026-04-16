package api

import (
	"context"
	"net/http"
	"strconv"

	"contractpro/api-orchestrator/internal/config"
	"contractpro/api-orchestrator/internal/infra/observability/logger"

	"github.com/google/uuid"
)

// Middleware lifecycle:
//
//   - CORSMiddleware         -> ORCH-TASK-012 (implemented)
//   - SecurityHeadersMiddleware -> ORCH-TASK-012 (implemented)
//   - authMiddleware          -> ORCH-TASK-010 (implemented in internal/ingress/middleware/auth)
//   - rbacMiddleware          -> ORCH-TASK-011 (implemented in internal/ingress/middleware/rbac)
//   - rateLimitMiddleware     -> ORCH-TASK-013 (implemented in internal/ingress/middleware/ratelimit)
//
// authMiddleware is wired at Server construction time via Deps.AuthMiddleware.
// rbacMiddleware is wired at Server construction time via Deps.RBACMiddleware.
// When nil (e.g., in tests that don't need auth/RBAC), they fall back to a
// no-op pass-through.

const (
	// correlationIDHeader is the canonical correlation ID header name.
	correlationIDHeader = "X-Correlation-Id"

	// requestIDHeader is the canonical request ID header name.
	requestIDHeader = "X-Request-Id"

	// corsAllowMethods lists the HTTP methods allowed for cross-origin requests.
	corsAllowMethods = "GET, POST, PUT, PATCH, DELETE, OPTIONS"

	// corsAllowHeaders lists the request headers allowed for cross-origin requests.
	// traceparent / tracestate are included for W3C Trace Context propagation
	// from frontend OpenTelemetry instrumentation (see security.md §5.1).
	corsAllowHeaders = "Authorization, Content-Type, X-Correlation-Id, traceparent, tracestate"

	// corsExposeHeaders lists the response headers exposed to cross-origin callers.
	// traceparent is exposed so frontend can read the backend-assigned trace.
	corsExposeHeaders = "X-Request-Id, Retry-After, traceparent"
)

// CORSMiddleware returns a chi-compatible middleware that handles
// Cross-Origin Resource Sharing (CORS) per architecture/security.md §5.
//
// Behavior:
//   - If cfg.AllowedOrigins is empty, only same-origin requests are permitted
//     (no CORS headers are ever set).
//   - Wildcard "*" origins are silently ignored because they are incompatible
//     with Access-Control-Allow-Credentials: true.
//   - Preflight requests (OPTIONS with Access-Control-Request-Method) receive
//     204 No Content. CORS headers are set only when the Origin is allowed.
//   - Actual cross-origin requests from allowed origins receive the appropriate
//     CORS response headers. Disallowed origins receive no CORS headers;
//     the browser enforces the block.
//   - Vary: Origin is always included when an Origin header is present to
//     ensure correct HTTP cache behaviour.
func CORSMiddleware(cfg config.CORSConfig, log *logger.Logger) func(http.Handler) http.Handler {
	// Build allowed-origins set at construction time for O(1) lookup.
	// Wildcard "*" is silently filtered here as defence-in-depth — config.Validate
	// is the primary gate and will fail startup before we reach this code.
	allowed := make(map[string]struct{}, len(cfg.AllowedOrigins))
	for _, o := range cfg.AllowedOrigins {
		if o == "*" {
			log.Warn(context.Background(),
				"CORS wildcard '*' origin ignored (incompatible with credentials mode)")
			continue
		}
		if o != "" {
			allowed[o] = struct{}{}
		}
	}

	// Same-origin deployment (ADR-6): when no origins are configured the
	// middleware is a pass-through. OPTIONS is routed normally (to handler /
	// 404 / 405) so the behaviour matches every other method.
	if len(allowed) == 0 {
		return func(next http.Handler) http.Handler { return next }
	}

	maxAge := strconv.Itoa(cfg.MaxAge)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")

			// No Origin header → same-origin request; skip CORS entirely.
			if origin == "" {
				next.ServeHTTP(w, r)
				return
			}

			_, ok := allowed[origin]

			// Preflight: OPTIONS with Access-Control-Request-Method.
			if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
				if ok {
					setCORSHeaders(w, origin, maxAge)
				}
				w.Header().Set("Vary", "Origin, Access-Control-Request-Method, Access-Control-Request-Headers")
				w.WriteHeader(http.StatusNoContent)
				return
			}

			// Actual request: set CORS headers only for allowed origins.
			w.Header().Add("Vary", "Origin")
			if ok {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Credentials", "true")
				w.Header().Set("Access-Control-Expose-Headers", corsExposeHeaders)
			}

			next.ServeHTTP(w, r)
		})
	}
}

// setCORSHeaders writes the full set of CORS response headers for a
// preflight response with an allowed origin.
func setCORSHeaders(w http.ResponseWriter, origin, maxAge string) {
	h := w.Header()
	h.Set("Access-Control-Allow-Origin", origin)
	h.Set("Access-Control-Allow-Methods", corsAllowMethods)
	h.Set("Access-Control-Allow-Headers", corsAllowHeaders)
	h.Set("Access-Control-Expose-Headers", corsExposeHeaders)
	h.Set("Access-Control-Max-Age", maxAge)
	h.Set("Access-Control-Allow-Credentials", "true")
}

// SecurityHeadersMiddleware returns a chi-compatible middleware that sets
// security response headers on every request per architecture/security.md §9.
//
// Headers set:
//   - X-Content-Type-Options: nosniff  — prevents MIME-type sniffing
//   - X-Frame-Options: DENY            — prevents iframe embedding (clickjacking)
//   - Strict-Transport-Security        — forces HTTPS (defense-in-depth)
//   - Cache-Control: no-store          — prevents caching of sensitive data
//   - X-Request-Id: {correlation_id}   — request identifier for log correlation
//   - X-Correlation-Id: {correlation_id} — same value for service-to-service tracing
//
// The middleware also generates a UUID v4 correlation ID when the client does
// not provide an X-Correlation-Id request header, and propagates it:
//   - On the request header (for downstream middleware such as auth)
//   - Into the request context via logger.RequestContext (for logging and
//     error responses on public endpoints that bypass auth)
//
// Handlers that need different Cache-Control (e.g., SSE text/event-stream,
// health probes) can override by calling w.Header().Set after this middleware.
func SecurityHeadersMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Correlation ID: read from client or generate.
			// Validate client-provided values to prevent log pollution
			// and header injection (Go sanitizes CRLF, but we also
			// enforce length and printable-ASCII constraints).
			cid := r.Header.Get(correlationIDHeader)
			if cid == "" || !isValidCorrelationID(cid) {
				cid = uuid.New().String()
			}
			// Propagate on the request header so that downstream middleware
			// (auth, authproxy) can read it without generating a new one.
			r.Header.Set(correlationIDHeader, cid)

			// Set all security + correlation headers before calling next.
			// These are set before WriteHeader so they survive even if a
			// panic triggers the recovery middleware.
			h := w.Header()
			h.Set(requestIDHeader, cid)
			h.Set(correlationIDHeader, cid)
			h.Set("X-Content-Type-Options", "nosniff")
			h.Set("X-Frame-Options", "DENY")
			h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
			h.Set("Cache-Control", "no-store")

			// Inject minimal RequestContext so that WriteError (used by
			// recovery middleware, auth errors, etc.) can include
			// correlation_id in the JSON body.
			ctx := logger.WithRequestContext(r.Context(), logger.RequestContext{
				CorrelationID: cid,
			})

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// maxCorrelationIDLen is the maximum accepted length for a client-provided
// correlation ID. Values longer than this are replaced with a generated UUID.
const maxCorrelationIDLen = 128

// isValidCorrelationID checks that a client-provided correlation ID is safe
// to echo in response headers and log output. It must be non-empty,
// at most maxCorrelationIDLen bytes, and contain only printable ASCII
// (0x20–0x7E) to prevent log injection and header pollution.
func isValidCorrelationID(s string) bool {
	if len(s) == 0 || len(s) > maxCorrelationIDLen {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < 0x20 || s[i] > 0x7E {
			return false
		}
	}
	return true
}

// noopMiddleware is a pass-through middleware used as a fallback when the
// real auth middleware is not injected (e.g., in tests).
func noopMiddleware(next http.Handler) http.Handler {
	return next
}
