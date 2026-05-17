# riskdelta Package — CLAUDE.md

**Agent 9 — Risk Delta** (LIC-TASK-033, `ai-agents-pipeline.md`
§9, `high-architecture.md` §6.6/§8.7). From the parent (base) version's
**MERGED** risk analysis and the current (target) version's **MERGED**
risk analysis — plus the two version UUIDs — it produces ONE
`model.RiskDelta`: risks `added` / `removed` / level-`changed` since
the parent, the before/after `profile_change` counts, and a short
Russian `summary`. The **9th and LAST** of the 9 per-agent packages.
**Stage 6, RE_CHECK only**, gated by the §8.7 RE_CHECK gate
(`parent_version_id != null` AND parent `RISK_ANALYSIS` retrieved).

**Non-critical** (`error-handling.md` groups agents 3 & 9): a timeout /
failure does NOT fail the pipeline — it continues with
`risk_delta=null` + a `RE_CHECK_PARENT_ANALYSIS_MISSING` warning. That
graceful degradation and the run/skip gate are owned by the **Stage
Executor (LIC-TASK-034)** / **Pipeline Orchestrator (LIC-TASK-036)**,
NOT this package (forward note 2).

A **thin** wrapper over the shared BaseAgent runner (LIC-TASK-024):
`RiskDeltaComparator` embeds `*base.BaseAgent` (so `ID()`/`Run()` make
it a `port.Agent` for free); the only per-agent code is the `Spec` —
`Parts` (envelope) + `Decode` (typed result). The invariant-heavy loop
lives in `base`, once.

Constructor: `NewRiskDeltaComparator(modelID string, timeout
time.Duration, deps base.Deps) (*RiskDeltaComparator, error)` —
stutter-free `NewTypeName` (`feedback_constructors.md`; never bare
`New`, never the package-and-model-stuttering `NewRiskDelta`);
fail-fast via `base.NewBaseAgent`.

## Files

- **riskdelta.go** — package doc, `RiskDeltaComparator`,
  `NewRiskDeltaComparator`, the §9 budget consts
  (`maxOutputTokens=1500`, **`temperature=0.0`**), the `var _
  port.Agent` assertion, the base/router `Config.Model`-fallback
  forward note (shared with the prior 8) + the §9-vs-§4.3.x timeout
  doc-conflict note (forward note 5).
- **spec.go** — `riskDeltaComparatorSpec` (stateless), `Parts` (the
  §9 4-block envelope), `Decode` (pure typed-unmarshal + closed-enum
  RiskLevel drift-guard).
- **internal_test.go** — `TestHermeticImports` (the **6-entry,
  artifacts-FREE** allowlist — the purest non-DM-artifact agent) +
  `TestGofmtClean`.
- **riskdelta_test.go** — constructor, fail-fast, the 4-block envelope
  order (no `<semantic_tree>`/`<contract_document>`), version-ids
  verbatim, whole-MERGED base/target analyses, empty-`Risks[]`
  tolerance, the **strictness errors incl. the load-bearing
  nil-merged/non-nil-raw RiskAnalysis subtest** (D2's pin),
  layer-2 escaping, `Decode` (valid; the RiskLevel drift-guard on
  every guarded surface incl. `profile_change` only-when-non-nil;
  absent-profile & null clause-refs decode clean; **no input
  cross-check / no UUID guard**), `Run` integration (acceptance Шаг 1),
  **`TestRun_ProfileChangeCountsMatchInput`** (acceptance Шаг 2),
  **`TestRun_InvalidLevel_RepairTriggered`**, `-race`.

## API

- `NewRiskDeltaComparator(modelID, timeout, base.Deps)
  (*RiskDeltaComparator, error)`.
- `(*RiskDeltaComparator)` satisfies `port.Agent` via the embedded
  `*base.BaseAgent` (`ID()==AGENT_RISK_DELTA`; `Run` returns
  `*model.RiskDelta` or a `*model.DomainError`).

## Conventions & deliberate decisions (code-architect design review D1–D9)

- **D1 — `base_risk_analysis` ← `in.ParentRiskAnalysis`, MANDATORY.**
  The parent-version `RISK_ANALYSIS` retrieved from DM, marshalled
  WHOLE. A nil here = the §8.7 RE_CHECK gate failed to skip Agent 9 →
  wiring defect → base `INTERNAL_ERROR` (forward note 2 — graceful
  degradation is the gate/orchestrator's job, NOT Agent 9's). Empty
  `Risks[]` (clean parent) tolerated, verbatim.
- **D2 — `target_risk_analysis` ← `in.MergedRiskAnalysis` (RAW
  NEVER), the LOAD-BEARING decision; the Agent-6/8 MERGED class, NOT
  the Agent-7 RAW class.** The driver is the **immutable** shape of
  the parent artifact: DM only ever persisted the **MERGED**, published
  `RISK_ANALYSIS` (`high-architecture.md` §6.11/§6.11.1/§4.3.3 — the
  Result Aggregator is the sole producer; Agent-5 `R-NNN` + folded
  Agent-3 `R-PNNN` + Agent-4 `R-MNNN`, rationale stripped; **no raw
  Agent-5 artifact is stored anywhere**). So `in.ParentRiskAnalysis`
  is unconditionally merged; §9's matching ("по семантической
  эквивалентности") and the `profile_change` EXACT counts require an
  apples-to-apples pairing — a RAW target would structurally guarantee
  every Agent-3/4-derived parent risk to be spuriously `removed`. Stage
  6 runs AFTER the LIC-TASK-035 merge, so `in.MergedRiskAnalysis` is
  available. This **OVERRIDES** the bare §9 "Зависимости" wording
  ("RiskAnalysis текущей версии") and the illustrative R-001..R-004
  prompt example exactly as Agent 8's D1 overrode the identically-bare
  §8 wording (the prompt example ids are illustrative — the ratified
  "prompt is SSOT, illustration form is not"). The nil-check is on
  `in.MergedRiskAnalysis`; a non-nil raw `in.RiskAnalysis` does **NOT**
  satisfy it (the Stage Executor must wire the MERGED field — forward
  note 3). Pinned by the **`nil merged but non-nil raw RiskAnalysis`**
  subtest (the Agent-6/8 pin, inverse of `summary`'s RAW). A reviewer
  must NOT "normalise" this to RAW to match Agent 7.
- **D3 — add `model.AgentInput.ParentVersionID *string`; `base_-
  version_id` ← `*in.ParentVersionID`, `target_version_id` ←
  `in.VersionID`.** `base_version_id` is a **hard-`required`** §9
  schema field (`risk_delta.json:6`) and §9 criterion 2, produced
  ONLY by Agent 9 (no downstream owner — the Result Aggregator passes
  `RiskDelta` through). `AgentInput` had **no** parent-version-id
  source (verified: not in correlation ids, not in `Artifacts`, not in
  `RiskAnalysis`). The additive optional `ParentVersionID *string
  \`json:"parent_version_id,omitempty"\`` is the **symmetric
  counterpart** of the already-present `ParentRiskAnalysis` ("populated
  only in RE_CHECK mode for Agent 9"); the Stage Executor copies it
  from `PipelineState.ParentVersionID` (which already exists). This is
  the **uniquely justified** `AgentInput` extension across the 9
  agents and does **NOT** violate Agent 8's D7 anti-carrier stance:
  D7's `re_check_meta` was *de-scoped* (Agent 8 had a safe default and
  no required field); here the field is hard-required with no other
  source and no default. `agent_input_test.go`
  `TestAgentInput_OmitsUnsetFields` updated (the omitempty contract
  pin — must-fix). Forward note 4.
- **D4 — strictness phase ENVELOPE-MIRROR (the recommendation/summary
  **CC-4** class, NOT the detailedreport **CC-1** grouped-by-class
  divergence).** CC-1 grouping was justified only because Agent 8 had
  FOUR classes incl. a genuinely OPTIONAL one; Agent 9 has exactly
  **TWO** classes, **BOTH MANDATORY**, **ZERO optional/tolerated**
  blocks (the fixed 4-block §9 shape, no "(если есть)" hedge) — the
  textbook CC-4 case ("envelope-mirroring is valid only when all
  blocks are mandatory across ≤2 classes"). Strictness checks run in
  envelope order: `ParentVersionID`(nil/empty) → `VersionID`(empty) →
  `ParentRiskAnalysis`(nil) → `MergedRiskAnalysis`(nil). The two
  CONCEPTUAL classes (for the forward-notes only): **Class A** —
  RE_CHECK-GATE `{base_version_id, base_risk_analysis}` (forward note
  2); **Class B** — PIPELINE-ORDERING `{target_version_id,
  target_risk_analysis}` (forward note 3). Empty `Risks[]` in either
  analysis tolerated, marshalled verbatim. A reviewer must NOT regroup
  to detailedreport CC-1.
- **D5 — `Decode` = pure typed-unmarshal + closed-enum drift-guard, NO
  transform, NO input cross-check.** base schema-validates against the
  embedded `risk_delta.json` BEFORE `Decode` (MF-1). Guards **every**
  `model.RiskLevel` surface whose Go whitelist (`derived.go`:
  `{high,medium,low}`) EXACTLY equals the §9 schema enum (the
  detailedreport-D9 "guard EVERY exactly-equal closed-enum surface,
  NOT a one-guard quota" rule): `added[i].level`, `removed[i].level`
  (`RiskRef.Level`), `changed[i].old_level`/`new_level`, and
  `profile_change.old_overall_level`/`new_overall_level` — the last
  pair **only when `ProfileChange != nil`** (`*RiskProfileChange`
  omitempty, NOT in §9 `required`; nil is the legitimate "no profile
  change" — guarding it would wrongly reject schema-valid output, the
  detailedreport nullable boundary). Since the enum IS in
  `risk_delta.json`, an out-of-enum value is schema-INVALID ⇒ base
  step-7 fires the sticky 1-shot **repair** (the Agent-4/5/8
  repair-triggered class, NOT the Agent-6 terminal class — pinned by
  `TestRun_InvalidLevel_RepairTriggered`); the Decode guards are the
  build-defect BACKSTOP for schema↔model drift. Deliberately **NOT**
  guarded (the enumerated house-style written record):
  `BaseVersionID`/`TargetVersionID` — schema `format:uuid` is draft-07
  annotation-only (NOT asserted by the validator → a non-UUID is
  schema-VALID); **NO `in`-cross-check** that the echoed ids equal the
  inputs — structurally impossible (`Decode([]byte)` has no `in`; base
  is not modified by per-agent tasks) AND it is §9 criterion-2
  SEMANTIC correctness owned by the repair loop, not a drift-guard
  (the recommendation orphan-not-guarded / summary no-guard
  precedent); `Description`/`ClauseRef`/`Explanation`/`Summary` + the
  nullable `OldClauseRef`/`NewClauseRef *string` (free strings, schema
  maxLength/nullable only); `added`/`removed`/`changed` membership
  correctness (§9 criterion 4/5 SEMANTIC LLM judgement — Decode has no
  access to the input analyses anyway). Pinned: absent-profile & null
  clause-refs decode clean; non-UUID/non-matching ids NOT guarded.
- **D6 — §9 budget SSOT.** `MaxTokens=1500`, **`Temperature=0.0`**
  from `ai-agents-pipeline.md` §9 "Бюджеты и параметры LLM" + the
  acceptance criteria. Agent 9 is a deterministic comparison/diff pass
  (not phrasing-variety) — `0.0` like Agents 1-5/8, UNLIKE the
  wording-variety Agents 6 (0.2)/7 (0.3); a reviewer must NOT carry
  forward a non-zero temp. `base.NewBaseAgent` validates `Temperature
  ∈ [0,1]` so `0.0` passes. Provider=Claude primary
  (`LIC_AGENT_RISK_DELTA_PROVIDER`); timeout supplied by wiring from
  `config.AgentsConfig.Timeouts[config.AgentRiskDelta]` (default
  **8s**, `config/agents.go:59`) — never hard-coded;
  `Stage=model.StageAgentRiskDelta` (the `base.canonicalStage` pair
  is already present — `NewBaseAgent` fail-fast enforces it).
- **D7 — Hermetic, 6-entry, artifacts-FREE — the PUREST
  non-DM-artifact agent.** Imports ONLY stdlib +
  `internal/domain/{model,port}` + `base` + `promptbuilder` (for
  `Content` only — Agent 9 mints **no** structural block) + `prompts`
  + `schemas`. Agent 9 consumes **ZERO** DM artifacts (§9
  "Зависимости" `ai-agents-pipeline.md:1510-1511`: input is only the
  two `RiskAnalysis` structs + the two version ids — no
  `SEMANTIC_TREE`/`EXTRACTED_TEXT`/`PROCESSING_WARNINGS`, even more so
  than Agent 8). `internal/agents/artifacts` **deliberately DROPPED**
  — the "non-artifact-consumer" class, NOT an omission; a reviewer
  must NOT re-add it (the `riskdetection` "deliberate absence is a
  class" house style; `TestHermeticImports` actively fails if it
  appears). **No `internal/config`** (resolved values are constructor
  params — LIC-TASK-047). **No `DocumentProcessing` module**.
- **D8 — envelope order = the §9 prompt (`risk_delta.txt:26-31`).**
  `Parts` returns exactly the 4 blocks `base_version_id` →
  `target_version_id` → `base_risk_analysis` → `target_risk_analysis`,
  all via `promptbuilder.Content` (escaped — layer-2; **NON-NEGOTIABLE**
  — `base_risk_analysis`/`target_risk_analysis` carry
  upstream-LLM-derived free text in `description`/`clause_ref`, a prime
  injection vector; the version ids are LIC-controlled but routed
  through `Content` for uniformity — the detailedreport
  `re_check_meta` precedent). `b` is unused (`_ *promptbuilder.Builder`
  — only Agent 3 mints).
- **D9 — Non-critical / graceful-degradation ownership.** The §8.7
  RE_CHECK gate (run Agent 9 only when parent present) + graceful
  degradation (`risk_delta=null` + `RE_CHECK_PARENT_ANALYSIS_MISSING`)
  is owned by **LIC-TASK-034 / LIC-TASK-036**, NOT Agent 9. A nil
  parent reaching `Parts` = the gate failed → `INTERNAL_ERROR` (the
  detailedreport pipeline-ordering MANDATORY class). DISTINCT
  forward-note class — there is **NO** DM-artifact-gate note (Agent 9
  consumes zero DM artifacts; another deliberate divergence).
- **Stateless / concurrency-safe `Spec`.** `riskDeltaComparatorSpec`
  is an empty struct with value-receiver methods; the uniform agent
  immutability contract is upheld (`TestRun_ConcurrentRaceClean`,
  `-race`).
- **`gofmt` self-check** via `go/format` (sandbox blocks `go fmt`).

## Forward notes (recorded, owners elsewhere)

1. **base/router `Config.Model` fallback defect (owner: LIC-TASK-024 /
   router / LIC-TASK-047).** `base.Run` sets `req.Model=cfg.Model`
   unconditionally and the router forwards it unchanged on fallback
   hops; passing the resolved primary-provider model here is correct on
   the primary path; the proper base/router fix is out of scope. Shared
   with the prior 8.
2. **RE_CHECK gate invariant (owner: LIC-TASK-034 Stage Executor /
   LIC-TASK-036 Pipeline Orchestrator) — DISTINCT from the
   pipeline-ordering note 3; there is NO DM-artifact-gate note (Agent
   9 consumes zero DM artifacts).** The §8.7 RE_CHECK gate MUST skip
   Agent 9 entirely when `parent_version_id == null` OR the parent
   `RISK_ANALYSIS` was not retrieved (cache miss / parent unprocessed /
   data deleted), emitting `risk_delta=null` + the
   `RE_CHECK_PARENT_ANALYSIS_MISSING` warning via the Result Aggregator
   (LIC-TASK-035). Graceful degradation for the missing-parent case is
   the gate/orchestrator's job, NOT Agent 9's internal concern. If a
   nil `in.ParentRiskAnalysis`, a nil `in.ParentVersionID`, or an empty
   `*in.ParentVersionID` ever reaches Agent 9's `Parts`, the gate
   failed to skip the agent — a Stage-Executor/Orchestrator wiring
   defect, correctly projected to base's `Parts`-error →
   `INTERNAL_ERROR`. A reviewer must NOT add
   graceful-degradation/null-emission logic into Agent 9.
3. **Pipeline-ordering invariant (owner: LIC-TASK-034 Stage Executor)
   — DISTINCT from the RE_CHECK-gate note 2.** The Stage Executor MUST
   populate `in.MergedRiskAnalysis` (the LIC-TASK-035 Result
   Aggregator output — **NOT** the raw Agent-5 `in.RiskAnalysis`) and
   a non-empty `in.VersionID` before dispatching Agent 9 (**Stage 6,
   RE_CHECK only, AFTER the LIC-TASK-035 merge**). A nil
   `in.MergedRiskAnalysis` — or wiring only the raw field — is a
   pipeline-wiring defect → `INTERNAL_ERROR`.
4. **`AgentInput.ParentVersionID` sourcing (owner: LIC-TASK-034 Stage
   Executor).** The Stage Executor copies
   `PipelineState.ParentVersionID → AgentInput.ParentVersionID` in
   RE_CHECK mode (symmetric with the existing
   `PipelineState.ParentRiskAnalysis → AgentInput.ParentRiskAnalysis`
   copy). This task added the `model.AgentInput.ParentVersionID` field
   (the unique justified `AgentInput` extension across the 9 agents —
   a hard-`required` §9 schema field with no other source; explicitly
   **NOT** the de-scoped Agent-8-D7 `re_check_meta` case).
5. **§9-vs-§4.3.x timeout doc conflict (owner: configuration.md /
   doc-reconciliation).** `ai-agents-pipeline.md` §9 table says **8
   сек**; `high-architecture.md` §4.3.x cites
   `LIC_AGENT_RISK_DELTA_TIMEOUT=10s`. The code uses the
   constructor-supplied `config.AgentsConfig.Timeouts[config.Agent-
   RiskDelta]` (default 8s, `config/agents.go:59`, mirroring
   `configuration.md` §2.11) and is correct against whatever that
   resolves to. The §9-vs-§4.3.x discrepancy is **recorded, not
   silently chosen** here.
6. **`RiskDelta` persistence (owner: Result Aggregator LIC-TASK-035 /
   future DM task; ADR-LIC-05).** DM v1.0 ignores the unknown
   `risk_delta` field; persisting it as a new `artifact_type=RISK_DELTA`
   is a v1.1 DM extension. Not Agent 9's concern; recorded for
   completeness.

## Forward requirement for LIC-TASK-047 (app-wiring)

Build `base.Deps` (the Router + Metrics/Tracer/Estimator/RepairMetrics
adapters) and call `NewRiskDeltaComparator(resolvedPrimaryModel,
config.AgentsConfig.Timeouts[config.AgentRiskDelta], deps)`. The
resolved primary model = the configured primary provider's env-pinned
model (default claude ⇒ `LIC_CLAUDE_MODEL`). Register the returned
`*RiskDeltaComparator` in the Stage Executor (LIC-TASK-034) by
`model.AgentRiskDelta` (**Stage 6, RE_CHECK only**), AFTER the
LIC-TASK-035 Result Aggregator merge has populated
`in.MergedRiskAnalysis`, with `in.VersionID` non-empty (forward note
3) and `in.ParentRiskAnalysis` + `in.ParentVersionID` populated from
`PipelineState` only when the §8.7 RE_CHECK gate passed (forward notes
2/4 — the gate skips Agent 9 entirely otherwise).
