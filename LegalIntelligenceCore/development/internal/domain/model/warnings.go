package model

// Warning codes used as keys in the DetailedReport.warnings object-map.
// Wire format locked by ai-agents-pipeline.md §8 schema.
const (
	WarningCodePromptInjectionDetected    = "PROMPT_INJECTION_DETECTED"
	WarningCodeReCheckParentAnalysisMissing = "RE_CHECK_PARENT_ANALYSIS_MISSING"
	WarningCodeInputTruncated             = "INPUT_TRUNCATED"
	WarningCodeClassificationParamsMismatch = "CLASSIFICATION_PARAMS_MISMATCH"
	WarningCodeRecommendationOrphanRef    = "RECOMMENDATION_ORPHAN_REF"
)

// Warnings is the typed wrapper around DetailedReport.warnings. Each nullable
// pointer corresponds to one warning code; nil fields are omitted from JSON
// output, yielding the object-map shape that the FROZEN ai-agents-pipeline §8
// schema expects.
//
// Adding a new warning code is a contract-breaking change in v1 — extend this
// struct (and the spec) together.
type Warnings struct {
	PromptInjectionDetected       *PromptInjectionDetectedWarning      `json:"PROMPT_INJECTION_DETECTED,omitempty"`
	ReCheckParentAnalysisMissing  *ReCheckParentAnalysisMissingWarning `json:"RE_CHECK_PARENT_ANALYSIS_MISSING,omitempty"`
	InputTruncated                *InputTruncatedWarning               `json:"INPUT_TRUNCATED,omitempty"`
	ClassificationParamsMismatch  *ClassificationParamsMismatchWarning `json:"CLASSIFICATION_PARAMS_MISMATCH,omitempty"`
	RecommendationOrphanRef       *RecommendationOrphanRefWarning      `json:"RECOMMENDATION_ORPHAN_REF,omitempty"`
}

// IsEmpty reports whether no warning is set. Cheaper than marshalling.
func (w *Warnings) IsEmpty() bool {
	if w == nil {
		return true
	}
	return w.PromptInjectionDetected == nil &&
		w.ReCheckParentAnalysisMissing == nil &&
		w.InputTruncated == nil &&
		w.ClassificationParamsMismatch == nil &&
		w.RecommendationOrphanRef == nil
}

// PromptInjectionDetectedWarning aggregates the outcome of the 5-layer
// prompt-injection defense across all agents.
// Detected is always true when this warning is emitted (C-lite policy, OQ-13).
// DetectedByAgents is sorted lexicographically for deterministic output.
type PromptInjectionDetectedWarning struct {
	Detected         bool     `json:"detected"`
	DetectedByAgents []string `json:"detected_by_agents"`
	DetectionCount   int      `json:"detection_count"`
	UserMessage      string   `json:"user_message"`
}

// ReCheckParentAnalysisMissingWarning is emitted in RE_CHECK mode when the
// parent version's RISK_ANALYSIS could not be retrieved from DM.
// Agent 9 is skipped; Aggregator inserts this warning instead
// (high-architecture.md §8.7).
type ReCheckParentAnalysisMissingWarning struct {
	UserMessage string `json:"user_message"`
}

// InputTruncatedWarning signals that the contract text exceeded
// LIC_MAX_INPUT_TOKENS and was truncated by head/tail before being sent to
// the agents (high-architecture.md §6.7, ASSUMPTION-LIC-12).
type InputTruncatedWarning struct {
	TruncatedBytes int    `json:"truncated_bytes"`
	TotalBytes     int    `json:"total_bytes"`
	UserMessage    string `json:"user_message"`
}

// ClassificationParamsMismatchWarning is emitted by the cross-agent sanity
// check when the inferred contract_type is inconsistent with the extracted
// KEY_PARAMETERS (e.g. NDA with price != null).
type ClassificationParamsMismatchWarning struct {
	UserMessage string `json:"user_message"`
}

// RecommendationOrphanRefWarning is emitted when Agent 6 references a
// risk_id that does not exist in the merged risks[]. OrphanRiskIDs lists the
// offending values verbatim (for ops debugging — not shown to end user).
type RecommendationOrphanRefWarning struct {
	OrphanRiskIDs []string `json:"orphan_risk_ids"`
	UserMessage   string   `json:"user_message"`
}
