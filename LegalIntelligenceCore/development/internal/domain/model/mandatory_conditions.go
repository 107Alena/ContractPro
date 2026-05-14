package model

import "regexp"

// MandatoryConditionStatus enumerates the three states an obligatory clause
// can be in (ai-agents-pipeline.md §4).
type MandatoryConditionStatus string

const (
	MandatoryConditionFoundOK        MandatoryConditionStatus = "FOUND_OK"
	MandatoryConditionFoundAmbiguous MandatoryConditionStatus = "FOUND_AMBIGUOUS"
	MandatoryConditionMissing        MandatoryConditionStatus = "MISSING"
)

// IsValid reports whether s is a known MandatoryConditionStatus value.
func (s MandatoryConditionStatus) IsValid() bool {
	switch s {
	case MandatoryConditionFoundOK, MandatoryConditionFoundAmbiguous, MandatoryConditionMissing:
		return true
	default:
		return false
	}
}

// mandatoryConditionCodeRE locks the wire format of MandatoryCondition.code
// (FROZEN in ai-agents-pipeline.md §4 schema: `^MC_[A-Z0-9_]+$`).
var mandatoryConditionCodeRE = regexp.MustCompile(`^MC_[A-Z0-9_]+$`)

// IsValidMandatoryConditionCode reports whether the given code matches the
// canonical ^MC_[A-Z0-9_]+$ format.
func IsValidMandatoryConditionCode(code string) bool {
	return mandatoryConditionCodeRE.MatchString(code)
}

// MandatoryConditionsReport is the output of Agent 4 — Legal Mandatory
// Conditions Checker (ai-agents-pipeline.md §4). Findings with status
// MISSING/FOUND_AMBIGUOUS are folded into RiskAnalysis.risks[] by Result
// Aggregator with the R-MNNN id prefix (high-architecture.md §6.11.1).
type MandatoryConditionsReport struct {
	ContractType            string               `json:"contract_type"`
	Conditions              []MandatoryCondition `json:"conditions"`
	Summary                 *string              `json:"summary,omitempty"`
	PromptInjectionDetected bool                 `json:"prompt_injection_detected"`
}

// MandatoryCondition describes one expected obligatory clause for the
// detected contract type, its presence status, and the legal basis.
//
// FoundIn and IssueDescription are wire-nullable per ai-agents-pipeline.md §4
// schema (`type: ["array","null"]` / `type: ["string","null"]`) — they serialise
// as null when unset, not omitted.
type MandatoryCondition struct {
	Code             string                   `json:"code"`
	Label            string                   `json:"label"`
	Status           MandatoryConditionStatus `json:"status"`
	LegalBasis       string                   `json:"legal_basis"`
	FoundIn          []string                 `json:"found_in"`
	IssueDescription *string                  `json:"issue_description"`
}
