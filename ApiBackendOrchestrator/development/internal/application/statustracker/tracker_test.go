package statustracker

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"contractpro/api-orchestrator/internal/egress/ssebroadcast"
	"contractpro/api-orchestrator/internal/infra/kvstore"
	"contractpro/api-orchestrator/internal/infra/observability/logger"
	"contractpro/api-orchestrator/internal/ingress/consumer"
)

// --- Mock KVStore ---

type mockKVStore struct {
	mu     sync.Mutex
	store  map[string]string
	getErr error
	setErr error
	setTTL time.Duration // captures the last Set TTL
}

func newMockKVStore() *mockKVStore {
	return &mockKVStore{store: make(map[string]string)}
}

func (m *mockKVStore) Get(_ context.Context, key string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.getErr != nil {
		return "", m.getErr
	}
	val, ok := m.store[key]
	if !ok {
		return "", kvstore.ErrKeyNotFound
	}
	return val, nil
}

func (m *mockKVStore) Set(_ context.Context, key string, value string, ttl time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.setErr != nil {
		return m.setErr
	}
	m.store[key] = value
	m.setTTL = ttl
	return nil
}

func (m *mockKVStore) getStored(key string) (string, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	val, ok := m.store[key]
	return val, ok
}

func (m *mockKVStore) setStored(key, value string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.store[key] = value
}

// --- Mock Broadcaster ---

type broadcastedEvent struct {
	orgID string
	event ssebroadcast.Event
}

type mockBroadcaster struct {
	mu     sync.Mutex
	events []broadcastedEvent
	err    error
}

func newMockBroadcaster() *mockBroadcaster {
	return &mockBroadcaster{}
}

func (m *mockBroadcaster) Broadcast(_ context.Context, orgID string, event ssebroadcast.Event) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return m.err
	}
	m.events = append(m.events, broadcastedEvent{orgID: orgID, event: event})
	return nil
}

func (m *mockBroadcaster) broadcastedEvents() []broadcastedEvent {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]broadcastedEvent, len(m.events))
	copy(cp, m.events)
	return cp
}

// --- Test Helpers ---

var fixedTime = time.Date(2026, 4, 9, 12, 0, 0, 0, time.UTC)

func fixedNow() time.Time { return fixedTime }

func newTestTracker(kv *mockKVStore, bc *mockBroadcaster) *Tracker {
	t := NewTracker(kv, bc, logger.NewLogger("error"))
	t.now = fixedNow
	return t
}

func mustMarshalRecord(status string) string {
	rec := statusRecord{Status: status, UpdatedAt: fixedTime.Format(time.RFC3339)}
	data, _ := json.Marshal(rec)
	return string(data)
}

func boolPtr(b bool) *bool { return &b }

// parseBroadcastedEvent returns the first broadcasted event.
func parseBroadcastedEvent(t *testing.T, bc *mockBroadcaster) ssebroadcast.Event {
	t.Helper()
	events := bc.broadcastedEvents()
	if len(events) == 0 {
		t.Fatal("expected at least one broadcasted event, got 0")
	}
	return events[0].event
}

// --- Status ordering tests ---

func TestIsTerminal(t *testing.T) {
	terminal := []UserStatus{StatusReady, StatusFailed, StatusAnalysisFailed, StatusReportsFailed, StatusPartiallyFailed, StatusRejected}
	nonTerminal := []UserStatus{StatusUploaded, StatusQueued, StatusProcessing, StatusAnalyzing, StatusGeneratingReports}

	for _, s := range terminal {
		if !isTerminal(s) {
			t.Errorf("expected %s to be terminal", s)
		}
	}
	for _, s := range nonTerminal {
		if isTerminal(s) {
			t.Errorf("expected %s to be non-terminal", s)
		}
	}
}

func TestIsForwardTransition_HappyPath(t *testing.T) {
	transitions := []struct{ from, to UserStatus }{
		{StatusUploaded, StatusQueued},
		{StatusQueued, StatusProcessing},
		{StatusProcessing, StatusAnalyzing},
		{StatusAnalyzing, StatusGeneratingReports},
		{StatusGeneratingReports, StatusReady},
		{StatusUploaded, StatusReady}, // skip multiple steps
	}
	for _, tc := range transitions {
		if !isForwardTransition(tc.from, tc.to) {
			t.Errorf("expected forward: %s → %s", tc.from, tc.to)
		}
	}
}

func TestIsForwardTransition_Backward(t *testing.T) {
	if isForwardTransition(StatusProcessing, StatusQueued) {
		t.Error("PROCESSING → QUEUED should not be forward")
	}
	if isForwardTransition(StatusAnalyzing, StatusProcessing) {
		t.Error("ANALYZING → PROCESSING should not be forward")
	}
}

func TestIsForwardTransition_SameStatus(t *testing.T) {
	if isForwardTransition(StatusProcessing, StatusProcessing) {
		t.Error("PROCESSING → PROCESSING should not be forward")
	}
}

func TestIsForwardTransition_FailureStatus(t *testing.T) {
	// Failure statuses are not in statusOrder — isForwardTransition returns false.
	if isForwardTransition(StatusProcessing, StatusFailed) {
		t.Error("PROCESSING → FAILED should not be a forward transition (failure handled by canTransition)")
	}
}

func TestCanTransition_ForwardHappyPath(t *testing.T) {
	if !canTransition(StatusQueued, StatusProcessing) {
		t.Error("QUEUED → PROCESSING should be allowed")
	}
}

func TestCanTransition_BackwardBlocked(t *testing.T) {
	if canTransition(StatusAnalyzing, StatusProcessing) {
		t.Error("ANALYZING → PROCESSING should be blocked")
	}
}

func TestCanTransition_TerminalCurrentBlocked(t *testing.T) {
	if canTransition(StatusFailed, StatusProcessing) {
		t.Error("FAILED → PROCESSING should be blocked (terminal)")
	}
	if canTransition(StatusReady, StatusFailed) {
		t.Error("READY → FAILED should be blocked (terminal)")
	}
}

func TestCanTransition_FailureOverridesNonTerminal(t *testing.T) {
	tests := []struct{ from, to UserStatus }{
		{StatusProcessing, StatusFailed},
		{StatusAnalyzing, StatusAnalysisFailed},
		{StatusGeneratingReports, StatusReportsFailed},
		{StatusQueued, StatusPartiallyFailed},
		{StatusUploaded, StatusRejected},
	}
	for _, tc := range tests {
		if !canTransition(tc.from, tc.to) {
			t.Errorf("failure override should be allowed: %s → %s", tc.from, tc.to)
		}
	}
}

// --- tryTransition tests ---

func TestTryTransition_FirstWrite(t *testing.T) {
	kv := newMockKVStore()
	bc := newMockBroadcaster()
	tr := newTestTracker(kv, bc)

	ok, err := tr.tryTransition(context.Background(), "org1", "doc1", "ver1", StatusProcessing)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected transition to succeed on first write")
	}

	raw, found := kv.getStored("status:org1:doc1:ver1")
	if !found {
		t.Fatal("expected status key to be written")
	}
	var rec statusRecord
	json.Unmarshal([]byte(raw), &rec)
	if rec.Status != "PROCESSING" {
		t.Errorf("expected PROCESSING, got %s", rec.Status)
	}
}

func TestTryTransition_ForwardTransition(t *testing.T) {
	kv := newMockKVStore()
	kv.setStored("status:org1:doc1:ver1", mustMarshalRecord("PROCESSING"))
	bc := newMockBroadcaster()
	tr := newTestTracker(kv, bc)

	ok, err := tr.tryTransition(context.Background(), "org1", "doc1", "ver1", StatusAnalyzing)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected forward transition to succeed")
	}

	raw, _ := kv.getStored("status:org1:doc1:ver1")
	var rec statusRecord
	json.Unmarshal([]byte(raw), &rec)
	if rec.Status != "ANALYZING" {
		t.Errorf("expected ANALYZING, got %s", rec.Status)
	}
}

func TestTryTransition_BackwardSkipped(t *testing.T) {
	kv := newMockKVStore()
	kv.setStored("status:org1:doc1:ver1", mustMarshalRecord("ANALYZING"))
	bc := newMockBroadcaster()
	tr := newTestTracker(kv, bc)

	ok, err := tr.tryTransition(context.Background(), "org1", "doc1", "ver1", StatusProcessing)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatal("expected backward transition to be skipped")
	}

	// Verify status unchanged.
	raw, _ := kv.getStored("status:org1:doc1:ver1")
	var rec statusRecord
	json.Unmarshal([]byte(raw), &rec)
	if rec.Status != "ANALYZING" {
		t.Errorf("expected ANALYZING unchanged, got %s", rec.Status)
	}
}

func TestTryTransition_TerminalSkipped(t *testing.T) {
	kv := newMockKVStore()
	kv.setStored("status:org1:doc1:ver1", mustMarshalRecord("FAILED"))
	bc := newMockBroadcaster()
	tr := newTestTracker(kv, bc)

	ok, err := tr.tryTransition(context.Background(), "org1", "doc1", "ver1", StatusAnalyzing)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatal("expected transition from terminal to be skipped")
	}
}

func TestTryTransition_FailureOverridesProgress(t *testing.T) {
	kv := newMockKVStore()
	kv.setStored("status:org1:doc1:ver1", mustMarshalRecord("ANALYZING"))
	bc := newMockBroadcaster()
	tr := newTestTracker(kv, bc)

	ok, err := tr.tryTransition(context.Background(), "org1", "doc1", "ver1", StatusAnalysisFailed)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected failure override to succeed")
	}

	raw, _ := kv.getStored("status:org1:doc1:ver1")
	var rec statusRecord
	json.Unmarshal([]byte(raw), &rec)
	if rec.Status != "ANALYSIS_FAILED" {
		t.Errorf("expected ANALYSIS_FAILED, got %s", rec.Status)
	}
}

func TestTryTransition_RedisGetError(t *testing.T) {
	kv := newMockKVStore()
	kv.getErr = errors.New("redis: connection refused")
	bc := newMockBroadcaster()
	tr := newTestTracker(kv, bc)

	ok, err := tr.tryTransition(context.Background(), "org1", "doc1", "ver1", StatusProcessing)
	if err == nil {
		t.Fatal("expected error on Redis failure")
	}
	if ok {
		t.Fatal("expected no transition on error")
	}
}

func TestTryTransition_RedisSetError(t *testing.T) {
	kv := newMockKVStore()
	kv.setErr = errors.New("redis: connection refused")
	bc := newMockBroadcaster()
	tr := newTestTracker(kv, bc)

	ok, err := tr.tryTransition(context.Background(), "org1", "doc1", "ver1", StatusProcessing)
	if err == nil {
		t.Fatal("expected error on Set failure")
	}
	if ok {
		t.Fatal("expected no transition on error")
	}
}

func TestTryTransition_KeyPattern(t *testing.T) {
	kv := newMockKVStore()
	bc := newMockBroadcaster()
	tr := newTestTracker(kv, bc)

	tr.tryTransition(context.Background(), "org-abc", "doc-123", "ver-456", StatusProcessing)

	_, found := kv.getStored("status:org-abc:doc-123:ver-456")
	if !found {
		t.Fatal("expected key pattern status:{org}:{doc}:{ver}")
	}
}

func TestTryTransition_TTL(t *testing.T) {
	kv := newMockKVStore()
	bc := newMockBroadcaster()
	tr := newTestTracker(kv, bc)

	tr.tryTransition(context.Background(), "org1", "doc1", "ver1", StatusProcessing)

	if kv.setTTL != 24*time.Hour {
		t.Errorf("expected TTL 24h, got %v", kv.setTTL)
	}
}

func TestTryTransition_CorruptRecord(t *testing.T) {
	kv := newMockKVStore()
	kv.setStored("status:org1:doc1:ver1", "not-json")
	bc := newMockBroadcaster()
	tr := newTestTracker(kv, bc)

	ok, err := tr.tryTransition(context.Background(), "org1", "doc1", "ver1", StatusProcessing)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected overwrite of corrupt record")
	}
}

// --- broadcaster integration tests ---

func TestBroadcaster_CalledOnTransition(t *testing.T) {
	kv := newMockKVStore()
	bc := newMockBroadcaster()
	tr := newTestTracker(kv, bc)

	err := tr.HandleEvent(context.Background(), consumer.EventDPStatusChanged,
		&consumer.DPStatusChangedEvent{
			OrganizationID: "org1",
			DocumentID:     "doc1",
			VersionID:      "ver1",
			Status:         "IN_PROGRESS",
		})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	evts := bc.broadcastedEvents()
	if len(evts) != 1 {
		t.Fatalf("expected 1 broadcasted event, got %d", len(evts))
	}
	if evts[0].orgID != "org1" {
		t.Errorf("expected orgID org1, got %s", evts[0].orgID)
	}
	if evts[0].event.Status != "PROCESSING" {
		t.Errorf("expected PROCESSING, got %s", evts[0].event.Status)
	}
}

func TestBroadcaster_ErrorIgnored(t *testing.T) {
	kv := newMockKVStore()
	bc := newMockBroadcaster()
	bc.err = errors.New("redis: connection refused")
	tr := newTestTracker(kv, bc)

	// Broadcaster error should not propagate — status is already persisted.
	err := tr.HandleEvent(context.Background(), consumer.EventDPStatusChanged,
		&consumer.DPStatusChangedEvent{
			OrganizationID: "org1",
			DocumentID:     "doc1",
			VersionID:      "ver1",
			Status:         "IN_PROGRESS",
		})
	if err != nil {
		t.Fatalf("expected nil error when broadcaster fails, got %v", err)
	}

	// Status should still be persisted in Redis.
	raw, found := kv.getStored("status:org1:doc1:ver1")
	if !found {
		t.Fatal("expected status to be persisted despite broadcast failure")
	}
	var rec statusRecord
	json.Unmarshal([]byte(raw), &rec)
	if rec.Status != "PROCESSING" {
		t.Errorf("expected PROCESSING, got %s", rec.Status)
	}
}

func TestBroadcaster_EventFields(t *testing.T) {
	kv := newMockKVStore()
	bc := newMockBroadcaster()
	tr := newTestTracker(kv, bc)

	tr.HandleEvent(context.Background(), consumer.EventDPStatusChanged,
		&consumer.DPStatusChangedEvent{
			OrganizationID: "org1",
			DocumentID:     "doc1",
			VersionID:      "ver1",
			JobID:          "job1",
			Status:         "TIMED_OUT",
			Message:        "Превышено время",
		})

	evts := bc.broadcastedEvents()
	if len(evts) == 0 {
		t.Fatal("expected at least one broadcasted event")
	}
	event := evts[0].event
	if event.EventType != "status_update" {
		t.Errorf("expected event_type status_update, got %s", event.EventType)
	}
	if event.ErrorCode != "TIMED_OUT" {
		t.Errorf("expected error_code TIMED_OUT, got %s", event.ErrorCode)
	}
	if !event.IsRetryable {
		t.Error("expected is_retryable true")
	}
}

// --- HandleEvent dispatch tests ---

func TestHandleEvent_UnrecognizedEventType(t *testing.T) {
	kv := newMockKVStore()
	bc := newMockBroadcaster()
	tr := newTestTracker(kv, bc)

	err := tr.HandleEvent(context.Background(), "unknown.event.type", nil)
	if err != nil {
		t.Fatalf("expected nil error for unknown event type, got %v", err)
	}
}

func TestHandleEvent_WrongConcreteType(t *testing.T) {
	kv := newMockKVStore()
	bc := newMockBroadcaster()
	tr := newTestTracker(kv, bc)

	// Pass DPProcessingFailedEvent for DPStatusChanged event type.
	err := tr.HandleEvent(context.Background(), consumer.EventDPStatusChanged,
		&consumer.DPProcessingFailedEvent{})
	if err != nil {
		t.Fatalf("expected nil error for wrong concrete type, got %v", err)
	}
}

// --- DP event handler tests ---

func TestDPStatusChanged_InProgress(t *testing.T) {
	kv := newMockKVStore()
	bc := newMockBroadcaster()
	tr := newTestTracker(kv, bc)

	err := tr.HandleEvent(context.Background(), consumer.EventDPStatusChanged,
		&consumer.DPStatusChangedEvent{
			OrganizationID: "org1",
			DocumentID:     "doc1",
			VersionID:      "ver1",
			JobID:          "job1",
			Status:         "IN_PROGRESS",
		})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	event := parseBroadcastedEvent(t, bc)
	if event.Status != "PROCESSING" {
		t.Errorf("expected PROCESSING, got %s", event.Status)
	}
	if event.EventType != "status_update" {
		t.Errorf("expected status_update, got %s", event.EventType)
	}
}

func TestDPStatusChanged_Rejected(t *testing.T) {
	kv := newMockKVStore()
	bc := newMockBroadcaster()
	tr := newTestTracker(kv, bc)

	err := tr.HandleEvent(context.Background(), consumer.EventDPStatusChanged,
		&consumer.DPStatusChangedEvent{
			OrganizationID: "org1",
			DocumentID:     "doc1",
			VersionID:      "ver1",
			Status:         "REJECTED",
			Message:        "Формат файла не поддерживается",
		})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	event := parseBroadcastedEvent(t, bc)
	if event.Status != "REJECTED" {
		t.Errorf("expected REJECTED, got %s", event.Status)
	}
	if event.ErrorCode != "REJECTED" {
		t.Errorf("expected error_code REJECTED, got %s", event.ErrorCode)
	}
	if event.ErrorMessage != "Формат файла не поддерживается" {
		t.Errorf("unexpected error_message: %s", event.ErrorMessage)
	}
}

func TestDPStatusChanged_TimedOut(t *testing.T) {
	kv := newMockKVStore()
	bc := newMockBroadcaster()
	tr := newTestTracker(kv, bc)

	err := tr.HandleEvent(context.Background(), consumer.EventDPStatusChanged,
		&consumer.DPStatusChangedEvent{
			OrganizationID: "org1",
			DocumentID:     "doc1",
			VersionID:      "ver1",
			Status:         "TIMED_OUT",
		})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	event := parseBroadcastedEvent(t, bc)
	if event.Status != "FAILED" {
		t.Errorf("expected FAILED, got %s", event.Status)
	}
	if event.ErrorCode != "TIMED_OUT" {
		t.Errorf("expected error_code TIMED_OUT, got %s", event.ErrorCode)
	}
	if !event.IsRetryable {
		t.Error("expected is_retryable true for TIMED_OUT")
	}
}

func TestDPStatusChanged_CompletedIgnored(t *testing.T) {
	kv := newMockKVStore()
	bc := newMockBroadcaster()
	tr := newTestTracker(kv, bc)

	err := tr.HandleEvent(context.Background(), consumer.EventDPStatusChanged,
		&consumer.DPStatusChangedEvent{
			OrganizationID: "org1",
			DocumentID:     "doc1",
			VersionID:      "ver1",
			Status:         "COMPLETED",
		})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	msgs := bc.broadcastedEvents()
	if len(msgs) != 0 {
		t.Errorf("expected no broadcast for COMPLETED, got %d", len(msgs))
	}
}

func TestDPStatusChanged_MissingOrgID(t *testing.T) {
	kv := newMockKVStore()
	bc := newMockBroadcaster()
	tr := newTestTracker(kv, bc)

	err := tr.HandleEvent(context.Background(), consumer.EventDPStatusChanged,
		&consumer.DPStatusChangedEvent{
			DocumentID: "doc1",
			VersionID:  "ver1",
			Status:     "IN_PROGRESS",
		})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	msgs := bc.broadcastedEvents()
	if len(msgs) != 0 {
		t.Errorf("expected no broadcast for missing org_id, got %d", len(msgs))
	}
}

func TestDPProcessingCompleted_NoTransition(t *testing.T) {
	kv := newMockKVStore()
	bc := newMockBroadcaster()
	tr := newTestTracker(kv, bc)

	err := tr.HandleEvent(context.Background(), consumer.EventDPProcessingCompleted,
		&consumer.DPProcessingCompletedEvent{
			OrganizationID: "org1",
			DocumentID:     "doc1",
			VersionID:      "ver1",
			JobID:          "job1",
		})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	msgs := bc.broadcastedEvents()
	if len(msgs) != 0 {
		t.Errorf("expected no broadcast for processing-completed, got %d", len(msgs))
	}
}

func TestDPProcessingFailed_TransitionsToFailed(t *testing.T) {
	kv := newMockKVStore()
	bc := newMockBroadcaster()
	tr := newTestTracker(kv, bc)

	err := tr.HandleEvent(context.Background(), consumer.EventDPProcessingFailed,
		&consumer.DPProcessingFailedEvent{
			OrganizationID: "org1",
			DocumentID:     "doc1",
			VersionID:      "ver1",
			JobID:          "job1",
			ErrorCode:      "OCR_FAILED",
			ErrorMessage:   "OCR service unavailable",
			IsRetryable:    true,
		})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	event := parseBroadcastedEvent(t, bc)
	if event.Status != "FAILED" {
		t.Errorf("expected FAILED, got %s", event.Status)
	}
	if event.ErrorCode != "OCR_FAILED" {
		t.Errorf("expected OCR_FAILED, got %s", event.ErrorCode)
	}
	if event.ErrorMessage != "OCR service unavailable" {
		t.Errorf("unexpected error_message: %s", event.ErrorMessage)
	}
	if !event.IsRetryable {
		t.Error("expected is_retryable true")
	}
}

func TestDPProcessingFailed_MissingOrgID(t *testing.T) {
	kv := newMockKVStore()
	bc := newMockBroadcaster()
	tr := newTestTracker(kv, bc)

	err := tr.HandleEvent(context.Background(), consumer.EventDPProcessingFailed,
		&consumer.DPProcessingFailedEvent{
			DocumentID: "doc1",
			VersionID:  "ver1",
			ErrorCode:  "ERR",
		})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	msgs := bc.broadcastedEvents()
	if len(msgs) != 0 {
		t.Errorf("expected no broadcast for missing org_id, got %d", len(msgs))
	}
}

// --- LIC event handler tests ---

func TestLICStatusChanged_InProgress(t *testing.T) {
	kv := newMockKVStore()
	bc := newMockBroadcaster()
	tr := newTestTracker(kv, bc)

	err := tr.HandleEvent(context.Background(), consumer.EventLICStatusChanged,
		&consumer.LICStatusChangedEvent{
			OrganizationID: "org1",
			DocumentID:     "doc1",
			VersionID:      "ver1",
			JobID:          "job1",
			Status:         "IN_PROGRESS",
		})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	event := parseBroadcastedEvent(t, bc)
	if event.Status != "ANALYZING" {
		t.Errorf("expected ANALYZING, got %s", event.Status)
	}
}

func TestLICStatusChanged_CompletedIgnored(t *testing.T) {
	kv := newMockKVStore()
	bc := newMockBroadcaster()
	tr := newTestTracker(kv, bc)

	err := tr.HandleEvent(context.Background(), consumer.EventLICStatusChanged,
		&consumer.LICStatusChangedEvent{
			OrganizationID: "org1",
			DocumentID:     "doc1",
			VersionID:      "ver1",
			Status:         "COMPLETED",
		})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	msgs := bc.broadcastedEvents()
	if len(msgs) != 0 {
		t.Errorf("expected no broadcast for LIC COMPLETED, got %d", len(msgs))
	}
}

func TestLICStatusChanged_Failed(t *testing.T) {
	kv := newMockKVStore()
	bc := newMockBroadcaster()
	tr := newTestTracker(kv, bc)

	err := tr.HandleEvent(context.Background(), consumer.EventLICStatusChanged,
		&consumer.LICStatusChangedEvent{
			OrganizationID: "org1",
			DocumentID:     "doc1",
			VersionID:      "ver1",
			JobID:          "job1",
			Status:         "FAILED",
			ErrorCode:      "MODEL_UNAVAILABLE",
			ErrorMessage:   "LLM service down",
			IsRetryable:    boolPtr(true),
		})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	event := parseBroadcastedEvent(t, bc)
	if event.Status != "ANALYSIS_FAILED" {
		t.Errorf("expected ANALYSIS_FAILED, got %s", event.Status)
	}
	if event.ErrorCode != "MODEL_UNAVAILABLE" {
		t.Errorf("expected MODEL_UNAVAILABLE, got %s", event.ErrorCode)
	}
	if !event.IsRetryable {
		t.Error("expected is_retryable true")
	}
}

func TestLICStatusChanged_FailedIsRetryableNil(t *testing.T) {
	kv := newMockKVStore()
	bc := newMockBroadcaster()
	tr := newTestTracker(kv, bc)

	err := tr.HandleEvent(context.Background(), consumer.EventLICStatusChanged,
		&consumer.LICStatusChangedEvent{
			OrganizationID: "org1",
			DocumentID:     "doc1",
			VersionID:      "ver1",
			Status:         "FAILED",
			IsRetryable:    nil,
		})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	event := parseBroadcastedEvent(t, bc)
	if event.IsRetryable {
		t.Error("expected is_retryable false when nil")
	}
}

func TestLICStatusChanged_UnknownStatus(t *testing.T) {
	kv := newMockKVStore()
	bc := newMockBroadcaster()
	tr := newTestTracker(kv, bc)

	err := tr.HandleEvent(context.Background(), consumer.EventLICStatusChanged,
		&consumer.LICStatusChangedEvent{
			OrganizationID: "org1",
			DocumentID:     "doc1",
			VersionID:      "ver1",
			Status:         "UNKNOWN_STATUS",
		})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	msgs := bc.broadcastedEvents()
	if len(msgs) != 0 {
		t.Errorf("expected no broadcast for unknown LIC status, got %d", len(msgs))
	}
}

// --- RE event handler tests ---

func TestREStatusChanged_InProgress(t *testing.T) {
	kv := newMockKVStore()
	bc := newMockBroadcaster()
	tr := newTestTracker(kv, bc)

	err := tr.HandleEvent(context.Background(), consumer.EventREStatusChanged,
		&consumer.REStatusChangedEvent{
			OrganizationID: "org1",
			DocumentID:     "doc1",
			VersionID:      "ver1",
			JobID:          "job1",
			Status:         "IN_PROGRESS",
		})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	event := parseBroadcastedEvent(t, bc)
	if event.Status != "GENERATING_REPORTS" {
		t.Errorf("expected GENERATING_REPORTS, got %s", event.Status)
	}
}

func TestREStatusChanged_Failed(t *testing.T) {
	kv := newMockKVStore()
	bc := newMockBroadcaster()
	tr := newTestTracker(kv, bc)

	err := tr.HandleEvent(context.Background(), consumer.EventREStatusChanged,
		&consumer.REStatusChangedEvent{
			OrganizationID: "org1",
			DocumentID:     "doc1",
			VersionID:      "ver1",
			JobID:          "job1",
			Status:         "FAILED",
			ErrorCode:      "TEMPLATE_ERROR",
			ErrorMessage:   "Report template missing",
			IsRetryable:    boolPtr(false),
		})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	event := parseBroadcastedEvent(t, bc)
	if event.Status != "REPORTS_FAILED" {
		t.Errorf("expected REPORTS_FAILED, got %s", event.Status)
	}
	if event.ErrorCode != "TEMPLATE_ERROR" {
		t.Errorf("expected TEMPLATE_ERROR, got %s", event.ErrorCode)
	}
	if event.IsRetryable {
		t.Error("expected is_retryable false")
	}
}

func TestREStatusChanged_CompletedIgnored(t *testing.T) {
	kv := newMockKVStore()
	bc := newMockBroadcaster()
	tr := newTestTracker(kv, bc)

	err := tr.HandleEvent(context.Background(), consumer.EventREStatusChanged,
		&consumer.REStatusChangedEvent{
			OrganizationID: "org1",
			DocumentID:     "doc1",
			VersionID:      "ver1",
			Status:         "COMPLETED",
		})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	msgs := bc.broadcastedEvents()
	if len(msgs) != 0 {
		t.Errorf("expected no broadcast for RE COMPLETED, got %d", len(msgs))
	}
}

// --- DM event handler tests ---

func TestDMVersionArtifactsReady(t *testing.T) {
	kv := newMockKVStore()
	bc := newMockBroadcaster()
	tr := newTestTracker(kv, bc)

	err := tr.HandleEvent(context.Background(), consumer.EventDMVersionArtifactsReady,
		&consumer.DMVersionArtifactsReadyEvent{
			OrganizationID: "org1",
			DocumentID:     "doc1",
			VersionID:      "ver1",
			ArtifactTypes:  []string{"OCR_TEXT", "SEMANTIC_TREE"},
		})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	event := parseBroadcastedEvent(t, bc)
	if event.Status != "ANALYZING" {
		t.Errorf("expected ANALYZING, got %s", event.Status)
	}
}

func TestDMVersionAnalysisReady(t *testing.T) {
	kv := newMockKVStore()
	kv.setStored("status:org1:doc1:ver1", mustMarshalRecord("ANALYZING"))
	bc := newMockBroadcaster()
	tr := newTestTracker(kv, bc)

	err := tr.HandleEvent(context.Background(), consumer.EventDMVersionAnalysisReady,
		&consumer.DMVersionAnalysisReadyEvent{
			OrganizationID: "org1",
			DocumentID:     "doc1",
			VersionID:      "ver1",
		})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	event := parseBroadcastedEvent(t, bc)
	if event.Status != "GENERATING_REPORTS" {
		t.Errorf("expected GENERATING_REPORTS, got %s", event.Status)
	}
}

func TestDMVersionReportsReady(t *testing.T) {
	kv := newMockKVStore()
	kv.setStored("status:org1:doc1:ver1", mustMarshalRecord("GENERATING_REPORTS"))
	bc := newMockBroadcaster()
	tr := newTestTracker(kv, bc)

	err := tr.HandleEvent(context.Background(), consumer.EventDMVersionReportsReady,
		&consumer.DMVersionReportsReadyEvent{
			OrganizationID: "org1",
			DocumentID:     "doc1",
			VersionID:      "ver1",
		})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	event := parseBroadcastedEvent(t, bc)
	if event.Status != "READY" {
		t.Errorf("expected READY, got %s", event.Status)
	}
	if event.Message != "Анализ завершён" {
		t.Errorf("expected Russian message 'Анализ завершён', got %s", event.Message)
	}
}

func TestDMVersionPartiallyAvailable(t *testing.T) {
	kv := newMockKVStore()
	bc := newMockBroadcaster()
	tr := newTestTracker(kv, bc)

	err := tr.HandleEvent(context.Background(), consumer.EventDMVersionPartiallyAvail,
		&consumer.DMVersionPartiallyAvailableEvent{
			OrganizationID: "org1",
			DocumentID:     "doc1",
			VersionID:      "ver1",
			FailedStage:    "REPORT_GENERATION",
			ErrorMessage:   "Template rendering failed",
		})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	event := parseBroadcastedEvent(t, bc)
	if event.Status != "PARTIALLY_FAILED" {
		t.Errorf("expected PARTIALLY_FAILED, got %s", event.Status)
	}
	if event.ErrorCode != "REPORT_GENERATION" {
		t.Errorf("expected error_code REPORT_GENERATION, got %s", event.ErrorCode)
	}
	if event.ErrorMessage != "Template rendering failed" {
		t.Errorf("unexpected error_message: %s", event.ErrorMessage)
	}
}

func TestDMVersionCreated_NoStatusTransition(t *testing.T) {
	kv := newMockKVStore()
	bc := newMockBroadcaster()
	tr := newTestTracker(kv, bc)

	err := tr.HandleEvent(context.Background(), consumer.EventDMVersionCreated,
		&consumer.DMVersionCreatedEvent{
			OrganizationID: "org1",
			DocumentID:     "doc1",
			VersionID:      "ver1",
			OriginType:     "UPLOAD",
		})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// No status_update broadcast for UPLOAD origin.
	msgs := bc.broadcastedEvents()
	if len(msgs) != 0 {
		t.Errorf("expected no broadcast for UPLOAD origin, got %d", len(msgs))
	}

	// No status written to Redis.
	_, found := kv.getStored("status:org1:doc1:ver1")
	if found {
		t.Error("expected no status key written for version-created")
	}
}

func TestDMVersionCreated_ReCheck_BroadcastsVersionCreated(t *testing.T) {
	kv := newMockKVStore()
	bc := newMockBroadcaster()
	tr := newTestTracker(kv, bc)

	err := tr.HandleEvent(context.Background(), consumer.EventDMVersionCreated,
		&consumer.DMVersionCreatedEvent{
			OrganizationID: "org1",
			DocumentID:     "doc1",
			VersionID:      "ver2",
			OriginType:     "RE_CHECK",
		})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	event := parseBroadcastedEvent(t, bc)
	if event.EventType != "version_created" {
		t.Errorf("expected version_created event type, got %s", event.EventType)
	}
	if event.Status != "VERSION_CREATED" {
		t.Errorf("expected VERSION_CREATED, got %s", event.Status)
	}
}

// --- Comparison event handler tests ---

func TestDPComparisonCompleted(t *testing.T) {
	kv := newMockKVStore()
	bc := newMockBroadcaster()
	tr := newTestTracker(kv, bc)

	err := tr.HandleEvent(context.Background(), consumer.EventDPComparisonCompleted,
		&consumer.DPComparisonCompletedEvent{
			OrganizationID:  "org1",
			DocumentID:      "doc1",
			JobID:           "job1",
			BaseVersionID:   "ver1",
			TargetVersionID: "ver2",
		})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	event := parseBroadcastedEvent(t, bc)
	if event.EventType != "comparison_update" {
		t.Errorf("expected comparison_update, got %s", event.EventType)
	}
	if event.Status != "COMPARISON_COMPLETED" {
		t.Errorf("expected COMPARISON_COMPLETED, got %s", event.Status)
	}
	if event.BaseVersionID != "ver1" {
		t.Errorf("expected base_version_id ver1, got %s", event.BaseVersionID)
	}
	if event.TargetVersionID != "ver2" {
		t.Errorf("expected target_version_id ver2, got %s", event.TargetVersionID)
	}
	if event.Message != "Сравнение версий завершено" {
		t.Errorf("expected Russian message, got %s", event.Message)
	}
}

func TestDPComparisonFailed(t *testing.T) {
	kv := newMockKVStore()
	bc := newMockBroadcaster()
	tr := newTestTracker(kv, bc)

	err := tr.HandleEvent(context.Background(), consumer.EventDPComparisonFailed,
		&consumer.DPComparisonFailedEvent{
			OrganizationID:  "org1",
			DocumentID:      "doc1",
			JobID:           "job1",
			BaseVersionID:   "ver1",
			TargetVersionID: "ver2",
			ErrorCode:       "TREE_MISSING",
			ErrorMessage:    "Semantic tree not found",
			IsRetryable:     false,
		})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	event := parseBroadcastedEvent(t, bc)
	if event.EventType != "comparison_update" {
		t.Errorf("expected comparison_update, got %s", event.EventType)
	}
	if event.Status != "COMPARISON_FAILED" {
		t.Errorf("expected COMPARISON_FAILED, got %s", event.Status)
	}
	if event.ErrorCode != "TREE_MISSING" {
		t.Errorf("expected TREE_MISSING, got %s", event.ErrorCode)
	}
}

func TestDPComparisonCompleted_MissingOrgID(t *testing.T) {
	kv := newMockKVStore()
	bc := newMockBroadcaster()
	tr := newTestTracker(kv, bc)

	err := tr.HandleEvent(context.Background(), consumer.EventDPComparisonCompleted,
		&consumer.DPComparisonCompletedEvent{
			DocumentID:      "doc1",
			BaseVersionID:   "ver1",
			TargetVersionID: "ver2",
		})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	msgs := bc.broadcastedEvents()
	if len(msgs) != 0 {
		t.Errorf("expected no broadcast for missing org_id, got %d", len(msgs))
	}
}

func TestDPComparisonFailed_MissingOrgID(t *testing.T) {
	kv := newMockKVStore()
	bc := newMockBroadcaster()
	tr := newTestTracker(kv, bc)

	err := tr.HandleEvent(context.Background(), consumer.EventDPComparisonFailed,
		&consumer.DPComparisonFailedEvent{
			DocumentID: "doc1",
		})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	msgs := bc.broadcastedEvents()
	if len(msgs) != 0 {
		t.Errorf("expected no broadcast for missing org_id, got %d", len(msgs))
	}
}

// --- Missing OrganizationID tests for LIC/RE/DM ---

func TestLICStatusChanged_MissingOrgID(t *testing.T) {
	kv := newMockKVStore()
	bc := newMockBroadcaster()
	tr := newTestTracker(kv, bc)

	err := tr.HandleEvent(context.Background(), consumer.EventLICStatusChanged,
		&consumer.LICStatusChangedEvent{
			DocumentID: "doc1",
			VersionID:  "ver1",
			Status:     "FAILED",
		})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	msgs := bc.broadcastedEvents()
	if len(msgs) != 0 {
		t.Errorf("expected no broadcast for missing org_id, got %d", len(msgs))
	}
}

func TestREStatusChanged_MissingOrgID(t *testing.T) {
	kv := newMockKVStore()
	bc := newMockBroadcaster()
	tr := newTestTracker(kv, bc)

	err := tr.HandleEvent(context.Background(), consumer.EventREStatusChanged,
		&consumer.REStatusChangedEvent{
			DocumentID: "doc1",
			VersionID:  "ver1",
			Status:     "FAILED",
		})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	msgs := bc.broadcastedEvents()
	if len(msgs) != 0 {
		t.Errorf("expected no broadcast for missing org_id, got %d", len(msgs))
	}
}

func TestDMVersionArtifactsReady_MissingOrgID(t *testing.T) {
	kv := newMockKVStore()
	bc := newMockBroadcaster()
	tr := newTestTracker(kv, bc)

	err := tr.HandleEvent(context.Background(), consumer.EventDMVersionArtifactsReady,
		&consumer.DMVersionArtifactsReadyEvent{
			DocumentID: "doc1",
			VersionID:  "ver1",
		})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	msgs := bc.broadcastedEvents()
	if len(msgs) != 0 {
		t.Errorf("expected no broadcast for missing org_id, got %d", len(msgs))
	}
}

func TestDMVersionPartiallyAvailable_MissingOrgID(t *testing.T) {
	kv := newMockKVStore()
	bc := newMockBroadcaster()
	tr := newTestTracker(kv, bc)

	err := tr.HandleEvent(context.Background(), consumer.EventDMVersionPartiallyAvail,
		&consumer.DMVersionPartiallyAvailableEvent{
			DocumentID: "doc1",
			VersionID:  "ver1",
		})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	msgs := bc.broadcastedEvents()
	if len(msgs) != 0 {
		t.Errorf("expected no broadcast for missing org_id, got %d", len(msgs))
	}
}

func TestDMVersionCreated_MissingOrgID(t *testing.T) {
	kv := newMockKVStore()
	bc := newMockBroadcaster()
	tr := newTestTracker(kv, bc)

	err := tr.HandleEvent(context.Background(), consumer.EventDMVersionCreated,
		&consumer.DMVersionCreatedEvent{
			DocumentID: "doc1",
			VersionID:  "ver1",
			OriginType: "RE_CHECK",
		})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	msgs := bc.broadcastedEvents()
	if len(msgs) != 0 {
		t.Errorf("expected no broadcast for missing org_id with RE_CHECK, got %d", len(msgs))
	}
}

// --- Redis error causes NACK (end-to-end) ---

func TestHandleEvent_RedisError_CausesNACK(t *testing.T) {
	kv := newMockKVStore()
	kv.getErr = errors.New("redis: connection refused")
	bc := newMockBroadcaster()
	tr := newTestTracker(kv, bc)

	err := tr.HandleEvent(context.Background(), consumer.EventDPStatusChanged,
		&consumer.DPStatusChangedEvent{
			OrganizationID: "org1",
			DocumentID:     "doc1",
			VersionID:      "ver1",
			Status:         "IN_PROGRESS",
		})
	if err == nil {
		t.Fatal("expected error to trigger consumer NACK")
	}
}

// --- DPProcessingCompleted with warnings ---

func TestDPProcessingCompleted_WithWarnings(t *testing.T) {
	kv := newMockKVStore()
	bc := newMockBroadcaster()
	tr := newTestTracker(kv, bc)

	err := tr.HandleEvent(context.Background(), consumer.EventDPProcessingCompleted,
		&consumer.DPProcessingCompletedEvent{
			OrganizationID: "org1",
			DocumentID:     "doc1",
			VersionID:      "ver1",
			JobID:          "job1",
			Warnings: []consumer.Warning{
				{Code: "LOW_QUALITY", Message: "Low scan quality", Severity: "warning"},
			},
		})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Still no broadcast — informational only.
	msgs := bc.broadcastedEvents()
	if len(msgs) != 0 {
		t.Errorf("expected no broadcast for processing-completed, got %d", len(msgs))
	}
}

// --- End-to-end monotonic ordering scenario tests ---

func TestFullHappyPath(t *testing.T) {
	kv := newMockKVStore()
	bc := newMockBroadcaster()
	tr := newTestTracker(kv, bc)
	ctx := context.Background()

	events := []struct {
		eventType consumer.EventType
		event     any
		expected  string // expected status, empty = no broadcast
	}{
		{consumer.EventDPStatusChanged, &consumer.DPStatusChangedEvent{
			OrganizationID: "org1", DocumentID: "doc1", VersionID: "ver1", Status: "IN_PROGRESS",
		}, "PROCESSING"},
		{consumer.EventDMVersionArtifactsReady, &consumer.DMVersionArtifactsReadyEvent{
			OrganizationID: "org1", DocumentID: "doc1", VersionID: "ver1",
		}, "ANALYZING"},
		{consumer.EventLICStatusChanged, &consumer.LICStatusChangedEvent{
			OrganizationID: "org1", DocumentID: "doc1", VersionID: "ver1", Status: "IN_PROGRESS",
		}, ""}, // Already ANALYZING, skip (same status).
		{consumer.EventDMVersionAnalysisReady, &consumer.DMVersionAnalysisReadyEvent{
			OrganizationID: "org1", DocumentID: "doc1", VersionID: "ver1",
		}, "GENERATING_REPORTS"},
		{consumer.EventREStatusChanged, &consumer.REStatusChangedEvent{
			OrganizationID: "org1", DocumentID: "doc1", VersionID: "ver1", Status: "IN_PROGRESS",
		}, ""}, // Already GENERATING_REPORTS, skip.
		{consumer.EventDMVersionReportsReady, &consumer.DMVersionReportsReadyEvent{
			OrganizationID: "org1", DocumentID: "doc1", VersionID: "ver1",
		}, "READY"},
	}

	broadcastIdx := 0
	for i, tc := range events {
		err := tr.HandleEvent(ctx, tc.eventType, tc.event)
		if err != nil {
			t.Fatalf("event %d: unexpected error: %v", i, err)
		}
		if tc.expected != "" {
			evts := bc.broadcastedEvents()
			if broadcastIdx >= len(evts) {
				t.Fatalf("event %d: expected broadcast for %s, but no new event", i, tc.expected)
			}
			if evts[broadcastIdx].event.Status != tc.expected {
				t.Errorf("event %d: expected %s, got %s", i, tc.expected, evts[broadcastIdx].event.Status)
			}
			broadcastIdx++
		}
	}

	// Total broadcasts: 4 (PROCESSING, ANALYZING, GENERATING_REPORTS, READY).
	evts := bc.broadcastedEvents()
	if len(evts) != 4 {
		t.Errorf("expected 4 total broadcasts, got %d", len(evts))
	}
}

func TestOutOfOrderEvents(t *testing.T) {
	kv := newMockKVStore()
	bc := newMockBroadcaster()
	tr := newTestTracker(kv, bc)
	ctx := context.Background()

	// DM artifacts-ready arrives BEFORE DP IN_PROGRESS.
	err := tr.HandleEvent(ctx, consumer.EventDMVersionArtifactsReady,
		&consumer.DMVersionArtifactsReadyEvent{
			OrganizationID: "org1", DocumentID: "doc1", VersionID: "ver1",
		})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// DP IN_PROGRESS arrives late → should be skipped (ANALYZING > PROCESSING).
	err = tr.HandleEvent(ctx, consumer.EventDPStatusChanged,
		&consumer.DPStatusChangedEvent{
			OrganizationID: "org1", DocumentID: "doc1", VersionID: "ver1",
			Status: "IN_PROGRESS",
		})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Only 1 broadcast (ANALYZING), not 2.
	evts := bc.broadcastedEvents()
	if len(evts) != 1 {
		t.Errorf("expected 1 broadcast, got %d", len(evts))
	}

	if evts[0].event.Status != "ANALYZING" {
		t.Errorf("expected ANALYZING, got %s", evts[0].event.Status)
	}
}

func TestFailureMidPipeline(t *testing.T) {
	kv := newMockKVStore()
	bc := newMockBroadcaster()
	tr := newTestTracker(kv, bc)
	ctx := context.Background()

	// PROCESSING state.
	tr.HandleEvent(ctx, consumer.EventDPStatusChanged,
		&consumer.DPStatusChangedEvent{
			OrganizationID: "org1", DocumentID: "doc1", VersionID: "ver1",
			Status: "IN_PROGRESS",
		})

	// LIC FAILED → ANALYSIS_FAILED (terminal).
	tr.HandleEvent(ctx, consumer.EventLICStatusChanged,
		&consumer.LICStatusChangedEvent{
			OrganizationID: "org1", DocumentID: "doc1", VersionID: "ver1",
			Status: "FAILED",
		})

	// Subsequent DM events should be skipped (terminal).
	tr.HandleEvent(ctx, consumer.EventDMVersionAnalysisReady,
		&consumer.DMVersionAnalysisReadyEvent{
			OrganizationID: "org1", DocumentID: "doc1", VersionID: "ver1",
		})
	tr.HandleEvent(ctx, consumer.EventDMVersionReportsReady,
		&consumer.DMVersionReportsReadyEvent{
			OrganizationID: "org1", DocumentID: "doc1", VersionID: "ver1",
		})

	// Only 2 broadcasts: PROCESSING and ANALYSIS_FAILED.
	evts := bc.broadcastedEvents()
	if len(evts) != 2 {
		t.Errorf("expected 2 broadcasts, got %d", len(evts))
	}

	if evts[1].event.Status != "ANALYSIS_FAILED" {
		t.Errorf("expected ANALYSIS_FAILED, got %s", evts[1].event.Status)
	}

	// Verify Redis has terminal status.
	raw, _ := kv.getStored("status:org1:doc1:ver1")
	var rec statusRecord
	json.Unmarshal([]byte(raw), &rec)
	if rec.Status != "ANALYSIS_FAILED" {
		t.Errorf("expected stored ANALYSIS_FAILED, got %s", rec.Status)
	}
}

func TestDuplicateEvent(t *testing.T) {
	kv := newMockKVStore()
	bc := newMockBroadcaster()
	tr := newTestTracker(kv, bc)
	ctx := context.Background()

	e := &consumer.DPStatusChangedEvent{
		OrganizationID: "org1", DocumentID: "doc1", VersionID: "ver1",
		Status: "IN_PROGRESS",
	}

	tr.HandleEvent(ctx, consumer.EventDPStatusChanged, e)
	tr.HandleEvent(ctx, consumer.EventDPStatusChanged, e) // duplicate

	// Only 1 broadcast.
	evts := bc.broadcastedEvents()
	if len(evts) != 1 {
		t.Errorf("expected 1 broadcast (duplicate skipped), got %d", len(evts))
	}
}

// --- Timestamp determinism ---

func TestTimestampFromNowFunc(t *testing.T) {
	kv := newMockKVStore()
	bc := newMockBroadcaster()
	tr := newTestTracker(kv, bc)

	tr.HandleEvent(context.Background(), consumer.EventDPStatusChanged,
		&consumer.DPStatusChangedEvent{
			OrganizationID: "org1", DocumentID: "doc1", VersionID: "ver1",
			Status: "IN_PROGRESS",
		})

	// Check status record timestamp.
	raw, _ := kv.getStored("status:org1:doc1:ver1")
	var rec statusRecord
	json.Unmarshal([]byte(raw), &rec)
	expected := fixedTime.Format(time.RFC3339)
	if rec.UpdatedAt != expected {
		t.Errorf("expected updated_at %s, got %s", expected, rec.UpdatedAt)
	}

	// Check SSE event timestamp.
	event := parseBroadcastedEvent(t, bc)
	if event.Timestamp != expected {
		t.Errorf("expected SSE timestamp %s, got %s", expected, event.Timestamp)
	}
}

// --- Status message tests ---

func TestStatusMessages_AllStatusesHaveMessages(t *testing.T) {
	allStatuses := []UserStatus{
		StatusUploaded, StatusQueued, StatusProcessing, StatusAnalyzing,
		StatusGeneratingReports, StatusReady, StatusFailed, StatusAnalysisFailed,
		StatusReportsFailed, StatusPartiallyFailed, StatusRejected,
	}

	for _, s := range allStatuses {
		msg, ok := statusMessages[s]
		if !ok {
			t.Errorf("missing message for status %s", s)
		}
		if msg == "" {
			t.Errorf("empty message for status %s", s)
		}
	}
}

// --- Interface compliance ---

func TestTrackerImplementsEventHandler(t *testing.T) {
	var _ consumer.EventHandler = (*Tracker)(nil)
}

// --- derefBool ---

func TestDerefBool(t *testing.T) {
	if derefBool(nil) != false {
		t.Error("nil should be false")
	}
	if derefBool(boolPtr(true)) != true {
		t.Error("true ptr should be true")
	}
	if derefBool(boolPtr(false)) != false {
		t.Error("false ptr should be false")
	}
}

// --- SSE channel and status key ---

func TestStatusKey(t *testing.T) {
	key := statusKey("org-1", "doc-2", "ver-3")
	if key != "status:org-1:doc-2:ver-3" {
		t.Errorf("unexpected key: %s", key)
	}
}

func TestSSEBroadcastChannel(t *testing.T) {
	ch := ssebroadcast.Channel("org-1")
	if ch != "sse:broadcast:org-1" {
		t.Errorf("unexpected channel: %s", ch)
	}
}
