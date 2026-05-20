# dm (egress publisher) Package — CLAUDE.md

Two outbound publishers for the LIC ↔ DM RabbitMQ contracts, sharing a
single hermetic boundary and a single seam stack:

1. **DM Artifact Requester** (LIC-TASK-042, `high-architecture.md` §6.5
   step 1; `integration-contracts.md` §2 + §6.1;
   `DocumentManagement/architecture/event-catalog.md` §1.4;
   `observability.md` §3.9). Publishes `lic.requests.artifacts` — the
   only way LIC asks Document Management for the DP-side artifacts of a
   given version.

2. **DM Analysis Artifacts Publisher** (LIC-TASK-043, `high-architecture.md`
   §6.5 step 8; `integration-contracts.md` §6.1;
   `DocumentManagement/architecture/event-catalog.md` §1.5;
   `LIC/architecture/event-catalog.md` §2; ADR-LIC-05;
   `observability.md` §3.5 + §3.9). Publishes
   `lic.artifacts.analysis-ready` — the TERMINAL publication carrying
   the consolidated payload of all eight mandatory artifacts plus the
   optional v1.1 `risk_delta` extension. DM persists, then emits the
   persist-confirmation that `PersistConfirmationAwaiterPort` awaits
   (§6.5 steps 9-10).

Two exported types, each satisfying ONE structural role:

```
ArtifactRequester:            port.ArtifactRequesterPort           — RequestArtifacts
AnalysisArtifactsPublisher:   port.AnalysisArtifactsPublisherPort  — Publish
```

The compile-time `var _ port.XxxPort = (*Impl)(nil)` assertions live in
the respective implementation files (`requester.go` /
`publisher.go`) — egress publishers concretely implement domain ports
(unlike the dmawaiter case where the `var _` lives at LIC-TASK-047
because the router Deliverer port is in `internal/ingress/router` and
asserting it locally would force a forbidden import).

Constructors: `NewArtifactRequester(RequesterConfig, RequesterDeps)
(*ArtifactRequester, error)` and `NewAnalysisArtifactsPublisher(
PublisherConfig, PublisherDeps) (*AnalysisArtifactsPublisher, error)`
(`NewTypeName`, `feedback_constructors.md`). Fail-fast on invalid Config
/ nil-Publisher via `errors.Join` (the `dmawaiter.NewArtifactAwaiter` /
`pendingconfirmation.NewManager` / `pipeline.NewOrchestrator`
precedent). Immutable and stateless after construction; both hot paths
(`RequestArtifacts` / `Publish`) are goroutine-safe across distinct
correlation_ids (the only shared state is the broker `Publisher` seam,
which itself serializes publishes internally on its `pubMu`).

The orchestrator-side caller pattern (LIC-TASK-036, forthcoming) wraps
each publish in its own `context.WithTimeout` and (for the artifact
requester only) Registers the correlation_id on the
`dmawaiter.ArtifactAwaiter` BEFORE the call (build-spec D10 + dmawaiter
D12 — Register-before-publish is the contract). For
AnalysisArtifactsPublisher, the orchestrator Registers the job_id on
`PersistConfirmationAwaiterPort` BEFORE the call (the same
Register-before-publish contract for the persist-confirmation path).

## Files

- **doc.go** — package godoc only: hermetic statement
  (integration-contracts.md §2 + §6.1 reference), the two-publisher
  attribution block (D1..D15 + R1..R3 for 042, D1..D18 + R1..R5 for 043),
  the three-entry allowlist enumeration.
- **requester.go** — `ArtifactRequester` struct, `RequesterConfig` +
  `validate()` (build-spec D2), `NewArtifactRequester` (build-spec
  D2/D9), `RequestArtifacts` (build-spec D4/D5/D6/D7/D8),
  `failValidation` private helper, the constant
  `topicArtifactsRequest = "lic.requests.artifacts"` (build-spec D9 —
  hardcoded), the compile-time `var _ port.ArtifactRequesterPort =
  (*ArtifactRequester)(nil)` assertion (build-spec D11), the
  package-level `marshalRequest = json.Marshal` test-overridable seam.
- **publisher.go** — `AnalysisArtifactsPublisher` struct,
  `PublisherConfig` + `validate()` (LIC-TASK-043 D2),
  `NewAnalysisArtifactsPublisher` (D2/D9/D10), `Publish` (D4/D5/D6/D7/D8
  + the eight artifact-pointer required-field branches + the
  ObservePublishedSize size-after-marshal hook), `failValidation` private
  helper, the constant `topicAnalysisReady =
  "lic.artifacts.analysis-ready"` (D6/D9 — hardcoded), the compile-time
  `var _ port.AnalysisArtifactsPublisherPort =
  (*AnalysisArtifactsPublisher)(nil)` assertion (D11), the
  package-level `marshalArtifacts = json.Marshal` test-overridable seam.
- **seams.go** — `Publisher` interface (the broker seam, REQUIRED —
  no noop default for either publisher), `PublishOutcome` typed enum
  (LOCAL MIRROR of metrics.PublishOutcome — the universal base.Outcome
  / router.CallOutcome / cost / schemavalidator local-mirror precedent;
  keeps production source hermetic), `Metrics` + `noopMetrics` (TWO
  methods: `IncPublish(topic, outcome)` — both publishers, build-spec
  D7 single-site emission; AND `ObservePublishedSize(bytes)` — only the
  AnalysisArtifactsPublisher, observability.md §3.5 histogram —
  documented in the seam godoc), `Clock` + `systemClock` (UTC), `Logger`
  + `noopLogger` (Warn/Error ONLY — build-spec D15: NO Info, NO §11.2
  audit mandate; reserved for future use, NOT actively called on the hot
  path). `var _ Seam = noop{}` after each pair (the universal
  pendingconfirmation / dmawaiter / router precedent).
- **seams_test.go** — `TestPublishOutcome_WireStringsPinned`: pins
  the four local PublishOutcome strings against
  `metrics.PublishOutcome` SSOT (observability.md §3.9 / metrics/
  labels.go:170-177) so the mirror cannot silently drift. This is the
  ONLY file in this package that imports
  internal/infra/observability/metrics, and it is a _test file, so
  package hermeticity holds.
- **deps.go** — `RequesterDeps{Publisher, Metrics, Clock, Logger}` +
  `PublisherDeps{Publisher, Metrics, Clock, Logger}` (symmetric shape,
  distinct types for call-site documentation) — Publisher REQUIRED for
  BOTH (no noop default; silent-swallow would block awaiters / lose the
  terminal payload), the three others optional with noop defaults
  (build-spec D2/D10). Each type has a `withDefaults()` substituting
  nil-optional → noop; the constructors' non-nil Publisher checks run
  AFTER `withDefaults` and are the authoritative wiring-defect signal.
- **errors.go** — `PublishError{Reason, Cause}` + `Error()` /
  `Unwrap()` (build-spec D4/D6), TWO reason-constant blocks:
    1. Requester block (LIC-TASK-042): seven snake_case identifiers
       (`reasonMissingCorrelationID`, `reasonMissingJobID`,
       `reasonMissingDocumentID`, `reasonMissingVersionID`,
       `reasonMissingArtifactTypes`, `reasonInvalidArtifactType`,
       `reasonMarshalFailure`). The first four ID reasons + the
       marshal-failure reason are REUSED by the AnalysisArtifactsPublisher.
    2. AnalysisArtifactsPublisher block (LIC-TASK-043): eight
       snake_case identifiers specific to the LegalAnalysisArtifactsReady
       payload's required artifact-pointer fields
       (`reasonMissingClassificationResult`, `reasonMissingKeyParameters`,
       `reasonMissingRiskAnalysis`, `reasonMissingRiskProfile`,
       `reasonMissingRecommendations`, `reasonMissingSummary`,
       `reasonMissingDetailedReport`, `reasonMissingAggregateScore`).
  The `classifyOutcome(err) PublishOutcome` broker-outcome classifier
  (build-spec D7 branches) is SHARED by both publishers — narrow contract
  ("given a broker.Publish return, what's the outcome label?"; validation
  failures are emitted at the call site, R3). Returns the local
  `PublishOutcome` mirror.
- **requester_test.go** — full ArtifactRequester behavioural suite
  T1..T17 + T-CTOR-1..3 + `TestClassifyOutcome_AllBranches` +
  `TestPublishError_ErrorAndUnwrap` with in-package fakes for every
  seam (`fakePublisher` / `fakeMetrics` / `fakeClock` / `fakeLogger`).
  `fakeMetrics.ObservePublishedSize` is a noop (the requester does NOT
  call it — the histogram is specific to LIC-TASK-043).
- **publisher_test.go** — full AnalysisArtifactsPublisher behavioural
  suite T1..T29 + T-CTOR-1..4. Reuses `fakePublisher` / `fakeClock` /
  `fakeLogger` from requester_test.go (same package); adds
  `pubFakeMetrics` that captures both `IncPublish` records AND
  `ObservePublishedSize` observations for histogram-call assertions.
  T-CTOR-4 pins the errors.Join "both defects surface together"
  contract. The marshal-failure leg uses the package-level
  `marshalArtifacts` seam.
- **internal_test.go** — `TestHermeticImports` (allowlist size EXACTLY
  3 — `{model, port, broker}` (the local `PublishOutcome` mirror lives
  in seams.go and is pinned in seams_test.go against the metrics
  package SSOT — production source therefore does not import
  `internal/infra/observability/metrics`), reviewer gate; active-fail
  forbidden set per build-spec D13 incl. `internal/config`,
  `internal/infra/observability/metrics` parent, every other
  `internal/application/*` / `internal/ingress/*` / third-party) +
  `TestGofmtClean` (`go/format`; the sandbox blocks `go fmt`).
  Automatically scans `publisher.go` alongside `requester.go` so the new
  AnalysisArtifactsPublisher is held to the same import contract.
- **CLAUDE.md** — this file.

## API

### ArtifactRequester (LIC-TASK-042)

- `NewArtifactRequester(RequesterConfig, RequesterDeps)
  (*ArtifactRequester, error)` — returns the wiring-defect error from
  `errors.Join` if `RequesterConfig.Exchange == ""` and/or
  `RequesterDeps.Publisher == nil`.
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
- `RequesterConfig{Exchange}` (validated `!= ""`).
- `RequesterDeps{Publisher, Metrics, Clock, Logger}` — Publisher
  REQUIRED; the rest optional (nil ⇒ noop).
- ObservePublishedSize is NEVER called by this publisher (the histogram
  is specific to AnalysisArtifactsPublisher per observability.md §3.5).

### AnalysisArtifactsPublisher (LIC-TASK-043)

- `NewAnalysisArtifactsPublisher(PublisherConfig, PublisherDeps)
  (*AnalysisArtifactsPublisher, error)` — returns the wiring-defect
  error from `errors.Join` if `PublisherConfig.Exchange == ""` and/or
  `PublisherDeps.Publisher == nil` (both defects surface together — T-CTOR-4).
- `(*AnalysisArtifactsPublisher) Publish(ctx,
  port.LegalAnalysisArtifactsReady) error`:
  - Validation failure ⇒ `(*PublishError){Reason: reasonMissing*,
    Cause: nil}`; metric `PublishOutcomeInvalid`;
    `ObservePublishedSize` NOT called; broker.Publish NOT called.
    Required fields (12 branches): correlationID / jobID / documentID
    / versionID (4 envelope IDs), ClassificationResult / KeyParameters
    / RiskAnalysis / RiskProfile / Recommendations (NON-NIL slice;
    empty len==0 is VALID) / Summary / DetailedReport / AggregateScore
    (8 artifact pointers).
    Optional fields (NOT validated): organizationID (omitempty),
    RiskDelta (omitempty pointer).
  - Marshal failure ⇒ `(*PublishError){Reason: reasonMarshalFailure,
    Cause: err}`; metric `PublishOutcomeFailure`;
    `ObservePublishedSize` NOT called; broker.Publish NOT called.
  - Broker NACK ⇒ raw err; metric `PublishOutcomeNacked`;
    `ObservePublishedSize` called exactly once with the wire-bytes
    length (the payload reached the wire boundary).
  - Broker ConfirmTimeout / NotConnected / non-retryable / unknown ⇒
    raw err; metric `PublishOutcomeFailure`; `ObservePublishedSize`
    called exactly once.
  - ctx.Canceled / ctx.DeadlineExceeded ⇒ raw ctx.Err() pass-through;
    metric `PublishOutcomeFailure`; `ObservePublishedSize` called
    exactly once.
  - Success ⇒ nil; metric `PublishOutcomeSuccess`;
    `ObservePublishedSize` called exactly once.
- The `payload.Timestamp` is REWRITTEN inside the publisher with
  `clock.Now().Format(time.RFC3339Nano)` (UTC). The caller-side
  variable is UNCHANGED because `Publish` accepts the envelope by
  VALUE (T2 pins both sides of the contract).
- `PublisherConfig{Exchange}` (validated `!= ""`).
- `PublisherDeps{Publisher, Metrics, Clock, Logger}` — Publisher
  REQUIRED; the rest optional (nil ⇒ noop).

### Shared seams

- `Publisher.Publish(ctx, exchange, routingKey, payload) error` —
  matches `broker.Client.Publish` signature exactly.
- `Metrics.IncPublish(topic string, outcome PublishOutcome)` — the
  single seam method that bridges to
  `lic_publisher_messages_total{topic,outcome}.Inc` at the 036/047
  adapter. The conversion from local `PublishOutcome` → string for
  the prometheus label happens at the adapter boundary.
- `Metrics.ObservePublishedSize(bytes int)` — bridges to the
  `lic_dm_artifacts_published_size_bytes` histogram
  (observability.md §3.5). Called ONLY by AnalysisArtifactsPublisher;
  ArtifactRequester's `IncPublish`-only counter path is unaffected.
- `Clock.Now()`; `Logger.Warn` / `Logger.Error` — see seams.go.
- `PublishError{Reason, Cause}` + `Error()` / `Unwrap()`.
- `classifyOutcome(err) PublishOutcome` — package-private, exercised
  by both publishers; covered by `TestClassifyOutcome_AllBranches`.

## Reconciliations (build-spec PART B — DEFECT-style, condensed)

**R1 — `Publisher` seam vs concrete `*broker.Client` (build-spec
D3).** *Tension:* the broker is a real infra dependency both publishers
need at runtime; importing `*broker.Client` would bring amqp091 into
the package and break the hermetic statement. *Resolution:* a 1-method
local `Publisher` interface whose signature is byte-identical to
`broker.Client.Publish` (publish.go:36). LIC-TASK-036/047 wires the
real `*broker.Client`; tests pass `fakePublisher`. The seam DOES NOT
hide the broker error TYPES — those still flow back raw and are
inspected via `errors.Is(err, broker.ErrPublishNack)` etc. The
documented R2 exception below covers that.

**R2 — TWO `internal/infra/broker` imports despite the
"hermeticity" statement (build-spec D13).** *Tension:* the three-entry
allowlist looks like a contradiction — two "pure" entries (domain/model,
domain/port) and one infra entry. *Resolution:* the `broker` import is
restricted to TWO sentinels (`ErrPublishNack`, `ErrConfirmTimeout`)
plus the `BrokerError` type traversed by `errors.As` in the classifier.
The concrete `*broker.Client` is behind the `Publisher` seam (R1), so
amqp091 does NOT transitively land in the publisher package. Recorded
as a deliberate exception to the otherwise infra-free egress allowlist;
a future "make broker sentinels live in port" pass could remove the
exception entirely.

**R3 — Validation-failure metric emission is OUT-OF-BAND of
`classifyOutcome` (build-spec D7).** *Tension:* the classifier covers
broker outcomes only; a strict "classifier covers all metric paths"
reading would force the classifier to take a validation-failure
parameter and bucket as `invalid`. *Resolution:* the call site
(`failValidation` in both publishers) emits `PublishOutcomeInvalid`
DIRECTLY before returning the `*PublishError`. The classifier stays
narrow: "given a broker.Publish return, what's the outcome label?" —
this is the dmawaiter D11 classifier-narrow-by-design precedent (the
awaiter classifiers also take a single broker-shape input, not a
wide-union of validation + broker errors).
`TestClassifyOutcome_AllBranches` pins the broker-only contract;
T3..T7 + T8 (requester) and T3..T14 + T15 (publisher) pin the
validation-emits-invalid-directly contract.

**R4 — ObservePublishedSize on FAILURE-AFTER-MARSHAL (LIC-TASK-043
build-spec).** *Tension:* "size observed" looks naturally tied to "success",
not "the payload reached the wire boundary but the broker nacked". A
strict "size = success" reading would call ObservePublishedSize only
when broker.Publish returned nil. *Resolution:* the §3.5 histogram
measures the wire-size distribution of every payload that crossed the
publisher's wire boundary; broker.Publish either acks, nacks or
times-out the SAME byte stream that was marshalled. Observing only on
success would bias the histogram toward broker-good days and erase the
nack/timeout payloads — exactly the data point ops needs to correlate
"large payloads" with "broker pressure". The histogram is therefore
observed UNCONDITIONALLY after marshal-success, and the IncPublish
outcome label provides the orthogonal success/failure breakdown. T28
pins the contract.

**R5 — Value-receiver payload + in-method Timestamp rewrite
(LIC-TASK-043 build-spec D5).** *Tension:* mutating an input "looks
like a smell" — Go style usually prefers either a pointer-receiver
contract (caller-visible mutation, explicit) or a "return modified copy"
shape. *Resolution:* `Publish` accepts `port.LegalAnalysisArtifactsReady`
by VALUE so the in-method `payload.Timestamp = ...` rewrite operates
on the local copy ONLY. The caller-side variable is byte-for-byte
unchanged. This keeps the API contract simple (one method, one
value-arg) while ensuring the wire timestamp is always
publisher-stamped (NOT trusting Aggregator-supplied values, which may
be stale by the time the broker accepts the publish). T2 pins both
halves of the contract (wire timestamp = clock.Now; caller variable
unchanged).

## Conventions & deliberate decisions (build-spec D1..D18, condensed)

- **D1/D2 — TWO exported types + REQUIRED Publisher in each Deps.**
  Each publisher has no internal state worth threading through generic
  helpers (no per-key registry, no TTL); a single
  `Xxx{cfg,publisher,metrics,clock,log}` is sufficient. Publisher is
  the ONE Deps field without a noop default — silent swallow on either
  topic would block the corresponding awaiter forever without a single
  log line or metric. The constructors enforce non-nil via
  `errors.Join` after `withDefaults`. Symmetric naming
  (`RequesterConfig` + `RequesterDeps`, `PublisherConfig` +
  `PublisherDeps`) makes the call-site documentation self-evident at
  LIC-TASK-036 / TASK-047 wiring time.
- **D3 — `Publisher` seam isolates the concrete broker.** Local
  1-method interface matching `broker.Client.Publish` byte-for-byte.
  Keeps amqp091 out of the publisher package via R1; broker error
  TYPES still flow back raw (R2). Same seam serves both publishers.
- **D4 — pre-publish validation short-circuit.**
  Requester: required correlationID, jobID, documentID, versionID,
  artifactTypes (non-empty + each `IsValid()`); optional organizationID.
  AnalysisArtifactsPublisher: required correlationID, jobID, documentID,
  versionID (envelope IDs) + ClassificationResult, KeyParameters,
  RiskAnalysis, RiskProfile, Recommendations (NON-NIL slice; empty
  valid), Summary, DetailedReport, AggregateScore (eight artifact
  pointers); optional organizationID + RiskDelta. Each branch emits
  `PublishOutcomeInvalid` directly (R3) and returns a
  `*PublishError{Reason, Cause: nil}` without invoking the broker. For
  AnalysisArtifactsPublisher, `ObservePublishedSize` is ALSO NOT
  called on validation-fail (no bytes were produced).
- **D5 — Timestamp via Clock seam.** `time.RFC3339Nano` UTC. The
  `systemClock` default returns `time.Now().UTC()`.
  AnalysisArtifactsPublisher rewrites `payload.Timestamp` IN-METHOD
  on a value-receiver copy; the caller variable is unchanged (R5 + T2).
  ArtifactRequester constructs the envelope in-method so there is no
  caller-side timestamp to consider.
- **D6 — `encoding/json` stdlib + typed-model values.** OrganizationID
  carries `omitempty` in both envelopes; RiskDelta carries `omitempty`
  in LegalAnalysisArtifactsReady. Marshal failure ⇒
  `*PublishError{Reason: reasonMarshalFailure, Cause: err}` (NOT
  invalid — defensive serialisation defect). The package-level
  `marshalRequest` / `marshalArtifacts` seams are test-overridable for
  the otherwise-unreachable branches.
- **D7 — `classifyOutcome` narrow contract (R3).** Maps a
  broker.Publish return to one of four `PublishOutcome*` (local mirror)
  values. Validation failures NOT covered — call sites emit `invalid`
  directly. The metric is ALWAYS incremented exactly once per
  publish call (no silent exit path). Shared by both publishers.
- **D8 — Context errors pass through RAW.** `ctx.Canceled` /
  `ctx.DeadlineExceeded` from `broker.Publish` returns are NOT
  wrapped in `*PublishError`. The codebase-wide convention
  (broker/errors.go:107; dmawaiter D13; the LLM adapters' treatment).
  The metric still buckets them as `failure` per `classifyOutcome`
  (the request did not produce an ack).
- **D9 — Wire topics hard-coded.** `topicArtifactsRequest =
  "lic.requests.artifacts"` and `topicAnalysisReady =
  "lic.artifacts.analysis-ready"` — no env-var override (build-spec
  D15 anti-scope): changing a routing key would silently de-route
  every publication on that topic and is a contract break, not a
  config knob. The wire topics are FROZEN at integration-contracts
  §6.1 / DM event-catalog §1.4 + §1.5.
- **D10 — single-call publisher, no fan-out.** Each publish
  invocation produces exactly ONE wire message. RE_CHECK base+parent
  fan-out belongs to the pipeline orchestrator (LIC-TASK-036) — it
  issues TWO RequestArtifacts calls with per-stage correlationID
  suffixes (e.g. `<base>:current`, `<base>:parent`). Per pipeline
  job there is exactly ONE AnalysisArtifactsPublisher.Publish call.
- **D11 — Compile-time `var _ Port = ...` assertions in implementation
  files.** Unlike dmawaiter (where the var _ lives at 047 because
  asserting the router Deliverer port locally would force a forbidden
  import), egress publishers concretely implement a domain port from
  the allowlist — the assertions are 1-line guarantees.
- **D12 — Stateless after construction.** Both `RequestArtifacts`
  and `Publish` are goroutine-safe across any pair of correlationIDs
  without a mutex. The shared state is the broker `Publisher` seam,
  which itself serializes publishes internally on its `pubMu`. T17
  (requester) + T27 (publisher) pin the contract.
- **D13 — Hermetic allowlist EXACTLY 3 entries** (codebase reality:
  the spec'd `metrics/labels` subpackage does not exist — `labels.go`
  lives inside the `metrics` parent — so `PublishOutcome` is a
  local mirror in seams.go pinned in seams_test.go against the
  metrics SSOT, per the universal `base.Outcome` /
  `router.CallOutcome` / `cost.Outcome` / `schemavalidator.RepairOutcome`
  precedent). `{model, port, broker (sentinels via R2)}`. Active-fail
  forbidden set incl. `internal/config` (local Configs —
  036/047-injected), `internal/infra/observability/metrics` parent
  (would pull prometheus dep into production source — the metrics
  import lives in seams_test.go only), every `internal/application/*` /
  `internal/ingress/*` (orthogonal layers — the orchestrator imports
  this package, not vice versa), every third-party path. Reviewer
  gate: `len(allowedInternal) == 3`.
- **D14 — Tests.** ArtifactRequester: T1..T17 + T-CTOR-1..3 (see 042
  CLAUDE.md). AnalysisArtifactsPublisher: T1 (success: exchange +
  routingKey + payload + 5 IDs + 8 artifacts + organization present +
  RiskDelta omitted + metric=success + size observed once), T2
  (timestamp rewrite + caller-side unchanged), T3..T6 (4 ID
  validation failures), T7..T14 (8 artifact-nil validation failures),
  T15 (Recommendations nil → invalid), T16 (Recommendations empty
  slice → success), T17 (OrganizationID omitempty empty+present), T18
  (RiskDelta omitempty nil+present), T19 (marshal-failure via the
  package-level seam), T20..T26 (7 broker outcomes: success, ctx
  errors, NACK, ConfirmTimeout, NotConnected, non-retryable), T27 (16
  concurrent goroutines, `-race` clean), T28 (ObservePublishedSize
  on broker-fail-after-marshal), T29 (ObservePublishedSize NOT called
  on validation/marshal fail), T-CTOR-1..3 + T-CTOR-4 (both-defects
  errors.Join). `TestClassifyOutcome_AllBranches` /
  `TestPublishError_ErrorAndUnwrap` are shared with the requester
  suite (same package).
- **D15 — Anti-scope (explicitly NOT in this package).** NO RE_CHECK
  fan-out (D10); NO DLQ routing (egress/dlq is a separate package);
  NO version-meta caching (Redis/orchestrator concern); NO
  correlationID/job_id registry (dmawaiter / pendingconfirmation
  concern); NO retry loop (broker client retries internally; per-publish
  retry is the orchestrator's decision); NO duration histogram (the
  §3.9 counter + §3.5 size histogram are the SSOT publisher metrics);
  NO topic env override (D9); NO sub-packages — both publishers share
  the same package because they share the seam stack and the hermetic
  contract.
- **D16 (LIC-TASK-043) — Symmetric naming.** RequesterConfig /
  RequesterDeps vs PublisherConfig / PublisherDeps. Both share the same
  shape (single Exchange field + four-Deps stack) but are kept as
  distinct types so the call-site documentation at LIC-TASK-036 /
  TASK-047 wiring time reads `dm.NewArtifactRequester(dm.RequesterConfig
  {...}, dm.RequesterDeps{...})` vs `dm.NewAnalysisArtifactsPublisher(
  dm.PublisherConfig{...}, dm.PublisherDeps{...})` — no ambiguity.
- **D17 (LIC-TASK-043) — Metrics seam carries TWO methods.** IncPublish
  (both publishers) and ObservePublishedSize (analysis-ready only).
  ArtifactRequester silently ignores the latter; AnalysisArtifactsPublisher
  exercises both. The fake metrics in the requester suite implements
  ObservePublishedSize as a noop; the publisher suite uses
  `pubFakeMetrics` which captures both call streams.
- **D18 (LIC-TASK-043) — RiskDelta optional, NOT validated.** RiskDelta
  is present only when parent_version_id != null AND parent
  RISK_ANALYSIS was available — strictly an orchestrator concern. The
  publisher accepts nil or non-nil RiskDelta verbatim and lets the
  `omitempty` tag drive the wire shape. T18 pins both legs.
- **gofmt self-check** via `go/format` (sandbox blocks `go fmt`) —
  the dmawaiter / pipeline / aggregator precedent.

## Forward notes (recorded, owners elsewhere)

1. **LIC-TASK-036 (pipeline orchestrator wiring).** Constructor calls:
   ```go
   artifactReq, _ := dm.NewArtifactRequester(
       dm.RequesterConfig{Exchange: cfg.Broker.Exchanges.LICRequests},
       dm.RequesterDeps{
           Publisher: brokerClient,
           Metrics:   publisherMetricsAdapter,
           Clock:     systemClock{},
           Logger:    loggerAdapter,
       })
   analysisPub, _ := dm.NewAnalysisArtifactsPublisher(
       dm.PublisherConfig{Exchange: cfg.Broker.Exchanges.LICArtifacts},
       dm.PublisherDeps{
           Publisher: brokerClient,
           Metrics:   publisherMetricsAdapter,
           Clock:     systemClock{},
           Logger:    loggerAdapter,
       })
   ```
   The orchestrator calls `artifactReq.RequestArtifacts` AFTER
   `dmawaiter.ArtifactAwaiter.Register`, and calls `analysisPub.Publish`
   AFTER `pendingconfirmation.Manager.Register` (the
   Register-before-publish contract for the persist-confirmation path).

2. **LIC-TASK-043 — implemented.** AnalysisArtifactsPublisher lives in
   this package next to ArtifactRequester (build-spec D1 same-package
   decision). Same hermetic boundary; same `Publisher` seam shape;
   shared `Metrics` seam (extended with ObservePublishedSize for the
   §3.5 size histogram). The split into TWO exported types (vs one
   "DMPublisher" with two methods) keeps the role-per-type contract
   clean — each implements ONE domain port.

3. **LIC-TASK-047 (wiring) `var _` assertions.** None needed for
   this package — the compile-time assertions in `requester.go` and
   `publisher.go` already pin `port.ArtifactRequesterPort` and
   `port.AnalysisArtifactsPublisherPort` respectively. The 047 wiring
   sees `*ArtifactRequester` / `*AnalysisArtifactsPublisher` as those
   ports directly.

4. **PublisherMetrics adapter (LIC-TASK-036 / TASK-047).** Tiny
   adapter over `*metrics.PublisherMetrics` that calls
   `MessagesTotal.WithLabelValues(topic, string(outcome)).Inc()` for
   `IncPublish`, and `PublishedSizeBytes.Observe(float64(bytes))` for
   `ObservePublishedSize`. The conversion from local `PublishOutcome`
   (typed) to string happens at the adapter boundary, not in this
   package — keeps the counter wiring inside the metrics package where
   label vocabulary is owned. seams_test.go pins the local mirror
   against the metrics SSOT.

5. **`go.mod` side-effects.** dm publisher production imports
   `context`, `encoding/json`, `errors`, `fmt`, `time` (stdlib) plus
   `contractpro/legal-intelligence-core/internal/domain/{model,port}`
   and `.../internal/infra/broker` (sentinels via the Publisher seam).
   No third-party transitive (amqp091 is behind the Publisher seam).
   The seams_test.go file additionally imports
   `.../internal/infra/observability/metrics` for the
   PublishOutcome SSOT pin — _test scope only, does not affect the
   production hermeticity. `go mod tidy` produces no diff.

6. **Architecture-doc note.** Both hard-coded topics
   (`topicArtifactsRequest = "lic.requests.artifacts"` and
   `topicAnalysisReady = "lic.artifacts.analysis-ready"`) match
   DM/architecture/event-catalog.md §1.4 + §1.5 +
   integration-contracts.md §6.1 + LIC event-catalog.md §2 +
   ADR-LIC-05. Any change to either wire topic is a contract break
   and MUST be coordinated with DM-side consumer wiring; the constants
   are the single source-of-truth on the LIC side.
