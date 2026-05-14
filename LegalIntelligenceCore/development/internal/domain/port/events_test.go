package port

import (
	"encoding/json"
	"testing"

	"contractpro/legal-intelligence-core/internal/domain/model"
)

func TestDLQTopic_IsValid(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   DLQTopic
		want bool
	}{
		{DLQTopicInvalidMessage, true},
		{DLQTopicConsumerFailed, true},
		{DLQTopicPublishFailed, true},
		{DLQTopicAgentOutputInvalid, true},
		{DLQTopic("lic.dlq.unknown"), false},
		{DLQTopic(""), false},
	}
	for _, tc := range cases {
		if got := tc.in.IsValid(); got != tc.want {
			t.Errorf("%q.IsValid() = %v, want %v", tc.in, got, tc.want)
		}
	}
}

// TestVersionProcessingArtifactsReady_JSONRoundTrip locks the wire shape
// for the main pipeline trigger event against DM event-catalog §2.2 so a
// drift in struct tags fails this test before it reaches integration.
func TestVersionProcessingArtifactsReady_JSONRoundTrip(t *testing.T) {
	t.Parallel()
	parent := "v0-parent"
	want := VersionProcessingArtifactsReady{
		CorrelationID:   "corr-1",
		Timestamp:       "2026-05-15T10:00:00Z",
		DocumentID:      "doc-1",
		VersionID:       "v1",
		OrganizationID:  "org-1",
		ArtifactTypes:   []string{"SEMANTIC_TREE", "EXTRACTED_TEXT"},
		JobID:           "job-1",
		OriginType:      "RE_CHECK",
		ParentVersionID: &parent,
		CreatedByUserID: "user-1",
	}
	raw, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got VersionProcessingArtifactsReady
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.CorrelationID != want.CorrelationID || got.JobID != want.JobID ||
		got.OriginType != want.OriginType || got.ParentVersionID == nil ||
		*got.ParentVersionID != parent || got.CreatedByUserID != want.CreatedByUserID {
		t.Fatalf("round-trip mismatch: got=%+v want=%+v", got, want)
	}
}

// TestLICStatusChangedEvent_OmitemptyContract ensures that COMPLETED-shaped
// events omit Stage / ErrorCode / ErrorMessage / IsRetryable from the wire
// payload — the Orchestrator's `lic-status:{job_id}:{status}` dedup relies
// on schema minimality (LIC event-catalog §1.1).
func TestLICStatusChangedEvent_OmitemptyContract(t *testing.T) {
	t.Parallel()
	evt := LICStatusChangedEvent{
		CorrelationID:  "corr-1",
		Timestamp:      "2026-05-15T10:00:00Z",
		JobID:          "job-1",
		DocumentID:     "doc-1",
		VersionID:      "v1",
		OrganizationID: "org-1",
		Status:         model.StatusCompleted,
	}
	raw, err := json.Marshal(evt)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, k := range []string{"stage", "error_code", "error_message", "is_retryable"} {
		if _, present := fields[k]; present {
			t.Errorf("field %q must be omitted for COMPLETED; got payload=%s", k, raw)
		}
	}
	if got, ok := fields["status"].(string); !ok || got != string(model.StatusCompleted) {
		t.Errorf("status mismatch: got=%v", fields["status"])
	}
}

// TestLICDLQEnvelope_RawPayloadOmitted asserts that the DLQ envelope has no
// `original_message` field — the v1.1 PII-safe contract removed it in favour
// of an HMAC hash (integration-contracts.md §10.1). Any reintroduction of
// the raw payload by accident would surface here.
func TestLICDLQEnvelope_RawPayloadOmitted(t *testing.T) {
	t.Parallel()
	env := LICDLQEnvelope{
		OriginalTopic:            "dm.events.version-artifacts-ready",
		OriginalMessageHash:      "abc123",
		OriginalMessageSizeBytes: 512,
		ErrorCode:                model.ErrCodeInvalidMessageSchema,
		ErrorMessage:             "schema validation failed",
		RetryCount:               0,
		FailedAt:                 "2026-05-15T10:00:00Z",
	}
	raw, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, present := fields["original_message"]; present {
		t.Fatalf("DLQ envelope leaked raw original_message: %s", raw)
	}
	if fields["original_message_hash"] != "abc123" {
		t.Errorf("hash mismatch: %v", fields["original_message_hash"])
	}
}
