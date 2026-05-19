# router Package — CLAUDE.md

**Event Router / Dispatcher** (LIC-TASK-040, `high-architecture.md`
§6.2/§6.3/§6.5/§6.10, `integration-contracts.md` §6.4, `observability.md`
§3.6). The inbound routing layer wired between the broker consumer
(LIC-TASK-039) and the application body: the pipeline orchestrator
(LIC-TASK-036), the pending-confirmation manager (LIC-TASK-037), the DM
awaiters (LIC-TASK-041), and the version-meta cache adapter (LIC-TASK-047).
It is hermetic against `internal/infra/*` (broker / kvstore / observability
concretes), `internal/application/pendingconfirmation` (inverted via the
local `PendingConfirmationManager` seam) and `internal/ingress/{consumer,
idempotency}` (inverted via local seams). It imports
`internal/application/pipeline` for ONE identity-comparable sentinel only —
`pipeline.ErrPipelinePaused` + `pipeline.IsPaused` — the same pattern the
orchestrator's `Config.PausedSentinel` uses to communicate paused-ness across
the `pendingconfirmation` boundary without a circular import.

```
RouteVersionArtifactsReady →
  CheckAndAcquire(lic-trigger, ProcessingTTL)
    transport err          ⇒ *DomainError{IDEMPOTENCY_STORE_UNAVAIL, retryable}  (NACK→retry-DLX)
    (Absent,false,nil)     ⇒ StartHeartbeat → Pipeline.Run →
                             pipeline.IsPaused(err)        ⇒ ACK (no SetCompleted)
                             nil                           ⇒ SetCompleted(CompletedTTL) + ACK
                             *DomainError                  ⇒ SetCompleted(CompletedTTL) + return err verbatim
    (PROCESSING, true, nil)⇒ *DomainError{IDEMPOTENCY_STORE_UNAVAIL, retryable}  (NACK→retry-DLX, non-publishable)
    (PAUSED, true, nil)    ⇒ Load lic-pending-state:
                             ErrPendingStateNotFound ⇒ publish FAILED{USER_CONFIRMATION_EXPIRED} +
                                                       SetCompleted(PendingStateTTL) + ACK (R3)
                             other err               ⇒ retryable IDEMPOTENCY_STORE_UNAVAIL (NACK)
                             hit                     ⇒ Manager.RepublishPauseEvents → verbatim
    (COMPLETED, true, nil) ⇒ ACK (§6.5:632)

RouteVersionCreated → 2-status guard + json.Marshal(parent_version_id,origin_type) +
                      VersionMetaCacheWriter.Set + SetCompleted (every failure ⇒ silent ACK + WARN, 036 DEFECT-1)
RouteArtifactsProvided → 2-status guard + ArtifactsAwaiterDeliverer.Deliver + SetCompleted
                         (registry-miss / Guard-down / duplicate ⇒ silent ACK)
RoutePersisted        → 2-status guard + PersistConfirmationDeliverer.Deliver(NewPersistConfirmationSuccess) + SetCompleted
RoutePersistFailed    → 2-status guard + PersistConfirmationDeliverer.Deliver(NewPersistConfirmationFailure) + SetCompleted
RouteUserConfirmedType → Manager.HandleUserConfirmedType → {nil ⇒ ACK | retryable ⇒ NACK | non-retryable ⇒ ACK} (R4)
```

One exported type `*Router`. Constructor `NewRouter(Config, PipelineRunner,
PendingConfirmationManager, ArtifactsAwaiterDeliverer,
PersistConfirmationDeliverer, VersionMetaCacheWriter, IdempotencyGuard,
port.PendingStatePort, port.StatusPublisherPort, Deps) (*Router, error)` —
`NewTypeName` (`feedback_constructors.md`), fail-fast on any nil required
collaborator / invalid Config via `errors.Join` (the `consumer.NewConsumer` /
`pendingconfirmation.NewManager` / `idempotency.NewGuard` precedent).
Immutable after construction; the six `Route*` methods are goroutine-safe for
distinct deliveries (no mutable per-instance state; collaborators are
goroutine-safe per their own contracts). The struct deliberately has NO
`JobLimiter`/`Semaphore`/`DLQPublisherPort` field (R2/R4); the negative
reflection-walk pins (`TestRouter_NoJobLimiterField` /
`TestRouter_NoDLQPublisherField`) enforce these invariants.

## Files

- **router.go** — package doc (hermetic statement; D1..D14 attribution),
  `Config` (+`validate`), `Router` struct + `NewRouter` (`errors.Join`
  fail-fast over the 8 required collaborators + 5 invalid-Config cases),
  the 5 key-prefix constants + the `keyXxx(id)` helpers (D14), and the
  Router-local `publishFailedTerminal` / `setCompletedSafe` private
  helpers (D4 step 4b miss-branch / R3).
- **routes.go** — the 6 exported `Route*` methods (the
  `consumer.EventRouter` surface) — thin tracer-wrapped delegators to the
  per-topic private `routeXxx` functions; the per-topic flow bodies
  (D4..D8).
- **seams.go** — Router-local seam interfaces + zero-dependency noop
  defaults: `PipelineRunner` / `PendingConfirmationManager` (required, no
  noop), `ArtifactsAwaiterDeliverer` / `PersistConfirmationDeliverer` /
  `VersionMetaCacheWriter` / `IdempotencyGuard` (required, no noop),
  `Metrics`+`noopMetrics` (optional, D11/D12/R5 — reserved no-op
  `Decision(topic,decision)`), `Clock`+`systemClock` (optional, 1-method —
  used by `publishFailedTerminal`), `Logger`+`noopLogger` (Info/Warn/Error
  only — NO `WithRequestContext`, D13), `Tracer`+`noopTracer` + `RouteSpan`
  + `noopRouteSpan` (optional, per-route span). `var _` after each noop
  pair; the
  `var _ consumer.EventRouter = (*router.Router)(nil)` / `var _
  router.PipelineRunner = (*pipeline.Orchestrator)(nil)` etc. structural-
  satisfaction assertions live in the LIC-TASK-047 wiring package, NOT
  here (D10).
- **deps.go** — `Deps{Metrics, Clock, Logger, Tracer}` (optional-with-noop)
  + `withDefaults()` (the `pipeline.Deps` / `pendingconfirmation.Deps` /
  `consumer.Deps` / `idempotency.Deps` precedent). The 8 required
  collaborators are positional `NewRouter` params, NOT in `Deps`.
- **internal_test.go** — `TestHermeticImports` (3-entry
  `{domain/model, domain/port, application/pipeline}` allowlist + ZERO
  permitted third-party + the D10 active-fail forbidden set incl.
  `internal/application/pendingconfirmation`, `internal/ingress/{consumer,
  idempotency}`, `internal/infra/*`, `internal/config`,
  `internal/egress/*`, all third-party) + `TestGofmtClean` (`go/format`;
  the sandbox blocks `go fmt`).
- **router_test.go** — the full behavioural suite (Part C #1..#30) with
  in-package fakes for every seam + every port; `-race` clean, ZERO
  `time.Sleep`. Includes the static reflection pins
  (`TestRouter_NoJobLimiterField` / `TestRouter_NoDLQPublisherField`) and
  the panic-stop-heartbeat gate (reviewer gate o).
- **CLAUDE.md** — this file.

## API

- `NewRouter(Config, PipelineRunner, PendingConfirmationManager,
  ArtifactsAwaiterDeliverer, PersistConfirmationDeliverer,
  VersionMetaCacheWriter, IdempotencyGuard, port.PendingStatePort,
  port.StatusPublisherPort, Deps) (*Router, error)`.
- `(*Router) RouteVersionArtifactsReady(ctx, port.VersionProcessingArtifactsReady) error` — §6.5 restart-decision-tree.
- `(*Router) RouteVersionCreated(ctx, port.VersionCreated) error` — cache populator.
- `(*Router) RouteArtifactsProvided(ctx, port.ArtifactsProvided) error` — awaiter routing.
- `(*Router) RoutePersisted(ctx, port.LegalAnalysisArtifactsPersisted) error` — persist-success routing.
- `(*Router) RoutePersistFailed(ctx, port.LegalAnalysisArtifactsPersistFailed) error` — persist-failure routing.
- `(*Router) RouteUserConfirmedType(ctx, port.UserConfirmedType) error` — delegates to Manager.
- `Config{ProcessingTTL, CompletedTTL, PendingStateTTL, MetaCacheTTL,
  HeartbeatInterval}`; `Deps{Metrics, Clock, Logger, Tracer}` — every
  Deps field optional (nil ⇒ noop).
- `PipelineRunner` / `PendingConfirmationManager` /
  `ArtifactsAwaiterDeliverer` / `PersistConfirmationDeliverer` /
  `VersionMetaCacheWriter` / `IdempotencyGuard` / `Metrics` / `Clock` /
  `Logger` / `Tracer` / `RouteSpan` — the router-local seams (D9/D11).

## Reconciliations (build-spec PART B — DEFECT-style, condensed)

**R1 — x-death retry-level escalation is undetectable.** *Doc (tasks.json):*
"Transient error → DLX-loop NACK (read x-death count, route to retry.1/2/3
or DLQ при >3)". *Impl:* the 039 `EventRouter` seam deliberately does NOT
carry `broker.Delivery` (consumer/seams.go:71-77 — 039 D8 YAGNI), so the
Router has no way to read `XDeath()`. *Why correct:* widening the seam would
break 039's hermeticity + leak `broker.Delivery` across the D10 allowlist.
*Resolution:* v1 Router does NOT decode x-death; `Nack(false)` cycles to the
DLX with NO routing key (main queue has no static `x-dead-letter-routing-
key`, broker §6.4 deviation) and broker-topology `x-message-ttl` caps
redelivery. *Forward-note:* if 039's seam is widened, the Router adds a 7th
decision branch (`x-death.count >= retryBudget` ⇒ explicit
`dlqPub.PublishDLQ(DLQTopicConsumerFailed, env)` + ACK).

**R2 — tasks.json "Concurrency control" already satisfied by 036.** *Doc:*
"acquire semaphore (LIC_PIPELINE_CONCURRENCY) для pipeline-triggering
messages". *Impl:* `pipeline.Orchestrator.Run` itself calls
`o.limiter.Acquire(ctx)` step 1 (orchestrator.go:273 — Acquire BEFORE
WithTimeout, the binding 036 rule). *Why:* a Router pre-Acquire would
double-count the `lic_pipeline_concurrent_jobs` gauge (036 DEFECT-2). The
Router struct deliberately has NO `JobLimiter` field, NewRouter has no such
param, and `TestRouter_NoJobLimiterField` reflects + source-scans every
non-test file to enforce.

**R3 — `PENDING_STATE_LOST` is not a catalog code.** *Doc (high-arch
§6.5:631):* `error_code=PENDING_STATE_LOST`. *Impl:* use
`model.ErrCodeUserConfirmationExpired` (`USER_CONFIRMATION_EXPIRED`) — the
semantically identical catalog row (error_codes.go:52,209-213; devMessage
literally "pending classification confirmation TTL expired in Redis";
retryable=false; publishable). 037 already binds this code for the
symmetric §6.10:777 case (manager.go:355-364, 037 R2); R3 binds 040 to the
SAME code. *Resolution:* Router publishes
`FAILED{ErrCodeUserConfirmationExpired, StageAwaitingUserConfirmation,
retryable=false}` via `port.StatusPublisherPort.PublishStatus` and then
`Guard.SetCompleted(lic-trigger, PendingStateTTL)` (§6.10:782); returns nil
(ACK). NO DLQ — an expired pause is not poison (037 R5 manager.go:360-364).

**R4 — UCT non-retryable ⇒ Router ACKs (Manager owns the DLQ).** *Doc:*
"Поведение для unknown events: route to `lic.dlq.invalid-message`". *Impl:*
"unknown event" is owned by Consumer (039 D9 invalid path); the Router
never sees one — Consumer dispatches by typed `Route*` only after
successful decode+validate. *Resolution:* the Router does NOT take
`port.DLQPublisherPort` as a constructor dep; the `RouteUserConfirmedType`
mapping is `{nil ⇒ ACK, retryable err ⇒ NACK, non-retryable err ⇒ ACK}`
(D8). For every non-retryable error that reaches the Router: Manager has
either already DLQ-published (`INVALID_CONTRACT_TYPE` /
`INVALID_ORG_ID_MISMATCH` — manager.go:317,384) or deliberately did NOT
(corrupt-stored-blob with nil ClassificationResult — manager.go:399-401);
pipeline-terminal FAILED is published by 036's `publishFailed`, NOT DLQ.
`TestRouter_NoDLQPublisherField` enforces the struct-level invariant.

**R5 — No new Router-specific metric counter.** *Choice:* reuse the
existing `lic_idempotency_lookups_total{result}` (Guard-emitted, 038 D8).
Every Router decision maps 1:1 onto a Guard lookup result (acquired_run ⇔
result=new; in_progress_nack ⇔ result=in_progress[PROCESSING]; paused_* ⇔
result=in_progress[PAUSED]; completed ⇔ result=completed; transport_nack ⇔
result=fallback_db | absent-error). Adding a Router counter would near-
perfectly correlate — wasted cardinality. The `Metrics.Decision` seam is
shape-committed for a future R5 wire-up without an API break (noop on every
v1 impl).

**R6 — Router does NOT extract W3C trace headers.** *Doc (high-arch
§6.10:754 / observability.md §4.4):* `state.TraceContext` carries the saved
W3C `traceparent`/`tracestate` for resume span linkage; 040 "owns
populating it at ingress". *Impl:* the consumer.EventRouter seam carries
only the typed event DTO, not `broker.Delivery` (R1); the typed DTOs in
`port/events.go` do not carry `TraceContext` either. Hence
`PipelineState.TraceContext` stays zero-valued in v1
(orchestrator.go:newState already sets it to zero). *Why acceptable:* 037
R3 sanctions a zero `TraceContext` ("the `TraceRestorer` noop/adapter
treats `IsZero()` as 'no linkage'") — functional pipeline runs; only
cross-pause trace linkage is degraded (telemetry, not correctness).
*Forward-note:* when 039's seam is widened (R1) OR when the inbound DTOs
are extended with `trace_context`, the Router (or 039) plumbs the saved
W3C context through.

**R7 — Task acceptance is non-exhaustive; frozen contracts win.** (037 R5
/ 038 R2 / 039 R5 precedent.) The `tasks.json LIC-TASK-040` acceptance
enumerates 6-7 bullet checklist items; the frozen contracts
(consumer.EventRouter seam, pipeline.Run return contract, Manager API,
idempotency.Guard API, port.IdempotencyStorePort, port.PendingStatePort,
port.StatusPublisherPort, integration-contracts §6.4, high-architecture
§6.3/§6.5/§6.10) are the binding SSOT. Every R1..R6 adjudicates one such
tension; R7 codifies the policy.

## Conventions & deliberate decisions (build-spec D1..D14, condensed)

- **D1/D2 — one `*Router`; `NewRouter` fail-fast.** `errors.Join` of
  per-arg `errors.New` for each of the 8 required-nil + each of the 5
  invalid-Config cases; `(nil, joinedErr)` on any failure;
  `deps = deps.withDefaults()` on success. 8 required collaborators
  positional (load-bearing); 4 telemetry seams optional-with-noop in
  `Deps`. `port.DLQPublisherPort` is NOT a param (R4 — pin 30 enforces at
  the struct level).
- **D3 — local `Config`, ctor-injected, NOT `internal/config` import.**
  5 TTL/interval fields; `validate()` enforces every>0 + `CompletedTTL ≥
  ProcessingTTL` + `HeartbeatInterval < ProcessingTTL`. `HeartbeatInterval`
  is INFORMATIONAL on the Router (the Guard owns it); kept for the 047
  wiring cross-check invariant.
- **D4 — `RouteVersionArtifactsReady` §6.5:624-634 restart-decision-tree.**
  Order of checks on Run's return: `pipeline.IsPaused(runErr)` FIRST (Pin
  9 intact — sentinel is not a DomainError), then `runErr == nil`, then
  `*model.DomainError` (verbatim — Consumer maps via `model.IsRetryable`;
  pipeline already published FAILED). `stopHB()` is deferred IMMEDIATELY
  after `(Absent,false,nil)` so a panic inside `Run` still stops the
  heartbeat (sync.Once-guarded, twice-safe — heartbeat.go:53). Router
  NEVER calls `Guard.SetPaused` on lic-trigger — that is 037's job
  (manager.go:251). The PAUSED+pending-miss flow publishes
  `FAILED{USER_CONFIRMATION_EXPIRED}` via Router's `statusPub` (NOT
  pipeline's `publishFailed` — pipeline never ran for this delivery), then
  `SetCompleted(lic-trigger, PendingStateTTL)`, then ACK. PROCESSING/Load-
  error/Guard-transport ⇒ non-publishable `IDEMPOTENCY_STORE_UNAVAILABLE`
  retryable NACK.
- **D5 — `RouteVersionCreated` cache populator.** 2-status guard + JSON-
  marshal `(parent_version_id, origin_type)` + `VersionMetaCacheWriter.Set`
  + `SetCompleted(CompletedTTL)`. Every failure (Guard transport / marshal
  defect / cache-write fail) degrades SILENTLY (ACK + WARN/ERROR log) per
  036 DEFECT-1: trigger.ParentVersionID is PRIMARY; cache miss/error ⇒
  degrade to INITIAL, NEVER FAILED. A cache-write failure intentionally
  does NOT SetCompleted — leaves PROCESSING for redelivery retry.
- **D6/D7 — `RouteArtifactsProvided` / `RoutePersisted` /
  `RoutePersistFailed`.** 2-status guard + `Deliver(key, evt)` +
  `SetCompleted(CompletedTTL)`. Registry-miss (slot timed out + Cancel'd)
  ⇒ silent ACK + WARN (pipeline will publish `FAILED{DM_*_TIMEOUT}`
  itself). Guard transport ⇒ best-effort Deliver + ACK + WARN (NACK-loop
  would not improve correctness — awaiter is single-receiver, idempotent).
  Two distinct keys for persist (lic-persist-resp / lic-persist-fail) per
  §6.3.
- **D8 — `RouteUserConfirmedType` Manager-driven.** Manager OWNS the
  SETNX lic-user-confirmed:{version_id}, contract_type validation (regex
  + 12-whitelist), the §11.2 tenant check, and DLQ publication for
  invalid-format / invalid-whitelist / tenant-mismatch (manager.go:317,
  384). Router transparently delegates and maps `{nil ⇒ ACK, retryable
  err ⇒ NACK, non-retryable err ⇒ ACK}` (NOT `non-retryable ⇒ DLQ` — R4).
- **D9/D10 — locally-declared seams + hermetic allowlist EXACTLY 3
  entries.** `internal/domain/{model,port}` + `internal/application/
  pipeline` (for `ErrPipelinePaused`/`IsPaused` identity-sentinel only —
  same pattern as the orchestrator's `Config.PausedSentinel`). NO
  `internal/application/pendingconfirmation`, NO `internal/ingress/
  {consumer,idempotency}`, NO `internal/infra/*`, NO `internal/config`,
  NO `internal/egress/*`, NO third-party. The
  `var _ consumer.EventRouter = (*router.Router)(nil)` and 6 sibling
  satisfaction assertions live in the LIC-TASK-047 WIRING package, NOT
  here.
- **D11 — telemetry seams.** `Metrics.Decision(topic,decision)` reserved-
  noop (R5 binding non-introduction); 1-method `Clock` used only by
  `publishFailedTerminal` to stamp the §6.5:631 LICStatusChangedEvent
  Timestamp; `Logger` is Info/Warn/Error ONLY (NO `WithRequestContext` —
  Consumer attached it at the ingress boundary, D13; the omission is the
  compile-time guarantee for reviewer gate p); `Tracer.StartRoute(ctx,
  topic) (ctx, RouteSpan)` per-route span (noop default).
- **D12 — no new metric counter in v1 (R5).**
- **D13 — RequestContext untouched.** Consumer (039 D6/R4) calls
  `Logger.WithRequestContext(ctx, ids)` ONCE per delivery before invoking
  `Route*`; Router uses ctx verbatim when calling collaborators — every
  downstream log line inherits the IDs. The Router's local `Logger` seam
  intentionally OMITS `WithRequestContext` so there is no way to double-
  attach (the consumer-seam Logger has it; the router-seam Logger does
  not — they are different interfaces).
- **D14 — key-prefix constants + helper funcs.** 5 unexported `keyXxx(id)`
  helpers over 5 unexported prefix constants; direct string concat in
  route bodies is forbidden (Pin 4
  `TestKeyHelpers_DeterministicAndCollisionFree` enforces).
- **RU user messages.** Never inlined — `model.NewDomainError` pulls them
  from the frozen catalog (the consumer/aggregator/pendingconfirmation
  domain-layer SSOT discipline).
- **Concurrency / gofmt.** `*Router` is immutable after construction; the
  6 Route* methods are goroutine-safe for distinct deliveries (no
  per-instance mutable state; collaborators are goroutine-safe per their
  own contracts). `-race`-pinned ×200 (`TestRouter_ConcurrentRouteRaceClean`).
  `gofmt` self-check via `go/format` (sandbox blocks `go fmt`) — the
  consumer/idempotency/pendingconfirmation precedent.

## Forward notes (recorded, owners elsewhere — build-spec PART E)

1. **LIC-TASK-041 (DM Awaiters, `internal/application/dmawaiter` or
   `internal/ingress/dmawaiter`).** Owns the concrete
   `*dmawaiter.ArtifactsAwaiter` and `*dmawaiter.PersistConfirmationAwaiter`
   types. Each exports the domain port
   (`port.ArtifactsAwaiterPort` /
   `port.PersistConfirmationAwaiterPort`) for the orchestrator-side
   `Register/Await/Cancel` AND an ingress-side `Deliver(key, evt)` method
   (locally exposed). LIC-TASK-047 wires the SAME concrete instance into
   Router (as `ArtifactsAwaiterDeliverer` / `PersistConfirmationDeliverer`)
   AND into `pipeline.Orchestrator` (as the corresponding ports). The
   `Deliver` methods MUST be safe to call after `Cancel(key)`, safe to call
   CONCURRENTLY for distinct keys, and deterministic single-receiver (a
   second `Deliver` for the same key while a registered receiver is
   waiting MUST either go to the registered receiver OR be silently
   dropped — never duplicate-fan).
2. **LIC-TASK-044 (Status Publisher, `internal/egress/publisher`).** Owns
   `port.StatusPublisherPort`. Router uses it ONLY for the D4 step 4b
   PAUSED-miss path: publish FAILED{USER_CONFIRMATION_EXPIRED} for `evt.
   JobID/DocumentID/VersionID/OrganizationID/CorrelationID`. The publisher
   MUST honour the `LICStatusChangedEvent` schema (events.go:190-238) —
   `Status=FAILED` ⇒ `ErrorCode/ErrorMessage/IsRetryable` non-empty. The
   Router constructs the event in-place (mirroring orchestrator.go:1101's
   `statusEvent` shape) and defensively gates on
   `IsPublishableToOrchestrator` even though USER_CONFIRMATION_EXPIRED is
   the only v1 call site (publishable per error_codes.go:211).
3. **LIC-TASK-046 (DLQ Publisher, `internal/egress/dlq`).** Owns
   `port.DLQPublisherPort`. Router does NOT use it (R4). For UCT: Manager
   owns DLQ publish for the two §11.2 poison paths
   (`INVALID_CONTRACT_TYPE` / `INVALID_ORG_ID_MISMATCH`); for §6.4
   retry-budget-exhausted: broker-level via §6.4 deviation + future R1
   seam-widening; for publish-failed / agent-output-invalid: 046's own
   domain. **Wiring rule:** 047 does NOT inject `dlqPub` into the Router.
4. **LIC-TASK-047 (App wiring, `internal/app` or `cmd/lic-worker/main.go`).**
   Constructs `router.NewRouter` with `router.Config` sourced from
   `cfg.Idempotency.{ProcessingTTL, TTL, HeartbeatInterval}`. Wires:
   `*pipeline.Orchestrator` as `PipelineRunner`; `*pendingconfirmation.
   Manager` as `PendingConfirmationManager`; concrete dmawaiter instances
   as `ArtifactsAwaiterDeliverer` / `PersistConfirmationDeliverer`; the
   kvstore version-meta adapter as `VersionMetaCacheWriter`; the SAME
   `*idempotency.Guard` instance shared with `pendingconfirmation.Manager`
   (one Guard, two roles — `IdempotencyGuard` here, `port.IdempotencyStore-
   Port` there); the pendingstate Redis adapter as `port.PendingStatePort`;
   the publisher adapter as `port.StatusPublisherPort`. Asserts in the
   WIRING package (NOT here) `var _ consumer.EventRouter =
   (*router.Router)(nil)`, `var _ router.PipelineRunner =
   (*pipeline.Orchestrator)(nil)`, `var _ router.PendingConfirmationManager
   = (*pendingconfirmation.Manager)(nil)`, `var _ router.IdempotencyGuard
   = (*idempotency.Guard)(nil)`, `var _ router.ArtifactsAwaiterDeliverer
   = (*dmawaiter.ArtifactsAwaiter)(nil)`, `var _ router.
   PersistConfirmationDeliverer = (*dmawaiter.PersistConfirmationAwaiter)
   (nil)`, `var _ router.VersionMetaCacheWriter = (*kvVersionMetaAdapter)
   (nil)`. **Wiring invariants:** ONE `*idempotency.Guard` shared (two
   roles would split the idempotency namespace);
   `cfg.Pipeline.Concurrency` flows into `pipeline.NewOrchestrator`'s
   `JobLimiter` (NOT into the Router — R2); `cfg.Pipeline.Pending-
   ConfirmationTTL` (25h) flows into `pendingconfirmation.Config.Pending-
   StateTTL` (NOT into `router.Config`); `cfg.Idempotency.UserConfirmed-
   ProcessingTTL` (90s) flows into `pendingconfirmation.Config.User-
   ConfirmedProcessingTTL` (NOT into `router.Config` — Manager owns that
   SETNX).
5. **`go.mod` side-effects.** The Router imports nothing new —
   `internal/application/pipeline` is already a sibling. `go mod tidy`
   produces no diff (verified).
6. **Architecture-doc staleness (recorded, architecture-team-owned).**
   `high-architecture.md §6.5:631` says `PENDING_STATE_LOST` — there is no
   such catalog code (R3); a future architecture-team pass should correct
   the doc to `USER_CONFIRMATION_EXPIRED`. `tasks.json LIC-TASK-040`
   acceptance "Concurrency control" is satisfied at the service-level by
   036 (R2); a future acceptance refresh should clarify split-ownership.
   `tasks.json LIC-TASK-040` acceptance "Transient error → DLX-loop NACK
   (read x-death...)" cannot be satisfied without 039 seam widening (R1);
   a future revision either widens the seam OR rephrases the acceptance.
