# kvstore Package — CLAUDE.md

Redis adapter for the Legal Intelligence Core (go-redis/v9). Pure
infrastructure: implements **no domain port** (LIC has none for the KV store,
mirroring `internal/infra/broker` and DocumentProcessing). Higher-level
adapters build on these primitives — the Idempotency Guard (LIC-TASK-038,
`port.IdempotencyStorePort`), the Pending Type Confirmation store
(LIC-TASK-037, `port.PendingStatePort`) and the per-provider token-bucket
Rate Limiter (LIC-TASK-017, via `Eval`).

Constructor: `NewClient(cfg config.RedisConfig) (*Client, error)`.

## Files

- **client.go** — `Client`, the injectable `RedisAPI` seam (mock seam),
  `NewClient` (dials + Pings, fail-fast), `newClientWithRedis` (test seam),
  `Ping`, idempotent graceful `Close`, `isClosed`.
- **options.go** — `buildOptions`: `redis.ParseURL` + explicit `LIC_REDIS_*`
  overrides + TLS hardening.
- **ops.go** — `Get`, `Set`, `SetNX`, `Delete`, `Expire`, `Eval`
  (+ `scriptFor` EVALSHA cache).
- **errors.go** — `ErrKeyNotFound` (plain sentinel), `RedisError{Op,Retryable,
  Cause}`, `IsRetryable`, `mapError`, `errClientClosed`,
  `redactURLCredentials`.

## Conventions & deliberate decisions

- **`NewClient`, not `NewKVStoreClient`.** Stutter-free per Effective Go;
  matches the `broker.NewClient` / `internal/llm/*` siblings and the DP
  kvstore. This intentionally diverges from the stale DP `infra/CLAUDE.md`
  `NewKVStoreClient` wording (recorded so a future "consistency" change does
  not reintroduce the stutter).
- **Error model:** package-local typed `RedisError`, never a
  `model.ErrorCode`. `model.errorCatalog` has an `init()` SSOT panic and
  Redis/infra errors are never Orchestrator-published. `context` errors pass
  through raw (codebase-wide convention). `redis.Nil` → `ErrKeyNotFound`,
  `redis.ErrClosed` → non-retryable, everything else retryable by default.
  The DP kvstore maps to `port.ErrCodeStorageFailed`; LIC has no storage
  domain port and that mapping is deliberately **not** copied
  (code-architect Q1).
- **`ErrKeyNotFound` is a plain `errors.New` sentinel, not a `RedisError`.**
  The LIC-TASK-037/038 adapters translate a miss into `IdempotencyAbsent`
  (no error) / `ErrPendingStateNotFound`; both need a clean
  `errors.Is(err, kvstore.ErrKeyNotFound)`.
- **No wrapper structs.** Unlike the broker (whose `amqp.Connection.Channel()`
  returns a concrete `*amqp.Channel`, forcing wrapper types), `*redis.Client`
  satisfies the methods-subset `RedisAPI` directly
  (`var _ RedisAPI = (*redis.Client)(nil)`). The asymmetry vs. the broker is
  deliberate — do not add pointless wrappers for "consistency"
  (code-architect Q2).
- **TLS SSOT.** The TLS decision lives in `config.RedisConfig.UsesTLS()`
  (exported by this task, also used by `config.enforceTLS`), so the rule
  cannot drift between the production TLS-everywhere enforcement and this
  adapter. kvstore only **honours** the decision, never re-enforces it
  (`config.enforceTLS` fails startup first). TLS is hardened to
  `MinVersion: TLS1.2` + `ServerName` pinned to the dialled host;
  `InsecureSkipVerify` is never set (code-architect Q4).
- **Read/WriteTimeout reuse `DialTimeout`.** `configuration.md` §2.3 freezes
  the Redis var set (no `LIC_REDIS_READ/WRITE_TIMEOUT`); inventing env vars is
  out of scope. Mirrors the DP kvstore precedent. `PoolTimeout` left at the
  go-redis default (code-architect must-fix 2).
- **`Ping` honours `ctx`** with an early `ctx.Err()` return. go-redis aborts
  the pooled dial/read on ctx cancellation, so the broker's off-goroutine
  half-open-TCP workaround is intentionally **not** needed here — do not copy
  it from the broker (code-architect must-fix 3).
- **Credential redaction.** `redactURLCredentials` strips the
  `LIC_REDIS_URL` password from dial/ParseURL/Ping error strings (152-ФЗ PII,
  security bar set by `broker.redactURLCredentials`, MF-1). Deliberately
  duplicated from the broker — no shared infra util package, small pure
  function (code-architect must-fix 5).
- **`Eval` script cache.** `scriptFor` caches one `*redis.Script` per source
  in a `sync.Map`; `redis.Script.Run` does EVALSHA→NOSCRIPT→EVAL. The
  token bucket runs per LLM call, so rebuilding the wrapper (and SHA1) each
  call is avoided (code-architect Q3). `ScriptLoad`/`EvalSha` are not exposed
  — `Eval` is the single generic primitive (YAGNI).
- **§6.3/§6.10 op coverage:** `SetNX` (idempotency PROCESSING),
  `Get`/`Set …EX` (status, pending blob), `Expire` (heartbeat — `false`
  return = key vanished, stop the heartbeat goroutine), `Delete` (cleanup,
  variadic), `Eval` (token bucket).

## Test strategy (deliberate, intent-preserving deviation)

`miniredis` is **absent from the offline module cache** and the network is
unavailable, so — exactly as `internal/infra/broker` shipped with in-memory
AMQP fakes instead of a live broker — kvstore is tested against:

- `fakeRedis` — faithful in-memory store (correct
  GET/SET/SETNX/DEL/EXPIRE/TTL/Ping, lazy expiry) + a recording script seam.
- `mockRedis` — fully programmable double (return/error injection, call
  recording) for error mapping, context passthrough, use-after-close, Ping.

True Lua bytecode execution is impossible offline; the `Eval` tests assert the
**dispatch contract** — the real `redis.Script.Run` EVALSHA→NOSCRIPT→EVAL
fallback path, exact script source / KEYS / ARGV passthrough, result decoding,
and the per-source script-handle cache. Token-bucket *behaviour* is verified
in LIC-TASK-017's adapter. Tests are `-race` clean.
