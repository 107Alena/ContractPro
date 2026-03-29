# Развертывание Document Processing

Документ описывает развертывание сервиса **Document Processing (dp-worker)** в локальной среде разработки и в production используя Docker Compose.

---

## 1. Быстрый старт (локальная разработка)

### 1.1. Предварительные условия

- **Docker Desktop** версии 4.0+ (с поддержкой Docker Compose v2)
- **Git** (для клонирования репозитория)
- Учетные данные **Yandex Cloud** для сервисов:
  - Yandex Object Storage (S3)
  - Yandex Cloud Vision OCR

### 1.2. Инициализация локального окружения

Из корневой директории проекта:

```bash
# Скопировать пример конфигурации
cp DocumentProcessing/development/.env.example DocumentProcessing/development/.env

# Отредактировать .env и заполнить учетные данные Yandex Cloud
# Обязательные переменные:
#   DP_STORAGE_ENDPOINT
#   DP_STORAGE_BUCKET
#   DP_STORAGE_ACCESS_KEY
#   DP_STORAGE_SECRET_KEY
#   DP_OCR_ENDPOINT
#   DP_OCR_API_KEY
#   DP_OCR_FOLDER_ID
#   DP_KVSTORE_ADDRESS
#   DP_BROKER_ADDRESS

nano DocumentProcessing/development/.env  # или любой другой редактор
```

Пример содержимого `.env`:

```env
# Yandex Object Storage
DP_STORAGE_ENDPOINT=https://storage.yandexcloud.net
DP_STORAGE_BUCKET=contractpro-artifacts
DP_STORAGE_ACCESS_KEY=AKIA1234567890ABCD
DP_STORAGE_SECRET_KEY=s3cr3tk3yf0rst0rag3=
DP_STORAGE_REGION=ru-central1

# Yandex Cloud Vision OCR
DP_OCR_ENDPOINT=https://ocr.api.cloud.yandex.net
DP_OCR_API_KEY=AQVNj0zK8_5xYz...
DP_OCR_FOLDER_ID=b1cs234567890abcdefg

# KV Store (Redis) — переопределяется в docker-compose для указания сервиса в сети
DP_KVSTORE_ADDRESS=redis:6379

# Брокер сообщений (RabbitMQ) — переопределяется в docker-compose
DP_BROKER_ADDRESS=amqp://guest:guest@rabbitmq:5672/

# Остальные параметры используют значения по умолчанию или устанавливаются в docker-compose
```

### 1.3. Запуск развертывания (Development)

Из корневой директории проекта:

```bash
# Собрать образы и запустить все сервисы
docker compose up --build

# Или запустить в фоновом режиме
docker compose up --build -d

# Посмотреть логи dp-worker
docker compose logs -f dp-worker

# Посмотреть логи всех сервисов
docker compose logs -f

# Остановить все сервисы (оставить volumes)
docker compose down

# Остановить все сервисы и удалить volumes
docker compose down -v
```

### 1.4. Проверка развертывания

После запуска `docker compose up`:

```bash
# Проверить статус контейнеров
docker compose ps

# Проверить liveness probe (должен вернуть 200)
curl -i http://localhost:8080/healthz

# Проверить readiness probe (должен вернуть 200)
curl -i http://localhost:8080/readyz

# Посмотреть Prometheus метрики
curl http://localhost:9090/metrics

# Получить доступ к RabbitMQ Management UI
# http://localhost:15672
# Логин: guest
# Пароль: guest

# Проверить Redis
docker exec cp-redis redis-cli ping
# Должен вернуть: PONG
```

### 1.5. Структура локального развертывания

```
docker-compose.yaml (из корня проекта)
├── rabbitmq:3-management-alpine
│   ├── Ports: 5672 (AMQP), 15672 (Management UI)
│   ├── Management UI: http://localhost:15672 (guest/guest)
│   └── Volume: rabbitmq-data:/var/lib/rabbitmq
├── redis:7-alpine
│   ├── Port: 6379
│   └── Volume: redis-data:/data
└── dp-worker (из ./DocumentProcessing/development/Dockerfile)
    ├── Port 8080: Health/readiness probes
    ├── Port 9090: Prometheus metrics
    ├── Env: загружается из .env + переопределяется в compose
    └── Depends on: rabbitmq (healthy), redis (healthy)
```

**Сетевая топология:**
- RabbitMQ доступен как `rabbitmq:5672` в сети контейнеров
- Redis доступен как `redis:6379` в сети контейнеров
- Все сервисы находятся в одной Docker Compose сети (создается автоматически)

---

## 2. Production развертывание

### 2.1. Предварительные условия

- **Docker** (контейнеры) + **Docker Compose v2** (оркестрация)
  - Или **Kubernetes** (адаптация команд требуется)
- **Credentials** для сервисов:
  - Yandex Cloud (Object Storage, OCR)
  - Безопасное хранилище для паролей (env vars, secrets, vault)
- Мониторинг и логирование (опционально, но рекомендуется)

### 2.2. Подготовка production конфигурации

Из корневой директории проекта:

```bash
# Создать .env.prod с production переменными
cat > .env.prod << 'EOF'
# Yandex Object Storage (production bucket)
DP_STORAGE_ENDPOINT=https://storage.yandexcloud.net
DP_STORAGE_BUCKET=contractpro-artifacts-prod
DP_STORAGE_ACCESS_KEY=<PRODUCTION_ACCESS_KEY>
DP_STORAGE_SECRET_KEY=<PRODUCTION_SECRET_KEY>
DP_STORAGE_REGION=ru-central1

# Yandex Cloud Vision OCR
DP_OCR_ENDPOINT=https://ocr.api.cloud.yandex.net
DP_OCR_API_KEY=<PRODUCTION_OCR_API_KEY>
DP_OCR_FOLDER_ID=<PRODUCTION_FOLDER_ID>

# Остальные переменные из configuration.md (опционально переопределяются)
EOF

# Защитить файл конфигурации
chmod 600 .env.prod
```

### 2.3. Build и push production образа

```bash
# Сборка образа (из DocumentProcessing/development/)
cd DocumentProcessing/development

# Собрать с тегом версии
make docker-build DP_IMAGE_TAG=v1.0.0

# Или с автоматическим тегом из git
make docker-build

# Просмотреть собранные образы
docker images | grep contractpro/dp-worker

# Push в реестр (если используется Docker Hub/регистр)
# docker push contractpro/dp-worker:v1.0.0
# docker push contractpro/dp-worker:latest
```

### 2.4. Запуск production развертывания

Из корневой директории проекта:

```bash
# Установить переменные окружения для credentials
export RABBITMQ_USER="contractpro-prod"
export RABBITMQ_PASS="<SECURE_PASSWORD>"
export REDIS_PASSWORD="<SECURE_PASSWORD>"
export DP_IMAGE_TAG="v1.0.0"

# Или положить их в shell profile / CI/CD переменные

# Запустить в фоновом режиме
docker compose -f docker-compose.prod.yaml up -d

# Проверить статус
docker compose -f docker-compose.prod.yaml ps

# Следить за логами
docker compose -f docker-compose.prod.yaml logs -f dp-worker

# Остановить
docker compose -f docker-compose.prod.yaml down
```

### 2.5. Структура production развертывания

```
docker-compose.prod.yaml (из корня проекта)
├── rabbitmq:3-alpine (без Management UI)
│   ├── Port: 5672
│   ├── Credentials: из env vars (RABBITMQ_USER/PASS)
│   ├── Restart: always
│   ├── Logging: json-file (max-size 10m, max-file 3)
│   └── Resources: memory 512M, CPU 1.0
├── redis:7-alpine
│   ├── Port: 6379
│   ├── Password: из env var (REDIS_PASSWORD)
│   ├── Maxmemory: 256MB, LRU eviction
│   ├── AOF persistence: enabled
│   ├── Restart: always
│   ├── Logging: json-file (max-size 10m, max-file 3)
│   └── Resources: memory 512M, CPU 0.5
└── dp-worker (pre-built image)
    ├── Image: contractpro/dp-worker:${DP_IMAGE_TAG:-latest}
    ├── Ports: 8080 (health), 9090 (metrics)
    ├── Env: из .env.prod + переопределено в compose
    ├── Restart: always
    ├── Logging: json-file (max-size 20m, max-file 5)
    ├── Resources: memory 1G, CPU 2.0
    └── Depends on: rabbitmq (healthy), redis (healthy)
```

**Отличия от Development:**

| Аспект | Development | Production |
|--------|-------------|-----------|
| RabbitMQ | С Management UI | Без Management UI |
| RabbitMQ тег | `:3-management-alpine` | `:3-alpine` |
| Credentials | `guest/guest` | Из env vars |
| Логирование | stdout | JSON-file (ротация) |
| Перезагрузка | нет | `restart: always` |
| Лимиты ресурсов | нет | Включены |
| Log level | debug | info |
| Yandex Credentials | .env файл | .env.prod файл |

### 2.6. Health Checks и мониторинг

```bash
# Проверить liveness (всегда 200 если процесс запущен)
curl -i http://localhost:8080/healthz

# Проверить readiness (200 если сервис готов, 503 при запуске/выключении)
curl -i http://localhost:8080/readyz

# Собрать Prometheus метрики
curl http://localhost:9090/metrics

# Проверить состояние контейнеров
docker compose -f docker-compose.prod.yaml ps

# Получить детальные логи контейнера
docker compose -f docker-compose.prod.yaml logs --tail=100 dp-worker
```

---

## 3. Graceful Shutdown

Сервис dp-worker корректно обрабатывает сигналы SIGTERM и SIGINT с таймаутом на выключение 30 секунд.

**Последовательность shutdown:**

1. **Mark not ready** — readiness probe начинает возвращать 503
   - Балансировщики нагрузки отводят новые соединения
   - Время: ~1 сек

2. **Close broker connection** — подключение к RabbitMQ закрывается
   - In-flight сообщения обрабатываются (timeout 120s по умолчанию)
   - Соединение gracefully закрывается
   - Время: зависит от in-flight сообщений

3. **Stop HTTP servers** — остановка health и metrics серверов
   - Текущие HTTP запросы завершаются
   - Время: ~2 сек

4. **Close KV store** — закрытие Redis соединения
   - Пулл соединений gracefully закрывается
   - Время: ~1 сек

5. **Flush observability** — сброс traces, logs, metrics
   - OpenTelemetry traces отправляются на collector (если enabled)
   - Время: ~2 сек

**Docker Compose behavior:**

```bash
# Отправляет SIGTERM (15 сек timeout по умолчанию)
docker compose down

# Отправляет SIGKILL после timeout (принудительное завершение)
docker compose kill

# Graceful при SIGTERM (используется в K8s, Systemd и т.д.)
docker stop <container>
```

---

## 4. Управление версиями и обновления

### 4.1. Тегирование образов

```bash
# Сборка с семантическим версионированием
make docker-build DP_IMAGE_TAG=v1.0.0
make docker-build DP_IMAGE_TAG=v1.0.1
make docker-build DP_IMAGE_TAG=v1.1.0

# Сборка с версией из git tags
make docker-build  # Использует git describe

# Сборка с меткой для разработки
make docker-build DP_IMAGE_TAG=dev
```

### 4.2. Rolling update (в production)

```bash
# Запустить новую версию контейнера
DP_IMAGE_TAG=v1.0.1 docker compose -f docker-compose.prod.yaml up -d

# Docker Compose заменит старый контейнер на новый (с graceful shutdown)
# Порядок остановки/запуска:
# 1. Старый контейнер получает SIGTERM
# 2. Старый контейнер gracefully завершается (30 сек timeout)
# 3. Новый контейнер запускается
# 4. Проверяются health checks
# 5. Трафик переводится на новый контейнер

# Проверить статус обновления
docker compose -f docker-compose.prod.yaml ps
docker compose -f docker-compose.prod.yaml logs --tail=50 dp-worker
```

### 4.3. Откат на предыдущую версию

```bash
# Если обновление привело к ошибке:
DP_IMAGE_TAG=v1.0.0 docker compose -f docker-compose.prod.yaml up -d

# Старая версия будет запущена вместо новой
```

---

## 5. Troubleshooting

### 5.1. Контейнер не запускается

```bash
# Проверить логи контейнера
docker compose logs dp-worker
docker compose logs rabbitmq
docker compose logs redis

# Распространенные ошибки:
# - "connection refused" → RabbitMQ/Redis не готовы (проверить healthcheck)
# - "missing required env var" → .env файл не загружен или неполный
# - "address already in use" → порт занят другим сервисом

# Убедиться, что контейнеры запущены
docker ps

# Перезапустить всё с чистого листа
docker compose down -v
docker compose up --build
```

### 5.2. Проблемы с сетью

```bash
# Проверить сетевую топологию
docker network ls
docker network inspect <compose-network-name>

# Проверить подключение между контейнерами
docker exec cp-dp-worker ping rabbitmq
docker exec cp-dp-worker ping redis

# Если "host not found", значит контейнер не находится в compose сети
```

### 5.3. Высокое использование памяти

```bash
# Проверить использование памяти
docker stats

# Если Redis превышает maxmemory в production:
# - Уменьшить DP_IDEMPOTENCY_TTL (по умолчанию 24h)
# - Увеличить REDIS_MAXMEMORY в docker-compose.prod.yaml
# - Использовать отдельный Redis instance для production

# Если dp-worker превышает лимит памяти (1G):
# - Снизить DP_CONCURRENCY_MAX_JOBS (обработка меньше задач одновременно)
# - Проверить логи на утечки (memory leaks)
```

### 5.4. RabbitMQ недоступен из dp-worker

```bash
# Проверить, что RabbitMQ запущен и здоров
docker compose ps rabbitmq

# Проверить healthcheck RabbitMQ
docker compose logs rabbitmq | grep -i health

# Если healthcheck падает:
# - Увеличить start_period в docker-compose.yaml (например, 60s)
# - Проверить логи RabbitMQ

# Вручную проверить подключение
docker exec cp-dp-worker wget -O- http://rabbitmq:15672/api/overview
```

### 5.5. Ошибки Yandex Cloud (OCR/Storage)

```bash
# Проверить, что credentials правильно загружены
docker exec cp-dp-worker env | grep "DP_OCR\|DP_STORAGE"

# Если credentials неправильные:
# - Обновить .env (development) или .env.prod (production)
# - Перезапустить контейнер: docker compose up -d

# Если сеть недоступна:
# - Проверить, что контейнер может выходить в интернет
# - Проверить firewall правила
```

---

## 6. Продвинутые сценарии

### 6.1. Масштабирование (Multiple Workers)

Если нужны несколько экземпляров dp-worker:

```bash
# Scale в Docker Compose (требует load-balanced broker)
docker compose up --scale dp-worker=3

# Или manually в .yaml:
services:
  dp-worker-1:
    # ... config ...
  dp-worker-2:
    # ... config ...
  dp-worker-3:
    # ... config ...
```

### 6.2. Использование внешних Redis/RabbitMQ

Если Redis и RabbitMQ запущены отдельно:

```bash
# Для development (docker-compose.yaml):
# - Удалить sервисы rabbitmq и redis из docker-compose.yaml
# - Изменить dp-worker environment:
environment:
  DP_BROKER_ADDRESS: amqp://guest:guest@external-rabbitmq:5672/
  DP_KVSTORE_ADDRESS: external-redis:6379
  DP_KVSTORE_PASSWORD: password
  # ... остальное ...
# - Удалить depends_on

# Для production (docker-compose.prod.yaml):
# - Аналогично для docker-compose.prod.yaml
```

### 6.3. Трейсинг и наблюдаемость

```bash
# Включить OpenTelemetry трейсинг в .env
DP_TRACING_ENABLED=true
DP_TRACING_ENDPOINT=http://localhost:4318  # OTLP HTTP endpoint
DP_TRACING_INSECURE=true  # Только для dev!

# В production:
# DP_TRACING_ENDPOINT=https://tracing.example.com
# DP_TRACING_INSECURE=false

# Собирать метрики (доступны через http://localhost:9090/metrics)
# Используется автоматически, не требует конфигурации
```

### 6.4. Использование .env.prod с .env файлом одновременно

```bash
# Docker Compose поддерживает несколько env_file:
# env_file:
#   - .env.defaults
#   - .env.prod
#
# Приоритет (от высшего к низшему):
# 1. environment: в docker-compose.yaml
# 2. Последний env_file в списке
# 3. Первый env_file в списке
# 4. Переменные окружения shell
```

---

## 7. Справочная информация

### 7.1. Порты

| Сервис | Порт | Назначение | Development | Production |
|--------|------|-----------|-------------|-----------|
| RabbitMQ | 5672 | AMQP | ✓ | ✓ |
| RabbitMQ | 15672 | Management UI | ✓ | ✗ |
| Redis | 6379 | KV Store | ✓ | ✓ |
| dp-worker | 8080 | Health/Readiness probes | ✓ | ✓ |
| dp-worker | 9090 | Prometheus metrics | ✓ | ✓ |

### 7.2. Переменные окружения по умолчанию

Полный список смотрите в [`configuration.md`](./configuration.md).

Часто используемые:

```env
DP_LOG_LEVEL=info|debug|warning|error
DP_LIMITS_MAX_FILE_SIZE=20971520  # 20 МБ
DP_LIMITS_MAX_PAGES=100
DP_LIMITS_JOB_TIMEOUT=120s
DP_CONCURRENCY_MAX_JOBS=5
DP_KVSTORE_TIMEOUT=5s
DP_IDEMPOTENCY_TTL=24h
```

### 7.3. Полезные команды

```bash
# Просмотр версии образа
docker image inspect contractpro/dp-worker:latest

# Очистка неиспользуемых images/volumes
docker image prune
docker volume prune

# Проверить содержимое volume
docker run -it --rm -v rabbitmq-data:/data alpine sh
ls -la /data

# Получить доступ к Redis cli
docker exec -it cp-redis redis-cli

# Получить доступ к контейнеру
docker exec -it cp-dp-worker sh
```

---

## 8. Ссылки

- [`configuration.md`](./configuration.md) — полная справка переменных окружения
- [`high-architecture.md`](./high-architecture.md) — архитектура Document Processing
- `docker-compose.yaml` — конфигурация для development
- `docker-compose.prod.yaml` — конфигурация для production
- `DocumentProcessing/development/Dockerfile` — Docker build конфигурация
- `DocumentProcessing/development/Makefile` — build команды
