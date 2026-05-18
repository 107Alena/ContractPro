package pendingconfirmation

import (
	"context"
	"time"

	"contractpro/legal-intelligence-core/internal/domain/model"
)

// This file holds every Pending Type Confirmation Manager SEAM — a local
// interface (plus, where applicable, a zero-dependency noop default) for
// collaborators that are telemetry / runtime-environment, would force a
// forbidden import if depended on concretely, or are the inverted dependency
// edge to the Pipeline Orchestrator (build-spec D11/D13/D14/D16/D20, the
// pipeline.seams.go / aggregator.Metrics precedent). Everything that crosses
// to a frozen cross-domain RabbitMQ/Redis wire is a domain.port instead
// (positional NewManager params, NOT here).
//
// var _ Seam = noop{} assertions follow each pair with a noop (the universal
// B-4 precedent). The pipeline.PauseController / port.UserConfirmedTypeHandler
// structural-satisfaction assertions live in the LIC-TASK-047 WIRING package,
// NOT here — asserting them here would force an internal/application/pipeline
// import and break hermeticity (build-spec D18, the aggregator Repairer-seam /
// pipeline JobLimiter-on-*concurrency.Semaphore precedent).

// PipelineResumer is the seam to the Pipeline Orchestrator's resume
// entrypoint (pipeline.Orchestrator.ResumeAfterConfirmation). Declared
// locally so this package stays hermetic ({model,port}-only — no
// internal/application/pipeline import; build-spec D11/D17). LIC-TASK-047
// wires *pipeline.Orchestrator (which structurally satisfies this) into the
// Manager; the var _ assertion lives in the WIRING package, not here. There
// is NO noop default: a Manager with no resumer cannot resume — NewManager
// fails fast (the orchestrator exec/agg required-collaborator precedent). It
// is a "seam" only in the structural-interface sense (avoids the pipeline
// import); it is a REQUIRED positional NewManager param, NOT in Deps.
type PipelineResumer interface {
	ResumeAfterConfirmation(ctx context.Context, state *model.PipelineState) error
}

// Metrics is the observability.md §3.7 seam for the three pending series.
// Concrete prometheus is forbidden here (hermeticity — the aggregator
// .Metrics / pipeline PipelineMetrics precedent). LIC-TASK-047 wires an
// adapter over *metrics.PendingMetrics.
type Metrics interface {
	// PendingStateInc/Dec adjust lic_pending_state_count (a gauge). Inc on a
	// successful Pause Save; Dec on a resumed run reaching COMPLETED. Per-call
	// (NOT a tick): 037 has no background goroutine and is hermetic, and the
	// metric's own SSOT sanctions per-call. The 25h-TTL natural-expiry
	// decrement is the LIC-TASK-047 periodic Redis-SCAN refresher's job
	// (build-spec D14, forward-noted).
	PendingStateInc()
	PendingStateDec()
	// PendingStateAgeMaxSeconds sets lic_pending_state_age_seconds_max. The
	// Manager NEVER calls this — model.PendingTypeConfirmation has no
	// CreatedAt and inventing one breaches the anti-carrier discipline. The
	// method is DECLARED so the adapter contract is complete and the
	// LIC-TASK-047 periodic Redis-namespace scanner can drive it (build-spec
	// D14, forward-noted).
	PendingStateAgeMaxSeconds(seconds float64)
	// UserConfirmation increments lic_user_confirmation_received_total
	// {outcome}. outcome ∈ {"resumed","expired","invalid"} — EXACT, the
	// observability.md:166 SSOT (observability.md:105 is the enum SSOT).
	// Duplicate-delivery and pipeline-failure-after-resume do NOT increment
	// (no enum value exists; not double-counting — build-spec D7/D10/D14).
	UserConfirmation(outcome string)
}

// noopMetrics is the zero-dependency default (the pipeline.noopPipelineMetrics
// precedent) so the hot path never nil-checks.
type noopMetrics struct{}

func (noopMetrics) PendingStateInc()                  {}
func (noopMetrics) PendingStateDec()                  {}
func (noopMetrics) PendingStateAgeMaxSeconds(float64) {}
func (noopMetrics) UserConfirmation(string)           {}

var _ Metrics = noopMetrics{}

// Clock is the deterministic-time seam (the pipeline.Clock precedent, reduced
// to a 1-method surface — 037 needs only Now() for event Timestamps, no
// duration; the aggregator/stages `now` smaller-surface precedent).
type Clock interface {
	Now() time.Time
}

// systemClock is the production default (UTC, the wall clock).
type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now().UTC() }

var _ Clock = systemClock{}

// Logger is the structured INFO/WARN/ERROR seam. Unlike the pipeline.Logger
// (Warn/Error only) this seam adds Info: security.md §11.2 step 4 mandates an
// audit trail for ALL UserConfirmedType receipts (build-spec D20/R5) — a
// mandatory security control, not optional telemetry, so it needs a
// first-class structured INFO sink. A concrete logger import here would break
// hermeticity.
type Logger interface {
	Info(ctx context.Context, msg string, kv ...any)
	Warn(ctx context.Context, msg string, kv ...any)
	Error(ctx context.Context, msg string, kv ...any)
}

// noopLogger is the zero-dependency default.
type noopLogger struct{}

func (noopLogger) Info(context.Context, string, ...any)  {}
func (noopLogger) Warn(context.Context, string, ...any)  {}
func (noopLogger) Error(context.Context, string, ...any) {}

var _ Logger = noopLogger{}

// TraceRestorer re-establishes the W3C trace context saved at pause time so
// the resumed pipeline's root span links to the original trace as a child
// (high-arch §6.10 Resume step 5: restore the OTel span as a child link to
// the original trace_context). Input: the saved model.TraceContext. Output:
// ctx enriched so pipeline.Tracer.StartPipeline (called inside
// ResumeAfterConfirmation) opens a span parented to the saved trace. A
// concrete tracer import here would break hermeticity (the pipeline Tracer-
// seam precedent). LIC-TASK-047 wires an adapter over
// tracer.Tracer.ExtractFromHeaders (TraceContext →
// map[string]string{"traceparent":...,"tracestate":...} then Extract); a
// zero/IsZero TraceContext degrades to "resume without trace linkage"
// (telemetry, non-functional — build-spec D13/R3).
type TraceRestorer interface {
	Restore(ctx context.Context, tc model.TraceContext) context.Context
}

// noopTraceRestorer returns ctx unchanged (the pipeline noopTracer
// "non-required telemetry default" discipline; resume still works, just
// without cross-pause trace linkage).
type noopTraceRestorer struct{}

func (noopTraceRestorer) Restore(ctx context.Context, _ model.TraceContext) context.Context {
	return ctx
}

var _ TraceRestorer = noopTraceRestorer{}
