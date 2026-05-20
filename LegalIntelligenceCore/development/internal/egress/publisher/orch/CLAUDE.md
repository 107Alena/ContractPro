# orch (egress publisher) Package — CLAUDE.md

Outbound Orchestrator-facing publisher(s) for the LIC → API/Backend
Orchestrator RabbitMQ contracts. Currently houses ONE publisher (the LIC
Status Event Publisher, LIC-TASK-044); the future siblings LIC-TASK-045
(ClassificationUncertain) and LIC-TASK-046 (DLQ) are slated to land here
without re-importing the dm package or growing the allowlist.

1. **Status Event Publisher** (LIC-TASK-044, `high-architecture.md` §6.13,
   `LIC event-catalog.md` §1.1, `observability.md` §3.9). Publishes
   `lic.events.status-changed` — every external-status transition
   (IN_PROGRESS / COMPLETED / FAILED) the Orchestrator needs to surface to
   the user. Deduplication key on the Orchestrator side:
   `lic-status:{job_id}:{status}` — safe re-publication on crash-recovery
   is supported by design.

One exported type, satisfying ONE structural role:

```
StatusPublisher: port.StatusPublisherPort — PublishStatus
```

The compile-time `var _ port.StatusPublisherPort = (*StatusPublisher)(nil)`
assertion lives in `publisher.go` next to the type itself — egress
publishers concretely implement domain ports (the universal
`var _ Port = (*Impl)(nil)` pattern, identical to `dm.ArtifactRequester` /
`dm.AnalysisArtifactsPublisher`).

Constructor: `NewStatusPublisher(PublisherConfig, PublisherDeps)
(*StatusPublisher, error)` (`NewTypeName`,
`feedback_constructors.md`). Fail-fast on invalid Config / nil-Publisher
via `errors.Join` (the `dmawaiter.NewArtifactAwaiter` /
`pendingconfirmation.NewManager` / `pipeline.NewOrchestrator` /
`dm.NewAnalysisArtifactsPublisher` precedent). Immutable and stateless
after construction; the hot path (`PublishStatus`) is goroutine-safe
across distinct correlation_ids (the only shared state is the broker
`Publisher` seam, which itself serializes publishes internally on its
`pubMu`).

## Files

- **doc.go** — package godoc only: hermetic statement
  (event-catalog §1.1 + observability §3.9 reference), the LIC-TASK-044
  attribution block (D1..D13 + R1..R6), the three-entry allowlist
  enumeration, the documented deliberate differences from the sibling
  `dm` publisher (required `organization_id`, no size histogram,
  status-conditional validation, catalog-driven ErrorMessage rewrite).
- **publisher.go** — `StatusPublisher` struct, `PublisherConfig` +
  `validate()`, `NewStatusPublisher`, `PublishStatus`, `failValidation`
  private helper, the constant `topicStatusChanged =
  "lic.events.status-changed"` (hardcoded, no env-var override), the
  compile-time `var _ port.StatusPublisherPort = (*StatusPublisher)(nil)`
  assertion, the package-level `marshalStatus = json.Marshal`
  test-overridable seam.
- **seams.go** — `Publisher` interface (the broker seam, REQUIRED — no noop
  default), `PublishOutcome` typed enum (LOCAL MIRROR of
  `metrics.PublishOutcome` — the universal `base.Outcome` /
  `router.CallOutcome` / `cost` / `schemavalidator` / `dm` local-mirror
  precedent; keeps production source hermetic), `Metrics` + `noopMetrics`
  (ONE method: `IncPublish(topic, outcome)` — no `ObservePublishedSize`,
  the status envelope is small with a fixed shape; the §3.5 size
  histogram is specific to the lic.artifacts.analysis-ready terminal
  payload), `Clock` + `systemClock` (UTC), `Logger` + `noopLogger`
  (Warn/Error ONLY — no §11.2 audit mandate; reserved for future use,
  NOT actively called on the hot path). `var _ Seam = noop{}` after each
  pair (the universal `pendingconfirmation` / `dmawaiter` / `router` /
  `dm` precedent).
- **seams_test.go** — `TestPublishOutcome_WireStringsPinned`: pins the
  four local PublishOutcome strings against `metrics.PublishOutcome`
  SSOT (observability.md §3.9 / metrics/labels.go:170-177). This is the
  ONLY file in this package that imports
  `internal/infra/observability/metrics`, and it is a `_test.go` file,
  so package hermeticity holds.
- **deps.go** — `PublisherDeps{Publisher, Metrics, Clock, Logger}` —
  Publisher REQUIRED (no noop default; silent-swallow would block every
  status transition from reaching the Orchestrator), the three others
  optional with noop defaults. `withDefaults()` substitutes nil-optional
  → noop; the constructor's non-nil Publisher check runs AFTER
  `withDefaults` and is the authoritative wiring-defect signal.
- **errors.go** — `PublishError{Reason, Cause}` + `Error()` / `Unwrap()`,
  ONE reason-constant block of 13 snake_case identifiers covering:
    - Block A — 5 envelope-ID required: `reasonMissingCorrelationID`,
      `reasonMissingJobID`, `reasonMissingDocumentID`,
      `reasonMissingVersionID`, `reasonMissingOrganizationID`.
    - Block B — Status enum: `reasonInvalidStatus`.
    - Block C — Stage (optional): `reasonInvalidStage`.
    - Block D — status-conditional: `reasonMissingErrorCode`,
      `reasonNonPublishableErrorCode`, `reasonMissingRetryable`,
      `reasonUnexpectedFailureFields`.
    - Defensive: `reasonErrorCodeNotInCatalog`, `reasonMarshalFailure`.
  The `classifyOutcome(err) PublishOutcome` broker-outcome classifier is
  identical in shape to the sibling dm publisher's — narrow contract
  ("given a broker.Publish return, what's the outcome label?";
  validation failures are emitted at the call site).
- **publisher_test.go** — full StatusPublisher behavioural suite
  T1..T26 + T-CTOR-1..3 + T-CTOR-EXTRA + `TestClassifyOutcome_AllBranches`
  + `TestPublishError_ErrorAndUnwrap`. In-package fakes
  (`fakePublisher` / `fakeMetrics` / `fakeClock` / `fakeLogger`) — does
  not depend on the sibling `dm` fakes.
- **internal_test.go** — `TestHermeticImports` (allowlist size EXACTLY 3
  — `{model, port, broker}`; reviewer gate; active-fail forbidden set
  incl. `internal/config`, `internal/infra/observability/metrics`
  parent, every other `internal/application/*` / `internal/ingress/*` /
  `internal/egress/publisher/dm` sibling / third-party) +
  `TestGofmtClean` (`go/format`; the sandbox blocks `go fmt`).
- **CLAUDE.md** — this file.

## API

### StatusPublisher (LIC-TASK-044)

- `NewStatusPublisher(PublisherConfig, PublisherDeps)
  (*StatusPublisher, error)` — returns the wiring-defect error from
  `errors.Join` if `PublisherConfig.Exchange == ""` and/or
  `PublisherDeps.Publisher == nil` (both defects surface together —
  T-CTOR-3).
- `(*StatusPublisher) PublishStatus(ctx, port.LICStatusChangedEvent)
  error`:
  - Validation failure ⇒ `(*PublishError){Reason: reasonMissing* /
    reasonInvalid* / reasonNonPublishableErrorCode /
    reasonUnexpectedFailureFields / reasonErrorCodeNotInCatalog,
    Cause: nil}`; metric `PublishOutcomeInvalid`; broker.Publish NOT
    called.
    - **Block A (5 branches):** correlation_id, job_id, document_id,
      version_id, organization_id — all required (organization_id is
      REQUIRED here, unlike the sibling dm publisher).
    - **Block B (1 branch):** Status — IsValid (empty + unknown rejected
      together).
    - **Block C (1 branch):** Stage — optional; non-empty MUST satisfy
      IsValid.
    - **Block D (3 + 1 = 4 branches):** if Status == FAILED then
      ErrorCode required + ErrorCode.IsPublishableToOrchestrator +
      IsRetryable required; else (IN_PROGRESS or COMPLETED) none of
      ErrorCode / ErrorMessage / IsRetryable may be set (one combined
      `reasonUnexpectedFailureFields`).
  - Marshal failure ⇒ `(*PublishError){Reason: reasonMarshalFailure,
    Cause: err}`; metric `PublishOutcomeFailure`; broker.Publish NOT
    called. Defensive — unreachable for compliant inputs.
  - Broker NACK ⇒ raw err (`errors.Is(err, broker.ErrPublishNack)`
    holds); metric `PublishOutcomeNacked`.
  - Broker ConfirmTimeout / NotConnected / non-retryable AMQP /
    unknown ⇒ raw err; metric `PublishOutcomeFailure`.
  - ctx.Canceled / ctx.DeadlineExceeded ⇒ raw ctx.Err() pass-through
    (NOT wrapped in `*PublishError` — codebase-wide convention);
    metric `PublishOutcomeFailure`.
  - Success ⇒ nil; metric `PublishOutcomeSuccess`.
- In-method rewrites (after validation passes):
  - `evt.Timestamp = clock.Now().Format(time.RFC3339Nano)` — UTC.
    Caller-side variable unchanged (value semantics; T2 pins both
    halves of the contract).
  - FAILED only: `evt.ErrorMessage =
    model.LookupErrorSpec(evt.ErrorCode).UserMessage` — catalog is the
    SSOT for the RU user-facing rendering (NFR-5.2). Any caller-supplied
    ErrorMessage is OVERWRITTEN. T18 / T19 pin the contract.
- `PublisherConfig{Exchange}` (validated `!= ""`).
- `PublisherDeps{Publisher, Metrics, Clock, Logger}` — Publisher
  REQUIRED; the rest optional (nil ⇒ noop).

### Shared seams

- `Publisher.Publish(ctx, exchange, routingKey, payload) error` —
  matches `broker.Client.Publish` signature exactly.
- `Metrics.IncPublish(topic string, outcome PublishOutcome)` — single
  seam method that bridges to
  `lic_publisher_messages_total{topic,outcome}.Inc` at the 036/047
  adapter. The conversion from local `PublishOutcome` → string for
  the prometheus label happens at the adapter boundary.
- `Clock.Now()`; `Logger.Warn` / `Logger.Error` — see seams.go.
- `PublishError{Reason, Cause}` + `Error()` / `Unwrap()`.
- `classifyOutcome(err) PublishOutcome` — package-private; covered by
  `TestClassifyOutcome_AllBranches`.

## Reconciliations (LIC-TASK-044, DEFECT-style, condensed)

**R1 — `Publisher` seam vs concrete `*broker.Client`.** Same rationale
as the sibling `dm` publisher's R1. A 1-method local `Publisher`
interface whose signature is byte-identical to `broker.Client.Publish`
(publish.go:36). LIC-TASK-036/047 wires the real `*broker.Client`;
tests pass `fakePublisher`. The seam DOES NOT hide the broker error
TYPES — those still flow back raw and are inspected via
`errors.Is(err, broker.ErrPublishNack)` etc. (R2).

**R2 — `internal/infra/broker` import despite the "hermeticity"
statement.** Same rationale as the sibling `dm` publisher's R2. The
`broker` import is restricted to TWO sentinels (`ErrPublishNack`,
`ErrConfirmTimeout`) plus the `BrokerError` type traversed by
`errors.As` in the classifier. The concrete `*broker.Client` is
behind the `Publisher` seam, so amqp091 does NOT transitively land in
the publisher package. Documented exception to the otherwise infra-free
egress allowlist.

**R3 — Validation-failure metric emission is OUT-OF-BAND of
`classifyOutcome`.** Same rationale as the sibling `dm` publisher's R3.
The call site (`failValidation`) emits `PublishOutcomeInvalid` DIRECTLY
before returning the `*PublishError`. The classifier stays narrow:
"given a broker.Publish return, what's the outcome label?". T3..T17 pin
the validation-emits-invalid-directly contract; T20 pins the
marshal-failure-emits-failure-directly contract.

**R4 — `organization_id` REQUIRED here vs OPTIONAL in `dm`
publisher.** *Tension:* the sibling dm.GetArtifactsRequest treats
`organization_id` as optional (omitempty). The "consistency" reading
would suggest the same here. *Resolution:* `port.LICStatusChangedEvent.
OrganizationID` carries NO `omitempty` tag — every Orchestrator-bound
status event MUST have a known organization (the user-facing UX
queries by tenant). Block A enforces non-empty
organization_id with `reasonMissingOrganizationID`. Documented in the
publisher godoc (`PublishStatus` — Block A) and in `doc.go` deliberate
differences.

**R5 — Value-receiver payload + in-method Timestamp + ErrorMessage
rewrites.** *Tension:* mutating an input "looks like a smell" — Go
style usually prefers either a pointer-receiver contract
(caller-visible mutation, explicit) or a "return modified copy" shape.
*Resolution:* `PublishStatus` accepts
`port.LICStatusChangedEvent` by VALUE so the in-method rewrites
operate on the local copy ONLY. The caller-side variable is
byte-for-byte unchanged. This keeps the API contract simple while
ensuring (a) the wire timestamp is always publisher-stamped (no stale
Aggregator timestamps) and (b) the wire ErrorMessage is always the
catalog SSOT (NFR-5.2 — no caller-supplied junk reaches the user). T2
pins the Timestamp half; T18 + T19 pin the ErrorMessage half + the
caller-unchanged guarantee.

**R6 — Status-conditional FAILED-only fields validation.** *Tension:*
a single "stale FAILED-fields leak" branch could be split into three
near-identical branches (one per field). *Resolution:* the combined
`reasonUnexpectedFailureFields` branch — one clear failure with a
self-explanatory name surfaces the root cause to the caller faster
than three branches that each report just one symptom. The FAILED leg
of Block D keeps three separate branches because the caller's fix is
different for each (provide an ErrorCode vs check the IsPublishableTo
list vs supply IsRetryable). T15 / T16 / T17 pin each non-FAILED leg
explicitly so the combined branch is not under-covered.

## Conventions & deliberate decisions (D1..D13, condensed)

- **D1 — ONE exported type.** Currently `StatusPublisher`; future
  siblings (045 ClassificationUncertain, 046 DLQ) will live alongside
  in this same package without re-importing dm.
- **D2 — REQUIRED Publisher in PublisherDeps.** Publisher is the ONE
  Deps field without a noop default — silent swallow on
  lic.events.status-changed would block every status transition.
- **D3 — `Publisher` seam isolates the concrete broker.** R1.
- **D4 — pre-publish validation short-circuit, 4 blocks (A/B/C/D) in
  fixed order.** Each branch emits `PublishOutcomeInvalid` directly
  (R3) and returns a `*PublishError{Reason, Cause: nil}` without
  invoking the broker.
- **D5 — Timestamp via Clock seam, RFC3339Nano UTC.** R5. The
  `systemClock` default returns `time.Now().UTC()`.
- **D6 — ErrorMessage catalog rewrite (FAILED only).** R5.
  `LookupErrorSpec` is the SSOT. Lookup miss → defensive
  `reasonErrorCodeNotInCatalog` (theoretically unreachable after
  Block D step 9).
- **D7 — `classifyOutcome` narrow contract.** R3. Maps a broker.Publish
  return to one of four `PublishOutcome*` values. Validation /
  marshal failures NOT covered — call sites emit `invalid` /
  `failure` directly.
- **D8 — Context errors pass through RAW.** `ctx.Canceled` /
  `ctx.DeadlineExceeded` NOT wrapped in `*PublishError`. The
  codebase-wide convention.
- **D9 — Wire topic hard-coded.** `topicStatusChanged =
  "lic.events.status-changed"` — no env-var override (anti-scope):
  changing a routing key would silently de-route every status
  transition and is a contract break, not a config knob. The wire
  topic is FROZEN at `LIC event-catalog.md` §1.1.
- **D10 — single-call publisher, no fan-out.** Each PublishStatus call
  produces exactly ONE wire message.
- **D11 — Compile-time `var _ Port = ...` assertion in publisher.go.**
  Egress publishers concretely implement a domain port from the
  allowlist.
- **D12 — Stateless after construction.** `PublishStatus` is
  goroutine-safe across any pair of correlationIDs without a mutex. T26
  pins the contract.
- **D13 — Hermetic allowlist EXACTLY 3 entries.** `{model, port,
  broker (sentinels via R2)}`. Active-fail forbidden set incl.
  `internal/config`, `internal/infra/observability/metrics` parent,
  every `internal/application/*` / `internal/ingress/*`,
  `internal/egress/publisher/dm` sibling (R-sibling: each
  Orchestrator-facing publisher owns its own seam stack so 045/046
  can land here without ever depending on dm), every third-party path.
  Reviewer gate: `len(allowedInternal) == 3`.

## Forward notes (recorded, owners elsewhere)

1. **LIC-TASK-036 (pipeline orchestrator wiring).** Constructor call:
   ```go
   statusPub, _ := orch.NewStatusPublisher(
       orch.PublisherConfig{Exchange: cfg.Broker.Exchanges.LICEvents},
       orch.PublisherDeps{
           Publisher: brokerClient,
           Metrics:   publisherMetricsAdapter,
           Clock:     systemClock{},
           Logger:    loggerAdapter,
       })
   ```
   The orchestrator calls `statusPub.PublishStatus` at every
   external-status transition (IN_PROGRESS at start +
   STAGE_AWAITING_USER_CONFIRMATION pause, COMPLETED at terminal
   success, FAILED at terminal failure with a publishable ErrorCode).

2. **LIC-TASK-045 (ClassificationUncertain) and LIC-TASK-046 (DLQ).**
   Slated to land in this same package next to `StatusPublisher` — they
   share the seam stack (Publisher / Metrics / Clock / Logger) and the
   hermetic boundary. The package was named `orch` (not
   `statuspublisher`) precisely so the two future publishers don't
   force a rename.

3. **PublisherMetrics adapter (LIC-TASK-036 / TASK-047).** Tiny adapter
   over `*metrics.PublisherMetrics` that calls
   `MessagesTotal.WithLabelValues(topic, string(outcome)).Inc()` for
   `IncPublish`. The conversion from local `PublishOutcome` (typed) to
   string happens at the adapter boundary, not in this package — keeps
   the counter wiring inside the metrics package where label vocabulary
   is owned. seams_test.go pins the local mirror against the metrics
   SSOT.

4. **`go.mod` side-effects.** orch publisher production imports
   `context`, `encoding/json`, `errors`, `fmt`, `time` (stdlib) plus
   `contractpro/legal-intelligence-core/internal/domain/{model,port}`
   and `.../internal/infra/broker` (sentinels via the Publisher seam).
   No third-party transitive (amqp091 is behind the Publisher seam).
   The seams_test.go file additionally imports
   `.../internal/infra/observability/metrics` for the
   PublishOutcome SSOT pin — `_test` scope only, does not affect the
   production hermeticity. `go mod tidy` produces no diff.

5. **Architecture-doc note.** The hard-coded topic
   (`topicStatusChanged = "lic.events.status-changed"`) matches
   LIC `event-catalog.md` §1.1 + `high-architecture.md` §6.13 +
   `observability.md` §3.9. Any change to the wire topic is a contract
   break and MUST be coordinated with the Orchestrator-side consumer
   wiring; the constant is the single source-of-truth on the LIC side.
