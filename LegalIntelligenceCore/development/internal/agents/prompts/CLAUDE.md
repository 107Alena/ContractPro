# prompts Package — CLAUDE.md

Embeds and serves the 9 LIC agent **system prompts** (LIC-TASK-020,
`ai-agents-pipeline.md` §1–9). The Prompt Builder (LIC-TASK-022) and
BaseAgent (LIC-TASK-024) consume `LoadPrompt` to obtain the System
message of every LLM call.

## Files

- **prompts.go** — `promptFS` (`//go:embed *.txt`), the `basenames`
  SSOT map, `LoadPrompt`, `Validate`.
- **`<agent>.txt`** ×9 — verbatim system-prompt text copied byte-for-byte
  from the `### Системный промпт` fenced blocks of `ai-agents-pipeline.md`
  §1–9 (Russian legal-domain SSOT). Basenames: `type_classifier`,
  `key_params`, `party_consistency`, `mandatory_conditions`,
  `risk_detection`, `recommendation`, `summary`, `detailed_report`,
  `risk_delta`.

## API

- `LoadPrompt(model.AgentID) (string, error)` — verbatim text; errors
  (never panics) on unknown id / missing / empty file. No trimming: the
  trailing newline of the SSOT block is preserved.
- `Validate() error` — all 9 present & non-empty, no orphan `*.txt`;
  `errors.Join`, deterministic order.

## Conventions & deliberate decisions

- **Verbatim, no reflow.** `.txt` files are copied byte-for-byte from the
  SSOT; `Validate`'s non-empty check is a floor, not a content check —
  fidelity is a review/diff concern, not automated here.
- **Explicit `AgentID → basename` map, not derived.** SSOT is an
  enumerated table checked against `model.AllAgentIDs()` (house style: cf.
  `domain/model` `errorCatalog`/`agentIDSet`), not a string transform of
  the wire id (code-architect Q2).
- **SSOT-table path-segment guard.** `Validate` rejects a `basenames`
  value that is empty, dot-prefixed, or contains `/`, `\`, or `..` — a
  stray table edit fails loud at startup instead of silently resolving a
  different embedded path (same fail-loud reason `pricing` rejects an
  empty model key).
- **FS-injectable cores.** `LoadPrompt`/`Validate` are thin wrappers over
  unexported `loadPrompt`/`validate(fs.FS, map)`; the public API is
  unchanged, but the deterministic-error-order contract is tested
  directly with `testing/fstest.MapFS` (test-only stdlib — hermeticity
  preserved).
- **Hermetic.** stdlib only + `internal/domain/model` (the typed
  `AgentID` key — necessary, the loader API is keyed by it; this is the
  *only* first-party import). No infra/llm deps; acyclic.
- **`embed.FS` unexported.** Callers cannot bypass the `AgentID`-keyed
  loader to read arbitrary files.
- **Compile-time presence guard.** `//go:embed *.txt` is a *compile*
  error if zero files match, and `TestValidate` fails if any of the 9 is
  absent/empty — that, not git, is what enforces the assets ship with
  `prompts.go` (git would happily commit the `.go` alone; the build/test
  would then fail loud). The scaffolding `.gitkeep` placeholders were
  `git rm`-ed once these packages held real content (the embed glob never
  matched `.gitkeep` anyway — dotfiles are excluded).
- **`Validate`-returns-error, not `init()`-panic.** Embedded files are
  build *inputs*, not compiled-in Go constants — the established response
  to bad *data* in this codebase is a typed deterministic error surfaced
  as a **fatal startup error** by app-wiring, never a package-load panic
  (pricing precedent; `init()`-panic is reserved for invariants over Go
  constants — `domain/model error_codes.go`; code-architect Q4).

## Forward requirement for LIC-TASK-024 / 047

Missing/invalid embedded prompts MUST be a **fatal startup error**
(consistent with `pricing.Load` / `ratelimit.NewLimiter` /
`kvstore.NewClient` fail-fast). `Validate` returns clear, deterministic
errors; wiring must call it at startup and must not swallow them.
