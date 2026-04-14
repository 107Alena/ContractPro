# Наблюдаемость и эксплуатация API/Backend Orchestrator

В рамках документа описана стратегия наблюдаемости (observability) для компонента **API/Backend Orchestrator** сервиса **ContractPro**. Стратегия включает структурированное логирование, метрики (Prometheus), распределённую трассировку (OpenTelemetry), алертинг и операционные дашборды.

Документ согласован с единой стратегией наблюдаемости DP и DM — формат логов, формат метрик и схема трассировки унифицированы для сквозной корреляции запросов через все домены.

---

## 1. Logging

### 1.1 Формат

**Structured JSON** — единый формат с DP и DM. Все логи пишутся в stdout в формате JSON (один JSON-объект на строку). Агрегация и ротация — на уровне инфраструктуры (Docker log driver → Loki / ELK / CloudWatch).

### 1.2 Обязательные поля в каждой log entry

| Поле | Тип | Описание |
|------|-----|----------|
| `timestamp` | string (ISO 8601) | Время события в UTC |
| `level` | string | Уровень: `DEBUG`, `INFO`, `WARN`, `ERROR` |
| `service` | string | Константа `api-orchestrator` |
| `correlation_id` | string (UUID) | Сквозной идентификатор запроса; генерируется оркестратором при первом HTTP-запросе, пробрасывается в DM (HTTP header), DP (поле события) |
| `organization_id` | string (UUID) | Идентификатор организации из JWT-claims |
| `user_id` | string (UUID) | Идентификатор пользователя из JWT-claims |
| `component` | string | Компонент оркестратора: `http-router`, `jwt-auth`, `rbac`, `rate-limiter`, `upload-coordinator`, `status-tracker`, `results-aggregator`, `comparison-coordinator`, `recheck-coordinator`, `export-service`, `feedback-service`, `admin-proxy`, `dm-client`, `command-publisher`, `sse-broadcaster`, `event-consumer`, `s3-client`, `redis-client`, `broker-client`, `health-check` |
| `message` | string | Человекочитаемое описание события |
| `error` | string / null | Текст ошибки (если есть); `null` при успешных операциях |

### 1.3 Опциональные контекстные поля

В зависимости от компонента и контекста операции добавляются:

| Поле | Когда добавляется |
|------|-------------------|
| `document_id` | Операции с документами/договорами |
| `version_id` | Операции с версиями |
| `job_id` | Публикация команд в DP, приём DP-событий |
| `request_method` | HTTP-запросы (`GET`, `POST`, `PUT`, `DELETE`) |
| `request_path` | HTTP-запросы (путь без query string) |
| `status_code` | HTTP-ответы |
| `duration_ms` | Время выполнения операции |
| `event_type` | Обработка async-событий |
| `source_domain` | Домен-источник события (`DP`, `DM`) |

### 1.4 Уровни логирования

| Уровень | Назначение | Примеры |
|---------|------------|---------|
| `DEBUG` | Детальная отладочная информация (отключен в production) | Содержимое JWT-claims (без токена), детали маршрутизации запроса, параметры downstream-вызовов |
| `INFO` | Успешные операции, значимые бизнес-события | Файл загружен (`upload completed`), документ создан в DM, команда опубликована в DP, результаты прочитаны пользователем, отчёт экспортирован, SSE-событие отправлено, SSE-соединение установлено/закрыто |
| `WARN` | Деградация, повторные попытки, медленные операции | Retry DM-запроса, fallback при недоступности OPM, slow query (> 500ms), rate limit приближается к лимиту, SSE-reconnect, circuit breaker half-open |
| `ERROR` | Сбои, потери данных, невосстановимые ошибки | DM недоступен (circuit breaker open), S3 upload failed (после всех retry), publish в RabbitMQ failed, DLQ message, JWT validation error (некорректная подпись), внутренняя ошибка оркестратора (panic recovery) |

### 1.5 Политика обращения с чувствительными данными

**Запрещено логировать:**
- JWT-токены (access token, refresh token) — ни целиком, ни частично.
- Содержимое загружаемых файлов.
- Персональные данные пользователей (ФИО, email, телефон).
- Содержимое артефактов и отчётов.
- Пароли, API-ключи, секреты.

**Разрешено логировать:**
- `user_id` (UUID) — для корреляции действий пользователя.
- `organization_id` (UUID) — для tenant-сегментации логов.
- `document_id`, `version_id`, `job_id` — для сквозной трассировки.
- Имя файла (`file_name`) — для диагностики проблем загрузки.
- MIME-тип и размер файла — для статистики и диагностики.

### 1.6 Пример log entry

```json
{
  "timestamp": "2026-04-06T10:15:32.847Z",
  "level": "INFO",
  "service": "api-orchestrator",
  "correlation_id": "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
  "organization_id": "org-001-uuid",
  "user_id": "user-042-uuid",
  "component": "upload-coordinator",
  "document_id": "doc-123-uuid",
  "version_id": "ver-456-uuid",
  "message": "contract upload completed: file uploaded to S3, version created in DM, processing command published to DP",
  "duration_ms": 1230,
  "error": null
}
```

### 1.7 Объём логов

Оценка объёма: **~50 log entries на жизненный цикл одного договора** (от загрузки до полной готовности результатов).

Расчёт при нагрузке 1000 договоров/сутки:
- ~50 000 log entries/сутки от оркестратора.
- При среднем размере entry ~500 байт: ~25 МБ/сутки (без сжатия).
- С учётом SSE и polling-запросов: до 100 000 entries/сутки (~50 МБ/сутки).

Рекомендации:
- Retention: 30 дней в горячем хранилище, 90 дней в архиве.
- Уровень `DEBUG` отключен в production (включается per-instance при диагностике).

---

## 2. Metrics

### 2.1 Формат

**Prometheus** — единый с DP и DM. Метрики доступны на endpoint `/metrics` (HTTP GET, без аутентификации).

Все метрики используют prefix `orch_` для отделения от метрик других доменов (`dp_`, `dm_`).

### 2.2 Таблица метрик

#### 2.2.1 HTTP-уровень

| Метрика | Тип | Labels | Описание |
|---------|-----|--------|----------|
| `orch_http_requests_total` | counter | `method`, `path`, `status_code` | Общее количество HTTP-запросов. `path` — нормализованный шаблон (например, `/api/v1/contracts/:id`, не фактический ID). |
| `orch_http_request_duration_seconds` | histogram | `method`, `path` | Время обработки HTTP-запроса (от получения до ответа). Buckets: 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2, 5, 10. |
| `orch_http_request_size_bytes` | histogram | `method` | Размер тела входящего запроса. Актуально для upload-эндпоинтов. Buckets: 1KB, 10KB, 100KB, 1MB, 5MB, 10MB, 20MB. |

#### 2.2.2 Загрузка файлов

| Метрика | Тип | Labels | Описание |
|---------|-----|--------|----------|
| `orch_upload_total` | counter | `status` (`success`, `error`) | Количество загрузок файлов (завершённый multi-step upload: S3 + DM + DP). |
| `orch_upload_size_bytes` | histogram | — | Размер загружаемых файлов. Buckets: 100KB, 500KB, 1MB, 5MB, 10MB, 15MB, 20MB. |
| `orch_upload_duration_seconds` | histogram | — | Общее время загрузки (от получения файла до публикации команды в DP). Buckets: 0.5, 1, 2, 5, 10, 30, 60. |

#### 2.2.3 Downstream: DM Client

| Метрика | Тип | Labels | Описание |
|---------|-----|--------|----------|
| `orch_dm_requests_total` | counter | `method`, `path`, `status_code` | HTTP-запросы к DM REST API. `path` — нормализованный шаблон DM API. |
| `orch_dm_request_duration_seconds` | histogram | `method`, `path` | Время ответа DM. Buckets: 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2, 5. |
| `orch_dm_circuit_breaker_state` | gauge | — | Текущее состояние circuit breaker для DM: `0` = closed (норма), `1` = half-open (проверка), `2` = open (DM недоступен). |

#### 2.2.4 Downstream: Object Storage (S3)

| Метрика | Тип | Labels | Описание |
|---------|-----|--------|----------|
| `orch_s3_operations_total` | counter | `operation` (`put`, `delete`), `status` (`success`, `error`) | Операции с Object Storage. `put` — загрузка файла, `delete` — cleanup при ошибке. |
| `orch_s3_operation_duration_seconds` | histogram | `operation` | Время операции с S3. Buckets: 0.1, 0.25, 0.5, 1, 2, 5, 10, 30. |

#### 2.2.5 Downstream: RabbitMQ (Broker)

| Метрика | Тип | Labels | Описание |
|---------|-----|--------|----------|
| `orch_broker_publish_total` | counter | `topic`, `status` (`success`, `error`) | Публикации в RabbitMQ. `topic` — routing key (например, `dp.process-document.requested`, `dp.compare-versions.requested`). |
| `orch_broker_publish_duration_seconds` | histogram | `topic` | Время публикации сообщения. Buckets: 0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.5. |
| `orch_events_received_total` | counter | `event_type`, `source_domain` | Принятые async-события из RabbitMQ. `event_type` — тип события (например, `StatusChanged`, `ProcessingCompleted`). `source_domain` — домен-источник (`DP`, `DM`). |

#### 2.2.6 SSE (Server-Sent Events)

| Метрика | Тип | Labels | Описание |
|---------|-----|--------|----------|
| `orch_sse_connections_active` | gauge | — | Количество активных SSE-соединений на данном инстансе. |
| `orch_sse_events_pushed_total` | counter | `event_type` | Количество SSE-событий, отправленных клиентам. `event_type` — тип пользовательского события (`status_changed`, `results_ready`, `comparison_ready`). |

#### 2.2.7 Безопасность и Rate Limiting

| Метрика | Тип | Labels | Описание |
|---------|-----|--------|----------|
| `orch_rate_limit_hits_total` | counter | `endpoint_class` | Количество отклонённых запросов по rate limit. `endpoint_class` — класс эндпоинта (`upload`, `read`, `sse`, `admin`). |
| `orch_auth_failures_total` | counter | `reason` (`expired`, `invalid`, `missing`) | Неуспешные попытки аутентификации. `expired` — истёкший токен, `invalid` — невалидная подпись, `missing` — отсутствующий заголовок `Authorization`. |

#### 2.2.8 Redis

| Метрика | Тип | Labels | Описание |
|---------|-----|--------|----------|
| `orch_redis_operations_total` | counter | `operation` (`get`, `set`, `delete`, `publish`, `subscribe`), `status` (`success`, `error`) | Операции с Redis. |
| `orch_redis_operation_duration_seconds` | histogram | `operation` | Время выполнения Redis-операции. Buckets: 0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.5. |

### 2.3 Правила именования labels

- `path` — нормализованный шаблон URL (с placeholder'ами вместо конкретных ID), не фактический URL. Это предотвращает кардинальный взрыв метрик.
- `status_code` — HTTP-код ответа как строка (`200`, `201`, `400`, `401`, `403`, `404`, `500`, `502`, `503`).
- `method` — HTTP-метод в верхнем регистре (`GET`, `POST`, `PUT`, `DELETE`).
- `status` — бинарный результат операции: `success` / `error`.
- `operation` — тип инфраструктурной операции (зависит от клиента).

### 2.4 Histogram buckets

Общий принцип выбора bucket'ов — покрытие диапазона от быстрых (p50) до медленных (p99+) ответов для каждой категории:

- **HTTP request duration:** от 10ms до 10s — покрывает от быстрых GET-запросов до медленных upload.
- **DM client duration:** от 10ms до 5s — с учётом NFR-1.3 (бюджет DM: 100ms, запас: до 5s при деградации).
- **S3 operations:** от 100ms до 30s — upload крупных файлов может занимать 10–30s.
- **Broker publish:** от 1ms до 500ms — быстрая операция при нормальной работе RabbitMQ.
- **Redis operations:** от 1ms до 500ms — in-memory store, быстрые операции.

---

## 3. Tracing

### 3.1 Формат

**OpenTelemetry** — единый с DP и DM. Traces экспортируются через OTLP (gRPC) в collector (Jaeger / Tempo / Grafana Cloud).

### 3.2 Propagation

Сквозная корреляция запросов через все домены:

1. **correlation_id** генерируется оркестратором при первом HTTP-запросе пользователя.
2. При sync-вызовах в DM: `correlation_id` передаётся как HTTP-заголовок `X-Correlation-ID`, а также пробрасывается стандартный W3C `traceparent` для trace propagation.
3. При async-публикации в DP: `correlation_id` включается как поле `EventMeta.correlation_id` в теле события.
4. При получении async-событий от DP/DM: `correlation_id` извлекается из `EventMeta` и привязывается к новому span.

Таким образом, один пользовательский запрос (upload → DP processing → DM persistence → SSE push) формирует единый distributed trace с участками в оркестраторе, DM и DP.

### 3.3 Spans

#### 3.3.1 HTTP-запрос (parent span)

```
orch.http.request
├── orch.auth.validate          — валидация JWT (подпись, expiration, claims)
├── orch.rbac.check             — проверка роли для endpoint
├── orch.ratelimit.check        — проверка rate limit (Redis)
│
├── [зависит от endpoint]
│   ├── orch.upload.s3          — загрузка файла в Object Storage (PutObject)
│   ├── orch.dm.request         — HTTP-запрос к DM (per вызов; может быть несколько)
│   ├── orch.opm.request        — HTTP-запрос к OPM (proxy admin operations)
│   ├── orch.broker.publish     — публикация команды в RabbitMQ
│   └── orch.redis.operation    — операция с Redis (set upload tracking, etc.)
│
└── [ответ клиенту]
```

**Атрибуты `orch.http.request`:**
- `http.method` — HTTP-метод.
- `http.url` — нормализованный URL.
- `http.status_code` — код ответа.
- `http.request_content_length` — размер тела запроса.
- `orch.correlation_id` — сквозной correlation_id.
- `orch.organization_id` — tenant.
- `orch.user_id` — пользователь.

**Атрибуты `orch.auth.validate`:**
- `orch.auth.result` — `success` / `expired` / `invalid` / `missing`.
- `orch.auth.role` — роль из JWT при успехе.

**Атрибуты `orch.upload.s3`:**
- `orch.s3.bucket` — bucket.
- `orch.s3.key` — storage key.
- `orch.s3.content_length` — размер файла.

**Атрибуты `orch.dm.request`:**
- `http.method`, `http.url`, `http.status_code` — стандартные HTTP-атрибуты.
- `orch.dm.retry_count` — количество retry (0 при первом успехе).
- `orch.dm.circuit_breaker_state` — состояние circuit breaker перед запросом.

**Атрибуты `orch.broker.publish`:**
- `messaging.system` — `rabbitmq`.
- `messaging.destination` — exchange + routing key.
- `orch.event_type` — тип события/команды.

#### 3.3.2 Обработка async-события (parent span)

```
orch.event.process
├── orch.event.deserialize      — десериализация события
├── orch.redis.operation        — обновление состояния в Redis
└── orch.sse.broadcast          — публикация в Redis Pub/Sub для SSE-broadcast
```

**Атрибуты `orch.event.process`:**
- `orch.event_type` — тип события (`StatusChanged`, `ProcessingCompleted`, `version-reports-ready` и т.д.).
- `orch.source_domain` — домен-источник (`DP`, `DM`).
- `orch.correlation_id` — correlation_id из EventMeta.
- `orch.document_id`, `orch.version_id` — если присутствуют в событии.

#### 3.3.3 SSE-соединение (parent span)

```
orch.sse.connection
├── orch.redis.operation        — подписка на Redis Pub/Sub channel
├── orch.sse.event.push         — отправка SSE-события клиенту (per event)
└── orch.redis.operation        — отписка при disconnect
```

**Атрибуты `orch.sse.connection`:**
- `orch.organization_id` — tenant.
- `orch.user_id` — пользователь.
- `orch.sse.duration_seconds` — длительность соединения.
- `orch.sse.events_pushed` — количество отправленных событий за время соединения.

### 3.4 Sampling

Для production рекомендуется **tail-based sampling**:
- 100% для запросов с ошибками (status >= 500).
- 100% для медленных запросов (duration > 2s).
- 10% для успешных запросов (достаточно для анализа производительности).
- 100% для async-событий с ошибками обработки.

Расчёт объёма: при 1000 договоров/сутки и ~5 HTTP-запросах на договор — ~5000 traces/сутки, из которых ~500 сэмплируются полностью (при 10% success sampling).

---

## 4. Alerts

### 4.1 Критические алерты (CRITICAL)

Требуют немедленного реагирования (on-call, уведомление в Telegram/PagerDuty).

| Alert | Условие | Severity | Действие |
|-------|---------|----------|----------|
| Высокий уровень 5xx-ошибок | `rate(orch_http_requests_total{status_code=~"5.."}[5m]) / rate(orch_http_requests_total[5m]) > 0.05` в течение 5 мин | CRITICAL | Проверить доступность DM (circuit breaker), Object Storage, RabbitMQ. Проверить логи `level=ERROR`. |
| Circuit breaker DM открыт | `orch_dm_circuit_breaker_state == 2` | CRITICAL | DM недоступен. Проверить health DM (`/healthz`), сеть между оркестратором и DM, логи DM. |
| Ошибки публикации в RabbitMQ | `rate(orch_broker_publish_total{status="error"}[5m]) > 0` | CRITICAL | RabbitMQ недоступен или queue/exchange отсутствует. Проверить RabbitMQ management UI, connectivity, readiness probe. |

### 4.2 Предупреждения (WARNING)

Требуют внимания в рабочее время (уведомление в Slack/email).

| Alert | Условие | Severity | Действие |
|-------|---------|----------|----------|
| Рост ошибок загрузки | `rate(orch_upload_total{status="error"}[5m]) / rate(orch_upload_total[5m]) > 0.1` в течение 5 мин | WARNING | Проверить Object Storage (latency, errors), DM API (создание версий), сеть. |
| Высокая задержка HTTP | `histogram_quantile(0.95, rate(orch_http_request_duration_seconds_bucket[5m])) > 2` | WARNING | Проверить DM latency (`orch_dm_request_duration_seconds`), Redis latency, load на инстансе. |
| Падение SSE-соединений | `orch_sse_connections_active` снижается > 50% за 1 мин | WARNING | Проверить Redis Pub/Sub connectivity, сетевые проблемы, OOM на инстансе. |
| Всплеск ошибок аутентификации | `rate(orch_auth_failures_total[5m]) > 50/60` (> 50 в минуту) | WARNING | Возможна brute-force атака. Проверить IP-адреса источников (если доступно на reverse proxy), рассмотреть временную блокировку. |

### 4.3 Информационные алерты (INFO)

Для анализа и планирования (dashboard, дайджест).

| Alert | Условие | Severity | Действие |
|-------|---------|----------|----------|
| Всплеск rate limit | `rate(orch_rate_limit_hits_total[5m]) > 100/60` (> 100 в минуту) | INFO | Определить, легитимный ли трафик. Если да — рассмотреть повышение лимитов. Если нет — рассмотреть IP-блокировку. |

### 4.4 Конфигурация алертов

Алерты определяются как Prometheus alerting rules и обрабатываются через Alertmanager:
- **CRITICAL** — PagerDuty / Telegram on-call channel, auto-escalation через 15 мин.
- **WARNING** — Slack #contractpro-alerts channel, дежурный инженер.
- **INFO** — Slack #contractpro-ops channel, weekly digest.

Каждый алерт содержит:
- Ссылку на соответствующий dashboard.
- Runbook URL с пошаговой инструкцией по диагностике.
- Информацию о затронутых компонентах.

---

## 5. Operational dashboards

Все дашборды реализуются в Grafana, данные из Prometheus (метрики), Loki (логи), Tempo/Jaeger (traces).

### 5.1 Orchestrator Overview

**Назначение:** Общая картина состояния оркестратора.

**Панели:**
- Request rate (RPS) — `rate(orch_http_requests_total[1m])`.
- Error rate (%) — `rate(orch_http_requests_total{status_code=~"5.."}[5m]) / rate(orch_http_requests_total[5m])`.
- Latency percentiles (p50, p95, p99) — `histogram_quantile(0.50/0.95/0.99, rate(orch_http_request_duration_seconds_bucket[5m]))`.
- Upload rate — `rate(orch_upload_total[1m])`.
- Upload success/error ratio — `orch_upload_total` по label `status`.
- Active SSE connections — `orch_sse_connections_active`.
- HTTP status code distribution — stacked area по `status_code`.

**Фильтры:** time range, инстанс оркестратора.

### 5.2 Downstream Health

**Назначение:** Состояние внешних зависимостей оркестратора.

**Панели:**
- DM latency (p50, p95, p99) — `histogram_quantile(..., rate(orch_dm_request_duration_seconds_bucket[5m]))`.
- DM error rate — `rate(orch_dm_requests_total{status_code=~"5.."}[5m])`.
- DM circuit breaker state — `orch_dm_circuit_breaker_state` (с цветовой индикацией: зелёный = 0, жёлтый = 1, красный = 2).
- S3 latency (p50, p95) — `histogram_quantile(..., rate(orch_s3_operation_duration_seconds_bucket[5m]))`.
- S3 error rate — `rate(orch_s3_operations_total{status="error"}[5m])`.
- RabbitMQ publish success rate — `rate(orch_broker_publish_total{status="success"}[5m])`.
- RabbitMQ publish latency — `histogram_quantile(..., rate(orch_broker_publish_duration_seconds_bucket[5m]))`.
- Redis latency (p50, p95) — `histogram_quantile(..., rate(orch_redis_operation_duration_seconds_bucket[5m]))`.
- Redis error rate — `rate(orch_redis_operations_total{status="error"}[5m])`.

**Фильтры:** time range, downstream service, operation type.

### 5.3 User Activity

**Назначение:** Активность пользователей для бизнес-анализа и capacity planning.

**Панели:**
- Загрузки по организациям — `orch_upload_total` с группировкой по `organization_id` (из логов / отдельной метрики при необходимости).
- Просмотры результатов — `orch_http_requests_total{path="/api/v1/contracts/:id/versions/:vid/results"}`.
- Экспорт отчётов — `orch_http_requests_total{path=~"/api/v1/contracts/:id/versions/:vid/export.*"}`.
- Отправки обратной связи — `orch_http_requests_total{path="/api/v1/contracts/:id/versions/:vid/feedback"}`.
- Сравнения версий — `orch_http_requests_total{path="/api/v1/contracts/:id/compare"}`.
- Распределение по ролям (из логов) — LAWYER / BUSINESS_USER / ORG_ADMIN.

**Фильтры:** time range, organization_id, role.

### 5.4 Real-time Pipeline

**Назначение:** Мониторинг прохождения договоров через pipeline (от загрузки до готовности результатов).

**Панели:**
- Полученные события по типам — `rate(orch_events_received_total[5m])` с группировкой по `event_type`.
- Полученные события по доменам — `rate(orch_events_received_total[5m])` с группировкой по `source_domain`.
- SSE-события, отправленные клиентам — `rate(orch_sse_events_pushed_total[5m])` с группировкой по `event_type`.
- Распределение статусов (из логов) — количество версий в каждом user-facing статусе (`UPLOADED`, `QUEUED`, `PROCESSING`, `ANALYZING`, `AWAITING_USER_INPUT`, `GENERATING_REPORTS`, `READY`, `PARTIALLY_FAILED`, `FAILED`, `REJECTED`).
- Время от загрузки до READY (из traces) — `orch.http.request` upload → последнее `orch.event.process` с `version-reports-ready`.

**Фильтры:** time range, organization_id, source_domain, event_type.

### 5.5 Security

**Назначение:** Мониторинг безопасности и обнаружение аномалий.

**Панели:**
- Ошибки аутентификации — `rate(orch_auth_failures_total[5m])` с группировкой по `reason`.
- Rate limit срабатывания — `rate(orch_rate_limit_hits_total[5m])` с группировкой по `endpoint_class`.
- CORS violations (из логов) — количество запросов с невалидным Origin.
- 401/403 ответы — `rate(orch_http_requests_total{status_code=~"401|403"}[5m])`.
- Top IP-адреса с ошибками аутентификации (из логов reverse proxy, если доступно).
- Подозрительные паттерны (из логов) — повторные `invalid` JWT от одного `user_id`.

**Фильтры:** time range, reason, endpoint_class, organization_id.

---

## 6. Реализация в коде

### 6.1 Observability SDK

Оркестратор использует единый **Observability SDK** (пакет `internal/infra/observability`), совместимый с DP и DM:

```
internal/infra/observability/
  logger.go         — structured JSON logger (slog)
  metrics.go        — Prometheus metrics registry + HTTP handler
  tracing.go        — OpenTelemetry tracer provider + OTLP exporter
  middleware.go      — HTTP middleware для автоматического логирования,
                       метрик и tracing каждого запроса
```

### 6.2 Конфигурация

Observability настраивается через переменные окружения с prefix `ORCH_`:

| Переменная | Описание | Значение по умолчанию |
|-----------|----------|-----------------------|
| `ORCH_LOG_LEVEL` | Уровень логирования | `INFO` |
| `ORCH_LOG_FORMAT` | Формат логов | `json` |
| `ORCH_METRICS_ENABLED` | Включить Prometheus endpoint | `true` |
| `ORCH_METRICS_PATH` | Путь для метрик | `/metrics` |
| `ORCH_TRACING_ENABLED` | Включить OpenTelemetry tracing | `true` |
| `ORCH_TRACING_ENDPOINT` | OTLP collector endpoint | `localhost:4317` |
| `ORCH_TRACING_SAMPLE_RATE` | Базовая вероятность сэмплирования | `0.1` |
| `ORCH_SERVICE_NAME` | Имя сервиса в traces | `api-orchestrator` |

### 6.3 HTTP Middleware

Каждый HTTP-запрос автоматически:
1. Создаёт span `orch.http.request` с HTTP-атрибутами.
2. Генерирует или извлекает `correlation_id` (из заголовка `X-Correlation-ID` или нового UUID).
3. Инкрементирует `orch_http_requests_total`.
4. Записывает duration в `orch_http_request_duration_seconds`.
5. Логирует `INFO` запись при завершении (с method, path, status_code, duration_ms).
6. При status >= 500 логирует `ERROR` с деталями ошибки.

### 6.4 Graceful Shutdown

При остановке сервиса observability подсистема завершается последней (аналогично DP):
1. Health endpoint переходит в `not ready` (readiness probe).
2. Ожидание завершения текущих запросов и SSE-соединений.
3. Flush pending traces и metrics.
4. Закрытие logger (flush stdout buffer).

---

## 7. Интеграция с доменами

### 7.1 Сквозная трассировка

```
Frontend                 Orchestrator              DM                    DP
   │                         │                      │                     │
   │  POST /contracts/upload │                      │                     │
   │ ───────────────────────►│                      │                     │
   │                         │                      │                     │
   │                         │ orch.http.request     │                     │
   │                         │  ├ orch.auth.validate │                     │
   │                         │  ├ orch.upload.s3     │                     │
   │                         │  ├ orch.dm.request ──►│ dm.api.request     │
   │                         │  │   (create version) │  ├ dm.db.transaction│
   │                         │  │                  ◄─│  └ (return)        │
   │                         │  ├ orch.broker.publish │                    │
   │                         │  │   (process cmd)    │                  ──►│ dp.process
   │                         │  └ (202 Accepted)     │                     │
   │  ◄──────────────────────│                      │                     │
   │                         │                      │                     │
   │     ... async pipeline ...                     │                     │
   │                         │                      │                     │
   │                         │ orch.event.process   │                     │
   │                         │  (version-reports-   │                     │
   │                         │   ready from DM)     │                     │
   │                         │  ├ orch.sse.broadcast│                     │
   │  SSE: results_ready     │  └                   │                     │
   │  ◄──────────────────────│                      │                     │
```

Все span'ы связаны через единый `correlation_id`, что позволяет в Jaeger/Tempo просматривать полный lifecycle договора от загрузки до доставки результатов пользователю.

### 7.2 Соотношение prefix'ов метрик

| Домен | Prefix | Пример |
|-------|--------|--------|
| Document Processing | `dp_` | `dp_processing_duration_seconds` |
| Document Management | `dm_` | `dm_events_received_total` |
| API/Backend Orchestrator | `orch_` | `orch_http_requests_total` |

### 7.3 Единый log format

Все три сервиса используют идентичный JSON-формат логов с полями `timestamp`, `level`, `service`, `correlation_id`, `component`, `message`, `error`. Это позволяет в Loki/ELK выполнять запросы вида:

```
{service=~"api-orchestrator|document-management|document-processing"} | json | correlation_id="a1b2c3d4-..."
```

для просмотра полного жизненного цикла одного запроса через все домены.
