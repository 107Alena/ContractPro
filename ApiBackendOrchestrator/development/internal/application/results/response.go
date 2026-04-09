// Package results implements the analysis results aggregation handlers for the
// GET /api/v1/contracts/{contract_id}/versions/{version_id}/results,
// GET .../risks, GET .../summary, and GET .../recommendations endpoints.
//
// Artifacts are fetched from DM as opaque JSON (json.RawMessage) and passed
// through to the client without deserialization. The handler resolves the
// version's processing status before fetching artifacts. If the version is not
// in a terminal-with-data state (FULLY_READY or PARTIALLY_AVAILABLE), the
// handler returns 200 with the mapped status and null data fields.
package results

import "encoding/json"

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

// processingStatusMessages maps user-facing processing_status to a Russian
// description string. Used in status_message fields per NFR-5.2.
var processingStatusMessages = map[string]string{
	"UPLOADED":           "Договор загружен",
	"QUEUED":             "В очереди на обработку",
	"PROCESSING":         "Извлечение текста и структуры",
	"ANALYZING":          "Юридический анализ",
	"GENERATING_REPORTS": "Формирование отчётов",
	"READY":              "Анализ завершён",
	"PARTIALLY_FAILED":   "Частично доступно (есть ошибки)",
	"FAILED":             "Ошибка обработки",
	"REJECTED":           "Файл отклонён (формат/размер)",
	"UNKNOWN":            "Статус неизвестен",
}

// dataAvailableStatuses defines DM artifact_status values that indicate
// artifacts may exist and should be fetched. All other statuses mean
// processing is still in progress or has failed without producing data.
var dataAvailableStatuses = map[string]struct{}{
	"FULLY_READY":        {},
	"PARTIALLY_AVAILABLE": {},
}

// mapProcessingStatus translates a DM artifact_status to user-facing status.
// Returns "UNKNOWN" for unrecognized values.
func mapProcessingStatus(artifactStatus string) string {
	if s, ok := processingStatusMap[artifactStatus]; ok {
		return s
	}
	return "UNKNOWN"
}

// mapProcessingStatusMessage returns the Russian description for a user-facing
// processing status.
func mapProcessingStatusMessage(status string) string {
	if msg, ok := processingStatusMessages[status]; ok {
		return msg
	}
	return processingStatusMessages["UNKNOWN"]
}

// isDataAvailable returns true if the DM artifact_status indicates that
// artifacts may exist and should be fetched.
func isDataAvailable(artifactStatus string) bool {
	_, ok := dataAvailableStatuses[artifactStatus]
	return ok
}

// ---------------------------------------------------------------------------
// Artifact type constants
// ---------------------------------------------------------------------------

const (
	ArtifactRiskAnalysis        = "RISK_ANALYSIS"
	ArtifactRiskProfile         = "RISK_PROFILE"
	ArtifactSummary             = "SUMMARY"
	ArtifactRecommendations     = "RECOMMENDATIONS"
	ArtifactKeyParameters       = "KEY_PARAMETERS"
	ArtifactClassificationResult = "CLASSIFICATION_RESULT"
	ArtifactAggregateScore      = "AGGREGATE_SCORE"
)

// artifactTypesResults lists the artifact types fetched for GET .../results.
var artifactTypesResults = []string{
	ArtifactRiskAnalysis,
	ArtifactRiskProfile,
	ArtifactSummary,
	ArtifactRecommendations,
	ArtifactKeyParameters,
	ArtifactClassificationResult,
	ArtifactAggregateScore,
}

// artifactTypesRisks lists the artifact types fetched for GET .../risks.
var artifactTypesRisks = []string{
	ArtifactRiskAnalysis,
	ArtifactRiskProfile,
}

// artifactTypesSummary lists the artifact types fetched for GET .../summary.
var artifactTypesSummary = []string{
	ArtifactSummary,
	ArtifactAggregateScore,
	ArtifactKeyParameters,
}

// artifactTypesRecommendations lists the artifact types fetched for GET .../recommendations.
var artifactTypesRecommendations = []string{
	ArtifactRecommendations,
}

// ---------------------------------------------------------------------------
// Response DTOs
// ---------------------------------------------------------------------------

// AnalysisResultsResponse is the response for GET .../results.
// All artifact fields use json.RawMessage for pass-through from DM.
// Null fields indicate that the artifact is not (yet) available.
type AnalysisResultsResponse struct {
	VersionID     string          `json:"version_id"`
	Status        string          `json:"status"`
	StatusMessage string          `json:"status_message"`
	ContractType  json.RawMessage `json:"contract_type"`
	RiskProfile   json.RawMessage `json:"risk_profile"`
	Risks         json.RawMessage `json:"risks"`
	Recommendations json.RawMessage `json:"recommendations"`
	Summary       json.RawMessage `json:"summary"`
	AggregateScore json.RawMessage `json:"aggregate_score"`
	KeyParameters json.RawMessage `json:"key_parameters"`
}

// RiskListResponse is the response for GET .../risks.
type RiskListResponse struct {
	VersionID   string          `json:"version_id"`
	Status      string          `json:"status"`
	StatusMessage string        `json:"status_message"`
	Risks       json.RawMessage `json:"risks"`
	RiskProfile json.RawMessage `json:"risk_profile"`
}

// SummaryResponse is the response for GET .../summary.
type SummaryResponse struct {
	VersionID      string          `json:"version_id"`
	Status         string          `json:"status"`
	StatusMessage  string          `json:"status_message"`
	Summary        json.RawMessage `json:"summary"`
	AggregateScore json.RawMessage `json:"aggregate_score"`
	KeyParameters  json.RawMessage `json:"key_parameters"`
}

// RecommendationsResponse is the response for GET .../recommendations.
type RecommendationsResponse struct {
	VersionID       string          `json:"version_id"`
	Status          string          `json:"status"`
	StatusMessage   string          `json:"status_message"`
	Recommendations json.RawMessage `json:"recommendations"`
}
