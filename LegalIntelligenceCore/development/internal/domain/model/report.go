package model

// Summary is the output of Agent 7 — Business Summary (ai-agents-pipeline.md §7).
// Text is meant for non-legal readers (200..3000 characters; bounds are enforced
// by the schema validator, not at the type level).
type Summary struct {
	Text string `json:"text"`
}

// ReportSectionCode enumerates the 7 fixed-order sections of DetailedReport
// (ai-agents-pipeline.md §8). Wire order is the iteration order of the enum
// as published by Agent 8.
type ReportSectionCode string

const (
	ReportSectionOverview               ReportSectionCode = "OVERVIEW"
	ReportSectionKeyParameters          ReportSectionCode = "KEY_PARAMETERS"
	ReportSectionPartyData              ReportSectionCode = "PARTY_DATA"
	ReportSectionMandatoryConditions    ReportSectionCode = "MANDATORY_CONDITIONS"
	ReportSectionRisks                  ReportSectionCode = "RISKS"
	ReportSectionRecommendationsSummary ReportSectionCode = "RECOMMENDATIONS_SUMMARY"
	ReportSectionWarnings               ReportSectionCode = "WARNINGS"
)

// IsValid reports whether c is a known ReportSectionCode value.
func (c ReportSectionCode) IsValid() bool {
	switch c {
	case ReportSectionOverview, ReportSectionKeyParameters, ReportSectionPartyData,
		ReportSectionMandatoryConditions, ReportSectionRisks,
		ReportSectionRecommendationsSummary, ReportSectionWarnings:
		return true
	default:
		return false
	}
}

// DetailedReport is the output of Agent 8 — Detailed Report
// (ai-agents-pipeline.md §8). Agent 8 emits sections[]; warnings is left empty
// by the agent and filled by Result Aggregator from cross-agent state.
type DetailedReport struct {
	Sections []ReportSection `json:"sections"`
	Warnings *Warnings       `json:"warnings,omitempty"`
}

// ReportSection is one named, ordered section of DetailedReport.
type ReportSection struct {
	SectionCode ReportSectionCode `json:"section_code"`
	Title       string            `json:"title"`
	Items       []ReportItem      `json:"items"`
}

// ReportItem is one row inside a section. Severity, ClauseRef, LegalBasis,
// LinkedRiskID, LinkedRecommendation are wire-nullable per ai-agents-pipeline.md
// §8 schema (`type: ["string","null"]` / `type: ["string","null",null]`) — they
// serialise as null when unset, not omitted.
type ReportItem struct {
	Title                string     `json:"title"`
	Content              string     `json:"content"`
	Severity             *RiskLevel `json:"severity"`
	ClauseRef            *string    `json:"clause_ref"`
	LegalBasis           *string    `json:"legal_basis"`
	LinkedRiskID         *string    `json:"linked_risk_id"`
	LinkedRecommendation *string    `json:"linked_recommendation"`
}
