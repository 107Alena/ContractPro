// Package dmawaiter implements the LIC DM Artifact Awaiter and the DM
// Confirmation Awaiter (LIC-TASK-041, high-architecture.md §6.5 step 1, §6.12;
// integration-contracts.md §6.4; observability.md §3.5; error-handling.md
// §3.2). It is the in-process correlation registry wired between the broker
// ingress (LIC-TASK-039 consumer → LIC-TASK-040 router) and the pipeline
// orchestrator (LIC-TASK-036). Two exported types — one per DM round-trip
// shape — each satisfying THREE structural roles via thin adapters over a
// SHARED private deliver[T] dispatcher (build-spec D1/D2/D6, reconciliation
// R1):
//
//  1. The domain port: port.ArtifactsAwaiterPort /
//     port.PersistConfirmationAwaiterPort (Register/Await/Cancel) —
//     orchestrator-side.
//  2. The inbound handler: port.ArtifactsProvidedHandler /
//     port.PersistConfirmationHandler — the LIC-TASK-039 subscription
//     target shape.
//  3. The Router deliverer (already shipped at LIC-TASK-040 —
//     router.ArtifactsAwaiterDeliverer / router.PersistConfirmation-
//     Deliverer): a Deliver(key, payload) error method.
//
// Hermetic: stdlib + internal/domain/{model,port} only (build-spec D17,
// enforced by internal_test.go). It does NOT import internal/infra/* (every
// telemetry / clock / logger is seamed away — Metrics/Clock/Logger in
// seams.go); it does NOT import internal/application/pipeline (the
// orchestrator is the CALLER of Register/Await/Cancel — the reverse edge
// would be circular); it does NOT import internal/ingress/router (the
// Router-side deliverer seam is satisfied structurally — the var _
// assertion lives in the LIC-TASK-047 wiring package, build-spec D19); it
// does NOT import internal/application/pendingconfirmation /
// internal/ingress/consumer / internal/ingress/idempotency / internal/config
// (every forbidden internal listed in internal_test.go's forbiddenInternal
// is enforced active-fail, build-spec D17/D18).
//
// Concurrency: each awaiter is goroutine-safe by construction. The registry
// is map[string]*slot[T] guarded by a single sync.Mutex per instance
// (build-spec D3); the channel inside each slot is buffered cap=1 so a
// Deliver is non-blocking (build-spec D4); Cancel closes the channel UNDER
// the mutex to prevent send-on-closed-channel panics (build-spec D5,
// reviewer gate G6); Await's cleanup phase ALWAYS removes the slot from the
// registry on every exit path (success / timeout / ctx-cancel / closed-chan)
// with a defensive `if reg[key] == s` guard (build-spec D5, gates G8/G9);
// the metric write happens AFTER the lock release (build-spec D11, gate G10).
// There are ZERO background goroutines (build-spec D12, gate G14) — TTL is
// enforced lazily inside each Await via time.NewTimer.
//
// Design adjudicated by subagent code-architect (build-spec — decisions
// D1..D20, reconciliations R1..R3); implemented by subagent golang-pro. The
// authoritative reconciliations are recorded in this package's CLAUDE.md.
package dmawaiter

import (
	"context"
	"errors"
	"sync"
	"time"

	"contractpro/legal-intelligence-core/internal/domain/port"
)

// op label values for lic_dm_request_duration_seconds{op} /
// lic_dm_request_outcome_total{op,outcome} — observability.md §3.5 (verified
// via prompt + metrics/buckets.go:45). The two awaiter types hard-code their
// own op at the deliver call site (D11) — the metric labels themselves
// enforce a typo (a prometheus exemplars test would fail at 047 wiring).
const (
	opGetArtifacts     = "get_artifacts"
	opPersistArtifacts = "persist_artifacts"
)

// outcome label values for lic_dm_request_outcome_total{op,outcome} —
// observability.md §3.5. The four values are the universe across BOTH ops;
// classifyArtifactsOutcome emits {success,timeout,missing} and
// classifyConfirmationOutcome emits {success,timeout,persist_failed} on
// compliant inputs (reconciliation R3 records the defensive
// outcomeMissing under persist_artifacts as a bounded deviation).
const (
	outcomeSuccess       = "success"
	outcomeTimeout       = "timeout"
	outcomePersistFailed = "persist_failed"
	outcomeMissing       = "missing"
)

// slot is the per-registration registry entry, parametric over the payload
// type (build-spec D2). Channel cap=1 (build-spec D4) so deliver never
// blocks on a single Await/Deliver pair; createdAt is set at Register time
// for the duration metric (build-spec D11). Module is Go 1.26.1 — generics
// are sanctioned (go.mod).
type slot[T any] struct {
	ch        chan T
	createdAt time.Time
}

// ---------------------------------------------------------------------------
// ArtifactConfig — local config for ArtifactAwaiter (build-spec D8).
// ---------------------------------------------------------------------------

// ArtifactConfig carries the ArtifactAwaiter's safety-net TTL. Local struct,
// NO internal/config import (build-spec D17 — the pipeline.Config /
// pendingconfirmation.Config / router.Config precedent). LIC-TASK-047 maps
// config.PipelineConfig.DMRequestTimeout (LIC_DM_REQUEST_TIMEOUT, default 30s,
// validated >0 at startup — config/pipeline.go:22) → this.TTL.
type ArtifactConfig struct {
	// TTL is the per-Await safety-net timeout for an outstanding
	// GetArtifactsRequest → ArtifactsProvided round-trip. MUST be > 0.
	// The orchestrator-side caller (pipeline.requestAndAwaitCurrent at
	// orchestrator.go:820-832) ALSO wraps the Await call in its own
	// context.WithTimeout(o.cfg.DMRequestTimeout); the awaiter's own
	// TTL is the safety net for any caller that bypassed WithTimeout
	// (build-spec D8/D12). At LIC-TASK-047 wiring both values are
	// sourced from LIC_DM_REQUEST_TIMEOUT so the observable behaviour
	// is deterministic (whichever fires first; with equal values
	// either path triggers port.ErrAwaitTimeout).
	TTL time.Duration
}

// validate fails fast on misconfiguration. errors.New (NOT a domain error —
// this is a startup-time wiring defect) per the pendingconfirmation.Config.
// validate precedent.
func (c ArtifactConfig) validate() error {
	if c.TTL <= 0 {
		return errors.New("dmawaiter: ArtifactConfig.TTL must be > 0 (LIC_DM_REQUEST_TIMEOUT)")
	}
	return nil
}

// ---------------------------------------------------------------------------
// ConfirmationConfig — local config for ConfirmationAwaiter (build-spec D8).
// ---------------------------------------------------------------------------

// ConfirmationConfig carries the ConfirmationAwaiter's safety-net TTL. Same
// shape and semantics as ArtifactConfig (build-spec D8); kept as a distinct
// type so the 047 wiring binds the right env var per awaiter
// (LIC_DM_PERSIST_CONFIRM_TIMEOUT, default 30s, validated >0 at startup —
// config/pipeline.go:23). The ~3 LOC duplication buys zero ambiguity at the
// wiring layer.
type ConfirmationConfig struct {
	// TTL is the per-Await safety-net timeout for an outstanding
	// LegalAnalysisArtifactsReady → Persisted / PersistFailed round-trip.
	// MUST be > 0. See ArtifactConfig.TTL for the caller-also-wraps
	// semantics.
	TTL time.Duration
}

// validate fails fast on misconfiguration.
func (c ConfirmationConfig) validate() error {
	if c.TTL <= 0 {
		return errors.New("dmawaiter: ConfirmationConfig.TTL must be > 0 (LIC_DM_PERSIST_CONFIRM_TIMEOUT)")
	}
	return nil
}

// ---------------------------------------------------------------------------
// ArtifactAwaiter — port.ArtifactsAwaiterPort + port.ArtifactsProvidedHandler
// + router.ArtifactsAwaiterDeliverer (build-spec D1/D2/R1).
// ---------------------------------------------------------------------------

// ArtifactAwaiter is the in-process correlation_id → channel registry for
// outstanding GetArtifactsRequest / ArtifactsProvided round-trips
// (high-architecture.md §6.12). Immutable after NewArtifactAwaiter; the six
// exported methods (Register / Await / Cancel / HandleArtifactsProvided /
// Deliver, plus the inherited zero-value receiver semantics for empty maps
// — eagerly allocated) are goroutine-safe across distinct correlation IDs.
//
// Roles satisfied (verified at LIC-TASK-047 wiring time, build-spec D19):
//
//   - port.ArtifactsAwaiterPort        — Register / Await / Cancel
//   - port.ArtifactsProvidedHandler    — HandleArtifactsProvided
//   - router.ArtifactsAwaiterDeliverer — Deliver
type ArtifactAwaiter struct {
	cfg     ArtifactConfig
	mu      sync.Mutex
	reg     map[string]*slot[port.ArtifactsProvided]
	metrics Metrics
	clock   Clock
	log     Logger
}

// NewArtifactAwaiter validates the wiring and assembles the awaiter. It
// fails fast (NewTypeName per feedback_constructors.md; the pendingconfirmation.
// NewManager / pipeline.NewOrchestrator precedent): an invalid Config is a
// LIC-TASK-047 wiring defect and must be a startup error, not a first-call
// nil-deref. errors.Join surfaces ALL defects at once. The optional seams
// (Deps.Metrics / Clock / Logger) never cause an error; Deps.withDefaults
// substitutes a noop for each nil one (build-spec D9/D10).
//
// The registry map is eagerly allocated so the first concurrent Register
// does not race with map creation (build-spec D9).
func NewArtifactAwaiter(cfg ArtifactConfig, deps Deps) (*ArtifactAwaiter, error) {
	var errs []error
	if err := cfg.validate(); err != nil {
		errs = append(errs, err)
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	d := deps.withDefaults()
	return &ArtifactAwaiter{
		cfg:     cfg,
		reg:     make(map[string]*slot[port.ArtifactsProvided]),
		metrics: d.Metrics,
		clock:   d.Clock,
		log:     d.Logger,
	}, nil
}

// Register reserves a slot for correlationID and returns the receive-only
// channel the matching Await will block on. Register MUST be called BEFORE
// the GetArtifactsRequest is published (the port godoc on dm.go:80-87
// binds the caller; a response arriving first would find no registry slot
// and be silently dropped — see deliver). A second Register for the same
// correlationID before Await/Cancel is a contract violation; the awaiter
// rejects it with port.ErrDuplicateRegistration so the caller can log or
// DLQ the conflict (dm.go:65-70). Satisfies port.ArtifactsAwaiterPort.
func (a *ArtifactAwaiter) Register(correlationID string) (<-chan port.ArtifactsProvided, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, exists := a.reg[correlationID]; exists {
		return nil, port.ErrDuplicateRegistration
	}
	s := &slot[port.ArtifactsProvided]{
		ch:        make(chan port.ArtifactsProvided, 1),
		createdAt: a.clock.Now(),
	}
	a.reg[correlationID] = s
	return s.ch, nil
}

// Await blocks until the matching ArtifactsProvided arrives, the awaiter's
// safety-net TTL elapses, or ctx is cancelled / deadline-exceeded. Returns:
//
//   - (val, nil) on a successful Deliver.
//   - (zero, port.ErrAwaitTimeout) on TTL elapse OR on a previously-
//     Cancel'd channel (the channel-close branch — defensive, matches the
//     natural-timeout semantic, build-spec D5 NOTE).
//   - (zero, ctx.Err()) on ctx cancellation / deadline-exceeded (verbatim,
//     so the orchestrator's errors.Is(err, context.DeadlineExceeded)
//     branch behaves as designed — build-spec D13).
//
// Calling Await on a never-Registered key returns (zero, port.ErrAwaitTimeout)
// immediately and records NO metric (no slot ⇒ no createdAt ⇒ no duration;
// the orchestrator's existing errors.Is(err, port.ErrAwaitTimeout) branch
// covers the defensive case — build-spec D5 NOTE). Satisfies
// port.ArtifactsAwaiterPort.
func (a *ArtifactAwaiter) Await(ctx context.Context, correlationID string) (port.ArtifactsProvided, error) {
	a.mu.Lock()
	s := a.reg[correlationID]
	a.mu.Unlock()
	if s == nil {
		return port.ArtifactsProvided{}, port.ErrAwaitTimeout
	}

	timer := time.NewTimer(a.cfg.TTL)
	defer timer.Stop()

	var (
		val port.ArtifactsProvided
		err error
	)
	select {
	case v, ok := <-s.ch:
		if !ok {
			err = port.ErrAwaitTimeout
		} else {
			val = v
		}
	case <-ctx.Done():
		err = ctx.Err()
	case <-timer.C:
		err = port.ErrAwaitTimeout
	}

	duration := a.clock.Now().Sub(s.createdAt)
	a.cleanupSlot(ctx, correlationID, s, opGetArtifacts)

	outcome := classifyArtifactsOutcome(val, err)
	a.metrics.RecordOutcome(opGetArtifacts, outcome, duration.Seconds())

	return val, err
}

// Cancel releases the registry slot for correlationID without waiting and
// closes the channel under the mutex (so a concurrent Deliver cannot send
// to a closed channel — see deliver's lock-then-read-reg discipline,
// build-spec D5/D6, reviewer gate G6). Idempotent: a Cancel on an
// already-released key is a silent no-op (Await self-cleans on success/
// timeout; another Cancel is fine). Satisfies port.ArtifactsAwaiterPort.
func (a *ArtifactAwaiter) Cancel(correlationID string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	s, ok := a.reg[correlationID]
	if !ok {
		return
	}
	delete(a.reg, correlationID)
	close(s.ch)
}

// HandleArtifactsProvided routes the inbound DM response to the registered
// in-flight pipeline goroutine by evt.CorrelationID (high-architecture.md
// §6.12). Late delivery (registry miss — slot timed-out / Cancel'd) is
// silently dropped with a WARN log; duplicate delivery (channel full —
// first-wins) is silently dropped with a WARN log. Returns nil in both
// cases so the consumer ACKs the broker message (a late or duplicate
// response is not poison; broker at-least-once delivery makes duplicates
// normal — build-spec D6/D7, the LIC-TASK-040 router silent-ACK-on-
// duplicate precedent). Satisfies port.ArtifactsProvidedHandler.
func (a *ArtifactAwaiter) HandleArtifactsProvided(_ context.Context, evt port.ArtifactsProvided) error {
	return deliver(&a.mu, a.reg, evt.CorrelationID, evt, a.log, opGetArtifacts)
}

// Deliver is the Router-side companion to HandleArtifactsProvided (build-
// spec R1 — three doors, one room). LIC-TASK-040's router calls this with
// the correlation_id it extracted from the inbound envelope; both surfaces
// dispatch through the SAME private deliver helper so correctness is
// single-sourced. Returns the same nil-on-late-or-duplicate semantics as
// HandleArtifactsProvided (build-spec D6/D7). Satisfies
// router.ArtifactsAwaiterDeliverer.
func (a *ArtifactAwaiter) Deliver(correlationID string, evt port.ArtifactsProvided) error {
	return deliver(&a.mu, a.reg, correlationID, evt, a.log, opGetArtifacts)
}

// cleanupSlot removes the slot from the registry on every Await exit path
// (success, timeout, ctx-cancel, closed-chan). The defensive `reg[key] == s`
// guard prevents a parallel Cancel + a delayed delete from corrupting a
// freshly-Registered slot under the same key (build-spec D5, reviewer gate
// G9). The "truly impossible" branch (reg[key] != nil && reg[key] != s)
// emits an ERROR log so a regression is observable (build-spec D15).
func (a *ArtifactAwaiter) cleanupSlot(ctx context.Context, key string, s *slot[port.ArtifactsProvided], op string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	cur, present := a.reg[key]
	if !present {
		return
	}
	if cur == s {
		delete(a.reg, key)
		return
	}
	a.log.Error(ctx, "dmawaiter: registry slot identity mismatch on cleanup",
		"op", op, "key", key)
}

// ---------------------------------------------------------------------------
// ConfirmationAwaiter — port.PersistConfirmationAwaiterPort +
// port.PersistConfirmationHandler + router.PersistConfirmationDeliverer
// (build-spec D1/D2/R1).
// ---------------------------------------------------------------------------

// ConfirmationAwaiter is the in-process job_id → channel registry for
// outstanding LegalAnalysisArtifactsReady → Persisted / PersistFailed
// round-trips (high-architecture.md §6.5 step 9, §6.12). It is split from
// ArtifactAwaiter per ISP: the keys differ (job_id vs correlation_id), the
// TTLs differ at wiring time (different env vars, build-spec D8), and only
// the persist awaiter has the per-response failure mode where
// PersistConfirmation.Failure.IsRetryable steers the orchestrator's decision
// (the awaiter itself is a pure dispatcher and does NOT interpret
// IsRetryable — build-spec D20).
//
// Immutable after NewConfirmationAwaiter; the six exported methods are
// goroutine-safe across distinct job_ids.
//
// Roles satisfied (verified at LIC-TASK-047 wiring time, build-spec D19):
//
//   - port.PersistConfirmationAwaiterPort   — Register / Await / Cancel
//   - port.PersistConfirmationHandler       — HandlePersisted /
//     HandlePersistFailed
//   - router.PersistConfirmationDeliverer   — Deliver
type ConfirmationAwaiter struct {
	cfg     ConfirmationConfig
	mu      sync.Mutex
	reg     map[string]*slot[port.PersistConfirmation]
	metrics Metrics
	clock   Clock
	log     Logger
}

// NewConfirmationAwaiter validates the wiring and assembles the awaiter.
// Symmetric to NewArtifactAwaiter (build-spec D9). The registry map is
// eagerly allocated.
func NewConfirmationAwaiter(cfg ConfirmationConfig, deps Deps) (*ConfirmationAwaiter, error) {
	var errs []error
	if err := cfg.validate(); err != nil {
		errs = append(errs, err)
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	d := deps.withDefaults()
	return &ConfirmationAwaiter{
		cfg:     cfg,
		reg:     make(map[string]*slot[port.PersistConfirmation]),
		metrics: d.Metrics,
		clock:   d.Clock,
		log:     d.Logger,
	}, nil
}

// Register reserves a slot for jobID. See ArtifactAwaiter.Register for the
// lifecycle contract (Register-before-publish; second Register before
// Await/Cancel ⇒ port.ErrDuplicateRegistration). Satisfies
// port.PersistConfirmationAwaiterPort.
func (c *ConfirmationAwaiter) Register(jobID string) (<-chan port.PersistConfirmation, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, exists := c.reg[jobID]; exists {
		return nil, port.ErrDuplicateRegistration
	}
	s := &slot[port.PersistConfirmation]{
		ch:        make(chan port.PersistConfirmation, 1),
		createdAt: c.clock.Now(),
	}
	c.reg[jobID] = s
	return s.ch, nil
}

// Await blocks until the matching Persisted / PersistFailed arrives, the
// safety-net TTL elapses, or ctx is cancelled / deadline-exceeded. The
// awaiter delivers the typed port.PersistConfirmation envelope verbatim —
// it does NOT interpret IsRetryable (build-spec D20); the orchestrator's
// awaitPersist (orchestrator.go:1037-1042) flows conf.Failure.IsRetryable
// into the response code. Same error contract as ArtifactAwaiter.Await
// (build-spec D5/D13). Satisfies port.PersistConfirmationAwaiterPort.
func (c *ConfirmationAwaiter) Await(ctx context.Context, jobID string) (port.PersistConfirmation, error) {
	c.mu.Lock()
	s := c.reg[jobID]
	c.mu.Unlock()
	if s == nil {
		return port.PersistConfirmation{}, port.ErrAwaitTimeout
	}

	timer := time.NewTimer(c.cfg.TTL)
	defer timer.Stop()

	var (
		val port.PersistConfirmation
		err error
	)
	select {
	case v, ok := <-s.ch:
		if !ok {
			err = port.ErrAwaitTimeout
		} else {
			val = v
		}
	case <-ctx.Done():
		err = ctx.Err()
	case <-timer.C:
		err = port.ErrAwaitTimeout
	}

	duration := c.clock.Now().Sub(s.createdAt)
	c.cleanupSlot(ctx, jobID, s, opPersistArtifacts)

	outcome := classifyConfirmationOutcome(val, err)
	c.metrics.RecordOutcome(opPersistArtifacts, outcome, duration.Seconds())

	return val, err
}

// Cancel releases the registry slot for jobID and closes the channel under
// the mutex. Idempotent. Satisfies port.PersistConfirmationAwaiterPort.
func (c *ConfirmationAwaiter) Cancel(jobID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	s, ok := c.reg[jobID]
	if !ok {
		return
	}
	delete(c.reg, jobID)
	close(s.ch)
}

// HandlePersisted routes the inbound positive DM persist confirmation to
// the registered in-flight pipeline goroutine by evt.JobID. The envelope is
// constructed via port.NewPersistConfirmationSuccess(&evt) — evt is a value
// parameter so &evt is non-nil, satisfying the constructor's panic-on-nil
// precondition without further defensive code (build-spec D6 IMPORTANT
// note). Same nil-return-on-late-or-duplicate semantics as
// ArtifactAwaiter.HandleArtifactsProvided. Satisfies
// port.PersistConfirmationHandler.
func (c *ConfirmationAwaiter) HandlePersisted(_ context.Context, evt port.LegalAnalysisArtifactsPersisted) error {
	conf := port.NewPersistConfirmationSuccess(&evt)
	return deliver(&c.mu, c.reg, evt.JobID, conf, c.log, opPersistArtifacts)
}

// HandlePersistFailed routes the inbound negative DM persist confirmation
// to the registered in-flight pipeline goroutine by evt.JobID. The envelope
// is constructed via port.NewPersistConfirmationFailure(&evt) — same &evt
// non-nil guarantee as HandlePersisted. The awaiter does NOT interpret
// evt.IsRetryable (build-spec D20). Satisfies
// port.PersistConfirmationHandler.
func (c *ConfirmationAwaiter) HandlePersistFailed(_ context.Context, evt port.LegalAnalysisArtifactsPersistFailed) error {
	conf := port.NewPersistConfirmationFailure(&evt)
	return deliver(&c.mu, c.reg, evt.JobID, conf, c.log, opPersistArtifacts)
}

// Deliver is the Router-side companion to HandlePersisted / HandlePersist-
// Failed (build-spec R1 — three doors, one room). LIC-TASK-040's router
// constructs the fully-built port.PersistConfirmation envelope once (via
// the NewPersistConfirmation* constructors itself) and passes it through;
// the awaiter never tries to discriminate Success vs Failure on this
// surface. Satisfies router.PersistConfirmationDeliverer.
func (c *ConfirmationAwaiter) Deliver(jobID string, conf port.PersistConfirmation) error {
	return deliver(&c.mu, c.reg, jobID, conf, c.log, opPersistArtifacts)
}

// cleanupSlot is the ConfirmationAwaiter twin of ArtifactAwaiter.cleanupSlot
// (build-spec D5/D15, gates G8/G9).
func (c *ConfirmationAwaiter) cleanupSlot(ctx context.Context, key string, s *slot[port.PersistConfirmation], op string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	cur, present := c.reg[key]
	if !present {
		return
	}
	if cur == s {
		delete(c.reg, key)
		return
	}
	c.log.Error(ctx, "dmawaiter: registry slot identity mismatch on cleanup",
		"op", op, "key", key)
}

// ---------------------------------------------------------------------------
// Shared dispatch + outcome classification (build-spec D6/D11).
// ---------------------------------------------------------------------------

// deliver is the SHARED private dispatcher for both awaiters (build-spec
// D6 — single-sourced correctness). It holds the awaiter's mutex briefly,
// reads the slot pointer, and performs a non-blocking send via
// select-with-default. Two failure-equivalent paths drop with WARN +
// return nil (so the consumer ACKs the broker message):
//
//   - Registry-miss: slot timed out / was Cancel'd / never Registered.
//     A late response is not poison (the LIC-TASK-040 router silent-ACK-
//     on-duplicate precedent).
//   - Channel full: a duplicate Deliver for the same key while the first
//     value is still buffered. First-wins (the Await receiver is the
//     authoritative classifier; overwriting would race with
//     classifyOutcome — build-spec D7).
//
// The send is performed UNDER the mutex (build-spec D3 exception) so a
// concurrent Cancel cannot close the channel between the lookup and the
// send (which would panic: send on closed channel — reviewer gate G6).
// Cap=1 channel + select-with-default is the dual invariant that makes the
// send a finite-time operation under the lock (G5/G7).
//
// noCtx (context.Background) is the WARN log ctx: a nil context would
// crash a structured logger that calls ctx.Value (build-spec D6).
func deliver[T any](mu *sync.Mutex, reg map[string]*slot[T], key string, val T, log Logger, op string) error {
	mu.Lock()
	defer mu.Unlock()
	s, ok := reg[key]
	if !ok {
		log.Warn(context.Background(), "dmawaiter: deliver on missing slot (late / cancelled)",
			"op", op, "key", key)
		return nil
	}
	select {
	case s.ch <- val:
		return nil
	default:
		log.Warn(context.Background(), "dmawaiter: duplicate deliver dropped (channel full)",
			"op", op, "key", key)
		return nil
	}
}

// classifyArtifactsOutcome maps an Await exit state (val + err) to the
// observability.md §3.5 outcome label for lic_dm_request_outcome_total
// {op=get_artifacts,outcome} — build-spec D11. err != nil universally
// classifies as outcomeTimeout: a pure port.ErrAwaitTimeout (TTL elapse OR
// channel closed via Cancel) and a ctx-cancel / ctx-deadline are all
// "the request did not produce a usable response" — same SLO bucket. A
// successful Deliver with ErrorCode!="" (e.g. ADR-LIC-05's
// UNKNOWN_ARTIFACT_TYPE) or MissingTypes!=∅ classifies as outcomeMissing
// (the orchestrator at orchestrator.go:833-836 will map this to
// ErrCodeDMArtifactsMissing). All other success ⇒ outcomeSuccess.
func classifyArtifactsOutcome(val port.ArtifactsProvided, err error) string {
	if err != nil {
		if errors.Is(err, port.ErrAwaitTimeout) {
			return outcomeTimeout
		}
		return outcomeTimeout
	}
	if val.ErrorCode != "" || len(val.MissingTypes) > 0 {
		return outcomeMissing
	}
	return outcomeSuccess
}

// classifyConfirmationOutcome maps an Await exit state to the observability.md
// §3.5 outcome label for lic_dm_request_outcome_total{op=persist_artifacts,
// outcome} — build-spec D11. Err semantics identical to
// classifyArtifactsOutcome. On a successful Deliver, IsSuccess ⇒
// outcomeSuccess; IsFailure ⇒ outcomePersistFailed; the both-set /
// zero-value defensive fallback is outcomeMissing (build-spec R3 records
// this as a bounded deviation from observability.md §3.5's persist_artifacts
// outcome enumeration — unreachable via the constructor-protected wire
// path, +1 series under a programming bug only).
func classifyConfirmationOutcome(val port.PersistConfirmation, err error) string {
	if err != nil {
		if errors.Is(err, port.ErrAwaitTimeout) {
			return outcomeTimeout
		}
		return outcomeTimeout
	}
	if val.IsSuccess() {
		return outcomeSuccess
	}
	if val.IsFailure() {
		return outcomePersistFailed
	}
	return outcomeMissing
}
