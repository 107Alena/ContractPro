package port

import "context"

// Orchestrator-facing publishers (LIC event-catalog §1) and the DLQ
// publisher used everywhere terminal errors are flushed.

// StatusPublisherPort publishes lic.events.status-changed on every external-
// status transition: IN_PROGRESS (start, also stage=STAGE_AWAITING_USER_
// CONFIRMATION at pause), COMPLETED, FAILED (high-architecture.md §6.13,
// LIC-TASK-044).
//
// Implementations validate the envelope before publishing — error_code /
// error_message / is_retryable MUST be present iff Status == FAILED.
type StatusPublisherPort interface {
	PublishStatus(ctx context.Context, evt LICStatusChangedEvent) error
}

// UncertaintyPublisherPort publishes lic.events.classification-uncertain
// exactly once per version after Agent 1 returns confidence < threshold
// (high-architecture.md §6.13, LIC-TASK-045).
//
// The Orchestrator deduplicates by lic-uncertain:{version_id} — safe
// re-publication on crash-recovery is supported (see §6.10 Resume safety
// net).
type UncertaintyPublisherPort interface {
	PublishClassificationUncertain(ctx context.Context, evt ClassificationUncertain) error
}

// DLQPublisherPort routes failed messages to the four PII-safe LIC DLQ
// topics with HMAC-hashed payload references (LIC event-catalog §3,
// integration-contracts.md §10, LIC-TASK-046).
//
// The envelope ALWAYS carries an HMAC of the original payload — never the
// raw bytes. For lic.dlq.publish-failed the full gzipped payload is
// uploaded to a dedicated Object Storage bucket; PayloadStorageKey on
// LICDLQEnvelope points to it (integration-contracts.md §10.2).
type DLQPublisherPort interface {
	PublishDLQ(ctx context.Context, topic DLQTopic, envelope LICDLQEnvelope) error
}
