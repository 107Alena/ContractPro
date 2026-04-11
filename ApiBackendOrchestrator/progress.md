# Progress Log — API/Backend Orchestrator

Лог прогресса выполнения задач из tasks.json.

---

<!-- Агенты добавляют записи в формате: -->
<!-- ## ORCH-TASK-XXX: <описание> -->
<!-- **Дата:** YYYY-MM-DD -->
<!-- **Статус:** done -->
<!-- **Summary:** что было сделано, ключевые решения, результаты тестов -->

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
