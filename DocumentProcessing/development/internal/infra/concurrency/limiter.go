package concurrency

import (
	"context"

	"github.com/prometheus/client_golang/prometheus"

	"contractpro/document-processing/internal/domain/port"
	"contractpro/document-processing/internal/infra/observability"
)

// Compile-time interface compliance check.
var _ port.ConcurrencyLimiterPort = (*Semaphore)(nil)

// Semaphore limits the number of concurrently executing jobs per DP instance.
// It uses a buffered channel as the semaphore primitive: sending acquires a
// slot, receiving releases it. All operations are goroutine-safe.
type Semaphore struct {
	sem     chan struct{}
	logger  *observability.Logger
	active  prometheus.Gauge
	waiting prometheus.Gauge
}

// New creates a Semaphore with the given maximum number of concurrent slots.
// If maxJobs < 1, it defaults to 1.
// Panics if metrics or logger is nil.
func New(maxJobs int, metrics *observability.Metrics, logger *observability.Logger) *Semaphore {
	if metrics == nil {
		panic("concurrency.New: metrics must not be nil")
	}
	if logger == nil {
		panic("concurrency.New: logger must not be nil")
	}
	if maxJobs < 1 {
		maxJobs = 1
	}
	return &Semaphore{
		sem:     make(chan struct{}, maxJobs),
		logger:  logger,
		active:  metrics.ConcurrentJobsActive,
		waiting: metrics.ConcurrentJobsWaiting,
	}
}

// Acquire blocks until a slot is available or the context is cancelled.
// Returns nil on success, or ctx.Err() on cancellation/timeout.
func (s *Semaphore) Acquire(ctx context.Context) error {
	// Fast path: try to acquire without blocking.
	select {
	case s.sem <- struct{}{}:
		s.active.Inc()
		s.logger.Debug(ctx, "semaphore slot acquired")
		return nil
	default:
	}

	// Slow path: all slots occupied, wait for a free slot or ctx cancellation.
	s.waiting.Inc()
	defer s.waiting.Dec()

	s.logger.Debug(ctx, "semaphore full, waiting for slot")

	select {
	case s.sem <- struct{}{}:
		s.active.Inc()
		s.logger.Debug(ctx, "semaphore slot acquired after wait")
		return nil
	case <-ctx.Done():
		s.logger.Warn(ctx, "semaphore acquire cancelled", "error", ctx.Err())
		return ctx.Err()
	}
}

// Release frees one slot. Must be called exactly once for each successful
// Acquire. Calling Release without a matching Acquire logs a warning.
func (s *Semaphore) Release() {
	select {
	case <-s.sem:
		s.active.Dec()
	default:
		s.logger.Warn(context.Background(), "semaphore release called without matching acquire")
	}
}
