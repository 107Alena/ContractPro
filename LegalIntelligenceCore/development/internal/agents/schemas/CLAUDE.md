# schemas Package — CLAUDE.md

Embeds and serves the 9 LIC agent **output JSON Schemas** (LIC-TASK-020,
`ai-agents-pipeline.md` §1–9). The Schema Validator + 1-shot repair loop
(LIC-TASK-023) and BaseAgent (LIC-TASK-024) consume `LoadSchema`.

## Files

- **schemas.go** — `schemaFS` (`//go:embed *.json`), the `basenames`
  SSOT map, `LoadSchema`, `Validate`, `validTopLevelType`.
- **`<agent>.json`** ×9 — verbatim schemas copied byte-for-byte from the
  `### JSON-схема выхода` fenced blocks of `ai-agents-pipeline.md` §1–9
  (same 9 basenames as the `prompts` package).

## API

- `LoadSchema(model.AgentID) ([]byte, error)` — **verbatim** bytes;
  errors (never panics) on unknown id / missing / empty / not-well-formed
  JSON. Bytes are returned unchanged — callers MUST NOT assume canonical
  key order (LIC-TASK-023's JSON-Schema lib needs the exact document).
- `Validate() error` — all 9 present, well-formed JSON, `$schema` pinned
  to draft-07 by exact string equality, non-empty `title`, valid
  top-level `type`, no orphan `*.json`; `errors.Join`, deterministic.

## Scope boundary (forward requirement)

TASK-020 does **pragmatic** schema validation: well-formed JSON +
exact-string draft-07 `$schema` pin + minimal top-level shape
(`title` + a valid draft-07 root `type`). Full JSON-Schema **meta-schema
conformance** and **instance validation** are owned by **LIC-TASK-023**,
which adds the real JSON-Schema library (kaptinlin/jsonschema or
xeipuuv/gojsonschema). Pulling that library in here would duplicate that
dependency decision and break hermeticity (mirrors how `pricing` does
structural validation only; semantic correctness is the caller's
contract). Acceptance criterion 7 ("valid as JSON Schema draft-07") is
satisfied in spirit by the well-formed + draft-07-pin checks.

## Conventions & deliberate decisions

- **Root `type` may be `array`, not only `object`.** `Recommendations`
  (§6) is a top-level `"type":"array"`; `validTopLevelType` accepts any
  draft-07 primitive type name (string or array form). Requiring
  `"object"` would wrongly reject it — locked by `TestSchemaRootTypes`
  (code-architect must-fix #1).
- **`$schema` exact-string pin.** Only
  `http://json-schema.org/draft-07/schema#` passes; draft/2020-12 or
  trailing-slash variants fail loud (pricing strict-decode philosophy;
  code-architect must-fix #2).
- **Verbatim bytes.** `LoadSchema` returns the file unchanged (validated
  for well-formedness, never re-marshalled) so the downstream lib sees
  the exact document.
- **Explicit `AgentID → basename` map / hermetic / `embed.FS`
  unexported / compile-time presence guard / SSOT-table path-segment
  guard / `Validate`-not-`init()`-panic** — identical rationale to the
  sibling `prompts` package (code-architect Q2/Q4).

## Forward requirement for LIC-TASK-023 / 024 / 047

Missing/invalid embedded schemas MUST be a **fatal startup error**
(pricing fail-fast). `Validate` returns clear deterministic errors;
wiring must call it at startup and must not swallow them. LIC-TASK-023
layers the real meta-schema/instance validation on top of these verbatim
bytes.
