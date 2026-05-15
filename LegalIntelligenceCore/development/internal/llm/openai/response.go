package openai

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"contractpro/legal-intelligence-core/internal/domain/port"
)

// responsesResponse is the wire-shape returned by /v1/responses on 2xx.
//
// Status is the top-level lifecycle marker: "completed" | "incomplete" |
// "failed". IncompleteDetails.Reason refines an "incomplete" status
// ("max_output_tokens" | "content_filter" | ...). Output is an ordered slice
// of typed items; under v1 we recognise "message" items and ignore the rest
// (e.g. "reasoning" items emitted by reasoning models).
//
// Usage carries token counts. input_tokens_details.cached_tokens is present on
// the wire but intentionally NOT surfaced — v1 reports CachedInputTokens=0 and
// bills all input at full rate (acceptance criteria LIC-TASK-015 /
// llm-provider-abstraction.md §4.1).
type responsesResponse struct {
	ID                string                 `json:"id"`
	Object            string                 `json:"object"`
	Model             string                 `json:"model"`
	Status            string                 `json:"status"`
	IncompleteDetails *responsesIncompleteDet `json:"incomplete_details"`
	Error             *responsesError        `json:"error"`
	Output            []responsesOutputItem  `json:"output"`
	Usage             responsesUsage         `json:"usage"`
}

// responsesIncompleteDet carries the reason a response stopped short of a
// natural end ("max_output_tokens", "content_filter", ...).
type responsesIncompleteDet struct {
	Reason string `json:"reason"`
}

// responsesError is the inline error object present when Status=="failed"
// (distinct from the HTTP-level error envelope handled in errors.go). It
// describes a provider-side generation failure inside an otherwise-2xx body.
type responsesError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// responsesOutputItem is one item in the response `output` array. For
// "message" items Role + Content are populated; other item types
// ("reasoning", tool calls) are ignored by the v1 adapter.
type responsesOutputItem struct {
	Type    string                 `json:"type"`
	ID      string                 `json:"id"`
	Status  string                 `json:"status"`
	Role    string                 `json:"role"`
	Content []responsesContentPart `json:"content"`
}

// responsesContentPart is one part of a message item's content array. For
// "output_text" parts Text is populated; for "refusal" parts Refusal carries
// the model's policy-grounded decline.
type responsesContentPart struct {
	Type    string `json:"type"`
	Text    string `json:"text,omitempty"`
	Refusal string `json:"refusal,omitempty"`
}

// responsesUsage carries the token counts for billing and observability.
// CachedTokens is decoded for completeness but deliberately discarded — v1
// does not track OpenAI implicit prompt caching.
type responsesUsage struct {
	InputTokens        int `json:"input_tokens"`
	OutputTokens       int `json:"output_tokens"`
	InputTokensDetails struct {
		CachedTokens int `json:"cached_tokens"`
	} `json:"input_tokens_details"`
}

const (
	statusCompleted  = "completed"
	statusIncomplete = "incomplete"
	statusFailed     = "failed"

	incompleteReasonMaxTokens     = "max_output_tokens"
	incompleteReasonContentFilter = "content_filter"

	contentPartOutputText = "output_text"
	contentPartRefusal    = "refusal"

	outputItemMessage = "message"
)

// parseResponse converts a successful 2xx Responses API body into the
// adapter-agnostic CompletionResponse.
//
//   - JSON decode failure on a 2xx body is provider misbehaviour, not our
//     payload bug → LLMErrorServerError (retryable + fallback-eligible),
//     mirroring the Claude sibling so the Router can retry / fall back rather
//     than locking out fallback with MALFORMED.
//   - Status=="failed" is a provider-side generation failure → SERVER_ERROR.
//   - A refusal content part OR an incomplete/content_filter status maps to
//     StopReasonContentFilter and returns a SUCCESSFUL response: the port
//     contract (llm.go godoc on StopReasonContentFilter) and arch §1.1 specify
//     that the Router — not the adapter — translates content_filter into
//     LLMErrorContentPolicy. This deliberately mirrors Claude's
//     mapStopReason("refusal") path so both adapters behave identically for
//     the same logical event (code-architect Q7).
//   - Otherwise empty / whitespace-only output is provider misbehaviour →
//     SERVER_ERROR (preserves Router fallback, mirrors Claude M4).
func parseResponse(body []byte, model string, latencyMs int64) (port.CompletionResponse, error) {
	var resp responsesResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return port.CompletionResponse{}, port.NewLLMProviderError(port.LLMErrorServerError,
			fmt.Errorf("openai: decode response: %w", err))
	}

	if resp.Status == statusFailed {
		// resp.Error.Message is provider-controlled and unbounded; route it
		// through boundedErrorMessage so a multi-MB or PII-bearing generation
		// message cannot bypass the truncation guarantee the HTTP path already
		// enforces (security-engineer review S1). errors.New over
		// fmt.Errorf("%s", …) for the static case (golang-pro N3).
		if resp.Error != nil {
			return port.CompletionResponse{}, port.NewLLMProviderError(port.LLMErrorServerError,
				fmt.Errorf("openai: response status=failed code=%s: %s",
					resp.Error.Code, boundedErrorMessage(resp.Error.Message)))
		}
		return port.CompletionResponse{}, port.NewLLMProviderError(port.LLMErrorServerError,
			errors.New("openai: response status=failed"))
	}

	text, refusal := extractContent(resp.Output)
	stop := mapStopReason(resp.Status, resp.IncompleteDetails, refusal)

	// When the model refused, the refusal string is the meaningful payload —
	// prefer it over any partial output_text so the content-filter event is
	// deterministic even in the mixed (output_text + refusal) case
	// (golang-pro S2). The refusal text is untrusted and unbounded; it is
	// returned as CompletionResponse.Content (NOT a bounded error string)
	// because the Router maps StopReasonContentFilter → LLMErrorContentPolicy
	// from the typed StopReason, and body-content redaction is the logger's
	// responsibility downstream, not the adapter's (security-engineer S3).
	content := text
	if refusal {
		if rt := refusalText(resp.Output); rt != "" {
			content = rt
		}
	}

	if stop != port.StopReasonContentFilter && strings.TrimSpace(content) == "" {
		// No output_text and not a policy decline — the provider returned a
		// structurally valid but empty 2xx. Provider misbehaviour, not our
		// payload bug: SERVER_ERROR preserves Router retry / fallback.
		return port.CompletionResponse{}, port.NewLLMProviderError(port.LLMErrorServerError,
			fmt.Errorf("openai: response contained no output_text (status=%q)", resp.Status))
	}

	reportedModel := resp.Model
	if reportedModel == "" {
		reportedModel = model
	}

	return port.CompletionResponse{
		Content:           content,
		InputTokens:       resp.Usage.InputTokens,
		CachedInputTokens: 0, // v1: OpenAI implicit cache not tracked (acceptance criteria)
		OutputTokens:      resp.Usage.OutputTokens,
		StopReason:        stop,
		LatencyMs:         latencyMs,
		ProviderID:        port.ProviderOpenAI,
		Model:             reportedModel,
	}, nil
}

// extractContent walks the output items, concatenating output_text parts of
// "message" items in order and reporting whether any refusal part was seen.
// Non-message items (reasoning, tool calls) are ignored — they are not part of
// the v1 contract.
func extractContent(items []responsesOutputItem) (text string, refused bool) {
	var sb strings.Builder
	for _, item := range items {
		if item.Type != outputItemMessage {
			continue
		}
		for _, part := range item.Content {
			switch part.Type {
			case contentPartOutputText:
				sb.WriteString(part.Text)
			case contentPartRefusal:
				refused = true
			}
		}
	}
	return sb.String(), refused
}

// refusalText returns the first non-empty refusal string across message items,
// used only to populate Content on the content-filter path for observability.
func refusalText(items []responsesOutputItem) string {
	for _, item := range items {
		if item.Type != outputItemMessage {
			continue
		}
		for _, part := range item.Content {
			if part.Type == contentPartRefusal && part.Refusal != "" {
				return part.Refusal
			}
		}
	}
	return ""
}

// mapStopReason derives the typed port StopReason from the Responses API
// top-level status, the incomplete reason, and whether a refusal was emitted.
//
// StopReasonStopSequence is intentionally never produced: the Responses API
// has no stop-sequence parameter (see package doc deviation), so it cannot
// stop for that reason. Unknown statuses fall through to StopReasonEndTurn —
// content extraction already guarded the shape, and a future OpenAI status
// addition must not break the adapter (mirrors Claude's lenient default).
func mapStopReason(status string, inc *responsesIncompleteDet, refused bool) port.StopReason {
	if refused {
		return port.StopReasonContentFilter
	}
	switch status {
	case statusIncomplete:
		if inc != nil {
			switch inc.Reason {
			case incompleteReasonMaxTokens:
				return port.StopReasonMaxTokens
			case incompleteReasonContentFilter:
				return port.StopReasonContentFilter
			}
		}
		return port.StopReasonEndTurn
	default:
		return port.StopReasonEndTurn
	}
}
