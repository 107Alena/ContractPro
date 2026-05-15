package openai

import (
	"strings"
	"testing"

	"contractpro/legal-intelligence-core/internal/domain/port"
)

func TestParseResponse_HappyPath_OutputText(t *testing.T) {
	body := []byte(`{
		"id":"resp_1","object":"response","model":"gpt-4.1-2025-04-14","status":"completed",
		"output":[{"type":"message","role":"assistant","status":"completed",
			"content":[{"type":"output_text","text":"hello world"}]}],
		"usage":{"input_tokens":50,"output_tokens":7,"input_tokens_details":{"cached_tokens":40}}
	}`)
	resp, err := parseResponse(body, "gpt-4.1", 123)
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if resp.Content != "hello world" {
		t.Errorf("Content=%q", resp.Content)
	}
	if resp.InputTokens != 50 || resp.OutputTokens != 7 {
		t.Errorf("tokens i=%d o=%d", resp.InputTokens, resp.OutputTokens)
	}
	// Acceptance criteria: CachedInputTokens=0 in v1 even though the wire
	// carries input_tokens_details.cached_tokens=40.
	if resp.CachedInputTokens != 0 {
		t.Errorf("CachedInputTokens=%d, want 0 (implicit cache not tracked in v1)", resp.CachedInputTokens)
	}
	if resp.StopReason != port.StopReasonEndTurn {
		t.Errorf("StopReason=%v, want end_turn", resp.StopReason)
	}
	if resp.ProviderID != port.ProviderOpenAI {
		t.Errorf("ProviderID=%v", resp.ProviderID)
	}
	if resp.Model != "gpt-4.1-2025-04-14" {
		t.Errorf("Model=%q, want echoed wire model", resp.Model)
	}
	if resp.LatencyMs != 123 {
		t.Errorf("LatencyMs=%d, want 123", resp.LatencyMs)
	}
}

func TestParseResponse_MultipleOutputTextParts_Concatenated(t *testing.T) {
	body := []byte(`{
		"status":"completed",
		"output":[{"type":"message","role":"assistant",
			"content":[{"type":"output_text","text":"foo"},{"type":"output_text","text":"bar"}]}],
		"usage":{"input_tokens":1,"output_tokens":1}
	}`)
	resp, err := parseResponse(body, "gpt-4.1", 0)
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if resp.Content != "foobar" {
		t.Errorf("Content=%q, want foobar", resp.Content)
	}
}

func TestParseResponse_ReasoningItemsIgnored(t *testing.T) {
	body := []byte(`{
		"status":"completed",
		"output":[
			{"type":"reasoning","id":"rs_1","content":[]},
			{"type":"message","role":"assistant","content":[{"type":"output_text","text":"answer"}]}
		],
		"usage":{"input_tokens":1,"output_tokens":1}
	}`)
	resp, err := parseResponse(body, "gpt-4.1", 0)
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if resp.Content != "answer" {
		t.Errorf("Content=%q, want answer (reasoning item must be ignored)", resp.Content)
	}
}

func TestParseResponse_Refusal_MapsToContentFilter_Success(t *testing.T) {
	// A refusal is a SUCCESSFUL 200/completed body. Per port godoc on
	// StopReasonContentFilter the adapter returns success and the Router maps
	// content_filter → LLMErrorContentPolicy (mirrors Claude refusal path).
	body := []byte(`{
		"status":"completed",
		"output":[{"type":"message","role":"assistant",
			"content":[{"type":"refusal","refusal":"I cannot help with that request."}]}],
		"usage":{"input_tokens":3,"output_tokens":5}
	}`)
	resp, err := parseResponse(body, "gpt-4.1", 0)
	if err != nil {
		t.Fatalf("err=%v, want success (Router converts content_filter)", err)
	}
	if resp.StopReason != port.StopReasonContentFilter {
		t.Errorf("StopReason=%v, want content_filter", resp.StopReason)
	}
	if resp.Content != "I cannot help with that request." {
		t.Errorf("Content=%q, want refusal text surfaced", resp.Content)
	}
}

func TestParseResponse_MixedOutputTextAndRefusal_PrefersRefusal(t *testing.T) {
	// When a message carries BOTH a partial output_text and a refusal part,
	// the refusal string must win (deterministic content-filter event —
	// golang-pro S2), not the partial text.
	body := []byte(`{
		"status":"completed",
		"output":[{"type":"message","role":"assistant","content":[
			{"type":"output_text","text":"partial leak"},
			{"type":"refusal","refusal":"I will not continue."}
		]}],
		"usage":{"input_tokens":2,"output_tokens":3}
	}`)
	resp, err := parseResponse(body, "gpt-4.1", 0)
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if resp.StopReason != port.StopReasonContentFilter {
		t.Errorf("StopReason=%v, want content_filter", resp.StopReason)
	}
	if resp.Content != "I will not continue." {
		t.Errorf("Content=%q, want refusal text (not partial output_text)", resp.Content)
	}
}

func TestParseResponse_StatusFailed_MessageIsBounded(t *testing.T) {
	// A hostile / oversized inline error.message must not bypass the 512-byte
	// truncation guarantee (security-engineer S1). 5000 chars in → the error
	// string must stay bounded.
	huge := strings.Repeat("A", 5000)
	body := []byte(`{"status":"failed","error":{"code":"server_error","message":"` + huge + `"},"output":[]}`)
	_, err := parseResponse(body, "gpt-4.1", 0)
	if err == nil {
		t.Fatalf("expected error")
	}
	if len(err.Error()) > 700 {
		t.Errorf("status=failed error length=%d, want <700 (message truncation broken)", len(err.Error()))
	}
}

func TestParseResponse_Incomplete_MaxOutputTokens(t *testing.T) {
	body := []byte(`{
		"status":"incomplete","incomplete_details":{"reason":"max_output_tokens"},
		"output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"partial"}]}],
		"usage":{"input_tokens":1,"output_tokens":16}
	}`)
	resp, err := parseResponse(body, "gpt-4.1", 0)
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if resp.StopReason != port.StopReasonMaxTokens {
		t.Errorf("StopReason=%v, want max_tokens", resp.StopReason)
	}
	if resp.Content != "partial" {
		t.Errorf("Content=%q, want partial", resp.Content)
	}
}

func TestParseResponse_Incomplete_ContentFilter_EmptyContent_Success(t *testing.T) {
	// content_filter with no message output: still a successful response with
	// StopReasonContentFilter — the empty-output SERVER_ERROR guard must NOT
	// fire on the content-filter path (Router maps it to a policy error).
	body := []byte(`{
		"status":"incomplete","incomplete_details":{"reason":"content_filter"},
		"output":[],
		"usage":{"input_tokens":1,"output_tokens":0}
	}`)
	resp, err := parseResponse(body, "gpt-4.1", 0)
	if err != nil {
		t.Fatalf("err=%v, want success on content_filter path", err)
	}
	if resp.StopReason != port.StopReasonContentFilter {
		t.Errorf("StopReason=%v, want content_filter", resp.StopReason)
	}
}

func TestParseResponse_StatusFailed_ServerError(t *testing.T) {
	body := []byte(`{
		"status":"failed","error":{"code":"server_error","message":"internal failure"},
		"output":[],"usage":{"input_tokens":0,"output_tokens":0}
	}`)
	_, err := parseResponse(body, "gpt-4.1", 0)
	lpe, ok := port.AsLLMProviderError(err)
	if !ok {
		t.Fatalf("err=%T, want *LLMProviderError", err)
	}
	if lpe.Code != port.LLMErrorServerError {
		t.Errorf("Code=%v, want LLMErrorServerError (preserves Router fallback)", lpe.Code)
	}
}

func TestParseResponse_InvalidJSON_ServerError(t *testing.T) {
	_, err := parseResponse([]byte("not-json"), "gpt-4.1", 0)
	lpe, ok := port.AsLLMProviderError(err)
	if !ok {
		t.Fatalf("err=%T, want *LLMProviderError", err)
	}
	if lpe.Code != port.LLMErrorServerError {
		t.Errorf("Code=%v, want LLMErrorServerError (corrupt 2xx = provider misbehaviour, not MALFORMED)", lpe.Code)
	}
}

func TestParseResponse_NoOutputText_ServerError(t *testing.T) {
	body := []byte(`{"status":"completed","output":[],"usage":{"input_tokens":1,"output_tokens":0}}`)
	_, err := parseResponse(body, "gpt-4.1", 0)
	lpe, ok := port.AsLLMProviderError(err)
	if !ok {
		t.Fatalf("err=%T, want *LLMProviderError", err)
	}
	if lpe.Code != port.LLMErrorServerError {
		t.Errorf("Code=%v, want LLMErrorServerError", lpe.Code)
	}
}

func TestParseResponse_WhitespaceOnlyOutput_ServerError(t *testing.T) {
	body := []byte(`{
		"status":"completed",
		"output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"   \n\t"}]}],
		"usage":{"input_tokens":1,"output_tokens":1}
	}`)
	_, err := parseResponse(body, "gpt-4.1", 0)
	lpe, ok := port.AsLLMProviderError(err)
	if !ok || lpe.Code != port.LLMErrorServerError {
		t.Fatalf("err=%v, want LLMErrorServerError for whitespace-only output", err)
	}
}

func TestParseResponse_EmptyModelField_FallsBackToConfig(t *testing.T) {
	body := []byte(`{
		"status":"completed",
		"output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"ok"}]}],
		"usage":{"input_tokens":1,"output_tokens":1}
	}`)
	resp, err := parseResponse(body, "gpt-4.1-fallback", 0)
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if resp.Model != "gpt-4.1-fallback" {
		t.Errorf("Model=%q, want config fallback when wire model empty", resp.Model)
	}
}

func TestMapStopReason_AllVariants(t *testing.T) {
	cases := []struct {
		name    string
		status  string
		inc     *responsesIncompleteDet
		refused bool
		want    port.StopReason
	}{
		{"completed", "completed", nil, false, port.StopReasonEndTurn},
		{"refused_completed", "completed", nil, true, port.StopReasonContentFilter},
		{"incomplete_maxtok", "incomplete", &responsesIncompleteDet{Reason: "max_output_tokens"}, false, port.StopReasonMaxTokens},
		{"incomplete_filter", "incomplete", &responsesIncompleteDet{Reason: "content_filter"}, false, port.StopReasonContentFilter},
		{"incomplete_unknown", "incomplete", &responsesIncompleteDet{Reason: "weird"}, false, port.StopReasonEndTurn},
		{"incomplete_nil_details", "incomplete", nil, false, port.StopReasonEndTurn},
		{"unknown_status", "queued", nil, false, port.StopReasonEndTurn},
		{"refusal_overrides_status", "incomplete", &responsesIncompleteDet{Reason: "max_output_tokens"}, true, port.StopReasonContentFilter},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := mapStopReason(tc.status, tc.inc, tc.refused); got != tc.want {
				t.Errorf("mapStopReason(%q,%+v,%v)=%v, want %v", tc.status, tc.inc, tc.refused, got, tc.want)
			}
		})
	}
}

func TestMapStopReason_NeverEmitsStopSequence(t *testing.T) {
	// The Responses API has no stop parameter, so the adapter must never
	// produce StopReasonStopSequence (documented deviation from Claude).
	for _, status := range []string{"completed", "incomplete", "failed", "queued"} {
		for _, reason := range []string{"", "max_output_tokens", "content_filter", "stop_sequence"} {
			got := mapStopReason(status, &responsesIncompleteDet{Reason: reason}, false)
			if got == port.StopReasonStopSequence {
				t.Errorf("mapStopReason(%q,%q) emitted StopReasonStopSequence", status, reason)
			}
		}
	}
}
