# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

**ContractPro** — AI-powered contract review platform for the Russian legal jurisdiction (ГК РФ). The system analyzes contracts to identify legal/financial risks, checks mandatory conditions, generates recommendations for improving wording, and provides summaries for non-legal users.

The **Document Processing** and **Document Management** domains are **fully implemented**. The **API/Backend Orchestrator** has complete architecture documentation ready for implementation.

## Domain Architecture

The system is decomposed into 8 domains communicating via event-driven architecture through a message broker:

1. **Document Processing (DP)** — stateless. PDF ingestion, OCR, text extraction, structure extraction, semantic tree building, version diff. **Fully implemented** — code in `DocumentProcessing/development/`.
2. **Legal Intelligence Core (LIC)** — stateless. The "legal brain": contract type classification, risk analysis, risk explanations, recommended wording, risk profile calculation, summary/report generation. Reads artifacts from Document Management (not directly from DP).
3. **Legal Knowledge Base** — stateful. Source of legal norms (ГК РФ) for training LIC neural networks.
4. **Organization Policy Management** — stateful. Client-specific templates, policies, checklists, strictness settings.
5. **Reporting Engine** — stateless. Transforms LIC outputs into user-facing reports (summary, detailed report, version comparison report, PDF/DOCX export).
6. **Document Management (DM)** — stateful. Document versioning, metadata, artifact storage (OCR results, semantic trees, reports). **Fully implemented** — code in `DocumentManagement/development/`, architecture in `DocumentManagement/architecture/`.
7. **User & Organization Management** — stateful. Auth, users, roles, permissions, org bindings.
8. **Payment Processing** — stateful. Service payment handling.

**API/Backend Orchestrator** — coordinating layer (not a domain). Single entry point for frontend and external integrations. JWT auth, RBAC, file upload, SSE real-time status, aggregation of data from DM/DP/OPM/UOM. **Architecture designed** — 11 docs in `ApiBackendOrchestrator/architecture/`, 37 implementation tasks in `ApiBackendOrchestrator/tasks.json`.

## Tech Stack

- **Language:** Go 1.26.1
- **PDF parsing:** pdfcpu
- **Unicode normalization:** golang.org/x/text
- **Config:** godotenv (`.env` files) + `DP_`-prefixed environment variables
- **Message broker:** RabbitMQ (event-driven inter-domain communication)
- **KV store:** Redis (idempotency, state)
- **External OCR:** Yandex Cloud Vision OCR
- **Temporary storage:** Yandex Object Storage (S3-compatible)
- **Observability:** structured logging, Prometheus metrics, OpenTelemetry tracing
- **Container:** Docker multi-stage build, Docker Compose (dev + prod)

## Document Processing — Code Structure

All DP code lives under `DocumentProcessing/development/`. Module: `contractpro/document-processing`.

```
cmd/dp-worker/main.go              — entrypoint
internal/
  app/                              — component wiring + graceful lifecycle (startup, shutdown)
  config/                           — env-based configuration (Load() with validation)
  domain/
    model/                          — domain entities: Job, Document, SemanticTree, Diff, DLQ, etc.
    port/                           — hexagonal ports: inbound, outbound, engine interfaces + DomainError
  application/
    lifecycle/                      — LifecycleManager: status transitions, event publishing, cleanup
    processing/                     — Processing Pipeline Orchestrator
    comparison/                     — Comparison Pipeline Orchestrator
    dmconfirmation/                 — DM Confirmation Awaiter (async DM persistence tracking)
    pendingresponse/                — Pending Response Registry (correlated async DM responses)
  engine/
    validator/                      — input validation (file size, MIME type, SSRF protection)
    fetcher/                        — source file fetcher (HTTP download + temp storage)
    ocr/                            — OCR integration adapter (text/scan PDF routing)
    textextract/                    — PDF/OCR text extraction + NFC normalization
    structure/                      — regex-based structure extraction (sections, clauses, appendices, party details)
    semantictree/                   — builds SemanticTree from ExtractedText + DocumentStructure
    comparison/                     — version comparison engine (text diff + structural diff)
  pdf/                              — pdfcpu wrapper: page count, text/scan detection, text extraction, CMap/font decoding
  infra/
    broker/                         — RabbitMQ client (publish/subscribe, auto-reconnect)
    kvstore/                        — Redis client (get/set/delete with TTL)
    objectstorage/                  — Yandex Object Storage (S3-compatible) client
    ocr/                            — Yandex Cloud Vision OCR client
    observability/                  — structured logging, Prometheus metrics, OpenTelemetry tracing
    concurrency/                    — semaphore-based concurrency limiter
    health/                         — HTTP health/readiness handler (/healthz, /readyz)
    httpdownloader/                 — HTTP file downloader with SSRF protection
  ingress/
    consumer/                       — RabbitMQ command consumer (deserialize + validate)
    dispatcher/                     — idempotency guard + concurrency control + routing
    idempotency/                    — Redis-based idempotency store
  egress/
    publisher/                      — event publisher (status, completion, failure events)
    dm/                             — DM sender (artifacts, tree requests) + DM receiver (responses)
    storage/                        — temporary artifact storage adapter
    dlq/                            — dead letter queue sender
  integration/                      — end-to-end pipeline tests with in-memory fakes
```

### Architecture Pattern

Hexagonal (ports & adapters):
- **Inbound ports** (`port/inbound.go`): `ProcessingCommandHandler`, `ComparisonCommandHandler`, `DMResponseHandler`
- **Engine ports** (`port/engine.go`): `InputValidatorPort`, `SourceFileFetcherPort`, `TextExtractionPort`, `StructureExtractionPort`, `SemanticTreeBuilderPort`, `OCRProcessorPort`, `VersionComparisonPort`
- **Outbound ports** (`port/outbound.go`): `TempStoragePort`, `OCRServicePort`, `EventPublisherPort`, `DMArtifactSenderPort`, `DMTreeRequesterPort`, `IdempotencyStorePort`, `ConcurrencyLimiterPort`, `DMConfirmationAwaiterPort`, `PendingResponseRegistryPort`, `DLQPort`
- **Domain errors** (`port/errors.go`): typed `DomainError` with error codes, retryable flag, and `errors.Is/As` unwrapping

### Pipelines

**Processing Pipeline:** Command → Validate → Fetch PDF → OCR (if scanned) → Extract Text → Extract Structure → Build Semantic Tree → Send Artifacts to DM → Await Confirmation → Publish Completion

**Comparison Pipeline:** Command → Request Semantic Trees from DM → Await Both → Compare → Send Diff to DM → Await Confirmation → Publish Completion

## Build, Test, Lint

All commands run from `DocumentProcessing/development/`:

```bash
make build          # go build ./cmd/dp-worker/
make test           # go test ./...
make lint           # go vet ./...
make docker-build   # docker build with git-based tag
```

## Deployment

Docker Compose files are at the project root:

```bash
# Local development (builds from source)
docker compose up --build

# Production (pre-built image)
docker compose -f docker-compose.prod.yaml up -d
```

See `DocumentProcessing/architecture/deployment.md` for the full deployment guide.

## Configuration

Config loads from `DP_`-prefixed env vars. See `DocumentProcessing/architecture/configuration.md` for the full reference.

**Required env vars** (service won't start without them):
- `DP_BROKER_ADDRESS` — RabbitMQ address
- `DP_KVSTORE_ADDRESS` — Redis address
- `DP_STORAGE_ENDPOINT`, `DP_STORAGE_BUCKET`, `DP_STORAGE_ACCESS_KEY`, `DP_STORAGE_SECRET_KEY` — Yandex Object Storage
- `DP_OCR_ENDPOINT`, `DP_OCR_API_KEY`, `DP_OCR_FOLDER_ID` — Yandex Cloud Vision OCR

**Key defaults:** max file 20 MB, max 100 pages, 120s job timeout, 5 concurrent jobs, OCR 10 RPS.

For local dev, create a `.env` file in `DocumentProcessing/development/`. Already-set env vars take precedence.

## Key Technical Decisions (Document Processing v1)

- Input format: **PDF only** (DOC/DOCX planned for later)
- Compile-time interface checks: `var _ Port = (*Impl)(nil)` pattern throughout
- Domain errors carry machine-readable codes and retryable flags for orchestrator decision-making
- Structure extraction uses Russian-language regex patterns for sections (Раздел), clauses (Пункт), appendices (Приложение), party details (Реквизиты)
- Text normalization: Unicode NFC + garbage character removal (C0/C1 control, zero-width, BOM, etc.)
- Version comparison: ID-based node matching with three-pass diff (removed, added, modified/moved)
- Deterministic diff output: sorted by Type+NodeID/Path; empty slices (not nil) for JSON `[]`
- Inter-domain communication: event-driven via RabbitMQ
- Dead letter queue for failed messages (post-mortem analysis)
- Graceful shutdown: ordered teardown (readiness → broker → HTTP → KV → observability)
- Constraints: ~1000 contracts/day initial load

## Documentation

- `docs/domain-decomposition.md` — high-level domain breakdown of the entire system
- `docs/ТЗ-1. Модуль проверки договора.md` — full requirements spec for the contract review module
- `DocumentProcessing/architecture/high-architecture.md` — detailed DP domain architecture
- `DocumentProcessing/architecture/configuration.md` — full env var reference for DP service
- `DocumentProcessing/architecture/deployment.md` — local and production deployment guide
- Each package under `DocumentProcessing/development/internal/` has its own `CLAUDE.md`
- `DocumentManagement/architecture/` — 12 architecture docs for DM (high-architecture, API spec, events, state machine, storage, security, etc.)
- `ApiBackendOrchestrator/architecture/` — 11 architecture docs for Orchestrator (high-architecture, OpenAPI spec, events, sequences, security, etc.)
- `ApiBackendOrchestrator/tasks.json` — 37 implementation tasks for Orchestrator
- `ApiBackendOrchestrator/architecture/CLAUDE.md` — quick reference for Orchestrator architecture

## Language

All documentation and requirements are in **Russian**. Code and technical identifiers (event names, statuses, field names) use **English**.
