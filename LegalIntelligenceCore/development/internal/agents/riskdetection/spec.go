package riskdetection

import (
	"bytes"
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

// detectorSpec is the Agent-5 strategy. It is an empty struct with
// value-receiver methods — STATELESS and safe for concurrent use, as base.Spec
// mandates (one *BaseAgent, hence one Spec, is shared by the parallel errgroup
// pipeline; pinned by TestRun_ConcurrentRaceClean, -race).
type detectorSpec struct{}

var _ base.Spec = detectorSpec{}

// emptyProcessingWarnings is the byte sentinel emitted into the
// <processing_warnings> block when the optional PROCESSING_WARNINGS artifact
// is absent / empty / the bare JSON `null` token (code-architect D3 + CC-2).
// It is the literal JSON empty ARRAY (NOT `{}`, NOT `null`, NOT ""): the §5
// prompt envelope illustrates the block as `[…]` (risk_detection.txt:55), so
// `[]` is the type-consistent "no warnings, clean text" value the model reads
// unambiguously — `{}`/`null` would invite the model to treat the payload as
// degenerate and spuriously bump every risk to level=medium
// (risk_detection.txt:60-62 ties the medium-escalation to the PRESENCE of
// warnings). This is the partyconsistency "never emit JSON null, always a
// well-formed [] the model can read" ratified rule (code-architect CC-3).
var emptyProcessingWarnings = []byte("[]")

// classificationProjection is the MINIMAL §5 view of Agent 1's result. §5
// "Зависимости" depends on `ClassificationResult.contract_type` (the FIELD,
// deliberately asymmetric with `KeyParameters` which §5 takes WHOLE — see
// Parts), and the §5 prompt envelope literal is exactly
// `<classification_result>{"contract_type":"…"}</classification_result>`
// (risk_detection.txt:53). confidence/alternatives/rationale/
// prompt_injection_detected are irrelevant to a risk-detection pass and
// emitting them is pure waste against the 3500-token output budget — the
// exact analogue of Agent 4's identical minimal projection. The field is
// typed model.ContractType (NOT string) so a future enum-value rename is a
// compile error here too and the rendered value is a real enum member
// (code-architect D1; the byte-exact single-key shape is pinned by
// TestSpec_Parts_ClassificationResultMinimalProjection).
type classificationProjection struct {
	ContractType model.ContractType `json:"contract_type"`
}

// Parts builds the §5 envelope in the system-prompt's EXACT block order
// (risk_detection.txt:52-58 — the "ВХОДНЫЕ ДАННЫЕ" <input> section):
//
//	<input>
//	  <classification_result>{"contract_type":"…"} — minted from Agent 1</classification_result>
//	  <key_parameters>… full JSON KeyParameters from Agent 2 …</key_parameters>
//	  <processing_warnings>… raw PROCESSING_WARNINGS JSON, or [] when absent …</processing_warnings>
//	  <semantic_tree>… raw SEMANTIC_TREE JSON, byte-faithful passthrough …</semantic_tree>
//	  <contract_document>… full EXTRACTED_TEXT …</contract_document>
//	</input>
//
// Wrong order risks the model reading the tree/text as classification input.
// The §5 prompt's inner illustration form is not binding — the ratified
// promptbuilder "the prompt is SSOT, the illustration form is not" precedent;
// five Content parts in the literal block order are the faithful mapping. All
// five blocks are routed through promptbuilder.Content (escaped — prompt-
// injection defence layer 2): a literal closing tag planted in the contract
// body, the SEMANTIC_TREE, the PROCESSING_WARNINGS, or an upstream-LLM-derived
// string carried inside KeyParameters (e.g. a prompt-injected
// subject/rationale) can never read as a block delimiter (code-architect CC-2;
// pinned by TestSpec_Parts_Layer2Escaping +
// TestSpec_Parts_UpstreamJSONInjectionNeutralised). The classification_result
// block is the LOWEST-risk vector by design: D1 projects only the typed
// model.ContractType enum, which cannot carry attacker bytes.
//
// The shared Builder b is UNUSED: only Agent 3 mints a structural block
// (b.ValidationFacts) — hence the keyparams/typeclassifier/mandatoryconditions
// `_` receiver-param, not Agent 3's named `b` (code-architect D1). Agent 5
// performs NO INN/OGRN validation and mints NO <validation_facts>. A returned
// error is a LIC programming/contract defect surfaced by base as
// INTERNAL_ERROR (never sent to the LLM).
//
// Input sourcing & strictness (code-architect D1/D2/D3, CC-1). The strictness
// phase is grouped BY CLASS — NOT mirroring the envelope order — because
// Agent 5 has THREE distinct strictness classes where Agent 4 had only two
// (mandatoryconditions CC-4 could mirror the envelope only because all four of
// its blocks were mandatory; Agent 5 cannot, and this divergence is
// deliberate — CLAUDE.md / code-architect CC-1):
//
//   - PIPELINE-ORDERING (forward note 2): classification_result ←
//     in.Classification.ContractType, key_parameters ← in.KeyParameters
//     (whole, incl. internal_extras when present). A nil in.Classification OR
//     a nil in.KeyParameters is a pipeline-ordering invariant breach (Agents
//     1/2 run Stage 1, Agent 5 Stage 3; the Stage Executor MUST populate
//     them) ⇒ error ⇒ base INTERNAL_ERROR. NO tolerated-empty case:
//     contract_type drives the risk taxonomy and the §5 prompt has no
//     "(если есть)" hedge for it. The contract_type VALUE is trusted as Agent
//     1's typed, drift-guarded output (the Agent-3/4 "trust the upstream typed
//     result" precedent — not re-validated here).
//   - MANDATORY DM-ARTIFACT (forward note 3): semantic_tree ← SEMANTIC_TREE,
//     contract_document ← EXTRACTED_TEXT. Both MANDATORY: absent / empty bytes
//     / not-well-formed (tree) / malformed-or-empty-after-trim (text) ⇒ error.
//     semantic_tree is a BYTE-FAITHFUL passthrough (string(raw), NOT
//     decoded/re-encoded — the §5 agent cites tree node "id" values as
//     clause_ref per risk_detection.txt:71, so pruning/re-keying would strip
//     the very ids it must reference; the Agent-2/4 precedent). Its strictness
//     gate is well-formedness (json.Valid), never a structural decode; an
//     empty-but-well-formed tree ({}) is TOLERATED, emitted verbatim.
//     contract_document is the shared artifacts.ExtractedText decoder emitted
//     in FULL with NO Agent-5-side compaction: §5 specifies NO fixed-size or
//     token-bounded head/tail rule (unlike §1's Agent-1 "head 4000 + tail
//     1000"); the only truncation is LIC-TASK-021's GENERIC per-artifact rule
//     UPSTREAM of Spec.Parts on model.AgentInput (base/seams.go MF-3 /
//     keyparams precedent). FORWARD NOTE 4 for LIC-TASK-021: Agent 5
//     deliberately emits the full EXTRACTED_TEXT.
//   - OPTIONAL (new Agent-5 forward note 5, NEITHER note 2 NOR note 3):
//     processing_warnings ← PROCESSING_WARNINGS. §5 marks it explicitly
//     OPTIONAL ("<!-- от DP, опционально -->" risk_detection.txt:55; "Если
//     присутствует processing_warnings" risk_detection.txt:60) — a clean text
//     PDF legitimately has none. So its ABSENCE is a normal in-spec state,
//     TOLERATED (the typeclassifier-empty-DOCUMENT_STRUCTURE /
//     partyconsistency-empty-party_roles supplementary-signal precedent), NOT
//     a DM-artifact-gate concern (note 3 covers only the MANDATORY tree/text).
//     Tiers: absent / empty bytes / the bare JSON `null` token (json.Valid
//     accepts `null` but it is a type-mismatched degenerate payload — CC-2) ⇒
//     normalised to the literal `[]` so the fixed 5-block envelope shape stays
//     invariant regardless of presence (the partyconsistency fixed-N-block
//     CHANGE-2 precedent); present & json.Valid ⇒ byte-faithful passthrough
//     verbatim (the SEMANTIC_TREE precedent — Agent 5 never cites ids from it,
//     it is OCR-quality context only); present & !json.Valid ⇒ a corrupt
//     PRESENT artifact is a defect ⇒ error (the SEMANTIC_TREE
//     well-formedness-gate precedent; distinct from "absent", tolerated here
//     because §5 marks it optional and §4's tree was not).
//
// FORWARD NOTE 3 (owner: LIC-TASK-034 — the artifact-bundle gate, DISTINCT
// from the pipeline-ordering note 2 and identical in spirit to typeclassifier
// #2 / keyparams / partyconsistency #3 / mandatoryconditions #3). A genuinely
// missing/empty MANDATORY DM artifact (SEMANTIC_TREE / EXTRACTED_TEXT) is
// semantically DM_ARTIFACTS_MISSING (retryable, "данные документа не
// найдены"), NOT INTERNAL_ERROR. That bundle gate MUST reject it BEFORE Agent
// 5 runs; reaching these Parts checks then means a true LIC invariant breach,
// for which base's Parts-error→INTERNAL_ERROR projection is the CORRECT code.
// The OPTIONAL PROCESSING_WARNINGS is explicitly OUT of that gate (note 5).
// base is not modified by this task.
func (detectorSpec) Parts(_ *promptbuilder.Builder, in model.AgentInput) ([]promptbuilder.Part, error) {
	// --- strictness phase: grouped BY CLASS, not by envelope order (CC-1) ---

	// Class 1 — pipeline-ordering (forward note 2).
	if in.Classification == nil {
		return nil, errors.New("riskdetection: mandatory upstream ClassificationResult (Agent 1) result is absent")
	}
	if in.KeyParameters == nil {
		return nil, errors.New("riskdetection: mandatory upstream KeyParameters (Agent 2) result is absent")
	}

	// Class 2 — mandatory DM artifacts (forward note 3).
	rawTree, ok := in.Artifacts[model.ArtifactSemanticTree]
	if !ok || len(rawTree) == 0 {
		return nil, errors.New("riskdetection: mandatory SEMANTIC_TREE artifact is absent")
	}
	if !json.Valid(rawTree) {
		return nil, errors.New("riskdetection: SEMANTIC_TREE artifact is not well-formed JSON")
	}

	rawText, ok := in.Artifacts[model.ArtifactExtractedText]
	if !ok || len(rawText) == 0 {
		return nil, errors.New("riskdetection: mandatory EXTRACTED_TEXT artifact is absent")
	}
	var et artifacts.ExtractedText
	if err := json.Unmarshal(rawText, &et); err != nil {
		return nil, fmt.Errorf("riskdetection: decode EXTRACTED_TEXT artifact: %w", err)
	}
	full := et.FullText()
	if strings.TrimSpace(full) == "" {
		return nil, errors.New("riskdetection: EXTRACTED_TEXT artifact decoded to empty text")
	}

	// Class 3 — OPTIONAL PROCESSING_WARNINGS (forward note 5; NOT note 3).
	// absent / empty / bare `null` token ⇒ normalise to `[]`; present &
	// well-formed ⇒ byte-faithful verbatim; present & malformed ⇒ error.
	pw := emptyProcessingWarnings
	if rawPW, present := in.Artifacts[model.ArtifactProcessingWarnings]; present {
		trimmed := bytes.TrimSpace(rawPW)
		switch {
		case len(trimmed) == 0 || string(trimmed) == "null":
			// keep the [] sentinel (CC-2: json.Valid("null")==true, but a bare
			// null is a degenerate non-array payload — fold into the
			// absent/empty tier, never emit JSON null).
		case !json.Valid(rawPW):
			return nil, errors.New("riskdetection: PROCESSING_WARNINGS artifact is present but not well-formed JSON")
		default:
			pw = rawPW // byte-faithful passthrough verbatim
		}
	}

	// --- assembly phase: envelope ORDER (risk_detection.txt:52-58) ---

	// classification_result: minimal {"contract_type":"…"} projection (D1).
	clsJSON, err := json.Marshal(classificationProjection{ContractType: in.Classification.ContractType})
	if err != nil {
		// model.ContractType is a string ⇒ Marshal cannot fail in practice;
		// a failure is a LIC build defect (base → INTERNAL_ERROR).
		return nil, fmt.Errorf("riskdetection: marshal classification_result: %w", err)
	}

	// key_parameters: the WHOLE KeyParameters JSON, incl. internal_extras when
	// present (D1 — the deliberate Agent-4 asymmetry with the projected
	// classification_result; a nil InternalExtras is dropped via omitempty).
	kpJSON, err := json.Marshal(in.KeyParameters)
	if err != nil {
		return nil, fmt.Errorf("riskdetection: marshal key_parameters: %w", err)
	}

	return []promptbuilder.Part{
		promptbuilder.Content("classification_result", string(clsJSON)),
		promptbuilder.Content("key_parameters", string(kpJSON)),
		promptbuilder.Content("processing_warnings", string(pw)),
		promptbuilder.Content("semantic_tree", string(rawTree)),
		promptbuilder.Content("contract_document", full),
	}, nil
}

// Decode unmarshals the schema-validated response into *model.RiskAnalysis
// (the concrete port.AgentResult the Stage Executor narrows by AgentID).
//
// base schema-validates the bytes against the embedded risk_detection.json
// BEFORE calling Decode (MF-1); that schema already constrains risks[].id to
// `^R-[0-9]{3,}$`, risks[].level to the 3-value enum, and the OPTIONAL
// risks[].category to the narrow 13-value Agent-5 subset. The explicit
// per-risk Level re-check is the codebase's enumerated schema-↔-Go cross-check
// house style (cf. base.canonicalStage, classifier's ContractType.IsValid,
// keyparams' PartyRoleType.IsValid, partyconsistency's f.Severity.IsValid,
// mandatoryconditions' dual guard): it pins the schema-enum ↔ Go-whitelist
// SSOT seam, turning a silent risk_detection.json ↔ model drift into a loud
// INTERNAL_ERROR build-defect signal — precisely Decode's contract ("schema
// and Go struct disagree ⇒ LIC build defect").
//
// ONLY Risk.Level is guarded (code-architect D4) — the SOLE closed-enum SSOT
// surface whose Go whitelist (model.RiskLevel.IsValid, high|medium|low)
// EXACTLY equals the §5 schema enum (the partyconsistency f.Severity
// precedent, same model.RiskLevel type). Deliberately NOT guarded, each for a
// ratified reason — enumerated so a future reviewer does not add an
// over-reaching guard (the mandatoryconditions enumerated-unguarded-fields
// house style; code-architect CC-6):
//
//   - Risk.Category — the §5 schema makes it OPTIONAL (not in required[]) and
//     uses the NARROW 13-value Agent-5 subset, whereas model.RiskCategory
//     .IsValid() is the BROADER 22-value MERGED OUTBOUND enum (13 Agent-5 + 7
//     Agent-3 PARTY_* + 2 Agent-4 MANDATORY_*; risk_analysis.go). It is
//     therefore LOOSER than Agent 5's input schema — guarding with it would
//     NOT be a faithful schema↔Go cross-check (a PARTY_*/MANDATORY_* value
//     would wrongly pass an "Agent-5 output" guard) AND, because category is
//     optional, an omitted category decodes to "" which fails IsValid() ⇒
//     would wrongly REJECT schema-valid output. This is the exact Agent-4
//     MandatoryConditionsReport.ContractType "don't guard free/optional,
//     don't wrongly reject schema-valid output" boundary.
//   - Risk.ID — the §5 schema pattern is `^R-[0-9]{3,}$`, but
//     model.IsValidRiskID is the MERGED `^R-(P|M)?[0-9]{3,}$` (it admits
//     R-PNNN/R-MNNN that §5's schema FORBIDS) — strictly LOOSER than the
//     agent schema. Unlike Agent 4 (whose frozen model SSOT regex
//     model.IsValidMandatoryConditionCode EQUALS its schema pattern, so the
//     guard was a faithful cross-check), NO model SSOT regex equals
//     `^R-[0-9]{3,}$`; a hand-rolled local regex would be a new un-SSOT'd
//     duplicate — over-reach past the ratified closed-enum/frozen-SSOT
//     boundary. The schema `pattern` (base, pre-Decode, MF-1) is the id-format
//     SSOT. Acceptance test_step 2 ("ids монотонно растут R-001, R-002, …")
//     is a SEQUENCE property no per-element guard can express — discharged via
//     schema pre-validation + the Run-integration fixture assertion (the
//     mandatoryconditions/partyconsistency "Run integration (acceptance Шаг
//     1/2)" precedent).
//   - Risk.Description / Risk.ClauseRef / Risk.LegalBasis — free strings
//     (schema maxLength only). Risk.Rationale — nullable *string, internal LLM
//     metadata STRIPPED downstream by the Result Aggregator (LIC-TASK-035,
//     risk_analysis.go); never Agent-5's to guard. Risk.MandatoryConditionCode
//     — *string the Aggregator populates ONLY for R-MNNN, never Agent 5.
//     RiskAnalysis.Summary — nullable *string. RiskAnalysis
//     .PromptInjectionDetected — a plain model-reported bool. All structurally
//     free / model-reported / downstream-owned: guarding any would over-reach
//     past the keyparams/partyconsistency-ratified boundary.
//
// prompt_injection_detected is a PURE passthrough here. When the §5 prompt's
// ЗАЩИТА ОТ ИНСТРУКЦИЙ guard (risk_detection.txt:79-82) and the §0.3 general
// guard fire, the LLM ITSELF sets prompt_injection_detected=true AND adds the
// category=PROMPT_INJECTION_ATTEMPT level=medium risk — the acceptance
// criterion's "агент также добавляет риск" is realized via the SYSTEM PROMPT,
// exactly the Agent-4 "per-contract-type catalogue embedded in the §4 system
// prompt, not Go" precedent. Decode does NOT synthesize/enforce that risk
// (code-architect D5): doing so would be a TRANSFORM (the ratified Agent-3/4
// "Decode is a pure typed-unmarshal + drift-guard, never a transform; no
// re-mapping/synthesis" principle), would risk duplicate ids / break the
// monotonic R-NNN counter the LLM owns, and would contradict the
// schema-validated-bytes-in contract (base MF-1 already accepts
// PROMPT_INJECTION_ATTEMPT as a normal category value). Any cross-cutting
// injection-flag handling is downstream: the Result Aggregator (LIC-TASK-035)
// is the SINGLE site that converts prompt_injection_detected=true into the
// DETAILED_REPORT.warnings.PROMPT_INJECTION_DETECTED warning and drops the raw
// flag from risks[] (ai-agents-pipeline.md §5 post-processing).
func (detectorSpec) Decode(content []byte) (port.AgentResult, error) {
	var r model.RiskAnalysis
	if err := json.Unmarshal(content, &r); err != nil {
		return nil, fmt.Errorf("riskdetection: decode RiskAnalysis: %w", err)
	}
	for i, rk := range r.Risks {
		if !rk.Level.IsValid() {
			return nil, fmt.Errorf("riskdetection: risks[%d].level %q not in the high|medium|low whitelist (schema/model drift)", i, rk.Level)
		}
	}
	return &r, nil
}
