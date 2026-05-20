# health Package — CLAUDE.md

**Health & Readiness Handler** (LIC-TASK-009, `high-architecture.md` §10
"Probes", `observability.md` §10.2 "Readiness budget", `deployment.md`
"Kubernetes probes"). HTTP endpoints for orchestrator/Kubernetes probes:

- **GET /healthz** — liveness; always 200 `{status:"ok"}` while the
  process is alive.
- **GET /readyz** — readiness; 200 only when all registered dependency
  checks pass AND the handler has not been flipped to draining.
- **GET /metrics** — forward to an injected `http.Handler`; the package
  itself never imports `promhttp` (architect D6).

Constructor: `NewHandler(checkers []Checker, metricsHandler http.Handler,
opts ...Option) *Handler`.

## Files

- **handler.go** — package doc, `Checker` seam, `Option`+
  `WithDefaultCheckerTimeout`/`WithCheckerTimeout`/`WithReadyDeadline`,
  `Handler`, `NewHandler`, `(*Handler).Mux`/`SetNotReady`, JSON shape
  types (`livenessResponse`, `readyResponse`, `checkJSON`), `writeJSON`.
- **internal_test.go** — `TestHermeticImports` (EMPTY first-party
  allowlist; active-fail on listed internals + ALL third-party) +
  `TestGofmtClean` (`go/format`).
- **handler_test.go** — full functional suite (liveness, readiness
  happy/failed/timeout paths, parallelism, SetNotReady sticky-once,
  NewHandler fail-fast panics, /metrics forward, latency measurement,
  bounded status values, -race concurrency).
- **CLAUDE.md** — this file.

## API

- `NewHandler(checkers []Checker, metricsHandler http.Handler,
  opts ...Option) *Handler` — panics on empty/duplicate `Checker.Name()`,
  on `defaultCheckerTimeout <= 0`, on
  `readyDeadline < max(per-checker timeouts)`.
- `WithDefaultCheckerTimeout(d) Option` — default 1s.
- `WithCheckerTimeout(name, d) Option` — per-name override.
- `WithReadyDeadline(d) Option` — default 2s.
- `(*Handler).Mux() *http.ServeMux`.
- `(*Handler).SetNotReady()` — sticky-once `atomic.Bool` flip; second
  call is a no-op; there is no path back to ready.
- `Checker interface { Name() string; Check(ctx context.Context) error }`
  — implementations MUST be goroutine-safe and MUST honour `ctx`.

## JSON shape

```json
// 200 OK — all checks pass
{"status":"ok","checks":[{"name":"redis","status":"ok","latency_ms":2}]}

// 503 — SetNotReady flipped
{"status":"not_ready","reason":"shutting_down","checks":[]}

// 503 — at least one check failed/timed out
{"status":"not_ready","checks":[
  {"name":"redis","status":"ok","latency_ms":2},
  {"name":"rabbitmq","status":"failed","latency_ms":110,
   "error":"Ping/Channel: connection refused"}
]}
```

- `"error"` field is `omitempty`; `"reason"` field is `omitempty`;
  `"checks"` field is ALWAYS present (even as `[]`).
- Per-check `"status"` ∈ `"ok" | "failed" | "timeout"` (timeout =
  `errors.Is(err, context.DeadlineExceeded)`).
- Overall `"status"` ∈ `"ok" | "not_ready"`.
- `latency_ms` is `int64`, measured externally in this package via
  `time.Since(start).Milliseconds()`; checkers never report latency.
- `Content-Type: application/json` on every response.

## Conventions & deliberate decisions (subagent architect-adjudicator D1–D8, subagent golang-pro correctness review)

- **D1 — hermetic Checker seam; concrete adapters elsewhere.** The
  package imports stdlib only. Dependency probes (Redis, RabbitMQ,
  LLM Router, ...) are inverted behind the two-method `Checker`
  interface; concrete adapters are built in app-wiring (LIC-TASK-047).
  This is the `aggregator.Metrics` / `schemavalidator.Metrics` /
  `concurrency.Gauge` precedent. **Deliberate divergence from
  DocumentProcessing**, whose `health` package is `SetReady(bool)` and
  has no Checker seam at all — LIC's broker (`infra/broker`) and KV
  store (`infra/kvstore`) already exist as independent packages with
  their own clients, so health stays hermetic and consumes them via
  the seam rather than importing them. Recorded so a "consistency
  pass" does not delete the seam or fold health into broker/kvstore.
- **D2 — `Checker` is a two-method interface; fail-fast on bad
  Name().** No `enum`/`type` ranking of dependencies (a wiring-level
  concern). `Name()` is the JSON identity, the
  `WithCheckerTimeout` lookup key, and a duplicate-registration
  detector. `NewHandler` PANICS on empty `Name()` or duplicate `Name()`
  across checkers — both are wiring bugs that no operator input can
  produce (the concurrency `New`-`max<1` / aggregator NoOp-on-error
  fail-fast precedent). **Deliberate divergence from DP**: DP has no
  `Checker` concept, so this question does not arise there. Recorded
  so a "be permissive — log + skip duplicates" simplification does
  not silently drop a check. Pinned by
  `TestNewHandler_DuplicateName_Panics` and
  `TestNewHandler_EmptyName_Panics`.
- **D3 — per-checker timeout is applied externally; checker only
  sees a deadline'd ctx.** `Handler` does
  `context.WithTimeout(reqCtx, h.timeoutFor(name))` and passes the
  wrapped ctx to `Check`. Default 1s
  (`WithDefaultCheckerTimeout`); per-name overrides via
  `WithCheckerTimeout("redis", 100ms)` realise
  `observability.md` §10.2's Redis 100ms budget. A `Check` that
  ignores its ctx still cuts off at the request-level deadline
  (D4), but its returned error will be the checker's own (likely
  context.DeadlineExceeded if it polls ctx, "failed" otherwise) —
  the package never invents an error on the checker's behalf.
- **D4 — wait-all with a request-level deadline.** `sync.WaitGroup`
  + index-stable per-checker results slice (one slot per checker,
  each goroutine writes its own index — no shared mutable state, no
  mutex on the hot path). The total request runtime is capped by
  `WithReadyDeadline` (2s default), and per-checker contexts are
  NESTED under the request context, so a request-deadline timeout
  cancels every in-flight `Check`. `NewHandler` PANICS if
  `readyDeadline < max(per-checker timeouts)` — without this guard,
  a misconfigured wiring (e.g. Redis 5s timeout but readyDeadline
  2s) would silently round every Redis probe up to "timeout".
  Recorded so a "let it slide" relaxation does not reintroduce
  silent-timeout. Pinned by
  `TestNewHandler_ReadyDeadlineLessThanCheckerTimeout_Panics`,
  `TestNewHandler_ReadyDeadlineLessThanPerNameOverride_Panics`,
  `TestReadiness_ParallelExecution`,
  `TestReadiness_RespectsRequestDeadline`.
- **D5 — `SetNotReady()` is sticky-once via `atomic.Bool`; the
  Handler starts ready.** **Deliberate divergence from
  DocumentProcessing**, which has `SetReady(bool)` (toggleable both
  directions) and starts in `false`. In LIC there is exactly ONE
  transition (ready → not_ready); attempting to re-ready a draining
  pod is structurally impossible — Kubernetes drains the pod once
  /readyz fails, and any future readiness must be a fresh process.
  The constructor's "starts ready" default fits LIC's bootstrap
  sequence: by the time `NewHandler` is reachable, the broker and KV
  clients are already constructed (and have their own Ping in
  Checker.Check), so there is no need for a SetReady(true) call.
  Pinned by `TestSetNotReady_Returns503_WithReason`,
  `TestSetNotReady_SecondCallNoop`,
  `TestSetNotReady_DoesNotRevertToReady`. Recorded so a "match DP"
  consistency pass does not reintroduce SetReady(bool) and the
  not-ready bootstrap window it implies.
- **D6 — `metricsHandler http.Handler` is injected, not imported.**
  No `prometheus/promhttp` import. Wiring (LIC-TASK-047) passes
  `promhttp.HandlerFor(metrics.Registry, ...)` from the
  `observability/metrics` package, and the health package mounts it
  verbatim on `/metrics`. This keeps the hermetic test passing
  (TestHermeticImports actively forbids `promhttp`), and it lets
  unit tests construct a Handler with a `nil` metricsHandler
  (route is simply not registered, default mux returns 404). Pinned
  by `TestMetrics_ProxiesToInjectedHandler`,
  `TestMetrics_NilHandler_Returns404`. Recorded so a "just use
  promhttp directly, why all this indirection" simplification does
  not reintroduce a Prometheus import.
- **D7 — only `Handler`, no `Server` wrapper.** No `Start`,
  `Shutdown`, `ListenAndServe`. The HTTP server (with read/write
  timeouts, TLS, graceful shutdown) is built by app-wiring
  (LIC-TASK-047) on top of `Mux()`. This package owns ONLY the
  routes and the handler logic. Recorded so a "make it usable
  standalone" wrapper does not couple the package to an `http.Server`
  lifecycle (it would force importing context cancellation and
  graceful-shutdown logic that already exists in app-wiring).
- **D8 — JSON shape pinned at the package boundary.** Distinct types
  (`livenessResponse`, `readyResponse`, `checkJSON`) — not
  `map[string]any` — so accidental field renames cause compile
  errors, and the shape is reviewable in one place. `"error"` and
  `"reason"` are `omitempty`; `"checks"` is ALWAYS present (even as
  `[]`) so clients branch on `len(checks)` without nil-checks.
  Per-check `"status"` is bounded to
  `"ok"|"failed"|"timeout"` — `"timeout"` is emitted only on
  `errors.Is(err, context.DeadlineExceeded)`, any other non-nil err
  is `"failed"` (the `Check`-author's own context error returns
  pass through as `"timeout"` because of `errors.Is`). Pinned by
  `TestStatusValuesAreBounded`, `TestErrorOmitemptyWhenOK`,
  `TestChecksFieldAlwaysPresent`. `latency_ms` is measured in THIS
  package via `time.Since(start).Milliseconds()` (MF-6); checkers
  never report latency (would force every Check signature change
  and could be falsified). Pinned by
  `TestLatencyMs_MeasuredByHealthPackage`.
- **Constructor name `NewHandler` (not `NewHealthHandler`)** — the
  Effective Go stutter-free rule, the same exemption from
  `feedback_constructors.md` recorded in `broker.NewClient` /
  `kvstore.NewClient` / `concurrency.New`. Recorded so a
  "consistency" pass does not rename it.
- **`atomic.Bool` for notReady; no `sync.Mutex`.** Only
  `sync.WaitGroup` is used (to fan-in checker goroutines). The
  per-request results slice is index-partitioned across goroutines,
  so no mutex protects it. `-race` clean: pinned by
  `TestReadiness_ConcurrentRequests_RaceClean`,
  `TestSetNotReady_ConcurrentWithReadyz_RaceClean`.
- **`Mux` assembled in `NewHandler`** (the DP precedent) — the
  Handler is immutable after construction except for `notReady`, so
  the mux is built once and re-used per request.
- **gofmt self-check via `go/format`** (sandbox blocks `go fmt`) —
  the aggregator/schemavalidator/concurrency precedent.
- **Hermetic test is the D1/D6 enforcement.** EMPTY first-party
  allowlist; active-fails on `internal/config`,
  `internal/infra/broker`, `internal/infra/kvstore`,
  `internal/llm/router`, AND on ALL third-party (notably
  `github.com/prometheus/client_golang/prometheus/promhttp` and
  `github.com/prometheus/client_golang`).

## Forward notes (recorded, owners elsewhere)

1. **LIC-TASK-047 (app-wiring) — concrete adapter signatures.** Build
   one `Checker` per real dependency, each as a tiny adapter that
   delegates to the existing client. Expected wiring:

   ```go
   // adapters live in app-wiring or a side package, NOT in infra/health
   type redisChecker struct{ kv *kvstore.Client }
   func (c *redisChecker) Name() string { return "redis" }
   func (c *redisChecker) Check(ctx context.Context) error {
       return c.kv.Ping(ctx)
   }

   type rabbitChecker struct{ br *broker.Client }
   func (c *rabbitChecker) Name() string { return "rabbitmq" }
   func (c *rabbitChecker) Check(ctx context.Context) error {
       return c.br.Ping(ctx) // or .EnsureConnection(ctx)
   }

   type llmRouterChecker struct{ rt *router.Router }
   func (c *llmRouterChecker) Name() string { return "llm-router" }
   func (c *llmRouterChecker) Check(ctx context.Context) error {
       return c.rt.Healthy(ctx)
   }

   h := health.NewHandler(
       []health.Checker{rc, qc, lc},
       promhttp.HandlerFor(metrics.Registry, promhttp.HandlerOpts{}),
       health.WithCheckerTimeout("redis", 100*time.Millisecond),
   )
   ```

   The app's HTTP server mounts `h.Mux()`. Wire the SIGTERM handler to
   call `h.SetNotReady()` BEFORE starting the broker drain — the
   sticky-once flip guarantees no race between graceful-shutdown and
   the readiness probe.

2. **LIC-TASK-???? (deployment.md "Kubernetes probes") — probe tuning.**
   Suggested values: `livenessProbe` initialDelaySeconds=10 periodSeconds=10
   failureThreshold=3 (so a hung process restarts in ~30s);
   `readinessProbe` initialDelaySeconds=5 periodSeconds=5
   failureThreshold=2 timeoutSeconds=3 (so a transient Redis blip removes
   the pod within ~15s and the 2s `WithReadyDeadline` default leaves a
   1s safety margin under timeoutSeconds=3).

3. **NTH-2 — `Logger` seam (deferred).** If readiness needs structured
   logging (e.g. WARN on a flapping checker), add a minimal
   `Logger interface { Warn(msg string, fields ...any) }` option via
   `WithLogger`. Not in scope here — the package has no logger today
   and the JSON response is the structured output.

4. **NTH-3 — `readyz_duration_seconds` histogram (deferred).** No SSOT
   in `observability.md` §3 today. If approved, add a histogram via
   a new option (`WithReadyDurationHistogram(h Histogram)`) — same
   seam pattern as `concurrency.Gauge`, no signature change to
   existing callers.
