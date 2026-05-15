package gemini

import (
	"strings"
	"testing"

	"contractpro/legal-intelligence-core/internal/domain/port"
)

func TestParseResponse_HappyPath(t *testing.T) {
	body := []byte(`{
		"candidates":[{"content":{"parts":[{"text":"part1"},{"text":"part2"}],"role":"model"},"finishReason":"STOP"}],
		"usageMetadata":{"promptTokenCount":11,"candidatesTokenCount":22,"cachedContentTokenCount":99},
		"modelVersion":"gemini-2.5-pro-002"
	}`)
	resp, err := parseResponse(body, "gemini-2.5-pro", 42)
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if resp.Content != "part1part2" {
		t.Errorf("Content=%q, want concatenated parts", resp.Content)
	}
	if resp.InputTokens != 11 || resp.OutputTokens != 22 {
		t.Errorf("tokens i=%d o=%d", resp.InputTokens, resp.OutputTokens)
	}
	if resp.CachedInputTokens != 0 {
		t.Errorf("CachedInputTokens=%d, want 0 (cachedContentTokenCount discarded in v1)", resp.CachedInputTokens)
	}
	if resp.StopReason != port.StopReasonEndTurn {
		t.Errorf("StopReason=%v, want EndTurn", resp.StopReason)
	}
	if resp.Model != "gemini-2.5-pro-002" {
		t.Errorf("Model=%q, want echoed modelVersion", resp.Model)
	}
	if resp.LatencyMs != 42 {
		t.Errorf("LatencyMs=%d", resp.LatencyMs)
	}
}

func TestParseResponse_ModelFallbackWhenNoModelVersion(t *testing.T) {
	body := []byte(`{"candidates":[{"content":{"parts":[{"text":"x"}]},"finishReason":"STOP"}]}`)
	resp, err := parseResponse(body, "gemini-2.5-pro", 1)
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if resp.Model != "gemini-2.5-pro" {
		t.Errorf("Model=%q, want fallback to requested model", resp.Model)
	}
}

func TestParseResponse_DecodeFailure_ServerError(t *testing.T) {
	_, err := parseResponse([]byte("not-json"), "m", 0)
	lpe, ok := port.AsLLMProviderError(err)
	if !ok || lpe.Code != port.LLMErrorServerError {
		t.Fatalf("err=%v, want LLMErrorServerError", err)
	}
}

// MUST-FIX #3: blockReason wins even when a (partial) candidate is present.
func TestParseResponse_BlockReasonPrecedenceOverCandidate(t *testing.T) {
	body := []byte(`{
		"candidates":[{"content":{"parts":[{"text":"leaked partial"}]},"finishReason":"STOP"}],
		"promptFeedback":{"blockReason":"BLOCKLIST"},
		"usageMetadata":{"promptTokenCount":7}
	}`)
	resp, err := parseResponse(body, "m", 0)
	if err != nil {
		t.Fatalf("err=%v, want success", err)
	}
	if resp.StopReason != port.StopReasonContentFilter {
		t.Errorf("StopReason=%v, want ContentFilter (blockReason precedence)", resp.StopReason)
	}
	if !strings.Contains(resp.Content, "BLOCKLIST") {
		t.Errorf("Content=%q, want block reason surfaced", resp.Content)
	}
}

func TestParseResponse_EmptyCandidatesNoBlock_ServerError(t *testing.T) {
	_, err := parseResponse([]byte(`{"candidates":[]}`), "m", 0)
	lpe, ok := port.AsLLMProviderError(err)
	if !ok || lpe.Code != port.LLMErrorServerError {
		t.Fatalf("err=%v, want LLMErrorServerError", err)
	}
}

func TestParseResponse_EmptyTextNonFilter_ServerError(t *testing.T) {
	_, err := parseResponse([]byte(`{"candidates":[{"content":{"parts":[{"text":"   "}]},"finishReason":"STOP"}]}`), "m", 0)
	lpe, ok := port.AsLLMProviderError(err)
	if !ok || lpe.Code != port.LLMErrorServerError {
		t.Fatalf("err=%v, want LLMErrorServerError for empty non-filter output", err)
	}
}

func TestParseResponse_SkipsThoughtParts(t *testing.T) {
	body := []byte(`{"candidates":[{"content":{"parts":[{"text":"reasoning...","thought":true},{"text":"answer"}]},"finishReason":"STOP"}]}`)
	resp, err := parseResponse(body, "m", 0)
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if resp.Content != "answer" {
		t.Errorf("Content=%q, want only non-thought text", resp.Content)
	}
}

func TestMapFinishReason_Precedence(t *testing.T) {
	cases := []struct {
		block, finish string
		want          port.StopReason
	}{
		{"SAFETY", "STOP", port.StopReasonContentFilter},   // 1: block wins
		{"", "SAFETY", port.StopReasonContentFilter},        // 2: safety family
		{"", "RECITATION", port.StopReasonContentFilter},
		{"", "PROHIBITED_CONTENT", port.StopReasonContentFilter},
		{"", "BLOCKLIST", port.StopReasonContentFilter},
		{"", "SPII", port.StopReasonContentFilter},
		{"", "IMAGE_SAFETY", port.StopReasonContentFilter},
		{"", "MAX_TOKENS", port.StopReasonMaxTokens},        // 3
		{"", "STOP", port.StopReasonEndTurn},                // 4
		{"", "OTHER", port.StopReasonEndTurn},               // 5 lenient
		{"", "", port.StopReasonEndTurn},                    // 5 lenient
		{"", "FUTURE_UNKNOWN", port.StopReasonEndTurn},      // 5 lenient
	}
	for _, c := range cases {
		if got := mapFinishReason(c.block, c.finish); got != c.want {
			t.Errorf("mapFinishReason(%q,%q)=%v, want %v", c.block, c.finish, got, c.want)
		}
	}
}

// StopReasonStopSequence is never emitted (Gemini reports STOP for a stop-
// sequence hit — Q5).
func TestMapFinishReason_NeverStopSequence(t *testing.T) {
	for _, fr := range []string{"STOP", "MAX_TOKENS", "OTHER", ""} {
		if got := mapFinishReason("", fr); got == port.StopReasonStopSequence {
			t.Errorf("mapFinishReason(%q) = StopSequence, but Gemini cannot report it", fr)
		}
	}
}
