package consumer

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"unicode/utf8"

	"contractpro/api-orchestrator/internal/config"
	"contractpro/api-orchestrator/internal/infra/observability/logger"
)

// Compile-time interface check.
var _ RetryTracker = (*inMemoryRetryTracker)(nil)

// maxAttempts is the total number of times the handler is invoked for a given
// message before giving up and ACKing with an ERROR log. The broker client
// always NACKs with requeue=true on handler error, so we track attempt count
// ourselves using a per-message counter keyed by message identity.
const maxAttempts = 3

// BrokerSubscriber is the consumer-side interface for subscribing to broker
// topics. Defined here (consumer-side) to keep the dependency inverted and
// enable unit testing with a mock.
//
// Satisfied by: *broker.Client
type BrokerSubscriber interface {
	Subscribe(topic string, handler func(ctx context.Context, body []byte) error) error
}

// EventHandler is the downstream consumer interface for processing deserialized
// events. The Processing Status Tracker (not yet implemented) will satisfy this
// interface.
//
// HandleEvent receives the event type, the deserialized event struct, and a
// context enriched with RequestContext fields extracted from the event envelope.
// If HandleEvent returns an error, the message is NACKed for requeue (up to
// maxRetries).
type EventHandler interface {
	HandleEvent(ctx context.Context, eventType EventType, event any) error
}

// RetryTracker tracks per-message delivery attempts so that messages exceeding
// maxRetries can be ACKed with an ERROR log instead of being requeued forever.
//
// The default implementation uses an in-memory map keyed on a message identity
// string. This is acceptable because:
//   - The orchestrator is a single-instance service (or uses sticky routing).
//   - Requeued messages return to the same consumer within a short window.
//   - Memory consumption is bounded: entries are removed on ACK or expiry via
//     the broker's message TTL.
type RetryTracker interface {
	// Increment records a delivery attempt and returns the current count.
	Increment(key string) int
	// Remove deletes the retry counter for the given key.
	Remove(key string)
}

// inMemoryRetryTracker is the default RetryTracker using a sync.Map.
type inMemoryRetryTracker struct {
	mu       sync.Mutex
	counters map[string]int
}

func newInMemoryRetryTracker() *inMemoryRetryTracker {
	return &inMemoryRetryTracker{counters: make(map[string]int)}
}

func (t *inMemoryRetryTracker) Increment(key string) int {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.counters[key]++
	return t.counters[key]
}

func (t *inMemoryRetryTracker) Remove(key string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.counters, key)
}

// topicBinding maps a topic name to its event type and deserialization factory.
// The topic name is stored directly in the binding so that buildBindings is the
// single source of truth (no separate lookup function needed).
type topicBinding struct {
	topic     string
	eventType EventType
	newEvent  func() any
}

// Consumer subscribes to inbound event topics from DP, LIC, RE, and DM, and
// dispatches deserialized events to the EventHandler.
type Consumer struct {
	broker   BrokerSubscriber
	handler  EventHandler
	retries  RetryTracker
	log      *logger.Logger
	bindings []topicBinding // populated by buildBindings

	startOnce sync.Once
	startErr  error
}

// NewConsumer creates a Consumer wired to the given broker, event handler, and
// topic names from the broker configuration.
// Panics if any required dependency is nil (programmer error at startup).
func NewConsumer(
	broker BrokerSubscriber,
	handler EventHandler,
	log *logger.Logger,
	brokerCfg config.BrokerConfig,
) *Consumer {
	if broker == nil {
		panic("consumer: broker must not be nil")
	}
	if handler == nil {
		panic("consumer: handler must not be nil")
	}
	if log == nil {
		panic("consumer: log must not be nil")
	}

	c := &Consumer{
		broker:  broker,
		handler: handler,
		retries: newInMemoryRetryTracker(),
		log:     log.With("component", "event-consumer"),
	}
	c.bindings = c.buildBindings(brokerCfg)
	return c
}

// newConsumerWithRetryTracker is a test constructor that allows injecting a
// custom RetryTracker.
func newConsumerWithRetryTracker(
	broker BrokerSubscriber,
	handler EventHandler,
	retries RetryTracker,
	log *logger.Logger,
	brokerCfg config.BrokerConfig,
) *Consumer {
	if broker == nil {
		panic("consumer: broker must not be nil")
	}
	if handler == nil {
		panic("consumer: handler must not be nil")
	}
	if log == nil {
		panic("consumer: log must not be nil")
	}

	c := &Consumer{
		broker:  broker,
		handler: handler,
		retries: retries,
		log:     log.With("component", "event-consumer"),
	}
	c.bindings = c.buildBindings(brokerCfg)
	return c
}

// buildBindings creates the mapping from topic names (from config) to event
// types and factory functions. This is the single source of truth for
// topic-to-event-type mapping.
func (c *Consumer) buildBindings(cfg config.BrokerConfig) []topicBinding {
	return []topicBinding{
		// DP events (5).
		{topic: cfg.TopicDPStatusChanged, eventType: EventDPStatusChanged, newEvent: func() any { return new(DPStatusChangedEvent) }},
		{topic: cfg.TopicDPProcessingCompleted, eventType: EventDPProcessingCompleted, newEvent: func() any { return new(DPProcessingCompletedEvent) }},
		{topic: cfg.TopicDPProcessingFailed, eventType: EventDPProcessingFailed, newEvent: func() any { return new(DPProcessingFailedEvent) }},
		{topic: cfg.TopicDPComparisonCompleted, eventType: EventDPComparisonCompleted, newEvent: func() any { return new(DPComparisonCompletedEvent) }},
		{topic: cfg.TopicDPComparisonFailed, eventType: EventDPComparisonFailed, newEvent: func() any { return new(DPComparisonFailedEvent) }},
		// LIC / RE events (2).
		{topic: cfg.TopicLICStatusChanged, eventType: EventLICStatusChanged, newEvent: func() any { return new(LICStatusChangedEvent) }},
		{topic: cfg.TopicREStatusChanged, eventType: EventREStatusChanged, newEvent: func() any { return new(REStatusChangedEvent) }},
		// DM events (5).
		{topic: cfg.TopicDMVersionArtifactsReady, eventType: EventDMVersionArtifactsReady, newEvent: func() any { return new(DMVersionArtifactsReadyEvent) }},
		{topic: cfg.TopicDMVersionAnalysisReady, eventType: EventDMVersionAnalysisReady, newEvent: func() any { return new(DMVersionAnalysisReadyEvent) }},
		{topic: cfg.TopicDMVersionReportsReady, eventType: EventDMVersionReportsReady, newEvent: func() any { return new(DMVersionReportsReadyEvent) }},
		{topic: cfg.TopicDMVersionPartiallyAvail, eventType: EventDMVersionPartiallyAvail, newEvent: func() any { return new(DMVersionPartiallyAvailableEvent) }},
		{topic: cfg.TopicDMVersionCreated, eventType: EventDMVersionCreated, newEvent: func() any { return new(DMVersionCreatedEvent) }},
	}
}

// Start subscribes to all 12 event topics. It is idempotent: repeated calls
// return the result of the first attempt.
//
// On partial failure (some subscriptions succeed, others fail), the caller must
// shut down the broker to clean up active subscriptions.
func (c *Consumer) Start() error {
	c.startOnce.Do(func() {
		for _, b := range c.bindings {
			if strings.TrimSpace(b.topic) == "" {
				c.startErr = fmt.Errorf("consumer: empty topic for event type %s", b.eventType)
				return
			}

			handler := c.makeHandler(b.eventType, b.newEvent, b.topic)
			if err := c.broker.Subscribe(b.topic, handler); err != nil {
				c.startErr = fmt.Errorf("consumer: subscribe to %s: %w", b.topic, err)
				return
			}

			c.log.Info(context.Background(), "subscribed to topic",
				"topic", b.topic,
				"event_type", string(b.eventType),
			)
		}
	})
	return c.startErr
}

// makeHandler returns a MessageHandler closure that deserializes JSON into the
// appropriate event struct, enriches the context with RequestContext, and
// dispatches to the EventHandler.
//
// Error classification:
//   - json.Unmarshal error: WARN + return nil (ACK). Poison pill protection.
//   - EventHandler error: return error (NACK with requeue) up to maxRetries,
//     then return nil (ACK) + ERROR log.
func (c *Consumer) makeHandler(
	eventType EventType,
	newEvent func() any,
	topic string,
) func(ctx context.Context, body []byte) error {
	return func(ctx context.Context, body []byte) error {
		event := newEvent()

		if err := json.Unmarshal(body, event); err != nil {
			c.log.Warn(ctx, "invalid JSON in event message, ACKing to prevent requeue",
				"topic", topic,
				"event_type", string(eventType),
				"raw_size", len(body),
				"raw_preview", rawPreview(body),
				logger.ErrorAttr(err),
			)
			return nil // ACK — poison pill protection.
		}

		// Enrich context with RequestContext fields from the event envelope.
		ctx = enrichContext(ctx, eventType, event)

		retryKey := buildRetryKey(eventType, event)

		if err := c.handler.HandleEvent(ctx, eventType, event); err != nil {
			attempt := c.retries.Increment(retryKey)
			if attempt >= maxAttempts {
				c.log.Error(ctx, "event handler failed after max attempts, ACKing",
					"topic", topic,
					"event_type", string(eventType),
					"attempt", attempt,
					logger.ErrorAttr(err),
				)
				c.retries.Remove(retryKey)
				return nil // ACK — give up after maxRetries.
			}

			c.log.Warn(ctx, "event handler failed, NACKing for requeue",
				"topic", topic,
				"event_type", string(eventType),
				"attempt", attempt,
				logger.ErrorAttr(err),
			)
			return err // NACK with requeue.
		}

		// Success — clean up retry counter and ACK.
		c.retries.Remove(retryKey)
		return nil
	}
}

// enrichContext creates a new context with RequestContext fields extracted from
// the event's envelope fields. This enables automatic log enrichment by the
// logger package.
func enrichContext(ctx context.Context, eventType EventType, event any) context.Context {
	rc := logger.RequestContext{}

	// Extract common envelope fields. Each event type embeds them at the top
	// level, so we use a type switch rather than reflection for safety and
	// clarity.
	switch e := event.(type) {
	case *DPStatusChangedEvent:
		rc.CorrelationID = e.CorrelationID
		rc.OrganizationID = e.OrganizationID
		rc.DocumentID = e.DocumentID
		rc.VersionID = e.VersionID
		rc.JobID = e.JobID
	case *DPProcessingCompletedEvent:
		rc.CorrelationID = e.CorrelationID
		rc.OrganizationID = e.OrganizationID
		rc.DocumentID = e.DocumentID
		rc.VersionID = e.VersionID
		rc.JobID = e.JobID
	case *DPProcessingFailedEvent:
		rc.CorrelationID = e.CorrelationID
		rc.OrganizationID = e.OrganizationID
		rc.DocumentID = e.DocumentID
		rc.VersionID = e.VersionID
		rc.JobID = e.JobID
	case *DPComparisonCompletedEvent:
		rc.CorrelationID = e.CorrelationID
		rc.OrganizationID = e.OrganizationID
		rc.DocumentID = e.DocumentID
		rc.JobID = e.JobID
	case *DPComparisonFailedEvent:
		rc.CorrelationID = e.CorrelationID
		rc.OrganizationID = e.OrganizationID
		rc.DocumentID = e.DocumentID
		rc.JobID = e.JobID
	case *LICStatusChangedEvent:
		rc.CorrelationID = e.CorrelationID
		rc.OrganizationID = e.OrganizationID
		rc.DocumentID = e.DocumentID
		rc.VersionID = e.VersionID
		rc.JobID = e.JobID
	case *REStatusChangedEvent:
		rc.CorrelationID = e.CorrelationID
		rc.OrganizationID = e.OrganizationID
		rc.DocumentID = e.DocumentID
		rc.VersionID = e.VersionID
		rc.JobID = e.JobID
	case *DMVersionArtifactsReadyEvent:
		rc.CorrelationID = e.CorrelationID
		rc.OrganizationID = e.OrganizationID
		rc.DocumentID = e.DocumentID
		rc.VersionID = e.VersionID
	case *DMVersionAnalysisReadyEvent:
		rc.CorrelationID = e.CorrelationID
		rc.OrganizationID = e.OrganizationID
		rc.DocumentID = e.DocumentID
		rc.VersionID = e.VersionID
	case *DMVersionReportsReadyEvent:
		rc.CorrelationID = e.CorrelationID
		rc.OrganizationID = e.OrganizationID
		rc.DocumentID = e.DocumentID
		rc.VersionID = e.VersionID
	case *DMVersionPartiallyAvailableEvent:
		rc.CorrelationID = e.CorrelationID
		rc.OrganizationID = e.OrganizationID
		rc.DocumentID = e.DocumentID
		rc.VersionID = e.VersionID
	case *DMVersionCreatedEvent:
		rc.CorrelationID = e.CorrelationID
		rc.OrganizationID = e.OrganizationID
		rc.DocumentID = e.DocumentID
		rc.VersionID = e.VersionID
	}

	return logger.WithRequestContext(ctx, rc)
}

// buildRetryKey constructs a unique identity string for a message, used to
// track retry counts across redeliveries. The key must be deterministic so
// that the same redelivered message maps to the same counter.
//
// Format: "{event_type}:{job_id}" for job-scoped events, or
// "{event_type}:{document_id}:{version_id}" for DM events that lack a job_id.
func buildRetryKey(eventType EventType, event any) string {
	switch e := event.(type) {
	case *DPStatusChangedEvent:
		return string(eventType) + ":" + e.JobID + ":" + e.Status
	case *DPProcessingCompletedEvent:
		return string(eventType) + ":" + e.JobID
	case *DPProcessingFailedEvent:
		return string(eventType) + ":" + e.JobID
	case *DPComparisonCompletedEvent:
		return string(eventType) + ":" + e.JobID
	case *DPComparisonFailedEvent:
		return string(eventType) + ":" + e.JobID
	case *LICStatusChangedEvent:
		return string(eventType) + ":" + e.JobID + ":" + e.Status
	case *REStatusChangedEvent:
		return string(eventType) + ":" + e.JobID + ":" + e.Status
	case *DMVersionArtifactsReadyEvent:
		return string(eventType) + ":" + e.DocumentID + ":" + e.VersionID
	case *DMVersionAnalysisReadyEvent:
		return string(eventType) + ":" + e.DocumentID + ":" + e.VersionID
	case *DMVersionReportsReadyEvent:
		return string(eventType) + ":" + e.DocumentID + ":" + e.VersionID
	case *DMVersionPartiallyAvailableEvent:
		return string(eventType) + ":" + e.DocumentID + ":" + e.VersionID
	case *DMVersionCreatedEvent:
		return string(eventType) + ":" + e.DocumentID + ":" + e.VersionID
	default:
		return string(eventType) + ":unknown"
	}
}

// rawPreview returns a truncated string preview of a raw message body for
// logging. This aids debugging of malformed messages without risking massive
// log entries. Truncation is UTF-8 safe: it never splits a multi-byte sequence.
func rawPreview(body []byte) string {
	const maxPreview = 200
	if len(body) <= maxPreview {
		return string(body)
	}
	truncated := body[:maxPreview]
	for len(truncated) > 0 && !utf8.Valid(truncated) {
		truncated = truncated[:len(truncated)-1]
	}
	return string(truncated) + "..."
}
