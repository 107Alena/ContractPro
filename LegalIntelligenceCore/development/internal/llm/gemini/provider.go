// Package gemini implements port.LLMProviderPort against Google's Gemini
// `generateContent` REST API
// (POST {BaseURL}/v1beta/models/{model}:generateContent). The adapter is
// stateless, hermetic (stdlib + internal/domain/{model,port} only), and
// consumes its config through a small local struct so it can be reused in
// tests without importing internal/config.
//
// Spec: LegalIntelligenceCore/architecture/llm-provider-abstraction.md §1.1–§1.6.
//
// **Deviation from the literal acceptance criteria / arch §1.1**
// (code-architect Q10): the criteria say
// `var _ port.LLMProviderPort = (*geminiProvider)(nil)` (a lowercase type).
// Both shipped siblings (claude, openai) deliberately export `Provider`
// instead, for stutter-free cross-package consistency; a lone lowercase
// `geminiProvider` would be the real inconsistency. This package follows the
// established, reviewed sibling precedent and exports `Provider`.
//
// **Deviation from the Claude / OpenAI siblings** (code-architect Q4): Gemini
// puts the model id in the request URL *path*
// (/v1beta/models/{model}:generateContent), not the request body. So the
// endpoint is rebuilt per call from the resolved model (chooseModel). The
// model id is validated against a path-safe charset (config.go isValidModelID)
// and the `:generateContent` custom-method suffix is kept literal — NOT
// url.PathEscape'd, which would turn ":" into "%3A" and 404 the route
// (code-architect MUST-FIX #2).
//
// **Structured outputs** (code-architect Q3 / MUST-FIX #1): a JSONSchema is
// transformed (schema.go) from JSON Schema draft-07 into the OpenAPI-3.0
// Schema subset Gemini's `generationConfig.responseSchema` accepts (UPPERCASE
// type enum, `nullable`, no `$ref`/`$defs`/`additionalProperties`), then sent
// alongside `responseMimeType:"application/json"`. The architectural intent
// (strict structured outputs) is preserved. Gemini-3 series models use a newer
// `generationConfig.responseFormat` object; v1's default model
// `gemini-2.5-pro` uses `responseSchema` — flagged as a docs follow-up so the
// spec and the adapter do not silently disagree, mirroring how the OpenAI
// sibling documents its Responses-API deviation.
//
// **stopSequences** (code-architect Q5): unlike the OpenAI Responses API,
// Gemini *does* support `generationConfig.stopSequences`, so
// port.CompletionRequest.StopSequences is forwarded. But Gemini reports a
// stop-sequence hit as finishReason "STOP" (no distinct reason), so
// mapFinishReason never emits StopReasonStopSequence — the inverse asymmetry
// of the OpenAI sibling.
//
// **Auth** (code-architect Q2): the API key travels in the `x-goog-api-key`
// HEADER, never the `?key=` query parameter (both are spec-permitted). A query
// param would land in *url.Error strings, the error chain and access logs
// (security.md §3.2, 152-ФЗ). The header keeps the secret off every
// URL-bearing surface, exactly like the Bearer / x-api-key siblings.
//
// The adapter does NOT implement retry, fallback, or repair-loop behaviour —
// those belong to the Provider Router (LIC-TASK-019). It guarantees one
// thing: every non-nil error returned from Complete / HealthCheck is (or
// unwraps to) a *port.LLMProviderError so the Router can branch on
// Retryable / FallbackEligible (the adapter invariant — port/llm.go godoc).
package gemini

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

// generateContentBasePath / generateContentAction frame the per-call route.
// The model id is interpolated between them; the ":generateContent" custom
// method delimiter is kept literal (code-architect MUST-FIX #2).
const (
	generateContentBasePath = "/v1beta/models/"
	generateContentAction   = ":generateContent"
)

// apiKeyHeader is the Gemini API-key header. Chosen over the ?key= query
// parameter so the secret never enters a URL (code-architect Q2).
const apiKeyHeader = "x-goog-api-key"

// healthCheckUserContent is the trivial user-side payload sent by
// HealthCheck. A single token keeps cost negligible while still exercising
// auth, transport, and response decoding.
const healthCheckUserContent = "ping"

// healthCheckMaxTokens is the output cap for the HealthCheck probe.
//
// Unlike the OpenAI Responses API (which rejects max_output_tokens < 16 and
// forced that sibling to deviate from the written spec), Gemini documents no
// such floor, so the probe uses 10 — matching llm-provider-abstraction.md §2.3
// and the Claude sibling exactly. No deviation note is required here
// (code-architect Q9).
const healthCheckMaxTokens = 10

// maxResponseBytes caps how many bytes we read from a response body before
// abandoning. 8 MiB comfortably covers any agent's maximum output budget and
// bounds memory in the face of a misbehaving wire peer. Mirrors the siblings.
const maxResponseBytes = 8 << 20

// maxDrainBytes caps how many post-LimitReader bytes the deferred body-drain
// will consume before giving up. Bounds tail latency under an adversarial
// peer that streams unbounded payload after a 4xx — Client.Timeout is
// intentionally unset (caller's ctx owns the budget). Mirrors the siblings.
const maxDrainBytes = 64 << 10

// HTTPClient is the consumer-side interface covering the only http.Client
// method this adapter calls. Defining it locally inverts the dependency and
// lets unit tests substitute an httptest.Server's client (or a hand-rolled
// fake) without touching the global http.DefaultTransport.
type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

// Provider is the adapter implementation. All fields are immutable after
// NewGeminiProvider returns; safe for concurrent use as long as the underlying
// HTTPClient is (the default net/http client is). The per-call endpoint string
// is a local in Complete / HealthCheck, never a memoized field, so the
// per-request model override introduces no shared mutable state
// (code-architect concurrency note).
type Provider struct {
	http    HTTPClient
	baseURL string // trailing slash trimmed; path is appended per call
	apiKey  string
	model   string
}

// Compile-time assertion that Provider satisfies the port contract.
var _ port.LLMProviderPort = (*Provider)(nil)

// NewGeminiProvider validates cfg and returns a ready provider. Returns the
// aggregated config error (errors.Join) on misconfiguration so an operator
// sees every fixable field in one log line; constructors are not retried.
func NewGeminiProvider(cfg GeminiConfig) (*Provider, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	client := cfg.HTTPClient
	if client == nil {
		client = defaultHTTPClient()
	}

	return &Provider{
		http:    client,
		baseURL: strings.TrimRight(cfg.BaseURL, "/"),
		apiKey:  cfg.APIKey,
		model:   cfg.Model,
	}, nil
}

// ID returns the canonical provider identifier used throughout the Router,
// metric labels and OTel attributes.
func (p *Provider) ID() port.LLMProviderID {
	return port.ProviderGemini
}

// endpointFor builds the per-call generateContent URL for the resolved model.
// model is assumed already validated by isValidModelID, so it is path-safe and
// concatenated literally (NOT url.PathEscape'd — that would encode the
// ":generateContent" delimiter and 404 the route, code-architect MUST-FIX #2).
func (p *Provider) endpointFor(model string) string {
	return p.baseURL + generateContentBasePath + model + generateContentAction
}

// Complete issues one generateContent call. The CompletionRequest is
// translated into Gemini's native format, the response parsed into the
// adapter-agnostic CompletionResponse, and any failure wrapped as a
// *LLMProviderError before being returned. The caller's context.WithTimeout
// owns the budget (the adapter does not impose its own).
func (p *Provider) Complete(ctx context.Context, req port.CompletionRequest) (port.CompletionResponse, error) {
	model := chooseModel(req.Model, p.model)
	// A per-request model override flows into the URL path; re-validate so a
	// caller-supplied junk model cannot break the route (code-architect
	// MUST-FIX #2). p.model was already validated at construction.
	if !isValidModelID(model) {
		return port.CompletionResponse{}, port.NewLLMProviderError(port.LLMErrorMalformedRequest,
			fmt.Errorf("gemini: model %q contains characters outside the path-safe set [A-Za-z0-9._-]", model))
	}

	payload, err := buildRequestPayload(req)
	if err != nil {
		return port.CompletionResponse{}, err
	}

	body, err := json.Marshal(payload)
	if err != nil {
		// json.Marshal failure on our own structs is a programming error;
		// surface it as MALFORMED so the Router does not retry blindly.
		return port.CompletionResponse{}, port.NewLLMProviderError(port.LLMErrorMalformedRequest,
			fmt.Errorf("gemini: marshal request: %w", err))
	}

	start := time.Now()
	respBody, httpErr := p.do(ctx, p.endpointFor(model), body)
	latencyMs := time.Since(start).Milliseconds()
	if httpErr != nil {
		return port.CompletionResponse{}, httpErr
	}

	return parseResponse(respBody, model, latencyMs)
}

// HealthCheck issues a minimal generateContent call (max_output_tokens=10, one
// short user message, NO systemInstruction). The dual return distinguishes
// wire-level provider failures (typed err, nil) — used by Router to
// permanently mark auth/quota issues — from transport failures (nil, err) the
// Router treats as transient (llm-provider-abstraction.md §1.3 / port godoc).
func (p *Provider) HealthCheck(ctx context.Context) (*port.LLMProviderError, error) {
	// No System: buildRequestPayload omits systemInstruction entirely rather
	// than sending {parts:[{text:""}]}, which Gemini rejects with 400 on some
	// model versions and would make HealthCheck permanently fail / trip
	// /readyz (code-architect MUST-FIX #5).
	req := port.CompletionRequest{
		Model:       p.model,
		User:        healthCheckUserContent,
		MaxTokens:   healthCheckMaxTokens,
		Temperature: 0,
	}
	payload, err := buildRequestPayload(req)
	if err != nil {
		// buildRequestPayload contractually returns only *LLMProviderError;
		// the wrap on the fall-through preserves the adapter invariant if that
		// contract were ever relaxed.
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
			fmt.Errorf("gemini: marshal healthcheck request: %w", marshalErr)), nil
	}

	_, httpErr := p.do(ctx, p.endpointFor(p.model), body)
	if httpErr == nil {
		return nil, nil
	}

	if lpe, ok := port.AsLLMProviderError(httpErr); ok {
		switch lpe.Code {
		case port.LLMErrorTimeout, port.LLMErrorNetwork:
			// Did not reach the provider; surface as transport so the Router
			// keeps the provider transient-unhealthy rather than permanently.
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

// do executes one HTTP POST to the generateContent endpoint with
// body=bodyBytes. Returns the response body on 2xx; otherwise a typed
// LLMProviderError produced by mapHTTPError / mapTransportError. The response
// body is fully drained (up to maxResponseBytes) so the connection is returned
// to the keep-alive pool.
func (p *Provider) do(ctx context.Context, endpoint string, bodyBytes []byte) ([]byte, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		// NewRequestWithContext only fails on URL parse / ctx == nil; both
		// are programming errors but we still must obey the adapter
		// invariant — surface as typed.
		return nil, port.NewLLMProviderError(port.LLMErrorMalformedRequest,
			fmt.Errorf("gemini: build request: %w", err))
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set(apiKeyHeader, p.apiKey)

	resp, err := p.http.Do(httpReq)
	if err != nil {
		return nil, mapTransportError(err)
	}
	defer func() {
		// Cap the drain so an adversarial peer streaming unbounded body after
		// a 4xx cannot hold our goroutine past the caller's ctx deadline. If
		// the body exceeds maxDrainBytes we lose connection reuse — a fair
		// trade against tail-latency blow-up under partial-failure scenarios.
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

// chooseModel resolves the model identifier used on the wire: an explicit
// per-request Model wins (allows agent-level overrides without re-wiring the
// provider), otherwise the adapter's default applies.
func chooseModel(reqModel, defaultModel string) string {
	if trimmed := strings.TrimSpace(reqModel); trimmed != "" {
		// Return the trimmed value: a padded-but-valid override
		// (" gemini-2.5-pro ") should resolve, not fail isValidModelID on the
		// surrounding whitespace (code-reviewer L1). The default model was
		// already validated at construction.
		return trimmed
	}
	return defaultModel
}
