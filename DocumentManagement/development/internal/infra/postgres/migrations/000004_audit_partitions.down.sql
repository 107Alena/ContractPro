-- 000004_audit_partitions.down.sql
-- Revert audit_records from partitioned table back to regular table.

BEGIN;

-- 1. Drop RLS policies.
DROP POLICY IF EXISTS tenant_isolation_audit ON audit_records;

-- 2. Drop indexes.
DROP INDEX IF EXISTS idx_audit_org_time;
DROP INDEX IF EXISTS idx_audit_doc;
DROP INDEX IF EXISTS idx_audit_version;

-- 3. Rename partitioned table.
ALTER TABLE audit_records RENAME TO audit_records_partitioned;

-- 4. Create regular table with original schema.
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

-- 5. Migrate data back.
INSERT INTO audit_records SELECT * FROM audit_records_partitioned;

-- 6. Recreate indexes.
CREATE INDEX idx_audit_org_time
    ON audit_records (organization_id, created_at);

CREATE INDEX idx_audit_doc
    ON audit_records (document_id);

CREATE INDEX idx_audit_version
    ON audit_records (version_id)
    WHERE version_id IS NOT NULL;

-- 7. Re-enable RLS (matching 000003_rls_policies).
ALTER TABLE audit_records ENABLE ROW LEVEL SECURITY;
ALTER TABLE audit_records FORCE ROW LEVEL SECURITY;

CREATE POLICY tenant_isolation_audit ON audit_records
    USING (
        current_setting('app.organization_id', true) = ''
        OR current_setting('app.organization_id', true) IS NULL
        OR organization_id = current_setting('app.organization_id', true)::uuid
    );

-- 8. Drop partitioned table (and all child partitions).
DROP TABLE audit_records_partitioned CASCADE;

COMMIT;
