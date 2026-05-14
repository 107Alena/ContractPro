package model

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestPartyFindingType_IsValid(t *testing.T) {
	for _, ft := range []PartyFindingType{
		PartyFindingDataInvalid, PartyFindingNameMismatch, PartyFindingAddressInconsistent,
		PartyFindingAuthorityMissing, PartyFindingINNInvalidChecksum,
		PartyFindingOGRNInvalidChecksum, PartyFindingOGRNINNMismatch,
	} {
		if !ft.IsValid() {
			t.Errorf("%q must be valid", ft)
		}
	}
	if PartyFindingType("PARTY_UNKNOWN").IsValid() {
		t.Error("PARTY_UNKNOWN must NOT be valid")
	}
}

func TestPartyConsistencyFindings_JSONRoundTrip(t *testing.T) {
	summary := "Одна проблема с полномочиями подписанта"
	in := PartyConsistencyFindings{
		Findings: []PartyFinding{
			{
				Type:        PartyFindingAuthorityMissing,
				Severity:    RiskLevelHigh,
				Description: "Не указаны полномочия подписанта стороны 1",
				PartyName:   strPtr("ООО „Альфа\""),
				ClauseRef:   "sec-7.1",
				LegalBasis:  strPtr("Ст. 53 ГК РФ"),
			},
		},
		Summary:                 &summary,
		PromptInjectionDetected: false,
	}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got PartyConsistencyFindings
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !reflect.DeepEqual(got, in) {
		t.Fatalf("round-trip mismatch:\n got=%+v\nwant=%+v", got, in)
	}
}
