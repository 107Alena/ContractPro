package dlq

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
	ctx     context.Context
}

func (m *mockBroker) Publish(ctx context.Context, topic string, payload []byte) error {
	m.ctx = ctx
	m.topic = topic
	m.payload = payload
	return m.err
}

// Compile-time check: mockBroker satisfies BrokerPublisher.
var _ BrokerPublisher = (*mockBroker)(nil)

// --- Interface compliance ---

func TestSender_ImplementsDLQPort(t *testing.T) {
	// Compile-time check is in sender.go (var _ port.DLQPort = (*Sender)(nil)).
	// This test verifies the same thing at runtime for documentation clarity.
	var _ port.DLQPort = (*Sender)(nil)
}

// --- Constructor panics ---

func TestNewSender_PanicsOnNilBroker(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for nil broker")
		}
	}()
	NewSender(nil, config.BrokerConfig{TopicDLQ: "dp.dlq"})
}

func TestNewSender_PanicsOnEmptyTopic(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for empty TopicDLQ")
		}
	}()
	NewSender(&mockBroker{}, config.BrokerConfig{TopicDLQ: ""})
}

// --- Helpers ---

func testDLQConfig() config.BrokerConfig {
	return config.BrokerConfig{
		TopicDLQ: "dp.dlq",
	}
}

func testDLQMessage() model.DLQMessage {
	return model.DLQMessage{
		EventMeta: model.EventMeta{
			CorrelationID: "job-42",
			Timestamp:     time.Date(2026, 3, 28, 12, 0, 0, 0, time.UTC),
		},
		JobID:           "job-42",
		DocumentID:      "doc-99",
		ErrorCode:       "BROKER_FAILED",
		ErrorMessage:    "broker unavailable",
		FailedAtStage:   "saving_artifacts",
		PipelineType:    "processing",
		OriginalCommand: json.RawMessage(`{"job_id":"job-42","document_id":"doc-99"}`),
	}
}

// --- Tests: Correct topic routing ---

func TestSendToDLQ_CorrectTopic(t *testing.T) {
	broker := &mockBroker{}
	sender := NewSender(broker, testDLQConfig())

	msg := testDLQMessage()
	err := sender.SendToDLQ(context.Background(), msg)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if broker.topic != "dp.dlq" {
		t.Errorf("expected topic dp.dlq, got %s", broker.topic)
	}
}

// --- Tests: JSON format — verify all fields present ---

func TestSendToDLQ_JSONContainsAllFields(t *testing.T) {
	broker := &mockBroker{}
	sender := NewSender(broker, testDLQConfig())

	msg := testDLQMessage()
	err := sender.SendToDLQ(context.Background(), msg)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	// Unmarshal into a generic map to verify all expected fields exist.
	var fields map[string]interface{}
	if err := json.Unmarshal(broker.payload, &fields); err != nil {
		t.Fatalf("failed to unmarshal published payload: %v", err)
	}

	expectedFields := []string{
		"correlation_id", "timestamp",
		"job_id", "document_id",
		"error_code", "error_message",
		"failed_at_stage", "pipeline_type",
		"original_command",
	}
	for _, field := range expectedFields {
		if _, ok := fields[field]; !ok {
			t.Errorf("expected field %q in published JSON, but not found", field)
		}
	}
}

// --- Tests: Full JSON round-trip ---

func TestSendToDLQ_JSONRoundTrip(t *testing.T) {
	broker := &mockBroker{}
	sender := NewSender(broker, testDLQConfig())

	msg := testDLQMessage()
	err := sender.SendToDLQ(context.Background(), msg)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	// Unmarshal the published payload back into a DLQMessage.
	var roundTripped model.DLQMessage
	if err := json.Unmarshal(broker.payload, &roundTripped); err != nil {
		t.Fatalf("failed to unmarshal published payload: %v", err)
	}

	if roundTripped.CorrelationID != msg.CorrelationID {
		t.Errorf("CorrelationID: expected %s, got %s", msg.CorrelationID, roundTripped.CorrelationID)
	}
	if !roundTripped.Timestamp.Equal(msg.Timestamp) {
		t.Errorf("Timestamp: expected %v, got %v", msg.Timestamp, roundTripped.Timestamp)
	}
	if roundTripped.JobID != msg.JobID {
		t.Errorf("JobID: expected %s, got %s", msg.JobID, roundTripped.JobID)
	}
	if roundTripped.DocumentID != msg.DocumentID {
		t.Errorf("DocumentID: expected %s, got %s", msg.DocumentID, roundTripped.DocumentID)
	}
	if roundTripped.ErrorCode != msg.ErrorCode {
		t.Errorf("ErrorCode: expected %s, got %s", msg.ErrorCode, roundTripped.ErrorCode)
	}
	if roundTripped.ErrorMessage != msg.ErrorMessage {
		t.Errorf("ErrorMessage: expected %s, got %s", msg.ErrorMessage, roundTripped.ErrorMessage)
	}
	if roundTripped.FailedAtStage != msg.FailedAtStage {
		t.Errorf("FailedAtStage: expected %s, got %s", msg.FailedAtStage, roundTripped.FailedAtStage)
	}
	if roundTripped.PipelineType != msg.PipelineType {
		t.Errorf("PipelineType: expected %s, got %s", msg.PipelineType, roundTripped.PipelineType)
	}
	if string(roundTripped.OriginalCommand) != string(msg.OriginalCommand) {
		t.Errorf("OriginalCommand: expected %s, got %s", string(msg.OriginalCommand), string(roundTripped.OriginalCommand))
	}
}

// --- Tests: Broker error passthrough ---

func TestSendToDLQ_BrokerErrorPassthrough(t *testing.T) {
	brokerErr := errors.New("broker connection refused")
	broker := &mockBroker{err: brokerErr}
	sender := NewSender(broker, testDLQConfig())

	msg := testDLQMessage()
	err := sender.SendToDLQ(context.Background(), msg)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, brokerErr) {
		t.Errorf("expected broker error passthrough, got: %v", err)
	}
}

// --- Tests: Context.Canceled passthrough ---

func TestSendToDLQ_ContextCanceledPassthrough(t *testing.T) {
	broker := &mockBroker{err: context.Canceled}
	sender := NewSender(broker, testDLQConfig())

	msg := testDLQMessage()
	err := sender.SendToDLQ(context.Background(), msg)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled passthrough, got: %v", err)
	}
}

// --- Tests: Context.DeadlineExceeded passthrough ---

func TestSendToDLQ_ContextDeadlineExceededPassthrough(t *testing.T) {
	broker := &mockBroker{err: context.DeadlineExceeded}
	sender := NewSender(broker, testDLQConfig())

	msg := testDLQMessage()
	err := sender.SendToDLQ(context.Background(), msg)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("expected context.DeadlineExceeded passthrough, got: %v", err)
	}
}

// --- Tests: Context forwarding to broker ---

func TestSendToDLQ_ContextForwardedToBroker(t *testing.T) {
	broker := &mockBroker{}
	sender := NewSender(broker, testDLQConfig())

	type ctxKey string
	key := ctxKey("test-key")
	ctx := context.WithValue(context.Background(), key, "test-value")

	msg := testDLQMessage()
	err := sender.SendToDLQ(ctx, msg)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	// Verify the context passed to the broker is the same context (not replaced).
	if broker.ctx == nil {
		t.Fatal("expected broker to receive context, got nil")
	}
	val, ok := broker.ctx.Value(key).(string)
	if !ok || val != "test-value" {
		t.Errorf("expected context value 'test-value', got %q", val)
	}
}
