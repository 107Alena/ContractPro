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

func TestProcessingJob_TransitionTo_ValidTransitions(t *testing.T) {
	valid := []struct {
		from JobStatus
		to   JobStatus
	}{
		{StatusQueued, StatusInProgress},
		{StatusQueued, StatusRejected},
		{StatusInProgress, StatusCompleted},
		{StatusInProgress, StatusCompletedWithWarnings},
		{StatusInProgress, StatusFailed},
		{StatusInProgress, StatusTimedOut},
		{StatusInProgress, StatusRejected},
	}

	for _, tc := range valid {
		t.Run(string(tc.from)+"->"+string(tc.to), func(t *testing.T) {
			job := NewProcessingJob("job-1", "doc-1", "https://example.com/file.pdf")
			job.Status = tc.from
			beforeTransition := job.UpdatedAt

			// Small sleep to ensure UpdatedAt changes.
			time.Sleep(time.Millisecond)

			if err := job.TransitionTo(tc.to); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if job.Status != tc.to {
				t.Errorf("Status = %q, want %q", job.Status, tc.to)
			}
			if !job.UpdatedAt.After(beforeTransition) {
				t.Error("UpdatedAt should be updated after transition")
			}
		})
	}
}

func TestProcessingJob_TransitionTo_InvalidTransitions(t *testing.T) {
	invalid := []struct {
		from JobStatus
		to   JobStatus
	}{
		{StatusQueued, StatusCompleted},
		{StatusQueued, StatusFailed},
		{StatusInProgress, StatusQueued},
		{StatusCompleted, StatusInProgress},
		{StatusFailed, StatusCompleted},
		{StatusTimedOut, StatusInProgress},
		{StatusRejected, StatusQueued},
	}

	for _, tc := range invalid {
		t.Run(string(tc.from)+"->"+string(tc.to), func(t *testing.T) {
			job := NewProcessingJob("job-1", "doc-1", "https://example.com/file.pdf")
			job.Status = tc.from
			originalStatus := job.Status
			originalUpdatedAt := job.UpdatedAt

			if err := job.TransitionTo(tc.to); err == nil {
				t.Errorf("expected error for transition %s -> %s, got nil", tc.from, tc.to)
			}
			if job.Status != originalStatus {
				t.Errorf("Status changed to %q on error, should remain %q", job.Status, originalStatus)
			}
			if !job.UpdatedAt.Equal(originalUpdatedAt) {
				t.Error("UpdatedAt should not change on failed transition")
			}
		})
	}
}

func TestComparisonJob_TransitionTo(t *testing.T) {
	job := NewComparisonJob("job-1", "doc-1", "ver-1", "ver-2")

	if err := job.TransitionTo(StatusInProgress); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if job.Status != StatusInProgress {
		t.Errorf("Status = %q, want %q", job.Status, StatusInProgress)
	}

	if err := job.TransitionTo(StatusCompleted); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if job.Status != StatusCompleted {
		t.Errorf("Status = %q, want %q", job.Status, StatusCompleted)
	}

	// Terminal status — no further transitions
	if err := job.TransitionTo(StatusInProgress); err == nil {
		t.Error("expected error for transition from terminal status, got nil")
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

func TestProcessingJob_GetOrgID(t *testing.T) {
	job := NewProcessingJob("job-1", "doc-1", "https://example.com/file.pdf")
	if job.GetOrgID() != "" {
		t.Errorf("GetOrgID() = %q, want empty for new job", job.GetOrgID())
	}
	job.OrgID = "org-test-42"
	if job.GetOrgID() != "org-test-42" {
		t.Errorf("GetOrgID() = %q, want %q", job.GetOrgID(), "org-test-42")
	}
}

func TestComparisonJob_GetOrgID(t *testing.T) {
	job := NewComparisonJob("job-2", "doc-1", "v1", "v2")
	if job.GetOrgID() != "" {
		t.Errorf("GetOrgID() = %q, want empty for new job", job.GetOrgID())
	}
	job.OrgID = "org-test-99"
	if job.GetOrgID() != "org-test-99" {
		t.Errorf("GetOrgID() = %q, want %q", job.GetOrgID(), "org-test-99")
	}
}
