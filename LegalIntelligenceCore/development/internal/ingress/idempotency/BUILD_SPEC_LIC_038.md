# BUILD SPEC — LIC-TASK-038 Idempotency Guard (AUTHORITATIVE)

**Status:** binding. The golang-pro implementer follows this verbatim and makes
**no further architecture decisions**. Every non-obvious ground-truth claim is
cited as `file:line`. Scope is strictly LIC-TASK-038; everything tagged
"040-owned" / "037-owned" / "047-owned" is OUT OF SCOPE and forward-noted.

- Package: `internal/ingress/idempotency`
- Module: `contractpro/legal-intelligence-core`, Go 1.26.1
- Dev root: `LegalIntelligenceCore/development`
- Output exported type: `*Guard`; constructor `NewGuard` (`feedback_constructors.md`)

This package IS the Redis-backed infrastructure adapter for
`port.IdempotencyStorePort` — the exact analog of `internal/ingress/consumer`
being the broker adapter (consumer BUILD_SPEC D16): it is an **adapter layer**,
not an `internal/application/*` hermetic core, so it MAY import
`internal/infra/kvstore` **for the primitive only**, behind a local seam, and
is itself hermetic against prometheus/otel/redis/config/logger/application.

---

## 0. Verified ground truth (re-confirmed by reading source)

| Claim | Evidence |
|---|---|
| Frozen port: 5 methods, exact signatures | `internal/domain/port/idempotency.go:42-74` (`SetNX(ctx,key,ttl)(IdempotencyStatus,error)`, `Get(ctx,key)(IdempotencyStatus,error)`, `ExtendTTL(ctx,key,ttl)error`, `SetCompleted(ctx,key,ttl)error`, `SetPaused(ctx,key,ttl)error`) |
| 4 statuses + sentinel + `IsTerminal` | `idempotency.go:15-26` (`IdempotencyAbsent=""`, `IdempotencyProcessing="PROCESSING"`, `IdempotencyPaused="PAUSED"`, `IdempotencyCompleted="COMPLETED"`; `IsTerminal()` true ⇔ COMPLETED), `:33` (`ErrIdempotencyKeyExists`) |
| Port `SetNX` contract: success ⇒ `(IdempotencyAbsent, nil)`; present ⇒ existing status **wrapped in** `ErrIdempotencyKeyExists` | `idempotency.go:43-48` |
| Port `Get` contract: miss ⇒ `(IdempotencyAbsent, nil)` (NOT a Go error) | `idempotency.go:50-53` |
| Port `ExtendTTL` = EXPIRE-without-touching-value; the heartbeat extender | `idempotency.go:55-61` |
| Port godoc: TTLs are **per-call**; adapter MUST NOT hardcode 150s/24h/25h | `idempotency.go:48,59-60,64-65,68-73` (every TTL is a method param; the godoc's "150s/24h/25h" are *caller* defaults, not adapter constants) |
| Port godoc: adapter owns Lua / EX-EXPIRE / 4 statuses; "Domain code stays unaware" | `idempotency.go:38-41` |
| EXISTING consumer of the port (do NOT break it) | `internal/application/pendingconfirmation/manager.go:325-350` (`SetNX` + `errors.Is(err, port.ErrIdempotencyKeyExists)` then `switch status {case IdempotencyCompleted: case IdempotencyProcessing, IdempotencyPaused:}`; non-`ErrIdempotencyKeyExists` err ⇒ `model.ErrCodeIdempotencyStoreUnavail` retryable), `:438-444` (`SetCompleted`), `:251` (`SetPaused`), `:622` (`SetCompleted`) |
| kvstore primitives | `internal/infra/kvstore/ops.go:13` (`Get(ctx,key)(string,error)` → `("",ErrKeyNotFound)` on miss), `:34` (`Set(ctx,k,v,ttl)error`; ttl<=0 ⇒ no expiry), `:48` (`SetNX(ctx,k,v,ttl)(bool,error)`), `:63` (`Delete(...keys)(int64,error)`), `:79` (`Expire(ctx,k,ttl)(bool,error)` — `false` ⇒ key vanished), `:98` (`Eval(ctx,script,keys,args...)(any,error)`; Lua-nil ⇒ `(nil,nil)`) |
| kvstore error model | `internal/infra/kvstore/errors.go:35` (`ErrKeyNotFound` PLAIN sentinel), `:42-46` (`RedisError{Op,Retryable,Cause}`), `:68-74` (`IsRetryable`), `:86-100` (`mapError` — `context.Canceled/DeadlineExceeded` pass through RAW; `redis.Nil`→`ErrKeyNotFound`; `redis.ErrClosed`→non-retryable; else retryable) |
| kvstore CLAUDE.md explicitly assigns LIC-TASK-038 to build on these, owns Lua/EX-EXPIRE/4 statuses, translates a miss via `errors.Is(err, kvstore.ErrKeyNotFound)` | `internal/infra/kvstore/CLAUDE.md:6-9,41-44,76-79` |
| `*kvstore.Client` is the concrete primitive; `RedisAPI` seam internal to kvstore | `internal/infra/kvstore/client.go:40-65` |
| `config.IdempotencyConfig` already has `{TTL,ProcessingTTL,HeartbeatInterval,UserConfirmedProcessingTTL,FallbackEnabled}` + validates `HeartbeatInterval>0` and `HeartbeatInterval<ProcessingTTL` | `internal/config/idempotency.go:12-46` |
| `metrics.Metrics.Idempotency *IdempotencyMetrics{LookupsTotal *CounterVec[result], FallbackTotal Counter}` | `internal/infra/observability/metrics/idempotency.go:5-28`, `registry.go:33,66` |
| Metric label SSOT enum (4 values) | `internal/infra/observability/metrics/labels.go:113-121` (`IdempLookupNew="new"`, `IdempLookupInProgress="in_progress"`, `IdempLookupCompleted="completed"`, `IdempLookupFallbackDB="fallback_db"`) |
| `model.ErrCodeIdempotencyStoreUnavail` exists, retryable, non-publishable (empty userMessage) | `internal/domain/model/error_codes.go:59`, `:226-230` (retryable=true, userMessage="" → NACK path, not surfaced) |
| §6.3 keys table + 4-status table + heartbeat mechanism | `architecture/high-architecture.md:535-578` |
| §6.3:563 SSOT TTL = **150s** `LIC_IDEMPOTENCY_PROCESSING_TTL`; §8.2:1020 says "90s" | `high-architecture.md:563` (150s) vs `:1020` ("90s TTL") — STALE pre-heartbeat scenario figure (see R3) |
| §6.3:566 + ops.go:74-78: `EXPIRE`→false ⇒ key vanished ⇒ stop heartbeat | `high-architecture.md:566`, `kvstore/ops.go:74-78` |
| §6.3:576: "Heartbeat — общий механизм для всех PROCESSING-ключей" | `high-architecture.md:576` |
| §6.5:624-634 restart-decision-tree (absent→SETNX; PROCESSING→NACK; PAUSED→read pending; COMPLETED→ACK) | `high-architecture.md:628-634` |
| §6.10 lic-user-confirmed SETNX (90s), PAUSED→COMPLETED | `high-architecture.md:772,782,786` |
| configuration.md §2.4: `LIC_IDEMPOTENCY_FALLBACK_ENABLED` "DB у LIC нет — фактический эффект: ack без проверки + alert" | `architecture/configuration.md:62-65` |
| observability.md §3.6 metric names + result enum | `architecture/observability.md:154-159` |
| error-handling.md §3: `IDEMPOTENCY_STORE_UNAVAILABLE` STAGE_RECEIVE retryable true (NACK, not Orch-published) | `architecture/error-handling.md:127` |
| miniredis ABSENT from offline module cache | `development/go.sum` (zero `miniredis` hits — verified); `kvstore/CLAUDE.md:81-96` (the documented in-memory-fake precedent) |
| `github.com/redis/go-redis/v9 v9.18.0` direct require | `development/go.mod` |
| Precedents to mirror | `internal/ingress/consumer/{BUILD_SPEC_LIC_039.md,CLAUDE.md,seams.go,deps.go,internal_test.go}`, `internal/application/pendingconfirmation/{seams.go,deps.go,manager.go,CLAUDE.md}`, `internal/infra/kvstore/CLAUDE.md` |

---

## PART A — BINDING DECISIONS (D1..D14)

### D1 — Package & file layout

`internal/ingress/idempotency/` (new package `idempotency`):

| File | Purpose (one line) |
|---|---|
| `guard.go` | Package doc (hermetic statement + D-attribution); the status-string parse helper (D5); `Guard` struct; `NewGuard` (fail-fast `errors.Join`); the 5 frozen `port.IdempotencyStorePort` methods (`SetNX`/`Get`/`ExtendTTL`/`SetCompleted`/`SetPaused` — D3); the ergonomic `CheckAndAcquire` (D3); the metric/fallback policy core (D7/R1). |
| `script.go` | The atomic SETNX-or-return-existing Lua source constant (D4), its `KEYS`/`ARGV` doc, and the `decodeEvalResult(any)→(IdempotencyStatus,acquired bool, err)` decoder (D4). |
| `heartbeat.go` | `StartHeartbeat` (D6) — the injectable-ticker goroutine, `stop func()` return, the 3 stop conditions, idempotent stop, no-leak guarantee. |
| `seams.go` | Adapter-local seam interfaces + zero-dep defaults: `RedisSeam` (no noop — required; the kvstore subset), `Metrics`+`noopMetrics`, `Clock`+`systemClock`, `Logger`+`noopLogger`. `var _` assertions after each noop pair. The metric-result string constants (D8). |
| `deps.go` | `Deps{Metrics, Clock, Logger}` optional-with-noop bundle + `withDefaults()`. |
| `config.go` | Local `Config{HeartbeatInterval time.Duration; FallbackEnabled bool}` + `validate()` (D9). |
| `internal_test.go` | `TestHermeticImports` (allowlist + active-fail forbidden set) + `TestGofmtClean` (`go/format`). |
| `guard_test.go` | Behavioural suite: every PART C item, with an in-package faithful `fakeRedis` (D11) + fakes for `Metrics`/`Clock`/`Logger`; `-race` clean, deterministic (injected ticker). |
| `CLAUDE.md` | Package guide mirroring `consumer/CLAUDE.md` shape (Files / API / Reconciliations / Conventions / Forward notes). |

No other files. No subpackages. (`script.go` and `heartbeat.go` are split out
purely for reviewer locality; they are the same package — the implementer MAY
inline them into `guard.go` only if every D/R citation remains greppable, but
the split is RECOMMENDED and the default.)

### D2 — `NewGuard`, not `New` (feedback_constructors.md)

`feedback_constructors.md` mandates `NewTypeName`. The type is `Guard`;
constructor is **`NewGuard`** (stutter-free; the `consumer.NewConsumer` /
`pendingconfirmation.NewManager` precedent).

**Exact signature (positional = required, fail-fast non-nil; Deps = optional-with-noop):**

```go
func NewGuard(
    redis RedisSeam,   // required (the kvstore primitive seam — D10)
    cfg Config,        // value; validated (D9)
    deps Deps,         // optional (nil fields → noop)
) (*Guard, error)
```

Fail-fast via `errors.Join` of per-arg errors (the
`pendingconfirmation.NewManager`/`consumer.NewConsumer` precedent —
`manager.go:157-181`): `redis==nil` ⇒ a distinct `errors.New(...)`;
`cfg.validate()` non-nil ⇒ appended; on any failure return `(nil, joinedErr)`.
On success store `deps = deps.withDefaults()`.

**Why positional vs Deps:** `RedisSeam` is the load-bearing collaborator — a
Guard with no Redis seam cannot perform its contract; positional + required
(the consumer/pendingconfirmation "required collaborators are positional, NOT
in Deps" rule). `Metrics`/`Clock`/`Logger` are telemetry / determinism seams —
optional-with-noop in `Deps`. `Config` is a value type (the
`pendingconfirmation.Config` / `consumer.dlqHashKey` "local config, ctor-
injected, NOT internal/config import" precedent).

### D3 — One exported `*Guard`: frozen port SSOT + ergonomic surface

`*Guard` is the **single** exported type. It satisfies
`port.IdempotencyStorePort` EXACTLY (the 5 frozen signatures of
`idempotency.go:42-74`, byte-for-byte) **and** adds two ergonomic methods
(`CheckAndAcquire`, `StartHeartbeat`) the task names. (Reconciliation R2
adjudicates the naming mismatch between the task wording and the frozen port.)

**`SetNX` is the core; `CheckAndAcquire` wraps it.** Rationale: the frozen
`port.IdempotencyStorePort.SetNX` is already consumed by
`pendingconfirmation.Manager` with a precise behavioural contract
(`manager.go:325-350`); it MUST remain the authoritative primitive. A second
independent implementation of the same Redis round-trip would risk divergence.
`CheckAndAcquire` is a thin adapter over the same atomic Lua (D4) that returns
the same information in a different shape.

#### D3.1 — `SetNX` exact behaviour (FROZEN port contract — do NOT change)

```go
func (g *Guard) SetNX(ctx context.Context, key string, ttl time.Duration) (port.IdempotencyStatus, error)
```

1. Execute the atomic Lua (D4) with `KEYS=[key]`, `ARGV=[PROCESSING, ttlSeconds]`.
2. Decode (D4) → `(existing port.IdempotencyStatus, acquired bool, decodeErr error)`.
3. `decodeErr != nil` (decode/garbage) ⇒ treat as **exists, defensive** (D5):
   return `(IdempotencyProcessing, ErrIdempotencyKeyExists)` is WRONG — instead
   return the *raw* existing string parsed by D5's defensive rule. (See D5: an
   unparseable stored value maps to `IdempotencyProcessing` so the caller's
   `switch` NACK-retries rather than re-runs. The decode-shape error path —
   the Eval returned something structurally unexpected — is a Redis/infra fault:
   return `(IdempotencyAbsent, <a kvstore-style/local error, NOT a model code>)`
   so `pendingconfirmation` maps it to `model.ErrCodeIdempotencyStoreUnavail`
   retryable; see R1.)
4. `acquired == true` ⇒ key did not exist, we wrote `PROCESSING`:
   return `(port.IdempotencyAbsent, nil)` — the caller now owns the slot.
   Emit `lic_idempotency_lookups_total{result="new"}` (D8).
5. `acquired == false` ⇒ key existed; `existing` is its parsed status (D5):
   return `(existing, port.ErrIdempotencyKeyExists)` — EXACT frozen contract
   (`idempotency.go:46-47`; `pendingconfirmation` branches on
   `errors.Is(err, port.ErrIdempotencyKeyExists)` then
   `switch status`). Emit `{result="completed"}` if `existing==COMPLETED`
   else `{result="in_progress"}` (PROCESSING and PAUSED both ⇒ `in_progress`
   — there is no `paused` metric value; the SSOT enum is exactly
   `new|in_progress|completed|fallback_db`, `labels.go:117-120`, D8).
6. Transport error from the Eval (Redis unreachable, context error, etc.):
   the **fallback policy** (R1) decides. SETNX is a *write/acquire* — the
   fallback "proceed without dedup" path is meaningful on the ergonomic
   `CheckAndAcquire` surface (040-driven), NOT on the frozen `SetNX`: returning
   `IdempotencyAbsent,nil` from `SetNX` on Redis-down would make
   `pendingconfirmation.Manager` (`manager.go:325`) silently *acquire* a slot
   it never wrote (double-resume risk). Therefore **`SetNX` on transport error
   ALWAYS returns `(IdempotencyAbsent, transportErr)` where `transportErr` is
   the kvstore/local error, NEVER a `model.ErrorCode`, NEVER wrapping
   `ErrIdempotencyKeyExists`** — preserving the existing
   `pendingconfirmation` contract (non-`ErrIdempotencyKeyExists` err ⇒
   `model.ErrCodeIdempotencyStoreUnavail` retryable NACK,
   `manager.go:346-349`). `SetNX` does NOT consult `FallbackEnabled` (R1).

#### D3.2 — `CheckAndAcquire` exact signature + mapping

```go
// CheckAndAcquire is the ergonomic restart-decision surface LIC-TASK-040's
// §6.5 decision tree consumes (high-architecture.md:628-634). It performs the
// SAME atomic SETNX-or-return-existing as SetNX (D4) — it does NOT add a
// second round-trip — and returns the decision as a (status, alreadyExists,
// err) triple instead of the frozen (status, error/ErrIdempotencyKeyExists)
// shape. ttl is REQUIRED (the per-call TTL the caller would pass to SetNX;
// the task's no-ttl wording is reconciled in R2 — TTLs are per-call/per-key
// and the adapter never hardcodes them, idempotency.go:48 / R3).
func (g *Guard) CheckAndAcquire(
    ctx context.Context, key string, ttl time.Duration,
) (status port.IdempotencyStatus, alreadyExists bool, err error)
```

Mapping (PINNED), all driven by the SAME atomic Lua (D4):

| Redis state | return `status` | `alreadyExists` | `err` | metric (D8) |
|---|---|---|---|---|
| absent (acquired PROCESSING) | `IdempotencyAbsent` | `false` | `nil` | `{result="new"}` |
| present, `PROCESSING` | `IdempotencyProcessing` | `true` | `nil` | `{result="in_progress"}` |
| present, `PAUSED` | `IdempotencyPaused` | `true` | `nil` | `{result="in_progress"}` |
| present, `COMPLETED` | `IdempotencyCompleted` | `true` | `nil` | `{result="completed"}` |
| present, unparseable value | `IdempotencyProcessing` (defensive — D5) | `true` | `nil` | `{result="in_progress"}` |
| Redis transport error, `FallbackEnabled==false` | `IdempotencyAbsent` | `false` | `<kvstore/local err, NOT model code>` | none for that err; see R1 |
| Redis transport error, `FallbackEnabled==true` | `IdempotencyAbsent` | `false` | `nil` (signals "proceed/ack without dedup") | `{result="fallback_db"}` + `FallbackTotal` + WARN/ERROR log (R1) |

**Decision (binding): on `present`, `CheckAndAcquire` returns `err == nil`** and
the caller reads `alreadyExists`/`status` — it does **NOT** return
`ErrIdempotencyKeyExists`. Rationale: `ErrIdempotencyKeyExists` is the frozen
`SetNX` carrier (`idempotency.go:33,46-47`) that `pendingconfirmation` already
depends on via `errors.Is`; `CheckAndAcquire` is a NEW, 040-only surface with
no existing consumer, so the cleaner `(status, alreadyExists, nil)` shape is
chosen (it matches the task's literal `(status, alreadyExists, err)` tuple).
The two surfaces deliberately differ in the present-key signalling; this is
recorded so a reviewer does not "unify" them and break `pendingconfirmation`.
`SetNX` keeps `ErrIdempotencyKeyExists`; `CheckAndAcquire` does not.

`CheckAndAcquire` **does** consult `FallbackEnabled` (R1) — it is the
040/§6.5-ingress surface where "ack без проверки + alert"
(`configuration.md:65`) is the documented behaviour. `SetNX` does not (D3.1.6).

### D4 — Atomic SETNX-then-read-existing via one Lua `Eval` (no TOCTOU)

A naive `kvstore.SetNX(false)` then `kvstore.Get` has a TOCTOU: the key may
expire (or transition PROCESSING→COMPLETED) in the gap, yielding a stale or
miss reply that could cause a double-analysis or a wrong NACK. The adapter
therefore uses ONE Lua script via `kvstore.Eval` (`ops.go:98`,
`kvstore/CLAUDE.md:71-79` — the adapter owns its Lua), single round-trip:

**Script source (a package-level `const luaSetNXOrGet string`):**

```lua
-- KEYS[1] = idempotency key
-- ARGV[1] = "PROCESSING"     (the value to set if absent)
-- ARGV[2] = ttl seconds (integer string)
-- Returns: {1, ""}        when the key was set (acquired; absent before)
--          {0, <value>}   when the key already existed (its current value)
if redis.call('SET', KEYS[1], ARGV[1], 'NX', 'EX', ARGV[2]) then
  return {1, ''}
else
  return {0, redis.call('GET', KEYS[1])}
end
```

Notes the implementer MUST honour:
- `SET ... NX EX` returns Lua `true`/`false` (a status-or-nil); the `if` works
  because `redis.call('SET',...,'NX',...)` yields a truthy reply only on a
  successful set. This is the atomic primitive — no separate `EXISTS`.
- The `GET` in the `else` branch is inside the same script ⇒ atomic with the
  failed `SET NX`: zero TOCTOU. If the key vanished between `SET NX` failing
  and `GET` (impossible inside a single Lua call — Redis Lua is atomic), this
  cannot occur; but DEFENSIVELY, if `GET` returns Lua `nil` (a benign race
  ONLY conceivable under keyspace eviction maxmemory policies), the decoder
  (below) treats it as **acquired-miss**: re-run the script once
  (bounded single retry, D4.1) — a vanished key means "absent now", which is
  the new-event path. Do NOT loop unbounded.

**`KEYS`/`ARGV` passed to `kvstore.Eval`:** `keys=[]string{key}`,
`args=[]any{string(port.IdempotencyProcessing), int(ttl/time.Second)}`. TTL is
converted to integer seconds; if `ttl < time.Second` use `1` (Redis `EX` min);
this is defensive — production TTLs are 150s/24h/25h. Never pass a hardcoded
TTL: the value comes from the method's `ttl` param (R3 — TTLs are per-call).

**`decodeEvalResult(any) (existing port.IdempotencyStatus, acquired bool, err error)`**
(`script.go`): `kvstore.Eval` returns `any` (`ops.go:98`). A Lua table
`{n, v}` decodes (via go-redis) as `[]interface{}{int64, string}`.
- Result is `[]interface{}` of length 2, `[0]` an `int64`:
  - `[0]==1` ⇒ `(IdempotencyAbsent, true, nil)` (acquired).
  - `[0]==0` ⇒ `[1]` is the stored value string ⇒
    `(parseStatus(v), false, nil)` (D5).
- Result is `nil` (the `ops.go:95-97` Lua-nil case) OR any other shape ⇒
  treat as a **decode-shape fault**: return
  `(IdempotencyAbsent, false, errEvalShape)` where `errEvalShape` is a
  package-local `errors.New("idempotency: unexpected EVAL result shape")`
  (NOT a `model.ErrorCode`). The caller (`SetNX`/`CheckAndAcquire`) treats
  this as a transport-class error (R1) — it is an infra anomaly, not a
  business state.

#### D4.1 — Bounded benign-race retry

If `[0]==0` but `[1]` is Lua `nil` / empty-and-not-a-valid-status (the
keyspace-eviction corner): the implementer re-runs the script **exactly once**
more. If the second attempt is still `[0]==0` with an unparseable value, fall
through to D5's defensive **treat-as-PROCESSING-exists** (never a re-run path).
This single retry is local to `SetNX`/`CheckAndAcquire`; it is NOT the
heartbeat. Bounded by a constant `1` (no config knob — YAGNI).

### D5 — Status value parsing & defensive unknown

Stored values are exactly the `port.IdempotencyStatus` const strings
(`idempotency.go:16-19`): `"PROCESSING"`, `"PAUSED"`, `"COMPLETED"` (the empty
string never gets *stored* — it is the absent sentinel). `parseStatus`:

```go
func parseStatus(v string) port.IdempotencyStatus {
    switch port.IdempotencyStatus(v) {
    case port.IdempotencyProcessing, port.IdempotencyPaused, port.IdempotencyCompleted:
        return port.IdempotencyStatus(v)
    default:
        // Defensive: a corrupt / unknown stored value MUST NOT be read as
        // IdempotencyAbsent — that would let a redelivery re-run a pipeline
        // that is in fact in-flight/done (double-analysis, the §6.3 invariant
        // this whole guard exists to prevent). The safest interpretation of
        // "a key exists but its value is garbage" is "something owns this
        // slot": map to IdempotencyProcessing so the caller NACK-retries
        // (pendingconfirmation switch: PROCESSING ⇒ retryable NACK,
        // manager.go:333-340) — never ACKs-as-completed, never re-runs.
        return port.IdempotencyProcessing
    }
}
```

This is the binding rule: **unknown/garbage non-empty value ⇒
`IdempotencyProcessing`**. (An *absent* key — `acquired==true` from D4 — is the
only path to `IdempotencyAbsent`.)

### D6 — `StartHeartbeat` lifecycle

```go
// StartHeartbeat launches a goroutine that, every cfg.HeartbeatInterval,
// calls ExtendTTL(ctx, key, ttl) (EXPIRE key ttl — idempotency.go:55-61,
// high-architecture.md:564) to keep a PROCESSING key alive while a pipeline
// holds it. It returns a stop func() the caller invokes on terminal status
// switch (COMPLETED/PAUSED/cleanup — high-architecture.md:576). ttl is the
// per-call PROCESSING TTL the caller used at SETNX (R3 — adapter never
// hardcodes 150s). LIC-TASK-040 (the §6.3:576 "Pipeline Orchestrator … always
// starts the heartbeat at SETNX PROCESSING and stops it at terminal switch")
// drives this for lic-trigger; the mechanism is key-agnostic (D12,
// high-architecture.md:576 "общий механизм для всех PROCESSING-ключей").
func (g *Guard) StartHeartbeat(
    ctx context.Context, key string, ttl time.Duration,
) (stop func())
```

Returns **only `stop func()`** (no error): launching a goroutine cannot fail;
an `ExtendTTL` failure is handled inside the loop (below), not at launch. (If a
later reviewer insists on an error return, it would always be `nil` — YAGNI;
the binding signature is `stop func()` only.)

Goroutine loop (PINNED):
1. Build a ticker via the injectable `Clock.NewTicker(cfg.HeartbeatInterval)`
   (D7's `Clock` seam — deterministic `-race` tests; the
   `pipeline.Clock`/`consumer.Clock` precedent extended with `NewTicker`).
2. `select` on, each iteration:
   - `<-ctx.Done()` ⇒ stop the ticker, return (the caller's ctx cancelled —
     shutdown / pipeline ended; `idempotency.go:57-60` "on crash the heartbeat
     stops"). No metric, no error.
   - `<-stopCh` (closed by `stop()`) ⇒ stop the ticker, return.
   - `<-ticker.C` ⇒ call `g.ExtendTTL(ctx, key, ttl)`:
     - returns `nil` ⇒ continue (TTL refreshed).
     - returns the **key-vanished sentinel** (D6.1) ⇒ the key expired/was
       deleted (`Expire`→false, `ops.go:74-78`, `high-architecture.md:566`):
       log WARN ("idempotency heartbeat: key vanished, stopping", key), stop
       the ticker, return. The PROCESSING marker is gone; continuing to
       `EXPIRE` a non-existent key is pointless and the §6.3:566 contract
       says the heartbeat stops.
     - returns any other (transport) error ⇒ log WARN
       ("idempotency heartbeat: ExtendTTL transient error", key, err) and
       **continue** (a single transient Redis blip must not abandon a live
       pipeline's PROCESSING marker; the next tick retries; the 150s TTL has
       slack over the 30s interval — `configuration.md:63-64`).
3. Idempotent `stop`: `stop()` is `sync.Once`-guarded `close(stopCh)`; calling
   it twice (or after ctx-cancel already returned) is safe and does not panic.
4. No goroutine leak: every exit path stops the ticker and returns; the
   goroutine terminates on ANY of {ctx.Done, stop(), key-vanished}. A test
   (PART C #8) asserts the goroutine exits within a bounded time after each
   trigger using a `done` channel the goroutine closes on return.

Heartbeat emits **no `lic_idempotency_*` metric** (the §3.6 enum has no
heartbeat result; observability.md:154-159) — only WARN logs on
vanished/transient. It does NOT emit `new/in_progress/completed/fallback_db`
(those are lookup outcomes, not tick outcomes — D8).

#### D6.1 — Key-vanished signal from `ExtendTTL`

`port.ExtendTTL` returns `error`. `kvstore.Expire` returns `(bool,error)` where
`false` = key gone (`ops.go:74-78`). The adapter's `ExtendTTL` (D3 port method)
MUST translate `Expire→(false,nil)` into a **package-local sentinel**
`ErrIdempotencyKeyVanished = errors.New("idempotency: key vanished")` (NOT a
`model.ErrorCode`, NOT `kvstore.ErrKeyNotFound` — that is Get-specific). A
`true` result ⇒ `nil`. A transport error ⇒ the kvstore error verbatim. The
heartbeat loop uses `errors.Is(err, ErrIdempotencyKeyVanished)` for its
stop-vs-continue branch (D6.2). `ExtendTTL` returning this sentinel is also the
correct contract for any direct caller: "the key you wanted to extend is gone"
is not a transient failure.

### D7 — Adapter-local seams (`RedisSeam`/`Metrics`/`Clock`/`Logger`)

All in `seams.go`. Mirrors `consumer/seams.go` + `pendingconfirmation/seams.go`.

```go
// RedisSeam is the SUBSET of *kvstore.Client this adapter uses. Declared
// adapter-side so kvstore is injected behind an interface for hermetic unit
// testing against an in-memory fakeRedis (D11). *kvstore.Client structurally
// satisfies it; the var _ RedisSeam = (*kvstore.Client)(nil) assertion lives
// in the LIC-TASK-047 WIRING package, NOT here (D10/D14 — asserting it here
// would force the kvstore import to be load-bearing for compilation rather
// than types-only, mirroring consumer D5). NO noop — required positional
// NewGuard param (D2).
type RedisSeam interface {
    SetNX(ctx context.Context, key, value string, ttl time.Duration) (bool, error)
    Get(ctx context.Context, key string) (string, error)
    Set(ctx context.Context, key, value string, ttl time.Duration) error
    Expire(ctx context.Context, key string, ttl time.Duration) (bool, error)
    Delete(ctx context.Context, keys ...string) (int64, error)
    Eval(ctx context.Context, script string, keys []string, args ...any) (any, error)
}
```

The subset is exactly the `kvstore/CLAUDE.md:76-79` §6.3/§6.10 op coverage.
`SetNX`/`Delete` are included for completeness/forward-use even though the
primary path is `Eval` (D4) + `Set` (SetCompleted/SetPaused) + `Expire`
(ExtendTTL) + `Get` (Get). Implementer MUST NOT widen it beyond these 6.

```go
type Metrics interface {
    // Lookup increments lic_idempotency_lookups_total{result}. result MUST be
    // one of the D8 constants (== labels.go:117-120 string values).
    Lookup(result string)
    // Fallback increments lic_idempotency_fallback_total (no labels —
    // idempotency.go metric is a plain Counter, metrics/idempotency.go:13).
    Fallback()
}
type noopMetrics struct{}
func (noopMetrics) Lookup(string) {}
func (noopMetrics) Fallback()     {}
var _ Metrics = noopMetrics{}
```

```go
// Clock is the deterministic-time seam. Unlike pendingconfirmation.Clock
// (Now() only) this seam ALSO exposes NewTicker so the heartbeat goroutine
// (D6) is deterministic under -race (the test injects a manually-driven
// ticker). Now() is currently unused by the Guard but is kept for parity with
// the universal Clock seam shape and forward use; an implementer MAY omit
// Now() if go vet/lint flags it unused — the BINDING surface is NewTicker;
// Now() is OPTIONAL. systemClock.NewTicker wraps time.NewTicker behind the
// Ticker seam.
type Ticker interface {
    C() <-chan time.Time
    Stop()
}
type Clock interface {
    NewTicker(d time.Duration) Ticker
}
type systemClock struct{}
func (systemClock) NewTicker(d time.Duration) Ticker { /* time.NewTicker behind Ticker */ }
var _ Clock = systemClock{}
```

(`time.Ticker` has a field `C`, not a method, so the seam wraps it: a
`realTicker struct{ t *time.Ticker }` with `C() <-chan time.Time { return rt.t.C }`
and `Stop() { rt.t.Stop() }`. This wrapper is the only reason `Ticker` is an
interface — required for the hermetic `-race`-deterministic heartbeat test.)

```go
type Logger interface {
    Warn(ctx context.Context, msg string, kv ...any)
    Error(ctx context.Context, msg string, kv ...any)
}
type noopLogger struct{}
func (noopLogger) Warn(context.Context, string, ...any)  {}
func (noopLogger) Error(context.Context, string, ...any) {}
var _ Logger = noopLogger{}
```

`Logger` is **Warn/Error only** (NO `Info`): unlike `pendingconfirmation`
(which needs `Info` for the §11.2 mandatory audit trail), the Guard has no
audit obligation — it only logs the fallback alert (ERROR/WARN, R1) and the
heartbeat vanished/transient (WARN, D6). This mirrors `pipeline.Logger`
(Warn/Error). No `WithRequestContext` (that is the consumer's ingress-once
concern, R4 of consumer; the Guard is invoked *by* 040 which already attached
the ctx — the Guard never re-attaches).

### D8 — Metric-result string constants (compile-safe, mirror the SSOT)

`labels.go` is in the forbidden `metrics` package (D10). The Guard declares its
own four typed constants in `seams.go`, value-identical to `labels.go:117-120`
(the consumer D18 precedent):

```go
const (
    lookupNew        = "new"         // == metrics.IdempLookupNew        (labels.go:117)
    lookupInProgress = "in_progress" // == metrics.IdempLookupInProgress (labels.go:118)
    lookupCompleted  = "completed"   // == metrics.IdempLookupCompleted  (labels.go:119)
    lookupFallbackDB = "fallback_db" // == metrics.IdempLookupFallbackDB (labels.go:120)
)
```

`guard_test.go` `TestLookupConstantsMatchSSOT` asserts the four literals are
exactly `"new"/"in_progress"/"completed"/"fallback_db"` WITHOUT importing
metrics (hermeticity; the consumer D18 precedent).

**Exact emission points (PINNED — no other emission anywhere):**

| Call site | Condition | Emission |
|---|---|---|
| `SetNX` / `CheckAndAcquire` | acquired (absent) | `Lookup(lookupNew)` |
| `SetNX` / `CheckAndAcquire` | present `PROCESSING`/`PAUSED`/unparseable | `Lookup(lookupInProgress)` |
| `SetNX` / `CheckAndAcquire` | present `COMPLETED` | `Lookup(lookupCompleted)` |
| `CheckAndAcquire` ONLY | Redis transport err + `FallbackEnabled==true` | `Lookup(lookupFallbackDB)` **and** `Fallback()` (both — observability.md:157-158: lookups{fallback_db} AND lic_idempotency_fallback_total) |
| any | Redis transport err + `FallbackEnabled==false` | **no** lookup metric for that path (it is an error return, not a classified lookup) |
| `Get` / `ExtendTTL` / `SetCompleted` / `SetPaused` | always | **no** lookup metric (these are not "lookups"; observability.md §3.6 counts the SETNX-class decision only — `SetNX`/`CheckAndAcquire`) |
| `StartHeartbeat` ticks | any | **no** metric (D6) |

`Get` deliberately emits NO metric: the §3.6 SSOT counts the
dedup-decision lookup (the SETNX-class). `Get` is a passive read used by
040's PAUSED→read-pending branch; double-counting it as a "lookup" would
distort the dedup-rate SLO. Recorded so a reviewer does not "add the missing
Get metric".

### D9 — `Config` + `validate` (local, NO internal/config import)

```go
type Config struct {
    // HeartbeatInterval is the EXPIRE cadence for StartHeartbeat (D6); from
    // config.IdempotencyConfig.HeartbeatInterval (LIC_IDEMPOTENCY_HEARTBEAT_
    // INTERVAL, default 30s — configuration.md:64). MUST be > 0.
    HeartbeatInterval time.Duration
    // FallbackEnabled gates the CheckAndAcquire Redis-down degraded path
    // (R1); from config.IdempotencyConfig.FallbackEnabled
    // (LIC_IDEMPOTENCY_FALLBACK_ENABLED, default false — configuration.md:65).
    FallbackEnabled bool
}
func (c Config) validate() error {
    var errs []error
    if c.HeartbeatInterval <= 0 {
        errs = append(errs, errors.New("idempotency: Config.HeartbeatInterval must be > 0"))
    }
    return errors.Join(errs...)
}
```

**Binding:** the Guard's `Config` carries ONLY `HeartbeatInterval` +
`FallbackEnabled` — the two **intrinsic** knobs. It does **NOT** carry
`ProcessingTTL`/`TTL`/`PendingConfirmationTTL`/`UserConfirmedProcessingTTL`:
every TTL is a **per-call method parameter** on the frozen port
(`idempotency.go:48,61,66,73`), so the adapter MUST NOT hold or hardcode them
(R3). The Guard does NOT re-validate `HeartbeatInterval < ProcessingTTL` —
that invariant is enforced once in `config.IdempotencyConfig.validate`
(`config/idempotency.go:40-41`) and ProcessingTTL is not even known to the
Guard (it arrives per-call as `ttl`). Duplicating it here would be a false
coupling. (`pendingconfirmation.Config` precedent: local config, only the
knobs the type owns.)

### D10 — Hermetic adapter import allowlist

`ingress/idempotency` is an inbound-side **infrastructure adapter** (it
implements a domain port over Redis), exactly like `ingress/consumer` is the
broker adapter (consumer BUILD_SPEC D16) and `kvstore/CLAUDE.md:6-9` explicitly
designates this package as the `port.IdempotencyStorePort` adapter built on
kvstore primitives. Therefore importing `internal/infra/kvstore` **for its
exported error sentinels/types only** (`kvstore.ErrKeyNotFound`,
`kvstore.RedisError`, `kvstore.IsRetryable`) is correct — but the *primitive
itself* is injected behind the `RedisSeam` (D7), so the kvstore import is
**types/sentinels only**, never `kvstore.NewClient`.

**MAY import (the EXACT allowlist for non-test files):**

stdlib:
- `context`, `errors`, `fmt`, `strings`, `sync`, `time`

first-party:
- `contractpro/legal-intelligence-core/internal/domain/port` (the frozen
  `IdempotencyStorePort`, `IdempotencyStatus`, `ErrIdempotencyKeyExists`)
- `contractpro/legal-intelligence-core/internal/infra/kvstore` (ADAPTER
  exception — exported error helpers ONLY: `kvstore.ErrKeyNotFound`,
  `kvstore.IsRetryable`, `*kvstore.RedisError`; the primitive is the
  `RedisSeam`, NOT a concrete `*kvstore.Client`)

NO third-party. NO `internal/domain/model` (the adapter returns
kvstore/local-typed errors + `port.ErrIdempotencyKeyExists`, **NEVER** a
`model.ErrorCode` — the kvstore/broker discipline, `kvstore/errors.go:13-25`;
the *Manager* maps to `model.ErrCodeIdempotencyStoreUnavail`, not the Guard,
R1). NO `uuid` (the Guard is key-agnostic — D12; it never parses keys).

**MUST NOT import (active-fail forbidden set in `internal_test.go`):**
- `github.com/redis/go-redis/v9` (the kvstore shields it — `client.go:30`;
  the Guard is one layer above, behind `RedisSeam`)
- `contractpro/legal-intelligence-core/internal/config` (Config is a local
  struct ctor-injected — D9; the pendingconfirmation/consumer precedent)
- `contractpro/legal-intelligence-core/internal/infra/observability/...`
  (logger / metrics / tracer — all are seams, D7)
- `contractpro/legal-intelligence-core/internal/domain/model` (no model code
  — R1; the kvstore discipline)
- `contractpro/legal-intelligence-core/internal/application/...` (any —
  pendingconfirmation/pipeline are CONSUMERS of the port, not deps; the
  dependency is INVERTED — the Guard must not import its own consumers)
- `contractpro/legal-intelligence-core/internal/ingress/consumer`,
  `internal/ingress/router` (040 — also a consumer of this adapter)
- `github.com/prometheus/...`, `go.opentelemetry.io/...`,
  `github.com/alicebob/miniredis/...` (the metrics/tracer are seams; miniredis
  is absent offline and replaced by the in-package fake — D11)

`internal_test.go` actively fails (like
`consumer/internal_test.go` / `pendingconfirmation/internal_test.go`) if any
forbidden internal lands in the allowlist BEFORE the scan, then scans every
non-test `.go` file: first-party imports must be in the 2-entry allowlist
(`domain/port`, `infra/kvstore`); ANY third-party fails (there is no permitted
third-party — unlike consumer's `google/uuid`).

### D11 — Test strategy (deliberate, intent-preserving deviation from "miniredis")

The task's `test_steps` say "go test ./internal/ingress/idempotency/...
(miniredis)". **`github.com/alicebob/miniredis` is absent from the offline
module cache and the network is unavailable** (verified: zero `miniredis` hits
in `development/go.sum`; the documented `kvstore/CLAUDE.md:81-96` /
`broker`/`pendingconfirmation` precedent — those packages shipped with faithful
in-memory fakes instead of a live/embedded server). This BUILD_SPEC therefore
**mandates an in-package faithful `fakeRedis`** as the recorded,
intent-preserving deviation that satisfies the task's "miniredis" step:

- `fakeRedis` (in `guard_test.go`) — a faithful in-memory store implementing
  `RedisSeam`: correct `SET … NX EX` (first-writer-wins + per-key expiry),
  `GET` (miss ⇒ `("", kvstore.ErrKeyNotFound)` — D-faithful to `ops.go:24`),
  `SET … EX`, `EXPIRE` (returns `false` when key absent/expired — faithful to
  `ops.go:74-78`), `DEL`, and a **recording `Eval`** that interprets the D4
  Lua's *observable contract* (SET-NX-EX-or-return-existing) over its in-memory
  map and returns the exact `[]interface{}{int64, string}` shape `Eval` would
  surface. Lazy TTL expiry driven by an injectable test clock so expiry is
  deterministic (no `time.Sleep`). Programmable error injection (force
  `Eval`/`Set`/`Expire` to return a `*kvstore.RedisError{Retryable:true}` or a
  `context.DeadlineExceeded`) for the R1 fallback / transport-error paths.
- The heartbeat tests inject a manually-driven `Ticker` (D7) so a "tick" is a
  test-controlled channel send — `-race` clean, zero wall-clock waits, fully
  deterministic. "Heartbeat extends TTL": drive 1 tick, assert the fake's
  recorded `Expire(key, ttl)` call + the key's deadline advanced. "Stop
  heartbeat → TTL expires naturally": call `stop()`, advance the fake clock
  past the last deadline, assert `Get`→absent.

True Lua bytecode execution is impossible offline; the `Eval` tests assert the
**observable contract** (the SET-NX-EX-or-GET semantics + the exact result
shape + KEYS/ARGV passthrough) exactly as `kvstore/CLAUDE.md:91-96` does for
its own `Eval` dispatch tests. This is a recorded deviation, not a silent one.

### D12 — Key-agnostic; "2-status keys" need no special-casing

The Guard operates on **opaque key strings**. It NEVER inspects, parses, or
special-cases a key prefix (`lic-trigger:` / `lic-version-created:` /
`lic-artifacts-resp:` / `lic-persist-resp:` / `lic-persist-fail:` /
`lic-user-confirmed:`). The §6.3:535-544 key→subscription mapping and the
§6.5:628-634 restart-decision-tree are **LIC-TASK-040's** interpretation
(040 builds the `lic-trigger:`+versionID string and decides per-status). The
"other idempotency keys — simplified 2-status (PROCESSING/COMPLETED)"
requirement is satisfied **structurally with zero code**: those keys are simply
keys on which `SetPaused` is never called by their callers, so they only ever
hold PROCESSING (via `SetNX`/`CheckAndAcquire`) or COMPLETED (via
`SetCompleted`). The 4-status machinery is uniform; PAUSED only ever appears on
`lic-trigger:` because only `pendingconfirmation.Manager`/040 calls `SetPaused`
on that key (`manager.go:251`). Recorded so a reviewer does not demand
prefix-dispatch logic in the adapter (it would be a layering violation —
key semantics are 040-owned).

`SetCompleted` / `SetPaused` (D3 port methods) are a plain
`RedisSeam.Set(ctx, key, string(IdempotencyCompleted|IdempotencyPaused), ttl)`
(`ops.go:34` — value + EX ttl; ttl per-call, R3). They unconditionally
overwrite (a status *switch* PROCESSING/PAUSED→COMPLETED or PROCESSING→PAUSED
is exactly what §6.3:565 / §6.10:782 require: "SET ... = COMPLETED EX 24h" /
"SET lic-trigger = PAUSED EX 25h" — not conditional). On `Set` error return
the kvstore error verbatim (NOT a model code — R1); `pendingconfirmation`
already logs-and-continues those (`manager.go:438-444,622-625`), so the
adapter just surfaces the error faithfully.

### D13 — `Guard` struct + immutability

```go
type Guard struct {
    redis RedisSeam
    cfg   Config

    metrics Metrics
    clock   Clock
    log     Logger
}
```

No mutable per-instance state; all 7 methods (`SetNX`, `Get`, `ExtendTTL`,
`SetCompleted`, `SetPaused`, `CheckAndAcquire`, `StartHeartbeat`) are
goroutine-safe for distinct keys (the `RedisSeam`/`*kvstore.Client` is
concurrency-safe — `kvstore/client.go:55-57`; each `StartHeartbeat` call owns
its own ticker + stopCh + goroutine; no shared mutable field). The
`var _ port.IdempotencyStorePort = (*Guard)(nil)` assertion is the
LIC-TASK-047 WIRING package's, NOT here (D10 — asserting it here is fine
hermetically since it only needs `domain/port`, but the universal
consumer/pendingconfirmation precedent puts the wiring satisfaction assertion
in 047; the Guard MAY include it in `guard.go` since `port` is already an
allowed import and it is a pure compile-time check — RECOMMENDED to include it
in `guard.go` as it costs nothing and catches drift early; this is the ONE
satisfaction assertion permitted in-package because the asserted interface is
in the allowlist, unlike consumer's broker/router which are not).

### D14 — gofmt self-check + hermetic test (the universal precedent)

`internal_test.go` carries `TestGofmtClean` (`go/format` over every `.go` —
the sandbox blocks `go fmt`; verbatim
`consumer/internal_test.go` / `pendingconfirmation/internal_test.go` shape) and
`TestHermeticImports` (D10 — the 2-entry allowlist `{domain/port,
infra/kvstore}` + the D10 active-fail forbidden set + ZERO permitted
third-party).

---

## PART B — RECONCILIATIONS (R1..R5, DEFECT-style: Doc / Impl / Why)

### R1 — Fallback (`LIC_IDEMPOTENCY_FALLBACK_ENABLED`): two surfaces, two contracts

**Doc — tension.** (a) `configuration.md:65`: `LIC_IDEMPOTENCY_FALLBACK_ENABLED`
"DB у LIC нет — фактический эффект: ack без проверки + alert". (b)
`observability.md:157-158`: on Redis-down fallback, emit
`lic_idempotency_lookups_total{result="fallback_db"}` AND
`lic_idempotency_fallback_total`. (c) the EXISTING consumer
`pendingconfirmation.Manager` (`manager.go:325-350`) calls the frozen `SetNX`
and, on a **non-`ErrIdempotencyKeyExists`** error, maps to
`model.ErrCodeIdempotencyStoreUnavail` **retryable NACK** (it does NOT
"proceed without dedup"). (d) `error-handling.md:127`:
`IDEMPOTENCY_STORE_UNAVAILABLE` is retryable, NACK, not Orch-published.

**Conflict:** "ack без проверки" (proceed) vs the existing `SetNX` consumer's
"retryable NACK". A single behaviour on Redis-down cannot serve both.

**Impl (binding).** The two surfaces get two contracts (the consumer R1
"split-ownership: the layer lacking the broader concern returns the simplest
correct primitive; the owning surface extends it" discipline):

- **Frozen `SetNX` (and `Get`/`ExtendTTL`/`SetCompleted`/`SetPaused`) on
  Redis-down ⇒ ALWAYS return the kvstore/local transport error verbatim,
  NEVER a `model.ErrorCode`, NEVER wrapping `ErrIdempotencyKeyExists`,
  NEVER consulting `FallbackEnabled`.** This preserves the existing
  `pendingconfirmation` contract EXACTLY: a non-`ErrIdempotencyKeyExists`
  error ⇒ `model.ErrCodeIdempotencyStoreUnavail` retryable NACK
  (`manager.go:343-349`). The Guard does NOT decide fallback for the frozen
  port — the *application layer* owns the NACK-vs-proceed policy
  (`error-handling.md:127`), and `pendingconfirmation` has already
  implemented "retryable NACK". Changing `SetNX` to silently proceed would
  let the Manager double-resume.

- **Ergonomic `CheckAndAcquire` (the 040/§6.5-ingress surface) ⇒ consults
  `FallbackEnabled`.** This is the surface where "ack без проверки + alert"
  (`configuration.md:65`) is the documented intent (the §6.5 restart-decision-
  tree, LIC-TASK-040). On a Redis transport error:
  - `FallbackEnabled == true`: return
    `(IdempotencyAbsent, alreadyExists=false, err=nil)` — the
    "proceed/ack without dedup" signal (040 then ACKs and processes,
    accepting the rare duplicate). Emit BOTH `Lookup(lookupFallbackDB)` AND
    `Fallback()` (observability.md:157-158). Log **ERROR**
    ("idempotency fallback: Redis unreachable, proceeding WITHOUT dedup
    (LIC_IDEMPOTENCY_FALLBACK_ENABLED=true)", key, err) — this is the
    "alert" (`configuration.md:65`); ERROR (not WARN) because proceeding
    without dedup is a degraded-correctness state operators must see.
  - `FallbackEnabled == false`: return
    `(IdempotencyAbsent, alreadyExists=false, err=<the kvstore/local
    transport error verbatim>)` — 040 maps it to
    `model.ErrCodeIdempotencyStoreUnavail` retryable NACK (the same mapping
    `pendingconfirmation.Manager` does, `manager.go:346`). No `fallback_db`
    metric (it did not fall back); no `Fallback()`. Log WARN
    ("idempotency: Redis unreachable, NACKing (fallback disabled)", key,
    err).

**Transport-error detection (PINNED).** A "Redis transport error" is: any
non-nil error from `RedisSeam.Eval`/`Set`/`Expire` that is NOT the benign D4
shape/parse path, i.e. `*kvstore.RedisError` (use `kvstore.IsRetryable` /
`errors.As(err,&*kvstore.RedisError)`), `context.Canceled`,
`context.DeadlineExceeded` (these pass through RAW per `kvstore/errors.go:90`),
or the D4 `errEvalShape`. `kvstore.ErrKeyNotFound` from `Get` is NOT a
transport error — it is the absent signal (D-Get below). `context.Canceled` /
`DeadlineExceeded` on `CheckAndAcquire` is treated as a transport error for
fallback purposes (it still went through `FallbackEnabled`): if the caller's
ctx died, fallback-enabled still returns proceed/nil (040 will observe its own
ctx); fallback-disabled returns the raw context error (040 NACKs). This keeps
the codebase-wide "context errors pass through raw" convention
(`kvstore/errors.go:78-81`) intact at the Guard boundary — the Guard does NOT
wrap context errors in a local type.

**Why.** This is the only reconciliation that (a) does NOT regress the
already-shipped `pendingconfirmation` `SetNX` behaviour, (b) still delivers the
documented `configuration.md:65` fallback on the surface 040 will actually use
for the §6.5 tree, and (c) emits the exact `observability.md:157-158` metric
pair only where a fallback truly happened. It mirrors the consumer R1
split-ownership pattern: the frozen primitive stays minimal-correct; the
richer 040-facing surface carries the policy.

### R2 — Task names `CheckAndAcquire`/`StartHeartbeat`/`SetCompleted(key)`/`SetPaused(key)`; the frozen port names `SetNX`/`ExtendTTL`/`SetCompleted(ctx,key,ttl)`/`SetPaused(ctx,key,ttl)`

**Doc — conflict.** tasks.json LIC-TASK-038 acceptance verbatim says
`CheckAndAcquire(ctx,key)→(status,alreadyExists,err)`, `StartHeartbeat(ctx,key)`,
`SetCompleted(key)` SET=COMPLETED EX 24h, `SetPaused(key)` SET=PAUSED EX 25h —
i.e. no `ttl` params and different method names. The FROZEN, ALREADY-CONSUMED
`port.IdempotencyStorePort` (`idempotency.go:42-74`) names them `SetNX`,
`ExtendTTL`, `SetCompleted(ctx,key,ttl)`, `SetPaused(ctx,key,ttl)` — all with
explicit per-call `ttl`.

**Impl (binding).** The **frozen port is the SSOT** (the project's documented
"task acceptance is a non-exhaustive checklist, frozen contracts win" discipline
— `pendingconfirmation` R5, recorded in `pendingconfirmation/CLAUDE.md`). One
exported `*Guard` (D3):
- Implements the 5 frozen signatures EXACTLY (`SetNX`, `Get`, `ExtendTTL`,
  `SetCompleted(ctx,key,ttl)`, `SetPaused(ctx,key,ttl)`) — `SetCompleted`/
  `SetPaused` keep the `ttl` param (the task's "EX 24h"/"EX 25h" are the
  *values 047/040 pass*, not adapter constants — R3); `pendingconfirmation`
  already calls `idem.SetCompleted(ctx,key,m.cfg.CompletedTTL)` /
  `idem.SetPaused(ctx,key,m.cfg.PendingStateTTL)` (`manager.go:251,438,442,622`)
  so the signatures CANNOT change.
- Adds `CheckAndAcquire(ctx,key,ttl)` (D3.2) and `StartHeartbeat(ctx,key,ttl)`
  (D6) as the task's ergonomic surface. `CheckAndAcquire` GAINS a `ttl` param
  the task wording omits: the frozen `SetNX` requires per-call ttl
  (`idempotency.go:48`), TTLs are per-call/per-key (R3), and 040 already holds
  the ProcessingTTL from config — passing it is correct and unavoidable. The
  task's "no ttl" wording is the non-binding shorthand; the binding signatures
  are D3.2/D6.
- "ExtendTTL vs StartHeartbeat": the task's `StartHeartbeat` is the goroutine
  *driver* (D6); the frozen `ExtendTTL` is the per-tick *primitive* it calls.
  Both exist; `StartHeartbeat` is built ON `ExtendTTL`. Not a conflict — a
  layering: 040 calls `StartHeartbeat`; `StartHeartbeat`'s loop calls the
  frozen `ExtendTTL` (`idempotency.go:55-61` "Called by the heartbeat
  goroutine every … 30s").

**Why.** `port.IdempotencyStorePort` is frozen AND already consumed by
`pendingconfirmation.Manager` (LIC-TASK-037, shipped — commit ed05a92). Any
signature change is a frozen-contract break that would not compile against the
existing consumer + the `port/idempotency_test.go` `var _` assertion. The task
acceptance is a non-exhaustive intent checklist; the frozen port + its existing
consumer are the binding SSOT. The ergonomic methods are ADDITIVE (new
surface, no existing consumer) so they take the task-named shape.

### R3 — `LIC_IDEMPOTENCY_PROCESSING_TTL` is 150s (SSOT), NOT the §8.2:1020 "90s"; and the adapter hardcodes NO TTL

**Doc — conflict.** `high-architecture.md:563` + `configuration.md:63`:
`LIC_IDEMPOTENCY_PROCESSING_TTL=150s` (with the documented rationale: must
exceed `LIC_JOB_TIMEOUT(90s)+LIC_DM_PERSIST_CONFIRM_TIMEOUT(30s)+buffer(30s)`).
`config/idempotency.go:23` codifies `150*time.Second` as the default. BUT
`high-architecture.md:1020` (scenario §8.2) says "Idempotency Guard SETNX
`lic-trigger:{version_id} = PROCESSING (90s TTL)`".

**Impl (binding).** The §8.2:1020 "90s" is a **STALE pre-heartbeat scenario
figure** — written before the heartbeat mechanism (§6.3:559-578) raised the
PROCESSING TTL to 150s with a 30s `EXPIRE` extender. `high-architecture.md:563`
+ `configuration.md:62-64` + `config/idempotency.go:23` (150s) are the SSOT.
**This reconciliation is moot for the adapter implementation:** the Guard
**hardcodes NO TTL** — every TTL (PROCESSING 150s, COMPLETED 24h, PAUSED 25h,
user-confirmed 90s) is a **per-call method parameter** on the frozen port
(`idempotency.go:48,61,66,73`), supplied by the caller
(`pendingconfirmation.Manager` from its own `Config`, `manager.go:251,325,438`;
040 from `config.IdempotencyConfig`). The adapter's only intrinsic time knob is
`Config.HeartbeatInterval` (D9). The stale figure cannot reach the adapter
because the adapter never names a TTL constant. Recorded so (a) a reviewer does
not "fix" the adapter to a 90s/150s literal, and (b) a future architecture-
consistency pass corrects `high-architecture.md:1020` to 150s (architecture-
team-owned; out of scope for this code task).

### R4 — Adapter returns kvstore/local-typed errors, NEVER a `model.ErrorCode`

**Doc.** The task mentions `model.ErrCodeIdempotencyStoreUnavail`. The
`pendingconfirmation.Manager` maps a non-`ErrIdempotencyKeyExists` `SetNX`
error to `model.ErrCodeIdempotencyStoreUnavail` (`manager.go:346`).
`error-handling.md:127` lists `IDEMPOTENCY_STORE_UNAVAILABLE`.

**Impl (binding).** The Guard NEVER constructs or returns a
`model.ErrorCode` / `*model.DomainError`. It returns: `nil`; the frozen
`port.ErrIdempotencyKeyExists` (only from `SetNX` on present-key, D3.1.5);
`port`-local sentinels `ErrIdempotencyKeyVanished` (D6.1); the package-local
`errEvalShape` (D4); or the kvstore error verbatim
(`*kvstore.RedisError`, `kvstore.ErrKeyNotFound`, raw context errors). The
**Manager / 040** (the application layer) owns the translation to
`model.ErrCodeIdempotencyStoreUnavail` — the Guard MUST NOT import
`internal/domain/model` (D10 forbidden set). This is the kvstore/broker
discipline verbatim (`kvstore/errors.go:13-25`: "Redis/infra failures are
never Orchestrator-published … a model.ErrorCode would both break that
invariant and be semantically wrong"; the DP-kvstore `port.ErrCodeStorageFailed`
mapping is deliberately NOT copied — `kvstore/CLAUDE.md:38-40`).

**Why.** Preserves the layering (`model.errorCatalog` SSOT panic invariant +
"infra errors never Orch-published"). The existing `pendingconfirmation`
`errors.Is(err, port.ErrIdempotencyKeyExists)` + "else map to
IdempotencyStoreUnavail" branch (`manager.go:327,346`) works ONLY if the Guard
returns a plain non-`ErrIdempotencyKeyExists` error on transport failure —
exactly what R1's frozen-`SetNX` contract guarantees.

### R5 — `Get` miss maps to `IdempotencyAbsent` with NO error (frozen contract)

**Doc.** `idempotency.go:50-53`: "Get … Returns IdempotencyAbsent without error
when the key is missing — adapters MUST translate Redis nil-reply into
IdempotencyAbsent, not a Go-level error." `kvstore.Get` returns
`("", kvstore.ErrKeyNotFound)` on miss (`ops.go:13,24`).

**Impl (binding).** `Guard.Get`: call `RedisSeam.Get(ctx,key)`.
- `err == nil` ⇒ `(parseStatus(value), nil)` (D5 defensive parse).
- `errors.Is(err, kvstore.ErrKeyNotFound)` ⇒ `(port.IdempotencyAbsent, nil)`
  (the frozen contract; `kvstore/CLAUDE.md:41-44` explicitly: "the LIC-TASK-038
  adapter translates a miss into IdempotencyAbsent (no error) … via a clean
  errors.Is(err, kvstore.ErrKeyNotFound)").
- any other error (transport/context) ⇒ `(port.IdempotencyAbsent, err)` — the
  kvstore error verbatim (R4). `Get` does NOT consult `FallbackEnabled` (R1 —
  only `CheckAndAcquire` does); a transport error on `Get` surfaces to the
  caller which decides (040's PAUSED→read-pending branch will NACK-retry).
- NO metric on `Get` (D8).

**Why.** A frozen, explicit godoc contract (`idempotency.go:50-53`) with a
documented kvstore primitive (`ErrKeyNotFound` sentinel). Recorded so the
implementer does NOT emit a `lookup` metric for `Get` (D8) and does NOT route a
`Get` miss through fallback (it is a normal "not found", not an infra failure —
`kvstore/errors.go:30-34`).

---

## PART C — REVIEWER-GATE CHECKLIST (objective pass/fail; parent runs BEFORE code-reviewer)

Run from `LegalIntelligenceCore/development`:

1. **Builds:** `go build ./internal/ingress/idempotency/...` exits 0.
2. **Vet clean:** `go vet ./internal/ingress/idempotency/...` exits 0.
3. **Tests + race:** `go test -race ./internal/ingress/idempotency/...`
   exits 0 (test_step 1).
4. **Frozen port satisfied:** the package (or 047 wiring) compiles
   `var _ port.IdempotencyStorePort = (*idempotency.Guard)(nil)`; a test in
   `guard_test.go` includes this assertion (D13) and it compiles — the 5
   signatures match `idempotency.go:42-74` byte-for-byte.
5. **`pendingconfirmation` NOT regressed:**
   `go build ./internal/application/pendingconfirmation/...` and
   `go test ./internal/application/pendingconfirmation/...` still exit 0 (the
   existing `SetNX`/`SetCompleted`/`SetPaused` consumer compiles & passes
   against the unchanged frozen port — R2).
6. **test_step coverage — named tests exist and pass:**
   - `TestGuard_SetNX_AbsentAcquiresProcessing` (absent ⇒
     `(IdempotencyAbsent,nil)` + key now `PROCESSING` + `{result="new"}`).
   - `TestGuard_SetNX_RepeatReturnsInProgress` (test_step 2: 2nd `SetNX` on the
     same key ⇒ `(IdempotencyProcessing, ErrIdempotencyKeyExists)` +
     `{result="in_progress"}` — "SETNX repeat → in_progress").
   - `TestGuard_SetNX_PresentCompleted` (key=COMPLETED ⇒
     `(IdempotencyCompleted, ErrIdempotencyKeyExists)` +
     `{result="completed"}`).
   - `TestGuard_SetNX_PresentPaused` (key=PAUSED ⇒
     `(IdempotencyPaused, ErrIdempotencyKeyExists)` +
     `{result="in_progress"}` — PAUSED maps to in_progress, D8).
   - `TestGuard_Heartbeat_ExtendsTTL` (test_step 3: drive 1 injected tick ⇒
     `Expire(key,ttl)` recorded + key deadline advanced).
   - `TestGuard_Heartbeat_StopThenTTLExpiresNaturally` (test_step 4: `stop()`,
     advance fake clock past deadline ⇒ `Get`→`IdempotencyAbsent`).
7. **Atomic Lua, no TOCTOU (D4):** a test asserts `SetNX`/`CheckAndAcquire`
   issue exactly ONE `RedisSeam.Eval` call (recorded) with
   `keys==[]string{key}`, `args==[]any{"PROCESSING", <ttlSeconds int>}`, and
   NO separate `Get` round-trip on the present path; the decoder maps
   `[]interface{}{int64(1),""}`⇒acquired and `[]interface{}{int64(0),"PAUSED"}`
   ⇒present/PAUSED; an unexpected shape / Lua-nil ⇒ `errEvalShape`
   (transport-class).
8. **Heartbeat lifecycle (D6) — all 3 stop conditions + no leak:**
   - ctx-cancel ⇒ goroutine returns (a `done` chan the goroutine closes is
     closed within a bounded select; ticker `Stop()` called).
   - `stop()` ⇒ goroutine returns; `stop()` called twice does NOT panic
     (`sync.Once`).
   - `ExtendTTL`→`ErrIdempotencyKeyVanished` (fake `Expire`→false) ⇒ WARN
     logged + goroutine returns.
   - transient `ExtendTTL` error ⇒ WARN logged + goroutine **continues**
     (next tick still fires).
   - `-race` clean; no goroutine outlives the test (asserted via the `done`
     channel, not `time.Sleep`).
9. **Defensive unknown value (D5):** a test seeds the fake with
   `key="GARBAGE"` then `SetNX`/`CheckAndAcquire` ⇒ status
   `IdempotencyProcessing` (NEVER `IdempotencyAbsent`); `CheckAndAcquire`
   `alreadyExists==true`.
10. **`CheckAndAcquire` mapping table (D3.2):** a table test proves every row:
    absent⇒`(Absent,false,nil)`+`new`; PROCESSING⇒`(Processing,true,nil)`+
    `in_progress`; PAUSED⇒`(Paused,true,nil)`+`in_progress`;
    COMPLETED⇒`(Completed,true,nil)`+`completed`; present ⇒ `err==nil` (NOT
    `ErrIdempotencyKeyExists` — D3.2 binding split vs `SetNX`).
11. **Fallback split (R1):** with the fake forced to return a
    `*kvstore.RedisError{Retryable:true}` from `Eval`:
    - `SetNX` ALWAYS returns `(IdempotencyAbsent, <that error>)` regardless of
      `FallbackEnabled` (true AND false subtests) — error is NOT
      `ErrIdempotencyKeyExists`, NOT a `model.ErrorCode`; no `fallback_db`
      metric; no `Fallback()`.
    - `CheckAndAcquire` + `FallbackEnabled==true` ⇒
      `(IdempotencyAbsent,false,nil)` + `Lookup("fallback_db")` ×1 +
      `Fallback()` ×1 + an ERROR log line.
    - `CheckAndAcquire` + `FallbackEnabled==false` ⇒
      `(IdempotencyAbsent,false,<that error verbatim>)` + NO `fallback_db`
      metric + NO `Fallback()` + a WARN log line.
    - context-error subtest: `Eval` returns `context.DeadlineExceeded`;
      fallback-disabled `CheckAndAcquire` returns it RAW (not wrapped — R1).
12. **`Get` contract (R5):** miss (fake ⇒ `kvstore.ErrKeyNotFound`) ⇒
    `(IdempotencyAbsent, nil)` + NO metric; present ⇒ `(parseStatus, nil)`;
    transport error ⇒ `(IdempotencyAbsent, errVerbatim)` + NO metric + NO
    fallback consultation.
13. **`SetCompleted`/`SetPaused` (D12):** call ⇒ exactly one
    `RedisSeam.Set(key, "COMPLETED"|"PAUSED", ttl)` with the per-call ttl
    (NOT a hardcoded 24h/25h — assert the ttl passed equals the test's input,
    R3); overwrites a prior `PROCESSING` value; `Set` error returned verbatim
    (NOT a model code — R4).
14. **`ExtendTTL` (D6.1):** `Expire`→`(true,nil)` ⇒ `ExtendTTL` returns `nil`;
    `Expire`→`(false,nil)` ⇒ returns `ErrIdempotencyKeyVanished`
    (`errors.Is` true); `Expire` transport error ⇒ returns it verbatim.
15. **No model import / no hardcoded TTL (R3/R4):** grep the package non-test
    files: zero `internal/domain/model` import; zero numeric duration literals
    used as a Redis TTL (the only `time.Duration` literal allowed is none —
    TTLs are params; `Config.HeartbeatInterval` is the sole intrinsic and it
    is a field, not a literal). `errEvalShape`/`ErrIdempotencyKeyVanished` are
    plain `errors.New`.
16. **Constructor fail-fast (D2):** tests assert `NewGuard` returns a non-nil
    error (and nil `*Guard`) for: nil `redis`; `Config{HeartbeatInterval:0}`;
    and the joined error mentions each failing arg.
17. **Lookup constants (D8):** `TestLookupConstantsMatchSSOT` asserts the four
    literals are exactly `"new"/"in_progress"/"completed"/"fallback_db"` (no
    metrics import).
18. **Hermetic + gofmt (D10/D14):** `TestHermeticImports` (2-entry allowlist
    `{domain/port,infra/kvstore}`, ZERO permitted third-party, active-fail
    forbidden set incl. `go-redis`, `internal/config`,
    `internal/domain/model`, observability, `internal/application/*`,
    `internal/ingress/{consumer,router}`, prometheus/otel/miniredis) and
    `TestGofmtClean` present and green; grep non-test files for
    `go-redis`/`prometheus`/`miniredis`/`model.` ⇒ zero hits.
19. **`fakeRedis` faithfulness (D11):** the in-package fake's `Eval` honours
    the D4 observable contract (SET-NX-EX-or-return-existing + exact
    `[]interface{}{int64,string}` shape), `Get` miss ⇒ `kvstore.ErrKeyNotFound`,
    `Expire` absent ⇒ `(false,nil)`, lazy expiry driven by an injectable test
    clock (no `time.Sleep` anywhere in the suite — grep `time.Sleep` ⇒ zero).
20. **CLAUDE.md present** at `internal/ingress/idempotency/CLAUDE.md` with
    Files / API / Reconciliations (R1..R5) / Conventions (D1..D14) /
    Forward-notes sections.

Any failing item ⇒ reject and return to golang-pro with the failing item
number. Only an all-green list proceeds to code-reviewer.

---

## PART D — FORWARD NOTES (recorded; owners elsewhere)

1. **LIC-TASK-040 (Event Router, `internal/ingress/router`).** Owns the
   §6.5:628-634 restart-decision-tree, x-death retry-level escalation, the
   broker ACK/NACK decision, the PAUSED→read-`lic-pending-state` branch
   (§6.5:631), and the §6.3:633 safety-net (pending-state present but
   `lic-trigger` gone ⇒ re-`SetPaused`). 040 builds the `lic-trigger:`+
   versionID key string (the Guard is key-agnostic — D12) and CONSUMES
   `CheckAndAcquire` (the §6.5 absent→SETNX / PROCESSING→NACK / PAUSED→read /
   COMPLETED→ACK tree maps directly onto the D3.2 return table). 040 DRIVES
   `StartHeartbeat(ctx, "lic-trigger:"+vid, processingTTL)` immediately after a
   `CheckAndAcquire`→`(Absent,false,nil)` and calls the returned `stop()` on
   terminal status switch (COMPLETED/PAUSED/cleanup — §6.3:576). 040 owns the
   `model.ErrCodeIdempotencyStoreUnavail` mapping for a fallback-disabled
   transport error from `CheckAndAcquire` (R1/R4 — the Guard returns the
   kvstore error verbatim; 040 maps it, exactly as
   `pendingconfirmation.Manager` does for `SetNX`, `manager.go:346`).
2. **LIC-TASK-037 (`pendingconfirmation.Manager`) is an ALREADY-SHIPPED
   consumer (commit ed05a92).** It calls the frozen `SetNX`/`SetCompleted`/
   `SetPaused` (`manager.go:251,325,438,442,622`) and branches on
   `errors.Is(err, port.ErrIdempotencyKeyExists)` then `switch status`. The
   Guard's frozen-method contract (D3.1, R1) preserves this EXACTLY: present ⇒
   `(status, ErrIdempotencyKeyExists)`; transport-down ⇒ a plain
   non-`ErrIdempotencyKeyExists` error ⇒ Manager maps to
   `IDEMPOTENCY_STORE_UNAVAILABLE` retryable. The Guard MUST NOT regress this
   (PART C #5). 037/036 drive the PAUSED→COMPLETED lifecycle via
   `SetPaused`/`SetCompleted`; the adapter's unconditional-overwrite `Set`
   (D12) is exactly that status switch.
3. **LIC-TASK-047 (app wiring).** Constructs
   `idempotency.NewGuard(kvstoreClient, idempotency.Config{HeartbeatInterval:
   cfg.Idempotency.HeartbeatInterval, FallbackEnabled:
   cfg.Idempotency.FallbackEnabled}, idempotency.Deps{Metrics:
   metricsAdapter over *metrics.IdempotencyMetrics (Lookup→
   LookupsTotal.WithLabelValues(result).Inc(); Fallback→FallbackTotal.Inc()),
   Clock: systemClock, Logger: loggerAdapter over *logger.Logger
   (Warn/Error)})`. Asserts in the WIRING package
   `var _ port.IdempotencyStorePort = (*idempotency.Guard)(nil)` and
   `var _ idempotency.RedisSeam = (*kvstore.Client)(nil)` (D10/D13 — the
   structural satisfaction of `RedisSeam` by `*kvstore.Client` is verified
   here, NOT in the Guard package). Injects the SAME `*idempotency.Guard` as
   the `port.IdempotencyStorePort` into `pendingconfirmation.NewManager`
   (`manager.go:151` `idem` param) AND as the `CheckAndAcquire`/
   `StartHeartbeat` provider into the LIC-TASK-040 router — one Guard
   instance, two roles (the `pendingconfirmation`-Manager dual-role precedent).
4. **No `go.mod` change.** The Guard imports only stdlib + `domain/port` +
   `infra/kvstore` (D10). `go-redis`/`uuid`/`miniredis` are NOT imported (the
   primitive is the `RedisSeam`; tests use the in-package `fakeRedis` — D11).
   `go mod tidy` produces no diff for this task.
5. **Architecture-doc staleness (R3, architecture-team-owned).**
   `high-architecture.md:1020` says "PROCESSING (90s TTL)"; the SSOT is 150s
   (`:563`, `configuration.md:63`, `config/idempotency.go:23`). The adapter is
   TTL-agnostic so this never reaches code; a future architecture-consistency
   pass should correct `:1020` to 150s. Recorded, not silent.

---

## PART E — IMPLEMENTER QUICK-REFERENCE (no decisions; pure recap)

- One exported `*Guard`; `NewGuard(RedisSeam, Config, Deps)(*Guard,error)`
  fail-fast `errors.Join` (D2). `Config{HeartbeatInterval, FallbackEnabled}`
  only (D9). Hermetic: stdlib + `domain/port` + `infra/kvstore` (error
  helpers only); NO model, NO go-redis, NO config, NO observability, NO
  third-party (D10).
- `*Guard` satisfies the 5 FROZEN `port.IdempotencyStorePort` signatures
  EXACTLY (`idempotency.go:42-74`) — `SetNX`/`Get`/`ExtendTTL`/
  `SetCompleted(ctx,key,ttl)`/`SetPaused(ctx,key,ttl)` — PLUS the additive
  `CheckAndAcquire(ctx,key,ttl)→(status,alreadyExists,err)` and
  `StartHeartbeat(ctx,key,ttl)→(stop func())` (R2 reconciles the naming;
  TTLs are per-call, NEVER hardcoded — R3).
- One atomic Lua via `kvstore.Eval` does SET-NX-EX-or-return-existing (D4) —
  zero TOCTOU; decoder maps `{1,""}`⇒acquired, `{0,v}`⇒present(parseStatus),
  anything else ⇒ `errEvalShape` (transport-class). Bounded single retry on
  the keyspace-eviction Lua-nil corner (D4.1).
- `parseStatus`: known ⇒ that status; unknown/garbage non-empty ⇒
  `IdempotencyProcessing` (defensive — never `Absent`, never double-analysis,
  D5). Absent only via `acquired==true` / `Get` `ErrKeyNotFound` (R5).
- `SetNX` present ⇒ `(status, port.ErrIdempotencyKeyExists)` (frozen);
  transport-down ⇒ `(Absent, kvstoreErrVerbatim)` ALWAYS, ignores
  `FallbackEnabled` (R1, preserves `pendingconfirmation`). `CheckAndAcquire`
  present ⇒ `(status,true,nil)` (NO `ErrIdempotencyKeyExists` — D3.2);
  transport-down ⇒ `FallbackEnabled` ? `(Absent,false,nil)`+fallback_db+
  Fallback()+ERROR-log : `(Absent,false,errVerbatim)`+WARN-log (R1).
- Metrics ONLY on `SetNX`/`CheckAndAcquire` (D8): new / in_progress
  (PROCESSING|PAUSED|garbage) / completed / fallback_db. `Get`/`ExtendTTL`/
  `SetCompleted`/`SetPaused`/heartbeat emit NO lookup metric.
- `StartHeartbeat`: injectable-ticker goroutine, `EXPIRE` each tick at
  `Config.HeartbeatInterval` with the per-call ttl; stop on
  {ctx.Done, idempotent `stop()`, `ExtendTTL`→`ErrIdempotencyKeyVanished`};
  transient `ExtendTTL` err ⇒ WARN + continue; `sync.Once` stop; no leak;
  WARN-only logs, NO metric (D6).
- `ExtendTTL`: `Expire`→true⇒nil; →false⇒`ErrIdempotencyKeyVanished`; err⇒
  verbatim (D6.1). `SetCompleted`/`SetPaused`: unconditional
  `Set(key,"COMPLETED"|"PAUSED",ttl)`; err verbatim (D12). `Get`: miss⇒
  `(Absent,nil)`; present⇒`(parseStatus,nil)`; err⇒`(Absent,errVerbatim)`
  (R5). NEVER a `model.ErrorCode` anywhere (R4).
- Tests: in-package faithful `fakeRedis` (D11 — the miniredis-absent recorded
  deviation), injected ticker, NO `time.Sleep`, `-race` clean; the PART C
  suite + hermetic + gofmt self-checks.
