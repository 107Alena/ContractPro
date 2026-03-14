package model

import (
	"encoding/json"
	"testing"
)

func TestTemporaryArtifacts_JSONRoundTrip(t *testing.T) {
	arts := TemporaryArtifacts{
		JobID: "job-abc-123",
		StorageKeys: []string{
			"jobs/job-abc-123/source.pdf",
			"jobs/job-abc-123/ocr_raw.json",
			"jobs/job-abc-123/extracted_text.json",
		},
	}

	data, err := json.Marshal(arts)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var got TemporaryArtifacts
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if got.JobID != arts.JobID {
		t.Errorf("JobID = %q, want %q", got.JobID, arts.JobID)
	}
	if len(got.StorageKeys) != 3 {
		t.Fatalf("StorageKeys count = %d, want 3", len(got.StorageKeys))
	}
	for i, key := range arts.StorageKeys {
		if got.StorageKeys[i] != key {
			t.Errorf("StorageKeys[%d] = %q, want %q", i, got.StorageKeys[i], key)
		}
	}
}

func TestTemporaryArtifacts_AddKey(t *testing.T) {
	arts := TemporaryArtifacts{JobID: "job-001"}

	if arts.HasKeys() {
		t.Error("HasKeys should return false for empty artifacts")
	}

	arts.AddKey("jobs/job-001/source.pdf")
	arts.AddKey("jobs/job-001/ocr_raw.json")

	if !arts.HasKeys() {
		t.Error("HasKeys should return true after AddKey")
	}
	if len(arts.StorageKeys) != 2 {
		t.Errorf("StorageKeys count = %d, want 2", len(arts.StorageKeys))
	}
	if arts.StorageKeys[0] != "jobs/job-001/source.pdf" {
		t.Errorf("StorageKeys[0] = %q", arts.StorageKeys[0])
	}
}

func TestTemporaryArtifacts_HasKeysEmpty(t *testing.T) {
	arts := TemporaryArtifacts{
		JobID:       "job-002",
		StorageKeys: []string{},
	}

	if arts.HasKeys() {
		t.Error("HasKeys should return false for empty slice")
	}
}

func TestTemporaryArtifacts_JSONEmptyKeys(t *testing.T) {
	arts := TemporaryArtifacts{
		JobID:       "job-003",
		StorageKeys: []string{},
	}

	data, err := json.Marshal(arts)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var got TemporaryArtifacts
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if got.JobID != "job-003" {
		t.Errorf("JobID = %q, want %q", got.JobID, "job-003")
	}
}
