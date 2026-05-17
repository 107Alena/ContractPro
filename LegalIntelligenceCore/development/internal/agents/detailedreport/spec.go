package detailedreport

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"

	"contractpro/legal-intelligence-core/internal/agents/base"
	"contractpro/legal-intelligence-core/internal/agents/promptbuilder"
	"contractpro/legal-intelligence-core/internal/domain/model"
	"contractpro/legal-intelligence-core/internal/domain/port"
)

// emptyProcessingWarnings is the byte sentinel emitted into the
// <processing_warnings> block when the OPTIONAL PROCESSING_WARNINGS artifact
// is absent / empty / the bare JSON `null` token (code-architect D6 — the
// EXACT riskdetection D3/CC-2 precedent, reused verbatim). It is the literal
// JSON empty ARRAY (NOT `{}`, NOT `null`, NOT ""): the §8 prompt envelope
// illustrates the block as `[…]` (detailed_report.txt:40), so `[]` is the
// type-consistent "no warnings" value the model reads unambiguously. This is
// the partyconsistency "never emit JSON null, always a well-formed [] the
// model can read" ratified rule (riskdetection CC-3); a bare PROCESSING_-
// WARNINGS being a byte-faithful passthrough with no LIC-side Go type, a byte
// literal is the faithful sentinel here (UNLIKE D4's typed sentinel).
var emptyProcessingWarnings = []byte("[]")

// emptyRecommendations is the byte sentinel for the <recommendations> block
// when in.Recommendations is nil or empty (code-architect D5). model.
// Recommendations is a bare SLICE: json.Marshal(nil-slice) yields `null`,
// which riskdetection CC-3 forbids in an LLM-facing block. A clean contract
// legitimately yields ZERO recommendations and a slice's nil is
// indistinguishable from empty, so this is the OPTIONAL/tolerated class (the
// Agent-6 "empty Risks[] ⇒ zero recommendations is in-spec" + Agent-7
// empty-slice tolerance), NOT a pipeline-ordering hard-fail. recommendation
// .json's root type is array, so `[]` is the type-consistent empty value.
var emptyRecommendations = []byte("[]")

// reCheckMetaDefault is the FIXED <re_check_meta> block (code-architect D7,
// Option A). model.AgentInput carries NO re_check_meta source (only
// ParentRiskAnalysis, nil in BOTH the INITIAL and the RE_CHECK-parent-missing
// states — indistinguishable), and ai-agents-pipeline.md:1390 de-scopes the
// machine warnings map from Agent 8 entirely: warnings (incl. the
// RE_CHECK_PARENT_ANALYSIS_MISSING code) are formed by the Result Aggregator
// (LIC-TASK-035), not this agent. re_check_meta is therefore LLM-prose-tone
// context only; the safe, in-spec default for the common INITIAL path is the
// all-false object. Accurate sourcing (INITIAL vs RE_CHECK / parent-missing)
// is owned downstream — see FORWARD NOTE 6. A future reviewer must NOT add a
// model.AgentInput carrier in THIS task (the ratified "re-touching a reviewed
// shared surface = unrequested scope creep" stance; the architecture
// CLAUDE.md YAGNI boundary). Both keys are present, matching the §8
// schema-illustration shape (detailed_report.txt:41). It is fixed
// LIC-authored bytes (no &<>) yet still routed through promptbuilder.Content
// for envelope-shape uniformity (D10).
var reCheckMetaDefault = []byte(`{"is_re_check":false,"parent_analysis_missing":false}`)

// emptyPartyConsistency returns the <party_consistency_findings> sentinel for
// a nil in.PartyConsistency (code-architect D4). Agent 3 is NON-CRITICAL
// (error-handling.md:304 — skipped on timeout/failure → no findings), so a
// nil in.PartyConsistency is an IN-SPEC degradation state, NOT a
// pipeline-ordering breach (FORWARD NOTE 4 — the riskdetection-FN-5
// OPTIONAL-tolerance class, DISTINCT from the pipeline-ordering note 2 and the
// DM-artifact-gate note 3). The §8 prompt (detailed_report.txt:19-20)
// explicitly handles the empty case ("Если расхождений нет — один item
// «согласованы и полны»").
//
// CRUCIAL (code-architect D4 binding correction): the sentinel is NOT a
// hand-written `{"findings":[]}` literal but the marshalled ZERO-VALUE
// model.PartyConsistencyFindings with an EXPLICITLY non-nil empty Findings
// slice — so the nil path and the non-nil-empty path emit BYTE-IDENTICAL
// shapes from the SAME type SSOT (a hand literal would silently drift if the
// struct tags change, e.g. the non-omitempty prompt_injection_detected bool).
// Marshalling this fixed typed value cannot fail in practice; an error is a
// LIC build defect surfaced by base as INTERNAL_ERROR (the classification_-
// result Marshal-cannot-fail precedent).
func emptyPartyConsistency() ([]byte, error) {
	return json.Marshal(model.PartyConsistencyFindings{Findings: []model.PartyFinding{}})
}

// classificationProjection is the MINIMAL §8 view of Agent 1's result
// (code-architect D2 — the ratified Agent-4/5/7 bare-ellipsis precedent). The
// §8 prompt envelope block is an ellipsis (`<classification_result>…`,
// detailed_report.txt:34); the OVERVIEW section needs only the contract-type
// LABEL ("это договор поставки…") — confidence / alternatives / rationale /
// prompt_injection_detected are irrelevant to a report and emitting them is
// pure waste against the 5000-token output budget. The field is typed
// model.ContractType (NOT string) so a future enum-value rename is a compile
// error here too and the rendered value is a real enum member (the byte-exact
// single-key shape is pinned by
// TestSpec_Parts_ClassificationResultMinimalProjection).
type classificationProjection struct {
	ContractType model.ContractType `json:"contract_type"`
}

// detailedReporterSpec is the Agent-8 strategy. It is an empty struct with
// value-receiver methods — STATELESS and safe for concurrent use, as base.Spec
// mandates (one *BaseAgent, hence one Spec, is shared by the parallel errgroup
// pipeline; Stage 5: Agent 8 ‖ Agent 7; pinned by
// TestRun_ConcurrentRaceClean, -race).
type detailedReporterSpec struct{}

var _ base.Spec = detailedReporterSpec{}

// Parts builds the §8 envelope in the system-prompt's EXACT block order
// (detailed_report.txt:33-43 — the "ВХОДНЫЕ ДАННЫЕ" <input> section), 9
// blocks:
//
//	<input>
//	  <classification_result>{"contract_type":"…"} — minted from Agent 1</classification_result>
//	  <key_parameters>… full JSON KeyParameters from Agent 2 …</key_parameters>
//	  <party_consistency_findings>… full PartyConsistencyFindings (Agent 3) or {findings:[]} …</party_consistency_findings>
//	  <mandatory_conditions_report>… full MandatoryConditionsReport from Agent 4 …</mandatory_conditions_report>
//	  <risk_analysis>… full MERGED RiskAnalysis (R-NNN + R-PNNN + R-MNNN) …</risk_analysis>
//	  <recommendations>… full Recommendations from Agent 6, or [] …</recommendations>
//	  <processing_warnings>… raw PROCESSING_WARNINGS JSON, or [] when absent …</processing_warnings>
//	  <re_check_meta>{"is_re_check":false,"parent_analysis_missing":false} (fixed default — D7)</re_check_meta>
//	  <semantic_tree>… raw SEMANTIC_TREE JSON, byte-faithful passthrough …</semantic_tree>
//	</input>
//
// Wrong order risks the model reading the tree as classification input. The §8
// prompt's inner illustration form is not binding — the ratified promptbuilder
// "the prompt is SSOT, the illustration form is not" precedent; nine Content
// parts in the literal block order are the faithful mapping. ALL nine blocks
// are routed through promptbuilder.Content (escaped — prompt-injection defence
// layer 2, NON-NEGOTIABLE): risk_analysis / mandatory_conditions_report /
// party_consistency_findings / recommendations carry upstream-LLM-derived free
// text (a prompt-injected description / clause_ref / legal_basis /
// issue_description / explanation) and semantic_tree is the raw contract tree
// — a literal closing tag in any of them can never read as a block delimiter
// (pinned by TestSpec_Parts_Layer2Escaping +
// TestSpec_Parts_UpstreamJSONInjectionNeutralised). classification_result is
// the LOWEST-risk vector by design: it projects only the typed
// model.ContractType enum, which cannot carry attacker bytes; re_check_meta is
// fixed LIC-authored bytes (escape is a no-op) yet routed through Content for
// uniformity.
//
// The shared Builder b is UNUSED: only Agent 3 mints a structural block
// (b.ValidationFacts) — hence the keyparams/typeclassifier/mandatoryconditions/
// riskdetection/recommendation/summary `_` receiver-param, not Agent 3's named
// `b`. Agent 8 performs NO INN/OGRN validation and mints NO <validation_facts>.
// A returned error is a LIC programming/contract defect surfaced by base as
// INTERNAL_ERROR (never sent to the LLM).
//
// Input sourcing & strictness — GROUPED BY CLASS, NOT mirroring the envelope
// order (code-architect D10 — the riskdetection CC-1 divergence, NOT the
// summary/recommendation CC-4 envelope-mirror). Envelope-mirroring is valid
// only when all blocks are mandatory across ≤2 classes; Agent 8 has FOUR
// distinct strictness classes (more than Agent 5's three), so the strictness
// phase groups by class while the assembly phase keeps the 9-block envelope
// order. A future reviewer must NOT regroup it to envelope-mirror:
//
//   - Class 1 — PIPELINE-ORDERING MANDATORY (forward note 2):
//     classification_result ← in.Classification.ContractType (minimal
//     projection — D2), key_parameters ← in.KeyParameters (whole — D3),
//     mandatory_conditions_report ← in.MandatoryConditions (whole — D3),
//     risk_analysis ← in.MergedRiskAnalysis (whole — D1, LOAD-BEARING). All
//     four are produced by critical/tier-2 agents (1=critical, 2/4/6=tier-2 —
//     error-handling.md:305-306) that MUST have run before Agent 8 (Stage 5);
//     a nil any-of-four is a Stage-Executor pipeline-wiring defect ⇒ error ⇒
//     base INTERNAL_ERROR. NO tolerated-empty (the §8 prompt has no "(если
//     есть)" hedge for these and the envelope is a fixed 9-block shape). The
//     contract_type VALUE is trusted as Agent 1's typed, drift-guarded output
//     (the Agent-3/4/5/7 "trust the upstream typed result" precedent).
//     risk_analysis is the DELIBERATE divergence from the immediately-
//     preceding Agent 7 (which sourced RAW): §8's prompt PROHIBITION
//     (detailed_report.txt:72-73) explicitly states the embedded Agent-3/4
//     risks "уже находятся в RiskAnalysis после Result Aggregator" and
//     criterion 5 (detailed_report.txt:55) ties linked_risk_id to ids the
//     MERGED set defines — raw Agent-5 (R-NNN only) makes the §8 prohibition
//     unsatisfiable (the Agent-6 D3 structural argument). So the nil-check is
//     on in.MergedRiskAnalysis, NEVER the raw RiskAnalysis field; a non-nil
//     raw in.RiskAnalysis with nil merged does NOT satisfy this — the Stage
//     Executor must wire the MERGED field (forward note 2). The §8
//     "Зависимости" bare `RiskAnalysis` wording (same as §7's) is overridden
//     by the §8 prompt-body's explicit post-merge requirement (the Agent-7-D1
//     corroborating factors are INVERTED here).
//   - Class 2 — MANDATORY DM-ARTIFACT (forward note 3): semantic_tree ←
//     SEMANTIC_TREE. BYTE-FAITHFUL passthrough (string(raw), NOT
//     decoded/re-encoded — §8 criterion 3 makes the model cite tree node ids
//     as clause_ref, so pruning/re-keying would strip the very ids it must
//     reference; the recommendation/Agent-2/4/5 precedent). Strictness gate is
//     well-formedness (json.Valid), never a structural decode. MANDATORY:
//     absent / empty bytes / !json.Valid ⇒ error. An empty-but-well-formed
//     tree ({}) is TOLERATED — emitted verbatim. There is NO EXTRACTED_TEXT
//     block here (the §8 envelope has no <contract_document>; Agent 8 is the
//     Agent-6 non-EXTRACTED_TEXT-consumer class, hence no artifacts import —
//     detailedreport.go hermeticity note / D10).
//   - Class 3 — OPTIONAL UPSTREAM (forward note 4 — Agent-3 non-critical
//     tolerance, the riskdetection-FN-5 class): party_consistency_findings ←
//     in.PartyConsistency (nil TOLERATED → typed zero-value sentinel — D4),
//     recommendations ← in.Recommendations (nil/empty TOLERATED → [] sentinel
//     — D5). NEITHER is a pipeline-ordering breach: Agent 3 is non-critical
//     (legitimately skipped) and a clean contract legitimately yields zero
//     recommendations.
//   - Class 4 — OPTIONAL DM-ARTIFACT (forward note 5 — the riskdetection
//     D3/CC-2 class) + DERIVED: processing_warnings ← PROCESSING_WARNINGS
//     (absent/empty/null → [], present&valid → verbatim, present&!valid →
//     error — D6), re_check_meta ← the fixed all-false default (D7).
//
// FORWARD NOTE 3 (owner: LIC-TASK-034 — the artifact-bundle gate, DISTINCT
// from the pipeline-ordering note 2 and identical in spirit to typeclassifier
// #2 / keyparams / partyconsistency #3 / mandatoryconditions #3 /
// riskdetection #3 / recommendation #3). A genuinely missing/empty
// SEMANTIC_TREE artifact is semantically DM_ARTIFACTS_MISSING (retryable,
// "данные документа не найдены"), NOT INTERNAL_ERROR. That bundle gate MUST
// reject it BEFORE Agent 8 runs; reaching this Parts check then means a true
// LIC invariant breach, for which base's Parts-error→INTERNAL_ERROR
// projection is the CORRECT code. The OPTIONAL PROCESSING_WARNINGS and the
// non-critical PartyConsistency are explicitly OUT of that gate (notes 4/5).
// base is not modified by this task.
func (detailedReporterSpec) Parts(_ *promptbuilder.Builder, in model.AgentInput) ([]promptbuilder.Part, error) {
	// --- strictness phase: GROUPED BY CLASS, not by envelope order (D10 — the
	// riskdetection CC-1 divergence). ---

	// Class 1 — pipeline-ordering MANDATORY (forward note 2).
	if in.Classification == nil {
		return nil, errors.New("detailedreport: mandatory upstream ClassificationResult (Agent 1) result is absent")
	}
	if in.KeyParameters == nil {
		return nil, errors.New("detailedreport: mandatory upstream KeyParameters (Agent 2) result is absent")
	}
	if in.MandatoryConditions == nil {
		return nil, errors.New("detailedreport: mandatory upstream MandatoryConditionsReport (Agent 4) result is absent")
	}
	if in.MergedRiskAnalysis == nil {
		// MERGED, not raw: §8's prohibition requires the post-merge
		// R-PNNN/R-MNNN ids (the LOAD-BEARING D1 divergence from Agent 7's
		// RAW). A non-nil raw in.RiskAnalysis does NOT satisfy this — the
		// Stage Executor must wire the MERGED field (forward note 2).
		return nil, errors.New("detailedreport: mandatory upstream MergedRiskAnalysis (post Result Aggregator merge, NOT raw RiskAnalysis) result is absent")
	}

	// Class 2 — mandatory DM artifact (forward note 3).
	rawTree, ok := in.Artifacts[model.ArtifactSemanticTree]
	if !ok || len(rawTree) == 0 {
		return nil, errors.New("detailedreport: mandatory SEMANTIC_TREE artifact is absent")
	}
	if !json.Valid(rawTree) {
		return nil, errors.New("detailedreport: SEMANTIC_TREE artifact is not well-formed JSON")
	}

	// Class 4 — OPTIONAL PROCESSING_WARNINGS (forward note 5; NOT note 3).
	// absent / empty / bare `null` token ⇒ normalise to `[]`; present &
	// well-formed ⇒ byte-faithful verbatim; present & malformed ⇒ error
	// (a corrupt PRESENT artifact is a defect — the SEMANTIC_TREE
	// well-formedness-gate precedent; distinct from "absent", tolerated here
	// because §8 marks it `<!-- от DP, опц. -->` and the tree is not).
	pw := emptyProcessingWarnings
	if rawPW, present := in.Artifacts[model.ArtifactProcessingWarnings]; present {
		trimmed := bytes.TrimSpace(rawPW)
		switch {
		case len(trimmed) == 0 || string(trimmed) == "null":
			// keep the [] sentinel (json.Valid("null")==true, but a bare null
			// is a degenerate non-array payload — fold into the absent/empty
			// tier, never emit JSON null — riskdetection CC-2/CC-3).
		case !json.Valid(rawPW):
			return nil, errors.New("detailedreport: PROCESSING_WARNINGS artifact is present but not well-formed JSON")
		default:
			pw = rawPW // byte-faithful passthrough verbatim
		}
	}

	// --- assembly phase: envelope ORDER (detailed_report.txt:33-43) ---

	// classification_result: minimal {"contract_type":"…"} projection (D2).
	clsJSON, err := json.Marshal(classificationProjection{ContractType: in.Classification.ContractType})
	if err != nil {
		// model.ContractType is a string ⇒ Marshal cannot fail in practice;
		// a failure is a LIC build defect (base → INTERNAL_ERROR).
		return nil, fmt.Errorf("detailedreport: marshal classification_result: %w", err)
	}

	// key_parameters: WHOLE (incl. internal_extras when present; a nil
	// InternalExtras drops the key via omitempty — D3, the Agent-4/5/6/7
	// tolerance precedent).
	kpJSON, err := json.Marshal(in.KeyParameters)
	if err != nil {
		return nil, fmt.Errorf("detailedreport: marshal key_parameters: %w", err)
	}

	// party_consistency_findings: WHOLE when present (D4); nil ⇒ the typed
	// zero-value sentinel (NOT a hand literal — drift-locked to the struct
	// SSOT). An empty Findings[] in a present struct is TOLERATED verbatim.
	var pcJSON []byte
	if in.PartyConsistency == nil {
		pcJSON, err = emptyPartyConsistency()
	} else {
		pcJSON, err = json.Marshal(in.PartyConsistency)
	}
	if err != nil {
		return nil, fmt.Errorf("detailedreport: marshal party_consistency_findings: %w", err)
	}

	// mandatory_conditions_report: WHOLE (D3); empty Conditions[] ("all
	// present") tolerated verbatim (the Agent-4/6/7 empty-slice tolerance).
	mcJSON, err := json.Marshal(in.MandatoryConditions)
	if err != nil {
		return nil, fmt.Errorf("detailedreport: marshal mandatory_conditions_report: %w", err)
	}

	// risk_analysis: the WHOLE MERGED struct (incl. Summary +
	// PromptInjectionDetected + the R-PNNN/R-MNNN ids — D1). An empty Risks[]
	// (a clean contract) is TOLERATED, marshalled verbatim.
	raJSON, err := json.Marshal(in.MergedRiskAnalysis)
	if err != nil {
		return nil, fmt.Errorf("detailedreport: marshal risk_analysis: %w", err)
	}

	// recommendations: WHOLE when non-empty; nil/empty ⇒ the [] sentinel (D5
	// — json.Marshal(nil-slice)==`null` which CC-3 forbids).
	rcJSON := emptyRecommendations
	if len(in.Recommendations) > 0 {
		rcJSON, err = json.Marshal(in.Recommendations)
		if err != nil {
			return nil, fmt.Errorf("detailedreport: marshal recommendations: %w", err)
		}
	}

	return []promptbuilder.Part{
		promptbuilder.Content("classification_result", string(clsJSON)),
		promptbuilder.Content("key_parameters", string(kpJSON)),
		promptbuilder.Content("party_consistency_findings", string(pcJSON)),
		promptbuilder.Content("mandatory_conditions_report", string(mcJSON)),
		promptbuilder.Content("risk_analysis", string(raJSON)),
		promptbuilder.Content("recommendations", string(rcJSON)),
		promptbuilder.Content("processing_warnings", string(pw)),
		promptbuilder.Content("re_check_meta", string(reCheckMetaDefault)),
		promptbuilder.Content("semantic_tree", string(rawTree)),
	}, nil
}

// Decode unmarshals the schema-validated response into *model.DetailedReport
// (the concrete port.AgentResult the Stage Executor narrows by AgentID).
//
// base schema-validates the bytes against the embedded detailed_report.json
// BEFORE calling Decode (MF-1). The §8 schema constrains section_code to the
// 7-value enum and item.severity to {high,medium,low,null}. Decode applies
// the codebase's enumerated schema-↔-Go cross-check house style (cf.
// base.canonicalStage, classifier's ContractType.IsValid, partyconsistency's
// f.Severity.IsValid, riskdetection's Risk.Level.IsValid): a value that is
// schema-valid yet outside the Go whitelist can ONLY arise from a
// detailed_report.json ↔ model drift — a LIC build defect, surfaced as a
// terminal INTERNAL_ERROR. This is the riskdetection-Level / partyconsistency-
// Severity "guard EVERY closed-enum surface whose Go whitelist EXACTLY equals
// the schema enum" rule (code-architect D9-sub — NOT a one-guard quota; the
// riskdetection "only the sole exactly-equal surface" phrasing described a
// FACT about Agent 5's surfaces, not a limit):
//
//   - sections[i].section_code via model.ReportSectionCode.IsValid() — the
//     7-value Go whitelist (report.go) EXACTLY equals the §8 schema enum
//     (detailed_report.json). Guarded.
//   - sections[i].items[j].severity via model.RiskLevel.IsValid() — guarded
//     ONLY when non-nil: the field is *RiskLevel; the schema enum's `null`
//     member maps to a nil pointer (the legitimate "no severity" case,
//     correctly decoded — guarding it would WRONGLY reject schema-valid
//     output, the Agent-4 MandatoryConditionsReport.ContractType "don't
//     wrongly reject schema-valid output" boundary). A non-nil pointer's
//     value-set is exactly {high,medium,low} — EXACTLY the schema's non-null
//     members and the identical model.RiskLevel type partyconsistency's
//     f.Severity guard already locks. Guarded (non-nil only).
//
// Since BOTH enums are present in detailed_report.json, a model-emitted
// out-of-enum value is schema-INVALID ⇒ base step-7 fires the sticky 1-shot
// repair (the Agent-4/5 repair-triggered class, NOT the Agent-6 terminal
// class). These Decode guards are the build-defect BACKSTOP for the
// schema↔model-drift case the repair loop cannot catch.
//
// Deliberately NOT guarded, each for a ratified reason (the mandatoryconditions/
// summary enumerated-unguarded-fields house style — the required written
// record even where the reason is "nothing to add"):
//
//   - ReportSection.Title / ReportItem.Title / ReportItem.Content — free
//     strings (schema maxLength only, base pre-Decode). Re-checking would
//     duplicate the schema and over-reach past the closed-enum boundary.
//   - ReportItem.ClauseRef / LegalBasis / LinkedRiskID / LinkedRecommendation
//     — nullable free *string. linked_risk_id EXISTENCE (does it reference a
//     real merged risk) is DOWNSTREAM, owned by the Result Aggregator
//     (LIC-TASK-035) which emits the orphan-ref warning — the exact
//     recommendation-Decode orphan-not-guarded precedent; Decode has no
//     access to the merged set anyway.
//   - DetailedReport.Warnings — passthrough VERBATIM, never stripped or
//     synthesised. ai-agents-pipeline.md:1390 makes the Result Aggregator
//     (LIC-TASK-035) the SINGLE owner of the final warnings merge; the agent
//     "оставляет warnings пустым ({})" but Decode stripping/normalising it
//     would be a forbidden TRANSFORM (the ratified Agent-3/4/5/6/7 "Decode is
//     a pure typed-unmarshal + drift-guard, never a transform / re-map /
//     synthesis" principle — forward note 6).
//
// Decode is therefore a PURE typed-unmarshal + closed-enum drift-guard — never
// a transform: no section reordering, no item synthesis, no warnings rewrite.
// Any such transformation belongs downstream (Result Aggregator / Reporting
// Engine), NEVER here.
func (detailedReporterSpec) Decode(content []byte) (port.AgentResult, error) {
	var r model.DetailedReport
	if err := json.Unmarshal(content, &r); err != nil {
		return nil, fmt.Errorf("detailedreport: decode DetailedReport: %w", err)
	}
	for i, sec := range r.Sections {
		if !sec.SectionCode.IsValid() {
			return nil, fmt.Errorf("detailedreport: sections[%d].section_code %q not in the 7-value report-section whitelist (schema/model drift)", i, sec.SectionCode)
		}
		for j, it := range sec.Items {
			if it.Severity != nil && !it.Severity.IsValid() {
				return nil, fmt.Errorf("detailedreport: sections[%d].items[%d].severity %q not in the high|medium|low whitelist (schema/model drift)", i, j, *it.Severity)
			}
		}
	}
	return &r, nil
}
