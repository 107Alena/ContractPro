package ocr

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync/atomic"
	"testing"

	"contractpro/document-processing/internal/domain/model"
	"contractpro/document-processing/internal/domain/port"
)

// --- Mock: OCRServicePort ---

type mockOCRService struct {
	recognizeFn func(ctx context.Context, pdfData io.Reader) (string, error)
	called      atomic.Int32
}

func (m *mockOCRService) Recognize(ctx context.Context, pdfData io.Reader) (string, error) {
	m.called.Add(1)
	if m.recognizeFn != nil {
		return m.recognizeFn(ctx, pdfData)
	}
	return "", nil
}

// --- Mock: TempStoragePort ---

type mockTempStorage struct {
	downloadFn func(ctx context.Context, key string) (io.ReadCloser, error)
}

func (m *mockTempStorage) Upload(ctx context.Context, key string, data io.Reader) error {
	return nil
}

func (m *mockTempStorage) Download(ctx context.Context, key string) (io.ReadCloser, error) {
	if m.downloadFn != nil {
		return m.downloadFn(ctx, key)
	}
	return io.NopCloser(strings.NewReader("")), nil
}

func (m *mockTempStorage) Delete(ctx context.Context, key string) error {
	return nil
}

func (m *mockTempStorage) DeleteByPrefix(ctx context.Context, prefix string) error {
	return nil
}

// --- Mock: trackingReadCloser ---

// trackingReadCloser wraps an io.ReadCloser and records whether Close was called.
type trackingReadCloser struct {
	io.ReadCloser
	closed atomic.Int32
}

func (t *trackingReadCloser) Close() error {
	t.closed.Add(1)
	return t.ReadCloser.Close()
}

func (t *trackingReadCloser) wasClosed() bool {
	return t.closed.Load() > 0
}

// --- Tests ---

func TestProcess_TextPDF(t *testing.T) {
	ocr := &mockOCRService{}
	storage := &mockTempStorage{}
	adapter := NewAdapter(ocr, storage)

	result, err := adapter.Process(context.Background(), "key/doc.pdf", true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Status != model.OCRStatusNotApplicable {
		t.Errorf("expected status %q, got %q", model.OCRStatusNotApplicable, result.Status)
	}
	if result.RawText != "" {
		t.Errorf("expected empty raw_text, got %q", result.RawText)
	}
	if ocr.called.Load() != 0 {
		t.Errorf("OCR service should not be called for text PDF, called %d times", ocr.called.Load())
	}
}

func TestProcess_ScanPDF(t *testing.T) {
	const ocrText = "Recognized OCR text from scanned PDF"
	const storageKey = "jobs/job-1/source.pdf"

	ocr := &mockOCRService{
		recognizeFn: func(_ context.Context, _ io.Reader) (string, error) {
			return ocrText, nil
		},
	}
	storage := &mockTempStorage{
		downloadFn: func(_ context.Context, key string) (io.ReadCloser, error) {
			if key != storageKey {
				t.Errorf("expected storage key %q, got %q", storageKey, key)
			}
			return io.NopCloser(strings.NewReader("fake-pdf-bytes")), nil
		},
	}
	adapter := NewAdapter(ocr, storage)

	result, err := adapter.Process(context.Background(), storageKey, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Status != model.OCRStatusApplicable {
		t.Errorf("expected status %q, got %q", model.OCRStatusApplicable, result.Status)
	}
	if result.RawText != ocrText {
		t.Errorf("expected raw_text %q, got %q", ocrText, result.RawText)
	}
	if ocr.called.Load() != 1 {
		t.Errorf("OCR service should be called once, called %d times", ocr.called.Load())
	}
}

func TestProcess_ScanPDF_StorageError(t *testing.T) {
	storageErr := errors.New("connection refused")
	storage := &mockTempStorage{
		downloadFn: func(_ context.Context, _ string) (io.ReadCloser, error) {
			return nil, storageErr
		},
	}
	ocr := &mockOCRService{}
	adapter := NewAdapter(ocr, storage)

	_, err := adapter.Process(context.Background(), "key/doc.pdf", false)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	if code := port.ErrorCode(err); code != port.ErrCodeStorageFailed {
		t.Errorf("expected error code %q, got %q", port.ErrCodeStorageFailed, code)
	}
	if !port.IsRetryable(err) {
		t.Error("storage errors should be retryable")
	}
	if ocr.called.Load() != 0 {
		t.Error("OCR service should not be called when storage fails")
	}
}

func TestProcess_ScanPDF_OCRError(t *testing.T) {
	ocrErr := errors.New("OCR service internal error")
	ocr := &mockOCRService{
		recognizeFn: func(_ context.Context, _ io.Reader) (string, error) {
			return "", ocrErr
		},
	}
	storage := &mockTempStorage{
		downloadFn: func(_ context.Context, _ string) (io.ReadCloser, error) {
			return io.NopCloser(strings.NewReader("fake-pdf-bytes")), nil
		},
	}
	adapter := NewAdapter(ocr, storage)

	_, err := adapter.Process(context.Background(), "key/doc.pdf", false)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	if code := port.ErrorCode(err); code != port.ErrCodeOCRFailed {
		t.Errorf("expected error code %q, got %q", port.ErrCodeOCRFailed, code)
	}
	// Non-DomainError from OCR → IsRetryable returns false → non-retryable OCR error.
	if port.IsRetryable(err) {
		t.Error("non-domain OCR error should not be retryable")
	}
}

func TestProcess_ScanPDF_OCRRetryableError(t *testing.T) {
	retryableOCRErr := port.NewOCRError("rate limit exceeded", true, errors.New("429"))
	ocr := &mockOCRService{
		recognizeFn: func(_ context.Context, _ io.Reader) (string, error) {
			return "", retryableOCRErr
		},
	}
	storage := &mockTempStorage{
		downloadFn: func(_ context.Context, _ string) (io.ReadCloser, error) {
			return io.NopCloser(strings.NewReader("fake-pdf-bytes")), nil
		},
	}
	adapter := NewAdapter(ocr, storage)

	_, err := adapter.Process(context.Background(), "key/doc.pdf", false)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	if code := port.ErrorCode(err); code != port.ErrCodeOCRFailed {
		t.Errorf("expected error code %q, got %q", port.ErrCodeOCRFailed, code)
	}
	if !port.IsRetryable(err) {
		t.Error("retryable OCR error should preserve retryability")
	}
}

func TestProcess_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	ocr := &mockOCRService{}
	storage := &mockTempStorage{
		downloadFn: func(ctx context.Context, _ string) (io.ReadCloser, error) {
			// Storage respects context cancellation.
			return nil, ctx.Err()
		},
	}
	adapter := NewAdapter(ocr, storage)

	_, err := adapter.Process(ctx, "key/doc.pdf", false)
	if err == nil {
		t.Fatal("expected error for cancelled context, got nil")
	}
	if ocr.called.Load() != 0 {
		t.Error("OCR service should not be called when context is cancelled")
	}
}

func TestProcess_ScanPDF_ReaderClosed(t *testing.T) {
	tracker := &trackingReadCloser{
		ReadCloser: io.NopCloser(strings.NewReader("fake-pdf-bytes")),
	}

	ocr := &mockOCRService{
		recognizeFn: func(_ context.Context, _ io.Reader) (string, error) {
			return "text", nil
		},
	}
	storage := &mockTempStorage{
		downloadFn: func(_ context.Context, _ string) (io.ReadCloser, error) {
			return tracker, nil
		},
	}
	adapter := NewAdapter(ocr, storage)

	result, err := adapter.Process(context.Background(), "key/doc.pdf", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Status != model.OCRStatusApplicable {
		t.Errorf("expected status %q, got %q", model.OCRStatusApplicable, result.Status)
	}
	if !tracker.wasClosed() {
		t.Error("reader from storage must be closed after use")
	}
}
