package dlq

// Deps bundles the DLQPublisher's collaborators (the dmawaiter.Deps /
// pipeline.Deps / pendingconfirmation.Deps / dm.PublisherDeps /
// orch.StatusPublisherDeps precedent). Publisher is REQUIRED — silent-
// swallowing publish failures on lic.dlq.* would make every failure
// invisible to ops, defeat the §11 LICDLQGrowth alert, and break the §9.3
// post-mortem channel — so withDefaults intentionally does NOT substitute
// a noop for it; the constructor enforces non-nil. The remaining three
// fields are optional with zero-dependency noop defaults so the common
// production wiring (LIC-TASK-047) passes only the real adapters and
// tests override exactly what they need.
type Deps struct {
	// Publisher — the broker seam. REQUIRED (no noop default);
	// constructor returns an error on nil.
	Publisher Publisher
	// Metrics — the lic_publisher_messages_total + lic_dlq_published_total
	// counter pair, called per the IncPublish / IncDLQPublished contracts
	// in seams.go. nil ⇒ noopMetrics.
	Metrics Metrics
	// Clock — deterministic time for the FailedAt-if-empty stamp. nil ⇒
	// systemClock (UTC wall clock). Asymmetry vs the dm / orch
	// publishers: see seams.go.Clock godoc.
	Clock Clock
	// Logger — structured WARN / ERROR seam. nil ⇒ noopLogger. Reserved
	// for future use; the current hot path does NOT call it (see
	// seams.go.Logger godoc — publish-failed payload logging is the
	// CALLER's responsibility, not the publisher's).
	Logger Logger
}

// withDefaults substitutes a zero-dependency noop for every nil OPTIONAL
// seam and returns the completed Deps so the hot path never nil-checks a
// seam (the dmawaiter.Deps.withDefaults / pipeline.Deps.withDefaults /
// dm.PublisherDeps.withDefaults / orch.StatusPublisherDeps.withDefaults
// pattern verbatim). Publisher is deliberately NOT substituted — see
// Deps.Publisher godoc. The constructor's non-nil check runs AFTER
// withDefaults so the produced error is the authoritative wiring-defect
// signal.
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
