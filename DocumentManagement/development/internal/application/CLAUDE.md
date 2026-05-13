# Application Layer — CLAUDE.md

Application services implementing inbound port handlers. Coordinates domain logic, infrastructure adapters, and cross-cutting concerns.

## Services

**ingestion/** — ArtifactIngestionService (implements ArtifactIngestionHandler)
- HandleDPArtifacts: 5 DP artifacts → PROCESSING_ARTIFACTS_RECEIVED
- HandleLICArtifacts: 8 baseline LIC artifacts (+ optional RISK_DELTA from LIC v1.1 for versions with non-empty parent_version_id) → ANALYSIS_ARTIFACTS_RECEIVED
- HandleREArtifacts: claim-check pattern → FULLY_READY
- Flow: validate → tenant check → content validation (BRE-029) → save blobs → DB tx (descriptors + status transition + audit + outbox) → compensation on failure
- Orphan candidate registration before compensation (BRE-008)

**query/** — ArtifactQueryService (implements ArtifactQueryHandler)
- HandleGetSemanticTree: async, publishes SemanticTreeProvided (with error fields if not found)
- HandleGetArtifacts: async, publishes ArtifactsProvided (with missing_types)
- GetArtifact: sync API, returns content or presigned URL for blob types
- ListArtifacts: sync API, returns descriptor list
- Content hash verification on read (BRE-027): SHA-256 check against ArtifactDescriptor.ContentHash

**lifecycle/** — DocumentLifecycleService (implements DocumentLifecycleHandler)
- CreateDocument, GetDocument, ListDocuments, ArchiveDocument, DeleteDocument
- State machine: ACTIVE → ARCHIVED, ACTIVE/ARCHIVED → DELETED
- All mutations transactional with audit records

**version/** — VersionManagementService (implements VersionManagementHandler)
- CreateVersion with optimistic locking (retry up to 3x on version_number conflict)
- SELECT FOR UPDATE on document row for serialization (BRE-005)
- RE_CHECK: copies source_file_key from parent version
- Outbox: publishes VersionCreated notification

**diff/** — DiffStorageService (implements DiffStorageHandler)
- HandleDiffReady: validates both versions exist, same document, saves blob, DB tx
- Idempotency (REV-028): pre-check via FindByVersionPair, skip blob upload if exists
- GetDiff: returns reference + blob content

**tenant/** — VerifyTenantOwnership utility
- Validates document belongs to claimed organization_id (BRE-015)
- Empty orgID → skip (backward compatibility for REV-001/REV-002)
- Mismatch → TENANT_MISMATCH error + metric + WARN log

## Background Jobs

**watchdog/** — StaleVersionWatchdog (REV-008/BRE-010/DM-TASK-053)
- Ticker-based (`DM_WATCHDOG_SCAN_INTERVAL`, default 5 min), finds versions stuck in intermediate status beyond **per-stage** timeouts (`DM_STALE_TIMEOUT_PROCESSING=5m`, `ANALYSIS=10m`, `REPORTS=5m`, `FINALIZATION=5m`). `DM_STALE_VERSION_TIMEOUT` is retained as a per-variable legacy fallback.
- Single SQL query with disjunction over `(artifact_status, created_at)` pairs, one per-stage cutoff
- Transitions to PARTIALLY_AVAILABLE with FOR UPDATE lock + audit (with `stage` field) + outbox notification
- Per-stage metrics: `dm_stuck_versions_count{stage}` (gauge, reset every scan) and `dm_stuck_versions_total{stage}` (counter)
- Cross-tenant system query

**orphancleanup/** — OrphanCleanupJob (BRE-008)
- Ticker-based (1 hour default), reads orphan_candidates table
- Per-candidate: check artifact_descriptors exists → false positive (remove) or confirmed orphan (delete S3 blob + remove)
- 1-hour grace period for TOCTOU safety

**retention/** — Three background jobs:
- DeletedBlobCleanupJob: deletes S3 blobs for DELETED docs older than DM_RETENTION_DELETED_BLOB_DAYS (30d)
- DeletedMetaCleanupJob: hard-deletes DB metadata for DELETED docs older than DM_RETENTION_DELETED_META_DAYS (365d). Cascade: clear current_version_id → artifacts → diffs → audit → versions → document
- AuditPartitionJob (REV-027): creates future monthly partitions, drops old ones older than DM_RETENTION_AUDIT_DAYS (1095d)

## Patterns

- All services use constructor DI with nil-check panics
- Transactional operations via port.Transactor.WithTransaction()
- Fallback resolvers for missing version_id/organization_id (REV-001/REV-002)
- Tenant isolation checks on all async event handlers
- Background jobs: Start/Stop/Done lifecycle with split stop+done channels
- Audit logging for all data mutations (append-only)
