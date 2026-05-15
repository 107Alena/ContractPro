package kvstore

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// Test doubles. miniredis is absent from the offline module cache and the
// network is unavailable, so — exactly as the internal/infra/broker package
// shipped with in-memory AMQP fakes instead of a live broker — kvstore is
// tested against:
//
//   - fakeRedis: a faithful in-memory store with correct
//     GET/SET/SETNX/DEL/EXPIRE/TTL/Ping semantics (exercises the ops.go decode
//     and mapError paths for real), plus a recording script seam.
//   - mockRedis: a fully programmable double (return / error injection + call
//     recording) for error-mapping, context-passthrough and use-after-close.
//
// True Lua bytecode execution is not possible offline; the Eval tests assert
// the dispatch contract (the EVALSHA→NOSCRIPT→EVAL fallback path, exact script
// source / KEYS / ARGV passthrough, result decoding). Token-bucket behaviour
// is verified in LIC-TASK-017's adapter. This is an intent-preserving
// deviation, documented here and in CLAUDE.md / completion notes
// (code-architect Q6).

// redisErrStr is a minimal value satisfying redis.Error (Error + RedisError),
// so redis.HasErrorPrefix(err, "NOSCRIPT") inside redis.Script.Run triggers
// the EVALSHA→EVAL fallback. (proto.RedisError is internal and cannot be
// imported by a test.)
type redisErrStr string

func (e redisErrStr) Error() string { return string(e) }
func (e redisErrStr) RedisError()   {}

func sha1Hex(s string) string {
	h := sha1.Sum([]byte(s))
	return hex.EncodeToString(h[:])
}

// --- mockRedis: fully programmable -----------------------------------------

type mockRedis struct {
	getFn    func(ctx context.Context, key string) *redis.StringCmd
	setFn    func(ctx context.Context, key string, value any, exp time.Duration) *redis.StatusCmd
	setNXFn  func(ctx context.Context, key string, value any, exp time.Duration) *redis.BoolCmd
	delFn    func(ctx context.Context, keys ...string) *redis.IntCmd
	expireFn func(ctx context.Context, key string, exp time.Duration) *redis.BoolCmd
	pingFn   func(ctx context.Context) *redis.StatusCmd
	evalFn   func(ctx context.Context, script string, keys []string, args ...any) *redis.Cmd
	closeFn  func() error
}

func (m *mockRedis) Get(ctx context.Context, key string) *redis.StringCmd {
	if m.getFn != nil {
		return m.getFn(ctx, key)
	}
	return redis.NewStringResult("", redis.Nil)
}

func (m *mockRedis) Set(ctx context.Context, key string, value any, exp time.Duration) *redis.StatusCmd {
	if m.setFn != nil {
		return m.setFn(ctx, key, value, exp)
	}
	return redis.NewStatusResult("OK", nil)
}

func (m *mockRedis) SetNX(ctx context.Context, key string, value any, exp time.Duration) *redis.BoolCmd {
	if m.setNXFn != nil {
		return m.setNXFn(ctx, key, value, exp)
	}
	return redis.NewBoolResult(true, nil)
}

func (m *mockRedis) Del(ctx context.Context, keys ...string) *redis.IntCmd {
	if m.delFn != nil {
		return m.delFn(ctx, keys...)
	}
	return redis.NewIntResult(int64(len(keys)), nil)
}

func (m *mockRedis) Expire(ctx context.Context, key string, exp time.Duration) *redis.BoolCmd {
	if m.expireFn != nil {
		return m.expireFn(ctx, key, exp)
	}
	return redis.NewBoolResult(true, nil)
}

func (m *mockRedis) Ping(ctx context.Context) *redis.StatusCmd {
	if m.pingFn != nil {
		return m.pingFn(ctx)
	}
	return redis.NewStatusResult("PONG", nil)
}

func (m *mockRedis) Eval(ctx context.Context, script string, keys []string, args ...any) *redis.Cmd {
	if m.evalFn != nil {
		return m.evalFn(ctx, script, keys, args...)
	}
	return redis.NewCmdResult("OK", nil)
}

// EvalSha forces the NOSCRIPT fallback so redis.Script.Run reaches Eval with
// the script source (the path the dispatch-contract tests assert).
func (m *mockRedis) EvalSha(ctx context.Context, sha1 string, keys []string, args ...any) *redis.Cmd {
	return redis.NewCmdResult(nil, redisErrStr("NOSCRIPT No matching script"))
}

func (m *mockRedis) EvalRO(ctx context.Context, script string, keys []string, args ...any) *redis.Cmd {
	return m.Eval(ctx, script, keys, args...)
}

func (m *mockRedis) EvalShaRO(ctx context.Context, sha1 string, keys []string, args ...any) *redis.Cmd {
	return m.EvalSha(ctx, sha1, keys, args...)
}

func (m *mockRedis) ScriptExists(ctx context.Context, hashes ...string) *redis.BoolSliceCmd {
	return redis.NewBoolSliceResult(make([]bool, len(hashes)), nil)
}

func (m *mockRedis) ScriptLoad(ctx context.Context, script string) *redis.StringCmd {
	return redis.NewStringResult(sha1Hex(script), nil)
}

func (m *mockRedis) Close() error {
	if m.closeFn != nil {
		return m.closeFn()
	}
	return nil
}

var _ RedisAPI = (*mockRedis)(nil)

// --- fakeRedis: faithful in-memory store -----------------------------------

type fakeEntry struct {
	val      string
	expireAt time.Time // zero ⇒ no expiry
}

type fakeRedis struct {
	mu   sync.Mutex
	data map[string]fakeEntry

	// Script seam. evalShaHit=false (default) makes EvalSha return NOSCRIPT so
	// redis.Script.Run falls back to Eval(src) — the realistic cold path.
	evalShaHit bool
	evalRet    any
	evalErr    error
	evalCalls  []evalCall
}

type evalCall struct {
	method string // "EvalSha" | "Eval"
	arg    string // SHA1 hash for EvalSha, script source for Eval
	keys   []string
	args   []any
}

func newFakeRedis() *fakeRedis {
	return &fakeRedis{data: make(map[string]fakeEntry)}
}

// liveEntry returns the entry for key if present and not expired; it lazily
// evicts expired keys (Redis-like).
func (f *fakeRedis) liveEntry(key string) (fakeEntry, bool) {
	e, ok := f.data[key]
	if !ok {
		return fakeEntry{}, false
	}
	if !e.expireAt.IsZero() && time.Now().After(e.expireAt) {
		delete(f.data, key)
		return fakeEntry{}, false
	}
	return e, true
}

func (f *fakeRedis) Get(_ context.Context, key string) *redis.StringCmd {
	f.mu.Lock()
	defer f.mu.Unlock()
	if e, ok := f.liveEntry(key); ok {
		return redis.NewStringResult(e.val, nil)
	}
	return redis.NewStringResult("", redis.Nil)
}

func (f *fakeRedis) Set(_ context.Context, key string, value any, exp time.Duration) *redis.StatusCmd {
	f.mu.Lock()
	defer f.mu.Unlock()
	var expireAt time.Time
	if exp > 0 {
		expireAt = time.Now().Add(exp)
	}
	f.data[key] = fakeEntry{val: toStr(value), expireAt: expireAt}
	return redis.NewStatusResult("OK", nil)
}

func (f *fakeRedis) SetNX(_ context.Context, key string, value any, exp time.Duration) *redis.BoolCmd {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.liveEntry(key); ok {
		return redis.NewBoolResult(false, nil)
	}
	var expireAt time.Time
	if exp > 0 {
		expireAt = time.Now().Add(exp)
	}
	f.data[key] = fakeEntry{val: toStr(value), expireAt: expireAt}
	return redis.NewBoolResult(true, nil)
}

func (f *fakeRedis) Del(_ context.Context, keys ...string) *redis.IntCmd {
	f.mu.Lock()
	defer f.mu.Unlock()
	var n int64
	for _, k := range keys {
		if _, ok := f.liveEntry(k); ok {
			delete(f.data, k)
			n++
		}
	}
	return redis.NewIntResult(n, nil)
}

func (f *fakeRedis) Expire(_ context.Context, key string, exp time.Duration) *redis.BoolCmd {
	f.mu.Lock()
	defer f.mu.Unlock()
	e, ok := f.liveEntry(key)
	if !ok {
		return redis.NewBoolResult(false, nil)
	}
	if exp > 0 {
		e.expireAt = time.Now().Add(exp)
	} else {
		e.expireAt = time.Time{}
	}
	f.data[key] = e
	return redis.NewBoolResult(true, nil)
}

func (f *fakeRedis) Ping(_ context.Context) *redis.StatusCmd {
	return redis.NewStatusResult("PONG", nil)
}

func (f *fakeRedis) Eval(_ context.Context, script string, keys []string, args ...any) *redis.Cmd {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.evalCalls = append(f.evalCalls, evalCall{method: "Eval", arg: script, keys: keys, args: args})
	return redis.NewCmdResult(f.evalRet, f.evalErr)
}

func (f *fakeRedis) EvalSha(_ context.Context, hash string, keys []string, args ...any) *redis.Cmd {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.evalCalls = append(f.evalCalls, evalCall{method: "EvalSha", arg: hash, keys: keys, args: args})
	if !f.evalShaHit {
		return redis.NewCmdResult(nil, redisErrStr("NOSCRIPT No matching script"))
	}
	return redis.NewCmdResult(f.evalRet, f.evalErr)
}

func (f *fakeRedis) EvalRO(ctx context.Context, script string, keys []string, args ...any) *redis.Cmd {
	return f.Eval(ctx, script, keys, args...)
}

func (f *fakeRedis) EvalShaRO(ctx context.Context, hash string, keys []string, args ...any) *redis.Cmd {
	return f.EvalSha(ctx, hash, keys, args...)
}

func (f *fakeRedis) ScriptExists(_ context.Context, hashes ...string) *redis.BoolSliceCmd {
	return redis.NewBoolSliceResult(make([]bool, len(hashes)), nil)
}

func (f *fakeRedis) ScriptLoad(_ context.Context, script string) *redis.StringCmd {
	return redis.NewStringResult(sha1Hex(script), nil)
}

func (f *fakeRedis) Close() error { return nil }

var _ RedisAPI = (*fakeRedis)(nil)

func toStr(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}
