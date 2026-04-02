package dlq

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"contractpro/document-management/internal/domain/model"
	"contractpro/document-management/internal/domain/port"
)

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

type mockBroker struct {
	mu        sync.Mutex
	published []publishCall
	publishErr error
}

type publishCall struct {
	topic   string
	payload []byte
}

func (m *mockBroker) Publish(_ context.Context, topic string, payload []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.publishErr != nil {
		return m.publishErr
	}
	m.published = append(m.published, publishCall{topic: topic, payload: payload})
	return nil
}

func (m *mockBroker) lastPublish() (publishCall, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.published) == 0 {
		return publishCall{}, false
	}
	return m.published[len(m.published)-1], true
}

type mockDLQRepo struct {
	mu        sync.Mutex
	records   []*model.DLQRecord
	insertErr error
}

func (m *mockDLQRepo) Insert(_ context.Context, record *model.DLQRecord) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.insertErr != nil {
		return m.insertErr
	}
	m.records = append(m.records, record)
	return nil
}

func (m *mockDLQRepo) FindByFilter(_ context.Context, _ port.DLQFilterParams) ([]*model.DLQRecordWithMeta, error) {
	return nil, nil
}

func (m *mockDLQRepo) IncrementReplayCount(_ context.Context, _ string) error {
	return nil
}

func (m *mockDLQRepo) lastRecord() (*model.DLQRecord, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.records) == 0 {
		return nil, false
	}
	return m.records[len(m.records)-1], true
}

type mockMetrics struct {
	mu      sync.Mutex
	reasons []string
}

func (m *mockMetrics) IncDLQMessages(reason string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.reasons = append(m.reasons, reason)
}

func (m *mockMetrics) lastReason() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.reasons) == 0 {
		return ""
	}
	return m.reasons[len(m.reasons)-1]
}

type mockLogger struct {
	mu   sync.Mutex
	msgs []string
}

func (m *mockLogger) Warn(msg string, _ ...any) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.msgs = append(m.msgs, msg)
}

func (m *mockLogger) Error(msg string, _ ...any) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.msgs = append(m.msgs, msg)
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestNewSender_PanicOnNilDeps(t *testing.T) {
	broker := &mockBroker{}
	repo := &mockDLQRepo{}
	metrics := &mockMetrics{}
	logger := &mockLogger{}

	cases := []struct {
		name    string
		factory func()
	}{
		{"nil broker", func() { NewSender(nil, repo, metrics, logger, "a", "b", "c") }},
		{"nil repo", func() { NewSender(broker, nil, metrics, logger, "a", "b", "c") }},
		{"nil metrics", func() { NewSender(broker, repo, nil, logger, "a", "b", "c") }},
		{"nil logger", func() { NewSender(broker, repo, metrics, nil, "a", "b", "c") }},
		{"empty ingestion topic", func() { NewSender(broker, repo, metrics, logger, "", "b", "c") }},
		{"empty query topic", func() { NewSender(broker, repo, metrics, logger, "a", "", "c") }},
		{"empty invalid topic", func() { NewSender(broker, repo, metrics, logger, "a", "b", "") }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r == nil {
					t.Error("expected panic, got none")
				}
			}()
			tc.factory()
		})
	}
}

func TestNewSender_Success(t *testing.T) {
	s := NewSender(&mockBroker{}, &mockDLQRepo{}, &mockMetrics{}, &mockLogger{},
		"dm.dlq.ingestion-failed", "dm.dlq.query-failed", "dm.dlq.invalid-message")
	if s == nil {
		t.Fatal("expected non-nil sender")
	}
}

func TestSender_InterfaceCompliance(t *testing.T) {
	var _ port.DLQPort = (*Sender)(nil)
}

func TestSender_SendToDLQ_IngestionCategory(t *testing.T) {
	broker := &mockBroker{}
	repo := &mockDLQRepo{}
	metrics := &mockMetrics{}
	s := NewSender(broker, repo, metrics, &mockLogger{},
		"dm.dlq.ingestion-failed", "dm.dlq.query-failed", "dm.dlq.invalid-message")

	record := model.DLQRecord{
		OriginalTopic:   "dp.artifacts.processing-ready",
		OriginalMessage: json.RawMessage(`{"job_id":"j1"}`),
		ErrorCode:       "DOCUMENT_NOT_FOUND",
		ErrorMessage:    "document not found",
		CorrelationID:   "corr-1",
		JobID:           "j1",
		FailedAt:        time.Now().UTC(),
		Category:        model.DLQCategoryIngestion,
	}

	err := s.SendToDLQ(context.Background(), record)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	pub, ok := broker.lastPublish()
	if !ok {
		t.Fatal("expected publish call")
	}
	if pub.topic != "dm.dlq.ingestion-failed" {
		t.Errorf("topic = %q, want dm.dlq.ingestion-failed", pub.topic)
	}

	if metrics.lastReason() != "ingestion" {
		t.Errorf("metric reason = %q, want ingestion", metrics.lastReason())
	}

	_, ok = repo.lastRecord()
	if !ok {
		t.Error("expected DLQ record persisted to DB")
	}
}

func TestSender_SendToDLQ_QueryCategory(t *testing.T) {
	broker := &mockBroker{}
	s := NewSender(broker, &mockDLQRepo{}, &mockMetrics{}, &mockLogger{},
		"dm.dlq.ingestion-failed", "dm.dlq.query-failed", "dm.dlq.invalid-message")

	record := model.DLQRecord{
		OriginalMessage: json.RawMessage(`{}`),
		FailedAt:        time.Now().UTC(),
		Category:        model.DLQCategoryQuery,
	}

	_ = s.SendToDLQ(context.Background(), record)

	pub, ok := broker.lastPublish()
	if !ok {
		t.Fatal("expected publish call")
	}
	if pub.topic != "dm.dlq.query-failed" {
		t.Errorf("topic = %q, want dm.dlq.query-failed", pub.topic)
	}
}

func TestSender_SendToDLQ_InvalidCategory(t *testing.T) {
	broker := &mockBroker{}
	s := NewSender(broker, &mockDLQRepo{}, &mockMetrics{}, &mockLogger{},
		"dm.dlq.ingestion-failed", "dm.dlq.query-failed", "dm.dlq.invalid-message")

	record := model.DLQRecord{
		OriginalMessage: json.RawMessage(`{}`),
		FailedAt:        time.Now().UTC(),
		Category:        model.DLQCategoryInvalid,
	}

	_ = s.SendToDLQ(context.Background(), record)

	pub, ok := broker.lastPublish()
	if !ok {
		t.Fatal("expected publish call")
	}
	if pub.topic != "dm.dlq.invalid-message" {
		t.Errorf("topic = %q, want dm.dlq.invalid-message", pub.topic)
	}
}

func TestSender_SendToDLQ_DefaultsToIngestion(t *testing.T) {
	broker := &mockBroker{}
	s := NewSender(broker, &mockDLQRepo{}, &mockMetrics{}, &mockLogger{},
		"dm.dlq.ingestion-failed", "dm.dlq.query-failed", "dm.dlq.invalid-message")

	record := model.DLQRecord{
		OriginalMessage: json.RawMessage(`{}`),
		FailedAt:        time.Now().UTC(),
		Category:        "unknown",
	}

	_ = s.SendToDLQ(context.Background(), record)

	pub, _ := broker.lastPublish()
	if pub.topic != "dm.dlq.ingestion-failed" {
		t.Errorf("unknown category should default to ingestion, got %q", pub.topic)
	}
}

func TestSender_SendToDLQ_DBInsertError_ContinuesPublish(t *testing.T) {
	broker := &mockBroker{}
	repo := &mockDLQRepo{insertErr: errors.New("db down")}
	logger := &mockLogger{}
	s := NewSender(broker, repo, &mockMetrics{}, logger,
		"dm.dlq.ingestion-failed", "dm.dlq.query-failed", "dm.dlq.invalid-message")

	record := model.DLQRecord{
		OriginalMessage: json.RawMessage(`{}`),
		FailedAt:        time.Now().UTC(),
		Category:        model.DLQCategoryIngestion,
	}

	err := s.SendToDLQ(context.Background(), record)
	if err != nil {
		t.Fatalf("expected no error (DB failure is non-fatal), got: %v", err)
	}

	// Broker publish should still happen.
	if _, ok := broker.lastPublish(); !ok {
		t.Error("expected broker publish even when DB insert fails")
	}
}

func TestSender_SendToDLQ_BrokerPublishError_NonFatal(t *testing.T) {
	broker := &mockBroker{publishErr: errors.New("broker down")}
	logger := &mockLogger{}
	s := NewSender(broker, &mockDLQRepo{}, &mockMetrics{}, logger,
		"dm.dlq.ingestion-failed", "dm.dlq.query-failed", "dm.dlq.invalid-message")

	record := model.DLQRecord{
		OriginalMessage: json.RawMessage(`{}`),
		FailedAt:        time.Now().UTC(),
		Category:        model.DLQCategoryIngestion,
	}

	// Broker failure is non-fatal — DB persistence is the source of truth.
	err := s.SendToDLQ(context.Background(), record)
	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}
}

func TestSender_SendToDLQ_JSONRoundTrip(t *testing.T) {
	broker := &mockBroker{}
	s := NewSender(broker, &mockDLQRepo{}, &mockMetrics{}, &mockLogger{},
		"dm.dlq.ingestion-failed", "dm.dlq.query-failed", "dm.dlq.invalid-message")

	original := json.RawMessage(`{"correlation_id":"corr-1","job_id":"j1"}`)
	record := model.DLQRecord{
		OriginalTopic:   "dp.artifacts.processing-ready",
		OriginalMessage: original,
		ErrorCode:       "DOCUMENT_NOT_FOUND",
		ErrorMessage:    "not found",
		CorrelationID:   "corr-1",
		JobID:           "j1",
		FailedAt:        time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC),
		Category:        model.DLQCategoryIngestion,
	}

	_ = s.SendToDLQ(context.Background(), record)

	pub, _ := broker.lastPublish()
	var restored model.DLQRecord
	if err := json.Unmarshal(pub.payload, &restored); err != nil {
		t.Fatalf("failed to unmarshal published payload: %v", err)
	}
	if restored.JobID != "j1" {
		t.Errorf("job_id = %q, want j1", restored.JobID)
	}
	if restored.Category != model.DLQCategoryIngestion {
		t.Errorf("category = %q, want ingestion", restored.Category)
	}
	if string(restored.OriginalMessage) != string(original) {
		t.Error("original_message was modified during publish")
	}
}

func TestSender_SendToDLQ_ContextCancelled(t *testing.T) {
	broker := &mockBroker{publishErr: context.Canceled}
	s := NewSender(broker, &mockDLQRepo{}, &mockMetrics{}, &mockLogger{},
		"dm.dlq.ingestion-failed", "dm.dlq.query-failed", "dm.dlq.invalid-message")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	record := model.DLQRecord{
		OriginalMessage: json.RawMessage(`{}`),
		FailedAt:        time.Now().UTC(),
		Category:        model.DLQCategoryIngestion,
	}

	// Context cancellation should not return an error — broker failure is non-fatal.
	err := s.SendToDLQ(ctx, record)
	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}
}
