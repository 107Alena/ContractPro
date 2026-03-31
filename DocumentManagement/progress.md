# Document Management — Progress Log

Лог прогресса реализации доменной области Document Management. Каждый агент записывает summary после завершения задачи.

---

## DM-TASK-001: Инициализация Go-модуля и структуры проекта (2026-03-31)

**Статус:** done

**Что сделано:**
- Создан `go.mod` с модулем `contractpro/document-management`, Go 1.26.1
- Создана полная структура каталогов (21 пакет):
  - `cmd/dm-service/` — точка входа
  - `internal/config/` — конфигурация
  - `internal/domain/model/`, `internal/domain/port/` — доменный слой
  - `internal/application/ingestion/`, `query/`, `lifecycle/`, `version/`, `diff/` — application layer
  - `internal/ingress/consumer/`, `api/`, `idempotency/` — ingress layer
  - `internal/egress/confirmation/`, `notification/`, `outbox/` — egress layer
  - `internal/infra/postgres/`, `objectstorage/`, `broker/`, `kvstore/`, `observability/`, `health/` — infra layer
- Создан `cmd/dm-service/main.go` с минимальным `run()` паттерном
- Создан `Makefile` с целями `build`, `test`, `lint`, `docker-build`
- Создан `.gitignore` (бинарники, .env, IDE файлы)
- Placeholder `.go` файлы с package declaration в каждом пакете

**Проверки:**
- `go mod tidy` — OK
- `make build` — OK
- `make test` — 21 пакет, 0 тестов, без ошибок
- `make lint` (`go vet ./...`) — OK
- `go test -count=1 ./...` — OK

**Следующие задачи:**
- DM-TASK-002 (доменные модели) и DM-TASK-005 (конфигурация) — оба зависят от DM-TASK-001
- DM-TASK-005 разблокирует инфраструктурные задачи (006-009)
- DM-TASK-002 → DM-TASK-003 → DM-TASK-004 — цепочка доменного слоя

---

## DM-TASK-002: Доменные модели (2026-04-01)

**Статус:** done

**Что сделано:**
- Удалён placeholder `model.go`
- Создано 6 файлов моделей в `internal/domain/model/`:
  - `document.go` — `Document` struct + `DocumentStatus` enum (ACTIVE, ARCHIVED, DELETED) + state machine (`validDocumentTransitions` map, `ValidateDocumentTransition()`, `TransitionTo()`, `IsTerminal()`)
  - `version.go` — `DocumentVersion` struct + `OriginType` enum (5 значений) + `ArtifactStatus` enum (6 значений) + state machine (`allowedArtifactTransitions` map, `ValidateArtifactTransition()`, `TransitionArtifactStatus()`, `IsTerminal()`)
  - `artifact.go` — `ArtifactDescriptor` struct + `ArtifactType` enum (15 типов: 5 DP + 8 LIC + 2 RE) + `ProducerDomain` enum (DP, LIC, RE) + `ArtifactTypesByProducer` map + `IsBlobArtifact()`
  - `diff.go` — `VersionDiffReference` struct
  - `audit.go` — `AuditRecord` struct + `AuditAction` enum (9 значений) + `ActorType` enum (USER, SYSTEM, DOMAIN) + builder chain (WithDocument/WithVersion/WithJob/WithDetails)
  - `idempotency.go` — `IdempotencyRecord` struct + `IdempotencyStatus` enum (PROCESSING, COMPLETED) + `MarkCompleted()` + `IsStuck()`
- Создано 6 файлов тестов (32 теста):
  - JSON round-trip для каждой сущности
  - omitempty проверки (optional fields не включаются в JSON)
  - Полная валидация state machine ArtifactStatus (21 тест-кейс переходов)
  - Полная валидация state machine DocumentStatus (6 переходов)
  - Проверка IsTerminal для всех статусов
  - Проверка ArtifactTypesByProducer completeness
  - Проверка IsBlobArtifact
  - Проверка builder chain для AuditRecord
  - Проверка IsStuck для IdempotencyRecord

**Проверки:**
- `go test ./internal/domain/model/... -race -count=1` — 32 PASS
- `go test -count=1 ./...` — OK
- `go vet ./...` — OK
- `make build` — OK
- `make test` — OK
- `make lint` — OK

**Паттерны:**
- Конструкторы `NewTypeName()` (как в DP)
- Type alias enums (`type DocumentStatus string`)
- State machines: `map[Status][]Status` + `Validate*Transition()` + метод `TransitionTo()`
- JSON tags с `omitempty` для optional/nullable полей
- `json.RawMessage` для JSONB поля `Details` в AuditRecord

**Следующие задачи:**
- DM-TASK-003 (доменные события) — зависит от DM-TASK-002 ✅
- DM-TASK-005 (конфигурация) — зависит от DM-TASK-001 ✅ (параллельная ветка)

---

## DM-TASK-003: Доменные события (2026-04-01)

**Статус:** done

**Что сделано:**
- Создано 5 файлов в `internal/domain/model/`:
  - `event.go` — `EventMeta` (correlation_id + timestamp, совместим с DP), `BlobReference` (claim-check для RE exports: storage_key, file_name, size_bytes, content_hash)
  - `event_incoming.go` — 6 входящих событий:
    - `DocumentProcessingArtifactsReady` — от DP, артефакты как `json.RawMessage` (ocr_raw, text, structure, semantic_tree, warnings)
    - `GetSemanticTreeRequest` — от DP, запрос дерева для comparison pipeline
    - `DocumentVersionDiffReady` — от DP, diff результат (text_diffs, structural_diffs как `json.RawMessage`)
    - `GetArtifactsRequest` — от LIC/RE, запрос артефактов по типам (`[]ArtifactType`)
    - `LegalAnalysisArtifactsReady` — от LIC, 8 артефактов как `json.RawMessage`
    - `ReportsArtifactsReady` — от RE, claim-check через `*BlobReference` (export_pdf, export_docx)
  - `event_outgoing.go` — 14 исходящих событий:
    - 10 confirmations: Persisted/PersistFailed для DP/LIC/RE + SemanticTreeProvided + ArtifactsProvided + Diff Persisted/Failed
    - 4 notifications: VersionProcessingArtifactsReady, VersionAnalysisArtifactsReady, VersionReportsReady, VersionCreated
  - `topic.go` — 25 topic constants (7 incoming + 10 confirmation + 5 notification + 3 DLQ)
  - `dlq.go` — `DLQRecord` (diagnostic envelope, НЕ embed EventMeta)
- Создано 5 файлов тестов (44 новых теста):
  - JSON round-trip для каждого типа события
  - Backward compatibility: unknown JSON fields игнорируются
  - Optional fields проверка omitempty (organization_id, warnings, error_code, parent_version_id)
  - Raw message preservation: json.RawMessage контент не модифицируется при round-trip
  - ArtifactType сериализуется как string
  - Topic naming convention: все DM topics начинаются с "dm."

**Проверки:**
- `go test ./internal/domain/model/... -race -count=1` — 76 PASS (32 старых + 44 новых)
- `go test -count=1 ./...` — OK
- `go vet ./...` — OK
- `make build` — OK
- `make test` — OK
- `make lint` — OK

**Ключевые решения:**
- Artifact content как `json.RawMessage`: DM — storage domain, не интерпретирует содержимое артефактов. Избегаем coupling к внутренним schema DP/LIC/RE
- `BlobReference` value object для claim-check pattern RE exports (pointer для optional: `*BlobReference`)
- Нет конструкторов для event structs (паттерн DP — struct literals)
- Не embed'ить общий base type для confirmation events — каждый event отдельный struct (evolvability)
- `DLQRecord` НЕ embed'ит `EventMeta` — собственная schema (original_message + diagnostic fields)
- `ArtifactsProvided` использует `map[ArtifactType]json.RawMessage` для artifacts

**Следующие задачи:**
- DM-TASK-004 (hexagonal ports) — зависит от DM-TASK-003 ✅
- DM-TASK-005 (конфигурация) — зависит от DM-TASK-001 ✅ (параллельная ветка)
- DM-TASK-004 разблокирует: DM-TASK-012, 013, 017-021

---
