package pdf

import (
	"strings"
	"testing"
)

// --- fontCodec.decode tests ---

func TestFontCodecDecode_NilCodec(t *testing.T) {
	var fc *fontCodec
	result := fc.decode([]byte("Hello"))
	if result != "Hello" {
		t.Errorf("nil codec decode = %q, want %q", result, "Hello")
	}
}

func TestFontCodecDecode_WithCMap(t *testing.T) {
	cm := &cmapTable{
		codeSpaceRanges: []codeSpaceRange{{lo: 0x0000, hi: 0xFFFF, numBytes: 2}},
		bfCharMap: map[uint32][]rune{
			0x0001: {0x041F}, // П
			0x0002: {0x0440}, // р
		},
	}
	fc := &fontCodec{cmap: cm}
	result := fc.decode([]byte{0x00, 0x01, 0x00, 0x02})
	if result != "Пр" {
		t.Errorf("CMap decode = %q, want %q", result, "Пр")
	}
}

func TestFontCodecDecode_WithDifferences(t *testing.T) {
	diffMap := map[byte]rune{
		0xC0: 0x0410, // А
		0xC1: 0x0411, // Б
		0xC2: 0x0412, // В
	}
	fc := &fontCodec{diffMap: diffMap}
	result := fc.decode([]byte{0xC0, 0xC1, 0xC2})
	if result != "АБВ" {
		t.Errorf("Differences decode = %q, want %q", result, "АБВ")
	}
}

func TestFontCodecDecode_WithEncoding(t *testing.T) {
	fc := &fontCodec{encoding: &winAnsiEncoding}
	result := fc.decode([]byte{0x41, 0x42, 0x43})
	if result != "ABC" {
		t.Errorf("encoding decode = %q, want %q", result, "ABC")
	}
}

func TestFontCodecDecode_DifferencesOverrideEncoding(t *testing.T) {
	// Differences should take priority over base encoding
	diffMap := map[byte]rune{0x41: 0x0410} // Override A → А
	fc := &fontCodec{
		encoding: &winAnsiEncoding,
		diffMap:  diffMap,
	}
	result := fc.decode([]byte{0x41, 0x42})
	if result != "АB" {
		t.Errorf("diff+encoding decode = %q, want %q", result, "АB")
	}
}

func TestFontCodecDecode_CMapPriorityOverAll(t *testing.T) {
	cm := &cmapTable{
		codeSpaceRanges: []codeSpaceRange{{lo: 0x00, hi: 0xFF, numBytes: 1}},
		bfCharMap:       map[uint32][]rune{0x41: {'X'}},
	}
	fc := &fontCodec{
		cmap:     cm,
		encoding: &winAnsiEncoding,
		diffMap:  map[byte]rune{0x41: 0x0410},
	}
	result := fc.decode([]byte{0x41})
	if result != "X" {
		t.Errorf("CMap should have priority, got %q", result)
	}
}

func TestFontCodecDecode_IdentityFallback(t *testing.T) {
	fc := &fontCodec{} // no cmap, no encoding, no diffMap
	result := fc.decode([]byte{0x48, 0x65, 0x6C, 0x6C, 0x6F})
	if result != "Hello" {
		t.Errorf("identity fallback = %q, want %q", result, "Hello")
	}
}

func TestFontCodecDecode_TwoByte_NoCMap(t *testing.T) {
	// CID font without CMap — should try identity two-byte
	fc := &fontCodec{isTwoByte: true}
	result := fc.decode([]byte{0x04, 0x10, 0x04, 0x11})
	if result != "АБ" {
		t.Errorf("twoByte identity decode = %q, want %q", result, "АБ")
	}
}

// --- fontCodec.decodeHexString tests ---

func TestFontCodecDecodeHexString_Basic(t *testing.T) {
	cm := &cmapTable{
		codeSpaceRanges: []codeSpaceRange{{lo: 0x0000, hi: 0xFFFF, numBytes: 2}},
		bfCharMap: map[uint32][]rune{
			0x041F: {'П'},
		},
	}
	fc := &fontCodec{cmap: cm}
	result := fc.decodeHexString("041F")
	if result != "П" {
		t.Errorf("decodeHexString = %q, want %q", result, "П")
	}
}

func TestFontCodecDecodeHexString_Empty(t *testing.T) {
	fc := &fontCodec{}
	result := fc.decodeHexString("")
	if result != "" {
		t.Errorf("decodeHexString empty = %q, want empty", result)
	}
}

func TestFontCodecDecodeHexString_OddLength(t *testing.T) {
	fc := &fontCodec{encoding: &winAnsiEncoding}
	// Odd hex: "4" → padded to "40" = byte 0x40 = '@'
	result := fc.decodeHexString("4")
	if result != "@" {
		t.Errorf("decodeHexString odd = %q, want %q", result, "@")
	}
}

func TestFontCodecDecodeHexString_InvalidHex(t *testing.T) {
	fc := &fontCodec{}
	result := fc.decodeHexString("ZZZZ")
	// Invalid hex should return empty
	if result != "" {
		t.Errorf("decodeHexString invalid = %q, want empty", result)
	}
}

// --- allReplacementChars tests ---

func TestAllReplacementChars(t *testing.T) {
	if !allReplacementChars("\uFFFD\uFFFD\uFFFD") {
		t.Error("expected true for all FFFD")
	}
	if allReplacementChars("A\uFFFD") {
		t.Error("expected false for mixed content")
	}
	if allReplacementChars("Hello") {
		t.Error("expected false for normal text")
	}
	if !allReplacementChars("") {
		t.Error("expected true for empty string")
	}
}

// --- identityDecode tests ---

func TestIdentityDecode(t *testing.T) {
	result := identityDecode([]byte{0x48, 0x65, 0x6C, 0x6C, 0x6F})
	if result != "Hello" {
		t.Errorf("identityDecode = %q, want Hello", result)
	}
}

// --- identityTwoByteDecode tests ---

func TestIdentityTwoByteDecode(t *testing.T) {
	// 0x0410 = А, 0x0411 = Б
	result := identityTwoByteDecode([]byte{0x04, 0x10, 0x04, 0x11})
	if result != "АБ" {
		t.Errorf("identityTwoByteDecode = %q, want %q", result, "АБ")
	}
}

func TestIdentityTwoByteDecode_OddByte(t *testing.T) {
	result := identityTwoByteDecode([]byte{0x04, 0x10, 0x41})
	if !strings.HasPrefix(result, "А") {
		t.Errorf("should start with А, got %q", result)
	}
}

// --- isStandard14Font tests ---

func TestIsStandard14Font(t *testing.T) {
	standard := []string{
		"Helvetica", "Helvetica-Bold", "Helvetica-Oblique", "Helvetica-BoldOblique",
		"Courier", "Courier-Bold", "Courier-Oblique", "Courier-BoldOblique",
		"Times-Roman", "Times-Bold", "Times-Italic", "Times-BoldItalic",
		"Symbol", "ZapfDingbats",
	}
	for _, name := range standard {
		if !isStandard14Font(name) {
			t.Errorf("isStandard14Font(%q) = false, want true", name)
		}
	}

	nonStandard := []string{
		"ArialMT", "TimesNewRomanPSMT", "ABCDEF+TimesNewRomanPSMT",
		"", "CustomFont",
	}
	for _, name := range nonStandard {
		if isStandard14Font(name) {
			t.Errorf("isStandard14Font(%q) = true, want false", name)
		}
	}
}
