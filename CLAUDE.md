# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

**ContractPro** — AI-powered contract review platform for the Russian legal jurisdiction (ГК РФ). The system analyzes contracts to identify legal/financial risks, checks mandatory conditions, generates recommendations for improving wording, and provides summaries for non-legal users.

The project is currently in the **architecture and requirements phase** — no application code has been written yet. The repository contains domain decomposition, architecture documents, and technical specifications.

## Domain Architecture

The system is decomposed into 8 domains communicating via event-driven architecture through a message broker:

1. **Document Processing (DP)** — stateless. PDF ingestion, OCR, text extraction, structure extraction, semantic tree building, version diff. This domain has detailed architecture docs in `DocumentProcessing/architecture/`.
2. **Legal Intelligence Core (LIC)** — stateless. The "legal brain": contract type classification, risk analysis, risk explanations, recommended wording, risk profile calculation, summary/report generation. Reads artifacts from Document Management (not directly from DP).
3. **Legal Knowledge Base** — stateful. Source of legal norms (ГК РФ) for training LIC neural networks.
4. **Organization Policy Management** — stateful. Client-specific templates, policies, checklists, strictness settings.
5. **Reporting Engine** — stateless. Transforms LIC outputs into user-facing reports (summary, detailed report, version comparison report, PDF/DOCX export).
6. **Document Management (DM)** — stateful. Document versioning, metadata, artifact storage (OCR results, semantic trees, reports).
7. **User & Organization Management** — stateful. Auth, users, roles, permissions, org bindings.
8. **Payment Processing** — stateful. Service payment handling.

## Key Technical Decisions (Document Processing v1)

- Input format: **PDF only** (DOC/DOCX planned for later)
- External OCR: **Yandex Cloud Vision OCR**
- Temporary artifact storage: **Yandex Object Storage**
- Programming language: Go
- Для обычного Rest микросервиса используется фреймворк Gin для языка программирования Go
- Для асинхронной работы используется фреймворк Encore для языка программирования Go
- Взаимодействия между домменными областями работает через event-driven подход с асинхронными вызовами
- Constraints: max file 20 MB, max 100 pages, 120s job timeout, ~1000 contracts/day initial load

## Documentation Structure

- `docs/domain-decomposition.md` — high-level domain breakdown of the entire system
- `docs/ТЗ-1. Модуль проверки договора.md` — full requirements spec for the contract review module (user requirements, functional requirements, NFRs)
- `DocumentProcessing/architecture/high-architecture.md` - верхнеуровневая архитектура сервиса DocumentProcessing

## Language

All documentation and requirements are in **Russian**. Code and technical identifiers (event names, statuses, field names) use **English**.
