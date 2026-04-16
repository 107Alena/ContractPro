package observability

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

func TestNewMetrics_AllRegistered(t *testing.T) {
	m := NewMetrics()
	if m.Registry() == nil {
		t.Fatal("Registry() is nil")
	}

	// Gather all metrics — verifies everything was registered.
	families, err := m.Registry().Gather()
	if err != nil {
		t.Fatalf("Gather() error: %v", err)
	}

	wantNames := []string{
		"dm_events_received_total",
		"dm_events_processed_total",
		"dm_event_processing_duration_seconds",
		"dm_artifacts_stored_total",
		"dm_api_requests_total",
		"dm_api_request_duration_seconds",
		"dm_outbox_pending_count",
		"dm_outbox_oldest_pending_age_seconds",
		"dm_outbox_published_total",
		"dm_outbox_publish_failed_total",
		"dm_outbox_cleaned_up_total",
		"dm_dlq_messages_total",
		"dm_missing_version_id_total",
		"dm_idempotency_fallback_total",
		"dm_idempotency_check_total",
		"dm_stuck_versions_count",
		"dm_stuck_versions_total",
		"dm_integrity_check_failures_total",
		"dm_circuit_breaker_state",
	}

	// Build a set of gathered names. Note: counters/histograms won't
	// appear until they have at least one observation, so we seed them.
	m.IncEventsReceived("test-topic")
	m.IncEventsProcessed("test-topic", "success")
	m.EventProcessingDuration.WithLabelValues("test-topic").Observe(0.1)
	m.ArtifactsStored.WithLabelValues("dp", "OCR_RAW").Inc()
	m.APIRequests.WithLabelValues("GET", "/healthz", "200").Inc()
	m.APIRequestDuration.WithLabelValues("GET", "/healthz").Observe(0.01)
	m.SetPendingCount(5)
	m.SetOldestPendingAge(10.5)
	m.IncPublished("test-topic")
	m.IncPublishFailed("test-topic")
	m.IncCleanedUp(3)
	m.DLQMessages.WithLabelValues("parse_error").Inc()
	m.MissingVersionIDTotal.Inc()
	m.IncFallbackTotal("test-topic")
	m.IncCheckTotal("process")
	m.SetStuckVersionsCount("processing", 2)
	m.IncStuckVersionsTotal("processing", 1)
	m.IntegrityCheckFailures.Inc()
	m.CircuitBreakerState.WithLabelValues("objectstorage").Set(0)

	families, err = m.Registry().Gather()
	if err != nil {
		t.Fatalf("Gather() error after seeding: %v", err)
	}

	nameSet := make(map[string]bool)
	for _, f := range families {
		nameSet[f.GetName()] = true
	}

	for _, name := range wantNames {
		if !nameSet[name] {
			t.Errorf("metric %q not found in gathered families", name)
		}
	}
}

func TestNewMetrics_MetricTypes(t *testing.T) {
	m := NewMetrics()

	// Seed metrics.
	m.IncEventsReceived("t")
	m.EventProcessingDuration.WithLabelValues("t").Observe(1)
	m.ArtifactsStored.WithLabelValues("dp", "OCR_RAW").Inc()
	m.APIRequests.WithLabelValues("GET", "/", "200").Inc()
	m.APIRequestDuration.WithLabelValues("GET", "/").Observe(0.01)
	m.DLQMessages.WithLabelValues("r").Inc()
	m.IncFallbackTotal("t")
	m.IncCheckTotal("process")
	m.IncPublished("t")
	m.IncPublishFailed("t")
	m.SetStuckVersionsCount("processing", 0)
	m.IncStuckVersionsTotal("processing", 1)
	m.CircuitBreakerState.WithLabelValues("s3").Set(0)

	families, _ := m.Registry().Gather()
	typeMap := make(map[string]dto.MetricType)
	for _, f := range families {
		typeMap[f.GetName()] = f.GetType()
	}

	counters := []string{
		"dm_events_received_total",
		"dm_events_processed_total",
		"dm_artifacts_stored_total",
		"dm_api_requests_total",
		"dm_outbox_published_total",
		"dm_outbox_publish_failed_total",
		"dm_outbox_cleaned_up_total",
		"dm_dlq_messages_total",
		"dm_missing_version_id_total",
		"dm_idempotency_fallback_total",
		"dm_idempotency_check_total",
		"dm_stuck_versions_total",
		"dm_integrity_check_failures_total",
	}

	histograms := []string{
		"dm_event_processing_duration_seconds",
		"dm_api_request_duration_seconds",
	}

	gauges := []string{
		"dm_outbox_pending_count",
		"dm_outbox_oldest_pending_age_seconds",
		"dm_stuck_versions_count",
		"dm_circuit_breaker_state",
	}

	for _, name := range counters {
		if got := typeMap[name]; got != dto.MetricType_COUNTER {
			t.Errorf("%s: got type %v, want COUNTER", name, got)
		}
	}
	for _, name := range histograms {
		if got := typeMap[name]; got != dto.MetricType_HISTOGRAM {
			t.Errorf("%s: got type %v, want HISTOGRAM", name, got)
		}
	}
	for _, name := range gauges {
		if got := typeMap[name]; got != dto.MetricType_GAUGE {
			t.Errorf("%s: got type %v, want GAUGE", name, got)
		}
	}
}

// --- Consumer MetricsCollector interface tests ---

func TestMetrics_IncEventsReceived(t *testing.T) {
	m := NewMetrics()
	m.IncEventsReceived("dp.artifacts.processing-ready")
	m.IncEventsReceived("dp.artifacts.processing-ready")
	m.IncEventsReceived("lic.artifacts.analysis-ready")

	assertCounterValue(t, m.EventsReceived.WithLabelValues("dp.artifacts.processing-ready"), 2)
	assertCounterValue(t, m.EventsReceived.WithLabelValues("lic.artifacts.analysis-ready"), 1)
}

func TestMetrics_IncEventsProcessed(t *testing.T) {
	m := NewMetrics()
	m.IncEventsProcessed("topic-1", "success")
	m.IncEventsProcessed("topic-1", "error")

	assertCounterValue(t, m.EventsProcessed.WithLabelValues("topic-1", "success"), 1)
	assertCounterValue(t, m.EventsProcessed.WithLabelValues("topic-1", "error"), 1)
}

// --- Idempotency MetricsCollector interface tests ---

func TestMetrics_IncFallbackTotal(t *testing.T) {
	m := NewMetrics()
	m.IncFallbackTotal("some-topic")

	assertCounterValue(t, m.IdempotencyFallbackTotal.WithLabelValues("some-topic"), 1)
}

func TestMetrics_IncCheckTotal(t *testing.T) {
	m := NewMetrics()
	m.IncCheckTotal("process")
	m.IncCheckTotal("skip")
	m.IncCheckTotal("process")

	assertCounterValue(t, m.IdempotencyCheckTotal.WithLabelValues("process"), 2)
	assertCounterValue(t, m.IdempotencyCheckTotal.WithLabelValues("skip"), 1)
}

// --- Outbox OutboxMetrics interface tests ---

func TestMetrics_SetPendingCount(t *testing.T) {
	m := NewMetrics()
	m.SetPendingCount(42)

	assertGaugeValue(t, m.OutboxPendingCount, 42)
}

func TestMetrics_SetOldestPendingAge(t *testing.T) {
	m := NewMetrics()
	m.SetOldestPendingAge(123.5)

	assertGaugeValue(t, m.OutboxOldestPendingAge, 123.5)
}

func TestMetrics_IncPublished(t *testing.T) {
	m := NewMetrics()
	m.IncPublished("topic-a")
	m.IncPublished("topic-a")

	assertCounterValue(t, m.OutboxPublished.WithLabelValues("topic-a"), 2)
}

func TestMetrics_IncPublishFailed(t *testing.T) {
	m := NewMetrics()
	m.IncPublishFailed("topic-b")

	assertCounterValue(t, m.OutboxPublishFailed.WithLabelValues("topic-b"), 1)
}

func TestMetrics_IncCleanedUp(t *testing.T) {
	m := NewMetrics()
	m.IncCleanedUp(10)
	m.IncCleanedUp(5)

	assertCounterValue(t, m.OutboxCleanedUp, 15)
}

func TestMetrics_IncCleanedUp_Zero(t *testing.T) {
	m := NewMetrics()
	m.IncCleanedUp(0) // should not panic

	assertCounterValue(t, m.OutboxCleanedUp, 0)
}

func TestMetrics_IncCleanedUp_Negative(t *testing.T) {
	m := NewMetrics()
	m.IncCleanedUp(-1) // should not panic — negative values are ignored

	assertCounterValue(t, m.OutboxCleanedUp, 0)
}

// --- helpers ---

func assertCounterValue(t *testing.T, c prometheus.Collector, want float64) {
	t.Helper()
	counter, ok := c.(prometheus.Counter)
	if !ok {
		t.Fatal("not a counter")
	}
	var m dto.Metric
	if err := counter.Write(&m); err != nil {
		t.Fatalf("Write() error: %v", err)
	}
	if got := m.GetCounter().GetValue(); got != want {
		t.Fatalf("counter value: got %v, want %v", got, want)
	}
}

func assertGaugeValue(t *testing.T, g prometheus.Gauge, want float64) {
	t.Helper()
	var m dto.Metric
	if err := g.Write(&m); err != nil {
		t.Fatalf("Write() error: %v", err)
	}
	if got := m.GetGauge().GetValue(); got != want {
		t.Fatalf("gauge value: got %v, want %v", got, want)
	}
}
