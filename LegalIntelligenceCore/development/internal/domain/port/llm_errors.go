package port

import (
	"errors"
	"fmt"
	"time"
)

// LLMErrorCode classifies provider-level failure modes
// (llm-provider-abstraction.md §1.2). It is intentionally distinct from
// model.ErrorCode (the LIC-wide domain-error taxonomy) — these codes describe
// HOW the call failed at the wire level (network, 401, 429, content policy),
// not the resulting pipeline state, and the Router translates between them.
type LLMErrorCode string

const (
	LLMErrorTimeout          LLMErrorCode = "TIMEOUT"
	LLMErrorRateLimit        LLMErrorCode = "RATE_LIMIT"
	LLMErrorServerError      LLMErrorCode = "SERVER_ERROR"
	LLMErrorNetwork          LLMErrorCode = "NETWORK"
	LLMErrorOverloaded       LLMErrorCode = "OVERLOADED" // Anthropic 529
	LLMErrorInvalidAPIKey    LLMErrorCode = "INVALID_API_KEY"
	LLMErrorQuotaExceeded    LLMErrorCode = "QUOTA_EXCEEDED"
	LLMErrorContentPolicy    LLMErrorCode = "CONTENT_POLICY"
	LLMErrorContextTooLong   LLMErrorCode = "CONTEXT_TOO_LONG"
	LLMErrorMalformedRequest LLMErrorCode = "MALFORMED_REQUEST"

	// LLMErrorAllProvidersFailed is set by the Router itself when the
	// fallback chain is exhausted (llm-provider-abstraction.md §2.1).
	// Adapters never emit this code.
	LLMErrorAllProvidersFailed LLMErrorCode = "ALL_PROVIDERS_FAILED"
)

// String returns the wire representation of the code.
func (c LLMErrorCode) String() string { return string(c) }

// LLMProviderError is the typed error every adapter MUST return (wrapped or
// constructed) on failure (llm-provider-abstraction.md §1.2). The two
// orthogonal flags drive Router behaviour:
//
//   - Retryable        — true ⇒ try the same provider again with backoff
//   - FallbackEligible — true ⇒ on giving up, advance to the next provider
//
// The matrix in §1.2 fixes the (Retryable, FallbackEligible) pair per code;
// see llm-provider-abstraction.md table for the full enumeration. The
// canonical mapping is captured in NewLLMProviderError so adapters cannot
// drift from the spec accidentally.
//
// LLMProviderError is an error (implements error via Error()), unwraps the
// underlying transport / SDK error via Wrapped, and is identifiable through
// errors.As(*LLMProviderError). The Router uses errors.As; never type-assert.
type LLMProviderError struct {
	Code             LLMErrorCode
	Retryable        bool
	FallbackEligible bool

	// RetryAfter is set only for 429 responses that carry a Retry-After
	// header; when nil, the caller uses its default backoff schedule.
	RetryAfter *time.Duration

	// Wrapped exposes the underlying transport / SDK error for errors.Is /
	// errors.Unwrap chains. Adapters MUST set this when an underlying error
	// exists so root-cause logging stays useful.
	Wrapped error
}

// Error implements the error interface. The format is intentionally
// machine-friendly first (code) and human-friendly second (wrapped detail) —
// see logger sanitiser, which strips API keys before this hits structured
// logs.
func (e *LLMProviderError) Error() string {
	if e == nil {
		return "<nil>"
	}
	if e.Wrapped != nil {
		return fmt.Sprintf("llm provider error: code=%s retryable=%t fallback=%t: %v",
			e.Code, e.Retryable, e.FallbackEligible, e.Wrapped)
	}
	return fmt.Sprintf("llm provider error: code=%s retryable=%t fallback=%t",
		e.Code, e.Retryable, e.FallbackEligible)
}

// Unwrap returns the underlying transport / SDK error, supporting errors.Is
// and errors.Unwrap chains.
func (e *LLMProviderError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Wrapped
}

// IsAuthError reports whether this error indicates an authentication failure
// (401 / invalid api key). Used by the Router to mark the provider permanently
// unhealthy (llm-provider-abstraction.md §1.3) — those entries are not
// rechecked until SIGHUP / restart.
//
// Nil receiver returns false; callers may rely on the helper without a
// pre-check.
func (e *LLMProviderError) IsAuthError() bool {
	if e == nil {
		return false
	}
	return e.Code == LLMErrorInvalidAPIKey
}

// IsRetryable reports whether the Router should retry on the same provider
// before considering fallback. Nil receiver returns false.
func (e *LLMProviderError) IsRetryable() bool {
	if e == nil {
		return false
	}
	return e.Retryable
}

// IsFallbackEligible reports whether the Router should advance to the next
// provider in the fallback chain after giving up on the current one. Nil
// receiver returns false.
func (e *LLMProviderError) IsFallbackEligible() bool {
	if e == nil {
		return false
	}
	return e.FallbackEligible
}

// llmCodePolicy enumerates the Retryable / FallbackEligible defaults per
// LLMErrorCode (llm-provider-abstraction.md §1.2). NewLLMProviderError reads
// the row for the given code; adapters that need to override (rare) must use
// the literal &LLMProviderError{...} form and document the deviation.
type llmCodePolicy struct {
	retryable        bool
	fallbackEligible bool
}

var llmCodeCatalog = map[LLMErrorCode]llmCodePolicy{
	LLMErrorTimeout:            {retryable: true, fallbackEligible: true},
	LLMErrorRateLimit:          {retryable: true, fallbackEligible: true},
	LLMErrorServerError:        {retryable: true, fallbackEligible: true},
	LLMErrorNetwork:            {retryable: true, fallbackEligible: true},
	LLMErrorOverloaded:         {retryable: true, fallbackEligible: true},
	LLMErrorInvalidAPIKey:      {retryable: false, fallbackEligible: true},
	LLMErrorQuotaExceeded:      {retryable: false, fallbackEligible: true},
	LLMErrorContentPolicy:      {retryable: false, fallbackEligible: true},
	LLMErrorContextTooLong:     {retryable: false, fallbackEligible: false},
	LLMErrorMalformedRequest:   {retryable: false, fallbackEligible: false},
	LLMErrorAllProvidersFailed: {retryable: true, fallbackEligible: false},
}

// NewLLMProviderError builds an *LLMProviderError with the canonical
// (Retryable, FallbackEligible) flags for the given code
// (llm-provider-abstraction.md §1.2 table). Returns nil for unknown codes —
// the caller is expected to use a literal struct in that case and tests
// catch the omission via TestLLMCodeCatalog_Exhaustive.
func NewLLMProviderError(code LLMErrorCode, wrapped error) *LLMProviderError {
	policy, ok := llmCodeCatalog[code]
	if !ok {
		return nil
	}
	return &LLMProviderError{
		Code:             code,
		Retryable:        policy.retryable,
		FallbackEligible: policy.fallbackEligible,
		Wrapped:          wrapped,
	}
}

// AllLLMErrorCodes returns a fresh slice with every declared LLMErrorCode in
// table order (llm-provider-abstraction.md §1.2). Callers may mutate the
// returned slice. Used by tests to assert catalog completeness.
func AllLLMErrorCodes() []LLMErrorCode {
	return []LLMErrorCode{
		LLMErrorTimeout,
		LLMErrorRateLimit,
		LLMErrorServerError,
		LLMErrorNetwork,
		LLMErrorOverloaded,
		LLMErrorInvalidAPIKey,
		LLMErrorQuotaExceeded,
		LLMErrorContentPolicy,
		LLMErrorContextTooLong,
		LLMErrorMalformedRequest,
		LLMErrorAllProvidersFailed,
	}
}

// AsLLMProviderError extracts an *LLMProviderError from err via errors.As,
// returning (nil, false) when err carries no such value. Convenience wrapper
// that keeps Router / Repair-loop call sites concise.
func AsLLMProviderError(err error) (*LLMProviderError, bool) {
	if err == nil {
		return nil, false
	}
	var e *LLMProviderError
	if errors.As(err, &e) {
		return e, true
	}
	return nil, false
}

// Compile-time assertion that LLMProviderError satisfies error.
var _ error = (*LLMProviderError)(nil)
