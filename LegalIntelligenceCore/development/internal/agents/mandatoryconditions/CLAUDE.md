# mandatoryconditions Package — CLAUDE.md

**Agent 4 — Legal Mandatory Conditions Checker** (LIC-TASK-028,
`ai-agents-pipeline.md` §4, `high-architecture.md` §6.6/§6.7.2). For the
contract type decided by Agent 1 it checks which обязательные /
существенные условия по ГК РФ are present (`FOUND_OK`), present but
ambiguously worded (`FOUND_AMBIGUOUS`) or absent (`MISSING`). The
per-contract-type catalogue of mandatory conditions is embedded in the §4
system prompt (no OPM/LKB in v1). Findings with status
`MISSING`/`FOUND_AMBIGUOUS` are later folded into `RISK_ANALYSIS.risks[]`
by the Result Aggregator (`high-architecture.md` §6.11.1).

The 4th of the 9 per-agent packages. A **thin** wrapper over the shared
BaseAgent runner (LIC-TASK-024): `Checker` embeds `*base.BaseAgent` (so
`ID()`/`Run()` make it a `port.Agent` for free); the only per-agent code
is the `Spec` — `Parts` (envelope) + `Decode` (typed result). The
invariant-heavy loop lives in `base`, once.

Constructor: `NewChecker(modelID string, timeout time.Duration, deps
base.Deps) (*Checker, error)` — stutter-free `NewTypeName`
(`feedback_constructors.md`); fail-fast via `base.NewBaseAgent`.

## Files

- **mandatoryconditions.go** — package doc, `Checker`, `NewChecker`, the
  §4 budget consts (`maxOutputTokens=3000`, `temperature=0.0`), the
  `var _ port.Agent` assertion, the base/router `Config.Model`-fallback
  forward note (shared with `typeclassifier` #1 / `keyparams` /
  `partyconsistency`).
- **spec.go** — `checkerSpec` (stateless), the minimal
  `classificationProjection{ContractType model.ContractType}`, `Parts`,
  `Decode`.
- **internal_test.go** — `TestHermeticImports` (exact allowlist) +
  `TestGofmtClean`.
- **mandatoryconditions_test.go** — constructor, fail-fast, 4-block
  envelope order, the byte-exact minimal classification projection (CC-1),
  whole-KeyParameters incl. `internal_extras` + nil-extras tolerated (D2),
  SEMANTIC_TREE byte-faithful passthrough + empty-tree tolerated, layer-2
  escaping, upstream-JSON injection neutralised (CC-2), full-text
  no-compaction, artifact/upstream strictness errors, `Decode` (dual
  `Code`+`Status` drift-guard, free `contract_type` not guarded), `Run`
  integration (acceptance Шаг 1/2), repair-triggered on a bad code,
  `-race`.

## API

- `NewChecker(modelID, timeout, base.Deps) (*Checker, error)`.
- `(*Checker)` satisfies `port.Agent` via the embedded `*base.BaseAgent`
  (`ID()==AGENT_MANDATORY_CONDITIONS`; `Run` returns
  `*model.MandatoryConditionsReport` or a `*model.DomainError`).

## Conventions & deliberate decisions (code-architect design review)

- **Hermetic.** Imports ONLY stdlib + `internal/domain/{model,port}` +
  `base` + `promptbuilder` + `prompts` + `schemas` + the shared
  `internal/agents/artifacts` (the LIC-TASK-026 steward decision). Like
  Agents 1/2 (and **unlike** Agent 3) `promptbuilder` is imported only
  for `Content` — Agent 4 mints **no** structural block. **No
  `internal/config`** (resolved per-agent values are constructor params;
  the config→value map is LIC-TASK-047 wiring's job — the
  `router.RouterConfig` precedent). **No `DocumentProcessing` module**
  (SEMANTIC_TREE is a byte-faithful passthrough; the `artifacts` package
  owns the local minimal DP-faithful EXTRACTED_TEXT struct).
  `TestHermeticImports` pins the allowlist.
- **§4 budget SSOT.** `MaxTokens=3000`, `Temperature=0.0` from
  `ai-agents-pipeline.md` §4 "Бюджеты и параметры LLM". Provider=Claude
  (sonnet) primary (`LIC_AGENT_MANDATORY_CONDITIONS_PROVIDER`);
  timeout=8s supplied by wiring from
  `LIC_AGENT_MANDATORY_CONDITIONS_TIMEOUT` (default `8s`,
  `configuration.md`; `config.AgentMandatoryConditions`);
  `Stage=model.StageAgentMandatoryConditions` (the `base.canonicalStage`
  table requires the exact pair or `NewBaseAgent` fails fast).
- **Envelope order = the §4 prompt (D5).** `Parts` returns exactly
  `Content("classification_result", …)` →
  `Content("key_parameters", …)` → `Content("semantic_tree", …)` →
  `Content("contract_document", …)` —
  `mandatory_conditions.txt:117-122`. Wrong order risks the model
  reading the tree/text as classification input. All four blocks are
  `promptbuilder.Content` (escaped — prompt-injection defence layer 2);
  `b` is unused (`_ *promptbuilder.Builder` — only Agent 3 mints).
- **`classification_result` = MINIMAL `{contract_type}` projection
  (D1).** `json.Marshal` of a local
  `classificationProjection{ContractType model.ContractType
  json:"contract_type"}` from `in.Classification.ContractType` →
  exactly `{"contract_type":"SUPPLY"}`. §4 "Зависимости" depends on the
  **field** `ClassificationResult.contract_type` (deliberately
  asymmetric with `KeyParameters`, taken WHOLE — D2), the prompt
  envelope literal is `{"contract_type":"…"}`, and
  confidence/alternatives/rationale are irrelevant to a
  mandatory-conditions check (token-budget discipline) — the exact
  analogue of Agent 3 projecting only `internal_extras.party_roles`. The
  field is typed `model.ContractType` (not `string`) so an enum rename
  is a compile error here too. The byte-exact single-key shape is pinned
  by `TestSpec_Parts_ClassificationResultMinimalProjection` (CC-1, the
  `partyconsistency` `representative`-tag-trap analogue). This is also
  the **lowest-risk** injection vector by design: only the typed enum is
  rendered, which cannot carry attacker bytes (CC-2).
- **`key_parameters` = the WHOLE KeyParameters JSON (D2).**
  `json.Marshal(in.KeyParameters)` as-is, **including `internal_extras`**
  when present — §4 "Зависимости" lists `KeyParameters` whole (no
  `.field` selector, the deliberate asymmetry with
  `ClassificationResult.contract_type`), and `key_parameters.go`'s godoc
  names Agent 4 a documented `internal_extras` consumer
  (`applicable_law`/`termination`/`acceptance_procedure` are first-order
  inputs). A non-nil KeyParameters with nil `InternalExtras` is
  TOLERATED: `omitempty` drops the key — a valid minimal Agent-2
  response (the `keyparams` Decode tolerance analogue), no
  special-casing.
- **SEMANTIC_TREE byte-faithful passthrough (D3, keyparams precedent).**
  `string(rawTree)` straight into `Content` — NOT decoded/re-encoded.
  The §4 agent cites tree node `id`s as `found_in` clause_ref, so
  pruning/re-keying would strip the very ids it must reference.
  Strictness gate = well-formedness (`json.Valid`), never a structural
  decode. Absent / empty bytes / `!json.Valid` ⇒ `Parts` error; an
  empty-but-well-formed tree (`{}`) is TOLERATED (emitted verbatim).
- **EXTRACTED_TEXT — full text, NO Agent-side compaction (D3, base
  MF-3).** Reuse the shared `artifacts.ExtractedText` + `FullText()`.
  **§4 specifies NO fixed/token-bounded head/tail rule** (unlike §1's
  Agent-1 "head 4000 + tail 1000"). The only truncation is
  LIC-TASK-021's GENERIC per-artifact rule UPSTREAM of `Spec.Parts` on
  `model.AgentInput`. Mandatory: absent / malformed JSON /
  decodes-to-empty-or-whitespace ⇒ `Parts` error.
- **Two-tier strictness; NO tolerated-empty upstream (D3).**
  `in.Classification == nil` OR `in.KeyParameters == nil` is a
  **pipeline-ordering** invariant breach (Agents 1/2 are Stage 1, Agent
  4 is Stage 3 — see forward note 2) ⇒ `Parts` error ⇒
  `INTERNAL_ERROR`. Unlike Agent 3's empty-`party_roles`
  "supplementary signal" tolerance, Agent 4 has **no** tolerated-empty
  upstream case: `contract_type` is the literal driver of *which*
  mandatory-condition catalogue applies and the §4 prompt has no
  "(если есть)" hedge. The contract_type **value** is trusted as Agent
  1's typed, drift-guarded output (the Agent-3 "trust the upstream typed
  result" precedent — not re-validated in `Parts`). Strictness-check
  order mirrors the envelope (Classification → KeyParameters →
  SEMANTIC_TREE → EXTRACTED_TEXT) so the two **distinct** forward-note
  classes (pipeline-ordering note 2 vs. DM-artifact-gate note 3) stay
  contiguous and legible in the code itself (CC-4); the assembly/marshal
  phase runs only after all four pass.
- **`Decode` drift-guard: BOTH `Code` and `Status` per condition (D4 /
  CC-3).** base schema-validates against the embedded
  `mandatory_conditions.json` BEFORE `Decode` (MF-1). For every
  `Conditions[i]`: `model.IsValidMandatoryConditionCode(c.Code)` (the
  FROZEN `^MC_[A-Z0-9_]+$` SSOT regex) AND `c.Status.IsValid()`
  (`model.MandatoryConditionStatus`, the closed 3-value enum) are
  re-checked; a miss ⇒ `INTERNAL_ERROR` build-defect (the enumerated
  schema-↔-Go cross-check house style; cf. `base.canonicalStage`,
  classifier's `ContractType.IsValid`, keyparams' `PartyRoleType`,
  partyconsistency's dual finding guard). **`Code` is NOT free-string
  over-reach (CC-3):** it has a closed *structural* SSOT (a regex)
  duplicated into the schema's `pattern`, so the guard is the EXACT same
  schema↔Go cross-check class as an enum guard, **and** it directly
  discharges acceptance test_step 2 ("code соответствует regex
  ^MC_[A-Z0-9_]+$") in Go, not only via the schema.
  `MandatoryConditionsReport.ContractType` is deliberately **NOT**
  guarded: the §4 schema leaves it a bare `{"type":"string"}` (the model
  may echo `OTHER` or a refined string) and the model field is plain
  `string` — no closed value-set, no SSOT regex; guarding it would
  over-reach **and** wrongly reject schema-valid output. Note the
  deliberate asymmetry with `Parts`: `Parts` *renders* `contract_type`
  from the typed `model.ContractType` enum (Agent 1's drift-guarded
  output), but `Decode` does **not** re-validate the model's *returned*
  `contract_type`. `Label`/`LegalBasis`/`IssueDescription`/`Summary`/
  `PromptInjectionDetected` are likewise structurally free /
  model-reported and NOT guarded. No `Status`/`Code` re-mapping — Decode
  is a pure typed-unmarshal + drift-guard, never a transform.
- **No INN/OGRN handling, no `<validation_facts>` (CC-6).** Agent 4
  passes `KeyParameters.internal_extras` (incl. raw, unvalidated
  `party_roles[].INN/OGRN`) through verbatim as part of the
  whole-KeyParameters block. Checksum validation and the
  `<validation_facts>` mint are **solely Agent 3's** pre-LLM role; a
  future reviewer should NOT expect a `validation_facts` block here nor
  mistake the raw identifiers for an Agent-4 omission.
- **Stateless / concurrency-safe `Spec`.** `checkerSpec` is an empty
  struct with value-receiver methods; one `*Checker` is shared by the
  parallel errgroup pipeline without locking
  (`TestRun_ConcurrentRaceClean`, `-race`).
- **`gofmt` self-check** via `go/format` (sandbox blocks `go fmt`).

## Forward notes (recorded, owners elsewhere)

1. **base/router `Config.Model` fallback defect (owner: LIC-TASK-024 /
   router / LIC-TASK-047).** `base.Run` sets `req.Model=cfg.Model`
   unconditionally and the router forwards it unchanged on fallback
   hops; passing the resolved primary-provider model here is correct on
   the primary path; the proper base/router fix is out of scope. Shared
   with `typeclassifier` #1 / `keyparams` / `partyconsistency`.
2. **Pipeline-ordering invariant (owner: LIC-TASK-034 Stage Executor) —
   DISTINCT from note 3.** The Stage Executor MUST populate
   `in.Classification` (from Agent 1's Stage-1 result) AND
   `in.KeyParameters` (from Agent 2's Stage-1 result) before dispatching
   Agent 4 (Stage 3). A nil either at Agent 4 is a Stage-Executor
   pipeline-wiring defect, correctly projected to `INTERNAL_ERROR` by
   base. (This is NOT the DM-artifact gate of note 3 — different
   invariant, do not collapse.)
3. **Upstream DM artifact-bundle gate (owner: LIC-TASK-034 / the bundle
   gate) — DISTINCT from note 2.** A genuinely missing/empty mandatory
   DM artifact (SEMANTIC_TREE / EXTRACTED_TEXT) is semantically
   `DM_ARTIFACTS_MISSING` (retryable), NOT `INTERNAL_ERROR`. That gate
   MUST reject it BEFORE Agent 4 runs; reaching `Parts`' artifact checks
   then means a true LIC invariant breach, for which base's
   `Parts`-error→`INTERNAL_ERROR` is the correct code. Identical in
   spirit to `typeclassifier` #2 / `keyparams` / `partyconsistency` #3.
4. **Token-budget head/tail (owner: LIC-TASK-021).** Agent 4
   deliberately emits the FULL `EXTRACTED_TEXT`. §4 specifies no
   fixed/token-bounded compaction; the only truncation is 021's generic
   upstream per-artifact rule on `model.AgentInput` (base MF-3), NOT
   this `Spec.Parts`.

## Forward requirement for LIC-TASK-047 (app-wiring)

Build `base.Deps` (the Router + Metrics/Tracer/Estimator/RepairMetrics
adapters) and call `NewChecker(resolvedPrimaryModel,
config.AgentsConfig.Timeouts[config.AgentMandatoryConditions], deps)`.
The resolved primary model = the configured primary provider's env-pinned
model (default claude ⇒ `LIC_CLAUDE_MODEL`). Register the returned
`*Checker` in the Stage Executor (LIC-TASK-034) by
`model.AgentMandatoryConditions` (Stage 3, parallel with Agent 5 —
`riskdetection`, after Stage 1's Agent 1 + Agent 2 with `in.Classification`
and `in.KeyParameters` populated — forward note 2).
