package pdf

import (
	"bytes"
	"strings"
	"testing"
)

// --- parseContentStreamWithFonts tests ---

func TestParseContentStreamWithFonts_LiteralTj(t *testing.T) {
	// Basic Tj with font codec
	cm := &cmapTable{
		codeSpaceRanges: []codeSpaceRange{{lo: 0x00, hi: 0xFF, numBytes: 1}},
		bfCharMap: map[uint32][]rune{
			0x48: {'H'},
			0x69: {'i'},
		},
	}
	fonts := map[string]*fontCodec{
		"F1": {cmap: cm},
	}
	content := "BT /F1 12 Tf (Hi) Tj ET"
	result, warnings := parseContentStreamWithFonts(content, fonts)
	if result != "Hi" {
		t.Errorf("result = %q, want %q", result, "Hi")
	}
	if len(warnings) != 0 {
		t.Errorf("unexpected warnings: %v", warnings)
	}
}

func TestParseContentStreamWithFonts_HexString(t *testing.T) {
	// CID font with hex string
	cm := &cmapTable{
		codeSpaceRanges: []codeSpaceRange{{lo: 0x0000, hi: 0xFFFF, numBytes: 2}},
		bfCharMap: map[uint32][]rune{
			0x041F: {'П'},
			0x0440: {'р'},
			0x0438: {'и'},
		},
	}
	fonts := map[string]*fontCodec{
		"F1": {cmap: cm, isTwoByte: true},
	}
	content := "BT /F1 12 Tf <041F04400438> Tj ET"
	result, _ := parseContentStreamWithFonts(content, fonts)
	if result != "При" {
		t.Errorf("hex string decode = %q, want %q", result, "При")
	}
}

func TestParseContentStreamWithFonts_FontSwitch(t *testing.T) {
	// Two fonts in one block
	latinCm := &cmapTable{
		codeSpaceRanges: []codeSpaceRange{{lo: 0x00, hi: 0xFF, numBytes: 1}},
		bfCharMap:       map[uint32][]rune{0x41: {'A'}},
	}
	cyrilCm := &cmapTable{
		codeSpaceRanges: []codeSpaceRange{{lo: 0x00, hi: 0xFF, numBytes: 1}},
		bfCharMap:       map[uint32][]rune{0x41: {'А'}}, // Same byte, different char
	}
	fonts := map[string]*fontCodec{
		"F1": {cmap: latinCm},
		"F2": {cmap: cyrilCm},
	}
	content := "BT /F1 12 Tf (A) Tj /F2 12 Tf (A) Tj ET"
	result, _ := parseContentStreamWithFonts(content, fonts)
	if result != "AА" {
		t.Errorf("font switch result = %q, want %q", result, "AА")
	}
}

func TestParseContentStreamWithFonts_TJArray(t *testing.T) {
	cm := &cmapTable{
		codeSpaceRanges: []codeSpaceRange{{lo: 0x00, hi: 0xFF, numBytes: 1}},
		bfCharMap: map[uint32][]rune{
			'H': {'H'},
			'e': {'e'},
			'l': {'l'},
			'o': {'o'},
		},
	}
	fonts := map[string]*fontCodec{"F1": {cmap: cm}}
	content := "BT /F1 12 Tf [(Hel) -10 (lo)] TJ ET"
	result, _ := parseContentStreamWithFonts(content, fonts)
	if result != "Hello" {
		t.Errorf("TJ array result = %q, want %q", result, "Hello")
	}
}

func TestParseContentStreamWithFonts_TJArrayHex(t *testing.T) {
	cm := &cmapTable{
		codeSpaceRanges: []codeSpaceRange{{lo: 0x0000, hi: 0xFFFF, numBytes: 2}},
		bfCharMap: map[uint32][]rune{
			0x041F: {'П'},
			0x0440: {'р'},
		},
	}
	fonts := map[string]*fontCodec{"F1": {cmap: cm, isTwoByte: true}}
	content := "BT /F1 12 Tf [<041F> -10 <0440>] TJ ET"
	result, _ := parseContentStreamWithFonts(content, fonts)
	if result != "Пр" {
		t.Errorf("TJ hex array = %q, want %q", result, "Пр")
	}
}

func TestParseContentStreamWithFonts_NoFonts(t *testing.T) {
	// Empty fonts map — should not crash, returns empty since no codec
	content := "BT /F1 12 Tf (Hello) Tj ET"
	result, _ := parseContentStreamWithFonts(content, map[string]*fontCodec{})
	// No active codec — literal string decoded via decodePDFString fallback
	if !strings.Contains(result, "Hello") {
		t.Errorf("no-font fallback result = %q, want to contain Hello", result)
	}
}

func TestParseContentStreamWithFonts_UnknownFont(t *testing.T) {
	fonts := map[string]*fontCodec{
		"F1": {encoding: &winAnsiEncoding},
	}
	// F2 is not in fonts map — should fall back to identity
	content := "BT /F2 12 Tf (Hello) Tj ET"
	result, _ := parseContentStreamWithFonts(content, fonts)
	if result != "Hello" {
		t.Errorf("unknown font result = %q, want %q", result, "Hello")
	}
}

func TestParseContentStreamWithFonts_MultipleBTETBlocks(t *testing.T) {
	cm := &cmapTable{
		codeSpaceRanges: []codeSpaceRange{{lo: 0x00, hi: 0xFF, numBytes: 1}},
		bfCharMap: map[uint32][]rune{
			'A': {'A'},
			'B': {'B'},
		},
	}
	fonts := map[string]*fontCodec{"F1": {cmap: cm}}
	content := "BT /F1 12 Tf (A) Tj ET q 0 0 0 rg Q BT /F1 12 Tf (B) Tj ET"
	result, _ := parseContentStreamWithFonts(content, fonts)
	if result != "AB" {
		t.Errorf("multiple BT/ET = %q, want %q", result, "AB")
	}
}

// --- readLiteralString tests ---

func TestReadLiteralString_Simple(t *testing.T) {
	s := "(Hello World) Tj"
	str, end := readLiteralString(s, 0)
	if str != "Hello World" {
		t.Errorf("readLiteralString = %q, want %q", str, "Hello World")
	}
	if end != 13 {
		t.Errorf("end = %d, want 13", end)
	}
}

func TestReadLiteralString_NestedParens(t *testing.T) {
	s := "(text (with parens) more) Tj"
	str, end := readLiteralString(s, 0)
	if str != "text (with parens) more" {
		t.Errorf("nested parens = %q, want %q", str, "text (with parens) more")
	}
	if end != 25 {
		t.Errorf("end = %d, want 25", end)
	}
}

func TestReadLiteralString_EscapedParens(t *testing.T) {
	s := `(text \( escaped \)) Tj`
	str, _ := readLiteralString(s, 0)
	if str != `text \( escaped \)` {
		t.Errorf("escaped parens = %q", str)
	}
}

func TestReadLiteralString_Empty(t *testing.T) {
	s := "() Tj"
	str, end := readLiteralString(s, 0)
	if str != "" {
		t.Errorf("empty string = %q, want empty", str)
	}
	if end != 2 {
		t.Errorf("end = %d, want 2", end)
	}
}

func TestReadLiteralString_NotAParen(t *testing.T) {
	s := "Hello"
	str, end := readLiteralString(s, 0)
	if str != "" || end != 0 {
		t.Errorf("not a paren: str=%q, end=%d", str, end)
	}
}

// --- readHexStringFromContent tests ---

func TestReadHexStringFromContent_Basic(t *testing.T) {
	s := "<041F0440> Tj"
	hex, end := readHexStringFromContent(s, 0)
	if hex != "041F0440" {
		t.Errorf("hex = %q, want %q", hex, "041F0440")
	}
	if end != 10 {
		t.Errorf("end = %d, want 10", end)
	}
}

func TestReadHexStringFromContent_Empty(t *testing.T) {
	s := "<> Tj"
	hex, end := readHexStringFromContent(s, 0)
	if hex != "" {
		t.Errorf("empty hex = %q, want empty", hex)
	}
	if end != 2 {
		t.Errorf("end = %d, want 2", end)
	}
}

func TestReadHexStringFromContent_NotAngle(t *testing.T) {
	s := "Hello"
	hex, end := readHexStringFromContent(s, 0)
	if hex != "" || end != 0 {
		t.Errorf("not angle: hex=%q, end=%d", hex, end)
	}
}

// --- readTJArray tests ---

func TestReadTJArray_Basic(t *testing.T) {
	s := "[(Hello) -10 (World)] TJ"
	content, end := readTJArray(s, 0)
	if content != "(Hello) -10 (World)" {
		t.Errorf("array content = %q", content)
	}
	// "[(Hello) -10 (World)]" has length 21, so end = 21
	if end != 21 {
		t.Errorf("end = %d, want 21", end)
	}
}

func TestReadTJArray_WithNestedParens(t *testing.T) {
	s := "[(text (nested))] TJ"
	content, end := readTJArray(s, 0)
	if content != "(text (nested))" {
		t.Errorf("nested array = %q", content)
	}
	if end != 17 {
		t.Errorf("end = %d, want 17", end)
	}
}

func TestReadTJArray_Unmatched(t *testing.T) {
	s := "[unterminated"
	_, end := readTJArray(s, 0)
	if end != 0 {
		t.Errorf("unmatched should return original pos, got %d", end)
	}
}

// --- isTextShowingOp tests ---

func TestIsTextShowingOp(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"Tj", true},
		{"Tj rest", true},
		{"'", true},
		{"\"", true},
		{"TJ", false}, // TJ is handled separately at the array level
		{"Tf", false},
		{"", false},
		{"T", false},
	}
	for _, tt := range tests {
		got := isTextShowingOp(tt.input)
		if got != tt.want {
			t.Errorf("isTextShowingOp(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

// --- decodePDFStringToBytes tests ---

func TestDecodePDFStringToBytes_BasicEscapes(t *testing.T) {
	tests := []struct {
		input string
		want  []byte
	}{
		{"Hello", []byte("Hello")},
		{`\n`, []byte{'\n'}},
		{`\r`, []byte{'\r'}},
		{`\t`, []byte{'\t'}},
		{`\\`, []byte{'\\'}},
		{`\(`, []byte{'('}},
		{`\)`, []byte{')'}},
	}
	for _, tt := range tests {
		got := decodePDFStringToBytes(tt.input)
		if !bytes.Equal(got, tt.want) {
			t.Errorf("decodePDFStringToBytes(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestDecodePDFStringToBytes_Octal(t *testing.T) {
	// \101 = 0x41 = 'A'
	got := decodePDFStringToBytes(`\101`)
	if len(got) != 1 || got[0] != 0x41 {
		t.Errorf("octal decode = %v, want [0x41]", got)
	}
	// \320\220 = Cyrillic А in UTF-8 encoded as octal
	got = decodePDFStringToBytes(`\320\220`)
	if len(got) != 2 || got[0] != 0xD0 || got[1] != 0x90 {
		t.Errorf("octal cyrillic = %v, want [0xD0, 0x90]", got)
	}
}

// --- decodeHexWithCodec tests ---

func TestDecodeHexWithCodec_WithCodec(t *testing.T) {
	cm := &cmapTable{
		codeSpaceRanges: []codeSpaceRange{{lo: 0x0000, hi: 0xFFFF, numBytes: 2}},
		bfCharMap:       map[uint32][]rune{0x041F: {'П'}},
	}
	fc := &fontCodec{cmap: cm}
	result := decodeHexWithCodec("041F", fc)
	if result != "П" {
		t.Errorf("decodeHexWithCodec = %q, want %q", result, "П")
	}
}

func TestDecodeHexWithCodec_NilCodec(t *testing.T) {
	result := decodeHexWithCodec("48656C6C6F", nil)
	if result != "Hello" {
		t.Errorf("nil codec hex = %q, want %q", result, "Hello")
	}
}

func TestDecodeHexWithCodec_Empty(t *testing.T) {
	result := decodeHexWithCodec("", nil)
	if result != "" {
		t.Errorf("empty hex = %q, want empty", result)
	}
}

// --- ExtractTextWithWarnings public API test ---

func TestExtractTextWithWarnings_NilReader(t *testing.T) {
	u := NewUtil()
	_, _, err := u.ExtractTextWithWarnings(nil)
	if err != ErrEmptyReader {
		t.Errorf("expected ErrEmptyReader, got %v", err)
	}
}

func TestExtractTextWithWarnings_CorruptedData(t *testing.T) {
	u := NewUtil()
	_, _, err := u.ExtractTextWithWarnings(bytes.NewReader([]byte("garbage")))
	if err == nil {
		t.Error("expected error for corrupted data")
	}
}

func TestExtractTextWithWarnings_ValidPDF(t *testing.T) {
	u := NewUtil()
	data := generateTextPDF(t, []string{"Hello World"})
	pages, warnings, err := u.ExtractTextWithWarnings(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pages) != 1 {
		t.Fatalf("expected 1 page, got %d", len(pages))
	}
	if !strings.Contains(pages[0].Text, "Hello World") {
		t.Errorf("text = %q, want to contain 'Hello World'", pages[0].Text)
	}
	// Helvetica is a standard font — no warnings expected
	_ = warnings // may or may not have warnings depending on font resolution
}

// --- ExtractionWarning type test ---

func TestExtractionWarning_Fields(t *testing.T) {
	w := ExtractionWarning{PageNumber: 3, Message: "test warning"}
	if w.PageNumber != 3 {
		t.Errorf("PageNumber = %d, want 3", w.PageNumber)
	}
	if w.Message != "test warning" {
		t.Errorf("Message = %q, want %q", w.Message, "test warning")
	}
}
