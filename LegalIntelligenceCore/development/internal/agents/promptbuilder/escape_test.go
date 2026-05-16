package promptbuilder

import "testing"

func TestEscapeText(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"acceptance step 2: nested closing tag", "</contract_document>", "&lt;/contract_document&gt;"},
		{"nested input tag", "<input>", "&lt;input&gt;"},
		{"all three, order-sensitive", "a & b < c > d", "a &amp; b &lt; c &gt; d"},
		// & MUST be escaped first and the emitted &amp; MUST NOT be re-scanned:
		// literal "&lt;" in the body becomes "&amp;lt;", not "&amp;amp;lt;".
		{"no double-escape of emitted entity", "&lt;", "&amp;lt;"},
		{"ampersand only", "Tom & Jerry", "Tom &amp; Jerry"},
		{"empty", "", ""},
		// Invalid UTF-8 is passed through byte-for-byte; a lone 0x3C is still
		// a single byte and still escaped, so the layer-2 invariant holds
		// even on malformed input (golang-pro nit 1, security regression).
		{"invalid utf8 byte preserved, delimiters still escaped", "\xff<x>", "\xff&lt;x&gt;"},
		{"clean prose untouched (quotes/newlines preserved)", "Договор \"оказания услуг\"\nп.1", "Договор \"оказания услуг\"\nп.1"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := escapeText(c.in); got != c.want {
				t.Fatalf("escapeText(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

func TestEscapeAttr(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"double quote closes attr", `a"b`, "a&quot;b"},
		{"all metacharacters, & first", `&<>"`, "&amp;&lt;&gt;&quot;"},
		{"control chars and newlines stripped", "a\nb\tc\rd\x00e\x7ff", "abcdef"},
		{"C1 control stripped", "xy", "xy"},
		{"clean cyrillic untouched", "ООО Ромашка", "ООО Ромашка"},
		{"empty", "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := escapeAttr(c.in); got != c.want {
				t.Fatalf("escapeAttr(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

// TestEscapers_AreDistinct pins code-architect MF-3: the text-node and
// attribute escapers MUST be different functions. A `"` is left alone by the
// text escaper but becomes &quot; in an attribute; merging them would mangle
// every quote in the contract body and break the acceptance-test exact
// string.
func TestEscapers_AreDistinct(t *testing.T) {
	const in = `he said "hi"`
	tn, at := escapeText(in), escapeAttr(in)
	if tn == at {
		t.Fatalf("escapeText and escapeAttr produced identical output %q — must differ", tn)
	}
	if tn != `he said "hi"` {
		t.Fatalf("escapeText must NOT touch quotes, got %q", tn)
	}
	if at != "he said &quot;hi&quot;" {
		t.Fatalf("escapeAttr must encode quotes, got %q", at)
	}
}
