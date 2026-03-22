package kvstore

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"contractpro/document-processing/internal/domain/port"
)

// --- mock ---

type mockRedis struct {
	setFn    func(ctx context.Context, key string, value interface{}, expiration time.Duration) *redis.StatusCmd
	setNXFn  func(ctx context.Context, key string, value interface{}, expiration time.Duration) *redis.BoolCmd
	getFn    func(ctx context.Context, key string) *redis.StringCmd
	existsFn func(ctx context.Context, keys ...string) *redis.IntCmd
	closeFn  func() error
	pingFn   func(ctx context.Context) *redis.StatusCmd
}

func (m *mockRedis) Set(ctx context.Context, key string, value interface{}, expiration time.Duration) *redis.StatusCmd {
	if m.setFn != nil {
		return m.setFn(ctx, key, value, expiration)
	}
	return redis.NewStatusResult("OK", nil)
}

func (m *mockRedis) SetNX(ctx context.Context, key string, value interface{}, expiration time.Duration) *redis.BoolCmd {
	if m.setNXFn != nil {
		return m.setNXFn(ctx, key, value, expiration)
	}
	return redis.NewBoolResult(true, nil)
}

func (m *mockRedis) Get(ctx context.Context, key string) *redis.StringCmd {
	if m.getFn != nil {
		return m.getFn(ctx, key)
	}
	return redis.NewStringResult("", redis.Nil)
}

func (m *mockRedis) Exists(ctx context.Context, keys ...string) *redis.IntCmd {
	if m.existsFn != nil {
		return m.existsFn(ctx, keys...)
	}
	return redis.NewIntResult(0, nil)
}

func (m *mockRedis) Close() error {
	if m.closeFn != nil {
		return m.closeFn()
	}
	return nil
}

func (m *mockRedis) Ping(ctx context.Context) *redis.StatusCmd {
	if m.pingFn != nil {
		return m.pingFn(ctx)
	}
	return redis.NewStatusResult("PONG", nil)
}

// --- in-memory store for integration-style tests ---

func newInMemoryRedis() *mockRedis {
	store := make(map[string]string)
	var mu sync.Mutex

	return &mockRedis{
		setFn: func(_ context.Context, key string, value interface{}, _ time.Duration) *redis.StatusCmd {
			mu.Lock()
			store[key] = fmt.Sprint(value)
			mu.Unlock()
			return redis.NewStatusResult("OK", nil)
		},
		setNXFn: func(_ context.Context, key string, value interface{}, _ time.Duration) *redis.BoolCmd {
			mu.Lock()
			defer mu.Unlock()
			if _, exists := store[key]; exists {
				return redis.NewBoolResult(false, nil)
			}
			store[key] = fmt.Sprint(value)
			return redis.NewBoolResult(true, nil)
		},
		getFn: func(_ context.Context, key string) *redis.StringCmd {
			mu.Lock()
			v, ok := store[key]
			mu.Unlock()
			if !ok {
				return redis.NewStringResult("", redis.Nil)
			}
			return redis.NewStringResult(v, nil)
		},
		existsFn: func(_ context.Context, keys ...string) *redis.IntCmd {
			mu.Lock()
			var count int64
			for _, k := range keys {
				if _, ok := store[k]; ok {
					count++
				}
			}
			mu.Unlock()
			return redis.NewIntResult(count, nil)
		},
		closeFn: func() error { return nil },
		pingFn: func(_ context.Context) *redis.StatusCmd {
			return redis.NewStatusResult("PONG", nil)
		},
	}
}

// --- interface compliance ---

var _ RedisAPI = (*mockRedis)(nil)

// --- Set tests ---

func TestSet_Success(t *testing.T) {
	var gotKey string
	var gotValue interface{}
	var gotTTL time.Duration

	mock := &mockRedis{
		setFn: func(_ context.Context, key string, value interface{}, exp time.Duration) *redis.StatusCmd {
			gotKey = key
			gotValue = value
			gotTTL = exp
			return redis.NewStatusResult("OK", nil)
		},
	}

	c := newClientWithRedis(mock)
	err := c.Set(context.Background(), "job:123", "in_progress", 24*time.Hour)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotKey != "job:123" {
		t.Errorf("key = %q, want %q", gotKey, "job:123")
	}
	if gotValue != "in_progress" {
		t.Errorf("value = %v, want %q", gotValue, "in_progress")
	}
	if gotTTL != 24*time.Hour {
		t.Errorf("ttl = %v, want %v", gotTTL, 24*time.Hour)
	}
}

func TestSet_RedisError(t *testing.T) {
	mock := &mockRedis{
		setFn: func(_ context.Context, _ string, _ interface{}, _ time.Duration) *redis.StatusCmd {
			return redis.NewStatusResult("", errors.New("connection refused"))
		},
	}

	c := newClientWithRedis(mock)
	err := c.Set(context.Background(), "key", "val", time.Minute)
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

func TestSet_ContextCancelled(t *testing.T) {
	mock := &mockRedis{
		setFn: func(_ context.Context, _ string, _ interface{}, _ time.Duration) *redis.StatusCmd {
			return redis.NewStatusResult("", context.Canceled)
		},
	}

	c := newClientWithRedis(mock)
	err := c.Set(context.Background(), "key", "val", time.Minute)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got: %v", err)
	}
	// Must NOT be a DomainError.
	if port.IsDomainError(err) {
		t.Error("context.Canceled should not be wrapped as DomainError")
	}
}

// --- Get tests ---

func TestGet_Success(t *testing.T) {
	mock := &mockRedis{
		getFn: func(_ context.Context, _ string) *redis.StringCmd {
			return redis.NewStringResult("completed", nil)
		},
	}

	c := newClientWithRedis(mock)
	val, err := c.Get(context.Background(), "job:123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != "completed" {
		t.Errorf("value = %q, want %q", val, "completed")
	}
}

func TestGet_KeyNotFound(t *testing.T) {
	mock := &mockRedis{
		getFn: func(_ context.Context, _ string) *redis.StringCmd {
			return redis.NewStringResult("", redis.Nil)
		},
	}

	c := newClientWithRedis(mock)
	val, err := c.Get(context.Background(), "nonexistent")
	if !errors.Is(err, ErrKeyNotFound) {
		t.Errorf("expected ErrKeyNotFound, got: %v", err)
	}
	if val != "" {
		t.Errorf("value = %q, want empty string", val)
	}
	// Must NOT be a DomainError.
	if port.IsDomainError(err) {
		t.Error("ErrKeyNotFound should not be a DomainError")
	}
}

func TestGet_RedisError(t *testing.T) {
	mock := &mockRedis{
		getFn: func(_ context.Context, _ string) *redis.StringCmd {
			return redis.NewStringResult("", errors.New("connection reset"))
		},
	}

	c := newClientWithRedis(mock)
	_, err := c.Get(context.Background(), "key")
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

func TestGet_ContextDeadlineExceeded(t *testing.T) {
	mock := &mockRedis{
		getFn: func(_ context.Context, _ string) *redis.StringCmd {
			return redis.NewStringResult("", context.DeadlineExceeded)
		},
	}

	c := newClientWithRedis(mock)
	_, err := c.Get(context.Background(), "key")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("expected context.DeadlineExceeded, got: %v", err)
	}
	if port.IsDomainError(err) {
		t.Error("context.DeadlineExceeded should not be wrapped as DomainError")
	}
}

// --- Exists tests ---

func TestExists_KeyPresent(t *testing.T) {
	mock := &mockRedis{
		existsFn: func(_ context.Context, _ ...string) *redis.IntCmd {
			return redis.NewIntResult(1, nil)
		},
	}

	c := newClientWithRedis(mock)
	exists, err := c.Exists(context.Background(), "job:123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !exists {
		t.Error("expected true, got false")
	}
}

func TestExists_KeyAbsent(t *testing.T) {
	mock := &mockRedis{
		existsFn: func(_ context.Context, _ ...string) *redis.IntCmd {
			return redis.NewIntResult(0, nil)
		},
	}

	c := newClientWithRedis(mock)
	exists, err := c.Exists(context.Background(), "nonexistent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if exists {
		t.Error("expected false, got true")
	}
}

func TestExists_RedisError(t *testing.T) {
	mock := &mockRedis{
		existsFn: func(_ context.Context, _ ...string) *redis.IntCmd {
			return redis.NewIntResult(0, errors.New("pool exhausted"))
		},
	}

	c := newClientWithRedis(mock)
	_, err := c.Exists(context.Background(), "key")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if port.ErrorCode(err) != port.ErrCodeStorageFailed {
		t.Errorf("error code = %q, want %q", port.ErrorCode(err), port.ErrCodeStorageFailed)
	}
}

// --- Close tests ---

func TestClose_GracefulShutdown(t *testing.T) {
	closeCalled := false
	mock := &mockRedis{
		closeFn: func() error {
			closeCalled = true
			return nil
		},
	}

	c := newClientWithRedis(mock)
	err := c.Close()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !closeCalled {
		t.Error("expected rdb.Close() to be called")
	}
}

func TestClose_Idempotent(t *testing.T) {
	callCount := 0
	mock := &mockRedis{
		closeFn: func() error {
			callCount++
			return nil
		},
	}

	c := newClientWithRedis(mock)
	if err := c.Close(); err != nil {
		t.Fatalf("first close: %v", err)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("second close: %v", err)
	}
	if callCount != 1 {
		t.Errorf("rdb.Close() called %d times, want 1", callCount)
	}
}

// --- Error mapping tests ---

func TestMapError_ContextCanceled(t *testing.T) {
	err := mapError(context.Canceled, "Test")
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled passthrough, got: %v", err)
	}
	if port.IsDomainError(err) {
		t.Error("should not be wrapped as DomainError")
	}
}

func TestMapError_ContextDeadlineExceeded(t *testing.T) {
	err := mapError(context.DeadlineExceeded, "Test")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("expected context.DeadlineExceeded passthrough, got: %v", err)
	}
	if port.IsDomainError(err) {
		t.Error("should not be wrapped as DomainError")
	}
}

func TestMapError_RedisNil(t *testing.T) {
	err := mapError(redis.Nil, "Test")
	if !errors.Is(err, ErrKeyNotFound) {
		t.Errorf("expected ErrKeyNotFound, got: %v", err)
	}
}

func TestMapError_UnknownError(t *testing.T) {
	err := mapError(errors.New("something broke"), "Set")
	if port.ErrorCode(err) != port.ErrCodeStorageFailed {
		t.Errorf("error code = %q, want %q", port.ErrorCode(err), port.ErrCodeStorageFailed)
	}
	if !port.IsRetryable(err) {
		t.Error("expected retryable error")
	}
}

// --- In-memory integration: Set → Get → Exists sequence ---

func TestSetGetExists_InMemory(t *testing.T) {
	mock := newInMemoryRedis()
	c := newClientWithRedis(mock)
	ctx := context.Background()

	// Key should not exist initially.
	exists, err := c.Exists(ctx, "job:abc")
	if err != nil {
		t.Fatalf("Exists: %v", err)
	}
	if exists {
		t.Error("expected key to not exist")
	}

	// Get should return ErrKeyNotFound.
	_, err = c.Get(ctx, "job:abc")
	if !errors.Is(err, ErrKeyNotFound) {
		t.Fatalf("Get non-existent: expected ErrKeyNotFound, got: %v", err)
	}

	// Set the key.
	err = c.Set(ctx, "job:abc", "in_progress", 24*time.Hour)
	if err != nil {
		t.Fatalf("Set: %v", err)
	}

	// Key should now exist.
	exists, err = c.Exists(ctx, "job:abc")
	if err != nil {
		t.Fatalf("Exists after Set: %v", err)
	}
	if !exists {
		t.Error("expected key to exist after Set")
	}

	// Get should return the value.
	val, err := c.Get(ctx, "job:abc")
	if err != nil {
		t.Fatalf("Get after Set: %v", err)
	}
	if val != "in_progress" {
		t.Errorf("value = %q, want %q", val, "in_progress")
	}

	// Overwrite with new value.
	err = c.Set(ctx, "job:abc", "completed", 24*time.Hour)
	if err != nil {
		t.Fatalf("Set overwrite: %v", err)
	}
	val, err = c.Get(ctx, "job:abc")
	if err != nil {
		t.Fatalf("Get after overwrite: %v", err)
	}
	if val != "completed" {
		t.Errorf("value = %q, want %q", val, "completed")
	}
}

// --- Concurrent access ---

func TestConcurrentAccess(t *testing.T) {
	mock := newInMemoryRedis()
	c := newClientWithRedis(mock)
	ctx := context.Background()

	const goroutines = 50
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func(n int) {
			defer wg.Done()
			key := fmt.Sprintf("job:%d", n)
			if err := c.Set(ctx, key, "value", time.Hour); err != nil {
				t.Errorf("Set(%s): %v", key, err)
			}
			if _, err := c.Get(ctx, key); err != nil {
				t.Errorf("Get(%s): %v", key, err)
			}
			if _, err := c.Exists(ctx, key); err != nil {
				t.Errorf("Exists(%s): %v", key, err)
			}
			if _, err := c.SetNX(ctx, key+":nx", "v", time.Hour); err != nil {
				t.Errorf("SetNX(%s:nx): %v", key, err)
			}
		}(i)
	}

	wg.Wait()
}

// --- Use-after-close guard ---

func TestSet_AfterClose(t *testing.T) {
	c := newClientWithRedis(&mockRedis{})
	_ = c.Close()

	err := c.Set(context.Background(), "k", "v", time.Minute)
	if err == nil {
		t.Fatal("expected error on Set after Close")
	}
	if port.ErrorCode(err) != port.ErrCodeStorageFailed {
		t.Errorf("error code = %q, want %q", port.ErrorCode(err), port.ErrCodeStorageFailed)
	}
	if port.IsRetryable(err) {
		t.Error("use-after-close should be non-retryable")
	}
}

func TestGet_AfterClose(t *testing.T) {
	c := newClientWithRedis(&mockRedis{})
	_ = c.Close()

	_, err := c.Get(context.Background(), "k")
	if err == nil {
		t.Fatal("expected error on Get after Close")
	}
	if port.ErrorCode(err) != port.ErrCodeStorageFailed {
		t.Errorf("error code = %q, want %q", port.ErrorCode(err), port.ErrCodeStorageFailed)
	}
	if port.IsRetryable(err) {
		t.Error("use-after-close should be non-retryable")
	}
}

func TestExists_AfterClose(t *testing.T) {
	c := newClientWithRedis(&mockRedis{})
	_ = c.Close()

	_, err := c.Exists(context.Background(), "k")
	if err == nil {
		t.Fatal("expected error on Exists after Close")
	}
	if port.ErrorCode(err) != port.ErrCodeStorageFailed {
		t.Errorf("error code = %q, want %q", port.ErrorCode(err), port.ErrCodeStorageFailed)
	}
	if port.IsRetryable(err) {
		t.Error("use-after-close should be non-retryable")
	}
}

// --- Set with zero TTL (no expiration) ---

func TestSet_ZeroTTL(t *testing.T) {
	var gotTTL time.Duration
	mock := &mockRedis{
		setFn: func(_ context.Context, _ string, _ interface{}, exp time.Duration) *redis.StatusCmd {
			gotTTL = exp
			return redis.NewStatusResult("OK", nil)
		},
	}

	c := newClientWithRedis(mock)
	err := c.Set(context.Background(), "persistent-key", "value", 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotTTL != 0 {
		t.Errorf("ttl = %v, want 0", gotTTL)
	}
}

// --- Context forwarding ---

func TestSet_ContextForwarding(t *testing.T) {
	type ctxKey struct{}
	expectedVal := "test-value"

	mock := &mockRedis{
		setFn: func(ctx context.Context, _ string, _ interface{}, _ time.Duration) *redis.StatusCmd {
			if v := ctx.Value(ctxKey{}); v != expectedVal {
				t.Errorf("context value = %v, want %q", v, expectedVal)
			}
			return redis.NewStatusResult("OK", nil)
		},
	}

	ctx := context.WithValue(context.Background(), ctxKey{}, expectedVal)
	c := newClientWithRedis(mock)
	_ = c.Set(ctx, "k", "v", time.Minute)
}

func TestGet_ContextForwarding(t *testing.T) {
	type ctxKey struct{}
	expectedVal := "test-value"

	mock := &mockRedis{
		getFn: func(ctx context.Context, _ string) *redis.StringCmd {
			if v := ctx.Value(ctxKey{}); v != expectedVal {
				t.Errorf("context value = %v, want %q", v, expectedVal)
			}
			return redis.NewStringResult("val", nil)
		},
	}

	ctx := context.WithValue(context.Background(), ctxKey{}, expectedVal)
	c := newClientWithRedis(mock)
	_, _ = c.Get(ctx, "k")
}

func TestExists_ContextForwarding(t *testing.T) {
	type ctxKey struct{}
	expectedVal := "test-value"

	mock := &mockRedis{
		existsFn: func(ctx context.Context, _ ...string) *redis.IntCmd {
			if v := ctx.Value(ctxKey{}); v != expectedVal {
				t.Errorf("context value = %v, want %q", v, expectedVal)
			}
			return redis.NewIntResult(1, nil)
		},
	}

	ctx := context.WithValue(context.Background(), ctxKey{}, expectedVal)
	c := newClientWithRedis(mock)
	_, _ = c.Exists(ctx, "k")
}

// --- Exists key forwarding ---

func TestExists_KeyForwarding(t *testing.T) {
	var gotKeys []string
	mock := &mockRedis{
		existsFn: func(_ context.Context, keys ...string) *redis.IntCmd {
			gotKeys = keys
			return redis.NewIntResult(1, nil)
		},
	}

	c := newClientWithRedis(mock)
	_, _ = c.Exists(context.Background(), "my-key")
	if len(gotKeys) != 1 || gotKeys[0] != "my-key" {
		t.Errorf("keys = %v, want [\"my-key\"]", gotKeys)
	}
}

// --- SetNX tests ---

func TestSetNX_Success(t *testing.T) {
	var gotKey string
	var gotValue interface{}
	var gotTTL time.Duration

	mock := &mockRedis{
		setNXFn: func(_ context.Context, key string, value interface{}, exp time.Duration) *redis.BoolCmd {
			gotKey = key
			gotValue = value
			gotTTL = exp
			return redis.NewBoolResult(true, nil)
		},
	}

	c := newClientWithRedis(mock)
	ok, err := c.SetNX(context.Background(), "job:123", "in_progress", 24*time.Hour)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Error("expected true (key was set), got false")
	}
	if gotKey != "job:123" {
		t.Errorf("key = %q, want %q", gotKey, "job:123")
	}
	if gotValue != "in_progress" {
		t.Errorf("value = %v, want %q", gotValue, "in_progress")
	}
	if gotTTL != 24*time.Hour {
		t.Errorf("ttl = %v, want %v", gotTTL, 24*time.Hour)
	}
}

func TestSetNX_KeyAlreadyExists(t *testing.T) {
	mock := &mockRedis{
		setNXFn: func(_ context.Context, _ string, _ interface{}, _ time.Duration) *redis.BoolCmd {
			return redis.NewBoolResult(false, nil)
		},
	}

	c := newClientWithRedis(mock)
	ok, err := c.SetNX(context.Background(), "job:123", "in_progress", 24*time.Hour)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Error("expected false (key already existed), got true")
	}
}

func TestSetNX_RedisError(t *testing.T) {
	mock := &mockRedis{
		setNXFn: func(_ context.Context, _ string, _ interface{}, _ time.Duration) *redis.BoolCmd {
			return redis.NewBoolResult(false, errors.New("connection refused"))
		},
	}

	c := newClientWithRedis(mock)
	_, err := c.SetNX(context.Background(), "key", "val", time.Minute)
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

func TestSetNX_ContextCancelled(t *testing.T) {
	mock := &mockRedis{
		setNXFn: func(_ context.Context, _ string, _ interface{}, _ time.Duration) *redis.BoolCmd {
			return redis.NewBoolResult(false, context.Canceled)
		},
	}

	c := newClientWithRedis(mock)
	_, err := c.SetNX(context.Background(), "key", "val", time.Minute)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got: %v", err)
	}
	if port.IsDomainError(err) {
		t.Error("context.Canceled should not be wrapped as DomainError")
	}
}

func TestSetNX_ContextForwarding(t *testing.T) {
	type ctxKey struct{}
	expectedVal := "test-value"

	mock := &mockRedis{
		setNXFn: func(ctx context.Context, _ string, _ interface{}, _ time.Duration) *redis.BoolCmd {
			if v := ctx.Value(ctxKey{}); v != expectedVal {
				t.Errorf("context value = %v, want %q", v, expectedVal)
			}
			return redis.NewBoolResult(true, nil)
		},
	}

	ctx := context.WithValue(context.Background(), ctxKey{}, expectedVal)
	c := newClientWithRedis(mock)
	_, _ = c.SetNX(ctx, "k", "v", time.Minute)
}

func TestSetNX_AfterClose(t *testing.T) {
	c := newClientWithRedis(&mockRedis{})
	_ = c.Close()

	_, err := c.SetNX(context.Background(), "k", "v", time.Minute)
	if err == nil {
		t.Fatal("expected error on SetNX after Close")
	}
	if port.ErrorCode(err) != port.ErrCodeStorageFailed {
		t.Errorf("error code = %q, want %q", port.ErrorCode(err), port.ErrCodeStorageFailed)
	}
	if port.IsRetryable(err) {
		t.Error("use-after-close should be non-retryable")
	}
}
