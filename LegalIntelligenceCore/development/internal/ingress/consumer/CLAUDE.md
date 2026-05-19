# consumer Package — CLAUDE.md

**Event Consumer** (LIC-TASK-039, `high-architecture.md` §6.2/§6.12,
`integration-contracts.md` §6.1/§10, `security.md` §10/§11.2,
`observability.md` §3.9). The inbound RabbitMQ adapter: it subscribes to the
six frozen LIC queues, deserialises and envelope-validates each typed event,
routes valid events to LIC-TASK-040 (via the `EventRouter` seam) and
dead-letters invalid ones to `lic.dlq.invalid-message` with a PII-safe HMAC
envelope — owning the **exactly-one-ack** and **exactly-one-metric-outcome**
invariants per delivery:

```
decode+validate (D9/D10/D12) →
  verr != nil ⇒ build PII-safe LICDLQEnvelope (HMAC-SHA-256, R3) →
                Error log (NO raw body) → PublishDLQ(invalid-message) →
                  ok   ⇒ metric{invalid} + Ack
                  fail ⇒ Error log + metric{nacked} + Nack(false)   (R2)
  verr == nil ⇒ ctx = Logger.WithRequestContext(ids) [once, R4] →
                Info → Route*(typed evt) →
                  nil   ⇒ metric{success} + Ack
                  error ⇒ Warn + metric{nacked} + Nack(false)       (R1)
```

One exported type `*Consumer`. Constructor `NewConsumer(BrokerSubscriber,
EventRouter, port.DLQPublisherPort, dlqHashKey string, Deps) (*Consumer,
error)` — `NewTypeName` (`feedback_constructors.md`), fail-fast on any nil
required collaborator / empty `dlqHashKey` via `errors.Join` (the
`pendingconfirmation.NewManager` precedent). Immutable after construction; the
six handler methods are goroutine-safe for distinct deliveries (no mutable
per-delivery state; `startOnce`/`startErr` guard only `Start()`). `Start()` is
idempotent (`sync.Once`) and subscribes the six (queue, handler) pairs in the
frozen D7 order; the first `Subscribe` error is wrapped and short-circuits.
`d.Reject` is never used; `Nack(false)` is the §6.4 DLX path.

## Files

- **consumer.go** — package doc (hermetic statement; D1..D20 attribution),
  the three D18 outcome string constants, the frozen `subscriptionTable`
  (D7), `Consumer` (D4), `NewConsumer` (D2 — `errors.Join` fail-fast),
  `Start()` (D3 — `sync.Once`, deterministic D7 order, wrap+short-circuit),
  the six thin `handleX` methods + the generic `handle[T]` core (D11 — the
  exactly-one-ack & exactly-one-metric invariants, advisory `return nil`
  after ack/nack), `ack`/`nack` (transport error logged, never re-acked).
- **seams.go** — `BrokerSubscriber` (D5, no noop — required), `EventRouter`
  (D8, no noop — required, one method per event), `Metrics`+`noopMetrics`
  (D17), `Clock`+`systemClock` (1-method — D14), `Logger`+`noopLogger`
  (Info/Warn/Error + `WithRequestContext` — D6/R4), `RequestIDs` POD (D6).
  `var _` after each noop pair; the broker/router structural-satisfaction
  assertions are the LIC-TASK-047 wiring package's, NOT here (D5/D8/D16).
- **deps.go** — `Deps{Metrics, Clock, Logger}` (optional-with-noop) +
  `withDefaults()` (D19). The 3 required collaborators + `dlqHashKey` are
  positional `NewConsumer` params, NOT in `Deps`.
- **validate.go** — `decodeAndValidate` per-event funcs returning
  `(typed DTO, RequestIDs, *model.DomainError)`; the D9 required-field +
  canonical-UUID matrix (validation order PINNED: unmarshal → required-present
  → canonical-UUID); `isCanonicalUUID` (D10 — `len==36 && uuid.Parse==nil`);
  `idProbe`/`probeIDs` best-effort extractor (D12); the single internal
  `*model.DomainError{INVALID_MESSAGE_SCHEMA, STAGE_RECEIVED}` (D15) listing
  every offending field in fixed source-order (no `sort` — D16 allowlist).
- **dlq_envelope.go** — `buildInvalidEnvelope` (D13 every field; clean
  canonical IDs only — D12; `ErrorMessage` capped 512 B, UTF-8-safe, NEVER
  raw body) + `hmacFirst64` (R3 — `crypto/hmac`+`sha256`, hex, first 64).
- **internal_test.go** — `TestHermeticImports` (3-entry `{model,port,broker}`
  allowlist + `github.com/google/uuid` single-third-party + the D16
  active-fail forbidden set incl. `internal/ingress/router`,
  `internal/config`, concrete logger/metrics/tracer) + `TestGofmtClean`
  (`go/format`; the sandbox blocks `go fmt`) — D16/D20.
- **consumer_test.go** — the full behavioural suite (PART C #5–#16) with
  in-package fakes for `BrokerSubscriber`, `EventRouter`,
  `port.DLQPublisherPort`, `Metrics`, `Clock`, `Logger`, `broker.Delivery`;
  `-race` clean and deterministic.
- **CLAUDE.md** — this file.

## API

- `NewConsumer(BrokerSubscriber, EventRouter, port.DLQPublisherPort,
  dlqHashKey string, Deps) (*Consumer, error)`.
- `(*Consumer) Start() error` — idempotent; subscribes the six frozen
  (queue, handler) pairs in D7 order; first `Subscribe` error wrapped +
  short-circuits; does NOT block.
- The six handler methods (`broker.MessageHandler` shape) are bound to the
  broker by `Start()`; they are not called directly outside tests.
- `Deps{Metrics, Clock, Logger}` — every field optional (nil ⇒ noop).
- `BrokerSubscriber` / `EventRouter` / `Metrics` / `Clock` / `Logger` /
  `RequestIDs` — the consumer-local seams (D5/D8/D17/D14/D6).

## Reconciliations (build-spec PART B — DEFECT-style, condensed)

**R1 — 039/040 ACK boundary.** 039 owns subscription, typed decode,
envelope-validation, the invalid→DLQ+ACK path, and mapping the `EventRouter`
return: `nil⇒Ack`(success) / `error⇒Nack(false)`(nacked). 039 does NOT read
`x-death`, NOT compute a retry-level routing key, NOT publish to the DLX. The
main queue has `x-dead-letter-exchange` with **no static
`x-dead-letter-routing-key`** (`topology.go:84-90`, broker CLAUDE.md "§6.4
deviation"), so a plain `Nack(false)` routes to the DLX exactly as §6.4
intends; level selection is explicitly 040-owned. Mirrors the
pendingconfirmation split-ownership reconciliation.

**R2 — DLQ-publish-failure must NOT silently drop.** If `PublishDLQ` itself
errors on the invalid path: `Error` log + `metric{nacked}` + `Nack(false)`
(NOT `Ack`). The message returns through the DLX-loop (post-040: eventually
`lic.dlq.consumer-failed` on retry-budget exhaustion). Satisfies the
acceptance "не отбрасываем permanently без логирования" with only
`port.DLQPublisherPort`.

**R3 — the consumer computes `OriginalMessageHash`.** The frozen
`PublishDLQ(ctx, topic, envelope)` passes no raw bytes, so the publisher
cannot hash a payload it never receives; the consumer (the sole holder of the
raw inbound bytes for the invalid-message path) computes HMAC-SHA-256 over the
full body with `LIC_DLQ_HASH_KEY` (the `dlqHashKey` ctor param), first 64 hex,
and fills `OriginalMessageHash`/`OriginalMessageSizeBytes` before
`PublishDLQ`. The raw body is NEVER placed in the envelope (PII-safe).
**Forward (046):** the DLQ Publisher MUST treat a pre-populated
hash/size as authoritative and NOT recompute/overwrite it.

**R4 — `logger.WithRequestContext` is forbidden-package.** The consumer-local
`Logger` seam carries a `WithRequestContext(ctx, RequestIDs) context.Context`
method; `noopLogger` returns ctx unchanged; the LIC-TASK-047 adapter
implements it over `logger.WithRequestContext` + `logger.RequestContext`. The
consumer calls it exactly once per delivery, after validation, before
`Route*` — the `context.go:27` "call once at ingress" contract satisfied via
the seam without a forbidden import. Without a real 047 adapter, downstream
lines lose correlation IDs (forward-noted).

**R5 — task acceptance lists a maximal set; struct shapes are the SSOT.** The
acceptance `{correlation_id, job_id, document_id, version_id,
organization_id}` is the maximal envelope, a non-exhaustive checklist. The
frozen `events.go` struct shapes are the binding SSOT: `ArtifactsProvided`
has no `organization_id`; `LegalAnalysisArtifactsPersisted` has only 4
fields; `VersionCreated.job_id` is `omitempty` (conditional UUID check);
`VersionProcessingArtifactsReady`/`VersionCreated` additionally require
`created_by_user_id`. The D9 matrix is the precise per-event projection.

**R6 — `contract_type` whitelist/regex is NOT a 039 concern.** 039 validates
`UserConfirmedType` only for envelope shape: `contract_type` present-and-
non-empty + the 5 envelope UUIDs canonical. It does NOT run the
`^[A-Z_]{1,32}$` regex or the 12-value whitelist and does NOT route a bad
`contract_type` to `lic.dlq.invalid-message`. A structurally-valid
`UserConfirmedType` with a bad `contract_type` is dispatched via
`RouteUserConfirmedType`; the Pending Type Confirmation Manager
(LIC-TASK-037) rejects it per `security.md` §11.2 and 040's
non-retryable→DLQ mapping. Splitting the whitelist into 039 would duplicate a
mandatory security control already owned and tested by 037.

## Conventions & deliberate decisions (build-spec D1..D20, condensed)

- **D1/D2 — one `*Consumer`; `NewConsumer` fail-fast.** `errors.Join` of
  per-arg `errors.New` for nil `sub`/`router`/`dlq` and empty `dlqHashKey`;
  `(nil, joinedErr)` on any failure.
- **D3/D4 — `Start()` `sync.Once`, immutable struct.** Deterministic D7
  subscription order; first `Subscribe` error wrapped
  (`consumer: subscribe to %s: %w`) + short-circuit; partial-subscription
  cleanup is the caller's broker-shutdown job.
- **D5/D8/D16 — adapter layer; broker import allowed (types only).**
  `internal/infra/broker` for `broker.Delivery`/`broker.MessageHandler` ONLY
  (it already inverted amqp091). `BrokerSubscriber`/`EventRouter` are
  required positional params, no noop; the `var _` satisfaction assertions
  are the 047 wiring package's. `EventRouter` is one method per event (typed)
  so a single 040 router satisfies both this seam and the six `port.*Handler`.
- **D6/D14/D17/D19 — seams.** `Logger` (Info/Warn/Error +
  `WithRequestContext`, noop ⇒ ctx unchanged); 1-method `Clock`; `Metrics`
  (`ConsumerMessage(topic,outcome)`); `Deps{Metrics,Clock,Logger}` +
  `withDefaults()` (nil ⇒ noop). `RequestIDs` is a consumer-local POD, NOT
  `logger.RequestContext`.
- **D7 — frozen local queue→topic table.** Hardcoded `[6]subscription`,
  order = `topology.go:49-56`; queue names + routing keys are FROZEN
  contract identifiers. Zero broker-package change (option a); pinned by
  `TestSubscriptionTableMatchesContract`.
- **D9/D10/D12/D15 — validation.** Order PINNED: unmarshal →
  required-present (trim-non-empty) → canonical-UUID. `isCanonicalUUID` =
  `len==36 && uuid.Parse==nil` (canonical hyphenated only; rejects
  urn/braced/no-hyphen; NOT v4-restricted). `idProbe` salvages forensic IDs
  best-effort; only clean canonical UUIDs adopted into the PII-safe
  envelope. All failures collapse into one
  `*model.DomainError{INVALID_MESSAGE_SCHEMA, STAGE_RECEIVED}` — internal to
  039, never published (empty userMessage, `IsPublishableToOrchestrator==
  false`).
- **D11 — generic `handle[T]` core.** Every path: exactly one
  `d.Ack()`/`d.Nack(false)` AND exactly one
  `metrics.ConsumerMessage(topic,outcome)` before the advisory `return nil`;
  no fall-through; `d.Reject` never used. An ack/nack transport error is
  logged but does NOT change the decided outcome nor re-ack; the metric
  classifies the message, not the transport.
- **D13/R3 — DLQ envelope.** `OriginalTopic`=D7 topic;
  `OriginalMessageHash`=HMAC first-64-hex; `OriginalMessageSizeBytes`=
  `len(body)`; `ErrorCode`=`INVALID_MESSAGE_SCHEMA`; `ErrorMessage`=sanitized
  field list, ≤512 B, NEVER raw body; `RetryCount`=0; best-effort IDs;
  agent/publish-only fields zero; `FailedAt`=`clock.Now().UTC()` RFC3339.
- **D18 — outcome constants.** `outcomeSuccess/Invalid/Nacked` =
  `"success"/"invalid"/"nacked"`, value-identical to
  `metrics.PublishOutcome{Success,Invalid,Nacked}`; pinned by
  `TestOutcomeConstantsMatchSSOT` WITHOUT a metrics import.
- **D16/D20 — hermetic adapter + gofmt self-check.** Imports: stdlib +
  `github.com/google/uuid` + `domain/{model,port}` + `infra/broker` (types
  only). Forbidden: amqp091, `internal/config`, concrete
  logger/metrics/tracer, `internal/application/*`, `internal/ingress/router`
  (the consumer's own downstream — INVERTED via `EventRouter`),
  prometheus/otel/redis. `TestGofmtClean` via `go/format`.

## Forward notes (recorded, owners elsewhere — build-spec PART D)

1. **LIC-TASK-040 (Event Router, `internal/ingress/router`).** Implements the
   `consumer.EventRouter` seam. Owns the pre-routing idempotency guard
   (LIC-TASK-038), pipeline concurrency-semaphore acquisition, the routing
   table (VersionProcessingArtifactsReady→PipelineOrchestrator.Run,
   VersionCreated→version-meta cache, ArtifactsProvided→DM Artifact Awaiter,
   Persisted/PersistFailed→DM Confirmation Awaiter,
   UserConfirmedType→`pendingconfirmation.Manager.HandleUserConfirmedType`),
   the x-death-aware retry-level escalation (read `d.XDeath()` →
   retry.1/2/3 / `lic.dlq.consumer-failed`), and PAUSED restart semantics
   (`RepublishPauseEvents`). 040 inherits the typed DTOs already validated +
   IDs in ctx (R4). 040 may **widen its own seam impl** to receive the raw
   `broker.Delivery` if it needs `XDeath()` — the 039 `EventRouter` seam
   deliberately does NOT carry `Delivery` (D8 YAGNI); 039's `Nack(false)` on
   router-error is the in-scope primitive 040's richer routing supersedes
   without changing the seam (R1). The
   `var _ consumer.EventRouter = (*router.Router)(nil)` assertion is the
   LIC-TASK-047 wiring package's.
2. **LIC-TASK-046 (DLQ Publisher, `internal/egress/dlq`).** Implements
   `port.DLQPublisherPort`. MUST treat a pre-populated
   `OriginalMessageHash`/`OriginalMessageSizeBytes` as authoritative and NOT
   recompute/overwrite it (R3 — the consumer fills them for the
   invalid-message path; 046 computes them only for its own-owned
   publish-failed / agent-output-invalid paths). 046's
   `lic_dlq_published_total{topic,reason}` is separate from 039's
   `lic_consumer_messages_total{topic,outcome}` — 039 does NOT emit
   `lic_dlq_published_total`.
3. **LIC-TASK-047 (app wiring).** Constructs
   `consumer.NewConsumer(brokerClient, theRouter(040), dlqPublisher(046),
   cfg.Security.DLQHashKey, consumer.Deps{Metrics: adapter over
   *metrics.Metrics.CrossCut.ConsumerMessagesTotal, Clock: systemClock,
   Logger: adapter implementing WithRequestContext over
   logger.WithRequestContext+logger.RequestContext and Info/Warn/Error over
   *logger.Logger})`. Asserts in the WIRING package (NOT here)
   `var _ consumer.BrokerSubscriber = (*broker.Client)(nil)` and
   `var _ consumer.EventRouter = (*router.Router)(nil)`. Without a real
   `Logger` adapter, downstream lines lose correlation IDs (R4). Calls
   `consumer.Start()` after the broker topology is declared
   (`NewClient` declares it — `client.go:194`).
4. **go.mod side-effect.** `github.com/google/uuid` moves from `// indirect`
   to a direct require after `go mod tidy` (D10 uses it directly). No other
   dependency change.
