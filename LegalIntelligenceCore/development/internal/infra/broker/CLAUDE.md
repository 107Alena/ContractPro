# Broker Package — CLAUDE.md

RabbitMQ adapter for the Legal Intelligence Core (amqp091-go). Pure
infrastructure: implements **no domain port** (LIC has none for the broker,
mirroring DocumentProcessing). Higher-level adapters build on it —
`internal/ingress` (consumer, LIC-TASK-043/044) and `internal/egress`
(publisher, LIC-TASK-039/042).

Constructor: `NewClient(cfg config.BrokerConfig) (*Client, error)`.

## Files

- **client.go** — `Client`, injectable `AMQPAPI`/`AMQPChannelAPI` (mock seam),
  real amqp091 wrappers, `NewClient`, `newClientWithAMQP` (test seam),
  `openPublishChannel(On)`, `Ping`, `Close`.
- **topology.go** — `DeclareTopology`/`declareTopologyOn`: 4 topic exchanges +
  6 main queues + 18 retry queues + bindings, from a static config-driven
  table. `ttlMillisInt32` overflow guard.
- **publish.go** — `Publish`: deferred publisher confirms, 3-attempt retry
  with backoff, serialized on a dedicated `pubMu` (decoupled from the
  connection lock so a dead broker never stalls Ping/Close/reconnect).
- **subscribe.go** — `Subscribe`, the amqp091-free `Delivery` interface +
  `XDeathEntry` decode (capped, hardened), manual-ack consume loop.
- **reconnect.go** — `reconnectLoop` (NotifyClose-driven), `reconnectWithBackoff`
  (no-store-until-validated, done-atomic swap), `backoffDelay` (exp + 25%
  jitter).
- **errors.go** — `BrokerError{Op,Retryable,Cause}`, sentinels, `mapError`,
  `redactURLCredentials`.

## Conventions & deliberate decisions

- **`NewClient`, not `NewBrokerClient`.** `broker.NewClient` is stutter-free
  per Effective Go. This intentionally diverges from the DP `infra/CLAUDE.md`
  `NewBrokerClient` wording (recorded so a future "consistency" change does
  not reintroduce the stutter). It matches the LIC `internal/llm/*` siblings.
- **Error model:** broker-package-local typed `BrokerError`, never a
  `model.ErrorCode`. `model.errorCatalog` has an `init()` SSOT panic and
  broker/infra errors are not published to the Orchestrator. `context`
  errors pass through raw (codebase-wide convention). AMQP reply codes
  404/403/406 are non-retryable.
- **§6.4 deviation:** main queues set `x-dead-letter-exchange` only, with
  **no static `x-dead-letter-routing-key`**. The integration-contracts §6.4
  ASCII diagram annotates "...retry.1", but the consumer-side §6.4 text
  specifies the retry level is chosen dynamically from `x-death[].count`. A
  static key would pin every dead-letter to retry.1 and make multi-level
  escalation impossible. Escalation is the consumer adapter's concern
  (LIC-TASK-043), which publishes to the DLX with the computed key. Intent
  of §6.4 is preserved; only the literal diagram annotation is not followed.
- **Compile-time interface assertions** (`var _ Iface = (*impl)(nil)`) for
  the real wrappers and `Delivery`.
- Tests run against the in-memory `AMQPAPI`/`AMQPChannelAPI` fakes (no live
  broker), race-clean.
