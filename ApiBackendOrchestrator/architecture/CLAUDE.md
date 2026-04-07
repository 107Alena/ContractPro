# API/Backend Orchestrator Architecture — CLAUDE.md

Russian-language design documentation for the API/Backend Orchestrator — the single entry point for frontend and external integrations of ContractPro.

## What the Orchestrator Does

The Orchestrator is a **coordinating layer** (not a domain service) that:
- Accepts file uploads, validates, uploads to S3, creates documents/versions in DM
- Publishes processing/comparison commands to DP via RabbitMQ
- Subscribes to DP/DM events for real-time status tracking
- Delivers status updates to frontend via SSE (Server-Sent Events)
- Aggregates data from DM into user-friendly responses
- Handles JWT authentication, RBAC authorization, rate limiting
- Proxies admin operations to OPM, auth to UOM

## Architecture Files

**high-architecture.md** — Foundational document (v1):
- Requirements mapping (UR/FR/NFR)
- 12 architectural assumptions (ASSUMPTION-ORCH-01..12)
- Component boundaries vs DM, DP, OPM, UOM, LIC, RE
- Data model (Redis ephemeral state, status mapping)
- 20 internal components across ingress/application/egress/infrastructure layers
- 10 user scenarios with happy path + error branches
- 5 ADRs (monolith, REST, SSE, upload proxy, JWT)

**api-specification.yaml** — OpenAPI 3.0 spec:
- All frontend-facing REST endpoints
- Upload (multipart), CRUD contracts, results, comparison, export, feedback, admin
- SSE endpoint for real-time events
- Auth proxy (login, refresh, logout)
- Error format with Russian messages (NFR-5.2)

**integration-contracts.md** — Integration contracts:
- Sync calls to DM, OPM, UOM REST APIs
- Async commands published to DP (2 topics)
- Async events subscribed from DP (5) and DM (5)
- Topic naming, envelope (EventMeta), correlation fields

**event-catalog.md** — Event catalog:
- Full JSON schemas for all 12 events (2 published, 10 received)

**sequence-diagrams.md** — Mermaid sequence diagrams:
- 12 diagrams covering all scenarios including end-to-end flow

**security.md** — Security:
- JWT auth, RBAC, multi-tenancy, rate limiting, CORS, file validation, TLS, audit

**error-handling.md** — Error handling and resilience:
- Retry strategies, circuit breaker, timeouts, graceful degradation, error codes, health checks

**observability.md** — Observability:
- Structured logging, Prometheus metrics, OpenTelemetry tracing, alerts, dashboards

**configuration.md** — Configuration reference:
- All `ORCH_*` environment variables (required and optional)
- Broker topics, timeouts, limits, .env example

**deployment.md** — Deployment:
- Docker multi-stage build, Docker Compose (dev + prod), health checks, graceful shutdown

## Key Design Decisions

- **Monolith service** — single Go binary handling HTTP + SSE + RabbitMQ consumer
- **SSE** for real-time status (not WebSocket) — simpler, unidirectional, auto-reconnect
- **Redis Pub/Sub** for SSE horizontal scaling across instances
- **Upload proxy** through orchestrator (not direct-to-S3) — server-side validation
- **JWT stateless auth** — local validation by public key, no UOM dependency per request
- **No PostgreSQL** — orchestrator is stateless, uses Redis for ephemeral state only

## Dependencies

- **DM** (sync REST) — primary dependency, CRUD + artifact reads
- **DP** (async RabbitMQ) — processing/comparison commands + status events
- **OPM** (sync REST) — admin policy proxy (optional)
- **UOM** (sync REST) — auth proxy (login/refresh)
- **Object Storage** (S3) — file upload
- **Redis** — SSE pub/sub, rate limiting, upload tracking
- **RabbitMQ** — event consumption + command publishing

All documentation is in Russian. Code identifiers and event names are in English.
