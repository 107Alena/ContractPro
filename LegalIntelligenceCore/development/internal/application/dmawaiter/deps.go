package dmawaiter

// Deps bundles the optional-with-noop telemetry / runtime collaborators (the
// pipeline.Deps / pendingconfirmation.Deps / router.Deps precedent). A nil
// field degrades to its zero-dependency noop so the common production wiring
// (LIC-TASK-047) passes only the real adapters and tests override exactly
// what they need. The two awaiters have NO required positional collaborators
// beyond their respective Config — the in-process registry has no port to
// inject (the broker side dispatches INTO the awaiter via the locally-
// declared Handler / Deliverer methods, NOT via an outbound port). Build-spec
// D9/D10.
//
// One shared Deps shape is used by BOTH constructors (NewArtifactAwaiter and
// NewConfirmationAwaiter) — the per-Config split (ArtifactConfig vs
// ConfirmationConfig, build-spec D8) already documents the env-var binding,
// so a per-Deps split is redundant.
type Deps struct {
	// Metrics — the two lic_dm_request_* series, collapsed into one
	// per-Await RecordOutcome call (build-spec D11). nil ⇒ noopMetrics.
	Metrics Metrics
	// Clock — deterministic time for the duration metric's start/end
	// (build-spec D14). nil ⇒ systemClock (UTC wall clock).
	Clock Clock
	// Logger — structured WARN (registry miss / duplicate deliver) /
	// ERROR (defensive registry-inconsistency) — build-spec D15. nil ⇒
	// noopLogger.
	Logger Logger
}

// withDefaults substitutes a zero-dependency noop for every nil seam and
// returns the completed Deps so the awaiter never nil-checks a seam on the
// hot path (the pipeline.Deps.withDefaults / pendingconfirmation.Deps.
// withDefaults pattern verbatim).
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
	return d
}
