# dlq (egress) Package — CLAUDE.md

Outbound DLQ publisher for the four PII-safe LIC dead-letter topics
(LIC-TASK-046). Failures across the consumer / agent / publisher
boundaries are routed here so DLQ depth is the SSOT post-mortem signal
(error-handling.md §9, integration-contracts.md §10, event-catalog.md §3,
observability.md §3.8, security.md §6.4-6.5).

One exported type — `DLQPublisher` — satisfies `port.DLQPublisherPort`
(compile-time `var _` assertion in `publisher.go`). `PublishDLQ` is
goroutine-safe across distinct envelopes; the only shared state is the
broker `Publisher` seam, which serializes publishes internally on its
`pubMu`.

Constructor: `NewDLQPublisher(Config, Deps) (*DLQPublisher, error)`.

## Files

- **doc.go** — package godoc + design summary + 3-entry allowlist
  enumeration.
- **hash.go** — two HMAC primitives:
    - `HashPayload(payload, key) string` — uncapped HMAC-SHA-256 over the
      FULL payload, used for `LICDLQEnvelope.OriginalMessageHash`. Empty
      key returns "" (defense against rainbow-table re-identification).
    - `HashRawLLMResponse(response, key) string` — same HMAC capped at
      `RawLLMResponseHashMaxBytes = 1024` per security.md §6.4 worked
      example. Used for `LICDLQEnvelope.RawLLMResponseHash` in the
      lic.dlq.agent-output-invalid topic. Empty key returns "".
- **publisher.go** — `DLQPublisher` struct, `Config{Exchange}` +
  `validate()`, `NewDLQPublisher`, `PublishDLQ(ctx, topic, envelope)`,
  `topicToReason` (4-arm switch + defensive "unknown" default),
  `failValidation` private helper, package-level `marshalEnvelope =
  json.Marshal` test-overridable seam, compile-time
  `var _ port.DLQPublisherPort = (*DLQPublisher)(nil)` assertion.
- **seams.go** — `Publisher` interface (broker seam, REQUIRED — no noop
  default), `PublishOutcome` typed enum (LOCAL MIRROR of
  `metrics.PublishOutcome`, pinned in `seams_test.go`), `Metrics`
  interface with TWO methods (`IncPublish` — every exit path,
  `IncDLQPublished` — broker-ack SUCCESS only), `noopMetrics`, `Clock` +
  `systemClock` (UTC), `Logger` + `noopLogger` (Warn/Error only — Info
  omitted, no §11.2 audit mandate; NOT called on hot path). The Q6
  asymmetry vs dm/orch publishers is documented on the `Clock` seam
  godoc.
- **deps.go** — `Deps{Publisher, Metrics, Clock, Logger}` bundle.
  Publisher REQUIRED (no noop default); the rest optional with noop
  defaults via `withDefaults()`. Constructor's non-nil Publisher check
  runs AFTER `withDefaults` and is the authoritative wiring-defect signal.
- **errors.go** — `PublishError{Reason, Cause}` + `Error()` / `Unwrap()`,
  TWO constant blocks (validation reasons + topic-derived reasons),
  shared `classifyOutcome(err) PublishOutcome`.
- **hash_test.go** — 10 tests: determinism, key-dependence,
  payload-dependence, empty-key→empty-hash, nil-payload-valid, full-bytes
  not-truncated (HashPayload vs HashRawLLMResponse semantic distinction),
  stdlib HMAC vector pin, cap behaviour at the 1024-byte boundary,
  short-payload no-pad, RawLLMResponseHashMaxBytes constant pin.
- **publisher_test.go** — full DLQPublisher behavioural suite: success-
  with-both-counters, FailedAt auto-stamped + caller-preserved (Q6),
  7 validation branches, best-effort empty IDs, non-publishable ErrorCode
  accepted (Q3), marshal-failure, 6 broker outcomes (nack, timeout,
  not-connected, non-retryable, ctx.Canceled, ctx.DeadlineExceeded), all
  4 topic→reason mappings, optional fields serialised, omitempty contract,
  32×16 concurrency race-clean, 4 constructor cases, classifyOutcome
  8-case exhaustive, PublishError.Error/Unwrap.
- **seams_test.go** — `TestPublishOutcome_WireStringsPinned` (pins local
  PublishOutcome mirror against metrics.PublishOutcome SSOT —
  observability.md §3.9 / metrics/labels.go:170-177);
  `TestDLQReason_TopicMappingExhaustive` (actively invokes
  `topicToReason` for all four topics — code-reviewer M1 strengthens the
  drift detector); `TestTopicToReason_DefensiveDefault` (exercises the
  unreachable "unknown" arm — code-reviewer M2). This is the ONLY file
  in this package that imports `internal/infra/observability/metrics`,
  and it is a `_test.go` file, so package hermeticity holds.
- **internal_test.go** — `TestHermeticImports` (3-entry allowlist
  + active forbidden set incl. `internal/config`, `metrics` parent,
  `logger`, every `internal/application/*` / `internal/ingress/*` /
  sibling `internal/egress/publisher/*` / third-party) +
  `TestGofmtClean` (`go/format` self-check; the sandbox blocks `go fmt`).
- **CLAUDE.md** — this file.

## API

### DLQPublisher (LIC-TASK-046)

- `NewDLQPublisher(Config, Deps) (*DLQPublisher, error)` — returns the
  wiring-defect error from `errors.Join` if `Config.Exchange == ""`
  and/or `Deps.Publisher == nil` (both defects surface together —
  T-CTOR-3).
- `(*DLQPublisher) PublishDLQ(ctx, port.DLQTopic, port.LICDLQEnvelope)
  error`:
  - Validation failure ⇒ `(*PublishError){Reason: reasonXxx, Cause: nil}`;
    metric `IncPublish(topic, PublishOutcomeInvalid)`; broker.Publish
    NOT called; `IncDLQPublished` NOT called.
    Validation branches (7):
    1. `topic.IsValid()` — one of the four declared DLQ topics.
    2. `envelope.OriginalTopic != ""`.
    3. `envelope.OriginalMessageHash != ""` (caller computed via
       `HashPayload` over the FULL raw bytes keyed by LIC_DLQ_HASH_KEY).
    4. `envelope.ErrorCode != ""` — NOT validated against the
       ErrorCatalog or IsPublishableToOrchestrator (DLQ catches ALL
       terminal errors INCLUDING non-publishable codes).
    5. `envelope.ErrorMessage != ""`.
    6. `envelope.RetryCount >= 0`.
    7. `envelope.OriginalMessageSizeBytes >= 0`.
    Best-effort (NOT validated): correlation_id / job_id / document_id /
    version_id / organization_id (per integration-contracts §10.1 —
    "when an invalid-message envelope fails JSON parsing entirely the
    publisher leaves them empty"); agent_id / stage / raw_llm_response_hash
    / payload_storage_key (defensive optional set per event-catalog §3.1).
  - Marshal failure ⇒ `(*PublishError){Reason: reasonMarshalFailure,
    Cause: err}`; metric `IncPublish(topic, PublishOutcomeFailure)`;
    broker.Publish NOT called; `IncDLQPublished` NOT called. Defensive —
    unreachable for compliant inputs.
  - Broker NACK ⇒ raw err (`errors.Is(err, broker.ErrPublishNack)`
    holds); `IncPublish(topic, PublishOutcomeNacked)`; `IncDLQPublished`
    NOT called.
  - Broker ConfirmTimeout / NotConnected / non-retryable AMQP /
    unknown ⇒ raw err; `IncPublish(topic, PublishOutcomeFailure)`;
    `IncDLQPublished` NOT called.
  - ctx.Canceled / ctx.DeadlineExceeded ⇒ raw ctx.Err() pass-through
    (NOT wrapped in `*PublishError` — codebase-wide convention);
    `IncPublish(topic, PublishOutcomeFailure)`; `IncDLQPublished` NOT
    called.
  - Success ⇒ nil; `IncPublish(topic, PublishOutcomeSuccess)` AND
    `IncDLQPublished(topic, reason)` — BOTH counters bump exactly once.
- In-method rewrite (after validation passes):
  - `evt.FailedAt` auto-stamp ONLY when EMPTY: `evt.FailedAt =
    clock.Now().Format(time.RFC3339Nano)`. If the caller pre-set
    FailedAt (semantically — "when did the failure HAPPEN"), it is
    preserved. T2 / T3 pin both halves. **This is a deliberate
    asymmetry vs the dm / orch publishers' always-overwrite Timestamp
    pattern** (architect Q6).
- `Config{Exchange}` (validated `!= ""`).
- `Deps{Publisher, Metrics, Clock, Logger}` — Publisher REQUIRED; the
  rest optional (nil ⇒ noop).

### Hash helpers

- `HashPayload(payload []byte, key []byte) string` — uncapped HMAC-SHA-256;
  empty key returns "". Used for OriginalMessageHash.
- `HashRawLLMResponse(response []byte, key []byte) string` — 1024-byte-
  capped HMAC-SHA-256 per security.md §6.4 worked example; empty key
  returns "". Used for RawLLMResponseHash (agent-output-invalid only).
- `RawLLMResponseHashMaxBytes = 1024` — pinned by
  `hash_test.go::TestHashRawLLMResponseHashMaxBytes_Pinned`.

Note on cap units (code-reviewer L1): the 1024-byte cap is BYTE-based,
not codepoint-based. security.md §6.4 says "first 1024 chars" but the
worked example (`rawResponse[:min(len(rawResponse), 1024)]`) is
byte-indexed; we follow the example as the authoritative interpretation.
For ASCII the distinction vanishes; for Russian Cyrillic (the actual
LLM response domain) "1024 chars" is ≤2048 bytes. Multi-byte rune
straddling byte 1024 is split mid-codepoint — fine because we hash
bytes, never display them. The dedup contract is preserved (two
payloads sharing the first 1024 bytes hash identically).

### Shared seams

- `Publisher.Publish(ctx, exchange, routingKey, payload) error` —
  matches `broker.Client.Publish` signature exactly.
- `Metrics.IncPublish(topic, outcome)` — bridges to
  `lic_publisher_messages_total{topic,outcome}.Inc` (observability.md
  §3.9); UNCONDITIONAL on every PublishDLQ exit path.
- `Metrics.IncDLQPublished(topic, reason)` — bridges to
  `lic_dlq_published_total{topic,reason}.Inc` (observability.md §3.8);
  called ONLY on broker-ack success per the §11 LICDLQGrowth alert
  semantic ("envelopes that REACHED the DLQ").
- `Clock.Now()` — UTC wall time for the FailedAt-if-empty stamp.
- `Logger.Warn` / `Logger.Error` — see seams.go.Logger godoc; NOT
  actively called on the hot path (architect Q5).
- `PublishError{Reason, Cause}` + `Error()` / `Unwrap()`.
- `classifyOutcome(err) PublishOutcome` — package-private, covered by
  `TestClassifyOutcome_AllBranches`.

## Reconciliations (architect Q1..Q6, DEFECT-style, condensed)

**R-Q1 — Hash helpers co-located in this package.** *Tension:*
`logger.HashContent` already exists and could be reused or extended.
*Resolution:* the two hashes have semantically distinct contracts —
`original_message_hash` is uncapped over the FULL payload while
`raw_llm_response_hash` is the 1024-byte log primitive. Mixing them
under one package risks silent misuse (a future caller passing a full
payload to a capped helper would get a meaningless forensic trail).
Co-locating `HashPayload` here preserves the 3-entry hermetic allowlist
(no `internal/infra/observability/logger` import) and matches the dm/orch
local-mirror precedent.

**R-Q2 — 1:1 topic→reason mapping (4 reasons, 4 emitted series).**
*Tension:* observability.md §3.10 estimates "DLQ: 4×3 = 12" — three
reasons, suggesting two topics should share a reason. *Resolution:*
each topic gets a distinct, diagnostic reason (`invalid_message`,
`consumer_failed`, `publish_failed`, `agent_output_invalid`) — the
emitted series count is exactly 4 (one labelled cell per topic), within
the §3.10 estimated budget and well within the 1500-series instance cap.
Collapsing topics for the sake of a 3-vs-4 estimate would lose
diagnostic signal at the dashboard layer for no measurable cardinality
gain.

**R-Q3 — Best-effort correlation IDs.** *Tension:* requiring at least
one ID (e.g. correlation_id) would force the consumer to fabricate a
placeholder UUID for parse-failed inbound messages, polluting forensics
with synthetic data. *Resolution:* the validation enforces the
FORENSIC INVARIANT (what failed + why + how to dedupe) without forcing
the CORRELATION INVARIANT (join to originating job). The 5-ID set is
omitempty on the wire and best-effort per integration-contracts §10.1.
ErrorCode is NOT validated against the ErrorCatalog — DLQ deliberately
catches non-publishable codes (INVALID_MESSAGE_SCHEMA,
INVALID_ORG_ID_MISMATCH, IDEMPOTENCY_STORE_UNAVAILABLE) that are
excluded from the Orchestrator-status catalog.

**R-Q4 — Caller logs raw failed payload BEFORE PublishDLQ; publisher
never sees PII.** *Tension:* the task spec says "Для
lic.dlq.publish-failed: full payload (LegalAnalysisArtifactsReady с PII)
логируется как warning + alert" — could be read as a publisher-side
mandate. *Resolution:* the publisher receives only the PII-safe envelope
and has NO access to the raw payload — so "log the raw payload" is
physically a caller-side action. Smearing it across both packages
violates separation of concerns. The "warning + alert" intent is
satisfied by (i) the caller's structured WARN with the raw payload
(already its responsibility, it's the one that saw the publish fail),
(ii) `lic_dlq_published_total{topic="lic.dlq.publish-failed"}` feeding
the §11 LICDLQGrowth alert, and (iii) the §10.2 v1-optional object
storage retention.

**R-Q5 — Logger.Warn / Logger.Error reserved for future use, NOT
called on the hot path.** *Tension:* the DLQ publisher is the OUTPUT for
failed signals — should it not log? *Resolution:* the metric IS the log,
in the structured-counter sense — `lic_dlq_published_total` is
unrate-limited, structured, and alertable. The sibling dm/orch
publishers explicitly document Logger as "reserved for future use"
(build-spec D15) and reserve it for future operator-visible WARN sites
(e.g. broker-NACK telemetry once §3.9 widens). All three publishers
should widen together when that day comes — not the dlq publisher alone.

**R-Q6 — FailedAt asymmetry: preserved-if-set, stamped-if-empty.**
*Tension:* the dm / orch publishers ALWAYS overwrite the envelope
Timestamp from `clock.Now()`. Consistency would suggest the same here.
*Resolution:* `failed_at` is semantically "when did the failure
HAPPEN" (caller knows, can pre-fill), not "when did this envelope
leave the publisher". A publish-failed envelope stamped at DLQ-publish
time would misattribute the failure window for LICPipelineFailureRate
(§11) if the caller buffered the failure across a broker reconnect. The
caller-set path (publish-failed / consumer-failed) and the
empty-auto-stamped path (invalid-message — time-of-failure ≈
time-of-DLQ-publish) are BOTH supported. T2 / T3 pin both halves.

## Conventions & deliberate decisions

- **One exported type.** `DLQPublisher` — symmetric with the dm/orch
  packages where each publisher is a separate exported type.
- **REQUIRED Publisher in Deps.** Publisher is the ONE Deps field
  without a noop default. Silent-swallow on lic.dlq.* would erase every
  DLQ envelope and defeat the §11 alert + §9.3 post-mortem.
- **`Publisher` seam isolates the concrete broker.** Local 1-method
  interface matching `broker.Client.Publish` byte-for-byte. Keeps
  amqp091 out of the publisher package; broker error TYPES still flow
  back raw and are inspected via `errors.Is/As`.
- **Pre-publish validation short-circuit.** 7 branches in Block A/B.
  Each branch emits `PublishOutcomeInvalid` directly via
  `failValidation` and returns `*PublishError{Reason, Cause: nil}`
  without invoking the broker.
- **FailedAt via Clock seam, asymmetric (Q6).** RFC3339Nano UTC. Only
  stamped if empty.
- **`classifyOutcome` narrow contract.** Maps a broker.Publish return
  to one of four `PublishOutcome*` values. Validation/marshal failures
  emit `invalid`/`failure` directly at the call site — not through
  the classifier.
- **Context errors pass through RAW.** `ctx.Canceled` /
  `ctx.DeadlineExceeded` NOT wrapped in `*PublishError`.
- **Wire topics hard-coded.** Routing key = literal DLQ topic string
  (e.g. "lic.dlq.invalid-message"). Exchange = `Config.Exchange`
  (LIC-TASK-047 wires `config.BrokerConfig.ExchangeDLX`).
- **Single-call publisher, no fan-out.** Each PublishDLQ produces
  exactly ONE wire message.
- **Compile-time `var _ port.DLQPublisherPort = (*DLQPublisher)(nil)`
  assertion in publisher.go.**
- **Stateless after construction.** PublishDLQ is goroutine-safe
  across distinct envelopes without a mutex (32×16 race test pins).
- **Hermetic allowlist EXACTLY 3 entries** — `{model, port, broker
  (sentinels)}`. Reviewer gate at `internal_test.go`.

## Forward notes (recorded, owners elsewhere)

1. **LIC-TASK-047 (application wiring).** Constructor call:
   ```go
   dlqPub, _ := dlq.NewDLQPublisher(
       dlq.Config{Exchange: cfg.Broker.ExchangeDLX},
       dlq.Deps{
           Publisher: brokerClient,
           Metrics:   dlqMetricsAdapter,
           Clock:     systemClock{},
           Logger:    loggerAdapter,
       })
   ```
   The orchestrator (LIC-TASK-036) and consumer (LIC-TASK-043) call
   `dlqPub.PublishDLQ` on terminal failure paths.

2. **Metrics adapter (LIC-TASK-047).** Tiny adapter over
   `*metrics.DLQMetrics` + `*metrics.CrossCutMetrics` that calls
   `PublishedTotal.WithLabelValues(topic, reason).Inc()` for
   `IncDLQPublished` and `PublisherMessagesTotal.WithLabelValues(topic,
   string(outcome)).Inc()` for `IncPublish`. The conversion from local
   `PublishOutcome` (typed) to string happens at the adapter boundary,
   not in this package — keeps the counter wiring inside the metrics
   package where label vocabulary is owned. seams_test.go pins the
   local mirror against the metrics SSOT.

3. **Caller-side raw-payload logging for lic.dlq.publish-failed
   (architect Q4).** The orchestrator-side caller (LIC-TASK-036) MUST
   log the failing LegalAnalysisArtifactsReady payload as a structured
   WARN BEFORE invoking `PublishDLQ(DLQTopicPublishFailed, ...)`. v1
   object-storage retention is OPTIONAL per integration-contracts §10.2
   — when enabled, the caller populates `envelope.PayloadStorageKey`
   with the bucket reference. When disabled, the warning log is the
   sole post-mortem trail.

4. **`go.mod` side-effects.** Production source imports stdlib
   (`context`, `crypto/hmac`, `crypto/sha256`, `encoding/hex`,
   `encoding/json`, `errors`, `fmt`, `time`) plus
   `contractpro/legal-intelligence-core/internal/domain/{model,port}`
   and `.../internal/infra/broker` (sentinels via the Publisher seam).
   No third-party transitive (amqp091 is behind the Publisher seam).
   The `seams_test.go` file additionally imports
   `.../internal/infra/observability/metrics` for the PublishOutcome
   SSOT pin — `_test` scope only, does not affect the production
   hermeticity. `go mod tidy` produces no diff.

5. **Architecture-doc note.** The wire topics
   (`lic.dlq.invalid-message`, `lic.dlq.consumer-failed`,
   `lic.dlq.publish-failed`, `lic.dlq.agent-output-invalid`) are FROZEN
   at `event-catalog.md` §3.2 / `integration-contracts.md` §10. They
   are encoded as `port.DLQTopic` constants in domain/port/events.go
   and used as RabbitMQ routing keys on the DLX exchange. Any change
   is a contract break and MUST be coordinated with the consumer-side
   wiring (the post-mortem tooling).
