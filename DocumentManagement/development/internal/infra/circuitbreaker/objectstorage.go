package circuitbreaker

import (
	"context"
	"errors"
	"io"
	"time"

	"contractpro/document-management/internal/config"
	"contractpro/document-management/internal/domain/port"

	"github.com/sony/gobreaker"
)

// Compile-time interface check.
var _ port.ObjectStoragePort = (*ObjectStorageBreaker)(nil)

// StateReporter receives circuit breaker state changes.
// Implemented by observability.Metrics (SetCircuitBreakerState).
type StateReporter interface {
	SetCircuitBreakerState(component string, state float64)
}

// ObjectStorageBreaker wraps port.ObjectStoragePort with a gobreaker circuit
// breaker (BRE-014). When the circuit is open, calls fail fast without
// reaching the underlying Object Storage, preventing cascading timeouts.
type ObjectStorageBreaker struct {
	inner  port.ObjectStoragePort
	cb     *gobreaker.CircuitBreaker
	budget time.Duration
}

// NewObjectStorageBreaker creates a circuit breaker decorator around the given
// ObjectStoragePort. The StateReporter receives state change notifications
// for the dm_circuit_breaker_state metric.
func NewObjectStorageBreaker(
	inner port.ObjectStoragePort,
	cfg config.CircuitBreakerConfig,
	reporter StateReporter,
) *ObjectStorageBreaker {
	if inner == nil {
		panic("circuitbreaker: inner ObjectStoragePort must not be nil")
	}
	if reporter == nil {
		panic("circuitbreaker: StateReporter must not be nil")
	}

	b := &ObjectStorageBreaker{
		inner:  inner,
		budget: cfg.PerEventBudget,
	}

	b.cb = gobreaker.NewCircuitBreaker(gobreaker.Settings{
		Name:        "object_storage",
		MaxRequests: cfg.MaxRequests,
		Interval:    cfg.Interval,
		Timeout:     cfg.Timeout,
		ReadyToTrip: func(counts gobreaker.Counts) bool {
			return counts.ConsecutiveFailures >= cfg.FailureThreshold
		},
		OnStateChange: func(name string, from, to gobreaker.State) {
			reporter.SetCircuitBreakerState(name, stateToFloat(to))
		},
		IsSuccessful: func(err error) bool {
			if err == nil {
				return true
			}
			// Context errors are caller-initiated (cancellation, deadline),
			// not infrastructure failures — must NOT trip the circuit.
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return true
			}
			// Non-retryable errors (NotFound, AccessDenied) are application-level,
			// not infrastructure failures. They must NOT trip the circuit.
			return !port.IsRetryable(err)
		},
	})

	// Set initial metric value (closed = 0).
	reporter.SetCircuitBreakerState("object_storage", 0)

	return b
}

// PutObject uploads an object, guarded by the circuit breaker.
func (b *ObjectStorageBreaker) PutObject(ctx context.Context, key string, data io.Reader, contentType string) error {
	ctx, cancel := b.withBudget(ctx)
	defer cancel()
	_, err := b.cb.Execute(func() (interface{}, error) {
		return nil, b.inner.PutObject(ctx, key, data, contentType)
	})
	return b.mapCircuitError(err)
}

// GetObject retrieves an object, guarded by the circuit breaker.
// The returned io.ReadCloser wraps the inner body with budget context
// ownership: the cancel function fires when the caller closes the body,
// not when GetObject returns (preventing stream truncation).
func (b *ObjectStorageBreaker) GetObject(ctx context.Context, key string) (io.ReadCloser, error) {
	ctx, cancel := b.withBudget(ctx)
	var body io.ReadCloser
	_, err := b.cb.Execute(func() (interface{}, error) {
		var innerErr error
		body, innerErr = b.inner.GetObject(ctx, key)
		return nil, innerErr
	})
	if err != nil {
		cancel()
		return nil, b.mapCircuitError(err)
	}
	// Transfer cancel ownership to the body stream — cancel fires on Close().
	return &cancelOnCloseReader{ReadCloser: body, cancel: cancel}, nil
}

// DeleteObject removes an object, guarded by the circuit breaker.
func (b *ObjectStorageBreaker) DeleteObject(ctx context.Context, key string) error {
	ctx, cancel := b.withBudget(ctx)
	defer cancel()
	_, err := b.cb.Execute(func() (interface{}, error) {
		return nil, b.inner.DeleteObject(ctx, key)
	})
	return b.mapCircuitError(err)
}

// HeadObject checks object existence, guarded by the circuit breaker.
func (b *ObjectStorageBreaker) HeadObject(ctx context.Context, key string) (int64, bool, error) {
	ctx, cancel := b.withBudget(ctx)
	defer cancel()
	var size int64
	var exists bool
	_, err := b.cb.Execute(func() (interface{}, error) {
		var innerErr error
		size, exists, innerErr = b.inner.HeadObject(ctx, key)
		return nil, innerErr
	})
	if err != nil {
		return 0, false, b.mapCircuitError(err)
	}
	return size, exists, nil
}

// GeneratePresignedURL generates a time-limited URL, guarded by the circuit breaker.
func (b *ObjectStorageBreaker) GeneratePresignedURL(ctx context.Context, key string, expiry time.Duration) (string, error) {
	ctx, cancel := b.withBudget(ctx)
	defer cancel()
	var url string
	_, err := b.cb.Execute(func() (interface{}, error) {
		var innerErr error
		url, innerErr = b.inner.GeneratePresignedURL(ctx, key, expiry)
		return nil, innerErr
	})
	if err != nil {
		return "", b.mapCircuitError(err)
	}
	return url, nil
}

// DeleteByPrefix removes objects by prefix, guarded by the circuit breaker.
func (b *ObjectStorageBreaker) DeleteByPrefix(ctx context.Context, prefix string) error {
	ctx, cancel := b.withBudget(ctx)
	defer cancel()
	_, err := b.cb.Execute(func() (interface{}, error) {
		return nil, b.inner.DeleteByPrefix(ctx, prefix)
	})
	return b.mapCircuitError(err)
}

// State returns the current circuit breaker state.
func (b *ObjectStorageBreaker) State() gobreaker.State {
	return b.cb.State()
}

// withBudget enforces the per-event budget timeout (BRE-014: 30-40s).
// If the caller's context has no deadline or a deadline further out than the
// budget, a tighter timeout is applied. If the caller already has a tighter
// deadline, it is respected as-is.
//
// The caller must defer cancel() to release resources.
func (b *ObjectStorageBreaker) withBudget(ctx context.Context) (context.Context, context.CancelFunc) {
	if deadline, ok := ctx.Deadline(); ok {
		if time.Until(deadline) <= b.budget {
			return ctx, func() {}
		}
	}
	return context.WithTimeout(ctx, b.budget)
}

// mapCircuitError converts gobreaker sentinel errors to DomainError.
// Errors from the inner client pass through unchanged (already mapped).
func (b *ObjectStorageBreaker) mapCircuitError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, gobreaker.ErrOpenState) || errors.Is(err, gobreaker.ErrTooManyRequests) {
		return port.NewStorageError(ErrCircuitOpen.Error(), err)
	}
	return err
}

// cancelOnCloseReader wraps an io.ReadCloser and calls the cancel function
// when Close is invoked. This transfers budget context ownership from
// GetObject to the caller, preventing the response stream from being
// canceled before it is fully read.
type cancelOnCloseReader struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (r *cancelOnCloseReader) Close() error {
	err := r.ReadCloser.Close()
	r.cancel()
	return err
}

// stateToFloat converts a gobreaker.State to a float64 for Prometheus gauge.
// 0 = closed, 1 = half-open, 2 = open.
func stateToFloat(s gobreaker.State) float64 {
	switch s {
	case gobreaker.StateClosed:
		return 0
	case gobreaker.StateHalfOpen:
		return 1
	case gobreaker.StateOpen:
		return 2
	default:
		return -1
	}
}
