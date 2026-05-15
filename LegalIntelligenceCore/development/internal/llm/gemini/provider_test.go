package gemini

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"contractpro/legal-intelligence-core/internal/domain/model"
	"contractpro/legal-intelligence-core/internal/domain/port"
)

func newTestProvider(t *testing.T, srv *httptest.Server, override ...func(*GeminiConfig)) *Provider {
	t.Helper()
	cfg := GeminiConfig{
		APIKey:     "AIza-test-key",
		BaseURL:    srv.URL,
		Model:      "gemini-2.5-pro",
		HTTPClient: srv.Client(),
	}
	for _, f := range override {
		f(&cfg)
	}
	p, err := NewGeminiProvider(cfg)
	if err != nil {
		t.Fatalf("NewGeminiProvider err=%v", err)
	}
	return p
}

const okBody = `{
	"candidates":[{"content":{"parts":[{"text":"ok"}],"role":"model"},"finishReason":"STOP","index":0}],
	"usageMetadata":{"promptTokenCount":1,"candidatesTokenCount":1,"totalTokenCount":2},
	"modelVersion":"gemini-2.5-pro"
}`

func TestComplete_HappyPath_TextOnly(t *testing.T) {
	var capturedReq generateContentRequest
	var capturedPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		if r.Method != http.MethodPost {
			t.Errorf("method=%q, want POST", r.Method)
		}
		if got := r.Header.Get(apiKeyHeader); got != "AIza-test-key" {
			t.Errorf("%s=%q, want AIza-test-key", apiKeyHeader, got)
		}
		// Key must NEVER appear in the URL (header-auth invariant).
		if strings.Contains(r.URL.RawQuery, "AIza-test-key") || strings.Contains(r.URL.Path, "AIza-test-key") {
			t.Errorf("API key leaked into URL: %s?%s", r.URL.Path, r.URL.RawQuery)
		}
		_ = json.NewDecoder(r.Body).Decode(&capturedReq)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"candidates":[{"content":{"parts":[{"text":"hello"}],"role":"model"},"finishReason":"STOP"}],
			"usageMetadata":{"promptTokenCount":50,"candidatesTokenCount":7,"totalTokenCount":57},
			"modelVersion":"gemini-2.5-pro"
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
	if resp.InputTokens != 50 || resp.OutputTokens != 7 || resp.CachedInputTokens != 0 {
		t.Errorf("tokens i=%d o=%d c=%d", resp.InputTokens, resp.OutputTokens, resp.CachedInputTokens)
	}
	if resp.ProviderID != port.ProviderGemini {
		t.Errorf("ProviderID=%v", resp.ProviderID)
	}
	if resp.Model != "gemini-2.5-pro" {
		t.Errorf("Model=%q", resp.Model)
	}
	// Path: /v1beta/models/gemini-2.5-pro:generateContent — colon NOT escaped.
	wantPath := "/v1beta/models/gemini-2.5-pro:generateContent"
	if capturedPath != wantPath {
		t.Errorf("path=%q, want %q (colon must stay literal)", capturedPath, wantPath)
	}
	// System → systemInstruction (no role); User → contents[user].
	if capturedReq.SystemInstruction == nil ||
		len(capturedReq.SystemInstruction.Parts) != 1 ||
		capturedReq.SystemInstruction.Parts[0].Text != "you are a summariser" {
		t.Errorf("systemInstruction=%+v", capturedReq.SystemInstruction)
	}
	if capturedReq.SystemInstruction.Role != "" {
		t.Errorf("systemInstruction.role=%q, want empty", capturedReq.SystemInstruction.Role)
	}
	if len(capturedReq.Contents) != 1 ||
		capturedReq.Contents[0].Role != "user" ||
		capturedReq.Contents[0].Parts[0].Text != "summarise: X" {
		t.Errorf("contents=%+v", capturedReq.Contents)
	}
}

// Acceptance step 2: a PriorTurn with Role=Assistant maps to "model".
func TestComplete_PriorTurns_AssistantMapsToModel(t *testing.T) {
	var capturedReq generateContentRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&capturedReq)
		_, _ = w.Write([]byte(okBody))
	}))
	defer srv.Close()
	p := newTestProvider(t, srv)
	_, err := p.Complete(context.Background(), port.CompletionRequest{
		User:      "fix it",
		MaxTokens: 100,
		PriorTurns: []port.Turn{
			{Role: port.RoleUser, Content: "first user"},
			{Role: port.RoleAssistant, Content: "bad json"},
		},
	})
	if err != nil {
		t.Fatalf("Complete err=%v", err)
	}
	if len(capturedReq.Contents) != 3 {
		t.Fatalf("contents len=%d, want 3", len(capturedReq.Contents))
	}
	if capturedReq.Contents[0].Role != "user" {
		t.Errorf("Contents[0].Role=%q, want user", capturedReq.Contents[0].Role)
	}
	if capturedReq.Contents[1].Role != "model" {
		t.Errorf("Contents[1].Role=%q, want model (Assistant→model)", capturedReq.Contents[1].Role)
	}
	if capturedReq.Contents[2].Role != "user" || capturedReq.Contents[2].Parts[0].Text != "fix it" {
		t.Errorf("Contents[2]=%+v, want trailing user turn", capturedReq.Contents[2])
	}
}

// Acceptance step 3: responseSchema is passed (and the draft-07 schema is
// transformed into Gemini's UPPERCASE-type dialect).
func TestComplete_WithJSONSchema_SendsResponseSchema(t *testing.T) {
	var raw json.RawMessage
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ = io.ReadAll(r.Body)
		_, _ = w.Write([]byte(`{
			"candidates":[{"content":{"parts":[{"text":"{\"contract_type\":\"SERVICES\"}"}],"role":"model"},"finishReason":"STOP"}],
			"usageMetadata":{"promptTokenCount":5,"candidatesTokenCount":12}
		}`))
	}))
	defer srv.Close()

	schema := json.RawMessage(`{"$schema":"http://json-schema.org/draft-07/schema#","type":"object","additionalProperties":false,"properties":{"contract_type":{"type":"string"}},"required":["contract_type"]}`)
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

	var sent generateContentRequest
	if err := json.Unmarshal(raw, &sent); err != nil {
		t.Fatalf("decode sent request: %v", err)
	}
	if sent.GenerationConfig.ResponseMimeType != "application/json" {
		t.Errorf("responseMimeType=%q, want application/json", sent.GenerationConfig.ResponseMimeType)
	}
	if len(sent.GenerationConfig.ResponseSchema) == 0 {
		t.Fatalf("responseSchema not sent")
	}
	var schemaOut map[string]any
	if err := json.Unmarshal(sent.GenerationConfig.ResponseSchema, &schemaOut); err != nil {
		t.Fatalf("responseSchema not valid JSON: %v", err)
	}
	if schemaOut["type"] != "OBJECT" {
		t.Errorf("transformed type=%v, want OBJECT (uppercased)", schemaOut["type"])
	}
	if _, leaked := schemaOut["additionalProperties"]; leaked {
		t.Errorf("additionalProperties leaked into Gemini schema: %v", schemaOut)
	}
	if _, leaked := schemaOut["$schema"]; leaked {
		t.Errorf("$schema leaked into Gemini schema: %v", schemaOut)
	}
}

func TestComplete_JSONModeOnly_SetsMimeTypeNoSchema(t *testing.T) {
	var sent generateContentRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&sent)
		_, _ = w.Write([]byte(okBody))
	}))
	defer srv.Close()
	p := newTestProvider(t, srv)
	if _, err := p.Complete(context.Background(), port.CompletionRequest{
		User: "u", MaxTokens: 16, JSONMode: true,
	}); err != nil {
		t.Fatalf("Complete err=%v", err)
	}
	if sent.GenerationConfig.ResponseMimeType != "application/json" {
		t.Errorf("responseMimeType=%q, want application/json", sent.GenerationConfig.ResponseMimeType)
	}
	if len(sent.GenerationConfig.ResponseSchema) != 0 {
		t.Errorf("responseSchema must be absent for JSONMode-only, got %s", sent.GenerationConfig.ResponseSchema)
	}
}

func TestComplete_401_MapsToInvalidAPIKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		_, _ = w.Write([]byte(`{"error":{"code":401,"message":"API key not valid","status":"UNAUTHENTICATED"}}`))
	}))
	defer srv.Close()
	p := newTestProvider(t, srv)
	_, err := p.Complete(context.Background(), port.CompletionRequest{User: "u", MaxTokens: 16})
	var lpe *port.LLMProviderError
	if !errors.As(err, &lpe) || !lpe.IsAuthError() {
		t.Fatalf("err=%v, want auth error", err)
	}
	if lpe.Retryable || !lpe.FallbackEligible {
		t.Errorf("INVALID_API_KEY must Retryable=false FallbackEligible=true; got %+v", lpe)
	}
}

func TestComplete_403_PermissionDenied_MapsToInvalidAPIKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(403)
		_, _ = w.Write([]byte(`{"error":{"code":403,"message":"Permission denied","status":"PERMISSION_DENIED"}}`))
	}))
	defer srv.Close()
	p := newTestProvider(t, srv)
	_, err := p.Complete(context.Background(), port.CompletionRequest{User: "u", MaxTokens: 16})
	var lpe *port.LLMProviderError
	if !errors.As(err, &lpe) || lpe.Code != port.LLMErrorInvalidAPIKey {
		t.Fatalf("err=%v, want LLMErrorInvalidAPIKey", err)
	}
}

func TestComplete_429_WithRetryAfter(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "30")
		w.WriteHeader(429)
		_, _ = w.Write([]byte(`{"error":{"code":429,"message":"Rate limit exceeded","status":"RESOURCE_EXHAUSTED"}}`))
	}))
	defer srv.Close()
	p := newTestProvider(t, srv)
	_, err := p.Complete(context.Background(), port.CompletionRequest{User: "u", MaxTokens: 16})
	var lpe *port.LLMProviderError
	if !errors.As(err, &lpe) || lpe.Code != port.LLMErrorRateLimit {
		t.Fatalf("err=%v, want LLMErrorRateLimit", err)
	}
	if lpe.RetryAfter == nil || *lpe.RetryAfter != 30*time.Second {
		t.Errorf("RetryAfter=%v, want 30s", lpe.RetryAfter)
	}
}

func TestComplete_429_QuotaExceeded(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(429)
		_, _ = w.Write([]byte(`{"error":{"code":429,"message":"You exceeded your current quota, please check your plan and billing","status":"RESOURCE_EXHAUSTED"}}`))
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
		_, _ = w.Write([]byte(`{"error":{"code":400,"message":"The input token count (2000001) exceeds the maximum number of tokens allowed","status":"INVALID_ARGUMENT"}}`))
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

func TestComplete_400_Malformed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		_, _ = w.Write([]byte(`{"error":{"code":400,"message":"Invalid JSON payload received","status":"INVALID_ARGUMENT"}}`))
	}))
	defer srv.Close()
	p := newTestProvider(t, srv)
	_, err := p.Complete(context.Background(), port.CompletionRequest{User: "u", MaxTokens: 16})
	var lpe *port.LLMProviderError
	if !errors.As(err, &lpe) || lpe.Code != port.LLMErrorMalformedRequest {
		t.Fatalf("err=%v, want LLMErrorMalformedRequest", err)
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
	if !errors.As(err, &lpe) || lpe.Code != port.LLMErrorNetwork {
		t.Fatalf("err=%v, want LLMErrorNetwork", err)
	}
	if !lpe.Retryable || !lpe.FallbackEligible {
		t.Errorf("network must Retryable + FallbackEligible; got %+v", lpe)
	}
}

// MUST-FIX #3: a blocked PROMPT (promptFeedback.blockReason) → successful
// content-filter response (Router maps StopReasonContentFilter→ContentPolicy).
func TestComplete_PromptBlocked_ContentFilterSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"promptFeedback":{"blockReason":"SAFETY"},"usageMetadata":{"promptTokenCount":9}}`))
	}))
	defer srv.Close()
	p := newTestProvider(t, srv)
	resp, err := p.Complete(context.Background(), port.CompletionRequest{User: "u", MaxTokens: 16})
	if err != nil {
		t.Fatalf("Complete err=%v, want success (content-filter is a successful response)", err)
	}
	if resp.StopReason != port.StopReasonContentFilter {
		t.Errorf("StopReason=%v, want StopReasonContentFilter", resp.StopReason)
	}
	if !strings.Contains(resp.Content, "SAFETY") {
		t.Errorf("Content=%q, want it to surface the block reason", resp.Content)
	}
	if resp.InputTokens != 9 {
		t.Errorf("InputTokens=%d, want 9 (tokens still accounted)", resp.InputTokens)
	}
}

// MUST-FIX #4: a candidate finishReason in the safety family → successful
// content-filter response.
func TestComplete_CandidateSafetyFinish_ContentFilterSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"candidates":[{"content":{"parts":[],"role":"model"},"finishReason":"PROHIBITED_CONTENT"}],"usageMetadata":{"promptTokenCount":3}}`))
	}))
	defer srv.Close()
	p := newTestProvider(t, srv)
	resp, err := p.Complete(context.Background(), port.CompletionRequest{User: "u", MaxTokens: 16})
	if err != nil {
		t.Fatalf("Complete err=%v, want success", err)
	}
	if resp.StopReason != port.StopReasonContentFilter {
		t.Errorf("StopReason=%v, want StopReasonContentFilter", resp.StopReason)
	}
}

func TestComplete_EmptyCandidatesNoBlock_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"candidates":[],"usageMetadata":{"promptTokenCount":1}}`))
	}))
	defer srv.Close()
	p := newTestProvider(t, srv)
	_, err := p.Complete(context.Background(), port.CompletionRequest{User: "u", MaxTokens: 16})
	var lpe *port.LLMProviderError
	if !errors.As(err, &lpe) || lpe.Code != port.LLMErrorServerError {
		t.Fatalf("err=%v, want LLMErrorServerError", err)
	}
}

func TestComplete_InvalidJSONSchema_Malformed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("server must not be reached — schema transform fails before any wire I/O")
		_, _ = w.Write([]byte(okBody))
	}))
	defer srv.Close()
	p := newTestProvider(t, srv)
	_, err := p.Complete(context.Background(), port.CompletionRequest{
		User: "u", MaxTokens: 16,
		JSONSchema: json.RawMessage("{not valid json"),
	})
	var lpe *port.LLMProviderError
	if !errors.As(err, &lpe) || lpe.Code != port.LLMErrorMalformedRequest {
		t.Fatalf("err=%v, want LLMErrorMalformedRequest", err)
	}
}

func TestComplete_AdapterInvariant_AllErrorsAreLLMProviderError(t *testing.T) {
	cases := []func(http.ResponseWriter, *http.Request){
		func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(401) },
		func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(403) },
		func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(429) },
		func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(500) },
		func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(418) },
		func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(400) },
		func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(200)
			_, _ = w.Write([]byte(`{"candidates":[]}`))
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

func TestHealthCheck_HappyPath_ReturnsNilNil_NoSystemInstruction(t *testing.T) {
	var captured generateContentRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&captured)
		_, _ = w.Write([]byte(okBody))
	}))
	defer srv.Close()
	p := newTestProvider(t, srv)
	pe, err := p.HealthCheck(context.Background())
	if pe != nil || err != nil {
		t.Fatalf("HealthCheck() = (%v, %v), want (nil, nil)", pe, err)
	}
	// MUST-FIX #5: no System ⇒ systemInstruction omitted entirely.
	if captured.SystemInstruction != nil {
		t.Errorf("HealthCheck sent systemInstruction=%+v, want nil (empty System must be omitted)", captured.SystemInstruction)
	}
	if captured.GenerationConfig.MaxOutputTokens != healthCheckMaxTokens {
		t.Errorf("probe maxOutputTokens=%d, want %d", captured.GenerationConfig.MaxOutputTokens, healthCheckMaxTokens)
	}
}

func TestHealthCheck_401_ReturnsTypedErrNilTransport(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		_, _ = w.Write([]byte(`{"error":{"code":401,"message":"x","status":"UNAUTHENTICATED"}}`))
	}))
	defer srv.Close()
	p := newTestProvider(t, srv)
	pe, err := p.HealthCheck(context.Background())
	if err != nil {
		t.Fatalf("transport err=%v, want nil (wire-level auth failure is typed)", err)
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
	p, err := NewGeminiProvider(GeminiConfig{
		APIKey:  "k",
		BaseURL: "https://generativelanguage.googleapis.com",
		Model:   "gemini-2.5-pro",
	})
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if p.ID() != port.ProviderGemini {
		t.Errorf("ID()=%v, want %v", p.ID(), port.ProviderGemini)
	}
}

func TestNewGeminiProvider_ValidationFails(t *testing.T) {
	if _, err := NewGeminiProvider(GeminiConfig{}); err == nil {
		t.Fatalf("expected validation error")
	}
}

func TestNewGeminiProvider_TrimsTrailingSlash(t *testing.T) {
	var hitPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hitPath = r.URL.Path
		_, _ = w.Write([]byte(okBody))
	}))
	defer srv.Close()
	p, err := NewGeminiProvider(GeminiConfig{
		APIKey: "k", BaseURL: srv.URL + "/", Model: "gemini-2.5-pro", HTTPClient: srv.Client(),
	})
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if _, err := p.Complete(context.Background(), port.CompletionRequest{User: "u", MaxTokens: 16}); err != nil {
		t.Fatalf("Complete err=%v", err)
	}
	if hitPath != "/v1beta/models/gemini-2.5-pro:generateContent" {
		t.Errorf("path=%q — trailing-slash trim broken", hitPath)
	}
}

func TestComplete_PerRequestModelOverride_InPath(t *testing.T) {
	var path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		_, _ = w.Write([]byte(okBody))
	}))
	defer srv.Close()
	p := newTestProvider(t, srv) // default gemini-2.5-pro
	_, err := p.Complete(context.Background(), port.CompletionRequest{
		User: "u", MaxTokens: 16, Model: "gemini-2.0-flash-001",
	})
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if path != "/v1beta/models/gemini-2.0-flash-001:generateContent" {
		t.Errorf("path=%q, want override model in path", path)
	}
}

func TestComplete_PerRequestModelOverride_PathUnsafe_Malformed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("server must not be reached — junk model rejected before wire I/O")
	}))
	defer srv.Close()
	p := newTestProvider(t, srv)
	_, err := p.Complete(context.Background(), port.CompletionRequest{
		User: "u", MaxTokens: 16, Model: "../../secret:generateContent",
	})
	var lpe *port.LLMProviderError
	if !errors.As(err, &lpe) || lpe.Code != port.LLMErrorMalformedRequest {
		t.Fatalf("err=%v, want LLMErrorMalformedRequest for path-unsafe model override", err)
	}
}

// L1: a padded-but-valid per-request model override resolves (trimmed) rather
// than failing path-safety validation on surrounding whitespace.
func TestChooseModel_TrimsOverride(t *testing.T) {
	if got := chooseModel("  gemini-2.0-flash  ", "gemini-2.5-pro"); got != "gemini-2.0-flash" {
		t.Errorf("chooseModel padded override = %q, want trimmed", got)
	}
	if got := chooseModel("   ", "gemini-2.5-pro"); got != "gemini-2.5-pro" {
		t.Errorf("chooseModel whitespace-only = %q, want default", got)
	}
	if got := chooseModel("", "gemini-2.5-pro"); got != "gemini-2.5-pro" {
		t.Errorf("chooseModel empty = %q, want default", got)
	}
}

func TestComplete_PerRequestModelOverride_PaddedResolves(t *testing.T) {
	var path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		_, _ = w.Write([]byte(okBody))
	}))
	defer srv.Close()
	p := newTestProvider(t, srv)
	if _, err := p.Complete(context.Background(), port.CompletionRequest{
		User: "u", MaxTokens: 16, Model: "  gemini-2.0-flash-001  ",
	}); err != nil {
		t.Fatalf("padded override should resolve, err=%v", err)
	}
	if path != "/v1beta/models/gemini-2.0-flash-001:generateContent" {
		t.Errorf("path=%q, want trimmed override model", path)
	}
}

func TestComplete_ResponseBodyCap_AtLeast8MiB(t *testing.T) {
	if maxResponseBytes < 8<<20 {
		t.Fatalf("maxResponseBytes=%d, want >= 8MiB", maxResponseBytes)
	}
}

// Canary: the configured API key must never surface in the returned error
// chain. The key rides in x-goog-api-key (a header), never the URL — so even a
// *url.Error (connection refused) cannot embed it. This is the regression the
// OpenAI sibling's comment names ("a future Gemini-style ?key= pattern").
func TestComplete_ErrorsDoNotLeakAPIKey(t *testing.T) {
	const apiKey = "AIza-canary-XYZ123ABCDEF"
	cases := []struct {
		name    string
		handler http.HandlerFunc
		baseURL string
	}{
		{"401", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(401)
			_, _ = w.Write([]byte(`{"error":{"code":401,"message":"x","status":"UNAUTHENTICATED"}}`))
		}, ""},
		{"500_with_body", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(500)
			_, _ = w.Write([]byte("upstream boom"))
		}, ""},
		{"corrupt_2xx", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(200)
			_, _ = w.Write([]byte("not-json"))
		}, ""},
		{"hijack_close", func(w http.ResponseWriter, _ *http.Request) {
			hj, _ := w.(http.Hijacker)
			c, _, _ := hj.Hijack()
			_ = c.Close()
		}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(tc.handler)
			defer srv.Close()
			p, err := NewGeminiProvider(GeminiConfig{
				APIKey: apiKey, BaseURL: srv.URL, Model: "gemini-2.5-pro", HTTPClient: srv.Client(),
			})
			if err != nil {
				t.Fatal(err)
			}
			_, callErr := p.Complete(context.Background(), port.CompletionRequest{User: "u", MaxTokens: 16})
			if callErr == nil {
				t.Fatal("expected error")
			}
			for u := callErr; u != nil; u = errors.Unwrap(u) {
				if strings.Contains(u.Error(), apiKey) {
					t.Fatalf("API key leaked into error chain: %v", u)
				}
			}
		})
	}
}

func TestComplete_ForcedURLError_NoKeyLeak(t *testing.T) {
	const apiKey = "AIza-canary-URLERR-9988776655"
	p, err := NewGeminiProvider(GeminiConfig{
		APIKey:  apiKey,
		BaseURL: "http://127.0.0.1:1", // connection refused → *url.Error
		Model:   "gemini-2.5-pro",
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

func TestComplete_Concurrent_NoRace(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(okBody))
	}))
	defer srv.Close()
	p := newTestProvider(t, srv)
	const N = 32
	errs := make(chan error, N)
	for i := 0; i < N; i++ {
		go func() {
			_, err := p.Complete(context.Background(), port.CompletionRequest{User: "u", MaxTokens: 16})
			errs <- err
		}()
	}
	for i := 0; i < N; i++ {
		if err := <-errs; err != nil {
			t.Errorf("goroutine %d err=%v", i, err)
		}
	}
}
