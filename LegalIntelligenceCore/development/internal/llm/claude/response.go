package claude

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"contractpro/legal-intelligence-core/internal/domain/port"
)

// anthropicResponse is the wire-shape returned by /v1/messages on 2xx.
//
// Content is an ordered slice of typed blocks; under v1 we recognise:
//   - "text"     — free-form model output (used when no JSONSchema was sent)
//   - "tool_use" — schema-driven structured output (Input is JSON object)
//
// Usage carries token counts; cache_read_input_tokens is broken out so the
// Cost & Usage Tracker can bill it at the ~10× discounted rate
// (llm-provider-abstraction.md §4.1).
type anthropicResponse struct {
	ID         string             `json:"id"`
	Type       string             `json:"type"`
	Role       string             `json:"role"`
	Model      string             `json:"model"`
	Content    []anthropicContent `json:"content"`
	StopReason string             `json:"stop_reason"`
	Usage      anthropicUsage     `json:"usage"`
}

// anthropicContent is one block in the response content array. For text
// blocks Text is populated; for tool_use blocks Name + Input are populated.
type anthropicContent struct {
	Type  string          `json:"type"`
	Text  string          `json:"text,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`
}

// anthropicUsage carries the token counts for billing and observability.
// CacheReadInputTokens may be absent in legacy / non-cache responses; the
// zero value is the correct fallback (no cache hit ⇒ all input is billable).
type anthropicUsage struct {
	InputTokens         int `json:"input_tokens"`
	CacheReadInputTokens int `json:"cache_read_input_tokens,omitempty"`
	OutputTokens        int `json:"output_tokens"`
}

// parseResponse converts a successful 2xx Anthropic response into the
// adapter-agnostic CompletionResponse. expectToolUse encodes whether the
// caller sent a JSONSchema (see provider.go); when true the parser demands
// exactly one tool_use block named virtualToolName and returns its `input`
// as a compact JSON string. When false it concatenates every text block in
// order — defensive against any stray tool_use blocks (code-architect Q5).
//
// expectToolUse mismatch (asked for schema, got no tool_use) yields
// LLMErrorMalformedRequest — Retryable=false, FallbackEligible=false per the
// catalog. The response shape is deterministic from request shape, so
// retrying same input would repeat the failure.
func parseResponse(body []byte, expectToolUse bool, model string, latencyMs int64) (port.CompletionResponse, error) {
	var resp anthropicResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		// Decode failure on a 2xx body means the provider sent corrupt JSON
		// — not our payload bug. Surface as SERVER_ERROR (retryable + fallback
		// eligible) so the Router can either retry the same provider or move
		// on, rather than locking out fallback with MALFORMED. golang-pro M4.
		return port.CompletionResponse{}, port.NewLLMProviderError(port.LLMErrorServerError,
			fmt.Errorf("claude: decode response: %w", err))
	}

	content, err := extractContent(resp.Content, expectToolUse)
	if err != nil {
		return port.CompletionResponse{}, err
	}

	reportedModel := resp.Model
	if reportedModel == "" {
		reportedModel = model
	}

	return port.CompletionResponse{
		Content:           content,
		InputTokens:       resp.Usage.InputTokens,
		CachedInputTokens: resp.Usage.CacheReadInputTokens,
		OutputTokens:      resp.Usage.OutputTokens,
		StopReason:        mapStopReason(resp.StopReason),
		LatencyMs:         latencyMs,
		ProviderID:        port.ProviderClaude,
		Model:             reportedModel,
	}, nil
}

// extractContent walks the response content blocks per the contract above.
// Both branches return LLMErrorMalformedRequest when the model violated the
// shape we requested — the Router maps this to AGENT_OUTPUT_INVALID at the
// pipeline level (error-handling.md §3.3).
func extractContent(blocks []anthropicContent, expectToolUse bool) (string, error) {
	if expectToolUse {
		for _, b := range blocks {
			if b.Type == "tool_use" && b.Name == virtualToolName {
				if len(b.Input) == 0 {
					// Empty tool_use input is provider misbehaviour, not our
					// payload bug — SERVER_ERROR preserves Router fallback.
					return "", port.NewLLMProviderError(port.LLMErrorServerError,
						errors.New("claude: tool_use block has empty input"))
				}
				return string(b.Input), nil
			}
		}
		// We forced tool_choice on the wire but got no matching tool_use back
		// — Anthropic violated the tool-choice contract. Provider problem,
		// not ours: SERVER_ERROR keeps the door open for retry / fallback to
		// a different provider. golang-pro M4.
		return "", port.NewLLMProviderError(port.LLMErrorServerError,
			fmt.Errorf("claude: expected tool_use block named %q, found none", virtualToolName))
	}

	var sb strings.Builder
	for _, b := range blocks {
		if b.Type == "text" {
			sb.WriteString(b.Text)
		}
	}
	if sb.Len() == 0 {
		return "", port.NewLLMProviderError(port.LLMErrorServerError,
			errors.New("claude: response contained no text blocks"))
	}
	return sb.String(), nil
}

// mapStopReason translates Anthropic's stop_reason string into the typed
// port enum. tool_use is mapped to end_turn because, in our flow, a tool_use
// stop means the model delivered the structured payload we asked for — the
// downstream agent treats that as a normal completion (the schema-driven
// happy path), not a content filter or truncation.
//
// Unknown stop reasons fall through to StopReasonEndTurn rather than failing
// the call: parse-side strictness already enforced the response shape, and
// future Anthropic enum additions should not break the adapter — the Router
// already inspects StopReason for the filter / truncation cases.
func mapStopReason(s string) port.StopReason {
	switch s {
	case "end_turn", "tool_use":
		return port.StopReasonEndTurn
	case "max_tokens":
		return port.StopReasonMaxTokens
	case "stop_sequence":
		return port.StopReasonStopSequence
	case "refusal":
		// Anthropic emits "refusal" when the model declined the request on
		// content-policy grounds. Port maps this to StopReasonContentFilter
		// (treated by Router as LLMErrorContentPolicy — llm-provider-
		// abstraction.md §1.1 spec note on content_filter).
		return port.StopReasonContentFilter
	default:
		return port.StopReasonEndTurn
	}
}
