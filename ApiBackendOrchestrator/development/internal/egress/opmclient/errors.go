package opmclient

import (
	"context"
	"errors"
	"fmt"
	"net"
)

// ErrOPMDisabled is a sentinel error returned by DisabledClient when
// ORCH_OPM_BASE_URL is empty (OPM not configured).
var ErrOPMDisabled = errors.New("opmclient: OPM is disabled (ORCH_OPM_BASE_URL is empty)")

// OPMError carries operation context, HTTP status, response body, retryability,
// and the original cause. Callers can use StatusCode and Body to call
// MapOPMError for translating OPM errors into orchestrator ErrorCodes.
type OPMError struct {
	// Operation identifies the client method (e.g. "ListPolicies", "UpdatePolicy").
	Operation string

	// StatusCode is the HTTP status code returned by OPM (0 if the request
	// never reached the server, e.g. timeout or connection refused).
	StatusCode int

	// Body is the raw response body from OPM (may be nil for non-HTTP errors).
	Body []byte

	// Retryable indicates whether the error is worth retrying. True for 5xx,
	// timeouts, and connection errors. False for 4xx client errors.
	Retryable bool

	// Cause is the underlying error (HTTP transport error, context error, etc.).
	Cause error
}

func (e *OPMError) Error() string {
	if e.StatusCode > 0 {
		return fmt.Sprintf("opmclient: %s: HTTP %d: %s", e.Operation, e.StatusCode, string(e.Body))
	}
	if e.Cause != nil {
		return fmt.Sprintf("opmclient: %s: %s", e.Operation, e.Cause)
	}
	return fmt.Sprintf("opmclient: %s: unknown error", e.Operation)
}

func (e *OPMError) Unwrap() error { return e.Cause }

// mapHTTPError translates an HTTP status code and body into an OPMError.
//
// Classification:
//   - 4xx: Retryable=false. Client-side problem (bad request, not found).
//   - 5xx: Retryable=true. OPM service has a transient problem.
func mapHTTPError(operation string, statusCode int, body []byte) *OPMError {
	retryable := statusCode >= 500
	return &OPMError{
		Operation:  operation,
		StatusCode: statusCode,
		Body:       body,
		Retryable:  retryable,
	}
}

// mapTransportError translates a non-HTTP error (timeout, DNS, connection
// refused, etc.) into an OPMError.
//
// Classification:
//  1. context.Canceled / context.DeadlineExceeded → pass through raw.
//  2. net.Error → Retryable=true.
//  3. Unknown → Retryable=true (safe default).
func mapTransportError(err error, operation string) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}

	var netErr net.Error
	if errors.As(err, &netErr) {
		return &OPMError{
			Operation: operation,
			Retryable: true,
			Cause:     err,
		}
	}

	return &OPMError{
		Operation: operation,
		Retryable: true,
		Cause:     err,
	}
}

// isRetryable determines whether a failed attempt should be retried.
func isRetryable(err error) bool {
	var oe *OPMError
	if errors.As(err, &oe) {
		return oe.Retryable
	}
	if errors.Is(err, context.Canceled) {
		return false
	}
	// Per-attempt DeadlineExceeded is retryable (parent ctx checked separately).
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	return false
}
