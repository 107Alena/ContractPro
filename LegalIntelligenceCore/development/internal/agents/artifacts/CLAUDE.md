# artifacts Package — CLAUDE.md

**Shared DP-faithful artifact decoders** for the LIC per-agent packages
(LIC-TASK-026, `ai-agents-pipeline.md` §1–9). Created by LIC-TASK-026 as
the **resolution** of `typeclassifier/CLAUDE.md` forward-note #3 /
code-reviewer LOW-2: agents 1,2,4,5,7 all reconstruct `EXTRACTED_TEXT`,
and re-deriving that DP-faithful page join per package risks silent
divergence from DP semantics. This is the ONE place that mirror lives, so
the "byte-identical to DP" claim is asserted by a **single** pinning test
instead of by-comment in every per-agent package.

## Files

- **artifacts.go** — package doc, `ExtractedText` (minimal DP wire
  decode), `ExtractedText.FullText()` (the DP-faithful page join).
- **artifacts_test.go** — `TestFullText_DPSemantics` (the single
  byte-identity assertion), `TestExtractedText_DecodesDPWireShape`.
- **internal_test.go** — `TestHermeticImports` (stdlib-only allowlist) +
  `TestGofmtClean` self-check (mirrors `base`).

## API

- `type ExtractedText struct{ Pages []struct{ Text string } }` — decode
  via the caller's `json.Unmarshal` (a decode error is the per-agent
  `Spec.Parts`'s to surface as `INTERNAL_ERROR` — same idiom as Agent 1's
  committed local copy).
- `(ExtractedText) FullText() string` — zero pages ⇒ `""`; one page ⇒ its
  text **verbatim** (no trailing newline); N pages ⇒ joined by **exactly
  one** `'\n'`. Byte-faithful mirror of DP's
  `model.ExtractedText.FullText()`
  (`DocumentProcessing/development/internal/domain/model/document.go:33-53`).

## Conventions & deliberate decisions

- **Stdlib-only hermeticity (strictest in the agents layer).**
  `TestHermeticImports` forbids ANY `contractpro/` import (not even
  `internal/domain`) and any third-party. A shared leaf with first-party
  deps would re-introduce the coupling this package exists to remove. It
  MUST NOT import the `DocumentProcessing` Go module — LIC's
  no-cross-module rule + the byte-faithful
  `InputArtifactsCompact = map[ArtifactType]json.RawMessage` defer-decode
  design mandate a local mirror validated against DP's documented contract
  by **test**, not by import.
- **The DP-identity claim is a TEST, not a comment.** Agent 1 asserted
  "byte-identical to DP" by godoc only. `TestFullText_DPSemantics`
  encodes DP's documented `FullText` contract (the three cases +
  no-trimming + empty-interior-page) as literal expectations — the single
  point where a future DP page-join change would be caught for the whole
  agents layer.
- **v1 scope is minimal (YAGNI / house style).** Only `EXTRACTED_TEXT`
  is centralised — the artifact whose reconstruction semantics must stay
  byte-identical to DP. `DOCUMENT_STRUCTURE` (agents 1/3 only) and
  `SEMANTIC_TREE` (passed through verbatim, never decoded — Agent 2
  routes raw bytes through the prompt builder) are deliberately NOT
  modelled here. Add a decoder only when a SECOND consumer actually needs
  one (code-architect Q1).
- **Agent 1's local copy is a recorded duplicate, NOT refactored here.**
  Re-touching the reviewed, committed LIC-TASK-025 to consume this
  package is unrequested scope creep that risks regressing its pinned
  tests. Retiring `typeclassifier`'s local `extractedText`/`fullText` is
  a recorded forward cleanup (see `typeclassifier/CLAUDE.md`
  forward-note #3, now annotated RESOLVED-by-026) — to be bundled into
  whichever later agent/cleanup task is cheapest, not done mid-flight.
- **`gofmt` self-check.** The sandbox blocks `gofmt`/`go fmt`;
  `TestGofmtClean` asserts canonical formatting in-process via
  `go/format` (same approach as `base`/`schemavalidator`).

## Consumers & forward notes

- **LIC-TASK-026 (Agent 2 — keyparams)** is the first consumer:
  `keyparams/spec.go` decodes `EXTRACTED_TEXT` via `artifacts.ExtractedText`
  + `FullText()` and emits the full text (no compaction — the >80K
  head/tail rule is LIC-TASK-021's upstream job).
- **LIC-TASK-027..033 (agents 4,5 — and 7 with its own §7 compaction)**
  consuming `EXTRACTED_TEXT` SHOULD reuse `artifacts.ExtractedText` rather
  than re-declaring a local copy, so the DP-identity invariant stays
  asserted once.
- **Later cleanup (owner: a dedicated task or the cheapest later agent
  task).** Migrate `typeclassifier` off its local `extractedText`/
  `fullText` onto this package, then delete the duplicate; keep
  `typeclassifier`'s envelope/compaction tests green.
