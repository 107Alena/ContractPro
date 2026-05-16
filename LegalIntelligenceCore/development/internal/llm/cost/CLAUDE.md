# cost Package — CLAUDE.md

Cost & Usage Tracker for the Legal Intelligence Core (LIC-TASK-018,
`llm-provider-abstraction.md` §4, `observability.md` §3.4). The Provider
Router (LIC-TASK-019) calls it after every `provider.Complete()`.

## Files

- **cost.go** — `Outcome` (local mirror of `metrics.LLMCallOutcome`),
  `Recorder`+`noopRecorder` seam, `Usage`, `Tracker`, `NewTracker`,
  `ObserveSuccess`, `ObserveCall`.

## API

- `NewTracker(table pricing.Table, rec Recorder) (*Tracker, error)` —
  fail-fast on empty table; nil `rec` ⇒ no-op.
- `(*Tracker) ObserveSuccess(u Usage) float64` — records the 5 usage
  families + a `success` call; **returns the computed USD** for the
  Router's OTel span.
- `(*Tracker) ObserveCall(provider, model, agent, outcome)` —
  `lic_llm_calls_total` only (repair|fail|fallback).

## Conventions & deliberate decisions

- **Hermetic.** Imports only stdlib + `internal/domain/{port,model}` +
  sibling `internal/llm/pricing` — exactly like every `internal/llm/*`
  sibling. No `internal/infra/observability/metrics` import: *no*
  `internal/*` package imports it before app-wiring; the Prometheus vecs
  are inverted behind the `Recorder` seam and the adapter over
  `*metrics.LLMMetrics` is wired in LIC-TASK-047 (code-architect OQ-1;
  mirrors `ratelimit`'s `Observer`).
- **One batched `RecordUsage`, not granular Add*.** The "5 usage families
  fire together for one success" atomicity invariant lives inside the
  Tracker, not duplicated across the wiring adapter and every test fake
  (code-architect MF-1; mirrors `ratelimit`'s "one seam, grouped signals").
  `RecordCall` is a separate method: different lifecycle (every outcome)
  and label set (`+outcome`).
- **`ObserveSuccess` emits BOTH usage AND `calls_total{success}`.** A
  success is a call too; recording calls only for repair/fail/fallback
  would undercount `lic_llm_calls_total` by the entire success volume
  (code-architect MF-2).
- **Cost is always returned, including 0.0 for an unknown model.** So the
  Router can set the `lic.llm.cost_usd` OTel span attribute
  deterministically.
- **Tracker never owns the OTel span.** It returns the cost; the Router
  (019), which owns the `lic.llm` span, attaches it via the existing
  `tracer.AttrLLMCostUSD` alongside `lic.pipeline.organization_id`.
  Importing `tracer`/`otel` here would break hermeticity *and*
  double-own `lic.llm.cost_usd` (code-architect OQ-2).
- **No `organization_id` anywhere.** `Usage` deliberately has no org field,
  making "no org id in any cost Prometheus label" a structural guarantee,
  not a discipline. Per-tenant cost is OTel-only (`observability.md`
  §3.10/§4.2).
- **Unknown model ⇒ record everything, cost 0, distinct signal.** Telemetry
  (tokens/latency/`calls{success}`) is still recorded; cost contribution
  is 0; `Recorder.UnknownModel(provider, model)` fires. `model` is passed
  for **logging only** — the LIC-TASK-047 adapter MUST aggregate it into a
  provider-labelled counter (e.g.
  `lic_llm_pricing_unknown_model_total{provider}`) and MUST NOT put the
  raw model string in a Prometheus label: an arbitrary model string is an
  unbounded-cardinality vector, the same class §3.10 closes for
  `organization_id` (code-architect MF-3). A missing price never errors or
  drops the call — a cost-tracking miss must never fail the LLM pipeline.
- **Negative token clamp.** Clamped to 0 at this boundary *and* inside
  `pricing.CostUSD`. `prometheus.Counter.Add(<0)` panics; a buggy adapter
  must never crash the agent pipeline (code-architect OQ-7).
- **Label values from typed sources only.** `provider` from
  `port.LLMProviderID.String()`, `agent` from `string(model.AgentID)` —
  never raw caller strings — so `{provider,model,agent}` cardinality is
  bounded as `metrics/llm.go` budgets (~405 series). `model` is the only
  free string; another reason MF-3's unknown-model counter must not be
  `model`-labelled.
- **`Outcome` is a local mirror of `metrics.LLMCallOutcome`.** Declared
  here (not imported) to stay hermetic; `cost_test.go`
  (`TestOutcome_WireStringsPinned`) pins the four wire strings so the
  mirror cannot silently drift from the `observability.md` §3.4 SSOT
  (same pattern as `ratelimit` mirroring rather than importing labels).
- **Immutable after construction.** `table` is read-only, `rec` is fixed,
  no other state → safe for concurrent use by the parallel errgroup
  pipeline without locking (`TestObserveSuccess_Concurrent`, `-race`).
- **Latency unit contract.** `Usage.Latency` is a `time.Duration`; the
  source is `CompletionResponse.LatencyMs` (int64 millis) which the Router
  converts via `time.Duration(resp.LatencyMs)*time.Millisecond`. The
  adapter observes `Latency.Seconds()` into `lic_llm_latency_seconds`
  (`llmLatencyBuckets` is seconds-based) — an off-by-1000 here silently
  corrupts every latency panel.

## Forward requirements for LIC-TASK-047 (app-wiring)

1. Implement the `Recorder` adapter over `*metrics.LLMMetrics`:
   `RecordUsage` → `InputTokensTotal`/`CachedTokensTotal`/
   `OutputTokensTotal`/`CostUSDTotal`(`.Add`) + `LatencySeconds`
   (`.Observe(latency.Seconds())`); `RecordCall` → `CallsTotal`;
   `UnknownModel` → a **provider-labelled** counter + a WARN log carrying
   the model string (NOT a metric label — MF-3).
2. `pricing.Load(cfg.Pricing.TablePath)` failure ⇒ **fatal** startup
   error (code-architect OQ-6).
3. Map `Usage.Latency = time.Duration(resp.LatencyMs)*time.Millisecond`
   and pass `resp.ProviderID` / `resp.Model` / `req.AgentID` straight
   through (no string munging).
4. `ObserveCall` records its `outcome` verbatim and never validates
   (telemetry must not fail the pipeline). Callers (019/047) MUST pass
   only the exported `Outcome` constants — never a raw `Outcome("…")`
   conversion — so a typo is a compile error, not a silent bad label
   (code-reviewer L-4).
