package dmclient

import (
	"encoding/json"
	"strings"
	"testing"
)

// CreateVersionRequest carries JobID as the Orchestrator-generated UUID v4
// correlation key. The field MUST be omitempty so requests issued before
// DM-TASK-054 stays wire-compatible with DM deployments that don't yet read
// the key.

func TestCreateVersionRequest_JobIDOmittedWhenEmpty(t *testing.T) {
	req := CreateVersionRequest{
		SourceFileKey:      "uploads/org/doc/file.pdf",
		SourceFileName:     "contract.pdf",
		SourceFileSize:     1024,
		SourceFileChecksum: "abc123",
		OriginType:         "UPLOAD",
	}

	b, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got := string(b)

	if strings.Contains(got, "job_id") {
		t.Fatalf("serialized payload contains job_id when JobID is empty: %s", got)
	}

	var decoded map[string]any
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, present := decoded["job_id"]; present {
		t.Fatalf("decoded payload contains job_id key: %v", decoded)
	}
}

func TestCreateVersionRequest_JobIDPresentWhenSet(t *testing.T) {
	const jobID = "8a3f5d44-6e2c-4f31-b9f0-7d2a1c4b5e90"
	req := CreateVersionRequest{
		JobID:              jobID,
		SourceFileKey:      "uploads/org/doc/file.pdf",
		SourceFileName:     "contract.pdf",
		SourceFileSize:     1024,
		SourceFileChecksum: "abc123",
		OriginType:         "UPLOAD",
	}

	b, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	v, present := decoded["job_id"]
	if !present {
		t.Fatalf("job_id missing from serialized payload: %s", string(b))
	}
	if v != jobID {
		t.Fatalf("job_id = %v, want %q", v, jobID)
	}
}
