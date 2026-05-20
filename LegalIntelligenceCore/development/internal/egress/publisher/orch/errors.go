package orch

import (
	"context"
	"errors"
	"fmt"

	"contractpro/legal-intelligence-core/internal/infra/broker"
)

// PublishError is the typed error returned by both PublishStatus
// (StatusPublisher, LIC-TASK-044) and PublishClassificationUncertain
// (UncertaintyPublisher, LIC-TASK-045) for the two classes of failure
// ATTRIBUTABLE TO THE PUBLISHER:
//
//  1. Pre-publish validation failures — caller supplied an invalid
//     LICStatusChangedEvent or ClassificationUncertain field set. Reason
//     is one of the reason* constants in this file; Cause is nil.
//  2. JSON marshal failure — should be unreachable for compliant inputs
//     (no exotic types in either DTO), but a defensive wrap preserves
//     the original encoding/json error in Cause so production triage can
//     identify the offending field.
//
// Broker failures pass through RAW — the caller's
// errors.Is(err, broker.ErrPublishNack) / errors.As(err, &broker.BrokerError{})
// chain stays intact. Context errors also pass through raw — the codebase-
// wide convention; matches broker/errors.go:107.
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
		return fmt.Sprintf("orch publisher: %s: %v", e.Reason, e.Cause)
	}
	return fmt.Sprintf("orch publisher: %s", e.Reason)
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
// log/metric-friendly identifiers. The 13-strong StatusPublisher set
// below covers (the 045 add-on block further down adds 5
// UncertaintyPublisher-specific reasons for Block B/C/D/E):
//
//   - 5 envelope-ID required-field branches (Block A)
//   - 1 Status-IsValid branch (Block B)
//   - 1 Stage-IsValid (non-empty) branch (Block C)
//   - 3 FAILED-conditional branches: missing ErrorCode, non-publishable
//     ErrorCode, missing IsRetryable (Block D)
//   - 1 non-FAILED conditional branch: stale FAILED-fields leak (Block D)
//   - 1 defensive "ErrorCode not in catalog" branch (post-validation
//     LookupErrorSpec mismatch — theoretically unreachable after Block D
//     step 9, but kept for triage if the catalog SSOT ever drifts at
//     runtime)
//   - 1 marshal-failure branch (defensive, encoding/json post-validation)
//
// Block A (envelope IDs) и reasonMarshalFailure ниже — SHARED с
// UncertaintyPublisher (045) 1:1; Block B/C/D/defensive — StatusPublisher-only.
const (
	reasonMissingCorrelationID    = "missing_correlation_id"
	reasonMissingJobID            = "missing_job_id"
	reasonMissingDocumentID       = "missing_document_id"
	reasonMissingVersionID        = "missing_version_id"
	reasonMissingOrganizationID   = "missing_organization_id"
	reasonInvalidStatus           = "invalid_status"
	reasonInvalidStage            = "invalid_stage"
	reasonMissingErrorCode        = "missing_error_code"
	reasonNonPublishableErrorCode = "non_publishable_error_code"
	reasonMissingRetryable        = "missing_retryable"
	reasonUnexpectedFailureFields = "unexpected_failure_fields"
	reasonErrorCodeNotInCatalog   = "error_code_not_in_catalog"
	reasonMarshalFailure          = "marshal_failure"
)

// LIC-TASK-045 — ClassificationUncertain envelope validation reason
// constants (event-catalog.md §1.2). The 044 Block A reason set
// (reasonMissingCorrelationID..reasonMissingOrganizationID) AND
// reasonMarshalFailure are SHARED with this publisher 1:1 — they encode
// the same caller-side defects on the same wire fields. The five
// constants below are UncertaintyPublisher-specific: SuggestedType enum
// (FROZEN whitelist), the two [0,1] float ranges (Confidence /
// Threshold), and the alternatives-inner pair. NaN-handling is folded
// into each Confidence/Threshold reason (the offending value is visible
// in the log payload via the caller-passed *DomainError chain — adding
// a dedicated reasonNaN* would split one semantic into two without
// debugger value).
const (
	reasonInvalidSuggestedType         = "invalid_suggested_type"
	reasonInvalidConfidence            = "invalid_confidence"
	reasonInvalidThreshold             = "invalid_threshold"
	reasonInvalidAlternativeType       = "invalid_alternative_type"
	reasonInvalidAlternativeConfidence = "invalid_alternative_confidence"
)

// classifyOutcome maps a broker.Publish return value to the local
// PublishOutcome label for lic_publisher_messages_total{topic, outcome}.
// Validation failures are NOT classified here (the call site emits
// PublishOutcomeInvalid directly before invoking broker.Publish — there is
// no broker err to inspect).
//
// Branches:
//
//   - nil                                      → success
//   - errors.Is(err, context.Canceled)         → failure (caller-driven exit;
//     metric semantic is "the publish did not produce an ack")
//   - errors.Is(err, context.DeadlineExceeded) → failure (same)
//   - errors.Is(err, broker.ErrPublishNack)    → nacked (broker rejected the
//     message — distinct outcome label per metrics/labels.go SSOT)
//   - errors.Is(err, broker.ErrConfirmTimeout) → failure (no ack within
//     PublisherConfirmTimeout — observability.md §3.9 buckets with
//     generic publish failures, not nacks)
//   - any other error                          → failure (broker.ErrNotConnected,
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
