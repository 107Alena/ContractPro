package model

import (
	"encoding/json"
	"testing"
)

func TestProcessingWarning_JSONRoundTrip(t *testing.T) {
	warnings := []ProcessingWarning{
		{
			Code:    "OCR_PARTIAL_RECOGNITION",
			Message: "Частичное распознавание текста на страницах 3, 7",
			Stage:   ProcessingStageOCR,
		},
		{
			Code:    "EMPTY_PAGE_DETECTED",
			Message: "Обнаружена пустая страница 5",
			Stage:   ProcessingStageTextExtraction,
		},
		{
			Code:    "STRUCTURE_UNDETERMINED",
			Message: "Не удалось определить структуру документа",
			Stage:   ProcessingStageStructureExtract,
		},
	}

	for _, w := range warnings {
		t.Run(w.Code, func(t *testing.T) {
			data, err := json.Marshal(w)
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}

			var got ProcessingWarning
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatalf("Unmarshal: %v", err)
			}

			if got.Code != w.Code {
				t.Errorf("Code = %q, want %q", got.Code, w.Code)
			}
			if got.Message != w.Message {
				t.Errorf("Message = %q, want %q", got.Message, w.Message)
			}
			if got.Stage != w.Stage {
				t.Errorf("Stage = %q, want %q", got.Stage, w.Stage)
			}
		})
	}
}

func TestProcessingWarning_JSONFields(t *testing.T) {
	w := ProcessingWarning{
		Code:    "TEST_CODE",
		Message: "test message",
		Stage:   ProcessingStageOCR,
	}

	data, err := json.Marshal(w)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal to map: %v", err)
	}

	for _, field := range []string{"code", "message", "stage"} {
		if _, exists := raw[field]; !exists {
			t.Errorf("expected field %q in JSON output", field)
		}
	}
}
