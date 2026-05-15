package openai

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
func newTestProvider(t *testing.T, srv *httptest.Server, cfgOverride ...func(*OpenAIConfig)) *Provider {
	t.Helper()
	cfg := OpenAIConfig{
		APIKey:     "sk-test-key",
		BaseURL:    srv.URL,
		Model:      "gpt-4.1",
		HTTPClient: srv.Client(),
	}
	for _, f := range cfgOverride {
		f(&cfg)
	}
	p, err := NewOpenAIProvider(cfg)
	if err != nil {
		t.Fatalf("NewOpenAIProvider err=%v", err)
	}
	return p
}

const okResponseBody = `{
	"id":"resp_1","object":"response","model":"gpt-4.1","status":"completed",
	"output":[{"type":"message","role":"assistant","status":"completed",
		"content":[{"type":"output_text","text":"ok"}]}],
	"usage":{"input_tokens":1,"output_tokens":1}
}`

func TestComplete_HappyPath_TextOnly(t *testing.T) {
	var capturedReq responsesRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != responsesEndpointPath {
			t.Errorf("path=%q, want %q", r.URL.Path, responsesEndpointPath)
		}
		if r.Method != http.MethodPost {
			t.Errorf("method=%q, want POST", r.Method)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer sk-test-key" {
			t.Errorf("Authorization=%q, want Bearer sk-test-key", got)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("Content-Type=%q", got)
		}
		_ = json.NewDecoder(r.Body).Decode(&capturedReq)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id":"resp_test","object":"response","model":"gpt-4.1","status":"completed",
			"output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"hello"}]}],
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
	if resp.CachedInputTokens != 0 {
		t.Errorf("CachedInputTokens=%d, want 0", resp.CachedInputTokens)
	}
	if resp.ProviderID != port.ProviderOpenAI {
		t.Errorf("ProviderID=%v", resp.ProviderID)
	}
	if resp.LatencyMs < 0 {
		t.Errorf("LatencyMs=%d, want >= 0", resp.LatencyMs)
	}
	// Acceptance test step 2: System → leading developer message in input.
	if len(capturedReq.Input) != 2 {
		t.Fatalf("Input len=%d, want 2", len(capturedReq.Input))
	}
	if capturedReq.Input[0].Role != "developer" || capturedReq.Input[0].Content != "you are a summariser" {
		t.Errorf("Input[0]=%+v, want developer/system", capturedReq.Input[0])
	}
	if capturedReq.Input[1].Role != "user" || capturedReq.Input[1].Content != "summarise: X" {
		t.Errorf("Input[1]=%+v, want user", capturedReq.Input[1])
	}
}

func TestComplete_WithJSONSchema_SendsStrictTextFormat(t *testing.T) {
	var rawReq json.RawMessage
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rawReq, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"status":"completed",
			"output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"{\"contract_type\":\"SERVICES\"}"}]}],
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
	if resp.Content != `{"contract_type":"SERVICES"}` {
		t.Errorf("Content=%q", resp.Content)
	}
	// Acceptance test step 3: strict json_schema passed (Responses-API
	// flattened text.format, NOT Chat Completions response_format).
	s := string(rawReq)
	if !strings.Contains(s, `"text":{"format":{"type":"json_schema"`) {
		t.Errorf("missing flattened text.format json_schema: %s", s)
	}
	if !strings.Contains(s, `"strict":true`) {
		t.Errorf("strict:true not sent: %s", s)
	}
	if strings.Contains(s, "response_format") {
		t.Errorf("used Chat-Completions response_format against /v1/responses: %s", s)
	}
}

func TestComplete_401_MapsToInvalidAPIKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		_, _ = w.Write([]byte(`{"error":{"type":"invalid_request_error","code":"invalid_api_key","message":"Incorrect API key"}}`))
	}))
	defer srv.Close()
	p := newTestProvider(t, srv)
	_, err := p.Complete(context.Background(), port.CompletionRequest{User: "u", MaxTokens: 16})
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
		_, _ = w.Write([]byte(`{"error":{"type":"requests","code":"rate_limit_exceeded","message":"slow down"}}`))
	}))
	defer srv.Close()
	p := newTestProvider(t, srv)
	_, err := p.Complete(context.Background(), port.CompletionRequest{User: "u", MaxTokens: 16})
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

func TestComplete_429_InsufficientQuota_MapsToQuotaExceeded(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(429)
		_, _ = w.Write([]byte(`{"error":{"type":"insufficient_quota","code":"insufficient_quota","message":"You exceeded your current quota"}}`))
	}))
	defer srv.Close()
	p := newTestProvider(t, srv)
	_, err := p.Complete(context.Background(), port.CompletionRequest{User: "u", MaxTokens: 16})
	var lpe *port.LLMProviderError
	if !errors.As(err, &lpe) || lpe.Code != port.LLMErrorQuotaExceeded {
		t.Fatalf("err=%v, want LLMErrorQuotaExceeded", err)
	}
	if lpe.Retryable || !lpe.FallbackEligible {
		t.Errorf("QUOTA_EXCEEDED must Retryable=false FallbackEligible=true; got %+v", lpe)
	}
}

func TestComplete_5xx_AsServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(503)
	}))
	defer srv.Close()
	p := newTestProvider(t, srv)
	_, err := p.Complete(context.Background(), port.CompletionRequest{User: "u", MaxTokens: 16})
	var lpe *port.LLMProviderError
	if !errors.As(err, &lpe) || lpe.Code != port.LLMErrorServerError {
		t.Fatalf("err=%v, want LLMErrorServerError", err)
	}
}

func TestComplete_400_ContextTooLong(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		_, _ = w.Write([]byte(`{"error":{"type":"invalid_request_error","code":"context_length_exceeded","message":"This model's maximum context length is 128000 tokens, however you requested 200001"}}`))
	}))
	defer srv.Close()
	p := newTestProvider(t, srv)
	_, err := p.Complete(context.Background(), port.CompletionRequest{User: "u", MaxTokens: 16})
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
		select {
		case <-r.Context().Done():
		case <-time.After(2 * time.Second):
		}
	}))
	defer srv.Close()
	p := newTestProvider(t, srv)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := p.Complete(ctx, port.CompletionRequest{User: "u", MaxTokens: 16})
	var lpe *port.LLMProviderError
	if !errors.As(err, &lpe) || lpe.Code != port.LLMErrorTimeout {
		t.Fatalf("err=%v, want LLMErrorTimeout", err)
	}
}

func TestComplete_NetworkError_MapsToNetwork(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hj, ok := w.(http.Hijacker)
		if !ok {
			return
		}
		conn, _, _ := hj.Hijack()
		_ = conn.Close()
	}))
	defer srv.Close()
	p := newTestProvider(t, srv)
	_, err := p.Complete(context.Background(), port.CompletionRequest{User: "u", MaxTokens: 16})
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
		func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(418) },
		func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(200)
			_, _ = w.Write([]byte(`{"status":"completed","output":[]}`)) // empty output
		},
		func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(200)
			_, _ = w.Write([]byte("not-json"))
		},
		func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(200)
			_, _ = w.Write([]byte(`{"status":"failed","error":{"code":"x","message":"y"}}`))
		},
	}
	for i, handler := range cases {
		t.Run(fmt.Sprintf("case_%d", i), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(handler))
			defer srv.Close()
			p := newTestProvider(t, srv)
			_, err := p.Complete(context.Background(), port.CompletionRequest{User: "u", MaxTokens: 16})
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
	var capturedReq responsesRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&capturedReq)
		_, _ = w.Write([]byte(okResponseBody))
	}))
	defer srv.Close()
	p := newTestProvider(t, srv)
	pe, err := p.HealthCheck(context.Background())
	if pe != nil || err != nil {
		t.Fatalf("HealthCheck() = (%v, %v), want (nil, nil)", pe, err)
	}
	// Probe must respect the Responses-API max_output_tokens floor (16).
	if capturedReq.MaxOutputTokens != healthCheckMaxTokens {
		t.Errorf("probe MaxOutputTokens=%d, want %d (Responses API floor)", capturedReq.MaxOutputTokens, healthCheckMaxTokens)
	}
	if healthCheckMaxTokens < 16 {
		t.Errorf("healthCheckMaxTokens=%d, want >= 16 (Responses API rejects below 16)", healthCheckMaxTokens)
	}
}

func TestHealthCheck_401_ReturnsTypedErrNilTransport(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		_, _ = w.Write([]byte(`{"error":{"type":"invalid_request_error","code":"invalid_api_key","message":"x"}}`))
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

func TestProviderID(t *testing.T) {
	p, err := NewOpenAIProvider(OpenAIConfig{
		APIKey:  "k",
		BaseURL: "https://api.openai.com",
		Model:   "gpt-4.1",
	})
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if p.ID() != port.ProviderOpenAI {
		t.Errorf("ID()=%v, want %v", p.ID(), port.ProviderOpenAI)
	}
}

func TestNewOpenAIProvider_ValidationFails(t *testing.T) {
	_, err := NewOpenAIProvider(OpenAIConfig{})
	if err == nil {
		t.Fatalf("expected validation error")
	}
}

func TestNewOpenAIProvider_TrimsTrailingSlashFromBaseURL(t *testing.T) {
	var hit atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == responsesEndpointPath {
			hit.Store(true)
		}
		_, _ = w.Write([]byte(okResponseBody))
	}))
	defer srv.Close()
	p, err := NewOpenAIProvider(OpenAIConfig{
		APIKey:     "k",
		BaseURL:    srv.URL + "/",
		Model:      "m",
		HTTPClient: srv.Client(),
	})
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if _, err := p.Complete(context.Background(), port.CompletionRequest{User: "u", MaxTokens: 16}); err != nil {
		t.Fatalf("Complete err=%v", err)
	}
	if !hit.Load() {
		t.Errorf("server did not receive request at %s — trailing slash trim is broken", responsesEndpointPath)
	}
}

func TestComplete_PerRequestModelOverride(t *testing.T) {
	var capturedModel string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var p responsesRequest
		_ = json.NewDecoder(r.Body).Decode(&p)
		capturedModel = p.Model
		_, _ = w.Write([]byte(okResponseBody))
	}))
	defer srv.Close()
	p := newTestProvider(t, srv) // default model gpt-4.1
	_, err := p.Complete(context.Background(), port.CompletionRequest{
		User: "u", MaxTokens: 16,
		Model: "gpt-4.1-mini",
	})
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if capturedModel != "gpt-4.1-mini" {
		t.Errorf("wire model=%q, want gpt-4.1-mini override", capturedModel)
	}
}

func TestComplete_ResponseBodyCap_AtLeast8MiB(t *testing.T) {
	if maxResponseBytes < 8<<20 {
		t.Fatalf("maxResponseBytes=%d, want >= 8MiB", maxResponseBytes)
	}
}

// TestComplete_ErrorsDoNotLeakAPIKey is the canary asserting the adapter
// never embeds the configured API key into its returned error chain — the
// secret rides in Authorization: Bearer, so a body/header echo would leak it
// (security.md §3.2).
func TestComplete_ErrorsDoNotLeakAPIKey(t *testing.T) {
	const apiKey = "sk-test-canary-XYZ123ABCDEF"
	cases := []struct {
		name    string
		handler http.HandlerFunc
	}{
		{"401", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(401)
			_, _ = w.Write([]byte(`{"error":{"type":"invalid_request_error","message":"x"}}`))
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
		{"status_failed_inline_message", func(w http.ResponseWriter, _ *http.Request) {
			// Drives the parseResponse status:"failed" path (security-engineer
			// S2) — the path that copies a provider-controlled message into
			// the error chain. The key must never appear there.
			w.WriteHeader(200)
			_, _ = w.Write([]byte(`{"status":"failed","error":{"code":"server_error","message":"the model produced an internal error"}}`))
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
			p, err := NewOpenAIProvider(OpenAIConfig{
				APIKey: apiKey, BaseURL: srv.URL, Model: "m", HTTPClient: srv.Client(),
			})
			if err != nil {
				t.Fatal(err)
			}
			_, err = p.Complete(context.Background(), port.CompletionRequest{User: "u", MaxTokens: 16})
			if err == nil {
				t.Fatal("expected error")
			}
			for unwrapped := err; unwrapped != nil; unwrapped = errors.Unwrap(unwrapped) {
				if strings.Contains(unwrapped.Error(), apiKey) {
					t.Fatalf("API key leaked into error chain: %v", unwrapped)
				}
			}
		})
	}
}

// TestComplete_InvalidJSONSchema_MarshalFails_Malformed exercises a REACHABLE
// adapter-invariant path the Claude sibling also leaves untested (golang-pro
// S3.3): an invalid json.RawMessage schema flows into the payload and makes
// json.Marshal(payload) fail. The Router could be handed such a schema, so
// this must surface as a typed MALFORMED error, not a bare json error.
func TestComplete_InvalidJSONSchema_MarshalFails_Malformed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("server must not be reached — marshal fails before any wire I/O")
		_, _ = w.Write([]byte(okResponseBody))
	}))
	defer srv.Close()
	p := newTestProvider(t, srv)
	_, err := p.Complete(context.Background(), port.CompletionRequest{
		User:       "u",
		MaxTokens:  16,
		JSONSchema: json.RawMessage("{not valid json"),
	})
	lpe, ok := port.AsLLMProviderError(err)
	if !ok {
		t.Fatalf("err=%T, want *LLMProviderError (adapter invariant)", err)
	}
	if lpe.Code != port.LLMErrorMalformedRequest {
		t.Errorf("Code=%v, want LLMErrorMalformedRequest", lpe.Code)
	}
}

// TestComplete_ForcedURLError_NoKeyLeak closes security-engineer S2: a
// transport failure that produces a *url.Error (which embeds the request URL)
// must never surface the configured API key. Userinfo is rejected at config
// time so the key is not in the URL — this guards against a future regression
// that puts a secret into the URL (e.g. a Gemini-style ?key= pattern).
func TestComplete_ForcedURLError_NoKeyLeak(t *testing.T) {
	const apiKey = "sk-test-canary-URLERR-9988776655"
	p, err := NewOpenAIProvider(OpenAIConfig{
		APIKey:  apiKey,
		BaseURL: "http://127.0.0.1:1", // port 1 → connection refused → *url.Error
		Model:   "m",
	})
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, callErr := p.Complete(ctx, port.CompletionRequest{User: "u", MaxTokens: 16})
	if callErr == nil {
		t.Fatalf("expected a transport error")
	}
	for u := callErr; u != nil; u = errors.Unwrap(u) {
		if strings.Contains(u.Error(), apiKey) {
			t.Fatalf("API key leaked into *url.Error chain: %v", u)
		}
	}
}

// TestComplete_Concurrent_NoRace asserts the adapter is safe for parallel use
// by the Router across stages 1/3/5 (two agents in parallel against the same
// provider).
func TestComplete_Concurrent_NoRace(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(okResponseBody))
	}))
	defer srv.Close()

	p := newTestProvider(t, srv)
	const N = 32
	errs := make(chan error, N)
	for i := 0; i < N; i++ {
		go func() {
			_, err := p.Complete(context.Background(), port.CompletionRequest{
				User: "u", MaxTokens: 16,
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
