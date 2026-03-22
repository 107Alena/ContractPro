package processing

import (
	"context"
	"log"
	"time"

	"contractpro/document-processing/internal/application/lifecycle"
	"contractpro/document-processing/internal/application/warning"
	"contractpro/document-processing/internal/domain/model"
	"contractpro/document-processing/internal/domain/port"
)

// Compile-time interface compliance check.
var _ port.ProcessingCommandHandler = (*Orchestrator)(nil)

// Orchestrator implements port.ProcessingCommandHandler — the main orchestrator
// that runs the full document processing pipeline (happy path).
//
// Pipeline stages:
//
//	VALIDATING_INPUT → FETCHING_SOURCE_FILE (includes file format/page validation) →
//	OCR/OCR_SKIPPED → TEXT_EXTRACTION → STRUCTURE_EXTRACTION →
//	SEMANTIC_TREE_BUILDING → SAVING_ARTIFACTS → CLEANUP_TEMP_ARTIFACTS
//
// NOTE: WAITING_DM_CONFIRMATION is currently a no-op placeholder until TASK-034
// implements the DM Inbound Adapter.
//
// NOTE: The shared *warning.Collector is not safe for concurrent HandleProcessDocument
// calls. Concurrent job processing will be addressed in TASK-036.
//
// Error handling and retry logic will be added in TASK-036. For now, any
// error encountered during processing is returned immediately.
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
}

// NewOrchestrator creates an Orchestrator with all required dependencies.
// Panics if any dependency is nil (programmer error at startup).
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
	}
}

// HandleProcessDocument executes the full document processing pipeline.
//
// The method creates a ProcessingJob, transitions it through each stage,
// and on success publishes a ProcessingCompletedEvent. This is the happy-path
// implementation; error handling with failure events will be added in TASK-036.
func (o *Orchestrator) HandleProcessDocument(ctx context.Context, cmd model.ProcessDocumentCommand) error {
	job := model.NewProcessingJob(cmd.JobID, cmd.DocumentID, cmd.FileURL)
	job.FileName = cmd.FileName
	job.FileSize = cmd.FileSize
	job.MimeType = cmd.MimeType
	job.Checksum = cmd.Checksum
	job.OrgID = cmd.OrgID
	job.UserID = cmd.UserID

	o.warnings.Reset()

	// Create a job-scoped context with timeout.
	jobCtx, cancel := o.lifecycle.NewJobContext(ctx)
	defer cancel()

	// --- Transition: QUEUED → IN_PROGRESS ---
	job.Stage = model.ProcessingStageValidatingInput
	if err := o.lifecycle.TransitionJob(jobCtx, job, model.StatusInProgress); err != nil {
		return err
	}

	// --- Stage 1: VALIDATING_INPUT ---
	if err := o.validator.Validate(jobCtx, cmd); err != nil {
		return err
	}

	// --- Stage 2: FETCHING_SOURCE_FILE (includes PDF format and page count validation) ---
	job.Stage = model.ProcessingStageFetchingSourceFile
	fetchResult, err := o.fetcher.Fetch(jobCtx, cmd)
	if err != nil {
		return err
	}

	// --- Stage 3: OCR ---
	job.Stage = model.ProcessingStageOCR
	ocrResult, err := o.ocrProcessor.Process(jobCtx, fetchResult.StorageKey, fetchResult.IsTextPDF)
	if err != nil {
		return err
	}
	if ocrResult.Status == model.OCRStatusNotApplicable {
		job.Stage = model.ProcessingStageOCRSkipped
	}

	// --- Stage 4: TEXT_EXTRACTION ---
	job.Stage = model.ProcessingStageTextExtraction
	extractedText, textWarnings, err := o.textExtract.Extract(jobCtx, fetchResult.StorageKey, ocrResult)
	if err != nil {
		return err
	}
	for _, w := range textWarnings {
		o.warnings.Add(w)
	}

	// --- Stage 5: STRUCTURE_EXTRACTION ---
	job.Stage = model.ProcessingStageStructureExtract
	structure, structWarnings, err := o.structExtract.Extract(jobCtx, extractedText)
	if err != nil {
		return err
	}
	for _, w := range structWarnings {
		o.warnings.Add(w)
	}

	// --- Stage 6: SEMANTIC_TREE_BUILDING ---
	job.Stage = model.ProcessingStageSemanticTree
	semanticTree, err := o.treeBuilder.Build(jobCtx, extractedText, structure)
	if err != nil {
		return err
	}

	// Collect all warnings once — used for both artifacts and completion event.
	allWarnings := o.warnings.Collect()

	// --- Stage 7: SAVING_ARTIFACTS (send to DM) ---
	job.Stage = model.ProcessingStageSavingArtifacts
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
	if err := o.dmSender.SendArtifacts(jobCtx, artifactsEvent); err != nil {
		return err
	}

	// --- Stage 8: CLEANUP_TEMP_ARTIFACTS ---
	// Best-effort: artifacts have already been sent to DM. A cleanup failure
	// should not prevent the job from completing successfully.
	job.Stage = model.ProcessingStageCleanup
	if err := o.tempStorage.DeleteByPrefix(jobCtx, cmd.JobID); err != nil {
		log.Printf("processing: cleanup failed for job %s: %v", cmd.JobID, err)
	}

	// --- Transition: IN_PROGRESS → COMPLETED / COMPLETED_WITH_WARNINGS ---
	finalStatus := model.StatusCompleted
	if len(allWarnings) > 0 {
		finalStatus = model.StatusCompletedWithWarnings
	}
	if err := o.lifecycle.TransitionJob(jobCtx, job, finalStatus); err != nil {
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
	if err := o.publisher.PublishProcessingCompleted(jobCtx, completedEvent); err != nil {
		return err
	}

	return nil
}
