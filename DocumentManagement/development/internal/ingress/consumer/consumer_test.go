package consumer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"contractpro/document-management/internal/domain/model"
	"contractpro/document-management/internal/domain/port"
	"contractpro/document-management/internal/ingress/idempotency"
)

// ---------------------------------------------------------------------------
// Mock implementations
// ---------------------------------------------------------------------------

type subscription struct {
	topic   string
	handler func(ctx context.Context, body []byte) error
}

type mockBroker struct {
	mu            sync.Mutex
	subscriptions []subscription
	failOn        string // topic name that should fail
}

func (m *mockBroker) Subscribe(topic string, handler func(ctx context.Context, body []byte) error) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failOn == topic {
		return fmt.Errorf("subscribe failed for %s", topic)
	}
	m.subscriptions = append(m.subscriptions, subscription{topic: topic, handler: handler})
	return nil
}

func (m *mockBroker) handlerFor(topic string) func(ctx context.Context, body []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, s := range m.subscriptions {
		if s.topic == topic {
			return s.handler
		}
	}
	return nil
}

type mockIdempotency struct {
	checkResult idempotency.CheckResult
	checkErr    error
	markErr     error
	cleanupErr  error

	mu           sync.Mutex
	checkCalls   int
	markCalls    int
	cleanupCalls int
	lastKey      string
}

func (m *mockIdempotency) Check(_ context.Context, key string, _ string, _ idempotency.FallbackChecker) (idempotency.CheckResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.checkCalls++
	m.lastKey = key
	return m.checkResult, m.checkErr
}

func (m *mockIdempotency) MarkCompleted(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.markCalls++
	m.lastKey = key
	return m.markErr
}

func (m *mockIdempotency) Cleanup(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cleanupCalls++
	m.lastKey = key
	return m.cleanupErr
}

type mockIngestion struct {
	mu             sync.Mutex
	dpCalls        int
	licCalls       int
	reCalls        int
	lastDPEvent    *model.DocumentProcessingArtifactsReady
	lastLICEvent   *model.LegalAnalysisArtifactsReady
	lastREEvent    *model.ReportsArtifactsReady
	dpErr          error
	licErr         error
	reErr          error
}

func (m *mockIngestion) HandleDPArtifacts(_ context.Context, event model.DocumentProcessingArtifactsReady) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.dpCalls++
	m.lastDPEvent = &event
	return m.dpErr
}

func (m *mockIngestion) HandleLICArtifacts(_ context.Context, event model.LegalAnalysisArtifactsReady) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.licCalls++
	m.lastLICEvent = &event
	return m.licErr
}

func (m *mockIngestion) HandleREArtifacts(_ context.Context, event model.ReportsArtifactsReady) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.reCalls++
	m.lastREEvent = &event
	return m.reErr
}

type mockQuery struct {
	mu               sync.Mutex
	getTreeCalls     int
	getArtifactsCalls int
	lastTreeEvent    *model.GetSemanticTreeRequest
	lastArtifactsEvent *model.GetArtifactsRequest
	getTreeErr       error
	getArtifactsErr  error
}

func (m *mockQuery) HandleGetSemanticTree(_ context.Context, event model.GetSemanticTreeRequest) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.getTreeCalls++
	m.lastTreeEvent = &event
	return m.getTreeErr
}

func (m *mockQuery) HandleGetArtifacts(_ context.Context, event model.GetArtifactsRequest) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.getArtifactsCalls++
	m.lastArtifactsEvent = &event
	return m.getArtifactsErr
}

func (m *mockQuery) GetArtifact(_ context.Context, _ port.GetArtifactParams) (*port.ArtifactContent, error) {
	return nil, nil
}

func (m *mockQuery) ListArtifacts(_ context.Context, _, _, _ string) ([]*model.ArtifactDescriptor, error) {
	return nil, nil
}

type mockDiff struct {
	mu        sync.Mutex
	calls     int
	lastEvent *model.DocumentVersionDiffReady
	err       error
}

func (m *mockDiff) HandleDiffReady(_ context.Context, event model.DocumentVersionDiffReady) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls++
	m.lastEvent = &event
	return m.err
}

func (m *mockDiff) GetDiff(_ context.Context, _ port.GetDiffParams) (*model.VersionDiffReference, []byte, error) {
	return nil, nil, nil
}

type mockArtifactRepo struct{}

func (m *mockArtifactRepo) Insert(_ context.Context, _ *model.ArtifactDescriptor) error { return nil }
func (m *mockArtifactRepo) FindByVersionAndType(_ context.Context, _, _, _ string, _ model.ArtifactType) (*model.ArtifactDescriptor, error) {
	return nil, nil
}
func (m *mockArtifactRepo) ListByVersion(_ context.Context, _, _, _ string) ([]*model.ArtifactDescriptor, error) {
	return nil, nil
}
func (m *mockArtifactRepo) ListByVersionAndTypes(_ context.Context, _, _, _ string, _ []model.ArtifactType) ([]*model.ArtifactDescriptor, error) {
	return nil, nil
}
func (m *mockArtifactRepo) DeleteByVersion(_ context.Context, _, _, _ string) error { return nil }

type mockDiffRepo struct{}

func (m *mockDiffRepo) Insert(_ context.Context, _ *model.VersionDiffReference) error { return nil }
func (m *mockDiffRepo) FindByVersionPair(_ context.Context, _, _, _, _ string) (*model.VersionDiffReference, error) {
	return nil, port.NewDiffNotFoundError("", "")
}
func (m *mockDiffRepo) ListByDocument(_ context.Context, _, _ string) ([]*model.VersionDiffReference, error) {
	return nil, nil
}
func (m *mockDiffRepo) DeleteByDocument(_ context.Context, _, _ string) error { return nil }

type logEntry struct {
	level string
	msg   string
	args  []any
}

type mockLogger struct {
	mu      sync.Mutex
	entries []logEntry
}

func (m *mockLogger) Info(msg string, args ...any) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.entries = append(m.entries, logEntry{level: "info", msg: msg, args: args})
}

func (m *mockLogger) Warn(msg string, args ...any) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.entries = append(m.entries, logEntry{level: "warn", msg: msg, args: args})
}

func (m *mockLogger) Error(msg string, args ...any) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.entries = append(m.entries, logEntry{level: "error", msg: msg, args: args})
}

func (m *mockLogger) hasLevel(level string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, e := range m.entries {
		if e.level == level {
			return true
		}
	}
	return false
}

func (m *mockLogger) hasMessage(msg string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, e := range m.entries {
		if strings.Contains(e.msg, msg) {
			return true
		}
	}
	return false
}

type metricsEntry struct {
	topic  string
	status string
}

type mockMetrics struct {
	mu        sync.Mutex
	received  []string
	processed []metricsEntry
}

func (m *mockMetrics) IncEventsReceived(topic string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.received = append(m.received, topic)
}

func (m *mockMetrics) IncEventsProcessed(topic string, status string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.processed = append(m.processed, metricsEntry{topic: topic, status: status})
}

func (m *mockMetrics) hasProcessed(topic, status string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, e := range m.processed {
		if e.topic == topic && e.status == status {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

func defaultTopics() TopicConfig {
	return TopicConfig{
		DPArtifactsReady:    model.TopicDPArtifactsProcessingReady,
		DPSemanticTreeReq:   model.TopicDPRequestsSemanticTree,
		DPDiffReady:         model.TopicDPArtifactsDiffReady,
		LICArtifactsReady:   model.TopicLICArtifactsAnalysisReady,
		LICRequestArtifacts: model.TopicLICRequestsArtifacts,
		REArtifactsReady:    model.TopicREArtifactsReportsReady,
		RERequestArtifacts:  model.TopicRERequestsArtifacts,
	}
}

type testDeps struct {
	broker       *mockBroker
	idempotency  *mockIdempotency
	logger       *mockLogger
	metrics      *mockMetrics
	ingestion    *mockIngestion
	query        *mockQuery
	diff         *mockDiff
	artifactRepo *mockArtifactRepo
	diffRepo     *mockDiffRepo
}

func newTestDeps() *testDeps {
	return &testDeps{
		broker:       &mockBroker{},
		idempotency:  &mockIdempotency{checkResult: idempotency.ResultProcess},
		logger:       &mockLogger{},
		metrics:      &mockMetrics{},
		ingestion:    &mockIngestion{},
		query:        &mockQuery{},
		diff:         &mockDiff{},
		artifactRepo: &mockArtifactRepo{},
		diffRepo:     &mockDiffRepo{},
	}
}

func (d *testDeps) newConsumer() *EventConsumer {
	return NewEventConsumer(
		d.broker, d.idempotency, d.logger, d.metrics,
		d.ingestion, d.query, d.diff,
		d.artifactRepo, d.diffRepo,
		defaultTopics(),
	)
}

func mustMarshal(t *testing.T, v any) []byte {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}
	return data
}

func validDPArtifactsEvent() model.DocumentProcessingArtifactsReady {
	return model.DocumentProcessingArtifactsReady{
		EventMeta: model.EventMeta{
			CorrelationID: "corr-123",
			Timestamp:     time.Now(),
		},
		JobID:        "job-1",
		DocumentID:   "doc-1",
		VersionID:    "ver-1",
		OrgID:        "org-1",
		OCRRaw:       json.RawMessage(`{"data":"ocr"}`),
		Text:         json.RawMessage(`{"data":"text"}`),
		Structure:    json.RawMessage(`{"data":"structure"}`),
		SemanticTree: json.RawMessage(`{"data":"tree"}`),
	}
}

func validGetSemanticTreeEvent() model.GetSemanticTreeRequest {
	return model.GetSemanticTreeRequest{
		EventMeta: model.EventMeta{
			CorrelationID: "corr-124",
			Timestamp:     time.Now(),
		},
		JobID:      "job-2",
		DocumentID: "doc-2",
		VersionID:  "ver-2",
		OrgID:      "org-1",
	}
}

func validDiffReadyEvent() model.DocumentVersionDiffReady {
	return model.DocumentVersionDiffReady{
		EventMeta: model.EventMeta{
			CorrelationID: "corr-125",
			Timestamp:     time.Now(),
		},
		JobID:           "job-3",
		DocumentID:      "doc-3",
		BaseVersionID:   "ver-3a",
		TargetVersionID: "ver-3b",
		OrgID:           "org-1",
		TextDiffs:       json.RawMessage(`[]`),
		StructuralDiffs: json.RawMessage(`[]`),
	}
}

func validLICArtifactsEvent() model.LegalAnalysisArtifactsReady {
	return model.LegalAnalysisArtifactsReady{
		EventMeta: model.EventMeta{
			CorrelationID: "corr-126",
			Timestamp:     time.Now(),
		},
		JobID:                "job-4",
		DocumentID:           "doc-4",
		VersionID:            "ver-4",
		OrgID:                "org-1",
		ClassificationResult: json.RawMessage(`{}`),
		KeyParameters:        json.RawMessage(`{}`),
		RiskAnalysis:         json.RawMessage(`{}`),
		RiskProfile:          json.RawMessage(`{}`),
		Recommendations:      json.RawMessage(`{}`),
		Summary:              json.RawMessage(`{}`),
		DetailedReport:       json.RawMessage(`{}`),
		AggregateScore:       json.RawMessage(`{}`),
	}
}

func validGetArtifactsEvent() model.GetArtifactsRequest {
	return model.GetArtifactsRequest{
		EventMeta: model.EventMeta{
			CorrelationID: "corr-127",
			Timestamp:     time.Now(),
		},
		JobID:         "job-5",
		DocumentID:    "doc-5",
		VersionID:     "ver-5",
		OrgID:         "org-1",
		ArtifactTypes: []model.ArtifactType{model.ArtifactTypeSemanticTree},
	}
}

func validREArtifactsEvent() model.ReportsArtifactsReady {
	return model.ReportsArtifactsReady{
		EventMeta: model.EventMeta{
			CorrelationID: "corr-128",
			Timestamp:     time.Now(),
		},
		JobID:      "job-6",
		DocumentID: "doc-6",
		VersionID:  "ver-6",
		OrgID:      "org-1",
		ExportPDF: &model.BlobReference{
			StorageKey:  "key-pdf",
			FileName:    "report.pdf",
			SizeBytes:   1024,
			ContentHash: "abc123",
		},
	}
}

// ---------------------------------------------------------------------------
// Tests: Constructor
// ---------------------------------------------------------------------------

func TestNewEventConsumer_PanicOnNilDeps(t *testing.T) {
	d := newTestDeps()
	topics := defaultTopics()

	cases := []struct {
		name    string
		factory func()
	}{
		{"nil broker", func() {
			NewEventConsumer(nil, d.idempotency, d.logger, d.metrics, d.ingestion, d.query, d.diff, d.artifactRepo, d.diffRepo, topics)
		}},
		{"nil idempotency", func() {
			NewEventConsumer(d.broker, nil, d.logger, d.metrics, d.ingestion, d.query, d.diff, d.artifactRepo, d.diffRepo, topics)
		}},
		{"nil logger", func() {
			NewEventConsumer(d.broker, d.idempotency, nil, d.metrics, d.ingestion, d.query, d.diff, d.artifactRepo, d.diffRepo, topics)
		}},
		{"nil metrics", func() {
			NewEventConsumer(d.broker, d.idempotency, d.logger, nil, d.ingestion, d.query, d.diff, d.artifactRepo, d.diffRepo, topics)
		}},
		{"nil ingestion", func() {
			NewEventConsumer(d.broker, d.idempotency, d.logger, d.metrics, nil, d.query, d.diff, d.artifactRepo, d.diffRepo, topics)
		}},
		{"nil query", func() {
			NewEventConsumer(d.broker, d.idempotency, d.logger, d.metrics, d.ingestion, nil, d.diff, d.artifactRepo, d.diffRepo, topics)
		}},
		{"nil diff handler", func() {
			NewEventConsumer(d.broker, d.idempotency, d.logger, d.metrics, d.ingestion, d.query, nil, d.artifactRepo, d.diffRepo, topics)
		}},
		{"nil artifact repo", func() {
			NewEventConsumer(d.broker, d.idempotency, d.logger, d.metrics, d.ingestion, d.query, d.diff, nil, d.diffRepo, topics)
		}},
		{"nil diff repo", func() {
			NewEventConsumer(d.broker, d.idempotency, d.logger, d.metrics, d.ingestion, d.query, d.diff, d.artifactRepo, nil, topics)
		}},
		{"empty topic", func() {
			topics := defaultTopics()
			topics.DPArtifactsReady = ""
			NewEventConsumer(d.broker, d.idempotency, d.logger, d.metrics, d.ingestion, d.query, d.diff, d.artifactRepo, d.diffRepo, topics)
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r == nil {
					t.Errorf("expected panic, got none")
				}
			}()
			tc.factory()
		})
	}
}

// ---------------------------------------------------------------------------
// Tests: Start
// ---------------------------------------------------------------------------

func TestStart_SubscribesAllTopics(t *testing.T) {
	d := newTestDeps()
	c := d.newConsumer()

	if err := c.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	d.broker.mu.Lock()
	count := len(d.broker.subscriptions)
	d.broker.mu.Unlock()

	if count != 7 {
		t.Errorf("expected 7 subscriptions, got %d", count)
	}
}

func TestStart_Idempotent(t *testing.T) {
	d := newTestDeps()
	c := d.newConsumer()

	if err := c.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	if err := c.Start(); err != nil {
		t.Fatalf("second Start() error: %v", err)
	}

	d.broker.mu.Lock()
	count := len(d.broker.subscriptions)
	d.broker.mu.Unlock()

	if count != 7 {
		t.Errorf("expected 7 subscriptions (not 14), got %d", count)
	}
}

func TestStart_PartialFailure(t *testing.T) {
	d := newTestDeps()
	d.broker.failOn = model.TopicDPArtifactsDiffReady
	c := d.newConsumer()

	err := c.Start()
	if err == nil {
		t.Fatal("expected error from Start()")
	}
	if !strings.Contains(err.Error(), model.TopicDPArtifactsDiffReady) {
		t.Errorf("error should mention failed topic, got: %s", err)
	}
}

// ---------------------------------------------------------------------------
// Tests: DP Artifacts (happy path)
// ---------------------------------------------------------------------------

func TestHandleDPArtifacts_HappyPath(t *testing.T) {
	d := newTestDeps()
	c := d.newConsumer()
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}

	event := validDPArtifactsEvent()
	body := mustMarshal(t, event)
	handler := d.broker.handlerFor(model.TopicDPArtifactsProcessingReady)

	err := handler(context.Background(), body)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	d.ingestion.mu.Lock()
	defer d.ingestion.mu.Unlock()
	if d.ingestion.dpCalls != 1 {
		t.Errorf("expected 1 DP call, got %d", d.ingestion.dpCalls)
	}
	if d.ingestion.lastDPEvent.JobID != "job-1" {
		t.Errorf("expected job_id=job-1, got %s", d.ingestion.lastDPEvent.JobID)
	}

	if !d.metrics.hasProcessed(model.TopicDPArtifactsProcessingReady, "success") {
		t.Error("expected success metric")
	}

	d.idempotency.mu.Lock()
	defer d.idempotency.mu.Unlock()
	if d.idempotency.markCalls != 1 {
		t.Errorf("expected 1 markCompleted call, got %d", d.idempotency.markCalls)
	}
}

// ---------------------------------------------------------------------------
// Tests: GetSemanticTree (happy path)
// ---------------------------------------------------------------------------

func TestHandleGetSemanticTree_HappyPath(t *testing.T) {
	d := newTestDeps()
	c := d.newConsumer()
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}

	event := validGetSemanticTreeEvent()
	body := mustMarshal(t, event)
	handler := d.broker.handlerFor(model.TopicDPRequestsSemanticTree)

	err := handler(context.Background(), body)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	d.query.mu.Lock()
	defer d.query.mu.Unlock()
	if d.query.getTreeCalls != 1 {
		t.Errorf("expected 1 getTree call, got %d", d.query.getTreeCalls)
	}
	if d.query.lastTreeEvent.VersionID != "ver-2" {
		t.Errorf("expected version_id=ver-2, got %s", d.query.lastTreeEvent.VersionID)
	}
}

func TestHandleGetSemanticTree_MissingVersionID(t *testing.T) {
	d := newTestDeps()
	c := d.newConsumer()
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}

	event := validGetSemanticTreeEvent()
	event.VersionID = ""
	body := mustMarshal(t, event)
	handler := d.broker.handlerFor(model.TopicDPRequestsSemanticTree)

	err := handler(context.Background(), body)
	if err != nil {
		t.Fatalf("expected nil error (always nil), got: %v", err)
	}

	d.query.mu.Lock()
	defer d.query.mu.Unlock()
	if d.query.getTreeCalls != 0 {
		t.Error("handler should not be called for invalid event")
	}

	if !d.metrics.hasProcessed(model.TopicDPRequestsSemanticTree, "invalid") {
		t.Error("expected invalid metric")
	}
}

// ---------------------------------------------------------------------------
// Tests: Diff Ready (happy path + missing fields)
// ---------------------------------------------------------------------------

func TestHandleDiffReady_HappyPath(t *testing.T) {
	d := newTestDeps()
	c := d.newConsumer()
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}

	event := validDiffReadyEvent()
	body := mustMarshal(t, event)
	handler := d.broker.handlerFor(model.TopicDPArtifactsDiffReady)

	err := handler(context.Background(), body)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	d.diff.mu.Lock()
	defer d.diff.mu.Unlock()
	if d.diff.calls != 1 {
		t.Errorf("expected 1 diff call, got %d", d.diff.calls)
	}
}

func TestHandleDiffReady_MissingBaseVersionID(t *testing.T) {
	d := newTestDeps()
	c := d.newConsumer()
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}

	event := validDiffReadyEvent()
	event.BaseVersionID = ""
	body := mustMarshal(t, event)
	handler := d.broker.handlerFor(model.TopicDPArtifactsDiffReady)

	err := handler(context.Background(), body)
	if err != nil {
		t.Fatalf("expected nil, got: %v", err)
	}

	d.diff.mu.Lock()
	defer d.diff.mu.Unlock()
	if d.diff.calls != 0 {
		t.Error("handler should not be called for invalid event")
	}

	if !d.metrics.hasProcessed(model.TopicDPArtifactsDiffReady, "invalid") {
		t.Error("expected invalid metric")
	}
}

func TestHandleDiffReady_MissingTargetVersionID(t *testing.T) {
	d := newTestDeps()
	c := d.newConsumer()
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}

	event := validDiffReadyEvent()
	event.TargetVersionID = ""
	body := mustMarshal(t, event)
	handler := d.broker.handlerFor(model.TopicDPArtifactsDiffReady)

	err := handler(context.Background(), body)
	if err != nil {
		t.Fatalf("expected nil, got: %v", err)
	}

	d.diff.mu.Lock()
	defer d.diff.mu.Unlock()
	if d.diff.calls != 0 {
		t.Error("handler should not be called for invalid event")
	}
}

// ---------------------------------------------------------------------------
// Tests: LIC Artifacts (happy path)
// ---------------------------------------------------------------------------

func TestHandleLICArtifacts_HappyPath(t *testing.T) {
	d := newTestDeps()
	c := d.newConsumer()
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}

	event := validLICArtifactsEvent()
	body := mustMarshal(t, event)
	handler := d.broker.handlerFor(model.TopicLICArtifactsAnalysisReady)

	err := handler(context.Background(), body)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	d.ingestion.mu.Lock()
	defer d.ingestion.mu.Unlock()
	if d.ingestion.licCalls != 1 {
		t.Errorf("expected 1 LIC call, got %d", d.ingestion.licCalls)
	}
}

// ---------------------------------------------------------------------------
// Tests: LIC Request Artifacts (happy path)
// ---------------------------------------------------------------------------

func TestHandleLICRequestArtifacts_HappyPath(t *testing.T) {
	d := newTestDeps()
	c := d.newConsumer()
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}

	event := validGetArtifactsEvent()
	body := mustMarshal(t, event)
	handler := d.broker.handlerFor(model.TopicLICRequestsArtifacts)

	err := handler(context.Background(), body)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	d.query.mu.Lock()
	defer d.query.mu.Unlock()
	if d.query.getArtifactsCalls != 1 {
		t.Errorf("expected 1 getArtifacts call, got %d", d.query.getArtifactsCalls)
	}
}

func TestHandleLICRequestArtifacts_MissingVersionID(t *testing.T) {
	d := newTestDeps()
	c := d.newConsumer()
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}

	event := validGetArtifactsEvent()
	event.VersionID = ""
	body := mustMarshal(t, event)
	handler := d.broker.handlerFor(model.TopicLICRequestsArtifacts)

	err := handler(context.Background(), body)
	if err != nil {
		t.Fatalf("expected nil, got: %v", err)
	}

	d.query.mu.Lock()
	defer d.query.mu.Unlock()
	if d.query.getArtifactsCalls != 0 {
		t.Error("handler should not be called for invalid event")
	}
}

// ---------------------------------------------------------------------------
// Tests: RE Artifacts (happy path)
// ---------------------------------------------------------------------------

func TestHandleREArtifacts_HappyPath(t *testing.T) {
	d := newTestDeps()
	c := d.newConsumer()
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}

	event := validREArtifactsEvent()
	body := mustMarshal(t, event)
	handler := d.broker.handlerFor(model.TopicREArtifactsReportsReady)

	err := handler(context.Background(), body)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	d.ingestion.mu.Lock()
	defer d.ingestion.mu.Unlock()
	if d.ingestion.reCalls != 1 {
		t.Errorf("expected 1 RE call, got %d", d.ingestion.reCalls)
	}
}

func TestHandleRERequestArtifacts_MissingVersionID(t *testing.T) {
	d := newTestDeps()
	c := d.newConsumer()
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}

	event := validGetArtifactsEvent()
	event.VersionID = ""
	body := mustMarshal(t, event)
	handler := d.broker.handlerFor(model.TopicRERequestsArtifacts)

	err := handler(context.Background(), body)
	if err != nil {
		t.Fatalf("expected nil, got: %v", err)
	}

	d.query.mu.Lock()
	defer d.query.mu.Unlock()
	if d.query.getArtifactsCalls != 0 {
		t.Error("handler should not be called for invalid event")
	}

	if !d.metrics.hasProcessed(model.TopicRERequestsArtifacts, "invalid") {
		t.Error("expected invalid metric")
	}
}

// ---------------------------------------------------------------------------
// Tests: RE Request Artifacts (happy path)
// ---------------------------------------------------------------------------

func TestHandleRERequestArtifacts_HappyPath(t *testing.T) {
	d := newTestDeps()
	c := d.newConsumer()
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}

	event := validGetArtifactsEvent()
	body := mustMarshal(t, event)
	handler := d.broker.handlerFor(model.TopicRERequestsArtifacts)

	err := handler(context.Background(), body)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	d.query.mu.Lock()
	defer d.query.mu.Unlock()
	if d.query.getArtifactsCalls != 1 {
		t.Errorf("expected 1 getArtifacts call, got %d", d.query.getArtifactsCalls)
	}
}

// ---------------------------------------------------------------------------
// Tests: Invalid JSON
// ---------------------------------------------------------------------------

func TestHandleDPArtifacts_InvalidJSON(t *testing.T) {
	d := newTestDeps()
	c := d.newConsumer()
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}

	handler := d.broker.handlerFor(model.TopicDPArtifactsProcessingReady)
	err := handler(context.Background(), []byte(`{invalid json`))
	if err != nil {
		t.Fatalf("expected nil (always nil), got: %v", err)
	}

	d.ingestion.mu.Lock()
	defer d.ingestion.mu.Unlock()
	if d.ingestion.dpCalls != 0 {
		t.Error("handler should not be called for invalid JSON")
	}

	if !d.logger.hasMessage("failed to unmarshal") {
		t.Error("expected unmarshal error log")
	}

	if !d.metrics.hasProcessed(model.TopicDPArtifactsProcessingReady, "invalid") {
		t.Error("expected invalid metric")
	}
}

func TestHandleGetArtifacts_InvalidJSON(t *testing.T) {
	d := newTestDeps()
	c := d.newConsumer()
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}

	handler := d.broker.handlerFor(model.TopicLICRequestsArtifacts)
	err := handler(context.Background(), []byte(`not json`))
	if err != nil {
		t.Fatalf("expected nil, got: %v", err)
	}

	if !d.metrics.hasProcessed(model.TopicLICRequestsArtifacts, "invalid") {
		t.Error("expected invalid metric")
	}
}

// ---------------------------------------------------------------------------
// Tests: Missing required fields
// ---------------------------------------------------------------------------

func TestHandleDPArtifacts_MissingFields(t *testing.T) {
	cases := []struct {
		name   string
		modify func(*model.DocumentProcessingArtifactsReady)
	}{
		{"missing correlation_id", func(e *model.DocumentProcessingArtifactsReady) { e.CorrelationID = "" }},
		{"missing job_id", func(e *model.DocumentProcessingArtifactsReady) { e.JobID = "" }},
		{"missing document_id", func(e *model.DocumentProcessingArtifactsReady) { e.DocumentID = "" }},
		{"missing timestamp", func(e *model.DocumentProcessingArtifactsReady) { e.Timestamp = time.Time{} }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := newTestDeps()
			c := d.newConsumer()
			if err := c.Start(); err != nil {
				t.Fatal(err)
			}

			event := validDPArtifactsEvent()
			tc.modify(&event)
			body := mustMarshal(t, event)
			handler := d.broker.handlerFor(model.TopicDPArtifactsProcessingReady)

			err := handler(context.Background(), body)
			if err != nil {
				t.Fatalf("expected nil, got: %v", err)
			}

			if !d.logger.hasMessage("event validation failed") {
				t.Error("expected validation failure log")
			}

			if !d.metrics.hasProcessed(model.TopicDPArtifactsProcessingReady, "invalid") {
				t.Error("expected invalid metric")
			}
		})
	}
}

func TestHandleDPArtifacts_MultipleFieldsMissing(t *testing.T) {
	d := newTestDeps()
	c := d.newConsumer()
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}

	event := model.DocumentProcessingArtifactsReady{
		// All required fields empty
		OCRRaw: json.RawMessage(`{}`),
	}
	body := mustMarshal(t, event)
	handler := d.broker.handlerFor(model.TopicDPArtifactsProcessingReady)

	err := handler(context.Background(), body)
	if err != nil {
		t.Fatalf("expected nil, got: %v", err)
	}

	if !d.metrics.hasProcessed(model.TopicDPArtifactsProcessingReady, "invalid") {
		t.Error("expected invalid metric")
	}
}

// ---------------------------------------------------------------------------
// Tests: Idempotency skip
// ---------------------------------------------------------------------------

func TestHandleDPArtifacts_IdempotencySkip(t *testing.T) {
	d := newTestDeps()
	d.idempotency.checkResult = idempotency.ResultSkip
	c := d.newConsumer()
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}

	event := validDPArtifactsEvent()
	body := mustMarshal(t, event)
	handler := d.broker.handlerFor(model.TopicDPArtifactsProcessingReady)

	err := handler(context.Background(), body)
	if err != nil {
		t.Fatalf("expected nil, got: %v", err)
	}

	d.ingestion.mu.Lock()
	defer d.ingestion.mu.Unlock()
	if d.ingestion.dpCalls != 0 {
		t.Error("handler should not be called on idempotency skip")
	}

	if !d.metrics.hasProcessed(model.TopicDPArtifactsProcessingReady, "skipped") {
		t.Error("expected skipped metric")
	}

	if !d.logger.hasMessage("duplicate event") {
		t.Error("expected duplicate event log")
	}
}

// ---------------------------------------------------------------------------
// Tests: Idempotency reprocess
// ---------------------------------------------------------------------------

func TestHandleDPArtifacts_IdempotencyReprocess(t *testing.T) {
	d := newTestDeps()
	d.idempotency.checkResult = idempotency.ResultReprocess
	c := d.newConsumer()
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}

	event := validDPArtifactsEvent()
	body := mustMarshal(t, event)
	handler := d.broker.handlerFor(model.TopicDPArtifactsProcessingReady)

	err := handler(context.Background(), body)
	if err != nil {
		t.Fatalf("expected nil, got: %v", err)
	}

	d.ingestion.mu.Lock()
	defer d.ingestion.mu.Unlock()
	if d.ingestion.dpCalls != 1 {
		t.Errorf("expected handler to be called on reprocess, got %d calls", d.ingestion.dpCalls)
	}

	if !d.logger.hasMessage("reprocessing stale event") {
		t.Error("expected reprocessing stale event log")
	}

	if !d.metrics.hasProcessed(model.TopicDPArtifactsProcessingReady, "success") {
		t.Error("expected success metric after reprocess")
	}
}

// ---------------------------------------------------------------------------
// Tests: Idempotency check error
// ---------------------------------------------------------------------------

func TestHandleDPArtifacts_IdempotencyError(t *testing.T) {
	d := newTestDeps()
	d.idempotency.checkErr = errors.New("redis down")
	c := d.newConsumer()
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}

	event := validDPArtifactsEvent()
	body := mustMarshal(t, event)
	handler := d.broker.handlerFor(model.TopicDPArtifactsProcessingReady)

	err := handler(context.Background(), body)
	if err != nil {
		t.Fatalf("expected nil, got: %v", err)
	}

	d.ingestion.mu.Lock()
	defer d.ingestion.mu.Unlock()
	if d.ingestion.dpCalls != 0 {
		t.Error("handler should not be called on idempotency error")
	}

	if !d.logger.hasMessage("idempotency check failed") {
		t.Error("expected idempotency check failed log")
	}

	if !d.metrics.hasProcessed(model.TopicDPArtifactsProcessingReady, "error") {
		t.Error("expected error metric")
	}
}

// ---------------------------------------------------------------------------
// Tests: Handler error
// ---------------------------------------------------------------------------

func TestHandleDPArtifacts_HandlerError_AlwaysReturnsNil(t *testing.T) {
	d := newTestDeps()
	d.ingestion.dpErr = port.NewStorageError("S3 down", nil)
	c := d.newConsumer()
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}

	event := validDPArtifactsEvent()
	body := mustMarshal(t, event)
	handler := d.broker.handlerFor(model.TopicDPArtifactsProcessingReady)

	err := handler(context.Background(), body)
	if err != nil {
		t.Fatalf("expected nil (always nil), got: %v", err)
	}

	if !d.logger.hasMessage("handler failed") {
		t.Error("expected handler failed log")
	}

	d.idempotency.mu.Lock()
	defer d.idempotency.mu.Unlock()
	if d.idempotency.cleanupCalls != 1 {
		t.Errorf("expected 1 cleanup call, got %d", d.idempotency.cleanupCalls)
	}

	if !d.metrics.hasProcessed(model.TopicDPArtifactsProcessingReady, "error") {
		t.Error("expected error metric")
	}
}

func TestHandleDiffReady_HandlerError(t *testing.T) {
	d := newTestDeps()
	d.diff.err = port.NewDatabaseError("tx failed", nil)
	c := d.newConsumer()
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}

	event := validDiffReadyEvent()
	body := mustMarshal(t, event)
	handler := d.broker.handlerFor(model.TopicDPArtifactsDiffReady)

	err := handler(context.Background(), body)
	if err != nil {
		t.Fatalf("expected nil, got: %v", err)
	}

	d.idempotency.mu.Lock()
	defer d.idempotency.mu.Unlock()
	if d.idempotency.cleanupCalls != 1 {
		t.Errorf("expected cleanup on handler error, got %d calls", d.idempotency.cleanupCalls)
	}
}

func TestHandleGetSemanticTree_HandlerError(t *testing.T) {
	d := newTestDeps()
	d.query.getTreeErr = port.NewStorageError("S3 down", nil)
	c := d.newConsumer()
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}

	event := validGetSemanticTreeEvent()
	body := mustMarshal(t, event)
	handler := d.broker.handlerFor(model.TopicDPRequestsSemanticTree)

	err := handler(context.Background(), body)
	if err != nil {
		t.Fatalf("expected nil, got: %v", err)
	}

	d.idempotency.mu.Lock()
	defer d.idempotency.mu.Unlock()
	if d.idempotency.cleanupCalls != 1 {
		t.Errorf("expected cleanup on handler error, got %d calls", d.idempotency.cleanupCalls)
	}

	if !d.metrics.hasProcessed(model.TopicDPRequestsSemanticTree, "error") {
		t.Error("expected error metric")
	}
}

func TestHandleGetArtifacts_HandlerError(t *testing.T) {
	d := newTestDeps()
	d.query.getArtifactsErr = port.NewArtifactNotFoundError("ver-5", "SEMANTIC_TREE")
	c := d.newConsumer()
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}

	event := validGetArtifactsEvent()
	body := mustMarshal(t, event)
	handler := d.broker.handlerFor(model.TopicLICRequestsArtifacts)

	err := handler(context.Background(), body)
	if err != nil {
		t.Fatalf("expected nil, got: %v", err)
	}

	d.idempotency.mu.Lock()
	defer d.idempotency.mu.Unlock()
	if d.idempotency.cleanupCalls != 1 {
		t.Errorf("expected cleanup on handler error, got %d calls", d.idempotency.cleanupCalls)
	}

	if !d.metrics.hasProcessed(model.TopicLICRequestsArtifacts, "error") {
		t.Error("expected error metric")
	}
}

// ---------------------------------------------------------------------------
// Tests: Schema version warning (REV-031)
// ---------------------------------------------------------------------------

func TestSchemaVersionWarning(t *testing.T) {
	d := newTestDeps()
	c := d.newConsumer()
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}

	// Event with unknown schema_version
	event := validDPArtifactsEvent()
	body := mustMarshal(t, event)
	// Inject schema_version into JSON
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatal(err)
	}
	m["schema_version"] = "2.0"
	body = mustMarshal(t, m)

	handler := d.broker.handlerFor(model.TopicDPArtifactsProcessingReady)
	err := handler(context.Background(), body)
	if err != nil {
		t.Fatalf("expected nil, got: %v", err)
	}

	if !d.logger.hasMessage("unknown schema_version") {
		t.Error("expected schema_version warning log")
	}

	// Handler should still be called (best effort processing)
	d.ingestion.mu.Lock()
	defer d.ingestion.mu.Unlock()
	if d.ingestion.dpCalls != 1 {
		t.Error("handler should be called despite unknown schema_version")
	}
}

func TestSchemaVersion_KnownVersion_NoWarning(t *testing.T) {
	d := newTestDeps()
	c := d.newConsumer()
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}

	event := validDPArtifactsEvent()
	body := mustMarshal(t, event)
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatal(err)
	}
	m["schema_version"] = "1.0"
	body = mustMarshal(t, m)

	handler := d.broker.handlerFor(model.TopicDPArtifactsProcessingReady)
	_ = handler(context.Background(), body)

	if d.logger.hasMessage("unknown schema_version") {
		t.Error("no warning expected for known schema version 1.0")
	}
}

// ---------------------------------------------------------------------------
// Tests: Forward compatibility (unknown fields ignored)
// ---------------------------------------------------------------------------

func TestForwardCompatibility_UnknownFieldsIgnored(t *testing.T) {
	d := newTestDeps()
	c := d.newConsumer()
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}

	event := validDPArtifactsEvent()
	body := mustMarshal(t, event)
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatal(err)
	}
	m["new_field_v2"] = "some_value"
	m["another_new_field"] = 42
	body = mustMarshal(t, m)

	handler := d.broker.handlerFor(model.TopicDPArtifactsProcessingReady)
	err := handler(context.Background(), body)
	if err != nil {
		t.Fatalf("expected nil, got: %v", err)
	}

	d.ingestion.mu.Lock()
	defer d.ingestion.mu.Unlock()
	if d.ingestion.dpCalls != 1 {
		t.Error("handler should be called — unknown fields must be ignored")
	}
}

// ---------------------------------------------------------------------------
// Tests: Panic recovery
// ---------------------------------------------------------------------------

func TestPanicRecovery_ReturnsNil(t *testing.T) {
	d := newTestDeps()
	d.ingestion.dpErr = nil
	c := d.newConsumer()
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}

	// Create a panicking ingestion handler
	panicIngestion := &mockIngestion{}
	c.ingestion = panicIngestion
	// Override the handler to panic
	// We need a custom approach: substitute the ingestion with a panicking version
	// Actually, we need to test the wrapper. Let's use a different approach:
	// call wrapHandler directly.
	topic := model.TopicDPArtifactsProcessingReady
	wrapped := c.wrapHandler(topic, func(_ context.Context, _ []byte) {
		panic("test panic")
	})

	err := wrapped(context.Background(), []byte(`{}`))
	if err != nil {
		t.Fatalf("expected nil after panic recovery, got: %v", err)
	}

	if !d.logger.hasMessage("panic in event handler") {
		t.Error("expected panic recovery log")
	}

	if !d.metrics.hasProcessed(topic, "panic") {
		t.Error("expected panic metric")
	}
}

// ---------------------------------------------------------------------------
// Tests: rawPreview
// ---------------------------------------------------------------------------

func TestRawPreview(t *testing.T) {
	short := []byte("short message")
	if got := rawPreview(short); got != "short message" {
		t.Errorf("expected full message, got: %s", got)
	}

	long := make([]byte, 300)
	for i := range long {
		long[i] = 'A'
	}
	got := rawPreview(long)
	if len(got) != maxPreviewLen+3 { // +3 for "..."
		t.Errorf("expected truncated to %d chars, got %d", maxPreviewLen+3, len(got))
	}
	if !strings.HasSuffix(got, "...") {
		t.Error("expected ... suffix")
	}
}

// ---------------------------------------------------------------------------
// Tests: validateCommon
// ---------------------------------------------------------------------------

func TestValidateCommon(t *testing.T) {
	now := time.Now()

	t.Run("all valid", func(t *testing.T) {
		err := validateCommon("corr", "job", "doc", now)
		if err != nil {
			t.Errorf("expected nil, got: %v", err)
		}
	})

	t.Run("all missing", func(t *testing.T) {
		err := validateCommon("", "", "", time.Time{})
		if err == nil {
			t.Fatal("expected error")
		}
		msg := err.Error()
		if !strings.Contains(msg, "correlation_id") ||
			!strings.Contains(msg, "job_id") ||
			!strings.Contains(msg, "document_id") ||
			!strings.Contains(msg, "timestamp") {
			t.Errorf("expected all missing fields listed, got: %s", msg)
		}
	})

	t.Run("whitespace only treated as empty", func(t *testing.T) {
		err := validateCommon("  ", "job", "doc", now)
		if err == nil {
			t.Fatal("expected error for whitespace-only correlation_id")
		}
	})
}

// ---------------------------------------------------------------------------
// Tests: checkSchemaVersion
// ---------------------------------------------------------------------------

func TestCheckSchemaVersion(t *testing.T) {
	t.Run("no schema_version", func(t *testing.T) {
		log := &mockLogger{}
		checkSchemaVersion(log, []byte(`{"job_id":"1"}`))
		if log.hasLevel("warn") {
			t.Error("no warning expected when schema_version absent")
		}
	})

	t.Run("schema_version 1.0", func(t *testing.T) {
		log := &mockLogger{}
		checkSchemaVersion(log, []byte(`{"schema_version":"1.0"}`))
		if log.hasLevel("warn") {
			t.Error("no warning expected for version 1.0")
		}
	})

	t.Run("unknown schema_version", func(t *testing.T) {
		log := &mockLogger{}
		checkSchemaVersion(log, []byte(`{"schema_version":"3.0"}`))
		if !log.hasMessage("unknown schema_version") {
			t.Error("expected warning for unknown schema_version")
		}
	})

	t.Run("invalid JSON", func(t *testing.T) {
		log := &mockLogger{}
		checkSchemaVersion(log, []byte(`not json`))
		if log.hasLevel("warn") {
			t.Error("no warning expected for invalid JSON")
		}
	})
}

// ---------------------------------------------------------------------------
// Tests: noopFallback
// ---------------------------------------------------------------------------

func TestNoopFallback(t *testing.T) {
	processed, err := noopFallback(context.Background())
	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}
	if processed {
		t.Error("expected false (not processed)")
	}
}

// ---------------------------------------------------------------------------
// Tests: MarkCompleted error is logged but not propagated
// ---------------------------------------------------------------------------

func TestMarkCompletedError_Logged(t *testing.T) {
	d := newTestDeps()
	d.idempotency.markErr = errors.New("redis write failed")
	c := d.newConsumer()
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}

	event := validDPArtifactsEvent()
	body := mustMarshal(t, event)
	handler := d.broker.handlerFor(model.TopicDPArtifactsProcessingReady)

	err := handler(context.Background(), body)
	if err != nil {
		t.Fatalf("expected nil (mark error not propagated), got: %v", err)
	}

	if !d.logger.hasMessage("failed to mark idempotency completed") {
		t.Error("expected mark completed error log")
	}

	// Success metric should still be emitted since the handler succeeded
	if !d.metrics.hasProcessed(model.TopicDPArtifactsProcessingReady, "success") {
		t.Error("expected success metric despite mark error")
	}
}

// ---------------------------------------------------------------------------
// Tests: Cleanup error is logged but processing continues
// ---------------------------------------------------------------------------

func TestCleanupError_Logged(t *testing.T) {
	d := newTestDeps()
	d.ingestion.dpErr = port.NewStorageError("S3 down", nil)
	d.idempotency.cleanupErr = errors.New("redis cleanup failed")
	c := d.newConsumer()
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}

	event := validDPArtifactsEvent()
	body := mustMarshal(t, event)
	handler := d.broker.handlerFor(model.TopicDPArtifactsProcessingReady)

	err := handler(context.Background(), body)
	if err != nil {
		t.Fatalf("expected nil, got: %v", err)
	}

	if !d.logger.hasMessage("failed to cleanup idempotency key") {
		t.Error("expected cleanup error log")
	}
}

// ---------------------------------------------------------------------------
// Tests: Correct idempotency key per topic
// ---------------------------------------------------------------------------

func TestIdempotencyKey_DPArtifacts(t *testing.T) {
	d := newTestDeps()
	c := d.newConsumer()
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}

	event := validDPArtifactsEvent()
	body := mustMarshal(t, event)
	handler := d.broker.handlerFor(model.TopicDPArtifactsProcessingReady)
	_ = handler(context.Background(), body)

	d.idempotency.mu.Lock()
	defer d.idempotency.mu.Unlock()
	expected := idempotency.KeyForDPArtifacts("job-1")
	if d.idempotency.lastKey != expected {
		t.Errorf("expected key %s, got %s", expected, d.idempotency.lastKey)
	}
}

func TestIdempotencyKey_GetSemanticTree(t *testing.T) {
	d := newTestDeps()
	c := d.newConsumer()
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}

	event := validGetSemanticTreeEvent()
	body := mustMarshal(t, event)
	handler := d.broker.handlerFor(model.TopicDPRequestsSemanticTree)
	_ = handler(context.Background(), body)

	d.idempotency.mu.Lock()
	defer d.idempotency.mu.Unlock()
	expected := idempotency.KeyForSemanticTreeRequest("job-2", "ver-2")
	if d.idempotency.lastKey != expected {
		t.Errorf("expected key %s, got %s", expected, d.idempotency.lastKey)
	}
}

func TestIdempotencyKey_LICRequest(t *testing.T) {
	d := newTestDeps()
	c := d.newConsumer()
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}

	event := validGetArtifactsEvent()
	body := mustMarshal(t, event)
	handler := d.broker.handlerFor(model.TopicLICRequestsArtifacts)
	_ = handler(context.Background(), body)

	d.idempotency.mu.Lock()
	defer d.idempotency.mu.Unlock()
	expected := idempotency.KeyForLICRequest("job-5", "ver-5")
	if d.idempotency.lastKey != expected {
		t.Errorf("expected key %s, got %s", expected, d.idempotency.lastKey)
	}
}

func TestIdempotencyKey_RERequest(t *testing.T) {
	d := newTestDeps()
	c := d.newConsumer()
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}

	event := validGetArtifactsEvent()
	body := mustMarshal(t, event)
	handler := d.broker.handlerFor(model.TopicRERequestsArtifacts)
	_ = handler(context.Background(), body)

	d.idempotency.mu.Lock()
	defer d.idempotency.mu.Unlock()
	expected := idempotency.KeyForRERequest("job-5", "ver-5")
	if d.idempotency.lastKey != expected {
		t.Errorf("expected key %s, got %s", expected, d.idempotency.lastKey)
	}
}

// ---------------------------------------------------------------------------
// Tests: All handlers return nil (always nil guarantee)
// ---------------------------------------------------------------------------

func TestAllHandlers_AlwaysReturnNil(t *testing.T) {
	topics := defaultTopics()
	allTopics := []string{
		topics.DPArtifactsReady,
		topics.DPSemanticTreeReq,
		topics.DPDiffReady,
		topics.LICArtifactsReady,
		topics.LICRequestArtifacts,
		topics.REArtifactsReady,
		topics.RERequestArtifacts,
	}

	for _, topic := range allTopics {
		t.Run(topic, func(t *testing.T) {
			d := newTestDeps()
			c := d.newConsumer()
			if err := c.Start(); err != nil {
				t.Fatal(err)
			}

			handler := d.broker.handlerFor(topic)
			// Send completely invalid data
			err := handler(context.Background(), []byte(`garbage`))
			if err != nil {
				t.Errorf("handler for %s returned non-nil: %v", topic, err)
			}
		})
	}
}
