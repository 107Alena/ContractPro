package model

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestRiskCategory_IsValid_22Exhaustive(t *testing.T) {
	all := AllRiskCategories()
	if got := len(all); got != 22 {
		t.Fatalf("AllRiskCategories: got %d, want 22 (13 agent-5 + 7 party + 2 mandatory)", got)
	}
	seen := make(map[RiskCategory]struct{}, len(all))
	for _, c := range all {
		if !c.IsValid() {
			t.Errorf("%q reported invalid by IsValid", c)
		}
		if _, dup := seen[c]; dup {
			t.Errorf("duplicate category in AllRiskCategories: %q", c)
		}
		seen[c] = struct{}{}
	}

	// Compile-time-locked spec-named values that downstream tests/aggregator rely on.
	for _, c := range []RiskCategory{
		RiskCategoryUnilateralChange, RiskCategoryPartyAuthorityMissing,
		RiskCategoryMandatoryConditionMissing, RiskCategoryPromptInjectionAttempt,
		RiskCategoryOther,
	} {
		if !c.IsValid() {
			t.Errorf("%q must be valid", c)
		}
	}

	if RiskCategory("UNKNOWN_RISK").IsValid() {
		t.Error("UNKNOWN_RISK must NOT be valid")
	}
}

func TestAllRiskCategories_FreshSliceOnEachCall(t *testing.T) {
	a := AllRiskCategories()
	a[0] = "MUTATED"
	b := AllRiskCategories()
	if b[0] == "MUTATED" {
		t.Fatal("AllRiskCategories must return a fresh slice — caller mutation leaked across calls")
	}
}

func TestIsValidRiskID(t *testing.T) {
	good := []string{"R-001", "R-999", "R-1000", "R-P001", "R-M042"}
	for _, g := range good {
		if !IsValidRiskID(g) {
			t.Errorf("%q must match regex", g)
		}
	}
	bad := []string{"", "R-1", "R-12", "R-PP001", "R-X001", "r-001", "001", "R--001"}
	for _, b := range bad {
		if IsValidRiskID(b) {
			t.Errorf("%q must NOT match regex", b)
		}
	}
}

func TestRiskAnalysis_JSONRoundTrip(t *testing.T) {
	rat := "Нарушает ст. 310 ГК РФ"
	mcCode := "MC_SUPPLY_QUALITY_REQUIREMENTS"
	summary := "Найдено 3 риска"
	in := RiskAnalysis{
		Risks: []Risk{
			{
				ID:          "R-001",
				Level:       RiskLevelHigh,
				Description: "Односторонее изменение цены",
				ClauseRef:   "sec-4.5",
				LegalBasis:  "Ст. 310 ГК РФ",
				Category:    RiskCategoryUnilateralChange,
				Rationale:   &rat,
			},
			{
				ID:                     "R-M001",
				Level:                  RiskLevelHigh,
				Description:            "Отсутствуют требования к качеству",
				ClauseRef:              "—",
				LegalBasis:             "Ст. 469 ГК РФ",
				Category:               RiskCategoryMandatoryConditionMissing,
				MandatoryConditionCode: &mcCode,
			},
		},
		Summary:                 &summary,
		PromptInjectionDetected: false,
	}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got RiskAnalysis
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !reflect.DeepEqual(got, in) {
		t.Fatalf("round-trip mismatch:\n got=%+v\nwant=%+v", got, in)
	}
}
