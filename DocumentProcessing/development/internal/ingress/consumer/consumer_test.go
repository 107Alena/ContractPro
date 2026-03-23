package consumer

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"contractpro/document-processing/internal/config"
	"contractpro/document-processing/internal/domain/model"
	"contractpro/document-processing/internal/infra/observability"
)

// --- mocks ---

type subscriberCall struct {
	topic   string
	handler func(ctx context.Context, body []byte) error
}

type mockBroker struct {
	mu          sync.Mutex
	calls       []subscriberCall
	subscribeErr error
}

func (m *mockBroker) Subscribe(topic string, handler func(ctx context.Context, body []byte) error) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, subscriberCall{topic: topic, handler: handler})
	return m.subscribeErr
}

var _ BrokerSubscriber = (*mockBroker)(nil)

type mockProcessingHandler struct {
	mu      sync.Mutex
	called  bool
	lastCmd model.ProcessDocumentCommand
	err     error
}

func (m *mockProcessingHandler) HandleProcessDocument(_ context.Context, cmd model.ProcessDocumentCommand) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.called = true
	m.lastCmd = cmd
	return m.err
}

type mockComparisonHandler struct {
	mu      sync.Mutex
	called  bool
	lastCmd model.CompareVersionsCommand
	err     error
}

func (m *mockComparisonHandler) HandleCompareVersions(_ context.Context, cmd model.CompareVersionsCommand) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.called = true
	m.lastCmd = cmd
	return m.err
}

// --- helpers ---

func testLogger() *observability.Logger {
	return observability.NewLogger("error") // suppress info/warn/debug in tests
}

func testBrokerCfg() config.BrokerConfig {
	return config.BrokerConfig{
		TopicProcessDocument: "dp.commands.process-document",
		TopicCompareVersions: "dp.commands.compare-versions",
	}
}

func validProcessCmd() model.ProcessDocumentCommand {
	return model.ProcessDocumentCommand{
		JobID:      "job-123",
		DocumentID: "doc-456",
		FileURL:    "https://storage.example.com/files/contract.pdf",
		FileName:   "contract.pdf",
		FileSize:   1024,
		MimeType:   "application/pdf",
	}
}

func validCompareCmd() model.CompareVersionsCommand {
	return model.CompareVersionsCommand{
		JobID:           "job-789",
		DocumentID:      "doc-456",
		BaseVersionID:   "v1",
		TargetVersionID: "v2",
	}
}

func mustMarshal(t *testing.T, v any) []byte {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}
	return data
}

// --- Constructor panic tests ---

func TestNewConsumer_PanicsOnNilBroker(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for nil broker")
		}
	}()
	NewConsumer(nil, &mockProcessingHandler{}, &mockComparisonHandler{}, testLogger(), testBrokerCfg())
}

func TestNewConsumer_PanicsOnNilProcessingHandler(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for nil processing handler")
		}
	}()
	NewConsumer(&mockBroker{}, nil, &mockComparisonHandler{}, testLogger(), testBrokerCfg())
}

func TestNewConsumer_PanicsOnNilComparisonHandler(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for nil comparison handler")
		}
	}()
	NewConsumer(&mockBroker{}, &mockProcessingHandler{}, nil, testLogger(), testBrokerCfg())
}

func TestNewConsumer_PanicsOnNilLogger(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for nil logger")
		}
	}()
	NewConsumer(&mockBroker{}, &mockProcessingHandler{}, &mockComparisonHandler{}, nil, testBrokerCfg())
}

// --- Start tests ---

func TestStart_SubscribesToBothTopics(t *testing.T) {
	broker := &mockBroker{}
	cfg := testBrokerCfg()
	c := NewConsumer(broker, &mockProcessingHandler{}, &mockComparisonHandler{}, testLogger(), cfg)

	if err := c.Start(); err != nil {
		t.Fatalf("Start() returned error: %v", err)
	}

	if len(broker.calls) != 2 {
		t.Fatalf("expected 2 Subscribe calls, got %d", len(broker.calls))
	}

	if broker.calls[0].topic != cfg.TopicProcessDocument {
		t.Errorf("first subscription topic = %q, want %q", broker.calls[0].topic, cfg.TopicProcessDocument)
	}
	if broker.calls[1].topic != cfg.TopicCompareVersions {
		t.Errorf("second subscription topic = %q, want %q", broker.calls[1].topic, cfg.TopicCompareVersions)
	}
}

func TestStart_SubscribeFailure(t *testing.T) {
	broker := &mockBroker{subscribeErr: errors.New("connection refused")}
	c := NewConsumer(broker, &mockProcessingHandler{}, &mockComparisonHandler{}, testLogger(), testBrokerCfg())

	err := c.Start()
	if err == nil {
		t.Fatal("expected error from Start")
	}
	if !errors.Is(err, broker.subscribeErr) {
		t.Errorf("expected wrapped subscribe error, got: %v", err)
	}
}

// --- handleProcessDocument tests ---

func TestHandleProcessDocument_ValidMessage(t *testing.T) {
	handler := &mockProcessingHandler{}
	c := NewConsumer(&mockBroker{}, handler, &mockComparisonHandler{}, testLogger(), testBrokerCfg())

	cmd := validProcessCmd()
	body := mustMarshal(t, cmd)

	err := c.handleProcessDocument(context.Background(), body)
	if err != nil {
		t.Fatalf("expected nil, got: %v", err)
	}

	if !handler.called {
		t.Fatal("processing handler was not called")
	}
	if handler.lastCmd.JobID != cmd.JobID {
		t.Errorf("handler received job_id = %q, want %q", handler.lastCmd.JobID, cmd.JobID)
	}
	if handler.lastCmd.DocumentID != cmd.DocumentID {
		t.Errorf("handler received document_id = %q, want %q", handler.lastCmd.DocumentID, cmd.DocumentID)
	}
	if handler.lastCmd.FileURL != cmd.FileURL {
		t.Errorf("handler received file_url = %q, want %q", handler.lastCmd.FileURL, cmd.FileURL)
	}
}

func TestHandleProcessDocument_InvalidJSON(t *testing.T) {
	handler := &mockProcessingHandler{}
	c := NewConsumer(&mockBroker{}, handler, &mockComparisonHandler{}, testLogger(), testBrokerCfg())

	err := c.handleProcessDocument(context.Background(), []byte("not-json"))
	if err != nil {
		t.Fatalf("expected nil (ack), got: %v", err)
	}
	if handler.called {
		t.Fatal("handler should not be called for invalid JSON")
	}
}

func TestHandleProcessDocument_MissingRequiredFields(t *testing.T) {
	handler := &mockProcessingHandler{}
	c := NewConsumer(&mockBroker{}, handler, &mockComparisonHandler{}, testLogger(), testBrokerCfg())

	// Command without required job_id
	cmd := model.ProcessDocumentCommand{DocumentID: "doc-1", FileURL: "https://example.com/f.pdf"}
	body := mustMarshal(t, cmd)

	err := c.handleProcessDocument(context.Background(), body)
	if err != nil {
		t.Fatalf("expected nil (ack), got: %v", err)
	}
	if handler.called {
		t.Fatal("handler should not be called for invalid command")
	}
}

func TestHandleProcessDocument_HandlerError_ReturnsNil(t *testing.T) {
	handler := &mockProcessingHandler{err: errors.New("internal processing error")}
	c := NewConsumer(&mockBroker{}, handler, &mockComparisonHandler{}, testLogger(), testBrokerCfg())

	body := mustMarshal(t, validProcessCmd())

	err := c.handleProcessDocument(context.Background(), body)
	if err != nil {
		t.Fatalf("expected nil (ack despite handler error), got: %v", err)
	}
	if !handler.called {
		t.Fatal("handler should have been called")
	}
}

func TestHandleProcessDocument_AllFieldsCopied(t *testing.T) {
	handler := &mockProcessingHandler{}
	c := NewConsumer(&mockBroker{}, handler, &mockComparisonHandler{}, testLogger(), testBrokerCfg())

	cmd := model.ProcessDocumentCommand{
		JobID:      "j1",
		DocumentID: "d1",
		FileURL:    "https://example.com/f.pdf",
		OrgID:      "org-1",
		UserID:     "user-1",
		FileName:   "contract.pdf",
		FileSize:   2048,
		MimeType:   "application/pdf",
		Checksum:   "sha256:abc",
	}
	body := mustMarshal(t, cmd)

	_ = c.handleProcessDocument(context.Background(), body)

	got := handler.lastCmd
	if got.OrgID != cmd.OrgID || got.UserID != cmd.UserID || got.FileName != cmd.FileName ||
		got.FileSize != cmd.FileSize || got.MimeType != cmd.MimeType || got.Checksum != cmd.Checksum {
		t.Errorf("optional fields not preserved: got %+v", got)
	}
}

func TestHandleProcessDocument_EmptyBody(t *testing.T) {
	handler := &mockProcessingHandler{}
	c := NewConsumer(&mockBroker{}, handler, &mockComparisonHandler{}, testLogger(), testBrokerCfg())

	err := c.handleProcessDocument(context.Background(), []byte{})
	if err != nil {
		t.Fatalf("expected nil (ack), got: %v", err)
	}
	if handler.called {
		t.Fatal("handler should not be called for empty body")
	}
}

// --- handleCompareVersions tests ---

func TestHandleCompareVersions_ValidMessage(t *testing.T) {
	handler := &mockComparisonHandler{}
	c := NewConsumer(&mockBroker{}, &mockProcessingHandler{}, handler, testLogger(), testBrokerCfg())

	cmd := validCompareCmd()
	body := mustMarshal(t, cmd)

	err := c.handleCompareVersions(context.Background(), body)
	if err != nil {
		t.Fatalf("expected nil, got: %v", err)
	}

	if !handler.called {
		t.Fatal("comparison handler was not called")
	}
	if handler.lastCmd.JobID != cmd.JobID {
		t.Errorf("handler received job_id = %q, want %q", handler.lastCmd.JobID, cmd.JobID)
	}
	if handler.lastCmd.BaseVersionID != cmd.BaseVersionID {
		t.Errorf("handler received base_version_id = %q, want %q", handler.lastCmd.BaseVersionID, cmd.BaseVersionID)
	}
	if handler.lastCmd.TargetVersionID != cmd.TargetVersionID {
		t.Errorf("handler received target_version_id = %q, want %q", handler.lastCmd.TargetVersionID, cmd.TargetVersionID)
	}
}

func TestHandleCompareVersions_InvalidJSON(t *testing.T) {
	handler := &mockComparisonHandler{}
	c := NewConsumer(&mockBroker{}, &mockProcessingHandler{}, handler, testLogger(), testBrokerCfg())

	err := c.handleCompareVersions(context.Background(), []byte("{invalid"))
	if err != nil {
		t.Fatalf("expected nil (ack), got: %v", err)
	}
	if handler.called {
		t.Fatal("handler should not be called for invalid JSON")
	}
}

func TestHandleCompareVersions_MissingRequiredFields(t *testing.T) {
	handler := &mockComparisonHandler{}
	c := NewConsumer(&mockBroker{}, &mockProcessingHandler{}, handler, testLogger(), testBrokerCfg())

	// Command without base_version_id and target_version_id
	cmd := model.CompareVersionsCommand{JobID: "job-1", DocumentID: "doc-1"}
	body := mustMarshal(t, cmd)

	err := c.handleCompareVersions(context.Background(), body)
	if err != nil {
		t.Fatalf("expected nil (ack), got: %v", err)
	}
	if handler.called {
		t.Fatal("handler should not be called for invalid command")
	}
}

func TestHandleCompareVersions_HandlerError_ReturnsNil(t *testing.T) {
	handler := &mockComparisonHandler{err: errors.New("comparison failed")}
	c := NewConsumer(&mockBroker{}, &mockProcessingHandler{}, handler, testLogger(), testBrokerCfg())

	body := mustMarshal(t, validCompareCmd())

	err := c.handleCompareVersions(context.Background(), body)
	if err != nil {
		t.Fatalf("expected nil (ack despite handler error), got: %v", err)
	}
	if !handler.called {
		t.Fatal("handler should have been called")
	}
}

func TestHandleCompareVersions_AllFieldsCopied(t *testing.T) {
	handler := &mockComparisonHandler{}
	c := NewConsumer(&mockBroker{}, &mockProcessingHandler{}, handler, testLogger(), testBrokerCfg())

	cmd := model.CompareVersionsCommand{
		JobID:           "j1",
		DocumentID:      "d1",
		BaseVersionID:   "v1",
		TargetVersionID: "v2",
		OrgID:           "org-1",
		UserID:          "user-1",
	}
	body := mustMarshal(t, cmd)

	_ = c.handleCompareVersions(context.Background(), body)

	got := handler.lastCmd
	if got.OrgID != cmd.OrgID || got.UserID != cmd.UserID {
		t.Errorf("optional fields not preserved: got %+v", got)
	}
}

// --- Integration: Start → dispatch via broker handler ---

func TestIntegration_StartAndDispatch(t *testing.T) {
	broker := &mockBroker{}
	processing := &mockProcessingHandler{}
	comparison := &mockComparisonHandler{}
	c := NewConsumer(broker, processing, comparison, testLogger(), testBrokerCfg())

	if err := c.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	// Simulate broker delivering a process-document message
	processHandler := broker.calls[0].handler
	body := mustMarshal(t, validProcessCmd())
	if err := processHandler(context.Background(), body); err != nil {
		t.Fatalf("process handler returned: %v", err)
	}
	if !processing.called {
		t.Error("processing handler was not called after Start+dispatch")
	}

	// Simulate broker delivering a compare-versions message
	compareHandler := broker.calls[1].handler
	body = mustMarshal(t, validCompareCmd())
	if err := compareHandler(context.Background(), body); err != nil {
		t.Fatalf("compare handler returned: %v", err)
	}
	if !comparison.called {
		t.Error("comparison handler was not called after Start+dispatch")
	}
}

// --- Context enrichment test ---

func TestHandleProcessDocument_ContextEnrichment(t *testing.T) {
	var capturedCtx context.Context
	handler := &mockProcessingHandler{}
	// Override handler to capture context
	processing := &contextCapturingProcessingHandler{inner: handler}
	c := NewConsumer(&mockBroker{}, processing, &mockComparisonHandler{}, testLogger(), testBrokerCfg())

	cmd := validProcessCmd()
	body := mustMarshal(t, cmd)
	_ = c.handleProcessDocument(context.Background(), body)

	capturedCtx = processing.lastCtx
	jc := observability.JobContextFrom(capturedCtx)
	if jc.JobID != cmd.JobID {
		t.Errorf("context job_id = %q, want %q", jc.JobID, cmd.JobID)
	}
	if jc.DocumentID != cmd.DocumentID {
		t.Errorf("context document_id = %q, want %q", jc.DocumentID, cmd.DocumentID)
	}
}

type contextCapturingProcessingHandler struct {
	mu      sync.Mutex
	inner   *mockProcessingHandler
	lastCtx context.Context
}

func (h *contextCapturingProcessingHandler) HandleProcessDocument(ctx context.Context, cmd model.ProcessDocumentCommand) error {
	h.mu.Lock()
	h.lastCtx = ctx
	h.mu.Unlock()
	return h.inner.HandleProcessDocument(ctx, cmd)
}

type contextCapturingComparisonHandler struct {
	mu      sync.Mutex
	inner   *mockComparisonHandler
	lastCtx context.Context
}

func (h *contextCapturingComparisonHandler) HandleCompareVersions(ctx context.Context, cmd model.CompareVersionsCommand) error {
	h.mu.Lock()
	h.lastCtx = ctx
	h.mu.Unlock()
	return h.inner.HandleCompareVersions(ctx, cmd)
}

// --- Context enrichment test for CompareVersions ---

func TestHandleCompareVersions_ContextEnrichment(t *testing.T) {
	handler := &contextCapturingComparisonHandler{inner: &mockComparisonHandler{}}
	c := NewConsumer(&mockBroker{}, &mockProcessingHandler{}, handler, testLogger(), testBrokerCfg())

	cmd := validCompareCmd()
	body := mustMarshal(t, cmd)
	_ = c.handleCompareVersions(context.Background(), body)

	handler.mu.Lock()
	capturedCtx := handler.lastCtx
	handler.mu.Unlock()

	jc := observability.JobContextFrom(capturedCtx)
	if jc.JobID != cmd.JobID {
		t.Errorf("context job_id = %q, want %q", jc.JobID, cmd.JobID)
	}
	if jc.DocumentID != cmd.DocumentID {
		t.Errorf("context document_id = %q, want %q", jc.DocumentID, cmd.DocumentID)
	}
}

// --- Empty topic panics (W-1) ---

func TestNewConsumer_PanicsOnEmptyTopicProcessDocument(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for empty TopicProcessDocument")
		}
	}()
	cfg := testBrokerCfg()
	cfg.TopicProcessDocument = ""
	NewConsumer(&mockBroker{}, &mockProcessingHandler{}, &mockComparisonHandler{}, testLogger(), cfg)
}

func TestNewConsumer_PanicsOnEmptyTopicCompareVersions(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for empty TopicCompareVersions")
		}
	}()
	cfg := testBrokerCfg()
	cfg.TopicCompareVersions = "  "
	NewConsumer(&mockBroker{}, &mockProcessingHandler{}, &mockComparisonHandler{}, testLogger(), cfg)
}

// --- Idempotent Start (W-2) ---

func TestStart_Idempotent(t *testing.T) {
	broker := &mockBroker{}
	c := NewConsumer(broker, &mockProcessingHandler{}, &mockComparisonHandler{}, testLogger(), testBrokerCfg())

	if err := c.Start(); err != nil {
		t.Fatalf("first Start() error: %v", err)
	}
	if err := c.Start(); err != nil {
		t.Fatalf("second Start() error: %v", err)
	}

	// Should still have only 2 subscriptions (not 4)
	if len(broker.calls) != 2 {
		t.Errorf("expected 2 Subscribe calls (idempotent), got %d", len(broker.calls))
	}
}

// --- Partial subscription failure (S-7) ---

func TestStart_SecondSubscribeFails(t *testing.T) {
	callCount := 0
	broker := &mockBroker{}
	origSubscribe := broker.Subscribe
	_ = origSubscribe // suppress unused

	// Custom broker that fails on second call
	failSecond := &failSecondBroker{}
	c := NewConsumer(failSecond, &mockProcessingHandler{}, &mockComparisonHandler{}, testLogger(), testBrokerCfg())

	err := c.Start()
	if err == nil {
		t.Fatal("expected error from Start when second subscribe fails")
	}
	_ = callCount

	// First subscription was registered
	if len(failSecond.calls) != 2 {
		t.Errorf("expected 2 Subscribe attempts, got %d", len(failSecond.calls))
	}
}

type failSecondBroker struct {
	mu    sync.Mutex
	calls []subscriberCall
	count int
}

func (b *failSecondBroker) Subscribe(topic string, handler func(ctx context.Context, body []byte) error) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.calls = append(b.calls, subscriberCall{topic: topic, handler: handler})
	b.count++
	if b.count == 2 {
		return errors.New("second subscribe failed")
	}
	return nil
}

// --- Nil body (S-8) ---

func TestHandleProcessDocument_NilBody(t *testing.T) {
	handler := &mockProcessingHandler{}
	c := NewConsumer(&mockBroker{}, handler, &mockComparisonHandler{}, testLogger(), testBrokerCfg())

	err := c.handleProcessDocument(context.Background(), nil)
	if err != nil {
		t.Fatalf("expected nil (ack), got: %v", err)
	}
	if handler.called {
		t.Fatal("handler should not be called for nil body")
	}
}

func TestHandleCompareVersions_NilBody(t *testing.T) {
	handler := &mockComparisonHandler{}
	c := NewConsumer(&mockBroker{}, &mockProcessingHandler{}, handler, testLogger(), testBrokerCfg())

	err := c.handleCompareVersions(context.Background(), nil)
	if err != nil {
		t.Fatalf("expected nil (ack), got: %v", err)
	}
	if handler.called {
		t.Fatal("handler should not be called for nil body")
	}
}
