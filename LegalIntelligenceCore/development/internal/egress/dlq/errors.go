package dlq

import (
	"context"
	"errors"
	"fmt"

	"contractpro/legal-intelligence-core/internal/infra/broker"
)

// PublishError is the typed error returned by PublishDLQ for the two
// classes of failure ATTRIBUTABLE TO THIS PUBLISHER:
//
//  1. Pre-publish validation failures — caller supplied an invalid
//     LICDLQEnvelope field set or an unknown DLQTopic. Reason is one of
//     the reason* constants in this file; Cause is nil.
//  2. JSON marshal failure — should be unreachable for compliant inputs
//     (LICDLQEnvelope is a typed struct with no exotic fields), but a
//     defensive wrap preserves the original encoding/json error in Cause
//     so production triage can identify the offending field.
//
// Broker failures pass through RAW — the caller's
// errors.Is(err, broker.ErrPublishNack) / errors.As(err, &broker.BrokerError{})
// chain stays intact. Context errors also pass through raw (codebase-wide
// convention; matches broker/errors.go:107).
type PublishError struct {
	// Reason is a machine-readable snake_case classifier. The constants
	// (reasonInvalidTopic etc.) are package-private — callers should not
	// switch on string literals; future code may expose a typed enum if
	// a use-case emerges.
	Reason string
	// Cause is the wrapped underlying error (e.g. the encoding/json
	// MarshalerError). nil for validation failures.
	Cause error
}

// Error formats the error for logs. Includes Cause when present.
func (e *PublishError) Error() string {
	if e == nil {
		return "<nil>"
	}
	if e.Cause != nil {
		return fmt.Sprintf("dlq publisher: %s: %v", e.Reason, e.Cause)
	}
	return fmt.Sprintf("dlq publisher: %s", e.Reason)
}

// Unwrap exposes Cause so errors.Is / errors.As traverse the chain when
// Cause is set (marshal-failure path).
func (e *PublishError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

// Validation-failure reason constants. snake_case so they map directly to
// log/metric-friendly identifiers. The required-field set is intentionally
// MINIMAL (architect Q3): topic + original_topic + original_message_hash +
// error_code + error_message. Correlation IDs are best-effort per
// integration-contracts.md §10.1 ("Correlation fields are best-effort —
// when an invalid-message envelope fails JSON parsing entirely the publisher
// leaves them empty").
const (
	reasonInvalidTopic         = "invalid_topic"
	reasonMissingOriginalTopic = "missing_original_topic"
	reasonMissingOriginalHash  = "missing_original_message_hash"
	reasonMissingErrorCode     = "missing_error_code"
	reasonMissingErrorMessage  = "missing_error_message"
	reasonNegativeRetryCount   = "negative_retry_count"
	reasonNegativeMessageSize  = "negative_message_size"
	reasonMarshalFailure       = "marshal_failure"
)

// Reason constants for lic_dlq_published_total{reason}. 1:1 derived from
// DLQTopic per architect Q2 — each topic maps to exactly ONE reason, so
// the actual emitted series count is 4 (one labelled cell per topic),
// well within the §3.10 estimated "DLQ: 4×3 = 12" budget and the
// 1500-series instance cap. The "1:1 redundancy with topic" is
// intentional — it lets Grafana split the counter on either dimension
// independently and keeps the LICDLQGrowth alert rule trivial. Kept as
// package-level constants so seams_test.go can pin them against the
// metrics.DLQMetrics label vocabulary if it ever enumerates them.
const (
	reasonInvalidMessage     = "invalid_message"
	reasonConsumerFailed     = "consumer_failed"
	reasonPublishFailed      = "publish_failed"
	reasonAgentOutputInvalid = "agent_output_invalid"
)

// classifyOutcome maps a broker.Publish return value to the local
// PublishOutcome label for lic_publisher_messages_total{topic, outcome}.
// Validation failures are NOT classified here (the call site emits
// PublishOutcomeInvalid directly before invoking broker.Publish — there
// is no broker err to inspect). Same shape and branches as the sibling
// dm / orch publisher classifiers.
//
//   - nil                                  → success
//   - errors.Is(err, context.Canceled)     → failure (caller-driven exit;
//     metric semantic is "the publish did not produce an ack")
//   - errors.Is(err, context.DeadlineExceeded) → failure (same)
//   - errors.Is(err, broker.ErrPublishNack) → nacked (broker rejected the
//     message — distinct outcome label per metrics/labels.go SSOT)
//   - errors.Is(err, broker.ErrConfirmTimeout) → failure (no ack within
//     PublisherConfirmTimeout — observability.md §3.9 buckets with
//     generic publish failures, not nacks)
//   - any other error                      → failure (broker.ErrNotConnected,
//     non-retryable AMQP 404/403/406 wrapped in *broker.BrokerError, any
//     network-level error wrapped retryable — all bucket as failure to
//     keep cardinality budget tight per metrics/labels.go §3.10)
func classifyOutcome(err error) PublishOutcome {
	if err == nil {
		return PublishOutcomeSuccess
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return PublishOutcomeFailure
	}
	if errors.Is(err, broker.ErrPublishNack) {
		return PublishOutcomeNacked
	}
	if errors.Is(err, broker.ErrConfirmTimeout) {
		return PublishOutcomeFailure
	}
	return PublishOutcomeFailure
}
