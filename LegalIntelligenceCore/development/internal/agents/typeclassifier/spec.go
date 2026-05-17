package typeclassifier

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"contractpro/legal-intelligence-core/internal/agents/base"
	"contractpro/legal-intelligence-core/internal/agents/promptbuilder"
	"contractpro/legal-intelligence-core/internal/domain/model"
	"contractpro/legal-intelligence-core/internal/domain/port"
)

// Per-agent input compaction for Agent 1 (ai-agents-pipeline.md §1
// "Зависимости": EXTRACTED_TEXT head 4 000 + tail 1 000 символов). This is
// Agent 1's OWN fixed-size character compaction — categorically distinct from,
// and composable with, LIC-TASK-021's general per-model token head-60/tail-40
// truncation, which operates upstream of Spec.Parts (base/seams.go MF-3).
// "символов" = characters: sliced by RUNE, never bytes, so a multi-byte
// Cyrillic codepoint is never split (same rune-safe discipline as
// promptbuilder.capRunes / passthroughEstimator).
const (
	headRunes = 4000
	tailRunes = 1000
	// elision marks the gap between the head and tail fragments. The §1
	// system prompt explicitly describes the block as "фрагменты текста
	// договора (начало + конец)", so the two fragments MUST be visibly
	// separated — fusing them would misrepresent the contract's structure to
	// the model. It is fixed, non-injectable content (no &<> ⇒ unchanged by
	// promptbuilder's escapeText) and is the one deliberate place LIC-authored
	// bytes enter the escaped user block, justified because the prompt itself
	// announces the head+tail shaping (contrast capRunes, whose truncation is
	// silent and therefore marker-free).
	elision = "\n[…]\n"
)

// extractedText is the minimal decode of the DM EXTRACTED_TEXT artifact (DP's
// model.ExtractedText: {document_id, pages:[{page_number,text}]}). Only the
// per-page text is consumed; declared locally (not imported from the
// DocumentProcessing module) to honour the byte-faithful
// InputArtifactsCompact=map[ArtifactType]json.RawMessage defer-decode design
// and LIC's no-cross-module-import rule.
type extractedText struct {
	Pages []struct {
		Text string `json:"text"`
	} `json:"pages"`
}

// fullText reconstructs the whole-document text, pages joined by '\n' —
// byte-identical to DP's model.ExtractedText.FullText() semantics.
func (e extractedText) fullText() string {
	switch len(e.Pages) {
	case 0:
		return ""
	case 1:
		return e.Pages[0].Text
	}
	var sb strings.Builder
	for i, p := range e.Pages {
		if i > 0 {
			sb.WriteByte('\n')
		}
		sb.WriteString(p.Text)
	}
	return sb.String()
}

// documentStructure is the minimal decode of the DM DOCUMENT_STRUCTURE
// artifact (DP's model.DocumentStructure). Per §1 Agent 1 consumes ONLY
// sections[].title (token-budget discipline, §0.6); number/content/clauses are
// deliberately ignored.
type documentStructure struct {
	Sections []struct {
		Title string `json:"title"`
	} `json:"sections"`
}

// titles is the newline-joined, trimmed, non-empty section titles. Zero
// sections / all-blank titles legitimately yield "" — a structurally flat but
// valid contract, NOT an error (the classification leans on the contract text;
// titles are a weak supplementary signal).
func (d documentStructure) titles() string {
	var sb strings.Builder
	first := true
	for _, s := range d.Sections {
		t := strings.TrimSpace(s.Title)
		if t == "" {
			continue
		}
		if !first {
			sb.WriteByte('\n')
		}
		sb.WriteString(t)
		first = false
	}
	return sb.String()
}

// compact applies the §1 head/tail rule. When the text is short enough that
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

// classifierSpec is the Agent-1 strategy. It is an empty struct with
// value-receiver methods — STATELESS and safe for concurrent use, as base.Spec
// mandates (one *BaseAgent, hence one Spec, is shared by the parallel errgroup
// pipeline; pinned by TestRun_ConcurrentRaceClean, -race).
type classifierSpec struct{}

var _ base.Spec = classifierSpec{}

// Parts builds the §1 envelope, in the system-prompt's exact block order:
//
//	<input>
//	  <document_structure>…section titles…</document_structure>
//	  <contract_document>…head+[…]+tail…</contract_document>
//	</input>
//
// promptbuilder emits only one-level <tag>escaped</tag> blocks; the §1
// prompt's inner <sections> wrapper is illustrative, not binding (the ratified
// promptbuilder "the prompt is SSOT, the illustration form is not" precedent),
// so two Content parts are the faithful, builder-expressible mapping. Both
// parts are user-controlled bytes routed through promptbuilder.Content
// (escaped — prompt-injection defence layer 2).
//
// The shared Builder b is unused: only Agent 3 mints a structural block
// (b.ValidationFacts). A returned error is a LIC programming/contract defect
// surfaced by base as INTERNAL_ERROR (never sent to the LLM). EXTRACTED_TEXT
// is mandatory (absent / malformed / empty ⇒ error); DOCUMENT_STRUCTURE is
// mandatory in shape (absent / malformed ⇒ error) but may be structurally
// empty (zero usable titles ⇒ an empty, valid <document_structure> block).
//
// FORWARD NOTE (owner: LIC-TASK-034 Stage-Executor input prep / the artifact
// bundle gate). A genuinely missing/empty mandatory DM artifact is
// semantically DM_ARTIFACTS_MISSING (retryable, "данные документа не найдены"),
// NOT INTERNAL_ERROR. By contract that bundle gate MUST reject it BEFORE Agent
// 1 runs; reaching this Parts check then means a true LIC invariant breach, for
// which base's Parts-error→INTERNAL_ERROR projection is the CORRECT code. base
// is not modified by this task.
func (classifierSpec) Parts(_ *promptbuilder.Builder, in model.AgentInput) ([]promptbuilder.Part, error) {
	rawText, ok := in.Artifacts[model.ArtifactExtractedText]
	if !ok || len(rawText) == 0 {
		return nil, errors.New("typeclassifier: mandatory EXTRACTED_TEXT artifact is absent")
	}
	var et extractedText
	if err := json.Unmarshal(rawText, &et); err != nil {
		return nil, fmt.Errorf("typeclassifier: decode EXTRACTED_TEXT artifact: %w", err)
	}
	full := et.fullText()
	if strings.TrimSpace(full) == "" {
		return nil, errors.New("typeclassifier: EXTRACTED_TEXT artifact decoded to empty text")
	}

	rawStruct, ok := in.Artifacts[model.ArtifactDocumentStructure]
	if !ok || len(rawStruct) == 0 {
		return nil, errors.New("typeclassifier: mandatory DOCUMENT_STRUCTURE artifact is absent")
	}
	var ds documentStructure
	if err := json.Unmarshal(rawStruct, &ds); err != nil {
		return nil, fmt.Errorf("typeclassifier: decode DOCUMENT_STRUCTURE artifact: %w", err)
	}

	return []promptbuilder.Part{
		promptbuilder.Content("document_structure", ds.titles()),
		promptbuilder.Content("contract_document", compact(full)),
	}, nil
}

// Decode unmarshals the schema-validated response into *model.ClassificationResult
// (the concrete port.AgentResult the Stage Executor narrows by AgentID).
//
// base schema-validates the bytes against the embedded type_classifier.json
// (whose enum already constrains contract_type to the 12 values) BEFORE
// calling Decode (MF-1). The explicit IsValid() re-check is the codebase's
// enumerated cross-check house style (cf. base.canonicalStage,
// prompts.basenames, model.IsValidContractType's documented purpose): it pins
// the schema-enum ↔ model.ContractType-whitelist seam, turning a silent
// schema/Go drift into a loud INTERNAL_ERROR build-defect signal — precisely
// Decode's contract ("schema and Go struct disagree ⇒ LIC build defect").
func (classifierSpec) Decode(content []byte) (port.AgentResult, error) {
	var r model.ClassificationResult
	if err := json.Unmarshal(content, &r); err != nil {
		return nil, fmt.Errorf("typeclassifier: decode ClassificationResult: %w", err)
	}
	if !r.ContractType.IsValid() {
		return nil, fmt.Errorf("typeclassifier: contract_type %q not in the 12-value whitelist (schema/model drift)", r.ContractType)
	}
	for i, alt := range r.Alternatives {
		if !alt.ContractType.IsValid() {
			return nil, fmt.Errorf("typeclassifier: alternatives[%d].contract_type %q not in the 12-value whitelist (schema/model drift)", i, alt.ContractType)
		}
	}
	return &r, nil
}
