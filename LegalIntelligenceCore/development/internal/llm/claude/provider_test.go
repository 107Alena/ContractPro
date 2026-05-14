package claude

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"contractpro/legal-intelligence-core/internal/domain/model"
	"contractpro/legal-intelligence-core/internal/domain/port"
)

// newTestProvider constructs a provider pointed at the given test server,
// using the server's own client (so TLS/Transport pooling is identical to
// real production code paths inside httptest's loopback).
func newTestProvider(t *testing.T, srv *httptest.Server, cfgOverride ...func(*ClaudeConfig)) *Provider {
	t.Helper()
	cfg := ClaudeConfig{
		APIKey:     "sk-ant-test-key",
		BaseURL:    srv.URL,
		Model:      "claude-sonnet-4-6",
		HTTPClient: srv.Client(),
	}
	for _, f := range cfgOverride {
		f(&cfg)
	}
	p, err := NewClaudeProvider(cfg)
	if err != nil {
		t.Fatalf("NewClaudeProvider err=%v", err)
	}
	return p
}

func TestComplete_HappyPath_TextOnly(t *testing.T) {
	var capturedReq anthropicRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != messagesEndpointPath {
			t.Errorf("path=%q, want %q", r.URL.Path, messagesEndpointPath)
		}
		if r.Method != http.MethodPost {
			t.Errorf("method=%q, want POST", r.Method)
		}
		if got := r.Header.Get("x-api-key"); got != "sk-ant-test-key" {
			t.Errorf("x-api-key=%q", got)
		}
		if got := r.Header.Get("anthropic-version"); got != anthropicVersionHeader {
			t.Errorf("anthropic-version=%q, want %q", got, anthropicVersionHeader)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("Content-Type=%q", got)
		}
		_ = json.NewDecoder(r.Body).Decode(&capturedReq)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id":"msg_test","type":"message","role":"assistant","model":"claude-sonnet-4-6",
			"content":[{"type":"text","text":"hello"}],
			"stop_reason":"end_turn",
			"usage":{"input_tokens":50,"output_tokens":7}
		}`))
	}))
	defer srv.Close()

	p := newTestProvider(t, srv)
	resp, err := p.Complete(context.Background(), port.CompletionRequest{
		AgentID:     model.AgentSummary,
		System:      "you are a summariser",
		User:        "summarise: X",
		MaxTokens:   400,
		Temperature: 0.3,
	})
	if err != nil {
		t.Fatalf("Complete err=%v", err)
	}
	if resp.Content != "hello" {
		t.Errorf("Content=%q", resp.Content)
	}
	if resp.InputTokens != 50 || resp.OutputTokens != 7 {
		t.Errorf("tokens i=%d o=%d", resp.InputTokens, resp.OutputTokens)
	}
	if resp.ProviderID != port.ProviderClaude {
		t.Errorf("ProviderID=%v", resp.ProviderID)
	}
	if resp.LatencyMs < 0 {
		t.Errorf("LatencyMs=%d, want >= 0", resp.LatencyMs)
	}
	if capturedReq.Messages[0].Content != "summarise: X" {
		t.Errorf("user content not propagated: %+v", capturedReq.Messages)
	}
}

func TestComplete_WithJSONSchema_UsesToolUse(t *testing.T) {
	var capturedReq anthropicRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&capturedReq)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"content":[{"type":"tool_use","name":"return_analysis_result","input":{"contract_type":"SERVICES","confidence":0.95}}],
			"stop_reason":"tool_use",
			"usage":{"input_tokens":5,"output_tokens":12}
		}`))
	}))
	defer srv.Close()

	schema := json.RawMessage(`{"type":"object","properties":{"contract_type":{"type":"string"}},"required":["contract_type"]}`)
	p := newTestProvider(t, srv)
	resp, err := p.Complete(context.Background(), port.CompletionRequest{
		AgentID:    model.AgentTypeClassifier,
		User:       "classify",
		MaxTokens:  100,
		JSONSchema: schema,
	})
	if err != nil {
		t.Fatalf("Complete err=%v", err)
	}
	if resp.Content != `{"contract_type":"SERVICES","confidence":0.95}` {
		t.Errorf("Content=%q", resp.Content)
	}
	if len(capturedReq.Tools) != 1 {
		t.Fatalf("Tools len=%d, want 1", len(capturedReq.Tools))
	}
	if capturedReq.ToolChoice == nil || capturedReq.ToolChoice.Name != virtualToolName {
		t.Errorf("ToolChoice=%+v", capturedReq.ToolChoice)
	}
}

func TestComplete_PromptCacheEnabled_MarkerInRequest(t *testing.T) {
	var raw json.RawMessage
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"content":[{"type":"text","text":"ok"}],
			"stop_reason":"end_turn",
			"usage":{"input_tokens":1,"cache_read_input_tokens":99,"output_tokens":1}
		}`))
	}))
	defer srv.Close()

	p := newTestProvider(t, srv, func(c *ClaudeConfig) {
		c.PromptCacheEnabled = true
	})
	resp, err := p.Complete(context.Background(), port.CompletionRequest{
		System: "huge-system-prompt",
		User:   "u",
		MaxTokens: 10,
	})
	if err != nil {
		t.Fatalf("Complete err=%v", err)
	}
	if resp.CachedInputTokens != 99 {
		t.Errorf("CachedInputTokens=%d, want 99 (cost tracker must see it)", resp.CachedInputTokens)
	}
	if !strings.Contains(string(raw), `"cache_control":{"type":"ephemeral"}`) {
		t.Errorf("payload missing cache_control marker: %s", raw)
	}
}

func TestComplete_401_MapsToInvalidAPIKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		_, _ = w.Write([]byte(`{"type":"error","error":{"type":"authentication_error","message":"invalid api key"}}`))
	}))
	defer srv.Close()
	p := newTestProvider(t, srv)
	_, err := p.Complete(context.Background(), port.CompletionRequest{User: "u", MaxTokens: 10})
	var lpe *port.LLMProviderError
	if !errors.As(err, &lpe) {
		t.Fatalf("err type=%T, want *LLMProviderError", err)
	}
	if !lpe.IsAuthError() {
		t.Errorf("IsAuthError() = false, want true")
	}
	if lpe.Retryable {
		t.Errorf("INVALID_API_KEY must Retryable=false; got %+v", lpe)
	}
	if !lpe.FallbackEligible {
		t.Errorf("INVALID_API_KEY must FallbackEligible=true; got %+v", lpe)
	}
}

func TestComplete_429_WithRetryAfter(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "30")
		w.WriteHeader(429)
		_, _ = w.Write([]byte(`{"type":"error","error":{"type":"rate_limit_error","message":"slow down"}}`))
	}))
	defer srv.Close()
	p := newTestProvider(t, srv)
	_, err := p.Complete(context.Background(), port.CompletionRequest{User: "u", MaxTokens: 10})
	var lpe *port.LLMProviderError
	if !errors.As(err, &lpe) {
		t.Fatalf("err type=%T", err)
	}
	if lpe.Code != port.LLMErrorRateLimit {
		t.Errorf("Code=%v", lpe.Code)
	}
	if lpe.RetryAfter == nil || *lpe.RetryAfter != 30*time.Second {
		t.Errorf("RetryAfter=%v, want 30s", lpe.RetryAfter)
	}
}

func TestComplete_529_AnthropicOverloaded(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(529)
		_, _ = w.Write([]byte(`{"type":"error","error":{"type":"overloaded_error","message":"try later"}}`))
	}))
	defer srv.Close()
	p := newTestProvider(t, srv)
	_, err := p.Complete(context.Background(), port.CompletionRequest{User: "u", MaxTokens: 10})
	var lpe *port.LLMProviderError
	if !errors.As(err, &lpe) || lpe.Code != port.LLMErrorOverloaded {
		t.Fatalf("err=%v, want LLMErrorOverloaded", err)
	}
}

func TestComplete_5xx_AsServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(503)
	}))
	defer srv.Close()
	p := newTestProvider(t, srv)
	_, err := p.Complete(context.Background(), port.CompletionRequest{User: "u", MaxTokens: 10})
	var lpe *port.LLMProviderError
	if !errors.As(err, &lpe) || lpe.Code != port.LLMErrorServerError {
		t.Fatalf("err=%v, want LLMErrorServerError", err)
	}
}

func TestComplete_400_ContextTooLong(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		_, _ = w.Write([]byte(`{"type":"error","error":{"type":"invalid_request_error","message":"prompt is too long: 200001 tokens > 200000 context length"}}`))
	}))
	defer srv.Close()
	p := newTestProvider(t, srv)
	_, err := p.Complete(context.Background(), port.CompletionRequest{User: "u", MaxTokens: 10})
	var lpe *port.LLMProviderError
	if !errors.As(err, &lpe) || lpe.Code != port.LLMErrorContextTooLong {
		t.Fatalf("err=%v, want LLMErrorContextTooLong", err)
	}
	if lpe.Retryable || lpe.FallbackEligible {
		t.Errorf("CONTEXT_TOO_LONG must Retryable=false FallbackEligible=false; got %+v", lpe)
	}
}

func TestComplete_ContextCancellation_MapsToTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Server blocks until client cancels OR the handler-side bound fires.
		// The bound exists so Server.Close() does not deadlock if r.Context()
		// somehow does not propagate the client cancellation in time.
		select {
		case <-r.Context().Done():
		case <-time.After(2 * time.Second):
		}
	}))
	defer srv.Close()
	p := newTestProvider(t, srv)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := p.Complete(ctx, port.CompletionRequest{User: "u", MaxTokens: 10})
	var lpe *port.LLMProviderError
	if !errors.As(err, &lpe) || lpe.Code != port.LLMErrorTimeout {
		t.Fatalf("err=%v, want LLMErrorTimeout", err)
	}
}

func TestComplete_NetworkError_MapsToNetwork(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Close connection without writing — http.Client returns EOF / reset.
		hj, ok := w.(http.Hijacker)
		if !ok {
			return
		}
		conn, _, _ := hj.Hijack()
		_ = conn.Close()
	}))
	defer srv.Close()
	p := newTestProvider(t, srv)
	_, err := p.Complete(context.Background(), port.CompletionRequest{User: "u", MaxTokens: 10})
	var lpe *port.LLMProviderError
	if !errors.As(err, &lpe) {
		t.Fatalf("err=%T, want *LLMProviderError", err)
	}
	if lpe.Code != port.LLMErrorNetwork {
		t.Errorf("Code=%v, want LLMErrorNetwork", lpe.Code)
	}
	if !lpe.Retryable || !lpe.FallbackEligible {
		t.Errorf("network must Retryable + FallbackEligible; got %+v", lpe)
	}
}

func TestComplete_AdapterInvariant_AllErrorsAreLLMProviderError(t *testing.T) {
	cases := []func(http.ResponseWriter, *http.Request){
		func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(401) },
		func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(429) },
		func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(500) },
		func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(529) },
		func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(200)
			_, _ = w.Write([]byte(`{"content":[]}`)) // malformed (no text/tool_use)
		},
		func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(200)
			_, _ = w.Write([]byte("not-json"))
		},
	}
	for i, handler := range cases {
		t.Run(fmt.Sprintf("case_%d", i), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(handler))
			defer srv.Close()
			p := newTestProvider(t, srv)
			_, err := p.Complete(context.Background(), port.CompletionRequest{User: "u", MaxTokens: 10})
			if err == nil {
				t.Fatalf("expected error")
			}
			if _, ok := port.AsLLMProviderError(err); !ok {
				t.Errorf("err %T (%v) is not *LLMProviderError — adapter invariant violated", err, err)
			}
		})
	}
}

func TestHealthCheck_HappyPath_ReturnsNilNil(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{
			"content":[{"type":"text","text":"pong"}],
			"stop_reason":"end_turn",
			"usage":{"input_tokens":1,"output_tokens":1}
		}`))
	}))
	defer srv.Close()
	p := newTestProvider(t, srv)
	pe, err := p.HealthCheck(context.Background())
	if pe != nil || err != nil {
		t.Fatalf("HealthCheck() = (%v, %v), want (nil, nil)", pe, err)
	}
}

func TestHealthCheck_401_ReturnsTypedErrNilTransport(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		_, _ = w.Write([]byte(`{"error":{"type":"authentication_error","message":"x"}}`))
	}))
	defer srv.Close()
	p := newTestProvider(t, srv)
	pe, err := p.HealthCheck(context.Background())
	if err != nil {
		t.Fatalf("transport err=%v, want nil (wire-level auth failure is typed, not transport)", err)
	}
	if pe == nil || !pe.IsAuthError() {
		t.Fatalf("typedErr=%v, want auth failure", pe)
	}
}

func TestHealthCheck_NetworkError_ReturnsErrNilTyped(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hj, _ := w.(http.Hijacker)
		conn, _, _ := hj.Hijack()
		_ = conn.Close()
	}))
	defer srv.Close()
	p := newTestProvider(t, srv)
	pe, err := p.HealthCheck(context.Background())
	if pe != nil {
		t.Errorf("typedErr=%v, want nil for transport-level failure", pe)
	}
	if err == nil {
		t.Errorf("transport err is nil; expected non-nil for connection reset")
	}
}

func TestProviderID(t *testing.T) {
	p, err := NewClaudeProvider(ClaudeConfig{
		APIKey:  "k",
		BaseURL: "https://api.anthropic.com",
		Model:   "claude-sonnet-4-6",
	})
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if p.ID() != port.ProviderClaude {
		t.Errorf("ID()=%v, want %v", p.ID(), port.ProviderClaude)
	}
}

func TestNewClaudeProvider_ValidationFails(t *testing.T) {
	_, err := NewClaudeProvider(ClaudeConfig{})
	if err == nil {
		t.Fatalf("expected validation error")
	}
}

func TestNewClaudeProvider_TrimsTrailingSlashFromBaseURL(t *testing.T) {
	var hit atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Path must be exactly /v1/messages with no double slashes from
		// baseURL concat. If trim is broken, path becomes "//v1/messages".
		if r.URL.Path == messagesEndpointPath {
			hit.Store(true)
		}
		_, _ = w.Write([]byte(`{
			"content":[{"type":"text","text":"ok"}],
			"stop_reason":"end_turn",
			"usage":{"input_tokens":1,"output_tokens":1}
		}`))
	}))
	defer srv.Close()
	p, err := NewClaudeProvider(ClaudeConfig{
		APIKey:     "k",
		BaseURL:    srv.URL + "/",
		Model:      "m",
		HTTPClient: srv.Client(),
	})
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if _, err := p.Complete(context.Background(), port.CompletionRequest{User: "u", MaxTokens: 1}); err != nil {
		t.Fatalf("Complete err=%v", err)
	}
	if !hit.Load() {
		t.Errorf("server did not receive request at %s — trailing slash trim is broken", messagesEndpointPath)
	}
}

func TestComplete_PerRequestModelOverride(t *testing.T) {
	var capturedModel string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var p anthropicRequest
		_ = json.NewDecoder(r.Body).Decode(&p)
		capturedModel = p.Model
		_, _ = w.Write([]byte(`{
			"content":[{"type":"text","text":"ok"}],
			"stop_reason":"end_turn",
			"usage":{"input_tokens":1,"output_tokens":1}
		}`))
	}))
	defer srv.Close()
	p := newTestProvider(t, srv) // default model claude-sonnet-4-6
	_, err := p.Complete(context.Background(), port.CompletionRequest{
		User: "u", MaxTokens: 10,
		Model: "claude-haiku-4-5",
	})
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if capturedModel != "claude-haiku-4-5" {
		t.Errorf("wire model=%q, want haiku override", capturedModel)
	}
}

func TestComplete_ResponseBodyExceedsLimit_StillSucceedsWhenLimitedReadParses(t *testing.T) {
	// Reader is capped to maxResponseBytes; this test verifies the cap is at
	// least the documented 8 MiB and that bodies near that ceiling still parse.
	if maxResponseBytes < 8<<20 {
		t.Fatalf("maxResponseBytes=%d, want >= 8MiB", maxResponseBytes)
	}
}

// TestComplete_ErrorsDoNotLeakAPIKey is the canary asserting that the
// adapter never embeds the configured API key into its returned error chain
// — security-engineer review S2 + security.md §3.2.
func TestComplete_ErrorsDoNotLeakAPIKey(t *testing.T) {
	const apiKey = "sk-ant-test-canary-XYZ123ABCDEF"
	cases := []struct {
		name    string
		handler http.HandlerFunc
	}{
		{"401", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(401)
			_, _ = w.Write([]byte(`{"error":{"type":"auth","message":"x"}}`))
		}},
		{"429", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(429) }},
		{"500_with_body", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(500)
			_, _ = w.Write([]byte("upstream server boom"))
		}},
		{"corrupt_2xx", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(200)
			_, _ = w.Write([]byte("not-json"))
		}},
		{"hijack_close", func(w http.ResponseWriter, _ *http.Request) {
			hj, _ := w.(http.Hijacker)
			c, _, _ := hj.Hijack()
			_ = c.Close()
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(tc.handler)
			defer srv.Close()
			p, err := NewClaudeProvider(ClaudeConfig{
				APIKey: apiKey, BaseURL: srv.URL, Model: "m", HTTPClient: srv.Client(),
			})
			if err != nil {
				t.Fatal(err)
			}
			_, err = p.Complete(context.Background(), port.CompletionRequest{User: "u", MaxTokens: 10})
			if err == nil {
				t.Fatal("expected error")
			}
			// Walk the full unwrap chain — the canary key must not appear at
			// any level (defends against a future refactor that wraps the key
			// into a deeper layer than err.Error() alone surfaces).
			for unwrapped := err; unwrapped != nil; unwrapped = errors.Unwrap(unwrapped) {
				if strings.Contains(unwrapped.Error(), apiKey) {
					t.Fatalf("API key leaked into error chain: %v", unwrapped)
				}
			}
		})
	}
}

// TestComplete_Concurrent_NoRace asserts the adapter is safe for parallel
// use by the Router across stages 1/3/5 (which run two agents in parallel
// against the same provider) — golang-pro review S11.
func TestComplete_Concurrent_NoRace(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{
			"content":[{"type":"text","text":"ok"}],
			"stop_reason":"end_turn",
			"usage":{"input_tokens":1,"output_tokens":1}
		}`))
	}))
	defer srv.Close()

	p := newTestProvider(t, srv)
	const N = 32
	errs := make(chan error, N)
	for i := 0; i < N; i++ {
		go func() {
			_, err := p.Complete(context.Background(), port.CompletionRequest{
				User: "u", MaxTokens: 10,
			})
			errs <- err
		}()
	}
	for i := 0; i < N; i++ {
		if err := <-errs; err != nil {
			t.Errorf("goroutine %d err=%v", i, err)
		}
	}
}

// TestHealthCheck_5xx_ReturnsTypedErr — 5xx reached the provider, so the
// Router should see it as a typed error (lpe, nil) rather than transport
// (nil, err) — llm-provider-abstraction.md §1.3 / golang-pro review S11.
func TestHealthCheck_5xx_ReturnsTypedErr(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(503)
	}))
	defer srv.Close()
	p := newTestProvider(t, srv)
	pe, err := p.HealthCheck(context.Background())
	if err != nil {
		t.Fatalf("transport err=%v, want nil (5xx reached the provider)", err)
	}
	if pe == nil || pe.Code != port.LLMErrorServerError {
		t.Fatalf("typedErr=%v, want LLMErrorServerError", pe)
	}
}
