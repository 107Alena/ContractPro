# pipeline Package — CLAUDE.md

**Pipeline Orchestrator** (LIC-TASK-036, `high-architecture.md` §6.5 /
§6.12 / §6.14 / §8.3 / §8.7 / §8.10, `observability.md` §3.2 / §4.2,
`error-handling.md` §3). The coordinating body that runs one analysis
job end-to-end over the Stage Executor (LIC-TASK-034) and the Result
Aggregator (LIC-TASK-035):

```
Acquire job semaphore → WithTimeout(90s) → root span → build state →
publish IN_PROGRESS → resolve parent+mode → request current (+RE_CHECK
parent) → await current → MAX_INGESTED_BYTES cap → await parent
(DEGRADE, never fail) → Stage 1 → confidence pause gate → Stage 2/3 →
aggregator MERGE-EARLY → Stage 4/5/6 → aggregator FINALIZE-LATE →
build payload (null risk_delta on parent-missing) → publish
analysis-ready → await persist → publish COMPLETED.
```

Constructor: `NewOrchestrator(cfg, exec, agg, artReq, artAwait,
analysisPub, persistAwait, statusPub, uncertainPub, deps) (*Orchestrator,
error)` — `NewTypeName` (`feedback_constructors.md`), fail-fast on
invalid `Config` / any nil required collaborator via `errors.Join` (the
`stages.NewExecutor` / `aggregator.NewAggregator` precedent). NOT a
`port.Agent`. Immutable after construction; one instance is shared
across concurrent jobs (each `Run` builds its own `*PipelineState`).

`Run(ctx, trigger) error` returns **nil ⇒ COMPLETED** (caller ACKs the
delivery, §6.5 step 11) or a **non-nil `*model.DomainError` AFTER
publishing the terminal FAILED itself** (caller uses `model.IsRetryable`
for the broker ACK/NACK decision). `Run` never touches the broker
(hermeticity).

## Files

- **orchestrator.go** — package doc, `Config` (+`validate`),
  `Orchestrator`, `NewOrchestrator`, `Run` (steps 0..12 + the single
  `continueFromStage2` call), `continueFromStage2` (the shared
  Stage-2..21 body — old Run steps 13..21 VERBATIM, behavior-preserving
  extraction; LIC-TASK-037 D2), `ResumeAfterConfirmation` (the exported
  post-confirmation resume entrypoint — re-establishes the Run wrapper
  for an already-classified state, R1..R8) + `reCheckParentRefetchFor-
  Resume` (the D8 `:parent:resume` degrade-never-fail block),
  `ErrPipelinePaused`/`IsPaused`/`outcomePaused` (the LIC-TASK-037
  paused sentinel + predicate + outcome label), the private helpers
  (`newState`, `resolveParentAndMode`, `requestAndAwaitCurrent`,
  `awaitParentAnalysis`, `missingRequired`, `buildAggregatorInput`,
  `mergeEarly`, `finalizeLate`, `buildPayload`, `awaitPersist`), the
  outcome path (`classifyOutcome` — now with the `ErrPipelinePaused`
  branch + 3-way finalizer switch, `codeLabelFor`, `statusEvent`,
  `publishFailed`, `finalizePrePipeline`).
- **seams.go** — `JobLimiter` / `PipelineMetrics` / `Tracer`+
  `PipelineSpan` / `Clock` / `Logger` / `VersionMetaCache` /
  `PauseController` seams + zero-dependency noop defaults + `var _`
  assertions (the universal B-4 precedent).
- **deps.go** — `Deps` (the optional-with-noop seam bundle) +
  `withDefaults()` (mirrors `stages.Deps`).
- **internal_test.go** — `TestHermeticImports` (4-entry allowlist +
  active-fail forbidden set incl. all third-party) + `TestGofmtClean`
  (`go/format`; the sandbox blocks `go fmt`).
- **orchestrator_test.go** — full behavioural suite with in-package
  fakes for every seam + every port and a REAL `stages.Executor`
  (fake `port.Agent`s) + REAL `aggregator.Aggregator`. The 24
  pre-037 functions are UNTOUCHED by LIC-TASK-037 (the D.B reviewer
  gate); the LIC-TASK-037 ADDITIVE pins (D.B 18..23: real-pause
  sentinel, `ResumeAfterConfirmation` happy / RE_CHECK parent-refetch /
  single-finalizer / job-timeout / acquire-before-WithTimeout, and the
  `continueFromStage2` shared-body byte-identity pin) are appended only.
- **CLAUDE.md** — this file.

## API

- `NewOrchestrator(Config, *stages.Executor, *aggregator.Aggregator,
  port.{ArtifactRequester,ArtifactsAwaiter,AnalysisArtifactsPublisher,
  PersistConfirmationAwaiter,StatusPublisher,UncertaintyPublisher}Port,
  Deps) (*Orchestrator, error)`.
- `(*Orchestrator) Run(ctx, port.VersionProcessingArtifactsReady)
  error`.
- `(*Orchestrator) ResumeAfterConfirmation(ctx,
  *model.PipelineState) error` — LIC-TASK-037 post-confirmation resume
  entrypoint (Stage 2..21 for an already-classified state; nil ⇒
  COMPLETED, else a `*model.DomainError` after publishing the single
  terminal FAILED itself). Invoked by `pendingconfirmation.Manager`
  via its local `PipelineResumer` seam (LIC-TASK-047 wires it; the
  `var _` assertion is in the wiring package, NOT here — D18).
- `ErrPipelinePaused error` + `IsPaused(error) bool` — the LIC-TASK-037
  paused sentinel and its predicate. `pendingconfirmation.Manager`
  returns this exact value via its injected `Config.PausedSentinel`
  (it does NOT import this package — D5); `errors.Is` works across
  that boundary because it is the same error value.
- `Config{JobTimeout, DMRequestTimeout, DMPersistConfirmTimeout,
  ConfidenceThreshold, MaxIngestedBytes}`.
- `Deps{JobLimiter, Metrics, Tracer, Clock, Logger, VersionMetaCache,
  PauseController}` — every field optional (nil ⇒ noop).

## Defect reconciliations (build-spec DEFECT-1..4 — binding, recorded here authoritatively)

- **DEFECT-1 — `trigger.ParentVersionID` is the PRIMARY RE_CHECK
  source; `VersionMetaCache` is a FALLBACK ONLY.** The verified trigger
  DTO already carries `ParentVersionID *string` (events.go:51;
  ASSUMPTION-LIC-02). The `lic-version-meta` cache is populated by the
  `VersionCreated` handler (LIC-TASK-040), not 036; 036 only *reads* a
  parent pointer. `resolveParentAndMode`: `parentVID :=
  trigger.ParentVersionID`; iff `nil`, consult
  `metaCache.GetParentVersionID` (the §8.3 race where the trigger lacks
  it but the cache has it). `mode = RE_CHECK iff resolvedParent != nil`.
  A cache **miss OR error** is treated identically — degrade to INITIAL,
  never FAILED (high-arch:1069-1070). The started/duration metric label
  is fixed at entry from the trigger pointer (cannot relabel after
  `PipelineStarted`); the *outcome* metric uses the resolved mode
  (`string(state.Mode)` read at finalize). Pinned by
  `TestRun_HappyReCheck_TriggerPointer` (cache NOT consulted) /
  `TestRun_ReCheck_CacheFallback` (cache consulted once).
- **DEFECT-2 — no `ConcurrentJobs` on `PipelineMetrics`.**
  `lic_pipeline_concurrent_jobs` is owned EXCLUSIVELY by the
  `*concurrency.Semaphore` gauge attached via
  `concurrency.WithGauge(metrics.Pipeline.ConcurrentJobs)` in
  LIC-TASK-047 (semaphore.go:115-148, concurrency/CLAUDE.md FN-1). The
  Orchestrator never touches that gauge; the `JobLimiter` seam exposes
  no gauge method. Adding Inc/Dec to the `PipelineMetrics` adapter in
  047 would double-count — recorded so a "consistency" pass does not
  reintroduce it. Pinned by `TestRun_Metrics` (`concurrentSeen` stays
  false; the seam has no such method to call).
- **DEFECT-3 — the noop `PauseController` MUST be non-retryable.**
  `model.NewDomainError(ErrCodeInternal, …)` defaults `Retryable:true`
  (error_codes.go:221-225). The happy-path-only stub therefore chains
  `.WithRetryable(false)` so a low-confidence run is a terminal
  non-retryable FAILED — never endlessly redelivered by LIC-TASK-040.
  Pinned by `TestRun_LowConfidence_NoopPause_NonRetryable` (asserts
  `IsRetryable==false`, stage `STAGE_AWAITING_USER_CONFIRMATION`).
- **DEFECT-4 — 036 owns the pipeline OUTCOME; 040 owns the broker
  ACK/NACK.** §6.5 step 11's "ACK" is the *consumer's* (LIC-TASK-040)
  action triggered by a nil `Run` return. 036 has no broker handle
  (hermeticity) and never calls `delivery.Ack/Nack`; it returns a typed
  error (or nil) and is the SOLE publisher of the terminal status event.
  The 036↔040 contract is the `Run` godoc.

## Conventions & deliberate decisions (build-spec D1..D14)

- **D1 — seam vs port.** A collaborator is a SEAM (local interface +
  noop, `Deps`-injected) iff it is telemetry/runtime-environment, its
  concrete impl would force a forbidden import, or it is non-required
  for the happy path; a PORT (`internal/domain/port`) iff it is a
  frozen cross-domain async wire. Seams: `JobLimiter`,
  `PipelineMetrics`, `Tracer`, `Clock`, `Logger`, `VersionMetaCache`,
  `PauseController`. Ports (on the struct directly): artifact request /
  artifacts-await / analysis-ready publish / persist-await / status
  publish / uncertainty publish. `*stages.Executor` /
  `*aggregator.Aggregator` are CONCRETE (sanctioned by the §7
  allowlist) — everything crossing to an unimplemented package
  (`publisher`/`dmawaiter`/`pendingconfirmation`) is a port.
- **D3 — aggregator MERGE-EARLY then FINALIZE-LATE.** `Aggregate` runs
  ONCE after Stage 3 (merge-early): `Output.MergedRiskAnalysis →
  state.MergedRiskAnalysis` (+ RiskProfile/AggregateScore) BEFORE
  Stage 4 — `stages.RunStage4` reads `state.MergedRiskAnalysis` via
  `buildInput`; skipping this makes Agent 6 fail INTERNAL_ERROR (stages
  D2). It runs AGAIN after Stage 6 (finalize-late) over the
  now-complete state for the final `Warnings` (the cross-agent
  `RECOMMENDATION_ORPHAN_REF` / `CLASSIFICATION_PARAMS_MISMATCH` need
  the full Recommendations set). `Aggregate` is pure & idempotent over a
  fixed Input (aggregator D5) so the second call is safe. Pinned by
  `TestRun_MergeEarly_Ordering` (Agent 6 observes a merged analysis) /
  `TestRun_AggregatorCalledTwice`.
- **D7 — RE_CHECK parent fetch: sequential, DEGRADE never fail.** Both
  requests are fired back-to-back (cheap fire-and-forget publishes with
  distinct correlation suffixes `:current`/`:parent` so out-of-order
  broker delivery still routes), then awaited SEQUENTIALLY (current
  first — it gates the whole pipeline and fails fast; the parent is
  degradable). NOT parallel: two goroutines + a join would need a
  stdlib errgroup-equivalent (no `golang.org/x/sync`) for a non-critical
  branch — unjustified vs. `stages.parallel()` (which exists because
  *both* its branches are critical). ANY parent problem (await error
  incl. `ErrAwaitTimeout`, `ErrorCode`, missing/empty RISK_ANALYSIS,
  unmarshal failure) ⇒ `parentMissing=true`, `ParentRiskAnalysis` stays
  nil, WARN-log, CONTINUE. `stages.RunStage6` self-gates on nil
  `ParentRiskAnalysis` (skips Agent 9); the
  `RE_CHECK_PARENT_ANALYSIS_MISSING` warning is rendered by the
  aggregator via `Input.ParentAnalysisMissing`; the Orchestrator (NOT
  the aggregator) nulls outbound `risk_delta` (§8.7 step 4). Pinned by
  `TestRun_ReCheck_ParentDegrade`.
- **D8 — inline `MAX_INGESTED_BYTES` cap; no Token Estimator.** 036 v1
  sums `len(raw)` over `ArtifactsProvided.Artifacts` and fails
  `DOCUMENT_TOO_LARGE` (non-retryable, `STAGE_ARTIFACTS_RECEIVED`) over
  the limit. `aggregator.Input.Truncation` is always nil here —
  `INPUT_TRUNCATED` is not raised by 036 v1 (forward note: LIC-TASK-021
  Token Estimator). Pinned by `TestRun_DocumentTooLarge`.
- **D11 — single terminal path + timeout discriminator.** Steps 6..21
  return a raw error through ONE named-return deferred finalizer that is
  the SOLE site classifying the outcome, recording the exit metrics,
  closing the span, and (on failure) publishing the SINGLE terminal
  FAILED. The discriminator is binding: `rootCtx.Err() ==
  context.DeadlineExceeded` ⇒ outcome `timeout`, fresh retryable
  `ANALYSIS_TIMEOUT` EVEN IF the body returned a different
  `*model.DomainError` (a stage's ctx-cancelled error is a symptom; the
  job deadline is the root cause — §8.10). Else a propagated
  `*model.DomainError` is verbatim; a non-DomainError is wrapped
  `INTERNAL_ERROR` (the base MF-2 "never swallow"). Success publishes
  COMPLETED inline at step 21 (persist-confirm ordering); the finalizer
  must NOT double-publish. Pinned by `TestRun_JobTimeout_Reclassify` /
  `TestRun_SingleFailedPublish`.
- **D-PAUSE — the LIC-TASK-037 resume continuation reuses the Run
  wrapper + single-publish finalizer; `continueFromStage2` is the
  shared Stage-2..21 body.** The old `Run` steps 13..21 were extracted
  VERBATIM into `continueFromStage2` (a pure, behavior-preserving
  refactor — the 24 pre-037 `orchestrator_test.go` functions pass
  unedited; `TestContinueFromStage2_SharedBody` pins that `Run` and
  `ResumeAfterConfirmation` emit byte-identical
  `LegalAnalysisArtifactsReady` for the same post-Stage-1 state).
  `ResumeAfterConfirmation` mirrors `Run` steps 1-5 (re-Acquire a
  JobLimiter slot on the raw ctx — build-spec D9; fresh
  `WithTimeout(JobTimeout)` — high-arch:784; root span linked to the
  caller-restored trace context; the SAME 3-way finalizer reusing
  `classifyOutcome`) MINUS artifact-request / Stage 1 / the confidence
  gate, then re-fetches the RE_CHECK parent degradably via the
  distinct `:parent:resume` correlation suffix (D8), then calls
  `continueFromStage2`. The paused sentinel: `classifyOutcome` gained
  ONE `errors.Is(bodyErr, ErrPipelinePaused)` branch (→ the new,
  non-failure `outcomePaused`); the finalizer became a 3-way switch
  (success / paused → `span.Finish(nil)` + return the sentinel, NO
  FAILED / failed|timeout). **Pin 9
  (`TestRun_LowConfidence_NoopPause_NonRetryable`) is intact** because
  the noop `PauseController` returns a terminal non-retryable
  `INTERNAL_ERROR` (NOT `ErrPipelinePaused`), so it falls through to
  `outcomeFailed` exactly as before. The new label value
  `lic_pipeline_outcome_total{outcome="paused"}` is the deliberate
  RECONCILIATION R7 (recorded in `pendingconfirmation/CLAUDE.md`).
  `NewOrchestrator` signature, `deps.go` (`PauseController` already a
  `Deps` field) and the `internal_test.go` 4-entry allowlist are
  UNCHANGED — `pipeline` does NOT import `pendingconfirmation` and
  vice-versa (the seam is declared in `pendingconfirmation`; the
  sentinel flows as `Config.PausedSentinel`). Pinned by
  `TestRun_LowConfidence_RealPause_Sentinel` + the D.B-19..23 resume
  pins.
- **Acquire BEFORE WithTimeout (binding).** `JobLimiter.Acquire` uses
  the RAW inbound `ctx`, not the timeout-wrapped one: a job queued
  behind the concurrency cap is "queued", not "timed out" (§6.14
  backpressure). The 90s budget covers only pipeline work. `Acquire`
  returns raw `context.Canceled`/`DeadlineExceeded` (semaphore.go) —
  branched via `errors.Is`, never `AsDomainError`. A failed Acquire
  consumes no slot ⇒ no `Release`; a pre-pipeline failure still
  publishes FAILED ("на любой ошибке") via `finalizePrePipeline`.
  Pinned by `TestRun_AcquireBeforeWithTimeout_BudgetIntact` /
  `TestRun_AcquireTimeout_PublishesFailed`.
- **Defer/cleanup LIFO (load-bearing).** Registration order →
  reverse-execution order: `cancel()` (WithTimeout) → `span.Finish`
  (via finalizer) → outcome finalizer (metrics + terminal publish) →
  `persistAwait.Cancel(jobID)` → `artAwait.Cancel(parCorr)` →
  `artAwait.Cancel(curCorr)` → `limiter.Release()`. `Release` is
  registered FIRST (right after a successful `Acquire`) so it runs LAST
  — the semaphore slot is held for the ENTIRE job incl. the terminal
  publish and span close (§6.14 "slot held while in-flight"; the gauge
  stays accurate). The outcome finalizer runs BEFORE `Release` so
  `lic_pipeline_total_duration_seconds` and the FAILED publish complete
  inside the held slot. A body panic still `Release`s (defer) and the
  panic propagates (036 does not `recover` — that is LIC-TASK-040's
  consumer-boundary job). Pinned by `TestRun_SemaphoreLifecycle`
  (incl. the panic sub-test, run via the SEQUENTIAL Stage 4 — a panic
  from a `parallel()` stage would crash the process, `stages.parallel`
  deliberately does not recover) / `TestRun_AwaiterCleanup`.
- **`IsPublishableToOrchestrator` gate.** `publishFailed` is the single
  FAILED-publish site. A non-publishable code (empty catalog
  UserMessage — `INVALID_MESSAGE_SCHEMA`/`INVALID_ORG_ID_MISMATCH`/
  `IDEMPOTENCY_STORE_UNAVAILABLE`) is DLQ-logged via the `Logger` seam
  instead of published with an empty error_message (a real DLQ publish
  is LIC-TASK-046/040's job — 036 has no `DLQPublisherPort` by design).
  None of 036's §6 codes are non-publishable, so the gate is defensive;
  a `PublishStatus` error is logged but does NOT mask the analysis
  failure (040 NACKs on the returned `de`).
- **036 does NOT re-derive Stage-2/6 degradation from nil fields.** The
  trace `Degraded` event (emitted inside `stages.Executor`) is the SSOT
  for a non-critical agent skip (stages D6); a returned err from
  `RunStage2/6` is a genuine non-timeout fatal (the executor already
  degraded a per-agent `AGENT_TIMEOUT` internally). The stages/CLAUDE.md
  FN-1 "036 owns structured WARN logging of a Stage-2/6 degradation" is
  satisfied by LIC-TASK-047's Tracer-adapter→logger bridge, NOT by a
  036 `state.PartyConsistency==nil` inspection (ambiguous: skip vs.
  legitimately empty).
- **RU user messages.** Never inlined — `model.NewDomainError` pulls
  them from the frozen error catalog (the aggregator D8 / domain-layer
  SSOT discipline).
- **Concurrency / gofmt.** `*Orchestrator` is immutable after
  construction; `Run` is goroutine-safe for distinct triggers (own
  `*PipelineState`; `stages.Executor`/`aggregator.Aggregator`
  concurrency-safe; ports stateless; seams immutable). `-race`-pinned
  ×16 (`TestRun_ConcurrentRaceClean`). `gofmt` self-check via
  `go/format` (sandbox blocks `go fmt`) — the aggregator/stages
  precedent.

## Forward notes (recorded, owners elsewhere)

1. **LIC-TASK-037 (pause/resume) — DONE (this forward note is now a
   reconciliation pointer).** The real pause/resume body shipped in
   `internal/application/pendingconfirmation` (the
   `*pendingconfirmation.Manager`: `Pause` = §6.5:611-617 strict order
   SET `lic-pending-state` → publish `classification-uncertain` →
   publish `IN_PROGRESS{STAGE_AWAITING_USER_CONFIRMATION}` → SET
   `lic-trigger=PAUSED`; `HandleUserConfirmedType` = §6.10 Resume;
   `RepublishPauseEvents` = §6.5:631 safety-net). It does NOT import
   this package: it returns this package's `ErrPipelinePaused` via the
   injected `Config.PausedSentinel` (D5) and drives Stage 2..21 via its
   local `PipelineResumer` seam bound to `ResumeAfterConfirmation`
   (D11). On this side: `classifyOutcome` gained the ONE
   `errors.Is(bodyErr, ErrPipelinePaused)` branch (→ the non-failure
   `outcomePaused`), the finalizer became the 3-way switch, `Run`'s
   Stage-2..21 body was extracted VERBATIM into `continueFromStage2`
   (behavior-preserving — the 24 pre-037 pins pass unedited), and
   `ResumeAfterConfirmation`/`reCheckParentRefetchForResume` were
   added. `NewOrchestrator` signature, `deps.go` and the
   `internal_test.go` allowlist are UNCHANGED. **The authoritative
   037 reconciliations (R1..R7) live in
   `internal/application/pendingconfirmation/CLAUDE.md`** and the
   D-PAUSE convention bullet above; this note remains only as the
   cross-reference.
2. **LIC-TASK-040 (Event Router/consumer + broker ACK).** Owns the
   RabbitMQ consumer, the `lic-trigger:{version_id}` idempotency guard,
   restart-semantics (§6.5:624-634), the W3C trace-context extraction
   into `state.TraceContext` (pass on the trigger or via ctx — 036
   currently zeroes it), and the broker ACK/NACK decision: call
   `orch.Run(deliveryCtx, trigger)`; `nil` → `delivery.Ack`; non-nil
   with `model.IsRetryable(err)==true` → NACK→retry-DLX; `==false` →
   DLQ. 040 owns the DLQ publish for non-publishable codes (036 only
   logs); 036 has no `DLQPublisherPort` by design.
3. **LIC-TASK-047 (wiring).** `stages.NewExecutor(9 agents,
   stages.Deps{...})`; `aggregator.NewAggregator(aggregator.Config{from
   config.ScoringConfig}, metricsAdapter)`; concrete
   `egress/publisher`/`dmawaiter`/`pendingconfirmation` impls for the
   ports; `pipeline.Config{from config.PipelineConfig/DMConfig}`;
   `Deps{JobLimiter: concurrency.New(cfg.Pipeline.Concurrency,
   concurrency.WithGauge(metrics.Pipeline.ConcurrentJobs)), Metrics:
   pipelineMetricsAdapter, Tracer: tracerAdapter, Clock: systemClock{},
   Logger: loggerAdapter, VersionMetaCache: kvVersionMetaAdapter,
   PauseController: <037 impl>}`. `*concurrency.Semaphore` satisfies
   `JobLimiter` STRUCTURALLY — pass directly, NO adapter; assert
   `var _ JobLimiter = (*concurrency.Semaphore)(nil)` in the *wiring*
   package, NOT here (the aggregator `Repairer`-seam precedent). The
   `lic_pipeline_concurrent_jobs` gauge is owned solely by that
   semaphore (DEFECT-2) — the `PipelineMetrics` adapter must NOT also
   Inc/Dec it. `tracerAdapter.StartPipeline` opens `lic.pipeline`
   (§4.2 root attrs); `StartChild` → `lic.dm.request`/`lic.dm.await`/
   `lic.dm.await.parent`/`lic.aggregate.merge`/`lic.aggregate.finalize`/
   `lic.publish`/`lic.persist.await`; per-stage `lic.stage.*` spans nest
   automatically because 036 passes `spanCtx` into `exec.RunStageN`.
4. **v1 SSOT gap — Agent-3/9 skip has no DETAILED_REPORT.warnings
   code** (inherited from stages FN-3 / aggregator). 036 does not
   invent one; the trace `Degraded` event + base invocation metric are
   the v1 transparency surface. Widening `model.Warnings` is a contract
   change owned by the architecture team.
5. **LIC-TASK-021 (Token Estimator).** When it lands, source
   `aggregator.Input.Truncation` from it (036 currently passes nil) so
   `INPUT_TRUNCATED` can fire; `MAX_INGESTED_BYTES` (D8) is the v1
   stopgap, not a replacement.
