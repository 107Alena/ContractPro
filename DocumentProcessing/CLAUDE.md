# Document Processing Domain — CLAUDE.md

**ContractPro** Document Processing (DP) is a stateless Go service that ingests PDFs, extracts text and structure, builds semantic trees, and compares document versions.

## Directory Structure

- **architecture/** — Design documentation and configuration reference (in Russian)
- **development/** — Go source code (module: `contractpro/document-processing`)

## What DP Does

**Processing Pipeline:** PDF → validator → OCR/text extraction → text normalization → structure extraction → semantic tree → DM artifacts → event publish

**Comparison Pipeline:** fetch two semantic trees from DM → text diff + structural diff → publish diff events

DP is **event-driven**: receives commands via RabbitMQ, processes asynchronously, publishes status/result events.

## External Dependencies

- **Yandex Cloud Vision OCR** — for scanning PDF text recognition
- **Yandex Object Storage (S3-compatible)** — temporary artifact storage during processing
- **Redis** — idempotency store and processing state
- **RabbitMQ** — inter-domain event communication

## Key Implementation Details

- Hexagonal architecture: domain/model, domain/port, engine, application, infra, ingress, egress
- Go 1.26.1 with pdfcpu for PDF utilities
- Compile-time interface checks: `var _ Port = (*Impl)(nil)` pattern
- Domain errors carry machine-readable codes and retryable flags
- Deterministic diff output (sorted by Type+NodeID/Path)
- All code identifiers in English; documentation in Russian
