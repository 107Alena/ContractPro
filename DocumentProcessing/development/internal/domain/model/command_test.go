package model

import (
	"encoding/json"
	"testing"
)

func TestProcessDocumentCommand_JSONRoundTrip(t *testing.T) {
	original := ProcessDocumentCommand{
		JobID:      "job-100",
		DocumentID: "doc-200",
		VersionID:  "ver-300",
		FileURL:    "https://storage.example.com/contracts/file.pdf",
		OrgID:      "org-1",
		UserID:     "user-1",
		FileName:   "contract.pdf",
		FileSize:   2048000,
		MimeType:   "application/pdf",
		Checksum:   "sha256:abc123",
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var restored ProcessDocumentCommand
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
	if restored.FileURL != original.FileURL {
		t.Errorf("FileURL = %q, want %q", restored.FileURL, original.FileURL)
	}
	if restored.OrgID != original.OrgID {
		t.Errorf("OrgID = %q, want %q", restored.OrgID, original.OrgID)
	}
	if restored.UserID != original.UserID {
		t.Errorf("UserID = %q, want %q", restored.UserID, original.UserID)
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

func TestProcessDocumentCommand_JSONOmitsEmptyOptionalFields(t *testing.T) {
	cmd := ProcessDocumentCommand{
		JobID:      "job-1",
		DocumentID: "doc-1",
		FileURL:    "https://example.com/file.pdf",
	}

	data, err := json.Marshal(cmd)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	optionalFields := []string{"organization_id", "requested_by_user_id", "file_name", "file_size", "mime_type", "checksum"}
	for _, field := range optionalFields {
		if _, exists := raw[field]; exists {
			t.Errorf("optional field %q should be omitted when empty, but was present in JSON", field)
		}
	}

	requiredFields := []string{"job_id", "document_id", "version_id", "file_url"}
	for _, field := range requiredFields {
		if _, exists := raw[field]; !exists {
			t.Errorf("required field %q should be present in JSON, but was missing", field)
		}
	}
}

func TestCompareVersionsCommand_JSONRoundTrip(t *testing.T) {
	original := CompareVersionsCommand{
		JobID:           "job-300",
		DocumentID:      "doc-200",
		BaseVersionID:   "ver-1",
		TargetVersionID: "ver-2",
		OrgID:           "org-1",
		UserID:          "user-1",
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var restored CompareVersionsCommand
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if restored.JobID != original.JobID {
		t.Errorf("JobID = %q, want %q", restored.JobID, original.JobID)
	}
	if restored.DocumentID != original.DocumentID {
		t.Errorf("DocumentID = %q, want %q", restored.DocumentID, original.DocumentID)
	}
	if restored.BaseVersionID != original.BaseVersionID {
		t.Errorf("BaseVersionID = %q, want %q", restored.BaseVersionID, original.BaseVersionID)
	}
	if restored.TargetVersionID != original.TargetVersionID {
		t.Errorf("TargetVersionID = %q, want %q", restored.TargetVersionID, original.TargetVersionID)
	}
	if restored.OrgID != original.OrgID {
		t.Errorf("OrgID = %q, want %q", restored.OrgID, original.OrgID)
	}
	if restored.UserID != original.UserID {
		t.Errorf("UserID = %q, want %q", restored.UserID, original.UserID)
	}
}

func TestCompareVersionsCommand_JSONOmitsEmptyOptionalFields(t *testing.T) {
	cmd := CompareVersionsCommand{
		JobID:           "job-1",
		DocumentID:      "doc-1",
		BaseVersionID:   "ver-1",
		TargetVersionID: "ver-2",
	}

	data, err := json.Marshal(cmd)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	optionalFields := []string{"organization_id", "requested_by_user_id"}
	for _, field := range optionalFields {
		if _, exists := raw[field]; exists {
			t.Errorf("optional field %q should be omitted when empty, but was present in JSON", field)
		}
	}

	requiredFields := []string{"job_id", "document_id", "base_version_id", "target_version_id"}
	for _, field := range requiredFields {
		if _, exists := raw[field]; !exists {
			t.Errorf("required field %q should be present in JSON, but was missing", field)
		}
	}
}
