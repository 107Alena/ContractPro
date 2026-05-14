// Package metrics provides a Prometheus metrics registry for the
// Legal Intelligence Core service. Metric names follow the lic_ prefix
// convention defined in architecture/observability.md §3.
//
// The package exposes a Metrics aggregator that groups metrics by domain
// (Pipeline, Agent, LLM, DM, Idempotency, Pending, DLQ, CrossCut, BuildInfo).
// Each call-site receives only the sub-group it needs, which keeps the
// dependency footprint narrow and simplifies testing.
//
// Cardinality discipline (observability.md §3.10):
//   - organization_id MUST NOT appear in any Prometheus label.
//   - Allowed labels are bounded enums (Outcome, Stage, AgentID, ProviderID,
//     model name from a small pricing table). See labels.go for the
//     authoritative enum values.
//
// All metrics are registered on a dedicated *prometheus.Registry returned
// by Registry(); the default global registry is left untouched. This makes
// the package safe to use in parallel unit tests.
package metrics

import "github.com/prometheus/client_golang/prometheus"

// Metrics aggregates every Prometheus metric exposed by lic-service.
//
// Sub-groups are accessed as fields (m.Pipeline, m.Agent, ...) — this gives
// call-sites a narrow handle and avoids a flat namespace of 35+ fields.
// Each sub-group is constructed and registered in New().
type Metrics struct {
	Pipeline    *PipelineMetrics
	Agent       *AgentMetrics
	LLM         *LLMMetrics
	DM          *DMMetrics
	Idempotency *IdempotencyMetrics
	Pending     *PendingMetrics
	DLQ         *DLQMetrics
	CrossCut    *CrossCutMetrics
	BuildInfo   *BuildInfoMetric

	registry *prometheus.Registry
}

// BuildInfo carries the immutable identifiers of the running binary.
// It is exported as the lic_build_info{version,commit,go_version} gauge,
// always set to 1, so dashboards can pivot by deployed version.
type BuildInfo struct {
	Version   string
	Commit    string
	GoVersion string
}

// New constructs the full Metrics aggregator, creates a dedicated
// *prometheus.Registry, registers every metric on it, and seeds
// lic_build_info with the supplied BuildInfo.
//
// Returning a fresh registry (rather than touching prometheus.DefaultRegisterer)
// keeps tests hermetic and lets the caller wire a promhttp.HandlerFor()
// against the exact set of LIC metrics.
func New(info BuildInfo) *Metrics {
	reg := prometheus.NewRegistry()

	m := &Metrics{
		Pipeline:    newPipelineMetrics(),
		Agent:       newAgentMetrics(),
		LLM:         newLLMMetrics(),
		DM:          newDMMetrics(),
		Idempotency: newIdempotencyMetrics(),
		Pending:     newPendingMetrics(),
		DLQ:         newDLQMetrics(),
		CrossCut:    newCrossCutMetrics(),
		BuildInfo:   newBuildInfoMetric(info),
		registry:    reg,
	}

	m.Pipeline.mustRegister(reg)
	m.Agent.mustRegister(reg)
	m.LLM.mustRegister(reg)
	m.DM.mustRegister(reg)
	m.Idempotency.mustRegister(reg)
	m.Pending.mustRegister(reg)
	m.DLQ.mustRegister(reg)
	m.CrossCut.mustRegister(reg)
	m.BuildInfo.mustRegister(reg)

	return m
}

// Registry returns the dedicated registry holding all LIC metrics.
// Pass it to promhttp.HandlerFor() to expose /metrics.
func (m *Metrics) Registry() *prometheus.Registry {
	return m.registry
}
