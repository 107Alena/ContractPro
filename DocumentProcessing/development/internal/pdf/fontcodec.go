// Package pdf — fontcodec.go builds font-level codec objects from PDF font dictionaries,
// combining ToUnicode CMap, encoding tables, and Differences arrays for correct
// character decoding during text extraction.
package pdf

import (
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

// fontCodec decodes raw byte strings from PDF content streams into Unicode text
// for a specific font. It combines ToUnicode CMap, base encoding, and Differences.
type fontCodec struct {
	name      string          // BaseFont name (for diagnostics)
	subtype   string          // Font subtype: Type1, TrueType, Type0, etc.
	cmap      *cmapTable      // ToUnicode CMap (highest priority)
	encoding  *[256]rune      // Base encoding table (WinAnsi, MacRoman)
	diffMap   map[byte]rune   // Differences overlay on base encoding
	isTwoByte bool            // True for CID fonts (Type0 with Identity-H/V)
}

// decode converts raw bytes from a PDF literal string into Unicode text.
// Priority: CMap > Differences > BaseEncoding > identity fallback.
func (fc *fontCodec) decode(raw []byte) string {
	if fc == nil {
		return identityDecode(raw)
	}

	// ToUnicode CMap takes highest priority
	if fc.cmap != nil {
		result := fc.cmap.Decode(raw)
		// If the CMap produced valid output (not all replacement chars), use it
		if result != "" && !allReplacementChars(result) {
			return result
		}
	}

	// For two-byte (CID) fonts without working CMap, try identity mapping
	if fc.isTwoByte {
		return identityTwoByteDecode(raw)
	}

	// Single-byte: Differences + BaseEncoding
	var buf strings.Builder
	buf.Grow(len(raw))
	for _, b := range raw {
		if fc.diffMap != nil {
			if r, ok := fc.diffMap[b]; ok {
				buf.WriteRune(r)
				continue
			}
		}
		if fc.encoding != nil {
			r := fc.encoding[b]
			if r != 0 {
				buf.WriteRune(r)
				continue
			}
		}
		// Identity fallback
		buf.WriteByte(b)
	}
	return buf.String()
}

// decodeHexString converts a hex string (from <...> in content stream) into Unicode text.
func (fc *fontCodec) decodeHexString(hexStr string) string {
	hexStr = strings.TrimSpace(hexStr)
	if hexStr == "" {
		return ""
	}
	// Pad to even length
	if len(hexStr)%2 != 0 {
		hexStr += "0"
	}
	raw, err := hex.DecodeString(hexStr)
	if err != nil {
		return ""
	}
	return fc.decode(raw)
}

// allReplacementChars returns true if the string consists entirely of U+FFFD.
func allReplacementChars(s string) bool {
	for _, r := range s {
		if r != 0xFFFD {
			return false
		}
	}
	return true
}

// identityDecode performs identity (pass-through) decoding of raw bytes.
func identityDecode(raw []byte) string {
	return string(raw)
}

// identityTwoByteDecode interprets raw as big-endian 2-byte code points.
func identityTwoByteDecode(raw []byte) string {
	var buf strings.Builder
	buf.Grow(len(raw))
	i := 0
	for i+1 < len(raw) {
		cp := rune(raw[i])<<8 | rune(raw[i+1])
		if cp > 0 {
			buf.WriteRune(cp)
		}
		i += 2
	}
	if i < len(raw) {
		buf.WriteByte(raw[i])
	}
	return buf.String()
}

// buildFontCodec constructs a fontCodec from a PDF font dictionary.
// Returns the codec and the font name (for Tf operator tracking).
// On any error, returns nil codec with a warning message in the second return value.
func buildFontCodec(xRefTable *model.XRefTable, fontDict types.Dict) (*fontCodec, string) {
	fc := &fontCodec{}

	// Read /Subtype
	if st := fontDict.NameEntry("Subtype"); st != nil {
		fc.subtype = *st
	}

	// Read /BaseFont
	if bf := fontDict.NameEntry("BaseFont"); bf != nil {
		fc.name = *bf
	}

	// Try /ToUnicode stream
	fc.cmap = extractToUnicode(xRefTable, fontDict)

	// Read /Encoding
	loadEncoding(xRefTable, fontDict, fc)

	// Type0 / CID font handling
	if fc.subtype == "Type0" {
		fc.isTwoByte = true
		// Check /Encoding for Identity-H / Identity-V
		if encName := fontDict.NameEntry("Encoding"); encName != nil {
			if *encName == "Identity-H" || *encName == "Identity-V" {
				fc.isTwoByte = true
			}
		}
		// Try to get ToUnicode from descendant font if not found on top level
		if fc.cmap == nil {
			fc.cmap = extractDescendantToUnicode(xRefTable, fontDict)
		}
	}

	// If we have nothing useful and it's a standard 14 font, skip
	if fc.cmap == nil && fc.encoding == nil && fc.diffMap == nil && isStandard14Font(fc.name) {
		return nil, fc.name
	}

	return fc, fc.name
}

// extractToUnicode reads the /ToUnicode stream from a font dict and parses it as a CMap.
func extractToUnicode(xRefTable *model.XRefTable, fontDict types.Dict) *cmapTable {
	tuObj, found := fontDict.Find("ToUnicode")
	if !found || tuObj == nil {
		return nil
	}

	sd, _, err := xRefTable.DereferenceStreamDict(tuObj)
	if err != nil || sd == nil {
		return nil
	}

	// Decode the stream (apply filters like FlateDecode)
	if err := sd.Decode(); err != nil {
		return nil
	}

	if len(sd.Content) == 0 {
		return nil
	}

	return parseCMap(sd.Content)
}

// extractDescendantToUnicode looks for ToUnicode in DescendantFonts of a Type0 font.
func extractDescendantToUnicode(xRefTable *model.XRefTable, fontDict types.Dict) *cmapTable {
	descArr := fontDict.ArrayEntry("DescendantFonts")
	if descArr == nil {
		return nil
	}

	for _, item := range descArr {
		descDict, err := xRefTable.DereferenceDict(item)
		if err != nil || descDict == nil {
			continue
		}
		if cm := extractToUnicode(xRefTable, descDict); cm != nil {
			return cm
		}
	}

	return nil
}

// loadEncoding reads /Encoding from font dict and populates the codec.
func loadEncoding(xRefTable *model.XRefTable, fontDict types.Dict, fc *fontCodec) {
	encObj, found := fontDict.Find("Encoding")
	if !found || encObj == nil {
		return
	}

	// Dereference if indirect
	encObj, err := xRefTable.Dereference(encObj)
	if err != nil || encObj == nil {
		return
	}

	switch enc := encObj.(type) {
	case types.Name:
		// Simple encoding name: WinAnsiEncoding, MacRomanEncoding, etc.
		fc.encoding = encodingTable(enc.Value())

	case types.Dict:
		// Encoding dictionary with possible BaseEncoding and Differences
		if base := enc.NameEntry("BaseEncoding"); base != nil {
			fc.encoding = encodingTable(*base)
		}

		// Parse Differences array
		diffArr := enc.ArrayEntry("Differences")
		if diffArr != nil {
			fc.diffMap = parseDifferencesFromArray(xRefTable, diffArr)
		}
	}
}

// parseDifferencesFromArray converts a pdfcpu Array into the format expected by
// parseDifferencesArray and calls it.
func parseDifferencesFromArray(xRefTable *model.XRefTable, arr types.Array) map[byte]rune {
	items := make([]interface{}, 0, len(arr))
	for _, obj := range arr {
		// Dereference indirect refs
		resolved, err := xRefTable.Dereference(obj)
		if err != nil {
			continue
		}
		switch v := resolved.(type) {
		case types.Integer:
			items = append(items, v.Value())
		case types.Name:
			items = append(items, v.Value())
		case types.Float:
			items = append(items, int(v.Value()))
		}
	}
	return parseDifferencesArray(items)
}

// buildPageFonts builds font codecs for all fonts referenced on a given page.
// Returns a map from font resource name (e.g. "F1") to fontCodec,
// and a slice of warning messages.
func buildPageFonts(ctx *model.Context, pageNr int) (map[string]*fontCodec, []string) {
	if ctx == nil {
		return nil, nil
	}

	var warnings []string

	// Get the page dict and inherited attributes
	pageDict, _, inhAttrs, err := ctx.PageDict(pageNr, false)
	if err != nil {
		warnings = append(warnings, fmt.Sprintf("page %d: failed to get page dict: %v", pageNr, err))
		return nil, warnings
	}

	// Try Resources from the page dict first, then inherited
	var resDict types.Dict

	resObj, found := pageDict.Find("Resources")
	if found && resObj != nil {
		resDict, err = ctx.DereferenceDict(resObj)
		if err != nil {
			resDict = nil
		}
	}

	// Fall back to inherited resources
	if resDict == nil && inhAttrs != nil && inhAttrs.Resources != nil {
		resDict = inhAttrs.Resources
	}

	if resDict == nil {
		return nil, warnings
	}

	// Get Font sub-dictionary
	fontObj, found := resDict.Find("Font")
	if !found || fontObj == nil {
		return nil, warnings
	}

	fontDict, err := ctx.DereferenceDict(fontObj)
	if err != nil || fontDict == nil {
		return nil, warnings
	}

	codecs := make(map[string]*fontCodec)

	for fontName, fontRef := range fontDict {
		fd, err := ctx.DereferenceDict(fontRef)
		if err != nil || fd == nil {
			warnings = append(warnings, fmt.Sprintf("page %d: font %s: failed to dereference: %v", pageNr, fontName, err))
			continue
		}

		codec, _ := buildFontCodec(ctx.XRefTable, fd)
		if codec != nil {
			codecs[fontName] = codec
		}
	}

	return codecs, warnings
}

// isStandard14Font checks if the font name is one of the PDF standard 14 fonts.
// These fonts use standard encoding and don't require special codec handling.
func isStandard14Font(name string) bool {
	switch name {
	case "Courier", "Courier-Bold", "Courier-Oblique", "Courier-BoldOblique",
		"Helvetica", "Helvetica-Bold", "Helvetica-Oblique", "Helvetica-BoldOblique",
		"Times-Roman", "Times-Bold", "Times-Italic", "Times-BoldItalic",
		"Symbol", "ZapfDingbats":
		return true
	}
	return false
}
