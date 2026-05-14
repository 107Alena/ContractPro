package model

// RiskDelta is the output of Agent 9 — Risk Delta (ai-agents-pipeline.md §9).
// Emitted only when parent_version_id != null AND the parent's RISK_ANALYSIS
// was successfully retrieved (ASSUMPTION-LIC-02). Persisted by DM as a v1.1
// schema extension (ADR-LIC-05).
type RiskDelta struct {
	BaseVersionID   string             `json:"base_version_id"`
	TargetVersionID string             `json:"target_version_id"`
	Added           []RiskRef          `json:"added"`
	Removed         []RiskRef          `json:"removed"`
	Changed         []RiskChange       `json:"changed"`
	ProfileChange   *RiskProfileChange `json:"profile_change,omitempty"`
	Summary         string             `json:"summary"`
}

// RiskRef is a compact reference to a risk in either the base or target version.
// Description is capped at 600 characters by the schema (validated upstream).
type RiskRef struct {
	ID          string    `json:"id"`
	Level       RiskLevel `json:"level"`
	Description string    `json:"description"`
	ClauseRef   string    `json:"clause_ref"`
}

// RiskChange describes a risk that was preserved across versions but had its
// level or clause attribution updated. OldClauseRef and NewClauseRef are
// wire-nullable per ai-agents-pipeline.md §9 schema.
type RiskChange struct {
	TargetID     string    `json:"target_id"`
	BaseID       string    `json:"base_id"`
	OldLevel     RiskLevel `json:"old_level"`
	NewLevel     RiskLevel `json:"new_level"`
	OldClauseRef *string   `json:"old_clause_ref"`
	NewClauseRef *string   `json:"new_clause_ref"`
	Explanation  string    `json:"explanation"`
}

// RiskProfileChange captures the before/after risk-profile counts and overall
// level — used to drive UI summaries of "what changed since last version".
type RiskProfileChange struct {
	OldOverallLevel RiskLevel `json:"old_overall_level"`
	NewOverallLevel RiskLevel `json:"new_overall_level"`
	OldHighCount    int       `json:"old_high_count"`
	NewHighCount    int       `json:"new_high_count"`
	OldMediumCount  int       `json:"old_medium_count"`
	NewMediumCount  int       `json:"new_medium_count"`
	OldLowCount     int       `json:"old_low_count"`
	NewLowCount     int       `json:"new_low_count"`
}
