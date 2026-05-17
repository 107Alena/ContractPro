package aggregator

import (
	"fmt"

	"contractpro/legal-intelligence-core/internal/domain/model"
)

// buildMergedRisks performs the §6.11.1 deterministic id-namespace
// resolution into ONE freshly-allocated risks[] slice:
//
//  1. raw Agent-5 risks, received order, ids R-NNN untouched, rationale stripped;
//  2. each Agent-3 finding, received order, id R-P%03d from 001;
//  3. each Agent-4 condition with status != FOUND_OK, received order,
//     id R-M%03d from 001.
//
// Index counters are INDEPENDENT per prefix and start at 1; collisions are
// impossible by prefix (§6.11.1 invariant). Empty input ⇒ a non-nil empty
// slice (deterministic JSON `[]`, the DP "empty slices not nil" house rule).
// Never mutates in (value-copies each raw Risk; D5).
func buildMergedRisks(in Input) []model.Risk {
	raw := in.RiskAnalysis.Risks // in.RiskAnalysis != nil guaranteed by Aggregate.

	var nParty int
	if in.PartyConsistency != nil {
		nParty = len(in.PartyConsistency.Findings)
	}
	var nMandatory int
	if in.MandatoryConditions != nil {
		nMandatory = len(in.MandatoryConditions.Conditions)
	}

	merged := make([]model.Risk, 0, len(raw)+nParty+nMandatory)

	// Step 1 — raw Agent-5 risks. Value-copy then strip rationale (D6);
	// the *string aliasing is irrelevant once the copy's field is nil.
	for _, r := range raw {
		r.Rationale = nil
		merged = append(merged, r)
	}

	// Step 2 — Agent-3 party findings → R-P{idx:03d}. Level is
	// DETERMINISTICALLY DERIVED from finding.Type (D1); finding.Severity is
	// deliberately ignored (defense-in-depth: a prompt-injected/buggy
	// Agent 3 must not influence the merged level — §4.3.4 "фиксированный
	// уровень"). category = finding.type verbatim (§6.11.2 / §4.3.4).
	if in.PartyConsistency != nil {
		for i, f := range in.PartyConsistency.Findings {
			merged = append(merged, model.Risk{
				ID:          fmt.Sprintf("R-P%03d", i+1),
				Level:       partyLevel(f.Type),
				Description: f.Description,
				ClauseRef:   f.ClauseRef,
				LegalBasis:  derefOr(f.LegalBasis, ""),
				Category:    model.RiskCategory(f.Type),
			})
		}
	}

	// Step 3 — Agent-4 conditions with status != FOUND_OK → R-M{idx:03d}.
	// The R-M counter increments ONLY for mapped (non-FOUND_OK) conditions,
	// not slice position (§6.11.1 step 3). The originating MC_* code is
	// preserved on mandatory_condition_code for UI filtering (§4.3.4).
	if in.MandatoryConditions != nil {
		mIdx := 0
		for _, c := range in.MandatoryConditions.Conditions {
			if c.Status == model.MandatoryConditionFoundOK {
				continue
			}
			mIdx++
			code := c.Code
			merged = append(merged, model.Risk{
				ID:                     fmt.Sprintf("R-M%03d", mIdx),
				Level:                  mandatoryLevel(c.Status),
				Description:            mandatoryDescription(c),
				ClauseRef:              firstFoundIn(c.FoundIn),
				LegalBasis:             c.LegalBasis,
				Category:               mandatoryCategory(c.Status),
				MandatoryConditionCode: &code,
			})
		}
	}

	return merged
}

// partyLevel is the §4.3.4 fixed-level table: PARTY_AUTHORITY_MISSING is the
// only high party risk; every other PARTY_* is medium. It takes ONLY the
// finding type — PartyFinding.Severity must never reach this path (D1).
func partyLevel(t model.PartyFindingType) model.RiskLevel {
	if t == model.PartyFindingAuthorityMissing {
		return model.RiskLevelHigh
	}
	return model.RiskLevelMedium
}

// mandatoryLevel is the §4.3.4 fixed-level table for Agent-4 conditions:
// MISSING → high, FOUND_AMBIGUOUS → medium. Only called for status != FOUND_OK.
func mandatoryLevel(s model.MandatoryConditionStatus) model.RiskLevel {
	if s == model.MandatoryConditionMissing {
		return model.RiskLevelHigh
	}
	return model.RiskLevelMedium
}

// mandatoryCategory maps the Agent-4 status to the merged-enum category
// (§6.11.2 / §4.3.4). Only called for status != FOUND_OK.
func mandatoryCategory(s model.MandatoryConditionStatus) model.RiskCategory {
	if s == model.MandatoryConditionMissing {
		return model.RiskCategoryMandatoryConditionMissing
	}
	return model.RiskCategoryMandatoryConditionAmbiguous
}

// mandatoryDescription resolves the merged Risk.Description for an Agent-4
// condition: the issue_description when present, else the condition label.
// §6.11 does not spell this composition out — this is the minimal,
// deterministic SSOT-gap resolution (D11; recorded in CLAUDE.md). It does
// not affect ids/counts/score (the load-bearing invariants).
func mandatoryDescription(c model.MandatoryCondition) string {
	if c.IssueDescription != nil && *c.IssueDescription != "" {
		return *c.IssueDescription
	}
	return c.Label
}

// firstFoundIn returns the first clause reference, or "" when the condition
// has no located clauses (MISSING conditions typically have found_in: null).
func firstFoundIn(found []string) string {
	if len(found) > 0 {
		return found[0]
	}
	return ""
}

// derefOr returns *p or def when p is nil.
func derefOr(p *string, def string) string {
	if p == nil {
		return def
	}
	return *p
}
