package metrics

import "github.com/prometheus/client_golang/prometheus"

// DMMetrics — observability.md §3.5.
//
// LIC speaks to DM purely via RabbitMQ request/response; latency here
// captures the wall-clock between request publish and response delivery.
type DMMetrics struct {
	// RequestDurationSeconds — round-trip latency for a DM request,
	// labelled by operation (get_artifacts | persist_artifacts).
	RequestDurationSeconds *prometheus.HistogramVec

	// RequestOutcomeTotal — terminal outcome of a DM request.
	RequestOutcomeTotal *prometheus.CounterVec

	// ArtifactsReceivedSizeBytes — size of the artifact payload returned
	// by DM (used to detect oversized contracts pre-ingest).
	ArtifactsReceivedSizeBytes prometheus.Histogram

	// ArtifactsPublishedSizeBytes — size of LegalAnalysisArtifactsReady
	// payload published to DM.
	ArtifactsPublishedSizeBytes prometheus.Histogram
}

func newDMMetrics() *DMMetrics {
	return &DMMetrics{
		RequestDurationSeconds: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "lic_dm_request_duration_seconds",
			Help:    "Duration of a DM request (publish → response).",
			Buckets: dmRequestDurationBuckets(),
		}, []string{"op"}),

		RequestOutcomeTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "lic_dm_request_outcome_total",
			Help: "DM request outcomes by operation.",
		}, []string{"op", "outcome"}),

		ArtifactsReceivedSizeBytes: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "lic_dm_artifacts_received_size_bytes",
			Help:    "Size of artifact payloads received from DM.",
			Buckets: artifactsSizeBytesBuckets(),
		}),

		ArtifactsPublishedSizeBytes: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "lic_dm_artifacts_published_size_bytes",
			Help:    "Size of LegalAnalysisArtifactsReady payload published to DM.",
			Buckets: artifactsSizeBytesBuckets(),
		}),
	}
}

func (d *DMMetrics) mustRegister(reg *prometheus.Registry) {
	reg.MustRegister(
		d.RequestDurationSeconds,
		d.RequestOutcomeTotal,
		d.ArtifactsReceivedSizeBytes,
		d.ArtifactsPublishedSizeBytes,
	)
}
