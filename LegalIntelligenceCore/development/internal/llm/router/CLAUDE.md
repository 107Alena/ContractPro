# router Package — CLAUDE.md

LIC-internal LLM **Provider Router** (LIC-TASK-019,
`llm-provider-abstraction.md` §2, `error-handling.md` §4–§6). Implements
`port.ProviderRouterPort`: per-agent primary + global fallback chain,
in-memory healthy registry, background health-check goroutine, sticky
repair. Called by the BaseAgent runner (LIC-TASK-024).

Constructor: `NewProviderRouter(providers, cfg RouterConfig, deps Deps)
(*ProviderRouter, error)` (stutter-free per the codebase-wide `NewTypeName`
convention / `feedback_constructors.md`; fail-fast like `NewLimiter` /
`NewTracker` / `kvstore.NewClient`).

## Files

- **seams.go** — package doc + the three consumer-side seams
  (`RateLimiter`, `UsageTracker`, `Metrics`) each with a zero-dependency
  noop default, `CallOutcome`/`HealthState` local mirrors, `Deps`,
  ctx-aware `sleepCtx`.
- **config.go** — `RouterConfig` + health-loop interval/timeout defaults.
- **registry.go** — `healthRegistry`: the SINGLE owner of every health
  state transition (`recordSuccess`/`recordFailure`), `isHealthy`,
  `shouldProbe`.
- **router.go** — `ProviderRouter`, `NewProviderRouter` (fail-fast
  validation), `Complete`, chain build/dedup, `attempt` (one same-provider
  retry), `backoffFor`.
- **repair.go** — `CompleteRepair` (sticky, single shot).
- **health.go** — `Start`/`Stop` lifecycle, `healthLoop`,
  `runHealthChecks`.

## Conventions & deliberate decisions

- **Hermetic.** Imports only stdlib + `internal/domain/{port,model}` —
  the strictest hermeticity, exactly like every `internal/llm/*` sibling.
  The rate limiter, cost tracker and Prometheus vecs are inverted behind
  the `RateLimiter`/`UsageTracker`/`Metrics` seams; the
  `var _ Seam = (*ratelimit.Limiter|*cost.Tracker|...)(nil)` assertions
  and the concrete adapters are app-wiring's job (LIC-TASK-047), NOT here
  — adding a ratelimit/cost/prometheus/tracer import would break the
  invariant (mirrors `ratelimit`'s `Observer` / `cost`'s `Recorder`).
- **Branch on the error FLAGS, never per-code (code-architect D2).** The
  Router reads `port.AsLLMProviderError` then branches purely on
  `.Retryable` / `.FallbackEligible` — the `port.llmCodeCatalog` SSOT. It
  never re-derives per-code policy.
- **Doc discrepancy (recorded).** `error-handling.md` §6.1 lists
  `INVALID_API_KEY` / `QUOTA_EXCEEDED` / `CONTENT_POLICY` as "fatal, не
  fallback", but the §1.2 matrix + `llmCodeCatalog` + the LIC-TASK-019
  acceptance text ("fail при ErrCodeContextTooLong/MalformedRequest"
  only) make those `FallbackEligible=true`. The Router follows the flag
  (catalog is SSOT); §6.1 is a lossy summary. Same handling pattern as
  `ratelimit`'s `LIC_LLM_RPS_*` discrepancy.
- **`errors.As` everywhere, never `err.(*LLMProviderError)`
  (code-architect MF-1).** A missing classification (adapter invariant
  breach) degrades to "fail this provider, no fallback, no retry" with a
  fixed `UNKNOWN` failed-metric code (bounded cardinality, surfaces the
  breach instead of dropping it). Any **ctx-derived** rate-limiter `Wait`
  error aborts the WHOLE chain (returns RATE_LIMIT wrapping `ctx.Err()`);
  it must never burn the rest of the chain and mask a cancellation as
  `ALL_PROVIDERS_FAILED`. A non-ctx `Wait` error (unconfigured-provider
  `MALFORMED`) skips just that provider, emitting `ProviderFailed` +
  `ObserveCall(fail)` (keeps the MF-5 "every failed attempt → fail"
  invariant / `LICAllProvidersFailing` alert correct, code-reviewer
  MEDIUM-2) but DELIBERATELY NOT `recordFailure` — a missing rate-limit
  bucket is a LIC wiring bug, not provider ill-health, so it must not
  poison the health registry. This is the one intentional `recordFailure`
  exclusion.
- **Non-fallback fatal codes don't degrade health (code-reviewer
  LOW-1).** `recordFailure` treats a non-retryable & non-fallback
  `*LLMProviderError` (`CONTEXT_TOO_LONG` / `MALFORMED_REQUEST`) as a
  no-op for health state: §2.3 classifies only Retryable / transport as
  transient, and a recurring LIC request-builder defect must surface as
  itself, not as a falsely-unhealthy provider diverting traffic.
- **`ALL_PROVIDERS_FAILED` wraps the genuine root cause (code-reviewer
  MEDIUM-1).** An unhealthy-skip does NOT overwrite `lastErr` (matches
  the §2.1/§6 "skip + metric" pseudocode); the wrapped error points at
  the real prior provider failure, not a synthetic skip marker (only
  seeded if the very first chain entry is skipped).
- **Side-effects never alter control flow (code-architect MF-2).** Per
  failed provider iteration the order is fixed: (1) `failed_total`
  metric, (2) registry side-effect (auth→permanent, quota→permanent-24h,
  retryable→consecutive++), (3) flag-driven retry/fallback/fatal
  decision. The registry write after an auth/quota failure does NOT
  short-circuit the `FallbackEligible`-true advance to the next provider.
- **One shared state-transition function (code-architect MF-3).** Both a
  `Complete` failure and a background `HealthCheck` failure funnel
  through `healthRegistry.recordFailure`, so the two producers cannot
  define divergent "unhealthy" semantics. `skipped_unhealthy_total` is
  incremented ONLY at the top-of-iteration registry check, never as a
  consequence of a failure handled later in the same iteration. The
  `health_status` gauge is emitted only on an actual state change (a
  steady-state healthy provider does not re-emit every 30s).
- **Same-provider retry is the Router's job, once
  (`error-handling.md` §4.1).** The adapters deliberately do NOT retry
  (see `claude/provider.go` godoc). On a `Retryable` typed error
  `attempt` does exactly ONE same-provider retry after the §4.3 backoff
  (200ms 5xx/network/timeout, `RetryAfter`|1s 429, 500ms 529). Metrics /
  registry / cost are recorded ONCE per provider chain-iteration on the
  terminal error — the retry is an internal HTTP-resilience detail and
  must not double-count.
- **Backoff is ctx-aware (code-architect D4).** `sleepCtx` uses a
  single-shot `time.Timer` + `select{ctx.Done()|timer.C}` (Go 1.23+
  Stop, go.mod 1.26.1 → no manual drain); a ctx that dies during backoff
  short-circuits the retry so `Complete`'s MF-1 ctx guard aborts the
  chain. Injectable via `Deps.sleep` for fast deterministic tests.
- **`CompleteRepair` is sticky + single shot (OQ-10 / §2.1 / §5.4).** No
  same-provider retry, no fallback. An unhealthy sticky provider →
  immediate escalation built as a **LITERAL**
  `&port.LLMProviderError{Code: LLMErrorServerError, Retryable:false,
  FallbackEligible:false}` — NOT `NewLLMProviderError`, whose
  `SERVER_ERROR` catalog row is `{retryable:true, fallbackEligible:true}`,
  the opposite of the sticky-escalation semantics (code-architect MF-4;
  the documented `llm_errors.go` literal-override path).
- **Cost telemetry split (code-architect MF-5).** `Complete` success →
  `UsageTracker.ObserveSuccess` (the 5 usage families +
  `lic_llm_calls_total{success}` — omitting it would undercount
  calls_total by the entire success volume). Every failed provider
  attempt in `Complete` → `ObserveCall(..., OutcomeFail)` so the
  `LICAllProvidersFailing` alert (keys on `outcome="fail"`) is correct.
  Every `CompleteRepair` invocation → `ObserveCall(..., OutcomeRepair)`
  recorded ONCE at the top of the method (so the unregistered-provider
  and unhealthy-escalation pre-wire returns count too) regardless of
  sub-outcome — the authoritative rule is the `metrics/llm.go`
  `CallsTotal` godoc ("`repair` increments on every CompleteRepair
  invocation"; golang-pro N-1). Repair therefore never calls
  `ObserveSuccess`; the resulting minor cost undercount of a *successful
  repair's* tokens is a **pre-existing, documented property of the cost
  package** (LIC-TASK-018, `cost.go` `ObserveCall` models repair as
  non-usage), not introduced here. `CallOutcome` is a local mirror of
  `cost.Outcome`; `seams_test.go` pins the four wire strings so the
  047 adapter's `cost.Outcome(routerOutcome)` cannot mislabel.
- **Immutable except the mutex-guarded registry.** `providers`/`cfg`
  maps are read-only; the parallel errgroup agent pipeline shares one
  `*ProviderRouter` without locking the hot path
  (`TestComplete_ConcurrentRaceClean`, `-race`).
- **Background loop lifecycle.** `Start(ctx)` is NOT called by the
  constructor (app-wiring owns the lifecycle, tests drive
  `runHealthChecks` directly). `Start`/`Stop` are idempotent
  (`sync.Once`); `Stop` blocks on a `WaitGroup` until the goroutine has
  fully exited (no leak under `-race`). Probes are sequential, each
  bounded by `HealthCheckTimeout`.

## Out of scope for LIC-TASK-019 (forward concerns)

- **Circuit breaker (gobreaker, §3.3, `lic_llm_provider_circuit_state`).**
  Not in the acceptance criteria; no gobreaker in `go.mod`; the health
  registry's unhealthy/permanent skip already delivers the cascade
  protection the task contracts for. `lic_llm_provider_circuit_state`
  exists centrally in `metrics/llm.go` (the org-wide SSOT) but is owned
  by a future task, exactly as `RateLimitedTotal` is owned by
  LIC-TASK-017. Confirmed OUT by code-architect (D8).
- **Cost-USD → OTel span attribution.** The Router has no `tracer`
  dependency and does not own the `lic.llm` span; it calls the cost
  tracker for metrics only. `ObserveSuccess` is the success-path call;
  the per-span `lic.llm.cost_usd` attribution is a forward note for the
  span owner (LIC-TASK-036 / the agent task). Confirmed OUT by
  code-architect (D8).

## Forward requirements for LIC-TASK-047 (app-wiring)

1. Implement the three seam adapters:
   - `RateLimiter` → `*ratelimit.Limiter.Wait` (direct;
     `var _ RateLimiter = (*ratelimit.Limiter)(nil)` lives in wiring).
   - `UsageTracker` → over `*cost.Tracker`: `ObserveSuccess` builds
     `cost.Usage` with `Latency = time.Duration(resp.LatencyMs)*
     time.Millisecond` (off-by-1000 hazard, `cost/CLAUDE.md` #3) and
     forwards `resp` token counts/model; `ObserveCall` →
     `tracker.ObserveCall(prov, mdl, agent, cost.Outcome(outcome))`.
   - `Metrics` → over `*metrics.LLMMetrics`: `ProviderFallback`→
     `ProviderFallbackTotal`, `ProviderSkippedUnhealthy`→
     `ProviderSkippedUnhealthyTotal`, `ProviderFailed`→
     `ProviderFailedTotal`, `ProviderHealthState`→ set
     `ProviderHealthStatus{provider,state}` gauge to 1 for `state` and 0
     for the other two states of that provider.
2. Build `RouterConfig` from `config`: `AgentPrimary` from
   `cfg.Agents.Providers` (string→`model.AgentID`/`port.LLMProviderID`),
   `FallbackOrder` from `cfg.LLM.ProviderFallbackOrder`,
   `HealthCheckInterval` 30s prod / relax to 60s on staging per env.
3. Call `Start(ctx)` after wiring and `Stop()` first in graceful
   shutdown (before the rate limiter / metrics registry close).
