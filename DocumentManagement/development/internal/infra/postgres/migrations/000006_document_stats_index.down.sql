-- 000006_document_stats_index.down.sql
-- Reverts the DM-TASK-059 aggregate indexes.

BEGIN;

DROP INDEX IF EXISTS idx_documents_org_status;
DROP INDEX IF EXISTS idx_versions_org_artifact_status;

COMMIT;
