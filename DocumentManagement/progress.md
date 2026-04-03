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

## DM-TASK-014: Event Consumer (2026-04-02)

**Статус:** done

**Что сделано:**
- Создан `internal/ingress/consumer/consumer.go` (~350 строк) — полная реализация EventConsumer
- Consumer-side interfaces: `Logger`, `MetricsCollector`, `BrokerSubscriber`, `IdempotencyChecker`
- `TopicConfig` — 7 incoming topic names из конфигурации
- `NewEventConsumer` — конструктор с panic на nil deps + empty topics
- `Start()` — sync.Once + 7 `broker.Subscribe()` вызовов
- `wrapHandler()` — panic recovery с `debug.Stack()` + always-nil return
- 7 per-topic handlers:
  - `handleDPArtifacts` → `ingestion.HandleDPArtifacts` (KeyForDPArtifacts, ArtifactFallback/DP)
  - `handleGetSemanticTree` → `query.HandleGetSemanticTree` (KeyForSemanticTreeRequest, noopFallback)
  - `handleDiffReady` → `diff.HandleDiffReady` (KeyForDiffReady, DiffFallback)
  - `handleLICArtifacts` → `ingestion.HandleLICArtifacts` (KeyForLICArtifacts, ArtifactFallback/LIC)
  - `handleLICRequestArtifacts` → shared `handleGetArtifactsRequest` (KeyForLICRequest, noopFallback)
  - `handleREArtifacts` → `ingestion.HandleREArtifacts` (KeyForREArtifacts, ArtifactFallback/RE)
  - `handleRERequestArtifacts` → shared `handleGetArtifactsRequest` (KeyForRERequest, noopFallback)
- `processWithIdempotency()` — shared: Check→Skip/Process/Reprocess→handler→MarkCompleted/Cleanup
- `validateCommon()` — 4 required fields (correlation_id, timestamp, job_id, document_id)
- `checkSchemaVersion()` — REV-031: WARN на unknown schema_version, обработка продолжается
- `noopFallback` — для query requests (idempotent reads, no DB state)
- `rawPreview()` — UTF-8 safe truncation at rune boundary

**Ключевые решения:**
- Always return nil — prevent poison-pill requeue. Все ошибки обрабатываются внутренне.
- IdempotencyChecker interface (не конкретный тип) — testability через mock
- LIC/RE GetArtifactsRequest — shared implementation, различаются по idempotency key (lic-req vs re-req)
- Fallback: ArtifactFallback для ingestion events (DB check), DiffFallback для diff, noopFallback для queries
- DLQ integration отложена до DM-TASK-023 (зависит от DM-TASK-014)

**Проверки:**
- `go test ./internal/ingress/consumer/... -race -count=1` — 70 PASS
- `go test -count=1 ./...` — ALL PASS (все 30 пакетов)
- `go vet ./...` — OK
- `make build` — OK
- `make test` — OK
- `make lint` — OK

**Code Review (code-reviewer + golang-pro):**
- 0 BLOCKING issues
- 10 warnings, исправлены:
  - debug.Stack() в panic recovery (stack trace для post-mortem)
  - UTF-8 safe rawPreview (Cyrillic text в артефактах)
  - Shared mocks fix в subtests (каждый subtest с fresh deps)
  - +5 missing tests (RE missing version_id, query handler errors, TargetVersionID only empty)

**Следующие задачи (ready, critical):**
- DM-TASK-018 (Artifact Query) — blocks DM-TASK-022 (API)
- DM-TASK-019 (Document Lifecycle) — blocks DM-TASK-022
- DM-TASK-020 (Version Management) — blocks DM-TASK-022
- DM-TASK-021 (Diff Storage) — blocks DM-TASK-022
- DM-TASK-036 (REV-001/REV-002 fallback) — now unblocked by DM-TASK-017
- DM-TASK-037 (BRE-001 FOR UPDATE) — now unblocked by DM-TASK-017
- DM-TASK-038 (BRE-003 idempotency TTL) — now unblocked by DM-TASK-013
- DM-TASK-010 (Observability) — high priority, blocks DM-TASK-025
- DM-TASK-011 (Health Check) — high priority, blocks DM-TASK-025

---

## DM-TASK-019: Document Lifecycle Service (2026-04-02)

**Статус:** done

**План реализации:**
1. Изучить порты (DocumentLifecycleHandler), модели (Document, AuditRecord), паттерны из ArtifactIngestionService
2. Спроектировать сервис: 5 методов (Create, Get, List, Archive, Delete), 4 зависимости (Transactor, DocRepo, AuditRepo, Logger)
3. Реализовать lifecycle.go с compile-time interface check
4. Реализовать lifecycle_test.go с ~35 тестами
5. Code review → исправления
6. Полный прогон тестов (go test, go vet, make build/test/lint)

**Что сделано:**
- Создан `internal/application/lifecycle/lifecycle.go` (~230 строк):
  - `DocumentLifecycleService` struct с 4 зависимостями + newUUID func
  - `NewDocumentLifecycleService` с panic на nil deps (4 проверки)
  - `CreateDocument` — validate(orgID, title, userID) → NewDocument → tx(Insert + AuditInsert)
  - `GetDocument` — validate(orgID, docID) → FindByID (tenant isolation через organization_id)
  - `ListDocuments` — validate(orgID, page, pageSize) → clamp pageSize(max 100) → List → nil-slice normalize
  - `ArchiveDocument` / `DeleteDocument` → shared `transitionDocument` helper: validate → tx(FindByID + TransitionTo + Update + AuditInsert)
  - `generateUUID` v4 crypto/rand (panic on CSPRNG failure)
  - Compile-time check: `var _ port.DocumentLifecycleHandler = (*DocumentLifecycleService)(nil)`
- Создан `internal/application/lifecycle/lifecycle_test.go` (36 тестов):
  - 5 constructor panics (nil deps)
  - 7 CreateDocument (happy path + 3 validation + insert fail + audit fail + already exists)
  - 4 GetDocument (happy path + 2 validation + not found)
  - 8 ListDocuments (happy path + filter + 3 validation + nil normalize + page size clamp + repo error)
  - 7 ArchiveDocument (happy path + 2 validation + not found + 2 invalid transition + update fail + audit fail)
  - 8 DeleteDocument (happy path active + happy path archived + 2 validation + not found + invalid transition + update fail + audit fail)

**Ключевые решения:**
- Транзакции для всех мутирующих операций (Document + Audit атомарно)
- Read-операции без транзакций (Get, List — single query)
- ActorTypeSystem + "system" для archive/delete (port interface не несёт user identity — будет добавлено в DM-TASK-022)
- ActorTypeUser + createdByUserID для CreateDocument (identity доступна через params)
- Shared `transitionDocument` для DRY (Archive/Delete отличаются только targetStatus + auditAction)
- maxPageSize = 100 для защиты от full-table scans
- Nil-slice normalize для JSON `[]` (не `null`)

**Code Review (code-reviewer):**
- 1 BLOCKING исправлено: ActorTypeUser → ActorTypeSystem для archive/delete audit records
- 3 WARNING исправлены: DRY refactor (transitionDocument), json.Marshal error logging
- 2 WARNING отклонены: title length validation (DB layer handles), StatusFilter validation (defense in depth, но repo handles)
- 2 WARNING деferred: generateUUID duplication (package-per-package is intentional Go pattern), Logger duplication (same)

**Проверки:**
- `go test ./internal/application/lifecycle/... -race -count=1` — 36 PASS
- `go test -count=1 ./...` — ALL PASS (16 пакетов)
- `go vet ./...` — OK
- `make build` — OK
- `make test` — OK
- `make lint` — OK

**Следующие задачи (ready, critical):**
- DM-TASK-018 (Artifact Query) — blocks DM-TASK-022 (API)
- DM-TASK-020 (Version Management) — blocks DM-TASK-022
- DM-TASK-021 (Diff Storage) — blocks DM-TASK-022
- DM-TASK-036 (REV-001/REV-002 fallback) — unblocked by DM-TASK-017
- DM-TASK-037 (BRE-001 FOR UPDATE) — unblocked by DM-TASK-017
- DM-TASK-038 (BRE-003 idempotency TTL) — unblocked by DM-TASK-013
- DM-TASK-010 (Observability) — high priority, blocks DM-TASK-025
- DM-TASK-011 (Health Check) — high priority, blocks DM-TASK-025

---

## DM-TASK-020: Version Management Service (2026-04-02)

**Статус:** done

**Что сделано:**
- Создан `internal/application/version/version.go` (~270 строк)
- Реализует `port.VersionManagementHandler` (3 метода: CreateVersion, GetVersion, ListVersions)
- Compile-time check: `var _ port.VersionManagementHandler = (*VersionManagementService)(nil)`

**CreateVersion flow:**
1. `validateCreateParams` — проверка required fields + OriginType validation + SourceFileSize > 0
2. RE_CHECK: parent version lookup → copy source_file_key
3. Retry loop (до 3 попыток) с `ctx.Err()` check:
   - `WithTransaction`: FindByID doc (inside tx для TOCTOU protection) → status check ACTIVE → NextVersionNumber → NewDocumentVersion → Insert → Update doc.current_version_id → Audit VERSION_CREATED → Outbox VersionCreated
4. Optimistic locking: retry при VersionAlreadyExists (unique constraint на version_number)

**GetVersion / ListVersions:**
- Стандартные validate + repo call
- ListVersions: clamp pageSize(100), nil-slice normalize

**Ревью (code-reviewer + golang-pro):**
- 2 BLOCKING исправлено:
  - TOCTOU: doc status check перенесён внутрь транзакции (как lifecycle.transitionDocument)
  - Missing OriginType validation
- 3 WARNINGS исправлено:
  - SourceFileSize > 0 validation
  - ctx.Err() check в retry loop
  - Doc re-fetch on each retry attempt

**Тесты:** 43 unit-теста:
- 6 constructor panics
- 13 CreateVersion happy paths (upload, parent, description, RE_CHECK, all 5 origin types)
- 10 validation errors (org, doc, origin, filename, filesize×2, user, source_key, RE_CHECK parent×2)
- 3 doc status errors (not found, archived, deleted + no-retry verify)
- 5 tx step failures (NextVersionNumber, Insert, Update, Audit, Outbox)
- 3 optimistic locking (success on retry, exhaust retries, non-conflict no retry)
- 2 ctx/refetch tests (context cancelled, doc re-fetched inside tx on retry)
- 5 GetVersion (happy path + 3 validation + not found)
- 8 ListVersions (happy path + 2 validation + invalid page/size + nil normalize + page clamp + repo error)
- 1 isValidOriginType helper

**Проверки:**
- `go test -race -count=1 ./internal/application/version/...` — 43 tests PASS
- `go test -count=1 ./...` — ALL PASS (16 пакетов)
- `go vet ./...` — OK
- `make build/test/lint` — ALL OK

**Следующие задачи (ready, critical):**
- DM-TASK-018 (Artifact Query) — blocks DM-TASK-022 (API)
- DM-TASK-021 (Diff Storage) — blocks DM-TASK-022
- DM-TASK-036 (REV-001/REV-002 fallback) — unblocked by DM-TASK-017
- DM-TASK-037 (BRE-001 FOR UPDATE) — unblocked by DM-TASK-017
- DM-TASK-038 (BRE-003 idempotency TTL) — unblocked by DM-TASK-013
- DM-TASK-010 (Observability) — high priority, blocks DM-TASK-025
- DM-TASK-011 (Health Check) — high priority, blocks DM-TASK-025

---

## DM-TASK-021: Diff Storage Service (2026-04-02)

**Статус:** done

**План реализации:**
1. Изучить порты (DiffStorageHandler, DiffRepository, ObjectStoragePort), модели (VersionDiffReference, DocumentVersionDiffReady/Persisted/PersistFailed), outbox pattern
2. Спроектировать DiffStorageService: struct, зависимости, HandleDiffReady flow, GetDiff flow, idempotency (REV-028)
3. Реализовать service + tests
4. Code review, полная проверка тестов, make targets

**Что сделано:**
- Создан `internal/application/diff/diff.go` (~260 строк):
  - `DiffStorageService` struct с 7 зависимостями (transactor, versionRepo, diffRepo, auditRepo, objectStorage, outboxWriter, logger)
  - `NewDiffStorageService` — constructor с panic на nil deps, `newUUID` hook для тестируемости
  - `HandleDiffReady` — полный flow: validate 5 полей → FindByID base+target versions → merge TextDiffs+StructuralDiffs в diffBlob (ensureJSONArray для nil→[]) → PutObject (deterministic DiffKey) → WithTransaction(Insert VersionDiffReference + AuditInsert DIFF_SAVED + Outbox Write DiffPersisted)
  - **REV-028 idempotency**: при DiffAlreadyExists → Write DiffPersisted для текущего job_id без перезаписи, без audit; S3 key deterministic → harmless PutObject overwrite
  - **Compensation**: при tx failure → compensateDiffBlob (context.Background(), 30s timeout, best-effort)
  - `GetDiff` — validate params → FindByVersionPair → GetObject → io.ReadAll → return ref+data
  - Helpers: validateDiffRequired, validateGetDiffParams, ensureJSONArray, sha256Hex, generateUUID, compensateDiffBlob
  - Compile-time check: `var _ port.DiffStorageHandler = (*DiffStorageService)(nil)`
- Создан `diff_test.go` с 23 unit-тестами:
  - 7 constructor panic tests
  - HandleDiffReady: happy path, nil diffs, 5 validation errors, base/target version not found, PutObject failure, tx failure+compensation, idempotency REV-028, audit failure, outbox failure, context cancelled, aggregate_id, audit details, storage key format, non-conflict DB error, correlation_id preserved, interface compliance
  - GetDiff: happy path, 4 validation errors, diff not found, storage get failure
  - Helpers: ensureJSONArray (3 cases), sha256Hex

**Проверки:**
- `go test ./internal/application/diff/... -race -count=1` — 23 tests PASS
- `go test -count=1 ./...` — ALL PASS (все 18 пакетов с тестами)
- `go vet ./...` — OK
- `make build` — OK
- `make test` — OK
- `make lint` — OK
- Code review (code-reviewer subagent): 0 blocking, 2 warnings (minor, not fixed: io.ReadAll edge case test, generateUUID panic test)

**Ключевые решения:**
- diffBlob struct для объединения TextDiffs и StructuralDiffs в один S3-объект
- Deterministic S3 key из version pair → PutObject идемпотентен
- aggregate_id = targetVersionID для FIFO ordering в outbox (как в ingestion)
- DiffPersistFailed НЕ публикуется сервисом (outbox недоступен после failed tx), ответственность consumer layer
- Version validation ДО upload (fail fast)

**Следующие задачи (ready, critical):**
- DM-TASK-018 (Artifact Query) — единственная critical pending, блокирует DM-TASK-022 (API)
- DM-TASK-036 (REV-001/REV-002 fallback) — unblocked by DM-TASK-017
- DM-TASK-037 (BRE-001 FOR UPDATE) — unblocked by DM-TASK-017
- DM-TASK-038 (BRE-003 idempotency TTL) — unblocked by DM-TASK-013
- DM-TASK-010 (Observability) — high priority, blocks DM-TASK-025
- DM-TASK-011 (Health Check) — high priority, blocks DM-TASK-025

---

## DM-TASK-018: Artifact Query Service (2026-04-02)

**Статус:** done

**Что сделано:**
- Создан `internal/application/query/query.go` (~310 строк) с `ArtifactQueryService`
- Реализованы 4 метода интерфейса `port.ArtifactQueryHandler`:
  - `HandleGetSemanticTree` — async: validate → FindByVersionAndType → not-found → publish error response / infra → return for retry / success → readArtifact → audit → PublishSemanticTreeProvided
  - `HandleGetArtifacts` — async: validate → ListByVersionAndTypes → read each → missing types detection → audit → PublishArtifactsProvided
  - `GetArtifact` — sync: validate → FindByVersionAndType → readArtifact → return ArtifactContent
  - `ListArtifacts` — sync: validate → ListByVersion → nil→[] normalize
- Создан `query_test.go` — 37 unit-тестов

**Архитектурные решения:**
- Direct publish через ConfirmationPublisher (не outbox): нет DB writes, не требуется transactional consistency
- Error handling: infra errors (retryable) → return для retry consumer'ом; not-found → publish response с ErrorCode/ErrorMessage (DP может продолжить)
- Async audit: recordAuditAsync с goroutine + context.Background() + 5s timeout — не блокирует response path
- io.LimitReader с 50MB лимитом для защиты от OOM
- inferRequesterDomain: LIC types → RE requester, DP types → LIC requester
- Thread-safe mock'и с sync.Mutex для async audit goroutine
- Polling helpers (waitForAudit, waitForLogs) вместо time.Sleep в тестах

**Ревью (code-reviewer):**
- APPROVED with warnings
- W1 (objectstorage import в application layer): matches ingestion pattern, kept as-is
- W2 (json.Marshal fallback): исправлено — details = `{}` вместо nil
- W3 (time.Sleep в тестах): исправлено — polling helpers с 2s deadline

**Проверки:**
- `go test -race -count=1 ./internal/application/query/...` — 37 tests PASS
- `go test -count=1 ./...` — ALL PASS
- `go vet ./...` — OK
- `make build/test/lint` — ALL OK

**Следующие задачи (ready):**
- Все critical задачи application layer завершены (017-021 done)
- DM-TASK-022 (API Handler) — now unblocked (deps: 017✅, 018✅, 019✅, 020✅, 021✅) — HIGH, blocks DM-TASK-025
- DM-TASK-036 (REV-001/REV-002 fallback) — critical, deps done
- DM-TASK-037 (BRE-001 FOR UPDATE) — critical, deps done
- DM-TASK-038 (BRE-003 idempotency TTL) — critical, deps done
- DM-TASK-010 (Observability) — high, deps done, blocks DM-TASK-025
- DM-TASK-011 (Health Check) — high, deps done, blocks DM-TASK-025

---

## DM-TASK-010: Observability SDK (2026-04-02)

**Статус:** done

**Что сделано:**
- Создан полный Observability SDK в `internal/infra/observability/` (6 Go файлов + 6 test файлов)
- **context.go** — `EventContext` struct (CorrelationID, JobID, DocumentID, VersionID, OrganizationID, Stage), `WithEventContext`, `EventContextFrom`, `WithStage`. Аналог DP `JobContext` с расширением для version_id и organization_id
- **logger.go** — `Logger` struct обёртка над `slog.Logger`, JSON output:
  - `Info/Warn/Error/Debug(msg, args...)` — без ctx, совместимость с 7 существующими consumer-side Logger interfaces (consumer, idempotency, lifecycle, ingestion, version, query, diff)
  - `InfoContext/WarnContext/ErrorContext/DebugContext(ctx, msg, args...)` — auto-enrichment из EventContext
  - `With(args...)` — component-scoped child loggers
  - `Slog()` — direct access к slog.Logger
- **metrics.go** — 18 Prometheus метрик в dedicated registry:
  - Event processing: `dm_events_received_total` counter[topic], `dm_events_processed_total` counter[topic,status], `dm_event_processing_duration_seconds` histogram[topic]
  - Artifacts: `dm_artifacts_stored_total` counter[producer,artifact_type]
  - Sync API: `dm_api_requests_total` counter[method,path,status_code], `dm_api_request_duration_seconds` histogram[method,path]
  - Outbox: `dm_outbox_pending_count` gauge, `dm_outbox_oldest_pending_age_seconds` gauge (REV-022), `dm_outbox_published_total` counter[topic], `dm_outbox_publish_failed_total` counter[topic], `dm_outbox_cleaned_up_total` counter
  - DLQ: `dm_dlq_messages_total` counter[reason]
  - Defensive: `dm_missing_version_id_total` counter, `dm_idempotency_fallback_total` counter[topic], `dm_idempotency_check_total` counter[result]
  - Version health: `dm_stuck_versions_count` gauge
  - Data integrity: `dm_integrity_check_failures_total` counter
  - Circuit breaker: `dm_circuit_breaker_state` gauge[component]
  - Реализованы методы для 3 consumer-side interfaces: consumer.MetricsCollector, idempotency.MetricsCollector, outbox.OutboxMetrics
- **tracer.go** — OpenTelemetry Tracer с OTLP/HTTP exporter, noop fallback, configurable insecure
- **handler.go** — MetricsHandler для /metrics endpoint через promhttp
- **observability.go** — SDK composite: `New(ctx, cfg)` с `service=document-management` attr, `Shutdown(ctx)`
- **config** — добавлен `TracingInsecure` bool + `DM_TRACING_INSECURE` env var

**Дизайн решения — отличия от DP:**
- Logger в DM НЕ принимает `ctx` как первый параметр в основных методах (Info/Warn/Error/Debug), потому что все 7 существующих consumer-side Logger interfaces определены без ctx. Вместо этого предоставляются отдельные *Context-методы (InfoContext/WarnContext/ErrorContext/DebugContext)
- EventContext расширен по сравнению с DP JobContext: добавлены VersionID и OrganizationID (DM — stateful, оперирует версиями и организациями)
- Metrics содержит 18 DM-специфичных метрик (vs 6 в DP) — отражает сложность stateful сервиса
- Logger обогащён атрибутом `service=document-management` при инициализации через SDK.New()

**Code review:**
- code-reviewer + golang-pro → 2 blocking исправлено:
  - B-1: hardcoded insecure tracer → configurable via TracingInsecure + DM_TRACING_INSECURE
  - B-2: IncCleanedUp panic on negative → guard added
- 5 warnings исправлено (dead code, missing tests, globalOTelOnce comment)
- W-1 (compile-time interface checks): не добавлены в observability пакет — это бы создало circular dependency. Будут добавлены в wiring layer (DM-TASK-025)

**Проверки:**
- `go test ./internal/infra/observability/... -race -count=1` — 52 PASS
- `go test -count=1 ./...` — ALL PASS
- `go vet ./...` — OK
- `make build` — OK
- `make test` — OK
- `make lint` — OK

**Следующие задачи (ready):**
- DM-TASK-022 (API Handler) — high, deps done, blocks DM-TASK-025
- DM-TASK-036 (REV-001/REV-002 fallback) — critical, deps done
- DM-TASK-037 (BRE-001 FOR UPDATE) — critical, deps done
- DM-TASK-038 (BRE-003 idempotency TTL) — critical, deps done
- DM-TASK-025 (Application wiring) — blocked by DM-TASK-010✅, DM-TASK-011✅, DM-TASK-014✅, DM-TASK-022, DM-TASK-016✅

---

## DM-TASK-011: Health Check Handler (2026-04-02)

**Статус:** done

**Что сделано:**
- Создан `internal/infra/health/health.go` (~200 строк) + `health_test.go` (26 тестов)
- **Handler struct** — `atomic.Bool` ready flag, core/nonCore checker maps, configurable timeout
- **ComponentChecker** — `func(ctx context.Context) error` функциональный тип для максимальной гибкости
- **GET /healthz** — liveness probe, always returns 200 OK `{"status":"ok"}`
- **GET /readyz** — readiness probe с component breakdown:
  - Core checkers (postgres, redis, rabbitmq) — обязательные, failure → 503
  - Non-core checkers (object_storage) — информационные, failure не блокирует readiness (REV-024)
  - JSON response: `{"status":"ready|not_ready","components":{"name":{"status":"up|down","error":"..."}}}`
- **Concurrent execution** — goroutines + sync.Mutex + sync.WaitGroup, per-component context.WithTimeout
- **Panic recovery** — checker panic → component reported as "down", не крашит handler
- **Go 1.22+ method-aware routing** — `GET /healthz`, `GET /readyz` (POST → 405)
- **Name collision guard** — panic в конструкторе при одинаковом имени в core и non-core
- **HandlerOption** — `WithCheckTimeout(d)` functional option, default 5s
- **SetReady/Mux** — toggle для graceful startup/shutdown

**Дизайн решения — отличия от DP:**
- DP health handler: простой SetReady toggle без реальных проверок компонентов
- DM health handler: фактические проверки инфра-клиентов через ComponentChecker:
  - PostgreSQL → `Ping(ctx)`
  - Redis → `Ping(ctx)`
  - RabbitMQ → `IsConnected()`
  - Object Storage → `HeadObject(ctx, key)` (будет в wiring)
- Разделение core/non-core для REV-024 compliance

**Code review:**
- code-reviewer + golang-pro → 0 blocking issues
- 7 warnings исправлено:
  - W-01: Name collision guard между core и non-core maps → panic
  - W-02: HTTP method restriction → Go 1.22+ `GET /healthz` pattern
  - W-03: Redundant sort.Strings → removed (sort.Slice sufficient)
  - W-05: POST → 405 test added
  - W-06: Panic recovery в goroutines + 2 теста
  - W-07: Checked type assertion в тесте

**Проверки:**
- `go test ./internal/infra/health/... -race -count=1` — 26 PASS
- `go test -count=1 ./...` — ALL PASS
- `go vet ./...` — OK
- `make build/test/lint` — ALL OK

**Следующие задачи (ready):**
- DM-TASK-022 (API Handler) — high, deps done, blocks DM-TASK-025 (и ещё 5 задач)
- DM-TASK-036 (REV-001/REV-002 fallback) — critical, deps done
- DM-TASK-037 (BRE-001 FOR UPDATE) — critical, deps done
- DM-TASK-038 (BRE-003 idempotency TTL) — critical, deps done
- DM-TASK-025 (Application wiring) — blocked by DM-TASK-022 only

---

## DM-TASK-022: API Handler — HTTP REST endpoints (2026-04-02)

**Статус:** done

**План:**
1. Изучить api-specification.yaml, inbound/outbound ports, модели, application services
2. Спроектировать: auth context extraction, middleware chain, error mapping, router
3. Реализовать 4 файла: auth.go, middleware.go, response.go, handler.go
4. Добавить ActorID в AuditListParams + обновить audit_repository
5. Написать unit-тесты
6. Code review (code-reviewer + golang-pro)
7. Исправить замечания
8. Финальная проверка

**Реализация:**
- **auth.go**: AuthContext struct (OrganizationID, UserID, Role), authMiddleware с header extraction + regex validation (identifierRe `^[a-zA-Z0-9._-]{1,128}$`) для defense-in-depth
- **middleware.go**: responseWriter с single-instance sharing (logging reuses metrics wrapper), WriteHeader guard (first-call-wins), Flush()/Unwrap() для http.Flusher+ResponseController; metricsMiddleware (dm_api_requests_total, dm_api_request_duration_seconds, r.Pattern for cardinality); loggingMiddleware
- **response.go**: ErrorResponse, PaginatedResponse, writeJSON с X-Content-Type-Options:nosniff, writeServiceError — DomainError→HTTP: NotFound→404, Conflict→409, Validation→400, TenantMismatch→404 (hidden), Retryable→500 (generic)
- **handler.go**: 12 endpoints с Go 1.22+ method-aware routing:
  - POST /api/v1/documents — CreateDocument
  - GET /api/v1/documents — ListDocuments (status filter, pagination)
  - GET /api/v1/documents/{document_id} — GetDocument
  - DELETE /api/v1/documents/{document_id} — DeleteDocument (soft)
  - POST /api/v1/documents/{document_id}/archive — ArchiveDocument
  - POST /api/v1/documents/{document_id}/versions — CreateVersion
  - GET /api/v1/documents/{document_id}/versions — ListVersions
  - GET /api/v1/documents/{document_id}/versions/{version_id} — GetVersion (с artifacts)
  - GET /api/v1/documents/{document_id}/versions/{version_id}/artifacts — ListArtifacts (filter by type/producer)
  - GET /api/v1/documents/{document_id}/versions/{version_id}/artifacts/{artifact_type} — GetArtifact (JSON inline / blob 302 redirect)
  - GET /api/v1/documents/{document_id}/diffs/{base_version_id}/{target_version_id} — GetDiff
  - GET /api/v1/audit — ListAuditRecords (filters: document_id, version_id, action, actor_id, from/to)
- MaxBytesReader 1MiB на POST bodies
- Blob redirect: ListArtifacts→descriptor.StorageKey→GeneratePresignedURL (без загрузки контента)
- isValidDocumentStatus/OriginType/ArtifactType validation на API boundary
- Pagination: defaults page=1 size=20, max=100

**Изменения в других файлах:**
- port/outbound.go: добавлен `ActorID string` в `AuditListParams`
- infra/postgres/audit_repository.go: добавлен `actor_id` filter в динамическом WHERE

**Ревью (code-reviewer + golang-pro):**
- 5 blocking исправлено: responseWriter double WriteHeader, Flush/Unwrap, MaxBytesReader, blob content discard→ListArtifacts, SOURCE_FILE removed
- 12 warnings исправлено: auth header validation, status/origin/artifact type validation, X-Content-Type-Options:nosniff, audit date 400 on invalid, middleware comment, test fixes

**Проверки:**
- `go test ./internal/ingress/api/... -race -count=1` — 55 PASS
- `go test -count=1 ./...` — ALL PASS (21 packages)
- `go vet ./...` — OK
- `make build/test/lint` — ALL OK

**Следующие задачи (ready):**
- DM-TASK-025 (Application wiring) — high, deps now all done (was blocked by 022)
- DM-TASK-036 (REV-001/REV-002 fallback) — critical, deps done
- DM-TASK-037 (BRE-001 FOR UPDATE) — critical, deps done
- DM-TASK-038 (BRE-003 idempotency TTL) — critical, deps done
- DM-TASK-023 (DLQ) — high, deps done
- DM-TASK-024 (Audit Trail) — high, deps now all done (was blocked by 022)
- DM-TASK-030 (Tenant isolation enforcement) — high, deps now all done

---

## DM-TASK-025: Application wiring: main.go с graceful startup/shutdown (2026-04-02)

**Статус:** done

**Что сделано:**
- Реализован `cmd/dm-service/main.go` (~370 строк) — полный wiring всех компонентов DM-сервиса
- **Startup (16 фаз):**
  1. `config.Load()` — env-based конфигурация с DM_ prefix
  2. `observability.New()` — Logger + Metrics + Tracer
  3. `postgres.NewPostgresClient()` + `Migrator.Up()` — подключение + миграции
  4. `kvstore.NewClient()` — Redis для идемпотентности
  5. `broker.NewClient()` + `DeclareTopology()` — RabbitMQ + queues
  6. `objectstorage.NewClient()` — S3-compatible хранилище
  7. Transactor + 6 repositories + `poolOutboxRepository` wrapper
  8. OutboxWriter
  9. ConfirmationPublisher (прямая публикация для query responses)
  10. IdempotencyGuard
  11. 5 Application Services (ingestion/query/lifecycle/version/diff)
  12. EventConsumer (7 topics)
  13. API Handler + `auditPortAdapter`
  14. OutboxPoller + OutboxMetricsCollector
  15. Health Handler (3 core: postgres/redis/rabbitmq, 1 non-core: object_storage)
  16. HTTP servers (API+health на HTTP порту, metrics на отдельном)

- **Shutdown (BRE-019, 8 фаз):** readiness=false → stop outbox poller → stop outbox metrics → close broker → stop HTTP → close Redis → close PostgreSQL → flush observability
- **3 адаптера:**
  - `poolSubscribeAdapter` — broker.Subscribe + pgxpool injection в consumer contexts
  - `poolMiddleware` — pgxpool injection в HTTP request contexts
  - `poolOutboxRepository` — wraps OutboxRepository для non-transactional operations (cleanup, PendingStats)
  - `auditPortAdapter` — bridges AuditRepository (Insert/List) → AuditPort (Record/List)

- **Fixes после code review (code-reviewer + golang-pro):**
  - B-1: `poolOutboxRepository` — предотвращает panic в ConnFromCtx для non-transactional paths
  - B-2: `errors.Is(err, http.ErrServerClosed)` вместо `!=`
  - B-3: compile-time interface checks для всех адаптеров
  - B-4: HTTP startup error detection через errCh
  - B-5: `sync.Once` для safe double-call shutdown
  - W: WriteTimeout/IdleTimeout на HTTP servers

**Проверки:**
- `make build` — OK
- `make test` — ALL PASS (20 packages)
- `make lint` (`go vet ./...`) — OK
- `go test -count=1 -race ./...` — ALL PASS

**Паттерны:**
- Context-based DI: pgxpool.Pool injection через context.Value (postgres.InjectPool)
- DP app.go pattern: thin main() + run() → exit code
- Progressive cleanup: partial startup failure cleans up all opened resources
- sync.Once shutdown: safe from concurrent/double-call

**Следующие задачи (unblocked by DM-TASK-025):**
- DM-TASK-026 (Integration test: DP→DM) — deps: DM-TASK-025 ✅
- DM-TASK-027 (Integration test: full pipeline) — deps: DM-TASK-026
- DM-TASK-028 (Integration test: error scenarios) — deps: DM-TASK-026
- DM-TASK-029 (Dockerfile + Docker Compose) — deps: DM-TASK-025 ✅
- DM-TASK-052 (CLAUDE.md files) — deps: DM-TASK-025 ✅
- DM-TASK-035 (deployment.md) — deps: DM-TASK-029

---

## DM-TASK-036: REV-001/REV-002 — Defensive fallback для version_id и organization_id (2026-04-02)

**Статус:** done

**Что сделано:**
- Создан `DocumentFallbackResolver` port в `internal/domain/port/outbound.go` — `ResolveByDocumentID(ctx, documentID)` возвращает `(orgID, currentVersionID, err)`, cross-tenant lookup
- Создан PostgreSQL адаптер `internal/infra/postgres/fallback_resolver.go` — `FallbackResolver` с `SELECT organization_id, current_version_id FROM documents WHERE document_id = $1` (без WHERE organization_id — TEMPORARY)
- Модифицирован `ArtifactIngestionService`:
  - Добавлены `fallbackResolver` и `fallbackMetrics` зависимости
  - `HandleDPArtifacts` — `resolveDPEventFields()` (single DB call для обоих полей: REV-001 version_id + REV-002 org_id)
  - `HandleLICArtifacts` / `HandleREArtifacts` — `resolveOrgID()` (REV-002 org_id only)
- Модифицирован `DiffStorageService` — добавлен `fallbackResolver`, org_id fallback в `HandleDiffReady`
- Модифицирован `ArtifactQueryService` — добавлен `fallbackResolver`, org_id fallback в `HandleGetSemanticTree` и `HandleGetArtifacts`
- Добавлен `IncMissingVersionID()` в `observability.Metrics` для `dm_missing_version_id_total`
- Обновлён `main.go` — wiring `FallbackResolver` для всех 3 сервисов

**Ревью (code-reviewer):**
- 1 optimization: double DB call → single call при пустых org_id + version_id → ИСПРАВЛЕНО
- 2 warnings accepted (DRY resolveOrgID across packages — acceptable for temporary code; missing org_id metric — not in scope)

**Тесты:**
- 19 новых fallback тестов:
  - Ingestion (8): DP version_id fallback, DP org_id fallback, both empty (single call), resolver error, empty version fallback validation, LIC org_id, RE org_id, no fallback when present
  - Diff (3): org_id fallback, resolver error, no fallback when present
  - Query (4): semantic tree org_id, get artifacts org_id, resolver error, no fallback when present
- Обновлены validation tests: удалены "empty org_id" subtests из diff/query (now resolved by fallback)

**Проверки:**
- `go test -count=1 -race ./...` — ALL PASS (21 пакет)
- `go vet ./...` — OK
- `make build` — OK, `make test` — OK, `make lint` — OK

**Паттерны:**
- TEMPORARY маркировка: все fallback код помечен `// TEMPORARY: remove when DP TASK-056 and TASK-057 are completed`
- Narrow port: `DocumentFallbackResolver` — отдельный интерфейс, не загрязняет `DocumentRepository`
- Single DB call: `resolveDPEventFields` для оптимизации при пустых обоих полях
- Event mutation: exported fields мутируются до вызова validateRequired

**Следующие задачи (critical pending):**
- DM-TASK-037 (BRE-001: SELECT FOR UPDATE на artifact_status) — deps: DM-TASK-017 ✅
- DM-TASK-038 (BRE-003: Idempotency Guard short TTL) — deps: DM-TASK-013 ✅

---

## DM-TASK-037: BRE-001 — SELECT FOR UPDATE на artifact_status (2026-04-02)

**Статус:** done

**Что сделано:**
- **PORT**: добавлен `FindByIDForUpdate` в `VersionRepository` interface (`internal/domain/port/outbound.go`)
  - SELECT ... FOR UPDATE с row-level exclusive lock
  - Должен вызываться внутри транзакции
  - Документация: BRE-001, назначение — сериализация конкурентных artifact_status updates
- **POSTGRES**: `FindByIDForUpdate` в `internal/infra/postgres/version_repository.go`
  - Та же SELECT-структура что и `FindByID`, но с `FOR UPDATE` clause
  - Tenant isolation сохранён (`organization_id = $3`)
  - Reuse `scanVersion` helper
- **INGESTION**: `processIngestion` в `internal/application/ingestion/ingestion.go`
  - Заменён `FindByID` → `FindByIDForUpdate` внутри `WithTransaction`
  - Status transition error → `Retryable: true` (подготовка к DM-TASK-023: NACK with requeue)
  - Комментарий уточнён: текущий consumer drops message, retryable flag для будущей DM-TASK-023
- **MOCK**: `FindByIDForUpdate` добавлен во все 3 mock реализации (ingestion, diff, version)
  - Делегация к `FindByID` по умолчанию (unit-тесты не тестируют реальную блокировку)

**Ревью:**
- code-reviewer → APPROVED with 1 warning
  - W-1: комментарий о NACK → исправлен, указана связь с DM-TASK-023

**Тесты (8 новых + 3 обновлённых):**
- 5 ingestion BRE-001: FindByIDForUpdate call count, all 3 producers (DP/LIC/RE), retryable status transition, error propagation, version not found
- 3 postgres adapter: FOR UPDATE SQL clause verification, not found, DB error
- 3 обновлённых: DP/LIC/RE InvalidStatusTransition → verify `IsRetryable(err) == true`

**Проверки:**
- `go test -count=1 -race ./...` — ALL PASS (21 пакет)
- `go vet ./...` — OK
- `make build` — OK, `make test` — OK, `make lint` — OK

**Паттерны:**
- Отдельный метод `FindByIDForUpdate` (не флаг на `FindByID`) — locking intent explicit
- Retryable status transition → подготовка к DM-TASK-023 (DLQ + NACK backoff)
- Нет риска deadlock: каждая транзакция блокирует ровно 1 строку в document_versions по PK

**Следующие задачи (critical pending):**
- DM-TASK-038 (BRE-003: Idempotency Guard short TTL) — deps: DM-TASK-013 ✅

---

## DM-TASK-038: BRE-003 — Idempotency Guard short TTL + stuck check (2026-04-02)

**Статус:** done (верификация — все criteria реализованы в DM-TASK-013)

**Acceptance Criteria → реализация:**
1. SET PROCESSING с TTL 120s (не 24h) → `SetNX(ctx, newRecord, g.cfg.ProcessingTTL)` в `idempotency.go:120`, `ProcessingTTL` default 120s в `sub_configs.go:175`
2. COMPLETED с TTL 24h → `MarkCompleted` использует `g.cfg.TTL` (24h) в `idempotency.go:171`
3. Stuck PROCESSING ≥ 240s → re-process → `IsStuck(StuckThreshold)` + overwrite в `idempotency.go:147-158`, `StuckThreshold` default 240s
4. COMPLETED → ACK без re-publish (BRE-002) → `ResultSkip` в `idempotency.go:141-143`, consumer `processWithIdempotency` → ACK (always nil)
5. Unit-тесты → 39 тестов в `idempotency_test.go`: `TestCheck_ProcessingStuck_ReturnsReprocess`, `TestCheck_ProcessingFresh_ReturnsSkip`, `TestCheck_Completed_ReturnsSkip`, `TestFullLifecycle_ProcessThenSkipOnRedelivery` и др.

**Проверки:**
- `go test -race -count=1 ./internal/ingress/idempotency/...` — 39 PASS
- `go test -count=1 -race ./...` — ALL PASS (21 пакет)
- `go vet ./...` — OK
- `make build/test/lint` — ALL OK

**Следующие задачи (high priority pending, deps met):**
- DM-TASK-023 (DLQ + backoff) — deps: DM-TASK-014 ✅, DM-TASK-017 ✅
- DM-TASK-039 (BRE-005: FOR UPDATE documents) — deps: DM-TASK-020 ✅
- DM-TASK-030 (Tenant isolation) — deps: DM-TASK-012 ✅, DM-TASK-014 ✅, DM-TASK-022 ✅
- DM-TASK-026 (Integration test DP→DM) — deps: DM-TASK-025 ✅

---

## DM-TASK-039: BRE-005 — SELECT FOR UPDATE на documents при создании версии (2026-04-02)

**Статус:** done

**Что сделано:**
- Добавлен `FindByIDForUpdate` в `DocumentRepository` interface (port/outbound.go) — SELECT ... FOR UPDATE с row-level exclusive lock
- Реализован `FindByIDForUpdate` в `postgres/document_repository.go` — SELECT с FOR UPDATE clause, tenant isolation, shared scanDocument helper
- Обновлён `VersionManagementService.createVersionInTx()` — заменён `FindByID` → `FindByIDForUpdate` внутри транзакции
  - Сериализация конкурентных создателей версий: NextVersionNumber (MAX+1) и current_version_id update выполняются под lock на document row
  - Retry loop (3 попытки) сохранён как defense-in-depth
- Обновлены mocks в `version_test.go` (с forUpdateCallCount tracking) и `lifecycle_test.go` (delegate to FindByID)

**Тесты (8 новых + 1 обновлённый):**
- 3 postgres adapter: FOR UPDATE SQL clause verification, not found, DB error
- 5 version service BRE-005: UsesFindByIDForUpdate, ForUpdateErrorPropagates, ForUpdateNotFound, RetryStillUsesForUpdate, AllOriginTypesCallForUpdate (включая RE_CHECK)
- 1 обновлённый: DocRefetchedInsideTxOnRetry → verifies forUpdateCallCount

**Ревью:**
- golang-pro → APPROVED plan
- code-reviewer → APPROVED, 0 blocking, 3 warnings исправлено (defense-in-depth комментарий, RE_CHECK в AllOriginTypes тесте)

**Проверки:**
- `go test -count=1 -race ./...` — ALL PASS (21 пакет)
- `go vet ./...` — OK
- `make build/test/lint` — ALL OK

**Следующие задачи (high priority pending, deps met):**
- DM-TASK-023 (DLQ + replay + backoff) — deps: DM-TASK-014 ✅, DM-TASK-017 ✅
- DM-TASK-030 (Tenant isolation enforcement) — deps: DM-TASK-012 ✅, DM-TASK-014 ✅, DM-TASK-022 ✅
- DM-TASK-026 (Integration test DP→DM) — deps: DM-TASK-025 ✅
- DM-TASK-041 (Stale Version Watchdog) — deps: DM-TASK-017 ✅, DM-TASK-016 ✅
- DM-TASK-042 (BRE-006 Outbox Poller) — deps: DM-TASK-016 ✅

---

## DM-TASK-023: DLQ — отправка необработанных сообщений + replay + backoff (2026-04-03)

**Статус:** done

**Что сделано:**

Реализована полная DLQ (Dead Letter Queue) система для Document Management:

1. **Доменная модель** (`internal/domain/model/dlq.go`):
   - `DLQCategory` enum: `ingestion`, `query`, `invalid` — определяет целевой DLQ-топик
   - `Category` field добавлен в `DLQRecord`
   - `DLQRecordWithMeta` struct — расширение для replay tracking: ID, ReplayCount, LastReplayedAt, CreatedAt

2. **Порты** (`internal/domain/port/outbound.go`):
   - `DLQRepository` interface: Insert, FindByFilter, IncrementReplayCount
   - `DLQFilterParams`: фильтрация по category, correlation_id, max_replay, limit

3. **Конфигурация** (`internal/config/`):
   - `DLQConfig` struct с `DM_DLQ_MAX_REPLAY_COUNT` (default 3, BRE-011)
   - Добавлен в корневой Config struct

4. **PostgreSQL миграция** (`migrations/000002_dlq_records.up.sql`):
   - Таблица `dm_dlq_records` с 12 колонками: id (UUID PK), original_topic, original_message (JSONB), error_code, error_message, correlation_id, job_id, category, failed_at, replay_count (default 0), last_replayed_at, created_at
   - 3 индекса: correlation_id (partial), category, created_at

5. **PostgreSQL DLQ Repository** (`internal/infra/postgres/dlq_repository.go`):
   - Insert — сохранение DLQ записи
   - FindByFilter — динамический WHERE + ORDER BY created_at DESC + LIMIT
   - IncrementReplayCount — атомарный UPDATE replay_count + last_replayed_at

6. **DLQ Sender** (`internal/egress/dlq/sender.go`):
   - Реализует `port.DLQPort`
   - Dual-write pattern: DB persist (replay source of truth) → broker publish (alerting/monitoring)
   - `resolveTopic` по DLQCategory: ingestion → dm.dlq.ingestion-failed, query → dm.dlq.query-failed, invalid → dm.dlq.invalid-message
   - Consumer-side interfaces: BrokerPublisher, DLQMetrics, Logger
   - Compile-time check: `var _ port.DLQPort = (*Sender)(nil)`
   - DB insert failure не блокирует broker publish; broker publish failure не фатальна (logged)

7. **Observability** (`internal/infra/observability/metrics.go`):
   - `IncDLQMessages(reason string)` helper method для dm_dlq_messages_total counter

8. **Consumer Integration** (`internal/ingress/consumer/consumer.go`):
   - Добавлены deps: `dlq port.DLQPort`, `retryCfg config.RetryConfig`
   - `dlqContext` struct: category, rawBody, correlationID, jobID
   - Все 7 per-topic handlers обновлены:
     - Invalid JSON → `DLQCategoryInvalid` + sendToDLQ
     - Validation failure → `DLQCategoryInvalid` + sendToDLQ
     - Missing required fields (version_id, base/target) → `DLQCategoryInvalid` + sendToDLQ
   - `processWithIdempotency` обновлён:
     - Non-retryable errors → sendToDLQ (immediate DLQ)
     - Retryable errors → applyBackoff (BRE-025, no DLQ)
   - `sendToDLQ` helper: строит DLQRecord из dlqContext + error, publishes via DLQ port
   - `applyBackoff`: context-aware time.Sleep с BackoffBase delay

9. **DLQ Replay Admin Endpoint** (`internal/ingress/api/handler.go`):
   - `POST /api/v1/admin/dlq/replay` — admin-only endpoint (REV-018, BRE-011)
   - `WithDLQReplay(repo, broker, maxReplay)` — optional setup (disabled if deps nil)
   - Request: `category` (optional), `correlation_id` (optional), `limit` (default 10, max 100)
   - Response: `replayed`, `skipped`, `errors`
   - Flow: FindByFilter → skip if replay_count >= max → Publish to original_topic → IncrementReplayCount
   - Category validation: only ingestion/query/invalid accepted

10. **Application Wiring** (`cmd/dm-service/main.go`):
    - DLQRepository + poolDLQRepository adapter (pool injection)
    - DLQ Sender wiring с 3 topic names из config
    - Consumer получает dlqSender + cfg.Retry
    - apiHandler.WithDLQReplay(poolDLQRepo, brokerClient, cfg.DLQ.MaxReplayCount)
    - Compile-time check: `var _ port.DLQRepository = (*poolDLQRepository)(nil)`

**Тесты (44 новых):**
- DLQ Sender: 13 тестов (7 constructor panics, success, 4 category routing, DB error continues, broker error non-fatal, JSON round-trip, context cancelled)
- DLQ Repository: 5 тестов (interface compliance, constructor, 3 panic-on-no-pool)
- Consumer DLQ: 15 тестов (invalid JSON→DLQ, validation→DLQ, non-retryable→DLQ, retryable→no DLQ, query category semantic tree, query category get artifacts, original message preserved, diff invalid JSON, LIC non-retryable, RE non-retryable, DLQ send error logged, missing version_id, missing base/target version_id, all topics invalid JSON→DLQ from previous tests updated)
- Backoff: 3 теста (retryable applies delay, context cancelled skips delay, zero backoff no delay)
- API Replay: 8 тестов (happy path, max replay exceeded, invalid JSON, invalid category, publish error, DB error, correlation_id filter, not enabled without deps)
- Config: 1 новый тест (DLQ.MaxReplayCount default)

**Проверки:**
- `go test -count=1 -race ./...` — ALL PASS (21 пакет)
- `go vet ./...` — OK
- `make build` — OK, `make test` — OK, `make lint` — OK

**Ключевые решения:**
- DLQ topic routing через DLQCategory enum (explicit at call site, not inferred by sender)
- Dual-write: DB первый (source of truth для replay), broker второй (alerting); ни одна ошибка не фатальна
- Backoff: fixed delay (BackoffBase, default 1s) вместо exponential per-message — consumer всегда ACK, нет per-message retry counter
- Replay через PostgreSQL (не Redis) — records survive TTL expiration (BRE-011)
- Max replay count = 3 для защиты от бесконечного цикла (REV-018)
- Consumer всегда возвращает nil (ACK) — DLQ publish происходит до return nil (at-least-once DLQ semantics)

**Следующие задачи (high priority pending, deps met):**
- DM-TASK-024 (Audit Trail) — deps: DM-TASK-012 ✅, DM-TASK-022 ✅
- DM-TASK-026 (Integration test DP→DM) — deps: DM-TASK-025 ✅
- DM-TASK-030 (Tenant isolation enforcement) — deps: DM-TASK-012 ✅, DM-TASK-014 ✅, DM-TASK-022 ✅
- DM-TASK-040 (REV-005 Archive endpoint) — deps: DM-TASK-022 ✅
- DM-TASK-041 (Stale Version Watchdog) — deps: DM-TASK-017 ✅, DM-TASK-016 ✅

---

## DM-TASK-026: Integration test DP→DM (end-to-end с in-memory fakes) (2026-04-03)

**Статус:** done

**Что сделано:**
- Создан `internal/integration/` с end-to-end integration тестами
- `testinfra.go` (~900 строк): 14 in-memory fakes реализующих все outbound ports:
  - `memoryTransactor` с txCallCount tracking для проверки transactional intent
  - `memoryDocumentRepository`, `memoryVersionRepository` (с FindByIDForUpdate)
  - `memoryArtifactRepository`, `memoryAuditRepository` (append-only)
  - `memoryOutboxRepository` с PendingStats
  - `memoryObjectStorage` (PutObject/GetObject/HeadObject/DeleteByPrefix)
  - `memoryIdempotencyStore` (с SetNX)
  - `memoryDiffRepository`, `recordingDLQPort`, `memoryFallbackResolver`
  - `captureBroker`, `noopConfirmationPublisher`, `recordingLogger`, noop metrics
  - `testHarness` wires real services: ArtifactIngestionService, ArtifactQueryService, DiffStorageService, OutboxWriter, IdempotencyGuard
  - Helpers: `defaultDocument`, `defaultVersion`, `defaultDPEvent`
- `dp_ingestion_test.go` (~650 строк): 14 integration тестов:
  1. `TestDPIngestion_HappyPath` — full pipeline: 4 artifacts → blobs + descriptors + status + audit + outbox
  2. `TestDPIngestion_WithWarnings` — 5 artifacts including warnings
  3. `TestDPIngestion_ContentHashIntegrity` — SHA-256 blob↔descriptor match
  4. `TestDPIngestion_IdempotencyDedup` — Check→Process→MarkCompleted→Check→Skip
  5. `TestDPIngestion_VersionNotFound` — error, no side effects
  6. `TestDPIngestion_FallbackVersionID` — REV-001+REV-002 fallback
  7. `TestDPIngestion_OutboxAggregateID` — REV-010 FIFO: aggregate_id=version_id
  8. `TestDPIngestion_CompensationOnTxFailure` — terminal status → blob compensation
  9. `TestDPIngestion_ContextCancelled` — no side effects on cancel
  10. `TestDPIngestion_AuditDetails` — ARTIFACT_SAVED + ARTIFACT_STATUS_CHANGED details
  11. `TestDPIngestion_StorageKeyConvention` — org/doc/ver/type naming
  12. `TestDPIngestion_FallbackDocumentNotFound` — fallback not found error
  13. `TestDPIngestion_TransactionalIntent` — WithTransaction called exactly once
  14. `TestDPIngestion_EndToEndIdempotency` — guard+handler integration

**Code Review (code-reviewer):** 3B + 5W → все исправлено:
- B-1: txCallCount tracking для transactor
- B-2: getBlob returns copy (не alias)
- B-3: EndToEndIdempotency тест (guard+handler)
- W-1: make() для slice filter patterns
- W-2: strings.Contains вместо manual substring
- W-3: FallbackDocumentNotFound тест
- W-4: removed unused waitForCondition
- W-5: ContextCancelled asserts no side effects

**Проверки:**
- `go test -count=1 -race ./...` — ALL PASS (22 пакета)
- `go vet ./...` — OK
- `make build/test/lint` — ALL OK

**Следующие задачи (high priority pending, deps met):**
- DM-TASK-024 (Audit Trail) — deps: DM-TASK-012 ✅, DM-TASK-022 ✅
- DM-TASK-028 (Integration test error scenarios) — deps: DM-TASK-026 ✅
- DM-TASK-029 (Dockerfile + Docker Compose) — deps: DM-TASK-025 ✅
- DM-TASK-030 (Tenant isolation enforcement) — deps: DM-TASK-012 ✅, DM-TASK-014 ✅, DM-TASK-022 ✅
- DM-TASK-040 (REV-005 Archive endpoint) — deps: DM-TASK-022 ✅
- DM-TASK-041 (Stale Version Watchdog) — deps: DM-TASK-017 ✅, DM-TASK-016 ✅

---

## DM-TASK-027: Integration test — полный pipeline (DP → LIC → RE) с version lifecycle (2026-04-03)

**Статус:** done

**Что сделано:**
- Создан `internal/integration/full_pipeline_test.go` с 10 integration тестами
- Расширен `internal/integration/testinfra.go` с новой инфраструктурой:
  - `recordingConfirmationPublisher` — captures SemanticTreeProvided + ArtifactsProvided events
  - `newTestHarnessWithRecordingPublisher` — harness variant с recording publisher для query тестов
  - `defaultLICEvent` — factory для 8-артефактного LIC события
  - `defaultREEvent` — factory для RE события с pre-seeded blobs (claim-check pattern)
- Тесты полного pipeline:
  - `TestFullPipeline_DPtoLICtoRE_FullyReady` — 3 стадии: PENDING→PROCESSING→ANALYSIS→FULLY_READY, 14 artifacts, 6 outbox в правильном порядке, 6 audit, correlation_id propagation
  - `TestFullPipeline_ListArtifactsAtEachStage` — sync API progressive: 0→4→12→14 artifacts, producer domain verification
  - `TestFullPipeline_AuditTrailIntegrity` — 3 status transitions from/to, actor_id per producer
  - `TestFullPipeline_OutOfOrder_LICBeforeDP_Fails` — state machine enforcement, no side effects
  - `TestFullPipeline_DuplicateDP_AfterLIC_Fails` — backward transition rejected
- Тесты query service:
  - `TestGetSemanticTree_HappyPath` — content match, empty errors, async audit ARTIFACT_READ
  - `TestGetSemanticTree_NotFound` — error fields populated, nil tree
  - `TestGetArtifacts_HappyPath_AllFound` — 2 artifacts from different producers, content match
  - `TestGetArtifacts_PartialResponse` — 1 found + 2 missing types
  - `TestGetArtifacts_AllMissing` — 0 artifacts + all missing

**Проверки:**
- `go test ./internal/integration/... -race -count=1` — 24 PASS (14 existing + 10 new)
- `go test -count=1 -race ./...` — ALL PASS (22 пакета)
- `go vet ./...` — OK
- `make build` — OK
- `make test` — OK
- `make lint` — OK

**Паттерны:**
- Recording publisher для capture published events (vs noop)
- Claim-check pattern testing (RE pre-seeds blobs → event has BlobReference)
- Progressive assertions — verify state at each pipeline stage
- Async audit verification with time.Sleep(100ms) for goroutine completion

---

## DM-TASK-028: Integration test — error scenarios (2026-04-03)

**Статус:** done

**Что сделано:**
- Создан `internal/integration/error_scenarios_test.go` (569 строк, 5 тестов + 3 test-local fake типа)
- Улучшена test infrastructure в `testinfra.go` (4 fixes для корректной симуляции concurrent DB behavior)

**Тесты:**
1. `TestErrorScenario_ObjectStorageFailOnFourthArtifact_CompensationAndRetry` — Object Storage fail на 4-м артефакте → compensation → retry → success
2. `TestErrorScenario_ConcurrentVersionCreation_BothSucceed` — 2 goroutines с sync barrier → оба succeed с version_numbers {1, 2}
3. `TestErrorScenario_DocumentNotFound_NoBlobsNoDescriptors` — not-found error, 0 side effects
4. `TestErrorScenario_RedisUnavailable_FallbackToDB_Success` — failing Redis → DB fallback → success
5. `TestErrorScenario_TerminalStatus_StatusTransitionError_CompensationRuns` — FULLY_READY → error + compensation

**Test-local fake types:** failingObjectStorage, failingIdempotencyStore, conflictingVersionRepository

**Testinfra fixes (code-reviewer 3B):**
- `memoryTransactor.WithTransaction` serializes via `txMu`
- `FindByIDForUpdate` returns shallow copy (prevents data race)
- `memoryVersionRepository.Insert` checks version_number uniqueness
- `sha256HexHelper` moved to testinfra.go from dp_ingestion_test.go

**Проверки:** go test -count=1 -race ALL PASS (22 пакета), go vet OK, make build/test/lint OK

**Следующие задачи (high priority pending, deps met):**
- DM-TASK-024 (Audit Trail) — deps: DM-TASK-012 ✅, DM-TASK-022 ✅
- DM-TASK-029 (Dockerfile + Docker Compose) — deps: DM-TASK-025 ✅
- DM-TASK-030 (Tenant isolation enforcement) — deps: DM-TASK-012 ✅, DM-TASK-014 ✅, DM-TASK-022 ✅
- DM-TASK-040 (REV-005 Archive endpoint) — deps: DM-TASK-022 ✅
- DM-TASK-041 (Stale Version Watchdog) — deps: DM-TASK-017 ✅, DM-TASK-016 ✅

---

## DM-TASK-024: Audit Trail — запись и чтение audit records (2026-04-03)

**Статус:** done

**Что сделано:**
- Верифицирован и дополнен audit trail для Document Management
- Добавлен `requireRole` middleware в `internal/ingress/api/auth.go`:
  - Map-based O(1) lookup, case-sensitive by design (API Gateway normalizes)
  - Fail-closed для nil/empty/unknown role → 403 FORBIDDEN
  - Не утекает информация о допустимых ролях
- Audit endpoint GET /api/v1/audit защищён `requireRole("admin", "auditor")`
- DLQ replay POST /api/v1/admin/dlq/replay защищён `requireRole("admin")`
- Добавлен `actor_type` query parameter для audit endpoint
- Добавлена валидация `action` и `actor_type` enum-значений (isValidAuditAction, isValidActorType) → 400 при невалидных значениях
- Все 8 action types записываются через 5 application services (verified)
- Append-only: AuditRepository interface только Insert+List, нет Update/Delete

**Новые тесты:**
- 18 новых API тестов: 6 role middleware + 2 invalid enum + 10 audit endpoint filters
- 7 integration тестов в `audit_trail_test.go`:
  - AllActionTypes — полная верификация 7 records (3×ARTIFACT_SAVED + 3×STATUS_CHANGED + 1×DIFF_SAVED)
  - ArtifactSavedDetails — проверка details JSON (producer, artifact_types, count)
  - StatusChangedDetails — проверка from/to
  - AsyncArtifactRead_SemanticTree — polling-based wait, ActorID=DP
  - AsyncArtifactRead_GetArtifacts_LIC — ActorID=RE (LIC artifacts → requester RE)
  - DiffSavedDetails — base/target version IDs, storage_key, content_hash
  - AppendOnly — compile-time + behavioral verification
- Обновлены DLQ replay тесты (добавлен X-User-Role: admin)

**Code review:** code-reviewer → 0B + 9W, 5 warnings исправлены (map lookup, case-sensitive doc, enum validation, response body check, polling вместо sleep)

**Проверки:** go test -count=1 -race ALL PASS (22 пакета), go vet OK, make build/test/lint OK

**Следующие задачи (high priority pending, deps met):**
- DM-TASK-030 (Tenant isolation enforcement) — deps: DM-TASK-012 ✅, DM-TASK-014 ✅, DM-TASK-022 ✅
- DM-TASK-040 (REV-005 Archive endpoint) — deps: DM-TASK-022 ✅
- DM-TASK-041 (Stale Version Watchdog) — deps: DM-TASK-017 ✅, DM-TASK-016 ✅
- DM-TASK-042 (Outbox Poller FIFO ordering) — deps: DM-TASK-016 ✅
- DM-TASK-043 (Consumer backpressure) — deps: DM-TASK-007 ✅
- DM-TASK-044 (Circuit breaker Object Storage) — deps: DM-TASK-008 ✅
- DM-TASK-045 (Rate limiting API) — deps: DM-TASK-022 ✅

---

## DM-TASK-029: Dockerfile (multi-stage) + Docker Compose (2026-04-03)

**Статус:** done

**Что сделано:**
- Создан `Dockerfile` (multi-stage build):
  - Stage 1 (builder): `golang:1.26.1-alpine`, layer-cached `go mod download`, CGO_ENABLED=0 static build
  - Stage 2 (runtime): `alpine:3.21`, non-root user `dmservice`, HEALTHCHECK `/readyz`, EXPOSE 8080+9090
  - Паттерн идентичен DP Dockerfile (только binary name и user name отличаются)
  - Миграции embedded через `//go:embed` — не нужно COPY отдельно
- Создан `docker-compose.yaml` (6 сервисов):
  - `postgres:16-alpine` (dm-postgres, host port 5433)
  - `redis:7-alpine` (dm-redis, host port 6380)
  - `rabbitmq:3-management-alpine` (dm-rabbitmq, host ports 5673/15673)
  - `minio/minio` (dm-minio, ports 9000/9001) + `minio-init` (bucket creation)
  - `dm-service` (build from context, host ports 8081/9091)
  - Port offset scheme для избежания конфликтов с DP stack
  - Все infra-сервисы с healthchecks, `depends_on` с conditions
- Создан `.env.example` — все DM_ переменные, grouped по категориям
- Создан `.dockerignore` — aligned с DP reference
- Обновлён `Makefile` — добавлены `compose-up`, `compose-down` targets

**Проверки:**
- `make build` — OK
- `make test` (`go test ./...`) — OK, 22 пакета
- `make lint` (`go vet ./...`) — OK
- `go test -count=1 -race ./...` — ALL PASS
- `make docker-build` — Docker не установлен на машине, не тестировался

**Code Review (code-reviewer agent):**
- 1 Blocking: MinIO healthcheck `mc ready local` → заменён на `curl -f http://localhost:9000/minio/health/live`
- 15 Warnings: 6 исправлены (.dockerignore aligned, Redis start_period, DM_STORAGE_REGION documented, broker topics section, DEV ONLY comment)

**Следующие задачи (ready):**
- DM-TASK-030 (Tenant isolation) — security, high priority
- DM-TASK-040 (Archive endpoint) — functional, high priority
- DM-TASK-041..045 — functional/security, high priority

---
