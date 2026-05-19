# BUILD SPEC — LIC-TASK-040 Event Router / Dispatcher (AUTHORITATIVE)

**Status:** binding. The golang-pro implementer follows this verbatim and makes
**no further architecture decisions**. Every non-obvious ground-truth claim is
cited as `file:line`. Scope is strictly LIC-TASK-040; everything tagged
"041-owned" / "044-owned" / "046-owned" / "047-owned" is OUT OF SCOPE and
forward-noted.

- Package: `internal/ingress/router`
- Module: `contractpro/legal-intelligence-core`, Go 1.26.1
- Dev root: `LegalIntelligenceCore/development`
- Output exported type: `*Router`; constructor `NewRouter`
  (`feedback_constructors.md`)

This package is the **routing layer** wired between the broker consumer
(LIC-TASK-039 — `internal/ingress/consumer`) and the application body
(LIC-TASK-036 pipeline orchestrator + LIC-TASK-037 pending-confirmation
manager + LIC-TASK-041 DM awaiters + LIC-TASK-047 version-meta cache
adapter). It IS hermetic against `internal/infra/*` (no broker, no kvstore,
no metrics-concrete, no logger-concrete) and against
`internal/application/pendingconfirmation` (inverted via a local seam). It
imports `internal/application/pipeline` for ONE identity-comparable sentinel
only (`pipeline.ErrPipelinePaused` + `pipeline.IsPaused`) — the same pattern
the orchestrator's `Config.PausedSentinel` uses to communicate paused-ness
across the `pendingconfirmation` boundary without a circular import.

---

## 0. Verified ground truth (re-confirmed by reading source)

| Claim | Evidence |
|---|---|
| Frozen consumer seam: 6 typed Route* methods, return-contract `nil⇒Ack`, `error⇒Nack(false)` | `internal/ingress/consumer/seams.go:42-78` (`BrokerSubscriber`, `EventRouter`); consumer's `BUILD_SPEC_LIC_039.md` D8 + `CLAUDE.md` R1 — "the retry-level (x-death) routing of that Nack is 040's concern" |
| Consumer NEVER passes `broker.Delivery` to the router (D8 YAGNI) — only the typed DTO | `internal/ingress/consumer/seams.go:55-77` (`Route*` methods take typed DTOs, NOT `Delivery`); `BUILD_SPEC_LIC_039.md` D8 "no raw Delivery/x-death in the seam" |
| Consumer attaches per-delivery `RequestContext` to ctx ONCE before calling Route* (D6/R4) | `internal/ingress/consumer/seams.go:140-157` (`Logger.WithRequestContext`); 039 CLAUDE.md "ctx = Logger.WithRequestContext(ids) [once, R4]" |
| Guard API: `SetNX/Get/ExtendTTL/SetCompleted/SetPaused` + ergonomic `CheckAndAcquire` (status, alreadyExists, err) + `StartHeartbeat (stop func())` | `internal/ingress/idempotency/guard.go:160-285` (SetNX, CheckAndAcquire, Get, ExtendTTL, SetCompleted, SetPaused); `internal/ingress/idempotency/heartbeat.go:48-84` (StartHeartbeat) |
| `CheckAndAcquire` present-key returns `err==nil` + `alreadyExists=true` (no `ErrIdempotencyKeyExists`); transport-down consults `FallbackEnabled`; FallbackEnabled=true ⇒ `(Absent,false,nil)` + Lookup{fallback_db} + ERROR log; false ⇒ `(Absent,false,errVerbatim)` + WARN log | `internal/ingress/idempotency/guard.go:200-225`; `BUILD_SPEC_LIC_038.md` D3.2 |
| Guard returns kvstore-typed errors verbatim, NEVER `model.ErrorCode` | `BUILD_SPEC_LIC_038.md` R4 (Adapter returns kvstore/local-typed errors) |
| `pipeline.Orchestrator.Run(ctx, trigger) error`: nil ⇒ COMPLETED (Orchestrator publishes COMPLETED inline at step 21); non-nil ⇒ already-published terminal FAILED (`publishFailed` is the single FAILED-publish site); paused ⇒ `pipeline.ErrPipelinePaused` sentinel | `internal/application/pipeline/orchestrator.go:253-437`, `:529-535`, `:1132-1159`, `:721-741`; `pipeline/CLAUDE.md` "Run returns nil ⇒ COMPLETED" |
| Pipeline.Run **itself** acquires `JobLimiter` on raw inbound ctx (`o.limiter.Acquire(ctx)` step 1) — Router MUST NOT acquire | `internal/application/pipeline/orchestrator.go:273-295` (step 1 — Acquire BEFORE WithTimeout); `pipeline/CLAUDE.md` "Acquire BEFORE WithTimeout (binding)" |
| Pipeline owns terminal FAILED publish; Router MUST NOT publish FAILED on a pipeline-returned `*model.DomainError` | `internal/application/pipeline/orchestrator.go:344-348` (publishFailed in finalizer), `:1139-1159`; `pipeline/CLAUDE.md` D11 "single terminal path" |
| `pipeline.IsPaused(err) bool` is the identity-predicate for `ErrPipelinePaused`; Router calls it BEFORE `model.IsRetryable` | `internal/application/pipeline/orchestrator.go:738-741`; `pipeline/CLAUDE.md` "Pin 9 intact" |
| `pendingconfirmation.Manager.HandleUserConfirmedType(ctx, cmd) error`: nil ⇒ ACK; retryable ⇒ NACK→retry-DLX; non-retryable ⇒ DLQ (DLQ publish owned BY MANAGER for invalid-format / tenant-mismatch / non-retryable resume defects); SETNX `lic-user-confirmed:{version_id}` is **Manager-owned** | `internal/application/pendingconfirmation/manager.go:307-449`, `:317` (Manager publishes DLQ for INVALID_CONTRACT_TYPE), `:384` (Manager publishes DLQ for tenant mismatch); `pendingconfirmation/CLAUDE.md` R5 |
| `pendingconfirmation.Manager.RepublishPauseEvents(ctx, *PendingTypeConfirmation) error` — §6.5:631 / §6.10 Resume safety-net (re-publish only) | `internal/application/pendingconfirmation/manager.go:277-296`; `pendingconfirmation/CLAUDE.md` D7 |
| `port.PendingStatePort.Load(ctx, versionID)`: miss ⇒ `port.ErrPendingStateNotFound` | `internal/domain/port/pending.go:14`, `:33` |
| `port.StatusPublisherPort.PublishStatus(ctx, LICStatusChangedEvent) error` — owned by LIC-TASK-044 | `internal/domain/port/publisher.go:15-17`, `internal/domain/port/events.go:190-238` (LICStatusChangedEvent) |
| `port.DLQPublisherPort` — owned by LIC-TASK-046 (Router does NOT use it; Manager-owned for UCT non-retryable; pipeline-side DLQ is Orchestrator's `publishFailed` log-only gate at 036, not a real DLQ publish) | `internal/domain/port/publisher.go:38-40`; `internal/application/pipeline/orchestrator.go:1143-1146` (036 has no DLQPublisherPort by design) |
| Error catalog: `ErrCodeUserConfirmationExpired` retryable=false, **IsPublishableToOrchestrator=true** (non-empty UserMessage `:211`); `ErrCodeIdempotencyStoreUnavail` retryable=true, **IsPublishableToOrchestrator=false** (empty UserMessage `:228`); `ErrCodeInvalidMessageSchema` retryable=false; `ErrCodeInternal` retryable=true | `internal/domain/model/error_codes.go:209-213,221-225,226-230,114-118` |
| `port.ArtifactsAwaiterPort`/`PersistConfirmationAwaiterPort` have only Register/Await/Cancel — NO `Deliver` API in domain port (Router-side delivery is LIC-TASK-041 owned, behind local seams) | `internal/domain/port/dm.go:80-120` |
| broker `Delivery` interface is amqp091-free; main queue `x-dead-letter-exchange` has **no static `x-dead-letter-routing-key`** (§6.4 deviation) — retry-level escalation requires reading `XDeath()`, which Router cannot do because consumer seam does not pass Delivery | `internal/infra/broker/subscribe.go:31-55`, `:45-47` (XDeath); `internal/infra/broker/CLAUDE.md` "§6.4 deviation" |
| Config TTLs: `LIC_IDEMPOTENCY_PROCESSING_TTL`=150s, `LIC_IDEMPOTENCY_TTL`=24h, `LIC_IDEMPOTENCY_HEARTBEAT_INTERVAL`=30s, `LIC_PENDING_CONFIRMATION_TTL`=25h, `LIC_USER_CONFIRMED_PROCESSING_TTL`=90s, `LIC_PIPELINE_CONCURRENCY`=5 | `internal/config/idempotency.go:13-26`, `internal/config/pipeline.go:11-24` |
| DLQ topics enum: `DLQTopicInvalidMessage`/`ConsumerFailed`/`PublishFailed`/`AgentOutputInvalid` | `internal/domain/port/events.go:245-255` |
| Pipeline never imports router; consumer never imports router (inverted via 039's `EventRouter` seam). 047 wiring asserts `var _ consumer.EventRouter = (*router.Router)(nil)`. | `internal/ingress/consumer/seams.go:46-77` ("var _ EventRouter = ... lives in WIRING package, NOT here"); `internal/application/pipeline/CLAUDE.md` "047 wiring forward note #3" |
| Precedent style (039/038/037 build-spec format) | `internal/ingress/consumer/BUILD_SPEC_LIC_039.md`, `internal/ingress/idempotency/BUILD_SPEC_LIC_038.md`, `internal/application/pendingconfirmation` (D-/R-style spec in CLAUDE.md condensed) |

---

## PART A — BINDING DECISIONS (D1..D14)

### D1 — Package & file layout

`internal/ingress/router/` (new package `router`):

| File | Purpose (one line) |
|---|---|
| `router.go` | Package doc (hermetic statement + D-attribution); `Config`+`validate`; `Router` struct (the 9 required collaborators + the 4 optional seams); `NewRouter` constructor (fail-fast `errors.Join`); key-prefix constants + the 5 `keyXxx(id)` helpers (D14); the package-local `classifyXxx` helpers that map decisions to ack/nack-equivalent return values (D11). |
| `routes.go` | The 6 exported `Route*` methods (the `consumer.EventRouter` surface) — thin delegators to the per-topic private `routeXxx` functions; the per-topic flow bodies (D4..D8). |
| `seams.go` | Router-local seam interfaces + zero-dependency noop defaults: `PipelineRunner`/`PendingConfirmationManager` (required — no noop), `ArtifactsAwaiterDeliverer`/`PersistConfirmationDeliverer`/`VersionMetaCacheWriter`/`IdempotencyGuard` (required — no noop), `Metrics`+`noopMetrics` (optional), `Clock`+`systemClock` (optional), `Logger`+`noopLogger` (optional), `Tracer`+`noopTracer` (optional). `var _` after each noop pair. |
| `deps.go` | `Deps{Metrics, Clock, Logger, Tracer}` optional-with-noop bundle + `withDefaults()`. |
| `internal_test.go` | `TestHermeticImports` (allowlist + active-fail forbidden set) + `TestGofmtClean` (`go/format`). |
| `router_test.go` | Behavioural suite (PART C #1..#27), with in-package fakes for every seam + ports; `-race` clean, ZERO `time.Sleep` (uses faithful in-package fakes). |
| `CLAUDE.md` | Package guide mirroring `consumer/CLAUDE.md` shape (Files / API / Reconciliations / Conventions / Forward notes). Populated by golang-pro after implementation; this build-spec is the SSOT for the content. |

No other files. No subpackages.

### D2 — `NewRouter`, not `New` (feedback_constructors.md)

`feedback_constructors.md` mandates `NewTypeName`. The type is `Router`;
constructor is **`NewRouter`** (stutter-free; the
`consumer.NewConsumer` / `idempotency.NewGuard` /
`pendingconfirmation.NewManager` precedent).

**Exact signature (positional = required, fail-fast non-nil; Deps =
optional-with-noop):**

```go
func NewRouter(
    cfg Config,                                  // value; validated (D3)
    pipelineRunner PipelineRunner,               // required (D9)
    pendingMgr PendingConfirmationManager,       // required (D9)
    artifactDeliverer ArtifactsAwaiterDeliverer, // required (D9)
    persistDeliverer PersistConfirmationDeliverer, // required (D9)
    versionMetaWriter VersionMetaCacheWriter,    // required (D9)
    idempGuard IdempotencyGuard,                 // required (D9)
    pendingStateLoader port.PendingStatePort,    // required (frozen port)
    statusPub port.StatusPublisherPort,          // required (frozen port — D4 PAUSED-miss flow)
    deps Deps,                                   // optional (nil fields → noop)
) (*Router, error)
```

Fail-fast via `errors.Join` of per-arg errors
(`consumer.NewConsumer` / `idempotency.NewGuard` /
`pendingconfirmation.NewManager` precedent — `errors.Join` surfaces ALL
defects at once):

- `cfg.validate()` non-nil ⇒ appended (D3).
- Each of `pipelineRunner==nil`, `pendingMgr==nil`,
  `artifactDeliverer==nil`, `persistDeliverer==nil`,
  `versionMetaWriter==nil`, `idempGuard==nil`, `pendingStateLoader==nil`,
  `statusPub==nil` contributes a distinct `errors.New(...)`.
- On any failure return `(nil, joinedErr)`.
- On success store `deps = deps.withDefaults()`.

**Why positional vs Deps:** all 8 collaborators + `cfg` are load-bearing;
a Router missing any of them cannot perform its contract (cannot route
even the happy path). The 4 telemetry/determinism seams
(`Metrics`/`Clock`/`Logger`/`Tracer`) are optional-with-noop in `Deps`
(the universal `consumer.Deps`/`pendingconfirmation.Deps`/`pipeline.Deps`
precedent).

**`port.DLQPublisherPort` is NOT a NewRouter param.** The Router does
NOT publish DLQ (R4 — Manager owns it for UserConfirmedType; consumer
owns it for invalid-message; 046 owns publish-failed/agent-output-
invalid). Adding a DLQ port here would invert R4 — recorded.

### D3 — `Config` layout

Local `Config{...}` struct, NO `internal/config` import (the
`pipeline.Config` / `pendingconfirmation.Config` / `consumer.dlqHashKey`
"local config, ctor-injected, NOT internal/config import" precedent;
hermeticity — D10). Fields:

```go
type Config struct {
    // ProcessingTTL is the lic-trigger PROCESSING TTL the Router passes
    // to CheckAndAcquire (PROCESSING) — extended by heartbeat. From
    // LIC_IDEMPOTENCY_PROCESSING_TTL (default 150s,
    // config/idempotency.go:23). MUST equal the value 036 expects (a 047
    // wiring invariant). Used ONLY for lic-trigger; the 2-status keys
    // (lic-version-created/lic-artifacts-resp/lic-persist-resp/lic-
    // persist-fail) do not need a PROCESSING slice.
    ProcessingTTL time.Duration

    // CompletedTTL is the per-call ttl the Router passes to SetCompleted
    // (lic-trigger on success/terminal-failed/paused-expired flows AND
    // every 2-status key on first acquisition). From
    // LIC_IDEMPOTENCY_TTL (default 24h, config/idempotency.go:22 — the
    // §6.3:565/§6.10:782 "EX 24h"). MUST be >= ProcessingTTL.
    CompletedTTL time.Duration

    // PendingStateTTL is the ttl used when SetCompleted-ing the
    // lic-trigger key on a PAUSED→USER_CONFIRMATION_EXPIRED transition
    // (D4 step "PAUSED + pending-miss"): the §6.10:782 ACK path. Equal
    // to CompletedTTL by spec (the §6.10:782 "EX 24h"); kept as a
    // distinct field for clarity in tests + so a future spec divergence
    // does not require touching D4 code. From
    // LIC_IDEMPOTENCY_TTL (default 24h). Mirror of CompletedTTL — the
    // validate() invariant pins them equal.
    PendingStateTTL time.Duration

    // MetaCacheTTL is the ttl for lic-version-meta:{version_id} written
    // by RouteVersionCreated. From LIC_IDEMPOTENCY_TTL (default 24h —
    // §6.3 / orchestrator.go:765-777 "cache miss OR error ⇒ degrade to
    // INITIAL"). There is no dedicated env var; 047 sources from
    // cfg.Idempotency.TTL (recorded — R8 staleness vs hypothetical
    // LIC_VERSION_META_TTL).
    MetaCacheTTL time.Duration

    // HeartbeatInterval is INFORMATIONAL only — the Guard owns it
    // (idempotency.Config.HeartbeatInterval). The Router does NOT pass
    // it to StartHeartbeat (StartHeartbeat reads it from the Guard's
    // own Config). Kept here as a frozen documented invariant so the
    // wiring layer (047) cross-checks Guard.cfg.HeartbeatInterval ==
    // router.cfg.HeartbeatInterval. From LIC_IDEMPOTENCY_HEARTBEAT_-
    // INTERVAL (default 30s, config/idempotency.go:24). DO NOT pass to
    // any Guard method.
    HeartbeatInterval time.Duration
}
```

`validate()` (fail-fast on misconfiguration — the
`pipeline.Config.validate` / `idempotency.Config.validate` precedent):

```go
func (c Config) validate() error {
    var errs []error
    if c.ProcessingTTL <= 0 { errs = append(errs, errors.New("router: Config.ProcessingTTL must be > 0")) }
    if c.CompletedTTL <= 0  { errs = append(errs, errors.New("router: Config.CompletedTTL must be > 0")) }
    if c.PendingStateTTL <= 0 { errs = append(errs, errors.New("router: Config.PendingStateTTL must be > 0")) }
    if c.MetaCacheTTL <= 0  { errs = append(errs, errors.New("router: Config.MetaCacheTTL must be > 0")) }
    if c.HeartbeatInterval <= 0 { errs = append(errs, errors.New("router: Config.HeartbeatInterval must be > 0")) }
    if c.ProcessingTTL > 0 && c.CompletedTTL > 0 && c.CompletedTTL < c.ProcessingTTL {
        errs = append(errs, errors.New("router: Config.CompletedTTL must be >= ProcessingTTL"))
    }
    if c.HeartbeatInterval > 0 && c.ProcessingTTL > 0 && c.HeartbeatInterval >= c.ProcessingTTL {
        errs = append(errs, errors.New("router: Config.HeartbeatInterval must be < ProcessingTTL"))
    }
    return errors.Join(errs...)
}
```

### D4 — `RouteVersionArtifactsReady` — §6.5:624-634 restart-decision-tree

This is the most complex routing flow. The full algorithm — PINNED
pseudocode:

```
key := "lic-trigger:" + evt.VersionID

// (1) Atomic SETNX-or-read-existing via the Guard's single Lua Eval
//     (idempotency D3.2). FallbackEnabled is the Guard's concern; if it
//     fires we observe (Absent,false,nil) here.
status, alreadyExists, gErr := r.idem.CheckAndAcquire(ctx, key, r.cfg.ProcessingTTL)

// (2) Transport error path (FallbackEnabled=false on the Guard):
if gErr != nil {
    // *model.DomainError{IDEMPOTENCY_STORE_UNAVAILABLE, retryable=true}
    // wrapping gErr (R1 — Router owns the model-mapping; Guard returns
    // kvstore err verbatim per BUILD_SPEC_LIC_038 R4).
    // IDEMPOTENCY_STORE_UNAVAILABLE is non-publishable (empty
    // UserMessage, error_codes.go:228) — Consumer Nack(false)→DLX,
    // pipeline never reached, no FAILED on the wire.
    return model.NewDomainError(model.ErrCodeIdempotencyStoreUnavail, model.StageReceived).
        WithCause(gErr).
        WithDevMessage("router: CheckAndAcquire(lic-trigger) failed; NACK→retry-DLX")
}

// (3) (Absent, false, nil) — acquired, we own the slot.
if !alreadyExists {
    // (3a) Start heartbeat — Guard.StartHeartbeat uses its own
    //      cfg.HeartbeatInterval; we supply the per-tick ttl that EXPIRE
    //      will refresh against (ProcessingTTL — the value the SET-NX-EX
    //      just wrote, R3-038 "TTLs per-call").
    stopHB := r.idem.StartHeartbeat(ctx, key, r.cfg.ProcessingTTL)
    defer stopHB() // sync.Once-guarded, twice-safe (heartbeat.go:53-55)

    // (3b) Drive the pipeline. pipeline.Orchestrator.Run:
    //      - itself Acquires JobLimiter on RAW ctx (orchestrator.go:273)
    //        — Router MUST NOT pre-Acquire (R2)
    //      - publishes its own COMPLETED inline (orchestrator.go:529)
    //        or terminal FAILED via publishFailed (orchestrator.go:346)
    //      - returns:
    //         nil                 ⇒ COMPLETED
    //         ErrPipelinePaused   ⇒ paused (037 already SetPaused
    //                              lic-trigger + published the 2
    //                              pause events; Router must NOT
    //                              SetCompleted)
    //         *model.DomainError  ⇒ already-FAILED (publishFailed done)
    runErr := r.pipelineRunner.Run(ctx, evt)

    // (3c) Outcome routing — pipeline.IsPaused BEFORE model.IsRetryable
    //      (pipeline/CLAUDE.md "Pin 9 intact"):
    if pipeline.IsPaused(runErr) {
        // 037 already SetPaused(lic-trigger) — DO NOT SetCompleted here.
        // ACK the source: return nil.
        return nil
    }
    if runErr == nil {
        // Terminal success — finalize lic-trigger=COMPLETED 24h.
        if cErr := r.idem.SetCompleted(ctx, key, r.cfg.CompletedTTL); cErr != nil {
            // Log-and-continue (the §6.3 / pendingconfirmation precedent
            // manager.go:438-444 "log but return nil" for cleanup): the
            // pipeline is COMPLETED on the wire; failing to flip the
            // lic-trigger to COMPLETED would re-run on redelivery
            // (pipeline.Orchestrator dedups via persist-confirm; a
            // duplicate run is wasteful but correct). The 150s
            // PROCESSING TTL expires naturally; orphan acceptable.
            r.log.Error(ctx, "router: SetCompleted(lic-trigger) failed after pipeline COMPLETED; orphan PROCESSING key, TTL will reconcile",
                "version_id", evt.VersionID, "cause", cErr)
        }
        return nil // ACK
    }
    // runErr is a *model.DomainError — pipeline ALREADY published FAILED.
    // We finalize lic-trigger=COMPLETED 24h (terminal state, NOT a
    // re-run path; non-retryable failures must not redeliver to a
    // half-paused state, and retryable failures are owned by
    // model.IsRetryable on the broker boundary — but the canonical
    // contract is: pipeline emitted the SINGLE terminal status, the
    // restart-tree path COMPLETED handler (§6.5:632) returns ACK).
    if cErr := r.idem.SetCompleted(ctx, key, r.cfg.CompletedTTL); cErr != nil {
        r.log.Error(ctx, "router: SetCompleted(lic-trigger) failed after pipeline FAILED; orphan, TTL reconciles",
            "version_id", evt.VersionID, "cause", cErr)
    }
    // Return the pipeline's *model.DomainError verbatim — Consumer maps
    // via model.IsRetryable: retryable ⇒ Nack→retry-DLX (eventually
    // consumer-failed); non-retryable ⇒ Nack→DLX too (broker §6.4
    // deviation — main queue has no static routing key, see R1). Router
    // does NOT swallow the error; the Consumer's exactly-one Nack(false)
    // is the SSOT.
    return runErr
}

// (4) (status, true, nil) — present.
switch status {
case port.IdempotencyProcessing:
    // (4a) Concurrent in-flight run. Nack→retry-DLX (§6.5:629 — "ждать
    //      или NACK для повтора"; the redelivery hits the same key,
    //      may then see COMPLETED). Return a retryable
    //      *model.DomainError so model.IsRetryable==true.
    //      IDEMPOTENCY_STORE_UNAVAILABLE is wrong (the store IS
    //      available); INTERNAL_ERROR(retryable=true) is the closest
    //      non-publishable equivalent — but INTERNAL_ERROR is
    //      publishable (catalog row error_codes.go:221-225, UserMessage
    //      non-empty). We MUST NOT publish FAILED here (the in-flight
    //      run will publish its own terminal status). Therefore: emit
    //      *model.DomainError{ErrCodeIdempotencyStoreUnavail, retryable=true}
    //      with a devMessage stating "concurrent in-flight, NACK→retry"
    //      (mirroring the pendingconfirmation D7 PROCESSING/PAUSED
    //      precedent — manager.go:333-340 "concurrent in-flight resume
    //      NACK→retry-DLX"). Code is non-publishable so no FAILED on
    //      the wire (gate at 036 publishFailed mirrors here).
    return model.NewDomainError(model.ErrCodeIdempotencyStoreUnavail, model.StageReceived).
        WithRetryable(true).
        WithDevMessage("router: lic-trigger=PROCESSING — concurrent in-flight pipeline; NACK→retry-DLX")

case port.IdempotencyPaused:
    // (4b) PAUSED — §6.5:631 safety-net. Load lic-pending-state.
    ptc, lErr := r.pendingStateLoader.Load(ctx, evt.VersionID)
    if lErr != nil {
        if errors.Is(lErr, port.ErrPendingStateNotFound) {
            // §6.5:631 + §6.10:777 "PENDING_STATE_LOST" — R3: there is
            // no such catalog code; use USER_CONFIRMATION_EXPIRED (037
            // R2 precedent — manager.go:355-364). FAILED is publishable
            // (catalog row error_codes.go:209-213 has non-empty
            // UserMessage; IsPublishableToOrchestrator=true). Publish
            // FAILED via the Router's statusPub (NOT pipeline's
            // publishFailed — pipeline never ran for this delivery).
            // Then SetCompleted lic-trigger 24h (§6.10:782) — closes
            // the slot; redelivery sees COMPLETED ⇒ ACK without work.
            // ACK (the message is terminal); NO DLQ (an expired pause
            // is not poison — 037 R5 precedent manager.go:360-364).
            r.publishFailedTerminal(ctx, evt, model.ErrCodeUserConfirmationExpired)
            r.setCompletedSafe(ctx, key) // log-and-continue
            return nil // ACK
        }
        // Other Load error (Redis transient on lic-pending-state) —
        // NACK→retry-DLX. PendingStatePort is Redis-backed but goes
        // through a separate adapter (047 — pendingstate package); a
        // transient there mirrors the lic-trigger transient (R1):
        // non-publishable IDEMPOTENCY_STORE_UNAVAILABLE retryable.
        return model.NewDomainError(model.ErrCodeIdempotencyStoreUnavail, model.StageReceived).
            WithRetryable(true).
            WithCause(lErr).
            WithDevMessage("router: lic-pending-state Load failed; NACK→retry-DLX")
    }
    // PAUSED + pending-state present — §6.5:631 republish.
    if rErr := r.pendingMgr.RepublishPauseEvents(ctx, ptc); rErr != nil {
        // Manager returns *model.DomainError (manager.go:283-294 —
        // ALWAYS retryable INTERNAL_ERROR on republish failure). Return
        // verbatim so Consumer NACK→retry-DLX.
        return rErr
    }
    return nil // ACK — Stage 1 NOT restarted (§6.5:631)

case port.IdempotencyCompleted:
    // (4c) Already done — §6.5:632 "ACK без обработки".
    return nil

default:
    // (4d) Defensive: parseStatus maps unknown → PROCESSING (idempotency
    //      D5). This case is unreachable but the switch is exhaustive
    //      for review clarity — same handling as PROCESSING (NACK
    //      retry).
    return model.NewDomainError(model.ErrCodeIdempotencyStoreUnavail, model.StageReceived).
        WithRetryable(true).
        WithDevMessage("router: lic-trigger unexpected status; NACK→retry-DLX")
}
```

Notes the implementer MUST honour:

- **`stopHB()` is deferred IMMEDIATELY after a successful acquisition**
  (3a) so a panic inside `r.pipelineRunner.Run` still stops the
  heartbeat. The `stop func()` is `sync.Once`-guarded (`heartbeat.go:53`)
  — safe even if the goroutine already exited via `<-ctx.Done()`.
- **Router NEVER calls `Guard.SetPaused` on lic-trigger** — that is
  037 Manager's job (manager.go:251) when the pipeline pauses. The
  Router only observes the resulting PAUSED status via
  `CheckAndAcquire` on redelivery.
- **`r.pipelineRunner.Run(ctx, evt)` is called WITH the ctx the consumer
  handed us** — Consumer (039) already called `Logger.WithRequestContext`
  on it (consumer/seams.go:144-156, 039 D6/R4). Router does NOT
  re-wrap.
- **No additional acquire**: pipeline.Orchestrator.Run itself calls
  `o.limiter.Acquire(ctx)` step 1 (orchestrator.go:273) — this is R2.

### D5 — `RouteVersionCreated` — version-meta cache write

The version-meta cache is the FALLBACK for the §8.3 race where
`VersionProcessingArtifactsReady` arrives without `parent_version_id`
but a `VersionCreated` for the same version has already populated the
cache (036 DEFECT-1 — orchestrator.go:765-777). The Router's job is
exactly to be that populator.

```
key := "lic-version-created:" + evt.VersionID

// 2-status guard (no heartbeat, no PAUSED branch — the write is a
// fire-once cache populator). Per-call ttl = CompletedTTL (24h) — the
// SETNX writes PROCESSING-ttl-24h (idempotency D4 atomic Lua), and on
// success we IMMEDIATELY SetCompleted (overwriting the PROCESSING value
// with COMPLETED, keeping the 24h ttl).
status, alreadyExists, gErr := r.idem.CheckAndAcquire(ctx, key, r.cfg.CompletedTTL)

if gErr != nil {
    // Transport error. The version-meta cache is a degradation-fallback
    // (036 DEFECT-1: trigger.ParentVersionID is PRIMARY; cache is
    // consulted IFF trigger lacks it; a cache miss/error degrades to
    // INITIAL, NEVER fails). The Router degrades symmetrically:
    // silently ACK so the cache stays empty for this version_id —
    // pipeline will degrade to INITIAL if a later RE_CHECK arrives
    // without ParentVersionID on the trigger. WARN log only.
    r.log.Warn(ctx, "router: CheckAndAcquire(lic-version-created) Redis-down; cache write skipped (pipeline degrades to INITIAL if trigger lacks parent_version_id)",
        "version_id", evt.VersionID, "cause", gErr)
    return nil // ACK — defensible degrade per 036 DEFECT-1
}

if !alreadyExists {
    // First sighting — write the meta payload, then flip status to
    // COMPLETED.
    payload, jErr := json.Marshal(struct {
        ParentVersionID *string `json:"parent_version_id,omitempty"`
        OriginType      string  `json:"origin_type,omitempty"`
    }{
        ParentVersionID: evt.ParentVersionID,
        OriginType:      evt.OriginType,
    })
    if jErr != nil {
        // Cannot happen for the simple struct (no unmarshalable fields)
        // — log + ACK (cache miss degrade). Defensive: never NACK on a
        // local marshal defect; it would loop forever.
        r.log.Error(ctx, "router: VersionCreated payload marshal failed; cache write skipped",
            "version_id", evt.VersionID, "cause", jErr)
        return nil
    }
    if wErr := r.versionMetaWriter.Set(ctx, evt.VersionID, payload, r.cfg.MetaCacheTTL); wErr != nil {
        // Cache write failed — degrade silently (036 DEFECT-1). The
        // lic-version-created PROCESSING marker stays for 24h; a
        // redelivery would re-enter this branch and try again. We do
        // NOT SetCompleted here — leave the slot "PROCESSING" so a
        // redelivery retries the Set (the cache value, not the
        // PROCESSING flag, is what matters; a successful Set then
        // SetCompleted on the second pass is the intended convergence).
        r.log.Warn(ctx, "router: lic-version-meta Set failed; redelivery will retry",
            "version_id", evt.VersionID, "cause", wErr)
        return nil // ACK — degrade per 036 DEFECT-1
    }
    if cErr := r.idem.SetCompleted(ctx, key, r.cfg.CompletedTTL); cErr != nil {
        r.log.Error(ctx, "router: SetCompleted(lic-version-created) failed after meta write; orphan PROCESSING, TTL reconciles",
            "version_id", evt.VersionID, "cause", cErr)
    }
    return nil
}

// alreadyExists — duplicate. The §6.3 2-status semantics: any
// non-Absent ⇒ ACK (the cache was already populated on the original
// delivery). PROCESSING/PAUSED for a 2-status key is defensive (D5
// idempotency parseStatus garbage → PROCESSING); treat as a stale
// in-flight or a completed-but-mid-transition — ACK either way (the
// cache is at best populated, at worst we degrade to INITIAL — both
// safe per DEFECT-1).
return nil
```

### D6 — `RouteArtifactsProvided` — DM artifacts response routing

```
key := "lic-artifacts-resp:" + evt.CorrelationID

status, alreadyExists, gErr := r.idem.CheckAndAcquire(ctx, key, r.cfg.CompletedTTL)

if gErr != nil {
    // Transport-class. The Awaiter is the in-process registry the
    // pipeline goroutine is blocked on (dm.go:80-96, port).
    // FallbackEnabled=true on the Guard ⇒ (Absent,false,nil) here, and
    // we proceed to Deliver — correct (an idempotent Deliver to the
    // awaiter slot is fine; the slot is single-receiver and the
    // duplicate would be silently dropped by 041's implementation).
    //
    // FallbackEnabled=false ⇒ gErr non-nil here. Returning a NACK
    // would loop the response back through retry-DLX while the
    // pipeline goroutine times out at DM_REQUEST_TIMEOUT (30s) — same
    // observable outcome but more wasted IO. Safer: degrade to ACK +
    // best-effort Deliver — the awaiter may already be cancelled
    // (timeout fired), in which case Deliver is a noop on the 041
    // side.
    r.log.Warn(ctx, "router: CheckAndAcquire(lic-artifacts-resp) Redis-down; best-effort Deliver, ACK",
        "correlation_id", evt.CorrelationID, "cause", gErr)
    _ = r.artifactDeliverer.Deliver(evt.CorrelationID, evt)
    return nil
}

if !alreadyExists {
    if dErr := r.artifactDeliverer.Deliver(evt.CorrelationID, evt); dErr != nil {
        // Awaiter registry-miss (slot timed out + Cancel'd) or other
        // local error. The pipeline goroutine has either already
        // received the response on a different correlation_id (impossible
        // — the suffix is unique per request) OR timed out at
        // DM_ARTIFACTS_TIMEOUT and moved on. Either way the response
        // is dead-letter material: silently ACK + WARN log (the
        // pipeline will publish FAILED{DM_ARTIFACTS_TIMEOUT} itself
        // — orchestrator.go:826).
        r.log.Warn(ctx, "router: Deliver(ArtifactsProvided) registry-miss/timeout; ACK silently",
            "correlation_id", evt.CorrelationID, "cause", dErr)
        return nil
    }
    if cErr := r.idem.SetCompleted(ctx, key, r.cfg.CompletedTTL); cErr != nil {
        r.log.Error(ctx, "router: SetCompleted(lic-artifacts-resp) failed; orphan, TTL reconciles",
            "correlation_id", evt.CorrelationID, "cause", cErr)
    }
    return nil
}

// alreadyExists — duplicate response. ACK silently; the awaiter has
// either already received or moved past (single-receiver slot
// semantics). No metric (D12).
return nil
```

### D7 — `RoutePersisted` / `RoutePersistFailed` — DM persist confirmation routing

Both methods share the same structure as D6, with two differences: (i)
key prefix per topic, (ii) `port.PersistConfirmation` envelope built via
`port.NewPersistConfirmationSuccess` / `NewPersistConfirmationFailure`
(dm.go:138-152 — these constructors panic on nil; the Router passes the
ALWAYS-non-zero `&evt` so the precondition is structurally satisfied).

```
// RoutePersisted:
key := "lic-persist-resp:" + evt.JobID
// ... CheckAndAcquire(ctx, key, CompletedTTL) ...
//   acquired ⇒ r.persistDeliverer.Deliver(evt.JobID, port.NewPersistConfirmationSuccess(&evt))
//             then SetCompleted

// RoutePersistFailed:
key := "lic-persist-fail:" + evt.JobID
// ... CheckAndAcquire(ctx, key, CompletedTTL) ...
//   acquired ⇒ r.persistDeliverer.Deliver(evt.JobID, port.NewPersistConfirmationFailure(&evt))
//             then SetCompleted
```

The pipeline's `awaitPersist` (orchestrator.go:1017-1046) calls
`persistAwait.Await(ctx, jobID)` and the awaiter returns the
`PersistConfirmation` envelope — discriminated via `IsSuccess`/`IsFailure`
(dm.go:157-173). The Router's job is only to **route** the typed
envelope into the awaiter slot; the orchestrator's `awaitPersist`
classifies failure (DM_PERSIST_FAILED with `conf.Failure.IsRetryable`
flowing through to the response code, orchestrator.go:1037-1042) — the
Router does NOT inspect `IsRetryable` here.

Two distinct keys (lic-persist-resp:{job_id} for Persisted /
lic-persist-fail:{job_id} for PersistFailed) because §6.3 separates them
(two distinct topics, two distinct idempotency surfaces). A redelivered
Persisted does NOT collide with a PersistFailed for the same job_id;
the awaiter receives whichever arrived first (the awaiter's job_id slot
is single-receiver — dm.go:106-120).

### D8 — `RouteUserConfirmedType` — Manager-driven (Router does not guard)

```
// The Manager (037) OWNS the SETNX lic-user-confirmed:{version_id}
// guard (manager.go:325; pendingconfirmation/CLAUDE.md "Manager
// Resume idempotency"), the contract_type validation (regex + 12-
// whitelist), the tenant check, and the DLQ publication for
// invalid-format / invalid-whitelist / tenant-mismatch (manager.go:
// 317, 384, 600-616). The Router transparently delegates.

err := r.pendingMgr.HandleUserConfirmedType(ctx, evt)
if err == nil {
    return nil // ACK
}
// non-nil ⇒ *model.DomainError (Manager always returns DomainError per
// manager.go:336-339,346-349,368-371,398-401).
if model.IsRetryable(err) {
    return err // NACK→retry-DLX
}
// non-retryable — Manager has ALREADY published DLQ (R4 — manager.go:
// 317,384,600-616). Router MUST NOT re-publish DLQ. ACK so the message
// does not loop (the §11.2 "ACK poison message" semantics — manager.go:
// 386).
//
// Manager's non-retryable cases (manager.go):
//   - publishInvalidDLQ(INVALID_CONTRACT_TYPE) on regex/whitelist
//     reject; returns nil (already ACK-path inside Manager — line 321).
//     **NOT reached here: Manager returns nil for this case.**
//   - tenant mismatch: publishInvalidDLQ(INVALID_ORG_ID_MISMATCH);
//     returns nil (line 386). **NOT reached here.**
//   - nil ClassificationResult post-restore: returns *DomainError{
//     INTERNAL_ERROR, retryable=false} WITHOUT DLQ publish (line 399).
//     **THIS reaches us as non-retryable err.** Router publishes DLQ?
//     **NO** — Manager intentionally did not (defensive non-recoverable
//     state on a stored blob, not a poison wire message). Best
//     behaviour: ACK (cannot retry; corrupt blob would re-fail; no
//     poison wire-message DLQ needed). R4 — recorded.
//   - USER_CONFIRMATION_EXPIRED (line 361): Manager returns nil after
//     publishing FAILED. **NOT reached here.**
//   - any pipeline-side resume failure: returns runErr verbatim (line
//     426) — Manager does NOT DLQ on resume failure. retryable per
//     model.IsRetryable. **If retryable, falls into the if-branch
//     above; if non-retryable, falls here.** Router ACKs (NO DLQ,
//     pipeline owns its terminal status publish via 036 publishFailed).
//
// Conclusion: every non-retryable err that reaches Router has ALREADY
// been (a) DLQ-published by Manager or (b) pipeline-FAILED-published
// by 036's publishFailed. Router ACKs all of them.
return nil
```

This is **D8 binding**: Router's `RouteUserConfirmedType` maps
`{nil ⇒ ACK, retryable err ⇒ NACK, non-retryable err ⇒ ACK}` (NOT the
naive `non-retryable ⇒ DLQ`). The DLQ ownership for this topic is split
between Manager (poison wire messages — invalid-format / tenant-
mismatch) and pipeline (terminal FAILED via publishFailed) — Router
trusts both and only handles the broker boundary. R4.

### D9 — Locally-declared seams (NOT imports)

The Router imports `internal/application/pipeline` ONLY for the
identity-comparable sentinel `pipeline.ErrPipelinePaused` +
`pipeline.IsPaused(err)` predicate (D10). Every other collaborator
crosses behind a **router-local seam**:

```go
// PipelineRunner is the seam to LIC-TASK-036's
// *pipeline.Orchestrator.Run. The orchestrator structurally satisfies
// it (Run(ctx, port.VersionProcessingArtifactsReady) error;
// orchestrator.go:253-437). Declared router-side so the router is
// hermetic and unit-testable with an in-package fake; the
// var _ PipelineRunner = (*pipeline.Orchestrator)(nil) assertion lives
// in the LIC-TASK-047 wiring package, NOT here (the
// pendingconfirmation.PipelineResumer / consumer.EventRouter / pipeline
// .JobLimiter precedent — D10). NO noop: a Router with no pipeline
// runner cannot dispatch — NewRouter fails fast (D2).
type PipelineRunner interface {
    Run(ctx context.Context, trigger port.VersionProcessingArtifactsReady) error
}

// PendingConfirmationManager is the seam to LIC-TASK-037's
// *pendingconfirmation.Manager. The Manager structurally satisfies it
// (HandleUserConfirmedType + RepublishPauseEvents; manager.go:307-449
// + :277-296). Declared router-side so the router does NOT import
// internal/application/pendingconfirmation (the 037 hermetic-allowlist
// precedent — pendingconfirmation does NOT import internal/application/
// pipeline either; the two halves of the pause/resume state-machine
// live behind seams across the wiring boundary). NO noop.
type PendingConfirmationManager interface {
    HandleUserConfirmedType(ctx context.Context, cmd port.UserConfirmedType) error
    RepublishPauseEvents(ctx context.Context, ptc *model.PendingTypeConfirmation) error
}

// ArtifactsAwaiterDeliverer is the inbound-side companion to
// port.ArtifactsAwaiterPort (dm.go:80-96 — the domain port only has
// Register/Await/Cancel for the orchestrator side; Deliver is the
// router-side ingress API LIC-TASK-041 dmawaiter exports as a separate
// public method on its concrete type). Declared router-side so 041 has
// freedom in its API shape; LIC-TASK-047 wires its concrete
// *dmawaiter.ArtifactsAwaiter as the seam impl. NO noop.
type ArtifactsAwaiterDeliverer interface {
    Deliver(correlationID string, evt port.ArtifactsProvided) error
}

// PersistConfirmationDeliverer is the inbound-side companion to
// port.PersistConfirmationAwaiterPort (dm.go:106-120). It takes the
// fully-built port.PersistConfirmation envelope (NewPersistConfirmation
// Success/Failure — dm.go:136-152). NO noop.
type PersistConfirmationDeliverer interface {
    Deliver(jobID string, conf port.PersistConfirmation) error
}

// VersionMetaCacheWriter writes lic-version-meta:{version_id} —
// the Redis-backed cache the orchestrator reads via the
// pipeline.VersionMetaCache seam at resolveParentAndMode
// (orchestrator.go:765-777). LIC-TASK-047 wires it over kvstore.Client.
// payload is opaque bytes (the Router marshals JSON; the cache adapter
// stores as-is). NO noop.
type VersionMetaCacheWriter interface {
    Set(ctx context.Context, versionID string, payload []byte, ttl time.Duration) error
}

// IdempotencyGuard unifies the 5 frozen port.IdempotencyStorePort
// methods (idempotency.go:42-74) with the additive CheckAndAcquire +
// StartHeartbeat (BUILD_SPEC_LIC_038 D3.2 + D6). *idempotency.Guard
// structurally satisfies all 7 methods (guard.go:160-285 +
// heartbeat.go:48-84). Declared router-side because the frozen
// port.IdempotencyStorePort is missing the two additive methods (and
// must stay frozen — pendingconfirmation already depends on the 5-
// method surface). LIC-TASK-047 wires the SAME *Guard instance into
// both Router (as IdempotencyGuard) and pendingconfirmation.Manager
// (as port.IdempotencyStorePort) — one Guard, two roles. NO noop.
type IdempotencyGuard interface {
    SetNX(ctx context.Context, key string, ttl time.Duration) (port.IdempotencyStatus, error)
    Get(ctx context.Context, key string) (port.IdempotencyStatus, error)
    ExtendTTL(ctx context.Context, key string, ttl time.Duration) error
    SetCompleted(ctx context.Context, key string, ttl time.Duration) error
    SetPaused(ctx context.Context, key string, ttl time.Duration) error
    CheckAndAcquire(ctx context.Context, key string, ttl time.Duration) (port.IdempotencyStatus, bool, error)
    StartHeartbeat(ctx context.Context, key string, ttl time.Duration) (stop func())
}
```

Plus the 4 optional `Deps` seams (Metrics/Clock/Logger/Tracer — D11).

### D10 — Imports / hermeticity (binding allowlist)

Non-test files MAY import:

- stdlib: `context`, `errors`, `encoding/json`, `fmt`, `time`
- `contractpro/legal-intelligence-core/internal/domain/model`
- `contractpro/legal-intelligence-core/internal/domain/port`
- `contractpro/legal-intelligence-core/internal/application/pipeline`
  — **for `ErrPipelinePaused` + `IsPaused` ONLY** (identity-comparable
  sentinel; same pattern the orchestrator's `Config.PausedSentinel`
  uses to communicate paused-ness to `pendingconfirmation` without a
  circular import — but in the opposite direction: pipeline⇐router
  rather than pipeline⇒pendingconfirmation. The pipeline package is
  the topologically lowest application/* package; importing it is
  safe because pipeline does NOT import router, manager, or consumer
  (orchestrator.go:46-57 import block).

Forbidden (active-fail in `TestHermeticImports`):

- `internal/application/pendingconfirmation` (inverted via
  `PendingConfirmationManager` seam)
- `internal/ingress/consumer` (inverted via `EventRouter` seam at the
  consumer side; the wiring is 047's job)
- `internal/ingress/idempotency` (inverted via `IdempotencyGuard` seam;
  the wiring is 047's job, NOT here — the
  `var _ IdempotencyGuard = (*idempotency.Guard)(nil)` assertion lives
  in 047)
- `internal/infra/*` (broker, kvstore, ocr, objectstorage,
  observability)
- `internal/config`
- `internal/egress/*` (DLQ — Router does NOT publish DLQ — R4)
- `github.com/rabbitmq/amqp091-go`, `github.com/redis/go-redis/...`,
  `github.com/prometheus/...`, `go.opentelemetry.io/...`,
  `golang.org/x/sync/...`

The `TestHermeticImports` allowlist is 3 entries
(`domain/model`, `domain/port`, `application/pipeline`) — the same
3-entry / 2-entry shape as idempotency (D10) / pendingconfirmation
(D17) / consumer (D16). Stdlib is allowed by omission.

### D11 — Telemetry seams (Metrics / Clock / Logger / Tracer)

```go
// Metrics — local seam. The Router DOES NOT emit
// lic_consumer_messages_total{topic,outcome} (Consumer owns that —
// 039 CLAUDE.md D11), DOES NOT emit lic_idempotency_lookups_total
// (Guard owns it — 038 D8). The Router has its own decision counter
// IFF reviewers want extra observability; per D12 we INTENTIONALLY do
// NOT introduce one (R5). The Metrics seam thus has ONE method (kept
// for forward-compatibility; the noop currently observes nothing).
type Metrics interface {
    // Decision is RESERVED for forward use (see R5). The v1 noop +
    // production wiring both call this with topic="" / decision="" so
    // a 047-added counter is structurally ready, but the call is
    // currently a no-op even on the wired adapter. Implementer MAY
    // emit nothing; the seam shape is committed.
    Decision(topic, decision string)
}
type noopMetrics struct{}
func (noopMetrics) Decision(string, string) {}
var _ Metrics = noopMetrics{}

// Clock — 1-method (the consumer/pendingconfirmation precedent). The
// Router uses Now() for log timestamps (kv "decided_at") if any. The
// LIC-TASK-044 status publisher owns its own timestamping; the Router
// does NOT timestamp LICStatusChangedEvent — the publisher does.
//
// Optional: the Router may not need Now() at all (the v1 implementation
// uses no clock — see D4 publishFailedTerminal which delegates to the
// statusPub). Keep the seam declared for parity with consumer.Clock /
// pendingconfirmation.Clock + a forward use slot, but the noop is the
// only impl exercised by tests.
type Clock interface { Now() time.Time }
type systemClock struct{}
func (systemClock) Now() time.Time { return time.Now().UTC() }
var _ Clock = systemClock{}

// Logger — Info/Warn/Error (NO WithRequestContext — Consumer
// already attached it at the ingress boundary, D13).
type Logger interface {
    Info(ctx context.Context, msg string, kv ...any)
    Warn(ctx context.Context, msg string, kv ...any)
    Error(ctx context.Context, msg string, kv ...any)
}
type noopLogger struct{}
func (noopLogger) Info(context.Context, string, ...any)  {}
func (noopLogger) Warn(context.Context, string, ...any)  {}
func (noopLogger) Error(context.Context, string, ...any) {}
var _ Logger = noopLogger{}

// Tracer — per-route span. The pipeline owns the root pipeline span
// (orchestrator.go:304-309 — StartPipeline). The Router opens a
// per-route ingress span ONLY if Tracer.StartRoute is wired (NOT
// the noop). v1: noop default — no tracing surface. The seam is
// committed for 047 to bridge to the OTEL tracer.
type Tracer interface {
    StartRoute(ctx context.Context, topic string) (context.Context, RouteSpan)
}
type RouteSpan interface { Finish(err error) }
type noopTracer struct{}
type noopRouteSpan struct{}
func (noopRouteSpan) Finish(error) {}
func (noopTracer) StartRoute(ctx context.Context, _ string) (context.Context, RouteSpan) {
    return ctx, noopRouteSpan{}
}
var _ Tracer = noopTracer{}
var _ RouteSpan = noopRouteSpan{}
```

The `Deps` bundle:

```go
type Deps struct {
    Metrics Metrics
    Clock   Clock
    Logger  Logger
    Tracer  Tracer
}
func (d Deps) withDefaults() Deps {
    if d.Metrics == nil { d.Metrics = noopMetrics{} }
    if d.Clock   == nil { d.Clock   = systemClock{} }
    if d.Logger  == nil { d.Logger  = noopLogger{} }
    if d.Tracer  == nil { d.Tracer  = noopTracer{} }
    return d
}
```

### D12 — No new metric counters (R5 binding)

The brief presents two alternatives:

- (A) Introduce `lic_router_decisions_total{topic,decision}` —
  decision ∈ {acquired_run, paused_republish, paused_expired,
  completed, in_progress_nack, transport_nack}.
- (B) Reuse the existing `lic_idempotency_lookups_total{result}`
  (Guard-emitted: new / in_progress / completed / fallback_db —
  038 D8) and emit no new counter.

**Binding decision: (B).** Rationale: every meaningful Router decision
maps 1:1 onto a Guard lookup result (acquired_run ⇔ result=new;
in_progress_nack ⇔ result=in_progress[PROCESSING]; paused_*
⇔ result=in_progress[PAUSED]; completed ⇔ result=completed;
transport_nack ⇔ result=fallback_db|absent-error). Adding a Router
counter would near-perfectly correlate with the existing one — wasted
cardinality. The post-Guard split (PAUSED→republish vs
PAUSED→USER_CONFIRMATION_EXPIRED) is exposed via the existing
`lic_pipeline_outcome_total{outcome="failed",code="USER_CONFIRMATION_-
EXPIRED"}` series (the Router publishes FAILED via statusPub which is
upstream of the orchestrator's outcome counter, but the
USER_CONFIRMATION_EXPIRED line is the SSOT for that outcome — the
Router-published one is dedup'd by `lic-status:{job_id}:{status}` at
the orchestrator subscriber).

R5 — recorded as a deliberate non-introduction. Forward-note for any
future "observability completeness" pass: prefer (A) only if Guard
result enum is widened with a fifth value the Router cannot derive
from lookup alone.

### D13 — RequestContext propagation (Consumer-attached, Router-transparent)

Consumer (039 D6/R4) calls `Logger.WithRequestContext(ctx, ids)` ONCE
per delivery, immediately after successful validation, BEFORE invoking
`Route*` (consumer/seams.go:144-156 — "called once at ingress"). The
Router receives a ctx that already carries the per-delivery correlation
IDs.

The Router does NOT call `WithRequestContext` again (would double-
attach; the noop logger returns ctx unchanged but the 047 logger
adapter MIGHT enforce single-attach). The Router uses the ctx verbatim
when calling collaborators (`pipelineRunner.Run(ctx, evt)`,
`pendingMgr.HandleUserConfirmedType(ctx, evt)`, `idem.CheckAndAcquire(ctx, key, ttl)`, etc.) — every downstream log line inherits the
attached IDs.

### D14 — Key-prefix constants + helper funcs

PINNED package-level constants (lowercase exported only if a test
peeks; recommend unexported — the keys are an implementation detail
and may be inspected via the in-package test fakes that observe what
the Guard sees):

```go
const (
    keyPrefixTrigger        = "lic-trigger:"          // 4-status: heartbeat-extended (idempotency D6 / Guard.StartHeartbeat)
    keyPrefixVersionCreated = "lic-version-created:"  // 2-status: fire-once cache populator (D5)
    keyPrefixArtifactsResp  = "lic-artifacts-resp:"   // 2-status: per-correlation_id (D6)
    keyPrefixPersistResp    = "lic-persist-resp:"     // 2-status: per-job_id (D7 success topic)
    keyPrefixPersistFail    = "lic-persist-fail:"     // 2-status: per-job_id (D7 failure topic)
)

func keyTrigger(versionID string) string         { return keyPrefixTrigger + versionID }
func keyVersionCreated(versionID string) string  { return keyPrefixVersionCreated + versionID }
func keyArtifactsResp(corrID string) string      { return keyPrefixArtifactsResp + corrID }
func keyPersistResp(jobID string) string         { return keyPrefixPersistResp + jobID }
func keyPersistFail(jobID string) string         { return keyPrefixPersistFail + jobID }
```

These are KEYS, not topics — the topic→key mapping is fixed by §6.3
(high-architecture.md). The Router uses them ONLY through the helpers;
direct string concatenation in route bodies is forbidden (tests pin
key shapes via `TestKeyHelpers_DeterministicAndCollisionFree`).

---

## PART B — RECONCILIATIONS (R1..R7) — DEFECT-style

### R1 — DEFECT: x-death retry-level escalation is undetectable from the Router

**Doc (tasks.json LIC-TASK-040 acceptance):** "Transient error → DLX-loop
NACK (read x-death count, route to retry.1/2/3 или DLQ при >3)."

**Doc (integration-contracts.md §6.4 + broker `CLAUDE.md` "§6.4
deviation"):** main queues set `x-dead-letter-exchange` only, **no
static `x-dead-letter-routing-key`**; the consumer adapter is supposed
to read `XDeath()` and republish to the DLX with the computed routing
key (retry.1/retry.2/retry.3/`lic.dlq.consumer-failed`).

**Impl (this build-spec):** the consumer.EventRouter seam DELIBERATELY
does NOT carry `broker.Delivery` — 039 D8 YAGNI ("no raw Delivery/x-
death in the seam"; consumer/seams.go:71-77). The Router therefore has
NO way to read `XDeath()`. A plain `Nack(false)` from the consumer
(when Router returns non-nil error) cycles to the DLX with NO routing
key — and because the main queue has no static routing key either,
RabbitMQ enforces the level via the **`x-message-ttl` per retry-queue**
+ the consumer's per-attempt re-publish (NOT exercised by this Router).

**Why this is the correct in-scope v1 behaviour:**

1. The 039 frozen `EventRouter` seam (consumer/seams.go:71-77) is
   binding — 039's hermetic acceptance + tests + DLQ-envelope
   constructions pivot on the typed-DTO-only shape. Widening the seam
   now to carry `broker.Delivery` would (a) break 039 + invalidate its
   reviewer-gate, (b) leak `broker.Delivery` into the Router's
   forbidden imports (D10 — `internal/infra/broker` is forbidden), and
   (c) move x-death decoding to a layer that has no business with
   AMQP topology.
2. The §6.4 escalation flow lives ENTIRELY at the broker topology level
   for v1: main queue → DLX (no key) → fans to ALL retry queues that
   bind by NO key (catch-all) → individual retry queues have
   `x-message-ttl` 2s/10s/60s + their own `x-dead-letter-exchange`
   back to the main exchange with a routing key that re-delivers. A
   Router-side x-death increment IS a useful refinement (it caps the
   loop count), but its absence does NOT corrupt the §6.4 contract;
   the main queue's `x-message-ttl` (set in topology.go) caps redelivery
   eventually.
3. The tasks.json acceptance line is non-exhaustive (R7 precedent —
   037/038/039); frozen architecture documents (broker §6.4 deviation,
   consumer 039 seam) win.

**Resolution (binding):** v1 Router does NOT decode x-death. A
non-retryable failure path that needs DLQ goes through Manager (D8 /
R4) or pipeline-statusPub (D4 PAUSED-miss / D4 step 3c via pipeline's
`publishFailed`). A retryable failure returns a `*model.DomainError`
that Consumer maps to `Nack(false)` → DLX → retry-queue cycle.

**Forward-note (recorded):** if a future revision widens the 039 seam
to carry `broker.Delivery`, the Router adds a 7th decision branch
(`x-death.count >= retryBudget` ⇒ explicit `dlqPub.PublishDLQ(
DLQTopicConsumerFailed, env)` + ACK). The seam widening is OUT OF
SCOPE for 040; track as a 039-revisit item.

### R2 — DEFECT: tasks.json "Concurrency control" already satisfied by 036

**Doc (tasks.json LIC-TASK-040 acceptance):** "Concurrency control:
acquire semaphore (LIC_PIPELINE_CONCURRENCY) для pipeline-triggering
messages (VersionProcessingArtifactsReady)."

**Impl (this build-spec):** `pipeline.Orchestrator.Run` itself calls
`o.limiter.Acquire(ctx)` as step 1 (orchestrator.go:273-295 — Acquire
BEFORE WithTimeout, the binding 036 rule). Router pre-Acquiring the
semaphore would **double-count** the slot — the gauge
`lic_pipeline_concurrent_jobs` (Semaphore-owned per 036 DEFECT-2;
pipeline/CLAUDE.md DEFECT-2) would inflate by 2× and capacity halve.

**Resolution (binding):** Router does NOT touch the `JobLimiter` seam;
the Router has no `JobLimiter` field on the struct, no positional
`NewRouter` param of that shape, and no test that exercises Acquire/
Release. The acceptance line is satisfied at the **service level** by
036's own Acquire — exactly the 036 DEFECT-4 split-ownership pattern
("036 owns the OUTCOME; 040 owns the broker ACK/NACK").

**Test pin (negative gate):** `TestRouter_NoJobLimiterField` —
reflection-based assertion that the `*Router` struct has no field of
seam type `JobLimiter` (and the `seams.go` does not declare one).
Belt-and-braces.

### R3 — DEFECT: PENDING_STATE_LOST is not a catalog code

**Doc (high-architecture.md §6.5:631):** "PENDING_STATE_LOST" is named
as the error_code for the lic-trigger=PAUSED + lic-pending-state-miss
flow.

**Impl (catalog):** `model.ErrorCode` enum (error_codes.go:16-59) has
NO `PENDING_STATE_LOST` row. The semantically identical row is
`ErrCodeUserConfirmationExpired` (`USER_CONFIRMATION_EXPIRED`,
error_codes.go:52,209-213): retryable=false, publishable
(UserMessage="Время на подтверждение типа договора истекло. Запустите
проверку заново."), devMessage="pending classification confirmation
TTL expired in Redis".

**Why USER_CONFIRMATION_EXPIRED is correct:** 037 already binds it for
the symmetric §6.10:777 case (pending-state miss inside
`HandleUserConfirmedType`) — manager.go:355-364 + 037 R2. The
"the pause state is gone" semantics are identical regardless of which
topic redelivered (UserConfirmedType vs VersionProcessingArtifactsReady);
inventing a second code (or a wire-level `PENDING_STATE_LOST`) is a
frozen-catalog change owned by the architecture team (the 036/037
"never inline RU messages; frozen catalog SSOT" discipline).

**Resolution (binding):** Router publishes
`FAILED{model.ErrCodeUserConfirmationExpired, StageAwaitingUserConfirmation,
retryable=false}` via `port.StatusPublisherPort.PublishStatus` and then
`Guard.SetCompleted(lic-trigger, CompletedTTL)` (§6.10:782); returns
nil (ACK). R3 binds 040 to the SAME code as 037 R2.

### R4 — DEFECT: UCT non-retryable ⇒ Router ACKs (Manager owns the DLQ)

**Doc (tasks.json LIC-TASK-040 acceptance):** "Поведение для unknown
events: route to `lic.dlq.invalid-message`."

**Doc (037 manager.go:317,384,600-616):** Manager publishes DLQ for
INVALID_CONTRACT_TYPE (line 317) and INVALID_ORG_ID_MISMATCH (line
384) and returns nil (ACK at the Manager-level for these two).

**Impl (this build-spec):** the Router's mapping for the
`RouteUserConfirmedType` topic is `{nil ⇒ ACK, retryable err ⇒ NACK,
non-retryable err ⇒ ACK}` (D8). The non-retryable path does NOT
auto-DLQ — Manager has either already DLQ-published (the poison-wire
cases) or deliberately not (the corrupt-stored-blob case at
manager.go:399-401, where DLQ-publishing a wire message because a
stored Redis blob is corrupt would be incorrect).

**Why this is correct (not a naive bug):**

- "Unknown event" in the tasks.json acceptance refers to a message
  that fails consumer-side envelope validation (039 D9 — Invalid Path:
  consumer builds a DLQ envelope, publishes to
  `lic.dlq.invalid-message`, ACKs — `BUILD_SPEC_LIC_039.md` D11/D13).
  Consumer (NOT Router) owns this code-path.
- The Router never sees an "unknown event" — consumer dispatches by
  typed Route* only after successful decode+validate.
- The remaining non-retryable cases all have a CORRECT owner already
  (Manager for poison wire messages; pipeline `publishFailed` for
  terminal pipeline failures published as FAILED{...}, NOT DLQ — 036
  D5 / `publishFailed` is the SINGLE FAILED-publish site).

**Resolution (binding):** Router does NOT take `port.DLQPublisherPort`
as a constructor dep. R4 codifies the split-ownership of DLQ
publishing in LIC:

| DLQ origin                                              | Owner    |
|---------------------------------------------------------|----------|
| Invalid-schema/uuid wire message                        | Consumer (039 D13) |
| Invalid contract_type (regex/whitelist) wire message    | Manager (037 manager.go:317) |
| Cross-tenant UserConfirmedType (forged org_id)          | Manager (037 manager.go:384) |
| Publish-failed agent-output-invalid                     | 046 (out-of-scope) |
| Consumer-failed (x-death budget exceeded)               | 046+broker (out-of-scope; R1 forward-note) |
| Pipeline terminal FAILED (publishable codes)            | NOT DLQ — published as FAILED status by 036 `publishFailed` |
| Pipeline terminal FAILED (non-publishable codes)        | logged-only by 036 (orchestrator.go:1143-1146); a real DLQ publish is 046's job |

Router publishes NEITHER FAILED on pipeline returns (036 already did)
NOR DLQ. Router's ONLY direct publish is the §6.5:631
USER_CONFIRMATION_EXPIRED FAILED — via statusPub, NOT dlqPub.

### R5 — Recordable design choice: no new Router-specific metric counter

See D12. Choice (B) — reuse existing
`lic_idempotency_lookups_total{result}` (Guard-emitted). R5 is recorded
as a deliberate non-introduction; the Metrics seam is shaped to allow a
future (A) wire-up without an API break.

### R6 — W3C trace propagation: Router does NOT extract trace headers

**Doc (high-architecture.md §6.10:754; observability.md §4.4):** the
`state.TraceContext` carries the saved W3C `traceparent`/`tracestate`
for resume span linkage (pendingconfirmation D13 / R3); LIC-TASK-040
"owns populating it at ingress" (pipeline/CLAUDE.md "036 forward-note
#2").

**Impl (this build-spec):** the Router CANNOT read AMQP headers — the
consumer.EventRouter seam carries only the typed event DTO, not
`broker.Delivery` (R1). The typed DTOs in `port/events.go` do NOT
carry `TraceContext` either (events.go:42-137 — none of the 6 inbound
DTOs have a trace_context field). Hence `PipelineState.TraceContext`
remains zero-valued in v1 (`pipeline.Orchestrator.newState` already
sets it to zero, orchestrator.go:757).

**Why this is acceptable degradation:**

- The 037 R3 reconciliation explicitly sanctions a zero TraceContext:
  "A zero context is persisted as-is; the TraceRestorer noop/adapter
  treats `IsZero()` as 'no linkage'." The functional pipeline still
  runs; only cross-pause trace linkage is degraded (telemetry, not
  correctness).
- The §6.4 / consumer 039 trace propagation owns AMQP-header
  extraction at the consumer boundary — but the 039 spec deliberately
  does NOT pass extracted headers downstream beyond
  `Logger.WithRequestContext` (which carries correlation IDs, NOT
  W3C trace state — consumer/seams.go:123-130).

**Resolution (binding):** v1 Router does NOT touch `TraceContext`. The
pipeline's `state.TraceContext = model.TraceContext{}` (orchestrator.go:
757) stays.

**Forward-note (recorded):** when 039's seam is widened (R1) OR when
the inbound DTOs are extended with `trace_context`, the Router (or
039) plumbs the saved W3C context through. Until then: documented
gap.

### R7 — Task acceptance is non-exhaustive — frozen contracts win

(037 R5 / 038 R2 / 039 R5 precedent.) The `tasks.json LIC-TASK-040`
acceptance enumerates 6-7 bullet checklist items; the frozen
contracts (consumer.EventRouter seam, pipeline.Run return contract,
Manager API, idempotency.Guard API, port.IdempotencyStorePort,
port.PendingStatePort, port.StatusPublisherPort, integration-contracts
§6.4, high-architecture §6.3/§6.5/§6.10) are the binding SSOT. Every
R1..R6 above adjudicates one such tension; R7 is the meta-rule that
codifies the adjudication policy.

---

## PART C — TEST PINS (PART C #1..#27)

Every pin is a single `TestXxx_Yyy_Zzz` (Go test naming convention)
with a 1-2 line assertion description. Each is a SEPARATE test
function (no `t.Run` sub-tests except where noted). All tests are
`-race` clean; ZERO `time.Sleep`; deterministic via injected Clock +
faithful in-package fakes for every seam.

### Hermeticity / package-shape pins

1. **`TestHermeticImports`** — golang `go/build` parses every `.go`
   file in the package (non-test); asserts the import set is EXACTLY
   subset of `{stdlib, internal/domain/model, internal/domain/port,
   internal/application/pipeline}`; active-fail forbidden set includes
   `internal/application/pendingconfirmation`, `internal/ingress/
   consumer`, `internal/ingress/idempotency`, `internal/infra/*`,
   `internal/config`, `internal/egress/*`, `github.com/rabbitmq/...`,
   `github.com/redis/...`, `github.com/prometheus/...`,
   `go.opentelemetry.io/...`, `golang.org/x/sync/...`. (D10)
2. **`TestGofmtClean`** — `go/format.Source` re-formats every `.go`
   file and asserts the result is byte-identical (sandbox blocks
   `go fmt`).
3. **`TestNewRouter_FailFast`** — table-driven: for each of the 8
   required collaborators independently set to nil + each of the 5
   invalid `Config` field cases, asserts `NewRouter` returns
   `(nil, err)` where `errors.Unwrap` chain includes the matching
   per-arg message and `errors.Is(err, errors.Join(...))` collects all
   simultaneously when multiple are bad. (D2)
4. **`TestKeyHelpers_DeterministicAndCollisionFree`** — for a fixed
   id "v-123", asserts `keyTrigger("v-123") == "lic-trigger:v-123"`,
   etc.; asserts no two helpers produce equal output for non-equal
   inputs in a small property test (random pair generation, 50
   iterations). (D14)

### `RouteVersionArtifactsReady` pins

5. **`TestRouteVAR_HappyPath_AcquireRunSetCompleted`** — Guard
   returns `(Absent, false, nil)`; fake PipelineRunner.Run returns
   nil; asserts (a) heartbeat started + stopped exactly once, (b)
   PipelineRunner.Run called with the same evt, (c)
   Guard.SetCompleted(keyTrigger, CompletedTTL) called exactly once,
   (d) Route returns nil. (D4 step 3, happy path)
6. **`TestRouteVAR_HeartbeatStartsAndStops`** — fake `IdempotencyGuard`
   instruments StartHeartbeat to return a stop func whose call count is
   observable; PipelineRunner.Run synchronously returns nil; asserts
   stop called exactly once (the `defer stopHB()` at D4 step 3a). Race-
   asserts via `-race` that concurrent Route calls do not reorder
   start/stop. (D4 step 3a)
7. **`TestRouteVAR_RunReturnsPausedSentinel_AckNoSetCompleted`** —
   fake PipelineRunner.Run returns `pipeline.ErrPipelinePaused`;
   asserts (a) Route returns nil (ACK), (b)
   Guard.SetCompleted NOT called (037 already SetPaused), (c)
   statusPub NOT called by Router. (D4 step 3c)
8. **`TestRouteVAR_RunReturnsRetryableDomainError_AckSetCompleted`** —
   fake PipelineRunner.Run returns
   `model.NewDomainError(ErrCodeInternal, StageReceived).WithRetryable(true)`;
   asserts (a) Route returns that exact error verbatim, (b)
   Guard.SetCompleted called (the terminal-status-published path
   finalizes the slot regardless of retryability), (c) statusPub
   NOT called by Router (036 publishFailed already did). (D4 step 3c
   `runErr != nil`)
9. **`TestRouteVAR_RunReturnsNonRetryableDomainError_AckSetCompleted`**
   — fake PipelineRunner.Run returns
   `model.NewDomainError(ErrCodeDocumentTooLarge, StageArtifactsReceived).WithRetryable(false)`;
   asserts (a) Route returns that exact error verbatim, (b)
   Guard.SetCompleted called, (c) statusPub NOT called. (D4 step 3c)
10. **`TestRouteVAR_GuardReturnsProcessing_NackForRetry`** — Guard
    returns `(IdempotencyProcessing, true, nil)`; asserts (a) Route
    returns a non-nil `*model.DomainError`, (b) the error's Code is
    `ErrCodeIdempotencyStoreUnavail` (non-publishable to keep the
    in-flight run's terminal status authoritative), (c)
    `model.IsRetryable(err) == true`, (d) Guard.SetCompleted NOT
    called, (e) PipelineRunner.Run NOT called, (f) Guard.StartHeartbeat
    NOT called. (D4 step 4a)
11. **`TestRouteVAR_GuardReturnsPaused_PendingHit_RepublishAck`** —
    Guard returns `(IdempotencyPaused, true, nil)`; fake
    PendingStatePort.Load returns a non-nil `*PendingTypeConfirmation`;
    fake PendingConfirmationManager.RepublishPauseEvents returns nil;
    asserts (a) Route returns nil (ACK), (b) RepublishPauseEvents
    called exactly once with the loaded ptc, (c) statusPub NOT called,
    (d) Guard.SetCompleted NOT called, (e) PipelineRunner.Run NOT
    called. (D4 step 4b — hit branch)
12. **`TestRouteVAR_GuardReturnsPaused_PendingMiss_PublishFailedSetCompletedAck`**
    — Guard returns `(IdempotencyPaused, true, nil)`; fake
    PendingStatePort.Load returns `port.ErrPendingStateNotFound`;
    asserts (a) Route returns nil (ACK), (b) statusPub.PublishStatus
    called exactly once with an LICStatusChangedEvent carrying
    `Status=FAILED, Stage=STAGE_AWAITING_USER_CONFIRMATION,
    ErrorCode=USER_CONFIRMATION_EXPIRED, *IsRetryable==false`, (c)
    Guard.SetCompleted(keyTrigger, PendingStateTTL) called exactly
    once, (d) RepublishPauseEvents NOT called, (e) PipelineRunner.Run
    NOT called. (D4 step 4b — miss branch; R3)
13. **`TestRouteVAR_GuardReturnsPaused_PendingLoadError_Nack`** —
    Guard returns `(IdempotencyPaused, true, nil)`; fake
    PendingStatePort.Load returns
    `errors.New("redis transient: dial timeout")`; asserts (a) Route
    returns a non-nil `*model.DomainError`, (b) Code is
    `ErrCodeIdempotencyStoreUnavail`, (c) `model.IsRetryable==true`,
    (d) statusPub NOT called, (e) Guard.SetCompleted NOT called.
    (D4 step 4b — Load transient)
14. **`TestRouteVAR_GuardReturnsCompleted_Ack`** — Guard returns
    `(IdempotencyCompleted, true, nil)`; asserts (a) Route returns
    nil, (b) no other collaborator invoked. (D4 step 4c)
15. **`TestRouteVAR_GuardTransportError_FallbackDisabled_Nack`** —
    Guard returns `(Absent, false, errors.New("redis: connection
    refused"))`; asserts (a) Route returns a non-nil
    `*model.DomainError`, (b) Code is
    `ErrCodeIdempotencyStoreUnavail`, (c) IsRetryable==true, (d)
    Guard.StartHeartbeat NOT called, (e) PipelineRunner.Run NOT
    called. (D4 step 2)

### `RouteVersionCreated` pins

16. **`TestRouteVC_HappyPath_WriteMetaSetCompleted`** — Guard returns
    `(Absent, false, nil)`; asserts (a) VersionMetaCacheWriter.Set
    called exactly once with `versionID=evt.VersionID`, `ttl=
    cfg.MetaCacheTTL`, `payload` containing the JSON-encoded
    parent_version_id + origin_type, (b) Guard.SetCompleted(
    keyVersionCreated, CompletedTTL) called exactly once, (c) Route
    returns nil. (D5)
17. **`TestRouteVC_DuplicateCompleted_Ack`** — Guard returns
    `(IdempotencyCompleted, true, nil)`; asserts (a) Route returns
    nil, (b) VersionMetaCacheWriter.Set NOT called, (c)
    Guard.SetCompleted NOT called. (D5 duplicate branch)
18. **`TestRouteVC_MetaWriteFails_AckWithWarn`** — Guard returns
    `(Absent, false, nil)`; fake VersionMetaCacheWriter.Set returns
    `errors.New("redis: timeout")`; asserts (a) Route returns nil
    (ACK), (b) Guard.SetCompleted NOT called (intentional — leave
    PROCESSING for redelivery retry; D5), (c) Logger.Warn invoked
    exactly once with `cause` containing "redis: timeout". (D5
    degrade)
19. **`TestRouteVC_GuardTransportError_AckWithWarn`** — Guard returns
    `(Absent, false, errors.New("redis: down"))`; asserts (a) Route
    returns nil (degrade per 036 DEFECT-1), (b)
    VersionMetaCacheWriter.Set NOT called, (c) Logger.Warn invoked.
    (D5 transport)

### `RouteArtifactsProvided` pins

20. **`TestRouteAP_DeliversAndSetCompleted`** — Guard returns
    `(Absent, false, nil)`; fake ArtifactsAwaiterDeliverer.Deliver
    returns nil; asserts (a) Deliver called exactly once with
    `correlationID=evt.CorrelationID` + evt verbatim, (b)
    Guard.SetCompleted(keyArtifactsResp, CompletedTTL) called, (c)
    Route returns nil. (D6 happy)
21. **`TestRouteAP_AwaiterRegistryMiss_AckSilently`** — Guard returns
    `(Absent, false, nil)`; Deliver returns `errors.New("awaiter:
    no slot")`; asserts (a) Route returns nil (ACK), (b) Logger.Warn
    invoked, (c) Guard.SetCompleted NOT called. (D6 registry-miss)
22. **`TestRouteAP_DuplicateCompleted_Ack`** — Guard returns
    `(IdempotencyCompleted, true, nil)`; asserts (a) Route returns
    nil, (b) Deliver NOT called. (D6 duplicate)

### `RoutePersisted` / `RoutePersistFailed` pins

23. **`TestRoutePersisted_DeliversSuccessConfirmation`** — fake
    PersistConfirmationDeliverer.Deliver returns nil; asserts (a)
    Deliver called exactly once with `jobID=evt.JobID` + a
    PersistConfirmation envelope where `IsSuccess()==true` and
    `Success != nil && Success.JobID == evt.JobID`, (b)
    Guard.SetCompleted(keyPersistResp, CompletedTTL) called, (c)
    Route returns nil. (D7)
24. **`TestRoutePersistFailed_DeliversFailureConfirmation`** —
    similar to #23 but for the failure path; asserts envelope
    `IsFailure()==true` and `Failure.IsRetryable` is whatever was
    on the evt. (D7)

### `RouteUserConfirmedType` pins (D8 / R4)

25. **`TestRouteUCT_ManagerNil_Ack`** — Manager returns nil; asserts
    Route returns nil; Guard NOT touched (Router does not guard UCT —
    Manager owns its own SETNX); statusPub NOT touched (Manager owns
    its own FAILED publish on USER_CONFIRMATION_EXPIRED). (D8)
26. **`TestRouteUCT_ManagerRetryableErr_ReturnsError`** — Manager
    returns `model.NewDomainError(ErrCodeInternal, StageAwaitingUserConfirmation).WithRetryable(true)`;
    asserts (a) Route returns that error verbatim (non-nil → Consumer
    Nack→retry-DLX), (b) Guard NOT touched, (c) statusPub NOT touched.
    (D8 retryable)
27. **`TestRouteUCT_ManagerNonRetryableErr_AcksTrustsManagerDLQ`** —
    Manager returns
    `model.NewDomainError(ErrCodeInternal, StageAwaitingUserConfirmation).WithRetryable(false).WithDevMessage("resume: pending-state has nil ClassificationResult")`;
    asserts (a) Route returns nil (ACK — trust Manager's prior DLQ
    decisions, R4), (b) Guard NOT touched, (c) statusPub NOT touched,
    (d) NO DLQ publish from Router (Router has no DLQ port — D2 + R4).
    (D8 non-retryable)

### Cross-cutting pins

28. **`TestRouter_ConcurrentRouteRaceClean`** — kicks 200 goroutines
    invoking the 6 Route* methods with distinct (versionID/jobID/
    correlationID) inputs through faithful in-package fakes; asserts
    `-race` clean + every Route returns the expected outcome with no
    deadlock (bounded by `t.Deadline()`). (Concurrency invariant)
29. **`TestRouter_NoJobLimiterField`** — reflection-based: iterates
    `reflect.TypeOf((*Router)(nil)).Elem().NumField()`; asserts no
    field's type name contains "JobLimiter" or "Semaphore"; AND
    grep-style assertion (via `go/parser` on the `seams.go` file
    text) that no interface named `JobLimiter` is declared in the
    package. (R2 — Router does NOT pre-acquire)
30. **`TestRouter_NoDLQPublisherField`** — same reflection style;
    asserts no field of type `port.DLQPublisherPort` (R4 — Router
    does NOT publish DLQ; Manager-owned for UCT).

(Pin count: 30. The brief asked for "minimum 20"; this set covers
every D-decision + every R-reconciliation with at least one assertion.)

---

## PART D — REVIEWER GATE (Gates a..n)

The implementer's PR description includes this checklist; each item
maps to one or more test pins from PART C. A reviewer checks every box
before approval.

- [ ] **a. Hermetic imports.** Non-test files import ONLY stdlib +
  `domain/{model,port}` + `application/pipeline`. Active-fail forbidden
  set includes the 10+ banned packages (D10). (Pin 1)
- [ ] **b. Gofmt clean.** Every `.go` file is gofmt-formatted (Pin 2).
- [ ] **c. Constructor fail-fast.** `NewRouter` errors.Join all 8
  required-nil + 5 invalid-Config cases; success returns a fully-
  initialized `*Router` with noop seams substituted (Pin 3).
- [ ] **d. Key helpers deterministic + collision-free.** Pin 4.
- [ ] **e. VersionArtifactsReady — happy-path full sequence.** Acquire
  → start heartbeat → Run → SetCompleted → stop heartbeat → ACK; no
  out-of-order interleavings (Pins 5, 6).
- [ ] **f. VersionArtifactsReady — paused sentinel handling.** Router
  ACKs without SetCompleted; pipeline.IsPaused predicate exercised; no
  FAILED published by Router (Pin 7).
- [ ] **g. VersionArtifactsReady — pipeline FAILED handling.** Router
  surfaces the `*model.DomainError` to Consumer, SetCompletes the slot,
  does NOT re-publish FAILED (036 owns it). Retryable and non-retryable
  paths distinguished (Pins 8, 9).
- [ ] **h. Restart-decision-tree.** PROCESSING ⇒ retryable NACK
  (non-publishable code); PAUSED+hit ⇒ republish+ACK; PAUSED+miss ⇒
  publish USER_CONFIRMATION_EXPIRED FAILED + SetCompleted + ACK
  (R3); PAUSED+Load-error ⇒ retryable NACK; COMPLETED ⇒ ACK; Guard
  transport ⇒ retryable NACK (Pins 10, 11, 12, 13, 14, 15).
- [ ] **i. VersionCreated — cache populator semantics.** Happy: meta
  Set + SetCompleted + ACK. Duplicate: ACK (Pin 17). Meta-write
  failure: ACK silently, no SetCompleted (Pin 18). Guard transport:
  ACK silently (Pin 19) — the 036 DEFECT-1 degrade.
- [ ] **j. ArtifactsProvided + persist confirmations — routing only.**
  Deliver invoked; SetCompleted on success; registry-miss ⇒ ACK;
  duplicate ⇒ ACK; success/failure confirmation envelopes built via
  `port.NewPersistConfirmationSuccess`/`Failure` (Pins 20-24).
- [ ] **k. UserConfirmedType — Manager-driven.** Router does NOT
  guard (no SETNX lic-user-confirmed inside Router). nil/retryable/
  non-retryable mapping per D8 + R4 (Pins 25, 26, 27).
- [ ] **l. No double semaphore acquire.** Router struct has no
  JobLimiter field; seams.go declares no such interface (Pin 29 / R2).
- [ ] **m. No DLQ publisher in Router.** Router struct has no
  `port.DLQPublisherPort` field; no `dlqPub` constructor param
  (Pin 30 / R4).
- [ ] **n. Concurrency — race-clean.** `-race` ×N concurrent Route
  invocations, no deadlock, deterministic outcomes (Pin 28).
- [ ] **o. Stop-heartbeat-on-panic.** A panic from `PipelineRunner.Run`
  still triggers `defer stopHB()` (heartbeat.go:53 sync.Once). The
  panic propagates (Router does NOT recover — that is 039's consumer
  boundary; consumer/seams.go does not, either — broker is the panic
  boundary in DP/LIC). Pinned by an in-test
  `defer func(){_ = recover()}()` around a single Route call that uses
  a panicking PipelineRunner — assert stop func was invoked once.
- [ ] **p. RequestContext untouched.** Router does NOT call
  `WithRequestContext`; the ctx received from Consumer is passed
  verbatim to every collaborator (D13). Pinned by spying on the fake
  Logger that no `WithRequestContext` call originates inside Router.
  *(Note: the Router-local Logger seam intentionally OMITS the
  WithRequestContext method to make this a compile-time guarantee —
  Pin "p" downgrades to a code-review verification.)*

---

## PART E — FORWARD NOTES (recorded; owners elsewhere)

### 1. LIC-TASK-041 (DM Awaiters, `internal/application/dmawaiter` or `internal/ingress/dmawaiter`)

Owns the concrete `*dmawaiter.ArtifactsAwaiter` and
`*dmawaiter.PersistConfirmationAwaiter` types. Each exports the
domain port (`port.ArtifactsAwaiterPort` / `port.PersistConfirmationAwaiterPort`)
for the orchestrator-side Register/Await/Cancel surface AND an ingress-
side `Deliver(key, evt)` method (NOT a domain port; locally exposed).
LIC-TASK-047 wires the same concrete instance into Router (as
`ArtifactsAwaiterDeliverer` / `PersistConfirmationDeliverer` seam impls
— `var _ router.ArtifactsAwaiterDeliverer = (*dmawaiter.ArtifactsAwaiter)(nil)`)
AND into pipeline.Orchestrator (as the corresponding ports). The
Deliver methods MUST:

- Be safe to call after the awaiter's `Cancel(key)` has fired (return
  `errors.New("awaiter: no slot")` or a sentinel; Router silently
  ACKs on miss per D6/D7).
- Be safe to call CONCURRENTLY for distinct keys.
- Be deterministic single-receiver (a second Deliver for the same key
  while a registered receiver is waiting MUST either go to the
  registered receiver OR be silently dropped — never duplicate-fan).

### 2. LIC-TASK-044 (Status Publisher, `internal/egress/publisher`)

Owns `port.StatusPublisherPort`. Router uses it ONLY for the D4 step
4b PAUSED-miss path: publish FAILED{USER_CONFIRMATION_EXPIRED} for
`evt.JobID/DocumentID/VersionID/OrganizationID/CorrelationID`. The
publisher MUST honour the `LICStatusChangedEvent` schema
(events.go:190-238) — Status=FAILED ⇒ `ErrorCode`/`ErrorMessage`/
`IsRetryable` non-empty (publisher.go:13-14 godoc).

The Router constructs the event in-place (it does not have a helper
because it is a one-off use; the orchestrator's `statusEvent` is
unexported and orchestrator-private — orchestrator.go:1101). Mirror
the orchestrator's shape:

```go
func (r *Router) publishFailedTerminal(ctx context.Context, evt port.VersionProcessingArtifactsReady, code model.ErrorCode) {
    de := model.NewDomainError(code, model.StageAwaitingUserConfirmation) // catalog-sourced UserMessage
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
        r.log.Error(ctx, "router: FAILED status publish errored on §6.5:631 path; decision stands",
            "publish_error", pErr, "error_code", code.String(), "version_id", evt.VersionID)
    }
}
```

`IsPublishableToOrchestrator` gate: USER_CONFIRMATION_EXPIRED has
non-empty UserMessage (error_codes.go:211) ⇒ publishable; the gate
that 036/037 use (`if !code.IsPublishableToOrchestrator() { ... return
}`) does NOT short-circuit this code. The Router code-path bypasses
the gate because the Router only ever calls this with one fixed code;
add the gate defensively for forward-compat:

```go
if !code.IsPublishableToOrchestrator() {
    r.log.Error(ctx, "router: non-publishable code on §6.5:631 path; logged only",
        "error_code", code.String(), "version_id", evt.VersionID)
    return
}
```

### 3. LIC-TASK-046 (DLQ Publisher, `internal/egress/dlq`)

Owns `port.DLQPublisherPort`. Router does NOT use it (R4). For UCT:
Manager owns DLQ publish for the two §11.2 poison paths
(INVALID_CONTRACT_TYPE / INVALID_ORG_ID_MISMATCH); for §6.4
retry-budget-exhausted: broker-level via §6.4 deviation + future R1
seam-widening; for publish-failed / agent-output-invalid: 046's own
domain. **Wiring rule:** 047 does NOT inject `dlqPub` into the Router.

### 4. LIC-TASK-047 (App wiring, `internal/app` or `cmd/lic-worker/main.go`)

Constructs:

```go
guard, _ := idempotency.NewGuard(kvstoreSeam, idempotency.Config{
    HeartbeatInterval: cfg.Idempotency.HeartbeatInterval,
    FallbackEnabled:   cfg.Idempotency.FallbackEnabled,
}, idempotency.Deps{...})

// router.Config from config.IdempotencyConfig (Processing/Completed/
// PendingState/MetaCache all from cfg.Idempotency.{ProcessingTTL,TTL};
// HeartbeatInterval from cfg.Idempotency.HeartbeatInterval — the
// invariant guard.cfg.HeartbeatInterval == router.cfg.HeartbeatInterval
// is wired-asserted).
r, _ := router.NewRouter(
    router.Config{
        ProcessingTTL:     cfg.Idempotency.ProcessingTTL,
        CompletedTTL:      cfg.Idempotency.TTL,
        PendingStateTTL:   cfg.Idempotency.TTL,
        MetaCacheTTL:      cfg.Idempotency.TTL,
        HeartbeatInterval: cfg.Idempotency.HeartbeatInterval,
    },
    orchestrator,                // satisfies router.PipelineRunner (*pipeline.Orchestrator)
    manager,                     // satisfies router.PendingConfirmationManager (*pendingconfirmation.Manager)
    dmAwaiterArtifacts,          // satisfies router.ArtifactsAwaiterDeliverer
    dmAwaiterPersist,            // satisfies router.PersistConfirmationDeliverer
    versionMetaAdapter,          // satisfies router.VersionMetaCacheWriter (over *kvstore.Client)
    guard,                       // satisfies router.IdempotencyGuard
    pendingStateAdapter,         // port.PendingStatePort
    statusPubAdapter,            // port.StatusPublisherPort
    router.Deps{
        Metrics: noopOrAdapter,
        Clock:   systemClock{},
        Logger:  loggerAdapter,
        Tracer:  tracerAdapter,
    },
)

// 047 satisfaction assertions (NOT in the router package — D9/D10):
var _ consumer.EventRouter                    = (*router.Router)(nil)
var _ router.PipelineRunner                   = (*pipeline.Orchestrator)(nil)
var _ router.PendingConfirmationManager       = (*pendingconfirmation.Manager)(nil)
var _ router.IdempotencyGuard                 = (*idempotency.Guard)(nil)
var _ router.ArtifactsAwaiterDeliverer        = (*dmawaiter.ArtifactsAwaiter)(nil)
var _ router.PersistConfirmationDeliverer     = (*dmawaiter.PersistConfirmationAwaiter)(nil)
var _ router.VersionMetaCacheWriter           = (*kvVersionMetaAdapter)(nil)
```

**Wiring invariants (047 must enforce):**

- ONE `*idempotency.Guard` instance is shared as
  `router.IdempotencyGuard` AND as `port.IdempotencyStorePort` (into
  `pendingconfirmation.NewManager`). Two Guards would split the
  idempotency namespace.
- `cfg.Pipeline.Concurrency` flows into `pipeline.NewOrchestrator`'s
  `JobLimiter` (`concurrency.NewSemaphore` — pipeline forward-note #3)
  — NOT into the Router. R2.
- `cfg.Pipeline.PendingConfirmationTTL` (25h) flows into
  `pendingconfirmation.Config.PendingStateTTL` — NOT into router.Config
  (the Router's PendingStateTTL/CompletedTTL is for the §6.10:782 ACK
  path, separate axis).
- `cfg.Idempotency.UserConfirmedProcessingTTL` (90s) flows into
  `pendingconfirmation.Config.UserConfirmedProcessingTTL` — NOT into
  router.Config (Manager owns that SETNX, D8).

### 5. `go.mod` side-effects

The Router imports nothing new — `internal/application/pipeline` is
already a sibling. `go mod tidy` produces no diff (verified).

### 6. Architecture-doc staleness

- **high-architecture.md §6.5:631 says `PENDING_STATE_LOST`** — there
  is no such catalog code (R3). A future architecture-team pass
  should correct the doc to `USER_CONFIRMATION_EXPIRED`.
- **tasks.json LIC-TASK-040 acceptance "Concurrency control"** — 036
  already acquires (R2); acceptance is satisfied service-level. A
  future acceptance refresh should clarify split-ownership.
- **tasks.json LIC-TASK-040 acceptance "Transient error → DLX-loop
  NACK (read x-death...)"** — Router cannot read x-death without 039
  seam widening (R1). A future revision either widens the seam OR
  rephrases the acceptance to clarify broker-topology-driven
  escalation.

### 7. Open questions (resolved by this build-spec — recorded for posterity)

- Q: Should Router own DLQ publish for UCT non-retryable? **A: NO**
  — Manager owns it for poison wire messages; pipeline owns FAILED
  for terminal failures; "non-retryable that reaches Router" is
  always one of those (R4).
- Q: Should Router pre-Acquire the JobLimiter to satisfy the
  tasks.json "concurrency control" acceptance line? **A: NO** — 036
  already does it on the raw inbound ctx (R2).
- Q: Should Router introduce `lic_router_decisions_total`? **A: NO**
  in v1 — reuse `lic_idempotency_lookups_total`; the seam is
  forward-compatible for a future addition without API break (D12 /
  R5).
- Q: Should Router decode x-death for retry-level routing? **A: NO**
  in v1 — 039 seam doesn't carry Delivery; broker-topology drives
  escalation (R1).
- Q: Should `pipeline.IsPaused` be checked BEFORE or AFTER
  `model.IsRetryable`? **A: BEFORE** — pipeline.IsPaused is the
  identity-predicate for `ErrPipelinePaused` (not a DomainError);
  pipeline/CLAUDE.md "Pin 9 intact" + D4 step 3c.
- Q: For PAUSED + Load returning a non-`ErrPendingStateNotFound`
  error, should Router NACK or ACK? **A: NACK retryable** — the
  PendingStatePort is Redis-backed (037 forward-note); a transient
  there should retry just like an idempotency-store transient (D4
  step 4b Load-error branch; pin 13).
- Q: For VersionCreated cache-write failure, should Router NACK
  retryable or ACK silently? **A: ACK silently** — the cache is a
  fallback per 036 DEFECT-1; a NACK loop would not improve
  correctness, and the lic-version-created PROCESSING marker stays
  for redelivery retry (D5; pin 18).
- Q: For ArtifactsProvided / Persist confirmations on Guard transport
  error, should Router NACK or best-effort Deliver+ACK? **A: best-
  effort Deliver + ACK** — the awaiter slot is idempotent
  single-receiver; a NACK loop would not improve correctness (D6 /
  D7).
- Q: Where does the §6.10:786 "lic-trigger=COMPLETED EX 24h" cleanup
  for the resume path live? **A: pendingconfirmation.Manager
  step 9** (manager.go:438) — NOT Router. Router only sets
  lic-trigger=COMPLETED on the V_A_R-driven happy/failed/§6.5:631
  paths, never on the UCT-driven resume.

---

## PART F — IMPLEMENTATION NOTES (for golang-pro)

These are NOT decisions — they are mechanical hints to avoid common
pitfalls. The implementer follows D1..D14 verbatim; this section is
just to flag the easy mistakes.

1. **Don't import `internal/ingress/idempotency`.** The Router-local
   `IdempotencyGuard` seam structurally satisfies `*idempotency.Guard`
   — that satisfaction is asserted in 047, NOT in `internal_test.go`
   (which would force the forbidden import). The Guard type is never
   named in this package; the `idempotency.IdempotencyStatus` /
   `idempotency.ErrIdempotencyKeyExists` symbols come from
   `internal/domain/port` (frozen port — port.IdempotencyStatus /
   port.ErrIdempotencyKeyExists) which IS in the allowlist.
2. **Don't shadow `pipeline.ErrPipelinePaused`.** Use the imported
   symbol directly: `if pipeline.IsPaused(runErr) { ... }`. The
   predicate is the SSOT (pipeline/CLAUDE.md "IsPaused is the
   exported predicate"). A local sentinel would diverge on identity.
3. **`port.NewPersistConfirmationSuccess(&evt)` PANICS on nil.** The
   Router calls it with `&evt` from a typed parameter — never nil.
   But: if a future refactor introduces a pointer parameter, beware.
4. **`pipeline.Orchestrator.Run` may return raw context errors in
   edge cases** — the documented contract is "nil / ErrPipelinePaused /
   *model.DomainError" (orchestrator.go:233-247 — "Run returns nil iff
   COMPLETED"); a raw `context.Canceled` would be a contract violation
   AND would be classified by the Router as a non-Domain error. The
   safe handling: pass through verbatim (Consumer Nack→retry-DLX).
   No special-case needed in Router code; this is "trust the
   pipeline contract" — pinned by pin 8/9 (only DomainError shapes
   tested).
5. **`Logger.Info` vs `Warn` vs `Error`:**
   - `Error`: SetCompleted failure on a terminal slot (D4 step 3c /
     D5 / D6 / D7); 047-side WiringDefect signals; "FAILED status
     publish errored" on the §6.5:631 path.
   - `Warn`: Redis-down / awaiter registry-miss / cache-write
     failure — all the "ACK silently and degrade" paths.
   - `Info`: NOT used by Router in v1 (Consumer owns the per-delivery
     INFO; the audit trail for UCT is Manager-owned — manager.go:633).
6. **The 6 `Route*` method bodies are 5-15 lines each.** They are
   thin delegators to per-topic private `routeXxx(ctx, evt)` helpers
   that contain the actual flow logic. The split is purely for review
   readability — golang-pro MAY inline if it keeps the methods under
   ~25 lines each.
7. **`internal_test.go` allowlist size: 3 entries.** Test files MAY
   additionally import `testing`, `errors`, `bytes`, `reflect`,
   `encoding/json`, `time`, `context`, `sync`, `sync/atomic`,
   `go/parser`, `go/token`, `go/format`, `go/build` — these are
   stdlib (allowed) for the hermeticity + gofmt + reflection pins.
8. **`-race` clean** — every goroutine in tests has a deterministic
   stop condition; no `time.Sleep` ANYWHERE; the heartbeat tests use
   the Guard's noop ticker (which is itself test-injectable inside
   the Guard's own tests — but Router tests use a FAKE
   `IdempotencyGuard` seam whose `StartHeartbeat` returns a no-op
   stop func, so the Router tests do not exercise the real
   heartbeat ticker).
9. **CLAUDE.md** — populated AFTER implementation; this build-spec
   is the SSOT. The CLAUDE.md follows the 039/038/037 shape: Files /
   API / Reconciliations / Conventions / Forward notes. Each
   bullet cites the corresponding D/R from this build-spec.

---

## PART G — SUMMARY (one-page TL;DR for reviewers)

**What 040 builds:** an inbound routing-layer adapter
(`internal/ingress/router`) wired between the broker consumer (039) and
the pipeline orchestrator (036) + pending-confirmation manager (037).

**One exported type:** `*Router`; constructor `NewRouter(cfg, 8 required
collaborators, Deps) (*Router, error)` — fail-fast `errors.Join`.

**Six `Route*` methods** (the `consumer.EventRouter` seam):

1. `RouteVersionArtifactsReady` — §6.5 restart-decision-tree: Acquire
   lic-trigger → start heartbeat → pipeline.Run → SetCompleted; or
   PROCESSING ⇒ retryable NACK; PAUSED + hit ⇒ republish; PAUSED + miss
   ⇒ publish USER_CONFIRMATION_EXPIRED + SetCompleted + ACK (R3);
   COMPLETED ⇒ ACK; transport ⇒ retryable NACK.
2. `RouteVersionCreated` — cache populator: write lic-version-meta +
   SetCompleted; degrade silently on any failure (036 DEFECT-1).
3. `RouteArtifactsProvided` — Deliver to awaiter slot + SetCompleted.
4. `RoutePersisted` — Deliver as Success envelope + SetCompleted.
5. `RoutePersistFailed` — Deliver as Failure envelope + SetCompleted.
6. `RouteUserConfirmedType` — delegate to Manager; map nil/retryable/
   non-retryable to ACK/NACK/ACK (Manager owns its own DLQ — R4).

**Hermetic against:** broker, kvstore, observability concretes,
`internal/application/pendingconfirmation`, `internal/ingress/consumer`,
`internal/ingress/idempotency`, `internal/config`, all third-party.

**Imports:** stdlib + `domain/{model,port}` + `application/pipeline`
(for `ErrPipelinePaused` + `IsPaused` ONLY — identity sentinel).

**Does NOT:**

- acquire the JobLimiter (036 already does — R2);
- publish DLQ (Manager / Consumer / 046 own it — R4);
- read x-death (039 seam doesn't carry Delivery — R1);
- introduce a new Prometheus counter in v1 (R5);
- attach RequestContext to ctx (Consumer already did — D13);
- guard the lic-user-confirmed SETNX (Manager owns it — D8).

**Does:**

- own the lic-trigger idempotency guard at ingress (D4);
- own the lic-version-meta cache populator (D5);
- route DM artifacts/persist responses to the awaiters (D6/D7);
- publish USER_CONFIRMATION_EXPIRED FAILED for §6.5:631 + §6.10:777
  (R3);
- map every Route* return to nil/error per the 039 ACK boundary
  contract.

**Out of scope (forward-noted):** 041 dmawaiter Deliver concrete; 044
status publisher concrete; 046 DLQ publisher concrete; 047 wiring +
satisfaction assertions; x-death-aware retry routing (R1 seam-
widening).

**End of build-spec.** Total D-decisions: 14. Total R-reconciliations:
7. Total test pins: 30. Total reviewer-gates: 16 (a..p). The
golang-pro implementer can now write the code without making any
further architectural decisions.
