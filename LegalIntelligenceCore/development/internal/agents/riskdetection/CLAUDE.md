# riskdetection Package — CLAUDE.md

**Agent 5 — Risk Detection & Severity Scoring** (LIC-TASK-029,
`ai-agents-pipeline.md` §5, `high-architecture.md` §6.6/§6.7.2). The
CENTRAL agent: it scans the contract for risky constructions and assigns
each a severity (`high`|`medium`|`low`), forming the primary
`RISK_ANALYSIS.risks[]` array. The Result Aggregator (LIC-TASK-035) later
folds Agent 3 (party-consistency) findings as `R-PNNN` and Agent 4
(mandatory-conditions) findings as `R-MNNN` into this same `risks[]`
slice (`high-architecture.md` §6.11.1); Agent 5 itself emits only the
bare `R-NNN` namespace.

The 5th of the 9 per-agent packages. A **thin** wrapper over the shared
BaseAgent runner (LIC-TASK-024): `Detector` embeds `*base.BaseAgent` (so
`ID()`/`Run()` make it a `port.Agent` for free); the only per-agent code
is the `Spec` — `Parts` (envelope) + `Decode` (typed result). The
invariant-heavy loop lives in `base`, once.

Constructor: `NewDetector(modelID string, timeout time.Duration, deps
base.Deps) (*Detector, error)` — stutter-free `NewTypeName`
(`feedback_constructors.md`); fail-fast via `base.NewBaseAgent`.

## Files

- **riskdetection.go** — package doc, `Detector`, `NewDetector`, the §5
  budget consts (`maxOutputTokens=3500`, `temperature=0.0`), the
  `var _ port.Agent` assertion, the base/router `Config.Model`-fallback
  forward note (shared with `typeclassifier` #1 / `keyparams` /
  `partyconsistency` / `mandatoryconditions`).
- **spec.go** — `detectorSpec` (stateless), the `emptyProcessingWarnings`
  `[]` sentinel, the minimal `classificationProjection{ContractType
  model.ContractType}`, `Parts`, `Decode`.
- **internal_test.go** — `TestHermeticImports` (allowlist
  **byte-identical** to `mandatoryconditions`') + `TestGofmtClean`.
- **riskdetection_test.go** — constructor, fail-fast, the 5-block
  envelope order, the byte-exact minimal classification projection,
  whole-KeyParameters incl. `internal_extras` + nil-extras tolerated,
  the PROCESSING_WARNINGS tiers (verbatim / absent·empty·whitespace·`null`
  → exactly `[]` / present-malformed → error / absent ≠ error),
  SEMANTIC_TREE byte-faithful passthrough + empty-tree tolerated, layer-2
  escaping, upstream-JSON injection neutralised, full-text no-compaction,
  artifact/upstream strictness errors, `Decode` (Level-only drift-guard;
  free/omitted/merged-enum category & free id tolerated), the
  prompt-driven `PROMPT_INJECTION_ATTEMPT` passthrough (D5), `Run`
  integration (acceptance Шаг 1; Шаг 2 monotonic ids R-001..R-003),
  repair-triggered on a bad level, `-race`.

## API

- `NewDetector(modelID, timeout, base.Deps) (*Detector, error)`.
- `(*Detector)` satisfies `port.Agent` via the embedded `*base.BaseAgent`
  (`ID()==AGENT_RISK_DETECTION`; `Run` returns `*model.RiskAnalysis` or a
  `*model.DomainError`).

## Conventions & deliberate decisions (code-architect design review)

- **Hermetic (CC-4).** Imports ONLY stdlib + `internal/domain/{model,port}`
  + `base` + `promptbuilder` + `prompts` + `schemas` + the shared
  `internal/agents/artifacts` — **byte-identical** to
  `mandatoryconditions`' allowlist. Like Agents 1/2/4 (and **unlike**
  Agent 3) `promptbuilder` is imported only for `Content` — Agent 5 mints
  **no** structural block. **No `internal/config`** (resolved per-agent
  values are constructor params; LIC-TASK-047's job — the
  `router.RouterConfig` precedent). **No `DocumentProcessing` module**:
  BOTH SEMANTIC_TREE **and** the optional PROCESSING_WARNINGS are
  byte-faithful passthroughs. The deliberate **absence** of a local
  `processingWarnings` mirror struct (cf. Agent 3's local
  `documentStructure`) is the **SEMANTIC_TREE-passthrough class, not the
  DOCUMENT_STRUCTURE-local-decode class** — not an omission; do not add
  one. `TestHermeticImports` pins the allowlist.
- **§5 budget SSOT (CC-5).** `MaxTokens=3500`, `Temperature=0.0` from
  `ai-agents-pipeline.md` §5 "Бюджеты и параметры LLM". Provider=Claude
  (sonnet) primary (`LIC_AGENT_RISK_DETECTION_PROVIDER`); timeout=12s
  supplied by wiring from `LIC_AGENT_RISK_DETECTION_TIMEOUT` (default
  `12s`, `configuration.md`; `config.AgentRiskDetection`);
  `Stage=model.StageAgentRiskDetection` (the `base.canonicalStage` table
  requires the exact pair or `NewBaseAgent` fails fast). Agent 5 is
  **Stage 3, parallel with Agent 4** (`mandatoryconditions`).
- **Envelope order = the §5 prompt (D1).** `Parts` returns exactly
  `Content("classification_result", …)` → `Content("key_parameters", …)`
  → `Content("processing_warnings", …)` → `Content("semantic_tree", …)`
  → `Content("contract_document", …)` — `risk_detection.txt:52-58`.
  Wrong order risks the model reading the tree/text as classification
  input. All five blocks are `promptbuilder.Content` (escaped — layer 2);
  `b` is unused (`_ *promptbuilder.Builder` — only Agent 3 mints).
- **`classification_result` = MINIMAL `{contract_type}` projection (D1)**
  — verbatim the Agent-4 decision. `json.Marshal` of a local
  `classificationProjection{ContractType model.ContractType
  json:"contract_type"}` from `in.Classification.ContractType` → exactly
  `{"contract_type":"SUPPLY"}`. Typed enum (not `string`) so a rename is
  a compile error; byte-exact single-key shape pinned. Lowest-risk
  injection vector (only the typed enum is rendered).
- **`key_parameters` = the WHOLE KeyParameters JSON (D1)** — verbatim
  Agent 4. `json.Marshal(in.KeyParameters)` as-is, **including
  `internal_extras`** when present (the deliberate asymmetry with the
  projected `classification_result`); a non-nil KeyParameters with nil
  `InternalExtras` is TOLERATED (`omitempty` drops the key).
- **SEMANTIC_TREE byte-faithful passthrough (D2, keyparams/Agent-4
  precedent).** `string(rawTree)` straight into `Content` — NOT
  decoded/re-encoded. §5 correctness criterion 3
  (`risk_detection.txt:71`) makes `clause_ref` cite tree node `id`s, so
  pruning/re-keying would strip the very ids it must reference.
  Strictness gate = well-formedness (`json.Valid`). Absent / empty bytes
  / `!json.Valid` ⇒ `Parts` error; an empty-but-well-formed tree (`{}`)
  is TOLERATED (emitted verbatim).
- **EXTRACTED_TEXT — full text, NO Agent-side compaction (D2, base
  MF-3).** Reuse the shared `artifacts.ExtractedText` + `FullText()`.
  §5 specifies **no** fixed/token-bounded head/tail rule (unlike §1's
  Agent-1 "head 4000 + tail 1000"); the only truncation is
  LIC-TASK-021's GENERIC per-artifact rule UPSTREAM of `Spec.Parts` on
  `model.AgentInput`. Mandatory: absent / malformed JSON /
  decodes-to-empty-or-whitespace ⇒ `Parts` error.
- **`processing_warnings` — OPTIONAL, byte-faithful passthrough,
  `[]`-normalised (D3 + CC-2/CC-3).** §5 marks PROCESSING_WARNINGS
  explicitly OPTIONAL (`<!-- от DP, опционально -->`
  `risk_detection.txt:55`; "Если присутствует processing_warnings"
  `risk_detection.txt:60`) — a clean text PDF legitimately has none.
  The §5 envelope is kept a **fixed 5-block shape** regardless of
  presence (the `partyconsistency` CHANGE-2 fixed-N-block precedent).
  Tiers:
  - absent key / empty bytes / whitespace-only / the bare JSON `null`
    token ⇒ normalised to the literal **`[]`** (`emptyProcessingWarnings`)
    — NOT `{}`/`null`/`""`: the prompt illustrates the block as `[…]`
    (an array), and `{}`/`null` would invite the model to treat the
    payload as degenerate and spuriously bump every risk to
    `level=medium` (`risk_detection.txt:60-62` ties the
    medium-escalation to the **presence** of warnings). This is the
    `partyconsistency` "never emit JSON `null`, always a well-formed
    `[]` the model can read" ratified rule (CC-3). `json.Valid("null")`
    is `true`, so the bare-`null`-token fold is explicit (CC-2).
  - present & `json.Valid` ⇒ **byte-faithful passthrough verbatim** (the
    SEMANTIC_TREE precedent — Agent 5 never cites ids from it; it is
    OCR-quality context only).
  - present & `!json.Valid` ⇒ `Parts` error (a corrupt **present**
    artifact is a defect — the SEMANTIC_TREE well-formedness-gate
    precedent; **distinct** from "absent", tolerated here because §5
    marks it optional and §4's tree was not). **Absence is NOT a
    DM-artifact-gate concern** (forward note 5, NOT note 3 — note 3
    covers only the MANDATORY tree/text).
- **Strictness phase grouped BY CLASS, not by envelope order (CC-1 —
  deliberate divergence from Agent 4).** `mandatoryconditions` CC-4
  could mirror the envelope order in the strictness phase only because
  all four of its blocks were mandatory. Agent 5 has **three** distinct
  strictness classes (pipeline-ordering note 2 → mandatory DM-gate note
  3 → optional PROCESSING_WARNINGS note 5), so the strictness phase
  groups by class while the **assembly phase keeps the envelope order**
  (`classification_result` → `key_parameters` → `processing_warnings` →
  `semantic_tree` → `contract_document`). A future reviewer must NOT
  "fix" the strictness order back to strict envelope-mirroring.
- **Two-tier upstream strictness; NO tolerated-empty upstream (D1).**
  `in.Classification == nil` OR `in.KeyParameters == nil` is a
  **pipeline-ordering** invariant breach (Agents 1/2 Stage 1, Agent 5
  Stage 3 — forward note 2) ⇒ `Parts` error ⇒ `INTERNAL_ERROR`. No
  tolerated-empty case: `contract_type` drives the risk taxonomy and the
  §5 prompt has no "(если есть)" hedge. The `contract_type` **value** is
  trusted as Agent 1's typed, drift-guarded output (not re-validated).
- **`Decode` drift-guard: `Risk.Level` ONLY (D4 / CC-6).** base
  schema-validates against the embedded `risk_detection.json` BEFORE
  `Decode` (MF-1). For every `Risks[i]`: `model.RiskLevel.IsValid()`
  (`high|medium|low`, **exactly equal** to the §5 schema enum — the
  `partyconsistency` `f.Severity` precedent, same `model.RiskLevel`
  type) is the SOLE guarded surface; a miss ⇒ `INTERNAL_ERROR`
  build-defect. Deliberately **NOT** guarded, each for a ratified reason
  (the `mandatoryconditions` enumerated-unguarded-fields house style;
  CC-6):
  - **`Risk.Category`** — §5 schema makes it OPTIONAL (not in
    `required[]`) and uses the NARROW 13-value Agent-5 subset, whereas
    `model.RiskCategory.IsValid()` is the BROADER 22-value MERGED
    OUTBOUND enum (looser than the input schema → not a faithful
    cross-check; **and** optional ⇒ an omitted category decodes to `""`
    which fails `IsValid()` ⇒ would wrongly reject schema-valid output).
    The exact Agent-4 `MandatoryConditionsReport.ContractType` boundary.
  - **`Risk.ID`** — §5 schema pattern is `^R-[0-9]{3,}$`, but
    `model.IsValidRiskID` is the MERGED `^R-(P|M)?[0-9]{3,}$` (admits
    `R-PNNN`/`R-MNNN` §5 FORBIDS) — strictly LOOSER than the agent
    schema. Unlike Agent 4 (whose frozen
    `model.IsValidMandatoryConditionCode` EQUALS its schema pattern),
    **no** model SSOT regex equals `^R-[0-9]{3,}$`; a hand-rolled local
    regex would be a new un-SSOT'd duplicate — over-reach past the
    closed-enum/frozen-SSOT boundary. The schema `pattern` (base,
    pre-Decode, MF-1) is the id-format SSOT.
  - `Description`/`ClauseRef`/`LegalBasis` (free strings),
    `Rationale`/`Summary` (nullable `*string`),
    `MandatoryConditionCode` (`*string`, Aggregator-only),
    `PromptInjectionDetected` (plain model-reported `bool`) — all
    structurally free / model-reported / downstream-owned; guarding any
    would over-reach. `Decode` is a pure typed-unmarshal +
    single-enum drift-guard, **never** a transform / re-map / synthesis.
- **`prompt_injection_detected` → `PROMPT_INJECTION_ATTEMPT` risk is
  PROMPT-DRIVEN, never Go-synthesized (D5).** The §5 prompt's ЗАЩИТА ОТ
  ИНСТРУКЦИЙ guard (`risk_detection.txt:79-82`) AND the §0.3 general
  guard already instruct the LLM to set
  `prompt_injection_detected=true` AND add a
  `category=PROMPT_INJECTION_ATTEMPT level=medium` risk. The acceptance
  criterion "При prompt_injection_detected=true агент также добавляет
  риск …" describes agent behavior **realized via the system prompt** —
  the exact Agent-4 "per-contract-type catalogue embedded in the §4
  system prompt, not Go" precedent. `Decode` does NOT synthesize/enforce
  that risk: doing so would be a TRANSFORM (the ratified Agent-3/4 "never
  a transform" principle), risk duplicate ids / break the monotonic
  `R-NNN` counter the LLM owns, and contradict the
  schema-validated-bytes-in contract. Any cross-cutting injection-flag
  handling is downstream: the Result Aggregator (LIC-TASK-035) is the
  SINGLE site that converts the flag into
  `DETAILED_REPORT.warnings.PROMPT_INJECTION_DETECTED` and drops it from
  `risks[]` (`ai-agents-pipeline.md` §5 post-processing). Pinned as a
  pure passthrough by
  `TestSpec_Decode_PromptInjectionRiskIsPromptDrivenPassthrough`.
- **Stateless / concurrency-safe `Spec`.** `detectorSpec` is an empty
  struct with value-receiver methods; one `*Detector` is shared by the
  parallel errgroup pipeline without locking
  (`TestRun_ConcurrentRaceClean`, `-race`).
- **`gofmt` self-check** via `go/format` (sandbox blocks `go fmt`).

## Forward notes (recorded, owners elsewhere)

1. **base/router `Config.Model` fallback defect (owner: LIC-TASK-024 /
   router / LIC-TASK-047).** `base.Run` sets `req.Model=cfg.Model`
   unconditionally and the router forwards it unchanged on fallback
   hops; passing the resolved primary-provider model here is correct on
   the primary path; the proper base/router fix is out of scope. Shared
   with `typeclassifier` #1 / `keyparams` / `partyconsistency` /
   `mandatoryconditions`.
2. **Pipeline-ordering invariant (owner: LIC-TASK-034 Stage Executor) —
   DISTINCT from note 3.** The Stage Executor MUST populate
   `in.Classification` (Agent 1 Stage-1) AND `in.KeyParameters` (Agent 2
   Stage-1) before dispatching Agent 5 (Stage 3, **parallel with Agent
   4**). A nil either at Agent 5 is a Stage-Executor pipeline-wiring
   defect, correctly projected to `INTERNAL_ERROR` by base. (NOT the
   DM-artifact gate of note 3 — do not collapse.)
3. **Upstream DM artifact-bundle gate (owner: LIC-TASK-034 / the bundle
   gate) — DISTINCT from notes 2 & 5.** A genuinely missing/empty
   **MANDATORY** DM artifact (SEMANTIC_TREE / EXTRACTED_TEXT) is
   semantically `DM_ARTIFACTS_MISSING` (retryable), NOT `INTERNAL_ERROR`.
   That gate MUST reject it BEFORE Agent 5 runs; reaching `Parts`'
   artifact checks then means a true LIC invariant breach, for which
   base's `Parts`-error→`INTERNAL_ERROR` is the correct code. The
   OPTIONAL PROCESSING_WARNINGS is explicitly OUT of this gate (note 5).
4. **Token-budget head/tail (owner: LIC-TASK-021).** Agent 5
   deliberately emits the FULL `EXTRACTED_TEXT`. §5 specifies no
   fixed/token-bounded compaction; the only truncation is 021's generic
   upstream per-artifact rule on `model.AgentInput` (base MF-3), NOT
   this `Spec.Parts`.
5. **OPTIONAL PROCESSING_WARNINGS tolerance (Agent-5-specific; NOT note
   3).** PROCESSING_WARNINGS is §5-optional: ABSENT/empty/`null` is a
   normal in-spec state (clean text PDF), TOLERATED → emitted as `[]`,
   and MUST NOT trip the DM-artifact gate (note 3) nor be a `Parts`
   error. Only a **present-but-malformed** payload is a `Parts` error.
   Parallel to `partyconsistency`'s empty-`party_roles` tolerance note,
   NOT to note 3 — a future reviewer must not require the bundle gate to
   hard-require PROCESSING_WARNINGS.
6. **`prompt_injection_detected` flag post-processing (owner:
   LIC-TASK-035 Result Aggregator).** Agent 5 passes the flag and any
   LLM-emitted `PROMPT_INJECTION_ATTEMPT` risk through verbatim. The
   Aggregator is the SINGLE site that converts the flag into the
   `DETAILED_REPORT.warnings.PROMPT_INJECTION_DETECTED` warning and
   strips it from the published `risks[]` (`ai-agents-pipeline.md` §5).

## Forward requirement for LIC-TASK-047 (app-wiring)

Build `base.Deps` (the Router + Metrics/Tracer/Estimator/RepairMetrics
adapters) and call `NewDetector(resolvedPrimaryModel,
config.AgentsConfig.Timeouts[config.AgentRiskDetection], deps)`. The
resolved primary model = the configured primary provider's env-pinned
model (default claude ⇒ `LIC_CLAUDE_MODEL`). Register the returned
`*Detector` in the Stage Executor (LIC-TASK-034) by
`model.AgentRiskDetection` (Stage 3, **parallel with Agent 4 —
`mandatoryconditions`**, after Stage 1's Agent 1 + Agent 2 with
`in.Classification` and `in.KeyParameters` populated — forward note 2).
