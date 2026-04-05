-- 000005_audit_protection.down.sql
-- Rolls back BRE-016 audit append-only enforcement.
--
-- NOTE: The dm_audit_writer role is NOT dropped because it may own objects
-- or have privileges granted outside this migration's scope. Role removal
-- is a manual DBA operation if needed.

BEGIN;

-- Remove triggers.
DROP TRIGGER IF EXISTS no_update_delete_audit ON audit_records;
DROP TRIGGER IF EXISTS no_truncate_audit ON audit_records;

-- Remove trigger functions.
DROP FUNCTION IF EXISTS fn_audit_no_update_delete();
DROP FUNCTION IF EXISTS fn_audit_no_truncate();

-- Revoke privileges from role (role itself is retained).
DO $$
BEGIN
    IF EXISTS (SELECT FROM pg_roles WHERE rolname = 'dm_audit_writer') THEN
        REVOKE ALL ON audit_records FROM dm_audit_writer;
    END IF;
END
$$;

COMMIT;
