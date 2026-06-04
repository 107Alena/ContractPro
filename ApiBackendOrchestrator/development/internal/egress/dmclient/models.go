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
//
// JobID is the Orchestrator-generated UUID v4 correlation key for the processing
// flow associated with the new version. DM persists it (DM-TASK-054) so that
// downstream DP events (ProcessDocumentRequested.job_id) and DM consumers can
// reconcile on a single identifier. Use omitempty for backward-compatibility
// with DM deployments that pre-date DM-TASK-054.
type CreateVersionRequest struct {
	JobID              string `json:"job_id,omitempty"`
	SourceFileKey      string `json:"source_file_key"`
	SourceFileName     string `json:"source_file_name"`
	SourceFileSize     int64  `json:"source_file_size"`
	SourceFileChecksum string `json:"source_file_checksum"`
	OriginType         string `json:"origin_type"`
	OriginDescription  string `json:"origin_description,omitempty"`
	ParentVersionID    string `json:"parent_version_id,omitempty"`
}

// ListDocumentsParams are query parameters for GET /documents.
//
// The first three fields (Page, Size, Status) are used by the plain
// ListDocuments call. The remaining fields are used only by
// ListDocumentsWithAnalysis (ORCH-TASK-056), which requests the DM
// list-aggregation read-contract (GET /documents?include=analysis): DM joins
// the CLASSIFICATION_RESULT and RISK_PROFILE artifacts of each document's
// current version and applies server-side filtering, sorting, and pagination.
// They are ignored by the plain ListDocuments call for backward-compatibility.
type ListDocumentsParams struct {
	Page   int    // default 1
	Size   int    // default 20
	Status string // optional filter: ACTIVE, ARCHIVED, DELETED

	// IncludeAnalysis requests the per-current-version analysis aggregate
	// (contract_type, risk_level, risk_counts) joined server-side by DM.
	IncludeAnalysis bool

	// RiskLevel filters by the aggregated risk level of the current version
	// (high|medium|low). Empty means no risk filter.
	RiskLevel string

	// ContractTypes filters by classification result (English LIC enum). An
	// empty slice means no contract-type filter; multiple values are OR-ed.
	ContractTypes []string

	// ArtifactStatuses filters by DM artifact_status of the current version.
	// The orchestrator derives these from the user-facing processing_status
	// filter. An empty slice means no status filter; multiple values are OR-ed.
	ArtifactStatuses []string

	// DateFrom / DateTo filter by the document's created_at timestamp
	// (inclusive bounds, ISO-8601). Empty means no lower/upper bound.
	DateFrom string
	DateTo   string

	// Sort selects the ordering field (date|title|risk). Empty means DM default.
	Sort string
	// Order selects ordering direction (asc|desc). Empty means DM default.
	Order string
}

// DocumentStatsParams are query parameters for GET /documents/stats
// (the DM count-by-artifact_status aggregate, DM-TASK-059).
type DocumentStatsParams struct {
	// IncludeArchived adds ARCHIVED documents to the counts. Default (false)
	// counts only ACTIVE documents; DELETED documents are never counted.
	IncludeArchived bool
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

// RiskCounts holds the per-severity risk counts of a version's RISK_PROFILE.
type RiskCounts struct {
	High   int `json:"high"`
	Medium int `json:"medium"`
	Low    int `json:"low"`
}

// DocumentAnalysisAggregate is the compact per-current-version analysis summary
// that DM computes (joins from CLASSIFICATION_RESULT + RISK_PROFILE) for the
// list-aggregation read-contract (ORCH-TASK-056, ASSUMPTION-ORCH-17). Every
// field is a pointer: a nil value means the datum is unknown (not yet produced,
// or the current version is not READY). A nil RiskCounts must NOT be treated as
// "zero risks" — it means "no result".
type DocumentAnalysisAggregate struct {
	ContractType *string     `json:"contract_type"`
	RiskLevel    *string     `json:"risk_level"`
	RiskCounts   *RiskCounts `json:"risk_counts"`
}

// DocumentWithAnalysis is a Document enriched with its current version and the
// analysis aggregate, returned by the DM list-aggregation read-contract
// (GET /documents?include=analysis). CurrentVersion and Analysis are nil when
// DM has no current version / no analysis for the document.
type DocumentWithAnalysis struct {
	Document
	CurrentVersion *DocumentVersion           `json:"current_version"`
	Analysis       *DocumentAnalysisAggregate `json:"analysis"`
}

// DocumentAnalysisList is a paginated list of documents enriched with analysis
// aggregates. Total reflects the FILTERED count (DM applies filters before
// pagination), so frontend pagination math stays correct.
type DocumentAnalysisList struct {
	Items []DocumentWithAnalysis `json:"items"`
	Total int                    `json:"total"`
	Page  int                    `json:"page"`
	Size  int                    `json:"size"`
}

// DocumentStats is the DM count-by-artifact_status aggregate (DM-TASK-059), the
// source of truth for the dashboard "in progress" metric (consumed by the
// orchestrator GET /contracts/stats, ORCH-TASK-057). Counts are over each
// document's CURRENT version, scoped to the organization (X-Organization-ID).
//
//   - ByArtifactStatus is keyed by DM-internal artifact_status (raw values such
//     as FULLY_READY, PROCESSING_IN_PROGRESS). DM returns them as-is; the
//     orchestrator maps them to the user-facing UserProcessingStatus.
//   - NotStarted counts documents WITHOUT a current version (disjoint from
//     ByArtifactStatus — a document is in exactly one of the two).
//   - Total is the DM document count in scope; the orchestrator recomputes its
//     own total from the mapped buckets and cross-checks against this value.
type DocumentStats struct {
	ByArtifactStatus map[string]int `json:"by_artifact_status"`
	NotStarted       int            `json:"not_started"`
	Total            int            `json:"total"`
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
