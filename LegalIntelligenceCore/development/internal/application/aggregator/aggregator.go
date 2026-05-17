// Package aggregator implements the LIC Result Aggregator (LIC-TASK-035,
// high-architecture.md §6.11 / §6.11.1-3 / §4.3.4). It is the deterministic
// (no-LLM) final stage that folds the nine agent outputs into the outbound
// LegalAnalysisArtifactsReady surface:
//
//   - resolves the three risk id namespaces into one merged risks[] —
//     R-NNN (Agent 5, untouched) + R-PNNN (Agent 3 findings) + R-MNNN
//     (Agent 4 conditions with status != FOUND_OK) — §6.11.1;
//   - derives RISK_PROFILE and AGGREGATE_SCORE deterministically (no LLM,
//     ASSUMPTION-LIC-17/18, §6.11 steps 3-4);
//   - strips agent-internal fields (risks[].rationale,
//     key_parameters.internal_extras / prompt_injection_detected) — §6.11
//     step 5, the single stripping site (risk_analysis.go Risk godoc);
//   - merges the DETAILED_REPORT.warnings object-map: PROMPT_INJECTION_DETECTED
//     (C-lite, §6.11 step 6), RE_CHECK_PARENT_ANALYSIS_MISSING (§8.7),
//     INPUT_TRUNCATED (ASSUMPTION-LIC-12), CLASSIFICATION_PARAMS_MISMATCH and
//     RECOMMENDATION_ORPHAN_REF (cross-agent sanity, §6.11 step 7 / §6.11.3).
//
// Aggregate is a pure function: it never mutates anything reachable from its
// Input and produces freshly-allocated outputs (the Pipeline Orchestrator,
// LIC-TASK-036, owns assigning them back onto PipelineState). The package is
// hermetic — stdlib + internal/domain/model only; the Prometheus counter is
// inverted behind the Metrics seam. Design adjudicated by subagent
// code-architect (decisions D1..D11 — see CLAUDE.md).
package aggregator

import (
	"errors"
	"fmt"

	"contractpro/legal-intelligence-core/internal/domain/model"
)

// ErrNilRiskAnalysis is returned by Aggregate when in.RiskAnalysis is nil.
// Risk Detection (Agent 5) is a CRITICAL agent and the merge base (§6.11
// step 1); a nil here is an upstream pipeline-ordering/wiring defect, not a
// tolerable degradation (the riskdelta-D1 "nil critical input = breach"
// class). All other agent inputs are nil-tolerated.
var ErrNilRiskAnalysis = errors.New("aggregator: in.RiskAnalysis is nil (Agent 5 is critical and the merge base)")

// Config holds the aggregate-risk-score weights and label thresholds
// (high-architecture.md §6.11 step 4, configuration.md §3). It is a LOCAL
// struct populated by app-wiring (LIC-TASK-047) from config.ScoringConfig —
// the package stays free of an internal/config import (the agents/* ctor-param
// hermeticity precedent). Validated by NewAggregator.
type Config struct {
	WeightHigh               float64 // LIC_SCORE_WEIGHT_HIGH (default 25)
	WeightMedium             float64 // LIC_SCORE_WEIGHT_MEDIUM (default 10)
	WeightLow                float64 // LIC_SCORE_WEIGHT_LOW (default 3)
	WeightMissingMandatory   float64 // LIC_SCORE_WEIGHT_MISSING_MANDATORY (default 15)
	WeightAmbiguousMandatory float64 // LIC_SCORE_WEIGHT_AMBIGUOUS_MANDATORY (default 5)
	LabelLowThreshold        float64 // LIC_SCORE_LABEL_LOW_THRESHOLD (default 0.75)
	LabelMediumThreshold     float64 // LIC_SCORE_LABEL_MEDIUM_THRESHOLD (default 0.45)
}

func (c Config) validate() error {
	var errs []error
	for _, wv := range []struct {
		name string
		v    float64
	}{
		{"WeightHigh", c.WeightHigh},
		{"WeightMedium", c.WeightMedium},
		{"WeightLow", c.WeightLow},
		{"WeightMissingMandatory", c.WeightMissingMandatory},
		{"WeightAmbiguousMandatory", c.WeightAmbiguousMandatory},
	} {
		if wv.v < 0 {
			errs = append(errs, fmt.Errorf("aggregator: Config.%s must be >= 0, got %v", wv.name, wv.v))
		}
	}
	for _, tv := range []struct {
		name string
		v    float64
	}{
		{"LabelLowThreshold", c.LabelLowThreshold},
		{"LabelMediumThreshold", c.LabelMediumThreshold},
	} {
		if tv.v < 0 || tv.v > 1 {
			errs = append(errs, fmt.Errorf("aggregator: Config.%s must be in [0,1], got %v", tv.name, tv.v))
		}
	}
	if c.LabelMediumThreshold >= c.LabelLowThreshold {
		errs = append(errs, fmt.Errorf("aggregator: Config.LabelMediumThreshold (%v) must be < Config.LabelLowThreshold (%v)", c.LabelMediumThreshold, c.LabelLowThreshold))
	}
	return errors.Join(errs...)
}

// TruncationInfo carries the input-truncation byte counts measured upstream
// by the Token Estimator (LIC-TASK-021) and forwarded by the Orchestrator
// (LIC-TASK-036). A nil *TruncationInfo on Input means the input was NOT
// truncated (no INPUT_TRUNCATED warning). It is the Aggregator-side mirror
// of model.InputTruncatedWarning's data — no model carrier is added (the
// Agent-8-D7 anti-carrier stance; model.InputTruncatedWarning already exists
// as the wire carrier).
type TruncationInfo struct {
	TruncatedBytes int
	TotalBytes     int
}

// Input is the read-only view the Orchestrator (LIC-TASK-036) copies from
// PipelineState before invoking Aggregate. Every pointer is treated as
// immutable — Aggregate never writes through it (D2/D5).
type Input struct {
	// Mode gates the RE_CHECK_PARENT_ANALYSIS_MISSING warning (D4).
	Mode model.PipelineMode

	Classification      *model.ClassificationResult
	KeyParameters       *model.KeyParameters
	PartyConsistency    *model.PartyConsistencyFindings // Agent 3 is NON-CRITICAL: may be nil.
	MandatoryConditions *model.MandatoryConditionsReport
	RiskAnalysis        *model.RiskAnalysis // RAW Agent-5 output (ids R-NNN); the merge base.
	Recommendations     model.Recommendations

	// RiskDelta is DELIBERATELY NOT consulted by Aggregate (D4): the
	// RE_CHECK_PARENT_ANALYSIS_MISSING decision is signal-driven
	// (ParentAnalysisMissing), never shape-derived from a nil delta. It is
	// kept on Input so the Orchestrator can pass the full state surface and
	// so the non-tautology test can prove independence.
	RiskDelta *model.RiskDelta

	// Truncation is the LIC-TASK-021/036-sourced truncation signal; nil ⇒
	// not truncated ⇒ no INPUT_TRUNCATED warning.
	Truncation *TruncationInfo

	// ParentAnalysisMissing is set true by the Orchestrator (LIC-TASK-036)
	// when, in RE_CHECK mode, the parent version's RISK_ANALYSIS could not
	// be retrieved from DM (§8.7 step 2). NOT derived from RiskDelta==nil.
	ParentAnalysisMissing bool
}

// Output is the freshly-allocated result the Orchestrator assigns back onto
// PipelineState / the outbound payload. Every pointer is a distinct
// allocation from Input (D5).
type Output struct {
	// MergedRiskAnalysis is the R-NNN ∪ R-PNNN ∪ R-MNNN merged analysis with
	// rationale stripped; a distinct allocation from Input.RiskAnalysis.
	MergedRiskAnalysis *model.RiskAnalysis

	// RiskProfile / AggregateScore are the deterministic derived artifacts.
	RiskProfile    *model.RiskProfile
	AggregateScore *model.AggregateScore

	// Warnings is the merged DETAILED_REPORT.warnings object-map, or nil
	// when no warning fired (Warnings.IsEmpty()). The Orchestrator assigns
	// it into DetailedReport.Warnings.
	Warnings *model.Warnings

	// StrippedKeyParameters is Input.KeyParameters with internal_extras +
	// prompt_injection_detected dropped, a distinct allocation; nil iff
	// Input.KeyParameters is nil.
	StrippedKeyParameters *model.KeyParameters
}

// Aggregator is the stateless Result Aggregator. It is safe for concurrent
// use after construction (Aggregate is a pure function — the Stage-5/8
// errgroup-parallel pipeline may hold the same Input pointers, D5).
type Aggregator struct {
	cfg     Config
	metrics Metrics
}

// NewAggregator validates cfg and returns a ready Aggregator. A nil metrics
// seam is replaced by a no-op (the schemavalidator.NewRepairLoop precedent —
// fail-fast only on truly-required deps; telemetry is optional). Misconfig
// fails here, not silently at runtime (mirrors config.ScoringConfig.validate).
func NewAggregator(cfg Config, metrics Metrics) (*Aggregator, error) {
	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("aggregator: invalid config: %w", err)
	}
	if metrics == nil {
		metrics = noopMetrics{}
	}
	return &Aggregator{cfg: cfg, metrics: metrics}, nil
}

// Aggregate runs §6.11 steps 1-7 deterministically. The only error is
// ErrNilRiskAnalysis (Agent 5 critical / merge base, D2); all other agent
// inputs are nil-tolerated. Aggregate never mutates anything reachable from
// in (D5).
func (a *Aggregator) Aggregate(in Input) (Output, error) {
	if in.RiskAnalysis == nil {
		return Output{}, ErrNilRiskAnalysis
	}

	// Step 1-3: namespace-resolved merged risks[] (freshly allocated).
	merged := buildMergedRisks(in)

	mergedRA := &model.RiskAnalysis{
		Risks:   merged,
		Summary: in.RiskAnalysis.Summary,
		// PromptInjectionDetected is an internal envelope signal surfaced
		// only via the warning (D6); never carried on the merged artifact.
		PromptInjectionDetected: false,
	}

	// Step 4-5: deterministic derived artifacts.
	profile := riskProfile(merged)
	score := a.aggregateScore(profile, in.MandatoryConditions)

	// Step 6: key_parameters stripping (distinct allocation, D6).
	strippedKP := stripKeyParameters(in.KeyParameters)

	// Step 6-7: merged warnings object-map. Prompt-injection flags are read
	// from the RAW inputs BEFORE any stripping (D3 ordering).
	warnings := a.buildWarnings(in, merged)

	return Output{
		MergedRiskAnalysis:    mergedRA,
		RiskProfile:           profile,
		AggregateScore:        score,
		Warnings:              warnings,
		StrippedKeyParameters: strippedKP,
	}, nil
}
