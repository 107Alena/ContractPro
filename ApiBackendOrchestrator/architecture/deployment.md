# Развертывание API/Backend Orchestrator

Документ описывает развертывание сервиса **API/Backend Orchestrator (orch-api)** в локальной среде разработки и в production используя Docker Compose.

Оркестратор — единая точка входа для frontend-приложений и внешних интеграций. Он координирует запросы между доменными сервисами (DM, DP, OPM, UOM), обеспечивает аутентификацию/авторизацию и доставляет статусные обновления через SSE.

---

## 1. Docker-образ (multi-stage build)

### 1.1. Dockerfile

Оркестратор собирается через multi-stage Docker build. Первый этап компилирует Go-бинарник, второй — формирует минимальный production-образ.

```dockerfile
# ============================================================================
# Stage 1: Build
# ============================================================================
FROM golang:1.26-alpine AS builder

RUN apk add --no-cache git ca-certificates tzdata

WORKDIR /build

# Кэширование зависимостей как отдельный слой.
COPY go.mod go.sum ./
RUN go mod download && go mod verify

# Копирование исходного кода и сборка статического бинарника.
COPY . .

ARG VERSION=dev
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-s -w -X main.version=${VERSION}" \
    -trimpath \
    -o /orch-api \
    ./cmd/orch-api/

# ============================================================================
# Stage 2: Production
# ============================================================================
FROM alpine:3.20

RUN apk --no-cache add ca-certificates tzdata \
    && addgroup -S orchapi \
    && adduser -S -G orchapi -H -D -s /sbin/nologin orchapi

COPY --from=builder /orch-api /usr/local/bin/orch-api

# API-порт (REST + SSE).
EXPOSE 8080
# Метрики (Prometheus).
EXPOSE 9090

USER orchapi:orchapi

HEALTHCHECK --interval=10s --timeout=3s --start-period=30s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:8080/healthz || exit 1

ENTRYPOINT ["orch-api"]
```

**Ключевые решения:**

| Аспект | Решение | Обоснование |
|--------|---------|-------------|
| Base image (build) | `golang:1.26-alpine` | Минимальный размер, содержит Go toolchain |
| Base image (runtime) | `alpine:3.20` | ~5 МБ, минимальная attack surface |
| `CGO_ENABLED=0` | Статическая линковка | Не требуется libc в runtime-образе |
| `-ldflags="-s -w"` | Удаление отладочной информации | Уменьшение размера бинарника на ~30% |
| `-X main.version` | Встраивание версии | Доступна через `/healthz` и логи |
| `-trimpath` | Удаление локальных путей | Воспроизводимость и безопасность |
| `USER orchapi` | Non-root | Принцип наименьших привилегий |
| `ca-certificates` | Корневые сертификаты | Для TLS-соединений к DM, OPM, UOM, S3 |
| `tzdata` | Таймзоны | Корректная работа `time.LoadLocation` |

### 1.2. Сборка образа

```bash
# Из директории ApiBackendOrchestrator/development/

# Сборка с семантическим версионированием
docker build \
    --build-arg VERSION=v1.0.0 \
    -t contractpro/orch-api:v1.0.0 \
    -t contractpro/orch-api:latest \
    .

# Сборка через Makefile (автоматический тег из git describe)
make docker-build

# Сборка с произвольным тегом
make docker-build ORCH_IMAGE_TAG=v1.0.0

# Просмотр собранных образов
docker images | grep contractpro/orch-api
```

### 1.3. Итоговый размер образа

Ожидаемый размер production-образа: **~25–35 МБ** (Alpine base ~5 МБ + Go binary ~20–30 МБ).

---

## 2. Docker Compose — локальная разработка

### 2.1. Предварительные условия

- **Docker Desktop** версии 4.0+ (с поддержкой Docker Compose v2)
- **Git** (для клонирования репозитория)
- Свободные порты: 5672, 6379, 9000, 9001, 8080, 9090
- **JWT-ключ** — RSA/ECDSA публичный ключ для валидации JWT-токенов

> **Примечание.** Оркестратор подключается к общей инфраструктуре ContractPro (RabbitMQ, Redis, MinIO). Если DP и/или DM уже запущены через их Docker Compose файлы, используйте external network или запускайте все сервисы из единого compose-файла.

### 2.2. Инициализация локального окружения

```bash
# Скопировать пример конфигурации
cp ApiBackendOrchestrator/development/.env.example ApiBackendOrchestrator/development/.env

# Отредактировать .env — заполнить обязательные переменные
nano ApiBackendOrchestrator/development/.env

# Создать директорию для JWT-ключа (для dev — самоподписанный)
mkdir -p ApiBackendOrchestrator/development/keys
# Скопировать публичный ключ UOM
cp /path/to/jwt-public.pem ApiBackendOrchestrator/development/keys/jwt-public.pem
```

Пример содержимого `.env`:

```env
# Object Storage (Yandex Object Storage / MinIO для dev)
ORCH_STORAGE_ENDPOINT=http://minio:9000
ORCH_STORAGE_BUCKET=contractpro-uploads
ORCH_STORAGE_ACCESS_KEY=minioadmin
ORCH_STORAGE_SECRET_KEY=minioadmin
ORCH_STORAGE_REGION=ru-central1

# DM Service (sync REST)
ORCH_DM_BASE_URL=http://dm-service:8081

# OPM Service (sync REST) — опционально для dev
ORCH_OPM_BASE_URL=http://opm-service:8082

# UOM Service (sync REST) — опционально для dev
ORCH_UOM_BASE_URL=http://uom-service:8083

# JWT
ORCH_JWT_PUBLIC_KEY_PATH=/keys/jwt-public.pem

# Остальные переменные используют значения по умолчанию
# Полный список — см. configuration.md
```

### 2.3. Docker Compose файл (Development)

```yaml
# =============================================================================
# ContractPro Orchestrator — Development Environment
# =============================================================================
# Usage:
#   docker compose up --build        — build and start all services
#   docker compose up -d             — start in background
#   docker compose logs -f orch-api  — follow orchestrator logs
#   docker compose down              — stop and remove containers
#   docker compose down -v           — stop and remove containers + volumes
#
# Prerequisites:
#   cp ApiBackendOrchestrator/development/.env.example \
#      ApiBackendOrchestrator/development/.env
#   # Fill in required variables in .env
# =============================================================================

services:

  # ---------------------------------------------------------------------------
  # Infrastructure
  # ---------------------------------------------------------------------------

  rabbitmq:
    image: rabbitmq:3-management-alpine
    container_name: cp-rabbitmq
    ports:
      - "5672:5672"       # AMQP
      - "15672:15672"     # Management UI: http://localhost:15672 (guest/guest)
    environment:
      RABBITMQ_DEFAULT_USER: guest
      RABBITMQ_DEFAULT_PASS: guest
    volumes:
      - rabbitmq-data:/var/lib/rabbitmq
    healthcheck:
      test: ["CMD", "rabbitmq-diagnostics", "-q", "ping"]
      interval: 10s
      timeout: 5s
      retries: 5
      start_period: 30s

  redis:
    image: redis:7-alpine
    container_name: cp-redis
    ports:
      - "6379:6379"
    volumes:
      - redis-data:/data
    healthcheck:
      test: ["CMD", "redis-cli", "ping"]
      interval: 5s
      timeout: 3s
      retries: 5

  minio:
    image: minio/minio:latest
    container_name: cp-minio
    command: server /data --console-address ":9001"
    ports:
      - "9000:9000"       # S3 API
      - "9001:9001"       # MinIO Console: http://localhost:9001 (minioadmin/minioadmin)
    environment:
      MINIO_ROOT_USER: minioadmin
      MINIO_ROOT_PASSWORD: minioadmin
    volumes:
      - minio-data:/data
    healthcheck:
      test: ["CMD", "mc", "ready", "local"]
      interval: 10s
      timeout: 5s
      retries: 5
      start_period: 10s

  # ---------------------------------------------------------------------------
  # Application
  # ---------------------------------------------------------------------------

  orch-api:
    build:
      context: ./ApiBackendOrchestrator/development
    container_name: cp-orch-api
    env_file:
      - ./ApiBackendOrchestrator/development/.env
    environment:
      # Override addresses to point to compose network services.
      ORCH_BROKER_ADDRESS: amqp://guest:guest@rabbitmq:5672/
      ORCH_REDIS_ADDRESS: redis:6379
      ORCH_STORAGE_ENDPOINT: http://minio:9000
      ORCH_STORAGE_ACCESS_KEY: minioadmin
      ORCH_STORAGE_SECRET_KEY: minioadmin
      ORCH_JWT_PUBLIC_KEY_PATH: /keys/jwt-public.pem
      ORCH_LOG_LEVEL: debug
    ports:
      - "8080:8080"       # REST API + SSE
      - "9090:9090"       # Prometheus metrics
    volumes:
      - ./ApiBackendOrchestrator/development/keys:/keys:ro
    depends_on:
      rabbitmq:
        condition: service_healthy
      redis:
        condition: service_healthy
      minio:
        condition: service_healthy
    healthcheck:
      test: ["CMD", "wget", "--no-verbose", "--tries=1", "--spider", "http://localhost:8080/healthz"]
      interval: 10s
      timeout: 3s
      retries: 3
      start_period: 15s

volumes:
  rabbitmq-data:
  redis-data:
  minio-data:
```

### 2.4. Запуск и проверка

```bash
# Собрать образы и запустить все сервисы
docker compose up --build

# Или запустить в фоновом режиме
docker compose up --build -d

# Посмотреть логи оркестратора
docker compose logs -f orch-api

# Проверить статус контейнеров
docker compose ps

# Проверить liveness probe (должен вернуть 200)
curl -i http://localhost:8080/healthz

# Проверить readiness probe (должен вернуть 200)
curl -i http://localhost:8080/readyz

# Посмотреть Prometheus метрики
curl http://localhost:9090/metrics

# RabbitMQ Management UI: http://localhost:15672 (guest/guest)
# MinIO Console:          http://localhost:9001  (minioadmin/minioadmin)

# Остановить все сервисы (оставить volumes)
docker compose down

# Остановить все сервисы и удалить volumes
docker compose down -v
```

### 2.5. Структура локального развертывания

```
docker-compose.yaml (из корня проекта)
├── rabbitmq:3-management-alpine (cp-rabbitmq)
│   ├── Ports: 5672 (AMQP), 15672 (Management UI)
│   ├── Credentials: guest/guest
│   └── Volume: rabbitmq-data
├── redis:7-alpine (cp-redis)
│   ├── Port: 6379
│   └── Volume: redis-data
├── minio (cp-minio)
│   ├── Ports: 9000 (S3 API), 9001 (Console)
│   ├── Credentials: minioadmin/minioadmin
│   └── Volume: minio-data
└── orch-api (cp-orch-api)
    ├── Port 8080: REST API + SSE + Health/Readiness probes
    ├── Port 9090: Prometheus metrics
    ├── Volume: keys/ (JWT public key, read-only)
    ├── Env: из .env + переопределение в compose
    └── Depends on: rabbitmq (healthy), redis (healthy), minio (healthy)
```

**Сетевая топология (development):**

```
                  ┌──────────────────────────────────────────────────┐
                  │       Docker Compose Network (default)           │
                  │                                                  │
  localhost:8080 ─┤─► orch-api:8080  (REST + SSE)                   │
  localhost:9090 ─┤─► orch-api:9090  (Metrics)                      │
                  │       │    │    │                                │
                  │       │    │    └── minio:9000 ◄── localhost:9000│
                  │       │    └─────── redis:6379 ◄── localhost:6379│
                  │       └──────────── rabbitmq:5672 ◄─ localhost:5672
                  │                                                  │
                  └──────────────────────────────────────────────────┘
```

---

## 3. Docker Compose — production

### 3.1. Предварительные условия

- **Docker** + **Docker Compose v2** (или Kubernetes — требуется адаптация)
- **Credentials** для сервисов:
  - RabbitMQ (отдельные production credentials)
  - Redis (пароль)
  - Yandex Object Storage (access key / secret key)
  - JWT public key (RSA/ECDSA)
- **TLS-сертификат** для reverse proxy (nginx / Traefik / облачный LB)
- Мониторинг и централизованное логирование (рекомендуется)

### 3.2. Подготовка production конфигурации

```bash
# Создать .env.prod с production переменными
cat > .env.prod << 'EOF'
# Object Storage (Yandex Object Storage)
ORCH_STORAGE_ENDPOINT=https://storage.yandexcloud.net
ORCH_STORAGE_BUCKET=contractpro-uploads-prod
ORCH_STORAGE_ACCESS_KEY=<PRODUCTION_ACCESS_KEY>
ORCH_STORAGE_SECRET_KEY=<PRODUCTION_SECRET_KEY>
ORCH_STORAGE_REGION=ru-central1

# DM Service
ORCH_DM_BASE_URL=http://dm-service:8081

# OPM Service
ORCH_OPM_BASE_URL=http://opm-service:8082

# UOM Service
ORCH_UOM_BASE_URL=http://uom-service:8083

# JWT
ORCH_JWT_PUBLIC_KEY_PATH=/keys/jwt-public.pem

# Timeouts
ORCH_HTTP_READ_TIMEOUT=30s
ORCH_HTTP_WRITE_TIMEOUT=60s
ORCH_SHUTDOWN_TIMEOUT=30s

# Rate Limiting
ORCH_RATE_LIMIT_READ=100
ORCH_RATE_LIMIT_WRITE=20

# SSE
ORCH_SSE_HEARTBEAT_INTERVAL=15s
EOF

# Защитить файл конфигурации
chmod 600 .env.prod
```

### 3.3. Docker Compose файл (Production)

```yaml
# =============================================================================
# ContractPro Orchestrator — Production Environment
# =============================================================================
# Usage:
#   docker compose -f docker-compose.orch.prod.yaml up -d
#   docker compose -f docker-compose.orch.prod.yaml logs -f orch-api
#   docker compose -f docker-compose.orch.prod.yaml down
#
# Required environment variables (set in shell or CI/CD):
#   RABBITMQ_USER, RABBITMQ_PASS — broker credentials
#   REDIS_PASSWORD               — Redis password
#   ORCH_IMAGE_TAG               — image version tag (default: latest)
#
# Yandex Cloud credentials go into .env.prod
# =============================================================================

services:

  # ---------------------------------------------------------------------------
  # Infrastructure (shared with DP/DM or external)
  # ---------------------------------------------------------------------------

  rabbitmq:
    image: rabbitmq:3-alpine
    container_name: cp-rabbitmq
    ports:
      - "5672:5672"
    environment:
      RABBITMQ_DEFAULT_USER: ${RABBITMQ_USER:?set RABBITMQ_USER}
      RABBITMQ_DEFAULT_PASS: ${RABBITMQ_PASS:?set RABBITMQ_PASS}
    volumes:
      - rabbitmq-data:/var/lib/rabbitmq
    restart: always
    healthcheck:
      test: ["CMD", "rabbitmq-diagnostics", "-q", "ping"]
      interval: 30s
      timeout: 10s
      retries: 3
      start_period: 40s
    logging:
      driver: json-file
      options:
        max-size: "10m"
        max-file: "3"
    deploy:
      resources:
        limits:
          memory: 512M
          cpus: "1.0"

  redis:
    image: redis:7-alpine
    container_name: cp-redis
    command: >
      redis-server
      --requirepass ${REDIS_PASSWORD:?set REDIS_PASSWORD}
      --maxmemory 512mb
      --maxmemory-policy allkeys-lru
      --appendonly yes
    ports:
      - "6379:6379"
    environment:
      REDISCLI_AUTH: ${REDIS_PASSWORD:?set REDIS_PASSWORD}
    volumes:
      - redis-data:/data
    restart: always
    healthcheck:
      test: ["CMD", "redis-cli", "ping"]
      interval: 10s
      timeout: 5s
      retries: 5
    logging:
      driver: json-file
      options:
        max-size: "10m"
        max-file: "3"
    deploy:
      resources:
        limits:
          memory: 768M
          cpus: "0.5"

  # ---------------------------------------------------------------------------
  # Application
  # ---------------------------------------------------------------------------

  orch-api:
    image: contractpro/orch-api:${ORCH_IMAGE_TAG:-latest}
    container_name: cp-orch-api
    env_file:
      - .env.prod
    environment:
      ORCH_BROKER_ADDRESS: amqp://${RABBITMQ_USER}:${RABBITMQ_PASS}@rabbitmq:5672/
      ORCH_REDIS_ADDRESS: redis:6379
      ORCH_REDIS_PASSWORD: ${REDIS_PASSWORD}
      ORCH_JWT_PUBLIC_KEY_PATH: /keys/jwt-public.pem
      ORCH_LOG_LEVEL: info
      ORCH_LOG_FORMAT: json
    ports:
      - "8080:8080"       # REST API + SSE
      - "9090:9090"       # Prometheus metrics
    volumes:
      - ./keys/jwt-public.pem:/keys/jwt-public.pem:ro
    depends_on:
      rabbitmq:
        condition: service_healthy
      redis:
        condition: service_healthy
    restart: always
    logging:
      driver: json-file
      options:
        max-size: "20m"
        max-file: "5"
    deploy:
      resources:
        limits:
          memory: 512M
          cpus: "1.0"

volumes:
  rabbitmq-data:
  redis-data:
```

### 3.4. Запуск production развертывания

```bash
# Установить credentials (в shell, CI/CD или .env.prod)
export RABBITMQ_USER="contractpro-prod"
export RABBITMQ_PASS="<SECURE_PASSWORD>"
export REDIS_PASSWORD="<SECURE_PASSWORD>"
export ORCH_IMAGE_TAG="v1.0.0"

# Запустить в фоновом режиме
docker compose -f docker-compose.orch.prod.yaml up -d

# Проверить статус
docker compose -f docker-compose.orch.prod.yaml ps

# Следить за логами
docker compose -f docker-compose.orch.prod.yaml logs -f orch-api

# Остановить
docker compose -f docker-compose.orch.prod.yaml down
```

### 3.5. TLS-конфигурация

Оркестратор сам **не терминирует TLS**. TLS termination выполняется на уровне reverse proxy или облачного балансировщика нагрузки перед оркестратором.

**Рекомендуемая конфигурация (nginx):**

```
                   TLS termination          Internal network (HTTP)
  Клиент ──────► nginx (443) ──────────► orch-api:8080
                   │
                   ├── SSL certificate
                   ├── HTTP/2
                   ├── Proxy headers: X-Forwarded-For, X-Real-IP
                   └── Connection upgrade для SSE
```

**Требования к reverse proxy:**

| Параметр | Значение | Обоснование |
|----------|----------|-------------|
| `proxy_read_timeout` | `3600s` | SSE-соединения живут долго (часы) |
| `proxy_buffering` | `off` | SSE требует небуферизированной передачи |
| `proxy_set_header Connection` | `""` | Предотвращение закрытия keep-alive |
| `X-Forwarded-For` | `$proxy_add_x_forwarded_for` | Для audit logging |
| `X-Forwarded-Proto` | `$scheme` | Для корректной генерации URL |
| Upload limit | `25m` | 20 МБ файл + overhead multipart encoding |

### 3.6. Отличия development от production

| Аспект | Development | Production |
|--------|-------------|-----------|
| RabbitMQ | С Management UI (`:3-management-alpine`) | Без Management UI (`:3-alpine`) |
| RabbitMQ credentials | `guest/guest` | Из env vars |
| Redis | Без пароля | С паролем + `maxmemory` |
| Redis `maxmemory` | Не задано | `512mb` (SSE + rate limits + tracking) |
| Object Storage | MinIO (local) | Yandex Object Storage |
| Логирование | stdout (text) | JSON-file (ротация) |
| Log level | `debug` | `info` |
| Перезагрузка | Нет | `restart: always` |
| Лимиты ресурсов | Нет | Включены (512 МБ RAM, 1 CPU) |
| TLS | Нет | Reverse proxy с TLS termination |
| JWT key | Self-signed (dev) | Production key от UOM |

---

## 4. Health checks

Оркестратор предоставляет три HTTP-эндпоинта для orchestrator-уровня health checking:

### 4.1. Liveness probe — `/healthz`

**Назначение:** Проверка, что процесс жив и способен обрабатывать запросы.

```bash
curl -i http://localhost:8080/healthz
# HTTP/1.1 200 OK
# Content-Type: application/json
#
# {"status":"ok","version":"v1.0.0"}
```

**Семантика:** Всегда возвращает 200, если HTTP-сервер запущен. Не проверяет зависимости.

**Конфигурация в Docker / Kubernetes:**

```yaml
# Docker Compose
healthcheck:
  test: ["CMD", "wget", "--no-verbose", "--tries=1", "--spider", "http://localhost:8080/healthz"]
  interval: 10s
  timeout: 3s
  retries: 3
  start_period: 15s

# Kubernetes
livenessProbe:
  httpGet:
    path: /healthz
    port: 8080
  initialDelaySeconds: 10
  periodSeconds: 10
  timeoutSeconds: 3
  failureThreshold: 3
```

### 4.2. Readiness probe — `/readyz`

**Назначение:** Проверка, что сервис готов принимать трафик. Проверяет доступность всех критических зависимостей.

```bash
curl -i http://localhost:8080/readyz
# HTTP/1.1 200 OK
# Content-Type: application/json
#
# {
#   "status": "ok",
#   "checks": {
#     "redis": "ok",
#     "rabbitmq": "ok",
#     "dm": "ok"
#   }
# }
```

**Проверяемые зависимости:**

| Зависимость | Проверка | Timeout |
|-------------|----------|---------|
| Redis | `PING` | 2s |
| RabbitMQ | TCP connection check | 2s |
| DM | `GET /healthz` | 3s |

**При недоступности:**

```bash
curl -i http://localhost:8080/readyz
# HTTP/1.1 503 Service Unavailable
# Content-Type: application/json
#
# {
#   "status": "not_ready",
#   "checks": {
#     "redis": "ok",
#     "rabbitmq": "ok",
#     "dm": "error: connection refused"
#   }
# }
```

**Конфигурация:**

```yaml
# Docker Compose
healthcheck:
  test: ["CMD", "wget", "--no-verbose", "--tries=1", "--spider", "http://localhost:8080/readyz"]
  interval: 5s
  timeout: 3s
  retries: 3
  start_period: 15s

# Kubernetes
readinessProbe:
  httpGet:
    path: /readyz
    port: 8080
  initialDelaySeconds: 5
  periodSeconds: 5
  timeoutSeconds: 3
  failureThreshold: 3
```

### 4.3. Startup probe (Kubernetes)

**Назначение:** Предотвращение преждевременного kill контейнера во время длительного старта.

```yaml
# Kubernetes
startupProbe:
  httpGet:
    path: /healthz
    port: 8080
  periodSeconds: 5
  timeoutSeconds: 3
  failureThreshold: 30    # 30 * 5s = 150s максимум на старт
```

**Сценарии длительного старта:**
- Загрузка JWT-ключа с файловой системы.
- Установление соединения с RabbitMQ (может требовать DNS resolution, TLS handshake).
- Установление соединения с Redis.
- Первичная проверка доступности DM.

---

## 5. Graceful shutdown

Оркестратор реализует упорядоченное завершение работы при получении сигналов `SIGTERM` или `SIGINT`. Общий бюджет времени на shutdown: `ORCH_SHUTDOWN_TIMEOUT` (по умолчанию **30 секунд**).

### 5.1. Последовательность фаз

```
SIGTERM
  │
  ▼
Phase 1: Mark not ready                              [0s]
  │  readiness probe → 503
  │  балансировщик перестаёт направлять новые запросы
  │
  ▼
Phase 2: Stop accepting new connections               [0–1s]
  │  HTTP listener.Close() — новые TCP-соединения отклоняются
  │
  ▼
Phase 3: Drain in-flight HTTP requests                [до 10s]
  │  Ожидание завершения текущих REST-запросов
  │  Timeout: ORCH_HTTP_DRAIN_TIMEOUT (default 10s)
  │
  ▼
Phase 4: Close SSE connections                        [1s]
  │  Отправка close-события всем SSE-клиентам:
  │    event: close
  │    data: {"reason":"server_shutdown"}
  │  Закрытие SSE-соединений
  │  Клиенты получат событие и переподключатся к другому инстансу
  │
  ▼
Phase 5: Stop RabbitMQ consumers                      [до 5s]
  │  Прекращение consuming новых сообщений
  │  Ожидание ACK для in-flight сообщений
  │  Закрытие channel и connection
  │
  ▼
Phase 6: Close Redis connections                      [1s]
  │  Отписка от Pub/Sub channels
  │  Закрытие connection pool
  │
  ▼
Phase 7: Close HTTP server                            [1s]
  │  Финальная остановка HTTP-сервера
  │  Освобождение портов 8080, 9090
  │
  ▼
Phase 8: Flush observability                          [1s]
  │  Сброс OpenTelemetry traces на collector
  │  Flush буфера логов
  │  Финальный push метрик (если push gateway)
  │
  ▼
Phase 9: Exit                                         [0s]
  │  os.Exit(0)
```

### 5.2. Бюджет времени

| Фаза | Максимальное время | Описание |
|------|-------------------|----------|
| 1. Mark not ready | ~0s | Атомарная установка флага |
| 2. Stop listener | ~1s | Закрытие TCP listener |
| 3. Drain HTTP | до 10s | Ожидание in-flight запросов |
| 4. Close SSE | ~1s | Уведомление и закрытие SSE |
| 5. Stop RabbitMQ | до 5s | Drain in-flight ACKs |
| 6. Close Redis | ~1s | Закрытие пула соединений |
| 7. Close HTTP server | ~1s | Финальная остановка |
| 8. Flush observability | ~1s | Сброс traces/logs/metrics |
| **Итого (worst case)** | **~21s** | **Укладывается в 30s бюджет** |

### 5.3. Docker Compose и shutdown

```bash
# Docker Compose отправляет SIGTERM, затем ждёт stop_grace_period (default 10s).
# Для оркестратора требуется увеличить до 35s:
```

В `docker-compose.yaml` для production:

```yaml
orch-api:
  # ...
  stop_grace_period: 35s    # ORCH_SHUTDOWN_TIMEOUT (30s) + 5s запас
```

**Команды:**

```bash
# Graceful shutdown (SIGTERM → ожидание → SIGKILL если не завершился)
docker compose down

# Принудительное завершение (SIGKILL — не рекомендуется)
docker compose kill orch-api
```

### 5.4. Kubernetes и shutdown

```yaml
spec:
  terminationGracePeriodSeconds: 35    # Должно быть >= ORCH_SHUTDOWN_TIMEOUT + запас
  containers:
    - name: orch-api
      lifecycle:
        preStop:
          exec:
            command: ["sh", "-c", "sleep 5"]   # Даёт время LB убрать pod из rotation
```

---

## 6. Зависимости и порядок запуска

### 6.1. Граф зависимостей

```
                    ┌───────────┐
                    │  orch-api │
                    └─────┬─────┘
                          │
            ┌─────────────┼─────────────────┬────────────────┐
            │             │                 │                │
            ▼             ▼                 ▼                ▼
       ┌─────────┐  ┌──────────┐   ┌──────────────┐  ┌────────────┐
       │  Redis   │  │ RabbitMQ │   │  DM Service  │  │ Object     │
       │ (ready)  │  │ (ready)  │   │  (reachable) │  │ Storage    │
       └─────────┘  └──────────┘   └──────────────┘  │ (reachable)│
                                                      └────────────┘
```

### 6.2. Требования к зависимостям при старте

| # | Зависимость | Требование | Проверка | Поведение при недоступности |
|---|-------------|-----------|----------|----------------------------|
| 1 | **Redis** | Доступен и отвечает на `PING` | Подключение при старте | Сервис не стартует (fatal error). Redis необходим для rate limiting, SSE broadcast, upload tracking. |
| 2 | **RabbitMQ** | Доступен, возможно создание queue/exchange | Подключение + declare | Сервис не стартует (fatal error). RabbitMQ необходим для публикации команд DP и получения событий. |
| 3 | **DM** | Доступен по HTTP (`/healthz` = 200) | HTTP GET при readiness check | Сервис стартует, но `readyz` возвращает 503. Без DM оркестратор не может обслуживать большинство запросов. |
| 4 | **Object Storage** | Bucket существует и доступен | HEAD bucket при старте | Сервис стартует с warning. Upload-запросы будут завершаться ошибкой 502. |
| 5 | **JWT public key** | Файл доступен по `ORCH_JWT_PUBLIC_KEY_PATH` | Чтение и парсинг при старте | Сервис не стартует (fatal error). Без ключа невозможна аутентификация. |

### 6.3. Docker Compose — порядок запуска

Docker Compose обеспечивает порядок через `depends_on` + `condition: service_healthy`:

```yaml
orch-api:
  depends_on:
    rabbitmq:
      condition: service_healthy    # Ждёт rabbitmq-diagnostics ping
    redis:
      condition: service_healthy    # Ждёт redis-cli ping
    # DM — не обязательный depends_on, проверяется через readiness probe
```

**Типичное время запуска зависимостей:**

| Зависимость | Время до healthy | Примечание |
|-------------|-----------------|------------|
| Redis | 2–5s | Быстрый старт |
| RabbitMQ | 15–30s | Erlang VM + plugin загрузка |
| MinIO | 5–10s | Инициализация bucket |
| orch-api | 1–3s (после зависимостей) | Загрузка JWT-ключа, подключение |

### 6.4. Retry-стратегия при старте

Для транзиентных ошибок подключения при старте оркестратор реализует retry:

| Зависимость | Max retries | Backoff | Total timeout |
|-------------|-------------|---------|---------------|
| Redis | 10 | Exponential (100ms → 5s) | ~30s |
| RabbitMQ | 10 | Exponential (100ms → 5s) | ~30s |
| JWT key file | 1 | — | Immediate fail |

---

## 7. Ссылки

- [`configuration.md`](./configuration.md) — полная справка переменных окружения
- [`high-architecture.md`](./high-architecture.md) — архитектура API/Backend Orchestrator
- `docker-compose.yaml` — конфигурация для development
- `docker-compose.orch.prod.yaml` — конфигурация для production
- `ApiBackendOrchestrator/development/Dockerfile` — Docker build конфигурация
- `ApiBackendOrchestrator/development/Makefile` — build команды
