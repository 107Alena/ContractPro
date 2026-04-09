// Package consumer subscribes to inbound RabbitMQ events from DP, LIC, RE, and
// DM domains, deserializes them into typed Go structs, and routes them to the
// Processing Status Tracker via the EventHandler interface.
//
// Error handling strategy:
//   - Invalid JSON: WARN log + ACK (poison pill protection).
//   - Handler error: NACK with requeue up to maxRetries, then ACK + ERROR log.
package consumer

// ---------------------------------------------------------------------------
// Event type constants
// ---------------------------------------------------------------------------

// EventType identifies the kind of inbound event for routing.
type EventType string

const (
	// DP events.
	EventDPStatusChanged        EventType = "dp.status-changed"
	EventDPProcessingCompleted  EventType = "dp.processing-completed"
	EventDPProcessingFailed     EventType = "dp.processing-failed"
	EventDPComparisonCompleted  EventType = "dp.comparison-completed"
	EventDPComparisonFailed     EventType = "dp.comparison-failed"

	// LIC events.
	EventLICStatusChanged EventType = "lic.status-changed"

	// RE events.
	EventREStatusChanged EventType = "re.status-changed"

	// DM events.
	EventDMVersionArtifactsReady EventType = "dm.version-artifacts-ready"
	EventDMVersionAnalysisReady  EventType = "dm.version-analysis-ready"
	EventDMVersionReportsReady   EventType = "dm.version-reports-ready"
	EventDMVersionPartiallyAvail EventType = "dm.version-partially-available"
	EventDMVersionCreated        EventType = "dm.version-created"
)

// ---------------------------------------------------------------------------
// Shared sub-types
// ---------------------------------------------------------------------------

// Warning represents a processing warning emitted by DP.
type Warning struct {
	Code     string `json:"code"`
	Message  string `json:"message"`
	Severity string `json:"severity"`
}

// ---------------------------------------------------------------------------
// DP event structs (5)
// ---------------------------------------------------------------------------

// DPStatusChangedEvent is emitted by DP when a job's processing status changes.
type DPStatusChangedEvent struct {
	CorrelationID  string `json:"correlation_id"`
	Timestamp      string `json:"timestamp"`
	JobID          string `json:"job_id"`
	DocumentID     string `json:"document_id"`
	VersionID      string `json:"version_id"`
	OrganizationID string `json:"organization_id,omitempty"`
	Status         string `json:"status"`
	Stage          string `json:"stage,omitempty"`
	Message        string `json:"message,omitempty"`
}

// DPProcessingCompletedEvent is emitted by DP when document processing finishes
// successfully (possibly with warnings).
type DPProcessingCompletedEvent struct {
	CorrelationID  string    `json:"correlation_id"`
	Timestamp      string    `json:"timestamp"`
	JobID          string    `json:"job_id"`
	DocumentID     string    `json:"document_id"`
	VersionID      string    `json:"version_id"`
	OrganizationID string    `json:"organization_id,omitempty"`
	Warnings       []Warning `json:"warnings,omitempty"`
}

// DPProcessingFailedEvent is emitted by DP when document processing fails.
type DPProcessingFailedEvent struct {
	CorrelationID  string `json:"correlation_id"`
	Timestamp      string `json:"timestamp"`
	JobID          string `json:"job_id"`
	DocumentID     string `json:"document_id"`
	VersionID      string `json:"version_id"`
	OrganizationID string `json:"organization_id,omitempty"`
	ErrorCode      string `json:"error_code"`
	ErrorMessage   string `json:"error_message"`
	FailedAtStage  string `json:"failed_at_stage"`
	IsRetryable    bool   `json:"is_retryable"`
}

// DPComparisonCompletedEvent is emitted by DP when version comparison finishes.
type DPComparisonCompletedEvent struct {
	CorrelationID   string `json:"correlation_id"`
	Timestamp       string `json:"timestamp"`
	JobID           string `json:"job_id"`
	DocumentID      string `json:"document_id"`
	OrganizationID  string `json:"organization_id,omitempty"`
	BaseVersionID   string `json:"base_version_id"`
	TargetVersionID string `json:"target_version_id"`
}

// DPComparisonFailedEvent is emitted by DP when version comparison fails.
type DPComparisonFailedEvent struct {
	CorrelationID   string `json:"correlation_id"`
	Timestamp       string `json:"timestamp"`
	JobID           string `json:"job_id"`
	DocumentID      string `json:"document_id"`
	OrganizationID  string `json:"organization_id,omitempty"`
	BaseVersionID   string `json:"base_version_id"`
	TargetVersionID string `json:"target_version_id"`
	ErrorCode       string `json:"error_code"`
	ErrorMessage    string `json:"error_message"`
	IsRetryable     bool   `json:"is_retryable"`
}

// ---------------------------------------------------------------------------
// LIC / RE event structs (2) — same shape, distinct types for type safety.
// ---------------------------------------------------------------------------

// LICStatusChangedEvent is emitted by Legal Intelligence Core on status change.
type LICStatusChangedEvent struct {
	CorrelationID  string `json:"correlation_id"`
	Timestamp      string `json:"timestamp"`
	JobID          string `json:"job_id"`
	DocumentID     string `json:"document_id"`
	VersionID      string `json:"version_id"`
	OrganizationID string `json:"organization_id"`
	Status         string `json:"status"`
	ErrorCode      string `json:"error_code,omitempty"`
	ErrorMessage   string `json:"error_message,omitempty"`
	IsRetryable    *bool  `json:"is_retryable,omitempty"`
}

// REStatusChangedEvent is emitted by Reporting Engine on status change.
type REStatusChangedEvent struct {
	CorrelationID  string `json:"correlation_id"`
	Timestamp      string `json:"timestamp"`
	JobID          string `json:"job_id"`
	DocumentID     string `json:"document_id"`
	VersionID      string `json:"version_id"`
	OrganizationID string `json:"organization_id"`
	Status         string `json:"status"`
	ErrorCode      string `json:"error_code,omitempty"`
	ErrorMessage   string `json:"error_message,omitempty"`
	IsRetryable    *bool  `json:"is_retryable,omitempty"`
}

// ---------------------------------------------------------------------------
// DM event structs (5)
// ---------------------------------------------------------------------------

// DMVersionArtifactsReadyEvent is emitted by DM when processing artifacts
// (OCR, text, structure, semantic tree) are persisted.
type DMVersionArtifactsReadyEvent struct {
	CorrelationID  string   `json:"correlation_id"`
	Timestamp      string   `json:"timestamp"`
	DocumentID     string   `json:"document_id"`
	VersionID      string   `json:"version_id"`
	OrganizationID string   `json:"organization_id"`
	ArtifactTypes  []string `json:"artifact_types"`
}

// DMVersionAnalysisReadyEvent is emitted by DM when LIC analysis artifacts are
// persisted.
type DMVersionAnalysisReadyEvent struct {
	CorrelationID  string   `json:"correlation_id"`
	Timestamp      string   `json:"timestamp"`
	DocumentID     string   `json:"document_id"`
	VersionID      string   `json:"version_id"`
	OrganizationID string   `json:"organization_id"`
	ArtifactTypes  []string `json:"artifact_types"`
}

// DMVersionReportsReadyEvent is emitted by DM when RE-generated reports are
// persisted.
type DMVersionReportsReadyEvent struct {
	CorrelationID  string   `json:"correlation_id"`
	Timestamp      string   `json:"timestamp"`
	DocumentID     string   `json:"document_id"`
	VersionID      string   `json:"version_id"`
	OrganizationID string   `json:"organization_id"`
	ArtifactTypes  []string `json:"artifact_types"`
}

// DMVersionPartiallyAvailableEvent is emitted by DM when some pipeline stage
// failed but partial results are available.
type DMVersionPartiallyAvailableEvent struct {
	CorrelationID  string `json:"correlation_id"`
	Timestamp      string `json:"timestamp"`
	DocumentID     string `json:"document_id"`
	VersionID      string `json:"version_id"`
	OrganizationID string `json:"organization_id"`
	FailedStage    string `json:"failed_stage,omitempty"`
	ErrorMessage   string `json:"error_message,omitempty"`
}

// DMVersionCreatedEvent is emitted by DM when a new document version is created.
type DMVersionCreatedEvent struct {
	CorrelationID   string `json:"correlation_id"`
	Timestamp       string `json:"timestamp"`
	DocumentID      string `json:"document_id"`
	VersionID       string `json:"version_id"`
	VersionNumber   int    `json:"version_number"`
	OrganizationID  string `json:"organization_id"`
	OriginType      string `json:"origin_type"`
	ParentVersionID string `json:"parent_version_id,omitempty"`
	CreatedByUserID string `json:"created_by_user_id"`
}
