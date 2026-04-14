# Конфигурация API/Backend Orchestrator

Сервис API/Backend Orchestrator настраивается через переменные окружения с префиксом `ORCH_`.

Реализация: `internal/config/` — `Load()` загружает `.env` файл (если есть), читает env, применяет значения по умолчанию, валидирует обязательные поля. При невалидной конфигурации `Load()` возвращает ошибку со списком всех проблем.

### Загрузка .env файла

При локальной разработке можно создать файл `.env` в директории запуска сервиса (как правило — `ApiBackendOrchestrator/development/`). `Load()` автоматически прочитает его при запуске. Уже установленные переменные окружения **имеют приоритет** над значениями из `.env` — файл не перезаписывает явно заданные переменные.

В продакшене `.env` файл не нужен — переменные передаются через Docker/K8s (env_file, ConfigMap, Secret).

---

## Содержание

1. [Обязательные переменные](#обязательные-переменные)
2. [Инфраструктура](#инфраструктура)
   - [HTTP-сервер](#http-сервер)
   - [Message Broker (RabbitMQ)](#message-broker-rabbitmq)
   - [Object Storage (S3)](#object-storage-s3-compatible)
   - [Redis](#redis)
3. [Загрузка файлов](#загрузка-файлов)
4. [Клиенты доменных сервисов](#клиенты-доменных-сервисов)
   - [DM Client](#dm-client)
   - [OPM Client](#opm-client)
   - [UOM Client](#uom-client)
5. [Аутентификация (JWT)](#аутентификация-jwt)
6. [SSE (Server-Sent Events)](#sse-server-sent-events)
7. [Устойчивость](#устойчивость)
   - [Rate Limiting](#rate-limiting)
   - [Circuit Breaker](#circuit-breaker)
   - [CORS](#cors)
8. [Observability](#observability)
9. [Топики брокера сообщений](#топики-брокера-сообщений)
   - [Исходящие команды (Orchestrator -> DP)](#исходящие-команды-orchestrator--dp)
   - [Входящие события от DP](#входящие-события-от-dp)
   - [Входящие события от DM](#входящие-события-от-dm)
10. [Пример .env файла](#пример-env-файла)

---

## Обязательные переменные

При отсутствии любой из этих переменных сервис не запустится и выведет ошибку со списком всех недостающих параметров.

| Переменная | Описание | Пример |
|-----------|----------|--------|
| `ORCH_DM_BASE_URL` | Base URL REST API сервиса Document Management | `http://dm:8080/api/v1` |
| `ORCH_BROKER_ADDRESS` | Адрес RabbitMQ | `amqp://guest:guest@localhost:5672/` |
| `ORCH_STORAGE_ENDPOINT` | Endpoint Object Storage (S3-compatible) | `https://storage.yandexcloud.net` |
| `ORCH_STORAGE_BUCKET` | Имя бакета для загрузок пользователей | `contractpro-uploads` |
| `ORCH_STORAGE_ACCESS_KEY` | Access Key для Object Storage | — |
| `ORCH_STORAGE_SECRET_KEY` | Secret Key для Object Storage | — |
| `ORCH_REDIS_ADDRESS` | Адрес Redis | `localhost:6379` |
| `ORCH_JWT_PUBLIC_KEY_PATH` | Путь к файлу публичного ключа JWT (RSA/ECDSA) | `/etc/orch/jwt-public.pem` |

**Ограничения (проверяются при старте):**
- `ORCH_HTTP_PORT` и `ORCH_METRICS_PORT` не должны совпадать
- `ORCH_BROKER_PREFETCH` >= 1
- Все duration/size значения должны быть положительными
- При включённом rate limiting (`ORCH_RATELIMIT_ENABLED=true`): `ORCH_RATELIMIT_READ_RPS` и `ORCH_RATELIMIT_WRITE_RPS` > 0
- `ORCH_JWT_PUBLIC_KEY_PATH` должен указывать на существующий readable файл
- `ORCH_UPLOAD_MAX_SIZE` > 0

---

## Инфраструктура

### HTTP-сервер

Оркестратор предоставляет HTTP REST API для frontend-приложений и внешних интеграций. Это единственная точка входа пользователей в систему (ASSUMPTION-ORCH-01).

| Переменная | Описание | По умолчанию | Заметки |
|-----------|----------|-------------|---------|
| `ORCH_HTTP_PORT` | Порт HTTP API + health probes | `8080` | Не должен совпадать с `ORCH_METRICS_PORT`. Endpoints: `/healthz`, `/readyz` |
| `ORCH_METRICS_PORT` | Порт Prometheus-метрик | `9090` | Endpoint: `/metrics` |
| `ORCH_REQUEST_TIMEOUT` | Таймаут по умолчанию для обработки запроса | `30s` | NFR-1.3: sync-ответы <= 500 мс p95, но таймаут — защитный барьер |
| `ORCH_UPLOAD_TIMEOUT` | Таймаут для запросов загрузки файлов | `60s` | ORCH-R-7: отдельный таймаут для upload, т.к. файлы до 20 МБ |
| `ORCH_SHUTDOWN_TIMEOUT` | Таймаут graceful shutdown | `30s` | Ordered teardown: readiness → SSE → HTTP → broker → Redis |

### Message Broker (RabbitMQ)

Оркестратор публикует команды на обработку (DP) и подписывается на события от DP и DM для доставки статусов пользователю через SSE.

| Переменная | Описание | По умолчанию | Заметки |
|-----------|----------|-------------|---------|
| `ORCH_BROKER_ADDRESS` | Адрес RabbitMQ | — (required) | `amqp://` или `amqps://` |
| `ORCH_BROKER_TLS` | Включить TLS для RabbitMQ | `false` | NFR-3.2: `true` в продакшене, MinVersion TLS 1.2 |
| `ORCH_BROKER_PREFETCH` | Consumer QoS prefetch count | `10` | Сколько сообщений RabbitMQ доставляет до ACK |

Топики RabbitMQ настраиваются отдельно — см. раздел [Топики брокера сообщений](#топики-брокера-сообщений).

### Object Storage (S3-compatible)

Оркестратор загружает исходные файлы пользователей в Object Storage до создания версии в DM (ASSUMPTION-ORCH-05). Ключ объекта: `uploads/{organization_id}/{document_id}/{uuid}`.

| Переменная | Описание | По умолчанию | Заметки |
|-----------|----------|-------------|---------|
| `ORCH_STORAGE_ENDPOINT` | Endpoint S3 | — (required) | MinIO: `http://localhost:9000`, prod: `https://storage.yandexcloud.net` |
| `ORCH_STORAGE_BUCKET` | Имя бакета | — (required) | ASSUMPTION-ORCH-05: bucket общий с DM |
| `ORCH_STORAGE_ACCESS_KEY` | Access Key | — (required) | |
| `ORCH_STORAGE_SECRET_KEY` | Secret Key | — (required) | |
| `ORCH_STORAGE_REGION` | Регион | `ru-central1` | MinIO: `us-east-1` |
| `ORCH_STORAGE_UPLOAD_TIMEOUT` | Таймаут S3 PutObject | `30s` | ORCH-R-3: streaming upload, retry 3 попытки |

### Redis

Redis используется оркестратором для ephemeral state: SSE pub/sub между инстансами, rate limiting counters, upload tracking (ASSUMPTION-ORCH-07). Оркестратор НЕ хранит данные в PostgreSQL.

| Переменная | Описание | По умолчанию | Заметки |
|-----------|----------|-------------|---------|
| `ORCH_REDIS_ADDRESS` | Адрес Redis | — (required) | |
| `ORCH_REDIS_PASSWORD` | Пароль | _(пусто)_ | |
| `ORCH_REDIS_DB` | Номер базы данных | `0` | |
| `ORCH_REDIS_POOL_SIZE` | Размер connection pool | `20` | |
| `ORCH_REDIS_TIMEOUT` | Таймаут операций | `2s` | |

---

## Загрузка файлов

Оркестратор принимает файлы через `multipart/form-data` (ASSUMPTION-ORCH-11: один файл + JSON-метаданные). Файл загружается в Object Storage streaming-режимом без полной буферизации в памяти (ORCH-R-7).

| Переменная | Описание | По умолчанию | Заметки |
|-----------|----------|-------------|---------|
| `ORCH_UPLOAD_MAX_SIZE` | Максимальный размер файла в байтах | `20971520` (20 МБ) | ORCH-R-7: валидация до начала чтения тела запроса |
| `ORCH_UPLOAD_ALLOWED_TYPES` | Допустимые MIME-типы (через запятую) | `application/pdf` | UR-2: в v1 только PDF. DOC/DOCX запланированы |
| `ORCH_UPLOAD_MAX_CONCURRENT` | Макс. одновременных загрузок на организацию | `10` | ORCH-R-7: semaphore per organization_id |

---

## Клиенты доменных сервисов

### DM Client

DM (Document Management) — основная синхронная зависимость оркестратора. Все CRUD-операции над документами, версиями и артефактами проходят через DM REST API. При недоступности DM оркестратор возвращает 503 (ORCH-R-1).

| Переменная | Описание | По умолчанию | Заметки |
|-----------|----------|-------------|---------|
| `ORCH_DM_BASE_URL` | Base URL REST API DM | — (required) | Формат: `http://dm:8080/api/v1` |
| `ORCH_DM_TIMEOUT_READ` | Таймаут чтения из DM (GET) | `5s` | Для списков, метаданных, артефактов |
| `ORCH_DM_TIMEOUT_WRITE` | Таймаут записи в DM (POST/PUT/DELETE) | `10s` | Для создания документов, версий |
| `ORCH_DM_RETRY_MAX` | Макс. повторов при ошибках DM | `3` | Retryable: 5xx, timeout, connection refused |
| `ORCH_DM_RETRY_BACKOFF` | Базовый интервал backoff | `200ms` | Exponential: 200ms, 400ms, 800ms |

### OPM Client

OPM (Organization Policy Management) — опциональная зависимость. Оркестратор проксирует запросы администраторов на управление политиками и чек-листами (UR-12). При недоступности OPM — fallback на default-политики (NFR-2.5).

| Переменная | Описание | По умолчанию | Заметки |
|-----------|----------|-------------|---------|
| `ORCH_OPM_BASE_URL` | Base URL REST API OPM | _(пусто — OPM выключен)_ | При пустом значении все запросы к OPM возвращают default-политики |
| `ORCH_OPM_TIMEOUT` | Таймаут запроса к OPM | `5s` | |

### UOM Client

UOM (User & Organization Management) — критическая зависимость для аутентификации. В v1 JWT валидируется локально по публичному ключу (ASSUMPTION-ORCH-02), UOM используется для проксирования auth-эндпоинтов (`/auth/login`, `/auth/refresh`, `/auth/logout`, `/users/me`).

| Переменная | Описание | По умолчанию | Заметки |
|-----------|----------|-------------|---------|
| `ORCH_UOM_BASE_URL` | Base URL REST API UOM | _(пусто — auth proxy выключен)_ | ASSUMPTION-ORCH-03: при пустом значении auth-эндпоинты возвращают 503 |
| `ORCH_UOM_TIMEOUT` | Таймаут запроса к UOM | `5s` | |

---

## Аутентификация (JWT)

Оркестратор валидирует JWT-токен на каждом запросе локально, без обращения к UOM (ASSUMPTION-ORCH-02). Токен содержит claims: `user_id`, `organization_id`, `role` (`LAWYER`, `BUSINESS_USER`, `ORG_ADMIN`).

| Переменная | Описание | По умолчанию | Заметки |
|-----------|----------|-------------|---------|
| `ORCH_JWT_PUBLIC_KEY_PATH` | Путь к файлу публичного ключа | — (required) | RSA или ECDSA. Файл должен быть readable при старте |

Публичный ключ загружается один раз при старте сервиса. Для ротации ключей — перезапуск пода (или hot-reload в будущих версиях).

---

## SSE (Server-Sent Events)

Оркестратор доставляет статусы обработки документов в реальном времени через SSE (ASSUMPTION-ORCH-06). Для горизонтального масштабирования используется Redis Pub/Sub как broadcast layer между инстансами (ORCH-R-6).

| Переменная | Описание | По умолчанию | Заметки |
|-----------|----------|-------------|---------|
| `ORCH_SSE_HEARTBEAT_INTERVAL` | Интервал SSE ping (`:heartbeat`) | `15s` | Поддерживает TCP-соединение живым через прокси/LB |
| `ORCH_SSE_MAX_CONNECTION_AGE` | Макс. время жизни SSE-соединения | `24h` | Принудительный reconnect для предотвращения утечки горутин |

### Подтверждение типа договора (FR-2.1.3)

Параметры обработки сценария 8.11 (Type Confirmation Handler).

| Переменная | Описание | По умолчанию | Заметки |
|-----------|----------|-------------|---------|
| `ORCH_USER_CONFIRMATION_TIMEOUT` | Таймаут ожидания подтверждения типа договора пользователем (статус `AWAITING_USER_INPUT`) | `24h` | По истечении watchdog переводит версию в `FAILED` с error_code `USER_CONFIRMATION_TIMEOUT`. Технически реализуется как TTL Redis-ключа `confirmation:wait:{version_id}` |
| `ORCH_USER_CONFIRMATION_IDEMPOTENCY_TTL` | TTL ключа идемпотентности `POST /confirm-type` | `60s` | Защищает от двойного клика; повторный вызов в окне возвращает 202 без повторной публикации команды |

### Permissions Resolver (UR-10)

Параметры компонента §6.21 — computed-флагов разрешений в `GET /users/me` (`UserProfile.permissions`). Frontend потребляет готовые boolean'ы; raw policy из OPM не запрашивает.

| Переменная | Описание | По умолчанию | Заметки |
|-----------|----------|-------------|---------|
| `ORCH_PERMISSIONS_CACHE_TTL` | TTL Redis-кеша computed permissions per `(org_id, role)` | `5m` | Инвалидация — событием `permissions:invalidate:{org_id}` от Admin Proxy при `PUT /admin/policies/{id}` |
| `ORCH_OPM_FALLBACK_BUSINESS_USER_EXPORT` | Fallback-значение `permissions.export_enabled` для BUSINESS_USER при недоступности OPM или отсутствии политики | `false` | Консервативный default — UR-2 «доступ ограничен» приоритетнее UR-10 «экспорт». Для прод-окружений с явно открытым экспортом — установить в `true` |
| `ORCH_OPM_PERMISSIONS_TIMEOUT` | Timeout запроса в OPM при cache miss | `2s` | По истечении — fallback на env, WARN-лог. /users/me не блокируется на OPM |

---

## Устойчивость

### Rate Limiting

Per-organization rate limiting для защиты от злоупотреблений. Лимиты раздельные для read (GET/HEAD) и write (POST/PUT/DELETE) операций.

| Переменная | Описание | По умолчанию | Заметки |
|-----------|----------|-------------|---------|
| `ORCH_RATELIMIT_ENABLED` | Включить rate limiting | `true` | |
| `ORCH_RATELIMIT_READ_RPS` | Лимит чтения (GET/HEAD) на организацию | `200` | Token bucket per organization_id |
| `ORCH_RATELIMIT_WRITE_RPS` | Лимит записи (POST/PUT/DELETE) на организацию | `50` | Token bucket per organization_id |

### Circuit Breaker

Circuit breaker защищает оркестратор от каскадных отказов при недоступности downstream-сервисов (DM, OPM, Object Storage). Паттерн: closed -> open -> half-open -> closed.

| Переменная | Описание | По умолчанию | Заметки |
|-----------|----------|-------------|---------|
| `ORCH_CB_FAILURE_THRESHOLD` | Последовательных ошибок для срабатывания (closed -> open) | `5` | Context errors не считаются |
| `ORCH_CB_TIMEOUT` | Длительность open state до перехода в half-open | `30s` | |
| `ORCH_CB_MAX_REQUESTS` | Запросов в half-open для решения open/close | `3` | |

### CORS

Настройки Cross-Origin Resource Sharing.

> **v1 production = same-origin** (см. ADR-6 в high-architecture.md). `ORCH_CORS_ALLOWED_ORIGINS` оставляется **пустым** — CORS middleware не активируется. Frontend и Orchestrator обслуживаются единым nginx (см. Frontend §13.2 nginx.conf). Конфигурация ниже — заготовка для cross-origin сценариев (внешние интеграции, разделение доменов в v1.x+).

| Переменная | Описание | По умолчанию | Заметки |
|-----------|----------|-------------|---------|
| `ORCH_CORS_ALLOWED_ORIGINS` | Разрешённые origins (через запятую) | _(пусто — same-origin, CORS не активируется)_ | Пример (cross-origin): `https://app.contractpro.ru,https://staging.contractpro.ru`. В v1 production не задаётся |
| `ORCH_CORS_MAX_AGE` | Время кэширования preflight-ответа (секунды) | `3600` | 1 час. Применимо только при cross-origin. Снижает количество OPTIONS-запросов |

---

## Observability

| Переменная | Описание | По умолчанию | Заметки |
|-----------|----------|-------------|---------|
| `ORCH_LOG_LEVEL` | Уровень логирования | `info` | Допустимые: `debug`, `info`, `warn`, `error` |
| `ORCH_TRACING_ENABLED` | Включить OpenTelemetry tracing | `false` | |
| `ORCH_TRACING_ENDPOINT` | OTLP/HTTP endpoint | _(пусто — трейсинг выключен)_ | Пример: `http://jaeger:4318` |
| `ORCH_TRACING_INSECURE` | HTTP вместо HTTPS для трейсинга | `false` | Только для dev/test |

---

## Топики брокера сообщений

Все топики настраиваются через переменные окружения `ORCH_BROKER_TOPIC_*`. По умолчанию используется иерархическое именование `{домен}.{тип}.{действие}`. Переопределяйте только если ваше окружение использует нестандартные имена.

### Исходящие команды (Orchestrator -> DP)

Оркестратор публикует команды на обработку и сравнение документов в DP.

| Переменная | По умолчанию | Событие |
|-----------|-------------|---------|
| `ORCH_BROKER_TOPIC_PROCESS_DOCUMENT` | `dp.commands.process-document` | ProcessDocumentRequested |
| `ORCH_BROKER_TOPIC_COMPARE_VERSIONS` | `dp.commands.compare-versions` | CompareDocumentVersionsRequested |
| `ORCH_BROKER_TOPIC_USER_CONFIRMED_TYPE` | `orch.commands.user-confirmed-type` | UserConfirmedType (в LIC, FR-2.1.3) |

### Входящие события от DP

Оркестратор подписывается на статусные события DP для отображения прогресса обработки пользователю через SSE.

| Переменная | По умолчанию | Событие |
|-----------|-------------|---------|
| `ORCH_BROKER_TOPIC_DP_STATUS_CHANGED` | `dp.events.status-changed` | StatusChangedEvent |
| `ORCH_BROKER_TOPIC_DP_PROCESSING_COMPLETED` | `dp.events.processing-completed` | ProcessingCompletedEvent |
| `ORCH_BROKER_TOPIC_DP_PROCESSING_FAILED` | `dp.events.processing-failed` | ProcessingFailedEvent |
| `ORCH_BROKER_TOPIC_DP_COMPARISON_COMPLETED` | `dp.events.comparison-completed` | ComparisonCompletedEvent |
| `ORCH_BROKER_TOPIC_DP_COMPARISON_FAILED` | `dp.events.comparison-failed` | ComparisonFailedEvent |

### Входящие события от LIC и RE

Оркестратор подписывается на статусные события LIC и RE для мгновенного обнаружения сбоев и гранулярного отображения прогресса (ASSUMPTION-ORCH-13).

| Переменная | По умолчанию | Событие |
|-----------|-------------|---------|
| `ORCH_BROKER_TOPIC_LIC_STATUS_CHANGED` | `lic.events.status-changed` | LICStatusChangedEvent |
| `ORCH_BROKER_TOPIC_LIC_CLASSIFICATION_UNCERTAIN` | `lic.events.classification-uncertain` | ClassificationUncertain (FR-2.1.3) |
| `ORCH_BROKER_TOPIC_RE_STATUS_CHANGED` | `re.events.status-changed` | REStatusChangedEvent |

### Входящие события от DM

Оркестратор подписывается на DM notification events для отображения промежуточного прогресса (ASSUMPTION-ORCH-10). Каждое событие транслируется в SSE-уведомление пользователю.

| Переменная | По умолчанию | Событие |
|-----------|-------------|---------|
| `ORCH_BROKER_TOPIC_DM_VERSION_ARTIFACTS_READY` | `dm.events.version-artifacts-ready` | VersionProcessingArtifactsReady |
| `ORCH_BROKER_TOPIC_DM_VERSION_ANALYSIS_READY` | `dm.events.version-analysis-ready` | VersionAnalysisArtifactsReady |
| `ORCH_BROKER_TOPIC_DM_VERSION_REPORTS_READY` | `dm.events.version-reports-ready` | VersionReportsReady |
| `ORCH_BROKER_TOPIC_DM_VERSION_PARTIALLY_AVAILABLE` | `dm.events.version-partially-available` | VersionPartiallyAvailable |
| `ORCH_BROKER_TOPIC_DM_VERSION_CREATED` | `dm.events.version-created` | VersionCreated |

---

## Пример .env файла

```env
# ============================================================
# API/Backend Orchestrator — конфигурация (.env)
# ============================================================
# Для локальной разработки. В продакшене переменные передаются
# через Docker env_file / K8s ConfigMap + Secret.
# ============================================================

# === Обязательные ===
ORCH_DM_BASE_URL=http://localhost:8081/api/v1
ORCH_BROKER_ADDRESS=amqp://guest:guest@localhost:5672/
ORCH_STORAGE_ENDPOINT=http://localhost:9000
ORCH_STORAGE_BUCKET=contractpro-uploads
ORCH_STORAGE_ACCESS_KEY=minioadmin
ORCH_STORAGE_SECRET_KEY=minioadmin
ORCH_REDIS_ADDRESS=localhost:6379
ORCH_JWT_PUBLIC_KEY_PATH=./jwt-public.pem

# === Необязательные (значения по умолчанию) ===
# Полный список переменных — см. таблицы выше

# HTTP-сервер
# ORCH_HTTP_PORT=8080
# ORCH_METRICS_PORT=9090
# ORCH_REQUEST_TIMEOUT=30s
# ORCH_UPLOAD_TIMEOUT=60s
# ORCH_SHUTDOWN_TIMEOUT=30s

# Загрузка файлов
# ORCH_UPLOAD_MAX_SIZE=20971520
# ORCH_UPLOAD_ALLOWED_TYPES=application/pdf
# ORCH_UPLOAD_MAX_CONCURRENT=10

# DM Client
# ORCH_DM_TIMEOUT_READ=5s
# ORCH_DM_TIMEOUT_WRITE=10s
# ORCH_DM_RETRY_MAX=3
# ORCH_DM_RETRY_BACKOFF=200ms

# OPM Client (пусто = OPM выключен, default-политики)
# ORCH_OPM_BASE_URL=
# ORCH_OPM_TIMEOUT=5s

# UOM Client (пусто = auth proxy выключен)
# ORCH_UOM_BASE_URL=
# ORCH_UOM_TIMEOUT=5s

# Object Storage
# ORCH_STORAGE_REGION=ru-central1
# ORCH_STORAGE_UPLOAD_TIMEOUT=30s

# Redis
# ORCH_REDIS_PASSWORD=
# ORCH_REDIS_DB=0
# ORCH_REDIS_POOL_SIZE=20
# ORCH_REDIS_TIMEOUT=2s

# RabbitMQ
# ORCH_BROKER_TLS=false
# ORCH_BROKER_PREFETCH=10

# SSE
# ORCH_SSE_HEARTBEAT_INTERVAL=15s
# ORCH_SSE_MAX_CONNECTION_AGE=24h

# Rate Limiting
# ORCH_RATELIMIT_ENABLED=true
# ORCH_RATELIMIT_READ_RPS=200
# ORCH_RATELIMIT_WRITE_RPS=50

# Circuit Breaker
# ORCH_CB_FAILURE_THRESHOLD=5
# ORCH_CB_TIMEOUT=30s
# ORCH_CB_MAX_REQUESTS=3

# CORS
# ORCH_CORS_ALLOWED_ORIGINS=
# ORCH_CORS_MAX_AGE=3600

# Observability
# ORCH_LOG_LEVEL=info
# ORCH_TRACING_ENABLED=false
# ORCH_TRACING_ENDPOINT=
# ORCH_TRACING_INSECURE=false

# Топики (переопределяйте только при нестандартных именах)
# ORCH_BROKER_TOPIC_PROCESS_DOCUMENT=dp.commands.process-document
# ORCH_BROKER_TOPIC_COMPARE_VERSIONS=dp.commands.compare-versions
# ORCH_BROKER_TOPIC_DP_STATUS_CHANGED=dp.events.status-changed
# ORCH_BROKER_TOPIC_DP_PROCESSING_COMPLETED=dp.events.processing-completed
# ORCH_BROKER_TOPIC_DP_PROCESSING_FAILED=dp.events.processing-failed
# ORCH_BROKER_TOPIC_DP_COMPARISON_COMPLETED=dp.events.comparison-completed
# ORCH_BROKER_TOPIC_DP_COMPARISON_FAILED=dp.events.comparison-failed
# ORCH_BROKER_TOPIC_DM_VERSION_ARTIFACTS_READY=dm.events.version-artifacts-ready
# ORCH_BROKER_TOPIC_DM_VERSION_ANALYSIS_READY=dm.events.version-analysis-ready
# ORCH_BROKER_TOPIC_DM_VERSION_REPORTS_READY=dm.events.version-reports-ready
# ORCH_BROKER_TOPIC_DM_VERSION_PARTIALLY_AVAILABLE=dm.events.version-partially-available
# ORCH_BROKER_TOPIC_DM_VERSION_CREATED=dm.events.version-created
```
