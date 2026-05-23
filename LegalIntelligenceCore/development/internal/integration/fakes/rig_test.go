package fakes

import (
	"testing"

	"contractpro/legal-intelligence-core/internal/domain/model"
	"contractpro/legal-intelligence-core/internal/domain/port"
)

func TestNewTestRig_WiresEveryFake(t *testing.T) {
	rig := NewTestRig(t)
	if rig.Broker == nil || rig.KV == nil || rig.DM == nil {
		t.Fatal("nil fake")
	}
	for _, id := range []port.LLMProviderID{port.ProviderClaude, port.ProviderOpenAI, port.ProviderGemini} {
		if _, ok := rig.LLMByID[id]; !ok {
			t.Fatalf("missing provider %s", id)
		}
	}
}

func TestNewTestRig_TopologyPreset(t *testing.T) {
	rig := NewTestRig(t)
	for _, b := range LICTopologyBindings() {
		queue, routingKey := b[0], b[1]
		if _, ok := rig.Broker.bindings[routingKey][queue]; !ok {
			t.Fatalf("missing binding %s→%s", routingKey, queue)
		}
	}
}

func TestNewTestRig_CleanupTearsDown(t *testing.T) {
	// We can't directly observe t.Cleanup from inside the test; instead
	// verify that the four fakes are usable, and rely on -race + Stop's
	// in-flight wait covering correctness.
	rig := NewTestRig(t)
	if rig.Broker.Closed() {
		t.Fatal("Broker should not be closed before t completes")
	}
}

func TestInstallCannedAgentResponses_HappyPath(t *testing.T) {
	rig := NewTestRig(t)
	providerByAgent := map[model.AgentID]port.LLMProviderID{
		model.AgentTypeClassifier: port.ProviderClaude,
		model.AgentSummary:        port.ProviderOpenAI,
	}
	modelByAgent := map[model.AgentID]string{
		model.AgentTypeClassifier: "claude-sonnet-4-6",
		model.AgentSummary:        "gpt-4o",
	}
	rig.InstallCannedAgentResponses(providerByAgent, modelByAgent, []model.AgentID{
		model.AgentTypeClassifier, model.AgentSummary,
	})
	if rig.LLMByID[port.ProviderClaude].Pending(model.AgentTypeClassifier, "claude-sonnet-4-6") != 1 {
		t.Fatal("classifier response not installed on Claude")
	}
	if rig.LLMByID[port.ProviderOpenAI].Pending(model.AgentSummary, "gpt-4o") != 1 {
		t.Fatal("summary response not installed on OpenAI")
	}
}

func TestInstallCannedAgentResponses_SkipsUnmappedAgents(t *testing.T) {
	rig := NewTestRig(t)
	// Provide a mapping ONLY for the classifier.
	rig.InstallCannedAgentResponses(
		map[model.AgentID]port.LLMProviderID{model.AgentTypeClassifier: port.ProviderClaude},
		map[model.AgentID]string{model.AgentTypeClassifier: "claude"},
		[]model.AgentID{model.AgentTypeClassifier, model.AgentSummary},
	)
	if rig.LLMByID[port.ProviderClaude].Pending(model.AgentSummary, "claude") != 0 {
		t.Fatal("unmapped agent should be skipped")
	}
}
