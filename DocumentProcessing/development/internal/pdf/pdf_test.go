package pdf

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
)

// --- Test PDF generators ---

// generateTextPDF creates a valid PDF with one or more pages, each containing
// the corresponding text string rendered via the Tj operator.
// Uses the built-in Helvetica font (no embedding required).
func generateTextPDF(t *testing.T, pages []string) []byte {
	t.Helper()

	var objects []string
	objectOffsets := make([]int, 0)
	currentOffset := 0

	header := "%PDF-1.4\n"
	currentOffset = len(header)

	// Object 1: Catalog
	obj1 := "1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n"
	objectOffsets = append(objectOffsets, currentOffset)
	currentOffset += len(obj1)
	objects = append(objects, obj1)

	// Object 2: Pages (will be built after we know page refs)
	// Reserve slot — we'll build it later.
	pageCount := len(pages)
	kidsRefs := make([]string, pageCount)
	// Page objects start at object 4 (object 3 is font).
	// Each page has 2 objects: page dict and content stream.
	for i := 0; i < pageCount; i++ {
		pageObjNum := 4 + i*2
		kidsRefs[i] = fmt.Sprintf("%d 0 R", pageObjNum)
	}
	obj2 := fmt.Sprintf("2 0 obj\n<< /Type /Pages /Kids [%s] /Count %d >>\nendobj\n",
		strings.Join(kidsRefs, " "), pageCount)
	objectOffsets = append(objectOffsets, currentOffset)
	currentOffset += len(obj2)
	objects = append(objects, obj2)

	// Object 3: Font (Helvetica — built-in, no embedding needed)
	obj3 := "3 0 obj\n<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>\nendobj\n"
	objectOffsets = append(objectOffsets, currentOffset)
	currentOffset += len(obj3)
	objects = append(objects, obj3)

	// Page objects: each page = page dict + content stream
	for i, text := range pages {
		pageObjNum := 4 + i*2
		contentObjNum := pageObjNum + 1

		// Content stream: BT (text) Tj ET
		contentStream := fmt.Sprintf("BT /F1 12 Tf 100 700 Td (%s) Tj ET", escapePDFString(text))
		contentLength := len(contentStream)

		// Content stream object
		contentObj := fmt.Sprintf("%d 0 obj\n<< /Length %d >>\nstream\n%s\nendstream\nendobj\n",
			contentObjNum, contentLength, contentStream)

		// Page dict object
		pageObj := fmt.Sprintf("%d 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents %d 0 R /Resources << /Font << /F1 3 0 R >> >> >>\nendobj\n",
			pageObjNum, contentObjNum)

		objectOffsets = append(objectOffsets, currentOffset)
		currentOffset += len(pageObj)
		objects = append(objects, pageObj)

		objectOffsets = append(objectOffsets, currentOffset)
		currentOffset += len(contentObj)
		objects = append(objects, contentObj)
	}

	// Build the full PDF
	var buf bytes.Buffer
	buf.WriteString(header)
	for _, obj := range objects {
		buf.WriteString(obj)
	}

	// Cross-reference table
	xrefOffset := buf.Len()
	totalObjects := len(objectOffsets) + 1 // +1 for object 0 (free)
	buf.WriteString(fmt.Sprintf("xref\n0 %d\n", totalObjects))
	buf.WriteString("0000000000 65535 f \n")
	for _, off := range objectOffsets {
		buf.WriteString(fmt.Sprintf("%010d 00000 n \n", off))
	}

	// Trailer
	buf.WriteString(fmt.Sprintf("trailer\n<< /Size %d /Root 1 0 R >>\n", totalObjects))
	buf.WriteString(fmt.Sprintf("startxref\n%d\n%%%%EOF\n", xrefOffset))

	return buf.Bytes()
}

// generateEmptyPagePDF creates a valid PDF with the specified number of pages,
// where each page has a content stream with only drawing operations (no text).
// This simulates a scanned PDF with images but no extractable text layer.
func generateEmptyPagePDF(t *testing.T, pageCount int) []byte {
	t.Helper()

	var objects []string
	objectOffsets := make([]int, 0)
	currentOffset := 0

	header := "%PDF-1.4\n"
	currentOffset = len(header)

	// Object 1: Catalog
	obj1 := "1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n"
	objectOffsets = append(objectOffsets, currentOffset)
	currentOffset += len(obj1)
	objects = append(objects, obj1)

	// Object 2: Pages
	kidsRefs := make([]string, pageCount)
	for i := 0; i < pageCount; i++ {
		pageObjNum := 3 + i*2
		kidsRefs[i] = fmt.Sprintf("%d 0 R", pageObjNum)
	}
	obj2 := fmt.Sprintf("2 0 obj\n<< /Type /Pages /Kids [%s] /Count %d >>\nendobj\n",
		strings.Join(kidsRefs, " "), pageCount)
	objectOffsets = append(objectOffsets, currentOffset)
	currentOffset += len(obj2)
	objects = append(objects, obj2)

	// Page objects: each page = page dict + content stream (drawing only, no text)
	for i := 0; i < pageCount; i++ {
		pageObjNum := 3 + i*2
		contentObjNum := pageObjNum + 1

		// Content stream with only graphics operators (no BT/ET, no Tj/TJ)
		contentStream := "q 0.5 0.5 0.5 rg 100 100 200 200 re f Q"
		contentLength := len(contentStream)

		contentObj := fmt.Sprintf("%d 0 obj\n<< /Length %d >>\nstream\n%s\nendstream\nendobj\n",
			contentObjNum, contentLength, contentStream)

		pageObj := fmt.Sprintf("%d 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents %d 0 R /Resources << >> >>\nendobj\n",
			pageObjNum, contentObjNum)

		objectOffsets = append(objectOffsets, currentOffset)
		currentOffset += len(pageObj)
		objects = append(objects, pageObj)

		objectOffsets = append(objectOffsets, currentOffset)
		currentOffset += len(contentObj)
		objects = append(objects, contentObj)
	}

	// Build PDF
	var buf bytes.Buffer
	buf.WriteString(header)
	for _, obj := range objects {
		buf.WriteString(obj)
	}

	xrefOffset := buf.Len()
	totalObjects := len(objectOffsets) + 1
	buf.WriteString(fmt.Sprintf("xref\n0 %d\n", totalObjects))
	buf.WriteString("0000000000 65535 f \n")
	for _, off := range objectOffsets {
		buf.WriteString(fmt.Sprintf("%010d 00000 n \n", off))
	}

	buf.WriteString(fmt.Sprintf("trailer\n<< /Size %d /Root 1 0 R >>\n", totalObjects))
	buf.WriteString(fmt.Sprintf("startxref\n%d\n%%%%EOF\n", xrefOffset))

	return buf.Bytes()
}

// generateMixedPDF creates a PDF where some pages have text and others do not.
// textPages maps 0-based page indices to their text content.
// Pages not present in the map get empty content streams (no text).
func generateMixedPDF(t *testing.T, totalPages int, textPages map[int]string) []byte {
	t.Helper()

	var objects []string
	objectOffsets := make([]int, 0)
	currentOffset := 0

	header := "%PDF-1.4\n"
	currentOffset = len(header)

	// Object 1: Catalog
	obj1 := "1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n"
	objectOffsets = append(objectOffsets, currentOffset)
	currentOffset += len(obj1)
	objects = append(objects, obj1)

	// Object 2: Pages
	kidsRefs := make([]string, totalPages)
	// Object 3 = font, pages start at 4
	for i := 0; i < totalPages; i++ {
		pageObjNum := 4 + i*2
		kidsRefs[i] = fmt.Sprintf("%d 0 R", pageObjNum)
	}
	obj2 := fmt.Sprintf("2 0 obj\n<< /Type /Pages /Kids [%s] /Count %d >>\nendobj\n",
		strings.Join(kidsRefs, " "), totalPages)
	objectOffsets = append(objectOffsets, currentOffset)
	currentOffset += len(obj2)
	objects = append(objects, obj2)

	// Object 3: Font
	obj3 := "3 0 obj\n<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>\nendobj\n"
	objectOffsets = append(objectOffsets, currentOffset)
	currentOffset += len(obj3)
	objects = append(objects, obj3)

	for i := 0; i < totalPages; i++ {
		pageObjNum := 4 + i*2
		contentObjNum := pageObjNum + 1

		var contentStream string
		var resources string
		if text, ok := textPages[i]; ok {
			contentStream = fmt.Sprintf("BT /F1 12 Tf 100 700 Td (%s) Tj ET", escapePDFString(text))
			resources = "/Resources << /Font << /F1 3 0 R >> >>"
		} else {
			contentStream = "q 0.5 0.5 0.5 rg 100 100 200 200 re f Q"
			resources = "/Resources << >>"
		}
		contentLength := len(contentStream)

		contentObj := fmt.Sprintf("%d 0 obj\n<< /Length %d >>\nstream\n%s\nendstream\nendobj\n",
			contentObjNum, contentLength, contentStream)

		pageObj := fmt.Sprintf("%d 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents %d 0 R %s >>\nendobj\n",
			pageObjNum, contentObjNum, resources)

		objectOffsets = append(objectOffsets, currentOffset)
		currentOffset += len(pageObj)
		objects = append(objects, pageObj)

		objectOffsets = append(objectOffsets, currentOffset)
		currentOffset += len(contentObj)
		objects = append(objects, contentObj)
	}

	var buf bytes.Buffer
	buf.WriteString(header)
	for _, obj := range objects {
		buf.WriteString(obj)
	}

	xrefOffset := buf.Len()
	totalObjects := len(objectOffsets) + 1
	buf.WriteString(fmt.Sprintf("xref\n0 %d\n", totalObjects))
	buf.WriteString("0000000000 65535 f \n")
	for _, off := range objectOffsets {
		buf.WriteString(fmt.Sprintf("%010d 00000 n \n", off))
	}

	buf.WriteString(fmt.Sprintf("trailer\n<< /Size %d /Root 1 0 R >>\n", totalObjects))
	buf.WriteString(fmt.Sprintf("startxref\n%d\n%%%%EOF\n", xrefOffset))

	return buf.Bytes()
}

// escapePDFString escapes special characters in a PDF string literal.
func escapePDFString(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "(", "\\(")
	s = strings.ReplaceAll(s, ")", "\\)")
	return s
}

// --- IsValidPDF tests ---

func TestIsValidPDF(t *testing.T) {
	u := NewUtil()

	tests := []struct {
		name string
		data []byte
		want bool
	}{
		{
			name: "valid PDF header",
			data: []byte("%PDF-1.4 rest of file..."),
			want: true,
		},
		{
			name: "empty reader",
			data: []byte{},
			want: false,
		},
		{
			name: "non-PDF file (PNG header)",
			data: []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A},
			want: false,
		},
		{
			name: "too short data (2 bytes)",
			data: []byte{0x25, 0x50},
			want: false,
		},
		{
			name: "just magic bytes without rest",
			data: []byte{0x25, 0x50, 0x44, 0x46},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := u.IsValidPDF(bytes.NewReader(tt.data))
			if got != tt.want {
				t.Errorf("IsValidPDF() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsValidPDF_NilReader(t *testing.T) {
	u := NewUtil()
	if u.IsValidPDF(nil) {
		t.Error("IsValidPDF(nil) should return false")
	}
}

// --- Analyze tests ---

func TestAnalyze_SinglePageTextPDF(t *testing.T) {
	u := NewUtil()
	data := generateTextPDF(t, []string{"Hello World"})

	info, err := u.Analyze(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("Analyze() error: %v", err)
	}
	if info.PageCount != 1 {
		t.Errorf("PageCount = %d, want 1", info.PageCount)
	}
	if !info.IsTextPDF {
		t.Error("IsTextPDF = false, want true")
	}
}

func TestAnalyze_MultiPageTextPDF(t *testing.T) {
	u := NewUtil()
	data := generateTextPDF(t, []string{"Page one", "Page two", "Page three"})

	info, err := u.Analyze(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("Analyze() error: %v", err)
	}
	if info.PageCount != 3 {
		t.Errorf("PageCount = %d, want 3", info.PageCount)
	}
	if !info.IsTextPDF {
		t.Error("IsTextPDF = false, want true")
	}
}

func TestAnalyze_ScanPDF(t *testing.T) {
	u := NewUtil()
	data := generateEmptyPagePDF(t, 2)

	info, err := u.Analyze(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("Analyze() error: %v", err)
	}
	if info.PageCount != 2 {
		t.Errorf("PageCount = %d, want 2", info.PageCount)
	}
	if info.IsTextPDF {
		t.Error("IsTextPDF = true, want false (scan-like PDF)")
	}
}

func TestAnalyze_MixedPDF(t *testing.T) {
	u := NewUtil()
	// 3 pages: page 0 has text, page 1 has no text, page 2 has text
	data := generateMixedPDF(t, 3, map[int]string{
		0: "Has text",
		2: "Also has text",
	})

	info, err := u.Analyze(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("Analyze() error: %v", err)
	}
	if info.PageCount != 3 {
		t.Errorf("PageCount = %d, want 3", info.PageCount)
	}
	if info.IsTextPDF {
		t.Error("IsTextPDF = true, want false (not ALL pages have text)")
	}
}

func TestAnalyze_CorruptedData(t *testing.T) {
	u := NewUtil()

	_, err := u.Analyze(bytes.NewReader([]byte("not a pdf at all")))
	if err == nil {
		t.Fatal("Analyze() expected error for corrupted data, got nil")
	}
}

func TestAnalyze_NilReader(t *testing.T) {
	u := NewUtil()

	_, err := u.Analyze(nil)
	if err == nil {
		t.Fatal("Analyze(nil) expected error, got nil")
	}
	if err != ErrEmptyReader {
		t.Errorf("expected ErrEmptyReader, got %v", err)
	}
}

// --- ExtractText tests ---

func TestExtractText_SinglePage(t *testing.T) {
	u := NewUtil()
	data := generateTextPDF(t, []string{"Hello World"})

	pages, err := u.ExtractText(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("ExtractText() error: %v", err)
	}
	if len(pages) != 1 {
		t.Fatalf("got %d pages, want 1", len(pages))
	}
	if pages[0].PageNumber != 1 {
		t.Errorf("PageNumber = %d, want 1", pages[0].PageNumber)
	}
	if !strings.Contains(pages[0].Text, "Hello World") {
		t.Errorf("Text = %q, want to contain 'Hello World'", pages[0].Text)
	}
}

func TestExtractText_MultiPage(t *testing.T) {
	u := NewUtil()
	texts := []string{"First page", "Second page", "Third page"}
	data := generateTextPDF(t, texts)

	pages, err := u.ExtractText(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("ExtractText() error: %v", err)
	}
	if len(pages) != 3 {
		t.Fatalf("got %d pages, want 3", len(pages))
	}
	for i, page := range pages {
		if page.PageNumber != i+1 {
			t.Errorf("page[%d].PageNumber = %d, want %d", i, page.PageNumber, i+1)
		}
		if !strings.Contains(page.Text, texts[i]) {
			t.Errorf("page[%d].Text = %q, want to contain %q", i, page.Text, texts[i])
		}
	}
}

func TestExtractText_ScanPDF(t *testing.T) {
	u := NewUtil()
	data := generateEmptyPagePDF(t, 2)

	pages, err := u.ExtractText(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("ExtractText() error: %v", err)
	}
	if len(pages) != 2 {
		t.Fatalf("got %d pages, want 2", len(pages))
	}
	for i, page := range pages {
		if page.PageNumber != i+1 {
			t.Errorf("page[%d].PageNumber = %d, want %d", i, page.PageNumber, i+1)
		}
		if strings.TrimSpace(page.Text) != "" {
			t.Errorf("page[%d].Text = %q, want empty for scan page", i, page.Text)
		}
	}
}

func TestExtractText_CorruptedData(t *testing.T) {
	u := NewUtil()

	_, err := u.ExtractText(bytes.NewReader([]byte("garbage data")))
	if err == nil {
		t.Fatal("ExtractText() expected error for corrupted data, got nil")
	}
}

func TestExtractText_PageOrdering(t *testing.T) {
	u := NewUtil()
	texts := []string{"Alpha", "Beta", "Gamma", "Delta", "Epsilon"}
	data := generateTextPDF(t, texts)

	pages, err := u.ExtractText(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("ExtractText() error: %v", err)
	}
	if len(pages) != 5 {
		t.Fatalf("got %d pages, want 5", len(pages))
	}
	for i := 0; i < len(pages); i++ {
		if pages[i].PageNumber != i+1 {
			t.Errorf("pages[%d].PageNumber = %d, want %d (ordering broken)", i, pages[i].PageNumber, i+1)
		}
	}
}

func TestExtractText_NilReader(t *testing.T) {
	u := NewUtil()

	_, err := u.ExtractText(nil)
	if err == nil {
		t.Fatal("ExtractText(nil) expected error, got nil")
	}
	if err != ErrEmptyReader {
		t.Errorf("expected ErrEmptyReader, got %v", err)
	}
}

// --- Internal helper tests ---

func TestDecodePDFString(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "plain text", input: "hello", want: "hello"},
		{name: "escaped newline", input: `hello\nworld`, want: "hello\nworld"},
		{name: "escaped parens", input: `\(text\)`, want: "(text)"},
		{name: "escaped backslash", input: `path\\to`, want: "path\\to"},
		{name: "escaped tab", input: `col1\tcol2`, want: "col1\tcol2"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := decodePDFString(tt.input)
			if got != tt.want {
				t.Errorf("decodePDFString(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestExtractBTETBlocks(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    int
	}{
		{
			name:    "single BT/ET block",
			content: "q BT /F1 12 Tf (Hello) Tj ET Q",
			want:    1,
		},
		{
			name:    "two BT/ET blocks",
			content: "BT (A) Tj ET BT (B) Tj ET",
			want:    2,
		},
		{
			name:    "no BT/ET blocks",
			content: "q 100 100 200 200 re f Q",
			want:    0,
		},
		{
			name:    "ET inside word should not close block",
			content: "BT /F1 12 Tf (PREDMET DOGOVORA) Tj ET",
			want:    1,
		},
		{
			name:    "BT inside word should not open block",
			content: "ABORT BT (Hello) Tj ET",
			want:    1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			blocks := extractBTETBlocks(tt.content)
			if len(blocks) != tt.want {
				t.Errorf("extractBTETBlocks() returned %d blocks, want %d", len(blocks), tt.want)
			}
		})
	}
}

func TestParseTextFromContentStream(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{
			name:    "Tj operator",
			content: "BT /F1 12 Tf 100 700 Td (Hello World) Tj ET",
			want:    "Hello World",
		},
		{
			name:    "TJ array operator",
			content: "BT /F1 12 Tf 100 700 Td [(Hel) -10 (lo)] TJ ET",
			want:    "Hello",
		},
		{
			name:    "no text operators",
			content: "q 0.5 0.5 0.5 rg 100 100 200 200 re f Q",
			want:    "",
		},
		{
			name:    "multiple Tj in one BT block",
			content: "BT /F1 12 Tf 100 700 Td (Hello) Tj 0 -20 Td (World) Tj ET",
			want:    "HelloWorld",
		},
		{
			name:    "text containing ET-like substrings",
			content: "BT /F1 12 Tf 100 700 Td (PREDMET DOGOVORA i RASCHETOV) Tj ET",
			want:    "PREDMET DOGOVORA i RASCHETOV",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseTextFromContentStream(tt.content)
			if got != tt.want {
				t.Errorf("parseTextFromContentStream() = %q, want %q", got, tt.want)
			}
		})
	}
}

// --- NewUtil constructor test ---

func TestNewUtil(t *testing.T) {
	u := NewUtil()
	if u == nil {
		t.Fatal("NewUtil() returned nil")
	}
}
