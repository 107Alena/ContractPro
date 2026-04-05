# Document Management Service — Development CLAUDE.md

Go service for ContractPro document versioning, artifact storage, and metadata management. Stateful domain: persists documents, versions, artifacts, diffs, and audit records.

## Getting Started

**Module:** `contractpro/document-management`
**Go version:** 1.26.1
**Entry points:** `cmd/dm-service/main.go` (service), `cmd/dm-migrate/main.go` (migration CLI)

**Build commands:**
```bash
make build          # Compile dm-service binary
make build-migrate  # Compile dm-migrate binary
make test           # Run all tests (500+ passing across 30 packages)
make lint           # go vet ./...
make docker-build   # Build Docker image
```

## Configuration

Environment variables with `DM_` prefix (see `architecture/configuration.md` for full reference):
- `DM_DB_DSN` — PostgreSQL connection string (required)
- `DM_BROKER_ADDRESS` — RabbitMQ address (required)
- `DM_STORAGE_*` — S3-compatible Object Storage credentials (required)
- `DM_KVSTORE_ADDRESS` — Redis address (required)
- `DM_HTTP_PORT` — API server port (default 8080)
- Load from `.env` file or system environment

## Code Structure

```
cmd/
  dm-service/       — main.go: wiring, startup, graceful shutdown
  dm-migrate/       — main.go: migration CLI (up/down/goto/version)
internal/
  domain/           — model (entities, events, topics), port (hexagonal interfaces, domain errors)
  application/      — services: ingestion, query, lifecycle, version, diff, tenant, watchdog,
                      orphancleanup, retention (3 jobs)
  infra/            — external clients: postgres (repos, migrator), broker, objectstorage,
                      kvstore, observability, health, concurrency, circuitbreaker
  ingress/          — consumer (7 RabbitMQ topics), api (REST + auth + rate limiting),
                      idempotency (Redis guard + DB fallback)
  egress/           — confirmation (10 types), notification (5 types),
                      outbox (writer + poller + metrics), dlq (sender)
  config/           — env-based config with DM_ prefix + validation
  integration/      — end-to-end tests with in-memory fakes
```

## Architecture Pattern

**Hexagonal (Ports & Adapters):**
- Inbound ports: DocumentLifecycleHandler, VersionManagementHandler, ArtifactIngestionHandler, ArtifactQueryHandler, DiffStorageHandler
- Outbound ports: 18 interfaces (6 repositories + Transactor + ObjectStoragePort + BrokerPublisherPort + ConfirmationPublisherPort + NotificationPublisherPort + IdempotencyStorePort + AuditPort + DLQPort + DLQRepository + OrphanCandidateRepository + AuditPartitionManager + DocumentFallbackResolver)
- Domain errors: 20 error codes with retryable flags and errors.Is/As support

**Design principles:**
- Compile-time interface checks: `var _ Port = (*Impl)(nil)` throughout
- Domain errors carry codes and retryable flags
- Transactional outbox for reliable event publishing (FIFO by aggregate_id)
- Tenant isolation: all SQL queries filter by organization_id + RLS policies
- Background jobs: watchdog, orphan cleanup, 3 retention jobs (blob/meta/audit partition)
- All identifiers in English, docs in Russian

## Pipelines

**Artifact Ingestion:** Event (DP/LIC/RE) → Validate → Tenant check → Save blobs → DB tx (descriptors + status + audit + outbox) → Publish confirmation + notification

**Artifact Query:** Request → Find descriptors → Read from storage → Integrity check → Publish response (async) or return data (sync)

**Artifact Access (sync API):** GET /artifacts/{type} → JSON content for metadata artifacts, 302 redirect with presigned URL for blob types (SOURCE_FILE, EXPORT_PDF, EXPORT_DOCX)

**Document Lifecycle (sync API):** HTTP → Auth → Rate limit → CRUD → Audit → Response

## External Dependencies

- **PostgreSQL** — persistent storage (documents, versions, artifacts, diffs, audit, outbox)
- **Redis** — idempotency store with TTL (fallback to DB when unavailable)
- **RabbitMQ** — inter-domain event communication (7 incoming + 15 outgoing + 3 DLQ topics)
- **S3-compatible Object Storage** — blob storage for artifacts and diffs
