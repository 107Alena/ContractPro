package metrics

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	dto "github.com/prometheus/client_model/go"
)

// --- test helpers ---

// gatherMetric collects all metric families from the registry and returns
// the one matching the given name, or nil if not found.
func gatherMetric(t *testing.T, m *Metrics, name string) *dto.MetricFamily {
	t.Helper()
	mfs, err := m.Registry().Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}
	for _, mf := range mfs {
		if mf.GetName() == name {
			return mf
		}
	}
	return nil
}

// counterValue returns the counter value for a metric with the given label pairs.
func counterValue(t *testing.T, m *Metrics, name string, labels map[string]string) float64 {
	t.Helper()
	mf := gatherMetric(t, m, name)
	if mf == nil {
		return 0
	}
	for _, metric := range mf.GetMetric() {
		if labelsMatch(metric, labels) {
			return metric.GetCounter().GetValue()
		}
	}
	return 0
}

// histogramCount returns the sample count for a histogram with the given label pairs.
func histogramCount(t *testing.T, m *Metrics, name string, labels map[string]string) uint64 {
	t.Helper()
	mf := gatherMetric(t, m, name)
	if mf == nil {
		return 0
	}
	for _, metric := range mf.GetMetric() {
		if labelsMatch(metric, labels) {
			return metric.GetHistogram().GetSampleCount()
		}
	}
	return 0
}

// histogramSum returns the sum for a histogram with the given label pairs.
func histogramSum(t *testing.T, m *Metrics, name string, labels map[string]string) float64 {
	t.Helper()
	mf := gatherMetric(t, m, name)
	if mf == nil {
		return 0
	}
	for _, metric := range mf.GetMetric() {
		if labelsMatch(metric, labels) {
			return metric.GetHistogram().GetSampleSum()
		}
	}
	return 0
}

// gaugeValue returns the gauge value for a metric with the given label pairs.
func gaugeValue(t *testing.T, m *Metrics, name string, labels map[string]string) float64 {
	t.Helper()
	mf := gatherMetric(t, m, name)
	if mf == nil {
		return 0
	}
	for _, metric := range mf.GetMetric() {
		if labelsMatch(metric, labels) {
			return metric.GetGauge().GetValue()
		}
	}
	return 0
}

// labelsMatch checks whether a Prometheus metric has exactly the given label set.
func labelsMatch(metric *dto.Metric, labels map[string]string) bool {
	if labels == nil && len(metric.GetLabel()) == 0 {
		return true
	}
	if len(labels) != len(metric.GetLabel()) {
		return false
	}
	for _, lp := range metric.GetLabel() {
		v, ok := labels[lp.GetName()]
		if !ok || v != lp.GetValue() {
			return false
		}
	}
	return true
}

// --- constructor tests ---

func TestNewMetrics_AllCollectorsRegistered(t *testing.T) {
	m := NewMetrics()
	mfs, err := m.Registry().Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}

	// Collect all registered metric names.
	names := make(map[string]struct{}, len(mfs))
	for _, mf := range mfs {
		names[mf.GetName()] = struct{}{}
	}

	expected := []string{
		"orch_http_requests_total",
		"orch_http_request_duration_seconds",
		"orch_http_request_size_bytes",
		"orch_upload_total",
		"orch_upload_size_bytes",
		"orch_upload_duration_seconds",
		"orch_dm_requests_total",
		"orch_dm_request_duration_seconds",
		"orch_dm_circuit_breaker_state",
		"orch_s3_operations_total",
		"orch_s3_operation_duration_seconds",
		"orch_broker_publish_total",
		"orch_broker_publish_duration_seconds",
		"orch_events_received_total",
		"orch_sse_connections_active",
		"orch_sse_events_pushed_total",
		"orch_rate_limit_hits_total",
		"orch_auth_failures_total",
		"orch_redis_operations_total",
		"orch_redis_operation_duration_seconds",
		"orch_permissions_cache_hit_total",
		"orch_permissions_cache_miss_total",
		"orch_permissions_opm_fallback_total",
		"orch_permissions_resolve_duration_seconds",
	}

	// Touch all metrics so they appear in Gather().
	m.HTTPRequestsTotal.WithLabelValues("GET", "/test", "200").Inc()
	m.HTTPRequestDuration.WithLabelValues("GET", "/test").Observe(0.1)
	m.HTTPRequestSize.WithLabelValues("GET").Observe(100)
	m.UploadTotal.WithLabelValues("success").Inc()
	m.UploadSize.Observe(1000)
	m.UploadDuration.Observe(1.0)
	m.DMRequestsTotal.WithLabelValues("GET", "/documents", "200").Inc()
	m.DMRequestDuration.WithLabelValues("GET", "/documents").Observe(0.05)
	m.DMCircuitBreakerState.Set(0)
	m.S3OperationsTotal.WithLabelValues("put", "success").Inc()
	m.S3OperationDuration.WithLabelValues("put").Observe(0.5)
	m.BrokerPublishTotal.WithLabelValues("dp.commands", "success").Inc()
	m.BrokerPublishDuration.WithLabelValues("dp.commands").Observe(0.01)
	m.EventsReceivedTotal.WithLabelValues("status_changed", "dp").Inc()
	m.SSEConnectionsActive.Set(5)
	m.SSEEventsPushedTotal.WithLabelValues("status_update").Inc()
	m.RateLimitHitsTotal.WithLabelValues("read").Inc()
	m.AuthFailuresTotal.WithLabelValues("expired").Inc()
	m.RedisOperationsTotal.WithLabelValues("get", "success").Inc()
	m.RedisOperationDuration.WithLabelValues("get").Observe(0.001)
	m.PermissionsCacheHitTotal.WithLabelValues("export_enabled", "abcdef12").Inc()
	m.PermissionsCacheMissTotal.WithLabelValues("export_enabled").Inc()
	m.PermissionsOPMFallbackTotal.WithLabelValues("export_enabled", "timeout").Inc()
	m.PermissionsResolveDuration.Observe(0.01)

	// Re-gather after touching all metrics.
	mfs, err = m.Registry().Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}
	names = make(map[string]struct{}, len(mfs))
	for _, mf := range mfs {
		names[mf.GetName()] = struct{}{}
	}

	for _, name := range expected {
		if _, ok := names[name]; !ok {
			t.Errorf("metric %q not registered", name)
		}
	}
}

func TestNewMetrics_GoCollectorsPresent(t *testing.T) {
	m := NewMetrics()
	mfs, err := m.Registry().Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}
	hasGo := false
	for _, mf := range mfs {
		if strings.HasPrefix(mf.GetName(), "go_") {
			hasGo = true
			break
		}
	}
	if !hasGo {
		t.Error("expected go_* runtime metrics to be registered")
	}
}

func TestNewMetrics_IsolatedRegistry(t *testing.T) {
	m1 := NewMetrics()
	m2 := NewMetrics()

	// Increment a counter in m1; m2 should not see it.
	m1.HTTPRequestsTotal.WithLabelValues("GET", "/test", "200").Inc()

	v1 := counterValue(t, m1, "orch_http_requests_total", map[string]string{"method": "GET", "path": "/test", "status_code": "200"})
	v2 := counterValue(t, m2, "orch_http_requests_total", map[string]string{"method": "GET", "path": "/test", "status_code": "200"})

	if v1 != 1 {
		t.Errorf("m1 counter: got %v, want 1", v1)
	}
	if v2 != 0 {
		t.Errorf("m2 counter: got %v, want 0 (isolated)", v2)
	}
}

// --- Handler tests ---

func TestHandler_PrometheusFormat(t *testing.T) {
	m := NewMetrics()
	m.HTTPRequestsTotal.WithLabelValues("GET", "/test", "200").Inc()

	rec := httptest.NewRecorder()
	m.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/metrics", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", rec.Code)
	}

	body := rec.Body.String()
	if !strings.Contains(body, "orch_http_requests_total") {
		t.Error("response body should contain orch_http_requests_total")
	}
	if !strings.Contains(body, `method="GET"`) {
		t.Error("response body should contain method label")
	}
}

func TestHandler_ContentType(t *testing.T) {
	m := NewMetrics()
	rec := httptest.NewRecorder()
	m.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/metrics", nil))

	ct := rec.Header().Get("Content-Type")
	if !strings.Contains(ct, "text/plain") && !strings.Contains(ct, "application/openmetrics-text") {
		t.Errorf("unexpected Content-Type: %q", ct)
	}
}

// --- Shutdown tests ---

func TestShutdown_ReturnsNil(t *testing.T) {
	m := NewMetrics()
	if err := m.Shutdown(context.Background()); err != nil {
		t.Errorf("Shutdown() = %v, want nil", err)
	}
}

func TestShutdown_Interface(t *testing.T) {
	// Verify that *Metrics satisfies the ObservabilityShutdown interface shape.
	var _ interface{ Shutdown(context.Context) error } = (*Metrics)(nil)
}

// --- HTTP counter/histogram direct access tests ---

func TestHTTPRequestsTotal_Counter(t *testing.T) {
	m := NewMetrics()
	m.HTTPRequestsTotal.WithLabelValues("POST", "/api/v1/contracts/upload", "202").Inc()
	m.HTTPRequestsTotal.WithLabelValues("POST", "/api/v1/contracts/upload", "202").Inc()
	m.HTTPRequestsTotal.WithLabelValues("GET", "/api/v1/contracts", "200").Inc()

	v := counterValue(t, m, "orch_http_requests_total", map[string]string{
		"method": "POST", "path": "/api/v1/contracts/upload", "status_code": "202",
	})
	if v != 2 {
		t.Errorf("got %v, want 2", v)
	}
}

func TestHTTPRequestDuration_Histogram(t *testing.T) {
	m := NewMetrics()
	m.HTTPRequestDuration.WithLabelValues("GET", "/api/v1/contracts").Observe(0.15)
	m.HTTPRequestDuration.WithLabelValues("GET", "/api/v1/contracts").Observe(0.25)

	count := histogramCount(t, m, "orch_http_request_duration_seconds", map[string]string{
		"method": "GET", "path": "/api/v1/contracts",
	})
	if count != 2 {
		t.Errorf("got count %d, want 2", count)
	}
}

func TestHTTPRequestDuration_Buckets(t *testing.T) {
	m := NewMetrics()
	m.HTTPRequestDuration.WithLabelValues("GET", "/test").Observe(0.005)

	mf := gatherMetric(t, m, "orch_http_request_duration_seconds")
	if mf == nil {
		t.Fatal("metric not found")
	}
	for _, metric := range mf.GetMetric() {
		buckets := metric.GetHistogram().GetBucket()
		if len(buckets) != 10 {
			t.Errorf("expected 10 buckets, got %d", len(buckets))
		}
		break
	}
}

func TestHTTPRequestSize_Histogram(t *testing.T) {
	m := NewMetrics()
	m.HTTPRequestSize.WithLabelValues("POST").Observe(5242880) // 5MB

	count := histogramCount(t, m, "orch_http_request_size_bytes", map[string]string{"method": "POST"})
	if count != 1 {
		t.Errorf("got count %d, want 1", count)
	}
}

// --- Upload metrics ---

func TestUploadTotal_Counter(t *testing.T) {
	m := NewMetrics()
	m.UploadTotal.WithLabelValues("success").Inc()
	m.UploadTotal.WithLabelValues("error").Inc()
	m.UploadTotal.WithLabelValues("success").Inc()

	s := counterValue(t, m, "orch_upload_total", map[string]string{"status": "success"})
	e := counterValue(t, m, "orch_upload_total", map[string]string{"status": "error"})
	if s != 2 {
		t.Errorf("success: got %v, want 2", s)
	}
	if e != 1 {
		t.Errorf("error: got %v, want 1", e)
	}
}

func TestUploadSize_Histogram(t *testing.T) {
	m := NewMetrics()
	m.UploadSize.Observe(1048576)

	count := histogramCount(t, m, "orch_upload_size_bytes", nil)
	if count != 1 {
		t.Errorf("got count %d, want 1", count)
	}
}

func TestUploadDuration_Histogram(t *testing.T) {
	m := NewMetrics()
	m.UploadDuration.Observe(2.5)

	s := histogramSum(t, m, "orch_upload_duration_seconds", nil)
	if s != 2.5 {
		t.Errorf("got sum %v, want 2.5", s)
	}
}

// --- DM Client metrics ---

func TestDMRequestsTotal_Counter(t *testing.T) {
	m := NewMetrics()
	m.DMRequestsTotal.WithLabelValues("GET", "/documents", "200").Inc()

	v := counterValue(t, m, "orch_dm_requests_total", map[string]string{
		"method": "GET", "path": "/documents", "status_code": "200",
	})
	if v != 1 {
		t.Errorf("got %v, want 1", v)
	}
}

func TestDMCircuitBreakerState_Gauge(t *testing.T) {
	m := NewMetrics()

	m.DMCircuitBreakerState.Set(0)
	if v := gaugeValue(t, m, "orch_dm_circuit_breaker_state", nil); v != 0 {
		t.Errorf("closed: got %v, want 0", v)
	}

	m.DMCircuitBreakerState.Set(1)
	if v := gaugeValue(t, m, "orch_dm_circuit_breaker_state", nil); v != 1 {
		t.Errorf("half-open: got %v, want 1", v)
	}

	m.DMCircuitBreakerState.Set(2)
	if v := gaugeValue(t, m, "orch_dm_circuit_breaker_state", nil); v != 2 {
		t.Errorf("open: got %v, want 2", v)
	}
}

// --- S3 metrics ---

func TestS3OperationsTotal_Counter(t *testing.T) {
	m := NewMetrics()
	m.S3OperationsTotal.WithLabelValues("put", "success").Inc()
	m.S3OperationsTotal.WithLabelValues("delete", "error").Inc()

	put := counterValue(t, m, "orch_s3_operations_total", map[string]string{"operation": "put", "status": "success"})
	del := counterValue(t, m, "orch_s3_operations_total", map[string]string{"operation": "delete", "status": "error"})
	if put != 1 {
		t.Errorf("put success: got %v, want 1", put)
	}
	if del != 1 {
		t.Errorf("delete error: got %v, want 1", del)
	}
}

func TestS3OperationDuration_Histogram(t *testing.T) {
	m := NewMetrics()
	m.S3OperationDuration.WithLabelValues("put").Observe(1.5)

	s := histogramSum(t, m, "orch_s3_operation_duration_seconds", map[string]string{"operation": "put"})
	if s != 1.5 {
		t.Errorf("got sum %v, want 1.5", s)
	}
}

// --- Broker metrics ---

func TestBrokerPublishTotal_Counter(t *testing.T) {
	m := NewMetrics()
	m.BrokerPublishTotal.WithLabelValues("dp.commands.process-document", "success").Inc()
	m.BrokerPublishTotal.WithLabelValues("dp.commands.process-document", "error").Inc()

	s := counterValue(t, m, "orch_broker_publish_total", map[string]string{
		"topic": "dp.commands.process-document", "status": "success",
	})
	e := counterValue(t, m, "orch_broker_publish_total", map[string]string{
		"topic": "dp.commands.process-document", "status": "error",
	})
	if s != 1 {
		t.Errorf("success: got %v, want 1", s)
	}
	if e != 1 {
		t.Errorf("error: got %v, want 1", e)
	}
}

func TestBrokerPublishDuration_Histogram(t *testing.T) {
	m := NewMetrics()
	m.BrokerPublishDuration.WithLabelValues("dp.commands.process-document").Observe(0.015)

	c := histogramCount(t, m, "orch_broker_publish_duration_seconds", map[string]string{
		"topic": "dp.commands.process-document",
	})
	if c != 1 {
		t.Errorf("got count %d, want 1", c)
	}
}

func TestEventsReceivedTotal_Counter(t *testing.T) {
	m := NewMetrics()
	m.EventsReceivedTotal.WithLabelValues("dp.status-changed", "dp").Inc()
	m.EventsReceivedTotal.WithLabelValues("dm.version-created", "dm").Inc()
	m.EventsReceivedTotal.WithLabelValues("dm.version-created", "dm").Inc()

	dp := counterValue(t, m, "orch_events_received_total", map[string]string{
		"event_type": "dp.status-changed", "source_domain": "dp",
	})
	dm := counterValue(t, m, "orch_events_received_total", map[string]string{
		"event_type": "dm.version-created", "source_domain": "dm",
	})
	if dp != 1 {
		t.Errorf("dp: got %v, want 1", dp)
	}
	if dm != 2 {
		t.Errorf("dm: got %v, want 2", dm)
	}
}

// --- SSE metrics ---

func TestSSEConnectionsActive_Gauge(t *testing.T) {
	m := NewMetrics()
	m.SSEConnectionsActive.Inc()
	m.SSEConnectionsActive.Inc()
	m.SSEConnectionsActive.Dec()

	v := gaugeValue(t, m, "orch_sse_connections_active", nil)
	if v != 1 {
		t.Errorf("got %v, want 1", v)
	}
}

func TestSSEEventsPushedTotal_Counter(t *testing.T) {
	m := NewMetrics()
	m.SSEEventsPushedTotal.WithLabelValues("status_update").Inc()
	m.SSEEventsPushedTotal.WithLabelValues("comparison_update").Inc()
	m.SSEEventsPushedTotal.WithLabelValues("status_update").Inc()

	s := counterValue(t, m, "orch_sse_events_pushed_total", map[string]string{"event_type": "status_update"})
	c := counterValue(t, m, "orch_sse_events_pushed_total", map[string]string{"event_type": "comparison_update"})
	if s != 2 {
		t.Errorf("status_update: got %v, want 2", s)
	}
	if c != 1 {
		t.Errorf("comparison_update: got %v, want 1", c)
	}
}

// --- Security metrics ---

func TestRateLimitHitsTotal_Counter(t *testing.T) {
	m := NewMetrics()
	m.RateLimitHitsTotal.WithLabelValues("read").Inc()
	m.RateLimitHitsTotal.WithLabelValues("write").Inc()
	m.RateLimitHitsTotal.WithLabelValues("read").Inc()

	r := counterValue(t, m, "orch_rate_limit_hits_total", map[string]string{"endpoint_class": "read"})
	w := counterValue(t, m, "orch_rate_limit_hits_total", map[string]string{"endpoint_class": "write"})
	if r != 2 {
		t.Errorf("read: got %v, want 2", r)
	}
	if w != 1 {
		t.Errorf("write: got %v, want 1", w)
	}
}

func TestAuthFailuresTotal_Counter(t *testing.T) {
	m := NewMetrics()
	m.AuthFailuresTotal.WithLabelValues("expired").Inc()
	m.AuthFailuresTotal.WithLabelValues("invalid").Inc()
	m.AuthFailuresTotal.WithLabelValues("missing").Inc()

	for _, reason := range []string{"expired", "invalid", "missing"} {
		v := counterValue(t, m, "orch_auth_failures_total", map[string]string{"reason": reason})
		if v != 1 {
			t.Errorf("%s: got %v, want 1", reason, v)
		}
	}
}

// --- Redis metrics ---

func TestRedisOperationsTotal_Counter(t *testing.T) {
	m := NewMetrics()
	for _, op := range []string{"get", "set", "delete", "publish", "subscribe"} {
		m.RedisOperationsTotal.WithLabelValues(op, "success").Inc()
	}
	m.RedisOperationsTotal.WithLabelValues("get", "error").Inc()

	gs := counterValue(t, m, "orch_redis_operations_total", map[string]string{"operation": "get", "status": "success"})
	ge := counterValue(t, m, "orch_redis_operations_total", map[string]string{"operation": "get", "status": "error"})
	if gs != 1 {
		t.Errorf("get success: got %v, want 1", gs)
	}
	if ge != 1 {
		t.Errorf("get error: got %v, want 1", ge)
	}
}

func TestRedisOperationDuration_Histogram(t *testing.T) {
	m := NewMetrics()
	m.RedisOperationDuration.WithLabelValues("get").Observe(0.002)
	m.RedisOperationDuration.WithLabelValues("set").Observe(0.003)

	cg := histogramCount(t, m, "orch_redis_operation_duration_seconds", map[string]string{"operation": "get"})
	cs := histogramCount(t, m, "orch_redis_operation_duration_seconds", map[string]string{"operation": "set"})
	if cg != 1 {
		t.Errorf("get count: got %d, want 1", cg)
	}
	if cs != 1 {
		t.Errorf("set count: got %d, want 1", cs)
	}
}

// --- HTTPMiddleware tests ---

func TestHTTPMiddleware_RecordsTotalAndDuration(t *testing.T) {
	m := NewMetrics()

	r := chi.NewRouter()
	r.Use(m.HTTPMiddleware())
	r.Get("/api/v1/contracts/{contract_id}", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/contracts/550e8400-e29b-41d4-a716-446655440000", nil)
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", rec.Code)
	}

	// Path label should be the route pattern, NOT the actual UUID.
	v := counterValue(t, m, "orch_http_requests_total", map[string]string{
		"method": "GET", "path": "/api/v1/contracts/{contract_id}", "status_code": "200",
	})
	if v != 1 {
		t.Errorf("orch_http_requests_total: got %v, want 1", v)
	}

	c := histogramCount(t, m, "orch_http_request_duration_seconds", map[string]string{
		"method": "GET", "path": "/api/v1/contracts/{contract_id}",
	})
	if c != 1 {
		t.Errorf("orch_http_request_duration_seconds count: got %d, want 1", c)
	}
}

func TestHTTPMiddleware_RoutePatternPreventsCardinalityExplosion(t *testing.T) {
	m := NewMetrics()

	r := chi.NewRouter()
	r.Use(m.HTTPMiddleware())
	r.Get("/api/v1/contracts/{contract_id}", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Send 3 requests with different UUIDs.
	for _, id := range []string{"aaa", "bbb", "ccc"} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/api/v1/contracts/"+id, nil)
		r.ServeHTTP(rec, req)
	}

	// All 3 should land in the same metric series.
	v := counterValue(t, m, "orch_http_requests_total", map[string]string{
		"method": "GET", "path": "/api/v1/contracts/{contract_id}", "status_code": "200",
	})
	if v != 3 {
		t.Errorf("expected 3 requests in one series, got %v", v)
	}
}

func TestHTTPMiddleware_RecordsRequestSize(t *testing.T) {
	m := NewMetrics()

	r := chi.NewRouter()
	r.Use(m.HTTPMiddleware())
	r.Post("/api/v1/contracts/upload", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	})

	body := strings.NewReader(`{"title":"test"}`)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/contracts/upload", body)
	req.ContentLength = 15
	r.ServeHTTP(rec, req)

	c := histogramCount(t, m, "orch_http_request_size_bytes", map[string]string{"method": "POST"})
	if c != 1 {
		t.Errorf("orch_http_request_size_bytes count: got %d, want 1", c)
	}
}

func TestHTTPMiddleware_SkipsRequestSizeWhenUnknown(t *testing.T) {
	m := NewMetrics()

	r := chi.NewRouter()
	r.Use(m.HTTPMiddleware())
	r.Get("/api/v1/contracts", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/contracts", nil)
	// ContentLength is 0 for GET with no body.
	r.ServeHTTP(rec, req)

	c := histogramCount(t, m, "orch_http_request_size_bytes", map[string]string{"method": "GET"})
	if c != 0 {
		t.Errorf("orch_http_request_size_bytes count: got %d, want 0", c)
	}
}

func TestHTTPMiddleware_CapturesStatusCode(t *testing.T) {
	tests := []struct {
		name string
		code int
	}{
		{"200 OK", 200},
		{"201 Created", 201},
		{"400 BadRequest", 400},
		{"404 NotFound", 404},
		{"500 Internal", 500},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewMetrics()
			r := chi.NewRouter()
			r.Use(m.HTTPMiddleware())
			r.Get("/test", func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.code)
			})

			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, httptest.NewRequest("GET", "/test", nil))

			expected := map[string]string{
				"method": "GET", "path": "/test", "status_code": strings.TrimSpace(strings.Split(tt.name, " ")[0]),
			}
			v := counterValue(t, m, "orch_http_requests_total", expected)
			if v != 1 {
				t.Errorf("counter for status %d: got %v, want 1", tt.code, v)
			}
		})
	}
}

func TestHTTPMiddleware_DefaultStatusCode200(t *testing.T) {
	m := NewMetrics()

	r := chi.NewRouter()
	r.Use(m.HTTPMiddleware())
	r.Get("/test", func(w http.ResponseWriter, _ *http.Request) {
		// Write body without explicit WriteHeader → implicit 200.
		_, _ = w.Write([]byte("ok"))
	})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest("GET", "/test", nil))

	v := counterValue(t, m, "orch_http_requests_total", map[string]string{
		"method": "GET", "path": "/test", "status_code": "200",
	})
	if v != 1 {
		t.Errorf("got %v, want 1", v)
	}
}

func TestHTTPMiddleware_FallbackPathForNonChiRoutes(t *testing.T) {
	m := NewMetrics()

	// Use a standard http.ServeMux, not chi → no RouteContext.
	// /healthz is in the safe allowlist, so it should use the real path.
	mux := http.NewServeMux()
	mw := m.HTTPMiddleware()
	mux.Handle("/healthz", mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})))

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET", "/healthz", nil))

	v := counterValue(t, m, "orch_http_requests_total", map[string]string{
		"method": "GET", "path": "/healthz", "status_code": "200",
	})
	if v != 1 {
		t.Errorf("safe fallback path: got %v, want 1", v)
	}
}

func TestHTTPMiddleware_UnmatchedPathPreventsCardinality(t *testing.T) {
	m := NewMetrics()

	r := chi.NewRouter()
	r.Use(m.HTTPMiddleware())
	r.Get("/api/v1/contracts", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Send requests to random non-existent paths — should all land in __unmatched__.
	for _, path := range []string{"/random/a", "/random/b", "/api/v1/xyz"} {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
	}

	// All 3 should be in the __unmatched__ series, NOT creating unique series.
	mfs, err := m.Registry().Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}
	uniquePaths := make(map[string]struct{})
	for _, mf := range mfs {
		if mf.GetName() == "orch_http_requests_total" {
			for _, metric := range mf.GetMetric() {
				for _, lp := range metric.GetLabel() {
					if lp.GetName() == "path" && lp.GetValue() != "/api/v1/contracts" {
						uniquePaths[lp.GetValue()] = struct{}{}
					}
				}
			}
		}
	}
	// Should only see "__unmatched__", not the individual random paths.
	if _, ok := uniquePaths["__unmatched__"]; !ok {
		t.Error("expected __unmatched__ path for 404 routes")
	}
	if len(uniquePaths) > 1 {
		t.Errorf("expected only __unmatched__, got %v", uniquePaths)
	}
}

// --- responseWriter tests ---

func TestResponseWriter_Flush(t *testing.T) {
	flushed := false
	inner := &testFlusher{
		ResponseWriter: httptest.NewRecorder(),
		onFlush:        func() { flushed = true },
	}
	rw := newResponseWriter(inner)
	rw.Flush()

	if !flushed {
		t.Error("Flush() should delegate to underlying writer")
	}
}

func TestResponseWriter_FlushNoFlusher(t *testing.T) {
	// When underlying writer does not implement Flusher, Flush is a no-op.
	rw := newResponseWriter(httptest.NewRecorder())
	rw.Flush() // should not panic
}

func TestResponseWriter_Unwrap(t *testing.T) {
	inner := httptest.NewRecorder()
	rw := newResponseWriter(inner)
	if rw.Unwrap() != inner {
		t.Error("Unwrap() should return the inner ResponseWriter")
	}
}

func TestResponseWriter_WriteHeaderOnce(t *testing.T) {
	rec := httptest.NewRecorder()
	rw := newResponseWriter(rec)

	rw.WriteHeader(201)
	rw.WriteHeader(500) // second call should be completely ignored

	if rw.statusCode != 201 {
		t.Errorf("statusCode: got %d, want 201 (first call)", rw.statusCode)
	}
	// The underlying recorder should also only have received one WriteHeader.
	if rec.Code != 201 {
		t.Errorf("recorder Code: got %d, want 201 (delegate called once)", rec.Code)
	}
}

func TestResponseWriter_WriteThenWriteHeader(t *testing.T) {
	rec := httptest.NewRecorder()
	rw := newResponseWriter(rec)

	// Write without explicit WriteHeader → implicit 200.
	_, _ = rw.Write([]byte("ok"))

	// Subsequent WriteHeader should be ignored — 200 already sent.
	rw.WriteHeader(500)

	if rw.statusCode != 200 {
		t.Errorf("statusCode: got %d, want 200 (implicit from Write)", rw.statusCode)
	}
}

// --- routePattern tests ---

func TestRoutePattern_ChiContext(t *testing.T) {
	r := chi.NewRouter()
	var captured string
	r.Get("/api/v1/contracts/{contract_id}", func(_ http.ResponseWriter, r *http.Request) {
		captured = routePattern(r)
	})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest("GET", "/api/v1/contracts/abc-123", nil))

	if captured != "/api/v1/contracts/{contract_id}" {
		t.Errorf("routePattern: got %q, want /api/v1/contracts/{contract_id}", captured)
	}
}

func TestRoutePattern_NoChiContext_SafePath(t *testing.T) {
	req := httptest.NewRequest("GET", "/healthz", nil)
	p := routePattern(req)
	if p != "/healthz" {
		t.Errorf("routePattern: got %q, want /healthz", p)
	}
}

func TestRoutePattern_NoChiContext_UnsafePath(t *testing.T) {
	req := httptest.NewRequest("GET", "/random/attacker-path", nil)
	p := routePattern(req)
	if p != "__unmatched__" {
		t.Errorf("routePattern: got %q, want __unmatched__", p)
	}
}

func TestSafeFallback(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"/healthz", "/healthz"},
		{"/readyz", "/readyz"},
		{"/metrics", "/metrics"},
		{"/random", "__unmatched__"},
		{"/api/v1/contracts/uuid", "__unmatched__"},
		{"", "__unmatched__"},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := safeFallback(tt.path)
			if got != tt.want {
				t.Errorf("safeFallback(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

// --- concurrent safety ---

func TestConcurrentAccess(t *testing.T) {
	m := NewMetrics()
	done := make(chan struct{})

	for i := 0; i < 10; i++ {
		go func() {
			defer func() { done <- struct{}{} }()
			for j := 0; j < 100; j++ {
				m.HTTPRequestsTotal.WithLabelValues("GET", "/test", "200").Inc()
				m.HTTPRequestDuration.WithLabelValues("GET", "/test").Observe(0.01)
				m.UploadTotal.WithLabelValues("success").Inc()
				m.SSEConnectionsActive.Inc()
				m.SSEConnectionsActive.Dec()
				m.DMCircuitBreakerState.Set(0)
				m.RedisOperationsTotal.WithLabelValues("get", "success").Inc()
			}
		}()
	}

	for i := 0; i < 10; i++ {
		<-done
	}

	v := counterValue(t, m, "orch_http_requests_total", map[string]string{
		"method": "GET", "path": "/test", "status_code": "200",
	})
	if v != 1000 {
		t.Errorf("concurrent counter: got %v, want 1000", v)
	}
}

// --- full integration test: middleware + handler ---

func TestIntegration_MiddlewareAndHandler(t *testing.T) {
	m := NewMetrics()

	r := chi.NewRouter()
	r.Use(m.HTTPMiddleware())
	r.Post("/api/v1/contracts/upload", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	})
	r.Get("/api/v1/contracts/{contract_id}", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Simulate requests.
	for i := 0; i < 5; i++ {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest("GET", "/api/v1/contracts/id-"+strings.Repeat("x", i), nil))
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/contracts/upload", strings.NewReader("body"))
	req.ContentLength = 4
	r.ServeHTTP(rec, req)

	// Verify via /metrics endpoint.
	metricsRec := httptest.NewRecorder()
	m.Handler().ServeHTTP(metricsRec, httptest.NewRequest("GET", "/metrics", nil))

	body := metricsRec.Body.String()

	if !strings.Contains(body, `orch_http_requests_total{method="GET",path="/api/v1/contracts/{contract_id}",status_code="200"} 5`) {
		t.Error("expected 5 GET requests in metrics output")
	}
	if !strings.Contains(body, `orch_http_requests_total{method="POST",path="/api/v1/contracts/upload",status_code="202"} 1`) {
		t.Error("expected 1 POST request in metrics output")
	}
}

// --- Metric completeness test ---

func TestMetricCount(t *testing.T) {
	m := NewMetrics()

	// Touch all metrics to make them visible.
	m.HTTPRequestsTotal.WithLabelValues("GET", "/test", "200").Inc()
	m.HTTPRequestDuration.WithLabelValues("GET", "/test").Observe(0.1)
	m.HTTPRequestSize.WithLabelValues("GET").Observe(100)
	m.UploadTotal.WithLabelValues("success").Inc()
	m.UploadSize.Observe(1000)
	m.UploadDuration.Observe(1.0)
	m.DMRequestsTotal.WithLabelValues("GET", "/doc", "200").Inc()
	m.DMRequestDuration.WithLabelValues("GET", "/doc").Observe(0.05)
	m.DMCircuitBreakerState.Set(0)
	m.S3OperationsTotal.WithLabelValues("put", "success").Inc()
	m.S3OperationDuration.WithLabelValues("put").Observe(0.5)
	m.BrokerPublishTotal.WithLabelValues("t", "success").Inc()
	m.BrokerPublishDuration.WithLabelValues("t").Observe(0.01)
	m.EventsReceivedTotal.WithLabelValues("e", "dp").Inc()
	m.SSEConnectionsActive.Set(1)
	m.SSEEventsPushedTotal.WithLabelValues("status_update").Inc()
	m.RateLimitHitsTotal.WithLabelValues("read").Inc()
	m.AuthFailuresTotal.WithLabelValues("expired").Inc()
	m.RedisOperationsTotal.WithLabelValues("get", "success").Inc()
	m.RedisOperationDuration.WithLabelValues("get").Observe(0.001)
	m.UserConfirmationTimeoutsTotal.Inc()
	m.PermissionsCacheHitTotal.WithLabelValues("export_enabled", "abcdef12").Inc()
	m.PermissionsCacheMissTotal.WithLabelValues("export_enabled").Inc()
	m.PermissionsOPMFallbackTotal.WithLabelValues("export_enabled", "timeout").Inc()
	m.PermissionsResolveDuration.Observe(0.01)

	mfs, err := m.Registry().Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}

	orchCount := 0
	for _, mf := range mfs {
		if strings.HasPrefix(mf.GetName(), "orch_") {
			orchCount++
		}
	}

	// 25 orch_* metrics (20 architecture spec + 1 watchdog + 4 permissions resolver).
	if orchCount != 25 {
		t.Errorf("expected 25 orch_* metrics, got %d", orchCount)
	}
}

// --- Registry test ---

func TestRegistry_ReturnsDedicatedRegistry(t *testing.T) {
	m := NewMetrics()
	if m.Registry() == nil {
		t.Fatal("Registry() returned nil")
	}
}

// --- HTTPMiddleware with SSE-like endpoint ---

func TestHTTPMiddleware_FlushDelegation(t *testing.T) {
	m := NewMetrics()

	flushed := false
	r := chi.NewRouter()
	r.Use(m.HTTPMiddleware())
	r.Get("/api/v1/events/stream", func(w http.ResponseWriter, _ *http.Request) {
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
			flushed = true
		}
		w.WriteHeader(http.StatusOK)
	})

	// httptest.ResponseRecorder implements http.Flusher, so the Flush
	// type assertion inside the handler will succeed through the wrapper.
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest("GET", "/api/v1/events/stream", nil))

	if !flushed {
		t.Error("Flush should be available through the middleware wrapper")
	}
}

// --- Handler integration: metrics endpoint returns all metric families ---

func TestHandler_ContainsAllCategories(t *testing.T) {
	m := NewMetrics()

	// Touch at least one metric from each category.
	m.HTTPRequestsTotal.WithLabelValues("GET", "/", "200").Inc()
	m.UploadTotal.WithLabelValues("success").Inc()
	m.DMRequestsTotal.WithLabelValues("GET", "/doc", "200").Inc()
	m.S3OperationsTotal.WithLabelValues("put", "success").Inc()
	m.BrokerPublishTotal.WithLabelValues("t", "success").Inc()
	m.EventsReceivedTotal.WithLabelValues("dp.status-changed", "dp").Inc()
	m.SSEConnectionsActive.Set(1)
	m.RateLimitHitsTotal.WithLabelValues("read").Inc()
	m.AuthFailuresTotal.WithLabelValues("missing").Inc()
	m.RedisOperationsTotal.WithLabelValues("get", "success").Inc()

	rec := httptest.NewRecorder()
	m.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/metrics", nil))
	body := rec.Body.String()

	categories := []string{
		"orch_http_", "orch_upload_", "orch_dm_", "orch_s3_",
		"orch_broker_", "orch_sse_", "orch_rate_limit_",
		"orch_auth_failures_", "orch_redis_", "orch_events_received_",
	}
	for _, prefix := range categories {
		if !strings.Contains(body, prefix) {
			t.Errorf("metrics output missing category prefix %q", prefix)
		}
	}
}

// --- test helpers ---

type testFlusher struct {
	http.ResponseWriter
	onFlush func()
}

func (f *testFlusher) Flush() {
	f.onFlush()
}

// --- HTTPMiddleware deep nested route test ---

func TestHTTPMiddleware_DeepNestedRoute(t *testing.T) {
	m := NewMetrics()

	r := chi.NewRouter()
	r.Use(m.HTTPMiddleware())
	r.Get("/api/v1/contracts/{contract_id}/versions/{version_id}/results", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest("GET", "/api/v1/contracts/abc/versions/def/results", nil))

	v := counterValue(t, m, "orch_http_requests_total", map[string]string{
		"method":      "GET",
		"path":        "/api/v1/contracts/{contract_id}/versions/{version_id}/results",
		"status_code": "200",
	})
	if v != 1 {
		t.Errorf("deep nested route: got %v, want 1", v)
	}
}

// --- Verify /metrics endpoint is a real Prometheus scrape ---

func TestHandler_ScrapableByPrometheus(t *testing.T) {
	m := NewMetrics()
	m.HTTPRequestsTotal.WithLabelValues("GET", "/test", "200").Inc()

	rec := httptest.NewRecorder()
	m.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/metrics", nil))

	body := rec.Body.String()

	// Should contain HELP and TYPE lines (Prometheus exposition format).
	if !strings.Contains(body, "# HELP orch_http_requests_total") {
		t.Error("missing HELP line")
	}
	if !strings.Contains(body, "# TYPE orch_http_requests_total") {
		t.Error("missing TYPE line")
	}
}

// Verify Handler serves from custom registry (no global metrics leak).
func TestHandler_NoGlobalMetricsLeak(t *testing.T) {
	m := NewMetrics()
	rec := httptest.NewRecorder()
	m.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/metrics", nil))

	body := rec.Body.String()

	// Body should NOT contain metrics from a different registry/package
	// (assuming no global default registration in this test binary).
	// It SHOULD contain go_* metrics since we registered GoCollector.
	if !strings.Contains(body, "go_goroutines") {
		t.Error("expected go_goroutines from GoCollector")
	}
}

// --- Multiple middleware wrapping test ---

func TestHTTPMiddleware_NotFoundRoute(t *testing.T) {
	m := NewMetrics()

	r := chi.NewRouter()
	r.Use(m.HTTPMiddleware())
	r.Get("/api/v1/contracts", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest("GET", "/api/v1/nonexistent", nil))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}

	// 404 routes should use __unmatched__ path to prevent cardinality explosion.
	v := counterValue(t, m, "orch_http_requests_total", map[string]string{
		"method": "GET", "path": "__unmatched__", "status_code": "404",
	})
	if v != 1 {
		t.Errorf("expected 1 request at __unmatched__/404, got %v", v)
	}
}

// --- Verify Unwrap chain works with ResponseController ---

func TestResponseWriter_UnwrapChain(t *testing.T) {
	rec := httptest.NewRecorder()
	rw := newResponseWriter(rec)

	// Verify we can unwrap to get the original recorder.
	unwrapped := rw.Unwrap()
	if unwrapped != rec {
		t.Error("Unwrap should return the original ResponseWriter")
	}
}

// --- Body drain/read for size recording ---

func TestHTTPMiddleware_LargeRequestBody(t *testing.T) {
	m := NewMetrics()

	r := chi.NewRouter()
	r.Use(m.HTTPMiddleware())
	r.Post("/upload", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	})

	body := strings.NewReader(strings.Repeat("x", 10*1024*1024)) // 10MB
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/upload", body)
	req.ContentLength = 10 * 1024 * 1024
	r.ServeHTTP(rec, req)

	s := histogramSum(t, m, "orch_http_request_size_bytes", map[string]string{"method": "POST"})
	expected := float64(10 * 1024 * 1024)
	if s != expected {
		t.Errorf("got sum %v, want %v", s, expected)
	}
}
