# Наблюдаемость и эксплуатация Document Management

## 1. Logging

**Формат:** Structured JSON (единый с DP).

**Обязательные поля в каждой log entry:**
- `timestamp`
- `level` (DEBUG, INFO, WARN, ERROR)
- `service` = `document-management`
- `correlation_id`
- `job_id` (если применимо)
- `document_id` (если применимо)
- `version_id` (если применимо)
- `organization_id`
- `component` (Event Consumer, Artifact Ingestion Service, etc.)
- `stage` (внутренняя стадия)
- `message`
- `error` (если есть)

**Уровни:**
- INFO: успешное сохранение артефакта, создание версии, confirmation published.
- WARN: retry, fallback Redis → DB, slow query.
- ERROR: persist failed, DLQ, unexpected error.

## 2. Metrics

**Формат:** Prometheus.

| Метрика | Тип | Labels | Описание |
|---------|-----|--------|----------|
| `dm_events_received_total` | counter | `event_type`, `source_domain` | Принятые события |
| `dm_events_processed_total` | counter | `event_type`, `status` (success/error) | Обработанные события |
| `dm_event_processing_duration_seconds` | histogram | `event_type`, `stage` | Время обработки по стадиям |
| `dm_artifacts_stored_total` | counter | `artifact_type`, `producer_domain` | Сохранённые артефакты |
| `dm_artifact_size_bytes` | histogram | `artifact_type` | Размер артефактов |
| `dm_object_storage_duration_seconds` | histogram | `operation` (put/get/delete) | Время операций с Object Storage |
| `dm_db_query_duration_seconds` | histogram | `operation`, `table` | Время SQL-запросов |
| `dm_idempotency_hits_total` | counter | `event_type` | Дедуплицированные события |
| `dm_dlq_messages_total` | counter | `reason` | Сообщения в DLQ |
| `dm_api_requests_total` | counter | `method`, `path`, `status_code` | HTTP-запросы |
| `dm_api_request_duration_seconds` | histogram | `method`, `path` | Время HTTP-ответов |
| `dm_versions_created_total` | counter | `origin_type` | Созданные версии |
| `dm_outbox_pending_count` | gauge | — | Непубликованные записи в outbox |

## 3. Tracing

**Формат:** OpenTelemetry (единый с DP).

**Spans:**
- `dm.event.process` — обработка входящего события (parent span).
  - `dm.idempotency.check`
  - `dm.validate`
  - `dm.object_storage.put` (per artifact)
  - `dm.db.transaction`
  - `dm.publish.confirmation`
  - `dm.publish.notification`
- `dm.api.request` — обработка HTTP-запроса (parent span).
  - `dm.db.query`
  - `dm.object_storage.get` (если чтение blob)

**Propagation:** `correlation_id` из входящих событий пробрасывается как trace context.

## 4. Alerts

| Alert | Условие | Severity | Действие |
|-------|---------|----------|----------|
| DM DLQ non-empty | `dm_dlq_messages_total` увеличивается | WARNING | Проверить DLQ dashboard, replay |
| DM event processing slow | `dm_event_processing_duration_seconds` p95 > 10s | WARNING | Проверить Object Storage / DB |
| DM Object Storage errors | Error rate > 1% за 5 мин | CRITICAL | Проверить Yandex Object Storage |
| DM DB connection pool exhausted | Available connections = 0 | CRITICAL | Scale up / check queries |
| DM API p95 latency > 500ms | `dm_api_request_duration_seconds` p95 > 0.5s | WARNING | Проверить DB queries, indexes |
| DM outbox backlog growing | `dm_outbox_pending_count` > 100 | WARNING | Проверить RabbitMQ connectivity |
| DM orphan blobs count growing | orphan cleanup detects > 50 orphans/hour | WARNING | Проверить DB transaction failures |

## 5. Operational dashboards

1. **DM Overview:** events received/processed rate, success/error ratio, processing latency distribution.
2. **Storage Health:** Object Storage latency, error rate, blob sizes. DB query latency, connection pool usage.
3. **Artifact Pipeline:** артефакты по типам и producer-доменам, artifact_status distribution по версиям.
4. **DLQ Monitor:** количество сообщений, breakdown по reason, replay history.
5. **API Performance:** request rate, latency percentiles, error codes.
6. **Tenant Activity:** documents created, versions created, artifacts stored — по организациям.
