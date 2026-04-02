package dlq

import (
	"context"
	"encoding/json"
	"fmt"

	"contractpro/document-management/internal/domain/model"
	"contractpro/document-management/internal/domain/port"
)

// BrokerPublisher is the consumer-side interface for publishing messages.
type BrokerPublisher interface {
	Publish(ctx context.Context, topic string, payload []byte) error
}

// DLQMetrics is the consumer-side interface for DLQ metrics.
type DLQMetrics interface {
	IncDLQMessages(reason string)
}

// Logger is the consumer-side logging interface.
type Logger interface {
	Warn(msg string, args ...any)
	Error(msg string, args ...any)
}

// Compile-time interface check.
var _ port.DLQPort = (*Sender)(nil)

// Sender implements port.DLQPort by serializing DLQRecord to JSON,
// persisting to the DLQ repository (for replay), and publishing
// to the correct DLQ topic based on DLQCategory.
type Sender struct {
	broker  BrokerPublisher
	repo    port.DLQRepository
	metrics DLQMetrics
	logger  Logger
	topics  topicMap
}

type topicMap struct {
	ingestionFailed string
	queryFailed     string
	invalidMessage  string
}

// NewSender creates a DLQ Sender.
// Panics if any required dependency is nil or any DLQ topic is empty.
func NewSender(
	broker BrokerPublisher,
	repo port.DLQRepository,
	metrics DLQMetrics,
	logger Logger,
	ingestionTopic, queryTopic, invalidTopic string,
) *Sender {
	if broker == nil {
		panic("dlq: broker must not be nil")
	}
	if repo == nil {
		panic("dlq: repository must not be nil")
	}
	if metrics == nil {
		panic("dlq: metrics must not be nil")
	}
	if logger == nil {
		panic("dlq: logger must not be nil")
	}
	for name, val := range map[string]string{
		"ingestionTopic": ingestionTopic,
		"queryTopic":     queryTopic,
		"invalidTopic":   invalidTopic,
	} {
		if val == "" {
			panic(fmt.Sprintf("dlq: %s must not be empty", name))
		}
	}
	return &Sender{
		broker:  broker,
		repo:    repo,
		metrics: metrics,
		logger:  logger,
		topics: topicMap{
			ingestionFailed: ingestionTopic,
			queryFailed:     queryTopic,
			invalidMessage:  invalidTopic,
		},
	}
}

// SendToDLQ persists the record to the DB (replay source of truth), then
// publishes to the appropriate DLQ topic (alerting/monitoring).
// Neither failure is fatal — the message is already ACKed.
func (s *Sender) SendToDLQ(ctx context.Context, record model.DLQRecord) error {
	// Persist to DB first (replay source of truth).
	if err := s.repo.Insert(ctx, &record); err != nil {
		s.logger.Error("failed to persist DLQ record to DB",
			"job_id", record.JobID, "error", err)
		// Continue to publish to broker anyway.
	}

	topic := s.resolveTopic(record.Category)

	data, err := json.Marshal(record)
	if err != nil {
		return &port.DomainError{
			Code:      port.ErrCodeBrokerFailed,
			Message:   fmt.Sprintf("dlq: marshal DLQRecord for job %s", record.JobID),
			Retryable: false,
			Cause:     err,
		}
	}

	s.metrics.IncDLQMessages(string(record.Category))

	if pubErr := s.broker.Publish(ctx, topic, data); pubErr != nil {
		s.logger.Error("failed to publish DLQ record to broker",
			"job_id", record.JobID, "topic", topic, "error", pubErr)
		// Already persisted to DB — not fatal for the caller.
	}

	return nil
}

func (s *Sender) resolveTopic(category model.DLQCategory) string {
	switch category {
	case model.DLQCategoryQuery:
		return s.topics.queryFailed
	case model.DLQCategoryInvalid:
		return s.topics.invalidMessage
	default:
		return s.topics.ingestionFailed
	}
}
