package ratelimit

import (
	"errors"
	"net/http"
	"time"

	"contractpro/api-orchestrator/internal/domain/model"
	"contractpro/api-orchestrator/internal/infra/observability/logger"
	"contractpro/api-orchestrator/internal/ingress/middleware/auth"

	"contractpro/api-orchestrator/internal/config"
)

// window is the fixed-window duration for rate limit counters.
// Each counter key auto-expires after this period.
const window = 1 * time.Second

// Middleware implements per-organization rate limiting for the API
// Orchestrator. It sits after Auth+RBAC in the middleware chain
// (AuthContext is guaranteed to be in the request context) and before
// route handlers.
//
// When disabled (cfg.Enabled=false), Handler() returns a no-op
// pass-through with zero per-request overhead.
type Middleware struct {
	store    RateLimiterStore
	readRPS  int
	writeRPS int
	enabled  bool
	log      *logger.Logger
}

// NewMiddleware constructs a rate limiter Middleware.
//
// When cfg.Enabled is true, store must not be nil (returns an error).
// When cfg.Enabled is false, store is ignored and may be nil.
func NewMiddleware(cfg config.RateLimitConfig, store RateLimiterStore, log *logger.Logger) (*Middleware, error) {
	if cfg.Enabled && store == nil {
		return nil, errors.New("ratelimit: store is required when rate limiting is enabled")
	}
	if cfg.Enabled && cfg.ReadRPS <= 0 {
		return nil, errors.New("ratelimit: ReadRPS must be > 0 when rate limiting is enabled")
	}
	if cfg.Enabled && cfg.WriteRPS <= 0 {
		return nil, errors.New("ratelimit: WriteRPS must be > 0 when rate limiting is enabled")
	}
	return &Middleware{
		store:    store,
		readRPS:  cfg.ReadRPS,
		writeRPS: cfg.WriteRPS,
		enabled:  cfg.Enabled,
		log:      log.With("component", "ratelimit-middleware"),
	}, nil
}

// Handler returns a chi-compatible middleware function.
//
// When rate limiting is disabled, it returns a no-op that passes
// requests directly to the next handler without any overhead.
func (m *Middleware) Handler() func(http.Handler) http.Handler {
	if !m.enabled {
		return func(next http.Handler) http.Handler { return next }
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()

			// 1. Extract AuthContext.
			//    This middleware runs after Auth+RBAC, so AuthContext
			//    must be present. Missing AuthContext is a programmer error.
			ac, ok := auth.AuthContextFrom(ctx)
			if !ok {
				m.log.Error(ctx, "AuthContext not found in context",
					"method", r.Method,
					"path", r.URL.Path,
				)
				model.WriteError(w, r, model.ErrInternalError, nil)
				return
			}

			// 2. Classify request and determine limit.
			class := operationClass(r.Method)
			limit := m.readRPS
			if class == "write" {
				limit = m.writeRPS
			}

			// 3. Check rate limit.
			key := rateLimitKey(ac.OrganizationID, class)
			allowed, err := m.store.Allow(ctx, key, limit, window)

			// 4. Redis failure → allow request (degraded mode).
			if err != nil {
				m.log.Warn(ctx, "Redis unavailable, allowing request",
					"error", err.Error(),
					"org_id", ac.OrganizationID,
					"class", class,
				)
				next.ServeHTTP(w, r)
				return
			}

			// 5. Rate exceeded → 429 with Retry-After.
			if !allowed {
				m.log.Warn(ctx, "rate limit exceeded",
					"org_id", ac.OrganizationID,
					"class", class,
					"limit", limit,
				)
				w.Header().Set("Retry-After", "1")
				model.WriteError(w, r, model.ErrRateLimitExceeded, nil)
				return
			}

			// 6. Within limits → proceed.
			next.ServeHTTP(w, r)
		})
	}
}

// operationClass returns "read" for GET/HEAD and "write" for all other
// HTTP methods (POST, PUT, DELETE, PATCH, and any unknown methods).
// OPTIONS requests are never seen here because the CORS middleware
// handles them before the auth chain.
func operationClass(method string) string {
	switch method {
	case http.MethodGet, http.MethodHead:
		return "read"
	default:
		return "write"
	}
}

// rateLimitKey builds the Redis key for rate limiting.
// Format: rl:{organization_id}:{read|write}
func rateLimitKey(orgID, class string) string {
	return "rl:" + orgID + ":" + class
}
