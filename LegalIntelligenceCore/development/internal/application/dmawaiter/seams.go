package dmawaiter

import (
	"context"
	"time"
)

// This file holds every DM Awaiter SEAM — a local interface (plus, where
// applicable, a zero-dependency noop default) for collaborators that are
// telemetry / runtime-environment, or that would force a forbidden import if
// depended on concretely (build-spec D11/D14/D15/D17). Everything crossing to
// a frozen cross-domain RabbitMQ/Redis wire is a domain.port instead — and the
// DM awaiters have NO such crossing on their hot path (the awaiter is a pure
// in-process correlation registry; the broker side is the consumer + router).
//
// var _ Seam = noop{} assertions follow each noop pair (the universal
// pendingconfirmation / router / consumer / idempotency precedent). The
// var _ port.ArtifactsAwaiterPort = (*ArtifactAwaiter)(nil) /
// var _ router.ArtifactsAwaiterDeliverer = (*ArtifactAwaiter)(nil) /
// var _ port.ArtifactsProvidedHandler = (*ArtifactAwaiter)(nil) structural-
// satisfaction assertions (and the symmetric ConfirmationAwaiter trio) live
// in the LIC-TASK-047 wiring package, NOT here — asserting them here would
// either be a tautology (the port is imported anyway) or force the forbidden
// internal/ingress/router import and break hermeticity (build-spec D17/D19).

// Metrics is the observability.md §3.5 seam for the two DM-request series
// (lic_dm_request_duration_seconds{op} and lic_dm_request_outcome_total
// {op,outcome}). Concrete prometheus is forbidden here (hermeticity — the
// aggregator.Metrics / pipeline.PipelineMetrics / pendingconfirmation.Metrics
// precedent). LIC-TASK-047 wires a per-awaiter adapter over *metrics.DMMetrics
// that bakes the op label and bridges both prometheus calls into one
// RecordOutcome invocation.
type Metrics interface {
	// RecordOutcome records both lic_dm_request_duration_seconds{op}
	// (Observe(seconds)) and lic_dm_request_outcome_total{op,outcome}
	// (Inc) for a single Await completion. The two metric writes are
	// collapsed into one seam method so the adapter (LIC-TASK-047) writes
	// both atomically per Await exit (build-spec D11).
	//
	// op is one of {opGetArtifacts, opPersistArtifacts} — the unexported
	// constants in awaiter.go.
	// outcome is one of {outcomeSuccess, outcomeTimeout,
	// outcomePersistFailed, outcomeMissing} — observability.md §3.5.
	RecordOutcome(op, outcome string, seconds float64)
}

// noopMetrics is the zero-dependency default (the pipeline.noopPipelineMetrics
// / pendingconfirmation.noopMetrics precedent) so the hot path never
// nil-checks.
type noopMetrics struct{}

// RecordOutcome is a noop.
func (noopMetrics) RecordOutcome(string, string, float64) {}

var _ Metrics = noopMetrics{}

// Clock is the deterministic-time seam (the pendingconfirmation.Clock /
// router.Clock 1-method precedent). The awaiter uses Now() at two points:
// slot creation (Register, for the duration metric's start) and Await exit
// (any branch — success, timeout, ctx-cancel — for the duration metric's
// end). Build-spec D14.
type Clock interface {
	Now() time.Time
}

// systemClock is the production default (UTC, the wall clock).
type systemClock struct{}

// Now returns the current wall time in UTC.
func (systemClock) Now() time.Time { return time.Now().UTC() }

var _ Clock = systemClock{}

// Logger is the structured Warn/Error seam. Unlike pendingconfirmation.Logger
// (Info/Warn/Error) this seam OMITS Info: the awaiter has no §11.2
// audit-trail mandate (no mandatory security control to log per call); the
// only Warn/Error sites are the registry-miss / duplicate-deliver / defensive
// identity-mismatch paths in deliver/Await — build-spec D15. A concrete
// logger import here would break hermeticity.
//
// Severity usage convention (build-spec D15):
//   - Warn:  late delivery (registry miss in deliver — slot timed-out /
//     Cancel'd before the response arrived); duplicate Deliver
//     dropped on the floor (channel full — first-wins).
//   - Error: defensive "registry inconsistency" — reserved for the
//     truly impossible case reg[key] != nil && reg[key] != s in
//     Await cleanup (a double-Register that bypassed the
//     ErrDuplicateRegistration gate). Unreachable in compliant code.
type Logger interface {
	Warn(ctx context.Context, msg string, kv ...any)
	Error(ctx context.Context, msg string, kv ...any)
}

// noopLogger is the zero-dependency default.
type noopLogger struct{}

// Warn is a noop.
func (noopLogger) Warn(context.Context, string, ...any) {}

// Error is a noop.
func (noopLogger) Error(context.Context, string, ...any) {}

var _ Logger = noopLogger{}
