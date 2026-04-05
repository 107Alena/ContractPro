# Развертывание Document Management

Документ описывает развертывание сервиса **Document Management (dm-service)** в локальной среде разработки и в production используя Docker Compose.

---

## 1. Быстрый старт (локальная разработка)

### 1.1. Предварительные условия

- **Docker Desktop** версии 4.0+ (с поддержкой Docker Compose v2)
- **Git** (для клонирования репозитория)
- Свободные порты: 5433, 6380, 5673, 15673, 9000, 9001, 8081, 9091

### 1.2. Инициализация локального окружения

Из корневой директории проекта:

```bash
# Скопировать пример конфигурации
cp DocumentManagement/development/.env.example DocumentManagement/development/.env

# При необходимости отредактировать .env
# Для локальной разработки с docker-compose все required значения
# уже заданы через environment блок в docker-compose.yaml
nano DocumentManagement/development/.env  # или любой другой редактор
```

### 1.3. Запуск развертывания (Development)

Из директории `DocumentManagement/development/`:

```bash
# Собрать образы и запустить все сервисы
docker compose up --build

# Или запустить в фоновом режиме
docker compose up --build -d

# Или через Makefile
make compose-up

# Посмотреть логи dm-service
docker compose logs -f dm-service

# Остановить все сервисы (оставить volumes)
docker compose down     # или make compose-down

# Остановить все сервисы и удалить volumes (данные будут потеряны)
docker compose down -v
```

### 1.4. Проверка развертывания

После запуска `docker compose up`:

```bash
# Проверить статус контейнеров
docker compose ps

# Проверить liveness probe (должен вернуть 200)
curl -i http://localhost:8081/healthz

# Проверить readiness probe (должен вернуть 200 с breakdown)
curl -i http://localhost:8081/readyz

# Посмотреть Prometheus метрики
curl http://localhost:9091/metrics

# RabbitMQ Management UI: http://localhost:15673 (guest/guest)
# MinIO Console:          http://localhost:9001  (minioadmin/minioadmin)

# Проверить Redis
docker exec dm-redis redis-cli ping
# Должен вернуть: PONG

# Проверить PostgreSQL
docker exec dm-postgres psql -U dm -d dm_dev -c "SELECT version();"

# Проверить версию миграций
docker compose run --rm dm-migrate version
```

### 1.5. Структура локального развертывания

```
docker-compose.yaml (из DocumentManagement/development/)
├── postgres:16-alpine (dm-postgres)
│   ├── Port: 5433 → 5432
│   ├── Credentials: dm / dm_dev_password
│   ├── Database: dm_dev
│   └── Volume: postgres-data
├── redis:7-alpine (dm-redis)
│   ├── Port: 6380 → 6379
│   └── Volume: redis-data
├── rabbitmq:3-management-alpine (dm-rabbitmq)
│   ├── Port: 5673 → 5672 (AMQP)
│   ├── Port: 15673 → 15672 (Management UI)
│   ├── Credentials: guest / guest
│   └── Volume: rabbitmq-data
├── minio/minio (dm-minio)
│   ├── Port: 9000 (S3 API)
│   ├── Port: 9001 (Console UI)
│   └── Volume: minio-data
├── minio-init (run-once)
│   └── Создаёт bucket dm-artifacts
├── dm-migrate (init-container)
│   ├── entrypoint: dm-migrate up
│   ├── Depends on: postgres (healthy)
│   └── restart: "no"
└── dm-service (приложение)
    ├── Port: 8081 → 8080 (API + health probes)
    ├── Port: 9091 → 9090 (Prometheus metrics)
    ├── Depends on: dm-migrate (completed), redis (healthy),
    │               rabbitmq (healthy), minio-init (completed)
    └── Env: DM_LOG_LEVEL=debug
```

**Сетевая топология:**
- PostgreSQL: `postgres:5432` в сети контейнеров
- Redis: `redis:6379` в сети контейнеров
- RabbitMQ: `rabbitmq:5672` в сети контейнеров
- MinIO: `minio:9000` в сети контейнеров
- Все сервисы находятся в одной Docker Compose сети (создается автоматически)

### 1.6. Сосуществование с Document Processing

DM использует смещённые порты для избежания конфликтов при одновременном запуске обоих стеков:

| Компонент | Document Processing | Document Management |
|-----------|-------------------|-------------------|
| HTTP API / Health | 8080 | **8081** |
| Prometheus Metrics | 9090 | **9091** |
| PostgreSQL | — | **5433** |
| Redis | 6379 | **6380** |
| RabbitMQ AMQP | 5672 | **5673** |
| RabbitMQ Management | 15672 | **15673** |
| MinIO S3 | — | **9000** |
| MinIO Console | — | **9001** |

---

## 2. Миграции базы данных

### 2.1. Архитектура

Миграции выполняются отдельным бинарником `dm-migrate` как init-container **перед** стартом приложения:

```
┌─────────────┐     ┌──────────────┐     ┌──────────────┐
│  dm-migrate │────▶│  PostgreSQL   │◀────│  dm-service  │
│ (init-cont.)│     │              │     │ (app)        │
└─────────────┘     └──────────────┘     └──────────────┘
   1. migrate up       schema ready        2. version check
   2. exit 0                               3. start serving
```

**Принцип разделения:**
- `dm-migrate` — единственный компонент, выполняющий DDL-операции
- `dm-service` — при старте только проверяет schema version > 0 и dirty = false. Если проверка не прошла — fail fast

### 2.2. Команды dm-migrate

```bash
# Применить все pending миграции
docker compose run --rm dm-migrate up

# Проверить текущую версию
docker compose run --rm dm-migrate version

# Мигрировать к конкретной версии
docker compose run --rm dm-migrate goto 3

# Откатить все миграции (УДАЛЯЕТ ВСЕ ТАБЛИЦЫ)
docker compose run --rm dm-migrate down --confirm-destroy
```

### 2.3. Файлы миграций

Расположение: `internal/infra/postgres/migrations/`

| Версия | Файл | Описание |
|--------|------|----------|
| 000001 | initial_schema | 7 таблиц: documents, document_versions, artifact_descriptors, version_diff_references, audit_records, outbox_events, orphan_candidates |
| 000002 | dlq_records | Таблица dm_dlq_records для DLQ replay |
| 000003 | rls_policies | Row-Level Security для tenant isolation (5 таблиц) |
| 000004 | audit_partitions | Конвертация audit_records в PARTITION BY RANGE (created_at) |
| 000005 | audit_protection | Append-only triggers + dm_audit_writer роль |

Все up-миграции обёрнуты в `BEGIN/COMMIT` для атомарности. Каждая версия имеет пару up + down.

### 2.4. Откат миграций

```bash
# Откат на одну версию назад (с 5 до 4)
docker compose run --rm dm-migrate goto 4

# Локально (при наличии DSN)
DM_DB_DSN="postgres://dm:password@localhost:5433/dm_dev?sslmode=disable" ./dm-migrate goto 4
```

### 2.5. Восстановление после dirty state

Если миграция завершилась с ошибкой, `schema_migrations` будет в dirty state. `dm-service` откажется стартовать.

**Действия:**
1. Проверить состояние: `docker compose run --rm dm-migrate version` — покажет версию и dirty=true
2. Исправить проблему вручную в БД (если partial apply)
3. Обновить `schema_migrations`: `UPDATE schema_migrations SET dirty = false;`
4. Повторить: `docker compose run --rm dm-migrate up`

### 2.6. Concurrent migration safety

`golang-migrate` автоматически использует PostgreSQL advisory lock. В multi-replica deployment несколько init-containers могут запустить `dm-migrate up` одновременно — только один получит lock, остальные дождутся. Дополнительной координации не требуется.

Подробнее: [`migration-strategy.md`](./migration-strategy.md)

---

## 3. Startup и Shutdown

### 3.1. Startup Sequence (16 фаз)

Цепочка зависимостей при старте:

```
PostgreSQL healthy → dm-migrate exit 0 → dm-service schema check →
→ connect broker/redis/storage → application services → readiness=true
```

Подробная последовательность `dm-service`:

| Фаза | Компонент | Описание |
|------|-----------|----------|
| 1 | Config | Загрузка конфигурации из env + .env файла |
| 2 | Observability | Logger (JSON/stderr), Prometheus metrics, OpenTelemetry tracer |
| 3 | PostgreSQL | Connection pool + schema version verification (fail fast) |
| 4 | Redis | KV store для idempotency |
| 5 | RabbitMQ | Broker client + topology declaration (queues, exchanges) |
| 6 | Object Storage | S3 client с circuit breaker (BRE-014) |
| 7 | Repositories | Transactor + 6 PostgreSQL repositories |
| 8 | Outbox Writer | Transactional outbox для event publishing |
| 9 | Confirmation Publisher | Прямая публикация для query responses |
| 10 | Idempotency Guard | Redis-backed дедупликация |
| 11 | Application Services | 5 сервисов: ingestion, query, lifecycle, version, diff |
| 12 | Background Jobs | Watchdog, orphan cleanup, 3 retention jobs |
| 13 | Event Consumer | Подписка на 7 входящих топиков |
| 14 | API Handler | REST endpoints + rate limiting (BRE-009) |
| 15 | Outbox Poller | Event publishing + health metrics |
| 16 | Health Handler | /healthz, /readyz (3 core + 1 non-core check) |

При ошибке на любой фазе — progressive cleanup уже инициализированных компонентов.

### 3.2. Graceful Shutdown (BRE-019)

Сервис корректно обрабатывает сигналы SIGTERM и SIGINT с таймаутом `DM_SHUTDOWN_TIMEOUT` (default 30s).

**Последовательность shutdown:**

| Фаза | Действие | Описание |
|------|----------|----------|
| 0 | readiness=false | /readyz возвращает 503, балансировщики отводят трафик |
| 1 | Stop Outbox Poller | Завершает текущий batch publish |
| 2 | Stop Outbox Metrics | Прекращает сбор метрик outbox |
| 3 | Stop Background Jobs | Watchdog, orphan cleanup, retention jobs |
| 3.9 | Close Rate Limiter | Остановка GC goroutine |
| 4 | Close Broker | Останавливает consumer'ы, drains in-flight messages |
| 5 | Stop HTTP Servers | API + metrics серверы завершают текущие запросы |
| 6 | Close Redis | Graceful close connection pool |
| 7 | Close PostgreSQL | Graceful close connection pool |
| 8 | Flush Observability | Отправка OpenTelemetry traces |

**Docker Compose behavior:**

```bash
# Отправляет SIGTERM (10 сек timeout по умолчанию)
docker compose down

# Graceful при SIGTERM (K8s, Systemd)
docker stop dm-service

# Принудительное завершение
docker compose kill
```

---

## 4. Production развертывание

### 4.1. Предварительные условия

- **Docker** (контейнеры) + **Docker Compose v2** (оркестрация) или **Kubernetes**
- **PostgreSQL 16+** (managed: Yandex Managed PostgreSQL) с point-in-time recovery
- **Redis 7+** (managed или с Sentinel/Cluster для HA)
- **RabbitMQ 3.x** (cluster с quorum queues для HA)
- **Yandex Object Storage** (S3-compatible) для blob storage
- Безопасное хранилище для секретов (env vars, Vault, cloud secrets)

### 4.2. Подготовка production конфигурации

```bash
# Создать .env.prod с production переменными
cat > .env.prod << 'EOF'
# PostgreSQL (managed)
DM_DB_DSN=postgres://dm_prod:<PASSWORD>@pg-host:5432/dm_prod?sslmode=require
DM_DB_MAX_CONNS=25
DM_DB_MIN_CONNS=5

# RabbitMQ (с TLS)
DM_BROKER_ADDRESS=amqps://dm_prod:<PASSWORD>@rabbitmq-host:5671/
DM_BROKER_TLS=true

# Yandex Object Storage
DM_STORAGE_ENDPOINT=https://storage.yandexcloud.net
DM_STORAGE_BUCKET=contractpro-dm-artifacts-prod
DM_STORAGE_ACCESS_KEY=<PRODUCTION_ACCESS_KEY>
DM_STORAGE_SECRET_KEY=<PRODUCTION_SECRET_KEY>
DM_STORAGE_REGION=ru-central1

# Redis (с паролем)
DM_KVSTORE_ADDRESS=redis-host:6379
DM_KVSTORE_PASSWORD=<PRODUCTION_REDIS_PASSWORD>

# Observability
DM_LOG_LEVEL=info
DM_TRACING_ENABLED=true
DM_TRACING_ENDPOINT=https://tracing.example.com
EOF

# Защитить файл конфигурации
chmod 600 .env.prod
```

### 4.3. Build и push production образа

```bash
cd DocumentManagement/development

# Собрать с тегом версии
make docker-build IMAGE_TAG=v1.0.0

# Или с автоматическим тегом из git
make docker-build

# Просмотреть собранные образы
docker images | grep contractpro/dm-service

# Push в реестр
# docker push contractpro/dm-service:v1.0.0
# docker push contractpro/dm-service:latest
```

### 4.4. Запуск production развертывания

Production deployment предполагает использование **внешних managed-сервисов** для PostgreSQL, Redis, RabbitMQ. Docker Compose используется только для dm-service и dm-migrate.

```bash
# Установить переменные окружения для credentials
export DM_IMAGE_TAG="v1.0.0"

# Запустить миграции (одноразово при обновлении)
docker run --rm --env-file .env.prod \
  contractpro/dm-service:${DM_IMAGE_TAG} \
  /usr/local/bin/dm-migrate up

# Запустить сервис
docker run -d --name dm-service \
  --env-file .env.prod \
  -p 8080:8080 -p 9090:9090 \
  --restart always \
  contractpro/dm-service:${DM_IMAGE_TAG}
```

### 4.5. Отличия Development от Production

| Аспект | Development | Production |
|--------|-------------|-----------|
| PostgreSQL | Локальный контейнер (5433) | Managed (Yandex Managed PG) |
| Redis | Локальный контейнер (6380) | Managed / Sentinel cluster |
| RabbitMQ | С Management UI (15673) | Cluster без Management UI |
| Object Storage | MinIO (9000) | Yandex Object Storage |
| TLS | Отключён | Включён (sslmode=require, amqps://) |
| Credentials | Hardcoded (dev) | Из env vars / Vault |
| Логирование | stdout, debug | JSON-file с ротацией, info |
| Перезагрузка | нет | `restart: always` |
| Rate limiting | Включён (по умолчанию) | Включён, лимиты по потребностям |
| Миграции | docker compose init-container | docker run + dm-migrate up |

### 4.6. Health Checks и мониторинг

```bash
# Liveness (всегда 200 если процесс запущен)
curl -i http://localhost:8080/healthz

# Readiness (200 если все core-компоненты доступны)
# Core: PostgreSQL, Redis, RabbitMQ
# Non-core: Object Storage (REV-024 — не блокирует readiness)
curl -i http://localhost:8080/readyz

# Prometheus метрики
curl http://localhost:9090/metrics
```

**Ключевые метрики для мониторинга:**

| Метрика | Тип | Описание |
|---------|-----|----------|
| `dm_events_received_total` | counter | Входящие события по топикам |
| `dm_events_processed_total` | counter | Обработанные события (status: success/error) |
| `dm_event_processing_duration_seconds` | histogram | Время обработки события |
| `dm_outbox_pending_count` | gauge | Количество неопубликованных событий в outbox |
| `dm_outbox_oldest_pending_age_seconds` | gauge | Возраст старейшего pending события (REV-022) |
| `dm_dlq_messages_total` | counter | Сообщения в DLQ (label: reason) |
| `dm_stuck_versions_count` | gauge | Версии в промежуточных состояниях |
| `dm_circuit_breaker_state` | gauge | Состояние circuit breaker (0=closed, 1=half-open, 2=open) |
| `dm_api_rate_limited_total` | counter | Запросы, заблокированные rate limiter |
| `dm_tenant_mismatch_total` | counter | Несовпадения tenant (BRE-015) |
| `dm_integrity_check_failures_total` | counter | Несовпадения content hash (BRE-027) |

---

## 5. Управление секретами

### 5.1. Классификация секретов

| Секрет | Использование | Уровень критичности |
|--------|--------------|-------------------|
| `DM_DB_DSN` | Строка подключения к PostgreSQL (включает пароль) | Критический |
| `DM_STORAGE_ACCESS_KEY` / `DM_STORAGE_SECRET_KEY` | Yandex Object Storage credentials | Критический |
| `DM_KVSTORE_PASSWORD` | Пароль Redis | Высокий |
| RabbitMQ password (в `DM_BROKER_ADDRESS`) | Аутентификация брокера | Высокий |

### 5.2. Рекомендации

**Development:**
- Секреты хранятся в `.env` файле (не коммитится, в `.gitignore`)
- Docker Compose `environment` блок переопределяет `.env`

**Production:**
- **Вариант A (минимальный):** env vars через CI/CD pipeline, chmod 600 на `.env.prod`
- **Вариант B (рекомендуемый):** HashiCorp Vault или Yandex Lockbox
  - Инъекция секретов через init-container или Vault Agent sidecar
  - Автоматическая ротация credentials
- **Вариант C (Kubernetes):** Kubernetes Secrets + External Secrets Operator

**Общие правила:**
- Никогда не коммитить `.env`, `.env.prod`, credentials в git
- Использовать отдельные credentials для dev и prod
- Ротация ключей Object Storage: не реже 1 раза в 90 дней
- Audit логирование доступа к секретам

---

## 6. Шифрование данных (NFR-3.2)

### 6.1. At Rest

**PostgreSQL:**
- **Managed (рекомендуется):** Yandex Managed PostgreSQL обеспечивает прозрачное шифрование дисков (AES-256)
- **Self-hosted:** dm-crypt / LUKS для шифрования файловой системы, на которой расположены data directory и WAL

**Object Storage:**
- **Yandex Object Storage:** SSE-S3 (server-side encryption с managed keys) включается на уровне bucket:
  ```bash
  yc storage bucket update \
    --name contractpro-dm-artifacts-prod \
    --default-encryption algorithm=aes256-gcm
  ```
- **MinIO (dev):** шифрование не требуется для локальной разработки

**Redis:**
- Redis используется как ephemeral кэш для idempotency. При потере Redis данные восстанавливаются через DB fallback
- Для production с persistence (AOF): шифрование диска через dm-crypt

### 6.2. In Transit

| Канал | Протокол | Настройка |
|-------|----------|-----------|
| PostgreSQL | TLS (sslmode=require) | В DSN: `?sslmode=require` |
| RabbitMQ | TLS (amqps://) | `DM_BROKER_TLS=true` |
| Object Storage | HTTPS | Endpoint: `https://storage.yandexcloud.net` |
| Redis | TLS | При необходимости: `rediss://` схема в адресе |
| HTTP API | HTTPS | Через reverse proxy / API Gateway (не DM) |

---

## 7. Отказоустойчивость (NFR-2.4 / NFR-2.5)

### 7.1. PostgreSQL HA

**Managed (рекомендуется):**
- Yandex Managed PostgreSQL с автоматическим failover (synchronous replication)
- Point-in-time recovery с WAL archiving (RPO ≤ 15 мин). Подробнее: [раздел 10](#10-резервное-копирование-и-восстановление-bre-021rev-033)
- RTO: автоматический failover < 60 сек

**Self-hosted:**
- Patroni + etcd для automatic failover
- Streaming replication (synchronous для RPO=0)
- pgBouncer для connection pooling и transparent failover

**Конфигурация DM:**
- `DM_DB_MAX_CONNS=25` — основной пул (sync API + async consumer)
- `DM_DB_MIN_CONNS=5` — минимум активных соединений
- Connection pool автоматически переподключается при failover

### 7.2. Redis HA

**Redis Sentinel (рекомендуется для production):**
- 3 Sentinel nodes для quorum-based failover
- DM автоматически fallback на DB при недоступности Redis
- При потере Redis: idempotency проверяется через `artifact_descriptors` таблицу

**Redis Cluster:**
- Для высоких нагрузок (> 10K idempotency checks/sec)
- Sharding по ключам idempotency

**DM resilience:**
- Redis — **не** критический компонент для data integrity
- При потере Redis: fallback на DB lookup + метрика `dm_idempotency_fallback_total`
- Worst case: duplicate processing (at-least-once delivery, idempotent handlers)

### 7.3. RabbitMQ HA

**Cluster с quorum queues:**
- Минимум 3 nodes для quorum (потеря 1 node без downtime)
- DLQ queues: `x-queue-type=quorum` (автоматическая репликация)
- Incoming queues: `durable=true`, `x-max-length=10000`

**DM resilience:**
- Auto-reconnect при обрыве соединения (exponential backoff 1s-30s)
- Publisher confirms для гарантии доставки
- Transactional Outbox: events не теряются при недоступности broker

### 7.4. Object Storage HA

**Yandex Object Storage:**
- Встроенная geo-redundancy (SLA 99.99%)
- S3 versioning для защиты от случайного удаления

**DM resilience:**
- Circuit breaker (BRE-014): fast fail после 5 consecutive failures, recovery через 30s
- Per-event budget: 35s (не 5 x 3 x 30s)
- Object Storage — non-core в readiness probe (REV-024)

---

## 8. Версионирование и обновления

### 8.1. Тегирование образов

```bash
# Сборка с семантическим версионированием
make docker-build IMAGE_TAG=v1.0.0
make docker-build IMAGE_TAG=v1.0.1

# Сборка с версией из git tags
make docker-build  # Использует git describe
```

### 8.2. Процедура обновления

```bash
# 1. Собрать и push новый образ
make docker-build IMAGE_TAG=v1.1.0

# 2. Выполнить миграции (если есть новые)
docker run --rm --env-file .env.prod \
  contractpro/dm-service:v1.1.0 \
  /usr/local/bin/dm-migrate up

# 3. Обновить сервис
docker stop dm-service
docker run -d --name dm-service \
  --env-file .env.prod \
  -p 8080:8080 -p 9090:9090 \
  --restart always \
  contractpro/dm-service:v1.1.0
```

### 8.3. Откат на предыдущую версию

```bash
# 1. Откатить миграции (если необходимо)
docker run --rm --env-file .env.prod \
  contractpro/dm-service:v1.1.0 \
  /usr/local/bin/dm-migrate goto <previous_version>

# 2. Запустить предыдущую версию
docker stop dm-service && docker rm dm-service
docker run -d --name dm-service \
  --env-file .env.prod \
  -p 8080:8080 -p 9090:9090 \
  --restart always \
  contractpro/dm-service:v1.0.0
```

---

## 9. Troubleshooting

### 9.1. Контейнер не запускается

```bash
# Проверить логи
docker compose logs dm-service
docker compose logs dm-migrate
docker compose logs postgres

# Распространенные ошибки:
# - "schema version is 0 or dirty" → dm-migrate не выполнился или упал
# - "missing required config" → .env не загружен или неполный
# - "connection refused" → PostgreSQL/Redis/RabbitMQ не готовы
# - "address already in use" → порт занят (проверить 8081, 9091, 5433, 6380, 5673)

# Перезапустить с чистого состояния
docker compose down -v
docker compose up --build
```

### 9.2. Проблемы с миграциями

```bash
# Проверить состояние миграций
docker compose run --rm dm-migrate version

# Если dirty=true:
# 1. Подключиться к БД
docker exec -it dm-postgres psql -U dm -d dm_dev

# 2. Проверить состояние schema_migrations
SELECT * FROM schema_migrations;

# 3. Исправить вручную (после анализа)
UPDATE schema_migrations SET dirty = false;

# 4. Повторить миграцию
docker compose run --rm dm-migrate up
```

### 9.3. PostgreSQL-specific проблемы

```bash
# Connection pool exhaustion
# Симптом: "too many clients" в логах
# Решение: увеличить DM_DB_MAX_CONNS или проверить утечки соединений
docker exec -it dm-postgres psql -U dm -d dm_dev \
  -c "SELECT count(*) FROM pg_stat_activity WHERE datname='dm_dev';"

# RLS misconfiguration
# Симптом: пустые результаты запросов
# Проверка: убедиться что app.organization_id установлен
docker exec -it dm-postgres psql -U dm -d dm_dev \
  -c "SELECT current_setting('app.organization_id', true);"

# Audit partition maintenance
# Проверка: audit_records partitions
docker exec -it dm-postgres psql -U dm -d dm_dev \
  -c "SELECT relname FROM pg_class WHERE relname LIKE 'audit_records%' ORDER BY relname;"
```

### 9.4. RabbitMQ не доступен

```bash
# Проверить состояние
docker compose ps rabbitmq

# Проверить healthcheck
docker compose logs rabbitmq | tail -20

# DM автоматически переподключается (exponential backoff 1s-30s)
# Проверить reconnect в логах dm-service:
docker compose logs dm-service | grep -i "reconnect\|connection"
```

### 9.5. Object Storage недоступен

```bash
# Проверить circuit breaker
curl -s http://localhost:9091/metrics | grep dm_circuit_breaker_state
# dm_circuit_breaker_state{component="object_storage"} 0   — closed (OK)
# dm_circuit_breaker_state{component="object_storage"} 2   — open (failing)

# Проверить MinIO
docker compose logs minio

# Проверить bucket
docker exec dm-minio mc ls local/dm-artifacts
```

### 9.6. Высокое использование памяти

```bash
# Проверить использование
docker stats

# Если dm-service потребляет слишком много:
# - Уменьшить DM_CONSUMER_CONCURRENCY (default 5)
# - Уменьшить DM_DB_MAX_CONNS (default 25)
# - Уменьшить DM_OUTBOX_BATCH_SIZE (default 50)
# - Проверить DM_INGESTION_MAX_JSON_BYTES (default 10MB)

# Если Redis превышает лимит:
# - Уменьшить DM_IDEMPOTENCY_TTL (default 24h)
# - Увеличить Redis maxmemory
```

---

## 10. Резервное копирование и восстановление (BRE-021/REV-033)

> Требования: **NFR-2.3 — RPO ≤ 15 минут, RTO ≤ 2 часа.**
> Все процедуры рассчитаны на Yandex Managed PostgreSQL 16 + Yandex Object Storage.
> Self-hosted альтернатива (WAL-G) описана в разделе 10.8.

### 10.1. Целевые показатели и бюджет RPO/RTO

**RPO (Recovery Point Objective) — максимально допустимая потеря данных:**

| Компонент | RPO | Обоснование |
|-----------|-----|-------------|
| PostgreSQL | ≤ 1 мин | Непрерывное WAL-архивирование (`archive_timeout=60s`) |
| Object Storage | ~0 (durable) | Yandex Object Storage гарантирует durability при ACK на PUT |
| Redis | N/A | Ephemeral кэш; DB fallback обеспечивает нулевую потерю данных |
| RabbitMQ | N/A | Transactional Outbox гарантирует нулевую потерю событий |
| **Итого** | **≤ 1 мин** | Определяется PostgreSQL WAL-архивированием |

**RTO (Recovery Time Objective) — максимальное время восстановления:**

| Фаза | Время | Описание |
|------|-------|----------|
| 1. Инфраструктура (параллельно) | 0–45 мин | PostgreSQL PITR (критический путь), Redis restart (5 мин), RabbitMQ restart (15 мин) |
| 2. Object Storage | 0–10 мин | SaaS — проверка/пересоздание bucket при необходимости |
| 3. Приложение | 45–60 мин | Обновить DSN, dm-migrate version check, запуск dm-service |
| 4. Валидация | 60–75 мин | Healthcheck, spot-checks, end-to-end тест |
| 5. Outbox drain | 75–90 мин | Публикация накопленных событий |
| 6. Буфер | 90–120 мин | Устранение непредвиденных проблем |
| **Итого** | **≤ 120 мин** | Критический путь — PostgreSQL PITR |

**Определение «восстановлен»:** DM считается восстановленным, когда:
1. `/healthz` → 200, `/readyz` → 200 (все core checks)
2. `dm_outbox_pending_count` стремится к 0
3. `dm_events_received_total` и `dm_events_processed_total{status="success"}` растут
4. `max(created_at)` в `audit_records` в пределах RPO от момента инцидента
5. Нет алертов `dm_integrity_check_failures_total`

### 10.2. PostgreSQL: backup и PITR

PostgreSQL — единственный компонент, потеря данных которого катастрофична и невосполнима. Все остальные компоненты либо ephemeral (Redis), либо восстанавливаемы из PostgreSQL (Object Storage — артефакты могут быть re-processed), либо защищены outbox-паттерном (RabbitMQ).

#### Yandex Managed PostgreSQL (рекомендуется)

**Конфигурация:**

```bash
# Включить PITR и установить retention
yc managed-postgresql cluster update contractpro-dm-prod \
  --backup-retain-period-days 14

# Рекомендуемые параметры через консоль или terraform:
# archive_timeout = 60       # WAL flush каждые 60 сек
# backup window:  02:00 UTC  # ежедневный snapshot в окно минимальной нагрузки
# wal_level = replica         # достаточно для PITR
```

**Расписание резервного копирования:**

| Тип | Инструмент | Расписание | Хранение |
|-----|-----------|-----------|---------|
| Полный snapshot | Yandex Managed PG (автоматический) | Ежедневно, 02:00 UTC | 14 дней |
| WAL-архивирование | Непрерывно (платформа) | `archive_timeout=60s` | 14 дней |
| Логический дамп | `pg_dump` (cron на jump-host) | Еженедельно, воскресенье 03:00 UTC | 4 недели, S3 |
| Экспорт схемы | `pg_dump --schema-only` | При каждом деплое | В git (migrations/) |
| Restore test | `restore-test.sh` (CI/CD) | Ежемесячно, 1-е число | Результат → Prometheus |

#### Логический дамп (дополнительная страховка)

```bash
#!/usr/bin/env bash
# logical-dump.sh — запуск через cron еженедельно
set -euo pipefail

DATE=$(date +%Y%m%d_%H%M%S)
DB_HOST="c-<cluster-id>.rw.mdb.yandexcloud.net"

# Роль dm_backup_ro: CONNECT + SELECT, без DML
pg_dump \
  --host="${DB_HOST}" --port=6432 \
  --username=dm_backup_ro --dbname=dm_prod \
  --format=directory --compress=6 --jobs=4 \
  --no-privileges --no-owner \
  --file="/tmp/dm_dump_${DATE}"

aws s3 sync "/tmp/dm_dump_${DATE}" \
  "s3://contractpro-dm-backups/logical-dumps/${DATE}/" \
  --endpoint-url=https://storage.yandexcloud.net --sse=AES256

rm -rf "/tmp/dm_dump_${DATE}"
```

> **RLS и pg_dump:** RLS-политики из миграции 000003 имеют fallback `current_setting('app.organization_id', true) = ''` → все строки видны при пустом GUC. Для полного дампа используйте роль с `BYPASSRLS` или суперпользователя.

> **Партиции audit_records:** `pg_dump` корректно дампит партиционированную таблицу: родительскую DDL + каждую партицию + индексы + триггеры. Партиция `audit_records_default` (миграция 000004) должна присутствовать после восстановления.

### 10.3. Runbook восстановления PostgreSQL (PITR)

**Сценарий:** требуется откатить данные к метке `TARGET_TIME` (например, после ошибочного массового удаления).

#### Шаг 0. Оценка инцидента

```bash
# Определить границу повреждения
psql "${DM_DB_DSN}" -c "
  SELECT created_at, action, actor_id, details
  FROM audit_records
  WHERE created_at > now() - interval '2 hours'
  ORDER BY created_at DESC LIMIT 100;"

# TARGET_TIME = за 1 минуту до первой проблемной записи
```

#### Шаг 1. Создать кластер из PITR

```bash
BACKUP_ID=$(yc managed-postgresql backup list \
  --cluster-id "<source-cluster-id>" \
  --format json | jq -r 'sort_by(.created_at) | last | .id')

yc managed-postgresql cluster restore \
  --backup-id "${BACKUP_ID}" \
  --time "2026-04-05T14:22:00Z" \
  --name "dm-prod-pitr-$(date +%Y%m%d)" \
  --environment production \
  --network-id "<vpc-network-id>" \
  --resource-preset s3-c2-m8 \
  --disk-type network-ssd --disk-size 50 \
  --host zone-id=ru-central1-a,subnet-id=<subnet-id>
```

> Время создания: 15–30 мин для БД до 10 ГБ. Yandex автоматически replay'ит WAL до указанного `--time`.

```bash
# Ожидать готовности кластера (15–30 мин)
# Повторять до status = RUNNING
yc managed-postgresql cluster get \
  --name "dm-prod-pitr-$(date +%Y%m%d)" --format json \
  | jq -r '.status'
```

#### Шаг 2. Проверить целостность

```bash
PITR_DSN="postgres://dm_prod:<pass>@<pitr-host>:6432/dm_prod?sslmode=require"

# Schema version (ожидаемо: version=5, dirty=false)
psql "${PITR_DSN}" -c "SELECT version, dirty FROM schema_migrations;"

# Документы
psql "${PITR_DSN}" -c "SELECT count(*) FROM documents WHERE deleted_at IS NULL;"

# RLS-политики (ожидаемо: 5 строк)
psql "${PITR_DSN}" -c "
  SELECT tablename, policyname FROM pg_policies
  WHERE tablename IN ('documents','document_versions','artifact_descriptors',
    'version_diff_references','audit_records');"

# Append-only триггеры (ожидаемо: no_update_delete_audit, no_truncate_audit)
psql "${PITR_DSN}" -c "
  SELECT tgname, tgenabled FROM pg_trigger
  WHERE tgrelid = 'audit_records'::regclass;"

# Партиции audit_records (ожидаемо: ≥1, включая default)
psql "${PITR_DSN}" -c "
  SELECT c.relname FROM pg_inherits i
  JOIN pg_class c ON c.oid = i.inhrelid
  WHERE i.inhparent = 'audit_records'::regclass ORDER BY c.relname;"
```

#### Шаг 3. Переключить приложение

```bash
# Остановить dm-service
docker stop dm-service
sleep 35  # drain in-flight (DM_SHUTDOWN_TIMEOUT)

# Обновить DM_DB_DSN в .env.prod / Vault / K8s Secret
# DM_DB_DSN=postgres://dm_prod:<pass>@<pitr-host>:6432/dm_prod?sslmode=require

# Проверить schema version
docker run --rm --env-file .env.prod \
  contractpro/dm-service:${DM_IMAGE_TAG} \
  /usr/local/bin/dm-migrate version

# Запустить dm-service
docker run -d --name dm-service --env-file .env.prod \
  -p 8080:8080 -p 9090:9090 --restart always \
  contractpro/dm-service:${DM_IMAGE_TAG}

# Проверить readiness
curl -sf http://localhost:8080/readyz | jq .
```

#### Шаг 4. Постинцидентные действия

```bash
# 1. Проверить outbox-события, потерянные в разрыве (от TARGET_TIME до переключения)
#    Запросить из старого кластера:
psql "<old-dsn>" -c "
  SELECT event_id, topic, created_at FROM outbox_events
  WHERE created_at > '2026-04-05T14:22:00Z' AND status = 'CONFIRMED'
  ORDER BY created_at;"

# 2. При необходимости — replay через DLQ API

# 3. Переименовать старый кластер, удалить через 48 часов
yc managed-postgresql cluster update <old-cluster-id> \
  --name "dm-prod-OLD-pre-pitr-$(date +%Y%m%d)"
```

#### golang-migrate schema_migrations при восстановлении

| Сценарий | Состояние schema_migrations | Действие |
|----------|---------------------------|---------|
| PITR (физический бэкап) | Восстанавливается как есть | `dm-service` проверит при старте; `dirty=true` → fail fast |
| Логический дамп (pg_restore) | Из дампа | Если версия совпадает — `dm-migrate up` не нужен |
| Пустой кластер | Отсутствует | `dm-migrate up` создаст схему |
| `dirty=true` | Ручное исправление | `UPDATE schema_migrations SET dirty = false;` → повторить `dm-migrate up` |

### 10.4. Object Storage: S3 versioning и восстановление

#### Включение S3 versioning

```bash
# Включить versioning на production bucket
yc storage bucket update \
  --name contractpro-dm-artifacts-prod \
  --versioning versioning-enabled

# Проверить
yc storage bucket get --name contractpro-dm-artifacts-prod | grep versioning
```

При включённом versioning:
- `DELETE` не удаляет объект физически — вставляет delete marker
- `PUT` создаёт новую версию; старая остаётся доступной по version ID
- Объекты восстанавливаемы через удаление delete marker

#### Lifecycle policy для версий

```bash
# Удалять non-current версии через 30 дней (баланс стоимости и защиты)
aws s3api put-bucket-lifecycle-configuration \
  --endpoint-url https://storage.yandexcloud.net \
  --bucket contractpro-dm-artifacts-prod \
  --lifecycle-configuration '{
    "Rules": [
      {
        "ID": "expire-noncurrent-versions",
        "Status": "Enabled",
        "Filter": {"Prefix": ""},
        "NoncurrentVersionExpiration": {"NoncurrentDays": 30}
      },
      {
        "ID": "remove-expired-delete-markers",
        "Status": "Enabled",
        "Filter": {"Prefix": ""},
        "Expiration": {"ExpiredObjectDeleteMarker": true}
      }
    ]
  }'
```

#### Восстановление при случайном удалении объектов

```bash
# 1. Найти delete markers
aws s3api list-object-versions \
  --endpoint-url https://storage.yandexcloud.net \
  --bucket contractpro-dm-artifacts-prod \
  --prefix "<org_id>/<doc_id>/" \
  --query "DeleteMarkers[?IsLatest==\`true\`].{Key:Key,VersionId:VersionId}"

# 2. Удалить delete marker для восстановления объекта
aws s3api delete-object \
  --endpoint-url https://storage.yandexcloud.net \
  --bucket contractpro-dm-artifacts-prod \
  --key "<storage_key>" \
  --version-id "<delete-marker-version-id>"

# 3. Проверить content hash
aws s3api get-object \
  --endpoint-url https://storage.yandexcloud.net \
  --bucket contractpro-dm-artifacts-prod \
  --key "<storage_key>" /tmp/artifact.json
sha256sum /tmp/artifact.json
# Сравнить с: SELECT content_hash FROM artifact_descriptors WHERE storage_key = '<key>';
```

#### Защита от удаления bucket

```bash
# Bucket policy: запрет DeleteBucket для всех (снять может только IAM с s3:PutBucketPolicy)
aws s3api put-bucket-policy \
  --endpoint-url https://storage.yandexcloud.net \
  --bucket contractpro-dm-artifacts-prod \
  --policy '{
    "Version": "2012-10-17",
    "Statement": [{
      "Effect": "Deny",
      "Principal": "*",
      "Action": "s3:DeleteBucket",
      "Resource": "arn:aws:s3:::contractpro-dm-artifacts-prod"
    }]
  }'
```

#### Cross-region replication

Для v1 (≤1000 contracts/day, ~2 ГБ/день blob volume) достаточно встроенной geo-redundancy Yandex Object Storage (SLA 99.99%, репликация внутри `ru-central1`). Cross-region replication рекомендуется при:
- Регуляторные требования к географическому размещению бэкапов
- Нагрузка > 10 000 contracts/day
- Требование RTO < 30 мин для blob storage

#### Стоимость versioning

| Компонент | Оценка (месяц) |
|-----------|---------------|
| Current objects | ~60 ГБ (1000 × 4 artifacts × 500 КБ × 30 дней) |
| Non-current версии (30 дней) | ~6 ГБ (при 10% overwrite rate) |
| Overhead versioning | ~10% дополнительных затрат на хранение |

### 10.5. Redis: ephemeral кэш с DB fallback

Redis хранит idempotency-ключи с TTL 24 ч. Данные **ephemeral by design** — потеря Redis не является потерей данных системы.

#### RPO для Redis

| Данные | TTL | Последствия потери |
|--------|-----|-------------------|
| Idempotency PROCESSING | 120 с | Возможна повторная обработка; DB unique constraint предотвращает дубликаты |
| Idempotency COMPLETED | 24 ч | DB fallback: `artifact_descriptors` unique constraint |

**Эффективный RPO Redis: N/A.** Вся долговечная информация в PostgreSQL.

#### Механизм DB fallback

```
Нормальный путь (Redis доступен):
  Событие → Redis GET → ключ COMPLETED → skip, ACK
                       → ключ отсутствует → SETNX PROCESSING → обработка → SET COMPLETED

Fallback (Redis недоступен):
  Событие → Redis GET fails → WARN лог + dm_idempotency_fallback_total++
          → DB: SELECT FROM artifact_descriptors WHERE version_id AND artifact_type
          → строка есть → skip, ACK
          → строки нет → обработка → DB unique constraint защищает от дублей
```

#### Восстановление после потери Redis

1. **DM не требует ручных действий** — продолжает работу через DB fallback
2. Восстановить Redis (managed: auto-recovery, self-hosted: restart)
3. Проверить reconnection в логах dm-service
4. Убедиться, что `dm_idempotency_fallback_total` перестал расти
5. Кэш repopulate'ится естественно при обработке новых событий

#### AOF persistence (production)

```bash
# redis.conf для production
appendonly yes
appendfsync everysec    # flush каждую секунду (баланс надёжности и производительности)
auto-aof-rewrite-percentage 100
auto-aof-rewrite-min-size 64mb
```

AOF обеспечивает сохранение данных при перезапуске Redis. Для DM это **convenience** (быстрый warm-up), а не requirement — DB fallback гарантирует корректность.

**v1:** Standalone Redis + AOF. Sentinel (3 nodes) — при необходимости минимизировать latency impact от Redis outages.

### 10.6. RabbitMQ: защита через Transactional Outbox

**Quorum queues** (`x-queue-type=quorum`) обеспечивают репликацию сообщений в 3-node кластере. Потеря 1 ноды — без downtime и потери сообщений.

**Transactional Outbox** делает DM устойчивым к полной потере RabbitMQ:

| Состояние RabbitMQ | Поведение DM | Потеря данных |
|-------------------|-------------|---------------|
| Healthy | Outbox Poller публикует PENDING → CONFIRMED каждые 200 мс | — |
| Down | Poller retry, PENDING накапливаются в PostgreSQL | Нулевая |
| Recovered | Poller drains backlog в FIFO order per aggregate_id | — |
| Outage > 48 ч | Outbox cleanup пропускает PENDING (только CONFIRMED) | Нулевая, но таблица растёт |

**Восстановление:** DM auto-reconnect (backoff 1s–30s). Topology re-declare при reconnect. Мониторинг: `dm_outbox_oldest_pending_age_seconds > 300` → warning, `> 1800` → critical.

### 10.7. Валидация backup/restore

#### Расписание проверок

| Тест | Частота | Среда | Автоматизация |
|------|---------|-------|--------------|
| PostgreSQL PITR restore на новый кластер | Ежемесячно | Staging | CI/CD pipeline |
| S3 object recovery (delete + undelete) | Ежеквартально | Staging bucket | Скрипт |
| Redis loss simulation (stop → DB fallback) | Ежемесячно | Staging | Скрипт |
| RabbitMQ loss simulation (stop → outbox) | Ежемесячно | Staging | Скрипт |
| Полный DR drill (все компоненты) | Раз в полгода | DR environment | Ручной |

#### Автоматический restore test (ежемесячно)

```bash
#!/usr/bin/env bash
# restore-test.sh — запуск через CI/CD 1-го числа каждого месяца
set -euo pipefail

CLUSTER_ID="${YC_CLUSTER_ID}"
TEST_CLUSTER="dm-restore-test-$(date +%Y%m)"
START_TIME=$(date +%s)
SUCCESS=0

report_and_cleanup() {
  DURATION=$(( $(date +%s) - START_TIME ))
  # Всегда отправлять метрику (в т.ч. при failure)
  cat <<METRICS | curl --data-binary @- "${PUSHGATEWAY_URL}/metrics/job/dm_backup_restore_test" 2>/dev/null || true
dm_backup_restore_test_success ${SUCCESS}
dm_backup_restore_test_duration_seconds ${DURATION}
dm_backup_restore_test_timestamp_seconds $(date +%s)
METRICS
  yc managed-postgresql cluster delete --name "${TEST_CLUSTER}" --async 2>/dev/null || true
}
trap report_and_cleanup EXIT

# 1. Восстановить из последнего бэкапа (PITR — 5 мин назад)
BACKUP_ID=$(yc managed-postgresql backup list \
  --cluster-id "${CLUSTER_ID}" --format json \
  | jq -r 'sort_by(.created_at) | last | .id')

PITR_TIME=$(date -u -d '5 minutes ago' +%Y-%m-%dT%H:%M:%SZ 2>/dev/null \
  || date -u -v-5M +%Y-%m-%dT%H:%M:%SZ)  # Linux || macOS

yc managed-postgresql cluster restore \
  --backup-id "${BACKUP_ID}" \
  --time "${PITR_TIME}" \
  --name "${TEST_CLUSTER}" \
  --environment prestable \
  --network-id "${YC_NETWORK_ID}" \
  --host zone-id=ru-central1-a,subnet-id="${YC_SUBNET_ID}" \
  --resource-preset s2.small --disk-size 50

# 2. Ожидать готовности (до 40 мин)
ELAPSED=0
STATUS=""
while [ "${ELAPSED}" -lt 2400 ]; do
  STATUS=$(yc managed-postgresql cluster get --name "${TEST_CLUSTER}" \
    --format json | jq -r '.status')
  [ "${STATUS}" = "RUNNING" ] && break
  sleep 30; ELAPSED=$((ELAPSED + 30))
done
[ "${STATUS}" = "RUNNING" ] || { echo "FAILED: cluster did not reach RUNNING in 2400s"; exit 1; }

# 3. Получить hostname восстановленного кластера
TEST_HOST=$(yc managed-postgresql host list \
  --cluster-name "${TEST_CLUSTER}" --format json \
  | jq -r '.[0].name')
TEST_DSN="postgres://dm_prod:${DM_DB_PASSWORD}@${TEST_HOST}:6432/dm_prod?sslmode=require"

# 4. Проверки целостности
VERSION=$(psql "${TEST_DSN}" -t -c "SELECT version FROM schema_migrations;")
[ "${VERSION// /}" = "5" ] || { echo "FAILED: schema version=${VERSION}"; exit 1; }

RLS=$(psql "${TEST_DSN}" -t -c "SELECT count(*) FROM pg_policies WHERE tablename IN
  ('documents','document_versions','artifact_descriptors',
   'version_diff_references','audit_records');")
[ "${RLS// /}" = "5" ] || { echo "FAILED: RLS count=${RLS}"; exit 1; }

PARTS=$(psql "${TEST_DSN}" -t -c "SELECT count(*) FROM pg_inherits
  WHERE inhparent = 'audit_records'::regclass;")
[ "${PARTS// /}" -ge "1" ] || { echo "FAILED: partitions=${PARTS}"; exit 1; }

psql "${TEST_DSN}" -c "SELECT 'documents' AS t, count(*) FROM documents
  UNION ALL SELECT 'versions', count(*) FROM document_versions
  UNION ALL SELECT 'audit', count(*) FROM audit_records;"

SUCCESS=1
echo "PASSED in $(( $(date +%s) - START_TIME ))s"
```

#### RPO validation

1. Зафиксировать `max(created_at)` из `audit_records`
2. PITR restore к метке `(max - 15 мин)`
3. Проверить, что восстановленные данные покрывают период до `(max - 15 мин)`
4. Если разрыв > 15 мин — проверить WAL-архивирование

#### RTO validation

1. Засечь время → полный runbook PITR (шаги 1–3 из раздела 10.3)
2. Финиш при `/readyz` → 200 и первое успешное событие
3. Цель: PostgreSQL PITR < 45 мин, полный RTO < 120 мин
4. Запускать ежеквартально на staging

### 10.8. Self-hosted альтернатива: WAL-G

Для self-hosted PostgreSQL (Patroni-кластер на Yandex Compute Cloud):

```bash
# postgresql.conf
archive_mode    = on
archive_command = 'wal-g wal-push %p'
archive_timeout = 60

# WAL-G environment
WALG_S3_PREFIX=s3://contractpro-dm-wal/production
AWS_ENDPOINT=https://storage.yandexcloud.net
AWS_REGION=ru-central1
WALG_COMPRESSION_METHOD=lz4
WALG_UPLOAD_CONCURRENCY=4
```

**Cron:**

```bash
# Полный бэкап — воскресенье 02:00
0 2 * * 0  postgres  wal-g backup-push $PGDATA --full-backup

# Инкрементальный — пн-сб 02:00
0 2 * * 1-6  postgres  wal-g backup-push $PGDATA

# Очистка (хранить 2 полных бэкапа ≈ 14 дней при еженедельном full)
0 4 * * *  postgres  wal-g delete retain FULL 2 --confirm
```

**PITR с WAL-G:**

```bash
systemctl stop postgresql
wal-g backup-fetch /var/lib/postgresql/16/main LATEST

cat >> postgresql.conf <<EOF
restore_command = 'wal-g wal-fetch %f %p'
recovery_target_time = '2026-04-05 14:22:00+00'
recovery_target_action = 'promote'
EOF

touch recovery.signal
chown -R postgres:postgres /var/lib/postgresql/16/main
systemctl start postgresql
```

**pgBackRest** — альтернатива для БД > 100 ГБ (параллельный backup/restore, diff backup, `verify` command).

### 10.9. Мониторинг бэкапов

**Алерты:**

| Метрика | Порог | Severity |
|---------|-------|----------|
| Последний успешный бэкап | > 26 ч назад | Critical |
| WAL archiving lag | > 5 мин | Critical (RPO risk) |
| Backup size anomaly | Отклонение > 50% от 7-дневного среднего | Warning |
| `dm_backup_restore_test_success` | = 0 | Critical |
| `dm_outbox_oldest_pending_age_seconds` | > 300 | Warning |
| `dm_idempotency_fallback_total` rate | > 0 в течение 5 мин | Warning (Redis down) |

**SQL-мониторинг (для self-hosted, раздел 10.8):**

> На Yandex Managed PostgreSQL WAL-архивирование управляется платформой. Для мониторинга lag используйте метрики Yandex Cloud Console / API, а не `pg_stat_archiver`.

```sql
-- WAL archiving status (self-hosted с WAL-G / pgBackRest)
SELECT archived_count, last_archived_wal, last_archived_time,
  EXTRACT(EPOCH FROM (now() - last_archived_time)) AS wal_lag_seconds,
  failed_count
FROM pg_stat_archiver;

-- Database size (тренд ёмкости)
SELECT pg_size_pretty(pg_database_size('dm_prod'));

-- Audit partition sizes
SELECT c.relname, pg_size_pretty(pg_total_relation_size(c.oid))
FROM pg_inherits i JOIN pg_class c ON c.oid = i.inhrelid
WHERE i.inhparent = 'audit_records'::regclass ORDER BY c.relname;
```

### 10.10. Сводная таблица

| Показатель | Целевое значение | Как достигается | Мониторинг |
|-----------|-----------------|----------------|-----------|
| RPO | ≤ 15 мин (фактически ≤ 1 мин) | WAL-архивирование каждые 60 сек | Yandex Cloud / `pg_stat_archiver` (self-hosted) |
| RTO | ≤ 2 часа | PITR + переключение DSN + dm-migrate check | Restore test ежемесячно |
| Хранение бэкапа | 14 дней | Yandex Managed PG retain period | Алерт при пропуске > 26 ч |
| S3 versioning | 30 дней non-current | Lifecycle policy | Yandex Cloud мониторинг |
| Проверка восстановления | Ежемесячно | `restore-test.sh` → Pushgateway | `dm_backup_restore_test_success` |
| Redis recovery | Автоматический (DB fallback) | DM architecture | `dm_idempotency_fallback_total` |
| RabbitMQ recovery | Автоматический (Outbox) | DM architecture | `dm_outbox_pending_count` |

---

## 11. Справочная информация

### 11.1. Порты

| Сервис | Порт (host) | Порт (container) | Назначение | Dev | Prod |
|--------|-------------|-------------------|-----------|-----|------|
| dm-service | 8081 | 8080 | API + Health probes | + | + |
| dm-service | 9091 | 9090 | Prometheus metrics | + | + |
| PostgreSQL | 5433 | 5432 | Database | + | external |
| Redis | 6380 | 6379 | KV Store | + | external |
| RabbitMQ | 5673 | 5672 | AMQP | + | external |
| RabbitMQ | 15673 | 15672 | Management UI | + | - |
| MinIO | 9000 | 9000 | S3 API | + | - |
| MinIO | 9001 | 9001 | Console UI | + | - |

### 11.2. Make targets

```bash
make build          # Сборка dm-service
make build-migrate  # Сборка dm-migrate
make test           # go test ./...
make lint           # go vet ./...
make docker-build   # Docker image с git-based тегом
make compose-up     # docker compose up --build
make compose-down   # docker compose down
```

### 11.3. Полезные команды

```bash
# Доступ к PostgreSQL CLI
docker exec -it dm-postgres psql -U dm -d dm_dev

# Доступ к Redis CLI
docker exec -it dm-redis redis-cli

# Проверить версию миграций
docker compose run --rm dm-migrate version

# Просмотр версии образа
docker image inspect contractpro/dm-service:latest

# Очистка неиспользуемых images/volumes
docker image prune
docker volume prune

# DLQ replay (admin API)
curl -X POST http://localhost:8081/api/v1/admin/dlq/replay \
  -H "Content-Type: application/json" \
  -H "X-Organization-ID: <org_id>" \
  -H "X-User-ID: <user_id>" \
  -H "X-User-Role: admin" \
  -d '{"category": "ingestion", "limit": 10}'
```

### 11.4. Переменные окружения

Полный список: [`configuration.md`](./configuration.md)

Часто используемые:

```env
DM_LOG_LEVEL=info|debug|warn|error
DM_HTTP_PORT=8080
DM_METRICS_PORT=9090
DM_CONSUMER_PREFETCH=10
DM_CONSUMER_CONCURRENCY=5
DM_DB_MAX_CONNS=25
DM_IDEMPOTENCY_TTL=24h
DM_OUTBOX_POLL_INTERVAL=200ms
DM_SHUTDOWN_TIMEOUT=30s
DM_STALE_VERSION_TIMEOUT=30m
DM_RATELIMIT_READ_RPS=100
DM_RATELIMIT_WRITE_RPS=20
```

---

## 12. Ссылки

- [`configuration.md`](./configuration.md) — полная справка переменных окружения
- [`migration-strategy.md`](./migration-strategy.md) — стратегия миграций PostgreSQL
- [`high-architecture.md`](./high-architecture.md) — архитектура Document Management
- [`security.md`](./security.md) — безопасность, RLS, аудит
- [`observability.md`](./observability.md) — логирование, метрики, трейсинг
- Раздел 10 настоящего документа — Backup & Disaster Recovery (BRE-021/REV-033)
- `docker-compose.yaml` — конфигурация для development
- `Dockerfile` — Docker build конфигурация
- `.env.example` — пример переменных окружения
- `Makefile` — build команды
