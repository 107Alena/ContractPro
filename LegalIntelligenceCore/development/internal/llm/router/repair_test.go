package router

import (
	"context"
	"errors"
	"testing"

	"contractpro/legal-intelligence-core/internal/domain/port"
)

// Step 3: CompleteRepair is sticky on usedProvider, NOT the agent primary.
func TestCompleteRepair_StickyOnUsedProvider(t *testing.T) {
	t.Parallel()
	provs, claude, openai, _ := threeProviders()
	openai.completeFn = func(_ context.Context, req port.CompletionRequest, _ int) (port.CompletionResponse, error) {
		return okResponse(port.ProviderOpenAI, req.Model), nil
	}
	usage := &recordingUsage{}
	r := newTestRouter(t, provs, defaultTestConfig(), Deps{UsageTracker: usage})

	// Primary is claude, but the original Complete was served by openai
	// (after a fallback). Repair MUST hit openai, never claude.
	resp, err := r.CompleteRepair(context.Background(), baseReq(), port.ProviderOpenAI)
	if err != nil {
		t.Fatalf("CompleteRepair: %v", err)
	}
	if resp.ProviderID != port.ProviderOpenAI {
		t.Fatalf("served by %q, want openai", resp.ProviderID)
	}
	if claude.completeCount() != 0 {
		t.Fatal("repair must be sticky on usedProvider, not the agent primary (claude)")
	}
	if openai.completeCount() != 1 {
		t.Fatalf("openai calls = %d, want exactly 1 (single shot, no retry)", openai.completeCount())
	}
	// metrics/llm.go SSOT: every CompleteRepair invocation → calls{repair}.
	if usage.count("call", OutcomeRepair) != 1 {
		t.Fatalf("want exactly one ObserveCall(repair), got events %+v", usage.snapshot())
	}
	if usage.count("success", OutcomeSuccess) != 0 {
		t.Fatal("repair must NOT emit ObserveSuccess (wrong outcome label)")
	}
}

// Repair never retries on the same provider and never falls back.
func TestCompleteRepair_NoRetryNoFallback(t *testing.T) {
	t.Parallel()
	provs, claude, openai, _ := threeProviders()
	claude.completeFn = func(context.Context, port.CompletionRequest, int) (port.CompletionResponse, error) {
		// SERVER_ERROR is Retryable+FallbackEligible for the PRIMARY path,
		// but repair must ignore both: single shot, sticky.
		return port.CompletionResponse{}, port.NewLLMProviderError(port.LLMErrorServerError, errors.New("503"))
	}
	usage := &recordingUsage{}
	mx := &recordingMetrics{}
	r := newTestRouter(t, provs, defaultTestConfig(), Deps{UsageTracker: usage, Metrics: mx, sleep: instantSleep})

	_, err := r.CompleteRepair(context.Background(), baseReq(), port.ProviderClaude)
	pe, ok := port.AsLLMProviderError(err)
	if !ok || pe.Code != port.LLMErrorServerError {
		t.Fatalf("err = %v, want the provider SERVER_ERROR returned verbatim", err)
	}
	if claude.completeCount() != 1 {
		t.Fatalf("claude calls = %d, want 1 (no same-provider retry in repair)", claude.completeCount())
	}
	if openai.completeCount() != 0 {
		t.Fatal("repair must never fall back to another provider")
	}
	if usage.count("call", OutcomeRepair) != 1 {
		t.Fatalf("repair must record exactly one calls{repair}; events %+v", usage.snapshot())
	}
	if mx.failedCount() != 1 {
		t.Fatalf("failed_total = %d, want 1", mx.failedCount())
	}
}

// Unhealthy sticky provider → immediate escalation with the LITERAL
// {SERVER_ERROR, false, false} flags (MF-4), NOT the NewLLMProviderError
// catalog defaults ({true,true}), and NO fallback.
func TestCompleteRepair_UnhealthyEscalatesNoFallback(t *testing.T) {
	t.Parallel()
	provs, claude, openai, _ := threeProviders()
	usage := &recordingUsage{}
	r := newTestRouter(t, provs, defaultTestConfig(), Deps{UsageTracker: usage})
	// Three transient failures flip claude unhealthy via the single
	// transition path.
	for i := 0; i < transientUnhealthyThreshold; i++ {
		r.registry.recordFailure(port.ProviderClaude, port.NewLLMProviderError(port.LLMErrorServerError, nil), nil)
	}

	_, err := r.CompleteRepair(context.Background(), baseReq(), port.ProviderClaude)
	pe, ok := port.AsLLMProviderError(err)
	if !ok || pe.Code != port.LLMErrorServerError {
		t.Fatalf("err = %v, want SERVER_ERROR escalation", err)
	}
	if pe.Retryable || pe.FallbackEligible {
		t.Fatalf("escalation flags = {retry:%v fallback:%v}, want {false false} (MF-4 literal struct)",
			pe.Retryable, pe.FallbackEligible)
	}
	if claude.completeCount() != 0 || openai.completeCount() != 0 {
		t.Fatal("unhealthy sticky provider must escalate without any wire call or fallback")
	}
	// N-1: a pre-wire escalation is still a CompleteRepair invocation —
	// metrics/llm.go SSOT requires calls{repair} to increment.
	if usage.count("call", OutcomeRepair) != 1 {
		t.Fatalf("escalation must still record calls{repair}; events %+v", usage.snapshot())
	}
}

// Repair on a provider the router never registered → MALFORMED, not a panic.
func TestCompleteRepair_UnregisteredProvider(t *testing.T) {
	t.Parallel()
	provs, _, _, _ := threeProviders()
	r := newTestRouter(t, provs, defaultTestConfig(), Deps{})

	_, err := r.CompleteRepair(context.Background(), baseReq(), port.LLMProviderID("mistral"))
	pe, ok := port.AsLLMProviderError(err)
	if !ok || pe.Code != port.LLMErrorMalformedRequest {
		t.Fatalf("err = %v, want MALFORMED_REQUEST", err)
	}
	if pe.Retryable || pe.FallbackEligible {
		t.Fatal("a caller bug must be non-retryable, non-fallback")
	}
}

// Rate-limit error on the sticky provider is returned as-is (still records
// the repair call so calls_total is not undercounted).
func TestCompleteRepair_RateLimitError(t *testing.T) {
	t.Parallel()
	provs, claude, _, _ := threeProviders()
	rl := &fakeRateLimiter{waitErr: port.NewLLMProviderError(port.LLMErrorRateLimit, context.DeadlineExceeded)}
	usage := &recordingUsage{}
	r := newTestRouter(t, provs, defaultTestConfig(), Deps{RateLimiter: rl, UsageTracker: usage})

	_, err := r.CompleteRepair(context.Background(), baseReq(), port.ProviderClaude)
	if pe, ok := port.AsLLMProviderError(err); !ok || pe.Code != port.LLMErrorRateLimit {
		t.Fatalf("err = %v, want RATE_LIMIT returned verbatim", err)
	}
	if claude.completeCount() != 0 {
		t.Fatal("must not call the provider when the rate limiter denied within ctx")
	}
	if usage.count("call", OutcomeRepair) != 1 {
		t.Fatalf("repair call must still be recorded; events %+v", usage.snapshot())
	}
}
