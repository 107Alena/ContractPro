package processing

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"contractpro/document-processing/internal/application/lifecycle"
	"contractpro/document-processing/internal/domain/model"
	"contractpro/document-processing/internal/domain/port"
	"contractpro/document-processing/internal/engine/ocr"
	"contractpro/document-processing/internal/infra/observability"
)

// --- Mocks ---

// mockPublisher records all published events.
type mockPublisher struct {
	statusChanged       []model.StatusChangedEvent
	processingCompleted []model.ProcessingCompletedEvent
	processingFailed    []model.ProcessingFailedEvent
	comparisonCompleted []model.ComparisonCompletedEvent
	comparisonFailed    []model.ComparisonFailedEvent
	err                 error
}

func (m *mockPublisher) PublishStatusChanged(_ context.Context, event model.StatusChangedEvent) error {
	if m.err != nil {
		return m.err
	}
	m.statusChanged = append(m.statusChanged, event)
	return nil
}

func (m *mockPublisher) PublishProcessingCompleted(_ context.Context, event model.ProcessingCompletedEvent) error {
	if m.err != nil {
		return m.err
	}
	m.processingCompleted = append(m.processingCompleted, event)
	return nil
}

func (m *mockPublisher) PublishProcessingFailed(_ context.Context, event model.ProcessingFailedEvent) error {
	if m.err != nil {
		return m.err
	}
	m.processingFailed = append(m.processingFailed, event)
	return nil
}

func (m *mockPublisher) PublishComparisonCompleted(_ context.Context, event model.ComparisonCompletedEvent) error {
	if m.err != nil {
		return m.err
	}
	m.comparisonCompleted = append(m.comparisonCompleted, event)
	return nil
}

func (m *mockPublisher) PublishComparisonFailed(_ context.Context, event model.ComparisonFailedEvent) error {
	if m.err != nil {
		return m.err
	}
	m.comparisonFailed = append(m.comparisonFailed, event)
	return nil
}

// mockIdempotency is a no-op idempotency store for lifecycle manager construction.
type mockIdempotency struct {
	completed []string
	err       error
}

func (m *mockIdempotency) Check(_ context.Context, _ string) (port.IdempotencyStatus, error) {
	return port.IdempotencyStatusNew, nil
}

func (m *mockIdempotency) Register(_ context.Context, _ string) error {
	return nil
}

func (m *mockIdempotency) MarkCompleted(_ context.Context, jobID string) error {
	if m.err != nil {
		return m.err
	}
	m.completed = append(m.completed, jobID)
	return nil
}

// mockValidator implements port.InputValidatorPort.
type mockValidator struct {
	err error
}

func (m *mockValidator) Validate(_ context.Context, _ model.ProcessDocumentCommand) error {
	return m.err
}

// mockFetcher implements port.SourceFileFetcherPort.
type mockFetcher struct {
	result *port.FetchResult
	err    error
}

func (m *mockFetcher) Fetch(_ context.Context, _ model.ProcessDocumentCommand) (*port.FetchResult, error) {
	return m.result, m.err
}

// mockOCRService implements port.OCRServicePort (used by ocr.Adapter).
type mockOCRService struct {
	result string
	err    error
}

func (m *mockOCRService) Recognize(_ context.Context, _ io.Reader) (string, error) {
	return m.result, m.err
}

// mockTextExtractor implements port.TextExtractionPort.
type mockTextExtractor struct {
	text     *model.ExtractedText
	warnings []model.ProcessingWarning
	err      error
}

func (m *mockTextExtractor) Extract(_ context.Context, _ string, _ *model.OCRRawArtifact) (*model.ExtractedText, []model.ProcessingWarning, error) {
	return m.text, m.warnings, m.err
}

// mockStructureExtractor implements port.StructureExtractionPort.
type mockStructureExtractor struct {
	structure *model.DocumentStructure
	warnings  []model.ProcessingWarning
	err       error
}

func (m *mockStructureExtractor) Extract(_ context.Context, _ *model.ExtractedText) (*model.DocumentStructure, []model.ProcessingWarning, error) {
	return m.structure, m.warnings, m.err
}

// mockTreeBuilder implements port.SemanticTreeBuilderPort.
type mockTreeBuilder struct {
	tree *model.SemanticTree
	err  error
}

func (m *mockTreeBuilder) Build(_ context.Context, _ *model.ExtractedText, _ *model.DocumentStructure) (*model.SemanticTree, error) {
	return m.tree, m.err
}

// mockTempStorage implements port.TempStoragePort.
type mockTempStorage struct {
	deletedPrefixes []string
	deleteErr       error
}

func (m *mockTempStorage) Upload(_ context.Context, _ string, _ io.Reader) error {
	return nil
}

func (m *mockTempStorage) Download(_ context.Context, _ string) (io.ReadCloser, error) {
	return io.NopCloser(io.LimitReader(nil, 0)), nil
}

func (m *mockTempStorage) Delete(_ context.Context, _ string) error {
	return nil
}

func (m *mockTempStorage) DeleteByPrefix(_ context.Context, prefix string) error {
	if m.deleteErr != nil {
		return m.deleteErr
	}
	m.deletedPrefixes = append(m.deletedPrefixes, prefix)
	return nil
}

// mockDMSender implements port.DMArtifactSenderPort.
type mockDMSender struct {
	sentArtifacts []model.DocumentProcessingArtifactsReady
	err           error
}

func (m *mockDMSender) SendArtifacts(_ context.Context, event model.DocumentProcessingArtifactsReady) error {
	if m.err != nil {
		return m.err
	}
	m.sentArtifacts = append(m.sentArtifacts, event)
	return nil
}

func (m *mockDMSender) SendDiffResult(_ context.Context, _ model.DocumentVersionDiffReady) error {
	return nil
}

// mockDMAwaiter implements port.DMConfirmationAwaiterPort.
// By default it auto-confirms on Await (no blocking), suitable for tests
// that don't exercise the DM confirmation path.
// All methods are guarded by a mutex so the mock is safe for concurrent use
// (e.g. TestConcurrentJobs_WarningIsolation).
type mockDMAwaiter struct {
	mu         sync.Mutex
	registered []string
	confirmed  []string
	rejected   map[string]error
	awaitFunc  func(ctx context.Context, jobID string) (port.DMConfirmationResult, error)
}

func (m *mockDMAwaiter) Register(jobID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.registered = append(m.registered, jobID)
	return nil
}

func (m *mockDMAwaiter) Await(ctx context.Context, jobID string) (port.DMConfirmationResult, error) {
	m.mu.Lock()
	fn := m.awaitFunc
	m.mu.Unlock()
	if fn != nil {
		return fn(ctx, jobID)
	}
	return port.DMConfirmationResult{JobID: jobID}, nil
}

func (m *mockDMAwaiter) Confirm(jobID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.confirmed = append(m.confirmed, jobID)
	return nil
}

func (m *mockDMAwaiter) Reject(jobID string, err error) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.rejected == nil {
		m.rejected = make(map[string]error)
	}
	m.rejected[jobID] = err
	return nil
}

func (m *mockDMAwaiter) Cancel(jobID string) {}

// mockDLQ implements port.DLQPort.
type mockDLQ struct {
	mu       sync.Mutex
	messages []model.DLQMessage
	err      error
}

func (m *mockDLQ) SendToDLQ(_ context.Context, msg model.DLQMessage) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return m.err
	}
	m.messages = append(m.messages, msg)
	return nil
}

// callCountOCRProcessor counts invocations and fails until failUntil is reached.
type callCountOCRProcessor struct {
	callCount     int
	failUntil     int
	err           error
	successResult *model.OCRRawArtifact
	warnings      []model.ProcessingWarning
}

func (m *callCountOCRProcessor) Process(_ context.Context, _ string, _ bool) (*model.OCRRawArtifact, []model.ProcessingWarning, error) {
	m.callCount++
	if m.callCount <= m.failUntil {
		return nil, nil, m.err
	}
	return m.successResult, m.warnings, nil
}

// callCountDMSender counts invocations and always returns err.
type callCountDMSender struct {
	callCount int
	err       error
}

func (m *callCountDMSender) SendArtifacts(_ context.Context, _ model.DocumentProcessingArtifactsReady) error {
	m.callCount++
	return m.err
}

func (m *callCountDMSender) SendDiffResult(_ context.Context, _ model.DocumentVersionDiffReady) error {
	return nil
}

// --- Helpers ---

func nopLogger() *observability.Logger { return observability.NewLogger("error") }

func defaultCmd() model.ProcessDocumentCommand {
	return model.ProcessDocumentCommand{
		JobID:      "job-1",
		DocumentID: "doc-1",
		FileURL:    "https://example.com/contract.pdf",
		FileName:   "contract.pdf",
		FileSize:   1024,
		MimeType:   "application/pdf",
		OrgID:      "org-1",
		UserID:     "user-1",
	}
}

func defaultFetchResult() *port.FetchResult {
	return &port.FetchResult{
		StorageKey: "job-1/source.pdf",
		PageCount:  5,
		IsTextPDF:  true,
		FileSize:   1024,
	}
}

func scannedFetchResult() *port.FetchResult {
	return &port.FetchResult{
		StorageKey: "job-1/source.pdf",
		PageCount:  3,
		IsTextPDF:  false,
		FileSize:   2048,
	}
}

func defaultExtractedText() *model.ExtractedText {
	return &model.ExtractedText{
		DocumentID: "doc-1",
		Pages: []model.PageText{
			{PageNumber: 1, Text: "ДОГОВОР ПОСТАВКИ"},
			{PageNumber: 2, Text: "1. Предмет договора"},
		},
	}
}

func defaultStructure() *model.DocumentStructure {
	return &model.DocumentStructure{
		DocumentID: "doc-1",
		Sections: []model.Section{
			{Number: "1", Title: "Предмет договора", Content: "Поставщик обязуется..."},
		},
	}
}

func defaultSemanticTree() *model.SemanticTree {
	return &model.SemanticTree{
		DocumentID: "doc-1",
		Root: &model.SemanticNode{
			ID:   "root",
			Type: model.NodeTypeRoot,
			Children: []*model.SemanticNode{
				{ID: "s1", Type: model.NodeTypeSection, Content: "Предмет договора"},
			},
		},
	}
}

// testDeps holds all mock dependencies for constructing an Orchestrator in tests.
type testDeps struct {
	publisher     *mockPublisher
	idempotency   *mockIdempotency
	validator     *mockValidator
	fetcher       *mockFetcher
	ocrService    *mockOCRService
	textExtractor *mockTextExtractor
	structExtract *mockStructureExtractor
	treeBuilder   *mockTreeBuilder
	tempStorage   *mockTempStorage
	dmSender      *mockDMSender
	dmAwaiter     *mockDMAwaiter
	dlq           *mockDLQ
}

// newTestDeps creates testDeps pre-configured for a successful happy-path
// pipeline (text PDF, no OCR, no warnings).
func newTestDeps() *testDeps {
	return &testDeps{
		publisher:   &mockPublisher{},
		idempotency: &mockIdempotency{},
		validator:   &mockValidator{},
		fetcher: &mockFetcher{
			result: defaultFetchResult(),
		},
		ocrService:    &mockOCRService{},
		textExtractor: &mockTextExtractor{text: defaultExtractedText()},
		structExtract: &mockStructureExtractor{structure: defaultStructure()},
		treeBuilder:   &mockTreeBuilder{tree: defaultSemanticTree()},
		tempStorage:   &mockTempStorage{},
		dmSender:      &mockDMSender{},
		dmAwaiter:     &mockDMAwaiter{},
		dlq:           &mockDLQ{},
	}
}

func (d *testDeps) buildOrchestrator() *Orchestrator {
	ocrAdapter := ocr.NewAdapter(d.ocrService, d.tempStorage, 10, 1, time.Second)
	lm := lifecycle.NewLifecycleManager(d.publisher, d.idempotency, 120*time.Second, nil, nopLogger())
	return NewOrchestrator(
		lm,
		d.validator,
		d.fetcher,
		ocrAdapter,
		d.textExtractor,
		d.structExtract,
		d.treeBuilder,
		d.tempStorage,
		d.publisher,
		d.dmSender,
		d.dmAwaiter,
		d.dlq,
		nopLogger(),
		1,
		time.Millisecond,
	)
}

func (d *testDeps) buildOrchestratorWithRetry(maxRetries int, backoff time.Duration) *Orchestrator {
	ocrAdapter := ocr.NewAdapter(d.ocrService, d.tempStorage, 10, 1, time.Second)
	lm := lifecycle.NewLifecycleManager(d.publisher, d.idempotency, 120*time.Second, nil, nopLogger())
	return NewOrchestrator(
		lm,
		d.validator,
		d.fetcher,
		ocrAdapter,
		d.textExtractor,
		d.structExtract,
		d.treeBuilder,
		d.tempStorage,
		d.publisher,
		d.dmSender,
		d.dmAwaiter,
		d.dlq,
		nopLogger(),
		maxRetries,
		backoff,
	)
}

func (d *testDeps) buildOrchestratorWithOCR(ocrProc port.OCRProcessorPort, maxRetries int, backoff time.Duration) *Orchestrator {
	lm := lifecycle.NewLifecycleManager(d.publisher, d.idempotency, 120*time.Second, nil, nopLogger())
	return NewOrchestrator(
		lm,
		d.validator,
		d.fetcher,
		ocrProc,
		d.textExtractor,
		d.structExtract,
		d.treeBuilder,
		d.tempStorage,
		d.publisher,
		d.dmSender,
		d.dmAwaiter,
		d.dlq,
		nopLogger(),
		maxRetries,
		backoff,
	)
}

func (d *testDeps) buildOrchestratorWithDMSender(sender port.DMArtifactSenderPort, maxRetries int, backoff time.Duration) *Orchestrator {
	ocrAdapter := ocr.NewAdapter(d.ocrService, d.tempStorage, 10, 1, time.Second)
	lm := lifecycle.NewLifecycleManager(d.publisher, d.idempotency, 120*time.Second, nil, nopLogger())
	return NewOrchestrator(
		lm,
		d.validator,
		d.fetcher,
		ocrAdapter,
		d.textExtractor,
		d.structExtract,
		d.treeBuilder,
		d.tempStorage,
		d.publisher,
		sender,
		d.dmAwaiter,
		d.dlq,
		nopLogger(),
		maxRetries,
		backoff,
	)
}

// --- Tests: Happy Path ---

func TestHappyPathTextPDF_NoWarnings(t *testing.T) {
	deps := newTestDeps()
	orch := deps.buildOrchestrator()
	cmd := defaultCmd()

	err := orch.HandleProcessDocument(context.Background(), cmd)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	// Verify status transitions: QUEUED -> IN_PROGRESS, then IN_PROGRESS -> COMPLETED.
	if len(deps.publisher.statusChanged) != 2 {
		t.Fatalf("expected 2 StatusChangedEvents, got %d", len(deps.publisher.statusChanged))
	}

	first := deps.publisher.statusChanged[0]
	if first.OldStatus != model.StatusQueued || first.NewStatus != model.StatusInProgress {
		t.Errorf("first transition: expected QUEUED->IN_PROGRESS, got %s->%s", first.OldStatus, first.NewStatus)
	}

	second := deps.publisher.statusChanged[1]
	if second.OldStatus != model.StatusInProgress || second.NewStatus != model.StatusCompleted {
		t.Errorf("second transition: expected IN_PROGRESS->COMPLETED, got %s->%s", second.OldStatus, second.NewStatus)
	}

	// Verify ProcessingCompletedEvent.
	if len(deps.publisher.processingCompleted) != 1 {
		t.Fatalf("expected 1 ProcessingCompletedEvent, got %d", len(deps.publisher.processingCompleted))
	}
	completed := deps.publisher.processingCompleted[0]
	if completed.JobID != "job-1" {
		t.Errorf("expected JobID job-1, got %s", completed.JobID)
	}
	if completed.DocumentID != "doc-1" {
		t.Errorf("expected DocumentID doc-1, got %s", completed.DocumentID)
	}
	if completed.Status != model.StatusCompleted {
		t.Errorf("expected status COMPLETED, got %s", completed.Status)
	}
	if completed.HasWarnings {
		t.Error("expected HasWarnings=false for no-warning path")
	}
	if completed.WarningCount != 0 {
		t.Errorf("expected WarningCount=0, got %d", completed.WarningCount)
	}
	if completed.CorrelationID != "job-1" {
		t.Errorf("expected CorrelationID job-1, got %s", completed.CorrelationID)
	}

	// Verify artifacts were sent to DM.
	if len(deps.dmSender.sentArtifacts) != 1 {
		t.Fatalf("expected 1 SendArtifacts call, got %d", len(deps.dmSender.sentArtifacts))
	}
	artifacts := deps.dmSender.sentArtifacts[0]
	if artifacts.JobID != "job-1" {
		t.Errorf("artifacts JobID: expected job-1, got %s", artifacts.JobID)
	}
	if artifacts.DocumentID != "doc-1" {
		t.Errorf("artifacts DocumentID: expected doc-1, got %s", artifacts.DocumentID)
	}
	if artifacts.OCRRaw.Status != model.OCRStatusNotApplicable {
		t.Errorf("expected OCR status not_applicable for text PDF, got %s", artifacts.OCRRaw.Status)
	}
	if artifacts.SemanticTree.DocumentID != "doc-1" {
		t.Errorf("artifacts SemanticTree DocumentID: expected doc-1, got %s", artifacts.SemanticTree.DocumentID)
	}

	// Verify temp storage cleanup.
	if len(deps.tempStorage.deletedPrefixes) != 1 || deps.tempStorage.deletedPrefixes[0] != "job-1" {
		t.Errorf("expected DeleteByPrefix with job-1, got %v", deps.tempStorage.deletedPrefixes)
	}

	// Verify idempotency was marked (LifecycleManager marks on terminal status).
	if len(deps.idempotency.completed) != 1 || deps.idempotency.completed[0] != "job-1" {
		t.Errorf("expected idempotency marked for job-1, got %v", deps.idempotency.completed)
	}
}

func TestHappyPathTextPDF_WithWarnings(t *testing.T) {
	deps := newTestDeps()

	// Text extractor returns warnings.
	deps.textExtractor.warnings = []model.ProcessingWarning{
		{Code: "TEXT_EXTRACTION_EMPTY_PAGE", Message: "page 2 is empty", Stage: model.ProcessingStageTextExtraction},
	}
	// Structure extractor returns warnings.
	deps.structExtract.warnings = []model.ProcessingWarning{
		{Code: "STRUCTURE_NO_SECTIONS", Message: "no sections found", Stage: model.ProcessingStageStructureExtract},
	}

	orch := deps.buildOrchestrator()
	cmd := defaultCmd()

	err := orch.HandleProcessDocument(context.Background(), cmd)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	// Verify terminal status is COMPLETED_WITH_WARNINGS.
	if len(deps.publisher.statusChanged) != 2 {
		t.Fatalf("expected 2 StatusChangedEvents, got %d", len(deps.publisher.statusChanged))
	}
	second := deps.publisher.statusChanged[1]
	if second.NewStatus != model.StatusCompletedWithWarnings {
		t.Errorf("expected COMPLETED_WITH_WARNINGS, got %s", second.NewStatus)
	}

	// Verify ProcessingCompletedEvent reflects warnings.
	if len(deps.publisher.processingCompleted) != 1 {
		t.Fatalf("expected 1 ProcessingCompletedEvent, got %d", len(deps.publisher.processingCompleted))
	}
	completed := deps.publisher.processingCompleted[0]
	if completed.Status != model.StatusCompletedWithWarnings {
		t.Errorf("expected status COMPLETED_WITH_WARNINGS, got %s", completed.Status)
	}
	if !completed.HasWarnings {
		t.Error("expected HasWarnings=true")
	}
	if completed.WarningCount != 2 {
		t.Errorf("expected WarningCount=2, got %d", completed.WarningCount)
	}

	// Verify warnings were included in artifacts sent to DM.
	if len(deps.dmSender.sentArtifacts) != 1 {
		t.Fatalf("expected 1 SendArtifacts call, got %d", len(deps.dmSender.sentArtifacts))
	}
	if len(deps.dmSender.sentArtifacts[0].Warnings) != 2 {
		t.Errorf("expected 2 warnings in artifacts, got %d", len(deps.dmSender.sentArtifacts[0].Warnings))
	}
}

func TestHappyPathScannedPDF_OCRApplied(t *testing.T) {
	deps := newTestDeps()

	// Fetcher returns scanned PDF (IsTextPDF=false).
	deps.fetcher.result = &port.FetchResult{
		StorageKey: "job-1/source.pdf",
		PageCount:  3,
		IsTextPDF:  false,
		FileSize:   2048,
	}
	// OCR service returns recognized text (>=50 runes to avoid OCR_LOW_QUALITY warning).
	deps.ocrService.result = "Договор поставки товаров между ООО Ромашка и ООО Василёк от 01.01.2026"

	orch := deps.buildOrchestrator()
	cmd := defaultCmd()

	err := orch.HandleProcessDocument(context.Background(), cmd)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	// Verify the OCR result was passed through with applicable status.
	// The OCR adapter downloads from mockTempStorage (returns an empty reader)
	// and calls mockOCRService.Recognize (returns the configured text).
	// OCR adapter internals are tested separately in engine/ocr.
	if len(deps.dmSender.sentArtifacts) != 1 {
		t.Fatalf("expected 1 artifacts event, got %d", len(deps.dmSender.sentArtifacts))
	}
	ocrArtifact := deps.dmSender.sentArtifacts[0].OCRRaw
	if ocrArtifact.Status != model.OCRStatusApplicable {
		t.Errorf("expected OCR status applicable for scanned PDF, got %s", ocrArtifact.Status)
	}
	if ocrArtifact.RawText != "Договор поставки товаров между ООО Ромашка и ООО Василёк от 01.01.2026" {
		t.Errorf("expected OCR raw text to match, got %q", ocrArtifact.RawText)
	}

	// Verify pipeline completed.
	if len(deps.publisher.processingCompleted) != 1 {
		t.Fatalf("expected 1 ProcessingCompletedEvent, got %d", len(deps.publisher.processingCompleted))
	}
}

func TestHappyPathScannedPDF_OCRWarnings(t *testing.T) {
	deps := newTestDeps()

	// Fetcher returns scanned PDF (IsTextPDF=false).
	deps.fetcher.result = &port.FetchResult{
		StorageKey: "job-1/source.pdf",
		PageCount:  3,
		IsTextPDF:  false,
		FileSize:   2048,
	}
	// OCR service returns short text (<50 runes) to trigger OCR_LOW_QUALITY warning.
	deps.ocrService.result = "Договор поставки товаров"

	orch := deps.buildOrchestrator()
	cmd := defaultCmd()

	err := orch.HandleProcessDocument(context.Background(), cmd)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	// Verify terminal status is COMPLETED_WITH_WARNINGS (OCR_LOW_QUALITY captured).
	if len(deps.publisher.statusChanged) != 2 {
		t.Fatalf("expected 2 StatusChangedEvents, got %d", len(deps.publisher.statusChanged))
	}
	second := deps.publisher.statusChanged[1]
	if second.NewStatus != model.StatusCompletedWithWarnings {
		t.Errorf("expected COMPLETED_WITH_WARNINGS, got %s", second.NewStatus)
	}

	// Verify ProcessingCompletedEvent reflects the OCR warning.
	if len(deps.publisher.processingCompleted) != 1 {
		t.Fatalf("expected 1 ProcessingCompletedEvent, got %d", len(deps.publisher.processingCompleted))
	}
	completed := deps.publisher.processingCompleted[0]
	if completed.Status != model.StatusCompletedWithWarnings {
		t.Errorf("expected status COMPLETED_WITH_WARNINGS, got %s", completed.Status)
	}
	if !completed.HasWarnings {
		t.Error("expected HasWarnings=true")
	}
	if completed.WarningCount != 1 {
		t.Errorf("expected WarningCount=1 (OCR_LOW_QUALITY), got %d", completed.WarningCount)
	}

	// Verify the OCR warning is included in artifacts sent to DM.
	if len(deps.dmSender.sentArtifacts) != 1 {
		t.Fatalf("expected 1 SendArtifacts call, got %d", len(deps.dmSender.sentArtifacts))
	}
	artifactWarnings := deps.dmSender.sentArtifacts[0].Warnings
	if len(artifactWarnings) != 1 {
		t.Fatalf("expected 1 warning in artifacts, got %d", len(artifactWarnings))
	}
	if artifactWarnings[0].Code != "OCR_LOW_QUALITY" {
		t.Errorf("expected warning code OCR_LOW_QUALITY, got %s", artifactWarnings[0].Code)
	}
	if artifactWarnings[0].Stage != model.ProcessingStageOCR {
		t.Errorf("expected warning stage %s, got %s", model.ProcessingStageOCR, artifactWarnings[0].Stage)
	}
}

// --- Tests: Existing Error Cases (updated to verify failure event, terminal status, cleanup) ---

func TestValidationError_ReturnsError(t *testing.T) {
	deps := newTestDeps()
	deps.validator.err = port.NewValidationError("document_id is required")

	orch := deps.buildOrchestrator()
	cmd := defaultCmd()

	err := orch.HandleProcessDocument(context.Background(), cmd)
	if err == nil {
		t.Fatal("expected validation error, got nil")
	}

	var domErr *port.DomainError
	if !errors.As(err, &domErr) {
		t.Fatalf("expected DomainError, got %T: %v", err, err)
	}
	if domErr.Code != port.ErrCodeValidation {
		t.Errorf("expected error code %s, got %s", port.ErrCodeValidation, domErr.Code)
	}

	// Verify the lifecycle transition happened: QUEUED -> IN_PROGRESS, then IN_PROGRESS -> REJECTED.
	if len(deps.publisher.statusChanged) != 2 {
		t.Fatalf("expected 2 StatusChangedEvents (QUEUED->IN_PROGRESS + IN_PROGRESS->REJECTED), got %d", len(deps.publisher.statusChanged))
	}
	if deps.publisher.statusChanged[1].NewStatus != model.StatusRejected {
		t.Errorf("expected terminal status REJECTED, got %s", deps.publisher.statusChanged[1].NewStatus)
	}

	// No completion event should be published.
	if len(deps.publisher.processingCompleted) != 0 {
		t.Error("no ProcessingCompletedEvent should be published on validation error")
	}

	// Verify ProcessingFailedEvent.
	if len(deps.publisher.processingFailed) != 1 {
		t.Fatalf("expected 1 ProcessingFailedEvent, got %d", len(deps.publisher.processingFailed))
	}
	failed := deps.publisher.processingFailed[0]
	if failed.Status != model.StatusRejected {
		t.Errorf("expected REJECTED in failed event, got %s", failed.Status)
	}
	if failed.IsRetryable {
		t.Error("expected IsRetryable=false for validation error")
	}

	// No artifacts sent to DM.
	if len(deps.dmSender.sentArtifacts) != 0 {
		t.Error("no artifacts should be sent to DM on validation error")
	}

	// Verify cleanup was called.
	if len(deps.tempStorage.deletedPrefixes) < 1 {
		t.Error("expected cleanup to be called on error path")
	}
}

func TestFetchError_ReturnsError(t *testing.T) {
	deps := newTestDeps()
	deps.fetcher.result = nil
	deps.fetcher.err = port.NewFileNotFoundError("file not found", errors.New("404"))

	orch := deps.buildOrchestrator()
	cmd := defaultCmd()

	err := orch.HandleProcessDocument(context.Background(), cmd)
	if err == nil {
		t.Fatal("expected fetch error, got nil")
	}

	var domErr *port.DomainError
	if !errors.As(err, &domErr) {
		t.Fatalf("expected DomainError, got %T: %v", err, err)
	}
	if domErr.Code != port.ErrCodeFileNotFound {
		t.Errorf("expected error code %s, got %s", port.ErrCodeFileNotFound, domErr.Code)
	}

	// Verify ProcessingFailedEvent was published.
	if len(deps.publisher.processingFailed) != 1 {
		t.Fatalf("expected 1 ProcessingFailedEvent, got %d", len(deps.publisher.processingFailed))
	}
	if deps.publisher.processingFailed[0].Status != model.StatusRejected {
		t.Errorf("expected REJECTED for FILE_NOT_FOUND, got %s", deps.publisher.processingFailed[0].Status)
	}

	// Verify cleanup was called.
	if len(deps.tempStorage.deletedPrefixes) < 1 {
		t.Error("expected cleanup to be called on error path")
	}
}

func TestTextExtractionError_ReturnsError(t *testing.T) {
	deps := newTestDeps()
	deps.textExtractor.text = nil
	deps.textExtractor.err = port.NewExtractionError("extraction failed", errors.New("parse error"))

	orch := deps.buildOrchestrator()
	cmd := defaultCmd()

	err := orch.HandleProcessDocument(context.Background(), cmd)
	if err == nil {
		t.Fatal("expected extraction error, got nil")
	}

	var domErr *port.DomainError
	if !errors.As(err, &domErr) {
		t.Fatalf("expected DomainError, got %T: %v", err, err)
	}
	if domErr.Code != port.ErrCodeExtractionFailed {
		t.Errorf("expected error code %s, got %s", port.ErrCodeExtractionFailed, domErr.Code)
	}

	// Verify ProcessingFailedEvent was published with FAILED status.
	if len(deps.publisher.processingFailed) != 1 {
		t.Fatalf("expected 1 ProcessingFailedEvent, got %d", len(deps.publisher.processingFailed))
	}
	if deps.publisher.processingFailed[0].Status != model.StatusFailed {
		t.Errorf("expected FAILED for EXTRACTION_FAILED, got %s", deps.publisher.processingFailed[0].Status)
	}

	// Verify cleanup was called.
	if len(deps.tempStorage.deletedPrefixes) < 1 {
		t.Error("expected cleanup to be called on error path")
	}
}

func TestStructureExtractionError_ReturnsError(t *testing.T) {
	deps := newTestDeps()
	deps.structExtract.structure = nil
	deps.structExtract.err = port.NewExtractionError("structure extraction failed", errors.New("regex error"))

	orch := deps.buildOrchestrator()
	cmd := defaultCmd()

	err := orch.HandleProcessDocument(context.Background(), cmd)
	if err == nil {
		t.Fatal("expected structure extraction error, got nil")
	}

	var domErr *port.DomainError
	if !errors.As(err, &domErr) {
		t.Fatalf("expected DomainError, got %T: %v", err, err)
	}
	if domErr.Code != port.ErrCodeExtractionFailed {
		t.Errorf("expected error code %s, got %s", port.ErrCodeExtractionFailed, domErr.Code)
	}

	// Verify ProcessingFailedEvent was published with FAILED status.
	if len(deps.publisher.processingFailed) != 1 {
		t.Fatalf("expected 1 ProcessingFailedEvent, got %d", len(deps.publisher.processingFailed))
	}
	if deps.publisher.processingFailed[0].Status != model.StatusFailed {
		t.Errorf("expected FAILED, got %s", deps.publisher.processingFailed[0].Status)
	}
}

func TestSemanticTreeBuildError_ReturnsError(t *testing.T) {
	deps := newTestDeps()
	deps.treeBuilder.tree = nil
	deps.treeBuilder.err = port.NewExtractionError("tree build failed", errors.New("nil structure"))

	orch := deps.buildOrchestrator()
	cmd := defaultCmd()

	err := orch.HandleProcessDocument(context.Background(), cmd)
	if err == nil {
		t.Fatal("expected tree build error, got nil")
	}

	var domErr *port.DomainError
	if !errors.As(err, &domErr) {
		t.Fatalf("expected DomainError, got %T: %v", err, err)
	}

	// Verify ProcessingFailedEvent was published.
	if len(deps.publisher.processingFailed) != 1 {
		t.Fatalf("expected 1 ProcessingFailedEvent, got %d", len(deps.publisher.processingFailed))
	}
}

func TestDMSendError_ReturnsError(t *testing.T) {
	deps := newTestDeps()
	deps.dmSender.err = port.NewBrokerError("broker unavailable", errors.New("connection refused"))

	orch := deps.buildOrchestrator()
	cmd := defaultCmd()

	err := orch.HandleProcessDocument(context.Background(), cmd)
	if err == nil {
		t.Fatal("expected DM send error, got nil")
	}

	var domErr *port.DomainError
	if !errors.As(err, &domErr) {
		t.Fatalf("expected DomainError, got %T: %v", err, err)
	}
	if domErr.Code != port.ErrCodeBrokerFailed {
		t.Errorf("expected error code %s, got %s", port.ErrCodeBrokerFailed, domErr.Code)
	}

	// No completion event should be published.
	if len(deps.publisher.processingCompleted) != 0 {
		t.Error("no ProcessingCompletedEvent should be published on DM send error")
	}

	// Verify ProcessingFailedEvent was published with FAILED status.
	if len(deps.publisher.processingFailed) != 1 {
		t.Fatalf("expected 1 ProcessingFailedEvent, got %d", len(deps.publisher.processingFailed))
	}
	if deps.publisher.processingFailed[0].Status != model.StatusFailed {
		t.Errorf("expected FAILED for BROKER_FAILED, got %s", deps.publisher.processingFailed[0].Status)
	}

	// Verify cleanup was called.
	if len(deps.tempStorage.deletedPrefixes) < 1 {
		t.Error("expected cleanup to be called on error path")
	}
}

func TestCleanupError_BestEffort(t *testing.T) {
	deps := newTestDeps()
	deps.tempStorage.deleteErr = port.NewStorageError("cleanup failed", errors.New("s3 error"))

	orch := deps.buildOrchestrator()
	cmd := defaultCmd()

	// Cleanup errors are best-effort: artifacts have already been sent to DM,
	// so the pipeline should complete successfully despite the cleanup failure.
	err := orch.HandleProcessDocument(context.Background(), cmd)
	if err != nil {
		t.Fatalf("expected no error (best-effort cleanup), got: %v", err)
	}

	// Verify the pipeline still completed.
	if len(deps.publisher.processingCompleted) != 1 {
		t.Fatalf("expected 1 ProcessingCompletedEvent, got %d", len(deps.publisher.processingCompleted))
	}
	if deps.publisher.processingCompleted[0].Status != model.StatusCompleted {
		t.Errorf("expected COMPLETED, got %s", deps.publisher.processingCompleted[0].Status)
	}
}

func TestPublishCompletedError_ReturnsError(t *testing.T) {
	deps := newTestDeps()

	// We need the publisher to fail only on PublishProcessingCompleted, not on
	// PublishStatusChanged. To achieve this we use a specialized publisher mock.
	specialPub := &completionFailPublisher{}
	ocrAdapter := ocr.NewAdapter(deps.ocrService, deps.tempStorage, 10, 1, time.Second)
	lm := lifecycle.NewLifecycleManager(specialPub, deps.idempotency, 120*time.Second, nil, nopLogger())
	orch := NewOrchestrator(
		lm,
		deps.validator,
		deps.fetcher,
		ocrAdapter,
		deps.textExtractor,
		deps.structExtract,
		deps.treeBuilder,
		deps.tempStorage,
		specialPub,
		deps.dmSender,
		&mockDMAwaiter{},
		&mockDLQ{},
		nopLogger(),
		1,
		time.Millisecond,
	)

	cmd := defaultCmd()
	err := orch.HandleProcessDocument(context.Background(), cmd)
	if err == nil {
		t.Fatal("expected publish completed error, got nil")
	}

	if !errors.Is(err, errCompletionPublish) {
		t.Errorf("expected errCompletionPublish, got: %v", err)
	}
}

var errCompletionPublish = errors.New("completion publish failed")

// completionFailPublisher fails only on PublishProcessingCompleted.
type completionFailPublisher struct {
	statusChanged       []model.StatusChangedEvent
	processingCompleted []model.ProcessingCompletedEvent
}

func (p *completionFailPublisher) PublishStatusChanged(_ context.Context, event model.StatusChangedEvent) error {
	p.statusChanged = append(p.statusChanged, event)
	return nil
}

func (p *completionFailPublisher) PublishProcessingCompleted(_ context.Context, _ model.ProcessingCompletedEvent) error {
	return errCompletionPublish
}

func (p *completionFailPublisher) PublishProcessingFailed(_ context.Context, _ model.ProcessingFailedEvent) error {
	return nil
}

func (p *completionFailPublisher) PublishComparisonCompleted(_ context.Context, _ model.ComparisonCompletedEvent) error {
	return nil
}

func (p *completionFailPublisher) PublishComparisonFailed(_ context.Context, _ model.ComparisonFailedEvent) error {
	return nil
}

func TestArtifactsEventContent(t *testing.T) {
	deps := newTestDeps()
	orch := deps.buildOrchestrator()
	cmd := defaultCmd()

	before := time.Now().UTC()
	err := orch.HandleProcessDocument(context.Background(), cmd)
	after := time.Now().UTC()
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if len(deps.dmSender.sentArtifacts) != 1 {
		t.Fatalf("expected 1 artifacts event, got %d", len(deps.dmSender.sentArtifacts))
	}

	a := deps.dmSender.sentArtifacts[0]

	if a.CorrelationID != "job-1" {
		t.Errorf("CorrelationID: expected job-1, got %s", a.CorrelationID)
	}
	if a.Timestamp.Before(before) || a.Timestamp.After(after) {
		t.Errorf("timestamp %v outside expected range [%v, %v]", a.Timestamp, before, after)
	}

	// Verify all artifact fields are populated.
	if a.Text.DocumentID != "doc-1" {
		t.Errorf("Text.DocumentID: expected doc-1, got %s", a.Text.DocumentID)
	}
	if len(a.Text.Pages) != 2 {
		t.Errorf("Text.Pages: expected 2, got %d", len(a.Text.Pages))
	}
	if a.Structure.DocumentID != "doc-1" {
		t.Errorf("Structure.DocumentID: expected doc-1, got %s", a.Structure.DocumentID)
	}
	if len(a.Structure.Sections) != 1 {
		t.Errorf("Structure.Sections: expected 1, got %d", len(a.Structure.Sections))
	}
	if a.SemanticTree.Root == nil {
		t.Error("SemanticTree.Root should not be nil")
	}

	// No warnings in artifact for clean run.
	if len(a.Warnings) != 0 {
		t.Errorf("expected 0 Warnings for no-warning path, got %v", a.Warnings)
	}
}

func TestCommandFieldsCopiedToJob(t *testing.T) {
	// This test verifies that command metadata is propagated to the job.
	// We intercept the StatusChangedEvent to check the correlation ID matches.
	deps := newTestDeps()
	orch := deps.buildOrchestrator()

	cmd := model.ProcessDocumentCommand{
		JobID:      "job-42",
		DocumentID: "doc-99",
		FileURL:    "https://example.com/test.pdf",
		FileName:   "test.pdf",
		FileSize:   512,
		MimeType:   "application/pdf",
		Checksum:   "abc123",
		OrgID:      "org-5",
		UserID:     "user-7",
	}

	// Adjust fetcher to return matching storage key.
	deps.fetcher.result = &port.FetchResult{
		StorageKey: "job-42/source.pdf",
		PageCount:  1,
		IsTextPDF:  true,
		FileSize:   512,
	}

	err := orch.HandleProcessDocument(context.Background(), cmd)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	// Verify job ID propagated to status events.
	if len(deps.publisher.statusChanged) < 1 {
		t.Fatal("expected at least 1 StatusChangedEvent")
	}
	if deps.publisher.statusChanged[0].JobID != "job-42" {
		t.Errorf("expected JobID job-42, got %s", deps.publisher.statusChanged[0].JobID)
	}
	if deps.publisher.statusChanged[0].DocumentID != "doc-99" {
		t.Errorf("expected DocumentID doc-99, got %s", deps.publisher.statusChanged[0].DocumentID)
	}

	// Verify cleanup uses the correct job ID prefix.
	if len(deps.tempStorage.deletedPrefixes) != 1 || deps.tempStorage.deletedPrefixes[0] != "job-42" {
		t.Errorf("expected cleanup prefix job-42, got %v", deps.tempStorage.deletedPrefixes)
	}
}

func TestNewOrchestrator_PanicsOnNilDeps(t *testing.T) {
	pub := &mockPublisher{}
	idem := &mockIdempotency{}
	lm := lifecycle.NewLifecycleManager(pub, idem, 120*time.Second, nil, nopLogger())
	ocrAdapter := ocr.NewAdapter(&mockOCRService{}, &mockTempStorage{}, 10, 1, time.Second)

	validArgs := []interface{}{
		lm,
		&mockValidator{},
		&mockFetcher{result: defaultFetchResult()},
		ocrAdapter,
		&mockTextExtractor{text: defaultExtractedText()},
		&mockStructureExtractor{structure: defaultStructure()},
		&mockTreeBuilder{tree: defaultSemanticTree()},
		&mockTempStorage{},
		pub,
		&mockDMSender{},
		&mockDMAwaiter{},
		&mockDLQ{},
		nopLogger(),
	}

	// Test that passing nil for each dependency panics.
	depNames := []string{
		"lifecycle", "validator", "fetcher", "ocrProcessor",
		"textExtract", "structExtract", "treeBuilder",
		"tempStorage", "publisher", "dmSender", "dmAwaiter", "dlq", "logger",
	}

	for i, name := range depNames {
		t.Run("nil_"+name, func(t *testing.T) {
			defer func() {
				if r := recover(); r == nil {
					t.Errorf("expected panic for nil %s", name)
				}
			}()

			// Build args with nil at position i.
			args := make([]interface{}, len(validArgs))
			copy(args, validArgs)
			args[i] = nil

			NewOrchestrator(
				asLifecycle(args[0]),
				asValidator(args[1]),
				asFetcher(args[2]),
				asOCRProcessor(args[3]),
				asTextExtractor(args[4]),
				asStructExtractor(args[5]),
				asTreeBuilder(args[6]),
				asTempStorage(args[7]),
				asPublisher(args[8]),
				asDMSender(args[9]),
				asDMAwaiter(args[10]),
				asDLQ(args[11]),
				asLogger(args[12]),
				1,
				time.Millisecond,
			)
		})
	}
}

// Type assertion helpers for the nil-panic test.
func asLifecycle(v interface{}) *lifecycle.LifecycleManager {
	if v == nil {
		return nil
	}
	return v.(*lifecycle.LifecycleManager)
}

func asValidator(v interface{}) port.InputValidatorPort {
	if v == nil {
		return nil
	}
	return v.(port.InputValidatorPort)
}

func asFetcher(v interface{}) port.SourceFileFetcherPort {
	if v == nil {
		return nil
	}
	return v.(port.SourceFileFetcherPort)
}

func asOCRProcessor(v interface{}) port.OCRProcessorPort {
	if v == nil {
		return nil
	}
	return v.(port.OCRProcessorPort)
}

func asTextExtractor(v interface{}) port.TextExtractionPort {
	if v == nil {
		return nil
	}
	return v.(port.TextExtractionPort)
}

func asStructExtractor(v interface{}) port.StructureExtractionPort {
	if v == nil {
		return nil
	}
	return v.(port.StructureExtractionPort)
}

func asTreeBuilder(v interface{}) port.SemanticTreeBuilderPort {
	if v == nil {
		return nil
	}
	return v.(port.SemanticTreeBuilderPort)
}

func asTempStorage(v interface{}) port.TempStoragePort {
	if v == nil {
		return nil
	}
	return v.(port.TempStoragePort)
}

func asPublisher(v interface{}) port.EventPublisherPort {
	if v == nil {
		return nil
	}
	return v.(port.EventPublisherPort)
}

func asDMSender(v interface{}) port.DMArtifactSenderPort {
	if v == nil {
		return nil
	}
	return v.(port.DMArtifactSenderPort)
}

func asDMAwaiter(v interface{}) port.DMConfirmationAwaiterPort {
	if v == nil {
		return nil
	}
	return v.(port.DMConfirmationAwaiterPort)
}

func asDLQ(v interface{}) port.DLQPort {
	if v == nil {
		return nil
	}
	return v.(port.DLQPort)
}

func asLogger(v interface{}) *observability.Logger {
	if v == nil {
		return nil
	}
	return v.(*observability.Logger)
}

func TestOCRError_ReturnsError(t *testing.T) {
	deps := newTestDeps()

	// Set up scanned PDF so OCR adapter actually runs.
	deps.fetcher.result = &port.FetchResult{
		StorageKey: "job-1/source.pdf",
		PageCount:  3,
		IsTextPDF:  false,
		FileSize:   2048,
	}
	// OCR service returns a retryable error.
	deps.ocrService.err = port.NewOCRError("OCR service unavailable", true, errors.New("503"))

	orch := deps.buildOrchestrator()
	cmd := defaultCmd()

	err := orch.HandleProcessDocument(context.Background(), cmd)
	if err == nil {
		t.Fatal("expected OCR error, got nil")
	}

	var domErr *port.DomainError
	if !errors.As(err, &domErr) {
		t.Fatalf("expected DomainError, got %T: %v", err, err)
	}

	// No completion event.
	if len(deps.publisher.processingCompleted) != 0 {
		t.Error("no ProcessingCompletedEvent should be published on OCR error")
	}

	// Verify ProcessingFailedEvent was published.
	if len(deps.publisher.processingFailed) != 1 {
		t.Fatalf("expected 1 ProcessingFailedEvent, got %d", len(deps.publisher.processingFailed))
	}
}

func TestContextCancellation_ReturnsError(t *testing.T) {
	deps := newTestDeps()

	// Use a context-aware validator that checks ctx.Err() before proceeding.
	deps.validator = &mockValidator{err: nil}

	// Build orchestrator with a very short job timeout so the context expires
	// during the pipeline. We simulate this by providing a validator that
	// respects context cancellation.
	ocrAdapter := ocr.NewAdapter(deps.ocrService, deps.tempStorage, 10, 1, time.Second)
	lm := lifecycle.NewLifecycleManager(deps.publisher, deps.idempotency, 120*time.Second, nil, nopLogger())

	// Use a fetcher that checks context and returns cancel error.
	cancelFetcher := &contextAwareFetcher{result: defaultFetchResult()}
	orch := NewOrchestrator(
		lm,
		deps.validator,
		cancelFetcher,
		ocrAdapter,
		deps.textExtractor,
		deps.structExtract,
		deps.treeBuilder,
		deps.tempStorage,
		deps.publisher,
		deps.dmSender,
		&mockDMAwaiter{},
		&mockDLQ{},
		nopLogger(),
		1,
		time.Millisecond,
	)
	cmd := defaultCmd()

	// Already-cancelled context simulates job timeout firing.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := orch.HandleProcessDocument(ctx, cmd)
	if err == nil {
		t.Fatal("expected context cancellation error, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got: %v", err)
	}

	// The pipeline should exit early without completing.
	if len(deps.publisher.processingCompleted) != 0 {
		t.Error("no ProcessingCompletedEvent should be published on cancelled context")
	}
}

// contextAwareFetcher checks context cancellation before returning.
type contextAwareFetcher struct {
	result *port.FetchResult
}

func (f *contextAwareFetcher) Fetch(ctx context.Context, _ model.ProcessDocumentCommand) (*port.FetchResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return f.result, nil
}

func TestPipelineStageProgression(t *testing.T) {
	// Verify that stages are set correctly during pipeline progression.
	// We capture the stage from StatusChangedEvents.
	deps := newTestDeps()
	orch := deps.buildOrchestrator()
	cmd := defaultCmd()

	err := orch.HandleProcessDocument(context.Background(), cmd)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	// First transition sets stage to VALIDATING_INPUT (before the transition).
	if len(deps.publisher.statusChanged) < 1 {
		t.Fatal("expected at least 1 StatusChangedEvent")
	}
	firstStage := deps.publisher.statusChanged[0].Stage
	if firstStage != string(model.ProcessingStageValidatingInput) {
		t.Errorf("first event stage: expected %s, got %s", model.ProcessingStageValidatingInput, firstStage)
	}

	// The final transition should have CLEANUP stage (set before the transition call).
	lastStage := deps.publisher.statusChanged[len(deps.publisher.statusChanged)-1].Stage
	if lastStage != string(model.ProcessingStageCleanup) {
		t.Errorf("last event stage: expected %s, got %s", model.ProcessingStageCleanup, lastStage)
	}
}

// --- Tests: REJECTED status ---

func TestRejected_ValidationError(t *testing.T) {
	deps := newTestDeps()
	deps.validator.err = port.NewValidationError("missing field")

	orch := deps.buildOrchestrator()
	cmd := defaultCmd()

	err := orch.HandleProcessDocument(context.Background(), cmd)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	// Verify terminal status is REJECTED.
	found := false
	for _, sc := range deps.publisher.statusChanged {
		if sc.NewStatus == model.StatusRejected {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected REJECTED status transition")
	}

	// Verify ProcessingFailedEvent.
	if len(deps.publisher.processingFailed) != 1 {
		t.Fatalf("expected 1 ProcessingFailedEvent, got %d", len(deps.publisher.processingFailed))
	}
	failed := deps.publisher.processingFailed[0]
	if failed.Status != model.StatusRejected {
		t.Errorf("expected REJECTED, got %s", failed.Status)
	}
	if failed.IsRetryable {
		t.Error("expected IsRetryable=false for REJECTED")
	}
	if failed.ErrorCode != port.ErrCodeValidation {
		t.Errorf("expected error code %s, got %s", port.ErrCodeValidation, failed.ErrorCode)
	}
}

func TestRejected_FileTooLarge(t *testing.T) {
	deps := newTestDeps()
	deps.fetcher.result = nil
	deps.fetcher.err = port.NewFileTooLargeError("file exceeds 20 MB")

	orch := deps.buildOrchestrator()
	cmd := defaultCmd()

	err := orch.HandleProcessDocument(context.Background(), cmd)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	if len(deps.publisher.processingFailed) != 1 {
		t.Fatalf("expected 1 ProcessingFailedEvent, got %d", len(deps.publisher.processingFailed))
	}
	if deps.publisher.processingFailed[0].Status != model.StatusRejected {
		t.Errorf("expected REJECTED, got %s", deps.publisher.processingFailed[0].Status)
	}
}

func TestRejected_InvalidFormat(t *testing.T) {
	deps := newTestDeps()
	deps.fetcher.result = nil
	deps.fetcher.err = port.NewInvalidFormatError("not a PDF")

	orch := deps.buildOrchestrator()
	cmd := defaultCmd()

	err := orch.HandleProcessDocument(context.Background(), cmd)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	if len(deps.publisher.processingFailed) != 1 {
		t.Fatalf("expected 1 ProcessingFailedEvent, got %d", len(deps.publisher.processingFailed))
	}
	if deps.publisher.processingFailed[0].Status != model.StatusRejected {
		t.Errorf("expected REJECTED, got %s", deps.publisher.processingFailed[0].Status)
	}
}

func TestRejected_TooManyPages(t *testing.T) {
	deps := newTestDeps()
	deps.fetcher.result = nil
	deps.fetcher.err = port.NewTooManyPagesError("exceeds 100 pages")

	orch := deps.buildOrchestrator()
	cmd := defaultCmd()

	err := orch.HandleProcessDocument(context.Background(), cmd)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	if len(deps.publisher.processingFailed) != 1 {
		t.Fatalf("expected 1 ProcessingFailedEvent, got %d", len(deps.publisher.processingFailed))
	}
	if deps.publisher.processingFailed[0].Status != model.StatusRejected {
		t.Errorf("expected REJECTED, got %s", deps.publisher.processingFailed[0].Status)
	}
}

func TestRejected_FileNotFound(t *testing.T) {
	deps := newTestDeps()
	deps.fetcher.result = nil
	deps.fetcher.err = port.NewFileNotFoundError("file not found", errors.New("404"))

	orch := deps.buildOrchestrator()
	cmd := defaultCmd()

	err := orch.HandleProcessDocument(context.Background(), cmd)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	if len(deps.publisher.processingFailed) != 1 {
		t.Fatalf("expected 1 ProcessingFailedEvent, got %d", len(deps.publisher.processingFailed))
	}
	if deps.publisher.processingFailed[0].Status != model.StatusRejected {
		t.Errorf("expected REJECTED, got %s", deps.publisher.processingFailed[0].Status)
	}
}

// --- Tests: Retry logic ---

func TestRetry_OCRRetryableError_ThenSuccess(t *testing.T) {
	deps := newTestDeps()
	// Configure scanned PDF so OCR actually runs.
	deps.fetcher.result = scannedFetchResult()

	ocrProc := &callCountOCRProcessor{
		failUntil: 1,
		err:       port.NewOCRError("OCR rate limit", true, errors.New("429")),
		successResult: &model.OCRRawArtifact{
			Status:  model.OCRStatusApplicable,
			RawText: "Договор поставки товаров между ООО Ромашка и ООО Василёк от 01.01.2026",
		},
	}

	orch := deps.buildOrchestratorWithOCR(ocrProc, 3, time.Millisecond)
	cmd := defaultCmd()

	err := orch.HandleProcessDocument(context.Background(), cmd)
	if err != nil {
		t.Fatalf("expected no error after retry success, got: %v", err)
	}

	if ocrProc.callCount != 2 {
		t.Errorf("expected 2 OCR calls (1 fail + 1 success), got %d", ocrProc.callCount)
	}

	// Verify pipeline completed.
	if len(deps.publisher.processingCompleted) != 1 {
		t.Fatalf("expected 1 ProcessingCompletedEvent, got %d", len(deps.publisher.processingCompleted))
	}
}

func TestRetry_ExhaustedRetries_Failed(t *testing.T) {
	deps := newTestDeps()
	deps.fetcher.result = scannedFetchResult()

	ocrProc := &callCountOCRProcessor{
		failUntil: 100, // always fail
		err:       port.NewOCRError("OCR rate limit", true, errors.New("429")),
	}

	orch := deps.buildOrchestratorWithOCR(ocrProc, 2, time.Millisecond)
	cmd := defaultCmd()

	err := orch.HandleProcessDocument(context.Background(), cmd)
	if err == nil {
		t.Fatal("expected error after exhausted retries, got nil")
	}

	if ocrProc.callCount != 2 {
		t.Errorf("expected 2 OCR calls (maxRetries=2), got %d", ocrProc.callCount)
	}

	// Verify terminal status is FAILED (not REJECTED or TIMED_OUT).
	if len(deps.publisher.processingFailed) != 1 {
		t.Fatalf("expected 1 ProcessingFailedEvent, got %d", len(deps.publisher.processingFailed))
	}
	if deps.publisher.processingFailed[0].Status != model.StatusFailed {
		t.Errorf("expected FAILED, got %s", deps.publisher.processingFailed[0].Status)
	}
}

// --- Tests: Timeout ---

func TestTimedOut_DeadlineExceeded(t *testing.T) {
	deps := newTestDeps()
	// Fetcher returns context.DeadlineExceeded.
	deps.fetcher.result = nil
	deps.fetcher.err = context.DeadlineExceeded

	orch := deps.buildOrchestrator()
	cmd := defaultCmd()

	err := orch.HandleProcessDocument(context.Background(), cmd)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("expected DeadlineExceeded, got: %v", err)
	}

	// Verify ProcessingFailedEvent with TIMED_OUT and is_retryable=true.
	if len(deps.publisher.processingFailed) != 1 {
		t.Fatalf("expected 1 ProcessingFailedEvent, got %d", len(deps.publisher.processingFailed))
	}
	failed := deps.publisher.processingFailed[0]
	if failed.Status != model.StatusTimedOut {
		t.Errorf("expected TIMED_OUT, got %s", failed.Status)
	}
	if !failed.IsRetryable {
		t.Error("expected IsRetryable=true for TIMED_OUT")
	}
}

// --- Tests: FAILED status ---

func TestFailed_NonRetryableError(t *testing.T) {
	deps := newTestDeps()
	deps.textExtractor.text = nil
	deps.textExtractor.err = port.NewExtractionError("extraction failed", errors.New("parse error"))

	orch := deps.buildOrchestrator()
	cmd := defaultCmd()

	err := orch.HandleProcessDocument(context.Background(), cmd)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	// Verify FAILED status.
	if len(deps.publisher.processingFailed) != 1 {
		t.Fatalf("expected 1 ProcessingFailedEvent, got %d", len(deps.publisher.processingFailed))
	}
	failed := deps.publisher.processingFailed[0]
	if failed.Status != model.StatusFailed {
		t.Errorf("expected FAILED, got %s", failed.Status)
	}
	if failed.IsRetryable {
		t.Error("expected IsRetryable=false for non-retryable error")
	}
}

func TestFailed_BrokerError_RetriesExhausted(t *testing.T) {
	deps := newTestDeps()

	dmSender := &callCountDMSender{
		err: port.NewBrokerError("broker unavailable", errors.New("connection refused")),
	}

	orch := deps.buildOrchestratorWithDMSender(dmSender, 2, time.Millisecond)
	cmd := defaultCmd()

	err := orch.HandleProcessDocument(context.Background(), cmd)
	if err == nil {
		t.Fatal("expected error after exhausted retries, got nil")
	}

	if dmSender.callCount != 2 {
		t.Errorf("expected 2 DM sender calls (maxRetries=2), got %d", dmSender.callCount)
	}

	// Verify terminal status is FAILED.
	if len(deps.publisher.processingFailed) != 1 {
		t.Fatalf("expected 1 ProcessingFailedEvent, got %d", len(deps.publisher.processingFailed))
	}
	if deps.publisher.processingFailed[0].Status != model.StatusFailed {
		t.Errorf("expected FAILED, got %s", deps.publisher.processingFailed[0].Status)
	}
}

// --- Tests: Cleanup on error paths ---

func TestCleanup_OnRejected(t *testing.T) {
	deps := newTestDeps()
	deps.validator.err = port.NewValidationError("bad input")

	orch := deps.buildOrchestrator()
	cmd := defaultCmd()

	_ = orch.HandleProcessDocument(context.Background(), cmd)

	// Verify cleanup was called.
	if len(deps.tempStorage.deletedPrefixes) < 1 {
		t.Error("expected cleanup to be called on REJECTED path")
	}
	found := false
	for _, p := range deps.tempStorage.deletedPrefixes {
		if p == "job-1" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected cleanup prefix job-1, got %v", deps.tempStorage.deletedPrefixes)
	}
}

func TestCleanup_OnFailed(t *testing.T) {
	deps := newTestDeps()
	deps.textExtractor.text = nil
	deps.textExtractor.err = port.NewExtractionError("failed", errors.New("err"))

	orch := deps.buildOrchestrator()
	cmd := defaultCmd()

	_ = orch.HandleProcessDocument(context.Background(), cmd)

	if len(deps.tempStorage.deletedPrefixes) < 1 {
		t.Error("expected cleanup to be called on FAILED path")
	}
}

func TestCleanup_OnTimedOut(t *testing.T) {
	deps := newTestDeps()
	deps.fetcher.result = nil
	deps.fetcher.err = context.DeadlineExceeded

	orch := deps.buildOrchestrator()
	cmd := defaultCmd()

	_ = orch.HandleProcessDocument(context.Background(), cmd)

	if len(deps.tempStorage.deletedPrefixes) < 1 {
		t.Error("expected cleanup to be called on TIMED_OUT path")
	}
}

// --- Tests: ProcessingFailedEvent field completeness ---

func TestProcessingFailedEvent_AllFieldsPopulated(t *testing.T) {
	deps := newTestDeps()
	deps.textExtractor.text = nil
	deps.textExtractor.err = port.NewExtractionError("extraction failed", errors.New("parse error"))

	orch := deps.buildOrchestrator()
	cmd := defaultCmd()

	before := time.Now().UTC()
	_ = orch.HandleProcessDocument(context.Background(), cmd)
	after := time.Now().UTC()

	if len(deps.publisher.processingFailed) != 1 {
		t.Fatalf("expected 1 ProcessingFailedEvent, got %d", len(deps.publisher.processingFailed))
	}
	f := deps.publisher.processingFailed[0]

	// 1. CorrelationID
	if f.CorrelationID != "job-1" {
		t.Errorf("CorrelationID: expected job-1, got %s", f.CorrelationID)
	}
	// 2. Timestamp
	if f.Timestamp.Before(before) || f.Timestamp.After(after) {
		t.Errorf("Timestamp %v outside range [%v, %v]", f.Timestamp, before, after)
	}
	// 3. JobID
	if f.JobID != "job-1" {
		t.Errorf("JobID: expected job-1, got %s", f.JobID)
	}
	// 4. DocumentID
	if f.DocumentID != "doc-1" {
		t.Errorf("DocumentID: expected doc-1, got %s", f.DocumentID)
	}
	// 5. Status
	if f.Status != model.StatusFailed {
		t.Errorf("Status: expected FAILED, got %s", f.Status)
	}
	// 6. ErrorCode
	if f.ErrorCode != port.ErrCodeExtractionFailed {
		t.Errorf("ErrorCode: expected %s, got %s", port.ErrCodeExtractionFailed, f.ErrorCode)
	}
	// 7. ErrorMessage
	if f.ErrorMessage == "" {
		t.Error("ErrorMessage should not be empty")
	}
	// 8. FailedAtStage
	if f.FailedAtStage != string(model.ProcessingStageTextExtraction) {
		t.Errorf("FailedAtStage: expected %s, got %s", model.ProcessingStageTextExtraction, f.FailedAtStage)
	}
	// is_retryable for non-retryable
	if f.IsRetryable {
		t.Error("expected IsRetryable=false for extraction error")
	}
}

// --- Tests: classifyError ---

func TestClassifyError_Table(t *testing.T) {
	tests := []struct {
		name            string
		err             error
		expectedStatus  model.JobStatus
		expectedRetryable bool
	}{
		{
			name:            "DeadlineExceeded",
			err:             context.DeadlineExceeded,
			expectedStatus:  model.StatusTimedOut,
			expectedRetryable: true,
		},
		{
			name:            "WrappedDeadlineExceeded",
			err:             port.NewTimeoutError("timed out", context.DeadlineExceeded),
			expectedStatus:  model.StatusTimedOut,
			expectedRetryable: true,
		},
		{
			name:            "ValidationError",
			err:             port.NewValidationError("bad input"),
			expectedStatus:  model.StatusRejected,
			expectedRetryable: false,
		},
		{
			name:            "FileTooLarge",
			err:             port.NewFileTooLargeError("too big"),
			expectedStatus:  model.StatusRejected,
			expectedRetryable: false,
		},
		{
			name:            "TooManyPages",
			err:             port.NewTooManyPagesError("too many"),
			expectedStatus:  model.StatusRejected,
			expectedRetryable: false,
		},
		{
			name:            "InvalidFormat",
			err:             port.NewInvalidFormatError("not pdf"),
			expectedStatus:  model.StatusRejected,
			expectedRetryable: false,
		},
		{
			name:            "FileNotFound",
			err:             port.NewFileNotFoundError("not found", errors.New("404")),
			expectedStatus:  model.StatusRejected,
			expectedRetryable: false,
		},
		{
			name:            "OCRError_Retryable",
			err:             port.NewOCRError("rate limit", true, errors.New("429")),
			expectedStatus:  model.StatusFailed,
			expectedRetryable: false,
		},
		{
			name:            "ExtractionError",
			err:             port.NewExtractionError("failed", errors.New("err")),
			expectedStatus:  model.StatusFailed,
			expectedRetryable: false,
		},
		{
			name:            "BrokerError",
			err:             port.NewBrokerError("broker down", errors.New("err")),
			expectedStatus:  model.StatusFailed,
			expectedRetryable: false,
		},
		{
			name:            "StorageError",
			err:             port.NewStorageError("s3 down", errors.New("err")),
			expectedStatus:  model.StatusFailed,
			expectedRetryable: false,
		},
		{
			name:            "PlainError",
			err:             errors.New("unknown error"),
			expectedStatus:  model.StatusFailed,
			expectedRetryable: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, retryable := classifyError(tt.err)
			if status != tt.expectedStatus {
				t.Errorf("expected status %s, got %s", tt.expectedStatus, status)
			}
			if retryable != tt.expectedRetryable {
				t.Errorf("expected retryable=%v, got %v", tt.expectedRetryable, retryable)
			}
		})
	}
}

// --- Tests: retryStep context cancellation ---

func TestRetryStep_RespectsContextCancellation(t *testing.T) {
	o := &Orchestrator{
		maxRetries:  5,
		backoffBase: time.Second, // Long backoff to ensure we detect cancellation.
	}

	ctx, cancel := context.WithCancel(context.Background())

	callCount := 0
	fn := func() error {
		callCount++
		if callCount == 1 {
			// Cancel context after first call — during backoff wait the context
			// should be detected as cancelled.
			cancel()
		}
		return port.NewOCRError("rate limit", true, errors.New("429"))
	}

	err := o.retryStep(ctx, fn)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got: %v", err)
	}

	// Should have been called exactly once before context cancellation
	// stopped the retry loop.
	if callCount != 1 {
		t.Errorf("expected 1 call before cancellation, got %d", callCount)
	}
}

// --- Tests: Concurrent warning isolation ---

// jobAwareTextExtractor returns different warnings based on the storage key
// (which contains the job ID). This enables testing that concurrent jobs
// receive only their own warnings.
type jobAwareTextExtractor struct {
	mu       sync.Mutex
	textByID map[string]*model.ExtractedText
	warnByID map[string][]model.ProcessingWarning
}

func (e *jobAwareTextExtractor) Extract(_ context.Context, storageKey string, _ *model.OCRRawArtifact) (*model.ExtractedText, []model.ProcessingWarning, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	text, ok := e.textByID[storageKey]
	if !ok {
		return nil, nil, port.NewExtractionError("unknown storage key: "+storageKey, nil)
	}
	return text, e.warnByID[storageKey], nil
}

// threadSafePublisher is a concurrent-safe mock publisher for multi-goroutine tests.
type threadSafePublisher struct {
	mu                  sync.Mutex
	statusChanged       []model.StatusChangedEvent
	processingCompleted []model.ProcessingCompletedEvent
	processingFailed    []model.ProcessingFailedEvent
}

func (p *threadSafePublisher) PublishStatusChanged(_ context.Context, event model.StatusChangedEvent) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.statusChanged = append(p.statusChanged, event)
	return nil
}

func (p *threadSafePublisher) PublishProcessingCompleted(_ context.Context, event model.ProcessingCompletedEvent) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.processingCompleted = append(p.processingCompleted, event)
	return nil
}

func (p *threadSafePublisher) PublishProcessingFailed(_ context.Context, event model.ProcessingFailedEvent) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.processingFailed = append(p.processingFailed, event)
	return nil
}

func (p *threadSafePublisher) PublishComparisonCompleted(_ context.Context, _ model.ComparisonCompletedEvent) error {
	return nil
}

func (p *threadSafePublisher) PublishComparisonFailed(_ context.Context, _ model.ComparisonFailedEvent) error {
	return nil
}

// threadSafeIdempotency is a concurrent-safe idempotency store.
type threadSafeIdempotency struct {
	mu    sync.Mutex
	store map[string]bool
}

func newThreadSafeIdempotency() *threadSafeIdempotency {
	return &threadSafeIdempotency{store: make(map[string]bool)}
}

func (m *threadSafeIdempotency) Check(_ context.Context, _ string) (port.IdempotencyStatus, error) {
	return port.IdempotencyStatusNew, nil
}

func (m *threadSafeIdempotency) Register(_ context.Context, _ string) error {
	return nil
}

func (m *threadSafeIdempotency) MarkCompleted(_ context.Context, jobID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.store[jobID] = true
	return nil
}

// threadSafeDMSender is a concurrent-safe DM sender mock.
type threadSafeDMSender struct {
	mu        sync.Mutex
	artifacts []model.DocumentProcessingArtifactsReady
}

func (m *threadSafeDMSender) SendArtifacts(_ context.Context, event model.DocumentProcessingArtifactsReady) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.artifacts = append(m.artifacts, event)
	return nil
}

func (m *threadSafeDMSender) SendDiffResult(_ context.Context, _ model.DocumentVersionDiffReady) error {
	return nil
}

// threadSafeTempStorage is a concurrent-safe temp storage mock.
type threadSafeTempStorage struct {
	mu sync.Mutex
}

func (m *threadSafeTempStorage) Upload(_ context.Context, _ string, _ io.Reader) error {
	return nil
}

func (m *threadSafeTempStorage) Download(_ context.Context, _ string) (io.ReadCloser, error) {
	return io.NopCloser(io.LimitReader(nil, 0)), nil
}

func (m *threadSafeTempStorage) Delete(_ context.Context, _ string) error {
	return nil
}

func (m *threadSafeTempStorage) DeleteByPrefix(_ context.Context, _ string) error {
	return nil
}

func TestConcurrentJobs_WarningIsolation(t *testing.T) {
	// Two concurrent jobs processed by a single Orchestrator should have
	// completely isolated warnings: job A's warnings must not leak into job B's
	// artifacts or completion event, and vice versa.

	pub := &threadSafePublisher{}
	idem := newThreadSafeIdempotency()
	dmSender := &threadSafeDMSender{}
	tempStorage := &threadSafeTempStorage{}

	// Job-aware text extractor returns different warnings per job.
	textExtract := &jobAwareTextExtractor{
		textByID: map[string]*model.ExtractedText{
			"job-A/source.pdf": {DocumentID: "doc-A", Pages: []model.PageText{{PageNumber: 1, Text: "Текст А"}}},
			"job-B/source.pdf": {DocumentID: "doc-B", Pages: []model.PageText{{PageNumber: 1, Text: "Текст Б"}}},
		},
		warnByID: map[string][]model.ProcessingWarning{
			"job-A/source.pdf": {
				{Code: "WARN_A_1", Message: "warning for job A page 1", Stage: model.ProcessingStageTextExtraction},
				{Code: "WARN_A_2", Message: "warning for job A page 2", Stage: model.ProcessingStageTextExtraction},
			},
			"job-B/source.pdf": {
				{Code: "WARN_B_1", Message: "warning for job B", Stage: model.ProcessingStageTextExtraction},
			},
		},
	}

	// Job-aware fetcher: returns different storage keys per job.
	fetcherA := &mockFetcher{result: &port.FetchResult{StorageKey: "job-A/source.pdf", PageCount: 1, IsTextPDF: true, FileSize: 100}}
	fetcherB := &mockFetcher{result: &port.FetchResult{StorageKey: "job-B/source.pdf", PageCount: 1, IsTextPDF: true, FileSize: 100}}

	// We need a job-aware fetcher that routes based on the command.
	jobFetcher := &jobAwareFetcher{
		byJobID: map[string]*port.FetchResult{
			"job-A": fetcherA.result,
			"job-B": fetcherB.result,
		},
	}

	ocrAdapter := ocr.NewAdapter(&mockOCRService{}, tempStorage, 10, 1, time.Second)
	lm := lifecycle.NewLifecycleManager(pub, idem, 120*time.Second, nil, nopLogger())

	orch := NewOrchestrator(
		lm,
		&mockValidator{},
		jobFetcher,
		ocrAdapter,
		textExtract,
		&mockStructureExtractor{structure: defaultStructure()},
		&mockTreeBuilder{tree: defaultSemanticTree()},
		tempStorage,
		pub,
		dmSender,
		&mockDMAwaiter{},
		&mockDLQ{},
		nopLogger(),
		1,
		time.Millisecond,
	)

	cmdA := model.ProcessDocumentCommand{
		JobID: "job-A", DocumentID: "doc-A",
		FileURL: "https://example.com/a.pdf", FileName: "a.pdf",
		FileSize: 100, MimeType: "application/pdf",
		OrgID: "org-1", UserID: "user-1",
	}
	cmdB := model.ProcessDocumentCommand{
		JobID: "job-B", DocumentID: "doc-B",
		FileURL: "https://example.com/b.pdf", FileName: "b.pdf",
		FileSize: 100, MimeType: "application/pdf",
		OrgID: "org-1", UserID: "user-1",
	}

	var wg sync.WaitGroup
	errA := make(chan error, 1)
	errB := make(chan error, 1)

	wg.Add(2)
	go func() {
		defer wg.Done()
		errA <- orch.HandleProcessDocument(context.Background(), cmdA)
	}()
	go func() {
		defer wg.Done()
		errB <- orch.HandleProcessDocument(context.Background(), cmdB)
	}()
	wg.Wait()

	if err := <-errA; err != nil {
		t.Fatalf("job-A failed: %v", err)
	}
	if err := <-errB; err != nil {
		t.Fatalf("job-B failed: %v", err)
	}

	// Verify both jobs completed.
	pub.mu.Lock()
	completedEvents := make([]model.ProcessingCompletedEvent, len(pub.processingCompleted))
	copy(completedEvents, pub.processingCompleted)
	pub.mu.Unlock()

	if len(completedEvents) != 2 {
		t.Fatalf("expected 2 ProcessingCompletedEvent, got %d", len(completedEvents))
	}

	// Find events by job ID.
	var completedA, completedB *model.ProcessingCompletedEvent
	for i := range completedEvents {
		switch completedEvents[i].JobID {
		case "job-A":
			completedA = &completedEvents[i]
		case "job-B":
			completedB = &completedEvents[i]
		}
	}

	if completedA == nil {
		t.Fatal("missing ProcessingCompletedEvent for job-A")
	}
	if completedB == nil {
		t.Fatal("missing ProcessingCompletedEvent for job-B")
	}

	// Job A should have exactly 2 warnings.
	if completedA.WarningCount != 2 {
		t.Errorf("job-A: expected WarningCount=2, got %d", completedA.WarningCount)
	}
	if completedA.Status != model.StatusCompletedWithWarnings {
		t.Errorf("job-A: expected COMPLETED_WITH_WARNINGS, got %s", completedA.Status)
	}

	// Job B should have exactly 1 warning.
	if completedB.WarningCount != 1 {
		t.Errorf("job-B: expected WarningCount=1, got %d", completedB.WarningCount)
	}
	if completedB.Status != model.StatusCompletedWithWarnings {
		t.Errorf("job-B: expected COMPLETED_WITH_WARNINGS, got %s", completedB.Status)
	}

	// Verify artifacts contain ONLY the correct warnings.
	dmSender.mu.Lock()
	artifacts := make([]model.DocumentProcessingArtifactsReady, len(dmSender.artifacts))
	copy(artifacts, dmSender.artifacts)
	dmSender.mu.Unlock()

	if len(artifacts) != 2 {
		t.Fatalf("expected 2 artifact events, got %d", len(artifacts))
	}

	var artifactsA, artifactsB *model.DocumentProcessingArtifactsReady
	for i := range artifacts {
		switch artifacts[i].JobID {
		case "job-A":
			artifactsA = &artifacts[i]
		case "job-B":
			artifactsB = &artifacts[i]
		}
	}

	if artifactsA == nil {
		t.Fatal("missing artifacts for job-A")
	}
	if artifactsB == nil {
		t.Fatal("missing artifacts for job-B")
	}

	// Job A artifacts: exactly 2 warnings, both with "WARN_A_" prefix.
	if len(artifactsA.Warnings) != 2 {
		t.Fatalf("job-A artifacts: expected 2 warnings, got %d", len(artifactsA.Warnings))
	}
	for _, w := range artifactsA.Warnings {
		if w.Code != "WARN_A_1" && w.Code != "WARN_A_2" {
			t.Errorf("job-A artifacts: unexpected warning code %q (expected WARN_A_1 or WARN_A_2)", w.Code)
		}
	}

	// Job B artifacts: exactly 1 warning with code "WARN_B_1".
	if len(artifactsB.Warnings) != 1 {
		t.Fatalf("job-B artifacts: expected 1 warning, got %d", len(artifactsB.Warnings))
	}
	if artifactsB.Warnings[0].Code != "WARN_B_1" {
		t.Errorf("job-B artifacts: expected warning code WARN_B_1, got %q", artifactsB.Warnings[0].Code)
	}
}

// jobAwareFetcher routes fetch results based on the command's JobID.
type jobAwareFetcher struct {
	mu      sync.Mutex
	byJobID map[string]*port.FetchResult
}

func (f *jobAwareFetcher) Fetch(_ context.Context, cmd model.ProcessDocumentCommand) (*port.FetchResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	result, ok := f.byJobID[cmd.JobID]
	if !ok {
		return nil, port.NewFileNotFoundError("unknown job: "+cmd.JobID, nil)
	}
	return result, nil
}

// --- Tests: DM Confirmation ---

func TestDMConfirmation_HappyPath(t *testing.T) {
	deps := newTestDeps()
	orch := deps.buildOrchestrator()
	cmd := defaultCmd()

	err := orch.HandleProcessDocument(context.Background(), cmd)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	// Verify dmAwaiter.registered contains the jobID (Register is called
	// before sending artifacts, so it must have been recorded).
	if len(deps.dmAwaiter.registered) < 1 {
		t.Fatal("expected at least 1 registration in dmAwaiter")
	}
	found := false
	for _, id := range deps.dmAwaiter.registered {
		if id == "job-1" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected dmAwaiter.registered to contain job-1, got %v", deps.dmAwaiter.registered)
	}

	// Verify pipeline completed successfully.
	if len(deps.publisher.processingCompleted) != 1 {
		t.Fatalf("expected 1 ProcessingCompletedEvent, got %d", len(deps.publisher.processingCompleted))
	}
	if deps.publisher.processingCompleted[0].Status != model.StatusCompleted {
		t.Errorf("expected COMPLETED, got %s", deps.publisher.processingCompleted[0].Status)
	}

	// No failure events.
	if len(deps.publisher.processingFailed) != 0 {
		t.Errorf("expected 0 ProcessingFailedEvent, got %d", len(deps.publisher.processingFailed))
	}
}

func TestDMConfirmation_RetryableFailureThenSuccess(t *testing.T) {
	deps := newTestDeps()

	callCount := 0
	deps.dmAwaiter.awaitFunc = func(_ context.Context, jobID string) (port.DMConfirmationResult, error) {
		callCount++
		if callCount == 1 {
			// First call: DM reports a retryable failure.
			return port.DMConfirmationResult{
				JobID: jobID,
				Err:   port.NewDMArtifactsPersistFailedError("transient DM failure", true, nil),
			}, nil
		}
		// Second call: success.
		return port.DMConfirmationResult{JobID: jobID}, nil
	}

	orch := deps.buildOrchestratorWithRetry(3, time.Millisecond)
	cmd := defaultCmd()

	err := orch.HandleProcessDocument(context.Background(), cmd)
	if err != nil {
		t.Fatalf("expected no error after retry success, got: %v", err)
	}

	// Verify pipeline completed.
	if len(deps.publisher.processingCompleted) != 1 {
		t.Fatalf("expected 1 ProcessingCompletedEvent, got %d", len(deps.publisher.processingCompleted))
	}
	if deps.publisher.processingCompleted[0].Status != model.StatusCompleted {
		t.Errorf("expected COMPLETED, got %s", deps.publisher.processingCompleted[0].Status)
	}

	// Verify dmSender.sentArtifacts has 2 entries: original send + retry resend.
	if len(deps.dmSender.sentArtifacts) != 2 {
		t.Fatalf("expected 2 SendArtifacts calls (original + retry), got %d", len(deps.dmSender.sentArtifacts))
	}

	// Verify dmAwaiter.registered has 2 entries: original + retry.
	if len(deps.dmAwaiter.registered) != 2 {
		t.Fatalf("expected 2 Register calls (original + retry), got %d", len(deps.dmAwaiter.registered))
	}
	for _, id := range deps.dmAwaiter.registered {
		if id != "job-1" {
			t.Errorf("expected all registrations for job-1, got %s", id)
		}
	}
}

func TestDMConfirmation_NonRetryableFailure(t *testing.T) {
	deps := newTestDeps()

	deps.dmAwaiter.awaitFunc = func(_ context.Context, jobID string) (port.DMConfirmationResult, error) {
		return port.DMConfirmationResult{
			JobID: jobID,
			Err:   port.NewDMArtifactsPersistFailedError("permanent DM failure", false, nil),
		}, nil
	}

	orch := deps.buildOrchestrator()
	cmd := defaultCmd()

	err := orch.HandleProcessDocument(context.Background(), cmd)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	var domErr *port.DomainError
	if !errors.As(err, &domErr) {
		t.Fatalf("expected DomainError, got %T: %v", err, err)
	}
	if domErr.Code != port.ErrCodeDMArtifactsPersistFailed {
		t.Errorf("expected error code %s, got %s", port.ErrCodeDMArtifactsPersistFailed, domErr.Code)
	}

	// Verify pipeline failed with FAILED status.
	if len(deps.publisher.processingFailed) != 1 {
		t.Fatalf("expected 1 ProcessingFailedEvent, got %d", len(deps.publisher.processingFailed))
	}
	failed := deps.publisher.processingFailed[0]
	if failed.Status != model.StatusFailed {
		t.Errorf("expected FAILED, got %s", failed.Status)
	}
	if failed.ErrorCode != port.ErrCodeDMArtifactsPersistFailed {
		t.Errorf("expected error code %s, got %s", port.ErrCodeDMArtifactsPersistFailed, failed.ErrorCode)
	}

	// Verify stage is WAITING_DM_CONFIRMATION.
	if failed.FailedAtStage != string(model.ProcessingStageWaitingDM) {
		t.Errorf("expected FailedAtStage %s, got %s", model.ProcessingStageWaitingDM, failed.FailedAtStage)
	}

	// No completion event.
	if len(deps.publisher.processingCompleted) != 0 {
		t.Error("no ProcessingCompletedEvent should be published on DM confirmation failure")
	}
}

func TestDMConfirmation_RetryExhausted(t *testing.T) {
	deps := newTestDeps()

	// awaitFunc always returns a retryable error.
	deps.dmAwaiter.awaitFunc = func(_ context.Context, jobID string) (port.DMConfirmationResult, error) {
		return port.DMConfirmationResult{
			JobID: jobID,
			Err:   port.NewDMArtifactsPersistFailedError("transient DM failure", true, nil),
		}, nil
	}

	orch := deps.buildOrchestratorWithRetry(2, time.Millisecond)
	cmd := defaultCmd()

	err := orch.HandleProcessDocument(context.Background(), cmd)
	if err == nil {
		t.Fatal("expected error after exhausted retries, got nil")
	}

	// Verify pipeline failed with FAILED status (not TIMED_OUT).
	if len(deps.publisher.processingFailed) != 1 {
		t.Fatalf("expected 1 ProcessingFailedEvent, got %d", len(deps.publisher.processingFailed))
	}
	failed := deps.publisher.processingFailed[0]
	if failed.Status != model.StatusFailed {
		t.Errorf("expected FAILED, got %s", failed.Status)
	}
	if failed.Status == model.StatusTimedOut {
		t.Error("expected FAILED, not TIMED_OUT")
	}

	// Verify dmSender.sentArtifacts has 2 entries: original send + 1 retry resend.
	// (maxRetries=2 means 2 total attempts in awaitDMConfirmation; the first
	// attempt uses the original send, the second re-sends.)
	if len(deps.dmSender.sentArtifacts) != 2 {
		t.Fatalf("expected 2 SendArtifacts calls (original + 1 retry), got %d", len(deps.dmSender.sentArtifacts))
	}

	// No completion event.
	if len(deps.publisher.processingCompleted) != 0 {
		t.Error("no ProcessingCompletedEvent should be published when retries exhausted")
	}
}

func TestDMConfirmation_ContextTimeout(t *testing.T) {
	deps := newTestDeps()

	// awaitFunc returns ctx.Err() to simulate a timeout during DM wait.
	deps.dmAwaiter.awaitFunc = func(ctx context.Context, _ string) (port.DMConfirmationResult, error) {
		// Ensure the context is done (it should be, given the very short deadline).
		<-ctx.Done()
		return port.DMConfirmationResult{}, ctx.Err()
	}

	// Build orchestrator with a very short job timeout so the context expires.
	ocrAdapter := ocr.NewAdapter(deps.ocrService, deps.tempStorage, 10, 1, time.Second)
	lm := lifecycle.NewLifecycleManager(deps.publisher, deps.idempotency, 1*time.Millisecond, nil, nopLogger())
	orch := NewOrchestrator(
		lm,
		deps.validator,
		deps.fetcher,
		ocrAdapter,
		deps.textExtractor,
		deps.structExtract,
		deps.treeBuilder,
		deps.tempStorage,
		deps.publisher,
		deps.dmSender,
		deps.dmAwaiter,
		deps.dlq,
		nopLogger(),
		1,
		time.Millisecond,
	)

	cmd := defaultCmd()

	err := orch.HandleProcessDocument(context.Background(), cmd)
	if err == nil {
		t.Fatal("expected error on context timeout, got nil")
	}

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("expected context.DeadlineExceeded, got: %v", err)
	}

	// Verify pipeline failed with TIMED_OUT status.
	if len(deps.publisher.processingFailed) != 1 {
		t.Fatalf("expected 1 ProcessingFailedEvent, got %d", len(deps.publisher.processingFailed))
	}
	failed := deps.publisher.processingFailed[0]
	if failed.Status != model.StatusTimedOut {
		t.Errorf("expected TIMED_OUT, got %s", failed.Status)
	}

	// No completion event.
	if len(deps.publisher.processingCompleted) != 0 {
		t.Error("no ProcessingCompletedEvent should be published on context timeout")
	}
}

// --- Tests: DLQ ---

func TestDLQ_SentOnFailed(t *testing.T) {
	// Trigger FAILED by using a retryable broker error from DM sender that
	// exhausts retries (maxRetries=1 means 1 attempt, no success).
	deps := newTestDeps()

	dmSender := &callCountDMSender{
		err: port.NewBrokerError("broker unavailable", errors.New("connection refused")),
	}

	orch := deps.buildOrchestratorWithDMSender(dmSender, 1, time.Millisecond)
	cmd := defaultCmd()

	err := orch.HandleProcessDocument(context.Background(), cmd)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	// Verify DLQ message was sent.
	deps.dlq.mu.Lock()
	dlqMessages := make([]model.DLQMessage, len(deps.dlq.messages))
	copy(dlqMessages, deps.dlq.messages)
	deps.dlq.mu.Unlock()

	if len(dlqMessages) != 1 {
		t.Fatalf("expected 1 DLQ message, got %d", len(dlqMessages))
	}

	dlqMsg := dlqMessages[0]

	// Verify DLQ message fields.
	if dlqMsg.JobID != "job-1" {
		t.Errorf("DLQ JobID: expected job-1, got %s", dlqMsg.JobID)
	}
	if dlqMsg.DocumentID != "doc-1" {
		t.Errorf("DLQ DocumentID: expected doc-1, got %s", dlqMsg.DocumentID)
	}
	if dlqMsg.ErrorCode != port.ErrCodeBrokerFailed {
		t.Errorf("DLQ ErrorCode: expected %s, got %s", port.ErrCodeBrokerFailed, dlqMsg.ErrorCode)
	}
	if dlqMsg.FailedAtStage == "" {
		t.Error("DLQ FailedAtStage should not be empty")
	}
	if dlqMsg.PipelineType != "processing" {
		t.Errorf("DLQ PipelineType: expected processing, got %s", dlqMsg.PipelineType)
	}

	// Verify OriginalCommand contains the command JSON.
	if len(dlqMsg.OriginalCommand) == 0 {
		t.Error("DLQ OriginalCommand should not be empty")
	}
	var cmdFromDLQ map[string]interface{}
	if err := json.Unmarshal(dlqMsg.OriginalCommand, &cmdFromDLQ); err != nil {
		t.Fatalf("failed to unmarshal DLQ OriginalCommand: %v", err)
	}
	if cmdFromDLQ["job_id"] != "job-1" {
		t.Errorf("DLQ OriginalCommand job_id: expected job-1, got %v", cmdFromDLQ["job_id"])
	}
}

func TestDLQ_NotSentOnRejected(t *testing.T) {
	// Trigger REJECTED using a validation error (non-retryable).
	deps := newTestDeps()
	deps.validator.err = port.NewValidationError("document_id is required")

	orch := deps.buildOrchestrator()
	cmd := defaultCmd()

	_ = orch.HandleProcessDocument(context.Background(), cmd)

	// Verify no DLQ message was sent.
	deps.dlq.mu.Lock()
	dlqCount := len(deps.dlq.messages)
	deps.dlq.mu.Unlock()

	if dlqCount != 0 {
		t.Errorf("expected 0 DLQ messages for REJECTED, got %d", dlqCount)
	}
}

func TestDLQ_NotSentOnTimedOut(t *testing.T) {
	// Trigger TIMED_OUT using context.DeadlineExceeded from fetcher.
	deps := newTestDeps()
	deps.fetcher.result = nil
	deps.fetcher.err = context.DeadlineExceeded

	orch := deps.buildOrchestrator()
	cmd := defaultCmd()

	_ = orch.HandleProcessDocument(context.Background(), cmd)

	// Verify no DLQ message was sent.
	deps.dlq.mu.Lock()
	dlqCount := len(deps.dlq.messages)
	deps.dlq.mu.Unlock()

	if dlqCount != 0 {
		t.Errorf("expected 0 DLQ messages for TIMED_OUT, got %d", dlqCount)
	}
}

func TestDLQ_ErrorIsLogged_NotPropagated(t *testing.T) {
	// Set mockDLQ to return an error, then trigger a FAILED pipeline error.
	// Verify that HandleProcessDocument returns the original pipeline error,
	// not the DLQ error.
	deps := newTestDeps()
	deps.dlq.err = errors.New("DLQ broker down")

	dmSender := &callCountDMSender{
		err: port.NewBrokerError("broker unavailable", errors.New("connection refused")),
	}

	orch := deps.buildOrchestratorWithDMSender(dmSender, 1, time.Millisecond)
	cmd := defaultCmd()

	err := orch.HandleProcessDocument(context.Background(), cmd)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	// The returned error should be the original pipeline error, not the DLQ error.
	var domErr *port.DomainError
	if !errors.As(err, &domErr) {
		t.Fatalf("expected DomainError, got %T: %v", err, err)
	}
	if domErr.Code != port.ErrCodeBrokerFailed {
		t.Errorf("expected original error code %s, got %s", port.ErrCodeBrokerFailed, domErr.Code)
	}

	// Verify the error is NOT the DLQ error.
	if err.Error() == "DLQ broker down" {
		t.Error("returned error should be the pipeline error, not the DLQ error")
	}
}

// --- Tests: FailedAtStage reclassification (TASK-051) ---

func TestFailedAtStage_FileTooLarge_IsValidatingFile(t *testing.T) {
	deps := newTestDeps()
	deps.fetcher.result = nil
	deps.fetcher.err = port.NewFileTooLargeError("file exceeds 20 MB")

	orch := deps.buildOrchestrator()
	cmd := defaultCmd()

	_ = orch.HandleProcessDocument(context.Background(), cmd)

	if len(deps.publisher.processingFailed) != 1 {
		t.Fatalf("expected 1 ProcessingFailedEvent, got %d", len(deps.publisher.processingFailed))
	}
	f := deps.publisher.processingFailed[0]
	if f.FailedAtStage != string(model.ProcessingStageValidatingFile) {
		t.Errorf("FailedAtStage: expected %s, got %s", model.ProcessingStageValidatingFile, f.FailedAtStage)
	}
	if f.ErrorCode != port.ErrCodeFileTooLarge {
		t.Errorf("ErrorCode: expected %s, got %s", port.ErrCodeFileTooLarge, f.ErrorCode)
	}
	// File validation errors → REJECTED → no DLQ.
	if len(deps.dlq.messages) != 0 {
		t.Errorf("expected 0 DLQ messages for REJECTED error, got %d", len(deps.dlq.messages))
	}
}

func TestFailedAtStage_InvalidFormat_IsValidatingFile(t *testing.T) {
	deps := newTestDeps()
	deps.fetcher.result = nil
	deps.fetcher.err = port.NewInvalidFormatError("not a PDF")

	orch := deps.buildOrchestrator()
	cmd := defaultCmd()

	_ = orch.HandleProcessDocument(context.Background(), cmd)

	if len(deps.publisher.processingFailed) != 1 {
		t.Fatalf("expected 1 ProcessingFailedEvent, got %d", len(deps.publisher.processingFailed))
	}
	f := deps.publisher.processingFailed[0]
	if f.FailedAtStage != string(model.ProcessingStageValidatingFile) {
		t.Errorf("FailedAtStage: expected %s, got %s", model.ProcessingStageValidatingFile, f.FailedAtStage)
	}
	if f.ErrorCode != port.ErrCodeInvalidFormat {
		t.Errorf("ErrorCode: expected %s, got %s", port.ErrCodeInvalidFormat, f.ErrorCode)
	}
}

func TestFailedAtStage_TooManyPages_IsValidatingFile(t *testing.T) {
	deps := newTestDeps()
	deps.fetcher.result = nil
	deps.fetcher.err = port.NewTooManyPagesError("exceeds 100 pages")

	orch := deps.buildOrchestrator()
	cmd := defaultCmd()

	_ = orch.HandleProcessDocument(context.Background(), cmd)

	if len(deps.publisher.processingFailed) != 1 {
		t.Fatalf("expected 1 ProcessingFailedEvent, got %d", len(deps.publisher.processingFailed))
	}
	f := deps.publisher.processingFailed[0]
	if f.FailedAtStage != string(model.ProcessingStageValidatingFile) {
		t.Errorf("FailedAtStage: expected %s, got %s", model.ProcessingStageValidatingFile, f.FailedAtStage)
	}
	if f.ErrorCode != port.ErrCodeTooManyPages {
		t.Errorf("ErrorCode: expected %s, got %s", port.ErrCodeTooManyPages, f.ErrorCode)
	}
}

func TestFailedAtStage_DownloadFailed_IsFetchingSourceFile(t *testing.T) {
	deps := newTestDeps()
	deps.fetcher.result = nil
	deps.fetcher.err = port.NewFileNotFoundError("file not found", errors.New("404"))

	orch := deps.buildOrchestrator()
	cmd := defaultCmd()

	_ = orch.HandleProcessDocument(context.Background(), cmd)

	if len(deps.publisher.processingFailed) != 1 {
		t.Fatalf("expected 1 ProcessingFailedEvent, got %d", len(deps.publisher.processingFailed))
	}
	f := deps.publisher.processingFailed[0]
	if f.FailedAtStage != string(model.ProcessingStageFetchingSourceFile) {
		t.Errorf("FailedAtStage: expected %s, got %s", model.ProcessingStageFetchingSourceFile, f.FailedAtStage)
	}
	if f.ErrorCode != port.ErrCodeFileNotFound {
		t.Errorf("ErrorCode: expected %s, got %s", port.ErrCodeFileNotFound, f.ErrorCode)
	}
}

func TestFailedAtStage_ServiceUnavailable_IsFetchingSourceFile(t *testing.T) {
	deps := newTestDeps()
	deps.fetcher.result = nil
	deps.fetcher.err = port.NewServiceUnavailableError("download failed", errors.New("connection refused"))

	orch := deps.buildOrchestrator()
	cmd := defaultCmd()

	_ = orch.HandleProcessDocument(context.Background(), cmd)

	if len(deps.publisher.processingFailed) != 1 {
		t.Fatalf("expected 1 ProcessingFailedEvent, got %d", len(deps.publisher.processingFailed))
	}
	f := deps.publisher.processingFailed[0]
	if f.FailedAtStage != string(model.ProcessingStageFetchingSourceFile) {
		t.Errorf("FailedAtStage: expected %s, got %s", model.ProcessingStageFetchingSourceFile, f.FailedAtStage)
	}
	if f.ErrorCode != port.ErrCodeServiceUnavailable {
		t.Errorf("ErrorCode: expected %s, got %s", port.ErrCodeServiceUnavailable, f.ErrorCode)
	}
}

func TestFailedAtStage_DLQ_FetchError_StageIsFetchingSourceFile(t *testing.T) {
	// Non-validation fetch error (SERVICE_UNAVAILABLE) → FAILED → DLQ sent
	// with FETCHING_SOURCE_FILE stage.
	deps := newTestDeps()
	deps.fetcher.result = nil
	deps.fetcher.err = port.NewServiceUnavailableError("download failed", errors.New("timeout"))

	orch := deps.buildOrchestrator()
	cmd := defaultCmd()

	_ = orch.HandleProcessDocument(context.Background(), cmd)

	// SERVICE_UNAVAILABLE → FAILED → DLQ sent.
	if len(deps.dlq.messages) != 1 {
		t.Fatalf("expected 1 DLQ message, got %d", len(deps.dlq.messages))
	}
	if deps.dlq.messages[0].FailedAtStage != string(model.ProcessingStageFetchingSourceFile) {
		t.Errorf("DLQ FailedAtStage: expected %s, got %s",
			model.ProcessingStageFetchingSourceFile, deps.dlq.messages[0].FailedAtStage)
	}
}

func TestFileValidationCodes_SubsetOfRejectedCodes(t *testing.T) {
	for code := range fileValidationCodes {
		if !rejectedCodes[code] {
			t.Errorf("fileValidationCodes contains %q which is not in rejectedCodes", code)
		}
	}
}

func TestIsFileValidationError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{"FileTooLarge", port.NewFileTooLargeError("too big"), true},
		{"InvalidFormat", port.NewInvalidFormatError("not pdf"), true},
		{"TooManyPages", port.NewTooManyPagesError("too many"), true},
		{"FileNotFound", port.NewFileNotFoundError("not found", nil), false},
		{"ServiceUnavailable", port.NewServiceUnavailableError("down", nil), false},
		{"ValidationError", port.NewValidationError("bad input"), false},
		{"ContextCanceled", context.Canceled, false},
		{"DeadlineExceeded", context.DeadlineExceeded, false},
		{"NilError", nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isFileValidationError(tt.err)
			if got != tt.expected {
				t.Errorf("isFileValidationError(%v) = %v, want %v", tt.err, got, tt.expected)
			}
		})
	}
}
