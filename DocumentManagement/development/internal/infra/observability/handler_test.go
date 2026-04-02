package observability

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
)

func TestNewMetricsHandler_ServesMetrics(t *testing.T) {
	reg := prometheus.NewRegistry()

	// Register a test counter.
	counter := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "test_metric_total",
		Help: "A test counter.",
	})
	reg.MustRegister(counter)
	counter.Inc()

	handler := NewMetricsHandler(reg)

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rr := httptest.NewRecorder()
	handler.Mux().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status code: got %d, want %d", rr.Code, http.StatusOK)
	}

	body := rr.Body.String()
	if !strings.Contains(body, "test_metric_total") {
		t.Fatal("response should contain test_metric_total")
	}
}

func TestNewMetricsHandler_NilRegistry(t *testing.T) {
	handler := NewMetricsHandler(nil)

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rr := httptest.NewRecorder()
	handler.Mux().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("nil registry: status code: got %d, want %d", rr.Code, http.StatusOK)
	}
}

func TestNewMetricsHandler_IntegrationWithMetrics(t *testing.T) {
	m := NewMetrics()
	m.IncEventsReceived("test-topic")

	handler := NewMetricsHandler(m.Registry())

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rr := httptest.NewRecorder()
	handler.Mux().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d", rr.Code, http.StatusOK)
	}

	body := rr.Body.String()
	if !strings.Contains(body, "dm_events_received_total") {
		t.Fatal("response should contain dm_events_received_total")
	}
}

func TestMetricsHandler_Mux_NotNil(t *testing.T) {
	handler := NewMetricsHandler(nil)
	if handler.Mux() == nil {
		t.Fatal("Mux() should not be nil")
	}
}
