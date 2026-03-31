package model

import (
	"fmt"
	"time"
)

// OriginType represents the source of a version creation.
type OriginType string

const (
	OriginTypeUpload                OriginType = "UPLOAD"
	OriginTypeReUpload              OriginType = "RE_UPLOAD"
	OriginTypeRecommendationApplied OriginType = "RECOMMENDATION_APPLIED"
	OriginTypeManualEdit            OriginType = "MANUAL_EDIT"
	OriginTypeReCheck               OriginType = "RE_CHECK"
)

// AllOriginTypes returns all valid origin types.
var AllOriginTypes = []OriginType{
	OriginTypeUpload,
	OriginTypeReUpload,
	OriginTypeRecommendationApplied,
	OriginTypeManualEdit,
	OriginTypeReCheck,
}

// ArtifactStatus represents the artifact readiness state of a document version.
type ArtifactStatus string

const (
	ArtifactStatusPending                      ArtifactStatus = "PENDING"
	ArtifactStatusProcessingArtifactsReceived  ArtifactStatus = "PROCESSING_ARTIFACTS_RECEIVED"
	ArtifactStatusAnalysisArtifactsReceived    ArtifactStatus = "ANALYSIS_ARTIFACTS_RECEIVED"
	ArtifactStatusReportsReady                 ArtifactStatus = "REPORTS_READY"
	ArtifactStatusFullyReady                   ArtifactStatus = "FULLY_READY"
	ArtifactStatusPartiallyAvailable           ArtifactStatus = "PARTIALLY_AVAILABLE"
)

// AllArtifactStatuses returns all valid artifact statuses.
var AllArtifactStatuses = []ArtifactStatus{
	ArtifactStatusPending,
	ArtifactStatusProcessingArtifactsReceived,
	ArtifactStatusAnalysisArtifactsReceived,
	ArtifactStatusReportsReady,
	ArtifactStatusFullyReady,
	ArtifactStatusPartiallyAvailable,
}

// allowedArtifactTransitions defines the state machine for artifact status.
var allowedArtifactTransitions = map[ArtifactStatus][]ArtifactStatus{
	ArtifactStatusPending: {
		ArtifactStatusProcessingArtifactsReceived,
		ArtifactStatusPartiallyAvailable,
	},
	ArtifactStatusProcessingArtifactsReceived: {
		ArtifactStatusAnalysisArtifactsReceived,
		ArtifactStatusPartiallyAvailable,
	},
	ArtifactStatusAnalysisArtifactsReceived: {
		ArtifactStatusReportsReady,
		ArtifactStatusFullyReady,
		ArtifactStatusPartiallyAvailable,
	},
	ArtifactStatusReportsReady: {
		ArtifactStatusFullyReady,
		ArtifactStatusPartiallyAvailable,
	},
	ArtifactStatusFullyReady:         {},
	ArtifactStatusPartiallyAvailable: {},
}

// ValidateArtifactTransition checks whether the transition from → to is allowed.
func ValidateArtifactTransition(from, to ArtifactStatus) error {
	targets, ok := allowedArtifactTransitions[from]
	if !ok {
		return fmt.Errorf("no transitions allowed from terminal status %s", from)
	}
	for _, t := range targets {
		if t == to {
			return nil
		}
	}
	return fmt.Errorf("invalid artifact status transition from %s to %s", from, to)
}

// IsTerminal returns true if the artifact status has no outgoing transitions.
func (s ArtifactStatus) IsTerminal() bool {
	targets, ok := allowedArtifactTransitions[s]
	return !ok || len(targets) == 0
}

// DocumentVersion represents an immutable snapshot of a document at a specific point in time.
type DocumentVersion struct {
	VersionID        string         `json:"version_id"`
	DocumentID       string         `json:"document_id"`
	OrganizationID   string         `json:"organization_id"`
	VersionNumber    int            `json:"version_number"`
	ParentVersionID  string         `json:"parent_version_id,omitempty"`
	OriginType       OriginType     `json:"origin_type"`
	OriginDescription string        `json:"origin_description,omitempty"`
	SourceFileKey    string         `json:"source_file_key"`
	SourceFileName   string         `json:"source_file_name"`
	SourceFileSize   int64          `json:"source_file_size"`
	SourceFileChecksum string       `json:"source_file_checksum"`
	ArtifactStatus   ArtifactStatus `json:"artifact_status"`
	CreatedByUserID  string         `json:"created_by_user_id"`
	CreatedAt        time.Time      `json:"created_at"`
}

// NewDocumentVersion creates a new DocumentVersion in PENDING artifact status.
func NewDocumentVersion(
	versionID, documentID, organizationID string,
	versionNumber int,
	originType OriginType,
	sourceFileKey, sourceFileName string,
	sourceFileSize int64,
	sourceFileChecksum, createdByUserID string,
) *DocumentVersion {
	return &DocumentVersion{
		VersionID:          versionID,
		DocumentID:         documentID,
		OrganizationID:     organizationID,
		VersionNumber:      versionNumber,
		OriginType:         originType,
		SourceFileKey:      sourceFileKey,
		SourceFileName:     sourceFileName,
		SourceFileSize:     sourceFileSize,
		SourceFileChecksum: sourceFileChecksum,
		ArtifactStatus:     ArtifactStatusPending,
		CreatedByUserID:    createdByUserID,
		CreatedAt:          time.Now().UTC(),
	}
}

// TransitionArtifactStatus transitions the artifact status if the transition is valid.
func (v *DocumentVersion) TransitionArtifactStatus(newStatus ArtifactStatus) error {
	if err := ValidateArtifactTransition(v.ArtifactStatus, newStatus); err != nil {
		return err
	}
	v.ArtifactStatus = newStatus
	return nil
}
