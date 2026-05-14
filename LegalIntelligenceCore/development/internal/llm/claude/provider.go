// Package claude implements port.LLMProviderPort against Anthropic's
// Messages API (POST /v1/messages). The adapter is stateless, hermetic
// (stdlib + internal/domain/{model,port} only), and consumes its config
// through a small local struct so it can be reused in tests without
// importing internal/config.
//
// Spec: LegalIntelligenceCore/architecture/llm-provider-abstraction.md §1.1–§1.6.
//
// The adapter does NOT implement retry, fallback, or repair-loop behaviour —
// those belong to the Provider Router (LIC-TASK-019). It guarantees one
// thing: every non-nil error returned from Complete / HealthCheck is (or
// unwraps to) a *port.LLMProviderError so the Router can branch on
// Retryable / FallbackEligible.
package claude

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

// anthropicVersionHeader pins the Messages API revision this adapter is
// implemented against (code-architect Q2). Operators MUST NOT tune this via
// env — bumping it requires reviewing payload / response wire schemas, and
// is therefore a code change.
const anthropicVersionHeader = "2023-06-01"

// messagesEndpointPath is the path appended to ClaudeConfig.BaseURL for the
// chat completion route (Anthropic API contract).
const messagesEndpointPath = "/v1/messages"

// healthCheckUserContent is the trivial user-side payload sent by
// HealthCheck. Single token keeps the cost negligible while still exercising
// auth, transport, and response decoding.
const healthCheckUserContent = "ping"

// maxResponseBytes caps how many bytes we read from a response body before
// abandoning. 8 MiB comfortably covers any agent's maximum max_tokens
// budget (the largest detailed-report agent emits ~5K output tokens ≈ 25 KiB
// of JSON) and bounds memory in the face of a misbehaving wire peer.
const maxResponseBytes = 8 << 20

// maxDrainBytes caps how many post-LimitReader bytes the deferred body-drain
// will consume before giving up. Bounds tail latency under an adversarial
// peer that streams unbounded payload after a 4xx — `Client.Timeout` is
// intentionally unset (caller's ctx owns the budget), so without this cap a
// 100 GiB error body would block our goroutine until TCP keepalive kicks in.
// 64 KiB lets most realistic error bodies finish draining (Anthropic error
// envelopes are ~200 B) while sacrificing connection reuse on truly oversized
// peers — a fair trade.
const maxDrainBytes = 64 << 10

// HTTPClient is the consumer-side interface covering the only http.Client
// method this adapter calls. Defining it locally inverts the dependency and
// lets unit tests substitute an httptest.Server's client (or a hand-rolled
// fake) without touching the global http.DefaultTransport.
type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

// Provider is the adapter implementation. All fields are immutable
// after NewClaudeProvider returns; safe for concurrent use as long as the
// underlying HTTPClient is (the default net/http client is).
type Provider struct {
	http               HTTPClient
	endpoint           string
	apiKey             string
	model              string
	promptCacheEnabled bool
}

// Compile-time assertion that Provider satisfies the port contract.
var _ port.LLMProviderPort = (*Provider)(nil)

// NewClaudeProvider validates cfg and returns a ready provider. Returns the
// aggregated config error (errors.Join) on misconfiguration so an operator
// sees every fixable field in one log line; constructors are not retried.
func NewClaudeProvider(cfg ClaudeConfig) (*Provider, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	client := cfg.HTTPClient
	if client == nil {
		client = defaultHTTPClient()
	}

	endpoint := strings.TrimRight(cfg.BaseURL, "/") + messagesEndpointPath

	return &Provider{
		http:               client,
		endpoint:           endpoint,
		apiKey:             cfg.APIKey,
		model:              cfg.Model,
		promptCacheEnabled: cfg.PromptCacheEnabled,
	}, nil
}

// ID returns the canonical provider identifier used throughout the Router,
// metric labels and OTel attributes.
func (c *Provider) ID() port.LLMProviderID {
	return port.ProviderClaude
}

// Complete issues one /v1/messages call. The CompletionRequest is translated
// into Anthropic's native chat format, the response is parsed into the
// adapter-agnostic CompletionResponse, and any failure is wrapped as a
// *LLMProviderError before being returned. The caller's context.WithTimeout
// owns the budget (the adapter does not impose its own).
func (c *Provider) Complete(ctx context.Context, req port.CompletionRequest) (port.CompletionResponse, error) {
	model := chooseModel(req.Model, c.model)
	expectToolUse := len(req.JSONSchema) > 0

	payload, err := buildRequestPayload(req, model, c.promptCacheEnabled)
	if err != nil {
		return port.CompletionResponse{}, err
	}

	body, err := json.Marshal(payload)
	if err != nil {
		// json.Marshal failure on our own structs is a programming error;
		// surface it as MALFORMED so the Router does not retry blindly.
		return port.CompletionResponse{}, port.NewLLMProviderError(port.LLMErrorMalformedRequest,
			fmt.Errorf("claude: marshal request: %w", err))
	}

	start := time.Now()
	respBody, httpErr := c.do(ctx, body)
	latencyMs := time.Since(start).Milliseconds()
	if httpErr != nil {
		return port.CompletionResponse{}, httpErr
	}

	return parseResponse(respBody, expectToolUse, model, latencyMs)
}

// HealthCheck issues a minimal /v1/messages call (max_tokens=10, one short
// user message). The dual return distinguishes wire-level provider failures
// (typed err, nil) — used by Router to permanently mark auth/quota issues —
// from transport failures (nil, err) that the Router treats as transient
// (llm-provider-abstraction.md §1.3 / port godoc).
func (c *Provider) HealthCheck(ctx context.Context) (*port.LLMProviderError, error) {
	req := port.CompletionRequest{
		Model:       c.model,
		User:        healthCheckUserContent,
		MaxTokens:   10,
		Temperature: 0,
	}
	payload, err := buildRequestPayload(req, c.model, false)
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
			fmt.Errorf("claude: marshal healthcheck request: %w", marshalErr)), nil
	}

	_, httpErr := c.do(ctx, body)
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
	// Defensive: `c.do` is contractually guaranteed to return only typed
	// errors, so this branch is dead. We still wrap to preserve the adapter
	// invariant in the face of a future regression.
	return port.NewLLMProviderError(port.LLMErrorNetwork, httpErr), nil
}

// do executes one HTTP POST to the Messages endpoint with body=bodyBytes.
// Returns the response body on 2xx; otherwise a typed LLMProviderError
// produced by mapHTTPError / mapTransportError. The response body is fully
// drained (up to maxResponseBytes) so the connection is returned to the
// keep-alive pool.
func (c *Provider) do(ctx context.Context, bodyBytes []byte) ([]byte, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		// NewRequestWithContext only fails on URL parse / ctx == nil; both
		// are programming errors but we still must obey the adapter
		// invariant — surface as typed.
		return nil, port.NewLLMProviderError(port.LLMErrorMalformedRequest,
			fmt.Errorf("claude: build request: %w", err))
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("x-api-key", c.apiKey)
	httpReq.Header.Set("anthropic-version", anthropicVersionHeader)

	resp, err := c.http.Do(httpReq)
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

	body, readErr := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if readErr != nil {
		return nil, mapTransportError(readErr)
	}

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return body, nil
	}
	return nil, mapHTTPError(resp.StatusCode, resp.Header, body)
}

// chooseModel resolves the model identifier sent on the wire: an explicit
// per-request Model wins (allows agent-level overrides without re-wiring
// the provider), otherwise the adapter's default applies.
func chooseModel(reqModel, defaultModel string) string {
	if strings.TrimSpace(reqModel) != "" {
		return reqModel
	}
	return defaultModel
}
