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

## DM-TASK-004: Hexagonal порты (2026-04-01)

**Статус:** done

**Что сделано:**
- Удалён placeholder `port.go`
- Создано 3 файла в `internal/domain/port/`:
  - `errors.go` — `DomainError` struct (Code, Message, Retryable, Cause) + 16 error code constants + 17 конструкторов + 6 helpers (IsDomainError, IsRetryable, ErrorCode, IsNotFound, IsConflict, IsDuplicateEvent)
  - `inbound.go` — 5 inbound handler interfaces:
    - `DocumentLifecycleHandler` (5 методов: CreateDocument, GetDocument, ListDocuments, ArchiveDocument, DeleteDocument)
    - `VersionManagementHandler` (3 метода: CreateVersion, GetVersion, ListVersions)
    - `ArtifactIngestionHandler` (3 метода: HandleDPArtifacts, HandleLICArtifacts, HandleREArtifacts)
    - `ArtifactQueryHandler` (4 метода: HandleGetSemanticTree, HandleGetArtifacts, GetArtifact, ListArtifacts)
    - `DiffStorageHandler` (2 метода: HandleDiffReady, GetDiff)
  - `outbound.go` — 12 outbound port interfaces:
    - `Transactor` — unit-of-work для DB-транзакций
    - 6 repositories: `DocumentRepository`, `VersionRepository`, `ArtifactRepository`, `DiffRepository`, `AuditRepository`, `OutboxRepository`
    - `ObjectStoragePort` (6 методов: PutObject, GetObject, DeleteObject, HeadObject, GeneratePresignedURL, DeleteByPrefix)
    - `BrokerPublisherPort` (Publish)
    - `IdempotencyStorePort` (Get, Set, Delete)
    - `AuditPort` (Record, List)
    - `DLQPort` (SendToDLQ)
- Вспомогательные типы: `PageResult[T]`, `CreateDocumentParams`, `ListDocumentsParams`, `CreateVersionParams`, `ListVersionsParams`, `GetArtifactParams`, `ArtifactContent`, `GetDiffParams`, `AuditListParams`, `OutboxEntry`

**Проверки:**
- `go build ./internal/domain/...` — OK
- `go vet ./...` — OK
- `go test -count=1 ./...` — 76 PASS (model тесты), port без тестов (interface-only)
- `make build` — OK
- `make test` — OK
- `make lint` — OK

**Ключевые решения:**
- Handler suffix для inbound (как DP: ProcessingCommandHandler, DMResponseHandler), Port/Repository suffix для outbound
- MetadataStorePort из acceptance criteria декомпозирован на 6 per-aggregate repositories (Interface Segregation Principle)
- OutboxEntry содержит AggregateID для FIFO ordering (REV-010)
- HeadObject возвращает `(sizeBytes, exists bool, err)` вместо error на not-found
- IsDuplicateEvent выделен из IsConflict (разная семантика: idempotency vs conflict)
- PageResult без JSON tags (domain layer serialization-agnostic)
- Compile-time interface checks (`var _ Port = (*Impl)(nil)`) будут в файлах адаптеров

**Ревью (code-reviewer + golang-pro):**
- Исправлено: удалён дублирующий FindByDocumentAndVersion, добавлен AggregateID/Status/PublishedAt в OutboxEntry, DeletePublished в OutboxRepository, consistent params в ArtifactRepository, улучшены doc comments

**Следующие задачи:**
- DM-TASK-005 (конфигурация) — зависит от DM-TASK-001 ✅ (параллельная ветка)
- DM-TASK-012 (PostgreSQL repositories) — зависит от DM-TASK-004 ✅ + DM-TASK-006
- DM-TASK-013 (Idempotency Guard) — зависит от DM-TASK-004 ✅ + DM-TASK-009
- DM-TASK-017-021 (application services) — зависят от DM-TASK-004 ✅ + infra tasks

---

## DM-TASK-005: Конфигурация сервиса (2026-04-01)

**Статус:** done

**Что сделано:**
- Удалён placeholder `config.go` (только `package config`)
- Создано 3 файла в `internal/config/`:
  - `config.go` — `Config` struct (12 nested sub-configs), `Load()`, `Validate()`, 4 env-хелпера (envString, envInt, envDuration, envBool)
  - `sub_configs.go` — 12 sub-config типов с `load*Config()` функциями
  - `config_test.go` — 20 test functions (96+ subtests)
- Добавлена зависимость `github.com/joho/godotenv v1.5.1` в `go.mod`

**Sub-config типы (12):**
- `DatabaseConfig` — DM_DB_DSN (required), MaxConns(25), MinConns(5), QueryTimeout(10s)
- `BrokerConfig` — DM_BROKER_ADDRESS (required), TLS(false), 25 configurable topic names
- `StorageConfig` — 4 required (Endpoint, Bucket, AccessKey, SecretKey), Region("ru-central1"), PresignedURLTTL(5m)
- `KVStoreConfig` — DM_KVSTORE_ADDRESS (required), Password(""), DB(0), PoolSize(10), Timeout(2s)
- `HTTPConfig` — Port(8080)
- `ConsumerConfig` — Prefetch(10), Concurrency(5)
- `IdempotencyConfig` — TTL(24h), ProcessingTTL(120s), StuckThreshold(240s)
- `OutboxConfig` — PollInterval(200ms), BatchSize(50), LockTimeout(5s), CleanupHours(48)
- `RetentionConfig` — ArchiveDays(90), DeletedBlobDays(30), DeletedMetaDays(365), AuditDays(1095)
- `RetryConfig` — MaxAttempts(3), BackoffBase(1s)
- `ObservabilityConfig` — LogLevel("info"), MetricsPort(9090), TracingEnabled(false), TracingEndpoint("")
- `TimeoutConfig` — StoragePut(30s), StorageGet(15s), EventProcessing(60s), BrokerPublish(10s), StaleVersion(30m), Shutdown(30s)

**Required env vars (7):** DM_DB_DSN, DM_BROKER_ADDRESS, DM_STORAGE_ENDPOINT, DM_STORAGE_BUCKET, DM_STORAGE_ACCESS_KEY, DM_STORAGE_SECRET_KEY, DM_KVSTORE_ADDRESS

**Проверки:**
- `go test ./internal/config/... -race -count=1` — 20 PASS (96+ subtests)
- `go test -count=1 ./...` — OK (config 20 + model 76 = 96 tests)
- `go vet ./...` — OK
- `make build` — OK
- `make test` — OK
- `make lint` — OK

**Ключевые решения:**
- Validate() разделяет `missing` (required fields) и `invalid` (constraint violations) в отдельные слайсы
- Port collision: DM_HTTP_PORT != DM_METRICS_PORT
- DM_HTTP_PORT optional с default 8080 (не required)
- Topic defaults в тестах cross-verified против model.Topic* constants (import model)
- envInt64 удалён как dead code (по результатам ревью)
- TestLoad_MissingMultipleRequiredFields явно очищает env vars через t.Setenv("", "")

**Ревью (code-reviewer + golang-pro):**
- Исправлено: удалён envInt64 (dead code), разделены missing/invalid в Validate(), topic тесты используют model constants, TestLoad_MissingMultipleRequiredFields защищён от ambient env

**Следующие задачи:**
- DM-TASK-006 (PostgreSQL клиент) — зависит от DM-TASK-005 ✅
- DM-TASK-007 (RabbitMQ клиент) — зависит от DM-TASK-005 ✅
- DM-TASK-008 (Object Storage) — зависит от DM-TASK-005 ✅
- DM-TASK-009 (Redis клиент) — зависит от DM-TASK-005 ✅
- DM-TASK-010 (Observability) — зависит от DM-TASK-005 ✅

---
