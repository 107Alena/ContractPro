package port

import (
	"context"

	"contractpro/document-processing/internal/domain/model"
)

// ProcessingCommandHandler is the inbound port for document processing commands.
// Implemented by: Processing Pipeline Orchestrator.
// Called by: Command Consumer (ingress layer).
type ProcessingCommandHandler interface {
	HandleProcessDocument(ctx context.Context, cmd model.ProcessDocumentCommand) error
}

// ComparisonCommandHandler is the inbound port for document version comparison commands.
// Implemented by: Comparison Pipeline Orchestrator.
// Called by: Command Consumer (ingress layer).
type ComparisonCommandHandler interface {
	HandleCompareVersions(ctx context.Context, cmd model.CompareVersionsCommand) error
}

// DMResponseHandler is the inbound port for handling responses from Document Management.
// Implemented by: composite handler that dispatches to the appropriate orchestrator/registry.
// Called by: DM Inbound Adapter (egress layer).
type DMResponseHandler interface {
	HandleArtifactsPersisted(ctx context.Context, event model.DocumentProcessingArtifactsPersisted) error
	HandleArtifactsPersistFailed(ctx context.Context, event model.DocumentProcessingArtifactsPersistFailed) error
	HandleSemanticTreeProvided(ctx context.Context, event model.SemanticTreeProvided) error
	HandleDiffPersisted(ctx context.Context, event model.DocumentVersionDiffPersisted) error
	HandleDiffPersistFailed(ctx context.Context, event model.DocumentVersionDiffPersistFailed) error
}
