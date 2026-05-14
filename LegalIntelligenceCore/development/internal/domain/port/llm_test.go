package port

import (
	"context"
	"testing"
)

type fakeLLMProvider struct{}

func (fakeLLMProvider) ID() LLMProviderID { return ProviderClaude }
func (fakeLLMProvider) Complete(context.Context, CompletionRequest) (CompletionResponse, error) {
	return CompletionResponse{}, nil
}
func (fakeLLMProvider) HealthCheck(context.Context) (*LLMProviderError, error) {
	return nil, nil
}

var _ LLMProviderPort = (*fakeLLMProvider)(nil)

func TestLLMProviderID_IsKnown(t *testing.T) {
	t.Parallel()
	for _, p := range []LLMProviderID{ProviderClaude, ProviderOpenAI, ProviderGemini} {
		if !p.IsKnown() {
			t.Errorf("declared provider %q reports IsKnown()=false", p)
		}
	}
	for _, p := range []LLMProviderID{"", "anthropic", "fake"} {
		if p.IsKnown() {
			t.Errorf("undeclared provider %q reports IsKnown()=true", p)
		}
	}
}

func TestRole_IsValid(t *testing.T) {
	t.Parallel()
	if !RoleUser.IsValid() || !RoleAssistant.IsValid() {
		t.Fatal("declared roles must report IsValid()=true")
	}
	for _, r := range []Role{"", "system", "model"} {
		if r.IsValid() {
			t.Errorf("undeclared role %q reports IsValid()=true (system/model are adapter-internal)", r)
		}
	}
}

func TestStopReason_IsValid(t *testing.T) {
	t.Parallel()
	for _, s := range []StopReason{
		StopReasonEndTurn, StopReasonMaxTokens,
		StopReasonStopSequence, StopReasonContentFilter,
	} {
		if !s.IsValid() {
			t.Errorf("declared stop reason %q reports IsValid()=false", s)
		}
	}
	for _, s := range []StopReason{"", "tool_use", "end"} {
		if s.IsValid() {
			t.Errorf("undeclared stop reason %q reports IsValid()=true", s)
		}
	}
}
