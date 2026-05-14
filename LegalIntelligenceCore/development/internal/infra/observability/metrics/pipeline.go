package metrics

import "github.com/prometheus/client_golang/prometheus"

// PipelineMetrics — observability.md §3.2.
//
// `mode` is a binary label (INITIAL|RE_CHECK), explicitly chosen over the
// 5-value origin_type to keep cardinality bounded. The richer origin_type
// goes into OTel span attributes (§4.3), not Prometheus.
type PipelineMetrics struct {
	// StartedTotal — number of pipelines launched (Run() entry).
	StartedTotal *prometheus.CounterVec

	// TotalDurationSeconds — wall-clock end-to-end pipeline duration.
	TotalDurationSeconds *prometheus.HistogramVec

	// OutcomeTotal — terminal outcome counter; error_code stays empty
	// for success/timeout outcomes by convention.
	OutcomeTotal *prometheus.CounterVec

	// ConcurrentJobs — gauge of in-flight pipelines on this instance.
	ConcurrentJobs prometheus.Gauge

	// StageDurationSeconds — per-stage histogram. Stage label values
	// come from domain/model/status.go (STAGE_*).
	StageDurationSeconds *prometheus.HistogramVec
}

func newPipelineMetrics() *PipelineMetrics {
	return &PipelineMetrics{
		StartedTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "lic_pipeline_started_total",
			Help: "Number of LIC pipeline runs started.",
		}, []string{"mode"}),

		TotalDurationSeconds: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "lic_pipeline_total_duration_seconds",
			Help:    "End-to-end LIC pipeline duration.",
			Buckets: pipelineDurationBuckets(),
		}, []string{"mode", "outcome"}),

		OutcomeTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "lic_pipeline_outcome_total",
			Help: "Number of LIC pipelines by terminal outcome and error_code.",
		}, []string{"mode", "outcome", "error_code"}),

		ConcurrentJobs: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "lic_pipeline_concurrent_jobs",
			Help: "Number of LIC pipelines currently running on this instance.",
		}),

		StageDurationSeconds: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "lic_pipeline_stage_duration_seconds",
			Help:    "Duration of each LIC pipeline stage.",
			Buckets: pipelineStageDurationBuckets(),
		}, []string{"stage"}),
	}
}

func (p *PipelineMetrics) mustRegister(reg *prometheus.Registry) {
	reg.MustRegister(
		p.StartedTotal,
		p.TotalDurationSeconds,
		p.OutcomeTotal,
		p.ConcurrentJobs,
		p.StageDurationSeconds,
	)
}
