package kvstore

import (
	"context"
	"errors"
	"fmt"

	"github.com/redis/go-redis/v9"

	"contractpro/document-processing/internal/domain/port"
)

// ErrKeyNotFound is returned by Get when the requested key does not exist.
// Callers can check with errors.Is(err, kvstore.ErrKeyNotFound).
// This is NOT a DomainError — it is a normal "not found" signal, not an
// infrastructure failure.
var ErrKeyNotFound = errors.New("kvstore: key not found")

// mapError translates Redis and context errors into domain errors.
// Context errors (Canceled, DeadlineExceeded) pass through raw — this is the
// established pattern across the codebase so the orchestrator can distinguish
// cancellation from infrastructure failures.
func mapError(err error, operation string) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}

	// redis.Nil means "key not found" — handled by Get before mapError,
	// but this guard is defensive.
	if errors.Is(err, redis.Nil) {
		return ErrKeyNotFound
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
