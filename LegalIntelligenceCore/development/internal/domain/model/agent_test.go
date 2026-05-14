package model

import "testing"

func TestAgentID_IsValid(t *testing.T) {
	for _, a := range AllAgentIDs() {
		if !a.IsValid() {
			t.Errorf("AllAgentIDs returned %q but IsValid() = false", a)
		}
	}
	for _, bad := range []AgentID{"", "AGENT_UNKNOWN", "agent_type_classifier", "TYPE_CLASSIFIER"} {
		if bad.IsValid() {
			t.Errorf("%q must be invalid", bad)
		}
	}
}

func TestAgentID_WireFormat_LockedConstants(t *testing.T) {
	// 9 agents per ai-agents-pipeline.md §1–9. Wire format is used in
	// Prometheus labels {agent=...} (observability.md §3.3) and OTel span
	// attributes lic.agent.id — drift breaks dashboards.
	expected := []struct {
		id   AgentID
		wire string
	}{
		{AgentTypeClassifier, "AGENT_TYPE_CLASSIFIER"},
		{AgentKeyParams, "AGENT_KEY_PARAMS"},
		{AgentPartyConsistency, "AGENT_PARTY_CONSISTENCY"},
		{AgentMandatoryConditions, "AGENT_MANDATORY_CONDITIONS"},
		{AgentRiskDetection, "AGENT_RISK_DETECTION"},
		{AgentRecommendation, "AGENT_RECOMMENDATION"},
		{AgentSummary, "AGENT_SUMMARY"},
		{AgentDetailedReport, "AGENT_DETAILED_REPORT"},
		{AgentRiskDelta, "AGENT_RISK_DELTA"},
	}
	if len(expected) != 9 {
		t.Fatalf("test invariant: expected 9 agent IDs, table has %d", len(expected))
	}
	if len(AllAgentIDs()) != 9 {
		t.Fatalf("AllAgentIDs() count = %d, want 9", len(AllAgentIDs()))
	}
	for _, tc := range expected {
		if string(tc.id) != tc.wire {
			t.Errorf("agent constant drift: got %q, want %q", string(tc.id), tc.wire)
		}
		if tc.id.String() != tc.wire {
			t.Errorf("%v.String() = %q, want %q", tc.id, tc.id.String(), tc.wire)
		}
	}
}

func TestAllAgentIDs_FreshSlice(t *testing.T) {
	a := AllAgentIDs()
	a[0] = "MUTATED"
	b := AllAgentIDs()
	if b[0] == "MUTATED" {
		t.Error("AllAgentIDs must return a fresh slice on each call")
	}
}

func TestAllAgentIDs_NoDuplicates(t *testing.T) {
	seen := make(map[AgentID]struct{})
	for _, a := range AllAgentIDs() {
		if _, dup := seen[a]; dup {
			t.Errorf("AllAgentIDs contains duplicate %q", a)
		}
		seen[a] = struct{}{}
	}
}
