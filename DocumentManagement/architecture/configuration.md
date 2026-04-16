# Конфигурация Document Management

Сервис Document Management настраивается через переменные окружения с префиксом `DM_`.

Реализация: `internal/config/` — `Load()` загружает `.env` файл (если есть), читает env, применяет значения по умолчанию, валидирует обязательные поля. При невалидной конфигурации `Load()` возвращает ошибку со списком всех проблем.

### Загрузка .env файла

При локальной разработке можно создать файл `.env` в директории запуска сервиса (как правило — `DocumentManagement/development/`). `Load()` автоматически прочитает его при запуске. Уже установленные переменные окружения **имеют приоритет** над значениями из `.env` — файл не перезаписывает явно заданные переменные.

В продакшене `.env` файл не нужен — переменные передаются через Docker/K8s (env_file, ConfigMap, Secret).

---

## Содержание

1. [Обязательные переменные](#обязательные-переменные)
2. [Инфраструктура](#инфраструктура)
   - [PostgreSQL Database](#postgresql-database)
   - [Message Broker (RabbitMQ)](#message-broker-rabbitmq)
   - [Object Storage (S3)](#object-storage-s3-compatible)
   - [KV Store (Redis)](#kv-store-redis)
   - [HTTP-сервер](#http-сервер)
3. [Приём и обработка событий](#приём-и-обработка-событий)
   - [Consumer](#consumer)
   - [Ingestion (валидация артефактов)](#ingestion-валидация-артефактов)
   - [Idempotency Guard](#idempotency-guard)
   - [Retry](#retry)
   - [Timeouts](#timeouts)
4. [Устойчивость](#устойчивость)
   - [Circuit Breaker (Object Storage)](#circuit-breaker-object-storage)
   - [Rate Limiting (sync API)](#rate-limiting-sync-api)
   - [Dead Letter Queue](#dead-letter-queue)
5. [Фоновые задачи](#фоновые-задачи)
   - [Transactional Outbox](#transactional-outbox)
   - [Retention (очистка данных)](#retention-очистка-данных)
   - [Stale Version Watchdog](#stale-version-watchdog)
   - [Orphan Cleanup](#orphan-cleanup)
6. [Observability](#observability)
7. [Топики брокера сообщений](#топики-брокера-сообщений)
8. [Пример .env файла](#пример-env-файла)

---

## Обязательные переменные

При отсутствии любой из этих переменных сервис не запустится и выведет ошибку со списком всех недостающих параметров.

| Переменная | Описание | Пример |
|-----------|----------|--------|
| `DM_DB_DSN` | PostgreSQL connection string | `postgres://dm:pass@localhost:5433/dm_dev?sslmode=disable` |
| `DM_BROKER_ADDRESS` | Адрес RabbitMQ | `amqp://guest:guest@localhost:5672/` |
| `DM_STORAGE_ENDPOINT` | Endpoint Object Storage (S3-compatible) | `http://localhost:9000` |
| `DM_STORAGE_BUCKET` | Имя бакета для артефактов | `dm-artifacts` |
| `DM_STORAGE_ACCESS_KEY` | Access Key для Object Storage | — |
| `DM_STORAGE_SECRET_KEY` | Secret Key для Object Storage | — |
| `DM_KVSTORE_ADDRESS` | Адрес Redis | `localhost:6380` |

**Ограничения (проверяются при старте):**
- `DM_HTTP_PORT` и `DM_METRICS_PORT` не должны совпадать
- `DM_CONSUMER_CONCURRENCY` и `DM_CONSUMER_PREFETCH` >= 1
- Все duration/size значения для circuit breaker, ingestion, orphan cleanup, retention должны быть положительными
- При включённом rate limiting (`DM_RATELIMIT_ENABLED=true`): `DM_RATELIMIT_READ_RPS` и `DM_RATELIMIT_WRITE_RPS` > 0

---

## Инфраструктура

### PostgreSQL Database

| Переменная | Описание | По умолчанию | Заметки |
|-----------|----------|-------------|---------|
| `DM_DB_DSN` | Connection string | — (required) | Формат: `postgres://user:pass@host:port/db?sslmode=...` |
| `DM_DB_MAX_CONNS` | Макс. соединений в пуле | `25` | BRE-012: раздельные пулы для API и consumer |
| `DM_DB_MIN_CONNS` | Мин. idle соединений | `5` | |
| `DM_DB_QUERY_TIMEOUT` | Таймаут одного SQL-запроса | `10s` | |

### Message Broker (RabbitMQ)

| Переменная | Описание | По умолчанию | Заметки |
|-----------|----------|-------------|---------|
| `DM_BROKER_ADDRESS` | Адрес RabbitMQ | — (required) | `amqp://` или `amqps://` |
| `DM_BROKER_TLS` | Включить TLS | `false` | NFR-3.2: `true` в продакшене, MinVersion TLS 1.2 |

Топики RabbitMQ (25 переменных) настраиваются отдельно — см. раздел [Топики брокера сообщений](#топики-брокера-сообщений).

### Object Storage (S3-compatible)

| Переменная | Описание | По умолчанию | Заметки |
|-----------|----------|-------------|---------|
| `DM_STORAGE_ENDPOINT` | Endpoint S3 | — (required) | MinIO: `http://localhost:9000`, prod: `https://storage.yandexcloud.net` |
| `DM_STORAGE_BUCKET` | Имя бакета | — (required) | |
| `DM_STORAGE_ACCESS_KEY` | Access Key | — (required) | |
| `DM_STORAGE_SECRET_KEY` | Secret Key | — (required) | |
| `DM_STORAGE_REGION` | Регион | `ru-central1` | MinIO: `us-east-1` |
| `DM_STORAGE_PRESIGNED_TTL` | TTL presigned URL | `5m` | API-endpoint `/artifacts/{type}` для blob-артефактов возвращает 302 с presigned URL |

### KV Store (Redis)

| Переменная | Описание | По умолчанию | Заметки |
|-----------|----------|-------------|---------|
| `DM_KVSTORE_ADDRESS` | Адрес Redis | — (required) | |
| `DM_KVSTORE_PASSWORD` | Пароль | _(пусто)_ | |
| `DM_KVSTORE_DB` | Номер базы данных | `0` | |
| `DM_KVSTORE_POOL_SIZE` | Размер connection pool | `10` | |
| `DM_KVSTORE_TIMEOUT` | Таймаут операций | `2s` | |

### HTTP-сервер

| Переменная | Описание | По умолчанию | Заметки |
|-----------|----------|-------------|---------|
| `DM_HTTP_PORT` | Порт HTTP API + health probes | `8080` | Не должен совпадать с `DM_METRICS_PORT` |

---

## Приём и обработка событий

### Consumer

| Переменная | Описание | По умолчанию | Заметки |
|-----------|----------|-------------|---------|
| `DM_CONSUMER_PREFETCH` | RabbitMQ QoS prefetch count | `10` | BRE-007: сколько сообщений RabbitMQ доставляет до ACK |
| `DM_CONSUMER_CONCURRENCY` | Макс. параллельных обработчиков | `5` | BRE-007: semaphore-based limiter. WARN при prefetch < concurrency |

### Ingestion (валидация артефактов)

| Переменная | Описание | По умолчанию | Заметки |
|-----------|----------|-------------|---------|
| `DM_INGESTION_MAX_JSON_BYTES` | Макс. размер JSON-артефакта в байтах | `10485760` (10 МБ) | BRE-029: размер проверяется ДО json.Valid() |
| `DM_INGESTION_MAX_BLOB_SIZE_BYTES` | Макс. размер blob-артефакта в байтах | `104857600` (100 МБ) | BRE-029: для RE claim-check artifacts |

### Idempotency Guard

| Переменная | Описание | По умолчанию | Заметки |
|-----------|----------|-------------|---------|
| `DM_IDEMPOTENCY_TTL` | TTL ключа в статусе COMPLETED | `24h` | |
| `DM_IDEMPOTENCY_PROCESSING_TTL` | TTL ключа в статусе PROCESSING | `120s` | BRE-003: короткий TTL для PROCESSING |
| `DM_IDEMPOTENCY_STUCK_THRESHOLD` | Возраст PROCESSING для re-process | `240s` | BRE-003: 2 × processing TTL, stuck key → delete + re-process |

### Retry

| Переменная | Описание | По умолчанию | Заметки |
|-----------|----------|-------------|---------|
| `DM_RETRY_MAX_ATTEMPTS` | Макс. попыток при retryable ошибках | `3` | |
| `DM_RETRY_BACKOFF_BASE` | Базовый интервал backoff | `1s` | BRE-025: client-side delay перед NACK |

### Timeouts

| Переменная | Описание | По умолчанию | Заметки |
|-----------|----------|-------------|---------|
| `DM_TIMEOUT_STORAGE_PUT` | Таймаут PutObject | `30s` | |
| `DM_TIMEOUT_STORAGE_GET` | Таймаут GetObject | `15s` | |
| `DM_TIMEOUT_EVENT_PROCESSING` | Общий таймаут обработки одного события | `60s` | |
| `DM_TIMEOUT_BROKER_PUBLISH` | Таймаут публикации в broker | `10s` | |
| `DM_STALE_VERSION_TIMEOUT` | Legacy fallback для per-stage таймаутов watchdog (см. ниже) | `—` (если не задан — каждый per-stage берёт собственный default) | DM-TASK-053: больше не используется напрямую watchdog'ом; сохранён как per-variable fallback для 4 новых переменных. См. секцию «Stale Version Watchdog». |
| `DM_SHUTDOWN_TIMEOUT` | Таймаут graceful shutdown | `30s` | BRE-019: ordered teardown 8 фаз |

---

## Устойчивость

### Circuit Breaker (Object Storage)

| Переменная | Описание | По умолчанию | Заметки |
|-----------|----------|-------------|---------|
| `DM_CB_MAX_REQUESTS` | Запросов в half-open для решения open/close | `3` | |
| `DM_CB_INTERVAL` | Период сброса счётчика ошибок (closed state) | `60s` | 0 = счётчик не сбрасывается до открытия |
| `DM_CB_TIMEOUT` | Длительность open state до перехода в half-open | `30s` | |
| `DM_CB_FAILURE_THRESHOLD` | Последовательных ошибок для срабатывания | `5` | BRE-014: context errors и non-retryable ошибки не считаются |
| `DM_CB_PER_EVENT_BUDGET` | Бюджет времени на S3 вызовы в рамках одного события | `35s` | BRE-014: предотвращает 5 × 3 × 30s = 7.5 мин ожидания |

### Rate Limiting (sync API)

| Переменная | Описание | По умолчанию | Заметки |
|-----------|----------|-------------|---------|
| `DM_RATELIMIT_ENABLED` | Включить rate limiting | `true` | |
| `DM_RATELIMIT_READ_RPS` | Лимит чтения (GET/HEAD) на организацию | `100` | BRE-009: per-organization token bucket |
| `DM_RATELIMIT_WRITE_RPS` | Лимит записи (POST/PUT/DELETE) на организацию | `20` | BRE-009: per-organization token bucket |
| `DM_RATELIMIT_CLEANUP_INTERVAL` | Период GC неактивных org entries | `5m` | |
| `DM_RATELIMIT_IDLE_TTL` | TTL неактивной организации перед eviction | `10m` | |

### Dead Letter Queue

| Переменная | Описание | По умолчанию | Заметки |
|-----------|----------|-------------|---------|
| `DM_DLQ_MAX_REPLAY_COUNT` | Макс. повторов через admin replay API | `3` | BRE-011: защита от бесконечного цикла |

---

## Фоновые задачи

### Transactional Outbox

| Переменная | Описание | По умолчанию | Заметки |
|-----------|----------|-------------|---------|
| `DM_OUTBOX_POLL_INTERVAL` | Интервал опроса outbox_events | `200ms` | |
| `DM_OUTBOX_BATCH_SIZE` | Макс. записей за один poll | `50` | BRE-006: SELECT FOR UPDATE SKIP LOCKED |
| `DM_OUTBOX_LOCK_TIMEOUT` | Таймаут блокировки строк | `5s` | |
| `DM_OUTBOX_CLEANUP_HOURS` | Удаление CONFIRMED записей старше N часов | `48` | BRE-018: DELETE LIMIT 1000 партициями |

### Retention (очистка данных)

| Переменная | Описание | По умолчанию | Заметки |
|-----------|----------|-------------|---------|
| `DM_RETENTION_ARCHIVE_DAYS` | Период хранения archived документов | `90` | S3 lifecycle rule (документировано) |
| `DM_RETENTION_DELETED_BLOB_DAYS` | Удаление S3 blobs для DELETED документов | `30` | Background job |
| `DM_RETENTION_DELETED_META_DAYS` | Hard delete метаданных DELETED документов | `365` | Cascade: versions → artifacts → diffs → audit → doc |
| `DM_RETENTION_AUDIT_DAYS` | Период хранения audit records | `1095` (3 года) | REV-027: drop monthly partitions |
| `DM_RETENTION_BLOB_SCAN_INTERVAL` | Интервал сканирования blob cleanup | `6h` | |
| `DM_RETENTION_META_SCAN_INTERVAL` | Интервал сканирования meta cleanup | `24h` | |
| `DM_RETENTION_AUDIT_SCAN_INTERVAL` | Интервал сканирования audit partition mgmt | `24h` | |
| `DM_RETENTION_BATCH_SIZE` | Документов за один scan cycle | `50` | |
| `DM_RETENTION_SCAN_TIMEOUT` | Таймаут одного scan цикла | `300s` | |
| `DM_RETENTION_AUDIT_MONTHS_AHEAD` | Создавать партиции на N месяцев вперёд | `3` | |

### Stale Version Watchdog

| Переменная | Описание | По умолчанию | Заметки |
|-----------|----------|-------------|---------|
| `DM_WATCHDOG_SCAN_INTERVAL` | Интервал сканирования stale версий | `5m` | REV-008: переводит в PARTIALLY_AVAILABLE. Worst-case lag детекции = `timeout + scan interval`. |
| `DM_WATCHDOG_BATCH_SIZE` | Макс. версий за один scan | `100` | |
| `DM_STALE_TIMEOUT_PROCESSING` | Таймаут для перехода `PENDING → PROCESSING_ARTIFACTS_RECEIVED` | `5m` | DM-TASK-053 / ASSUMPTION-ORCH-14. Fallback: `DM_STALE_VERSION_TIMEOUT` → per-stage default. |
| `DM_STALE_TIMEOUT_ANALYSIS` | Таймаут для перехода `PROCESSING_ARTIFACTS_RECEIVED → ANALYSIS_ARTIFACTS_RECEIVED` | `10m` | DM-TASK-053 / ASSUMPTION-ORCH-14. Fallback: `DM_STALE_VERSION_TIMEOUT` → per-stage default. |
| `DM_STALE_TIMEOUT_REPORTS` | Таймаут для перехода `ANALYSIS_ARTIFACTS_RECEIVED → REPORTS_READY` | `5m` | DM-TASK-053 / ASSUMPTION-ORCH-14. Fallback: `DM_STALE_VERSION_TIMEOUT` → per-stage default. |
| `DM_STALE_TIMEOUT_FINALIZATION` | Таймаут для перехода `REPORTS_READY → FULLY_READY` | `5m` | DM-TASK-053. Fallback: `DM_STALE_VERSION_TIMEOUT` → per-stage default. |
| `DM_STALE_VERSION_TIMEOUT` | Legacy: общий fallback для 4 таймаутов выше | `—` | DM-TASK-053: применяется per-variable — если per-stage переменная не задана, берётся этот fallback; если и он не задан — собственный default стадии. Смешанные конфигурации поддерживаются. |

### Orphan Cleanup

| Переменная | Описание | По умолчанию | Заметки |
|-----------|----------|-------------|---------|
| `DM_ORPHAN_SCAN_INTERVAL` | Интервал сканирования orphan candidates | `1h` | BRE-008: table-based (не full S3 scan) |
| `DM_ORPHAN_BATCH_SIZE` | Макс. candidates за один scan | `100` | |
| `DM_ORPHAN_GRACE_PERIOD` | Grace period перед удалением orphan blob | `1h` | TOCTOU safety: ждём завершения in-flight транзакций |
| `DM_ORPHAN_SCAN_TIMEOUT` | Таймаут одного scan цикла | `120s` | |

---

## Observability

| Переменная | Описание | По умолчанию | Заметки |
|-----------|----------|-------------|---------|
| `DM_LOG_LEVEL` | Уровень логирования | `info` | Допустимые: `debug`, `info`, `warn`, `error` |
| `DM_METRICS_PORT` | Порт Prometheus-метрик | `9090` | Endpoint: `/metrics` |
| `DM_TRACING_ENABLED` | Включить OpenTelemetry tracing | `false` | |
| `DM_TRACING_ENDPOINT` | OTLP/HTTP endpoint | _(пусто — трейсинг выключен)_ | Пример: `http://jaeger:4318` |
| `DM_TRACING_INSECURE` | HTTP вместо HTTPS для трейсинга | `false` | Только для dev/test |

---

## Топики брокера сообщений

Все топики настраиваются через переменные окружения `DM_BROKER_TOPIC_*`. По умолчанию используется иерархическое именование `{домен}.{тип}.{действие}`. Переопределяйте только если ваше окружение использует нестандартные имена.

### Входящие топики (DM слушает)

| Переменная | По умолчанию | Событие |
|-----------|-------------|---------|
| `DM_BROKER_TOPIC_DP_ARTIFACTS_PROCESSING_READY` | `dp.artifacts.processing-ready` | DocumentProcessingArtifactsReady |
| `DM_BROKER_TOPIC_DP_REQUESTS_SEMANTIC_TREE` | `dp.requests.semantic-tree` | GetSemanticTreeRequest |
| `DM_BROKER_TOPIC_DP_ARTIFACTS_DIFF_READY` | `dp.artifacts.diff-ready` | DocumentVersionDiffReady |
| `DM_BROKER_TOPIC_LIC_ARTIFACTS_ANALYSIS_READY` | `lic.artifacts.analysis-ready` | LegalAnalysisArtifactsReady |
| `DM_BROKER_TOPIC_LIC_REQUESTS_ARTIFACTS` | `lic.requests.artifacts` | GetArtifactsRequest (LIC) |
| `DM_BROKER_TOPIC_RE_ARTIFACTS_REPORTS_READY` | `re.artifacts.reports-ready` | ReportsArtifactsReady |
| `DM_BROKER_TOPIC_RE_REQUESTS_ARTIFACTS` | `re.requests.artifacts` | GetArtifactsRequest (RE) |

### DM → отправители (confirmation responses)

| Переменная | По умолчанию | Событие |
|-----------|-------------|---------|
| `DM_BROKER_TOPIC_DM_RESPONSES_ARTIFACTS_PERSISTED` | `dm.responses.artifacts-persisted` | DocumentProcessingArtifactsPersisted |
| `DM_BROKER_TOPIC_DM_RESPONSES_ARTIFACTS_PERSIST_FAILED` | `dm.responses.artifacts-persist-failed` | DocumentProcessingArtifactsPersistFailed |
| `DM_BROKER_TOPIC_DM_RESPONSES_SEMANTIC_TREE_PROVIDED` | `dm.responses.semantic-tree-provided` | SemanticTreeProvided |
| `DM_BROKER_TOPIC_DM_RESPONSES_ARTIFACTS_PROVIDED` | `dm.responses.artifacts-provided` | ArtifactsProvided |
| `DM_BROKER_TOPIC_DM_RESPONSES_DIFF_PERSISTED` | `dm.responses.diff-persisted` | DocumentVersionDiffPersisted |
| `DM_BROKER_TOPIC_DM_RESPONSES_DIFF_PERSIST_FAILED` | `dm.responses.diff-persist-failed` | DocumentVersionDiffPersistFailed |
| `DM_BROKER_TOPIC_DM_RESPONSES_LIC_ARTIFACTS_PERSISTED` | `dm.responses.lic-artifacts-persisted` | LegalAnalysisArtifactsPersisted |
| `DM_BROKER_TOPIC_DM_RESPONSES_LIC_ARTIFACTS_PERSIST_FAILED` | `dm.responses.lic-artifacts-persist-failed` | LegalAnalysisArtifactsPersistFailed |
| `DM_BROKER_TOPIC_DM_RESPONSES_RE_REPORTS_PERSISTED` | `dm.responses.re-reports-persisted` | ReportsArtifactsPersisted |
| `DM_BROKER_TOPIC_DM_RESPONSES_RE_REPORTS_PERSIST_FAILED` | `dm.responses.re-reports-persist-failed` | ReportsArtifactsPersistFailed |

### DM → downstream домены (notification events)

| Переменная | По умолчанию | Событие |
|-----------|-------------|---------|
| `DM_BROKER_TOPIC_DM_EVENTS_VERSION_ARTIFACTS_READY` | `dm.events.version-artifacts-ready` | VersionProcessingArtifactsReady |
| `DM_BROKER_TOPIC_DM_EVENTS_VERSION_ANALYSIS_READY` | `dm.events.version-analysis-ready` | VersionAnalysisArtifactsReady |
| `DM_BROKER_TOPIC_DM_EVENTS_VERSION_REPORTS_READY` | `dm.events.version-reports-ready` | VersionReportsReady |
| `DM_BROKER_TOPIC_DM_EVENTS_VERSION_CREATED` | `dm.events.version-created` | VersionCreated |
| `DM_BROKER_TOPIC_DM_EVENTS_VERSION_PARTIALLY_AVAILABLE` | `dm.events.version-partially-available` | VersionPartiallyAvailable (BRE-010) |

### Dead Letter Queue

| Переменная | По умолчанию | Событие |
|-----------|-------------|---------|
| `DM_BROKER_TOPIC_DM_DLQ_INGESTION_FAILED` | `dm.dlq.ingestion-failed` | Ошибки при сохранении артефактов |
| `DM_BROKER_TOPIC_DM_DLQ_QUERY_FAILED` | `dm.dlq.query-failed` | Ошибки при обслуживании запросов |
| `DM_BROKER_TOPIC_DM_DLQ_INVALID_MESSAGE` | `dm.dlq.invalid-message` | Невалидные сообщения (JSON, schema) |

---

## Пример .env файла

```env
# === Обязательные ===
DM_DB_DSN=postgres://dm:dm_dev_password@localhost:5433/dm_dev?sslmode=disable
DM_BROKER_ADDRESS=amqp://guest:guest@localhost:5673/
DM_STORAGE_ENDPOINT=http://localhost:9000
DM_STORAGE_BUCKET=dm-artifacts
DM_STORAGE_ACCESS_KEY=minioadmin
DM_STORAGE_SECRET_KEY=minioadmin
DM_KVSTORE_ADDRESS=localhost:6380

# === Необязательные (значения по умолчанию) ===
# Полный список переменных — см. таблицы выше

# PostgreSQL
# DM_DB_MAX_CONNS=25
# DM_DB_MIN_CONNS=5
# DM_DB_QUERY_TIMEOUT=10s

# RabbitMQ
# DM_BROKER_TLS=false

# Object Storage
# DM_STORAGE_REGION=ru-central1
# DM_STORAGE_PRESIGNED_TTL=5m

# Redis
# DM_KVSTORE_PASSWORD=
# DM_KVSTORE_DB=0
# DM_KVSTORE_POOL_SIZE=10
# DM_KVSTORE_TIMEOUT=2s

# HTTP
# DM_HTTP_PORT=8080

# Consumer
# DM_CONSUMER_PREFETCH=10
# DM_CONSUMER_CONCURRENCY=5

# Idempotency
# DM_IDEMPOTENCY_TTL=24h
# DM_IDEMPOTENCY_PROCESSING_TTL=120s
# DM_IDEMPOTENCY_STUCK_THRESHOLD=240s

# Outbox
# DM_OUTBOX_POLL_INTERVAL=200ms
# DM_OUTBOX_BATCH_SIZE=50
# DM_OUTBOX_LOCK_TIMEOUT=5s
# DM_OUTBOX_CLEANUP_HOURS=48

# Retry
# DM_RETRY_MAX_ATTEMPTS=3
# DM_RETRY_BACKOFF_BASE=1s

# DLQ
# DM_DLQ_MAX_REPLAY_COUNT=3

# Retention
# DM_RETENTION_ARCHIVE_DAYS=90
# DM_RETENTION_DELETED_BLOB_DAYS=30
# DM_RETENTION_DELETED_META_DAYS=365
# DM_RETENTION_AUDIT_DAYS=1095
# DM_RETENTION_BLOB_SCAN_INTERVAL=6h
# DM_RETENTION_META_SCAN_INTERVAL=24h
# DM_RETENTION_AUDIT_SCAN_INTERVAL=24h
# DM_RETENTION_BATCH_SIZE=50
# DM_RETENTION_SCAN_TIMEOUT=300s
# DM_RETENTION_AUDIT_MONTHS_AHEAD=3

# Timeouts
# DM_TIMEOUT_STORAGE_PUT=30s
# DM_TIMEOUT_STORAGE_GET=15s
# DM_TIMEOUT_EVENT_PROCESSING=60s
# DM_TIMEOUT_BROKER_PUBLISH=10s
# DM_STALE_VERSION_TIMEOUT=  # legacy fallback для per-stage watchdog таймаутов (см. раздел Watchdog)
# DM_SHUTDOWN_TIMEOUT=30s

# Observability
# DM_LOG_LEVEL=info
# DM_METRICS_PORT=9090
# DM_TRACING_ENABLED=false
# DM_TRACING_ENDPOINT=
# DM_TRACING_INSECURE=false

# Circuit Breaker
# DM_CB_MAX_REQUESTS=3
# DM_CB_INTERVAL=60s
# DM_CB_FAILURE_THRESHOLD=5
# DM_CB_TIMEOUT=30s
# DM_CB_PER_EVENT_BUDGET=35s

# Rate Limiting
# DM_RATELIMIT_ENABLED=true
# DM_RATELIMIT_READ_RPS=100
# DM_RATELIMIT_WRITE_RPS=20
# DM_RATELIMIT_CLEANUP_INTERVAL=5m
# DM_RATELIMIT_IDLE_TTL=10m

# Watchdog
# DM_WATCHDOG_SCAN_INTERVAL=5m
# DM_WATCHDOG_BATCH_SIZE=100
# DM-TASK-053: per-stage таймауты. Если не заданы — используется DM_STALE_VERSION_TIMEOUT
# как per-variable fallback; если и он не задан — встроенный per-stage default.
# DM_STALE_TIMEOUT_PROCESSING=5m
# DM_STALE_TIMEOUT_ANALYSIS=10m
# DM_STALE_TIMEOUT_REPORTS=5m
# DM_STALE_TIMEOUT_FINALIZATION=5m

# Orphan Cleanup
# DM_ORPHAN_SCAN_INTERVAL=1h
# DM_ORPHAN_BATCH_SIZE=100
# DM_ORPHAN_GRACE_PERIOD=1h
# DM_ORPHAN_SCAN_TIMEOUT=120s

# Ingestion
# DM_INGESTION_MAX_JSON_BYTES=10485760
# DM_INGESTION_MAX_BLOB_SIZE_BYTES=104857600
```
