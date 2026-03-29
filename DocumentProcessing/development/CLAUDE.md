# Document Processing Service — Development CLAUDE.md

Go service for ContractPro PDF processing: text extraction, structure extraction, semantic tree building, and version comparison.

## Getting Started

**Module:** `contractpro/document-processing`
**Go version:** 1.26.1
**Entry point:** `cmd/dp-worker/main.go`

**Build commands:**
```bash
make build          # Compile dp-worker binary
make test           # Run all tests (212+ passing)
make lint           # go vet ./...
make docker-build   # Build Docker image
```

## Configuration

Environment variables with `DP_` prefix (see `/architecture/configuration.md` for full reference):
- `DP_BROKER_ADDRESS` — RabbitMQ address (required)
- `DP_STORAGE_*` — Yandex Object Storage credentials (required)
- `DP_OCR_*` — Yandex Cloud Vision OCR endpoints (required)
- `DP_KVSTORE_ADDRESS` — Redis address (required)
- Load from `.env` file or system environment

## Code Structure

```
internal/
  domain/         — model (entities), port (hexagonal interfaces, domain errors)
  engine/         — business logic (validator, ocr adapter, text/structure/semantic tree/comparison engines)
  application/    — orchestrators, lifecycle manager, warning collector
  infra/          — external service clients (broker, kvstore, objectstorage, ocr, observability, etc.)
  ingress/        — message consumer, dispatcher, idempotency handler
  egress/         — event publisher, DM sender, temp storage adapter, DLQ handler
  pdf/            — pdfcpu wrapper utilities
  integration/    — integration tests
  app/            — wiring and lifecycle
```

## Architecture Pattern

**Hexagonal (Ports & Adapters):**
- Inbound ports: ProcessingCommandHandler, ComparisonCommandHandler, DMResponseHandler
- Engine ports: InputValidatorPort, TextExtractionPort, StructureExtractionPort, SemanticTreeBuilderPort, VersionComparisonPort
- Outbound ports: TempStoragePort, OCRServicePort, EventPublisherPort, DMArtifactSenderPort, IdempotencyStorePort

**Design principles:**
- Compile-time interface checks: `var _ Port = (*Impl)(nil)` throughout
- Domain errors carry codes and retryable flags
- Deterministic diff output (sorted by Type+NodeID/Path)
- All identifiers in English, docs in Russian
