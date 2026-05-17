package aggregator

// Metrics is the telemetry seam. The Prometheus CounterVec
// lic_prompt_injection_detected_total{agent} is centrally declared in
// metrics.CrossCutMetrics; this package inverts it behind this interface and
// a noop default so the Result Aggregator stays hermetic (stdlib +
// internal/domain/model only) before LIC-TASK-047 wires Prometheus — exactly
// the schemavalidator.Metrics / cost.Recorder / router.Metrics precedent (an
// internal/infra/observability/metrics import here would break the
// hermeticity invariant the rest of internal/* upholds before wiring).
//
// The agent label value MUST come from a typed source only
// (model.AgentID.String()) so the {agent} cardinality stays bounded (the 5
// flag-carrying agents — matches metrics/crosscut.go []string{"agent"} and
// observability.md §3.9 "9 series, ничтожно").
type Metrics interface {
	// PromptInjectionDetected increments
	// lic_prompt_injection_detected_total{agent}. Called EXACTLY once per
	// agent whose prompt_injection_detected flag is true (high-architecture.md
	// §6.11 step 6: "Метрика lic_prompt_injection_detected_total{agent}
	// инкрементируется per-agent"). Never called for non-detecting agents.
	PromptInjectionDetected(agent string)
}

// noopMetrics is the zero-dependency default so Aggregator is usable in tests
// and before LIC-TASK-047 wires Prometheus, without a nil check on every
// telemetry call (mirrors schemavalidator.noopMetrics / cost.noopRecorder).
type noopMetrics struct{}

func (noopMetrics) PromptInjectionDetected(string) {}

var _ Metrics = noopMetrics{}
