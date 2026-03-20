// Package textextract implements the Text Extraction & Normalization Engine.
//
// Two extraction paths:
//   - PDF path: ocrResult is nil or not_applicable → download PDF from storage,
//     extract text via PDF utility.
//   - OCR path: ocrResult has status applicable → split raw OCR text by form-feed
//     characters into pages.
//
// All text undergoes Unicode NFC normalization, garbage character removal,
// and whitespace trimming.
package textextract

import (
	"bytes"
	"context"
	"io"
	"strconv"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"

	"contractpro/document-processing/internal/domain/model"
	"contractpro/document-processing/internal/domain/port"
	pdfpkg "contractpro/document-processing/internal/pdf"
)

// Warning codes emitted during text extraction.
const (
	WarnEmptyPage     = "TEXT_EXTRACTION_EMPTY_PAGE"
	WarnAllPagesEmpty = "TEXT_EXTRACTION_ALL_PAGES_EMPTY"
)

// PDFTextExtractor defines the PDF text extraction capability.
// Satisfied by *pdf.Util.
type PDFTextExtractor interface {
	ExtractText(r io.ReadSeeker) ([]pdfpkg.PageText, error)
}

// Extractor implements port.TextExtractionPort.
type Extractor struct {
	pdfExtractor PDFTextExtractor
	storage      port.TempStoragePort
}

// Compile-time interface compliance check.
var _ port.TextExtractionPort = (*Extractor)(nil)

// NewExtractor creates a new Extractor.
func NewExtractor(pdfExtractor PDFTextExtractor, storage port.TempStoragePort) *Extractor {
	return &Extractor{
		pdfExtractor: pdfExtractor,
		storage:      storage,
	}
}

// Extract extracts and normalizes text from a document.
//
// When ocrResult is non-nil with status "applicable", the raw OCR text is split
// into pages by form-feed characters. Otherwise, the PDF is downloaded from
// storage and text is extracted directly.
func (e *Extractor) Extract(
	ctx context.Context,
	storageKey string,
	ocrResult *model.OCRRawArtifact,
) (*model.ExtractedText, []model.ProcessingWarning, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}

	var rawPages []model.PageText

	if ocrResult != nil && ocrResult.Status == model.OCRStatusApplicable {
		rawPages = splitOCRPages(ocrResult.RawText)
	} else {
		extracted, err := e.extractFromPDF(ctx, storageKey)
		if err != nil {
			return nil, nil, err
		}
		rawPages = extracted
	}

	pages := make([]model.PageText, 0, len(rawPages))
	var warnings []model.ProcessingWarning
	allEmpty := true

	for _, rp := range rawPages {
		normalized := normalizeText(rp.Text)

		pages = append(pages, model.PageText{
			PageNumber: rp.PageNumber,
			Text:       normalized,
		})

		if normalized == "" {
			warnings = append(warnings, model.ProcessingWarning{
				Code:    WarnEmptyPage,
				Message: "page " + strconv.Itoa(rp.PageNumber) + " is empty after text extraction and normalization",
				Stage:   model.ProcessingStageTextExtraction,
			})
		} else {
			allEmpty = false
		}
	}

	if len(pages) > 0 && allEmpty {
		warnings = append(warnings, model.ProcessingWarning{
			Code:    WarnAllPagesEmpty,
			Message: "all pages are empty after text extraction and normalization",
			Stage:   model.ProcessingStageTextExtraction,
		})
	}

	return &model.ExtractedText{DocumentID: storageKey, Pages: pages}, warnings, nil
}

// extractFromPDF downloads PDF from storage and extracts text.
func (e *Extractor) extractFromPDF(ctx context.Context, storageKey string) ([]model.PageText, error) {
	reader, err := e.storage.Download(ctx, storageKey)
	if err != nil {
		return nil, port.NewStorageError("failed to download PDF for text extraction", err)
	}
	defer reader.Close()

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, port.NewStorageError("failed to read PDF data", err)
	}

	pdfPages, err := e.pdfExtractor.ExtractText(bytes.NewReader(data))
	if err != nil {
		return nil, port.NewExtractionError("failed to extract text from PDF", err)
	}

	pages := make([]model.PageText, len(pdfPages))
	for i, pp := range pdfPages {
		pages[i] = model.PageText{PageNumber: pp.PageNumber, Text: pp.Text}
	}
	return pages, nil
}

// splitOCRPages splits OCR raw text by form-feed (\f) into separate pages.
func splitOCRPages(rawText string) []model.PageText {
	segments := strings.Split(rawText, "\f")
	pages := make([]model.PageText, len(segments))
	for i, seg := range segments {
		pages[i] = model.PageText{PageNumber: i + 1, Text: seg}
	}
	return pages
}

// normalizeText applies NFC normalization, removes garbage characters, and trims whitespace.
func normalizeText(s string) string {
	s = norm.NFC.String(s)
	s = cleanText(s)
	s = strings.TrimSpace(s)
	return s
}

// cleanText removes garbage characters while preserving legitimate text.
func cleanText(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if !isGarbage(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// isGarbage returns true for characters that should be stripped:
//   - C0 control chars (U+0000–U+001F) except \t, \n, \r
//   - DEL (U+007F)
//   - C1 control chars (U+0080–U+009F)
//   - Zero-width chars, BOM, replacement char, directional markers
func isGarbage(r rune) bool {
	if r == '\t' || r == '\n' || r == '\r' {
		return false
	}
	if r <= 0x1F || r == 0x7F {
		return true
	}
	if r >= 0x80 && r <= 0x9F {
		return true
	}
	switch r {
	case '\uFEFF', // BOM / zero-width no-break space
		'\uFFFE', // reversed BOM
		'\uFFFD', // replacement character
		'\uFFFC', // object replacement character
		'\u200B', // zero-width space
		'\u200C', // zero-width non-joiner
		'\u200D', // zero-width joiner
		'\u200E', // left-to-right mark
		'\u200F', // right-to-left mark
		'\u202A', // left-to-right embedding
		'\u202B', // right-to-left embedding
		'\u202C', // pop directional formatting
		'\u202D', // left-to-right override
		'\u202E', // right-to-left override
		'\u2060', // word joiner
		'\u2066', // left-to-right isolate
		'\u2067', // right-to-left isolate
		'\u2068', // first strong isolate
		'\u2069': // pop directional isolate
		return true
	}
	if unicode.Is(unicode.Cc, r) {
		return true
	}
	return false
}
