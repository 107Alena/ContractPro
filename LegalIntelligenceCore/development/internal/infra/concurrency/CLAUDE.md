# concurrency Package — CLAUDE.md

**Concurrency Limiter** (LIC-TASK-010, `high-architecture.md` §6.14 /
§6.9 / §911, `ai-agents-pipeline.md` §"Семафоры" §1705-1708,
`observability.md` §3.2). A counting `Semaphore` (buffered-channel) that
bounds concurrent operations on one LIC instance, serving both backpressure
sites of high-architecture §6.14 with a single primitive:

- **job-level** — `LIC_PIPELINE_CONCURRENCY` (default 5): the cap on
  simultaneously processed contract versions. Wired with
  `WithGauge(metrics.Pipeline.ConcurrentJobs)` so the
  `lic_pipeline_concurrent_jobs` gauge tracks in-flight jobs (LIC-TASK-036).
- **LLM-level per provider** — `LIC_LLM_CONCURRENCY_PER_PROVIDER` (default
  10): the cap on simultaneous HTTP calls to one provider. The Provider
  Router (LIC-TASK-016/017/047) builds one **gauge-free** instance per
  provider.

Constructor: `New(max int, opts ...Option) *Semaphore`.

## Files

- **semaphore.go** — package doc, `Gauge` seam + `noopGauge` +
  `var _ Gauge = noopGauge{}`, `Semaphore`, `Option`, `WithGauge`,
  `New`, `Acquire`, `Release`.
- **internal_test.go** — `TestHermeticImports` (EMPTY first-party
  allowlist; active-fail on ALL third-party + listed internals) +
  `TestGofmtClean` (`go/format`).
- **semaphore_test.go** — full suite.
- **CLAUDE.md** — this file.

## API

- `New(max int, opts ...Option) *Semaphore` — panics if `max < 1`.
- `WithGauge(g Gauge) Option` — attaches the in-flight gauge; nil ignored.
- `(*Semaphore) Acquire(ctx) error` — nil on slot acquired; raw
  `ctx.Err()` (unwrapped) on cancel/deadline, no slot consumed.
- `(*Semaphore) Release()` — frees one slot; panics on over-release.
- `Gauge interface { Inc(); Dec() }` — implementations MUST be
  goroutine-safe (`prometheus.Gauge` is).

## Conventions & deliberate decisions (subagent code-architect D1–D9, subagent golang-pro correctness review)

- **D1 — no domain port.** Pure infrastructure: implements **no domain
  port** (LIC has none for concurrency, mirroring `infra/broker` &
  `infra/kvstore`). The DocumentProcessing
  `var _ port.ConcurrencyLimiterPort` assertion is deliberately **NOT**
  copied (would force a single-impl/single-callsite port; YAGNI, the
  kvstore Q1/Q2 "no copy for consistency" precedent).
- **D2 — `New(max int, opts ...Option)` + `WithGauge`.** The variadic
  keeps the acceptance signature `New(max int)` a valid call (`New(5)`)
  while the job-level wiring attaches the gauge via
  `New(n, WithGauge(g))` without a second constructor. Function is named
  **`New`** (not `NewSemaphore`): acceptance pins `New(max int)` and it
  is stutter-free per Effective Go (the `broker.NewClient` /
  `kvstore.NewClient` deliberate exemption from
  `feedback_constructors.md` — recorded so a "consistency" pass does not
  rename it).
- **D3 — hermetic `Gauge` seam, NOT a direct `prometheus` import.**
  `Gauge interface { Inc(); Dec() }` + `noopGauge` default;
  `prometheus.Gauge` satisfies it structurally (wiring passes
  `metrics.Pipeline.ConcurrentJobs` with zero adapter). `broker`/
  `kvstore` import amqp/go-redis because those are **irreducible
  protocol clients**; a two-method gauge is **not** irreducible — it is
  exactly the telemetry dependency LIC inverts everywhere
  (`aggregator.Metrics`/`schemavalidator.Metrics`/`cost.Recorder`).
  The seam also keeps the hermetic `application`/`llm` consumers
  Prometheus-free. Field defaults to `noopGauge{}` so a gauge-free
  instance never nil-checks on the hot path.
- **D4 — single gauge; Inc on acquire-success, Dec on matched Release;
  NO "waiting" gauge.** `observability.md` §3.2 defines exactly one
  concurrency gauge (`lic_pipeline_concurrent_jobs`, in-flight holders).
  DocumentProcessing's `waiting` gauge has **no SSOT** — deliberately
  **not** replicated (the kvstore "do not invent metrics without SSOT"
  rule). `Inc` is emitted after the slot send succeeds, never on
  wait-entry; the gauge counts holders, not waiters.
- **D5 — `Release()` over-release ⇒ PANIC** (the
  `sync.WaitGroup`-negative / `Mutex`-unlock-of-unlocked idiom; LIC
  fail-fast). **Deliberate divergence from DocumentProcessing**, which
  logs a warning: this package is hermetic and has **no logger**, and
  the panic structurally prevents `lic_pipeline_concurrent_jobs` going
  negative — `gauge.Dec()` runs only inside the successful
  token-receive `case`, so the panic path provably never reaches it
  even if `recover()`ed upstream. Recorded so a "consistency with DP"
  change does not reintroduce the silent drop. Pinned by
  `TestRelease_WithoutAcquire_Panics`.
- **D6 — `New` `max < 1` ⇒ PANIC.** Config already
  fail-fast-validates `LIC_PIPELINE_CONCURRENCY >= 1` /
  `LIC_LLM_CONCURRENCY_PER_PROVIDER >= 1` (`config/pipeline.go`,
  `config/llm.go`), so `max < 1` here is a LIC wiring/build defect, not
  operator input. **Deliberate divergence from DocumentProcessing**
  (which clamps `<1 → 1`): clamping silently masks the defect and runs
  the pipeline at the wrong limit — the `NewExecutor`/`NewAggregator`
  "no silent degradation" discipline. Pinned by
  `TestNew_MaxBelowOne_Panics`.
- **D7 — `Acquire` deterministic ctx pre-check; raw `ctx.Err()`.**
  `if err := ctx.Err(); err != nil { return err }` is the **first
  statement** of `Acquire`. Without it, a `select` over a free slot AND
  an already-done ctx would pick pseudo-randomly (Go spec) — a
  pre-cancelled ctx could non-deterministically consume a slot. This is
  intentionally **stricter than DocumentProcessing** (whose
  `…CancelledContextWhenSlotAvailable` test asserts the opposite — that
  DP test is **not** ported). The error is the raw
  `context.Canceled`/`context.DeadlineExceeded`, unwrapped — the
  codebase-wide "context errors pass through raw" convention
  (kvstore/broker CLAUDE.md); this package never imports
  `internal/domain/model`. Pinned (×2000 loop to defeat the select
  tie-break) by
  `TestAcquire_PreCancelledCtx_ReturnsRawErrAndConsumesNoSlot`.
- **D8 — scope = the `Semaphore` primitive ONLY.** No
  `map[providerID]*Semaphore`, no provider-router glue: LLM-level
  per-provider ownership is the Provider Router's
  (`high-architecture.md` §6.9/§741), job-level is the consumer/
  Orchestrator's (§911). The `WithGauge` option is precisely what lets
  the *same* primitive serve gauge-attached job-level and gauge-free
  per-provider use without this package knowing either context (the
  kvstore "single generic primitive, adapters build on it" precedent).
- **D9 — `TestHermeticImports` is the D3 enforcement.** Unlike sibling
  `infra/broker`/`infra/kvstore` (which import amqp/go-redis and ship
  NO hermetic test), `infra/concurrency` is **hermetic** (stdlib-only)
  and ships `TestHermeticImports` with an **EMPTY** first-party
  allowlist (it needs neither `model` nor `config`) that active-fails
  on ALL third-party (notably `github.com/prometheus/...`) + the listed
  internals. Recorded so this test is neither "consistency-deleted" to
  match broker/kvstore nor weakened to admit prometheus.
- **No `sync` import.** The buffered channel is the sole synchronization
  primitive (golang-pro §5/§6). `Semaphore` has exactly two fields
  (`slots chan struct{}`, `gauge Gauge`); no redundant `max` field
  (`cap(slots)` recovers it for tests). Immutable after `New`; safe for
  concurrent use by many jobs (`-race`, `TestConcurrentAcquireRelease_-
  RaceClean`, bounded-holders invariant).
- **gofmt self-check** via `go/format` (sandbox blocks `go fmt`) — the
  aggregator/schemavalidator precedent.

## Forward notes (recorded, owners elsewhere)

1. **LIC-TASK-036 (Pipeline Orchestrator) — job-level wiring.**
   Construct at the consumer/broker level (§911):
   `concurrency.New(cfg.Pipeline.Concurrency,
   concurrency.WithGauge(metrics.Pipeline.ConcurrentJobs))`.
   `cfg.Pipeline.Concurrency` is fail-fast-validated `>=1`, so D6's
   panic is unreachable from valid config (it guards wiring bugs only).
   Pair every `Acquire(ctx)==nil` with **exactly one**
   `defer sem.Release()` on the job goroutine — a missing/double Release
   **panics** (D5) by design; treat such a panic as an Orchestrator
   job-lifecycle bug. `Acquire` returns raw `context.Canceled`/
   `DeadlineExceeded` on shutdown/job-timeout — branch via `errors.Is`,
   not a `model.DomainError`.
2. **LIC-TASK-016/017/047 (Provider Router) — LLM-level per-provider
   wiring.** The Router owns a `map[providerID]*concurrency.Semaphore`,
   each `concurrency.New(cfg.LLM.ConcurrencyPerProvider)` — **no
   `WithGauge`** (no SSOT LLM-level concurrency gauge; do not invent
   one without an `observability.md` amendment). Distinct from the
   per-provider **Redis token-bucket rate limiter** (LIC-TASK-017,
   `llm-provider-abstraction.md:285` "denied → backpressure, NOT
   fallback") — different component, different package (`kvstore.Eval`);
   do not conflate. If an LLM-level gauge is later approved, add it via
   the existing `WithGauge` option — no signature change.
