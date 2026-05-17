# stages Package — CLAUDE.md

**Stage Executor** (LIC-TASK-034, `high-architecture.md` §4.3.1 / §8.1,
`ai-agents-pipeline.md` §0.1 / §"errgroup для параллельных стадий",
`error-handling.md` §7.3 / §8, `observability.md` §3.2 / §4.2). The thin
orchestrator that runs the six LIC pipeline stages over the nine AI agents:

```
RunStage1  Type Classifier ‖ Key Parameters Extractor   parallel, both must succeed
RunStage2  Party Data Consistency                        sequential, NON-CRITICAL
RunStage3  Mandatory Conditions ‖ Risk Detection         parallel, both must succeed
RunStage4  Recommendation                                sequential, must succeed
RunStage5  Business Summary ‖ Detailed Report            parallel, both must succeed
RunStage6  Risk Delta                                    sequential, RE_CHECK only, NON-CRITICAL
```

Constructor: `NewExecutor(agents map[model.AgentID]port.Agent, deps Deps)
(*Executor, error)` — `NewTypeName` (`feedback_constructors.md`), fail-fast
(the `base.NewBaseAgent` / `NewAggregator` precedent). NOT a `port.Agent`.

## Files

- **stages.go** — package doc, `Executor`, `NewExecutor`, `Deps`,
  `canonicalStage` (model.AgentID→model.Stage, D4), `buildInput` (D3),
  `assign`+`resultMismatch` (D4), `runAgent`, `runParallel` /
  `runSequentialCritical` / `runNonCritical`, `isAgentTimeout` (D6),
  `RunStage1..RunStage6`.
- **stage.go** — local closed 6-value `Stage` enum (D5).
- **parallel.go** — the stdlib-only `parallel()` errgroup.WithContext
  equivalent (D1 Option A, B-1/B-2).
- **seams.go** — `StageMetrics` / `Tracer`+`StageSpan` seams + noop
  defaults + `var _` assertions (B-4).
- **internal_test.go** — `TestHermeticImports` (model+port allowlist,
  active-fail config/infra/agents/aggregator AND any third-party
  incl. golang.org/x/sync) + `TestGofmtClean`.
- **stages_test.go** / **parallel_test.go** — full suites.
- **CLAUDE.md** — this file.

## API

- `NewExecutor(map[model.AgentID]port.Agent, Deps) (*Executor, error)`.
- `(*Executor) RunStage1..RunStage6(ctx, *model.PipelineState) error`.
- `Stage` (`s1.parallel`..`s6.risk_delta`), `Stage.String/IsValid`,
  `AllStages()`.

## Conventions & deliberate decisions (subagent code-architect D1–D8)

- **D1 Option A — errgroup SSOT reconciliation (offline-build
  constraint).** *The SSOT says:* `ai-agents-pipeline.md:38` and
  `:1681-1703` illustrate parallel stages with
  `errgroup.WithContext`; `high-architecture.md` ADR-LIC-01 says
  "errgroup for parallel stages". These name errgroup as the
  **reference realization of a semantic contract** (`:1703` — "При
  ошибке в одной из goroutines — gctx отменяется, остальные goroutines
  прерывают LLM-вызовы"), NOT a frozen byte artifact (contrast: the
  schemavalidator §5.2 repair-prompt bytes). *The environment forbids
  it:* `golang.org/x/sync` is absent from `go.mod`/`go.sum` (verified,
  incl. transitively), the build sandbox has no network / no module
  cache, and the task acceptance requires `make build/test/lint` to
  pass — an unresolvable import fails the whole module build. *The
  resolution (authoritative):* `parallel.go`'s unexported,
  stdlib-only `parallel(ctx, fns...) error` is a deliberate minimal
  errgroup.WithContext equivalent, byte-for-byte SEMANTICALLY:
  first-non-nil-error-wins, derived-context cancellation aborting
  sibling in-flight `agent.Run`/`router.Complete`, verbatim
  (unwrapped) error return so `errors.As(*model.DomainError)` survives
  the join. This is *more* hermetic than vendoring (Option B,
  rejected) — it keeps the Stage Executor fully hermetic and
  `schemavalidator` the codebase's single non-hermetic exception. Same
  reconciliation class as the schemavalidator PriorTurns / LIC-TASK-019
  / aggregator SSOT-gap resolutions: the architecture docs are NOT
  edited (frozen SSOT stays frozen); **this CLAUDE.md is the
  authoritative record**. Where sibling CLAUDE.mds say "errgroup" in
  prose (e.g. `schemavalidator/CLAUDE.md`) it denotes the *semantic*
  contract, realized in-house — no retro-edit. Pinned by
  `TestParallel_FirstErrorWins` / `_SiblingCancellation` /
  `_DomainErrorPropagatesVerbatim` / `_AllSucceed_NilAndParentCtxAlive`
  / `_NoGoroutineLeak` + the `-race` `TestRunStages_ConcurrentRaceClean`.
- **D2 — NO aggregator call.** RunStage4 just runs Agent 6 reading
  `state.MergedRiskAnalysis`. The Pipeline Orchestrator
  (LIC-TASK-036) runs `aggregator.Aggregate` between RunStage3 and
  RunStage4 and assigns `Output.MergedRiskAnalysis →
  state.MergedRiskAnalysis` (tasks.json LIC-TASK-036 acceptance;
  `aggregator/CLAUDE.md` FN-1). `internal/application/aggregator` is on
  the `TestHermeticImports` forbidden list — enforced, not convention.
- **D3 — per-stage shallow `buildInput` snapshot.** Called ONCE at the
  top of each `RunStageN`, BEFORE launching goroutines: an immutable
  value shared read-only across the parallel goroutines, each writing
  its OWN disjoint `state.*` field. Per-stage (NOT per-pipeline) is
  load-bearing — Stages 4/5/6 must observe `state.MergedRiskAnalysis`
  as written by 036 between stages. Shallow, NOT deep: agents treat
  `AgentInput` strictly read-only (base immutability; every Spec
  stateless; aggregator D2/D5). `ParentVersionID`/`ParentRiskAnalysis`
  copied unconditionally (`riskdelta` D3/FN-4 — Agent 9's hard-required
  `base_version_id` has no other source); the RunStage6 gate, not
  `buildInput`, decides whether Agent 9 runs. Field-name asymmetry:
  `PipelineState.InputArtifacts` → `AgentInput.Artifacts`.
- **D4 — per-AgentID dispatch; `DomainError.Stage` uses
  `model.Stage`.** `assign` narrows `port.AgentResult` to the concrete
  type (`port/agents.go:45-49`) keyed by AgentID and writes the
  matching `state.*` field. `AgentRecommendation` is the lone
  VALUE-type (`model.Recommendations`, a slice), not a pointer. A
  type/AgentID mismatch is a defensively-guarded LIC build defect →
  `INTERNAL_ERROR` carrying the agent's **canonical `model.Stage`**
  (`canonicalStage`, the `base` N3 precedent) — NOT the local `Stage`
  type — plus the `agent_id` attribute (base MF-2/N1). The two stage
  enums have disjoint SSOTs (see D5).
- **D5 — local `Stage` enum == observability.md §4.2 span suffixes.**
  Closed 6-value typed enum; `String()` = the `observability.md:213-231`
  span-tree suffixes VERBATIM (`s1.parallel`, `s2.party_consistency`,
  `s3.parallel`, `s4.recommendation`, `s5.parallel`, `s6.risk_delta`).
  Used as BOTH the `lic_pipeline_stage_duration_seconds{stage}` label
  AND the `"lic.stage."+suffix` span name (single closed
  low-cardinality set). The `metrics/pipeline.go:24-25` comment ("Stage
  label values come from status.go STAGE_*") is non-normative,
  UNWORKABLE (no group-level `model.Stage` for parallel 2-agent stages)
  and loses to `observability.md:105` ("при расхождении приоритет за
  observability.md"). DISTINCT from `model.Stage` (status.go SSOT for
  `status-changed`/`DomainError`); never interchanged. Pinned by
  `TestStageStrings_PinnedToObservabilitySpanTree`.
- **D6 — non-critical degradation gated on AGENT_TIMEOUT ONLY (the
  decisive code-architect reversal).** `error-handling.md:304` +
  `ai-agents-pipeline.md:1665` gate the non-critical (Agent 3 / 9)
  skip on "timed out", not any failure; the schemavalidator 1-shot
  repair inside `base.BaseAgent` already absorbs transient invalid
  output, so a post-repair `AGENT_OUTPUT_INVALID` (or any non-timeout
  error) is a genuine retryable failure that MUST fail the pipeline
  (`ai-agents-pipeline.md:1673` — for contract analytics a low-quality
  result is worse than a retryable error; `error-handling.md:311`
  "degradation does not fill with fake data"). `isAgentTimeout` is a
  typed `model.ErrCodeAgentTimeout` comparison via
  `model.AsDomainError`, never a string match (the `base` S4 decisive-
  check discipline). On timeout: result field stays nil (skip), a span
  `Degraded` event records the un-lossy truth, pipeline continues
  (`err==nil`). No new `model.Warnings` code (closed-5, contract-frozen;
  the Agent-8-D7/Agent-9-D3 anti-carrier line); no new metric — the
  base-emitted `lic_agent_invocations_total{agent,outcome=timeout}` IS
  the §8.2-3 degradation metric. The §8.2-1 "warning in
  DETAILED_REPORT.warnings" obligation for the missing-party case has
  **no closed-set code in v1** — a known v1 SSOT gap owned by
  LIC-TASK-035/036 (forward note 3). Pinned by
  `TestRunStage2_Timeout_Degrades` / `_NonTimeout_Propagates`.
- **D7 — RunStage6 gate; emit-nothing-when-gated.** Runs Agent 9 only
  when `state.Mode==RE_CHECK && state.ParentRiskAnalysis != nil`
  (`riskdelta` FN-2). Gated out ⇒ `return nil` immediately, NO span,
  NO stage-duration sample (observability.md §4.2 marks s6 "(опц.)" —
  no zero-duration pollution on every INITIAL run). When it runs, the
  D6 timeout-vs-fatal discrimination applies identically (Agent 9 is
  non-critical, bracketed with Agent 3). The
  `RE_CHECK_PARENT_ANALYSIS_MISSING` warning + outbound
  `risk_delta=null` are the Aggregator/Orchestrator's job
  (`aggregator` D4 / `riskdelta` FN-2), NOT this task. Pinned by
  `TestRunStage6_GatedOut_EmitsNothing` / `_RunsWhenGateMet` /
  `_Timeout_Degrades` / `_NonTimeout_Propagates`.
- **D8 — no per-stage timeout.** The Executor adds NO
  `context.WithTimeout`: parallel stages derive their context solely
  via `parallel()` (errgroup-equivalent sibling-cancellation);
  sequential stages pass `ctx` through. The per-agent deadline is
  `base.BaseAgent`'s job; the 90s job deadline is the Orchestrator's
  (036). A redundant per-stage timer would mask base's precise
  per-agent `AGENT_TIMEOUT` (load-bearing for D6/D7) and introduce a
  no-SSOT magic constant. `error-handling.md:283`'s "stage ctx +
  per-stage budget" is realised as the errgroup-derived cancellation
  scope with NO independent budget (no doc supplies one) — recorded
  reconciliation, not silently chosen.
- **Concurrency.** `*Executor` is immutable after `NewExecutor` (which
  defensively copies the registry); shared across concurrent jobs. The
  `pipeline_state.go:32-35` disjoint-field-write invariant is
  empirically pinned (`TestRunStages_ConcurrentRaceClean`, `-race`,
  ×32), not assumed: `buildInput` reads `state` strictly before any
  `g.Go`; goroutine writes are published at the `parallel()` join. A
  future "optimisation" moving `buildInput` inside a goroutine would
  introduce a data race — do not.
- **Fail-fast wiring (`NewExecutor`).** Rejects empty map / missing
  agent / nil agent / `agent.ID() != key` / extra entries — a
  LIC-TASK-047 wiring defect is a startup error, not a first-job panic
  (the `base.canonicalStage` N3 class). Defensively copies the map.
- **gofmt self-check** via `go/format` (sandbox blocks `go fmt`) — the
  aggregator/schemavalidator precedent.

## Forward notes (recorded, owners elsewhere)

1. **LIC-TASK-036 (Pipeline Orchestrator) — the single most important
   integration invariant.** 036 owns the job-level
   `context.WithTimeout(LIC_JOB_TIMEOUT=90s)`, the stage SEQUENCING,
   and the aggregator call. It MUST: assign each stage's agent results
   are already written by 034 into `state.*`; run `aggregator.Aggregate`
   after `RunStage3` returns and assign `Output.MergedRiskAnalysis →
   state.MergedRiskAnalysis` BEFORE `RunStage4`; assign upstream
   results into `state.*` between stages so each per-stage `buildInput`
   snapshot sees them. If 036 violates the post-Stage-3 merge contract,
   Agents 6/8/9 will (correctly) fail with `INTERNAL_ERROR` — their
   pinned pipeline-ordering breach, NOT a 034 defect. 036 also owns the
   confidence-check pause between RunStage1 and RunStage2, the
   `lic.events.status-changed` mapping of the propagated
   `*model.DomainError`, structured WARN logging of a Stage-2/6
   degradation (034 is hermetic — no logger), and nulling outbound
   `risk_delta` + the `RE_CHECK_PARENT_ANALYSIS_MISSING` warning.
2. **LIC-TASK-047 (app-wiring).** Construct the 9 concrete agents
   (`NewTypeClassifier`/.../`NewRiskDeltaComparator`, each `base.Deps`)
   and pass `map[model.AgentID]port.Agent`. Wire `Deps.Metrics` =
   adapter over `*metrics.PipelineMetrics` (`StageDuration` →
   `StageDurationSeconds.WithLabelValues(stage).Observe(s)`), `Deps.Tracer`
   = adapter over `*tracer.Tracer` (`StartStage` → span
   `"lic.stage."+name`; `StageSpan.Degraded` → span event;
   `StageSpan.Finish` → `RecordError`+`End`). The adapter MUST map the
   local 6-value `Stage` set, **NOT `model.Stage` STAGE_***; the
   `metrics/pipeline.go:24-25` comment is stale-on-conflict and should
   be corrected there (or in a metrics-doc task) to point at the
   observability.md §4.2 6-value set (D5 reconciliation).
3. **v1 SSOT gap — Agent-3/9 skip has no DETAILED_REPORT.warnings
   code.** `error-handling.md:8.2-1` says every degradation gets a
   user-facing warning, but `model.Warnings` is the contract-frozen
   closed-5 set with no agent-skip code. v1 surfaces an Agent-3 timeout
   only via the trace `Degraded` event + the base invocation metric;
   user transparency is preserved because Agent 8 tolerates nil
   `PartyConsistency` (`detailedreport` D4/FN-4). Widening the warning
   set is a contract change owned by LIC-TASK-035/036 + the
   architecture team — do NOT invent a code here (the Agent-7/8 "no
   scope creep" precedent).
