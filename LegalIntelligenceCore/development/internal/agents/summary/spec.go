package summary

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"contractpro/legal-intelligence-core/internal/agents/artifacts"
	"contractpro/legal-intelligence-core/internal/agents/base"
	"contractpro/legal-intelligence-core/internal/agents/promptbuilder"
	"contractpro/legal-intelligence-core/internal/domain/model"
	"contractpro/legal-intelligence-core/internal/domain/port"
)

// Per-agent input compaction for Agent 7 (ai-agents-pipeline.md §7
// "Зависимости": `EXTRACTED_TEXT (compact — head 4 000 + tail 1 000
// символов)`). This is Agent 7's OWN fixed-size CHARACTER compaction —
// categorically distinct from, and composable with, LIC-TASK-021's general
// per-model token head-60/tail-40 truncation, which operates upstream of
// Spec.Parts (base/seams.go MF-3). "символов" = characters: sliced by RUNE,
// never bytes, so a multi-byte Cyrillic codepoint is never split (the same
// rune-safe discipline as promptbuilder.capRunes / passthroughEstimator).
//
// This is a DELIBERATE byte-identical copy of typeclassifier's §1 compact
// (headRunes/tailRunes/elision/compact): §1 and §7 specify the SAME
// head-4000/tail-1000 rule, but typeclassifier's local copy is itself a
// recorded duplicate pending retirement (typeclassifier/CLAUDE.md
// forward-note #3, artifacts/CLAUDE.md "Later cleanup") — importing it would
// couple Agent 7 to a to-be-deleted symbol and breach this package's hermetic
// allowlist. Consolidating the §1/§7 compaction into a shared hermetic home
// (candidate: internal/agents/artifacts — where the EXTRACTED_TEXT decoder
// consolidation landed) is a DEFERRED cleanup, NOT this task's scope
// (re-touching reviewed Agent 1 = unrequested scope creep risking its pinned
// compaction tests — the exact artifacts/CLAUDE.md ratified stance). Recorded
// here and in CLAUDE.md so a reviewer reads the duplication as a deliberate
// deferral, not an oversight.
const (
	headRunes = 4000
	tailRunes = 1000
	// elision marks the gap between the head and tail fragments. The §7
	// prompt explicitly describes the block as "<contract_document>…
	// фрагменты текста …</contract_document>" (ai-agents-pipeline.md:1241),
	// so the two fragments MUST be visibly separated — fusing them would
	// misrepresent the contract's structure to the model. It is fixed,
	// non-injectable content (no &<> ⇒ unchanged by promptbuilder's
	// escapeText) and is the one deliberate place LIC-authored bytes enter
	// the escaped user block, justified because Agent 7's own §7 prompt
	// announces the fragment shaping (contrast capRunes, whose truncation is
	// silent and therefore marker-free).
	elision = "\n[…]\n"
)

// compact applies the §7 head/tail rule. When the text is short enough that
// the head and tail would meet or overlap (len ≤ headRunes+tailRunes) it is
// emitted verbatim with NO elision marker — inserting one into contiguous real
// text would be a lie.
func compact(full string) string {
	r := []rune(full)
	if len(r) <= headRunes+tailRunes {
		return full
	}
	return string(r[:headRunes]) + elision + string(r[len(r)-tailRunes:])
}

// classificationProjection is the MINIMAL §7 view of Agent 1's result. The §7
// prompt envelope block is an ellipsis (`<classification_result>…`,
// ai-agents-pipeline.md:1237) — NOT the explicit `{"contract_type":"…"}`
// literal Agent 5 had — so the whole-vs-projection call is made on the
// ratified Agent-4/5 token-budget + lowest-injection-vector precedent applied
// to a bare-ellipsis block: the §7 task needs only the contract-type label to
// phrase "это договор поставки…" (criterion 1); confidence / alternatives /
// rationale / prompt_injection_detected are irrelevant to a plain-language
// summary and emitting them is pure waste against the 1000-token output
// budget. The field is typed model.ContractType (NOT string) so a future
// enum-value rename is a compile error here too and the rendered value is a
// real enum member (the byte-exact single-key shape is pinned by
// TestSpec_Parts_ClassificationResultMinimalProjection).
type classificationProjection struct {
	ContractType model.ContractType `json:"contract_type"`
}

// summarizerSpec is the Agent-7 strategy. It is an empty struct with
// value-receiver methods — STATELESS and safe for concurrent use, as base.Spec
// mandates (one *BaseAgent, hence one Spec, is shared by the parallel errgroup
// pipeline; pinned by TestRun_ConcurrentRaceClean, -race).
type summarizerSpec struct{}

var _ base.Spec = summarizerSpec{}

// Parts builds the §7 envelope in the system-prompt's EXACT block order
// (ai-agents-pipeline.md:1236-1242 — the "ВХОДНЫЕ ДАННЫЕ" <input> section):
//
//	<input>
//	  <classification_result>{"contract_type":"…"} — minted from Agent 1</classification_result>
//	  <key_parameters>… full JSON KeyParameters from Agent 2 …</key_parameters>
//	  <risk_analysis>… full RAW Agent-5 RiskAnalysis …</risk_analysis>
//	  <mandatory_conditions_report>… full MandatoryConditionsReport from Agent 4 …</mandatory_conditions_report>
//	  <contract_document>… §7-compacted head+[…]+tail of EXTRACTED_TEXT …</contract_document>
//	</input>
//
// Wrong order risks the model reading the contract text as classification
// input. The §7 prompt's inner illustration form is not binding — the ratified
// promptbuilder "the prompt is SSOT, the illustration form is not" precedent;
// five Content parts in the literal block order are the faithful mapping. All
// five blocks are routed through promptbuilder.Content (escaped — prompt-
// injection defence layer 2, NON-NEGOTIABLE): the risk_analysis and
// mandatory_conditions_report blocks carry upstream-LLM-derived free text (a
// prompt-injected description / clause_ref / legal_basis / issue_description)
// and the contract_document is the raw contract body — a literal closing tag
// in any of them can never read as a block delimiter (pinned by
// TestSpec_Parts_Layer2Escaping + TestSpec_Parts_UpstreamJSONInjectionNeutralised).
// The classification_result block is the LOWEST-risk vector by design: it
// projects only the typed model.ContractType enum, which cannot carry
// attacker bytes.
//
// The shared Builder b is UNUSED: only Agent 3 mints a structural block
// (b.ValidationFacts) — hence the keyparams/typeclassifier/mandatoryconditions/
// riskdetection/recommendation `_` receiver-param, not Agent 3's named `b`.
// Agent 7 performs NO INN/OGRN validation and mints NO <validation_facts>. A
// returned error is a LIC programming/contract defect surfaced by base as
// INTERNAL_ERROR (never sent to the LLM).
//
// Input sourcing & strictness. The strictness-check order MIRRORS the envelope
// (the recommendation / mandatoryconditions CC-4 precedent). This is
// DELIBERATELY NOT the Agent-5 grouped-by-class structure: Agent 5 grouped by
// class because it had a THIRD, OPTIONAL strictness class (its OPTIONAL
// PROCESSING_WARNINGS); Agent 7 has only the TWO MANDATORY classes
// (pipeline-ordering then the single DM-artifact gate) and NO optional class,
// so envelope-mirroring keeps the two forward-note classes contiguous and
// legible — the recommendation CC-4 precedent, NOT the riskdetection CC-1
// divergence. A future reviewer must NOT regroup it.
//
//   - classification_result ← in.Classification.ContractType, the minimal
//     {"contract_type":"…"} projection. A nil in.Classification is a
//     PIPELINE-ORDERING invariant breach (Agent 1 runs Stage 1, Agent 7
//     Stage 5; the Stage Executor MUST populate it) ⇒ error ⇒ base
//     INTERNAL_ERROR (FORWARD NOTE 2 for LIC-TASK-034, DISTINCT from the
//     DM-artifact gate note 3). NO tolerated-empty case — the §7 prompt has
//     no "(если есть)" hedge and the envelope is a fixed 5-block shape. The
//     contract_type VALUE is trusted as Agent 1's typed, drift-guarded
//     output (the Agent-3/4/5 "trust the upstream typed result" precedent —
//     not re-validated here).
//   - key_parameters ← in.KeyParameters, marshalled WHOLE (incl.
//     internal_extras when present; a nil InternalExtras drops the key via
//     omitempty — the Agent-4/5 tolerance precedent). §7 "Зависимости" lists
//     KeyParameters with no `.field` selector. A nil in.KeyParameters is the
//     same pipeline-ordering breach (Agent 2 Stage 1) ⇒ error.
//   - risk_analysis ← in.RiskAnalysis, the RAW Agent-5 result, marshalled
//     WHOLE. This is the LOAD-BEARING sourcing decision and the DELIBERATE
//     divergence from the immediately-preceding Agent 6: §7 "Зависимости"
//     (ai-agents-pipeline.md:1175) lists a BARE `RiskAnalysis` with NO "со
//     всеми findings, включая встроенные из агентов 3, 4" qualifier —
//     whereas §6 EXPLICITLY carried that qualifier, which is why Agent 6
//     sources in.MergedRiskAnalysis. The §7 SSOT wording is THE decision
//     driver. (Corroborating only, not the rule: the §7 prompt cites no risk
//     ids, and §7 separately feeds mandatory_conditions_report as its own
//     block — so the post-merge R-MNNN/R-PNNN namespace is unused here and
//     sourcing merged would duplicate missing-condition content. These are
//     consequences of the SSOT call, not the call itself — the Agent-5
//     "the consumer doesn't use field X is not a sourcing driver" house
//     style.) Hence the nil check is on in.RiskAnalysis, NEVER the merged
//     field; a non-nil in.MergedRiskAnalysis does NOT satisfy this — the
//     Stage Executor must wire the RAW Agent-5 field (forward note 2). The
//     whole struct (incl. Summary + PromptInjectionDetected) is emitted, NOT
//     a projection — the deliberate asymmetry with the projected
//     classification_result (§7 "Зависимости" lists RiskAnalysis whole, no
//     `.field` selector — the Agent-4 whole-KeyParameters precedent). An
//     empty Risks[] (a clean contract, zero risks) is TOLERATED, marshalled
//     verbatim.
//   - mandatory_conditions_report ← in.MandatoryConditions, marshalled
//     WHOLE. §7 "Зависимости" lists MandatoryConditionsReport whole. A nil
//     in.MandatoryConditions is the same pipeline-ordering breach (Agent 4
//     Stage 3 → Agent 7 Stage 5) ⇒ error. A non-nil report with an empty
//     Conditions[] ("all mandatory conditions present" — a valid Agent-4
//     result) is TOLERATED, marshalled verbatim (the Agent-4/6
//     empty-slice tolerance analogue), no special-casing.
//   - contract_document ← EXTRACTED_TEXT, decoded via the shared DP-faithful
//     artifacts.ExtractedText + FullText() (the ratified reuse rule for
//     every EXTRACTED_TEXT consumer — artifacts/CLAUDE.md "Consumers"), then
//     §7-compacted (head 4000 + tail 1000 runes — compact above; UNLIKE
//     Agents 2/5 which emit the FULL text, Agent 7's §7 "Зависимости"
//     specifies this fixed-size rule). MANDATORY: absent / empty bytes /
//     malformed JSON / decodes-to-empty-or-whitespace ⇒ error.
//
// FORWARD NOTE 3 (owner: LIC-TASK-034 — the artifact-bundle gate, DISTINCT
// from the pipeline-ordering note 2 above and identical in spirit to
// typeclassifier #2 / keyparams / partyconsistency #3 / mandatoryconditions
// #3 / riskdetection #3). A genuinely missing/empty EXTRACTED_TEXT artifact
// is semantically DM_ARTIFACTS_MISSING (retryable, "данные документа не
// найдены"), NOT INTERNAL_ERROR. That bundle gate MUST reject it BEFORE
// Agent 7 runs; reaching this Parts check then means a true LIC invariant
// breach, for which base's Parts-error→INTERNAL_ERROR projection is the
// CORRECT code. base is not modified by this task.
func (summarizerSpec) Parts(_ *promptbuilder.Builder, in model.AgentInput) ([]promptbuilder.Part, error) {
	// --- strictness phase: all checks BEFORE the assembly/marshal phase.
	// Order MIRRORS the envelope (recommendation/mandatoryconditions CC-4):
	// the four pipeline-ordering breaches (forward note 2) first, then the
	// single DM-artifact-gate check (note 3). ---

	if in.Classification == nil {
		return nil, errors.New("summary: mandatory upstream ClassificationResult (Agent 1) result is absent")
	}
	if in.KeyParameters == nil {
		return nil, errors.New("summary: mandatory upstream KeyParameters (Agent 2) result is absent")
	}
	if in.RiskAnalysis == nil {
		// RAW Agent-5 RiskAnalysis, NOT merged (the §7 vs §6 SSOT
		// divergence — the load-bearing sourcing decision). A non-nil
		// in.MergedRiskAnalysis does NOT satisfy this: the Stage Executor
		// must wire the raw Agent-5 field for Agent 7 (forward note 2).
		return nil, errors.New("summary: mandatory upstream RiskAnalysis (raw Agent-5, NOT MergedRiskAnalysis) result is absent")
	}
	if in.MandatoryConditions == nil {
		return nil, errors.New("summary: mandatory upstream MandatoryConditionsReport (Agent 4) result is absent")
	}

	rawText, ok := in.Artifacts[model.ArtifactExtractedText]
	if !ok || len(rawText) == 0 {
		return nil, errors.New("summary: mandatory EXTRACTED_TEXT artifact is absent")
	}
	var et artifacts.ExtractedText
	if err := json.Unmarshal(rawText, &et); err != nil {
		return nil, fmt.Errorf("summary: decode EXTRACTED_TEXT artifact: %w", err)
	}
	full := et.FullText()
	if strings.TrimSpace(full) == "" {
		return nil, errors.New("summary: EXTRACTED_TEXT artifact decoded to empty text")
	}

	// --- assembly phase: envelope ORDER (ai-agents-pipeline.md:1236-1242) ---

	// classification_result: minimal {"contract_type":"…"} projection.
	clsJSON, err := json.Marshal(classificationProjection{ContractType: in.Classification.ContractType})
	if err != nil {
		// model.ContractType is a string ⇒ Marshal cannot fail in practice;
		// a failure is a LIC build defect (base → INTERNAL_ERROR).
		return nil, fmt.Errorf("summary: marshal classification_result: %w", err)
	}
	kpJSON, err := json.Marshal(in.KeyParameters)
	if err != nil {
		return nil, fmt.Errorf("summary: marshal key_parameters: %w", err)
	}
	raJSON, err := json.Marshal(in.RiskAnalysis)
	if err != nil {
		return nil, fmt.Errorf("summary: marshal risk_analysis: %w", err)
	}
	mcJSON, err := json.Marshal(in.MandatoryConditions)
	if err != nil {
		return nil, fmt.Errorf("summary: marshal mandatory_conditions_report: %w", err)
	}

	return []promptbuilder.Part{
		promptbuilder.Content("classification_result", string(clsJSON)),
		promptbuilder.Content("key_parameters", string(kpJSON)),
		promptbuilder.Content("risk_analysis", string(raJSON)),
		promptbuilder.Content("mandatory_conditions_report", string(mcJSON)),
		promptbuilder.Content("contract_document", compact(full)),
	}, nil
}

// Decode unmarshals the schema-validated response into *model.Summary (the
// concrete port.AgentResult the Stage Executor narrows by AgentID).
//
// base schema-validates the bytes against the embedded summary.json BEFORE
// calling Decode (MF-1). The §7 schema FULLY constrains the only field —
// text: {"type":"string","minLength":200,"maxLength":3000} — so there is
// genuinely NOTHING left for Decode to enforce. This is the
// "enumerated-unguarded — there is nothing to guard" terminal case, the
// riskdetection-Risk.ID-NOT-guarded / mandatoryconditions-unguarded-fields
// house style (NOT the Agent-6 "schema is silent ⇒ Decode is the sole/
// terminal guard" class — unlike §6's risk_id, the §7 schema is NOT silent
// on its only field, so no sole/terminal Decode guard exists to add):
//
//   - model.Summary.Text — a structurally FREE string whose 200..3000 bound
//     is the SCHEMA's SSOT, validated by base pre-Decode (MF-1;
//     report.go:5 even states the bound is "enforced by the schema
//     validator, not at the type level"). Re-checking the length here would
//     DUPLICATE the schema and over-reach PAST the ratified closed-enum/
//     frozen-SSOT guard boundary — a future reviewer must NOT add a "safety"
//     length re-check (this explicit single-field enumeration is the house
//     style's required written record even though the set is a singleton).
//
// Decode is therefore a PURE typed-unmarshal — never a transform / re-map /
// synthesis (the ratified Agent-3/4/5/6 principle): no trimming, no
// jargon-stripping, no prose post-processing. Any such transformation belongs
// to the Reporting Engine downstream, NEVER here.
func (summarizerSpec) Decode(content []byte) (port.AgentResult, error) {
	var s model.Summary
	if err := json.Unmarshal(content, &s); err != nil {
		return nil, fmt.Errorf("summary: decode Summary: %w", err)
	}
	return &s, nil
}
