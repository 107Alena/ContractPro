# Ingress Layer — CLAUDE.md

Inbound message and HTTP request handling: subscribe to events, deserialize, deduplicate, rate limit, dispatch to application services.

## consumer/ — Event Consumer

RabbitMQ subscriber for 7 incoming topics from DP, LIC, and RE domains.

- **consumer.go** — EventConsumer: subscribes to all 7 topics via broker.Subscribe. Per-topic handlers deserialize JSON → model structs, validate required fields (correlation_id, timestamp, job_id, document_id), check schema_version (REV-031: unknown version → WARN, continue). processWithIdempotency wrapper: Check → Skip (with optional re-publish) / Process / Reprocess → handler → snapshot build → MarkCompleted/Cleanup. On Skip with stored snapshot (DM-TASK-058): decode envelope and re-publish stored confirmation via broker.Publish + IncRepublishedConfirmations metric — closes producer-crash-in-acknowledgment-window race for the 4 producer→DM confirmation flows (DP/LIC/RE artifacts + DP diff). Invalid JSON/missing fields → DLQ (dm.dlq.invalid-message). Non-retryable handler errors → DLQ (dm.dlq.ingestion-failed). Retryable errors → backoff delay (BRE-025) then return error for NACK. Always returns nil to prevent poison-pill requeue. Panic recovery with debug.Stack()

Topic→Handler routing:
- dp.artifacts.processing-ready → ingestion.HandleDPArtifacts
- dp.requests.semantic-tree → query.HandleGetSemanticTree
- dp.artifacts.diff-ready → diff.HandleDiffReady
- lic.artifacts.analysis-ready → ingestion.HandleLICArtifacts
- lic.requests.artifacts → query.HandleGetArtifacts
- re.artifacts.reports-ready → ingestion.HandleREArtifacts
- re.requests.artifacts → query.HandleGetArtifacts

Constructor: `NewEventConsumer(subscriber, publisher, idem, logger, metrics, dlq, ingestionHandler, queryHandler, diffHandler, artifactRepo, diffRepo, topicCfg, retryCfg)`. `subscriber` and `publisher` are the two halves of the broker client (broker.Client implements both). Start() begins subscriptions.

## idempotency/ — Idempotency Guard

Redis-based event deduplication with DB fallback.

- **idempotency.go** — IdempotencyGuard: Check(ctx, key, topic, fallback) → `CheckResult{Status, StoredSnapshot}`. CheckStatus enum: ResultProcess/ResultSkip/ResultReprocess. Atomic SETNX claim with ProcessingTTL (120s). COMPLETED → Skip (StoredSnapshot=*string when record carries a non-empty snapshot — DM-TASK-058). Fresh PROCESSING → Skip. Stuck PROCESSING (≥240s) → overwrite + Reprocess. Redis failure → DB fallback via FallbackChecker. MarkCompleted(ctx, key, snapshot) persists optional confirmation envelope alongside the COMPLETED record (TTL 24h). Cleanup(delete PROCESSING key on non-retryable error)
- **snapshot.go** — ConfirmationSnapshot envelope `{schema_version, topic, payload}`. EncodeConfirmationSnapshot validates topic/event + enforces 64 KiB size cap. DecodeConfirmationSnapshot is forward-compatible: unknown schema_version still returns the parsed envelope (caller logs WARN, re-publishes best-effort)
- **keys.go** — 7 key generators: KeyForDPArtifacts, KeyForSemanticTreeRequest, KeyForDiffReady, KeyForLICArtifacts, KeyForLICRequest, KeyForREArtifacts, KeyForRERequest. Format: dm:idem:{topic-short}:{job_id}[:{version_id}]
- **fallback.go** — ArtifactFallback (checks artifact_descriptors by producer + job_id), DiffFallback (checks diff by version pair). Used when Redis is unavailable

Constructor: `NewIdempotencyGuard(store, cfg, metrics, logger)`.

## api/ — HTTP REST API

Full REST API with auth, rate limiting, and middleware.

- **handler.go** — Handler: 13 endpoints via Go 1.22+ method-aware routing. Document CRUD (POST/GET /documents, GET/DELETE /documents/{id}, POST /documents/{id}/archive), Version management (POST/GET /documents/{id}/versions, GET /documents/{id}/versions/{id}), Artifact access (GET /artifacts list, GET /artifacts/{type} single — JSON content for metadata types, 302 presigned URL for blob types including SOURCE_FILE pseudo-type), Diff retrieval (GET /diffs/{base}/{target}), Audit (GET /audit, requires admin/auditor role), Admin (POST /admin/dlq/replay, requires admin role). MaxBytesReader 1MiB on POST bodies. Soft-deleted documents: 404 for non-admin, 200 for admin (BRE-024)
- **auth.go** — AuthContext (OrganizationID, UserID, Role). authMiddleware extracts from X-Organization-ID, X-User-ID headers with regex validation (^[a-zA-Z0-9._-]{1,128}$). requireRole(allowedRoles...) middleware for role-based access
- **middleware.go** — metricsMiddleware (dm_api_requests_total, dm_api_request_duration_seconds), loggingMiddleware (method, path, status, duration_ms). Shared responseWriter with WriteHeader guard, Flush/Unwrap support
- **response.go** — ErrorResponse, PaginatedResponse. writeJSON (X-Content-Type-Options: nosniff), writeServiceError (DomainError → HTTP: NotFound→404, Conflict→409, Validation→400, TenantMismatch→404 hidden, Retryable→500 generic)
- **ratelimit.go** — OrgRateLimiter: per-organization token bucket (golang.org/x/time/rate). Separate read (100 RPS) and write (20 RPS) budgets. HTTP 429 + Retry-After header. Background GC for idle org entries. Close() with sync.Once

Middleware stack: logging → metrics → auth → rateLimit → handler.

Constructor: `NewHandler(lifecycle, versions, queries, diffs, audit, storage, logger)`. Optional: WithDLQReplay(), WithRateLimit().

## Message Flow

```
RabbitMQ → EventConsumer (deserialize + validate + schema check)
         → IdempotencyGuard (dedup: Redis check + DB fallback)
         → Application Service (ingestion/query/diff)
         → IdempotencyGuard (MarkCompleted / Cleanup)

HTTP → authMiddleware → rateLimitMiddleware → Handler → Application Service → JSON Response
```

## Patterns

- Consumer always returns nil (poison-pill prevention), routes errors to DLQ
- Idempotency: SETNX atomic claim, short TTL for PROCESSING (120s), long TTL for COMPLETED (24h)
- Per-topic idempotency key generators with fallback checkers
- Auth context propagated via request context
- Rate limiter: per-org isolation, read/write split, background cleanup
