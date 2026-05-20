package dm

// Deps bundles the requester's collaborators (the dmawaiter.Deps /
// pipeline.Deps / pendingconfirmation.Deps precedent). Publisher is
// REQUIRED — silent-swallowing publish failures on lic.requests.artifacts
// would block the pipeline awaiter without a single log line or metric, so
// withDefaults intentionally does NOT substitute a noop for it (build-spec
// D2); the constructor enforces non-nil. The remaining three fields are
// optional with zero-dependency noop defaults so the common production
// wiring (LIC-TASK-036 / TASK-047) passes only the real adapters and tests
// override exactly what they need.
type Deps struct {
	// Publisher — the broker seam. REQUIRED (no noop default);
	// constructor returns an error on nil (build-spec D2).
	Publisher Publisher
	// Metrics — the lic_publisher_messages_total counter, called
	// unconditionally on every exit path (build-spec D7). nil ⇒ noopMetrics.
	Metrics Metrics
	// Clock — deterministic time for the RFC3339Nano timestamp stamping
	// (build-spec D5). nil ⇒ systemClock (UTC wall clock).
	Clock Clock
	// Logger — structured WARN / ERROR seam (build-spec D15). nil ⇒
	// noopLogger. Reserved for future use; the current hot path does
	// NOT call it.
	Logger Logger
}

// withDefaults substitutes a zero-dependency noop for every nil OPTIONAL
// seam and returns the completed Deps so the hot path never nil-checks a
// seam (the dmawaiter.Deps.withDefaults / pipeline.Deps.withDefaults pattern
// verbatim). Publisher is deliberately NOT substituted — see Deps.Publisher
// godoc. The constructor's non-nil check runs AFTER withDefaults so the
// produced error is the authoritative wiring-defect signal.
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
