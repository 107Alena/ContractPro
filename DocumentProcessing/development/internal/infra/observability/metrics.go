package observability

import "github.com/prometheus/client_golang/prometheus"

// Metrics holds all Prometheus metrics for the Document Processing service.
// A dedicated prometheus.Registry is used to avoid polluting the global
// default registry, which simplifies testing and handler wiring.
type Metrics struct {
	// JobDuration tracks end-to-end processing duration per job outcome.
	JobDuration *prometheus.HistogramVec

	// JobStatusTotal counts completed jobs by final status.
	JobStatusTotal *prometheus.CounterVec

	// OCRDuration tracks OCR call latency by outcome.
	OCRDuration *prometheus.HistogramVec

	// ConcurrentJobsActive tracks how many jobs are executing right now.
	ConcurrentJobsActive prometheus.Gauge

	// FileSizeBytes records the size of incoming PDF files.
	FileSizeBytes prometheus.Histogram

	registry *prometheus.Registry
}

// NewMetrics creates and registers all Document Processing metrics
// with a dedicated Prometheus registry.
func NewMetrics() *Metrics {
	reg := prometheus.NewRegistry()

	m := &Metrics{
		JobDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "dp_job_duration_seconds",
			Help:    "End-to-end processing duration of a document processing job.",
			Buckets: []float64{0.5, 1, 2, 5, 10, 30, 60, 120},
		}, []string{"status"}),

		JobStatusTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "dp_job_status_total",
			Help: "Total number of document processing jobs by final status.",
		}, []string{"status"}),

		OCRDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "dp_ocr_duration_seconds",
			Help:    "Duration of OCR service calls.",
			Buckets: []float64{0.5, 1, 2, 5, 10, 30, 60},
		}, []string{"ocr_status"}),

		ConcurrentJobsActive: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "dp_concurrent_jobs_active",
			Help: "Number of document processing jobs currently in progress.",
		}),

		FileSizeBytes: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "dp_file_size_bytes",
			Help:    "Size of incoming PDF files in bytes.",
			Buckets: []float64{102400, 512000, 1048576, 2097152, 5242880, 10485760, 20971520},
		}),

		registry: reg,
	}

	reg.MustRegister(
		m.JobDuration,
		m.JobStatusTotal,
		m.OCRDuration,
		m.ConcurrentJobsActive,
		m.FileSizeBytes,
	)

	return m
}

// Registry returns the dedicated Prometheus registry containing all DP
// metrics. Pass it to promhttp.HandlerFor() to expose the /metrics endpoint.
func (m *Metrics) Registry() *prometheus.Registry {
	return m.registry
}
