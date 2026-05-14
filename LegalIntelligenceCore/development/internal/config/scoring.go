package config

import (
	"errors"
	"fmt"
)

// oneMiB and minIngestedBytes are floor constants for LIC_MAX_INGESTED_BYTES
// (configuration.md §3 rule 9). One mebibyte is the lower bound below which
// a contract artifact cannot plausibly carry the required structured data.
const (
	oneMiB           int64 = 1024 * 1024
	minIngestedBytes int64 = oneMiB
)

// ScoringConfig holds the aggregate-risk-score weights and label thresholds
// (high-architecture.md §6.11).
type ScoringConfig struct {
	// Risk weights (number of risk events × weight contributes to the score).
	WeightHigh   float64 // LIC_SCORE_WEIGHT_HIGH
	WeightMedium float64 // LIC_SCORE_WEIGHT_MEDIUM
	WeightLow    float64 // LIC_SCORE_WEIGHT_LOW

	// Mandatory-conditions penalties.
	WeightMissingMandatory   float64 // LIC_SCORE_WEIGHT_MISSING_MANDATORY
	WeightAmbiguousMandatory float64 // LIC_SCORE_WEIGHT_AMBIGUOUS_MANDATORY

	// Label thresholds — bands are (HIGH, MEDIUM, LOW) by normalized score.
	LabelLowThreshold    float64 // LIC_SCORE_LABEL_LOW_THRESHOLD
	LabelMediumThreshold float64 // LIC_SCORE_LABEL_MEDIUM_THRESHOLD

	// Confidence threshold for type classification (FR-2.1.3).
	ConfidenceThreshold float64 // LIC_CONFIDENCE_THRESHOLD

	// Input-size limits.
	MaxInputTokens      int   // LIC_MAX_INPUT_TOKENS
	MaxAgentInputTokens int   // LIC_MAX_AGENT_INPUT_TOKENS
	MaxIngestedBytes    int64 // LIC_MAX_INGESTED_BYTES — hard cap on aggregate artifact bytes
}

func loadScoringConfig() ScoringConfig {
	return ScoringConfig{
		WeightHigh:               envFloat64("LIC_SCORE_WEIGHT_HIGH", 25),
		WeightMedium:             envFloat64("LIC_SCORE_WEIGHT_MEDIUM", 10),
		WeightLow:                envFloat64("LIC_SCORE_WEIGHT_LOW", 3),
		WeightMissingMandatory:   envFloat64("LIC_SCORE_WEIGHT_MISSING_MANDATORY", 15),
		WeightAmbiguousMandatory: envFloat64("LIC_SCORE_WEIGHT_AMBIGUOUS_MANDATORY", 5),
		LabelLowThreshold:        envFloat64("LIC_SCORE_LABEL_LOW_THRESHOLD", 0.75),
		LabelMediumThreshold:     envFloat64("LIC_SCORE_LABEL_MEDIUM_THRESHOLD", 0.45),
		ConfidenceThreshold:      envFloat64("LIC_CONFIDENCE_THRESHOLD", 0.75),
		MaxInputTokens:           envInt("LIC_MAX_INPUT_TOKENS", 150000),
		MaxAgentInputTokens:      envInt("LIC_MAX_AGENT_INPUT_TOKENS", 120000),
		MaxIngestedBytes:         envInt64("LIC_MAX_INGESTED_BYTES", 10*oneMiB),
	}
}

func (s ScoringConfig) validate() error {
	var errs []error
	for _, wv := range []struct {
		name string
		v    float64
	}{
		{"LIC_SCORE_WEIGHT_HIGH", s.WeightHigh},
		{"LIC_SCORE_WEIGHT_MEDIUM", s.WeightMedium},
		{"LIC_SCORE_WEIGHT_LOW", s.WeightLow},
		{"LIC_SCORE_WEIGHT_MISSING_MANDATORY", s.WeightMissingMandatory},
		{"LIC_SCORE_WEIGHT_AMBIGUOUS_MANDATORY", s.WeightAmbiguousMandatory},
	} {
		if wv.v < 0 {
			errs = append(errs, fmt.Errorf("config: %s must be >= 0, got %v", wv.name, wv.v))
		}
	}
	for _, tv := range []struct {
		name string
		v    float64
	}{
		{"LIC_SCORE_LABEL_LOW_THRESHOLD", s.LabelLowThreshold},
		{"LIC_SCORE_LABEL_MEDIUM_THRESHOLD", s.LabelMediumThreshold},
		{"LIC_CONFIDENCE_THRESHOLD", s.ConfidenceThreshold},
	} {
		if tv.v < 0 || tv.v > 1 {
			errs = append(errs, fmt.Errorf("config: %s must be in [0,1], got %v", tv.name, tv.v))
		}
	}
	if s.LabelMediumThreshold >= s.LabelLowThreshold {
		errs = append(errs, fmt.Errorf("config: LIC_SCORE_LABEL_MEDIUM_THRESHOLD (%v) must be < LIC_SCORE_LABEL_LOW_THRESHOLD (%v)", s.LabelMediumThreshold, s.LabelLowThreshold))
	}
	if s.MaxInputTokens < 1 {
		errs = append(errs, fmt.Errorf("config: LIC_MAX_INPUT_TOKENS must be >= 1, got %d", s.MaxInputTokens))
	}
	if s.MaxAgentInputTokens < 1 {
		errs = append(errs, fmt.Errorf("config: LIC_MAX_AGENT_INPUT_TOKENS must be >= 1, got %d", s.MaxAgentInputTokens))
	}
	if s.MaxInputTokens >= 1 && s.MaxAgentInputTokens >= 1 && s.MaxAgentInputTokens > s.MaxInputTokens {
		errs = append(errs, fmt.Errorf("config: LIC_MAX_AGENT_INPUT_TOKENS (%d) must be <= LIC_MAX_INPUT_TOKENS (%d)", s.MaxAgentInputTokens, s.MaxInputTokens))
	}
	if s.MaxIngestedBytes < minIngestedBytes {
		errs = append(errs, fmt.Errorf("config: LIC_MAX_INGESTED_BYTES must be >= %d (1 MiB), got %d", minIngestedBytes, s.MaxIngestedBytes))
	}
	return errors.Join(errs...)
}
