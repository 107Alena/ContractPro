package storage

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"contractpro/document-processing/internal/domain/port"
)

// --- Mock ---

// mockStorage captures arguments and returns configurable results.
type mockStorage struct {
	uploadFn         func(ctx context.Context, key string, data io.Reader) error
	downloadFn       func(ctx context.Context, key string) (io.ReadCloser, error)
	deleteFn         func(ctx context.Context, key string) error
	deleteByPrefixFn func(ctx context.Context, prefix string) error
}

// Compile-time check: mockStorage satisfies StorageClient.
var _ StorageClient = (*mockStorage)(nil)

func (m *mockStorage) Upload(ctx context.Context, key string, data io.Reader) error {
	if m.uploadFn != nil {
		return m.uploadFn(ctx, key, data)
	}
	return nil
}

func (m *mockStorage) Download(ctx context.Context, key string) (io.ReadCloser, error) {
	if m.downloadFn != nil {
		return m.downloadFn(ctx, key)
	}
	return io.NopCloser(strings.NewReader("")), nil
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

// --- Tests ---

// 1. Interface Compliance

func TestInterfaceCompliance(t *testing.T) {
	adapter := NewAdapter(&mockStorage{}, "")

	var iface port.TempStoragePort = adapter
	if iface == nil {
		t.Fatal("Adapter does not satisfy TempStoragePort")
	}
}

// 2. Constructor — nil client panics

func TestNewAdapter_NilClientPanics(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic for nil client, got none")
		}
	}()
	NewAdapter(nil, "prefix/")
}

// 3. Upload — success with key prefixing

func TestUpload_Success(t *testing.T) {
	var capturedKey string
	var capturedData []byte
	mock := &mockStorage{
		uploadFn: func(_ context.Context, key string, data io.Reader) error {
			capturedKey = key
			b, _ := io.ReadAll(data)
			capturedData = b
			return nil
		},
	}

	adapter := NewAdapter(mock, "pfx/")
	body := strings.NewReader("file-content")

	err := adapter.Upload(context.Background(), "abc/source.pdf", body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedKey != "pfx/abc/source.pdf" {
		t.Errorf("key = %q, want %q", capturedKey, "pfx/abc/source.pdf")
	}
	if string(capturedData) != "file-content" {
		t.Errorf("data = %q, want %q", string(capturedData), "file-content")
	}
}

// 4. Upload — key prefixing

func TestUpload_KeyPrefixing(t *testing.T) {
	var capturedKey string
	mock := &mockStorage{
		uploadFn: func(_ context.Context, key string, _ io.Reader) error {
			capturedKey = key
			return nil
		},
	}

	adapter := NewAdapter(mock, "jobs/")
	err := adapter.Upload(context.Background(), "abc/source.pdf", strings.NewReader(""))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedKey != "jobs/abc/source.pdf" {
		t.Errorf("key = %q, want %q", capturedKey, "jobs/abc/source.pdf")
	}
}

// 5. Upload — empty key prefix passes key unchanged

func TestUpload_EmptyKeyPrefix(t *testing.T) {
	var capturedKey string
	mock := &mockStorage{
		uploadFn: func(_ context.Context, key string, _ io.Reader) error {
			capturedKey = key
			return nil
		},
	}

	adapter := NewAdapter(mock, "")
	err := adapter.Upload(context.Background(), "abc/source.pdf", strings.NewReader(""))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedKey != "abc/source.pdf" {
		t.Errorf("key = %q, want %q", capturedKey, "abc/source.pdf")
	}
}

// 6. Upload — empty key returns non-retryable DomainError

func TestUpload_EmptyKey(t *testing.T) {
	adapter := NewAdapter(&mockStorage{}, "pfx/")

	err := adapter.Upload(context.Background(), "", strings.NewReader("data"))
	if err == nil {
		t.Fatal("expected error for empty key, got nil")
	}
	if !port.IsDomainError(err) {
		t.Errorf("expected DomainError, got %T", err)
	}
	if port.ErrorCode(err) != port.ErrCodeStorageFailed {
		t.Errorf("ErrorCode = %q, want %q", port.ErrorCode(err), port.ErrCodeStorageFailed)
	}
	if port.IsRetryable(err) {
		t.Error("empty key errors should NOT be retryable")
	}
}

// 7. Upload — client error passes through

func TestUpload_ClientError(t *testing.T) {
	clientErr := port.NewStorageError("connection failed", errors.New("tcp reset"))
	mock := &mockStorage{
		uploadFn: func(_ context.Context, _ string, _ io.Reader) error {
			return clientErr
		},
	}

	adapter := NewAdapter(mock, "")
	err := adapter.Upload(context.Background(), "key", strings.NewReader("data"))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, clientErr) {
		t.Errorf("error = %v, want errors.Is to match client error", err)
	}
}

// 8. Upload — context.Canceled passes through

func TestUpload_ContextCanceled(t *testing.T) {
	mock := &mockStorage{
		uploadFn: func(_ context.Context, _ string, _ io.Reader) error {
			return context.Canceled
		},
	}

	adapter := NewAdapter(mock, "")
	err := adapter.Upload(context.Background(), "key", strings.NewReader("data"))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("error = %v, want errors.Is(context.Canceled)", err)
	}
}

// 9. Download — success with key prefixed

func TestDownload_Success(t *testing.T) {
	var capturedKey string
	mock := &mockStorage{
		downloadFn: func(_ context.Context, key string) (io.ReadCloser, error) {
			capturedKey = key
			return io.NopCloser(strings.NewReader("pdf-bytes")), nil
		},
	}

	adapter := NewAdapter(mock, "pfx/")
	rc, err := adapter.Download(context.Background(), "abc/source.pdf")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer rc.Close()

	if capturedKey != "pfx/abc/source.pdf" {
		t.Errorf("key = %q, want %q", capturedKey, "pfx/abc/source.pdf")
	}

	data, _ := io.ReadAll(rc)
	if string(data) != "pdf-bytes" {
		t.Errorf("data = %q, want %q", string(data), "pdf-bytes")
	}
}

// 10. Download — empty key returns non-retryable DomainError

func TestDownload_EmptyKey(t *testing.T) {
	adapter := NewAdapter(&mockStorage{}, "pfx/")

	rc, err := adapter.Download(context.Background(), "")
	if err == nil {
		t.Fatal("expected error for empty key, got nil")
	}
	if rc != nil {
		t.Error("expected nil ReadCloser on error")
	}
	if !port.IsDomainError(err) {
		t.Errorf("expected DomainError, got %T", err)
	}
	if port.ErrorCode(err) != port.ErrCodeStorageFailed {
		t.Errorf("ErrorCode = %q, want %q", port.ErrorCode(err), port.ErrCodeStorageFailed)
	}
	if port.IsRetryable(err) {
		t.Error("empty key errors should NOT be retryable")
	}
}

// 11. Download — client error passes through

func TestDownload_ClientError(t *testing.T) {
	clientErr := port.NewStorageError("not found", errors.New("NoSuchKey"))
	mock := &mockStorage{
		downloadFn: func(_ context.Context, _ string) (io.ReadCloser, error) {
			return nil, clientErr
		},
	}

	adapter := NewAdapter(mock, "")
	_, err := adapter.Download(context.Background(), "key")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, clientErr) {
		t.Errorf("error = %v, want errors.Is to match client error", err)
	}
}

// 12. Delete — success with prefixed key

func TestDelete_Success(t *testing.T) {
	var capturedKey string
	mock := &mockStorage{
		deleteFn: func(_ context.Context, key string) error {
			capturedKey = key
			return nil
		},
	}

	adapter := NewAdapter(mock, "pfx/")
	err := adapter.Delete(context.Background(), "abc/source.pdf")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedKey != "pfx/abc/source.pdf" {
		t.Errorf("key = %q, want %q", capturedKey, "pfx/abc/source.pdf")
	}
}

// 13. Delete — empty key returns non-retryable DomainError

func TestDelete_EmptyKey(t *testing.T) {
	adapter := NewAdapter(&mockStorage{}, "pfx/")

	err := adapter.Delete(context.Background(), "")
	if err == nil {
		t.Fatal("expected error for empty key, got nil")
	}
	if !port.IsDomainError(err) {
		t.Errorf("expected DomainError, got %T", err)
	}
	if port.ErrorCode(err) != port.ErrCodeStorageFailed {
		t.Errorf("ErrorCode = %q, want %q", port.ErrorCode(err), port.ErrCodeStorageFailed)
	}
	if port.IsRetryable(err) {
		t.Error("empty key errors should NOT be retryable")
	}
}

// 14. Delete — client error passes through

func TestDelete_ClientError(t *testing.T) {
	clientErr := port.NewStorageError("bucket error", errors.New("access denied"))
	mock := &mockStorage{
		deleteFn: func(_ context.Context, _ string) error {
			return clientErr
		},
	}

	adapter := NewAdapter(mock, "")
	err := adapter.Delete(context.Background(), "key")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, clientErr) {
		t.Errorf("error = %v, want errors.Is to match client error", err)
	}
}

// 15. DeleteByPrefix — success with prefixed prefix

func TestDeleteByPrefix_Success(t *testing.T) {
	var capturedPrefix string
	mock := &mockStorage{
		deleteByPrefixFn: func(_ context.Context, prefix string) error {
			capturedPrefix = prefix
			return nil
		},
	}

	adapter := NewAdapter(mock, "pfx/")
	err := adapter.DeleteByPrefix(context.Background(), "job-123/")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedPrefix != "pfx/job-123/" {
		t.Errorf("prefix = %q, want %q", capturedPrefix, "pfx/job-123/")
	}
}

// 16. DeleteByPrefix — empty prefix returns non-retryable DomainError

func TestDeleteByPrefix_EmptyPrefix(t *testing.T) {
	adapter := NewAdapter(&mockStorage{}, "pfx/")

	err := adapter.DeleteByPrefix(context.Background(), "")
	if err == nil {
		t.Fatal("expected error for empty prefix, got nil")
	}
	if !port.IsDomainError(err) {
		t.Errorf("expected DomainError, got %T", err)
	}
	if port.ErrorCode(err) != port.ErrCodeStorageFailed {
		t.Errorf("ErrorCode = %q, want %q", port.ErrorCode(err), port.ErrCodeStorageFailed)
	}
	if port.IsRetryable(err) {
		t.Error("empty prefix errors should NOT be retryable")
	}
}

// 17. DeleteByPrefix — client error passes through

func TestDeleteByPrefix_ClientError(t *testing.T) {
	clientErr := port.NewStorageError("list failed", errors.New("network error"))
	mock := &mockStorage{
		deleteByPrefixFn: func(_ context.Context, _ string) error {
			return clientErr
		},
	}

	adapter := NewAdapter(mock, "")
	err := adapter.DeleteByPrefix(context.Background(), "prefix/")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, clientErr) {
		t.Errorf("error = %v, want errors.Is to match client error", err)
	}
}

// 18. Context forwarding — context with custom value forwarded to client

func TestContextForwarding(t *testing.T) {
	type ctxKey struct{}
	ctx := context.WithValue(context.Background(), ctxKey{}, "test-val")

	var capturedCtx context.Context
	mock := &mockStorage{
		uploadFn: func(c context.Context, _ string, _ io.Reader) error {
			capturedCtx = c
			return nil
		},
	}

	adapter := NewAdapter(mock, "")
	_ = adapter.Upload(ctx, "key", strings.NewReader("data"))

	if capturedCtx == nil {
		t.Fatal("context was not captured")
	}
	if capturedCtx.Value(ctxKey{}) != "test-val" {
		t.Error("context was not forwarded to storage client")
	}
}

// 19. Upload — io.Reader body passed unchanged, verify content

func TestUpload_DataPassthrough(t *testing.T) {
	content := "binary\x00data\xFFwith\x01special\x02bytes"
	var capturedData []byte
	mock := &mockStorage{
		uploadFn: func(_ context.Context, _ string, data io.Reader) error {
			b, err := io.ReadAll(data)
			if err != nil {
				return err
			}
			capturedData = b
			return nil
		},
	}

	adapter := NewAdapter(mock, "")
	err := adapter.Upload(context.Background(), "key", bytes.NewReader([]byte(content)))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !bytes.Equal(capturedData, []byte(content)) {
		t.Errorf("data mismatch: got %v, want %v", capturedData, []byte(content))
	}
}
