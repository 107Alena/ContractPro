// Package ssebroadcast provides SSE event broadcasting via Redis Pub/Sub.
//
// The broadcaster is the single owner of the sse:broadcast:{org_id} channel
// convention. It is used by the statustracker to publish events and by the
// SSE handler to subscribe. This eliminates channel name duplication across
// packages.
package ssebroadcast

import (
	"context"
	"encoding/json"
	"fmt"

	"contractpro/api-orchestrator/internal/infra/observability/logger"
)

// ---------------------------------------------------------------------------
// Consumer-side interface (dependency inversion)
// ---------------------------------------------------------------------------

// Publisher provides Redis Pub/Sub publish capability.
//
// Satisfied by: *kvstore.Client
type Publisher interface {
	Publish(ctx context.Context, channel string, message string) error
}

// ---------------------------------------------------------------------------
// Broadcaster interface
// ---------------------------------------------------------------------------

// Broadcaster publishes SSE events to organization-scoped Redis Pub/Sub
// channels for real-time delivery to connected SSE clients.
type Broadcaster interface {
	Broadcast(ctx context.Context, orgID string, event Event) error
}

// ---------------------------------------------------------------------------
// Event payload
// ---------------------------------------------------------------------------

// Event is the canonical SSE event payload published to Redis Pub/Sub and
// delivered to SSE clients. All SSE event producers must use this type.
type Event struct {
	EventType       string `json:"event_type"`
	DocumentID      string `json:"document_id"`
	VersionID       string `json:"version_id,omitempty"`
	JobID           string `json:"job_id,omitempty"`
	Status          string `json:"status"`
	Message         string `json:"message"`
	Timestamp       string `json:"timestamp"`
	IsRetryable     bool   `json:"is_retryable"`
	ErrorCode       string `json:"error_code,omitempty"`
	ErrorMessage    string `json:"error_message,omitempty"`
	BaseVersionID   string `json:"base_version_id,omitempty"`
	TargetVersionID string `json:"target_version_id,omitempty"`

	// Classification fields (event_type = "type_confirmation_required").
	SuggestedType string                       `json:"suggested_type,omitempty"`
	Confidence    float64                      `json:"confidence,omitempty"`
	Threshold     float64                      `json:"threshold,omitempty"`
	Alternatives  []ClassificationAlternative  `json:"alternatives,omitempty"`
}

// ClassificationAlternative represents an alternative contract type suggestion
// with its confidence score. Used in type_confirmation_required SSE events.
type ClassificationAlternative struct {
	ContractType string  `json:"contract_type"`
	Confidence   float64 `json:"confidence"`
}

// ---------------------------------------------------------------------------
// Channel naming
// ---------------------------------------------------------------------------

const channelPrefix = "sse:broadcast"

// Channel builds the Redis Pub/Sub channel name for an organization's SSE
// broadcasts. This is the single source of truth for the channel naming
// convention — both producers (statustracker) and consumers (SSE handler)
// must use this function.
func Channel(orgID string) string {
	return channelPrefix + ":" + orgID
}

// ---------------------------------------------------------------------------
// Implementation
// ---------------------------------------------------------------------------

// Compile-time interface check.
var _ Broadcaster = (*broadcaster)(nil)

type broadcaster struct {
	pub Publisher
	log *logger.Logger
}

// NewBroadcaster creates a Broadcaster backed by Redis Pub/Sub.
func NewBroadcaster(pub Publisher, log *logger.Logger) Broadcaster {
	return &broadcaster{
		pub: pub,
		log: log.With("component", "sse-broadcaster"),
	}
}

// Broadcast publishes an SSE event to the Redis Pub/Sub channel for the given
// organization. Returns nil on success, or an error on marshal/publish failure.
//
// Callers decide the error-handling policy: the statustracker logs and ignores
// (status is already persisted; SSE clients catch up via polling fallback),
// while other callers may propagate the error.
func (b *broadcaster) Broadcast(ctx context.Context, orgID string, event Event) error {
	if orgID == "" {
		b.log.Warn(ctx, "broadcast called with empty orgID, skipping")
		return nil
	}

	data, err := json.Marshal(event)
	if err != nil {
		b.log.Error(ctx, "failed to marshal SSE event",
			logger.ErrorAttr(err),
			"status", event.Status)
		return fmt.Errorf("ssebroadcast: marshal: %w", err)
	}

	channel := Channel(orgID)
	if err := b.pub.Publish(ctx, channel, string(data)); err != nil {
		b.log.Error(ctx, "failed to publish SSE event",
			logger.ErrorAttr(err),
			"channel", channel,
			"status", event.Status)
		return fmt.Errorf("ssebroadcast: publish: %w", err)
	}

	b.log.Debug(ctx, "broadcast SSE event",
		"channel", channel,
		"event_type", event.EventType,
		"status", event.Status)
	return nil
}
