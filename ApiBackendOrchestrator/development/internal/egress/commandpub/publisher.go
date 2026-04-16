package commandpub

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"contractpro/api-orchestrator/internal/infra/observability/logger"
)

// BrokerPublisher is the subset of the broker.Client API that the command
// publisher depends on. Defined here (consumer-side) for dependency inversion.
type BrokerPublisher interface {
	Publish(ctx context.Context, topic string, payload []byte) error
}

// CommandPublisher publishes processing, comparison, and type confirmation
// commands to downstream domains (DP, LIC).
type CommandPublisher interface {
	// PublishProcessDocument publishes a process-document command to DP.
	PublishProcessDocument(ctx context.Context, cmd ProcessDocumentCommand) error

	// PublishCompareVersions publishes a compare-versions command to DP.
	PublishCompareVersions(ctx context.Context, cmd CompareVersionsCommand) error

	// PublishUserConfirmedType publishes a user-confirmed-type command to LIC.
	PublishUserConfirmedType(ctx context.Context, cmd UserConfirmedTypeCommand) error
}

// Publisher implements CommandPublisher using a RabbitMQ broker client.
type Publisher struct {
	broker              BrokerPublisher
	topicProcess        string
	topicCompare        string
	topicUserConfirmed  string
	log                 *logger.Logger
}

// Compile-time interface check.
var _ CommandPublisher = (*Publisher)(nil)

// NewPublisher creates a Publisher with the given broker client, topic names,
// and logger.
func NewPublisher(broker BrokerPublisher, topicProcess, topicCompare, topicUserConfirmed string, log *logger.Logger) *Publisher {
	return &Publisher{
		broker:             broker,
		topicProcess:       topicProcess,
		topicCompare:       topicCompare,
		topicUserConfirmed: topicUserConfirmed,
		log:                log.With("component", "command-publisher"),
	}
}

// PublishProcessDocument marshals a ProcessDocumentCommand into a JSON envelope
// and publishes it to the process-document topic.
func (p *Publisher) PublishProcessDocument(ctx context.Context, cmd ProcessDocumentCommand) error {
	rc := logger.RequestContextFrom(ctx)

	event := processDocumentEvent{
		CorrelationID:      rc.CorrelationID,
		Timestamp:          time.Now().UTC().Format(time.RFC3339),
		JobID:              cmd.JobID,
		DocumentID:         cmd.DocumentID,
		VersionID:          cmd.VersionID,
		OrganizationID:     cmd.OrganizationID,
		RequestedByUserID:  cmd.RequestedByUserID,
		SourceFileKey:      cmd.SourceFileKey,
		SourceFileName:     cmd.SourceFileName,
		SourceFileSize:     cmd.SourceFileSize,
		SourceFileChecksum: cmd.SourceFileChecksum,
		SourceFileMIMEType: cmd.SourceFileMIMEType,
	}

	payload, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("commandpub: PublishProcessDocument: marshal: %w", err)
	}

	if err := p.broker.Publish(ctx, p.topicProcess, payload); err != nil {
		p.log.Warn(ctx, "failed to publish process-document command",
			"operation", "PublishProcessDocument",
			"topic", p.topicProcess,
			"job_id", cmd.JobID,
			logger.ErrorAttr(err),
		)
		return fmt.Errorf("commandpub: PublishProcessDocument: publish: %w", err)
	}

	return nil
}

// PublishCompareVersions marshals a CompareVersionsCommand into a JSON envelope
// and publishes it to the compare-versions topic.
func (p *Publisher) PublishCompareVersions(ctx context.Context, cmd CompareVersionsCommand) error {
	rc := logger.RequestContextFrom(ctx)

	event := compareVersionsEvent{
		CorrelationID:     rc.CorrelationID,
		Timestamp:         time.Now().UTC().Format(time.RFC3339),
		JobID:             cmd.JobID,
		DocumentID:        cmd.DocumentID,
		OrganizationID:    cmd.OrganizationID,
		RequestedByUserID: cmd.RequestedByUserID,
		BaseVersionID:     cmd.BaseVersionID,
		TargetVersionID:   cmd.TargetVersionID,
	}

	payload, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("commandpub: PublishCompareVersions: marshal: %w", err)
	}

	if err := p.broker.Publish(ctx, p.topicCompare, payload); err != nil {
		p.log.Warn(ctx, "failed to publish compare-versions command",
			"operation", "PublishCompareVersions",
			"topic", p.topicCompare,
			"job_id", cmd.JobID,
			logger.ErrorAttr(err),
		)
		return fmt.Errorf("commandpub: PublishCompareVersions: publish: %w", err)
	}

	return nil
}

// PublishUserConfirmedType marshals a UserConfirmedTypeCommand into a JSON
// envelope and publishes it to the user-confirmed-type topic (→ LIC).
func (p *Publisher) PublishUserConfirmedType(ctx context.Context, cmd UserConfirmedTypeCommand) error {
	rc := logger.RequestContextFrom(ctx)

	event := userConfirmedTypeEvent{
		CorrelationID:     rc.CorrelationID,
		Timestamp:         time.Now().UTC().Format(time.RFC3339),
		JobID:             cmd.JobID,
		DocumentID:        cmd.DocumentID,
		VersionID:         cmd.VersionID,
		OrganizationID:    cmd.OrganizationID,
		ConfirmedByUserID: cmd.ConfirmedByUserID,
		ContractType:      cmd.ContractType,
	}

	payload, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("commandpub: PublishUserConfirmedType: marshal: %w", err)
	}

	if err := p.broker.Publish(ctx, p.topicUserConfirmed, payload); err != nil {
		p.log.Warn(ctx, "failed to publish user-confirmed-type command",
			"operation", "PublishUserConfirmedType",
			"topic", p.topicUserConfirmed,
			"version_id", cmd.VersionID,
			logger.ErrorAttr(err),
		)
		return fmt.Errorf("commandpub: PublishUserConfirmedType: publish: %w", err)
	}

	return nil
}
