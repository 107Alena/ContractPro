package router

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"contractpro/legal-intelligence-core/internal/domain/model"
	"contractpro/legal-intelligence-core/internal/domain/port"
)

func baseReq() port.CompletionRequest {
	return port.CompletionRequest{
		AgentID: model.AgentTypeClassifier,
		Model:   "claude-sonnet-4-6",
		System:  "sys",
		User:    "user",
	}
}

// --- constructor fail-fast --------------------------------------------------

func TestNewProviderRouter_FailFast(t *testing.T) {
	t.Parallel()
	provs, _, _, _ := threeProviders()

	cases := []struct {
		name      string
		providers map[port.LLMProviderID]port.LLMProviderPort
		cfg       RouterConfig
	}{
		{
			name:      "empty providers",
			providers: map[port.LLMProviderID]port.LLMProviderPort{},
			cfg:       defaultTestConfig(),
		},
		{
			name:      "empty fallback order",
			providers: provs,
			cfg:       RouterConfig{AgentPrimary: allClaudePrimary()},
		},
		{
			name:      "fallback references unregistered provider",
			providers: map[port.LLMProviderID]port.LLMProviderPort{port.ProviderClaude: newFakeProvider(port.ProviderClaude)},
			cfg: RouterConfig{
				AgentPrimary:  allClaudePrimary(),
				FallbackOrder: []port.LLMProviderID{port.ProviderClaude, port.ProviderOpenAI},
			},
		},
		{
			name:      "duplicate in fallback order",
			providers: provs,
			cfg: RouterConfig{
				AgentPrimary:  allClaudePrimary(),
				FallbackOrder: []port.LLMProviderID{port.ProviderClaude, port.ProviderClaude},
			},
		},
		{
			name:      "agent primary missing an agent",
			providers: provs,
			cfg: RouterConfig{
				AgentPrimary:  map[model.AgentID]port.LLMProviderID{model.AgentTypeClassifier: port.ProviderClaude},
				FallbackOrder: []port.LLMProviderID{port.ProviderClaude},
			},
		},
		{
			name:      "agent primary points at unregistered provider",
			providers: provs,
			cfg: func() RouterConfig {
				p := allClaudePrimary()
				p[model.AgentRiskDetection] = port.LLMProviderID("mistral")
				return RouterConfig{AgentPrimary: p, FallbackOrder: []port.LLMProviderID{port.ProviderClaude, port.ProviderOpenAI, port.ProviderGemini}}
			}(),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := NewProviderRouter(tc.providers, tc.cfg, Deps{}); err == nil {
				t.Fatalf("expected error for %q, got nil", tc.name)
			}
		})
	}
}

func TestNewProviderRouter_OK_NoopDefaults(t *testing.T) {
	t.Parallel()
	provs, _, _, _ := threeProviders()
	r, err := NewProviderRouter(provs, defaultTestConfig(), Deps{})
	if err != nil {
		t.Fatalf("NewProviderRouter: %v", err)
	}
	if _, _, _ = r.rl, r.usage, r.mx; r.rl == nil || r.usage == nil || r.mx == nil {
		t.Fatal("nil seams must degrade to noop defaults, not nil")
	}
	// Compile-time port conformance is asserted in router.go; assert it is
	// usable as the port type here too.
	var _ port.ProviderRouterPort = r
}

// --- Step 1/primary happy path ---------------------------------------------

func TestComplete_PrimarySuccess(t *testing.T) {
	t.Parallel()
	provs, claude, openai, _ := threeProviders()
	claude.completeFn = func(_ context.Context, req port.CompletionRequest, _ int) (port.CompletionResponse, error) {
		return okResponse(port.ProviderClaude, req.Model), nil
	}
	usage := &recordingUsage{}
	mx := &recordingMetrics{}
	r := newTestRouter(t, provs, defaultTestConfig(), Deps{UsageTracker: usage, Metrics: mx})

	res, err := r.Complete(context.Background(), baseReq())
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if res.UsedProvider != port.ProviderClaude {
		t.Fatalf("UsedProvider = %q, want claude", res.UsedProvider)
	}
	if openai.completeCount() != 0 {
		t.Fatal("fallback provider must not be called on primary success")
	}
	if usage.count("success", OutcomeSuccess) != 1 {
		t.Fatalf("want exactly one ObserveSuccess, got %d", usage.count("success", OutcomeSuccess))
	}
	if mx.fallbackCount() != 0 {
		t.Fatal("no fallback metric on primary success")
	}
}

// --- Step 2: primary 5xx → fallback secondary → success --------------------

func TestComplete_FallbackOnServerError(t *testing.T) {
	t.Parallel()
	provs, claude, openai, gemini := threeProviders()
	claude.completeFn = func(context.Context, port.CompletionRequest, int) (port.CompletionResponse, error) {
		return port.CompletionResponse{}, port.NewLLMProviderError(port.LLMErrorServerError, errors.New("503"))
	}
	openai.completeFn = func(_ context.Context, req port.CompletionRequest, _ int) (port.CompletionResponse, error) {
		return okResponse(port.ProviderOpenAI, req.Model), nil
	}
	usage := &recordingUsage{}
	mx := &recordingMetrics{}
	r := newTestRouter(t, provs, defaultTestConfig(), Deps{UsageTracker: usage, Metrics: mx, sleep: instantSleep})

	res, err := r.Complete(context.Background(), baseReq())
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if res.UsedProvider != port.ProviderOpenAI {
		t.Fatalf("UsedProvider = %q, want openai", res.UsedProvider)
	}
	// claude: 2 calls (1 + 1 same-provider retry, SERVER_ERROR is Retryable).
	if claude.completeCount() != 2 {
		t.Fatalf("claude completeCount = %d, want 2 (call + 1 same-provider retry)", claude.completeCount())
	}
	if gemini.completeCount() != 0 {
		t.Fatal("gemini must not be reached once openai succeeds")
	}
	if mx.fallbackCount() != 1 {
		t.Fatalf("want 1 fallback metric, got %d", mx.fallbackCount())
	}
	if got := mx.fallback[0]; got[0] != "claude" || got[1] != "openai" {
		t.Fatalf("fallback metric = %v, want {claude openai ...}", got)
	}
	// claude failed (fail), openai success.
	if usage.count("call", OutcomeFail) != 1 || usage.count("success", OutcomeSuccess) != 1 {
		t.Fatalf("usage events = %+v", usage.snapshot())
	}
}

// --- same-provider retry happens exactly once ------------------------------

func TestComplete_SameProviderRetryOnce(t *testing.T) {
	t.Parallel()
	provs, claude, openai, _ := threeProviders()
	// claude fails twice (call + 1 retry) then we expect fallback to openai.
	claude.completeFn = func(context.Context, port.CompletionRequest, int) (port.CompletionResponse, error) {
		return port.CompletionResponse{}, port.NewLLMProviderError(port.LLMErrorOverloaded, errors.New("529"))
	}
	openai.completeFn = func(_ context.Context, req port.CompletionRequest, _ int) (port.CompletionResponse, error) {
		return okResponse(port.ProviderOpenAI, req.Model), nil
	}
	mx := &recordingMetrics{}
	r := newTestRouter(t, provs, defaultTestConfig(), Deps{Metrics: mx, sleep: instantSleep})

	if _, err := r.Complete(context.Background(), baseReq()); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if claude.completeCount() != 2 {
		t.Fatalf("claude retried %d times, want exactly 2 attempts (1 retry)", claude.completeCount())
	}
	if openai.completeCount() != 1 {
		t.Fatalf("openai completeCount = %d, want 1", openai.completeCount())
	}
	// failed_total counted once per provider chain-iteration (not per HTTP
	// attempt) — the retry is an internal resilience detail.
	if mx.failedCount() != 1 {
		t.Fatalf("failed_total count = %d, want 1 (per provider iteration)", mx.failedCount())
	}
}

// --- Step 5: ALL_PROVIDERS_FAILED ------------------------------------------

func TestComplete_AllProvidersFailed(t *testing.T) {
	t.Parallel()
	provs, claude, openai, gemini := threeProviders()
	fail := func(context.Context, port.CompletionRequest, int) (port.CompletionResponse, error) {
		return port.CompletionResponse{}, port.NewLLMProviderError(port.LLMErrorServerError, errors.New("503"))
	}
	claude.completeFn, openai.completeFn, gemini.completeFn = fail, fail, fail
	r := newTestRouter(t, provs, defaultTestConfig(), Deps{sleep: instantSleep})

	_, err := r.Complete(context.Background(), baseReq())
	pe, ok := port.AsLLMProviderError(err)
	if !ok || pe.Code != port.LLMErrorAllProvidersFailed {
		t.Fatalf("err = %v, want ALL_PROVIDERS_FAILED", err)
	}
	// Last provider's error must be wrapped for root-cause logging.
	if pe.Wrapped == nil {
		t.Fatal("ALL_PROVIDERS_FAILED must wrap the last provider error")
	}
}

// --- non-fallback fatal codes return immediately ---------------------------

func TestComplete_NonFallbackCodesAreFatal(t *testing.T) {
	t.Parallel()
	for _, code := range []port.LLMErrorCode{port.LLMErrorContextTooLong, port.LLMErrorMalformedRequest} {
		t.Run(string(code), func(t *testing.T) {
			provs, claude, openai, _ := threeProviders()
			claude.completeFn = func(context.Context, port.CompletionRequest, int) (port.CompletionResponse, error) {
				return port.CompletionResponse{}, port.NewLLMProviderError(code, errors.New("boom"))
			}
			r := newTestRouter(t, provs, defaultTestConfig(), Deps{sleep: instantSleep})
			_, err := r.Complete(context.Background(), baseReq())
			pe, ok := port.AsLLMProviderError(err)
			if !ok || pe.Code != code {
				t.Fatalf("err = %v, want %s returned verbatim (no fallback)", err, code)
			}
			if openai.completeCount() != 0 {
				t.Fatal("must not fall back on a non-fallback-eligible code")
			}
			if claude.completeCount() != 1 {
				t.Fatalf("non-retryable code must not retry; claude calls=%d", claude.completeCount())
			}
		})
	}
}

// --- Step 4: INVALID_API_KEY → permanent unhealthy + still falls back ------

func TestComplete_InvalidAPIKeyMarksPermanentAndFallsBack(t *testing.T) {
	t.Parallel()
	provs, claude, openai, _ := threeProviders()
	claude.completeFn = func(context.Context, port.CompletionRequest, int) (port.CompletionResponse, error) {
		return port.CompletionResponse{}, port.NewLLMProviderError(port.LLMErrorInvalidAPIKey, errors.New("401"))
	}
	openai.completeFn = func(_ context.Context, req port.CompletionRequest, _ int) (port.CompletionResponse, error) {
		return okResponse(port.ProviderOpenAI, req.Model), nil
	}
	mx := &recordingMetrics{}
	r := newTestRouter(t, provs, defaultTestConfig(), Deps{Metrics: mx, sleep: instantSleep})

	res, err := r.Complete(context.Background(), baseReq())
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if res.UsedProvider != port.ProviderOpenAI {
		t.Fatalf("UsedProvider = %q, want openai (auth is fallback-eligible)", res.UsedProvider)
	}
	// claude marked permanently unhealthy in the registry.
	h, ok := r.registry.snapshot(port.ProviderClaude)
	if !ok || !h.permanent || h.healthy {
		t.Fatalf("claude registry = %+v, want permanent & not healthy", h)
	}
	if !h.quotaUntil.IsZero() {
		t.Fatal("auth-permanent must have zero quotaUntil (never auto-recovers)")
	}
	if st, _ := mx.lastHealth(port.ProviderClaude); st != string(HealthPermanent) {
		t.Fatalf("health metric for claude = %q, want permanent", st)
	}
	// INVALID_API_KEY is not retryable → exactly one claude call.
	if claude.completeCount() != 1 {
		t.Fatalf("claude calls = %d, want 1 (auth not retryable)", claude.completeCount())
	}
}

// --- skip unhealthy provider at top of iteration ---------------------------

func TestComplete_SkipsUnhealthyProvider(t *testing.T) {
	t.Parallel()
	provs, claude, openai, _ := threeProviders()
	openai.completeFn = func(_ context.Context, req port.CompletionRequest, _ int) (port.CompletionResponse, error) {
		return okResponse(port.ProviderOpenAI, req.Model), nil
	}
	mx := &recordingMetrics{}
	r := newTestRouter(t, provs, defaultTestConfig(), Deps{Metrics: mx})
	// Force claude permanently unhealthy via the registry's single
	// transition path (an auth failure recorded out-of-band).
	r.registry.recordFailure(port.ProviderClaude, port.NewLLMProviderError(port.LLMErrorInvalidAPIKey, nil), nil)

	res, err := r.Complete(context.Background(), baseReq())
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if res.UsedProvider != port.ProviderOpenAI {
		t.Fatalf("UsedProvider = %q, want openai", res.UsedProvider)
	}
	if claude.completeCount() != 0 {
		t.Fatal("unhealthy claude must be skipped, not called")
	}
	if mx.skippedCount() != 1 {
		t.Fatalf("skipped_unhealthy count = %d, want 1", mx.skippedCount())
	}
}

// --- rate-limiter ctx-derived error aborts the whole chain (MF-1) ----------

func TestComplete_RateLimiterCtxErrorAbortsChain(t *testing.T) {
	t.Parallel()
	provs, claude, openai, _ := threeProviders()
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // dead before the call
	rl := &fakeRateLimiter{waitFn: func(c context.Context, _ port.LLMProviderID) error {
		return port.NewLLMProviderError(port.LLMErrorRateLimit, c.Err())
	}}
	r := newTestRouter(t, provs, defaultTestConfig(), Deps{RateLimiter: rl})

	_, err := r.Complete(ctx, baseReq())
	pe, ok := port.AsLLMProviderError(err)
	if !ok || pe.Code != port.LLMErrorRateLimit {
		t.Fatalf("err = %v, want RATE_LIMIT", err)
	}
	if claude.completeCount() != 0 || openai.completeCount() != 0 {
		t.Fatal("a dead ctx must abort the chain, not try every provider")
	}
	if rl.waitCount() != 1 {
		t.Fatalf("rate limiter Wait calls = %d, want 1 (chain aborted after first)", rl.waitCount())
	}
}

// --- rate-limiter non-ctx error skips just that provider -------------------

func TestComplete_RateLimiterNonCtxErrorSkipsProvider(t *testing.T) {
	t.Parallel()
	provs, claude, openai, _ := threeProviders()
	openai.completeFn = func(_ context.Context, req port.CompletionRequest, _ int) (port.CompletionResponse, error) {
		return okResponse(port.ProviderOpenAI, req.Model), nil
	}
	rl := &fakeRateLimiter{waitFn: func(_ context.Context, id port.LLMProviderID) error {
		if id == port.ProviderClaude {
			// MALFORMED == unconfigured-provider wiring bug for claude only.
			return port.NewLLMProviderError(port.LLMErrorMalformedRequest, errors.New("no bucket"))
		}
		return nil
	}}
	r := newTestRouter(t, provs, defaultTestConfig(), Deps{RateLimiter: rl})

	res, err := r.Complete(context.Background(), baseReq())
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if res.UsedProvider != port.ProviderOpenAI {
		t.Fatalf("UsedProvider = %q, want openai (claude skipped on bucket misconfig)", res.UsedProvider)
	}
	if claude.completeCount() != 0 {
		t.Fatal("claude must be skipped when its rate-limit bucket errors")
	}
}

// --- adapter invariant breach (untyped error) degrades to fail-no-fallback -

func TestComplete_UntypedErrorDegradesNoFallback(t *testing.T) {
	t.Parallel()
	provs, claude, openai, _ := threeProviders()
	claude.completeFn = func(context.Context, port.CompletionRequest, int) (port.CompletionResponse, error) {
		return port.CompletionResponse{}, errors.New("bare network error") // invariant breach
	}
	mx := &recordingMetrics{}
	r := newTestRouter(t, provs, defaultTestConfig(), Deps{Metrics: mx, sleep: instantSleep})

	_, err := r.Complete(context.Background(), baseReq())
	if err == nil || err.Error() != "bare network error" {
		t.Fatalf("err = %v, want the raw error returned (no fallback)", err)
	}
	if openai.completeCount() != 0 {
		t.Fatal("untyped error must not trigger fallback (MF-1)")
	}
	if mx.failedCount() != 1 || mx.failed[0][1] != string(unknownCode) {
		t.Fatalf("failed metric = %v, want one UNKNOWN-coded failure", mx.failed)
	}
}

// --- concurrent use is race-clean (parallel errgroup pipeline) -------------

func TestComplete_ConcurrentRaceClean(t *testing.T) {
	t.Parallel()
	provs, claude, openai, _ := threeProviders()
	claude.completeFn = func(context.Context, port.CompletionRequest, int) (port.CompletionResponse, error) {
		return port.CompletionResponse{}, port.NewLLMProviderError(port.LLMErrorServerError, errors.New("503"))
	}
	openai.completeFn = func(_ context.Context, req port.CompletionRequest, _ int) (port.CompletionResponse, error) {
		return okResponse(port.ProviderOpenAI, req.Model), nil
	}
	r := newTestRouter(t, provs, defaultTestConfig(), Deps{
		UsageTracker: &recordingUsage{},
		Metrics:      &recordingMetrics{},
		sleep:        instantSleep,
	})

	var wg sync.WaitGroup
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := r.Complete(context.Background(), baseReq()); err != nil {
				t.Errorf("concurrent Complete: %v", err)
			}
		}()
	}
	wg.Wait()
}

// --- per-agent primary routing --------------------------------------------

func TestComplete_PerAgentPrimary(t *testing.T) {
	t.Parallel()
	provs, claude, openai, _ := threeProviders()
	for _, p := range []*fakeProvider{claude, openai} {
		p.completeFn = func(_ context.Context, req port.CompletionRequest, _ int) (port.CompletionResponse, error) {
			return okResponse(p.id, req.Model), nil
		}
	}
	cfg := defaultTestConfig()
	cfg.AgentPrimary[model.AgentRiskDetection] = port.ProviderOpenAI // override one agent

	r := newTestRouter(t, provs, cfg, Deps{})
	req := baseReq()
	req.AgentID = model.AgentRiskDetection
	res, err := r.Complete(context.Background(), req)
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if res.UsedProvider != port.ProviderOpenAI {
		t.Fatalf("UsedProvider = %q, want openai (per-agent primary override)", res.UsedProvider)
	}
	if claude.completeCount() != 0 {
		t.Fatal("claude must not be called when the agent's primary is openai")
	}
}

// --- MEDIUM-1: a terminal skipped-unhealthy provider must not mask the
// genuine earlier failure in ALL_PROVIDERS_FAILED.Wrapped ------------------

func TestComplete_LastProviderSkippedPreservesRootCause(t *testing.T) {
	t.Parallel()
	provs, claude, openai, _ := threeProviders()
	// claude + openai both fail with CONTENT_POLICY (fallback-eligible,
	// not retryable). gemini (last in chain) is forced unhealthy → skipped.
	cp := func(context.Context, port.CompletionRequest, int) (port.CompletionResponse, error) {
		return port.CompletionResponse{}, port.NewLLMProviderError(port.LLMErrorContentPolicy, errors.New("policy"))
	}
	claude.completeFn, openai.completeFn = cp, cp
	r := newTestRouter(t, provs, defaultTestConfig(), Deps{sleep: instantSleep})
	r.registry.recordFailure(port.ProviderGemini, port.NewLLMProviderError(port.LLMErrorInvalidAPIKey, nil), nil)

	_, err := r.Complete(context.Background(), baseReq())
	pe, ok := port.AsLLMProviderError(err)
	if !ok || pe.Code != port.LLMErrorAllProvidersFailed {
		t.Fatalf("err = %v, want ALL_PROVIDERS_FAILED", err)
	}
	inner, ok := port.AsLLMProviderError(pe.Wrapped)
	if !ok || inner.Code != port.LLMErrorContentPolicy {
		t.Fatalf("Wrapped = %v, want the genuine CONTENT_POLICY root cause, not a synthetic skip marker", pe.Wrapped)
	}
}

// --- backoff timing is honoured (real sleep, short codes) ------------------

func TestBackoffFor(t *testing.T) {
	t.Parallel()
	ra := 3 * time.Second
	cases := []struct {
		pe   *port.LLMProviderError
		want time.Duration
	}{
		{&port.LLMProviderError{Code: port.LLMErrorServerError}, transientBackoff},
		{&port.LLMProviderError{Code: port.LLMErrorNetwork}, transientBackoff},
		{&port.LLMProviderError{Code: port.LLMErrorTimeout}, transientBackoff},
		{&port.LLMProviderError{Code: port.LLMErrorOverloaded}, overloadedBackoff},
		{&port.LLMProviderError{Code: port.LLMErrorRateLimit}, rateLimitDefaultBackoff},
		{&port.LLMProviderError{Code: port.LLMErrorRateLimit, RetryAfter: &ra}, ra},
	}
	for _, tc := range cases {
		if got := backoffFor(tc.pe); got != tc.want {
			t.Fatalf("backoffFor(%s) = %v, want %v", tc.pe.Code, got, tc.want)
		}
	}
}
