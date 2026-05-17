package artifacts

import (
	"encoding/json"
	"testing"
)

// TestFullText_DPSemantics is the SINGLE assertion of the "byte-identical to
// DP" claim for the whole LIC agents layer. It pins ExtractedText.FullText()
// against DP's documented model.ExtractedText.FullText() contract
// (DocumentProcessing/development/internal/domain/model/document.go:33-53),
// which LIC cannot import (hermeticity): zero pages ⇒ "", one page ⇒ its text
// VERBATIM with NO trailing newline, N pages ⇒ joined by EXACTLY one '\n',
// and nothing else (no trimming, no per-page newline, empty interior pages
// keep their join).
func TestFullText_DPSemantics(t *testing.T) {
	mk := func(pages ...string) ExtractedText {
		var et ExtractedText
		for _, p := range pages {
			et.Pages = append(et.Pages, struct {
				Text string `json:"text"`
			}{Text: p})
		}
		return et
	}
	cases := []struct {
		name string
		et   ExtractedText
		want string
	}{
		{"zero pages ⇒ empty", ExtractedText{}, ""},
		{"single page ⇒ verbatim, no trailing newline", mk("строка договора"), "строка договора"},
		{"single page with interior newlines is verbatim", mk("стр1\nстр2"), "стр1\nстр2"},
		{"multi page joined by exactly one \\n", mk("п1", "п2", "п3"), "п1\nп2\nп3"},
		{"empty interior page keeps the join", mk("a", "", "c"), "a\n\nc"},
		{"no whitespace trimming", mk("  лидирующий ", " хвостовой  "), "  лидирующий \n хвостовой  "},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.et.FullText(); got != tc.want {
				t.Fatalf("FullText() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestExtractedText_DecodesDPWireShape pins that the local minimal struct
// decodes the DP EXTRACTED_TEXT wire JSON ({document_id, pages:[{page_number,
// text}]}) — extra DP fields ignored, the `text` field consumed, pages:null
// is the zero-page case, and a non-string page text is a decode error the
// caller surfaces.
func TestExtractedText_DecodesDPWireShape(t *testing.T) {
	raw := json.RawMessage(`{"document_id":"d1","pages":[{"page_number":1,"text":"первая"},{"page_number":2,"text":"вторая"}]}`)
	var et ExtractedText
	if err := json.Unmarshal(raw, &et); err != nil {
		t.Fatalf("Unmarshal DP wire shape: %v", err)
	}
	if got, want := et.FullText(), "первая\nвторая"; got != want {
		t.Fatalf("FullText() = %q, want %q", got, want)
	}

	var z ExtractedText
	if err := json.Unmarshal([]byte(`{"document_id":"d","pages":null}`), &z); err != nil {
		t.Fatalf("Unmarshal pages:null: %v", err)
	}
	if z.FullText() != "" {
		t.Fatalf("pages:null FullText() = %q, want empty", z.FullText())
	}

	if err := json.Unmarshal([]byte(`{"pages":[{"text":123}]}`), new(ExtractedText)); err == nil {
		t.Fatal("want decode error for non-string page text, got nil")
	}
}
