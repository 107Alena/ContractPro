# Integration Package — CLAUDE.md

End-to-end integration tests for Document Management pipelines using in-memory fakes.

## Main Components

- **testinfra.go** — Test infrastructure: 14 in-memory fakes (memoryTransactor, memoryDocumentRepository, memoryVersionRepository, memoryArtifactRepository, memoryAuditRepository, memoryOutboxRepository, memoryObjectStorage, memoryIdempotencyStore with SetNX, memoryDiffRepository, recordingDLQPort, recordingConfirmationPublisher, recordingLogger, noopFallbackMetrics, noopIdempotencyMetrics). testHarness wires real application services (ArtifactIngestionService, ArtifactQueryService, DiffStorageService, OutboxWriter, IdempotencyGuard) with in-memory fakes. Helpers: defaultDocument, defaultVersion, defaultDPEvent, defaultLICEvent, defaultREEvent
- **dp_ingestion_test.go** — 14 tests: DP artifact ingestion happy path, warnings, content hash, idempotency dedup, version not found, fallback version_id/org_id, outbox aggregate_id (REV-010), compensation on tx failure, context cancelled, audit details, storage key convention, end-to-end idempotency
- **full_pipeline_test.go** — 11 tests: full DP→LIC→RE pipeline (PENDING→FULLY_READY), artifacts at each stage, audit trail integrity, out-of-order rejection (LIC before DP, RE before LIC), duplicate DP after LIC, GetSemanticTree/GetArtifacts happy path and edge cases
- **error_scenarios_test.go** — 5 tests: Object Storage partial failure + compensation + retry, concurrent version creation (both succeed), document not found, Redis unavailable → DB fallback, terminal status → status transition error
- **audit_trail_test.go** — 7 tests: all 8 action types verified, ARTIFACT_SAVED/STATUS_CHANGED details, async ARTIFACT_READ for semantic tree and GetArtifacts, diff saved details, append-only enforcement
- **tenant_isolation_test.go** — 10 tests: wrong org rejected for DP/LIC/RE/diff/queries, correct org succeeds, empty org fallback bypass, sync API cross-org isolation

## Run Tests

```bash
# All tests including integration:
make test

# Integration tests only:
go test ./internal/integration/... -race -count=1
```

## Test Pattern

1. Use testHarness factories (newTestHarness, newTestHarnessWithRecordingPublisher) to build in-memory app
2. Seed documents and versions in memory fakes
3. Call application service methods directly (HandleDPArtifacts, etc.)
4. Assert: descriptors in artifact repo, blobs in object storage, outbox entries, audit records, idempotency state

## No External Dependencies

All integration tests use in-memory fakes: no PostgreSQL, Redis, RabbitMQ, or S3 required. Thread-safe fakes (sync.RWMutex) for -race safety.
