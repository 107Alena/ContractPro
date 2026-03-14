package model

import (
	"encoding/json"
	"testing"
)

func TestInputDocumentReference_JSONRoundTrip(t *testing.T) {
	original := InputDocumentReference{
		DocumentID: "doc-1",
		FileName:   "contract.pdf",
		FileSize:   2048000,
		MimeType:   "application/pdf",
		Checksum:   "sha256:abc123",
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var restored InputDocumentReference
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if restored.DocumentID != original.DocumentID {
		t.Errorf("DocumentID = %q, want %q", restored.DocumentID, original.DocumentID)
	}
	if restored.FileName != original.FileName {
		t.Errorf("FileName = %q, want %q", restored.FileName, original.FileName)
	}
	if restored.FileSize != original.FileSize {
		t.Errorf("FileSize = %d, want %d", restored.FileSize, original.FileSize)
	}
	if restored.MimeType != original.MimeType {
		t.Errorf("MimeType = %q, want %q", restored.MimeType, original.MimeType)
	}
	if restored.Checksum != original.Checksum {
		t.Errorf("Checksum = %q, want %q", restored.Checksum, original.Checksum)
	}
}

func TestInputDocumentReference_JSONOmitsEmptyChecksum(t *testing.T) {
	ref := InputDocumentReference{
		DocumentID: "doc-1",
		FileName:   "contract.pdf",
		FileSize:   1024,
		MimeType:   "application/pdf",
	}

	data, err := json.Marshal(ref)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if _, exists := raw["checksum"]; exists {
		t.Error("checksum should be omitted when empty")
	}
}

func TestExtractedText_JSONRoundTrip(t *testing.T) {
	original := ExtractedText{
		DocumentID: "doc-1",
		Pages: []PageText{
			{PageNumber: 1, Text: "Page one text."},
			{PageNumber: 2, Text: "Page two text."},
		},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var restored ExtractedText
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if restored.DocumentID != original.DocumentID {
		t.Errorf("DocumentID = %q, want %q", restored.DocumentID, original.DocumentID)
	}
	if len(restored.Pages) != len(original.Pages) {
		t.Fatalf("Pages count = %d, want %d", len(restored.Pages), len(original.Pages))
	}
	for i, page := range restored.Pages {
		if page.PageNumber != original.Pages[i].PageNumber {
			t.Errorf("Pages[%d].PageNumber = %d, want %d", i, page.PageNumber, original.Pages[i].PageNumber)
		}
		if page.Text != original.Pages[i].Text {
			t.Errorf("Pages[%d].Text = %q, want %q", i, page.Text, original.Pages[i].Text)
		}
	}
}

func TestExtractedText_FullText(t *testing.T) {
	tests := []struct {
		name  string
		pages []PageText
		want  string
	}{
		{
			name:  "multiple pages",
			pages: []PageText{{PageNumber: 1, Text: "Page one."}, {PageNumber: 2, Text: "Page two."}},
			want:  "Page one.\nPage two.",
		},
		{
			name:  "single page",
			pages: []PageText{{PageNumber: 1, Text: "Only page."}},
			want:  "Only page.",
		},
		{
			name:  "empty pages",
			pages: []PageText{},
			want:  "",
		},
		{
			name:  "nil pages",
			pages: nil,
			want:  "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			et := &ExtractedText{DocumentID: "doc-1", Pages: tc.pages}
			got := et.FullText()
			if got != tc.want {
				t.Errorf("FullText() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestExtractedText_EmptyPages(t *testing.T) {
	original := ExtractedText{
		DocumentID: "doc-2",
		Pages:      []PageText{},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var restored ExtractedText
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if len(restored.Pages) != 0 {
		t.Errorf("Pages count = %d, want 0", len(restored.Pages))
	}
}

func TestOCRRawArtifact_Applicable(t *testing.T) {
	original := OCRRawArtifact{
		RawText: "Recognized text from scan",
		Status:  OCRStatusApplicable,
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var restored OCRRawArtifact
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if restored.RawText != original.RawText {
		t.Errorf("RawText = %q, want %q", restored.RawText, original.RawText)
	}
	if restored.Status != original.Status {
		t.Errorf("Status = %q, want %q", restored.Status, original.Status)
	}
}

func TestOCRRawArtifact_NotApplicable(t *testing.T) {
	original := OCRRawArtifact{
		Status: OCRStatusNotApplicable,
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var restored OCRRawArtifact
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if restored.Status != OCRStatusNotApplicable {
		t.Errorf("Status = %q, want %q", restored.Status, OCRStatusNotApplicable)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}
	if _, exists := raw["raw_text"]; exists {
		t.Error("raw_text should be omitted when not applicable")
	}
}

func TestOCRStatusConstants(t *testing.T) {
	statuses := []OCRStatus{OCRStatusApplicable, OCRStatusNotApplicable}
	expected := []string{"applicable", "not_applicable"}

	if len(statuses) != len(expected) {
		t.Fatalf("expected %d OCR statuses, got %d", len(expected), len(statuses))
	}

	for i, s := range statuses {
		if string(s) != expected[i] {
			t.Errorf("OCRStatus[%d] = %q, want %q", i, s, expected[i])
		}
	}
}
