package kvstore

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"contractpro/legal-intelligence-core/internal/config"
)

func TestNewClient_InvalidURLFailsFast(t *testing.T) {
	// Deterministic, no network: ParseURL rejects the scheme locally.
	_, err := NewClient(config.RedisConfig{
		URL:         "http://nope",
		PoolSize:    10,
		DialTimeout: time.Second,
	})
	if err == nil {
		t.Fatal("expected NewClient to fail on invalid URL")
	}
	var re *RedisError
	if !errors.As(err, &re) || re.Op != "ParseURL" {
		t.Fatalf("want *RedisError{Op:ParseURL}, got %T %v", err, err)
	}
}

func TestPing_Success(t *testing.T) {
	c := newClientWithRedis(&mockRedis{})
	if err := c.Ping(context.Background()); err != nil {
		t.Fatalf("Ping: %v", err)
	}
}

func TestPing_ContextAlreadyCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	called := false
	c := newClientWithRedis(&mockRedis{
		pingFn: func(context.Context) *redis.StatusCmd {
			called = true
			return redis.NewStatusResult("PONG", nil)
		},
	})
	err := c.Ping(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("want context.Canceled passthrough, got %v", err)
	}
	if called {
		t.Error("Ping must short-circuit on a dead context before hitting Redis")
	}
}

func TestPing_Error(t *testing.T) {
	c := newClientWithRedis(&mockRedis{
		pingFn: func(context.Context) *redis.StatusCmd {
			return redis.NewStatusResult("", errors.New("connection refused"))
		},
	})
	err := c.Ping(context.Background())
	if !IsRetryable(err) {
		t.Errorf("want retryable RedisError, got %v", err)
	}
}

func TestPing_AfterClose(t *testing.T) {
	c := newClientWithRedis(&mockRedis{})
	_ = c.Close()
	err := c.Ping(context.Background())
	if err == nil || IsRetryable(err) {
		t.Errorf("Ping after Close must be a non-retryable error, got %v", err)
	}
}

func TestClose_GracefulAndIdempotent(t *testing.T) {
	count := 0
	c := newClientWithRedis(&mockRedis{
		closeFn: func() error { count++; return nil },
	})
	if err := c.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	if count != 1 {
		t.Errorf("underlying Close called %d times, want 1 (idempotent)", count)
	}
}

func TestClose_MapsUnderlyingError(t *testing.T) {
	c := newClientWithRedis(&mockRedis{
		closeFn: func() error { return errors.New("pool drain failed") },
	})
	err := c.Close()
	var re *RedisError
	if !errors.As(err, &re) || re.Op != "Close" {
		t.Errorf("want *RedisError{Op:Close}, got %v", err)
	}
}

func TestClose_Concurrent(t *testing.T) {
	count := 0
	var mu sync.Mutex
	c := newClientWithRedis(&mockRedis{
		closeFn: func() error { mu.Lock(); count++; mu.Unlock(); return nil },
	})

	var wg sync.WaitGroup
	for i := 0; i < 25; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _ = c.Close() }()
	}
	wg.Wait()
	if count != 1 {
		t.Errorf("underlying Close called %d times under concurrency, want 1", count)
	}
}
