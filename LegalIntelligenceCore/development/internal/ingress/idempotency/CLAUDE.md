# idempotency Package — CLAUDE.md

**Idempotency Guard** (LIC-TASK-038, `high-architecture.md` §6.3/§6.5/§6.10,
`configuration.md` §2.4, `observability.md` §3.6, `error-handling.md` §3). The
Redis-backed infrastructure adapter for `port.IdempotencyStorePort` — the
exact analog of `internal/ingress/consumer` being the broker adapter: an
**adapter layer** (not an `internal/application/*` hermetic core), so it MAY
import `internal/infra/kvstore` for the primitive ONLY (behind the local
`RedisSeam`), and is itself hermetic against
prometheus/otel/redis/config/logger/application:

```
SetNX(absent)          → atomic Lua Eval (D4) → (Absent, nil)        + Lookup{new}
SetNX(present)         → (parsed status, port.ErrIdempotencyKeyExists)
                         + Lookup{completed|in_progress}
SetNX(transport-down)  → (Absent, kvstoreErrVerbatim) ALWAYS         (R1)
CheckAndAcquire(absent)  → (Absent, false, nil)                      + Lookup{new}
CheckAndAcquire(present) → (status, true, nil)  [NO ErrIdempotencyKeyExists — D3.2]
CheckAndAcquire(down)    → FallbackEnabled ? (Absent,false,nil)+fallback_db+
                           Fallback()+ERROR : (Absent,false,errVerbatim)+WARN (R1)
Get(miss)   → (Absent, nil)   Get(present) → (parseStatus, nil)   Get(err) → (Absent, errVerbatim) (R5)
ExtendTTL   → Expire true⇒nil / false⇒ErrIdempotencyKeyVanished / err⇒verbatim (D6.1)
SetCompleted/SetPaused → unconditional Set(COMPLETED|PAUSED, per-call ttl) (D12)
StartHeartbeat → injected-ticker goroutine; stop on {ctx.Done, stop(), vanished} (D6)
```

One exported type `*Guard`. Constructor `NewGuard(RedisSeam, Config, Deps)
(*Guard, error)` — `NewTypeName` (`feedback_constructors.md`), fail-fast on
nil `redis` / invalid `Config` via `errors.Join` (the `consumer.NewConsumer` /
`pendingconfirmation.NewManager` precedent). Immutable after construction; all
7 methods are goroutine-safe for distinct keys (no per-instance mutable state;
each `StartHeartbeat` owns its own ticker + stopCh + goroutine). One atomic
Lua via `kvstore.Eval` does SET-NX-EX-or-return-existing — zero TOCTOU,
exactly ONE `Eval` per `SetNX`/`CheckAndAcquire` on the normal path.

## Files

- **guard.go** — package doc (hermetic statement; D1..D14 attribution),
  `parseStatus` (D5 — unknown/garbage non-empty ⇒ `IdempotencyProcessing`,
  never `Absent`), `Guard` (D13) + the in-package
  `var _ port.IdempotencyStorePort = (*Guard)(nil)` (D13), `NewGuard` (D2 —
  `errors.Join` fail-fast), the 5 frozen port methods + `CheckAndAcquire`
  (D3), `classifyLookup` (D8), the metric/fallback policy core (D7/D8/R1).
- **script.go** — `luaSetNXOrGet` (D4 exact source), `errEvalShape` (D4 —
  plain `errors.New`), `luaArgs` (KEYS/ARGV; integer-seconds ttl, min 1 —
  R3, never hardcoded), `decodeEvalResult` (D4 — `{1,""}`⇒acquired,
  `{0,v}`⇒present, else⇒`errEvalShape`, `{0,nil|""}`⇒needsRetry),
  `evalSetNXOrGet` (the D4.1 bounded single-retry helper — exactly 2
  attempts max, never unbounded).
- **heartbeat.go** — `ErrIdempotencyKeyVanished` sentinel (D6.1 — plain
  `errors.New`), `StartHeartbeat` (D6 — injectable ticker, 3 stop
  conditions, `sync.Once` idempotent stop, transient⇒WARN+continue,
  no leak, no metric).
- **seams.go** — `RedisSeam` (6 methods, no noop — required positional),
  the 4 lookup-result constants (D8), `Metrics`+`noopMetrics`,
  `Ticker`/`Clock`+`realTicker`+`systemClock`, `Logger`+`noopLogger`
  (Warn/Error only — no Info). `var _` after each noop pair; the
  `RedisSeam`-satisfaction assertion is the LIC-TASK-047 wiring package's.
- **deps.go** — `Deps{Metrics, Clock, Logger}` (optional-with-noop) +
  `withDefaults()`. `RedisSeam`/`Config` are positional `NewGuard` params.
- **config.go** — local `Config{HeartbeatInterval, FallbackEnabled}` +
  `validate()` (D9 — only `HeartbeatInterval>0`; NO TTL fields — R3).
- **internal_test.go** — `TestHermeticImports` (2-entry
  `{domain/port, infra/kvstore}` allowlist, ZERO third-party, active-fail
  forbidden set incl. go-redis/config/model/observability/application*/
  ingress{consumer,router}/prometheus/otel/miniredis) + `TestGofmtClean`
  (`go/format`; the sandbox blocks `go fmt`) — D10/D14.
- **guard_test.go** — the full behavioural suite (PART C #3,#4,#6–#19)
  with an in-package faithful `fakeRedis` (D11), manually-driven `Ticker`,
  fake `Metrics`/`Logger`/`Clock`; `-race` clean, ZERO `time.Sleep`.
- **CLAUDE.md** — this file.

## API

- `NewGuard(RedisSeam, Config, Deps) (*Guard, error)`.
- `(*Guard) SetNX(ctx,key,ttl) (port.IdempotencyStatus, error)` — FROZEN
  port; present ⇒ `port.ErrIdempotencyKeyExists`.
- `(*Guard) Get(ctx,key) (port.IdempotencyStatus, error)` — FROZEN; miss ⇒
  `(Absent, nil)`.
- `(*Guard) ExtendTTL(ctx,key,ttl) error` — FROZEN; vanished ⇒
  `ErrIdempotencyKeyVanished`.
- `(*Guard) SetCompleted(ctx,key,ttl) error` /
  `(*Guard) SetPaused(ctx,key,ttl) error` — FROZEN; unconditional Set.
- `(*Guard) CheckAndAcquire(ctx,key,ttl) (status, alreadyExists, err)` —
  additive 040 surface; present ⇒ `err==nil` (NOT
  `ErrIdempotencyKeyExists`); consults `FallbackEnabled`.
- `(*Guard) StartHeartbeat(ctx,key,ttl) (stop func())` — additive;
  goroutine driving `ExtendTTL` every `Config.HeartbeatInterval`.
- `Config{HeartbeatInterval, FallbackEnabled}`; `Deps{Metrics, Clock,
  Logger}` — every Deps field optional (nil ⇒ noop).
- `RedisSeam` / `Metrics` / `Clock` / `Ticker` / `Logger` — the
  adapter-local seams.

## Reconciliations (build-spec PART B — DEFECT-style, condensed)

**R1 — Fallback: two surfaces, two contracts.** `configuration.md:65`
("ack без проверки + alert") vs the existing `pendingconfirmation.Manager`
`SetNX` consumer ("retryable NACK") cannot share one Redis-down behaviour.
**Frozen `SetNX`/`Get`/`ExtendTTL`/`SetCompleted`/`SetPaused` on Redis-down
⇒ ALWAYS the kvstore/local error verbatim, NEVER a `model.ErrorCode`, NEVER
wrapping `ErrIdempotencyKeyExists`, NEVER consulting `FallbackEnabled`** —
preserves `pendingconfirmation` (`manager.go:343-349` maps it to
`IDEMPOTENCY_STORE_UNAVAILABLE` retryable NACK). **Ergonomic
`CheckAndAcquire` consults `FallbackEnabled`**: true ⇒
`(Absent,false,nil)`+`Lookup{fallback_db}`+`Fallback()`+ERROR log (the
"alert"); false ⇒ `(Absent,false,errVerbatim)`+WARN log. Context errors
pass through RAW (the codebase-wide `kvstore/errors.go:78-81` convention).

**R2 — Task names vs frozen port.** tasks.json says
`CheckAndAcquire(ctx,key)`/`StartHeartbeat(ctx,key)`/`SetCompleted(key)`/
`SetPaused(key)` (no ttl). The FROZEN, already-consumed
`port.IdempotencyStorePort` (`idempotency.go:42-74`) names them `SetNX`,
`ExtendTTL`, `SetCompleted(ctx,key,ttl)`, `SetPaused(ctx,key,ttl)`. **The
frozen port is the SSOT** ("task acceptance is a non-exhaustive checklist,
frozen contracts win" — `pendingconfirmation` R5). The 5 frozen signatures
are byte-for-byte; `CheckAndAcquire`/`StartheartBeat` are ADDITIVE and GAIN
the per-call `ttl` the task wording omits (`SetNX` requires it; R3).
`StartHeartbeat` is the goroutine driver, `ExtendTTL` the per-tick
primitive it calls — a layering, not a conflict.

**R3 — No hardcoded TTL; the §8.2:1020 "90s" is stale.** The SSOT
PROCESSING TTL is 150s (`:563`, `configuration.md:63`,
`config/idempotency.go:23`), not the stale pre-heartbeat §8.2:1020 "90s".
**Moot for the adapter:** the Guard hardcodes NO TTL — every TTL
(PROCESSING/COMPLETED/PAUSED/user-confirmed) is a per-call method
parameter; the only intrinsic time knob is `Config.HeartbeatInterval`. The
stale figure cannot reach the adapter because the adapter never names a TTL
constant. (`high-architecture.md:1020` correction is architecture-team-
owned, out of scope.)

**R4 — Adapter returns kvstore/local-typed errors, NEVER a
`model.ErrorCode`.** The Guard returns: `nil`; the frozen
`port.ErrIdempotencyKeyExists` (`SetNX` present-key only); the
package-local `ErrIdempotencyKeyVanished` (D6.1) / `errEvalShape` (D4); or
the kvstore error verbatim (`*kvstore.RedisError`, `kvstore.ErrKeyNotFound`,
raw context errors). The Manager/040 owns the
`model.ErrCodeIdempotencyStoreUnavail` mapping — the Guard MUST NOT import
`internal/domain/model`. The kvstore/broker discipline verbatim
(`kvstore/errors.go:13-25`; the DP `port.ErrCodeStorageFailed` mapping is
deliberately NOT copied).

**R5 — `Get` miss ⇒ `IdempotencyAbsent` with NO error.**
`idempotency.go:50-53` frozen godoc + `kvstore.Get`⇒`("",ErrKeyNotFound)`
on miss (`ops.go:24`). `Guard.Get`: `nil`⇒`(parseStatus(v), nil)`;
`errors.Is(err, kvstore.ErrKeyNotFound)`⇒`(Absent, nil)`; any other
error⇒`(Absent, errVerbatim)` (R4). `Get` does NOT consult
`FallbackEnabled` and emits NO `lookup` metric (the §3.6 SSOT counts the
SETNX-class decision only — D8).

## Conventions & deliberate decisions (build-spec D1..D14, condensed)

- **D1/D2 — one `*Guard`; `NewGuard` fail-fast.** `errors.Join` of per-arg
  `errors.New` for nil `redis` + `cfg.validate()`; `(nil, joinedErr)` on
  any failure; `deps = deps.withDefaults()` on success. `RedisSeam` is the
  load-bearing positional collaborator (no Deps); `Config` is a value;
  `Metrics`/`Clock`/`Logger` are optional-with-noop `Deps`.
- **D3/D3.1/D3.2 — frozen SSOT + ergonomic surface.** `SetNX` is the core
  primitive `pendingconfirmation` depends on; `CheckAndAcquire` is a thin
  adapter over the SAME atomic Lua. The two surfaces deliberately differ in
  the present-key signalling (`SetNX`⇒`ErrIdempotencyKeyExists`;
  `CheckAndAcquire`⇒`err==nil`+`alreadyExists`) — a reviewer must NOT
  "unify" them and break `pendingconfirmation`.
- **D4/D4.1 — one atomic Lua, no TOCTOU.** `luaSetNXOrGet` via
  `kvstore.Eval`, single round-trip; `decodeEvalResult` maps
  `{1,""}`⇒acquired / `{0,v}`⇒present(`parseStatus`) / else⇒`errEvalShape`
  (transport-class). Bounded single retry (exactly 2 attempts) on the
  `{0,nil}`/`{0,""}` keyspace-eviction corner, then D5's
  treat-as-PROCESSING-exists; never unbounded.
- **D5 — defensive `parseStatus`.** Known status ⇒ itself; unknown/garbage
  non-empty ⇒ `IdempotencyProcessing` (NEVER `Absent` — prevents the §6.3
  double-analysis the guard exists to stop). Absent only via
  `acquired==true` (D4) / `Get` `ErrKeyNotFound` (R5).
- **D6/D6.1 — `StartHeartbeat`.** Injectable `Clock.NewTicker`; `select`
  on `{ctx.Done(), stopCh, ticker.C()}`; per-tick `ExtendTTL`:
  `nil`⇒continue, `ErrIdempotencyKeyVanished`⇒WARN+return, transient
  err⇒WARN+continue. `sync.Once`-guarded `stop()` (twice-safe); every exit
  stops the ticker (no leak). NO metric (the §3.6 enum has no heartbeat
  result). `ExtendTTL`: `Expire`→true⇒nil / false⇒`ErrIdempotencyKey-
  Vanished` / err⇒verbatim.
- **D7/D8/D9 — seams + constants + Config.** `RedisSeam` (6 methods,
  required, no noop — the `var _` is 047's); `Metrics`/`Clock`/`Logger`
  optional-with-noop (`Logger` is Warn/Error only — no audit obligation);
  4 lookup constants value-identical to `labels.go:117-120` (pinned
  WITHOUT a metrics import). `Config` carries ONLY `HeartbeatInterval`+
  `FallbackEnabled` (no TTL fields — R3); `validate()` only
  `HeartbeatInterval>0` (the `HeartbeatInterval<ProcessingTTL` invariant is
  enforced once in `config.IdempotencyConfig.validate`, not duplicated).
- **D8 emission points.** `Lookup` ONLY on `SetNX`/`CheckAndAcquire`
  (new / in_progress[PROCESSING|PAUSED|garbage] / completed / fallback_db).
  `Get`/`ExtendTTL`/`SetCompleted`/`SetPaused`/heartbeat emit NO lookup
  metric. PAUSED maps to `in_progress` (there is no `paused` enum value).
- **D10 — hermetic allowlist.** Non-test files: stdlib
  (`context/errors/fmt/strings/sync/time`) + `internal/domain/port` +
  `internal/infra/kvstore` (error helpers/sentinels ONLY — never
  `kvstore.NewClient`). ZERO third-party. Forbidden (active-fail in
  `internal_test.go`): go-redis, `internal/config`,
  `internal/domain/model`, observability, `internal/application/*` (the
  Guard's own consumers — INVERTED), `internal/ingress/{consumer,router}`,
  prometheus/otel/miniredis.
- **D11 — in-package faithful `fakeRedis` (the miniredis-absent recorded
  deviation).** miniredis is absent offline; the fake is a faithful
  in-memory store (correct SET-NX-EX, GET miss⇒`kvstore.ErrKeyNotFound`,
  EXPIRE absent⇒`(false,nil)`, recording `Eval` honouring the D4 observable
  contract + exact `[]interface{}{int64,string}` shape, injectable-clock
  lazy expiry, programmable error injection). Heartbeat tests use a
  manually-driven `Ticker`; ZERO `time.Sleep`; `-race` clean.
- **D12 — key-agnostic.** Opaque key strings; the Guard NEVER inspects /
  parses / prefix-dispatches a key. "2-status keys" need zero code: they
  are simply keys their callers never `SetPaused`. `SetCompleted`/
  `SetPaused` are plain unconditional `RedisSeam.Set` (the §6.3:565 /
  §6.10:782 status switch); error verbatim.
- **D13/D14 — struct + self-checks.** Immutable `Guard{redis,cfg,metrics,
  clock,log}`; the in-package `var _ port.IdempotencyStorePort =
  (*Guard)(nil)` (the ONE permitted satisfaction assertion — the asserted
  interface is in the allowlist). `TestGofmtClean` via `go/format`
  (sandbox blocks `go fmt`); `TestHermeticImports` active-fail.

## Forward notes (recorded, owners elsewhere — build-spec PART D)

1. **LIC-TASK-040 (Event Router, `internal/ingress/router`).** Owns the
   §6.5:628-634 restart-decision-tree, x-death retry-level escalation, the
   broker ACK/NACK decision, the PAUSED→read-`lic-pending-state` branch,
   and the §6.3:633 safety-net. 040 builds the `lic-trigger:`+versionID key
   string (the Guard is key-agnostic — D12) and CONSUMES `CheckAndAcquire`
   (the §6.5 absent→SETNX / PROCESSING→NACK / PAUSED→read / COMPLETED→ACK
   tree maps directly onto the D3.2 return table). 040 DRIVES
   `StartHeartbeat(ctx, "lic-trigger:"+vid, processingTTL)` immediately
   after a `CheckAndAcquire`→`(Absent,false,nil)` and calls `stop()` on
   terminal status switch. 040 owns the
   `model.ErrCodeIdempotencyStoreUnavail` mapping for a fallback-disabled
   transport error (R1/R4 — the Guard returns the kvstore error verbatim).
2. **LIC-TASK-037 (`pendingconfirmation.Manager`) is an ALREADY-SHIPPED
   consumer (commit ed05a92).** It calls the frozen `SetNX`/`SetCompleted`/
   `SetPaused` (`manager.go:251,325,438,442,622`) and branches on
   `errors.Is(err, port.ErrIdempotencyKeyExists)` then `switch status`. The
   Guard's frozen-method contract (D3.1, R1) preserves this EXACTLY:
   present ⇒ `(status, ErrIdempotencyKeyExists)`; transport-down ⇒ a plain
   non-`ErrIdempotencyKeyExists` error ⇒ Manager maps to
   `IDEMPOTENCY_STORE_UNAVAILABLE` retryable. NOT regressed (PART C #5 —
   `pendingconfirmation` + `domain/port` tests still green).
3. **LIC-TASK-047 (app wiring).** Constructs
   `idempotency.NewGuard(kvstoreClient, idempotency.Config{HeartbeatInterval:
   cfg.Idempotency.HeartbeatInterval, FallbackEnabled:
   cfg.Idempotency.FallbackEnabled}, idempotency.Deps{Metrics:
   metricsAdapter over *metrics.IdempotencyMetrics (Lookup→
   LookupsTotal.WithLabelValues(result).Inc(); Fallback→FallbackTotal.Inc()),
   Clock: systemClock, Logger: loggerAdapter over *logger.Logger
   (Warn/Error)})`. Asserts in the WIRING package
   `var _ port.IdempotencyStorePort = (*idempotency.Guard)(nil)` and
   `var _ idempotency.RedisSeam = (*kvstore.Client)(nil)` (the structural
   `RedisSeam` satisfaction is verified there, NOT here). Injects the SAME
   `*Guard` as `port.IdempotencyStorePort` into
   `pendingconfirmation.NewManager` AND as the `CheckAndAcquire`/
   `StartHeartbeat` provider into the LIC-TASK-040 router — one Guard
   instance, two roles.
4. **No `go.mod` change.** The Guard imports only stdlib + `domain/port` +
   `infra/kvstore`. `go-redis`/`uuid`/`miniredis` are NOT imported (the
   primitive is the `RedisSeam`; tests use the in-package `fakeRedis`).
   `go mod tidy` produces no diff (verified).
5. **Architecture-doc staleness (R3, architecture-team-owned).**
   `high-architecture.md:1020` says "PROCESSING (90s TTL)"; the SSOT is
   150s (`:563`, `configuration.md:63`, `config/idempotency.go:23`). The
   adapter is TTL-agnostic so this never reaches code; a future
   architecture-consistency pass should correct `:1020` to 150s. Recorded,
   not silent.
