package ratelimit

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"contractpro/legal-intelligence-core/internal/domain/port"
)

func cfg(rps float64, burst int, ids ...port.LLMProviderID) Config {
	m := make(map[port.LLMProviderID]ProviderLimit, len(ids))
	for _, id := range ids {
		m[id] = ProviderLimit{RPS: rps, Burst: burst}
	}
	return Config{Providers: m}
}

func TestNewLimiter_Validation(t *testing.T) {
	f := newFakeEvaluator()
	tests := []struct {
		name    string
		cfg     Config
		eval    LuaEvaluator
		wantErr bool
	}{
		{"ok", cfg(10, 20, port.ProviderClaude), f, false},
		{"nil eval", cfg(10, 20, port.ProviderClaude), nil, true},
		{"empty providers", Config{Providers: map[port.LLMProviderID]ProviderLimit{}}, f, true},
		{"zero rps", Config{Providers: map[port.LLMProviderID]ProviderLimit{port.ProviderClaude: {RPS: 0, Burst: 1}}}, f, true},
		{"negative rps", Config{Providers: map[port.LLMProviderID]ProviderLimit{port.ProviderClaude: {RPS: -1, Burst: 1}}}, f, true},
		{"burst < 1", Config{Providers: map[port.LLMProviderID]ProviderLimit{port.ProviderClaude: {RPS: 1, Burst: 0}}}, f, true},
		{"empty id", Config{Providers: map[port.LLMProviderID]ProviderLimit{"": {RPS: 1, Burst: 1}}}, f, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewLimiter(tc.cfg, tc.eval, nil)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr = %v", err, tc.wantErr)
			}
		})
	}
}

func TestNewLimiter_NilObserverIsNoop(t *testing.T) {
	l, err := NewLimiter(cfg(10, 20, port.ProviderClaude), newFakeEvaluator(), nil)
	if err != nil {
		t.Fatalf("NewLimiter: %v", err)
	}
	if _, ok := l.obs.(noopObserver); !ok {
		t.Fatalf("nil observer must degrade to noopObserver, got %T", l.obs)
	}
	if err := l.Wait(context.Background(), port.ProviderClaude); err != nil {
		t.Fatalf("Wait with noop observer: %v", err)
	}
}

func TestWait_AllowsImmediatelyWithinBurst(t *testing.T) {
	obs := &recordingObserver{}
	l, _ := NewLimiter(cfg(10, 20, port.ProviderClaude), newFakeEvaluator().withVirtualClock(0), obs)
	for i := 0; i < 20; i++ {
		if err := l.Wait(context.Background(), port.ProviderClaude); err != nil {
			t.Fatalf("burst Wait %d: %v", i+1, err)
		}
	}
	if rl, fo, an := obs.snapshot(); rl != 0 || fo != 0 || an != 0 {
		t.Fatalf("no signals expected within burst, got rl=%d fo=%d an=%d", rl, fo, an)
	}
}

func TestWait_SustainsRPSOver1s(t *testing.T) {
	// test_step 2: bucket sustains RPS=10 over 1s. Virtual clock keeps it
	// deterministic & -race clean (no real sleeping in the allowed path).
	f := newFakeEvaluator().withVirtualClock(0)
	obs := &recordingObserver{}
	l, _ := NewLimiter(cfg(10, 20, port.ProviderClaude), f, obs)

	for i := 0; i < 20; i++ { // drain burst
		if err := l.Wait(context.Background(), port.ProviderClaude); err != nil {
			t.Fatalf("drain %d: %v", i+1, err)
		}
	}
	for i := 0; i < 10; i++ { // 10 req over a simulated second at 10rps
		f.advance(100 * time.Millisecond)
		if err := l.Wait(context.Background(), port.ProviderClaude); err != nil {
			t.Fatalf("sustained req %d: %v", i+1, err)
		}
	}
	if rl, _, _ := obs.snapshot(); rl != 0 {
		t.Fatalf("steady 10rps must not be rate-limited, got %d denials", rl)
	}
}

func TestWait_DeniedThenRefilledIsAllowed(t *testing.T) {
	// burst=1: 2nd immediate call is denied; after the timer fires and the
	// virtual clock has advanced enough, the retry within Wait succeeds.
	f := newFakeEvaluator().withVirtualClock(0)
	obs := &recordingObserver{firstDenied: make(chan struct{}, 1)}
	l, _ := NewLimiter(cfg(50, 1, port.ProviderClaude), f, obs)

	if err := l.Wait(context.Background(), port.ProviderClaude); err != nil {
		t.Fatalf("first Wait: %v", err)
	}

	// Advance the virtual clock only AFTER a denial is proven (the Wait
	// goroutine is then provably parked on its real backoff timer), so the
	// in-loop retry observes the refill — no fragile sleep, no vacuous assert.
	done := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		done <- l.Wait(ctx, port.ProviderClaude)
	}()

	select {
	case <-obs.firstDenied:
	case <-time.After(2 * time.Second):
		t.Fatal("expected a denial within 2s, got none")
	}
	f.advance(time.Second) // plenty of refill for rps=50
	if err := <-done; err != nil {
		t.Fatalf("Wait after refill: %v", err)
	}
	if rl, _, _ := obs.snapshot(); rl < 1 {
		t.Fatalf("the denied attempt must have incremented RateLimited, got %d", rl)
	}
}

func TestWait_UnknownProviderMalformed(t *testing.T) {
	l, _ := NewLimiter(cfg(10, 20, port.ProviderClaude), newFakeEvaluator(), &recordingObserver{})
	err := l.Wait(context.Background(), port.ProviderOpenAI)
	pe, ok := port.AsLLMProviderError(err)
	if !ok {
		t.Fatalf("unknown provider must yield *LLMProviderError, got %T %v", err, err)
	}
	if pe.Code != port.LLMErrorMalformedRequest {
		t.Fatalf("code = %s, want MALFORMED_REQUEST", pe.Code)
	}
	if pe.Retryable || pe.FallbackEligible {
		t.Fatalf("MALFORMED must be neither retryable nor fallback-eligible")
	}
}

func TestWait_CtxAlreadyCancelled(t *testing.T) {
	l, _ := NewLimiter(cfg(10, 20, port.ProviderClaude), newFakeEvaluator(), &recordingObserver{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := l.Wait(ctx, port.ProviderClaude)
	pe, ok := port.AsLLMProviderError(err)
	if !ok {
		t.Fatalf("want *LLMProviderError, got %T %v", err, err)
	}
	if pe.Code != port.LLMErrorRateLimit {
		t.Fatalf("code = %s, want RATE_LIMIT", pe.Code)
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err must wrap context.Canceled, got %v", err)
	}
}

func TestWait_CtxDeadlineDuringBackpressure(t *testing.T) {
	// Bucket permanently denies (clock never advances): Wait must give up
	// when ctx's deadline passes, returning an error that BOTH the §2.1
	// router branches accept (errors.Is DeadlineExceeded + *LLMProviderError).
	f := newFakeEvaluator().withVirtualClock(0)
	obs := &recordingObserver{}
	l, _ := NewLimiter(cfg(200, 1, port.ProviderClaude), f, obs)

	if err := l.Wait(context.Background(), port.ProviderClaude); err != nil {
		t.Fatalf("first (burst) Wait: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	start := time.Now()
	err := l.Wait(ctx, port.ProviderClaude)
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Fatalf("Wait did not respect ctx deadline, took %v", elapsed)
	}

	pe, ok := port.AsLLMProviderError(err)
	if !ok {
		t.Fatalf("router non-deadline path needs *LLMProviderError, got %T %v", err, err)
	}
	if pe.Code != port.LLMErrorRateLimit || !pe.Retryable || !pe.FallbackEligible {
		t.Fatalf("RATE_LIMIT must be retryable+fallback-eligible, got %+v", pe)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("router deadline branch needs errors.Is(DeadlineExceeded); got %v", err)
	}
	if rl, _, _ := obs.snapshot(); rl < 1 {
		t.Fatalf("backpressure denials must increment RateLimited, got %d", rl)
	}
}

func TestWait_FailOpenOnInfraError(t *testing.T) {
	f := newFakeEvaluator()
	f.setForcedErr(errors.New("redis: connection pool timeout"))
	obs := &recordingObserver{}
	l, _ := NewLimiter(cfg(10, 20, port.ProviderClaude), f, obs)

	if err := l.Wait(context.Background(), port.ProviderClaude); err != nil {
		t.Fatalf("infra failure must fail-OPEN (nil), got %v", err)
	}
	rl, fo, an := obs.snapshot()
	if fo != 1 || rl != 0 || an != 0 {
		t.Fatalf("infra failure → FailOpen only; got rl=%d fo=%d an=%d", rl, fo, an)
	}
}

func TestWait_FailOpenOnScriptAnomaly(t *testing.T) {
	for _, m := range []anomalyMode{anomalyNilReply, anomalyBadShape, anomalyBadElem} {
		f := newFakeEvaluator()
		f.setAnomaly(m)
		obs := &recordingObserver{}
		l, _ := NewLimiter(cfg(10, 20, port.ProviderClaude), f, obs)

		if err := l.Wait(context.Background(), port.ProviderClaude); err != nil {
			t.Fatalf("anomaly %v must fail-OPEN (nil), got %v", m, err)
		}
		rl, fo, an := obs.snapshot()
		if an != 1 || rl != 0 || fo != 0 {
			t.Fatalf("anomaly %v → ScriptAnomaly only; got rl=%d fo=%d an=%d", m, rl, fo, an)
		}
	}
}

func TestComputeSleep_Bounds(t *testing.T) {
	l := &Limiter{maxSleep: maxSleepDefault}

	// rps=10 → minSleep = max(1ms, 1/(2*10)s=50ms) = 50ms.
	l.randF = func() float64 { return 0 }
	if got := l.computeSleep(10*time.Millisecond, 10); got != 50*time.Millisecond {
		t.Fatalf("jitter=0 → floor minSleep=50ms, got %v", got)
	}
	l.randF = func() float64 { return 0.999999 }
	hi := l.computeSleep(50*time.Millisecond, 10) // base=50ms, upper=60ms
	if hi <= 50*time.Millisecond || hi > 60*time.Millisecond {
		t.Fatalf("jittered sleep = %v, want (50ms, 60ms]", hi)
	}

	// maxSleep cap: huge retry-after must not exceed maxSleep (MF-5).
	l.randF = func() float64 { return 0.999999 }
	if got := l.computeSleep(10*time.Minute, 100); got > l.maxSleep {
		t.Fatalf("computeSleep = %v, must be capped at %v", got, l.maxSleep)
	}

	// Very low rps misconfig: minSleep (1/(2·0.01)s = 50s) is capped to
	// maxSleep, base=max(0,2s)=2s, upper=2s, span=0 → exactly maxSleep.
	l.randF = func() float64 { return 0 }
	if got := l.computeSleep(0, 0.01); got != l.maxSleep {
		t.Fatalf("low-rps sleep = %v, want exactly %v (minSleep capped)", got, l.maxSleep)
	}
}

func TestClampToDeadline(t *testing.T) {
	if got := clampToDeadline(context.Background(), time.Second); got != time.Second {
		t.Fatalf("no deadline → unchanged, got %v", got)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if got := clampToDeadline(ctx, time.Hour); got > 20*time.Millisecond {
		t.Fatalf("clamp = %v, want <= 20ms", got)
	}
}

func TestWait_ConcurrentRaceClean(t *testing.T) {
	// Generous bucket → every goroutine is allowed immediately; the point is
	// the -race detector on the shared Limiter / fake / observer.
	f := newFakeEvaluator().withVirtualClock(0)
	obs := &recordingObserver{}
	l, _ := NewLimiter(cfg(1000, 5000, port.ProviderClaude, port.ProviderOpenAI), f, obs)

	var wg sync.WaitGroup
	for i := 0; i < 64; i++ {
		wg.Add(1)
		prov := port.ProviderClaude
		if i%2 == 0 {
			prov = port.ProviderOpenAI
		}
		go func(p port.LLMProviderID) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if err := l.Wait(ctx, p); err != nil {
				t.Errorf("concurrent Wait(%s): %v", p, err)
			}
		}(prov)
	}
	wg.Wait()
}
