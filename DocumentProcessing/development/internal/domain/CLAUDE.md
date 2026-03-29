# Domain Layer — CLAUDE.md

Pure domain logic with no external dependencies. Implements hexagonal architecture.

## model/ — Domain Entities & Value Objects

Core business domain:
- **job.go** — Job entity (state machine: Pending → Processing → Completed/Failed)
- **document.go** — Document value object (metadata: ID, version, title, etc.)
- **command.go** — ProcessDocumentCommand, CompareVersionsCommand (inbound requests)
- **event.go** — Domain events (StatusChanged, ProcessingCompleted, ProcessingFailed, ComparisonCompleted, ComparisonFailed, ArtifactsReady, DiffReady, DM response events)
- **artifacts.go** — Processing artifacts (OCRRawArtifact, ExtractedText)
- **semantic_tree.go** — SemanticTree, SemanticNode (document structure as tree)
- **structure.go** — DocumentStructure, Section, Clause, SubClause, Appendix, PartyDetails
- **diff.go** — VersionDiffResult, TextDiff, StructuralDiff
- **status.go** — JobStatus enum with valid state transitions
- **stage.go** — ProcessingStage enum (Validating, FetchingSourceFile, ExtractingText, etc.)
- **warning.go** — ProcessingWarning (non-fatal issues during pipeline)
- **dlq.go** — DLQMessage (dead letter queue entries for failed messages)

## port/ — Hexagonal Port Interfaces

Boundaries between domain and external layers:
- **inbound.go** — ProcessingCommandHandler, ComparisonCommandHandler, DMResponseHandler
- **engine.go** — InputValidatorPort, SourceFileFetcherPort, TextExtractionPort, StructureExtractionPort, SemanticTreeBuilderPort, OCRProcessorPort, VersionComparisonPort
- **outbound.go** — TempStoragePort, OCRServicePort, EventPublisherPort, DMArtifactSenderPort, DMTreeRequesterPort, IdempotencyStorePort, ConcurrencyLimiterPort, DMConfirmationAwaiterPort, PendingResponseRegistryPort, DLQPort
- **errors.go** — DomainError (typed errors with codes, retryable flag, errors.Is/As support)

## Patterns

- All entities use `New*` constructors
- Compile-time interface checks: `var _ Port = (*Impl)(nil)`
- Immutable value objects, mutable entities
- Domain errors carry machine-readable error codes
