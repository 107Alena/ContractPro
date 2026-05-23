package fakes

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"contractpro/legal-intelligence-core/internal/domain/model"
	"contractpro/legal-intelligence-core/internal/domain/port"
)

func TestNewFakeLLMProvider_ID(t *testing.T) {
	p := NewFakeLLMProvider(port.ProviderClaude)
	if p.ID() != port.ProviderClaude {
		t.Fatalf("id mismatch: %s", p.ID())
	}
}

func TestComplete_NoResponseReturnsTypedMalformed(t *testing.T) {
	p := NewFakeLLMProvider(port.ProviderClaude)
	_, err := p.Complete(context.Background(), port.CompletionRequest{
		AgentID: model.AgentSummary, Model: "claude-sonnet-4-6",
	})
	if err == nil {
		t.Fatal("expected error")
	}
	var lerr *port.LLMProviderError
	if !errors.As(err, &lerr) {
		t.Fatalf("expected *LLMProviderError, got %T", err)
	}
	if lerr.Code != port.LLMErrorMalformedRequest {
		t.Fatalf("code: %s", lerr.Code)
	}
}

func TestComplete_ReturnsQueuedResponseFIFO(t *testing.T) {
	p := NewFakeLLMProvider(port.ProviderClaude)
	p.SetResponseJSON(model.AgentSummary, "claude-sonnet-4-6", `{"summary":"a"}`)
	p.SetResponseJSON(model.AgentSummary, "claude-sonnet-4-6", `{"summary":"b"}`)
	for i, want := range []string{`{"summary":"a"}`, `{"summary":"b"}`} {
		resp, err := p.Complete(context.Background(), port.CompletionRequest{
			AgentID: model.AgentSummary, Model: "claude-sonnet-4-6",
		})
		if err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
		if resp.Content != want {
			t.Fatalf("call %d got %q want %q", i, resp.Content, want)
		}
	}
}

func TestComplete_ErrorWinsOverResponse(t *testing.T) {
	p := NewFakeLLMProvider(port.ProviderClaude)
	p.SetResponseJSON(model.AgentSummary, "claude", `{"summary":"x"}`)
	p.InjectError(model.AgentSummary, "claude", errors.New("boom"))

	_, err := p.Complete(context.Background(), port.CompletionRequest{
		AgentID: model.AgentSummary, Model: "claude",
	})
	if err == nil {
		t.Fatal("expected error")
	}
	// The canned response must still be queued.
	if p.Pending(model.AgentSummary, "claude") != 1 {
		t.Fatalf("queued response was consumed: pending=%d", p.Pending(model.AgentSummary, "claude"))
	}
}

func TestComplete_BareErrorWrappedToTyped(t *testing.T) {
	p := NewFakeLLMProvider(port.ProviderClaude)
	p.InjectError(model.AgentSummary, "claude", errors.New("transport"))
	_, err := p.Complete(context.Background(), port.CompletionRequest{
		AgentID: model.AgentSummary, Model: "claude",
	})
	var lerr *port.LLMProviderError
	if !errors.As(err, &lerr) {
		t.Fatalf("not *LLMProviderError: %T", err)
	}
	if lerr.Code != port.LLMErrorNetwork {
		t.Fatalf("expected NETWORK, got %s", lerr.Code)
	}
}

func TestComplete_TypedErrorPassthrough(t *testing.T) {
	p := NewFakeLLMProvider(port.ProviderClaude)
	typed := port.NewLLMProviderError(port.LLMErrorRateLimit, errors.New("429"))
	p.InjectError(model.AgentSummary, "claude", typed)
	_, err := p.Complete(context.Background(), port.CompletionRequest{
		AgentID: model.AgentSummary, Model: "claude",
	})
	if err != typed {
		t.Fatalf("expected verbatim passthrough, got %v", err)
	}
}

func TestComplete_RecordsCall(t *testing.T) {
	p := NewFakeLLMProvider(port.ProviderClaude)
	p.SetResponseJSON(model.AgentSummary, "claude", `{}`)
	_, err := p.Complete(context.Background(), port.CompletionRequest{
		AgentID:    model.AgentSummary,
		Model:      "claude",
		System:     "sys",
		User:       "user-text",
		JSONSchema: []byte(`{"$schema":"http://json-schema.org/draft-07/schema#"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	calls := p.Calls()
	if len(calls) != 1 {
		t.Fatalf("calls=%d", len(calls))
	}
	c := calls[0]
	if c.AgentID != model.AgentSummary || c.Model != "claude" {
		t.Fatalf("agent/model: %+v", c)
	}
	if !c.JSONSchemaSet {
		t.Fatal("JSONSchemaSet should be true")
	}
	if c.System != "sys" || c.User != "user-text" {
		t.Fatalf("system/user: %+v", c)
	}
}

func TestComplete_RespectsCtxCancelBeforeLatency(t *testing.T) {
	p := NewFakeLLMProvider(port.ProviderClaude)
	p.SetLatency(50 * time.Millisecond)
	p.SetResponseJSON(model.AgentSummary, "claude", `{}`)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()
	_, err := p.Complete(ctx, port.CompletionRequest{
		AgentID: model.AgentSummary, Model: "claude",
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected DeadlineExceeded, got %v", err)
	}
}

func TestComplete_LatencyApplies(t *testing.T) {
	p := NewFakeLLMProvider(port.ProviderClaude)
	p.SetLatency(20 * time.Millisecond)
	p.SetResponseJSON(model.AgentSummary, "claude", `{}`)
	start := time.Now()
	_, err := p.Complete(context.Background(), port.CompletionRequest{
		AgentID: model.AgentSummary, Model: "claude",
	})
	if err != nil {
		t.Fatal(err)
	}
	if time.Since(start) < 20*time.Millisecond {
		t.Fatalf("latency not applied: %v", time.Since(start))
	}
}

func TestHealthCheck_Healthy(t *testing.T) {
	p := NewFakeLLMProvider(port.ProviderClaude)
	typed, transport := p.HealthCheck(context.Background())
	if typed != nil || transport != nil {
		t.Fatalf("expected healthy, got typed=%v transport=%v", typed, transport)
	}
}

func TestHealthCheck_TypedFailure(t *testing.T) {
	p := NewFakeLLMProvider(port.ProviderClaude)
	want := port.NewLLMProviderError(port.LLMErrorInvalidAPIKey, errors.New("401"))
	p.SetHealth(want, nil)
	typed, transport := p.HealthCheck(context.Background())
	if typed != want || transport != nil {
		t.Fatalf("typed=%v transport=%v", typed, transport)
	}
}

func TestHealthCheck_TransportFailure(t *testing.T) {
	p := NewFakeLLMProvider(port.ProviderClaude)
	transportErr := errors.New("dns")
	p.SetHealth(nil, transportErr)
	typed, transport := p.HealthCheck(context.Background())
	if typed != nil || transport != transportErr {
		t.Fatalf("typed=%v transport=%v", typed, transport)
	}
}

func TestSetResponses_Replaces(t *testing.T) {
	p := NewFakeLLMProvider(port.ProviderClaude)
	p.SetResponseJSON(model.AgentSummary, "claude", `{"old":1}`)
	p.SetResponses(model.AgentSummary, "claude", []CompletionScript{
		{Content: `{"new":1}`, StopReason: port.StopReasonEndTurn},
	})
	resp, _ := p.Complete(context.Background(), port.CompletionRequest{
		AgentID: model.AgentSummary, Model: "claude",
	})
	if resp.Content != `{"new":1}` {
		t.Fatalf("content: %s", resp.Content)
	}
}

func TestDefaultResponse_UsedAfterFIFODrained(t *testing.T) {
	p := NewFakeLLMProvider(port.ProviderClaude)
	p.SetDefaultResponse(CompletionScript{Content: `{"default":true}`, StopReason: port.StopReasonEndTurn})
	resp, _ := p.Complete(context.Background(), port.CompletionRequest{
		AgentID: model.AgentSummary, Model: "claude",
	})
	if resp.Content != `{"default":true}` {
		t.Fatalf("default not used: %s", resp.Content)
	}
}

func TestFakeLLMProvider_ConcurrentCompleteRaceClean(t *testing.T) {
	p := NewFakeLLMProvider(port.ProviderClaude)
	p.SetDefaultResponse(CompletionScript{Content: `{}`, StopReason: port.StopReasonEndTurn})

	const G = 16
	const N = 32
	var wg sync.WaitGroup
	var ok atomic.Int64
	for g := 0; g < G; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < N; i++ {
				if _, err := p.Complete(context.Background(), port.CompletionRequest{
					AgentID: model.AgentSummary, Model: "claude",
				}); err == nil {
					ok.Add(1)
				}
			}
		}()
	}
	wg.Wait()
	if ok.Load() != int64(G*N) {
		t.Fatalf("ok=%d want=%d", ok.Load(), G*N)
	}
	if len(p.Calls()) != G*N {
		t.Fatalf("calls=%d", len(p.Calls()))
	}
}
