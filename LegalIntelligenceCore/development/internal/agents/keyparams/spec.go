package keyparams

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

// extractorSpec is the Agent-2 strategy. It is an empty struct with
// value-receiver methods — STATELESS and safe for concurrent use, as base.Spec
// mandates (one *BaseAgent, hence one Spec, is shared by the parallel errgroup
// pipeline; pinned by TestRun_ConcurrentRaceClean, -race).
type extractorSpec struct{}

var _ base.Spec = extractorSpec{}

// Parts builds the §2 envelope, in the system-prompt's EXACT block order
// (prompts/key_params.txt:35-40):
//
//	<input>
//	  <semantic_tree>… raw SEMANTIC_TREE JSON, escaped …</semantic_tree>
//	  <contract_document>… full EXTRACTED_TEXT, escaped …</contract_document>
//	</input>
//
// The order is the INVERSE of Agent 1's (semantic_tree first, not
// document_structure) — get it wrong and the model reads the tree as the
// contract body. The §2 prompt's inner <input> wrapper is added by
// Builder.Build (NOT emitted here) — the ratified promptbuilder "the prompt
// is SSOT, the illustration form is not binding" precedent Agent 1 applied to
// its <sections>. Both parts are user-controlled bytes routed through
// promptbuilder.Content (escaped — prompt-injection defence layer 2): a
// literal </semantic_tree> or </contract_document> planted in the tree's node
// content or the contract body can never read as a block delimiter.
//
// SEMANTIC_TREE is passed THROUGH VERBATIM (string(rawTree)) — NOT decoded
// and re-encoded. §2 requires the tree "полностью"; the agent is instructed
// to cite semantic_tree node "id" values as clause_ref, so pruning or
// re-keying the tree would strip the very ids it must reference. This is
// exactly what the byte-faithful InputArtifactsCompact =
// map[ArtifactType]json.RawMessage defer-decode design exists for; the
// strictness gate is well-formedness (json.Valid), not a structural decode.
//
// EXTRACTED_TEXT is emitted in FULL (artifacts.ExtractedText.FullText(), the
// shared DP-faithful page join) with NO Agent-2-side char/token compaction.
// §2's ">80K tokens ⇒ head/tail truncation" is the GENERIC per-artifact
// truncation that is explicitly LIC-TASK-021's job UPSTREAM of Spec.Parts on
// model.AgentInput (base/seams.go MF-3 / base/CLAUDE.md) — categorically
// different from Agent 1's fixed §1 "head 4000 + tail 1000" which was an
// Agent-1-specific prompt requirement. Carrying an interim truncation here
// would be a second, divergent truncation site 021 must later find and
// remove; until 021 lands the v1 passthrough estimator delegates the
// over-budget verdict to the provider's CONTEXT_TOO_LONG → AGENT_INPUT_TOO_LARGE
// path (base classifyCompleteError). FORWARD NOTE for LIC-TASK-021: Agent 2
// deliberately emits the full EXTRACTED_TEXT; the head/tail rule belongs in
// the upstream artifact-bundle preparation, not this Spec.
//
// The shared Builder b is unused: only Agent 3 mints a structural block
// (b.ValidationFacts). A returned error is a LIC programming/contract defect
// surfaced by base as INTERNAL_ERROR (never sent to the LLM).
//
// Artifact strictness (mirrors typeclassifier Q4). SEMANTIC_TREE is mandatory:
// absent / empty bytes / not-well-formed JSON ⇒ error. An empty-but-valid
// tree ({"document_id":"d","root":null} or {}) is TOLERATED — emitted as-is
// (the gate is well-formedness, not semantic richness; a structurally flat
// tree is a legitimate contract shape, and the model can still extract every
// parameter from <contract_document> — the tree only supplies clause_ref ids,
// a supplementary signal). EXTRACTED_TEXT is mandatory: absent / malformed
// JSON / decodes to empty-or-whitespace text ⇒ error.
//
// FORWARD NOTE (owner: LIC-TASK-034 Stage-Executor input prep / the artifact
// bundle gate — identical to typeclassifier forward-note #2). A genuinely
// missing/empty mandatory DM artifact is semantically DM_ARTIFACTS_MISSING
// (retryable, "данные документа не найдены"), NOT INTERNAL_ERROR. That bundle
// gate MUST reject it BEFORE Agent 2 runs; reaching these Parts checks then
// means a true LIC invariant breach, for which base's
// Parts-error→INTERNAL_ERROR projection is the CORRECT code. base is not
// modified by this task.
func (extractorSpec) Parts(_ *promptbuilder.Builder, in model.AgentInput) ([]promptbuilder.Part, error) {
	rawTree, ok := in.Artifacts[model.ArtifactSemanticTree]
	if !ok || len(rawTree) == 0 {
		return nil, errors.New("keyparams: mandatory SEMANTIC_TREE artifact is absent")
	}
	if !json.Valid(rawTree) {
		return nil, errors.New("keyparams: SEMANTIC_TREE artifact is not well-formed JSON")
	}

	rawText, ok := in.Artifacts[model.ArtifactExtractedText]
	if !ok || len(rawText) == 0 {
		return nil, errors.New("keyparams: mandatory EXTRACTED_TEXT artifact is absent")
	}
	var et artifacts.ExtractedText
	if err := json.Unmarshal(rawText, &et); err != nil {
		return nil, fmt.Errorf("keyparams: decode EXTRACTED_TEXT artifact: %w", err)
	}
	full := et.FullText()
	if strings.TrimSpace(full) == "" {
		return nil, errors.New("keyparams: EXTRACTED_TEXT artifact decoded to empty text")
	}

	return []promptbuilder.Part{
		promptbuilder.Content("semantic_tree", string(rawTree)),
		promptbuilder.Content("contract_document", full),
	}, nil
}

// Decode unmarshals the schema-validated response into *model.KeyParameters
// (the concrete port.AgentResult the Stage Executor narrows by AgentID).
//
// base schema-validates the bytes against the embedded key_params.json BEFORE
// calling Decode (MF-1); that schema already enum-constrains
// internal_extras.party_roles[].role to the 9-value set. The explicit
// PartyRoleType.IsValid() re-check is the codebase's enumerated cross-check
// house style (cf. base.canonicalStage, prompts.basenames, classifier's
// ContractType.IsValid re-check): it pins the schema-enum ↔
// model.PartyRoleType-whitelist seam, turning a silent key_params.json ↔ Go
// drift into a loud INTERNAL_ERROR build-defect signal — precisely Decode's
// contract ("schema and Go struct disagree ⇒ LIC build defect").
//
// `role` is the SOLE drift surface for KeyParameters: every other field is
// structurally free (parties []string, subject/price/duration/penalties/
// jurisdiction strings-or-null, KeyDate.{Label,Value,ClauseRef} free strings,
// prompt_injection_detected a plain model-reported bool) — guarding a free
// string would over-reach past the house style. internal_extras itself is
// schema-optional and decodes to a nil *KeyParametersInternalExtras when the
// LLM omits it: a nil-guard (not an error) before ranging PartyRoles keeps a
// perfectly valid minimal response valid.
//
// INN/OGRN in party_roles are decoded AS-IS (raw *string): Agent 2 must NOT
// validate checksums — that is Agent 3's pre-LLM job (ai-agents-pipeline.md §2
// correctness criterion 6 / acceptance Шаг 2). No guard is added for them by
// design.
func (extractorSpec) Decode(content []byte) (port.AgentResult, error) {
	var r model.KeyParameters
	if err := json.Unmarshal(content, &r); err != nil {
		return nil, fmt.Errorf("keyparams: decode KeyParameters: %w", err)
	}
	if r.InternalExtras != nil {
		for i, pr := range r.InternalExtras.PartyRoles {
			if !pr.Role.IsValid() {
				return nil, fmt.Errorf("keyparams: internal_extras.party_roles[%d].role %q not in the 9-value whitelist (schema/model drift)", i, pr.Role)
			}
		}
	}
	return &r, nil
}
