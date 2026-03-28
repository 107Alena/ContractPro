package comparison

import (
	"context"
	"errors"
	"fmt"
	"time"

	"contractpro/document-processing/internal/application/lifecycle"
	"contractpro/document-processing/internal/domain/model"
	"contractpro/document-processing/internal/domain/port"
	"contractpro/document-processing/internal/infra/observability"
)

// Compile-time interface compliance check.
var _ port.ComparisonCommandHandler = (*Orchestrator)(nil)

// rejectedCodes maps error codes that result in REJECTED status.
// Comparison pipeline does not include file-related codes (no file download step).
var rejectedCodes = map[string]bool{
	port.ErrCodeValidation:       true,
	port.ErrCodeDMVersionNotFound: true,
}

// Orchestrator implements port.ComparisonCommandHandler — the main orchestrator
// that runs the full version comparison pipeline with error handling and retry.
//
// Pipeline stages:
//
//	VALIDATING_INPUT -> REQUESTING_SEMANTIC_TREES -> WAITING_DM_RESPONSE ->
//	EXECUTING_DIFF -> SAVING_COMPARISON_RESULT -> WAITING_DM_CONFIRMATION
//
// The comparison pipeline does not currently produce engine-level warnings,
// but uses the same per-job local slice pattern as the processing pipeline
// for future extensibility and consistency.
type Orchestrator struct {
	lifecycle   *lifecycle.LifecycleManager
	treeReq     port.DMTreeRequesterPort
	dmSender    port.DMArtifactSenderPort
	registry    port.PendingResponseRegistryPort
	comparer    port.VersionComparisonPort
	publisher   port.EventPublisherPort
	logger      *observability.Logger
	maxRetries  int
	backoffBase time.Duration
}

// NewOrchestrator creates an Orchestrator with all required dependencies.
// Panics if any dependency is nil (programmer error at startup).
// maxRetries defaults to 1 if < 1, backoffBase defaults to time.Second if <= 0.
func NewOrchestrator(
	lifecycle *lifecycle.LifecycleManager,
	treeReq port.DMTreeRequesterPort,
	dmSender port.DMArtifactSenderPort,
	registry port.PendingResponseRegistryPort,
	comparer port.VersionComparisonPort,
	publisher port.EventPublisherPort,
	logger *observability.Logger,
	maxRetries int,
	backoffBase time.Duration,
) *Orchestrator {
	if lifecycle == nil {
		panic("comparison: lifecycle manager must not be nil")
	}
	if treeReq == nil {
		panic("comparison: tree requester must not be nil")
	}
	if dmSender == nil {
		panic("comparison: dm sender must not be nil")
	}
	if registry == nil {
		panic("comparison: registry must not be nil")
	}
	if comparer == nil {
		panic("comparison: comparer must not be nil")
	}
	if publisher == nil {
		panic("comparison: publisher must not be nil")
	}
	if logger == nil {
		panic("comparison: logger must not be nil")
	}
	if maxRetries < 1 {
		maxRetries = 1
	}
	if backoffBase <= 0 {
		backoffBase = time.Second
	}
	return &Orchestrator{
		lifecycle:   lifecycle,
		treeReq:     treeReq,
		dmSender:    dmSender,
		registry:    registry,
		comparer:    comparer,
		publisher:   publisher,
		logger:      logger.With("component", "comparison"),
		maxRetries:  maxRetries,
		backoffBase: backoffBase,
	}
}

// classifyError determines the terminal status and event-level is_retryable flag.
// Context errors (DeadlineExceeded, Canceled) are checked first because they can
// wrap inside any DomainError when the job context expires or is cancelled.
// For FAILED, is_retryable is derived from the DomainError's Retryable flag,
// which allows external error sources (e.g., DM's DiffPersistFailed with
// is_retryable) to propagate retryability to the upstream consumer.
func classifyError(err error) (model.JobStatus, bool) {
	if errors.Is(err, context.DeadlineExceeded) {
		return model.StatusTimedOut, true
	}
	if errors.Is(err, context.Canceled) {
		return model.StatusTimedOut, true
	}

	code := port.ErrorCode(err)
	if rejectedCodes[code] {
		return model.StatusRejected, false
	}

	return model.StatusFailed, port.IsRetryable(err)
}

// validateCompareCommand checks that the comparison command has all required
// fields and that the base and target version IDs are different.
func validateCompareCommand(cmd model.CompareVersionsCommand) error {
	var missing []string
	if cmd.JobID == "" {
		missing = append(missing, "job_id")
	}
	if cmd.DocumentID == "" {
		missing = append(missing, "document_id")
	}
	if cmd.BaseVersionID == "" {
		missing = append(missing, "base_version_id")
	}
	if cmd.TargetVersionID == "" {
		missing = append(missing, "target_version_id")
	}
	if len(missing) > 0 {
		return port.NewValidationError(fmt.Sprintf("comparison: missing required fields: %v", missing))
	}
	if cmd.BaseVersionID == cmd.TargetVersionID {
		return port.NewValidationError("comparison: base_version_id and target_version_id must differ")
	}
	return nil
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

// runPipeline executes the happy-path comparison pipeline stages.
// Retry is applied only to stages that can produce retryable errors:
// treeReq.RequestSemanticTree and dmSender.SendDiffResult.
func (o *Orchestrator) runPipeline(ctx context.Context, job *model.ComparisonJob, cmd model.CompareVersionsCommand) error {
	// --- Stage 1: VALIDATING_INPUT — Transition: QUEUED -> IN_PROGRESS ---
	job.Stage = model.ComparisonStageValidatingInput
	ctx = observability.WithStage(ctx, string(job.Stage))
	o.logger.Info(ctx, "pipeline stage started")
	if err := o.lifecycle.TransitionJob(ctx, job, model.StatusInProgress); err != nil {
		return err
	}

	// Validate command fields: all IDs must be non-empty and versions must differ.
	if err := validateCompareCommand(cmd); err != nil {
		return err
	}

	// --- Stage 2: REQUESTING_SEMANTIC_TREES ---
	job.Stage = model.ComparisonStageRequestingTrees
	ctx = observability.WithStage(ctx, string(job.Stage))
	o.logger.Info(ctx, "pipeline stage started")
	baseCorrID := fmt.Sprintf("%s:base:%s", cmd.JobID, cmd.BaseVersionID)
	targetCorrID := fmt.Sprintf("%s:target:%s", cmd.JobID, cmd.TargetVersionID)
	confirmCorrID := cmd.JobID + ":diff-confirm"

	// Register expected responses BEFORE sending requests.
	if err := o.registry.Register(cmd.JobID, []string{baseCorrID, targetCorrID}); err != nil {
		return err
	}

	// Send base tree request with retry.
	if err := o.retryStep(ctx, func() error {
		return o.treeReq.RequestSemanticTree(ctx, model.GetSemanticTreeRequest{
			EventMeta:  model.EventMeta{CorrelationID: baseCorrID, Timestamp: time.Now().UTC()},
			JobID:      cmd.JobID,
			DocumentID: cmd.DocumentID,
			VersionID:  cmd.BaseVersionID,
		})
	}); err != nil {
		o.registry.Cancel(cmd.JobID)
		return err
	}

	// Send target tree request with retry.
	if err := o.retryStep(ctx, func() error {
		return o.treeReq.RequestSemanticTree(ctx, model.GetSemanticTreeRequest{
			EventMeta:  model.EventMeta{CorrelationID: targetCorrID, Timestamp: time.Now().UTC()},
			JobID:      cmd.JobID,
			DocumentID: cmd.DocumentID,
			VersionID:  cmd.TargetVersionID,
		})
	}); err != nil {
		o.registry.Cancel(cmd.JobID)
		return err
	}

	// --- Stage 3: WAITING_DM_RESPONSE ---
	job.Stage = model.ComparisonStageWaitingDM
	ctx = observability.WithStage(ctx, string(job.Stage))
	o.logger.Info(ctx, "pipeline stage started")
	responses, err := o.registry.AwaitAll(ctx, cmd.JobID)
	if err != nil {
		return err
	}

	// Extract base and target trees from responses.
	var baseTree, targetTree *model.SemanticTree
	for _, resp := range responses {
		if resp.Err != nil {
			return resp.Err
		}
		if resp.CorrelationID == baseCorrID {
			baseTree = resp.Tree
		} else {
			targetTree = resp.Tree
		}
	}

	// Defensive nil check: both trees must be present.
	if baseTree == nil || targetTree == nil {
		return port.NewValidationError("comparison: missing semantic tree in DM response")
	}

	// --- Stage 4: EXECUTING_DIFF ---
	job.Stage = model.ComparisonStageExecutingDiff
	ctx = observability.WithStage(ctx, string(job.Stage))
	o.logger.Info(ctx, "pipeline stage started")
	diffResult, err := o.comparer.Compare(ctx, baseTree, targetTree)
	if err != nil {
		return err
	}

	// --- Stage 5: SAVING_COMPARISON_RESULT ---
	job.Stage = model.ComparisonStageSavingResult
	ctx = observability.WithStage(ctx, string(job.Stage))
	o.logger.Info(ctx, "pipeline stage started")
	diffReadyEvent := model.DocumentVersionDiffReady{
		EventMeta: model.EventMeta{
			CorrelationID: confirmCorrID,
			Timestamp:     time.Now().UTC(),
		},
		JobID:           cmd.JobID,
		DocumentID:      cmd.DocumentID,
		BaseVersionID:   cmd.BaseVersionID,
		TargetVersionID: cmd.TargetVersionID,
		TextDiffs:       diffResult.TextDiffs,
		StructuralDiffs: diffResult.StructuralDiffs,
	}

	// Register confirmation BEFORE sending diff, so that an async DM
	// confirmation arriving immediately after publish is accepted by
	// the registry (same pattern as tree requests in Stage 2).
	if err := o.registry.Register(cmd.JobID, []string{confirmCorrID}); err != nil {
		return err
	}
	if err := o.retryStep(ctx, func() error {
		return o.dmSender.SendDiffResult(ctx, diffReadyEvent)
	}); err != nil {
		o.registry.Cancel(cmd.JobID)
		return err
	}

	// --- Stage 6: WAITING_DM_CONFIRMATION ---
	job.Stage = model.ComparisonStageWaitingConfirm
	ctx = observability.WithStage(ctx, string(job.Stage))
	o.logger.Info(ctx, "pipeline stage started")
	confirmResponses, err := o.registry.AwaitAll(ctx, cmd.JobID)
	if err != nil {
		return err
	}
	for _, resp := range confirmResponses {
		if resp.Err != nil {
			return resp.Err
		}
	}

	// --- Transition: IN_PROGRESS -> COMPLETED / COMPLETED_WITH_WARNINGS ---
	// Per-job warning accumulator — placeholder for future extensibility.
	// The comparison pipeline does not currently produce engine-level warnings,
	// but uses the same pattern as the processing pipeline for consistency.
	allWarnings := make([]model.ProcessingWarning, 0)
	finalStatus := model.StatusCompleted
	if len(allWarnings) > 0 {
		finalStatus = model.StatusCompletedWithWarnings
	}
	if err := o.lifecycle.TransitionJob(ctx, job, finalStatus); err != nil {
		return err
	}

	// --- Publish ComparisonCompletedEvent ---
	completedEvent := model.ComparisonCompletedEvent{
		EventMeta: model.EventMeta{
			CorrelationID: cmd.JobID,
			Timestamp:     time.Now().UTC(),
		},
		JobID:               cmd.JobID,
		DocumentID:          cmd.DocumentID,
		BaseVersionID:       cmd.BaseVersionID,
		TargetVersionID:     cmd.TargetVersionID,
		Status:              finalStatus,
		TextDiffCount:       len(diffResult.TextDiffs),
		StructuralDiffCount: len(diffResult.StructuralDiffs),
	}
	if err := o.publisher.PublishComparisonCompleted(ctx, completedEvent); err != nil {
		return err
	}

	o.logger.Info(ctx, "comparison pipeline completed", "status", string(finalStatus), "text_diff_count", len(diffResult.TextDiffs), "structural_diff_count", len(diffResult.StructuralDiffs))

	return nil
}

// handlePipelineError handles a pipeline failure: transitions the job to the
// appropriate terminal status, publishes a ComparisonFailedEvent, and performs
// best-effort registry cancellation. All side effects use context.Background()
// since the job context may have expired (e.g. for TIMED_OUT errors).
func (o *Orchestrator) handlePipelineError(
	job *model.ComparisonJob,
	cmd model.CompareVersionsCommand,
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

	o.logger.Error(bgCtx, "comparison pipeline failed",
		"terminal_status", string(terminalStatus),
		"error_code", port.ErrorCode(pipelineErr),
		"error", pipelineErr,
		"failed_at_stage", string(job.Stage),
		"is_retryable", isRetryable,
	)

	// Transition job to terminal status (best-effort: log and continue on failure).
	// Skip if job is already in a terminal status (e.g. COMPLETED was set before
	// PublishComparisonCompleted failed) to avoid inconsistent state.
	if !job.Status.IsTerminal() {
		if err := o.lifecycle.TransitionJob(bgCtx, job, terminalStatus); err != nil {
			o.logger.Error(bgCtx, "failed to transition job to terminal status", "terminal_status", string(terminalStatus), "error", err)
		}
	}

	// Cancel any pending responses (best-effort: always safe to call).
	o.registry.Cancel(cmd.JobID)

	// Publish ComparisonFailedEvent (best-effort: log and continue on failure).
	failedEvent := model.ComparisonFailedEvent{
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
	if err := o.publisher.PublishComparisonFailed(bgCtx, failedEvent); err != nil {
		o.logger.Error(bgCtx, "failed to publish ComparisonFailedEvent", "error", err)
	}

	return pipelineErr
}

// HandleCompareVersions executes the full version comparison pipeline.
//
// The method creates a ComparisonJob, transitions it through each stage,
// and on success publishes a ComparisonCompletedEvent. On failure it
// transitions the job to REJECTED, FAILED, or TIMED_OUT, publishes a
// ComparisonFailedEvent, and performs best-effort registry cancellation.
func (o *Orchestrator) HandleCompareVersions(ctx context.Context, cmd model.CompareVersionsCommand) error {
	job := model.NewComparisonJob(cmd.JobID, cmd.DocumentID, cmd.BaseVersionID, cmd.TargetVersionID)
	job.OrgID = cmd.OrgID
	job.UserID = cmd.UserID

	o.logger.Info(ctx, "comparison pipeline started", "base_version_id", cmd.BaseVersionID, "target_version_id", cmd.TargetVersionID)

	// Create a job-scoped context with timeout.
	jobCtx, cancel := o.lifecycle.NewJobContext(ctx)
	defer cancel()

	if err := o.runPipeline(jobCtx, job, cmd); err != nil {
		return o.handlePipelineError(job, cmd, err)
	}
	return nil
}
