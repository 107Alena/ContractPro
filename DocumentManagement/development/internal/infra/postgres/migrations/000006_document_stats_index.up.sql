-- 000006_document_stats_index.up.sql
-- DM-TASK-059: indexes backing the count-by-artifact_status aggregate
-- (GET /documents/stats). The aggregate counts documents grouped by the
-- artifact_status of their CURRENT version (documents.current_version_id ->
-- document_versions), scoped by organization. Documents with no current
-- version are counted as "not_started".

BEGIN;

-- Required by the DM-TASK-059 contract. Backs org-scoped scans of versions by
-- artifact_status (e.g. direct status rollups across all versions of an org).
-- NOTE: the /documents/stats query joins versions by PK (current_version_id),
-- so for that specific query this index is forward-looking — the documents
-- composite index below is what actually drives the plan.
CREATE INDEX idx_versions_org_artifact_status
    ON document_versions (organization_id, artifact_status);

-- Drives the /documents/stats query: filter documents by (organization_id,
-- status) and read current_version_id straight from the index leaf (covering
-- INCLUDE) to probe document_versions by primary key, avoiding a heap fetch on
-- the driving scan.
CREATE INDEX idx_documents_org_status
    ON documents (organization_id, status)
    INCLUDE (current_version_id);

COMMIT;
