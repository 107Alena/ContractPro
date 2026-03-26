//go:build integration

package integration

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	compapp "contractpro/document-processing/internal/application/comparison"
	"contractpro/document-processing/internal/application/lifecycle"
	"contractpro/document-processing/internal/application/pendingresponse"
	"contractpro/document-processing/internal/application/processing"
	"contractpro/document-processing/internal/application/warning"
	"contractpro/document-processing/internal/config"
	"contractpro/document-processing/internal/domain/model"
	"contractpro/document-processing/internal/domain/port"
	compengine "contractpro/document-processing/internal/engine/comparison"
	"contractpro/document-processing/internal/infra/concurrency"
	"contractpro/document-processing/internal/infra/observability"
	"contractpro/document-processing/internal/ingress/consumer"
	"contractpro/document-processing/internal/ingress/dispatcher"
)

// testTopicProcessDocument is the topic used for process-document commands
// in integration tests, defined once to prevent typos.
const testTopicProcessDocument = "dp.commands.process-document"

// testTopicCompareVersions is the topic used for compare-versions commands
// in integration tests, defined once to prevent typos.
const testTopicCompareVersions = "dp.commands.compare-versions"

// ---------------------------------------------------------------------------
// 1. captureBroker — implements consumer.BrokerSubscriber
// ---------------------------------------------------------------------------

// Compile-time interface compliance check.
var _ consumer.BrokerSubscriber = (*captureBroker)(nil)

// captureBroker captures Subscribe handlers so that tests can deliver messages
// synchronously via deliverToTopic.
type captureBroker struct {
	mu       sync.Mutex
	handlers map[string]func(ctx context.Context, body []byte) error
}

func (b *captureBroker) Subscribe(topic string, handler func(ctx context.Context, body []byte) error) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.handlers == nil {
		b.handlers = make(map[string]func(ctx context.Context, body []byte) error)
	}
	b.handlers[topic] = handler
	return nil
}

// deliverToTopic invokes the handler registered for the given topic
// synchronously. The entire pipeline completes before it returns.
// Returns an error if no handler is registered for the topic.
func (b *captureBroker) deliverToTopic(topic string, body []byte) error {
	b.mu.Lock()
	handler, ok := b.handlers[topic]
	b.mu.Unlock()
	if !ok {
		return fmt.Errorf("captureBroker: no handler registered for topic %q", topic)
	}
	return handler(context.Background(), body)
}

// ---------------------------------------------------------------------------
// 2. recordingPublisher — implements port.EventPublisherPort
// ---------------------------------------------------------------------------

var _ port.EventPublisherPort = (*recordingPublisher)(nil)

type recordingPublisher struct {
	mu                  sync.Mutex
	statusChanged       []model.StatusChangedEvent
	processingCompleted []model.ProcessingCompletedEvent
	processingFailed    []model.ProcessingFailedEvent
	comparisonCompleted []model.ComparisonCompletedEvent
	comparisonFailed    []model.ComparisonFailedEvent
}

func (p *recordingPublisher) PublishStatusChanged(_ context.Context, event model.StatusChangedEvent) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.statusChanged = append(p.statusChanged, event)
	return nil
}

func (p *recordingPublisher) PublishProcessingCompleted(_ context.Context, event model.ProcessingCompletedEvent) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.processingCompleted = append(p.processingCompleted, event)
	return nil
}

func (p *recordingPublisher) PublishProcessingFailed(_ context.Context, event model.ProcessingFailedEvent) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.processingFailed = append(p.processingFailed, event)
	return nil
}

func (p *recordingPublisher) PublishComparisonCompleted(_ context.Context, event model.ComparisonCompletedEvent) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.comparisonCompleted = append(p.comparisonCompleted, event)
	return nil
}

func (p *recordingPublisher) PublishComparisonFailed(_ context.Context, event model.ComparisonFailedEvent) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.comparisonFailed = append(p.comparisonFailed, event)
	return nil
}

// ---------------------------------------------------------------------------
// 3. memoryIdempotencyStore — implements port.IdempotencyStorePort
// ---------------------------------------------------------------------------

var _ port.IdempotencyStorePort = (*memoryIdempotencyStore)(nil)

type memoryIdempotencyStore struct {
	mu    sync.Mutex
	store map[string]port.IdempotencyStatus
}

func newMemoryIdempotencyStore() *memoryIdempotencyStore {
	return &memoryIdempotencyStore{
		store: make(map[string]port.IdempotencyStatus),
	}
}

func (s *memoryIdempotencyStore) Check(_ context.Context, jobID string) (port.IdempotencyStatus, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	status, ok := s.store[jobID]
	if !ok {
		return port.IdempotencyStatusNew, nil
	}
	return status, nil
}

func (s *memoryIdempotencyStore) Register(_ context.Context, jobID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, ok := s.store[jobID]; ok {
		if existing == port.IdempotencyStatusInProgress || existing == port.IdempotencyStatusCompleted {
			return port.NewDuplicateJobError(jobID)
		}
	}
	s.store[jobID] = port.IdempotencyStatusInProgress
	return nil
}

func (s *memoryIdempotencyStore) MarkCompleted(_ context.Context, jobID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.store[jobID] = port.IdempotencyStatusCompleted
	return nil
}

// ---------------------------------------------------------------------------
// 4. recordingTempStorage — implements port.TempStoragePort
// ---------------------------------------------------------------------------

var _ port.TempStoragePort = (*recordingTempStorage)(nil)

type recordingTempStorage struct {
	mu              sync.Mutex
	data            map[string][]byte
	deletedPrefixes []string
	deletedKeys     []string
}

func newRecordingTempStorage() *recordingTempStorage {
	return &recordingTempStorage{
		data: make(map[string][]byte),
	}
}

func (s *recordingTempStorage) Upload(_ context.Context, key string, data io.Reader) error {
	b, err := io.ReadAll(data)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[key] = b
	return nil
}

func (s *recordingTempStorage) Download(_ context.Context, key string) (io.ReadCloser, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.data[key]
	if !ok {
		return nil, port.NewStorageError("key not found: "+key, nil)
	}
	return io.NopCloser(bytes.NewReader(b)), nil
}

func (s *recordingTempStorage) Delete(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.data, key)
	s.deletedKeys = append(s.deletedKeys, key)
	return nil
}

func (s *recordingTempStorage) DeleteByPrefix(_ context.Context, prefix string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for k := range s.data {
		if strings.HasPrefix(k, prefix) {
			delete(s.data, k)
		}
	}
	s.deletedPrefixes = append(s.deletedPrefixes, prefix)
	return nil
}

// ---------------------------------------------------------------------------
// 5. recordingDMSender — implements port.DMArtifactSenderPort
// ---------------------------------------------------------------------------

var _ port.DMArtifactSenderPort = (*recordingDMSender)(nil)

type recordingDMSender struct {
	mu            sync.Mutex
	sentArtifacts []model.DocumentProcessingArtifactsReady
	sentDiffs     []model.DocumentVersionDiffReady
}

func (d *recordingDMSender) SendArtifacts(_ context.Context, event model.DocumentProcessingArtifactsReady) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.sentArtifacts = append(d.sentArtifacts, event)
	return nil
}

func (d *recordingDMSender) SendDiffResult(_ context.Context, event model.DocumentVersionDiffReady) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.sentDiffs = append(d.sentDiffs, event)
	return nil
}

// ---------------------------------------------------------------------------
// 6. stubFetcher — implements port.SourceFileFetcherPort
// ---------------------------------------------------------------------------

var _ port.SourceFileFetcherPort = (*stubFetcher)(nil)

type stubFetcher struct {
	mu        sync.Mutex
	result    *port.FetchResult
	err       error
	callCount int
}

func (f *stubFetcher) Fetch(_ context.Context, _ model.ProcessDocumentCommand) (*port.FetchResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.callCount++
	if f.err != nil {
		return nil, f.err
	}
	return f.result, nil
}

// ---------------------------------------------------------------------------
// 7. stubOCRProcessor — implements port.OCRProcessorPort
// ---------------------------------------------------------------------------

var _ port.OCRProcessorPort = (*stubOCRProcessor)(nil)

type stubOCRProcessor struct {
	mu     sync.Mutex
	result *model.OCRRawArtifact
	err    error
}

func (o *stubOCRProcessor) Process(_ context.Context, _ string, _ bool) (*model.OCRRawArtifact, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.err != nil {
		return nil, o.err
	}
	return o.result, nil
}

// ---------------------------------------------------------------------------
// 8. stubTextExtractor — implements port.TextExtractionPort
// ---------------------------------------------------------------------------

var _ port.TextExtractionPort = (*stubTextExtractor)(nil)

type stubTextExtractor struct {
	mu       sync.Mutex
	text     *model.ExtractedText
	warnings []model.ProcessingWarning
	err      error
}

func (e *stubTextExtractor) Extract(_ context.Context, _ string, _ *model.OCRRawArtifact) (*model.ExtractedText, []model.ProcessingWarning, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.err != nil {
		return nil, nil, e.err
	}
	return e.text, e.warnings, nil
}

// ---------------------------------------------------------------------------
// 9. stubStructExtractor — implements port.StructureExtractionPort
// ---------------------------------------------------------------------------

var _ port.StructureExtractionPort = (*stubStructExtractor)(nil)

type stubStructExtractor struct {
	mu        sync.Mutex
	structure *model.DocumentStructure
	warnings  []model.ProcessingWarning
	err       error
}

func (e *stubStructExtractor) Extract(_ context.Context, _ *model.ExtractedText) (*model.DocumentStructure, []model.ProcessingWarning, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.err != nil {
		return nil, nil, e.err
	}
	return e.structure, e.warnings, nil
}

// ---------------------------------------------------------------------------
// 10. stubTreeBuilder — implements port.SemanticTreeBuilderPort
// ---------------------------------------------------------------------------

var _ port.SemanticTreeBuilderPort = (*stubTreeBuilder)(nil)

type stubTreeBuilder struct {
	mu   sync.Mutex
	tree *model.SemanticTree
	err  error
}

func (b *stubTreeBuilder) Build(_ context.Context, _ *model.ExtractedText, _ *model.DocumentStructure) (*model.SemanticTree, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.err != nil {
		return nil, b.err
	}
	return b.tree, nil
}

// ---------------------------------------------------------------------------
// 11. stubValidator — implements port.InputValidatorPort
// ---------------------------------------------------------------------------

var _ port.InputValidatorPort = (*stubValidator)(nil)

type stubValidator struct {
	mu  sync.Mutex
	err error
}

func (v *stubValidator) Validate(_ context.Context, _ model.ProcessDocumentCommand) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.err
}

// ---------------------------------------------------------------------------
// 12. noopComparisonHandler — implements port.ComparisonCommandHandler
// ---------------------------------------------------------------------------

var _ port.ComparisonCommandHandler = (*noopComparisonHandler)(nil)

type noopComparisonHandler struct{}

func (n *noopComparisonHandler) HandleCompareVersions(_ context.Context, _ model.CompareVersionsCommand) error {
	return nil
}

// ---------------------------------------------------------------------------
// 13. testHarness
// ---------------------------------------------------------------------------

type testHarness struct {
	broker        *captureBroker
	publisher     *recordingPublisher
	dmSender      *recordingDMSender
	idempotency   *memoryIdempotencyStore
	tempStorage   *recordingTempStorage
	fetcher       *stubFetcher
	ocrProcessor  *stubOCRProcessor
	textExtract   *stubTextExtractor
	structExtract *stubStructExtractor
	treeBuilder   *stubTreeBuilder
	validator     *stubValidator
	warnings      *warning.Collector
}

// ---------------------------------------------------------------------------
// 14. harnessOption + newTestHarness
// ---------------------------------------------------------------------------

type harnessConfig struct {
	maxRetries  int
	backoffBase time.Duration
}

type harnessOption func(*harnessConfig)

func withMaxRetries(n int) harnessOption {
	return func(c *harnessConfig) { c.maxRetries = n }
}

// newTestHarness creates a harness wired for the default happy-path scenario
// (text PDF, no warnings, maxRetries=1, backoff=1ms).
// Override stubs on the returned harness BEFORE calling deliverToTopic.
func newTestHarness(t *testing.T, opts ...harnessOption) *testHarness {
	t.Helper()

	cfg := harnessConfig{
		maxRetries:  1, // No retries in the default harness; tests requiring retry should use withMaxRetries.
		backoffBase: time.Millisecond,
	}
	for _, o := range opts {
		o(&cfg)
	}

	publisher := &recordingPublisher{}
	idempotency := newMemoryIdempotencyStore()
	tempStorage := newRecordingTempStorage()
	dmSender := &recordingDMSender{}

	validator := &stubValidator{err: nil}
	fetcher := &stubFetcher{result: defaultFetchResult()}
	ocrProcessor := &stubOCRProcessor{
		result: &model.OCRRawArtifact{Status: model.OCRStatusNotApplicable},
	}
	textExtract := &stubTextExtractor{
		text:     defaultExtractedText(),
		warnings: nil,
	}
	structExtract := &stubStructExtractor{
		structure: defaultStructure(),
		warnings:  nil,
	}
	treeBuilder := &stubTreeBuilder{
		tree: defaultSemanticTree(),
	}

	warningCollector := warning.NewCollector()

	logger := observability.NewLogger("error")
	metrics := observability.NewMetrics()

	lifecycleMgr := lifecycle.NewLifecycleManager(publisher, idempotency, 30*time.Second, nil, logger)

	orchestrator := processing.NewOrchestrator(
		lifecycleMgr,
		warningCollector,
		validator,
		fetcher,
		ocrProcessor,
		textExtract,
		structExtract,
		treeBuilder,
		tempStorage,
		publisher,
		dmSender,
		logger,
		cfg.maxRetries,
		cfg.backoffBase,
	)

	compOrch := &noopComparisonHandler{}
	limiter := concurrency.New(5, metrics, logger)

	disp := dispatcher.NewDispatcher(idempotency, limiter, orchestrator, compOrch, logger)

	broker := &captureBroker{}
	brokerCfg := config.BrokerConfig{
		TopicProcessDocument: testTopicProcessDocument,
		TopicCompareVersions: testTopicCompareVersions,
	}

	cons := consumer.NewConsumer(broker, disp, logger, brokerCfg)
	if err := cons.Start(); err != nil {
		t.Fatalf("newTestHarness: consumer.Start failed: %v", err)
	}

	return &testHarness{
		broker:        broker,
		publisher:     publisher,
		dmSender:      dmSender,
		idempotency:   idempotency,
		tempStorage:   tempStorage,
		fetcher:       fetcher,
		ocrProcessor:  ocrProcessor,
		textExtract:   textExtract,
		structExtract: structExtract,
		treeBuilder:   treeBuilder,
		validator:     validator,
		warnings:      warningCollector,
	}
}

// ---------------------------------------------------------------------------
// 15. Default helper functions
// ---------------------------------------------------------------------------

func defaultCommand() model.ProcessDocumentCommand {
	return model.ProcessDocumentCommand{
		JobID:      "job-integ-1",
		DocumentID: "doc-integ-1",
		FileURL:    "https://example.com/contract.pdf",
		OrgID:      "org-1",
		UserID:     "user-1",
		FileName:   "contract.pdf",
		FileSize:   1024,
		MimeType:   "application/pdf",
	}
}

func defaultFetchResult() *port.FetchResult {
	return &port.FetchResult{
		StorageKey: "job-integ-1/source.pdf",
		PageCount:  5,
		IsTextPDF:  true,
		FileSize:   1024,
	}
}

func defaultExtractedText() *model.ExtractedText {
	return &model.ExtractedText{
		DocumentID: "doc-integ-1",
		Pages: []model.PageText{
			{PageNumber: 1, Text: "ДОГОВОР ПОСТАВКИ №123\n\n1. Предмет договора\n1.1. Поставщик обязуется передать товар."},
			{PageNumber: 2, Text: "2. Цена и порядок расчётов\n2.1. Общая стоимость составляет 100 000 руб."},
		},
	}
}

func defaultStructure() *model.DocumentStructure {
	return &model.DocumentStructure{
		DocumentID: "doc-integ-1",
		Sections: []model.Section{
			{
				Number: "1",
				Title:  "Предмет договора",
				Clauses: []model.Clause{
					{Number: "1.1", Content: "Поставщик обязуется передать товар."},
				},
			},
		},
	}
}

func defaultSemanticTree() *model.SemanticTree {
	return &model.SemanticTree{
		DocumentID: "doc-integ-1",
		Root: &model.SemanticNode{
			ID:   "root",
			Type: model.NodeTypeRoot,
			Children: []*model.SemanticNode{
				{
					ID:      "section-1",
					Type:    model.NodeTypeSection,
					Content: "Предмет договора",
					Metadata: map[string]string{
						"number": "1",
						"title":  "Предмет договора",
					},
					Children: []*model.SemanticNode{
						{
							ID:      "clause-1.1",
							Type:    model.NodeTypeClause,
							Content: "Поставщик обязуется передать товар.",
							Metadata: map[string]string{
								"number": "1.1",
							},
						},
					},
				},
			},
		},
	}
}

// ---------------------------------------------------------------------------
// 16. noopProcessingHandler — implements port.ProcessingCommandHandler
// ---------------------------------------------------------------------------

var _ port.ProcessingCommandHandler = (*noopProcessingHandler)(nil)

type noopProcessingHandler struct{}

func (n *noopProcessingHandler) HandleProcessDocument(_ context.Context, _ model.ProcessDocumentCommand) error {
	return nil
}

// ---------------------------------------------------------------------------
// 17. treeRequesterMock — implements port.DMTreeRequesterPort
// ---------------------------------------------------------------------------

var _ port.DMTreeRequesterPort = (*treeRequesterMock)(nil)

// treeRequesterMock simulates the DM tree request/response cycle.
// When RequestSemanticTree is called, it records the request and immediately
// delivers the pre-configured tree (or error) to the pending response registry.
type treeRequesterMock struct {
	mu       sync.Mutex
	registry port.PendingResponseRegistryPort
	trees    map[string]model.SemanticTree // correlationID → tree to deliver
	errors   map[string]error              // correlationID → error to deliver
	requests []model.GetSemanticTreeRequest
	reqErr   error // if set, RequestSemanticTree returns this error immediately
}

func (m *treeRequesterMock) RequestSemanticTree(_ context.Context, req model.GetSemanticTreeRequest) error {
	m.mu.Lock()
	m.requests = append(m.requests, req)
	reqErr := m.reqErr
	errForCID := m.errors[req.EventMeta.CorrelationID]
	tree, hasTree := m.trees[req.EventMeta.CorrelationID]
	m.mu.Unlock()

	if reqErr != nil {
		return reqErr
	}

	// Deliver tree or error synchronously via the registry.
	if errForCID != nil {
		return m.registry.ReceiveError(req.EventMeta.CorrelationID, errForCID)
	}
	if hasTree {
		return m.registry.Receive(req.EventMeta.CorrelationID, tree)
	}

	// No tree configured — deliver an empty tree.
	return m.registry.Receive(req.EventMeta.CorrelationID, model.SemanticTree{})
}

// ---------------------------------------------------------------------------
// 18. confirmingDMSender — implements port.DMArtifactSenderPort
// ---------------------------------------------------------------------------

var _ port.DMArtifactSenderPort = (*confirmingDMSender)(nil)

// confirmingDMSender records sent artifacts and diffs. On SendDiffResult, it
// spawns a goroutine that confirms the diff via the pending response registry
// after a short delay, simulating DM's asynchronous confirmation.
type confirmingDMSender struct {
	mu            sync.Mutex
	registry      port.PendingResponseRegistryPort
	sentArtifacts []model.DocumentProcessingArtifactsReady
	sentDiffs     []model.DocumentVersionDiffReady
	confirmErr    error // if set, ReceiveError is called instead of Receive
	sendErr       error // if set, SendDiffResult returns this error immediately
}

func (d *confirmingDMSender) SendArtifacts(_ context.Context, event model.DocumentProcessingArtifactsReady) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.sentArtifacts = append(d.sentArtifacts, event)
	return nil
}

func (d *confirmingDMSender) SendDiffResult(_ context.Context, event model.DocumentVersionDiffReady) error {
	d.mu.Lock()
	d.sentDiffs = append(d.sentDiffs, event)
	sendErr := d.sendErr
	confirmErr := d.confirmErr
	registry := d.registry
	d.mu.Unlock()

	if sendErr != nil {
		return sendErr
	}

	// Simulate async DM confirmation after a short delay.
	go func() {
		time.Sleep(10 * time.Millisecond)
		if confirmErr != nil {
			_ = registry.ReceiveError(event.CorrelationID, confirmErr)
		} else {
			_ = registry.Receive(event.CorrelationID, model.SemanticTree{})
		}
	}()

	return nil
}

// ---------------------------------------------------------------------------
// 19. comparisonHarness
// ---------------------------------------------------------------------------

type comparisonHarness struct {
	broker      *captureBroker
	publisher   *recordingPublisher
	idempotency *memoryIdempotencyStore
	treeReq     *treeRequesterMock
	dmSender    *confirmingDMSender
	registry    *pendingresponse.Registry
	warnings    *warning.Collector
}

// ---------------------------------------------------------------------------
// 20. newComparisonHarness
// ---------------------------------------------------------------------------

// newComparisonHarness creates a harness wired for the comparison pipeline.
// It uses a real pendingresponse.Registry, real comparison.Comparer, and
// mock/stub implementations for broker, publisher, DM tree requester, and
// DM artifact sender. Default trees are pre-configured for the default
// compare command's correlation IDs.
func newComparisonHarness(t *testing.T, opts ...harnessOption) *comparisonHarness {
	t.Helper()

	cfg := harnessConfig{
		maxRetries:  1,
		backoffBase: time.Millisecond,
	}
	for _, o := range opts {
		o(&cfg)
	}

	publisher := &recordingPublisher{}
	idempotency := newMemoryIdempotencyStore()
	registry := pendingresponse.New()

	// Pre-configure default trees keyed by correlation ID.
	defaultCmd := defaultCompareCommand()
	baseCorrID := baseCorrelationID(defaultCmd)
	targetCorrID := targetCorrelationID(defaultCmd)

	treeReq := &treeRequesterMock{
		registry: registry,
		trees: map[string]model.SemanticTree{
			baseCorrID:   *defaultBaseTree(),
			targetCorrID: *defaultTargetTree(),
		},
		errors: make(map[string]error),
	}

	dmSender := &confirmingDMSender{
		registry: registry,
	}

	warningCollector := warning.NewCollector()
	logger := observability.NewLogger("error")
	metrics := observability.NewMetrics()

	comparer := compengine.NewComparer()

	lifecycleMgr := lifecycle.NewLifecycleManager(publisher, idempotency, 30*time.Second, nil, logger)

	compOrch := compapp.NewOrchestrator(
		lifecycleMgr,
		warningCollector,
		treeReq,
		dmSender,
		registry,
		comparer,
		publisher,
		logger,
		cfg.maxRetries,
		cfg.backoffBase,
	)

	procHandler := &noopProcessingHandler{}
	limiter := concurrency.New(5, metrics, logger)

	disp := dispatcher.NewDispatcher(idempotency, limiter, procHandler, compOrch, logger)

	broker := &captureBroker{}
	brokerCfg := config.BrokerConfig{
		TopicProcessDocument: testTopicProcessDocument,
		TopicCompareVersions: testTopicCompareVersions,
	}

	cons := consumer.NewConsumer(broker, disp, logger, brokerCfg)
	if err := cons.Start(); err != nil {
		t.Fatalf("newComparisonHarness: consumer.Start failed: %v", err)
	}

	return &comparisonHarness{
		broker:      broker,
		publisher:   publisher,
		idempotency: idempotency,
		treeReq:     treeReq,
		dmSender:    dmSender,
		registry:    registry,
		warnings:    warningCollector,
	}
}

// ---------------------------------------------------------------------------
// 21. Comparison default helpers
// ---------------------------------------------------------------------------

func defaultCompareCommand() model.CompareVersionsCommand {
	return model.CompareVersionsCommand{
		JobID:           "job-comp-1",
		DocumentID:      "doc-comp-1",
		BaseVersionID:   "v1",
		TargetVersionID: "v2",
	}
}

func defaultBaseTree() *model.SemanticTree {
	return &model.SemanticTree{
		DocumentID: "doc-comp-1",
		Root: &model.SemanticNode{
			ID:   "root",
			Type: model.NodeTypeRoot,
			Children: []*model.SemanticNode{
				{
					ID:      "section-1",
					Type:    model.NodeTypeSection,
					Content: "Предмет договора",
					Metadata: map[string]string{
						"number": "1",
						"title":  "Предмет договора",
					},
					Children: []*model.SemanticNode{
						{
							ID:      "clause-1.1",
							Type:    model.NodeTypeClause,
							Content: "Поставщик обязуется передать товар.",
							Metadata: map[string]string{
								"number": "1.1",
							},
						},
					},
				},
			},
		},
	}
}

func defaultTargetTree() *model.SemanticTree {
	return &model.SemanticTree{
		DocumentID: "doc-comp-1",
		Root: &model.SemanticNode{
			ID:   "root",
			Type: model.NodeTypeRoot,
			Children: []*model.SemanticNode{
				{
					ID:      "section-1",
					Type:    model.NodeTypeSection,
					Content: "Предмет договора",
					Metadata: map[string]string{
						"number": "1",
						"title":  "Предмет договора",
					},
					Children: []*model.SemanticNode{
						{
							ID:      "clause-1.1",
							Type:    model.NodeTypeClause,
							Content: "Поставщик обязуется передать товар и оборудование.",
							Metadata: map[string]string{
								"number": "1.1",
							},
						},
					},
				},
				{
					ID:      "section-2",
					Type:    model.NodeTypeSection,
					Content: "Цена и порядок расчётов",
					Metadata: map[string]string{
						"number": "2",
						"title":  "Цена и порядок расчётов",
					},
				},
			},
		},
	}
}

func baseCorrelationID(cmd model.CompareVersionsCommand) string {
	return fmt.Sprintf("%s:base:%s", cmd.JobID, cmd.BaseVersionID)
}

func targetCorrelationID(cmd model.CompareVersionsCommand) string {
	return fmt.Sprintf("%s:target:%s", cmd.JobID, cmd.TargetVersionID)
}
