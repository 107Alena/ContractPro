# Event Catalog — API/Backend Orchestrator

Полный каталог всех событий API/Backend Orchestrator: исходящие команды, входящие события, DLQ. Для каждого события — JSON schema, направление, топик, потребитель.

**Роль оркестратора в событийной архитектуре:** оркестратор является координирующим слоем между пользователем и доменными сервисами. Он публикует команды в DP (Document Processing) и подписывается на события из DP и DM (Document Management) для отслеживания хода обработки и доставки статусов пользователю через SSE.

---

## 1. Исходящие события (Orchestrator публикует)

### 1.1 ProcessDocumentRequested

**Направление:** Orchestrator --> DP
**Топик:** `dp.commands.process-document`
**Потребитель:** DP (Document Processing)
**Trigger:** Пользователь загрузил файл через REST API, файл сохранён в Object Storage, документ/версия созданы в DM.
**Idempotency key:** `orch-process:{job_id}`

```json
{
  "correlation_id": "string (UUID)",
  "timestamp": "string (ISO 8601)",
  "job_id": "string (UUID)",
  "document_id": "string (UUID)",
  "version_id": "string (UUID)",
  "organization_id": "string (UUID)",
  "requested_by_user_id": "string (UUID)",
  "source_file_key": "string (S3 object key, e.g. uploads/{org_id}/{uuid}/contract.pdf)",
  "source_file_name": "string (original filename)",
  "source_file_size": "int (bytes)",
  "source_file_checksum": "string (SHA-256)",
  "source_file_mime_type": "string (application/pdf)"
}
```

**Обязательные поля:** все поля обязательны.

**Описание полей:**

| Поле | Тип | Описание |
|------|-----|----------|
| `correlation_id` | UUID | Уникальный идентификатор корреляции, сквозной для всей цепочки обработки. Генерируется оркестратором при получении HTTP-запроса. |
| `timestamp` | ISO 8601 | Время отправки команды. |
| `job_id` | UUID | Уникальный идентификатор задания обработки. Генерируется оркестратором. Используется для отслеживания статуса и идемпотентности. |
| `document_id` | UUID | Идентификатор документа в DM. |
| `version_id` | UUID | Идентификатор версии документа в DM. |
| `organization_id` | UUID | Идентификатор организации (tenant). Извлекается из JWT. |
| `requested_by_user_id` | UUID | Идентификатор пользователя, инициировавшего загрузку. Извлекается из JWT. |
| `source_file_key` | string | Ключ объекта в Object Storage (S3). Файл загружается оркестратором ДО отправки команды. |
| `source_file_name` | string | Оригинальное имя файла, предоставленное пользователем. |
| `source_file_size` | int | Размер файла в байтах. Оркестратор валидирует лимит (20 МБ по умолчанию) до отправки. |
| `source_file_checksum` | string | SHA-256 хэш содержимого файла. Вычисляется оркестратором после загрузки в S3. |
| `source_file_mime_type` | string | MIME-тип файла. В v1 — только `application/pdf`. |

> **Порядок действий оркестратора:** (1) валидация файла --> (2) загрузка в Object Storage --> (3) создание документа/версии в DM (sync REST) --> (4) публикация `ProcessDocumentRequested` в DP --> (5) возврат `202 Accepted` клиенту.

---

### 1.2 CompareDocumentVersionsRequested

**Направление:** Orchestrator --> DP
**Топик:** `dp.commands.compare-versions`
**Потребитель:** DP (Document Processing)
**Trigger:** Пользователь запросил сравнение двух версий документа через REST API.
**Idempotency key:** `orch-compare:{job_id}`

```json
{
  "correlation_id": "string (UUID)",
  "timestamp": "string (ISO 8601)",
  "job_id": "string (UUID)",
  "document_id": "string (UUID)",
  "organization_id": "string (UUID)",
  "requested_by_user_id": "string (UUID)",
  "base_version_id": "string (UUID)",
  "target_version_id": "string (UUID)"
}
```

**Обязательные поля:** все поля обязательны.

**Описание полей:**

| Поле | Тип | Описание |
|------|-----|----------|
| `correlation_id` | UUID | Уникальный идентификатор корреляции. |
| `timestamp` | ISO 8601 | Время отправки команды. |
| `job_id` | UUID | Уникальный идентификатор задания сравнения. |
| `document_id` | UUID | Идентификатор документа, которому принадлежат обе версии. |
| `organization_id` | UUID | Идентификатор организации (tenant). |
| `requested_by_user_id` | UUID | Идентификатор пользователя, инициировавшего сравнение. |
| `base_version_id` | UUID | Идентификатор базовой (более ранней) версии документа. |
| `target_version_id` | UUID | Идентификатор целевой (более поздней) версии документа. |

> **Предусловия:** обе версии должны иметь статус обработки `COMPLETED` или `COMPLETED_WITH_WARNINGS`. Оркестратор проверяет это через DM sync API до публикации команды. Если хотя бы одна версия не обработана — возврат `409 Conflict` клиенту.

---

## 2. Входящие события (Orchestrator принимает)

### 2.1 События от DP (Document Processing)

#### 2.1.1 StatusChangedEvent

**Направление:** DP --> Orchestrator
**Топик:** `dp.events.status-changed`
**Обработчик:** Event Router --> SSE Publisher
**Idempotency key:** `dp-status:{job_id}:{status}`

```json
{
  "correlation_id": "string (UUID)",
  "timestamp": "string (ISO 8601)",
  "job_id": "string (UUID)",
  "document_id": "string (UUID)",
  "version_id": "string (UUID)",
  "organization_id": "string (UUID, optional)",
  "status": "string (QUEUED | IN_PROGRESS | COMPLETED | COMPLETED_WITH_WARNINGS | FAILED | TIMED_OUT | REJECTED)",
  "stage": "string (optional, internal stage for observability)",
  "message": "string (optional)"
}
```

**Обязательные поля:** `correlation_id`, `timestamp`, `job_id`, `document_id`, `version_id`, `status`.
**Optional:** `organization_id`, `stage`, `message` (omitempty).

**Описание полей:**

| Поле | Тип | Описание |
|------|-----|----------|
| `correlation_id` | UUID | Корреляция — совпадает с `correlation_id` исходной команды. |
| `timestamp` | ISO 8601 | Время смены статуса в DP. |
| `job_id` | UUID | Идентификатор задания. |
| `document_id` | UUID | Идентификатор документа. |
| `version_id` | UUID | Идентификатор версии документа. |
| `organization_id` | UUID | Идентификатор организации (optional, т.к. DP — stateless и может не сохранять org_id). |
| `status` | string | Текущий статус обработки. См. таблицу статусов ниже. |
| `stage` | string | Внутренний этап обработки DP (для observability/debug, не отображается пользователю). |
| `message` | string | Человекочитаемое сообщение (для observability). |

**Статусы обработки:**

| Статус | Описание | Действие оркестратора |
|--------|----------|----------------------|
| `QUEUED` | Задание принято в очередь DP | SSE: уведомить пользователя "документ в очереди" |
| `IN_PROGRESS` | Обработка началась | SSE: уведомить пользователя "обработка идёт" |
| `COMPLETED` | Обработка завершена успешно | Ожидать `ProcessingCompletedEvent` для деталей |
| `COMPLETED_WITH_WARNINGS` | Обработка завершена с предупреждениями | Ожидать `ProcessingCompletedEvent` для деталей и warnings |
| `FAILED` | Обработка провалена | Ожидать `ProcessingFailedEvent` для деталей ошибки |
| `TIMED_OUT` | Обработка не завершена в отведённое время | SSE: уведомить пользователя, предложить повторить |
| `REJECTED` | Задание отклонено (невалидный файл) | SSE: уведомить пользователя об ошибке валидации |

---

#### 2.1.2 ProcessingCompletedEvent

**Направление:** DP --> Orchestrator
**Топик:** `dp.events.processing-completed`
**Обработчик:** Event Router --> Job Tracker --> SSE Publisher
**Idempotency key:** `dp-completed:{job_id}`

```json
{
  "correlation_id": "string (UUID)",
  "timestamp": "string (ISO 8601)",
  "job_id": "string (UUID)",
  "document_id": "string (UUID)",
  "version_id": "string (UUID)",
  "organization_id": "string (UUID, optional)",
  "warnings": [
    {
      "code": "string",
      "message": "string",
      "severity": "string (low | medium | high)"
    }
  ]
}
```

**Обязательные поля:** `correlation_id`, `timestamp`, `job_id`, `document_id`, `version_id`.
**Optional:** `organization_id`, `warnings` (omitempty; пустой массив `[]` если предупреждений нет).

**Описание полей:**

| Поле | Тип | Описание |
|------|-----|----------|
| `correlation_id` | UUID | Корреляция — совпадает с исходной командой. |
| `timestamp` | ISO 8601 | Время завершения обработки. |
| `job_id` | UUID | Идентификатор задания. |
| `document_id` | UUID | Идентификатор документа. |
| `version_id` | UUID | Идентификатор версии. |
| `organization_id` | UUID | Идентификатор организации. |
| `warnings` | array | Массив предупреждений, возникших при обработке. |
| `warnings[].code` | string | Машинный код предупреждения (e.g. `LOW_OCR_CONFIDENCE`, `TRUNCATED_TEXT`). |
| `warnings[].message` | string | Человекочитаемое описание предупреждения. |
| `warnings[].severity` | string | Уровень серьёзности: `low`, `medium`, `high`. |

> **Реакция оркестратора:** обновить внутренний статус задания на COMPLETED/COMPLETED_WITH_WARNINGS. Если есть warnings с severity=high — отобразить пользователю через SSE. Не публиковать дальнейших команд — следующий этап (юридический анализ LIC) запускается событием `VersionProcessingArtifactsReady` из DM.

---

#### 2.1.3 ProcessingFailedEvent

**Направление:** DP --> Orchestrator
**Топик:** `dp.events.processing-failed`
**Обработчик:** Event Router --> Job Tracker --> Retry Evaluator --> SSE Publisher
**Idempotency key:** `dp-failed:{job_id}`

```json
{
  "correlation_id": "string (UUID)",
  "timestamp": "string (ISO 8601)",
  "job_id": "string (UUID)",
  "document_id": "string (UUID)",
  "version_id": "string (UUID)",
  "organization_id": "string (UUID, optional)",
  "error_code": "string",
  "error_message": "string",
  "failed_at_stage": "string",
  "is_retryable": "boolean"
}
```

**Обязательные поля:** `correlation_id`, `timestamp`, `job_id`, `document_id`, `version_id`, `error_code`, `error_message`, `failed_at_stage`, `is_retryable`.
**Optional:** `organization_id` (omitempty).

**Описание полей:**

| Поле | Тип | Описание |
|------|-----|----------|
| `correlation_id` | UUID | Корреляция. |
| `timestamp` | ISO 8601 | Время ошибки. |
| `job_id` | UUID | Идентификатор задания. |
| `document_id` | UUID | Идентификатор документа. |
| `version_id` | UUID | Идентификатор версии. |
| `organization_id` | UUID | Идентификатор организации. |
| `error_code` | string | Машинный код ошибки (e.g. `INVALID_PDF`, `OCR_TIMEOUT`, `FETCH_FAILED`, `SSRF_BLOCKED`). |
| `error_message` | string | Человекочитаемое описание ошибки. |
| `failed_at_stage` | string | Этап обработки, на котором произошла ошибка (e.g. `VALIDATE`, `FETCH`, `OCR`, `TEXT_EXTRACT`, `STRUCTURE_EXTRACT`, `SEMANTIC_TREE`, `DM_PERSIST`). |
| `is_retryable` | boolean | `true` — оркестратор может повторить команду. `false` — ошибка постоянная, повтор бессмысленен. |

> **Реакция оркестратора:**
> - Если `is_retryable = true` — запланировать retry с exponential backoff (max 3 попытки). При исчерпании попыток — уведомить пользователя.
> - Если `is_retryable = false` — немедленно уведомить пользователя через SSE. Маппить `error_code` на user-friendly сообщение на русском (NFR-5.2).
> - Обновить статус задания в internal state.
> - Записать в audit log.

---

#### 2.1.4 ComparisonCompletedEvent

**Направление:** DP --> Orchestrator
**Топик:** `dp.events.comparison-completed`
**Обработчик:** Event Router --> Job Tracker --> SSE Publisher
**Idempotency key:** `dp-comparison-completed:{job_id}`

```json
{
  "correlation_id": "string (UUID)",
  "timestamp": "string (ISO 8601)",
  "job_id": "string (UUID)",
  "document_id": "string (UUID)",
  "organization_id": "string (UUID, optional)",
  "base_version_id": "string (UUID)",
  "target_version_id": "string (UUID)"
}
```

**Обязательные поля:** `correlation_id`, `timestamp`, `job_id`, `document_id`, `base_version_id`, `target_version_id`.
**Optional:** `organization_id` (omitempty).

**Описание полей:**

| Поле | Тип | Описание |
|------|-----|----------|
| `correlation_id` | UUID | Корреляция. |
| `timestamp` | ISO 8601 | Время завершения сравнения. |
| `job_id` | UUID | Идентификатор задания сравнения. |
| `document_id` | UUID | Идентификатор документа. |
| `organization_id` | UUID | Идентификатор организации. |
| `base_version_id` | UUID | Идентификатор базовой версии. |
| `target_version_id` | UUID | Идентификатор целевой версии. |

> **Реакция оркестратора:** уведомить пользователя через SSE о завершении сравнения. Результат (diff) доступен из DM по запросу.

---

#### 2.1.5 ComparisonFailedEvent

**Направление:** DP --> Orchestrator
**Топик:** `dp.events.comparison-failed`
**Обработчик:** Event Router --> Job Tracker --> Retry Evaluator --> SSE Publisher
**Idempotency key:** `dp-comparison-failed:{job_id}`

```json
{
  "correlation_id": "string (UUID)",
  "timestamp": "string (ISO 8601)",
  "job_id": "string (UUID)",
  "document_id": "string (UUID)",
  "organization_id": "string (UUID, optional)",
  "base_version_id": "string (UUID)",
  "target_version_id": "string (UUID)",
  "error_code": "string",
  "error_message": "string",
  "is_retryable": "boolean"
}
```

**Обязательные поля:** `correlation_id`, `timestamp`, `job_id`, `document_id`, `base_version_id`, `target_version_id`, `error_code`, `error_message`, `is_retryable`.
**Optional:** `organization_id` (omitempty).

**Описание полей:**

| Поле | Тип | Описание |
|------|-----|----------|
| `correlation_id` | UUID | Корреляция. |
| `timestamp` | ISO 8601 | Время ошибки. |
| `job_id` | UUID | Идентификатор задания сравнения. |
| `document_id` | UUID | Идентификатор документа. |
| `organization_id` | UUID | Идентификатор организации. |
| `base_version_id` | UUID | Идентификатор базовой версии. |
| `target_version_id` | UUID | Идентификатор целевой версии. |
| `error_code` | string | Машинный код ошибки (e.g. `TREE_NOT_FOUND`, `DM_UNAVAILABLE`, `COMPARISON_TIMEOUT`). |
| `error_message` | string | Человекочитаемое описание ошибки. |
| `is_retryable` | boolean | `true` — можно повторить, `false` — ошибка постоянная. |

> **Реакция оркестратора:** аналогична `ProcessingFailedEvent` — retry при `is_retryable = true`, уведомление при `false`.

---

### 2.2 События от LIC (Legal Intelligence Core)

> **ASSUMPTION-ORCH-13:** LIC публикует статусные события по аналогии с DP. LIC ещё не спроектирован — данная схема является архитектурным требованием к будущему домену.

#### 2.2.1 LICStatusChangedEvent

**Направление:** LIC --> Orchestrator
**Топик:** `lic.events.status-changed`
**Обработчик:** Event Router --> Processing Status Tracker --> SSE Publisher
**Idempotency key:** `lic-status:{job_id}:{status}`

```json
{
  "correlation_id": "string (UUID)",
  "timestamp": "string (ISO 8601)",
  "job_id": "string (UUID)",
  "document_id": "string (UUID)",
  "version_id": "string (UUID)",
  "organization_id": "string (UUID)",
  "status": "string (IN_PROGRESS | COMPLETED | FAILED)",
  "error_code": "string (optional, при FAILED)",
  "error_message": "string (optional, при FAILED)",
  "is_retryable": "boolean (optional, при FAILED)"
}
```

**Обязательные поля:** `correlation_id`, `timestamp`, `job_id`, `document_id`, `version_id`, `organization_id`, `status`.
**Optional:** `error_code`, `error_message`, `is_retryable` (заполняются при `status=FAILED`).

**Статусы:**

| Статус | Описание | Реакция оркестратора |
|--------|----------|----------------------|
| `IN_PROGRESS` | LIC начал юридический анализ | SSE: подтверждение статуса `ANALYZING` |
| `COMPLETED` | LIC завершил анализ успешно | Ожидать `VersionAnalysisArtifactsReady` от DM для перехода в `GENERATING_REPORTS` |
| `FAILED` | LIC не смог выполнить анализ | SSE: немедленное уведомление `ANALYSIS_FAILED` с `error_message`. Не ждать DM Watchdog |

> **Мгновенное обнаружение сбоя:** При `status=FAILED` оркестратор немедленно уведомляет пользователя через SSE, не дожидаясь DM Stale Version Watchdog (который сработает через 5–10 мин как safety net). Пользователь видит ошибку за секунды, а не за минуты.

---

### 2.3 События от RE (Reporting Engine)

> **ASSUMPTION-ORCH-13:** RE публикует статусные события по аналогии с DP. RE ещё не спроектирован — данная схема является архитектурным требованием к будущему домену.

#### 2.3.1 REStatusChangedEvent

**Направление:** RE --> Orchestrator
**Топик:** `re.events.status-changed`
**Обработчик:** Event Router --> Processing Status Tracker --> SSE Publisher
**Idempotency key:** `re-status:{job_id}:{status}`

```json
{
  "correlation_id": "string (UUID)",
  "timestamp": "string (ISO 8601)",
  "job_id": "string (UUID)",
  "document_id": "string (UUID)",
  "version_id": "string (UUID)",
  "organization_id": "string (UUID)",
  "status": "string (IN_PROGRESS | COMPLETED | FAILED)",
  "error_code": "string (optional, при FAILED)",
  "error_message": "string (optional, при FAILED)",
  "is_retryable": "boolean (optional, при FAILED)"
}
```

**Обязательные поля:** `correlation_id`, `timestamp`, `job_id`, `document_id`, `version_id`, `organization_id`, `status`.
**Optional:** `error_code`, `error_message`, `is_retryable` (заполняются при `status=FAILED`).

**Статусы:**

| Статус | Описание | Реакция оркестратора |
|--------|----------|----------------------|
| `IN_PROGRESS` | RE начал формирование отчётов | SSE: подтверждение статуса `GENERATING_REPORTS` |
| `COMPLETED` | RE завершил формирование отчётов | Ожидать `VersionReportsReady` от DM для перехода в `READY` |
| `FAILED` | RE не смог сформировать отчёты | SSE: немедленное уведомление `REPORTS_FAILED` с `error_message`. Не ждать DM Watchdog |

---

### 2.4 События от DM (Document Management)

#### 2.4.1 VersionProcessingArtifactsReady

**Направление:** DM --> Orchestrator
**Топик:** `dm.events.version-artifacts-ready`
**Обработчик:** Event Router --> Pipeline Tracker
**Idempotency key:** `dm-artifacts-ready:{document_id}:{version_id}`

```json
{
  "correlation_id": "string (UUID)",
  "timestamp": "string (ISO 8601)",
  "document_id": "string (UUID)",
  "version_id": "string (UUID)",
  "organization_id": "string (UUID)",
  "artifact_types": [
    "string (OCR_RAW | EXTRACTED_TEXT | DOCUMENT_STRUCTURE | SEMANTIC_TREE | PROCESSING_WARNINGS)"
  ]
}
```

**Обязательные поля:** `correlation_id`, `timestamp`, `document_id`, `version_id`, `organization_id`, `artifact_types`.

**Описание полей:**

| Поле | Тип | Описание |
|------|-----|----------|
| `correlation_id` | UUID | Корреляция. |
| `timestamp` | ISO 8601 | Время события. |
| `document_id` | UUID | Идентификатор документа. |
| `version_id` | UUID | Идентификатор версии. |
| `organization_id` | UUID | Идентификатор организации. |
| `artifact_types` | array | Список типов артефактов, сохранённых в DM. |

**Допустимые значения `artifact_types`:**

| Тип | Описание |
|-----|----------|
| `OCR_RAW` | Сырой результат OCR (страницы + confidence) |
| `EXTRACTED_TEXT` | Извлечённый текст документа |
| `DOCUMENT_STRUCTURE` | Структура документа (разделы, пункты, приложения, реквизиты) |
| `SEMANTIC_TREE` | Семантическое дерево документа |
| `PROCESSING_WARNINGS` | Предупреждения, возникшие при обработке |

> **Реакция оркестратора:** это информационное событие. Оркестратор обновляет pipeline tracker — артефакты DP сохранены в DM, юридический анализ (LIC) может быть запущен. Оркестратор НЕ запускает LIC напрямую — LIC самостоятельно подписан на этот топик.

---

#### 2.4.2 VersionAnalysisArtifactsReady

**Направление:** DM --> Orchestrator
**Топик:** `dm.events.version-analysis-ready`
**Обработчик:** Event Router --> Pipeline Tracker --> SSE Publisher
**Idempotency key:** `dm-analysis-ready:{document_id}:{version_id}`

```json
{
  "correlation_id": "string (UUID)",
  "timestamp": "string (ISO 8601)",
  "document_id": "string (UUID)",
  "version_id": "string (UUID)",
  "organization_id": "string (UUID)",
  "artifact_types": [
    "string (CLASSIFICATION_RESULT | KEY_PARAMETERS | RISK_ANALYSIS | RISK_PROFILE | RECOMMENDATIONS | SUMMARY | DETAILED_REPORT | AGGREGATE_SCORE)"
  ]
}
```

**Обязательные поля:** `correlation_id`, `timestamp`, `document_id`, `version_id`, `organization_id`, `artifact_types`.

**Описание полей:**

| Поле | Тип | Описание |
|------|-----|----------|
| `correlation_id` | UUID | Корреляция. |
| `timestamp` | ISO 8601 | Время события. |
| `document_id` | UUID | Идентификатор документа. |
| `version_id` | UUID | Идентификатор версии. |
| `organization_id` | UUID | Идентификатор организации. |
| `artifact_types` | array | Список типов артефактов юридического анализа, сохранённых в DM. |

**Допустимые значения `artifact_types`:**

| Тип | Описание |
|-----|----------|
| `CLASSIFICATION_RESULT` | Результат классификации типа договора |
| `KEY_PARAMETERS` | Ключевые параметры (стороны, предмет, цена, сроки, штрафы, юрисдикция) |
| `RISK_ANALYSIS` | Детальный анализ рисков |
| `RISK_PROFILE` | Профиль рисков (общий уровень, количество по категориям) |
| `RECOMMENDATIONS` | Рекомендации по улучшению формулировок |
| `SUMMARY` | Краткое резюме для нетехнических пользователей |
| `DETAILED_REPORT` | Детальный отчёт |
| `AGGREGATE_SCORE` | Агрегированная оценка договора |

> **Реакция оркестратора:** уведомить пользователя через SSE о доступности результатов анализа. Результаты юридического анализа теперь можно запросить через REST API. Генерация отчётов (RE) запускается следующим этапом (RE подписан на этот же топик).

---

#### 2.4.3 VersionReportsReady

**Направление:** DM --> Orchestrator
**Топик:** `dm.events.version-reports-ready`
**Обработчик:** Event Router --> Pipeline Tracker --> SSE Publisher
**Idempotency key:** `dm-reports-ready:{document_id}:{version_id}`

```json
{
  "correlation_id": "string (UUID)",
  "timestamp": "string (ISO 8601)",
  "document_id": "string (UUID)",
  "version_id": "string (UUID)",
  "organization_id": "string (UUID)",
  "artifact_types": [
    "string (EXPORT_PDF | EXPORT_DOCX)"
  ]
}
```

**Обязательные поля:** `correlation_id`, `timestamp`, `document_id`, `version_id`, `organization_id`, `artifact_types`.

**Описание полей:**

| Поле | Тип | Описание |
|------|-----|----------|
| `correlation_id` | UUID | Корреляция. |
| `timestamp` | ISO 8601 | Время события. |
| `document_id` | UUID | Идентификатор документа. |
| `version_id` | UUID | Идентификатор версии. |
| `organization_id` | UUID | Идентификатор организации. |
| `artifact_types` | array | Типы готовых отчётов. |

**Допустимые значения `artifact_types`:**

| Тип | Описание |
|-----|----------|
| `EXPORT_PDF` | Отчёт в формате PDF |
| `EXPORT_DOCX` | Отчёт в формате DOCX |

> **Реакция оркестратора:** уведомить пользователя через SSE о готовности отчётов для скачивания. Это финальный этап pipeline обработки документа. Пользователь может запросить presigned URL для скачивания через REST API (UR-10).

---

#### 2.4.4 VersionPartiallyAvailable

**Направление:** DM --> Orchestrator
**Топик:** `dm.events.version-partially-available`
**Обработчик:** Event Router --> Pipeline Tracker --> SSE Publisher
**Idempotency key:** `dm-partial:{document_id}:{version_id}`

```json
{
  "correlation_id": "string (UUID)",
  "timestamp": "string (ISO 8601)",
  "document_id": "string (UUID)",
  "version_id": "string (UUID)",
  "organization_id": "string (UUID)",
  "failed_stage": "string (optional)",
  "error_message": "string (optional)"
}
```

**Обязательные поля:** `correlation_id`, `timestamp`, `document_id`, `version_id`, `organization_id`.
**Optional:** `failed_stage`, `error_message` (omitempty).

**Описание полей:**

| Поле | Тип | Описание |
|------|-----|----------|
| `correlation_id` | UUID | Корреляция. |
| `timestamp` | ISO 8601 | Время события. |
| `document_id` | UUID | Идентификатор документа. |
| `version_id` | UUID | Идентификатор версии. |
| `organization_id` | UUID | Идентификатор организации. |
| `failed_stage` | string | Этап pipeline, на котором произошёл сбой (e.g. `LIC_ANALYSIS`, `RE_REPORT_GENERATION`). |
| `error_message` | string | Человекочитаемое описание проблемы. |

> **Реакция оркестратора:** уведомить пользователя через SSE о частичной доступности результатов. Пример: юридический анализ выполнен, но генерация отчёта (RE) провалилась — пользователь может видеть результаты анализа через UI, но скачивание PDF/DOCX недоступно. Оркестратор маппит `failed_stage` на user-friendly сообщение на русском.

---

#### 2.4.5 VersionCreated

**Направление:** DM --> Orchestrator
**Топик:** `dm.events.version-created`
**Обработчик:** Event Router --> Version Tracker --> SSE Publisher
**Idempotency key:** `dm-version-created:{document_id}:{version_id}`

```json
{
  "correlation_id": "string (UUID)",
  "timestamp": "string (ISO 8601)",
  "document_id": "string (UUID)",
  "version_id": "string (UUID)",
  "version_number": "int",
  "organization_id": "string (UUID)",
  "origin_type": "string (UPLOAD | RE_UPLOAD | RECOMMENDATION_APPLIED | MANUAL_EDIT | RE_CHECK)",
  "parent_version_id": "string (UUID, optional)",
  "created_by_user_id": "string (UUID)"
}
```

**Обязательные поля:** `correlation_id`, `timestamp`, `document_id`, `version_id`, `version_number`, `organization_id`, `origin_type`, `created_by_user_id`.
**Optional:** `parent_version_id` (omitempty; отсутствует для первой версии документа).

**Описание полей:**

| Поле | Тип | Описание |
|------|-----|----------|
| `correlation_id` | UUID | Корреляция. |
| `timestamp` | ISO 8601 | Время создания версии. |
| `document_id` | UUID | Идентификатор документа. |
| `version_id` | UUID | Идентификатор новой версии. |
| `version_number` | int | Порядковый номер версии (1, 2, 3, ...). |
| `organization_id` | UUID | Идентификатор организации. |
| `origin_type` | string | Причина создания версии. См. таблицу ниже. |
| `parent_version_id` | UUID | Идентификатор родительской версии (для RE_UPLOAD, RECOMMENDATION_APPLIED, MANUAL_EDIT, RE_CHECK). |
| `created_by_user_id` | UUID | Идентификатор пользователя, создавшего версию. |

**Допустимые значения `origin_type`:**

| Значение | Описание |
|----------|----------|
| `UPLOAD` | Первичная загрузка документа |
| `RE_UPLOAD` | Повторная загрузка файла для существующего документа |
| `RECOMMENDATION_APPLIED` | Пользователь применил рекомендацию LIC |
| `MANUAL_EDIT` | Пользователь внёс ручные правки |
| `RE_CHECK` | Повторная проверка существующей версии (UR-9) |

> **Реакция оркестратора:** зависит от `origin_type`:
> - `UPLOAD` / `RE_UPLOAD` — оркестратор уже инициировал создание версии, событие используется для подтверждения. SSE: уведомить пользователя.
> - `RECOMMENDATION_APPLIED` / `MANUAL_EDIT` — оркестратор публикует `ProcessDocumentRequested` для обработки новой версии.
> - `RE_CHECK` — оркестратор публикует `ProcessDocumentRequested` с тем же `source_file_key` для повторной обработки.

---

## 3. DLQ события

Все DLQ-записи оркестратора имеют единый envelope:

```json
{
  "original_topic": "string",
  "original_message": "object (raw JSON of the failed message)",
  "error_code": "string",
  "error_message": "string",
  "retry_count": "int",
  "correlation_id": "string (UUID)",
  "job_id": "string (UUID, optional)",
  "document_id": "string (UUID, optional)",
  "organization_id": "string (UUID, optional)",
  "failed_at": "string (ISO 8601)"
}
```

**Описание полей:**

| Поле | Тип | Описание |
|------|-----|----------|
| `original_topic` | string | Топик, из которого получено сообщение. |
| `original_message` | object | Оригинальное сообщение (raw JSON) для post-mortem анализа. |
| `error_code` | string | Код ошибки обработки (e.g. `DESERIALIZATION_FAILED`, `HANDLER_PANIC`, `RETRIES_EXHAUSTED`). |
| `error_message` | string | Описание ошибки. |
| `retry_count` | int | Количество выполненных retry до перемещения в DLQ. |
| `correlation_id` | UUID | Корреляция (извлечённая из original_message, если возможно). |
| `job_id` | UUID | Идентификатор задания (если удалось извлечь). |
| `document_id` | UUID | Идентификатор документа (если удалось извлечь). |
| `organization_id` | UUID | Идентификатор организации (если удалось извлечь). |
| `failed_at` | ISO 8601 | Время перемещения в DLQ. |

### DLQ-топики

| Топик | Описание |
|-------|----------|
| `orch.dlq.dp-events-failed` | Неудачная обработка событий из DP (после исчерпания retry) |
| `orch.dlq.dm-events-failed` | Неудачная обработка событий из DM (после исчерпания retry) |
| `orch.dlq.publish-failed` | Неудачная публикация исходящих команд |
| `orch.dlq.invalid-message` | Невалидная схема входящего сообщения (десериализация провалена) |

---

## 4. Общие правила

1. **Envelope:** Все события содержат `correlation_id` (UUID) и `timestamp` (ISO 8601) — совместимость с `EventMeta` из DP и DM.
2. **Backward compatibility:** Новые поля добавляются как optional с `omitempty`. Потребители игнорируют неизвестные поля.
3. **Serialization:** JSON. UTF-8.
4. **Correlation:** `correlation_id` пробрасывается из входящих событий во все исходящие сообщения. При получении HTTP-запроса оркестратор генерирует новый `correlation_id` и включает его во все последующие команды и логи.
5. **Idempotency:** Все входящие события обрабатываются идемпотентно. Повторное получение одного и того же события (at-least-once delivery) не приводит к дублированию действий. Ключи идемпотентности хранятся в Redis с TTL.
6. **Tenant isolation:** Оркестратор валидирует `organization_id` из события против контекста задания. Несовпадение — событие отклоняется и направляется в DLQ.
7. **Retry policy:** Для retryable-ошибок оркестратор применяет exponential backoff: 1s, 2s, 4s (max 3 попытки). После исчерпания попыток — DLQ + уведомление пользователя.
8. **SSE delivery:** Все статусные обновления доставляются пользователю через Server-Sent Events. Для маршрутизации используется `organization_id` + `document_id` как SSE channel.

---

## 5. Матрица топиков

### Исходящие (Orchestrator публикует)

| Топик | Событие | Потребитель |
|-------|---------|-------------|
| `dp.commands.process-document` | ProcessDocumentRequested | DP |
| `dp.commands.compare-versions` | CompareDocumentVersionsRequested | DP |

### Входящие (Orchestrator подписан)

| Топик | Событие | Источник |
|-------|---------|----------|
| `dp.events.status-changed` | StatusChangedEvent | DP |
| `dp.events.processing-completed` | ProcessingCompletedEvent | DP |
| `dp.events.processing-failed` | ProcessingFailedEvent | DP |
| `dp.events.comparison-completed` | ComparisonCompletedEvent | DP |
| `dp.events.comparison-failed` | ComparisonFailedEvent | DP |
| `dm.events.version-artifacts-ready` | VersionProcessingArtifactsReady | DM |
| `dm.events.version-analysis-ready` | VersionAnalysisArtifactsReady | DM |
| `dm.events.version-reports-ready` | VersionReportsReady | DM |
| `dm.events.version-partially-available` | VersionPartiallyAvailable | DM |
| `dm.events.version-created` | VersionCreated | DM |

### DLQ

| Топик | Описание |
|-------|----------|
| `orch.dlq.dp-events-failed` | Ошибки обработки DP-событий |
| `orch.dlq.dm-events-failed` | Ошибки обработки DM-событий |
| `orch.dlq.publish-failed` | Ошибки публикации команд |
| `orch.dlq.invalid-message` | Невалидные входящие сообщения |

---

## 6. Диаграмма потока событий

```
                                    +-----------+
                                    |  Frontend |
                                    +-----+-----+
                                          |
                                    REST / SSE
                                          |
                              +-----------+-----------+
                              |  API/Backend          |
                              |  Orchestrator         |
                              +---+-------+-------+---+
                                  |       |       |
              publish             |       |       |          subscribe
    +-------------------------+   |       |       |   +-------------------------+
    |                         v   |       |       v   |                         |
    |   dp.commands.process-document      |       dp.events.status-changed      |
    |   dp.commands.compare-versions      |       dp.events.processing-completed|
    |                                     |       dp.events.processing-failed   |
    |                                     |       dp.events.comparison-completed|
    |                                     |       dp.events.comparison-failed   |
    |              +------+               |                                     |
    +------------->|  DP  |               |       dm.events.version-artifacts-ready
                   +------+               |       dm.events.version-analysis-ready
                                          |       dm.events.version-reports-ready
                                          |       dm.events.version-partially-available
                              +-----------+       dm.events.version-created
                              |                                     |
                              v                                     |
                         +----+---+                                 |
                         |   DM   |---------------------------------+
                         +--------+
```

---

## 7. Жизненный цикл обработки документа (event flow)

Последовательность событий при загрузке нового документа:

```
1. [HTTP]  POST /api/v1/documents/upload
2. [S3]    Оркестратор загружает файл в Object Storage
3. [REST]  Оркестратор создаёт документ/версию в DM (sync)
4. [PUB]   Оркестратор --> dp.commands.process-document (ProcessDocumentRequested)
5. [HTTP]  Оркестратор --> 202 Accepted (client получает job_id)
6. [SUB]   Оркестратор <-- dp.events.status-changed (QUEUED)
7. [SSE]   Оркестратор --> Frontend: "Документ в очереди на обработку"
8. [SUB]   Оркестратор <-- dp.events.status-changed (IN_PROGRESS)
9. [SSE]   Оркестратор --> Frontend: "Обработка документа..."
10. [SUB]  Оркестратор <-- dp.events.processing-completed
11. [SUB]  Оркестратор <-- dm.events.version-artifacts-ready
12. [SSE]  Оркестратор --> Frontend: "Текст извлечён, юридический анализ..."
13.        LIC обрабатывает (Оркестратор не участвует)
14. [SUB]  Оркестратор <-- dm.events.version-analysis-ready
15. [SSE]  Оркестратор --> Frontend: "Анализ завершён, результаты доступны"
16.        RE генерирует отчёты (Оркестратор не участвует)
17. [SUB]  Оркестратор <-- dm.events.version-reports-ready
18. [SSE]  Оркестратор --> Frontend: "Отчёты готовы для скачивания"
```

**При ошибке на любом этапе:**

```
[SUB]  Оркестратор <-- dp.events.processing-failed (is_retryable=true)
       Оркестратор: retry с exponential backoff
[PUB]  Оркестратор --> dp.commands.process-document (retry)
       ...или после исчерпания попыток:
[SSE]  Оркестратор --> Frontend: "Не удалось обработать документ: {причина}"

[SUB]  Оркестратор <-- dm.events.version-partially-available
[SSE]  Оркестратор --> Frontend: "Результаты доступны частично"
```
