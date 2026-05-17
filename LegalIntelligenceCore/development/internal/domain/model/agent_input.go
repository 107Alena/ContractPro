package model

import "encoding/json"

// ArtifactType enumerates the DM-side artifact types that LIC requests via
// lic.requests.artifacts (integration-contracts.md §2.1). Wire format matches
// the FROZEN DM GetArtifactsRequest.artifact_types enum.
type ArtifactType string

const (
	ArtifactSemanticTree       ArtifactType = "SEMANTIC_TREE"
	ArtifactExtractedText      ArtifactType = "EXTRACTED_TEXT"
	ArtifactDocumentStructure  ArtifactType = "DOCUMENT_STRUCTURE"
	ArtifactProcessingWarnings ArtifactType = "PROCESSING_WARNINGS"
	// ArtifactRiskAnalysis is requested only in RE_CHECK mode, for the parent version.
	ArtifactRiskAnalysis ArtifactType = "RISK_ANALYSIS"
)

// IsValid reports whether t is a known ArtifactType value.
func (t ArtifactType) IsValid() bool {
	switch t {
	case ArtifactSemanticTree, ArtifactExtractedText, ArtifactDocumentStructure,
		ArtifactProcessingWarnings, ArtifactRiskAnalysis:
		return true
	default:
		return false
	}
}

// InputArtifactsCompact is the DM-supplied artifact bundle in a defer-decoded
// form: keys are typed artifact identifiers, values are raw JSON bytes.
//
// Using json.RawMessage keeps LIC byte-for-byte faithful to the DM payload
// (no decode/re-encode round-trip) and avoids double allocation when the
// pipeline is paused-and-resumed via Redis.
type InputArtifactsCompact map[ArtifactType]json.RawMessage

// Has reports whether the given artifact type is present (non-nil bytes).
func (a InputArtifactsCompact) Has(t ArtifactType) bool {
	v, ok := a[t]
	return ok && len(v) > 0
}

// AgentInput is the common envelope handed to every agent's Run() call.
// It bundles the correlation identifiers, the DM-supplied artifacts, and the
// already-computed upstream agent results so a single agent has everything
// it needs without reaching into PipelineState directly.
//
// Fields not relevant to a given agent stay nil — agents document which ones
// they consume (see ai-agents-pipeline.md §4.3.2).
type AgentInput struct {
	// Correlation identifiers — propagated verbatim into LLM-call metadata.
	CorrelationID    string `json:"correlation_id"`
	JobID            string `json:"job_id"`
	DocumentID       string `json:"document_id"`
	VersionID        string `json:"version_id"`
	OrganizationID   string `json:"organization_id"`
	CreatedByUserID  string `json:"created_by_user_id,omitempty"`

	// DM-supplied artifacts. May be partial — only what was requested.
	Artifacts InputArtifactsCompact `json:"artifacts,omitempty"`

	// Upstream agent results, populated incrementally as the pipeline progresses.
	// All fields are pointers; nil means "not yet computed" for this run.
	Classification        *ClassificationResult        `json:"classification_result,omitempty"`
	KeyParameters         *KeyParameters               `json:"key_parameters,omitempty"`
	PartyConsistency      *PartyConsistencyFindings    `json:"party_consistency_findings,omitempty"`
	MandatoryConditions   *MandatoryConditionsReport   `json:"mandatory_conditions_report,omitempty"`
	RiskAnalysis          *RiskAnalysis                `json:"risk_analysis,omitempty"`
	MergedRiskAnalysis    *RiskAnalysis                `json:"merged_risk_analysis,omitempty"`
	Recommendations       Recommendations              `json:"recommendations,omitempty"`

	// Parent-version RISK_ANALYSIS — populated only in RE_CHECK mode for Agent 9.
	ParentRiskAnalysis *RiskAnalysis `json:"parent_risk_analysis,omitempty"`

	// ParentVersionID is the parent (base) version UUID — populated only in
	// RE_CHECK mode for Agent 9 (the symmetric counterpart of
	// ParentRiskAnalysis; copied by the Stage Executor from
	// PipelineState.ParentVersionID). Agent 9 rewrites it verbatim into
	// RiskDelta.base_version_id (ai-agents-pipeline.md §9 criterion 2;
	// risk_delta.json requires base_version_id). nil = INITIAL / not-RE_CHECK.
	ParentVersionID *string `json:"parent_version_id,omitempty"`
}
