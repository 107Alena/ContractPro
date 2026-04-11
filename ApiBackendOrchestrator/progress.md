# Progress Log — API/Backend Orchestrator

Лог прогресса выполнения задач из tasks.json.

---

<!-- Агенты добавляют записи в формате: -->
<!-- ## ORCH-TASK-XXX: <описание> -->
<!-- **Дата:** YYYY-MM-DD -->
<!-- **Статус:** done -->
<!-- **Summary:** что было сделано, ключевые решения, результаты тестов -->

## ORCH-TASK-033: OpenTelemetry tracing — spans, propagation, context
**Дата:** 2026-04-11
**Статус:** done

### План реализации
1. Изучить архитектурную документацию по tracing (observability.md §3) и существующую кодовую базу (subagent: Explore)
2. Спроектировать архитектуру пакета (subagent: code-architect) — файловая структура, API, интеграция, noop strategy
3. Создать пакет internal/infra/observability/tracing:
   - tracing.go — Tracer struct, NewTracer, OTLP/HTTP exporter, noop fallback
   - sampler.go — ParentBased + TraceIDRatioBased(0.10) custom sampler
   - middleware.go — chi HTTP middleware (orch.http.request parent span)
   - propagation.go — W3C traceparent injection/extraction, correlation_id
   - attributes.go — 10 span names, 15 attribute keys, 4 builder functions
4. Написать тесты (49 тестов: tracing 19, sampler 4, middleware 16, propagation 5, attributes 7)
5. Интегрировать в app.go (NewTracer step 1c, compositeObservability, TracingMiddleware) и server.go (middleware chain)
6. Code review (subagent: code-reviewer) → исправления (M-5, M-6, M-7, N-3, N-4)
7. Полный прогон go test -race -count=1 ./... + go vet + Makefile targets

### Summary
- **tracing.go**: Tracer struct с OTel TracerProvider, noop.TracerProvider при отключении (zero-cost). NewTracer: OTLP/HTTP exporter при TracingEnabled+TracingEndpoint, W3C TraceContext+Baggage global propagator. Shutdown: sync.Once идемпотентный (M-6 code review fix). Methods: StartSpan, SpanFromContext, Shutdown, Enabled, Provider.
- **sampler.go**: NewOrchSampler() — ParentBased(TraceIDRatioBased(0.10)). Head-based 10% sampling в SDK, tail-based (100% errors, 100% slow >2s) делегируется collector (Jaeger/Tempo).
- **middleware.go**: HTTPMiddleware(t *Tracer) — chi-compatible, responseCapture wrapper (WriteHeader idempotent, Flush delegation для SSE, Unwrap для ResponseController). Span: SpanKindServer, http.method/http.route/http.status_code + orch.correlation_id/organization_id/user_id. 5xx → codes.Error.
- **propagation.go**: InjectHTTPHeaders (W3C traceparent для DM sync calls), ExtractHTTPHeaders, CorrelationAttribute (для async event linking).
- **attributes.go**: 10 span name констант, 15 attribute key констант, HTTPRequestAttrs/DMRequestAttrs/BrokerPublishAttrs/S3UploadAttrs.
- **Wiring**: app.go — NewTracer (step 1c, before Redis), compositeObservability{tracer, metrics} для shutdown. server.go — TracingMiddleware в Deps, chain: HTTPMetrics → Recovery → CORS → SecurityHeaders → **Tracing** → Auth → RBAC → RateLimit.
- **Ключевое решение**: OTLP/HTTP (не gRPC) — меньше зависимостей, совместимость с DP domain. Noop tracer через OTel noop package — zero allocations, compiler inlining. Head-based sampling в SDK, tail-based в collector.
- **Тесты**: 49 тестов. tracing (19): noop/enabled/shutdown/StartSpan/SpanFromContext/Enabled/Provider. sampler (4): constructor/description/ParentBased/TraceIDRatio. middleware (16): passthrough/span/attributes/correlation/auth/errors/default200/chiPattern/SpanKind/propagation/concurrent. propagation (5): noop/traceparent/extract/correlation. attributes (7): HTTP/DM/broker/S3/span names/keys.
- **Результаты**: go test -race -count=1 ./... PASS (32 пакета, 0 failures), go vet clean, make build/test/lint OK.
- **Code review**: Approve — M-5 global state cleanup (applied), M-6 sync.Once Shutdown (applied), M-7 enabled double-shutdown test (added), N-3 unused helper removed, N-4 sampler test improved.
- **Зависимости**: go.opentelemetry.io/otel v1.43.0, otel/trace v1.43.0, otel/sdk v1.43.0, otlptracehttp v1.43.0.
- **ORCH-TASK-033 — последняя задача в проекте. Все 37 задач ApiBackendOrchestrator выполнены.**

---

## ORCH-TASK-026: Admin Proxy Service — проксирование политик и чек-листов в OPM
**Дата:** 2026-04-11
**Статус:** done

### План реализации
1. Изучить OPM client (opmclient), существующие handler-паттерны (authproxy, contracts)
2. Спроектировать handler по architecture/high-architecture.md — прозрачный прокси (ASSUMPTION-ORCH-04)
3. Создать пакет internal/application/adminproxy с handler.go
4. Создать тесты handler_test.go (40 тестов)
5. Обновить server.go (Deps), routes.go (замена stubs), app.go (DI wiring)
6. Code review (subagent: code-reviewer) → исправления (аудит-логирование, resource_hint в логах)
7. Полный прогон go test -race -count=1 ./... + go vet + Makefile targets

### Summary
- **handler.go**: Handler struct с OPMClient consumer-side интерфейсом (4 метода)
- **HandleListPolicies()**: defense-in-depth auth → OPM ListPolicies(orgID) → 200 json.RawMessage pass-through
- **HandleUpdatePolicy()**: auth → policyID validation → readBody (MaxBytesReader 1MB, json.Valid) → OPM UpdatePolicy → 200
- **HandleListChecklists()**: auth → OPM ListChecklists(orgID) → 200
- **HandleUpdateChecklist()**: auth → checklistID validation → readBody → OPM UpdateChecklist → 200
- **readBody**: MaxBytesReader 1MB DoS protection, empty body guard, json.Valid validation
- **handleOPMError**: context.Canceled/DeadlineExceeded → 502, ErrOPMDisabled → 502, OPMError HTTP → MapOPMError(resourceHint), transport → 502, unknown → 500
- **writeRawJSON**: nil data → `{}` fallback
- **RBAC**: adminOnly (ORG_ADMIN) — enforced by middleware, handler performs defense-in-depth auth check
- **Аудит**: Update handlers логируют organization_id + user_id для мутационных операций
- **Wiring**: Deps.AdminProxyHandler в server.go, routes.go с nil-guard, app.go: opmclient.NewOPMClient + adminproxy.NewHandler
- **Тесты**: 40 тестов: HandleListPolicies (7), HandleUpdatePolicy (11), HandleListChecklists (4), HandleUpdateChecklist (6), response format (2), handleOPMError (2), interface (1), constructor (1), readBody (5), writeRawJSON (3), concurrent (1), pass-through (2), wrapped errors (1), no-call guards (2), nil body (1)
- **Результаты**: go test -race -count=1 ./... PASS (32 пакета, 0 failures), go vet clean, make build/test/lint OK
- **Code review**: Approve — S1 аудит-логирование applied, N5 resource_hint в логах applied
- **Нет новых зависимостей**
- **Оставшаяся задача**: ORCH-TASK-033 (OpenTelemetry tracing)

---

## ORCH-TASK-025: Feedback Service — приём и сохранение обратной связи
**Дата:** 2026-04-11
**Статус:** done

### План реализации
1. Изучить существующие handler-паттерны (export, comparison, contracts)
2. Спроектировать handler по architecture/high-architecture.md §6.14, §8.8
3. Создать пакет internal/application/feedback с handler.go
4. Создать тесты handler_test.go (34 теста + 11 sub-tests)
5. Подключить в routes.go, server.go, app.go (DI wiring)
6. Code review (subagent: code-reviewer)
7. Полный прогон go test -race -count=1 ./... + go vet + Makefile targets

### Summary
- **handler.go**: Handler struct с DMClient (GetVersion) и KVStore (Set) consumer-side интерфейсами
- **HandleSubmit()**: auth → UUID validation → JSON parsing (DisallowUnknownFields, MaxBytesReader 1MB) → validate (is_useful *bool required, comment ≤2000 runes, trimmed) → DM GetVersion → Redis Set (TTL 30 дней) → 201 Created
- **feedbackRecord**: feedback_id, contract_id, version_id, organization_id, user_id, is_useful, comment, created_at (RFC3339)
- **Redis key**: `feedback:{org_id}:{version_id}:{feedback_id}`, TTL 30 дней (ASSUMPTION-ORCH-08)
- **Redis failure**: non-critical — WARN лог, 201 возвращается (fallback storage до поддержки USER_FEEDBACK артефакта в DM)
- **RBAC**: все роли (LAWYER, BUSINESS_USER, ORG_ADMIN) — per security.md matrix
- **DM error handling**: context.Canceled/DeadlineExceeded→502, ErrCircuitOpen→502, DMError→MapDMError(version), transport→502, unknown→500
- **Wiring**: Deps.FeedbackHandler в server.go, registerRoutes с nil-guard, app.go: feedback.NewHandler(dmClient, kvClient, log)
- **Тесты**: 34 теста (+ 11 sub-tests): happy path (3), auth (1), all roles (3), UUID (4), body validation (8), DM errors (6), Redis failure (1), data integrity (5), response format (1), feedbackKey (3), validate (8), interface (1), constructor (1), concurrent (1), no-call guards (2)
- **Результаты**: go test -race -count=1 ./... PASS (31 пакет, 0 failures), go vet clean, make build/test/lint OK
- **Нет новых зависимостей**
- **Оставшиеся задачи**: ORCH-TASK-026 (Admin Proxy), ORCH-TASK-033 (OpenTelemetry tracing)

---

## ORCH-TASK-001: Scaffolding проекта — Go-модуль, структура директорий, Makefile
**Дата:** 2026-04-08
**Статус:** done

### План реализации
1. Спроектировать структуру на основе sibling-проекта DocumentProcessing (subagent: code-architect)
2. Создать go.mod, cmd/orch-api/main.go (stub), Makefile, Dockerfile, .gitignore
3. Создать internal/ директории с .gitkeep
4. Code review (subagent: code-reviewer) → исправления
5. Проверить make build, make test, make lint

### Summary
- **go.mod**: module contractpro/api-orchestrator, Go 1.26.1
- **cmd/orch-api/main.go**: stub с log.Println
- **Makefile**: build, test, lint, docker-build (паттерн идентичен DP)
- **Dockerfile**: multi-stage (golang:1.26.1-alpine → alpine:3.21), non-root user `orchapi`, healthcheck
- **.gitignore**: orch-api binary, .env, coverage files
- **internal/**: config, domain/model, domain/port, application, app, ingress, egress, infra, integration
- **Результаты тестов**: make build ✓, make test ✓ (no test files), make lint ✓ (0 warnings)
- **Ключевое решение**: Dockerfile использует `COPY go.mod go.sum* ./` (glob) т.к. go.sum ещё не существует; будет заменено на точное имя при добавлении зависимостей
- **Следующая задача**: ORCH-TASK-002 (config) или другие critical tasks, зависящие от 001
