package model

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestReportSectionCode_IsValid(t *testing.T) {
	for _, c := range []ReportSectionCode{
		ReportSectionOverview, ReportSectionKeyParameters, ReportSectionPartyData,
		ReportSectionMandatoryConditions, ReportSectionRisks,
		ReportSectionRecommendationsSummary, ReportSectionWarnings,
	} {
		if !c.IsValid() {
			t.Errorf("%q must be valid", c)
		}
	}
	if ReportSectionCode("OTHER").IsValid() {
		t.Error("OTHER must NOT be valid")
	}
}

func TestSummary_JSONRoundTrip(t *testing.T) {
	in := Summary{Text: "Краткое резюме на простом русском языке."}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got Summary
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got != in {
		t.Fatalf("round-trip mismatch: got %+v, want %+v", got, in)
	}
}

func TestDetailedReport_JSONRoundTrip(t *testing.T) {
	sev := RiskLevelHigh
	clause := "sec-4.5"
	basis := "Ст. 310 ГК РФ"
	link := "R-001"
	rec := "См. Рекомендация к R-001"
	in := DetailedReport{
		Sections: []ReportSection{
			{
				SectionCode: ReportSectionRisks,
				Title:       "Выявленные риски",
				Items: []ReportItem{
					{
						Title:                "Одностороннее изменение цены",
						Content:              "Пункт 4.5 договора...",
						Severity:             &sev,
						ClauseRef:            &clause,
						LegalBasis:           &basis,
						LinkedRiskID:         &link,
						LinkedRecommendation: &rec,
					},
				},
			},
			{SectionCode: ReportSectionWarnings, Title: "Системные предупреждения", Items: []ReportItem{}},
		},
		Warnings: nil,
	}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(b), `"warnings":`) {
		t.Fatalf("warnings must be omitted when nil, got %s", b)
	}
	var got DetailedReport
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !reflect.DeepEqual(got, in) {
		t.Fatalf("round-trip mismatch:\n got=%+v\nwant=%+v", got, in)
	}
}
