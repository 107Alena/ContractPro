# summary Package — CLAUDE.md

**Agent 7 — Business Summary** (LIC-TASK-031, `ai-agents-pipeline.md`
§7, `high-architecture.md` §6.6/§6.7.2). From the classification, the
key parameters, the **RAW Agent-5** risk analysis, the
mandatory-conditions report and a head/tail-compacted slice of the
contract text it produces ONE plain-language `model.Summary{text}`
(200..3000 chars) for a non-legal business reader: what the contract is,
its key terms, what to watch out for (high/medium risks + missing
important conditions), and an overall verdict — **no legal jargon, no
article references**. **Stage 5, in PARALLEL with Agent 8 (detailed
report)**, after Stage-1 (Agents 1/2) and Stage-3 (Agents 4/5).

The 7th of the 9 per-agent packages. A **thin** wrapper over the shared
BaseAgent runner (LIC-TASK-024): `Summarizer` embeds `*base.BaseAgent`
(so `ID()`/`Run()` make it a `port.Agent` for free); the only per-agent
code is the `Spec` — `Parts` (envelope) + `Decode` (typed result). The
invariant-heavy loop lives in `base`, once.

Constructor: `NewSummarizer(modelID string, timeout time.Duration, deps
base.Deps) (*Summarizer, error)` — stutter-free `NewTypeName`
(`feedback_constructors.md`; never bare `New`, never the
package-stuttering `NewSummary`); fail-fast via `base.NewBaseAgent`.

## Files

- **summary.go** — package doc, `Summarizer`, `NewSummarizer`, the §7
  budget consts (`maxOutputTokens=1000`, **`temperature=0.3`** — the
  SECOND non-zero-temperature agent, the highest so far), the
  `var _ port.Agent` assertion, the base/router `Config.Model`-fallback
  forward note (shared with `typeclassifier` #1 / `keyparams` /
  `partyconsistency` / `mandatoryconditions` / `riskdetection` /
  `recommendation`).
- **spec.go** — `summarizerSpec` (stateless), the local §7 `compact`
  (head-4000/tail-1000 RUNE) + its consts (`headRunes`/`tailRunes`/
  `elision`), the minimal `classificationProjection{ContractType
  model.ContractType}`, `Parts`, `Decode`.
- **internal_test.go** — `TestHermeticImports` (the **7-entry,
  artifacts-PRESENT** allowlist — the deliberate Agent-7 re-add vs.
  Agent 6's drop) + `TestGofmtClean`.
- **summary_test.go** — constructor, fail-fast, the 5-block envelope
  order, the byte-exact minimal classification projection, whole
  risk_analysis (RAW R-NNN only) / mandatory_conditions_report /
  KeyParameters incl. `internal_extras` + nil-extras tolerated, the §7
  rune compaction (marker >5000 / verbatim+no-marker ≤5000 / multi-byte
  Cyrillic not split), layer-2 escaping, upstream-JSON injection
  neutralised, the 4 nil-upstream + absent/empty/malformed/empty-text
  EXTRACTED_TEXT strictness errors, **the load-bearing nil-raw/
  non-nil-merged RiskAnalysis subtest** (D1's pin), empty Risks[]/empty
  Conditions[] tolerated, `Decode` pure-unmarshal (no guard; short &
  3000-char bodies both decode), `Run` integration (acceptance), `-race`.

## API

- `NewSummarizer(modelID, timeout, base.Deps) (*Summarizer, error)`.
- `(*Summarizer)` satisfies `port.Agent` via the embedded
  `*base.BaseAgent` (`ID()==AGENT_SUMMARY`; `Run` returns
  `*model.Summary` or a `*model.DomainError`).

## Conventions & deliberate decisions (code-architect design review D1–D6)

- **Hermetic — 7-entry, artifacts-PRESENT (the Agent-4/5
  EXTRACTED_TEXT-consumer CLASS).** Imports ONLY stdlib +
  `internal/domain/{model,port}` + `base` + `promptbuilder` (for
  `Content` only — Agent 7 mints **no** structural block) + `prompts` +
  `schemas` + `internal/agents/artifacts`. Agent 7 **RE-ADDS**
  `artifacts`: the deliberate Agent-6 DROP (its 6-entry artifacts-free
  set) is **reversed** here because §7 "Зависимости" lists
  `EXTRACTED_TEXT` and the §7 envelope has a `<contract_document>`
  block. This is **NOT a regression toward Agents 4/5** — it is §7
  putting Agent 7 back in the EXTRACTED_TEXT-consumer class
  (`artifacts/CLAUDE.md` "Consumers" names "agents 4,5 — **and 7 with
  its own §7 compaction**" as a reuse consumer). It is **NOT**
  "byte-identical to `mandatoryconditions`" (that phrase was a
  `riskdetection`-specific claim) — it is the Agent-4/5 *class* with
  `artifacts` present. `TestHermeticImports` pins the 7-entry set AND
  explicitly fails if `artifacts` ever DISAPPEARS. **No
  `internal/config`** (resolved values are constructor params —
  LIC-TASK-047 wiring's job, the `router.RouterConfig` precedent). **No
  `DocumentProcessing` module** (the `artifacts` package owns the
  DP-faithful EXTRACTED_TEXT decoder; no other artifact is structurally
  decoded here).
- **§7 budget SSOT (D5).** `MaxTokens=1000`, **`Temperature=0.3`** from
  `ai-agents-pipeline.md` §7 "Бюджеты и параметры LLM". Agent 7 is the
  **SECOND non-zero-temperature agent** (after Agent 6's 0.2) and the
  highest so far. **Unlike §6, the §7 budget table gives NO inline
  rationale** — 0.3 is the SSOT table value, full stop; a reviewer must
  NOT "normalise" it to `0.0` to match Agents 1–5 (the Agent-6 MF-D5.1
  doc-lock precedent). `base.NewBaseAgent` validates `Temperature ∈
  [0,1]` so `0.3` passes. Provider=Claude (sonnet) primary
  (`LIC_AGENT_SUMMARY_PROVIDER`); timeout=**6s** supplied by wiring from
  `LIC_AGENT_SUMMARY_TIMEOUT` (default `6s`, `configuration.md`;
  `config.AgentSummary`) — never hard-coded;
  `Stage=model.StageAgentSummary` (the `base.canonicalStage` table
  requires the exact pair or `NewBaseAgent` fails fast).
- **Envelope order = the §7 prompt (D6).** `Parts` returns exactly
  `Content("classification_result", …)` → `Content("key_parameters",
  …)` → `Content("risk_analysis", …)` →
  `Content("mandatory_conditions_report", …)` →
  `Content("contract_document", …)` — `ai-agents-pipeline.md`
  :1236-1242. All five blocks are `promptbuilder.Content` (escaped —
  layer-2; **NON-NEGOTIABLE** — the risk_analysis /
  mandatory_conditions_report carry upstream-LLM-derived free text and
  the contract_document is the raw contract body, prime injection
  vectors). `b` is unused (`_ *promptbuilder.Builder` — only Agent 3
  mints).
- **`classification_result` = MINIMAL `{contract_type}` projection
  (D2).** A local typed `classificationProjection{ContractType
  model.ContractType}` from `in.Classification.ContractType` → exactly
  `{"contract_type":"SUPPLY"}`. The §7 prompt block is an **ellipsis**
  (`<classification_result>…`) — NOT the explicit
  `{"contract_type":"…"}` literal Agent 5 had — so the
  whole-vs-projection call rests on the ratified **Agent-4/5
  token-budget + lowest-injection-vector precedent applied to a
  bare-ellipsis block** (the §7 task needs only the type label for
  "это договор поставки…", criterion 1), NOT a "prompt envelope
  literal" claim. Typed enum (not `string`) ⇒ a rename is a compile
  error; byte-exact single-key shape pinned.
- **`risk_analysis` ← `in.RiskAnalysis` (RAW Agent-5), NEVER
  `in.MergedRiskAnalysis` (D1 — load-bearing, the DELIBERATE divergence
  from the immediately-preceding Agent 6).** The decision driver is the
  **SSOT text**: §7 "Зависимости" (`ai-agents-pipeline.md:1175`) lists a
  **bare `RiskAnalysis` with NO "со всеми findings, включая встроенные
  из агентов 3, 4" qualifier** — whereas §6 EXPLICITLY carried that
  qualifier, which is why Agent 6 sources merged. Corroborating only
  (NOT the rule — the Agent-5 "the consumer doesn't use field X is not a
  sourcing driver" house style): the §7 prompt cites no risk ids, and §7
  separately feeds `mandatory_conditions_report` as its own block, so
  the post-merge R-MNNN/R-PNNN namespace is unused and sourcing merged
  would duplicate missing-condition content. The nil check is on
  `in.RiskAnalysis`; a non-nil `in.MergedRiskAnalysis` does **NOT**
  satisfy it (the Stage Executor must wire the RAW field — forward
  note 2). Pinned by the **`nil raw but non-nil merged RiskAnalysis`**
  subtest (the exact inverse of `recommendation`'s
  `nil-merged/non-nil-raw` pin).
- **Each upstream block emitted WHOLE, not projected (D6).**
  `json.Marshal` of `in.KeyParameters` (incl. `internal_extras`; nil
  `InternalExtras` dropped via `omitempty`), `in.RiskAnalysis` (RAW,
  incl. `summary` + `prompt_injection_detected`), `in.MandatoryConditions`
  (incl. `conditions`/`contract_type`). The deliberate asymmetry with
  the *projected* `classification_result`: §7 "Зависимости" lists each
  whole, no `.field` selector (the Agent-4 whole-KeyParameters
  precedent). Empty `Risks[]` (clean contract) / empty `Conditions[]`
  (all present) are TOLERATED, marshalled verbatim (the Agent-4/6
  empty-slice tolerance).
- **`contract_document` ← EXTRACTED_TEXT via the shared
  `artifacts.ExtractedText`+`FullText()`, then §7 head-4000/tail-1000
  RUNE compaction (D3).** Reuse the ratified shared DP-faithful decoder
  (`artifacts/CLAUDE.md` "Consumers"), then apply Agent 7's OWN §7
  fixed-size compaction (`compact`) — **UNLIKE Agents 2/5 which emit the
  FULL text**, §7 "Зависимости" specifies `compact — head 4 000 + tail
  1 000 символов`. `"символов"` ⇒ sliced by `[]rune`, never bytes (no
  multi-byte Cyrillic split — the `capRunes`/`passthroughEstimator`
  house style). Head+tail joined by a fixed `"\n[…]\n"` marker because
  the §7 prompt block is `<contract_document>… фрагменты текста …`
  (`ai-agents-pipeline.md:1241`) — two fragments must be visibly
  separated; the marker is non-injectable (no `&<>`, unchanged by
  `escapeText`). Text ≤ `headRunes+tailRunes` (5000) ⇒ verbatim, **no**
  marker. This is Agent 7's own `Spec.Parts` job, categorically distinct
  from and composable with LIC-TASK-021's generic upstream per-model
  token truncation (`base/seams.go` MF-3). Mandatory: EXTRACTED_TEXT
  absent / empty bytes / malformed JSON / decodes-to-empty-or-whitespace
  ⇒ `Parts` error.
- **Strictness phase MIRRORS the envelope (D6 — the
  `recommendation`/`mandatoryconditions` CC-4 precedent).** This is
  DELIBERATELY **NOT** the Agent-5 grouped-by-class structure: Agent 5
  grouped by class because it had a THIRD, OPTIONAL class (its OPTIONAL
  PROCESSING_WARNINGS); Agent 7 has only the TWO MANDATORY classes
  (pipeline-ordering, then the single DM-artifact gate) and NO optional
  class — so envelope-mirroring keeps the two forward-note classes
  contiguous (the `recommendation` CC-4 precedent, **NOT** the
  `riskdetection` CC-1 divergence). A reviewer must NOT regroup it.
  `in.Classification==nil || in.KeyParameters==nil ||
  in.RiskAnalysis==nil || in.MandatoryConditions==nil` is a
  **pipeline-ordering** breach (forward note 2) ⇒ `INTERNAL_ERROR`;
  **no tolerated-empty** (the §7 prompt has no "(если есть)" hedge,
  fixed 5-block envelope).
- **`Decode` is a PURE typed-unmarshal — NO drift-guard (D4).** base
  schema-validates against the embedded `summary.json` BEFORE `Decode`
  (MF-1). The §7 schema **fully constrains the only field** —
  `text: {"type":"string","minLength":200,"maxLength":3000}` — so there
  is genuinely **nothing left for Decode to enforce**. This is the
  **enumerated-unguarded "there is nothing to guard" terminal case**,
  the `riskdetection`-`Risk.ID`-NOT-guarded /
  `mandatoryconditions`-unguarded-fields house style — **NOT** the
  Agent-6 "schema is silent ⇒ Decode is the sole/terminal guard" class
  (unlike §6's `risk_id`, the §7 schema is NOT silent on its only
  field, so no sole/terminal Decode guard exists to add). Explicit
  single-field enumeration (the house style's required written record
  even for a singleton): `model.Summary.Text` is a structurally FREE
  string whose 200..3000 bound is the **SCHEMA's** SSOT, validated by
  base pre-Decode (`report.go:5` states it is "enforced by the schema
  validator, not at the type level"); re-checking length here would
  DUPLICATE the schema and over-reach PAST the ratified
  closed-enum/frozen-SSOT boundary — a future reviewer must NOT add a
  "safety" length re-check. `Decode` is **never a transform / re-map /
  synthesis** (the ratified Agent-3/4/5/6 principle): no trimming, no
  jargon-stripping, no prose post-processing — any such transformation
  belongs to the Reporting Engine downstream, NEVER here.
- **Stateless / concurrency-safe `Spec`.** `summarizerSpec` is an empty
  struct with value-receiver methods; one `*Summarizer` is shared by
  the parallel errgroup pipeline (Stage 5: Agent 7 ‖ Agent 8) without
  locking (`TestRun_ConcurrentRaceClean`, `-race`).
- **`gofmt` self-check** via `go/format` (sandbox blocks `go fmt`).

## Forward notes (recorded, owners elsewhere)

1. **base/router `Config.Model` fallback defect (owner: LIC-TASK-024 /
   router / LIC-TASK-047).** `base.Run` sets `req.Model=cfg.Model`
   unconditionally and the router forwards it unchanged on fallback
   hops; passing the resolved primary-provider model here is correct on
   the primary path; the proper base/router fix is out of scope. Shared
   with `typeclassifier` #1 / `keyparams` / `partyconsistency` /
   `mandatoryconditions` / `riskdetection` / `recommendation`.
2. **Pipeline-ordering invariant (owner: LIC-TASK-034 Stage Executor) —
   DISTINCT from note 3.** The Stage Executor MUST populate
   `in.Classification` (Agent 1 Stage 1), `in.KeyParameters` (Agent 2
   Stage 1), **`in.RiskAnalysis`** (the **RAW Agent-5** Stage-3 result,
   **NOT** `in.MergedRiskAnalysis`) AND `in.MandatoryConditions` (Agent
   4 Stage 3) before dispatching Agent 7 (**Stage 5, parallel with
   Agent 8**). A nil any-of-four — or wiring only the merged field — is
   a Stage-Executor pipeline-wiring defect, correctly projected to
   `INTERNAL_ERROR`.
3. **Upstream DM artifact-bundle gate (owner: LIC-TASK-034 / the bundle
   gate) — DISTINCT from note 2.** A genuinely missing/empty
   `EXTRACTED_TEXT` is semantically `DM_ARTIFACTS_MISSING` (retryable),
   NOT `INTERNAL_ERROR`. That gate MUST reject it BEFORE Agent 7 runs;
   reaching `Parts`' text check then means a true LIC invariant breach,
   for which base's `Parts`-error→`INTERNAL_ERROR` is the correct code.
   Identical in spirit to `typeclassifier` #2 / `keyparams` /
   `partyconsistency` #3 / `mandatoryconditions` #3 / `riskdetection`
   #3.
4. **`compact` shared-code duplication (owner: a dedicated cleanup task
   or the cheapest later agent task).** §1 (`typeclassifier`) and §7
   (this package) now hold **two byte-identical copies** of
   `compact`/`headRunes`/`tailRunes`/`elision` (§1 and §7 specify the
   SAME head-4000/tail-1000 rule). `typeclassifier`'s copy is itself a
   recorded duplicate pending retirement (`typeclassifier/CLAUDE.md`
   forward-note #3; `artifacts/CLAUDE.md` "Later cleanup"). Importing it
   would couple Agent 7 to a to-be-deleted symbol and breach this
   package's hermetic allowlist. Consolidating the §1/§7 compaction into
   a shared hermetic home (candidate: `internal/agents/artifacts` —
   where the EXTRACTED_TEXT decoder consolidation landed) is a
   **DEFERRED** cleanup, NOT done mid-flight (re-touching reviewed Agent
   1 = unrequested scope creep risking its pinned compaction tests — the
   exact `artifacts/CLAUDE.md` ratified stance). Recorded so a reviewer
   reads the duplication as a deliberate deferral, not an oversight.

## Forward requirement for LIC-TASK-047 (app-wiring)

Build `base.Deps` (the Router + Metrics/Tracer/Estimator/RepairMetrics
adapters) and call `NewSummarizer(resolvedPrimaryModel,
config.AgentsConfig.Timeouts[config.AgentSummary], deps)`. The resolved
primary model = the configured primary provider's env-pinned model
(default claude ⇒ `LIC_CLAUDE_MODEL`). Register the returned
`*Summarizer` in the Stage Executor (LIC-TASK-034) by
`model.AgentSummary` (**Stage 5, in PARALLEL with Agent 8 — detailed
report**), after Stage-1 (Agents 1/2) and Stage-3 (Agents 4/5), with
`in.Classification`, `in.KeyParameters`, `in.RiskAnalysis` (**RAW
Agent-5, NOT merged**) and `in.MandatoryConditions` populated — forward
note 2.
