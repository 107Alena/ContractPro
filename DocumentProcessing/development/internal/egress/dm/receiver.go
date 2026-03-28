package dm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"contractpro/document-processing/internal/config"
	"contractpro/document-processing/internal/domain/model"
	"contractpro/document-processing/internal/domain/port"
	"contractpro/document-processing/internal/infra/observability"
)

// BrokerSubscriber is the consumer-side interface for subscribing to broker
// topics. Defined here (consumer-side) to keep the dependency inverted and
// enable unit testing with a mock.
//
// Satisfied by: *broker.Client
type BrokerSubscriber interface {
	Subscribe(topic string, handler func(ctx context.Context, body []byte) error) error
}

// Receiver subscribes to DM response topics and dispatches deserialized
// events to the appropriate application-layer handlers.
//
// For artifacts persisted/failed events, the Receiver delegates to a
// DMResponseHandler (which routes to the DMConfirmationAwaiterPort for
// the processing pipeline).
//
// For SemanticTreeProvided, DiffPersisted, and DiffPersistFailed events,
// the Receiver calls the PendingResponseRegistry directly, using
// correlation-based async dispatch (for the comparison pipeline).
type Receiver struct {
	broker   BrokerSubscriber
	handler  port.DMResponseHandler
	registry port.PendingResponseRegistryPort
	logger   *observability.Logger

	topics dmTopicMap

	startOnce sync.Once
	startErr  error
}

// dmTopicMap holds the 5 DM→DP response topic names.
type dmTopicMap struct {
	artifactsPersisted     string
	artifactsPersistFailed string
	semanticTreeProvided   string
	diffPersisted          string
	diffPersistFailed      string
}

// NewReceiver creates a Receiver wired to the given broker, handlers, and
// topic names from the broker configuration.
// Panics if any required dependency is nil or any topic is empty
// (programmer/config error at startup).
func NewReceiver(
	broker BrokerSubscriber,
	handler port.DMResponseHandler,
	registry port.PendingResponseRegistryPort,
	logger *observability.Logger,
	cfg config.BrokerConfig,
) *Receiver {
	if broker == nil {
		panic("dm receiver: broker must not be nil")
	}
	if handler == nil {
		panic("dm receiver: handler must not be nil")
	}
	if registry == nil {
		panic("dm receiver: registry must not be nil")
	}
	if logger == nil {
		panic("dm receiver: logger must not be nil")
	}

	topics := dmTopicMap{
		artifactsPersisted:     cfg.TopicDMArtifactsPersisted,
		artifactsPersistFailed: cfg.TopicDMArtifactsPersistFailed,
		semanticTreeProvided:   cfg.TopicDMSemanticTreeProvided,
		diffPersisted:          cfg.TopicDMDiffPersisted,
		diffPersistFailed:      cfg.TopicDMDiffPersistFailed,
	}

	for _, entry := range []struct{ name, val string }{
		{"TopicDMArtifactsPersisted", topics.artifactsPersisted},
		{"TopicDMArtifactsPersistFailed", topics.artifactsPersistFailed},
		{"TopicDMSemanticTreeProvided", topics.semanticTreeProvided},
		{"TopicDMDiffPersisted", topics.diffPersisted},
		{"TopicDMDiffPersistFailed", topics.diffPersistFailed},
	} {
		if strings.TrimSpace(entry.val) == "" {
			panic(fmt.Sprintf("dm receiver: %s topic must not be empty", entry.name))
		}
	}

	return &Receiver{
		broker:   broker,
		handler:  handler,
		registry: registry,
		logger:   logger.With("component", "dm-receiver"),
		topics:   topics,
	}
}

// Start subscribes to the 5 DM response topics.
// It is idempotent: repeated calls return the result of the first attempt.
// On partial failure, the caller must shut down the broker to clean up
// active subscriptions.
func (r *Receiver) Start() error {
	r.startOnce.Do(func() {
		subs := []struct {
			topic   string
			handler func(ctx context.Context, body []byte) error
		}{
			{r.topics.artifactsPersisted, r.handleArtifactsPersisted},
			{r.topics.artifactsPersistFailed, r.handleArtifactsPersistFailed},
			{r.topics.semanticTreeProvided, r.handleSemanticTreeProvided},
			{r.topics.diffPersisted, r.handleDiffPersisted},
			{r.topics.diffPersistFailed, r.handleDiffPersistFailed},
		}
		for _, s := range subs {
			if err := r.broker.Subscribe(s.topic, s.handler); err != nil {
				r.startErr = fmt.Errorf("dm receiver: subscribe to %s: %w", s.topic, err)
				return
			}
		}
	})
	return r.startErr
}

// handleArtifactsPersisted deserializes and dispatches a
// DocumentProcessingArtifactsPersisted event.
//
// Always returns nil to prevent poison-pill requeue loops.
func (r *Receiver) handleArtifactsPersisted(ctx context.Context, body []byte) error {
	var event model.DocumentProcessingArtifactsPersisted
	if err := json.Unmarshal(body, &event); err != nil {
		r.logger.Error(ctx, "failed to unmarshal artifacts persisted event",
			"error", err,
			"raw_size", len(body),
			"raw_preview", rawPreview(body),
		)
		return nil
	}

	if err := validateArtifactsPersisted(event); err != nil {
		r.logger.Error(ctx, "invalid artifacts persisted event",
			"error", err,
			"job_id", event.JobID,
			"document_id", event.DocumentID,
		)
		return nil
	}

	ctx = observability.WithJobContext(ctx, observability.JobContext{
		JobID:         event.JobID,
		DocumentID:    event.DocumentID,
		CorrelationID: event.CorrelationID,
	})

	r.logger.Info(ctx, "received artifacts persisted event")

	if err := r.handler.HandleArtifactsPersisted(ctx, event); err != nil {
		r.logger.Warn(ctx, "handle artifacts persisted returned error", "error", err)
	}
	return nil
}

// handleArtifactsPersistFailed deserializes and dispatches a
// DocumentProcessingArtifactsPersistFailed event.
//
// Always returns nil to prevent poison-pill requeue loops.
func (r *Receiver) handleArtifactsPersistFailed(ctx context.Context, body []byte) error {
	var event model.DocumentProcessingArtifactsPersistFailed
	if err := json.Unmarshal(body, &event); err != nil {
		r.logger.Error(ctx, "failed to unmarshal artifacts persist failed event",
			"error", err,
			"raw_size", len(body),
			"raw_preview", rawPreview(body),
		)
		return nil
	}

	if err := validateArtifactsPersistFailed(event); err != nil {
		r.logger.Error(ctx, "invalid artifacts persist failed event",
			"error", err,
			"job_id", event.JobID,
			"document_id", event.DocumentID,
		)
		return nil
	}

	ctx = observability.WithJobContext(ctx, observability.JobContext{
		JobID:         event.JobID,
		DocumentID:    event.DocumentID,
		CorrelationID: event.CorrelationID,
	})

	r.logger.Warn(ctx, "received artifacts persist failed event",
		"error_code", event.ErrorCode,
		"error_message", event.ErrorMessage,
		"is_retryable", event.IsRetryable,
	)

	if err := r.handler.HandleArtifactsPersistFailed(ctx, event); err != nil {
		r.logger.Warn(ctx, "handle artifacts persist failed returned error", "error", err)
	}
	return nil
}

// handleSemanticTreeProvided deserializes a SemanticTreeProvided event and
// dispatches it to the PendingResponseRegistry via Receive or ReceiveError.
//
// This handler bypasses DMResponseHandler because semantic tree responses
// use correlation-based async dispatch through the registry, not
// orchestrator-level handling.
//
// Always returns nil to prevent poison-pill requeue loops.
func (r *Receiver) handleSemanticTreeProvided(ctx context.Context, body []byte) error {
	var event model.SemanticTreeProvided
	if err := json.Unmarshal(body, &event); err != nil {
		r.logger.Error(ctx, "failed to unmarshal semantic tree provided event",
			"error", err,
			"raw_size", len(body),
			"raw_preview", rawPreview(body),
		)
		return nil
	}

	if err := validateSemanticTreeProvided(event); err != nil {
		r.logger.Error(ctx, "invalid semantic tree provided event",
			"error", err,
			"job_id", event.JobID,
			"document_id", event.DocumentID,
		)
		return nil
	}

	ctx = observability.WithJobContext(ctx, observability.JobContext{
		JobID:         event.JobID,
		DocumentID:    event.DocumentID,
		CorrelationID: event.CorrelationID,
	})

	r.logger.Info(ctx, "received semantic tree provided event",
		"version_id", event.VersionID,
	)

	if event.SemanticTree.Root == nil {
		dmErr := fmt.Errorf("dm: empty semantic tree for version %s", event.VersionID)
		if err := r.registry.ReceiveError(event.CorrelationID, dmErr); err != nil {
			r.logger.Warn(ctx, "registry receive error returned error",
				"error", err,
				"version_id", event.VersionID,
			)
		}
		return nil
	}

	if err := r.registry.Receive(event.CorrelationID, event.SemanticTree); err != nil {
		r.logger.Warn(ctx, "registry receive returned error",
			"error", err,
			"version_id", event.VersionID,
		)
	}
	return nil
}

// handleDiffPersisted deserializes a DocumentVersionDiffPersisted event and
// dispatches it to the PendingResponseRegistry via Receive to unblock the
// comparison orchestrator's WAITING_DM_CONFIRMATION stage.
//
// This handler bypasses DMResponseHandler because diff confirmations use
// correlation-based async dispatch through the registry, matching the
// pattern used by handleSemanticTreeProvided.
//
// Always returns nil to prevent poison-pill requeue loops.
func (r *Receiver) handleDiffPersisted(ctx context.Context, body []byte) error {
	var event model.DocumentVersionDiffPersisted
	if err := json.Unmarshal(body, &event); err != nil {
		r.logger.Error(ctx, "failed to unmarshal diff persisted event",
			"error", err,
			"raw_size", len(body),
			"raw_preview", rawPreview(body),
		)
		return nil
	}

	if err := validateDiffPersisted(event); err != nil {
		r.logger.Error(ctx, "invalid diff persisted event",
			"error", err,
			"job_id", event.JobID,
			"document_id", event.DocumentID,
		)
		return nil
	}

	ctx = observability.WithJobContext(ctx, observability.JobContext{
		JobID:         event.JobID,
		DocumentID:    event.DocumentID,
		CorrelationID: event.CorrelationID,
	})

	r.logger.Info(ctx, "received diff persisted event")

	if err := r.registry.Receive(event.CorrelationID, model.SemanticTree{}); err != nil {
		r.logger.Warn(ctx, "registry receive returned error",
			"error", err,
		)
	}
	return nil
}

// handleDiffPersistFailed deserializes a DocumentVersionDiffPersistFailed
// event and dispatches it to the PendingResponseRegistry via ReceiveError
// to propagate the failure to the comparison orchestrator's
// WAITING_DM_CONFIRMATION stage.
//
// The error is constructed as a typed DomainError with the is_retryable flag
// from the DM event, so the orchestrator's classifyError can determine the
// correct terminal status and retry behavior.
//
// This handler bypasses DMResponseHandler because diff confirmations use
// correlation-based async dispatch through the registry.
//
// Always returns nil to prevent poison-pill requeue loops.
func (r *Receiver) handleDiffPersistFailed(ctx context.Context, body []byte) error {
	var event model.DocumentVersionDiffPersistFailed
	if err := json.Unmarshal(body, &event); err != nil {
		r.logger.Error(ctx, "failed to unmarshal diff persist failed event",
			"error", err,
			"raw_size", len(body),
			"raw_preview", rawPreview(body),
		)
		return nil
	}

	if err := validateDiffPersistFailed(event); err != nil {
		r.logger.Error(ctx, "invalid diff persist failed event",
			"error", err,
			"job_id", event.JobID,
			"document_id", event.DocumentID,
		)
		return nil
	}

	ctx = observability.WithJobContext(ctx, observability.JobContext{
		JobID:         event.JobID,
		DocumentID:    event.DocumentID,
		CorrelationID: event.CorrelationID,
	})

	r.logger.Warn(ctx, "received diff persist failed event",
		"error_code", event.ErrorCode,
		"error_message", event.ErrorMessage,
		"is_retryable", event.IsRetryable,
	)

	dmErr := port.NewDMDiffPersistFailedError(event.ErrorMessage, event.IsRetryable, nil)
	if err := r.registry.ReceiveError(event.CorrelationID, dmErr); err != nil {
		r.logger.Warn(ctx, "registry receive error returned error",
			"error", err,
		)
	}
	return nil
}

// rawPreview returns a truncated string preview of a raw message body for
// logging. Limits output to prevent massive log entries from malformed messages.
func rawPreview(body []byte) string {
	const maxPreview = 200
	if len(body) <= maxPreview {
		return string(body)
	}
	return string(body[:maxPreview]) + "..."
}
