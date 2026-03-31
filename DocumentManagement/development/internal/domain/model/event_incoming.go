package model

import "encoding/json"

// --- Incoming events: DP → DM ---

// DocumentProcessingArtifactsReady is received when DP finishes processing
// a document and sends all artifacts to DM for persistent storage.
// Artifact contents are stored as json.RawMessage because DM treats them
// as opaque blobs — it persists without interpreting the content.
type DocumentProcessingArtifactsReady struct {
	EventMeta
	JobID        string          `json:"job_id"`
	DocumentID   string          `json:"document_id"`
	VersionID    string          `json:"version_id"`
	OrgID        string          `json:"organization_id,omitempty"`
	OCRRaw       json.RawMessage `json:"ocr_raw"`
	Text         json.RawMessage `json:"text"`
	Structure    json.RawMessage `json:"structure"`
	SemanticTree json.RawMessage `json:"semantic_tree"`
	Warnings     json.RawMessage `json:"warnings,omitempty"`
}

// GetSemanticTreeRequest is received from DP to request a semantic tree
// for a specific document version (used in comparison pipeline).
type GetSemanticTreeRequest struct {
	EventMeta
	JobID      string `json:"job_id"`
	DocumentID string `json:"document_id"`
	VersionID  string `json:"version_id"`
	OrgID      string `json:"organization_id,omitempty"`
}

// DocumentVersionDiffReady is received when DP finishes comparing two
// document versions and sends the diff result to DM for storage.
// Diff content is stored as json.RawMessage for opaque persistence.
type DocumentVersionDiffReady struct {
	EventMeta
	JobID               string          `json:"job_id"`
	DocumentID          string          `json:"document_id"`
	BaseVersionID       string          `json:"base_version_id"`
	TargetVersionID     string          `json:"target_version_id"`
	OrgID               string          `json:"organization_id,omitempty"`
	TextDiffs           json.RawMessage `json:"text_diffs"`
	StructuralDiffs     json.RawMessage `json:"structural_diffs"`
	TextDiffCount       int             `json:"text_diff_count"`
	StructuralDiffCount int             `json:"structural_diff_count"`
}

// --- Incoming events: LIC / RE → DM ---

// GetArtifactsRequest is received from LIC or RE to request specific
// artifacts for a document version.
type GetArtifactsRequest struct {
	EventMeta
	JobID         string         `json:"job_id"`
	DocumentID    string         `json:"document_id"`
	VersionID     string         `json:"version_id"`
	OrgID         string         `json:"organization_id,omitempty"`
	ArtifactTypes []ArtifactType `json:"artifact_types"`
}

// LegalAnalysisArtifactsReady is received when LIC finishes legal analysis
// and sends all analysis artifacts to DM for persistent storage.
// Each artifact is json.RawMessage because DM stores them opaquely.
type LegalAnalysisArtifactsReady struct {
	EventMeta
	JobID                string          `json:"job_id"`
	DocumentID           string          `json:"document_id"`
	VersionID            string          `json:"version_id"`
	OrgID                string          `json:"organization_id,omitempty"`
	ClassificationResult json.RawMessage `json:"classification_result"`
	KeyParameters        json.RawMessage `json:"key_parameters"`
	RiskAnalysis         json.RawMessage `json:"risk_analysis"`
	RiskProfile          json.RawMessage `json:"risk_profile"`
	Recommendations      json.RawMessage `json:"recommendations"`
	Summary              json.RawMessage `json:"summary"`
	DetailedReport       json.RawMessage `json:"detailed_report"`
	AggregateScore       json.RawMessage `json:"aggregate_score"`
}

// ReportsArtifactsReady is received when RE finishes generating export
// reports. Uses the claim-check pattern: binary files are uploaded to
// Object Storage before the event; the event contains only references.
// At least one of ExportPDF or ExportDOCX must be present.
type ReportsArtifactsReady struct {
	EventMeta
	JobID      string         `json:"job_id"`
	DocumentID string         `json:"document_id"`
	VersionID  string         `json:"version_id"`
	OrgID      string         `json:"organization_id,omitempty"`
	ExportPDF  *BlobReference `json:"export_pdf,omitempty"`
	ExportDOCX *BlobReference `json:"export_docx,omitempty"`
}
