package model

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestPipelineMode_IsValid(t *testing.T) {
	if !PipelineModeInitial.IsValid() || !PipelineModeReCheck.IsValid() {
		t.Fatal("known modes must be valid")
	}
	if PipelineMode("INVALID").IsValid() {
		t.Fatal("INVALID must NOT be valid")
	}
}

func TestNewPipelineState_DefaultsAreSet(t *testing.T) {
	before := time.Now().UTC()
	s := NewPipelineState("c1", "j1", "d1", "v1", "o1")
	after := time.Now().UTC()

	if s.CorrelationID != "c1" || s.JobID != "j1" || s.DocumentID != "d1" ||
		s.VersionID != "v1" || s.OrganizationID != "o1" {
		t.Fatalf("identifying fields not propagated: %+v", s)
	}
	if s.Mode != PipelineModeInitial {
		t.Errorf("default Mode: got %q, want INITIAL", s.Mode)
	}
	if s.CurrentStage != StageReceived {
		t.Errorf("default CurrentStage: got %q, want STAGE_RECEIVED", s.CurrentStage)
	}
	if s.StartedAt.IsZero() {
		t.Fatal("StartedAt must be set")
	}
	if s.StartedAt.Before(before) || s.StartedAt.After(after) {
		t.Errorf("StartedAt %v not between %v and %v", s.StartedAt, before, after)
	}
	if s.StartedAt.Location() != time.UTC {
		t.Errorf("StartedAt must be UTC, got %v", s.StartedAt.Location())
	}
}

func TestPipelineState_JSONRoundTripWithAgentOutputs(t *testing.T) {
	parent := "parent-version-uuid"
	s := &PipelineState{
		CorrelationID:   "c",
		JobID:           "j",
		DocumentID:      "d",
		VersionID:       "v",
		OrganizationID:  "o",
		CreatedByUserID: "u",
		Mode:            PipelineModeReCheck,
		OriginType:      "RE_UPLOAD",
		ParentVersionID: &parent,
		CurrentStage:    StageAgentRiskDetection,
		StartedAt:       time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC),
		TraceContext:    TraceContext{TraceParent: "00-aaa-bbb-01"},
		Classification: &ClassificationResult{
			ContractType:            ContractTypeSupply,
			Confidence:              0.92,
			Alternatives:            []ClassificationAlternative{},
			PromptInjectionDetected: false,
		},
		RiskProfile: &RiskProfile{OverallLevel: RiskLevelHigh, HighCount: 1},
	}

	b, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, must := range []string{
		`"mode":"RE_CHECK"`,
		`"origin_type":"RE_UPLOAD"`,
		`"parent_version_id":"parent-version-uuid"`,
		`"current_stage":"STAGE_AGENT_RISK_DETECTION"`,
		`"trace_context":{"traceparent":"00-aaa-bbb-01"}`,
		`"classification_result":{`,
		`"risk_profile":{`,
	} {
		if !strings.Contains(string(b), must) {
			t.Errorf("expected %q in JSON, got %s", must, b)
		}
	}

	var got PipelineState
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Mode != s.Mode || got.OriginType != s.OriginType ||
		got.ParentVersionID == nil || *got.ParentVersionID != parent {
		t.Fatalf("round-trip mismatch: got %+v", got)
	}
	if got.Classification == nil || got.Classification.ContractType != ContractTypeSupply {
		t.Fatal("Classification not preserved")
	}
	if got.RiskProfile == nil || got.RiskProfile.HighCount != 1 {
		t.Fatal("RiskProfile not preserved")
	}
}

func TestPipelineState_OmitsNilAgentOutputs(t *testing.T) {
	s := NewPipelineState("c", "j", "d", "v", "o")
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, mustNot := range []string{
		`"classification_result":`, `"key_parameters":`,
		`"party_consistency_findings":`, `"mandatory_conditions_report":`,
		`"risk_analysis":`, `"merged_risk_analysis":`, `"recommendations":`,
		`"summary":`, `"detailed_report":`, `"risk_delta":`,
		`"parent_risk_analysis":`, `"risk_profile":`, `"aggregate_score":`,
		`"parent_version_id":`,
	} {
		if strings.Contains(string(b), mustNot) {
			t.Errorf("unset %q must be omitted, got %s", mustNot, b)
		}
	}
}
