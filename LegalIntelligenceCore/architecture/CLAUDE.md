# Legal Intelligence Core Architecture — CLAUDE.md

Russian-language design documentation for the Legal Intelligence Core (LIC) — the «legal brain» of ContractPro. LIC is a stateless Go service that runs a 9-agent AI pipeline on each contract version (classification, parameter extraction, party consistency, mandatory conditions, risk detection, recommendations, summary, detailed report, risk delta).

## What LIC Does

- **Trigger**: subscribes to `dm.events.version-artifacts-ready` from Document Management.
- **Input**: requests artifacts (`SEMANTIC_TREE`, `EXTRACTED_TEXT`, `DOCUMENT_STRUCTURE`, `PROCESSING_WARNINGS`) from DM via async request-response.
- **Pipeline**: runs 9 AI agents in 6 stages with parallel execution where possible (Stage 1: Type Classifier ‖ Key Params; Stage 2: Party Consistency; Stage 3: Mandatory Conditions ‖ Risk Detection; Stage 4: Recommendation; Stage 5: Summary ‖ Detailed Report; Stage 6: Risk Delta — only for `RE_CHECK`).
- **Pause-and-resume**: at low classification confidence (< threshold) — publishes `lic.events.classification-uncertain`, persists pipeline state in Redis (TTL 25h), waits for `orch.commands.user-confirmed-type`, then continues.
- **Output**: publishes `lic.artifacts.analysis-ready` (`LegalAnalysisArtifactsReady`) to DM with all 8 artifacts (+ `risk_delta` for RE_CHECK).
- **Status events**: `lic.events.status-changed` (IN_PROGRESS / COMPLETED / FAILED) to Orchestrator.
- **Stateless**: no own database; uses Redis only for idempotency, pending state, rate limiting, version meta cache.

## Architecture Files

**high-architecture.md** — The foundational document:
- Boundaries (what's in/out)
- 20 architectural assumptions (`ASSUMPTION-LIC-01..20`)
- Data model (in-memory entities)
- External statuses (3) and internal stages (`STAGE_*`)
- Component layout (ingress / application / agents / agent-infra / llm-providers / egress / cross-cutting)
- 10 working scenarios (happy path, low confidence, RE_CHECK, errors, timeout)
- 7 ADRs

**ai-agents-pipeline.md** — Full text of all 9 agent system prompts (in Russian) with:
- Role, Applicable law, Task, Input, Output schema, Correctness criteria, Few-shot examples, Prohibitions, Prompt injection guard
- JSON Schema for each agent output
- Token budget and LLM parameters
- Mapping to FROZEN `LegalAnalysisArtifactsReady` contract
- Parallelism strategy (errgroup), retry/repair logic, prompt injection protection

**llm-provider-abstraction.md** — `LLMProviderPort`:
- Go interface contract
- Adapters for Claude (default), OpenAI, Gemini
- Provider Router (per-agent default + global fallback)
- Rate limiting via Redis token bucket
- Cost & usage tracking via Prometheus
- Caching policy (system prompt only)
- Secret management
- Data residency considerations (152-ФЗ)

**integration-contracts.md** — Subscriptions and publications:
- 6 incoming topics with FROZEN contract references
- 4 outgoing topics + 4 DLQ topics
- No sync REST integrations (sync API only for `/healthz`, `/readyz`, `/metrics`)
- Envelope, correlation fields, versioning

**event-catalog.md** — JSON schemas for LIC-owned events:
- `LICStatusChangedEvent`, `ClassificationUncertain` (FROZEN by Orchestrator, served by LIC)
- DLQ envelopes
- Reused FROZEN DM/Orchestrator contracts via references (no duplication)

**sequence-diagrams.md** — 14 Mermaid diagrams covering all scenarios from high-architecture §8.

**security.md** — Multi-tenancy, API key protection, prompt injection (5-layer defense), PII redaction in logs, data residency, TLS, audit, abuse protection.

**error-handling.md** — Error codes catalog (RU user messages + EN dev messages), retry policy (5 levels), repair loop, provider fallback, timeout policy hierarchy, graceful degradation (critical / tier-2 / non-critical agents), DLQ strategy, health checks, graceful shutdown.

**observability.md** — Structured logging (allowlist for PII), Prometheus metrics (pipeline / agent / LLM / DM / idempotency / pending / DLQ), OpenTelemetry tracing with W3C Trace Context propagation, dashboards, alerts, runbooks.

**configuration.md** — All `LIC_*` env variables with defaults, per-agent provider/timeout overrides, aggregate score weights, OTel settings, sample `.env`.

**deployment.md** — Multi-stage Dockerfile (distroless `nonroot` base), Docker Compose for local dev, Kubernetes Deployment with readiness/liveness probes, HPA, PDB, secret management via External Secrets Operator + Yandex Lockbox.

## Key Design Decisions

- **Stateless service** — no own database; Redis only for idempotency, pending state, rate limit. All persistent data — in DM (ADR-LIC-02).
- **In-process pipeline orchestration** — `errgroup` for parallel stages; long pause (up to 24h) handled via Redis state + event-driven resume, not durable workflow engine (ADR-LIC-01).
- **Per-agent LLM provider with global fallback** — primary provider per agent (default: Claude), fallback chain for resilience (ADR-LIC-03).
- **JSON Schema validation + 1-shot repair loop** — invalid LLM output → one retry with repair prompt; on second failure → `AGENT_OUTPUT_INVALID`, `is_retryable=true` (ADR-LIC-04).
- **`RISK_DELTA` as schema extension v1.1** — backward-compatible; DM v1.0 ignores unknown field, DM v1.1+ persists as new artifact_type (ADR-LIC-05).
- **24h TTL for user type confirmation** — coordinated with Orchestrator watchdog; LIC pending state in Redis with 25h TTL as safety net (ADR-LIC-06).
- **5-layer prompt injection defense** — system prompt instruction + XML envelope + JSON-only response + `prompt_injection_detected` flag + warning in `DETAILED_REPORT.warnings` (ADR-LIC-07).

## YAGNI Boundary

- **OPM and LKB are not implemented in v1** and are not even mentioned in this architecture (no extension points, no «out of scope» sections, no Open Questions). When/if they appear, that's a separate redesign.
- **No RAG, no fine-tuned models, no embeddings, no on-premise LLM** in v1. System prompts contain embedded knowledge of the Russian Civil Code.
- **No per-tenant LLM provider override** in v1 — `LIC_AGENT_*_PROVIDER` is per-deployment. Per-tenant — v2.
- **No streaming, no tool use, no multimodal** for LLM calls in v1.

## Dependencies

- **DM** (async via RabbitMQ) — primary integration: subscribes to `version-artifacts-ready`, requests artifacts, publishes `analysis-ready`.
- **Orchestrator** (async via RabbitMQ) — receives `UserConfirmedType` command; publishes `classification-uncertain` and `status-changed` events.
- **LLM providers** (HTTPS): Anthropic Claude (default), OpenAI, Google Gemini — pluggable.
- **Redis** — idempotency, pending type confirmation state, rate limiting tokens, version meta cache.
- **RabbitMQ** — all inter-domain communication.
- **OpenTelemetry collector**, **Prometheus** — observability.

All inter-domain communication is async via RabbitMQ. **No sync REST to DM, OPM, LKB, UOM.** LIC does not authenticate users (it trusts `organization_id` from event envelope, validated upstream by Orchestrator/DM).

All documentation is in Russian. Code identifiers, event names, statuses, JSON field names — in English.
