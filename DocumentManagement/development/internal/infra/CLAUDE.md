# Infrastructure Layer — CLAUDE.md

External service clients and platform adapters. All clients implement hexagonal outbound ports.

## postgres/ — PostgreSQL Client & Repositories

Database layer with pgx driver, connection pool, transaction support, and 9 repositories.

- **client.go** — Client: pgxpool.Pool wrapper. NewPostgresClient(ctx, cfg), Ping(), Close(), Pool()
- **context.go** — DBTX interface (Exec/Query/QueryRow). ConnFromCtx extracts pool or tx from context. InjectPool/HasTx for transaction join semantics
- **transactor.go** — Transactor: implements port.Transactor. WithTransaction with join semantics for nested calls, deferred rollback for panic safety
- **migrate.go** — Migrator: golang-migrate wrapper with embedded SQL files. Up/Down/MigrateToVersion/Version
- **pg_error.go** — Helpers: isPgUniqueViolation (23505), isPgFKViolation (23503), nullableString converters
- **document_repository.go** — DocumentRepository: Insert, FindByID, FindByIDForUpdate (BRE-005), List, Update, ExistsByID, FindDeletedOlderThan, DeleteByID. Tenant isolation via WHERE organization_id
- **version_repository.go** — VersionRepository: Insert, FindByID, FindByIDForUpdate (BRE-001), List, Update, NextVersionNumber (MAX+1), DeleteByDocument, ListByDocument, FindStaleInIntermediateStatus (watchdog)
- **artifact_repository.go** — ArtifactRepository: Insert, FindByVersionAndType, ListByVersion, ListByVersionAndTypes, DeleteByVersion
- **diff_repository.go** — DiffRepository: Insert, FindByVersionPair, ListByDocument, DeleteByDocument
- **audit_repository.go** — AuditRepository: Insert, List (dynamic WHERE), DeleteByDocument (with retention_override SET LOCAL)
- **outbox_repository.go** — OutboxRepository: Insert multi-row, FetchUnpublished (FOR UPDATE SKIP LOCKED ORDER BY aggregate_id, created_at), MarkPublished, DeletePublished (batched), PendingStats
- **dlq_repository.go** — DLQRepository: Insert, FindByFilter (dynamic WHERE + pagination), IncrementReplayCount
- **orphan_candidate_repository.go** — OrphanCandidateRepository: FindOlderThan, ExistsByStorageKey (cross-tenant), DeleteByKeys, Insert (ON CONFLICT DO NOTHING)
- **fallback_resolver.go** — FallbackResolver: ResolveByDocumentID (cross-tenant lookup, temporary for REV-001/REV-002)
- **audit_partition_manager.go** — AuditPartitionManager: EnsurePartitions (monthly CREATE TABLE), DropPartitionsOlderThan
- **migrations/** — 5 SQL migrations: initial schema (7 tables, 12 indexes), DLQ records, RLS policies (5 tables), audit partitions (RANGE by created_at), audit protection (trigger + role)

Constructor: `NewPostgresClient(ctx, cfg)` returns (*Client, error). Repositories use `NewXxxRepository()`.

## broker/ — RabbitMQ Client

Message broker for publishing and subscribing to events.

- **client.go** — Client: implements port.BrokerPublisherPort. Publish with publisher confirms, Subscribe with manual ack and configurable prefetch, DeclareTopology (7 incoming + 3 DLQ quorum queues), concurrent dispatch via Semaphore limiter (BRE-007)
- **reconnect.go** — Auto-reconnect with exponential backoff (1s-30s, 25% jitter)
- **errors.go** — mapError: AMQP codes → DomainError, context passthrough

Constructor: `NewClient(cfg, consumerCfg, limiter)` returns (*Client, error).

## objectstorage/ — S3-Compatible Client

Blob storage for artifacts and diffs.

- **client.go** — Client: implements port.ObjectStoragePort. PutObject, GetObject, DeleteObject, HeadObject, GeneratePresignedURL (negative expiry guard), DeleteByPrefix (paginated). AWS SDK v2 with RetryMaxAttempts=3
- **keys.go** — ArtifactKey ({org_id}/{doc_id}/{ver_id}/{type}), DiffKey, VersionPrefix, DocumentPrefix, ContentTypeForArtifact (JSON/PDF/DOCX)
- **errors.go** — mapError: S3 error codes → DomainError, nonRetryableCodes set

Constructor: `NewClient(cfg)` returns (*Client, error).

## kvstore/ — Redis Client

Key-value store for idempotency records with TTL.

- **client.go** — Client: implements port.IdempotencyStorePort. Get (JSON unmarshal → *IdempotencyRecord), Set (JSON marshal + TTL), SetNX (atomic set-if-not-exists), Delete, Ping, Close
- **errors.go** — mapError: redis.Nil → non-retryable, generic → retryable

Constructor: `NewClient(cfg)` returns (*Client, error).

## observability/ — Logging, Metrics, Tracing

Unified observability stack.

- **context.go** — EventContext (CorrelationID, JobID, DocumentID, VersionID, OrganizationID, Stage). WithEventContext/EventContextFrom for ctx enrichment
- **logger.go** — Logger: slog.Logger wrapper with JSON output, auto-enrichment from EventContext, With() for component-scoped child loggers
- **metrics.go** — Metrics: 30+ Prometheus metrics in dedicated registry. Key metrics: dm_events_received/processed_total, dm_event_processing_duration_seconds, dm_artifacts_stored_total, dm_api_requests_total, dm_outbox_pending_count, dm_dlq_messages_total, dm_stuck_versions_count, dm_circuit_breaker_state. Bridge methods for consumer-side interfaces
- **tracer.go** — Tracer: OpenTelemetry OTLP/HTTP exporter, noop fallback when disabled
- **handler.go** — MetricsHandler: /metrics endpoint via promhttp
- **observability.go** — SDK composite: Logger + Metrics + Tracer. New(ctx, cfg), Shutdown()

Constructor: `New(ctx, cfg)` returns (*SDK, error).

## health/ — Health & Readiness Handler

HTTP handlers for orchestrator health checks.

- **health.go** — Handler: GET /healthz (liveness, always 200), GET /readyz (readiness, core checkers block, non-core informational per REV-024). Concurrent health checks with per-component timeout and panic recovery

Constructor: `NewHandler(coreCheckers, nonCoreCheckers, opts...)` returns *Handler.

## concurrency/ — Semaphore Limiter

Channel-based concurrency limiter for consumer backpressure (BRE-007).

- **limiter.go** — Semaphore: Acquire(ctx)/Release, ActiveCount(), Capacity(). Used by broker consumer for concurrent message dispatch

Constructor: `NewSemaphore(maxConcurrent, logger)` returns *Semaphore.

## circuitbreaker/ — Object Storage Circuit Breaker

Decorator pattern around ObjectStoragePort with gobreaker (BRE-014).

- **objectstorage.go** — ObjectStorageBreaker: implements port.ObjectStoragePort. All 6 methods wrapped with cb.Execute. Per-event budget (35s default). Context errors and non-retryable errors don't trip circuit. cancelOnCloseReader for GetObject body stream
- **errors.go** — ErrCircuitOpen sentinel

Constructor: `NewObjectStorageBreaker(inner, cfg, reporter)` returns *ObjectStorageBreaker.

## Patterns

- All clients use constructor pattern returning (*Client, error) or panicking on nil deps
- Compile-time interface checks: `var _ Port = (*Client)(nil)` in implementation files
- ConnFromCtx pattern: repositories get DBTX (pool or tx) from context transparently
- Graceful shutdown hooks: all stateful clients implement Close()
- Error mapping: external errors → DomainError with retryable classification
