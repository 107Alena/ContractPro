# Sequence Diagrams — Document Management

Диаграммы последовательности для каждого сценария из раздела 8 high-architecture.md. Формат: Mermaid.

---

## 8.1 Сохранение артефактов после обработки документа

```mermaid
sequenceDiagram
    participant DP as Document Processing
    participant RMQ as RabbitMQ
    participant EC as Event Consumer
    participant IG as Idempotency Guard
    participant Redis
    participant AIS as Artifact Ingestion Service
    participant S3 as Object Storage
    participant DB as PostgreSQL
    participant OP as Outbox Poller
    participant NP as Notification Publisher

    DP->>RMQ: DocumentProcessingArtifactsReady
    RMQ->>EC: deliver message
    EC->>EC: deserialize + validate contract
    EC->>IG: check idempotency
    IG->>Redis: GET dp-artifacts:{job_id}
    Redis-->>IG: not found
    IG->>Redis: SET dp-artifacts:{job_id} = PROCESSING (TTL 24h)
    IG->>AIS: route event

    AIS->>DB: SELECT document + version (validate existence)
    DB-->>AIS: OK

    loop for each artifact (ocr_raw, text, structure, semantic_tree, warnings)
        AIS->>S3: PutObject({org_id}/{doc_id}/{ver_id}/{type})
        S3-->>AIS: storage_key
    end

    AIS->>DB: BEGIN TX
    AIS->>DB: INSERT artifact_descriptors × N
    AIS->>DB: UPDATE document_versions SET artifact_status = PROCESSING_ARTIFACTS_RECEIVED
    AIS->>DB: INSERT audit_record
    AIS->>DB: INSERT outbox_events (artifacts-persisted + version-artifacts-ready)
    AIS->>DB: COMMIT

    AIS->>Redis: SET dp-artifacts:{job_id} = COMPLETED

    OP->>DB: SELECT outbox_events WHERE status = PENDING
    OP->>RMQ: publish DocumentProcessingArtifactsPersisted
    OP->>RMQ: publish VersionProcessingArtifactsReady
    OP->>DB: UPDATE outbox_events SET status = PUBLISHED

    EC->>RMQ: ACK

    Note over RMQ: LIC подписан на dm.events.version-artifacts-ready
```

### Альтернатива: документ не найден

```mermaid
sequenceDiagram
    participant DP as Document Processing
    participant RMQ as RabbitMQ
    participant EC as Event Consumer
    participant IG as Idempotency Guard
    participant AIS as Artifact Ingestion Service
    participant DB as PostgreSQL

    DP->>RMQ: DocumentProcessingArtifactsReady
    RMQ->>EC: deliver message
    EC->>IG: check (new)
    IG->>AIS: route event
    AIS->>DB: SELECT document + version
    DB-->>AIS: NOT FOUND

    AIS->>RMQ: DocumentProcessingArtifactsPersistFailed (DOCUMENT_NOT_FOUND, is_retryable=false)
    EC->>RMQ: ACK
```

### Альтернатива: Object Storage недоступен

```mermaid
sequenceDiagram
    participant RMQ as RabbitMQ
    participant EC as Event Consumer
    participant IG as Idempotency Guard
    participant AIS as Artifact Ingestion Service
    participant S3 as Object Storage

    RMQ->>EC: deliver message
    EC->>IG: check (new)
    IG->>AIS: route event
    AIS->>S3: PutObject
    S3-->>AIS: timeout / 5xx

    AIS->>AIS: retry 1..3 (exponential backoff)
    S3-->>AIS: still failing

    AIS->>RMQ: DocumentProcessingArtifactsPersistFailed (is_retryable=true)
    AIS->>RMQ: send to dm.dlq.ingestion-failed
    EC->>RMQ: ACK
```

---

## 8.2 Создание новой версии документа

```mermaid
sequenceDiagram
    participant Client as API / Backend Orchestrator
    participant API as API Handler
    participant ACE as Auth Context Extractor
    participant VMS as Version Management Service
    participant S3 as Object Storage
    participant DB as PostgreSQL
    participant OP as Outbox Poller
    participant RMQ as RabbitMQ

    Client->>API: POST /documents/{id}/versions
    API->>ACE: extract organization_id, user_id
    ACE-->>API: auth context

    API->>VMS: createVersion(doc_id, origin_type, source_file_key, ...)
    VMS->>DB: SELECT document WHERE document_id AND organization_id
    DB-->>VMS: document (status=ACTIVE)

    VMS->>S3: HEAD source_file_key
    S3-->>VMS: 200 OK (file exists)

    VMS->>DB: BEGIN TX
    VMS->>DB: INSERT document_versions (version_number=max+1, parent_version_id=current)
    VMS->>DB: UPDATE documents SET current_version_id, updated_at
    VMS->>DB: INSERT audit_record
    VMS->>DB: INSERT outbox_events (version-created)
    VMS->>DB: COMMIT

    OP->>DB: SELECT outbox_events WHERE status = PENDING
    OP->>RMQ: publish VersionCreated
    OP->>DB: UPDATE outbox_events SET status = PUBLISHED

    VMS-->>API: new version metadata
    API-->>Client: HTTP 201 Created
```

### Альтернатива: конфликт версии (race condition)

```mermaid
sequenceDiagram
    participant A as Request A
    participant B as Request B
    participant VMS as Version Management Service
    participant DB as PostgreSQL

    A->>VMS: createVersion(doc_id)
    B->>VMS: createVersion(doc_id)

    VMS->>DB: INSERT version_number=3 (request A)
    VMS->>DB: INSERT version_number=3 (request B)

    DB-->>VMS: OK (request A)
    DB-->>VMS: UNIQUE CONSTRAINT VIOLATION (request B)

    VMS->>VMS: retry request B with version_number=4
    VMS->>DB: INSERT version_number=4
    DB-->>VMS: OK
```

---

## 8.3 Выдача semantic tree для сравнения версий

```mermaid
sequenceDiagram
    participant DP as Document Processing
    participant RMQ as RabbitMQ
    participant EC as Event Consumer
    participant IG as Idempotency Guard
    participant Redis
    participant AQS as Artifact Query Service
    participant DB as PostgreSQL
    participant S3 as Object Storage

    DP->>RMQ: GetSemanticTreeRequest (version_id, correlation_id)
    RMQ->>EC: deliver message
    EC->>IG: check dp-tree-req:{job_id}:{version_id}
    IG->>Redis: GET key
    Redis-->>IG: not found
    IG->>Redis: SET key = PROCESSING
    IG->>AQS: route event

    AQS->>DB: SELECT artifact_descriptor WHERE version_id AND type=SEMANTIC_TREE
    DB-->>AQS: artifact_descriptor (storage_key)

    AQS->>S3: GetObject(storage_key)
    S3-->>AQS: blob (SemanticTree JSON)

    AQS->>DB: INSERT audit_record (ARTIFACT_READ)
    AQS->>RMQ: SemanticTreeProvided (correlation_id, semantic_tree)
    EC->>RMQ: ACK
```

### Альтернатива: версия или артефакт не найдены

```mermaid
sequenceDiagram
    participant DP as Document Processing
    participant RMQ as RabbitMQ
    participant EC as Event Consumer
    participant AQS as Artifact Query Service
    participant DB as PostgreSQL

    DP->>RMQ: GetSemanticTreeRequest (version_id)
    RMQ->>EC: deliver message
    EC->>AQS: route event (after idempotency check)

    AQS->>DB: SELECT artifact_descriptor WHERE version_id AND type=SEMANTIC_TREE
    DB-->>AQS: NOT FOUND

    AQS->>RMQ: SemanticTreeProvided (error_code, error_message, is_retryable=false, empty tree)
    EC->>RMQ: ACK

    Note over DP: DP receiver: error_message != "" → ReceiveError → FAILED
```

---

## 8.4 Сохранение результата сравнения версий

```mermaid
sequenceDiagram
    participant DP as Document Processing
    participant RMQ as RabbitMQ
    participant EC as Event Consumer
    participant IG as Idempotency Guard
    participant Redis
    participant DSS as Diff Storage Service
    participant S3 as Object Storage
    participant DB as PostgreSQL
    participant OP as Outbox Poller

    DP->>RMQ: DocumentVersionDiffReady (base_version_id, target_version_id)
    RMQ->>EC: deliver message
    EC->>IG: check dp-diff:{job_id}
    IG->>Redis: GET key
    Redis-->>IG: not found
    IG->>Redis: SET key = PROCESSING
    IG->>DSS: route event

    DSS->>DB: SELECT versions (validate both exist, same document)
    DB-->>DSS: OK

    DSS->>DSS: serialize diff → JSON blob
    DSS->>S3: PutObject({org_id}/{doc_id}/diffs/{base}_{target})
    S3-->>DSS: storage_key

    DSS->>DB: BEGIN TX
    DSS->>DB: INSERT version_diff_references
    DSS->>DB: INSERT audit_record
    DSS->>DB: INSERT outbox_events (diff-persisted)
    DSS->>DB: COMMIT

    DSS->>Redis: SET dp-diff:{job_id} = COMPLETED

    OP->>DB: SELECT outbox_events WHERE status = PENDING
    OP->>RMQ: publish DocumentVersionDiffPersisted
    OP->>DB: UPDATE outbox_events SET status = PUBLISHED

    EC->>RMQ: ACK
```

---

## 8.5 Сохранение результатов LIC

```mermaid
sequenceDiagram
    participant LIC as Legal Intelligence Core
    participant RMQ as RabbitMQ
    participant EC as Event Consumer
    participant IG as Idempotency Guard
    participant Redis
    participant AIS as Artifact Ingestion Service
    participant S3 as Object Storage
    participant DB as PostgreSQL
    participant OP as Outbox Poller

    LIC->>RMQ: LegalAnalysisArtifactsReady
    RMQ->>EC: deliver message
    EC->>IG: check lic-artifacts:{job_id}
    IG->>Redis: GET key → not found → SET PROCESSING
    IG->>AIS: route event

    AIS->>DB: validate document + version
    DB-->>AIS: OK

    loop for each artifact (classification, parameters, risks, profile, recommendations, summary, report, score)
        AIS->>S3: PutObject
        S3-->>AIS: storage_key
    end

    AIS->>DB: BEGIN TX
    AIS->>DB: INSERT artifact_descriptors × N
    AIS->>DB: UPDATE document_versions SET artifact_status = ANALYSIS_ARTIFACTS_RECEIVED
    AIS->>DB: INSERT audit_record
    AIS->>DB: INSERT outbox_events (lic-artifacts-persisted + version-analysis-ready)
    AIS->>DB: COMMIT

    AIS->>Redis: SET lic-artifacts:{job_id} = COMPLETED

    OP->>RMQ: publish LegalAnalysisArtifactsPersisted
    OP->>RMQ: publish VersionAnalysisArtifactsReady

    EC->>RMQ: ACK

    Note over RMQ: RE подписан на dm.events.version-analysis-ready
```

---

## 8.6 Сохранение результатов Reporting Engine

### Получение артефактов для формирования отчёта (RE ← DM)

```mermaid
sequenceDiagram
    participant DM_NP as DM Notification Publisher
    participant RMQ as RabbitMQ
    participant RE as Reporting Engine
    participant EC as DM Event Consumer
    participant AQS as DM Artifact Query Service
    participant S3 as Object Storage
    participant DB as PostgreSQL

    DM_NP->>RMQ: VersionAnalysisArtifactsReady
    RMQ->>RE: deliver notification

    RE->>RMQ: GetArtifactsRequest (version_id, artifact_types=[RISK_ANALYSIS, RISK_PROFILE, SUMMARY, ...])
    RMQ->>EC: deliver request
    EC->>AQS: route event (after idempotency check)

    loop for each requested artifact_type
        AQS->>DB: SELECT artifact_descriptor WHERE version_id AND type
        DB-->>AQS: storage_key
        AQS->>S3: GetObject(storage_key)
        S3-->>AQS: blob
    end

    AQS->>RMQ: ArtifactsProvided (all requested artifacts in one message)
    EC->>RMQ: ACK
    RMQ->>RE: deliver ArtifactsProvided

    RE->>RE: формирование отчёта (PDF/DOCX)
```

### Сохранение экспортных артефактов (RE → DM)

```mermaid
sequenceDiagram
    participant RE as Reporting Engine
    participant RMQ as RabbitMQ
    participant EC as Event Consumer
    participant IG as Idempotency Guard
    participant Redis
    participant AIS as Artifact Ingestion Service
    participant S3 as Object Storage
    participant DB as PostgreSQL
    participant OP as Outbox Poller

    RE->>RMQ: ReportsArtifactsReady (EXPORT_PDF, EXPORT_DOCX)
    RMQ->>EC: deliver message
    EC->>IG: check re-reports:{job_id}
    IG->>Redis: GET key → not found → SET PROCESSING
    IG->>AIS: route event

    AIS->>DB: validate document + version
    DB-->>AIS: OK

    loop for each artifact (EXPORT_PDF, EXPORT_DOCX)
        AIS->>S3: PutObject
        S3-->>AIS: storage_key
    end

    AIS->>DB: BEGIN TX
    AIS->>DB: INSERT artifact_descriptors × N
    AIS->>DB: UPDATE document_versions SET artifact_status = REPORTS_READY
    AIS->>DB: INSERT audit_record
    AIS->>DB: INSERT outbox_events (re-reports-persisted + version-reports-ready)
    AIS->>DB: COMMIT

    AIS->>Redis: SET re-reports:{job_id} = COMPLETED

    OP->>RMQ: publish ReportsArtifactsPersisted
    OP->>RMQ: publish VersionReportsReady

    EC->>RMQ: ACK

    Note over RMQ: Orchestrator подписан на dm.events.version-reports-ready
```

---

## 8.7 Получение артефактов для API / UI

```mermaid
sequenceDiagram
    participant Client as API / Backend Orchestrator
    participant API as API Handler
    participant ACE as Auth Context Extractor
    participant AQS as Artifact Query Service
    participant DB as PostgreSQL
    participant S3 as Object Storage

    Client->>API: GET /documents/{id}/versions/{vid}/artifacts/{type}
    API->>ACE: extract organization_id
    ACE-->>API: auth context

    API->>AQS: getArtifact(doc_id, version_id, type, org_id)
    AQS->>DB: SELECT artifact_descriptor WHERE version_id AND type AND organization_id
    DB-->>AQS: artifact_descriptor (storage_key, size_bytes)

    alt Small artifact (JSON metadata)
        AQS->>S3: GetObject(storage_key)
        S3-->>AQS: blob
        AQS-->>API: artifact content
        API-->>Client: HTTP 200 (application/json)
    else Large artifact (PDF/DOCX)
        AQS->>S3: GeneratePresignedURL(storage_key, TTL=15min)
        S3-->>AQS: presigned URL
        AQS-->>API: presigned URL
        API-->>Client: HTTP 302 Redirect → presigned URL
    end

    AQS->>DB: INSERT audit_record (ARTIFACT_READ)
```

---

## 8.8 Повторная доставка одного и того же события

```mermaid
sequenceDiagram
    participant DP as Document Processing
    participant RMQ as RabbitMQ
    participant EC as Event Consumer
    participant IG as Idempotency Guard
    participant Redis

    DP->>RMQ: DocumentProcessingArtifactsReady (duplicate)
    RMQ->>EC: deliver message
    EC->>IG: check dp-artifacts:{job_id}

    IG->>Redis: GET dp-artifacts:{job_id}
    Redis-->>IG: {status: COMPLETED, result_snapshot: {...}}

    IG->>RMQ: re-publish DocumentProcessingArtifactsPersisted (from snapshot)
    EC->>RMQ: ACK

    Note over EC: No processing performed. Cost: 1 Redis GET + 1 publish.
```

---

## 8.9 Ошибка частичного сохранения

```mermaid
sequenceDiagram
    participant RMQ as RabbitMQ
    participant EC as Event Consumer
    participant AIS as Artifact Ingestion Service
    participant S3 as Object Storage
    participant DB as PostgreSQL

    RMQ->>EC: deliver DocumentProcessingArtifactsReady
    EC->>AIS: route event (after idempotency check)

    AIS->>S3: PutObject(ocr_raw) → OK
    AIS->>S3: PutObject(text) → OK
    AIS->>S3: PutObject(structure) → OK
    AIS->>S3: PutObject(semantic_tree) → TIMEOUT

    Note over AIS: 4th blob failed. Compensation:

    AIS->>S3: DeleteObject(ocr_raw)
    AIS->>S3: DeleteObject(text)
    AIS->>S3: DeleteObject(structure)

    Note over AIS: DB transaction never started — no rollback needed

    AIS->>RMQ: NACK (requeue for retry)

    Note over RMQ: Broker redelivers after backoff

    RMQ->>EC: redeliver message (retry 1)
    EC->>AIS: route event (idempotency key = PROCESSING → allow retry)

    AIS->>S3: PutObject(ocr_raw) → OK
    AIS->>S3: PutObject(text) → OK
    AIS->>S3: PutObject(structure) → OK
    AIS->>S3: PutObject(semantic_tree) → OK
    AIS->>S3: PutObject(warnings) → OK

    AIS->>DB: BEGIN TX → INSERT × 5 + UPDATE status + audit → COMMIT
    AIS->>RMQ: ACK
```

### Альтернатива: retry исчерпан

```mermaid
sequenceDiagram
    participant RMQ as RabbitMQ
    participant EC as Event Consumer
    participant AIS as Artifact Ingestion Service
    participant S3 as Object Storage

    Note over RMQ: Retry 3/3 — last attempt

    RMQ->>EC: redeliver message (retry 3)
    EC->>AIS: route event

    AIS->>S3: PutObject → TIMEOUT (still failing)
    AIS->>AIS: compensation (delete saved blobs)

    AIS->>RMQ: DocumentProcessingArtifactsPersistFailed (is_retryable=true)
    AIS->>RMQ: send original message to dm.dlq.ingestion-failed
    EC->>RMQ: ACK

    Note over RMQ: artifact_status remains PENDING
```

---

## 8.10 Конфликт версии

```mermaid
sequenceDiagram
    participant A as Request A
    participant B as Request B
    participant API as API Handler
    participant VMS as Version Management Service
    participant DB as PostgreSQL

    A->>API: POST /documents/{id}/versions
    B->>API: POST /documents/{id}/versions

    API->>VMS: createVersion (request A)
    API->>VMS: createVersion (request B)

    VMS->>DB: SELECT max(version_number) → 2
    VMS->>DB: SELECT max(version_number) → 2

    VMS->>DB: BEGIN TX (A): INSERT version_number=3
    VMS->>DB: BEGIN TX (B): INSERT version_number=3

    DB-->>VMS: COMMIT (A) → OK
    DB-->>VMS: COMMIT (B) → UNIQUE CONSTRAINT VIOLATION

    VMS->>VMS: retry (B): version_number = max+1 = 4
    VMS->>DB: BEGIN TX (B retry): INSERT version_number=4
    DB-->>VMS: COMMIT → OK

    VMS-->>API: version 3 (request A)
    VMS-->>API: version 4 (request B)
    API-->>A: HTTP 201
    API-->>B: HTTP 201
```

### Альтернатива: retry исчерпан

```mermaid
sequenceDiagram
    participant Client as Client
    participant API as API Handler
    participant VMS as Version Management Service
    participant DB as PostgreSQL

    Client->>API: POST /documents/{id}/versions

    loop 3 attempts
        VMS->>DB: INSERT version → UNIQUE CONSTRAINT VIOLATION
    end

    VMS-->>API: error (max retries exceeded)
    API-->>Client: HTTP 409 Conflict
```

---

## 8.11 Таймаут или недоступность зависимого хранилища

### PostgreSQL недоступен (sync API)

```mermaid
sequenceDiagram
    participant Client as Client
    participant API as API Handler
    participant DB as PostgreSQL
    participant LB as Load Balancer
    participant Health as Health Check

    Client->>API: GET /documents/{id}
    API->>DB: SELECT ...
    DB-->>API: connection refused / timeout

    API-->>Client: HTTP 503 Service Unavailable

    Health->>DB: readiness check
    DB-->>Health: connection refused
    Health->>Health: readiness = NOT READY
    LB->>Health: /readyz
    Health-->>LB: 503
    LB->>LB: remove instance from pool
```

### PostgreSQL недоступен (async)

```mermaid
sequenceDiagram
    participant RMQ as RabbitMQ
    participant EC as Event Consumer
    participant AIS as Artifact Ingestion Service
    participant DB as PostgreSQL

    RMQ->>EC: deliver message
    EC->>AIS: route event (after idempotency check)
    AIS->>DB: SELECT document
    DB-->>AIS: connection refused

    AIS->>RMQ: NACK (requeue)

    Note over RMQ: Message returns to queue. Will be redelivered when DB recovers.
```

### Redis недоступен

```mermaid
sequenceDiagram
    participant RMQ as RabbitMQ
    participant EC as Event Consumer
    participant IG as Idempotency Guard
    participant Redis
    participant DB as PostgreSQL
    participant AIS as Artifact Ingestion Service

    RMQ->>EC: deliver message
    EC->>IG: check idempotency

    IG->>Redis: GET key
    Redis-->>IG: connection refused

    Note over IG: Fallback to DB check

    IG->>DB: SELECT artifact_descriptor WHERE job_id AND artifact_type
    DB-->>IG: NOT FOUND (new event)

    IG->>AIS: route event (proceed without Redis)

    Note over IG: Performance degradation, but not blocking
```
