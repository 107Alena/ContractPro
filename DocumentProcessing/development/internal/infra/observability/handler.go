package observability

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// MetricsHandler serves the Prometheus /metrics endpoint using a dedicated
// registry. It mirrors the Handler pattern from the health package.
type MetricsHandler struct {
	mux *http.ServeMux
}

// NewMetricsHandler creates a handler that exposes the given registry
// on /metrics via promhttp.HandlerFor. A nil registry is replaced with
// an empty one to avoid a nil-pointer panic.
func NewMetricsHandler(reg *prometheus.Registry) *MetricsHandler {
	if reg == nil {
		reg = prometheus.NewRegistry()
	}
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))

	return &MetricsHandler{mux: mux}
}

// Mux returns the http.ServeMux for use with http.Server.
func (h *MetricsHandler) Mux() *http.ServeMux { return h.mux }
