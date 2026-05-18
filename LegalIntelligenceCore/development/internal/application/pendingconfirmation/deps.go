package pendingconfirmation

// Deps bundles the optional-with-noop telemetry / runtime collaborators (the
// pipeline.Deps / stages.Deps precedent). A nil field degrades to its
// zero-dependency noop so the common production wiring (LIC-TASK-047) passes
// only the real adapters and tests override exactly what they need. The five
// frozen-wire ports and the PipelineResumer seam are positional NewManager
// params (REQUIRED, fail-fast non-nil — build-spec D11/D16), NOT in Deps:
// keeping them off Deps keeps the optional/required split honest.
type Deps struct {
	// Metrics — the three lic_pending_*/lic_user_confirmation_* series. nil ⇒
	// noopMetrics.
	Metrics Metrics
	// Clock — deterministic time for event Timestamps. nil ⇒ systemClock
	// (UTC wall clock).
	Clock Clock
	// Logger — structured INFO (audit trail, build-spec D20) / WARN / ERROR.
	// nil ⇒ noopLogger.
	Logger Logger
	// TraceRestorer — saved-W3C-context → ctx for cross-pause span linkage.
	// nil ⇒ noopTraceRestorer (ctx unchanged).
	TraceRestorer TraceRestorer
}

// withDefaults substitutes a zero-dependency noop for every nil seam and
// returns the completed Deps so the Manager never nil-checks a seam on the
// hot path (the pipeline.Deps.withDefaults pattern verbatim).
func (d Deps) withDefaults() Deps {
	if d.Metrics == nil {
		d.Metrics = noopMetrics{}
	}
	if d.Clock == nil {
		d.Clock = systemClock{}
	}
	if d.Logger == nil {
		d.Logger = noopLogger{}
	}
	if d.TraceRestorer == nil {
		d.TraceRestorer = noopTraceRestorer{}
	}
	return d
}
