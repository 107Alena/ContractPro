package contracts

import (
	"sort"
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

// statusReady is the user-facing processing_status that signals the analysis is
// complete and risk aggregates are meaningful (ORCH-TASK-056).
const statusReady = "READY"

// reverseProcessingStatusMap maps a user-facing processing_status to the set of
// DM artifact_status values it expands to. It is derived from
// processingStatusMap at package init (single source of truth), so it never
// drifts when the forward map changes. A user status that maps from several
// artifact_status values (e.g. ANALYZING ← ARTIFACTS_READY, ANALYSIS_IN_PROGRESS)
// expands back to all of them; the orchestrator OR-s them in the DM filter.
//
// AWAITING_USER_INPUT is intentionally absent: it is an orchestrator-managed
// status with no DM artifact_status equivalent, so it cannot be filtered DM-side
// in this increment (the handler rejects it with 400 rather than silently
// returning unfiltered data).
var reverseProcessingStatusMap = func() map[string][]string {
	out := make(map[string][]string)
	for artifactStatus, userStatus := range processingStatusMap {
		out[userStatus] = append(out[userStatus], artifactStatus)
	}
	// Deterministic order for stable query strings and tests.
	for _, v := range out {
		sort.Strings(v)
	}
	return out
}()

// artifactStatusesForUserStatus returns the DM artifact_status values that map
// to the given user-facing processing_status, and whether the status is
// supported for DM-side filtering. Unsupported (e.g. AWAITING_USER_INPUT) →
// false.
func artifactStatusesForUserStatus(userStatus string) ([]string, bool) {
	v, ok := reverseProcessingStatusMap[userStatus]
	return v, ok
}

// ---------------------------------------------------------------------------
// Response DTOs
// ---------------------------------------------------------------------------

// RiskCounts holds the per-severity risk counts of the current version's
// RISK_PROFILE. A nil *RiskCounts on ContractSummary means "no result" — it is
// NOT the same as {high:0, medium:0, low:0} ("no risks found").
type RiskCounts struct {
	High   int `json:"high"`
	Medium int `json:"medium"`
	Low    int `json:"low"`
}

// ContractSummary is the lightweight contract representation used in list,
// archive, and delete responses.
//
// ContractType, RiskLevel, and RiskCounts (ORCH-TASK-056) are aggregated from
// the current version's DM artifacts and are populated only by the list
// endpoint when list-aggregation is enabled. They are nullable/optional: a nil
// value serializes as JSON null (no omitempty) so existing clients that ignore
// them are unaffected, and the archive/delete responses (which use a different
// mapper) always emit them as null.
type ContractSummary struct {
	ContractID           string      `json:"contract_id"`
	Title                string      `json:"title"`
	Status               string      `json:"status"`
	CurrentVersionNumber *int        `json:"current_version_number"`
	ProcessingStatus     *string     `json:"processing_status"`
	ContractType         *string     `json:"contract_type"`
	RiskLevel            *string     `json:"risk_level"`
	RiskCounts           *RiskCounts `json:"risk_counts"`
	CreatedAt            string      `json:"created_at"`
	UpdatedAt            string      `json:"updated_at"`
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

// mapDocumentWithAnalysisToContractSummary maps a DM DocumentWithAnalysis (from
// the list-aggregation read-contract) to a ContractSummary, populating the
// current-version fields and the analysis aggregate (ORCH-TASK-056).
//
// Null semantics:
//   - No current version → current_version_number / processing_status null.
//   - contract_type is passed through from DM (may be known before READY).
//   - risk_level / risk_counts are force-nulled by the orchestrator unless the
//     current version's processing_status is READY. This keeps the row-level
//     contract consistent with the READY-only definition of risk used by
//     GET /contracts/stats (by_risk_level) and avoids a contradiction where a
//     row filtered in by risk_level would display a stale/partial risk.
func mapDocumentWithAnalysisToContractSummary(doc dmclient.DocumentWithAnalysis) ContractSummary {
	cs := ContractSummary{
		ContractID: doc.DocumentID,
		Title:      doc.Title,
		Status:     doc.Status,
		CreatedAt:  formatTime(doc.CreatedAt),
		UpdatedAt:  formatTime(doc.UpdatedAt),
	}

	isReady := false
	if doc.CurrentVersion != nil {
		n := doc.CurrentVersion.VersionNumber
		cs.CurrentVersionNumber = &n
		ps := mapProcessingStatus(doc.CurrentVersion.ArtifactStatus)
		cs.ProcessingStatus = &ps
		isReady = ps == statusReady
	}

	if doc.Analysis != nil {
		cs.ContractType = doc.Analysis.ContractType
		// Risk is meaningful only for a READY current version.
		if isReady {
			cs.RiskLevel = doc.Analysis.RiskLevel
			if doc.Analysis.RiskCounts != nil {
				cs.RiskCounts = &RiskCounts{
					High:   doc.Analysis.RiskCounts.High,
					Medium: doc.Analysis.RiskCounts.Medium,
					Low:    doc.Analysis.RiskCounts.Low,
				}
			}
		}
	}

	return cs
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
