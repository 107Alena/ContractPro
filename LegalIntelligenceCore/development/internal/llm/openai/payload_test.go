package openai

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"

	"contractpro/legal-intelligence-core/internal/domain/port"
)

func TestSchemaName_MatchesOpenAICharset(t *testing.T) {
	// OpenAI constrains text.format.name to ^[a-zA-Z0-9_-]+$. A regression
	// renaming the constant to an invalid value would be rejected at the wire
	// with a 400 — catch it here instead.
	re := regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)
	if !re.MatchString(schemaName) {
		t.Fatalf("schemaName %q violates OpenAI's ^[a-zA-Z0-9_-]+$ charset", schemaName)
	}
}

func TestBuildRequestPayload_NoSchema_NoJSONMode_OmitsText(t *testing.T) {
	p, err := buildRequestPayload(port.CompletionRequest{
		System:    "sys",
		User:      "u",
		MaxTokens: 100,
	}, "gpt-4.1")
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if p.Text != nil {
		t.Errorf("Text=%+v, want nil (free-form, no JSON requested)", p.Text)
	}
	if p.MaxOutputTokens != 100 {
		t.Errorf("MaxOutputTokens=%d, want 100", p.MaxOutputTokens)
	}
}

func TestBuildRequestPayload_SystemBecomesLeadingDeveloperMessage(t *testing.T) {
	p, err := buildRequestPayload(port.CompletionRequest{
		System: "you are a classifier",
		User:   "classify this",
	}, "gpt-4.1")
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if len(p.Input) != 2 {
		t.Fatalf("Input len=%d, want 2 (developer + user)", len(p.Input))
	}
	if p.Input[0].Role != "developer" || p.Input[0].Content != "you are a classifier" {
		t.Errorf("Input[0]=%+v, want developer/system", p.Input[0])
	}
	if p.Input[1].Role != "user" || p.Input[1].Content != "classify this" {
		t.Errorf("Input[1]=%+v, want user", p.Input[1])
	}
}

func TestBuildRequestPayload_EmptySystem_OmitsDeveloperMessage(t *testing.T) {
	p, err := buildRequestPayload(port.CompletionRequest{
		User: "ping",
	}, "gpt-4.1")
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if len(p.Input) != 1 {
		t.Fatalf("Input len=%d, want 1 (user only — no System)", len(p.Input))
	}
	if p.Input[0].Role != "user" {
		t.Errorf("Input[0].Role=%q, want user", p.Input[0].Role)
	}
}

func TestBuildRequestPayload_PriorTurns_OrderedBetweenSystemAndUser(t *testing.T) {
	p, err := buildRequestPayload(port.CompletionRequest{
		System: "sys",
		User:   "repair please",
		PriorTurns: []port.Turn{
			{Role: port.RoleUser, Content: "original"},
			{Role: port.RoleAssistant, Content: "invalid-json"},
		},
	}, "gpt-4.1")
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	wantRoles := []string{"developer", "user", "assistant", "user"}
	if len(p.Input) != len(wantRoles) {
		t.Fatalf("Input len=%d, want %d", len(p.Input), len(wantRoles))
	}
	for i, want := range wantRoles {
		if p.Input[i].Role != want {
			t.Errorf("Input[%d].Role=%q, want %q", i, p.Input[i].Role, want)
		}
	}
	if p.Input[3].Content != "repair please" {
		t.Errorf("final user content=%q, want repair prompt", p.Input[3].Content)
	}
}

func TestBuildRequestPayload_InvalidPriorTurnRole_ReturnsMalformed(t *testing.T) {
	_, err := buildRequestPayload(port.CompletionRequest{
		User:       "u",
		PriorTurns: []port.Turn{{Role: port.Role("system"), Content: "x"}},
	}, "gpt-4.1")
	lpe, ok := port.AsLLMProviderError(err)
	if !ok {
		t.Fatalf("err=%T, want *LLMProviderError", err)
	}
	if lpe.Code != port.LLMErrorMalformedRequest {
		t.Errorf("Code=%v, want LLMErrorMalformedRequest", lpe.Code)
	}
}

func TestBuildRequestPayload_WithJSONSchema_EmitsFlattenedTextFormat(t *testing.T) {
	schema := json.RawMessage(`{"type":"object","properties":{"contract_type":{"type":"string"}},"required":["contract_type"],"additionalProperties":false}`)
	p, err := buildRequestPayload(port.CompletionRequest{
		User:       "classify",
		JSONSchema: schema,
	}, "gpt-4.1")
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if p.Text == nil {
		t.Fatalf("Text is nil; want json_schema format")
	}
	f := p.Text.Format
	if f.Type != "json_schema" {
		t.Errorf("format.type=%q, want json_schema", f.Type)
	}
	if f.Name != schemaName {
		t.Errorf("format.name=%q, want %q", f.Name, schemaName)
	}
	if !f.Strict {
		t.Errorf("format.strict=false, want true (acceptance criteria: strict structured outputs)")
	}
	if string(f.Schema) != string(schema) {
		t.Errorf("format.schema mutated: got %s want %s", f.Schema, schema)
	}

	// The wire shape MUST be the Responses-API flattened text.format, NOT the
	// Chat Completions response_format.json_schema. Marshal and assert.
	raw, _ := json.Marshal(p)
	s := string(raw)
	if !strings.Contains(s, `"text":{"format":{"type":"json_schema"`) {
		t.Errorf("payload missing flattened text.format: %s", s)
	}
	if strings.Contains(s, "response_format") {
		t.Errorf("payload uses Chat-Completions response_format (wrong for /v1/responses): %s", s)
	}
	if strings.Contains(s, `"json_schema":{`) {
		t.Errorf("schema is nested under json_schema key (Chat Completions shape); want flattened: %s", s)
	}
}

func TestBuildRequestPayload_JSONModeWithoutSchema_EmitsJSONObject(t *testing.T) {
	p, err := buildRequestPayload(port.CompletionRequest{
		User:     "give me json",
		JSONMode: true,
	}, "gpt-4.1")
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if p.Text == nil || p.Text.Format.Type != "json_object" {
		t.Fatalf("Text.Format=%+v, want json_object", p.Text)
	}
	raw, _ := json.Marshal(p)
	if !strings.Contains(string(raw), `"text":{"format":{"type":"json_object"}}`) {
		t.Errorf("json_object format not minimal: %s", raw)
	}
}

func TestBuildRequestPayload_SchemaWins_OverJSONMode(t *testing.T) {
	p, err := buildRequestPayload(port.CompletionRequest{
		User:       "u",
		JSONMode:   true,
		JSONSchema: json.RawMessage(`{"type":"object"}`),
	}, "gpt-4.1")
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if p.Text.Format.Type != "json_schema" {
		t.Errorf("format.type=%q, want json_schema (schema must win over JSONMode)", p.Text.Format.Type)
	}
}

func TestBuildRequestPayload_NoStopSequencesField(t *testing.T) {
	// The Responses API has no stop parameter; StopSequences from the port
	// contract must be silently ignored (documented deviation).
	p, err := buildRequestPayload(port.CompletionRequest{
		User:          "u",
		StopSequences: []string{"\n\n", "STOP"},
	}, "gpt-4.1")
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	raw, _ := json.Marshal(p)
	if strings.Contains(string(raw), "stop") {
		t.Errorf("payload leaked a stop field; Responses API has none: %s", raw)
	}
}

func TestBuildRequestPayload_JSONMarshallable_GoldenShape(t *testing.T) {
	p, err := buildRequestPayload(port.CompletionRequest{
		System:      "S",
		User:        "U",
		MaxTokens:   42,
		Temperature: 0.7,
		JSONSchema:  json.RawMessage(`{"type":"object"}`),
	}, "gpt-4.1")
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal err=%v", err)
	}
	want := `{"model":"gpt-4.1","input":[{"role":"developer","content":"S"},{"role":"user","content":"U"}],"max_output_tokens":42,"temperature":0.7,"text":{"format":{"type":"json_schema","name":"return_analysis_result","strict":true,"schema":{"type":"object"}}}}`
	if string(raw) != want {
		t.Errorf("golden shape mismatch:\n got  %s\n want %s", raw, want)
	}
}
