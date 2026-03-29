# Ingress Layer — CLAUDE.md

Inbound message handling: subscribe to commands, deserialize, deduplicate, enforce concurrency limits, dispatch to orchestrators.

## consumer/ — Command Consumer

RabbitMQ subscriber for inbound processing commands.

- **consumer.go** — CommandConsumer: subscribes to topics (process-document, compare-versions), receives JSON messages
- **validate.go** — Message validation: required fields, field types, length constraints

Flow: RabbitMQ topic → Consumer → deserialize JSON → validate required fields → return Message or error.

Constructor: `NewCommandConsumer(broker BrokerPort)` returns (*Consumer, error).

## dispatcher/ — Message Dispatcher

Enforces idempotency and concurrency control before dispatching to orchestrators.

- **dispatcher.go** — Dispatcher: receives Message, checks IdempotencyStore for duplicate (early return if completed), acquires ConcurrencyLimiter slot, then dispatches to appropriate handler (ProcessingCommandHandler or ComparisonCommandHandler)

Handlers called (from domain/port/inbound.go):
- ProcessingCommandHandler.HandleProcessDocument(ctx, cmd) — for process-document topic
- ComparisonCommandHandler.HandleCompareVersions(ctx, cmd) — for compare-versions topic

Constructor: `NewDispatcher(idempotency IdempotencyStorePort, limiter ConcurrencyLimiterPort)` returns *Dispatcher.

## idempotency/ — Idempotency Store

Redis-backed deduplication store with TTL expiry.

- **store.go** — RedisIdempotencyStore: implements IdempotencyStorePort
  - Check(msgID) — returns (completed bool, result interface{}, error)
  - Register(msgID, ttl) — marks message as in-progress
  - MarkCompleted(msgID, result) — marks message as completed with result for replay

All operations use Redis transactions for atomicity. Completed results cached with TTL (e.g., 24 hours).

Constructor: `NewRedisIdempotencyStore(client KVStorePort, ttl time.Duration)` returns (*Store, error).

## Message Flow

```
RabbitMQ → Consumer (deserialize + validate)
         → Dispatcher (dedup check)
         → Dispatcher (acquire concurrency slot)
         → Handler (ProcessingCommandHandler or ComparisonCommandHandler)
         → Orchestrator (actual processing)
```

## Patterns

- All handlers use hexagonal inbound port interfaces
- Idempotency key = message ID (from command/topic source)
- Concurrency limiter acquired per-message, released after handler completes
- Errors propagate back to consumer for nack/retry via broker
