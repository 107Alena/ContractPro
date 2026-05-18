# pendingconfirmation Package — CLAUDE.md

**Pending Type Confirmation Manager** (LIC-TASK-037,
`high-architecture.md` §6.5 / §6.10, `security.md` §11.2,
`error-handling.md` §3.6, `observability.md` §3.7). The two-halves-of-
one-state-machine body of the low-confidence pause/resume cycle around
one Redis namespace (`lic-pending-state` / `lic-trigger` /
`lic-user-confirmed`) and one metric family:

```
Pause:   SET lic-pending-state → publish classification-uncertain
         (broker-confirmed) → publish IN_PROGRESS{STAGE_AWAITING_USER_
         CONFIRMATION} (broker-confirmed) → SET lic-trigger=PAUSED →
         return Config.PausedSentinel (= pipeline.ErrPipelinePaused).
Resume:  validate contract_type → SETNX lic-user-confirmed → Load
         lic-pending-state → tenant check → restore state → override
         classification (confidence=1.0) → restore trace → drive
         pipeline.ResumeAfterConfirmation → §6.10-step-9 cleanup.
Republish: re-publish the two pause events only (§6.5:631 safety-net).
```

One exported type `*Manager` satisfies BOTH roles (build-spec D1):
`Pause` structurally satisfies `pipeline.PauseController`;
`HandleUserConfirmedType` structurally satisfies
`port.UserConfirmedTypeHandler`. `RepublishPauseEvents` is called by
LIC-TASK-040.

Constructor: `NewManager(Config, port.PendingStatePort,
port.IdempotencyStorePort, port.UncertaintyPublisherPort,
port.StatusPublisherPort, port.DLQPublisherPort, PipelineResumer, Deps)
(*Manager, error)` — `NewTypeName` (`feedback_constructors.md`),
fail-fast on invalid `Config` / any nil required collaborator via
`errors.Join` (the `pipeline.NewOrchestrator` precedent). Immutable
after construction; `Pause` / `HandleUserConfirmedType` /
`RepublishPauseEvents` are goroutine-safe for distinct version_ids
(no per-instance mutable state).

`Pause` returns the injected sentinel ONLY on full success; any step
failure returns a `*model.DomainError` (the orchestrator's
`classifyOutcome` routes that to `outcomeFailed`, NOT `outcomePaused`).
`HandleUserConfirmedType` return contract (mapped by LIC-TASK-040):
**nil ⇒ ACK**; non-nil `*model.DomainError` `Retryable=true` ⇒
**NACK→retry-DLX**; `Retryable=false` ⇒ **DLQ**.

## Files

- **manager.go** — package doc (hermetic statement; D1..D20
  attribution), `Config` (+`validate`), `Manager`, `NewManager`,
  `Pause` (§6.5:611-617 strict order, per-step failure contract — D4),
  `RepublishPauseEvents` (D7), `HandleUserConfirmedType` (SETNX + body
  — D7/D10), the private helpers (`buildPending`, `restoreState`,
  `publishPauseUncertain`(+`FromPending`), `publishPauseInProgress`
  (+`FromPending`), `publishFailed` — the Manager's own
  `IsPublishableToOrchestrator` gate, `publishInvalidDLQ`,
  `setUserConfirmedCompleted`, `auditLog` — D20).
- **seams.go** — `PipelineResumer` (D11, no noop — required).
  `Metrics`+`noopMetrics` (D14). `Clock`+`systemClock` (1-method —
  D16). `Logger`+`noopLogger` (Info/Warn/Error — D20). `TraceRestorer`+
  `noopTraceRestorer` (D13). `var _` after each noop pair (B-4).
- **deps.go** — `Deps{Metrics, Clock, Logger, TraceRestorer}` (the
  optional-with-noop bundle) + `withDefaults()` (the `pipeline.Deps`
  precedent). The 5 ports + `PipelineResumer` are positional
  `NewManager` params, NOT in `Deps`.
- **internal_test.go** — `TestHermeticImports` (allowlist EXACTLY
  `{model,port}`, active-fail forbidden set incl.
  `internal/application/pipeline` + all third-party — D17) +
  `TestGofmtClean` (`go/format`; the sandbox blocks `go fmt`).
- **manager_test.go** — full behavioural suite (Part D.A pins 1..15)
  with in-package fakes for every port + every seam; `-race` clean.
- **CLAUDE.md** — this file.

## API

- `NewManager(Config, port.{PendingState,IdempotencyStore,Uncertainty-
  Publisher,StatusPublisher,DLQPublisher}Port, PipelineResumer, Deps)
  (*Manager, error)`.
- `(*Manager) Pause(ctx, *model.PipelineState) error` — satisfies
  `pipeline.PauseController`.
- `(*Manager) HandleUserConfirmedType(ctx, port.UserConfirmedType)
  error` — satisfies `port.UserConfirmedTypeHandler`.
- `(*Manager) RepublishPauseEvents(ctx,
  *model.PendingTypeConfirmation) error` — called by LIC-TASK-040.
- `Config{PendingStateTTL(25h), UserConfirmedProcessingTTL(90s),
  CompletedTTL(24h), ConfidenceThreshold, PausedSentinel(=pipeline.
  ErrPipelinePaused)}`; `Deps{Metrics, Clock, Logger, TraceRestorer}`
  — every field optional (nil ⇒ noop).
- `PipelineResumer` / `Metrics` / `Clock` / `Logger` / `TraceRestorer`
  — the local seams.

## Reconciliations (build-spec PART E — DEFECT-style, recorded here authoritatively)

**R1 — Pause ACK ownership.** *Doc (high-arch §6.5:617, §6.10 Pause
step 6):* "ACK исходного сообщения dm.events.version-artifacts-ready"
is step 5 of the strict order. *Impl:* the application layer
(`Manager.Pause`) performs steps 1-4 (Redis + 2 broker-confirmed
publishes + SetPaused) then returns `pipeline.ErrPipelinePaused`; the
orchestrator's `classifyOutcome`→`outcomePaused` finalizer returns the
sentinel; **LIC-TASK-040 ACKs the source on
`pipeline.IsPaused(err)==true`**. *Why:* DEFECT-4 (binding, 036) — the
application layer has no broker handle and never calls `delivery.Ack`.
The doc's "LIC ACKs" is satisfied at the service level by 040 acting on
the typed sentinel. Identical to 036's nil⇒ACK / `*DomainError`⇒NACK
contract, extended with a third outcome.

**R2 — `PENDING_STATE_LOST` is not a catalog code.** *Doc (high-arch
§6.5:631):* `error_code=PENDING_STATE_LOST`. *Impl:* use
`model.ErrCodeUserConfirmationExpired` (`USER_CONFIRMATION_EXPIRED`)
for the pending-state-miss case (both §6.5:631 and §6.10:777). *Why:*
`PENDING_STATE_LOST` is absent from the frozen catalog
(`error_codes.go`/`AllErrorCodes()`); `USER_CONFIRMATION_EXPIRED` is
the semantically identical catalog row (devMessage literally "pending
classification confirmation TTL expired in Redis", error_codes.go:212),
non-retryable, `STAGE_AWAITING_USER_CONFIRMATION`. Inventing a wire
code is a frozen-contract change owned by the architecture team (the
036 "RU messages never inlined — frozen catalog SSOT" discipline). The
§6.5:631 branch is 040-owned; this reconciliation binds 040's
implementer to the same code.

**R3 — Pause may persist a zero TraceContext.** *Doc (high-arch
§6.10:754, pending.go:24-28):* `trace_context` carries W3C
traceparent/tracestate for resume span linkage. *Impl:* 037 copies
`st.TraceContext` verbatim into the blob; 036's `newState` zeroes it
(`st.TraceContext = model.TraceContext{}`, orchestrator.go) because
LIC-TASK-040 owns W3C extraction (036 forward-note #2). A zero context
is persisted as-is; the `TraceRestorer` noop/adapter treats `IsZero()`
as "no linkage". *Why:* the cross-pause trace link materializes only
once 040 populates `state.TraceContext` at ingress (its owned
responsibility, pending). 037 must not block on 040; a zero context
degrades to "resume without trace linkage" (telemetry, non-functional).
pending.go:24-28 explicitly sanctions a zero TraceContext as
forward-compatible.

**R4 — No env var for the 90s user-confirmed SETNX TTL.** *Doc
(high-arch §6.10 Resume step 2 / acceptance):* SETNX
`lic-user-confirmed:{version_id}` PROCESSING with 90s TTL;
lic-trigger=COMPLETED / lic-user-confirmed=COMPLETED with 24h. *Impl:*
`config.IdempotencyConfig` gains `LIC_USER_CONFIRMED_PROCESSING_TTL`
(default 90s, validated >0); `CompletedTTL` reuses
`IdempotencyConfig.TTL` (24h, existing); `PendingStateTTL` reuses
`PipelineConfig.PendingConfirmationTTL` (25h, existing). All three
reach the Manager via `pendingconfirmation.Config` (the `pipeline.
Config` 047-injection precedent). *Why:* the 90s value has no existing
env home (`IdempotencyConfig.ProcessingTTL=150s` is the lic-trigger
heartbeat TTL, a different key with a heartbeat extender — not the
unextended 90s user-confirmed window). Adding it to `IdempotencyConfig`
(its natural domain) is the minimal in-scope config change; 047 maps
config→local-Config exactly as for pipeline.

**R5 — Task acceptance omits §11.2 tenant check + audit trail; both are
mandatory.** *Doc (security.md §11.2 steps 2+4, "Mandatory defence не
safety net", closes F-8.1/TOP-4):* org_id mismatch ⇒ DLQ + alert +
pending-state NOT consumed; audit-trail for ALL receipts with
`validation_outcome ∈ {accepted, rejected_format, rejected_whitelist,
rejected_tenant_mismatch}`. *Impl (tasks.json acceptance lists only
regex+whitelist):* 037 IMPLEMENTS the full §11.2: the tenant check
(D10 step 3) and the audit log (D20, `Logger.Info`) are IN scope.
*Why:* security.md §11.2 is FROZEN normative SSOT and explicitly
elevates these from safety-net to mandatory; the task acceptance is a
non-exhaustive checklist, not a ceiling. Omitting them would ship a
documented security gap (forged-UserConfirmedType cross-tenant
injection).

**R6 — A retryable Pause-step failure publishes a transient
FAILED{INTERNAL_ERROR}.** *Doc:* §6.5 wants pause to be recoverable on
crash/redelivery. *Impl:* if Save succeeds but a later Pause step
(uncertain/IN_PROGRESS/SetPaused) fails, `Pause` returns
`*DomainError{INTERNAL_ERROR, retryable=true}`; the orchestrator's
`classifyOutcome`→`outcomeFailed`→`publishFailed` publishes
FAILED{INTERNAL_ERROR, is_retryable=true} (INTERNAL_ERROR is
publishable). *Why:* we do NOT invent a new "pause-in-progress" code
(frozen catalog). INTERNAL_ERROR/retryable is correct per
error-handling.md §3.7; the Orchestrator dedups by
`lic-status:{job_id}:{status}` (events.go:195) and 040 NACKs→retry-DLX
on retryable, so the redelivery completes the pause (or COMPLETED) and
the transient FAILED is superseded. The user-visible blip is the
acceptable cost of not extending the frozen error catalog.

**R7 — New Prometheus outcome label value `paused`.** *Doc
(observability.md:103/105, SSOT):* `lic_pipeline_outcome_total{outcome}
∈ {success, failed, timeout}`. *Impl:* the finalizer emits a 4th value
`"paused"` for a paused run (D3). *Why:* 036 was happy-path-only;
"paused" is a non-terminal, non-failure outcome that did not exist
then. It is bounded internal cardinality (+1 fixed value), NOT a
wire/event-contract change. The only alternatives — mislabel pause as
`success` (corrupts the success-rate SLO
`rate(...outcome="success") / rate(started)`, observability.md:272) or
as `failed` (false-alerts the failure SLO, observability.md:334) — are
strictly worse. Recorded so a future observability-consistency pass
treats `paused` as the deliberate 4th value, and so the LIC-TASK-047
metrics adapter / dashboards add it. This is a recorded deviation, not
a silent one (the 036 DEFECT-discipline).

## Conventions & deliberate decisions (build-spec D1..D20, condensed)

- **D1 — one `*Manager`, two interface roles.** `Pause` (structurally
  `pipeline.PauseController`) + `HandleUserConfirmedType` (structurally
  `port.UserConfirmedTypeHandler`) + `RepublishPauseEvents` (040-called)
  around one Redis namespace + one metric family. The two halves are
  one state machine; splitting them across types would fragment the
  idempotency invariants. No interface re-declaration here.
- **D2/D9/D19 — resume drives Stage 2..21 via
  `pipeline.Orchestrator.ResumeAfterConfirmation`** (Option A): the
  orchestrator extracted `continueFromStage2` and added the resume
  entrypoint (re-Acquires a JobLimiter slot — D9; fresh
  `WithTimeout`; IN_PROGRESS{STAGE_AGENT_PARTY_CONSISTENCY} — D19).
  The Manager invokes it via the `PipelineResumer` seam, never
  importing `pipeline`.
- **D3/D5 — paused sentinel.** `pipeline.ErrPipelinePaused` +
  `pipeline.IsPaused`. The Manager returns it via
  `Config.PausedSentinel error` (047 injects the exact value);
  `errors.Is` works because it is the same value. `NewManager`
  fail-fast if `PausedSentinel == nil`.
- **D4 — Pause strict order + per-step failure contract.** Full
  success → sentinel. Save fails → `*DomainError{INTERNAL_ERROR,
  non-retryable, STAGE_AWAITING_USER_CONFIRMATION}` (before the first
  stable point, no cleanup). Steps 3/4/5 fail → same code but
  `Retryable=true` (pending-state saved ⇒ recoverable). `PendingState-
  Inc` immediately after a successful `Save` (the gauge mirrors Redis,
  not whole-sequence success).
- **D6 — `ClassificationUncertain.Alternatives` = `st.Classification.
  Alternatives`** verbatim; `SuggestedType`/`Confidence` from the
  classification; `Threshold` from `cfg.ConfidenceThreshold` (a 047
  wiring invariant: it MUST equal `pipeline.Config.ConfidenceThreshold`
  — same `LIC_CONFIDENCE_THRESHOLD`).
- **D7/D10 — Resume idempotency + body.** SETNX
  lic-user-confirmed(90s): absent⇒proceed; PROCESSING/PAUSED⇒retryable
  NACK; COMPLETED⇒nil; Redis-down⇒`IDEMPOTENCY_STORE_UNAVAILABLE`
  retryable NACK (non-publishable — not surfaced). Body:
  validate-first (regex then 12-whitelist) → SETNX → Load
  (`ErrPendingStateNotFound`⇒`USER_CONFIRMATION_EXPIRED`+SetCompleted+
  ACK, NO DLQ) → tenant check → restore → override
  (ContractType=cmd, Confidence=1.0; nil Classification ⇒ non-retryable
  INTERNAL_ERROR ⇒ DLQ) → trace restore → drive resumer → on
  nil:§6.10-step-9 cleanup (Delete + 2×SetCompleted + Dec + "resumed");
  on err: return verbatim, NO cleanup, NO re-publish, NO increment.
- **D8 — RE_CHECK parent re-fetch on resume** (degrade-never-fail,
  `:parent:resume` suffix) lives in `pipeline.reCheckParentRefetchFor-
  Resume`, NOT here; the pending blob deliberately does not carry
  `ParentRiskAnalysis` (anti-carrier — DM is authoritative 25h later).
- **D11/D18 — local `PipelineResumer` seam, required (no noop);
  `var _` assertions in the WIRING package, NOT here** (asserting
  `pipeline.PauseController` here would force the forbidden import).
- **D12/D17 — local `Config` (no internal/config import); hermetic
  allowlist EXACTLY `{model,port}`.** No `internal/application/
  pipeline`, no `internal/infra/*`. Active-fail forbidden set incl.
  `internal/application/pipeline` + prometheus/otel/redis.
- **D13/D14/D16/D20 — seams.** `TraceRestorer` (noop ⇒ ctx unchanged);
  `Metrics` (per-call Inc/Dec, never `PendingStateAgeMaxSeconds` from
  the Manager — no `CreatedAt` source, anti-carrier; outcome enum
  EXACTLY `{resumed,expired,invalid}`); 1-method `Clock`; `Logger` with
  a first-class `Info` for the mandatory §11.2 audit trail.
- **RU user messages.** Never inlined — `model.NewDomainError` /
  `model.LookupErrorSpec` pull them from the frozen catalog
  (`USER_CONFIRMATION_EXPIRED` / `INVALID_CONTRACT_TYPE`). No new
  `model.ErrorCode` invented (R2/R6).
- **gofmt self-check** via `go/format` (sandbox blocks `go fmt`) — the
  pipeline/aggregator/stages precedent.

## Forward notes (recorded, owners elsewhere)

1. **LIC-TASK-047 (app-wiring).** `NewManager(pendingconfirmation.
   Config{PendingStateTTL: cfg.Pipeline.PendingConfirmationTTL,
   UserConfirmedProcessingTTL: cfg.Idempotency.UserConfirmed-
   ProcessingTTL, CompletedTTL: cfg.Idempotency.TTL, ConfidenceThreshold:
   cfg.Scoring.ConfidenceThreshold (MUST equal pipeline.Config.
   ConfidenceThreshold — the D6 wiring invariant), PausedSentinel:
   pipeline.ErrPipelinePaused}, redisPendingAdapter, redisIdem-
   Adapter, uncertainPubAdapter, statusPubAdapter, dlqAdapter,
   theOrchestrator (structurally satisfies `PipelineResumer` via
   `ResumeAfterConfirmation`), Deps{Metrics: pendingMetricsAdapter
   over *metrics.PendingMetrics, Clock: systemClock, Logger:
   loggerAdapter (with Info), TraceRestorer: traceRestoreAdapter over
   tracer.Tracer.ExtractFromHeaders})`. Assert
   `var _ pipeline.PauseController = (*Manager)(nil)`,
   `var _ port.UserConfirmedTypeHandler = (*Manager)(nil)` and
   `var _ pendingconfirmation.PipelineResumer =
   (*pipeline.Orchestrator)(nil)` in the WIRING package, NOT here
   (D18, the aggregator `Repairer`-seam / pipeline `JobLimiter`-on-
   `*concurrency.Semaphore` precedent). The LIC-TASK-047 metrics
   adapter MAY additionally attach a periodic Redis-SCAN-based
   `lic_pending_state_count` / `lic_pending_state_age_seconds_max`
   refresher (the Manager's per-call Inc/Dec is an approximation that
   does NOT decrement on 25h-TTL natural expiry without a resume — D14)
   and MUST add the `lic_pipeline_outcome_total{outcome="paused"}`
   series + dashboard (R7). Also wire the `pipeline.Deps.Pause-
   Controller` field to this `*Manager`.
2. **LIC-TASK-040 (Event Router/consumer + broker ACK).** Owns the
   RabbitMQ consumer, the `lic-trigger:{version_id}` idempotency guard
   & restart-semantics ingress (§6.5:624-634), the W3C trace-context
   extraction into `state.TraceContext` at ingress (R3 — until then
   resume degrades to no cross-pause trace linkage), and the broker
   ACK/NACK decision. For `orch.commands.user-confirmed-type`: call
   `manager.HandleUserConfirmedType`; `pipeline.IsPaused(err)==true` is
   NOT applicable here (that is the version-artifacts-ready path) —
   map nil ⇒ ACK, `model.IsRetryable==true` ⇒ NACK→retry-DLX, false ⇒
   DLQ (R1). For a redelivered `dm.events.version-artifacts-ready` that
   hits `lic-trigger=PAUSED` with `lic-pending-state` present: after
   040's own lic-trigger guard + the pending-state Load, call
   `manager.RepublishPauseEvents(ctx, decodedState)` (Stage 1 NOT
   restarted, §6.5:631); on a pending-state MISS in that path 040
   publishes FAILED{`model.ErrCodeUserConfirmationExpired`} itself
   (R2 binds 040 to that exact code). For the version-artifacts-ready
   pipeline path: on `pipeline.IsPaused(orch.Run(...))==true` ACK the
   source WITHOUT COMPLETED/FAILED (R1).
3. **v1 SSOT gaps recorded (R2/R4/R6/R7).** `PENDING_STATE_LOST` →
   `USER_CONFIRMATION_EXPIRED` (R2); the 90s SETNX TTL env var added to
   `IdempotencyConfig` (R4); a transient pause-step FAILED{INTERNAL_-
   ERROR,retryable} is the accepted cost of not extending the frozen
   catalog (R6); `lic_pipeline_outcome_total{outcome="paused"}` is the
   deliberate 4th label value (R7). None of these are silent — a future
   architecture/observability-consistency pass must treat them as
   recorded deviations, not regressions.
