package aggregator

import "contractpro/legal-intelligence-core/internal/domain/model"

// stripKeyParameters is §6.11 step 5 for KEY_PARAMETERS: a distinct
// allocation of in.KeyParameters with the two LIC-internal signals dropped —
// internal_extras (downstream-agent extension, NOT in the FROZEN DM contract)
// and prompt_injection_detected (surfaced only via the aggregated warning,
// D3/D6). The struct is shallow-copied so clearing the two fields cannot
// mutate in.KeyParameters (D5); the remaining *string/[]string fields are
// only marshalled by the Orchestrator, never mutated here, so a deep copy of
// their targets is unnecessary. Returns nil when in.KeyParameters is nil
// (Agent 2 output absent — the Orchestrator handles the outbound shape).
//
// risks[].rationale stripping is performed inline in buildMergedRisks on the
// value-copies (merge.go) — this is the single stripping site
// (risk_analysis.go Risk godoc).
func stripKeyParameters(kp *model.KeyParameters) *model.KeyParameters {
	if kp == nil {
		return nil
	}
	out := *kp // shallow struct copy — distinct allocation.
	out.InternalExtras = nil
	out.PromptInjectionDetected = false
	return &out
}
