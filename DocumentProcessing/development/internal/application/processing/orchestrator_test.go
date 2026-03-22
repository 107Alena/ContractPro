package processing

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"contractpro/document-processing/internal/application/lifecycle"
	"contractpro/document-processing/internal/application/warning"
	"contractpro/document-processing/internal/domain/model"
	"contractpro/document-processing/internal/domain/port"
	"contractpro/document-processing/internal/engine/ocr"
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

// --- Helpers ---

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
	wc            *warning.Collector
	validator     *mockValidator
	fetcher       *mockFetcher
	ocrService    *mockOCRService
	textExtractor *mockTextExtractor
	structExtract *mockStructureExtractor
	treeBuilder   *mockTreeBuilder
	tempStorage   *mockTempStorage
	dmSender      *mockDMSender
}

// newTestDeps creates testDeps pre-configured for a successful happy-path
// pipeline (text PDF, no OCR, no warnings).
func newTestDeps() *testDeps {
	return &testDeps{
		publisher:   &mockPublisher{},
		idempotency: &mockIdempotency{},
		wc:          warning.NewCollector(),
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
	}
}

func (d *testDeps) buildOrchestrator() *Orchestrator {
	ocrAdapter := ocr.NewAdapter(d.ocrService, d.tempStorage, d.wc, 10, 1, time.Second)
	lm := lifecycle.NewLifecycleManager(d.publisher, d.idempotency, 120*time.Second, nil)
	return NewOrchestrator(
		lm,
		d.wc,
		d.validator,
		d.fetcher,
		ocrAdapter,
		d.textExtractor,
		d.structExtract,
		d.treeBuilder,
		d.tempStorage,
		d.publisher,
		d.dmSender,
	)
}

// --- Tests ---

func TestHappyPathTextPDF_NoWarnings(t *testing.T) {
	deps := newTestDeps()
	orch := deps.buildOrchestrator()
	cmd := defaultCmd()

	err := orch.HandleProcessDocument(context.Background(), cmd)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	// Verify status transitions: QUEUED → IN_PROGRESS, then IN_PROGRESS → COMPLETED.
	if len(deps.publisher.statusChanged) != 2 {
		t.Fatalf("expected 2 StatusChangedEvents, got %d", len(deps.publisher.statusChanged))
	}

	first := deps.publisher.statusChanged[0]
	if first.OldStatus != model.StatusQueued || first.NewStatus != model.StatusInProgress {
		t.Errorf("first transition: expected QUEUED→IN_PROGRESS, got %s→%s", first.OldStatus, first.NewStatus)
	}

	second := deps.publisher.statusChanged[1]
	if second.OldStatus != model.StatusInProgress || second.NewStatus != model.StatusCompleted {
		t.Errorf("second transition: expected IN_PROGRESS→COMPLETED, got %s→%s", second.OldStatus, second.NewStatus)
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

	// Verify the lifecycle transition still happened (QUEUED → IN_PROGRESS).
	if len(deps.publisher.statusChanged) != 1 {
		t.Fatalf("expected 1 StatusChangedEvent (QUEUED→IN_PROGRESS), got %d", len(deps.publisher.statusChanged))
	}

	// No completion event should be published.
	if len(deps.publisher.processingCompleted) != 0 {
		t.Error("no ProcessingCompletedEvent should be published on validation error")
	}

	// No artifacts sent to DM.
	if len(deps.dmSender.sentArtifacts) != 0 {
		t.Error("no artifacts should be sent to DM on validation error")
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
	ocrAdapter := ocr.NewAdapter(deps.ocrService, deps.tempStorage, deps.wc, 10, 1, time.Second)
	lm := lifecycle.NewLifecycleManager(specialPub, deps.idempotency, 120*time.Second, nil)
	orch := NewOrchestrator(
		lm,
		deps.wc,
		deps.validator,
		deps.fetcher,
		ocrAdapter,
		deps.textExtractor,
		deps.structExtract,
		deps.treeBuilder,
		deps.tempStorage,
		specialPub,
		deps.dmSender,
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
	if a.Warnings != nil {
		t.Errorf("expected nil Warnings for no-warning path, got %v", a.Warnings)
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
	wc := warning.NewCollector()
	lm := lifecycle.NewLifecycleManager(pub, idem, 120*time.Second, nil)
	ocrAdapter := ocr.NewAdapter(&mockOCRService{}, &mockTempStorage{}, wc, 10, 1, time.Second)

	validArgs := []interface{}{
		lm,
		wc,
		&mockValidator{},
		&mockFetcher{result: defaultFetchResult()},
		ocrAdapter,
		&mockTextExtractor{text: defaultExtractedText()},
		&mockStructureExtractor{structure: defaultStructure()},
		&mockTreeBuilder{tree: defaultSemanticTree()},
		&mockTempStorage{},
		pub,
		&mockDMSender{},
	}

	// Test that passing nil for each dependency panics.
	depNames := []string{
		"lifecycle", "warnings", "validator", "fetcher", "ocrProcessor",
		"textExtract", "structExtract", "treeBuilder",
		"tempStorage", "publisher", "dmSender",
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
				asWarnings(args[1]),
				asValidator(args[2]),
				asFetcher(args[3]),
				asOCRProcessor(args[4]),
				asTextExtractor(args[5]),
				asStructExtractor(args[6]),
				asTreeBuilder(args[7]),
				asTempStorage(args[8]),
				asPublisher(args[9]),
				asDMSender(args[10]),
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

func asWarnings(v interface{}) *warning.Collector {
	if v == nil {
		return nil
	}
	return v.(*warning.Collector)
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
}

func TestContextCancellation_ReturnsError(t *testing.T) {
	deps := newTestDeps()

	// Use a context-aware validator that checks ctx.Err() before proceeding.
	deps.validator = &mockValidator{err: nil}

	// Build orchestrator with a very short job timeout so the context expires
	// during the pipeline. We simulate this by providing a validator that
	// respects context cancellation.
	wc := warning.NewCollector()
	ocrAdapter := ocr.NewAdapter(deps.ocrService, deps.tempStorage, wc, 10, 1, time.Second)
	lm := lifecycle.NewLifecycleManager(deps.publisher, deps.idempotency, 120*time.Second, nil)

	// Use a fetcher that checks context and returns cancel error.
	cancelFetcher := &contextAwareFetcher{result: defaultFetchResult()}
	orch := NewOrchestrator(
		lm,
		wc,
		deps.validator,
		cancelFetcher,
		ocrAdapter,
		deps.textExtractor,
		deps.structExtract,
		deps.treeBuilder,
		deps.tempStorage,
		deps.publisher,
		deps.dmSender,
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
