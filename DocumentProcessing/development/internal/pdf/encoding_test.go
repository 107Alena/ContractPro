package pdf

import (
	"testing"
)

// --- WinAnsiEncoding tests ---

func TestWinAnsiEncoding_ASCII(t *testing.T) {
	tests := []struct {
		code byte
		want rune
	}{
		{0x41, 'A'},
		{0x5A, 'Z'},
		{0x61, 'a'},
		{0x7A, 'z'},
		{0x30, '0'},
		{0x39, '9'},
		{0x20, ' '},
	}
	for _, tt := range tests {
		got := winAnsiEncoding[tt.code]
		if got != tt.want {
			t.Errorf("winAnsiEncoding[0x%02X] = 0x%04X (%c), want 0x%04X (%c)",
				tt.code, got, got, tt.want, tt.want)
		}
	}
}

func TestWinAnsiEncoding_CP1252Specials(t *testing.T) {
	tests := []struct {
		code byte
		want rune
	}{
		{128, 0x20AC}, // €
		{130, 0x201A}, // ‚
		{132, 0x201E}, // „
		{133, 0x2026}, // …
		{150, 0x2013}, // –
		{151, 0x2014}, // —
		{153, 0x2122}, // ™
	}
	for _, tt := range tests {
		got := winAnsiEncoding[tt.code]
		if got != tt.want {
			t.Errorf("winAnsiEncoding[%d] = 0x%04X, want 0x%04X", tt.code, got, tt.want)
		}
	}
}

func TestWinAnsiEncoding_LatinSupplement(t *testing.T) {
	// 160-255 should match Unicode directly
	tests := []struct {
		code byte
		want rune
	}{
		{0xA0, 0x00A0}, // non-breaking space
		{0xC0, 0x00C0}, // À
		{0xE9, 0x00E9}, // é
		{0xFF, 0x00FF}, // ÿ
	}
	for _, tt := range tests {
		got := winAnsiEncoding[tt.code]
		if got != tt.want {
			t.Errorf("winAnsiEncoding[0x%02X] = 0x%04X, want 0x%04X", tt.code, got, tt.want)
		}
	}
}

// --- MacRomanEncoding tests ---

func TestMacRomanEncoding_ASCII(t *testing.T) {
	// 0-127 should match ASCII
	for i := 0; i < 128; i++ {
		if macRomanEncoding[i] != rune(i) {
			t.Errorf("macRomanEncoding[%d] = 0x%04X, want 0x%04X", i, macRomanEncoding[i], i)
		}
	}
}

func TestMacRomanEncoding_UpperHalf(t *testing.T) {
	tests := []struct {
		code byte
		want rune
	}{
		{128, 0x00C4}, // Ä
		{133, 0x00D6}, // Ö
		{134, 0x00DC}, // Ü
		{142, 0x00E9}, // é
		{167, 0x00DF}, // ß
		{169, 0x00A9}, // ©
	}
	for _, tt := range tests {
		got := macRomanEncoding[tt.code]
		if got != tt.want {
			t.Errorf("macRomanEncoding[%d] = 0x%04X, want 0x%04X", tt.code, got, tt.want)
		}
	}
}

// --- encodingTable tests ---

func TestEncodingTable(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"WinAnsiEncoding", true},
		{"MacRomanEncoding", true},
		{"UnknownEncoding", false},
		{"", false},
	}
	for _, tt := range tests {
		got := encodingTable(tt.name)
		if (got != nil) != tt.want {
			t.Errorf("encodingTable(%q) nil=%v, want nil=%v", tt.name, got == nil, !tt.want)
		}
	}
}

// --- glyphToUnicode tests ---

func TestGlyphToUnicode_CyrillicUppercase(t *testing.T) {
	tests := []struct {
		glyph string
		want  rune
	}{
		{"afii10017", 0x0410}, // А
		{"afii10018", 0x0411}, // Б
		{"afii10019", 0x0412}, // В
		{"afii10020", 0x0413}, // Г
		{"afii10021", 0x0414}, // Д
		{"afii10022", 0x0415}, // Е
		{"afii10023", 0x0416}, // Ж
		{"afii10024", 0x0417}, // З
		{"afii10025", 0x0418}, // И
		{"afii10026", 0x0419}, // Й
		{"afii10027", 0x041A}, // К
		{"afii10028", 0x041B}, // Л
		{"afii10029", 0x041C}, // М
		{"afii10030", 0x041D}, // Н
		{"afii10031", 0x041E}, // О
		{"afii10032", 0x041F}, // П
		{"afii10033", 0x0420}, // Р
		{"afii10034", 0x0421}, // С
		{"afii10035", 0x0422}, // Т
		{"afii10036", 0x0423}, // У
		{"afii10037", 0x0424}, // Ф
		{"afii10038", 0x0425}, // Х
		{"afii10039", 0x0426}, // Ц
		{"afii10040", 0x0427}, // Ч
		{"afii10041", 0x0428}, // Ш
		{"afii10042", 0x0429}, // Щ
		{"afii10043", 0x042A}, // Ъ
		{"afii10044", 0x042B}, // Ы
		{"afii10045", 0x042C}, // Ь
		{"afii10046", 0x042D}, // Э
		{"afii10047", 0x042E}, // Ю
		{"afii10048", 0x042F}, // Я
	}
	for _, tt := range tests {
		got, ok := glyphToUnicode[tt.glyph]
		if !ok {
			t.Errorf("glyphToUnicode[%q] not found", tt.glyph)
			continue
		}
		if got != tt.want {
			t.Errorf("glyphToUnicode[%q] = 0x%04X, want 0x%04X", tt.glyph, got, tt.want)
		}
	}
}

func TestGlyphToUnicode_CyrillicLowercase(t *testing.T) {
	tests := []struct {
		glyph string
		want  rune
	}{
		{"afii10049", 0x0430}, // а
		{"afii10050", 0x0431}, // б
		{"afii10051", 0x0432}, // в
		{"afii10052", 0x0433}, // г
		{"afii10053", 0x0434}, // д
		{"afii10054", 0x0435}, // е
		{"afii10055", 0x0436}, // ж
		{"afii10056", 0x0437}, // з
		{"afii10057", 0x0438}, // и
		{"afii10058", 0x0439}, // й
		{"afii10059", 0x043A}, // к
		{"afii10060", 0x043B}, // л
		{"afii10061", 0x043C}, // м
		{"afii10062", 0x043D}, // н
		{"afii10063", 0x043E}, // о
		{"afii10064", 0x043F}, // п
		{"afii10065", 0x0440}, // р
		{"afii10066", 0x0441}, // с
		{"afii10067", 0x0442}, // т
		{"afii10068", 0x0443}, // у
		{"afii10069", 0x0444}, // ф
		{"afii10070", 0x0445}, // х
		{"afii10071", 0x0446}, // ц
		{"afii10072", 0x0447}, // ч
		{"afii10073", 0x0448}, // ш
		{"afii10074", 0x0449}, // щ
		{"afii10075", 0x044A}, // ъ
		{"afii10076", 0x044B}, // ы
		{"afii10077", 0x044C}, // ь
		{"afii10078", 0x044D}, // э
		{"afii10079", 0x044E}, // ю
		{"afii10080", 0x044F}, // я
	}
	for _, tt := range tests {
		got, ok := glyphToUnicode[tt.glyph]
		if !ok {
			t.Errorf("glyphToUnicode[%q] not found", tt.glyph)
			continue
		}
		if got != tt.want {
			t.Errorf("glyphToUnicode[%q] = 0x%04X, want 0x%04X", tt.glyph, got, tt.want)
		}
	}
}

func TestGlyphToUnicode_YoMappings(t *testing.T) {
	// Ё and ё through alternative names
	tests := []struct {
		glyph string
		want  rune
	}{
		{"Iocyrillic", 0x0401}, // Ё
		{"iocyrillic", 0x0451}, // ё
		{"uni0401", 0x0401},    // Ё via uniXXXX
		{"uni0451", 0x0451},    // ё via uniXXXX
	}
	for _, tt := range tests {
		got, ok := glyphToUnicode[tt.glyph]
		if !ok {
			t.Errorf("glyphToUnicode[%q] not found", tt.glyph)
			continue
		}
		if got != tt.want {
			t.Errorf("glyphToUnicode[%q] = 0x%04X, want 0x%04X", tt.glyph, got, tt.want)
		}
	}
}

func TestGlyphToUnicode_CommonSymbols(t *testing.T) {
	tests := []struct {
		glyph string
		want  rune
	}{
		{"space", 0x0020},
		{"period", 0x002E},
		{"comma", 0x002C},
		{"hyphen", 0x002D},
		{"endash", 0x2013},
		{"emdash", 0x2014},
		{"numero", 0x2116}, // №
		{"guillemotleft", 0x00AB},  // «
		{"guillemotright", 0x00BB}, // »
	}
	for _, tt := range tests {
		got, ok := glyphToUnicode[tt.glyph]
		if !ok {
			t.Errorf("glyphToUnicode[%q] not found", tt.glyph)
			continue
		}
		if got != tt.want {
			t.Errorf("glyphToUnicode[%q] = 0x%04X, want 0x%04X", tt.glyph, got, tt.want)
		}
	}
}

// --- parseDifferencesArray tests ---

func TestParseDifferencesArray_CyrillicBlock(t *testing.T) {
	// Simulates LibreOffice-style Differences for Cyrillic
	arr := []interface{}{
		192, "afii10017", "afii10018", "afii10019", // 192=А, 193=Б, 194=В
	}
	result := parseDifferencesArray(arr)
	if len(result) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(result))
	}
	expected := map[byte]rune{192: 0x0410, 193: 0x0411, 194: 0x0412}
	for code, want := range expected {
		got, ok := result[code]
		if !ok {
			t.Errorf("missing mapping for code %d", code)
			continue
		}
		if got != want {
			t.Errorf("code %d = 0x%04X, want 0x%04X", code, got, want)
		}
	}
}

func TestParseDifferencesArray_MultipleBlocks(t *testing.T) {
	arr := []interface{}{
		65, "A", "B", "C",   // 65=A, 66=B, 67=C
		192, "afii10017",     // 192=А
	}
	result := parseDifferencesArray(arr)
	if len(result) != 4 {
		t.Fatalf("expected 4 entries, got %d", len(result))
	}
	if result[65] != 'A' {
		t.Errorf("code 65 = %c, want A", result[65])
	}
	if result[192] != 0x0410 {
		t.Errorf("code 192 = 0x%04X, want 0x0410", result[192])
	}
}

func TestParseDifferencesArray_WithSlashPrefix(t *testing.T) {
	arr := []interface{}{65, "/A", "/B"}
	result := parseDifferencesArray(arr)
	if result[65] != 'A' {
		t.Errorf("code 65 = %c, want A", result[65])
	}
	if result[66] != 'B' {
		t.Errorf("code 66 = %c, want B", result[66])
	}
}

func TestParseDifferencesArray_UniNames(t *testing.T) {
	arr := []interface{}{192, "uni0410", "uni0411"}
	result := parseDifferencesArray(arr)
	if result[192] != 0x0410 {
		t.Errorf("code 192 = 0x%04X, want 0x0410", result[192])
	}
	if result[193] != 0x0411 {
		t.Errorf("code 193 = 0x%04X, want 0x0411", result[193])
	}
}

func TestParseDifferencesArray_UnknownGlyphs(t *testing.T) {
	arr := []interface{}{65, "unknownglyph", "A"}
	result := parseDifferencesArray(arr)
	// unknownglyph should be skipped, but code increments
	if _, ok := result[65]; ok {
		t.Error("unknown glyph should not produce a mapping")
	}
	if result[66] != 'A' {
		t.Errorf("code 66 = %c, want A", result[66])
	}
}

func TestParseDifferencesArray_EmptyArray(t *testing.T) {
	result := parseDifferencesArray(nil)
	if len(result) != 0 {
		t.Errorf("expected empty result, got %d entries", len(result))
	}
}

func TestParseDifferencesArray_FloatCode(t *testing.T) {
	arr := []interface{}{float64(65), "A"}
	result := parseDifferencesArray(arr)
	if result[65] != 'A' {
		t.Errorf("float64 code: got %c, want A", result[65])
	}
}

// --- parseUniName tests ---

func TestParseUniName(t *testing.T) {
	tests := []struct {
		name string
		want rune
	}{
		{"uni0410", 0x0410},
		{"uni0451", 0x0451},
		{"uni0020", 0x0020},
		{"uniFFFF", 0xFFFF},
		{"uni", 0},           // too short
		{"uni04", 0},         // only 2 hex digits
		{"uni041G", 0},       // invalid hex
		{"notuni", 0},        // doesn't start with "uni"
		{"", 0},
	}
	for _, tt := range tests {
		got := parseUniName(tt.name)
		if got != tt.want {
			t.Errorf("parseUniName(%q) = 0x%04X, want 0x%04X", tt.name, got, tt.want)
		}
	}
}
