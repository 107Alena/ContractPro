// Package pendingconfirmation implements the LIC Pending Type Confirmation
// Manager (LIC-TASK-037, high-architecture.md §6.5 / §6.10, security.md
// §11.2, error-handling.md §3.6). It is the two-halves-of-one-state-machine
// body of the low-confidence pause/resume cycle around one Redis namespace
// (lic-pending-state / lic-trigger / lic-user-confirmed) and one metric
// family:
//
//   - Pause(ctx, *PipelineState)            — high-arch §6.5:611-617 strict
//     order: SET lic-pending-state → publish classification-uncertain
//     (broker-confirmed) → publish IN_PROGRESS{STAGE_AWAITING_USER_
//     CONFIRMATION} (broker-confirmed) → SET lic-trigger=PAUSED. On full
//     success it returns the injected paused sentinel
//     (pipeline.ErrPipelinePaused, via Config.PausedSentinel) so the
//     orchestrator's classifyOutcome routes it to outcomePaused and
//     LIC-TASK-040 ACKs the source without COMPLETED/FAILED. Structurally
//     satisfies pipeline.PauseController.
//   - HandleUserConfirmedType(ctx, cmd)     — high-arch §6.10 Resume: SETNX
//     lic-user-confirmed → validate contract_type (security.md §11.2) →
//     Load+restore pending-state → tenant check → override classification →
//     restore trace → drive ResumeAfterConfirmation → §6.10-step-9 cleanup.
//     Structurally satisfies port.UserConfirmedTypeHandler.
//   - RepublishPauseEvents(ctx, *PendingTypeConfirmation) — §6.5:631 /
//     §6.10 Resume safety-net re-publication body, called by LIC-TASK-040.
//
// Hermetic: stdlib + internal/domain/{model,port} only (build-spec D17,
// enforced by internal_test.go). It does NOT import internal/application/
// pipeline: the resumer is the local PipelineResumer seam and the paused
// sentinel flows as Config.PausedSentinel error (build-spec D5/D11). It does
// NOT own the broker delivery / ACK (LIC-TASK-040 — DEFECT-4) or the
// lic-trigger ingress idempotency guard (LIC-TASK-040 — forward-note #2).
//
// Design adjudicated by subagent code-architect (build-spec — decisions
// D1..D20, reconciliations R1..R7); implemented by subagent golang-pro. The
// authoritative reconciliations are recorded in this package's CLAUDE.md.
package pendingconfirmation

import (
	"context"
	"errors"
	"time"

	"contractpro/legal-intelligence-core/internal/domain/model"
	"contractpro/legal-intelligence-core/internal/domain/port"
)

// Redis key prefixes (high-arch §6.3 / §6.10). The ports operate on opaque
// key strings; the Manager owns the lic-trigger PAUSED→COMPLETED transition
// writes and the lic-user-confirmed key (LIC-TASK-040 owns the lic-trigger
// ingress SETNX guard — build-spec D4/D7 forward-note #2).
const (
	keyPrefixTrigger       = "lic-trigger:"
	keyPrefixUserConfirmed = "lic-user-confirmed:"
)

// sourceTopicUserConfirmedType is the original topic recorded in DLQ
// envelopes for a rejected orch.commands.user-confirmed-type message.
const sourceTopicUserConfirmedType = "orch.commands.user-confirmed-type"

// audit validation_outcome values (security.md §11.2 step 4 / build-spec
// D20). EXACT set: every HandleUserConfirmedType decision emits exactly one
// audit line carrying one of these.
const (
	auditAccepted          = "accepted"
	auditRejectedFormat    = "rejected_format"
	auditRejectedWhitelist = "rejected_whitelist"
	auditRejectedTenant    = "rejected_tenant_mismatch"
)

// Config carries the Manager's TTLs + threshold + the injected paused
// sentinel. Local struct, NO internal/config import (build-spec D12/D17 — the
// pipeline.Config precedent; LIC-TASK-047 maps config.* → this).
type Config struct {
	// PendingStateTTL is the lic-pending-state + lic-trigger=PAUSED TTL
	// (25h; from config.PipelineConfig.PendingConfirmationTTL — §6.10:755/758).
	PendingStateTTL time.Duration
	// UserConfirmedProcessingTTL is the lic-user-confirmed SETNX PROCESSING
	// TTL (90s; from config.IdempotencyConfig.UserConfirmedProcessingTTL,
	// LIC_USER_CONFIRMED_PROCESSING_TTL — build-spec R4/D12).
	UserConfirmedProcessingTTL time.Duration
	// CompletedTTL is the lic-trigger=COMPLETED + lic-user-confirmed=COMPLETED
	// TTL (24h; from config.IdempotencyConfig.TTL — §6.10:782 "EX 24h").
	CompletedTTL time.Duration
	// ConfidenceThreshold populates ClassificationUncertain.Threshold; it
	// MUST equal pipeline.Config.ConfidenceThreshold (same LIC_CONFIDENCE_
	// THRESHOLD — a 047 wiring invariant; build-spec D6). [0,1].
	ConfidenceThreshold float64
	// PausedSentinel is pipeline.ErrPipelinePaused, injected by LIC-TASK-047
	// (build-spec D5). Pause returns this on full success; errors.Is in the
	// orchestrator's classifyOutcome works because it is the same error
	// value. Required (non-nil) — a Manager that cannot signal "paused" is
	// misconfigured.
	PausedSentinel error
}

// validate fails fast on misconfiguration (errors.Join surfaces ALL at once —
// feedback_constructors.md; the pipeline.Config.validate precedent).
func (c Config) validate() error {
	var errs []error
	if c.PendingStateTTL <= 0 {
		errs = append(errs, errors.New("pendingconfirmation: Config.PendingStateTTL must be > 0"))
	}
	if c.UserConfirmedProcessingTTL <= 0 {
		errs = append(errs, errors.New("pendingconfirmation: Config.UserConfirmedProcessingTTL must be > 0"))
	}
	if c.CompletedTTL <= 0 {
		errs = append(errs, errors.New("pendingconfirmation: Config.CompletedTTL must be > 0"))
	}
	if c.ConfidenceThreshold < 0 || c.ConfidenceThreshold > 1 {
		errs = append(errs, errors.New("pendingconfirmation: Config.ConfidenceThreshold must be in [0,1]"))
	}
	if c.PausedSentinel == nil {
		errs = append(errs, errors.New("pendingconfirmation: Config.PausedSentinel must not be nil (LIC-TASK-047 injects pipeline.ErrPipelinePaused)"))
	}
	if c.UserConfirmedProcessingTTL > 0 && c.PendingStateTTL > 0 && c.UserConfirmedProcessingTTL >= c.PendingStateTTL {
		errs = append(errs, errors.New("pendingconfirmation: Config.UserConfirmedProcessingTTL must be < PendingStateTTL"))
	}
	return errors.Join(errs...)
}

// Manager is the single type satisfying both the PauseController (Pause) and
// the UserConfirmedTypeHandler (HandleUserConfirmedType) roles (build-spec
// D1). Immutable after NewManager; Pause / HandleUserConfirmedType /
// RepublishPauseEvents are goroutine-safe for distinct version_ids (no
// per-instance mutable state; the ports/seams are stateless/immutable; each
// call builds its own *model.PipelineState / *PendingTypeConfirmation).
type Manager struct {
	cfg     Config
	pending port.PendingStatePort
	idem    port.IdempotencyStorePort
	uncert  port.UncertaintyPublisherPort
	status  port.StatusPublisherPort
	dlq     port.DLQPublisherPort
	resumer PipelineResumer

	metrics Metrics
	clock   Clock
	log     Logger
	trace   TraceRestorer
}

// NewManager validates the wiring and assembles the Manager. It fails fast
// (NewTypeName per feedback_constructors.md; the pipeline.NewOrchestrator
// precedent): an invalid Config or any nil required collaborator is a
// LIC-TASK-047 wiring defect and must be a startup error, not a first-call
// nil-deref. errors.Join surfaces ALL defects at once. Optional seams never
// cause an error; Deps.withDefaults substitutes a noop for each nil one.
func NewManager(
	cfg Config,
	pending port.PendingStatePort,
	idem port.IdempotencyStorePort,
	uncert port.UncertaintyPublisherPort,
	status port.StatusPublisherPort,
	dlq port.DLQPublisherPort,
	resumer PipelineResumer,
	deps Deps,
) (*Manager, error) {
	var errs []error
	if err := cfg.validate(); err != nil {
		errs = append(errs, err)
	}
	if pending == nil {
		errs = append(errs, errors.New("pendingconfirmation: pending (port.PendingStatePort) must not be nil"))
	}
	if idem == nil {
		errs = append(errs, errors.New("pendingconfirmation: idem (port.IdempotencyStorePort) must not be nil"))
	}
	if uncert == nil {
		errs = append(errs, errors.New("pendingconfirmation: uncert (port.UncertaintyPublisherPort) must not be nil"))
	}
	if status == nil {
		errs = append(errs, errors.New("pendingconfirmation: status (port.StatusPublisherPort) must not be nil"))
	}
	if dlq == nil {
		errs = append(errs, errors.New("pendingconfirmation: dlq (port.DLQPublisherPort) must not be nil"))
	}
	if resumer == nil {
		errs = append(errs, errors.New("pendingconfirmation: resumer (PipelineResumer) must not be nil"))
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}

	d := deps.withDefaults()
	return &Manager{
		cfg:     cfg,
		pending: pending,
		idem:    idem,
		uncert:  uncert,
		status:  status,
		dlq:     dlq,
		resumer: resumer,
		metrics: d.Metrics,
		clock:   d.Clock,
		log:     d.Logger,
		trace:   d.TraceRestorer,
	}, nil
}

// ---------------------------------------------------------------------------
// Pause — high-arch §6.5:611-617 strict order (build-spec D4).
// ---------------------------------------------------------------------------

// Pause executes high-arch §6.10 Pause steps 2-5 in the binding strict order
// (step 1 "form state object" is the orchestrator already owning st). It
// returns the injected paused sentinel ONLY on full success; any step failure
// returns a *model.DomainError (which the orchestrator's classifyOutcome
// routes to outcomeFailed, NOT outcomePaused). Structurally satisfies
// pipeline.PauseController (the var _ assertion lives in LIC-TASK-047 — D18).
func (m *Manager) Pause(ctx context.Context, st *model.PipelineState) error {
	// Build the pending blob from st (build-spec D4 step 1). st.TraceContext
	// MAY be zero (036 zeroes it; LIC-TASK-040 owns populating it —
	// RECONCILIATION R3); persisted as-is, the resume TraceRestorer treats a
	// zero context as "no linkage".
	ptc := m.buildPending(st)

	// Step 1 (§6.5:613) — SET pending state. BEFORE the first stable point,
	// so a failure is non-retryable (re-running Stage 1 on redelivery is
	// wasteful; a non-retryable FAILED is the honest terminal state).
	if err := m.pending.Save(ctx, st.VersionID, ptc, m.cfg.PendingStateTTL); err != nil {
		return model.NewDomainError(model.ErrCodeInternal, model.StageAwaitingUserConfirmation).
			WithRetryable(false).
			WithCause(err).
			WithDevMessage("pause: pending-state Save failed before any publish")
	}
	// The gauge mirrors the Redis namespace, not the success of the whole
	// sequence: Inc immediately after a successful Save (build-spec D4.7).
	m.metrics.PendingStateInc()

	// Step 2 (§6.5:614) — publish classification-uncertain (broker-confirmed:
	// a nil return means broker-confirmed, LIC-TASK-045's concern). Retryable
	// from here on: the pending-state IS saved (recoverable on redelivery).
	if err := m.publishPauseUncertain(ctx, st); err != nil {
		return model.NewDomainError(model.ErrCodeInternal, model.StageAwaitingUserConfirmation).
			WithRetryable(true).
			WithCause(err).
			WithDevMessage("pause: classification-uncertain publish failed after pending-state saved")
	}

	// Step 3 (§6.5:615) — publish IN_PROGRESS{STAGE_AWAITING_USER_CONFIRMATION}
	// (broker-confirmed). Recoverable: restart re-publishes, Orchestrator
	// dedups by lic-status:{job_id}:{status}.
	if err := m.publishPauseInProgress(ctx, st); err != nil {
		return model.NewDomainError(model.ErrCodeInternal, model.StageAwaitingUserConfirmation).
			WithRetryable(true).
			WithCause(err).
			WithDevMessage("pause: IN_PROGRESS status publish failed")
	}

	// Step 4 (§6.5:616) — SET lic-trigger:{version_id}=PAUSED (25h TTL).
	// Recoverable: §6.5 restart-semantics safety-net restores the invariant.
	if err := m.idem.SetPaused(ctx, keyPrefixTrigger+st.VersionID, m.cfg.PendingStateTTL); err != nil {
		return model.NewDomainError(model.ErrCodeInternal, model.StageAwaitingUserConfirmation).
			WithRetryable(true).
			WithCause(err).
			WithDevMessage("pause: SetPaused(lic-trigger) failed; events already published")
	}

	// Step 5 (§6.5:617) — ACK the source: NOT performed here (no broker
	// handle — DEFECT-4 / RECONCILIATION R1). Return the sentinel; the
	// orchestrator's classifyOutcome→outcomePaused→finalizer returns it and
	// LIC-TASK-040 ACKs on pipeline.IsPaused(err)==true.
	return m.cfg.PausedSentinel
}

// RepublishPauseEvents re-publishes the two pause events for an
// already-paused version (high-arch §6.5:631 / §6.10 Resume safety-net).
// LIC-TASK-040 calls this when a redelivered dm.events.version-artifacts-
// ready hits lic-trigger=PAUSED and lic-pending-state is present: 040 has
// already done the lic-trigger guard + the pending-state Load; it passes the
// decoded state in. Re-runs Pause steps 3+4 (the two broker-confirmed
// publishes) ONLY — NOT Save (state present), NOT SetPaused (lic-trigger
// already PAUSED), NOT the sentinel (this is a restart re-publish, not a
// fresh pause). The Orchestrator dedups status by lic-status:{job_id}:
// {status} and uncertain by lic-uncertain:{version_id}, so re-publication is
// idempotent. Returns nil ⇒ 040 ACKs the source (Stage 1 NOT restarted,
// §6.5:631); a retryable *model.DomainError ⇒ 040 NACK→retry-DLX.
func (m *Manager) RepublishPauseEvents(ctx context.Context, st *model.PendingTypeConfirmation) error {
	if st == nil {
		return model.NewDomainError(model.ErrCodeInternal, model.StageAwaitingUserConfirmation).
			WithRetryable(false).
			WithDevMessage("RepublishPauseEvents: nil PendingTypeConfirmation")
	}
	if err := m.publishPauseUncertainFromPending(ctx, st); err != nil {
		return model.NewDomainError(model.ErrCodeInternal, model.StageAwaitingUserConfirmation).
			WithRetryable(true).
			WithCause(err).
			WithDevMessage("republish: classification-uncertain publish failed")
	}
	if err := m.publishPauseInProgressFromPending(ctx, st); err != nil {
		return model.NewDomainError(model.ErrCodeInternal, model.StageAwaitingUserConfirmation).
			WithRetryable(true).
			WithCause(err).
			WithDevMessage("republish: IN_PROGRESS status publish failed")
	}
	return nil
}

// ---------------------------------------------------------------------------
// HandleUserConfirmedType — high-arch §6.10 Resume (build-spec D7 + D10).
// ---------------------------------------------------------------------------

// HandleUserConfirmedType resumes a paused pipeline after a low-confidence
// classification (high-arch §6.10, security.md §11.2). Return contract
// (mapped by LIC-TASK-040): nil ⇒ ACK; non-nil *model.DomainError with
// Retryable=true ⇒ NACK→retry-DLX; Retryable=false ⇒ DLQ. Structurally
// satisfies port.UserConfirmedTypeHandler.
func (m *Manager) HandleUserConfirmedType(ctx context.Context, cmd port.UserConfirmedType) error {
	// Step 1 — contract_type validation FIRST (security.md §11.2 step 3 —
	// before any state read for the format/whitelist check). Order: regex
	// then 12-whitelist (model.IsValidContractType).
	if !model.IsValidContractType(cmd.ContractType) {
		outcome := auditRejectedWhitelist
		if !model.ValidateContractTypeFormat(cmd.ContractType) {
			outcome = auditRejectedFormat
		}
		m.auditLog(ctx, cmd, outcome)
		m.publishInvalidDLQ(ctx, cmd, model.ErrCodeInvalidContractType)
		m.publishFailed(ctx, cmd, model.ErrCodeInvalidContractType)
		m.setUserConfirmedCompleted(ctx, cmd.VersionID)
		m.metrics.UserConfirmation("invalid")
		return nil // §6.10:776 — ACK
	}

	// Step 2 (§6.10 Resume step 2-3) — SETNX lic-user-confirmed.
	status, err := m.idem.SetNX(ctx, keyPrefixUserConfirmed+cmd.VersionID, m.cfg.UserConfirmedProcessingTTL)
	if err != nil {
		if errors.Is(err, port.ErrIdempotencyKeyExists) {
			switch status {
			case port.IdempotencyCompleted:
				// Resume already done — §6.10:786 "ACK для COMPLETED". No
				// UserConfirmation increment (counted on the original).
				return nil
			case port.IdempotencyProcessing, port.IdempotencyPaused:
				// Concurrent in-flight resume (PAUSED is defensive: it
				// should never hold for lic-user-confirmed — treat as
				// PROCESSING). NACK→retry-DLX (§6.10:786). No increment
				// (duplicate, not a received-and-decided confirmation).
				return model.NewDomainError(model.ErrCodeInternal, model.StageAwaitingUserConfirmation).
					WithRetryable(true).
					WithDevMessage("user-confirmed: concurrent in-flight resume (lic-user-confirmed=PROCESSING)")
			}
		}
		// Redis down — NACK, not published to Orch (IDEMPOTENCY_STORE_-
		// UNAVAILABLE is non-publishable; the Manager mirrors the 036
		// IsPublishableToOrchestrator gate by not publishing it).
		return model.NewDomainError(model.ErrCodeIdempotencyStoreUnavail, model.StageReceived).
			WithRetryable(true).
			WithCause(err).
			WithDevMessage("user-confirmed: SETNX lic-user-confirmed failed (Redis unavailable)")
	}

	// Step 3 (§6.10 Resume step 4) — GET lic-pending-state.
	ptc, err := m.pending.Load(ctx, cmd.VersionID)
	if err != nil {
		if errors.Is(err, port.ErrPendingStateNotFound) {
			// The pause state is gone — USER_CONFIRMATION_EXPIRED (RU from
			// the frozen catalog; PENDING_STATE_LOST is not a catalog code —
			// RECONCILIATION R2). §6.10:777: FAILED + lic-user-confirmed=
			// COMPLETED + ACK. NO DLQ (an expired pause is not poison).
			m.auditLog(ctx, cmd, auditAccepted)
			m.publishFailed(ctx, cmd, model.ErrCodeUserConfirmationExpired)
			m.setUserConfirmedCompleted(ctx, cmd.VersionID)
			m.metrics.UserConfirmation("expired")
			return nil // §6.10:777 — ACK
		}
		// Redis transient — NACK→retry-DLX; do NOT publish FAILED
		// (IDEMPOTENCY_STORE_UNAVAILABLE non-publishable).
		return model.NewDomainError(model.ErrCodeIdempotencyStoreUnavail, model.StageReceived).
			WithRetryable(true).
			WithCause(err).
			WithDevMessage("user-confirmed: lic-pending-state Load failed (Redis transient)")
	}

	// Step 4 (security.md §11.2 step 2 — MANDATORY, RECONCILIATION R5) —
	// tenant check. A forged cross-tenant UserConfirmedType: DLQ + alert,
	// pending-state NOT consumed (§11.2:494), NO FAILED (INVALID_ORG_ID_-
	// MISMATCH non-publishable), ACK so the poison message does not loop.
	if cmd.OrganizationID != ptc.OrganizationID {
		m.auditLog(ctx, cmd, auditRejectedTenant)
		m.log.Error(ctx, "LICTenantMismatch: forged UserConfirmedType org_id mismatch",
			"version_id", cmd.VersionID,
			"cmd_organization_id", cmd.OrganizationID,
			"pending_organization_id", ptc.OrganizationID)
		m.publishInvalidDLQ(ctx, cmd, model.ErrCodeInvalidOrgIDMismatch)
		m.metrics.UserConfirmation("invalid")
		return nil // §11.2 — ACK (poison message)
	}

	// Step 5 — decompress+restore (build-spec D10 step 4). ptc is already
	// decoded by PendingStatePort.Load (the port returns the typed struct).
	st := m.restoreState(ptc)

	// Step 6 (§6.10 Resume step 5 / §11.2) — override classification. A
	// corrupt blob with a nil ClassificationResult is unrecoverable,
	// non-retryable → 040 DLQ (defensive; the gate requires a non-nil
	// Classification at pause).
	if st.Classification == nil {
		m.auditLog(ctx, cmd, auditAccepted)
		return model.NewDomainError(model.ErrCodeInternal, model.StageAwaitingUserConfirmation).
			WithRetryable(false).
			WithDevMessage("resume: pending-state has nil ClassificationResult")
	}
	st.Classification.ContractType = model.ContractType(cmd.ContractType)
	st.Classification.Confidence = 1.0

	// Step 7 (§6.10 Resume step 5) — restore the saved W3C trace context so
	// the orchestrator's root span (opened inside ResumeAfterConfirmation)
	// links to the original trace as a child (build-spec D13).
	ctx = m.trace.Restore(ctx, st.TraceContext)

	// The receipt reached a decision (resume); audit it once.
	m.auditLog(ctx, cmd, auditAccepted)

	// Step 8 — drive the pipeline via the PipelineResumer seam (build-spec
	// D11). ResumeAfterConfirmation re-establishes the Run wrapper +
	// single-publish finalizer and publishes its own terminal status.
	runErr := m.resumer.ResumeAfterConfirmation(ctx, st)
	if runErr != nil {
		// ResumeAfterConfirmation ALREADY published the terminal FAILED
		// (build-spec D2 single-publish). Do NOT re-publish, do NOT Delete
		// pending-state (a retryable failure may resume again on the same
		// pending-state — §6.10:790-791), do NOT SetCompleted lic-trigger,
		// do NOT increment UserConfirmation (no enum value for a pipeline
		// failure; the pipeline metrics carry that signal — build-spec D10).
		// Return verbatim; 040 maps via model.IsRetryable.
		return runErr
	}

	// Step 9 (§6.10 Resume step 9) — COMPLETED cleanup. A Delete/SetCompleted
	// failure AFTER COMPLETED is logged but the method still returns nil
	// (the analysis is persisted and COMPLETED published; failing here would
	// re-run a completed pipeline on retry — strictly worse; the 25h/24h
	// TTLs reconcile the orphaned keys — build-spec D10).
	if dErr := m.pending.Delete(ctx, st.VersionID); dErr != nil {
		m.log.Error(ctx, "resume cleanup: lic-pending-state Delete failed after COMPLETED",
			"version_id", st.VersionID, "cause", dErr)
	}
	if cErr := m.idem.SetCompleted(ctx, keyPrefixTrigger+st.VersionID, m.cfg.CompletedTTL); cErr != nil {
		m.log.Error(ctx, "resume cleanup: SetCompleted(lic-trigger) failed after COMPLETED",
			"version_id", st.VersionID, "cause", cErr)
	}
	if cErr := m.idem.SetCompleted(ctx, keyPrefixUserConfirmed+st.VersionID, m.cfg.CompletedTTL); cErr != nil {
		m.log.Error(ctx, "resume cleanup: SetCompleted(lic-user-confirmed) failed after COMPLETED",
			"version_id", st.VersionID, "cause", cErr)
	}
	m.metrics.PendingStateDec()
	m.metrics.UserConfirmation("resumed")
	return nil // §6.10:782 — ACK
}

// ---------------------------------------------------------------------------
// Private helpers.
// ---------------------------------------------------------------------------

// buildPending maps every field of st into the pending blob (build-spec D4
// step 1). st.TraceContext is copied verbatim (RECONCILIATION R3).
func (m *Manager) buildPending(st *model.PipelineState) *model.PendingTypeConfirmation {
	return &model.PendingTypeConfirmation{
		JobID:                st.JobID,
		DocumentID:           st.DocumentID,
		VersionID:            st.VersionID,
		OrganizationID:       st.OrganizationID,
		CreatedByUserID:      st.CreatedByUserID,
		CorrelationID:        st.CorrelationID,
		TraceContext:         st.TraceContext,
		ClassificationResult: st.Classification,
		KeyParameters:        st.KeyParameters,
		InputArtifacts:       st.InputArtifacts,
		OriginType:           st.OriginType,
		ParentVersionID:      st.ParentVersionID,
	}
}

// restoreState rebuilds *model.PipelineState from the decoded pending blob
// (build-spec D10 step 4). Mode is set to RE_CHECK iff ParentVersionID != nil
// (mirrors the orchestrator's resolveParentAndMode).
func (m *Manager) restoreState(ptc *model.PendingTypeConfirmation) *model.PipelineState {
	st := model.NewPipelineState(ptc.CorrelationID, ptc.JobID, ptc.DocumentID, ptc.VersionID, ptc.OrganizationID)
	st.CreatedByUserID = ptc.CreatedByUserID
	st.OriginType = ptc.OriginType
	st.ParentVersionID = ptc.ParentVersionID
	if ptc.ParentVersionID != nil {
		st.Mode = model.PipelineModeReCheck
	}
	st.TraceContext = ptc.TraceContext
	st.InputArtifacts = ptc.InputArtifacts
	st.Classification = ptc.ClassificationResult
	st.KeyParameters = ptc.KeyParameters
	st.StartedAt = m.clock.Now()
	return st
}

// publishPauseUncertain builds + publishes classification-uncertain from a
// live PipelineState (Pause step 2). SuggestedType/Confidence/Alternatives
// from st.Classification; Threshold from cfg (build-spec D4 step 3 / D6).
func (m *Manager) publishPauseUncertain(ctx context.Context, st *model.PipelineState) error {
	return m.uncert.PublishClassificationUncertain(ctx, port.ClassificationUncertain{
		CorrelationID:  st.CorrelationID,
		Timestamp:      m.clock.Now().Format(time.RFC3339),
		JobID:          st.JobID,
		DocumentID:     st.DocumentID,
		VersionID:      st.VersionID,
		OrganizationID: st.OrganizationID,
		SuggestedType:  st.Classification.ContractType,
		Confidence:     st.Classification.Confidence,
		Threshold:      m.cfg.ConfidenceThreshold,
		Alternatives:   st.Classification.Alternatives,
	})
}

// publishPauseUncertainFromPending is the RepublishPauseEvents variant
// sourcing the event from the decoded pending blob (same wire shape).
func (m *Manager) publishPauseUncertainFromPending(ctx context.Context, st *model.PendingTypeConfirmation) error {
	evt := port.ClassificationUncertain{
		CorrelationID:  st.CorrelationID,
		Timestamp:      m.clock.Now().Format(time.RFC3339),
		JobID:          st.JobID,
		DocumentID:     st.DocumentID,
		VersionID:      st.VersionID,
		OrganizationID: st.OrganizationID,
		Threshold:      m.cfg.ConfidenceThreshold,
	}
	if st.ClassificationResult != nil {
		evt.SuggestedType = st.ClassificationResult.ContractType
		evt.Confidence = st.ClassificationResult.Confidence
		evt.Alternatives = st.ClassificationResult.Alternatives
	}
	return m.uncert.PublishClassificationUncertain(ctx, evt)
}

// publishPauseInProgress publishes IN_PROGRESS{STAGE_AWAITING_USER_-
// CONFIRMATION} from a live PipelineState (Pause step 3). The Manager builds
// the LICStatusChangedEvent directly — statusEvent is an unexported
// orchestrator method (build-spec D4 step 4).
func (m *Manager) publishPauseInProgress(ctx context.Context, st *model.PipelineState) error {
	return m.status.PublishStatus(ctx, port.LICStatusChangedEvent{
		CorrelationID:  st.CorrelationID,
		Timestamp:      m.clock.Now().Format(time.RFC3339),
		JobID:          st.JobID,
		DocumentID:     st.DocumentID,
		VersionID:      st.VersionID,
		OrganizationID: st.OrganizationID,
		Status:         model.StatusInProgress,
		Stage:          model.StageAwaitingUserConfirmation,
	})
}

// publishPauseInProgressFromPending is the RepublishPauseEvents variant.
func (m *Manager) publishPauseInProgressFromPending(ctx context.Context, st *model.PendingTypeConfirmation) error {
	return m.status.PublishStatus(ctx, port.LICStatusChangedEvent{
		CorrelationID:  st.CorrelationID,
		Timestamp:      m.clock.Now().Format(time.RFC3339),
		JobID:          st.JobID,
		DocumentID:     st.DocumentID,
		VersionID:      st.VersionID,
		OrganizationID: st.OrganizationID,
		Status:         model.StatusInProgress,
		Stage:          model.StageAwaitingUserConfirmation,
	})
}

// publishFailed is the Manager's own single-FAILED helper for the
// pre-pipeline validation rejects (INVALID_CONTRACT_TYPE / USER_CONFIRMATION_-
// EXPIRED). It mirrors the 036 IsPublishableToOrchestrator gate: a
// non-publishable code (empty catalog UserMessage) is NEVER published with an
// empty error_message (build-spec D10 — the INVALID_ORG_ID_MISMATCH /
// IDEMPOTENCY_STORE_UNAVAILABLE path never reaches here). A PublishStatus
// error is logged but does not change the caller's decision.
func (m *Manager) publishFailed(ctx context.Context, cmd port.UserConfirmedType, code model.ErrorCode) {
	if !code.IsPublishableToOrchestrator() {
		m.log.Error(ctx, "non-publishable terminal code, not publishing FAILED (DLQ/NACK owns it)",
			"error_code", code.String(), "version_id", cmd.VersionID, "job_id", cmd.JobID)
		return
	}
	de := model.NewDomainError(code, model.StageAwaitingUserConfirmation)
	retry := de.Retryable
	evt := port.LICStatusChangedEvent{
		CorrelationID:  cmd.CorrelationID,
		Timestamp:      m.clock.Now().Format(time.RFC3339),
		JobID:          cmd.JobID,
		DocumentID:     cmd.DocumentID,
		VersionID:      cmd.VersionID,
		OrganizationID: cmd.OrganizationID,
		Status:         model.StatusFailed,
		Stage:          de.Stage,
		ErrorCode:      de.Code,
		ErrorMessage:   de.UserMessage,
		IsRetryable:    &retry,
	}
	if pErr := m.status.PublishStatus(ctx, evt); pErr != nil {
		m.log.Error(ctx, "FAILED status publish errored; decision stands",
			"publish_error", pErr, "error_code", code.String(), "version_id", cmd.VersionID)
	}
}

// publishInvalidDLQ routes a poison UserConfirmedType to lic.dlq.invalid-
// message with a PII-safe envelope (best-effort correlation fields from cmd;
// the adapter computes the HMAC of the raw payload — DLQPublisherPort godoc).
// A DLQ publish error is logged but does not change the caller's decision.
func (m *Manager) publishInvalidDLQ(ctx context.Context, cmd port.UserConfirmedType, code model.ErrorCode) {
	env := port.LICDLQEnvelope{
		OriginalTopic:  sourceTopicUserConfirmedType,
		ErrorCode:      code,
		ErrorMessage:   model.NewDomainError(code, model.StageAwaitingUserConfirmation).DevMessage,
		CorrelationID:  cmd.CorrelationID,
		JobID:          cmd.JobID,
		DocumentID:     cmd.DocumentID,
		VersionID:      cmd.VersionID,
		OrganizationID: cmd.OrganizationID,
		FailedAt:       m.clock.Now().Format(time.RFC3339),
	}
	if err := m.dlq.PublishDLQ(ctx, port.DLQTopicInvalidMessage, env); err != nil {
		m.log.Error(ctx, "DLQ publish errored; decision stands",
			"dlq_error", err, "error_code", code.String(), "version_id", cmd.VersionID)
	}
}

// setUserConfirmedCompleted moves lic-user-confirmed:{version_id} to
// COMPLETED (§6.10:777/782). A failure is logged but does not change the
// caller's decision (the 24h TTL reconciles an orphaned PROCESSING key).
func (m *Manager) setUserConfirmedCompleted(ctx context.Context, versionID string) {
	if err := m.idem.SetCompleted(ctx, keyPrefixUserConfirmed+versionID, m.cfg.CompletedTTL); err != nil {
		m.log.Error(ctx, "SetCompleted(lic-user-confirmed) failed; decision stands",
			"version_id", versionID, "cause", err)
	}
}

// auditLog emits the security.md §11.2 step 4 audit trail (build-spec D20 /
// RECONCILIATION R5): exactly one structured INFO line per
// HandleUserConfirmedType decision with validation_outcome. PII discipline:
// only IDs/enums (organization_id, version_id, confirmed_by_user_id,
// contract_type) — never document content (observability.md PII allowlist).
func (m *Manager) auditLog(ctx context.Context, cmd port.UserConfirmedType, outcome string) {
	m.log.Info(ctx, "LICUserConfirmedTypeAudit",
		"timestamp", m.clock.Now().Format(time.RFC3339),
		"version_id", cmd.VersionID,
		"organization_id", cmd.OrganizationID,
		"confirmed_by_user_id", cmd.UserID,
		"contract_type", cmd.ContractType,
		"validation_outcome", outcome)
}
