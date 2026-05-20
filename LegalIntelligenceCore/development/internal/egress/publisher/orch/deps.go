package orch

// StatusPublisherDeps bundles the StatusPublisher's collaborators
// (LIC-TASK-044 build-spec; the dmawaiter.Deps / pipeline.Deps /
// pendingconfirmation.Deps / dm.PublisherDeps precedent). Publisher is
// REQUIRED — silent-swallowing publish failures on
// lic.events.status-changed would make every status transition invisible to
// the Orchestrator (no broker, no log, no metric), so withDefaults
// intentionally does NOT substitute a noop for it; the constructor enforces
// non-nil. The remaining three fields are optional with zero-dependency
// noop defaults so the common production wiring (LIC-TASK-036 / TASK-047)
// passes only the real adapters and tests override exactly what they need.
type StatusPublisherDeps struct {
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
// seam and returns the completed StatusPublisherDeps so the hot path never
// nil-checks a seam (the dmawaiter.Deps.withDefaults / pipeline.Deps.
// withDefaults / dm.PublisherDeps.withDefaults pattern verbatim). Publisher
// is deliberately NOT substituted — see StatusPublisherDeps.Publisher
// godoc. The constructor's non-nil check runs AFTER withDefaults so the
// produced error is the authoritative wiring-defect signal.
func (d StatusPublisherDeps) withDefaults() StatusPublisherDeps {
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

// UncertaintyPublisherDeps bundles the UncertaintyPublisher's
// collaborators (LIC-TASK-045). Structurally identical to
// StatusPublisherDeps — both share the same Publisher / Metrics / Clock
// / Logger seam stack (per orch package's hermetic boundary). Two
// disambiguating types (vs. one shared bundle) keep call-sites
// self-documenting at the LIC-TASK-036 / TASK-047 wiring layer: the
// type at the call site names which publisher is being assembled.
type UncertaintyPublisherDeps struct {
	// Publisher — the broker seam. REQUIRED (no noop default);
	// constructor returns an error on nil. Silent-swallow on
	// lic.events.classification-uncertain would block every pause from
	// reaching the Orchestrator — the user would never see the
	// type-confirmation prompt and the run would dead-end at the 25h
	// pending-state TTL.
	Publisher Publisher
	// Metrics — the lic_publisher_messages_total{topic, outcome}
	// counter, called unconditionally on every exit path. nil ⇒
	// noopMetrics. Same SHAPE as StatusPublisherDeps.Metrics (one
	// IncPublish method, no size histogram).
	Metrics Metrics
	// Clock — deterministic time for the RFC3339Nano Timestamp
	// stamping. nil ⇒ systemClock (UTC wall clock).
	Clock Clock
	// Logger — structured WARN / ERROR seam. nil ⇒ noopLogger.
	// Reserved for future use; the current hot path does NOT call it.
	Logger Logger
}

// withDefaults substitutes a zero-dependency noop for every nil
// OPTIONAL seam (same pattern as StatusPublisherDeps.withDefaults).
// Publisher is deliberately NOT substituted — the constructor's
// non-nil check runs AFTER withDefaults and is the authoritative
// wiring-defect signal.
func (d UncertaintyPublisherDeps) withDefaults() UncertaintyPublisherDeps {
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
