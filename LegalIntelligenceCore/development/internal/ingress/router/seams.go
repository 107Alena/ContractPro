package router

import (
	"context"
	"time"

	"contractpro/legal-intelligence-core/internal/domain/model"
	"contractpro/legal-intelligence-core/internal/domain/port"
)

// This file holds every Router SEAM — a router-local interface (plus, where
// applicable, a zero-dependency noop default) for collaborators that are
// telemetry / runtime-environment, or that would force a forbidden import if
// depended on concretely (build-spec D9/D10). Everything crossing to a frozen
// cross-domain RabbitMQ wire (the PendingStatePort / StatusPublisherPort) is
// a domain.port directly on the struct (a positional NewRouter param, NOT
// here — D2).
//
// var _ Seam = noop{} assertions follow each noop pair (the universal
// pendingconfirmation / consumer / idempotency precedent). The
// var _ router.PipelineRunner = (*pipeline.Orchestrator)(nil) /
// var _ router.PendingConfirmationManager = (*pendingconfirmation.Manager)(nil) /
// var _ router.IdempotencyGuard = (*idempotency.Guard)(nil) structural-
// satisfaction assertions live in the LIC-TASK-047 wiring package, NOT here —
// asserting them here would force forbidden imports and break hermeticity
// (build-spec D10).

// PipelineRunner is the seam to LIC-TASK-036's *pipeline.Orchestrator.Run. The
// orchestrator structurally satisfies it
// (Run(ctx, port.VersionProcessingArtifactsReady) error;
// orchestrator.go:253-437). Declared router-side so the router is hermetic
// and unit-testable with an in-package fake (build-spec D9). NO noop default:
// a Router with no pipeline runner cannot dispatch — NewRouter fails fast
// (D2). It is a REQUIRED positional NewRouter param, NOT in Deps.
//
// Return contract (build-spec D4 / R2): Run returns one of
//
//	nil                        ⇒ COMPLETED (orchestrator publishes inline);
//	pipeline.ErrPipelinePaused ⇒ paused (037 already SetPaused lic-trigger
//	                             + published the 2 pause events);
//	*model.DomainError         ⇒ terminal FAILED (orchestrator publishFailed
//	                             already published).
//
// Run itself acquires the JobLimiter on the raw inbound ctx
// (orchestrator.go:273 — Acquire BEFORE WithTimeout, the binding 036 rule):
// the Router MUST NOT pre-Acquire (build-spec R2 — adding a second Acquire
// would double-count the lic_pipeline_concurrent_jobs gauge).
type PipelineRunner interface {
	Run(ctx context.Context, trigger port.VersionProcessingArtifactsReady) error
}

// PendingConfirmationManager is the seam to LIC-TASK-037's
// *pendingconfirmation.Manager. The Manager structurally satisfies it
// (HandleUserConfirmedType + RepublishPauseEvents — manager.go:307-449 +
// :277-296). Declared router-side so the router does NOT import
// internal/application/pendingconfirmation (build-spec D10 hermetic
// allowlist). NO noop default: a Router with no pending-confirmation manager
// cannot route the user-confirmed-type topic — NewRouter fails fast.
//
// Return contracts:
//   - HandleUserConfirmedType: nil ⇒ ACK; *model.DomainError retryable=true
//     ⇒ NACK→retry-DLX; retryable=false ⇒ ACK (Manager already published
//     DLQ for poison cases or deliberately did not for corrupt-stored-blob
//     — build-spec D8/R4; Router NEVER republishes).
//   - RepublishPauseEvents: nil ⇒ ACK (§6.5:631 safety-net republish done);
//     *model.DomainError retryable=true ⇒ NACK→retry-DLX (Manager always
//     returns retryable INTERNAL_ERROR on republish failure —
//     manager.go:283-294).
type PendingConfirmationManager interface {
	HandleUserConfirmedType(ctx context.Context, cmd port.UserConfirmedType) error
	RepublishPauseEvents(ctx context.Context, ptc *model.PendingTypeConfirmation) error
}

// ArtifactsAwaiterDeliverer is the inbound-side companion to
// port.ArtifactsAwaiterPort (dm.go:80-96 — the domain port only carries
// Register/Await/Cancel for the orchestrator side; Deliver is the router-side
// ingress API LIC-TASK-041 dmawaiter exports as a separate public method on
// its concrete type). Declared router-side so 041 has freedom in its API
// shape; LIC-TASK-047 wires its concrete *dmawaiter.ArtifactsAwaiter as the
// seam impl (build-spec PART E #1). NO noop default.
//
// Deliver returns a non-nil error iff the awaiter slot is gone (timed out +
// Cancel'd) or the registry is in an invalid state — Router silently ACKs on
// miss (build-spec D6 — the response is dead-letter material; the pipeline
// goroutine will publish FAILED{DM_ARTIFACTS_TIMEOUT} itself,
// orchestrator.go:826).
type ArtifactsAwaiterDeliverer interface {
	Deliver(correlationID string, evt port.ArtifactsProvided) error
}

// PersistConfirmationDeliverer is the inbound-side companion to
// port.PersistConfirmationAwaiterPort (dm.go:106-120). It takes the fully-
// built port.PersistConfirmation envelope (NewPersistConfirmationSuccess /
// NewPersistConfirmationFailure — dm.go:138-152). NO noop default.
type PersistConfirmationDeliverer interface {
	Deliver(jobID string, conf port.PersistConfirmation) error
}

// VersionMetaCacheWriter writes lic-version-meta:{version_id} — the Redis-
// backed cache the orchestrator reads via the pipeline.VersionMetaCache seam
// at resolveParentAndMode (orchestrator.go:765-777). LIC-TASK-047 wires it
// over kvstore.Client. payload is opaque bytes (the Router marshals JSON; the
// cache adapter stores as-is). NO noop default.
type VersionMetaCacheWriter interface {
	Set(ctx context.Context, versionID string, payload []byte, ttl time.Duration) error
}

// IdempotencyGuard unifies the 5 frozen port.IdempotencyStorePort methods
// (idempotency.go:42-74) with the additive CheckAndAcquire + StartHeartbeat
// (BUILD_SPEC_LIC_038 D3.2 + D6). *idempotency.Guard structurally satisfies
// all 7 methods (guard.go:160-285 + heartbeat.go:48-84). Declared router-side
// because the frozen port.IdempotencyStorePort is missing the two additive
// methods (and must stay frozen — pendingconfirmation already depends on the
// 5-method surface). LIC-TASK-047 wires the SAME *Guard instance into both
// Router (as IdempotencyGuard) AND pendingconfirmation.Manager (as
// port.IdempotencyStorePort) — one Guard, two roles (build-spec PART E #4).
// NO noop default.
type IdempotencyGuard interface {
	SetNX(ctx context.Context, key string, ttl time.Duration) (port.IdempotencyStatus, error)
	Get(ctx context.Context, key string) (port.IdempotencyStatus, error)
	ExtendTTL(ctx context.Context, key string, ttl time.Duration) error
	SetCompleted(ctx context.Context, key string, ttl time.Duration) error
	SetPaused(ctx context.Context, key string, ttl time.Duration) error
	CheckAndAcquire(ctx context.Context, key string, ttl time.Duration) (port.IdempotencyStatus, bool, error)
	StartHeartbeat(ctx context.Context, key string, ttl time.Duration) (stop func())
}

// Metrics is the optional decision-counter seam (build-spec D11/D12/R5). The
// Router DOES NOT emit lic_consumer_messages_total{topic,outcome} (Consumer
// owns that — 039 CLAUDE.md D11) and DOES NOT emit
// lic_idempotency_lookups_total{result} (Guard owns it — 038 D8). v1
// intentionally introduces NO new Router-specific counter (R5 — every
// Router decision maps 1:1 onto a Guard lookup result; adding a Router
// counter would wastefully near-perfectly correlate); the noop default
// emits nothing. The seam shape is committed for a future R5 wire-up
// without an API break.
type Metrics interface {
	// Decision is RESERVED for forward use. The v1 noop + the LIC-TASK-047
	// production wiring both observe nothing; the call is currently a noop
	// on every implementation. topic / decision are opaque labels.
	Decision(topic, decision string)
}

// noopMetrics is the zero-dependency default.
type noopMetrics struct{}

// Decision is a noop.
func (noopMetrics) Decision(string, string) {}

var _ Metrics = noopMetrics{}

// Clock is the deterministic-time seam (the consumer.Clock /
// pendingconfirmation.Clock precedent, a 1-method surface). The Router uses
// Now() only inside publishFailedTerminal to stamp the Timestamp field of
// the §6.5:631 LICStatusChangedEvent (build-spec D11/PART E #2). The
// LIC-TASK-044 status publisher MAY override the timestamp at the wire
// boundary; the Router-stamped value is the upstream provenance.
type Clock interface {
	Now() time.Time
}

// systemClock is the production default (UTC, the wall clock).
type systemClock struct{}

// Now returns the current wall time in UTC.
func (systemClock) Now() time.Time { return time.Now().UTC() }

var _ Clock = systemClock{}

// Logger is the structured INFO/WARN/ERROR seam. It deliberately OMITS
// WithRequestContext: the Consumer (039 D6/R4) already attached the per-
// delivery correlation IDs to ctx ONCE before invoking Route*; the Router
// uses ctx verbatim when calling collaborators so every downstream log line
// inherits the IDs (build-spec D13). The omission is the compile-time
// guarantee that "Pin p" reviewer-gate degrades to (it cannot be violated
// here — there is no method to call).
//
// Severity usage convention (build-spec PART F #5):
//   - Error: SetCompleted failure on a terminal slot; FAILED status publish
//     errored on the §6.5:631 path.
//   - Warn:  Redis-down / awaiter registry-miss / cache-write failure —
//     all the "ACK silently and degrade" paths.
//   - Info:  NOT used by Router in v1 (Consumer owns per-delivery INFO; the
//     UCT audit trail is Manager-owned — manager.go:633).
type Logger interface {
	Info(ctx context.Context, msg string, kv ...any)
	Warn(ctx context.Context, msg string, kv ...any)
	Error(ctx context.Context, msg string, kv ...any)
}

// noopLogger is the zero-dependency default.
type noopLogger struct{}

// Info is a noop.
func (noopLogger) Info(context.Context, string, ...any) {}

// Warn is a noop.
func (noopLogger) Warn(context.Context, string, ...any) {}

// Error is a noop.
func (noopLogger) Error(context.Context, string, ...any) {}

var _ Logger = noopLogger{}

// Tracer is the per-route ingress-span seam (build-spec D11). The pipeline
// owns the root pipeline span (orchestrator.go:304 — StartPipeline). The
// Router opens a per-route span only when wired to a real OTEL tracer; v1
// defaults to noop (no tracing surface — the seam is committed for 047 to
// bridge to the OTEL tracer).
type Tracer interface {
	// StartRoute returns a child ctx and a RouteSpan handle. topic is the
	// frozen wire topic (e.g. dm.events.version-artifacts-ready). The
	// handle's Finish MUST be called exactly once per StartRoute.
	StartRoute(ctx context.Context, topic string) (context.Context, RouteSpan)
}

// RouteSpan is the handle returned by Tracer.StartRoute; Finish records the
// outcome (err==nil ⇒ ok; non-nil ⇒ failed) and ends the span.
type RouteSpan interface {
	Finish(err error)
}

// noopTracer is the zero-dependency default.
type noopTracer struct{}

// noopRouteSpan is the zero-dependency RouteSpan default.
type noopRouteSpan struct{}

// Finish is a noop.
func (noopRouteSpan) Finish(error) {}

// StartRoute returns ctx unchanged + a noop RouteSpan.
func (noopTracer) StartRoute(ctx context.Context, _ string) (context.Context, RouteSpan) {
	return ctx, noopRouteSpan{}
}

var _ Tracer = noopTracer{}
var _ RouteSpan = noopRouteSpan{}
