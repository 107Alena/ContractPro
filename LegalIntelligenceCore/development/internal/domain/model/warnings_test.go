package model

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestWarnings_IsEmpty(t *testing.T) {
	var nilW *Warnings
	if !nilW.IsEmpty() {
		t.Error("nil receiver must be reported empty")
	}
	if !(&Warnings{}).IsEmpty() {
		t.Error("zero-value Warnings must be reported empty")
	}
	w := &Warnings{
		PromptInjectionDetected: &PromptInjectionDetectedWarning{Detected: true, DetectedByAgents: []string{"X"}, DetectionCount: 1, UserMessage: "msg"},
	}
	if w.IsEmpty() {
		t.Error("Warnings with one entry must NOT be empty")
	}
}

func TestWarnings_JSONShapeIsObjectMap(t *testing.T) {
	w := &Warnings{
		PromptInjectionDetected: &PromptInjectionDetectedWarning{
			Detected:         true,
			DetectedByAgents: []string{"AGENT_KEY_PARAMS", "AGENT_RISK_DETECTION"},
			DetectionCount:   2,
			UserMessage:      "В тексте обнаружены признаки попытки воздействия.",
		},
		InputTruncated: &InputTruncatedWarning{
			TruncatedBytes: 5000,
			TotalBytes:     205000,
			UserMessage:    "Текст усечён",
		},
	}
	b, err := json.Marshal(w)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	// Object-map shape: keys equal to warning codes.
	for _, must := range []string{
		`"PROMPT_INJECTION_DETECTED":{`, `"INPUT_TRUNCATED":{`,
		`"detected":true`, `"detection_count":2`, `"truncated_bytes":5000`,
	} {
		if !strings.Contains(string(b), must) {
			t.Errorf("expected substring %q in JSON, got %s", must, b)
		}
	}
	// Unset warnings must be omitted.
	for _, mustNot := range []string{`"RE_CHECK_PARENT_ANALYSIS_MISSING"`, `"CLASSIFICATION_PARAMS_MISMATCH"`, `"RECOMMENDATION_ORPHAN_REF"`} {
		if strings.Contains(string(b), mustNot) {
			t.Errorf("nil warning must be omitted, but %q present in %s", mustNot, b)
		}
	}
}

func TestWarnings_RoundTrip(t *testing.T) {
	w := &Warnings{
		ReCheckParentAnalysisMissing: &ReCheckParentAnalysisMissingWarning{
			UserMessage: "Сравнение с предыдущей версией недоступно",
		},
		RecommendationOrphanRef: &RecommendationOrphanRefWarning{
			OrphanRiskIDs: []string{"R-999", "R-P777"},
			UserMessage:   "Внутренняя несогласованность ссылок на риски",
		},
	}
	b, err := json.Marshal(w)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got Warnings
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !reflect.DeepEqual(&got, w) {
		t.Fatalf("round-trip mismatch:\n got=%+v\nwant=%+v", &got, w)
	}
}

func TestWarningCodes_WireStringsStable(t *testing.T) {
	// Locks the wire-format constants — drift here breaks downstream Reporting Engine.
	checks := map[string]string{
		"PROMPT_INJECTION_DETECTED":       WarningCodePromptInjectionDetected,
		"RE_CHECK_PARENT_ANALYSIS_MISSING": WarningCodeReCheckParentAnalysisMissing,
		"INPUT_TRUNCATED":                  WarningCodeInputTruncated,
		"CLASSIFICATION_PARAMS_MISMATCH":   WarningCodeClassificationParamsMismatch,
		"RECOMMENDATION_ORPHAN_REF":        WarningCodeRecommendationOrphanRef,
	}
	for want, got := range checks {
		if got != want {
			t.Errorf("warning code drift: %q != %q", got, want)
		}
	}
}
