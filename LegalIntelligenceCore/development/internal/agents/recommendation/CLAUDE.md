# recommendation Package — CLAUDE.md

**Agent 6 — Recommendation** (LIC-TASK-030, `ai-agents-pipeline.md` §6,
`high-architecture.md` §6.6/§6.7.2). For every risk in the **merged**
`RiskAnalysis` (Agent-5 `R-NNN` plus the Aggregator-folded Agent-3
`R-PNNN` and Agent-4 `R-MNNN` findings — `high-architecture.md` §6.11.1)
and every `MISSING`/`FOUND_AMBIGUOUS` mandatory condition it proposes a
replacement clause: `original_text` (the disputed text by `clause_ref`),
`recommended_text` (a balanced, ГК-РФ-correct alternative), `explanation`
(which risk is removed, on which ГК РФ norm). Output is the flat
`model.Recommendations` list. **Stage 4 (sequential), runs AFTER the
Result Aggregator (LIC-TASK-035) merge.**

The 6th of the 9 per-agent packages. A **thin** wrapper over the shared
BaseAgent runner (LIC-TASK-024): `Recommender` embeds `*base.BaseAgent`
(so `ID()`/`Run()` make it a `port.Agent` for free); the only per-agent
code is the `Spec` — `Parts` (envelope) + `Decode` (typed result). The
invariant-heavy loop lives in `base`, once.

Constructor: `NewRecommender(modelID string, timeout time.Duration, deps
base.Deps) (*Recommender, error)` — stutter-free `NewTypeName`
(`feedback_constructors.md`; never bare `New`, never the
package-stuttering `NewRecommendation`); fail-fast via `base.NewBaseAgent`.

## Files

- **recommendation.go** — package doc, `Recommender`, `NewRecommender`,
  the §6 budget consts (`maxOutputTokens=3000`, **`temperature=0.2`** —
  the FIRST non-zero-temperature agent), the `var _ port.Agent`
  assertion, the base/router `Config.Model`-fallback forward note (shared
  with `typeclassifier` #1 / `keyparams` / `partyconsistency` /
  `mandatoryconditions` / `riskdetection`).
- **spec.go** — `recommenderSpec` (stateless), `Parts`, `Decode`.
- **internal_test.go** — `TestHermeticImports` (the **6-entry,
  artifacts-free** allowlist — the deliberate Agent-6 divergence) +
  `TestGofmtClean`.
- **recommendation_test.go** — constructor, fail-fast, the 4-block
  envelope order + no `<contract_document>`, whole upstream blocks
  (merged risk_analysis incl. `summary`/`R-PNNN`/`R-MNNN`,
  whole-KeyParameters incl. `internal_extras` + nil-extras tolerated,
  whole MandatoryConditionsReport), SEMANTIC_TREE byte-faithful
  passthrough + empty-`{}` tolerated, layer-2 escaping, upstream-JSON
  injection neutralised, the nil-`KeyParameters`/nil-`MergedRiskAnalysis`/
  nil-`MandatoryConditions`/absent·empty·malformed-SEMANTIC_TREE
  strictness errors, the **load-bearing nil-merged/non-nil-raw**
  RiskAnalysis case, empty-`Conditions[]`/empty-`Risks[]` tolerated,
  `Decode` across `R-001`/`R-P001`/`R-M001`, orphan-ref-not-guarded,
  **`TestRun_MalformedRiskID_TerminalNotRepaired`** (the crux), `Run`
  integration (acceptance Шаг 1/2), `-race`.

## API

- `NewRecommender(modelID, timeout, base.Deps) (*Recommender, error)`.
- `(*Recommender)` satisfies `port.Agent` via the embedded
  `*base.BaseAgent` (`ID()==AGENT_RECOMMENDATION`; `Run` returns
  `*model.Recommendations` or a `*model.DomainError`).

## Conventions & deliberate decisions (code-architect design review)

- **Hermetic — a DELIBERATE allowlist DIVERGENCE (D1 / MF-D1.2).** Imports
  ONLY stdlib + `internal/domain/{model,port}` + `base` + `promptbuilder`
  (for `Content` only — Agent 6 mints **no** structural block) +
  `prompts` + `schemas`. This is a **strict 6-entry SUBSET** of the
  Agent-4/5 allowlist with `internal/agents/artifacts` **deliberately
  DROPPED** — Agent 6 is the FIRST per-agent package that consumes **no
  EXTRACTED_TEXT** (the §6 envelope has no `<contract_document>` block;
  §6 "Зависимости" lists only `SEMANTIC_TREE` from DM). The
  artifacts-drop is the deliberate **non-EXTRACTED_TEXT-consumer class,
  NOT an omission** — a reviewer must NOT re-add `artifacts` to "match"
  Agents 4/5 (the `riskdetection` "deliberate absence is a class, not an
  omission" house style). The allowlist doc/CLAUDE.md must NOT copy the
  Agent-4/5 "byte-identical to `mandatoryconditions`" phrase.
  `TestHermeticImports` pins the 6-entry set AND explicitly fails if
  `artifacts` ever re-appears. **No `internal/config`** (resolved values
  are constructor params — LIC-TASK-047 wiring's job, the
  `router.RouterConfig` precedent). **No `DocumentProcessing` module**
  (SEMANTIC_TREE is a byte-faithful passthrough).
- **§6 budget SSOT.** `MaxTokens=3000`, **`Temperature=0.2`** from
  `ai-agents-pipeline.md` §6 "Бюджеты и параметры LLM". Agent 6 is the
  **FIRST and only agent so far with a non-zero temperature** — §6
  deliberately wants variety in clause *wording* ("немного выше 0 — для
  разнообразия формулировок"); a reviewer must NOT "normalise" it to
  `0.0` to match siblings (MF-D5.1). `base.NewBaseAgent` validates
  `Temperature ∈ [0,1]` so `0.2` passes. Provider=Claude (sonnet) primary
  (`LIC_AGENT_RECOMMENDATION_PROVIDER`); timeout=10s supplied by wiring
  from `LIC_AGENT_RECOMMENDATION_TIMEOUT` (default `10s`,
  `configuration.md`; `config.AgentRecommendation`);
  `Stage=model.StageAgentRecommendation` (the `base.canonicalStage` table
  requires the exact pair or `NewBaseAgent` fails fast).
- **Envelope order = the §6 prompt (D2).** `Parts` returns exactly
  `Content("key_parameters", …)` → `Content("risk_analysis", …)` →
  `Content("mandatory_conditions_report", …)` →
  `Content("semantic_tree", …)` — `recommendation.txt:32-37`. There is
  **NO `<contract_document>` block** (Agent 6 consumes no EXTRACTED_TEXT
  — D1). All four blocks are `promptbuilder.Content` (escaped — layer-2;
  **MF-D2.1, NON-NEGOTIABLE** — the merged `risk_analysis` and the
  `mandatory_conditions_report` carry upstream-LLM-derived free text that
  is a prime injection vector); `b` is unused (`_ *promptbuilder.Builder`
  — only Agent 3 mints).
- **Each upstream block emitted WHOLE, not projected (D2).**
  `json.Marshal` of `in.KeyParameters` (incl. `internal_extras`; nil
  `InternalExtras` dropped via `omitempty`), `in.MergedRiskAnalysis`
  (incl. `summary` + `prompt_injection_detected`), `in.MandatoryConditions`
  (incl. `conditions`/`contract_type`). The deliberate asymmetry with
  Agents 4/5's *projected* `classification_result`: §6 "Зависимости"
  lists each whole, no `.field` selector (the Agent-4 whole-KeyParameters
  precedent).
- **`risk_analysis` ← `in.MergedRiskAnalysis`, NEVER `in.RiskAnalysis`
  (D3 / MF-D3.1 — load-bearing).** Agent 6 is Stage 4, AFTER the Result
  Aggregator (LIC-TASK-035) folds the Agent-3 `R-PNNN`/Agent-4 `R-MNNN`
  findings into the merged `risks[]`. §6 requires the model to attribute
  recommendations to `R-MNNN`/`R-PNNN` ids that exist **only post-merge**
  (`recommendation.txt:27-29`); feeding the raw `in.RiskAnalysis`
  (Agent-5 `R-NNN` only) would make every mandatory/party recommendation
  a guaranteed orphan. A nil-merged / non-nil-raw input is a
  Stage-Executor pipeline-wiring defect ⇒ `INTERNAL_ERROR` (pinned by the
  `nil merged but non-nil raw RiskAnalysis` subtest).
- **Strictness phase MIRRORS the envelope (D3 / the `mandatoryconditions`
  CC-4 precedent).** Agent 6 has only MANDATORY blocks across TWO classes
  (pipeline-ordering vs. DM-artifact-gate) and **no optional class** like
  Agent-5's PROCESSING_WARNINGS, so the strictness checks run in envelope
  order (`KeyParameters` → `MergedRiskAnalysis` → `MandatoryConditions` →
  `SEMANTIC_TREE`) — NOT the Agent-5 grouped-by-class structure. A
  reviewer must NOT regroup it. `in.KeyParameters==nil ||
  in.MergedRiskAnalysis==nil || in.MandatoryConditions==nil` is a
  **pipeline-ordering** breach (forward note 2) ⇒ `INTERNAL_ERROR`; **no
  tolerated-empty** (the §6 prompt has no "(если есть)" hedge, fixed
  4-block envelope). A non-nil report with empty `Conditions[]` ("all
  present") and a non-nil merged analysis with empty `Risks[]` (a clean
  contract) **are TOLERATED**, marshalled verbatim (the Agent-4
  nil-`InternalExtras` tolerance analogue; MF-D3.2).
- **SEMANTIC_TREE byte-faithful passthrough (D3).** `string(rawTree)`
  straight into `Content` — NOT decoded/re-encoded (the model cites
  clause text "по clause_ref" against tree node ids —
  `recommendation.txt:14,17`). Strictness gate = well-formedness
  (`json.Valid`). Absent / empty bytes / `!json.Valid` ⇒ `Parts` error;
  an empty-but-well-formed tree (`{}`) is TOLERATED (emitted verbatim).
- **`Decode` drift-guard: `risk_id` FORMAT only — terminal, NOT
  repair-triggered (D4 / the crux).** Decode unmarshals into
  `*model.Recommendations` then guards every `recommendations[i].risk_id`
  with **`model.IsValidRiskID`** — the FROZEN merged SSOT regex
  `^R-(P|M)?[0-9]{3,}$` (`risk_analysis.go`), whose godoc verbatim names
  `recommendations[].risk_id` as a surface it locks. A **local
  `regexp` duplicate is FORBIDDEN (MF-D4.3)** — only the existing frozen
  model SSOT function.
  - **This is the Agent-4 `Code`-guard CLASS, not the Agent-5 `Risk.ID`
    over-reach class.** Agent 5 did NOT guard `Risk.ID` because
    `model.IsValidRiskID` is strictly *looser* than its own narrower
    `^R-[0-9]{3,}$` schema pattern (an un-faithful, un-SSOT'd guard).
    Here the §6 schema has **no** `pattern` on `risk_id` and the frozen
    merged regex **IS** the `recommendations[].risk_id` contract — so the
    guard is the exact, faithful schema/contract↔Go cross-check, identical
    in kind to Agent 4 guarding `Code` via the equal frozen
    `model.IsValidMandatoryConditionCode`. It directly discharges
    acceptance test_step 2 in Go.
  - **TERMINAL, NOT repair-triggered — a genuine divergence from Agent
    4.** `recommendation.json` constrains `risk_id` only to
    `{"type":"string"}` (no `pattern`), so a malformed id is
    **schema-VALID** ⇒ base step-7 passes ⇒ the sticky 1-shot repair loop
    is **never entered** ⇒ the step-8 Decode guard is the **sole and
    terminal** format enforcement ⇒ a miss is a terminal
    `INTERNAL_ERROR`, never a repair turn. Contrast Agent 4
    (`mandatory_conditions.json` HAS the pattern → a bad code is
    schema-INVALID → step-7 fires the repair) / Agent 5 (its `level` enum
    is in-schema). Pinned by
    **`TestRun_MalformedRiskID_TerminalNotRepaired`** — the explicit
    INVERSE of `riskdetection`'s `TestRun_InvalidLevel_RepairTriggered`.
  - **FORMAT ONLY.** Decode does NOT validate EXISTENCE (does `risk_id`
    reference a real merged risk) nor de-duplicate — those are
    downstream, owned by the Result Aggregator (LIC-TASK-035), which
    emits `DETAILED_REPORT.warnings.RECOMMENDATION_ORPHAN_REF` per §6
    criterion 2. A well-formed orphan id decodes successfully (pinned by
    `TestSpec_Decode_OrphanRefNotGuarded`; Decode has no access to the
    merged set anyway). `original_text`/`recommended_text`/`explanation`
    (+ `maxLength`) are schema-enforced pre-Decode and structurally free
    — NOT guarded (the `mandatoryconditions`/`riskdetection`
    enumerated-unguarded-fields house style). Decode is a pure
    typed-unmarshal + single-format drift-guard, **never** a transform.
- **Stateless / concurrency-safe `Spec`.** `recommenderSpec` is an empty
  struct with value-receiver methods; one `*Recommender` is shared by the
  parallel errgroup pipeline without locking (`TestRun_ConcurrentRaceClean`,
  `-race`).
- **`gofmt` self-check** via `go/format` (sandbox blocks `go fmt`).

## Forward notes (recorded, owners elsewhere)

1. **base/router `Config.Model` fallback defect (owner: LIC-TASK-024 /
   router / LIC-TASK-047).** `base.Run` sets `req.Model=cfg.Model`
   unconditionally and the router forwards it unchanged on fallback hops;
   passing the resolved primary-provider model here is correct on the
   primary path; the proper base/router fix is out of scope. Shared with
   `typeclassifier` #1 / `keyparams` / `partyconsistency` /
   `mandatoryconditions` / `riskdetection`.
2. **Pipeline-ordering invariant (owner: LIC-TASK-034 Stage Executor) —
   DISTINCT from note 3.** The Stage Executor MUST populate
   `in.KeyParameters` (Agent 2 Stage 1), `in.MandatoryConditions` (Agent
   4 Stage 3) AND `in.MergedRiskAnalysis` (the LIC-TASK-035 Result
   Aggregator output, **NOT** the raw Agent-5 `in.RiskAnalysis`) before
   dispatching Agent 6 (Stage 4, **sequential, AFTER the aggregator
   merge**). A nil any-of-three at Agent 6 is a Stage-Executor
   pipeline-wiring defect, correctly projected to `INTERNAL_ERROR`.
3. **Upstream DM artifact-bundle gate (owner: LIC-TASK-034 / the bundle
   gate) — DISTINCT from note 2.** A genuinely missing/empty
   `SEMANTIC_TREE` is semantically `DM_ARTIFACTS_MISSING` (retryable),
   NOT `INTERNAL_ERROR`. That gate MUST reject it BEFORE Agent 6 runs;
   reaching `Parts`' tree check then means a true LIC invariant breach,
   for which base's `Parts`-error→`INTERNAL_ERROR` is the correct code.
   Identical in spirit to `typeclassifier` #2 / `keyparams` /
   `partyconsistency` #3 / `mandatoryconditions` #3 / `riskdetection` #3.
4. **`risk_id` EXISTENCE/dedup post-processing (owner: LIC-TASK-035
   Result Aggregator).** Agent 6 `Decode` guards `risk_id` FORMAT only.
   The Aggregator is the SINGLE site that validates that each `risk_id`
   references a real element of the merged `risks[]`, de-duplicates, and
   emits `DETAILED_REPORT.warnings.RECOMMENDATION_ORPHAN_REF` for orphans
   (`ai-agents-pipeline.md` §6 criterion 2; `model/recommendations.go`
   godoc). A future reviewer must NOT add cross-reference validation into
   `Decode` (it would be a transform and need state Decode must not have).

## Forward requirement for LIC-TASK-047 (app-wiring)

Build `base.Deps` (the Router + Metrics/Tracer/Estimator/RepairMetrics
adapters) and call `NewRecommender(resolvedPrimaryModel,
config.AgentsConfig.Timeouts[config.AgentRecommendation], deps)`. The
resolved primary model = the configured primary provider's env-pinned
model (default claude ⇒ `LIC_CLAUDE_MODEL`). Register the returned
`*Recommender` in the Stage Executor (LIC-TASK-034) by
`model.AgentRecommendation` (**Stage 4, sequential**, AFTER the
LIC-TASK-035 Result Aggregator merge has populated `in.MergedRiskAnalysis`,
and with `in.KeyParameters` + `in.MandatoryConditions` populated — forward
note 2).
