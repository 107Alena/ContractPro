# pricing Package — CLAUDE.md

Per-model LLM price-table loader for the Legal Intelligence Core
(LIC-TASK-018, `llm-provider-abstraction.md` §4.1, `configuration.md`
§2.15). Pure read-only data: `internal/llm/cost` depends on it to turn
token counts into `lic_llm_cost_usd_total`, the metric that gates the
`LICCostSpike` alert (`observability.md` §4.3).

## Files

- **pricing.go** — `ModelPricing`, `Table`, `Table.CostUSD` (the §4.1
  formula SSOT), `Load` (read + strict YAML decode + validate).

## API

- `Load(path string) (Table, error)` — descriptive, path-wrapped errors
  only (file-read & YAML-parse via `%w`; validation errors are plain but
  path-scoped — not sentinels, because 047 only needs "fatal, don't
  swallow", not failure-kind branching); never panics; no fallback table.
- `Table.CostUSD(model string, input, cached, output int) (usd float64, known bool)`.

## Conventions & deliberate decisions

- **Pure data, near-hermetic.** Imports only stdlib + `go.yaml.in/yaml/v2`.
  No domain/port, no metrics, no first-party deps — the cost Tracker
  imports *this*, never the reverse (acyclic edge; code-architect OQ-3).
- **Dependency choice: `go.yaml.in/yaml/v2`, NOT `gopkg.in/yaml.v3`.**
  `go.yaml.in/yaml/v2 v2.4.2` is already pinned in `go.sum` and already in
  the build graph as a `prometheus/common` transitive dep, so it is in the
  offline module cache — importing it added **zero** new module, only an
  indirect→direct `go.mod` reclassification (`go mod tidy` produced exactly
  that one-line move; `go.sum` unchanged). `gopkg.in/yaml.v3` appears in
  `go.sum` as a checksum-only transitive entry but is NOT in the require
  graph — using it *would* be a genuine new direct dependency with offline
  risk. Hand-rolling a YAML-subset parser was rejected: a parser bug →
  mis-billed cost → false/missed `LICCostSpike` (the exact failure §4.3
  warns of); robustness wins when the data gates a money alert
  (code-architect OQ-5). This differs from why `ratelimit` faked
  Redis/Lua — there the deps were genuinely absent offline; here it is
  present.
- **Strict decode.** `yaml.UnmarshalStrict` rejects unknown keys: a typo
  like `imput_per_m_token_usd` in a money-critical file must fail loud at
  startup, not silently zero a rate.
- **Reject non-finite rates.** Validation rejects `< 0` *and* `NaN`/`±Inf`
  (`.nan`/`.inf`/`-.inf` are valid YAML floats). `NaN < 0` and `+Inf < 0`
  are both false, so a bare sign check would let them through → every
  `CostUSD` returns `NaN`/`Inf`, silently defeating `LICCostSpike` or
  poisoning a Prometheus counter — the same threat model strict decode
  exists for (golang-pro Y1).
- **Deterministic error order.** Models are validated in sorted key order
  so a multi-defect pricing file fails with the *same* message every run
  (a money-critical file is debugged against a stable error, not a
  map-order lottery; golang-pro E1).
- **`cached_input_per_m_token_usd` absent ⇒ explicit 0.0 + observable.**
  `configuration.md` §2.15's example omits the cached key; `Load` accepts
  it as optional and on absence sets `0.0` **and** flips
  `ModelPricing.CachedRateDefaulted=true` so app-wiring (LIC-TASK-047) can
  log "model X: cached rate defaulted to 0.0". The default is **not** the
  input rate — that would re-introduce the up-to-10× Anthropic cache-hit
  over-bill that splitting cached out of input exists to prevent
  (`llm-provider-abstraction.md` §4.1, `metrics/llm.go` doc; code-architect
  MF-5).
- **Pointer-decode to distinguish absent from 0.0.** `yamlEntry` uses
  `*float64` so a missing required `input`/`output` is an error, and an
  absent `cached` is distinguishable from an intentional `0.0`.
- **Per-term float64 promotion + negative clamp.** `CostUSD` promotes each
  token count to `float64` *before* the multiply (no int overflow in the
  intermediate — accuracy gates `LICCostSpike`; code-architect MF-4) and
  clamps negative counts to 0 (an upstream adapter bug must never yield a
  negative cost; a negative downstream would panic
  `prometheus.Counter.Add`).
- **`Table` is read-only after `Load`.** Never mutate it (e.g. caching
  unknown models in): one `Table` is shared across the parallel errgroup
  pipeline; concurrent reads of a never-written map are race-free, a write
  is not (code-architect OQ-7).

## Forward requirement for LIC-TASK-047

Missing/invalid `LIC_PRICING_TABLE_PATH` MUST be a **fatal startup error**
(consistent with `ratelimit.NewLimiter` / `kvstore.NewClient` fail-fast).
A silently empty table bills every call at $0 and hides every spike from
`LICCostSpike` — worse than not starting (code-architect OQ-6). `Load`
already returns clear typed errors; 047 must not swallow them.
