// Package pipeline implements the LIC Pipeline Orchestrator (LIC-TASK-036,
// high-architecture.md §6.5 / §6.12 / §6.14 / §8.3 / §8.7 / §8.10,
// observability.md §3.2 / §4.2, error-handling.md §3). It is the coordinating
// body that runs one analysis job end-to-end:
//
//	Acquire job semaphore → WithTimeout(90s) → root span → build state →
//	publish IN_PROGRESS → resolve parent + mode → request current (+RE_CHECK
//	parent) artifacts → await current → MAX_INGESTED_BYTES cap → await parent
//	(degrade, never fail) → Stage 1 → confidence pause gate → Stage 2/3 →
//	aggregator MERGE-EARLY → Stage 4/5/6 → aggregator FINALIZE-LATE → build
//	payload (null risk_delta on parent-missing) → publish analysis-ready →
//	await persist → publish COMPLETED.
//
// It owns the job-level context.WithTimeout, the stage sequencing, the two
// aggregator calls (merge-early before Stage 4, finalize-late after Stage 6),
// the confidence pause gate, the single terminal-status-publish path, the
// error→status mapping, and nulling outbound risk_delta on a missing parent.
// It does NOT own broker ACK/NACK (LIC-TASK-040 — it returns a typed error
// instead), the lic-trigger idempotency guard / restart-semantics
// (LIC-TASK-040), or the real pause body (LIC-TASK-037's
// pendingconfirmation.Manager — invoked via the PauseController seam).
//
// LIC-TASK-037 extension: the Stage-2..21 body is extracted into the shared
// continueFromStage2 continuation, reused by ResumeAfterConfirmation — the
// exported resume entrypoint that re-establishes the Run wrapper for an
// already-classified (user-confirmed) state and runs Stage 2..21 without
// Stage 1 / artifact-request / the confidence gate. When the real
// PauseController completes a pause it returns ErrPipelinePaused; Run's
// classifyOutcome routes it to the non-failure outcomePaused (no FAILED
// published) and Run returns the sentinel for LIC-TASK-040 to map to "ACK
// the source, no COMPLETED". IsPaused is the exported predicate for that.
//
// Hermetic: stdlib + internal/domain/{model,port} +
// internal/application/{pipeline/stages,aggregator} only (build-spec §7
// allowlist, enforced by internal_test.go). Telemetry / clock / logger /
// version-meta-cache / job-limiter / pause-controller are inverted behind
// the seams in seams.go (the stages / aggregator / concurrency seam
// precedent). It awaits sequentially and delegates parallelism to
// stages.Executor, so it imports no errgroup (golang.org/x/sync is absent).
//
// Design adjudicated by subagent code-architect (build spec — decisions
// D1..D14, defects DEFECT-1..4); implemented by subagent golang-pro. The
// authoritative reconciliations are recorded in this package's CLAUDE.md.
package pipeline

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"contractpro/legal-intelligence-core/internal/application/aggregator"
	"contractpro/legal-intelligence-core/internal/application/pipeline/stages"
	"contractpro/legal-intelligence-core/internal/domain/model"
	"contractpro/legal-intelligence-core/internal/domain/port"
)

// Default timeouts (build-spec §4). LIC-TASK-047 overrides these from
// config.PipelineConfig / config.DMConfig.
const (
	defaultJobTimeout              = 90 * time.Second
	defaultDMRequestTimeout        = 30 * time.Second
	defaultDMPersistConfirmTimeout = 30 * time.Second
)

// Config is the local Orchestrator config (build-spec §4). No internal/config
// import — LIC-TASK-047 builds this from config.PipelineConfig/DMConfig (the
// aggregator.Config ctor-param precedent). Validated by NewOrchestrator.
type Config struct {
	// JobTimeout is the Run-level context.WithTimeout budget (LIC_JOB_TIMEOUT,
	// default 90s, high-arch §8.10 step 1). Covers pipeline work only, NOT
	// the time queued behind the concurrency cap.
	JobTimeout time.Duration
	// DMRequestTimeout is the per-artifacts-await sub-context deadline
	// (LIC_DM_REQUEST_TIMEOUT, default 30s).
	DMRequestTimeout time.Duration
	// DMPersistConfirmTimeout is the persist-await sub-context deadline
	// (LIC_DM_PERSIST_CONFIRM_TIMEOUT, default 30s).
	DMPersistConfirmTimeout time.Duration
	// ConfidenceThreshold gates the Stage-1 pause (compared against
	// ClassificationResult.Confidence); must be in [0,1]
	// (LIC_CONFIDENCE_THRESHOLD).
	ConfidenceThreshold float64
	// MaxIngestedBytes is the build-spec-D8 inline cap over the sum of raw
	// artifact bytes (LIC_MAX_INGESTED_BYTES); no Token Estimator dependency
	// in 036 v1.
	MaxIngestedBytes int
}

// validate fails fast on misconfiguration (the aggregator.Config.validate /
// stages.NewExecutor precedent — a wiring defect must be a startup error).
func (c Config) validate() error {
	var errs []error
	if c.JobTimeout <= 0 {
		errs = append(errs, errors.New("pipeline: Config.JobTimeout must be > 0"))
	}
	if c.DMRequestTimeout <= 0 {
		errs = append(errs, errors.New("pipeline: Config.DMRequestTimeout must be > 0"))
	}
	if c.DMPersistConfirmTimeout <= 0 {
		errs = append(errs, errors.New("pipeline: Config.DMPersistConfirmTimeout must be > 0"))
	}
	if c.ConfidenceThreshold < 0 || c.ConfidenceThreshold > 1 {
		errs = append(errs, fmt.Errorf("pipeline: Config.ConfidenceThreshold must be in [0,1], got %v", c.ConfidenceThreshold))
	}
	if c.MaxIngestedBytes <= 0 {
		errs = append(errs, errors.New("pipeline: Config.MaxIngestedBytes must be > 0"))
	}
	if c.DMRequestTimeout >= c.JobTimeout {
		errs = append(errs, fmt.Errorf("pipeline: DMRequestTimeout (%v) must be < JobTimeout (%v)", c.DMRequestTimeout, c.JobTimeout))
	}
	return errors.Join(errs...)
}

// requiredArtifacts is the current-version artifact set the pipeline cannot
// proceed without (high-arch §6.5 step 1). Order is the wire order.
var requiredArtifacts = []model.ArtifactType{
	model.ArtifactSemanticTree,
	model.ArtifactExtractedText,
	model.ArtifactDocumentStructure,
	model.ArtifactProcessingWarnings,
}

// Orchestrator coordinates one analysis job. Immutable after NewOrchestrator;
// one instance is shared across concurrent jobs — each Run builds its own
// *model.PipelineState and the held collaborators (stages.Executor /
// aggregator.Aggregator are concurrency-safe per their CLAUDE.md; the ports
// are stateless registries; the seams are immutable). It holds NO per-job
// mutable state.
type Orchestrator struct {
	cfg Config

	// --- required collaborators: the two concrete engines + the cross-domain
	// ports (build-spec §2.1). The engines are concrete (sanctioned by the
	// §7 allowlist); everything crossing to an unimplemented package is a
	// port, never a concrete type.
	exec         *stages.Executor
	agg          *aggregator.Aggregator
	artReq       port.ArtifactRequesterPort
	artAwait     port.ArtifactsAwaiterPort
	analysisPub  port.AnalysisArtifactsPublisherPort
	persistAwait port.PersistConfirmationAwaiterPort
	statusPub    port.StatusPublisherPort
	// uncertainPub is held so LIC-TASK-037's real PauseController wiring needs
	// no NewOrchestrator change (build-spec §2.1). The happy-path body does
	// not use it directly.
	uncertainPub port.UncertaintyPublisherPort

	// --- seams (always non-nil after Deps.withDefaults) ---
	limiter   JobLimiter
	metrics   PipelineMetrics
	tracer    Tracer
	clock     Clock
	log       Logger
	metaCache VersionMetaCache
	pause     PauseController
}

// NewOrchestrator validates the wiring and assembles the Orchestrator. It
// fails fast (NewTypeName per feedback_constructors.md; the
// stages.NewExecutor / aggregator.NewAggregator precedent): every required
// collaborator that is nil — or an invalid Config — is a LIC-TASK-047 wiring
// defect and must be a startup error, not a first-job nil-deref. errors.Join
// surfaces ALL defects at once. Optional seams never cause an error;
// Deps.withDefaults substitutes a noop for each nil one.
func NewOrchestrator(
	cfg Config,
	exec *stages.Executor,
	agg *aggregator.Aggregator,
	artReq port.ArtifactRequesterPort,
	artAwait port.ArtifactsAwaiterPort,
	analysisPub port.AnalysisArtifactsPublisherPort,
	persistAwait port.PersistConfirmationAwaiterPort,
	statusPub port.StatusPublisherPort,
	uncertainPub port.UncertaintyPublisherPort,
	deps Deps,
) (*Orchestrator, error) {
	var errs []error
	if err := cfg.validate(); err != nil {
		errs = append(errs, err)
	}
	if exec == nil {
		errs = append(errs, errors.New("pipeline: exec (*stages.Executor) must not be nil"))
	}
	if agg == nil {
		errs = append(errs, errors.New("pipeline: agg (*aggregator.Aggregator) must not be nil"))
	}
	if artReq == nil {
		errs = append(errs, errors.New("pipeline: artReq (port.ArtifactRequesterPort) must not be nil"))
	}
	if artAwait == nil {
		errs = append(errs, errors.New("pipeline: artAwait (port.ArtifactsAwaiterPort) must not be nil"))
	}
	if analysisPub == nil {
		errs = append(errs, errors.New("pipeline: analysisPub (port.AnalysisArtifactsPublisherPort) must not be nil"))
	}
	if persistAwait == nil {
		errs = append(errs, errors.New("pipeline: persistAwait (port.PersistConfirmationAwaiterPort) must not be nil"))
	}
	if statusPub == nil {
		errs = append(errs, errors.New("pipeline: statusPub (port.StatusPublisherPort) must not be nil"))
	}
	if uncertainPub == nil {
		errs = append(errs, errors.New("pipeline: uncertainPub (port.UncertaintyPublisherPort) must not be nil"))
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}

	d := deps.withDefaults()
	return &Orchestrator{
		cfg:          cfg,
		exec:         exec,
		agg:          agg,
		artReq:       artReq,
		artAwait:     artAwait,
		analysisPub:  analysisPub,
		persistAwait: persistAwait,
		statusPub:    statusPub,
		uncertainPub: uncertainPub,
		limiter:      d.JobLimiter,
		metrics:      d.Metrics,
		tracer:       d.Tracer,
		clock:        d.Clock,
		log:          d.Logger,
		metaCache:    d.VersionMetaCache,
		pause:        d.PauseController,
	}, nil
}

// Run executes one analysis job for trigger. It returns nil iff the pipeline
// reached COMPLETED (analysis-ready published, persist confirmed, COMPLETED
// status published) — the caller (LIC-TASK-040) then ACKs the broker
// delivery (high-arch §6.5 step 11). On any failure it returns a non-nil
// *model.DomainError AFTER having published the terminal FAILED status itself
// (the single-publish path, build-spec §5); the returned error exists solely
// so LIC-TASK-040 can make the broker ACK/NACK decision via
// model.IsRetryable. Run never touches the broker delivery (no broker handle —
// hermeticity).
//
// On a low-confidence classification Run calls the PauseController seam: the
// real LIC-TASK-037 impl returns ErrPipelinePaused, which classifyOutcome
// maps to outcomePaused (no FAILED published) so Run returns the sentinel
// (LIC-TASK-040 ACKs the source, no COMPLETED); the noop returns a terminal
// non-retryable INTERNAL_ERROR (Pin 9). The post-confirmation resume runs via
// the separate ResumeAfterConfirmation entrypoint, NOT Run.
//
// Run is goroutine-safe for distinct triggers: it builds its own
// *model.PipelineState and the Orchestrator holds only immutable
// config/collaborators. It does NOT handle the resume body, the idempotency
// guard, or restart-semantics (LIC-TASK-037 / LIC-TASK-040).
func (o *Orchestrator) Run(ctx context.Context, trigger port.VersionProcessingArtifactsReady) (runErr error) {
	start := o.clock.Now()

	// Step 0 — provisional mode label for the started/duration metrics and
	// the root span. DEFECT-1: the trigger pointer is the primary RE_CHECK
	// signal; the cache fallback is rare and the *outcome* metric uses the
	// resolved mode (set at step 6). The started/duration label is fixed
	// here because PipelineStarted cannot be relabelled later.
	modeLabel := string(model.PipelineModeInitial)
	if trigger.ParentVersionID != nil {
		modeLabel = string(model.PipelineModeReCheck)
	}
	o.metrics.PipelineStarted(modeLabel)

	// Step 1 — acquire the job semaphore on the RAW inbound ctx (NOT the
	// timeout-wrapped ctx): a job queued behind the concurrency cap is
	// "queued", not "timed out" (high-arch §6.14 backpressure). The 90s
	// budget must cover only pipeline work. Acquire returns the raw
	// context.Canceled/DeadlineExceeded (semaphore.go:111-114) — branch via
	// errors.Is, never AsDomainError.
	if err := o.limiter.Acquire(ctx); err != nil {
		// Pre-pipeline failure: still publish FAILED ("на любой ошибке").
		// No state yet → use a minimal one for correlation.
		st := o.newState(trigger)
		st.CurrentStage = model.StageReceived
		var de *model.DomainError
		var outcome string
		if errors.Is(err, context.DeadlineExceeded) {
			de = model.NewDomainError(model.ErrCodeAnalysisTimeout, model.StageReceived).WithCause(err)
			outcome = outcomeTimeout
		} else {
			de = model.NewDomainError(model.ErrCodeInternal, model.StageReceived).WithCause(err)
			outcome = outcomeFailed
		}
		o.finalizePrePipeline(ctx, st, de, modeLabel, outcome, start)
		return de
	}
	// Release is registered FIRST (right after a successful Acquire) so it
	// runs LAST (defer LIFO): the slot is held for the entire job including
	// the terminal publish and span close (high-arch §6.14 "slot held while
	// in-flight"; the gauge stays accurate). concurrency.Semaphore PANICS on
	// missing/double Release — exactly one Release per successful Acquire.
	defer o.limiter.Release()

	// Step 2 — job timeout, INSIDE the held semaphore region (high-arch
	// §8.10 step 1).
	rootCtx, cancel := context.WithTimeout(ctx, o.cfg.JobTimeout)
	defer cancel()

	// Step 3 — root span. All downstream work uses spanCtx so the per-stage
	// spans opened inside stages.Executor nest beneath it.
	spanCtx, span := o.tracer.StartPipeline(rootCtx, PipelineSpanAttrs{
		JobID:      trigger.JobID,
		VersionID:  trigger.VersionID,
		Mode:       modeLabel,
		OriginType: trigger.OriginType,
	})

	// Step 4 — per-job state.
	state := o.newState(trigger)

	// Step 5 — the single terminal-outcome path. The body returns the raw
	// error (or nil); this finalizer is the SOLE site that classifies the
	// outcome (D11 timeout discriminator), records the exit metrics, closes
	// the span, and — on failure — publishes the single terminal FAILED
	// status event. The success COMPLETED publish happens inline at step 21
	// so the persist-confirm ordering is preserved; this finalizer must NOT
	// double-publish on success.
	//
	// resolvedMode is read at finalize time: step 6 sets state.Mode, so by
	// the time the body returns it reflects the cache-fallback outcome.
	defer func() {
		bodyErr := runErr // the named return carries the body's error here
		outcome, de := o.classifyOutcome(rootCtx, state, bodyErr)
		resolvedMode := string(state.Mode)

		o.metrics.PipelineFinished(resolvedMode, outcome, o.clock.Since(start).Seconds())
		o.metrics.PipelineOutcome(resolvedMode, outcome, codeLabelFor(outcome, de))

		switch outcome {
		case outcomeSuccess:
			span.Finish(nil)
			runErr = nil
		case outcomePaused:
			// The PauseController (LIC-TASK-037) already published
			// classification-uncertain + IN_PROGRESS{STAGE_AWAITING_USER_
			// CONFIRMATION} with broker confirms. Do NOT publish FAILED.
			// Close the span cleanly and surface the sentinel so
			// LIC-TASK-040 ACKs the source without COMPLETED (build-spec
			// D3 / RECONCILIATION R1).
			span.Finish(nil)
			runErr = ErrPipelinePaused
		default: // outcomeFailed / outcomeTimeout
			o.publishFailed(spanCtx, state, de)
			span.Finish(de)
			runErr = de
		}
	}()

	// --- Body (steps 6..21) ---------------------------------------------

	// Step 6 — publish IN_PROGRESS{STAGE_REQUESTING_ARTIFACTS} and resolve
	// the parent + mode.
	state.CurrentStage = model.StageRequestingArtifacts
	if err := o.statusPub.PublishStatus(spanCtx, o.statusEvent(trigger, model.StatusInProgress, model.StageRequestingArtifacts, nil)); err != nil {
		return model.NewDomainError(model.ErrCodeInternal, model.StageRequestingArtifacts).WithCause(err)
	}
	o.resolveParentAndMode(spanCtx, trigger, state)

	// Step 7 — register the awaiter(s) BEFORE any request (ArtifactsAwaiter
	// godoc: Register MUST precede the publish or the response races ahead
	// of the slot). Cancel is deferred at THIS level so the registry stays
	// bounded even if a later step fails before the parent await runs (the
	// build-spec §5 defer-LIFO: parCorr Cancel after curCorr Cancel).
	curCorr := trigger.CorrelationID + ":current"
	if _, regErr := o.artAwait.Register(curCorr); regErr != nil {
		return model.NewDomainError(model.ErrCodeInternal, model.StageRequestingArtifacts).
			WithCause(regErr).
			WithDevMessage("current-artifacts awaiter Register failed (LIC-TASK-040 routing/idempotency defect)")
	}
	defer o.artAwait.Cancel(curCorr)

	parCorr := trigger.CorrelationID + ":parent"
	if state.Mode == model.PipelineModeReCheck {
		if _, regErr := o.artAwait.Register(parCorr); regErr != nil {
			return model.NewDomainError(model.ErrCodeInternal, model.StageRequestingArtifacts).
				WithCause(regErr).
				WithDevMessage("parent-artifacts awaiter Register failed (LIC-TASK-040 routing/idempotency defect)")
		}
		defer o.artAwait.Cancel(parCorr)
	}

	// Step 7 (cont.) — fire the request(s) back-to-back, then step 8 awaits
	// the CURRENT artifacts (the parent await is step 10 — sequential, not
	// parallel: build-spec §5 step-7 justification).
	provided, err := o.requestAndAwaitCurrent(spanCtx, span, trigger, state, curCorr, parCorr)
	if err != nil {
		return err
	}

	// Step 9 — D8 inline LIC_MAX_INGESTED_BYTES cap (no Token Estimator).
	total := 0
	for _, raw := range provided.Artifacts {
		total += len(raw)
	}
	if total > o.cfg.MaxIngestedBytes {
		return model.NewDomainError(model.ErrCodeDocumentTooLarge, model.StageArtifactsReceived).
			WithRetryable(false).
			WithAttribute("ingested_bytes", total).
			WithAttribute("limit", o.cfg.MaxIngestedBytes)
	}
	state.InputArtifacts = model.InputArtifactsCompact(provided.Artifacts)

	// Step 10 — await parent RISK_ANALYSIS (RE_CHECK only): DEGRADE, never
	// fail (build-spec D7). parentMissing drives aggregator.Input and the
	// outbound risk_delta nulling.
	parentMissing := o.awaitParentAnalysis(spanCtx, span, trigger, state, parCorr)

	// Step 11 — Stage 1 (Classifier ‖ KeyParams). Verbatim DomainError
	// propagation (the executor stamps de.Stage via canonicalStage).
	if err := o.exec.RunStage1(spanCtx, state); err != nil {
		return err
	}

	// Step 12 — confidence pause gate. Agent 1 is critical: a nil result is
	// a breach.
	if state.Classification == nil {
		return model.NewDomainError(model.ErrCodeInternal, model.StageAgentTypeClassifier).
			WithDevMessage("Agent 1 returned nil ClassificationResult (critical-agent breach)")
	}
	if state.Classification.Confidence < o.cfg.ConfidenceThreshold {
		state.CurrentStage = model.StageAwaitingUserConfirmation
		// With the noop PauseController this returns the terminal
		// non-retryable ErrCodeInternal (D5/DEFECT-3). With LIC-TASK-037's
		// real impl it returns the paused sentinel — the finalizer's
		// classifyOutcome gains an errors.Is branch then (forward note).
		return o.pause.Pause(spanCtx, state)
	}

	// Step 13 — the shared Stage-2..21 continuation (build-spec D2). It is
	// identical for Run (post confidence-gate) and ResumeAfterConfirmation
	// (post user-confirmed-type); extracting it keeps the ~80 lines of
	// pin-covered ordering single-sourced.
	return o.continueFromStage2(spanCtx, span, trigger, state, parentMissing)
}

// continueFromStage2 is the shared Stage-2..21 continuation used by BOTH
// Run (post confidence-gate, INITIAL/RE_CHECK) and ResumeAfterConfirmation
// (post user-confirmed-type). It assumes Stage 1 is complete and
// state.Classification is non-nil & validated. spanCtx carries the root
// span; span is its handle; parentMissing is the RE_CHECK parent-degrade
// flag resolved by the caller. This body is the VERBATIM old Run steps
// 13..21 — a pure, behavior-preserving refactor (build-spec D2 point 1/2,
// Pin 23): Run and ResumeAfterConfirmation produce byte-identical payloads
// for the same post-Stage-1 state.
func (o *Orchestrator) continueFromStage2(
	spanCtx context.Context,
	span PipelineSpan,
	trigger port.VersionProcessingArtifactsReady,
	state *model.PipelineState,
	parentMissing bool,
) error {
	// Step 13 — Stage 2 (Party Consistency, non-critical: a returned err is
	// a genuine non-timeout fatal — the executor already degraded a
	// per-agent AGENT_TIMEOUT internally; D6). 036 does NOT re-derive
	// degradation from nil fields (the trace Degraded event is the SSOT).
	if err := o.exec.RunStage2(spanCtx, state); err != nil {
		return err
	}

	// Step 14 — Stage 3 (Mandatory ‖ Risk Detection).
	if err := o.exec.RunStage3(spanCtx, state); err != nil {
		return err
	}

	// Step 15 — aggregator MERGE-EARLY (build-spec D3, the load-bearing
	// invariant: stages.RunStage4 reads state.MergedRiskAnalysis via
	// buildInput; if 036 skips this, Agent 6 fails INTERNAL_ERROR).
	if err := o.mergeEarly(spanCtx, span, state, parentMissing); err != nil {
		return err
	}

	// Step 16 — Stage 4 (Recommendation), Stage 5 (Summary ‖ Detailed
	// Report), Stage 6 (Risk Delta — self-gates on
	// Mode==RE_CHECK && ParentRiskAnalysis != nil).
	if err := o.exec.RunStage4(spanCtx, state); err != nil {
		return err
	}
	if err := o.exec.RunStage5(spanCtx, state); err != nil {
		return err
	}
	if err := o.exec.RunStage6(spanCtx, state); err != nil {
		return err
	}

	// Step 17 — aggregator FINALIZE-LATE (build-spec D3): re-run over the
	// now-complete state. Aggregate is pure & idempotent over a fixed Input
	// (aggregator D5) so the second call is safe; its Warnings now include
	// the full-Recommendations cross-agent checks.
	out2, err := o.finalizeLate(spanCtx, span, state, parentMissing)
	if err != nil {
		return err
	}

	// Step 18 — build the outbound payload + null risk_delta on
	// parent-missing (high-arch §8.7 step 4 — Orchestrator-owned, NOT
	// aggregator).
	payload := o.buildPayload(trigger, state, out2, parentMissing)

	// Step 19 — register the persist awaiter BEFORE publishing
	// (PersistConfirmationAwaiterPort godoc; key = job_id), then publish
	// analysis-ready.
	state.CurrentStage = model.StagePublishingArtifacts
	pubCtx, pubSpan := span.StartChild(spanCtx, "publish")
	if _, regErr := o.persistAwait.Register(trigger.JobID); regErr != nil {
		pubSpan.Finish(nil)
		return model.NewDomainError(model.ErrCodeInternal, model.StagePublishingArtifacts).
			WithCause(regErr).
			WithDevMessage("persist awaiter Register failed (LIC-TASK-040 routing/idempotency defect)")
	}
	defer o.persistAwait.Cancel(trigger.JobID)
	if pubErr := o.analysisPub.Publish(pubCtx, payload); pubErr != nil {
		pubSpan.Finish(pubErr)
		return model.NewDomainError(model.ErrCodeInternal, model.StagePublishingArtifacts).WithCause(pubErr)
	}
	pubSpan.Finish(nil)

	// Step 20 — await persist confirmation.
	state.CurrentStage = model.StageAwaitingDMConfirmation
	if err := o.awaitPersist(spanCtx, span, trigger); err != nil {
		return err
	}

	// Step 21 — publish COMPLETED inline (preserves persist-confirm
	// ordering; the finalizer must not double-publish on success).
	state.CurrentStage = model.StageDone
	if cErr := o.statusPub.PublishStatus(spanCtx, o.statusEvent(trigger, model.StatusCompleted, model.StageDone, nil)); cErr != nil {
		return model.NewDomainError(model.ErrCodeInternal, model.StageDone).
			WithCause(cErr).
			WithDevMessage("work done but COMPLETED status publish failed; LIC-TASK-040 will NACK→redeliver (idempotency-guarded)")
	}
	return nil
}

// ResumeAfterConfirmation continues a paused pipeline from Stage 2
// (LIC-TASK-037, high-arch §6.10 Resume steps 6..9). It re-establishes the
// Run wrapper (semaphore slot → WithTimeout(JobTimeout) → root span linked
// to the saved trace context → single-terminal-finalizer) for an ALREADY-
// classified state (Stage 1 done, Classification overridden by the user),
// then runs the shared continuation (Stage 2..21). It does NOT request
// current artifacts, does NOT run Stage 1, does NOT consult the confidence
// gate. The caller (pendingconfirmation.Manager, via the PipelineResumer
// seam) has already SETNX'd lic-user-confirmed, validated the contract_type,
// loaded + restored the pending state, overridden the classification, and
// (via the TraceRestorer seam) seeded ctx with the saved W3C trace context.
//
// Returns nil ⇒ COMPLETED (the caller performs §6.10 step-9 cleanup and
// ACKs); a non-nil *model.DomainError AFTER publishing the terminal FAILED
// itself (the SAME single-publish finalizer as Run — there is exactly one
// terminal-status publish per invocation). It is a method ON the
// Orchestrator because resume re-acquires a JobLimiter slot (build-spec D9)
// and the orchestrator owns the limiter; the Manager does not and must not.
func (o *Orchestrator) ResumeAfterConfirmation(ctx context.Context, state *model.PipelineState) (runErr error) {
	start := o.clock.Now()

	// Synthesize the trigger the Stage-2..21 helpers consume (statusEvent /
	// buildPayload / persist Register key / publishFailed) from the restored
	// state (build-spec D2 point 4). Timestamp/ArtifactTypes intentionally
	// empty — helpers re-clock Timestamp via o.clock.Now() and never read
	// ArtifactTypes post-Stage-1.
	trigger := port.VersionProcessingArtifactsReady{
		CorrelationID:   state.CorrelationID,
		JobID:           state.JobID,
		DocumentID:      state.DocumentID,
		VersionID:       state.VersionID,
		OrganizationID:  state.OrganizationID,
		OriginType:      state.OriginType,
		ParentVersionID: state.ParentVersionID,
		CreatedByUserID: state.CreatedByUserID,
	}

	// Step R1 — metrics. A resumed run counts as a fresh pipeline start
	// (§6.10:784 "fresh budget"; the metric reflects pipeline executions).
	modeLabel := string(state.Mode)
	o.metrics.PipelineStarted(modeLabel)

	// Step R2 — acquire the job semaphore on the RAW inbound ctx (a resume
	// queued behind the cap is "queued", not "timed out" — the 036 binding
	// rule). Resume DOES re-acquire a slot: it runs Stage 2..6, full
	// pipeline cost (build-spec D9; §6.14 backpressure).
	if err := o.limiter.Acquire(ctx); err != nil {
		state.CurrentStage = model.StageReceived
		var de *model.DomainError
		var outcome string
		if errors.Is(err, context.DeadlineExceeded) {
			de = model.NewDomainError(model.ErrCodeAnalysisTimeout, model.StageReceived).WithCause(err)
			outcome = outcomeTimeout
		} else {
			de = model.NewDomainError(model.ErrCodeInternal, model.StageReceived).WithCause(err)
			outcome = outcomeFailed
		}
		o.finalizePrePipeline(ctx, state, de, modeLabel, outcome, start)
		return de
	}
	// Registered FIRST after success ⇒ runs LAST (defer LIFO): the slot is
	// held through the terminal publish + span close, identical to Run.
	defer o.limiter.Release()

	// Step R3 — fresh full JobTimeout budget for Stage 2..5 (high-arch:784 —
	// deliberate, NOT elapsed-aware).
	rootCtx, cancel := context.WithTimeout(ctx, o.cfg.JobTimeout)
	defer cancel()

	// Step R4 — root span linked to the saved trace context. The link is
	// established by the caller seeding ctx via the TraceRestorer seam BEFORE
	// calling here; StartPipeline opens a span as a child of whatever trace
	// context ctx carries (the real adapter, LIC-TASK-047, continues the
	// remote context across the pause boundary; the noopTracer is a no-op).
	spanCtx, span := o.tracer.StartPipeline(rootCtx, PipelineSpanAttrs{
		JobID:      trigger.JobID,
		VersionID:  trigger.VersionID,
		Mode:       modeLabel,
		OriginType: trigger.OriginType,
	})

	// Step R5 — the SAME single-terminal-outcome deferred finalizer as Run
	// step 5 (replicated structurally). classifyOutcome is reused unchanged,
	// so a Stage-2..5 overrun of the fresh rootCtx is correctly classified
	// timeout (build-spec D2 invariant-preservation).
	defer func() {
		bodyErr := runErr
		outcome, de := o.classifyOutcome(rootCtx, state, bodyErr)
		resolvedMode := string(state.Mode)

		o.metrics.PipelineFinished(resolvedMode, outcome, o.clock.Since(start).Seconds())
		o.metrics.PipelineOutcome(resolvedMode, outcome, codeLabelFor(outcome, de))

		switch outcome {
		case outcomeSuccess:
			span.Finish(nil)
			runErr = nil
		case outcomePaused:
			// Unreachable on the resume path (no confidence gate here), but
			// the same 3-way switch is kept for structural parity with Run.
			span.Finish(nil)
			runErr = ErrPipelinePaused
		default: // outcomeFailed / outcomeTimeout
			o.publishFailed(spanCtx, state, de)
			span.Finish(de)
			runErr = de
		}
	}()

	// Step R6 — Stage stamp + IN_PROGRESS{STAGE_AGENT_PARTY_CONSISTENCY}
	// publish (§6.10 Resume step 6 / build-spec D19). The orchestrator owns
	// statusEvent; the Manager only publishes the pause events.
	state.CurrentStage = model.StageAgentPartyConsistency
	if err := o.statusPub.PublishStatus(spanCtx, o.statusEvent(trigger, model.StatusInProgress, model.StageAgentPartyConsistency, nil)); err != nil {
		return model.NewDomainError(model.ErrCodeInternal, model.StageAgentPartyConsistency).WithCause(err)
	}

	// Step R7 — RE_CHECK parent re-fetch (build-spec D8): re-request +
	// re-await the parent RISK_ANALYSIS with the SAME degrade-never-fail
	// pattern as Run; current artifacts are NOT re-fetched (Stage 1 already
	// consumed state.InputArtifacts, restored from the pending blob).
	parentMissing, refetchErr := o.reCheckParentRefetchForResume(spanCtx, span, trigger, state)
	if refetchErr != nil {
		return refetchErr
	}

	// Step R8 — the shared Stage-2..21 continuation (build-spec D2).
	return o.continueFromStage2(spanCtx, span, trigger, state, parentMissing)
}

// reCheckParentRefetchForResume houses the build-spec D8 step-R7 block: for a
// RE_CHECK resume it re-requests and re-awaits the parent RISK_ANALYSIS using
// a DISTINCT ":parent:resume" correlation suffix (so a stray late delivery of
// the original pre-pause ":parent" request cannot be misrouted to the resumed
// awaiter slot, and vice-versa) and the SAME degrade-never-fail semantics as
// Run's awaitParentAnalysis. It returns parentMissing for the continuation
// (stages.RunStage6 self-gates on a nil ParentRiskAnalysis; the aggregator
// renders RE_CHECK_PARENT_ANALYSIS_MISSING; buildPayload nulls outbound
// risk_delta). The only non-degrade error is an awaiter Register failure
// (a LIC-TASK-040 routing/idempotency defect — INTERNAL_ERROR), kept fatal
// exactly as Run step 7. INITIAL ⇒ (false, nil) immediately.
func (o *Orchestrator) reCheckParentRefetchForResume(
	spanCtx context.Context,
	span PipelineSpan,
	trigger port.VersionProcessingArtifactsReady,
	state *model.PipelineState,
) (parentMissing bool, fatalErr error) {
	if state.Mode != model.PipelineModeReCheck {
		return false, nil
	}
	parCorr := state.CorrelationID + ":parent:resume"
	if _, regErr := o.artAwait.Register(parCorr); regErr != nil {
		return false, model.NewDomainError(model.ErrCodeInternal, model.StageAgentPartyConsistency).
			WithCause(regErr).
			WithDevMessage("resume: parent-artifacts awaiter Register failed (LIC-TASK-040 routing/idempotency defect)")
	}
	defer o.artAwait.Cancel(parCorr)

	if rErr := o.artReq.RequestArtifacts(spanCtx, parCorr, trigger.JobID, trigger.DocumentID,
		*state.ParentVersionID, trigger.OrganizationID,
		[]model.ArtifactType{model.ArtifactRiskAnalysis}); rErr != nil {
		// DEGRADE: a failed parent request is non-fatal (036 D7 / D8).
		o.log.Warn(spanCtx, "resume RE_CHECK parent request failed; degrading",
			"version_id", state.VersionID, "cause", rErr)
		return true, nil
	}
	return o.awaitParentAnalysis(spanCtx, span, trigger, state, parCorr), nil
}

// outcome label constants (build-spec §3.1 PipelineMetrics godoc).
const (
	outcomeSuccess = "success"
	outcomeFailed  = "failed"
	outcomeTimeout = "timeout"
	// outcomePaused is the LIC-TASK-037 non-terminal, non-failure outcome:
	// the confidence gate fired and the real PauseController completed the
	// pause (pending-state saved, classification-uncertain + IN_PROGRESS
	// published with broker confirms, lic-trigger=PAUSED). It is a NEW
	// lic_pipeline_outcome_total label value beyond 036's success/failed/
	// timeout (build-spec RECONCILIATION R7 — a deliberate, recorded
	// extension: a paused run is neither a success nor a failure, and
	// mislabelling it as either corrupts the success/failure SLOs).
	outcomePaused = "paused"
)

// ErrPipelinePaused is the distinct sentinel returned by Run when the
// confidence gate fired and the real PauseController (LIC-TASK-037) completed
// the pause (pending-state saved, classification-uncertain + IN_PROGRESS
// published with broker confirms, lic-trigger=PAUSED). It is NOT a
// *model.DomainError and NOT a failure: LIC-TASK-040 maps it to "ACK the
// source dm.events.version-artifacts-ready, do NOT publish COMPLETED, do NOT
// publish FAILED" (high-arch §6.5 step 5 / §6.10 Pause step 6). The noop
// PauseController does NOT return this (it returns a terminal non-retryable
// INTERNAL_ERROR — DEFECT-3), so Pin 9 is intact (build-spec D3).
//
// LIC-TASK-037's *pendingconfirmation.Manager does NOT import this package; it
// returns this exact value because LIC-TASK-047 injects it as the Manager's
// Config.PausedSentinel (build-spec D5). errors.Is succeeds across that
// boundary because it is the same error value (identity-comparable).
var ErrPipelinePaused = errors.New("pipeline: paused awaiting user type confirmation")

// IsPaused reports whether err is (or wraps) ErrPipelinePaused. LIC-TASK-040
// calls this BEFORE model.IsRetryable in its ACK/NACK decision (build-spec
// D3): IsPaused(err)==true ⇒ ACK the source without COMPLETED/FAILED.
func IsPaused(err error) bool { return errors.Is(err, ErrPipelinePaused) }

// newState builds the per-job PipelineState from the trigger. Mode stays
// INITIAL here; resolveParentAndMode (step 6) flips it for RE_CHECK.
func (o *Orchestrator) newState(trigger port.VersionProcessingArtifactsReady) *model.PipelineState {
	st := model.NewPipelineState(
		trigger.CorrelationID,
		trigger.JobID,
		trigger.DocumentID,
		trigger.VersionID,
		trigger.OrganizationID,
	)
	st.CreatedByUserID = trigger.CreatedByUserID
	st.OriginType = trigger.OriginType
	st.StartedAt = o.clock.Now()
	// LIC-TASK-040 propagates W3C trace context; absent ⇒ zero value.
	st.TraceContext = model.TraceContext{}
	return st
}

// resolveParentAndMode applies the DEFECT-1 rule: trigger.ParentVersionID is
// the PRIMARY RE_CHECK source; the VersionMetaCache is consulted IFF the
// trigger lacks it (the §8.3 race). A cache miss/error is NOT a failure — it
// degrades to INITIAL (high-arch:1069-1070).
func (o *Orchestrator) resolveParentAndMode(ctx context.Context, trigger port.VersionProcessingArtifactsReady, state *model.PipelineState) {
	parentVID := trigger.ParentVersionID
	if parentVID == nil {
		if cached, cErr := o.metaCache.GetParentVersionID(ctx, trigger.VersionID); cErr == nil {
			parentVID = cached
		}
		// cErr != nil ⇒ treat exactly like a miss (degrade to INITIAL).
	}
	state.ParentVersionID = parentVID
	if parentVID != nil {
		state.Mode = model.PipelineModeReCheck
	}
}

// requestAndAwaitCurrent fires the request(s) and awaits the CURRENT
// artifacts. The awaiter slots for curCorr (and, for RE_CHECK, parCorr) are
// already registered by Run BEFORE this call (ArtifactsAwaiterPort godoc —
// Register must precede the publish or the response races ahead of the slot)
// and their Cancel is on Run's defer chain. This function therefore only
// publishes and awaits. For RE_CHECK the parent request is fired back-to-back
// with the current one (distinct correlation suffixes — build-spec D7) but
// the parent is AWAITED separately at step 10 (sequential, not parallel:
// build-spec §5 step-7 justification — the parent branch is degradable and
// awaiting current first fails fast without burning the parent budget; no
// errgroup is available anyway).
func (o *Orchestrator) requestAndAwaitCurrent(
	ctx context.Context,
	span PipelineSpan,
	trigger port.VersionProcessingArtifactsReady,
	state *model.PipelineState,
	curCorr, parCorr string,
) (port.ArtifactsProvided, error) {
	reCheck := state.Mode == model.PipelineModeReCheck

	reqCtx, reqSpan := span.StartChild(ctx, "dm.request")
	if rErr := o.artReq.RequestArtifacts(
		reqCtx, curCorr, trigger.JobID, trigger.DocumentID, trigger.VersionID, trigger.OrganizationID,
		append([]model.ArtifactType(nil), requiredArtifacts...),
	); rErr != nil {
		reqSpan.Finish(rErr)
		return port.ArtifactsProvided{}, model.NewDomainError(model.ErrCodeInternal, model.StageRequestingArtifacts).WithCause(rErr)
	}
	if reCheck {
		// The parent request subject is the PARENT's version_id
		// (high-arch:1063, §8.3 step 5).
		if rErr := o.artReq.RequestArtifacts(
			reqCtx, parCorr, trigger.JobID, trigger.DocumentID, *state.ParentVersionID, trigger.OrganizationID,
			[]model.ArtifactType{model.ArtifactRiskAnalysis},
		); rErr != nil {
			reqSpan.Finish(rErr)
			return port.ArtifactsProvided{}, model.NewDomainError(model.ErrCodeInternal, model.StageRequestingArtifacts).WithCause(rErr)
		}
	}
	reqSpan.Finish(nil)

	awCtx, awSpan := span.StartChild(ctx, "dm.await")
	subCtx, subCancel := context.WithTimeout(awCtx, o.cfg.DMRequestTimeout)
	defer subCancel()
	prov, awErr := o.artAwait.Await(subCtx, curCorr)
	if awErr != nil {
		awSpan.Finish(awErr)
		if errors.Is(awErr, port.ErrAwaitTimeout) {
			return port.ArtifactsProvided{}, model.NewDomainError(model.ErrCodeDMArtifactsTimeout, model.StageRequestingArtifacts).WithCause(awErr)
		}
		// A ctx-cancelled await under the job deadline is reclassified to
		// timeout by classifyOutcome (D11); else internal.
		return port.ArtifactsProvided{}, model.NewDomainError(model.ErrCodeInternal, model.StageRequestingArtifacts).WithCause(awErr)
	}
	if prov.ErrorCode != "" || len(prov.Artifacts) == 0 || missingRequired(prov) {
		awSpan.Finish(nil)
		return port.ArtifactsProvided{}, model.NewDomainError(model.ErrCodeDMArtifactsMissing, model.StageRequestingArtifacts).
			WithDevMessage("DM ArtifactsProvided missing/empty required artifacts or carried an error_code")
	}
	awSpan.Finish(nil)
	state.CurrentStage = model.StageArtifactsReceived
	return prov, nil
}

// awaitParentAnalysis awaits the parent RISK_ANALYSIS for a RE_CHECK run and
// DEGRADES on any problem (build-spec D7 / high-arch §8.7 step 1-5): a
// non-nil error (incl. ErrAwaitTimeout), an ErrorCode, a missing/empty
// RISK_ANALYSIS, or an unmarshal failure all set parentMissing=true, leave
// state.ParentRiskAnalysis nil, WARN-log, and continue (NEVER fail the
// pipeline). On success it unmarshals into state.ParentRiskAnalysis.
// stages.RunStage6 self-gates on a nil ParentRiskAnalysis so a nil here
// correctly skips Agent 9; the RE_CHECK_PARENT_ANALYSIS_MISSING warning is
// rendered by the aggregator via Input.ParentAnalysisMissing. The parCorr
// slot was registered by Run and its Cancel is on Run's defer chain.
func (o *Orchestrator) awaitParentAnalysis(
	ctx context.Context,
	span PipelineSpan,
	trigger port.VersionProcessingArtifactsReady,
	state *model.PipelineState,
	parCorr string,
) bool {
	if state.Mode != model.PipelineModeReCheck {
		return false
	}

	awCtx, awSpan := span.StartChild(ctx, "dm.await.parent")
	subCtx, subCancel := context.WithTimeout(awCtx, o.cfg.DMRequestTimeout)
	defer subCancel()

	prov, awErr := o.artAwait.Await(subCtx, parCorr)
	degrade := func(cause any) bool {
		awSpan.Finish(nil) // a degraded parent is NOT a span error
		o.log.Warn(ctx, "RE_CHECK parent RISK_ANALYSIS unavailable; degrading",
			"version_id", trigger.VersionID, "cause", cause)
		state.ParentRiskAnalysis = nil
		return true
	}
	if awErr != nil {
		return degrade(awErr)
	}
	if prov.ErrorCode != "" {
		return degrade("ArtifactsProvided.error_code=" + prov.ErrorCode)
	}
	raw, ok := prov.Artifacts[model.ArtifactRiskAnalysis]
	if !ok || len(raw) == 0 {
		return degrade("parent RISK_ANALYSIS absent/empty")
	}
	var parentRA model.RiskAnalysis
	if jErr := json.Unmarshal(raw, &parentRA); jErr != nil {
		return degrade(jErr)
	}
	awSpan.Finish(nil)
	state.ParentRiskAnalysis = &parentRA
	return false
}

// missingRequired reports whether prov omits any of the four required
// current-version artifacts (either listed in MissingTypes or simply absent
// from the Artifacts map).
func missingRequired(prov port.ArtifactsProvided) bool {
	missing := make(map[model.ArtifactType]struct{}, len(prov.MissingTypes))
	for _, mt := range prov.MissingTypes {
		missing[mt] = struct{}{}
	}
	for _, want := range requiredArtifacts {
		if _, gone := missing[want]; gone {
			return true
		}
		if v, present := prov.Artifacts[want]; !present || len(v) == 0 {
			return true
		}
	}
	return false
}

// buildAggregatorInput projects the current PipelineState into an
// aggregator.Input (aggregator/CLAUDE.md FN-1). Used by BOTH the merge-early
// (Recommendations nil, RiskDelta nil) and finalize-late (both populated)
// calls — the shape is identical; only the state contents differ by step.
// RiskAnalysis is the RAW Agent-5 output (NOT MergedRiskAnalysis); RiskDelta
// is deliberately not consulted by the aggregator (D4) but passed for
// full-state fidelity; Truncation is nil (no Token Estimator in 036 v1 —
// forward note).
func (o *Orchestrator) buildAggregatorInput(state *model.PipelineState, parentMissing bool) aggregator.Input {
	return aggregator.Input{
		Mode:                  state.Mode,
		Classification:        state.Classification,
		KeyParameters:         state.KeyParameters,
		PartyConsistency:      state.PartyConsistency,
		MandatoryConditions:   state.MandatoryConditions,
		RiskAnalysis:          state.RiskAnalysis,
		Recommendations:       state.Recommendations,
		RiskDelta:             state.RiskDelta,
		Truncation:            nil,
		ParentAnalysisMissing: state.Mode == model.PipelineModeReCheck && parentMissing,
	}
}

// mergeEarly runs aggregator.Aggregate after Stage 3 and assigns the merged
// risk analysis / profile / score back onto state BEFORE Stage 4 (build-spec
// D3). The only Aggregate error is ErrNilRiskAnalysis (Agent-5 critical
// breach) → INTERNAL_ERROR stamped at the Risk-Detection stage.
func (o *Orchestrator) mergeEarly(ctx context.Context, span PipelineSpan, state *model.PipelineState, parentMissing bool) error {
	_, aggSpan := span.StartChild(ctx, "aggregate.merge")
	out, err := o.agg.Aggregate(o.buildAggregatorInput(state, parentMissing))
	if err != nil {
		aggSpan.Finish(err)
		return model.NewDomainError(model.ErrCodeInternal, model.StageAgentRiskDetection).
			WithCause(err).
			WithDevMessage("aggregator merge-early failed (Agent-5 critical/merge-base breach)")
	}
	aggSpan.Finish(nil)
	state.MergedRiskAnalysis = out.MergedRiskAnalysis
	state.RiskProfile = out.RiskProfile
	state.AggregateScore = out.AggregateScore
	state.CurrentStage = model.StageRiskProfileCalc
	return nil
}

// finalizeLate re-runs aggregator.Aggregate over the now-complete state after
// Stage 6 (build-spec D3). Aggregate is pure & idempotent over a fixed Input
// (aggregator D5) so the second call is safe; out2 carries the final Warnings
// (incl. the cross-agent checks needing the full Recommendations set).
func (o *Orchestrator) finalizeLate(ctx context.Context, span PipelineSpan, state *model.PipelineState, parentMissing bool) (aggregator.Output, error) {
	_, aggSpan := span.StartChild(ctx, "aggregate.finalize")
	out2, err := o.agg.Aggregate(o.buildAggregatorInput(state, parentMissing))
	if err != nil {
		aggSpan.Finish(err)
		return aggregator.Output{}, model.NewDomainError(model.ErrCodeInternal, model.StageAggregateScoreCalc).
			WithCause(err).
			WithDevMessage("aggregator finalize-late failed (Agent-5 critical/merge-base breach)")
	}
	aggSpan.Finish(nil)
	state.MergedRiskAnalysis = out2.MergedRiskAnalysis
	state.RiskProfile = out2.RiskProfile
	state.AggregateScore = out2.AggregateScore
	state.CurrentStage = model.StageAggregateScoreCalc
	return out2, nil
}

// buildPayload assembles the outbound LegalAnalysisArtifactsReady from the
// finalize-late Output + state, attaches the aggregator-owned Warnings onto
// the DetailedReport, and nulls outbound risk_delta when the RE_CHECK parent
// was missing (high-arch §8.7 step 4 — Orchestrator-owned, NOT aggregator).
func (o *Orchestrator) buildPayload(
	trigger port.VersionProcessingArtifactsReady,
	state *model.PipelineState,
	out2 aggregator.Output,
	parentMissing bool,
) port.LegalAnalysisArtifactsReady {
	if state.DetailedReport != nil {
		state.DetailedReport.Warnings = out2.Warnings
	}
	payload := port.LegalAnalysisArtifactsReady{
		CorrelationID:        trigger.CorrelationID,
		Timestamp:            o.clock.Now().Format(time.RFC3339),
		JobID:                trigger.JobID,
		DocumentID:           trigger.DocumentID,
		VersionID:            trigger.VersionID,
		OrganizationID:       trigger.OrganizationID,
		ClassificationResult: state.Classification,
		KeyParameters:        out2.StrippedKeyParameters,
		RiskAnalysis:         out2.MergedRiskAnalysis,
		RiskProfile:          out2.RiskProfile,
		Recommendations:      state.Recommendations,
		Summary:              state.Summary,
		DetailedReport:       state.DetailedReport,
		AggregateScore:       out2.AggregateScore,
		RiskDelta:            state.RiskDelta,
	}
	if state.Mode == model.PipelineModeReCheck && (parentMissing || state.ParentRiskAnalysis == nil) {
		payload.RiskDelta = nil
	}
	return payload
}

// awaitPersist awaits the DM persist confirmation for the job and maps the
// envelope to the §6 table (timeout / malformed / DM-failure / success).
func (o *Orchestrator) awaitPersist(ctx context.Context, span PipelineSpan, trigger port.VersionProcessingArtifactsReady) error {
	awCtx, awSpan := span.StartChild(ctx, "persist.await")
	subCtx, subCancel := context.WithTimeout(awCtx, o.cfg.DMPersistConfirmTimeout)
	defer subCancel()

	conf, err := o.persistAwait.Await(subCtx, trigger.JobID)
	if err != nil {
		awSpan.Finish(err)
		if errors.Is(err, port.ErrAwaitTimeout) {
			return model.NewDomainError(model.ErrCodeDMPersistTimeout, model.StageAwaitingDMConfirmation).WithCause(err)
		}
		return model.NewDomainError(model.ErrCodeInternal, model.StageAwaitingDMConfirmation).WithCause(err)
	}
	if !conf.IsValid() {
		awSpan.Finish(nil)
		return model.NewDomainError(model.ErrCodeInternal, model.StageAwaitingDMConfirmation).
			WithDevMessage("malformed PersistConfirmation envelope (LIC-TASK-040 routing defect)")
	}
	if conf.IsFailure() {
		awSpan.Finish(nil)
		de := model.NewDomainError(model.ErrCodeDMPersistFailed, model.StageAwaitingDMConfirmation).
			WithRetryable(conf.Failure.IsRetryable)
		if conf.Failure.ErrorMessage != "" {
			de = de.WithDevMessage(conf.Failure.ErrorMessage)
		}
		return de
	}
	awSpan.Finish(nil)
	return nil // conf.IsSuccess()
}

// classifyOutcome is the SINGLE outcome decision (build-spec §5 / D11). The
// timeout discriminator is binding: if the job context deadline fired, the
// outcome is timeout and the error is a fresh retryable ANALYSIS_TIMEOUT
// EVEN IF the body returned a different *model.DomainError (a stage's
// ctx-cancelled error is a symptom; the deadline is the root cause —
// high-arch §8.10). Otherwise a propagated *model.DomainError is used
// verbatim; a non-DomainError is wrapped into INTERNAL_ERROR (the base MF-2
// "never swallow" discipline). nil ⇒ success.
func (o *Orchestrator) classifyOutcome(rootCtx context.Context, state *model.PipelineState, bodyErr error) (string, *model.DomainError) {
	if rootCtx.Err() == context.DeadlineExceeded {
		stage := state.CurrentStage
		if stage == "" {
			stage = model.StageReceived
		}
		de := model.NewDomainError(model.ErrCodeAnalysisTimeout, stage).WithRetryable(true)
		if bodyErr != nil {
			de = de.WithCause(bodyErr)
		}
		return outcomeTimeout, de
	}
	// LIC-TASK-037 paused outcome (build-spec D3). Placed AFTER the timeout
	// discriminator (a deadline that fired while pausing is still a timeout)
	// and BEFORE the success/failure split: a completed pause is neither.
	// de is nil — it is a control-flow outcome, not a failure.
	if errors.Is(bodyErr, ErrPipelinePaused) {
		return outcomePaused, nil
	}
	if bodyErr == nil {
		return outcomeSuccess, nil
	}
	if de, ok := model.AsDomainError(bodyErr); ok {
		return outcomeFailed, de
	}
	stage := state.CurrentStage
	if stage == "" {
		stage = model.StageReceived
	}
	return outcomeFailed, model.NewDomainError(model.ErrCodeInternal, stage).WithCause(bodyErr)
}

// codeLabelFor returns the error_code metric label: empty for success and
// timeout (the pipeline.go convention — timeout's code is implicit in the
// outcome), the model.ErrorCode string for failed.
func codeLabelFor(outcome string, de *model.DomainError) string {
	if outcome != outcomeFailed || de == nil {
		return ""
	}
	return de.Code.String()
}

// statusEvent builds an LICStatusChangedEvent with the verbatim correlation
// fields and a clock-sourced timestamp. de is non-nil only for FAILED; its
// Code/UserMessage/Retryable populate the conditional fields.
func (o *Orchestrator) statusEvent(
	trigger port.VersionProcessingArtifactsReady,
	status model.ExternalStatus,
	stage model.Stage,
	de *model.DomainError,
) port.LICStatusChangedEvent {
	evt := port.LICStatusChangedEvent{
		CorrelationID:  trigger.CorrelationID,
		Timestamp:      o.clock.Now().Format(time.RFC3339),
		JobID:          trigger.JobID,
		DocumentID:     trigger.DocumentID,
		VersionID:      trigger.VersionID,
		OrganizationID: trigger.OrganizationID,
		Status:         status,
	}
	// Stage is omitempty on the wire; COMPLETED leaves it zero (the
	// events.go:206-208 omit-on-zero policy) so we only set it for
	// IN_PROGRESS / FAILED.
	if status != model.StatusCompleted {
		evt.Stage = stage
	}
	if status == model.StatusFailed && de != nil {
		evt.Stage = de.Stage
		evt.ErrorCode = de.Code
		evt.ErrorMessage = de.UserMessage
		retry := de.Retryable
		evt.IsRetryable = &retry
	}
	return evt
}

// publishFailed is the SINGLE FAILED-status publish site (build-spec §5).
// Gate: a non-publishable code (empty catalog UserMessage —
// IsPublishableToOrchestrator false) must NOT be published with an empty
// error_message; instead it is DLQ-logged (a real DLQ publish is
// LIC-TASK-046/040's job — 036 has no DLQPublisherPort by design). A
// PublishStatus error is logged but does NOT mask the analysis failure
// (LIC-TASK-040 will NACK on the returned de).
func (o *Orchestrator) publishFailed(ctx context.Context, state *model.PipelineState, de *model.DomainError) {
	if de == nil {
		return
	}
	if !de.Code.IsPublishableToOrchestrator() {
		o.log.Error(ctx, "non-publishable terminal code, routing to DLQ-log (LIC-TASK-046/040 owns the real DLQ publish)",
			"error_code", de.Code.String(), "stage", de.Stage.String(), "job_id", state.JobID, "version_id", state.VersionID)
		return
	}
	trigger := port.VersionProcessingArtifactsReady{
		CorrelationID:  state.CorrelationID,
		JobID:          state.JobID,
		DocumentID:     state.DocumentID,
		VersionID:      state.VersionID,
		OrganizationID: state.OrganizationID,
	}
	if pErr := o.statusPub.PublishStatus(ctx, o.statusEvent(trigger, model.StatusFailed, de.Stage, de)); pErr != nil {
		o.log.Error(ctx, "FAILED status publish errored; analysis failure stands (LIC-TASK-040 will NACK)",
			"publish_error", pErr, "error_code", de.Code.String(), "job_id", state.JobID)
	}
}

// finalizePrePipeline records the exit metrics and publishes the terminal
// FAILED status for a failure that occurred BEFORE the main finalizer was
// armed (Acquire failure at step 1). It mirrors the finalizer's metric +
// publish behaviour so acceptance "на любой ошибке" holds pre-pipeline.
func (o *Orchestrator) finalizePrePipeline(ctx context.Context, state *model.PipelineState, de *model.DomainError, modeLabel, outcome string, start time.Time) {
	o.metrics.PipelineFinished(modeLabel, outcome, o.clock.Since(start).Seconds())
	o.metrics.PipelineOutcome(modeLabel, outcome, codeLabelFor(outcome, de))
	o.publishFailed(ctx, state, de)
}
