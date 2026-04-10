package ratelimit

import (
	"context"
	"errors"
	"testing"
	"time"
)

// --- RedisStore tests ---

func TestRedisStore_Allow_ReturnsTrue(t *testing.T) {
	store := newRedisStoreWithEval(
		func(_ context.Context, _ string, _ []string, _ ...interface{}) (int64, error) {
			return 1, nil // Lua script returns 1 = allowed
		},
	)

	allowed, err := store.Allow(context.Background(), "rl:org-1:read", 200, time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !allowed {
		t.Fatal("expected allowed=true")
	}
}

func TestRedisStore_Allow_ReturnsFalse(t *testing.T) {
	store := newRedisStoreWithEval(
		func(_ context.Context, _ string, _ []string, _ ...interface{}) (int64, error) {
			return 0, nil // Lua script returns 0 = rejected
		},
	)

	allowed, err := store.Allow(context.Background(), "rl:org-1:read", 200, time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if allowed {
		t.Fatal("expected allowed=false")
	}
}

func TestRedisStore_Allow_RedisError(t *testing.T) {
	redisErr := errors.New("LOADING Redis is loading the dataset in memory")
	store := newRedisStoreWithEval(
		func(_ context.Context, _ string, _ []string, _ ...interface{}) (int64, error) {
			return 0, redisErr
		},
	)

	_, err := store.Allow(context.Background(), "rl:org-1:read", 200, time.Second)
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, redisErr) {
		t.Errorf("expected wrapped Redis error, got: %v", err)
	}
}

func TestRedisStore_Allow_CorrectArgs(t *testing.T) {
	var capturedScript string
	var capturedKeys []string
	var capturedArgs []interface{}

	store := newRedisStoreWithEval(
		func(_ context.Context, script string, keys []string, args ...interface{}) (int64, error) {
			capturedScript = script
			capturedKeys = keys
			capturedArgs = args
			return 1, nil
		},
	)

	_, _ = store.Allow(context.Background(), "rl:org-1:read", 200, time.Second)

	if capturedScript != luaAllowScript {
		t.Error("expected Lua script to be passed")
	}

	if len(capturedKeys) != 1 || capturedKeys[0] != "rl:org-1:read" {
		t.Errorf("expected keys=[rl:org-1:read], got %v", capturedKeys)
	}

	if len(capturedArgs) != 2 {
		t.Fatalf("expected 2 args, got %d", len(capturedArgs))
	}

	// First arg: limit (int)
	if limit, ok := capturedArgs[0].(int); !ok || limit != 200 {
		t.Errorf("expected arg[0]=200 (int), got %v (%T)", capturedArgs[0], capturedArgs[0])
	}

	// Second arg: window in milliseconds (int64)
	if windowMs, ok := capturedArgs[1].(int64); !ok || windowMs != 1000 {
		t.Errorf("expected arg[1]=1000 (int64), got %v (%T)", capturedArgs[1], capturedArgs[1])
	}
}

func TestRedisStore_Allow_ContextForwarded(t *testing.T) {
	type testKey struct{}
	ctx := context.WithValue(context.Background(), testKey{}, "test-value")

	var capturedCtx context.Context
	store := newRedisStoreWithEval(
		func(c context.Context, _ string, _ []string, _ ...interface{}) (int64, error) {
			capturedCtx = c
			return 1, nil
		},
	)

	_, _ = store.Allow(ctx, "rl:org-1:read", 200, time.Second)

	if capturedCtx.Value(testKey{}) != "test-value" {
		t.Error("expected context to be forwarded")
	}
}

func TestRedisStore_Allow_WindowMilliseconds(t *testing.T) {
	var capturedArgs []interface{}
	store := newRedisStoreWithEval(
		func(_ context.Context, _ string, _ []string, args ...interface{}) (int64, error) {
			capturedArgs = args
			return 1, nil
		},
	)

	// Test with 500ms window
	_, _ = store.Allow(context.Background(), "rl:org-1:read", 100, 500*time.Millisecond)

	if len(capturedArgs) < 2 {
		t.Fatalf("expected 2 args, got %d", len(capturedArgs))
	}
	if windowMs, ok := capturedArgs[1].(int64); !ok || windowMs != 500 {
		t.Errorf("expected window=500ms, got %v", capturedArgs[1])
	}
}

// --- compile-time interface check ---

func TestRedisStore_ImplementsRateLimiterStore(t *testing.T) {
	var _ RateLimiterStore = (*RedisStore)(nil)
}
