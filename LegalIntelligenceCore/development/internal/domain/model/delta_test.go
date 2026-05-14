package model

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestRiskDelta_JSONRoundTrip(t *testing.T) {
	oldClause := "sec-4.5"
	newClause := "sec-4.6"
	in := RiskDelta{
		BaseVersionID:   "11111111-1111-1111-1111-111111111111",
		TargetVersionID: "22222222-2222-2222-2222-222222222222",
		Added: []RiskRef{
			{ID: "R-003", Level: RiskLevelMedium, Description: "Новый риск", ClauseRef: "sec-7.1"},
		},
		Removed: []RiskRef{
			{ID: "R-002", Level: RiskLevelHigh, Description: "Исчез после правки", ClauseRef: "sec-3.2"},
		},
		Changed: []RiskChange{
			{
				TargetID: "R-001", BaseID: "R-001",
				OldLevel: RiskLevelHigh, NewLevel: RiskLevelMedium,
				OldClauseRef: &oldClause, NewClauseRef: &newClause,
				Explanation: "Формулировка смягчена",
			},
		},
		ProfileChange: &RiskProfileChange{
			OldOverallLevel: RiskLevelHigh, NewOverallLevel: RiskLevelMedium,
			OldHighCount: 2, NewHighCount: 0,
			OldMediumCount: 1, NewMediumCount: 2,
			OldLowCount: 0, NewLowCount: 1,
		},
		Summary: "Профиль улучшился: high → medium",
	}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, must := range []string{`"base_version_id":`, `"profile_change":`, `"changed":[`, `"old_level":"high"`, `"new_level":"medium"`} {
		if !strings.Contains(string(b), must) {
			t.Errorf("expected %q in JSON, got %s", must, b)
		}
	}
	var got RiskDelta
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !reflect.DeepEqual(got, in) {
		t.Fatalf("round-trip mismatch:\n got=%+v\nwant=%+v", got, in)
	}
}

func TestRiskDelta_OmitProfileChangeWhenNil(t *testing.T) {
	in := RiskDelta{
		BaseVersionID:   "a", TargetVersionID: "b",
		Added: []RiskRef{}, Removed: []RiskRef{}, Changed: []RiskChange{},
		Summary: "no profile change",
	}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(b), `"profile_change":`) {
		t.Fatalf("profile_change must be omitted when nil, got %s", b)
	}
}
