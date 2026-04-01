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

## DM-TASK-006: PostgreSQL клиент (2026-04-01)

**Статус:** done

**Что сделано:**
- Удалён placeholder `postgres.go` (только `package postgres`)
- Создано 4 файла Go + 2 SQL миграции в `internal/infra/postgres/`:
  - `client.go` — `Client` struct обёртка над `*pgxpool.Pool`, `NewPostgresClient(ctx, DatabaseConfig)`, `Ping()`, `Close()` (idempотентный через `sync.Mutex` + `chan struct{}`), `Pool()`, `String()`
  - `transactor.go` — `Transactor` реализует `port.Transactor` (compile-time check). `WithTransaction` с join semantics для вложенных вызовов, deferred rollback для panic safety
  - `context.go` — `DBTX` interface (Exec/Query/QueryRow), compile-time checks для `*pgxpool.Pool` и `pgx.Tx`. `InjectPool`/`ConnFromCtx` (panic при nil), `HasTx` для join semantics
  - `migrate.go` — `Migrator` с `embed.FS`, `iofs` source driver, `pgx5` database driver. `Up`/`Down`/`MigrateToVersion`/`Version`/`Close` (с `errors.Join`)
  - `migrations/000001_initial_schema.up.sql` — 7 таблиц, 12 индексов, все FK включая circular FK
  - `migrations/000001_initial_schema.down.sql` — drop FK + drop 7 таблиц в обратном порядке
- Добавлены зависимости: `pgx/v5 v5.7.4`, `golang-migrate/v4 v4.19.1`

**Таблицы в миграции (7):**
- `documents` — корневой агрегат с soft delete (status: ACTIVE/ARCHIVED/DELETED)
- `document_versions` — иммутабельные версии с artifact_status state machine, UNIQUE(document_id, version_number)
- `artifact_descriptors` — метаданные артефактов, UNIQUE(version_id, artifact_type)
- `version_diff_references` — метаданные diff, UNIQUE(base_version_id, target_version_id)
- `audit_records` — append-only аудит с JSONB details
- `outbox_events` — transactional outbox (PENDING/CONFIRMED)
- `orphan_candidates` — отслеживание orphan blobs (BRE-008)

**Индексы (12):**
- `idx_documents_org`, `idx_documents_deleted` (partial)
- `idx_versions_doc`, `idx_versions_org`
- `idx_artifacts_version`, `idx_artifacts_org`, `idx_artifacts_storage_key`
- `idx_audit_org_time`, `idx_audit_doc`, `idx_audit_version` (partial)
- `idx_outbox_pending` (partial), `idx_outbox_aggregate` (partial)

**Проверки:**
- `go build ./internal/infra/postgres/...` — OK
- `go build ./...` — OK
- `go test -count=1 ./...` — 96 PASS (config 20 + model 76), postgres без тестов (infra, требует DB)
- `go vet ./...` — OK
- `make build` — OK
- `make test` — OK
- `make lint` — OK

**Ключевые решения:**
- `DBTX` interface вместо concrete type — repositories работают и с pool, и с tx прозрачно
- Join semantics для вложенных WithTransaction (без savepoints)
- Deferred `tx.Rollback(context.Background())` для panic safety
- `ConnFromCtx` panic при nil (programming error, не runtime)
- Circular FK (documents ↔ document_versions) через ALTER TABLE после создания обеих таблиц
- Outbox без партиционирования (упрощение, партиционирование можно добавить в отдельной миграции)

**Ревью (code-reviewer + golang-pro):**
- Исправлено: rollback с `context.Background()` (не parent ctx который может быть cancelled), deferred rollback для panic safety, fnErr возвращается без маскировки error code (не double %w), ConnFromCtx panic вместо nil, Migrator.Close с `errors.Join`

**Следующие задачи:**
- DM-TASK-007 (RabbitMQ клиент) — зависит от DM-TASK-005 ✅
- DM-TASK-008 (Object Storage) — зависит от DM-TASK-005 ✅
- DM-TASK-009 (Redis клиент) — зависит от DM-TASK-005 ✅
- DM-TASK-012 (PostgreSQL repositories) — зависит от DM-TASK-004 ✅ + DM-TASK-006 ✅

---

## DM-TASK-012: PostgreSQL repositories (2026-04-01)

**Статус:** done

**Что сделано:**
- Создано 7 файлов реализации + 8 тест-файлов в `internal/infra/postgres/`:
  - `pg_error.go` — shared helpers: `isPgUniqueViolation`, `isPgFKViolation`, `nullableString`, `fromNullableString`
  - `document_repository.go` — `DocumentRepository` (5 методов: Insert, FindByID, List, Update, ExistsByID)
  - `version_repository.go` — `VersionRepository` (5 методов: Insert, FindByID, List, Update, NextVersionNumber)
  - `artifact_repository.go` — `ArtifactRepository` (5 методов: Insert, FindByVersionAndType, ListByVersion, ListByVersionAndTypes, DeleteByVersion)
  - `diff_repository.go` — `DiffRepository` (4 метода: Insert, FindByVersionPair, ListByDocument, DeleteByDocument)
  - `audit_repository.go` — `AuditRepository` (2 метода: Insert, List с dynamic WHERE builder)
  - `outbox_repository.go` — `OutboxRepository` (4 метода: Insert multi-row, FetchUnpublished FOR UPDATE SKIP LOCKED, MarkPublished, DeletePublished)
- 73 unit-теста с mock pgx.Tx (`mockTx`, `mockRow`, `mockRows`)

**Ключевые паттерны:**
- Stateless repo structs — `ConnFromCtx(ctx)` для каждого вызова
- Compile-time interface checks: `var _ port.XxxRepository = (*XxxRepository)(nil)` для всех 6 repos
- Tenant isolation: ВСЕ SQL-запросы содержат `WHERE organization_id` (кроме outbox — cross-tenant by design)
- Error mapping: `23505` → `AlreadyExists`, `23503` → `NotFound`/`DatabaseError`, `pgx.ErrNoRows` → `NotFound`, generic → `DatabaseError(retryable)`
- Pagination: `COUNT(*) OVER()` window function (single query)
- Nullable strings: `""` → SQL NULL через `nullableString()`, обратно через `fromNullableString()`
- Empty slices guarantee: all List operations return `[]*T{}` not nil
- `rows.Close()` via defer + `rows.Err()` check after every iteration
- Audit List: dynamic WHERE builder с `fmt.Sprintf("$%d", argIdx)` — safe from SQL injection
- Outbox Insert: multi-row INSERT via `strings.Builder` + positional params
- Outbox FetchUnpublished: `FOR UPDATE SKIP LOCKED` для concurrent pollers
- Outbox MarkPublished: `now()` DB-side timestamp
- NextVersionNumber: non-locking, relies on UNIQUE constraint as arbiter

**Проверки:**
- `go test ./internal/infra/postgres/... -race -count=1` — 73 PASS
- `go test -count=1 -race ./...` — 169 PASS (config 20 + model 76 + postgres 73)
- `go vet ./...` — OK
- `make build` — OK
- `make test` — OK
- `make lint` — OK

**Ревью (code-reviewer + golang-pro):**
- Исправлено: удалён dead code (`scanDocumentWithTotal`, `constraintName`)
- Исправлено: добавлен FK violation handling в `Document.Insert` и `Document.Update` (для `current_version_id`)
- Исправлено: добавлен `document_id` в Version `Update` WHERE (defense-in-depth)
- Исправлено: `MarkPublished` использует `now()` вместо `time.Now().UTC()` (DB-side timestamp)
- Исправлено: consistent `DeletedAt` scan через intermediate `*time.Time` variable
- Добавлено: SKIP LOCKED FIFO caveat comment, NextVersionNumber race documentation, outbox cross-tenant comment

**Следующие задачи:**
- DM-TASK-007 (RabbitMQ клиент) — зависит от DM-TASK-005 ✅
- DM-TASK-008 (Object Storage) — зависит от DM-TASK-005 ✅
- DM-TASK-009 (Redis клиент) — зависит от DM-TASK-005 ✅
- DM-TASK-013 (Idempotency Guard) — зависит от DM-TASK-004 ✅ + DM-TASK-009
- DM-TASK-019 (Document Lifecycle) — зависит от DM-TASK-004 ✅ + DM-TASK-012 ✅

---

## DM-TASK-007: RabbitMQ клиент (2026-04-01)

**Статус:** done

**Что сделано:**
- Удалён placeholder `broker.go` (только `package broker`)
- Создано 3 файла реализации + 1 тест-файл в `internal/infra/broker/`:
  - `client.go` — `Client` struct, `AMQPAPI`/`AMQPChannelAPI` интерфейсы, `amqpConnWrapper`/`amqpChanWrapper`, `NewClient` с TLS и publisher confirms, `Publish` (synchronous confirm + stale confirm drain), `Subscribe` (QoS prefetch), `DeclareTopology` (7 incoming + 3 DLQ quorum), `IsConnected`, `Close`
  - `errors.go` — `mapError` (AMQP→DomainError, nonRetryableAMQPCodes 404/403/406, context passthrough)
  - `reconnect.go` — `reconnectLoop` + `reconnectWithBackoff` (exponential backoff 1s-30s, 25% jitter, confirms re-enable, topology re-declare, re-subscribe)
  - `client_test.go` — 32 unit-теста с mock
- Добавлена зависимость `github.com/rabbitmq/amqp091-go v1.10.0` в `go.mod`

**Ключевые паттерны:**
- Publisher confirms: dedicated publish channel в confirm mode, `publishMu` serializes publish+confirm, stale confirm drain
- TLS: `amqp.DialTLS` с `MinVersion: tls.VersionTLS12` при `DM_BROKER_TLS=true`
- QoS: `channel.Qos(prefetch, 0, false)` на consumer channels
- Queue policies (BRE-026): `x-max-length=10000`, `x-overflow=reject-publish`, `x-message-ttl=24h`
- DLQ (REV-025): `x-queue-type=quorum`, `x-max-length=50000`, `x-message-ttl=7d`
- AMQP Table values: explicit `int32` для cross-client compatibility
- Dependency inversion: `AMQPAPI`/`AMQPChannelAPI` interfaces + wrapper types для тестирования
- Injectable `dialFn` + `newClientWithAMQP` test constructor
- Separate publish/consume channels (AMQP best practice)
- Compile-time check: `var _ port.BrokerPublisherPort = (*Client)(nil)`
- Default exchange (routing key = queue name), consistent с DP

**Проверки:**
- `go test ./internal/infra/broker/... -race -count=1` — 32 PASS
- `go test -count=1 -race ./...` — 201 PASS (config 20 + model 76 + broker 32 + postgres 73)
- `go vet ./...` — OK
- `make build` — OK
- `make test` — OK
- `make lint` — OK

**Ревью (code-reviewer + golang-pro):**
- Исправлено: stale confirm drain перед каждым publish (предотвращает чтение confirm от предыдущего timed-out publish)
- Исправлено: `int32` для AMQP Table значений (предотвращает 406 при cross-client декларациях)
- Исправлено: DLQ quorum queue version requirement comment (RabbitMQ >= 3.10 для x-message-ttl)
- Исправлено: `mockAcknowledger` в `TestSubscribe_Success` (предотвращает nil pointer deref)
- Добавлено: reconnect re-subscribe failures documentation comment

**Следующие задачи:**
- DM-TASK-008 (Object Storage) — зависит от DM-TASK-005 ✅ ← NEXT
- DM-TASK-009 (Redis клиент) — зависит от DM-TASK-005 ✅
- DM-TASK-013 (Idempotency Guard) — зависит от DM-TASK-004 ✅ + DM-TASK-009
- DM-TASK-015 (Confirmation Publisher) — зависит от DM-TASK-003 ✅ + DM-TASK-007 ✅
- DM-TASK-019 (Document Lifecycle) — зависит от DM-TASK-004 ✅ + DM-TASK-012 ✅

---

## DM-TASK-008: Object Storage адаптер (2026-04-01)

**Статус:** done

**Что сделано:**
- Удалён placeholder `objectstorage.go` (только `package objectstorage`)
- Создано 3 файла реализации + 2 тест-файла в `internal/infra/objectstorage/`:
  - `client.go` — `Client` struct, `S3API`/`PresignAPI` interfaces, `NewClient` (EndpointResolverFunc, path-style, static credentials, RetryMaxAttempts=3), `newClientWithS3` (test constructor), 6 port method implementations
  - `errors.go` — `nonRetryableCodes` map (5 codes), `mapError` (S3→DomainError, context passthrough)
  - `keys.go` — `ArtifactKey`, `DiffKey`, `VersionPrefix`, `DocumentPrefix`, `ContentTypeForArtifact` (JSON/PDF/DOCX)
  - `client_test.go` — 33 unit-теста (mockS3, mockPresigner, apiError helper)
  - `keys_test.go` — 8 unit-тестов
- Добавлены зависимости: `aws-sdk-go-v2 v1.16.16`, `aws-sdk-go-v2/credentials v1.12.20`, `aws-sdk-go-v2/service/s3 v1.27.11`, `smithy-go v1.13.3`

**Ключевые паттерны:**
- Dependency inversion: `S3API` и `PresignAPI` interfaces (ISP — разные типы SDK)
- EndpointResolverFunc для custom endpoint (Yandex Object Storage, HostnameImmutable=true)
- UsePathStyle=true (required для S3-compatible)
- RetryMaxAttempts=3 (встроенный exponential backoff SDK)
- Context errors pass through raw (не обёрнуты в DomainError)
- HeadObject: `isNotFoundError()` проверяет `types.NotFound` + `smithy.APIError` codes (NotFound, NoSuchKey)
- DeleteByPrefix: pagination с `MaxKeys=1000`, empty prefix guard, partial delete error count
- GeneratePresignedURL: negative expiry guard, zero→defaultTTL fallback
- Content-type: `ContentTypeForArtifact()` — PDF для EXPORT_PDF, DOCX для EXPORT_DOCX, JSON для всех остальных
- Key naming: `{org}/{doc}/{ver}/{type}` для артефактов, `{org}/{doc}/diffs/{base}_{target}` для diff
- Compile-time check: `var _ port.ObjectStoragePort = (*Client)(nil)`
- Consistent error mapping: `nonRetryableCodes` map (NoSuchKey, NoSuchBucket, AccessDenied, InvalidBucketName, NotFound)

**Проверки:**
- `go test ./internal/infra/objectstorage/... -race -count=1` — 41 PASS
- `go test -count=1 -race ./...` — 242 PASS (config 20 + model 76 + broker 32 + objectstorage 41 + postgres 73)
- `go vet ./...` — OK
- `make build` — OK
- `make test` — OK
- `make lint` — OK

**Тесты (41):**
- PutObject: success, S3 error, context cancelled, access denied (4)
- GetObject: success, NoSuchKey, nil body, context cancelled (4)
- DeleteObject: success, idempotent, S3 error (3)
- HeadObject: exists, NotFound (types), NoSuchKey (API error), S3 error, context cancelled (5)
- GeneratePresignedURL: success, zero expiry → default, custom expiry, negative expiry, context cancelled, error (6)
- DeleteByPrefix: zero objects, single page, multiple pages, empty prefix, list error, delete error, partial delete, context cancelled between pages (8)
- Error mapping: nil, context canceled, deadline exceeded, retryable API, access denied, no such bucket, unknown (7)
- isNotFoundError: types.NotFound, API NotFound, API NoSuchKey, other error (4)
- Interface compliance (1)
- Keys: artifact key, diff key, version prefix, document prefix (4)
- ContentType: JSON (13 types), PDF, DOCX, all types (4)

**Ревью (code-reviewer + golang-pro):**
- Исправлено: negative expiry guard в GeneratePresignedURL
- Исправлено: error count в DeleteByPrefix partial failure message (N of M)
- Исправлено: explicit MaxKeys=1000 в ListObjectsV2
- Добавлены: TestGeneratePresignedURL_NegativeExpiry, TestGeneratePresignedURL_ContextCancelled
- Не исправлено (deferred): key segment validation (application layer responsibility), empty key validation (same), structured logging (DM-TASK-010)

**Следующие задачи:**
- DM-TASK-009 (Redis клиент) — зависит от DM-TASK-005 ✅
- DM-TASK-015 (Confirmation Publisher) — зависит от DM-TASK-003 ✅ + DM-TASK-007 ✅
- DM-TASK-019 (Document Lifecycle) — зависит от DM-TASK-004 ✅ + DM-TASK-012 ✅
- DM-TASK-011 (Health Check) — зависит от DM-TASK-006 ✅ + DM-TASK-007 ✅ + DM-TASK-008 ✅ + DM-TASK-009
- DM-TASK-044 (Circuit Breaker для Object Storage) — зависит от DM-TASK-008 ✅

---

## DM-TASK-009: Redis клиент — idempotency store с TTL (2026-04-01)

**Статус:** done

**Что сделано:**
- Удалён placeholder `kvstore.go`
- Создано 3 файла в `internal/infra/kvstore/`:
  - `client.go` — `RedisAPI` interface (Set/Get/Del/Ping/Close), `Client` struct, `NewClient(cfg)`, `newClientWithRedis(api)` для тестов
  - `errors.go` — `mapError` (context passthrough, redis.Nil→non-retryable, generic→retryable StorageFailed), `errClientClosed` (non-retryable)
  - `client_test.go` — `mockRedis`, `newInMemoryRedis`, 39 тестов
- Добавлена зависимость `github.com/redis/go-redis/v9 v9.18.0` в `go.mod`

**Реализация Client:**
- `Get(ctx, key)` — Redis GET → JSON unmarshal → `*model.IdempotencyRecord` (nil, nil для not-found — per port contract)
- `Set(ctx, record, ttl)` — JSON marshal → Redis SET с TTL. Nil record guard, empty key в Get/Delete
- `Delete(ctx, key)` — Redis DEL (delete nonexistent key is not an error)
- `Ping(ctx)` — healthcheck (не часть порта, используется health handler)
- `Close()` — graceful shutdown: `sync.Mutex` + `done` channel, idempotentный
- `isClosed()` — non-blocking check через select на `done` channel
- Compile-time: `var _ port.IdempotencyStorePort = (*Client)(nil)`

**Error mapping:**
- `context.Canceled` / `context.DeadlineExceeded` → passthrough (не DomainError)
- `redis.Nil` → nil, nil в Get; non-retryable defensive guard в mapError
- Другие Redis ошибки → `port.NewStorageError` (retryable=true, code=STORAGE_FAILED)
- `errClientClosed` → non-retryable STORAGE_FAILED

**Тесты (39):**
- Get: success, completed record, not found (nil,nil), Redis error, context canceled, context deadline exceeded, invalid JSON, context forwarding, empty key (8+1=9 get tests)
- Set: success + JSON round-trip, all fields, Redis error, context canceled, context deadline exceeded, zero TTL, context forwarding, nil record (7+1=8 set tests)
- Delete: success, key not exists, Redis error, context canceled, context deadline exceeded, context forwarding, empty key (5+2=7 delete tests)
- Ping: success, Redis error (2)
- Close: graceful, idempotent, returns error (3)
- Use-after-close: Get, Set, Delete, Ping (4)
- Error mapping: context canceled, context deadline exceeded, redis.Nil defensive, unknown error (4)
- In-memory lifecycle: Set → Get → Update → Get → Delete → Get (1)
- Concurrent access: 50 goroutines (1)

**Проверки:**
- `go test ./internal/infra/kvstore/... -race -count=1` — 39 PASS
- `go test -count=1 -race ./...` — OK (config 20 + model 76 + broker 32 + objectstorage 41 + kvstore 39 + postgres 73 = 281 PASS)
- `go vet ./...` — OK
- `go build ./cmd/dm-service/` — OK

**Ревью (code-reviewer + golang-pro):**
- Исправлено: nil record guard в Set (H-1)
- Исправлено: empty-key validation в Get/Delete (M-3)
- Исправлено: mapError redis.Nil → non-retryable (M-1)
- Исправлено: RedisAPI doc comment (redis.Cmdable → *redis.Client) (M-3)
- Добавлены: TestSet_NilRecord, TestGet_EmptyKey, TestDelete_EmptyKey, TestDelete_ContextDeadlineExceeded, TestClose_ReturnsError
- Не исправлено (deferred): H-2 (direct port impl vs adapter layer — design choice), TOCTOU isClosed (go-redis internally safe), errClientClosed non-constructor pattern (intentional — non-retryable)

**Следующие задачи (unblocked by DM-TASK-009):**
- DM-TASK-011 (Health Check) — зависит от DM-TASK-006 ✅ + DM-TASK-007 ✅ + DM-TASK-008 ✅ + DM-TASK-009 ✅
- DM-TASK-013 (Idempotency Guard) — зависит от DM-TASK-004 ✅ + DM-TASK-009 ✅
- DM-TASK-015 (Confirmation Publisher) — зависит от DM-TASK-003 ✅ + DM-TASK-007 ✅ (already ready)
- DM-TASK-019 (Document Lifecycle) — зависит от DM-TASK-004 ✅ + DM-TASK-012 ✅ (already ready)

---

## DM-TASK-015: Confirmation Publisher + Notification Publisher (2026-04-01)

**Статус:** done

**План реализации:**
1. Добавить VersionPartiallyAvailable struct в event_outgoing.go (BRE-010)
2. Добавить ConfirmationPublisherPort (10 методов) и NotificationPublisherPort (5 методов) в port/outbound.go
3. Реализовать ConfirmationPublisher в egress/confirmation/ (10 publish методов)
4. Реализовать NotificationPublisher в egress/notification/ (5 publish методов)
5. Написать тесты для обоих publishers
6. Code review через code-reviewer и golang-pro subagents

**Что сделано:**
- Добавлен `VersionPartiallyAvailable` struct в `event_outgoing.go`:
  - Поля: DocumentID, VersionID, OrgID, ArtifactStatus, AvailableTypes, FailedStage (omitempty), ErrorMessage (omitempty)
  - 2 теста: JSON round-trip + omitempty для optional полей
- Добавлены 2 port interface в `port/outbound.go`:
  - `ConfirmationPublisherPort` — 10 методов (Persisted/PersistFailed для DP/LIC/RE + SemanticTreeProvided + ArtifactsProvided + DiffPersisted/DiffPersistFailed)
  - `NotificationPublisherPort` — 5 методов (VersionProcessingArtifactsReady, VersionAnalysisArtifactsReady, VersionReportsReady, VersionCreated, VersionPartiallyAvailable)
- Реализован `ConfirmationPublisher` в `egress/confirmation/confirmation.go`:
  - `confirmationTopicMap` — 10 топиков из BrokerConfig
  - `NewConfirmationPublisher` — panic on nil broker / empty topics
  - `publishJSON` — JSON marshal → broker.Publish, non-retryable DomainError на marshal failure
  - 10 public методов, каждый делегирует в publishJSON с правильным топиком
  - Compile-time check: `var _ port.ConfirmationPublisherPort = (*ConfirmationPublisher)(nil)`
- Реализован `NotificationPublisher` в `egress/notification/notification.go`:
  - `notificationTopicMap` — 5 топиков из BrokerConfig
  - `NewNotificationPublisher` — аналогичная валидация
  - 5 public методов
  - Compile-time check: `var _ port.NotificationPublisherPort = (*NotificationPublisher)(nil)`
- Паттерн: consumer-side `BrokerPublisher` interface per-package (идентичен DP publisher)

**Тесты:**
- confirmation: 39 тестов (10 correct topic + 3 JSON format + 10 round-trip + 3 error passthrough + 1 marshal error + 1 ctx forwarding + 3 omitempty + 1 interface compliance + 2 constructor panic + 3 correlation_id + 2 correlation_id subtests)
- notification: 29 тестов (5 correct topic + 3 JSON format + 5 round-trip + 3 error passthrough + 1 marshal error + 1 ctx forwarding + 2 omitempty + 1 interface compliance + 2 constructor panic + 3 correlation_id + 3 correlation_id subtests)
- `go test ./internal/egress/... -race -count=1` — ALL PASS
- `go test -count=1 ./...` — ALL PASS
- `go vet ./...` — OK
- `make build` — OK, `make test` — OK, `make lint` — OK

**Ревью:**
- code-reviewer: APPROVED — no critical/blocking issues, high quality code
- golang-pro: APPROVED — idiomatic Go, goroutine-safe (immutable after construction), no memory issues
- Замечание (low, inherited from DP): publishJSON uses ErrCodeBrokerFailed for marshal errors instead of ErrCodeInvalidPayload. Kept for DP consistency.

**Следующие задачи (unblocked by DM-TASK-015):**
- DM-TASK-016 (Transactional Outbox) — зависит от DM-TASK-006 ✅ + DM-TASK-007 ✅ + DM-TASK-015 ✅
- DM-TASK-018 (Artifact Query Service) — зависит от DM-TASK-004 ✅ + DM-TASK-008 ✅ + DM-TASK-012 ✅ + DM-TASK-015 ✅

---

## DM-TASK-016: Transactional Outbox (2026-04-01)

**Статус:** done

**Что сделано:**
- Удалён placeholder `outbox.go`
- Создано 3 файла реализации + 3 файла тестов в `internal/egress/outbox/`:
  - `writer.go` — `OutboxWriter` struct:
    - `Write(ctx, aggregateID, topic, event)` — JSON-сериализация + INSERT одного PENDING entry в текущей транзакции
    - `WriteMultiple(ctx, aggregateID, items []TopicEvent)` — batch INSERT нескольких entry с shared CreatedAt
    - `TopicEvent` type — пара (Topic, Event) для batch writes
    - `newUUID()` — UUID v4 через crypto/rand с panic при ошибке CSPRNG
    - Topic validation: пустой topic → VALIDATION_ERROR
    - `StatusPending`/`StatusConfirmed` константы
  - `poller.go` — `OutboxPoller` struct:
    - `Start()` / `Stop()` / `Done()` — goroutine lifecycle с split stop+done channels (safe graceful shutdown)
    - `poll()` — WithTransaction → FetchUnpublished(FOR UPDATE SKIP LOCKED) → Publish → MarkPublished
    - Pre-allocated publishedIDs, skip-on-failure (partial publish → mark only successful)
    - At-least-once delivery guarantee documented
    - `cleanup()` — batched DELETE LIMIT 1000 loop (BRE-018), auto-committed (outside tx)
    - `BrokerPublisher` interface (consumer-side, satisfied by broker.Client)
    - `OutboxMetrics` interface (SetPendingCount, SetOldestPendingAge, IncPublished, IncPublishFailed, IncCleanedUp)
  - `metrics.go` — `OutboxMetricsCollector` struct:
    - `Start()` / `Stop()` / `Done()` — goroutine lifecycle с split stop+done channels
    - Periodic `PendingStats` query → SetPendingCount + SetOldestPendingAge (REV-022)
    - Default interval 5s, immediate collect on Start
- Расширен `port/outbound.go`:
  - `OutboxRepository.PendingStats(ctx) (count, oldestAgeSeconds, err)` — новый метод
  - `OutboxRepository.DeletePublished(ctx, olderThan, limit)` — добавлен limit parameter (0 = unlimited)
- Обновлён `infra/postgres/outbox_repository.go`:
  - `PendingStats` — COUNT(*) + EXTRACT(EPOCH FROM (now() - MIN(created_at))) с partial index
  - `DeletePublished` — conditional LIMIT via subquery + ORDER BY published_at
  - Добавлен import `pgconn`

**Тесты (35 total):**
- `writer_test.go` (10 тестов): happy path, empty aggregateID, marshal error, repo error, WriteMultiple happy path, WriteMultiple empty, WriteMultiple marshal error, empty topic, WriteMultiple empty topic, UUID uniqueness
- `poller_test.go` (15 тестов): 5 constructor panics, poll happy path, poll empty batch, poll partial publish failure, poll all publish fail, poll fetch error, poll mark error, cleanup single batch, cleanup multi batch, cleanup error, Start/Stop lifecycle
- `metrics_test.go` (8 тестов): 3 constructor panics, 2 interval defaults, collect happy path, collect no pending, collect error, Start/Stop lifecycle
- `outbox_repository_test.go` (+5 обновлённых/новых): DeletePublished no limit, DeletePublished with limit, DeletePublished error, PendingStats with results, PendingStats empty, PendingStats error

**Проверки:**
- `go test ./internal/egress/outbox/... -race -count=1` — 30 PASS, no race conditions
- `go test ./internal/infra/postgres/... -race -count=1` — PASS (включая 5 новых outbox тестов)
- `go test -count=1 ./...` — ALL PASS
- `go vet ./...` — OK
- `make build` — OK, `make test` — OK, `make lint` — OK

**Ревью (code-reviewer + golang-pro):**
- 2 BLOCKING исправлено:
  - B1: Stop/Done channel dual semantics → split into stop (signal) + done (completion) channels
  - B2: Silent UUID crypto/rand error → panic on failure (fatal system condition)
- 6 WARNING исправлено:
  - W3: mockTransactor propagates parent context (deadline/cancellation)
  - W4: topic validation — empty topic returns VALIDATION_ERROR
  - At-least-once delivery documented in OutboxPoller comment
  - Pre-allocated publishedIDs slice
  - Status constants (StatusPending, StatusConfirmed)
  - Cleanup comment corrected (auto-committed, not "long-running tx")

**Ключевые решения:**
- OutboxWriter не владеет транзакцией — работает внутри tx caller'а (application service)
- OutboxPoller владеет своей tx через transactor.WithTransaction
- Split stop+done channels для safe graceful shutdown (Stop → signal, Done → wait for goroutine exit)
- UUID v4 через crypto/rand (не google/uuid — нет внешней зависимости), panic при ошибке
- Batched cleanup (DELETE LIMIT 1000) предотвращает long-running DELETE на больших таблицах
- Cleanup вне транзакции (auto-commit) — идемпотентный DELETE
- OutboxMetricsCollector отделён от Poller — независимый lifecycle, не зависит от poll cycle

**Следующие задачи (unblocked by DM-TASK-016):**
- DM-TASK-017 (Artifact Ingestion Service) — зависит от DM-TASK-004 ✅ + DM-TASK-008 ✅ + DM-TASK-012 ✅ + DM-TASK-016 ✅
- DM-TASK-020 (Version Management Service) — зависит от DM-TASK-004 ✅ + DM-TASK-008 ✅ + DM-TASK-012 ✅ + DM-TASK-016 ✅
- DM-TASK-021 (Diff Storage Service) — зависит от DM-TASK-004 ✅ + DM-TASK-008 ✅ + DM-TASK-012 ✅ + DM-TASK-016 ✅
- DM-TASK-042 (Outbox Poller ordering) — зависит от DM-TASK-016 ✅

---

## DM-TASK-013: Idempotency Guard (2026-04-01)

**Статус:** done

**Что сделано:**
- Удалён placeholder `idempotency.go`
- Создано 3 файла в `internal/ingress/idempotency/`:
  - `idempotency.go` — `IdempotencyGuard` struct, `Check()` с atomic SETNX, `MarkCompleted()`, `Cleanup()`
    - `CheckResult` enum: `ResultProcess` / `ResultSkip` / `ResultReprocess`
    - `FallbackChecker` function type для DB fallback при недоступности Redis
    - `MetricsCollector` interface: `IncFallbackTotal(topic)`, `IncCheckTotal(result)` — consumer-side
    - `Logger` interface: `Warn(msg, ...any)`, `Info(msg, ...any)` — consumer-side
    - `NewIdempotencyGuard` с panic на nil deps (store, metrics, logger)
    - Check logic: `ctx.Err()` → SETNX → acquired=true → ResultProcess; acquired=false → Get → evaluate
    - COMPLETED → ResultSkip, PROCESSING fresh → ResultSkip, PROCESSING stuck (≥240s) → Set overwrite → ResultReprocess
    - Redis error → FallbackChecker → ResultProcess/ResultSkip (safe default: process)
  - `keys.go` — 7 key generators для всех входящих event types
    - Формат: `dm:idem:{topic-short}:{job_id}[:{version_id}]`
    - Ingestion events (dp-art, dp-diff, lic-art, re-art): keyed by job_id
    - Query events (dp-tree, lic-req, re-req): keyed by job_id + version_id
    - `mustNotEmpty` validation — panic на пустые IDs
    - `topicShortNames` map для 7 incoming topics
  - `fallback.go` — DB fallback builders
    - `ArtifactFallback` — проверяет artifact_descriptors по producer domain + job_id. Panic на unknown producer
    - `DiffFallback` — проверяет diff по version pair (existence check, unique constraint гарантирует один diff)
- Изменено `port/outbound.go` — добавлен `SetNX(ctx, record, ttl) (bool, error)` в `IdempotencyStorePort`
- Изменено `infra/kvstore/client.go` — добавлен `Client.SetNX()` с Redis SETNX, `SetNX` в `RedisAPI` interface
- Изменено `infra/kvstore/client_test.go` — `setNXFn` в mock, atomic `SetNX` в in-memory store
- Создано 3 файла тестов (53 теста):
  - `idempotency_test.go` — 28 тестов: constructor panics, all Check decision branches, SETNX atomicity, concurrent claim, Redis down + all fallback variants, stuck overwrite, context cancellation, MarkCompleted/Cleanup success+error, CheckResult.String, full lifecycle (process→skip, fail→cleanup→reprocess)
  - `keys_test.go` — 17 тестов: all 7 key generators, uniqueness cross-topic, uniqueness cross-version, all TopicShortName, coverage check, 8 panic-on-empty tests
  - `fallback_test.go` — 8 тестов: artifact fallback (matching/different/no artifacts/repo error, LIC/RE producers, unknown producer panic), diff fallback (exists/not-found/repo error)

**Проверки:**
- `go test ./internal/ingress/idempotency/... -race -count=1` — 53 PASS
- `go test -count=1 ./...` — OK (все пакеты)
- `go vet ./...` — OK
- `make build` — OK
- `make test` — OK
- `make lint` — OK

**Ревью (code-reviewer + golang-pro):**
- 2 BLOCKING исправлено:
  - B1: GET+SET race → atomic SETNX (добавлен SetNX в port + kvstore + guard)
  - B2: Context cancellation не проверялась → early ctx.Err() check, returns error
- 5 WARNING исправлено:
  - W1: Stuck cleanup delete+set → single Set overwrite (no gap window)
  - W2: Key functions без валидации → mustNotEmpty panic on empty IDs
  - W3: Logger `interface{}` → `any` (Go 1.26.1 modern style)
  - W4: Custom contains в тестах → `strings.Contains`
  - W5: ArtifactFallback silently returns false for unknown producer → panic

**Ключевые решения:**
- Atomic SETNX вместо GET+SET: eliminates race window при concurrent consumers
- FallbackChecker as function type (не repository injection): decoupled, testable, nil = "always process"
- Consumer-side interfaces (MetricsCollector, Logger): hexagonal pattern, no Prometheus/slog coupling
- Key format dm:idem:{topic-short}:{ids}: topic prefix prevents cross-topic collisions
- Ingestion events keyed by job_id alone (job produces exactly one artifact set)
- Query events keyed by job_id+version_id (comparison pipeline requests two trees)
- Safe defaults: Redis failure → process (handlers are idempotent), DB fallback failure → process
- Stuck threshold 240s = 2× ProcessingTTL (120s): gives ample time for legitimate processing

**Следующие задачи (unblocked by DM-TASK-013):**
- DM-TASK-014 (Event Consumer) — зависит от DM-TASK-003 ✅ + DM-TASK-007 ✅ + DM-TASK-013 ✅
- DM-TASK-038 (Idempotency Guard enhancements) — зависит от DM-TASK-013 ✅

**Ready critical tasks:**
- DM-TASK-014 (Event Consumer) — all deps done
- DM-TASK-017 (Artifact Ingestion Service) — all deps done
- DM-TASK-018 (Artifact Query Service) — all deps done
- DM-TASK-019 (Document Lifecycle Service) — all deps done
- DM-TASK-020 (Version Management Service) — all deps done
- DM-TASK-021 (Diff Storage Service) — all deps done

---

## DM-TASK-017: Artifact Ingestion Service (2026-04-01)

**Статус:** done

**Что сделано:**
- Создан `internal/application/ingestion/ingestion.go` — ArtifactIngestionService:
  - `HandleDPArtifacts` — 5 артефактов (OCR_RAW, EXTRACTED_TEXT, DOCUMENT_STRUCTURE, SEMANTIC_TREE, PROCESSING_WARNINGS), status PENDING → PROCESSING_ARTIFACTS_RECEIVED
  - `HandleLICArtifacts` — 8 артефактов (CLASSIFICATION_RESULT, KEY_PARAMETERS, RISK_ANALYSIS, RISK_PROFILE, RECOMMENDATIONS, SUMMARY, DETAILED_REPORT, AGGREGATE_SCORE), status → ANALYSIS_ARTIFACTS_RECEIVED
  - `HandleREArtifacts` — claim-check pattern (EXPORT_PDF, EXPORT_DOCX via BlobReference), status → FULLY_READY
  - `processIngestion` — shared flow: saveBlobs → WithTransaction(FindByID + TransitionArtifactStatus + Insert descriptors + Update version + Insert 2 audit records + WriteMultiple outbox)
  - `saveBlobs` — PutObject для DP/LIC, HeadObject verify для RE
  - `compensate` — best-effort DeleteObject с 30s timeout, context.Background()
  - `extractDPArtifacts/extractLICArtifacts/extractREArtifacts` — event → artifactItem helpers
  - `validateRequired` — orgID + jobID + documentID + versionID validation
  - `sha256Hex` — SHA-256 content hash
  - `generateUUID` — UUID v4 via crypto/rand
- Compile-time check: `var _ port.ArtifactIngestionHandler = (*ArtifactIngestionService)(nil)`
- Outbox: WriteMultiple(versionID, [confirmation, notification]) — aggregate_id = versionID для FIFO (REV-010)
- Audit: 2 records per ingestion — ARTIFACT_SAVED + ARTIFACT_STATUS_CHANGED с Details JSON

**Ревью:** code-reviewer + golang-pro → 2 blocking + 14 warnings исправлено:
- B1: Missing compensation after DB tx failure → added s.compensate(blobs) в error path
- B2: orgID validation missing → добавлен в validateRequired
- W1: json.Marshal audit error ignored → added error check + warn log
- W2: compensate unbounded context → added 30s timeout
- W3: State transition error not wrapped → wraps original as Cause
- W4: Missing tests → added outbox/audit/version update failure tests

**Проверки:**
- `go test ./internal/application/ingestion/... -race -count=1` — 30 PASS
- `go test -count=1 ./...` — OK (all packages)
- `go vet ./...` — OK
- `make build` — OK
- `make test` — OK
- `make lint` — OK

**Следующие задачи (ready, critical):**
- DM-TASK-014 (Event Consumer) — blocks DM-TASK-025 (wiring)
- DM-TASK-018 (Artifact Query) — blocks DM-TASK-022 (API)
- DM-TASK-019 (Document Lifecycle) — blocks DM-TASK-022
- DM-TASK-020 (Version Management) — blocks DM-TASK-022
- DM-TASK-021 (Diff Storage) — blocks DM-TASK-022
- DM-TASK-036 (REV-001/REV-002 fallback) — now unblocked by DM-TASK-017
- DM-TASK-037 (BRE-001 FOR UPDATE) — now unblocked by DM-TASK-017

---
