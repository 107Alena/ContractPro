package processing

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"contractpro/document-processing/internal/application/lifecycle"
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

// fileValidationCodes are error codes that indicate file content validation
// failure (post-download), mapping to the VALIDATING_FILE stage.
// These errors are returned by Fetch() but semantically belong to the
// file validation phase, not the download phase.
var fileValidationCodes = map[string]bool{
	port.ErrCodeFileTooLarge: true,
	port.ErrCodeInvalidFormat: true,
	port.ErrCodeTooManyPages: true,
}

// isFileValidationError returns true if the error code indicates a file
// content validation failure (format, size, page count) as opposed to a
// download/network failure.
func isFileValidationError(err error) bool {
	return fileValidationCodes[port.ErrorCode(err)]
}

// Orchestrator implements port.ProcessingCommandHandler — the main orchestrator
// that runs the full document processing pipeline with error handling and retry.
//
// Pipeline stages:
//
//	VALIDATING_INPUT -> FETCHING_SOURCE_FILE -> VALIDATING_FILE ->
//	OCR/OCR_SKIPPED -> TEXT_EXTRACTION -> STRUCTURE_EXTRACTION ->
//	SEMANTIC_TREE_BUILDING -> SAVING_ARTIFACTS -> WAITING_DM_CONFIRMATION ->
//	CLEANUP_TEMP_ARTIFACTS
//
// FETCHING_SOURCE_FILE handles download; VALIDATING_FILE covers
// format/size/page validation. Both are executed within Fetch() but
// errors are attributed to the correct stage via error-code classification.
//
// Warnings are collected per-job in a local slice within runPipeline,
// ensuring concurrent HandleProcessDocument calls are fully isolated.
type Orchestrator struct {
	lifecycle     *lifecycle.LifecycleManager
	validator     port.InputValidatorPort
	fetcher       port.SourceFileFetcherPort
	ocrProcessor  port.OCRProcessorPort
	textExtract   port.TextExtractionPort
	structExtract port.StructureExtractionPort
	treeBuilder   port.SemanticTreeBuilderPort
	tempStorage   port.TempStoragePort
	publisher     port.EventPublisherPort
	dmSender      port.DMArtifactSenderPort
	dmAwaiter     port.DMConfirmationAwaiterPort
	dlq           port.DLQPort
	logger        *observability.Logger
	maxRetries    int
	backoffBase   time.Duration
}

// NewOrchestrator creates an Orchestrator with all required dependencies.
// Panics if any dependency is nil (programmer error at startup).
// maxRetries defaults to 1 if < 1, backoffBase defaults to time.Second if <= 0.
func NewOrchestrator(
	lifecycle *lifecycle.LifecycleManager,
	validator port.InputValidatorPort,
	fetcher port.SourceFileFetcherPort,
	ocrProcessor port.OCRProcessorPort,
	textExtract port.TextExtractionPort,
	structExtract port.StructureExtractionPort,
	treeBuilder port.SemanticTreeBuilderPort,
	tempStorage port.TempStoragePort,
	publisher port.EventPublisherPort,
	dmSender port.DMArtifactSenderPort,
	dmAwaiter port.DMConfirmationAwaiterPort,
	dlq port.DLQPort,
	logger *observability.Logger,
	maxRetries int,
	backoffBase time.Duration,
) *Orchestrator {
	if lifecycle == nil {
		panic("processing: lifecycle manager must not be nil")
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
	if dmAwaiter == nil {
		panic("processing: dm awaiter must not be nil")
	}
	if dlq == nil {
		panic("processing: dlq must not be nil")
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
		validator:     validator,
		fetcher:       fetcher,
		ocrProcessor:  ocrProcessor,
		textExtract:   textExtract,
		structExtract: structExtract,
		treeBuilder:   treeBuilder,
		tempStorage:   tempStorage,
		publisher:     publisher,
		dmSender:      dmSender,
		dmAwaiter:     dmAwaiter,
		dlq:           dlq,
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
//
// Warnings are collected in a local slice, ensuring per-job isolation
// when multiple jobs are processed concurrently.
func (o *Orchestrator) runPipeline(ctx context.Context, job *model.ProcessingJob, cmd model.ProcessDocumentCommand) error {
	// Per-job warning accumulator — no shared state between concurrent jobs.
	allWarnings := make([]model.ProcessingWarning, 0)

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

	// --- Stage 2: FETCHING_SOURCE_FILE ---
	job.Stage = model.ProcessingStageFetchingSourceFile
	ctx = observability.WithStage(ctx, string(job.Stage))
	o.logger.Info(ctx, "pipeline stage started")
	var fetchResult *port.FetchResult
	if err := o.retryStep(ctx, func() error {
		var fetchErr error
		fetchResult, fetchErr = o.fetcher.Fetch(ctx, cmd)
		return fetchErr
	}); err != nil {
		// Reclassify: file content validation errors (format, size, pages)
		// are attributed to VALIDATING_FILE stage for accurate failed_at_stage.
		if isFileValidationError(err) {
			job.Stage = model.ProcessingStageValidatingFile
			ctx = observability.WithStage(ctx, string(job.Stage))
		}
		return err
	}

	// --- Stage 3: OCR ---
	job.Stage = model.ProcessingStageOCR
	ctx = observability.WithStage(ctx, string(job.Stage))
	o.logger.Info(ctx, "pipeline stage started")
	var ocrResult *model.OCRRawArtifact
	var ocrWarnings []model.ProcessingWarning
	if err := o.retryStep(ctx, func() error {
		var ocrErr error
		ocrResult, ocrWarnings, ocrErr = o.ocrProcessor.Process(ctx, fetchResult.StorageKey, fetchResult.IsTextPDF)
		return ocrErr
	}); err != nil {
		return err
	}
	allWarnings = append(allWarnings, ocrWarnings...)
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
	allWarnings = append(allWarnings, textWarnings...)

	// --- Stage 5: STRUCTURE_EXTRACTION ---
	job.Stage = model.ProcessingStageStructureExtract
	ctx = observability.WithStage(ctx, string(job.Stage))
	o.logger.Info(ctx, "pipeline stage started")
	structure, structWarnings, err := o.structExtract.Extract(ctx, extractedText)
	if err != nil {
		return err
	}
	allWarnings = append(allWarnings, structWarnings...)

	// --- Stage 6: SEMANTIC_TREE_BUILDING ---
	job.Stage = model.ProcessingStageSemanticTree
	ctx = observability.WithStage(ctx, string(job.Stage))
	o.logger.Info(ctx, "pipeline stage started")
	semanticTree, err := o.treeBuilder.Build(ctx, extractedText, structure)
	if err != nil {
		return err
	}

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

	// Register confirmation BEFORE sending, so an immediate DM response
	// is captured even if DM processes it faster than we reach Await.
	if err := o.dmAwaiter.Register(cmd.JobID); err != nil {
		return err
	}
	if err := o.retryStep(ctx, func() error {
		return o.dmSender.SendArtifacts(ctx, artifactsEvent)
	}); err != nil {
		o.dmAwaiter.Cancel(cmd.JobID)
		return err
	}

	// --- Stage 7.5: WAITING_DM_CONFIRMATION ---
	job.Stage = model.ProcessingStageWaitingDM
	ctx = observability.WithStage(ctx, string(job.Stage))
	o.logger.Info(ctx, "pipeline stage started")
	if err := o.awaitDMConfirmation(ctx, cmd, artifactsEvent); err != nil {
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

// awaitDMConfirmation waits for DM to confirm artifact persistence.
// On retryable DM failures, it re-sends artifacts and re-waits,
// up to o.maxRetries total attempts. Uses exponential backoff between retries.
func (o *Orchestrator) awaitDMConfirmation(
	ctx context.Context,
	cmd model.ProcessDocumentCommand,
	artifactsEvent model.DocumentProcessingArtifactsReady,
) error {
	for attempt := 0; attempt < o.maxRetries; attempt++ {
		result, err := o.dmAwaiter.Await(ctx, cmd.JobID)
		if err != nil {
			// Context timeout/cancellation — propagate immediately.
			return err
		}

		if result.Err == nil {
			// DM confirmed success.
			o.logger.Info(ctx, "DM confirmed artifacts persisted")
			return nil
		}

		// DM reported failure.
		o.logger.Warn(ctx, "DM artifacts persist failed",
			"attempt", attempt+1,
			"max_retries", o.maxRetries,
			"error", result.Err,
			"is_retryable", port.IsRetryable(result.Err),
		)

		if !port.IsRetryable(result.Err) {
			return result.Err
		}

		// Last attempt: return the error, do not retry.
		if attempt == o.maxRetries-1 {
			return result.Err
		}

		// Exponential backoff before re-send.
		delay := o.backoffBase * (1 << uint(attempt))
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}

		// Re-register and re-send artifacts for the next attempt.
		if err := o.dmAwaiter.Register(cmd.JobID); err != nil {
			return err
		}
		artifactsEvent.Timestamp = time.Now().UTC()
		if err := o.retryStep(ctx, func() error {
			return o.dmSender.SendArtifacts(ctx, artifactsEvent)
		}); err != nil {
			o.dmAwaiter.Cancel(cmd.JobID)
			return err
		}
	}

	return port.NewDMArtifactsPersistFailedError(
		"DM confirmation retries exhausted", false, nil)
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

	// Cancel any pending DM confirmation (best-effort: always safe to call).
	o.dmAwaiter.Cancel(cmd.JobID)

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

	// Send to DLQ (best-effort: log and continue on failure).
	// Only for FAILED status — REJECTED jobs have deterministic input errors
	// (not worth reprocessing), and TIMED_OUT jobs are already marked retryable
	// in the failed event for the upstream consumer.
	if terminalStatus == model.StatusFailed {
		o.sendToDLQ(bgCtx, cmd.JobID, cmd.DocumentID, pipelineErr, job, "processing", cmd)
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

// sendToDLQ marshals the original command and sends a DLQ message.
// Best-effort: errors are logged, not propagated.
//
// NOTE: OriginalCommand includes FileURL, which may contain pre-signed URLs.
// DLQ topic access must be restricted to the same security boundary.
//
// This method mirrors comparison.Orchestrator.sendToDLQ by design:
// each pipeline owns its DLQ integration independently.
func (o *Orchestrator) sendToDLQ(
	ctx context.Context,
	jobID, documentID string,
	pipelineErr error,
	job *model.ProcessingJob,
	pipelineType string,
	originalCmd any,
) {
	cmdJSON, marshalErr := json.Marshal(originalCmd)
	if marshalErr != nil {
		o.logger.Error(ctx, "failed to marshal command for DLQ", "error", marshalErr)
		return
	}
	dlqMsg := model.DLQMessage{
		EventMeta: model.EventMeta{
			CorrelationID: jobID,
			Timestamp:     time.Now().UTC(),
		},
		JobID:           jobID,
		DocumentID:      documentID,
		ErrorCode:       port.ErrorCode(pipelineErr),
		ErrorMessage:    pipelineErr.Error(),
		FailedAtStage:   string(job.Stage),
		PipelineType:    pipelineType,
		OriginalCommand: cmdJSON,
	}
	if err := o.dlq.SendToDLQ(ctx, dlqMsg); err != nil {
		o.logger.Error(ctx, "failed to send to DLQ", "error", err)
	}
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

	// Create a job-scoped context with timeout.
	jobCtx, cancel := o.lifecycle.NewJobContext(ctx)
	defer cancel()

	if err := o.runPipeline(jobCtx, job, cmd); err != nil {
		return o.handlePipelineError(job, cmd, err)
	}
	return nil
}
