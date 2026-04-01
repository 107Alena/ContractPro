package kvstore

import (
	"context"
	"errors"
	"fmt"

	"github.com/redis/go-redis/v9"

	"contractpro/document-management/internal/domain/port"
)

// mapError translates Redis and context errors into domain errors.
// Context errors (Canceled, DeadlineExceeded) pass through raw — the
// orchestrator uses them to distinguish cancellation from infrastructure
// failures.
func mapError(err error, operation string) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}

	// redis.Nil should be handled by the caller before reaching mapError.
	// This is a defensive guard — not retryable since the key simply does
	// not exist.
	if errors.Is(err, redis.Nil) {
		return &port.DomainError{
			Code:      port.ErrCodeStorageFailed,
			Message:   fmt.Sprintf("kvstore: %s: unexpected nil", operation),
			Retryable: false,
			Cause:     err,
		}
	}

	return port.NewStorageError(
		fmt.Sprintf("kvstore: %s", operation), err,
	)
}

// errClientClosed returns a non-retryable error for operations on a closed client.
func errClientClosed(operation string) error {
	return &port.DomainError{
		Code:      port.ErrCodeStorageFailed,
		Message:   fmt.Sprintf("kvstore: %s: client closed", operation),
		Retryable: false,
	}
}
