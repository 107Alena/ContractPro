package model

import (
	"errors"
	"fmt"
)

// DomainError is the typed error returned across all LIC domain boundaries.
// Unlike the DocumentProcessing equivalent, LIC errors carry separate RU
// (UserMessage) and EN (DevMessage) renderings because UserMessage is
// forwarded verbatim to the Orchestrator via lic.events.status-changed
// (NFR-5.2; see error-handling.md §2.1).
//
// Construction: ALWAYS go through NewDomainError, which pulls catalog
// defaults (UserMessage, DevMessage, Retryable). Customise individual fields
// via the chainable With* builders:
//
//	return NewDomainError(ErrCodeDMPersistFailed, StagePublishingArtifacts).
//	    WithCause(err).
//	    WithRetryable(false).                                // DM is_retryable=false override
//	    WithUserMessage("Документ был удалён или недоступен.") // non-retryable RU variant
//
// Ownership: a *DomainError is owned by a single goroutine at a time. The
// With* builders MUTATE the receiver and return it for chaining — they are
// safe to use immediately after NewDomainError. Once a *DomainError leaves
// the constructing scope (e.g. is returned up the call stack), callers must
// not mutate it concurrently with reads from another goroutine.
type DomainError struct {
	Code ErrorCode // machine-readable code (see error_codes.go)
	// UserMessage is the RU rendering forwarded verbatim to the
	// Orchestrator's lic.events.status-changed.error_message field.
	//
	// For DLQ-only codes (INVALID_MESSAGE_SCHEMA, INVALID_ORG_ID_MISMATCH,
	// IDEMPOTENCY_STORE_UNAVAILABLE) this field is intentionally empty —
	// such errors must not be published to the Orchestrator. Use
	// ErrorCode.IsPublishableToOrchestrator() before publishing.
	UserMessage string
	DevMessage  string         // EN, used in structured logs
	Retryable   bool           // is_retryable flag forwarded to Orchestrator
	Stage       Stage          // pipeline stage where the failure occurred
	Cause       error          // wrapped error (errors.Is / errors.As)
	Attributes  map[string]any // optional structured metadata (agent_id, provider_id, …)
}

// Error renders the error for log lines. The user-facing UserMessage is
// intentionally NOT included — it is for the API surface, not log output.
//
// Note: the spec snippet in error-handling.md §2.1 illustrates a trivial
// Error()→DevMessage implementation; the richer rendering here is
// deliberately preferred because the catalog DevMessage alone gives no
// context about which stage failed or whether a cause was wrapped.
func (e *DomainError) Error() string {
	if e == nil {
		return "<nil>"
	}
	stage := string(e.Stage)
	if stage == "" {
		stage = "STAGE_UNSPECIFIED"
	}
	if e.Cause != nil {
		return fmt.Sprintf("[%s] stage=%s: %s: %v", e.Code, stage, e.DevMessage, e.Cause)
	}
	return fmt.Sprintf("[%s] stage=%s: %s", e.Code, stage, e.DevMessage)
}

// Unwrap exposes the wrapped Cause for errors.Is / errors.As traversal.
func (e *DomainError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

// NewDomainError constructs a DomainError from the catalog. UserMessage,
// DevMessage and Retryable are populated from errorCatalog; Stage is taken
// from the argument; Cause / Attributes are left zero (use the With*
// builders to set them).
//
// Panics if code is not registered in the catalog — this is a startup-time
// guarantee enforced by init() in error_codes.go, so a runtime panic here
// indicates a programming bug (a constant that bypassed catalog
// registration).
func NewDomainError(code ErrorCode, stage Stage) *DomainError {
	spec, ok := errorCatalog[code]
	if !ok {
		panic(fmt.Sprintf("model.NewDomainError: unknown error code %q (must be registered in errorCatalog)", code))
	}
	return &DomainError{
		Code:        code,
		UserMessage: spec.userMessage,
		DevMessage:  spec.devMessage,
		Retryable:   spec.retryable,
		Stage:       stage,
	}
}

// WithCause sets the wrapped error and returns the receiver for chaining.
// Returns nil if e is nil.
func (e *DomainError) WithCause(err error) *DomainError {
	if e == nil {
		return nil
	}
	e.Cause = err
	return e
}

// WithDevMessage overrides the catalog DevMessage (EN, for logs). Use when
// the call site has more specific detail than the catalog default —
// e.g. include elapsed_ms or job_id directly in the log line.
func (e *DomainError) WithDevMessage(msg string) *DomainError {
	if e == nil {
		return nil
	}
	e.DevMessage = msg
	return e
}

// WithUserMessage overrides the catalog UserMessage (RU). Use only when
// error-handling.md §3 documents a per-condition variant — e.g.
// DM_PERSIST_FAILED has two RU renderings depending on is_retryable.
// Inlining arbitrary RU strings across the codebase is forbidden; the
// catalog must remain the source of truth.
func (e *DomainError) WithUserMessage(msg string) *DomainError {
	if e == nil {
		return nil
	}
	e.UserMessage = msg
	return e
}

// WithRetryable overrides the catalog Retryable default. Required for codes
// whose retryable flag depends on the cause:
//   - DM_PERSIST_FAILED — mirrors DM's is_retryable field
//   - AGENT_DEPENDENCY_FAILED — inherits from the wrapped DomainError
func (e *DomainError) WithRetryable(retryable bool) *DomainError {
	if e == nil {
		return nil
	}
	e.Retryable = retryable
	return e
}

// WithAttributes replaces the Attributes map. Passing a non-nil map
// transfers ownership to the receiver (callers must not mutate the input
// map after the call). Passing nil clears existing attributes.
func (e *DomainError) WithAttributes(attrs map[string]any) *DomainError {
	if e == nil {
		return nil
	}
	e.Attributes = attrs
	return e
}

// WithAttribute adds a single (key, value) pair to Attributes, lazily
// allocating the map if it is nil. Mutates the receiver in place.
func (e *DomainError) WithAttribute(key string, value any) *DomainError {
	if e == nil {
		return nil
	}
	if e.Attributes == nil {
		e.Attributes = make(map[string]any, 1)
	}
	e.Attributes[key] = value
	return e
}

// IsDomainError reports whether err is or wraps a *DomainError.
func IsDomainError(err error) bool {
	var de *DomainError
	return errors.As(err, &de)
}

// IsRetryable returns the Retryable flag of err if it is a DomainError,
// or false otherwise. The Orchestrator uses this to decide whether to
// surface a "повторить" button to the user.
func IsRetryable(err error) bool {
	var de *DomainError
	if errors.As(err, &de) {
		return de.Retryable
	}
	return false
}

// GetErrorCode extracts the ErrorCode from a DomainError chain, or returns
// an empty ErrorCode for non-DomainError errors. The Orchestrator uses this
// for status-changed.error_code population.
func GetErrorCode(err error) ErrorCode {
	var de *DomainError
	if errors.As(err, &de) {
		return de.Code
	}
	return ""
}

// AsDomainError unwraps err to its first *DomainError link, returning
// (de, true) on success or (nil, false) otherwise. This is the idiomatic
// type-assertion helper for callers that need to inspect Stage / Attributes.
func AsDomainError(err error) (*DomainError, bool) {
	var de *DomainError
	if errors.As(err, &de) {
		return de, true
	}
	return nil, false
}
