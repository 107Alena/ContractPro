# promptbuilder Package — CLAUDE.md

Prompt Builder for the Legal Intelligence Core (LIC-TASK-022,
`high-architecture.md` §6.7/§6.7.1/§6.7.2, `ai-agents-pipeline.md`
§0.2/§0.3). It is **prompt-injection defence layer 2** (ADR-LIC-07).
Every per-agent `Run` (LIC-TASK-025..033) calls it to produce the
`System` + `User` of its LLM `port.CompletionRequest`.

Constructor: `NewBuilder(rec Recorder)` (stutter-free `NewTypeName` per
the codebase convention / `feedback_constructors.md`).

## Files

- **escape.go** — `escapeText` (XML text-node: `&`,`<`,`>`), `escapeAttr`
  (XML attribute: `&`,`<`,`>`,`"` + control/newline strip). Two
  **deliberately distinct** `strings.Replacer`s.
- **innogrn.go** — pure FNS (ФНС) checksums: `ValidateINN` (10/12-digit),
  `ValidateOGRN` (13/15-digit → `LEGAL_ENTITY`/`INDIVIDUAL_ENTREPRENEUR`/
  `null`). No side effects.
- **builder.go** — `PartyKind` (mirror of `metrics.PartyValidationType`),
  `Recorder`+`noopRecorder` seam, `Part` (closed type) + `Content`,
  `Builder`, `NewBuilder`, `Build`, `Party`/`PartyValidation`,
  `ValidationFacts`.

## API

- `Content(tag, content string) Part` — an **escaped** content block;
  `tag` must match `^[a-z_]{1,32}$`. The only Part constructor available
  to other packages.
- `(*Builder) Build(agentID, system string, parts []Part)
  (port.CompletionRequest, error)` — `System` = baked prompt
  (passed in), `User` = `<input>{parts}</input>`. Sets **only**
  `AgentID/System/User`. Fail-fast deterministic error on empty system /
  no parts / invalid|duplicate tag / empty minted block.
- `(*Builder) ValidationFacts([]Party) (Part, []PartyValidation)` —
  agent-3 pre-LLM ИНН/ОГРН check; returns a Builder-minted
  `<validation_facts>` Part + structured results; fires
  `lic_party_validation_total` once per **present** identifier.
- `ValidateINN`/`ValidateOGRN` — exported pure funcs (reuse / direct
  table-tests).

## Conventions & deliberate decisions

- **Hermetic.** stdlib + `internal/domain/{model,port}` only — exactly
  like `internal/llm/cost`. Prometheus is inverted behind the `Recorder`
  seam (`noopRecorder` zero-default; `NewBuilder(nil)` ⇒ no-op, no
  per-call nil check). The adapter over
  `metrics.CrossCutMetrics.PartyValidationTotal` (via
  `metrics.PartyValidationType` + `metrics.BoolLabel`) is **app-wiring's
  job, LIC-TASK-047** — importing `metrics` here would break hermeticity
  (mirrors `cost`/`ratelimit`).
- **Builder is envelope-structure-agnostic; the caller owns block order.**
  The 9 agents have 6+ distinct envelope shapes that track prompt edits;
  a per-agentID template in this leaf package would couple it to all of
  them and force a Builder change on every prompt tweak. The caller
  (per-agent `Run`) supplies the ordered `[]Part`; Build only
  escapes+wraps+assembles (code-architect Q1).
- **`Part` is a closed type, NOT a `Raw bool`.** Layer-2 defence holds
  only if *every* user byte is escaped. A caller-fabricable
  "insert verbatim" string is an injection bypass by construction. `Part`
  has unexported fields; other packages can obtain one only from
  `Content` (always escaped) or a Builder method that mints structural
  XML (`ValidationFacts`). "No caller can inject un-escaped structural
  XML" is a **type** guarantee, not a discipline (code-architect MF-2).
- **Two distinct escapers, never merged.** Text-node vs attribute need
  different encodings. Folding `"`→`&quot;` into the text escaper would
  mangle every quote in the contract body and break the acceptance-test
  exact string. `escapeAttr` additionally strips C0/C1/DEL controls and
  newlines *first* so an attribute can never carry a structural line
  break (code-architect MF-3). `TestEscapers_AreDistinct` pins this.
- **`&` escaped first, single-pass.** One `strings.Replacer` (not three
  sequential `.Replace`s): it does a non-overlapping left-to-right pass
  and never re-scans replacement output, so the `&` emitted as part of
  `&lt;` is not re-escaped. `>` is escaped (not strictly XML-required)
  because an LLM runs no XML parser and the acceptance test pins
  `&gt;`. `TestEscapeText/no_double-escape_of_emitted_entity` guards it.
- **validation_facts uses the `<inn_check>/<ogrn_check>` form, NOT the
  §6.7.2 flat `<party>`.** `high-architecture.md` §6.7.2 is an
  *illustration*; the SSOT is what the model actually reads — the
  embedded agent-3 system prompt
  (`internal/agents/prompts/party_consistency.txt:37-41`,
  `ai-agents-pipeline.md` §3). The Builder MUST emit what the prompt
  expects or agent 3's "use validation_facts as ground truth, do not
  re-validate" instruction is meaningless (code-architect-confirmed).
- **`name`/`inn`/`ogrn` are attribute-escaped; `valid`/`entity_type` are
  not.** All three identifiers originate from the document / agent-2
  output (user-controlled) — never echoed raw into an attribute
  (code-architect MF-4). `valid`/`entity_type` come from closed const
  sets (`boolAttr`, `Entity*`).
- **Metric fires only for a *present* identifier.** An absent INN/OGRN is
  not "invalid" — `lic_party_validation_total` is a data-quality signal
  (§6.7.2); counting absent as `valid=false` would poison it.
  `PartyValidation.{INN,OGRN}Present` make absent-vs-invalid explicit to
  agent-3's future Run (code-architect MF-5d / NTH-2).
- **Build sets only System/User/AgentID.** `Model/MaxTokens/Temperature/
  JSONSchema` are the agent/router layer's (`high-architecture.md` §6.7);
  the baked `system` prompt is passed *in* so this package needs no
  `prompts` import and stays hermetic + envelope-agnostic.
- **No `<metadata>` minter (YAGNI).** §6.7's generic example shows
  `<metadata>` but **none** of the 9 real per-agent envelopes use it —
  every real block is user-controlled content. Add a minter only when an
  agent actually needs a non-validation structural block.
- **Immutable after construction.** The only field is the fixed
  `Recorder`; safe for the parallel errgroup pipeline without locking
  (`TestBuilder_Concurrent`, `-race`).
- **All-zero INN passes the checksum.** Documented edge
  (`TestValidateINN`): this is a *checksum*, not a registry lookup —
  exactly why it is a pre-LLM filter feeding agent 3, not a verdict.

## Forward requirements for LIC-TASK-047 (app-wiring)

1. Implement the `Recorder` adapter over
   `metrics.CrossCutMetrics.PartyValidationTotal`:
   `RecordPartyValidation(kind, valid)` →
   `.WithLabelValues(string(kind), metrics.BoolLabel(valid)).Inc()`.
   `kind`'s wire strings are pinned (`TestPartyKind_WireStringsPinned`)
   against `metrics.PartyValidationType`.
2. The per-agent `Run` (LIC-TASK-025..033) owns block ORDER and must
   match the agent's system prompt envelope exactly
   (`ai-agents-pipeline.md` §1–9). Agent 3 inserts the
   `ValidationFacts` Part between `<party_roles>` and
   `<party_details_block>`.
3. `prompts.LoadPrompt(agentID)` provides the `system` argument; a
   missing prompt is already a fatal startup error there.
