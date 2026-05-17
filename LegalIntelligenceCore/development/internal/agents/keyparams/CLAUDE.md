# keyparams Package — CLAUDE.md

**Agent 2 — Key Parameters Extractor** (LIC-TASK-026,
`ai-agents-pipeline.md` §2, `high-architecture.md` §6.6). Extracts the
contract's key parameters (parties, subject, price, duration, penalties,
jurisdiction) + the LIC-internal extras consumed by downstream agents
3/4/8 (applicable law, termination, acceptance procedure, party roles
with **raw** INN/OGRN, key dates) from the FULL semantic tree and the
extracted contract text.

The 2nd of the 9 per-agent packages. A **thin** wrapper over the shared
BaseAgent runner (LIC-TASK-024): `Extractor` embeds `*base.BaseAgent` (so
`ID()`/`Run()` make it a `port.Agent` for free); the only per-agent code
is the `Spec` — `Parts` (envelope) + `Decode` (typed result). The
invariant-heavy loop lives in `base`, once.

Constructor: `NewExtractor(modelID string, timeout time.Duration, deps
base.Deps) (*Extractor, error)` — stutter-free `NewTypeName`
(`feedback_constructors.md`); fail-fast via `base.NewBaseAgent`.

## Files

- **keyparams.go** — package doc, `Extractor`, `NewExtractor`, the §2
  budget consts (`maxOutputTokens=2000`, `temperature=0.0`), the
  `var _ port.Agent` assertion, the base/router `Config.Model`-fallback
  forward note (shared with `typeclassifier` #1).
- **spec.go** — `extractorSpec` (stateless), `Parts`, `Decode`.
- **internal_test.go** — `TestHermeticImports` (exact allowlist incl.
  `artifacts`) + `TestGofmtClean`.
- **keyparams_test.go** — constructor, fail-fast, envelope order +
  layer-2 escaping, byte-faithful tree passthrough, full-text
  no-compaction, artifact strictness (errors + tolerated empty tree),
  `Decode` (full round-trip, INN/OGRN as-is, nil internal_extras, role
  drift), `Run` integration (acceptance Шаг 1/2), repair-triggered,
  `-race`.

## API

- `NewExtractor(modelID, timeout, base.Deps) (*Extractor, error)`.
- `(*Extractor)` satisfies `port.Agent` via the embedded `*base.BaseAgent`
  (`ID()==AGENT_KEY_PARAMS`; `Run` returns `*model.KeyParameters` or a
  `*model.DomainError`).

## Conventions & deliberate decisions (code-architect design review)

- **Hermetic.** Imports ONLY stdlib + `internal/domain/{model,port}` +
  `base` + `promptbuilder` + `prompts` + `schemas` + the shared
  `internal/agents/artifacts` (the LIC-TASK-026 steward decision). **No
  `internal/config`** (resolved per-agent values are constructor params;
  the config→value map is LIC-TASK-047 wiring's job — the
  `router.RouterConfig` precedent). **No `DocumentProcessing` module**
  (the `artifacts` package owns the local minimal DP-faithful structs).
  `TestHermeticImports` pins the allowlist.
- **§2 budget SSOT.** `MaxTokens=2000`, `Temperature=0.0` from
  `ai-agents-pipeline.md` §2 "Бюджеты и параметры LLM". Provider=Claude
  (sonnet) primary; timeout=8s supplied by wiring from
  `LIC_AGENT_KEY_PARAMS_TIMEOUT` (default `8s`, `configuration.md`).
- **Envelope order is the INVERSE of Agent 1 (Q-cross-cutting #1).**
  `Parts` returns exactly `Content("semantic_tree", string(rawTree))`
  then `Content("contract_document", fullText)` — `<semantic_tree>` FIRST
  per `prompts/key_params.txt:35-40`. Builder.Build adds the `<input>`
  wrapper (NOT emitted here — the ratified "prompt is SSOT, illustration
  not binding" precedent). Wrong order ⇒ the model reads the tree as the
  contract body.
- **SEMANTIC_TREE is byte-faithful PASSTHROUGH, not decoded (Q2).** The
  raw `json.RawMessage` is `string()`-ed straight into
  `promptbuilder.Content` — no decode/re-encode. §2 requires the tree
  "полностью"; the agent must cite node `id`s as `clause_ref`, so
  pruning/re-keying would strip the very ids it references. Strictness
  gate = well-formedness (`json.Valid`), never a structural decode. Two
  defence layers hold: DP's `encoding/json` already `<`-escapes
  `<`/`>`/`&` at the JSON layer (pinned by
  `TestSpec_Parts_TreePassthroughByteFaithful`), and `promptbuilder.Content`
  layer-2-escapes any literal angle bracket that DOES survive in the raw
  bytes (pinned by `TestSpec_Parts_EnvelopeOrderAndEscaping`, which uses a
  hand-written raw literal — NOT `json.Marshal` — to exercise that path).
- **EXTRACTED_TEXT is emitted in FULL — NO Agent-2-side compaction (Q3,
  base MF-3).** §2's ">80K tokens ⇒ head/tail" is the GENERIC
  per-artifact truncation, explicitly **LIC-TASK-021's job upstream of
  `Spec.Parts`** on `model.AgentInput` (`base/seams.go` MF-3 /
  `base/CLAUDE.md`) — categorically different from Agent 1's fixed §1
  "head 4000 + tail 1000" Agent-1-specific prompt requirement. Carrying
  an interim truncation here would be a second divergent site 021 must
  later find/remove; until 021 lands the v1 passthrough estimator
  delegates the over-budget verdict to the provider's `CONTEXT_TOO_LONG`
  → `AGENT_INPUT_TOO_LARGE` path. **FORWARD NOTE for LIC-TASK-021:** Agent
  2 deliberately emits the full `EXTRACTED_TEXT`; the head/tail rule
  belongs in the upstream artifact-bundle preparation, not this `Spec`.
- **Artifact strictness mirrors Agent 1's Q4 (Q4).** EXTRACTED_TEXT
  mandatory: absent / malformed JSON / decodes-to-empty-or-whitespace ⇒
  `Parts` error ⇒ base `INTERNAL_ERROR`. SEMANTIC_TREE mandatory in
  shape: absent / empty bytes / `!json.Valid` ⇒ `Parts` error. An
  **empty-but-well-formed** tree (`{}`, `{"document_id":"d","root":null}`,
  a root with no children) is **TOLERATED** — emitted verbatim (the gate
  is well-formedness, not semantic richness; a flat-but-valid tree is a
  legitimate contract shape; the model still extracts from
  `<contract_document>`, the tree only supplies `clause_ref` ids). This
  is the exact analogue of Agent 1 tolerating a structurally-empty
  DOCUMENT_STRUCTURE.
- **`Decode` drift-guard: `role` is the SOLE enum surface (Q5).** After
  `json.Unmarshal` into `model.KeyParameters`, every
  `InternalExtras.PartyRoles[i].Role` is re-checked via
  `PartyRoleType.IsValid()`; a miss ⇒ `INTERNAL_ERROR` build-defect (the
  enumerated schema-enum ↔ Go-whitelist cross-check house style; cf.
  `base.canonicalStage`, classifier's `ContractType.IsValid`). Every
  other field is structurally free (parties/subject/price/… strings or
  null, `prompt_injection_detected` a plain model-reported bool) — no
  closed value-set to drift; guarding a free string would over-reach.
  `internal_extras` is schema-optional ⇒ nil-guard before ranging
  `PartyRoles` (absent extras is a valid minimal response, NOT an error —
  pinned by the `minimal` case).
- **INN/OGRN decoded AS-IS, never checksum-validated.** §2 criterion 6 /
  acceptance Шаг 2: extraction only; checksum validation is Agent 3's
  pre-LLM job (`promptbuilder.ValidateINN/OGRN`). `TestSpec_Decode` /
  `TestRun_Integration_ValidEnvelope` pin a deliberately
  checksum-invalid `0000000000`/`0000000000000` surviving verbatim.
- **`Config.Model` is the resolved PRIMARY-provider model
  (forward note, shared with `typeclassifier` #1).** `base.Run` sets
  `req.Model=cfg.Model` unconditionally and the router forwards it
  unchanged on fallback hops; passing the resolved primary model here is
  correct on the primary path; the proper base/router fix is out of scope
  (owner: LIC-TASK-024 / router / LIC-TASK-047).
- **Stateless / concurrency-safe `Spec`.** `extractorSpec` is an empty
  struct with value-receiver methods; one `*Extractor` is shared by the
  parallel errgroup pipeline without locking
  (`TestRun_ConcurrentRaceClean`, `-race`).
- **`gofmt` self-check** via `go/format` (sandbox blocks `go fmt`).

## Forward requirement for LIC-TASK-047 (app-wiring)

Build `base.Deps` (the Router + Metrics/Tracer/Estimator/RepairMetrics
adapters) and call `NewExtractor(resolvedPrimaryModel,
config.AgentsConfig.Timeouts[config.AgentKeyParams], deps)`. The resolved
primary model = the configured primary provider's env-pinned model
(default claude ⇒ `LIC_CLAUDE_MODEL`). Register the returned `*Extractor`
in the Stage Executor (LIC-TASK-034) by `model.AgentKeyParams` (Stage 1,
parallel with Agent 1). The real `TokenEstimator` (LIC-TASK-021) — wired
via `Deps.Estimator` — is what enforces §2's >80K head/tail truncation,
upstream of this `Spec.Parts`.
