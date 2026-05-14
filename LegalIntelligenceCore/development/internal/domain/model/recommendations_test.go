package model

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestRecommendations_JSONRoundTrip(t *testing.T) {
	in := Recommendations{
		{
			RiskID:          "R-001",
			OriginalText:    "Поставщик вправе в одностороннем порядке изменять цену",
			RecommendedText: "Цена является фиксированной и изменяется только по соглашению сторон",
			Explanation:     "Устраняет риск произвольного изменения цены, ст. 451 ГК РФ",
		},
		{
			RiskID:          "R-M001",
			OriginalText:    "Условие отсутствует",
			RecommendedText: "Качество товара должно соответствовать ГОСТ.",
			Explanation:     "Защищает Покупателя, ст. 469 ГК РФ",
		},
	}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got Recommendations
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !reflect.DeepEqual(got, in) {
		t.Fatalf("round-trip mismatch:\n got=%+v\nwant=%+v", got, in)
	}
	if !IsValidRiskID(got[0].RiskID) || !IsValidRiskID(got[1].RiskID) {
		t.Fatal("risk_id values must match the merged-output regex")
	}
}
