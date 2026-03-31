package model

import (
	"encoding/json"
	"testing"
	"time"
)

func TestEventMetaJSONRoundTrip(t *testing.T) {
	ts := time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)
	meta := EventMeta{
		CorrelationID: "corr-abc-123",
		Timestamp:     ts,
	}

	data, err := json.Marshal(meta)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var restored EventMeta
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if restored.CorrelationID != meta.CorrelationID {
		t.Errorf("correlation_id mismatch: %s != %s", restored.CorrelationID, meta.CorrelationID)
	}
	if !restored.Timestamp.Equal(meta.Timestamp) {
		t.Errorf("timestamp mismatch: %v != %v", restored.Timestamp, meta.Timestamp)
	}
}

func TestEventMetaJSONFieldNames(t *testing.T) {
	ts := time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)
	meta := EventMeta{CorrelationID: "corr-1", Timestamp: ts}

	data, err := json.Marshal(meta)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	for _, key := range []string{"correlation_id", "timestamp"} {
		if _, ok := raw[key]; !ok {
			t.Errorf("expected JSON key %q", key)
		}
	}
}

func TestBlobReferenceJSONRoundTrip(t *testing.T) {
	ref := BlobReference{
		StorageKey:  "org-1/doc-1/ver-1/EXPORT_PDF",
		FileName:    "report.pdf",
		SizeBytes:   1048576,
		ContentHash: "sha256:deadbeef",
	}

	data, err := json.Marshal(ref)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var restored BlobReference
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if restored.StorageKey != ref.StorageKey {
		t.Errorf("storage_key mismatch: %s != %s", restored.StorageKey, ref.StorageKey)
	}
	if restored.FileName != ref.FileName {
		t.Errorf("file_name mismatch: %s != %s", restored.FileName, ref.FileName)
	}
	if restored.SizeBytes != ref.SizeBytes {
		t.Errorf("size_bytes mismatch: %d != %d", restored.SizeBytes, ref.SizeBytes)
	}
	if restored.ContentHash != ref.ContentHash {
		t.Errorf("content_hash mismatch: %s != %s", restored.ContentHash, ref.ContentHash)
	}
}
