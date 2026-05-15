package kvstore

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

// --- Get -------------------------------------------------------------------

func TestGet_Success(t *testing.T) {
	f := newFakeRedis()
	c := newClientWithRedis(f)
	_ = c.Set(context.Background(), "k", "v", time.Hour)

	got, err := c.Get(context.Background(), "k")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != "v" {
		t.Errorf("Get = %q, want v", got)
	}
}

func TestGet_KeyNotFound(t *testing.T) {
	c := newClientWithRedis(newFakeRedis())
	_, err := c.Get(context.Background(), "missing")
	if !errors.Is(err, ErrKeyNotFound) {
		t.Errorf("want ErrKeyNotFound, got %v", err)
	}
	var re *RedisError
	if errors.As(err, &re) {
		t.Error("ErrKeyNotFound must be a plain sentinel, not *RedisError")
	}
}

func TestGet_ContextPassthrough(t *testing.T) {
	c := newClientWithRedis(&mockRedis{
		getFn: func(context.Context, string) *redis.StringCmd {
			return redis.NewStringResult("", context.DeadlineExceeded)
		},
	})
	_, err := c.Get(context.Background(), "k")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("want context.DeadlineExceeded passthrough, got %v", err)
	}
}

func TestGet_RedisErrorRetryable(t *testing.T) {
	c := newClientWithRedis(&mockRedis{
		getFn: func(context.Context, string) *redis.StringCmd {
			return redis.NewStringResult("", errors.New("connection reset"))
		},
	})
	_, err := c.Get(context.Background(), "k")
	if !IsRetryable(err) {
		t.Errorf("want retryable RedisError, got %v", err)
	}
}

// --- Set -------------------------------------------------------------------

func TestSet_ForwardsKeyValueTTL(t *testing.T) {
	var gotK string
	var gotV any
	var gotTTL time.Duration
	c := newClientWithRedis(&mockRedis{
		setFn: func(_ context.Context, k string, v any, ttl time.Duration) *redis.StatusCmd {
			gotK, gotV, gotTTL = k, v, ttl
			return redis.NewStatusResult("OK", nil)
		},
	})
	if err := c.Set(context.Background(), "lic-pending-state:42", "blob", 25*time.Hour); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if gotK != "lic-pending-state:42" || gotV != "blob" || gotTTL != 25*time.Hour {
		t.Errorf("forwarded (%q,%v,%v)", gotK, gotV, gotTTL)
	}
}

func TestSet_ZeroTTLNoExpiry(t *testing.T) {
	var gotTTL time.Duration = -1
	c := newClientWithRedis(&mockRedis{
		setFn: func(_ context.Context, _ string, _ any, ttl time.Duration) *redis.StatusCmd {
			gotTTL = ttl
			return redis.NewStatusResult("OK", nil)
		},
	})
	_ = c.Set(context.Background(), "k", "v", 0)
	if gotTTL != 0 {
		t.Errorf("ttl forwarded = %v, want 0", gotTTL)
	}
}

func TestSet_ErrorMapped(t *testing.T) {
	c := newClientWithRedis(&mockRedis{
		setFn: func(context.Context, string, any, time.Duration) *redis.StatusCmd {
			return redis.NewStatusResult("", errors.New("OOM"))
		},
	})
	err := c.Set(context.Background(), "k", "v", time.Minute)
	var re *RedisError
	if !errors.As(err, &re) || re.Op != "Set" {
		t.Errorf("want *RedisError{Op:Set}, got %v", err)
	}
}

// --- SetNX (acceptance test step 2) ----------------------------------------

func TestSetNX_FirstWriterWins_DuplicateReturnsFalse(t *testing.T) {
	c := newClientWithRedis(newFakeRedis())
	ctx := context.Background()

	ok, err := c.SetNX(ctx, "lic-trigger:v1", "PROCESSING", 150*time.Second)
	if err != nil || !ok {
		t.Fatalf("first SetNX = (%v,%v), want (true,nil)", ok, err)
	}

	ok, err = c.SetNX(ctx, "lic-trigger:v1", "PROCESSING", 150*time.Second)
	if err != nil {
		t.Fatalf("dup SetNX err: %v", err)
	}
	if ok {
		t.Error("duplicate SetNX with same key must return false")
	}
}

func TestSetNX_ErrorMapped(t *testing.T) {
	c := newClientWithRedis(&mockRedis{
		setNXFn: func(context.Context, string, any, time.Duration) *redis.BoolCmd {
			return redis.NewBoolResult(false, errors.New("loading"))
		},
	})
	_, err := c.SetNX(context.Background(), "k", "v", time.Minute)
	if !IsRetryable(err) {
		t.Errorf("want retryable RedisError, got %v", err)
	}
}

// --- Delete ----------------------------------------------------------------

func TestDelete_CountsRemoved(t *testing.T) {
	f := newFakeRedis()
	c := newClientWithRedis(f)
	ctx := context.Background()
	_ = c.Set(ctx, "a", "1", time.Hour)
	_ = c.Set(ctx, "b", "2", time.Hour)

	n, err := c.Delete(ctx, "a", "b", "missing")
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if n != 2 {
		t.Errorf("Delete removed %d, want 2 (missing key is not an error)", n)
	}
}

func TestDelete_ErrorMapped(t *testing.T) {
	c := newClientWithRedis(&mockRedis{
		delFn: func(context.Context, ...string) *redis.IntCmd {
			return redis.NewIntResult(0, errors.New("down"))
		},
	})
	_, err := c.Delete(context.Background(), "k")
	var re *RedisError
	if !errors.As(err, &re) || re.Op != "Delete" {
		t.Errorf("want *RedisError{Op:Delete}, got %v", err)
	}
}

// --- Expire ----------------------------------------------------------------

func TestExpire_KeyPresentTrue(t *testing.T) {
	f := newFakeRedis()
	c := newClientWithRedis(f)
	ctx := context.Background()
	_ = c.Set(ctx, "lic-trigger:v1", "PROCESSING", 150*time.Second)

	ok, err := c.Expire(ctx, "lic-trigger:v1", 150*time.Second)
	if err != nil || !ok {
		t.Fatalf("Expire on existing key = (%v,%v), want (true,nil)", ok, err)
	}
}

func TestExpire_MissingKeyFalse(t *testing.T) {
	c := newClientWithRedis(newFakeRedis())
	ok, err := c.Expire(context.Background(), "gone", time.Minute)
	if err != nil {
		t.Fatalf("Expire: %v", err)
	}
	if ok {
		t.Error("Expire on missing key must return false (heartbeat stop signal)")
	}
}

func TestExpire_ErrorMapped(t *testing.T) {
	c := newClientWithRedis(&mockRedis{
		expireFn: func(context.Context, string, time.Duration) *redis.BoolCmd {
			return redis.NewBoolResult(false, errors.New("net"))
		},
	})
	_, err := c.Expire(context.Background(), "k", time.Minute)
	if !IsRetryable(err) {
		t.Errorf("want retryable RedisError, got %v", err)
	}
}

// --- Eval (acceptance test step 3) -----------------------------------------

const tokenBucketScript = `
local tokens = tonumber(redis.call("GET", KEYS[1]) or ARGV[1])
if tokens > 0 then
  redis.call("SET", KEYS[1], tokens - 1)
  return 1
end
return 0`

// EVALSHA misses (cold), redis.Script.Run falls back to EVAL with the script
// source; assert the exact source / KEYS / ARGV reached Redis and the result
// decodes.
func TestEval_NoScriptFallbackDispatchContract(t *testing.T) {
	f := newFakeRedis()
	f.evalRet = int64(1)
	c := newClientWithRedis(f)

	res, err := c.Eval(context.Background(), tokenBucketScript,
		[]string{"lic:rate:claude"}, 100, "ctx")
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	if res != int64(1) {
		t.Errorf("Eval result = %v, want int64(1)", res)
	}

	if len(f.evalCalls) != 2 {
		t.Fatalf("want EVALSHA then EVAL (2 calls), got %d: %+v", len(f.evalCalls), f.evalCalls)
	}
	if f.evalCalls[0].method != "EvalSha" || f.evalCalls[0].arg != sha1Hex(tokenBucketScript) {
		t.Errorf("first call must be EvalSha with SHA1(src), got %+v", f.evalCalls[0])
	}
	ev := f.evalCalls[1]
	if ev.method != "Eval" || ev.arg != tokenBucketScript {
		t.Errorf("fallback must be Eval with raw source, got method=%s", ev.method)
	}
	if !reflect.DeepEqual(ev.keys, []string{"lic:rate:claude"}) {
		t.Errorf("KEYS = %v, want [lic:rate:claude]", ev.keys)
	}
	if !reflect.DeepEqual(ev.args, []any{100, "ctx"}) {
		t.Errorf("ARGV = %v, want [100 ctx]", ev.args)
	}
}

// When the script is already cached server-side, EVALSHA succeeds and EVAL is
// never sent.
func TestEval_EvalShaHitNoFallback(t *testing.T) {
	f := newFakeRedis()
	f.evalShaHit = true
	f.evalRet = int64(0)
	c := newClientWithRedis(f)

	res, err := c.Eval(context.Background(), tokenBucketScript, []string{"lic:rate:openai"}, 10)
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	if res != int64(0) {
		t.Errorf("result = %v, want int64(0)", res)
	}
	if len(f.evalCalls) != 1 || f.evalCalls[0].method != "EvalSha" {
		t.Fatalf("want a single EvalSha call, got %+v", f.evalCalls)
	}
}

// The per-source *redis.Script is cached: a second Eval of the same source
// reuses the same handle (code-architect Q3 hot-path concern).
func TestEval_ScriptHandleCachedPerSource(t *testing.T) {
	c := newClientWithRedis(newFakeRedis())
	s1 := c.scriptFor(tokenBucketScript)
	s2 := c.scriptFor(tokenBucketScript)
	if s1 != s2 {
		t.Error("scriptFor must return the same cached *redis.Script for identical source")
	}
	if s1.Hash() != sha1Hex(tokenBucketScript) {
		t.Errorf("cached script hash = %s, want SHA1(src)", s1.Hash())
	}
}

func TestEval_ScriptNilResult(t *testing.T) {
	f := newFakeRedis()
	f.evalShaHit = true
	f.evalErr = redis.Nil // Lua returned nil
	c := newClientWithRedis(f)

	res, err := c.Eval(context.Background(), "return nil", nil)
	if err != nil {
		t.Errorf("Lua nil must surface as (nil,nil), got err %v", err)
	}
	if res != nil {
		t.Errorf("result = %v, want nil", res)
	}
}

func TestEval_ErrorMapped(t *testing.T) {
	f := newFakeRedis()
	f.evalShaHit = true
	f.evalErr = errors.New("script bug")
	c := newClientWithRedis(f)

	_, err := c.Eval(context.Background(), "return 1", nil)
	var re *RedisError
	if !errors.As(err, &re) || re.Op != "Eval" {
		t.Errorf("want *RedisError{Op:Eval}, got %v", err)
	}
}

// --- use-after-close guard (all ops) ---------------------------------------

func TestOps_AfterClose(t *testing.T) {
	c := newClientWithRedis(&mockRedis{})
	_ = c.Close()
	ctx := context.Background()

	checks := []struct {
		name string
		err  error
	}{
		{"Get", func() error { _, e := c.Get(ctx, "k"); return e }()},
		{"Set", c.Set(ctx, "k", "v", time.Minute)},
		{"SetNX", func() error { _, e := c.SetNX(ctx, "k", "v", time.Minute); return e }()},
		{"Delete", func() error { _, e := c.Delete(ctx, "k"); return e }()},
		{"Expire", func() error { _, e := c.Expire(ctx, "k", time.Minute); return e }()},
		{"Eval", func() error { _, e := c.Eval(ctx, "return 1", nil); return e }()},
	}
	for _, ch := range checks {
		if ch.err == nil {
			t.Errorf("%s after Close must error", ch.name)
			continue
		}
		if IsRetryable(ch.err) {
			t.Errorf("%s after Close must be non-retryable, got %v", ch.name, ch.err)
		}
	}
}

// --- concurrency (race detector) -------------------------------------------

func TestConcurrentAccess(t *testing.T) {
	c := newClientWithRedis(newFakeRedis())
	ctx := context.Background()

	const n = 50
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			k := fmt.Sprintf("job:%d", i)
			if err := c.Set(ctx, k, "v", time.Hour); err != nil {
				t.Errorf("Set: %v", err)
			}
			if _, err := c.Get(ctx, k); err != nil {
				t.Errorf("Get: %v", err)
			}
			if _, err := c.SetNX(ctx, k+":nx", "v", time.Hour); err != nil {
				t.Errorf("SetNX: %v", err)
			}
			if _, err := c.Expire(ctx, k, time.Minute); err != nil {
				t.Errorf("Expire: %v", err)
			}
			if _, err := c.Eval(ctx, "return 1", []string{k}); err != nil {
				t.Errorf("Eval: %v", err)
			}
			if _, err := c.Delete(ctx, k); err != nil {
				t.Errorf("Delete: %v", err)
			}
		}(i)
	}
	wg.Wait()
}
