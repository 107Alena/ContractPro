package model

import (
	"encoding/json"
	"testing"
	"time"
)

var testTimestamp = time.Date(2026, 3, 15, 14, 30, 0, 0, time.UTC)

var testEventMeta = EventMeta{
	CorrelationID: "corr-abc-123",
	Timestamp:     testTimestamp,
}

func TestEventMeta_JSONRoundTrip(t *testing.T) {
	data, err := json.Marshal(testEventMeta)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var restored EventMeta
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if restored.CorrelationID != testEventMeta.CorrelationID {
		t.Errorf("CorrelationID = %q, want %q", restored.CorrelationID, testEventMeta.CorrelationID)
	}
	if !restored.Timestamp.Equal(testEventMeta.Timestamp) {
		t.Errorf("Timestamp = %v, want %v", restored.Timestamp, testEventMeta.Timestamp)
	}
}

func TestDocumentProcessingArtifactsReady_JSONRoundTrip(t *testing.T) {
	original := DocumentProcessingArtifactsReady{
		EventMeta:  testEventMeta,
		JobID:      "job-1",
		DocumentID: "doc-1",
		VersionID:  "ver-1",
		OCRRaw:     OCRRawArtifact{Status: OCRStatusNotApplicable},
		Text: ExtractedText{
			DocumentID: "doc-1",
			Pages:      []PageText{{PageNumber: 1, Text: "Договор"}},
		},
		Structure: DocumentStructure{
			DocumentID: "doc-1",
			Sections:   []Section{{Number: "1", Title: "Предмет договора"}},
		},
		SemanticTree: SemanticTree{
			DocumentID: "doc-1",
			Root:       &SemanticNode{ID: "root", Type: NodeTypeRoot},
		},
		Warnings: []ProcessingWarning{
			{Code: "EMPTY_PAGE", Message: "Page 3 is empty", Stage: ProcessingStageTextExtraction},
		},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var restored DocumentProcessingArtifactsReady
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if restored.JobID != original.JobID {
		t.Errorf("JobID = %q, want %q", restored.JobID, original.JobID)
	}
	if restored.DocumentID != original.DocumentID {
		t.Errorf("DocumentID = %q, want %q", restored.DocumentID, original.DocumentID)
	}
	if restored.VersionID != original.VersionID {
		t.Errorf("VersionID = %q, want %q", restored.VersionID, original.VersionID)
	}
	if restored.CorrelationID != original.CorrelationID {
		t.Errorf("CorrelationID = %q, want %q", restored.CorrelationID, original.CorrelationID)
	}
	if restored.OCRRaw.Status != original.OCRRaw.Status {
		t.Errorf("OCRRaw.Status = %q, want %q", restored.OCRRaw.Status, original.OCRRaw.Status)
	}
	if len(restored.Text.Pages) != 1 {
		t.Errorf("Text.Pages length = %d, want 1", len(restored.Text.Pages))
	}
	if len(restored.Structure.Sections) != 1 {
		t.Errorf("Structure.Sections length = %d, want 1", len(restored.Structure.Sections))
	}
	if restored.SemanticTree.Root.Type != NodeTypeRoot {
		t.Errorf("SemanticTree.Root.Type = %q, want %q", restored.SemanticTree.Root.Type, NodeTypeRoot)
	}
	if len(restored.Warnings) != 1 {
		t.Errorf("Warnings length = %d, want 1", len(restored.Warnings))
	}
}

func TestDocumentProcessingArtifactsReady_JSONOmitsEmptyWarnings(t *testing.T) {
	event := DocumentProcessingArtifactsReady{
		EventMeta:  testEventMeta,
		JobID:      "job-1",
		DocumentID: "doc-1",
	}

	data, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if _, exists := raw["warnings"]; exists {
		t.Error("warnings should be omitted when empty")
	}
}

func TestDocumentProcessingArtifactsPersisted_JSONRoundTrip(t *testing.T) {
	original := DocumentProcessingArtifactsPersisted{
		EventMeta:  testEventMeta,
		JobID:      "job-1",
		DocumentID: "doc-1",
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var restored DocumentProcessingArtifactsPersisted
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if restored.JobID != original.JobID {
		t.Errorf("JobID = %q, want %q", restored.JobID, original.JobID)
	}
	if restored.CorrelationID != original.CorrelationID {
		t.Errorf("CorrelationID = %q, want %q", restored.CorrelationID, original.CorrelationID)
	}
}

func TestDocumentProcessingArtifactsPersistFailed_JSONRoundTrip(t *testing.T) {
	original := DocumentProcessingArtifactsPersistFailed{
		EventMeta:    testEventMeta,
		JobID:        "job-1",
		DocumentID:   "doc-1",
		ErrorCode:    "STORAGE_QUOTA_EXCEEDED",
		ErrorMessage: "storage unavailable",
		IsRetryable:  true,
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var restored DocumentProcessingArtifactsPersistFailed
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if restored.ErrorCode != original.ErrorCode {
		t.Errorf("ErrorCode = %q, want %q", restored.ErrorCode, original.ErrorCode)
	}
	if restored.ErrorMessage != original.ErrorMessage {
		t.Errorf("ErrorMessage = %q, want %q", restored.ErrorMessage, original.ErrorMessage)
	}
	if restored.IsRetryable != original.IsRetryable {
		t.Errorf("IsRetryable = %v, want %v", restored.IsRetryable, original.IsRetryable)
	}

	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("Unmarshal to map: %v", err)
	}
	if _, ok := m["error_code"]; !ok {
		t.Error("JSON missing key error_code")
	}
}

func TestDocumentProcessingArtifactsPersistFailed_JSONOmitsEmptyErrorCode(t *testing.T) {
	event := DocumentProcessingArtifactsPersistFailed{
		EventMeta:    testEventMeta,
		JobID:        "job-1",
		DocumentID:   "doc-1",
		ErrorMessage: "storage unavailable",
		IsRetryable:  true,
		// ErrorCode intentionally empty
	}

	data, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if _, exists := raw["error_code"]; exists {
		t.Error("error_code should be omitted when empty")
	}
}

func TestDocumentProcessingArtifactsPersistFailed_JSONBackwardsCompatibility(t *testing.T) {
	// Simulate old DM payload without error_code field.
	payload := `{"correlation_id":"corr-1","timestamp":"2026-03-15T14:30:00Z","job_id":"job-1","document_id":"doc-1","error_message":"old DM version","is_retryable":false}`

	var event DocumentProcessingArtifactsPersistFailed
	if err := json.Unmarshal([]byte(payload), &event); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if event.ErrorCode != "" {
		t.Errorf("ErrorCode = %q, want empty (backwards compat)", event.ErrorCode)
	}
	if event.ErrorMessage != "old DM version" {
		t.Errorf("ErrorMessage = %q, want %q", event.ErrorMessage, "old DM version")
	}
}

func TestGetSemanticTreeRequest_JSONRoundTrip(t *testing.T) {
	original := GetSemanticTreeRequest{
		EventMeta:  testEventMeta,
		JobID:      "job-1",
		DocumentID: "doc-1",
		VersionID:  "ver-1",
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var restored GetSemanticTreeRequest
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if restored.VersionID != original.VersionID {
		t.Errorf("VersionID = %q, want %q", restored.VersionID, original.VersionID)
	}
	if restored.CorrelationID != original.CorrelationID {
		t.Errorf("CorrelationID = %q, want %q", restored.CorrelationID, original.CorrelationID)
	}
}

func TestSemanticTreeProvided_JSONRoundTrip(t *testing.T) {
	original := SemanticTreeProvided{
		EventMeta:  testEventMeta,
		JobID:      "job-1",
		DocumentID: "doc-1",
		VersionID:  "ver-1",
		SemanticTree: SemanticTree{
			DocumentID: "doc-1",
			Root:       &SemanticNode{ID: "root", Type: NodeTypeRoot},
		},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var restored SemanticTreeProvided
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if restored.VersionID != original.VersionID {
		t.Errorf("VersionID = %q, want %q", restored.VersionID, original.VersionID)
	}
	if restored.SemanticTree.Root.Type != NodeTypeRoot {
		t.Errorf("SemanticTree.Root.Type = %q, want %q", restored.SemanticTree.Root.Type, NodeTypeRoot)
	}
}

func TestDocumentVersionDiffReady_JSONRoundTrip(t *testing.T) {
	original := DocumentVersionDiffReady{
		EventMeta:       testEventMeta,
		JobID:           "job-1",
		DocumentID:      "doc-1",
		BaseVersionID:   "ver-1",
		TargetVersionID: "ver-2",
		TextDiffs: []TextDiffEntry{
			{Type: DiffTypeModified, Path: "1.1", OldContent: "old", NewContent: "new"},
		},
		StructuralDiffs: []StructuralDiffEntry{
			{Type: DiffTypeAdded, NodeType: NodeTypeClause, NodeID: "clause-5"},
		},
		TextDiffCount:       1,
		StructuralDiffCount: 1,
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var restored DocumentVersionDiffReady
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if restored.BaseVersionID != original.BaseVersionID {
		t.Errorf("BaseVersionID = %q, want %q", restored.BaseVersionID, original.BaseVersionID)
	}
	if len(restored.TextDiffs) != 1 {
		t.Errorf("TextDiffs length = %d, want 1", len(restored.TextDiffs))
	}
	if len(restored.StructuralDiffs) != 1 {
		t.Errorf("StructuralDiffs length = %d, want 1", len(restored.StructuralDiffs))
	}
	if restored.TextDiffCount != 1 {
		t.Errorf("TextDiffCount = %d, want 1", restored.TextDiffCount)
	}
	if restored.StructuralDiffCount != 1 {
		t.Errorf("StructuralDiffCount = %d, want 1", restored.StructuralDiffCount)
	}

	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("Unmarshal to map: %v", err)
	}
	if _, ok := m["text_diff_count"]; !ok {
		t.Error("JSON missing key text_diff_count")
	}
	if _, ok := m["structural_diff_count"]; !ok {
		t.Error("JSON missing key structural_diff_count")
	}
}

func TestDocumentVersionDiffPersisted_JSONRoundTrip(t *testing.T) {
	original := DocumentVersionDiffPersisted{
		EventMeta:  testEventMeta,
		JobID:      "job-1",
		DocumentID: "doc-1",
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var restored DocumentVersionDiffPersisted
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if restored.JobID != original.JobID {
		t.Errorf("JobID = %q, want %q", restored.JobID, original.JobID)
	}
	if restored.CorrelationID != original.CorrelationID {
		t.Errorf("CorrelationID = %q, want %q", restored.CorrelationID, original.CorrelationID)
	}
}

func TestDocumentVersionDiffPersistFailed_JSONRoundTrip(t *testing.T) {
	original := DocumentVersionDiffPersistFailed{
		EventMeta:    testEventMeta,
		JobID:        "job-1",
		DocumentID:   "doc-1",
		ErrorCode:    "WRITE_CONFLICT",
		ErrorMessage: "write conflict",
		IsRetryable:  false,
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var restored DocumentVersionDiffPersistFailed
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if restored.ErrorCode != original.ErrorCode {
		t.Errorf("ErrorCode = %q, want %q", restored.ErrorCode, original.ErrorCode)
	}
	if restored.ErrorMessage != original.ErrorMessage {
		t.Errorf("ErrorMessage = %q, want %q", restored.ErrorMessage, original.ErrorMessage)
	}
	if restored.IsRetryable != original.IsRetryable {
		t.Errorf("IsRetryable = %v, want %v", restored.IsRetryable, original.IsRetryable)
	}

	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("Unmarshal to map: %v", err)
	}
	if _, ok := m["error_code"]; !ok {
		t.Error("JSON missing key error_code")
	}
}

func TestDocumentVersionDiffPersistFailed_JSONOmitsEmptyErrorCode(t *testing.T) {
	event := DocumentVersionDiffPersistFailed{
		EventMeta:    testEventMeta,
		JobID:        "job-1",
		DocumentID:   "doc-1",
		ErrorMessage: "write conflict",
		IsRetryable:  false,
		// ErrorCode intentionally empty
	}

	data, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if _, exists := raw["error_code"]; exists {
		t.Error("error_code should be omitted when empty")
	}
}

func TestDocumentVersionDiffPersistFailed_JSONBackwardsCompatibility(t *testing.T) {
	// Simulate old DM payload without error_code field.
	payload := `{"correlation_id":"corr-1","timestamp":"2026-03-15T14:30:00Z","job_id":"job-1","document_id":"doc-1","error_message":"old DM version","is_retryable":true}`

	var event DocumentVersionDiffPersistFailed
	if err := json.Unmarshal([]byte(payload), &event); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if event.ErrorCode != "" {
		t.Errorf("ErrorCode = %q, want empty (backwards compat)", event.ErrorCode)
	}
	if event.ErrorMessage != "old DM version" {
		t.Errorf("ErrorMessage = %q, want %q", event.ErrorMessage, "old DM version")
	}
}

func TestStatusChangedEvent_JSONRoundTrip(t *testing.T) {
	original := StatusChangedEvent{
		EventMeta:  testEventMeta,
		JobID:      "job-1",
		DocumentID: "doc-1",
		OldStatus:  StatusQueued,
		NewStatus:  StatusInProgress,
		Stage:      string(ProcessingStageOCR),
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var restored StatusChangedEvent
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if restored.OldStatus != original.OldStatus {
		t.Errorf("OldStatus = %q, want %q", restored.OldStatus, original.OldStatus)
	}
	if restored.NewStatus != original.NewStatus {
		t.Errorf("NewStatus = %q, want %q", restored.NewStatus, original.NewStatus)
	}
	if restored.Stage != original.Stage {
		t.Errorf("Stage = %q, want %q", restored.Stage, original.Stage)
	}
}

func TestStatusChangedEvent_JSONOmitsEmptyStage(t *testing.T) {
	event := StatusChangedEvent{
		EventMeta:  testEventMeta,
		JobID:      "job-1",
		DocumentID: "doc-1",
		OldStatus:  StatusQueued,
		NewStatus:  StatusInProgress,
	}

	data, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if _, exists := raw["stage"]; exists {
		t.Error("stage should be omitted when empty")
	}
}

func TestProcessingCompletedEvent_JSONRoundTrip(t *testing.T) {
	original := ProcessingCompletedEvent{
		EventMeta:    testEventMeta,
		JobID:        "job-1",
		DocumentID:   "doc-1",
		Status:       StatusCompletedWithWarnings,
		HasWarnings:  true,
		WarningCount: 3,
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var restored ProcessingCompletedEvent
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if restored.Status != original.Status {
		t.Errorf("Status = %q, want %q", restored.Status, original.Status)
	}
	if restored.HasWarnings != original.HasWarnings {
		t.Errorf("HasWarnings = %v, want %v", restored.HasWarnings, original.HasWarnings)
	}
	if restored.WarningCount != original.WarningCount {
		t.Errorf("WarningCount = %d, want %d", restored.WarningCount, original.WarningCount)
	}
}

func TestComparisonCompletedEvent_JSONRoundTrip(t *testing.T) {
	original := ComparisonCompletedEvent{
		EventMeta:           testEventMeta,
		JobID:               "job-1",
		DocumentID:          "doc-1",
		BaseVersionID:       "ver-1",
		TargetVersionID:     "ver-2",
		Status:              StatusCompleted,
		TextDiffCount:       5,
		StructuralDiffCount: 2,
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var restored ComparisonCompletedEvent
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if restored.BaseVersionID != original.BaseVersionID {
		t.Errorf("BaseVersionID = %q, want %q", restored.BaseVersionID, original.BaseVersionID)
	}
	if restored.TextDiffCount != original.TextDiffCount {
		t.Errorf("TextDiffCount = %d, want %d", restored.TextDiffCount, original.TextDiffCount)
	}
	if restored.StructuralDiffCount != original.StructuralDiffCount {
		t.Errorf("StructuralDiffCount = %d, want %d", restored.StructuralDiffCount, original.StructuralDiffCount)
	}
}

func TestProcessingFailedEvent_JSONRoundTrip(t *testing.T) {
	original := ProcessingFailedEvent{
		EventMeta:     testEventMeta,
		JobID:         "job-1",
		DocumentID:    "doc-1",
		Status:        StatusFailed,
		ErrorCode:     "OCR_SERVICE_UNAVAILABLE",
		ErrorMessage:  "Yandex Vision OCR returned HTTP 503",
		FailedAtStage: string(ProcessingStageOCR),
		IsRetryable:   true,
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var restored ProcessingFailedEvent
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if restored.Status != original.Status {
		t.Errorf("Status = %q, want %q", restored.Status, original.Status)
	}
	if restored.ErrorCode != original.ErrorCode {
		t.Errorf("ErrorCode = %q, want %q", restored.ErrorCode, original.ErrorCode)
	}
	if restored.ErrorMessage != original.ErrorMessage {
		t.Errorf("ErrorMessage = %q, want %q", restored.ErrorMessage, original.ErrorMessage)
	}
	if restored.FailedAtStage != original.FailedAtStage {
		t.Errorf("FailedAtStage = %q, want %q", restored.FailedAtStage, original.FailedAtStage)
	}
	if restored.IsRetryable != original.IsRetryable {
		t.Errorf("IsRetryable = %v, want %v", restored.IsRetryable, original.IsRetryable)
	}
}

func TestComparisonFailedEvent_JSONRoundTrip(t *testing.T) {
	original := ComparisonFailedEvent{
		EventMeta:     testEventMeta,
		JobID:         "job-1",
		DocumentID:    "doc-1",
		Status:        StatusTimedOut,
		ErrorCode:     "DM_RESPONSE_TIMEOUT",
		ErrorMessage:  "Timed out waiting for semantic tree from DM",
		FailedAtStage: string(ComparisonStageWaitingDM),
		IsRetryable:   false,
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var restored ComparisonFailedEvent
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if restored.Status != original.Status {
		t.Errorf("Status = %q, want %q", restored.Status, original.Status)
	}
	if restored.ErrorCode != original.ErrorCode {
		t.Errorf("ErrorCode = %q, want %q", restored.ErrorCode, original.ErrorCode)
	}
	if restored.FailedAtStage != original.FailedAtStage {
		t.Errorf("FailedAtStage = %q, want %q", restored.FailedAtStage, original.FailedAtStage)
	}
	if restored.IsRetryable != original.IsRetryable {
		t.Errorf("IsRetryable = %v, want %v", restored.IsRetryable, original.IsRetryable)
	}
}

func TestEventMeta_EmbeddedInJSON(t *testing.T) {
	event := StatusChangedEvent{
		EventMeta:  testEventMeta,
		JobID:      "job-1",
		DocumentID: "doc-1",
		OldStatus:  StatusQueued,
		NewStatus:  StatusInProgress,
	}

	data, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	// EventMeta fields should be at top level, not nested
	if _, exists := raw["correlation_id"]; !exists {
		t.Error("correlation_id should be present at top level of JSON (embedded EventMeta)")
	}
	if _, exists := raw["timestamp"]; !exists {
		t.Error("timestamp should be present at top level of JSON (embedded EventMeta)")
	}
}

// --- OrgID JSON round-trip tests for all 8 outbound events ---

func TestOrgID_JSONRoundTrip_AllOutboundEvents(t *testing.T) {
	const orgID = "org-acme-123"

	t.Run("DocumentProcessingArtifactsReady", func(t *testing.T) {
		original := DocumentProcessingArtifactsReady{
			EventMeta:  testEventMeta,
			JobID:      "job-1",
			DocumentID: "doc-1",
			VersionID:  "ver-1",
			OrgID:      orgID,
		}
		data, err := json.Marshal(original)
		if err != nil {
			t.Fatalf("Marshal error: %v", err)
		}
		var restored DocumentProcessingArtifactsReady
		if err := json.Unmarshal(data, &restored); err != nil {
			t.Fatalf("Unmarshal error: %v", err)
		}
		if restored.OrgID != orgID {
			t.Errorf("OrgID = %q, want %q", restored.OrgID, orgID)
		}
		// Verify JSON key name
		var raw map[string]any
		if err := json.Unmarshal(data, &raw); err != nil {
			t.Fatalf("Unmarshal to map: %v", err)
		}
		if raw["organization_id"] != orgID {
			t.Errorf("JSON key organization_id = %v, want %q", raw["organization_id"], orgID)
		}
	})

	t.Run("GetSemanticTreeRequest", func(t *testing.T) {
		original := GetSemanticTreeRequest{
			EventMeta:  testEventMeta,
			JobID:      "job-1",
			DocumentID: "doc-1",
			VersionID:  "ver-1",
			OrgID:      orgID,
		}
		data, err := json.Marshal(original)
		if err != nil {
			t.Fatalf("Marshal error: %v", err)
		}
		var restored GetSemanticTreeRequest
		if err := json.Unmarshal(data, &restored); err != nil {
			t.Fatalf("Unmarshal error: %v", err)
		}
		if restored.OrgID != orgID {
			t.Errorf("OrgID = %q, want %q", restored.OrgID, orgID)
		}
	})

	t.Run("DocumentVersionDiffReady", func(t *testing.T) {
		original := DocumentVersionDiffReady{
			EventMeta:       testEventMeta,
			JobID:           "job-1",
			DocumentID:      "doc-1",
			BaseVersionID:   "ver-1",
			TargetVersionID: "ver-2",
			OrgID:           orgID,
			TextDiffs:       []TextDiffEntry{},
			StructuralDiffs: []StructuralDiffEntry{},
		}
		data, err := json.Marshal(original)
		if err != nil {
			t.Fatalf("Marshal error: %v", err)
		}
		var restored DocumentVersionDiffReady
		if err := json.Unmarshal(data, &restored); err != nil {
			t.Fatalf("Unmarshal error: %v", err)
		}
		if restored.OrgID != orgID {
			t.Errorf("OrgID = %q, want %q", restored.OrgID, orgID)
		}
	})

	t.Run("StatusChangedEvent", func(t *testing.T) {
		original := StatusChangedEvent{
			EventMeta:  testEventMeta,
			JobID:      "job-1",
			DocumentID: "doc-1",
			OrgID:      orgID,
			OldStatus:  StatusQueued,
			NewStatus:  StatusInProgress,
		}
		data, err := json.Marshal(original)
		if err != nil {
			t.Fatalf("Marshal error: %v", err)
		}
		var restored StatusChangedEvent
		if err := json.Unmarshal(data, &restored); err != nil {
			t.Fatalf("Unmarshal error: %v", err)
		}
		if restored.OrgID != orgID {
			t.Errorf("OrgID = %q, want %q", restored.OrgID, orgID)
		}
	})

	t.Run("ProcessingCompletedEvent", func(t *testing.T) {
		original := ProcessingCompletedEvent{
			EventMeta:    testEventMeta,
			JobID:        "job-1",
			DocumentID:   "doc-1",
			OrgID:        orgID,
			Status:       StatusCompleted,
			HasWarnings:  false,
			WarningCount: 0,
		}
		data, err := json.Marshal(original)
		if err != nil {
			t.Fatalf("Marshal error: %v", err)
		}
		var restored ProcessingCompletedEvent
		if err := json.Unmarshal(data, &restored); err != nil {
			t.Fatalf("Unmarshal error: %v", err)
		}
		if restored.OrgID != orgID {
			t.Errorf("OrgID = %q, want %q", restored.OrgID, orgID)
		}
	})

	t.Run("ProcessingFailedEvent", func(t *testing.T) {
		original := ProcessingFailedEvent{
			EventMeta:     testEventMeta,
			JobID:         "job-1",
			DocumentID:    "doc-1",
			OrgID:         orgID,
			Status:        StatusFailed,
			ErrorCode:     "ERR",
			ErrorMessage:  "fail",
			FailedAtStage: "OCR",
			IsRetryable:   true,
		}
		data, err := json.Marshal(original)
		if err != nil {
			t.Fatalf("Marshal error: %v", err)
		}
		var restored ProcessingFailedEvent
		if err := json.Unmarshal(data, &restored); err != nil {
			t.Fatalf("Unmarshal error: %v", err)
		}
		if restored.OrgID != orgID {
			t.Errorf("OrgID = %q, want %q", restored.OrgID, orgID)
		}
	})

	t.Run("ComparisonCompletedEvent", func(t *testing.T) {
		original := ComparisonCompletedEvent{
			EventMeta:           testEventMeta,
			JobID:               "job-1",
			DocumentID:          "doc-1",
			OrgID:               orgID,
			BaseVersionID:       "ver-1",
			TargetVersionID:     "ver-2",
			Status:              StatusCompleted,
			TextDiffCount:       1,
			StructuralDiffCount: 2,
		}
		data, err := json.Marshal(original)
		if err != nil {
			t.Fatalf("Marshal error: %v", err)
		}
		var restored ComparisonCompletedEvent
		if err := json.Unmarshal(data, &restored); err != nil {
			t.Fatalf("Unmarshal error: %v", err)
		}
		if restored.OrgID != orgID {
			t.Errorf("OrgID = %q, want %q", restored.OrgID, orgID)
		}
	})

	t.Run("ComparisonFailedEvent", func(t *testing.T) {
		original := ComparisonFailedEvent{
			EventMeta:     testEventMeta,
			JobID:         "job-1",
			DocumentID:    "doc-1",
			OrgID:         orgID,
			Status:        StatusFailed,
			ErrorCode:     "ERR",
			ErrorMessage:  "fail",
			FailedAtStage: "DIFF",
			IsRetryable:   false,
		}
		data, err := json.Marshal(original)
		if err != nil {
			t.Fatalf("Marshal error: %v", err)
		}
		var restored ComparisonFailedEvent
		if err := json.Unmarshal(data, &restored); err != nil {
			t.Fatalf("Unmarshal error: %v", err)
		}
		if restored.OrgID != orgID {
			t.Errorf("OrgID = %q, want %q", restored.OrgID, orgID)
		}
	})
}

// --- OrgID backward compatibility: JSON without organization_id deserializes with empty OrgID ---

func TestOrgID_BackwardCompatibility_AllOutboundEvents(t *testing.T) {
	t.Run("DocumentProcessingArtifactsReady", func(t *testing.T) {
		payload := `{"correlation_id":"c1","timestamp":"2026-03-15T14:30:00Z","job_id":"j1","document_id":"d1","version_id":"v1","ocr_raw":{"status":"not_applicable"},"text":{"document_id":"d1","pages":[]},"structure":{"document_id":"d1"},"semantic_tree":{"document_id":"d1"}}`
		var event DocumentProcessingArtifactsReady
		if err := json.Unmarshal([]byte(payload), &event); err != nil {
			t.Fatalf("Unmarshal error: %v", err)
		}
		if event.OrgID != "" {
			t.Errorf("OrgID = %q, want empty (backward compat)", event.OrgID)
		}
	})

	t.Run("GetSemanticTreeRequest", func(t *testing.T) {
		payload := `{"correlation_id":"c1","timestamp":"2026-03-15T14:30:00Z","job_id":"j1","document_id":"d1","version_id":"v1"}`
		var event GetSemanticTreeRequest
		if err := json.Unmarshal([]byte(payload), &event); err != nil {
			t.Fatalf("Unmarshal error: %v", err)
		}
		if event.OrgID != "" {
			t.Errorf("OrgID = %q, want empty (backward compat)", event.OrgID)
		}
	})

	t.Run("DocumentVersionDiffReady", func(t *testing.T) {
		payload := `{"correlation_id":"c1","timestamp":"2026-03-15T14:30:00Z","job_id":"j1","document_id":"d1","base_version_id":"v1","target_version_id":"v2","text_diffs":[],"structural_diffs":[],"text_diff_count":0,"structural_diff_count":0}`
		var event DocumentVersionDiffReady
		if err := json.Unmarshal([]byte(payload), &event); err != nil {
			t.Fatalf("Unmarshal error: %v", err)
		}
		if event.OrgID != "" {
			t.Errorf("OrgID = %q, want empty (backward compat)", event.OrgID)
		}
	})

	t.Run("StatusChangedEvent", func(t *testing.T) {
		payload := `{"correlation_id":"c1","timestamp":"2026-03-15T14:30:00Z","job_id":"j1","document_id":"d1","old_status":"QUEUED","new_status":"IN_PROGRESS"}`
		var event StatusChangedEvent
		if err := json.Unmarshal([]byte(payload), &event); err != nil {
			t.Fatalf("Unmarshal error: %v", err)
		}
		if event.OrgID != "" {
			t.Errorf("OrgID = %q, want empty (backward compat)", event.OrgID)
		}
	})

	t.Run("ProcessingCompletedEvent", func(t *testing.T) {
		payload := `{"correlation_id":"c1","timestamp":"2026-03-15T14:30:00Z","job_id":"j1","document_id":"d1","status":"COMPLETED","has_warnings":false,"warning_count":0}`
		var event ProcessingCompletedEvent
		if err := json.Unmarshal([]byte(payload), &event); err != nil {
			t.Fatalf("Unmarshal error: %v", err)
		}
		if event.OrgID != "" {
			t.Errorf("OrgID = %q, want empty (backward compat)", event.OrgID)
		}
	})

	t.Run("ProcessingFailedEvent", func(t *testing.T) {
		payload := `{"correlation_id":"c1","timestamp":"2026-03-15T14:30:00Z","job_id":"j1","document_id":"d1","status":"FAILED","error_code":"ERR","error_message":"fail","failed_at_stage":"OCR","is_retryable":true}`
		var event ProcessingFailedEvent
		if err := json.Unmarshal([]byte(payload), &event); err != nil {
			t.Fatalf("Unmarshal error: %v", err)
		}
		if event.OrgID != "" {
			t.Errorf("OrgID = %q, want empty (backward compat)", event.OrgID)
		}
	})

	t.Run("ComparisonCompletedEvent", func(t *testing.T) {
		payload := `{"correlation_id":"c1","timestamp":"2026-03-15T14:30:00Z","job_id":"j1","document_id":"d1","base_version_id":"v1","target_version_id":"v2","status":"COMPLETED","text_diff_count":0,"structural_diff_count":0}`
		var event ComparisonCompletedEvent
		if err := json.Unmarshal([]byte(payload), &event); err != nil {
			t.Fatalf("Unmarshal error: %v", err)
		}
		if event.OrgID != "" {
			t.Errorf("OrgID = %q, want empty (backward compat)", event.OrgID)
		}
	})

	t.Run("ComparisonFailedEvent", func(t *testing.T) {
		payload := `{"correlation_id":"c1","timestamp":"2026-03-15T14:30:00Z","job_id":"j1","document_id":"d1","status":"FAILED","error_code":"ERR","error_message":"fail","failed_at_stage":"DIFF","is_retryable":false}`
		var event ComparisonFailedEvent
		if err := json.Unmarshal([]byte(payload), &event); err != nil {
			t.Fatalf("Unmarshal error: %v", err)
		}
		if event.OrgID != "" {
			t.Errorf("OrgID = %q, want empty (backward compat)", event.OrgID)
		}
	})
}

// --- OrgID omitempty: empty OrgID should not appear in JSON output ---

func TestOrgID_OmittedWhenEmpty(t *testing.T) {
	events := []struct {
		name  string
		event any
	}{
		{"DocumentProcessingArtifactsReady", DocumentProcessingArtifactsReady{EventMeta: testEventMeta, JobID: "j1", DocumentID: "d1"}},
		{"GetSemanticTreeRequest", GetSemanticTreeRequest{EventMeta: testEventMeta, JobID: "j1", DocumentID: "d1", VersionID: "v1"}},
		{"DocumentVersionDiffReady", DocumentVersionDiffReady{EventMeta: testEventMeta, JobID: "j1", DocumentID: "d1", TextDiffs: []TextDiffEntry{}, StructuralDiffs: []StructuralDiffEntry{}}},
		{"StatusChangedEvent", StatusChangedEvent{EventMeta: testEventMeta, JobID: "j1", DocumentID: "d1", OldStatus: StatusQueued, NewStatus: StatusInProgress}},
		{"ProcessingCompletedEvent", ProcessingCompletedEvent{EventMeta: testEventMeta, JobID: "j1", DocumentID: "d1", Status: StatusCompleted}},
		{"ProcessingFailedEvent", ProcessingFailedEvent{EventMeta: testEventMeta, JobID: "j1", DocumentID: "d1", Status: StatusFailed, ErrorCode: "E", ErrorMessage: "m", FailedAtStage: "S"}},
		{"ComparisonCompletedEvent", ComparisonCompletedEvent{EventMeta: testEventMeta, JobID: "j1", DocumentID: "d1", Status: StatusCompleted}},
		{"ComparisonFailedEvent", ComparisonFailedEvent{EventMeta: testEventMeta, JobID: "j1", DocumentID: "d1", Status: StatusFailed, ErrorCode: "E", ErrorMessage: "m", FailedAtStage: "S"}},
	}

	for _, tc := range events {
		t.Run(tc.name, func(t *testing.T) {
			data, err := json.Marshal(tc.event)
			if err != nil {
				t.Fatalf("Marshal error: %v", err)
			}
			var raw map[string]any
			if err := json.Unmarshal(data, &raw); err != nil {
				t.Fatalf("Unmarshal to map: %v", err)
			}
			if _, exists := raw["organization_id"]; exists {
				t.Error("organization_id should be omitted when empty (omitempty)")
			}
		})
	}
}
