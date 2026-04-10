// Package metrics provides Prometheus metrics for the API/Backend Orchestrator.
//
// It registers all application-level metrics with a dedicated prometheus.Registry
// (not the global default) and provides:
//
//   - HTTPMiddleware: chi-compatible middleware that auto-instruments HTTP requests
//     (total count, duration, request body size) using route patterns for labels.
//   - Handler: returns an http.Handler for the /metrics scrape endpoint.
//   - Shutdown: satisfies the app.ObservabilityShutdown interface (no-op for
//     pull-based Prometheus, but provides an extension point for future push-based
//     metrics).
//
// All collector fields are exported so that components receiving *Metrics via DI
// can record observations directly. Prometheus collectors are concurrency-safe by
// design, so no additional synchronization is needed.
package metrics

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Metrics holds all Prometheus collectors for the API/Backend Orchestrator.
// It uses a dedicated prometheus.Registry (not the global default) for clean
// testing and isolated handler wiring.
type Metrics struct {
	// --- HTTP ---
	HTTPRequestsTotal   *prometheus.CounterVec
	HTTPRequestDuration *prometheus.HistogramVec
	HTTPRequestSize     *prometheus.HistogramVec

	// --- Upload ---
	UploadTotal    *prometheus.CounterVec
	UploadSize     prometheus.Histogram
	UploadDuration prometheus.Histogram

	// --- DM Client ---
	DMRequestsTotal       *prometheus.CounterVec
	DMRequestDuration     *prometheus.HistogramVec
	DMCircuitBreakerState prometheus.Gauge

	// --- S3 ---
	S3OperationsTotal   *prometheus.CounterVec
	S3OperationDuration *prometheus.HistogramVec

	// --- Broker ---
	BrokerPublishTotal    *prometheus.CounterVec
	BrokerPublishDuration *prometheus.HistogramVec
	EventsReceivedTotal   *prometheus.CounterVec

	// --- SSE ---
	SSEConnectionsActive prometheus.Gauge
	SSEEventsPushedTotal *prometheus.CounterVec

	// --- Security ---
	RateLimitHitsTotal *prometheus.CounterVec
	AuthFailuresTotal  *prometheus.CounterVec

	// --- Redis ---
	RedisOperationsTotal   *prometheus.CounterVec
	RedisOperationDuration *prometheus.HistogramVec

	registry *prometheus.Registry
}

// Compile-time interface check: Metrics must satisfy the
// app.ObservabilityShutdown interface (Shutdown(context.Context) error).
var _ interface{ Shutdown(context.Context) error } = (*Metrics)(nil)

// NewMetrics creates and registers all orchestrator metrics with a dedicated
// Prometheus registry. The returned Metrics is safe for concurrent use.
func NewMetrics() *Metrics {
	reg := prometheus.NewRegistry()

	// Register Go runtime and process collectors for standard dashboards.
	reg.MustRegister(prometheus.NewGoCollector())
	reg.MustRegister(prometheus.NewProcessCollector(prometheus.ProcessCollectorOpts{}))

	m := &Metrics{
		// --- HTTP ---
		HTTPRequestsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "orch_http_requests_total",
			Help: "Total HTTP requests by method, route pattern, and status code.",
		}, []string{"method", "path", "status_code"}),

		HTTPRequestDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "orch_http_request_duration_seconds",
			Help:    "HTTP request duration in seconds.",
			Buckets: []float64{0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2, 5, 10},
		}, []string{"method", "path"}),

		HTTPRequestSize: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "orch_http_request_size_bytes",
			Help:    "HTTP request body size in bytes.",
			Buckets: []float64{1024, 10240, 102400, 1048576, 5242880, 10485760, 20971520},
		}, []string{"method"}),

		// --- Upload ---
		UploadTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "orch_upload_total",
			Help: "Total upload operations by status.",
		}, []string{"status"}),

		UploadSize: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "orch_upload_size_bytes",
			Help:    "Uploaded file size in bytes.",
			Buckets: []float64{102400, 512000, 1048576, 5242880, 10485760, 15728640, 20971520},
		}),

		UploadDuration: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "orch_upload_duration_seconds",
			Help:    "Upload operation duration in seconds.",
			Buckets: []float64{0.5, 1, 2, 5, 10, 30, 60},
		}),

		// --- DM Client ---
		DMRequestsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "orch_dm_requests_total",
			Help: "Total DM service requests by method, path, and status code.",
		}, []string{"method", "path", "status_code"}),

		DMRequestDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "orch_dm_request_duration_seconds",
			Help:    "DM service request duration in seconds.",
			Buckets: []float64{0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2, 5},
		}, []string{"method", "path"}),

		DMCircuitBreakerState: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "orch_dm_circuit_breaker_state",
			Help: "DM circuit breaker state: 0=closed, 1=half-open, 2=open.",
		}),

		// --- S3 ---
		S3OperationsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "orch_s3_operations_total",
			Help: "Total S3 operations by operation type and status.",
		}, []string{"operation", "status"}),

		S3OperationDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "orch_s3_operation_duration_seconds",
			Help:    "S3 operation duration in seconds.",
			Buckets: []float64{0.1, 0.25, 0.5, 1, 2, 5, 10, 30},
		}, []string{"operation"}),

		// --- Broker ---
		BrokerPublishTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "orch_broker_publish_total",
			Help: "Total broker publish operations by topic and status.",
		}, []string{"topic", "status"}),

		BrokerPublishDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "orch_broker_publish_duration_seconds",
			Help:    "Broker publish duration in seconds.",
			Buckets: []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.5},
		}, []string{"topic"}),

		EventsReceivedTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "orch_events_received_total",
			Help: "Total events received from domains.",
		}, []string{"event_type", "source_domain"}),

		// --- SSE ---
		SSEConnectionsActive: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "orch_sse_connections_active",
			Help: "Number of active SSE connections.",
		}),

		SSEEventsPushedTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "orch_sse_events_pushed_total",
			Help: "Total SSE events pushed to clients by event type.",
		}, []string{"event_type"}),

		// --- Security ---
		RateLimitHitsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "orch_rate_limit_hits_total",
			Help: "Total rate limit rejections by endpoint class.",
		}, []string{"endpoint_class"}),

		AuthFailuresTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "orch_auth_failures_total",
			Help: "Total authentication failures by reason.",
		}, []string{"reason"}),

		// --- Redis ---
		RedisOperationsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "orch_redis_operations_total",
			Help: "Total Redis operations by operation type and status.",
		}, []string{"operation", "status"}),

		RedisOperationDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "orch_redis_operation_duration_seconds",
			Help:    "Redis operation duration in seconds.",
			Buckets: []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.5},
		}, []string{"operation"}),

		registry: reg,
	}

	reg.MustRegister(
		m.HTTPRequestsTotal,
		m.HTTPRequestDuration,
		m.HTTPRequestSize,
		m.UploadTotal,
		m.UploadSize,
		m.UploadDuration,
		m.DMRequestsTotal,
		m.DMRequestDuration,
		m.DMCircuitBreakerState,
		m.S3OperationsTotal,
		m.S3OperationDuration,
		m.BrokerPublishTotal,
		m.BrokerPublishDuration,
		m.EventsReceivedTotal,
		m.SSEConnectionsActive,
		m.SSEEventsPushedTotal,
		m.RateLimitHitsTotal,
		m.AuthFailuresTotal,
		m.RedisOperationsTotal,
		m.RedisOperationDuration,
	)

	return m
}

// Handler returns an http.Handler that serves the /metrics endpoint in
// Prometheus exposition format. It uses promhttp.HandlerFor with the custom
// registry so only orchestrator metrics (plus Go/process collectors) are
// exposed.
func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{
		EnableOpenMetrics: true,
	})
}

// Registry returns the dedicated Prometheus registry. Useful for tests that
// need to collect and assert on metric values via Gather().
func (m *Metrics) Registry() *prometheus.Registry {
	return m.registry
}

// Shutdown satisfies the app.ObservabilityShutdown interface. Prometheus is
// pull-based (scraped by the Prometheus server), so there is no buffered data
// to flush. This method is a clean extension point for future push-based
// metrics (e.g., Pushgateway).
func (m *Metrics) Shutdown(_ context.Context) error {
	return nil
}

// --- HTTP Middleware ---

// responseWriter wraps http.ResponseWriter to capture the status code written
// by downstream handlers. It is used by HTTPMiddleware to record the status
// code label without inspecting the response body.
type responseWriter struct {
	http.ResponseWriter
	statusCode  int
	wroteHeader bool
}

func newResponseWriter(w http.ResponseWriter) *responseWriter {
	return &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}
}

func (rw *responseWriter) WriteHeader(code int) {
	if rw.wroteHeader {
		return
	}
	rw.statusCode = code
	rw.wroteHeader = true
	rw.ResponseWriter.WriteHeader(code)
}

// Write delegates to the underlying ResponseWriter and marks the header as
// written. Go's http.ResponseWriter.Write implicitly calls WriteHeader(200)
// if not already called — tracking this prevents a subsequent explicit
// WriteHeader from overwriting the recorded status code.
func (rw *responseWriter) Write(b []byte) (int, error) {
	if !rw.wroteHeader {
		rw.wroteHeader = true
		// statusCode is already http.StatusOK from newResponseWriter.
	}
	return rw.ResponseWriter.Write(b)
}

// Unwrap returns the underlying ResponseWriter. Required for Go 1.20+
// http.ResponseController to discover the original writer's capabilities
// (e.g., SetWriteDeadline used by the SSE handler).
func (rw *responseWriter) Unwrap() http.ResponseWriter {
	return rw.ResponseWriter
}

// Flush delegates to the underlying writer's Flush method if available.
// Required for SSE streaming (the SSE handler checks w.(http.Flusher)).
func (rw *responseWriter) Flush() {
	if f, ok := rw.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// HTTPMiddleware returns a chi-compatible middleware that records HTTP request
// metrics: total count, duration, and request body size.
//
// Path labels use chi's route pattern (e.g., "/api/v1/contracts/{contract_id}")
// to prevent label cardinality explosion from actual UUIDs. The pattern is
// resolved AFTER the request completes (chi.RouteContext is populated by the
// router during ServeHTTP).
func (m *Metrics) HTTPMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rw := newResponseWriter(w)

			next.ServeHTTP(rw, r)

			duration := time.Since(start).Seconds()
			status := strconv.Itoa(rw.statusCode)
			method := r.Method
			pattern := routePattern(r)

			m.HTTPRequestsTotal.WithLabelValues(method, pattern, status).Inc()
			m.HTTPRequestDuration.WithLabelValues(method, pattern).Observe(duration)

			if r.ContentLength > 0 {
				m.HTTPRequestSize.WithLabelValues(method).Observe(float64(r.ContentLength))
			}
		})
	}
}

// safeFixedPaths are paths mounted outside chi (e.g., via http.ServeMux for
// health probes) that are safe to use as-is in metric labels because they
// contain no dynamic segments.
var safeFixedPaths = map[string]struct{}{
	"/healthz":  {},
	"/readyz":   {},
	"/metrics":  {},
}

// routePattern extracts the chi route pattern from the request context.
// Returns the matched pattern (e.g., "/api/v1/contracts/{contract_id}").
//
// When the route context is nil (requests handled by non-chi muxes like
// http.ServeMux), it returns the raw path only for known safe fixed paths;
// otherwise "__unmatched__" is returned to prevent label cardinality explosion
// from attacker-generated random paths.
func routePattern(r *http.Request) string {
	rctx := chi.RouteContext(r.Context())
	if rctx == nil {
		return safeFallback(r.URL.Path)
	}
	pattern := rctx.RoutePattern()
	if pattern == "" {
		return safeFallback(r.URL.Path)
	}
	return pattern
}

// safeFallback returns the path if it is in the safe allowlist, or the
// sentinel "__unmatched__" to prevent metric cardinality explosion.
func safeFallback(path string) string {
	if _, ok := safeFixedPaths[path]; ok {
		return path
	}
	return "__unmatched__"
}
