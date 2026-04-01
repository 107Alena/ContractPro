package confirmation

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
var _ port.ConfirmationPublisherPort = (*ConfirmationPublisher)(nil)

// ConfirmationPublisher serializes confirmation/response events to JSON and
// publishes them to the appropriate broker topics. Each method corresponds
// to one of the 10 confirmation event types that DM sends back to DP, LIC, or RE.
type ConfirmationPublisher struct {
	broker BrokerPublisher
	topics confirmationTopicMap
}

// confirmationTopicMap holds the mapping from confirmation event type to topic name.
type confirmationTopicMap struct {
	dpArtifactsPersisted      string
	dpArtifactsPersistFailed  string
	semanticTreeProvided      string
	artifactsProvided         string
	diffPersisted             string
	diffPersistFailed         string
	licArtifactsPersisted     string
	licArtifactsPersistFailed string
	reReportsPersisted        string
	reReportsPersistFailed    string
}

// NewConfirmationPublisher creates a ConfirmationPublisher with the given broker
// client and topic configuration.
// Panics if broker is nil or any topic is empty (programmer/config error at startup).
func NewConfirmationPublisher(broker BrokerPublisher, cfg config.BrokerConfig) *ConfirmationPublisher {
	if broker == nil {
		panic("confirmation: broker must not be nil")
	}

	topics := confirmationTopicMap{
		dpArtifactsPersisted:      cfg.TopicDMResponsesArtifactsPersisted,
		dpArtifactsPersistFailed:  cfg.TopicDMResponsesArtifactsPersistFailed,
		semanticTreeProvided:      cfg.TopicDMResponsesSemanticTreeProvided,
		artifactsProvided:         cfg.TopicDMResponsesArtifactsProvided,
		diffPersisted:             cfg.TopicDMResponsesDiffPersisted,
		diffPersistFailed:         cfg.TopicDMResponsesDiffPersistFailed,
		licArtifactsPersisted:     cfg.TopicDMResponsesLICArtifactsPersisted,
		licArtifactsPersistFailed: cfg.TopicDMResponsesLICArtifactsPersistFailed,
		reReportsPersisted:        cfg.TopicDMResponsesREReportsPersisted,
		reReportsPersistFailed:    cfg.TopicDMResponsesREReportsPersistFailed,
	}

	for name, val := range map[string]string{
		"TopicDMResponsesArtifactsPersisted":        topics.dpArtifactsPersisted,
		"TopicDMResponsesArtifactsPersistFailed":    topics.dpArtifactsPersistFailed,
		"TopicDMResponsesSemanticTreeProvided":      topics.semanticTreeProvided,
		"TopicDMResponsesArtifactsProvided":         topics.artifactsProvided,
		"TopicDMResponsesDiffPersisted":             topics.diffPersisted,
		"TopicDMResponsesDiffPersistFailed":         topics.diffPersistFailed,
		"TopicDMResponsesLICArtifactsPersisted":     topics.licArtifactsPersisted,
		"TopicDMResponsesLICArtifactsPersistFailed": topics.licArtifactsPersistFailed,
		"TopicDMResponsesREReportsPersisted":        topics.reReportsPersisted,
		"TopicDMResponsesREReportsPersistFailed":    topics.reReportsPersistFailed,
	} {
		if val == "" {
			panic(fmt.Sprintf("confirmation: %s topic must not be empty", name))
		}
	}

	return &ConfirmationPublisher{
		broker: broker,
		topics: topics,
	}
}

// publishJSON marshals event to JSON and publishes it to the given topic.
// JSON serialization failures produce a non-retryable DomainError (deterministic
// programming error). Context errors and broker errors pass through unchanged.
func (p *ConfirmationPublisher) publishJSON(ctx context.Context, topic string, event any) error {
	data, err := json.Marshal(event)
	if err != nil {
		return &port.DomainError{
			Code:      port.ErrCodeBrokerFailed,
			Message:   fmt.Sprintf("confirmation: marshal %T", event),
			Retryable: false,
			Cause:     err,
		}
	}
	return p.broker.Publish(ctx, topic, data)
}

// PublishDPArtifactsPersisted publishes a confirmation that DP processing
// artifacts were successfully stored.
func (p *ConfirmationPublisher) PublishDPArtifactsPersisted(ctx context.Context, event model.DocumentProcessingArtifactsPersisted) error {
	return p.publishJSON(ctx, p.topics.dpArtifactsPersisted, event)
}

// PublishDPArtifactsPersistFailed publishes a failure notification for DP
// processing artifact storage.
func (p *ConfirmationPublisher) PublishDPArtifactsPersistFailed(ctx context.Context, event model.DocumentProcessingArtifactsPersistFailed) error {
	return p.publishJSON(ctx, p.topics.dpArtifactsPersistFailed, event)
}

// PublishSemanticTreeProvided publishes the response containing a semantic tree.
func (p *ConfirmationPublisher) PublishSemanticTreeProvided(ctx context.Context, event model.SemanticTreeProvided) error {
	return p.publishJSON(ctx, p.topics.semanticTreeProvided, event)
}

// PublishArtifactsProvided publishes the response containing requested artifacts.
func (p *ConfirmationPublisher) PublishArtifactsProvided(ctx context.Context, event model.ArtifactsProvided) error {
	return p.publishJSON(ctx, p.topics.artifactsProvided, event)
}

// PublishDiffPersisted publishes a confirmation that a version diff was
// successfully stored.
func (p *ConfirmationPublisher) PublishDiffPersisted(ctx context.Context, event model.DocumentVersionDiffPersisted) error {
	return p.publishJSON(ctx, p.topics.diffPersisted, event)
}

// PublishDiffPersistFailed publishes a failure notification for version diff storage.
func (p *ConfirmationPublisher) PublishDiffPersistFailed(ctx context.Context, event model.DocumentVersionDiffPersistFailed) error {
	return p.publishJSON(ctx, p.topics.diffPersistFailed, event)
}

// PublishLICArtifactsPersisted publishes a confirmation that LIC analysis
// artifacts were successfully stored.
func (p *ConfirmationPublisher) PublishLICArtifactsPersisted(ctx context.Context, event model.LegalAnalysisArtifactsPersisted) error {
	return p.publishJSON(ctx, p.topics.licArtifactsPersisted, event)
}

// PublishLICArtifactsPersistFailed publishes a failure notification for LIC
// analysis artifact storage.
func (p *ConfirmationPublisher) PublishLICArtifactsPersistFailed(ctx context.Context, event model.LegalAnalysisArtifactsPersistFailed) error {
	return p.publishJSON(ctx, p.topics.licArtifactsPersistFailed, event)
}

// PublishREReportsPersisted publishes a confirmation that RE export reports
// were successfully stored.
func (p *ConfirmationPublisher) PublishREReportsPersisted(ctx context.Context, event model.ReportsArtifactsPersisted) error {
	return p.publishJSON(ctx, p.topics.reReportsPersisted, event)
}

// PublishREReportsPersistFailed publishes a failure notification for RE
// export report storage.
func (p *ConfirmationPublisher) PublishREReportsPersistFailed(ctx context.Context, event model.ReportsArtifactsPersistFailed) error {
	return p.publishJSON(ctx, p.topics.reReportsPersistFailed, event)
}
