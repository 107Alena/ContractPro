package objectstorage

import (
	"context"
	"errors"
	"fmt"
	"net"

	"github.com/aws/smithy-go"
)

// Sentinel errors for caller-side classification via errors.Is.

// ErrCircuitOpen is returned when the circuit breaker is in OPEN state
// and the request is rejected without reaching Object Storage.
var ErrCircuitOpen = errors.New("objectstorage: circuit breaker is open")

// ErrObjectNotFound is returned by HeadObject when the object does not
// exist (HTTP 404 / NoSuchKey). Not an infrastructure error.
var ErrObjectNotFound = errors.New("objectstorage: object not found")

// StorageError carries operation context, retryability, and the original
// cause. Follows the same pattern as broker.BrokerError.
type StorageError struct {
	Operation string // "PutObject", "DeleteObject", "HeadObject"
	Message   string
	Retryable bool
	Cause     error
}

func (e *StorageError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("objectstorage: %s: %s: %s", e.Operation, e.Message, e.Cause)
	}
	return fmt.Sprintf("objectstorage: %s: %s", e.Operation, e.Message)
}

func (e *StorageError) Unwrap() error { return e.Cause }

// nonRetryableS3Codes lists S3 API error codes that represent permanent
// client errors. "NotFound" and "NoSuchKey" are handled separately
// (mapped to ErrObjectNotFound).
var nonRetryableS3Codes = map[string]bool{
	"AccessDenied":         true,
	"NoSuchBucket":         true,
	"InvalidBucketName":    true,
	"InvalidObjectName":    true,
	"InvalidAccessKeyId":   true,
	"SignatureDoesNotMatch": true,
	"AccountProblem":       true,
}

// mapError translates AWS SDK errors into StorageError or sentinel errors.
//
// Classification:
//  1. context.Canceled / context.DeadlineExceeded → pass through raw.
//  2. NoSuchKey / NotFound / 404 → ErrObjectNotFound (wrapped).
//  3. smithy.APIError with code in nonRetryableS3Codes → Retryable: false.
//  4. smithy.APIError with server code (InternalError, etc.) → Retryable: true.
//  5. net.Error → Retryable: true.
//  6. Unknown → Retryable: true (safe default).
func mapError(err error, operation string) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}

	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		code := apiErr.ErrorCode()
		if code == "NotFound" || code == "NoSuchKey" || code == "404" {
			return fmt.Errorf("objectstorage: %s: %w", operation, ErrObjectNotFound)
		}
		retryable := !nonRetryableS3Codes[code]
		return &StorageError{
			Operation: operation,
			Message:   code + ": " + apiErr.ErrorMessage(),
			Retryable: retryable,
			Cause:     err,
		}
	}

	var netErr net.Error
	if errors.As(err, &netErr) {
		return &StorageError{
			Operation: operation,
			Message:   "network error",
			Retryable: true,
			Cause:     err,
		}
	}

	return &StorageError{
		Operation: operation,
		Message:   err.Error(),
		Retryable: true,
		Cause:     err,
	}
}

// isRetryable determines whether a failed attempt should be retried.
func isRetryable(err error) bool {
	if errors.Is(err, ErrCircuitOpen) {
		return false
	}
	if errors.Is(err, ErrObjectNotFound) {
		return false
	}
	var se *StorageError
	if errors.As(err, &se) {
		return se.Retryable
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

// isCBFailure determines whether an error should count as a circuit
// breaker failure (increment consecutive failure count).
//
// 4xx, ErrObjectNotFound, and context.Canceled are NOT CB failures.
// 5xx, timeouts, connection errors ARE CB failures.
func isCBFailure(err error) bool {
	if errors.Is(err, context.Canceled) {
		return false
	}
	if errors.Is(err, ErrObjectNotFound) {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var se *StorageError
	if errors.As(err, &se) {
		return se.Retryable
	}
	return true
}
