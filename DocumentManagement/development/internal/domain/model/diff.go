package model

import "time"

// VersionDiffReference represents a reference to the result of comparing two document versions.
type VersionDiffReference struct {
	DiffID              string    `json:"diff_id"`
	DocumentID          string    `json:"document_id"`
	OrganizationID      string    `json:"organization_id"`
	BaseVersionID       string    `json:"base_version_id"`
	TargetVersionID     string    `json:"target_version_id"`
	StorageKey          string    `json:"storage_key"`
	TextDiffCount       int       `json:"text_diff_count"`
	StructuralDiffCount int       `json:"structural_diff_count"`
	JobID               string    `json:"job_id"`
	CorrelationID       string    `json:"correlation_id"`
	CreatedAt           time.Time `json:"created_at"`
}

// NewVersionDiffReference creates a new VersionDiffReference.
func NewVersionDiffReference(
	diffID, documentID, organizationID, baseVersionID, targetVersionID string,
	storageKey string,
	textDiffCount, structuralDiffCount int,
	jobID, correlationID string,
) *VersionDiffReference {
	return &VersionDiffReference{
		DiffID:              diffID,
		DocumentID:          documentID,
		OrganizationID:      organizationID,
		BaseVersionID:       baseVersionID,
		TargetVersionID:     targetVersionID,
		StorageKey:          storageKey,
		TextDiffCount:       textDiffCount,
		StructuralDiffCount: structuralDiffCount,
		JobID:               jobID,
		CorrelationID:       correlationID,
		CreatedAt:           time.Now().UTC(),
	}
}
