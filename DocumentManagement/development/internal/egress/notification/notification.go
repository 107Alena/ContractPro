package notification

import (
	"context"
	"encoding/json"
	"fmt"

	"contractpro/document-management/internal/config"
	"contractpro/document-management/internal/domain/model"
	"contractpro/document-management/internal/domain/port"
)

// BrokerPublisher is a consumer-side interface covering the subset of
// broker.Client methods used by this adapter.
// Implementations must be safe for concurrent use by multiple goroutines.
type BrokerPublisher interface {
	Publish(ctx context.Context, topic string, payload []byte) error
}

// Compile-time interface compliance check.
var _ port.NotificationPublisherPort = (*NotificationPublisher)(nil)

// NotificationPublisher serializes notification events to JSON and publishes
// them to downstream domain topics. Each method corresponds to one of the 5
// notification event types that DM broadcasts after internal state transitions.
type NotificationPublisher struct {
	broker BrokerPublisher
	topics notificationTopicMap
}

// notificationTopicMap holds the mapping from notification event type to topic name.
type notificationTopicMap struct {
	versionArtifactsReady     string
	versionAnalysisReady      string
	versionReportsReady       string
	versionCreated            string
	versionPartiallyAvailable string
}

// NewNotificationPublisher creates a NotificationPublisher with the given broker
// client and topic configuration.
// Panics if broker is nil or any topic is empty (programmer/config error at startup).
func NewNotificationPublisher(broker BrokerPublisher, cfg config.BrokerConfig) *NotificationPublisher {
	if broker == nil {
		panic("notification: broker must not be nil")
	}

	topics := notificationTopicMap{
		versionArtifactsReady:     cfg.TopicDMEventsVersionArtifactsReady,
		versionAnalysisReady:      cfg.TopicDMEventsVersionAnalysisReady,
		versionReportsReady:       cfg.TopicDMEventsVersionReportsReady,
		versionCreated:            cfg.TopicDMEventsVersionCreated,
		versionPartiallyAvailable: cfg.TopicDMEventsVersionPartiallyAvailable,
	}

	for name, val := range map[string]string{
		"TopicDMEventsVersionArtifactsReady":     topics.versionArtifactsReady,
		"TopicDMEventsVersionAnalysisReady":      topics.versionAnalysisReady,
		"TopicDMEventsVersionReportsReady":       topics.versionReportsReady,
		"TopicDMEventsVersionCreated":            topics.versionCreated,
		"TopicDMEventsVersionPartiallyAvailable": topics.versionPartiallyAvailable,
	} {
		if val == "" {
			panic(fmt.Sprintf("notification: %s topic must not be empty", name))
		}
	}

	return &NotificationPublisher{
		broker: broker,
		topics: topics,
	}
}

// publishJSON marshals event to JSON and publishes it to the given topic.
// JSON serialization failures produce a non-retryable DomainError (deterministic
// programming error). Context errors and broker errors pass through unchanged.
func (p *NotificationPublisher) publishJSON(ctx context.Context, topic string, event any) error {
	data, err := json.Marshal(event)
	if err != nil {
		return &port.DomainError{
			Code:      port.ErrCodeBrokerFailed,
			Message:   fmt.Sprintf("notification: marshal %T", event),
			Retryable: false,
			Cause:     err,
		}
	}
	return p.broker.Publish(ctx, topic, data)
}

// PublishVersionProcessingArtifactsReady notifies LIC that DP processing
// artifacts are persisted and available for legal analysis.
func (p *NotificationPublisher) PublishVersionProcessingArtifactsReady(ctx context.Context, event model.VersionProcessingArtifactsReady) error {
	return p.publishJSON(ctx, p.topics.versionArtifactsReady, event)
}

// PublishVersionAnalysisArtifactsReady notifies RE that LIC analysis
// artifacts are persisted and available for report generation.
func (p *NotificationPublisher) PublishVersionAnalysisArtifactsReady(ctx context.Context, event model.VersionAnalysisArtifactsReady) error {
	return p.publishJSON(ctx, p.topics.versionAnalysisReady, event)
}

// PublishVersionReportsReady notifies the orchestrator / API that export
// reports are persisted and the version is fully processed.
func (p *NotificationPublisher) PublishVersionReportsReady(ctx context.Context, event model.VersionReportsReady) error {
	return p.publishJSON(ctx, p.topics.versionReportsReady, event)
}

// PublishVersionCreated notifies the orchestrator that a new document
// version has been created and is ready for processing.
func (p *NotificationPublisher) PublishVersionCreated(ctx context.Context, event model.VersionCreated) error {
	return p.publishJSON(ctx, p.topics.versionCreated, event)
}

// PublishVersionPartiallyAvailable notifies the orchestrator that some
// artifacts for a version are available but the full pipeline has not
// completed (BRE-010).
func (p *NotificationPublisher) PublishVersionPartiallyAvailable(ctx context.Context, event model.VersionPartiallyAvailable) error {
	return p.publishJSON(ctx, p.topics.versionPartiallyAvailable, event)
}
