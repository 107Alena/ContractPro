package concurrency

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"contractpro/document-processing/internal/domain/port"
	"contractpro/document-processing/internal/infra/observability"

	dto "github.com/prometheus/client_model/go"
)

// --- helpers ---

func newTestSemaphore(t *testing.T, maxJobs int) (*Semaphore, *observability.Metrics) {
	t.Helper()
	metrics := observability.NewMetrics()
	logger := observability.NewLogger("error") // suppress debug/warn noise in tests
	s := New(maxJobs, metrics, logger)
	return s, metrics
}

func gaugeValue(t *testing.T, g interface{ Write(*dto.Metric) error }) float64 {
	t.Helper()
	var m dto.Metric
	if err := g.Write(&m); err != nil {
		t.Fatalf("failed to read gauge: %v", err)
	}
	return m.GetGauge().GetValue()
}

// --- Constructor tests ---

func TestNew_defaultsToOneIfZero(t *testing.T) {
	t.Parallel()
	s, _ := newTestSemaphore(t, 0)
	if cap(s.sem) != 1 {
		t.Errorf("expected cap 1, got %d", cap(s.sem))
	}
}

func TestNew_defaultsToOneIfNegative(t *testing.T) {
	t.Parallel()
	s, _ := newTestSemaphore(t, -5)
	if cap(s.sem) != 1 {
		t.Errorf("expected cap 1, got %d", cap(s.sem))
	}
}

func TestNew_respectsConfiguredMaxJobs(t *testing.T) {
	t.Parallel()
	s, _ := newTestSemaphore(t, 10)
	if cap(s.sem) != 10 {
		t.Errorf("expected cap 10, got %d", cap(s.sem))
	}
}

// --- Interface compliance ---

func TestSemaphore_implementsConcurrencyLimiterPort(t *testing.T) {
	t.Parallel()
	var _ port.ConcurrencyLimiterPort = (*Semaphore)(nil)
}

// --- Acquire/Release happy path ---

func TestAcquire_immediateWhenSlotAvailable(t *testing.T) {
	t.Parallel()
	s, _ := newTestSemaphore(t, 3)

	err := s.Acquire(context.Background())
	if err != nil {
		t.Fatalf("Acquire returned unexpected error: %v", err)
	}
	s.Release()
}

func TestAcquireRelease_cycle(t *testing.T) {
	t.Parallel()
	s, _ := newTestSemaphore(t, 1)

	if err := s.Acquire(context.Background()); err != nil {
		t.Fatal(err)
	}
	s.Release()

	// Should be able to acquire again after release.
	if err := s.Acquire(context.Background()); err != nil {
		t.Fatal(err)
	}
	s.Release()
}

func TestAcquire_exactlyMaxConcurrent(t *testing.T) {
	t.Parallel()
	s, _ := newTestSemaphore(t, 3)

	for i := 0; i < 3; i++ {
		if err := s.Acquire(context.Background()); err != nil {
			t.Fatalf("Acquire %d failed: %v", i, err)
		}
	}

	// All 3 slots occupied, should not be able to acquire without blocking.
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := s.Acquire(ctx)
	if err != context.DeadlineExceeded {
		t.Errorf("expected DeadlineExceeded, got %v", err)
	}

	for i := 0; i < 3; i++ {
		s.Release()
	}
}

// --- Blocking behavior ---

func TestAcquire_blocksWhenFullThenUnblocks(t *testing.T) {
	t.Parallel()
	s, _ := newTestSemaphore(t, 1)

	if err := s.Acquire(context.Background()); err != nil {
		t.Fatal(err)
	}

	acquired := make(chan struct{})
	go func() {
		if err := s.Acquire(context.Background()); err != nil {
			t.Errorf("blocked Acquire failed: %v", err)
		}
		close(acquired)
	}()

	// Give the goroutine time to enter the blocking path.
	time.Sleep(50 * time.Millisecond)

	select {
	case <-acquired:
		t.Fatal("Acquire should be blocked while semaphore is full")
	default:
		// Good — still blocked.
	}

	// Release one slot → blocked goroutine should proceed.
	s.Release()

	select {
	case <-acquired:
		// Good — unblocked.
	case <-time.After(2 * time.Second):
		t.Fatal("blocked Acquire did not unblock after Release")
	}

	s.Release()
}

// --- Context cancellation ---

func TestAcquire_returnsContextCanceled(t *testing.T) {
	t.Parallel()
	s, _ := newTestSemaphore(t, 1)

	if err := s.Acquire(context.Background()); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		done <- s.Acquire(ctx)
	}()

	// Let the goroutine enter the blocking path.
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err != context.Canceled {
			t.Errorf("expected context.Canceled, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Acquire did not return after context cancellation")
	}

	s.Release()
}

func TestAcquire_returnsDeadlineExceeded(t *testing.T) {
	t.Parallel()
	s, _ := newTestSemaphore(t, 1)

	if err := s.Acquire(context.Background()); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err := s.Acquire(ctx)
	if err != context.DeadlineExceeded {
		t.Errorf("expected DeadlineExceeded, got %v", err)
	}

	s.Release()
}

func TestAcquire_contextAlreadyCancelled(t *testing.T) {
	t.Parallel()
	s, _ := newTestSemaphore(t, 1)

	// Fill the semaphore.
	if err := s.Acquire(context.Background()); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Already cancelled.

	err := s.Acquire(ctx)
	if err != context.Canceled {
		t.Errorf("expected context.Canceled, got %v", err)
	}

	s.Release()
}

// --- Metrics integration ---

func TestAcquire_incrementsActiveGauge(t *testing.T) {
	t.Parallel()
	s, m := newTestSemaphore(t, 3)

	if err := s.Acquire(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := s.Acquire(context.Background()); err != nil {
		t.Fatal(err)
	}

	v := gaugeValue(t, m.ConcurrentJobsActive)
	if v != 2 {
		t.Errorf("expected active=2, got %v", v)
	}

	s.Release()
	s.Release()
}

func TestRelease_decrementsActiveGauge(t *testing.T) {
	t.Parallel()
	s, m := newTestSemaphore(t, 3)

	if err := s.Acquire(context.Background()); err != nil {
		t.Fatal(err)
	}
	s.Release()

	v := gaugeValue(t, m.ConcurrentJobsActive)
	if v != 0 {
		t.Errorf("expected active=0, got %v", v)
	}
}

func TestAcquire_incrementsWaitingGaugeWhenBlocking(t *testing.T) {
	t.Parallel()
	s, m := newTestSemaphore(t, 1)

	if err := s.Acquire(context.Background()); err != nil {
		t.Fatal(err)
	}

	acquired := make(chan struct{})
	go func() {
		_ = s.Acquire(context.Background())
		close(acquired)
	}()

	// Wait for goroutine to enter blocking path.
	time.Sleep(100 * time.Millisecond)

	w := gaugeValue(t, m.ConcurrentJobsWaiting)
	if w != 1 {
		t.Errorf("expected waiting=1, got %v", w)
	}

	// Release → blocked goroutine acquires → waiting should go to 0.
	s.Release()

	select {
	case <-acquired:
	case <-time.After(2 * time.Second):
		t.Fatal("blocked Acquire did not unblock")
	}

	w = gaugeValue(t, m.ConcurrentJobsWaiting)
	if w != 0 {
		t.Errorf("expected waiting=0 after acquire, got %v", w)
	}

	s.Release()
}

func TestAcquire_waitingGaugeDecrementsOnContextCancel(t *testing.T) {
	t.Parallel()
	s, m := newTestSemaphore(t, 1)

	if err := s.Acquire(context.Background()); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		_ = s.Acquire(ctx)
		close(done)
	}()

	// Wait for goroutine to enter blocking path.
	time.Sleep(100 * time.Millisecond)

	w := gaugeValue(t, m.ConcurrentJobsWaiting)
	if w != 1 {
		t.Errorf("expected waiting=1, got %v", w)
	}

	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Acquire did not return after cancellation")
	}

	w = gaugeValue(t, m.ConcurrentJobsWaiting)
	if w != 0 {
		t.Errorf("expected waiting=0 after cancellation, got %v", w)
	}

	s.Release()
}

// --- Concurrent stress test ---

func TestConcurrentAcquireRelease(t *testing.T) {
	t.Parallel()
	s, m := newTestSemaphore(t, 5)

	const goroutines = 50
	const iterations = 100

	var maxConcurrent atomic.Int64
	var current atomic.Int64

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				if err := s.Acquire(context.Background()); err != nil {
					t.Errorf("Acquire failed: %v", err)
					return
				}
				c := current.Add(1)
				if c > 5 {
					t.Errorf("concurrent count %d exceeds max 5", c)
				}
				// Track max for sanity.
				for {
					old := maxConcurrent.Load()
					if c <= old || maxConcurrent.CompareAndSwap(old, c) {
						break
					}
				}
				current.Add(-1)
				s.Release()
			}
		}()
	}

	wg.Wait()

	if maxConcurrent.Load() == 0 {
		t.Error("expected some concurrent acquisitions")
	}

	// Verify gauges return to zero after all goroutines finish.
	if v := gaugeValue(t, m.ConcurrentJobsActive); v != 0 {
		t.Errorf("expected active=0 after all goroutines, got %v", v)
	}
	if v := gaugeValue(t, m.ConcurrentJobsWaiting); v != 0 {
		t.Errorf("expected waiting=0 after all goroutines, got %v", v)
	}
}

// --- Edge case: Release without Acquire ---

func TestRelease_withoutAcquire_doesNotPanic(t *testing.T) {
	t.Parallel()
	s, _ := newTestSemaphore(t, 3)

	// Should not panic or deadlock — just log a warning.
	s.Release()
}

// --- Active gauge does not go negative on mismatched release ---

func TestRelease_withoutAcquire_activeGaugeUnchanged(t *testing.T) {
	t.Parallel()
	s, m := newTestSemaphore(t, 3)

	// Mismatched release should not decrement active gauge.
	s.Release()

	v := gaugeValue(t, m.ConcurrentJobsActive)
	if v != 0 {
		t.Errorf("expected active=0 after mismatched release, got %v", v)
	}
}

// --- Constructor nil-safety ---

func TestNew_nilMetrics_panics(t *testing.T) {
	t.Parallel()
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic on nil metrics")
		}
	}()
	logger := observability.NewLogger("error")
	New(5, nil, logger)
}

func TestNew_nilLogger_panics(t *testing.T) {
	t.Parallel()
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic on nil logger")
		}
	}()
	metrics := observability.NewMetrics()
	New(5, metrics, nil)
}

// --- Fast path with already-cancelled context ---

func TestAcquire_succeedsWithCancelledContextWhenSlotAvailable(t *testing.T) {
	t.Parallel()
	s, _ := newTestSemaphore(t, 3)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Fast path succeeds even with cancelled context when slot is available.
	err := s.Acquire(ctx)
	if err != nil {
		t.Errorf("expected nil (fast path), got %v", err)
	}
	s.Release()
}
