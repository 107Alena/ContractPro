package model

import "time"

// PipelineMode is the internal binary mode of an analysis pipeline run,
// derived from VersionCreated.parent_version_id (ASSUMPTION-LIC-02).
//
//	INITIAL  — parent_version_id is null (first analysis of the document).
//	RE_CHECK — parent_version_id is set; Agent 9 (Risk Delta) is candidate.
//
// The 5-value DM origin_type enum (UPLOAD/RE_UPLOAD/RECOMMENDATION_APPLIED/
// MANUAL_EDIT/RE_CHECK) is carried as an opaque string on PipelineState — it
// surfaces in DETAILED_REPORT.metadata.origin_type for the UI but does NOT
// gate Agent 9.
type PipelineMode string

const (
	PipelineModeInitial PipelineMode = "INITIAL"
	PipelineModeReCheck PipelineMode = "RE_CHECK"
)

// IsValid reports whether m is a known PipelineMode value.
func (m PipelineMode) IsValid() bool {
	switch m {
	case PipelineModeInitial, PipelineModeReCheck:
		return true
	default:
		return false
	}
}

// PipelineState is the per-job in-memory state of an analysis pipeline run.
// It is owned by the Pipeline Orchestrator goroutine; concurrent reads/writes
// across goroutines are NOT supported (parallel stages mutate disjoint result
// fields via errgroup, and synchronisation happens at stage boundaries).
//
// On pause (low classification confidence), a compact subset of this struct
// is serialized to Redis as PendingTypeConfirmation — see pending.go.
type PipelineState struct {
	// --- Identity & correlation ---
	CorrelationID   string `json:"correlation_id"`
	JobID           string `json:"job_id"`
	DocumentID      string `json:"document_id"`
	VersionID       string `json:"version_id"`
	OrganizationID  string `json:"organization_id"`
	CreatedByUserID string `json:"created_by_user_id,omitempty"`

	// --- Mode & lineage ---
	Mode            PipelineMode `json:"mode"`
	OriginType      string       `json:"origin_type,omitempty"`
	ParentVersionID *string      `json:"parent_version_id,omitempty"`

	// --- Progress tracking ---
	CurrentStage Stage     `json:"current_stage"`
	StartedAt    time.Time `json:"started_at"`

	// --- Tracing ---
	TraceContext TraceContext `json:"trace_context"`

	// --- DM-supplied artifacts (deferred-decoded) ---
	InputArtifacts InputArtifactsCompact `json:"input_artifacts,omitempty"`

	// --- Agent outputs (each populated by its stage) ---
	Classification      *ClassificationResult      `json:"classification_result,omitempty"`
	KeyParameters       *KeyParameters             `json:"key_parameters,omitempty"`
	PartyConsistency    *PartyConsistencyFindings  `json:"party_consistency_findings,omitempty"`
	MandatoryConditions *MandatoryConditionsReport `json:"mandatory_conditions_report,omitempty"`
	RiskAnalysis        *RiskAnalysis              `json:"risk_analysis,omitempty"`
	MergedRiskAnalysis  *RiskAnalysis              `json:"merged_risk_analysis,omitempty"`
	Recommendations     Recommendations            `json:"recommendations,omitempty"`
	Summary             *Summary                   `json:"summary,omitempty"`
	DetailedReport      *DetailedReport            `json:"detailed_report,omitempty"`
	RiskDelta           *RiskDelta                 `json:"risk_delta,omitempty"`
	ParentRiskAnalysis  *RiskAnalysis              `json:"parent_risk_analysis,omitempty"`

	// --- Derived (deterministic) artifacts ---
	RiskProfile    *RiskProfile    `json:"risk_profile,omitempty"`
	AggregateScore *AggregateScore `json:"aggregate_score,omitempty"`
}

// NewPipelineState constructs a fresh state with the given identifying fields,
// PipelineModeInitial, StageReceived, and StartedAt = now (UTC).
func NewPipelineState(correlationID, jobID, documentID, versionID, organizationID string) *PipelineState {
	return &PipelineState{
		CorrelationID:  correlationID,
		JobID:          jobID,
		DocumentID:     documentID,
		VersionID:      versionID,
		OrganizationID: organizationID,
		Mode:           PipelineModeInitial,
		CurrentStage:   StageReceived,
		StartedAt:      time.Now().UTC(),
	}
}
