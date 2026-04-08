package commandpub

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"contractpro/api-orchestrator/internal/infra/observability/logger"
)

// ---------------------------------------------------------------------------
// Mock broker
// ---------------------------------------------------------------------------

// mockBroker captures the topic and payload of the last Publish call.
type mockBroker struct {
	topic   string
	payload []byte
	err     error // If non-nil, Publish returns this error.
}

func (m *mockBroker) Publish(_ context.Context, topic string, payload []byte) error {
	m.topic = topic
	m.payload = payload
	return m.err
}

// ---------------------------------------------------------------------------
// Compile-time interface check
// ---------------------------------------------------------------------------

func TestPublisher_ImplementsCommandPublisher(t *testing.T) {
	// This test exists solely to document the compile-time check.
	// The actual check is: var _ CommandPublisher = (*Publisher)(nil)
	var _ CommandPublisher = (*Publisher)(nil)
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func testLogger() *logger.Logger {
	return logger.NewLogger("debug")
}

func testContext(correlationID string) context.Context {
	return logger.WithRequestContext(context.Background(), logger.RequestContext{
		CorrelationID:  correlationID,
		OrganizationID: "org-001",
		UserID:         "user-001",
	})
}

// ---------------------------------------------------------------------------
// PublishProcessDocument tests
// ---------------------------------------------------------------------------

func TestPublishProcessDocument_CorrectTopicAndJSON(t *testing.T) {
	mb := &mockBroker{}
	pub := NewPublisher(mb, "dp.commands.process-document", "dp.commands.compare-versions", testLogger())

	ctx := testContext("corr-aaa")
	cmd := ProcessDocumentCommand{
		JobID:              "job-1",
		DocumentID:         "doc-1",
		VersionID:          "ver-1",
		OrganizationID:     "org-1",
		RequestedByUserID:  "user-1",
		SourceFileKey:      "uploads/abc.pdf",
		SourceFileName:     "contract.pdf",
		SourceFileSize:     12345,
		SourceFileChecksum: "sha256:deadbeef",
		SourceFileMIMEType: "application/pdf",
	}

	if err := pub.PublishProcessDocument(ctx, cmd); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify topic.
	if mb.topic != "dp.commands.process-document" {
		t.Errorf("topic = %q, want %q", mb.topic, "dp.commands.process-document")
	}

	// Verify JSON payload.
	var got processDocumentEvent
	if err := json.Unmarshal(mb.payload, &got); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}

	if got.CorrelationID != "corr-aaa" {
		t.Errorf("correlation_id = %q, want %q", got.CorrelationID, "corr-aaa")
	}
	if got.JobID != "job-1" {
		t.Errorf("job_id = %q, want %q", got.JobID, "job-1")
	}
	if got.DocumentID != "doc-1" {
		t.Errorf("document_id = %q, want %q", got.DocumentID, "doc-1")
	}
	if got.VersionID != "ver-1" {
		t.Errorf("version_id = %q, want %q", got.VersionID, "ver-1")
	}
	if got.OrganizationID != "org-1" {
		t.Errorf("organization_id = %q, want %q", got.OrganizationID, "org-1")
	}
	if got.RequestedByUserID != "user-1" {
		t.Errorf("requested_by_user_id = %q, want %q", got.RequestedByUserID, "user-1")
	}
	if got.SourceFileKey != "uploads/abc.pdf" {
		t.Errorf("source_file_key = %q, want %q", got.SourceFileKey, "uploads/abc.pdf")
	}
	if got.SourceFileName != "contract.pdf" {
		t.Errorf("source_file_name = %q, want %q", got.SourceFileName, "contract.pdf")
	}
	if got.SourceFileSize != 12345 {
		t.Errorf("source_file_size = %d, want %d", got.SourceFileSize, 12345)
	}
	if got.SourceFileChecksum != "sha256:deadbeef" {
		t.Errorf("source_file_checksum = %q, want %q", got.SourceFileChecksum, "sha256:deadbeef")
	}
	if got.SourceFileMIMEType != "application/pdf" {
		t.Errorf("source_file_mime_type = %q, want %q", got.SourceFileMIMEType, "application/pdf")
	}
}

func TestPublishProcessDocument_TimestampIsValidRFC3339(t *testing.T) {
	mb := &mockBroker{}
	pub := NewPublisher(mb, "topic.process", "topic.compare", testLogger())

	before := time.Now().UTC().Add(-time.Second)
	err := pub.PublishProcessDocument(testContext("corr-ts"), ProcessDocumentCommand{
		JobID:      "job-ts",
		DocumentID: "doc-ts",
		VersionID:  "ver-ts",
	})
	after := time.Now().UTC().Add(time.Second)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var got processDocumentEvent
	if err := json.Unmarshal(mb.payload, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	ts, parseErr := time.Parse(time.RFC3339, got.Timestamp)
	if parseErr != nil {
		t.Fatalf("timestamp %q is not valid RFC3339: %v", got.Timestamp, parseErr)
	}
	if ts.Before(before) || ts.After(after) {
		t.Errorf("timestamp %v is outside expected range [%v, %v]", ts, before, after)
	}
}

func TestPublishProcessDocument_BrokerErrorPropagated(t *testing.T) {
	brokerErr := errors.New("connection lost")
	mb := &mockBroker{err: brokerErr}
	pub := NewPublisher(mb, "topic.process", "topic.compare", testLogger())

	err := pub.PublishProcessDocument(testContext("corr-err"), ProcessDocumentCommand{
		JobID:      "job-err",
		DocumentID: "doc-err",
		VersionID:  "ver-err",
	})

	if !errors.Is(err, brokerErr) {
		t.Errorf("err = %v, want errors.Is(%v)", err, brokerErr)
	}
}

func TestPublishProcessDocument_EmptyCorrelationID(t *testing.T) {
	mb := &mockBroker{}
	pub := NewPublisher(mb, "topic.process", "topic.compare", testLogger())

	// Context with no RequestContext — correlation_id will be empty.
	err := pub.PublishProcessDocument(context.Background(), ProcessDocumentCommand{
		JobID:      "job-empty",
		DocumentID: "doc-empty",
		VersionID:  "ver-empty",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var got processDocumentEvent
	if err := json.Unmarshal(mb.payload, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.CorrelationID != "" {
		t.Errorf("correlation_id = %q, want empty string", got.CorrelationID)
	}
}

// ---------------------------------------------------------------------------
// PublishCompareVersions tests
// ---------------------------------------------------------------------------

func TestPublishCompareVersions_CorrectTopicAndJSON(t *testing.T) {
	mb := &mockBroker{}
	pub := NewPublisher(mb, "dp.commands.process-document", "dp.commands.compare-versions", testLogger())

	ctx := testContext("corr-bbb")
	cmd := CompareVersionsCommand{
		JobID:             "job-2",
		DocumentID:        "doc-2",
		OrganizationID:    "org-2",
		RequestedByUserID: "user-2",
		BaseVersionID:     "ver-base",
		TargetVersionID:   "ver-target",
	}

	if err := pub.PublishCompareVersions(ctx, cmd); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify topic.
	if mb.topic != "dp.commands.compare-versions" {
		t.Errorf("topic = %q, want %q", mb.topic, "dp.commands.compare-versions")
	}

	// Verify JSON payload.
	var got compareVersionsEvent
	if err := json.Unmarshal(mb.payload, &got); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}

	if got.CorrelationID != "corr-bbb" {
		t.Errorf("correlation_id = %q, want %q", got.CorrelationID, "corr-bbb")
	}
	if got.JobID != "job-2" {
		t.Errorf("job_id = %q, want %q", got.JobID, "job-2")
	}
	if got.DocumentID != "doc-2" {
		t.Errorf("document_id = %q, want %q", got.DocumentID, "doc-2")
	}
	if got.OrganizationID != "org-2" {
		t.Errorf("organization_id = %q, want %q", got.OrganizationID, "org-2")
	}
	if got.RequestedByUserID != "user-2" {
		t.Errorf("requested_by_user_id = %q, want %q", got.RequestedByUserID, "user-2")
	}
	if got.BaseVersionID != "ver-base" {
		t.Errorf("base_version_id = %q, want %q", got.BaseVersionID, "ver-base")
	}
	if got.TargetVersionID != "ver-target" {
		t.Errorf("target_version_id = %q, want %q", got.TargetVersionID, "ver-target")
	}
}

func TestPublishCompareVersions_TimestampIsValidRFC3339(t *testing.T) {
	mb := &mockBroker{}
	pub := NewPublisher(mb, "topic.process", "topic.compare", testLogger())

	before := time.Now().UTC().Add(-time.Second)
	err := pub.PublishCompareVersions(testContext("corr-ts2"), CompareVersionsCommand{
		JobID:      "job-ts2",
		DocumentID: "doc-ts2",
	})
	after := time.Now().UTC().Add(time.Second)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var got compareVersionsEvent
	if err := json.Unmarshal(mb.payload, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	ts, parseErr := time.Parse(time.RFC3339, got.Timestamp)
	if parseErr != nil {
		t.Fatalf("timestamp %q is not valid RFC3339: %v", got.Timestamp, parseErr)
	}
	if ts.Before(before) || ts.After(after) {
		t.Errorf("timestamp %v is outside expected range [%v, %v]", ts, before, after)
	}
}

func TestPublishCompareVersions_BrokerErrorPropagated(t *testing.T) {
	brokerErr := errors.New("broker timeout")
	mb := &mockBroker{err: brokerErr}
	pub := NewPublisher(mb, "topic.process", "topic.compare", testLogger())

	err := pub.PublishCompareVersions(testContext("corr-err2"), CompareVersionsCommand{
		JobID:      "job-err2",
		DocumentID: "doc-err2",
	})

	if !errors.Is(err, brokerErr) {
		t.Errorf("err = %v, want errors.Is(%v)", err, brokerErr)
	}
}

func TestPublishCompareVersions_EmptyCorrelationID(t *testing.T) {
	mb := &mockBroker{}
	pub := NewPublisher(mb, "topic.process", "topic.compare", testLogger())

	err := pub.PublishCompareVersions(context.Background(), CompareVersionsCommand{
		JobID:      "job-empty2",
		DocumentID: "doc-empty2",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var got compareVersionsEvent
	if err := json.Unmarshal(mb.payload, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.CorrelationID != "" {
		t.Errorf("correlation_id = %q, want empty string", got.CorrelationID)
	}
}

// ---------------------------------------------------------------------------
// JSON field completeness (round-trip)
// ---------------------------------------------------------------------------

func TestPublishProcessDocument_AllFieldsPresentInJSON(t *testing.T) {
	mb := &mockBroker{}
	pub := NewPublisher(mb, "topic.process", "topic.compare", testLogger())

	err := pub.PublishProcessDocument(testContext("corr-full"), ProcessDocumentCommand{
		JobID:              "j",
		DocumentID:         "d",
		VersionID:          "v",
		OrganizationID:     "o",
		RequestedByUserID:  "u",
		SourceFileKey:      "k",
		SourceFileName:     "n",
		SourceFileSize:     99,
		SourceFileChecksum: "c",
		SourceFileMIMEType: "m",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Unmarshal into a generic map to verify all expected keys are present.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(mb.payload, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	expectedKeys := []string{
		"correlation_id", "timestamp", "job_id", "document_id", "version_id",
		"organization_id", "requested_by_user_id", "source_file_key",
		"source_file_name", "source_file_size", "source_file_checksum",
		"source_file_mime_type",
	}
	for _, key := range expectedKeys {
		if _, ok := raw[key]; !ok {
			t.Errorf("missing JSON key %q", key)
		}
	}

	// No extra keys should be present.
	if len(raw) != len(expectedKeys) {
		t.Errorf("JSON has %d keys, want %d", len(raw), len(expectedKeys))
	}
}

func TestPublishCompareVersions_AllFieldsPresentInJSON(t *testing.T) {
	mb := &mockBroker{}
	pub := NewPublisher(mb, "topic.process", "topic.compare", testLogger())

	err := pub.PublishCompareVersions(testContext("corr-full2"), CompareVersionsCommand{
		JobID:             "j",
		DocumentID:        "d",
		OrganizationID:    "o",
		RequestedByUserID: "u",
		BaseVersionID:     "b",
		TargetVersionID:   "t",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(mb.payload, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	expectedKeys := []string{
		"correlation_id", "timestamp", "job_id", "document_id",
		"organization_id", "requested_by_user_id",
		"base_version_id", "target_version_id",
	}
	for _, key := range expectedKeys {
		if _, ok := raw[key]; !ok {
			t.Errorf("missing JSON key %q", key)
		}
	}

	if len(raw) != len(expectedKeys) {
		t.Errorf("JSON has %d keys, want %d", len(raw), len(expectedKeys))
	}
}

// ---------------------------------------------------------------------------
// BrokerPublisher interface check
// ---------------------------------------------------------------------------

func TestMockBroker_ImplementsBrokerPublisher(t *testing.T) {
	var _ BrokerPublisher = (*mockBroker)(nil)
}
