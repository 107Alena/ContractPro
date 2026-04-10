package uomclient

import (
	"context"
	"errors"
	"fmt"
	"net"
)

// UOMError carries operation context, HTTP status, response body, retryability,
// and the original cause. Callers can use StatusCode and Body to call
// MapUOMError for translating UOM errors into orchestrator ErrorCodes.
type UOMError struct {
	// Operation identifies the client method (e.g. "Login", "GetMe").
	Operation string

	// StatusCode is the HTTP status code returned by UOM (0 if the request
	// never reached the server, e.g. timeout or connection refused).
	StatusCode int

	// Body is the raw response body from UOM (may be nil for non-HTTP errors).
	Body []byte

	// Retryable indicates whether the error is worth retrying. True for 5xx,
	// timeouts, and connection errors. False for 4xx client errors.
	Retryable bool

	// Cause is the underlying error (HTTP transport error, context error, etc.).
	Cause error
}

func (e *UOMError) Error() string {
	if e.StatusCode > 0 {
		return fmt.Sprintf("uomclient: %s: HTTP %d: %s", e.Operation, e.StatusCode, string(e.Body))
	}
	if e.Cause != nil {
		return fmt.Sprintf("uomclient: %s: %s", e.Operation, e.Cause)
	}
	return fmt.Sprintf("uomclient: %s: unknown error", e.Operation)
}

func (e *UOMError) Unwrap() error { return e.Cause }

// mapHTTPError translates an HTTP status code and body into a UOMError.
//
// Classification:
//   - 4xx: Retryable=false. Client-side problem (invalid credentials, bad request).
//   - 5xx: Retryable=true. UOM service has a transient problem.
func mapHTTPError(operation string, statusCode int, body []byte) *UOMError {
	retryable := statusCode >= 500
	return &UOMError{
		Operation:  operation,
		StatusCode: statusCode,
		Body:       body,
		Retryable:  retryable,
	}
}

// mapTransportError translates a non-HTTP error (timeout, DNS, connection
// refused, etc.) into a UOMError.
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
		return &UOMError{
			Operation: operation,
			Retryable: true,
			Cause:     err,
		}
	}

	return &UOMError{
		Operation: operation,
		Retryable: true,
		Cause:     err,
	}
}

// isRetryable determines whether a failed attempt should be retried.
func isRetryable(err error) bool {
	var ue *UOMError
	if errors.As(err, &ue) {
		return ue.Retryable
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
