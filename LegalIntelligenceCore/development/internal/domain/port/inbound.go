package port

import "context"

// Inbound ports — one Handler per subscribed topic (integration-contracts.md
// §1, §6.1). The Event Router (LIC-TASK-040) holds one handler per topic and
// dispatches deserialised events to it after idempotency and tenant checks.
//
// Each Handle returns an error per Go idiom; the consumer adapter maps
// errors to NACK / DLQ decisions based on whether the error wraps a
// *model.DomainError with Retryable=true (NACK to retry-DLX) or not (DLQ).

// VersionArtifactsReadyHandler is invoked for every dm.events.version-
// artifacts-ready message — the main pipeline trigger
// (high-architecture.md §6.2, §6.5).
type VersionArtifactsReadyHandler interface {
	HandleVersionArtifactsReady(ctx context.Context, evt VersionProcessingArtifactsReady) error
}

// VersionCreatedHandler is invoked for dm.events.version-created and writes
// the per-version metadata cache (Redis lic-version-meta:{version_id}, TTL
// 24h) consumed by the orchestrator to decide whether Agent 9 — Risk Delta
// runs (high-architecture.md §6.2, ASSUMPTION-LIC-02).
type VersionCreatedHandler interface {
	HandleVersionCreated(ctx context.Context, evt VersionCreated) error
}

// ArtifactsProvidedHandler is invoked for dm.responses.artifacts-provided.
// The DM Artifact Awaiter routes the message to the correct in-flight
// pipeline goroutine by correlation_id (high-architecture.md §6.12).
type ArtifactsProvidedHandler interface {
	HandleArtifactsProvided(ctx context.Context, evt ArtifactsProvided) error
}

// PersistConfirmationHandler is invoked for the two DM persist-confirmation
// topics (dm.responses.lic-artifacts-persisted and ...persist-failed). A
// single handler with both methods keeps the DM Confirmation Awaiter
// register/routing logic colocated (high-architecture.md §6.12).
type PersistConfirmationHandler interface {
	HandlePersisted(ctx context.Context, evt LegalAnalysisArtifactsPersisted) error
	HandlePersistFailed(ctx context.Context, evt LegalAnalysisArtifactsPersistFailed) error
}

// UserConfirmedTypeHandler is invoked for orch.commands.user-confirmed-type.
// The Pending Type Confirmation Manager resumes the paused pipeline
// (high-architecture.md §6.10) — see that document for the strict ordering
// of operations and the mandatory validation of ContractType against the
// 12-value whitelist + ^[A-Z_]{1,32}$ regex.
type UserConfirmedTypeHandler interface {
	HandleUserConfirmedType(ctx context.Context, evt UserConfirmedType) error
}
