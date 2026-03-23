package consumer

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"contractpro/document-processing/internal/config"
	"contractpro/document-processing/internal/domain/model"
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

// CommandDispatcher is the consumer-side interface for dispatching validated
// commands through the ingress pipeline (idempotency + concurrency + handler).
//
// Satisfied by: *dispatcher.Dispatcher
type CommandDispatcher interface {
	DispatchProcessDocument(ctx context.Context, cmd model.ProcessDocumentCommand) error
	DispatchCompareVersions(ctx context.Context, cmd model.CompareVersionsCommand) error
}

// Consumer subscribes to inbound command topics and dispatches deserialized
// commands to the appropriate application-layer handler.
type Consumer struct {
	broker     BrokerSubscriber
	dispatcher CommandDispatcher
	logger     *observability.Logger

	topicProcessDocument string
	topicCompareVersions string

	startOnce sync.Once
	startErr  error
}

// NewConsumer creates a Consumer wired to the given broker, dispatcher, and
// topic names from the broker configuration.
// Panics if any required dependency is nil (programmer error at startup).
func NewConsumer(
	broker BrokerSubscriber,
	dispatcher CommandDispatcher,
	logger *observability.Logger,
	brokerCfg config.BrokerConfig,
) *Consumer {
	if broker == nil {
		panic("consumer: broker must not be nil")
	}
	if dispatcher == nil {
		panic("consumer: dispatcher must not be nil")
	}
	if logger == nil {
		panic("consumer: logger must not be nil")
	}
	if strings.TrimSpace(brokerCfg.TopicProcessDocument) == "" {
		panic("consumer: TopicProcessDocument must not be empty")
	}
	if strings.TrimSpace(brokerCfg.TopicCompareVersions) == "" {
		panic("consumer: TopicCompareVersions must not be empty")
	}
	return &Consumer{
		broker:               broker,
		dispatcher:           dispatcher,
		logger:               logger.With("component", "consumer"),
		topicProcessDocument: brokerCfg.TopicProcessDocument,
		topicCompareVersions: brokerCfg.TopicCompareVersions,
	}
}

// Start subscribes to the process-document and compare-versions topics.
// It is idempotent: repeated calls return the result of the first attempt.
// After a failed Start(), the Consumer is unusable; callers must create a new
// Consumer. On partial failure (first subscription succeeds, second fails),
// the caller must shut down the broker to clean up the active subscription.
func (c *Consumer) Start() error {
	c.startOnce.Do(func() {
		if err := c.broker.Subscribe(c.topicProcessDocument, c.handleProcessDocument); err != nil {
			c.startErr = fmt.Errorf("consumer: subscribe to %s: %w", c.topicProcessDocument, err)
			return
		}
		if err := c.broker.Subscribe(c.topicCompareVersions, c.handleCompareVersions); err != nil {
			c.startErr = fmt.Errorf("consumer: subscribe to %s: %w", c.topicCompareVersions, err)
			return
		}
	})
	return c.startErr
}

// handleProcessDocument deserializes, validates, and dispatches a
// ProcessDocumentCommand to the processing handler.
//
// Always returns nil to prevent poison-pill requeue loops:
// - Malformed or invalid messages are logged and acknowledged.
// - Handler errors are logged and acknowledged (the handler manages failure
//   semantics internally via lifecycle status transitions and events).
func (c *Consumer) handleProcessDocument(ctx context.Context, body []byte) error {
	var cmd model.ProcessDocumentCommand
	if err := json.Unmarshal(body, &cmd); err != nil {
		c.logger.Error(ctx, "failed to unmarshal process document command",
			"error", err,
			"raw_size", len(body),
			"raw_preview", rawPreview(body),
		)
		return nil
	}

	if err := validateProcessDocumentCommand(cmd); err != nil {
		c.logger.Error(ctx, "invalid process document command",
			"error", err,
			"job_id", cmd.JobID,
			"document_id", cmd.DocumentID,
		)
		return nil
	}

	ctx = observability.WithJobContext(ctx, observability.JobContext{
		JobID:      cmd.JobID,
		DocumentID: cmd.DocumentID,
	})

	c.logger.Info(ctx, "received process document command")

	if err := c.dispatcher.DispatchProcessDocument(ctx, cmd); err != nil {
		c.logger.Warn(ctx, "dispatch process document returned error", "error", err)
		return nil
	}

	return nil
}

// handleCompareVersions deserializes, validates, and dispatches a
// CompareVersionsCommand to the comparison handler.
//
// Always returns nil to prevent poison-pill requeue loops.
func (c *Consumer) handleCompareVersions(ctx context.Context, body []byte) error {
	var cmd model.CompareVersionsCommand
	if err := json.Unmarshal(body, &cmd); err != nil {
		c.logger.Error(ctx, "failed to unmarshal compare versions command",
			"error", err,
			"raw_size", len(body),
			"raw_preview", rawPreview(body),
		)
		return nil
	}

	if err := validateCompareVersionsCommand(cmd); err != nil {
		c.logger.Error(ctx, "invalid compare versions command",
			"error", err,
			"job_id", cmd.JobID,
			"document_id", cmd.DocumentID,
		)
		return nil
	}

	ctx = observability.WithJobContext(ctx, observability.JobContext{
		JobID:      cmd.JobID,
		DocumentID: cmd.DocumentID,
	})

	c.logger.Info(ctx, "received compare versions command")

	if err := c.dispatcher.DispatchCompareVersions(ctx, cmd); err != nil {
		c.logger.Warn(ctx, "dispatch compare versions returned error", "error", err)
		return nil
	}

	return nil
}

// rawPreview returns a truncated string preview of a raw message body for
// logging. This aids debugging of malformed messages without risking massive
// log entries.
func rawPreview(body []byte) string {
	const maxPreview = 200
	if len(body) <= maxPreview {
		return string(body)
	}
	return string(body[:maxPreview]) + "..."
}
