# typeclassifier Package — CLAUDE.md

**Agent 1 — Contract Type Classifier** (LIC-TASK-025,
`ai-agents-pipeline.md` §1, `high-architecture.md` §6.6). Determines the
Russian-civil-law contract type (12-value whitelist) + confidence from a
compacted contract-text slice and the document's section titles.

The first of the 9 per-agent packages. It is a **thin** wrapper over the
shared BaseAgent runner (LIC-TASK-024): `Classifier` embeds
`*base.BaseAgent` (so `ID()`/`Run()` make it a `port.Agent` for free); the
only per-agent code is the `Spec` — `Parts` (envelope) + `Decode` (typed
result). The invariant-heavy loop (Prompt Builder → Token Estimator →
primary LLM call → Schema Validator → sticky 1-shot Repair → typed decode,
+ span tree + 4 metrics) lives in `base`, once.

Constructor: `NewClassifier(modelID string, timeout time.Duration, deps
base.Deps) (*Classifier, error)` — stutter-free `NewTypeName`
(`feedback_constructors.md`); fail-fast via `base.NewBaseAgent`.

## Files

- **classifier.go** — package doc, `Classifier`, `NewClassifier`, the §1
  budget consts (`maxOutputTokens=400`, `temperature=0.0`), the
  `var _ port.Agent` assertion.
- **spec.go** — `classifierSpec` (stateless), the local minimal artifact
  decode types (`extractedText`/`documentStructure`), `compact`
  (head/tail), `Parts`, `Decode`.
- **internal_test.go** — `TestHermeticImports` (exact allowlist) +
  `TestGofmtClean` self-check (mirrors `base`).
- **classifier_test.go** — constructor, Spec.Parts envelope &
  compaction, Spec.Decode drift guard, Run() integration (acceptance
  Шаг 2/3), `-race`.

## API

- `NewClassifier(modelID, timeout, base.Deps) (*Classifier, error)`.
- `(*Classifier)` satisfies `port.Agent` via the embedded `*base.BaseAgent`
  (`ID()==AGENT_TYPE_CLASSIFIER`; `Run` returns
  `*model.ClassificationResult` or a `*model.DomainError`).

## Conventions & deliberate decisions

- **Hermetic (the universal `internal/agents|llm/*` invariant).** Imports
  ONLY stdlib + `internal/domain/{model,port}` + `base` + `promptbuilder`
  + `prompts` + `schemas`. **No `internal/config`** (code-architect Q5):
  resolved per-agent values are constructor params; the config→value map
  is LIC-TASK-047 wiring's job — the hermetic `router.RouterConfig`
  precedent. **No `contractpro/document-processing`**: the DM
  EXTRACTED_TEXT / DOCUMENT_STRUCTURE artifacts are decoded via local
  minimal structs (only the fields Agent 1 reads), honouring the
  byte-faithful `InputArtifactsCompact = map[ArtifactType]json.RawMessage`
  defer-decode design. `TestHermeticImports` pins the allowlist.
- **`deps base.Deps`, not a parallel Option API (YAGNI).** `base.Deps` is
  the established injection seam shared by `base`/`router`; it already
  carries the required `Router` + the optional Metrics/Tracer/Estimator/
  RepairMetrics seams (047 adapters / tests) with nil→zero-default. A
  second functional-Option surface would duplicate it.
- **§1 budget SSOT.** `MaxTokens=400`, `Temperature=0.0` from
  `ai-agents-pipeline.md` §1 "Бюджеты и параметры LLM". §0.6's aggregate
  table lists 200 output tokens for Agent 1 — it disagrees with the §1
  per-agent table; **§1 wins** (docs-side reconciliation note, not code).
- **Two-Part envelope, prompt order.** promptbuilder emits one-level
  `<tag>escaped</tag>` blocks; the §1 prompt's inner `<sections>` is
  *illustrative* (the ratified promptbuilder "prompt is SSOT, the
  illustration form is not binding" precedent). `Parts` returns exactly
  `Content("document_structure", titles)` then
  `Content("contract_document", compact(text))` — order = the §1
  system-prompt envelope (`base/CLAUDE.md` forward-req #1). Both are
  user-controlled bytes ⇒ `promptbuilder.Content` (escaped =
  prompt-injection defence layer 2).
- **head-4000 + tail-1000 compaction is Agent 1's OWN `Spec.Parts` job,
  RUNE-based (code-architect Q2).** A fixed per-agent **character**
  compact from `ai-agents-pipeline.md` §1 "Зависимости" — categorically
  distinct from, and composable with, LIC-TASK-021's general per-model
  **token** head-60/tail-40 truncation (which is upstream of `Spec.Parts`,
  `base/seams.go` MF-3). "символов" ⇒ sliced by `[]rune`, never bytes, so
  a multi-byte Cyrillic codepoint is never split (the `capRunes` /
  `passthroughEstimator` rune-safe house style). Head and tail are joined
  by a fixed `"\n[…]\n"` marker because the §1 prompt explicitly says the
  block holds "фрагменты … (начало + конец)" — two fragments must be
  visibly separated; the marker is non-injectable (no `&<>`, unchanged by
  `escapeText`). Text ≤ `headRunes+tailRunes` (5000) ⇒ emitted verbatim,
  **no** marker (head/tail would meet; a marker in contiguous real text
  would lie).
- **Artifact strictness (code-architect Q4).** EXTRACTED_TEXT is
  mandatory: absent / malformed JSON / empty-or-whitespace text ⇒ `Parts`
  error ⇒ base `INTERNAL_ERROR` (`outcome=invalid_output`).
  DOCUMENT_STRUCTURE is mandatory in shape (absent / malformed ⇒ `Parts`
  error) but may be structurally empty — zero usable titles ⇒ an empty,
  valid `<document_structure></document_structure>` block, **not** an
  error (titles are a weak supplementary signal; classification leans on
  the contract text).
- **`Decode` re-checks `ContractType.IsValid()` — defense-in-depth
  (code-architect Q5).** base schema-validates against the embedded
  `type_classifier.json` enum *before* `Decode` (MF-1), so this is a
  redundancy by design: the enumerated cross-check house style (cf.
  `base.canonicalStage`, `prompts.basenames`, `model.IsValidContractType`)
  that converts a silent `type_classifier.json` enum ↔ `model.ContractType`
  whitelist **drift** into a loud `INTERNAL_ERROR` build-defect signal —
  exactly `Spec.Decode`'s contract ("schema and Go struct disagree ⇒ LIC
  build defect"). Checked for the primary and every alternative.
- **Stateless / concurrency-safe `Spec`.** `classifierSpec` is an empty
  struct with value-receiver methods; one `*Classifier` (hence one Spec)
  is shared by the parallel errgroup pipeline without locking
  (`TestRun_ConcurrentRaceClean`, `-race`; `base/CLAUDE.md` immutability).
- **`Config.Model` is the resolved PRIMARY-provider model
  (code-architect Q1 MUST-FIX) — see the forward note.**

## Forward notes (recorded, owners elsewhere)

1. **base/router `Config.Model` fallback defect (owner: LIC-TASK-024 /
   router / LIC-TASK-047).** `base.Run` sets `req.Model = cfg.Model`
   unconditionally; the router forwards the request UNCHANGED to every
   provider in the chain; each adapter's `chooseModel` lets a non-empty
   `req.Model` OVERRIDE that provider's env-pinned default. So on a
   fallback hop a non-primary provider receives the primary's model id
   (invalid for that vendor → that hop fails), contradicting ADR-LIC-03 /
   `llm-provider-abstraction.md` §1.3. `NewClassifier` therefore takes the
   **resolved primary-provider model** (default claude ⇒ `LIC_CLAUDE_MODEL`,
   `claude-sonnet-4-6`) — correct on the primary path, no base change. The
   proper fix (base leaving `req.Model==""` so each adapter falls through
   to its own env-pinned default, or the router rewriting per provider) is
   out of scope here.
2. **Upstream DM artifact gate (owner: LIC-TASK-034 Stage-Executor input
   prep / the artifact-bundle gate).** A genuinely missing/empty mandatory
   DM artifact is semantically `DM_ARTIFACTS_MISSING` (retryable), NOT
   `INTERNAL_ERROR`. That gate MUST reject it BEFORE Agent 1 runs;
   reaching `Parts`' artifact checks then means a true LIC invariant
   breach, for which base's `Parts`-error→`INTERNAL_ERROR` is the correct
   code.

3. **Shared artifact-decoder stewardship (owner: LIC-TASK-026, the next
   per-agent author).** `extractedText`/`documentStructure` +
   `fullText()`/`titles()` are local minimal decoders, byte-faithful to
   DP's `model.ExtractedText.FullText()` / `DocumentStructure`. Later
   agents (026..033) also consume EXTRACTED_TEXT / DOCUMENT_STRUCTURE /
   SEMANTIC_TREE; re-deriving these per package risks silent divergence
   from DP semantics. Consider extracting a hermetic
   `internal/agents/artifacts` helper (stdlib + `internal/domain` only)
   with ONE `FullText` mirror pinned by a test against DP's documented
   semantics, so the "byte-identical to DP" claim is asserted once, not
   by-comment 9×. Not done here (the hermeticity rule correctly forbids
   importing the DP module; per-agent local structs are the sanctioned
   v1 pattern) — recorded so agent 2 makes the call deliberately
   (code-reviewer LOW-2).
   **RESOLVED by LIC-TASK-026 (doc-only annotation, no code change here):**
   the hermetic `internal/agents/artifacts` package was created (stdlib-only;
   `ExtractedText`+`FullText` pinned by `TestFullText_DPSemantics`). Agent 2
   consumes it. Agent 1 was deliberately NOT refactored (re-touching a
   reviewed DONE task = unrequested scope creep risking its pinned tests);
   `typeclassifier`'s local `extractedText`/`fullText` is now a recorded
   duplicate to be retired by a later cleanup task — see
   `internal/agents/artifacts/CLAUDE.md` "Consumers & forward notes".

## Forward requirement for LIC-TASK-047 (app-wiring)

Build `base.Deps` (the Router + the Metrics/Tracer/Estimator/RepairMetrics
adapters) and call `NewClassifier(resolvedPrimaryModel,
config.AgentsConfig.Timeouts[config.AgentTypeClassifier], deps)`. The
resolved primary model = the configured primary provider's env-pinned
model (default claude ⇒ `LIC_CLAUDE_MODEL`). Register the returned
`*Classifier` in the Stage Executor (LIC-TASK-034) by
`model.AgentTypeClassifier`.
