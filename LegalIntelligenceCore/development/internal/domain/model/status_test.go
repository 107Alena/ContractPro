package model

import "testing"

func TestExternalStatus_IsValid(t *testing.T) {
	for _, s := range AllExternalStatuses() {
		if !s.IsValid() {
			t.Errorf("AllExternalStatuses returned %q but IsValid() = false", s)
		}
	}
	if ExternalStatus("QUEUED").IsValid() {
		t.Error("QUEUED is part of the system-wide enum but must NOT be published by LIC")
	}
	if ExternalStatus("").IsValid() {
		t.Error("empty ExternalStatus must be invalid")
	}
	if ExternalStatus("in_progress").IsValid() {
		t.Error("ExternalStatus must be case-sensitive (lowercase rejected)")
	}
}

func TestExternalStatus_WireFormat(t *testing.T) {
	want := map[ExternalStatus]string{
		StatusInProgress: "IN_PROGRESS",
		StatusCompleted:  "COMPLETED",
		StatusFailed:     "FAILED",
	}
	for s, expected := range want {
		if s.String() != expected {
			t.Errorf("%v.String() = %q, want %q", s, s.String(), expected)
		}
		if string(s) != expected {
			t.Errorf("ExternalStatus underlying string = %q, want %q (wire-format lock)", string(s), expected)
		}
	}
}

func TestAllExternalStatuses_ReturnsFreshSlice(t *testing.T) {
	a := AllExternalStatuses()
	b := AllExternalStatuses()
	if &a[0] == &b[0] {
		t.Error("AllExternalStatuses must return a fresh slice — mutation would leak")
	}
	a[0] = "MUTATED"
	c := AllExternalStatuses()
	if c[0] != StatusInProgress {
		t.Errorf("caller mutation leaked into subsequent call: got %q", c[0])
	}
}

func TestStage_IsValid(t *testing.T) {
	for _, s := range AllStages() {
		if !s.IsValid() {
			t.Errorf("AllStages returned %q but IsValid() = false", s)
		}
	}
	if Stage("STAGE_UNKNOWN").IsValid() {
		t.Error("STAGE_UNKNOWN must be invalid")
	}
	if Stage("").IsValid() {
		t.Error("empty Stage must be invalid")
	}
}

func TestStage_WireFormat_LockedConstants(t *testing.T) {
	// Wire-format strings are referenced in logs, Prometheus labels and OTel
	// span attributes. Drifting any of these silently breaks dashboards —
	// this table locks them in.
	expected := []struct {
		stage Stage
		wire  string
	}{
		{StageReceived, "STAGE_RECEIVED"},
		{StageRequestingArtifacts, "STAGE_REQUESTING_ARTIFACTS"},
		{StageArtifactsReceived, "STAGE_ARTIFACTS_RECEIVED"},
		{StageAgentTypeClassifier, "STAGE_AGENT_TYPE_CLASSIFIER"},
		{StageAgentKeyParams, "STAGE_AGENT_KEY_PARAMS"},
		{StageAwaitingUserConfirmation, "STAGE_AWAITING_USER_CONFIRMATION"},
		{StageAgentPartyConsistency, "STAGE_AGENT_PARTY_CONSISTENCY"},
		{StageAgentMandatoryConditions, "STAGE_AGENT_MANDATORY_CONDITIONS"},
		{StageAgentRiskDetection, "STAGE_AGENT_RISK_DETECTION"},
		{StageAgentRecommendation, "STAGE_AGENT_RECOMMENDATION"},
		{StageAgentSummary, "STAGE_AGENT_SUMMARY"},
		{StageAgentDetailedReport, "STAGE_AGENT_DETAILED_REPORT"},
		{StageAgentRiskDelta, "STAGE_AGENT_RISK_DELTA"},
		{StageRiskProfileCalc, "STAGE_RISK_PROFILE_CALC"},
		{StageAggregateScoreCalc, "STAGE_AGGREGATE_SCORE_CALC"},
		{StagePublishingArtifacts, "STAGE_PUBLISHING_ARTIFACTS"},
		{StageAwaitingDMConfirmation, "STAGE_AWAITING_DM_CONFIRMATION"},
		{StageDone, "STAGE_DONE"},
	}
	for _, tc := range expected {
		if string(tc.stage) != tc.wire {
			t.Errorf("stage constant drift: got %q, want %q", string(tc.stage), tc.wire)
		}
	}
	if len(expected) != len(AllStages()) {
		t.Errorf("AllStages count drift: expected lock table has %d entries, AllStages() returned %d",
			len(expected), len(AllStages()))
	}
}

func TestAllStages_ContainsExpectedCount(t *testing.T) {
	// high-architecture.md §2.1.3 enumerates 18 stages. Drifting this count
	// without updating the spec is a contract regression.
	const expectedCount = 18
	got := AllStages()
	if len(got) != expectedCount {
		t.Errorf("AllStages count = %d, want %d (high-architecture.md §2.1.3)", len(got), expectedCount)
	}
	seen := make(map[Stage]struct{}, len(got))
	for _, s := range got {
		if _, dup := seen[s]; dup {
			t.Errorf("AllStages contains duplicate %q", s)
		}
		seen[s] = struct{}{}
	}
}

func TestAllStages_FreshSlice(t *testing.T) {
	a := AllStages()
	a[0] = "MUTATED"
	b := AllStages()
	if b[0] == "MUTATED" {
		t.Error("AllStages must return a fresh slice on each call")
	}
}
