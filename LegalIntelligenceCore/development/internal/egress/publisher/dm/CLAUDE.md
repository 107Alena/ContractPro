# dm (egress publisher) Package — CLAUDE.md

**DM Artifact Requester** (LIC-TASK-042, `high-architecture.md` §6.5
step 1; `integration-contracts.md` §2 + §6.1;
`DocumentManagement/architecture/event-catalog.md` §1.4;
`observability.md` §3.9). The outbound publisher for the
`lic.requests.artifacts` topic — the only way LIC asks Document
Management for the DP-side artifacts of a given version. One exported
type that satisfies ONE structural role:

```
ArtifactRequester:
  port.ArtifactRequesterPort  — RequestArtifacts
```

The compile-time `var _ port.ArtifactRequesterPort =
(*ArtifactRequester)(nil)` assertion lives in `requester.go` (egress
publishers concretely implement domain ports — unlike the dmawaiter
case where the `var _` lives at LIC-TASK-047 because the router
Deliverer port is in `internal/ingress/router` and asserting it
locally would force a forbidden import).

Constructor: `NewArtifactRequester(Config, Deps) (*ArtifactRequester,
error)` (`NewTypeName`, `feedback_constructors.md`). Fail-fast on
invalid Config / nil-Publisher via `errors.Join` (the
`dmawaiter.NewArtifactAwaiter` / `pendingconfirmation.NewManager` /
`pipeline.NewOrchestrator` precedent). Immutable and stateless after
construction; `RequestArtifacts` is goroutine-safe across distinct
correlation IDs (the only shared state is the broker `Publisher` seam,
which itself serializes publishes internally on its `pubMu`).

The orchestrator-side caller pattern (LIC-TASK-036, forthcoming) wraps
`RequestArtifacts` in its own `context.WithTimeout(o.cfg.
DMRequestTimeout)` and Register's the correlation_id on the
`dmawaiter.ArtifactAwaiter` BEFORE this call (build-spec D10 +
dmawaiter D12 — Register-before-publish is the contract).

## Files

- **doc.go** — package godoc only: hermetic statement
  (integration-contracts.md §2 + §6.1 reference), D1..D15 + R1..R3
  attribution block, the four-entry allowlist enumeration.
- **requester.go** — `ArtifactRequester` struct, `Config` +
  `validate()` (build-spec D2), `NewArtifactRequester` (build-spec
  D2/D9), `RequestArtifacts` (build-spec D4/D5/D6/D7/D8),
  `failValidation` private helper, the constant
  `topicArtifactsRequest = "lic.requests.artifacts"` (build-spec D9 —
  hardcoded), the compile-time `var _ port.ArtifactRequesterPort =
  (*ArtifactRequester)(nil)` assertion (build-spec D11).
- **seams.go** — `Publisher` interface (the broker seam, REQUIRED —
  no noop default), `PublishOutcome` typed enum (LOCAL MIRROR of
  metrics.PublishOutcome — the universal base.Outcome /
  router.CallOutcome / cost / schemavalidator local-mirror precedent;
  keeps production source hermetic), `Metrics` + `noopMetrics` (one
  method `IncPublish(topic, outcome)` — build-spec D7 single-site
  emission), `Clock` + `systemClock` (UTC), `Logger` + `noopLogger`
  (Warn/Error ONLY — build-spec D15: NO Info, NO §11.2 audit mandate;
  reserved for future use, NOT actively called on the hot path).
  `var _ Seam = noop{}` after each pair (the universal pendingconfirmation
  / dmawaiter / router precedent).
- **seams_test.go** — `TestPublishOutcome_WireStringsPinned`: pins
  the four local PublishOutcome strings against
  `metrics.PublishOutcome` SSOT (observability.md §3.9 / metrics/
  labels.go:170-177) so the mirror cannot silently drift. This is the
  ONLY file in this package that imports
  internal/infra/observability/metrics, and it is a _test file, so
  package hermeticity holds.
- **deps.go** — `Deps{Publisher, Metrics, Clock, Logger}` bundle —
  Publisher REQUIRED, the three others optional with noop defaults
  (build-spec D2/D10). `withDefaults()` substitutes nil-optional →
  noop; the constructor's non-nil Publisher check runs AFTER
  `withDefaults` and is the authoritative wiring-defect signal.
- **errors.go** — `PublishError{Reason, Cause}` + `Error()` /
  `Unwrap()` (build-spec D4/D6), the eight package-private reason
  constants (`reasonMissing*`, `reasonInvalidArtifactType`,
  `reasonMarshalFailure`) — snake_case so they
  map directly to log/metric-friendly identifiers; the
  `classifyOutcome(err) PublishOutcome` broker-outcome classifier
  (build-spec D7 branches). Returns the local `PublishOutcome` mirror.
- **requester_test.go** — full behavioural suite T1..T17 + T-CTOR-1..3
  + `TestClassifyOutcome_AllBranches` + `TestPublishError_ErrorAndUnwrap`
  with in-package fakes for every seam (`fakePublisher` / `fakeMetrics`
  / `fakeClock` / `fakeLogger`); `-race`-clean; deterministic.
- **internal_test.go** — `TestHermeticImports` (allowlist size EXACTLY
  3 — `{model, port, broker}` (the local `PublishOutcome` mirror lives
  in seams.go and is pinned in seams_test.go against the metrics
  package SSOT — production source therefore does not import
  `internal/infra/observability/metrics`), reviewer gate; active-fail
  forbidden set per build-spec D13 incl. `internal/config`,
  `internal/infra/observability/metrics` parent, every other
  `internal/application/*` / `internal/ingress/*` / third-party) +
  `TestGofmtClean` (`go/format`; the sandbox blocks `go fmt`).
- **CLAUDE.md** — this file.

## API

- `NewArtifactRequester(Config, Deps) (*ArtifactRequester, error)` —
  returns the wiring-defect error from `errors.Join` if `Config.Exchange
  == ""` and/or `Deps.Publisher == nil`.
- `(*ArtifactRequester) RequestArtifacts(ctx, correlationID, jobID,
  documentID, versionID, organizationID string, []model.ArtifactType)
  error`:
  - Validation failure ⇒ `(*PublishError){Reason: reasonMissing* /
    reasonInvalidArtifactType, Cause: nil}`; metric
    `PublishOutcomeInvalid`; broker.Publish NOT called.
  - Marshal failure (defensive, unreachable for compliant inputs) ⇒
    `(*PublishError){Reason: reasonMarshalFailure, Cause: err}`;
    metric `PublishOutcomeFailure`; broker.Publish NOT called.
  - Broker NACK ⇒ raw err (`errors.Is(err, broker.ErrPublishNack)`
    holds); metric `PublishOutcomeNacked`.
  - Broker ConfirmTimeout / NotConnected / non-retryable AMQP /
    unknown ⇒ raw err; metric `PublishOutcomeFailure`.
  - ctx.Canceled / ctx.DeadlineExceeded ⇒ raw ctx.Err() pass-through
    (NOT wrapped in `*PublishError` — codebase-wide convention);
    metric `PublishOutcomeFailure`.
  - Success ⇒ nil; metric `PublishOutcomeSuccess`.
- `Config{Exchange}` (validated `!= ""`).
- `Deps{Publisher, Metrics, Clock, Logger}` — Publisher REQUIRED;
  the rest optional (nil ⇒ noop).
- `Publisher.Publish(ctx, exchange, routingKey, payload) error` —
  matches `broker.Client.Publish` signature exactly.
- `Metrics.IncPublish(topic string, outcome PublishOutcome)` — the
  single seam method that bridges to
  `lic_publisher_messages_total{topic,outcome}.Inc` at the 036/047
  adapter. The conversion from local `PublishOutcome` → string for
  the prometheus label happens at the adapter boundary.
- `Clock.Now()`; `Logger.Warn` / `Logger.Error` — see seams.go.
- `PublishError{Reason, Cause}` + `Error()` / `Unwrap()`.
- `classifyOutcome(err) PublishOutcome` — package-private, exported
  only via the test file's table-driven branch coverage.

## Reconciliations (build-spec PART B — DEFECT-style, condensed)

**R1 — `Publisher` seam vs concrete `*broker.Client` (build-spec
D3).** *Tension:* the broker is a real infra dependency the requester
needs at runtime; importing `*broker.Client` would bring amqp091 into
the package and break the hermetic statement. *Resolution:* a 1-method
local `Publisher` interface whose signature is byte-identical to
`broker.Client.Publish` (publish.go:36). LIC-TASK-036/047 wires the
real `*broker.Client`; tests pass `fakePublisher`. The seam DOES NOT
hide the broker error TYPES — those still flow back raw and are
inspected via `errors.Is(err, broker.ErrPublishNack)` etc. The
documented R2 exception below covers that.

**R2 — TWO `internal/infra/broker` imports despite the
"hermeticity" statement (build-spec D13).** *Tension:* the four-entry
allowlist looks like a contradiction — three "pure" entries
(domain/model, domain/port, labels) and one infra entry. *Resolution:*
the `broker` import is restricted to TWO sentinels (`ErrPublishNack`,
`ErrConfirmTimeout`) plus the `BrokerError` type traversed by
`errors.As` in the classifier. The concrete `*broker.Client` is
behind the `Publisher` seam (R1), so amqp091 does NOT transitively
land in the requester package. Recorded as a deliberate exception to
the otherwise infra-free egress allowlist; a future "make broker
sentinels live in port" pass could remove the exception entirely.

**R3 — Validation-failure metric emission is OUT-OF-BAND of
`classifyOutcome` (build-spec D7).** *Tension:* the classifier covers
broker outcomes only; a strict "classifier covers all metric paths"
reading would force the classifier to take a validation-failure
parameter and bucket as `invalid`. *Resolution:* the call site
(`failValidation`) emits `PublishOutcomeInvalid` DIRECTLY
before returning the `*PublishError`. The classifier stays narrow:
"given a broker.Publish return, what's the outcome label?" — this is
the dmawaiter D11 classifier-narrow-by-design precedent (the awaiter
classifiers also take a single broker-shape input, not a
wide-union of validation + broker errors).
`TestClassifyOutcome_AllBranches` pins the broker-only contract;
T3..T7 + T8 pin the validation-emits-invalid-directly contract.

## Conventions & deliberate decisions (build-spec D1..D15, condensed)

- **D1/D2 — one exported type + REQUIRED Publisher in Deps.** The
  publisher has no internal state worth threading through generic
  helpers (no per-key registry, no TTL); a single
  `ArtifactRequester{cfg,publisher,metrics,clock,log}` is sufficient.
  Publisher is the ONE Deps field without a noop default — silent
  swallow on `lic.requests.artifacts` would block the pipeline
  awaiter forever without a single log line or metric (D2). The
  constructor enforces non-nil via `errors.Join` after `withDefaults`.
- **D3 — `Publisher` seam isolates the concrete broker.** Local
  1-method interface matching `broker.Client.Publish` byte-for-byte.
  Keeps amqp091 out of the requester package via R1; broker error
  TYPES still flow back raw (R2).
- **D4 — pre-publish validation short-circuit.** Required:
  correlationID, jobID, documentID, versionID, artifactTypes
  (non-empty + each `IsValid()`). Optional: organizationID. Duplicate
  artifact types NOT rejected (orchestrator may de-duplicate
  upstream; we forward verbatim). Each branch emits
  `PublishOutcomeInvalid` directly (R3) and returns a
  `*PublishError{Reason, Cause: nil}` without invoking the broker.
- **D5 — Timestamp via Clock seam.** `time.RFC3339Nano` UTC. The
  `systemClock` default returns `time.Now().UTC()` (matches the
  dmawaiter / pendingconfirmation Clock precedent).
- **D6 — `encoding/json` stdlib + `port.GetArtifactsRequest` value.**
  `OrganizationID` carries `omitempty` so an empty value is omitted
  from the JSON object (T9 pins). Marshal failure ⇒
  `*PublishError{Reason: reasonMarshalFailure, Cause: err}` (NOT
  invalid — defensive: a fixed-shape struct with stdlib-friendly
  types should not fail; if it does it is a serialisation defect,
  not caller input).
- **D7 — `classifyOutcome` narrow contract (R3).** Maps a
  broker.Publish return to one of four `PublishOutcome*` (local mirror)
  values. Validation failures NOT covered — call site emits
  `invalid` directly. The metric is ALWAYS incremented exactly once
  per `RequestArtifacts` call (no silent exit path).
- **D8 — Context errors pass through RAW.** `ctx.Canceled` /
  `ctx.DeadlineExceeded` from `broker.Publish` returns are NOT
  wrapped in `*PublishError`. The codebase-wide convention
  (broker/errors.go:107; dmawaiter D13; the LLM adapters' treatment).
  The metric still buckets them as `failure` per `classifyOutcome`
  (the request did not produce an ack).
- **D9 — `topicArtifactsRequest` hard-coded.** No env-var override
  (build-spec D15 anti-scope): changing the routing key would
  silently de-route every LIC artifact request and is a contract
  break, not a config knob. The wire topic is FROZEN at
  integration-contracts §6.1 / DM event-catalog §1.4.
- **D10 — single-call publisher, no fan-out.** Each
  `RequestArtifacts` invocation produces exactly ONE wire message.
  RE_CHECK base+parent fan-out belongs to the pipeline orchestrator
  (LIC-TASK-036) — it issues TWO RequestArtifacts calls with
  per-stage correlationID suffixes (e.g. `<base>:current`,
  `<base>:parent`).
- **D11 — Compile-time `var _ Port = ...` assertion in requester.go.**
  Unlike dmawaiter (where the var _ lives at 047 because asserting
  the router Deliverer port locally would force a forbidden
  import), egress publishers concretely implement a domain port from
  the allowlist — the assertion is a 1-line guarantee.
- **D12 — Stateless after construction.** `RequestArtifacts` is
  goroutine-safe across any pair of correlationIDs without a mutex.
  The shared state is the broker `Publisher` seam, which itself
  serializes publishes internally on its `pubMu`. T17 pins the
  contract.
- **D13 — Hermetic allowlist EXACTLY 3 entries** (codebase reality:
  the spec'd `metrics/labels` subpackage does not exist — `labels.go`
  lives inside the `metrics` parent — so `PublishOutcome` is a
  local mirror in seams.go pinned in seams_test.go against the
  metrics SSOT, per the universal `base.Outcome` /
  `router.CallOutcome` / `cost.Outcome` / `schemavalidator.RepairOutcome`
  precedent). `{model, port, broker (sentinels via R2)}`. Active-fail
  forbidden set incl. `internal/config` (local Config — 036/047-injected),
  `internal/infra/observability/metrics` parent (would pull
  prometheus dep into production source — the metrics import lives
  in seams_test.go only), every `internal/application/*` /
  `internal/ingress/*` (orthogonal layers — the orchestrator imports
  this package, not vice versa), every third-party path. Reviewer
  gate: `len(allowedInternal) == 3`.
- **D14 — Tests.** T1 (success: exchange + routingKey + payload + 6
  fields + metric=success), T2 (timestamp RFC3339Nano UTC), T3..T7
  (5 validation failures: empty required field), T8 (invalid
  artifact type), T9 (empty organizationID omitted via `omitempty`),
  T10..T16 (broker outcomes + ctx errors: 7 branches), T17 (16
  concurrent goroutines distinct correlationIDs, `-race` clean),
  T-CTOR-1..3 (nil Publisher / empty Exchange / nil-optional-seams
  noop defaults), `TestClassifyOutcome_AllBranches` (table-driven
  classifier coverage), `TestPublishError_ErrorAndUnwrap` (typed
  error contract).
- **D15 — Anti-scope (explicitly NOT in this package).** NO RE_CHECK
  fan-out (D10); NO DLQ routing (egress/dlq is a separate package);
  NO version-meta caching (Redis/orchestrator concern); NO
  correlationID registry (dmawaiter concern); NO retry loop (broker
  client retries internally; per-publish retry is the orchestrator's
  decision); NO duration histogram (the §3.9 counter is the SSOT
  publisher metric); NO topic env override (D9); NO
  AnalysisReadyPublisher (LIC-TASK-043 sibling package — separate
  egress publisher for `lic.artifacts.analysis-ready`).
- **gofmt self-check** via `go/format` (sandbox blocks `go fmt`) —
  the dmawaiter / pipeline / aggregator precedent.

## Forward notes (recorded, owners elsewhere)

1. **LIC-TASK-036 (pipeline orchestrator wiring).** Constructor
   call:
   ```go
   artifactReq, _ := dm.NewArtifactRequester(
       dm.Config{Exchange: cfg.Broker.Exchanges.LICRequests},
       dm.Deps{
           Publisher: brokerClient,
           Metrics:   publisherMetricsAdapter,
           Clock:     systemClock{},
           Logger:    loggerAdapter,
       })
   ```
   The orchestrator calls `artifactReq.RequestArtifacts` AFTER
   `dmawaiter.ArtifactAwaiter.Register` (Register-before-publish is
   the contract — `dm.go:80-87`).

2. **LIC-TASK-043 (sibling — AnalysisReadyPublisher).** Will live in
   `internal/egress/publisher/dm/` next to this file (or in a
   separate `analysisready` sub-package — owner's call). Same
   hermetic boundary; same `Publisher` seam shape; same `Metrics`
   seam (different topic label). The split avoids over-coupling
   "request artifacts" and "publish analysis-ready" — they have
   distinct lifecycle phases and distinct payloads.

3. **LIC-TASK-047 (wiring) `var _` assertions.** None needed for
   this package — the compile-time assertion in `requester.go`
   already pins `port.ArtifactRequesterPort`. The 047 wiring sees
   `*ArtifactRequester` as a `port.ArtifactRequesterPort` directly.

4. **PublisherMetrics adapter (LIC-TASK-036 / TASK-047).** Tiny
   adapter over `*metrics.PublisherMetrics` that calls
   `MessagesTotal.WithLabelValues(topic, string(outcome)).Inc()`.
   The conversion from local `PublishOutcome` (typed) to string
   happens at the adapter boundary, not in this package — keeps the
   counter wiring inside the metrics package where label vocabulary
   is owned. seams_test.go pins the local mirror against the metrics
   SSOT.

5. **`go.mod` side-effects.** dm publisher production imports
   `context`, `encoding/json`, `errors`, `fmt`, `time` (stdlib) plus
   `contractpro/legal-intelligence-core/internal/domain/{model,port}`
   and `.../internal/infra/broker` (sentinels via the Publisher seam).
   No third-party transitive (amqp091 is behind the Publisher seam).
   The seams_test.go file additionally imports
   `.../internal/infra/observability/metrics` for the
   PublishOutcome SSOT pin — _test scope only, does not affect the
   production hermeticity. `go mod tidy` produces no diff.

6. **Architecture-doc note.** The hard-coded
   `topicArtifactsRequest = "lic.requests.artifacts"` matches
   DM/architecture/event-catalog.md §1.4 + integration-contracts.md
   §6.1. Any change to the wire topic is a contract break and MUST
   be coordinated with DM-side consumer wiring; the constant is the
   single source-of-truth on the LIC side.
