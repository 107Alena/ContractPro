# detailedreport Package — CLAUDE.md

**Agent 8 — Detailed Report** (LIC-TASK-032, `ai-agents-pipeline.md`
§8, `high-architecture.md` §6.6/§6.7.2). From the classification, the
key parameters, the party-consistency findings, the
mandatory-conditions report, the **MERGED** risk analysis, the
recommendations and the semantic tree it produces ONE
`model.DetailedReport` for a fellow lawyer: **seven fixed-order
sections** (OVERVIEW, KEY_PARAMETERS, PARTY_DATA,
MANDATORY_CONDITIONS, RISKS, RECOMMENDATIONS_SUMMARY, WARNINGS), each
with items carrying a clause locator (`clause_ref`), a `legal_basis`
and links to the underlying risk / recommendation. Unlike Agent 7
(plain language) the §8 register is **professional legal prose**.
**Stage 5, in PARALLEL with Agent 7 (business summary)**, after the
Result Aggregator (LIC-TASK-035) merge.

The 8th of the 9 per-agent packages. A **thin** wrapper over the shared
BaseAgent runner (LIC-TASK-024): `DetailedReporter` embeds
`*base.BaseAgent` (so `ID()`/`Run()` make it a `port.Agent` for free);
the only per-agent code is the `Spec` — `Parts` (envelope) + `Decode`
(typed result). The invariant-heavy loop lives in `base`, once.

Constructor: `NewDetailedReporter(modelID string, timeout
time.Duration, deps base.Deps) (*DetailedReporter, error)` —
stutter-free `NewTypeName` (`feedback_constructors.md`; never bare
`New`, never the package-stuttering `NewDetailedReport`); fail-fast via
`base.NewBaseAgent`.

## Files

- **detailedreport.go** — package doc, `DetailedReporter`,
  `NewDetailedReporter`, the §8 budget consts (`maxOutputTokens=5000`,
  **`temperature=0.0`** — the RETURN to deterministic 0.0, the inverse
  of Agents 6/7's non-zero), the `var _ port.Agent` assertion, the
  base/router `Config.Model`-fallback forward note (shared with the
  prior 7).
- **spec.go** — `detailedReporterSpec` (stateless), the sentinels
  (`emptyProcessingWarnings`/`emptyRecommendations` `[]`,
  `reCheckMetaDefault`, the typed `emptyPartyConsistency()`), the
  minimal `classificationProjection{ContractType model.ContractType}`,
  `Parts`, `Decode`.
- **internal_test.go** — `TestHermeticImports` (the **6-entry,
  artifacts-FREE** allowlist — the deliberate Agent-6-class divergence)
  + `TestGofmtClean`.
- **detailedreport_test.go** — constructor, fail-fast, the 9-block
  envelope order, the byte-exact minimal classification projection,
  whole mandatory upstream blocks (MERGED risk_analysis incl.
  `summary`/`R-PNNN`/`R-MNNN`, whole-KeyParameters incl.
  `internal_extras` + nil-extras tolerated, whole
  MandatoryConditionsReport), the **D4** PartyConsistency tolerance
  (whole / nil→typed zero-value sentinel / nil≡non-nil-empty / never
  `null`), the **D5** Recommendations tolerance (whole / nil·empty →
  exactly `[]`), the **D6** PROCESSING_WARNINGS tiers (verbatim /
  absent·empty·whitespace·`null` → `[]` / present-malformed → error /
  absent ≠ error), the **D7** fixed `re_check_meta`
  (input-invariant, unchanged by `ParentRiskAnalysis`), SEMANTIC_TREE
  byte-faithful + empty-`{}` tolerated + absent·empty·malformed error,
  layer-2 escaping, upstream-JSON injection neutralised, the **4 hard
  pipeline-ordering nil errors + the load-bearing nil-merged/non-nil-raw
  RiskAnalysis subtest** (D1's pin), empty-slice tolerance, `Decode`
  (7-section fixed order; section_code & non-nil-severity drift-guard;
  `null`-severity & orphan `linked_risk_id` decode clean;
  **warnings-passthrough NOT stripped**), `Run` integration
  (acceptance Шаг 1/2), **`TestRun_InvalidSectionCode_RepairTriggered`**
  (the inverse of recommendation's terminal pin), `-race`.

## API

- `NewDetailedReporter(modelID, timeout, base.Deps)
  (*DetailedReporter, error)`.
- `(*DetailedReporter)` satisfies `port.Agent` via the embedded
  `*base.BaseAgent` (`ID()==AGENT_DETAILED_REPORT`; `Run` returns
  `*model.DetailedReport` or a `*model.DomainError`).

## Conventions & deliberate decisions (code-architect design review D1–D10)

- **Hermetic — 6-entry, artifacts-FREE (the Agent-6
  non-EXTRACTED_TEXT-consumer CLASS, D10).** Imports ONLY stdlib +
  `internal/domain/{model,port}` + `base` + `promptbuilder` (for
  `Content` only — Agent 8 mints **no** structural block) + `prompts` +
  `schemas`. A **strict 6-entry SUBSET** of the Agent-4/5/7 allowlist
  with `internal/agents/artifacts` **deliberately DROPPED**: the §8
  envelope (`detailed_report.txt:33-43`) has **no
  `<contract_document>`** block and §8 "Зависимости"
  (`ai-agents-pipeline.md:1280`) lists only `SEMANTIC_TREE` from DM —
  Agent 8 consumes **no EXTRACTED_TEXT**. The artifacts-drop is the
  deliberate "non-EXTRACTED_TEXT consumer" class, **NOT a regression
  toward Agents 4/5/7** — a reviewer must NOT re-add `artifacts` (the
  `riskdetection` "deliberate absence is a class, not an omission"
  house style). `TestHermeticImports` pins the 6-entry set AND
  explicitly fails if `artifacts` ever appears. **No `internal/config`**
  (resolved values are constructor params — LIC-TASK-047). **No
  `DocumentProcessing` module** (SEMANTIC_TREE is a byte-faithful
  passthrough).
- **§8 budget SSOT (D8).** `MaxTokens=5000`, **`Temperature=0.0`** from
  `ai-agents-pipeline.md` §8 "Бюджеты и параметры LLM". Agent 8
  **RETURNS to a deterministic 0.0** — the INVERSE of Agents 6 (0.2)
  and 7 (0.3). A reviewer must NOT "carry forward" `0.3` from the
  immediately-preceding Agent 7 to match the predecessor — `0.0` is
  the binding §8 SSOT value (the mirror image of the Agent-6/7
  MF-D5.1 doc-lock). `base.NewBaseAgent` validates `Temperature ∈
  [0,1]` so `0.0` passes. Provider=Claude (sonnet) primary
  (`LIC_AGENT_DETAILED_REPORT_PROVIDER`); timeout=**12s** supplied by
  wiring from `LIC_AGENT_DETAILED_REPORT_TIMEOUT` (default `12s`,
  `configuration.md`; `config.AgentDetailedReport`) — never hard-coded;
  `Stage=model.StageAgentDetailedReport` (the `base.canonicalStage`
  table requires the exact pair or `NewBaseAgent` fails fast).
- **Envelope order = the §8 prompt (D10).** `Parts` returns exactly the
  9 blocks `classification_result` → `key_parameters` →
  `party_consistency_findings` → `mandatory_conditions_report` →
  `risk_analysis` → `recommendations` → `processing_warnings` →
  `re_check_meta` → `semantic_tree` (`detailed_report.txt:33-43`). All
  nine are `promptbuilder.Content` (escaped — layer-2; **NON-NEGOTIABLE**
  — `risk_analysis`/`mandatory_conditions_report`/
  `party_consistency_findings`/`recommendations` carry
  upstream-LLM-derived free text and `semantic_tree` is the raw
  contract tree, prime injection vectors). `b` is unused (`_
  *promptbuilder.Builder` — only Agent 3 mints).
- **`classification_result` = MINIMAL `{contract_type}` projection
  (D2).** Local typed `classificationProjection{ContractType
  model.ContractType}` from `in.Classification.ContractType` → exactly
  `{"contract_type":"SUPPLY"}`. The §8 prompt block is an **ellipsis**
  (`<classification_result>…`) and OVERVIEW needs only the type label
  — the ratified Agent-4/5/7 bare-ellipsis precedent. Typed enum (not
  `string`) ⇒ a rename is a compile error; byte-exact single-key shape
  pinned.
- **`risk_analysis` ← `in.MergedRiskAnalysis` (RAW NEVER), the
  LOAD-BEARING D1 — the DELIBERATE divergence from the
  immediately-preceding Agent 7 (which used RAW).** The decision driver
  is the **§8 prompt body**, not the bare "Зависимости" line: the §8
  PROHIBITION (`detailed_report.txt:72-73`) explicitly states the
  embedded Agent-3/4 risks "уже находятся в RiskAnalysis **после
  Result Aggregator**" and criterion 5 (`detailed_report.txt:55`) ties
  `linked_risk_id` to ids the MERGED set defines — raw Agent-5 (R-NNN
  only) makes the §8 prohibition **unsatisfiable** (the Agent-6 D3
  structural argument). §8 "Зависимости" lists a bare `RiskAnalysis`
  (same wording as §7's) BUT, unlike §7, the §8 prompt body explicitly
  cites the post-merge namespace — the Agent-7-D1 corroborating factors
  are **INVERTED** here, so this is the Agent-6 MERGED class, NOT the
  Agent-7 RAW class. Agent 8 is Stage 5, after the LIC-TASK-035 merge.
  The nil-check is on `in.MergedRiskAnalysis`; a non-nil raw
  `in.RiskAnalysis` does **NOT** satisfy it (the Stage Executor must
  wire the MERGED field — forward note 2). Pinned by the **`nil merged
  but non-nil raw RiskAnalysis`** subtest (the exact inverse of
  `summary`'s pin, identical to `recommendation`'s). A reviewer must NOT
  "normalise" this back to RAW to match Agent 7.
- **Each MANDATORY upstream block emitted WHOLE (D1/D3).**
  `json.Marshal` of `in.KeyParameters` (incl. `internal_extras`; nil
  `InternalExtras` dropped via `omitempty`), `in.MandatoryConditions`
  (incl. `conditions`/`contract_type`), `in.MergedRiskAnalysis` (incl.
  `summary` + `prompt_injection_detected` + the R-PNNN/R-MNNN ids). The
  deliberate asymmetry with the *projected* `classification_result`:
  §8 "Зависимости" lists each whole, no `.field` selector (the Agent-4
  whole-KeyParameters precedent). Empty `Conditions[]`/`Risks[]` (clean
  contract) TOLERATED, marshalled verbatim (the Agent-4/6/7 empty-slice
  tolerance).
- **`party_consistency_findings` — TOLERATED-nil, typed zero-value
  sentinel (D4 — a NEW class for this package).** Agent 3 is
  **non-critical** (`error-handling.md:304` — skipped on
  timeout/failure → no findings), so a nil `in.PartyConsistency` is an
  **in-spec degradation state**, NOT a pipeline-ordering breach
  (**forward note 4** — the riskdetection-FN-5 OPTIONAL-tolerance
  class, DISTINCT from the pipeline-ordering note 2 and the
  DM-artifact-gate note 3; a reviewer must NOT promote it to
  `INTERNAL_ERROR`). The §8 prompt (`detailed_report.txt:19-20`)
  explicitly handles the empty case. **The sentinel is the marshalled
  ZERO-VALUE `model.PartyConsistencyFindings` with an explicitly
  non-nil empty `Findings` slice — NOT a hand-written `{"findings":[]}`
  literal, NOT `null`** (code-architect D4 binding correction): a byte
  literal would silently drift if the struct tags change (e.g. the
  non-`omitempty` `prompt_injection_detected` bool); marshalling the
  typed value keeps the nil-path and non-nil-empty-path **byte-identical
  from the same type SSOT** (pinned: nil≡non-nil-empty, never `null`).
  Non-nil ⇒ marshalled WHOLE, empty `Findings[]` tolerated.
- **`recommendations` — TOLERATED-empty, `[]` sentinel (D5).**
  `in.Recommendations` is a bare SLICE: `json.Marshal(nil-slice)` is
  `null` (riskdetection CC-3 forbidden in an LLM-facing block). Agent 6
  is tier-2 (if it failed the pipeline fails ⇒ Agent 8 never runs), and
  a clean contract legitimately yields **zero** recommendations; a
  slice's nil is indistinguishable from empty — so this is the
  OPTIONAL/tolerated class (the Agent-6/7 empty-slice tolerance), **NOT
  a pipeline-ordering hard-fail**. `len==0` ⇒ exactly `[]`, else
  marshalled WHOLE. Pinned (nil AND empty → `[]`).
- **`processing_warnings` — OPTIONAL, byte-faithful, `[]`-normalised
  (D6 — the EXACT riskdetection D3/CC-2 precedent, reused verbatim).**
  §8 marks it `<!-- от DP, опц. -->` (`detailed_report.txt:40`).
  absent / empty / whitespace / bare `null` token ⇒ the literal `[]`
  (`emptyProcessingWarnings`); present & `json.Valid` ⇒ byte-faithful
  verbatim; present & `!json.Valid` ⇒ `Parts` error (a corrupt
  **present** artifact is a defect — distinct from "absent", tolerated;
  **forward note 5**, NOT the DM-gate note 3).
- **`re_check_meta` — FIXED all-false default (D7, Option A).**
  `model.AgentInput` carries **no** `re_check_meta` source (only
  `ParentRiskAnalysis`, nil in BOTH the INITIAL and the
  RE_CHECK-parent-missing states), and `ai-agents-pipeline.md:1390`
  de-scopes the machine warnings map from Agent 8 entirely (the Result
  Aggregator owns it). `re_check_meta` is LLM-prose-tone context only;
  the safe in-spec default is the fixed
  `{"is_re_check":false,"parent_analysis_missing":false}` (both keys
  per `detailed_report.txt:41`). **No `model.AgentInput` change in this
  task** — accurate sourcing is owned downstream (**forward note 6**);
  a reviewer must NOT add a carrier here (the ratified
  "re-touching a reviewed shared surface = scope creep" + YAGNI
  stance). Pinned input-invariant (unchanged even by a non-nil
  `ParentRiskAnalysis`).
- **Strictness phase GROUPED-BY-CLASS, not envelope-mirror (D10 — the
  riskdetection CC-1 divergence, NOT the summary/recommendation CC-4
  envelope-mirror).** Agent 8 has FOUR strictness classes (more than
  Agent 5's three): (1) HARD pipeline-ordering MANDATORY
  `{Classification, KeyParameters, MandatoryConditions,
  MergedRiskAnalysis}`; (2) MANDATORY DM-artifact `{SEMANTIC_TREE
  json.Valid gate}`; (3) OPTIONAL upstream `{PartyConsistency
  tolerated-nil, Recommendations tolerated-empty}`; (4) OPTIONAL
  DM-artifact `{PROCESSING_WARNINGS}` + derived `{re_check_meta}`. The
  assembly phase keeps the 9-block envelope order; the strictness phase
  groups by class. A reviewer must NOT regroup it to envelope-mirror.
- **`Decode` — pure typed-unmarshal + closed-enum drift-guard, NO
  transform (D9).** base schema-validates against the embedded
  `detailed_report.json` BEFORE `Decode` (MF-1). Guards **both**
  exactly-equal closed-enum surfaces (the riskdetection-Level /
  partyconsistency-Severity rule — "guard EVERY surface whose Go
  whitelist EXACTLY equals the schema enum", NOT a one-guard quota):
  `sections[i].section_code` via `model.ReportSectionCode.IsValid()`
  (the 7-value whitelist exactly equals the §8 schema enum) and every
  **non-nil** `sections[i].items[j].severity` via
  `model.RiskLevel.IsValid()` (skip nil pointers — `null` is the
  schema's legitimate "no severity"; guarding it would wrongly reject
  schema-valid output, the Agent-4 boundary). Since BOTH enums are in
  `detailed_report.json`, a model-emitted out-of-enum value is
  schema-INVALID ⇒ base step-7 fires the sticky 1-shot **repair** (the
  Agent-4/5 repair-triggered class, the INVERSE of recommendation's
  terminal class — pinned by `TestRun_InvalidSectionCode_RepairTriggered`);
  the Decode guards are the build-defect BACKSTOP for the
  schema↔model-drift case repair cannot catch (terminal
  `INTERNAL_ERROR`). Deliberately **NOT** guarded (the enumerated
  house-style written record): `Title`/`Content` (free strings, schema
  `maxLength` only); `ClauseRef`/`LegalBasis`/`LinkedRiskID`/
  `LinkedRecommendation` (nullable free `*string`; `linked_risk_id`
  EXISTENCE is the Result Aggregator's job — the recommendation
  orphan-not-guarded precedent); **`Warnings` — passthrough VERBATIM**,
  never stripped/synthesised (`ai-agents-pipeline.md:1390` makes the
  Aggregator the SINGLE owner of the final merge; stripping would be a
  forbidden TRANSFORM — the ratified Agent-3/4/5/6/7 principle, forward
  note 6). Pinned: `null`-severity & orphan `linked_risk_id` decode
  clean; warnings NOT stripped.
- **Stateless / concurrency-safe `Spec`.** `detailedReporterSpec` is an
  empty struct with value-receiver methods; one `*DetailedReporter` is
  shared by the parallel errgroup pipeline (Stage 5: Agent 8 ‖ Agent 7)
  without locking (`TestRun_ConcurrentRaceClean`, `-race`).
- **`gofmt` self-check** via `go/format` (sandbox blocks `go fmt`).

## Forward notes (recorded, owners elsewhere)

1. **base/router `Config.Model` fallback defect (owner: LIC-TASK-024 /
   router / LIC-TASK-047).** `base.Run` sets `req.Model=cfg.Model`
   unconditionally and the router forwards it unchanged on fallback
   hops; passing the resolved primary-provider model here is correct on
   the primary path; the proper base/router fix is out of scope. Shared
   with the prior 7.
2. **Pipeline-ordering invariant (owner: LIC-TASK-034 Stage Executor) —
   DISTINCT from notes 3/4.** The Stage Executor MUST populate
   `in.Classification` (Agent 1), `in.KeyParameters` (Agent 2),
   `in.MandatoryConditions` (Agent 4) AND **`in.MergedRiskAnalysis`**
   (the LIC-TASK-035 Result Aggregator output, **NOT** the raw Agent-5
   `in.RiskAnalysis`) before dispatching Agent 8 (**Stage 5, parallel
   with Agent 7**). A nil any-of-four — or wiring only the raw field —
   is a Stage-Executor pipeline-wiring defect ⇒ `INTERNAL_ERROR`.
3. **Upstream DM artifact-bundle gate (owner: LIC-TASK-034 / the bundle
   gate) — DISTINCT from notes 2/4/5.** A genuinely missing/empty
   `SEMANTIC_TREE` is semantically `DM_ARTIFACTS_MISSING` (retryable),
   NOT `INTERNAL_ERROR`. That gate MUST reject it BEFORE Agent 8 runs;
   reaching `Parts`' tree check then means a true LIC invariant breach,
   for which base's `Parts`-error→`INTERNAL_ERROR` is the correct code.
   Identical in spirit to the prior 7's note.
4. **Agent-3 non-critical degradation tolerance (D4) — DISTINCT from
   notes 2/3.** `in.PartyConsistency` is OPTIONAL at Agent 8: Agent 3
   is non-critical (`error-handling.md:304`), so nil is in-spec →
   emitted as the typed zero-value sentinel, and MUST NOT trip the
   pipeline-ordering note 2 nor the DM-gate note 3. Parallel to
   `riskdetection`'s FN-5; a reviewer must not require PartyConsistency.
5. **OPTIONAL PROCESSING_WARNINGS tolerance (D6) — DISTINCT from note
   3.** §8-optional: absent/empty/`null` is in-spec (clean text PDF) →
   `[]`; only a present-but-malformed payload is a `Parts` error.
   Reuse of `riskdetection`'s FN-5.
6. **`re_check_meta` / `warnings` ownership (owner: LIC-TASK-034 Stage
   Executor / LIC-TASK-035 Result Aggregator).** Accurate
   `re_check_meta` sourcing (INITIAL vs RE_CHECK / parent-missing) and
   the machine `RE_CHECK_PARENT_ANALYSIS_MISSING` (and all
   `DetailedReport.warnings`) are owned downstream
   (`ai-agents-pipeline.md:1390`); Agent 8 emits the all-false default
   and `Decode` passes `warnings` through verbatim. A reviewer must NOT
   add a `model.AgentInput` carrier nor a Decode warnings-rewrite in
   this task.

## Forward requirement for LIC-TASK-047 (app-wiring)

Build `base.Deps` (the Router + Metrics/Tracer/Estimator/RepairMetrics
adapters) and call `NewDetailedReporter(resolvedPrimaryModel,
config.AgentsConfig.Timeouts[config.AgentDetailedReport], deps)`. The
resolved primary model = the configured primary provider's env-pinned
model (default claude ⇒ `LIC_CLAUDE_MODEL`). Register the returned
`*DetailedReporter` in the Stage Executor (LIC-TASK-034) by
`model.AgentDetailedReport` (**Stage 5, in PARALLEL with Agent 7 —
business summary**), AFTER the LIC-TASK-035 Result Aggregator merge has
populated `in.MergedRiskAnalysis`, with `in.Classification` +
`in.KeyParameters` + `in.MandatoryConditions` populated (forward note
2) and `in.PartyConsistency` / `in.Recommendations` populated when
available (forward notes 4/5 — tolerated absent).
