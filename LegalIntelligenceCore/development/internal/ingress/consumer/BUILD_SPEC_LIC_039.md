# BUILD SPEC — LIC-TASK-039 Event Consumer (AUTHORITATIVE)

**Status:** binding. The golang-pro implementer follows this verbatim and makes
**no further architecture decisions**. Every non-obvious ground-truth claim is
cited as `file:line`. Scope is strictly LIC-TASK-039; everything tagged
"040-owned" / "046-owned" is OUT OF SCOPE and forward-noted.

- Package: `internal/ingress/consumer`
- Module: `contractpro/legal-intelligence-core`, Go 1.26.1
- Dev root: `LegalIntelligenceCore/development`

---

## 0. Verified ground truth (re-confirmed by reading source)

| Claim | Evidence |
|---|---|
| No `port.EventRouter`; 6 handler ifaces exist | `internal/domain/port/inbound.go:16-51` (`VersionArtifactsReadyHandler`, `VersionCreatedHandler`, `ArtifactsProvidedHandler`, `PersistConfirmationHandler{HandlePersisted,HandlePersistFailed}`, `UserConfirmedTypeHandler`) |
| 6 inbound DTOs + DLQ envelope + topics | `internal/domain/port/events.go:42-137` (DTOs), `:244-249` (`DLQTopicInvalidMessage = "lic.dlq.invalid-message"`), `:272-300` (`LICDLQEnvelope`) |
| Broker `Subscribe(queue string, MessageHandler) error`; manual ack; handler owns lifecycle; `Delivery` is amqp091-free | `internal/infra/broker/client.go:72` (`MessageHandler`), `subscribe.go:36-55` (`Delivery`), `subscribe.go:175` (`Subscribe`), `client.go:66-72` (handler owns ack), `subscribe.go:174` (auto-ack false), `subscribe.go:31-35` (no amqp091 in `Delivery`) |
| Reconnect re-subscribes automatically | `subscribe.go:167-195`, `client.go:1-18`, broker `CLAUDE.md` ("re-subscribed after reconnect") |
| Frozen queue→topic table (unexported) | `internal/infra/broker/topology.go:48-57` `subscriptionSpecs()` (method on `*Client`, unexported) |
| Metric exists, labels `{topic,outcome}` | `internal/infra/observability/metrics/crosscut.go:43-46` (`ConsumerMessagesTotal`, `[]string{"topic","outcome"}`) |
| Outcome SSOT constants | `internal/infra/observability/metrics/labels.go:170-177` (`PublishOutcomeSuccess="success"`, `PublishOutcomeInvalid="invalid"`, `PublishOutcomeNacked="nacked"`) |
| DLQ port (no raw bytes param) | `internal/domain/port/publisher.go:38-40` `PublishDLQ(ctx, DLQTopic, LICDLQEnvelope) error` |
| `model.NewDomainError` panics on unregistered code | `internal/domain/model/errors.go:83-95`; `ErrCodeInvalidMessageSchema="INVALID_MESSAGE_SCHEMA"` `error_codes.go:16`; catalog row `error_codes.go:114-118` (retryable=false, userMessage="") |
| `model.IsRetryable/IsDomainError/GetErrorCode` | `internal/domain/model/errors.go:168-204` |
| `uuid.Validate(s) error` exists in v1.6.0; accepts non-canonical forms | `github.com/google/uuid@v1.6.0/uuid.go:195` (and `:189-224` — accepts urn/braced/no-hyphen); `uuid.Parse` `:68` (same non-canonical leniency, doc `:66-67` "should not be used to validate") |
| `google/uuid v1.6.0` is `// indirect` in go.mod | `development/go.mod:27` |
| Logger seam: `logger.WithRequestContext`, `RequestContext{...}` | `internal/infra/observability/logger/context.go:16-32` ("Call this once at ingress (broker consumer reads the envelope, builds RequestContext, attaches it)"), `logger.go:63-97` (`Info/Warn/Error(ctx,msg,...slog.Attr)`, `With(component)`) |
| Wire envelope IDs are `uuid-v4`; LIC never mints `correlation_id` | `integration-contracts.md:121-141` |
| `created_by_user_id` required for version-* events | `integration-contracts.md:138`, `events.go:52` (`VersionProcessingArtifactsReady.CreatedByUserID` no `omitempty`), `events.go:70` (`VersionCreated.CreatedByUserID` no `omitempty`) |
| HMAC: `original_message_hash` = HMAC-SHA-256 over full payload via `LIC_DLQ_HASH_KEY`, first 64 hex | `integration-contracts.md:331`, `:349-351`; `cfg.Security.DLQHashKey` from `LIC_DLQ_HASH_KEY` `config/security.go:11,17` |
| LIC-TASK-046 acceptance assigns HMAC to the DLQ Publisher | tasks.json LIC-TASK-046 acceptance ("original_message_hash (HMAC-SHA-256 c LIC_DLQ_HASH_KEY, first 64 chars hex)") |
| §11.2 audit-trail validation_outcome enum | `security.md:499` |
| Pendingconfirmation precedent: local seams + noop + Deps.withDefaults + `var _` in wiring, hermetic test, gofmt self-check | `pendingconfirmation/seams.go`, `deps.go`, `internal_test.go`, `CLAUDE.md` |
| DP reference consumer (style only; LIC broker API differs) | `DocumentProcessing/.../internal/ingress/consumer/consumer.go`, `validate.go` |

---

## PART A — BINDING DECISIONS (D1..D20)

### D1 — Package & file layout

`internal/ingress/consumer/` (new package `consumer`):

| File | Purpose (one line) |
|---|---|
| `consumer.go` | Package doc (hermetic statement + D-attribution); `Consumer` struct; `NewConsumer` constructor (fail-fast); `Start()`; the 6 per-topic handler methods + the generic `handle` core. |
| `seams.go` | Consumer-local seam interfaces + zero-dep defaults: `EventRouter` (no noop — required), `Metrics`+`noopMetrics`, `Clock`+`systemClock`, `Logger`+`noopLogger`, `BrokerSubscriber` (no noop — required). `var _` assertions after each noop pair. |
| `deps.go` | `Deps{Metrics, Clock, Logger}` optional-with-noop bundle + `withDefaults()`. |
| `validate.go` | `decodeAndValidate` per-event funcs; the required-field + canonical-UUID matrix (D9); best-effort ID extraction from partially-parsed JSON (D12). |
| `dlq_envelope.go` | `buildInvalidEnvelope(...)` — constructs `port.LICDLQEnvelope` for `lic.dlq.invalid-message`; the HMAC computation (`crypto/hmac`+`sha256`, first 64 hex — R3). |
| `internal_test.go` | `TestHermeticImports` (allowlist + active-fail forbidden set) + `TestGofmtClean` (`go/format`). |
| `consumer_test.go` | Behavioural suite: 4 test_steps + every ack path + metric assertions + nil-deps fail-fast, with in-package fakes for `BrokerSubscriber`, `EventRouter`, `port.DLQPublisherPort`, `Metrics`, `Clock`, `Delivery`. |
| `CLAUDE.md` | Package guide mirroring `pendingconfirmation/CLAUDE.md` shape (Files / API / Reconciliations / Conventions / Forward notes). |

No other files. No subpackages.

### D2 — `NewConsumer`, not `New` (feedback_constructors.md)

`feedback_constructors.md` mandates `NewTypeName`. Constructor is **`NewConsumer`**
(the DP `NewConsumer` precedent + the feedback rule; note broker uses
`NewClient` deliberately for stutter — irrelevant here, the type is `Consumer`,
so `NewConsumer` is both correct and stutter-free).

**Exact signature (positional = required, fail-fast non-nil; Deps = optional-with-noop):**

```go
func NewConsumer(
    sub BrokerSubscriber,            // required
    router EventRouter,              // required
    dlq port.DLQPublisherPort,       // required
    dlqHashKey string,               // required, non-empty (LIC_DLQ_HASH_KEY)
    deps Deps,                       // optional (nil fields → noop)
) (*Consumer, error)
```

Fail-fast via `errors.Join` of per-arg errors (the `pendingconfirmation.NewManager`
precedent — `pendingconfirmation/CLAUDE.md`: "fail-fast on … any nil required
collaborator via errors.Join"). Each of `sub==nil`, `router==nil`, `dlq==nil`,
`strings.TrimSpace(dlqHashKey)==""` contributes a distinct `errors.New(...)`
joined into the returned error; on any failure return `(nil, joinedErr)`. On
success store `deps = deps.withDefaults()` and bind the component logger:
`deps.Logger` is used as-is (it is a seam, see D6) — there is no `.With` on the
seam; component scoping is the wiring layer's job (D6/forward-note).

**Why positional vs Deps:** `BrokerSubscriber`, `EventRouter`,
`port.DLQPublisherPort`, `dlqHashKey` are load-bearing collaborators / a
mandatory security secret — a consumer without any of them cannot perform its
contract. They are positional and required (the pendingconfirmation
"5 frozen-wire ports + resumer are positional, NOT in Deps" rule). `Metrics`,
`Clock`, `Logger` are telemetry / determinism seams — optional-with-noop in
`Deps` (the pendingconfirmation `Deps{Metrics,Clock,Logger,...}` precedent).

### D3 — `Start()` contract

```go
func (c *Consumer) Start() error
```

Idempotent via `sync.Once` (DP `consumer.go:85-97` precedent). On first call,
subscribe **in the deterministic frozen order of D7** by calling
`c.sub.Subscribe(queue, c.handleX)` for each of the 6 (queue, handler) pairs.
On the first `Subscribe` error, wrap with
`fmt.Errorf("consumer: subscribe to %s: %w", queue, err)`, store it, and stop
(do not attempt the remaining subscriptions — partial-subscription cleanup is
the caller's broker-shutdown responsibility, DP `consumer.go:80-97` precedent).
Repeated `Start()` calls return the stored first-attempt result. `Start()` does
**not** block; the broker's `consumeLoop` runs the handlers
(`subscribe.go:249-274`).

### D4 — `Consumer` struct (immutable after construction)

```go
type Consumer struct {
    sub        BrokerSubscriber
    router     EventRouter
    dlq        port.DLQPublisherPort
    dlqHashKey string
    metrics    Metrics
    clock      Clock
    log        Logger

    startOnce sync.Once
    startErr  error
}
```

No other mutable per-instance state; the 6 handler methods are goroutine-safe
for distinct deliveries (the broker may invoke them concurrently across the 6
consumer channels — `subscribe.go:249`).

### D5 — `BrokerSubscriber` consumer-side interface (DI seam over `*broker.Client`)

Defined in `seams.go`. Mirrors DP's `BrokerSubscriber` but with the **LIC**
`MessageHandler`/`Delivery` shapes (DP uses `func(ctx, []byte) error`; LIC's
broker is manual-ack with a `Delivery` — `subscribe.go:36-55`,
`client.go:66-72`):

```go
// BrokerSubscriber is satisfied structurally by *broker.Client
// (broker.Client.Subscribe — subscribe.go:175). Declared consumer-side so the
// broker is injected behind an interface for hermetic unit testing; the
// var _ broker-satisfaction assertion lives in the LIC-TASK-047 wiring package
// (D17), NOT here.
type BrokerSubscriber interface {
    Subscribe(queue string, handler broker.MessageHandler) error
}
```

`broker.MessageHandler` = `func(ctx context.Context, d broker.Delivery) error`
(`client.go:72`). See D16 for why importing `internal/infra/broker` here is
correct (this is the adapter layer) and is the **only** infra import allowed.

### D6 — Logger: consumer-local seam (NOT the concrete `*logger.Logger`)

Match the pendingconfirmation precedent exactly: a local `Logger` **seam**
(`pendingconfirmation/seams.go:99-103`), NOT a direct `*logger.Logger`
dependency, because the package is hermetic (D16) and may not import
`internal/infra/observability/logger`. The seam shape — chosen to be wire-able
over `*logger.Logger` by LIC-TASK-047:

```go
type Logger interface {
    Info(ctx context.Context, msg string, kv ...any)
    Warn(ctx context.Context, msg string, kv ...any)
    Error(ctx context.Context, msg string, kv ...any)
}
```

`Info/Warn/Error` (3 methods — the pendingconfirmation D20 "first-class Info"
shape; the consumer needs `Error` for invalid-message + DLQ-publish-failure
logging, `Warn` for router-error, `Info` for accepted dispatch). Signature uses
`...any` (the pendingconfirmation seam shape) so the LIC-TASK-047 adapter
bridges to `*logger.Logger`'s `...slog.Attr` API. `noopLogger` is the
zero-dependency default (`var _ Logger = noopLogger{}`).

**RequestContext attachment (R4):** `logger.WithRequestContext` lives in the
forbidden `internal/infra/observability/logger` package
(`context.go:30`). The consumer therefore does NOT call it directly. Instead
the consumer-local `Logger` seam carries this contract: **the LIC-TASK-047
adapter that satisfies `Logger` is responsible for attaching the
`logger.RequestContext` to the ctx** — but the **consumer must hand the adapter
the per-delivery IDs**. To keep the ingress-attaches-once contract
(`context.go:27`) satisfiable without a forbidden import, the consumer enriches
the ctx through a 4th seam method on `Logger`:

```go
    // WithRequestContext returns a child ctx carrying the per-delivery
    // correlation IDs (the logger.WithRequestContext contract — context.go:27,
    // "call once at ingress"). The LIC-TASK-047 adapter implements this over
    // logger.WithRequestContext + logger.RequestContext; noopLogger returns ctx
    // unchanged. Declared on the Logger seam (not a separate seam) because it
    // is the same observability concern and avoids a 5th seam (YAGNI).
    WithRequestContext(ctx context.Context, ids RequestIDs) context.Context
```

`RequestIDs` is a consumer-local POD (declared in `seams.go`) — NOT the
forbidden `logger.RequestContext`:

```go
type RequestIDs struct {
    CorrelationID   string
    JobID           string
    DocumentID      string
    VersionID       string
    OrganizationID  string
    CreatedByUserID string
}
```

`noopLogger.WithRequestContext` returns `ctx` unchanged. The consumer calls
`ctx = c.log.WithRequestContext(ctx, ids)` **exactly once per delivery,
immediately after successful validation, before the router call** (D11). This
satisfies `context.go:27` ("Call this once at ingress") through the seam
without breaking hermeticity. (Per-event field population — D9 matrix /
"which RequestIDs fields are set" — in D11.)

### D7 — Frozen local queue→topic table (consumer hardcodes; broker NOT modified)

`broker.subscriptionSpecs()` is unexported (`topology.go:48`). Per the brief's
option (a): the consumer hardcodes a **local frozen table**. This is the
minimal, lowest-risk choice — it requires **zero broker-package change** (the
brief: "prefer NOT modifying the broker package"), the queue names + routing
keys are FROZEN contract identifiers (`topology.go:38-39`,
`integration-contracts.md:170-175`), and a divergence is caught by the broker's
own topology test + the consumer's `TestSubscriptionTableMatchesContract` (D14).

Local table (a private package-level `[]subscription` in `consumer.go`, exact
order = the `topology.go:49-56` order so subscription order is deterministic and
the test can pin it):

| # | queue (broker `topology.go:50-55`) | topic = routingKey = metric `topic` label | handler method | DTO |
|---|---|---|---|---|
| 1 | `lic.q.version-artifacts-ready` | `dm.events.version-artifacts-ready` | `handleVersionArtifactsReady` | `port.VersionProcessingArtifactsReady` |
| 2 | `lic.q.version-created` | `dm.events.version-created` | `handleVersionCreated` | `port.VersionCreated` |
| 3 | `lic.q.artifacts-provided` | `dm.responses.artifacts-provided` | `handleArtifactsProvided` | `port.ArtifactsProvided` |
| 4 | `lic.q.lic-persist-confirm` | `dm.responses.lic-artifacts-persisted` | `handlePersisted` | `port.LegalAnalysisArtifactsPersisted` |
| 5 | `lic.q.lic-persist-fail` | `dm.responses.lic-artifacts-persist-failed` | `handlePersistFailed` | `port.LegalAnalysisArtifactsPersistFailed` |
| 6 | `lic.q.user-confirmed-type` | `orch.commands.user-confirmed-type` | `handleUserConfirmedType` | `port.UserConfirmedType` |

The `topic` string is the metric `topic` label AND the
`LICDLQEnvelope.OriginalTopic` value (D13). The struct:

```go
type subscription struct {
    queue   string
    topic   string // = routing key = metric label = OriginalTopic
}
```

### D8 — `EventRouter` seam: one method per event (recommended), declared here, NO noop

The seam LIC-TASK-040 implements. Per the brief: **one method per event taking
the typed struct** (recommended) — it is strongly typed, the consumer fully
exercises every method in tests, and it structurally matches the 6 existing
`port.*Handler` interfaces (`inbound.go:16-51`) so a single 040 router type can
satisfy both this seam and those ports. A single `Route(ctx, topic, raw)`
method is rejected: it would push JSON-typing into 040 and make the consumer's
typed-decode + per-event validation (the core of LIC-TASK-039) untestable
against a typed seam.

```go
// EventRouter is the consumer→router seam. LIC-TASK-040 implements it
// (internal/ingress/router). Declared consumer-side so the consumer is
// hermetic and unit-testable with an in-package fake; the
// var _ EventRouter = (*router.Router)(nil) satisfaction assertion lives in
// the LIC-TASK-047 wiring package, NOT here (D17, the pendingconfirmation
// PipelineResumer precedent). NO noop default: a consumer with no router
// cannot dispatch — NewConsumer fails fast (D2). It is a REQUIRED positional
// NewConsumer param, NOT in Deps.
//
// Return contract (mapped by the consumer per D10/R1):
//   nil   ⇒ d.Ack(),       metric outcome = "success"
//   error ⇒ d.Nack(false), metric outcome = "nacked"
// The retry-level (x-death) routing of that Nack is 040's concern (R1); a
// plain Nack(false) is the correct in-scope 039 behaviour (integration-
// contracts.md:209-246 — DLX routing is consumer-adapter-owned; broker main
// queue sets x-dead-letter-exchange with NO static routing key, topology.go:
// 84-90, broker CLAUDE.md "§6.4 deviation").
type EventRouter interface {
    RouteVersionArtifactsReady(ctx context.Context, evt port.VersionProcessingArtifactsReady) error
    RouteVersionCreated(ctx context.Context, evt port.VersionCreated) error
    RouteArtifactsProvided(ctx context.Context, evt port.ArtifactsProvided) error
    RoutePersisted(ctx context.Context, evt port.LegalAnalysisArtifactsPersisted) error
    RoutePersistFailed(ctx context.Context, evt port.LegalAnalysisArtifactsPersistFailed) error
    RouteUserConfirmedType(ctx context.Context, evt port.UserConfirmedType) error
}
```

**Method names** are deliberately `Route*` (not `Handle*`) so this seam is
distinct from `port.*Handler` at the type level while remaining structurally
trivial for 040 to bridge (040 owns the `port.*Handler` ↔ `Route*` adaptation;
the consumer never imports `port.*Handler` runtime — it only constructs the
typed DTOs which already live in `port`).

**YAGNI — no raw `Delivery`/x-death in the seam:** 040 owns x-death/retry-level
routing (tasks.json LIC-TASK-040; `pendingconfirmation/CLAUDE.md` "Forward
notes #2"). 040 depends on 039 and may widen the seam itself when it needs
`XDeath()`. The 039 seam carries only the typed event — sufficient for every
039 test. Do not add `Delivery` now.

### D9 — Per-event required-field + canonical-UUID validation matrix (FROZEN)

Validation pipeline order (PINNED, applied in `validate.go`):

1. `json.Unmarshal(body, &dto)` — on error ⇒ `outcome=invalid` (decode
   failure; best-effort ID extraction via D12).
2. **required-present**: every field in the event's REQUIRED set must be
   present and, for string IDs, non-empty after `strings.TrimSpace`.
3. **canonical-UUID-valid**: every field in the event's UUID-checked subset
   must pass `isCanonicalUUID` (D10).

Any failure at step 1, 2 or 3 ⇒ a single `*model.DomainError`
(`model.NewDomainError(model.ErrCodeInvalidMessageSchema, model.StageReceived)`
— D15) wrapping a reason string listing every offending field ⇒ the invalid
path (D11/D13). All-pass ⇒ the valid path.

REQUIRED set is derived from `events.go` json tags **and**
`integration-contracts.md:121-141` (envelope) — it differs per event because
the structs differ. Fields with `omitempty` or absent from a struct are NOT
required.

| Event (DTO) | REQUIRED present (json tags) | UUID-checked subset |
|---|---|---|
| `VersionProcessingArtifactsReady` (events.go:42-53) | `correlation_id`, `timestamp`, `document_id`, `version_id`, `organization_id`, `job_id`, `created_by_user_id` (no `omitempty`, `events.go:52`; `integration-contracts.md:138` "required") | `correlation_id`, `document_id`, `version_id`, `organization_id`, `job_id`, `created_by_user_id` |
| `VersionCreated` (events.go:60-71) | `correlation_id`, `timestamp`, `document_id`, `version_id`, `organization_id`, `created_by_user_id` (no `omitempty`, `events.go:70`). **`job_id` NOT required** (`json:"job_id,omitempty"` `events.go:69`). | `correlation_id`, `document_id`, `version_id`, `organization_id`, `created_by_user_id`; **`job_id` UUID-checked ONLY IF present and non-empty** (conditional) |
| `ArtifactsProvided` (events.go:81-91) | `correlation_id`, `timestamp`, `job_id`, `document_id`, `version_id`. **NO `organization_id` field exists** on this struct (events.go:81-91) — MUST NOT be required. | `correlation_id`, `job_id`, `document_id`, `version_id` |
| `LegalAnalysisArtifactsPersisted` (events.go:97-102) | `correlation_id`, `timestamp`, `job_id`, `document_id`. **Only these 4 fields exist**; no `version_id`/`organization_id`. | `correlation_id`, `job_id`, `document_id` |
| `LegalAnalysisArtifactsPersistFailed` (events.go:108-116) | `correlation_id`, `timestamp`, `job_id`, `document_id`. (`error_message` is present-but-not-an-ID — `events.go:114` has no `omitempty`; treat as required-present, NOT UUID-checked; `error_code` is `omitempty` `:113` → not required; `is_retryable` is a bool → not required-present.) | `correlation_id`, `job_id`, `document_id` |
| `UserConfirmedType` (events.go:128-137) | `correlation_id`, `timestamp`, `job_id`, `document_id`, `version_id`, `organization_id`, `contract_type` (no `omitempty`, `events.go:135`). `user_id` is `omitempty` (`:136`) → not required. | `correlation_id`, `job_id`, `document_id`, `version_id`, `organization_id` (NOT `contract_type` — it is `^[A-Z_]{1,32}$`, not a UUID; the whitelist/regex check is **040/manager-owned per security.md §11.2** — R6; 039 only requires it present-and-non-empty) |

`timestamp` is **required-present** for all 6 (envelope field,
`integration-contracts.md:122`) but NOT format-validated by 039 (it is
`ISO 8601` text; deep timestamp parsing is not in the acceptance — acceptance
only enumerates "correlation_id, job_id, document_id, version_id,
organization_id присутствуют + UUIDs валидны"). Required-present means
non-empty after trim.

**Acceptance reconciliation (R5):** the task acceptance literally lists
`{correlation_id, job_id, document_id, version_id, organization_id}` as the
required+UUID set. That list is the **maximal** envelope (`integration-
contracts.md:121-127`); the per-event struct shapes (`events.go`) are the
binding SSOT for which of those fields actually exist on each event. The matrix
above is the precise per-event projection — see R5.

### D10 — Canonical-UUID check (strict canonical form, NOT `uuid.Validate`/`uuid.Parse` raw)

`uuid.Validate` AND `uuid.Parse` both accept non-canonical encodings
(`urn:uuid:…`, `{…}`, 32-char no-hyphen) — `uuid.go:189-224`, `:62-67` (doc:
"Parse should not be used to validate"). The wire contract is **`uuid-v4`**
canonical form (`integration-contracts.md:121-127`). To avoid both
false-rejects of legitimate IDs AND silent acceptance of non-canonical forms,
the binding rule is:

```go
// isCanonicalUUID reports whether s is a canonical 36-char hyphenated UUID
// (xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx). It does NOT enforce version==4:
// any valid UUID in canonical form is accepted (RECOMMENDED — security.md
// does NOT mandate v4-strictness; integration-contracts.md:121 is a wire-shape
// hint, not a version constraint; rejecting a legitimately-formatted non-v4 ID
// would be over-strict). It DOES reject urn:/braced/no-hyphen encodings (those
// are not what any LIC producer emits and admitting them widens the trust
// surface).
func isCanonicalUUID(s string) bool {
    if len(s) != 36 {
        return false
    }
    if err := uuid.Parse(s); err != nil {
        return false
    }
    return true // len==36 + uuid.Parse ok ⇒ canonical hyphenated form
}
```

Rationale for `len==36 && uuid.Parse(s)==nil`: at exactly 36 bytes
`uuid.Parse` takes only the canonical `case 36:` branch (`uuid.go:71-72,98-116`,
which then enforces the four `-` positions and hex), so this composition is the
canonical-form check the brief asks for (any-valid-UUID, canonical encoding,
not v4-restricted). `uuid.Validate` is NOT used (it would also pass the 32/38
forms at their lengths; the explicit `len==36` gate is clearer and the binding
choice).

### D11 — Generic handler flow (PINNED pseudocode) + per-topic specifics

There are 6 thin handler methods; each delegates to one generic core
parameterised by the subscription row + a typed decode/validate closure. The
core (exact control flow — the **exactly-one-of-Ack/Nack/Reject** and
**exactly-one-metric-outcome** invariants are structural):

```
func (c *Consumer) handle[Event](ctx, d broker.Delivery):
    body := d.Body()
    sub  := the frozen subscription row for this event (D7)

    evt, ids, verr := decodeAndValidate[Event](body)   // validate.go (D9/D12)

    if verr != nil:
        // INVALID PATH (D13)
        env := buildInvalidEnvelope(sub.topic, body, verr, ids, c.clock, c.dlqHashKey)
        c.log.Error(ctx, "invalid inbound message",
            "topic", sub.topic, "reason", verr.Error(),
            "raw_size", len(body))                       // sanitized reason; NO raw body (PII — §10)
        if perr := c.dlq.PublishDLQ(ctx, port.DLQTopicInvalidMessage, env); perr != nil:
            // DLQ publish itself failed — DO NOT silently lose (acceptance:
            // "не отбрасываем permanently без логирования"). R2 fallback:
            c.log.Error(ctx, "DLQ publish failed for invalid message",
                "topic", sub.topic, "error", perr)
            c.metrics.ConsumerMessage(sub.topic, "nacked")   // labels.PublishOutcomeNacked
            return d.Nack(false)                              // redeliver via DLX-loop; not lost
        c.metrics.ConsumerMessage(sub.topic, "invalid")       // labels.PublishOutcomeInvalid
        return d.Ack()                                        // invalid → DLQ + ACK

    // VALID PATH
    ids := RequestIDs{ ...populate per D11-per-event table... }
    ctx  = c.log.WithRequestContext(ctx, ids)                 // attach ONCE at ingress (D6/R4)
    c.log.Info(ctx, "inbound message accepted", "topic", sub.topic)

    rerr := c.router.Route[Event](ctx, evt)                   // EventRouter seam (D8)
    if rerr != nil:
        c.log.Warn(ctx, "router returned error; nacking",
            "topic", sub.topic, "error", rerr)
        c.metrics.ConsumerMessage(sub.topic, "nacked")        // labels.PublishOutcomeNacked
        return d.Nack(false)                                  // R1 — retry-level routing is 040's
    c.metrics.ConsumerMessage(sub.topic, "success")           // labels.PublishOutcomeSuccess
    return d.Ack()
```

**Invariant proofs the implementer must preserve:**
- Every code path returns exactly one of `d.Ack()` / `d.Nack(false)` and on
  every path exactly one `c.metrics.ConsumerMessage(topic, outcome)` is called
  before the return. No path falls through. `d.Reject` is never used (Nack with
  requeue=false is the §6.4 DLX path — `subscribe.go:51-52`,
  `topology.go:84-90`).
- The return value of the broker `MessageHandler` (advisory only —
  `client.go:71`, `subscribe.go:271`) is `nil` on every path AFTER the
  ack/nack call has been made (so a future advisory-error consumer of the
  return is not misled). i.e. each handler does
  `if err := d.Ack(); err != nil { c.log.Error(ctx,"ack failed",...) }; return nil`
  — an `Ack`/`Nack` transport error is logged but does NOT change the already-
  decided outcome and does NOT re-ack (idempotency: the broker channel is gone
  if ack failed; re-acking would panic/error). The metric is incremented for
  the **decided** outcome regardless of ack transport success (the outcome
  classifies the message, not the transport).

**Per-event `RequestIDs` population (valid path; only fields that exist on the
DTO, D9):**

| Event | CorrelationID | JobID | DocumentID | VersionID | OrganizationID | CreatedByUserID |
|---|---|---|---|---|---|---|
| VersionProcessingArtifactsReady | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ (`events.go:52`) |
| VersionCreated | ✓ | if present (`omitempty`) | ✓ | ✓ | ✓ | ✓ (`events.go:70`) |
| ArtifactsProvided | ✓ | ✓ | ✓ | ✓ | "" (no field — events.go:81-91) | "" |
| LegalAnalysisArtifactsPersisted | ✓ | ✓ | ✓ | "" | "" | "" |
| LegalAnalysisArtifactsPersistFailed | ✓ | ✓ | ✓ | "" | "" | "" |
| UserConfirmedType | ✓ | ✓ | ✓ | ✓ | ✓ | "" (`user_id` is `omitempty` and is a *confirmer*, NOT `created_by_user_id`; map it to neither — the consumer-local `RequestIDs` has no confirmer field by design; 040/manager owns confirmer audit per security.md §11.2 / R6) |

Empty string fields are simply left zero in `RequestIDs`; the
LIC-TASK-047 logger adapter emits only non-empty IDs (`context.go:14-15`).

### D12 — Best-effort ID extraction for the invalid path

When validation fails (including a hard `json.Unmarshal` error), still populate
`LICDLQEnvelope.{CorrelationID,JobID,DocumentID,VersionID,OrganizationID}`
best-effort (`events.go:281-287` doc: "best-effort — when … fails JSON parsing
entirely the publisher leaves them empty"; here the **consumer** fills them
when it can since the consumer holds the bytes — see R3):

- Attempt a tolerant decode into a private flat struct
  `type idProbe struct { CorrelationID string `json:"correlation_id"`; JobID
  string `json:"job_id"`; DocumentID string `json:"document_id"`; VersionID
  string `json:"version_id"`; OrganizationID string `json:"organization_id"` }`
  via `json.Unmarshal(body, &probe)`. Ignore the error (it is the same body
  that failed strict decode); take whatever string fields populated.
- Only copy a probed value into the envelope if it is non-empty AND
  `isCanonicalUUID` (D10) — never put a malformed/garbage value into the
  PII-safe envelope (defense: a forged body must not inject arbitrary strings
  into forensic IDs). A probed value that is present-but-not-a-UUID is dropped
  (left empty), matching the `events.go:281-282` best-effort contract.

### D13 — `LICDLQEnvelope` construction for `lic.dlq.invalid-message` (every field)

`buildInvalidEnvelope(topic string, body []byte, verr error, ids idProbe,
clock Clock, dlqHashKey string) port.LICDLQEnvelope`:

| Field | Value |
|---|---|
| `OriginalTopic` | `topic` (the D7 frozen topic string, = routing key) |
| `OriginalMessageHash` | HMAC-SHA-256(body, key=dlqHashKey), hex, **first 64 chars** (R3 — `integration-contracts.md:331`) |
| `OriginalMessageSizeBytes` | `len(body)` |
| `ErrorCode` | `model.ErrCodeInvalidMessageSchema` (`error_codes.go:16`) |
| `ErrorMessage` | sanitized reason from `verr` — the field list (e.g. `"missing/invalid: version_id, organization_id"` or `"json decode error"`). NEVER include raw body bytes / payload content (PII — `integration-contracts.md:319` "Нет (только IDs)"). Cap at 512 bytes. |
| `RetryCount` | `0` (039 is the first failure; retry-budget tracking is 040/x-death-owned — R1) |
| `CorrelationID`/`JobID`/`DocumentID`/`VersionID`/`OrganizationID` | best-effort from D12 (empty if not a clean canonical UUID) |
| `AgentID`/`Stage`/`RawLLMResponseHash`/`PayloadStorageKey` | left zero (these are agent-output-invalid / publish-failed only — `events.go:289-298`) |
| `FailedAt` | `clock.Now().UTC().Format(time.RFC3339)` (D14 Clock seam; UTC; RFC3339 — `event-catalog.md:252` `format:"date-time"`) |

### D14 — `Clock` seam (1-method, the pendingconfirmation precedent)

```go
type Clock interface { Now() time.Time }
type systemClock struct{}
func (systemClock) Now() time.Time { return time.Now().UTC() }
var _ Clock = systemClock{}
```

Identical to `pendingconfirmation/seams.go:82-91`. Used only for
`LICDLQEnvelope.FailedAt`. `noopMetrics`-style default via `Deps.withDefaults`
(`nil ⇒ systemClock`).

### D15 — Stage for the validation `DomainError`: `model.StageReceived`

`model.NewDomainError` panics on an unregistered code (`errors.go:83-87`);
`ErrCodeInvalidMessageSchema` IS registered (`error_codes.go:114-118`,
retryable=false, userMessage=""). `NewDomainError` takes a `Stage` arg
(`errors.go:83`) but does NOT validate it against the catalog (the catalog
binds code→retryable/messages, not code→stage). The semantically correct stage
for an envelope that failed schema validation at the consumer ingress is
**`model.StageReceived`** (`status.go:45` `StageReceived = "STAGE_RECEIVED"` —
the receive/artifacts ingress stage; the message was *received* and rejected
before any pipeline stage). This `DomainError` is internal to 039 (it is the
`verr`; it is NEVER published to the Orchestrator — `ErrCodeInvalidMessageSchema`
has empty `userMessage` / `IsPublishableToOrchestrator()==false`
`error_codes.go:73-79,114-118`; it only feeds `ErrorMessage` in the DLQ
envelope and the log line). Do NOT publish a status event from 039 (status
publishing is pipeline/044-owned, out of scope).

### D16 — Hermetic import allowlist (this is the ADAPTER layer — broker import IS allowed)

`ingress/consumer` is an inbound **adapter**, not an `internal/application/*`
hermetic core. The DP precedent imports `internal/infra/...`
(`DocumentProcessing/.../consumer.go:10-13`). The LIC `Delivery`/`MessageHandler`
types are DEFINED in `internal/infra/broker` and are deliberately amqp091-free
(`subscribe.go:31-35`, `client.go:1-18`). Therefore importing
`internal/infra/broker` **only for `broker.Delivery` + `broker.MessageHandler`**
is correct and matches DP — do NOT over-abstract with a consumer-local
Delivery interface (YAGNI; the broker already inverted the amqp dependency).

**MAY import (the EXACT allowlist for non-test files):**

stdlib:
- `context`, `encoding/json`, `errors`, `fmt`, `strings`, `time`,
  `crypto/hmac`, `crypto/sha256`, `encoding/hex`, `sync`

third-party:
- `github.com/google/uuid` (D10)

first-party:
- `contractpro/legal-intelligence-core/internal/domain/model`
- `contractpro/legal-intelligence-core/internal/domain/port`
- `contractpro/legal-intelligence-core/internal/infra/broker` (ADAPTER
  exception — types only: `broker.Delivery`, `broker.MessageHandler`)

**MUST NOT import (active-fail forbidden set in `internal_test.go`):**
- `github.com/rabbitmq/amqp091-go` (the broker shields it — `subscribe.go:31-35`)
- `contractpro/legal-intelligence-core/internal/infra/observability/logger`
  (Logger is a seam — D6/R4)
- `contractpro/legal-intelligence-core/internal/infra/observability/metrics`
  (Metrics is a seam — D17)
- `contractpro/legal-intelligence-core/internal/infra/observability/tracer`
- `contractpro/legal-intelligence-core/internal/infra/kvstore`
- `contractpro/legal-intelligence-core/internal/config` (no config import;
  `dlqHashKey` is a `NewConsumer` string param — the pendingconfirmation
  "local Config / ctor-injected" precedent)
- `contractpro/legal-intelligence-core/internal/application/...` (any —
  pipeline, pendingconfirmation, aggregator)
- `contractpro/legal-intelligence-core/internal/ingress/router`
  (LIC-TASK-040 — the dependency is INVERTED via the `EventRouter` seam; the
  consumer must not import its own consumer)
- `contractpro/legal-intelligence-core/internal/agents/...`
- `github.com/prometheus/...`, `go.opentelemetry.io/...`, `github.com/redis/...`

`internal_test.go` actively fails (like
`pendingconfirmation/internal_test.go:33-58`) if any forbidden internal lands
in the allowlist BEFORE the import scan, then scans every non-test `.go` file:
first-party imports must be in the 3-entry allowlist; any third-party other
than `github.com/google/uuid` fails. `github.com/google/uuid` is the single
permitted third-party (explicitly allowlisted, NOT flagged by the generic
"contains dot ⇒ third-party" rule).

### D17 — Metrics seam (consumer-local, the pendingconfirmation precedent)

NOT the concrete `*prometheus.CounterVec` and NOT `*metrics.Metrics`
(hermeticity — D16; the `pendingconfirmation.Metrics` /
`aggregator.Metrics` precedent, `pendingconfirmation/seams.go:40-66`):

```go
// Metrics is the lic_consumer_messages_total{topic,outcome} seam
// (observability.md §3.9, crosscut.go:43-46). LIC-TASK-047 wires an adapter
// over *metrics.Metrics.CrossCut.ConsumerMessagesTotal.
type Metrics interface {
    // ConsumerMessage increments lic_consumer_messages_total{topic,outcome}.
    // outcome MUST be one of metrics.PublishOutcome{Success,Invalid,Nacked}
    // string values "success"|"invalid"|"nacked" (labels.go:170-177) — the
    // consumer passes those exact literals via package-local typed constants
    // (D18) so a typo is a compile error.
    ConsumerMessage(topic, outcome string)
}
type noopMetrics struct{}
func (noopMetrics) ConsumerMessage(string, string) {}
var _ Metrics = noopMetrics{}
```

`nil ⇒ noopMetrics` via `Deps.withDefaults`.

### D18 — Outcome string constants (compile-time-safe, mirror the SSOT)

`labels.go` is in the forbidden `metrics` package (D16). The consumer declares
its own three typed constants in `consumer.go`, value-identical to
`labels.go:173-176` (the labels.go doc explicitly sanctions this:
"package-defined and the de-facto contract"; the values are the wire/metric
contract):

```go
const (
    outcomeSuccess = "success" // == metrics.PublishOutcomeSuccess (labels.go:173)
    outcomeInvalid = "invalid" // == metrics.PublishOutcomeInvalid (labels.go:176)
    outcomeNacked  = "nacked"  // == metrics.PublishOutcomeNacked  (labels.go:175)
)
```

A `consumer_test.go` test (`TestOutcomeConstantsMatchSSOT`) does NOT import
metrics (hermeticity) but asserts the three literals are exactly
`"success"/"invalid"/"nacked"` (guarding against drift; the SSOT correspondence
is documented and review-enforced — the labels.go "coordinate any addition"
note).

### D19 — `Deps` bundle + `withDefaults` (the pendingconfirmation precedent verbatim)

```go
type Deps struct {
    Metrics Metrics // nil ⇒ noopMetrics
    Clock   Clock   // nil ⇒ systemClock
    Logger  Logger  // nil ⇒ noopLogger
}
func (d Deps) withDefaults() Deps {
    if d.Metrics == nil { d.Metrics = noopMetrics{} }
    if d.Clock == nil   { d.Clock = systemClock{} }
    if d.Logger == nil  { d.Logger = noopLogger{} }
    return d
}
```

`EventRouter`, `BrokerSubscriber`, `port.DLQPublisherPort`, `dlqHashKey` are
positional/required (D2), NOT in `Deps` (the pendingconfirmation
"required-collaborators are positional, NOT in Deps" rule).

### D20 — gofmt self-check + hermetic test (the universal precedent)

`internal_test.go` carries `TestGofmtClean` (`go/format` over every `.go` —
the sandbox blocks `go fmt`; verbatim
`pendingconfirmation/internal_test.go:98-120`) and `TestHermeticImports`
(D16 — verbatim shape of `pendingconfirmation/internal_test.go:52-92` with the
3-entry allowlist + the D16 forbidden set + the `github.com/google/uuid`
single-third-party exception).

---

## PART B — RECONCILIATIONS (R1..R6, DEFECT-style: Doc / Impl / Why)

### R1 — 039/040 ACK ownership boundary; plain `Nack(false)` is correct for 039

**Doc.** tasks.json LIC-TASK-040 acceptance: "Transient error → DLX-loop NACK
(read x-death count, route to retry.1/2/3 или DLQ при >3)";
`integration-contracts.md:239-246` (consumer reads `x-death[0].count`, picks
retry level); `pendingconfirmation/CLAUDE.md` Forward-note #2: "040 … owns …
the broker ACK/NACK decision … x-death escalation".

**Impl.** 039 owns: subscription, typed decode, envelope-validation, the
invalid→DLQ+ACK path, and mapping the `EventRouter` seam's return to
Ack/Nack: `nil ⇒ d.Ack()` (outcome=success); `error ⇒ d.Nack(false)`
(outcome=nacked). 039 does **not** read `x-death`, does **not** compute a
retry-level routing key, does **not** publish to the DLX. The
`d.Nack(false)` dead-letters via the main queue's `x-dead-letter-exchange`
(`topology.go:84-90` — set, with NO static routing key). 040, when implemented,
takes over the retry-level decision (it will widen its own seam to receive the
`Delivery`/`XDeath` — D8 YAGNI note).

**Why.** A plain `Nack(false)` is the correct, in-scope, spec-intent-preserving
039 behaviour: the broker's main queue already has `x-dead-letter-exchange` and
deliberately **no static `x-dead-letter-routing-key`** (`topology.go:78-90`,
broker `CLAUDE.md` "§6.4 deviation"), so a `Nack(false)` from 039 routes to the
DLX exactly as the §6.4 design intends; the *level selection* is a separate,
explicitly-040-owned concern (`integration-contracts.md:241-246`). 039 must not
implement x-death logic (YAGNI + tasks.json assigns it to 040). This mirrors
the pendingconfirmation R1 "split ownership" reconciliation: the application/
ingress layer that lacks the broader concern returns the simplest correct
primitive and the owning task extends it.

### R2 — DLQ-publish-failure must NOT silently drop (acceptance: "не отбрасываем permanently без логирования")

**Doc.** Task acceptance: "На invalid → DLQ lic.dlq.invalid-message + ACK (не
отбрасываем permanently без логирования)". The happy invalid path is
DLQ+ACK. But the acceptance does not define behaviour when `PublishDLQ` itself
errors (LIC-TASK-046, the DLQ publisher, is pending — `port.DLQPublisherPort`
is the only thing 039 may rely on).

**Impl.** If `c.dlq.PublishDLQ(...)` returns a non-nil error on the invalid
path: log `Error` ("DLQ publish failed for invalid message", topic, error),
increment `lic_consumer_messages_total{topic,outcome="nacked"}`, and
`d.Nack(false)` (NOT `d.Ack()`). The message is NOT lost — it returns through
the DLX-loop and will be retried (and, post-040, eventually land in
`lic.dlq.consumer-failed` when the retry budget is exhausted).

**Why.** ACK-after-failed-DLQ would permanently drop a message the operator
explicitly forbade dropping silently. `Nack(false)` + Error log is the only
behaviour that satisfies "не отбрасываем permanently без логирования" while
remaining implementable with just `port.DLQPublisherPort` (no 046, no 040). It
is fully testable with a fake `DLQPublisherPort` returning an error.

### R3 — HMAC ownership: the **consumer** computes `OriginalMessageHash` (the port has no raw-bytes param)

**Doc — conflict.** (a) `integration-contracts.md:331,349-351`:
`original_message_hash` = HMAC-SHA-256 of the **full payload** via
`LIC_DLQ_HASH_KEY`, first 64 hex. (b) tasks.json **LIC-TASK-046 acceptance**
explicitly assigns HMAC computation to the DLQ Publisher
("original_message_hash (HMAC-SHA-256 c LIC_DLQ_HASH_KEY, first 64 chars
hex)"). (c) `port.DLQPublisherPort.PublishDLQ(ctx, DLQTopic, LICDLQEnvelope)`
(`publisher.go:39`) passes **no raw bytes** — the publisher physically cannot
hash a payload it never receives. (d) `LICDLQEnvelope.OriginalMessageHash` is a
plain string field (`events.go:274`).

**Impl (binding).** For the LIC-TASK-039 invalid-message path, **the consumer
computes `OriginalMessageHash`** from the raw delivery body (the only component
holding the bytes) using `cfg.Security.DLQHashKey` (injected as the
`dlqHashKey` `NewConsumer` param — D2), and fills it into the envelope BEFORE
calling `PublishDLQ`. Exact algorithm (`dlq_envelope.go`):

```go
import ("crypto/hmac"; "crypto/sha256"; "encoding/hex")

func hmacFirst64(body []byte, key string) string {
    m := hmac.New(sha256.New, []byte(key))
    _, _ = m.Write(body)                       // hmac.Write never errors
    full := hex.EncodeToString(m.Sum(nil))     // 64 hex chars (sha256 = 32 bytes)
    if len(full) > 64 { return full[:64] }     // defensive; sha256→exactly 64
    return full
}
```

`OriginalMessageSizeBytes = len(body)`. The raw `body` is **never** placed in
the envelope (PII-safe — `integration-contracts.md:319,326`,
`events.go:262-271`).

**Why.** The frozen `PublishDLQ` signature (`publisher.go:39`) cannot be
changed (it is a frozen port, OUT OF SCOPE for 039; the brief forbids changing
frozen ports). The publisher cannot hash bytes it never receives. The
`LIC-TASK-046` acceptance "the publisher computes the HMAC" is reconcilable
only as: **for paths where 046 holds the raw payload (e.g. the
publish-failed/agent-output-invalid paths it owns), 046 hashes; for the
consumer-invalid-message path, the consumer — the sole holder of the raw
inbound bytes — hashes and passes a complete envelope.** The
`LICDLQEnvelope.OriginalMessageHash` field is a string precisely so its
producer can vary by path; `events.go:262-271,272-300` describes the envelope
as the carrier, not who fills which field. This is fully implementable WITHOUT
046 existing and fully testable with a fake `DLQPublisherPort` that records the
envelope (assert `OriginalMessageHash` is 64 lowercase-hex chars and
reproducible for a fixed key+body — mirrors LIC-TASK-046 test_step 3 "HMAC
reproducible с одинаковым key"). **Forward note (046):** the 046 DLQ Publisher
MUST treat a pre-populated `OriginalMessageHash`/`OriginalMessageSizeBytes` as
authoritative and NOT recompute/overwrite it (idempotent envelope completion);
for its own-owned paths it computes them itself. Recorded so 046 does not
double-hash or zero the consumer's value.

### R4 — `logger.WithRequestContext` is in a forbidden package; the seam carries the ingress-once contract

**Doc.** `logger/context.go:27`: "Call this once at ingress (broker consumer
reads the envelope, builds RequestContext, attaches it)". The consumer IS that
ingress point. But `internal/infra/observability/logger` is on the consumer's
forbidden-import list (D16 — hermeticity; the pendingconfirmation `Logger`-seam
discipline).

**Impl.** The consumer-local `Logger` seam gains a
`WithRequestContext(ctx, RequestIDs) context.Context` method (D6).
`noopLogger` returns ctx unchanged; the LIC-TASK-047 adapter implements it over
`logger.WithRequestContext(ctx, logger.RequestContext{...})`. The consumer
calls it exactly once per delivery, after validation, before `Route*` (D11), so
every downstream log line carries the IDs — the `context.go:27` contract is
satisfied at the service level via the seam, never via a forbidden import.

**Why.** Identical pattern to pendingconfirmation R3/`TraceRestorer`: an
ingress responsibility that requires a forbidden infra import is expressed as a
seam method the wiring layer adapts. The consumer keeps the
"build the IDs once at ingress" responsibility (it owns the typed event and the
D11 per-event field map); only the concrete `context.WithValue` mechanics move
behind the seam. No silent gap: the wiring (047) MUST supply a real `Logger`
adapter or every downstream line loses correlation IDs (forward-noted).

### R5 — Task acceptance lists a maximal required set; the per-event struct shapes are the binding SSOT

**Doc.** Task acceptance: "required fields validation (correlation_id, job_id,
document_id, version_id, organization_id присутствуют + UUIDs валидны)" —
phrased as one flat set for all events.

**Impl.** `ArtifactsProvided` has no `organization_id` field
(`events.go:81-91`); `LegalAnalysisArtifactsPersisted` has only
`correlation_id/job_id/document_id` (`events.go:97-102`); `VersionCreated`'s
`job_id` is `omitempty` (`events.go:69`); `VersionProcessingArtifactsReady` /
`VersionCreated` additionally require `created_by_user_id`
(`events.go:52,70`, `integration-contracts.md:138`). The binding required +
UUID matrix is the per-event projection in D9 — requiring `organization_id` on
`ArtifactsProvided` would reject every valid DM response (the struct cannot
even carry it).

**Why.** The acceptance line is the **maximal envelope**
(`integration-contracts.md:121-127`), an inclusive checklist, not a per-event
ceiling. The frozen `events.go` struct shapes are the SSOT for which fields
exist on each event (the pendingconfirmation R5 "task acceptance is a
non-exhaustive checklist, not a ceiling; the frozen contract is the SSOT"
discipline). The D9 matrix is the precise, struct-faithful projection and is
the binding requirement.

### R6 — `contract_type` whitelist/regex is NOT a 039 concern (security.md §11.2 is manager-owned)

**Doc.** `events.go:123-127`: `UserConfirmedType.ContractType` MUST match the
12-value whitelist + `^[A-Z_]{1,32}$`; "Any mismatch is routed to
lic.dlq.invalid-message". `security.md` §11.2 (the
`pendingconfirmation/CLAUDE.md` R5 reconciliation) makes the whitelist + tenant
check a **mandatory** control owned by the Pending Type Confirmation Manager
(LIC-TASK-037), invoked via 040.

**Impl.** 039 validates `UserConfirmedType` only for envelope shape:
`contract_type` **present and non-empty** (it is required-present, D9), the 5
envelope UUIDs canonical. 039 does NOT run the regex or the 12-value
whitelist and does NOT route to `lic.dlq.invalid-message` for a bad
`contract_type`. A structurally-valid `UserConfirmedType` with a bad
`contract_type` is dispatched via `RouteUserConfirmedType`; the
manager (already implemented, LIC-TASK-037 `HandleUserConfirmedType`) rejects
it per security.md §11.2 and the 040 mapping (nil⇒ACK / retryable⇒NACK /
non-retryable⇒DLQ — `pendingconfirmation/CLAUDE.md`).

**Why.** Splitting the whitelist into 039 would duplicate a mandatory security
control already owned and tested by LIC-TASK-037 (`model.IsValidContractType`,
the §11.2 audit trail with `validation_outcome` — `security.md:499`,
pendingconfirmation R5). 039's contract is **envelope schema validation only**
(its description: "десериализация, валидация envelope"); business/semantic
validation of `contract_type` is downstream. The events.go:126 "routed to
lic.dlq.invalid-message" is satisfied at the service level by the manager's
non-retryable→DLQ mapping (pendingconfirmation R1/Forward-note #2), not by 039.

---

## PART C — REVIEWER-GATE CHECKLIST (objective pass/fail; parent runs BEFORE code-reviewer)

Run from `LegalIntelligenceCore/development`:

1. **Builds:** `go build ./internal/ingress/consumer/...` exits 0.
2. **Vet clean:** `go vet ./internal/ingress/consumer/...` exits 0.
3. **Tests + race:** `go test -race ./internal/ingress/consumer/...` exits 0.
4. **go.mod tidy effect:** after `go mod tidy`, `github.com/google/uuid` is a
   **direct** require (was `// indirect` `go.mod:27`); `go build ./...` still
   green; no other new deps. (The implementer runs `go mod tidy`; the parent
   verifies the only diff is the uuid promotion.)
5. **test_step coverage — named tests exist and pass:**
   - `TestConsumer_InvalidJSON_DLQAndAck` (test_step 2: invalid JSON → DLQ + ACK)
   - `TestConsumer_MissingRequiredField_DLQ` (test_step 3, one subtest per event
     for at least one missing required field each — 6 subtests)
   - `TestConsumer_ValidMessage_Dispatched` (test_step 4: one subtest per event,
     asserts the correct `Route*` fake method received the typed struct)
   - `go test ./internal/ingress/consumer/...` green (test_step 1)
6. **Exactly-one-ack invariant tested for EVERY path** (fake `Delivery` records
   ack/nack calls; assert the call count is exactly 1 and the kind is correct):
   - valid → `Ack` (×1, no Nack)
   - invalid-json → DLQ published + `Ack` (×1)
   - missing-field → DLQ published + `Ack` (×1)
   - bad-uuid (canonical-form fail) → DLQ published + `Ack` (×1)
   - router returns error → `Nack(false)` (×1, no Ack, no DLQ)
   - DLQ publish fails on invalid path → `Nack(false)` (×1, no Ack) + Error log (R2)
7. **Metric outcome asserted per path** (fake `Metrics` records (topic,outcome)
   tuples; assert exactly one per delivery, correct topic from D7, correct
   outcome): valid⇒`success`; invalid-json/missing/bad-uuid⇒`invalid`;
   router-error⇒`nacked`; dlq-publish-fails⇒`nacked`.
8. **HMAC (R3):** a test asserts `OriginalMessageHash` is 64 lowercase hex
   chars, equals an independently-computed `hmacFirst64(body,key)`, and is
   reproducible for a fixed (key, body); `OriginalMessageSizeBytes==len(body)`;
   raw body NOT present anywhere in the envelope; `OriginalTopic` == the D7
   topic; `ErrorCode==model.ErrCodeInvalidMessageSchema`; `RetryCount==0`;
   `FailedAt` parses as RFC3339 and equals the injected fake `Clock.Now()`.
9. **Best-effort IDs (D12):** a test feeds an invalid body that still contains
   a clean `correlation_id` (canonical UUID) and a garbage `job_id`; assert the
   envelope has `CorrelationID` set, `JobID` empty.
10. **Per-event matrix (D9):** a table test per event proves the exact REQUIRED
    set and UUID subset (e.g. `ArtifactsProvided` with absent
    `organization_id` is VALID; `LegalAnalysisArtifactsPersisted` with only the
    4 fields is VALID; `VersionProcessingArtifactsReady` missing
    `created_by_user_id` is INVALID; `VersionCreated` missing `job_id` is
    VALID; `UserConfirmedType` with a non-whitelist but non-empty
    `contract_type` is VALID and dispatched — R6).
11. **RequestContext attach (R4):** a test with a fake `Logger` asserts
    `WithRequestContext` is called exactly once per valid delivery, before the
    `Route*` call, with the per-event `RequestIDs` from the D11 table; and
    NOT called on the invalid path.
12. **Hermetic + gofmt:** `TestHermeticImports` and `TestGofmtClean` present
    and green; no `github.com/rabbitmq/amqp091-go` import anywhere in the
    package (grep the package non-test files for `amqp091` ⇒ zero hits);
    forbidden-set active-fail assertion present (D16).
13. **Constructor fail-fast:** tests assert `NewConsumer` returns a non-nil
    error (and nil `*Consumer`) for each of: nil `sub`, nil `router`, nil
    `dlq`, empty `dlqHashKey`; and the joined error mentions each failing arg.
14. **Subscription contract (D7/D14):** `TestSubscriptionTableMatchesContract`
    asserts the local table's 6 (queue, topic) pairs exactly equal the frozen
    `integration-contracts.md:170-175` / `topology.go:50-55` values in order.
15. **`Start()` semantics:** test asserts idempotency (2nd call returns 1st
    result), the 6 `Subscribe` calls are made in the D7 order with the correct
    (queue, handler) pairing, and a `Subscribe` error short-circuits + is
    wrapped.
16. **Outcome constants (D18):** `TestOutcomeConstantsMatchSSOT` asserts the
    three literals are exactly `"success"/"invalid"/"nacked"` (no metrics
    import).
17. **CLAUDE.md present** at `internal/ingress/consumer/CLAUDE.md` with Files /
    API / Reconciliations (R1..R6) / Conventions / Forward-notes sections.

Any failing item ⇒ reject and return to golang-pro with the failing item
number. Only an all-green list proceeds to code-reviewer.

---

## PART D — FORWARD NOTES (recorded; owners elsewhere)

1. **LIC-TASK-040 (Event Router, `internal/ingress/router`).** Implements the
   `consumer.EventRouter` seam (D8). Owns: pre-routing idempotency guard
   (LIC-TASK-038), pipeline concurrency semaphore acquisition, the routing
   table (VersionProcessingArtifactsReady→PipelineOrchestrator.Run,
   VersionCreated→version-meta cache, ArtifactsProvided→DM Artifact Awaiter,
   Persisted/PersistFailed→DM Confirmation Awaiter,
   UserConfirmedType→`pendingconfirmation.Manager.HandleUserConfirmedType`),
   the x-death-aware retry-level escalation (read `d.XDeath()` → retry.1/2/3 /
   `lic.dlq.consumer-failed`), and PAUSED restart semantics
   (`RepublishPauseEvents`). 040 inherits from 039: the typed DTOs already
   validated + IDs in ctx (R4). 040 may **widen its own seam impl** to receive
   the raw `broker.Delivery` if it needs `XDeath()` — the 039 `EventRouter`
   seam deliberately does NOT carry `Delivery` (D8 YAGNI); 040 either threads
   x-death through its own router internals or proposes a seam widening (its
   call — it depends on 039, 039 does not depend on it). 039's `Nack(false)`
   on router-error is the in-scope primitive; 040's richer retry-level routing
   supersedes it without changing the 039 seam (R1). The
   `var _ consumer.EventRouter = (*router.Router)(nil)` assertion is the
   LIC-TASK-047 wiring package's, NOT 039's, NOT 040's (D8/D16).
2. **LIC-TASK-046 (DLQ Publisher, `internal/egress/dlq`).** Implements
   `port.DLQPublisherPort`. MUST treat a pre-populated
   `OriginalMessageHash`/`OriginalMessageSizeBytes` on an incoming envelope as
   authoritative and NOT recompute/overwrite it (R3 — the consumer fills them
   for the invalid-message path; 046 computes them only for its own-owned
   publish-failed / agent-output-invalid paths where it holds the raw payload).
   No double-hashing, no zeroing the consumer's value. 046's
   `lic_dlq_published_total{topic,reason}` metric is separate from 039's
   `lic_consumer_messages_total{topic,outcome}` — 039 does NOT emit
   `lic_dlq_published_total`.
3. **LIC-TASK-047 (app wiring).** Constructs
   `consumer.NewConsumer(brokerClient, theRouter (040), dlqPublisher (046),
   cfg.Security.DLQHashKey, consumer.Deps{Metrics: consumerMetricsAdapter over
   *metrics.Metrics.CrossCut.ConsumerMessagesTotal, Clock: systemClock,
   Logger: loggerAdapter that implements WithRequestContext over
   logger.WithRequestContext+logger.RequestContext and Info/Warn/Error over
   *logger.Logger})`. Asserts in the WIRING package (NOT in consumer):
   `var _ consumer.BrokerSubscriber = (*broker.Client)(nil)` and
   `var _ consumer.EventRouter = (*router.Router)(nil)`. Without a real
   `Logger` adapter, downstream lines lose correlation IDs (R4) — wiring MUST
   supply it. Calls `consumer.Start()` after the broker topology is declared
   (broker `NewClient` declares it — `client.go:194`).
4. **go.mod side-effect.** `github.com/google/uuid` moves from `// indirect`
   (`go.mod:27`) to a direct require after `go mod tidy` (D10 uses it
   directly). No other dependency change. No tasks.json change required for
   039 (it is `status:"pending"` and self-contained); the parent updates
   tasks.json `status` per its own process, not the implementer.

---

## PART E — IMPLEMENTER QUICK-REFERENCE (no decisions; pure recap)

- 6 thin `handleX(ctx, d broker.Delivery) error` methods, each calling the
  generic core (D11) with its frozen subscription row (D7) and a typed
  `decodeAndValidate` (D9/D10/D12).
- Validation order: unmarshal → required-present → canonical-UUID (D9), all
  failures ⇒ one `*model.DomainError{ErrCodeInvalidMessageSchema, StageReceived}`
  (D15) ⇒ build envelope (D13, HMAC R3) ⇒ `PublishDLQ(DLQTopicInvalidMessage)`
  ⇒ `Ack`; if `PublishDLQ` errors ⇒ `Nack(false)` + Error log (R2).
- Valid ⇒ `ctx = log.WithRequestContext(ctx, ids)` (R4/D6) ⇒ `Route*` ⇒
  `nil:Ack/success` else `Nack(false)/nacked` (R1).
- Exactly one ack-kind + exactly one metric outcome per delivery; never
  `Reject`; advisory return always `nil` after ack/nack.
- Hermetic: stdlib + `google/uuid` + `domain/{model,port}` + `infra/broker`
  (types only). No amqp091, no config, no logger/metrics concrete, no
  application/*, no ingress/router (D16).
- Constructor `NewConsumer(BrokerSubscriber, EventRouter, port.DLQPublisherPort,
  dlqHashKey string, Deps) (*Consumer, error)`, fail-fast `errors.Join` (D2).
- Tests: hermetic + gofmt self-checks + the PART C suite; `-race` clean.
```
