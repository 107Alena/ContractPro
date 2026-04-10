// Package api provides the HTTP server for the API/Backend Orchestrator.
//
// The server uses the chi router to handle incoming HTTP requests from
// frontend applications and external integrations. It manages two separate
// HTTP servers:
//
//   - Main server (ORCH_HTTP_PORT, default 8080): serves the REST API,
//     health probes, and SSE endpoint.
//   - Metrics server (ORCH_METRICS_PORT, default 9090): serves the
//     /metrics endpoint for Prometheus scraping.
//
// Route groups:
//
//   - Public routes (/api/v1/auth/*, /healthz, /readyz): no authentication
//     required. CORS middleware is applied.
//   - Protected routes (/api/v1/contracts/*, /api/v1/users/me, etc.):
//     full middleware chain (CORS -> Auth -> RBAC -> RateLimit).
//   - SSE route (/api/v1/events/stream): authentication via query parameter,
//     handled separately from the standard auth middleware.
//
// Middleware chain: Recovery → CORS → SecurityHeaders (global);
// Auth → RBAC → RateLimit (protected routes).
//
// Route handlers are placeholder stubs returning 501 Not Implemented.
// Real handlers will be added starting from ORCH-TASK-018.
package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"time"

	"contractpro/api-orchestrator/internal/application/authproxy"
	"contractpro/api-orchestrator/internal/application/comparison"
	"contractpro/api-orchestrator/internal/application/contracts"
	"contractpro/api-orchestrator/internal/application/export"
	"contractpro/api-orchestrator/internal/application/results"
	"contractpro/api-orchestrator/internal/application/versions"
	"contractpro/api-orchestrator/internal/config"
	"contractpro/api-orchestrator/internal/infra/health"
	"contractpro/api-orchestrator/internal/infra/observability/logger"
	"contractpro/api-orchestrator/internal/ingress/sse"

	"github.com/go-chi/chi/v5"
)

// Server is the HTTP server for the API/Backend Orchestrator.
//
// It manages two http.Server instances: the main API server and a
// separate metrics server. Both support graceful shutdown via the
// Shutdown method.
type Server struct {
	main    *http.Server
	metrics *http.Server
	router  chi.Router
	log     *logger.Logger
	cfg     config.HTTPConfig
}

// Deps holds the dependencies required to construct a Server.
//
// Config, Health, and Logger are required.
// AuthMiddleware, RBACMiddleware, and RateLimitMiddleware are optional:
// when nil, a no-op pass-through is used (useful for tests).
// UploadHandler is optional: when nil, a 501 Not Implemented stub is used.
// MetricsHandler is optional: when nil, a placeholder stub is used.
// HTTPMetricsMiddleware is optional: when nil, no HTTP metrics are recorded.
type Deps struct {
	Config          config.HTTPConfig
	CORSConfig      config.CORSConfig
	Health          *health.Handler
	Logger          *logger.Logger
	AuthMiddleware      func(http.Handler) http.Handler
	RBACMiddleware      func(http.Handler) http.Handler
	RateLimitMiddleware func(http.Handler) http.Handler
	MetricsHandler        http.Handler
	HTTPMetricsMiddleware func(http.Handler) http.Handler
	AuthHandler     *authproxy.Handler
	UploadHandler   http.HandlerFunc
	ContractHandler    *contracts.Handler
	VersionHandler     *versions.Handler
	ResultsHandler     *results.Handler
	ComparisonHandler  *comparison.Handler
	ExportHandler      *export.Handler
	SSEHandler         *sse.Handler
}

// NewServer constructs a Server with the chi router, middleware chain,
// and all route groups registered. The server is not started until
// Start is called.
func NewServer(deps Deps) *Server {
	log := deps.Logger.With("component", "http-server")

	r := chi.NewRouter()

	// Apply global middleware.
	// HTTPMetricsMiddleware is first so that it captures the full request
	// lifecycle (including panics caught by recovery). The route pattern
	// is resolved after ServeHTTP returns, so it sees the final pattern.
	// Recovery must be next to catch panics from all downstream middleware
	// and handlers. CORS must come before auth because preflight OPTIONS
	// requests carry no JWT. SecurityHeaders generates the correlation ID
	// used by all downstream middleware and error responses.
	if deps.HTTPMetricsMiddleware != nil {
		r.Use(deps.HTTPMetricsMiddleware)
	}
	r.Use(RecoveryMiddleware(log))
	r.Use(CORSMiddleware(deps.CORSConfig, log))
	r.Use(SecurityHeadersMiddleware())

	// System endpoints: mount the health handler's ServeMux at the root
	// so that /healthz and /readyz paths are preserved as-is. Mounting at
	// a sub-path would cause chi to strip the prefix before delegating,
	// which breaks the health handler's exact pattern registration.
	r.Mount("/", deps.Health.Mux())

	authMW := deps.AuthMiddleware
	if authMW == nil {
		authMW = noopMiddleware
	}
	rbacMW := deps.RBACMiddleware
	if rbacMW == nil {
		rbacMW = noopMiddleware
	}
	rateLimitMW := deps.RateLimitMiddleware
	if rateLimitMW == nil {
		rateLimitMW = noopMiddleware
	}

	uploadH := deps.UploadHandler
	if uploadH == nil {
		uploadH = notImplemented
	}

	registerRoutes(r, authMW, rbacMW, rateLimitMW, uploadH, deps.AuthHandler, deps.ContractHandler, deps.VersionHandler, deps.ResultsHandler, deps.ComparisonHandler, deps.ExportHandler, deps.SSEHandler)

	mainAddr := fmt.Sprintf(":%d", deps.Config.Port)
	main := &http.Server{
		Addr:         mainAddr,
		Handler:      r,
		ReadTimeout:  deps.Config.RequestTimeout,
		WriteTimeout: deps.Config.RequestTimeout + 5*time.Second,
		IdleTimeout:  2 * deps.Config.RequestTimeout,
	}

	metricsRouter := chi.NewRouter()
	if deps.MetricsHandler != nil {
		metricsRouter.Handle("/metrics", deps.MetricsHandler)
	} else {
		metricsRouter.Get("/metrics", handleMetricsStub)
	}

	metricsAddr := fmt.Sprintf(":%d", deps.Config.MetricsPort)
	metricsSrv := &http.Server{
		Addr:         metricsAddr,
		Handler:      metricsRouter,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
		IdleTimeout:  30 * time.Second,
	}

	return &Server{
		main:    main,
		metrics: metricsSrv,
		router:  r,
		log:     log,
		cfg:     deps.Config,
	}
}

// Start begins listening on both the main and metrics ports.
// It blocks until both servers exit. If either server fails to start
// (except via Shutdown), the error is returned.
//
// Start spawns the metrics server in a separate goroutine and runs
// the main server on the calling goroutine. Call Shutdown to stop both.
func (s *Server) Start() error {
	metricsErrCh := make(chan error, 1)
	go func() {
		s.log.Info(context.Background(), "metrics server starting",
			"addr", s.metrics.Addr,
		)
		if err := s.metrics.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			metricsErrCh <- fmt.Errorf("metrics server: %w", err)
			return
		}
		metricsErrCh <- nil
	}()

	s.log.Info(context.Background(), "main server starting",
		"addr", s.main.Addr,
	)

	if err := s.main.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("main server: %w", err)
	}

	// Main server has stopped (via Shutdown); check if metrics server had an error.
	if err := <-metricsErrCh; err != nil {
		return err
	}
	return nil
}

// Shutdown gracefully stops both the main and metrics servers.
// The provided context controls the maximum time to wait for in-flight
// requests to complete. Typically ctx should carry a deadline derived
// from HTTPConfig.ShutdownTimeout.
func (s *Server) Shutdown(ctx context.Context) error {
	s.log.Info(ctx, "server shutdown initiated")

	// Shut down both servers concurrently.
	mainErrCh := make(chan error, 1)
	go func() {
		mainErrCh <- s.main.Shutdown(ctx)
	}()

	metricsErr := s.metrics.Shutdown(ctx)

	mainErr := <-mainErrCh

	if mainErr != nil {
		return fmt.Errorf("main server shutdown: %w", mainErr)
	}
	if metricsErr != nil {
		return fmt.Errorf("metrics server shutdown: %w", metricsErr)
	}

	s.log.Info(ctx, "server shutdown complete")
	return nil
}

// Router returns the chi router for testing purposes.
// Production code should not call this method.
func (s *Server) Router() chi.Router {
	return s.router
}

// MainAddr returns the address the main server is configured to listen on.
// This is useful for integration tests that use port 0 for ephemeral ports.
func (s *Server) MainAddr() string {
	return s.main.Addr
}

// MetricsAddr returns the address the metrics server is configured to listen on.
func (s *Server) MetricsAddr() string {
	return s.metrics.Addr
}

// StartWithListeners starts both servers using the provided listeners
// instead of binding to their configured addresses. This avoids port
// conflicts in tests.
func (s *Server) StartWithListeners(mainLn, metricsLn net.Listener) error {
	metricsErrCh := make(chan error, 1)
	go func() {
		s.log.Info(context.Background(), "metrics server starting",
			"addr", metricsLn.Addr().String(),
		)
		if err := s.metrics.Serve(metricsLn); err != nil && err != http.ErrServerClosed {
			metricsErrCh <- fmt.Errorf("metrics server: %w", err)
			return
		}
		metricsErrCh <- nil
	}()

	s.log.Info(context.Background(), "main server starting",
		"addr", mainLn.Addr().String(),
	)

	if err := s.main.Serve(mainLn); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("main server: %w", err)
	}

	if err := <-metricsErrCh; err != nil {
		return err
	}
	return nil
}

// --- stub handlers ---

// handleMetricsStub is a fallback for the Prometheus metrics endpoint when
// Deps.MetricsHandler is nil (e.g., in tests). In production, NewApp always
// wires the real metrics handler from the metrics package (ORCH-TASK-032).
func handleMetricsStub(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("# HELP orch_placeholder Placeholder metric\n# TYPE orch_placeholder gauge\norch_placeholder 1\n"))
}

// notImplemented returns a 501 Not Implemented JSON response.
// Used as a placeholder for all route handlers until their real
// implementations are added in subsequent tasks. Uses the unified
// ErrorResponse field names for consistency.
func notImplemented(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotImplemented)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error_code": "NOT_IMPLEMENTED",
		"message":    "Данный endpoint ещё не реализован.",
	})
}
