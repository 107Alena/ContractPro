-- 000003_rls_policies.down.sql
-- Rolls back RLS policies added in 000003.

BEGIN;

DROP POLICY IF EXISTS tenant_isolation_documents ON documents;
ALTER TABLE documents NO FORCE ROW LEVEL SECURITY;
ALTER TABLE documents DISABLE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS tenant_isolation_document_versions ON document_versions;
ALTER TABLE document_versions NO FORCE ROW LEVEL SECURITY;
ALTER TABLE document_versions DISABLE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS tenant_isolation_artifact_descriptors ON artifact_descriptors;
ALTER TABLE artifact_descriptors NO FORCE ROW LEVEL SECURITY;
ALTER TABLE artifact_descriptors DISABLE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS tenant_isolation_version_diff_references ON version_diff_references;
ALTER TABLE version_diff_references NO FORCE ROW LEVEL SECURITY;
ALTER TABLE version_diff_references DISABLE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS tenant_isolation_audit_records ON audit_records;
ALTER TABLE audit_records NO FORCE ROW LEVEL SECURITY;
ALTER TABLE audit_records DISABLE ROW LEVEL SECURITY;

COMMIT;
