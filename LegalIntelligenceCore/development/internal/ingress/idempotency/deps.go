package idempotency

// Deps bundles the optional-with-noop telemetry / runtime collaborators (the
// consumer.Deps / pendingconfirmation.Deps precedent — build-spec D2). A nil
// field degrades to its zero-dependency noop so the common production wiring
// (LIC-TASK-047) passes only the real adapters and tests override exactly
// what they need.
//
// RedisSeam is a positional/required NewGuard param (REQUIRED, fail-fast
// non-nil — build-spec D2), NOT in Deps: keeping it off Deps keeps the
// optional/required split honest (the consumer/pendingconfirmation
// "required collaborators are positional, NOT in Deps" rule). Config is a
// value param (D9).
type Deps struct {
	// Metrics — the lic_idempotency_lookups_total{result} /
	// lic_idempotency_fallback_total series. nil ⇒ noopMetrics.
	Metrics Metrics
	// Clock — the deterministic ticker source for StartHeartbeat (D6). nil ⇒
	// systemClock (time.NewTicker behind the Ticker seam).
	Clock Clock
	// Logger — structured WARN / ERROR for the fallback alert (R1) and the
	// heartbeat vanished/transient (D6). nil ⇒ noopLogger.
	Logger Logger
}

// withDefaults substitutes a zero-dependency noop for every nil seam and
// returns the completed Deps so the Guard never nil-checks a seam on the hot
// path (the consumer.Deps.withDefaults / pendingconfirmation.Deps.withDefaults
// pattern verbatim).
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
