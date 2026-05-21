# tokenestimator Package — CLAUDE.md

**Token Estimator** (LIC-TASK-021, `high-architecture.md` §6.7 /
ASSUMPTION-LIC-12, `ai-agents-pipeline.md` `INPUT_TRUNCATED` warning
schema). The SOLE source of `TruncatedBytes` / `TotalBytes` counts that
the Pipeline Orchestrator (LIC-TASK-036) forwards to
`aggregator.Input.Truncation` for `INPUT_TRUNCATED` warning rendering
(`aggregator/CLAUDE.md` forward note #2), AND the real implementation of
`base.TokenEstimator` via `Fit` (`base/seams.go:135-137`) — the
LIC-TASK-047 wiring injects an `*Estimator` into `base.Deps.Estimator`.

Constructor: `NewEstimator(cfg Config) (*Estimator, error)` —
stutter-free `NewTypeName` per `feedback_constructors.md`; fail-fast on
invalid `Config` via `errors.Join` (the
`scoring.ScoringConfig` / `aggregator.Config` precedent).

## Files

- **estimator.go** — package doc, `DefaultCharsPerToken`, `Config`
  (+`validate`), `Estimator`, `NewEstimator`, `EstimateTokens`,
  `TruncationInfo`, `Truncate`, `TruncateToInputBudget`,
  `CheckIngestSize`, `Fit`.
- **estimator_test.go** — full behavioural suite (28 functions; the
  load-bearing pins are `_HeadTailProportions`,
  `_RuneBoundary_UTF8Valid`, `_DefensiveFallback_SmallBudget`,
  `_AttributeWireParity_WithOrchestrator`,
  `_OverBudget_FalseAtExactLimit`, `_NoMutation`,
  `_PropertyTotalTokensInvariant`, `_ConcurrentRaceClean`).
- **internal_test.go** — `TestHermeticImports` (2-entry allowlist:
  `model`, `port`) + `TestGofmtClean` (via `go/format`; the sandbox
  blocks `go fmt`); mirrors `internal/agents/base/internal_test.go`.
- **external_test.go** — `package tokenestimator_test`. Hosts the
  compile-time `var _ base.TokenEstimator =
  (*tokenestimator.Estimator)(nil)` wiring pin (the SOLE place
  `internal/agents/base` may be imported from this package — and it is
  a `_test.go` file, so `TestHermeticImports` does not flag it). Also
  hosts the black-box `TestFit_SeamContract_BlackBox` smoke test.
- **CLAUDE.md** — this file.

## API

- `NewEstimator(Config) (*Estimator, error)`.
- `(*Estimator) EstimateTokens(text string) int` — `⌈len([]rune(text)) /
  CharsPerToken⌉`. Rune-aware (UTF-8 / Cyrillic safe). Conservative
  ceiling rounding (D4).
- `(*Estimator) Truncate(text string, maxTokens int) (string,
  *TruncationInfo)` — `§6.7` head-60 / tail-40 rule; returns
  `(text, nil)` when no truncation needed OR when the budget would not
  drop any bytes (D-DEFENSIVE). Rune-aware slicing — never byte-indexed.
- `(*Estimator) TruncateToInputBudget(text string) (string,
  *TruncationInfo)` — convenience wrapper around
  `Truncate(text, cfg.MaxInputTokens)`.
- `(*Estimator) CheckIngestSize(model.InputArtifactsCompact) error` —
  `DOCUMENT_TOO_LARGE` when `Σ len(raw) > cfg.MaxIngestedBytes`;
  byte-parity with `orchestrator.go:393-403` (D5).
- `(*Estimator) Fit(port.CompletionRequest) (int, bool)` —
  satisfies `base.TokenEstimator` (`base/seams.go:135-137`). NEVER
  mutates `req`. Strict `>` against `cfg.MaxAgentInputTokens`.
- `TruncationInfo{TruncatedBytes int, TotalBytes int}` — same wire
  shape as `aggregator.TruncationInfo`; declared here so the LIC-TASK-036
  adapter can construct one without re-importing `aggregator` (D1).
- `DefaultCharsPerToken = 3.5` — empirical RU heuristic
  (tasks.json acceptance criterion: "приближённо 1 token ≈ 3.5 RU chars").

## Conventions & deliberate decisions

- **Hermetic (the universal `internal/agents|llm/*` invariant).** Imports
  ONLY stdlib + `internal/domain/{model,port}`. NO `internal/config`
  (`Config` is ctor-injected by app-wiring — the
  `aggregator`/`base.Config` precedent), NO `internal/agents/base` (the
  LIC-TASK-047 wiring goes the OTHER way — `base.Deps.Estimator` accepts
  an `*Estimator` via the `base.TokenEstimator` seam; importing `base`
  here would invert the dependency direction and create a cycle once
  `base` adds a real estimator), NO `internal/infra/*` (no telemetry
  seam — pure compute). `TestHermeticImports` pins this against the
  2-entry allowlist `{model, port}`.
- **D1 — own `TruncationInfo` declaration, NOT an import of
  `aggregator.TruncationInfo`.** The wire shape is identical, but the
  type is owned independently by each side: this package's
  `TruncationInfo` is what `*Estimator` produces; `aggregator`'s is what
  `Aggregate` consumes. The LIC-TASK-036 orchestrator adapts between
  them by value-copy (`&aggregator.TruncationInfo{TruncatedBytes:
  ti.TruncatedBytes, TotalBytes: ti.TotalBytes}`) — a one-line
  adaptation that costs the layering cycle nothing. Importing
  `internal/application/aggregator` from a leaf agents package would
  reverse the call direction and is forbidden by `aggregator`'s own
  hermeticity invariant.
- **D2 — fail-fast `Config.validate` via `errors.Join`.** Returns every
  violation in one error so the operator sees all bad fields at startup,
  not the first only (the `aggregator.Config.validate` / `stages` /
  `scoring.ScoringConfig` precedent). `validate` has a pointer receiver
  because it MUTATES `CharsPerToken` to apply the `DefaultCharsPerToken`
  default when it was passed as `0` — a small, documented exception to
  the value-receiver default for `validate`-style methods.
  `NewEstimator` returns the `errors.Join` VERBATIM (no `%w` wrap) so
  the call site (LIC-TASK-047) can table-test against substrings without
  knowing about an outer wrapper.
- **D3 — rune-aware slicing (UTF-8 / Cyrillic safety).** Build
  `runes := []rune(text)` once; slice with rune indices; re-encode with
  `string(runes[lo:hi])`. Byte-indexing (`text[i]`) would split a 2-byte
  Cyrillic sequence and produce invalid UTF-8. Pinned by
  `TestTruncate_RuneBoundary_UTF8Valid` (Cyrillic input, `utf8.Valid-
  String` asserted).
- **D4 — conservative ceiling rounding (`math.Ceil`).** `EstimateTokens`
  is `⌈runes/CharsPerToken⌉`, not floor/round. The ceiling is the
  conservative direction: a returned `est<=maxTokens` GUARANTEES true
  tokens `<=maxTokens` under any real tokeniser within the heuristic
  accuracy. The opposite direction (floor) would let `est==maxTokens`
  pass when actual tokens exceed it — a real budget breach. Same
  rationale for Fit's strict `>` against `MaxAgentInputTokens`:
  `est==MaxAgentInputTokens` passes (the est itself was already
  conservatively rounded up).
- **D5 — `CheckIngestSize` BYTE-PARITY with
  `orchestrator.go:393-403`.** The returned `*model.DomainError`'s
  `Code` / `Stage` / `Retryable` / `Attributes` (incl. their Go types:
  `ingested_bytes int`, `limit int` — both Go `int` to mirror
  `pipeline.Config.MaxIngestedBytes` today) MUST be byte-identical to the
  inline form 036 ships today. The LIC-TASK-036 forward-note #5 enabler:
  when 036 later delegates this check (`pipeline/CLAUDE.md` forward
  note #5), observable behaviour does NOT change. Strict `>` comparison
  (`total == limit` passes — matches `orchestrator.go:398`).
  `.WithRetryable(false)` is set EXPLICITLY even though the catalog
  default for `ErrCodeDocumentTooLarge` already has `retryable: false` —
  the explicit call mirrors 036 character-for-character and protects
  against a future catalog edit silently breaking parity. Pinned by
  `TestCheckIngestSize_AttributeWireParity_WithOrchestrator` (constructs
  both sides inline, `reflect.DeepEqual` on `.Attributes`, plus a
  type-switch on each attribute value).
- **D-DEFENSIVE — `Truncate` returns `(text, nil)` when the budget
  cannot drop any bytes.** If `maxTokens` is so small that
  `headRunes+tailRunes >= total` OR either side `<=0`, the function
  falls back to the no-op return so the invariant "non-nil
  `TruncationInfo` ⇒ `TruncatedBytes>=1`" holds — matching the
  `INPUT_TRUNCATED.truncated_bytes minimum:1` schema in
  `ai-agents-pipeline.md:1362`. Without this fallback, a degenerate
  budget (e.g. `maxTokens=1`) would produce `headRunes=0, tailRunes=3`
  and either output the tail-only string (silently dropping the head)
  or fire a non-nil `*TruncationInfo` with `TruncatedBytes==0`, both of
  which break the upstream schema. Pinned by
  `TestTruncate_DefensiveFallback_SmallBudget`.
- **D-NO-MARKER — no `[…]` / `<truncated/>` join marker between head
  and tail.** The §6.7 spec is silent on a join marker; v1 emits the
  concatenated `head+tail` directly. Two reasons:
  (a) any marker would inflate the post-truncation rune count and push
  `EstimateTokens(truncated)` above `maxTokens`, breaking the property
  invariant `info!=nil ⇒ EstimateTokens(truncated)<=maxTokens` that
  `TestTruncate_PropertyTotalTokensInvariant` pins;
  (b) the truncation is operating on EXTRACTED_TEXT pre-envelope (§6.7
  per-artifact upstream truncation, before `Spec.Parts` /
  `promptbuilder.Build`), so the LLM sees the head and tail wrapped in
  the per-agent prompt structure — the marker is not visually load-
  bearing for the model. If a future spec mandates a marker, it MUST be
  added here AND `maxTokens` MUST be lowered by `len(marker)/CharsPer-
  Token` runes before computing the head/tail split.
- **`Fit` is non-mutating (MF-3 invariant).** `port.CompletionRequest`
  is passed by value, so even a `req.System = …` write would not
  propagate; but `req.PriorTurns` is a slice header that aliases the
  caller's backing array. `Fit` reads the slice (range loop) but never
  writes through it — pinned by `TestFit_NoMutation`
  (`reflect.DeepEqual` pre/post Fit on both `req` and a manually-copied
  `req.PriorTurns`).
- **Immutable / concurrency-safe.** `*Estimator` is fixed after
  construction (Config is a value copy; no per-call mutable state) and
  all per-call state is stack-local, so one instance is shared by the
  parallel errgroup pipeline without locking.
  `TestEstimator_ConcurrentRaceClean` (`-race`, 32 goroutines × 50
  iterations) pins it.
- **gofmt self-check.** The sandbox blocks `gofmt` / `go fmt`;
  `TestGofmtClean` asserts canonical formatting in-process via
  `go/format` (same approach as `base` / `schemavalidator` /
  `aggregator`).

## Forward notes (recorded, owners elsewhere)

1. **LIC-TASK-036 (Pipeline Orchestrator).** Will source
   `aggregator.Input.Truncation` from `*Estimator.TruncateToInputBudget`
   applied to the per-version EXTRACTED_TEXT artifact (the
   `pipeline/CLAUDE.md` forward note #5). The orchestrator adapts
   `*tokenestimator.TruncationInfo` → `*aggregator.TruncationInfo` by
   value-copy (one line); both types share the same wire shape (D1).
   Today 036 passes `Input.Truncation = nil` and uses an inline
   `MAX_INGESTED_BYTES` cap (`orchestrator.go:393-403`); when 036
   migrates, `CheckIngestSize` here delivers byte-identical observable
   behaviour (D5).
2. **LIC-TASK-047 (app-wiring).** `cfg := tokenestimator.Config{from
   config.AgentsConfig.MaxInputTokens / MaxAgentInputTokens /
   MaxIngestedBytes / CharsPerToken}`; `est, err :=
   tokenestimator.NewEstimator(cfg)`; assign `base.Deps.Estimator = est`
   for every per-agent `NewBaseAgent` call (the compile-time
   `var _ base.TokenEstimator = (*tokenestimator.Estimator)(nil)`
   assertion in `external_test.go` pins the signature). For 036, the
   same `*est` is injected into the orchestrator's
   `Config.TokenEstimator` (or successor) — one Estimator instance
   shared across all 9 agents AND the orchestrator (immutable +
   concurrency-safe).
3. **v1.1 — per-model tokeniser.** The 3.5-chars-per-token heuristic is
   deliberately model-agnostic and provider-agnostic; v1.1 may swap in
   a real BPE tokeniser per-model (Claude tiktoken-like, OpenAI
   tiktoken, Gemini SentencePiece) behind the same `Estimator` API.
   `EstimateTokens`/`Truncate`/`Fit` already operate on runes, so the
   public contract does not change — only the constant-rate divisor
   becomes a per-model function. The conservative ceiling (D4) still
   holds.
