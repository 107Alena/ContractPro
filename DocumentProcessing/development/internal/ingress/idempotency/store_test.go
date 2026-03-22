package idempotency

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"contractpro/document-processing/internal/domain/port"
	"contractpro/document-processing/internal/infra/kvstore"
)

// --- mock ---

type mockKVStore struct {
	getFn   func(ctx context.Context, key string) (string, error)
	setFn   func(ctx context.Context, key string, value string, ttl time.Duration) error
	setNXFn func(ctx context.Context, key string, value string, ttl time.Duration) (bool, error)
}

func (m *mockKVStore) Get(ctx context.Context, key string) (string, error) {
	if m.getFn != nil {
		return m.getFn(ctx, key)
	}
	return "", kvstore.ErrKeyNotFound
}

func (m *mockKVStore) Set(ctx context.Context, key string, value string, ttl time.Duration) error {
	if m.setFn != nil {
		return m.setFn(ctx, key, value, ttl)
	}
	return nil
}

func (m *mockKVStore) SetNX(ctx context.Context, key string, value string, ttl time.Duration) (bool, error) {
	if m.setNXFn != nil {
		return m.setNXFn(ctx, key, value, ttl)
	}
	return true, nil
}

// --- interface compliance ---

var _ KVStoreAPI = (*mockKVStore)(nil)

// --- Constructor tests ---

func TestNewStore_NilKV(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for nil kv")
		}
	}()
	NewStore(nil, 24*time.Hour)
}

func TestNewStore_ZeroTTL(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for zero TTL")
		}
	}()
	NewStore(&mockKVStore{}, 0)
}

func TestNewStore_NegativeTTL(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for negative TTL")
		}
	}()
	NewStore(&mockKVStore{}, -1*time.Second)
}

// --- Check tests ---

func TestCheck_NewJob(t *testing.T) {
	mock := &mockKVStore{
		getFn: func(_ context.Context, _ string) (string, error) {
			return "", kvstore.ErrKeyNotFound
		},
	}

	s := NewStore(mock, 24*time.Hour)
	status, err := s.Check(context.Background(), "job-001")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status != port.IdempotencyStatusNew {
		t.Errorf("status = %q, want %q", status, port.IdempotencyStatusNew)
	}
}

func TestCheck_InProgressJob(t *testing.T) {
	mock := &mockKVStore{
		getFn: func(_ context.Context, _ string) (string, error) {
			return "in_progress", nil
		},
	}

	s := NewStore(mock, 24*time.Hour)
	status, err := s.Check(context.Background(), "job-001")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status != port.IdempotencyStatusInProgress {
		t.Errorf("status = %q, want %q", status, port.IdempotencyStatusInProgress)
	}
}

func TestCheck_CompletedJob(t *testing.T) {
	mock := &mockKVStore{
		getFn: func(_ context.Context, _ string) (string, error) {
			return "completed", nil
		},
	}

	s := NewStore(mock, 24*time.Hour)
	status, err := s.Check(context.Background(), "job-001")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status != port.IdempotencyStatusCompleted {
		t.Errorf("status = %q, want %q", status, port.IdempotencyStatusCompleted)
	}
}

func TestCheck_StorageError(t *testing.T) {
	storageErr := port.NewStorageError("kvstore: Get", errors.New("connection refused"))
	mock := &mockKVStore{
		getFn: func(_ context.Context, _ string) (string, error) {
			return "", storageErr
		},
	}

	s := NewStore(mock, 24*time.Hour)
	_, err := s.Check(context.Background(), "job-001")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if port.ErrorCode(err) != port.ErrCodeStorageFailed {
		t.Errorf("error code = %q, want %q", port.ErrorCode(err), port.ErrCodeStorageFailed)
	}
	if !port.IsRetryable(err) {
		t.Error("expected retryable error")
	}
}

func TestCheck_ContextCanceled(t *testing.T) {
	mock := &mockKVStore{
		getFn: func(_ context.Context, _ string) (string, error) {
			return "", context.Canceled
		},
	}

	s := NewStore(mock, 24*time.Hour)
	_, err := s.Check(context.Background(), "job-001")
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got: %v", err)
	}
	if port.IsDomainError(err) {
		t.Error("context.Canceled should not be a DomainError")
	}
}

func TestCheck_ContextDeadlineExceeded(t *testing.T) {
	mock := &mockKVStore{
		getFn: func(_ context.Context, _ string) (string, error) {
			return "", context.DeadlineExceeded
		},
	}

	s := NewStore(mock, 24*time.Hour)
	_, err := s.Check(context.Background(), "job-001")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("expected context.DeadlineExceeded, got: %v", err)
	}
	if port.IsDomainError(err) {
		t.Error("context.DeadlineExceeded should not be a DomainError")
	}
}

func TestCheck_InvalidStoredValue(t *testing.T) {
	mock := &mockKVStore{
		getFn: func(_ context.Context, _ string) (string, error) {
			return "garbage_value", nil
		},
	}

	s := NewStore(mock, 24*time.Hour)
	_, err := s.Check(context.Background(), "job-001")
	if err == nil {
		t.Fatal("expected error for invalid stored value")
	}
	if port.ErrorCode(err) != port.ErrCodeStorageFailed {
		t.Errorf("error code = %q, want %q", port.ErrorCode(err), port.ErrCodeStorageFailed)
	}
	if port.IsRetryable(err) {
		t.Error("invalid stored value should be non-retryable")
	}
}

func TestCheck_KeyPrefix(t *testing.T) {
	var gotKey string
	mock := &mockKVStore{
		getFn: func(_ context.Context, key string) (string, error) {
			gotKey = key
			return "", kvstore.ErrKeyNotFound
		},
	}

	s := NewStore(mock, 24*time.Hour)
	_, _ = s.Check(context.Background(), "abc-123")
	if gotKey != "idempotency:abc-123" {
		t.Errorf("key = %q, want %q", gotKey, "idempotency:abc-123")
	}
}

// --- Register tests ---

func TestRegister_Success(t *testing.T) {
	var gotKey string
	var gotValue string
	var gotTTL time.Duration

	mock := &mockKVStore{
		setNXFn: func(_ context.Context, key string, value string, ttl time.Duration) (bool, error) {
			gotKey = key
			gotValue = value
			gotTTL = ttl
			return true, nil
		},
	}

	s := NewStore(mock, 24*time.Hour)
	err := s.Register(context.Background(), "job-001")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotKey != "idempotency:job-001" {
		t.Errorf("key = %q, want %q", gotKey, "idempotency:job-001")
	}
	if gotValue != "in_progress" {
		t.Errorf("value = %q, want %q", gotValue, "in_progress")
	}
	if gotTTL != 24*time.Hour {
		t.Errorf("ttl = %v, want %v", gotTTL, 24*time.Hour)
	}
}

func TestRegister_AlreadyExists(t *testing.T) {
	mock := &mockKVStore{
		setNXFn: func(_ context.Context, _ string, _ string, _ time.Duration) (bool, error) {
			return false, nil
		},
	}

	s := NewStore(mock, 24*time.Hour)
	err := s.Register(context.Background(), "job-001")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if port.ErrorCode(err) != port.ErrCodeDuplicateJob {
		t.Errorf("error code = %q, want %q", port.ErrorCode(err), port.ErrCodeDuplicateJob)
	}
	if port.IsRetryable(err) {
		t.Error("duplicate job should be non-retryable")
	}
}

func TestRegister_StorageError(t *testing.T) {
	storageErr := port.NewStorageError("kvstore: SetNX", errors.New("connection refused"))
	mock := &mockKVStore{
		setNXFn: func(_ context.Context, _ string, _ string, _ time.Duration) (bool, error) {
			return false, storageErr
		},
	}

	s := NewStore(mock, 24*time.Hour)
	err := s.Register(context.Background(), "job-001")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if port.ErrorCode(err) != port.ErrCodeStorageFailed {
		t.Errorf("error code = %q, want %q", port.ErrorCode(err), port.ErrCodeStorageFailed)
	}
	if !port.IsRetryable(err) {
		t.Error("expected retryable error")
	}
}

func TestRegister_ContextCanceled(t *testing.T) {
	mock := &mockKVStore{
		setNXFn: func(_ context.Context, _ string, _ string, _ time.Duration) (bool, error) {
			return false, context.Canceled
		},
	}

	s := NewStore(mock, 24*time.Hour)
	err := s.Register(context.Background(), "job-001")
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got: %v", err)
	}
	if port.IsDomainError(err) {
		t.Error("context.Canceled should not be a DomainError")
	}
}

// --- MarkCompleted tests ---

func TestMarkCompleted_Success(t *testing.T) {
	var gotKey string
	var gotValue string
	var gotTTL time.Duration

	mock := &mockKVStore{
		setFn: func(_ context.Context, key string, value string, ttl time.Duration) error {
			gotKey = key
			gotValue = value
			gotTTL = ttl
			return nil
		},
	}

	s := NewStore(mock, 24*time.Hour)
	err := s.MarkCompleted(context.Background(), "job-001")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotKey != "idempotency:job-001" {
		t.Errorf("key = %q, want %q", gotKey, "idempotency:job-001")
	}
	if gotValue != "completed" {
		t.Errorf("value = %q, want %q", gotValue, "completed")
	}
	if gotTTL != 24*time.Hour {
		t.Errorf("ttl = %v, want %v", gotTTL, 24*time.Hour)
	}
}

func TestMarkCompleted_StorageError(t *testing.T) {
	storageErr := port.NewStorageError("kvstore: Set", errors.New("connection refused"))
	mock := &mockKVStore{
		setFn: func(_ context.Context, _ string, _ string, _ time.Duration) error {
			return storageErr
		},
	}

	s := NewStore(mock, 24*time.Hour)
	err := s.MarkCompleted(context.Background(), "job-001")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if port.ErrorCode(err) != port.ErrCodeStorageFailed {
		t.Errorf("error code = %q, want %q", port.ErrorCode(err), port.ErrCodeStorageFailed)
	}
	if !port.IsRetryable(err) {
		t.Error("expected retryable error")
	}
}

func TestMarkCompleted_ContextCanceled(t *testing.T) {
	mock := &mockKVStore{
		setFn: func(_ context.Context, _ string, _ string, _ time.Duration) error {
			return context.Canceled
		},
	}

	s := NewStore(mock, 24*time.Hour)
	err := s.MarkCompleted(context.Background(), "job-001")
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got: %v", err)
	}
	if port.IsDomainError(err) {
		t.Error("context.Canceled should not be a DomainError")
	}
}

// --- Full lifecycle test ---

func TestFullLifecycle(t *testing.T) {
	store := make(map[string]string)
	var mu sync.Mutex

	mock := &mockKVStore{
		getFn: func(_ context.Context, key string) (string, error) {
			mu.Lock()
			defer mu.Unlock()
			v, ok := store[key]
			if !ok {
				return "", kvstore.ErrKeyNotFound
			}
			return v, nil
		},
		setFn: func(_ context.Context, key string, value string, _ time.Duration) error {
			mu.Lock()
			store[key] = value
			mu.Unlock()
			return nil
		},
		setNXFn: func(_ context.Context, key string, value string, _ time.Duration) (bool, error) {
			mu.Lock()
			defer mu.Unlock()
			if _, exists := store[key]; exists {
				return false, nil
			}
			store[key] = value
			return true, nil
		},
	}

	s := NewStore(mock, 24*time.Hour)
	ctx := context.Background()

	// Step 1: Check → new
	status, err := s.Check(ctx, "job-lifecycle")
	if err != nil {
		t.Fatalf("Check new: %v", err)
	}
	if status != port.IdempotencyStatusNew {
		t.Fatalf("expected new, got %q", status)
	}

	// Step 2: Register → success
	err = s.Register(ctx, "job-lifecycle")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	// Step 3: Check → in_progress
	status, err = s.Check(ctx, "job-lifecycle")
	if err != nil {
		t.Fatalf("Check in_progress: %v", err)
	}
	if status != port.IdempotencyStatusInProgress {
		t.Fatalf("expected in_progress, got %q", status)
	}

	// Step 4: Register again → duplicate
	err = s.Register(ctx, "job-lifecycle")
	if err == nil {
		t.Fatal("expected duplicate error on second Register")
	}
	if port.ErrorCode(err) != port.ErrCodeDuplicateJob {
		t.Errorf("error code = %q, want %q", port.ErrorCode(err), port.ErrCodeDuplicateJob)
	}

	// Step 5: MarkCompleted → success
	err = s.MarkCompleted(ctx, "job-lifecycle")
	if err != nil {
		t.Fatalf("MarkCompleted: %v", err)
	}

	// Step 6: Check → completed
	status, err = s.Check(ctx, "job-lifecycle")
	if err != nil {
		t.Fatalf("Check completed: %v", err)
	}
	if status != port.IdempotencyStatusCompleted {
		t.Fatalf("expected completed, got %q", status)
	}
}

// --- Context forwarding ---

func TestCheck_ContextForwarding(t *testing.T) {
	type ctxKey struct{}
	expectedVal := "test-value"

	mock := &mockKVStore{
		getFn: func(ctx context.Context, _ string) (string, error) {
			if v := ctx.Value(ctxKey{}); v != expectedVal {
				t.Errorf("context value = %v, want %q", v, expectedVal)
			}
			return "", kvstore.ErrKeyNotFound
		},
	}

	ctx := context.WithValue(context.Background(), ctxKey{}, expectedVal)
	s := NewStore(mock, 24*time.Hour)
	_, _ = s.Check(ctx, "job-001")
}

func TestRegister_ContextForwarding(t *testing.T) {
	type ctxKey struct{}
	expectedVal := "test-value"

	mock := &mockKVStore{
		setNXFn: func(ctx context.Context, _ string, _ string, _ time.Duration) (bool, error) {
			if v := ctx.Value(ctxKey{}); v != expectedVal {
				t.Errorf("context value = %v, want %q", v, expectedVal)
			}
			return true, nil
		},
	}

	ctx := context.WithValue(context.Background(), ctxKey{}, expectedVal)
	s := NewStore(mock, 24*time.Hour)
	_ = s.Register(ctx, "job-001")
}

func TestMarkCompleted_ContextForwarding(t *testing.T) {
	type ctxKey struct{}
	expectedVal := "test-value"

	mock := &mockKVStore{
		setFn: func(ctx context.Context, _ string, _ string, _ time.Duration) error {
			if v := ctx.Value(ctxKey{}); v != expectedVal {
				t.Errorf("context value = %v, want %q", v, expectedVal)
			}
			return nil
		},
	}

	ctx := context.WithValue(context.Background(), ctxKey{}, expectedVal)
	s := NewStore(mock, 24*time.Hour)
	_ = s.MarkCompleted(ctx, "job-001")
}

// --- Empty jobID validation ---

func TestCheck_EmptyJobID(t *testing.T) {
	s := NewStore(&mockKVStore{}, 24*time.Hour)
	_, err := s.Check(context.Background(), "")
	if err == nil {
		t.Fatal("expected error for empty jobID")
	}
	if port.ErrorCode(err) != port.ErrCodeValidation {
		t.Errorf("error code = %q, want %q", port.ErrorCode(err), port.ErrCodeValidation)
	}
	if port.IsRetryable(err) {
		t.Error("empty jobID should be non-retryable")
	}
}

func TestRegister_EmptyJobID(t *testing.T) {
	s := NewStore(&mockKVStore{}, 24*time.Hour)
	err := s.Register(context.Background(), "")
	if err == nil {
		t.Fatal("expected error for empty jobID")
	}
	if port.ErrorCode(err) != port.ErrCodeValidation {
		t.Errorf("error code = %q, want %q", port.ErrorCode(err), port.ErrCodeValidation)
	}
}

func TestMarkCompleted_EmptyJobID(t *testing.T) {
	s := NewStore(&mockKVStore{}, 24*time.Hour)
	err := s.MarkCompleted(context.Background(), "")
	if err == nil {
		t.Fatal("expected error for empty jobID")
	}
	if port.ErrorCode(err) != port.ErrCodeValidation {
		t.Errorf("error code = %q, want %q", port.ErrorCode(err), port.ErrCodeValidation)
	}
}

// --- Interface compliance ---

func TestInterfaceCompliance(t *testing.T) {
	// Compile-time check already above, but this documents the intent.
	var store port.IdempotencyStorePort = NewStore(&mockKVStore{}, 24*time.Hour)
	_ = store
}
