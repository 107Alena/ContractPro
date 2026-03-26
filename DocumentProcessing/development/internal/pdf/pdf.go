// Package pdf provides PDF inspection and text extraction utilities
// built on top of the pdfcpu library.
package pdf

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
)

// Sentinel errors returned by Util methods.
var (
	ErrInvalidPDF  = errors.New("pdf: invalid or corrupted PDF data")
	ErrEmptyReader = errors.New("pdf: empty reader provided")
)

// pdfMagic is the PDF file signature (%PDF = 0x25504446).
var pdfMagic = []byte{0x25, 0x50, 0x44, 0x46}

// PageText holds extracted text for a single page.
type PageText struct {
	PageNumber int    // 1-based page number
	Text       string // extracted text content (may be empty for scan pages)
}

// ExtractionWarning records a non-fatal warning encountered during text extraction.
type ExtractionWarning struct {
	PageNumber int    // 1-based page number (0 if not page-specific)
	Message    string // human-readable warning message
}

// Info holds metadata extracted from a PDF during analysis.
type Info struct {
	PageCount int  // total number of pages
	IsTextPDF bool // true if ALL pages have extractable text layer
}

// Util provides PDF inspection and text extraction utilities.
// Stateless and safe for concurrent use.
type Util struct{}

// NewUtil creates a new Util instance.
func NewUtil() *Util {
	return &Util{}
}

// IsValidPDF checks if data starts with PDF magic bytes (%PDF = 0x25504446).
// Reads only the first 4 bytes. Returns false on any read error or if the
// reader provides fewer than 4 bytes.
func (u *Util) IsValidPDF(r io.Reader) bool {
	if r == nil {
		return false
	}
	buf := make([]byte, 4)
	n, _ := io.ReadFull(r, buf)
	if n < 4 {
		return false
	}
	return bytes.Equal(buf, pdfMagic)
}

// Analyze reads the PDF and returns page count + text/scan classification.
// A PDF is classified as text-based ONLY if ALL pages contain extractable text.
// If even one page has no text, IsTextPDF is false.
// For a 0-page PDF, IsTextPDF is false.
func (u *Util) Analyze(r io.ReadSeeker) (*Info, error) {
	if r == nil {
		return nil, ErrEmptyReader
	}

	ctx, err := readPDF(r)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidPDF, err)
	}

	pageCount := ctx.PageCount
	if pageCount == 0 {
		return &Info{PageCount: 0, IsTextPDF: false}, nil
	}

	allText := true
	for i := 1; i <= pageCount; i++ {
		text, _ := extractTextFromPage(ctx, i)
		if strings.TrimSpace(text) == "" {
			allText = false
			break
		}
	}

	return &Info{
		PageCount: pageCount,
		IsTextPDF: allText,
	}, nil
}

// ExtractText extracts text content from each page of the PDF.
// Returns a slice of PageText ordered by page number (1-based).
// Pages with no extractable text return empty strings.
// This method discards extraction warnings; use ExtractTextWithWarnings
// to capture them.
func (u *Util) ExtractText(r io.ReadSeeker) ([]PageText, error) {
	if r == nil {
		return nil, ErrEmptyReader
	}

	ctx, err := readPDF(r)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidPDF, err)
	}

	pages := make([]PageText, 0, ctx.PageCount)
	for i := 1; i <= ctx.PageCount; i++ {
		text, _ := extractTextFromPage(ctx, i)
		pages = append(pages, PageText{
			PageNumber: i,
			Text:       text,
		})
	}

	return pages, nil
}

// ExtractTextWithWarnings extracts text content from each page using font-aware
// decoding (ToUnicode CMap, encoding tables, Differences arrays) for correct
// Cyrillic and other non-Latin text extraction from PDFs with embedded fonts.
// Returns extracted pages, any non-fatal warnings, and an error if the PDF is invalid.
func (u *Util) ExtractTextWithWarnings(r io.ReadSeeker) ([]PageText, []ExtractionWarning, error) {
	if r == nil {
		return nil, nil, ErrEmptyReader
	}

	ctx, err := readPDF(r)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %v", ErrInvalidPDF, err)
	}

	var allWarnings []ExtractionWarning
	pages := make([]PageText, 0, ctx.PageCount)

	for i := 1; i <= ctx.PageCount; i++ {
		text, warns := extractTextFromPage(ctx, i)
		pages = append(pages, PageText{
			PageNumber: i,
			Text:       text,
		})
		for _, w := range warns {
			allWarnings = append(allWarnings, ExtractionWarning{
				PageNumber: i,
				Message:    w,
			})
		}
	}

	return pages, allWarnings, nil
}

// readPDF parses and validates a PDF from the given reader.
func readPDF(r io.ReadSeeker) (*model.Context, error) {
	// Disable pdfcpu config dir to avoid filesystem side effects.
	api.DisableConfigDir()

	conf := model.NewDefaultConfiguration()
	conf.ValidationMode = model.ValidationRelaxed

	ctx, err := api.ReadAndValidate(r, conf)
	if err != nil {
		return nil, err
	}

	return ctx, nil
}

// extractTextFromPage extracts human-readable text from a single page's content stream.
// Returns the extracted text and any non-fatal warnings.
// If the page has no text content, returns empty string with no warnings.
func extractTextFromPage(ctx *model.Context, pageNr int) (string, []string) {
	r, err := pdfcpu.ExtractPageContent(ctx, pageNr)
	if err != nil {
		return "", nil
	}

	content, err := io.ReadAll(r)
	if err != nil || len(content) == 0 {
		return "", nil
	}

	// Try font-aware extraction first
	fonts, fontWarnings := buildPageFonts(ctx, pageNr)
	if len(fonts) > 0 {
		text, parseWarnings := parseContentStreamWithFonts(string(content), fonts)
		return text, append(fontWarnings, parseWarnings...)
	}

	// Fall back to legacy regex-based extraction (no fonts available)
	return parseTextFromContentStream(string(content)), fontWarnings
}

// reTj matches the Tj operator: (string) Tj
var reTj = regexp.MustCompile(`\(([^)]*)\)\s*Tj`)

// reTjQuote matches the ' operator (move to next line and show text): (string) '
var reTjQuote = regexp.MustCompile(`\(([^)]*)\)\s*'`)

// reTjDoubleQuote matches the " operator: aw ac (string) "
var reTjDoubleQuote = regexp.MustCompile(`\(([^)]*)\)\s*"`)

// reTJ matches individual string elements inside TJ arrays: [(str) num (str)] TJ
var reTJ = regexp.MustCompile(`\(([^)]*)\)`)

// reTJBlock matches the entire TJ array expression: [...] TJ
var reTJBlock = regexp.MustCompile(`\[([^\]]*)\]\s*TJ`)

// parseTextFromContentStream extracts text strings from PDF content stream operators.
// It handles Tj, TJ, ', and " text-showing operators inside BT...ET blocks.
// This is the legacy extraction path with no font decoding.
func parseTextFromContentStream(content string) string {
	var parts []string

	// Find all BT...ET blocks (text objects).
	btEtBlocks := extractBTETBlocks(content)

	for _, block := range btEtBlocks {
		// Extract text from Tj operators: (text) Tj
		for _, match := range reTj.FindAllStringSubmatch(block, -1) {
			if len(match) > 1 {
				parts = append(parts, decodePDFString(match[1]))
			}
		}

		// Extract text from ' operators: (text) '
		for _, match := range reTjQuote.FindAllStringSubmatch(block, -1) {
			if len(match) > 1 {
				parts = append(parts, decodePDFString(match[1]))
			}
		}

		// Extract text from " operators: aw ac (text) "
		for _, match := range reTjDoubleQuote.FindAllStringSubmatch(block, -1) {
			if len(match) > 1 {
				parts = append(parts, decodePDFString(match[1]))
			}
		}

		// Extract text from TJ arrays: [(text) kerning (text)] TJ
		for _, match := range reTJBlock.FindAllStringSubmatch(block, -1) {
			if len(match) > 1 {
				for _, sub := range reTJ.FindAllStringSubmatch(match[1], -1) {
					if len(sub) > 1 {
						parts = append(parts, decodePDFString(sub[1]))
					}
				}
			}
		}
	}

	return strings.Join(parts, "")
}

// reFontOp matches the Tf operator: /FontName size Tf
var reFontOp = regexp.MustCompile(`/(\S+)\s+[\d.]+\s+Tf`)

// parseContentStreamWithFonts extracts text from content stream operators using
// font codec decoding for correct character mapping. It processes BT/ET blocks,
// tracks the active font via Tf operators, and decodes literal strings (...)
// and hex strings <...> through the appropriate fontCodec.
// Returns extracted text and any warnings.
func parseContentStreamWithFonts(content string, fonts map[string]*fontCodec) (string, []string) {
	var parts []string
	var warnings []string

	btEtBlocks := extractBTETBlocks(content)

	for _, block := range btEtBlocks {
		var activeCodec *fontCodec

		// Process the block line by line, tracking font changes
		// We scan sequentially to handle interleaved Tf and text operators
		i := 0
		blockLen := len(block)

		for i < blockLen {
			// Skip whitespace
			for i < blockLen && (block[i] == ' ' || block[i] == '\t' || block[i] == '\n' || block[i] == '\r' || block[i] == '\f') {
				i++
			}
			if i >= blockLen {
				break
			}

			// Check for Tf operator: /FontName size Tf
			if block[i] == '/' {
				// Try to match font setting
				remaining := block[i:]
				if loc := reFontOp.FindStringSubmatchIndex(remaining); loc != nil && loc[0] == 0 {
					if loc[2] >= 0 && loc[3] >= 0 {
						fontName := remaining[loc[2]:loc[3]]
						if fc, ok := fonts[fontName]; ok {
							activeCodec = fc
						} else {
							activeCodec = nil
						}
					}
					i += loc[1]
					continue
				}
				// Not a Tf — skip to next whitespace
				for i < blockLen && block[i] != ' ' && block[i] != '\t' && block[i] != '\n' && block[i] != '\r' {
					i++
				}
				continue
			}

			// Check for hex string: <...>
			if block[i] == '<' && (i+1 >= blockLen || block[i+1] != '<') {
				hexStr, end := readHexStringFromContent(block, i)
				if end > i {
					text := decodeHexWithCodec(hexStr, activeCodec)
					// Check if this is followed by a text-showing operator
					rest := strings.TrimLeft(block[end:], " \t\n\r")
					if isTextShowingOp(rest) {
						parts = append(parts, text)
					}
					i = end
					continue
				}
				i++
				continue
			}

			// Check for literal string: (...)
			if block[i] == '(' {
				litStr, end := readLiteralString(block, i)
				if end > i {
					// Check if followed by Tj, ', or " operator
					rest := strings.TrimLeft(block[end:], " \t\n\r")
					if isTextShowingOp(rest) {
						raw := decodePDFStringToBytes(litStr)
						if activeCodec != nil {
							parts = append(parts, activeCodec.decode(raw))
						} else {
							parts = append(parts, decodePDFString(litStr))
						}
					}
					i = end
					continue
				}
				i++
				continue
			}

			// Check for TJ array: [...]
			if block[i] == '[' {
				arrayContent, end := readTJArray(block, i)
				if end > i {
					// Check if followed by TJ
					rest := strings.TrimLeft(block[end:], " \t\n\r")
					if len(rest) >= 2 && rest[0] == 'T' && rest[1] == 'J' {
						parts = append(parts, decodeTJArrayWithFonts(arrayContent, activeCodec)...)
					}
					i = end
					continue
				}
				i++
				continue
			}

			// Skip other tokens
			for i < blockLen && block[i] != ' ' && block[i] != '\t' && block[i] != '\n' && block[i] != '\r' &&
				block[i] != '(' && block[i] != '<' && block[i] != '[' && block[i] != '/' {
				i++
			}
		}
	}

	return strings.Join(parts, ""), warnings
}

// readLiteralString reads a PDF literal string starting at position i (which must be '(').
// Handles nested parentheses and backslash escapes.
// Returns the string content (without outer parens) and the position after the closing ')'.
func readLiteralString(s string, i int) (string, int) {
	if i >= len(s) || s[i] != '(' {
		return "", i
	}

	depth := 1
	j := i + 1
	var buf strings.Builder

	for j < len(s) && depth > 0 {
		ch := s[j]
		if ch == '\\' && j+1 < len(s) {
			buf.WriteByte('\\')
			j++
			buf.WriteByte(s[j])
			j++
			continue
		}
		if ch == '(' {
			depth++
		} else if ch == ')' {
			depth--
			if depth == 0 {
				return buf.String(), j + 1
			}
		}
		buf.WriteByte(ch)
		j++
	}

	// Unmatched parens — return what we have
	return buf.String(), j
}

// readHexStringFromContent reads a hex string <...> starting at position i.
// Returns the hex content (without angle brackets) and position after '>'.
func readHexStringFromContent(s string, i int) (string, int) {
	if i >= len(s) || s[i] != '<' {
		return "", i
	}
	j := i + 1
	for j < len(s) && s[j] != '>' {
		j++
	}
	if j >= len(s) {
		return "", i
	}
	return s[i+1 : j], j + 1
}

// readTJArray reads a TJ array [...] starting at position i.
// Returns the array content (without brackets) and position after ']'.
func readTJArray(s string, i int) (string, int) {
	if i >= len(s) || s[i] != '[' {
		return "", i
	}
	// Find matching ']', accounting for nested parens and hex strings
	j := i + 1
	depth := 0
	for j < len(s) {
		ch := s[j]
		if ch == '(' {
			depth++
		} else if ch == ')' {
			if depth > 0 {
				depth--
			}
		} else if ch == '\\' && depth > 0 && j+1 < len(s) {
			j += 2
			continue
		} else if ch == ']' && depth == 0 {
			return s[i+1 : j], j + 1
		}
		j++
	}
	return "", i
}

// isTextShowingOp checks if the string starts with a text-showing operator (Tj, ', ").
func isTextShowingOp(s string) bool {
	if len(s) == 0 {
		return false
	}
	// Tj
	if len(s) >= 2 && s[0] == 'T' && s[1] == 'j' {
		return true
	}
	// ' (move to next line and show text)
	if s[0] == '\'' {
		return true
	}
	// " (set word/char spacing and show text)
	if s[0] == '"' {
		return true
	}
	return false
}

// decodeHexWithCodec decodes a hex string using the given font codec.
func decodeHexWithCodec(hexStr string, fc *fontCodec) string {
	hexStr = strings.TrimSpace(hexStr)
	if hexStr == "" {
		return ""
	}
	// Pad to even length
	if len(hexStr)%2 != 0 {
		hexStr += "0"
	}
	raw, err := hex.DecodeString(strings.Map(func(r rune) rune {
		if (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F') {
			return r
		}
		return -1
	}, hexStr))
	if err != nil {
		return ""
	}
	if fc != nil {
		return fc.decode(raw)
	}
	return identityDecode(raw)
}

// decodeTJArrayWithFonts extracts and decodes string elements from a TJ array content.
func decodeTJArrayWithFonts(arrayContent string, fc *fontCodec) []string {
	var parts []string
	i := 0
	for i < len(arrayContent) {
		ch := arrayContent[i]
		if ch == '(' {
			litStr, end := readLiteralString(arrayContent, i)
			if end > i {
				raw := decodePDFStringToBytes(litStr)
				if fc != nil {
					parts = append(parts, fc.decode(raw))
				} else {
					parts = append(parts, decodePDFString(litStr))
				}
				i = end
				continue
			}
			i++
		} else if ch == '<' {
			hexStr, end := readHexStringFromContent(arrayContent, i)
			if end > i {
				parts = append(parts, decodeHexWithCodec(hexStr, fc))
				i = end
				continue
			}
			i++
		} else {
			i++
		}
	}
	return parts
}

// decodePDFStringToBytes converts a PDF string (with escape sequences) to raw bytes.
// This is similar to decodePDFString but returns bytes instead of string,
// preserving the raw byte values for font codec processing.
func decodePDFStringToBytes(s string) []byte {
	var buf []byte
	i := 0
	for i < len(s) {
		if s[i] == '\\' && i+1 < len(s) {
			i++
			switch s[i] {
			case 'n':
				buf = append(buf, '\n')
			case 'r':
				buf = append(buf, '\r')
			case 't':
				buf = append(buf, '\t')
			case 'b':
				buf = append(buf, '\b')
			case 'f':
				buf = append(buf, '\f')
			case '(':
				buf = append(buf, '(')
			case ')':
				buf = append(buf, ')')
			case '\\':
				buf = append(buf, '\\')
			default:
				// Octal sequence
				if s[i] >= '0' && s[i] <= '7' {
					val := s[i] - '0'
					j := 1
					for j < 3 && i+j < len(s) && s[i+j] >= '0' && s[i+j] <= '7' {
						val = val*8 + (s[i+j] - '0')
						j++
					}
					buf = append(buf, val)
					i += j
					continue
				}
				buf = append(buf, s[i])
			}
		} else {
			buf = append(buf, s[i])
		}
		i++
	}
	return buf
}

// isStandaloneOperator checks that the operator at pos (with given length) in s
// is not part of a longer alphanumeric token.
func isStandaloneOperator(s string, pos, length int) bool {
	if pos > 0 && isAlphaNum(s[pos-1]) {
		return false
	}
	end := pos + length
	if end < len(s) && isAlphaNum(s[end]) {
		return false
	}
	return true
}

// findStandaloneOperator finds the first standalone occurrence of op in s.
// Returns -1 if not found.
func findStandaloneOperator(s, op string) int {
	offset := 0
	for {
		idx := strings.Index(s[offset:], op)
		if idx < 0 {
			return -1
		}
		absIdx := offset + idx
		if isStandaloneOperator(s, absIdx, len(op)) {
			return absIdx
		}
		offset = absIdx + len(op)
	}
}

// extractBTETBlocks returns all BT...ET text object blocks from a content stream.
func extractBTETBlocks(content string) []string {
	var blocks []string
	s := content
	for {
		btIdx := findStandaloneOperator(s, "BT")
		if btIdx < 0 {
			break
		}
		rest := s[btIdx+2:]
		etIdx := findStandaloneOperator(rest, "ET")
		if etIdx < 0 {
			break
		}
		blocks = append(blocks, rest[:etIdx])
		s = rest[etIdx+2:]
	}
	return blocks
}

// isAlphaNum returns true if b is an ASCII letter or digit.
func isAlphaNum(b byte) bool {
	return (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z') || (b >= '0' && b <= '9')
}

// decodePDFString performs PDF string decoding, handling escape sequences
// including octal (\NNN). Delegates to decodePDFStringToBytes for the actual work.
func decodePDFString(s string) string {
	return string(decodePDFStringToBytes(s))
}
