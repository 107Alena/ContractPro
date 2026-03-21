# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

**ContractPro** — AI-powered contract review platform for the Russian legal jurisdiction (ГК РФ). The system analyzes contracts to identify legal/financial risks, checks mandatory conditions, generates recommendations for improving wording, and provides summaries for non-legal users.

The project is in **active development** — the Document Processing domain has working application code (domain model, engine layer, application services) with 212+ tests passing. Other domains remain in the architecture/requirements phase.

## Domain Architecture

The system is decomposed into 8 domains communicating via event-driven architecture through a message broker:

1. **Document Processing (DP)** — stateless. PDF ingestion, OCR, text extraction, structure extraction, semantic tree building, version diff. **Active development** — code in `DocumentProcessing/development/`.
2. **Legal Intelligence Core (LIC)** — stateless. The "legal brain": contract type classification, risk analysis, risk explanations, recommended wording, risk profile calculation, summary/report generation. Reads artifacts from Document Management (not directly from DP).
3. **Legal Knowledge Base** — stateful. Source of legal norms (ГК РФ) for training LIC neural networks.
4. **Organization Policy Management** — stateful. Client-specific templates, policies, checklists, strictness settings.
5. **Reporting Engine** — stateless. Transforms LIC outputs into user-facing reports (summary, detailed report, version comparison report, PDF/DOCX export).
6. **Document Management (DM)** — stateful. Document versioning, metadata, artifact storage (OCR results, semantic trees, reports).
7. **User & Organization Management** — stateful. Auth, users, roles, permissions, org bindings.
8. **Payment Processing** — stateful. Service payment handling.

## Tech Stack

- **Language:** Go 1.26.1
- **PDF parsing:** pdfcpu
- **Unicode normalization:** golang.org/x/text
- **Config:** godotenv (`.env` files) + `DP_`-prefixed environment variables
- **Message broker:** RabbitMQ (event-driven inter-domain communication)
- **External OCR:** Yandex Cloud Vision OCR
- **Temporary storage:** Yandex Object Storage (S3-compatible)
- **REST framework (planned):** Gin
- **Async framework (planned):** Encore

## Document Processing — Code Structure

All DP code lives under `DocumentProcessing/development/`. Module: `contractpro/document-processing`.

```
cmd/dp-worker/main.go          — entrypoint (skeleton)
internal/
  config/                       — env-based configuration (Load() with validation)
  domain/
    model/                      — domain entities: Job, Document, SemanticTree, Diff, etc.
    port/                       — hexagonal ports: inbound, outbound, engine interfaces + DomainError
  application/
    lifecycle/                  — LifecycleManager: status transitions, event publishing, cleanup
    warning/                    — thread-safe WarningCollector for pipeline warnings
    processing/                 — processing pipeline orchestrator (placeholder)
    comparison/                 — comparison pipeline orchestrator (placeholder)
  engine/
    validator/                  — input validation (file size, mime type, required fields)
    ocr/                        — OCR integration adapter (text/scan PDF routing)
    textextract/                — PDF/OCR text extraction + NFC normalization
    structure/                  — regex-based structure extraction (sections, clauses, appendices, party details)
    semantictree/               — builds SemanticTree from ExtractedText + DocumentStructure
    comparison/                 — version comparison engine (text diff + structural diff)
    fetcher/                    — source file fetcher (placeholder)
  pdf/                          — pdfcpu wrapper: page count, text/scan detection, text extraction
  infra/                        — infrastructure adapters (placeholders: broker, objectstorage, ocr, kvstore, observability, concurrency)
  ingress/                      — inbound adapters (placeholders: consumer, idempotency)
  egress/                       — outbound adapters (placeholders: publisher, dm, storage)
```

### Implemented Engine Components

| Component | File | Port Interface |
|-----------|------|----------------|
| Input Validator | `engine/validator/validator.go` | `InputValidatorPort` |
| OCR Adapter | `engine/ocr/adapter.go` | (internal, routes text/scan PDFs) |
| Text Extraction | `engine/textextract/extractor.go` | `TextExtractionPort` |
| Structure Extraction | `engine/structure/extractor.go` | `StructureExtractionPort` |
| Semantic Tree Builder | `engine/semantictree/builder.go` | `SemanticTreeBuilderPort` |
| Version Comparison | `engine/comparison/comparer.go` | `VersionComparisonPort` |
| PDF Utilities | `pdf/pdf.go` | `PDFTextExtractor` (consumer-side) |

### Architecture Pattern

Hexagonal (ports & adapters):
- **Inbound ports** (`port/inbound.go`): `ProcessingCommandHandler`, `ComparisonCommandHandler`, `DMResponseHandler`
- **Engine ports** (`port/engine.go`): `InputValidatorPort`, `TextExtractionPort`, `StructureExtractionPort`, `SemanticTreeBuilderPort`, `VersionComparisonPort`
- **Outbound ports** (`port/outbound.go`): `TempStoragePort`, `OCRServicePort`, `EventPublisherPort`, `DMArtifactSenderPort`, `IdempotencyStorePort`, etc.
- **Domain errors** (`port/errors.go`): typed `DomainError` with error codes, retryable flag, and `errors.Is/As` unwrapping

## Build, Test, Lint

All commands run from `DocumentProcessing/development/`:

```bash
make build    # go build ./cmd/dp-worker/
make test     # go test ./...
make lint     # go vet ./...
```

## Configuration

Config loads from `DP_`-prefixed env vars. See `DocumentProcessing/architecture/configuration.md` for the full reference.

**Required env vars** (service won't start without them):
- `DP_BROKER_ADDRESS` — message broker address
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
- Constraints: ~1000 contracts/day initial load

## Documentation

- `docs/domain-decomposition.md` — high-level domain breakdown of the entire system
- `docs/ТЗ-1. Модуль проверки договора.md` — full requirements spec for the contract review module (user requirements, functional requirements, NFRs)
- `DocumentProcessing/architecture/high-architecture.md` — detailed DP domain architecture (entities, components, flows)
- `DocumentProcessing/architecture/configuration.md` — full env var reference for DP service
- `progress.md` — task tracking and implementation progress

## Language

All documentation and requirements are in **Russian**. Code and technical identifiers (event names, statuses, field names) use **English**.
