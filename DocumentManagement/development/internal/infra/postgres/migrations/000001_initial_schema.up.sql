-- 000001_initial_schema.up.sql
-- Document Management — initial database schema.
-- Tables: documents, document_versions, artifact_descriptors,
--         version_diff_references, audit_records, outbox_events,
--         orphan_candidates.
-- Based on: DocumentManagement/architecture/storage.md

BEGIN;

-- ---------------------------------------------------------------------------
-- documents
-- ---------------------------------------------------------------------------
CREATE TABLE documents (
    document_id        UUID        PRIMARY KEY,
    organization_id    UUID        NOT NULL,
    title              TEXT        NOT NULL,
    current_version_id UUID,
    status             TEXT        NOT NULL DEFAULT 'ACTIVE',
    created_by_user_id UUID        NOT NULL,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at         TIMESTAMPTZ
);

CREATE INDEX idx_documents_org
    ON documents (organization_id);

CREATE INDEX idx_documents_deleted
    ON documents (deleted_at)
    WHERE deleted_at IS NOT NULL;

-- ---------------------------------------------------------------------------
-- document_versions
-- ---------------------------------------------------------------------------
CREATE TABLE document_versions (
    version_id          UUID        PRIMARY KEY,
    document_id         UUID        NOT NULL REFERENCES documents (document_id),
    version_number      INT         NOT NULL,
    parent_version_id   UUID        REFERENCES document_versions (version_id),
    origin_type         TEXT        NOT NULL,
    origin_description  TEXT,
    source_file_key     TEXT        NOT NULL,
    source_file_name    TEXT        NOT NULL,
    source_file_size    BIGINT      NOT NULL,
    source_file_checksum TEXT       NOT NULL,
    organization_id     UUID        NOT NULL,
    artifact_status     TEXT        NOT NULL DEFAULT 'PENDING',
    created_by_user_id  UUID        NOT NULL,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),

    UNIQUE (document_id, version_number)
);

CREATE INDEX idx_versions_doc
    ON document_versions (document_id);

CREATE INDEX idx_versions_org
    ON document_versions (organization_id);

-- Deferred FK: documents.current_version_id → document_versions.version_id.
-- Created after document_versions to resolve the circular dependency.
ALTER TABLE documents
    ADD CONSTRAINT documents_current_version_fk
    FOREIGN KEY (current_version_id) REFERENCES document_versions (version_id);

-- ---------------------------------------------------------------------------
-- artifact_descriptors
-- ---------------------------------------------------------------------------
CREATE TABLE artifact_descriptors (
    artifact_id     UUID        PRIMARY KEY,
    version_id      UUID        NOT NULL REFERENCES document_versions (version_id),
    document_id     UUID        NOT NULL,
    organization_id UUID        NOT NULL,
    artifact_type   TEXT        NOT NULL,
    producer_domain TEXT        NOT NULL,
    storage_key     TEXT        NOT NULL,
    size_bytes      BIGINT      NOT NULL,
    content_hash    TEXT        NOT NULL,
    schema_version  TEXT        NOT NULL DEFAULT '1.0',
    job_id          TEXT        NOT NULL,
    correlation_id  TEXT        NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),

    UNIQUE (version_id, artifact_type)
);

CREATE INDEX idx_artifacts_version
    ON artifact_descriptors (version_id);

CREATE INDEX idx_artifacts_org
    ON artifact_descriptors (organization_id);

CREATE INDEX idx_artifacts_storage_key
    ON artifact_descriptors (storage_key);

-- ---------------------------------------------------------------------------
-- version_diff_references
-- ---------------------------------------------------------------------------
CREATE TABLE version_diff_references (
    diff_id               UUID        PRIMARY KEY,
    document_id           UUID        NOT NULL REFERENCES documents (document_id),
    organization_id       UUID        NOT NULL,
    base_version_id       UUID        NOT NULL REFERENCES document_versions (version_id),
    target_version_id     UUID        NOT NULL REFERENCES document_versions (version_id),
    storage_key           TEXT        NOT NULL,
    text_diff_count       INT         NOT NULL DEFAULT 0,
    structural_diff_count INT         NOT NULL DEFAULT 0,
    job_id                TEXT        NOT NULL,
    correlation_id        TEXT        NOT NULL,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT now(),

    UNIQUE (base_version_id, target_version_id)
);

-- ---------------------------------------------------------------------------
-- audit_records
-- ---------------------------------------------------------------------------
CREATE TABLE audit_records (
    audit_id        UUID        PRIMARY KEY,
    organization_id UUID        NOT NULL,
    document_id     UUID,
    version_id      UUID,
    action          TEXT        NOT NULL,
    actor_type      TEXT        NOT NULL,
    actor_id        TEXT        NOT NULL,
    job_id          TEXT,
    correlation_id  TEXT,
    details         JSONB,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_audit_org_time
    ON audit_records (organization_id, created_at);

CREATE INDEX idx_audit_doc
    ON audit_records (document_id);

CREATE INDEX idx_audit_version
    ON audit_records (version_id)
    WHERE version_id IS NOT NULL;

-- ---------------------------------------------------------------------------
-- outbox_events (transactional outbox pattern)
-- ---------------------------------------------------------------------------
CREATE TABLE outbox_events (
    event_id     UUID        PRIMARY KEY,
    aggregate_id UUID,
    topic        TEXT        NOT NULL,
    payload      JSONB       NOT NULL,
    status       TEXT        NOT NULL DEFAULT 'PENDING',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    published_at TIMESTAMPTZ
);

CREATE INDEX idx_outbox_pending
    ON outbox_events (status, created_at)
    WHERE status = 'PENDING';

CREATE INDEX idx_outbox_aggregate
    ON outbox_events (aggregate_id, created_at)
    WHERE status = 'PENDING';

-- ---------------------------------------------------------------------------
-- orphan_candidates (BRE-008: table-based instead of full S3 scan)
-- ---------------------------------------------------------------------------
CREATE TABLE orphan_candidates (
    storage_key TEXT        PRIMARY KEY,
    version_id  UUID,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

COMMIT;
