// Package openai implements port.LLMProviderPort against OpenAI's Responses
// API (POST /v1/responses). The adapter is stateless, hermetic (stdlib +
// internal/domain/{model,port} only), and consumes its config through a small
// local struct so it can be reused in tests without importing internal/config.
//
// Spec: LegalIntelligenceCore/architecture/llm-provider-abstraction.md §1.1–§1.6.
//
// **Deviation from the literal acceptance criteria / arch §1.5** (code-architect
// Q2): the criteria text says structured outputs use
// `response_format:{type:"json_schema",strict:true,schema:...}`. That is the
// *Chat Completions* wire shape. This adapter targets the *Responses API*
// (`/v1/responses`, arch §1.3), which expresses structured outputs as
// `text:{format:{type:"json_schema",name:...,strict:true,schema:...}}` — the
// json-schema fields flattened inside `format`, with NO `response_format` key.
// The architectural *intent* (strict structured outputs, json_schema,
// strict:true) is preserved exactly; only the provider-specific wire framing
// differs. Future maintainers must NOT "fix" this to `response_format` — that
// is rejected by `/v1/responses`.
//
// **Deviation from the Claude sibling** (code-architect Q4): the Responses API
// has no `stop`/`stop_sequences` request parameter (unlike Chat Completions).
// port.CompletionRequest.StopSequences is therefore IGNORED here. LIC v1 agents
// rely on strict JSON-schema outputs, not stop sequences, and the port marks
// StopSequences "optional". Consequently mapStopReason never emits
// StopReasonStopSequence — the Responses API cannot produce it.
//
// The adapter does NOT implement retry, fallback, or repair-loop behaviour —
// those belong to the Provider Router (LIC-TASK-019). It guarantees one
// thing: every non-nil error returned from Complete / HealthCheck is (or
// unwraps to) a *port.LLMProviderError so the Router can branch on
// Retryable / FallbackEligible (the adapter invariant — port/llm.go godoc).
package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"contractpro/legal-intelligence-core/internal/domain/port"
)

// responsesEndpointPath is the path appended to OpenAIConfig.BaseURL for the
// Responses API route (OpenAI API contract).
const responsesEndpointPath = "/v1/responses"

// healthCheckUserContent is the trivial user-side payload sent by
// HealthCheck. A single token keeps cost negligible while still exercising
// auth, transport, and response decoding.
const healthCheckUserContent = "ping"

// healthCheckMaxTokens is the output cap for the HealthCheck probe.
//
// **Deliberate deviation from the Claude sibling and the written spec**
// (code-architect Q6): port/llm.go godoc and llm-provider-abstraction.md §2.3
// both say `max_tokens=10`. The OpenAI Responses API rejects
// `max_output_tokens` below 16 with a 400, so the probe would never succeed at
// 10. 16 is the documented Responses-API floor; this is a provider-specific
// minimum, not a copy error. Flagged for a docs follow-up so the spec and the
// two adapters do not silently disagree.
const healthCheckMaxTokens = 16

// maxResponseBytes caps how many bytes we read from a response body before
// abandoning. 8 MiB comfortably covers any agent's maximum max_output_tokens
// budget (the largest detailed-report agent emits ~5K output tokens ≈ 25 KiB
// of JSON) and bounds memory in the face of a misbehaving wire peer. Mirrors
// the Claude sibling.
const maxResponseBytes = 8 << 20

// maxDrainBytes caps how many post-LimitReader bytes the deferred body-drain
// will consume before giving up. Bounds tail latency under an adversarial
// peer that streams unbounded payload after a 4xx — Client.Timeout is
// intentionally unset (caller's ctx owns the budget), so without this cap a
// huge error body would block our goroutine until TCP keepalive kicks in.
// 64 KiB lets most realistic error bodies finish draining (OpenAI error
// envelopes are ~200 B) while sacrificing connection reuse on truly oversized
// peers — a fair trade. Mirrors the Claude sibling.
const maxDrainBytes = 64 << 10

// HTTPClient is the consumer-side interface covering the only http.Client
// method this adapter calls. Defining it locally inverts the dependency and
// lets unit tests substitute an httptest.Server's client (or a hand-rolled
// fake) without touching the global http.DefaultTransport.
type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

// Provider is the adapter implementation. All fields are immutable after
// NewOpenAIProvider returns; safe for concurrent use as long as the underlying
// HTTPClient is (the default net/http client is).
type Provider struct {
	http     HTTPClient
	endpoint string
	apiKey   string
	model    string
}

// Compile-time assertion that Provider satisfies the port contract.
var _ port.LLMProviderPort = (*Provider)(nil)

// NewOpenAIProvider validates cfg and returns a ready provider. Returns the
// aggregated config error (errors.Join) on misconfiguration so an operator
// sees every fixable field in one log line; constructors are not retried.
func NewOpenAIProvider(cfg OpenAIConfig) (*Provider, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	client := cfg.HTTPClient
	if client == nil {
		client = defaultHTTPClient()
	}

	endpoint := strings.TrimRight(cfg.BaseURL, "/") + responsesEndpointPath

	return &Provider{
		http:     client,
		endpoint: endpoint,
		apiKey:   cfg.APIKey,
		model:    cfg.Model,
	}, nil
}

// ID returns the canonical provider identifier used throughout the Router,
// metric labels and OTel attributes.
func (p *Provider) ID() port.LLMProviderID {
	return port.ProviderOpenAI
}

// Complete issues one /v1/responses call. The CompletionRequest is translated
// into OpenAI's native Responses format, the response is parsed into the
// adapter-agnostic CompletionResponse, and any failure is wrapped as a
// *LLMProviderError before being returned. The caller's context.WithTimeout
// owns the budget (the adapter does not impose its own).
func (p *Provider) Complete(ctx context.Context, req port.CompletionRequest) (port.CompletionResponse, error) {
	model := chooseModel(req.Model, p.model)

	payload, err := buildRequestPayload(req, model)
	if err != nil {
		return port.CompletionResponse{}, err
	}

	body, err := json.Marshal(payload)
	if err != nil {
		// json.Marshal failure on our own structs is a programming error;
		// surface it as MALFORMED so the Router does not retry blindly.
		return port.CompletionResponse{}, port.NewLLMProviderError(port.LLMErrorMalformedRequest,
			fmt.Errorf("openai: marshal request: %w", err))
	}

	start := time.Now()
	respBody, httpErr := p.do(ctx, body)
	latencyMs := time.Since(start).Milliseconds()
	if httpErr != nil {
		return port.CompletionResponse{}, httpErr
	}

	return parseResponse(respBody, model, latencyMs)
}

// HealthCheck issues a minimal /v1/responses call (max_output_tokens=16, one
// short user message). The dual return distinguishes wire-level provider
// failures (typed err, nil) — used by Router to permanently mark auth/quota
// issues — from transport failures (nil, err) the Router treats as transient
// (llm-provider-abstraction.md §1.3 / port godoc).
func (p *Provider) HealthCheck(ctx context.Context) (*port.LLMProviderError, error) {
	req := port.CompletionRequest{
		Model:       p.model,
		User:        healthCheckUserContent,
		MaxTokens:   healthCheckMaxTokens,
		Temperature: 0,
	}
	payload, err := buildRequestPayload(req, p.model)
	if err != nil {
		// buildRequestPayload returning err means our own input was invalid;
		// surface as a non-transport typed err so the Router can act on it.
		// buildRequestPayload contractually returns only *LLMProviderError;
		// the wrap on the fall-through preserves the adapter invariant if
		// that contract were ever relaxed.
		if lpe, ok := port.AsLLMProviderError(err); ok {
			return lpe, nil
		}
		return port.NewLLMProviderError(port.LLMErrorMalformedRequest, err), nil
	}
	body, marshalErr := json.Marshal(payload)
	if marshalErr != nil {
		// Treat marshal failure of our OWN struct as a programming error and
		// surface it typed so the Router can act on it (and the adapter
		// invariant — every non-nil error is or wraps *LLMProviderError —
		// holds for HealthCheck just as it does for Complete).
		return port.NewLLMProviderError(port.LLMErrorMalformedRequest,
			fmt.Errorf("openai: marshal healthcheck request: %w", marshalErr)), nil
	}

	_, httpErr := p.do(ctx, body)
	if httpErr == nil {
		return nil, nil
	}

	if lpe, ok := port.AsLLMProviderError(httpErr); ok {
		switch lpe.Code {
		case port.LLMErrorTimeout, port.LLMErrorNetwork:
			// Did not reach the provider; surface as transport so the Router
			// keeps the provider as transient-unhealthy rather than
			// permanently-unhealthy.
			return nil, httpErr
		default:
			return lpe, nil
		}
	}
	// Defensive: p.do is contractually guaranteed to return only typed
	// errors, so this branch is dead. We still wrap to preserve the adapter
	// invariant in the face of a future regression.
	return port.NewLLMProviderError(port.LLMErrorNetwork, httpErr), nil
}

// do executes one HTTP POST to the Responses endpoint with body=bodyBytes.
// Returns the response body on 2xx; otherwise a typed LLMProviderError
// produced by mapHTTPError / mapTransportError. The response body is fully
// drained (up to maxResponseBytes) so the connection is returned to the
// keep-alive pool.
func (p *Provider) do(ctx context.Context, bodyBytes []byte) ([]byte, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		// NewRequestWithContext only fails on URL parse / ctx == nil; both
		// are programming errors but we still must obey the adapter
		// invariant — surface as typed.
		return nil, port.NewLLMProviderError(port.LLMErrorMalformedRequest,
			fmt.Errorf("openai: build request: %w", err))
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)

	resp, err := p.http.Do(httpReq)
	if err != nil {
		return nil, mapTransportError(err)
	}
	defer func() {
		// Cap the drain so an adversarial peer streaming unbounded body after
		// a 4xx cannot hold our goroutine past the caller's ctx deadline. If
		// the body exceeds maxDrainBytes we lose connection reuse — that is
		// a fair trade against tail-latency blow-up under partial-failure
		// scenarios.
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxDrainBytes))
		_ = resp.Body.Close()
	}()

	respBody, readErr := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if readErr != nil {
		return nil, mapTransportError(readErr)
	}

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return respBody, nil
	}
	return nil, mapHTTPError(resp.StatusCode, resp.Header, respBody)
}

// chooseModel resolves the model identifier sent on the wire: an explicit
// per-request Model wins (allows agent-level overrides without re-wiring the
// provider), otherwise the adapter's default applies.
func chooseModel(reqModel, defaultModel string) string {
	if strings.TrimSpace(reqModel) != "" {
		return reqModel
	}
	return defaultModel
}
