package stages

import (
	"context"
	"errors"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"contractpro/legal-intelligence-core/internal/domain/model"
)

// These tests pin the frozen behavioural contract of parallel() — the
// stdlib-only errgroup.WithContext equivalent (code-architect B-3). If any of
// them cannot be made deterministic the helper is wrong, not the test.

// TestParallel_FirstErrorWins — the FIRST fn to fail wins; later errors are
// dropped (errgroup-identical). Determinism: fn1 fails before unblocking the
// others (which only return after ctx is cancelled).
func TestParallel_FirstErrorWins(t *testing.T) {
	errFirst := errors.New("first")
	errLater := errors.New("later")

	got := parallel(context.Background(),
		func(ctx context.Context) error {
			return errFirst // fails immediately, triggers cancel
		},
		func(ctx context.Context) error {
			<-ctx.Done() // unblocked only by fn1's cancel
			return errLater
		},
		func(ctx context.Context) error {
			<-ctx.Done()
			return errLater
		},
	)
	if !errors.Is(got, errFirst) {
		t.Fatalf("want first error %v, got %v", errFirst, got)
	}
	if errors.Is(got, errLater) {
		t.Fatalf("later error must be dropped, got %v", got)
	}
}

// TestParallel_SiblingCancellation — on the first error the derived context
// is cancelled so siblings' in-flight work aborts (the literal
// ai-agents-pipeline.md:1703 requirement).
func TestParallel_SiblingCancellation(t *testing.T) {
	boom := errors.New("boom")
	var siblingSawCancel atomic.Bool
	start := make(chan struct{})

	got := parallel(context.Background(),
		func(ctx context.Context) error {
			<-start // ensure the sibling is parked on ctx.Done() first
			return boom
		},
		func(ctx context.Context) error {
			close(start)
			<-ctx.Done()
			if errors.Is(ctx.Err(), context.Canceled) {
				siblingSawCancel.Store(true)
			}
			return ctx.Err()
		},
	)
	if !errors.Is(got, boom) {
		t.Fatalf("want %v, got %v", boom, got)
	}
	if !siblingSawCancel.Load() {
		t.Fatal("sibling did not observe context cancellation on first error")
	}
}

// TestParallel_DomainErrorPropagatesVerbatim — a *model.DomainError survives
// the join UNWRAPPED so the Orchestrator's errors.As works (the load-bearing
// D4 / additional-finding-3 invariant). The returned error must be the SAME
// pointer (no copy/wrap).
func TestParallel_DomainErrorPropagatesVerbatim(t *testing.T) {
	want := model.NewDomainError(model.ErrCodeAgentTimeout, model.StageAgentRiskDetection).
		WithAttribute("agent_id", model.AgentRiskDetection.String())

	got := parallel(context.Background(),
		func(ctx context.Context) error { return want },
		func(ctx context.Context) error { <-ctx.Done(); return ctx.Err() },
	)

	var de *model.DomainError
	if !errors.As(got, &de) {
		t.Fatalf("errors.As(*model.DomainError) failed for %v", got)
	}
	if de != want {
		t.Fatalf("DomainError must propagate verbatim (same pointer); got %p want %p", de, want)
	}
	if de.Code != model.ErrCodeAgentTimeout || de.Stage != model.StageAgentRiskDetection {
		t.Fatalf("DomainError fields mutated: %+v", de)
	}
}

// TestParallel_AllSucceed_NilAndParentCtxAlive — all fns succeed ⇒ nil; the
// PARENT ctx is never cancelled by parallel (only the internal derived ctx).
func TestParallel_AllSucceed_NilAndParentCtxAlive(t *testing.T) {
	parent, cancel := context.WithCancel(context.Background())
	defer cancel()

	var ran atomic.Int32
	got := parallel(parent,
		func(ctx context.Context) error { ran.Add(1); return nil },
		func(ctx context.Context) error { ran.Add(1); return nil },
		func(ctx context.Context) error { ran.Add(1); return nil },
	)
	if got != nil {
		t.Fatalf("want nil, got %v", got)
	}
	if ran.Load() != 3 {
		t.Fatalf("want all 3 fns run, got %d", ran.Load())
	}
	if parent.Err() != nil {
		t.Fatalf("parent ctx must stay alive, got %v", parent.Err())
	}
}

// TestParallel_NoGoroutineLeak — every fn goroutine is joined before
// parallel returns (no detached goroutine; required for the -race
// disjoint-write invariant to be meaningful).
func TestParallel_NoGoroutineLeak(t *testing.T) {
	base := runtime.NumGoroutine()
	var done sync.WaitGroup
	done.Add(1)

	go func() {
		defer done.Done()
		_ = parallel(context.Background(),
			func(ctx context.Context) error { return nil },
			func(ctx context.Context) error { time.Sleep(2 * time.Millisecond); return nil },
			func(ctx context.Context) error { return errors.New("x") },
		)
	}()
	done.Wait()

	// Allow the scheduler to reclaim finished goroutines, then assert we did
	// not leak relative to the pre-call baseline.
	deadline := time.Now().Add(time.Second)
	for runtime.NumGoroutine() > base && time.Now().Before(deadline) {
		time.Sleep(2 * time.Millisecond)
	}
	if n := runtime.NumGoroutine(); n > base {
		t.Fatalf("goroutine leak: baseline %d, now %d", base, n)
	}
}
