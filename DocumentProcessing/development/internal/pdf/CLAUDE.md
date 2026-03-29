# PDF Package — CLAUDE.md

PDF utilities built on **pdfcpu** for PDF inspection, text detection, and text extraction.

## Main Components

- **pdf.go** — `Util` struct provides stateless PDF operations: `IsValidPDF()`, `GetPageCount()`, `GetInfo()`, `ExtractPageText()`, `ExtractAllPageText()`. Implements `PDFTextExtractor` consumer interface.
- **cmap.go** — CMap parser for PDF ToUnicode CMaps (font encoding maps). Parses CMap binary format to build character code → Unicode mappings.
- **encoding.go** — Standard PDF built-in encodings (WinAnsiEncoding, MacRomanEncoding, etc.). Decodes character codes when no CMap is present.
- **fontcodec.go** — FontCodec combines CMap + encoding data to decode PDF text content. Routes to CMap or fallback encoding based on what's available.
- **data/** — test PDF fixtures: `first.pdf`, `second.pdf` for unit and integration tests.
- **real_pdf_test.go** — integration tests against real PDF fixtures.

## Key Methods

- `IsValidPDF(r io.Reader) bool` — checks PDF magic bytes
- `GetPageCount(r io.Reader) (int, error)` — extracts page count
- `GetInfo(r io.Reader) (*Info, error)` — returns Info (PageCount + IsTextPDF flag)
- `ExtractPageText(r io.Reader, pageNum int) (*PageText, []ExtractionWarning, error)` — single page extraction
- `ExtractAllPageText(r io.Reader) ([]*PageText, []ExtractionWarning, error)` — all pages

## Usage Notes

- All Util methods are **stateless and safe for concurrent use**.
- Use `NewUtil()` constructor.
- Returns `ErrInvalidPDF` for corrupted data, `ErrEmptyReader` for nil/empty readers.
- IsTextPDF = true only if **ALL** pages have extractable text layer.
