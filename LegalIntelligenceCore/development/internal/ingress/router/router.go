// Package router implements the LIC Event Router / Dispatcher (LIC-TASK-040,
// high-architecture.md §6.2/§6.3/§6.5/§6.10, integration-contracts.md §6.4,
// observability.md §3.6). It is the inbound routing layer wired between the
// broker consumer (LIC-TASK-039, internal/ingress/consumer) and the
// application body: the pipeline orchestrator (LIC-TASK-036), the pending-
// confirmation manager (LIC-TASK-037), the DM awaiters (LIC-TASK-041), and
// the version-meta cache adapter (LIC-TASK-047). The package is hermetic
// against internal/infra/* (broker / kvstore / observability concretes),
// internal/application/pendingconfirmation (inverted via the local
// PendingConfirmationManager seam) and internal/ingress/{consumer,
// idempotency} (inverted via local seams). It imports
// internal/application/pipeline for ONE identity-comparable sentinel only —
// pipeline.ErrPipelinePaused + pipeline.IsPaused — the same pattern the
// orchestrator's Config.PausedSentinel uses to communicate paused-ness across
// the pendingconfirmation boundary without a circular import.
//
//   - NewRouter(Config, 8 required collaborators, Deps) (*Router, error) —
//     fail-fast errors.Join (build-spec D2). The required positional
//     collaborators are: PipelineRunner (036 orchestrator),
//     PendingConfirmationManager (037 manager), ArtifactsAwaiterDeliverer
//     and PersistConfirmationDeliverer (041 awaiters' ingress-side Deliver),
//     VersionMetaCacheWriter (047 kvstore adapter), IdempotencyGuard (038
//     Guard — the 7-method superset of port.IdempotencyStorePort),
//     port.PendingStatePort (037 forward — Redis-backed),
//     port.StatusPublisherPort (044). The Deps bundle carries the four
//     optional-with-noop telemetry seams (Metrics, Clock, Logger, Tracer).
//   - Six Route* methods satisfy the FROZEN consumer.EventRouter seam — the
//     var _ consumer.EventRouter = (*router.Router)(nil) satisfaction
//     assertion lives in the LIC-TASK-047 wiring package, NOT here.
//
// Design adjudicated by the authoritative build-spec
// (BUILD_SPEC_LIC_040.md — decisions D1..D14, reconciliations R1..R7).
// The package-level CLAUDE.md records the binding reconciliations
// (R1..R7 condensed) and the forward notes (041/044/046/047).
package router

import (
	"context"
	"errors"
	"time"

	"contractpro/legal-intelligence-core/internal/domain/model"
	"contractpro/legal-intelligence-core/internal/domain/port"
)

// Config carries the per-call TTLs the Router passes to the IdempotencyGuard.
// All durations are positional values (NOT internal/config — D3/D10
// hermeticity); LIC-TASK-047 sources them from cfg.Idempotency.{ProcessingTTL,
// TTL, HeartbeatInterval}. Local Config, ctor-injected (the pipeline.Config /
// pendingconfirmation.Config / idempotency.Config precedent).
type Config struct {
	// ProcessingTTL is the lic-trigger PROCESSING TTL passed to
	// CheckAndAcquire and extended every Config.HeartbeatInterval by the
	// Guard's heartbeat (build-spec D3/D14). From
	// LIC_IDEMPOTENCY_PROCESSING_TTL (default 150s,
	// config/idempotency.go:23). The Guard never hardcodes it (R3 in
	// LIC-TASK-038). Used ONLY for the lic-trigger key; the 4 two-status
	// keys (lic-version-created / lic-artifacts-resp / lic-persist-resp /
	// lic-persist-fail) do not need a PROCESSING slice.
	ProcessingTTL time.Duration

	// CompletedTTL is the per-call ttl passed to SetCompleted on the
	// lic-trigger key (success / terminal-failed flows) AND on every
	// 2-status key at first acquisition (build-spec D3/D4/D5/D6/D7). From
	// LIC_IDEMPOTENCY_TTL (default 24h, config/idempotency.go:22 — the
	// §6.3:565 / §6.10:782 "EX 24h"). MUST be >= ProcessingTTL.
	CompletedTTL time.Duration

	// PendingStateTTL is the ttl used when SetCompleted-ing the lic-trigger
	// key on a PAUSED→USER_CONFIRMATION_EXPIRED transition (build-spec D4
	// step 4b miss-branch / §6.10:782). Equal to CompletedTTL by spec;
	// kept as a distinct field so a future spec divergence does not require
	// touching D4 code. From LIC_IDEMPOTENCY_TTL (default 24h).
	PendingStateTTL time.Duration

	// MetaCacheTTL is the ttl for lic-version-meta:{version_id} written by
	// RouteVersionCreated (build-spec D5). From LIC_IDEMPOTENCY_TTL (default
	// 24h — §6.3 / orchestrator.go:765-777 "cache miss OR error ⇒ degrade to
	// INITIAL"). There is no dedicated env var; 047 sources from
	// cfg.Idempotency.TTL (R8 staleness recorded in CLAUDE.md).
	MetaCacheTTL time.Duration

	// HeartbeatInterval is INFORMATIONAL only — the Guard owns it
	// (idempotency.Config.HeartbeatInterval). The Router does NOT pass it
	// to StartHeartbeat (StartHeartbeat reads it from the Guard's own
	// Config). Kept here so the LIC-TASK-047 wiring layer cross-checks
	// Guard.cfg.HeartbeatInterval == router.cfg.HeartbeatInterval. From
	// LIC_IDEMPOTENCY_HEARTBEAT_INTERVAL (default 30s,
	// config/idempotency.go:24). DO NOT pass to any Guard method.
	HeartbeatInterval time.Duration
}

// validate enforces the binding invariants (build-spec D3). Each defect is a
// distinct errors.New so errors.Join surfaces ALL of them at once (the
// pipeline.Config.validate / idempotency.Config.validate precedent).
func (c Config) validate() error {
	var errs []error
	if c.ProcessingTTL <= 0 {
		errs = append(errs, errors.New("router: Config.ProcessingTTL must be > 0"))
	}
	if c.CompletedTTL <= 0 {
		errs = append(errs, errors.New("router: Config.CompletedTTL must be > 0"))
	}
	if c.PendingStateTTL <= 0 {
		errs = append(errs, errors.New("router: Config.PendingStateTTL must be > 0"))
	}
	if c.MetaCacheTTL <= 0 {
		errs = append(errs, errors.New("router: Config.MetaCacheTTL must be > 0"))
	}
	if c.HeartbeatInterval <= 0 {
		errs = append(errs, errors.New("router: Config.HeartbeatInterval must be > 0"))
	}
	if c.ProcessingTTL > 0 && c.CompletedTTL > 0 && c.CompletedTTL < c.ProcessingTTL {
		errs = append(errs, errors.New("router: Config.CompletedTTL must be >= ProcessingTTL"))
	}
	if c.HeartbeatInterval > 0 && c.ProcessingTTL > 0 && c.HeartbeatInterval >= c.ProcessingTTL {
		errs = append(errs, errors.New("router: Config.HeartbeatInterval must be < ProcessingTTL"))
	}
	return errors.Join(errs...)
}

// Key-prefix constants for the 5 idempotency-key families the Router writes
// (build-spec D14). The key family ↔ topic mapping is fixed by §6.3
// (high-architecture.md); the Router uses the helpers below — never direct
// string concatenation — so a typo is a compile error.
const (
	keyPrefixTrigger        = "lic-trigger:"         // 4-status: heartbeat-extended (D4)
	keyPrefixVersionCreated = "lic-version-created:" // 2-status: fire-once cache populator (D5)
	keyPrefixArtifactsResp  = "lic-artifacts-resp:"  // 2-status: per-correlation_id (D6)
	keyPrefixPersistResp    = "lic-persist-resp:"    // 2-status: per-job_id (D7 success)
	keyPrefixPersistFail    = "lic-persist-fail:"    // 2-status: per-job_id (D7 failure)
)

// keyTrigger returns the lic-trigger idempotency key for a versionID.
func keyTrigger(versionID string) string { return keyPrefixTrigger + versionID }

// keyVersionCreated returns the lic-version-created idempotency key for a
// versionID. Used as the 2-status guard around the lic-version-meta write.
func keyVersionCreated(versionID string) string { return keyPrefixVersionCreated + versionID }

// keyArtifactsResp returns the lic-artifacts-resp idempotency key for a
// correlation_id.
func keyArtifactsResp(correlationID string) string { return keyPrefixArtifactsResp + correlationID }

// keyPersistResp returns the lic-persist-resp idempotency key for a jobID
// (the LegalAnalysisArtifactsPersisted success topic — D7).
func keyPersistResp(jobID string) string { return keyPrefixPersistResp + jobID }

// keyPersistFail returns the lic-persist-fail idempotency key for a jobID
// (the LegalAnalysisArtifactsPersistFailed failure topic — D7).
func keyPersistFail(jobID string) string { return keyPrefixPersistFail + jobID }

// Router is the single exported type — the inbound routing layer. It is
// immutable after construction; all 6 Route* methods are goroutine-safe for
// distinct deliveries (no mutable per-instance state; the collaborators are
// goroutine-safe per their own contracts).
//
// The struct deliberately has NO JobLimiter / Semaphore / DLQPublisherPort
// field (build-spec R2/R4): the pipeline orchestrator acquires the
// concurrency semaphore itself (orchestrator.go:273, R2 — adding a second
// Acquire here would double-count the gauge), and DLQ publishing is split
// between Consumer (invalid-message), Manager (poison user-confirmed-type)
// and the LIC-TASK-046 publisher (publish-failed / agent-output-invalid /
// consumer-failed) — never Router (R4). The negative reflection-walk pins
// (TestRouter_NoJobLimiterField / TestRouter_NoDLQPublisherField) enforce
// these invariants.
type Router struct {
	cfg Config

	pipelineRunner    PipelineRunner
	pendingMgr        PendingConfirmationManager
	artifactDeliverer ArtifactsAwaiterDeliverer
	persistDeliverer  PersistConfirmationDeliverer
	versionMetaWriter VersionMetaCacheWriter
	idem              IdempotencyGuard

	pendingStateLoader port.PendingStatePort
	statusPub          port.StatusPublisherPort

	metrics Metrics
	clock   Clock
	log     Logger
	tracer  Tracer
}

// NewRouter constructs a *Router. cfg is a validated value; all 8 required
// collaborators are positional (the load-bearing rule — a Router missing any
// of them cannot route even the happy path); deps is optional-with-noop (the
// universal consumer.Deps / pendingconfirmation.Deps / pipeline.Deps
// precedent). On any failure it returns (nil, errors.Join(...)) collecting
// every offending arg + every Config defect at once (the consumer.NewConsumer
// / pendingconfirmation.NewManager / idempotency.NewGuard precedent —
// build-spec D2).
//
// port.DLQPublisherPort is NOT a parameter (build-spec R4 — the Router does
// NOT publish DLQ; pin TestRouter_NoDLQPublisherField enforces this at the
// struct level).
func NewRouter(
	cfg Config,
	pipelineRunner PipelineRunner,
	pendingMgr PendingConfirmationManager,
	artifactDeliverer ArtifactsAwaiterDeliverer,
	persistDeliverer PersistConfirmationDeliverer,
	versionMetaWriter VersionMetaCacheWriter,
	idempGuard IdempotencyGuard,
	pendingStateLoader port.PendingStatePort,
	statusPub port.StatusPublisherPort,
	deps Deps,
) (*Router, error) {
	var errs []error
	if err := cfg.validate(); err != nil {
		errs = append(errs, err)
	}
	if pipelineRunner == nil {
		errs = append(errs, errors.New("router: pipelineRunner (PipelineRunner) must not be nil"))
	}
	if pendingMgr == nil {
		errs = append(errs, errors.New("router: pendingMgr (PendingConfirmationManager) must not be nil"))
	}
	if artifactDeliverer == nil {
		errs = append(errs, errors.New("router: artifactDeliverer (ArtifactsAwaiterDeliverer) must not be nil"))
	}
	if persistDeliverer == nil {
		errs = append(errs, errors.New("router: persistDeliverer (PersistConfirmationDeliverer) must not be nil"))
	}
	if versionMetaWriter == nil {
		errs = append(errs, errors.New("router: versionMetaWriter (VersionMetaCacheWriter) must not be nil"))
	}
	if idempGuard == nil {
		errs = append(errs, errors.New("router: idempGuard (IdempotencyGuard) must not be nil"))
	}
	if pendingStateLoader == nil {
		errs = append(errs, errors.New("router: pendingStateLoader (port.PendingStatePort) must not be nil"))
	}
	if statusPub == nil {
		errs = append(errs, errors.New("router: statusPub (port.StatusPublisherPort) must not be nil"))
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}

	d := deps.withDefaults()
	return &Router{
		cfg:                cfg,
		pipelineRunner:     pipelineRunner,
		pendingMgr:         pendingMgr,
		artifactDeliverer:  artifactDeliverer,
		persistDeliverer:   persistDeliverer,
		versionMetaWriter:  versionMetaWriter,
		idem:               idempGuard,
		pendingStateLoader: pendingStateLoader,
		statusPub:          statusPub,
		metrics:            d.Metrics,
		clock:              d.Clock,
		log:                d.Logger,
		tracer:             d.Tracer,
	}, nil
}

// publishFailedTerminal publishes a single FAILED status event for the §6.5:631
// PAUSED-miss path (build-spec D4 step 4b miss-branch + R3). The Router is the
// SOLE caller of this private helper; it is NOT a generic FAILED-publisher —
// the pipeline orchestrator's publishFailed (orchestrator.go:1132) owns
// pipeline-terminal FAILED, the Manager's publishFailed
// (manager.go:317/318/361) owns user-confirmed-type FAILED. Mirrors the
// orchestrator's statusEvent shape (orchestrator.go:1101) including the
// IsPublishableToOrchestrator defensive gate (build-spec PART E #2 forward-
// compatibility).
func (r *Router) publishFailedTerminal(
	ctx context.Context,
	evt port.VersionProcessingArtifactsReady,
	code model.ErrorCode,
) {
	if !code.IsPublishableToOrchestrator() {
		// Defensive: build-spec PART E #2 forward-compat. v1 callers pass
		// USER_CONFIRMATION_EXPIRED only (non-empty UserMessage —
		// error_codes.go:211 ⇒ publishable), so this branch is unreachable
		// in v1 — but a future code addition with empty UserMessage would
		// silently corrupt the user-facing UX if we published it.
		r.log.Error(ctx,
			"router: non-publishable code on §6.5:631 path; logged only",
			"error_code", code.String(), "version_id", evt.VersionID)
		return
	}
	de := model.NewDomainError(code, model.StageAwaitingUserConfirmation)
	retry := de.Retryable
	pubEvt := port.LICStatusChangedEvent{
		CorrelationID:  evt.CorrelationID,
		Timestamp:      r.clock.Now().Format(time.RFC3339),
		JobID:          evt.JobID,
		DocumentID:     evt.DocumentID,
		VersionID:      evt.VersionID,
		OrganizationID: evt.OrganizationID,
		Status:         model.StatusFailed,
		Stage:          de.Stage,
		ErrorCode:      de.Code,
		ErrorMessage:   de.UserMessage,
		IsRetryable:    &retry,
	}
	if pErr := r.statusPub.PublishStatus(ctx, pubEvt); pErr != nil {
		// Log-and-continue: the §6.5:631 decision stands; a publish miss
		// is degradation, not a routing defect. The Router still
		// SetCompletes lic-trigger and ACKs.
		r.log.Error(ctx,
			"router: FAILED status publish errored on §6.5:631 path; decision stands",
			"publish_error", pErr,
			"error_code", code.String(),
			"version_id", evt.VersionID)
	}
}

// setCompletedSafe SetCompletes the given lic-trigger key with the per-call
// PendingStateTTL and log-and-continues on any Guard error (build-spec D4
// step 4b miss-branch / log-and-continue cleanup pattern — the
// pendingconfirmation manager.go:438-444 precedent). The Router returns nil
// (ACK) to the caller regardless: the message is terminal (FAILED already
// published), and failing to flip the slot to COMPLETED only orphans a
// PROCESSING/PAUSED key whose 24h TTL reconciles naturally.
func (r *Router) setCompletedSafe(ctx context.Context, key string, versionID string) {
	if cErr := r.idem.SetCompleted(ctx, key, r.cfg.PendingStateTTL); cErr != nil {
		r.log.Error(ctx,
			"router: SetCompleted(lic-trigger) failed on §6.5:631 path; orphan, TTL reconciles",
			"version_id", versionID, "cause", cErr)
	}
}
