# Конфигурация Document Processing

Сервис Document Processing настраивается через переменные окружения с префиксом `DP_`.

Реализация: `internal/config/` — `Load()` загружает `.env` файл (если есть), читает env, применяет значения по умолчанию, валидирует обязательные поля.

### Загрузка .env файла

При локальной разработке можно создать файл `.env` в директории запуска сервиса (т.е. в директории, из которой запускается бинарник `dp-worker`, как правило — `DocumentProcessing/development/`). `Load()` автоматически прочитает его при запуске. Уже установленные переменные окружения **имеют приоритет** над значениями из `.env` — файл не перезаписывает явно заданные переменные.

В продакшене `.env` файл не нужен — переменные передаются через Docker/K8s (env_file, ConfigMap, Secret).

---

## Обязательные переменные

При отсутствии любой из этих переменных сервис не запустится и выведет ошибку со списком всех недостающих параметров.

| Переменная | Описание | Пример |
|-----------|----------|--------|
| `DP_BROKER_ADDRESS` | Адрес брокера сообщений | `localhost:9092` |
| `DP_STORAGE_ENDPOINT` | Endpoint Yandex Object Storage | `https://storage.yandexcloud.net` |
| `DP_STORAGE_BUCKET` | Имя бакета для временных артефактов | `dp-artifacts` |
| `DP_STORAGE_ACCESS_KEY` | Access Key для Object Storage | — |
| `DP_STORAGE_SECRET_KEY` | Secret Key для Object Storage | — |
| `DP_OCR_ENDPOINT` | Endpoint Yandex Cloud Vision OCR | `https://ocr.api.cloud.yandex.net` |
| `DP_OCR_API_KEY` | API-ключ для OCR | — |
| `DP_OCR_FOLDER_ID` | ID каталога в Yandex Cloud | — |
| `DP_KVSTORE_ADDRESS` | Адрес Redis для KV-store (idempotency, state) | `localhost:6379` |

---

## Необязательные переменные (значения по умолчанию)

### Лимиты обработки

| Переменная | Описание | По умолчанию |
|-----------|----------|-------------|
| `DP_LIMITS_MAX_FILE_SIZE` | Макс. размер файла в байтах | `20971520` (20 МБ) |
| `DP_LIMITS_MAX_PAGES` | Макс. количество страниц | `100` |
| `DP_LIMITS_JOB_TIMEOUT` | Таймаут задачи (Go duration) | `120s` |

### Конкурентность

| Переменная | Описание | По умолчанию |
|-----------|----------|-------------|
| `DP_CONCURRENCY_MAX_JOBS` | Макс. одновременных задач на экземпляре | `5` |
| `DP_OCR_RPS_LIMIT` | Rate limit для OCR (запросов/сек) | `10` |

### Idempotency Guard

| Переменная | Описание | По умолчанию |
|-----------|----------|-------------|
| `DP_IDEMPOTENCY_TTL` | Время жизни ключа идемпотентности (Go duration) | `24h` |

### KV Store (Redis)

| Переменная | Описание | По умолчанию |
|-----------|----------|-------------|
| `DP_KVSTORE_PASSWORD` | Пароль для Redis | _(пусто)_ |
| `DP_KVSTORE_DB` | Номер базы данных Redis | `0` |
| `DP_KVSTORE_POOL_SIZE` | Размер connection pool | `10` |
| `DP_KVSTORE_TIMEOUT` | Таймаут операций (Go duration) | `5s` |

### Retry

| Переменная | Описание | По умолчанию |
|-----------|----------|-------------|
| `DP_RETRY_MAX_ATTEMPTS` | Макс. количество повторных попыток | `3` |
| `DP_RETRY_BACKOFF_BASE` | Базовый интервал между попытками (Go duration) | `1s` |

### HTTP-сервер (health/readiness probes)

| Переменная | Описание | По умолчанию |
|-----------|----------|-------------|
| `DP_HTTP_PORT` | Порт HTTP-сервера | `8080` |

### Observability

| Переменная | Описание | По умолчанию |
|-----------|----------|-------------|
| `DP_LOG_LEVEL` | Уровень логирования | `info` |
| `DP_METRICS_PORT` | Порт Prometheus-метрик | `9090` |
| `DP_TRACING_ENABLED` | Включить OpenTelemetry tracing | `false` |
| `DP_TRACING_ENDPOINT` | Endpoint для OpenTelemetry tracing (OTLP/HTTP) | _(пусто — трейсинг выключен)_ |
| `DP_TRACING_INSECURE` | Использовать HTTP вместо HTTPS для трейсинга (только для dev) | `false` |

### Object Storage

| Переменная | Описание | По умолчанию |
|-----------|----------|-------------|
| `DP_STORAGE_REGION` | Регион Object Storage | `ru-central1` |

---

## Топики брокера сообщений

Все топики настраиваются через переменные окружения. По умолчанию используется иерархическое именование `{домен}.{тип}.{действие}`.

### Входящие команды (DP слушает)

| Переменная | По умолчанию | Событие |
|-----------|-------------|---------|
| `DP_BROKER_TOPIC_PROCESS_DOCUMENT` | `dp.commands.process-document` | ProcessDocumentRequested |
| `DP_BROKER_TOPIC_COMPARE_VERSIONS` | `dp.commands.compare-versions` | CompareDocumentVersionsRequested |

### DP → Document Management

| Переменная | По умолчанию | Событие |
|-----------|-------------|---------|
| `DP_BROKER_TOPIC_ARTIFACTS_READY` | `dp.artifacts.processing-ready` | DocumentProcessingArtifactsReady |
| `DP_BROKER_TOPIC_SEMANTIC_TREE_REQUEST` | `dp.requests.semantic-tree` | GetSemanticTreeRequest |
| `DP_BROKER_TOPIC_DIFF_READY` | `dp.artifacts.diff-ready` | DocumentVersionDiffReady |

### Document Management → DP (DP слушает)

| Переменная | По умолчанию | Событие |
|-----------|-------------|---------|
| `DP_BROKER_TOPIC_DM_ARTIFACTS_PERSISTED` | `dm.responses.artifacts-persisted` | DocumentProcessingArtifactsPersisted |
| `DP_BROKER_TOPIC_DM_ARTIFACTS_PERSIST_FAILED` | `dm.responses.artifacts-persist-failed` | DocumentProcessingArtifactsPersistFailed |
| `DP_BROKER_TOPIC_DM_SEMANTIC_TREE_PROVIDED` | `dm.responses.semantic-tree-provided` | SemanticTreeProvided |
| `DP_BROKER_TOPIC_DM_DIFF_PERSISTED` | `dm.responses.diff-persisted` | DocumentVersionDiffPersisted |
| `DP_BROKER_TOPIC_DM_DIFF_PERSIST_FAILED` | `dm.responses.diff-persist-failed` | DocumentVersionDiffPersistFailed |

### Dead Letter Queue

| Переменная | По умолчанию | Событие |
|-----------|-------------|---------|
| `DP_BROKER_TOPIC_DLQ` | `dp.dlq` | Сообщения, не обработанные после исчерпания retry |

### DP → внешние потребители

| Переменная | По умолчанию | Событие |
|-----------|-------------|---------|
| `DP_BROKER_TOPIC_STATUS_CHANGED` | `dp.events.status-changed` | StatusChangedEvent |
| `DP_BROKER_TOPIC_PROCESSING_COMPLETED` | `dp.events.processing-completed` | ProcessingCompletedEvent |
| `DP_BROKER_TOPIC_PROCESSING_FAILED` | `dp.events.processing-failed` | ProcessingFailedEvent |
| `DP_BROKER_TOPIC_COMPARISON_COMPLETED` | `dp.events.comparison-completed` | ComparisonCompletedEvent |
| `DP_BROKER_TOPIC_COMPARISON_FAILED` | `dp.events.comparison-failed` | ComparisonFailedEvent |

---

## Пример .env файла

```env
# === Обязательные ===
DP_BROKER_ADDRESS=amqp://user:password@localhost:5672/
DP_STORAGE_ENDPOINT=https://storage.yandexcloud.net
DP_STORAGE_BUCKET=dp-artifacts
DP_STORAGE_ACCESS_KEY=your-access-key
DP_STORAGE_SECRET_KEY=your-secret-key
DP_OCR_ENDPOINT=https://ocr.api.cloud.yandex.net
DP_OCR_API_KEY=your-api-key
DP_OCR_FOLDER_ID=your-folder-id
DP_KVSTORE_ADDRESS=localhost:6379

# === Необязательные (значения по умолчанию) ===
# DP_LIMITS_MAX_FILE_SIZE=20971520
# DP_LIMITS_MAX_PAGES=100
# DP_LIMITS_JOB_TIMEOUT=120s
# DP_CONCURRENCY_MAX_JOBS=5
# DP_OCR_RPS_LIMIT=10
# DP_IDEMPOTENCY_TTL=24h
# DP_KVSTORE_PASSWORD=
# DP_KVSTORE_DB=0
# DP_KVSTORE_POOL_SIZE=10
# DP_KVSTORE_TIMEOUT=5s
# DP_RETRY_MAX_ATTEMPTS=3
# DP_RETRY_BACKOFF_BASE=1s
# DP_HTTP_PORT=8080
# DP_LOG_LEVEL=info
# DP_METRICS_PORT=9090
# DP_TRACING_ENABLED=false
# DP_TRACING_ENDPOINT=
# DP_TRACING_INSECURE=false
# DP_STORAGE_REGION=ru-central1
```
