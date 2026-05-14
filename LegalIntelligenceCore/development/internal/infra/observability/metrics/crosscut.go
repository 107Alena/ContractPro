package metrics

import "github.com/prometheus/client_golang/prometheus"

// CrossCutMetrics — observability.md §3.9.
//
// Holds metrics that don't fit a single pipeline-stage or LLM concern.
// `prompt_injection_detected_total` is intentionally per-agent (9 series);
// severity is *not* a label by C-lite policy (OQ-13).
type CrossCutMetrics struct {
	// PromptInjectionDetectedTotal — counter increments when any agent
	// sets prompt_injection_detected=true in its response.
	PromptInjectionDetectedTotal *prometheus.CounterVec

	// PartyValidationTotal — INN/OGRN checksum results from
	// high-architecture.md §6.7.2.
	PartyValidationTotal *prometheus.CounterVec

	// ConsumerMessagesTotal — observability.md §3.9: per-topic delivery
	// outcome on the inbound side.
	ConsumerMessagesTotal *prometheus.CounterVec

	// PublisherMessagesTotal — outbound publish outcome.
	PublisherMessagesTotal *prometheus.CounterVec

	// CircuitBreakerState — generic circuit breaker gauge for arbitrary
	// components (not LLM-specific; e.g. broker conn or redis).
	CircuitBreakerState *prometheus.GaugeVec
}

func newCrossCutMetrics() *CrossCutMetrics {
	return &CrossCutMetrics{
		PromptInjectionDetectedTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "lic_prompt_injection_detected_total",
			Help: "Number of agent responses where prompt_injection_detected=true.",
		}, []string{"agent"}),

		PartyValidationTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "lic_party_validation_total",
			Help: "INN/OGRN deterministic checksum results.",
		}, []string{"type", "valid"}),

		ConsumerMessagesTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "lic_consumer_messages_total",
			Help: "Messages received by the broker consumer, by topic and outcome.",
		}, []string{"topic", "outcome"}),

		PublisherMessagesTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "lic_publisher_messages_total",
			Help: "Messages published to the broker, by topic and outcome.",
		}, []string{"topic", "outcome"}),

		CircuitBreakerState: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "lic_circuit_breaker_state",
			Help: "Circuit breaker state per component (0=closed, 1=half_open, 2=open).",
		}, []string{"component"}),
	}
}

func (c *CrossCutMetrics) mustRegister(reg *prometheus.Registry) {
	reg.MustRegister(
		c.PromptInjectionDetectedTotal,
		c.PartyValidationTotal,
		c.ConsumerMessagesTotal,
		c.PublisherMessagesTotal,
		c.CircuitBreakerState,
	)
}
