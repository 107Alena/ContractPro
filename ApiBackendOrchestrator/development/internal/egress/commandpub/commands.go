// Package commandpub publishes processing and comparison commands to the
// Document Processing (DP) domain via RabbitMQ.
//
// Command structs carry only business fields. The publisher wraps them into
// event envelopes with correlation_id and timestamp before serialization.
package commandpub

// ---------------------------------------------------------------------------
// Input command types (business fields only)
// ---------------------------------------------------------------------------

// ProcessDocumentCommand contains the business fields for requesting document
// processing. Envelope fields (correlation_id, timestamp) are added by the
// publisher at publish time.
type ProcessDocumentCommand struct {
	JobID              string `json:"job_id"`
	DocumentID         string `json:"document_id"`
	VersionID          string `json:"version_id"`
	OrganizationID     string `json:"organization_id"`
	RequestedByUserID  string `json:"requested_by_user_id"`
	SourceFileKey      string `json:"source_file_key"`
	SourceFileName     string `json:"source_file_name"`
	SourceFileSize     int64  `json:"source_file_size"`
	SourceFileChecksum string `json:"source_file_checksum"`
	SourceFileMIMEType string `json:"source_file_mime_type"`
}

// CompareVersionsCommand contains the business fields for requesting a version
// comparison. Envelope fields (correlation_id, timestamp) are added by the
// publisher at publish time.
type CompareVersionsCommand struct {
	JobID             string `json:"job_id"`
	DocumentID        string `json:"document_id"`
	OrganizationID    string `json:"organization_id"`
	RequestedByUserID string `json:"requested_by_user_id"`
	BaseVersionID     string `json:"base_version_id"`
	TargetVersionID   string `json:"target_version_id"`
}

// UserConfirmedTypeCommand contains the business fields for notifying LIC that
// the user has confirmed the contract type (FR-2.1.3). Envelope fields
// (correlation_id, timestamp) are added by the publisher at publish time.
type UserConfirmedTypeCommand struct {
	JobID             string `json:"job_id"`
	DocumentID        string `json:"document_id"`
	VersionID         string `json:"version_id"`
	OrganizationID    string `json:"organization_id"`
	ConfirmedByUserID string `json:"confirmed_by_user_id"`
	ContractType      string `json:"contract_type"`
}

// ---------------------------------------------------------------------------
// Event envelopes (internal, for JSON serialization)
// ---------------------------------------------------------------------------

// processDocumentEvent is the full JSON envelope published to the broker.
type processDocumentEvent struct {
	CorrelationID      string `json:"correlation_id"`
	Timestamp          string `json:"timestamp"`
	JobID              string `json:"job_id"`
	DocumentID         string `json:"document_id"`
	VersionID          string `json:"version_id"`
	OrganizationID     string `json:"organization_id"`
	RequestedByUserID  string `json:"requested_by_user_id"`
	SourceFileKey      string `json:"source_file_key"`
	SourceFileName     string `json:"source_file_name"`
	SourceFileSize     int64  `json:"source_file_size"`
	SourceFileChecksum string `json:"source_file_checksum"`
	SourceFileMIMEType string `json:"source_file_mime_type"`
}

// compareVersionsEvent is the full JSON envelope published to the broker.
type compareVersionsEvent struct {
	CorrelationID     string `json:"correlation_id"`
	Timestamp         string `json:"timestamp"`
	JobID             string `json:"job_id"`
	DocumentID        string `json:"document_id"`
	OrganizationID    string `json:"organization_id"`
	RequestedByUserID string `json:"requested_by_user_id"`
	BaseVersionID     string `json:"base_version_id"`
	TargetVersionID   string `json:"target_version_id"`
}

// userConfirmedTypeEvent is the full JSON envelope published to the broker.
type userConfirmedTypeEvent struct {
	CorrelationID     string `json:"correlation_id"`
	Timestamp         string `json:"timestamp"`
	JobID             string `json:"job_id"`
	DocumentID        string `json:"document_id"`
	VersionID         string `json:"version_id"`
	OrganizationID    string `json:"organization_id"`
	ConfirmedByUserID string `json:"confirmed_by_user_id"`
	ContractType      string `json:"contract_type"`
}
