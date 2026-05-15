# ratelimit Package — CLAUDE.md

Per-provider token-bucket rate limiter for the Legal Intelligence Core
(LIC-TASK-017, `llm-provider-abstraction.md` §3.1–§3.2). The Provider Router
(LIC-TASK-019) calls `Limiter.Wait(ctx, providerID)` as its
`r.rateLimit(ctx, providerID)` hook before every wire call.

Constructors: `NewLimiter(cfg Config, eval LuaEvaluator, obs Observer)`,
`NewTokenBucket(provider, rps, burst, eval)` (stutter-free per the
codebase-wide `NewTypeName` convention / `feedback_constructors.md`).

## Files

- **ratelimit.go** — `LuaEvaluator` + `Observer` seams (+ `noopObserver`
  default), `Config`/`ProviderLimit`, `Limiter`, `NewLimiter`, `Wait`,
  `computeSleep`, ctx clamping.
- **bucket.go** — `TokenBucket`, `outcome` taxonomy, `allow` (one
  non-blocking script run + classification).
- **script.go** — `tokenBucketScript` (Lua), `computeBucket` (the Go SSOT for
  the bucket math, mirrored by the script), defensive `decodeResult`,
  fixed-point `microScale`.

## Conventions & deliberate decisions

- **Hermetic.** Imports only stdlib + `internal/domain/port`, exactly like
  every `internal/llm/*` sibling (claude/openai/gemini). Redis is inverted
  behind `LuaEvaluator` (satisfied by `*kvstore.Client.Eval`); the
  `var _ LuaEvaluator = (*kvstore.Client)(nil)` assertion and the
  Prometheus/logger `Observer` adapter belong to app-wiring (LIC-TASK-047),
  NOT here — adding a `kvstore`/`prometheus` import would break hermeticity
  (code-architect MF-3 / note 2).
- **Seam naming.** `LuaEvaluator`, not `ScriptRunner` — the latter shadows
  `redis.Scripter` conceptually (code-architect MF-3).
- **One `Observer` seam, three signals.** `RateLimited` (denied →
  `lic_llm_rate_limited_total{provider}`, strictly denied-only),
  `FailOpen` (Redis Eval failed → sampled WARN), `ScriptAnomaly` (nil/garbage
  reply → distinct counter). Grouped into one seam so wiring injects one
  adapter and tests need one fake; sampling policy is the adapter's job
  (code-architect note 3 — choice recorded here).
- **Key `lic:rate:{provider}` WITHOUT shard.** §3.1's
  `lic:rate:{provider}:{shard}` (shard=`org_id_hash%4`) is explicitly
  "(опц.)"; the acceptance criteria freeze the no-shard key; a single global
  per-provider bucket tracks the provider's aggregate quota more accurately
  than a 4× split; keeping (even hashed) `org_id` out of the key is 152-ФЗ
  PII minimisation (code-architect OQ-F).
- **Fail-OPEN on infra failure, by design.** An Eval transport error (Redis
  down/ctx) → allow the call (`Wait` returns nil) + `Observer.FailOpen`. The
  limiter must never be a single point of failure for the whole LLM pipeline;
  brief provider over-call beats a full analysis outage (`error-handling.md`
  graceful degradation; code-architect OQ-A).
- **Anomaly ≠ outage.** The script is contractually *never nil*. A
  nil/malformed reply is a script/data bug, not "Redis down": still fail-open,
  but via the **distinct** `Observer.ScriptAnomaly` signal so dashboards never
  conflate it with infra outages, and it never touches
  `lic_llm_rate_limited_total` (code-architect OQ-A). NOTE: `kvstore.Client.Eval`
  also maps a transport `redis.Nil` to `(nil, nil)`, so a rare transport
  `redis.Nil` is indistinguishable from a script-nil and will also surface as
  `ScriptAnomaly` — both are correctly fail-open; an on-call should not assume
  `ScriptAnomaly` always means a Lua bug (code-reviewer M1).
- **ctx-expiry race vs fail-open.** ctx can elapse between the loop-top
  `ctx.Err()` check and the Eval round-trip; a real Redis client then returns
  a transport error. `Wait` re-checks `ctx.Err()` on the infra-error path and
  returns RATE_LIMIT (wrapping ctx.Err()) instead of fail-open, so an elapsed
  deadline is never silently masked as success (code-reviewer H1).
- **ctx-expiry error shape.** `Wait` returns
  `port.NewLLMProviderError(port.LLMErrorRateLimit, ctx.Err())`. This single
  value satisfies BOTH §2.1 router branches: `errors.Is(_,
  context.DeadlineExceeded)` is true via `LLMProviderError.Unwrap` (deadline
  branch → router builds its own RATE_LIMIT error), and it is a
  `*LLMProviderError` so the non-deadline (`context.Canceled`)
  `err.(*LLMProviderError)` assertion is panic-safe. `ctx.Err()` (not the
  `context.DeadlineExceeded` constant) is wrapped so `Canceled` stays
  `Canceled` (code-architect OQ-C).
- **Unknown provider → MALFORMED_REQUEST.** `port.NewLLMProviderError(
  port.LLMErrorMalformedRequest, …)` → not retryable, not fallback-eligible
  (a config bug); a `*LLMProviderError` so the router assertion is panic-safe;
  never a panic in a stateless shared-goroutine service (code-architect OQ-B).
- **Sleep policy (no exponential backoff).** Denied → sleep =
  full-jitter within `[minSleep, base*1.2]`, `minSleep =
  max(1ms, 1/(2·rps))` (anti busy-spin), `base` floored at minSleep and
  **capped at `maxSleep`=2s** so a low-RPS misconfig can't park a goroutine
  in one opaque sleep (code-architect MF-5), then clamped to ctx deadline.
  Exponential `200ms*2^attempt` between providers is the Router's §3.2
  concern, deliberately NOT here (code-architect OQ-D). Reusable
  `time.Timer` (Go 1.23+ Reset/Stop ⇒ no manual drain; go.mod `go 1.26.1`),
  lazily created so the allowed-immediately hot path allocates nothing.
- **Lossless integer micro-tokens.** Token balances persist as `tokens × 1e6`
  integers; a float `HSET` round-trips lossily. `computeBucket` mirrors the
  Lua `math.floor` before persisting (code-reviewer B1 / code-architect OQ-E).
- **`redis.replicate_commands()` first.** The script reads `redis.TIME` (the
  only multi-instance-correct clock — several LIC pods share one bucket;
  passing Go time would skew across pods), making it non-deterministic.
  `redis.replicate_commands()` is the mandatory first statement (no-op on
  effects-replication, required on Redis < 5 / script-replication).
  **Minimum supported Redis: 5.0** (code-architect MF-4 / OQ-E).
- **Cold key never refills from ts=0.** A missing key initialises
  `tokens=burst×1e6, ts=now_us` and decrements immediately; computing
  `elapsed = now_us - 0` would overflow Lua double precision (code-architect
  OQ-E).

## Doc discrepancy (for LIC-TASK-047)

Architecture §3.2 and the LIC-TASK-017 acceptance text spell the params
`LIC_LLM_RPS_<PROVIDER>` / `LIC_LLM_BURST_<PROVIDER>`. Those env vars **do not
exist**. The implemented SSOT (`configuration.md` §2, `config/llm.go`) is
`LIC_<PROVIDER>_RPS` / `LIC_<PROVIDER>_BURST`. This package reads no env;
LIC-TASK-047 MUST map `Config.Providers[id] = {cfg.LLM.<Provider>.RPS,
cfg.LLM.<Provider>.Burst}`. Recorded in `progress.md`.

## Test strategy (deliberate, intent-preserving deviation)

`miniredis` is absent from the offline module cache and the network is
unavailable, and no Lua VM is available offline — identical to the constraint
LIC-TASK-007 shipped under. The true `redis.Script.Run` EVALSHA→NOSCRIPT→EVAL
dispatch contract is already proven by LIC-TASK-007's kvstore tests; here we
verify token-bucket **behaviour**:

- `fakeEvaluator` — a faithful in-memory `LuaEvaluator` that executes the
  exact bucket semantics via the shared `computeBucket` (so Lua and Go cannot
  drift), driven by an **injectable virtual microsecond clock** so the
  RPS-sustain / burst / refill tests are deterministic and `-race` clean
  (code-architect MF-1/MF-2). Programmable error & anomaly injection covers
  fail-open and script-anomaly paths.
- `script_test.go` — pins the Lua source structural invariants
  (`redis.replicate_commands()` first, `redis.TIME`, HSET on both paths,
  EXPIRE, `return {…}` never nil) and exercises `computeBucket`/`decodeResult`
  directly.
- `script_integration_test.go` (`//go:build redis_integration`, off by
  default; needs `LIC_TEST_REDIS_URL`) — runs the **real** Lua against a live
  Redis and asserts it agrees with `computeBucket`, closing the Lua/Go drift
  gap when an integration Redis is available.
