package idempotency

import (
	"encoding/json"
	"strings"
	"testing"

	"contractpro/document-management/internal/domain/model"
)

// TestEncodeConfirmationSnapshot_RoundTrip verifies that an encoded confirmation
// envelope can be decoded back into the same topic + payload (DM-TASK-058).
func TestEncodeConfirmationSnapshot_RoundTrip(t *testing.T) {
	topic := model.TopicDMResponsesLICArtifactsPersisted
	event := model.LegalAnalysisArtifactsPersisted{
		EventMeta:  model.EventMeta{CorrelationID: "corr-1"},
		JobID:      "job-lic-1",
		DocumentID: "doc-1",
	}

	encoded, err := EncodeConfirmationSnapshot(topic, event)
	if err != nil {
		t.Fatalf("EncodeConfirmationSnapshot error: %v", err)
	}

	snap, err := DecodeConfirmationSnapshot(encoded)
	if err != nil {
		t.Fatalf("DecodeConfirmationSnapshot error: %v", err)
	}
	if snap.SchemaVersion != SnapshotSchemaVersion {
		t.Errorf("SchemaVersion = %q, want %q", snap.SchemaVersion, SnapshotSchemaVersion)
	}
	if snap.Topic != topic {
		t.Errorf("Topic = %q, want %q", snap.Topic, topic)
	}

	var decoded model.LegalAnalysisArtifactsPersisted
	if err := json.Unmarshal(snap.Payload, &decoded); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if decoded.JobID != event.JobID || decoded.DocumentID != event.DocumentID {
		t.Errorf("decoded event mismatch: got %+v, want %+v", decoded, event)
	}
}

func TestEncodeConfirmationSnapshot_EmptyTopic_Errors(t *testing.T) {
	_, err := EncodeConfirmationSnapshot("", struct{ X string }{X: "y"})
	if err == nil {
		t.Fatal("expected error for empty topic")
	}
	if !strings.Contains(err.Error(), "topic must not be empty") {
		t.Errorf("error = %q, want topic-empty message", err.Error())
	}
}

func TestEncodeConfirmationSnapshot_NilEvent_Errors(t *testing.T) {
	_, err := EncodeConfirmationSnapshot("topic", nil)
	if err == nil {
		t.Fatal("expected error for nil event")
	}
}

// TestEncodeConfirmationSnapshot_TypedNilPointer_Errors guards against the
// case where a typed-nil pointer marshals to JSON "null" — we must reject
// it so re-publish never emits a meaningless payload.
func TestEncodeConfirmationSnapshot_TypedNilPointer_Errors(t *testing.T) {
	var typedNil *struct{ X string }
	_, err := EncodeConfirmationSnapshot("topic", typedNil)
	if err == nil {
		t.Fatal("expected error for typed-nil payload marshaling to JSON null")
	}
	if !strings.Contains(err.Error(), "must not be empty or JSON null") {
		t.Errorf("error = %q, want null-payload guard message", err.Error())
	}
}

func TestEncodeConfirmationSnapshot_Oversize_Errors(t *testing.T) {
	// Build a payload that, after envelope wrapping, exceeds MaxSnapshotSizeBytes.
	huge := strings.Repeat("a", MaxSnapshotSizeBytes)
	_, err := EncodeConfirmationSnapshot("topic", map[string]string{"data": huge})
	if err == nil {
		t.Fatal("expected oversize error")
	}
	if !strings.Contains(err.Error(), "exceeds limit") {
		t.Errorf("error = %q, want oversize message", err.Error())
	}
}

func TestDecodeConfirmationSnapshot_EmptyString_Errors(t *testing.T) {
	if _, err := DecodeConfirmationSnapshot(""); err == nil {
		t.Fatal("expected error for empty snapshot")
	}
}

func TestDecodeConfirmationSnapshot_InvalidJSON_Errors(t *testing.T) {
	if _, err := DecodeConfirmationSnapshot("{not json"); err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestDecodeConfirmationSnapshot_MissingTopic_Errors(t *testing.T) {
	raw := `{"schema_version":"1.0","topic":"","payload":{"x":1}}`
	if _, err := DecodeConfirmationSnapshot(raw); err == nil {
		t.Fatal("expected error for missing topic")
	}
}

func TestDecodeConfirmationSnapshot_MissingPayload_Errors(t *testing.T) {
	raw := `{"schema_version":"1.0","topic":"t"}`
	if _, err := DecodeConfirmationSnapshot(raw); err == nil {
		t.Fatal("expected error for missing payload")
	}
}

// TestDecodeConfirmationSnapshot_UnknownSchemaVersion_StillSucceeds documents
// the forward-compatibility behavior: an envelope produced by a newer version
// still decodes so the consumer can re-publish best-effort during rolling
// deploys (the consumer is responsible for emitting a WARN log).
func TestDecodeConfirmationSnapshot_UnknownSchemaVersion_StillSucceeds(t *testing.T) {
	raw := `{"schema_version":"99.0","topic":"t","payload":{"x":1}}`
	snap, err := DecodeConfirmationSnapshot(raw)
	if err != nil {
		t.Fatalf("expected success on unknown schema_version, got: %v", err)
	}
	if snap.SchemaVersion != "99.0" {
		t.Errorf("SchemaVersion = %q, want %q", snap.SchemaVersion, "99.0")
	}
}
