# partyconsistency Package — CLAUDE.md

**Agent 3 — Party Data Consistency** (LIC-TASK-027,
`ai-agents-pipeline.md` §3, `high-architecture.md` §6.6/§6.7.2). Checks
the consistency/completeness of the contracting parties' details:
requisites, names, ИНН/ОГРН (formal validation — length, control digits,
mutual consistency), addresses, signatory authority, and divergences
across parts of the document. The deterministic ИНН/ОГРН control-digit
validation is **pre-LLM, LLM-free**, performed by the Prompt Builder and
handed to the agent as the `<validation_facts>` ground-truth block —
Agent 3 is the **sole v1** consumer of
`promptbuilder.Builder.ValidationFacts` (a future cross-agent
verification task may add a second — forward note 5).

The 3rd of the 9 per-agent packages. A **thin** wrapper over the shared
BaseAgent runner (LIC-TASK-024): `Checker` embeds `*base.BaseAgent` (so
`ID()`/`Run()` make it a `port.Agent` for free); the only per-agent code
is the `Spec` — `Parts` (envelope, which **mints** `<validation_facts>`)
+ `Decode` (typed result). The invariant-heavy loop lives in `base`, once.

Constructor: `NewChecker(modelID string, timeout time.Duration, deps
base.Deps) (*Checker, error)` — stutter-free `NewTypeName`
(`feedback_constructors.md`); fail-fast via `base.NewBaseAgent`.

## Files

- **partyconsistency.go** — package doc, `Checker`, `NewChecker`, the §3
  budget consts (`maxOutputTokens=1000`, `temperature=0.0`), the
  `var _ port.Agent` assertion, the base/router `Config.Model`-fallback
  forward note (shared with `typeclassifier` #1 / `keyparams`).
- **spec.go** — `checkerSpec` (stateless), the local minimal
  `documentStructure{PartyDetails []partyDetails}` decode, the nil-safe
  `derefString`, `Parts`, `Decode`.
- **internal_test.go** — `TestHermeticImports` (exact allowlist incl.
  `artifacts`+`promptbuilder`) + `TestGofmtClean`.
- **partyconsistency_test.go** — constructor, fail-fast, envelope order +
  the minted `validation_facts`, checksum integration + nil-INN/OGRN
  no-panic, layer-2 escaping, full-text no-compaction, tolerated
  empty/absent inputs, artifact/upstream strictness errors, the
  `representative` json-tag round-trip (CC-3), `Decode` (dual enum
  drift-guard, `low`-severity tolerated, no re-map), `Run` integration
  (acceptance Шаг 1/2), repair-triggered, `-race`.

## API

- `NewChecker(modelID, timeout, base.Deps) (*Checker, error)`.
- `(*Checker)` satisfies `port.Agent` via the embedded `*base.BaseAgent`
  (`ID()==AGENT_PARTY_CONSISTENCY`; `Run` returns
  `*model.PartyConsistencyFindings` or a `*model.DomainError`).

## Conventions & deliberate decisions (code-architect design review)

- **Hermetic.** Imports ONLY stdlib + `internal/domain/{model,port}` +
  `base` + `promptbuilder` + `prompts` + `schemas` + `artifacts`. Unlike
  Agents 1/2, `promptbuilder` is imported for a **structural** purpose
  (`b.ValidationFacts` + `promptbuilder.Party`), not merely `Content` —
  the mandated §3 difference, anticipated by `base.go`'s `Spec` godoc.
  **No `internal/config`** (resolved per-agent values are constructor
  params; the config→value map is LIC-TASK-047 wiring's job — the
  `router.RouterConfig` precedent). **No `DocumentProcessing` module**
  (the local `documentStructure` mirror owns the DP-faithful
  `party_details` shape). `TestHermeticImports` pins the allowlist
  (code-architect CC-2 — without it the no-cross-module rule that
  justifies the local decode is unenforced).
- **§3 budget SSOT (CC-4).** `MaxTokens=1000`, `Temperature=0.0` from
  `ai-agents-pipeline.md` §3 "Бюджеты и параметры LLM". Provider=Claude
  (sonnet) primary (`LIC_AGENT_PARTY_CONSISTENCY_PROVIDER`); timeout=6s
  supplied by wiring from `LIC_AGENT_PARTY_CONSISTENCY_TIMEOUT` (default
  `6s`, `configuration.md`); `Stage=model.StageAgentPartyConsistency`
  (the `base.canonicalStage` table requires the exact pair or
  `NewBaseAgent` fails fast).
- **Envelope order = the §3 prompt (Q4).** `Parts` returns exactly
  `Content("party_roles", rolesJSON)` → `factsPart` (minted) →
  `Content("party_details_block", pdJSON)` → `Content("contract_document",
  fullText)` — `party_consistency.txt:595-604`. The `ValidationFacts`
  Part sits **between** `<party_roles>` and `<party_details_block>`
  (`base/CLAUDE.md` forward-req #2). `validation_facts` is a *minted*
  Part (closed `promptbuilder.Part` type), exempt from Build's tag
  regexp/dedup; the three user-controlled blocks go through
  `promptbuilder.Content` (escaped — prompt-injection defence layer 2).
- **`b.ValidationFacts` called ONLY on the happy path, after ALL
  strictness checks (Q4).** Every input-strictness error (nil
  `KeyParameters`, absent/empty/malformed DOCUMENT_STRUCTURE,
  absent/malformed/empty EXTRACTED_TEXT) returns **before** the first
  `b.` dereference, so the keyparams-style nil-Builder error-case test
  (`checkerSpec{}.Parts(nil, badInput)`) still surfaces the strictness
  error without a nil-pointer panic.
- **`[]PartyValidation` discarded in v1 (Q4 / YAGNI).** The model
  consumes the `<validation_facts>` XML; the structured results exist
  only for a **future** cross-agent verification
  (`promptbuilder.PartyValidation` godoc). No in-process v1 consumer ⇒
  discard (the documented-YAGNI house style; recorded as a forward note,
  not a silent drop).
- **Empty-roles path is NOT special-cased (CHANGE-2).**
  `ValidationFacts([])` naturally mints exactly
  `<validation_facts></validation_facts>`; the empty `[]Party` flows
  through it. A hand-crafted empty block is impossible anyway (the `Part`
  type is closed). The envelope's fixed 4-block shape stays invariant
  regardless of party count.
- **`derefString` is nil-safe (CHANGE-1).** `model.PartyRole.INN/OGRN`
  are `*string`, wire-nullable (serialised as `null` when unset, never
  omitted — §2 schema). A nil ⇒ `""`, which is exactly
  `promptbuilder.Party`'s "empty ⇒ not present (not invalid)" contract,
  so an absent identifier is neither rendered into `<validation_facts>`
  nor counted by `lic_party_validation_total`. A blind `*p` would panic
  on that common legal path — pinned by the nil-INN/OGRN party in
  `TestSpec_Parts_ValidationFacts`.
- **party_roles & party_details rendered as JSON via `Content` (Q2).**
  `json.Marshal(in.KeyParameters.internal_extras.party_roles)` and
  `json.Marshal(documentStructure.party_details)`, both layer-2 escaped.
  A nil/empty slice is normalised to a non-nil empty slice so the literal
  `[]` is emitted (never JSON `null`) — a well-formed empty array the
  model can read.
- **DOCUMENT_STRUCTURE decode is LOCAL, not in `artifacts` (Q2).** The
  shared `artifacts` package deliberately centralises **only**
  EXTRACTED_TEXT (its page-join must stay byte-identical to DP, asserted
  once by `TestFullText_DPSemantics`). DOCUMENT_STRUCTURE is a flat
  passthrough with no reconstruction invariant, and Agent 1 already reads
  a **disjoint** projection (`sections[].title`). A per-agent local
  minimal `documentStructure{PartyDetails []partyDetails}` is the
  ratified v1 pattern (`artifacts/CLAUDE.md` scope boundary) — the exact
  analogue of `typeclassifier`'s local `documentStructure{Sections}`.
- **`partyDetails` json tag is `representative`, NOT `signatory` (CC-3).**
  It must match the DP wire SSOT
  (`DocumentProcessing/.../structure.go:31-38`). `signatory` is the
  DISTINCT `model.PartyRole` field name; a wrong tag would silently
  decode to `""` with no error — a build-defect-class trap. Pinned by
  `TestSpec_Parts_PartyDetailsRepresentativeTag` (real DP wire fixture
  round-trip).
- **DOCUMENT_STRUCTURE mandatory-in-shape; empty party_details tolerated
  (Q2).** Absent / empty bytes / malformed JSON ⇒ `Parts` error ⇒ base
  `INTERNAL_ERROR`; an absent/empty `party_details` list ⇒ `[]` (the
  prompt's "(если есть)"). Exact mirror of Agent 1's mandatory-in-shape
  DOCUMENT_STRUCTURE with a tolerated-empty projection.
- **party_roles source & two-tier strictness (Q1).** Source =
  `in.KeyParameters.internal_extras.party_roles`. A nil
  `in.KeyParameters` is a **pipeline-ordering** invariant breach (see
  forward note 2) ⇒ `Parts` error ⇒ `INTERNAL_ERROR`. A non-nil
  `KeyParameters` with nil `InternalExtras` OR empty `PartyRoles` is
  **TOLERATED** (the typeclassifier empty-DOCUMENT_STRUCTURE / keyparams
  nil-internal_extras analogue): party_roles is a supplementary
  structured signal; the model still reads
  `party_details_block` + `contract_document`.
- **EXTRACTED_TEXT — full text, NO Agent-side compaction (Q3, base
  MF-3).** Reuse the shared `artifacts.ExtractedText` + `FullText()`.
  **§3 specifies NO fixed-size or token-bounded head/tail rule** (unlike
  §1's Agent-1 "head 4000 + tail 1000"); "фрагменты с реквизитами" is
  descriptive prose, not a deterministic slicing instruction. The only
  truncation is LIC-TASK-021's GENERIC per-artifact rule UPSTREAM of
  `Spec.Parts` on `model.AgentInput`. Carrying an interim truncation here
  would be a second divergent truncation site 021 must later find/remove.
  Mandatory: absent / malformed JSON / decodes-to-empty-or-whitespace ⇒
  `Parts` error.
- **`Decode` drift-guard: BOTH enum surfaces per finding (Q5).** A
  `PartyFinding` has two closed-enum surfaces. For every finding
  `f.Type.IsValid()` (`model.PartyFindingType`, 7-value) AND
  `f.Severity.IsValid()` (`model.RiskLevel`, high|medium|low) are
  re-checked; a miss ⇒ `INTERNAL_ERROR` build-defect (the enumerated
  schema-enum ↔ Go-whitelist cross-check house style; cf.
  `base.canonicalStage`, classifier's `ContractType.IsValid`, keyparams'
  `PartyRoleType.IsValid`). Every other field is structurally free
  (`Description`/`ClauseRef` strings, `PartyName`/`LegalBasis`/`Summary`
  `*string`, `PromptInjectionDetected` a model-reported bool) — guarding
  one would over-reach past the ratified house style. **No severity
  re-mapping** (`model/party_consistency.go`: the agent sets severity per
  the prompt's `PARTY_AUTHORITY_MISSING→high` / rest→medium table,
  "Result Aggregator does not re-map"). A schema-valid `low` severity
  decodes WITHOUT error — enum-membership is Decode's concern,
  prompt-policy is not.
- **Stateless / concurrency-safe `Spec`.** `checkerSpec` is an empty
  struct with value-receiver methods; one `*Checker` is shared by the
  parallel errgroup pipeline without locking
  (`TestRun_ConcurrentRaceClean`, `-race`).
- **`gofmt` self-check** via `go/format` (sandbox blocks `go fmt`).

## Forward notes (recorded, owners elsewhere)

1. **base/router `Config.Model` fallback defect (owner: LIC-TASK-024 /
   router / LIC-TASK-047).** `base.Run` sets `req.Model=cfg.Model`
   unconditionally and the router forwards it unchanged on fallback hops;
   passing the resolved primary-provider model here is correct on the
   primary path; the proper base/router fix is out of scope. Shared with
   `typeclassifier` #1 / `keyparams`.
2. **Pipeline-ordering invariant (owner: LIC-TASK-034 Stage Executor) —
   DISTINCT from note 3 (code-architect CC-1).** The Stage Executor MUST
   populate `in.KeyParameters` from Agent 2's Stage-1 result before
   dispatching Agent 3 (Stage 2). A nil `in.KeyParameters` at Agent 3 is
   a Stage-Executor pipeline-wiring defect, correctly projected to
   `INTERNAL_ERROR` by base. (This is NOT the DM-artifact gate of note 3
   — different invariant, do not collapse.)
3. **Upstream DM artifact-bundle gate (owner: LIC-TASK-034 / the bundle
   gate) — DISTINCT from note 2 (CC-1).** A genuinely missing/empty
   mandatory DM artifact (DOCUMENT_STRUCTURE / EXTRACTED_TEXT) is
   semantically `DM_ARTIFACTS_MISSING` (retryable), NOT `INTERNAL_ERROR`.
   That gate MUST reject it BEFORE Agent 3 runs; reaching `Parts`' artifact
   checks then means a true LIC invariant breach, for which base's
   `Parts`-error→`INTERNAL_ERROR` is the correct code. Identical in spirit
   to `typeclassifier` #2 / `keyparams`.
4. **Token-budget head/tail (owner: LIC-TASK-021).** Agent 3 deliberately
   emits the FULL `EXTRACTED_TEXT`. §3 specifies no fixed/token-bounded
   compaction; the only truncation is 021's generic upstream per-artifact
   rule on `model.AgentInput` (base MF-3), NOT this `Spec.Parts`.
5. **`[]PartyValidation` (owner: a future cross-agent verification
   task).** `ValidationFacts` returns structured per-party results that
   v1 discards; a later task wanting Go-side ground truth (without
   re-parsing the XML) consumes them here.
6. **"Non-critical agent" graceful degradation (owner: LIC-TASK-034).**
   Acceptance says Agent 3 is non-critical (graceful degradation on
   timeout). The timeout itself is base's job (per-agent
   `context.WithTimeout` → `AGENT_TIMEOUT`, retryable); whether a failed
   Agent 3 degrades gracefully vs. fails the pipeline is the Stage
   Executor's tier policy (`error-handling.md` graceful degradation), not
   this Spec's.

## Forward requirement for LIC-TASK-047 (app-wiring)

Build `base.Deps` (the Router + Metrics/Tracer/Estimator/RepairMetrics
adapters; the `Builder` must be a `promptbuilder.NewBuilder(rec)` with the
`Recorder` adapter over `metrics.CrossCutMetrics.PartyValidationTotal` so
Agent 3's `<validation_facts>` minting actually records
`lic_party_validation_total`) and call `NewChecker(resolvedPrimaryModel,
config.AgentsConfig.Timeouts[config.AgentPartyConsistency], deps)`. The
resolved primary model = the configured primary provider's env-pinned
model (default claude ⇒ `LIC_CLAUDE_MODEL`). Register the returned
`*Checker` in the Stage Executor (LIC-TASK-034) by
`model.AgentPartyConsistency` (Stage 2, after Stage 1's Agent 1 ‖ Agent 2,
with `in.KeyParameters` populated from Agent 2 — forward note 2).
