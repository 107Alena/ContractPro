package contracts

import (
	"time"

	"contractpro/api-orchestrator/internal/egress/dmclient"
)

// ---------------------------------------------------------------------------
// Processing status mapping
// ---------------------------------------------------------------------------

// processingStatusMap maps DM artifact_status to user-facing processing_status.
// The map is never mutated after package init.
var processingStatusMap = map[string]string{
	"PENDING_UPLOAD":         "UPLOADED",
	"PENDING_PROCESSING":     "QUEUED",
	"PROCESSING_IN_PROGRESS": "PROCESSING",
	"ARTIFACTS_READY":        "ANALYZING",
	"ANALYSIS_IN_PROGRESS":   "ANALYZING",
	"ANALYSIS_READY":         "GENERATING_REPORTS",
	"REPORTS_IN_PROGRESS":    "GENERATING_REPORTS",
	"FULLY_READY":            "READY",
	"PARTIALLY_AVAILABLE":    "PARTIALLY_FAILED",
	"PROCESSING_FAILED":      "FAILED",
	"REJECTED":               "REJECTED",
}

// mapProcessingStatus translates a DM artifact_status to user-facing
// processing_status. Returns "UNKNOWN" for unrecognized values.
func mapProcessingStatus(artifactStatus string) string {
	if s, ok := processingStatusMap[artifactStatus]; ok {
		return s
	}
	return "UNKNOWN"
}

// ---------------------------------------------------------------------------
// Response DTOs
// ---------------------------------------------------------------------------

// ContractSummary is the lightweight contract representation used in list,
// archive, and delete responses.
type ContractSummary struct {
	ContractID           string  `json:"contract_id"`
	Title                string  `json:"title"`
	Status               string  `json:"status"`
	CurrentVersionNumber *int    `json:"current_version_number"`
	ProcessingStatus     *string `json:"processing_status"`
	CreatedAt            string  `json:"created_at"`
	UpdatedAt            string  `json:"updated_at"`
}

// ContractVersion is the version representation embedded in ContractDetails.
// It omits internal fields (source_file_key, artifact_status) that are not
// exposed to frontend users.
type ContractVersion struct {
	VersionID          string  `json:"version_id"`
	ContractID         string  `json:"contract_id"`
	VersionNumber      int     `json:"version_number"`
	OriginType         string  `json:"origin_type"`
	OriginDescription  *string `json:"origin_description"`
	ParentVersionID    *string `json:"parent_version_id"`
	SourceFileName     string  `json:"source_file_name"`
	SourceFileSize     int64   `json:"source_file_size"`
	SourceFileChecksum string  `json:"source_file_checksum"`
	CreatedAt          string  `json:"created_at"`
	CreatedByUserID    string  `json:"created_by_user_id"`
}

// ContractDetails is the full contract representation used in get responses.
type ContractDetails struct {
	ContractID       string           `json:"contract_id"`
	Title            string           `json:"title"`
	Status           string           `json:"status"`
	CurrentVersion   *ContractVersion `json:"current_version"`
	ProcessingStatus *string          `json:"processing_status"`
	CreatedByUserID  string           `json:"created_by_user_id"`
	CreatedAt        string           `json:"created_at"`
	UpdatedAt        string           `json:"updated_at"`
}

// ContractListResponse is the paginated list envelope.
type ContractListResponse struct {
	Items []ContractSummary `json:"items"`
	Total int               `json:"total"`
	Page  int               `json:"page"`
	Size  int               `json:"size"`
}

// ---------------------------------------------------------------------------
// Mapping functions
// ---------------------------------------------------------------------------

func formatTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339)
}

// mapDocumentToContractSummary maps a DM Document to a lightweight
// ContractSummary. CurrentVersionNumber and ProcessingStatus are always nil
// because DM's Document type (used in list, archive, and delete responses)
// does not include version details. The GET endpoint provides full version
// info via mapDocumentWithVersionToContractDetails.
func mapDocumentToContractSummary(doc dmclient.Document) ContractSummary {
	return ContractSummary{
		ContractID:           doc.DocumentID,
		Title:                doc.Title,
		Status:               doc.Status,
		CurrentVersionNumber: nil,
		ProcessingStatus:     nil,
		CreatedAt:            formatTime(doc.CreatedAt),
		UpdatedAt:            formatTime(doc.UpdatedAt),
	}
}

func mapDocumentWithVersionToContractDetails(doc dmclient.DocumentWithCurrentVersion) ContractDetails {
	details := ContractDetails{
		ContractID:      doc.DocumentID,
		Title:           doc.Title,
		Status:          doc.Status,
		CreatedByUserID: doc.CreatedByUserID,
		CreatedAt:       formatTime(doc.CreatedAt),
		UpdatedAt:       formatTime(doc.UpdatedAt),
	}

	if doc.CurrentVersion != nil {
		cv := mapDocumentVersionToContractVersion(doc.CurrentVersion)
		details.CurrentVersion = &cv
		ps := mapProcessingStatus(doc.CurrentVersion.ArtifactStatus)
		details.ProcessingStatus = &ps
	}

	return details
}

func mapDocumentVersionToContractVersion(v *dmclient.DocumentVersion) ContractVersion {
	return ContractVersion{
		VersionID:          v.VersionID,
		ContractID:         v.DocumentID,
		VersionNumber:      v.VersionNumber,
		OriginType:         v.OriginType,
		OriginDescription:  v.OriginDescription,
		ParentVersionID:    v.ParentVersionID,
		SourceFileName:     v.SourceFileName,
		SourceFileSize:     v.SourceFileSize,
		SourceFileChecksum: v.SourceFileChecksum,
		CreatedAt:          formatTime(v.CreatedAt),
		CreatedByUserID:    v.CreatedByUserID,
	}
}
