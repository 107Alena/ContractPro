-- 000003_rls_policies.up.sql
-- Row-Level Security for tenant isolation (defense-in-depth).
--
-- Strategy: each tenant-scoped table gets RLS enabled with a permissive
-- policy checking organization_id = current_setting('app.organization_id').
-- The application sets this GUC per-transaction via SET LOCAL.
--
-- When app.organization_id is empty (not set / default), all rows are visible.
-- This supports:
--   1. Normal tenant-scoped operation: SET LOCAL → strict filter.
--   2. Fallback resolver (REV-001/REV-002): no SET LOCAL → all rows visible.
--   3. Migrations / admin queries: no SET LOCAL → all rows visible.
--   4. Outbox poller / DLQ: system-level, no tenant context.
--
-- Tables covered: documents, document_versions, artifact_descriptors,
--                 version_diff_references, audit_records.
--
-- Excluded (no organization_id column, intentionally cross-tenant):
--   outbox_events, dm_dlq_records, orphan_candidates.

BEGIN;

-- ---------------------------------------------------------------------------
-- documents
-- ---------------------------------------------------------------------------
ALTER TABLE documents ENABLE ROW LEVEL SECURITY;
ALTER TABLE documents FORCE ROW LEVEL SECURITY;

CREATE POLICY tenant_isolation_documents ON documents
    USING (
        current_setting('app.organization_id', true) = ''
        OR organization_id = current_setting('app.organization_id', true)::uuid
    );

-- ---------------------------------------------------------------------------
-- document_versions
-- ---------------------------------------------------------------------------
ALTER TABLE document_versions ENABLE ROW LEVEL SECURITY;
ALTER TABLE document_versions FORCE ROW LEVEL SECURITY;

CREATE POLICY tenant_isolation_document_versions ON document_versions
    USING (
        current_setting('app.organization_id', true) = ''
        OR organization_id = current_setting('app.organization_id', true)::uuid
    );

-- ---------------------------------------------------------------------------
-- artifact_descriptors
-- ---------------------------------------------------------------------------
ALTER TABLE artifact_descriptors ENABLE ROW LEVEL SECURITY;
ALTER TABLE artifact_descriptors FORCE ROW LEVEL SECURITY;

CREATE POLICY tenant_isolation_artifact_descriptors ON artifact_descriptors
    USING (
        current_setting('app.organization_id', true) = ''
        OR organization_id = current_setting('app.organization_id', true)::uuid
    );

-- ---------------------------------------------------------------------------
-- version_diff_references
-- ---------------------------------------------------------------------------
ALTER TABLE version_diff_references ENABLE ROW LEVEL SECURITY;
ALTER TABLE version_diff_references FORCE ROW LEVEL SECURITY;

CREATE POLICY tenant_isolation_version_diff_references ON version_diff_references
    USING (
        current_setting('app.organization_id', true) = ''
        OR organization_id = current_setting('app.organization_id', true)::uuid
    );

-- ---------------------------------------------------------------------------
-- audit_records
-- ---------------------------------------------------------------------------
ALTER TABLE audit_records ENABLE ROW LEVEL SECURITY;
ALTER TABLE audit_records FORCE ROW LEVEL SECURITY;

CREATE POLICY tenant_isolation_audit_records ON audit_records
    USING (
        current_setting('app.organization_id', true) = ''
        OR organization_id = current_setting('app.organization_id', true)::uuid
    );

COMMIT;
