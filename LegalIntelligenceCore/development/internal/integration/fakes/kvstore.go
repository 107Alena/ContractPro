package fakes

import (
	"context"
	"errors"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	"contractpro/legal-intelligence-core/internal/domain/port"
	"contractpro/legal-intelligence-core/internal/llm/ratelimit"
)

// ErrKeyNotFound mirrors kvstore.ErrKeyNotFound so callers that rely on
// errors.Is(err, kvstore.ErrKeyNotFound) work against the fake too. The
// fake re-declares the sentinel rather than importing it from
// internal/infra/kvstore because errors.Is uses value identity ONLY when
// the production code compares to the SAME sentinel: every consumer of the
// kvstore.ErrKeyNotFound sentinel does so through a transitive interface
// boundary (idempotency.RedisSeam.Get returns ("", err) where err is the
// raw kvstore error). When the production Get is wired to the fake, the
// fake MUST return the SAME sentinel value — so we re-export the
// production one by aliasing in seams_check_test.go and consumers re-test
// the equivalence.
//
// NOTE: kvstore.ErrKeyNotFound is a plain errors.New sentinel; the fake's
// ErrKeyNotFound has the same MESSAGE but its own VALUE — so callers
// asserting errors.Is(err, kvstore.ErrKeyNotFound) would FAIL. The rig
// constructor (NewTestRig) installs an opt-in adapter that re-translates
// the fake's miss-error to kvstore.ErrKeyNotFound at the seam boundary;
// see rig.go.
var ErrKeyNotFound = errors.New("fakes: key not found")

// stringEntry is one Redis string key.
type stringEntry struct {
	value     string
	expiresAt time.Time // zero ⇒ no expiry
}

// hashEntry is one Redis hash key (used by the token-bucket script via
// HMGET/HSET — the only Redis hash in the LIC code path). Field values
// are strings (Redis hash semantics).
type hashEntry struct {
	fields    map[string]string
	expiresAt time.Time // zero ⇒ no expiry
}

// FakeKVStore is the in-memory Redis double. Goroutine-safe. TTL uses
// REAL wall-clock time (acceptance: "real time, не frozen"). Lazy
// expiry: each access first sweeps the entry it touches.
//
// Satisfies (structurally):
//   - idempotency.RedisSeam       — SetNX, Get, Set, Expire, Delete, Eval
//   - ratelimit.LuaEvaluator      — Eval
//   - the general kvstore op set  — Get/Set/SetNX/Delete/Expire/Eval/Ping/Close
//
// The two LIC Lua scripts are recognised by substring marker (the script
// sources are private in their owning packages so source-identity import
// is impossible; the marker strings are uniquely-identifying tokens of
// each script's body):
//   - idempotency.luaSetNXOrGet  — atomic SET-NX-EX-or-return-existing
//   - ratelimit.tokenBucketScript — atomic token bucket (delegates the
//     math to ratelimit.computeBucket — same Go SSOT the production Lua
//     mirrors).
//
// An unrecognised script returns ErrUnknownLuaScript so a future Lua
// addition surfaces in tests as a clear "register me" defect.
type FakeKVStore struct {
	mu      sync.Mutex
	strings map[string]stringEntry
	hashes  map[string]hashEntry

	pingErrs []error // FIFO programmable failures for Ping
	closed   bool
}

// NewFakeKVStore returns an empty store.
func NewFakeKVStore() *FakeKVStore {
	return &FakeKVStore{
		strings: make(map[string]stringEntry),
		hashes:  make(map[string]hashEntry),
	}
}

// expired reports whether t is non-zero and in the past relative to now.
func expired(t time.Time, now time.Time) bool {
	return !t.IsZero() && !t.After(now)
}

// sweepLocked drops the named key from both string and hash maps if it
// has expired. Caller holds f.mu.
func (f *FakeKVStore) sweepLocked(key string, now time.Time) {
	if e, ok := f.strings[key]; ok && expired(e.expiresAt, now) {
		delete(f.strings, key)
	}
	if e, ok := f.hashes[key]; ok && expired(e.expiresAt, now) {
		delete(f.hashes, key)
	}
}

// Get returns the value of key, or ("", ErrKeyNotFound) on miss. Returns
// a context error if ctx is already done.
func (f *FakeKVStore) Get(ctx context.Context, key string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return "", errClosed("Get")
	}
	f.sweepLocked(key, time.Now())
	e, ok := f.strings[key]
	if !ok {
		return "", ErrKeyNotFound
	}
	return e.value, nil
}

// Set stores key=value with ttl. ttl <= 0 means no expiration.
func (f *FakeKVStore) Set(ctx context.Context, key, value string, ttl time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return errClosed("Set")
	}
	f.strings[key] = stringEntry{
		value:     value,
		expiresAt: ttlToExpiry(ttl),
	}
	return nil
}

// SetNX atomically sets key=value with ttl iff key does not exist (or
// has expired). Returns (true, nil) on success, (false, nil) on already-
// present. Matches kvstore.Client.SetNX semantics.
func (f *FakeKVStore) SetNX(ctx context.Context, key, value string, ttl time.Duration) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return false, errClosed("SetNX")
	}
	now := time.Now()
	f.sweepLocked(key, now)
	if _, ok := f.strings[key]; ok {
		return false, nil
	}
	f.strings[key] = stringEntry{
		value:     value,
		expiresAt: ttlToExpiry(ttl),
	}
	return true, nil
}

// Delete drops one or more keys and returns the number actually removed.
// Missing keys are not an error (Redis DEL semantics).
func (f *FakeKVStore) Delete(ctx context.Context, keys ...string) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return 0, errClosed("Delete")
	}
	var removed int64
	now := time.Now()
	for _, k := range keys {
		// Pre-sweep: if the entry already expired, do NOT count it as
		// removed (matches Redis: DEL returns "key existed" only for
		// keys that were live at the moment of the call).
		f.sweepLocked(k, now)
		if _, ok := f.strings[k]; ok {
			delete(f.strings, k)
			removed++
			continue
		}
		if _, ok := f.hashes[k]; ok {
			delete(f.hashes, k)
			removed++
		}
	}
	return removed, nil
}

// Expire sets / refreshes key's TTL. Returns (true, nil) when the key
// existed (and the TTL was applied), (false, nil) when the key is gone.
// Mirrors kvstore.Client.Expire.
func (f *FakeKVStore) Expire(ctx context.Context, key string, ttl time.Duration) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return false, errClosed("Expire")
	}
	now := time.Now()
	f.sweepLocked(key, now)
	exp := ttlToExpiry(ttl)
	if e, ok := f.strings[key]; ok {
		e.expiresAt = exp
		f.strings[key] = e
		return true, nil
	}
	if e, ok := f.hashes[key]; ok {
		e.expiresAt = exp
		f.hashes[key] = e
		return true, nil
	}
	return false, nil
}

// Ping returns nil on a healthy store. The FIFO error queue (pre-loaded
// via InjectPingError) drives /readyz-style negative tests.
func (f *FakeKVStore) Ping(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return errClosed("Ping")
	}
	if len(f.pingErrs) > 0 {
		err := f.pingErrs[0]
		f.pingErrs = f.pingErrs[1:]
		return err
	}
	return nil
}

// Close marks the store closed. Idempotent.
func (f *FakeKVStore) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
	return nil
}

// InjectPingError queues a future Ping failure (FIFO).
func (f *FakeKVStore) InjectPingError(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pingErrs = append(f.pingErrs, err)
}

// Reset clears all keys (string + hash) and pending Ping errors. closed
// state is NOT changed — call NewFakeKVStore for a fresh instance.
func (f *FakeKVStore) Reset() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.strings = make(map[string]stringEntry)
	f.hashes = make(map[string]hashEntry)
	f.pingErrs = nil
}

// Size returns the live (post-sweep) string + hash key count. Used by
// tests asserting eviction behaviour.
func (f *FakeKVStore) Size() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	now := time.Now()
	for k := range f.strings {
		f.sweepLocked(k, now)
	}
	for k := range f.hashes {
		f.sweepLocked(k, now)
	}
	return len(f.strings) + len(f.hashes)
}

// TTL returns the remaining time on key (0 if no expiry, -1 if absent).
// Test convenience — not part of any port.
func (f *FakeKVStore) TTL(key string) time.Duration {
	f.mu.Lock()
	defer f.mu.Unlock()
	now := time.Now()
	f.sweepLocked(key, now)
	if e, ok := f.strings[key]; ok {
		if e.expiresAt.IsZero() {
			return 0
		}
		return time.Until(e.expiresAt)
	}
	if e, ok := f.hashes[key]; ok {
		if e.expiresAt.IsZero() {
			return 0
		}
		return time.Until(e.expiresAt)
	}
	return -1
}

// ----------------------------------------------------------------------------
// Eval — LIC Lua dispatch.
// ----------------------------------------------------------------------------

// ErrUnknownLuaScript is returned by Eval for a script the fake does not
// recognise. The two LIC scripts are recognised by substring markers; a
// new script must add a dispatch entry here.
var ErrUnknownLuaScript = errors.New("fakes: unknown Lua script — register a handler in fakes/kvstore.go")

// markerIdempotencyLua uniquely identifies the idempotency.luaSetNXOrGet
// script (idempotency/script.go:31). The full source string is private;
// the marker is a token that exists ONLY in that script.
const markerIdempotencyLua = `SET', KEYS[1], ARGV[1], 'NX', 'EX'`

// markerTokenBucketLua uniquely identifies the ratelimit.tokenBucketScript
// (ratelimit/script.go:42). Same private-source rationale as above.
const markerTokenBucketLua = `redis.replicate_commands()`

// Eval recognises the known LIC Lua scripts and runs them in pure Go.
//
// Idempotency luaSetNXOrGet (script.go:31):
//
//	KEYS[1] = key
//	ARGV[1] = value to store (e.g. "PROCESSING")
//	ARGV[2] = ttl seconds (int or numeric string)
//	returns []interface{}{int64(1), ""}        when acquired
//	        []interface{}{int64(0), <current>} when present
//
// Token-bucket script (script.go:42): math delegated to ratelimit.computeBucket.
//
//	KEYS[1] = key
//	ARGV[1] = rps (float string)
//	ARGV[2] = burst (int string)
//	ARGV[3] = requested (int string)
//	returns []interface{}{int64(allowed), int64(retryAfterMS)}
//
// An unrecognised script returns ErrUnknownLuaScript.
func (f *FakeKVStore) Eval(ctx context.Context, script string, keys []string, args ...any) (any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	switch {
	case strings.Contains(script, markerIdempotencyLua):
		return f.evalIdempotency(keys, args)
	case strings.Contains(script, markerTokenBucketLua):
		return f.evalTokenBucket(keys, args)
	default:
		return nil, ErrUnknownLuaScript
	}
}

func (f *FakeKVStore) evalIdempotency(keys []string, args []any) (any, error) {
	if len(keys) != 1 || len(args) != 2 {
		return nil, errors.New("fakes: luaSetNXOrGet expects 1 KEYS + 2 ARGV")
	}
	key := keys[0]
	value, ok := args[0].(string)
	if !ok {
		return nil, errors.New("fakes: luaSetNXOrGet ARGV[1] must be string")
	}
	secs, ok := toIntArg(args[1])
	if !ok || secs < 1 {
		return nil, errors.New("fakes: luaSetNXOrGet ARGV[2] must be positive int")
	}
	ttl := time.Duration(secs) * time.Second

	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return nil, errClosed("Eval")
	}
	now := time.Now()
	f.sweepLocked(key, now)
	if e, present := f.strings[key]; present {
		return []interface{}{int64(0), e.value}, nil
	}
	f.strings[key] = stringEntry{value: value, expiresAt: now.Add(ttl)}
	return []interface{}{int64(1), ""}, nil
}

func (f *FakeKVStore) evalTokenBucket(keys []string, args []any) (any, error) {
	if len(keys) != 1 || len(args) != 3 {
		return nil, errors.New("fakes: tokenBucketScript expects 1 KEYS + 3 ARGV")
	}
	key := keys[0]
	rps, err := toFloatArg(args[0])
	if err != nil {
		return nil, err
	}
	burst, ok := toIntArg(args[1])
	if !ok {
		return nil, errors.New("fakes: tokenBucketScript ARGV[2] must be int")
	}
	requested, ok := toIntArg(args[2])
	if !ok {
		return nil, errors.New("fakes: tokenBucketScript ARGV[3] must be int")
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return nil, errClosed("Eval")
	}
	now := time.Now()
	f.sweepLocked(key, now)

	var prev *bucketState
	if e, present := f.hashes[key]; present {
		tokens, errT := strconv.ParseInt(e.fields["tokens"], 10, 64)
		ts, errS := strconv.ParseInt(e.fields["ts"], 10, 64)
		if errT == nil && errS == nil {
			prev = &bucketState{tokensMicro: tokens, tsUS: ts}
		}
	}

	nowUS := now.UnixMicro()
	newState, result := computeBucketSlim(prev, rps, burst, requested, nowUS)

	// Persist hash and EXPIRE per script.
	windowS := int(math.Ceil(float64(burst) / rps))
	if windowS < 60 {
		windowS = 60
	}
	exp := now.Add(time.Duration(windowS) * time.Second)
	f.hashes[key] = hashEntry{
		fields: map[string]string{
			"tokens": strconv.FormatInt(newState.tokensMicro, 10),
			"ts":     strconv.FormatInt(newState.tsUS, 10),
		},
		expiresAt: exp,
	}

	allowed := int64(0)
	if result.allowed {
		allowed = 1
	}
	return []interface{}{allowed, result.retryAfterMS}, nil
}

// bucketState is the per-key persisted token-bucket state in the fake.
// Same shape as the production internal type.
type bucketState struct {
	tokensMicro int64
	tsUS        int64
}

// bucketSlimResult mirrors the production bucketResult (private).
type bucketSlimResult struct {
	allowed      bool
	retryAfterMS int64
}

// computeBucketSlim shells out to ratelimit.NewLimiter would force a full
// validation pass; the production math is exposed only through Limiter.Wait
// and the unexported computeBucket. We re-implement the same arithmetic
// here — IDENTICAL to ratelimit/script.go's computeBucket — so the fake's
// Eval is faithful WITHOUT importing private symbols. The arithmetic IS
// the script-Go SSOT; production tests pin it in ratelimit/script_test.go.
func computeBucketSlim(prev *bucketState, rps float64, burst, requested int, nowUS int64) (bucketState, bucketSlimResult) {
	const microScale = 1_000_000
	capMicro := int64(burst) * microScale
	reqMicro := int64(requested) * microScale

	var tokens float64
	var ts int64
	if prev == nil {
		tokens = float64(capMicro)
		ts = nowUS
	} else {
		elapsed := nowUS - prev.tsUS
		if elapsed < 0 {
			elapsed = 0
		}
		tokens = float64(prev.tokensMicro) + float64(elapsed)*rps
		if tokens > float64(capMicro) {
			tokens = float64(capMicro)
		}
		ts = nowUS
	}

	res := bucketSlimResult{}
	if tokens >= float64(reqMicro) {
		tokens -= float64(reqMicro)
		res.allowed = true
	} else {
		deficit := float64(reqMicro) - tokens
		ms := int64(math.Ceil(deficit / (rps * 1000.0)))
		if ms < 1 {
			ms = 1
		}
		res.retryAfterMS = ms
	}

	return bucketState{tokensMicro: int64(math.Floor(tokens)), tsUS: ts}, res
}

// ----------------------------------------------------------------------------
// helpers
// ----------------------------------------------------------------------------

func ttlToExpiry(ttl time.Duration) time.Time {
	if ttl <= 0 {
		return time.Time{}
	}
	return time.Now().Add(ttl)
}

func errClosed(op string) error {
	return errors.New("fakes: " + op + ": store closed")
}

// toIntArg accepts int, int32, int64 or a string parseable as int64.
// luaArgs in production sends int (see idempotency/script.go:54); we
// accept a wider input set for test ergonomics.
func toIntArg(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int32:
		return int(n), true
	case int64:
		return int(n), true
	case string:
		i, err := strconv.Atoi(n)
		return i, err == nil
	default:
		return 0, false
	}
}

func toFloatArg(v any) (float64, error) {
	switch n := v.(type) {
	case float64:
		return n, nil
	case float32:
		return float64(n), nil
	case int:
		return float64(n), nil
	case int64:
		return float64(n), nil
	case string:
		return strconv.ParseFloat(n, 64)
	default:
		return 0, errors.New("fakes: not a number")
	}
}

// ensure ratelimit import is observable — kept here so a future "unused
// import" sweep doesn't tear it out: we intentionally keep the dependency
// because LIC-TASK-049 (RE_CHECK + provider fallback tests) will need to
// configure ratelimit.NewLimiter directly with this fake as evaluator and
// the link must survive a compile in this file even if the in-file usage
// is removed. The struct value below has zero runtime cost.
var _ ratelimit.LuaEvaluator = (*FakeKVStore)(nil)

// String returns a short identifier for tests.
func (f *FakeKVStore) String() string {
	return "FakeKVStore"
}

// Verify the structural contract with idempotency.RedisSeam at compile
// time, kept here so a method-signature drift is caught immediately. The
// real interface lives in internal/ingress/idempotency/seams.go; we use
// the local typed alias to avoid a circular-import path (the production
// adapter must not import this test-helper package, but tests of THIS
// package can — see seams_check_test.go for the assertion against the
// real interface).
var (
	_ kvOpsSurface = (*FakeKVStore)(nil)
)

// kvOpsSurface is the union we structurally claim, declared in this file
// so a method removal here is a compile error. The strong cross-package
// assertion is in seams_check_test.go.
type kvOpsSurface interface {
	Get(ctx context.Context, key string) (string, error)
	Set(ctx context.Context, key, value string, ttl time.Duration) error
	SetNX(ctx context.Context, key, value string, ttl time.Duration) (bool, error)
	Delete(ctx context.Context, keys ...string) (int64, error)
	Expire(ctx context.Context, key string, ttl time.Duration) (bool, error)
	Eval(ctx context.Context, script string, keys []string, args ...any) (any, error)
	Ping(ctx context.Context) error
	Close() error
}

// IdempotencyValueString returns the wire-canonical string of an
// idempotency status so tests asserting Get results don't repeat the
// stringification every line.
func IdempotencyValueString(s port.IdempotencyStatus) string {
	return string(s)
}
