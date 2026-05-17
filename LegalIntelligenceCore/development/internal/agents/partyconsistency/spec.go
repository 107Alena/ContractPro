package partyconsistency

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

// checkerSpec is the Agent-3 strategy. It is an empty struct with
// value-receiver methods — STATELESS and safe for concurrent use, as base.Spec
// mandates (one *BaseAgent, hence one Spec, is shared by the parallel errgroup
// pipeline; pinned by TestRun_ConcurrentRaceClean, -race).
type checkerSpec struct{}

var _ base.Spec = checkerSpec{}

// partyDetails is the minimal decode of one DOCUMENT_STRUCTURE.party_details
// entry (DP's model.PartyDetails —
// DocumentProcessing/development/internal/domain/model/structure.go:31-38).
// The JSON tags MUST match the DP wire SSOT byte-for-byte: the signatory field
// is `representative` here — NOT `signatory`, which is the DISTINCT
// model.PartyRole field name (key_parameters.go) that would silently decode to
// "" with no error (a build-defect-class trap; code-architect CC-3, pinned by
// TestSpec_Parts_PartyDetailsRepresentativeTag).
//
// Declared LOCALLY (not in the shared internal/agents/artifacts package): that
// package deliberately centralises ONLY EXTRACTED_TEXT, because its page-join
// must stay byte-identical to DP and is asserted once by
// TestFullText_DPSemantics. DOCUMENT_STRUCTURE has no such reconstruction
// invariant — it is a flat passthrough — and Agent 1 already reads a DISJOINT
// projection (sections[].title, typeclassifier/spec.go documentStructure). A
// per-agent local minimal decode is therefore the ratified v1 pattern
// (artifacts/CLAUDE.md scope boundary; code-architect Q2). The `omitempty`
// tags mirror DP's struct so a future re-marshal stays wire-faithful.
type partyDetails struct {
	Name           string `json:"name"`
	INN            string `json:"inn,omitempty"`
	OGRN           string `json:"ogrn,omitempty"`
	Address        string `json:"address,omitempty"`
	Representative string `json:"representative,omitempty"`
}

// documentStructure is the minimal decode of the DM DOCUMENT_STRUCTURE
// artifact. §3 Agent 3 consumes ONLY party_details; sections/appendices are
// deliberately ignored (token-budget discipline, the Agent-1 local-projection
// precedent). party_details is decoded WITHOUT omitempty so the field is
// always addressable; an absent/empty list is a legitimate, TOLERATED shape
// (the prompt says "DOCUMENT_STRUCTURE.party_details (если есть)").
type documentStructure struct {
	PartyDetails []partyDetails `json:"party_details"`
}

// derefString maps a wire-nullable *string to its value; nil ⇒ "". A nil
// INN/OGRN is the LEGAL, common "not present" case (model.PartyRole.INN/OGRN
// are *string, serialised as null when unset — never omitted, per
// ai-agents-pipeline.md §2 schema), and "" is exactly promptbuilder.Party's
// "empty ⇒ not present (not invalid)" contract, so an absent identifier is
// neither rendered into <validation_facts> nor counted by
// lic_party_validation_total. A blind `*p` would panic on that path — this
// nil-safe helper is code-architect CHANGE-1 (pinned by the nil-INN/OGRN
// case of TestSpec_Parts_ValidationFacts).
func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// Parts builds the §3 envelope in the system-prompt's EXACT block order
// (prompts/party_consistency.txt:595-604):
//
//	<input>
//	  <party_roles>… JSON KeyParameters.internal_extras.party_roles …</party_roles>
//	  <validation_facts>… Builder-minted <inn_check>/<ogrn_check> ground truth …</validation_facts>
//	  <party_details_block>… JSON DOCUMENT_STRUCTURE.party_details …</party_details_block>
//	  <contract_document>… full EXTRACTED_TEXT …</contract_document>
//	</input>
//
// The shared Builder b is USED here — Agent 3 is the SOLE minter of a
// structural block (b.ValidationFacts), exactly as base.go's Spec godoc and
// promptbuilder/CLAUDE.md anticipate. b.ValidationFacts is called ONLY on the
// happy path, AFTER every input-strictness check has passed, so the
// nil-Builder error-case test pattern (checkerSpec{}.Parts(nil, badInput))
// still surfaces the strictness error without dereferencing b (code-architect
// Q4). The returned []PartyValidation is DISCARDED in v1: the model consumes
// the XML block; the structured results exist only for a FUTURE cross-agent
// verification (YAGNI — promptbuilder.PartyValidation godoc; forward note in
// CLAUDE.md). A returned error is a LIC programming/contract defect surfaced
// by base as INTERNAL_ERROR (never sent to the LLM).
//
// Input sourcing & strictness (code-architect Q1/Q2/Q3):
//
//   - party_roles ← in.KeyParameters.internal_extras.party_roles. A nil
//     in.KeyParameters is a PIPELINE-ORDERING invariant breach (Agent 2 runs
//     Stage 1, Agent 3 Stage 2; the Stage Executor MUST populate it) ⇒ error
//     ⇒ base INTERNAL_ERROR (FORWARD NOTE for LIC-TASK-034, distinct from the
//     DM-artifact gate note below). A non-nil KeyParameters with nil
//     InternalExtras OR empty PartyRoles is TOLERATED — the empty list flows
//     through ValidationFacts([]) which mints exactly
//     <validation_facts></validation_facts>, and <party_roles> carries `[]`;
//     party_roles is a supplementary structured signal, the model still reads
//     party_details_block + contract_document (the typeclassifier
//     empty-DOCUMENT_STRUCTURE / keyparams nil-internal_extras tolerance
//     analogue).
//   - party_details ← DOCUMENT_STRUCTURE. The artifact is MANDATORY IN SHAPE:
//     absent / empty bytes / malformed JSON ⇒ error. A structurally-empty or
//     absent party_details list is TOLERATED — emitted as `[]` (the prompt's
//     "(если есть)"). Mirrors Agent 1's mandatory-in-shape DOCUMENT_STRUCTURE
//     with a tolerated-empty projection.
//   - contract_document ← EXTRACTED_TEXT via the shared artifacts.ExtractedText
//     decoder, emitted in FULL with NO Agent-3-side compaction. §3 specifies
//     NO fixed-size or token-bounded head/tail rule (unlike §1's Agent-1
//     "head 4000 + tail 1000"); "фрагменты" is descriptive prose, not a
//     deterministic slicing instruction. The only truncation is
//     LIC-TASK-021's GENERIC per-artifact rule UPSTREAM of Spec.Parts on
//     model.AgentInput (base/seams.go MF-3 / keyparams precedent). Carrying an
//     interim truncation here would be a second divergent truncation site 021
//     must later find and remove. EXTRACTED_TEXT is MANDATORY: absent /
//     malformed JSON / decodes-to-empty-or-whitespace ⇒ error. FORWARD NOTE
//     for LIC-TASK-021: Agent 3 deliberately emits the full EXTRACTED_TEXT.
//
// FORWARD NOTE (owner: LIC-TASK-034 — the artifact-bundle gate, distinct from
// the pipeline-ordering note above and identical in spirit to typeclassifier
// forward-note #2 / keyparams). A genuinely missing/empty mandatory DM
// artifact (DOCUMENT_STRUCTURE / EXTRACTED_TEXT) is semantically
// DM_ARTIFACTS_MISSING (retryable, "данные документа не найдены"), NOT
// INTERNAL_ERROR. That bundle gate MUST reject it BEFORE Agent 3 runs;
// reaching these Parts checks then means a true LIC invariant breach, for
// which base's Parts-error→INTERNAL_ERROR projection is the CORRECT code.
// base is not modified by this task.
func (checkerSpec) Parts(b *promptbuilder.Builder, in model.AgentInput) ([]promptbuilder.Part, error) {
	// --- strictness phase: all checks BEFORE the first b.* dereference ---

	if in.KeyParameters == nil {
		return nil, errors.New("partyconsistency: mandatory upstream KeyParameters (Agent 2) result is absent")
	}

	rawStruct, ok := in.Artifacts[model.ArtifactDocumentStructure]
	if !ok || len(rawStruct) == 0 {
		return nil, errors.New("partyconsistency: mandatory DOCUMENT_STRUCTURE artifact is absent")
	}
	var ds documentStructure
	if err := json.Unmarshal(rawStruct, &ds); err != nil {
		return nil, fmt.Errorf("partyconsistency: decode DOCUMENT_STRUCTURE artifact: %w", err)
	}

	rawText, ok := in.Artifacts[model.ArtifactExtractedText]
	if !ok || len(rawText) == 0 {
		return nil, errors.New("partyconsistency: mandatory EXTRACTED_TEXT artifact is absent")
	}
	var et artifacts.ExtractedText
	if err := json.Unmarshal(rawText, &et); err != nil {
		return nil, fmt.Errorf("partyconsistency: decode EXTRACTED_TEXT artifact: %w", err)
	}
	full := et.FullText()
	if strings.TrimSpace(full) == "" {
		return nil, errors.New("partyconsistency: EXTRACTED_TEXT artifact decoded to empty text")
	}

	// --- assembly phase ---

	// party_roles: a non-nil InternalExtras may still carry a nil PartyRoles
	// slice; normalise both nils to a non-nil empty slice so json.Marshal
	// emits the literal `[]` (a well-formed empty JSON array the model can
	// read), never `null` (code-architect Q2).
	roles := []model.PartyRole{}
	if in.KeyParameters.InternalExtras != nil && in.KeyParameters.InternalExtras.PartyRoles != nil {
		roles = in.KeyParameters.InternalExtras.PartyRoles
	}
	rolesJSON, err := json.Marshal(roles)
	if err != nil {
		// []model.PartyRole is string/*string only ⇒ Marshal cannot fail in
		// practice; a failure is a LIC build defect (base → INTERNAL_ERROR).
		return nil, fmt.Errorf("partyconsistency: marshal party_roles: %w", err)
	}

	// party_details: normalise nil → empty slice so an absent/empty list is
	// emitted as `[]` (the prompt's "(если есть)"), never `null`.
	pd := ds.PartyDetails
	if pd == nil {
		pd = []partyDetails{}
	}
	pdJSON, err := json.Marshal(pd)
	if err != nil {
		return nil, fmt.Errorf("partyconsistency: marshal party_details: %w", err)
	}

	// validation_facts: pre-LLM, LLM-free ИНН/ОГРН checksum ground truth.
	// derefString is nil-safe (CHANGE-1): a null *string INN/OGRN maps to ""
	// = promptbuilder.Party's "not present" (neither rendered nor counted).
	// The empty-roles path is NOT special-cased: ValidationFacts([]) naturally
	// mints exactly <validation_facts></validation_facts>, keeping the
	// envelope's fixed 4-block shape invariant regardless of party count
	// (code-architect CHANGE-2). The []PartyValidation result is discarded
	// (v1 YAGNI — no in-process consumer).
	parties := make([]promptbuilder.Party, 0, len(roles))
	for _, r := range roles {
		parties = append(parties, promptbuilder.Party{
			Name: r.Name,
			INN:  derefString(r.INN),
			OGRN: derefString(r.OGRN),
		})
	}
	factsPart, _ := b.ValidationFacts(parties)

	return []promptbuilder.Part{
		promptbuilder.Content("party_roles", string(rolesJSON)),
		factsPart,
		promptbuilder.Content("party_details_block", string(pdJSON)),
		promptbuilder.Content("contract_document", full),
	}, nil
}

// Decode unmarshals the schema-validated response into
// *model.PartyConsistencyFindings (the concrete port.AgentResult the Stage
// Executor narrows by AgentID).
//
// base schema-validates the bytes against the embedded party_consistency.json
// BEFORE calling Decode (MF-1); that schema already enum-constrains
// findings[].type (7-value) and findings[].severity (high|medium|low). The
// explicit per-finding IsValid() re-checks are the codebase's enumerated
// cross-check house style (cf. base.canonicalStage, classifier's
// ContractType.IsValid, keyparams' PartyRoleType.IsValid): they pin the
// schema-enum ↔ Go-whitelist seam, turning a silent party_consistency.json ↔
// model drift into a loud INTERNAL_ERROR build-defect signal — precisely
// Decode's contract ("schema and Go struct disagree ⇒ LIC build defect").
//
// A PartyFinding has TWO closed-enum surfaces, so BOTH are guarded per finding
// (model.PartyFindingType — 7 values; model.RiskLevel — high|medium|low). Every
// other field is structurally free: Description/ClauseRef (free strings, schema
// maxLength only), PartyName/LegalBasis (*string nullable/optional), Summary
// (*string), PromptInjectionDetected (a plain model-reported bool) — guarding a
// free or model-reported field would over-reach past the keyparams-ratified
// house style. No severity RE-MAPPING is done here: model/party_consistency.go
// documents that the agent sets severity per the prompt's
// PARTY_AUTHORITY_MISSING→high / rest→medium table and "Result Aggregator does
// not re-map"; Decode is a pure typed-unmarshal + drift-guard, never a
// transform. A schema-valid `low` severity (the enum admits it even though the
// prompt table never asks for it) decodes WITHOUT error — enum-membership is
// Decode's concern, prompt-policy is not.
func (checkerSpec) Decode(content []byte) (port.AgentResult, error) {
	var r model.PartyConsistencyFindings
	if err := json.Unmarshal(content, &r); err != nil {
		return nil, fmt.Errorf("partyconsistency: decode PartyConsistencyFindings: %w", err)
	}
	for i, f := range r.Findings {
		if !f.Type.IsValid() {
			return nil, fmt.Errorf("partyconsistency: findings[%d].type %q not in the 7-value whitelist (schema/model drift)", i, f.Type)
		}
		if !f.Severity.IsValid() {
			return nil, fmt.Errorf("partyconsistency: findings[%d].severity %q not in the high|medium|low whitelist (schema/model drift)", i, f.Severity)
		}
	}
	return &r, nil
}
