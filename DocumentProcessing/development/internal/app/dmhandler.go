package app

import (
	"context"

	"contractpro/document-processing/internal/domain/model"
	"contractpro/document-processing/internal/domain/port"
	"contractpro/document-processing/internal/infra/observability"
)

// Compile-time interface compliance check.
var _ port.DMResponseHandler = (*dmResponseHandler)(nil)

// dmResponseHandler is a composite that dispatches DM response events to the
// appropriate application-layer handler. It satisfies port.DMResponseHandler.
//
// Artifacts persisted/failed events are logged (actual delegation to the
// processing orchestrator will be wired once it exposes DM response methods).
//
// Diff persisted/failed events are handled similarly for the comparison path.
//
// SemanticTreeProvided is dispatched directly by the DM Receiver to the
// PendingResponseRegistry, so the method here is a no-op safety net.
type dmResponseHandler struct {
	logger *observability.Logger
}

func newDMResponseHandler(logger *observability.Logger) *dmResponseHandler {
	return &dmResponseHandler{logger: logger}
}

func (h *dmResponseHandler) HandleArtifactsPersisted(ctx context.Context, event model.DocumentProcessingArtifactsPersisted) error {
	h.logger.Info(ctx, "artifacts persisted by DM",
		"job_id", event.JobID, "document_id", event.DocumentID)
	return nil
}

func (h *dmResponseHandler) HandleArtifactsPersistFailed(ctx context.Context, event model.DocumentProcessingArtifactsPersistFailed) error {
	h.logger.Warn(ctx, "artifacts persist failed by DM",
		"job_id", event.JobID, "document_id", event.DocumentID, "error", event.ErrorMessage)
	return nil
}

func (h *dmResponseHandler) HandleSemanticTreeProvided(ctx context.Context, event model.SemanticTreeProvided) error {
	h.logger.Info(ctx, "semantic tree provided (unexpected in composite handler)",
		"correlation_id", event.EventMeta.CorrelationID)
	return nil
}

func (h *dmResponseHandler) HandleDiffPersisted(ctx context.Context, event model.DocumentVersionDiffPersisted) error {
	h.logger.Info(ctx, "diff persisted by DM",
		"job_id", event.JobID, "document_id", event.DocumentID)
	return nil
}

func (h *dmResponseHandler) HandleDiffPersistFailed(ctx context.Context, event model.DocumentVersionDiffPersistFailed) error {
	h.logger.Warn(ctx, "diff persist failed by DM",
		"job_id", event.JobID, "document_id", event.DocumentID, "error", event.ErrorMessage)
	return nil
}
