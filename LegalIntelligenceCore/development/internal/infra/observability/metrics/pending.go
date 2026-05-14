package metrics

import "github.com/prometheus/client_golang/prometheus"

// PendingMetrics — observability.md §3.7.
//
// Gauges are scraped periodically; the orchestrator updates them from
// the lic-pending-state:* Redis namespace either on a tick or after
// each Pause/Resume call (whichever fits the implementation).
type PendingMetrics struct {
	// StateCount — current number of pending-type-confirmation records.
	StateCount prometheus.Gauge

	// StateAgeSecondsMax — age of the oldest pending record in seconds.
	// Drives the LICStuckPendingState alert at 22h (§6).
	StateAgeSecondsMax prometheus.Gauge

	// UserConfirmationReceivedTotal — terminal outcome when a
	// UserConfirmedType command arrives.
	UserConfirmationReceivedTotal *prometheus.CounterVec
}

func newPendingMetrics() *PendingMetrics {
	return &PendingMetrics{
		StateCount: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "lic_pending_state_count",
			Help: "Number of pending type-confirmation records in Redis.",
		}),

		StateAgeSecondsMax: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "lic_pending_state_age_seconds_max",
			Help: "Age of the oldest pending type-confirmation record in seconds.",
		}),

		UserConfirmationReceivedTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "lic_user_confirmation_received_total",
			Help: "Number of UserConfirmedType commands received, by outcome.",
		}, []string{"outcome"}),
	}
}

func (p *PendingMetrics) mustRegister(reg *prometheus.Registry) {
	reg.MustRegister(p.StateCount, p.StateAgeSecondsMax, p.UserConfirmationReceivedTotal)
}
