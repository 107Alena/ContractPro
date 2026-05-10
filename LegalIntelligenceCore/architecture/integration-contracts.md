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
- **`version-created`** — кэш `origin_type` и `parent_version_id` для определения режима `INITIAL` vs `RE_CHECK` (см. ASSUMPTION-LIC-02 в `high-architecture.md`).
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

- **`lic.requests.artifacts`** — async request artifact'ов у DM. Содержит `artifact_types` — список нужных типов: для INITIAL-анализа `[SEMANTIC_TREE, EXTRACTED_TEXT, DOCUMENT_STRUCTURE, PROCESSING_WARNINGS]`; для RE_CHECK дополнительно отдельным запросом — `[RISK_ANALYSIS]` для родительской версии.
- **`lic.artifacts.analysis-ready`** — публикация результатов анализа в DM. Соответствует FROZEN-контракту `LegalAnalysisArtifactsReady` плюс optional поле `risk_delta` (v1.1, ADR-LIC-05).
- **`lic.events.classification-uncertain`** — сигнал Orchestrator о низкой уверенности классификации; запускает сценарий `AWAITING_USER_INPUT`.
- **`lic.events.status-changed`** — пять статусных событий пайплайна: `IN_PROGRESS` (старт), `IN_PROGRESS` (с stage=`STAGE_AWAITING_USER_CONFIRMATION`), `COMPLETED`, `FAILED`.

### 2.2 Расширение `LegalAnalysisArtifactsReady` для `RISK_DELTA`

См. ADR-LIC-05 в `high-architecture.md`. Дополнение к схеме (v1.1, backward-compatible):

```jsonc
{
  // ... все обязательные поля из v1.0 (см. DM event-catalog §1.5) ...
  "risk_delta": {
    // optional, заполняется только при RE_CHECK
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

Поле появляется **только** при `origin_type=RE_CHECK` и наличии `RISK_ANALYSIS` родительской версии. В v1.0 DM эту схему не знает — поле отбрасывается при сохранении (DM игнорирует unknown fields) с warning в логах. Когда DM добавит поддержку (отдельный TASK), `RISK_DELTA` будет сохраняться как новый `ArtifactDescriptor.artifact_type=RISK_DELTA`. До этого момента — graceful degradation: warning в `DETAILED_REPORT.warnings.RISK_DELTA_PERSIST_PENDING` и метрика `lic_risk_delta_emitted_total` для отслеживания.

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
  "requested_by_user_id": "uuid-v4 (если применимо)"
  // ... event-specific fields
}
```

| Поле | Источник в LIC | Поведение |
|------|-----------------|-----------|
| `correlation_id` | Из входящего `version-artifacts-ready` или `user-confirmed-type` | Пробрасывается во все исходящие |
| `job_id` | Из входящего | Пробрасывается |
| `document_id`, `version_id` | Из входящего | Пробрасывается |
| `organization_id` | Из входящего | Tenant isolation enforcement |
| `requested_by_user_id` | Из входящего (если есть) | Audit trail; для `version-artifacts-ready` отсутствует — кэшируется из `version-created` |
| `timestamp` | Server time публикации | Genередируется при публикации |

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

---

## 7. Correlation и трассировка

| Поле | Назначение | Где используется |
|------|------------|-------------------|
| `correlation_id` | Сквозной ID бизнес-операции (uploaded contract → final report) | Все события, логи, audit, OTel tracing |
| `job_id` | ID конкретной задачи DP/LIC/RE | Idempotency, метрики per job |
| `document_id` | Документ в DM | Все операции |
| `version_id` | Версия документа в DM | Привязка артефактов |
| `organization_id` | Tenant | Все операции; audit, OTel attribute, обязательная фильтрация |
| `requested_by_user_id` | Audit trail | Логи, OTel attribute |

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

| Топик DLQ | Триггер |
|-----------|---------|
| `lic.dlq.invalid-message` | Невалидная схема входящего сообщения (deserialize / required fields missing) |
| `lic.dlq.consumer-failed` | Исчерпан retry-budget на consumer-handler уровне |
| `lic.dlq.publish-failed` | Не удалось опубликовать исходящее (DM/Orch) после retry |
| `lic.dlq.agent-output-invalid` | LLM-агент вернул невалидный JSON после repair × 1 |

Envelope DLQ-сообщения:

```json
{
  "original_topic": "string",
  "original_message": "object (raw JSON)",
  "error_code": "string",
  "error_message": "string",
  "retry_count": "int",
  "correlation_id": "uuid",
  "job_id": "uuid",
  "document_id": "uuid",
  "version_id": "uuid",
  "organization_id": "uuid",
  "agent_id": "string (optional, для agent-output-invalid)",
  "raw_llm_response_hash": "string (optional, для agent-output-invalid; sha256 first 256 chars)",
  "failed_at": "ISO 8601"
}
```

Подробнее — `error-handling.md` §7.

---

## 11. Матрица топиков

### Подписки

| Топик | Источник | Когда | Idempotency Key |
|-------|----------|-------|-----------------|
| `dm.events.version-artifacts-ready` | DM | DP завершил обработку версии, артефакты сохранены в DM | `lic-trigger:{version_id}` |
| `dm.events.version-created` | DM | Любая новая версия создана (для кэширования origin_type) | `lic-version-created:{version_id}` |
| `dm.responses.artifacts-provided` | DM | Ответ на наш `lic.requests.artifacts` | `lic-artifacts-resp:{correlation_id}` |
| `dm.responses.lic-artifacts-persisted` | DM | Ответ на наш `lic.artifacts.analysis-ready` (success) | `lic-persist-resp:{job_id}` |
| `dm.responses.lic-artifacts-persist-failed` | DM | Ответ на наш `lic.artifacts.analysis-ready` (failed) | `lic-persist-fail:{job_id}` |
| `orch.commands.user-confirmed-type` | Orchestrator | Пользователь подтвердил тип после ClassificationUncertain | `lic-user-confirmed:{version_id}` |

### Публикации

| Топик | Потребитель | Когда |
|-------|-------------|-------|
| `lic.requests.artifacts` | DM | После получения `version-artifacts-ready`; и для родительского `RISK_ANALYSIS` при RE_CHECK |
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
