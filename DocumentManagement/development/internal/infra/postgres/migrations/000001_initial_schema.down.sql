-- 000001_initial_schema.down.sql
-- Rolls back the initial schema. Drop order respects foreign key dependencies.

BEGIN;

DROP TABLE IF EXISTS orphan_candidates;
DROP TABLE IF EXISTS outbox_events;
DROP TABLE IF EXISTS audit_records;
DROP TABLE IF EXISTS version_diff_references;
DROP TABLE IF EXISTS artifact_descriptors;

-- Drop the circular FK before dropping tables.
ALTER TABLE IF EXISTS documents DROP CONSTRAINT IF EXISTS documents_current_version_fk;

DROP TABLE IF EXISTS document_versions;
DROP TABLE IF EXISTS documents;

COMMIT;
