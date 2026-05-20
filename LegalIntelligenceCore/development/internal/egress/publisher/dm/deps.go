package dm

// RequesterDeps bundles the ArtifactRequester's collaborators (the
// dmawaiter.Deps / pipeline.Deps / pendingconfirmation.Deps precedent).
// Publisher is REQUIRED — silent-swallowing publish failures on
// lic.requests.artifacts would block the pipeline awaiter without a single
// log line or metric, so withDefaults intentionally does NOT substitute a
// noop for it (build-spec D2); the constructor enforces non-nil. The
// remaining three fields are optional with zero-dependency noop defaults so
// the common production wiring (LIC-TASK-036 / TASK-047) passes only the
// real adapters and tests override exactly what they need.
//
// Symmetric to PublisherDeps below (LIC-TASK-043 build-spec D2) — both
// publishers in this package share the seam shape and the
// required-Publisher / optional-rest contract.
type RequesterDeps struct {
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
// seam and returns the completed RequesterDeps so the hot path never
// nil-checks a seam (the dmawaiter.Deps.withDefaults / pipeline.Deps.
// withDefaults pattern verbatim). Publisher is deliberately NOT
// substituted — see RequesterDeps.Publisher godoc. The constructor's
// non-nil check runs AFTER withDefaults so the produced error is the
// authoritative wiring-defect signal.
func (d RequesterDeps) withDefaults() RequesterDeps {
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

// PublisherDeps bundles the AnalysisArtifactsPublisher's collaborators
// (LIC-TASK-043 build-spec D2). Identical shape to RequesterDeps —
// Publisher REQUIRED (no noop default; silent-swallow on
// lic.artifacts.analysis-ready would lose the terminal payload of the
// pipeline and break the §6.5 step 9 persist-confirmation contract), the
// remaining three optional with noop defaults.
//
// Kept as a distinct type (not aliased) so the two publishers in this
// package each have a self-documenting Deps name at the call site of their
// constructor — `dm.NewAnalysisArtifactsPublisher(cfg, dm.PublisherDeps{...})`
// vs `dm.NewArtifactRequester(cfg, dm.RequesterDeps{...})` — which makes
// the LIC-TASK-036 / TASK-047 wiring read cleanly.
type PublisherDeps struct {
	// Publisher — the broker seam. REQUIRED (no noop default);
	// constructor returns an error on nil.
	Publisher Publisher
	// Metrics — the lic_publisher_messages_total counter PLUS the
	// lic_dm_artifacts_published_size_bytes histogram. Called
	// unconditionally on every exit path. nil ⇒ noopMetrics.
	Metrics Metrics
	// Clock — deterministic time for the RFC3339Nano timestamp stamping.
	// nil ⇒ systemClock (UTC wall clock).
	Clock Clock
	// Logger — structured WARN / ERROR seam. nil ⇒ noopLogger. Reserved
	// for future use; the current hot path does NOT call it.
	Logger Logger
}

// withDefaults substitutes a zero-dependency noop for every nil OPTIONAL
// seam and returns the completed PublisherDeps (symmetric to
// RequesterDeps.withDefaults). Publisher is deliberately NOT substituted
// — see PublisherDeps.Publisher godoc.
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
