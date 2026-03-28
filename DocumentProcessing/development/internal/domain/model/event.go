package model

import "time"

// EventMeta contains fields shared by all events: correlation ID for tracing
// and timestamp for ordering/observability.
type EventMeta struct {
	CorrelationID string    `json:"correlation_id"`
	Timestamp     time.Time `json:"timestamp"`
}

// --- Events: DP → Document Management (artifact transfer) ---

// DocumentProcessingArtifactsReady is published when DP finishes processing
// a document and sends all artifacts to DM for persistent storage.
type DocumentProcessingArtifactsReady struct {
	EventMeta
	JobID        string              `json:"job_id"`
	DocumentID   string              `json:"document_id"`
	OCRRaw       OCRRawArtifact      `json:"ocr_raw"`
	Text         ExtractedText       `json:"text"`
	Structure    DocumentStructure   `json:"structure"`
	SemanticTree SemanticTree        `json:"semantic_tree"`
	Warnings     []ProcessingWarning `json:"warnings,omitempty"`
}

// GetSemanticTreeRequest is published to request a semantic tree
// for a specific document version from DM (used in comparison pipeline).
type GetSemanticTreeRequest struct {
	EventMeta
	JobID      string `json:"job_id"`
	DocumentID string `json:"document_id"`
	VersionID  string `json:"version_id"`
}

// DocumentVersionDiffReady is published when DP finishes comparing
// two document versions and sends the diff result to DM.
type DocumentVersionDiffReady struct {
	EventMeta
	JobID               string                `json:"job_id"`
	DocumentID          string                `json:"document_id"`
	BaseVersionID       string                `json:"base_version_id"`
	TargetVersionID     string                `json:"target_version_id"`
	TextDiffs           []TextDiffEntry       `json:"text_diffs"`
	StructuralDiffs     []StructuralDiffEntry `json:"structural_diffs"`
	TextDiffCount       int                   `json:"text_diff_count"`
	StructuralDiffCount int                   `json:"structural_diff_count"`
}

// --- Events: Document Management → DP (responses) ---

// DocumentProcessingArtifactsPersisted confirms that DM successfully
// stored the processing artifacts.
type DocumentProcessingArtifactsPersisted struct {
	EventMeta
	JobID      string `json:"job_id"`
	DocumentID string `json:"document_id"`
}

// DocumentProcessingArtifactsPersistFailed indicates that DM failed
// to store the processing artifacts.
type DocumentProcessingArtifactsPersistFailed struct {
	EventMeta
	JobID        string `json:"job_id"`
	DocumentID   string `json:"document_id"`
	ErrorCode    string `json:"error_code,omitempty"`
	ErrorMessage string `json:"error_message"`
	IsRetryable  bool   `json:"is_retryable"`
}

// SemanticTreeProvided is the DM response containing the requested
// semantic tree for a document version (correlated by correlation_id).
type SemanticTreeProvided struct {
	EventMeta
	JobID        string       `json:"job_id"`
	DocumentID   string       `json:"document_id"`
	VersionID    string       `json:"version_id"`
	SemanticTree SemanticTree `json:"semantic_tree"`
}

// DocumentVersionDiffPersisted confirms that DM successfully
// stored the version comparison result.
type DocumentVersionDiffPersisted struct {
	EventMeta
	JobID      string `json:"job_id"`
	DocumentID string `json:"document_id"`
}

// DocumentVersionDiffPersistFailed indicates that DM failed
// to store the version comparison result.
type DocumentVersionDiffPersistFailed struct {
	EventMeta
	JobID        string `json:"job_id"`
	DocumentID   string `json:"document_id"`
	ErrorCode    string `json:"error_code,omitempty"`
	ErrorMessage string `json:"error_message"`
	IsRetryable  bool   `json:"is_retryable"`
}

// --- Events: DP → external consumers (status & completion) ---

// StatusChangedEvent is published on every job status transition.
// Stage is a string to accommodate both ProcessingStage and ComparisonStage.
type StatusChangedEvent struct {
	EventMeta
	JobID      string    `json:"job_id"`
	DocumentID string    `json:"document_id"`
	OldStatus  JobStatus `json:"old_status"`
	NewStatus  JobStatus `json:"new_status"`
	Stage      string    `json:"stage,omitempty"`
}

// ProcessingCompletedEvent is published when document processing
// finishes successfully (COMPLETED or COMPLETED_WITH_WARNINGS).
type ProcessingCompletedEvent struct {
	EventMeta
	JobID        string    `json:"job_id"`
	DocumentID   string    `json:"document_id"`
	Status       JobStatus `json:"status"`
	HasWarnings  bool      `json:"has_warnings"`
	WarningCount int       `json:"warning_count"`
}

// ComparisonCompletedEvent is published when version comparison
// finishes successfully (COMPLETED).
type ComparisonCompletedEvent struct {
	EventMeta
	JobID               string    `json:"job_id"`
	DocumentID          string    `json:"document_id"`
	BaseVersionID       string    `json:"base_version_id"`
	TargetVersionID     string    `json:"target_version_id"`
	Status              JobStatus `json:"status"`
	TextDiffCount       int       `json:"text_diff_count"`
	StructuralDiffCount int       `json:"structural_diff_count"`
}

// ProcessingFailedEvent is published when document processing
// fails (FAILED, TIMED_OUT, or REJECTED).
type ProcessingFailedEvent struct {
	EventMeta
	JobID         string    `json:"job_id"`
	DocumentID    string    `json:"document_id"`
	Status        JobStatus `json:"status"`
	ErrorCode     string    `json:"error_code"`
	ErrorMessage  string    `json:"error_message"`
	FailedAtStage string    `json:"failed_at_stage"`
	IsRetryable   bool      `json:"is_retryable"`
}

// ComparisonFailedEvent is published when version comparison
// fails (FAILED or TIMED_OUT).
type ComparisonFailedEvent struct {
	EventMeta
	JobID         string    `json:"job_id"`
	DocumentID    string    `json:"document_id"`
	Status        JobStatus `json:"status"`
	ErrorCode     string    `json:"error_code"`
	ErrorMessage  string    `json:"error_message"`
	FailedAtStage string    `json:"failed_at_stage"`
	IsRetryable   bool      `json:"is_retryable"`
}
