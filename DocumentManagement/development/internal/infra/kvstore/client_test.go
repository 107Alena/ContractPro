package kvstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"contractpro/document-management/internal/domain/model"
	"contractpro/document-management/internal/domain/port"
)

// --- mock ---

type mockRedis struct {
	setFn   func(ctx context.Context, key string, value interface{}, expiration time.Duration) *redis.StatusCmd
	getFn   func(ctx context.Context, key string) *redis.StringCmd
	delFn   func(ctx context.Context, keys ...string) *redis.IntCmd
	pingFn  func(ctx context.Context) *redis.StatusCmd
	closeFn func() error
}

func (m *mockRedis) Set(ctx context.Context, key string, value interface{}, expiration time.Duration) *redis.StatusCmd {
	if m.setFn != nil {
		return m.setFn(ctx, key, value, expiration)
	}
	return redis.NewStatusResult("OK", nil)
}

func (m *mockRedis) Get(ctx context.Context, key string) *redis.StringCmd {
	if m.getFn != nil {
		return m.getFn(ctx, key)
	}
	return redis.NewStringResult("", redis.Nil)
}

func (m *mockRedis) Del(ctx context.Context, keys ...string) *redis.IntCmd {
	if m.delFn != nil {
		return m.delFn(ctx, keys...)
	}
	return redis.NewIntResult(0, nil)
}

func (m *mockRedis) Ping(ctx context.Context) *redis.StatusCmd {
	if m.pingFn != nil {
		return m.pingFn(ctx)
	}
	return redis.NewStatusResult("PONG", nil)
}

func (m *mockRedis) Close() error {
	if m.closeFn != nil {
		return m.closeFn()
	}
	return nil
}

// --- interface compliance ---

var _ RedisAPI = (*mockRedis)(nil)

// --- in-memory store for integration-style tests ---

func newInMemoryRedis() *mockRedis {
	store := make(map[string]string)
	var mu sync.Mutex

	return &mockRedis{
		setFn: func(_ context.Context, key string, value interface{}, _ time.Duration) *redis.StatusCmd {
			mu.Lock()
			switch v := value.(type) {
			case []byte:
				store[key] = string(v)
			default:
				store[key] = fmt.Sprint(v)
			}
			mu.Unlock()
			return redis.NewStatusResult("OK", nil)
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
		delFn: func(_ context.Context, keys ...string) *redis.IntCmd {
			mu.Lock()
			var count int64
			for _, k := range keys {
				if _, ok := store[k]; ok {
					delete(store, k)
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

// --- test helpers ---

func makeRecord(key string, status model.IdempotencyStatus) *model.IdempotencyRecord {
	now := time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)
	return &model.IdempotencyRecord{
		Key:       key,
		Status:    status,
		CreatedAt: now,
		UpdatedAt: now,
	}
}

func marshalRecord(t *testing.T, r *model.IdempotencyRecord) string {
	t.Helper()
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("failed to marshal record: %v", err)
	}
	return string(data)
}

// --- Get tests ---

func TestGet_Success(t *testing.T) {
	record := makeRecord("dp-artifacts:job-1", model.IdempotencyStatusProcessing)
	jsonStr := marshalRecord(t, record)

	mock := &mockRedis{
		getFn: func(_ context.Context, key string) *redis.StringCmd {
			if key != "dp-artifacts:job-1" {
				t.Errorf("key = %q, want %q", key, "dp-artifacts:job-1")
			}
			return redis.NewStringResult(jsonStr, nil)
		},
	}

	c := newClientWithRedis(mock)
	got, err := c.Get(context.Background(), "dp-artifacts:job-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil {
		t.Fatal("expected record, got nil")
	}
	if got.Key != record.Key {
		t.Errorf("Key = %q, want %q", got.Key, record.Key)
	}
	if got.Status != record.Status {
		t.Errorf("Status = %q, want %q", got.Status, record.Status)
	}
}

func TestGet_CompletedRecord(t *testing.T) {
	record := makeRecord("dp-artifacts:job-2", model.IdempotencyStatusCompleted)
	record.ResultSnapshot = `{"persisted":true}`
	jsonStr := marshalRecord(t, record)

	mock := &mockRedis{
		getFn: func(_ context.Context, _ string) *redis.StringCmd {
			return redis.NewStringResult(jsonStr, nil)
		},
	}

	c := newClientWithRedis(mock)
	got, err := c.Get(context.Background(), "dp-artifacts:job-2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Status != model.IdempotencyStatusCompleted {
		t.Errorf("Status = %q, want %q", got.Status, model.IdempotencyStatusCompleted)
	}
	if got.ResultSnapshot != `{"persisted":true}` {
		t.Errorf("ResultSnapshot = %q, want %q", got.ResultSnapshot, `{"persisted":true}`)
	}
}

func TestGet_KeyNotFound_ReturnsNilNil(t *testing.T) {
	mock := &mockRedis{
		getFn: func(_ context.Context, _ string) *redis.StringCmd {
			return redis.NewStringResult("", redis.Nil)
		},
	}

	c := newClientWithRedis(mock)
	got, err := c.Get(context.Background(), "nonexistent")
	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil record, got: %+v", got)
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

func TestGet_ContextCanceled(t *testing.T) {
	mock := &mockRedis{
		getFn: func(_ context.Context, _ string) *redis.StringCmd {
			return redis.NewStringResult("", context.Canceled)
		},
	}

	c := newClientWithRedis(mock)
	_, err := c.Get(context.Background(), "key")
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got: %v", err)
	}
	if port.IsDomainError(err) {
		t.Error("context.Canceled should not be wrapped as DomainError")
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

func TestGet_InvalidJSON(t *testing.T) {
	mock := &mockRedis{
		getFn: func(_ context.Context, _ string) *redis.StringCmd {
			return redis.NewStringResult("not-valid-json{", nil)
		},
	}

	c := newClientWithRedis(mock)
	_, err := c.Get(context.Background(), "key")
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
	if port.ErrorCode(err) != port.ErrCodeStorageFailed {
		t.Errorf("error code = %q, want %q", port.ErrorCode(err), port.ErrCodeStorageFailed)
	}
}

func TestGet_ContextForwarding(t *testing.T) {
	type ctxKey struct{}
	expectedVal := "test-value"

	mock := &mockRedis{
		getFn: func(ctx context.Context, _ string) *redis.StringCmd {
			if v := ctx.Value(ctxKey{}); v != expectedVal {
				t.Errorf("context value = %v, want %q", v, expectedVal)
			}
			return redis.NewStringResult("", redis.Nil)
		},
	}

	ctx := context.WithValue(context.Background(), ctxKey{}, expectedVal)
	c := newClientWithRedis(mock)
	_, _ = c.Get(ctx, "k")
}

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

	record := makeRecord("dp-artifacts:job-1", model.IdempotencyStatusProcessing)
	c := newClientWithRedis(mock)
	err := c.Set(context.Background(), record, 120*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotKey != "dp-artifacts:job-1" {
		t.Errorf("key = %q, want %q", gotKey, "dp-artifacts:job-1")
	}
	if gotTTL != 120*time.Second {
		t.Errorf("ttl = %v, want %v", gotTTL, 120*time.Second)
	}

	// Verify JSON round-trip.
	data, ok := gotValue.([]byte)
	if !ok {
		t.Fatalf("value type = %T, want []byte", gotValue)
	}
	var roundTrip model.IdempotencyRecord
	if err := json.Unmarshal(data, &roundTrip); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if roundTrip.Key != record.Key {
		t.Errorf("roundTrip.Key = %q, want %q", roundTrip.Key, record.Key)
	}
	if roundTrip.Status != record.Status {
		t.Errorf("roundTrip.Status = %q, want %q", roundTrip.Status, record.Status)
	}
}

func TestSet_JSONContainsAllFields(t *testing.T) {
	var gotValue interface{}

	mock := &mockRedis{
		setFn: func(_ context.Context, _ string, value interface{}, _ time.Duration) *redis.StatusCmd {
			gotValue = value
			return redis.NewStatusResult("OK", nil)
		},
	}

	record := makeRecord("dp-artifacts:job-1", model.IdempotencyStatusCompleted)
	record.ResultSnapshot = `{"some":"data"}`

	c := newClientWithRedis(mock)
	err := c.Set(context.Background(), record, 24*time.Hour)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, ok := gotValue.([]byte)
	if !ok {
		t.Fatalf("value type = %T, want []byte", gotValue)
	}
	var roundTrip model.IdempotencyRecord
	if err := json.Unmarshal(data, &roundTrip); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if roundTrip.ResultSnapshot != `{"some":"data"}` {
		t.Errorf("ResultSnapshot = %q, want %q", roundTrip.ResultSnapshot, `{"some":"data"}`)
	}
	if roundTrip.CreatedAt.IsZero() {
		t.Error("CreatedAt should not be zero")
	}
	if roundTrip.UpdatedAt.IsZero() {
		t.Error("UpdatedAt should not be zero")
	}
}

func TestSet_RedisError(t *testing.T) {
	mock := &mockRedis{
		setFn: func(_ context.Context, _ string, _ interface{}, _ time.Duration) *redis.StatusCmd {
			return redis.NewStatusResult("", errors.New("connection refused"))
		},
	}

	record := makeRecord("key", model.IdempotencyStatusProcessing)
	c := newClientWithRedis(mock)
	err := c.Set(context.Background(), record, time.Minute)
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

func TestSet_ContextCanceled(t *testing.T) {
	mock := &mockRedis{
		setFn: func(_ context.Context, _ string, _ interface{}, _ time.Duration) *redis.StatusCmd {
			return redis.NewStatusResult("", context.Canceled)
		},
	}

	record := makeRecord("key", model.IdempotencyStatusProcessing)
	c := newClientWithRedis(mock)
	err := c.Set(context.Background(), record, time.Minute)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got: %v", err)
	}
	if port.IsDomainError(err) {
		t.Error("context.Canceled should not be wrapped as DomainError")
	}
}

func TestSet_ContextDeadlineExceeded(t *testing.T) {
	mock := &mockRedis{
		setFn: func(_ context.Context, _ string, _ interface{}, _ time.Duration) *redis.StatusCmd {
			return redis.NewStatusResult("", context.DeadlineExceeded)
		},
	}

	record := makeRecord("key", model.IdempotencyStatusProcessing)
	c := newClientWithRedis(mock)
	err := c.Set(context.Background(), record, time.Minute)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("expected context.DeadlineExceeded, got: %v", err)
	}
}

func TestSet_ZeroTTL(t *testing.T) {
	var gotTTL time.Duration
	mock := &mockRedis{
		setFn: func(_ context.Context, _ string, _ interface{}, exp time.Duration) *redis.StatusCmd {
			gotTTL = exp
			return redis.NewStatusResult("OK", nil)
		},
	}

	record := makeRecord("key", model.IdempotencyStatusProcessing)
	c := newClientWithRedis(mock)
	err := c.Set(context.Background(), record, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotTTL != 0 {
		t.Errorf("ttl = %v, want 0", gotTTL)
	}
}

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
	record := makeRecord("key", model.IdempotencyStatusProcessing)
	c := newClientWithRedis(mock)
	_ = c.Set(ctx, record, time.Minute)
}

// --- Delete tests ---

func TestDelete_Success(t *testing.T) {
	var gotKey string
	mock := &mockRedis{
		delFn: func(_ context.Context, keys ...string) *redis.IntCmd {
			if len(keys) > 0 {
				gotKey = keys[0]
			}
			return redis.NewIntResult(1, nil)
		},
	}

	c := newClientWithRedis(mock)
	err := c.Delete(context.Background(), "dp-artifacts:job-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotKey != "dp-artifacts:job-1" {
		t.Errorf("key = %q, want %q", gotKey, "dp-artifacts:job-1")
	}
}

func TestDelete_KeyNotExists(t *testing.T) {
	mock := &mockRedis{
		delFn: func(_ context.Context, _ ...string) *redis.IntCmd {
			return redis.NewIntResult(0, nil) // key didn't exist
		},
	}

	c := newClientWithRedis(mock)
	err := c.Delete(context.Background(), "nonexistent")
	if err != nil {
		t.Fatalf("expected nil error for deleting nonexistent key, got: %v", err)
	}
}

func TestDelete_RedisError(t *testing.T) {
	mock := &mockRedis{
		delFn: func(_ context.Context, _ ...string) *redis.IntCmd {
			return redis.NewIntResult(0, errors.New("connection refused"))
		},
	}

	c := newClientWithRedis(mock)
	err := c.Delete(context.Background(), "key")
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

func TestDelete_ContextCanceled(t *testing.T) {
	mock := &mockRedis{
		delFn: func(_ context.Context, _ ...string) *redis.IntCmd {
			return redis.NewIntResult(0, context.Canceled)
		},
	}

	c := newClientWithRedis(mock)
	err := c.Delete(context.Background(), "key")
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got: %v", err)
	}
	if port.IsDomainError(err) {
		t.Error("context.Canceled should not be wrapped as DomainError")
	}
}

func TestDelete_ContextForwarding(t *testing.T) {
	type ctxKey struct{}
	expectedVal := "test-value"

	mock := &mockRedis{
		delFn: func(ctx context.Context, _ ...string) *redis.IntCmd {
			if v := ctx.Value(ctxKey{}); v != expectedVal {
				t.Errorf("context value = %v, want %q", v, expectedVal)
			}
			return redis.NewIntResult(1, nil)
		},
	}

	ctx := context.WithValue(context.Background(), ctxKey{}, expectedVal)
	c := newClientWithRedis(mock)
	_ = c.Delete(ctx, "k")
}

// --- Nil record / empty key tests ---

func TestSet_NilRecord(t *testing.T) {
	c := newClientWithRedis(&mockRedis{})
	err := c.Set(context.Background(), nil, time.Minute)
	if err == nil {
		t.Fatal("expected error for nil record, got nil")
	}
	if port.ErrorCode(err) != port.ErrCodeValidation {
		t.Errorf("error code = %q, want %q", port.ErrorCode(err), port.ErrCodeValidation)
	}
}

func TestGet_EmptyKey(t *testing.T) {
	c := newClientWithRedis(&mockRedis{})
	_, err := c.Get(context.Background(), "")
	if err == nil {
		t.Fatal("expected error for empty key, got nil")
	}
	if port.ErrorCode(err) != port.ErrCodeValidation {
		t.Errorf("error code = %q, want %q", port.ErrorCode(err), port.ErrCodeValidation)
	}
}

func TestDelete_EmptyKey(t *testing.T) {
	c := newClientWithRedis(&mockRedis{})
	err := c.Delete(context.Background(), "")
	if err == nil {
		t.Fatal("expected error for empty key, got nil")
	}
	if port.ErrorCode(err) != port.ErrCodeValidation {
		t.Errorf("error code = %q, want %q", port.ErrorCode(err), port.ErrCodeValidation)
	}
}

func TestDelete_ContextDeadlineExceeded(t *testing.T) {
	mock := &mockRedis{
		delFn: func(_ context.Context, _ ...string) *redis.IntCmd {
			return redis.NewIntResult(0, context.DeadlineExceeded)
		},
	}

	c := newClientWithRedis(mock)
	err := c.Delete(context.Background(), "key")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("expected context.DeadlineExceeded, got: %v", err)
	}
	if port.IsDomainError(err) {
		t.Error("context.DeadlineExceeded should not be wrapped as DomainError")
	}
}

// --- Ping tests ---

func TestPing_Success(t *testing.T) {
	mock := &mockRedis{
		pingFn: func(_ context.Context) *redis.StatusCmd {
			return redis.NewStatusResult("PONG", nil)
		},
	}

	c := newClientWithRedis(mock)
	err := c.Ping(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPing_RedisError(t *testing.T) {
	mock := &mockRedis{
		pingFn: func(_ context.Context) *redis.StatusCmd {
			return redis.NewStatusResult("", errors.New("connection refused"))
		},
	}

	c := newClientWithRedis(mock)
	err := c.Ping(context.Background())
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

func TestClose_ReturnsError(t *testing.T) {
	mock := &mockRedis{
		closeFn: func() error {
			return errors.New("connection pool drain failed")
		},
	}

	c := newClientWithRedis(mock)
	err := c.Close()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if port.ErrorCode(err) != port.ErrCodeStorageFailed {
		t.Errorf("error code = %q, want %q", port.ErrorCode(err), port.ErrCodeStorageFailed)
	}
}

// --- Use-after-close guard tests ---

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

func TestSet_AfterClose(t *testing.T) {
	c := newClientWithRedis(&mockRedis{})
	_ = c.Close()

	record := makeRecord("key", model.IdempotencyStatusProcessing)
	err := c.Set(context.Background(), record, time.Minute)
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

func TestDelete_AfterClose(t *testing.T) {
	c := newClientWithRedis(&mockRedis{})
	_ = c.Close()

	err := c.Delete(context.Background(), "k")
	if err == nil {
		t.Fatal("expected error on Delete after Close")
	}
	if port.ErrorCode(err) != port.ErrCodeStorageFailed {
		t.Errorf("error code = %q, want %q", port.ErrorCode(err), port.ErrCodeStorageFailed)
	}
	if port.IsRetryable(err) {
		t.Error("use-after-close should be non-retryable")
	}
}

func TestPing_AfterClose(t *testing.T) {
	c := newClientWithRedis(&mockRedis{})
	_ = c.Close()

	err := c.Ping(context.Background())
	if err == nil {
		t.Fatal("expected error on Ping after Close")
	}
	if port.ErrorCode(err) != port.ErrCodeStorageFailed {
		t.Errorf("error code = %q, want %q", port.ErrorCode(err), port.ErrCodeStorageFailed)
	}
	if port.IsRetryable(err) {
		t.Error("use-after-close should be non-retryable")
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

func TestMapError_RedisNilDefensive(t *testing.T) {
	err := mapError(redis.Nil, "Test")
	if port.ErrorCode(err) != port.ErrCodeStorageFailed {
		t.Errorf("error code = %q, want %q", port.ErrorCode(err), port.ErrCodeStorageFailed)
	}
	if port.IsRetryable(err) {
		t.Error("redis.Nil defensive guard should be non-retryable")
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

// --- In-memory integration: Set → Get → Delete lifecycle ---

func TestSetGetDelete_InMemory(t *testing.T) {
	mock := newInMemoryRedis()
	c := newClientWithRedis(mock)
	ctx := context.Background()

	// 1. Get nonexistent key → nil, nil.
	got, err := c.Get(ctx, "dp-artifacts:job-abc")
	if err != nil {
		t.Fatalf("Get nonexistent: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil record, got: %+v", got)
	}

	// 2. Set a PROCESSING record.
	record := makeRecord("dp-artifacts:job-abc", model.IdempotencyStatusProcessing)
	if err := c.Set(ctx, record, 120*time.Second); err != nil {
		t.Fatalf("Set: %v", err)
	}

	// 3. Get returns the record.
	got, err = c.Get(ctx, "dp-artifacts:job-abc")
	if err != nil {
		t.Fatalf("Get after Set: %v", err)
	}
	if got == nil {
		t.Fatal("expected record, got nil")
	}
	if got.Key != "dp-artifacts:job-abc" {
		t.Errorf("Key = %q, want %q", got.Key, "dp-artifacts:job-abc")
	}
	if got.Status != model.IdempotencyStatusProcessing {
		t.Errorf("Status = %q, want %q", got.Status, model.IdempotencyStatusProcessing)
	}

	// 4. Update to COMPLETED with ResultSnapshot.
	record.MarkCompleted(`{"persisted":true}`)
	if err := c.Set(ctx, record, 24*time.Hour); err != nil {
		t.Fatalf("Set overwrite: %v", err)
	}

	// 5. Get returns updated record.
	got, err = c.Get(ctx, "dp-artifacts:job-abc")
	if err != nil {
		t.Fatalf("Get after overwrite: %v", err)
	}
	if got.Status != model.IdempotencyStatusCompleted {
		t.Errorf("Status = %q, want %q", got.Status, model.IdempotencyStatusCompleted)
	}
	if got.ResultSnapshot != `{"persisted":true}` {
		t.Errorf("ResultSnapshot = %q, want %q", got.ResultSnapshot, `{"persisted":true}`)
	}

	// 6. Delete.
	if err := c.Delete(ctx, "dp-artifacts:job-abc"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	// 7. Get returns nil, nil again.
	got, err = c.Get(ctx, "dp-artifacts:job-abc")
	if err != nil {
		t.Fatalf("Get after Delete: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil after delete, got: %+v", got)
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
			key := fmt.Sprintf("dp-artifacts:job-%d", n)
			record := makeRecord(key, model.IdempotencyStatusProcessing)

			if err := c.Set(ctx, record, time.Hour); err != nil {
				t.Errorf("Set(%s): %v", key, err)
			}
			got, err := c.Get(ctx, key)
			if err != nil {
				t.Errorf("Get(%s): %v", key, err)
			}
			if got == nil {
				t.Errorf("Get(%s): expected record, got nil", key)
			}
			if err := c.Delete(ctx, key); err != nil {
				t.Errorf("Delete(%s): %v", key, err)
			}
		}(i)
	}

	wg.Wait()
}
