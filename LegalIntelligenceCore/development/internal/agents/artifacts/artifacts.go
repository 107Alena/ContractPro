// Package artifacts holds the canonical, DP-faithful decoders for the DM
// artifact bundle shared by the LIC per-agent packages
// (LIC-TASK-025..033). It exists because LIC-TASK-026 was made the
// owner-of-record for the shared-decoder decision recorded in
// typeclassifier/CLAUDE.md forward-note #3 (code-reviewer LOW-2): agents
// 1,2,4,5,7 all reconstruct EXTRACTED_TEXT, and re-deriving that DP-faithful
// reconstruction per package risks silent divergence from DP semantics. This
// package is the ONE place that mirror lives, so the "byte-identical to DP"
// claim is asserted by a SINGLE pinning test (TestFullText_DPSemantics)
// instead of by-comment in every per-agent package.
//
// Hermeticity. Like every internal/agents|llm/* sibling, this package imports
// ONLY stdlib (it needs nothing from internal/domain — the artifact shape is
// a local minimal struct). It MUST NOT import the DocumentProcessing Go
// module: the byte-faithful InputArtifactsCompact =
// map[ArtifactType]json.RawMessage defer-decode design and LIC's
// no-cross-module-import rule mandate a local mirror, validated against DP's
// documented contract by test rather than by import. TestHermeticImports
// pins the (stdlib-only) allowlist.
//
// v1 scope is deliberately minimal (YAGNI / the codebase house style): only
// EXTRACTED_TEXT is centralised here, because its reconstruction semantics
// (page join) must stay byte-identical to DP's
// model.ExtractedText.FullText(). DOCUMENT_STRUCTURE (agents 1/3 only) and
// SEMANTIC_TREE (passed through verbatim, never decoded — agent 2 routes the
// raw bytes through the prompt builder) are intentionally NOT modelled here;
// add to this package only when a second consumer actually needs a shared
// decoder. Agent 1 (LIC-TASK-025) keeps its already-committed local copy;
// retiring that now-duplicate `extractedText`/`fullText` is a recorded
// forward cleanup (NOT this task's scope — re-touching a reviewed DONE task
// is unrequested scope creep that risks regressing its pinned tests).
package artifacts

import "strings"

// ExtractedText is the minimal decode of the DM EXTRACTED_TEXT artifact (DP's
// model.ExtractedText: {document_id, pages:[{page_number,text}]}). Only the
// per-page text is consumed; declared locally (not imported from the
// DocumentProcessing module) to honour the byte-faithful
// InputArtifactsCompact = map[ArtifactType]json.RawMessage defer-decode
// design and LIC's no-cross-module-import rule. Unmarshalling is the caller's
// job (a decode error is a LIC build/contract defect the per-agent Spec.Parts
// surfaces as INTERNAL_ERROR — same idiom as Agent 1's committed copy).
type ExtractedText struct {
	Pages []struct {
		Text string `json:"text"`
	} `json:"pages"`
}

// FullText reconstructs the whole-document text, pages joined by a single
// '\n'. It is a BYTE-FAITHFUL mirror of DP's model.ExtractedText.FullText()
// (DocumentProcessing/development/internal/domain/model/document.go:33-53):
// zero pages ⇒ "", a single page ⇒ its text VERBATIM (no trailing newline),
// N pages ⇒ joined by EXACTLY one '\n' and nothing else. The exact-equality
// contract is pinned by TestFullText_DPSemantics — the single assertion of
// the "byte-identical to DP" claim for the whole LIC agents layer (LIC cannot
// import DP to assert it directly: hermeticity).
func (e ExtractedText) FullText() string {
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
