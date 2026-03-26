package pdf

import (
	"testing"
)

// --- parseCMap tests ---

func TestParseCMap_Empty(t *testing.T) {
	cm := parseCMap(nil)
	if cm != nil {
		t.Fatal("parseCMap(nil) should return nil")
	}
	cm = parseCMap([]byte{})
	if cm != nil {
		t.Fatal("parseCMap(empty) should return nil")
	}
}

func TestParseCMap_Garbage(t *testing.T) {
	cm := parseCMap([]byte("this is not a cmap at all"))
	if cm != nil {
		t.Fatal("parseCMap(garbage) should return nil")
	}
}

func TestParseCMap_BFChar_SingleMapping(t *testing.T) {
	cmap := `/CIDInit /ProcSet findresource begin
12 dict begin
begincmap
/CMapType 2 def
1 begincodespacerange
<0000> <FFFF>
endcodespacerange
1 beginbfchar
<0041> <0410>
endbfchar
endcmap`
	cm := parseCMap([]byte(cmap))
	if cm == nil {
		t.Fatal("parseCMap returned nil for valid CMap")
	}
	if len(cm.bfCharMap) != 1 {
		t.Fatalf("expected 1 bfchar entry, got %d", len(cm.bfCharMap))
	}
	runes, ok := cm.bfCharMap[0x0041]
	if !ok {
		t.Fatal("expected mapping for 0x0041")
	}
	if len(runes) != 1 || runes[0] != 0x0410 {
		t.Errorf("dstRunes = %v, want [0x0410]", runes)
	}
}

func TestParseCMap_BFChar_MultipleMappings(t *testing.T) {
	cmap := `1 begincodespacerange
<00> <FF>
endcodespacerange
3 beginbfchar
<C0> <0410>
<C1> <0411>
<C2> <0412>
endbfchar`
	cm := parseCMap([]byte(cmap))
	if cm == nil {
		t.Fatal("parseCMap returned nil")
	}
	if len(cm.bfCharMap) != 3 {
		t.Fatalf("expected 3 bfchar entries, got %d", len(cm.bfCharMap))
	}
	expected := map[uint32]rune{
		0xC0: 0x0410, // А
		0xC1: 0x0411, // Б
		0xC2: 0x0412, // В
	}
	for code, want := range expected {
		runes, ok := cm.bfCharMap[code]
		if !ok {
			t.Errorf("missing mapping for 0x%04X", code)
			continue
		}
		if len(runes) != 1 || runes[0] != want {
			t.Errorf("code 0x%04X: dstRunes = %v, want [0x%04X]", code, runes, want)
		}
	}
}

func TestParseCMap_BFChar_Ligature(t *testing.T) {
	// Multi-rune destination: "ff" ligature
	cmap := `1 begincodespacerange
<00> <FF>
endcodespacerange
1 beginbfchar
<FB> <00660066>
endbfchar`
	cm := parseCMap([]byte(cmap))
	if cm == nil {
		t.Fatal("parseCMap returned nil")
	}
	if len(cm.bfCharMap) != 1 {
		t.Fatalf("expected 1 bfchar, got %d", len(cm.bfCharMap))
	}
	runes := cm.bfCharMap[0xFB]
	if len(runes) != 2 || runes[0] != 'f' || runes[1] != 'f' {
		t.Errorf("expected ['f', 'f'], got %v", runes)
	}
}

func TestParseCMap_BFRange_SimpleRange(t *testing.T) {
	cmap := `1 begincodespacerange
<0000> <FFFF>
endcodespacerange
1 beginbfrange
<0041> <0043> <0410>
endbfrange`
	cm := parseCMap([]byte(cmap))
	if cm == nil {
		t.Fatal("parseCMap returned nil")
	}
	if len(cm.bfRanges) != 1 {
		t.Fatalf("expected 1 bfrange, got %d", len(cm.bfRanges))
	}
	r := cm.bfRanges[0]
	if r.srcLo != 0x0041 || r.srcHi != 0x0043 {
		t.Errorf("range = [0x%04X, 0x%04X], want [0x0041, 0x0043]", r.srcLo, r.srcHi)
	}
	if len(r.dstBase) != 1 || r.dstBase[0] != 0x0410 {
		t.Errorf("dstBase = %v, want [0x0410]", r.dstBase)
	}
}

func TestParseCMap_BFRange_ArrayForm(t *testing.T) {
	cmap := `1 begincodespacerange
<00> <FF>
endcodespacerange
1 beginbfrange
<01> <03> [<0410> <0411> <0412>]
endbfrange`
	cm := parseCMap([]byte(cmap))
	if cm == nil {
		t.Fatal("parseCMap returned nil")
	}
	if len(cm.bfRanges) != 1 {
		t.Fatalf("expected 1 bfrange, got %d", len(cm.bfRanges))
	}
	r := cm.bfRanges[0]
	if r.dstArray == nil {
		t.Fatal("dstArray is nil for array-form bfrange")
	}
	if len(r.dstArray) != 3 {
		t.Fatalf("expected 3 array entries, got %d", len(r.dstArray))
	}
	expected := []rune{0x0410, 0x0411, 0x0412}
	for i, exp := range expected {
		if len(r.dstArray[i]) != 1 || r.dstArray[i][0] != exp {
			t.Errorf("dstArray[%d] = %v, want [0x%04X]", i, r.dstArray[i], exp)
		}
	}
}

func TestParseCMap_CodeSpaceRange(t *testing.T) {
	cmap := `2 begincodespacerange
<00> <FF>
<0000> <FFFF>
endcodespacerange
1 beginbfchar
<41> <0041>
endbfchar`
	cm := parseCMap([]byte(cmap))
	if cm == nil {
		t.Fatal("parseCMap returned nil")
	}
	if len(cm.codeSpaceRanges) != 2 {
		t.Fatalf("expected 2 codespace ranges, got %d", len(cm.codeSpaceRanges))
	}
	// Check 1-byte range
	found1byte := false
	found2byte := false
	for _, csr := range cm.codeSpaceRanges {
		if csr.numBytes == 1 && csr.lo == 0x00 && csr.hi == 0xFF {
			found1byte = true
		}
		if csr.numBytes == 2 && csr.lo == 0x0000 && csr.hi == 0xFFFF {
			found2byte = true
		}
	}
	if !found1byte {
		t.Error("missing 1-byte codespace range")
	}
	if !found2byte {
		t.Error("missing 2-byte codespace range")
	}
}

func TestParseCMap_MultipleSections(t *testing.T) {
	cmap := `1 begincodespacerange
<0000> <FFFF>
endcodespacerange
2 beginbfchar
<0041> <0410>
<0042> <0411>
endbfchar
1 beginbfrange
<0043> <0045> <0412>
endbfrange
1 beginbfchar
<0046> <0424>
endbfchar`
	cm := parseCMap([]byte(cmap))
	if cm == nil {
		t.Fatal("parseCMap returned nil")
	}
	if len(cm.bfCharMap) != 3 {
		t.Errorf("expected 3 bfchars, got %d", len(cm.bfCharMap))
	}
	if len(cm.bfRanges) != 1 {
		t.Errorf("expected 1 bfrange, got %d", len(cm.bfRanges))
	}
}

// --- Decode tests ---

func TestCMapDecode_NilCMap(t *testing.T) {
	var cm *cmapTable
	result := cm.Decode([]byte{0x00, 0x41})
	if result != "" {
		t.Errorf("nil CMap Decode should return empty, got %q", result)
	}
}

func TestCMapDecode_EmptyInput(t *testing.T) {
	cm := &cmapTable{}
	result := cm.Decode(nil)
	if result != "" {
		t.Errorf("Decode(nil) should return empty, got %q", result)
	}
	result = cm.Decode([]byte{})
	if result != "" {
		t.Errorf("Decode(empty) should return empty, got %q", result)
	}
}

func TestCMapDecode_BFChar(t *testing.T) {
	cm := &cmapTable{
		codeSpaceRanges: []codeSpaceRange{{lo: 0x0000, hi: 0xFFFF, numBytes: 2}},
		bfCharMap: map[uint32][]rune{
			0x0041: {0x0410},
			0x0042: {0x0411},
		},
	}
	result := cm.Decode([]byte{0x00, 0x41, 0x00, 0x42})
	if result != "АБ" {
		t.Errorf("Decode = %q, want %q", result, "АБ")
	}
}

func TestCMapDecode_BFRange(t *testing.T) {
	cm := &cmapTable{
		codeSpaceRanges: []codeSpaceRange{{lo: 0x0000, hi: 0xFFFF, numBytes: 2}},
		bfRanges: []bfRangeEntry{
			{srcLo: 0x0041, srcHi: 0x005A, dstBase: []rune{0x0410}}, // A-Z → А-Я
		},
	}
	// 0x0041 → А (offset 0), 0x0042 → Б (offset 1), 0x0043 → В (offset 2)
	result := cm.Decode([]byte{0x00, 0x41, 0x00, 0x42, 0x00, 0x43})
	if result != "АБВ" {
		t.Errorf("Decode = %q, want %q", result, "АБВ")
	}
}

func TestCMapDecode_BFRangeArray(t *testing.T) {
	cm := &cmapTable{
		codeSpaceRanges: []codeSpaceRange{{lo: 0x00, hi: 0xFF, numBytes: 1}},
		bfRanges: []bfRangeEntry{
			{srcLo: 0x01, srcHi: 0x03, dstArray: [][]rune{{0x0410}, {0x0411}, {0x0412}}},
		},
	}
	result := cm.Decode([]byte{0x01, 0x02, 0x03})
	if result != "АБВ" {
		t.Errorf("Decode = %q, want %q", result, "АБВ")
	}
}

func TestCMapDecode_UnmappedCode(t *testing.T) {
	cm := &cmapTable{
		codeSpaceRanges: []codeSpaceRange{{lo: 0x0000, hi: 0xFFFF, numBytes: 2}},
		bfCharMap: map[uint32][]rune{
			0x0041: {0x0410},
		},
	}
	result := cm.Decode([]byte{0x00, 0xFF})
	if result != "\uFFFD" {
		t.Errorf("unmapped code should produce U+FFFD, got %q", result)
	}
}

func TestCMapDecode_DefaultTwoByte(t *testing.T) {
	// No codeSpaceRanges → default to 2-byte
	cm := &cmapTable{
		bfCharMap: map[uint32][]rune{
			0x043F: {'п'},
		},
	}
	result := cm.Decode([]byte{0x04, 0x3F})
	if result != "п" {
		t.Errorf("Decode = %q, want %q", result, "п")
	}
}

func TestCMapDecode_CyrillicFullWord(t *testing.T) {
	// Simulate "Привет" in a CID font with ToUnicode CMap
	cmap := `1 begincodespacerange
<0000> <FFFF>
endcodespacerange
6 beginbfchar
<0001> <041F>
<0002> <0440>
<0003> <0438>
<0004> <0432>
<0005> <0435>
<0006> <0442>
endbfchar`
	cm := parseCMap([]byte(cmap))
	if cm == nil {
		t.Fatal("parseCMap returned nil")
	}
	raw := []byte{0x00, 0x01, 0x00, 0x02, 0x00, 0x03, 0x00, 0x04, 0x00, 0x05, 0x00, 0x06}
	result := cm.Decode(raw)
	if result != "Привет" {
		t.Errorf("Decode = %q, want %q", result, "Привет")
	}
}

// --- Helper tests ---

func TestParseHexToken(t *testing.T) {
	tests := []struct {
		input    string
		wantVal  uint32
		wantSize int
	}{
		{"0041", 0x0041, 2},
		{"FFFF", 0xFFFF, 2},
		{"FF", 0xFF, 1},
		{"0410", 0x0410, 2},
		{"", 0, 0},
		{"  0041  ", 0x0041, 2},
	}
	for _, tt := range tests {
		val, size := parseHexToken(tt.input)
		if val != tt.wantVal || size != tt.wantSize {
			t.Errorf("parseHexToken(%q) = (%d, %d), want (%d, %d)", tt.input, val, size, tt.wantVal, tt.wantSize)
		}
	}
}

func TestHexToRunes(t *testing.T) {
	tests := []struct {
		input string
		want  []rune
	}{
		{"0410", []rune{0x0410}},           // А
		{"00660066", []rune{'f', 'f'}},      // ff ligature
		{"041F04400438", []rune{0x041F, 0x0440, 0x0438}}, // При
		{"", nil},
	}
	for _, tt := range tests {
		got := hexToRunes(tt.input)
		if len(got) != len(tt.want) {
			t.Errorf("hexToRunes(%q) = %v, want %v", tt.input, got, tt.want)
			continue
		}
		for i := range got {
			if got[i] != tt.want[i] {
				t.Errorf("hexToRunes(%q)[%d] = 0x%04X, want 0x%04X", tt.input, i, got[i], tt.want[i])
			}
		}
	}
}

func TestExtractHexTokens(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{"<0041> <0410>", 2},
		{"<0041>", 1},
		{"no hex here", 0},
		{"<0041> text <0042>", 2},
	}
	for _, tt := range tests {
		got := extractHexTokens(tt.input)
		if len(got) != tt.want {
			t.Errorf("extractHexTokens(%q) returned %d tokens, want %d", tt.input, len(got), tt.want)
		}
	}
}

func TestReadBigEndian(t *testing.T) {
	tests := []struct {
		input []byte
		want  uint32
	}{
		{[]byte{0x00, 0x41}, 0x0041},
		{[]byte{0x04, 0x10}, 0x0410},
		{[]byte{0xFF}, 0xFF},
		{[]byte{0x00, 0x00, 0x04, 0x10}, 0x0410},
	}
	for _, tt := range tests {
		got := readBigEndian(tt.input)
		if got != tt.want {
			t.Errorf("readBigEndian(%v) = 0x%X, want 0x%X", tt.input, got, tt.want)
		}
	}
}

func TestCMapDecode_BFCharPriorityOverRange(t *testing.T) {
	// bfchar should take priority over bfrange for the same code
	cm := &cmapTable{
		codeSpaceRanges: []codeSpaceRange{{lo: 0x00, hi: 0xFF, numBytes: 1}},
		bfCharMap:       map[uint32][]rune{0x41: {'X'}},
		bfRanges:        []bfRangeEntry{{srcLo: 0x40, srcHi: 0x50, dstBase: []rune{'Y'}}},
	}
	result := cm.Decode([]byte{0x41})
	if result != "X" {
		t.Errorf("bfchar should have priority, got %q", result)
	}
}
