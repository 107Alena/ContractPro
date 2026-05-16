package promptbuilder

import "strings"

// escape.go holds the two — deliberately distinct — XML escapers that
// implement prompt-injection defence layer 2 (high-architecture.md §6.7.1,
// ai-agents-pipeline.md §0.3).
//
// They MUST NOT be merged into one function: a text-node value and an
// attribute value need different encodings, and folding `"`→&quot; into the
// text escaper would mangle ordinary contract prose (every double-quote in
// the document body) and break the exact-string acceptance test
// (`</contract_document>` → `&lt;/contract_document&gt;`).

// textEscaper encodes user-controlled CONTENT placed inside an XML text node
// of the prompt envelope (EXTRACTED_TEXT, SEMANTIC_TREE-as-JSON,
// KEY_PARAMETERS, PROCESSING_WARNINGS, any upstream findings/parameters).
//
// `&` MUST be replaced first, then `<`, then `>`. strings.Replacer does a
// single non-overlapping left-to-right pass and never re-scans replacement
// output, so the `&` it just emitted as part of `&lt;` is not re-escaped —
// using one Replacer (not three sequential .Replace calls) makes the
// "& first / no double-escape" invariant structural. Encoding `>` (not
// strictly required by XML) is mandatory here: an LLM does not run an XML
// parser, so a half-escaped `&lt;/contract_document>` could still read as a
// block delimiter; the acceptance test pins `&gt;`.
var textEscaper = strings.NewReplacer(
	"&", "&amp;",
	"<", "&lt;",
	">", "&gt;",
)

// escapeText escapes a user-controlled content block. It is the ONLY way
// untrusted bytes reach the envelope; Build applies it to every non-minted
// Part.
func escapeText(s string) string { return textEscaper.Replace(s) }

// attrEscaper encodes a user-controlled value placed inside an XML ATTRIBUTE
// (the validation_facts name/inn/ogrn — all originate from the document /
// agent-2 output). It additionally escapes `"`→&quot; so an injected quote
// cannot close the attribute; `>` is escaped for the same non-XML-parsing-LLM
// reason as textEscaper. `&` first, same single-pass guarantee.
var attrEscaper = strings.NewReplacer(
	"&", "&amp;",
	"<", "&lt;",
	">", "&gt;",
	`"`, "&quot;",
)

// escapeAttr strips control characters and newlines FIRST (so a value can
// never carry a structural line break into an attribute even with quotes
// escaped), then applies attribute encoding. It is intentionally a different
// function from escapeText (TestEscapers_AreDistinct pins this).
func escapeAttr(s string) string {
	return attrEscaper.Replace(stripControl(s))
}

// stripControl removes C0 controls (incl. TAB/LF/CR), DEL and C1 controls.
// Returning -1 from strings.Map drops the rune.
func stripControl(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) {
			return -1
		}
		return r
	}, s)
}
