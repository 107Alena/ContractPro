package fakes

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"testing"
	"time"

	"contractpro/legal-intelligence-core/internal/domain/port"
)

const idempotencyLuaForTest = `if redis.call('SET', KEYS[1], ARGV[1], 'NX', 'EX', ARGV[2]) then
  return {1, ''}
else
  return {0, redis.call('GET', KEYS[1])}
end`

const tokenBucketLuaForTest = `redis.replicate_commands()
local key = KEYS[1]
` // suffix doesn't matter — only the marker is used for dispatch

func TestGet_MissReturnsErrKeyNotFound(t *testing.T) {
	kv := NewFakeKVStore()
	if _, err := kv.Get(context.Background(), "absent"); !errors.Is(err, ErrKeyNotFound) {
		t.Fatalf("expected ErrKeyNotFound, got %v", err)
	}
}

func TestSet_Get_RoundTrip(t *testing.T) {
	kv := NewFakeKVStore()
	if err := kv.Set(context.Background(), "k", "v", time.Minute); err != nil {
		t.Fatal(err)
	}
	got, err := kv.Get(context.Background(), "k")
	if err != nil || got != "v" {
		t.Fatalf("get: got=%q err=%v", got, err)
	}
}

func TestSet_TTLZero_NoExpiry(t *testing.T) {
	kv := NewFakeKVStore()
	if err := kv.Set(context.Background(), "k", "v", 0); err != nil {
		t.Fatal(err)
	}
	if kv.TTL("k") != 0 {
		t.Fatalf("expected TTL=0 (no expiry), got %v", kv.TTL("k"))
	}
}

func TestSetNX_FirstWriterWins(t *testing.T) {
	kv := NewFakeKVStore()
	ok, err := kv.SetNX(context.Background(), "k", "first", time.Minute)
	if err != nil || !ok {
		t.Fatalf("first SetNX: ok=%v err=%v", ok, err)
	}
	ok, err = kv.SetNX(context.Background(), "k", "second", time.Minute)
	if err != nil || ok {
		t.Fatalf("second SetNX: ok=%v err=%v", ok, err)
	}
	got, _ := kv.Get(context.Background(), "k")
	if got != "first" {
		t.Fatalf("expected 'first', got %q", got)
	}
}

func TestSetNX_ExpiredKeyAllowsReSet(t *testing.T) {
	kv := NewFakeKVStore()
	_, _ = kv.SetNX(context.Background(), "k", "first", 10*time.Millisecond)
	time.Sleep(20 * time.Millisecond)
	ok, _ := kv.SetNX(context.Background(), "k", "second", time.Minute)
	if !ok {
		t.Fatal("expected second SetNX to win after expiry")
	}
	got, _ := kv.Get(context.Background(), "k")
	if got != "second" {
		t.Fatalf("got %q", got)
	}
}

func TestExpire_KeyExists(t *testing.T) {
	kv := NewFakeKVStore()
	_ = kv.Set(context.Background(), "k", "v", time.Hour)
	ok, err := kv.Expire(context.Background(), "k", 10*time.Millisecond)
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	time.Sleep(20 * time.Millisecond)
	if _, err := kv.Get(context.Background(), "k"); !errors.Is(err, ErrKeyNotFound) {
		t.Fatalf("expected expired, got %v", err)
	}
}

func TestExpire_KeyAbsent(t *testing.T) {
	kv := NewFakeKVStore()
	ok, err := kv.Expire(context.Background(), "absent", time.Minute)
	if err != nil || ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
}

func TestDelete_RemovesAndCountsLive(t *testing.T) {
	kv := NewFakeKVStore()
	_ = kv.Set(context.Background(), "a", "1", time.Minute)
	_ = kv.Set(context.Background(), "b", "2", time.Minute)
	n, err := kv.Delete(context.Background(), "a", "b", "c")
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("removed=%d want 2", n)
	}
}

func TestPing_ReturnsInjectedFIFO(t *testing.T) {
	kv := NewFakeKVStore()
	want := errors.New("ping-fail-1")
	kv.InjectPingError(want)
	if err := kv.Ping(context.Background()); err != want {
		t.Fatalf("first ping: got %v", err)
	}
	if err := kv.Ping(context.Background()); err != nil {
		t.Fatalf("second ping: got %v", err)
	}
}

func TestClose_BlocksSubsequentOps(t *testing.T) {
	kv := NewFakeKVStore()
	_ = kv.Close()
	if err := kv.Set(context.Background(), "k", "v", 0); err == nil {
		t.Fatal("expected closed error")
	}
}

func TestEval_UnknownScriptErrors(t *testing.T) {
	kv := NewFakeKVStore()
	_, err := kv.Eval(context.Background(), "return 1", nil)
	if !errors.Is(err, ErrUnknownLuaScript) {
		t.Fatalf("expected ErrUnknownLuaScript, got %v", err)
	}
}

func TestEval_IdempotencyLua_Acquired(t *testing.T) {
	kv := NewFakeKVStore()
	res, err := kv.Eval(context.Background(), idempotencyLuaForTest,
		[]string{"lic-trigger:v1"},
		string(port.IdempotencyProcessing), 150)
	if err != nil {
		t.Fatal(err)
	}
	arr, ok := res.([]interface{})
	if !ok || len(arr) != 2 {
		t.Fatalf("shape: %+v", res)
	}
	if code, _ := arr[0].(int64); code != 1 {
		t.Fatalf("expected acquired (1), got %v", arr[0])
	}
	if s, _ := arr[1].(string); s != "" {
		t.Fatalf("expected empty existing, got %q", s)
	}
}

func TestEval_IdempotencyLua_Present(t *testing.T) {
	kv := NewFakeKVStore()
	_, _ = kv.Eval(context.Background(), idempotencyLuaForTest,
		[]string{"lic-trigger:v1"},
		string(port.IdempotencyProcessing), 150)
	// Second call: present.
	res, err := kv.Eval(context.Background(), idempotencyLuaForTest,
		[]string{"lic-trigger:v1"},
		string(port.IdempotencyCompleted), 150)
	if err != nil {
		t.Fatal(err)
	}
	arr := res.([]interface{})
	if code, _ := arr[0].(int64); code != 0 {
		t.Fatalf("expected present (0), got %v", arr[0])
	}
	if s, _ := arr[1].(string); s != string(port.IdempotencyProcessing) {
		t.Fatalf("expected stored value, got %q", s)
	}
}

func TestEval_IdempotencyLua_TTLExpiresIndependently(t *testing.T) {
	kv := NewFakeKVStore()
	_, _ = kv.Eval(context.Background(), idempotencyLuaForTest,
		[]string{"k"},
		"PROCESSING", 1)
	time.Sleep(1500 * time.Millisecond)
	res, _ := kv.Eval(context.Background(), idempotencyLuaForTest,
		[]string{"k"},
		"PROCESSING", 5)
	arr := res.([]interface{})
	if code, _ := arr[0].(int64); code != 1 {
		t.Fatalf("expected re-acquired after TTL, got code=%v", code)
	}
}

func TestEval_TokenBucket_AllowsThenDenies(t *testing.T) {
	kv := NewFakeKVStore()
	rps := "1"   // 1 token/sec
	burst := "2" // capacity 2
	requested := "1"
	for i := 0; i < 2; i++ {
		res, err := kv.Eval(context.Background(), tokenBucketLuaForTest,
			[]string{"lic:rate:claude"}, rps, burst, requested)
		if err != nil {
			t.Fatalf("eval %d: %v", i, err)
		}
		arr := res.([]interface{})
		allowed, _ := arr[0].(int64)
		if allowed != 1 {
			t.Fatalf("call %d expected allow=1 got %d", i, allowed)
		}
	}
	// Third call within the same second should deny (capacity 2 used).
	res, _ := kv.Eval(context.Background(), tokenBucketLuaForTest,
		[]string{"lic:rate:claude"}, rps, burst, requested)
	arr := res.([]interface{})
	allowed, _ := arr[0].(int64)
	if allowed != 0 {
		t.Fatalf("third call expected deny, got allow=%d", allowed)
	}
	retryAfter, _ := arr[1].(int64)
	if retryAfter <= 0 {
		t.Fatalf("expected retryAfterMS>0, got %d", retryAfter)
	}
}

func TestEval_TokenBucket_PersistsStateAsHash(t *testing.T) {
	kv := NewFakeKVStore()
	_, _ = kv.Eval(context.Background(), tokenBucketLuaForTest,
		[]string{"lic:rate:openai"}, "10", "10", "1")

	kv.mu.Lock()
	defer kv.mu.Unlock()
	if _, ok := kv.hashes["lic:rate:openai"]; !ok {
		t.Fatal("expected hash persisted under bucket key")
	}
	if _, ok := kv.hashes["lic:rate:openai"].fields["tokens"]; !ok {
		t.Fatal("expected 'tokens' field")
	}
}

func TestEval_HonoursCtxError(t *testing.T) {
	kv := NewFakeKVStore()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := kv.Eval(ctx, idempotencyLuaForTest, []string{"k"}, "v", 1); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected Canceled, got %v", err)
	}
}

func TestReset_ClearsAllState(t *testing.T) {
	kv := NewFakeKVStore()
	_ = kv.Set(context.Background(), "a", "1", time.Minute)
	_, _ = kv.Eval(context.Background(), tokenBucketLuaForTest,
		[]string{"b"}, "10", "10", "1")
	kv.InjectPingError(errors.New("x"))
	kv.Reset()
	if kv.Size() != 0 {
		t.Fatalf("size=%d", kv.Size())
	}
	if err := kv.Ping(context.Background()); err != nil {
		t.Fatalf("ping after reset: %v", err)
	}
}

func TestEval_IdempotencyLua_TtlAsStringArg(t *testing.T) {
	kv := NewFakeKVStore()
	_, err := kv.Eval(context.Background(), idempotencyLuaForTest,
		[]string{"k"}, "PROCESSING", strconv.Itoa(150))
	if err != nil {
		t.Fatalf("string TTL: %v", err)
	}
}

func TestFakeKVStore_ConcurrentSetNXFairness(t *testing.T) {
	kv := NewFakeKVStore()
	const G = 32
	wins := make([]bool, G)
	var wg sync.WaitGroup
	for i := 0; i < G; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			ok, _ := kv.SetNX(context.Background(), "race", "p"+strconv.Itoa(i), time.Minute)
			wins[i] = ok
		}()
	}
	wg.Wait()
	n := 0
	for _, w := range wins {
		if w {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("expected exactly 1 winner, got %d", n)
	}
}
