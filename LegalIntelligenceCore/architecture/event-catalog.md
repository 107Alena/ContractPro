# Event Catalog — Legal Intelligence Core

Каталог **только LIC-собственных** событий: что LIC публикует, и какие у него собственные DLQ-топики. Чужие контракты переиспользуются со ссылками — без дублирования.

| Тип | Где определён |
|-----|---------------|
| Что LIC получает (FROZEN, чужие контракты) | См. `integration-contracts.md` §1 — все ссылки на DM event-catalog и Orchestrator event-catalog. |
| Что LIC публикует в DM (FROZEN, чужие контракты — `GetArtifactsRequest`, `LegalAnalysisArtifactsReady`) | DM event-catalog §1.4, §1.5. |
| Что LIC публикует в Orchestrator | **Этот документ** — §1, §2 ниже. |
| LIC DLQ-события | **Этот документ** — §3. |

---

## 1. LIC → Orchestrator: статусные события

### 1.1 LICStatusChangedEvent

**Топик:** `lic.events.status-changed`
**Потребитель:** Orchestrator
**Контракт:** FROZEN — определён в `ApiBackendOrchestrator/architecture/event-catalog.md` §2.2.1.

Здесь приводится для удобства разработки **полная JSON-схема LIC-публикации** (LIC должен формировать сообщение строго по этому контракту):

```json
{
  "$schema": "http://json-schema.org/draft-07/schema#",
  "title": "LICStatusChangedEvent",
  "type": "object",
  "additionalProperties": false,
  "required": ["correlation_id","timestamp","job_id","document_id","version_id","organization_id","status"],
  "properties": {
    "correlation_id": {"type":"string","format":"uuid"},
    "timestamp": {"type":"string","format":"date-time"},
    "job_id": {"type":"string","format":"uuid"},
    "document_id": {"type":"string","format":"uuid"},
    "version_id": {"type":"string","format":"uuid"},
    "organization_id": {"type":"string","format":"uuid"},
    "status": {"type":"string","enum":["IN_PROGRESS","COMPLETED","FAILED"]},
    "stage": {"type":"string"},
    "error_code": {"type":"string"},
    "error_message": {"type":"string"},
    "is_retryable": {"type":"boolean"}
  }
}
```

**Когда публикуется:**

| Внешний статус | `stage` (optional) | Триггер в LIC |
|----------------|---------------------|---------------|
| `IN_PROGRESS` | `STAGE_REQUESTING_ARTIFACTS` | LIC начал обработку — отправил `GetArtifactsRequest` |
| `IN_PROGRESS` | `STAGE_AWAITING_USER_CONFIRMATION` | Confidence < threshold; одновременно с `classification-uncertain` |
| `COMPLETED` | (omit) | Получен `lic-artifacts-persisted` от DM |
| `FAILED` | `<последняя стадия>` | Любая фатальная ошибка пайплайна |

**Поля `error_code` / `error_message` / `is_retryable`** — заполняются **только** при `status=FAILED`. См. `error-handling.md` §3 для каталога error_code.

**Idempotency:** Orchestrator-side через `lic-status:{job_id}:{status}` (см. Orchestrator event-catalog).

**Пример (IN_PROGRESS):**
```json
{
  "correlation_id":"5b8e3a7c-91e2-4d1a-9b8f-2e6a3f1c4d8a",
  "timestamp":"2026-05-06T12:00:00Z",
  "job_id":"a1b2c3d4-e5f6-7890-abcd-ef1234567890",
  "document_id":"d0c0d0c0-1111-2222-3333-444444444444",
  "version_id":"v1v1v1v1-1111-2222-3333-555555555555",
  "organization_id":"00000000-1111-2222-3333-444444444444",
  "status":"IN_PROGRESS",
  "stage":"STAGE_REQUESTING_ARTIFACTS"
}
```

**Пример (FAILED):**
```json
{
  "correlation_id":"5b8e3a7c-91e2-4d1a-9b8f-2e6a3f1c4d8a",
  "timestamp":"2026-05-06T12:00:35Z",
  "job_id":"a1b2c3d4-e5f6-7890-abcd-ef1234567890",
  "document_id":"d0c0d0c0-1111-2222-3333-444444444444",
  "version_id":"v1v1v1v1-1111-2222-3333-555555555555",
  "organization_id":"00000000-1111-2222-3333-444444444444",
  "status":"FAILED",
  "stage":"STAGE_AGENT_RISK_DETECTION",
  "error_code":"AGENT_OUTPUT_INVALID",
  "error_message":"Не удалось получить корректный анализ рисков. Запустите повторную проверку.",
  "is_retryable":true
}
```

> Сообщения `error_message` — на русском (NFR-5.2). Маппинг `error_code` → русский текст определён в `error-handling.md` §3.

---

### 1.2 ClassificationUncertain

**Топик:** `lic.events.classification-uncertain`
**Потребитель:** Orchestrator
**Контракт:** FROZEN — определён в `ApiBackendOrchestrator/architecture/event-catalog.md` §2.2.2.

Полная JSON-схема LIC-публикации:

```json
{
  "$schema": "http://json-schema.org/draft-07/schema#",
  "title": "ClassificationUncertain",
  "type": "object",
  "additionalProperties": false,
  "required": ["correlation_id","timestamp","job_id","document_id","version_id","organization_id","suggested_type","confidence","threshold"],
  "properties": {
    "correlation_id": {"type":"string","format":"uuid"},
    "timestamp": {"type":"string","format":"date-time"},
    "job_id": {"type":"string","format":"uuid"},
    "document_id": {"type":"string","format":"uuid"},
    "version_id": {"type":"string","format":"uuid"},
    "organization_id": {"type":"string","format":"uuid"},
    "suggested_type": {"type":"string","enum":[
      "SERVICES","SUPPLY","WORK_CONTRACT","LEASE","NDA","SALE",
      "LICENSE","AGENCY","LOAN","INSURANCE","EMPLOYMENT_CIVIL","OTHER"]},
    "confidence": {"type":"number","minimum":0.0,"maximum":1.0},
    "threshold": {"type":"number","minimum":0.0,"maximum":1.0},
    "alternatives": {
      "type":"array",
      "items": {
        "type":"object",
        "required":["contract_type","confidence"],
        "properties": {
          "contract_type": {"type":"string","enum":[
            "SERVICES","SUPPLY","WORK_CONTRACT","LEASE","NDA","SALE",
            "LICENSE","AGENCY","LOAN","INSURANCE","EMPLOYMENT_CIVIL","OTHER"]},
          "confidence": {"type":"number","minimum":0.0,"maximum":1.0}
        }
      }
    }
  }
}
```

**Когда публикуется:** один раз на версию, при условии `ClassificationResult.confidence < LIC_CONFIDENCE_THRESHOLD`. Pipeline LIC переходит в pause до `UserConfirmedType`.

**Idempotency:** Orchestrator-side через `lic-uncertain:{version_id}`.

**Пример:**
```json
{
  "correlation_id":"5b8e3a7c-91e2-4d1a-9b8f-2e6a3f1c4d8a",
  "timestamp":"2026-05-06T12:00:08Z",
  "job_id":"a1b2c3d4-e5f6-7890-abcd-ef1234567890",
  "document_id":"d0c0d0c0-1111-2222-3333-444444444444",
  "version_id":"v1v1v1v1-1111-2222-3333-555555555555",
  "organization_id":"00000000-1111-2222-3333-444444444444",
  "suggested_type":"OTHER",
  "confidence":0.62,
  "threshold":0.75,
  "alternatives":[
    {"contract_type":"SUPPLY","confidence":0.55},
    {"contract_type":"WORK_CONTRACT","confidence":0.50}
  ]
}
```

---

## 2. LIC → DM: переиспользуемые FROZEN-контракты

LIC публикует:
- `GetArtifactsRequest` в `lic.requests.artifacts` — полная схема в `DocumentManagement/architecture/event-catalog.md` §1.4.
- `LegalAnalysisArtifactsReady` в `lic.artifacts.analysis-ready` — полная схема в `DocumentManagement/architecture/event-catalog.md` §1.5 + расширение `risk_delta` (см. ADR-LIC-05).

**Расширение схемы `LegalAnalysisArtifactsReady` для `risk_delta` (v1.1):**

LIC отправляет дополнительное optional-поле `risk_delta` на верхнем уровне payload. Полная JSON-схема расширения — см. `ai-agents-pipeline.md` (агент 9, RiskDelta).

```jsonc
{
  // обязательные поля v1.0 — переиспользуются один-в-один из DM event-catalog §1.5:
  "correlation_id":"...",
  "timestamp":"...",
  "job_id":"...",
  "document_id":"...",
  "version_id":"...",
  "organization_id":"...",
  "classification_result":{...},
  "key_parameters":{...},
  "risk_analysis":{...},
  "risk_profile":{...},
  "recommendations":[...],
  "summary":{...},
  "detailed_report":{...},
  "aggregate_score":{...},

  // optional — только при RE_CHECK + наличии родительского RISK_ANALYSIS (v1.1):
  "risk_delta": {
    "base_version_id":"uuid",
    "target_version_id":"uuid",
    "added":[...],
    "removed":[...],
    "changed":[...],
    "profile_change":{...},
    "summary":"string"
  }
}
```

**Idempotency:** DM-side через `lic-artifacts:{job_id}` (см. DM event-catalog §1.5).

> Координация для extension: при выкатке новой версии DM (v1.1+) DM добавит `RISK_DELTA` в enum `artifact_type` и сохранит как отдельный `ArtifactDescriptor`. До этого — graceful: warning в LIC-логах + warning в `DETAILED_REPORT.warnings`.

---

## 3. DLQ-события LIC

### 3.1 Общий envelope

```json
{
  "$schema":"http://json-schema.org/draft-07/schema#",
  "title":"LICDLQEnvelope",
  "type":"object",
  "additionalProperties": false,
  "required":["original_topic","original_message","error_code","error_message","retry_count","failed_at"],
  "properties": {
    "original_topic": {"type":"string"},
    "original_message": {"type":"object"},
    "error_code": {"type":"string"},
    "error_message": {"type":"string"},
    "retry_count": {"type":"integer","minimum":0},
    "correlation_id": {"type":"string","format":"uuid"},
    "job_id": {"type":"string","format":"uuid"},
    "document_id": {"type":"string","format":"uuid"},
    "version_id": {"type":"string","format":"uuid"},
    "organization_id": {"type":"string","format":"uuid"},
    "agent_id": {"type":"string"},
    "stage": {"type":"string"},
    "raw_llm_response_hash": {"type":"string"},
    "failed_at": {"type":"string","format":"date-time"}
  }
}
```

`agent_id`, `stage`, `raw_llm_response_hash` — заполняются для DLQ типа `agent-output-invalid`.

`raw_llm_response_hash` — sha256 от первых 1024 символов raw response (для дедупликации повторяющихся проблем без хранения PII).

### 3.2 Топики DLQ

| Топик | Триггер | Заполняемые поля envelope |
|-------|---------|--------------------------|
| `lic.dlq.invalid-message` | Невалидная схема входящего сообщения | base + correlation_id (если удалось извлечь) |
| `lic.dlq.consumer-failed` | Исчерпан retry на consumer-handler уровне | base + все corr fields + stage |
| `lic.dlq.publish-failed` | Не удалось опубликовать исходящее в DM/Orch | base + корр. fields + topic, что не удалось |
| `lic.dlq.agent-output-invalid` | LLM-агент вернул невалидный JSON после repair × 1 | base + corr fields + agent_id + stage + raw_llm_response_hash |

---

## 4. Сводная матрица событий LIC

### 4.1 Что публикует LIC

| Топик | Событие | Контракт | Потребитель |
|-------|---------|----------|-------------|
| `lic.requests.artifacts` | `GetArtifactsRequest` | FROZEN (DM event-catalog §1.4) | DM |
| `lic.artifacts.analysis-ready` | `LegalAnalysisArtifactsReady` (v1.1, extension `risk_delta`) | FROZEN + extension (ADR-LIC-05) | DM |
| `lic.events.classification-uncertain` | `ClassificationUncertain` | §1.2 (FROZEN от Orchestrator) | Orchestrator |
| `lic.events.status-changed` | `LICStatusChangedEvent` | §1.1 (FROZEN от Orchestrator) | Orchestrator |
| `lic.dlq.invalid-message` | DLQ envelope | §3.1 | пост-мортем |
| `lic.dlq.consumer-failed` | DLQ envelope | §3.1 | пост-мортем |
| `lic.dlq.publish-failed` | DLQ envelope | §3.1 | пост-мортем |
| `lic.dlq.agent-output-invalid` | DLQ envelope | §3.1 | пост-мортем + ML quality dashboard |

### 4.2 На что подписан LIC

| Топик | Событие | Контракт |
|-------|---------|----------|
| `dm.events.version-artifacts-ready` | `VersionProcessingArtifactsReady` | FROZEN (DM event-catalog §2.2) |
| `dm.events.version-created` | `VersionCreated` | FROZEN (DM event-catalog §2.2) |
| `dm.responses.artifacts-provided` | `ArtifactsProvided` | FROZEN (DM event-catalog §2.1) |
| `dm.responses.lic-artifacts-persisted` | `LegalAnalysisArtifactsPersisted` | FROZEN (DM event-catalog §2.1) |
| `dm.responses.lic-artifacts-persist-failed` | `LegalAnalysisArtifactsPersistFailed` | FROZEN (DM event-catalog §2.1) |
| `orch.commands.user-confirmed-type` | `UserConfirmedType` | FROZEN (Orchestrator event-catalog §1.3) |

---

## 5. Общие правила

1. **Envelope:** все исходящие события содержат `correlation_id` (UUID) и `timestamp` (ISO 8601). `correlation_id` пробрасывается из триггерного входящего события.
2. **Backward compatibility:** новые поля добавляются как optional с `omitempty`. LIC игнорирует unknown поля во входящих.
3. **Schema versioning:** при breaking change в `LICStatusChangedEvent` или `ClassificationUncertain` — координируется с Orchestrator (это его FROZEN-контракты, LIC не вправе их менять без согласования).
4. **Serialization:** JSON, UTF-8.
5. **Размер сообщения:** `LegalAnalysisArtifactsReady` обычно ≤ 1 МБ (включая `detailed_report`). Лимит RabbitMQ-frame — конфигурируется на стороне брокера (стандарт ≥ 16 МБ). Claim-check pattern в LIC v1 не используется.
6. **Tenant isolation:** `organization_id` обязателен во всех исходящих и проверяется LIC при получении входящих (mismatch со state pipeline → DLQ).
