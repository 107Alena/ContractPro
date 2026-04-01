package model

import (
	"encoding/json"
	"testing"
	"time"
)

func TestDocumentProcessingArtifactsPersistedJSONRoundTrip(t *testing.T) {
	ts := time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)
	event := DocumentProcessingArtifactsPersisted{
		EventMeta:  EventMeta{CorrelationID: "corr-1", Timestamp: ts},
		JobID:      "job-1",
		DocumentID: "doc-1",
	}

	data, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var restored DocumentProcessingArtifactsPersisted
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
}

func TestDocumentProcessingArtifactsPersistFailedJSONRoundTrip(t *testing.T) {
	ts := time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)
	event := DocumentProcessingArtifactsPersistFailed{
		EventMeta:    EventMeta{CorrelationID: "corr-2", Timestamp: ts},
		JobID:        "job-2",
		DocumentID:   "doc-2",
		ErrorCode:    "STORAGE_UNAVAILABLE",
		ErrorMessage: "Object Storage is temporarily unavailable",
		IsRetryable:  true,
	}

	data, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var restored DocumentProcessingArtifactsPersistFailed
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if restored.ErrorCode != event.ErrorCode {
		t.Errorf("error_code mismatch: %s != %s", restored.ErrorCode, event.ErrorCode)
	}
	if restored.ErrorMessage != event.ErrorMessage {
		t.Errorf("error_message mismatch")
	}
	if restored.IsRetryable != event.IsRetryable {
		t.Errorf("is_retryable mismatch: %v != %v", restored.IsRetryable, event.IsRetryable)
	}
}

func TestDocumentProcessingArtifactsPersistFailedOptionalErrorCode(t *testing.T) {
	event := DocumentProcessingArtifactsPersistFailed{
		EventMeta:    EventMeta{CorrelationID: "corr-1", Timestamp: time.Now().UTC()},
		JobID:        "job-1",
		DocumentID:   "doc-1",
		ErrorMessage: "unknown error",
		IsRetryable:  false,
	}

	data, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if _, ok := raw["error_code"]; ok {
		t.Error("expected error_code to be omitted when empty")
	}
	if _, ok := raw["error_message"]; !ok {
		t.Error("expected error_message to be present")
	}
}

func TestSemanticTreeProvidedJSONRoundTrip(t *testing.T) {
	ts := time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)
	rawTree := `{"root":{"id":"n1","type":"ROOT","children":[]}}`
	event := SemanticTreeProvided{
		EventMeta:    EventMeta{CorrelationID: "corr-3", Timestamp: ts},
		JobID:        "job-3",
		DocumentID:   "doc-3",
		VersionID:    "ver-3",
		SemanticTree: json.RawMessage(rawTree),
	}

	data, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var restored SemanticTreeProvided
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if string(restored.SemanticTree) != rawTree {
		t.Errorf("semantic_tree content mismatch")
	}
	if restored.ErrorCode != "" {
		t.Errorf("expected empty error_code, got %s", restored.ErrorCode)
	}
}

func TestSemanticTreeProvidedWithError(t *testing.T) {
	event := SemanticTreeProvided{
		EventMeta:    EventMeta{CorrelationID: "corr-1", Timestamp: time.Now().UTC()},
		JobID:        "job-1",
		DocumentID:   "doc-1",
		VersionID:    "ver-1",
		SemanticTree: nil,
		ErrorCode:    "VERSION_NOT_FOUND",
		ErrorMessage: "version ver-1 not found",
		IsRetryable:  false,
	}

	data, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var restored SemanticTreeProvided
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if restored.ErrorCode != "VERSION_NOT_FOUND" {
		t.Errorf("error_code mismatch: %s", restored.ErrorCode)
	}
	if restored.ErrorMessage != "version ver-1 not found" {
		t.Errorf("error_message mismatch")
	}
}

func TestSemanticTreeProvidedOptionalErrorFieldsOmitted(t *testing.T) {
	event := SemanticTreeProvided{
		EventMeta:    EventMeta{CorrelationID: "corr-1", Timestamp: time.Now().UTC()},
		JobID:        "job-1",
		DocumentID:   "doc-1",
		VersionID:    "ver-1",
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

	for _, key := range []string{"error_code", "error_message", "is_retryable"} {
		if _, ok := raw[key]; ok {
			t.Errorf("expected %q to be omitted when zero-valued", key)
		}
	}
}

func TestDocumentVersionDiffPersistedJSONRoundTrip(t *testing.T) {
	ts := time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)
	event := DocumentVersionDiffPersisted{
		EventMeta:  EventMeta{CorrelationID: "corr-4", Timestamp: ts},
		JobID:      "job-4",
		DocumentID: "doc-4",
	}

	data, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var restored DocumentVersionDiffPersisted
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if restored.JobID != event.JobID {
		t.Errorf("job_id mismatch")
	}
}

func TestDocumentVersionDiffPersistFailedJSONRoundTrip(t *testing.T) {
	event := DocumentVersionDiffPersistFailed{
		EventMeta:    EventMeta{CorrelationID: "corr-5", Timestamp: time.Now().UTC()},
		JobID:        "job-5",
		DocumentID:   "doc-5",
		ErrorCode:    "VERSIONS_NOT_FOUND",
		ErrorMessage: "base version not found",
		IsRetryable:  false,
	}

	data, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var restored DocumentVersionDiffPersistFailed
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if restored.ErrorCode != "VERSIONS_NOT_FOUND" {
		t.Errorf("error_code mismatch")
	}
	if restored.IsRetryable != false {
		t.Errorf("is_retryable should be false")
	}
}

func TestArtifactsProvidedJSONRoundTrip(t *testing.T) {
	ts := time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)
	event := ArtifactsProvided{
		EventMeta:  EventMeta{CorrelationID: "corr-6", Timestamp: ts},
		JobID:      "job-6",
		DocumentID: "doc-6",
		VersionID:  "ver-6",
		Artifacts: map[ArtifactType]json.RawMessage{
			ArtifactTypeSemanticTree:  json.RawMessage(`{"root":{}}`),
			ArtifactTypeExtractedText: json.RawMessage(`{"content":"hello"}`),
		},
		MissingTypes: []ArtifactType{ArtifactTypeRiskAnalysis},
	}

	data, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var restored ArtifactsProvided
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if len(restored.Artifacts) != 2 {
		t.Fatalf("expected 2 artifacts, got %d", len(restored.Artifacts))
	}
	if string(restored.Artifacts[ArtifactTypeSemanticTree]) != `{"root":{}}` {
		t.Errorf("SEMANTIC_TREE content mismatch")
	}
	if len(restored.MissingTypes) != 1 {
		t.Fatalf("expected 1 missing type, got %d", len(restored.MissingTypes))
	}
	if restored.MissingTypes[0] != ArtifactTypeRiskAnalysis {
		t.Errorf("missing_types[0] mismatch: %s", restored.MissingTypes[0])
	}
}

func TestArtifactsProvidedWithError(t *testing.T) {
	event := ArtifactsProvided{
		EventMeta:    EventMeta{CorrelationID: "corr-1", Timestamp: time.Now().UTC()},
		JobID:        "job-1",
		DocumentID:   "doc-1",
		VersionID:    "ver-1",
		Artifacts:    nil,
		ErrorCode:    "VERSION_NOT_FOUND",
		ErrorMessage: "version not found",
	}

	data, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var restored ArtifactsProvided
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if restored.ErrorCode != "VERSION_NOT_FOUND" {
		t.Errorf("error_code mismatch")
	}
}

func TestArtifactsProvidedOptionalFieldsOmitted(t *testing.T) {
	event := ArtifactsProvided{
		EventMeta:  EventMeta{CorrelationID: "corr-1", Timestamp: time.Now().UTC()},
		JobID:      "job-1",
		DocumentID: "doc-1",
		VersionID:  "ver-1",
		Artifacts: map[ArtifactType]json.RawMessage{
			ArtifactTypeSemanticTree: json.RawMessage(`{}`),
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

	for _, key := range []string{"missing_types", "error_code", "error_message"} {
		if _, ok := raw[key]; ok {
			t.Errorf("expected %q to be omitted when empty/zero", key)
		}
	}
}

func TestLegalAnalysisArtifactsPersistedJSONRoundTrip(t *testing.T) {
	event := LegalAnalysisArtifactsPersisted{
		EventMeta:  EventMeta{CorrelationID: "corr-7", Timestamp: time.Now().UTC()},
		JobID:      "job-7",
		DocumentID: "doc-7",
	}

	data, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var restored LegalAnalysisArtifactsPersisted
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if restored.JobID != event.JobID {
		t.Errorf("job_id mismatch")
	}
}

func TestLegalAnalysisArtifactsPersistFailedJSONRoundTrip(t *testing.T) {
	event := LegalAnalysisArtifactsPersistFailed{
		EventMeta:    EventMeta{CorrelationID: "corr-8", Timestamp: time.Now().UTC()},
		JobID:        "job-8",
		DocumentID:   "doc-8",
		ErrorMessage: "DB constraint violation",
		IsRetryable:  false,
	}

	data, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var restored LegalAnalysisArtifactsPersistFailed
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if restored.ErrorMessage != event.ErrorMessage {
		t.Errorf("error_message mismatch")
	}
}

func TestReportsArtifactsPersistedJSONRoundTrip(t *testing.T) {
	event := ReportsArtifactsPersisted{
		EventMeta:  EventMeta{CorrelationID: "corr-9", Timestamp: time.Now().UTC()},
		JobID:      "job-9",
		DocumentID: "doc-9",
	}

	data, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var restored ReportsArtifactsPersisted
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if restored.JobID != event.JobID {
		t.Errorf("job_id mismatch")
	}
}

func TestReportsArtifactsPersistFailedJSONRoundTrip(t *testing.T) {
	event := ReportsArtifactsPersistFailed{
		EventMeta:    EventMeta{CorrelationID: "corr-10", Timestamp: time.Now().UTC()},
		JobID:        "job-10",
		DocumentID:   "doc-10",
		ErrorCode:    "STORAGE_FULL",
		ErrorMessage: "Object Storage bucket full",
		IsRetryable:  true,
	}

	data, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var restored ReportsArtifactsPersistFailed
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if restored.ErrorCode != "STORAGE_FULL" {
		t.Errorf("error_code mismatch")
	}
	if restored.IsRetryable != true {
		t.Errorf("is_retryable should be true")
	}
}

// --- Notification events ---

func TestVersionProcessingArtifactsReadyJSONRoundTrip(t *testing.T) {
	ts := time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)
	event := VersionProcessingArtifactsReady{
		EventMeta:  EventMeta{CorrelationID: "corr-11", Timestamp: ts},
		DocumentID: "doc-11",
		VersionID:  "ver-11",
		OrgID:      "org-11",
		ArtifactTypes: []ArtifactType{
			ArtifactTypeOCRRaw,
			ArtifactTypeExtractedText,
			ArtifactTypeDocumentStructure,
			ArtifactTypeSemanticTree,
			ArtifactTypeProcessingWarnings,
		},
	}

	data, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var restored VersionProcessingArtifactsReady
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if len(restored.ArtifactTypes) != 5 {
		t.Errorf("expected 5 artifact_types, got %d", len(restored.ArtifactTypes))
	}
	if restored.OrgID != event.OrgID {
		t.Errorf("organization_id mismatch")
	}
}

func TestVersionAnalysisArtifactsReadyJSONRoundTrip(t *testing.T) {
	event := VersionAnalysisArtifactsReady{
		EventMeta:  EventMeta{CorrelationID: "corr-12", Timestamp: time.Now().UTC()},
		DocumentID: "doc-12",
		VersionID:  "ver-12",
		OrgID:      "org-12",
		ArtifactTypes: []ArtifactType{
			ArtifactTypeClassificationResult,
			ArtifactTypeKeyParameters,
			ArtifactTypeRiskAnalysis,
			ArtifactTypeRiskProfile,
			ArtifactTypeRecommendations,
			ArtifactTypeSummary,
			ArtifactTypeDetailedReport,
			ArtifactTypeAggregateScore,
		},
	}

	data, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var restored VersionAnalysisArtifactsReady
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if len(restored.ArtifactTypes) != 8 {
		t.Errorf("expected 8 artifact_types, got %d", len(restored.ArtifactTypes))
	}
}

func TestVersionReportsReadyJSONRoundTrip(t *testing.T) {
	event := VersionReportsReady{
		EventMeta:  EventMeta{CorrelationID: "corr-13", Timestamp: time.Now().UTC()},
		DocumentID: "doc-13",
		VersionID:  "ver-13",
		OrgID:      "org-13",
		ArtifactTypes: []ArtifactType{
			ArtifactTypeExportPDF,
			ArtifactTypeExportDOCX,
		},
	}

	data, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var restored VersionReportsReady
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if len(restored.ArtifactTypes) != 2 {
		t.Errorf("expected 2 artifact_types, got %d", len(restored.ArtifactTypes))
	}
}

func TestVersionCreatedJSONRoundTrip(t *testing.T) {
	ts := time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)
	event := VersionCreated{
		EventMeta:       EventMeta{CorrelationID: "corr-14", Timestamp: ts},
		DocumentID:      "doc-14",
		VersionID:       "ver-14",
		VersionNumber:   3,
		OrgID:           "org-14",
		OriginType:      OriginTypeRecommendationApplied,
		ParentVersionID: "ver-13",
		CreatedByUserID: "user-1",
	}

	data, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var restored VersionCreated
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if restored.VersionNumber != 3 {
		t.Errorf("version_number mismatch: %d", restored.VersionNumber)
	}
	if restored.OriginType != OriginTypeRecommendationApplied {
		t.Errorf("origin_type mismatch: %s", restored.OriginType)
	}
	if restored.ParentVersionID != "ver-13" {
		t.Errorf("parent_version_id mismatch: %s", restored.ParentVersionID)
	}
}

func TestVersionCreatedOptionalParentVersionID(t *testing.T) {
	event := VersionCreated{
		EventMeta:       EventMeta{CorrelationID: "corr-1", Timestamp: time.Now().UTC()},
		DocumentID:      "doc-1",
		VersionID:       "ver-1",
		VersionNumber:   1,
		OrgID:           "org-1",
		OriginType:      OriginTypeUpload,
		CreatedByUserID: "user-1",
	}

	data, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if _, ok := raw["parent_version_id"]; ok {
		t.Error("expected parent_version_id to be omitted for first version")
	}
}

func TestVersionPartiallyAvailableJSONRoundTrip(t *testing.T) {
	ts := time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)
	event := VersionPartiallyAvailable{
		EventMeta:      EventMeta{CorrelationID: "corr-15", Timestamp: ts},
		DocumentID:     "doc-15",
		VersionID:      "ver-15",
		OrgID:          "org-15",
		ArtifactStatus: ArtifactStatusProcessingArtifactsReceived,
		AvailableTypes: []ArtifactType{
			ArtifactTypeOCRRaw,
			ArtifactTypeExtractedText,
			ArtifactTypeSemanticTree,
		},
		FailedStage:  "legal_analysis",
		ErrorMessage: "LIC analysis timed out",
	}

	data, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var restored VersionPartiallyAvailable
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if restored.CorrelationID != event.CorrelationID {
		t.Errorf("correlation_id mismatch")
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
	if restored.ArtifactStatus != event.ArtifactStatus {
		t.Errorf("artifact_status mismatch: got %s, want %s", restored.ArtifactStatus, event.ArtifactStatus)
	}
	if len(restored.AvailableTypes) != 3 {
		t.Errorf("expected 3 available_types, got %d", len(restored.AvailableTypes))
	}
	if restored.FailedStage != event.FailedStage {
		t.Errorf("failed_stage mismatch")
	}
	if restored.ErrorMessage != event.ErrorMessage {
		t.Errorf("error_message mismatch")
	}
}

func TestVersionPartiallyAvailableOptionalFieldsOmitted(t *testing.T) {
	event := VersionPartiallyAvailable{
		EventMeta:      EventMeta{CorrelationID: "corr-1", Timestamp: time.Now().UTC()},
		DocumentID:     "doc-1",
		VersionID:      "ver-1",
		OrgID:          "org-1",
		ArtifactStatus: ArtifactStatusProcessingArtifactsReceived,
		AvailableTypes: []ArtifactType{ArtifactTypeOCRRaw},
	}

	data, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	for _, key := range []string{"failed_stage", "error_message"} {
		if _, ok := raw[key]; ok {
			t.Errorf("expected %q to be omitted when empty", key)
		}
	}
}

func TestVersionCreatedOriginTypeSerialization(t *testing.T) {
	// Verify OriginType serializes as string value.
	event := VersionCreated{
		EventMeta:       EventMeta{CorrelationID: "corr-1", Timestamp: time.Now().UTC()},
		DocumentID:      "doc-1",
		VersionID:       "ver-1",
		VersionNumber:   1,
		OrgID:           "org-1",
		OriginType:      OriginTypeReCheck,
		CreatedByUserID: "user-1",
	}

	data, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	var originType string
	if err := json.Unmarshal(raw["origin_type"], &originType); err != nil {
		t.Fatalf("origin_type unmarshal error: %v", err)
	}
	if originType != "RE_CHECK" {
		t.Errorf("expected RE_CHECK, got %s", originType)
	}
}
