package model

import (
	"encoding/json"
	"testing"
	"time"
)

func TestNewVersionDiffReference(t *testing.T) {
	diff := NewVersionDiffReference(
		"diff-1", "doc-1", "org-1", "ver-1", "ver-2",
		"org-1/doc-1/diffs/ver-1_ver-2",
		5, 3, "job-1", "corr-1",
	)

	if diff.DiffID != "diff-1" {
		t.Errorf("expected diff_id diff-1, got %s", diff.DiffID)
	}
	if diff.BaseVersionID != "ver-1" {
		t.Errorf("expected base_version_id ver-1, got %s", diff.BaseVersionID)
	}
	if diff.TargetVersionID != "ver-2" {
		t.Errorf("expected target_version_id ver-2, got %s", diff.TargetVersionID)
	}
	if diff.TextDiffCount != 5 {
		t.Errorf("expected text_diff_count 5, got %d", diff.TextDiffCount)
	}
	if diff.StructuralDiffCount != 3 {
		t.Errorf("expected structural_diff_count 3, got %d", diff.StructuralDiffCount)
	}
	if diff.CreatedAt.IsZero() {
		t.Error("expected non-zero created_at")
	}
}

func TestVersionDiffReferenceJSONRoundTrip(t *testing.T) {
	now := time.Date(2026, 3, 31, 12, 0, 0, 0, time.UTC)
	diff := &VersionDiffReference{
		DiffID:              "diff-123",
		DocumentID:          "doc-456",
		OrganizationID:      "org-789",
		BaseVersionID:       "ver-100",
		TargetVersionID:     "ver-101",
		StorageKey:          "org-789/doc-456/diffs/ver-100_ver-101",
		TextDiffCount:       12,
		StructuralDiffCount: 4,
		JobID:               "job-222",
		CorrelationID:       "corr-333",
		CreatedAt:           now,
	}

	data, err := json.Marshal(diff)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var restored VersionDiffReference
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if restored.DiffID != diff.DiffID {
		t.Errorf("diff_id mismatch: %s != %s", restored.DiffID, diff.DiffID)
	}
	if restored.BaseVersionID != diff.BaseVersionID {
		t.Errorf("base_version_id mismatch: %s != %s", restored.BaseVersionID, diff.BaseVersionID)
	}
	if restored.TargetVersionID != diff.TargetVersionID {
		t.Errorf("target_version_id mismatch: %s != %s", restored.TargetVersionID, diff.TargetVersionID)
	}
	if restored.TextDiffCount != diff.TextDiffCount {
		t.Errorf("text_diff_count mismatch: %d != %d", restored.TextDiffCount, diff.TextDiffCount)
	}
	if restored.StructuralDiffCount != diff.StructuralDiffCount {
		t.Errorf("structural_diff_count mismatch: %d != %d", restored.StructuralDiffCount, diff.StructuralDiffCount)
	}
	if restored.StorageKey != diff.StorageKey {
		t.Errorf("storage_key mismatch: %s != %s", restored.StorageKey, diff.StorageKey)
	}
	if restored.JobID != diff.JobID {
		t.Errorf("job_id mismatch: %s != %s", restored.JobID, diff.JobID)
	}
	if restored.CorrelationID != diff.CorrelationID {
		t.Errorf("correlation_id mismatch: %s != %s", restored.CorrelationID, diff.CorrelationID)
	}
}
