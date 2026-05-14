package model

import "regexp"

// RiskCategory enumerates the 22 possible values of risks[].category in the
// merged outbound payload (high-architecture.md §6.11.2). The wire enum is
// the union of three sources:
//
//   - 13 values from Agent 5 (Risk Detection) — generic contract-risk taxonomy.
//   - 7 values from Agent 3 (Party Consistency) — finding.type copied as-is.
//   - 2 values from Agent 4 (Mandatory Conditions) — derived by the Aggregator
//     based on MandatoryCondition.Status (MISSING / FOUND_AMBIGUOUS).
//
// Agent 5's own JSON schema is narrower (13 values) — that is input-validation
// for the agent itself; this 22-value enum is the LIC outbound contract.
type RiskCategory string

const (
	// From Agent 5 (13 values).
	RiskCategoryUnilateralChange         RiskCategory = "UNILATERAL_CHANGE"
	RiskCategoryUnilateralTermination    RiskCategory = "UNILATERAL_TERMINATION"
	RiskCategoryAutoRenewal              RiskCategory = "AUTO_RENEWAL"
	RiskCategoryJurisdictionUnfavorable  RiskCategory = "JURISDICTION_UNFAVORABLE"
	RiskCategoryAsymmetricLiability      RiskCategory = "ASYMMETRIC_LIABILITY"
	RiskCategoryAmbiguousAcceptance      RiskCategory = "AMBIGUOUS_ACCEPTANCE"
	RiskCategoryHiddenFees               RiskCategory = "HIDDEN_FEES"
	RiskCategoryWaiverOfRights           RiskCategory = "WAIVER_OF_RIGHTS"
	RiskCategoryForceMajeureOverreach    RiskCategory = "FORCE_MAJEURE_OVERREACH"
	RiskCategoryConfidentialityOverreach RiskCategory = "CONFIDENTIALITY_OVERREACH"
	RiskCategoryDataProcessingConcerns   RiskCategory = "DATA_PROCESSING_CONCERNS"
	RiskCategoryPromptInjectionAttempt   RiskCategory = "PROMPT_INJECTION_ATTEMPT"
	RiskCategoryOther                    RiskCategory = "OTHER"

	// From Agent 3 (Party Consistency) — copied from finding.type (7 values).
	RiskCategoryPartyDataInvalid         RiskCategory = "PARTY_DATA_INVALID"
	RiskCategoryPartyNameMismatch        RiskCategory = "PARTY_NAME_MISMATCH"
	RiskCategoryPartyAddressInconsistent RiskCategory = "PARTY_ADDRESS_INCONSISTENT"
	RiskCategoryPartyAuthorityMissing    RiskCategory = "PARTY_AUTHORITY_MISSING"
	RiskCategoryPartyINNInvalidChecksum  RiskCategory = "PARTY_INN_INVALID_CHECKSUM"
	RiskCategoryPartyOGRNInvalidChecksum RiskCategory = "PARTY_OGRN_INVALID_CHECKSUM"
	RiskCategoryPartyOGRNINNMismatch     RiskCategory = "PARTY_OGRN_INN_MISMATCH"

	// From Agent 4 (Mandatory Conditions) — derived by Aggregator (2 values).
	RiskCategoryMandatoryConditionMissing   RiskCategory = "MANDATORY_CONDITION_MISSING"
	RiskCategoryMandatoryConditionAmbiguous RiskCategory = "MANDATORY_CONDITION_AMBIGUOUS"
)

// allRiskCategories is the authoritative set used by IsValid + AllRiskCategories.
// Backed by a slice for deterministic iteration order; lookup map is built in init.
var allRiskCategories = []RiskCategory{
	RiskCategoryUnilateralChange, RiskCategoryUnilateralTermination, RiskCategoryAutoRenewal,
	RiskCategoryJurisdictionUnfavorable, RiskCategoryAsymmetricLiability,
	RiskCategoryAmbiguousAcceptance, RiskCategoryHiddenFees, RiskCategoryWaiverOfRights,
	RiskCategoryForceMajeureOverreach, RiskCategoryConfidentialityOverreach,
	RiskCategoryDataProcessingConcerns, RiskCategoryPromptInjectionAttempt, RiskCategoryOther,
	RiskCategoryPartyDataInvalid, RiskCategoryPartyNameMismatch, RiskCategoryPartyAddressInconsistent,
	RiskCategoryPartyAuthorityMissing, RiskCategoryPartyINNInvalidChecksum,
	RiskCategoryPartyOGRNInvalidChecksum, RiskCategoryPartyOGRNINNMismatch,
	RiskCategoryMandatoryConditionMissing, RiskCategoryMandatoryConditionAmbiguous,
}

var riskCategorySet map[RiskCategory]struct{}

func init() {
	riskCategorySet = make(map[RiskCategory]struct{}, len(allRiskCategories))
	for _, c := range allRiskCategories {
		riskCategorySet[c] = struct{}{}
	}
}

// IsValid reports whether c is a known RiskCategory value (any of the 22 merged-enum
// outbound values).
func (c RiskCategory) IsValid() bool {
	_, ok := riskCategorySet[c]
	return ok
}

// AllRiskCategories returns a fresh slice containing every known RiskCategory
// in canonical order: 13 Agent-5 values, then 7 PARTY_* values, then 2 MANDATORY_*.
func AllRiskCategories() []RiskCategory {
	out := make([]RiskCategory, len(allRiskCategories))
	copy(out, allRiskCategories)
	return out
}

// riskIDRE locks the wire format of risks[].id and recommendations[].risk_id
// after Result Aggregator merging (high-architecture.md §6.11.1 / event-catalog §2.1):
//
//	R-NNN  — from Agent 5 (Risk Detection)
//	R-PNNN — from Agent 3 (Party Consistency)
//	R-MNNN — from Agent 4 (Mandatory Conditions)
var riskIDRE = regexp.MustCompile(`^R-(P|M)?[0-9]{3,}$`)

// IsValidRiskID reports whether the given id matches the merged-output regex
// ^R-(P|M)?[0-9]{3,}$. Agent-5-only outputs (no P/M prefix) also pass.
func IsValidRiskID(id string) bool {
	return riskIDRE.MatchString(id)
}

// RiskAnalysis is the output of Agent 5 — Risk Detection & Severity Scoring
// (ai-agents-pipeline.md §5). It is the central artifact: Result Aggregator
// folds findings from Agents 3 and 4 into its risks[] slice with the R-PNNN
// and R-MNNN id namespaces. Outbound shape uses the 22-value RiskCategory enum;
// inbound (agent) shape is narrower (13 values).
type RiskAnalysis struct {
	Risks                   []Risk  `json:"risks"`
	Summary                 *string `json:"summary,omitempty"`
	PromptInjectionDetected bool    `json:"prompt_injection_detected"`
}

// Risk is one merged risk element. Id and category use the broadened wire
// formats. MandatoryConditionCode is populated only for R-MNNN entries
// (Result Aggregator copies it from the originating MandatoryCondition.Code
// for UI filtering — event-catalog §2.1). Rationale is internal LLM
// metadata; the future Result Aggregator (LIC-TASK-035) is the single
// stripping site that drops it before publishing LegalAnalysisArtifactsReady
// to DM (high-architecture.md §6.11 step 5).
type Risk struct {
	ID                      string       `json:"id"`
	Level                   RiskLevel    `json:"level"`
	Description             string       `json:"description"`
	ClauseRef               string       `json:"clause_ref"`
	LegalBasis              string       `json:"legal_basis"`
	Category                RiskCategory `json:"category"`
	MandatoryConditionCode  *string      `json:"mandatory_condition_code,omitempty"`
	Rationale               *string      `json:"rationale,omitempty"`
}
