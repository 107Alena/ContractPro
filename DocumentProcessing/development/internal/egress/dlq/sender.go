package dlq

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
var _ port.DLQPort = (*Sender)(nil)

// Sender implements port.DLQPort by serializing DLQ messages to JSON
// and publishing them to the configured DLQ broker topic.
type Sender struct {
	broker   BrokerPublisher
	topicDLQ string
}

// NewSender creates a Sender with the given broker client and topic configuration.
// Panics if broker is nil or the DLQ topic is empty (programmer/config error at startup).
func NewSender(broker BrokerPublisher, cfg config.BrokerConfig) *Sender {
	if broker == nil {
		panic("dlq: broker must not be nil")
	}
	if cfg.TopicDLQ == "" {
		panic("dlq: TopicDLQ topic must not be empty")
	}
	return &Sender{
		broker:   broker,
		topicDLQ: cfg.TopicDLQ,
	}
}

// SendToDLQ serializes the DLQ message to JSON and publishes it to the DLQ topic.
// JSON serialization failures produce a non-retryable DomainError.
// Context errors and broker errors pass through unchanged.
func (s *Sender) SendToDLQ(ctx context.Context, msg model.DLQMessage) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return &port.DomainError{
			Code:      port.ErrCodeBrokerFailed,
			Message:   fmt.Sprintf("dlq: marshal DLQMessage for job %s", msg.JobID),
			Retryable: false,
			Cause:     err,
		}
	}
	return s.broker.Publish(ctx, s.topicDLQ, data)
}
