package metrics

import "github.com/prometheus/client_golang/prometheus"

// IdempotencyMetrics — observability.md §3.6.
type IdempotencyMetrics struct {
	// LookupsTotal — outcome of an idempotency check
	// (new|in_progress|completed|fallback_db).
	LookupsTotal *prometheus.CounterVec

	// FallbackTotal — Redis unreachable: caller fell back to the
	// degraded path (rare; alarms on any sustained rate).
	FallbackTotal prometheus.Counter
}

func newIdempotencyMetrics() *IdempotencyMetrics {
	return &IdempotencyMetrics{
		LookupsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "lic_idempotency_lookups_total",
			Help: "Idempotency key lookup outcomes.",
		}, []string{"result"}),

		FallbackTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "lic_idempotency_fallback_total",
			Help: "Idempotency check fell back to a degraded path (Redis unreachable).",
		}),
	}
}

func (i *IdempotencyMetrics) mustRegister(reg *prometheus.Registry) {
	reg.MustRegister(i.LookupsTotal, i.FallbackTotal)
}
