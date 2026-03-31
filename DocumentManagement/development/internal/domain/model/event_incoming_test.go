package model

import (
	"encoding/json"
	"testing"
	"time"
)

func TestDocumentProcessingArtifactsReadyJSONRoundTrip(t *testing.T) {
	ts := time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)
	event := DocumentProcessingArtifactsReady{
		EventMeta:    EventMeta{CorrelationID: "corr-1", Timestamp: ts},
		JobID:        "job-1",
		DocumentID:   "doc-1",
		VersionID:    "ver-1",
		OrgID:        "org-1",
		OCRRaw:       json.RawMessage(`{"pages":[{"text":"hello","confidence":0.95}]}`),
		Text:         json.RawMessage(`{"content":"hello world","page_count":1}`),
		Structure:    json.RawMessage(`{"sections":[]}`),
		SemanticTree: json.RawMessage(`{"root":{"type":"ROOT","children":[]}}`),
		Warnings:     json.RawMessage(`[{"code":"LOW_CONFIDENCE","message":"low OCR confidence"}]`),
	}

	data, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var restored DocumentProcessingArtifactsReady
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if restored.CorrelationID != event.CorrelationID {
		t.Errorf("correlation_id mismatch")
	}
	if restored.JobID != event.JobID {
		t.Errorf("job_id mismatch")
	}
	if restored.DocumentID != event.DocumentID {
		t.Errorf("document_id mismatch")
	}
	if restored.VersionID != event.VersionID {
		t.Errorf("version_id mismatch")
	}
	if restored.OrgID != event.OrgID {
		t.Errorf("organization_id mismatch")
	}
	if string(restored.OCRRaw) != string(event.OCRRaw) {
		t.Errorf("ocr_raw content mismatch")
	}
	if string(restored.SemanticTree) != string(event.SemanticTree) {
		t.Errorf("semantic_tree content mismatch")
	}
}

func TestDocumentProcessingArtifactsReadyRawMessagePreservation(t *testing.T) {
	// Verify that json.RawMessage fields survive round-trip without modification.
	rawTree := `{"root":{"id":"n1","type":"ROOT","children":[{"id":"n2","type":"SECTION","content":"Раздел 1"}]}}`
	event := DocumentProcessingArtifactsReady{
		EventMeta:    EventMeta{CorrelationID: "corr-1", Timestamp: time.Now().UTC()},
		JobID:        "job-1",
		DocumentID:   "doc-1",
		VersionID:    "ver-1",
		OCRRaw:       json.RawMessage(`{}`),
		Text:         json.RawMessage(`{}`),
		Structure:    json.RawMessage(`{}`),
		SemanticTree: json.RawMessage(rawTree),
	}

	data, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var restored DocumentProcessingArtifactsReady
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if string(restored.SemanticTree) != rawTree {
		t.Errorf("semantic_tree raw content was modified during round-trip:\ngot:  %s\nwant: %s",
			string(restored.SemanticTree), rawTree)
	}
}

func TestDocumentProcessingArtifactsReadyOptionalFields(t *testing.T) {
	// organization_id and warnings are optional — should be omitted when empty.
	event := DocumentProcessingArtifactsReady{
		EventMeta:    EventMeta{CorrelationID: "corr-1", Timestamp: time.Now().UTC()},
		JobID:        "job-1",
		DocumentID:   "doc-1",
		VersionID:    "ver-1",
		OCRRaw:       json.RawMessage(`{}`),
		Text:         json.RawMessage(`{}`),
		Structure:    json.RawMessage(`{}`),
		SemanticTree: json.RawMessage(`{}`),
	}

	data, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if _, ok := raw["organization_id"]; ok {
		t.Error("expected organization_id to be omitted when empty")
	}
	if _, ok := raw["warnings"]; ok {
		t.Error("expected warnings to be omitted when nil")
	}
}

func TestDocumentProcessingArtifactsReadyBackwardCompatibility(t *testing.T) {
	// Simulate an event from a newer DP version with unknown fields.
	// DM should ignore unknown fields gracefully.
	jsonData := `{
		"correlation_id": "corr-1",
		"timestamp": "2026-04-01T12:00:00Z",
		"job_id": "job-1",
		"document_id": "doc-1",
		"version_id": "ver-1",
		"ocr_raw": {},
		"text": {},
		"structure": {},
		"semantic_tree": {},
		"unknown_future_field": "should be ignored",
		"another_new_field": 42
	}`

	var event DocumentProcessingArtifactsReady
	if err := json.Unmarshal([]byte(jsonData), &event); err != nil {
		t.Fatalf("unmarshal with unknown fields should succeed: %v", err)
	}
	if event.JobID != "job-1" {
		t.Errorf("expected job_id job-1, got %s", event.JobID)
	}
}

func TestGetSemanticTreeRequestJSONRoundTrip(t *testing.T) {
	ts := time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)
	event := GetSemanticTreeRequest{
		EventMeta:  EventMeta{CorrelationID: "corr-2", Timestamp: ts},
		JobID:      "job-2",
		DocumentID: "doc-2",
		VersionID:  "ver-2",
		OrgID:      "org-2",
	}

	data, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var restored GetSemanticTreeRequest
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if restored.CorrelationID != event.CorrelationID {
		t.Errorf("correlation_id mismatch")
	}
	if restored.JobID != event.JobID {
		t.Errorf("job_id mismatch")
	}
	if restored.VersionID != event.VersionID {
		t.Errorf("version_id mismatch")
	}
}

func TestGetSemanticTreeRequestOptionalOrgID(t *testing.T) {
	event := GetSemanticTreeRequest{
		EventMeta:  EventMeta{CorrelationID: "corr-1", Timestamp: time.Now().UTC()},
		JobID:      "job-1",
		DocumentID: "doc-1",
		VersionID:  "ver-1",
	}

	data, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if _, ok := raw["organization_id"]; ok {
		t.Error("expected organization_id to be omitted when empty")
	}
}

func TestDocumentVersionDiffReadyJSONRoundTrip(t *testing.T) {
	ts := time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)
	event := DocumentVersionDiffReady{
		EventMeta:           EventMeta{CorrelationID: "corr-3", Timestamp: ts},
		JobID:               "job-3",
		DocumentID:          "doc-3",
		BaseVersionID:       "ver-1",
		TargetVersionID:     "ver-2",
		OrgID:               "org-3",
		TextDiffs:           json.RawMessage(`[{"type":"modified","path":"1.1","old_text":"old","new_text":"new"}]`),
		StructuralDiffs:     json.RawMessage(`[{"type":"added","node_id":"n5"}]`),
		TextDiffCount:       1,
		StructuralDiffCount: 1,
	}

	data, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var restored DocumentVersionDiffReady
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if restored.BaseVersionID != event.BaseVersionID {
		t.Errorf("base_version_id mismatch")
	}
	if restored.TargetVersionID != event.TargetVersionID {
		t.Errorf("target_version_id mismatch")
	}
	if restored.TextDiffCount != event.TextDiffCount {
		t.Errorf("text_diff_count mismatch: %d != %d", restored.TextDiffCount, event.TextDiffCount)
	}
	if string(restored.TextDiffs) != string(event.TextDiffs) {
		t.Errorf("text_diffs content mismatch")
	}
}

func TestGetArtifactsRequestJSONRoundTrip(t *testing.T) {
	ts := time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)
	event := GetArtifactsRequest{
		EventMeta:     EventMeta{CorrelationID: "corr-4", Timestamp: ts},
		JobID:         "job-4",
		DocumentID:    "doc-4",
		VersionID:     "ver-4",
		OrgID:         "org-4",
		ArtifactTypes: []ArtifactType{ArtifactTypeSemanticTree, ArtifactTypeExtractedText},
	}

	data, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var restored GetArtifactsRequest
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if len(restored.ArtifactTypes) != 2 {
		t.Fatalf("expected 2 artifact_types, got %d", len(restored.ArtifactTypes))
	}
	if restored.ArtifactTypes[0] != ArtifactTypeSemanticTree {
		t.Errorf("artifact_types[0] mismatch: %s", restored.ArtifactTypes[0])
	}
	if restored.ArtifactTypes[1] != ArtifactTypeExtractedText {
		t.Errorf("artifact_types[1] mismatch: %s", restored.ArtifactTypes[1])
	}
}

func TestGetArtifactsRequestArtifactTypesSerialization(t *testing.T) {
	// Verify ArtifactType values serialize as strings, not integers.
	event := GetArtifactsRequest{
		EventMeta:     EventMeta{CorrelationID: "corr-1", Timestamp: time.Now().UTC()},
		JobID:         "job-1",
		DocumentID:    "doc-1",
		VersionID:     "ver-1",
		ArtifactTypes: []ArtifactType{ArtifactTypeRiskAnalysis},
	}

	data, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	var types []string
	if err := json.Unmarshal(raw["artifact_types"], &types); err != nil {
		t.Fatalf("artifact_types unmarshal error: %v", err)
	}
	if types[0] != "RISK_ANALYSIS" {
		t.Errorf("expected RISK_ANALYSIS string, got %s", types[0])
	}
}

func TestLegalAnalysisArtifactsReadyJSONRoundTrip(t *testing.T) {
	ts := time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)
	event := LegalAnalysisArtifactsReady{
		EventMeta:            EventMeta{CorrelationID: "corr-5", Timestamp: ts},
		JobID:                "job-5",
		DocumentID:           "doc-5",
		VersionID:            "ver-5",
		OrgID:                "org-5",
		ClassificationResult: json.RawMessage(`{"contract_type":"supply","confidence":0.92}`),
		KeyParameters:        json.RawMessage(`{"parties":["ООО Альфа","ООО Бета"]}`),
		RiskAnalysis:         json.RawMessage(`{"risks":[{"id":"r1","level":"high"}]}`),
		RiskProfile:          json.RawMessage(`{"overall_level":"high","high_count":1}`),
		Recommendations:      json.RawMessage(`[{"risk_id":"r1","recommended_text":"new text"}]`),
		Summary:              json.RawMessage(`{"text":"Договор поставки между ООО Альфа и ООО Бета"}`),
		DetailedReport:       json.RawMessage(`{"sections":[{"title":"Общие положения"}]}`),
		AggregateScore:       json.RawMessage(`{"score":3.5,"label":"medium_risk"}`),
	}

	data, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var restored LegalAnalysisArtifactsReady
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if restored.JobID != event.JobID {
		t.Errorf("job_id mismatch")
	}
	if string(restored.ClassificationResult) != string(event.ClassificationResult) {
		t.Errorf("classification_result content mismatch")
	}
	if string(restored.RiskAnalysis) != string(event.RiskAnalysis) {
		t.Errorf("risk_analysis content mismatch")
	}
	if string(restored.AggregateScore) != string(event.AggregateScore) {
		t.Errorf("aggregate_score content mismatch")
	}
}

func TestLegalAnalysisArtifactsReadyAllFieldsPresent(t *testing.T) {
	event := LegalAnalysisArtifactsReady{
		EventMeta:            EventMeta{CorrelationID: "corr-1", Timestamp: time.Now().UTC()},
		JobID:                "job-1",
		DocumentID:           "doc-1",
		VersionID:            "ver-1",
		ClassificationResult: json.RawMessage(`{}`),
		KeyParameters:        json.RawMessage(`{}`),
		RiskAnalysis:         json.RawMessage(`{}`),
		RiskProfile:          json.RawMessage(`{}`),
		Recommendations:      json.RawMessage(`{}`),
		Summary:              json.RawMessage(`{}`),
		DetailedReport:       json.RawMessage(`{}`),
		AggregateScore:       json.RawMessage(`{}`),
	}

	data, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	expectedFields := []string{
		"classification_result", "key_parameters", "risk_analysis",
		"risk_profile", "recommendations", "summary",
		"detailed_report", "aggregate_score",
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	for _, field := range expectedFields {
		if _, ok := raw[field]; !ok {
			t.Errorf("expected JSON key %q to be present", field)
		}
	}
}

func TestReportsArtifactsReadyJSONRoundTrip(t *testing.T) {
	ts := time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)
	event := ReportsArtifactsReady{
		EventMeta:  EventMeta{CorrelationID: "corr-6", Timestamp: ts},
		JobID:      "job-6",
		DocumentID: "doc-6",
		VersionID:  "ver-6",
		OrgID:      "org-6",
		ExportPDF: &BlobReference{
			StorageKey:  "org-6/doc-6/ver-6/EXPORT_PDF",
			FileName:    "report.pdf",
			SizeBytes:   2048576,
			ContentHash: "sha256:abc123",
		},
		ExportDOCX: &BlobReference{
			StorageKey:  "org-6/doc-6/ver-6/EXPORT_DOCX",
			FileName:    "report.docx",
			SizeBytes:   1024000,
			ContentHash: "sha256:def456",
		},
	}

	data, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var restored ReportsArtifactsReady
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if restored.ExportPDF == nil {
		t.Fatal("expected non-nil export_pdf")
	}
	if restored.ExportPDF.StorageKey != event.ExportPDF.StorageKey {
		t.Errorf("export_pdf.storage_key mismatch")
	}
	if restored.ExportPDF.SizeBytes != event.ExportPDF.SizeBytes {
		t.Errorf("export_pdf.size_bytes mismatch")
	}
	if restored.ExportDOCX == nil {
		t.Fatal("expected non-nil export_docx")
	}
	if restored.ExportDOCX.FileName != event.ExportDOCX.FileName {
		t.Errorf("export_docx.file_name mismatch")
	}
}

func TestReportsArtifactsReadyOnlyPDF(t *testing.T) {
	event := ReportsArtifactsReady{
		EventMeta:  EventMeta{CorrelationID: "corr-1", Timestamp: time.Now().UTC()},
		JobID:      "job-1",
		DocumentID: "doc-1",
		VersionID:  "ver-1",
		ExportPDF: &BlobReference{
			StorageKey:  "key",
			FileName:    "report.pdf",
			SizeBytes:   100,
			ContentHash: "sha256:aaa",
		},
	}

	data, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if _, ok := raw["export_pdf"]; !ok {
		t.Error("expected export_pdf to be present")
	}
	if _, ok := raw["export_docx"]; ok {
		t.Error("expected export_docx to be omitted when nil")
	}
}

func TestReportsArtifactsReadyOnlyDOCX(t *testing.T) {
	event := ReportsArtifactsReady{
		EventMeta:  EventMeta{CorrelationID: "corr-1", Timestamp: time.Now().UTC()},
		JobID:      "job-1",
		DocumentID: "doc-1",
		VersionID:  "ver-1",
		ExportDOCX: &BlobReference{
			StorageKey:  "key",
			FileName:    "report.docx",
			SizeBytes:   200,
			ContentHash: "sha256:bbb",
		},
	}

	data, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if _, ok := raw["export_docx"]; !ok {
		t.Error("expected export_docx to be present")
	}
	if _, ok := raw["export_pdf"]; ok {
		t.Error("expected export_pdf to be omitted when nil")
	}
}
