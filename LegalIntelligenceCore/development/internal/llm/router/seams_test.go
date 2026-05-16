package router

import (
	"context"
	"testing"
	"time"
)

// TestCallOutcome_WireStringsPinned freezes the four lic_llm_calls_total
// {outcome} label values. CallOutcome is a local mirror of cost.Outcome
// (itself a mirror of metrics.LLMCallOutcome, observability.md §3.4); the
// LIC-TASK-047 adapter does `cost.Outcome(routerOutcome)`, so a silent
// drift here would mislabel every call metric. Identical guard to cost's
// TestOutcome_WireStringsPinned (intentional cross-package duplication —
// the seam stays hermetic by NOT importing cost).
func TestCallOutcome_WireStringsPinned(t *testing.T) {
	t.Parallel()
	want := map[CallOutcome]string{
		OutcomeSuccess:  "success",
		OutcomeRepair:   "repair",
		OutcomeFail:     "fail",
		OutcomeFallback: "fallback",
	}
	for got, s := range want {
		if string(got) != s {
			t.Fatalf("CallOutcome %q drifted from SSOT wire string %q", got, s)
		}
	}
}

func TestHealthState_WireStringsPinned(t *testing.T) {
	t.Parallel()
	want := map[HealthState]string{
		HealthHealthy:   "healthy",
		HealthUnhealthy: "unhealthy",
		HealthPermanent: "permanent",
	}
	for got, s := range want {
		if string(got) != s {
			t.Fatalf("HealthState %q drifted from §2.3 wire string %q", got, s)
		}
	}
}

// Nil seams degrade to noop defaults that never panic.
func TestNoopSeams(t *testing.T) {
	t.Parallel()
	d := Deps{}.withDefaults()
	if err := d.RateLimiter.Wait(context.Background(), "x"); err != nil {
		t.Fatalf("noop rate limiter must never error: %v", err)
	}
	d.UsageTracker.ObserveSuccess("p", "m", "a", okResponse("p", "m"))
	d.UsageTracker.ObserveCall("p", "m", "a", OutcomeFail)
	d.Metrics.ProviderFallback("a", "b", "agent")
	d.Metrics.ProviderSkippedUnhealthy("p")
	d.Metrics.ProviderFailed("p", "CODE")
	d.Metrics.ProviderHealthState("p", HealthHealthy)
}

// sleepCtx returns nil after the wait and ctx.Err() if ctx fires first.
func TestSleepCtx(t *testing.T) {
	t.Parallel()
	if err := sleepCtx(context.Background(), 0); err != nil {
		t.Fatalf("zero/neg duration with live ctx → nil, got %v", err)
	}
	if err := sleepCtx(context.Background(), time.Millisecond); err != nil {
		t.Fatalf("normal sleep → nil, got %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := sleepCtx(ctx, time.Hour); err == nil {
		t.Fatal("a cancelled ctx must short-circuit the backoff")
	}
}
