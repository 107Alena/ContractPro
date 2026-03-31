package dm

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"contractpro/document-processing/internal/config"
	"contractpro/document-processing/internal/domain/model"
	"contractpro/document-processing/internal/domain/port"
	"contractpro/document-processing/internal/infra/observability"
)

// --- Mocks ---

// subscriberCall records a single Subscribe invocation.
type subscriberCall struct {
	topic   string
	handler func(ctx context.Context, body []byte) error
}

// mockSubscriber is a test double for BrokerSubscriber.
type mockSubscriber struct {
	mu           sync.Mutex
	calls        []subscriberCall
	subscribeErr error
}

func (m *mockSubscriber) Subscribe(topic string, handler func(ctx context.Context, body []byte) error) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, subscriberCall{topic: topic, handler: handler})
	return m.subscribeErr
}

var _ BrokerSubscriber = (*mockSubscriber)(nil)

// mockDMHandler is a test double for port.DMResponseHandler.
type mockDMHandler struct {
	mu sync.Mutex

	artifactsPersistedCalled     bool
	artifactsPersistFailedCalled bool
	semanticTreeProvidedCalled   bool
	diffPersistedCalled          bool
	diffPersistFailedCalled      bool

	lastArtifactsPersisted     model.DocumentProcessingArtifactsPersisted
	lastArtifactsPersistFailed model.DocumentProcessingArtifactsPersistFailed
	lastSemanticTreeProvided   model.SemanticTreeProvided
	lastDiffPersisted          model.DocumentVersionDiffPersisted
	lastDiffPersistFailed      model.DocumentVersionDiffPersistFailed

	artifactsPersistedErr     error
	artifactsPersistFailedErr error
	semanticTreeProvidedErr   error
	diffPersistedErr          error
	diffPersistFailedErr      error
}

func (m *mockDMHandler) HandleArtifactsPersisted(_ context.Context, event model.DocumentProcessingArtifactsPersisted) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.artifactsPersistedCalled = true
	m.lastArtifactsPersisted = event
	return m.artifactsPersistedErr
}

func (m *mockDMHandler) HandleArtifactsPersistFailed(_ context.Context, event model.DocumentProcessingArtifactsPersistFailed) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.artifactsPersistFailedCalled = true
	m.lastArtifactsPersistFailed = event
	return m.artifactsPersistFailedErr
}

func (m *mockDMHandler) HandleSemanticTreeProvided(_ context.Context, event model.SemanticTreeProvided) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.semanticTreeProvidedCalled = true
	m.lastSemanticTreeProvided = event
	return m.semanticTreeProvidedErr
}

func (m *mockDMHandler) HandleDiffPersisted(_ context.Context, event model.DocumentVersionDiffPersisted) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.diffPersistedCalled = true
	m.lastDiffPersisted = event
	return m.diffPersistedErr
}

func (m *mockDMHandler) HandleDiffPersistFailed(_ context.Context, event model.DocumentVersionDiffPersistFailed) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.diffPersistFailedCalled = true
	m.lastDiffPersistFailed = event
	return m.diffPersistFailedErr
}

var _ port.DMResponseHandler = (*mockDMHandler)(nil)

// mockRegistry is a test double for port.PendingResponseRegistryPort.
type mockRegistry struct {
	mu sync.Mutex

	receiveCalled      bool
	receiveErrorCalled bool

	lastReceiveCorrelationID      string
	lastReceiveTree               model.SemanticTree
	lastReceiveErrorCorrelationID string
	lastReceiveErrorErr           error

	receiveErr      error
	receiveErrorErr error
}

func (m *mockRegistry) Register(_ string, _ []string) error { return nil }
func (m *mockRegistry) AwaitAll(_ context.Context, _ string) ([]port.PendingResponse, error) {
	return nil, nil
}
func (m *mockRegistry) Cancel(_ string) {}

func (m *mockRegistry) Receive(correlationID string, tree model.SemanticTree) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.receiveCalled = true
	m.lastReceiveCorrelationID = correlationID
	m.lastReceiveTree = tree
	return m.receiveErr
}

func (m *mockRegistry) ReceiveError(correlationID string, err error) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.receiveErrorCalled = true
	m.lastReceiveErrorCorrelationID = correlationID
	m.lastReceiveErrorErr = err
	return m.receiveErrorErr
}

var _ port.PendingResponseRegistryPort = (*mockRegistry)(nil)

// --- Helpers ---

func testReceiverLogger() *observability.Logger {
	return observability.NewLogger("error") // suppress info/warn/debug in tests
}

func testReceiverBrokerCfg() config.BrokerConfig {
	return config.BrokerConfig{
		TopicDMArtifactsPersisted:     "dm.responses.artifacts-persisted",
		TopicDMArtifactsPersistFailed: "dm.responses.artifacts-persist-failed",
		TopicDMSemanticTreeProvided:   "dm.responses.semantic-tree-provided",
		TopicDMDiffPersisted:          "dm.responses.diff-persisted",
		TopicDMDiffPersistFailed:      "dm.responses.diff-persist-failed",
	}
}

func mustMarshalReceiver(t *testing.T, v any) []byte {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}
	return data
}

func testReceiverTimestamp() time.Time {
	return time.Date(2026, 3, 24, 12, 0, 0, 0, time.UTC)
}

func testReceiverMeta() model.EventMeta {
	return model.EventMeta{
		CorrelationID: "corr-recv-001",
		Timestamp:     testReceiverTimestamp(),
	}
}

func validArtifactsPersistedEvent() model.DocumentProcessingArtifactsPersisted {
	return model.DocumentProcessingArtifactsPersisted{
		EventMeta:  testReceiverMeta(),
		JobID:      "job-ap-1",
		DocumentID: "doc-ap-1",
	}
}

func validArtifactsPersistFailedEvent() model.DocumentProcessingArtifactsPersistFailed {
	return model.DocumentProcessingArtifactsPersistFailed{
		EventMeta:    testReceiverMeta(),
		JobID:        "job-apf-1",
		DocumentID:   "doc-apf-1",
		ErrorCode:    "STORAGE_QUOTA_EXCEEDED",
		ErrorMessage: "storage quota exceeded",
		IsRetryable:  true,
	}
}

func validSemanticTreeProvidedEvent() model.SemanticTreeProvided {
	return model.SemanticTreeProvided{
		EventMeta:  testReceiverMeta(),
		JobID:      "job-stp-1",
		DocumentID: "doc-stp-1",
		VersionID:  "v1",
		SemanticTree: model.SemanticTree{
			DocumentID: "doc-stp-1",
			Root: &model.SemanticNode{
				ID:   "root",
				Type: model.NodeTypeRoot,
				Children: []*model.SemanticNode{
					{
						ID:      "sec-1",
						Type:    model.NodeTypeSection,
						Content: "Предмет договора",
					},
				},
			},
		},
	}
}

func validDiffPersistedEvent() model.DocumentVersionDiffPersisted {
	return model.DocumentVersionDiffPersisted{
		EventMeta:  model.EventMeta{CorrelationID: "job-dp-1:diff-confirm", Timestamp: testReceiverTimestamp()},
		JobID:      "job-dp-1",
		DocumentID: "doc-dp-1",
	}
}

func validDiffPersistFailedEvent() model.DocumentVersionDiffPersistFailed {
	return model.DocumentVersionDiffPersistFailed{
		EventMeta:    model.EventMeta{CorrelationID: "job-dpf-1:diff-confirm", Timestamp: testReceiverTimestamp()},
		JobID:        "job-dpf-1",
		DocumentID:   "doc-dpf-1",
		ErrorCode:    "DISK_FULL",
		ErrorMessage: "disk full",
		IsRetryable:  false,
	}
}

func newTestReceiver() (*Receiver, *mockSubscriber, *mockDMHandler, *mockRegistry) {
	sub := &mockSubscriber{}
	handler := &mockDMHandler{}
	registry := &mockRegistry{}
	r := NewReceiver(sub, handler, registry, testReceiverLogger(), testReceiverBrokerCfg())
	return r, sub, handler, registry
}

// --- Constructor panic tests ---

func TestNewReceiver_PanicsOnNilBroker(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for nil broker")
		}
	}()
	NewReceiver(nil, &mockDMHandler{}, &mockRegistry{}, testReceiverLogger(), testReceiverBrokerCfg())
}

func TestNewReceiver_PanicsOnNilHandler(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for nil handler")
		}
	}()
	NewReceiver(&mockSubscriber{}, nil, &mockRegistry{}, testReceiverLogger(), testReceiverBrokerCfg())
}

func TestNewReceiver_PanicsOnNilRegistry(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for nil registry")
		}
	}()
	NewReceiver(&mockSubscriber{}, &mockDMHandler{}, nil, testReceiverLogger(), testReceiverBrokerCfg())
}

func TestNewReceiver_PanicsOnNilLogger(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for nil logger")
		}
	}()
	NewReceiver(&mockSubscriber{}, &mockDMHandler{}, &mockRegistry{}, nil, testReceiverBrokerCfg())
}

func TestNewReceiver_PanicsOnEmptyTopics(t *testing.T) {
	topicFields := []struct {
		name  string
		setup func(cfg *config.BrokerConfig)
	}{
		{"TopicDMArtifactsPersisted", func(cfg *config.BrokerConfig) { cfg.TopicDMArtifactsPersisted = "" }},
		{"TopicDMArtifactsPersistFailed", func(cfg *config.BrokerConfig) { cfg.TopicDMArtifactsPersistFailed = "" }},
		{"TopicDMSemanticTreeProvided", func(cfg *config.BrokerConfig) { cfg.TopicDMSemanticTreeProvided = "" }},
		{"TopicDMDiffPersisted", func(cfg *config.BrokerConfig) { cfg.TopicDMDiffPersisted = "" }},
		{"TopicDMDiffPersistFailed", func(cfg *config.BrokerConfig) { cfg.TopicDMDiffPersistFailed = "  " }}, // whitespace-only
	}

	for _, tc := range topicFields {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r == nil {
					t.Fatalf("expected panic for empty %s", tc.name)
				}
			}()
			cfg := testReceiverBrokerCfg()
			tc.setup(&cfg)
			NewReceiver(&mockSubscriber{}, &mockDMHandler{}, &mockRegistry{}, testReceiverLogger(), cfg)
		})
	}
}

// --- Start tests ---

func TestStart_SubscribesToAllTopics(t *testing.T) {
	r, sub, _, _ := newTestReceiver()

	if err := r.Start(); err != nil {
		t.Fatalf("Start() returned error: %v", err)
	}

	sub.mu.Lock()
	defer sub.mu.Unlock()

	if len(sub.calls) != 5 {
		t.Fatalf("expected 5 Subscribe calls, got %d", len(sub.calls))
	}

	cfg := testReceiverBrokerCfg()
	expectedTopics := []string{
		cfg.TopicDMArtifactsPersisted,
		cfg.TopicDMArtifactsPersistFailed,
		cfg.TopicDMSemanticTreeProvided,
		cfg.TopicDMDiffPersisted,
		cfg.TopicDMDiffPersistFailed,
	}
	for i, want := range expectedTopics {
		if sub.calls[i].topic != want {
			t.Errorf("subscription[%d].topic = %q, want %q", i, sub.calls[i].topic, want)
		}
	}
}

func TestStart_SubscribeFailure(t *testing.T) {
	sub := &mockSubscriber{subscribeErr: errors.New("connection refused")}
	r := NewReceiver(sub, &mockDMHandler{}, &mockRegistry{}, testReceiverLogger(), testReceiverBrokerCfg())

	err := r.Start()
	if err == nil {
		t.Fatal("expected error from Start")
	}
	if !errors.Is(err, sub.subscribeErr) {
		t.Errorf("expected wrapped subscribe error, got: %v", err)
	}
}

func TestStart_Idempotent(t *testing.T) {
	r, sub, _, _ := newTestReceiver()

	if err := r.Start(); err != nil {
		t.Fatalf("first Start() error: %v", err)
	}
	if err := r.Start(); err != nil {
		t.Fatalf("second Start() error: %v", err)
	}

	sub.mu.Lock()
	defer sub.mu.Unlock()

	if len(sub.calls) != 5 {
		t.Errorf("expected 5 Subscribe calls (idempotent), got %d", len(sub.calls))
	}
}

func TestStart_PartialSubscribeFailure(t *testing.T) {
	// Fails on the third subscription (semanticTreeProvided)
	sub := &failNthSubscriber{failAt: 3}
	r := NewReceiver(sub, &mockDMHandler{}, &mockRegistry{}, testReceiverLogger(), testReceiverBrokerCfg())

	err := r.Start()
	if err == nil {
		t.Fatal("expected error from Start when third subscribe fails")
	}

	sub.mu.Lock()
	defer sub.mu.Unlock()

	if len(sub.calls) != 3 {
		t.Errorf("expected 3 Subscribe attempts, got %d", len(sub.calls))
	}
}

type failNthSubscriber struct {
	mu     sync.Mutex
	calls  []subscriberCall
	count  int
	failAt int
}

func (b *failNthSubscriber) Subscribe(topic string, handler func(ctx context.Context, body []byte) error) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.calls = append(b.calls, subscriberCall{topic: topic, handler: handler})
	b.count++
	if b.count == b.failAt {
		return errors.New("subscribe failed")
	}
	return nil
}

// --- handleArtifactsPersisted tests ---

func TestHandleArtifactsPersisted_ValidEvent(t *testing.T) {
	r, _, handler, _ := newTestReceiver()
	event := validArtifactsPersistedEvent()
	body := mustMarshalReceiver(t, event)

	err := r.handleArtifactsPersisted(context.Background(), body)
	if err != nil {
		t.Fatalf("expected nil, got: %v", err)
	}

	handler.mu.Lock()
	defer handler.mu.Unlock()

	if !handler.artifactsPersistedCalled {
		t.Fatal("handler was not called")
	}
	if handler.lastArtifactsPersisted.JobID != event.JobID {
		t.Errorf("job_id = %q, want %q", handler.lastArtifactsPersisted.JobID, event.JobID)
	}
	if handler.lastArtifactsPersisted.DocumentID != event.DocumentID {
		t.Errorf("document_id = %q, want %q", handler.lastArtifactsPersisted.DocumentID, event.DocumentID)
	}
	if handler.lastArtifactsPersisted.CorrelationID != event.CorrelationID {
		t.Errorf("correlation_id = %q, want %q", handler.lastArtifactsPersisted.CorrelationID, event.CorrelationID)
	}
}

func TestHandleArtifactsPersisted_InvalidJSON(t *testing.T) {
	r, _, handler, _ := newTestReceiver()

	err := r.handleArtifactsPersisted(context.Background(), []byte("not-json"))
	if err != nil {
		t.Fatalf("expected nil (ack), got: %v", err)
	}
	if handler.artifactsPersistedCalled {
		t.Fatal("handler should not be called for invalid JSON")
	}
}

func TestHandleArtifactsPersisted_MissingRequiredFields(t *testing.T) {
	r, _, handler, _ := newTestReceiver()
	event := model.DocumentProcessingArtifactsPersisted{} // empty
	body := mustMarshalReceiver(t, event)

	err := r.handleArtifactsPersisted(context.Background(), body)
	if err != nil {
		t.Fatalf("expected nil (ack), got: %v", err)
	}
	if handler.artifactsPersistedCalled {
		t.Fatal("handler should not be called for invalid event")
	}
}

func TestHandleArtifactsPersisted_HandlerError_ReturnsNil(t *testing.T) {
	r, _, handler, _ := newTestReceiver()
	handler.artifactsPersistedErr = errors.New("orchestrator error")

	body := mustMarshalReceiver(t, validArtifactsPersistedEvent())
	err := r.handleArtifactsPersisted(context.Background(), body)
	if err != nil {
		t.Fatalf("expected nil (ack despite handler error), got: %v", err)
	}
	if !handler.artifactsPersistedCalled {
		t.Fatal("handler should have been called")
	}
}

func TestHandleArtifactsPersisted_EmptyBody(t *testing.T) {
	r, _, handler, _ := newTestReceiver()

	err := r.handleArtifactsPersisted(context.Background(), []byte{})
	if err != nil {
		t.Fatalf("expected nil (ack), got: %v", err)
	}
	if handler.artifactsPersistedCalled {
		t.Fatal("handler should not be called for empty body")
	}
}

func TestHandleArtifactsPersisted_NilBody(t *testing.T) {
	r, _, handler, _ := newTestReceiver()

	err := r.handleArtifactsPersisted(context.Background(), nil)
	if err != nil {
		t.Fatalf("expected nil (ack), got: %v", err)
	}
	if handler.artifactsPersistedCalled {
		t.Fatal("handler should not be called for nil body")
	}
}

// --- handleArtifactsPersistFailed tests ---

func TestHandleArtifactsPersistFailed_ValidEvent(t *testing.T) {
	r, _, handler, _ := newTestReceiver()
	event := validArtifactsPersistFailedEvent()
	body := mustMarshalReceiver(t, event)

	err := r.handleArtifactsPersistFailed(context.Background(), body)
	if err != nil {
		t.Fatalf("expected nil, got: %v", err)
	}

	handler.mu.Lock()
	defer handler.mu.Unlock()

	if !handler.artifactsPersistFailedCalled {
		t.Fatal("handler was not called")
	}
	if handler.lastArtifactsPersistFailed.JobID != event.JobID {
		t.Errorf("job_id = %q, want %q", handler.lastArtifactsPersistFailed.JobID, event.JobID)
	}
	if handler.lastArtifactsPersistFailed.ErrorCode != event.ErrorCode {
		t.Errorf("error_code = %q, want %q", handler.lastArtifactsPersistFailed.ErrorCode, event.ErrorCode)
	}
	if handler.lastArtifactsPersistFailed.ErrorMessage != event.ErrorMessage {
		t.Errorf("error_message = %q, want %q", handler.lastArtifactsPersistFailed.ErrorMessage, event.ErrorMessage)
	}
	if handler.lastArtifactsPersistFailed.IsRetryable != event.IsRetryable {
		t.Errorf("is_retryable = %v, want %v", handler.lastArtifactsPersistFailed.IsRetryable, event.IsRetryable)
	}
}

func TestHandleArtifactsPersistFailed_InvalidJSON(t *testing.T) {
	r, _, handler, _ := newTestReceiver()

	err := r.handleArtifactsPersistFailed(context.Background(), []byte("{bad"))
	if err != nil {
		t.Fatalf("expected nil (ack), got: %v", err)
	}
	if handler.artifactsPersistFailedCalled {
		t.Fatal("handler should not be called for invalid JSON")
	}
}

func TestHandleArtifactsPersistFailed_MissingErrorMessage(t *testing.T) {
	r, _, handler, _ := newTestReceiver()
	event := model.DocumentProcessingArtifactsPersistFailed{
		JobID:      "job-1",
		DocumentID: "doc-1",
		// ErrorMessage missing
	}
	body := mustMarshalReceiver(t, event)

	err := r.handleArtifactsPersistFailed(context.Background(), body)
	if err != nil {
		t.Fatalf("expected nil (ack), got: %v", err)
	}
	if handler.artifactsPersistFailedCalled {
		t.Fatal("handler should not be called for invalid event")
	}
}

func TestHandleArtifactsPersistFailed_HandlerError_ReturnsNil(t *testing.T) {
	r, _, handler, _ := newTestReceiver()
	handler.artifactsPersistFailedErr = errors.New("orchestrator error")

	body := mustMarshalReceiver(t, validArtifactsPersistFailedEvent())
	err := r.handleArtifactsPersistFailed(context.Background(), body)
	if err != nil {
		t.Fatalf("expected nil (ack despite handler error), got: %v", err)
	}
	if !handler.artifactsPersistFailedCalled {
		t.Fatal("handler should have been called")
	}
}

// --- handleSemanticTreeProvided tests ---

func TestHandleSemanticTreeProvided_ValidEvent_DispatchesToRegistry(t *testing.T) {
	r, _, handler, registry := newTestReceiver()
	event := validSemanticTreeProvidedEvent()
	body := mustMarshalReceiver(t, event)

	err := r.handleSemanticTreeProvided(context.Background(), body)
	if err != nil {
		t.Fatalf("expected nil, got: %v", err)
	}

	registry.mu.Lock()
	defer registry.mu.Unlock()

	if !registry.receiveCalled {
		t.Fatal("registry.Receive was not called")
	}
	if registry.lastReceiveCorrelationID != event.CorrelationID {
		t.Errorf("correlation_id = %q, want %q", registry.lastReceiveCorrelationID, event.CorrelationID)
	}
	if registry.lastReceiveTree.Root == nil {
		t.Fatal("registry received nil tree root")
	}
	if registry.lastReceiveTree.Root.ID != "root" {
		t.Errorf("tree root ID = %q, want %q", registry.lastReceiveTree.Root.ID, "root")
	}

	// Handler should NOT be called (semantic tree goes to registry, not handler)
	handler.mu.Lock()
	defer handler.mu.Unlock()
	if handler.semanticTreeProvidedCalled {
		t.Fatal("DMResponseHandler should not be called for semantic tree provided (goes to registry)")
	}
}

func TestHandleSemanticTreeProvided_EmptyTree_DispatchesError(t *testing.T) {
	r, _, _, registry := newTestReceiver()
	event := validSemanticTreeProvidedEvent()
	event.SemanticTree = model.SemanticTree{DocumentID: "doc-stp-1"} // Root is nil
	body := mustMarshalReceiver(t, event)

	err := r.handleSemanticTreeProvided(context.Background(), body)
	if err != nil {
		t.Fatalf("expected nil, got: %v", err)
	}

	registry.mu.Lock()
	defer registry.mu.Unlock()

	if registry.receiveCalled {
		t.Fatal("registry.Receive should not be called for empty tree")
	}
	if !registry.receiveErrorCalled {
		t.Fatal("registry.ReceiveError was not called")
	}
	if registry.lastReceiveErrorCorrelationID != event.CorrelationID {
		t.Errorf("correlation_id = %q, want %q", registry.lastReceiveErrorCorrelationID, event.CorrelationID)
	}
	if registry.lastReceiveErrorErr == nil {
		t.Fatal("registry.ReceiveError received nil error")
	}
}

func TestHandleSemanticTreeProvided_InvalidJSON(t *testing.T) {
	r, _, _, registry := newTestReceiver()

	err := r.handleSemanticTreeProvided(context.Background(), []byte("not-json"))
	if err != nil {
		t.Fatalf("expected nil (ack), got: %v", err)
	}
	if registry.receiveCalled || registry.receiveErrorCalled {
		t.Fatal("registry should not be called for invalid JSON")
	}
}

func TestHandleSemanticTreeProvided_MissingCorrelationID(t *testing.T) {
	r, _, _, registry := newTestReceiver()
	event := model.SemanticTreeProvided{
		JobID:      "job-1",
		DocumentID: "doc-1",
		VersionID:  "v1",
		// CorrelationID missing
	}
	body := mustMarshalReceiver(t, event)

	err := r.handleSemanticTreeProvided(context.Background(), body)
	if err != nil {
		t.Fatalf("expected nil (ack), got: %v", err)
	}
	if registry.receiveCalled || registry.receiveErrorCalled {
		t.Fatal("registry should not be called for invalid event")
	}
}

func TestHandleSemanticTreeProvided_MissingVersionID(t *testing.T) {
	r, _, _, registry := newTestReceiver()
	event := model.SemanticTreeProvided{
		EventMeta:  testReceiverMeta(),
		JobID:      "job-1",
		DocumentID: "doc-1",
		// VersionID missing
	}
	body := mustMarshalReceiver(t, event)

	err := r.handleSemanticTreeProvided(context.Background(), body)
	if err != nil {
		t.Fatalf("expected nil (ack), got: %v", err)
	}
	if registry.receiveCalled || registry.receiveErrorCalled {
		t.Fatal("registry should not be called for invalid event")
	}
}

func TestHandleSemanticTreeProvided_RegistryReceiveError_ReturnsNil(t *testing.T) {
	r, _, _, registry := newTestReceiver()
	registry.receiveErr = errors.New("unknown correlation ID")

	body := mustMarshalReceiver(t, validSemanticTreeProvidedEvent())
	err := r.handleSemanticTreeProvided(context.Background(), body)
	if err != nil {
		t.Fatalf("expected nil (ack despite registry error), got: %v", err)
	}
	if !registry.receiveCalled {
		t.Fatal("registry.Receive should have been called")
	}
}

func TestHandleSemanticTreeProvided_RegistryReceiveErrorError_ReturnsNil(t *testing.T) {
	r, _, _, registry := newTestReceiver()
	registry.receiveErrorErr = errors.New("unknown correlation ID")

	event := validSemanticTreeProvidedEvent()
	event.SemanticTree = model.SemanticTree{} // empty tree triggers ReceiveError
	body := mustMarshalReceiver(t, event)

	err := r.handleSemanticTreeProvided(context.Background(), body)
	if err != nil {
		t.Fatalf("expected nil (ack despite registry error), got: %v", err)
	}
	if !registry.receiveErrorCalled {
		t.Fatal("registry.ReceiveError should have been called")
	}
}

// --- handleDiffPersisted tests ---

func TestHandleDiffPersisted_ValidEvent_DispatchesToRegistry(t *testing.T) {
	r, _, handler, registry := newTestReceiver()
	event := validDiffPersistedEvent()
	body := mustMarshalReceiver(t, event)

	err := r.handleDiffPersisted(context.Background(), body)
	if err != nil {
		t.Fatalf("expected nil, got: %v", err)
	}

	registry.mu.Lock()
	defer registry.mu.Unlock()

	if !registry.receiveCalled {
		t.Fatal("registry.Receive was not called")
	}
	if registry.lastReceiveCorrelationID != event.CorrelationID {
		t.Errorf("correlation_id = %q, want %q", registry.lastReceiveCorrelationID, event.CorrelationID)
	}

	// Handler should NOT be called (diff persisted goes to registry, not handler).
	handler.mu.Lock()
	defer handler.mu.Unlock()
	if handler.diffPersistedCalled {
		t.Fatal("DMResponseHandler should not be called for diff persisted (goes to registry)")
	}
}

func TestHandleDiffPersisted_InvalidJSON(t *testing.T) {
	r, _, _, registry := newTestReceiver()

	err := r.handleDiffPersisted(context.Background(), []byte("not-json"))
	if err != nil {
		t.Fatalf("expected nil (ack), got: %v", err)
	}
	if registry.receiveCalled {
		t.Fatal("registry should not be called for invalid JSON")
	}
}

func TestHandleDiffPersisted_MissingRequiredFields(t *testing.T) {
	r, _, _, registry := newTestReceiver()
	event := model.DocumentVersionDiffPersisted{} // empty
	body := mustMarshalReceiver(t, event)

	err := r.handleDiffPersisted(context.Background(), body)
	if err != nil {
		t.Fatalf("expected nil (ack), got: %v", err)
	}
	if registry.receiveCalled {
		t.Fatal("registry should not be called for invalid event")
	}
}

func TestHandleDiffPersisted_MissingCorrelationID(t *testing.T) {
	r, _, _, registry := newTestReceiver()
	event := model.DocumentVersionDiffPersisted{
		JobID:      "job-1",
		DocumentID: "doc-1",
		// CorrelationID missing
	}
	body := mustMarshalReceiver(t, event)

	err := r.handleDiffPersisted(context.Background(), body)
	if err != nil {
		t.Fatalf("expected nil (ack), got: %v", err)
	}
	if registry.receiveCalled {
		t.Fatal("registry should not be called for missing correlation_id")
	}
}

func TestHandleDiffPersisted_RegistryReceiveError_ReturnsNil(t *testing.T) {
	r, _, _, registry := newTestReceiver()
	registry.receiveErr = errors.New("unknown correlation ID")

	body := mustMarshalReceiver(t, validDiffPersistedEvent())
	err := r.handleDiffPersisted(context.Background(), body)
	if err != nil {
		t.Fatalf("expected nil (ack despite registry error), got: %v", err)
	}
	if !registry.receiveCalled {
		t.Fatal("registry.Receive should have been called")
	}
}

// --- handleDiffPersistFailed tests ---

func TestHandleDiffPersistFailed_ValidEvent_DispatchesToRegistry(t *testing.T) {
	r, _, handler, registry := newTestReceiver()
	event := validDiffPersistFailedEvent()
	body := mustMarshalReceiver(t, event)

	err := r.handleDiffPersistFailed(context.Background(), body)
	if err != nil {
		t.Fatalf("expected nil, got: %v", err)
	}

	registry.mu.Lock()
	defer registry.mu.Unlock()

	if !registry.receiveErrorCalled {
		t.Fatal("registry.ReceiveError was not called")
	}
	if registry.lastReceiveErrorCorrelationID != event.CorrelationID {
		t.Errorf("correlation_id = %q, want %q", registry.lastReceiveErrorCorrelationID, event.CorrelationID)
	}
	if registry.lastReceiveErrorErr == nil {
		t.Fatal("registry.ReceiveError received nil error")
	}

	// Verify the error is a DomainError with correct code and retryable flag.
	var domErr *port.DomainError
	if !errors.As(registry.lastReceiveErrorErr, &domErr) {
		t.Fatalf("expected *DomainError, got %T", registry.lastReceiveErrorErr)
	}
	if domErr.Code != port.ErrCodeDMDiffPersistFailed {
		t.Errorf("error code = %q, want %q", domErr.Code, port.ErrCodeDMDiffPersistFailed)
	}
	if domErr.Retryable != event.IsRetryable {
		t.Errorf("retryable = %v, want %v", domErr.Retryable, event.IsRetryable)
	}

	// Handler should NOT be called (diff persist failed goes to registry, not handler).
	handler.mu.Lock()
	defer handler.mu.Unlock()
	if handler.diffPersistFailedCalled {
		t.Fatal("DMResponseHandler should not be called for diff persist failed (goes to registry)")
	}
}

func TestHandleDiffPersistFailed_RetryableEvent_DispatchesToRegistry(t *testing.T) {
	r, _, _, registry := newTestReceiver()
	event := validDiffPersistFailedEvent()
	event.IsRetryable = true
	body := mustMarshalReceiver(t, event)

	err := r.handleDiffPersistFailed(context.Background(), body)
	if err != nil {
		t.Fatalf("expected nil, got: %v", err)
	}

	registry.mu.Lock()
	defer registry.mu.Unlock()

	if !registry.receiveErrorCalled {
		t.Fatal("registry.ReceiveError was not called")
	}

	var domErr *port.DomainError
	if !errors.As(registry.lastReceiveErrorErr, &domErr) {
		t.Fatalf("expected *DomainError, got %T", registry.lastReceiveErrorErr)
	}
	if !domErr.Retryable {
		t.Error("expected retryable=true for retryable DM event")
	}
}

func TestHandleDiffPersistFailed_InvalidJSON(t *testing.T) {
	r, _, _, registry := newTestReceiver()

	err := r.handleDiffPersistFailed(context.Background(), []byte("not-json"))
	if err != nil {
		t.Fatalf("expected nil (ack), got: %v", err)
	}
	if registry.receiveErrorCalled {
		t.Fatal("registry should not be called for invalid JSON")
	}
}

func TestHandleDiffPersistFailed_MissingErrorMessage(t *testing.T) {
	r, _, _, registry := newTestReceiver()
	event := model.DocumentVersionDiffPersistFailed{
		EventMeta:  model.EventMeta{CorrelationID: "corr-1"},
		JobID:      "job-1",
		DocumentID: "doc-1",
		// ErrorMessage missing
	}
	body := mustMarshalReceiver(t, event)

	err := r.handleDiffPersistFailed(context.Background(), body)
	if err != nil {
		t.Fatalf("expected nil (ack), got: %v", err)
	}
	if registry.receiveErrorCalled {
		t.Fatal("registry should not be called for invalid event")
	}
}

func TestHandleDiffPersistFailed_MissingCorrelationID(t *testing.T) {
	r, _, _, registry := newTestReceiver()
	event := model.DocumentVersionDiffPersistFailed{
		JobID:        "job-1",
		DocumentID:   "doc-1",
		ErrorMessage: "disk full",
		// CorrelationID missing
	}
	body := mustMarshalReceiver(t, event)

	err := r.handleDiffPersistFailed(context.Background(), body)
	if err != nil {
		t.Fatalf("expected nil (ack), got: %v", err)
	}
	if registry.receiveErrorCalled {
		t.Fatal("registry should not be called for missing correlation_id")
	}
}

func TestHandleDiffPersistFailed_RegistryReceiveErrorError_ReturnsNil(t *testing.T) {
	r, _, _, registry := newTestReceiver()
	registry.receiveErrorErr = errors.New("unknown correlation ID")

	body := mustMarshalReceiver(t, validDiffPersistFailedEvent())
	err := r.handleDiffPersistFailed(context.Background(), body)
	if err != nil {
		t.Fatalf("expected nil (ack despite registry error), got: %v", err)
	}
	if !registry.receiveErrorCalled {
		t.Fatal("registry.ReceiveError should have been called")
	}
}

// --- handleSemanticTreeProvided error field tests ---

func TestHandleSemanticTreeProvided_ErrorMessage_DispatchesTypedError(t *testing.T) {
	r, _, handler, registry := newTestReceiver()
	event := model.SemanticTreeProvided{
		EventMeta:    testReceiverMeta(),
		JobID:        "job-stp-err-1",
		DocumentID:   "doc-stp-err-1",
		VersionID:    "v1",
		ErrorCode:    "VERSION_NOT_FOUND",
		ErrorMessage: "version v1 not found in DM",
		IsRetryable:  false,
	}
	body := mustMarshalReceiver(t, event)

	err := r.handleSemanticTreeProvided(context.Background(), body)
	if err != nil {
		t.Fatalf("expected nil, got: %v", err)
	}

	registry.mu.Lock()
	defer registry.mu.Unlock()

	if registry.receiveCalled {
		t.Fatal("registry.Receive should not be called for error response")
	}
	if !registry.receiveErrorCalled {
		t.Fatal("registry.ReceiveError was not called")
	}
	if registry.lastReceiveErrorCorrelationID != event.CorrelationID {
		t.Errorf("correlation_id = %q, want %q", registry.lastReceiveErrorCorrelationID, event.CorrelationID)
	}

	// Verify typed DomainError.
	var domErr *port.DomainError
	if !errors.As(registry.lastReceiveErrorErr, &domErr) {
		t.Fatalf("expected *DomainError, got %T", registry.lastReceiveErrorErr)
	}
	if domErr.Code != port.ErrCodeDMSemanticTreeFailed {
		t.Errorf("error code = %q, want %q", domErr.Code, port.ErrCodeDMSemanticTreeFailed)
	}
	if domErr.Message != event.ErrorMessage {
		t.Errorf("error message = %q, want %q", domErr.Message, event.ErrorMessage)
	}
	if domErr.Retryable != event.IsRetryable {
		t.Errorf("retryable = %v, want %v", domErr.Retryable, event.IsRetryable)
	}

	// Handler should NOT be called.
	handler.mu.Lock()
	defer handler.mu.Unlock()
	if handler.semanticTreeProvidedCalled {
		t.Fatal("DMResponseHandler should not be called (goes to registry)")
	}
}

func TestHandleSemanticTreeProvided_ErrorMessage_RetryableTrue(t *testing.T) {
	r, _, _, registry := newTestReceiver()
	event := model.SemanticTreeProvided{
		EventMeta:    testReceiverMeta(),
		JobID:        "job-stp-err-2",
		DocumentID:   "doc-stp-err-2",
		VersionID:    "v2",
		ErrorCode:    "STORAGE_TEMPORARILY_UNAVAILABLE",
		ErrorMessage: "storage temporarily unavailable",
		IsRetryable:  true,
	}
	body := mustMarshalReceiver(t, event)

	err := r.handleSemanticTreeProvided(context.Background(), body)
	if err != nil {
		t.Fatalf("expected nil, got: %v", err)
	}

	registry.mu.Lock()
	defer registry.mu.Unlock()

	if !registry.receiveErrorCalled {
		t.Fatal("registry.ReceiveError was not called")
	}

	var domErr *port.DomainError
	if !errors.As(registry.lastReceiveErrorErr, &domErr) {
		t.Fatalf("expected *DomainError, got %T", registry.lastReceiveErrorErr)
	}
	if !domErr.Retryable {
		t.Error("expected retryable=true for retryable DM event")
	}
}

func TestHandleSemanticTreeProvided_EmptyErrorMessage_NilRoot_FallbackBehavior(t *testing.T) {
	// When ErrorMessage is empty and Root is nil, the existing fallback
	// (fmt.Errorf) should be used — NOT a typed DomainError.
	r, _, _, registry := newTestReceiver()
	event := validSemanticTreeProvidedEvent()
	event.SemanticTree = model.SemanticTree{DocumentID: "doc-stp-1"} // Root is nil, no ErrorMessage
	body := mustMarshalReceiver(t, event)

	err := r.handleSemanticTreeProvided(context.Background(), body)
	if err != nil {
		t.Fatalf("expected nil, got: %v", err)
	}

	registry.mu.Lock()
	defer registry.mu.Unlock()

	if !registry.receiveErrorCalled {
		t.Fatal("registry.ReceiveError was not called")
	}

	// Should NOT be a DomainError (existing behavior preserved).
	var domErr *port.DomainError
	if errors.As(registry.lastReceiveErrorErr, &domErr) {
		t.Fatal("expected plain error (not DomainError) for empty tree without error fields")
	}
}

func TestHandleSemanticTreeProvided_ErrorMessage_PriorityOverNilRoot(t *testing.T) {
	// ErrorMessage is checked BEFORE Root == nil. When both are present,
	// the typed DomainError from ErrorMessage should be dispatched.
	r, _, _, registry := newTestReceiver()
	event := model.SemanticTreeProvided{
		EventMeta:    testReceiverMeta(),
		JobID:        "job-stp-err-3",
		DocumentID:   "doc-stp-err-3",
		VersionID:    "v3",
		ErrorCode:    "INTERNAL_ERROR",
		ErrorMessage: "internal DM error",
		IsRetryable:  true,
		// SemanticTree.Root is nil
	}
	body := mustMarshalReceiver(t, event)

	err := r.handleSemanticTreeProvided(context.Background(), body)
	if err != nil {
		t.Fatalf("expected nil, got: %v", err)
	}

	registry.mu.Lock()
	defer registry.mu.Unlock()

	if !registry.receiveErrorCalled {
		t.Fatal("registry.ReceiveError was not called")
	}

	var domErr *port.DomainError
	if !errors.As(registry.lastReceiveErrorErr, &domErr) {
		t.Fatalf("expected *DomainError (ErrorMessage takes priority), got %T", registry.lastReceiveErrorErr)
	}
	if domErr.Code != port.ErrCodeDMSemanticTreeFailed {
		t.Errorf("error code = %q, want %q", domErr.Code, port.ErrCodeDMSemanticTreeFailed)
	}
}

func TestHandleSemanticTreeProvided_ErrorResponse_WithoutVersionID_PassesValidation(t *testing.T) {
	// DM error response may omit version_id; validation should still pass.
	r, _, _, registry := newTestReceiver()
	event := model.SemanticTreeProvided{
		EventMeta:    testReceiverMeta(),
		JobID:        "job-stp-err-4",
		DocumentID:   "doc-stp-err-4",
		ErrorCode:    "VERSION_NOT_FOUND",
		ErrorMessage: "version not found",
		IsRetryable:  false,
		// VersionID intentionally empty
	}
	body := mustMarshalReceiver(t, event)

	err := r.handleSemanticTreeProvided(context.Background(), body)
	if err != nil {
		t.Fatalf("expected nil, got: %v", err)
	}

	registry.mu.Lock()
	defer registry.mu.Unlock()

	if !registry.receiveErrorCalled {
		t.Fatal("registry.ReceiveError was not called (error response without version_id should pass validation)")
	}

	var domErr *port.DomainError
	if !errors.As(registry.lastReceiveErrorErr, &domErr) {
		t.Fatalf("expected *DomainError, got %T", registry.lastReceiveErrorErr)
	}
	if domErr.Code != port.ErrCodeDMSemanticTreeFailed {
		t.Errorf("error code = %q, want %q", domErr.Code, port.ErrCodeDMSemanticTreeFailed)
	}
}

// --- Integration: Start -> dispatch via broker handlers ---

func TestIntegration_StartAndDispatchAllEvents(t *testing.T) {
	r, sub, handler, registry := newTestReceiver()

	if err := r.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	sub.mu.Lock()
	handlers := make([]func(ctx context.Context, body []byte) error, len(sub.calls))
	for i, c := range sub.calls {
		handlers[i] = c.handler
	}
	sub.mu.Unlock()

	ctx := context.Background()

	// 1. artifacts persisted
	body := mustMarshalReceiver(t, validArtifactsPersistedEvent())
	if err := handlers[0](ctx, body); err != nil {
		t.Fatalf("artifacts persisted handler returned: %v", err)
	}
	if !handler.artifactsPersistedCalled {
		t.Error("handler not called for artifacts persisted")
	}

	// 2. artifacts persist failed
	body = mustMarshalReceiver(t, validArtifactsPersistFailedEvent())
	if err := handlers[1](ctx, body); err != nil {
		t.Fatalf("artifacts persist failed handler returned: %v", err)
	}
	if !handler.artifactsPersistFailedCalled {
		t.Error("handler not called for artifacts persist failed")
	}

	// 3. semantic tree provided → registry
	body = mustMarshalReceiver(t, validSemanticTreeProvidedEvent())
	if err := handlers[2](ctx, body); err != nil {
		t.Fatalf("semantic tree provided handler returned: %v", err)
	}
	if !registry.receiveCalled {
		t.Error("registry.Receive not called for semantic tree provided")
	}

	// 4. diff persisted → registry
	body = mustMarshalReceiver(t, validDiffPersistedEvent())
	if err := handlers[3](ctx, body); err != nil {
		t.Fatalf("diff persisted handler returned: %v", err)
	}
	// registry.Receive was already called for semantic tree (step 3),
	// so we check that the correlationID matches the diff event.
	registry.mu.Lock()
	diffPersistedReceived := registry.receiveCalled
	lastCorrID := registry.lastReceiveCorrelationID
	registry.mu.Unlock()
	if lastCorrID != validDiffPersistedEvent().CorrelationID {
		t.Errorf("registry.Receive correlationID = %q, want %q", lastCorrID, validDiffPersistedEvent().CorrelationID)
	}
	if !diffPersistedReceived {
		t.Error("registry.Receive not called for diff persisted")
	}

	// 5. diff persist failed → registry
	body = mustMarshalReceiver(t, validDiffPersistFailedEvent())
	if err := handlers[4](ctx, body); err != nil {
		t.Fatalf("diff persist failed handler returned: %v", err)
	}
	registry.mu.Lock()
	if !registry.receiveErrorCalled {
		t.Error("registry.ReceiveError not called for diff persist failed")
	}
	if registry.lastReceiveErrorCorrelationID != validDiffPersistFailedEvent().CorrelationID {
		t.Errorf("registry.ReceiveError correlationID = %q, want %q",
			registry.lastReceiveErrorCorrelationID, validDiffPersistFailedEvent().CorrelationID)
	}
	registry.mu.Unlock()
}

// --- Context enrichment tests ---

type contextCapturingHandler struct {
	mu       sync.Mutex
	lastCtx  context.Context
	inner    *mockDMHandler
}

func (h *contextCapturingHandler) HandleArtifactsPersisted(ctx context.Context, event model.DocumentProcessingArtifactsPersisted) error {
	h.mu.Lock()
	h.lastCtx = ctx
	h.mu.Unlock()
	return h.inner.HandleArtifactsPersisted(ctx, event)
}

func (h *contextCapturingHandler) HandleArtifactsPersistFailed(ctx context.Context, event model.DocumentProcessingArtifactsPersistFailed) error {
	h.mu.Lock()
	h.lastCtx = ctx
	h.mu.Unlock()
	return h.inner.HandleArtifactsPersistFailed(ctx, event)
}

func (h *contextCapturingHandler) HandleSemanticTreeProvided(ctx context.Context, event model.SemanticTreeProvided) error {
	return h.inner.HandleSemanticTreeProvided(ctx, event)
}

func (h *contextCapturingHandler) HandleDiffPersisted(ctx context.Context, event model.DocumentVersionDiffPersisted) error {
	h.mu.Lock()
	h.lastCtx = ctx
	h.mu.Unlock()
	return h.inner.HandleDiffPersisted(ctx, event)
}

func (h *contextCapturingHandler) HandleDiffPersistFailed(ctx context.Context, event model.DocumentVersionDiffPersistFailed) error {
	h.mu.Lock()
	h.lastCtx = ctx
	h.mu.Unlock()
	return h.inner.HandleDiffPersistFailed(ctx, event)
}

var _ port.DMResponseHandler = (*contextCapturingHandler)(nil)

func TestHandleArtifactsPersisted_ContextEnrichment(t *testing.T) {
	capturing := &contextCapturingHandler{inner: &mockDMHandler{}}
	r := NewReceiver(&mockSubscriber{}, capturing, &mockRegistry{}, testReceiverLogger(), testReceiverBrokerCfg())

	event := validArtifactsPersistedEvent()
	body := mustMarshalReceiver(t, event)
	_ = r.handleArtifactsPersisted(context.Background(), body)

	capturing.mu.Lock()
	capturedCtx := capturing.lastCtx
	capturing.mu.Unlock()

	jc := observability.JobContextFrom(capturedCtx)
	if jc.JobID != event.JobID {
		t.Errorf("context job_id = %q, want %q", jc.JobID, event.JobID)
	}
	if jc.DocumentID != event.DocumentID {
		t.Errorf("context document_id = %q, want %q", jc.DocumentID, event.DocumentID)
	}
	if jc.CorrelationID != event.CorrelationID {
		t.Errorf("context correlation_id = %q, want %q", jc.CorrelationID, event.CorrelationID)
	}
}

func TestHandleDiffPersistFailed_HandlerNotCalled(t *testing.T) {
	// Verify that the handler is NOT called — diff persist failed goes to registry.
	r, _, handler, registry := newTestReceiver()

	event := validDiffPersistFailedEvent()
	body := mustMarshalReceiver(t, event)
	_ = r.handleDiffPersistFailed(context.Background(), body)

	handler.mu.Lock()
	defer handler.mu.Unlock()
	if handler.diffPersistFailedCalled {
		t.Fatal("handler should not be called (diff persist failed goes to registry)")
	}

	registry.mu.Lock()
	defer registry.mu.Unlock()
	if !registry.receiveErrorCalled {
		t.Fatal("registry.ReceiveError should have been called")
	}
}

// --- rawPreview tests ---

func TestRawPreview_Short(t *testing.T) {
	body := []byte("short message")
	got := rawPreview(body)
	if got != "short message" {
		t.Errorf("rawPreview = %q, want %q", got, "short message")
	}
}

func TestRawPreview_Truncated(t *testing.T) {
	body := make([]byte, 300)
	for i := range body {
		body[i] = 'x'
	}
	got := rawPreview(body)
	if len(got) > 210 { // 200 + "..."
		t.Errorf("rawPreview length = %d, expected <= 210", len(got))
	}
	if got[len(got)-3:] != "..." {
		t.Errorf("expected truncated preview to end with '...', got %q", got[len(got)-5:])
	}
}

func TestRawPreview_Empty(t *testing.T) {
	got := rawPreview([]byte{})
	if got != "" {
		t.Errorf("rawPreview = %q, want empty", got)
	}
}

func TestRawPreview_Nil(t *testing.T) {
	got := rawPreview(nil)
	if got != "" {
		t.Errorf("rawPreview = %q, want empty", got)
	}
}

func TestRawPreview_ExactBoundary(t *testing.T) {
	body := make([]byte, 200)
	for i := range body {
		body[i] = 'a'
	}
	got := rawPreview(body)
	if len(got) != 200 {
		t.Errorf("rawPreview length = %d, expected 200 (no truncation)", len(got))
	}
}
