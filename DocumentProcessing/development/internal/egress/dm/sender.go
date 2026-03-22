package dm

import (
	"context"
	"encoding/json"
	"fmt"

	"contractpro/document-processing/internal/config"
	"contractpro/document-processing/internal/domain/model"
	"contractpro/document-processing/internal/domain/port"
)

// BrokerPublisher is a consumer-side interface covering the subset of
// broker.Client methods used by this adapter.
// Implementations must be safe for concurrent use by multiple goroutines.
type BrokerPublisher interface {
	Publish(ctx context.Context, topic string, payload []byte) error
}

// Compile-time interface compliance checks.
var _ port.DMArtifactSenderPort = (*Sender)(nil)
var _ port.DMTreeRequesterPort = (*Sender)(nil)

// Sender implements port.DMArtifactSenderPort and port.DMTreeRequesterPort
// by serializing domain events/requests to JSON and publishing them to the
// appropriate broker topics for Document Management consumption.
type Sender struct {
	broker BrokerPublisher
	topics topicMap
}

// topicMap holds the mapping from message type to broker topic name.
type topicMap struct {
	artifactsReady  string
	semanticTreeReq string
	diffReady       string
}

// NewSender creates a Sender with the given broker client and topic configuration.
// Panics if broker is nil or any topic is empty (programmer/config error at startup).
func NewSender(broker BrokerPublisher, cfg config.BrokerConfig) *Sender {
	if broker == nil {
		panic("dm: broker must not be nil")
	}

	topics := topicMap{
		artifactsReady:  cfg.TopicArtifactsReady,
		semanticTreeReq: cfg.TopicSemanticTreeReq,
		diffReady:       cfg.TopicDiffReady,
	}

	for _, entry := range []struct{ name, val string }{
		{"TopicArtifactsReady", topics.artifactsReady},
		{"TopicSemanticTreeReq", topics.semanticTreeReq},
		{"TopicDiffReady", topics.diffReady},
	} {
		if entry.val == "" {
			panic(fmt.Sprintf("dm: %s topic must not be empty", entry.name))
		}
	}

	return &Sender{
		broker: broker,
		topics: topics,
	}
}

// publishJSON marshals event to JSON and publishes it to the given topic.
// JSON serialization failures produce a non-retryable DomainError (deterministic
// programming error). Context errors and broker errors pass through unchanged.
func (s *Sender) publishJSON(ctx context.Context, topic string, event any) error {
	data, err := json.Marshal(event)
	if err != nil {
		return &port.DomainError{
			Code:      port.ErrCodeBrokerFailed,
			Message:   fmt.Sprintf("dm: marshal %T", event),
			Retryable: false,
			Cause:     err,
		}
	}
	return s.broker.Publish(ctx, topic, data)
}

// SendArtifacts publishes processing artifacts to Document Management.
func (s *Sender) SendArtifacts(ctx context.Context, event model.DocumentProcessingArtifactsReady) error {
	return s.publishJSON(ctx, s.topics.artifactsReady, event)
}

// SendDiffResult publishes a version diff result to Document Management.
func (s *Sender) SendDiffResult(ctx context.Context, event model.DocumentVersionDiffReady) error {
	return s.publishJSON(ctx, s.topics.diffReady, event)
}

// RequestSemanticTree publishes a semantic tree request to Document Management.
func (s *Sender) RequestSemanticTree(ctx context.Context, req model.GetSemanticTreeRequest) error {
	return s.publishJSON(ctx, s.topics.semanticTreeReq, req)
}
