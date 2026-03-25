package processing

import (
	"context"
	"errors"
	"time"

	"contractpro/document-processing/internal/application/lifecycle"
	"contractpro/document-processing/internal/application/warning"
	"contractpro/document-processing/internal/domain/model"
	"contractpro/document-processing/internal/domain/port"
	"contractpro/document-processing/internal/infra/observability"
)

// Compile-time interface compliance check.
var _ port.ProcessingCommandHandler = (*Orchestrator)(nil)

// rejectedCodes maps error codes that result in REJECTED status.
var rejectedCodes = map[string]bool{
	port.ErrCodeValidation:   true,
	port.ErrCodeFileTooLarge: true,
	port.ErrCodeTooManyPages: true,
	port.ErrCodeInvalidFormat: true,
	port.ErrCodeFileNotFound:  true,
	port.ErrCodeSSRFBlocked:   true,
}

// Orchestrator implements port.ProcessingCommandHandler — the main orchestrator
// that runs the full document processing pipeline with error handling and retry.
//
// Pipeline stages:
//
//	VALIDATING_INPUT -> FETCHING_SOURCE_FILE (includes file format/page validation) ->
//	OCR/OCR_SKIPPED -> TEXT_EXTRACTION -> STRUCTURE_EXTRACTION ->
//	SEMANTIC_TREE_BUILDING -> SAVING_ARTIFACTS -> CLEANUP_TEMP_ARTIFACTS
//
// NOTE: WAITING_DM_CONFIRMATION is currently a no-op placeholder until TASK-034
// implements the DM Inbound Adapter.
//
// NOTE: The shared *warning.Collector is not safe for concurrent HandleProcessDocument
// calls. Concurrent job processing will be addressed separately.
type Orchestrator struct {
	lifecycle     *lifecycle.LifecycleManager
	warnings      *warning.Collector
	validator     port.InputValidatorPort
	fetcher       port.SourceFileFetcherPort
	ocrProcessor  port.OCRProcessorPort
	textExtract   port.TextExtractionPort
	structExtract port.StructureExtractionPort
	treeBuilder   port.SemanticTreeBuilderPort
	tempStorage   port.TempStoragePort
	publisher     port.EventPublisherPort
	dmSender      port.DMArtifactSenderPort
	logger        *observability.Logger
	maxRetries    int
	backoffBase   time.Duration
}

// NewOrchestrator creates an Orchestrator with all required dependencies.
// Panics if any dependency is nil (programmer error at startup).
// maxRetries defaults to 1 if < 1, backoffBase defaults to time.Second if <= 0.
func NewOrchestrator(
	lifecycle *lifecycle.LifecycleManager,
	warnings *warning.Collector,
	validator port.InputValidatorPort,
	fetcher port.SourceFileFetcherPort,
	ocrProcessor port.OCRProcessorPort,
	textExtract port.TextExtractionPort,
	structExtract port.StructureExtractionPort,
	treeBuilder port.SemanticTreeBuilderPort,
	tempStorage port.TempStoragePort,
	publisher port.EventPublisherPort,
	dmSender port.DMArtifactSenderPort,
	logger *observability.Logger,
	maxRetries int,
	backoffBase time.Duration,
) *Orchestrator {
	if lifecycle == nil {
		panic("processing: lifecycle manager must not be nil")
	}
	if warnings == nil {
		panic("processing: warnings collector must not be nil")
	}
	if validator == nil {
		panic("processing: validator must not be nil")
	}
	if fetcher == nil {
		panic("processing: fetcher must not be nil")
	}
	if ocrProcessor == nil {
		panic("processing: ocr processor must not be nil")
	}
	if textExtract == nil {
		panic("processing: text extractor must not be nil")
	}
	if structExtract == nil {
		panic("processing: structure extractor must not be nil")
	}
	if treeBuilder == nil {
		panic("processing: tree builder must not be nil")
	}
	if tempStorage == nil {
		panic("processing: temp storage must not be nil")
	}
	if publisher == nil {
		panic("processing: publisher must not be nil")
	}
	if dmSender == nil {
		panic("processing: dm sender must not be nil")
	}
	if logger == nil {
		panic("processing: logger must not be nil")
	}
	if maxRetries < 1 {
		maxRetries = 1
	}
	if backoffBase <= 0 {
		backoffBase = time.Second
	}
	return &Orchestrator{
		lifecycle:     lifecycle,
		warnings:      warnings,
		validator:     validator,
		fetcher:       fetcher,
		ocrProcessor:  ocrProcessor,
		textExtract:   textExtract,
		structExtract: structExtract,
		treeBuilder:   treeBuilder,
		tempStorage:   tempStorage,
		publisher:     publisher,
		dmSender:      dmSender,
		logger:        logger.With("component", "processing"),
		maxRetries:    maxRetries,
		backoffBase:   backoffBase,
	}
}

// classifyError determines the terminal status and event-level is_retryable flag.
// DeadlineExceeded is checked first because it can wrap inside any DomainError
// when the job context expires mid-operation.
// The event is_retryable is false for FAILED because the DP service has already
// exhausted its own retries. Only TIMED_OUT is marked retryable to signal
// the upstream consumer that re-submission may succeed.
func classifyError(err error) (model.JobStatus, bool) {
	if errors.Is(err, context.DeadlineExceeded) {
		return model.StatusTimedOut, true
	}

	code := port.ErrorCode(err)
	if rejectedCodes[code] {
		return model.StatusRejected, false
	}

	return model.StatusFailed, false
}

// retryStep runs fn up to o.maxRetries times. On retryable errors it applies
// exponential backoff (backoffBase * 2^attempt) while respecting context
// cancellation. Non-retryable errors are returned immediately.
func (o *Orchestrator) retryStep(ctx context.Context, fn func() error) error {
	var lastErr error
	for attempt := 0; attempt < o.maxRetries; attempt++ {
		lastErr = fn()
		if lastErr == nil {
			return nil
		}
		if !port.IsRetryable(lastErr) {
			return lastErr
		}
		// Last attempt: do not wait, just return the error.
		if attempt == o.maxRetries-1 {
			break
		}
		// Exponential backoff.
		delay := o.backoffBase * (1 << uint(attempt))
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	return lastErr
}

// runPipeline executes the happy-path processing pipeline stages.
// Retry is applied only to stages that can produce retryable errors:
// fetcher.Fetch, ocrProcessor.Process, and dmSender.SendArtifacts.
func (o *Orchestrator) runPipeline(ctx context.Context, job *model.ProcessingJob, cmd model.ProcessDocumentCommand) error {
	// --- Transition: QUEUED -> IN_PROGRESS ---
	job.Stage = model.ProcessingStageValidatingInput
	ctx = observability.WithStage(ctx, string(job.Stage))
	o.logger.Info(ctx, "pipeline stage started")
	if err := o.lifecycle.TransitionJob(ctx, job, model.StatusInProgress); err != nil {
		return err
	}

	// --- Stage 1: VALIDATING_INPUT ---
	if err := o.validator.Validate(ctx, cmd); err != nil {
		return err
	}

	// --- Stage 2: FETCHING_SOURCE_FILE (includes PDF format and page count validation) ---
	job.Stage = model.ProcessingStageFetchingSourceFile
	ctx = observability.WithStage(ctx, string(job.Stage))
	o.logger.Info(ctx, "pipeline stage started")
	var fetchResult *port.FetchResult
	if err := o.retryStep(ctx, func() error {
		var fetchErr error
		fetchResult, fetchErr = o.fetcher.Fetch(ctx, cmd)
		return fetchErr
	}); err != nil {
		return err
	}

	// --- Stage 3: OCR ---
	job.Stage = model.ProcessingStageOCR
	ctx = observability.WithStage(ctx, string(job.Stage))
	o.logger.Info(ctx, "pipeline stage started")
	var ocrResult *model.OCRRawArtifact
	if err := o.retryStep(ctx, func() error {
		var ocrErr error
		ocrResult, ocrErr = o.ocrProcessor.Process(ctx, fetchResult.StorageKey, fetchResult.IsTextPDF)
		return ocrErr
	}); err != nil {
		return err
	}
	if ocrResult.Status == model.OCRStatusNotApplicable {
		job.Stage = model.ProcessingStageOCRSkipped
	}

	// --- Stage 4: TEXT_EXTRACTION ---
	job.Stage = model.ProcessingStageTextExtraction
	ctx = observability.WithStage(ctx, string(job.Stage))
	o.logger.Info(ctx, "pipeline stage started")
	extractedText, textWarnings, err := o.textExtract.Extract(ctx, fetchResult.StorageKey, ocrResult)
	if err != nil {
		return err
	}
	for _, w := range textWarnings {
		o.warnings.Add(w)
	}

	// --- Stage 5: STRUCTURE_EXTRACTION ---
	job.Stage = model.ProcessingStageStructureExtract
	ctx = observability.WithStage(ctx, string(job.Stage))
	o.logger.Info(ctx, "pipeline stage started")
	structure, structWarnings, err := o.structExtract.Extract(ctx, extractedText)
	if err != nil {
		return err
	}
	for _, w := range structWarnings {
		o.warnings.Add(w)
	}

	// --- Stage 6: SEMANTIC_TREE_BUILDING ---
	job.Stage = model.ProcessingStageSemanticTree
	ctx = observability.WithStage(ctx, string(job.Stage))
	o.logger.Info(ctx, "pipeline stage started")
	semanticTree, err := o.treeBuilder.Build(ctx, extractedText, structure)
	if err != nil {
		return err
	}

	// Collect all warnings once — used for both artifacts and completion event.
	allWarnings := o.warnings.Collect()

	// --- Stage 7: SAVING_ARTIFACTS (send to DM) ---
	job.Stage = model.ProcessingStageSavingArtifacts
	ctx = observability.WithStage(ctx, string(job.Stage))
	o.logger.Info(ctx, "pipeline stage started")
	artifactsEvent := model.DocumentProcessingArtifactsReady{
		EventMeta: model.EventMeta{
			CorrelationID: cmd.JobID,
			Timestamp:     time.Now().UTC(),
		},
		JobID:        cmd.JobID,
		DocumentID:   cmd.DocumentID,
		OCRRaw:       *ocrResult,
		Text:         *extractedText,
		Structure:    *structure,
		SemanticTree: *semanticTree,
		Warnings:     allWarnings,
	}
	if err := o.retryStep(ctx, func() error {
		return o.dmSender.SendArtifacts(ctx, artifactsEvent)
	}); err != nil {
		return err
	}

	// --- Stage 8: CLEANUP_TEMP_ARTIFACTS ---
	// Best-effort: artifacts have already been sent to DM. A cleanup failure
	// should not prevent the job from completing successfully.
	job.Stage = model.ProcessingStageCleanup
	ctx = observability.WithStage(ctx, string(job.Stage))
	o.logger.Info(ctx, "pipeline stage started")
	if err := o.tempStorage.DeleteByPrefix(ctx, cmd.JobID); err != nil {
		o.logger.Warn(ctx, "cleanup failed", "error", err)
	}

	// --- Transition: IN_PROGRESS -> COMPLETED / COMPLETED_WITH_WARNINGS ---
	finalStatus := model.StatusCompleted
	if len(allWarnings) > 0 {
		finalStatus = model.StatusCompletedWithWarnings
	}
	if err := o.lifecycle.TransitionJob(ctx, job, finalStatus); err != nil {
		return err
	}

	// --- Publish ProcessingCompletedEvent ---
	completedEvent := model.ProcessingCompletedEvent{
		EventMeta: model.EventMeta{
			CorrelationID: cmd.JobID,
			Timestamp:     time.Now().UTC(),
		},
		JobID:        cmd.JobID,
		DocumentID:   cmd.DocumentID,
		Status:       finalStatus,
		HasWarnings:  len(allWarnings) > 0,
		WarningCount: len(allWarnings),
	}
	if err := o.publisher.PublishProcessingCompleted(ctx, completedEvent); err != nil {
		return err
	}

	o.logger.Info(ctx, "processing pipeline completed", "status", string(finalStatus), "warning_count", len(allWarnings))

	return nil
}

// handlePipelineError handles a pipeline failure: transitions the job to the
// appropriate terminal status, publishes a ProcessingFailedEvent, and performs
// best-effort cleanup. All side effects use context.Background() since the
// job context may have expired (e.g. for TIMED_OUT errors).
func (o *Orchestrator) handlePipelineError(
	job *model.ProcessingJob,
	cmd model.ProcessDocumentCommand,
	pipelineErr error,
) error {
	terminalStatus, isRetryable := classifyError(pipelineErr)

	bgCtx := context.Background()
	bgCtx = observability.WithJobContext(bgCtx, observability.JobContext{
		JobID:         cmd.JobID,
		DocumentID:    cmd.DocumentID,
		CorrelationID: cmd.JobID,
		OrgID:         cmd.OrgID,
		UserID:        cmd.UserID,
		Stage:         string(job.Stage),
	})

	o.logger.Error(bgCtx, "processing pipeline failed",
		"terminal_status", string(terminalStatus),
		"error_code", port.ErrorCode(pipelineErr),
		"error", pipelineErr,
		"failed_at_stage", string(job.Stage),
		"is_retryable", isRetryable,
	)

	// Transition job to terminal status (best-effort: log and continue on failure).
	if err := o.lifecycle.TransitionJob(bgCtx, job, terminalStatus); err != nil {
		o.logger.Error(bgCtx, "failed to transition job to terminal status", "terminal_status", string(terminalStatus), "error", err)
	}

	// Publish ProcessingFailedEvent (best-effort: log and continue on failure).
	failedEvent := model.ProcessingFailedEvent{
		EventMeta: model.EventMeta{
			CorrelationID: cmd.JobID,
			Timestamp:     time.Now().UTC(),
		},
		JobID:         cmd.JobID,
		DocumentID:    cmd.DocumentID,
		Status:        terminalStatus,
		ErrorCode:     port.ErrorCode(pipelineErr),
		ErrorMessage:  pipelineErr.Error(),
		FailedAtStage: string(job.Stage),
		IsRetryable:   isRetryable,
	}
	if err := o.publisher.PublishProcessingFailed(bgCtx, failedEvent); err != nil {
		o.logger.Error(bgCtx, "failed to publish ProcessingFailedEvent", "error", err)
	}

	// Cleanup temp storage (best-effort: log and continue on failure).
	// NOTE: LifecycleManager.TransitionJob may also run cleanup on terminal status
	// if a cleanup function was provided. DeleteByPrefix is idempotent so double
	// cleanup is safe. In production wiring, pass nil cleanup to LifecycleManager
	// or keep all cleanup in the orchestrator to avoid confusion.
	if err := o.tempStorage.DeleteByPrefix(bgCtx, cmd.JobID); err != nil {
		o.logger.Warn(bgCtx, "cleanup failed in error handler", "error", err)
	}

	return pipelineErr
}

// HandleProcessDocument executes the full document processing pipeline.
//
// The method creates a ProcessingJob, transitions it through each stage,
// and on success publishes a ProcessingCompletedEvent. On failure it
// transitions the job to REJECTED, FAILED, or TIMED_OUT, publishes a
// ProcessingFailedEvent, and performs best-effort cleanup.
func (o *Orchestrator) HandleProcessDocument(ctx context.Context, cmd model.ProcessDocumentCommand) error {
	job := model.NewProcessingJob(cmd.JobID, cmd.DocumentID, cmd.FileURL)
	job.FileName = cmd.FileName
	job.FileSize = cmd.FileSize
	job.MimeType = cmd.MimeType
	job.Checksum = cmd.Checksum
	job.OrgID = cmd.OrgID
	job.UserID = cmd.UserID

	o.logger.Info(ctx, "processing pipeline started", "file_name", cmd.FileName, "file_size", cmd.FileSize, "mime_type", cmd.MimeType)

	o.warnings.Reset()

	// Create a job-scoped context with timeout.
	jobCtx, cancel := o.lifecycle.NewJobContext(ctx)
	defer cancel()

	if err := o.runPipeline(jobCtx, job, cmd); err != nil {
		return o.handlePipelineError(job, cmd, err)
	}
	return nil
}
