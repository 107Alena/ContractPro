-- 000005_audit_protection.up.sql
-- BRE-016: Audit append-only enforcement at database level.
-- REV-020: RLS for all tenant-scoped tables — already applied in 000003.
--
-- This migration adds:
--   1. Trigger function that blocks UPDATE (always) and DELETE (without override).
--   2. Row-level BEFORE UPDATE OR DELETE trigger on audit_records.
--   3. Statement-level BEFORE TRUNCATE trigger on audit_records.
--   4. Restricted DB role dm_audit_writer (INSERT + SELECT only).
--
-- The audit_records table is partitioned (000004_audit_partitions). In PG 13+,
-- row-level triggers on a partitioned parent automatically propagate to all
-- existing and future partitions.
--
-- Retention cleanup: the metadata retention job (metacleanup) needs to DELETE
-- audit records for documents past the retention period. It sets
--   SET LOCAL app.retention_override = 'true'
-- within its transaction before calling DELETE. The trigger checks this GUC
-- and allows the operation when the override is set.
--
-- Partition drops (AuditPartitionManager.DropPartitionsOlderThan) use DDL
-- (DROP TABLE), which bypasses row-level triggers entirely — no override needed.

BEGIN;

-- ---------------------------------------------------------------------------
-- 1. Trigger function: append-only enforcement
-- ---------------------------------------------------------------------------
CREATE OR REPLACE FUNCTION fn_audit_no_update_delete()
RETURNS TRIGGER AS $$
BEGIN
    IF TG_OP = 'UPDATE' THEN
        RAISE EXCEPTION 'audit_records is append-only: UPDATE operations are prohibited'
            USING ERRCODE = 'P0001';
    END IF;
    IF TG_OP = 'DELETE' THEN
        -- Allow DELETE only when retention override is explicitly set in the
        -- current transaction via SET LOCAL app.retention_override = 'true'.
        IF current_setting('app.retention_override', true) IS DISTINCT FROM 'true' THEN
            RAISE EXCEPTION 'audit_records is append-only: DELETE operations require retention override'
                USING ERRCODE = 'P0001';
        END IF;
        RETURN OLD;
    END IF;
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;

-- ---------------------------------------------------------------------------
-- 2. Row-level trigger on partitioned parent
-- ---------------------------------------------------------------------------
CREATE TRIGGER no_update_delete_audit
    BEFORE UPDATE OR DELETE ON audit_records
    FOR EACH ROW
    EXECUTE FUNCTION fn_audit_no_update_delete();

-- ---------------------------------------------------------------------------
-- 3. Statement-level TRUNCATE protection (defense-in-depth)
-- ---------------------------------------------------------------------------
CREATE OR REPLACE FUNCTION fn_audit_no_truncate()
RETURNS TRIGGER AS $$
BEGIN
    RAISE EXCEPTION 'audit_records is append-only: TRUNCATE operations are prohibited'
        USING ERRCODE = 'P0001';
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER no_truncate_audit
    BEFORE TRUNCATE ON audit_records
    EXECUTE FUNCTION fn_audit_no_truncate();

-- ---------------------------------------------------------------------------
-- 4. Restricted DB role for audit writes (BRE-016)
-- ---------------------------------------------------------------------------
-- NOLOGIN role, intended for use via SET ROLE by the application.
-- Only INSERT (write new records) and SELECT (read for API queries) are granted.
-- No UPDATE, DELETE, TRUNCATE, or TRIGGER privileges.
DO $$
BEGIN
    IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'dm_audit_writer') THEN
        CREATE ROLE dm_audit_writer NOLOGIN;
    END IF;
END
$$;

GRANT INSERT, SELECT ON audit_records TO dm_audit_writer;

-- GRANT on parent does not propagate to existing partitions in PostgreSQL.
-- Explicitly grant on the default partition created in 000004.
-- NOTE: The AuditPartitionManager (Go runtime) must also GRANT INSERT, SELECT
-- on each newly-created monthly partition to dm_audit_writer if this role is
-- used via SET ROLE for application connections.
GRANT INSERT, SELECT ON audit_records_default TO dm_audit_writer;

COMMIT;
