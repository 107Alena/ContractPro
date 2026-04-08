package dmclient

import (
	"encoding/json"
	"time"
)

// ---------------------------------------------------------------------------
// Request models
// ---------------------------------------------------------------------------

// CreateDocumentRequest is the payload for POST /documents.
type CreateDocumentRequest struct {
	Title string `json:"title"`
}

// CreateVersionRequest is the payload for POST /documents/{document_id}/versions.
type CreateVersionRequest struct {
	SourceFileKey      string `json:"source_file_key"`
	SourceFileName     string `json:"source_file_name"`
	SourceFileSize     int64  `json:"source_file_size"`
	SourceFileChecksum string `json:"source_file_checksum"`
	OriginType         string `json:"origin_type"`
	OriginDescription  string `json:"origin_description,omitempty"`
	ParentVersionID    string `json:"parent_version_id,omitempty"`
}

// ListDocumentsParams are query parameters for GET /documents.
type ListDocumentsParams struct {
	Page   int    // default 1
	Size   int    // default 20
	Status string // optional filter: ACTIVE, ARCHIVED, DELETED
}

// ListVersionsParams are query parameters for GET /documents/{document_id}/versions.
type ListVersionsParams struct {
	Page int // default 1
	Size int // default 20
}

// ListArtifactsParams are query parameters for GET /documents/{document_id}/versions/{version_id}/artifacts.
type ListArtifactsParams struct {
	ArtifactType   string // optional filter
	ProducerDomain string // optional filter
}

// ListAuditParams are query parameters for GET /audit.
type ListAuditParams struct {
	DocumentID string // optional filter
	VersionID  string // optional filter
	Action     string // optional filter
	ActorID    string // optional filter
	From       string // optional, RFC3339 datetime
	To         string // optional, RFC3339 datetime
	Page       int    // default 1
	Size       int    // default 20
}

// ---------------------------------------------------------------------------
// Response models
// ---------------------------------------------------------------------------

// Document represents a DM document resource.
type Document struct {
	DocumentID       string    `json:"document_id"`
	OrganizationID   string    `json:"organization_id"`
	Title            string    `json:"title"`
	CurrentVersionID *string   `json:"current_version_id"`
	Status           string    `json:"status"`
	CreatedByUserID  string    `json:"created_by_user_id"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

// DocumentWithCurrentVersion is a Document that embeds the current version.
type DocumentWithCurrentVersion struct {
	Document
	CurrentVersion *DocumentVersion `json:"current_version"`
}

// DocumentList is a paginated list of documents.
type DocumentList struct {
	Items []Document `json:"items"`
	Total int        `json:"total"`
	Page  int        `json:"page"`
	Size  int        `json:"size"`
}

// DocumentVersion represents a DM document version resource.
type DocumentVersion struct {
	VersionID          string    `json:"version_id"`
	DocumentID         string    `json:"document_id"`
	VersionNumber      int       `json:"version_number"`
	ParentVersionID    *string   `json:"parent_version_id"`
	OriginType         string    `json:"origin_type"`
	OriginDescription  *string   `json:"origin_description"`
	SourceFileKey      string    `json:"source_file_key"`
	SourceFileName     string    `json:"source_file_name"`
	SourceFileSize     int64     `json:"source_file_size"`
	SourceFileChecksum string    `json:"source_file_checksum"`
	ArtifactStatus     string    `json:"artifact_status"`
	CreatedByUserID    string    `json:"created_by_user_id"`
	CreatedAt          time.Time `json:"created_at"`
}

// DocumentVersionWithArtifacts is a DocumentVersion that embeds artifact descriptors.
type DocumentVersionWithArtifacts struct {
	DocumentVersion
	Artifacts []ArtifactDescriptor `json:"artifacts"`
}

// VersionList is a paginated list of document versions.
type VersionList struct {
	Items []DocumentVersion `json:"items"`
	Total int               `json:"total"`
	Page  int               `json:"page"`
	Size  int               `json:"size"`
}

// ArtifactDescriptor describes a single artifact stored by DM.
type ArtifactDescriptor struct {
	ArtifactID     string    `json:"artifact_id"`
	VersionID      string    `json:"version_id"`
	ArtifactType   string    `json:"artifact_type"`
	ProducerDomain string    `json:"producer_domain"`
	SizeBytes      int64     `json:"size_bytes"`
	ContentHash    string    `json:"content_hash"`
	SchemaVersion  string    `json:"schema_version"`
	CreatedAt      time.Time `json:"created_at"`
}

// ArtifactDescriptorList is a list of artifact descriptors (not paginated per DM spec).
type ArtifactDescriptorList struct {
	Items []ArtifactDescriptor `json:"items"`
}

// ArtifactResponse represents the result of GetArtifact. For JSON artifacts
// (HTTP 200), Content holds the raw JSON body. For blob artifacts (HTTP 302),
// RedirectURL holds the presigned S3 URL from the Location header.
type ArtifactResponse struct {
	// Content is the raw JSON body for JSON-type artifacts (200 response).
	// Nil for redirect (302) responses.
	Content json.RawMessage

	// RedirectURL is the presigned URL from the Location header (302 response).
	// Empty for JSON (200) responses.
	RedirectURL string
}

// TextDiff is a single text-level difference in a version comparison.
type TextDiff struct {
	Type    string  `json:"type"` // added, removed, modified
	Path    string  `json:"path"`
	OldText *string `json:"old_text"`
	NewText *string `json:"new_text"`
}

// StructuralDiff is a single structural-level difference in a version comparison.
type StructuralDiff struct {
	Type     string          `json:"type"` // added, removed, modified, moved
	NodeID   string          `json:"node_id"`
	OldValue json.RawMessage `json:"old_value"`
	NewValue json.RawMessage `json:"new_value"`
}

// VersionDiff represents the result of comparing two document versions.
type VersionDiff struct {
	DiffID              string           `json:"diff_id"`
	DocumentID          string           `json:"document_id"`
	BaseVersionID       string           `json:"base_version_id"`
	TargetVersionID     string           `json:"target_version_id"`
	TextDiffCount       int              `json:"text_diff_count"`
	StructuralDiffCount int              `json:"structural_diff_count"`
	TextDiffs           []TextDiff       `json:"text_diffs"`
	StructuralDiffs     []StructuralDiff `json:"structural_diffs"`
	CreatedAt           time.Time        `json:"created_at"`
}

// AuditRecord represents a single audit log entry from DM.
type AuditRecord struct {
	AuditID       string          `json:"audit_id"`
	DocumentID    *string         `json:"document_id"`
	VersionID     *string         `json:"version_id"`
	Action        string          `json:"action"`
	ActorType     string          `json:"actor_type"`
	ActorID       string          `json:"actor_id"`
	JobID         *string         `json:"job_id"`
	CorrelationID *string         `json:"correlation_id"`
	Details       json.RawMessage `json:"details"`
	CreatedAt     time.Time       `json:"created_at"`
}

// AuditRecordList is a paginated list of audit records.
type AuditRecordList struct {
	Items []AuditRecord `json:"items"`
	Total int           `json:"total"`
	Page  int           `json:"page"`
	Size  int           `json:"size"`
}
