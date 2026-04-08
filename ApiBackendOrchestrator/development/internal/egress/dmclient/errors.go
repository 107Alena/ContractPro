package dmclient

import (
	"context"
	"errors"
	"fmt"
	"net"
)

// Sentinel errors for caller-side classification via errors.Is.

// ErrCircuitOpen is returned when the circuit breaker is in OPEN state
// and the request is rejected without reaching the DM service.
var ErrCircuitOpen = errors.New("dmclient: circuit breaker is open")

// ErrDMUnavailable is returned after all retries are exhausted and the DM
// service remains unreachable or returns 5xx errors.
var ErrDMUnavailable = errors.New("dmclient: DM service unavailable")

// DMError carries operation context, HTTP status, response body, retryability,
// and the original cause. Callers can use StatusCode and Body to call
// model.MapDMError for translating DM errors into orchestrator ErrorCodes.
type DMError struct {
	// Operation identifies the client method (e.g. "CreateDocument", "GetVersion").
	Operation string

	// StatusCode is the HTTP status code returned by DM (0 if the request
	// never reached the server, e.g. timeout or connection refused).
	StatusCode int

	// Body is the raw response body from DM (may be nil for non-HTTP errors).
	Body []byte

	// Retryable indicates whether the error is worth retrying. True for 5xx,
	// timeouts, and connection errors. False for 4xx client errors.
	Retryable bool

	// Cause is the underlying error (HTTP transport error, context error, etc.).
	Cause error
}

func (e *DMError) Error() string {
	if e.StatusCode > 0 {
		return fmt.Sprintf("dmclient: %s: HTTP %d: %s", e.Operation, e.StatusCode, string(e.Body))
	}
	if e.Cause != nil {
		return fmt.Sprintf("dmclient: %s: %s", e.Operation, e.Cause)
	}
	return fmt.Sprintf("dmclient: %s: unknown error", e.Operation)
}

func (e *DMError) Unwrap() error { return e.Cause }

// mapHTTPError translates an HTTP status code and body into a DMError.
//
// Classification:
//   - 2xx: caller handles success — this function is not called.
//   - 4xx: Retryable=false. The response is a client-side problem (bad request,
//     not found, conflict, etc.). The caller should NOT retry.
//   - 5xx: Retryable=true. The DM service has a transient problem.
func mapHTTPError(operation string, statusCode int, body []byte) *DMError {
	retryable := statusCode >= 500
	return &DMError{
		Operation:  operation,
		StatusCode: statusCode,
		Body:       body,
		Retryable:  retryable,
	}
}

// mapTransportError translates a non-HTTP error (timeout, DNS, connection
// refused, etc.) into a DMError.
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
		return &DMError{
			Operation: operation,
			Retryable: true,
			Cause:     err,
		}
	}

	return &DMError{
		Operation: operation,
		Retryable: true,
		Cause:     err,
	}
}

// isRetryable determines whether a failed attempt should be retried.
func isRetryable(err error) bool {
	if errors.Is(err, ErrCircuitOpen) {
		return false
	}
	var de *DMError
	if errors.As(err, &de) {
		return de.Retryable
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

// isCBFailure determines whether an error should count as a circuit breaker
// failure (increment consecutive failure count).
//
// 4xx and context.Canceled are NOT CB failures.
// 5xx, timeouts, and connection errors ARE CB failures.
func isCBFailure(err error) bool {
	if errors.Is(err, context.Canceled) {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var de *DMError
	if errors.As(err, &de) {
		return de.Retryable
	}
	return true
}
