# Хранение и консистентность Document Management

## 1. Metadata store

**Технология:** PostgreSQL 15+.

**Схема (основные таблицы):**

```sql
-- documents
CREATE TABLE documents (
    document_id       UUID PRIMARY KEY,
    organization_id   UUID NOT NULL,
    title             TEXT NOT NULL,
    current_version_id UUID,
    status            TEXT NOT NULL DEFAULT 'ACTIVE',
    created_by_user_id UUID NOT NULL,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at        TIMESTAMPTZ
);
CREATE INDEX idx_documents_org ON documents(organization_id);

-- document_versions
CREATE TABLE document_versions (
    version_id        UUID PRIMARY KEY,
    document_id       UUID NOT NULL REFERENCES documents(document_id),
    version_number    INT NOT NULL,
    parent_version_id UUID REFERENCES document_versions(version_id),
    origin_type       TEXT NOT NULL,
    origin_description TEXT,
    source_file_key   TEXT NOT NULL,
    source_file_name  TEXT NOT NULL,
    source_file_size  BIGINT NOT NULL,
    source_file_checksum TEXT NOT NULL,
    organization_id   UUID NOT NULL,
    artifact_status   TEXT NOT NULL DEFAULT 'PENDING',
    created_by_user_id UUID NOT NULL,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(document_id, version_number)
);
CREATE INDEX idx_versions_doc ON document_versions(document_id);
CREATE INDEX idx_versions_org ON document_versions(organization_id);

-- artifact_descriptors
CREATE TABLE artifact_descriptors (
    artifact_id       UUID PRIMARY KEY,
    version_id        UUID NOT NULL REFERENCES document_versions(version_id),
    document_id       UUID NOT NULL,
    organization_id   UUID NOT NULL,
    artifact_type     TEXT NOT NULL,
    producer_domain   TEXT NOT NULL,
    storage_key       TEXT NOT NULL,
    size_bytes        BIGINT NOT NULL,
    content_hash      TEXT NOT NULL,
    schema_version    TEXT NOT NULL DEFAULT '1.0',
    job_id            TEXT NOT NULL,
    correlation_id    TEXT NOT NULL,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(version_id, artifact_type)
);
CREATE INDEX idx_artifacts_version ON artifact_descriptors(version_id);
CREATE INDEX idx_artifacts_org ON artifact_descriptors(organization_id);

-- version_diff_references
CREATE TABLE version_diff_references (
    diff_id           UUID PRIMARY KEY,
    document_id       UUID NOT NULL REFERENCES documents(document_id),
    organization_id   UUID NOT NULL,
    base_version_id   UUID NOT NULL REFERENCES document_versions(version_id),
    target_version_id UUID NOT NULL REFERENCES document_versions(version_id),
    storage_key       TEXT NOT NULL,
    text_diff_count   INT NOT NULL DEFAULT 0,
    structural_diff_count INT NOT NULL DEFAULT 0,
    job_id            TEXT NOT NULL,
    correlation_id    TEXT NOT NULL,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(base_version_id, target_version_id)
);

-- audit_records
CREATE TABLE audit_records (
    audit_id          UUID PRIMARY KEY,
    organization_id   UUID NOT NULL,
    document_id       UUID,
    version_id        UUID,
    action            TEXT NOT NULL,
    actor_type        TEXT NOT NULL,
    actor_id          TEXT NOT NULL,
    job_id            TEXT,
    correlation_id    TEXT,
    details           JSONB,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_audit_org_time ON audit_records(organization_id, created_at);
CREATE INDEX idx_audit_doc ON audit_records(document_id);
CREATE INDEX idx_audit_version ON audit_records(version_id) WHERE version_id IS NOT NULL;

-- orphan_candidates (BRE-008: таблица вместо full S3 scan)
CREATE TABLE orphan_candidates (
    storage_key  TEXT PRIMARY KEY,
    version_id   UUID,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- дополнительные индексы
CREATE INDEX idx_artifacts_storage_key ON artifact_descriptors(storage_key);
CREATE INDEX idx_documents_deleted ON documents(deleted_at) WHERE deleted_at IS NOT NULL;
```

## 2. Blob/Object storage

**Технология:** Yandex Object Storage (S3-compatible).

**Bucket:** `contractpro-dm-artifacts` (один bucket, логическое разделение через prefix).

**Naming convention:** `{organization_id}/{document_id}/{version_id}/{artifact_type}` — уникальный путь, совпадает с unique constraint в metadata store.

**Для diff:** `{organization_id}/{document_id}/diffs/{base_version_id}_{target_version_id}`.

**Content-Type:** `application/json` для структурированных артефактов, `application/pdf` для исходных файлов.

## 3. Transaction boundaries

**Атомарные операции (одна DB-транзакция):**
1. Создание версии + обновление `current_version_id` + audit record.
2. Создание N ArtifactDescriptor + обновление `artifact_status` + audit record.
3. Создание VersionDiffReference + audit record.

**Не атомарные (eventual consistency):**
1. Object Storage write + DB write: сначала blob, затем metadata. Orphan cleanup для компенсации.
2. DB commit + event publish: сначала commit, затем publish. При crash между ними — transactional outbox pattern (см. раздел 5).

## 4. Atomicity vs eventual consistency

| Операция | Гарантия | Обоснование |
|----------|----------|-------------|
| Metadata write (DB) | ACID | PostgreSQL транзакция |
| Blob write + metadata write | Eventual | Object Storage — внешняя система. Compensating: orphan cleanup |
| DB commit + event publish | Eventual | RabbitMQ — внешняя система. Compensating: transactional outbox |
| Idempotency update (Redis) | Best effort | При потере — повторная обработка безопасна (идемпотентность на уровне DB unique constraints) |

## 5. Idempotency design

### Уровни защиты

1. **Redis (fast path):** Idempotency Guard проверяет ключ до начала обработки. `PROCESSING` — short TTL (120s), `COMPLETED` — TTL 24 часа (BRE-003).
2. **DB (durable path):** Unique constraint `(version_id, artifact_type)` предотвращает дублирование артефактов. Unique constraint `(base_version_id, target_version_id)` — для diff.
3. **Object Storage:** Перезапись по тому же ключу — идемпотентна (S3 PutObject).

### Transactional Outbox

Для гарантии доставки events после DB commit:
1. В DB-транзакции вместе с основными данными записывается запись в таблицу `outbox_events`. Поле `aggregate_id` (= `version_id`) обеспечивает FIFO-порядок в рамках одной версии (REV-010).
2. Outbox Poller: `SELECT ... FOR UPDATE SKIP LOCKED LIMIT N` (BRE-006), предотвращает конкурентную обработку при нескольких инстансах.
3. После успешной публикации + publisher confirm: UPDATE status = `CONFIRMED` (BRE-013).
4. Cleanup: `DELETE ... WHERE status = 'CONFIRMED' AND published_at < now() - interval` — партициями или `LIMIT 1000` в цикле (BRE-018).

**Конфигурация:** `DM_OUTBOX_POLL_INTERVAL` (default 200ms), `DM_OUTBOX_BATCH_SIZE` (default 50), `DM_OUTBOX_LOCK_TIMEOUT` (default 5s).

```sql
CREATE TABLE outbox_events (
    event_id     UUID PRIMARY KEY,
    aggregate_id UUID,               -- version_id для FIFO ordering
    topic        TEXT NOT NULL,
    payload      JSONB NOT NULL,
    status       TEXT NOT NULL DEFAULT 'PENDING',  -- PENDING, CONFIRMED
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    published_at TIMESTAMPTZ
) PARTITION BY RANGE (created_at);
CREATE INDEX idx_outbox_pending ON outbox_events(status, created_at) WHERE status = 'PENDING';
CREATE INDEX idx_outbox_aggregate ON outbox_events(aggregate_id, created_at) WHERE status = 'PENDING';
```

## 6. Deduplication strategy

| Уровень | Механизм | Ключ |
|---------|----------|------|
| Event ingestion | Redis idempotency key | `{domain}-{type}:{job_id}` |
| Artifact storage | DB unique constraint | `(version_id, artifact_type)` |
| Diff storage | DB unique constraint | `(base_version_id, target_version_id)` |
| Version creation | DB unique constraint | `(document_id, version_number)` |
| Blob storage | S3 PutObject (overwrite) | storage_key path |

## 7. Retention / cleanup / archival

**Retention policies (v1 — глобальные, через переменные окружения DM):**

| Категория | Срок | Механизм | Переменная окружения |
|-----------|------|----------|---------------------|
| Активные документы | Без ограничений | — | — |
| Архивированные документы (blob) | 90 дней → cold storage | S3 lifecycle rule | `DM_RETENTION_ARCHIVE_DAYS` |
| Deleted документы (blob) | 30 дней после soft delete → физическое удаление | Background job | `DM_RETENTION_DELETED_BLOB_DAYS` |
| Deleted документы (метаданные) | 365 дней после soft delete → физическое удаление | Background job | `DM_RETENTION_DELETED_META_DAYS` |
| Audit records | 3 года | Партиционирование + drop partition | `DM_RETENTION_AUDIT_DAYS` |
| Idempotency records (Redis) | 24 часа | TTL | `DM_IDEMPOTENCY_TTL` |
| Outbox events | 48 часов после публикации | Background job | `DM_OUTBOX_CLEANUP_HOURS` |

> **Эволюция в следующих версиях:** глобальная retention policy может быть расширена до per-organization. Потребуется сущность `RetentionPolicy` с привязкой к `organization_id`, UI для администратора организации (R-3), валидация минимальных допустимых сроков на уровне платформы.

**Orphan cleanup job:**
- Периодичность: раз в час.
- Логика: список ключей в Object Storage с prefix `{org_id}/{doc_id}/{ver_id}/` → сравнение с ArtifactDescriptor в DB → удаление blob без descriptor, если `created_at` blob > 1 час (даёт время на in-flight транзакции).
