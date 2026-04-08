package kvstore

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

// --- mock ---

type mockRedis struct {
	getFn       func(ctx context.Context, key string) *redis.StringCmd
	setFn       func(ctx context.Context, key string, value interface{}, expiration time.Duration) *redis.StatusCmd
	delFn       func(ctx context.Context, keys ...string) *redis.IntCmd
	publishFn   func(ctx context.Context, channel string, message interface{}) *redis.IntCmd
	subscribeFn func(ctx context.Context, channels ...string) *redis.PubSub
	pingFn      func(ctx context.Context) *redis.StatusCmd
	closeFn     func() error
}

func (m *mockRedis) Get(ctx context.Context, key string) *redis.StringCmd {
	if m.getFn != nil {
		return m.getFn(ctx, key)
	}
	return redis.NewStringResult("", redis.Nil)
}

func (m *mockRedis) Set(ctx context.Context, key string, value interface{}, expiration time.Duration) *redis.StatusCmd {
	if m.setFn != nil {
		return m.setFn(ctx, key, value, expiration)
	}
	return redis.NewStatusResult("OK", nil)
}

func (m *mockRedis) Del(ctx context.Context, keys ...string) *redis.IntCmd {
	if m.delFn != nil {
		return m.delFn(ctx, keys...)
	}
	return redis.NewIntResult(0, nil)
}

func (m *mockRedis) Publish(ctx context.Context, channel string, message interface{}) *redis.IntCmd {
	if m.publishFn != nil {
		return m.publishFn(ctx, channel, message)
	}
	return redis.NewIntResult(0, nil)
}

func (m *mockRedis) Subscribe(ctx context.Context, channels ...string) *redis.PubSub {
	if m.subscribeFn != nil {
		return m.subscribeFn(ctx, channels...)
	}
	return &redis.PubSub{}
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
			var deleted int64
			for _, k := range keys {
				if _, ok := store[k]; ok {
					delete(store, k)
					deleted++
				}
			}
			mu.Unlock()
			return redis.NewIntResult(deleted, nil)
		},
		publishFn: func(_ context.Context, _ string, _ interface{}) *redis.IntCmd {
			return redis.NewIntResult(0, nil)
		},
		closeFn: func() error { return nil },
		pingFn: func(_ context.Context) *redis.StatusCmd {
			return redis.NewStatusResult("PONG", nil)
		},
	}
}

// --- interface compliance ---

var _ RedisAPI = (*mockRedis)(nil)

// --- Error mapping tests ---

func TestMapError_ContextCanceled(t *testing.T) {
	err := mapError(context.Canceled, "Test")
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled passthrough, got: %v", err)
	}
}

func TestMapError_ContextDeadlineExceeded(t *testing.T) {
	err := mapError(context.DeadlineExceeded, "Test")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("expected context.DeadlineExceeded passthrough, got: %v", err)
	}
}

func TestMapError_RedisNil(t *testing.T) {
	err := mapError(redis.Nil, "Test")
	if !errors.Is(err, ErrKeyNotFound) {
		t.Errorf("expected ErrKeyNotFound, got: %v", err)
	}
}

func TestMapError_UnknownError(t *testing.T) {
	origErr := errors.New("something broke")
	err := mapError(origErr, "Set")
	if !strings.Contains(err.Error(), "kvstore: Set:") {
		t.Errorf("expected wrapped error with operation prefix, got: %v", err)
	}
	if !errors.Is(err, origErr) {
		t.Error("expected original error to be unwrappable")
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
	if !strings.Contains(err.Error(), "kvstore: Get:") {
		t.Errorf("expected wrapped error, got: %v", err)
	}
}

func TestGet_ContextCancelled(t *testing.T) {
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

func TestGet_AfterClose(t *testing.T) {
	c := newClientWithRedis(&mockRedis{})
	_ = c.Close()

	_, err := c.Get(context.Background(), "k")
	if err == nil {
		t.Fatal("expected error on Get after Close")
	}
	if !errors.Is(err, ErrClientClosed) {
		t.Errorf("expected ErrClientClosed, got: %v", err)
	}
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

	c := newClientWithRedis(mock)
	err := c.Set(context.Background(), "upload:abc", "pending", 24*time.Hour)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotKey != "upload:abc" {
		t.Errorf("key = %q, want %q", gotKey, "upload:abc")
	}
	if gotValue != "pending" {
		t.Errorf("value = %v, want %q", gotValue, "pending")
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
	if !strings.Contains(err.Error(), "kvstore: Set:") {
		t.Errorf("expected wrapped error, got: %v", err)
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
}

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

func TestSet_AfterClose(t *testing.T) {
	c := newClientWithRedis(&mockRedis{})
	_ = c.Close()

	err := c.Set(context.Background(), "k", "v", time.Minute)
	if err == nil {
		t.Fatal("expected error on Set after Close")
	}
	if !errors.Is(err, ErrClientClosed) {
		t.Errorf("expected ErrClientClosed, got: %v", err)
	}
}

// --- Delete tests ---

func TestDelete_Success(t *testing.T) {
	var gotKeys []string
	mock := &mockRedis{
		delFn: func(_ context.Context, keys ...string) *redis.IntCmd {
			gotKeys = keys
			return redis.NewIntResult(1, nil)
		},
	}

	c := newClientWithRedis(mock)
	err := c.Delete(context.Background(), "upload:abc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(gotKeys) != 1 || gotKeys[0] != "upload:abc" {
		t.Errorf("keys = %v, want [\"upload:abc\"]", gotKeys)
	}
}

func TestDelete_NonExistentKey(t *testing.T) {
	mock := &mockRedis{
		delFn: func(_ context.Context, _ ...string) *redis.IntCmd {
			return redis.NewIntResult(0, nil) // 0 keys deleted
		},
	}

	c := newClientWithRedis(mock)
	err := c.Delete(context.Background(), "nonexistent")
	if err != nil {
		t.Fatalf("expected nil error for non-existent key, got: %v", err)
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
	if !strings.Contains(err.Error(), "kvstore: Delete:") {
		t.Errorf("expected wrapped error, got: %v", err)
	}
}

func TestDelete_ContextCancelled(t *testing.T) {
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

func TestDelete_AfterClose(t *testing.T) {
	c := newClientWithRedis(&mockRedis{})
	_ = c.Close()

	err := c.Delete(context.Background(), "k")
	if err == nil {
		t.Fatal("expected error on Delete after Close")
	}
	if !errors.Is(err, ErrClientClosed) {
		t.Errorf("expected ErrClientClosed, got: %v", err)
	}
}

// --- Publish tests ---

func TestPublish_Success(t *testing.T) {
	var gotChannel string
	var gotMessage interface{}

	mock := &mockRedis{
		publishFn: func(_ context.Context, channel string, message interface{}) *redis.IntCmd {
			gotChannel = channel
			gotMessage = message
			return redis.NewIntResult(2, nil)
		},
	}

	c := newClientWithRedis(mock)
	err := c.Publish(context.Background(), "sse:broadcast:org123", `{"status":"READY"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotChannel != "sse:broadcast:org123" {
		t.Errorf("channel = %q, want %q", gotChannel, "sse:broadcast:org123")
	}
	if gotMessage != `{"status":"READY"}` {
		t.Errorf("message = %v, want %q", gotMessage, `{"status":"READY"}`)
	}
}

func TestPublish_RedisError(t *testing.T) {
	mock := &mockRedis{
		publishFn: func(_ context.Context, _ string, _ interface{}) *redis.IntCmd {
			return redis.NewIntResult(0, errors.New("connection refused"))
		},
	}

	c := newClientWithRedis(mock)
	err := c.Publish(context.Background(), "ch", "msg")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "kvstore: Publish:") {
		t.Errorf("expected wrapped error, got: %v", err)
	}
}

func TestPublish_ContextCancelled(t *testing.T) {
	mock := &mockRedis{
		publishFn: func(_ context.Context, _ string, _ interface{}) *redis.IntCmd {
			return redis.NewIntResult(0, context.Canceled)
		},
	}

	c := newClientWithRedis(mock)
	err := c.Publish(context.Background(), "ch", "msg")
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got: %v", err)
	}
}

func TestPublish_ContextForwarding(t *testing.T) {
	type ctxKey struct{}
	expectedVal := "test-value"

	mock := &mockRedis{
		publishFn: func(ctx context.Context, _ string, _ interface{}) *redis.IntCmd {
			if v := ctx.Value(ctxKey{}); v != expectedVal {
				t.Errorf("context value = %v, want %q", v, expectedVal)
			}
			return redis.NewIntResult(0, nil)
		},
	}

	ctx := context.WithValue(context.Background(), ctxKey{}, expectedVal)
	c := newClientWithRedis(mock)
	_ = c.Publish(ctx, "ch", "msg")
}

func TestPublish_AfterClose(t *testing.T) {
	c := newClientWithRedis(&mockRedis{})
	_ = c.Close()

	err := c.Publish(context.Background(), "ch", "msg")
	if err == nil {
		t.Fatal("expected error on Publish after Close")
	}
	if !errors.Is(err, ErrClientClosed) {
		t.Errorf("expected ErrClientClosed, got: %v", err)
	}
}

// --- Subscribe tests ---

func TestSubscribe_AfterClose(t *testing.T) {
	c := newClientWithRedis(&mockRedis{})
	_ = c.Close()

	_, err := c.Subscribe(context.Background(), "ch", func(_ string) {})
	if err == nil {
		t.Fatal("expected error on Subscribe after Close")
	}
	if !errors.Is(err, ErrClientClosed) {
		t.Errorf("expected ErrClientClosed, got: %v", err)
	}
}

// --- Subscription lifecycle tests (via helper) ---

// testSubscription creates a Subscription with a fake channel for testing
// the delivery goroutine and Close behavior without a real Redis connection.
func testSubscription(handler func(msg string)) (*Subscription, chan<- string) {
	msgCh := make(chan string, 10)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	sub := &Subscription{
		pubsub: nil,
		cancel: cancel,
		done:   done,
	}

	go func() {
		defer close(done)
		for {
			select {
			case msg, ok := <-msgCh:
				if !ok {
					return
				}
				handler(msg)
			case <-ctx.Done():
				return
			}
		}
	}()

	return sub, msgCh
}

func TestSubscription_DeliversMessages(t *testing.T) {
	var received []string
	var mu sync.Mutex

	sub, ch := testSubscription(func(msg string) {
		mu.Lock()
		received = append(received, msg)
		mu.Unlock()
	})

	ch <- "msg1"
	ch <- "msg2"
	ch <- "msg3"

	// Allow goroutine to process.
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	if len(received) != 3 {
		t.Fatalf("received %d messages, want 3", len(received))
	}
	for i, want := range []string{"msg1", "msg2", "msg3"} {
		if received[i] != want {
			t.Errorf("received[%d] = %q, want %q", i, received[i], want)
		}
	}

	_ = sub.Close()
}

func TestSubscription_CloseStopsDelivery(t *testing.T) {
	deliverCount := 0
	var mu sync.Mutex

	sub, ch := testSubscription(func(_ string) {
		mu.Lock()
		deliverCount++
		mu.Unlock()
	})

	ch <- "before-close"
	time.Sleep(50 * time.Millisecond)

	_ = sub.Close()

	// Send after close — should not be delivered.
	select {
	case ch <- "after-close":
	default:
	}

	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if deliverCount != 1 {
		t.Errorf("deliverCount = %d, want 1", deliverCount)
	}
}

func TestSubscription_CloseIdempotent(t *testing.T) {
	sub, _ := testSubscription(func(_ string) {})

	err1 := sub.Close()
	err2 := sub.Close()

	// Both calls should succeed without panic.
	if err1 != nil {
		t.Errorf("first Close: %v", err1)
	}
	if err2 != nil {
		t.Errorf("second Close: %v", err2)
	}
}

func TestSubscription_ContextCancelStops(t *testing.T) {
	msgCh := make(chan string, 10)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	deliverCount := 0
	var mu sync.Mutex

	sub := &Subscription{
		pubsub: nil,
		cancel: cancel,
		done:   done,
	}

	go func() {
		defer close(done)
		for {
			select {
			case msg, ok := <-msgCh:
				if !ok {
					return
				}
				mu.Lock()
				deliverCount++
				mu.Unlock()
				_ = msg
			case <-ctx.Done():
				return
			}
		}
	}()

	msgCh <- "before"
	time.Sleep(50 * time.Millisecond)

	cancel() // Cancel the context.

	// Wait for goroutine to exit.
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("goroutine did not exit after context cancel")
	}

	mu.Lock()
	defer mu.Unlock()
	if deliverCount != 1 {
		t.Errorf("deliverCount = %d, want 1", deliverCount)
	}

	// sub.Close should still be safe.
	_ = sub.Close()
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
	if !strings.Contains(err.Error(), "kvstore: Ping:") {
		t.Errorf("expected wrapped error, got: %v", err)
	}
}

func TestPing_AfterClose(t *testing.T) {
	c := newClientWithRedis(&mockRedis{})
	_ = c.Close()

	err := c.Ping(context.Background())
	if err == nil {
		t.Fatal("expected error on Ping after Close")
	}
	if !errors.Is(err, ErrClientClosed) {
		t.Errorf("expected ErrClientClosed, got: %v", err)
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
			return errors.New("pool shutdown failed")
		},
	}

	c := newClientWithRedis(mock)
	err := c.Close()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "kvstore: Close:") {
		t.Errorf("expected wrapped error, got: %v", err)
	}
}

// --- In-memory integration: Set → Get → Delete → Get ---

func TestSetGetDelete_InMemory(t *testing.T) {
	mock := newInMemoryRedis()
	c := newClientWithRedis(mock)
	ctx := context.Background()

	// Key should not exist initially.
	_, err := c.Get(ctx, "upload:abc")
	if !errors.Is(err, ErrKeyNotFound) {
		t.Fatalf("Get non-existent: expected ErrKeyNotFound, got: %v", err)
	}

	// Set the key.
	err = c.Set(ctx, "upload:abc", "pending", time.Hour)
	if err != nil {
		t.Fatalf("Set: %v", err)
	}

	// Get should return the value.
	val, err := c.Get(ctx, "upload:abc")
	if err != nil {
		t.Fatalf("Get after Set: %v", err)
	}
	if val != "pending" {
		t.Errorf("value = %q, want %q", val, "pending")
	}

	// Overwrite with new value.
	err = c.Set(ctx, "upload:abc", "completed", time.Hour)
	if err != nil {
		t.Fatalf("Set overwrite: %v", err)
	}
	val, err = c.Get(ctx, "upload:abc")
	if err != nil {
		t.Fatalf("Get after overwrite: %v", err)
	}
	if val != "completed" {
		t.Errorf("value = %q, want %q", val, "completed")
	}

	// Delete the key.
	err = c.Delete(ctx, "upload:abc")
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}

	// Get should return ErrKeyNotFound.
	_, err = c.Get(ctx, "upload:abc")
	if !errors.Is(err, ErrKeyNotFound) {
		t.Fatalf("Get after Delete: expected ErrKeyNotFound, got: %v", err)
	}

	// Delete again should be idempotent.
	err = c.Delete(ctx, "upload:abc")
	if err != nil {
		t.Fatalf("Delete idempotent: %v", err)
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
			key := fmt.Sprintf("upload:%d", n)
			if err := c.Set(ctx, key, "value", time.Hour); err != nil {
				t.Errorf("Set(%s): %v", key, err)
			}
			if _, err := c.Get(ctx, key); err != nil {
				t.Errorf("Get(%s): %v", key, err)
			}
			if err := c.Delete(ctx, key); err != nil {
				t.Errorf("Delete(%s): %v", key, err)
			}
		}(i)
	}

	wg.Wait()
}
