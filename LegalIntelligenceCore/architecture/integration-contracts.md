# Интеграции и контракты Legal Intelligence Core

Документ описывает все async-интеграции LIC с другими доменами. **Sync REST к DM или иным сервисам — отсутствует.**

Все FROZEN-контракты DM и Orchestrator переиспользуются один-в-один со ссылками на источники. LIC-собственные контракты описаны в [event-catalog.md](event-catalog.md).

---

## 1. Подписки LIC (входящие сообщения)

| Топик | Событие | Источник | Контракт (FROZEN) |
|-------|---------|----------|-------------------|
| `dm.events.version-artifacts-ready` | `VersionProcessingArtifactsReady` | DM | `DocumentManagement/architecture/event-catalog.md` §2.2 |
| `dm.events.version-created` | `VersionCreated` | DM | `DocumentManagement/architecture/event-catalog.md` §2.2 |
| `dm.responses.artifacts-provided` | `ArtifactsProvided` | DM | `DocumentManagement/architecture/event-catalog.md` §2.1 |
| `dm.responses.lic-artifacts-persisted` | `LegalAnalysisArtifactsPersisted` | DM | `DocumentManagement/architecture/event-catalog.md` §2.1 |
| `dm.responses.lic-artifacts-persist-failed` | `LegalAnalysisArtifactsPersistFailed` | DM | `DocumentManagement/architecture/event-catalog.md` §2.1 |
| `orch.commands.user-confirmed-type` | `UserConfirmedType` | Orchestrator | `ApiBackendOrchestrator/architecture/event-catalog.md` §1.3 |

### 1.1 Назначение каждой подписки

- **`version-artifacts-ready`** — главный триггер пайплайна. После получения LIC запрашивает артефакты у DM.
- **`version-created`** — кэш `parent_version_id` (драйвер решения о запуске Stage 6 / Risk Delta) и `origin_type` (opaque string DM-enum, пробрасывается в `DETAILED_REPORT.metadata.origin_type` для UI). Внутренний бинарный режим LIC `mode = parent_version_id ? "RE_CHECK" : "INITIAL"` вычисляется на лету. См. ASSUMPTION-LIC-02 в `high-architecture.md`.
- **`artifacts-provided`** — ответ DM на наш запрос артефактов; маршрутизация по `correlation_id`.
- **`lic-artifacts-persisted`** / **`lic-artifacts-persist-failed`** — подтверждение/отказ DM по нашей публикации `LegalAnalysisArtifactsReady`.
- **`user-confirmed-type`** — пользовательское подтверждение типа договора при низкой уверенности классификации (FR-2.1.3); возобновляет приостановленный pipeline.

### 1.2 Idempotency keys

| Подписка | Idempotency Key |
|----------|------------------|
| `version-artifacts-ready` | `lic-trigger:{version_id}` |
| `version-created` | `lic-version-created:{version_id}` |
| `artifacts-provided` | `lic-artifacts-resp:{correlation_id}` |
| `lic-artifacts-persisted` | `lic-persist-resp:{job_id}` |
| `lic-artifacts-persist-failed` | `lic-persist-fail:{job_id}` |
| `user-confirmed-type` | `lic-user-confirmed:{version_id}` |

TTL — 24 часа (`LIC_IDEMPOTENCY_TTL`).

### 1.3 Fan-out шеринга очередей с другими консьюмерами

`dm.events.version-artifacts-ready` — **shared topic, multiple consumers** (LIC, plus возможные observability-сервисы). Каждый consumer подписывается на свою очередь, привязанную к exchange. Реализация в RabbitMQ — `fanout exchange` либо `topic exchange` с pattern `dm.events.version-artifacts-ready` → отдельные queues:
- `lic.q.version-artifacts-ready` (LIC consumer).

Это означает: каждый consumer получает свою копию события, не конкурирует за сообщения с другими доменами.

---

## 2. Публикации LIC (исходящие сообщения)

| Топик | Событие | Потребитель | Контракт |
|-------|---------|-------------|----------|
| `lic.requests.artifacts` | `GetArtifactsRequest` | DM | FROZEN — `DocumentManagement/architecture/event-catalog.md` §1.4 |
| `lic.artifacts.analysis-ready` | `LegalAnalysisArtifactsReady` | DM | FROZEN — `DocumentManagement/architecture/event-catalog.md` §1.5 (+ extension `risk_delta`, см. ADR-LIC-05) |
| `lic.events.classification-uncertain` | `ClassificationUncertain` | Orchestrator | FROZEN — `ApiBackendOrchestrator/architecture/event-catalog.md` §2.2.2 |
| `lic.events.status-changed` | `LICStatusChangedEvent` | Orchestrator | FROZEN — `ApiBackendOrchestrator/architecture/event-catalog.md` §2.2.1 |

LIC **не публикует** отдельных `analysis-completed` / `analysis-failed` событий — единое событие `lic.events.status-changed` с разными значениями `status` покрывает все случаи (consistency с DP/RE).

### 2.1 Назначение каждой публикации

- **`lic.requests.artifacts`** — async request artifact'ов у DM. Содержит `artifact_types` — список нужных типов: базовый набор `[SEMANTIC_TREE, EXTRACTED_TEXT, DOCUMENT_STRUCTURE, PROCESSING_WARNINGS]`; при `parent_version_id != null` — дополнительный запрос `[RISK_ANALYSIS]` для родительской версии.
- **`lic.artifacts.analysis-ready`** — публикация результатов анализа в DM. Соответствует FROZEN-контракту `LegalAnalysisArtifactsReady` плюс optional поле `risk_delta` (v1.1, ADR-LIC-05).
- **`lic.events.classification-uncertain`** — сигнал Orchestrator о низкой уверенности классификации; запускает сценарий `AWAITING_USER_INPUT`.
- **`lic.events.status-changed`** — пять статусных событий пайплайна: `IN_PROGRESS` (старт), `IN_PROGRESS` (с stage=`STAGE_AWAITING_USER_CONFIRMATION`), `COMPLETED`, `FAILED`.

### 2.2 Расширение `LegalAnalysisArtifactsReady` для `RISK_DELTA`

См. ADR-LIC-05 в `high-architecture.md`. Дополнение к схеме (v1.1, backward-compatible):

```jsonc
{
  // ... все обязательные поля из v1.0 (см. DM event-catalog §1.5) ...
  "risk_delta": {
    // optional, заполняется только при parent_version_id != null + доступном parent RISK_ANALYSIS
    "base_version_id": "uuid",
    "target_version_id": "uuid",
    "added": [...],
    "removed": [...],
    "changed": [...],
    "profile_change": {...},
    "summary": "string"
  }
}
```

Поле появляется **только** при `parent_version_id != null` (любое из 5 значений `origin_type` кроме `UPLOAD`) и наличии `RISK_ANALYSIS` родительской версии. В v1.0 DM эту схему не знает — поле отбрасывается при сохранении (DM игнорирует unknown fields) с warning в логах. Когда DM добавит поддержку (отдельный TASK), `RISK_DELTA` будет сохраняться как новый `ArtifactDescriptor.artifact_type=RISK_DELTA`. До этого момента — graceful degradation: warning в `DETAILED_REPORT.warnings.RISK_DELTA_PERSIST_PENDING` и метрика `lic_risk_delta_emitted_total` для отслеживания.

### 2.3 Идентификаторы сообщений

Каждое исходящее сообщение содержит:
- `message_id` (UUID) в headers RabbitMQ — для tracing.
- `correlation_id` — наследуется из входящего события.
- `timestamp` — server time публикации.
- `job_id`, `document_id`, `version_id`, `organization_id` — переписываются из входящего envelope.

Publisher confirms (RabbitMQ feature): wait-for-ack перед удалением сообщения из internal buffer LIC.

---

## 3. Sync API LIC

**Отсутствует для бизнес-логики.**

LIC экспонирует только operational endpoints:
- `GET /healthz` — liveness.
- `GET /readyz` — readiness (Redis + RabbitMQ + хотя бы один LLM-провайдер healthy).
- `GET /metrics` — Prometheus scrape.

Бизнес-данные не экспонируются через LIC: пользователи и оркестратор читают результаты анализа из DM.

---

## 4. Envelope сообщений

Совместим с DP, DM, Orchestrator. Все события содержат:

```json
{
  "correlation_id": "uuid-v4",
  "timestamp": "ISO 8601",
  "job_id": "uuid-v4",
  "document_id": "uuid-v4",
  "version_id": "uuid-v4",
  "organization_id": "uuid-v4",
  "created_by_user_id": "uuid-v4 (если применимо)"
  // ... event-specific fields
}
```

| Поле | Источник в LIC | Поведение |
|------|-----------------|-----------|
| `correlation_id` | Из входящего `version-artifacts-ready` или `user-confirmed-type` | Пробрасывается во все исходящие |
| `job_id` | Из входящего (required в `VersionProcessingArtifactsReady`, см. DM-TASK-054) | Пробрасывается во все downstream-публикации (`LICStatusChangedEvent`, `ClassificationUncertain`, `LegalAnalysisArtifactsReady`) |
| `document_id`, `version_id` | Из входящего | Пробрасывается |
| `organization_id` | Из входящего | Tenant isolation enforcement |
| `created_by_user_id` | Из входящего `VersionProcessingArtifactsReady` (required, см. DM-TASK-054) | Audit trail; имя поля совпадает с FROZEN DM `VersionCreated.created_by_user_id` |
| `timestamp` | Server time публикации | Генерируется при публикации |

LIC **никогда** не генерирует новый `correlation_id` — всегда наследует из входящего события (для сквозного tracing).

---

## 5. Именование топиков (RabbitMQ)

Стандарт: `{домен}.{тип}.{действие}`.

| Prefix | Тип | Примеры |
|--------|-----|---------|
| `lic.requests.*` | Async request к DM | `lic.requests.artifacts` |
| `lic.artifacts.*` | Артефакты, отправляемые в DM | `lic.artifacts.analysis-ready` |
| `lic.events.*` | События для downstream-потребителей | `lic.events.status-changed`, `lic.events.classification-uncertain` |
| `lic.dlq.*` | Dead Letter Queue | `lic.dlq.consumer-failed`, `lic.dlq.publish-failed`, `lic.dlq.invalid-message`, `lic.dlq.agent-output-invalid` |

LIC **не использует** топики `lic.commands.*`: это означало бы, что кто-то даёт LIC команды, но LIC сам реактивен (триггер — DM-event); единственная команда — `orch.commands.user-confirmed-type`, и она в неймспейсе оркестратора.

---

## 6. RabbitMQ exchanges и queues

### 6.1 Exchange topology

Соглашение, унаследованное от DM/DP/Orchestrator: единый topic exchange `contractpro.events` (а также `contractpro.responses`, `contractpro.commands`, `contractpro.dlx`) с routing keys = topic name.

LIC binds to:

| Queue | Bound to exchange | Routing key | Consumer |
|-------|-------------------|-------------|----------|
| `lic.q.version-artifacts-ready` | `contractpro.events` | `dm.events.version-artifacts-ready` | LIC pipeline |
| `lic.q.version-created` | `contractpro.events` | `dm.events.version-created` | LIC version meta cache |
| `lic.q.artifacts-provided` | `contractpro.responses` | `dm.responses.artifacts-provided` | LIC DM Artifact Awaiter |
| `lic.q.lic-persist-confirm` | `contractpro.responses` | `dm.responses.lic-artifacts-persisted` | LIC DM Confirmation Awaiter |
| `lic.q.lic-persist-fail` | `contractpro.responses` | `dm.responses.lic-artifacts-persist-failed` | LIC DM Confirmation Awaiter |
| `lic.q.user-confirmed-type` | `contractpro.commands` | `orch.commands.user-confirmed-type` | LIC Pending Type Confirmation Manager |

### 6.2 Queue policies

Все LIC-queues:
- `durable: true`
- `auto-delete: false`
- `x-message-ttl: 86400000` (24h) — для входящих
- `x-dead-letter-exchange: contractpro.dlx`
- `x-dead-letter-routing-key: lic.dlq.<reason>` (выставляется publisher'ом DLQ)
- `x-max-length: 100000` (anti-runaway protection; alert при превышении)

### 6.3 Consumer parameters

```
prefetch: LIC_CONSUMER_PREFETCH (default 10)
auto_ack: false (manual ack only)
exclusive: false
no_local: false
```

Concurrency limit на уровне LIC-приложения — `LIC_PIPELINE_CONCURRENCY=5` (см. high-architecture §6.14).

### 6.4 DLX retry topology (exponential backoff)

При transient ошибках обработки сообщения (например, Redis hiccup, DM timeout, network glitch) **простой NACK с requeue приводит к hot-loop**: сообщение немедленно возвращается в очередь, моментально консумится тем же или другим инстансом, снова падает → CPU/network burn. RabbitMQ natively не поддерживает delayed redelivery — реализация через **DLX-loop с per-attempt TTL**.

**Топология (per consumed topic, например `dm.events.version-artifacts-ready`):**

```
                        ┌────────────────────────────────────────┐
                        │ Main queue: lic.q.version-artifacts-ready│
                        │ binding: dm.events.version-artifacts-ready│
                        │ x-dead-letter-exchange: contractpro.dlx │
                        │ x-dead-letter-routing-key: ...retry.1   │ ← NACK сюда
                        └─────────────────────┬──────────────────┘
                                              │ NACK (transient err)
                                              ▼
                  ┌────────────────────────────────────────┐
                  │ Retry queue 1: lic.q.<topic>.retry.1    │
                  │ x-message-ttl: 2000 (2 секунды)         │
                  │ x-dead-letter-exchange: contractpro.dlx │
                  │ x-dead-letter-routing-key: <main topic> │ ← возврат в main после TTL
                  └─────────────────────┬──────────────────┘
                                        │ TTL expired → back to main
                                        ▼
                              [повтор обработки в main]
                                        │ NACK (всё ещё err)
                                        ▼
                  ┌────────────────────────────────────────┐
                  │ Retry queue 2: lic.q.<topic>.retry.2    │
                  │ x-message-ttl: 10000 (10 секунд)        │
                  └─────────────────────┬──────────────────┘
                                        │ TTL → main → если err →
                                        ▼
                  ┌────────────────────────────────────────┐
                  │ Retry queue 3: lic.q.<topic>.retry.3    │
                  │ x-message-ttl: 60000 (60 секунд)        │
                  └─────────────────────┬──────────────────┘
                                        │ TTL → main → если err →
                                        ▼
                              [DLQ: lic.dlq.consumer-failed]
```

**Логика на consumer-стороне:**

- LIC consumer читает `x-death` header сообщения — он содержит число попыток и через какие retry-queues прошло (RabbitMQ автоматически заполняет при DLX-routing).
- При NACK consumer проверяет `x-death[0].count`:
  - 0 (первый NACK) → routing-key `lic.q.<topic>.retry.1` (TTL 2s).
  - 1 → `retry.2` (TTL 10s).
  - 2 → `retry.3` (TTL 60s).
  - ≥ 3 → `lic.dlq.consumer-failed` (исчерпан retry budget, см. §10).

Total max delay перед DLQ: 2s + 10s + 60s = 72s — приемлемо для transient outages.

**Конфигурация per-topic** (env):

```env
LIC_CONSUMER_MAX_REDELIVERIES=3       # количество retry-уровней
LIC_CONSUMER_RETRY_TTL_1=2s           # первый retry
LIC_CONSUMER_RETRY_TTL_2=10s          # второй retry
LIC_CONSUMER_RETRY_TTL_3=60s          # третий retry
```

**Где НЕ применяется retry-DLX:**

- `INVALID_MESSAGE_SCHEMA` / `INVALID_ORG_ID_MISMATCH` / `MALFORMED_REQUEST` — данные сообщения некорректны, retry не поможет → прямо в `lic.dlq.invalid-message`.
- `is_retryable=false` ошибки бизнес-логики (например, `INVALID_CONTRACT_TYPE`) — публикуется FAILED + ACK, без retry.

> Закрывает F-7.4: hot-loop при transient ошибках устранён через DLX-loop с per-attempt TTL. Альтернатива (RabbitMQ delayed-message-exchange plugin) — не требуется в v1 (DLX-loop работает на чистом core RabbitMQ).

---

## 7. Correlation и трассировка

| Поле | Назначение | Где используется |
|------|------------|-------------------|
| `correlation_id` | Сквозной ID бизнес-операции (uploaded contract → final report) | Все события, логи, audit, OTel tracing |
| `job_id` | ID конкретной задачи DP/LIC/RE | Idempotency, метрики per job |
| `document_id` | Документ в DM | Все операции |
| `version_id` | Версия документа в DM | Привязка артефактов |
| `organization_id` | Tenant | Все операции; audit, OTel attribute, обязательная фильтрация |
| `created_by_user_id` | Audit trail (кто создал версию в DM) | Логи, OTel attribute |

OpenTelemetry trace создаётся при получении `version-artifacts-ready`; родительский span — span DP processing pipeline (через `traceparent` в headers). Дочерние spans — стадии пайплайна и LLM-вызовы.

---

## 8. Versioning контрактов

### 8.1 Backward compatibility

- Новые поля LIC-собственных событий — добавляются как optional с `omitempty`.
- При breaking change в LIC-собственных событиях (тех, на которые подписан Orchestrator) — координируется с командой Orchestrator.
- Изменение FROZEN-контрактов DM или Orchestrator со стороны LIC **запрещено**.

### 8.2 Schema version в `LegalAnalysisArtifactsReady`

Расширение схемы для `risk_delta` — это minor version (v1.0 → v1.1). LIC отправляет всегда v1.1; DM v1.0 игнорирует unknown поле, DM v1.1+ персистирует. Координация — в ADR-LIC-05.

### 8.3 Топики стабильны

Имена топиков — стабильный контракт. При необходимости major version — новый топик с суффиксом (`.v2`); transition period с поддержкой обоих.

В v1 — никаких suffix'ов. Все топики без version-suffix.

---

## 9. Sync взаимодействия — отсутствуют

**Жёсткое ограничение архитектуры.** LIC не имеет sync REST зависимостей:
- DM API (sync) — **не используется**.
- OPM, LKB — **не существуют в v1**, не упоминаются.
- UOM (auth) — **не используется** (LIC получает org_id из envelope события, который уже валидирован Orchestrator при триггере DM-pipeline'а).
- Orchestrator REST — **не используется** (Orchestrator → LIC только через async команду `user-confirmed-type`).

LLM HTTP API — это **внешняя зависимость**, а не внутренняя интеграция; описана в `llm-provider-abstraction.md`.

---

## 10. DLQ-стратегия

| Топик DLQ | Триггер | PII в `original_message`? |
|-----------|---------|:-------------------------:|
| `lic.dlq.invalid-message` | Невалидная схема входящего сообщения (deserialize / required fields missing) | Нет (только IDs) |
| `lic.dlq.consumer-failed` | Исчерпан retry-budget на consumer-handler уровне | Нет (только IDs) |
| `lic.dlq.publish-failed` | Не удалось опубликовать исходящее (DM/Orch) после retry | **Да** (`LegalAnalysisArtifactsReady` содержит ПДн — стороны, цены, тексты рисков) |
| `lic.dlq.agent-output-invalid` | LLM-агент вернул невалидный JSON после repair × 1 | Частично (raw LLM response) |

### 10.1 DLQ envelope (PII-safe baseline)

Стандартный envelope для DLQ-сообщений **БЕЗ полного payload** (закрывает F-8.4):

```json
{
  "original_topic": "string",
  "original_message_hash": "string (HMAC-SHA-256 от полного payload через LIC_DLQ_HASH_KEY; first 64 chars hex)",
  "original_message_size_bytes": "int",
  "error_code": "string",
  "error_message": "string",
  "retry_count": "int",
  "correlation_id": "uuid",
  "job_id": "uuid",
  "document_id": "uuid",
  "version_id": "uuid",
  "organization_id": "uuid",
  "agent_id": "string (optional, для agent-output-invalid)",
  "raw_llm_response_hash": "string (optional; HMAC-SHA-256 от full LLM response; first 64 chars hex)",
  "failed_at": "ISO 8601"
}
```

**Изменения относительно прежней версии (B13):**
- `original_message: object (raw JSON)` **удалено** из envelope.
- Добавлены `original_message_hash` (HMAC, не sha256-plain — защита от rainbow-table) и `original_message_size_bytes`.
- `raw_llm_response_hash` — HMAC вместо sha256-plain.
- Все hash'и используют один secret `LIC_DLQ_HASH_KEY` (отдельный от `LIC_PROMPT_INJECTION_HASH_KEY`); ротация ключа допустима, но invalidate'ит forensics-history.

### 10.2 Full payload retention для `lic.dlq.publish-failed`

Для `lic.dlq.publish-failed` (где `original_message` = `LegalAnalysisArtifactsReady` с PII) нужен полный payload для post-mortem (что именно DM отверг). Решение:

- Полный payload **НЕ** в RabbitMQ DLQ envelope.
- Сохраняется в **отдельное Object Storage хранилище** с TTL 24h: bucket `lic-dlq-payloads-{env}`, key `{failed_at_date}/{correlation_id}.json.gz` (gzip).
- Access control: только security team + on-call engineers (через Yandex IAM роли `lic-dlq-reader`); standard developers НЕ имеют доступа.
- Audit-log на каждый read access.
- DLQ envelope содержит ссылку: `payload_storage_key: "lic-dlq-payloads-prod/2026-05-12/abc-uuid.json.gz"` для restricted retrieval.

Для остальных DLQ-топиков (invalid-message / consumer-failed) — full payload не нужен (envelope содержит достаточно метаданных).

### 10.3 Конфигурация

```env
LIC_DLQ_HASH_KEY=...                                # HMAC secret для original_message_hash (32 bytes)
LIC_DLQ_PUBLISH_FAILED_STORAGE_BUCKET=lic-dlq-payloads-prod
LIC_DLQ_PUBLISH_FAILED_STORAGE_TTL=24h
```

Подробнее — `error-handling.md` §7, `security.md` §6.

---

## 11. Матрица топиков

### Подписки

| Топик | Источник | Когда | Idempotency Key |
|-------|----------|-------|-----------------|
| `dm.events.version-artifacts-ready` | DM | DP завершил обработку версии, артефакты сохранены в DM | `lic-trigger:{version_id}` |
| `dm.events.version-created` | DM | Любая новая версия создана (для кэширования `parent_version_id` + `origin_type`) | `lic-version-created:{version_id}` |
| `dm.responses.artifacts-provided` | DM | Ответ на наш `lic.requests.artifacts` | `lic-artifacts-resp:{correlation_id}` |
| `dm.responses.lic-artifacts-persisted` | DM | Ответ на наш `lic.artifacts.analysis-ready` (success) | `lic-persist-resp:{job_id}` |
| `dm.responses.lic-artifacts-persist-failed` | DM | Ответ на наш `lic.artifacts.analysis-ready` (failed) | `lic-persist-fail:{job_id}` |
| `orch.commands.user-confirmed-type` | Orchestrator | Пользователь подтвердил тип после ClassificationUncertain | `lic-user-confirmed:{version_id}` |

### Публикации

| Топик | Потребитель | Когда |
|-------|-------------|-------|
| `lic.requests.artifacts` | DM | После получения `version-artifacts-ready`; и для родительского `RISK_ANALYSIS` при `parent_version_id != null` |
| `lic.artifacts.analysis-ready` | DM | После завершения пайплайна (success) |
| `lic.events.classification-uncertain` | Orchestrator | Confidence классификации < threshold |
| `lic.events.status-changed` | Orchestrator | На каждой границе внешнего статуса (IN_PROGRESS / COMPLETED / FAILED) |

### DLQ

| Топик | Триггер |
|-------|---------|
| `lic.dlq.invalid-message` | Десериализация / контракт |
| `lic.dlq.consumer-failed` | Retry exhausted |
| `lic.dlq.publish-failed` | Publish failed |
| `lic.dlq.agent-output-invalid` | LLM output не прошёл schema + repair |

---

## 12. Self-check

- [x] Все подписки и публикации — async через RabbitMQ.
- [x] Sync REST к DM, OPM, LKB, UOM — отсутствует (YAGNI: OPM/LKB не существуют).
- [x] FROZEN-контракты DM и Orchestrator переиспользованы один-в-один со ссылками.
- [x] LIC-собственные события — описаны в `event-catalog.md`.
- [x] Idempotency keys определены для каждой подписки.
- [x] DLQ-стратегия описана.
- [x] Envelope единый и совместимый с DP/DM/Orchestrator.
- [x] Расширение схемы `LegalAnalysisArtifactsReady` для `RISK_DELTA` оформлено через ADR-LIC-05 (backward-compatible).
