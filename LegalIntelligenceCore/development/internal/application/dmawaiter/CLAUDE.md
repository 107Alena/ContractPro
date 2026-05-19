# dmawaiter Package — CLAUDE.md

**DM Artifact Awaiter + DM Confirmation Awaiter** (LIC-TASK-041,
`high-architecture.md` §6.5 step 1, §6.12; `integration-contracts.md`
§6.4; `observability.md` §3.5; `error-handling.md` §3.2). The in-process
correlation registry wired between the broker ingress (LIC-TASK-039
consumer → LIC-TASK-040 router) and the pipeline orchestrator
(LIC-TASK-036). Two exported types, each satisfying THREE structural
roles via thin adapters over a SHARED private generic dispatcher
(build-spec D1/D2/D6, reconciliation R1):

```
ArtifactAwaiter:
  port.ArtifactsAwaiterPort        — Register / Await / Cancel
  port.ArtifactsProvidedHandler    — HandleArtifactsProvided
  router.ArtifactsAwaiterDeliverer — Deliver

ConfirmationAwaiter:
  port.PersistConfirmationAwaiterPort — Register / Await / Cancel
  port.PersistConfirmationHandler     — HandlePersisted /
                                        HandlePersistFailed
  router.PersistConfirmationDeliverer — Deliver
```

All three surfaces dispatch through the SAME `deliver[T]` private
generic; correctness is single-sourced. The `var _ Port = ...`
structural-satisfaction assertions live in the LIC-TASK-047 wiring
package, NOT here (build-spec D19 — asserting them locally would force
the forbidden `internal/ingress/router` import).

Constructors: `NewArtifactAwaiter(ArtifactConfig, Deps) (*ArtifactAwaiter,
error)` and `NewConfirmationAwaiter(ConfirmationConfig, Deps)
(*ConfirmationAwaiter, error)` — `NewTypeName` (`feedback_constructors.md`),
fail-fast on invalid `Config.TTL` via `errors.Join` (the
`pendingconfirmation.NewManager` / `pipeline.NewOrchestrator` precedent).
Immutable after construction; all 5+5 methods (Register, Await, Cancel,
Handle*, Deliver) are goroutine-safe across distinct keys (`sync.Mutex`
over a `map[string]*slot[T]`).

The orchestrator-side caller pattern (`orchestrator.go:820-832`) wraps
`Await` in its OWN `context.WithTimeout(o.cfg.DMRequestTimeout)`; the
awaiter's `Config.TTL` is the safety-net for any caller that bypassed
`WithTimeout`. At LIC-TASK-047 wiring time the two values are sourced
from the SAME env var (`LIC_DM_REQUEST_TIMEOUT` / `LIC_DM_PERSIST_-
CONFIRM_TIMEOUT`) so the observable behaviour is deterministic.

## Files

- **awaiter.go** — package doc (hermetic statement; D1..D20
  attribution), op/outcome STRING constants (`opGetArtifacts` /
  `opPersistArtifacts` / `outcomeSuccess` / `outcomeTimeout` /
  `outcomePersistFailed` / `outcomeMissing` — observability.md §3.5
  verbatim, reviewer gate G11), the SHARED private generic `slot[T any]`
  (Go 1.26 generics — D2/D4: `chan T` cap=1 + `createdAt time.Time`),
  the two `Config` value types (`ArtifactConfig` / `ConfirmationConfig`)
  + per-type `validate()` (D8 — distinct types document the env-var
  binding), the two structs (`ArtifactAwaiter` / `ConfirmationAwaiter`)
  + their constructors (D9 — `errors.Join` fail-fast, eager `reg`
  allocation), the FIVE exported methods per type (Register / Await /
  Cancel / HandleX / Deliver — six total on ConfirmationAwaiter because
  it has two handler methods), the private `cleanupSlot` helper per
  awaiter (D5/D15 — defensive `reg[key] == s` guard + Error log on the
  truly-impossible identity-mismatch branch), the SHARED `deliver[T]`
  generic dispatcher (D6 — lock-then-select-with-default, WARN on
  miss/duplicate), and the two outcome classifiers
  (`classifyArtifactsOutcome` / `classifyConfirmationOutcome` — D11/R3).
- **seams.go** — local `Metrics`+`noopMetrics` (one method
  `RecordOutcome(op, outcome, seconds)` — D11/G10 single-site emission),
  1-method `Clock`+`systemClock` (D14 — UTC), `Logger`+`noopLogger`
  (Warn/Error ONLY — D15: NO `Info`, no §11.2 audit mandate here).
  `var _ Seam = noop{}` after each pair (the universal B-4 precedent).
- **deps.go** — `Deps{Metrics, Clock, Logger}` shared bundle (D10 —
  ONE shape for both constructors) + `withDefaults()` (the
  `pipeline.Deps.withDefaults` / `pendingconfirmation.Deps.withDefaults`
  pattern verbatim).
- **awaiter_test.go** — full behavioural suite (T1..T22 + T-CTOR-1..4 +
  T-RACE-1 + a targeted classifier pin for the R3 defensive branch)
  with in-package fakes for every seam (`fakeMetrics` / `fakeClock` /
  `fakeLogger`); `-race`-clean; deterministic. ONE real-timer pin
  (T15) — all other timeout pins use a tiny `TTL` (10ms in `-short`;
  100ms otherwise).
- **internal_test.go** — `TestHermeticImports` (allowlist size EXACTLY
  2 — `{model, port}`, reviewer gate G15; active-fail forbidden set
  per D17 incl. `internal/application/pipeline`,
  `internal/ingress/router`, `internal/infra/*`, third-party) +
  `TestGofmtClean` (`go/format`; the sandbox blocks `go fmt`).
- **CLAUDE.md** — this file.

## API

- `NewArtifactAwaiter(ArtifactConfig, Deps) (*ArtifactAwaiter, error)`.
- `NewConfirmationAwaiter(ConfirmationConfig, Deps) (*ConfirmationAwaiter,
  error)`.
- `(*ArtifactAwaiter) Register(correlationID string) (<-chan
  port.ArtifactsProvided, error)` — `port.ErrDuplicateRegistration` on
  re-Register before Await/Cancel.
- `(*ArtifactAwaiter) Await(ctx, correlationID string)
  (port.ArtifactsProvided, error)` — `(zero, port.ErrAwaitTimeout)` on
  TTL elapse / channel-closed-on-Cancel / never-Registered; `(zero,
  ctx.Err())` verbatim on ctx cancel/deadline (D13).
- `(*ArtifactAwaiter) Cancel(correlationID string)` — idempotent;
  closes channel under mutex (G6).
- `(*ArtifactAwaiter) HandleArtifactsProvided(ctx,
  port.ArtifactsProvided) error` — satisfies
  `port.ArtifactsProvidedHandler`; nil on late / duplicate (D6/D7).
- `(*ArtifactAwaiter) Deliver(correlationID string, evt
  port.ArtifactsProvided) error` — satisfies
  `router.ArtifactsAwaiterDeliverer`; same nil-semantics as
  `HandleArtifactsProvided` (R1).
- `(*ConfirmationAwaiter) Register / Await / Cancel` — symmetric over
  `port.PersistConfirmation` keyed by `jobID`.
- `(*ConfirmationAwaiter) HandlePersisted(ctx,
  port.LegalAnalysisArtifactsPersisted) error` /
  `HandlePersistFailed(ctx, port.LegalAnalysisArtifactsPersistFailed)
  error` — satisfy `port.PersistConfirmationHandler`; construct the
  envelope via `port.NewPersistConfirmationSuccess(&evt)` /
  `NewPersistConfirmationFailure(&evt)` (the panic-on-nil precondition
  is structurally satisfied — D6 IMPORTANT note: `&evt` from a value
  parameter is non-nil).
- `(*ConfirmationAwaiter) Deliver(jobID string, conf
  port.PersistConfirmation) error` — satisfies
  `router.PersistConfirmationDeliverer`; Router constructs the
  envelope once and passes it through.
- `ArtifactConfig{TTL}` (LIC_DM_REQUEST_TIMEOUT); `ConfirmationConfig
  {TTL}` (LIC_DM_PERSIST_CONFIRM_TIMEOUT); `Deps{Metrics, Clock,
  Logger}` — every Deps field optional (nil ⇒ noop).
- `Metrics.RecordOutcome(op, outcome string, seconds float64)` — the
  single seam method that bridges to both
  `lic_dm_request_duration_seconds{op}.Observe` AND
  `lic_dm_request_outcome_total{op,outcome}.Inc` at the 047 adapter.
- `Clock.Now()`; `Logger.Warn` / `Logger.Error` — see seams.go.

## Reconciliations (build-spec PART B — DEFECT-style, condensed)

**R1 — The Deliver / Handle / port-Cancel "three doors, one room"
structural reconciliation.** *Tension:* the LIC-TASK-041 acceptance says
`Deliver(correlationID, response)`; the domain port
`port.ArtifactsAwaiterPort` exposes only `Register/Await/Cancel`
(`dm.go:80-96`); the inbound side surfaces via
`port.ArtifactsProvidedHandler.HandleArtifactsProvided`
(`inbound.go:30-33`) — different name, same role; the already-shipped
Router seam (LIC-TASK-040, untouched here) declares
`router.ArtifactsAwaiterDeliverer.Deliver(correlationID, evt) error`
(`router/seams.go:87`). *Resolution:* each awaiter exposes BOTH method
names — `HandleArtifactsProvided` for the inbound handler interface AND
`Deliver` for the router seam (symmetric for confirmation:
`HandlePersisted` + `HandlePersistFailed` + `Deliver`). Both surfaces
call the SAME `deliver[T]` dispatcher; the duplication is structural-
interface noise; the runtime cost is one extra unconditional call
frame. Neither 039 nor 040 can drop one surface without breaking
already-merged code. T2 / T21 pin the dual-surface invariant.

**R2 — Port godoc "(or is closed on Cancel)" is the SOLE close
condition.** *Tension:* `dm.go:83-84` reads "Returns a channel that
receives exactly one ArtifactsProvided (or is closed on Cancel)" —
literally suggesting two close scenarios (post-Await-success vs Cancel).
*Resolution:* the channel is closed ONLY on Cancel. After a successful
Deliver+Await pair the channel is unreferenced and garbage-collected
(its single cap=1 slot consumed). The port godoc reads consistently
with this: "exactly one" value, then either consumed (open-but-
unreachable) or, if Cancel arrives first, closed. T11 pins via the
two-value channel receive `v, ok := <-ch` (`ok == false` after Cancel).

**R3 — Defensive `outcomeMissing` under `persist_artifacts` is OUT OF
SSOT but bounded.** *Tension:* `observability.md` §3.5 enumerates
`persist_artifacts` outcomes as `{success, timeout, persist_failed}`;
`outcomeMissing` is documented only under `get_artifacts`. *Resolution:*
the `classifyConfirmationOutcome` defensive branch (an impossible
both-set / zero-value `port.PersistConfirmation`) maps to
`outcomeMissing` for label-bounded cardinality. The compliant wire path
(constructed via `port.NewPersistConfirmationSuccess` /
`NewPersistConfirmationFailure`) NEVER reaches this branch — the
constructor's panic-on-nil precondition (`dm.go:138-152`) makes a
zero-value envelope unreachable from a compliant Handler call site. The
+1 cardinality is the deliberate cost of NOT panicking inside `deliver`
(which would corrupt the consumer ACK path). Recorded as a deliberate
deviation so a future observability-consistency pass either widens
§3.5 OR adds a panic to the awaiter.
`TestClassifyConfirmationOutcome_BothZero_DefensiveMissing` pins the
classifier output for the defensive case.

## Conventions & deliberate decisions (build-spec D1..D20, condensed)

- **D1/D2 — two exported types + one SHARED `slot[T any]` generic.**
  `ArtifactAwaiter` and `ConfirmationAwaiter` are distinct because their
  keys (`correlation_id` vs `job_id`), payload types, metric ops, and
  outcome classifiers differ. The registry is `map[string]*slot[T]` per
  awaiter; `slot` is parametric (Go 1.26 generics, module-sanctioned).
  Merging into a single `Awaiter[K,T]` is rejected — the parametric
  saving is zero once op + classifier are bound. Reviewer gate G1: one
  generic helper, two exported types.
- **D3 — `map[string]*slot[T]` guarded by a single `sync.Mutex`, NOT
  `sync.Map`.** Both hot operations (Register / Cancel / deliver) do
  TWO atomic map mutations; `sync.Map`'s `LoadOrStore` covers Register
  but not Cancel, and the mixed pattern needs a second mutex anyway.
  Contention is bounded by per-key independence (microsecond windows).
- **D4 — channel cap=1.** `make(chan T, 1)` (G5 — no cap=0 / cap>1
  anywhere). cap=1 makes the producer's non-blocking send via
  `select { case ch<-val: default: WARN }` correct under both
  Register-then-Deliver and Deliver-before-Await orderings. Cap=0 would
  leak a goroutine on ctx-cancel before Await arrives.
- **D5 — Register / Await / Cancel lifecycle.** Register: under mutex,
  reject duplicate with `port.ErrDuplicateRegistration`. Await: lookup
  under mutex; if nil ⇒ return `port.ErrAwaitTimeout` immediately (NO
  metric — no slot, no createdAt, no duration; build-spec D5 NOTE +
  T12); else select over `<-s.ch` (with `!ok` defensive ⇒
  `ErrAwaitTimeout` for the channel-closed-on-Cancel race), `<-ctx.Done`
  (return `ctx.Err()` verbatim — D13), `<-timer.C` (TTL ⇒
  `ErrAwaitTimeout`). Cleanup phase ALWAYS runs on every exit path
  (G8) — `cleanupSlot` uses the defensive `reg[key] == s` guard (G9).
  Metric AFTER the lock release (G10 — `deliver` does NOT record
  metrics; that is bound to Await exit only). Cancel: under mutex,
  delete + `close(s.ch)` UNDER the lock (G6) so a concurrent Deliver
  cannot send-on-closed-channel; idempotent silent no-op if slot
  already gone.
- **D6 — single `deliver[T]` dispatcher.** Lock + lookup +
  non-blocking `select` send (G7). Registry-miss ⇒ WARN + nil; channel
  full ⇒ first-wins + WARN + nil. WARN ctx is `context.Background()`
  (a nil ctx would crash a structured logger calling `ctx.Value`).
  Both the inbound Handler* method and the Router `Deliver` method are
  thin wrappers (R1).
- **D7 — duplicate Deliver = first-wins.** The second Deliver finds
  the channel full, drops with WARN, returns nil. Overwriting would
  race with the Await receiver's classifyOutcome; the broker
  at-least-once guarantee makes duplicates a normal expected event.
- **D8 — per-awaiter local `Config` with `validate()`.** Two types
  (not one shared) so the 047 wiring binds the right env var per
  awaiter. NO `internal/config` import — the
  `pipeline.Config`/`pendingconfirmation.Config`/`router.Config`
  precedent. validate fails fast (errors.New, not domain error — this
  is a startup wiring defect).
- **D9 — constructors fail-fast with `errors.Join`.** The
  `pendingconfirmation.NewManager` / `pipeline.NewOrchestrator`
  precedent. No required positional collaborators beyond `Config` (the
  awaiter has no outbound port — the broker side dispatches INTO it).
  Registry map is eagerly allocated so the first concurrent Register
  doesn't race with map creation.
- **D10 — shared `Deps{Metrics, Clock, Logger}` bundle + per-call
  `withDefaults()`.** All three optional with noop defaults — the
  universal `internal/application/*` precedent.
- **D11 — single Metrics seam method + outcome classification at Await
  exit.** `Metrics.RecordOutcome(op, outcome, seconds)` collapses both
  prometheus operations into one adapter call. op is hard-coded per
  awaiter (`opGetArtifacts` / `opPersistArtifacts`). The Metrics call
  is the ONLY site (G10) — `deliver` does NOT record metrics (a
  ctx-cancel-before-receive case would otherwise miss the metric).
  Classifiers (`classifyArtifactsOutcome` / `classifyConfirmationOutcome`)
  inspect `(val, err)` per the strict rules: artifact ⇒ err? timeout :
  (ErrorCode!="" || len(MissingTypes)>0)? missing : success;
  confirmation ⇒ err? timeout : IsSuccess()? success :
  IsFailure()? persist_failed : missing (R3 defensive).
- **D12 — no background sweeper goroutine.** TTL is enforced lazily
  inside each Await via `time.NewTimer`. Reviewer gate G14: ZERO `go`
  keywords in non-test files. The orchestrator's defer chain
  (`pipeline.Orchestrator.Run` LIFO) makes a leaked-slot-without-Cancel
  impossible in compliant callers; T16 pins the leak-free invariant.
- **D13 — ctx propagation verbatim.** Await returns `ctx.Err()` (NOT
  re-classified into `ErrAwaitTimeout`) so the orchestrator's
  `errors.Is(err, context.DeadlineExceeded)` branch behaves as
  designed. The classifier independently buckets ctx-cancel as
  `outcomeTimeout` (D11 — the metric semantic is "the request did not
  produce a usable response").
- **D14 — 1-method `Clock` seam.** Two reads: `Register` (slot
  createdAt — duration start) and Await exit (duration end). The
  `pendingconfirmation.Clock` 1-method precedent.
- **D15 — `Logger` seam with Warn/Error ONLY.** NO `Info`: the
  awaiter has no §11.2 audit-trail mandate. WARN sites: registry-miss
  (`deliver`), duplicate (`deliver`). ERROR site: defensive
  registry-identity-mismatch in `cleanupSlot` (the unreachable
  `reg[key] != nil && reg[key] != s` case — Reviewer gate G12).
- **D16 — goroutine-safety statement.** All exported methods are safe
  to call concurrently from any goroutine for ANY key (same or
  distinct) — the only shared state is the mutex (microsecond windows)
  + the channel itself (Go-runtime-safe). Two Awaits on the SAME key
  is the one disallowed combination (port godoc binds the caller to a
  single Await per Register).
- **D17/D18 — hermetic allowlist EXACTLY `{model, port}` (G15).**
  Active-fail forbidden set incl. `internal/application/pipeline` (the
  reverse-dependency edge — the orchestrator IS the caller), `internal/
  ingress/router` (the deliverer seam is satisfied structurally, the
  var _ lives at 047), `internal/application/pendingconfirmation` /
  `internal/ingress/consumer` / `internal/ingress/idempotency`
  (orthogonal application/ingress siblings), all `internal/infra/*` (every
  telemetry/clock/logger seamed), `internal/config` (local Config),
  every third-party path (anything containing `.`).
- **D19 — `var _ Port = ...` assertions live in the WIRING package.**
  Asserting them locally would either be a no-op tautology (the port
  is imported anyway) or force the forbidden
  `internal/ingress/router` import. The 047 wiring package imports
  port + router + dmawaiter and is the natural assertion location
  (see Forward notes #1).
- **D20 — `IsRetryable` / `UNKNOWN_ARTIFACT_TYPE` policy: awaiter does
  NOT decide.** `PersistFailed.IsRetryable==false` flows through
  verbatim — the orchestrator's `awaitPersist`
  (`orchestrator.go:1037-1042`) surfaces `DM_PERSIST_FAILED`.
  `UNKNOWN_ARTIFACT_TYPE` (ADR-LIC-05) flows through in
  `ArtifactsProvided.ErrorCode` — the orchestrator decides
  retry-without-risk_delta. The awaiter's only contribution is the
  outcome label classification for the metric.
- **gofmt self-check** via `go/format` (sandbox blocks `go fmt`) —
  the pipeline / aggregator / stages / pendingconfirmation precedent.

## Forward notes (recorded, owners elsewhere — build-spec PART E)

1. **LIC-TASK-047 (app-wiring, `internal/app` or
   `cmd/lic-service/main.go`).** Constructor calls:
   ```
   artifactAwaiter, _ := dmawaiter.NewArtifactAwaiter(
       dmawaiter.ArtifactConfig{TTL: cfg.Pipeline.DMRequestTimeout},
       dmawaiter.Deps{Metrics: artifactMetricsAdapter,
                       Clock: systemClock{}, Logger: loggerAdapter})
   confirmationAwaiter, _ := dmawaiter.NewConfirmationAwaiter(
       dmawaiter.ConfirmationConfig{TTL: cfg.Pipeline.
                                          DMPersistConfirmTimeout},
       dmawaiter.Deps{Metrics: confirmationMetricsAdapter,
                       Clock: systemClock{}, Logger: loggerAdapter})
   ```
   `var _` structural-satisfaction assertions (D19), written in the
   WIRING package, NOT here:
   ```
   var (
     _ port.ArtifactsAwaiterPort        = (*dmawaiter.ArtifactAwaiter)(nil)
     _ port.ArtifactsProvidedHandler    = (*dmawaiter.ArtifactAwaiter)(nil)
     _ router.ArtifactsAwaiterDeliverer = (*dmawaiter.ArtifactAwaiter)(nil)
     _ port.PersistConfirmationAwaiterPort = (*dmawaiter.ConfirmationAwaiter)(nil)
     _ port.PersistConfirmationHandler     = (*dmawaiter.ConfirmationAwaiter)(nil)
     _ router.PersistConfirmationDeliverer = (*dmawaiter.ConfirmationAwaiter)(nil)
   )
   ```
   Metrics adapter: per-awaiter tiny adapter over `*metrics.DMMetrics`
   that bakes the `op` label and bridges
   `RequestDurationSeconds.WithLabelValues(op).Observe(seconds)` +
   `RequestOutcomeTotal.WithLabelValues(op,outcome).Inc()`. The
   adapter does NOT write `ArtifactsReceivedSizeBytes` /
   `ArtifactsPublishedSizeBytes` — those are owned by the publisher
   (LIC-TASK-043) and the pipeline payload-build (LIC-TASK-036).

2. **LIC-TASK-039 (consumer) subscription topology.** Two delivery
   paths are wired and asserted; the current frozen path is
   dispatch-via-Router (the existing 040 ships the deliverer seams).
   The Handler interfaces remain wired so a future SSOT can collapse
   the Router-side indirection without revisiting 041:
   | RabbitMQ topic | Handler | Method |
   |---|---|---|
   | `dm.responses.artifacts-provided` | `port.ArtifactsProvidedHandler` | `(*ArtifactAwaiter).HandleArtifactsProvided` |
   | `dm.responses.lic-artifacts-persisted` | `port.PersistConfirmationHandler` | `(*ConfirmationAwaiter).HandlePersisted` |
   | `dm.responses.lic-artifacts-persist-failed` | `port.PersistConfirmationHandler` | `(*ConfirmationAwaiter).HandlePersistFailed` |

3. **Pipeline orchestrator wiring (unchanged).**
   `pipeline.NewOrchestrator(..., artAwait port.ArtifactsAwaiterPort,
   ..., persistAwait port.PersistConfirmationAwaiterPort, ...)` accepts
   the same concrete `*dmawaiter.ArtifactAwaiter` /
   `*dmawaiter.ConfirmationAwaiter` instances. 047 wires the SAME
   instance into BOTH the Router's deliverer seam AND the pipeline's
   Awaiter port — one awaiter per type, two ports satisfied.

4. **TTL parity invariant (047-wiring CLAUDE.md).**
   `dmawaiter.ArtifactConfig.TTL` MUST equal
   `pipeline.Config.DMRequestTimeout` (both sourced from
   `config.PipelineConfig.DMRequestTimeout` /
   `LIC_DM_REQUEST_TIMEOUT`). With equal values either path triggers
   `port.ErrAwaitTimeout` (whichever fires first). Symmetric for
   `ConfirmationConfig.TTL` vs `pipeline.Config.DMPersistConfirmTimeout`.
   The mismatched-TTL case is observable via
   `TestDMAwaiter_TTLMismatch` (NOT in scope here — 047's test).

5. **`go.mod` side-effects.** dmawaiter imports `context`, `errors`,
   `sync`, `time` (stdlib) plus `contractpro/legal-intelligence-core/
   internal/domain/{model,port}`. `go mod tidy` produces no diff
   (verified). Go 1.26 generics required for `slot[T]` — `go.mod`
   already pins 1.26.1.

6. **Architecture-doc staleness (recorded, architecture-team-owned).**
   The port godoc on `ArtifactsAwaiterPort` (`dm.go:80-96`) does NOT
   mention the inbound handler surface (`ArtifactsProvidedHandler`) or
   the router deliverer surface — three method-name variations for the
   same logical "deliver from broker" verb. R1 reconciles by exposing
   all three; a future architecture-doc pass should add an "Inbound
   dispatch interfaces" subsection cross-referencing `inbound.go:30-42`
   and `router/seams.go:74-97`.
