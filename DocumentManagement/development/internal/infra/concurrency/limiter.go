package concurrency

import (
	"context"
	"sync/atomic"
)

// Logger is the consumer-side interface for structured logging.
type Logger interface {
	Debug(msg string, args ...any)
	Warn(msg string, args ...any)
}

// Semaphore limits the number of concurrently executing handlers.
// It uses a buffered channel as the semaphore primitive: sending acquires a
// slot, receiving releases it. All operations are goroutine-safe.
type Semaphore struct {
	sem    chan struct{}
	logger Logger
	active atomic.Int64
}

// NewSemaphore creates a Semaphore with the given maximum number of concurrent slots.
// If maxConcurrent < 1, it defaults to 1.
// Panics if logger is nil.
func NewSemaphore(maxConcurrent int, logger Logger) *Semaphore {
	if logger == nil {
		panic("concurrency: logger must not be nil")
	}
	if maxConcurrent < 1 {
		maxConcurrent = 1
	}
	return &Semaphore{
		sem:    make(chan struct{}, maxConcurrent),
		logger: logger,
	}
}

// Acquire blocks until a slot is available or the context is cancelled.
// Returns nil on success, or ctx.Err() on cancellation/timeout.
func (s *Semaphore) Acquire(ctx context.Context) error {
	// Fast path: try to acquire without blocking.
	select {
	case s.sem <- struct{}{}:
		s.active.Add(1)
		return nil
	default:
	}

	// Slow path: all slots occupied, wait.
	s.logger.Debug("semaphore full, waiting for slot",
		"active", s.active.Load(), "capacity", cap(s.sem))

	select {
	case s.sem <- struct{}{}:
		s.active.Add(1)
		return nil
	case <-ctx.Done():
		s.logger.Warn("semaphore acquire cancelled", "error", ctx.Err())
		return ctx.Err()
	}
}

// Release frees one slot. Must be called exactly once for each successful
// Acquire. Calling Release without a matching Acquire logs a warning.
func (s *Semaphore) Release() {
	select {
	case <-s.sem:
		s.active.Add(-1)
	default:
		s.logger.Warn("semaphore release called without matching acquire")
	}
}

// ActiveCount returns the number of currently held slots.
func (s *Semaphore) ActiveCount() int64 {
	return s.active.Load()
}

// Capacity returns the maximum number of concurrent slots.
func (s *Semaphore) Capacity() int {
	return cap(s.sem)
}
