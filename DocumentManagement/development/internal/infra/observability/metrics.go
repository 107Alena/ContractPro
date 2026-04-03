package observability

import "github.com/prometheus/client_golang/prometheus"

// Metrics holds all Prometheus metrics for the Document Management service.
// A dedicated prometheus.Registry is used to avoid polluting the global
// default registry, which simplifies testing and handler wiring.
//
// Metrics implements the consumer-side interfaces defined across DM packages:
//   - consumer.MetricsCollector (IncEventsReceived, IncEventsProcessed)
//   - idempotency.MetricsCollector (IncFallbackTotal, IncCheckTotal)
//   - outbox.OutboxMetrics (SetPendingCount, SetOldestPendingAge, IncPublished, IncPublishFailed, IncCleanedUp)
type Metrics struct {
	// --- Event processing ---

	// EventsReceived counts incoming events by topic.
	EventsReceived *prometheus.CounterVec

	// EventsProcessed counts processed events by topic and status (success/error).
	EventsProcessed *prometheus.CounterVec

	// EventProcessingDuration tracks event processing latency by topic.
	EventProcessingDuration *prometheus.HistogramVec

	// --- Artifacts ---

	// ArtifactsStored counts stored artifacts by producer and artifact_type.
	ArtifactsStored *prometheus.CounterVec

	// --- Sync API ---

	// APIRequests counts HTTP API requests by method, path, and status_code.
	APIRequests *prometheus.CounterVec

	// APIRequestDuration tracks HTTP API request latency by method and path.
	APIRequestDuration *prometheus.HistogramVec

	// --- Outbox ---

	// OutboxPendingCount is the number of PENDING events in the outbox table.
	OutboxPendingCount prometheus.Gauge

	// OutboxOldestPendingAge is the age in seconds of the oldest PENDING
	// event in the outbox (REV-022).
	OutboxOldestPendingAge prometheus.Gauge

	// OutboxPublished counts events successfully published from outbox by topic.
	OutboxPublished *prometheus.CounterVec

	// OutboxPublishFailed counts failed outbox publish attempts by topic.
	OutboxPublishFailed *prometheus.CounterVec

	// OutboxCleanedUp counts cleaned-up CONFIRMED outbox entries.
	OutboxCleanedUp prometheus.Counter

	// --- DLQ ---

	// DLQMessages counts messages sent to the dead-letter queue by reason.
	DLQMessages *prometheus.CounterVec

	// --- Defensive fallbacks ---

	// MissingVersionIDTotal counts events received without version_id
	// that required a fallback lookup.
	MissingVersionIDTotal prometheus.Counter

	// IdempotencyFallbackTotal counts idempotency checks that fell back
	// from Redis to DB, by topic.
	IdempotencyFallbackTotal *prometheus.CounterVec

	// IdempotencyCheckTotal counts idempotency check outcomes by result
	// (process/skip/reprocess/error).
	IdempotencyCheckTotal *prometheus.CounterVec

	// --- Version health ---

	// StuckVersionsCount is the current number of versions stuck in an
	// intermediate artifact_status.
	StuckVersionsCount prometheus.Gauge

	// --- Data integrity ---

	// IntegrityCheckFailures counts content hash mismatches when reading artifacts.
	IntegrityCheckFailures prometheus.Counter

	// --- Tenant isolation ---

	// TenantMismatchTotal counts events where the claimed organization_id
	// did not match the document's actual owner (BRE-015 violation).
	TenantMismatchTotal prometheus.Counter

	// --- Circuit breaker ---

	// CircuitBreakerState tracks the circuit breaker state (0=closed, 1=half-open, 2=open)
	// per component.
	CircuitBreakerState *prometheus.GaugeVec

	registry *prometheus.Registry
}

// NewMetrics creates and registers all Document Management metrics
// with a dedicated Prometheus registry.
func NewMetrics() *Metrics {
	reg := prometheus.NewRegistry()

	m := &Metrics{
		EventsReceived: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "dm_events_received_total",
			Help: "Total number of incoming events received by topic.",
		}, []string{"topic"}),

		EventsProcessed: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "dm_events_processed_total",
			Help: "Total number of events processed by topic and outcome status.",
		}, []string{"topic", "status"}),

		EventProcessingDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "dm_event_processing_duration_seconds",
			Help:    "Duration of event processing in seconds.",
			Buckets: []float64{0.01, 0.05, 0.1, 0.25, 0.5, 1, 2, 5, 10, 30},
		}, []string{"topic"}),

		ArtifactsStored: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "dm_artifacts_stored_total",
			Help: "Total number of artifacts stored by producer domain and type.",
		}, []string{"producer", "artifact_type"}),

		APIRequests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "dm_api_requests_total",
			Help: "Total number of HTTP API requests by method, path, and status code.",
		}, []string{"method", "path", "status_code"}),

		APIRequestDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "dm_api_request_duration_seconds",
			Help:    "Duration of HTTP API requests in seconds.",
			Buckets: []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5},
		}, []string{"method", "path"}),

		OutboxPendingCount: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "dm_outbox_pending_count",
			Help: "Current number of PENDING events in the outbox table.",
		}),

		OutboxOldestPendingAge: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "dm_outbox_oldest_pending_age_seconds",
			Help: "Age in seconds of the oldest PENDING event in the outbox (REV-022).",
		}),

		OutboxPublished: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "dm_outbox_published_total",
			Help: "Total number of events successfully published from outbox by topic.",
		}, []string{"topic"}),

		OutboxPublishFailed: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "dm_outbox_publish_failed_total",
			Help: "Total number of failed outbox publish attempts by topic.",
		}, []string{"topic"}),

		OutboxCleanedUp: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "dm_outbox_cleaned_up_total",
			Help: "Total number of CONFIRMED outbox entries cleaned up.",
		}),

		DLQMessages: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "dm_dlq_messages_total",
			Help: "Total number of messages sent to the dead-letter queue by reason.",
		}, []string{"reason"}),

		MissingVersionIDTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "dm_missing_version_id_total",
			Help: "Total number of events received without version_id requiring fallback lookup.",
		}),

		IdempotencyFallbackTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "dm_idempotency_fallback_total",
			Help: "Total number of idempotency checks that fell back from Redis to DB.",
		}, []string{"topic"}),

		IdempotencyCheckTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "dm_idempotency_check_total",
			Help: "Total number of idempotency check outcomes by result.",
		}, []string{"result"}),

		StuckVersionsCount: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "dm_stuck_versions_count",
			Help: "Current number of versions stuck in an intermediate artifact status.",
		}),

		IntegrityCheckFailures: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "dm_integrity_check_failures_total",
			Help: "Total number of content hash mismatches when reading artifacts.",
		}),

		TenantMismatchTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "dm_tenant_mismatch_total",
			Help: "Total number of events with organization_id mismatch (BRE-015).",
		}),

		CircuitBreakerState: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "dm_circuit_breaker_state",
			Help: "Circuit breaker state per component (0=closed, 1=half-open, 2=open).",
		}, []string{"component"}),

		registry: reg,
	}

	reg.MustRegister(
		m.EventsReceived,
		m.EventsProcessed,
		m.EventProcessingDuration,
		m.ArtifactsStored,
		m.APIRequests,
		m.APIRequestDuration,
		m.OutboxPendingCount,
		m.OutboxOldestPendingAge,
		m.OutboxPublished,
		m.OutboxPublishFailed,
		m.OutboxCleanedUp,
		m.DLQMessages,
		m.MissingVersionIDTotal,
		m.IdempotencyFallbackTotal,
		m.IdempotencyCheckTotal,
		m.StuckVersionsCount,
		m.IntegrityCheckFailures,
		m.TenantMismatchTotal,
		m.CircuitBreakerState,
	)

	return m
}

// Registry returns the dedicated Prometheus registry containing all DM
// metrics. Pass it to promhttp.HandlerFor() to expose the /metrics endpoint.
func (m *Metrics) Registry() *prometheus.Registry {
	return m.registry
}

// ---------------------------------------------------------------------------
// consumer.MetricsCollector interface
// ---------------------------------------------------------------------------

// IncEventsReceived increments dm_events_received_total for the given topic.
func (m *Metrics) IncEventsReceived(topic string) {
	m.EventsReceived.WithLabelValues(topic).Inc()
}

// IncEventsProcessed increments dm_events_processed_total for the given
// topic and status.
func (m *Metrics) IncEventsProcessed(topic string, status string) {
	m.EventsProcessed.WithLabelValues(topic, status).Inc()
}

// ---------------------------------------------------------------------------
// idempotency.MetricsCollector interface
// ---------------------------------------------------------------------------

// IncFallbackTotal increments dm_idempotency_fallback_total for the given topic.
func (m *Metrics) IncFallbackTotal(topic string) {
	m.IdempotencyFallbackTotal.WithLabelValues(topic).Inc()
}

// IncCheckTotal increments dm_idempotency_check_total for the given result.
func (m *Metrics) IncCheckTotal(result string) {
	m.IdempotencyCheckTotal.WithLabelValues(result).Inc()
}

// ---------------------------------------------------------------------------
// outbox.OutboxMetrics interface
// ---------------------------------------------------------------------------

// SetPendingCount sets the dm_outbox_pending_count gauge.
func (m *Metrics) SetPendingCount(count float64) {
	m.OutboxPendingCount.Set(count)
}

// SetOldestPendingAge sets the dm_outbox_oldest_pending_age_seconds gauge.
func (m *Metrics) SetOldestPendingAge(ageSeconds float64) {
	m.OutboxOldestPendingAge.Set(ageSeconds)
}

// IncPublished increments dm_outbox_published_total for the given topic.
func (m *Metrics) IncPublished(topic string) {
	m.OutboxPublished.WithLabelValues(topic).Inc()
}

// IncPublishFailed increments dm_outbox_publish_failed_total for the given topic.
func (m *Metrics) IncPublishFailed(topic string) {
	m.OutboxPublishFailed.WithLabelValues(topic).Inc()
}

// IncCleanedUp increments dm_outbox_cleaned_up_total by the given count.
// Non-positive values are ignored (prometheus.Counter.Add panics on negative).
func (m *Metrics) IncCleanedUp(count int64) {
	if count > 0 {
		m.OutboxCleanedUp.Add(float64(count))
	}
}

// ---------------------------------------------------------------------------
// ingestion.FallbackMetrics interface (REV-001/REV-002)
// ---------------------------------------------------------------------------

// IncMissingVersionID increments dm_missing_version_id_total.
// Called when an incoming event lacks version_id and a DB fallback is used.
func (m *Metrics) IncMissingVersionID() {
	m.MissingVersionIDTotal.Inc()
}

// ---------------------------------------------------------------------------
// dlq.DLQMetrics interface
// ---------------------------------------------------------------------------

// IncDLQMessages increments dm_dlq_messages_total for the given reason.
func (m *Metrics) IncDLQMessages(reason string) {
	m.DLQMessages.WithLabelValues(reason).Inc()
}

// ---------------------------------------------------------------------------
// tenant.Metrics interface (BRE-015)
// ---------------------------------------------------------------------------

// IncTenantMismatch increments dm_tenant_mismatch_total.
// Called when an incoming event's organization_id does not match the
// document's actual owner.
func (m *Metrics) IncTenantMismatch() {
	m.TenantMismatchTotal.Inc()
}
