package model

// Recommendations is the output of Agent 6 — Recommendation
// (ai-agents-pipeline.md §6). It is a flat list of suggested clause
// replacements, each tied to one risk id (which may be R-NNN, R-PNNN
// or R-MNNN after Result Aggregator merging).
type Recommendations []Recommendation

// Recommendation is one suggested clause replacement. RiskID must reference
// an existing element in the merged RiskAnalysis.risks[] (Aggregator
// validates this and emits a RECOMMENDATION_ORPHAN_REF warning otherwise).
type Recommendation struct {
	RiskID          string `json:"risk_id"`
	OriginalText    string `json:"original_text"`
	RecommendedText string `json:"recommended_text"`
	Explanation     string `json:"explanation"`
}
