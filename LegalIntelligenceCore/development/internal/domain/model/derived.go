package model

// RiskLevel enumerates the three risk severities used across LIC artifacts
// (RiskAnalysis.risks[].level, RiskProfile.overall_level, RiskDelta levels).
//
// Wire-format: lowercase strings (high|medium|low). FROZEN by DM
// LegalAnalysisArtifactsReady contract (§1.5).
type RiskLevel string

const (
	RiskLevelHigh   RiskLevel = "high"
	RiskLevelMedium RiskLevel = "medium"
	RiskLevelLow    RiskLevel = "low"
)

// IsValid reports whether v is a known RiskLevel value.
func (l RiskLevel) IsValid() bool {
	switch l {
	case RiskLevelHigh, RiskLevelMedium, RiskLevelLow:
		return true
	default:
		return false
	}
}

// AggregateScoreLabel enumerates the qualitative label attached to AggregateScore.
// Calculated deterministically by the Result Aggregator
// (high-architecture.md §6.11 step 4):
//
//	low    if score >= 0.75
//	medium if score >= 0.45
//	high   otherwise
type AggregateScoreLabel string

const (
	AggregateScoreLabelLow    AggregateScoreLabel = "low"
	AggregateScoreLabelMedium AggregateScoreLabel = "medium"
	AggregateScoreLabelHigh   AggregateScoreLabel = "high"
)

// IsValid reports whether v is a known AggregateScoreLabel value.
func (l AggregateScoreLabel) IsValid() bool {
	switch l {
	case AggregateScoreLabelLow, AggregateScoreLabelMedium, AggregateScoreLabelHigh:
		return true
	default:
		return false
	}
}

// RiskProfile is the derived artifact summarising the merged risks[] —
// produced by deterministic calculation, not by an LLM agent
// (ASSUMPTION-LIC-17, high-architecture.md §6.11 step 3).
type RiskProfile struct {
	OverallLevel RiskLevel `json:"overall_level"`
	HighCount    int       `json:"high_count"`
	MediumCount  int       `json:"medium_count"`
	LowCount     int       `json:"low_count"`
}

// AggregateScore is the derived artifact summarising contract quality.
// Score is a value in [0.0, 1.0]; Label is its qualitative bucket.
// Produced deterministically (ASSUMPTION-LIC-18, high-architecture.md §6.11 step 4).
type AggregateScore struct {
	Score float64             `json:"score"`
	Label AggregateScoreLabel `json:"label"`
}
