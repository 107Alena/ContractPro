package mandatoryconditions

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

// checkerSpec is the Agent-4 strategy. It is an empty struct with
// value-receiver methods — STATELESS and safe for concurrent use, as base.Spec
// mandates (one *BaseAgent, hence one Spec, is shared by the parallel errgroup
// pipeline; pinned by TestRun_ConcurrentRaceClean, -race).
type checkerSpec struct{}

var _ base.Spec = checkerSpec{}

// classificationProjection is the MINIMAL §4 view of Agent 1's result. §4
// "Зависимости" depends on `ClassificationResult.contract_type` (the FIELD,
// deliberately asymmetric with `KeyParameters` which §4 takes WHOLE — see
// Parts), and the §4 prompt envelope literal is exactly
// `<classification_result>{"contract_type":"…"}</classification_result>`
// (mandatory_conditions.txt:118). confidence/alternatives/rationale/
// prompt_injection_detected are irrelevant to a mandatory-conditions check and
// emitting them is pure waste against the 3000-token output budget — so this
// is the exact analogue of Agent 3 projecting only
// internal_extras.party_roles, NOT the whole struct. The field is typed
// model.ContractType (NOT string) so a future enum-value rename is a compile
// error here too and the rendered value is a real enum member
// (code-architect D1; the byte-exact single-key shape is pinned by
// TestSpec_Parts_ClassificationResultMinimalProjection — CC-1, the
// partyconsistency representative-tag-trap analogue).
type classificationProjection struct {
	ContractType model.ContractType `json:"contract_type"`
}

// Parts builds the §4 envelope in the system-prompt's EXACT block order
// (mandatory_conditions.txt:117-122):
//
//	<input>
//	  <classification_result>{"contract_type":"…"} — minted from Agent 1</classification_result>
//	  <key_parameters>… full JSON KeyParameters from Agent 2 …</key_parameters>
//	  <semantic_tree>… raw SEMANTIC_TREE JSON, byte-faithful passthrough …</semantic_tree>
//	  <contract_document>… full EXTRACTED_TEXT …</contract_document>
//	</input>
//
// Wrong order risks the model reading the tree/text as classification input.
// The §4 prompt's inner illustration form is not binding — the ratified
// promptbuilder "the prompt is SSOT, the illustration form is not" precedent;
// four Content parts in the literal block order are the faithful mapping. All
// four blocks are routed through promptbuilder.Content (escaped — prompt-
// injection defence layer 2): a literal closing tag planted in the contract
// body, the SEMANTIC_TREE, or an upstream-LLM-derived string carried inside
// KeyParameters (e.g. a prompt-injected `subject`/`rationale`) can never read
// as a block delimiter (CC-2; pinned by TestSpec_Parts_Layer2Escaping +
// TestSpec_Parts_UpstreamJSONInjectionNeutralised). The classification_result
// block is the LOWEST-risk vector by design: D1 projects only the typed
// model.ContractType enum, which cannot carry attacker bytes.
//
// The shared Builder b is UNUSED: only Agent 3 mints a structural block
// (b.ValidationFacts) — hence the keyparams/typeclassifier `_` receiver-param,
// not Agent 3's named `b` (code-architect D5). Agent 4 performs NO INN/OGRN
// validation and mints NO <validation_facts>: KeyParameters.internal_extras
// (incl. raw, unvalidated party_roles[].INN/OGRN — checksum validation is
// solely Agent 3's pre-LLM job) is passed through verbatim as part of the
// whole-KeyParameters block (code-architect CC-6). A returned error is a LIC
// programming/contract defect surfaced by base as INTERNAL_ERROR (never sent
// to the LLM).
//
// Input sourcing & strictness (code-architect D1/D2/D3; check order mirrors
// the envelope so the two DISTINCT forward-note classes — pipeline-ordering
// vs. DM-artifact-gate — stay contiguous and legible in the code itself,
// CC-4):
//
//   - classification_result ← in.Classification.ContractType. A nil
//     in.Classification is a PIPELINE-ORDERING invariant breach (Agent 1 runs
//     Stage 1, Agent 4 Stage 3; the Stage Executor MUST populate it) ⇒ error
//     ⇒ base INTERNAL_ERROR (FORWARD NOTE 2 for LIC-TASK-034, DISTINCT from
//     the DM-artifact gate note 3). There is NO tolerated-empty case (unlike
//     Agent 3's "supplementary" empty party_roles): contract_type is the
//     literal driver of WHICH mandatory-condition catalogue applies; the §4
//     prompt has no "(если есть)" hedge. The contract_type VALUE is trusted
//     as Agent 1's typed, drift-guarded output (the Agent-3 "trust the
//     upstream typed result" precedent — not re-validated here).
//   - key_parameters ← in.KeyParameters, marshalled WHOLE (incl.
//     internal_extras when present — Agent 4 is a documented internal_extras
//     consumer, key_parameters.go godoc). A nil in.KeyParameters is the same
//     pipeline-ordering breach (Agent 2 Stage 1 → Agent 4 Stage 3) ⇒ error
//     ⇒ INTERNAL_ERROR (forward note 2). A non-nil KeyParameters with nil
//     InternalExtras is TOLERATED (a valid minimal Agent-2 response;
//     `internal_extras` carries omitempty so the key is simply dropped — the
//     keyparams Decode tolerance analogue), no special-casing.
//   - semantic_tree ← SEMANTIC_TREE, BYTE-FAITHFUL passthrough (string(raw),
//     NOT decoded/re-encoded — the §4 agent cites tree node "id" values as
//     found_in clause_ref, so pruning/re-keying would strip the very ids it
//     must reference; the exact Agent-2 precedent). Strictness gate is
//     well-formedness (json.Valid), never a structural decode. Mandatory:
//     absent / empty bytes / !json.Valid ⇒ error. An empty-but-well-formed
//     tree is TOLERATED — emitted verbatim (the model still reads
//     contract_document; the tree only supplies clause_ref ids).
//   - contract_document ← EXTRACTED_TEXT via the shared artifacts.ExtractedText
//     decoder, emitted in FULL with NO Agent-4-side compaction. §4 specifies
//     NO fixed-size or token-bounded head/tail rule (unlike §1's Agent-1
//     "head 4000 + tail 1000"). The only truncation is LIC-TASK-021's GENERIC
//     per-artifact rule UPSTREAM of Spec.Parts on model.AgentInput
//     (base/seams.go MF-3 / keyparams precedent). Carrying an interim
//     truncation here would be a second divergent truncation site 021 must
//     later find and remove. EXTRACTED_TEXT is MANDATORY: absent / malformed
//     JSON / decodes-to-empty-or-whitespace ⇒ error. FORWARD NOTE 4 for
//     LIC-TASK-021: Agent 4 deliberately emits the full EXTRACTED_TEXT.
//
// FORWARD NOTE 3 (owner: LIC-TASK-034 — the artifact-bundle gate, DISTINCT
// from the pipeline-ordering note 2 above and identical in spirit to
// typeclassifier #2 / keyparams / partyconsistency #3). A genuinely
// missing/empty mandatory DM artifact (SEMANTIC_TREE / EXTRACTED_TEXT) is
// semantically DM_ARTIFACTS_MISSING (retryable, "данные документа не
// найдены"), NOT INTERNAL_ERROR. That bundle gate MUST reject it BEFORE
// Agent 4 runs; reaching these Parts checks then means a true LIC invariant
// breach, for which base's Parts-error→INTERNAL_ERROR projection is the
// CORRECT code. base is not modified by this task.
func (checkerSpec) Parts(_ *promptbuilder.Builder, in model.AgentInput) ([]promptbuilder.Part, error) {
	// --- strictness phase: all checks BEFORE the assembly/marshal phase.
	// Order mirrors the envelope (CC-4): the two pipeline-ordering breaches
	// (forward note 2) first, then the two DM-artifact-gate checks (note 3).

	if in.Classification == nil {
		return nil, errors.New("mandatoryconditions: mandatory upstream ClassificationResult (Agent 1) result is absent")
	}
	if in.KeyParameters == nil {
		return nil, errors.New("mandatoryconditions: mandatory upstream KeyParameters (Agent 2) result is absent")
	}

	rawTree, ok := in.Artifacts[model.ArtifactSemanticTree]
	if !ok || len(rawTree) == 0 {
		return nil, errors.New("mandatoryconditions: mandatory SEMANTIC_TREE artifact is absent")
	}
	if !json.Valid(rawTree) {
		return nil, errors.New("mandatoryconditions: SEMANTIC_TREE artifact is not well-formed JSON")
	}

	rawText, ok := in.Artifacts[model.ArtifactExtractedText]
	if !ok || len(rawText) == 0 {
		return nil, errors.New("mandatoryconditions: mandatory EXTRACTED_TEXT artifact is absent")
	}
	var et artifacts.ExtractedText
	if err := json.Unmarshal(rawText, &et); err != nil {
		return nil, fmt.Errorf("mandatoryconditions: decode EXTRACTED_TEXT artifact: %w", err)
	}
	full := et.FullText()
	if strings.TrimSpace(full) == "" {
		return nil, errors.New("mandatoryconditions: EXTRACTED_TEXT artifact decoded to empty text")
	}

	// --- assembly phase ---

	// classification_result: minimal {"contract_type":"…"} projection (D1).
	clsJSON, err := json.Marshal(classificationProjection{ContractType: in.Classification.ContractType})
	if err != nil {
		// model.ContractType is a string ⇒ Marshal cannot fail in practice;
		// a failure is a LIC build defect (base → INTERNAL_ERROR).
		return nil, fmt.Errorf("mandatoryconditions: marshal classification_result: %w", err)
	}

	// key_parameters: the WHOLE KeyParameters JSON, incl. internal_extras when
	// present (D2 — Agent 4 is a documented internal_extras consumer).
	kpJSON, err := json.Marshal(in.KeyParameters)
	if err != nil {
		return nil, fmt.Errorf("mandatoryconditions: marshal key_parameters: %w", err)
	}

	return []promptbuilder.Part{
		promptbuilder.Content("classification_result", string(clsJSON)),
		promptbuilder.Content("key_parameters", string(kpJSON)),
		promptbuilder.Content("semantic_tree", string(rawTree)),
		promptbuilder.Content("contract_document", full),
	}, nil
}

// Decode unmarshals the schema-validated response into
// *model.MandatoryConditionsReport (the concrete port.AgentResult the Stage
// Executor narrows by AgentID).
//
// base schema-validates the bytes against the embedded mandatory_conditions.json
// BEFORE calling Decode (MF-1); that schema already constrains conditions[].code
// to the `^MC_[A-Z0-9_]+$` pattern and conditions[].status to the 3-value enum.
// The explicit per-condition re-checks are the codebase's enumerated
// schema-↔-Go cross-check house style (cf. base.canonicalStage, classifier's
// ContractType.IsValid, keyparams' PartyRoleType.IsValid, partyconsistency's
// dual finding guard): they pin the schema ↔ model SSOT seam, turning a silent
// mandatory_conditions.json ↔ model drift into a loud INTERNAL_ERROR
// build-defect signal — precisely Decode's contract ("schema and Go struct
// disagree ⇒ LIC build defect").
//
// A MandatoryCondition has TWO guarded SSOT surfaces (code-architect D4/CC-3):
//
//   - Status — model.MandatoryConditionStatus, a CLOSED 3-value enum
//     (FOUND_OK|FOUND_AMBIGUOUS|MISSING) with IsValid().
//   - Code — guarded by model.IsValidMandatoryConditionCode, the FROZEN
//     `^MC_[A-Z0-9_]+$` SSOT regex (model/mandatory_conditions.go). Code is
//     NOT a free string: it has a closed STRUCTURAL SSOT (a regex) duplicated
//     into the schema's `pattern`, so this guard is the EXACT same schema↔Go
//     cross-check class as an enum guard — not "free-string over-reach" — AND
//     it directly discharges acceptance test_step 2 ("code соответствует
//     regex ^MC_[A-Z0-9_]+$") in Go, not only via the schema.
//
// MandatoryConditionsReport.ContractType is deliberately NOT guarded: the §4
// output schema leaves it a bare {"type":"string"} (the model may echo OTHER
// or a refined string) and the model field is plain string — no closed
// value-set, no SSOT regex. Guarding it would over-reach past the
// keyparams/partyconsistency-ratified boundary (free or model-reported fields
// are not guarded), AND would wrongly reject schema-valid output. Note the
// deliberate asymmetry with Parts: Parts RENDERS contract_type from the typed
// model.ContractType enum (Agent 1's drift-guarded output), but Decode does
// NOT re-validate the model's RETURNED contract_type, because §4's schema
// makes it free. Label / LegalBasis / IssueDescription / Summary /
// PromptInjectionDetected are likewise structurally free / model-reported and
// NOT guarded. Decode is a pure typed-unmarshal + drift-guard, never a
// transform (no status/code re-mapping).
func (checkerSpec) Decode(content []byte) (port.AgentResult, error) {
	var r model.MandatoryConditionsReport
	if err := json.Unmarshal(content, &r); err != nil {
		return nil, fmt.Errorf("mandatoryconditions: decode MandatoryConditionsReport: %w", err)
	}
	for i, c := range r.Conditions {
		if !model.IsValidMandatoryConditionCode(c.Code) {
			return nil, fmt.Errorf("mandatoryconditions: conditions[%d].code %q does not match the frozen ^MC_[A-Z0-9_]+$ format (schema/model drift)", i, c.Code)
		}
		if !c.Status.IsValid() {
			return nil, fmt.Errorf("mandatoryconditions: conditions[%d].status %q not in the FOUND_OK|FOUND_AMBIGUOUS|MISSING whitelist (schema/model drift)", i, c.Status)
		}
	}
	return &r, nil
}
