package kvstore

import (
	"context"
	"errors"
	"fmt"

	"github.com/redis/go-redis/v9"
)

// ErrKeyNotFound is returned by Get when the requested key does not exist.
// Callers can check with errors.Is(err, kvstore.ErrKeyNotFound).
// This is NOT an infrastructure error — it is a normal "not found" signal.
var ErrKeyNotFound = errors.New("kvstore: key not found")

// ErrClientClosed is returned by operations on a closed client.
// Callers can check with errors.Is(err, kvstore.ErrClientClosed).
var ErrClientClosed = errors.New("kvstore: client closed")

// mapError translates Redis and context errors into wrapped errors.
// Context errors (Canceled, DeadlineExceeded) pass through raw so callers
// can distinguish cancellation from infrastructure failures.
func mapError(err error, operation string) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}

	if errors.Is(err, redis.Nil) {
		return ErrKeyNotFound
	}

	return fmt.Errorf("kvstore: %s: %w", operation, err)
}

// errClientClosed returns an error for operations on a closed client.
func errClientClosed(operation string) error {
	return fmt.Errorf("kvstore: %s: %w", operation, ErrClientClosed)
}
