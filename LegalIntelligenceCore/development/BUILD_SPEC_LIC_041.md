# BUILD SPEC — LIC-TASK-041 DM Artifact Awaiter + DM Confirmation Awaiter (AUTHORITATIVE)

**Status:** binding. The golang-pro implementer follows this verbatim and
makes **no further architecture decisions**. Every non-obvious ground-truth
claim is cited as `file:line`. Scope is strictly LIC-TASK-041; everything
tagged "039-owned" / "040-owned" / "047-owned" is OUT OF SCOPE and
forward-noted.

- Package: `internal/application/dmawaiter`
- Module: `contractpro/legal-intelligence-core`, Go 1.26.1
- Dev root: `LegalIntelligenceCore/development`
- Output exported types: `*ArtifactAwaiter`, `*ConfirmationAwaiter`
- Constructors: `NewArtifactAwaiter`, `NewConfirmationAwaiter`
  (`feedback_constructors.md`)

This package is the **in-process correlation registry** wired between the
broker consumer / event router (LIC-TASK-039 / LIC-TASK-040 — inbound
side) and the pipeline orchestrator (LIC-TASK-036 — `Register/Await/Cancel`
side). It IS hermetic against `internal/infra/*` (no broker, no kvstore,
no metrics-concrete, no logger-concrete) and against
`internal/application/pipeline` / `internal/application/pendingconfirmation`
/ `internal/ingress/*` (no inverted-dependency import — Router-side wiring
asserts the `ArtifactsAwaiterDeliverer` / `PersistConfirmationDeliverer`
seam satisfaction in the LIC-TASK-047 wiring package, NOT here).

Each awaiter type satisfies THREE structural roles in one struct:

1. The domain port (`port.ArtifactsAwaiterPort` / `port.PersistConfirmationAwaiterPort`) — `Register / Await / Cancel`. Orchestrator-side.
2. The inbound handler interface (`port.ArtifactsProvidedHandler` / `port.PersistConfirmationHandler`) — `HandleArtifactsProvided` / `HandlePersisted` / `HandlePersistFailed`. LIC-TASK-039 subscription target.
3. The Router-side delivery seam (`router.ArtifactsAwaiterDeliverer` / `router.PersistConfirmationDeliverer`) — `Deliver(key, payload) error`. LIC-TASK-040 calls this.

All three surfaces dispatch through the SAME in-process channel registry;
the three method shapes are thin adapters over one private `deliver(key, T)`
helper per awaiter. The wiring concretely uses (2) when the consumer's
router dispatches by typed DTO (the current frozen path — `seams.go` in the
consumer); (3) is kept for the Router seam already shipped at 040 so 040
remains untouched. Both adapters call into the same dispatch path so
correctness is single-sourced (D6).

---

## 0. Verified ground truth (re-confirmed by reading source)

| Claim | Evidence |
|---|---|
| `port.ArtifactsAwaiterPort` = `Register(correlationID) (<-chan ArtifactsProvided, error)` + `Await(ctx, correlationID) (ArtifactsProvided, error)` + `Cancel(correlationID)`; Register's channel "receives exactly one ArtifactsProvided (or is closed on Cancel)"; "Register MUST be called BEFORE the request is published — otherwise the response may arrive first and find no registry slot"; "Each call must be paired with Cancel on completion (success or timeout) so the registry stays bounded" | `internal/domain/port/dm.go:80-96` |
| `port.PersistConfirmationAwaiterPort` = `Register(jobID) (<-chan PersistConfirmation, error)` + `Await(ctx, jobID) (PersistConfirmation, error)` + `Cancel(jobID)` | `internal/domain/port/dm.go:106-120` |
| `port.ErrAwaitTimeout = errors.New("await timeout")` — sentinel, NOT a DomainError; "the orchestrator translates it into a DomainError with the appropriate error_code (DM_ARTIFACTS_TIMEOUT or DM_PERSIST_TIMEOUT)" | `internal/domain/port/dm.go:59-63` |
| `port.ErrDuplicateRegistration = errors.New("awaiter: duplicate registration")` — "Duplicates would silently shadow each other; the awaiter rejects them explicitly so the caller can log or DLQ the conflict" | `internal/domain/port/dm.go:65-70` |
| `port.PersistConfirmation` = `{Success *LegalAnalysisArtifactsPersisted, Failure *LegalAnalysisArtifactsPersistFailed}` discriminated union; `NewPersistConfirmationSuccess(p)` / `NewPersistConfirmationFailure(p)` panic on nil; `IsSuccess()` / `IsFailure()` return false on both-set / zero-value | `internal/domain/port/dm.go:122-173` |
| `port.ArtifactsProvidedHandler.HandleArtifactsProvided(ctx, evt) error` (consumer-facing single-topic handler) | `internal/domain/port/inbound.go:30-33` |
| `port.PersistConfirmationHandler.HandlePersisted(ctx, evt) error` + `HandlePersistFailed(ctx, evt) error` (consumer-facing two-method handler) | `internal/domain/port/inbound.go:39-42` |
| Inbound DTOs: `port.ArtifactsProvided{CorrelationID, Timestamp, JobID, DocumentID, VersionID, Artifacts map[ArtifactType]json.RawMessage, MissingTypes []ArtifactType, ErrorCode, ErrorMessage}`; `port.LegalAnalysisArtifactsPersisted{CorrelationID, Timestamp, JobID, DocumentID}`; `port.LegalAnalysisArtifactsPersistFailed{CorrelationID, Timestamp, JobID, DocumentID, ErrorCode, ErrorMessage, IsRetryable}` | `internal/domain/port/events.go:81-116` |
| Frozen error catalog rows: `ErrCodeDMArtifactsTimeout` (retryable=true, RU non-empty), `ErrCodeDMArtifactsMissing` (retryable=true, RU non-empty), `ErrCodeDMPersistFailed` (retryable=true default — caller overrides on `IsRetryable==false`), `ErrCodeDMPersistTimeout` (retryable=true, RU non-empty) | `internal/domain/model/error_codes.go:126-148` |
| Orchestrator pattern: `subCtx, subCancel := context.WithTimeout(awCtx, o.cfg.DMRequestTimeout); defer subCancel(); prov, awErr := o.artAwait.Await(subCtx, curCorr); if errors.Is(awErr, port.ErrAwaitTimeout) { return ... NewDomainError(ErrCodeDMArtifactsTimeout, ...).WithCause(awErr) }` — **Await is wrapped in a per-call WithTimeout owned by the caller; the awaiter's own internal TTL is a SAFETY NET, not the primary deadline** | `internal/application/pipeline/orchestrator.go:820-832` |
| Pipeline-side cleanup is "Cancel on defer" — `artAwait.Cancel(curCorr)` / `artAwait.Cancel(parCorr)` registered on Run's defer chain | `internal/application/pipeline/CLAUDE.md` "Defer/cleanup LIFO" + orchestrator.go register sites |
| LIC-TASK-039 (consumer) dispatches by typed DTO via `consumer.EventRouter` 6 `Route*` methods — the consumer never calls `port.ArtifactsProvidedHandler` directly, the *Router* (`internal/ingress/router`) does. LIC-TASK-039 acceptance: "registered against the 4 topics: `dm.responses.artifacts-provided` ⇒ `ArtifactAwaiter.HandleArtifactsProvided`; `lic-artifacts-persisted` ⇒ `ConfirmationAwaiter.HandlePersisted`; `lic-artifacts-persist-failed` ⇒ `ConfirmationAwaiter.HandlePersistFailed`" — this is the LIC-TASK-039 subscription target shape (verified via prompt) | tasks.json LIC-TASK-039; `internal/ingress/consumer/seams.go:42-78`; prompt §"Forward notes to LIC-TASK-047" |
| Router-side delivery seam (already shipped at 040): `router.ArtifactsAwaiterDeliverer.Deliver(correlationID, evt) error` + `router.PersistConfirmationDeliverer.Deliver(jobID, conf) error` — the awaiter's concrete type MUST satisfy BOTH the inbound port handlers AND the Router seam | `internal/ingress/router/seams.go:74-97` |
| DM metrics (already shipped): `metrics.DMMetrics.RequestDurationSeconds *HistogramVec` labelled `{op}` + `RequestOutcomeTotal *CounterVec` labelled `{op,outcome}`; metric names `lic_dm_request_duration_seconds` / `lic_dm_request_outcome_total` | `internal/infra/observability/metrics/dm.go:9-50` |
| `observability.md` §3.5 (verified via prompt + DM bucket comment): op values are EXACTLY `get_artifacts` / `persist_artifacts`; outcomes are EXACTLY `success` / `timeout` / `persist_failed` / `missing` | prompt; `internal/infra/observability/metrics/buckets.go:45` ("LIC_DM_REQUEST_TIMEOUT=30s is the spec hard ceiling") |
| Config env vars (already shipped): `LIC_DM_REQUEST_TIMEOUT` default 30s (`config.PipelineConfig.DMRequestTimeout`), `LIC_DM_PERSIST_CONFIRM_TIMEOUT` default 30s (`config.PipelineConfig.DMPersistConfirmTimeout`) — these are validated >0 at startup | `internal/config/pipeline.go:13-39` |
| Precedent style (037 / 038 / 039 / 040 build-spec format) | `internal/ingress/router/BUILD_SPEC_LIC_040.md`; `internal/ingress/consumer/BUILD_SPEC_LIC_039.md`; `internal/ingress/idempotency/BUILD_SPEC_LIC_038.md`; `internal/application/pendingconfirmation/CLAUDE.md` (D-/R-style spec) |
| Constructor convention: `NewTypeName` (`feedback_constructors.md`) — NOT `New` | `internal/application/pendingconfirmation/manager.go:147` (`NewManager`); `internal/ingress/router/router.go` (`NewRouter`); `internal/ingress/consumer/consumer.go` (`NewConsumer`) |
| Local Config + Deps optional-with-noop pattern (the universal `internal/application/*` precedent) | `internal/application/pendingconfirmation/{deps.go,seams.go}`; `internal/application/pipeline/{deps.go,seams.go}` |
| Hermetic allowlist for `internal/application/*` (the universal invariant) | `internal/application/pendingconfirmation/internal_test.go` (allowlist EXACTLY `{model,port}`, active-fail forbidden set) |
| `gofmt`-self-check via `go/format` (sandbox blocks `go fmt`) | `internal/application/pendingconfirmation/internal_test.go:98-120` |

---

## PART A — BINDING DECISIONS (D1..D20)

### D1 — Package & file layout

`internal/application/dmawaiter/` (new package `dmawaiter`):

| File | Purpose (one line) |
|---|---|
| `awaiter.go` | Package doc (hermetic statement; D1..D20 attribution); the SHARED key-prefix constants (none required — keys are opaque); the two `Config` value types (`ArtifactConfig`, `ConfirmationConfig`) + their `validate()` (D8); the two structs (`ArtifactAwaiter` / `ConfirmationAwaiter`) + the SHARED private `slot[T]` parametric generic struct (D2/D4) — Go 1.26 generics are sanctioned per module go.mod (verify with `head go.mod`); the two `NewXxxAwaiter` constructors (`errors.Join` fail-fast — D9); the SIX exported method signatures (`Register`, `Await`, `Cancel`, plus the handler/deliverer wrappers); the private `deliver[T]` core dispatcher + `classifyArtifactsOutcome` / `classifyConfirmationOutcome` helpers (D11); the `op` constants (`opGetArtifacts` / `opPersistArtifacts`) + the outcome constants (`outcomeSuccess` / `outcomeTimeout` / `outcomePersistFailed` / `outcomeMissing`) (D11). |
| `seams.go` | Awaiter-local seam interfaces + zero-dependency noop defaults: `Metrics`+`noopMetrics` (D11/D12, one method `RecordOutcome(op,outcome,seconds)`), `Clock`+`systemClock` (D14, 1-method), `Logger`+`noopLogger` (D15, Warn/Error only — no Info). `var _ Seam = noop{}` assertion after each pair. NO `PipelineResumer`-style required seams: the dispatch path's only required collaborators are the per-awaiter `Config` (positional) — telemetry/clock/logger are all noop-defaulted via `Deps`. |
| `deps.go` | `Deps{Metrics, Clock, Logger}` optional-with-noop bundle + `withDefaults()`. Used by BOTH constructors (one shared Deps shape — D10). |
| `awaiter_test.go` | Behavioural suite (PART C — pins T1..T22) with in-package fakes for every seam; `-race` clean; deterministic via fake clock + ZERO real-time sleeps except the **one** real-timer pin (T15) that scales to <500ms in `-short`. |
| `internal_test.go` | `TestHermeticImports` (allowlist EXACTLY `{model,port}` — active-fail forbidden set per D17) + `TestGofmtClean` (`go/format`; the sandbox blocks `go fmt`). |
| `CLAUDE.md` | Package guide mirroring `pendingconfirmation/CLAUDE.md` shape (Files / API / Reconciliations / Conventions / Forward notes). Populated by golang-pro after implementation; this build-spec is the SSOT for the content. |

No other files. No subpackages. The generic `slot[T]` lives in `awaiter.go`
alongside both awaiter types (`ArtifactAwaiter` / `ConfirmationAwaiter`)
because they share the dispatch logic — splitting into two files would
duplicate the registry pattern (the F-3 DRY discipline).

### D2 — Two distinct types, one shared dispatch (`slot[T any]` generic)

**Two structs, not one.** `ArtifactAwaiter` and `ConfirmationAwaiter` are
SEPARATE exported types — they correspond to two distinct domain ports
with two distinct key spaces (`correlation_id` vs `job_id`), two distinct
payload types, two distinct metric ops, and two distinct outcome
classification rules. Merging them via a generic `Awaiter[K,T]` is
**rejected**: the 4 outcome classifications differ (artifact: `success`
vs `missing` requires inspecting two fields of `ArtifactsProvided`;
confirmation: `success` vs `persist_failed` requires inspecting
`PersistConfirmation.IsSuccess()`). The op label is fixed at construction
per type (D11), which would require a second type parameter or a config
field — at which point the parametric saving is zero.

**SHARED `slot[T]` generic.** The in-process registry is a
`map[string]*slot[T]` per awaiter type; the `slot` struct is parametric:

```go
// slot is the per-registration registry entry. Buffered channel cap=1 so
// deliver never blocks on a single Await/Deliver pair (D5). createdAt is
// set at Register time for the duration metric (D11).
type slot[T any] struct {
    ch        chan T    // cap=1, send-once
    createdAt time.Time // for duration metric
}
```

The `ArtifactAwaiter` holds `map[string]*slot[port.ArtifactsProvided]`;
the `ConfirmationAwaiter` holds `map[string]*slot[port.PersistConfirmation]`.
This is the minimum dedup. Both types embed a shared dispatch helper
`deliver[T](mu *sync.Mutex, reg map[string]*slot[T], key string, val T,
log Logger, op string)` (free function on the private generic — D6).

### D3 — `map[string]*slot[T]` guarded by `sync.Mutex`, NOT `sync.Map`

The registry is `map[string]*slot[T]` guarded by a single `sync.Mutex`
per awaiter instance. **Rationale:**

- The hot operations (`Register`, `Cancel`, `deliver`) all perform
  TWO map mutations atomically: presence check + insert/delete. `sync.Map`'s
  `LoadOrStore` covers Register but NOT the Cancel pattern (read slot,
  close channel, delete). Mixing `sync.Map` operations with manual
  channel-close-then-delete requires a second mutex anyway — net loss vs
  the simpler `sync.Mutex + map` pattern.
- Contention is bounded by per-key independence (different correlation_ids
  never touch the same slot). The mutex window is microseconds (map
  lookup + channel non-blocking send/close + delete). Throughput is
  limited by the broker / pipeline anyway, NOT this mutex.
- `sync.Map` documentation explicitly steers callers away from
  "concurrent map of mostly-write keys" — exactly this workload's
  Register/Cancel pulse.

Hold the mutex for the SHORTEST window: read the slot pointer under the
lock, then perform the non-blocking channel send / close OUTSIDE the
lock when feasible. **Exception:** the slot's channel send/close MUST
happen UNDER the lock so a concurrent Cancel cannot close the channel
between the lookup and the send (which would `panic: send on closed
channel`). See D5 for the channel close discipline.

Concretely:

```go
type ArtifactAwaiter struct {
    cfg     ArtifactConfig
    mu      sync.Mutex
    reg     map[string]*slot[port.ArtifactsProvided]
    metrics Metrics
    clock   Clock
    log     Logger
}
```

`reg` is allocated by the constructor (never lazy). The struct is
goroutine-safe by construction; the mutex is the single sync primitive.

### D4 — Channel buffering: cap=1 (send-once, never block on Deliver)

`slot.ch` is allocated as `make(chan T, 1)`. **Why:**

- The single Register/Await/Deliver triple is the happy path. With cap=1
  the producer (`Deliver`/`HandleX`) does a non-blocking send via
  `select { case s.ch <- val: default: ... }` — even if the consumer
  (Await) is mid-`select` between the channel and the timer, the send
  always completes immediately.
- A duplicate Deliver (a late re-publish from DM) finds the channel
  already full and the `default` branch executes a WARN log + drop
  (D7). Without cap=1 the duplicate would block forever (or require
  buffered=N with N>1, which conceals correctness bugs by making
  duplicate-Deliver work "by accident").
- Cap=0 (unbuffered) is **rejected**: if `Deliver` is called before
  `Await` runs the receive (race-free per the port godoc "Register must
  precede the publish"), an unbuffered send blocks until Await arrives
  — but with the orchestrator's `WithTimeout` wrapper, ctx cancellation
  must release the slot, leaving the unbuffered send forever blocked
  (goroutine leak). Cap=1 is the smallest size that decouples producer
  and consumer; cap>1 carries no signal beyond "the channel is full".

The buffered channel is **send-once** by construction: the only producer
is `deliver`, which never re-fires for the same slot (Cancel removes
the slot from `reg` so a follow-up Deliver becomes a registry-miss and
drops with WARN — the duplicate semantics are at the registry level,
not the channel level).

The channel is **closed on Cancel and never closed on successful Await**
(D5). A closed channel + a previously-buffered value = the value is
still received before the close-zero-value (Go runtime guarantee). A
closed channel without a buffered value = receiver gets the zero T —
Await detects this via the cancel-during-await race (handled in D5).

### D5 — Register / Await / Cancel lifecycle (canonical)

#### Register(key)

```
Register(key string) returns (<-chan T, error):
  lock mu
    if reg[key] != nil:
      unlock
      return nil, port.ErrDuplicateRegistration
    s := &slot[T]{ch: make(chan T, 1), createdAt: clock.Now()}
    reg[key] = s
  unlock
  return s.ch, nil
```

#### Await(ctx, key)

```
Await(ctx, key string) returns (T, error):
  // Lookup phase — under lock only.
  lock mu
    s := reg[key]
  unlock
  if s == nil:
    return zeroT, port.ErrAwaitTimeout   // never Registered, never
                                          // delivered — treat as timeout
                                          // (the caller's intent is
                                          // "wait for it" and the only
                                          // reason to bypass Register is
                                          // a programming bug — D9 fail-
                                          // fast covers that at startup).
                                          // SEE NOTE BELOW.

  // Wait phase — NO lock held.
  timer := time.NewTimer(cfg.TTL)
  defer timer.Stop()

  var (
    val T
    err error
  )
  select {
  case v, ok := <-s.ch:
    if !ok {
      // Cancel closed the channel before any Deliver. Treat as timeout
      // (the caller's lifecycle "Register-then-Cancel without Await" is
      // a programming bug; defensive return of ErrAwaitTimeout matches
      // the natural-timeout semantic — D9).
      err = port.ErrAwaitTimeout
    } else {
      val = v
    }
  case <-ctx.Done():
    err = ctx.Err()  // context.Canceled or context.DeadlineExceeded
  case <-timer.C:
    err = port.ErrAwaitTimeout
  }

  // Cleanup phase — under lock, idempotent. Always remove the slot on
  // any Await exit path (success, timeout, ctx-cancel, closed-channel).
  duration := clock.Now().Sub(s.createdAt)
  lock mu
    // Only delete if reg[key] is OUR slot (defensive — a parallel
    // Cancel may have already removed it, in which case the lookup
    // returns nil and we no-op).
    if reg[key] == s {
      delete(reg, key)
    }
  unlock

  // Outcome classification + metrics — OUTSIDE the lock.
  outcome := classifyArtifactsOutcome(val, err)   // OR ConfirmationOutcome
  metrics.RecordOutcome(op, outcome, duration.Seconds())

  return val, err
```

**NOTE on `s == nil` after Lookup.** The current port godoc binds the
caller to Register-before-publish. A second Await on a key that was
never Registered (or was already Cancel'd) is a defensive case. Two
options:

- **(a)** Return `port.ErrAwaitTimeout` immediately. Simple, safe,
  matches the natural-timeout user error.
- **(b)** Return a typed `errors.New("dmawaiter: Await on unregistered
  key")`.

**BINDING: option (a).** Rationale: the port godoc requires Register
before Await, so this branch is unreachable in compliant callers; the
defensive return is for chaos-tests / wiring bugs. ErrAwaitTimeout
preserves the caller's existing `errors.Is(err, port.ErrAwaitTimeout)`
branch (orchestrator.go:826) — no new error class to handle.

#### Cancel(key)

```
Cancel(key string):
  // Idempotent. Safe to call multiple times. Safe to call after Await
  // has already returned (and self-cleaned).
  lock mu
    s := reg[key]
    if s == nil:
      unlock
      return  // already cancelled / already cleaned by Await — noop.
    delete(reg, key)
    // Close the channel UNDER the lock to prevent a parallel Deliver
    // from observing the slot in reg (already deleted above) and
    // sending to a closed channel (D6 — Deliver takes the lock and
    // re-reads reg before sending).
    close(s.ch)
  unlock
```

A close on a channel that already had a buffered value is safe: a
receiver gets the buffered value first, then on the next receive gets
zero+`!ok`. The Await select therefore observes the buffered value
when Deliver+Cancel race, and the post-Await cleanup is a no-op
(`reg[key] == s` is false).

### D6 — Deliver / HandleX core dispatcher

The SHARED private dispatch is:

```
deliver[T any](mu *sync.Mutex, reg map[string]*slot[T], key string,
                val T, log Logger, op string) error:
  mu.Lock()
  defer mu.Unlock()
  s := reg[key]
  if s == nil:
    // Registry-miss: late delivery (slot timed out / Cancel'd) or
    // never-registered. Drop silently + WARN log. Return nil — the
    // consumer ACKs the source message (a late response is not poison,
    // 040 D6 R5 precedent — internal/ingress/router/BUILD_SPEC_LIC_040.md
    // §D6).
    log.Warn(noCtx, "dmawaiter: deliver on missing slot (late / cancelled)",
      "op", op, "key", key)
    return nil
  // Non-blocking send via select-with-default. With cap=1 the send
  // succeeds on the first call and fails-into-default on a duplicate
  // (D4 / D7).
  select {
  case s.ch <- val:
    return nil
  default:
    // Duplicate Deliver — channel already full. Drop with WARN. The
    // first Deliver's value is the authoritative one (Await will
    // receive it); the second is silently dropped. Return nil so the
    // consumer ACKs the duplicate (D7).
    log.Warn(noCtx, "dmawaiter: duplicate deliver dropped (channel full)",
      "op", op, "key", key)
    return nil
  }
```

`noCtx` is `context.Background()` — the Logger's WARN log is hot-path
metadata; passing a nil ctx would crash a structured logger that calls
`ctx.Value`. The two HandleX / Deliver methods are thin adapters that
call `deliver` with the right `(reg, key, val, op)` tuple.

**Exposed method signatures:**

```go
// --- ArtifactAwaiter ---

// port.ArtifactsAwaiterPort
func (a *ArtifactAwaiter) Register(correlationID string) (<-chan port.ArtifactsProvided, error)
func (a *ArtifactAwaiter) Await(ctx context.Context, correlationID string) (port.ArtifactsProvided, error)
func (a *ArtifactAwaiter) Cancel(correlationID string)

// port.ArtifactsProvidedHandler (LIC-TASK-039 subscription target)
func (a *ArtifactAwaiter) HandleArtifactsProvided(ctx context.Context, evt port.ArtifactsProvided) error {
    return deliver(&a.mu, a.reg, evt.CorrelationID, evt, a.log, opGetArtifacts)
}

// router.ArtifactsAwaiterDeliverer (LIC-TASK-040 already-shipped seam)
func (a *ArtifactAwaiter) Deliver(correlationID string, evt port.ArtifactsProvided) error {
    return deliver(&a.mu, a.reg, correlationID, evt, a.log, opGetArtifacts)
}

// --- ConfirmationAwaiter ---

// port.PersistConfirmationAwaiterPort
func (c *ConfirmationAwaiter) Register(jobID string) (<-chan port.PersistConfirmation, error)
func (c *ConfirmationAwaiter) Await(ctx context.Context, jobID string) (port.PersistConfirmation, error)
func (c *ConfirmationAwaiter) Cancel(jobID string)

// port.PersistConfirmationHandler (LIC-TASK-039 subscription target)
func (c *ConfirmationAwaiter) HandlePersisted(ctx context.Context, evt port.LegalAnalysisArtifactsPersisted) error {
    conf := port.NewPersistConfirmationSuccess(&evt)  // safe: evt is a value, &evt is non-nil
    return deliver(&c.mu, c.reg, evt.JobID, conf, c.log, opPersistArtifacts)
}
func (c *ConfirmationAwaiter) HandlePersistFailed(ctx context.Context, evt port.LegalAnalysisArtifactsPersistFailed) error {
    conf := port.NewPersistConfirmationFailure(&evt)
    return deliver(&c.mu, c.reg, evt.JobID, conf, c.log, opPersistArtifacts)
}

// router.PersistConfirmationDeliverer (LIC-TASK-040 already-shipped seam)
// Takes the fully-built envelope so the Router constructs it once
// (router.go calls port.NewPersistConfirmationSuccess/Failure itself).
func (c *ConfirmationAwaiter) Deliver(jobID string, conf port.PersistConfirmation) error {
    return deliver(&c.mu, c.reg, jobID, conf, c.log, opPersistArtifacts)
}
```

**IMPORTANT** about `&evt` capture inside `HandlePersisted` /
`HandlePersistFailed`: `evt` is a value parameter, so `&evt` points to
the local copy. The `NewPersistConfirmation*` constructors panic on nil,
which `&evt` is structurally guaranteed not to be — so the panic
precondition is satisfied without further defensive code. `&evt` IS a
new pointer per call; the previous call's `&evt` is unreachable after
return, so there is no cross-call aliasing concern.

### D7 — Duplicate Deliver semantics (silent drop + WARN)

A second Deliver for the same key while the first value is still
buffered is dropped on the floor with a WARN log (D6 — `default` branch
of the non-blocking send). The dispatcher returns nil so the consumer
ACKs the duplicate (a duplicate is not poison; broker at-least-once
delivery makes duplicates a normal expected event).

The semantics are NOT "second-overwrites-first" because:

- The Await receiver may already be holding the first value (mid-
  classification). Overwriting would race with `classifyOutcome`.
- Even if Await hasn't received yet, the first value is the
  authoritative DM response per its arrival order (channel send order
  matches broker dispatch order for a single queue topology).

A future SSOT could prefer "later-overwrites-earlier" for the rare DM
correction scenario, but v1 binds first-wins with WARN. This matches
the Router's silent-ACK-on-duplicate semantics (040 D6).

### D8 — `Config` per awaiter type + `validate()`

Two local `Config` value types (NO `internal/config` import — the
`pipeline.Config` / `pendingconfirmation.Config` / `router.Config`
precedent; hermeticity — D17):

```go
type ArtifactConfig struct {
    // TTL is the per-await safety-net timeout. From
    // LIC_DM_REQUEST_TIMEOUT (default 30s, config/pipeline.go:22).
    // MUST be > 0. The orchestrator-side caller (pipeline.requestAnd-
    // AwaitCurrent) ALSO wraps the Await call in its own
    // context.WithTimeout(o.cfg.DMRequestTimeout) (orchestrator.go:821);
    // the Awaiter's own TTL is the safety net for a caller that
    // bypassed WithTimeout (e.g. a future call site). They MUST be set
    // to the same value at LIC-TASK-047 wiring time so the observable
    // behaviour is deterministic (whichever fires first; with equal
    // values either path triggers ErrAwaitTimeout).
    TTL time.Duration
}

type ConfirmationConfig struct {
    // TTL is the per-await safety-net timeout. From
    // LIC_DM_PERSIST_CONFIRM_TIMEOUT (default 30s, config/pipeline.go:23).
    // MUST be > 0. Same caller-also-wraps semantics as ArtifactConfig.
    TTL time.Duration
}
```

`validate()` per type:

```go
func (c ArtifactConfig) validate() error {
    if c.TTL <= 0 {
        return errors.New("dmawaiter: ArtifactConfig.TTL must be > 0 (LIC_DM_REQUEST_TIMEOUT)")
    }
    return nil
}
// symmetric for ConfirmationConfig
```

Two separate Config types (rather than one shared `Config{TTL}`)
because the env vars differ and the per-call site at 047 wiring
threads them from distinct `config.PipelineConfig` fields — keeping
them distinct documents the binding to the right env var. Cost: ~3
lines of code duplication; benefit: zero ambiguity at the wiring layer.

### D9 — Constructor signatures (fail-fast `errors.Join`)

```go
func NewArtifactAwaiter(cfg ArtifactConfig, deps Deps) (*ArtifactAwaiter, error)
func NewConfirmationAwaiter(cfg ConfirmationConfig, deps Deps) (*ConfirmationAwaiter, error)
```

**No required collaborators beyond `Config`.** The two awaiters depend
ONLY on:

- The `Config.TTL` (validated >0)
- The three optional seams (`Metrics`/`Clock`/`Logger`) in `Deps` (each
  defaults to noop via `deps.withDefaults()`)

There are NO required positional collaborators — every external touch
is either the consumer/router dispatching INTO the awaiter (no port
needed; the awaiter holds the registry, callers invoke methods on the
awaiter directly), or the orchestrator calling Register/Await/Cancel
(again, on the awaiter directly). There is no outbound port to inject.

Fail-fast pattern (mirrors `pendingconfirmation.NewManager` —
`manager.go:147-197`, `errors.Join` of all defects at once):

```go
func NewArtifactAwaiter(cfg ArtifactConfig, deps Deps) (*ArtifactAwaiter, error) {
    var errs []error
    if err := cfg.validate(); err != nil {
        errs = append(errs, err)
    }
    if len(errs) > 0 {
        return nil, errors.Join(errs...)
    }
    d := deps.withDefaults()
    return &ArtifactAwaiter{
        cfg:     cfg,
        reg:     make(map[string]*slot[port.ArtifactsProvided]),
        metrics: d.Metrics,
        clock:   d.Clock,
        log:     d.Logger,
    }, nil
}
// symmetric for NewConfirmationAwaiter
```

Both constructors **eagerly allocate `reg`** so the first Register
under contention does not race with map creation.

### D10 — Shared `Deps{Metrics, Clock, Logger}` bundle

A single `Deps` shape is shared by both constructors (one less
parametric concept; D8's per-Config split is enough to document the
TTL-env-var binding without splitting Deps). All three fields are
optional with noop defaults via `Deps.withDefaults()` — the universal
`internal/application/*` precedent.

```go
type Deps struct {
    Metrics Metrics  // nil ⇒ noopMetrics
    Clock   Clock    // nil ⇒ systemClock (UTC wall clock)
    Logger  Logger   // nil ⇒ noopLogger
}

func (d Deps) withDefaults() Deps {
    if d.Metrics == nil { d.Metrics = noopMetrics{} }
    if d.Clock == nil   { d.Clock = systemClock{} }
    if d.Logger == nil  { d.Logger = noopLogger{} }
    return d
}
```

LIC-TASK-047 wires real `Metrics` adapter, `tracer`-attached `Clock`,
and structured `Logger` (forward note #4).

### D11 — Metrics seam + outcome classification

The seam is a **single method** `RecordOutcome(op, outcome string,
seconds float64)` — collapsing both `RequestDurationSeconds.Observe`
and `RequestOutcomeTotal.Inc` into one adapter call so the adapter
(LIC-TASK-047) writes both metrics atomically per Await completion.

```go
type Metrics interface {
    // RecordOutcome records both lic_dm_request_duration_seconds{op}
    // and lic_dm_request_outcome_total{op,outcome} for a single Await
    // completion. The adapter (LIC-TASK-047, over *metrics.DMMetrics)
    // wraps both prometheus operations.
    //
    // op is one of {opGetArtifacts, opPersistArtifacts} — see the
    // unexported constants in awaiter.go.
    // outcome is one of {outcomeSuccess, outcomeTimeout,
    // outcomePersistFailed, outcomeMissing} — observability.md §3.5.
    RecordOutcome(op, outcome string, seconds float64)
}
```

**`op` and `outcome` are STRING constants, not enums.** The metrics
adapter (047) consumes them as label values directly — same shape as
`metrics.DMMetrics.RequestOutcomeTotal.WithLabelValues(op, outcome)`.
Defining a Go enum would force a string conversion at the adapter
boundary with zero correctness benefit (the constants are validated by
the metric labels themselves — a typo fails the prometheus exemplars
test).

```go
// op label values — observability.md §3.5 (verified).
const (
    opGetArtifacts     = "get_artifacts"
    opPersistArtifacts = "persist_artifacts"
)

// outcome label values — observability.md §3.5 (verified).
const (
    outcomeSuccess       = "success"
    outcomeTimeout       = "timeout"
    outcomePersistFailed = "persist_failed"
    outcomeMissing       = "missing"
)
```

#### Outcome classification (PER AWAITER, applied at Await return)

```go
// ArtifactAwaiter — applied to (val port.ArtifactsProvided, err error).
func classifyArtifactsOutcome(val port.ArtifactsProvided, err error) string {
    if err != nil {
        if errors.Is(err, port.ErrAwaitTimeout) {
            return outcomeTimeout
        }
        // ctx-cancelled / ctx-deadline / closed-chan-on-cancel all
        // classify as timeout-equivalent for the metric: the receiving
        // pipeline goroutine has either timed out at its own
        // WithTimeout layer (orchestrator.go:821, which is precisely
        // the LIC_DM_REQUEST_TIMEOUT bound) or been actively
        // cancelled. The metric semantic is "the request did not
        // produce a usable response" — same bucket as a pure
        // ErrAwaitTimeout.
        return outcomeTimeout
    }
    // err == nil ⇒ a value arrived via the channel. Classify by
    // payload contents:
    if val.ErrorCode != "" || len(val.MissingTypes) > 0 {
        // DM responded but did not include the requested artifacts —
        // pipeline will map this to ErrCodeDMArtifactsMissing
        // (orchestrator.go:833-836).
        return outcomeMissing
    }
    return outcomeSuccess
}

// ConfirmationAwaiter — applied to (val port.PersistConfirmation, err).
func classifyConfirmationOutcome(val port.PersistConfirmation, err error) string {
    if err != nil {
        if errors.Is(err, port.ErrAwaitTimeout) {
            return outcomeTimeout
        }
        return outcomeTimeout  // same reasoning as above
    }
    if val.IsSuccess() {
        return outcomeSuccess
    }
    if val.IsFailure() {
        return outcomePersistFailed
    }
    // both-set / zero-value via NewPersistConfirmation*: impossible
    // per the constructor contract (panic on nil); the both-set case
    // would only arise from a literal-form misuse the Router does not
    // perform. Defensive fallback = outcomeMissing (no data delivered).
    return outcomeMissing
}
```

The `outcomeMissing` value is **not** used by the ConfirmationAwaiter
in any "compliant" code path; the defensive branch keeps the metric
range bounded. (observability.md §3.5 lists `missing` as a
get_artifacts outcome only; emitting it under persist_artifacts is a
deviation but bounded and is recorded as R3 of this spec.)

#### Duration timing

`duration := clock.Now().Sub(slot.createdAt)`. The clock reading at
`Register` (slot creation) is the start; the clock reading at Await
exit (any branch — success, timeout, ctx-cancel) is the end. This
matches "round-trip latency for a DM request, labelled by operation
(get_artifacts | persist_artifacts)" (`metrics/dm.go:11`).

The `metrics.RecordOutcome` call happens AFTER the lock-release of the
cleanup phase (D5) so a metric write does not stall the dispatcher.

### D12 — No background sweeper goroutine; TTL enforced lazily

The awaiter has **no background goroutine**. The TTL is enforced inside
each Await via `time.NewTimer(cfg.TTL)` (D5). No `time.AfterFunc`, no
`select{case <-time.Tick(...)}` sweeper, no per-slot watchdog goroutine.

**Why:** a sweeper would need its own lifecycle (Start/Stop), would
introduce a third concurrency axis to test (Register/Cancel/Sweep
races), and would complicate `-race`-clean proofs. The lazy TTL is
correct because every Register MUST be paired with either Await or
Cancel per the port contract — under that contract no slot is leaked.

**Slot leak under contract violation:** If a caller calls `Register`
without ever calling Await or Cancel, the slot stays in the registry
forever (until process restart). This is **documented as a contract
violation** — the orchestrator's defer chain (`pipeline.Orchestrator.Run`
LIFO defer registers `Cancel` immediately after `Register`) makes it
impossible in compliant callers.

**Test pin T16** exercises the leak-free behaviour under the documented
contract (N=100 register+await+timeout iterations leave len(reg)==0).
A leaked-by-misuse test is OUT OF SCOPE — it would test bug behaviour.

If a caller forgets Cancel without ever Await-ing, the registry slot
leaks until process restart. Late deliveries to that slot would
successfully send (channel cap=1, send succeeds once) but no receiver
ever arrives — the value is buffered indefinitely. The duration metric
is never recorded (it lives in Await's path). This is acceptable
post-restart behaviour: the SLO impact is bounded by Register call
volume between restarts, which is ≤ jobs in flight × 2 (current +
parent correlation_ids) ≤ `LIC_PIPELINE_CONCURRENCY` × 2.

### D13 — ctx propagation in Await

The Await select includes `<-ctx.Done()` and returns `ctx.Err()`
verbatim (`context.Canceled` for parent cancel, `context.DeadlineExceeded`
for an outer-WithTimeout). The caller (orchestrator) inspects via
`errors.Is(err, context.DeadlineExceeded)` if it wishes to reclassify,
but the port godoc says "the orchestrator translates it into a
DomainError with the appropriate error_code" — leaving the translation
to the caller (the awaiter does NOT wrap or re-classify into ErrAwait-
Timeout, because the two failures have different metric outcomes per
D11's classifier).

Critically, ctx-cancel during Await **still triggers the cleanup
phase** (slot removal from `reg`) so the leak invariant holds (D12).

### D14 — Clock seam (1-method)

```go
type Clock interface {
    Now() time.Time
}

type systemClock struct{}
func (systemClock) Now() time.Time { return time.Now().UTC() }

var _ Clock = systemClock{}
```

Used for two reads: slot creation timestamp (`createdAt`) and Await
exit timestamp (duration). The `pendingconfirmation.Clock` 1-method
precedent (the `pipeline.Clock` larger surface was unnecessary here).

### D15 — Logger seam (Warn/Error only)

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

**NO `Info`** (the `pipeline.Logger` precedent; `pendingconfirmation`'s
`Info` was added for the §11.2 audit-trail mandate — not applicable
here). Two log sites in the awaiter:

- WARN: late delivery (registry miss in `deliver`) — D6.
- WARN: duplicate Deliver (channel full in `deliver`) — D6/D7.
- ERROR: reserved for "registry inconsistency" — a defensive log site
  in `cleanup phase` if `reg[key] != s && reg[key] != nil`. This case
  is unreachable under correct mutex usage but the log path is wired
  so a regression is observable. SEE D5 cleanup-phase pseudocode — the
  `if reg[key] == s { delete }` defensive guard does NOT emit a log on
  the false-branch (a parallel Cancel deletes ahead-of-us is the
  common case, not an inconsistency). Reserve the `Error` channel for
  the *truly impossible* case: `reg[key] != nil && reg[key] != s`,
  which would indicate a double-Register slipped through (the
  Register's `ErrDuplicateRegistration` gate is supposed to prevent).
  Defensive `log.Error(noCtx, "dmawaiter: registry slot identity
  mismatch on cleanup", "op", op, "key", key)` covers it.

### D16 — Goroutine safety statement

`*ArtifactAwaiter` and `*ConfirmationAwaiter` are goroutine-safe by
construction. Specifically:

- Register / Await / Cancel / HandleArtifactsProvided / HandlePersisted
  / HandlePersistFailed / Deliver are all safe to call concurrently
  from any goroutine for ANY key (same or distinct).
- For the SAME key: Register's idempotency gate (`ErrDuplicateRegistration`)
  + Cancel's no-op-on-missing makes "two Awaits on the same key" the
  one disallowed combination (the port godoc binds the caller to a
  single Await per Register).
- For DISTINCT keys: full independence — the only shared state is the
  mutex, held briefly per call (D3).
- `time.NewTimer`/`Stop` is goroutine-safe by Go runtime guarantee.
- The shared `Clock`/`Logger`/`Metrics` seams MUST be goroutine-safe
  per their adapter contract (047 wiring — forward note #4).

### D17 — Hermetic allowlist EXACTLY `{model, port}` (active-fail)

```go
var allowedInternal = map[string]struct{}{
    "contractpro/legal-intelligence-core/internal/domain/model": {},
    "contractpro/legal-intelligence-core/internal/domain/port":  {},
}

var forbiddenInternal = []string{
    "contractpro/legal-intelligence-core/internal/config",
    "contractpro/legal-intelligence-core/internal/infra/observability/metrics",
    "contractpro/legal-intelligence-core/internal/infra/observability/tracer",
    "contractpro/legal-intelligence-core/internal/infra/observability/logger",
    "contractpro/legal-intelligence-core/internal/infra/kvstore",
    "contractpro/legal-intelligence-core/internal/infra/broker",
    "contractpro/legal-intelligence-core/internal/application/pipeline",
    "contractpro/legal-intelligence-core/internal/application/pendingconfirmation",
    "contractpro/legal-intelligence-core/internal/application/aggregator",
    "contractpro/legal-intelligence-core/internal/application/pipeline/stages",
    "contractpro/legal-intelligence-core/internal/ingress/consumer",
    "contractpro/legal-intelligence-core/internal/ingress/idempotency",
    "contractpro/legal-intelligence-core/internal/ingress/router",
    "contractpro/legal-intelligence-core/internal/egress/publisher",
    "contractpro/legal-intelligence-core/internal/egress/dlq",
    "contractpro/legal-intelligence-core/internal/agents/base",
    "contractpro/legal-intelligence-core/internal/llm",
}
```

`TestHermeticImports` MUST first assert that none of `forbiddenInternal`
is present in `allowedInternal` (an active-fail guard against a future
"consistency" edit silently widening the allowlist — the 037/038/040
precedent), THEN scan every non-test `.go` file in the package and
fail on any third-party import (any path containing a `.` — prometheus,
otel, redis, etc.).

### D18 — Forbidden imports — RATIONALE

- `internal/application/pipeline` — `pipeline.Orchestrator` is the
  caller; importing it here would force a circular import (`pipeline`
  imports `port.ArtifactsAwaiterPort` which we satisfy). The 047
  wiring asserts `var _ port.ArtifactsAwaiterPort = (*dmawaiter.
  ArtifactAwaiter)(nil)` in the wiring package, NOT here (the same
  inversion pattern as `pendingconfirmation.PipelineResumer` — D11 of
  037).
- `internal/ingress/router` — same reverse-dependency: the Router
  imports `router.ArtifactsAwaiterDeliverer` (which our `Deliver`
  method satisfies); 047 asserts `var _ router.ArtifactsAwaiterDeliverer
  = (*dmawaiter.ArtifactAwaiter)(nil)` in the wiring package.
- `internal/ingress/consumer` — analogous: the consumer registers
  handlers via `consumer.EventRouter`; the router uses our methods
  indirectly. No direct edge here.
- `internal/application/pendingconfirmation` — orthogonal package.
- `internal/infra/*` — all telemetry/storage seamed away.
- `internal/config` — local Config, ctor-injected.

### D19 — `var _ Port = ...` assertions live in the WIRING package

The structural-satisfaction assertions (verify that
`*ArtifactAwaiter` satisfies `port.ArtifactsAwaiterPort` AND
`port.ArtifactsProvidedHandler` AND `router.ArtifactsAwaiterDeliverer`;
likewise for `*ConfirmationAwaiter`) MUST live in the LIC-TASK-047
wiring package, NOT in this package — asserting any of these in
`awaiter.go` would either (a) be a no-op tautology for the port
assertions (since the port is imported here anyway) or (b) FORCE a
forbidden import for the router-seam / consumer-handler asserts. The
047 wiring package imports all three (port + router + dmawaiter) and
is the natural place for the cross-package satisfaction proofs.

The wiring package MUST write (forward note #1):

```go
var _ port.ArtifactsAwaiterPort = (*dmawaiter.ArtifactAwaiter)(nil)
var _ port.ArtifactsProvidedHandler = (*dmawaiter.ArtifactAwaiter)(nil)
var _ router.ArtifactsAwaiterDeliverer = (*dmawaiter.ArtifactAwaiter)(nil)

var _ port.PersistConfirmationAwaiterPort = (*dmawaiter.ConfirmationAwaiter)(nil)
var _ port.PersistConfirmationHandler = (*dmawaiter.ConfirmationAwaiter)(nil)
var _ router.PersistConfirmationDeliverer = (*dmawaiter.ConfirmationAwaiter)(nil)
```

### D20 — `IsRetryable` / `UNKNOWN_ARTIFACT_TYPE` policy: awaiter does NOT decide

Per the prompt and per the orchestrator's existing `awaitPersist`
behaviour (orchestrator.go:1037-1042 — caller flows
`conf.Failure.IsRetryable` into the response code), the awaiter is a
**pure dispatcher**:

- `PersistFailed.IsRetryable==false` ⇒ the awaiter delivers the typed
  envelope unchanged. The orchestrator surfaces `DM_PERSIST_FAILED`
  fatal via its own DomainError construction.
- `UNKNOWN_ARTIFACT_TYPE` (ADR-LIC-05) ⇒ the awaiter delivers the
  `ArtifactsProvided` (with `ErrorCode=UNKNOWN_ARTIFACT_TYPE`) unchanged.
  The orchestrator decides retry-without-risk_delta.

The awaiter's only contribution is the **outcome label classification**
for the metric (D11) — which is independent of the caller's
DomainError decision (the metric is "did DM deliver"; the DomainError
is "what should the user see"). Both views are needed; neither is
sourced from the other.

---

## PART B — RECONCILIATIONS (R1..R3)

These are DEFECT-style: a documented gap between SSOTs, with binding
resolution.

### R1 — The Deliver/Handle/port-Cancel "three doors, one room" structural reconciliation

**Tension.** The task acceptance criteria literally says:

> `Deliver(correlationID, response)` — invoked by consumer when
> `ArtifactsProvided` arrives.

But the domain port `ArtifactsAwaiterPort` does NOT have a `Deliver`
method — it has `Register/Await/Cancel` only (`dm.go:80-96`). The
inbound side surfaces via `port.ArtifactsProvidedHandler.Handle-
ArtifactsProvided` (`inbound.go:30-33`) — different name, same role.
Meanwhile, the already-shipped router seam declares
`router.ArtifactsAwaiterDeliverer.Deliver(correlationID, evt) error`
(`router/seams.go:87`).

**Resolution (binding).** The awaiter exposes BOTH method names —
`HandleArtifactsProvided(ctx, evt) error` for the inbound port
satisfaction (LIC-TASK-039 subscription target) AND `Deliver(corr, evt)
error` for the router seam (LIC-TASK-040 already shipped — leaving the
seam untouched). Both call the SAME private `deliver(...)` dispatcher.
The duplication is structural-interface noise; the runtime cost is one
extra unconditional call frame (negligible).

**Why correct.** Neither LIC-TASK-039 nor LIC-TASK-040 can be edited
to drop one surface without breaking already-merged code (040's router
shipped with the `ArtifactsAwaiterDeliverer` seam). The two method
names are forward-compatible: a future "consolidation pass" can drop
one without ABI-breaking any other 037-class application package.

`PersistConfirmation` follows the symmetric pattern (`HandlePersisted` +
`HandlePersistFailed` for the inbound handler interface; `Deliver(jobID,
conf)` for the router seam — three exported methods, two interface
roles).

### R2 — Port godoc says "(or is closed on Cancel)" but our channel is closed only on Cancel, not on Await success

**Tension.** `dm.go:83-84`: "Returns a channel that receives exactly
one ArtifactsProvided (or is closed on Cancel)". Read literally, this
suggests the channel is closed in TWO scenarios: (i) a value was
delivered AND received (channel still open, but exhausted); (ii)
Cancel was called.

**Resolution.** The "(or is closed on Cancel)" clause is the SOLE
channel-close condition. After a successful Deliver+Await pair, the
channel is **not closed** — it has had its one buffered value
consumed; the slot is removed from `reg`; the channel is unreferenced
and will be garbage-collected. The port godoc reads consistently with
this: "exactly one" value, then either consumed (open-but-unreachable)
or, if Cancel arrives first, closed.

Test pin T11 verifies "channel is closed after Cancel" via the
two-value channel receive `v, ok := <-ch` (with `ok == false`).

### R3 — Defensive `outcomeMissing` under `persist_artifacts` is OUT OF SSOT but bounded

**Tension.** `observability.md` §3.5 lists `outcome` values per op as:
`get_artifacts ∈ {success, timeout, missing}`; `persist_artifacts ∈
{success, timeout, persist_failed}`. `outcomeMissing` under
`persist_artifacts` is undeclared.

**Resolution.** The `classifyConfirmationOutcome` defensive branch
(an impossible both-set / zero-value `PersistConfirmation`) maps to
`outcomeMissing` for label-bounded cardinality. The compliant path
(constructed via `NewPersistConfirmationSuccess/Failure` per `dm.go`)
NEVER reaches this branch; the metric only emits the defensive value
under a programming bug (caller passes a literal `port.PersistConfirmation{}`).

The cardinality impact is +1 series under a bug; the alternative
(panic on the both-set literal) would propagate to the consumer
ACK path and corrupt redelivery. Recorded as a deliberate deviation
so a future observability-consistency pass widens §3.5 OR adds a
panic to the awaiter.

---

## PART C — TEST PINS (T1..T22)

All pins are in `awaiter_test.go` (same package, in-package fakes for
every seam). Conventions:

- Use a fake `Clock` that advances on `Now()` calls (deterministic).
  Define `fakeClock` with an `Advance(d)` method.
- Use a fake `Metrics` that records every `RecordOutcome` call into a
  slice `[]recOut{op, outcome, seconds}` for assertions.
- Use a fake `Logger` that records every `Warn` / `Error` call.
- Use REAL `time.NewTimer` in T15 (TTL elapse path). All other timeout
  tests use a tiny `TTL` (10ms in `-short`; 100ms otherwise) and the
  REAL clock — these are deterministic enough because no goroutines
  contend on the timer.
- Each test that exercises `Await` runs Await on a goroutine with a
  `done := make(chan struct{})` rendezvous so the test's body can
  observe ordering.
- `-race` must pass.

### Artifact awaiter

- **T1 — Register/Deliver/Await happy path (no timeout).** Register
  `key=corr1`; on goroutine call `Await(ctx, corr1)`; main goroutine
  calls `(*ArtifactAwaiter).Deliver(corr1, evt)` (via the router-seam
  surface) where `evt = port.ArtifactsProvided{CorrelationID: corr1,
  JobID: "j", DocumentID: "d", VersionID: "v", Artifacts: map[...]
  RawMessage{...}}`. Assert: Await returns `(evt, nil)` within
  100ms; `reg` is empty; metric recorded `(op=get_artifacts,
  outcome=success, seconds≈delta)`.

- **T2 — Same happy path via the HandlerInterface entry point.** Repeat
  T1 but call `(*ArtifactAwaiter).HandleArtifactsProvided(ctx, evt)`
  instead of `Deliver`. Assert the same Awaiter behaviour. Pins R1:
  both entry points hit the same dispatcher.

- **T3 — Register twice ⇒ ErrDuplicateRegistration on the second.**
  Call Register(corr1) twice; assert the second returns `(nil, port.
  ErrDuplicateRegistration)`. Then Cancel; assert third Register
  succeeds with a fresh channel (`!=` first channel).

- **T4 — Await TTL elapsed ⇒ port.ErrAwaitTimeout + slot removed.**
  Use `Config.TTL=10ms`. Register(corr1); start Await on goroutine;
  do NOT deliver; assert Await returns `(zero, port.ErrAwaitTimeout)`
  within 50ms; `reg` is empty; metric recorded
  `(get_artifacts, timeout, seconds>=10ms)`.

- **T5 — Await ctx cancel before TTL ⇒ ctx.Err() + slot removed.**
  `Config.TTL=1s`; use `ctx, cancel := context.WithCancel(parent)`;
  Register(corr1); start Await on goroutine; main goroutine calls
  `cancel()`; assert Await returns `(zero, ctx.Err())` where
  `errors.Is(ctx.Err(), context.Canceled)==true`; `reg` is empty;
  metric recorded `(get_artifacts, timeout, seconds<1s)` (per D11
  any ctx-failure classifies as outcome=timeout).

- **T6 — Await ctx deadline exceeded ⇒ context.DeadlineExceeded +
  slot removed.** As T5 but `ctx, cancel := context.WithTimeout(parent,
  20ms)` and `cfg.TTL=1s`; assert Await returns
  `errors.Is(err, context.DeadlineExceeded)==true`; metric recorded
  `(get_artifacts, timeout, ...)`.

- **T7 — Concurrent N=100 Register/Deliver/Await on distinct keys.**
  Run 100 goroutines, each registering its own key (`corr-i`),
  awaiting, with a separate goroutine delivering after a tiny random
  sleep (0-2ms). All 100 Awaits complete with the right
  `ArtifactsProvided` (assert `val.CorrelationID == corr-i`). After
  the wait-group joins, `len(reg) == 0`. Run with `-race`. The test
  iterates 10x to catch flakes.

- **T8 — Late Deliver after Cancel ⇒ silent drop + WARN log + returns
  nil from HandlerInterface.** Register(corr1); Cancel(corr1); call
  `HandleArtifactsProvided(ctx, evt{CorrelationID: corr1})`; assert
  return value `== nil`; assert exactly one WARN log with
  `"key", corr1`. `reg` stays empty.

- **T9 — Cancel idempotency.** Register(corr1); Cancel(corr1);
  Cancel(corr1); Cancel(corr1); assert no panic, no log, `reg` empty.

- **T10 — Duplicate Deliver ⇒ second drops silently with WARN +
  returns nil; first Await still receives the first value.**
  Register(corr1); on goroutine start Await; main goroutine calls
  `Deliver(corr1, evt1)`; then `Deliver(corr1, evt2)`; assert: Await
  returns `(evt1, nil)` — NOT evt2; second Deliver call returned nil;
  exactly one WARN log "duplicate deliver dropped". `reg` empty.

- **T11 — Channel is closed after Cancel (R2 pin).** Register(corr1)
  returns `<-chan ArtifactsProvided`. Cancel(corr1). On the returned
  channel, `v, ok := <-ch`; assert `ok == false` and `v` is zero.

- **T12 — Await on never-Registered key ⇒ ErrAwaitTimeout (D5 NOTE
  pin).** Without calling Register, call `Await(ctx, "nope")`; assert
  return `(zero, port.ErrAwaitTimeout)`. Metric NOT recorded (the
  defensive path returns immediately; no slot ⇒ no duration ⇒ no
  metric — see D5 NOTE). The test asserts metric capture is empty
  for this case.

- **T13 — Outcome classification: ErrorCode set ⇒ missing.** Register
  + Deliver `evt = port.ArtifactsProvided{CorrelationID: corr1,
  ErrorCode: "UNKNOWN_ARTIFACT_TYPE", Artifacts: map{...}}` (the
  ErrorCode is the ADR-LIC-05 hint); assert Await returns
  `(evt, nil)` (NOT an error — the awaiter delivers verbatim; the
  orchestrator interprets); assert metric `(get_artifacts, missing, ...)`.

- **T14 — Outcome classification: MissingTypes non-empty ⇒ missing.**
  Register + Deliver `evt = port.ArtifactsProvided{Artifacts: map{...},
  MissingTypes: []ArtifactType{ArtifactRiskAnalysis}}`; assert Await
  returns `(evt, nil)`; metric `(get_artifacts, missing, ...)`.

- **T15 — Real-timer TTL pin (cap at 500ms in -short).** This is the
  ONE pin that exercises the real `time.NewTimer` end-to-end.
  `Config.TTL = 50ms` (raised to 500ms cap in `-short`); Register +
  Await without Deliver; assert ErrAwaitTimeout within `TTL +
  10ms` jitter budget; assert metric duration is `>= TTL`. The test
  uses `if testing.Short() { ttl = 50ms; jitter = 200ms } else
  { ttl = 50ms; jitter = 50ms }` (i.e., the cap applies to the
  WHOLE test wallclock; the TTL itself stays 50ms).

- **T16 — Leak-free under contract.** Run N=100 cycles of
  Register+Await+(timeout or deliver). After each cycle assert
  `len(reg) == 0`. After all cycles assert `len(reg) == 0`. Pins
  D12's "no leak under documented contract" claim.

### Confirmation awaiter

- **T17 — Persisted (success) happy path.** Register(`jobID="j1"`);
  on goroutine Await; `(*ConfirmationAwaiter).HandlePersisted(ctx,
  port.LegalAnalysisArtifactsPersisted{CorrelationID: "c", JobID:
  "j1", DocumentID: "d"})`; assert Await returns
  `(conf, nil)` with `conf.IsSuccess() == true` and `conf.Success
  != nil`; metric `(persist_artifacts, success, ...)`.

- **T18 — PersistFailed (retryable=true) happy path.** Register(j2);
  HandlePersistFailed with `evt.IsRetryable=true, ErrorCode="…",
  ErrorMessage="…"`; assert Await returns `(conf, nil)`,
  `conf.IsFailure() == true`, `conf.Failure.IsRetryable == true`;
  metric `(persist_artifacts, persist_failed, ...)`.

- **T19 — PersistFailed (retryable=false) happy path.** Same as T18
  but `evt.IsRetryable=false`; assert Awaiter delivers verbatim — it
  does NOT interpret IsRetryable (D20). Caller's responsibility.
  metric `(persist_artifacts, persist_failed, ...)`.

- **T20 — Confirmation envelope construction via constructors panic
  on nil (defensive pin).** A unit test on the awaiter calls
  `defer func() { recover() }()`; tries to call
  `port.NewPersistConfirmationSuccess(nil)`; asserts a panic message
  containing `"NewPersistConfirmationSuccess called with nil envelope"`.
  Pins the awaiter's `HandlePersisted` invariant — `&evt` from a value
  parameter is non-nil, so the awaiter's own call site cannot
  produce the panic in practice, BUT the constructor protects against
  future literal-form misuse.

- **T21 — Router-seam Deliver entry point on confirmation.** Call
  `(*ConfirmationAwaiter).Deliver(jobID, port.NewPersistConfirmation-
  Success(&persisted))` directly; assert Await receives the same
  envelope. Pins the R1 dual-surface invariant for
  ConfirmationAwaiter.

- **T22 — Confirmation awaiter parallel keys (race-clean).** Run
  N=100 goroutines registering distinct jobIDs and delivering on
  goroutines; all Awaits complete; `len(reg)==0`. `-race`-clean.

### Constructor & hermeticity

- **T-CTOR-1 — NewArtifactAwaiter fail-fast on TTL<=0.** Call with
  `ArtifactConfig{TTL: 0}`; assert `err != nil` and the message
  contains `"TTL must be > 0"`. Repeat with `TTL: -1ms`.

- **T-CTOR-2 — NewConfirmationAwaiter fail-fast on TTL<=0.** Symmetric.

- **T-CTOR-3 — NewArtifactAwaiter with all nil Deps fields ⇒ noop
  defaults used.** Call with `Deps{}`; assert constructor succeeds;
  call Register+Cancel; assert no nil-deref panic.

- **T-CTOR-4 — Concurrent Register+Cancel pairs do not race.**
  N=1000 paired goroutines (Register-then-immediate-Cancel) on
  distinct keys. `-race`-clean. `len(reg)==0` at end. This is a
  TTL-budgetless flush test for the mutex windows in D3.

- **T-HERM-1 — `TestHermeticImports`.** Per D17: allowlist EXACTLY
  `{model, port}`; active-fail forbidden set listed in D17; reject
  any third-party import (path contains `.`); reject any
  contractpro path not in `allowedInternal`.

- **T-FMT-1 — `TestGofmtClean`.** Per the 037 / 040 precedent.

Optional `-race`-stress pin (RECOMMENDED, lives in `awaiter_test.go`):

- **T-RACE-1 — Combined Artifact+Confirmation stress.** N=200
  parallel cycles mixing both awaiter types on disjoint keys; assert
  no panic, no race, both `len(reg) == 0`. Use `testing.Short()` to
  scale N down to 20 in `-short`.

---

## PART D — REVIEWER-GATE INVARIANTS (G1..G15)

The code-reviewer (subagent) MUST verify each gate at `file:line`. Each
gate is a single boolean assertion over the implementation; the
reviewer's job is to find the file:line that proves it OR fail the
review.

- **G1 — `awaiter.go` declares exactly two exported types
  (`ArtifactAwaiter`, `ConfirmationAwaiter`) and one private generic
  helper (`slot[T any]`).** No other exported types in the package.

- **G2 — `Config` types are local (no `internal/config` import in any
  awaiter.go file).** Pinned by `TestHermeticImports`. Reviewer
  cross-checks the import block at `awaiter.go` head.

- **G3 — Every `Register` returns `<-chan T` (receive-only) — never
  `chan T` (bidirectional).** Verify the return type in the two
  method signatures.

- **G4 — Every `Await` returns the port-typed `error` (NOT a custom
  awaiter error type).** Verify the return type matches the port
  interface exactly.

- **G5 — `cap(slot.ch) == 1` everywhere it is constructed.** Find
  the single `make(chan T, 1)` call site (D4); reviewer asserts no
  cap=0 / cap>1 / unbuffered channel construction in the package.

- **G6 — Cancel closes the channel UNDER the mutex.** D5 pseudocode
  literal. Reviewer asserts the `close(s.ch)` line is between
  `mu.Lock()` and `mu.Unlock()` in `Cancel`. Failure mode if absent:
  send-on-closed-channel panic under concurrent Deliver+Cancel.

- **G7 — `deliver` performs the non-blocking send via
  `select { case ch <- val: default: log.Warn(...) }`.** Verify the
  exact form (no `for { select }` loop, no fallthrough). Failure mode
  if absent: blocking deliver under duplicate / unbuffered config bug.

- **G8 — Await cleanup phase ALWAYS runs (defer or unconditional
  post-select).** Verify the cleanup phase (slot removal from `reg`)
  executes on EVERY Await exit path (success, timeout, ctx-cancel,
  closed-chan). Use `go vet` reading of the function or manual
  inspection. Failure mode if absent: registry leak.

- **G9 — Cleanup phase uses `if reg[key] == s { delete }` defensive
  guard.** D5 cleanup-phase literal. Failure mode if absent: a
  parallel Cancel + a delayed delete would race and corrupt a freshly-
  Registered slot under the same key.

- **G10 — `metrics.RecordOutcome` is called from EXACTLY one site
  per awaiter (the cleanup-phase tail in Await).** Reviewer
  cross-checks that the dispatch path (`deliver`) does NOT call
  `RecordOutcome` — the metric is bound to Await exit, not Deliver
  arrival (D11). Failure mode if violated: missed-metric on
  ctx-cancel-before-receive (no Await exit ⇒ no metric).

- **G11 — `op` and `outcome` constants are exactly the strings
  `"get_artifacts"` / `"persist_artifacts"` / `"success"` /
  `"timeout"` / `"persist_failed"` / `"missing"`.** D11 literal.
  No typos. Reviewer cross-references `observability.md` §3.5.

- **G12 — `ArtifactAwaiter` exports exactly: `Register`, `Await`,
  `Cancel`, `HandleArtifactsProvided`, `Deliver`. No
  `DeliverArtifactsProvided` / `RouteArtifactsProvided` / other
  duplicates.** Symmetric for `ConfirmationAwaiter`: `Register`,
  `Await`, `Cancel`, `HandlePersisted`, `HandlePersistFailed`,
  `Deliver`.

- **G13 — `NewArtifactAwaiter` / `NewConfirmationAwaiter` follow
  `feedback_constructors.md`.** Names are `NewArtifactAwaiter` /
  `NewConfirmationAwaiter` (NOT `New` / `NewArtifact` /
  `NewArtifactsAwaiter`). Reviewer verifies the names verbatim.

- **G14 — No background goroutines.** Reviewer scans the package for
  any `go func() {...}()` literal. There MUST be ZERO `go` keywords
  in `awaiter.go` (and `seams.go` / `deps.go`). The only goroutines
  are caller-owned (the goroutine that calls Await blocks on the
  select). Failure mode if violated: D12 invariant broken; cleanup
  & lifecycle become a multi-axis concurrency problem.

- **G15 — `TestHermeticImports` allowlist size is exactly 2.**
  Reviewer counts the `allowedInternal` literal entries; rejects any
  PR that grows it (the 037 D17 active-fail precedent — `internal/
  application/*` is `{model, port}`-only forever).

---

## PART E — FORWARD NOTES TO LIC-TASK-047 (WIRING)

These are the binding handoffs to the wiring task. Each is a
constructor call, a `var _` assertion, OR a metric-adapter mapping —
all carried out by 047, NOT 041.

### 1. Constructor calls

```go
// internal/app/wiring.go (or cmd/lic-worker/main.go)

artifactAwaiter, err := dmawaiter.NewArtifactAwaiter(
    dmawaiter.ArtifactConfig{TTL: cfg.Pipeline.DMRequestTimeout},
    dmawaiter.Deps{
        Metrics: artifactMetricsAdapter,  // see (3) below
        Clock:   systemClock{},
        Logger:  loggerAdapter,           // structured logger from infra
    },
)
if err != nil { return fmt.Errorf("wire artifact awaiter: %w", err) }

confirmationAwaiter, err := dmawaiter.NewConfirmationAwaiter(
    dmawaiter.ConfirmationConfig{TTL: cfg.Pipeline.DMPersistConfirmTimeout},
    dmawaiter.Deps{
        Metrics: confirmationMetricsAdapter,
        Clock:   systemClock{},
        Logger:  loggerAdapter,
    },
)
if err != nil { return fmt.Errorf("wire confirmation awaiter: %w", err) }
```

The two awaiters share the same Logger / Clock seam adapters but
distinct Metrics adapters (so each adapter bakes its `op` label —
see (3)).

### 2. `var _` structural-satisfaction assertions (wiring package)

```go
// internal/app/wiring.go (next to the constructor calls)

var (
    // ArtifactAwaiter — three roles.
    _ port.ArtifactsAwaiterPort        = (*dmawaiter.ArtifactAwaiter)(nil)
    _ port.ArtifactsProvidedHandler    = (*dmawaiter.ArtifactAwaiter)(nil)
    _ router.ArtifactsAwaiterDeliverer = (*dmawaiter.ArtifactAwaiter)(nil)

    // ConfirmationAwaiter — three roles.
    _ port.PersistConfirmationAwaiterPort = (*dmawaiter.ConfirmationAwaiter)(nil)
    _ port.PersistConfirmationHandler     = (*dmawaiter.ConfirmationAwaiter)(nil)
    _ router.PersistConfirmationDeliverer = (*dmawaiter.ConfirmationAwaiter)(nil)
)
```

(D19.) The wiring package imports all three of `port`, `router`, and
`dmawaiter`, so it is the natural assertion location. The dmawaiter
package itself MUST NOT contain these asserts.

### 3. Metrics adapter mapping

The 047 wiring layer writes a tiny adapter per awaiter that bakes the
`op` label and bridges to `*metrics.DMMetrics`:

```go
// internal/infra/observability/metrics/dm_adapter.go (or in 047 wiring)

type artifactMetricsAdapter struct{ m *DMMetrics }

func (a artifactMetricsAdapter) RecordOutcome(op, outcome string, seconds float64) {
    // op is always "get_artifacts" because that is what
    // dmawaiter.ArtifactAwaiter's classifier passes (D11). The adapter
    // does not validate — the metric labels themselves enforce.
    a.m.RequestDurationSeconds.WithLabelValues(op).Observe(seconds)
    a.m.RequestOutcomeTotal.WithLabelValues(op, outcome).Inc()
}

var _ dmawaiter.Metrics = artifactMetricsAdapter{}

// symmetric confirmationMetricsAdapter binding op="persist_artifacts"
```

The adapter does NOT attempt to write `ArtifactsReceivedSizeBytes` /
`ArtifactsPublishedSizeBytes` — those are owned by the publisher
(LIC-TASK-043) and pipeline (LIC-TASK-036's payload-build) respectively.

### 4. LIC-TASK-039 (consumer) subscription topology

The consumer's `EventRouter` seam dispatches typed `Route*` events to
the Router, which forwards (via the awaiter Deliver entry point) to
the awaiters. **Alternatively**, the consumer can be subscribed
DIRECTLY against the inbound handler interfaces:

| RabbitMQ topic | Handler | Method |
|---|---|---|
| `dm.responses.artifacts-provided` | `port.ArtifactsProvidedHandler` | `(*dmawaiter.ArtifactAwaiter).HandleArtifactsProvided` |
| `dm.responses.lic-artifacts-persisted` | `port.PersistConfirmationHandler` | `(*dmawaiter.ConfirmationAwaiter).HandlePersisted` |
| `dm.responses.lic-artifacts-persist-failed` | `port.PersistConfirmationHandler` | `(*dmawaiter.ConfirmationAwaiter).HandlePersistFailed` |

The current frozen path is dispatch-via-Router (the existing 040 ships
the deliverer seams already). The Handler interfaces are kept WIRED
and asserted so a future SSOT can collapse the Router-side delivery
indirection without revisiting 041.

### 5. Pipeline orchestrator wiring (unchanged)

`pipeline.NewOrchestrator(..., artAwait port.ArtifactsAwaiterPort, ...,
persistAwait port.PersistConfirmationAwaiterPort, ...)` accepts the
same concrete `*dmawaiter.ArtifactAwaiter` / `*dmawaiter.Confirmation-
Awaiter` instances. No constructor change in `pipeline`. 047 wires the
same instance into BOTH the Router's deliverer seam AND the pipeline's
Awaiter port — one awaiter per type, two ports satisfied.

### 6. TTL parity invariant

`dmawaiter.ArtifactConfig.TTL` MUST equal `pipeline.Config.DMRequestTimeout`
(both sourced from `config.PipelineConfig.DMRequestTimeout` /
`LIC_DM_REQUEST_TIMEOUT`). Otherwise:
- Awaiter TTL < pipeline timeout: the orchestrator sees
  `ErrAwaitTimeout` instead of `context.DeadlineExceeded` —
  classifier still picks `outcomeTimeout` (D11), but the DomainError
  is `DM_ARTIFACTS_TIMEOUT` instead of `ANALYSIS_TIMEOUT`. Acceptable
  but renders the awaiter the deadline source (against the pipeline's
  design where the per-step WithTimeout owns the deadline).
- Awaiter TTL > pipeline timeout: the orchestrator's `subCtx`
  fires first; Await returns `context.DeadlineExceeded`; classifier
  still picks `outcomeTimeout`. Acceptable; awaiter TTL becomes a
  pure safety net for non-orchestrator callers (which v1 does not
  have).

Document the invariant in the 047 wiring CLAUDE.md. The mismatched-
TTL case is observable via `TestDMAwaiter_TTLMismatch` (NOT in scope
here — 047's test).

Symmetric for `ConfirmationConfig.TTL` and `pipeline.Config.DMPersist-
ConfirmTimeout`.

### 7. `go.mod` side-effects

The dmawaiter package imports `context`, `errors`, `sync`, `time`
(stdlib), plus `contractpro/legal-intelligence-core/internal/domain/
{model,port}`. `go mod tidy` produces no diff (verified). Go 1.26
generics required for `slot[T]` — `go.mod` already pins 1.26.1.

### 8. Architecture-doc staleness (recorded, architecture-team-owned)

The port godoc on `ArtifactsAwaiterPort` (`dm.go:80-96`) does NOT
mention the **inbound handler** surface (`ArtifactsProvidedHandler`)
or the **router deliverer** surface — three method-name variations
for the same logical "Deliver from broker" verb. R1 of this spec
reconciles by exposing all three; a future architecture-doc pass
should add a "Inbound dispatch interfaces" subsection cross-
referencing `inbound.go:30-42` and `router/seams.go:74-97`.

---

## PART F — CLAUDE.md TEMPLATE (for the implementer to populate)

The implementer (golang-pro) populates the package CLAUDE.md AFTER
implementation. The template (~150-200 lines, mirroring
`pendingconfirmation/CLAUDE.md`) is:

```
# dmawaiter Package — CLAUDE.md

**DM Artifact Awaiter + DM Confirmation Awaiter** (LIC-TASK-041,
`high-architecture.md` §6.5 step 1, §6.12; `integration-contracts.md`
§6.4; `observability.md` §3.5; `error-handling.md` §3.2). The
in-process correlation registry between the broker ingress
(LIC-TASK-039 consumer → LIC-TASK-040 router) and the pipeline
orchestrator (LIC-TASK-036). Two exported types, each satisfying
THREE structural roles:

  - port.ArtifactsAwaiterPort  / port.PersistConfirmationAwaiterPort
  - port.ArtifactsProvidedHandler / port.PersistConfirmationHandler
  - router.ArtifactsAwaiterDeliverer / router.PersistConfirmationDeliverer

Constructors: `NewArtifactAwaiter(ArtifactConfig, Deps) (*ArtifactAwaiter,
error)` and `NewConfirmationAwaiter(ConfirmationConfig, Deps)
(*ConfirmationAwaiter, error)` — `NewTypeName` (feedback_constructors.md),
fail-fast on invalid Config via errors.Join (the pendingconfirmation
precedent). Immutable after construction; all 6+6 methods are goroutine-
safe across distinct keys (sync.Mutex over a map[string]*slot[T]).

## Files
... (mirror pendingconfirmation; one bullet per file)

## API
... (the 6+6 method signatures + ArtifactConfig + ConfirmationConfig + Deps)

## Reconciliations (build-spec PART B — DEFECT-style)
... (R1, R2, R3 of this build-spec)

## Conventions & deliberate decisions (build-spec D1..D20, condensed)
... (D1..D20 of this build-spec)

## Forward notes (recorded, owners elsewhere — build-spec PART E)
... (E.1..E.8 of this build-spec)
```

---

## PART G — CHECKLIST FOR THE IMPLEMENTER

Strict order. Pass each gate before moving to the next.

1. **Skeleton + Config + Deps + seams.** Write `seams.go`, `deps.go`,
   the `Config` types + `validate()`, and the `slot[T]` private struct.
   `go build ./...` succeeds. Hermetic import-test compiles (no real
   functional code yet).
2. **Constructors.** Write `NewArtifactAwaiter` / `NewConfirmation-
   Awaiter` with the fail-fast `errors.Join` body. Add T-CTOR-1 /
   T-CTOR-2 tests. `go test ./...` passes.
3. **Register / Cancel.** Write the two pairs (one per awaiter), with
   the D3 mutex discipline + D5 lifecycle. Add T3, T9, T11, T-CTOR-4
   tests. `-race` passes.
4. **deliver dispatcher + HandleX + Deliver methods.** Write the
   shared `deliver[T]` generic + the four HandleX methods + the two
   `Deliver` methods. Add T1, T2, T8, T10, T17, T20, T21 tests.
   `-race` passes.
5. **Await + outcome classification + metrics.** Write Await with
   the full D5 select; the two classifier free functions; the
   metrics-emit at cleanup-phase tail. Add T4, T5, T6, T7, T12,
   T13, T14, T15, T16, T18, T19, T22 tests. `-race` passes.
6. **Hermetic / gofmt tests.** Write `TestHermeticImports` +
   `TestGofmtClean` per D17. `go test ./internal/application/
   dmawaiter -run 'TestHerm|TestGofmt'` passes.
7. **CLAUDE.md.** Populate per PART F.
8. **Reviewer gate.** Submit to the code-reviewer subagent;
   reviewer checks G1..G15 at file:line.

End of build spec.
