package dm

import (
	"context"
	"time"
)

// This file holds every Artifact Requester SEAM — a local interface (plus,
// where applicable, a zero-dependency noop default) for collaborators that
// are telemetry / runtime-environment, or that would force a forbidden import
// if depended on concretely (build-spec D3/D5/D11/D13). Everything crossing
// to a frozen cross-domain RabbitMQ wire is the broker Publisher seam below
// (NOT a concrete *broker.Client) so the package stays hermetic.
//
// var _ Seam = noop{} assertions follow each noop pair (the universal
// dmawaiter / pendingconfirmation / router / consumer precedent). The
// var _ port.ArtifactRequesterPort = (*ArtifactRequester)(nil) structural-
// satisfaction assertion lives in requester.go next to the type itself
// (compile-time interface contract).
//
// NOTE on Publisher: unlike the other three seams, Publisher has NO noop
// default. A silent-swallow Publisher would make lic.requests.artifacts
// publish failures invisible — the pipeline would block forever on the
// awaiter side without a single log line or metric. The constructor
// requires a non-nil Publisher and fails fast otherwise (build-spec D2).

// Publisher is the broker seam — a 1-method interface that matches
// broker.Client.Publish's signature exactly (publish.go:36) but keeps the
// concrete amqp091-backed type out of this package. LIC-TASK-036 / TASK-047
// wiring passes the real *broker.Client; tests pass an in-memory fake. The
// seam isolation lets this package stay hermetic per build-spec D13 (no
// amqp091 transitive import) while preserving publisher-confirm semantics —
// the broker client serializes publish + waits for the broker ack, and
// returns either nil, a *broker.BrokerError, or a raw context error.
type Publisher interface {
	// Publish sends payload to exchange with the given routingKey and
	// blocks until either the broker confirms the message (publisher
	// confirms) or the broker client's attempt budget is exhausted. Returns
	// nil on broker ack; broker.ErrPublishNack on a negative ack;
	// broker.ErrConfirmTimeout on a confirm-timeout; broker.ErrNotConnected
	// on a no-connection terminal; a *broker.BrokerError for any other
	// broker fault (wraps the underlying cause). ctx errors
	// (context.Canceled / context.DeadlineExceeded) pass through raw —
	// the codebase-wide convention (broker/errors.go:107, dmawaiter D13).
	Publish(ctx context.Context, exchange, routingKey string, payload []byte) error
}

// PublishOutcome is the lic_publisher_messages_total{outcome} label value
// (observability.md §3.9 — the authoritative enum SSOT lives in
// metrics/labels.go as metrics.PublishOutcome). It is a LOCAL MIRROR —
// declared here, NOT imported — so this package stays hermetic (no
// internal/infra/observability/metrics import before LIC-TASK-036 / TASK-047
// wiring), exactly like base.Outcome / router.CallOutcome / cost.Outcome /
// schemavalidator.RepairOutcome. The seams_test.go file pins the four wire
// strings against the metrics package SSOT so the mirror cannot silently
// drift (the codebase-wide local-mirror precedent).
//
// The four values are a CLOSED set (observability.md §3.9 / cardinality
// budget §3.10). Validation failures map to PublishOutcomeInvalid; broker
// nacks to PublishOutcomeNacked; ctx-cancel / confirm-timeout / not-connected
// / non-retryable AMQP to PublishOutcomeFailure; broker ack to
// PublishOutcomeSuccess.
type PublishOutcome string

const (
	// PublishOutcomeSuccess — broker acked the publish.
	PublishOutcomeSuccess PublishOutcome = "success"
	// PublishOutcomeFailure — generic publish failure (ctx cancel /
	// confirm timeout / not-connected / non-retryable AMQP / unknown).
	PublishOutcomeFailure PublishOutcome = "failure"
	// PublishOutcomeNacked — broker negatively acknowledged the publish.
	PublishOutcomeNacked PublishOutcome = "nacked"
	// PublishOutcomeInvalid — pre-publish validation failed (caller-side
	// input defect; broker.Publish was NOT called).
	PublishOutcomeInvalid PublishOutcome = "invalid"
)

// String returns the wire representation of the outcome.
func (o PublishOutcome) String() string { return string(o) }

// IsValid reports whether o is one of the four declared publish outcomes.
func (o PublishOutcome) IsValid() bool {
	switch o {
	case PublishOutcomeSuccess, PublishOutcomeFailure,
		PublishOutcomeNacked, PublishOutcomeInvalid:
		return true
	default:
		return false
	}
}

// Metrics is the observability.md §3.9 seam for the publisher-side counter
// lic_publisher_messages_total{topic, outcome}. Concrete prometheus is
// forbidden here (hermeticity — the dmawaiter.Metrics / pipeline.PipelineMetrics
// precedent). LIC-TASK-036 / TASK-047 wires a tiny adapter over
// *metrics.PublisherMetrics that bakes both labels and calls Inc().
type Metrics interface {
	// IncPublish records exactly one increment of
	// lic_publisher_messages_total{topic, outcome} for a single
	// RequestArtifacts call. Called UNCONDITIONALLY on every exit path
	// (success, invalid, nacked, failure — build-spec D7) so the counter
	// never silently drops a request.
	//
	// topic is the wire topic ("lic.requests.artifacts" for this
	// publisher); outcome is one of the four PublishOutcome* constants
	// (the local mirror of metrics.PublishOutcome — pinned in
	// seams_test.go to prevent drift).
	IncPublish(topic string, outcome PublishOutcome)
}

// noopMetrics is the zero-dependency default (the dmawaiter.noopMetrics
// precedent) so the hot path never nil-checks.
type noopMetrics struct{}

// IncPublish is a noop.
func (noopMetrics) IncPublish(string, PublishOutcome) {}

var _ Metrics = noopMetrics{}

// Clock is the deterministic-time seam (the dmawaiter.Clock /
// pendingconfirmation.Clock 1-method precedent). The requester uses Now()
// at one point: timestamp stamping for the GetArtifactsRequest envelope
// (build-spec D5 — RFC3339Nano in UTC).
type Clock interface {
	Now() time.Time
}

// systemClock is the production default (UTC, the wall clock).
type systemClock struct{}

// Now returns the current wall time in UTC.
func (systemClock) Now() time.Time { return time.Now().UTC() }

var _ Clock = systemClock{}

// Logger is the structured Warn/Error seam. Unlike pendingconfirmation.Logger
// (Info/Warn/Error) this seam OMITS Info: the publisher has no §11.2
// audit-trail mandate (no mandatory security control to log per call).
// In the current implementation the Logger is NOT actively called — the
// metric is the sole observability signal on the hot path (build-spec D15).
// The seam is reserved for future operator-visible WARN sites (e.g. broker
// nack telemetry once §3.9 widens) without a contract change.
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
