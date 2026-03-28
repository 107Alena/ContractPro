package observability

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMetricsHandler_Returns200(t *testing.T) {
	t.Parallel()

	m := NewMetrics()
	h := NewMetricsHandler(m.Registry())

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	h.Mux().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("/metrics: want status 200, got %d", rec.Code)
	}
}

func TestMetricsHandler_ContainsRegisteredMetrics(t *testing.T) {
	t.Parallel()

	m := NewMetrics()
	// Record observations so metrics appear in output.
	m.JobDuration.WithLabelValues("COMPLETED").Observe(1.5)
	m.JobStatusTotal.WithLabelValues("COMPLETED").Inc()
	m.OCRDuration.WithLabelValues("ok").Observe(0.5)
	m.ConcurrentJobsActive.Set(2)
	m.ConcurrentJobsWaiting.Set(1)
	m.FileSizeBytes.Observe(1048576)

	h := NewMetricsHandler(m.Registry())

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	h.Mux().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("/metrics: want status 200, got %d", rec.Code)
	}

	body := rec.Body.String()
	wantMetrics := []string{
		"dp_job_duration_seconds",
		"dp_job_status_total",
		"dp_ocr_duration_seconds",
		"dp_concurrent_jobs_active",
		"dp_concurrent_jobs_waiting",
		"dp_file_size_bytes",
	}
	for _, metric := range wantMetrics {
		if !strings.Contains(body, metric) {
			t.Errorf("/metrics: response body does not contain %q", metric)
		}
	}
}

func TestMetricsHandler_ContentType(t *testing.T) {
	t.Parallel()

	m := NewMetrics()
	h := NewMetricsHandler(m.Registry())

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	h.Mux().ServeHTTP(rec, req)

	ct := rec.Header().Get("Content-Type")
	// promhttp returns "text/plain; version=0.0.4; charset=utf-8" by default.
	if !strings.HasPrefix(ct, "text/plain") {
		t.Fatalf("/metrics: want Content-Type starting with \"text/plain\", got %q", ct)
	}
}

func TestMetricsHandler_EmptyRegistryStillReturns200(t *testing.T) {
	t.Parallel()

	// A fresh Metrics instance (no observations recorded) should still respond 200.
	m := NewMetrics()
	h := NewMetricsHandler(m.Registry())

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	h.Mux().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("/metrics (empty): want status 200, got %d", rec.Code)
	}
}

func TestMetricsHandler_NilRegistry(t *testing.T) {
	t.Parallel()

	// A nil registry must not panic — it is replaced with an empty registry.
	h := NewMetricsHandler(nil)

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	h.Mux().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("/metrics (nil registry): want status 200, got %d", rec.Code)
	}
}

func TestMetricsHandler_UnknownPathReturns404(t *testing.T) {
	t.Parallel()

	m := NewMetrics()
	h := NewMetricsHandler(m.Registry())

	req := httptest.NewRequest(http.MethodGet, "/unknown", nil)
	rec := httptest.NewRecorder()
	h.Mux().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("/unknown: want status 404, got %d", rec.Code)
	}
}
