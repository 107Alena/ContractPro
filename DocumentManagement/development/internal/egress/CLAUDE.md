# Egress Layer — CLAUDE.md

Outbound communication: event publishing via transactional outbox, confirmation/notification publishers, and dead letter queue.

## confirmation/ — Confirmation Publisher

Publishes 10 types of confirmation events back to producer domains (implements ConfirmationPublisherPort).

- **confirmation.go** — ConfirmationPublisher: publishes directly to RabbitMQ (not via outbox — these are responses, not domain events). 10 methods:
  - DP: PublishDPArtifactsPersisted/PersistFailed, PublishSemanticTreeProvided, PublishDiffPersisted/PersistFailed
  - LIC: PublishArtifactsProvided, PublishLICArtifactsPersisted/PersistFailed
  - RE: PublishREReportsPersisted/PersistFailed
- EventMeta (correlation_id, timestamp) copied from incoming event (REV-021)

Constructor: `NewConfirmationPublisher(broker, cfg)`. Panics on nil broker or empty topics.

## notification/ — Notification Publisher

Publishes 5 types of notification events to downstream domains (implements NotificationPublisherPort).

- **notification.go** — NotificationPublisher: publishes directly to RabbitMQ. 5 methods:
  - PublishVersionProcessingArtifactsReady (→ LIC)
  - PublishVersionAnalysisArtifactsReady (→ RE)
  - PublishVersionReportsReady (→ orchestrator)
  - PublishVersionCreated (→ orchestrator)
  - PublishVersionPartiallyAvailable (BRE-010, → orchestrator)

Constructor: `NewNotificationPublisher(broker, cfg)`. Panics on nil broker or empty topics.

## outbox/ — Transactional Outbox

Reliable event publishing via DB-backed outbox pattern.

- **writer.go** — OutboxWriter: Write/WriteMultiple inserts PENDING entries within the current DB transaction. aggregate_id = version_id for FIFO ordering (REV-010). UUID v4 via crypto/rand. TopicEvent struct for batch writes
- **poller.go** — OutboxPoller: background goroutine polling DB for PENDING entries. FetchUnpublished (FOR UPDATE SKIP LOCKED, ORDER BY aggregate_id, created_at) → Publish to broker → MarkPublished (CONFIRMED). FIFO protection: if one aggregate fails, skip remaining entries for that aggregate in batch (BRE-006). Cleanup: batched DELETE of CONFIRMED entries older than CleanupHours (BRE-018). At-least-once delivery (downstream must be idempotent)
- **metrics.go** — OutboxMetricsCollector: background goroutine collecting PendingStats from DB → updates dm_outbox_pending_count and dm_outbox_oldest_pending_age_seconds gauges (REV-022)

Constructors: `NewOutboxWriter(repo)`, `NewOutboxPoller(repo, transactor, broker, metrics, logger, cfg)`, `NewOutboxMetricsCollector(repo, metrics, logger, interval)`.

## dlq/ — Dead Letter Queue Sender

Publishes failed messages for post-mortem analysis and replay (implements DLQPort).

- **sender.go** — Sender: dual-write pattern — persist DLQRecord to DB (replay source of truth) + publish to broker (alerting/monitoring). resolveTopic by DLQCategory: ingestion → dm.dlq.ingestion-failed, query → dm.dlq.query-failed, invalid → dm.dlq.invalid-message

Constructor: `NewSender(broker, repo, metrics, logger, ingestionTopic, queryTopic, invalidTopic)`.

## Event Flow (Outbound)

```
Application Service → OutboxWriter.Write() [within DB transaction]
                    → OutboxPoller [background] → Broker (publish + confirm)
                    → OutboxMetricsCollector [background] → Prometheus gauges

Async responses     → ConfirmationPublisher → Broker (direct publish)
                    → NotificationPublisher → Broker (direct publish)

Failed messages     → DLQ Sender → DB (persist) + Broker (alert)
```

## Patterns

- Outbox writer operates within the caller's DB transaction for atomicity
- Outbox poller uses FOR UPDATE SKIP LOCKED for concurrent-safe polling
- Confirmation/notification publishers bypass outbox (direct publish) — they are responses, not domain state changes
- DLQ uses dual-write: DB for reliable replay, broker for real-time alerting
- All publishers use consumer-side BrokerPublisher interface (not the full Client)
- Split stop+done channels for safe graceful shutdown of background goroutines
