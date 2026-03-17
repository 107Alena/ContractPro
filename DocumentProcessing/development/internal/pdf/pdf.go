// Package pdf provides PDF inspection and text extraction utilities
// built on top of the pdfcpu library.
package pdf

import (
	"bytes"
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
		text := extractTextFromPage(ctx, i)
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
		text := extractTextFromPage(ctx, i)
		pages = append(pages, PageText{
			PageNumber: i,
			Text:       text,
		})
	}

	return pages, nil
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
// Returns empty string if the page has no text content.
func extractTextFromPage(ctx *model.Context, pageNr int) string {
	r, err := pdfcpu.ExtractPageContent(ctx, pageNr)
	if err != nil {
		return ""
	}

	content, err := io.ReadAll(r)
	if err != nil || len(content) == 0 {
		return ""
	}

	return parseTextFromContentStream(string(content))
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

// decodePDFString performs basic PDF string decoding, handling common escape sequences.
func decodePDFString(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	i := 0
	for i < len(s) {
		if s[i] == '\\' && i+1 < len(s) {
			i++
			switch s[i] {
			case 'n':
				b.WriteByte('\n')
			case 'r':
				b.WriteByte('\r')
			case 't':
				b.WriteByte('\t')
			case 'b':
				b.WriteByte('\b')
			case 'f':
				b.WriteByte('\f')
			case '(':
				b.WriteByte('(')
			case ')':
				b.WriteByte(')')
			case '\\':
				b.WriteByte('\\')
			default:
				// Octal sequence or unknown escape — pass through.
				b.WriteByte(s[i])
			}
		} else {
			b.WriteByte(s[i])
		}
		i++
	}
	return b.String()
}
