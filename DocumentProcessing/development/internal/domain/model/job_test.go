package model

import (
	"encoding/json"
	"testing"
	"time"
)

func TestNewProcessingJob(t *testing.T) {
	job := NewProcessingJob("job-1", "doc-1", "https://example.com/file.pdf")

	if job.JobID != "job-1" {
		t.Errorf("JobID = %q, want %q", job.JobID, "job-1")
	}
	if job.DocumentID != "doc-1" {
		t.Errorf("DocumentID = %q, want %q", job.DocumentID, "doc-1")
	}
	if job.FileURL != "https://example.com/file.pdf" {
		t.Errorf("FileURL = %q, want %q", job.FileURL, "https://example.com/file.pdf")
	}
	if job.Status != StatusQueued {
		t.Errorf("Status = %q, want %q", job.Status, StatusQueued)
	}
	if job.CreatedAt.IsZero() {
		t.Error("CreatedAt should not be zero")
	}
	if job.UpdatedAt.IsZero() {
		t.Error("UpdatedAt should not be zero")
	}
}

func TestNewComparisonJob(t *testing.T) {
	job := NewComparisonJob("job-2", "doc-1", "ver-1", "ver-2")

	if job.JobID != "job-2" {
		t.Errorf("JobID = %q, want %q", job.JobID, "job-2")
	}
	if job.DocumentID != "doc-1" {
		t.Errorf("DocumentID = %q, want %q", job.DocumentID, "doc-1")
	}
	if job.BaseVersionID != "ver-1" {
		t.Errorf("BaseVersionID = %q, want %q", job.BaseVersionID, "ver-1")
	}
	if job.TargetVersionID != "ver-2" {
		t.Errorf("TargetVersionID = %q, want %q", job.TargetVersionID, "ver-2")
	}
	if job.Status != StatusQueued {
		t.Errorf("Status = %q, want %q", job.Status, StatusQueued)
	}
	if job.CreatedAt.IsZero() {
		t.Error("CreatedAt should not be zero")
	}
	if job.UpdatedAt.IsZero() {
		t.Error("UpdatedAt should not be zero")
	}
}

func TestProcessingJob_JSONRoundTrip(t *testing.T) {
	original := &ProcessingJob{
		JobMeta: JobMeta{
			JobID:     "job-1",
			Status:    StatusInProgress,
			CreatedAt: time.Date(2026, 3, 14, 10, 0, 0, 0, time.UTC),
			UpdatedAt: time.Date(2026, 3, 14, 10, 1, 0, 0, time.UTC),
		},
		DocumentID: "doc-1",
		FileURL:    "https://example.com/file.pdf",
		Stage:      ProcessingStageOCR,
		FileName:   "contract.pdf",
		FileSize:   1024000,
		MimeType:   "application/pdf",
		Checksum:   "abc123",
		OrgID:      "org-1",
		UserID:     "user-1",
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var restored ProcessingJob
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if restored.JobID != original.JobID {
		t.Errorf("JobID = %q, want %q", restored.JobID, original.JobID)
	}
	if restored.Status != original.Status {
		t.Errorf("Status = %q, want %q", restored.Status, original.Status)
	}
	if restored.DocumentID != original.DocumentID {
		t.Errorf("DocumentID = %q, want %q", restored.DocumentID, original.DocumentID)
	}
	if restored.Stage != original.Stage {
		t.Errorf("Stage = %q, want %q", restored.Stage, original.Stage)
	}
	if restored.FileName != original.FileName {
		t.Errorf("FileName = %q, want %q", restored.FileName, original.FileName)
	}
	if restored.FileSize != original.FileSize {
		t.Errorf("FileSize = %d, want %d", restored.FileSize, original.FileSize)
	}
	if !restored.CreatedAt.Equal(original.CreatedAt) {
		t.Errorf("CreatedAt = %v, want %v", restored.CreatedAt, original.CreatedAt)
	}
}

func TestComparisonJob_JSONRoundTrip(t *testing.T) {
	original := &ComparisonJob{
		JobMeta: JobMeta{
			JobID:     "job-2",
			Status:    StatusQueued,
			CreatedAt: time.Date(2026, 3, 14, 12, 0, 0, 0, time.UTC),
			UpdatedAt: time.Date(2026, 3, 14, 12, 0, 0, 0, time.UTC),
		},
		DocumentID:      "doc-1",
		BaseVersionID:   "ver-1",
		TargetVersionID: "ver-2",
		Stage:           ComparisonStageRequestingTrees,
		OrgID:           "org-1",
		UserID:          "user-1",
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var restored ComparisonJob
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if restored.JobID != original.JobID {
		t.Errorf("JobID = %q, want %q", restored.JobID, original.JobID)
	}
	if restored.BaseVersionID != original.BaseVersionID {
		t.Errorf("BaseVersionID = %q, want %q", restored.BaseVersionID, original.BaseVersionID)
	}
	if restored.TargetVersionID != original.TargetVersionID {
		t.Errorf("TargetVersionID = %q, want %q", restored.TargetVersionID, original.TargetVersionID)
	}
	if restored.Stage != original.Stage {
		t.Errorf("Stage = %q, want %q", restored.Stage, original.Stage)
	}
}

func TestProcessingJob_JSONOmitsEmptyOptionalFields(t *testing.T) {
	job := NewProcessingJob("job-1", "doc-1", "https://example.com/file.pdf")

	data, err := json.Marshal(job)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	optionalFields := []string{"file_name", "file_size", "mime_type", "checksum", "organization_id", "requested_by_user_id"}
	for _, field := range optionalFields {
		if _, exists := raw[field]; exists {
			t.Errorf("optional field %q should be omitted when empty, but was present in JSON", field)
		}
	}
}
