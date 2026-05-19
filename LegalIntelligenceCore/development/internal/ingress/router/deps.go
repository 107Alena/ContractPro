package router

// Deps bundles the optional-with-noop telemetry / runtime collaborators (the
// pipeline.Deps / pendingconfirmation.Deps / consumer.Deps / idempotency.Deps
// precedent). A nil field degrades to its zero-dependency noop so the common
// production wiring (LIC-TASK-047) passes only the real adapters and tests
// override exactly what they need. The 8 required collaborators (Config-
// validated value, PipelineRunner, PendingConfirmationManager,
// ArtifactsAwaiterDeliverer, PersistConfirmationDeliverer,
// VersionMetaCacheWriter, IdempotencyGuard, port.PendingStatePort,
// port.StatusPublisherPort) are positional NewRouter params (REQUIRED,
// fail-fast non-nil — build-spec D2), NOT in Deps: keeping them off Deps
// keeps the optional/required split honest.
type Deps struct {
	// Metrics — the reserved Router decision-counter seam (build-spec
	// D11/D12/R5). nil ⇒ noopMetrics. v1 emits nothing on every impl; the
	// seam shape is forward-committed.
	Metrics Metrics
	// Clock — deterministic time for the §6.5:631 LICStatusChangedEvent
	// Timestamp. nil ⇒ systemClock (UTC wall clock).
	Clock Clock
	// Logger — structured WARN / ERROR (build-spec PART F #5; no Info in
	// v1). nil ⇒ noopLogger.
	Logger Logger
	// Tracer — per-route ingress span. nil ⇒ noopTracer (no tracing
	// surface). LIC-TASK-047 wires an OTEL adapter.
	Tracer Tracer
}

// withDefaults substitutes a zero-dependency noop for every nil seam and
// returns the completed Deps so the Router never nil-checks a seam on the
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
	if d.Tracer == nil {
		d.Tracer = noopTracer{}
	}
	return d
}
