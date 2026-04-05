# Domain Layer — CLAUDE.md

Pure domain logic with no external dependencies. Implements hexagonal architecture.

## model/ — Domain Entities, Events & Value Objects

Core business domain (11 source files):

**Entities & State Machines:**
- **document.go** — Document entity (ID, org, title, status, timestamps). DocumentStatus enum: ACTIVE → ARCHIVED → DELETED. ValidateDocumentTransition(), TransitionTo()
- **version.go** — DocumentVersion entity (immutable version snapshot). ArtifactStatus state machine: PENDING → PROCESSING_ARTIFACTS_RECEIVED → ANALYSIS_ARTIFACTS_RECEIVED → REPORTS_READY → FULLY_READY (not strictly linear: ANALYSIS_ARTIFACTS_RECEIVED can also transition directly to FULLY_READY). PARTIALLY_AVAILABLE for stale versions (set by watchdog). OriginType enum (5 values). ValidateArtifactTransition()
- **artifact.go** — ArtifactDescriptor (metadata for stored artifact). ArtifactType enum (15 types across DP/LIC/RE). ProducerDomain enum. IsBlobArtifact() for EXPORT_PDF/EXPORT_DOCX. ArtifactTypesByProducer map
- **diff.go** — VersionDiffReference (metadata for version comparison result)
- **audit.go** — AuditRecord (append-only). AuditAction enum (9 actions). ActorType enum (USER/SYSTEM/DOMAIN). Builder chain: WithDocument/WithVersion/WithJob/WithDetails
- **idempotency.go** — IdempotencyRecord (PROCESSING/COMPLETED). IsStuck(threshold) for detecting stuck keys
- **dlq.go** — DLQRecord, DLQRecordWithMeta, DLQCategory enum (ingestion/query/invalid)

**Events:**
- **event.go** — EventMeta (correlation_id, timestamp), BlobReference (claim-check pattern for binary files)
- **event_incoming.go** — 6 incoming event structs: DocumentProcessingArtifactsReady, GetSemanticTreeRequest, DocumentVersionDiffReady, GetArtifactsRequest, LegalAnalysisArtifactsReady, ReportsArtifactsReady
- **event_outgoing.go** — 15 outgoing event structs: 10 confirmations (persisted/persist-failed per domain), 5 notifications (version-artifacts-ready, version-analysis-ready, version-reports-ready, version-created, version-partially-available)
- **topic.go** — 25 topic constants: 7 incoming + 10 confirmation + 5 notification + 3 DLQ

## port/ — Hexagonal Port Interfaces

Boundaries between domain and external layers:
- **inbound.go** — 5 handler interfaces: DocumentLifecycleHandler, VersionManagementHandler, ArtifactIngestionHandler, ArtifactQueryHandler, DiffStorageHandler. Helper types: PageResult[T], CreateDocumentParams, CreateVersionParams, ArtifactContent, GetArtifactParams, GetDiffParams
- **outbound.go** — 18 outbound interfaces: Transactor, 6 repositories (Document, Version, Artifact, Diff, Audit, Outbox), ObjectStoragePort, BrokerPublisherPort, ConfirmationPublisherPort (10 methods), NotificationPublisherPort (5 methods), IdempotencyStorePort, AuditPort, DLQPort, DLQRepository, OrphanCandidateRepository, AuditPartitionManager, DocumentFallbackResolver. Helper types: OutboxEntry, AuditListParams, DLQFilterParams, OrphanCandidate
- **errors.go** — DomainError (Code, Message, Retryable, Cause). 20 error codes, 20 constructors (NewValidationError, NewDocumentNotFoundError, NewStorageError, NewTenantMismatchError, etc.). Helpers: IsDomainError, IsRetryable, IsNotFound, IsConflict, IsDuplicateEvent

## Patterns

- All entities use `New*` constructors
- Compile-time interface checks: `var _ Port = (*Impl)(nil)` in adapters
- State machines with validated transitions (Document, ArtifactStatus)
- Domain errors carry machine-readable error codes and retryable flags
- json.RawMessage for artifact content (DM stores, does not interpret)
- Claim-check pattern (BlobReference) for binary artifacts from RE
