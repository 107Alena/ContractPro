package gemini

import (
	"encoding/json"
	"fmt"
	"strings"

	"contractpro/legal-intelligence-core/internal/domain/port"
)

// generateContentResponse is the wire-shape returned by generateContent on
// 2xx.
//
//   - PromptFeedback is present when the *prompt itself* was blocked (then
//     Candidates is empty) and MAY also be present alongside candidates
//     carrying only advisory safety ratings. BlockReason non-empty is the
//     authoritative "prompt blocked" signal (code-architect MUST-FIX #3).
//   - Candidates[0].FinishReason drives the stop reason when the prompt was
//     not blocked.
//   - UsageMetadata.CachedContentTokenCount is decoded for completeness but
//     intentionally discarded — v1 does not use Gemini context caching and
//     reports CachedInputTokens=0 (arch §4.1/§5.1, code-architect Q8), exactly
//     as the OpenAI sibling discards input_tokens_details.cached_tokens.
//   - safetyRatings is intentionally NOT decoded: it is advisory metadata and
//     MUST NOT independently drive the StopReason — only finishReason /
//     blockReason do (code-architect MUST-FIX #4).
type generateContentResponse struct {
	Candidates     []geminiCandidate     `json:"candidates"`
	PromptFeedback *geminiPromptFeedback `json:"promptFeedback"`
	UsageMetadata  geminiUsageMetadata   `json:"usageMetadata"`
	ModelVersion   string                `json:"modelVersion"`
}

// geminiCandidate is one generated candidate. v1 reads only the first.
type geminiCandidate struct {
	Content      geminiRespContent `json:"content"`
	FinishReason string            `json:"finishReason"`
	Index        int               `json:"index"`
}

// geminiRespContent is the candidate's content. Parts may include reasoning
// ("thought") parts on thinking-enabled models; those are skipped in
// extractContent — only answer text is surfaced (mirrors the OpenAI sibling
// ignoring "reasoning" output items).
type geminiRespContent struct {
	Parts []geminiRespPart `json:"parts"`
	Role  string           `json:"role"`
}

// geminiRespPart is one response content part. Thought marks an internal
// reasoning-summary part (not the answer).
type geminiRespPart struct {
	Text    string `json:"text"`
	Thought bool   `json:"thought"`
}

// geminiPromptFeedback carries prompt-level safety feedback. BlockReason is
// non-empty (e.g. "SAFETY", "BLOCKLIST", "PROHIBITED_CONTENT", "OTHER") when
// the prompt was rejected outright.
type geminiPromptFeedback struct {
	BlockReason string `json:"blockReason"`
}

// geminiUsageMetadata carries token counts for billing and observability.
// CachedContentTokenCount is decoded for completeness but discarded (see
// type doc).
type geminiUsageMetadata struct {
	PromptTokenCount        int `json:"promptTokenCount"`
	CandidatesTokenCount    int `json:"candidatesTokenCount"`
	CachedContentTokenCount int `json:"cachedContentTokenCount"`
	TotalTokenCount         int `json:"totalTokenCount"`
}

// Gemini finishReason tokens. The safety family maps to content-filter; STOP
// (also reported for a stop-sequence hit) is a normal end; MAX_TOKENS is
// truncation; everything else (incl. "OTHER", FINISH_REASON_UNSPECIFIED,
// unknown future tokens) falls through to a lenient end_turn — matching the
// claude/openai unknown-status posture.
const (
	finishReasonStop       = "STOP"
	finishReasonMaxTokens  = "MAX_TOKENS"
	finishReasonSafety     = "SAFETY"
	finishReasonRecitation = "RECITATION"
	finishReasonProhibited = "PROHIBITED_CONTENT"
	finishReasonBlocklist  = "BLOCKLIST"
	finishReasonSPII       = "SPII"
	finishReasonImageSafe  = "IMAGE_SAFETY"
)

// contentFilterFinishReasons is the set of candidate finishReason tokens that
// represent a policy-grounded decline (code-architect MUST-FIX #4).
var contentFilterFinishReasons = map[string]struct{}{
	finishReasonSafety:     {},
	finishReasonRecitation: {},
	finishReasonProhibited: {},
	finishReasonBlocklist:  {},
	finishReasonSPII:       {},
	finishReasonImageSafe:  {},
}

// parseResponse converts a successful 2xx generateContent body into the
// adapter-agnostic CompletionResponse.
//
//   - JSON decode failure on a 2xx body is provider misbehaviour, not our
//     payload bug → LLMErrorServerError (retryable + fallback-eligible),
//     mirroring claude/openai so the Router can retry / fall back rather than
//     locking out fallback with MALFORMED.
//   - A blocked prompt (promptFeedback.blockReason set) OR a candidate
//     finishReason in the safety family maps to StopReasonContentFilter and
//     returns a SUCCESSFUL response: the port contract (llm.go godoc on
//     StopReasonContentFilter) and arch §1.1 specify the Router — not the
//     adapter — translates content_filter into LLMErrorContentPolicy. This
//     mirrors claude mapStopReason("refusal") / the OpenAI refusal path so all
//     three adapters behave identically for the same logical event
//     (code-architect Q6).
//   - Otherwise empty / whitespace-only output is provider misbehaviour →
//     SERVER_ERROR (preserves Router fallback, mirrors claude/openai).
func parseResponse(body []byte, model string, latencyMs int64) (port.CompletionResponse, error) {
	var resp generateContentResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return port.CompletionResponse{}, port.NewLLMProviderError(port.LLMErrorServerError,
			fmt.Errorf("gemini: decode response: %w", err))
	}

	blockReason := ""
	if resp.PromptFeedback != nil {
		blockReason = strings.TrimSpace(resp.PromptFeedback.BlockReason)
	}

	// Prompt-block precedence: blockReason is authoritative regardless of
	// whether a (partial) candidate is also present (code-architect
	// MUST-FIX #3). Return SUCCESS with the descriptive reason as Content —
	// untrusted and unbounded, redacted by the logger downstream, NOT a
	// bounded error string (mirrors the OpenAI refusalText decision).
	if blockReason != "" {
		return contentFilterResponse("prompt blocked: "+blockReason, resp, model, latencyMs), nil
	}

	if len(resp.Candidates) == 0 {
		// No block and no candidates — a structurally valid but empty 2xx.
		// Provider misbehaviour, not our payload bug: SERVER_ERROR preserves
		// Router retry / fallback (mirrors claude/openai empty-2xx path).
		return port.CompletionResponse{}, port.NewLLMProviderError(port.LLMErrorServerError,
			fmt.Errorf("gemini: response contained no candidates and no blockReason"))
	}

	cand := resp.Candidates[0]
	stop := mapFinishReason(blockReason, cand.FinishReason)
	content := extractContent(cand.Content.Parts)

	if stop == port.StopReasonContentFilter {
		// Safety-family finishReason. Prefer any partial answer text; fall
		// back to the reason token so the content-filter event is observable
		// and deterministic even when Gemini returned no parts.
		msg := content
		if strings.TrimSpace(msg) == "" {
			msg = "candidate blocked: " + cand.FinishReason
		}
		return contentFilterResponse(msg, resp, model, latencyMs), nil
	}

	if strings.TrimSpace(content) == "" {
		// Not a policy decline and no text — provider misbehaviour.
		return port.CompletionResponse{}, port.NewLLMProviderError(port.LLMErrorServerError,
			fmt.Errorf("gemini: response contained no text parts (finishReason=%q)", cand.FinishReason))
	}

	return port.CompletionResponse{
		Content:           content,
		InputTokens:       resp.UsageMetadata.PromptTokenCount,
		CachedInputTokens: 0, // v1: Gemini context caching not used (arch §4.1/§5.1)
		OutputTokens:      resp.UsageMetadata.CandidatesTokenCount,
		StopReason:        stop,
		LatencyMs:         latencyMs,
		ProviderID:        port.ProviderGemini,
		Model:             reportedModel(resp.ModelVersion, model),
	}, nil
}

// contentFilterResponse builds the SUCCESSFUL content-filter CompletionResponse
// (StopReason=StopReasonContentFilter) the Router translates into
// LLMErrorContentPolicy. Token counts are still surfaced for cost accounting.
func contentFilterResponse(content string, resp generateContentResponse, model string, latencyMs int64) port.CompletionResponse {
	return port.CompletionResponse{
		Content:           content,
		InputTokens:       resp.UsageMetadata.PromptTokenCount,
		CachedInputTokens: 0,
		OutputTokens:      resp.UsageMetadata.CandidatesTokenCount,
		StopReason:        port.StopReasonContentFilter,
		LatencyMs:         latencyMs,
		ProviderID:        port.ProviderGemini,
		Model:             reportedModel(resp.ModelVersion, model),
	}
}

// extractContent concatenates the answer text parts in order, skipping
// reasoning ("thought") parts which are internal summaries, not the answer
// (mirrors the OpenAI sibling ignoring "reasoning" output items).
func extractContent(parts []geminiRespPart) string {
	var sb strings.Builder
	for _, part := range parts {
		if part.Thought {
			continue
		}
		sb.WriteString(part.Text)
	}
	return sb.String()
}

// mapFinishReason derives the typed port StopReason with a deterministic,
// fixed precedence (code-architect MUST-FIX #4):
//
//  1. prompt blockReason set            → StopReasonContentFilter
//  2. finishReason ∈ safety family      → StopReasonContentFilter
//  3. MAX_TOKENS                        → StopReasonMaxTokens
//  4. STOP                              → StopReasonEndTurn
//  5. OTHER / "" / unknown              → StopReasonEndTurn (lenient)
//
// StopReasonStopSequence is intentionally never produced: Gemini reports a
// stop-sequence hit as "STOP" (no distinct reason), so even though
// stopSequences IS forwarded (Gemini supports it — code-architect Q5) the
// adapter cannot observe that case distinctly. safetyRatings is advisory and
// never consulted here.
func mapFinishReason(blockReason, finishReason string) port.StopReason {
	if blockReason != "" {
		return port.StopReasonContentFilter
	}
	if _, blocked := contentFilterFinishReasons[finishReason]; blocked {
		return port.StopReasonContentFilter
	}
	switch finishReason {
	case finishReasonMaxTokens:
		return port.StopReasonMaxTokens
	case finishReasonStop:
		return port.StopReasonEndTurn
	default:
		return port.StopReasonEndTurn
	}
}

// reportedModel prefers Gemini's echoed modelVersion and falls back to the
// model we requested when the field is absent.
func reportedModel(modelVersion, requested string) string {
	if strings.TrimSpace(modelVersion) != "" {
		return modelVersion
	}
	return requested
}
