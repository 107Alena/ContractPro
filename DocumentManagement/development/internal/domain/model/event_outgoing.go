package model

import "encoding/json"

// --- Outgoing confirmation events: DM → DP ---

// DocumentProcessingArtifactsPersisted confirms that DM successfully
// stored the processing artifacts from DP.
type DocumentProcessingArtifactsPersisted struct {
	EventMeta
	JobID      string `json:"job_id"`
	DocumentID string `json:"document_id"`
}

// DocumentProcessingArtifactsPersistFailed indicates that DM failed
// to store the processing artifacts from DP.
type DocumentProcessingArtifactsPersistFailed struct {
	EventMeta
	JobID        string `json:"job_id"`
	DocumentID   string `json:"document_id"`
	ErrorCode    string `json:"error_code,omitempty"`
	ErrorMessage string `json:"error_message"`
	IsRetryable  bool   `json:"is_retryable"`
}

// SemanticTreeProvided is the DM response containing the requested
// semantic tree for a document version. When DM encounters an error,
// ErrorMessage is non-empty and SemanticTree may be nil.
type SemanticTreeProvided struct {
	EventMeta
	JobID        string          `json:"job_id"`
	DocumentID   string          `json:"document_id"`
	VersionID    string          `json:"version_id"`
	SemanticTree json.RawMessage `json:"semantic_tree"`
	ErrorCode    string          `json:"error_code,omitempty"`
	ErrorMessage string          `json:"error_message,omitempty"`
	IsRetryable  bool            `json:"is_retryable,omitempty"`
}

// DocumentVersionDiffPersisted confirms that DM successfully
// stored the version comparison result from DP.
type DocumentVersionDiffPersisted struct {
	EventMeta
	JobID      string `json:"job_id"`
	DocumentID string `json:"document_id"`
}

// DocumentVersionDiffPersistFailed indicates that DM failed
// to store the version comparison result from DP.
type DocumentVersionDiffPersistFailed struct {
	EventMeta
	JobID        string `json:"job_id"`
	DocumentID   string `json:"document_id"`
	ErrorCode    string `json:"error_code,omitempty"`
	ErrorMessage string `json:"error_message"`
	IsRetryable  bool   `json:"is_retryable"`
}

// --- Outgoing confirmation events: DM → LIC / RE ---

// ArtifactsProvided is the DM response containing requested artifacts
// for a document version, sent to LIC or RE.
type ArtifactsProvided struct {
	EventMeta
	JobID        string                          `json:"job_id"`
	DocumentID   string                          `json:"document_id"`
	VersionID    string                          `json:"version_id"`
	Artifacts    map[ArtifactType]json.RawMessage `json:"artifacts"`
	MissingTypes []ArtifactType                  `json:"missing_types,omitempty"`
	ErrorCode    string                          `json:"error_code,omitempty"`
	ErrorMessage string                          `json:"error_message,omitempty"`
}

// LegalAnalysisArtifactsPersisted confirms that DM successfully
// stored the legal analysis artifacts from LIC.
type LegalAnalysisArtifactsPersisted struct {
	EventMeta
	JobID      string `json:"job_id"`
	DocumentID string `json:"document_id"`
}

// LegalAnalysisArtifactsPersistFailed indicates that DM failed
// to store the legal analysis artifacts from LIC.
type LegalAnalysisArtifactsPersistFailed struct {
	EventMeta
	JobID        string `json:"job_id"`
	DocumentID   string `json:"document_id"`
	ErrorCode    string `json:"error_code,omitempty"`
	ErrorMessage string `json:"error_message"`
	IsRetryable  bool   `json:"is_retryable"`
}

// ReportsArtifactsPersisted confirms that DM successfully
// stored the export reports from RE.
type ReportsArtifactsPersisted struct {
	EventMeta
	JobID      string `json:"job_id"`
	DocumentID string `json:"document_id"`
}

// ReportsArtifactsPersistFailed indicates that DM failed
// to store the export reports from RE.
type ReportsArtifactsPersistFailed struct {
	EventMeta
	JobID        string `json:"job_id"`
	DocumentID   string `json:"document_id"`
	ErrorCode    string `json:"error_code,omitempty"`
	ErrorMessage string `json:"error_message"`
	IsRetryable  bool   `json:"is_retryable"`
}

// --- Outgoing notification events: DM → downstream domains ---

// VersionProcessingArtifactsReady notifies LIC that DP processing
// artifacts are persisted and available for legal analysis.
type VersionProcessingArtifactsReady struct {
	EventMeta
	DocumentID    string         `json:"document_id"`
	VersionID     string         `json:"version_id"`
	OrgID         string         `json:"organization_id"`
	ArtifactTypes []ArtifactType `json:"artifact_types"`
}

// VersionAnalysisArtifactsReady notifies RE that LIC analysis
// artifacts are persisted and available for report generation.
type VersionAnalysisArtifactsReady struct {
	EventMeta
	DocumentID    string         `json:"document_id"`
	VersionID     string         `json:"version_id"`
	OrgID         string         `json:"organization_id"`
	ArtifactTypes []ArtifactType `json:"artifact_types"`
}

// VersionReportsReady notifies the orchestrator / API that export
// reports are persisted and the version is fully processed.
type VersionReportsReady struct {
	EventMeta
	DocumentID    string         `json:"document_id"`
	VersionID     string         `json:"version_id"`
	OrgID         string         `json:"organization_id"`
	ArtifactTypes []ArtifactType `json:"artifact_types"`
}

// VersionCreated notifies the orchestrator that a new document
// version has been created and is ready for processing.
type VersionCreated struct {
	EventMeta
	DocumentID      string     `json:"document_id"`
	VersionID       string     `json:"version_id"`
	VersionNumber   int        `json:"version_number"`
	OrgID           string     `json:"organization_id"`
	OriginType      OriginType `json:"origin_type"`
	ParentVersionID string     `json:"parent_version_id,omitempty"`
	CreatedByUserID string     `json:"created_by_user_id"`
}
