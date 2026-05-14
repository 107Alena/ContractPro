package claude

import (
	"errors"
	"strings"
	"testing"

	"contractpro/legal-intelligence-core/internal/domain/port"
)

func TestParseResponse_TextBlocks_Concatenated(t *testing.T) {
	body := []byte(`{
		"id":"msg_1","type":"message","role":"assistant","model":"claude-sonnet-4-6",
		"content":[
			{"type":"text","text":"part-one "},
			{"type":"text","text":"part-two"}
		],
		"stop_reason":"end_turn",
		"usage":{"input_tokens":12,"output_tokens":34}
	}`)
	resp, err := parseResponse(body, false, "claude-sonnet-4-6", 42)
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if resp.Content != "part-one part-two" {
		t.Errorf("Content=%q", resp.Content)
	}
	if resp.InputTokens != 12 || resp.OutputTokens != 34 {
		t.Errorf("tokens i=%d o=%d", resp.InputTokens, resp.OutputTokens)
	}
	if resp.CachedInputTokens != 0 {
		t.Errorf("Cached=%d, want 0", resp.CachedInputTokens)
	}
	if resp.StopReason != port.StopReasonEndTurn {
		t.Errorf("StopReason=%v", resp.StopReason)
	}
	if resp.LatencyMs != 42 {
		t.Errorf("Latency=%d, want 42", resp.LatencyMs)
	}
	if resp.ProviderID != port.ProviderClaude {
		t.Errorf("ProviderID=%v", resp.ProviderID)
	}
	if resp.Model != "claude-sonnet-4-6" {
		t.Errorf("Model=%q", resp.Model)
	}
}

func TestParseResponse_CachedInputTokens_ParsedSeparately(t *testing.T) {
	body := []byte(`{
		"id":"x","type":"message","role":"assistant","model":"m",
		"content":[{"type":"text","text":"ok"}],
		"stop_reason":"end_turn",
		"usage":{"input_tokens":100,"cache_read_input_tokens":900,"output_tokens":10}
	}`)
	resp, err := parseResponse(body, false, "m", 1)
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if resp.InputTokens != 100 {
		t.Errorf("InputTokens=%d, want 100 (uncached billable only)", resp.InputTokens)
	}
	if resp.CachedInputTokens != 900 {
		t.Errorf("CachedInputTokens=%d, want 900", resp.CachedInputTokens)
	}
}

func TestParseResponse_ToolUse_HappyPath(t *testing.T) {
	body := []byte(`{
		"id":"x","type":"message","role":"assistant","model":"m",
		"content":[{"type":"tool_use","name":"return_analysis_result","input":{"x":"y"}}],
		"stop_reason":"tool_use",
		"usage":{"input_tokens":5,"output_tokens":7}
	}`)
	resp, err := parseResponse(body, true, "m", 1)
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if resp.Content != `{"x":"y"}` {
		t.Errorf("Content=%q, want %q", resp.Content, `{"x":"y"}`)
	}
	if resp.StopReason != port.StopReasonEndTurn {
		t.Errorf("StopReason=%v, want StopReasonEndTurn (tool_use mapped to end_turn)", resp.StopReason)
	}
}

// Provider-side response misbehaviour (no tool_use when requested, wrong tool
// name, no text blocks, corrupt JSON) is classified as SERVER_ERROR — not
// MALFORMED — so the Router preserves retry + fallback semantics. MALFORMED
// is reserved for OUR-side payload bugs that re-sending cannot fix
// (golang-pro review M4).

func TestParseResponse_ExpectToolUse_NoToolUseBlock_ServerError(t *testing.T) {
	body := []byte(`{
		"content":[{"type":"text","text":"oops"}],
		"stop_reason":"end_turn",
		"usage":{"input_tokens":1,"output_tokens":1}
	}`)
	_, err := parseResponse(body, true, "m", 1)
	if err == nil {
		t.Fatalf("expected error")
	}
	var lpe *port.LLMProviderError
	if !errors.As(err, &lpe) || lpe.Code != port.LLMErrorServerError {
		t.Fatalf("err=%v want LLMErrorServerError", err)
	}
	if !lpe.Retryable || !lpe.FallbackEligible {
		t.Errorf("SERVER_ERROR must be Retryable + FallbackEligible; got %+v", lpe)
	}
}

func TestParseResponse_ExpectToolUse_WrongName_ServerError(t *testing.T) {
	body := []byte(`{
		"content":[{"type":"tool_use","name":"other_tool","input":{"x":1}}],
		"stop_reason":"tool_use",
		"usage":{"input_tokens":1,"output_tokens":1}
	}`)
	_, err := parseResponse(body, true, "m", 1)
	if err == nil {
		t.Fatalf("expected error for wrong tool name")
	}
	var lpe *port.LLMProviderError
	if !errors.As(err, &lpe) || lpe.Code != port.LLMErrorServerError {
		t.Fatalf("err=%v want LLMErrorServerError", err)
	}
}

func TestParseResponse_NoTextBlocks_ServerError(t *testing.T) {
	body := []byte(`{
		"content":[{"type":"tool_use","name":"x","input":{"x":1}}],
		"stop_reason":"end_turn",
		"usage":{"input_tokens":1,"output_tokens":1}
	}`)
	_, err := parseResponse(body, false, "m", 1)
	if err == nil {
		t.Fatalf("expected error when no text blocks for text-mode")
	}
	var lpe *port.LLMProviderError
	if !errors.As(err, &lpe) || lpe.Code != port.LLMErrorServerError {
		t.Fatalf("err=%v want LLMErrorServerError", err)
	}
}

func TestParseResponse_InvalidJSON_ServerError(t *testing.T) {
	_, err := parseResponse([]byte("not-json"), false, "m", 1)
	if err == nil {
		t.Fatalf("expected error")
	}
	var lpe *port.LLMProviderError
	if !errors.As(err, &lpe) || lpe.Code != port.LLMErrorServerError {
		t.Fatalf("err=%v want LLMErrorServerError (provider sent corrupt JSON)", err)
	}
}

func TestParseResponse_EmptyModelField_FallsBackToConfig(t *testing.T) {
	body := []byte(`{
		"content":[{"type":"text","text":"ok"}],
		"stop_reason":"end_turn",
		"usage":{"input_tokens":1,"output_tokens":1}
	}`)
	resp, err := parseResponse(body, false, "config-model", 1)
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if resp.Model != "config-model" {
		t.Errorf("Model=%q, want fallback to config-model", resp.Model)
	}
}

func TestMapStopReason_AllVariants(t *testing.T) {
	cases := map[string]port.StopReason{
		"end_turn":      port.StopReasonEndTurn,
		"tool_use":      port.StopReasonEndTurn,
		"max_tokens":    port.StopReasonMaxTokens,
		"stop_sequence": port.StopReasonStopSequence,
		// "refusal" signals Anthropic's content-policy denial. Surface as
		// StopReasonContentFilter so the Router can branch on it (port spec
		// §1.1) — golang-pro review S6.
		"refusal": port.StopReasonContentFilter,
		"unknown": port.StopReasonEndTurn,
		"":        port.StopReasonEndTurn,
	}
	for in, want := range cases {
		if got := mapStopReason(in); got != want {
			t.Errorf("mapStopReason(%q)=%v, want %v", in, got, want)
		}
	}
}

func TestParseResponse_EmptyToolInput_ServerError(t *testing.T) {
	body := []byte(`{
		"content":[{"type":"tool_use","name":"return_analysis_result"}],
		"stop_reason":"tool_use",
		"usage":{"input_tokens":1,"output_tokens":1}
	}`)
	_, err := parseResponse(body, true, "m", 1)
	if err == nil || !strings.Contains(err.Error(), "empty input") {
		t.Fatalf("err=%v, want empty-input error", err)
	}
	var lpe *port.LLMProviderError
	if !errors.As(err, &lpe) || lpe.Code != port.LLMErrorServerError {
		t.Fatalf("err=%v, want LLMErrorServerError (empty tool_use is provider misbehaviour)", err)
	}
}
