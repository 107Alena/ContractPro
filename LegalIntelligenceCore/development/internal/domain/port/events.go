// Package port defines the hexagonal-architecture interfaces (ports) for the
// Legal Intelligence Core service. Ports describe what the application layer
// asks of the outside world — inbound event handlers, outbound publishers,
// idempotency / pending-state stores, LLM providers and the agent contract —
// without leaning on any concrete infrastructure (RabbitMQ, Redis, HTTP, ...).
//
// Wire-shape DTOs for the six subscribed events, the four orchestrator-facing
// publications and the DLQ envelope live in this file (events.go). They sit
// next to the handler / publisher ports that consume or produce them so
// callers see the whole contract in one import; they are NOT domain entities
// — those live in internal/domain/model.
//
// FROZEN contract references:
//   - Inbound DM contracts: DocumentManagement/architecture/event-catalog.md §2
//   - Inbound Orchestrator contract: ApiBackendOrchestrator/architecture/event-catalog.md §1.3
//   - Outbound to DM: DocumentManagement/architecture/event-catalog.md §1.4-1.5
//   - Outbound to Orchestrator: LegalIntelligenceCore/architecture/event-catalog.md §1
//   - DLQ envelope: LegalIntelligenceCore/architecture/event-catalog.md §3 (PII-safe; HMAC over original_message)
//
// All timestamps are ISO-8601 strings on the wire; callers convert to/from
// time.Time at the adapter boundary.
package port

import (
	"encoding/json"

	"contractpro/legal-intelligence-core/internal/domain/model"
)

// ----------------------------------------------------------------------------
// Inbound DTOs — what LIC receives from RabbitMQ.
// ----------------------------------------------------------------------------

// VersionProcessingArtifactsReady is the main pipeline trigger event, published
// by DM on `dm.events.version-artifacts-ready` after DP-stage artifacts are
// persisted (DM event-catalog §2.2 → VersionProcessingArtifactsReady).
//
// `ArtifactTypes` lists what DP actually persisted; LIC uses it to anticipate
// what a subsequent GetArtifactsRequest will return (high-architecture.md §6.5).
// `ParentVersionID` is the sole driver of the internal RE_CHECK mode
// (ASSUMPTION-LIC-02); `OriginType` is opaque and forwarded to the UI.
type VersionProcessingArtifactsReady struct {
	CorrelationID   string   `json:"correlation_id"`
	Timestamp       string   `json:"timestamp"`
	DocumentID      string   `json:"document_id"`
	VersionID       string   `json:"version_id"`
	OrganizationID  string   `json:"organization_id"`
	ArtifactTypes   []string `json:"artifact_types"`
	JobID           string   `json:"job_id"`
	OriginType      string   `json:"origin_type"`
	ParentVersionID *string  `json:"parent_version_id,omitempty"`
	CreatedByUserID string   `json:"created_by_user_id"`
}

// VersionCreated is published by DM on `dm.events.version-created` for every
// newly registered document version (DM event-catalog §2.2). LIC subscribes
// solely to populate the Redis version-meta cache (lic-version-meta:{version_id})
// so the subsequent VersionProcessingArtifactsReady can decide whether to run
// Agent 9 — Risk Delta (high-architecture.md §8.3).
type VersionCreated struct {
	CorrelationID   string  `json:"correlation_id"`
	Timestamp       string  `json:"timestamp"`
	DocumentID      string  `json:"document_id"`
	VersionID       string  `json:"version_id"`
	VersionNumber   int     `json:"version_number"`
	OrganizationID  string  `json:"organization_id"`
	OriginType      string  `json:"origin_type"`
	ParentVersionID *string `json:"parent_version_id,omitempty"`
	JobID           string  `json:"job_id,omitempty"`
	CreatedByUserID string  `json:"created_by_user_id"`
}

// ArtifactsProvided is the async response from DM on
// `dm.responses.artifacts-provided` to a LIC GetArtifactsRequest (DM
// event-catalog §2.1). It carries every requested artifact whose content was
// found, plus `MissingTypes` for the rest.
//
// `Artifacts` is intentionally a map[ArtifactType]json.RawMessage: it is a
// byte-faithful copy of what DM published, never re-encoded, which is what
// PendingTypeConfirmation.InputArtifacts expects for pause-and-resume.
type ArtifactsProvided struct {
	CorrelationID  string                                  `json:"correlation_id"`
	Timestamp      string                                  `json:"timestamp"`
	JobID          string                                  `json:"job_id"`
	DocumentID     string                                  `json:"document_id"`
	VersionID      string                                  `json:"version_id"`
	Artifacts      map[model.ArtifactType]json.RawMessage  `json:"artifacts"`
	MissingTypes   []model.ArtifactType                    `json:"missing_types,omitempty"`
	ErrorCode      string                                  `json:"error_code,omitempty"`
	ErrorMessage   string                                  `json:"error_message,omitempty"`
}

// LegalAnalysisArtifactsPersisted is DM's positive confirmation that
// LegalAnalysisArtifactsReady was persisted (DM event-catalog §2.1). The
// PipelineOrchestrator waits for this before publishing the COMPLETED
// status event (high-architecture.md §6.5 step 10).
type LegalAnalysisArtifactsPersisted struct {
	CorrelationID string `json:"correlation_id"`
	Timestamp     string `json:"timestamp"`
	JobID         string `json:"job_id"`
	DocumentID    string `json:"document_id"`
}

// LegalAnalysisArtifactsPersistFailed is DM's negative confirmation
// (DM event-catalog §2.1). When `IsRetryable==false` the orchestrator
// surfaces a fatal FAILED with DM_PERSIST_FAILED; when true, the pipeline
// may retry on the same or new job (error-handling.md §3.2).
type LegalAnalysisArtifactsPersistFailed struct {
	CorrelationID string `json:"correlation_id"`
	Timestamp     string `json:"timestamp"`
	JobID         string `json:"job_id"`
	DocumentID    string `json:"document_id"`
	ErrorCode     string `json:"error_code,omitempty"`
	ErrorMessage  string `json:"error_message"`
	IsRetryable   bool   `json:"is_retryable"`
}

// UserConfirmedType is the Orchestrator command on
// `orch.commands.user-confirmed-type` that resumes a paused pipeline after a
// low-confidence classification (Orchestrator event-catalog §1.3,
// high-architecture.md §6.10).
//
// `ContractType` MUST match the LIC whitelist (12 values, regex
// `^[A-Z_]{1,32}$`) — see model.IsValidContractType. Any mismatch is routed to
// lic.dlq.invalid-message and the pipeline publishes FAILED with
// INVALID_CONTRACT_TYPE (security.md §11.2: this is mandatory validation, not
// a safety net).
type UserConfirmedType struct {
	CorrelationID  string `json:"correlation_id"`
	Timestamp      string `json:"timestamp"`
	JobID          string `json:"job_id"`
	DocumentID     string `json:"document_id"`
	VersionID      string `json:"version_id"`
	OrganizationID string `json:"organization_id"`
	ContractType   string `json:"contract_type"`
	UserID         string `json:"user_id,omitempty"`
}

// ----------------------------------------------------------------------------
// Outbound DTOs — what LIC publishes onto RabbitMQ.
// ----------------------------------------------------------------------------

// GetArtifactsRequest is published on `lic.requests.artifacts` to fetch DM-
// stored artifacts for a version (DM event-catalog §1.4). Each request carries
// its own correlation_id suffix so ArtifactsAwaiterPort can route the eventual
// ArtifactsProvided to the right awaiting goroutine.
type GetArtifactsRequest struct {
	CorrelationID  string               `json:"correlation_id"`
	Timestamp      string               `json:"timestamp"`
	JobID          string               `json:"job_id"`
	DocumentID     string               `json:"document_id"`
	VersionID      string               `json:"version_id"`
	OrganizationID string               `json:"organization_id,omitempty"`
	ArtifactTypes  []model.ArtifactType `json:"artifact_types"`
}

// LegalAnalysisArtifactsReady is the pipeline's terminal publication on
// `lic.artifacts.analysis-ready` — the consolidated payload of all eight
// mandatory artifacts plus the optional v1.1 `risk_delta` extension
// (DM event-catalog §1.5, LIC event-catalog §2, ADR-LIC-05).
//
// Result Aggregator (LIC-TASK-035) is the only writer of this struct and is
// also responsible for stripping internal fields (risks[].rationale,
// key_parameters.internal_extras, prompt_injection_detected) before construction.
//
// Each artifact slot is the typed model from internal/domain/model — adapters
// rely on those types' json tags to produce a wire payload faithful to the
// DM-side schema. `RiskDelta` is *RiskDelta with omitempty: present only when
// parent_version_id != null AND parent RISK_ANALYSIS was available.
type LegalAnalysisArtifactsReady struct {
	CorrelationID  string `json:"correlation_id"`
	Timestamp      string `json:"timestamp"`
	JobID          string `json:"job_id"`
	DocumentID     string `json:"document_id"`
	VersionID      string `json:"version_id"`
	OrganizationID string `json:"organization_id,omitempty"`

	ClassificationResult *model.ClassificationResult      `json:"classification_result"`
	KeyParameters        *model.KeyParameters             `json:"key_parameters"`
	RiskAnalysis         *model.RiskAnalysis              `json:"risk_analysis"`
	RiskProfile          *model.RiskProfile               `json:"risk_profile"`
	Recommendations      model.Recommendations            `json:"recommendations"`
	Summary              *model.Summary                   `json:"summary"`
	DetailedReport       *model.DetailedReport            `json:"detailed_report"`
	AggregateScore       *model.AggregateScore            `json:"aggregate_score"`

	RiskDelta *model.RiskDelta `json:"risk_delta,omitempty"`
}

// LICStatusChangedEvent is published on `lic.events.status-changed` for every
// IN_PROGRESS / COMPLETED / FAILED transition (LIC event-catalog §1.1).
// `Stage`, `ErrorCode`, `ErrorMessage`, `IsRetryable` are set conditionally;
// see error-handling.md §3 for the catalog.
//
// The Orchestrator deduplicates by `lic-status:{job_id}:{status}` so safe
// re-publication on crash-recovery is supported.
type LICStatusChangedEvent struct {
	CorrelationID  string             `json:"correlation_id"`
	Timestamp      string             `json:"timestamp"`
	JobID          string             `json:"job_id"`
	DocumentID     string             `json:"document_id"`
	VersionID      string             `json:"version_id"`
	OrganizationID string             `json:"organization_id"`
	Status         model.ExternalStatus `json:"status"`

	// Set only for IN_PROGRESS or FAILED — the omit-on-zero policy keeps
	// COMPLETED minimal (LIC event-catalog §1.1).
	Stage model.Stage `json:"stage,omitempty"`

	// All three are emitted only when Status == FAILED. ErrorMessage is RU
	// (NFR-5.2); ErrorCode and the catalog row are model.ErrorCode.
	ErrorCode    model.ErrorCode `json:"error_code,omitempty"`
	ErrorMessage string          `json:"error_message,omitempty"`
	IsRetryable  *bool           `json:"is_retryable,omitempty"`
}

// ClassificationUncertain is published once per version on
// `lic.events.classification-uncertain` when Agent 1 returns
// confidence < LIC_CONFIDENCE_THRESHOLD (LIC event-catalog §1.2). The
// Orchestrator routes the user to a contract-type confirmation prompt and
// later sends back UserConfirmedType.
type ClassificationUncertain struct {
	CorrelationID  string                              `json:"correlation_id"`
	Timestamp      string                              `json:"timestamp"`
	JobID          string                              `json:"job_id"`
	DocumentID     string                              `json:"document_id"`
	VersionID      string                              `json:"version_id"`
	OrganizationID string                              `json:"organization_id"`
	SuggestedType  model.ContractType                  `json:"suggested_type"`
	Confidence     float64                             `json:"confidence"`
	Threshold      float64                             `json:"threshold"`
	Alternatives   []model.ClassificationAlternative   `json:"alternatives,omitempty"`
}

// ----------------------------------------------------------------------------
// DLQ envelope — PII-safe shape for all four lic.dlq.* topics.
// ----------------------------------------------------------------------------

// DLQTopic enumerates the four LIC DLQ topics (integration-contracts.md §10,
// LIC event-catalog §3.2). Each carries the same LICDLQEnvelope shape with
// topic-specific fields conditionally populated.
type DLQTopic string

const (
	DLQTopicInvalidMessage     DLQTopic = "lic.dlq.invalid-message"
	DLQTopicConsumerFailed     DLQTopic = "lic.dlq.consumer-failed"
	DLQTopicPublishFailed      DLQTopic = "lic.dlq.publish-failed"
	DLQTopicAgentOutputInvalid DLQTopic = "lic.dlq.agent-output-invalid"
)

// IsValid reports whether t is one of the four declared DLQ topics.
func (t DLQTopic) IsValid() bool {
	switch t {
	case DLQTopicInvalidMessage, DLQTopicConsumerFailed,
		DLQTopicPublishFailed, DLQTopicAgentOutputInvalid:
		return true
	default:
		return false
	}
}

// LICDLQEnvelope is the PII-safe envelope for all four DLQ topics
// (LIC event-catalog §3.1, integration-contracts.md §10.1). The raw failed
// message is NEVER embedded — only an HMAC-SHA-256 hash keyed by
// LIC_DLQ_HASH_KEY (first 64 hex chars) plus its byte size. This closes the
// 152-ФЗ data-residency concern while preserving forensics correlation.
//
// For lic.dlq.publish-failed (where the full payload is needed for post-
// mortem because DM rejected it), the full gzipped payload is uploaded to a
// dedicated Object Storage bucket and referenced via PayloadStorageKey with
// restricted IAM access (integration-contracts.md §10.2).
type LICDLQEnvelope struct {
	OriginalTopic            string `json:"original_topic"`
	OriginalMessageHash      string `json:"original_message_hash"`
	OriginalMessageSizeBytes int    `json:"original_message_size_bytes"`

	ErrorCode    model.ErrorCode `json:"error_code"`
	ErrorMessage string          `json:"error_message"`
	RetryCount   int             `json:"retry_count"`

	// Correlation fields are best-effort — when an invalid-message envelope
	// fails JSON parsing entirely the publisher leaves them empty.
	CorrelationID  string `json:"correlation_id,omitempty"`
	JobID          string `json:"job_id,omitempty"`
	DocumentID     string `json:"document_id,omitempty"`
	VersionID      string `json:"version_id,omitempty"`
	OrganizationID string `json:"organization_id,omitempty"`

	// Set for DLQTopicAgentOutputInvalid only.
	AgentID            model.AgentID `json:"agent_id,omitempty"`
	Stage              model.Stage   `json:"stage,omitempty"`
	RawLLMResponseHash string        `json:"raw_llm_response_hash,omitempty"`

	// Set for DLQTopicPublishFailed only; resolves to the full gzipped
	// payload in the restricted object-storage bucket
	// (integration-contracts.md §10.2).
	PayloadStorageKey string `json:"payload_storage_key,omitempty"`

	FailedAt string `json:"failed_at"`
}
