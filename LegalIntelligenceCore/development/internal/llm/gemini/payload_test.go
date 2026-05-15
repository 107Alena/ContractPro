package gemini

import (
	"encoding/json"
	"testing"

	"contractpro/legal-intelligence-core/internal/domain/port"
)

func TestBuildRequestPayload_NoSystem_OmitsSystemInstruction(t *testing.T) {
	p, err := buildRequestPayload(port.CompletionRequest{User: "u", MaxTokens: 10})
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if p.SystemInstruction != nil {
		t.Errorf("SystemInstruction=%+v, want nil when System empty (MUST-FIX #5)", p.SystemInstruction)
	}
	// Confirm it is dropped on the wire, not serialised as null/empty parts.
	b, _ := json.Marshal(p)
	if got := string(b); contains(got, "systemInstruction") {
		t.Errorf("marshalled payload still contains systemInstruction: %s", got)
	}
}

func TestBuildRequestPayload_RoleMapping(t *testing.T) {
	p, err := buildRequestPayload(port.CompletionRequest{
		User: "now",
		PriorTurns: []port.Turn{
			{Role: port.RoleUser, Content: "a"},
			{Role: port.RoleAssistant, Content: "b"},
		},
		MaxTokens: 10,
	})
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	want := []string{"user", "model", "user"}
	if len(p.Contents) != 3 {
		t.Fatalf("contents len=%d, want 3", len(p.Contents))
	}
	for i, w := range want {
		if p.Contents[i].Role != w {
			t.Errorf("Contents[%d].Role=%q, want %q", i, p.Contents[i].Role, w)
		}
	}
}

func TestBuildRequestPayload_InvalidPriorTurnRole_Malformed(t *testing.T) {
	_, err := buildRequestPayload(port.CompletionRequest{
		User:       "u",
		MaxTokens:  10,
		PriorTurns: []port.Turn{{Role: port.Role("system"), Content: "x"}},
	})
	lpe, ok := port.AsLLMProviderError(err)
	if !ok || lpe.Code != port.LLMErrorMalformedRequest {
		t.Fatalf("err=%v, want LLMErrorMalformedRequest", err)
	}
}

func TestBuildRequestPayload_StopSequencesForwarded(t *testing.T) {
	p, err := buildRequestPayload(port.CompletionRequest{
		User: "u", MaxTokens: 10, StopSequences: []string{"END"},
	})
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if len(p.GenerationConfig.StopSequences) != 1 || p.GenerationConfig.StopSequences[0] != "END" {
		t.Errorf("StopSequences=%v, want [END] (Gemini supports stopSequences — Q5)", p.GenerationConfig.StopSequences)
	}
}

func TestBuildRequestPayload_JSONModeOnly_NoSchema(t *testing.T) {
	p, err := buildRequestPayload(port.CompletionRequest{User: "u", MaxTokens: 10, JSONMode: true})
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if p.GenerationConfig.ResponseMimeType != "application/json" {
		t.Errorf("ResponseMimeType=%q", p.GenerationConfig.ResponseMimeType)
	}
	if len(p.GenerationConfig.ResponseSchema) != 0 {
		t.Errorf("ResponseSchema must be empty for JSONMode-only, got %s", p.GenerationConfig.ResponseSchema)
	}
}

func TestBuildRequestPayload_JSONSchema_TransformError_Malformed(t *testing.T) {
	_, err := buildRequestPayload(port.CompletionRequest{
		User: "u", MaxTokens: 10,
		JSONSchema: json.RawMessage(`{"type":"object","properties":{"x":{"$ref":"#/$defs/Missing"}}}`),
	})
	lpe, ok := port.AsLLMProviderError(err)
	if !ok || lpe.Code != port.LLMErrorMalformedRequest {
		t.Fatalf("err=%v, want LLMErrorMalformedRequest for un-resolvable $ref", err)
	}
}

func TestWireRole(t *testing.T) {
	if wireRole(port.RoleAssistant) != "model" {
		t.Errorf("RoleAssistant→%q, want model", wireRole(port.RoleAssistant))
	}
	if wireRole(port.RoleUser) != "user" {
		t.Errorf("RoleUser→%q, want user", wireRole(port.RoleUser))
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
