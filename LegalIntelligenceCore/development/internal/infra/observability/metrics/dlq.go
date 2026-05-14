package metrics

import "github.com/prometheus/client_golang/prometheus"

// DLQMetrics — observability.md §3.8.
type DLQMetrics struct {
	// PublishedTotal — increments on each DLQ envelope published.
	// `reason` is a small bounded enum: invalid_envelope, agent_output_invalid,
	// publish_failed, consumer_failed, etc. Keep cardinality tight.
	PublishedTotal *prometheus.CounterVec
}

func newDLQMetrics() *DLQMetrics {
	return &DLQMetrics{
		PublishedTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "lic_dlq_published_total",
			Help: "Number of messages published to LIC dead-letter queues.",
		}, []string{"topic", "reason"}),
	}
}

func (d *DLQMetrics) mustRegister(reg *prometheus.Registry) {
	reg.MustRegister(d.PublishedTotal)
}
