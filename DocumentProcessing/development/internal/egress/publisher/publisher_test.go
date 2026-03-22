package publisher

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"contractpro/document-processing/internal/config"
	"contractpro/document-processing/internal/domain/model"
	"contractpro/document-processing/internal/domain/port"
)

// --- Mock ---

// mockBroker captures the topic and payload of the last Publish call.
type mockBroker struct {
	topic   string
	payload []byte
	err     error // if set, Publish returns this error
}

func (m *mockBroker) Publish(_ context.Context, topic string, payload []byte) error {
	m.topic = topic
	m.payload = payload
	return m.err
}

// Compile-time check: mockBroker satisfies BrokerPublisher.
var _ BrokerPublisher = (*mockBroker)(nil)

// --- Helpers ---

func testBrokerConfig() config.BrokerConfig {
	return config.BrokerConfig{
		TopicStatusChanged:       "dp.events.status-changed",
		TopicProcessingCompleted: "dp.events.processing-completed",
		TopicProcessingFailed:    "dp.events.processing-failed",
		TopicComparisonCompleted: "dp.events.comparison-completed",
		TopicComparisonFailed:    "dp.events.comparison-failed",
	}
}

func testTimestamp() time.Time {
	return time.Date(2026, 3, 22, 12, 0, 0, 0, time.UTC)
}

func testMeta() model.EventMeta {
	return model.EventMeta{
		CorrelationID: "corr-001",
		Timestamp:     testTimestamp(),
	}
}

func testStatusChangedEvent() model.StatusChangedEvent {
	return model.StatusChangedEvent{
		EventMeta:  testMeta(),
		JobID:      "job-1",
		DocumentID: "doc-1",
		OldStatus:  model.StatusQueued,
		NewStatus:  model.StatusInProgress,
		Stage:      "text_extraction",
	}
}

func testProcessingCompletedEvent() model.ProcessingCompletedEvent {
	return model.ProcessingCompletedEvent{
		EventMeta:    testMeta(),
		JobID:        "job-2",
		DocumentID:   "doc-2",
		Status:       model.StatusCompletedWithWarnings,
		HasWarnings:  true,
		WarningCount: 3,
	}
}

func testProcessingFailedEvent() model.ProcessingFailedEvent {
	return model.ProcessingFailedEvent{
		EventMeta:     testMeta(),
		JobID:         "job-3",
		DocumentID:    "doc-3",
		Status:        model.StatusFailed,
		ErrorCode:     "OCR_FAILED",
		ErrorMessage:  "OCR service timeout",
		FailedAtStage: "ocr",
		IsRetryable:   true,
	}
}

func testComparisonCompletedEvent() model.ComparisonCompletedEvent {
	return model.ComparisonCompletedEvent{
		EventMeta:           testMeta(),
		JobID:               "job-4",
		DocumentID:          "doc-4",
		BaseVersionID:       "v1",
		TargetVersionID:     "v2",
		Status:              model.StatusCompleted,
		TextDiffCount:       5,
		StructuralDiffCount: 2,
	}
}

func testComparisonFailedEvent() model.ComparisonFailedEvent {
	return model.ComparisonFailedEvent{
		EventMeta:     testMeta(),
		JobID:         "job-5",
		DocumentID:    "doc-5",
		Status:        model.StatusFailed,
		ErrorCode:     "EXTRACTION_FAILED",
		ErrorMessage:  "structure extraction error",
		FailedAtStage: "structure_extraction",
		IsRetryable:   false,
	}
}

// --- Tests ---

func TestInterfaceCompliance(t *testing.T) {
	// Compile-time check is var _ port.EventPublisherPort = (*Publisher)(nil)
	// in publisher.go. This test verifies at runtime as well.
	broker := &mockBroker{}
	pub := NewPublisher(broker, testBrokerConfig())

	var iface port.EventPublisherPort = pub
	if iface == nil {
		t.Fatal("Publisher does not satisfy EventPublisherPort")
	}
}

// --- Correct Topic Tests ---

func TestPublishStatusChanged_CorrectTopic(t *testing.T) {
	broker := &mockBroker{}
	pub := NewPublisher(broker, testBrokerConfig())

	err := pub.PublishStatusChanged(context.Background(), testStatusChangedEvent())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if broker.topic != "dp.events.status-changed" {
		t.Errorf("topic = %q, want %q", broker.topic, "dp.events.status-changed")
	}
}

func TestPublishProcessingCompleted_CorrectTopic(t *testing.T) {
	broker := &mockBroker{}
	pub := NewPublisher(broker, testBrokerConfig())

	err := pub.PublishProcessingCompleted(context.Background(), testProcessingCompletedEvent())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if broker.topic != "dp.events.processing-completed" {
		t.Errorf("topic = %q, want %q", broker.topic, "dp.events.processing-completed")
	}
}

func TestPublishProcessingFailed_CorrectTopic(t *testing.T) {
	broker := &mockBroker{}
	pub := NewPublisher(broker, testBrokerConfig())

	err := pub.PublishProcessingFailed(context.Background(), testProcessingFailedEvent())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if broker.topic != "dp.events.processing-failed" {
		t.Errorf("topic = %q, want %q", broker.topic, "dp.events.processing-failed")
	}
}

func TestPublishComparisonCompleted_CorrectTopic(t *testing.T) {
	broker := &mockBroker{}
	pub := NewPublisher(broker, testBrokerConfig())

	err := pub.PublishComparisonCompleted(context.Background(), testComparisonCompletedEvent())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if broker.topic != "dp.events.comparison-completed" {
		t.Errorf("topic = %q, want %q", broker.topic, "dp.events.comparison-completed")
	}
}

func TestPublishComparisonFailed_CorrectTopic(t *testing.T) {
	broker := &mockBroker{}
	pub := NewPublisher(broker, testBrokerConfig())

	err := pub.PublishComparisonFailed(context.Background(), testComparisonFailedEvent())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if broker.topic != "dp.events.comparison-failed" {
		t.Errorf("topic = %q, want %q", broker.topic, "dp.events.comparison-failed")
	}
}

// --- JSON Format Tests (unmarshal to map, check key fields present) ---

func TestPublishStatusChanged_JSONFormat(t *testing.T) {
	broker := &mockBroker{}
	pub := NewPublisher(broker, testBrokerConfig())

	_ = pub.PublishStatusChanged(context.Background(), testStatusChangedEvent())

	var m map[string]any
	if err := json.Unmarshal(broker.payload, &m); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	for _, key := range []string{"correlation_id", "timestamp", "job_id", "document_id", "old_status", "new_status", "stage"} {
		if _, ok := m[key]; !ok {
			t.Errorf("missing key %q in JSON payload", key)
		}
	}
}

func TestPublishProcessingCompleted_JSONFormat(t *testing.T) {
	broker := &mockBroker{}
	pub := NewPublisher(broker, testBrokerConfig())

	_ = pub.PublishProcessingCompleted(context.Background(), testProcessingCompletedEvent())

	var m map[string]any
	if err := json.Unmarshal(broker.payload, &m); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	for _, key := range []string{"correlation_id", "timestamp", "job_id", "document_id", "status", "has_warnings", "warning_count"} {
		if _, ok := m[key]; !ok {
			t.Errorf("missing key %q in JSON payload", key)
		}
	}
}

func TestPublishProcessingFailed_JSONFormat(t *testing.T) {
	broker := &mockBroker{}
	pub := NewPublisher(broker, testBrokerConfig())

	_ = pub.PublishProcessingFailed(context.Background(), testProcessingFailedEvent())

	var m map[string]any
	if err := json.Unmarshal(broker.payload, &m); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	for _, key := range []string{"correlation_id", "timestamp", "job_id", "document_id", "status", "error_code", "error_message", "failed_at_stage", "is_retryable"} {
		if _, ok := m[key]; !ok {
			t.Errorf("missing key %q in JSON payload", key)
		}
	}
}

func TestPublishComparisonCompleted_JSONFormat(t *testing.T) {
	broker := &mockBroker{}
	pub := NewPublisher(broker, testBrokerConfig())

	_ = pub.PublishComparisonCompleted(context.Background(), testComparisonCompletedEvent())

	var m map[string]any
	if err := json.Unmarshal(broker.payload, &m); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	for _, key := range []string{"correlation_id", "timestamp", "job_id", "document_id", "base_version_id", "target_version_id", "status", "text_diff_count", "structural_diff_count"} {
		if _, ok := m[key]; !ok {
			t.Errorf("missing key %q in JSON payload", key)
		}
	}
}

func TestPublishComparisonFailed_JSONFormat(t *testing.T) {
	broker := &mockBroker{}
	pub := NewPublisher(broker, testBrokerConfig())

	_ = pub.PublishComparisonFailed(context.Background(), testComparisonFailedEvent())

	var m map[string]any
	if err := json.Unmarshal(broker.payload, &m); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	for _, key := range []string{"correlation_id", "timestamp", "job_id", "document_id", "status", "error_code", "error_message", "failed_at_stage", "is_retryable"} {
		if _, ok := m[key]; !ok {
			t.Errorf("missing key %q in JSON payload", key)
		}
	}
}

// --- Round-Trip Tests (unmarshal back to concrete type, compare all fields) ---

func TestPublishStatusChanged_RoundTrip(t *testing.T) {
	broker := &mockBroker{}
	pub := NewPublisher(broker, testBrokerConfig())
	want := testStatusChangedEvent()

	_ = pub.PublishStatusChanged(context.Background(), want)

	var got model.StatusChangedEvent
	if err := json.Unmarshal(broker.payload, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.CorrelationID != want.CorrelationID {
		t.Errorf("CorrelationID = %q, want %q", got.CorrelationID, want.CorrelationID)
	}
	if !got.Timestamp.Equal(want.Timestamp) {
		t.Errorf("Timestamp = %v, want %v", got.Timestamp, want.Timestamp)
	}
	if got.JobID != want.JobID {
		t.Errorf("JobID = %q, want %q", got.JobID, want.JobID)
	}
	if got.DocumentID != want.DocumentID {
		t.Errorf("DocumentID = %q, want %q", got.DocumentID, want.DocumentID)
	}
	if got.OldStatus != want.OldStatus {
		t.Errorf("OldStatus = %q, want %q", got.OldStatus, want.OldStatus)
	}
	if got.NewStatus != want.NewStatus {
		t.Errorf("NewStatus = %q, want %q", got.NewStatus, want.NewStatus)
	}
	if got.Stage != want.Stage {
		t.Errorf("Stage = %q, want %q", got.Stage, want.Stage)
	}
}

func TestPublishProcessingCompleted_RoundTrip(t *testing.T) {
	broker := &mockBroker{}
	pub := NewPublisher(broker, testBrokerConfig())
	want := testProcessingCompletedEvent()

	_ = pub.PublishProcessingCompleted(context.Background(), want)

	var got model.ProcessingCompletedEvent
	if err := json.Unmarshal(broker.payload, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.CorrelationID != want.CorrelationID {
		t.Errorf("CorrelationID = %q, want %q", got.CorrelationID, want.CorrelationID)
	}
	if got.JobID != want.JobID {
		t.Errorf("JobID = %q, want %q", got.JobID, want.JobID)
	}
	if got.DocumentID != want.DocumentID {
		t.Errorf("DocumentID = %q, want %q", got.DocumentID, want.DocumentID)
	}
	if got.Status != want.Status {
		t.Errorf("Status = %q, want %q", got.Status, want.Status)
	}
	if got.HasWarnings != want.HasWarnings {
		t.Errorf("HasWarnings = %v, want %v", got.HasWarnings, want.HasWarnings)
	}
	if got.WarningCount != want.WarningCount {
		t.Errorf("WarningCount = %d, want %d", got.WarningCount, want.WarningCount)
	}
}

func TestPublishProcessingFailed_RoundTrip(t *testing.T) {
	broker := &mockBroker{}
	pub := NewPublisher(broker, testBrokerConfig())
	want := testProcessingFailedEvent()

	_ = pub.PublishProcessingFailed(context.Background(), want)

	var got model.ProcessingFailedEvent
	if err := json.Unmarshal(broker.payload, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.CorrelationID != want.CorrelationID {
		t.Errorf("CorrelationID = %q, want %q", got.CorrelationID, want.CorrelationID)
	}
	if got.JobID != want.JobID {
		t.Errorf("JobID = %q, want %q", got.JobID, want.JobID)
	}
	if got.DocumentID != want.DocumentID {
		t.Errorf("DocumentID = %q, want %q", got.DocumentID, want.DocumentID)
	}
	if got.Status != want.Status {
		t.Errorf("Status = %q, want %q", got.Status, want.Status)
	}
	if got.ErrorCode != want.ErrorCode {
		t.Errorf("ErrorCode = %q, want %q", got.ErrorCode, want.ErrorCode)
	}
	if got.ErrorMessage != want.ErrorMessage {
		t.Errorf("ErrorMessage = %q, want %q", got.ErrorMessage, want.ErrorMessage)
	}
	if got.FailedAtStage != want.FailedAtStage {
		t.Errorf("FailedAtStage = %q, want %q", got.FailedAtStage, want.FailedAtStage)
	}
	if got.IsRetryable != want.IsRetryable {
		t.Errorf("IsRetryable = %v, want %v", got.IsRetryable, want.IsRetryable)
	}
}

func TestPublishComparisonCompleted_RoundTrip(t *testing.T) {
	broker := &mockBroker{}
	pub := NewPublisher(broker, testBrokerConfig())
	want := testComparisonCompletedEvent()

	_ = pub.PublishComparisonCompleted(context.Background(), want)

	var got model.ComparisonCompletedEvent
	if err := json.Unmarshal(broker.payload, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.CorrelationID != want.CorrelationID {
		t.Errorf("CorrelationID = %q, want %q", got.CorrelationID, want.CorrelationID)
	}
	if got.JobID != want.JobID {
		t.Errorf("JobID = %q, want %q", got.JobID, want.JobID)
	}
	if got.DocumentID != want.DocumentID {
		t.Errorf("DocumentID = %q, want %q", got.DocumentID, want.DocumentID)
	}
	if got.BaseVersionID != want.BaseVersionID {
		t.Errorf("BaseVersionID = %q, want %q", got.BaseVersionID, want.BaseVersionID)
	}
	if got.TargetVersionID != want.TargetVersionID {
		t.Errorf("TargetVersionID = %q, want %q", got.TargetVersionID, want.TargetVersionID)
	}
	if got.Status != want.Status {
		t.Errorf("Status = %q, want %q", got.Status, want.Status)
	}
	if got.TextDiffCount != want.TextDiffCount {
		t.Errorf("TextDiffCount = %d, want %d", got.TextDiffCount, want.TextDiffCount)
	}
	if got.StructuralDiffCount != want.StructuralDiffCount {
		t.Errorf("StructuralDiffCount = %d, want %d", got.StructuralDiffCount, want.StructuralDiffCount)
	}
}

func TestPublishComparisonFailed_RoundTrip(t *testing.T) {
	broker := &mockBroker{}
	pub := NewPublisher(broker, testBrokerConfig())
	want := testComparisonFailedEvent()

	_ = pub.PublishComparisonFailed(context.Background(), want)

	var got model.ComparisonFailedEvent
	if err := json.Unmarshal(broker.payload, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.CorrelationID != want.CorrelationID {
		t.Errorf("CorrelationID = %q, want %q", got.CorrelationID, want.CorrelationID)
	}
	if got.JobID != want.JobID {
		t.Errorf("JobID = %q, want %q", got.JobID, want.JobID)
	}
	if got.DocumentID != want.DocumentID {
		t.Errorf("DocumentID = %q, want %q", got.DocumentID, want.DocumentID)
	}
	if got.Status != want.Status {
		t.Errorf("Status = %q, want %q", got.Status, want.Status)
	}
	if got.ErrorCode != want.ErrorCode {
		t.Errorf("ErrorCode = %q, want %q", got.ErrorCode, want.ErrorCode)
	}
	if got.ErrorMessage != want.ErrorMessage {
		t.Errorf("ErrorMessage = %q, want %q", got.ErrorMessage, want.ErrorMessage)
	}
	if got.FailedAtStage != want.FailedAtStage {
		t.Errorf("FailedAtStage = %q, want %q", got.FailedAtStage, want.FailedAtStage)
	}
	if got.IsRetryable != want.IsRetryable {
		t.Errorf("IsRetryable = %v, want %v", got.IsRetryable, want.IsRetryable)
	}
}

// --- Error Passthrough Tests ---

func TestPublish_BrokerError(t *testing.T) {
	brokerErr := port.NewBrokerError("connection lost", errors.New("tcp reset"))
	broker := &mockBroker{err: brokerErr}
	pub := NewPublisher(broker, testBrokerConfig())

	err := pub.PublishStatusChanged(context.Background(), testStatusChangedEvent())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, brokerErr) {
		t.Errorf("error = %v, want errors.Is to match broker error", err)
	}
	if !port.IsDomainError(err) {
		t.Errorf("expected DomainError, got %T", err)
	}
	if !port.IsRetryable(err) {
		t.Error("broker errors should be retryable")
	}
	if port.ErrorCode(err) != port.ErrCodeBrokerFailed {
		t.Errorf("ErrorCode = %q, want %q", port.ErrorCode(err), port.ErrCodeBrokerFailed)
	}
}

func TestPublish_ContextCanceled(t *testing.T) {
	broker := &mockBroker{err: context.Canceled}
	pub := NewPublisher(broker, testBrokerConfig())

	err := pub.PublishProcessingCompleted(context.Background(), testProcessingCompletedEvent())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("error = %v, want errors.Is(context.Canceled)", err)
	}
}

func TestPublish_ContextDeadlineExceeded(t *testing.T) {
	broker := &mockBroker{err: context.DeadlineExceeded}
	pub := NewPublisher(broker, testBrokerConfig())

	err := pub.PublishProcessingFailed(context.Background(), testProcessingFailedEvent())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("error = %v, want errors.Is(context.DeadlineExceeded)", err)
	}
}

// --- Marshal Error Test ---

func TestPublishJSON_MarshalError(t *testing.T) {
	broker := &mockBroker{}
	pub := NewPublisher(broker, testBrokerConfig())

	// Channels are not JSON-serializable.
	err := pub.publishJSON(context.Background(), "any-topic", make(chan int))
	if err == nil {
		t.Fatal("expected error for un-marshalable value, got nil")
	}
	if !port.IsDomainError(err) {
		t.Errorf("expected DomainError, got %T", err)
	}
	if port.ErrorCode(err) != port.ErrCodeBrokerFailed {
		t.Errorf("ErrorCode = %q, want %q", port.ErrorCode(err), port.ErrCodeBrokerFailed)
	}
	if port.IsRetryable(err) {
		t.Error("marshal errors should NOT be retryable (deterministic programming error)")
	}
}

// --- Constructor Validation Tests ---

func TestNewPublisher_NilBrokerPanics(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic for nil broker, got none")
		}
	}()
	NewPublisher(nil, testBrokerConfig())
}

func TestNewPublisher_EmptyTopicPanics(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic for empty topic, got none")
		}
	}()
	cfg := testBrokerConfig()
	cfg.TopicStatusChanged = "" // empty topic
	NewPublisher(&mockBroker{}, cfg)
}

// --- Context Forwarding Test ---

func TestPublish_ForwardsContext(t *testing.T) {
	type ctxKey struct{}
	ctx := context.WithValue(context.Background(), ctxKey{}, "test-val")

	ctxBroker := &ctxCapturingBroker{}
	pub := NewPublisher(ctxBroker, testBrokerConfig())

	_ = pub.PublishStatusChanged(ctx, testStatusChangedEvent())

	if ctxBroker.ctx == nil {
		t.Fatal("context was not captured")
	}
	if ctxBroker.ctx.Value(ctxKey{}) != "test-val" {
		t.Error("context was not forwarded to broker.Publish")
	}
}

// ctxCapturingBroker captures the context passed to Publish.
type ctxCapturingBroker struct {
	ctx context.Context
}

func (m *ctxCapturingBroker) Publish(ctx context.Context, _ string, _ []byte) error {
	m.ctx = ctx
	return nil
}

// --- OmitEmpty Test ---

func TestPublishStatusChanged_OmitsEmptyStage(t *testing.T) {
	broker := &mockBroker{}
	pub := NewPublisher(broker, testBrokerConfig())

	event := testStatusChangedEvent()
	event.Stage = "" // explicitly empty

	_ = pub.PublishStatusChanged(context.Background(), event)

	var m map[string]any
	if err := json.Unmarshal(broker.payload, &m); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if _, ok := m["stage"]; ok {
		t.Error("expected 'stage' key to be omitted when empty, but it was present")
	}
}
