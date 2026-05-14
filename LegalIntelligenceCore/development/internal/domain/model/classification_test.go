package model

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestClassificationResult_JSONRoundTrip(t *testing.T) {
	rationale := "Признаки услуг: ежемесячная плата за процесс."
	in := ClassificationResult{
		ContractType: ContractTypeServices,
		Confidence:   0.92,
		Alternatives: []ClassificationAlternative{
			{ContractType: ContractTypeWorkContract, Confidence: 0.18},
		},
		Rationale:               &rationale,
		PromptInjectionDetected: false,
	}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(b), `"contract_type":"SERVICES"`) {
		t.Fatalf("expected contract_type SERVICES in JSON, got %s", b)
	}
	if !strings.Contains(string(b), `"rationale":`) {
		t.Fatalf("rationale must be present (non-nil pointer), got %s", b)
	}

	var got ClassificationResult
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !reflect.DeepEqual(got, in) {
		t.Fatalf("round-trip mismatch:\n got=%+v\nwant=%+v", got, in)
	}
}

func TestClassificationResult_OmitsRationaleWhenNil(t *testing.T) {
	in := ClassificationResult{
		ContractType:            ContractTypeOther,
		Confidence:              0.6,
		Alternatives:            []ClassificationAlternative{},
		PromptInjectionDetected: false,
	}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(b), `"rationale"`) {
		t.Fatalf("rationale must be omitted when nil, got %s", b)
	}
	if !strings.Contains(string(b), `"prompt_injection_detected":false`) {
		t.Fatalf("prompt_injection_detected must always be present, got %s", b)
	}
}
