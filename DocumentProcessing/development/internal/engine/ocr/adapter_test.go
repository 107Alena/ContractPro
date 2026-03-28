package ocr

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"contractpro/document-processing/internal/domain/model"
	"contractpro/document-processing/internal/domain/port"
)

// --- Mock: OCRServicePort ---

type mockOCRService struct {
	recognizeFns []func(ctx context.Context, pdfData io.Reader) (string, error)
	called       atomic.Int32
}

func (m *mockOCRService) Recognize(ctx context.Context, pdfData io.Reader) (string, error) {
	idx := int(m.called.Add(1)) - 1
	if len(m.recognizeFns) == 0 {
		return "", nil
	}
	if idx < len(m.recognizeFns) {
		return m.recognizeFns[idx](ctx, pdfData)
	}
	// Fall back to last function.
	return m.recognizeFns[len(m.recognizeFns)-1](ctx, pdfData)
}

// --- Mock: TempStoragePort ---

type mockTempStorage struct {
	downloadFns []func(ctx context.Context, key string) (io.ReadCloser, error)
	called      atomic.Int32
}

func (m *mockTempStorage) Upload(ctx context.Context, key string, data io.Reader) error {
	return nil
}

func (m *mockTempStorage) Download(ctx context.Context, key string) (io.ReadCloser, error) {
	idx := int(m.called.Add(1)) - 1
	if len(m.downloadFns) == 0 {
		return io.NopCloser(strings.NewReader("")), nil
	}
	if idx < len(m.downloadFns) {
		return m.downloadFns[idx](ctx, key)
	}
	// Fall back to last function.
	return m.downloadFns[len(m.downloadFns)-1](ctx, key)
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

func (t *trackingReadCloser) closeCount() int32 {
	return t.closed.Load()
}

// --- Helper: default adapter params ---

func newTestAdapter(ocr *mockOCRService, storage *mockTempStorage) *Adapter {
	return NewAdapter(ocr, storage, 100, 3, 1*time.Millisecond)
}

func newTestAdapterNoRetry(ocr *mockOCRService, storage *mockTempStorage) *Adapter {
	return NewAdapter(ocr, storage, 100, 1, 1*time.Millisecond)
}

func defaultDownloadFn(key string) func(ctx context.Context, k string) (io.ReadCloser, error) {
	return func(_ context.Context, k string) (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader("fake-pdf-bytes")), nil
	}
}

// --- Tests: Existing (updated constructor) ---

func TestProcess_TextPDF(t *testing.T) {
	ocr := &mockOCRService{}
	storage := &mockTempStorage{}
	adapter := newTestAdapter(ocr, storage)

	result, warnings, err := adapter.Process(context.Background(), "key/doc.pdf", true)
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
	if len(warnings) != 0 {
		t.Errorf("expected no warnings for text PDF, got %v", warnings)
	}
}

func TestProcess_ScanPDF(t *testing.T) {
	const ocrText = "Recognized OCR text from scanned PDF — достаточно длинный текст для проверки порога"
	const storageKey = "jobs/job-1/source.pdf"

	ocr := &mockOCRService{
		recognizeFns: []func(context.Context, io.Reader) (string, error){
			func(_ context.Context, _ io.Reader) (string, error) {
				return ocrText, nil
			},
		},
	}
	storage := &mockTempStorage{
		downloadFns: []func(context.Context, string) (io.ReadCloser, error){
			func(_ context.Context, key string) (io.ReadCloser, error) {
				if key != storageKey {
					t.Errorf("expected storage key %q, got %q", storageKey, key)
				}
				return io.NopCloser(strings.NewReader("fake-pdf-bytes")), nil
			},
		},
	}
	adapter := newTestAdapter(ocr, storage)

	result, warnings, err := adapter.Process(context.Background(), storageKey, false)
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
	if len(warnings) != 0 {
		t.Errorf("expected no warnings, got %v", warnings)
	}
}

func TestProcess_ScanPDF_StorageError(t *testing.T) {
	storageErr := errors.New("connection refused")
	storage := &mockTempStorage{
		downloadFns: []func(context.Context, string) (io.ReadCloser, error){
			func(_ context.Context, _ string) (io.ReadCloser, error) {
				return nil, storageErr
			},
		},
	}
	ocr := &mockOCRService{}
	adapter := newTestAdapter(ocr, storage)

	_, _, err := adapter.Process(context.Background(), "key/doc.pdf", false)
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
		recognizeFns: []func(context.Context, io.Reader) (string, error){
			func(_ context.Context, _ io.Reader) (string, error) {
				return "", ocrErr
			},
		},
	}
	storage := &mockTempStorage{
		downloadFns: []func(context.Context, string) (io.ReadCloser, error){
			defaultDownloadFn(""),
		},
	}
	adapter := newTestAdapter(ocr, storage)

	_, _, err := adapter.Process(context.Background(), "key/doc.pdf", false)
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
	if ocr.called.Load() != 1 {
		t.Errorf("OCR should be called once (no retry for non-retryable), called %d times", ocr.called.Load())
	}
}

func TestProcess_ScanPDF_OCRRetryableError(t *testing.T) {
	retryableOCRErr := port.NewOCRError("rate limit exceeded", true, errors.New("429"))
	ocr := &mockOCRService{
		recognizeFns: []func(context.Context, io.Reader) (string, error){
			func(_ context.Context, _ io.Reader) (string, error) {
				return "", retryableOCRErr
			},
		},
	}
	storage := &mockTempStorage{
		downloadFns: []func(context.Context, string) (io.ReadCloser, error){
			defaultDownloadFn(""),
		},
	}
	adapter := newTestAdapter(ocr, storage) // maxAttempts=3

	_, _, err := adapter.Process(context.Background(), "key/doc.pdf", false)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	if code := port.ErrorCode(err); code != port.ErrCodeOCRFailed {
		t.Errorf("expected error code %q, got %q", port.ErrCodeOCRFailed, code)
	}
	if !port.IsRetryable(err) {
		t.Error("retryable OCR error should preserve retryability")
	}
	if ocr.called.Load() != 3 {
		t.Errorf("OCR should be called 3 times (3 attempts), called %d times", ocr.called.Load())
	}
	// 1 initial download + 2 re-downloads for retry attempts.
	if storage.called.Load() != 3 {
		t.Errorf("storage should be called 3 times, called %d times", storage.called.Load())
	}
	if !strings.Contains(err.Error(), "after 3 attempts") {
		t.Errorf("error message should mention attempts, got: %v", err)
	}
}

func TestProcess_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	ocr := &mockOCRService{}
	storage := &mockTempStorage{
		downloadFns: []func(context.Context, string) (io.ReadCloser, error){
			func(ctx context.Context, _ string) (io.ReadCloser, error) {
				// Storage respects context cancellation.
				return nil, ctx.Err()
			},
		},
	}
	adapter := newTestAdapter(ocr, storage)

	_, _, err := adapter.Process(ctx, "key/doc.pdf", false)
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
		recognizeFns: []func(context.Context, io.Reader) (string, error){
			func(_ context.Context, _ io.Reader) (string, error) {
				return "Recognized text that is long enough to avoid warnings from the adapter", nil
			},
		},
	}
	storage := &mockTempStorage{
		downloadFns: []func(context.Context, string) (io.ReadCloser, error){
			func(_ context.Context, _ string) (io.ReadCloser, error) {
				return tracker, nil
			},
		},
	}
	adapter := newTestAdapter(ocr, storage)

	result, _, err := adapter.Process(context.Background(), "key/doc.pdf", false)
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

// --- Tests: Rate Limiter ---

func TestRateLimiter_AcquireImmediate(t *testing.T) {
	ocr := &mockOCRService{}
	storage := &mockTempStorage{}
	adapter := NewAdapter(ocr, storage, 100, 1, 1*time.Millisecond)

	start := time.Now()
	err := adapter.acquireToken(context.Background())
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if elapsed > 50*time.Millisecond {
		t.Errorf("acquireToken should return immediately with high rpsLimit, took %v", elapsed)
	}
}

func TestRateLimiter_RespectsContextCancellation(t *testing.T) {
	ocr := &mockOCRService{}
	storage := &mockTempStorage{}
	adapter := NewAdapter(ocr, storage, 1, 1, 1*time.Millisecond)

	// Drain all tokens.
	if err := adapter.acquireToken(context.Background()); err != nil {
		t.Fatalf("failed to drain token: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	err := adapter.acquireToken(ctx)
	if err == nil {
		t.Fatal("expected error for cancelled context, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

func TestRateLimiter_ConcurrentAccess(t *testing.T) {
	ocr := &mockOCRService{}
	storage := &mockTempStorage{}
	adapter := NewAdapter(ocr, storage, 5, 1, 1*time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	const goroutines = 10
	var wg sync.WaitGroup
	errs := make([]error, goroutines)

	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			errs[idx] = adapter.acquireToken(ctx)
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("goroutine %d failed: %v", i, err)
		}
	}
}

// --- Tests: Retry ---

func TestProcess_RetrySucceedsOnSecondAttempt(t *testing.T) {
	retryableErr := port.NewOCRError("temporary failure", true, errors.New("503"))
	const ocrText = "Recognized text on second attempt — достаточно длинный для порога"

	ocr := &mockOCRService{
		recognizeFns: []func(context.Context, io.Reader) (string, error){
			func(_ context.Context, _ io.Reader) (string, error) {
				return "", retryableErr
			},
			func(_ context.Context, _ io.Reader) (string, error) {
				return ocrText, nil
			},
		},
	}
	storage := &mockTempStorage{
		downloadFns: []func(context.Context, string) (io.ReadCloser, error){
			defaultDownloadFn(""),
		},
	}
	adapter := newTestAdapter(ocr, storage)

	result, _, err := adapter.Process(context.Background(), "key/doc.pdf", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Status != model.OCRStatusApplicable {
		t.Errorf("expected status %q, got %q", model.OCRStatusApplicable, result.Status)
	}
	if result.RawText != ocrText {
		t.Errorf("expected raw_text %q, got %q", ocrText, result.RawText)
	}
	if ocr.called.Load() != 2 {
		t.Errorf("OCR should be called 2 times, called %d times", ocr.called.Load())
	}
	// 1 initial download + 1 re-download.
	if storage.called.Load() != 2 {
		t.Errorf("storage should be called 2 times, called %d times", storage.called.Load())
	}
}

func TestProcess_RetryExhausted_ReturnsError(t *testing.T) {
	retryableErr := port.NewOCRError("rate limit", true, errors.New("429"))
	ocr := &mockOCRService{
		recognizeFns: []func(context.Context, io.Reader) (string, error){
			func(_ context.Context, _ io.Reader) (string, error) {
				return "", retryableErr
			},
		},
	}
	storage := &mockTempStorage{
		downloadFns: []func(context.Context, string) (io.ReadCloser, error){
			defaultDownloadFn(""),
		},
	}
	adapter := newTestAdapter(ocr, storage) // maxAttempts=3

	_, _, err := adapter.Process(context.Background(), "key/doc.pdf", false)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	if ocr.called.Load() != 3 {
		t.Errorf("OCR should be called 3 times, called %d times", ocr.called.Load())
	}
	if !strings.Contains(err.Error(), "after 3 attempts") {
		t.Errorf("error should mention '3 attempts', got: %v", err)
	}
	if !port.IsRetryable(err) {
		t.Error("exhausted retry error should be retryable (for upper-layer retry)")
	}
}

func TestProcess_NonRetryableError_NoRetry(t *testing.T) {
	nonRetryableErr := errors.New("invalid PDF format")
	ocr := &mockOCRService{
		recognizeFns: []func(context.Context, io.Reader) (string, error){
			func(_ context.Context, _ io.Reader) (string, error) {
				return "", nonRetryableErr
			},
		},
	}
	storage := &mockTempStorage{
		downloadFns: []func(context.Context, string) (io.ReadCloser, error){
			defaultDownloadFn(""),
		},
	}
	adapter := newTestAdapter(ocr, storage)

	_, _, err := adapter.Process(context.Background(), "key/doc.pdf", false)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	if ocr.called.Load() != 1 {
		t.Errorf("OCR should be called once (no retry), called %d times", ocr.called.Load())
	}
	if port.IsRetryable(err) {
		t.Error("non-retryable error should not become retryable")
	}
}

func TestProcess_RetryReDownloadFails(t *testing.T) {
	retryableErr := port.NewOCRError("temporary", true, errors.New("503"))
	reDownloadErr := errors.New("storage unavailable")

	ocr := &mockOCRService{
		recognizeFns: []func(context.Context, io.Reader) (string, error){
			func(_ context.Context, _ io.Reader) (string, error) {
				return "", retryableErr
			},
		},
	}
	storage := &mockTempStorage{
		downloadFns: []func(context.Context, string) (io.ReadCloser, error){
			// First download succeeds.
			defaultDownloadFn(""),
			// Re-download fails.
			func(_ context.Context, _ string) (io.ReadCloser, error) {
				return nil, reDownloadErr
			},
		},
	}
	adapter := newTestAdapter(ocr, storage)

	_, _, err := adapter.Process(context.Background(), "key/doc.pdf", false)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	if code := port.ErrorCode(err); code != port.ErrCodeStorageFailed {
		t.Errorf("expected error code %q, got %q", port.ErrCodeStorageFailed, code)
	}
	if ocr.called.Load() != 1 {
		t.Errorf("OCR should be called once before re-download fails, called %d times", ocr.called.Load())
	}
}

func TestProcess_ContextCancelledDuringBackoff(t *testing.T) {
	retryableErr := port.NewOCRError("temporary", true, errors.New("503"))

	ocr := &mockOCRService{
		recognizeFns: []func(context.Context, io.Reader) (string, error){
			func(_ context.Context, _ io.Reader) (string, error) {
				return "", retryableErr
			},
		},
	}
	storage := &mockTempStorage{
		downloadFns: []func(context.Context, string) (io.ReadCloser, error){
			defaultDownloadFn(""),
		},
	}
	// Use a longer backoff so we can cancel during it.
	adapter := NewAdapter(ocr, storage, 100, 3, 500*time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())

	// Cancel after a short delay to hit the backoff sleep.
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	_, _, err := adapter.Process(ctx, "key/doc.pdf", false)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

func TestProcess_ReaderClosedOnRetry(t *testing.T) {
	retryableErr := port.NewOCRError("temporary", true, errors.New("503"))
	const ocrText = "Success text that is long enough to not trigger any quality warnings at all"

	var trackers []*trackingReadCloser

	ocr := &mockOCRService{
		recognizeFns: []func(context.Context, io.Reader) (string, error){
			func(_ context.Context, _ io.Reader) (string, error) {
				return "", retryableErr
			},
			func(_ context.Context, _ io.Reader) (string, error) {
				return ocrText, nil
			},
		},
	}

	var mu sync.Mutex
	storage := &mockTempStorage{
		downloadFns: []func(context.Context, string) (io.ReadCloser, error){
			func(_ context.Context, _ string) (io.ReadCloser, error) {
				tracker := &trackingReadCloser{
					ReadCloser: io.NopCloser(strings.NewReader("fake-pdf-bytes")),
				}
				mu.Lock()
				trackers = append(trackers, tracker)
				mu.Unlock()
				return tracker, nil
			},
		},
	}
	adapter := newTestAdapter(ocr, storage)

	result, _, err := adapter.Process(context.Background(), "key/doc.pdf", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.RawText != ocrText {
		t.Errorf("expected raw_text %q, got %q", ocrText, result.RawText)
	}

	mu.Lock()
	defer mu.Unlock()

	if len(trackers) != 2 {
		t.Fatalf("expected 2 readers created, got %d", len(trackers))
	}
	for i, tracker := range trackers {
		if !tracker.wasClosed() {
			t.Errorf("reader %d was not closed", i)
		}
	}
}

// --- Tests: Warnings ---

func TestProcess_WarningPartialRecognition_EmptyText(t *testing.T) {
	ocr := &mockOCRService{
		recognizeFns: []func(context.Context, io.Reader) (string, error){
			func(_ context.Context, _ io.Reader) (string, error) {
				return "", nil
			},
		},
	}
	storage := &mockTempStorage{
		downloadFns: []func(context.Context, string) (io.ReadCloser, error){
			defaultDownloadFn(""),
		},
	}
	adapter := newTestAdapter(ocr, storage)

	result, warnings, err := adapter.Process(context.Background(), "key/doc.pdf", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Status != model.OCRStatusApplicable {
		t.Errorf("expected status %q, got %q", model.OCRStatusApplicable, result.Status)
	}

	if len(warnings) != 1 {
		t.Fatalf("expected 1 warning, got %d", len(warnings))
	}
	if warnings[0].Code != WarnPartialRecognition {
		t.Errorf("expected warning code %q, got %q", WarnPartialRecognition, warnings[0].Code)
	}
	if warnings[0].Stage != model.ProcessingStageOCR {
		t.Errorf("expected stage %q, got %q", model.ProcessingStageOCR, warnings[0].Stage)
	}
}

func TestProcess_WarningPartialRecognition_WhitespaceOnly(t *testing.T) {
	ocr := &mockOCRService{
		recognizeFns: []func(context.Context, io.Reader) (string, error){
			func(_ context.Context, _ io.Reader) (string, error) {
				return "  \n\t ", nil
			},
		},
	}
	storage := &mockTempStorage{
		downloadFns: []func(context.Context, string) (io.ReadCloser, error){
			defaultDownloadFn(""),
		},
	}
	adapter := newTestAdapter(ocr, storage)

	result, warnings, err := adapter.Process(context.Background(), "key/doc.pdf", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Status != model.OCRStatusApplicable {
		t.Errorf("expected status %q, got %q", model.OCRStatusApplicable, result.Status)
	}

	if len(warnings) != 1 {
		t.Fatalf("expected 1 warning, got %d", len(warnings))
	}
	if warnings[0].Code != WarnPartialRecognition {
		t.Errorf("expected warning code %q, got %q", WarnPartialRecognition, warnings[0].Code)
	}
}

func TestProcess_WarningLowQuality_ShortText(t *testing.T) {
	ocr := &mockOCRService{
		recognizeFns: []func(context.Context, io.Reader) (string, error){
			func(_ context.Context, _ io.Reader) (string, error) {
				return "Дог", nil
			},
		},
	}
	storage := &mockTempStorage{
		downloadFns: []func(context.Context, string) (io.ReadCloser, error){
			defaultDownloadFn(""),
		},
	}
	adapter := newTestAdapter(ocr, storage)

	result, warnings, err := adapter.Process(context.Background(), "key/doc.pdf", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Status != model.OCRStatusApplicable {
		t.Errorf("expected status %q, got %q", model.OCRStatusApplicable, result.Status)
	}

	if len(warnings) != 1 {
		t.Fatalf("expected 1 warning, got %d", len(warnings))
	}
	if warnings[0].Code != WarnLowQuality {
		t.Errorf("expected warning code %q, got %q", WarnLowQuality, warnings[0].Code)
	}
	if warnings[0].Stage != model.ProcessingStageOCR {
		t.Errorf("expected stage %q, got %q", model.ProcessingStageOCR, warnings[0].Stage)
	}
}

func TestProcess_NoWarning_NormalText(t *testing.T) {
	// 200+ characters of normal text.
	longText := strings.Repeat("Договор подряда на выполнение работ. ", 10)
	ocr := &mockOCRService{
		recognizeFns: []func(context.Context, io.Reader) (string, error){
			func(_ context.Context, _ io.Reader) (string, error) {
				return longText, nil
			},
		},
	}
	storage := &mockTempStorage{
		downloadFns: []func(context.Context, string) (io.ReadCloser, error){
			defaultDownloadFn(""),
		},
	}
	adapter := newTestAdapter(ocr, storage)

	_, warnings, err := adapter.Process(context.Background(), "key/doc.pdf", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(warnings) != 0 {
		t.Errorf("expected no warnings for normal text, got %v", warnings)
	}
}

func TestProcess_NoWarning_TextPDF(t *testing.T) {
	ocr := &mockOCRService{}
	storage := &mockTempStorage{}
	adapter := newTestAdapter(ocr, storage)

	_, warnings, err := adapter.Process(context.Background(), "key/doc.pdf", true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(warnings) != 0 {
		t.Errorf("expected no warnings for text PDF, got %v", warnings)
	}
}

func TestProcess_EmptyText_NotBothWarnings(t *testing.T) {
	ocr := &mockOCRService{
		recognizeFns: []func(context.Context, io.Reader) (string, error){
			func(_ context.Context, _ io.Reader) (string, error) {
				return "", nil
			},
		},
	}
	storage := &mockTempStorage{
		downloadFns: []func(context.Context, string) (io.ReadCloser, error){
			defaultDownloadFn(""),
		},
	}
	adapter := newTestAdapter(ocr, storage)

	_, warnings, err := adapter.Process(context.Background(), "key/doc.pdf", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(warnings) != 1 {
		t.Fatalf("expected exactly 1 warning (partial, not both), got %d: %v", len(warnings), warnings)
	}
	if warnings[0].Code != WarnPartialRecognition {
		t.Errorf("expected %q, got %q", WarnPartialRecognition, warnings[0].Code)
	}
}

// --- Tests: Edge Cases ---

func TestProcess_MaxAttemptsOne_NoRetry(t *testing.T) {
	retryableErr := port.NewOCRError("temporary", true, errors.New("503"))
	ocr := &mockOCRService{
		recognizeFns: []func(context.Context, io.Reader) (string, error){
			func(_ context.Context, _ io.Reader) (string, error) {
				return "", retryableErr
			},
		},
	}
	storage := &mockTempStorage{
		downloadFns: []func(context.Context, string) (io.ReadCloser, error){
			defaultDownloadFn(""),
		},
	}
	adapter := newTestAdapterNoRetry(ocr, storage) // maxAttempts=1

	_, _, err := adapter.Process(context.Background(), "key/doc.pdf", false)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	if ocr.called.Load() != 1 {
		t.Errorf("OCR should be called once with maxAttempts=1, called %d times", ocr.called.Load())
	}
	if !strings.Contains(err.Error(), "after 1 attempts") {
		t.Errorf("error should mention '1 attempts', got: %v", err)
	}
}

func TestProcess_RetryWithWarning(t *testing.T) {
	retryableErr := port.NewOCRError("temporary", true, errors.New("503"))
	shortText := "Дог"

	ocr := &mockOCRService{
		recognizeFns: []func(context.Context, io.Reader) (string, error){
			func(_ context.Context, _ io.Reader) (string, error) {
				return "", retryableErr
			},
			func(_ context.Context, _ io.Reader) (string, error) {
				return shortText, nil
			},
		},
	}
	storage := &mockTempStorage{
		downloadFns: []func(context.Context, string) (io.ReadCloser, error){
			defaultDownloadFn(""),
		},
	}
	adapter := newTestAdapter(ocr, storage)

	result, warnings, err := adapter.Process(context.Background(), "key/doc.pdf", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.RawText != shortText {
		t.Errorf("expected raw_text %q, got %q", shortText, result.RawText)
	}
	if ocr.called.Load() != 2 {
		t.Errorf("OCR should be called 2 times, called %d times", ocr.called.Load())
	}

	if len(warnings) != 1 {
		t.Fatalf("expected 1 warning, got %d", len(warnings))
	}
	if warnings[0].Code != WarnLowQuality {
		t.Errorf("expected warning code %q, got %q", WarnLowQuality, warnings[0].Code)
	}
}

func TestNewAdapter_DefaultsForInvalidParams(t *testing.T) {
	ocr := &mockOCRService{}
	storage := &mockTempStorage{}

	adapter := NewAdapter(ocr, storage, 0, 0, 1*time.Millisecond)

	if adapter.rpsLimit != 10 {
		t.Errorf("expected default rpsLimit 10, got %d", adapter.rpsLimit)
	}
	if adapter.maxAttempts != 1 {
		t.Errorf("expected default maxAttempts 1, got %d", adapter.maxAttempts)
	}
	if adapter.capacity != 10.0 {
		t.Errorf("expected capacity 10.0, got %f", adapter.capacity)
	}
	if adapter.tokens != 10.0 {
		t.Errorf("expected tokens 10.0, got %f", adapter.tokens)
	}

	// Also test negative values.
	adapter2 := NewAdapter(ocr, storage, -5, -3, 1*time.Millisecond)
	if adapter2.rpsLimit != 10 {
		t.Errorf("expected default rpsLimit 10 for negative input, got %d", adapter2.rpsLimit)
	}
	if adapter2.maxAttempts != 1 {
		t.Errorf("expected default maxAttempts 1 for negative input, got %d", adapter2.maxAttempts)
	}
}
