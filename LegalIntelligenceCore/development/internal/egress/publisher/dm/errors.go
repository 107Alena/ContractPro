package dm

import (
	"context"
	"errors"
	"fmt"

	"contractpro/legal-intelligence-core/internal/infra/broker"
)

// PublishError is the typed error returned by RequestArtifacts for the two
// classes of failure ATTRIBUTABLE TO THIS PUBLISHER:
//
//  1. Pre-publish validation failures (build-spec D4) — caller supplied an
//     invalid GetArtifactsRequest field set. Reason is one of the
//     reason* constants in this file; Cause is nil.
//  2. JSON marshal failure (build-spec D6) — should be unreachable for
//     compliant inputs (no exotic types in port.GetArtifactsRequest), but
//     a defensive wrap preserves the original encoding/json error in
//     Cause so production triage can identify the offending field.
//
// Broker failures pass through RAW (build-spec D7) — the caller's
// errors.Is(err, broker.ErrPublishNack) / errors.As(err, &broker.BrokerError{})
// chain stays intact. Context errors also pass through raw (build-spec
// D8 — codebase-wide convention; matches broker/errors.go:107).
type PublishError struct {
	// Reason is a machine-readable snake_case classifier. The constants
	// (reasonMissingCorrelationID etc.) are package-private — callers
	// should not switch on string literals; future code may expose a
	// typed enum if a use-case emerges.
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
		return fmt.Sprintf("dm publisher: %s: %v", e.Reason, e.Cause)
	}
	return fmt.Sprintf("dm publisher: %s", e.Reason)
}

// Unwrap exposes Cause so errors.Is / errors.As traverse the chain when
// Cause is set (marshal-failure path).
func (e *PublishError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

// Validation-failure reason constants (build-spec D4). snake_case so they
// map directly to log/metric-friendly identifiers.
const (
	reasonMissingCorrelationID = "missing_correlation_id"
	reasonMissingJobID         = "missing_job_id"
	reasonMissingDocumentID    = "missing_document_id"
	reasonMissingVersionID     = "missing_version_id"
	reasonMissingArtifactTypes = "missing_artifact_types"
	reasonInvalidArtifactType  = "invalid_artifact_type"
	reasonMarshalFailure       = "marshal_failure"
)

// AnalysisArtifactsPublisher reasons (LIC-TASK-043, build-spec D4 — the
// pre-publish required-field validation predicate; D7 is the broker-outcome
// classifier below). The four ID reason constants (reasonMissingCorrelationID,
// reasonMissingJobID, reasonMissingDocumentID, reasonMissingVersionID) are
// REUSED from the ArtifactRequester block above — both publishers validate
// the same envelope IDs. The eight constants below are specific to the
// LegalAnalysisArtifactsReady payload's required artifact-pointer fields.
const (
	reasonMissingClassificationResult = "missing_classification_result"
	reasonMissingKeyParameters        = "missing_key_parameters"
	reasonMissingRiskAnalysis         = "missing_risk_analysis"
	reasonMissingRiskProfile          = "missing_risk_profile"
	reasonMissingRecommendations      = "missing_recommendations"
	reasonMissingSummary              = "missing_summary"
	reasonMissingDetailedReport       = "missing_detailed_report"
	reasonMissingAggregateScore       = "missing_aggregate_score"
)

// classifyOutcome maps a broker.Publish return value to the local
// PublishOutcome label for lic_publisher_messages_total{topic, outcome} —
// build-spec D7. Validation failures are NOT classified here (the call
// site emits PublishOutcomeInvalid directly before invoking broker.Publish
// — there is no broker err to inspect).
//
// Branches (build-spec D7):
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
