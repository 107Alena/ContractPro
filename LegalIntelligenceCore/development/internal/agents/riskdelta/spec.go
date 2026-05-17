package riskdelta

import (
	"encoding/json"
	"errors"
	"fmt"

	"contractpro/legal-intelligence-core/internal/agents/base"
	"contractpro/legal-intelligence-core/internal/agents/promptbuilder"
	"contractpro/legal-intelligence-core/internal/domain/model"
	"contractpro/legal-intelligence-core/internal/domain/port"
)

// riskDeltaComparatorSpec is the Agent-9 strategy. It is an empty struct with
// value-receiver methods — STATELESS and safe for concurrent use, as base.Spec
// mandates (one *BaseAgent, hence one Spec, is shared by the parallel errgroup
// pipeline; pinned by TestRun_ConcurrentRaceClean, -race). Stage 6 runs a
// single agent, but the immutability contract is upheld uniformly across the 9.
type riskDeltaComparatorSpec struct{}

var _ base.Spec = riskDeltaComparatorSpec{}

// Parts builds the §9 envelope in the system-prompt's EXACT block order
// (risk_delta.txt:26-31 — the "ВХОДНЫЕ ДАННЫЕ" <input> section), 4 blocks:
//
//	<input>
//	  <base_version_id>… *in.ParentVersionID (parent/base version UUID) …</base_version_id>
//	  <target_version_id>… in.VersionID (current/target version UUID) …</target_version_id>
//	  <base_risk_analysis>… full MERGED RiskAnalysis of the PARENT version (in.ParentRiskAnalysis) …</base_risk_analysis>
//	  <target_risk_analysis>… full MERGED RiskAnalysis of the CURRENT version (in.MergedRiskAnalysis) …</target_risk_analysis>
//	</input>
//
// The §9 prompt's inner illustration form (`{"risks":[…]}`) is not binding —
// the ratified promptbuilder "the prompt is SSOT, the illustration form is
// not" precedent; four Content parts in the literal block order are the
// faithful mapping. ALL four blocks are routed through promptbuilder.Content
// (escaped — prompt-injection defence layer 2, NON-NEGOTIABLE): base_risk_-
// analysis / target_risk_analysis carry upstream-LLM-derived free text (a
// prompt-injected Risk.Description / ClauseRef / LegalBasis) — a literal
// closing tag in any of them can never read as a block delimiter (pinned by
// TestSpec_Parts_Layer2Escaping + TestSpec_Parts_UpstreamJSONInjection-
// Neutralised). base_version_id / target_version_id are LIC-controlled strings
// (from in.ParentVersionID / in.VersionID, NOT LLM output — the lowest-risk
// vectors) yet still routed through Content for envelope-shape uniformity (the
// detailedreport re_check_meta precedent). The shared Builder b is UNUSED:
// only Agent 3 mints a structural block (b.ValidationFacts) — hence the
// keyparams/typeclassifier/.../detailedreport `_` receiver-param. A returned
// error is a LIC programming/contract defect surfaced by base as
// INTERNAL_ERROR (never sent to the LLM).
//
// Input sourcing & strictness — ENVELOPE-MIRROR (code-architect D4 — the
// recommendation/summary CC-4 class, NOT the detailedreport CC-1
// grouped-by-class divergence). CC-1 grouping was justified ONLY because
// Agent 8 had FOUR strictness classes including a genuinely OPTIONAL/tolerated
// one; Agent 9 has exactly TWO classes, BOTH MANDATORY, ZERO optional/tolerated
// blocks (the fixed 4-block §9 shape has no "(если есть)" hedge), so the
// ratified rule "envelope-mirroring is valid only when all blocks are
// mandatory across ≤2 classes" makes this the textbook CC-4 case. A future
// reviewer must NOT regroup it to detailedreport CC-1. The two CONCEPTUAL
// classes (for the forward-notes) are:
//
//   - Class A — RE_CHECK-GATE MANDATORY (forward note 2): base_version_id ←
//     *in.ParentVersionID, base_risk_analysis ← in.ParentRiskAnalysis (the
//     WHOLE MERGED parent RiskAnalysis retrieved from DM — see below). Both
//     are present IFF the §8.7 RE_CHECK gate (parent_version_id != null AND
//     parent RISK_ANALYSIS retrieved) passed; a nil/empty either-of-two
//     reaching here means the gate (LIC-TASK-034) FAILED to skip Agent 9 — a
//     wiring defect ⇒ error ⇒ base INTERNAL_ERROR. The non-critical
//     "graceful degradation when the parent is missing" (risk_delta=null +
//     RE_CHECK_PARENT_ANALYSIS_MISSING warning, agent NOT invoked) is the
//     gate/orchestrator's job, NOT Agent 9's internal concern (forward note
//     2 — DISTINCT from the pipeline-ordering note 3; there is NO
//     DM-artifact-gate note because Agent 9 consumes ZERO DM artifacts).
//   - Class B — PIPELINE-ORDERING MANDATORY (forward note 3): target_-
//     version_id ← in.VersionID, target_risk_analysis ←
//     in.MergedRiskAnalysis. The Stage Executor MUST have populated the
//     LIC-TASK-035 Result Aggregator output before dispatching Agent 9
//     (Stage 6, after the merge); a nil any-of-two is a Stage-Executor
//     pipeline-wiring defect ⇒ error ⇒ base INTERNAL_ERROR.
//
// target_risk_analysis ← in.MergedRiskAnalysis, the LOAD-BEARING D2 (the
// Agent-6/8 MERGED class, NOT the Agent-7 RAW class). The decision driver is
// the IMMUTABLE shape of the parent artifact, not the bare §9 "Зависимости"
// line: DM only ever persisted the MERGED, published RISK_ANALYSIS
// (high-architecture.md §6.11/§6.11.1/§4.3.3 — the Result Aggregator is the
// SOLE producer; Agent-5 R-NNN + folded Agent-3 R-PNNN + Agent-4 R-MNNN,
// rationale stripped; there is NO raw-Agent-5 artifact stored anywhere). So
// in.ParentRiskAnalysis is UNCONDITIONALLY merged. §9's matching rule ("по
// семантической эквивалентности описания + близости clause_ref") and the
// profile_change EXACT counts demand an apples-to-apples pairing: comparing a
// merged parent against a RAW target would structurally guarantee every
// Agent-3/4-derived parent risk (R-PNNN/R-MNNN) to be spuriously classified
// `removed`. Stage 6 runs AFTER the LIC-TASK-035 merge, so in.MergedRisk-
// Analysis is available — the exact Agent-6/8 precedent. This OVERRIDES the
// bare §9 "RiskAnalysis текущей версии" wording and the illustrative R-001..
// R-004 prompt example exactly as Agent 8's D1 overrode the identically-bare
// §8 wording (the prompt example ids are illustrative, not a raw-vs-merged
// decision). Hence the nil-check is on in.MergedRiskAnalysis, NEVER the raw
// in.RiskAnalysis field; a non-nil raw in.RiskAnalysis with nil merged does
// NOT satisfy this — the Stage Executor must wire the MERGED field (forward
// note 3; pinned by the "nil merged but non-nil raw RiskAnalysis" subtest —
// the Agent-6/8 pin).
//
// Empty Risks[] in EITHER analysis (a clean version) is TOLERATED, marshalled
// verbatim WHOLE (the Agent-4/6/8 empty-slice tolerance — a present struct
// with no risks is in-spec, not a defect).
func (riskDeltaComparatorSpec) Parts(_ *promptbuilder.Builder, in model.AgentInput) ([]promptbuilder.Part, error) {
	// --- strictness phase: ENVELOPE-MIRROR order (D4 — CC-4, not CC-1). ---

	// base_version_id ← *in.ParentVersionID (Class A — RE_CHECK gate, fwd
	// note 2). nil pointer OR empty string ⇒ the §8.7 gate failed to skip
	// Agent 9 ⇒ wiring defect. Dereference ONLY after the nil check.
	if in.ParentVersionID == nil || *in.ParentVersionID == "" {
		return nil, errors.New("riskdelta: mandatory ParentVersionID (base/parent version id — §8.7 RE_CHECK gate must skip Agent 9 when absent) is nil or empty")
	}
	// target_version_id ← in.VersionID (Class B — pipeline-ordering, fwd
	// note 3). An empty current version id is a pipeline-wiring defect.
	if in.VersionID == "" {
		return nil, errors.New("riskdelta: mandatory VersionID (target/current version id) is empty")
	}
	// base_risk_analysis ← in.ParentRiskAnalysis (Class A — RE_CHECK gate,
	// fwd note 2). The WHOLE MERGED parent RiskAnalysis from DM.
	if in.ParentRiskAnalysis == nil {
		return nil, errors.New("riskdelta: mandatory upstream ParentRiskAnalysis (parent-version RISK_ANALYSIS — §8.7 RE_CHECK gate must skip Agent 9 when absent) result is absent")
	}
	// target_risk_analysis ← in.MergedRiskAnalysis (Class B — D2
	// LOAD-BEARING). MERGED, not raw: §9's matching/profile_change require
	// the post-merge R-PNNN/R-MNNN namespace to be symmetric with the
	// merged parent artifact. A non-nil raw in.RiskAnalysis does NOT satisfy
	// this — the Stage Executor must wire the MERGED field (forward note 3).
	if in.MergedRiskAnalysis == nil {
		return nil, errors.New("riskdelta: mandatory upstream MergedRiskAnalysis (post Result Aggregator merge, NOT raw RiskAnalysis) result is absent")
	}

	// --- assembly phase: envelope ORDER (risk_delta.txt:26-31) ---

	// base_risk_analysis: the WHOLE MERGED parent struct, verbatim (incl.
	// Summary + PromptInjectionDetected + the R-PNNN/R-MNNN ids — D2). An
	// empty Risks[] (a clean parent) is TOLERATED, marshalled verbatim.
	baseJSON, err := json.Marshal(in.ParentRiskAnalysis)
	if err != nil {
		return nil, fmt.Errorf("riskdelta: marshal base_risk_analysis: %w", err)
	}
	// target_risk_analysis: the WHOLE MERGED current struct, verbatim (D2).
	targetJSON, err := json.Marshal(in.MergedRiskAnalysis)
	if err != nil {
		return nil, fmt.Errorf("riskdelta: marshal target_risk_analysis: %w", err)
	}

	return []promptbuilder.Part{
		promptbuilder.Content("base_version_id", *in.ParentVersionID),
		promptbuilder.Content("target_version_id", in.VersionID),
		promptbuilder.Content("base_risk_analysis", string(baseJSON)),
		promptbuilder.Content("target_risk_analysis", string(targetJSON)),
	}, nil
}

// Decode unmarshals the schema-validated response into *model.RiskDelta (the
// concrete port.AgentResult the Stage Executor narrows by AgentID).
//
// base schema-validates the bytes against the embedded risk_delta.json BEFORE
// calling Decode (MF-1). The §9 schema constrains every risk-level field to
// the {high,medium,low} enum. Decode applies the codebase's enumerated
// schema-↔-Go cross-check house style (cf. base.canonicalStage, classifier's
// ContractType.IsValid, riskdetection's Risk.Level.IsValid, detailedreport's
// section_code/severity guard): a value that is schema-valid yet outside the
// Go whitelist can ONLY arise from a risk_delta.json ↔ model drift — a LIC
// build defect, surfaced as a terminal INTERNAL_ERROR. This is the
// detailedreport-D9 "guard EVERY closed-enum surface whose Go whitelist
// EXACTLY equals the schema enum" rule (NOT a one-guard quota): the §9 schema
// has the {high,medium,low} enum at FIVE surface kinds, ALL typed
// model.RiskLevel whose Go whitelist (RiskLevelHigh/Medium/Low — derived.go)
// EXACTLY equals it:
//
//   - added[i].level, removed[i].level via model.RiskLevel.IsValid() — the
//     RiskRef.Level surface (delta.go). Guarded.
//   - changed[i].old_level, changed[i].new_level via model.RiskLevel.IsValid()
//     — the RiskChange surface. Guarded.
//   - profile_change.old_overall_level / new_overall_level via
//     model.RiskLevel.IsValid() — guarded ONLY when ProfileChange != nil:
//     RiskProfileChange is *RiskProfileChange with omitempty and §9 schema
//     does NOT list profile_change in `required`, so a nil pointer is the
//     legitimate "no profile change" case (correctly decoded — guarding it
//     would WRONGLY reject schema-valid output, the detailedreport-D9
//     nullable-severity boundary). Guarded (non-nil only).
//
// Since the {high,medium,low} enum IS present in risk_delta.json, a
// model-emitted out-of-enum value is schema-INVALID ⇒ base step-7 fires the
// sticky 1-shot repair (the Agent-4/5/8 repair-triggered class, NOT the
// Agent-6 terminal class). These Decode guards are the build-defect BACKSTOP
// for the schema↔model-drift case the repair loop cannot catch (pinned by
// TestRun_InvalidLevel_RepairTriggered).
//
// Deliberately NOT guarded, each for a ratified reason (the mandatoryconditions/
// summary/detailedreport enumerated-unguarded-fields house style — the
// required written record even where the reason is "nothing to add"):
//
//   - BaseVersionID / TargetVersionID — schema `format:uuid` only. draft-07
//     `format` is annotation-only and the JSON-Schema validator does NOT
//     assert it, so a non-UUID is schema-VALID; re-validating UUID shape here
//     would duplicate the schema and over-reach past the closed-enum
//     boundary. CRUCIALLY there is NO `in`-cross-check that BaseVersionID ==
//     *in.ParentVersionID / TargetVersionID == in.VersionID: (a) it is
//     structurally impossible — base.Spec.Decode([]byte) has no `in`
//     parameter, and base is not modified by per-agent tasks; (b) the LLM
//     echoing the ids back is §9 criterion-2 SEMANTIC correctness owned by
//     the repair loop, NOT a schema↔model drift-guard (the recommendation
//     orphan-not-guarded / summary no-guard precedent — Decode is a pure
//     typed-unmarshal + closed-enum drift-guard, never a transform /
//     re-validation).
//   - Description / ClauseRef / Explanation / Summary and the nullable
//     OldClauseRef / NewClauseRef *string — free strings (schema maxLength /
//     nullable only, base pre-Decode). Re-checking would duplicate the schema.
//   - added/removed/changed MEMBERSHIP correctness (is a returned risk_ref
//     truly drawn from the corresponding analysis; is a changed-pair real)
//     is §9 criterion 4/5 SEMANTIC LLM judgement, owned by the repair loop —
//     Decode has no access to the input analyses anyway (the recommendation
//     orphan-not-guarded precedent).
//
// Decode is therefore a PURE typed-unmarshal + closed-enum drift-guard — never
// a transform: no re-mapping, no synthesis, no input cross-check. Any such
// transformation belongs downstream (Result Aggregator / Reporting Engine),
// NEVER here.
func (riskDeltaComparatorSpec) Decode(content []byte) (port.AgentResult, error) {
	var d model.RiskDelta
	if err := json.Unmarshal(content, &d); err != nil {
		return nil, fmt.Errorf("riskdelta: decode RiskDelta: %w", err)
	}
	for i, r := range d.Added {
		if !r.Level.IsValid() {
			return nil, fmt.Errorf("riskdelta: added[%d].level %q not in the high|medium|low whitelist (schema/model drift)", i, r.Level)
		}
	}
	for i, r := range d.Removed {
		if !r.Level.IsValid() {
			return nil, fmt.Errorf("riskdelta: removed[%d].level %q not in the high|medium|low whitelist (schema/model drift)", i, r.Level)
		}
	}
	for i, c := range d.Changed {
		if !c.OldLevel.IsValid() {
			return nil, fmt.Errorf("riskdelta: changed[%d].old_level %q not in the high|medium|low whitelist (schema/model drift)", i, c.OldLevel)
		}
		if !c.NewLevel.IsValid() {
			return nil, fmt.Errorf("riskdelta: changed[%d].new_level %q not in the high|medium|low whitelist (schema/model drift)", i, c.NewLevel)
		}
	}
	if d.ProfileChange != nil {
		if !d.ProfileChange.OldOverallLevel.IsValid() {
			return nil, fmt.Errorf("riskdelta: profile_change.old_overall_level %q not in the high|medium|low whitelist (schema/model drift)", d.ProfileChange.OldOverallLevel)
		}
		if !d.ProfileChange.NewOverallLevel.IsValid() {
			return nil, fmt.Errorf("riskdelta: profile_change.new_overall_level %q not in the high|medium|low whitelist (schema/model drift)", d.ProfileChange.NewOverallLevel)
		}
	}
	return &d, nil
}
