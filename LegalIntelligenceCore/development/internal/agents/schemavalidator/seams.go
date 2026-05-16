package schemavalidator

// RepairOutcome is the lic_agent_repair_outcome_total{outcome} label value
// (error-handling.md §5.5, observability.md §3.3). It is a LOCAL MIRROR of
// metrics.AgentRepairOutcome — declared here, not imported, so this package
// does not pull internal/infra/observability/metrics before app-wiring
// (same hermeticity-of-telemetry choice as cost.Outcome / router.CallOutcome).
// seams_test.go pins the three wire strings against the shipped
// metrics/labels.go SSOT so the mirror cannot silently drift.
type RepairOutcome string

const (
	// OutcomeRepairedOK — the repair call returned content that passed
	// schema validation on the second attempt.
	OutcomeRepairedOK RepairOutcome = "repaired_ok"
	// OutcomeRepairFailed — the repair call succeeded at the wire level but
	// its content still failed schema validation (hard limit N=1).
	OutcomeRepairFailed RepairOutcome = "repair_failed"
	// OutcomeProviderError — the repair call itself failed at the provider
	// (sticky CompleteRepair returned a non-nil error).
	OutcomeProviderError RepairOutcome = "repair_provider_error"
)

// String returns the wire representation of the repair outcome.
func (o RepairOutcome) String() string { return string(o) }

// IsValid reports whether o is one of the three declared repair outcomes.
func (o RepairOutcome) IsValid() bool {
	switch o {
	case OutcomeRepairedOK, OutcomeRepairFailed, OutcomeProviderError:
		return true
	default:
		return false
	}
}

// Metrics is the telemetry seam. The Prometheus CounterVecs
// lic_agent_repair_attempts_total{agent,provider} and
// lic_agent_repair_outcome_total{agent,provider,outcome} are centrally
// declared in metrics.AgentMetrics; this package inverts them behind this
// interface and a noop default, exactly like cost.Recorder / router.Metrics.
// The concrete adapter over *metrics.AgentMetrics is wired in LIC-TASK-047
// (an internal/infra/observability/metrics import here would break the
// hermeticity invariant the rest of internal/* upholds before wiring).
//
// agent and provider label values MUST come from typed sources only
// (model.AgentID.String() / port.LLMProviderID.String()) so the
// {agent,provider,outcome} cardinality stays bounded as metrics/agent.go
// budgets (9 × 3 × 3 = 81 series worst case).
type Metrics interface {
	// RepairAttempt increments lic_agent_repair_attempts_total{agent,
	// provider}. Called EXACTLY once per repair turn issued — never on the
	// happy path where the primary response already validated
	// (code-architect MF-4; agent.go SSOT "incremented each time we issue a
	// repair turn").
	RepairAttempt(agent, provider string)

	// RepairOutcome increments lic_agent_repair_outcome_total{agent,
	// provider,outcome}. Called exactly once per repair turn, after the
	// outcome is known (repaired_ok | repair_failed | repair_provider_error).
	RepairOutcome(agent, provider, outcome string)
}

// noopMetrics is the zero-dependency default so RepairLoop is usable in tests
// and before LIC-TASK-047 wires Prometheus, without a nil check on every
// telemetry call (mirrors cost.noopRecorder / router noop seams).
type noopMetrics struct{}

func (noopMetrics) RepairAttempt(string, string)         {}
func (noopMetrics) RepairOutcome(string, string, string) {}

var _ Metrics = noopMetrics{}
