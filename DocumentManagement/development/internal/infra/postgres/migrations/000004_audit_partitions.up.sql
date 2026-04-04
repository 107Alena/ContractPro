-- 000004_audit_partitions.up.sql
-- Convert audit_records from regular table to PARTITION BY RANGE (created_at)
-- for monthly partitions. Enables efficient partition drop for retention
-- policy (DM_RETENTION_AUDIT_DAYS). (REV-027)
--
-- The AuditPartitionJob background process creates future monthly partitions
-- and drops expired ones at runtime.

BEGIN;

-- 1. Drop indexes that will be recreated on the partitioned table.
DROP INDEX IF EXISTS idx_audit_org_time;
DROP INDEX IF EXISTS idx_audit_doc;
DROP INDEX IF EXISTS idx_audit_version;

-- 2. Drop RLS policies on audit_records (from 000003_rls_policies).
DROP POLICY IF EXISTS tenant_isolation_audit ON audit_records;

-- 3. Rename existing table to preserve data.
ALTER TABLE audit_records RENAME TO audit_records_old;

-- 4. Create partitioned table with identical schema.
--    PRIMARY KEY must include the partition key for partitioned tables.
CREATE TABLE audit_records (
    audit_id        UUID        NOT NULL,
    organization_id UUID        NOT NULL,
    document_id     UUID,
    version_id      UUID,
    action          TEXT        NOT NULL,
    actor_type      TEXT        NOT NULL,
    actor_id        TEXT        NOT NULL,
    job_id          TEXT,
    correlation_id  TEXT,
    details         JSONB,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (audit_id, created_at)
) PARTITION BY RANGE (created_at);

-- 5. Create default partition (catches rows outside named partitions).
CREATE TABLE audit_records_default PARTITION OF audit_records DEFAULT;

-- 6. Migrate data from old table.
INSERT INTO audit_records SELECT * FROM audit_records_old;

-- 7. Recreate indexes on the partitioned parent (propagated to partitions).
CREATE INDEX idx_audit_org_time
    ON audit_records (organization_id, created_at);

CREATE INDEX idx_audit_doc
    ON audit_records (document_id);

CREATE INDEX idx_audit_version
    ON audit_records (version_id)
    WHERE version_id IS NOT NULL;

-- 8. Re-enable RLS on the new partitioned table.
ALTER TABLE audit_records ENABLE ROW LEVEL SECURITY;
ALTER TABLE audit_records FORCE ROW LEVEL SECURITY;

CREATE POLICY tenant_isolation_audit ON audit_records
    USING (
        current_setting('app.organization_id', true) = ''
        OR current_setting('app.organization_id', true) IS NULL
        OR organization_id = current_setting('app.organization_id', true)::uuid
    );

-- 9. Drop old table.
DROP TABLE audit_records_old;

COMMIT;
