package claude

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"contractpro/legal-intelligence-core/internal/domain/model"
	"contractpro/legal-intelligence-core/internal/domain/port"
)

func TestBuildRequestPayload_NoSchema_NoCache(t *testing.T) {
	req := port.CompletionRequest{
		AgentID:     model.AgentSummary,
		User:        "user-body",
		System:      "sys-body",
		MaxTokens:   100,
		Temperature: 0.0,
	}
	p, err := buildRequestPayload(req, "claude-sonnet-4-6", false)
	if err != nil {
		t.Fatalf("buildRequestPayload err=%v", err)
	}

	if p.Model != "claude-sonnet-4-6" {
		t.Errorf("Model=%q", p.Model)
	}
	if len(p.System) != 1 || p.System[0].Type != "text" || p.System[0].Text != "sys-body" {
		t.Errorf("System=%+v", p.System)
	}
	if p.System[0].CacheControl != nil {
		t.Errorf("CacheControl should be nil when cache disabled, got %+v", p.System[0].CacheControl)
	}
	if len(p.Messages) != 1 || p.Messages[0].Role != "user" || p.Messages[0].Content != "user-body" {
		t.Errorf("Messages=%+v", p.Messages)
	}
	if p.MaxTokens != 100 || p.Temperature != 0.0 {
		t.Errorf("MaxTokens/Temperature wrong: %d / %v", p.MaxTokens, p.Temperature)
	}
	if p.Tools != nil || p.ToolChoice != nil {
		t.Errorf("Tools/ToolChoice should be nil without JSONSchema; got %+v / %+v", p.Tools, p.ToolChoice)
	}
}

func TestBuildRequestPayload_PromptCacheEnabled_EmitsEphemeralMarker(t *testing.T) {
	req := port.CompletionRequest{System: "sys", User: "u", MaxTokens: 10}
	p, err := buildRequestPayload(req, "m", true)
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if p.System[0].CacheControl == nil || p.System[0].CacheControl.Type != "ephemeral" {
		t.Fatalf("expected ephemeral cache_control, got %+v", p.System[0].CacheControl)
	}
}

func TestBuildRequestPayload_EmptySystem_OmitsSystemKey(t *testing.T) {
	req := port.CompletionRequest{User: "u", MaxTokens: 10}
	p, err := buildRequestPayload(req, "m", true) // cache toggle irrelevant when no system
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if p.System != nil {
		t.Fatalf("System should be nil when req.System == \"\", got %+v", p.System)
	}
	b, _ := json.Marshal(p)
	if strings.Contains(string(b), `"system"`) {
		t.Fatalf("marshalled payload contains system key when it should be omitted: %s", b)
	}
}

func TestBuildRequestPayload_PriorTurns_Append(t *testing.T) {
	req := port.CompletionRequest{
		User: "fresh-user",
		PriorTurns: []port.Turn{
			{Role: port.RoleUser, Content: "old-user"},
			{Role: port.RoleAssistant, Content: "old-assistant"},
		},
		MaxTokens: 10,
	}
	p, err := buildRequestPayload(req, "m", false)
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	want := []struct {
		role    string
		content string
	}{
		{"user", "old-user"},
		{"assistant", "old-assistant"},
		{"user", "fresh-user"},
	}
	if len(p.Messages) != len(want) {
		t.Fatalf("len(Messages)=%d, want %d", len(p.Messages), len(want))
	}
	for i, w := range want {
		if p.Messages[i].Role != w.role || p.Messages[i].Content != w.content {
			t.Errorf("msg[%d] = {%s, %s}, want {%s, %s}",
				i, p.Messages[i].Role, p.Messages[i].Content, w.role, w.content)
		}
	}
}

func TestBuildRequestPayload_InvalidPriorTurnRole_ReturnsMalformed(t *testing.T) {
	req := port.CompletionRequest{
		PriorTurns: []port.Turn{{Role: port.Role("system"), Content: "leaked"}},
		User:       "u",
		MaxTokens:  10,
	}
	_, err := buildRequestPayload(req, "m", false)
	if err == nil {
		t.Fatalf("expected error for invalid Turn.Role")
	}
	var lpe *port.LLMProviderError
	if !errors.As(err, &lpe) || lpe.Code != port.LLMErrorMalformedRequest {
		t.Fatalf("err type=%T code=%v, want *LLMProviderError with LLMErrorMalformedRequest", err, lpe)
	}
	if lpe.Retryable || lpe.FallbackEligible {
		t.Errorf("MALFORMED must be Retryable=false, FallbackEligible=false; got %+v", lpe)
	}
}

func TestBuildRequestPayload_WithJSONSchema_EmitsVirtualTool(t *testing.T) {
	schema := json.RawMessage(`{"type":"object","properties":{"x":{"type":"string"}},"required":["x"]}`)
	req := port.CompletionRequest{
		System:     "sys",
		User:       "u",
		MaxTokens:  100,
		JSONSchema: schema,
	}
	p, err := buildRequestPayload(req, "m", false)
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if len(p.Tools) != 1 {
		t.Fatalf("Tools len=%d, want 1", len(p.Tools))
	}
	if p.Tools[0].Name != virtualToolName {
		t.Errorf("tool name=%q, want %q", p.Tools[0].Name, virtualToolName)
	}
	if string(p.Tools[0].InputSchema) != string(schema) {
		t.Errorf("InputSchema does not match: got %s want %s", p.Tools[0].InputSchema, schema)
	}
	if p.ToolChoice == nil || p.ToolChoice.Type != "tool" || p.ToolChoice.Name != virtualToolName {
		t.Fatalf("ToolChoice=%+v, want type=tool name=%s", p.ToolChoice, virtualToolName)
	}
}

func TestBuildRequestPayload_JSONMarshallable_GoldenShape(t *testing.T) {
	req := port.CompletionRequest{
		System: "sys", User: "u", MaxTokens: 100, Temperature: 0,
		StopSequences: []string{"</done>"},
	}
	p, err := buildRequestPayload(req, "claude-sonnet-4-6", true)
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("Marshal err=%v", err)
	}
	s := string(b)
	for _, want := range []string{
		`"model":"claude-sonnet-4-6"`,
		`"system":[{"type":"text","text":"sys","cache_control":{"type":"ephemeral"}}]`,
		`"messages":[{"role":"user","content":"u"}]`,
		`"max_tokens":100`,
		// json.Marshal HTML-escapes <, >, & by default (functionally equivalent
		// after decode on the Anthropic side, but we assert the actual wire form).
		"\"stop_sequences\":[\"\\u003c/done\\u003e\"]",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("payload missing fragment %q in %s", want, s)
		}
	}
}
