package textextract

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"contractpro/document-processing/internal/domain/model"
	"contractpro/document-processing/internal/domain/port"
	pdfpkg "contractpro/document-processing/internal/pdf"
)

// --- Mocks ---

type mockStorage struct {
	downloadFn func(ctx context.Context, key string) (io.ReadCloser, error)
}

func (m *mockStorage) Upload(ctx context.Context, key string, data io.Reader) error { return nil }
func (m *mockStorage) Download(ctx context.Context, key string) (io.ReadCloser, error) {
	return m.downloadFn(ctx, key)
}
func (m *mockStorage) Delete(ctx context.Context, key string) error          { return nil }
func (m *mockStorage) DeleteByPrefix(ctx context.Context, prefix string) error { return nil }

type mockPDFExtractor struct {
	extractTextFn func(r io.ReadSeeker) ([]pdfpkg.PageText, error)
}

func (m *mockPDFExtractor) ExtractText(r io.ReadSeeker) ([]pdfpkg.PageText, error) {
	return m.extractTextFn(r)
}

// readerCloser wraps a strings.Reader with a Close method and tracks close calls.
type readerCloser struct {
	*strings.Reader
	closed bool
}

func (r *readerCloser) Close() error {
	r.closed = true
	return nil
}

func newReaderCloser(s string) *readerCloser {
	return &readerCloser{Reader: strings.NewReader(s), closed: false}
}

// --- Tests ---

func TestExtract_TextPDF_Success(t *testing.T) {
	storage := &mockStorage{
		downloadFn: func(ctx context.Context, key string) (io.ReadCloser, error) {
			return newReaderCloser("pdf-data"), nil
		},
	}
	pdfEx := &mockPDFExtractor{
		extractTextFn: func(r io.ReadSeeker) ([]pdfpkg.PageText, error) {
			return []pdfpkg.PageText{
				{PageNumber: 1, Text: "Договор оказания услуг"},
				{PageNumber: 2, Text: "Предмет договора"},
			}, nil
		},
	}

	ext := NewExtractor(pdfEx, storage)
	result, warnings, err := ext.Extract(context.Background(), "jobs/123/doc.pdf", nil)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("expected no warnings, got %d", len(warnings))
	}
	if result.DocumentID != "jobs/123/doc.pdf" {
		t.Errorf("DocumentID = %q, want %q", result.DocumentID, "jobs/123/doc.pdf")
	}
	if len(result.Pages) != 2 {
		t.Fatalf("expected 2 pages, got %d", len(result.Pages))
	}
	if result.Pages[0].Text != "Договор оказания услуг" {
		t.Errorf("page 1 text = %q, want %q", result.Pages[0].Text, "Договор оказания услуг")
	}
	if result.Pages[1].Text != "Предмет договора" {
		t.Errorf("page 2 text = %q, want %q", result.Pages[1].Text, "Предмет договора")
	}
}

func TestExtract_OCR_Success(t *testing.T) {
	storage := &mockStorage{
		downloadFn: func(ctx context.Context, key string) (io.ReadCloser, error) {
			t.Fatal("storage should not be called for OCR path")
			return nil, nil
		},
	}
	pdfEx := &mockPDFExtractor{
		extractTextFn: func(r io.ReadSeeker) ([]pdfpkg.PageText, error) {
			t.Fatal("pdfExtractor should not be called for OCR path")
			return nil, nil
		},
	}

	ocrResult := &model.OCRRawArtifact{
		Status:  model.OCRStatusApplicable,
		RawText: "Распознанный текст договора",
	}

	ext := NewExtractor(pdfEx, storage)
	result, warnings, err := ext.Extract(context.Background(), "jobs/123/doc.pdf", ocrResult)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("expected no warnings, got %d", len(warnings))
	}
	if len(result.Pages) != 1 {
		t.Fatalf("expected 1 page, got %d", len(result.Pages))
	}
	if result.Pages[0].PageNumber != 1 {
		t.Errorf("page number = %d, want 1", result.Pages[0].PageNumber)
	}
	if result.Pages[0].Text != "Распознанный текст договора" {
		t.Errorf("page text = %q, want %q", result.Pages[0].Text, "Распознанный текст договора")
	}
}

func TestExtract_OCR_FormFeedSplit(t *testing.T) {
	ocrResult := &model.OCRRawArtifact{
		Status:  model.OCRStatusApplicable,
		RawText: "Страница 1\fСтраница 2\fСтраница 3",
	}

	ext := NewExtractor(&mockPDFExtractor{}, &mockStorage{})
	result, warnings, err := ext.Extract(context.Background(), "key", ocrResult)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("expected no warnings, got %d", len(warnings))
	}
	if len(result.Pages) != 3 {
		t.Fatalf("expected 3 pages, got %d", len(result.Pages))
	}
	for i, want := range []string{"Страница 1", "Страница 2", "Страница 3"} {
		if result.Pages[i].PageNumber != i+1 {
			t.Errorf("page[%d].PageNumber = %d, want %d", i, result.Pages[i].PageNumber, i+1)
		}
		if result.Pages[i].Text != want {
			t.Errorf("page[%d].Text = %q, want %q", i, result.Pages[i].Text, want)
		}
	}
}

func TestExtract_StorageError(t *testing.T) {
	storage := &mockStorage{
		downloadFn: func(ctx context.Context, key string) (io.ReadCloser, error) {
			return nil, errors.New("connection refused")
		},
	}

	ext := NewExtractor(&mockPDFExtractor{}, storage)
	_, _, err := ext.Extract(context.Background(), "key", nil)

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if port.ErrorCode(err) != port.ErrCodeStorageFailed {
		t.Errorf("error code = %q, want %q", port.ErrorCode(err), port.ErrCodeStorageFailed)
	}
	if !port.IsRetryable(err) {
		t.Error("storage error should be retryable")
	}
}

func TestExtract_PDFExtractionError(t *testing.T) {
	storage := &mockStorage{
		downloadFn: func(ctx context.Context, key string) (io.ReadCloser, error) {
			return newReaderCloser("bad-pdf"), nil
		},
	}
	pdfEx := &mockPDFExtractor{
		extractTextFn: func(r io.ReadSeeker) ([]pdfpkg.PageText, error) {
			return nil, errors.New("corrupted PDF")
		},
	}

	ext := NewExtractor(pdfEx, storage)
	_, _, err := ext.Extract(context.Background(), "key", nil)

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if port.ErrorCode(err) != port.ErrCodeExtractionFailed {
		t.Errorf("error code = %q, want %q", port.ErrorCode(err), port.ErrCodeExtractionFailed)
	}
	if port.IsRetryable(err) {
		t.Error("extraction error should not be retryable")
	}
}

func TestExtract_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	ext := NewExtractor(&mockPDFExtractor{}, &mockStorage{})
	_, _, err := ext.Extract(ctx, "key", nil)

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

func TestExtract_ContextCancelledBetweenDownloadAndExtract(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	storage := &mockStorage{
		downloadFn: func(ctx context.Context, key string) (io.ReadCloser, error) {
			cancel() // cancel after download succeeds
			return newReaderCloser("pdf-data"), nil
		},
	}

	ext := NewExtractor(&mockPDFExtractor{}, storage)
	_, _, err := ext.Extract(ctx, "key", nil)

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

func TestExtract_NormalizationNFC(t *testing.T) {
	// й as decomposed: и (U+0438) + combining breve (U+0306)
	decomposed := "\u0438\u0306"
	composed := "\u0439" // й as single codepoint

	ocrResult := &model.OCRRawArtifact{
		Status:  model.OCRStatusApplicable,
		RawText: "Догово" + decomposed + "р",
	}

	ext := NewExtractor(&mockPDFExtractor{}, &mockStorage{})
	result, _, err := ext.Extract(context.Background(), "key", ocrResult)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "Догово" + composed + "р"
	if result.Pages[0].Text != want {
		t.Errorf("text = %q, want %q", result.Pages[0].Text, want)
	}
}

func TestExtract_GarbageCharRemoval(t *testing.T) {
	// Text with various garbage characters mixed in
	input := "Договор\u0000 оказания\u200B услуг\uFEFF\uFFFD\u200E"

	ocrResult := &model.OCRRawArtifact{
		Status:  model.OCRStatusApplicable,
		RawText: input,
	}

	ext := NewExtractor(&mockPDFExtractor{}, &mockStorage{})
	result, _, err := ext.Extract(context.Background(), "key", ocrResult)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "Договор оказания услуг"
	if result.Pages[0].Text != want {
		t.Errorf("text = %q, want %q", result.Pages[0].Text, want)
	}
}

func TestExtract_PreservesWhitespace(t *testing.T) {
	input := "Строка 1\n\tСтрока 2\r\nСтрока 3"

	ocrResult := &model.OCRRawArtifact{
		Status:  model.OCRStatusApplicable,
		RawText: input,
	}

	ext := NewExtractor(&mockPDFExtractor{}, &mockStorage{})
	result, _, err := ext.Extract(context.Background(), "key", ocrResult)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Pages[0].Text != input {
		t.Errorf("text = %q, want %q", result.Pages[0].Text, input)
	}
}

func TestExtract_EmptyPageWarning(t *testing.T) {
	storage := &mockStorage{
		downloadFn: func(ctx context.Context, key string) (io.ReadCloser, error) {
			return newReaderCloser("pdf"), nil
		},
	}
	pdfEx := &mockPDFExtractor{
		extractTextFn: func(r io.ReadSeeker) ([]pdfpkg.PageText, error) {
			return []pdfpkg.PageText{
				{PageNumber: 1, Text: "Текст первой страницы"},
				{PageNumber: 2, Text: ""},
				{PageNumber: 3, Text: "Текст третьей страницы"},
			}, nil
		},
	}

	ext := NewExtractor(pdfEx, storage)
	result, warnings, err := ext.Extract(context.Background(), "key", nil)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Pages) != 3 {
		t.Fatalf("expected 3 pages, got %d", len(result.Pages))
	}
	if len(warnings) != 1 {
		t.Fatalf("expected 1 warning, got %d", len(warnings))
	}
	if warnings[0].Code != WarnEmptyPage {
		t.Errorf("warning code = %q, want %q", warnings[0].Code, WarnEmptyPage)
	}
	if !strings.Contains(warnings[0].Message, "2") {
		t.Errorf("warning message should mention page 2, got %q", warnings[0].Message)
	}
}

func TestExtract_AllPagesEmptyWarning(t *testing.T) {
	storage := &mockStorage{
		downloadFn: func(ctx context.Context, key string) (io.ReadCloser, error) {
			return newReaderCloser("pdf"), nil
		},
	}
	pdfEx := &mockPDFExtractor{
		extractTextFn: func(r io.ReadSeeker) ([]pdfpkg.PageText, error) {
			return []pdfpkg.PageText{
				{PageNumber: 1, Text: ""},
				{PageNumber: 2, Text: "   "},
			}, nil
		},
	}

	ext := NewExtractor(pdfEx, storage)
	_, warnings, err := ext.Extract(context.Background(), "key", nil)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// 2 empty page warnings + 1 all-pages-empty warning
	if len(warnings) != 3 {
		t.Fatalf("expected 3 warnings, got %d", len(warnings))
	}

	emptyPageCount := 0
	allPagesEmpty := false
	for _, w := range warnings {
		switch w.Code {
		case WarnEmptyPage:
			emptyPageCount++
		case WarnAllPagesEmpty:
			allPagesEmpty = true
		}
	}
	if emptyPageCount != 2 {
		t.Errorf("expected 2 empty page warnings, got %d", emptyPageCount)
	}
	if !allPagesEmpty {
		t.Error("expected all-pages-empty warning")
	}
}

func TestExtract_WarningStage(t *testing.T) {
	ocrResult := &model.OCRRawArtifact{
		Status:  model.OCRStatusApplicable,
		RawText: "",
	}

	ext := NewExtractor(&mockPDFExtractor{}, &mockStorage{})
	_, warnings, err := ext.Extract(context.Background(), "key", ocrResult)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for i, w := range warnings {
		if w.Stage != model.ProcessingStageTextExtraction {
			t.Errorf("warning[%d].Stage = %q, want %q", i, w.Stage, model.ProcessingStageTextExtraction)
		}
	}
}

func TestExtract_ReaderClosed(t *testing.T) {
	rc := newReaderCloser("pdf-data")
	storage := &mockStorage{
		downloadFn: func(ctx context.Context, key string) (io.ReadCloser, error) {
			return rc, nil
		},
	}
	pdfEx := &mockPDFExtractor{
		extractTextFn: func(r io.ReadSeeker) ([]pdfpkg.PageText, error) {
			return []pdfpkg.PageText{{PageNumber: 1, Text: "text"}}, nil
		},
	}

	ext := NewExtractor(pdfEx, storage)
	_, _, err := ext.Extract(context.Background(), "key", nil)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !rc.closed {
		t.Error("storage reader was not closed")
	}
}

func TestExtract_ReaderClosedOnError(t *testing.T) {
	rc := newReaderCloser("pdf-data")
	storage := &mockStorage{
		downloadFn: func(ctx context.Context, key string) (io.ReadCloser, error) {
			return rc, nil
		},
	}
	pdfEx := &mockPDFExtractor{
		extractTextFn: func(r io.ReadSeeker) ([]pdfpkg.PageText, error) {
			return nil, errors.New("extraction failed")
		},
	}

	ext := NewExtractor(pdfEx, storage)
	_, _, _ = ext.Extract(context.Background(), "key", nil)

	if !rc.closed {
		t.Error("storage reader was not closed after extraction error")
	}
}

func TestExtract_NilOCRResult(t *testing.T) {
	downloadCalled := false
	storage := &mockStorage{
		downloadFn: func(ctx context.Context, key string) (io.ReadCloser, error) {
			downloadCalled = true
			return newReaderCloser("pdf"), nil
		},
	}
	pdfEx := &mockPDFExtractor{
		extractTextFn: func(r io.ReadSeeker) ([]pdfpkg.PageText, error) {
			return []pdfpkg.PageText{{PageNumber: 1, Text: "text"}}, nil
		},
	}

	ext := NewExtractor(pdfEx, storage)
	_, _, err := ext.Extract(context.Background(), "key", nil)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !downloadCalled {
		t.Error("expected PDF path (storage.Download) for nil ocrResult")
	}
}

func TestExtract_OCRNotApplicable(t *testing.T) {
	downloadCalled := false
	storage := &mockStorage{
		downloadFn: func(ctx context.Context, key string) (io.ReadCloser, error) {
			downloadCalled = true
			return newReaderCloser("pdf"), nil
		},
	}
	pdfEx := &mockPDFExtractor{
		extractTextFn: func(r io.ReadSeeker) ([]pdfpkg.PageText, error) {
			return []pdfpkg.PageText{{PageNumber: 1, Text: "text"}}, nil
		},
	}

	ocrResult := &model.OCRRawArtifact{Status: model.OCRStatusNotApplicable}

	ext := NewExtractor(pdfEx, storage)
	_, _, err := ext.Extract(context.Background(), "key", ocrResult)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !downloadCalled {
		t.Error("expected PDF path (storage.Download) for not_applicable ocrResult")
	}
}

func TestExtract_InterfaceCompliance(t *testing.T) {
	var _ port.TextExtractionPort = (*Extractor)(nil)
}
