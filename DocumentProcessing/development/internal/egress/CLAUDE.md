# Egress Layer — CLAUDE.md

Outbound communication: event publishing, Document Management integration, artifact storage, and dead letter queue handling.

## publisher/ — Event Publisher

Publishes domain events to RabbitMQ topics (implements EventPublisherPort).

- **publisher.go** — PublishEvent(ctx, event) routes events to topic based on event type:
  - StatusChanged → status-changed topic
  - ProcessingCompleted → processing-completed topic
  - ProcessingFailed → processing-failed topic
  - ComparisonCompleted → comparison-completed topic
  - ComparisonFailed → comparison-failed topic
  - ArtifactsReady → artifacts-ready topic
  - DiffReady → diff-ready topic

Serializes event to JSON, publishes via broker with retry logic.

Constructor: `NewEventPublisher(broker BrokerPort)` returns *Publisher.

## dm/ — Document Management Adapters

Bidirectional communication with DM service (sender/receiver pattern).

- **sender.go** — DMSender:
  - SendArtifacts(ctx, cmd) — publish to dm-artifacts topic (implements DMArtifactSenderPort)
  - RequestSemanticTree(ctx, req) — publish to dm-tree-request topic (implements DMTreeRequesterPort)
- **receiver.go** — DMReceiver: subscribes to DM response topics (dm-response, dm-tree-response), deserializes, dispatches to DMResponseHandler and PendingResponseRegistryPort
- **validate.go** — Validation helpers for DM response message structure

Constructor: `NewDMSender(broker BrokerPort)` returns *Sender.
Constructor: `NewDMReceiver(broker BrokerPort)` returns (*Receiver, error).

## storage/ — Temporary Artifact Storage Adapter

Wrapper around objectstorage client with prefix isolation (implements TempStoragePort).

- **adapter.go** — TempStorageAdapter: Upload(ctx, key, data), Download(ctx, key), Delete(ctx, key), DeleteByPrefix(ctx, prefix)
  - All keys prefixed with jobID for isolation: `jobs/{jobID}/{key}`
  - Handles large binary artifacts (OCR results, semantic trees, diffs)

Constructor: `NewTempStorageAdapter(client ObjectStoragePort)` returns *Adapter.

## dlq/ — Dead Letter Queue Handler

Publishes failed messages for post-mortem analysis (implements DLQPort).

- **sender.go** — DLQSender: PublishFailedMessage(ctx, msg, reason, error) → dlq topic
  - Captures original message, failure reason, error details, timestamp
  - Used after all retry attempts exhausted

Constructor: `NewDLQSender(broker BrokerPort)` returns *Sender.

## Message Flow (Outbound)

```
Orchestrator → EventPublisher → RabbitMQ (status-changed, processing-completed, etc.)
            → DMSender → RabbitMQ (dm-artifacts, dm-tree-request)
            → TempStorageAdapter → Object Storage (artifacts, trees, diffs)
            → DLQSender → RabbitMQ (dlq) [on fatal errors]

DM response topics → DMReceiver → DMResponseHandler + PendingResponseRegistry
```

## Patterns

- All adapters use hexagonal outbound port interfaces
- Errors from external services wrapped in DomainError with retryable flag
- DM sender/receiver decouples DP from DM implementation details
- Temp storage prefix isolation prevents key collisions across jobs
