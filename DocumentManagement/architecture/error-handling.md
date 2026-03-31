# Статусы, ошибки и отказоустойчивость Document Management

## 1. Внешние статусы

DM использует общий набор внешних статусов **только в контексте long-running operations**, где DM участвует как обработчик (например, при обработке batch-загрузки, если такой сценарий будет добавлен). В v1 DM не инициирует long-running operations от имени пользователя — он сервисный домен.

Однако DM публикует `artifact_status` версии, который является **domain-specific набором**:

| Статус | Описание |
|--------|----------|
| `PENDING` | Версия создана, артефакты не получены |
| `PROCESSING_ARTIFACTS_RECEIVED` | Артефакты DP сохранены |
| `ANALYSIS_ARTIFACTS_RECEIVED` | Результаты LIC сохранены |
| `REPORTS_READY` | Отчёты RE сохранены |
| `FULLY_READY` | Все этапы завершены |
| `PARTIALLY_AVAILABLE` | Часть артефактов доступна, ошибка на одном из этапов |

## 2. Внутренние технические статусы

Для observability DM отслеживает внутренние стадии обработки события:

| Стадия | Описание |
|--------|----------|
| `RECEIVED` | Событие получено, десериализовано |
| `IDEMPOTENCY_CHECK` | Проверка идемпотентности |
| `VALIDATING` | Валидация входных данных (существование документа/версии) |
| `PERSISTING_BLOB` | Запись blob в Object Storage |
| `PERSISTING_METADATA` | Запись метаданных в PostgreSQL |
| `PUBLISHING_CONFIRMATION` | Публикация confirmation event |
| `PUBLISHING_NOTIFICATION` | Публикация notification event |
| `COMPLETED` | Обработка завершена |
| `FAILED` | Ошибка (с указанием стадии) |

Эти стадии не хранятся в DB — только в structured logs и metrics. Они нужны для:
- дашбордов (время на каждой стадии);
- дебага (на какой стадии произошёл сбой);
- алертов (аномальное время на стадии `PERSISTING_BLOB`).

## 3. Retryable / non-retryable errors

| Ошибка | Retryable | Действие |
|--------|-----------|----------|
| Object Storage timeout/5xx | Да | Retry 3 раза, exponential backoff (1s, 2s, 4s) |
| PostgreSQL connection error | Да | NACK, requeue |
| Redis connection error | — | Fallback на DB-проверку, продолжить |
| RabbitMQ publish error | Да | Retry 3 раза; при неудаче — запись в outbox |
| Document not found | Нет | Publish PersistFailed, ACK |
| Version not found | Нет | Publish PersistFailed, ACK |
| Invalid event schema | Нет | DLQ, ACK |
| Artifact already exists (constraint violation) | Нет | Idempotent: повторная публикация confirmation, ACK |
| Organization mismatch | Нет | Publish PersistFailed, ACK, alert |

## 4. DLQ strategy

**Топики DLQ:**

| DLQ topic | Описание |
|-----------|----------|
| `dm.dlq.ingestion-failed` | Неудачный приём артефактов (после retry) |
| `dm.dlq.query-failed` | Неудачное чтение (semantic tree request) |
| `dm.dlq.invalid-message` | Невалидная схема сообщения |

**DLQ record содержит:**
- Оригинальное сообщение (raw JSON).
- `error_code`, `error_message`.
- `original_topic`.
- `retry_count`.
- `failed_at` timestamp.
- `correlation_id`, `job_id`.

**Обработка DLQ:**
- Manual review через operational dashboard.
- Replay capability: оператор может переместить сообщение обратно в основную очередь.
- Alert при появлении сообщений в DLQ.

## 5. Timeout policy

| Операция | Timeout | При срабатывании |
|----------|---------|-----------------|
| Object Storage PutObject | 30s | Retry |
| Object Storage GetObject | 15s | Retry |
| PostgreSQL query | 10s | Retry (если connection), fail (если query) |
| Overall event processing | 60s | Abort, NACK |
| Redis operation | 2s | Fallback на DB |
| RabbitMQ publish | 10s | Outbox fallback |
