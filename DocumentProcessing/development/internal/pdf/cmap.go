// Package pdf — cmap.go provides a ToUnicode CMap parser for decoding
// character codes from PDF content streams into Unicode text.
package pdf

import (
	"bytes"
	"strings"
)

// codeSpaceRange defines a range of valid input codes and their byte width.
type codeSpaceRange struct {
	lo, hi   uint32
	numBytes int
}

// bfCharEntry maps a single source code to a sequence of Unicode runes.
type bfCharEntry struct {
	srcCode  uint32
	dstRunes []rune
}

// bfRangeEntry maps a contiguous range of source codes to Unicode.
// Either dstBase or dstArray is used, never both.
type bfRangeEntry struct {
	srcLo, srcHi uint32
	dstBase      []rune   // simple range: dstBase[0] + (code - srcLo)
	dstArray     [][]rune // array mapping: one entry per code in [srcLo..srcHi]
}

// cmapTable holds the parsed ToUnicode CMap data.
// Safe for concurrent use after construction (read-only).
type cmapTable struct {
	codeSpaceRanges []codeSpaceRange
	bfCharMap       map[uint32][]rune // O(1) lookup by source code
	bfRanges        []bfRangeEntry
}

// parseCMap parses a raw CMap byte stream and returns a cmapTable.
// Returns nil if data is empty or unparseable.
// Gracefully handles malformed data — never panics.
func parseCMap(data []byte) *cmapTable {
	if len(data) == 0 {
		return nil
	}

	cm := &cmapTable{}
	s := string(data)

	// Parse codespacerange sections
	cm.codeSpaceRanges = parseCodeSpaceRanges(s)

	// Parse bfchar sections into O(1) map
	bfChars := parseBFChars(s)
	cm.bfCharMap = make(map[uint32][]rune, len(bfChars))
	for _, entry := range bfChars {
		cm.bfCharMap[entry.srcCode] = entry.dstRunes
	}

	// Parse bfrange sections
	cm.bfRanges = parseBFRanges(s)

	// If nothing was parsed, return nil
	if len(cm.codeSpaceRanges) == 0 && len(cm.bfCharMap) == 0 && len(cm.bfRanges) == 0 {
		return nil
	}

	return cm
}

// parseCodeSpaceRanges extracts all begincodespacerange..endcodespacerange sections.
func parseCodeSpaceRanges(s string) []codeSpaceRange {
	var ranges []codeSpaceRange

	for {
		startIdx := strings.Index(s, "begincodespacerange")
		if startIdx < 0 {
			break
		}
		s = s[startIdx+len("begincodespacerange"):]

		endIdx := strings.Index(s, "endcodespacerange")
		if endIdx < 0 {
			break
		}
		block := s[:endIdx]
		s = s[endIdx+len("endcodespacerange"):]

		tokens := extractHexTokens(block)
		// Tokens come in pairs: <lo> <hi>
		for i := 0; i+1 < len(tokens); i += 2 {
			lo, loBytes := parseHexToken(tokens[i])
			hi, hiBytes := parseHexToken(tokens[i+1])
			numBytes := loBytes
			if hiBytes > numBytes {
				numBytes = hiBytes
			}
			ranges = append(ranges, codeSpaceRange{lo: lo, hi: hi, numBytes: numBytes})
		}
	}

	return ranges
}

// parseBFChars extracts all beginbfchar..endbfchar sections.
func parseBFChars(s string) []bfCharEntry {
	var entries []bfCharEntry

	for {
		startIdx := strings.Index(s, "beginbfchar")
		if startIdx < 0 {
			break
		}
		s = s[startIdx+len("beginbfchar"):]

		endIdx := strings.Index(s, "endbfchar")
		if endIdx < 0 {
			break
		}
		block := s[:endIdx]
		s = s[endIdx+len("endbfchar"):]

		tokens := extractHexTokens(block)
		// Tokens come in pairs: <srcCode> <dstUnicode>
		for i := 0; i+1 < len(tokens); i += 2 {
			src, _ := parseHexToken(tokens[i])
			dstRunes := hexToRunes(tokens[i+1])
			entries = append(entries, bfCharEntry{srcCode: src, dstRunes: dstRunes})
		}
	}

	return entries
}

// parseBFRanges extracts all beginbfrange..endbfrange sections.
func parseBFRanges(s string) []bfRangeEntry {
	var entries []bfRangeEntry

	for {
		startIdx := strings.Index(s, "beginbfrange")
		if startIdx < 0 {
			break
		}
		s = s[startIdx+len("beginbfrange"):]

		endIdx := strings.Index(s, "endbfrange")
		if endIdx < 0 {
			break
		}
		block := s[:endIdx]
		s = s[endIdx+len("endbfrange"):]

		entries = append(entries, parseBFRangeBlock(block)...)
	}

	return entries
}

// parseBFRangeBlock parses a single bfrange block content.
// Supports two formats:
//
//	<srcLo> <srcHi> <dstBase>           — simple range
//	<srcLo> <srcHi> [<dst1> <dst2> ...] — array mapping
func parseBFRangeBlock(block string) []bfRangeEntry {
	var entries []bfRangeEntry

	i := 0
	for i < len(block) {
		// Skip whitespace
		for i < len(block) && isWhitespace(block[i]) {
			i++
		}
		if i >= len(block) {
			break
		}

		// Read srcLo
		srcLoStr, newI := readHexOrBracket(block, i)
		if srcLoStr == "" {
			break
		}
		i = newI
		srcLo, _ := parseHexToken(srcLoStr)

		// Skip whitespace
		for i < len(block) && isWhitespace(block[i]) {
			i++
		}

		// Read srcHi
		srcHiStr, newI2 := readHexOrBracket(block, i)
		if srcHiStr == "" {
			break
		}
		i = newI2
		srcHi, _ := parseHexToken(srcHiStr)

		// Skip whitespace
		for i < len(block) && isWhitespace(block[i]) {
			i++
		}

		if i >= len(block) {
			break
		}

		if block[i] == '[' {
			// Array form: [<dst1> <dst2> ...]
			closeBracket := strings.IndexByte(block[i:], ']')
			if closeBracket < 0 {
				break
			}
			arrayContent := block[i+1 : i+closeBracket]
			i = i + closeBracket + 1

			arrayTokens := extractHexTokens(arrayContent)
			dstArray := make([][]rune, len(arrayTokens))
			for j, tok := range arrayTokens {
				dstArray[j] = hexToRunes(tok)
			}
			entries = append(entries, bfRangeEntry{
				srcLo:    srcLo,
				srcHi:    srcHi,
				dstArray: dstArray,
			})
		} else if block[i] == '<' {
			// Simple range: <dstBase>
			dstStr, newI3 := readHexOrBracket(block, i)
			if dstStr == "" {
				break
			}
			i = newI3
			dstRunes := hexToRunes(dstStr)
			entries = append(entries, bfRangeEntry{
				srcLo:   srcLo,
				srcHi:   srcHi,
				dstBase: dstRunes,
			})
		} else {
			// Unexpected token — skip character
			i++
		}
	}

	return entries
}

// Decode converts a raw byte sequence into a Unicode string using this CMap.
// Uses codeSpaceRanges to determine byte width per code.
// Falls back to 2-byte codes if no codeSpaceRanges are defined (common for Identity-H).
// Unmapped codes produce U+FFFD.
func (cm *cmapTable) Decode(raw []byte) string {
	if cm == nil || len(raw) == 0 {
		return ""
	}

	var buf strings.Builder
	buf.Grow(len(raw))

	i := 0
	for i < len(raw) {
		code, consumed := cm.readCode(raw, i)
		if consumed == 0 {
			// Could not determine code width — skip one byte
			buf.WriteRune(0xFFFD)
			i++
			continue
		}

		runes := cm.lookup(code)
		if runes != nil {
			for _, r := range runes {
				buf.WriteRune(r)
			}
		} else {
			buf.WriteRune(0xFFFD)
		}
		i += consumed
	}

	return buf.String()
}

// readCode reads one character code from raw starting at position i.
// Returns the code value and number of bytes consumed.
// If no codeSpaceRanges are defined, defaults to 2-byte codes.
func (cm *cmapTable) readCode(raw []byte, i int) (uint32, int) {
	if len(cm.codeSpaceRanges) == 0 {
		// Default to 2-byte codes (common for Identity-H CID fonts)
		if i+2 <= len(raw) {
			code := uint32(raw[i])<<8 | uint32(raw[i+1])
			return code, 2
		}
		if i < len(raw) {
			return uint32(raw[i]), 1
		}
		return 0, 0
	}

	// Try matching code space ranges, preferring longer matches
	bestBytes := 0
	var bestCode uint32
	for _, csr := range cm.codeSpaceRanges {
		if i+csr.numBytes > len(raw) {
			continue
		}
		code := readBigEndian(raw[i : i+csr.numBytes])
		if code >= csr.lo && code <= csr.hi {
			if csr.numBytes > bestBytes {
				bestBytes = csr.numBytes
				bestCode = code
			}
		}
	}

	if bestBytes > 0 {
		return bestCode, bestBytes
	}

	// No matching range — try 1-byte fallback
	if i < len(raw) {
		return uint32(raw[i]), 1
	}
	return 0, 0
}

// lookup finds the Unicode mapping for a given code.
// Checks bfCharMap first (O(1)), then bfRanges.
func (cm *cmapTable) lookup(code uint32) []rune {
	// Check bfCharMap first (exact match, O(1))
	if runes, ok := cm.bfCharMap[code]; ok {
		return runes
	}

	// Check bfRanges
	for _, entry := range cm.bfRanges {
		if code >= entry.srcLo && code <= entry.srcHi {
			offset := code - entry.srcLo
			if entry.dstArray != nil {
				idx := int(offset)
				if idx < len(entry.dstArray) {
					return entry.dstArray[idx]
				}
			} else if entry.dstBase != nil && len(entry.dstBase) > 0 {
				// Simple range: increment the last rune of dstBase by offset
				result := make([]rune, len(entry.dstBase))
				copy(result, entry.dstBase)
				result[len(result)-1] += rune(offset)
				return result
			}
		}
	}

	return nil
}

// --- Hex parsing helpers ---

// extractHexTokens extracts all <...> hex tokens from a string.
// Skips PDF dictionary operators << >> to avoid misinterpreting them as hex tokens.
func extractHexTokens(s string) []string {
	var tokens []string
	for {
		open := strings.IndexByte(s, '<')
		if open < 0 {
			break
		}
		// Skip dictionary operators <<
		if open+1 < len(s) && s[open+1] == '<' {
			s = s[open+2:]
			continue
		}
		close := strings.IndexByte(s[open+1:], '>')
		if close < 0 {
			break
		}
		tokens = append(tokens, s[open+1:open+1+close])
		s = s[open+1+close+1:]
	}
	return tokens
}

// readHexOrBracket reads a single <hex> token from s starting at position i.
// Returns the hex content (without angle brackets) and the new position after the token.
// Returns empty string if no token found.
func readHexOrBracket(s string, i int) (string, int) {
	if i >= len(s) || s[i] != '<' {
		return "", i
	}
	end := strings.IndexByte(s[i+1:], '>')
	if end < 0 {
		return "", i
	}
	return s[i+1 : i+1+end], i + 1 + end + 1
}

// parseHexToken parses a hex string (without angle brackets) into a uint32 value.
// Returns the value and the number of bytes it represents (hex digits / 2).
func parseHexToken(hex string) (uint32, int) {
	hex = strings.TrimSpace(hex)
	if len(hex) == 0 {
		return 0, 0
	}

	var val uint32
	digitCount := 0
	for _, c := range hex {
		if isWhitespace(byte(c)) {
			continue
		}
		val <<= 4
		switch {
		case c >= '0' && c <= '9':
			val |= uint32(c - '0')
		case c >= 'A' && c <= 'F':
			val |= uint32(c-'A') + 10
		case c >= 'a' && c <= 'f':
			val |= uint32(c-'a') + 10
		default:
			// Skip invalid hex characters
			continue
		}
		digitCount++
	}

	numBytes := (digitCount + 1) / 2 // round up
	return val, numBytes
}

// hexToRunes converts a hex string (without angle brackets) into a sequence
// of Unicode runes. Each group of 4 hex digits represents one Unicode code point.
// For example, "00410042" becomes ['A', 'B'] and "00660066" becomes ['f', 'f'].
func hexToRunes(hex string) []rune {
	hex = strings.TrimSpace(hex)
	if len(hex) == 0 {
		return nil
	}

	// Strip whitespace
	var clean bytes.Buffer
	for _, c := range hex {
		if !isWhitespace(byte(c)) {
			clean.WriteByte(byte(c))
		}
	}
	hexClean := clean.String()

	// Pad to even length
	if len(hexClean)%2 != 0 {
		hexClean = "0" + hexClean
	}

	// Decode as sequence of bytes
	rawBytes := make([]byte, len(hexClean)/2)
	for i := 0; i < len(hexClean); i += 2 {
		rawBytes[i/2] = hexByte(hexClean[i], hexClean[i+1])
	}

	// Group into 2-byte (16-bit) code points
	if len(rawBytes)%2 != 0 {
		// Odd number of bytes — prepend a zero byte
		rawBytes = append([]byte{0}, rawBytes...)
	}

	runes := make([]rune, len(rawBytes)/2)
	for i := 0; i < len(rawBytes); i += 2 {
		runes[i/2] = rune(uint16(rawBytes[i])<<8 | uint16(rawBytes[i+1]))
	}

	return runes
}

// hexByte converts two hex characters into a byte.
func hexByte(hi, lo byte) byte {
	return hexDigit(hi)<<4 | hexDigit(lo)
}

// hexDigit converts a single hex character to its 4-bit value.
func hexDigit(c byte) byte {
	switch {
	case c >= '0' && c <= '9':
		return c - '0'
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10
	default:
		return 0
	}
}

// readBigEndian reads a big-endian unsigned integer from a byte slice.
func readBigEndian(b []byte) uint32 {
	var val uint32
	for _, by := range b {
		val = val<<8 | uint32(by)
	}
	return val
}

// isWhitespace returns true for ASCII whitespace characters.
func isWhitespace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f'
}
