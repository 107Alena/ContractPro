package port

import (
	"context"
	"io"

	"contractpro/document-processing/internal/domain/model"
)

// IdempotencyStatus represents the state of a job in the idempotency store.
type IdempotencyStatus string

const (
	IdempotencyStatusNew        IdempotencyStatus = "new"
	IdempotencyStatusInProgress IdempotencyStatus = "in_progress"
	IdempotencyStatusCompleted  IdempotencyStatus = "completed"
)

// TempStoragePort provides access to temporary artifact storage (Yandex Object Storage).
// Implemented by: Temporary Artifact Storage Adapter (infra layer).
// Used by: Source File Fetcher, Processing/Comparison Orchestrators (cleanup).
type TempStoragePort interface {
	Upload(ctx context.Context, key string, data io.Reader) error
	Download(ctx context.Context, key string) (io.ReadCloser, error)
	Delete(ctx context.Context, key string) error
	DeleteByPrefix(ctx context.Context, prefix string) error
}

// SourceFileDownloaderPort provides HTTP file download capability.
// Implemented by: HTTP client (infra layer).
// Used by: Source File Fetcher (engine).
// Returns: response body, Content-Length (-1 if unknown), error.
type SourceFileDownloaderPort interface {
	Download(ctx context.Context, fileURL string) (io.ReadCloser, int64, error)
}

// OCRServicePort provides access to external OCR service (Yandex Cloud Vision).
// Implemented by: Yandex Vision OCR client (infra layer).
// Used by: OCR Integration Adapter (engine).
// Returns: recognized raw text, error.
type OCRServicePort interface {
	Recognize(ctx context.Context, pdfData io.Reader) (string, error)
}

// EventPublisherPort publishes status, completion, and failure events
// for external consumers (API/backend orchestrator, other domains).
// Implemented by: Event Publisher (egress layer).
// Used by: Job Lifecycle Manager, Processing/Comparison Orchestrators.
type EventPublisherPort interface {
	PublishStatusChanged(ctx context.Context, event model.StatusChangedEvent) error
	PublishProcessingCompleted(ctx context.Context, event model.ProcessingCompletedEvent) error
	PublishProcessingFailed(ctx context.Context, event model.ProcessingFailedEvent) error
	PublishComparisonCompleted(ctx context.Context, event model.ComparisonCompletedEvent) error
	PublishComparisonFailed(ctx context.Context, event model.ComparisonFailedEvent) error
}

// DMArtifactSenderPort sends processing artifacts and diff results to Document Management.
// Implemented by: DM Outbound Adapter (egress layer).
// Used by: Processing Orchestrator (artifacts), Comparison Orchestrator (diff result).
type DMArtifactSenderPort interface {
	SendArtifacts(ctx context.Context, event model.DocumentProcessingArtifactsReady) error
	SendDiffResult(ctx context.Context, event model.DocumentVersionDiffReady) error
}

// DMTreeRequesterPort requests semantic trees from Document Management
// for version comparison.
// Implemented by: DM Outbound Adapter (egress layer).
// Used by: Comparison Pipeline Orchestrator.
type DMTreeRequesterPort interface {
	RequestSemanticTree(ctx context.Context, req model.GetSemanticTreeRequest) error
}

// IdempotencyStorePort provides job deduplication via key-value store with TTL.
// Implemented by: KV-store client (infra layer).
// Used by: Idempotency Guard (ingress layer).
type IdempotencyStorePort interface {
	Check(ctx context.Context, jobID string) (IdempotencyStatus, error)
	Register(ctx context.Context, jobID string) error
	MarkCompleted(ctx context.Context, jobID string) error
}

// ConcurrencyLimiterPort limits the number of concurrent jobs per DP instance.
// Implemented by: semaphore (infra layer).
// Used by: Ingress layer (between Idempotency Guard and Orchestrator dispatch).
type ConcurrencyLimiterPort interface {
	Acquire(ctx context.Context) error
	Release()
}

// PendingResponse holds one correlated response from Document Management.
// Tree is non-nil on success; Err is non-nil on failure. Exactly one is set.
type PendingResponse struct {
	CorrelationID string
	Tree          *model.SemanticTree
	Err           error
}

// PendingResponseRegistryPort tracks and correlates asynchronous responses
// from Document Management during the comparison pipeline.
// Implemented by: Pending Response Registry (application layer).
// Used by: Comparison Pipeline Orchestrator (Register, AwaitAll, Cancel),
//
//	DM Inbound Adapter (Receive, ReceiveError).
type PendingResponseRegistryPort interface {
	Register(jobID string, correlationIDs []string) error
	AwaitAll(ctx context.Context, jobID string) ([]PendingResponse, error)
	Receive(correlationID string, tree model.SemanticTree) error
	ReceiveError(correlationID string, err error) error
	Cancel(jobID string)
}
