package aggregator

import "contractpro/legal-intelligence-core/internal/domain/model"

// riskProfile is §6.11 step 3 (ASSUMPTION-LIC-17): per-level counts over the
// MERGED risks[] (raw R-NNN + folded R-PNNN + R-MNNN) and the maximum level.
//
//	overall_level = high if high>0 else medium if medium>0 else low
//
// An all-empty merged list yields {low, 0, 0, 0} (low is the floor — there is
// no "none" level in the FROZEN DM RiskLevel enum).
func riskProfile(merged []model.Risk) *model.RiskProfile {
	p := &model.RiskProfile{}
	for _, r := range merged {
		switch r.Level {
		case model.RiskLevelHigh:
			p.HighCount++
		case model.RiskLevelMedium:
			p.MediumCount++
		case model.RiskLevelLow:
			p.LowCount++
		}
	}
	switch {
	case p.HighCount > 0:
		p.OverallLevel = model.RiskLevelHigh
	case p.MediumCount > 0:
		p.OverallLevel = model.RiskLevelMedium
	default:
		p.OverallLevel = model.RiskLevelLow
	}
	return p
}

// aggregateScore is §6.11 step 4 (ASSUMPTION-LIC-18), verbatim:
//
//	score = clamp(100 - Wh·high - Wm·medium - Wl·low
//	              - Wmm·missing_mandatory - Wam·ambiguous_mandatory, 0, 100)/100
//	label = low    if score >= LabelLowThreshold
//	        medium if score >= LabelMediumThreshold
//	        high   otherwise
//
// high/medium/low are the MERGED counts (same domain as RISK_PROFILE);
// missing/ambiguous_mandatory are counted over the ORIGINAL Agent-4
// conditions (status MISSING / FOUND_AMBIGUOUS), NOT a re-count of R-MNNN
// risks. The mandatory penalty layering ON TOP of the merged-risk penalty is
// the spec's deliberate empirical baseline — implemented exactly, not
// "de-duplicated" (D11). nil conditions ⇒ zero mandatory penalties.
func (a *Aggregator) aggregateScore(p *model.RiskProfile, mc *model.MandatoryConditionsReport) *model.AggregateScore {
	var missing, ambiguous int
	if mc != nil {
		for _, c := range mc.Conditions {
			switch c.Status {
			case model.MandatoryConditionMissing:
				missing++
			case model.MandatoryConditionFoundAmbiguous:
				ambiguous++
			}
		}
	}

	points := 100 -
		a.cfg.WeightHigh*float64(p.HighCount) -
		a.cfg.WeightMedium*float64(p.MediumCount) -
		a.cfg.WeightLow*float64(p.LowCount) -
		a.cfg.WeightMissingMandatory*float64(missing) -
		a.cfg.WeightAmbiguousMandatory*float64(ambiguous)

	score := clamp(points, 0, 100) / 100.0

	var label model.AggregateScoreLabel
	switch {
	case score >= a.cfg.LabelLowThreshold:
		label = model.AggregateScoreLabelLow
	case score >= a.cfg.LabelMediumThreshold:
		label = model.AggregateScoreLabelMedium
	default:
		label = model.AggregateScoreLabelHigh
	}

	return &model.AggregateScore{Score: score, Label: label}
}

// clamp constrains v to [lo, hi].
func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
