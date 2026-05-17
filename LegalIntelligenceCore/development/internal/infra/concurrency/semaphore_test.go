package concurrency

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeGauge is a deterministic, goroutine-safe Gauge double for asserting the
// in-flight holder count without pulling Prometheus into this hermetic package.
type fakeGauge struct{ n atomic.Int64 }

func (g *fakeGauge) Inc()        { g.n.Add(1) }
func (g *fakeGauge) Dec()        { g.n.Add(-1) }
func (g *fakeGauge) Load() int64 { return g.n.Load() }

var _ Gauge = (*fakeGauge)(nil)

// --- Constructor ---

func TestNew_RespectsMax(t *testing.T) {
	t.Parallel()
	s := New(10)
	if cap(s.slots) != 10 {
		t.Errorf("cap(slots) = %d, want 10", cap(s.slots))
	}
}

func TestNew_DefaultGaugeIsNoop(t *testing.T) {
	t.Parallel()
	s := New(1) // no WithGauge → noopGauge; Acquire/Release must not nil-panic.
	if err := s.Acquire(context.Background()); err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	s.Release() // would deref a nil Gauge if the default were not set.
}

func TestNew_MaxBelowOne_Panics(t *testing.T) {
	t.Parallel()
	for _, m := range []int{0, -1, -5} {
		func() {
			defer func() {
				if recover() == nil {
					t.Fatalf("New(%d) must panic (config fail-fast-guarantees >=1; <1 = wiring defect)", m)
				}
			}()
			New(m)
		}()
	}
}

func TestWithGauge_NilIsIgnored(t *testing.T) {
	t.Parallel()
	s := New(1, WithGauge(nil)) // nil option must preserve the noop default.
	if err := s.Acquire(context.Background()); err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	s.Release()
}

// --- Acquire / Release happy path ---

func TestAcquireRelease_Cycle(t *testing.T) {
	t.Parallel()
	s := New(1)
	if err := s.Acquire(context.Background()); err != nil {
		t.Fatal(err)
	}
	s.Release()
	if err := s.Acquire(context.Background()); err != nil {
		t.Fatalf("re-acquire after release: %v", err)
	}
	s.Release()
}

// TestAcquire_NPlusOneBlocksUntilRelease covers acceptance Шаг 2: N+1
// concurrent Acquires — the last blocks until a Release frees a slot.
func TestAcquire_NPlusOneBlocksUntilRelease(t *testing.T) {
	t.Parallel()
	g := &fakeGauge{}
	s := New(3, WithGauge(g))

	for i := 0; i < 3; i++ {
		if err := s.Acquire(context.Background()); err != nil {
			t.Fatalf("Acquire %d: %v", i, err)
		}
	}
	if g.Load() != 3 {
		t.Fatalf("gauge after 3 acquires = %d, want 3", g.Load())
	}

	acquired := make(chan struct{})
	go func() {
		if err := s.Acquire(context.Background()); err != nil {
			t.Errorf("blocked Acquire: %v", err)
		}
		close(acquired)
	}()

	time.Sleep(50 * time.Millisecond) // let the 4th Acquire enter the wait.
	select {
	case <-acquired:
		t.Fatal("4th Acquire should block while all 3 slots are held")
	default:
	}

	s.Release() // frees one slot → the blocked Acquire proceeds.
	select {
	case <-acquired:
	case <-time.After(2 * time.Second):
		t.Fatal("Release did not unblock the waiting Acquire")
	}
	if g.Load() != 3 {
		t.Fatalf("gauge after release+acquire = %d, want 3", g.Load())
	}

	for i := 0; i < 3; i++ {
		s.Release()
	}
	if g.Load() != 0 {
		t.Fatalf("gauge after draining = %d, want 0", g.Load())
	}
}

// --- Context cancellation (acceptance Шаг 3) ---

func TestAcquire_CancelInterruptsBlocked(t *testing.T) {
	t.Parallel()
	s := New(1)
	if err := s.Acquire(context.Background()); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Acquire(ctx) }()

	time.Sleep(50 * time.Millisecond) // let the Acquire block.
	cancel()

	select {
	case err := <-done:
		if err != context.Canceled {
			t.Errorf("got %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cancel did not interrupt the blocked Acquire")
	}
	s.Release()
}

func TestAcquire_DeadlineExceededWhenFull(t *testing.T) {
	t.Parallel()
	s := New(1)
	if err := s.Acquire(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := s.Acquire(ctx); err != context.DeadlineExceeded {
		t.Errorf("got %v, want context.DeadlineExceeded", err)
	}
	s.Release()
}

// TestAcquire_PreCancelledCtx_ReturnsRawErrAndConsumesNoSlot is the
// LIC-specific load-bearing invariant (code-architect D7, deliberately
// stricter than DP): an already-cancelled ctx must return its raw error and
// must NOT consume a free slot, deterministically. Looped to defeat the Go
// select pseudo-random tie-break — without the ctx pre-check this fails
// intermittently.
func TestAcquire_PreCancelledCtx_ReturnsRawErrAndConsumesNoSlot(t *testing.T) {
	t.Parallel()
	for iter := 0; iter < 2000; iter++ {
		g := &fakeGauge{}
		s := New(2, WithGauge(g)) // slots free, but ctx pre-cancelled.
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		if err := s.Acquire(ctx); err != context.Canceled {
			t.Fatalf("iter %d: got %v, want context.Canceled (raw, unwrapped)", iter, err)
		}
		if g.Load() != 0 {
			t.Fatalf("iter %d: pre-cancelled Acquire must not Inc gauge, got %d", iter, g.Load())
		}
		// Proof no slot was silently consumed: both slots must still be free.
		for i := 0; i < 2; i++ {
			if err := s.Acquire(context.Background()); err != nil {
				t.Fatalf("iter %d: slot wrongly consumed by cancelled Acquire: %v", iter, err)
			}
		}
		if g.Load() != 2 {
			t.Fatalf("iter %d: gauge = %d, want 2", iter, g.Load())
		}
	}
}

// --- Release imbalance ---

func TestRelease_WithoutAcquire_Panics(t *testing.T) {
	t.Parallel()
	g := &fakeGauge{}
	s := New(1, WithGauge(g))
	defer func() {
		if recover() == nil {
			t.Fatal("Release without a matching Acquire must panic")
		}
		// The panic path must never reach gauge.Dec → no negative gauge.
		if g.Load() != 0 {
			t.Fatalf("gauge = %d after recovered over-release, want 0", g.Load())
		}
	}()
	s.Release()
}

// --- Gauge accounting ---

func TestGauge_IncOnAcquire_DecOnRelease(t *testing.T) {
	t.Parallel()
	g := &fakeGauge{}
	s := New(5, WithGauge(g))

	if err := s.Acquire(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := s.Acquire(context.Background()); err != nil {
		t.Fatal(err)
	}
	if g.Load() != 2 {
		t.Fatalf("gauge after 2 acquires = %d, want 2", g.Load())
	}
	s.Release()
	if g.Load() != 1 {
		t.Fatalf("gauge after 1 release = %d, want 1", g.Load())
	}
	s.Release()
	if g.Load() != 0 {
		t.Fatalf("gauge after 2 releases = %d, want 0", g.Load())
	}
}

// --- Concurrent stress (-race): bounded-holders invariant ---

func TestConcurrentAcquireRelease_RaceClean(t *testing.T) {
	t.Parallel()
	const (
		maxSlots   = 5
		goroutines = 50
		iterations = 200
	)
	g := &fakeGauge{}
	s := New(maxSlots, WithGauge(g))

	var current, peak atomic.Int64
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				if err := s.Acquire(context.Background()); err != nil {
					t.Errorf("Acquire: %v", err)
					return
				}
				c := current.Add(1)
				if c > maxSlots {
					t.Errorf("invariant violated: %d concurrent holders > %d", c, maxSlots)
				}
				for {
					p := peak.Load()
					if c <= p || peak.CompareAndSwap(p, c) {
						break
					}
				}
				current.Add(-1)
				s.Release()
			}
		}()
	}
	wg.Wait()

	if peak.Load() == 0 {
		t.Fatal("expected some concurrent acquisitions")
	}
	if g.Load() != 0 {
		t.Fatalf("gauge after all goroutines = %d, want 0", g.Load())
	}
	// All slots must be free again.
	for i := 0; i < maxSlots; i++ {
		if err := s.Acquire(context.Background()); err != nil {
			t.Fatalf("slot %d not free after stress: %v", i, err)
		}
	}
}
