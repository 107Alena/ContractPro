package dlq

import (
	"context"
	"time"
)

// This file holds every DLQ Publisher SEAM — a local interface (plus, where
// applicable, a zero-dependency noop default) for collaborators that are
// telemetry / runtime-environment, or that would force a forbidden import
// if depended on concretely (LIC-TASK-046 hermeticity). Everything crossing
// to the frozen RabbitMQ DLX exchange is the broker Publisher seam below
// (NOT a concrete *broker.Client) so the package stays hermetic.
//
// var _ Seam = noop{} assertions follow each noop pair (the universal
// dmawaiter / pendingconfirmation / router / dm publisher / orch publisher
// precedent). The var _ port.DLQPublisherPort = (*DLQPublisher)(nil)
// structural-satisfaction assertion lives in publisher.go next to the type
// itself (compile-time interface contract).
//
// NOTE on Publisher: unlike the other three seams, Publisher has NO noop
// default. A silent-swallow Publisher would erase every DLQ envelope — the
// post-mortem signal §9 relies on would silently disappear, the LICDLQGrowth
// alert (§11) would never fire, and the operator would have no idea
// failures were even happening. The constructor requires a non-nil Publisher
// and fails fast otherwise.

// Publisher is the broker seam — a 1-method interface that matches
// broker.Client.Publish's signature exactly (publish.go:36) but keeps the
// concrete amqp091-backed type out of this package. LIC-TASK-047 wiring
// passes the real *broker.Client; tests pass an in-memory fake. The seam
// isolation lets this package stay hermetic (no amqp091 transitive import)
// while preserving publisher-confirm semantics — the broker client
// serializes publish + waits for the broker ack, and returns either nil, a
// *broker.BrokerError, or a raw context error.
type Publisher interface {
	// Publish sends payload to exchange with the given routingKey and
	// blocks until either the broker confirms the message (publisher
	// confirms) or the broker client's attempt budget is exhausted.
	// Returns nil on broker ack; broker.ErrPublishNack on a negative ack;
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
// internal/infra/observability/metrics import before LIC-TASK-047 wiring),
// exactly like base.Outcome / router.CallOutcome / cost.Outcome /
// schemavalidator.RepairOutcome / the sibling dm + orch publisher's local
// mirrors. The seams_test.go file pins the four wire strings against the
// metrics package SSOT so the mirror cannot silently drift.
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

// Metrics is the two-counter observability seam for the DLQ publisher:
//
//   - lic_publisher_messages_total{topic, outcome} (observability.md §3.9 —
//     same counter the sibling dm / orch publishers feed; called
//     UNCONDITIONALLY on every PublishDLQ exit path so the broker-level
//     outcome is always observable).
//   - lic_dlq_published_total{topic, reason} (observability.md §3.8 — DLQ-
//     specific counter; called ONLY on broker-ack success because the
//     §11 LICDLQGrowth alert reads this counter as "envelopes that
//     reached the DLQ", not "envelopes we tried to put on the DLQ").
//
// Concrete prometheus is forbidden here (hermeticity — the dmawaiter.Metrics
// / dm.Metrics / orch.Metrics precedent). LIC-TASK-047 wires a tiny adapter
// over *metrics.CrossCutMetrics + *metrics.DLQMetrics that bakes both label
// vocabularies and calls Inc().
type Metrics interface {
	// IncPublish records exactly one increment of
	// lic_publisher_messages_total{topic, outcome} for a single
	// PublishDLQ call. Called UNCONDITIONALLY on every exit path
	// (success, invalid, nacked, failure) so the counter never silently
	// drops a request.
	//
	// topic is the wire topic ("lic.dlq.invalid-message" /
	// "lic.dlq.consumer-failed" / "lic.dlq.publish-failed" /
	// "lic.dlq.agent-output-invalid"); outcome is one of the four
	// PublishOutcome* constants (the local mirror of
	// metrics.PublishOutcome — pinned in seams_test.go to prevent drift).
	IncPublish(topic string, outcome PublishOutcome)

	// IncDLQPublished records exactly one increment of
	// lic_dlq_published_total{topic, reason} on broker-ack SUCCESS only.
	// On any failure (validation / marshal / broker nack / ctx / etc.)
	// this counter is NOT touched — the §11 LICDLQGrowth alert reads it
	// as "envelopes that reached the DLQ".
	//
	// reason is one of the four topic-derived reason constants
	// (reasonInvalidMessage, reasonConsumerFailed, reasonPublishFailed,
	// reasonAgentOutputInvalid) — package-private but exported as
	// strings to the prometheus adapter. The 1:1 topic→reason mapping
	// emits exactly 4 series (one labelled cell per topic), inside the
	// §3.10 estimated "DLQ: 4×3 = 12" budget and well within the
	// 1500-series instance cap. The mapping is encoded in topicToReason()
	// in publisher.go.
	IncDLQPublished(topic string, reason string)
}

// noopMetrics is the zero-dependency default (the dmawaiter.noopMetrics
// precedent) so the hot path never nil-checks.
type noopMetrics struct{}

// IncPublish is a noop.
func (noopMetrics) IncPublish(string, PublishOutcome) {}

// IncDLQPublished is a noop.
func (noopMetrics) IncDLQPublished(string, string) {}

var _ Metrics = noopMetrics{}

// Clock is the deterministic-time seam (the dmawaiter.Clock /
// pendingconfirmation.Clock / dm publisher / orch publisher 1-method
// precedent). The publisher uses Now() at one point: stamping FailedAt
// when the caller left it empty (RFC3339Nano in UTC).
//
// Asymmetry vs the dm / orch publishers (build-spec Q6): those publishers
// ALWAYS overwrite the envelope Timestamp from clock.Now(). The DLQ
// publisher only stamps FailedAt when it is empty, because the semantic
// of failed_at is "when did the failure happen?" (caller knows, can
// pre-fill), not "when did this envelope leave the publisher?". A
// publish-failed envelope stamped at DLQ-publish time would misattribute
// the failure window for LICPipelineFailureRate (§11) if the caller
// buffered the failure across a broker reconnect.
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
// audit-trail mandate (no mandatory security control to log per call). In
// the current implementation the Logger is NOT actively called — the
// metrics pair (lic_publisher_messages_total + lic_dlq_published_total) is
// the sole observability signal on the hot path. The seam is reserved for
// future operator-visible WARN sites (e.g. broker nack telemetry once §3.9
// widens) without a contract change.
//
// For lic.dlq.publish-failed specifically, the task acceptance criteria
// states "full payload (LegalAnalysisArtifactsReady с PII) логируется как
// warning + alert" — this is satisfied by the CALLER logging the raw
// payload BEFORE invoking PublishDLQ (build-spec Q4). The DLQ publisher
// only handles the envelope and has no access to the raw payload; logging
// inside the publisher would be a no-op since there is nothing PII-rich to
// log. The §11 LICDLQGrowth alert rule reading
// lic_dlq_published_total{topic="lic.dlq.publish-failed"} provides the
// "alert" leg of the warning+alert intent.
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
