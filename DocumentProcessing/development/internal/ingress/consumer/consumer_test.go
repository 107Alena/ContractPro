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
	mu           sync.Mutex
	calls        []subscriberCall
	subscribeErr error
}

func (m *mockBroker) Subscribe(topic string, handler func(ctx context.Context, body []byte) error) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, subscriberCall{topic: topic, handler: handler})
	return m.subscribeErr
}

var _ BrokerSubscriber = (*mockBroker)(nil)

type mockDispatcher struct {
	mu             sync.Mutex
	processCalled  bool
	compareCalled  bool
	lastProcessCmd model.ProcessDocumentCommand
	lastCompareCmd model.CompareVersionsCommand
	processErr     error
	compareErr     error
}

func (m *mockDispatcher) DispatchProcessDocument(_ context.Context, cmd model.ProcessDocumentCommand) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.processCalled = true
	m.lastProcessCmd = cmd
	return m.processErr
}

func (m *mockDispatcher) DispatchCompareVersions(_ context.Context, cmd model.CompareVersionsCommand) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.compareCalled = true
	m.lastCompareCmd = cmd
	return m.compareErr
}

var _ CommandDispatcher = (*mockDispatcher)(nil)

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
		VersionID:  "ver-789",
		FileURL:    "https://storage.example.com/files/contract.pdf",
		FileName:   "contract.pdf",
		FileSize:   1024,
		MimeType:   "application/pdf",
		OrgID:      "org-42",
		UserID:     "user-99",
	}
}

func validCompareCmd() model.CompareVersionsCommand {
	return model.CompareVersionsCommand{
		JobID:           "job-789",
		DocumentID:      "doc-456",
		BaseVersionID:   "v1",
		TargetVersionID: "v2",
		OrgID:           "org-42",
		UserID:          "user-99",
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
	NewConsumer(nil, &mockDispatcher{}, testLogger(), testBrokerCfg())
}

func TestNewConsumer_PanicsOnNilDispatcher(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for nil dispatcher")
		}
	}()
	NewConsumer(&mockBroker{}, nil, testLogger(), testBrokerCfg())
}

func TestNewConsumer_PanicsOnNilLogger(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for nil logger")
		}
	}()
	NewConsumer(&mockBroker{}, &mockDispatcher{}, nil, testBrokerCfg())
}

// --- Start tests ---

func TestStart_SubscribesToBothTopics(t *testing.T) {
	broker := &mockBroker{}
	cfg := testBrokerCfg()
	c := NewConsumer(broker, &mockDispatcher{}, testLogger(), cfg)

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
	c := NewConsumer(broker, &mockDispatcher{}, testLogger(), testBrokerCfg())

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
	disp := &mockDispatcher{}
	c := NewConsumer(&mockBroker{}, disp, testLogger(), testBrokerCfg())

	cmd := validProcessCmd()
	body := mustMarshal(t, cmd)

	err := c.handleProcessDocument(context.Background(), body)
	if err != nil {
		t.Fatalf("expected nil, got: %v", err)
	}

	if !disp.processCalled {
		t.Fatal("dispatcher was not called")
	}
	if disp.lastProcessCmd.JobID != cmd.JobID {
		t.Errorf("dispatcher received job_id = %q, want %q", disp.lastProcessCmd.JobID, cmd.JobID)
	}
	if disp.lastProcessCmd.DocumentID != cmd.DocumentID {
		t.Errorf("dispatcher received document_id = %q, want %q", disp.lastProcessCmd.DocumentID, cmd.DocumentID)
	}
	if disp.lastProcessCmd.FileURL != cmd.FileURL {
		t.Errorf("dispatcher received file_url = %q, want %q", disp.lastProcessCmd.FileURL, cmd.FileURL)
	}
}

func TestHandleProcessDocument_InvalidJSON(t *testing.T) {
	disp := &mockDispatcher{}
	c := NewConsumer(&mockBroker{}, disp, testLogger(), testBrokerCfg())

	err := c.handleProcessDocument(context.Background(), []byte("not-json"))
	if err != nil {
		t.Fatalf("expected nil (ack), got: %v", err)
	}
	if disp.processCalled {
		t.Fatal("dispatcher should not be called for invalid JSON")
	}
}

func TestHandleProcessDocument_MissingRequiredFields(t *testing.T) {
	disp := &mockDispatcher{}
	c := NewConsumer(&mockBroker{}, disp, testLogger(), testBrokerCfg())

	// Command without required job_id
	cmd := model.ProcessDocumentCommand{DocumentID: "doc-1", VersionID: "ver-1", FileURL: "https://example.com/f.pdf"}
	body := mustMarshal(t, cmd)

	err := c.handleProcessDocument(context.Background(), body)
	if err != nil {
		t.Fatalf("expected nil (ack), got: %v", err)
	}
	if disp.processCalled {
		t.Fatal("dispatcher should not be called for invalid command")
	}
}

func TestHandleProcessDocument_DispatcherError_ReturnsNil(t *testing.T) {
	disp := &mockDispatcher{processErr: errors.New("internal processing error")}
	c := NewConsumer(&mockBroker{}, disp, testLogger(), testBrokerCfg())

	body := mustMarshal(t, validProcessCmd())

	err := c.handleProcessDocument(context.Background(), body)
	if err != nil {
		t.Fatalf("expected nil (ack despite dispatcher error), got: %v", err)
	}
	if !disp.processCalled {
		t.Fatal("dispatcher should have been called")
	}
}

func TestHandleProcessDocument_AllFieldsCopied(t *testing.T) {
	disp := &mockDispatcher{}
	c := NewConsumer(&mockBroker{}, disp, testLogger(), testBrokerCfg())

	cmd := model.ProcessDocumentCommand{
		JobID:      "j1",
		DocumentID: "d1",
		VersionID:  "v1",
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

	got := disp.lastProcessCmd
	if got.VersionID != cmd.VersionID {
		t.Errorf("VersionID not preserved: got %q, want %q", got.VersionID, cmd.VersionID)
	}
	if got.OrgID != cmd.OrgID || got.UserID != cmd.UserID || got.FileName != cmd.FileName ||
		got.FileSize != cmd.FileSize || got.MimeType != cmd.MimeType || got.Checksum != cmd.Checksum {
		t.Errorf("optional fields not preserved: got %+v", got)
	}
}

func TestHandleProcessDocument_EmptyBody(t *testing.T) {
	disp := &mockDispatcher{}
	c := NewConsumer(&mockBroker{}, disp, testLogger(), testBrokerCfg())

	err := c.handleProcessDocument(context.Background(), []byte{})
	if err != nil {
		t.Fatalf("expected nil (ack), got: %v", err)
	}
	if disp.processCalled {
		t.Fatal("dispatcher should not be called for empty body")
	}
}

// --- handleCompareVersions tests ---

func TestHandleCompareVersions_ValidMessage(t *testing.T) {
	disp := &mockDispatcher{}
	c := NewConsumer(&mockBroker{}, disp, testLogger(), testBrokerCfg())

	cmd := validCompareCmd()
	body := mustMarshal(t, cmd)

	err := c.handleCompareVersions(context.Background(), body)
	if err != nil {
		t.Fatalf("expected nil, got: %v", err)
	}

	if !disp.compareCalled {
		t.Fatal("dispatcher was not called")
	}
	if disp.lastCompareCmd.JobID != cmd.JobID {
		t.Errorf("dispatcher received job_id = %q, want %q", disp.lastCompareCmd.JobID, cmd.JobID)
	}
	if disp.lastCompareCmd.BaseVersionID != cmd.BaseVersionID {
		t.Errorf("dispatcher received base_version_id = %q, want %q", disp.lastCompareCmd.BaseVersionID, cmd.BaseVersionID)
	}
	if disp.lastCompareCmd.TargetVersionID != cmd.TargetVersionID {
		t.Errorf("dispatcher received target_version_id = %q, want %q", disp.lastCompareCmd.TargetVersionID, cmd.TargetVersionID)
	}
}

func TestHandleCompareVersions_InvalidJSON(t *testing.T) {
	disp := &mockDispatcher{}
	c := NewConsumer(&mockBroker{}, disp, testLogger(), testBrokerCfg())

	err := c.handleCompareVersions(context.Background(), []byte("{invalid"))
	if err != nil {
		t.Fatalf("expected nil (ack), got: %v", err)
	}
	if disp.compareCalled {
		t.Fatal("dispatcher should not be called for invalid JSON")
	}
}

func TestHandleCompareVersions_MissingRequiredFields(t *testing.T) {
	disp := &mockDispatcher{}
	c := NewConsumer(&mockBroker{}, disp, testLogger(), testBrokerCfg())

	// Command without base_version_id and target_version_id
	cmd := model.CompareVersionsCommand{JobID: "job-1", DocumentID: "doc-1"}
	body := mustMarshal(t, cmd)

	err := c.handleCompareVersions(context.Background(), body)
	if err != nil {
		t.Fatalf("expected nil (ack), got: %v", err)
	}
	if disp.compareCalled {
		t.Fatal("dispatcher should not be called for invalid command")
	}
}

func TestHandleCompareVersions_DispatcherError_ReturnsNil(t *testing.T) {
	disp := &mockDispatcher{compareErr: errors.New("comparison failed")}
	c := NewConsumer(&mockBroker{}, disp, testLogger(), testBrokerCfg())

	body := mustMarshal(t, validCompareCmd())

	err := c.handleCompareVersions(context.Background(), body)
	if err != nil {
		t.Fatalf("expected nil (ack despite dispatcher error), got: %v", err)
	}
	if !disp.compareCalled {
		t.Fatal("dispatcher should have been called")
	}
}

func TestHandleCompareVersions_AllFieldsCopied(t *testing.T) {
	disp := &mockDispatcher{}
	c := NewConsumer(&mockBroker{}, disp, testLogger(), testBrokerCfg())

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

	got := disp.lastCompareCmd
	if got.OrgID != cmd.OrgID || got.UserID != cmd.UserID {
		t.Errorf("optional fields not preserved: got %+v", got)
	}
}

// --- Integration: Start -> dispatch via broker handler ---

func TestIntegration_StartAndDispatch(t *testing.T) {
	broker := &mockBroker{}
	disp := &mockDispatcher{}
	c := NewConsumer(broker, disp, testLogger(), testBrokerCfg())

	if err := c.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	// Simulate broker delivering a process-document message
	processHandler := broker.calls[0].handler
	body := mustMarshal(t, validProcessCmd())
	if err := processHandler(context.Background(), body); err != nil {
		t.Fatalf("process handler returned: %v", err)
	}
	if !disp.processCalled {
		t.Error("dispatcher was not called for process document after Start+dispatch")
	}

	// Simulate broker delivering a compare-versions message
	compareHandler := broker.calls[1].handler
	body = mustMarshal(t, validCompareCmd())
	if err := compareHandler(context.Background(), body); err != nil {
		t.Fatalf("compare handler returned: %v", err)
	}
	if !disp.compareCalled {
		t.Error("dispatcher was not called for compare versions after Start+dispatch")
	}
}

// --- Context enrichment tests ---

type contextCapturingDispatcher struct {
	mu             sync.Mutex
	lastProcessCtx context.Context
	lastCompareCtx context.Context
	inner          *mockDispatcher
}

func (d *contextCapturingDispatcher) DispatchProcessDocument(ctx context.Context, cmd model.ProcessDocumentCommand) error {
	d.mu.Lock()
	d.lastProcessCtx = ctx
	d.mu.Unlock()
	return d.inner.DispatchProcessDocument(ctx, cmd)
}

func (d *contextCapturingDispatcher) DispatchCompareVersions(ctx context.Context, cmd model.CompareVersionsCommand) error {
	d.mu.Lock()
	d.lastCompareCtx = ctx
	d.mu.Unlock()
	return d.inner.DispatchCompareVersions(ctx, cmd)
}

var _ CommandDispatcher = (*contextCapturingDispatcher)(nil)

func TestHandleProcessDocument_ContextEnrichment(t *testing.T) {
	capturing := &contextCapturingDispatcher{inner: &mockDispatcher{}}
	c := NewConsumer(&mockBroker{}, capturing, testLogger(), testBrokerCfg())

	cmd := validProcessCmd()
	body := mustMarshal(t, cmd)
	_ = c.handleProcessDocument(context.Background(), body)

	capturing.mu.Lock()
	capturedCtx := capturing.lastProcessCtx
	capturing.mu.Unlock()

	jc := observability.JobContextFrom(capturedCtx)
	if jc.JobID != cmd.JobID {
		t.Errorf("context job_id = %q, want %q", jc.JobID, cmd.JobID)
	}
	if jc.DocumentID != cmd.DocumentID {
		t.Errorf("context document_id = %q, want %q", jc.DocumentID, cmd.DocumentID)
	}
	if jc.CorrelationID != cmd.JobID {
		t.Errorf("context correlation_id = %q, want %q", jc.CorrelationID, cmd.JobID)
	}
	if jc.OrgID != cmd.OrgID {
		t.Errorf("context org_id = %q, want %q", jc.OrgID, cmd.OrgID)
	}
	if jc.UserID != cmd.UserID {
		t.Errorf("context user_id = %q, want %q", jc.UserID, cmd.UserID)
	}
}

func TestHandleCompareVersions_ContextEnrichment(t *testing.T) {
	capturing := &contextCapturingDispatcher{inner: &mockDispatcher{}}
	c := NewConsumer(&mockBroker{}, capturing, testLogger(), testBrokerCfg())

	cmd := validCompareCmd()
	body := mustMarshal(t, cmd)
	_ = c.handleCompareVersions(context.Background(), body)

	capturing.mu.Lock()
	capturedCtx := capturing.lastCompareCtx
	capturing.mu.Unlock()

	jc := observability.JobContextFrom(capturedCtx)
	if jc.JobID != cmd.JobID {
		t.Errorf("context job_id = %q, want %q", jc.JobID, cmd.JobID)
	}
	if jc.DocumentID != cmd.DocumentID {
		t.Errorf("context document_id = %q, want %q", jc.DocumentID, cmd.DocumentID)
	}
	if jc.CorrelationID != cmd.JobID {
		t.Errorf("context correlation_id = %q, want %q", jc.CorrelationID, cmd.JobID)
	}
	if jc.OrgID != cmd.OrgID {
		t.Errorf("context org_id = %q, want %q", jc.OrgID, cmd.OrgID)
	}
	if jc.UserID != cmd.UserID {
		t.Errorf("context user_id = %q, want %q", jc.UserID, cmd.UserID)
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
	NewConsumer(&mockBroker{}, &mockDispatcher{}, testLogger(), cfg)
}

func TestNewConsumer_PanicsOnEmptyTopicCompareVersions(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for empty TopicCompareVersions")
		}
	}()
	cfg := testBrokerCfg()
	cfg.TopicCompareVersions = "  "
	NewConsumer(&mockBroker{}, &mockDispatcher{}, testLogger(), cfg)
}

// --- Idempotent Start (W-2) ---

func TestStart_Idempotent(t *testing.T) {
	broker := &mockBroker{}
	c := NewConsumer(broker, &mockDispatcher{}, testLogger(), testBrokerCfg())

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
	// Custom broker that fails on second call
	failSecond := &failSecondBroker{}
	c := NewConsumer(failSecond, &mockDispatcher{}, testLogger(), testBrokerCfg())

	err := c.Start()
	if err == nil {
		t.Fatal("expected error from Start when second subscribe fails")
	}

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
	disp := &mockDispatcher{}
	c := NewConsumer(&mockBroker{}, disp, testLogger(), testBrokerCfg())

	err := c.handleProcessDocument(context.Background(), nil)
	if err != nil {
		t.Fatalf("expected nil (ack), got: %v", err)
	}
	if disp.processCalled {
		t.Fatal("dispatcher should not be called for nil body")
	}
}

func TestHandleCompareVersions_NilBody(t *testing.T) {
	disp := &mockDispatcher{}
	c := NewConsumer(&mockBroker{}, disp, testLogger(), testBrokerCfg())

	err := c.handleCompareVersions(context.Background(), nil)
	if err != nil {
		t.Fatalf("expected nil (ack), got: %v", err)
	}
	if disp.compareCalled {
		t.Fatal("dispatcher should not be called for nil body")
	}
}
