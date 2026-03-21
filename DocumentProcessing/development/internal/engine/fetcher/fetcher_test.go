package fetcher

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"sync/atomic"
	"testing"

	"contractpro/document-processing/internal/domain/model"
	"contractpro/document-processing/internal/domain/port"
)

const testMaxFileSize = int64(1024) // 1 KB for tests

// --- Inline mocks ---

// mockDownloader implements port.SourceFileDownloaderPort with function fields.
type mockDownloader struct {
	downloadFn func(ctx context.Context, fileURL string) (io.ReadCloser, int64, error)
}

func (m *mockDownloader) Download(ctx context.Context, fileURL string) (io.ReadCloser, int64, error) {
	return m.downloadFn(ctx, fileURL)
}

// mockStorage implements port.TempStoragePort with function fields.
type mockStorage struct {
	uploadFn         func(ctx context.Context, key string, data io.Reader) error
	downloadFn       func(ctx context.Context, key string) (io.ReadCloser, error)
	deleteFn         func(ctx context.Context, key string) error
	deleteByPrefixFn func(ctx context.Context, prefix string) error
}

func (m *mockStorage) Upload(ctx context.Context, key string, data io.Reader) error {
	if m.uploadFn != nil {
		return m.uploadFn(ctx, key, data)
	}
	// Default: drain the reader (simulates real upload reading all bytes).
	_, _ = io.Copy(io.Discard, data)
	return nil
}

func (m *mockStorage) Download(ctx context.Context, key string) (io.ReadCloser, error) {
	if m.downloadFn != nil {
		return m.downloadFn(ctx, key)
	}
	return nil, errors.New("not implemented")
}

func (m *mockStorage) Delete(ctx context.Context, key string) error {
	if m.deleteFn != nil {
		return m.deleteFn(ctx, key)
	}
	return nil
}

func (m *mockStorage) DeleteByPrefix(ctx context.Context, prefix string) error {
	if m.deleteByPrefixFn != nil {
		return m.deleteByPrefixFn(ctx, prefix)
	}
	return nil
}

// trackingCloser wraps an io.Reader and tracks whether Close was called.
type trackingCloser struct {
	io.Reader
	closed atomic.Bool
}

func (tc *trackingCloser) Close() error {
	tc.closed.Store(true)
	return nil
}

// --- Helpers ---

func testCommand() model.ProcessDocumentCommand {
	return model.ProcessDocumentCommand{
		JobID:      "job-123",
		DocumentID: "doc-456",
		FileURL:    "https://storage.example.com/file.pdf",
	}
}

func newTrackingCloser(data []byte) *trackingCloser {
	return &trackingCloser{Reader: bytes.NewReader(data)}
}

// --- Tests ---

func TestFetch(t *testing.T) {
	t.Run("success with known Content-Length", func(t *testing.T) {
		data := []byte("PDF content here")
		tc := newTrackingCloser(data)

		dl := &mockDownloader{
			downloadFn: func(_ context.Context, _ string) (io.ReadCloser, int64, error) {
				return tc, int64(len(data)), nil
			},
		}
		st := &mockStorage{}
		f := NewFetcher(dl, st, testMaxFileSize)

		result, err := f.Fetch(context.Background(), testCommand())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.StorageKey != "job-123/source.pdf" {
			t.Errorf("expected storage key %q, got %q", "job-123/source.pdf", result.StorageKey)
		}
		if result.FileSize != int64(len(data)) {
			t.Errorf("expected file size %d, got %d", len(data), result.FileSize)
		}
		if result.PageCount != 0 {
			t.Errorf("expected page count 0, got %d", result.PageCount)
		}
		if result.IsTextPDF {
			t.Error("expected IsTextPDF false")
		}
		if !tc.closed.Load() {
			t.Error("expected body to be closed")
		}
	})

	t.Run("success with Content-Length unknown", func(t *testing.T) {
		data := []byte("small data")
		tc := newTrackingCloser(data)

		dl := &mockDownloader{
			downloadFn: func(_ context.Context, _ string) (io.ReadCloser, int64, error) {
				return tc, -1, nil
			},
		}
		st := &mockStorage{}
		f := NewFetcher(dl, st, testMaxFileSize)

		result, err := f.Fetch(context.Background(), testCommand())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.FileSize != int64(len(data)) {
			t.Errorf("expected file size %d, got %d", len(data), result.FileSize)
		}
		if result.StorageKey != "job-123/source.pdf" {
			t.Errorf("expected storage key %q, got %q", "job-123/source.pdf", result.StorageKey)
		}
	})

	t.Run("Content-Length exceeds limit early reject", func(t *testing.T) {
		bodyRead := false
		tc := &trackingCloser{
			Reader: readerFunc(func(p []byte) (int, error) {
				bodyRead = true
				return 0, io.EOF
			}),
		}

		dl := &mockDownloader{
			downloadFn: func(_ context.Context, _ string) (io.ReadCloser, int64, error) {
				return tc, testMaxFileSize + 1, nil
			},
		}
		st := &mockStorage{}
		f := NewFetcher(dl, st, testMaxFileSize)

		_, err := f.Fetch(context.Background(), testCommand())
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if port.ErrorCode(err) != port.ErrCodeFileTooLarge {
			t.Errorf("expected error code %q, got %q", port.ErrCodeFileTooLarge, port.ErrorCode(err))
		}
		if bodyRead {
			t.Error("expected body not to be read on early reject")
		}
		if !tc.closed.Load() {
			t.Error("expected body to be closed even on early reject")
		}
	})

	t.Run("streaming size exceeds limit", func(t *testing.T) {
		// Content-Length unknown (-1), actual data exceeds limit.
		data := make([]byte, testMaxFileSize+100)
		tc := newTrackingCloser(data)

		var deletedKey string
		dl := &mockDownloader{
			downloadFn: func(_ context.Context, _ string) (io.ReadCloser, int64, error) {
				return tc, -1, nil
			},
		}
		st := &mockStorage{
			deleteFn: func(_ context.Context, key string) error {
				deletedKey = key
				return nil
			},
		}
		f := NewFetcher(dl, st, testMaxFileSize)

		_, err := f.Fetch(context.Background(), testCommand())
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if port.ErrorCode(err) != port.ErrCodeFileTooLarge {
			t.Errorf("expected error code %q, got %q", port.ErrCodeFileTooLarge, port.ErrorCode(err))
		}
		if deletedKey != "job-123/source.pdf" {
			t.Errorf("expected cleanup delete for key %q, got %q", "job-123/source.pdf", deletedKey)
		}
	})

	t.Run("download returns DomainError passthrough", func(t *testing.T) {
		dl := &mockDownloader{
			downloadFn: func(_ context.Context, _ string) (io.ReadCloser, int64, error) {
				return nil, 0, port.NewFileNotFoundError("file not found", nil)
			},
		}
		st := &mockStorage{}
		f := NewFetcher(dl, st, testMaxFileSize)

		_, err := f.Fetch(context.Background(), testCommand())
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if port.ErrorCode(err) != port.ErrCodeFileNotFound {
			t.Errorf("expected error code %q, got %q", port.ErrCodeFileNotFound, port.ErrorCode(err))
		}
	})

	t.Run("download returns unknown error becomes SERVICE_UNAVAILABLE", func(t *testing.T) {
		dl := &mockDownloader{
			downloadFn: func(_ context.Context, _ string) (io.ReadCloser, int64, error) {
				return nil, 0, errors.New("some network failure")
			},
		}
		st := &mockStorage{}
		f := NewFetcher(dl, st, testMaxFileSize)

		_, err := f.Fetch(context.Background(), testCommand())
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if port.ErrorCode(err) != port.ErrCodeServiceUnavailable {
			t.Errorf("expected error code %q, got %q", port.ErrCodeServiceUnavailable, port.ErrorCode(err))
		}
		if !port.IsRetryable(err) {
			t.Error("expected error to be retryable")
		}
	})

	t.Run("context already canceled", func(t *testing.T) {
		dl := &mockDownloader{
			downloadFn: func(_ context.Context, _ string) (io.ReadCloser, int64, error) {
				t.Fatal("downloader should not be called when context is canceled")
				return nil, 0, nil
			},
		}
		st := &mockStorage{}
		f := NewFetcher(dl, st, testMaxFileSize)

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := f.Fetch(ctx, testCommand())
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !errors.Is(err, context.Canceled) {
			t.Errorf("expected context.Canceled, got %v", err)
		}
	})

	t.Run("download returns context.DeadlineExceeded passthrough", func(t *testing.T) {
		dl := &mockDownloader{
			downloadFn: func(_ context.Context, _ string) (io.ReadCloser, int64, error) {
				return nil, 0, context.DeadlineExceeded
			},
		}
		st := &mockStorage{}
		f := NewFetcher(dl, st, testMaxFileSize)

		_, err := f.Fetch(context.Background(), testCommand())
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("expected context.DeadlineExceeded, got %v", err)
		}
	})

	t.Run("storage upload error becomes STORAGE_FAILED", func(t *testing.T) {
		data := []byte("some pdf data")
		tc := newTrackingCloser(data)

		dl := &mockDownloader{
			downloadFn: func(_ context.Context, _ string) (io.ReadCloser, int64, error) {
				return tc, int64(len(data)), nil
			},
		}
		st := &mockStorage{
			uploadFn: func(_ context.Context, _ string, data io.Reader) error {
				// Drain the reader to simulate partial read before failure.
				_, _ = io.Copy(io.Discard, data)
				return errors.New("S3 write failed")
			},
		}
		f := NewFetcher(dl, st, testMaxFileSize)

		_, err := f.Fetch(context.Background(), testCommand())
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if port.ErrorCode(err) != port.ErrCodeStorageFailed {
			t.Errorf("expected error code %q, got %q", port.ErrCodeStorageFailed, port.ErrorCode(err))
		}
		if !port.IsRetryable(err) {
			t.Error("expected storage error to be retryable")
		}
	})

	t.Run("storage key format", func(t *testing.T) {
		data := []byte("test")
		tc := newTrackingCloser(data)

		var uploadedKey string
		dl := &mockDownloader{
			downloadFn: func(_ context.Context, _ string) (io.ReadCloser, int64, error) {
				return tc, int64(len(data)), nil
			},
		}
		st := &mockStorage{
			uploadFn: func(_ context.Context, key string, data io.Reader) error {
				uploadedKey = key
				_, _ = io.Copy(io.Discard, data)
				return nil
			},
		}
		f := NewFetcher(dl, st, testMaxFileSize)

		cmd := testCommand()
		cmd.JobID = "my-job-id"

		result, err := f.Fetch(context.Background(), cmd)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		expectedKey := "my-job-id/source.pdf"
		if uploadedKey != expectedKey {
			t.Errorf("expected upload key %q, got %q", expectedKey, uploadedKey)
		}
		if result.StorageKey != expectedKey {
			t.Errorf("expected result storage key %q, got %q", expectedKey, result.StorageKey)
		}
	})

	t.Run("body always closed on success", func(t *testing.T) {
		data := []byte("some data")
		tc := newTrackingCloser(data)

		dl := &mockDownloader{
			downloadFn: func(_ context.Context, _ string) (io.ReadCloser, int64, error) {
				return tc, int64(len(data)), nil
			},
		}
		st := &mockStorage{}
		f := NewFetcher(dl, st, testMaxFileSize)

		_, err := f.Fetch(context.Background(), testCommand())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !tc.closed.Load() {
			t.Error("expected body to be closed after successful fetch")
		}
	})

	t.Run("file exactly at size limit succeeds", func(t *testing.T) {
		data := make([]byte, testMaxFileSize)
		tc := newTrackingCloser(data)

		dl := &mockDownloader{
			downloadFn: func(_ context.Context, _ string) (io.ReadCloser, int64, error) {
				return tc, int64(len(data)), nil
			},
		}
		st := &mockStorage{}
		f := NewFetcher(dl, st, testMaxFileSize)

		result, err := f.Fetch(context.Background(), testCommand())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.FileSize != testMaxFileSize {
			t.Errorf("expected file size %d, got %d", testMaxFileSize, result.FileSize)
		}
	})

	t.Run("Content-Length lies small but body exceeds limit", func(t *testing.T) {
		// Content-Length claims 100 bytes, but actual body exceeds limit.
		data := make([]byte, testMaxFileSize+500)
		tc := newTrackingCloser(data)

		var deletedKey string
		dl := &mockDownloader{
			downloadFn: func(_ context.Context, _ string) (io.ReadCloser, int64, error) {
				return tc, 100, nil // lies about size
			},
		}
		st := &mockStorage{
			deleteFn: func(_ context.Context, key string) error {
				deletedKey = key
				return nil
			},
		}
		f := NewFetcher(dl, st, testMaxFileSize)

		_, err := f.Fetch(context.Background(), testCommand())
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if port.ErrorCode(err) != port.ErrCodeFileTooLarge {
			t.Errorf("expected error code %q, got %q", port.ErrCodeFileTooLarge, port.ErrorCode(err))
		}
		if deletedKey != "job-123/source.pdf" {
			t.Errorf("expected cleanup delete, got key %q", deletedKey)
		}
	})

	t.Run("zero-byte file success", func(t *testing.T) {
		tc := newTrackingCloser([]byte{})

		dl := &mockDownloader{
			downloadFn: func(_ context.Context, _ string) (io.ReadCloser, int64, error) {
				return tc, 0, nil
			},
		}
		st := &mockStorage{}
		f := NewFetcher(dl, st, testMaxFileSize)

		result, err := f.Fetch(context.Background(), testCommand())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.FileSize != 0 {
			t.Errorf("expected file size 0, got %d", result.FileSize)
		}
		if result.StorageKey != "job-123/source.pdf" {
			t.Errorf("expected storage key %q, got %q", "job-123/source.pdf", result.StorageKey)
		}
	})
}

func TestClassifyDownloadError(t *testing.T) {
	tests := []struct {
		name        string
		err         error
		wantCode    string
		wantRaw     error // if non-nil, errors.Is must match
	}{
		{
			name:     "context.Canceled passes through",
			err:      context.Canceled,
			wantRaw:  context.Canceled,
		},
		{
			name:     "context.DeadlineExceeded passes through",
			err:      context.DeadlineExceeded,
			wantRaw:  context.DeadlineExceeded,
		},
		{
			name:     "DomainError passes through",
			err:      port.NewFileNotFoundError("gone", nil),
			wantCode: port.ErrCodeFileNotFound,
		},
		{
			name:     "unknown error becomes SERVICE_UNAVAILABLE",
			err:      errors.New("unknown"),
			wantCode: port.ErrCodeServiceUnavailable,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classifyDownloadError(tt.err)
			if tt.wantRaw != nil {
				if !errors.Is(got, tt.wantRaw) {
					t.Errorf("expected errors.Is(%v, %v) to be true", got, tt.wantRaw)
				}
				return
			}
			if tt.wantCode != "" {
				if port.ErrorCode(got) != tt.wantCode {
					t.Errorf("expected error code %q, got %q", tt.wantCode, port.ErrorCode(got))
				}
			}
		})
	}
}

func TestLimitedReader(t *testing.T) {
	t.Run("within limit", func(t *testing.T) {
		lr := &limitedReader{
			r:     strings.NewReader("hello"),
			limit: 100,
		}
		data, err := io.ReadAll(lr)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if string(data) != "hello" {
			t.Errorf("expected %q, got %q", "hello", string(data))
		}
		if lr.exceeded {
			t.Error("expected exceeded to be false")
		}
		if lr.bytesRead != 5 {
			t.Errorf("expected bytesRead 5, got %d", lr.bytesRead)
		}
	})

	t.Run("exactly at limit", func(t *testing.T) {
		lr := &limitedReader{
			r:     strings.NewReader("hello"),
			limit: 5,
		}
		data, err := io.ReadAll(lr)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if string(data) != "hello" {
			t.Errorf("expected %q, got %q", "hello", string(data))
		}
		if lr.exceeded {
			t.Error("expected exceeded to be false for file exactly at limit")
		}
		if lr.bytesRead != 5 {
			t.Errorf("expected bytesRead 5, got %d", lr.bytesRead)
		}
	})

	t.Run("exceeds limit", func(t *testing.T) {
		lr := &limitedReader{
			r:     strings.NewReader("hello world"),
			limit: 5,
		}
		_, err := io.ReadAll(lr)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !lr.exceeded {
			t.Error("expected exceeded to be true")
		}
	})
}

// readerFunc adapts a function into an io.Reader.
type readerFunc func(p []byte) (int, error)

func (f readerFunc) Read(p []byte) (int, error) { return f(p) }
