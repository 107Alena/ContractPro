# aggregator Package — CLAUDE.md

**Result Aggregator** (LIC-TASK-035, `high-architecture.md` §6.11 /
§6.11.1-3 / §4.3.4 / §8.7, `ai-agents-pipeline.md` §3 / warnings
schema). The deterministic (no-LLM) final stage of the LIC pipeline.
It folds the nine agent outputs into the outbound
`LegalAnalysisArtifactsReady` surface:

- **id-namespace resolution** — one merged `risks[]` =
  `R-NNN` (Agent 5, untouched) + `R-PNNN` (Agent 3 findings) +
  `R-MNNN` (Agent 4 conditions with `status != FOUND_OK`), §6.11.1;
- **`RISK_PROFILE` / `AGGREGATE_SCORE`** — deterministic derivation
  (ASSUMPTION-LIC-17/18, §6.11 steps 3-4);
- **stripping** — `risks[].rationale`,
  `key_parameters.internal_extras` / `prompt_injection_detected`
  (§6.11 step 5 — the single stripping site);
- **warnings merge** — `PROMPT_INJECTION_DETECTED`,
  `RE_CHECK_PARENT_ANALYSIS_MISSING`, `INPUT_TRUNCATED`,
  `CLASSIFICATION_PARAMS_MISMATCH`, `RECOMMENDATION_ORPHAN_REF`
  (§6.11 steps 6-7 / §6.11.3 / §8.7).

`Aggregate` is a **pure function**: never mutates anything reachable
from `Input`, returns freshly-allocated `Output`. The Pipeline
Orchestrator (LIC-TASK-036) owns building `Input` from `PipelineState`
and assigning `Output` back. NOT a `port.Agent` (no LLM, no `Run`).

Constructor: `NewAggregator(cfg Config, m Metrics) (*Aggregator,
error)` — `NewTypeName` (`feedback_constructors.md`), fail-fast on
invalid `Config` (`%w`); nil `Metrics` ⇒ no-op.

## Files

- **aggregator.go** — package doc, `Aggregator`, `NewAggregator`,
  `Aggregate`, `Config` (+`validate`), `Input`, `Output`,
  `TruncationInfo`, `ErrNilRiskAnalysis`.
- **merge.go** — `buildMergedRisks` (§6.11.1 steps 1-3),
  `partyLevel` / `mandatoryLevel` / `mandatoryCategory` /
  `mandatoryDescription` (§4.3.4 fixed-level tables), id generation.
- **derive.go** — `riskProfile` (step 3), `aggregateScore` (step 4,
  `clamp` + label).
- **strip.go** — `stripKeyParameters` (step 5; rationale stripping is
  inline in `buildMergedRisks`).
- **warnings.go** — `buildWarnings` + the 5 `apply*` helpers + the RU
  `user_message` constants.
- **seams.go** — `Metrics` interface + `noopMetrics` +
  `var _ Metrics = noopMetrics{}`.
- **internal_test.go** — `TestHermeticImports` (model-only allowlist,
  active-fail on config/infra/agents) + `TestGofmtClean`.
- **aggregator_test.go** — full suite (all D1..D11 invariants;
  load-bearing pins marked ★ in the design).
- **CLAUDE.md** — this file.

## API

- `NewAggregator(Config, Metrics) (*Aggregator, error)`.
- `(*Aggregator) Aggregate(Input) (Output, error)` — only error is
  `ErrNilRiskAnalysis` (Agent 5 critical / merge base); all other
  agent inputs are nil-tolerated.

## Conventions & deliberate decisions (subagent code-architect D1–D11)

- **D1 — party `Risk.Level` is DETERMINISTICALLY DERIVED from
  `finding.Type`, NOT `PartyFinding.Severity`.** `PARTY_AUTHORITY_-
  MISSING → high`, every other `PARTY_*` → `medium` (`partyLevel`).
  Authoritative SSOT: task acceptance step 2 + `high-architecture.md`
  §4.3.4 ("**фиксированный** уровень") + `ai-agents-pipeline.md`
  543-546 ("через Result Aggregator"). The
  `party_consistency.go` `PartyFinding` godoc clause "Result
  Aggregator does not re-map" is **descriptive of coincidence**
  (Agent 3's prompt emits the same fixed mapping under the happy
  path) — it cannot override three normative lines, and "fixed level"
  cannot be a function of an LLM-chosen value. Defense-in-depth: a
  prompt-injected/buggy Agent 3 emitting `Severity:"low"` for a
  `PARTY_AUTHORITY_MISSING` finding must NOT depress the merged
  profile/score (the OQ-13 / ADR-LIC-07 threat model). The model
  godoc is NOT edited (never re-touch a reviewed model file —
  the Agent-5..9 precedent); this is the recorded SSOT-resolution
  (schemavalidator "PriorTurns SSOT reconciliation" class). Pinned by
  `TestBuildMergedRisks_PartyLevel_DefenseInDepth` (the load-bearing
  `Severity:"low"`/`"high"` ignored subtests).
- **D2 — pure function; Aggregator owns Input/Output; no model /
  PipelineState mutation; NO carrier added.** The Agent-8-D7 /
  Agent-9-D3 anti-carrier line: `model.AgentInput`/`PipelineState`
  are NOT extended. `model.InputTruncatedWarning` ALREADY carries
  `{TruncatedBytes,TotalBytes}` (warnings.go) — the wire carrier
  exists; the raw counts reach the Aggregator via `Input.Truncation`
  (sourced later by LIC-TASK-021/036, the Agent-8-D7 "точный sourcing
  → forward note" pattern). Pure shape = hermetic + `-race` +
  table-test friendly (no `PipelineState` pointer aliasing).
  `Input.RiskDelta` is DELIBERATELY NOT consulted (kept for full-state
  passing + the D4 non-tautology pin).
- **D3 — `PROMPT_INJECTION_DETECTED` attribution = exactly the 5
  flag-carrying model types → their `model.AgentID`.** Only
  `ClassificationResult`/`KeyParameters`/`PartyConsistencyFindings`/
  `MandatoryConditionsReport`/`RiskAnalysis` carry the flag in the
  model SSOT; Recommendations/Summary/DetailedReport/RiskDelta have
  no such field by design (they consume structured upstream output,
  not raw contract text; ADR-LIC-07). §6.11's "all 9" is resolved
  against the model SSOT to these 5 (the schemavalidator "model is
  the harder constraint" class). Read from the RAW `Input` BEFORE
  stripping (ordering pinned). `detected_by_agents` sorted
  lexicographically; metric `lic_prompt_injection_detected_total
  {agent}` incremented EXACTLY once per detecting agent, label =
  `model.AgentID.String()` only (bounded cardinality). Pinned by
  `TestApplyPromptInjection_*` + `TestAggregate_InjectionReadFromRaw_-
  BeforeStrip`.
- **D4 — `RE_CHECK_PARENT_ANALYSIS_MISSING` needs an EXPLICIT signal,
  NOT `RiskDelta==nil`.** Predicate exactly: `Mode==RE_CHECK &&
  ParentAnalysisMissing`. A nil delta is ambiguous (Agent-9 gate
  breach / provider error / non-critical skip — Agent-9 D1). The root
  cause "parent `RISK_ANALYSIS` unavailable" (§8.7 step 2) is knowable
  only by the Orchestrator at the DM-fetch step (Agent-9 FN-2). The
  Aggregator only renders; it does NOT null out `risk_delta` (the
  Orchestrator owns the outbound payload, §8.7 step 4). Pinned by
  `TestApplyReCheckParentMissing_NonTautology` (warns for BOTH
  `RiskDelta==nil` and non-nil; does NOT warn on `!ParentAnalysis-
  Missing + RiskDelta==nil`).
- **D5 — mutate-in-place FORBIDDEN; `MergedRiskAnalysis` /
  `StrippedKeyParameters` are distinct deep allocations.** The merged
  artifact MUST be separate from raw (Agent-9 D2 relies on it;
  `PipelineState` has distinct `RiskAnalysis` vs `MergedRiskAnalysis`
  fields; Agent 7 D1 consumes RAW). `Aggregate` may run concurrently
  with Stage-5 errgroup holders → immutability is a hard invariant,
  not a "runs last" assumption. Fresh `[]model.Risk` backing array;
  value-copy each raw `Risk` then `Rationale=nil`. Pinned by
  `TestAggregate_DoesNotMutateInput` (JSON snapshot equality +
  distinct-pointer + write-back) and `TestAggregate_Concurrent-
  RaceClean` (`-race` ×32 shared `Input`).
- **D6 — single stripping site.** On the COPIES only: merged
  `Risk.Rationale→nil`, `KeyParameters.InternalExtras→nil`,
  `KeyParameters.PromptInjectionDetected→false`; merged
  `RiskAnalysis.PromptInjectionDetected→false` (internal envelope
  signal, surfaced only via the D3 warning). Ordering D3 (read raw)
  **before** D6 (stripped copies) — pinned.
- **D7 — hermetic; telemetry seamed; config ctor-injected.** Imports
  ONLY stdlib + `internal/domain/model`. NO `internal/config`
  (scoring weights via local `Config`, the agents/* ctor-param
  precedent), NO `internal/infra/observability/metrics` (Prometheus
  inverted behind `Metrics`/`noopMetrics`, the schemavalidator.Metrics
  / cost.Recorder precedent), NO `internal/agents/*`. `Test-
  HermeticImports` active-fails on any of those.
- **D8 — `user_message` sourcing.** `msgPromptInjection` VERBATIM
  `high-architecture.md:837`; `msgReCheckParentMissing` VERBATIM
  `high-architecture.md:1105`. `msgInputTruncated` /
  `msgClassificationParamsMismatch` / `msgRecommendationOrphanRef` —
  faithful RU (no verbatim SSOT exists), package constants pinned
  byte-exact + `len<=500` (schema `maxLength:500`) by
  `TestUserMessageConstants`. Deviating from a frozen-message SSOT is
  the larger risk (schemavalidator BYTE-EXACT precedent).
- **D9 — `CLASSIFICATION_PARAMS_MISMATCH` v1 = EXACTLY ONE rule:**
  `contract_type==NDA && key_parameters.price != null` (the §6.11
  step 7 sole concrete example). Broader rule sets + severity tiering
  are explicitly v1.1 (`ai-agents-pipeline.md:59`) — a future agent
  must NOT widen this mid-flight (the Agent-7/8 "no scope creep"
  precedent). `price != null` = pointer non-nil (an empty-but-present
  `""` still counts — do NOT add a non-empty stricter test). nil
  Classification/KeyParameters ⇒ no warning, no panic.
- **D10 — `RECOMMENDATION_ORPHAN_REF` = EXISTENCE vs the MERGED
  set.** Agent 6 owns risk_id FORMAT terminally (recommendation D4);
  the Aggregator checks only membership against the merged ids
  (R-NNN ∪ R-PNNN ∪ R-MNNN — Agent 6 received the merged list,
  §6.11.3). Orphans de-duped, first-seen order (deterministic).
  Pinned by `TestApplyRecommendationOrphanRef` (synthesized
  R-P001/R-M001 are NOT orphans — the Agent-9-D2-class pin).
- **D11 — count domains + layered score formula (verbatim).**
  `RISK_PROFILE` high/medium/low over the MERGED `risks[]`;
  `AGGREGATE_SCORE` uses the SAME merged counts AND a SEPARATE
  `missing/ambiguous_mandatory` penalty counted over the ORIGINAL
  Agent-4 conditions (status MISSING / FOUND_AMBIGUOUS), NOT a
  re-count of R-MNNN. The apparent overlap (a missing condition adds
  both a 25-high and a 15-missing penalty) is the spec's deliberate
  empirical baseline (ASSUMPTION-LIC-18) — implemented exactly, NOT
  "de-duplicated". `clamp(points,0,100)/100` then `>=` threshold
  labels. Pinned by `TestAggregate_Score_Step3` (0.65/medium),
  `TestAggregate_Score_MandatoryPenaltyLayered` (0.60/medium proves
  layering), `TestAggregate_Score_ClampFloor`.
- **SSOT-gap resolution — R-MNNN `Description`/`ClauseRef`.** §6.11
  does not spell out the merged `Risk.Description` for an Agent-4
  condition. Minimal deterministic rule (`mandatoryDescription`):
  `*IssueDescription` when non-empty, else `Label`; `ClauseRef` =
  first `FoundIn` else `""`. Does not affect ids/counts/score (the
  load-bearing invariants). Recorded here per the schemavalidator
  "authoritative record of the reconciliation" discipline.
- **Stateless / concurrency-safe.** `Aggregator` is fixed after
  construction; `Aggregate` is pure → safe for the parallel pipeline
  (`-race`). `gofmt` self-check via `go/format` (sandbox blocks
  `go fmt`).

## Forward notes (recorded, owners elsewhere)

1. **LIC-TASK-036 (Pipeline Orchestrator).** Runs `Aggregate`
   strictly AFTER all stages join. Builds `Input` from
   `PipelineState`: `Input.RiskAnalysis = state.RiskAnalysis` (RAW
   Agent-5, NOT `state.MergedRiskAnalysis`), `Input.ParentAnalysis-
   Missing` per the §8.7-step-2 DM-fetch outcome, `Input.Truncation`
   from the LIC-TASK-021 Token Estimator (nil if not truncated).
   Assigns back: `Output.MergedRiskAnalysis → state.Merged-
   RiskAnalysis`, `Output.RiskProfile/AggregateScore → state.*`,
   `Output.Warnings → state.DetailedReport.Warnings`,
   `Output.StrippedKeyParameters` = the outbound `key_parameters`
   artifact. The Orchestrator (NOT the Aggregator) nulls outbound
   `risk_delta` when `RE_CHECK_PARENT_ANALYSIS_MISSING` (§8.7 step 4).
   The merged `risks[]` must be available to Agents 6/8/9 (their
   forward notes) — 036 decides whether `Aggregate` runs once
   post-join or merge-early/finalize-late; the pure function is
   idempotent over a fixed `Input`, supporting either.
2. **LIC-TASK-021 (Token Estimator).** Sole source of
   `TruncatedBytes`/`TotalBytes`; the Aggregator never estimates or
   truncates — it only renders `INPUT_TRUNCATED` when 036 passes a
   non-nil `Input.Truncation`. No model carrier added (Agent-8-D7
   anti-carrier upheld; `model.InputTruncatedWarning` is the wire
   carrier).
3. **LIC-TASK-034 (Stage Executor).** Must populate the MERGED risk
   artifact before Agents 6/8 (its existing forward note from
   Agents 6/8/9); this package is the producer of that merge.
4. **LIC-TASK-047 (app-wiring).** `NewAggregator(Config{from
   config.ScoringConfig}, metricsAdapter{*metrics.CrossCutMetrics})`;
   the adapter maps `PromptInjectionDetected(agent)` →
   `CrossCut.PromptInjectionDetectedTotal.WithLabelValues(agent)
   .Inc()`. Assert `var _ Metrics = (*metricsAdapter)(nil)` in
   wiring, NOT here (schemavalidator `Repairer`-seam precedent).
5. **v1.1.** Broader `CLASSIFICATION_PARAMS_MISMATCH` rule set +
   prompt-injection severity tiering — a separate task
   (`ai-agents-pipeline.md:59`). Single-rule v1 boundary recorded
   (D9) so a future agent does not widen it mid-flight.
