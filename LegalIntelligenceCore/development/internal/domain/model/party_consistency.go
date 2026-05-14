package model

// PartyFindingType enumerates the 7 finding types produced by Agent 3 —
// Party Data Consistency (ai-agents-pipeline.md §3).
type PartyFindingType string

const (
	PartyFindingDataInvalid          PartyFindingType = "PARTY_DATA_INVALID"
	PartyFindingNameMismatch         PartyFindingType = "PARTY_NAME_MISMATCH"
	PartyFindingAddressInconsistent  PartyFindingType = "PARTY_ADDRESS_INCONSISTENT"
	PartyFindingAuthorityMissing     PartyFindingType = "PARTY_AUTHORITY_MISSING"
	PartyFindingINNInvalidChecksum   PartyFindingType = "PARTY_INN_INVALID_CHECKSUM"
	PartyFindingOGRNInvalidChecksum  PartyFindingType = "PARTY_OGRN_INVALID_CHECKSUM"
	PartyFindingOGRNINNMismatch      PartyFindingType = "PARTY_OGRN_INN_MISMATCH"
)

// IsValid reports whether t is a known PartyFindingType value.
func (t PartyFindingType) IsValid() bool {
	switch t {
	case PartyFindingDataInvalid, PartyFindingNameMismatch, PartyFindingAddressInconsistent,
		PartyFindingAuthorityMissing, PartyFindingINNInvalidChecksum, PartyFindingOGRNInvalidChecksum,
		PartyFindingOGRNINNMismatch:
		return true
	default:
		return false
	}
}

// PartyConsistencyFindings is the output of Agent 3.
// Findings are folded into the merged RiskAnalysis.risks[] by Result Aggregator
// with the R-PNNN id prefix (high-architecture.md §6.11.1).
type PartyConsistencyFindings struct {
	Findings                []PartyFinding `json:"findings"`
	Summary                 *string        `json:"summary,omitempty"`
	PromptInjectionDetected bool           `json:"prompt_injection_detected"`
}

// PartyFinding is a single party-data inconsistency. Severity is set by the
// agent according to the spec table (PARTY_AUTHORITY_MISSING → high, all others
// default to medium — Result Aggregator does not re-map).
// PartyName is wire-nullable per ai-agents-pipeline.md §3 schema; LegalBasis is
// optional (omittable) and not nullable in the schema.
type PartyFinding struct {
	Type        PartyFindingType `json:"type"`
	Severity    RiskLevel        `json:"severity"`
	Description string           `json:"description"`
	PartyName   *string          `json:"party_name"`
	ClauseRef   string           `json:"clause_ref"`
	LegalBasis  *string          `json:"legal_basis,omitempty"`
}
