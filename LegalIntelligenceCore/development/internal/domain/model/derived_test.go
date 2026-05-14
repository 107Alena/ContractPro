package model

import (
	"encoding/json"
	"testing"
)

func TestRiskLevel_IsValid(t *testing.T) {
	valid := []RiskLevel{RiskLevelHigh, RiskLevelMedium, RiskLevelLow}
	for _, l := range valid {
		if !l.IsValid() {
			t.Errorf("%q must be valid", l)
		}
	}
	if RiskLevel("HIGH").IsValid() {
		t.Error("HIGH (upper-case) must NOT be valid")
	}
	if RiskLevel("").IsValid() {
		t.Error("empty string must NOT be valid")
	}
}

func TestAggregateScoreLabel_IsValid(t *testing.T) {
	for _, l := range []AggregateScoreLabel{AggregateScoreLabelLow, AggregateScoreLabelMedium, AggregateScoreLabelHigh} {
		if !l.IsValid() {
			t.Errorf("%q must be valid", l)
		}
	}
	if AggregateScoreLabel("Low").IsValid() {
		t.Error("Low (title-case) must NOT be valid")
	}
}

func TestRiskProfile_JSONRoundTrip(t *testing.T) {
	p := RiskProfile{OverallLevel: RiskLevelHigh, HighCount: 3, MediumCount: 5, LowCount: 2}
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{"overall_level":"high","high_count":3,"medium_count":5,"low_count":2}`
	if string(b) != want {
		t.Fatalf("got %s, want %s", b, want)
	}
	var got RiskProfile
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got != p {
		t.Fatalf("round-trip mismatch: got %+v, want %+v", got, p)
	}
}

func TestAggregateScore_JSONRoundTrip(t *testing.T) {
	a := AggregateScore{Score: 0.65, Label: AggregateScoreLabelMedium}
	b, err := json.Marshal(a)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got AggregateScore
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got != a {
		t.Fatalf("round-trip mismatch: got %+v, want %+v", got, a)
	}
}
