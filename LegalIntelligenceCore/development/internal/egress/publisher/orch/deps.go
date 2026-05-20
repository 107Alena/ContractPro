package orch

// PublisherDeps bundles the StatusPublisher's collaborators (LIC-TASK-044
// build-spec; the dmawaiter.Deps / pipeline.Deps / pendingconfirmation.Deps
// / dm.PublisherDeps precedent). Publisher is REQUIRED — silent-swallowing
// publish failures on lic.events.status-changed would make every status
// transition invisible to the Orchestrator (no broker, no log, no metric),
// so withDefaults intentionally does NOT substitute a noop for it; the
// constructor enforces non-nil. The remaining three fields are optional with
// zero-dependency noop defaults so the common production wiring
// (LIC-TASK-036 / TASK-047) passes only the real adapters and tests
// override exactly what they need.
type PublisherDeps struct {
	// Publisher — the broker seam. REQUIRED (no noop default);
	// constructor returns an error on nil.
	Publisher Publisher
	// Metrics — the lic_publisher_messages_total counter, called
	// unconditionally on every exit path. nil ⇒ noopMetrics. UNLIKE the
	// sibling dm publisher's Metrics seam, this one carries ONLY
	// IncPublish (no §3.5 size histogram — that metric is specific to the
	// terminal analysis-ready payload).
	Metrics Metrics
	// Clock — deterministic time for the RFC3339Nano timestamp stamping.
	// nil ⇒ systemClock (UTC wall clock).
	Clock Clock
	// Logger — structured WARN / ERROR seam. nil ⇒ noopLogger. Reserved
	// for future use; the current hot path does NOT call it.
	Logger Logger
}

// withDefaults substitutes a zero-dependency noop for every nil OPTIONAL
// seam and returns the completed PublisherDeps so the hot path never
// nil-checks a seam (the dmawaiter.Deps.withDefaults / pipeline.Deps.
// withDefaults / dm.PublisherDeps.withDefaults pattern verbatim). Publisher
// is deliberately NOT substituted — see PublisherDeps.Publisher godoc. The
// constructor's non-nil check runs AFTER withDefaults so the produced error
// is the authoritative wiring-defect signal.
func (d PublisherDeps) withDefaults() PublisherDeps {
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
