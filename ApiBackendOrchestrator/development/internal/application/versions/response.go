package versions

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

// processingStatusMessages maps user-facing processing_status to a Russian
// description string. Used in processing_status_message fields per NFR-5.2.
var processingStatusMessages = map[string]string{
	"UPLOADED":           "Договор загружен",
	"QUEUED":             "В очереди на обработку",
	"PROCESSING":         "Извлечение текста и структуры",
	"ANALYZING":          "Юридический анализ",
	"GENERATING_REPORTS": "Формирование отчётов",
	"READY":              "Результаты готовы",
	"PARTIALLY_FAILED":   "Частично доступно (есть ошибки)",
	"FAILED":             "Ошибка обработки",
	"ANALYSIS_FAILED":    "Ошибка юридического анализа",
	"REPORTS_FAILED":     "Ошибка формирования отчётов",
	"REJECTED":           "Файл отклонён (формат/размер)",
	"UNKNOWN":            "Статус неизвестен",
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

// ---------------------------------------------------------------------------
// Response DTOs
// ---------------------------------------------------------------------------

// VersionResponse is the version representation used in both list and get
// responses. It omits internal DM fields (source_file_key, artifact_status)
// that are not exposed to frontend users. This matches the OpenAPI
// VersionDetails schema.
type VersionResponse struct {
	VersionID               string  `json:"version_id"`
	ContractID              string  `json:"contract_id"`
	VersionNumber           int     `json:"version_number"`
	OriginType              string  `json:"origin_type"`
	OriginDescription       *string `json:"origin_description,omitempty"`
	ParentVersionID         *string `json:"parent_version_id,omitempty"`
	SourceFileName          string  `json:"source_file_name"`
	SourceFileSize          int64   `json:"source_file_size"`
	ProcessingStatus        string  `json:"processing_status"`
	ProcessingStatusMessage string  `json:"processing_status_message"`
	CreatedAt               string  `json:"created_at"`
}

// VersionListResponse is the paginated list envelope.
type VersionListResponse struct {
	Items []VersionResponse `json:"items"`
	Total int               `json:"total"`
	Page  int               `json:"page"`
	Size  int               `json:"size"`
}

// VersionStatusResponse is the lightweight status response for polling.
type VersionStatusResponse struct {
	VersionID string `json:"version_id"`
	Status    string `json:"status"`
	Message   string `json:"message"`
	UpdatedAt string `json:"updated_at"`
}

// VersionUploadResponse is the JSON body returned on successful version
// upload (202 Accepted).
type VersionUploadResponse struct {
	ContractID    string `json:"contract_id"`
	VersionID     string `json:"version_id"`
	VersionNumber int    `json:"version_number"`
	JobID         string `json:"job_id"`
	Status        string `json:"status"`
	Message       string `json:"message"`
}

// ---------------------------------------------------------------------------
// Mapping functions
// ---------------------------------------------------------------------------

func formatTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339)
}

// mapVersionToResponse converts a DM DocumentVersion to the user-facing DTO.
func mapVersionToResponse(v dmclient.DocumentVersion) VersionResponse {
	status := mapProcessingStatus(v.ArtifactStatus)
	return VersionResponse{
		VersionID:               v.VersionID,
		ContractID:              v.DocumentID,
		VersionNumber:           v.VersionNumber,
		OriginType:              v.OriginType,
		OriginDescription:       v.OriginDescription,
		ParentVersionID:         v.ParentVersionID,
		SourceFileName:          v.SourceFileName,
		SourceFileSize:          v.SourceFileSize,
		ProcessingStatus:        status,
		ProcessingStatusMessage: mapProcessingStatusMessage(status),
		CreatedAt:               formatTime(v.CreatedAt),
	}
}
