package metrics

import "github.com/prometheus/client_golang/prometheus"

// BuildInfoMetric — observability.md §3.9. `lic_build_info{version,commit,go_version}`
// is a constant gauge always set to 1; its purpose is to carry deployment
// metadata into Prometheus so dashboards/alerts can pivot by version.
type BuildInfoMetric struct {
	gauge *prometheus.GaugeVec
	info  BuildInfo
}

// buildInfoUnknownLabel is substituted for any empty BuildInfo field.
// Empty labels in Prometheus silently break version-pivoting dashboards;
// "unknown" makes the gap visible without panicking on partial config.
const buildInfoUnknownLabel = "unknown"

// normalizeBuildInfo replaces empty fields with "unknown" so the seeded
// series carries non-empty labels regardless of whether main wired the
// build metadata correctly.
func normalizeBuildInfo(info BuildInfo) BuildInfo {
	if info.Version == "" {
		info.Version = buildInfoUnknownLabel
	}
	if info.Commit == "" {
		info.Commit = buildInfoUnknownLabel
	}
	if info.GoVersion == "" {
		info.GoVersion = buildInfoUnknownLabel
	}
	return info
}

func newBuildInfoMetric(info BuildInfo) *BuildInfoMetric {
	info = normalizeBuildInfo(info)

	g := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "lic_build_info",
		Help: "Build identifiers of the running lic-service binary; always 1.",
	}, []string{"version", "commit", "go_version"})

	// Seed exactly one series — the running binary's identity. Per spec
	// the gauge is constant at 1; we never call .Set() again.
	g.WithLabelValues(info.Version, info.Commit, info.GoVersion).Set(1)

	return &BuildInfoMetric{gauge: g, info: info}
}

func (b *BuildInfoMetric) mustRegister(reg *prometheus.Registry) {
	reg.MustRegister(b.gauge)
}

// Info returns the BuildInfo this metric was constructed with. Fields are
// normalized — empty inputs are replaced with "unknown". Returned by
// callers that want the seeded labels without re-walking the registry.
func (b *BuildInfoMetric) Info() BuildInfo { return b.info }
