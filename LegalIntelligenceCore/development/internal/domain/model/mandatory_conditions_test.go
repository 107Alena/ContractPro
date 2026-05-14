package model

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"
)

func TestMandatoryConditionStatus_IsValid(t *testing.T) {
	for _, s := range []MandatoryConditionStatus{
		MandatoryConditionFoundOK, MandatoryConditionFoundAmbiguous, MandatoryConditionMissing,
	} {
		if !s.IsValid() {
			t.Errorf("%q must be valid", s)
		}
	}
	if MandatoryConditionStatus("UNKNOWN").IsValid() {
		t.Error("UNKNOWN must NOT be valid")
	}
}

func TestIsValidMandatoryConditionCode(t *testing.T) {
	good := []string{"MC_SUPPLY_GOODS", "MC_SERVICES_PRICE", "MC_NDA_TERM", "MC_LICENSE_USE_METHODS", "MC_X1"}
	for _, g := range good {
		if !IsValidMandatoryConditionCode(g) {
			t.Errorf("%q must match regex", g)
		}
	}
	bad := []string{"", "mc_supply", "MC-LOWER", "MCMissingPrefix", "MC_", "MC_supply"}
	for _, b := range bad {
		if IsValidMandatoryConditionCode(b) {
			t.Errorf("%q must NOT match regex", b)
		}
	}
}

func TestMandatoryCondition_NullableFieldsSerialiseAsNull(t *testing.T) {
	// ai-agents-pipeline.md §4 schema declares found_in (`type:["array","null"]`)
	// and issue_description (`type:["string","null"]`) as nullable wire fields.
	c := MandatoryCondition{
		Code: "MC_X1", Label: "x", Status: MandatoryConditionMissing, LegalBasis: "y",
	}
	b, err := json.Marshal(c)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, must := range []string{`"found_in":null`, `"issue_description":null`} {
		if !bytes.Contains(b, []byte(must)) {
			t.Errorf("expected %q in JSON, got %s", must, b)
		}
	}
}

func TestMandatoryConditionsReport_JSONRoundTrip(t *testing.T) {
	issue := "Срок указан как „в разумные сроки\", без конкретики."
	summary := "Из 5 условий: 3 OK, 1 неоднозначное, 1 отсутствует."
	in := MandatoryConditionsReport{
		ContractType: string(ContractTypeSupply),
		Conditions: []MandatoryCondition{
			{
				Code:       "MC_SUPPLY_GOODS",
				Label:      "Предмет",
				Status:     MandatoryConditionFoundOK,
				LegalBasis: "§ 3 гл. 30 ГК РФ",
				FoundIn:    []string{"sec-1.1", "sec-1.2"},
			},
			{
				Code:             "MC_SUPPLY_DELIVERY_TERM",
				Label:            "Срок поставки",
				Status:           MandatoryConditionFoundAmbiguous,
				LegalBasis:       "Ст. 506 ГК РФ",
				FoundIn:          []string{"sec-3.1"},
				IssueDescription: &issue,
			},
		},
		Summary:                 &summary,
		PromptInjectionDetected: false,
	}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got MandatoryConditionsReport
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !reflect.DeepEqual(got, in) {
		t.Fatalf("round-trip mismatch:\n got=%+v\nwant=%+v", got, in)
	}
}
