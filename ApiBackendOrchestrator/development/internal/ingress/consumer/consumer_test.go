package consumer

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"contractpro/api-orchestrator/internal/config"
	"contractpro/api-orchestrator/internal/infra/observability/logger"
)

// ---------------------------------------------------------------------------
// Mock broker
// ---------------------------------------------------------------------------

// mockBroker captures Subscribe calls and allows tests to invoke handlers
// directly by topic name.
type mockBroker struct {
	mu       sync.Mutex
	handlers map[string]func(ctx context.Context, body []byte) error
	subErr   error // If non-nil, Subscribe returns this error.
}

func newMockBroker() *mockBroker {
	return &mockBroker{handlers: make(map[string]func(ctx context.Context, body []byte) error)}
}

func (m *mockBroker) Subscribe(topic string, handler func(ctx context.Context, body []byte) error) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.subErr != nil {
		return m.subErr
	}
	m.handlers[topic] = handler
	return nil
}

// deliver simulates a message delivery on the given topic.
func (m *mockBroker) deliver(ctx context.Context, topic string, body []byte) error {
	m.mu.Lock()
	h, ok := m.handlers[topic]
	m.mu.Unlock()
	if !ok {
		return errors.New("no handler for topic: " + topic)
	}
	return h(ctx, body)
}

// ---------------------------------------------------------------------------
// Mock event handler
// ---------------------------------------------------------------------------

// mockEventHandler records HandleEvent calls for assertion.
type mockEventHandler struct {
	mu        sync.Mutex
	calls     []handleEventCall
	returnErr error // If non-nil, HandleEvent returns this error.
}

type handleEventCall struct {
	eventType EventType
	event     any
}

func (h *mockEventHandler) HandleEvent(_ context.Context, eventType EventType, event any) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.calls = append(h.calls, handleEventCall{eventType: eventType, event: event})
	return h.returnErr
}

func (h *mockEventHandler) callCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.calls)
}

func (h *mockEventHandler) lastCall() handleEventCall {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.calls) == 0 {
		return handleEventCall{}
	}
	return h.calls[len(h.calls)-1]
}

// ---------------------------------------------------------------------------
// Mock retry tracker
// ---------------------------------------------------------------------------

type mockRetryTracker struct {
	mu       sync.Mutex
	counters map[string]int
	removed  []string
}

func newMockRetryTracker() *mockRetryTracker {
	return &mockRetryTracker{counters: make(map[string]int)}
}

func (t *mockRetryTracker) Increment(key string) int {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.counters[key]++
	return t.counters[key]
}

func (t *mockRetryTracker) Remove(key string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.removed = append(t.removed, key)
	delete(t.counters, key)
}

func (t *mockRetryTracker) count(key string) int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.counters[key]
}

func (t *mockRetryTracker) wasRemoved(key string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, k := range t.removed {
		if k == key {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func testLogger() *logger.Logger {
	return logger.NewLogger("debug")
}

func testBrokerConfig() config.BrokerConfig {
	return config.BrokerConfig{
		TopicDPStatusChanged:        "dp.events.status-changed",
		TopicDPProcessingCompleted:  "dp.events.processing-completed",
		TopicDPProcessingFailed:     "dp.events.processing-failed",
		TopicDPComparisonCompleted:  "dp.events.comparison-completed",
		TopicDPComparisonFailed:     "dp.events.comparison-failed",
		TopicLICStatusChanged:       "lic.events.status-changed",
		TopicREStatusChanged:        "re.events.status-changed",
		TopicDMVersionArtifactsReady: "dm.events.version-artifacts-ready",
		TopicDMVersionAnalysisReady:  "dm.events.version-analysis-ready",
		TopicDMVersionReportsReady:   "dm.events.version-reports-ready",
		TopicDMVersionPartiallyAvail: "dm.events.version-partially-available",
		TopicDMVersionCreated:        "dm.events.version-created",
	}
}

func mustMarshal(t *testing.T, v any) []byte {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("mustMarshal: %v", err)
	}
	return data
}

// ---------------------------------------------------------------------------
// Start tests
// ---------------------------------------------------------------------------

func TestStart_SubscribesTo12Topics(t *testing.T) {
	mb := newMockBroker()
	handler := &mockEventHandler{}
	cfg := testBrokerConfig()

	c := NewConsumer(mb, handler, testLogger(), cfg)
	if err := c.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	expectedTopics := []string{
		"dp.events.status-changed",
		"dp.events.processing-completed",
		"dp.events.processing-failed",
		"dp.events.comparison-completed",
		"dp.events.comparison-failed",
		"lic.events.status-changed",
		"re.events.status-changed",
		"dm.events.version-artifacts-ready",
		"dm.events.version-analysis-ready",
		"dm.events.version-reports-ready",
		"dm.events.version-partially-available",
		"dm.events.version-created",
	}

	mb.mu.Lock()
	count := len(mb.handlers)
	mb.mu.Unlock()

	if count != 12 {
		t.Errorf("subscribed to %d topics, want 12", count)
	}

	for _, topic := range expectedTopics {
		mb.mu.Lock()
		_, ok := mb.handlers[topic]
		mb.mu.Unlock()
		if !ok {
			t.Errorf("not subscribed to %q", topic)
		}
	}
}

func TestStart_Idempotent(t *testing.T) {
	mb := newMockBroker()
	handler := &mockEventHandler{}
	cfg := testBrokerConfig()

	c := NewConsumer(mb, handler, testLogger(), cfg)

	if err := c.Start(); err != nil {
		t.Fatalf("Start 1: %v", err)
	}
	if err := c.Start(); err != nil {
		t.Fatalf("Start 2: %v", err)
	}

	mb.mu.Lock()
	count := len(mb.handlers)
	mb.mu.Unlock()

	// Should still be exactly 12, not 24.
	if count != 12 {
		t.Errorf("subscribed to %d topics after 2x Start, want 12", count)
	}
}

func TestStart_BrokerSubscribeError(t *testing.T) {
	mb := newMockBroker()
	mb.subErr = errors.New("connection refused")
	handler := &mockEventHandler{}
	cfg := testBrokerConfig()

	c := NewConsumer(mb, handler, testLogger(), cfg)
	err := c.Start()

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, mb.subErr) {
		t.Errorf("err = %v, want errors.Is(connection refused)", err)
	}
}

func TestStart_EmptyTopicError(t *testing.T) {
	mb := newMockBroker()
	handler := &mockEventHandler{}
	cfg := testBrokerConfig()
	cfg.TopicDPStatusChanged = "" // Blank out the first topic.

	c := NewConsumer(mb, handler, testLogger(), cfg)
	err := c.Start()

	if err == nil {
		t.Fatal("expected error for empty topic, got nil")
	}
}

// ---------------------------------------------------------------------------
// Deserialization + routing tests for all 12 event types
// ---------------------------------------------------------------------------

func TestDPStatusChangedEvent_DeserializeAndRoute(t *testing.T) {
	mb := newMockBroker()
	handler := &mockEventHandler{}
	cfg := testBrokerConfig()
	c := NewConsumer(mb, handler, testLogger(), cfg)
	if err := c.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	event := DPStatusChangedEvent{
		CorrelationID: "corr-1",
		Timestamp:     "2026-04-09T10:00:00Z",
		JobID:         "job-1",
		DocumentID:    "doc-1",
		VersionID:     "ver-1",
		Status:        "IN_PROGRESS",
		Stage:         "OCR",
		Message:       "Processing OCR",
	}

	err := mb.deliver(context.Background(), "dp.events.status-changed", mustMarshal(t, event))
	if err != nil {
		t.Fatalf("deliver: %v", err)
	}

	if handler.callCount() != 1 {
		t.Fatalf("handler called %d times, want 1", handler.callCount())
	}

	call := handler.lastCall()
	if call.eventType != EventDPStatusChanged {
		t.Errorf("eventType = %s, want %s", call.eventType, EventDPStatusChanged)
	}

	got, ok := call.event.(*DPStatusChangedEvent)
	if !ok {
		t.Fatalf("event type = %T, want *DPStatusChangedEvent", call.event)
	}
	if got.JobID != "job-1" {
		t.Errorf("JobID = %q, want %q", got.JobID, "job-1")
	}
	if got.Status != "IN_PROGRESS" {
		t.Errorf("Status = %q, want %q", got.Status, "IN_PROGRESS")
	}
	if got.Stage != "OCR" {
		t.Errorf("Stage = %q, want %q", got.Stage, "OCR")
	}
}

func TestDPProcessingCompletedEvent_DeserializeAndRoute(t *testing.T) {
	mb := newMockBroker()
	handler := &mockEventHandler{}
	cfg := testBrokerConfig()
	c := NewConsumer(mb, handler, testLogger(), cfg)
	if err := c.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	event := DPProcessingCompletedEvent{
		CorrelationID: "corr-2",
		Timestamp:     "2026-04-09T10:01:00Z",
		JobID:         "job-2",
		DocumentID:    "doc-2",
		VersionID:     "ver-2",
		Warnings: []Warning{
			{Code: "LOW_OCR_CONFIDENCE", Message: "Low confidence", Severity: "medium"},
		},
	}

	err := mb.deliver(context.Background(), "dp.events.processing-completed", mustMarshal(t, event))
	if err != nil {
		t.Fatalf("deliver: %v", err)
	}

	call := handler.lastCall()
	if call.eventType != EventDPProcessingCompleted {
		t.Errorf("eventType = %s, want %s", call.eventType, EventDPProcessingCompleted)
	}

	got := call.event.(*DPProcessingCompletedEvent)
	if len(got.Warnings) != 1 {
		t.Fatalf("Warnings count = %d, want 1", len(got.Warnings))
	}
	if got.Warnings[0].Code != "LOW_OCR_CONFIDENCE" {
		t.Errorf("warning code = %q, want %q", got.Warnings[0].Code, "LOW_OCR_CONFIDENCE")
	}
}

func TestDPProcessingFailedEvent_DeserializeAndRoute(t *testing.T) {
	mb := newMockBroker()
	handler := &mockEventHandler{}
	cfg := testBrokerConfig()
	c := NewConsumer(mb, handler, testLogger(), cfg)
	if err := c.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	event := DPProcessingFailedEvent{
		CorrelationID: "corr-3",
		Timestamp:     "2026-04-09T10:02:00Z",
		JobID:         "job-3",
		DocumentID:    "doc-3",
		VersionID:     "ver-3",
		ErrorCode:     "INVALID_PDF",
		ErrorMessage:  "Not a valid PDF",
		FailedAtStage: "VALIDATE",
		IsRetryable:   false,
	}

	err := mb.deliver(context.Background(), "dp.events.processing-failed", mustMarshal(t, event))
	if err != nil {
		t.Fatalf("deliver: %v", err)
	}

	call := handler.lastCall()
	if call.eventType != EventDPProcessingFailed {
		t.Errorf("eventType = %s, want %s", call.eventType, EventDPProcessingFailed)
	}

	got := call.event.(*DPProcessingFailedEvent)
	if got.ErrorCode != "INVALID_PDF" {
		t.Errorf("ErrorCode = %q, want %q", got.ErrorCode, "INVALID_PDF")
	}
	if got.IsRetryable != false {
		t.Error("IsRetryable = true, want false")
	}
}

func TestDPComparisonCompletedEvent_DeserializeAndRoute(t *testing.T) {
	mb := newMockBroker()
	handler := &mockEventHandler{}
	cfg := testBrokerConfig()
	c := NewConsumer(mb, handler, testLogger(), cfg)
	if err := c.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	event := DPComparisonCompletedEvent{
		CorrelationID:   "corr-4",
		Timestamp:       "2026-04-09T10:03:00Z",
		JobID:           "job-4",
		DocumentID:      "doc-4",
		BaseVersionID:   "ver-base",
		TargetVersionID: "ver-target",
	}

	err := mb.deliver(context.Background(), "dp.events.comparison-completed", mustMarshal(t, event))
	if err != nil {
		t.Fatalf("deliver: %v", err)
	}

	call := handler.lastCall()
	if call.eventType != EventDPComparisonCompleted {
		t.Errorf("eventType = %s, want %s", call.eventType, EventDPComparisonCompleted)
	}

	got := call.event.(*DPComparisonCompletedEvent)
	if got.BaseVersionID != "ver-base" {
		t.Errorf("BaseVersionID = %q, want %q", got.BaseVersionID, "ver-base")
	}
}

func TestDPComparisonFailedEvent_DeserializeAndRoute(t *testing.T) {
	mb := newMockBroker()
	handler := &mockEventHandler{}
	cfg := testBrokerConfig()
	c := NewConsumer(mb, handler, testLogger(), cfg)
	if err := c.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	event := DPComparisonFailedEvent{
		CorrelationID:   "corr-5",
		Timestamp:       "2026-04-09T10:04:00Z",
		JobID:           "job-5",
		DocumentID:      "doc-5",
		BaseVersionID:   "ver-base",
		TargetVersionID: "ver-target",
		ErrorCode:       "TREE_NOT_FOUND",
		ErrorMessage:    "Base tree missing",
		IsRetryable:     true,
	}

	err := mb.deliver(context.Background(), "dp.events.comparison-failed", mustMarshal(t, event))
	if err != nil {
		t.Fatalf("deliver: %v", err)
	}

	call := handler.lastCall()
	if call.eventType != EventDPComparisonFailed {
		t.Errorf("eventType = %s, want %s", call.eventType, EventDPComparisonFailed)
	}

	got := call.event.(*DPComparisonFailedEvent)
	if got.IsRetryable != true {
		t.Error("IsRetryable = false, want true")
	}
}

func TestLICStatusChangedEvent_DeserializeAndRoute(t *testing.T) {
	mb := newMockBroker()
	handler := &mockEventHandler{}
	cfg := testBrokerConfig()
	c := NewConsumer(mb, handler, testLogger(), cfg)
	if err := c.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	retryable := true
	event := LICStatusChangedEvent{
		CorrelationID:  "corr-6",
		Timestamp:      "2026-04-09T10:05:00Z",
		JobID:          "job-6",
		DocumentID:     "doc-6",
		VersionID:      "ver-6",
		OrganizationID: "org-6",
		Status:         "FAILED",
		ErrorCode:      "ANALYSIS_TIMEOUT",
		ErrorMessage:   "Analysis timed out",
		IsRetryable:    &retryable,
	}

	err := mb.deliver(context.Background(), "lic.events.status-changed", mustMarshal(t, event))
	if err != nil {
		t.Fatalf("deliver: %v", err)
	}

	call := handler.lastCall()
	if call.eventType != EventLICStatusChanged {
		t.Errorf("eventType = %s, want %s", call.eventType, EventLICStatusChanged)
	}

	got := call.event.(*LICStatusChangedEvent)
	if got.Status != "FAILED" {
		t.Errorf("Status = %q, want %q", got.Status, "FAILED")
	}
	if got.IsRetryable == nil || *got.IsRetryable != true {
		t.Error("IsRetryable = nil or false, want true")
	}
}

func TestREStatusChangedEvent_DeserializeAndRoute(t *testing.T) {
	mb := newMockBroker()
	handler := &mockEventHandler{}
	cfg := testBrokerConfig()
	c := NewConsumer(mb, handler, testLogger(), cfg)
	if err := c.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	event := REStatusChangedEvent{
		CorrelationID:  "corr-7",
		Timestamp:      "2026-04-09T10:06:00Z",
		JobID:          "job-7",
		DocumentID:     "doc-7",
		VersionID:      "ver-7",
		OrganizationID: "org-7",
		Status:         "COMPLETED",
	}

	err := mb.deliver(context.Background(), "re.events.status-changed", mustMarshal(t, event))
	if err != nil {
		t.Fatalf("deliver: %v", err)
	}

	call := handler.lastCall()
	if call.eventType != EventREStatusChanged {
		t.Errorf("eventType = %s, want %s", call.eventType, EventREStatusChanged)
	}

	got := call.event.(*REStatusChangedEvent)
	if got.Status != "COMPLETED" {
		t.Errorf("Status = %q, want %q", got.Status, "COMPLETED")
	}
}

func TestDMVersionArtifactsReadyEvent_DeserializeAndRoute(t *testing.T) {
	mb := newMockBroker()
	handler := &mockEventHandler{}
	cfg := testBrokerConfig()
	c := NewConsumer(mb, handler, testLogger(), cfg)
	if err := c.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	event := DMVersionArtifactsReadyEvent{
		CorrelationID:  "corr-8",
		Timestamp:      "2026-04-09T10:07:00Z",
		DocumentID:     "doc-8",
		VersionID:      "ver-8",
		OrganizationID: "org-8",
		ArtifactTypes:  []string{"OCR_RAW", "EXTRACTED_TEXT", "SEMANTIC_TREE"},
	}

	err := mb.deliver(context.Background(), "dm.events.version-artifacts-ready", mustMarshal(t, event))
	if err != nil {
		t.Fatalf("deliver: %v", err)
	}

	call := handler.lastCall()
	if call.eventType != EventDMVersionArtifactsReady {
		t.Errorf("eventType = %s, want %s", call.eventType, EventDMVersionArtifactsReady)
	}

	got := call.event.(*DMVersionArtifactsReadyEvent)
	if len(got.ArtifactTypes) != 3 {
		t.Errorf("ArtifactTypes count = %d, want 3", len(got.ArtifactTypes))
	}
}

func TestDMVersionAnalysisReadyEvent_DeserializeAndRoute(t *testing.T) {
	mb := newMockBroker()
	handler := &mockEventHandler{}
	cfg := testBrokerConfig()
	c := NewConsumer(mb, handler, testLogger(), cfg)
	if err := c.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	event := DMVersionAnalysisReadyEvent{
		CorrelationID:  "corr-9",
		Timestamp:      "2026-04-09T10:08:00Z",
		DocumentID:     "doc-9",
		VersionID:      "ver-9",
		OrganizationID: "org-9",
		ArtifactTypes:  []string{"RISK_ANALYSIS"},
	}

	err := mb.deliver(context.Background(), "dm.events.version-analysis-ready", mustMarshal(t, event))
	if err != nil {
		t.Fatalf("deliver: %v", err)
	}

	call := handler.lastCall()
	if call.eventType != EventDMVersionAnalysisReady {
		t.Errorf("eventType = %s, want %s", call.eventType, EventDMVersionAnalysisReady)
	}
}

func TestDMVersionReportsReadyEvent_DeserializeAndRoute(t *testing.T) {
	mb := newMockBroker()
	handler := &mockEventHandler{}
	cfg := testBrokerConfig()
	c := NewConsumer(mb, handler, testLogger(), cfg)
	if err := c.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	event := DMVersionReportsReadyEvent{
		CorrelationID:  "corr-10",
		Timestamp:      "2026-04-09T10:09:00Z",
		DocumentID:     "doc-10",
		VersionID:      "ver-10",
		OrganizationID: "org-10",
		ArtifactTypes:  []string{"EXPORT_PDF", "EXPORT_DOCX"},
	}

	err := mb.deliver(context.Background(), "dm.events.version-reports-ready", mustMarshal(t, event))
	if err != nil {
		t.Fatalf("deliver: %v", err)
	}

	call := handler.lastCall()
	if call.eventType != EventDMVersionReportsReady {
		t.Errorf("eventType = %s, want %s", call.eventType, EventDMVersionReportsReady)
	}
}

func TestDMVersionPartiallyAvailableEvent_DeserializeAndRoute(t *testing.T) {
	mb := newMockBroker()
	handler := &mockEventHandler{}
	cfg := testBrokerConfig()
	c := NewConsumer(mb, handler, testLogger(), cfg)
	if err := c.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	event := DMVersionPartiallyAvailableEvent{
		CorrelationID:  "corr-11",
		Timestamp:      "2026-04-09T10:10:00Z",
		DocumentID:     "doc-11",
		VersionID:      "ver-11",
		OrganizationID: "org-11",
		FailedStage:    "RE_REPORT_GENERATION",
		ErrorMessage:   "Report generation failed",
	}

	err := mb.deliver(context.Background(), "dm.events.version-partially-available", mustMarshal(t, event))
	if err != nil {
		t.Fatalf("deliver: %v", err)
	}

	call := handler.lastCall()
	if call.eventType != EventDMVersionPartiallyAvail {
		t.Errorf("eventType = %s, want %s", call.eventType, EventDMVersionPartiallyAvail)
	}

	got := call.event.(*DMVersionPartiallyAvailableEvent)
	if got.FailedStage != "RE_REPORT_GENERATION" {
		t.Errorf("FailedStage = %q, want %q", got.FailedStage, "RE_REPORT_GENERATION")
	}
}

func TestDMVersionCreatedEvent_DeserializeAndRoute(t *testing.T) {
	mb := newMockBroker()
	handler := &mockEventHandler{}
	cfg := testBrokerConfig()
	c := NewConsumer(mb, handler, testLogger(), cfg)
	if err := c.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	event := DMVersionCreatedEvent{
		CorrelationID:   "corr-12",
		Timestamp:       "2026-04-09T10:11:00Z",
		DocumentID:      "doc-12",
		VersionID:       "ver-12",
		VersionNumber:   3,
		OrganizationID:  "org-12",
		OriginType:      "UPLOAD",
		ParentVersionID: "ver-11",
		CreatedByUserID: "user-12",
	}

	err := mb.deliver(context.Background(), "dm.events.version-created", mustMarshal(t, event))
	if err != nil {
		t.Fatalf("deliver: %v", err)
	}

	call := handler.lastCall()
	if call.eventType != EventDMVersionCreated {
		t.Errorf("eventType = %s, want %s", call.eventType, EventDMVersionCreated)
	}

	got := call.event.(*DMVersionCreatedEvent)
	if got.VersionNumber != 3 {
		t.Errorf("VersionNumber = %d, want 3", got.VersionNumber)
	}
	if got.OriginType != "UPLOAD" {
		t.Errorf("OriginType = %q, want %q", got.OriginType, "UPLOAD")
	}
	if got.ParentVersionID != "ver-11" {
		t.Errorf("ParentVersionID = %q, want %q", got.ParentVersionID, "ver-11")
	}
}

// ---------------------------------------------------------------------------
// Invalid JSON: WARN + ACK (poison pill protection)
// ---------------------------------------------------------------------------

func TestInvalidJSON_ACKsWithoutRequeue(t *testing.T) {
	mb := newMockBroker()
	handler := &mockEventHandler{}
	cfg := testBrokerConfig()
	c := NewConsumer(mb, handler, testLogger(), cfg)
	if err := c.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Deliver garbage bytes to each of the 12 topics.
	topics := []string{
		"dp.events.status-changed",
		"dp.events.processing-completed",
		"dp.events.processing-failed",
		"dp.events.comparison-completed",
		"dp.events.comparison-failed",
		"lic.events.status-changed",
		"re.events.status-changed",
		"dm.events.version-artifacts-ready",
		"dm.events.version-analysis-ready",
		"dm.events.version-reports-ready",
		"dm.events.version-partially-available",
		"dm.events.version-created",
	}

	for _, topic := range topics {
		err := mb.deliver(context.Background(), topic, []byte("not-json{{{"))
		if err != nil {
			t.Errorf("topic %s: expected nil (ACK) for invalid JSON, got %v", topic, err)
		}
	}

	// Handler should never be called for invalid JSON.
	if handler.callCount() != 0 {
		t.Errorf("handler called %d times, want 0 for invalid JSON", handler.callCount())
	}
}

func TestInvalidJSON_EmptyBody(t *testing.T) {
	mb := newMockBroker()
	handler := &mockEventHandler{}
	cfg := testBrokerConfig()
	c := NewConsumer(mb, handler, testLogger(), cfg)
	if err := c.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	err := mb.deliver(context.Background(), "dp.events.status-changed", []byte{})
	if err != nil {
		t.Errorf("expected nil (ACK) for empty body, got %v", err)
	}

	if handler.callCount() != 0 {
		t.Errorf("handler called %d times, want 0", handler.callCount())
	}
}

// ---------------------------------------------------------------------------
// Retry counting: NACK up to maxAttempts, then ACK + give up
// ---------------------------------------------------------------------------

func TestRetry_NACKsUpToMaxRetries_ThenACKs(t *testing.T) {
	mb := newMockBroker()
	handler := &mockEventHandler{returnErr: errors.New("transient failure")}
	retries := newMockRetryTracker()
	cfg := testBrokerConfig()

	c := newConsumerWithRetryTracker(mb, handler, retries, testLogger(), cfg)
	if err := c.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	event := DPStatusChangedEvent{
		CorrelationID: "corr-retry",
		JobID:         "job-retry",
		Status:        "IN_PROGRESS",
	}
	body := mustMarshal(t, event)
	topic := "dp.events.status-changed"

	// First delivery: attempt 1 -> NACK (error returned).
	err := mb.deliver(context.Background(), topic, body)
	if err == nil {
		t.Error("attempt 1: expected error (NACK), got nil")
	}

	// Second delivery: attempt 2 -> NACK.
	err = mb.deliver(context.Background(), topic, body)
	if err == nil {
		t.Error("attempt 2: expected error (NACK), got nil")
	}

	// Third delivery: attempt 3 >= maxAttempts -> ACK (nil returned).
	err = mb.deliver(context.Background(), topic, body)
	if err != nil {
		t.Errorf("attempt 3: expected nil (ACK after max retries), got %v", err)
	}

	// Verify retry tracker was cleaned up.
	retryKey := "dp.status-changed:job-retry:IN_PROGRESS"
	if !retries.wasRemoved(retryKey) {
		t.Errorf("retry key %q was not removed after max retries", retryKey)
	}
}

func TestRetry_SuccessResetsCounter(t *testing.T) {
	mb := newMockBroker()
	handler := &mockEventHandler{returnErr: errors.New("transient failure")}
	retries := newMockRetryTracker()
	cfg := testBrokerConfig()

	c := newConsumerWithRetryTracker(mb, handler, retries, testLogger(), cfg)
	if err := c.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	event := DPProcessingCompletedEvent{
		CorrelationID: "corr-reset",
		JobID:         "job-reset",
	}
	body := mustMarshal(t, event)
	topic := "dp.events.processing-completed"

	// First delivery: fail.
	_ = mb.deliver(context.Background(), topic, body)

	// Second delivery: succeed.
	handler.mu.Lock()
	handler.returnErr = nil
	handler.mu.Unlock()

	err := mb.deliver(context.Background(), topic, body)
	if err != nil {
		t.Errorf("expected nil (ACK) on success, got %v", err)
	}

	retryKey := "dp.processing-completed:job-reset"
	if !retries.wasRemoved(retryKey) {
		t.Errorf("retry key %q was not removed after successful delivery", retryKey)
	}
}

// ---------------------------------------------------------------------------
// Context enrichment tests
// ---------------------------------------------------------------------------

func TestContextEnrichment_DPEvent(t *testing.T) {
	mb := newMockBroker()
	customHandler := &contextCapturingHandler{}
	cfg := testBrokerConfig()

	c := NewConsumer(mb, customHandler, testLogger(), cfg)
	if err := c.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	event := DPStatusChangedEvent{
		CorrelationID:  "corr-ctx",
		OrganizationID: "org-ctx",
		DocumentID:     "doc-ctx",
		VersionID:      "ver-ctx",
		JobID:          "job-ctx",
		Status:         "QUEUED",
	}

	_ = mb.deliver(context.Background(), "dp.events.status-changed", mustMarshal(t, event))

	rc := logger.RequestContextFrom(customHandler.ctx())
	if rc.CorrelationID != "corr-ctx" {
		t.Errorf("CorrelationID = %q, want %q", rc.CorrelationID, "corr-ctx")
	}
	if rc.OrganizationID != "org-ctx" {
		t.Errorf("OrganizationID = %q, want %q", rc.OrganizationID, "org-ctx")
	}
	if rc.DocumentID != "doc-ctx" {
		t.Errorf("DocumentID = %q, want %q", rc.DocumentID, "doc-ctx")
	}
	if rc.VersionID != "ver-ctx" {
		t.Errorf("VersionID = %q, want %q", rc.VersionID, "ver-ctx")
	}
	if rc.JobID != "job-ctx" {
		t.Errorf("JobID = %q, want %q", rc.JobID, "job-ctx")
	}
}

func TestContextEnrichment_DMEvent_NoJobID(t *testing.T) {
	mb := newMockBroker()
	customHandler := &contextCapturingHandler{}
	cfg := testBrokerConfig()

	c := NewConsumer(mb, customHandler, testLogger(), cfg)
	if err := c.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	event := DMVersionCreatedEvent{
		CorrelationID:   "corr-dm",
		DocumentID:      "doc-dm",
		VersionID:       "ver-dm",
		OrganizationID:  "org-dm",
		VersionNumber:   1,
		OriginType:      "UPLOAD",
		CreatedByUserID: "user-dm",
	}

	_ = mb.deliver(context.Background(), "dm.events.version-created", mustMarshal(t, event))

	rc := logger.RequestContextFrom(customHandler.ctx())
	if rc.CorrelationID != "corr-dm" {
		t.Errorf("CorrelationID = %q, want %q", rc.CorrelationID, "corr-dm")
	}
	if rc.JobID != "" {
		t.Errorf("JobID = %q, want empty (DM events have no job_id)", rc.JobID)
	}
	if rc.DocumentID != "doc-dm" {
		t.Errorf("DocumentID = %q, want %q", rc.DocumentID, "doc-dm")
	}
}

// contextCapturingHandler captures the context passed to HandleEvent.
type contextCapturingHandler struct {
	mu       sync.Mutex
	captured context.Context
}

func (h *contextCapturingHandler) HandleEvent(ctx context.Context, _ EventType, _ any) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.captured = ctx
	return nil
}

func (h *contextCapturingHandler) ctx() context.Context {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.captured
}

// ---------------------------------------------------------------------------
// rawPreview tests
// ---------------------------------------------------------------------------

func TestRawPreview_Short(t *testing.T) {
	input := []byte("short message")
	got := rawPreview(input)
	if got != "short message" {
		t.Errorf("rawPreview = %q, want %q", got, "short message")
	}
}

func TestRawPreview_Truncated(t *testing.T) {
	input := make([]byte, 300)
	for i := range input {
		input[i] = 'x'
	}
	got := rawPreview(input)
	if len(got) > 210 { // 200 + "..."
		t.Errorf("rawPreview length = %d, expected <= 203", len(got))
	}
	if got[len(got)-3:] != "..." {
		t.Errorf("rawPreview should end with ..., got %q", got[len(got)-3:])
	}
}

// ---------------------------------------------------------------------------
// buildRetryKey tests
// ---------------------------------------------------------------------------

func TestBuildRetryKey_DPStatusChanged_IncludesStatus(t *testing.T) {
	event := &DPStatusChangedEvent{JobID: "j1", Status: "QUEUED"}
	key := buildRetryKey(EventDPStatusChanged, event)
	expected := "dp.status-changed:j1:QUEUED"
	if key != expected {
		t.Errorf("retryKey = %q, want %q", key, expected)
	}
}

func TestBuildRetryKey_DPProcessingCompleted(t *testing.T) {
	event := &DPProcessingCompletedEvent{JobID: "j2"}
	key := buildRetryKey(EventDPProcessingCompleted, event)
	expected := "dp.processing-completed:j2"
	if key != expected {
		t.Errorf("retryKey = %q, want %q", key, expected)
	}
}

func TestBuildRetryKey_DMEvent_UsesDocAndVersion(t *testing.T) {
	event := &DMVersionCreatedEvent{DocumentID: "d1", VersionID: "v1"}
	key := buildRetryKey(EventDMVersionCreated, event)
	expected := "dm.version-created:d1:v1"
	if key != expected {
		t.Errorf("retryKey = %q, want %q", key, expected)
	}
}

// ---------------------------------------------------------------------------
// Constructor panic tests
// ---------------------------------------------------------------------------

func TestNewConsumer_PanicOnNilBroker(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for nil broker")
		}
	}()
	NewConsumer(nil, &mockEventHandler{}, testLogger(), testBrokerConfig())
}

func TestNewConsumer_PanicOnNilHandler(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for nil handler")
		}
	}()
	NewConsumer(newMockBroker(), nil, testLogger(), testBrokerConfig())
}

func TestNewConsumer_PanicOnNilLogger(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for nil logger")
		}
	}()
	NewConsumer(newMockBroker(), &mockEventHandler{}, nil, testBrokerConfig())
}

// ---------------------------------------------------------------------------
// Interface compile-time checks
// ---------------------------------------------------------------------------

func TestMockBroker_ImplementsBrokerSubscriber(t *testing.T) {
	var _ BrokerSubscriber = (*mockBroker)(nil)
}

func TestMockEventHandler_ImplementsEventHandler(t *testing.T) {
	var _ EventHandler = (*mockEventHandler)(nil)
}

func TestContextCapturingHandler_ImplementsEventHandler(t *testing.T) {
	var _ EventHandler = (*contextCapturingHandler)(nil)
}

func TestMockRetryTracker_ImplementsRetryTracker(t *testing.T) {
	var _ RetryTracker = (*mockRetryTracker)(nil)
}

// ---------------------------------------------------------------------------
// Optional field handling
// ---------------------------------------------------------------------------

func TestOptionalFields_OmittedInJSON(t *testing.T) {
	mb := newMockBroker()
	handler := &mockEventHandler{}
	cfg := testBrokerConfig()
	c := NewConsumer(mb, handler, testLogger(), cfg)
	if err := c.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// JSON with only required fields; optional fields omitted.
	payload := `{
		"correlation_id": "corr-opt",
		"timestamp": "2026-04-09T10:12:00Z",
		"job_id": "job-opt",
		"document_id": "doc-opt",
		"version_id": "ver-opt",
		"status": "QUEUED"
	}`

	err := mb.deliver(context.Background(), "dp.events.status-changed", []byte(payload))
	if err != nil {
		t.Fatalf("deliver: %v", err)
	}

	got := handler.lastCall().event.(*DPStatusChangedEvent)
	if got.OrganizationID != "" {
		t.Errorf("OrganizationID = %q, want empty", got.OrganizationID)
	}
	if got.Stage != "" {
		t.Errorf("Stage = %q, want empty", got.Stage)
	}
	if got.Message != "" {
		t.Errorf("Message = %q, want empty", got.Message)
	}
}

func TestLICEvent_IsRetryableNilWhenOmitted(t *testing.T) {
	mb := newMockBroker()
	handler := &mockEventHandler{}
	cfg := testBrokerConfig()
	c := NewConsumer(mb, handler, testLogger(), cfg)
	if err := c.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	payload := `{
		"correlation_id": "c",
		"timestamp": "2026-04-09T10:00:00Z",
		"job_id": "j",
		"document_id": "d",
		"version_id": "v",
		"organization_id": "o",
		"status": "IN_PROGRESS"
	}`

	err := mb.deliver(context.Background(), "lic.events.status-changed", []byte(payload))
	if err != nil {
		t.Fatalf("deliver: %v", err)
	}

	got := handler.lastCall().event.(*LICStatusChangedEvent)
	if got.IsRetryable != nil {
		t.Errorf("IsRetryable = %v, want nil for non-FAILED status", *got.IsRetryable)
	}
}

// ---------------------------------------------------------------------------
// Extra JSON fields are tolerated (forward compatibility)
// ---------------------------------------------------------------------------

func TestUnknownJSONFields_AreIgnored(t *testing.T) {
	mb := newMockBroker()
	handler := &mockEventHandler{}
	cfg := testBrokerConfig()
	c := NewConsumer(mb, handler, testLogger(), cfg)
	if err := c.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	payload := `{
		"correlation_id": "corr-unknown",
		"timestamp": "2026-04-09T10:13:00Z",
		"job_id": "job-unk",
		"document_id": "doc-unk",
		"version_id": "ver-unk",
		"status": "COMPLETED",
		"future_field": "should be ignored",
		"another_new_field": 42
	}`

	err := mb.deliver(context.Background(), "dp.events.status-changed", []byte(payload))
	if err != nil {
		t.Fatalf("deliver: %v", err)
	}

	if handler.callCount() != 1 {
		t.Errorf("handler called %d times, want 1", handler.callCount())
	}

	got := handler.lastCall().event.(*DPStatusChangedEvent)
	if got.JobID != "job-unk" {
		t.Errorf("JobID = %q, want %q", got.JobID, "job-unk")
	}
}

// ---------------------------------------------------------------------------
// inMemoryRetryTracker unit tests
// ---------------------------------------------------------------------------

func TestInMemoryRetryTracker_IncrementAndRemove(t *testing.T) {
	tracker := newInMemoryRetryTracker()

	if got := tracker.Increment("key-1"); got != 1 {
		t.Errorf("Increment 1: got %d, want 1", got)
	}
	if got := tracker.Increment("key-1"); got != 2 {
		t.Errorf("Increment 2: got %d, want 2", got)
	}

	tracker.Remove("key-1")

	// After removal, counter resets.
	if got := tracker.Increment("key-1"); got != 1 {
		t.Errorf("Increment after Remove: got %d, want 1", got)
	}
}

func TestInMemoryRetryTracker_IndependentKeys(t *testing.T) {
	tracker := newInMemoryRetryTracker()

	tracker.Increment("a")
	tracker.Increment("a")
	tracker.Increment("b")

	if tracker.counters["a"] != 2 {
		t.Errorf("a count = %d, want 2", tracker.counters["a"])
	}
	if tracker.counters["b"] != 1 {
		t.Errorf("b count = %d, want 1", tracker.counters["b"])
	}
}
