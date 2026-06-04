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
// ContractStats (ORCH-TASK-057)
// ---------------------------------------------------------------------------

// ProcessingStatusCounts holds the per-UserProcessingStatus document counts for
// the dashboard. The JSON keys are the snake_case form of the UserProcessingStatus
// enum; their sum equals ContractStats.Total. not_started counts documents
// without a current version or processing status.
type ProcessingStatusCounts struct {
	Uploaded          int `json:"uploaded"`
	Queued            int `json:"queued"`
	Processing        int `json:"processing"`
	Analyzing         int `json:"analyzing"`
	AwaitingUserInput int `json:"awaiting_user_input"`
	GeneratingReports int `json:"generating_reports"`
	Ready             int `json:"ready"`
	PartiallyFailed   int `json:"partially_failed"`
	Failed            int `json:"failed"`
	Rejected          int `json:"rejected"`
	NotStarted        int `json:"not_started"`
}

// sum returns the total across all processing-status buckets. By construction it
// equals ContractStats.Total (every source count lands in exactly one bucket).
func (c ProcessingStatusCounts) sum() int {
	return c.Uploaded + c.Queued + c.Processing + c.Analyzing + c.AwaitingUserInput +
		c.GeneratingReports + c.Ready + c.PartiallyFailed + c.Failed + c.Rejected + c.NotStarted
}

// RiskLevelCounts holds the per-risk-level counts of READY documents.
type RiskLevelCounts struct {
	High    int `json:"high"`
	Medium  int `json:"medium"`
	Low     int `json:"low"`
	Unknown int `json:"unknown"`
}

// ContractStats is the dashboard aggregate response for GET /contracts/stats.
//
// ByRiskLevel is a pointer so a nil value serializes as JSON null (not {}): the
// risk breakdown is unavailable in this increment because the DM stats
// read-contract provides no risk aggregation (ASSUMPTION-ORCH-18). UpdatedAt is
// the response-assembly time (computed-at, not data-freshness).
type ContractStats struct {
	Total              int                    `json:"total"`
	ByProcessingStatus ProcessingStatusCounts `json:"by_processing_status"`
	ByRiskLevel        *RiskLevelCounts       `json:"by_risk_level"`
	UpdatedAt          string                 `json:"updated_at"`
}

// addProcessingCount adds n to the bucket for the given user-facing
// processing_status and reports whether the status was recognized. An
// unrecognized status (mapProcessingStatus → "UNKNOWN") returns false so the
// caller can route the stray count to the closest in-flight bucket and warn.
func (c *ProcessingStatusCounts) addProcessingCount(userStatus string, n int) bool {
	switch userStatus {
	case "UPLOADED":
		c.Uploaded += n
	case "QUEUED":
		c.Queued += n
	case "PROCESSING":
		c.Processing += n
	case "ANALYZING":
		c.Analyzing += n
	case "AWAITING_USER_INPUT":
		c.AwaitingUserInput += n
	case "GENERATING_REPORTS":
		c.GeneratingReports += n
	case "READY":
		c.Ready += n
	case "PARTIALLY_FAILED":
		c.PartiallyFailed += n
	case "FAILED":
		c.Failed += n
	case "REJECTED":
		c.Rejected += n
	default:
		return false
	}
	return true
}

// mapDocumentStatsToContractStats maps the DM count-by-artifact_status aggregate
// to the user-facing ContractStats (ORCH-TASK-057).
//
// Each DM artifact_status count is translated to a UserProcessingStatus
// (processingStatusMap) and accumulated into the matching bucket; documents
// without a current version (DM not_started) land in not_started. An
// unrecognized DM artifact_status — which would otherwise have no bucket — is
// routed to processing (the closest "in flight" state, never not_started, which
// has a distinct dashboard meaning) and its raw value is returned in
// unknownStatuses so the caller can warn about DM/Orchestrator enum drift.
//
// Total is recomputed from the buckets (not trusted from DM) so the
// sum(by_processing_status) == total invariant holds by construction;
// totalMismatch reports whether the recomputed total diverged from the DM total
// (a correctness canary worth logging).
func mapDocumentStatsToContractStats(stats dmclient.DocumentStats, now time.Time) (result ContractStats, unknownStatuses []string, totalMismatch bool) {
	var counts ProcessingStatusCounts

	// Deterministic iteration order so warnings/tests are stable.
	artifactStatuses := make([]string, 0, len(stats.ByArtifactStatus))
	for s := range stats.ByArtifactStatus {
		artifactStatuses = append(artifactStatuses, s)
	}
	sort.Strings(artifactStatuses)

	for _, artifactStatus := range artifactStatuses {
		n := stats.ByArtifactStatus[artifactStatus]
		userStatus := mapProcessingStatus(artifactStatus)
		if !counts.addProcessingCount(userStatus, n) {
			// Enum drift: keep sum == total by routing to the closest in-flight
			// bucket, and surface the raw value for a warning.
			counts.Processing += n
			unknownStatuses = append(unknownStatuses, artifactStatus)
		}
	}
	counts.NotStarted += stats.NotStarted

	result = ContractStats{
		Total:              counts.sum(),
		ByProcessingStatus: counts,
		ByRiskLevel:        nil, // no risk-aggregation source yet (ASSUMPTION-ORCH-18)
		UpdatedAt:          now.UTC().Format(time.RFC3339),
	}
	totalMismatch = result.Total != stats.Total
	return result, unknownStatuses, totalMismatch
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
