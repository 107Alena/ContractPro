package model

import (
	"encoding/json"
	"testing"
	"time"
)

func TestNewDocumentVersion(t *testing.T) {
	v := NewDocumentVersion(
		"ver-1", "doc-1", "org-1", 1,
		OriginTypeUpload,
		"s3://bucket/file.pdf", "contract.pdf", 1024, "sha256:abc123", "user-1",
	)

	if v.VersionID != "ver-1" {
		t.Errorf("expected version_id ver-1, got %s", v.VersionID)
	}
	if v.ArtifactStatus != ArtifactStatusPending {
		t.Errorf("expected PENDING, got %s", v.ArtifactStatus)
	}
	if v.VersionNumber != 1 {
		t.Errorf("expected version_number 1, got %d", v.VersionNumber)
	}
	if v.OriginType != OriginTypeUpload {
		t.Errorf("expected UPLOAD, got %s", v.OriginType)
	}
	if v.CreatedAt.IsZero() {
		t.Error("expected non-zero created_at")
	}
}

func TestDocumentVersionJSONRoundTrip(t *testing.T) {
	now := time.Date(2026, 3, 31, 12, 0, 0, 0, time.UTC)
	v := &DocumentVersion{
		VersionID:          "ver-123",
		DocumentID:         "doc-456",
		OrganizationID:     "org-789",
		VersionNumber:      3,
		ParentVersionID:    "ver-122",
		OriginType:         OriginTypeReUpload,
		OriginDescription:  "Повторная загрузка после правок",
		SourceFileKey:      "org-789/doc-456/ver-123/source.pdf",
		SourceFileName:     "договор_v3.pdf",
		SourceFileSize:     2048576,
		SourceFileChecksum: "sha256:def456",
		ArtifactStatus:     ArtifactStatusAnalysisArtifactsReceived,
		CreatedByUserID:    "user-111",
		CreatedAt:          now,
	}

	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var restored DocumentVersion
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if restored.VersionID != v.VersionID {
		t.Errorf("version_id mismatch: %s != %s", restored.VersionID, v.VersionID)
	}
	if restored.VersionNumber != v.VersionNumber {
		t.Errorf("version_number mismatch: %d != %d", restored.VersionNumber, v.VersionNumber)
	}
	if restored.ParentVersionID != v.ParentVersionID {
		t.Errorf("parent_version_id mismatch: %s != %s", restored.ParentVersionID, v.ParentVersionID)
	}
	if restored.OriginType != v.OriginType {
		t.Errorf("origin_type mismatch: %s != %s", restored.OriginType, v.OriginType)
	}
	if restored.OriginDescription != v.OriginDescription {
		t.Errorf("origin_description mismatch: %s != %s", restored.OriginDescription, v.OriginDescription)
	}
	if restored.ArtifactStatus != v.ArtifactStatus {
		t.Errorf("artifact_status mismatch: %s != %s", restored.ArtifactStatus, v.ArtifactStatus)
	}
	if restored.SourceFileSize != v.SourceFileSize {
		t.Errorf("source_file_size mismatch: %d != %d", restored.SourceFileSize, v.SourceFileSize)
	}
}

func TestDocumentVersionJSONOmitsEmptyFields(t *testing.T) {
	v := NewDocumentVersion(
		"ver-1", "doc-1", "org-1", 1,
		OriginTypeUpload,
		"key", "file.pdf", 1024, "sha256:abc", "user-1",
	)

	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if _, ok := raw["parent_version_id"]; ok {
		t.Error("expected parent_version_id to be omitted")
	}
	if _, ok := raw["origin_description"]; ok {
		t.Error("expected origin_description to be omitted")
	}
}

func TestArtifactStatusStateMachine(t *testing.T) {
	tests := []struct {
		from    ArtifactStatus
		to      ArtifactStatus
		wantErr bool
	}{
		// Valid transitions from PENDING
		{ArtifactStatusPending, ArtifactStatusProcessingArtifactsReceived, false},
		{ArtifactStatusPending, ArtifactStatusPartiallyAvailable, false},
		// Invalid from PENDING
		{ArtifactStatusPending, ArtifactStatusAnalysisArtifactsReceived, true},
		{ArtifactStatusPending, ArtifactStatusReportsReady, true},
		{ArtifactStatusPending, ArtifactStatusFullyReady, true},

		// Valid transitions from PROCESSING_ARTIFACTS_RECEIVED
		{ArtifactStatusProcessingArtifactsReceived, ArtifactStatusAnalysisArtifactsReceived, false},
		{ArtifactStatusProcessingArtifactsReceived, ArtifactStatusPartiallyAvailable, false},
		// Invalid from PROCESSING_ARTIFACTS_RECEIVED
		{ArtifactStatusProcessingArtifactsReceived, ArtifactStatusPending, true},
		{ArtifactStatusProcessingArtifactsReceived, ArtifactStatusReportsReady, true},

		// Valid transitions from ANALYSIS_ARTIFACTS_RECEIVED
		{ArtifactStatusAnalysisArtifactsReceived, ArtifactStatusReportsReady, false},
		{ArtifactStatusAnalysisArtifactsReceived, ArtifactStatusFullyReady, false},
		{ArtifactStatusAnalysisArtifactsReceived, ArtifactStatusPartiallyAvailable, false},
		// Invalid from ANALYSIS_ARTIFACTS_RECEIVED
		{ArtifactStatusAnalysisArtifactsReceived, ArtifactStatusPending, true},
		{ArtifactStatusAnalysisArtifactsReceived, ArtifactStatusProcessingArtifactsReceived, true},

		// Valid transitions from REPORTS_READY
		{ArtifactStatusReportsReady, ArtifactStatusFullyReady, false},
		{ArtifactStatusReportsReady, ArtifactStatusPartiallyAvailable, false},
		// Invalid from REPORTS_READY
		{ArtifactStatusReportsReady, ArtifactStatusPending, true},

		// Terminal states: no transitions
		{ArtifactStatusFullyReady, ArtifactStatusPending, true},
		{ArtifactStatusFullyReady, ArtifactStatusPartiallyAvailable, true},
		{ArtifactStatusPartiallyAvailable, ArtifactStatusPending, true},
		{ArtifactStatusPartiallyAvailable, ArtifactStatusFullyReady, true},
	}

	for _, tt := range tests {
		t.Run(string(tt.from)+"->"+string(tt.to), func(t *testing.T) {
			err := ValidateArtifactTransition(tt.from, tt.to)
			if (err != nil) != tt.wantErr {
				t.Errorf("expected error=%v, got error=%v", tt.wantErr, err)
			}
		})
	}
}

func TestDocumentVersionTransitionArtifactStatus(t *testing.T) {
	v := NewDocumentVersion(
		"ver-1", "doc-1", "org-1", 1,
		OriginTypeUpload,
		"key", "file.pdf", 1024, "sha256:abc", "user-1",
	)

	// PENDING -> PROCESSING_ARTIFACTS_RECEIVED
	if err := v.TransitionArtifactStatus(ArtifactStatusProcessingArtifactsReceived); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if v.ArtifactStatus != ArtifactStatusProcessingArtifactsReceived {
		t.Errorf("expected PROCESSING_ARTIFACTS_RECEIVED, got %s", v.ArtifactStatus)
	}

	// PROCESSING_ARTIFACTS_RECEIVED -> ANALYSIS_ARTIFACTS_RECEIVED
	if err := v.TransitionArtifactStatus(ArtifactStatusAnalysisArtifactsReceived); err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	// ANALYSIS_ARTIFACTS_RECEIVED -> FULLY_READY
	if err := v.TransitionArtifactStatus(ArtifactStatusFullyReady); err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	// Terminal: no more transitions
	if err := v.TransitionArtifactStatus(ArtifactStatusPending); err == nil {
		t.Error("expected error from terminal state, got nil")
	}
}

func TestArtifactStatusIsTerminal(t *testing.T) {
	nonTerminal := []ArtifactStatus{
		ArtifactStatusPending,
		ArtifactStatusProcessingArtifactsReceived,
		ArtifactStatusAnalysisArtifactsReceived,
		ArtifactStatusReportsReady,
	}
	for _, s := range nonTerminal {
		if s.IsTerminal() {
			t.Errorf("%s should not be terminal", s)
		}
	}

	terminal := []ArtifactStatus{
		ArtifactStatusFullyReady,
		ArtifactStatusPartiallyAvailable,
	}
	for _, s := range terminal {
		if !s.IsTerminal() {
			t.Errorf("%s should be terminal", s)
		}
	}
}

func TestAllOriginTypes(t *testing.T) {
	expected := 5
	if len(AllOriginTypes) != expected {
		t.Errorf("expected %d origin types, got %d", expected, len(AllOriginTypes))
	}
}
