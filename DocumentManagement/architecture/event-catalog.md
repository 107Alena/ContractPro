# Event Catalog — Document Management

Полный каталог всех событий DM: входящие, исходящие, DLQ. Для каждого события — JSON schema, направление, топик, потребитель.

---

## 1. Входящие события (DM принимает)

### 1.1 DocumentProcessingArtifactsReady

**Направление:** DP → DM
**Топик:** `dp.artifacts.processing-ready`
**Обработчик:** Artifact Ingestion Service
**Idempotency key:** `dp-artifacts:{job_id}`

```json
{
  "correlation_id": "string (UUID)",
  "timestamp": "string (ISO 8601)",
  "job_id": "string (UUID)",
  "document_id": "string (UUID)",
  "version_id": "string (UUID)",
  "organization_id": "string (UUID, optional)",
  "ocr_raw": {
    "status": "string (completed | not_applicable)",
    "pages": [
      {
        "page_number": "int",
        "text": "string",
        "confidence": "float"
      }
    ]
  },
  "text": {
    "content": "string",
    "page_count": "int",
    "char_count": "int"
  },
  "structure": {
    "sections": ["..."],
    "clauses": ["..."],
    "appendices": ["..."],
    "party_details": "object | null"
  },
  "semantic_tree": {
    "root": {
      "id": "string",
      "type": "string",
      "text": "string",
      "children": ["...recursive"]
    }
  },
  "warnings": [
    {
      "code": "string",
      "message": "string",
      "severity": "string (low | medium | high)"
    }
  ]
}
```

**Обязательные поля:** `correlation_id`, `timestamp`, `job_id`, `document_id`, `version_id`, `ocr_raw`, `text`, `structure`, `semantic_tree`.
**Optional:** `organization_id`, `warnings` (omitempty).

---

### 1.2 GetSemanticTreeRequest

**Направление:** DP → DM
**Топик:** `dp.requests.semantic-tree`
**Обработчик:** Artifact Query Service
**Idempotency key:** `dp-tree-req:{job_id}:{version_id}`

```json
{
  "correlation_id": "string (UUID)",
  "timestamp": "string (ISO 8601)",
  "job_id": "string (UUID)",
  "document_id": "string (UUID)",
  "version_id": "string (UUID)",
  "organization_id": "string (UUID, optional)"
}
```

**Обязательные поля:** `correlation_id`, `timestamp`, `job_id`, `document_id`, `version_id`.

---

### 1.3 DocumentVersionDiffReady

**Направление:** DP → DM
**Топик:** `dp.artifacts.diff-ready`
**Обработчик:** Diff Storage Service
**Idempotency key:** `dp-diff:{job_id}`

```json
{
  "correlation_id": "string (UUID)",
  "timestamp": "string (ISO 8601)",
  "job_id": "string (UUID)",
  "document_id": "string (UUID)",
  "organization_id": "string (UUID, optional)",
  "base_version_id": "string (UUID)",
  "target_version_id": "string (UUID)",
  "text_diffs": [
    {
      "type": "string (added | removed | modified)",
      "path": "string",
      "old_text": "string | null",
      "new_text": "string | null"
    }
  ],
  "structural_diffs": [
    {
      "type": "string (added | removed | modified | moved)",
      "node_id": "string",
      "old_value": "object | null",
      "new_value": "object | null"
    }
  ],
  "text_diff_count": "int",
  "structural_diff_count": "int"
}
```

**Обязательные поля:** `correlation_id`, `timestamp`, `job_id`, `document_id`, `base_version_id`, `target_version_id`, `text_diffs`, `structural_diffs`, `text_diff_count`, `structural_diff_count`.

---

### 1.4 GetArtifactsRequest

**Направление:** LIC → DM или RE → DM
**Топики:** `lic.requests.artifacts`, `re.requests.artifacts`
**Обработчик:** Artifact Query Service
**Idempotency key:** `{lic|re}-get-artifacts:{job_id}:{version_id}`

```json
{
  "correlation_id": "string (UUID)",
  "timestamp": "string (ISO 8601)",
  "job_id": "string (UUID)",
  "document_id": "string (UUID)",
  "version_id": "string (UUID)",
  "organization_id": "string (UUID, optional)",
  "artifact_types": [
    "string (SEMANTIC_TREE | EXTRACTED_TEXT | DOCUMENT_STRUCTURE | RISK_ANALYSIS | RISK_PROFILE | SUMMARY | DETAILED_REPORT | KEY_PARAMETERS | AGGREGATE_SCORE | ...)"
  ]
}
```

**Обязательные поля:** `correlation_id`, `timestamp`, `job_id`, `document_id`, `version_id`, `artifact_types` (non-empty array).

---

### 1.5 LegalAnalysisArtifactsReady

**Направление:** LIC → DM
**Топик:** `lic.artifacts.analysis-ready`
**Обработчик:** Artifact Ingestion Service
**Idempotency key:** `lic-artifacts:{job_id}`

```json
{
  "correlation_id": "string (UUID)",
  "timestamp": "string (ISO 8601)",
  "job_id": "string (UUID)",
  "document_id": "string (UUID)",
  "version_id": "string (UUID)",
  "organization_id": "string (UUID, optional)",
  "classification_result": {
    "contract_type": "string",
    "confidence": "float"
  },
  "key_parameters": {
    "parties": ["string"],
    "subject": "string",
    "price": "string | null",
    "duration": "string | null",
    "penalties": "string | null",
    "jurisdiction": "string | null"
  },
  "risk_analysis": {
    "risks": [
      {
        "id": "string",
        "level": "string (high | medium | low)",
        "description": "string",
        "clause_ref": "string",
        "legal_basis": "string"
      }
    ]
  },
  "risk_profile": {
    "overall_level": "string (high | medium | low)",
    "high_count": "int",
    "medium_count": "int",
    "low_count": "int"
  },
  "recommendations": [
    {
      "risk_id": "string",
      "original_text": "string",
      "recommended_text": "string",
      "explanation": "string"
    }
  ],
  "summary": {
    "text": "string"
  },
  "detailed_report": {
    "sections": ["..."]
  },
  "aggregate_score": {
    "score": "float",
    "label": "string"
  }
}
```

**Обязательные поля:** `correlation_id`, `timestamp`, `job_id`, `document_id`, `version_id`, `classification_result`, `key_parameters`, `risk_analysis`, `risk_profile`, `recommendations`, `summary`, `detailed_report`, `aggregate_score`.

---

### 1.6 ReportsArtifactsReady

**Направление:** RE → DM
**Топик:** `re.artifacts.reports-ready`
**Обработчик:** Artifact Ingestion Service
**Idempotency key:** `re-reports:{job_id}`

```json
{
  "correlation_id": "string (UUID)",
  "timestamp": "string (ISO 8601)",
  "job_id": "string (UUID)",
  "document_id": "string (UUID)",
  "version_id": "string (UUID)",
  "organization_id": "string (UUID, optional)",
  "export_pdf": {
    "storage_key": "string (claim check — RE загружает blob в Object Storage до отправки события)",
    "file_name": "string",
    "size_bytes": "int",
    "content_hash": "string (SHA-256)"
  },
  "export_docx": {
    "storage_key": "string (claim check)",
    "file_name": "string",
    "size_bytes": "int",
    "content_hash": "string (SHA-256)"
  }
}
```

> **Claim check pattern (REV-015, BRE-004):** RE загружает blob (PDF/DOCX) в Object Storage до отправки события. В событие передаётся только `storage_key`. DM не получает бинарное содержимое через RabbitMQ — только метаданные. Это предотвращает 14 МБ+ сообщения в брокере.

**Обязательные поля:** `correlation_id`, `timestamp`, `job_id`, `document_id`, `version_id`. Как минимум один из `export_pdf`, `export_docx`.

---

## 2. Исходящие события (DM публикует)

### 2.1 Confirmations (ответы на входящие события)

#### DocumentProcessingArtifactsPersisted

**Топик:** `dm.responses.artifacts-persisted`
**Потребитель:** DP

```json
{
  "correlation_id": "string (UUID)",
  "timestamp": "string (ISO 8601)",
  "job_id": "string (UUID)",
  "document_id": "string (UUID)"
}
```

#### DocumentProcessingArtifactsPersistFailed

**Топик:** `dm.responses.artifacts-persist-failed`
**Потребитель:** DP

```json
{
  "correlation_id": "string (UUID)",
  "timestamp": "string (ISO 8601)",
  "job_id": "string (UUID)",
  "document_id": "string (UUID)",
  "error_code": "string (optional, e.g. DOCUMENT_NOT_FOUND, STORAGE_ERROR)",
  "error_message": "string",
  "is_retryable": "boolean"
}
```

#### SemanticTreeProvided

**Топик:** `dm.responses.semantic-tree-provided`
**Потребитель:** DP

```json
{
  "correlation_id": "string (UUID)",
  "timestamp": "string (ISO 8601)",
  "job_id": "string (UUID)",
  "document_id": "string (UUID)",
  "version_id": "string (UUID)",
  "semantic_tree": {
    "root": { "...": "SemanticTree or null if error" }
  },
  "error_code": "string (optional, e.g. VERSION_NOT_FOUND, ARTIFACT_NOT_FOUND)",
  "error_message": "string (optional)",
  "is_retryable": "boolean (optional, default false)"
}
```

> При ошибке: `semantic_tree.root = null`, `error_code` и `error_message` заполнены. Backward-compatible: поля error с `omitempty`.

#### ArtifactsProvided

**Топик:** `dm.responses.artifacts-provided`
**Потребитель:** LIC, RE

```json
{
  "correlation_id": "string (UUID)",
  "timestamp": "string (ISO 8601)",
  "job_id": "string (UUID)",
  "document_id": "string (UUID)",
  "version_id": "string (UUID)",
  "artifacts": {
    "SEMANTIC_TREE": { "root": { "...": "..." } },
    "EXTRACTED_TEXT": { "content": "...", "page_count": 10 },
    "RISK_ANALYSIS": { "risks": ["..."] }
  },
  "missing_types": ["string (artifact types that were not found)"],
  "error_code": "string (optional)",
  "error_message": "string (optional)"
}
```

> `artifacts` — map `artifact_type → artifact_content`. `missing_types` — типы, запрошенные, но не найденные (не ошибка — артефакт может ещё не существовать).

#### DocumentVersionDiffPersisted

**Топик:** `dm.responses.diff-persisted`
**Потребитель:** DP

```json
{
  "correlation_id": "string (UUID)",
  "timestamp": "string (ISO 8601)",
  "job_id": "string (UUID)",
  "document_id": "string (UUID)"
}
```

#### DocumentVersionDiffPersistFailed

**Топик:** `dm.responses.diff-persist-failed`
**Потребитель:** DP

```json
{
  "correlation_id": "string (UUID)",
  "timestamp": "string (ISO 8601)",
  "job_id": "string (UUID)",
  "document_id": "string (UUID)",
  "error_code": "string (optional)",
  "error_message": "string",
  "is_retryable": "boolean"
}
```

#### LegalAnalysisArtifactsPersisted

**Топик:** `dm.responses.lic-artifacts-persisted`
**Потребитель:** LIC

```json
{
  "correlation_id": "string (UUID)",
  "timestamp": "string (ISO 8601)",
  "job_id": "string (UUID)",
  "document_id": "string (UUID)"
}
```

#### LegalAnalysisArtifactsPersistFailed

**Топик:** `dm.responses.lic-artifacts-persist-failed`
**Потребитель:** LIC

```json
{
  "correlation_id": "string (UUID)",
  "timestamp": "string (ISO 8601)",
  "job_id": "string (UUID)",
  "document_id": "string (UUID)",
  "error_code": "string (optional)",
  "error_message": "string",
  "is_retryable": "boolean"
}
```

#### ReportsArtifactsPersisted

**Топик:** `dm.responses.re-reports-persisted`
**Потребитель:** RE

```json
{
  "correlation_id": "string (UUID)",
  "timestamp": "string (ISO 8601)",
  "job_id": "string (UUID)",
  "document_id": "string (UUID)"
}
```

#### ReportsArtifactsPersistFailed

**Топик:** `dm.responses.re-reports-persist-failed`
**Потребитель:** RE

```json
{
  "correlation_id": "string (UUID)",
  "timestamp": "string (ISO 8601)",
  "job_id": "string (UUID)",
  "document_id": "string (UUID)",
  "error_code": "string (optional)",
  "error_message": "string",
  "is_retryable": "boolean"
}
```

---

### 2.2 Notifications (уведомления для нижестоящих доменов)

#### VersionProcessingArtifactsReady

**Топик:** `dm.events.version-artifacts-ready`
**Потребитель:** LIC

```json
{
  "correlation_id": "string (UUID)",
  "timestamp": "string (ISO 8601)",
  "document_id": "string (UUID)",
  "version_id": "string (UUID)",
  "organization_id": "string (UUID)",
  "artifact_types": ["OCR_RAW", "EXTRACTED_TEXT", "DOCUMENT_STRUCTURE", "SEMANTIC_TREE", "PROCESSING_WARNINGS"]
}
```

> `artifact_types` — список типов артефактов, которые были сохранены. Позволяет потребителю знать, что доступно, до запроса `GetArtifactsRequest`.

#### VersionAnalysisArtifactsReady

**Топик:** `dm.events.version-analysis-ready`
**Потребитель:** RE

```json
{
  "correlation_id": "string (UUID)",
  "timestamp": "string (ISO 8601)",
  "document_id": "string (UUID)",
  "version_id": "string (UUID)",
  "organization_id": "string (UUID)",
  "artifact_types": ["CLASSIFICATION_RESULT", "KEY_PARAMETERS", "RISK_ANALYSIS", "RISK_PROFILE", "RECOMMENDATIONS", "SUMMARY", "DETAILED_REPORT", "AGGREGATE_SCORE"]
}
```

#### VersionReportsReady

**Топик:** `dm.events.version-reports-ready`
**Потребитель:** Orchestrator / API

```json
{
  "correlation_id": "string (UUID)",
  "timestamp": "string (ISO 8601)",
  "document_id": "string (UUID)",
  "version_id": "string (UUID)",
  "organization_id": "string (UUID)",
  "artifact_types": ["EXPORT_PDF", "EXPORT_DOCX"]
}
```

#### VersionCreated

**Топик:** `dm.events.version-created`
**Потребитель:** Orchestrator

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

---

## 3. DLQ события

Все DLQ-записи имеют единый envelope:

```json
{
  "original_topic": "string",
  "original_message": "object (raw JSON of the failed message)",
  "error_code": "string",
  "error_message": "string",
  "retry_count": "int",
  "correlation_id": "string (UUID)",
  "job_id": "string (UUID)",
  "failed_at": "string (ISO 8601)"
}
```

### DLQ-топики

| Топик | Описание |
|-------|----------|
| `dm.dlq.ingestion-failed` | Неудачный приём артефактов (после исчерпания retry) |
| `dm.dlq.query-failed` | Неудачное чтение (semantic tree / artifacts request) |
| `dm.dlq.invalid-message` | Невалидная схема входящего сообщения |

---

## 4. Общие правила

1. **Envelope:** Все события содержат `correlation_id` (UUID) и `timestamp` (ISO 8601) — наследие `EventMeta` из DP.
2. **Backward compatibility:** Новые поля добавляются как optional с `omitempty`. Потребители игнорируют неизвестные поля.
3. **Schema versioning:** Каждый артефакт в `ArtifactDescriptor` несёт `schema_version`. При breaking change — новый `schema_version`, DM поддерживает чтение обеих версий в transition period.
4. **Correlation:** `correlation_id` пробрасывается из входящего события во все исходящие ответы/уведомления.
5. **Serialization:** JSON. UTF-8.
