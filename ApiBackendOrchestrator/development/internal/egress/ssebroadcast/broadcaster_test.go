package ssebroadcast

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"contractpro/api-orchestrator/internal/infra/observability/logger"
)

// --- Mock Publisher ---

type publishedMessage struct {
	channel string
	message string
}

type mockPublisher struct {
	mu        sync.Mutex
	published []publishedMessage
	err       error
}

func (m *mockPublisher) Publish(_ context.Context, channel string, message string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return m.err
	}
	m.published = append(m.published, publishedMessage{channel: channel, message: message})
	return nil
}

func (m *mockPublisher) messages() []publishedMessage {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]publishedMessage, len(m.published))
	copy(cp, m.published)
	return cp
}

// --- Tests ---

func TestNewBroadcaster_InterfaceCompliance(t *testing.T) {
	var _ Broadcaster = (*broadcaster)(nil)
}

func TestBroadcast_Success(t *testing.T) {
	pub := &mockPublisher{}
	bc := NewBroadcaster(pub, logger.NewLogger("error"))

	event := Event{
		EventType:  "status_update",
		DocumentID: "doc-1",
		VersionID:  "ver-1",
		Status:     "PROCESSING",
		Message:    "Извлечение текста и структуры",
		Timestamp:  "2026-04-09T12:00:00Z",
	}

	err := bc.Broadcast(context.Background(), "org-1", event)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	msgs := pub.messages()
	if len(msgs) != 1 {
		t.Fatalf("expected 1 published message, got %d", len(msgs))
	}
	if msgs[0].channel != "sse:broadcast:org-1" {
		t.Errorf("expected channel sse:broadcast:org-1, got %s", msgs[0].channel)
	}

	// Verify JSON content.
	var parsed Event
	if err := json.Unmarshal([]byte(msgs[0].message), &parsed); err != nil {
		t.Fatalf("failed to parse published JSON: %v", err)
	}
	if parsed.EventType != "status_update" {
		t.Errorf("expected event_type status_update, got %s", parsed.EventType)
	}
	if parsed.DocumentID != "doc-1" {
		t.Errorf("expected document_id doc-1, got %s", parsed.DocumentID)
	}
	if parsed.Status != "PROCESSING" {
		t.Errorf("expected status PROCESSING, got %s", parsed.Status)
	}
}

func TestBroadcast_PublishError(t *testing.T) {
	pub := &mockPublisher{err: errors.New("redis: connection refused")}
	bc := NewBroadcaster(pub, logger.NewLogger("error"))

	event := Event{Status: "PROCESSING"}

	err := bc.Broadcast(context.Background(), "org-1", event)
	if err == nil {
		t.Fatal("expected error on publish failure")
	}
	if !errors.Is(err, pub.err) {
		t.Errorf("expected wrapped redis error, got %v", err)
	}
}

func TestBroadcast_AllEventFields(t *testing.T) {
	pub := &mockPublisher{}
	bc := NewBroadcaster(pub, logger.NewLogger("error"))

	event := Event{
		EventType:       "comparison_update",
		DocumentID:      "doc-1",
		VersionID:       "ver-1",
		JobID:           "job-1",
		Status:          "COMPARISON_COMPLETED",
		Message:         "Сравнение версий завершено",
		Timestamp:       "2026-04-09T12:00:00Z",
		IsRetryable:     true,
		ErrorCode:       "TREE_MISSING",
		ErrorMessage:    "Semantic tree not found",
		BaseVersionID:   "ver-base",
		TargetVersionID: "ver-target",
	}

	err := bc.Broadcast(context.Background(), "org-1", event)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	msgs := pub.messages()
	var parsed Event
	json.Unmarshal([]byte(msgs[0].message), &parsed)

	if parsed.EventType != "comparison_update" {
		t.Errorf("expected comparison_update, got %s", parsed.EventType)
	}
	if parsed.JobID != "job-1" {
		t.Errorf("expected job-1, got %s", parsed.JobID)
	}
	if !parsed.IsRetryable {
		t.Error("expected is_retryable true")
	}
	if parsed.ErrorCode != "TREE_MISSING" {
		t.Errorf("expected TREE_MISSING, got %s", parsed.ErrorCode)
	}
	if parsed.ErrorMessage != "Semantic tree not found" {
		t.Errorf("expected 'Semantic tree not found', got %s", parsed.ErrorMessage)
	}
	if parsed.BaseVersionID != "ver-base" {
		t.Errorf("expected ver-base, got %s", parsed.BaseVersionID)
	}
	if parsed.TargetVersionID != "ver-target" {
		t.Errorf("expected ver-target, got %s", parsed.TargetVersionID)
	}
}

func TestBroadcast_OmitsEmptyOptionalFields(t *testing.T) {
	pub := &mockPublisher{}
	bc := NewBroadcaster(pub, logger.NewLogger("error"))

	event := Event{
		EventType:  "status_update",
		DocumentID: "doc-1",
		Status:     "PROCESSING",
		Message:    "test",
		Timestamp:  "2026-04-09T12:00:00Z",
	}

	bc.Broadcast(context.Background(), "org-1", event)

	msgs := pub.messages()
	raw := msgs[0].message

	// Fields with omitempty should not appear in JSON.
	var m map[string]interface{}
	json.Unmarshal([]byte(raw), &m)

	for _, key := range []string{"version_id", "job_id", "error_code", "error_message", "base_version_id", "target_version_id"} {
		if _, ok := m[key]; ok {
			t.Errorf("expected %s to be omitted from JSON, but it was present", key)
		}
	}
}

func TestBroadcast_ChannelPerOrg(t *testing.T) {
	pub := &mockPublisher{}
	bc := NewBroadcaster(pub, logger.NewLogger("error"))

	event := Event{Status: "PROCESSING"}

	bc.Broadcast(context.Background(), "org-alpha", event)
	bc.Broadcast(context.Background(), "org-beta", event)

	msgs := pub.messages()
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
	if msgs[0].channel != "sse:broadcast:org-alpha" {
		t.Errorf("expected sse:broadcast:org-alpha, got %s", msgs[0].channel)
	}
	if msgs[1].channel != "sse:broadcast:org-beta" {
		t.Errorf("expected sse:broadcast:org-beta, got %s", msgs[1].channel)
	}
}

func TestBroadcast_ContextCancelled(t *testing.T) {
	pub := &mockPublisher{err: context.Canceled}
	bc := NewBroadcaster(pub, logger.NewLogger("error"))

	err := bc.Broadcast(context.Background(), "org-1", Event{Status: "PROCESSING"})
	if err == nil {
		t.Fatal("expected error on context cancellation")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

// --- Channel function tests ---

func TestChannel(t *testing.T) {
	tests := []struct {
		orgID    string
		expected string
	}{
		{"org-123", "sse:broadcast:org-123"},
		{"acme-corp", "sse:broadcast:acme-corp"},
		{"", "sse:broadcast:"},
	}

	for _, tc := range tests {
		result := Channel(tc.orgID)
		if result != tc.expected {
			t.Errorf("Channel(%q) = %q, want %q", tc.orgID, result, tc.expected)
		}
	}
}

func TestBroadcast_EmptyOrgID(t *testing.T) {
	pub := &mockPublisher{}
	bc := NewBroadcaster(pub, logger.NewLogger("error"))

	err := bc.Broadcast(context.Background(), "", Event{Status: "PROCESSING"})
	if err != nil {
		t.Fatalf("expected nil error for empty orgID, got %v", err)
	}

	msgs := pub.messages()
	if len(msgs) != 0 {
		t.Errorf("expected no publish for empty orgID, got %d", len(msgs))
	}
}

// --- Concurrent safety ---

func TestBroadcast_ConcurrentSafety(t *testing.T) {
	pub := &mockPublisher{}
	bc := NewBroadcaster(pub, logger.NewLogger("error"))

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			bc.Broadcast(context.Background(), "org-1", Event{Status: "PROCESSING"})
		}()
	}
	wg.Wait()

	msgs := pub.messages()
	if len(msgs) != 50 {
		t.Errorf("expected 50 messages, got %d", len(msgs))
	}
}

// --- Publisher interface compliance ---

func TestPublisherInterface(t *testing.T) {
	var _ Publisher = (*mockPublisher)(nil)
}
