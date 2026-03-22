package publisher

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

// Compile-time interface compliance check.
var _ port.EventPublisherPort = (*Publisher)(nil)

// Publisher implements port.EventPublisherPort by serializing domain events
// to JSON and publishing them to the appropriate broker topics.
type Publisher struct {
	broker BrokerPublisher
	topics topicMap
}

// topicMap holds the mapping from event type to broker topic name.
type topicMap struct {
	statusChanged       string
	processingCompleted string
	processingFailed    string
	comparisonCompleted string
	comparisonFailed    string
}

// NewPublisher creates a Publisher with the given broker client and topic configuration.
// Panics if broker is nil or any topic is empty (programmer/config error at startup).
func NewPublisher(broker BrokerPublisher, cfg config.BrokerConfig) *Publisher {
	if broker == nil {
		panic("publisher: broker must not be nil")
	}

	topics := topicMap{
		statusChanged:       cfg.TopicStatusChanged,
		processingCompleted: cfg.TopicProcessingCompleted,
		processingFailed:    cfg.TopicProcessingFailed,
		comparisonCompleted: cfg.TopicComparisonCompleted,
		comparisonFailed:    cfg.TopicComparisonFailed,
	}

	for name, val := range map[string]string{
		"TopicStatusChanged":       topics.statusChanged,
		"TopicProcessingCompleted": topics.processingCompleted,
		"TopicProcessingFailed":    topics.processingFailed,
		"TopicComparisonCompleted": topics.comparisonCompleted,
		"TopicComparisonFailed":    topics.comparisonFailed,
	} {
		if val == "" {
			panic(fmt.Sprintf("publisher: %s topic must not be empty", name))
		}
	}

	return &Publisher{
		broker: broker,
		topics: topics,
	}
}

// publishJSON marshals event to JSON and publishes it to the given topic.
// JSON serialization failures produce a non-retryable DomainError (deterministic
// programming error). Context errors and broker errors pass through unchanged.
func (p *Publisher) publishJSON(ctx context.Context, topic string, event any) error {
	data, err := json.Marshal(event)
	if err != nil {
		return &port.DomainError{
			Code:      port.ErrCodeBrokerFailed,
			Message:   fmt.Sprintf("publisher: marshal %T", event),
			Retryable: false,
			Cause:     err,
		}
	}
	return p.broker.Publish(ctx, topic, data)
}

// PublishStatusChanged publishes a job status transition event.
func (p *Publisher) PublishStatusChanged(ctx context.Context, event model.StatusChangedEvent) error {
	return p.publishJSON(ctx, p.topics.statusChanged, event)
}

// PublishProcessingCompleted publishes a processing success event.
func (p *Publisher) PublishProcessingCompleted(ctx context.Context, event model.ProcessingCompletedEvent) error {
	return p.publishJSON(ctx, p.topics.processingCompleted, event)
}

// PublishProcessingFailed publishes a processing failure event.
func (p *Publisher) PublishProcessingFailed(ctx context.Context, event model.ProcessingFailedEvent) error {
	return p.publishJSON(ctx, p.topics.processingFailed, event)
}

// PublishComparisonCompleted publishes a comparison success event.
func (p *Publisher) PublishComparisonCompleted(ctx context.Context, event model.ComparisonCompletedEvent) error {
	return p.publishJSON(ctx, p.topics.comparisonCompleted, event)
}

// PublishComparisonFailed publishes a comparison failure event.
func (p *Publisher) PublishComparisonFailed(ctx context.Context, event model.ComparisonFailedEvent) error {
	return p.publishJSON(ctx, p.topics.comparisonFailed, event)
}
