package idempotency

import (
	"context"
	"errors"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"contractpro/legal-intelligence-core/internal/domain/port"
	"contractpro/legal-intelligence-core/internal/infra/kvstore"
)

// ---------------------------------------------------------------------------
// In-package faithful fakeRedis (build-spec D11). miniredis is absent from the
// offline module cache and the network is unavailable (verified — zero
// miniredis hits in go.sum; the kvstore/CLAUDE.md:81-96 in-memory-fake
// precedent). This is a faithful in-memory store implementing RedisSeam:
// correct SET-NX-EX (first-writer-wins + per-key expiry), GET miss ⇒
// kvstore.ErrKeyNotFound (ops.go:24), SET-EX, EXPIRE absent ⇒ (false,nil)
// (ops.go:74-78), DEL, and a recording Eval that interprets the D4 Lua's
// OBSERVABLE contract (SET-NX-EX-or-return-existing) over its map and returns
// the exact []interface{}{int64,string} shape Eval would surface. Lazy TTL
// expiry is driven by an injectable test clock so expiry is deterministic
// (no time.Sleep anywhere). Programmable error injection forces Eval/Set/
// Expire to return a *kvstore.RedisError / context error for the R1 paths.
// ---------------------------------------------------------------------------

// testTime is the injectable monotone clock the fakeRedis uses for lazy
// expiry. It is advanced explicitly by tests — there is no wall clock and no
// time.Sleep anywhere in this suite (PART C #19).
type testTime struct {
	mu  sync.Mutex
	now time.Time
}

func newTestTime() *testTime {
	return &testTime{now: time.Unix(1_700_000_000, 0).UTC()}
}

func (tt *testTime) Now() time.Time {
	tt.mu.Lock()
	defer tt.mu.Unlock()
	return tt.now
}

func (tt *testTime) Advance(d time.Duration) {
	tt.mu.Lock()
	defer tt.mu.Unlock()
	tt.now = tt.now.Add(d)
}

type fakeEntry struct {
	value    string
	deadline time.Time // zero ⇒ no expiry
}

// evalCall records one RedisSeam.Eval invocation (PART C #7 asserts exactly
// one Eval per SetNX/CheckAndAcquire on the normal path with the exact
// KEYS/ARGV).
type evalCall struct {
	script string
	keys   []string
	args   []any
}

type fakeRedis struct {
	mu    sync.Mutex
	clk   *testTime
	store map[string]fakeEntry

	// Recording.
	evalCalls   []evalCall
	expireCalls []struct {
		key string
		ttl time.Duration
	}
	setCalls []struct {
		key, value string
		ttl        time.Duration
	}

	// Error injection (programmable per-op — D11). When set, the matching
	// op returns this error BEFORE touching the store.
	evalErr   error
	setErr    error
	expireErr error
	getErr    error
}

func newFakeRedis(clk *testTime) *fakeRedis {
	return &fakeRedis{clk: clk, store: make(map[string]fakeEntry)}
}

// expiredLocked drops key if its deadline has passed (lazy expiry — the
// ops.go semantics; deterministic via the injected testTime). Caller holds mu.
func (f *fakeRedis) expiredLocked(key string) bool {
	e, ok := f.store[key]
	if !ok {
		return true
	}
	if !e.deadline.IsZero() && !f.clk.Now().Before(e.deadline) {
		delete(f.store, key)
		return true
	}
	return false
}

func (f *fakeRedis) deadlineFor(ttl time.Duration) time.Time {
	if ttl <= 0 {
		return time.Time{}
	}
	return f.clk.Now().Add(ttl)
}

// seed sets key=value with ttl directly (test setup; bypasses recording).
func (f *fakeRedis) seed(key, value string, ttl time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.store[key] = fakeEntry{value: value, deadline: f.deadlineFor(ttl)}
}

func (f *fakeRedis) SetNX(_ context.Context, key, value string, ttl time.Duration) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.expiredLocked(key) {
		return false, nil
	}
	f.store[key] = fakeEntry{value: value, deadline: f.deadlineFor(ttl)}
	return true, nil
}

func (f *fakeRedis) Get(_ context.Context, key string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.getErr != nil {
		return "", f.getErr
	}
	if f.expiredLocked(key) {
		return "", kvstore.ErrKeyNotFound
	}
	return f.store[key].value, nil
}

func (f *fakeRedis) Set(_ context.Context, key, value string, ttl time.Duration) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.setCalls = append(f.setCalls, struct {
		key, value string
		ttl        time.Duration
	}{key, value, ttl})
	if f.setErr != nil {
		return f.setErr
	}
	f.store[key] = fakeEntry{value: value, deadline: f.deadlineFor(ttl)}
	return nil
}

func (f *fakeRedis) Expire(_ context.Context, key string, ttl time.Duration) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.expireCalls = append(f.expireCalls, struct {
		key string
		ttl time.Duration
	}{key, ttl})
	if f.expireErr != nil {
		return false, f.expireErr
	}
	if f.expiredLocked(key) {
		return false, nil // key gone — faithful to ops.go:74-78.
	}
	e := f.store[key]
	e.deadline = f.deadlineFor(ttl)
	f.store[key] = e
	return true, nil
}

func (f *fakeRedis) Delete(_ context.Context, keys ...string) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var n int64
	for _, k := range keys {
		if _, ok := f.store[k]; ok {
			delete(f.store, k)
			n++
		}
	}
	return n, nil
}

// Eval interprets the D4 luaSetNXOrGet OBSERVABLE contract over the in-memory
// map and returns the EXACT []interface{}{int64,string} shape go-redis would
// surface (PART C #19). True Lua bytecode is impossible offline; this asserts
// the SET-NX-EX-or-GET semantics + the result shape + KEYS/ARGV passthrough,
// exactly as kvstore/CLAUDE.md:91-96 does for its own Eval tests.
func (f *fakeRedis) Eval(_ context.Context, script string, keys []string, args ...any) (any, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.evalCalls = append(f.evalCalls, evalCall{script: script, keys: append([]string(nil), keys...), args: append([]any(nil), args...)})
	if f.evalErr != nil {
		return nil, f.evalErr
	}
	if len(keys) != 1 || len(args) != 2 {
		return nil, errors.New("fakeRedis.Eval: unexpected KEYS/ARGV arity")
	}
	key := keys[0]
	value, _ := args[0].(string)
	secs, _ := args[1].(int)
	ttl := time.Duration(secs) * time.Second
	if f.expiredLocked(key) {
		// Absent ⇒ SET NX succeeds ⇒ {1, ""}.
		f.store[key] = fakeEntry{value: value, deadline: f.deadlineFor(ttl)}
		return []interface{}{int64(1), ""}, nil
	}
	// Present ⇒ SET NX fails ⇒ {0, GET}.
	return []interface{}{int64(0), f.store[key].value}, nil
}

// evalNilRedis is a fakeRedis variant whose Eval ALWAYS returns the {0, nil}
// keyspace-eviction corner ({0, <Lua nil>}) so the D4.1 bounded-retry path is
// exercised deterministically.
type evalNilRedis struct {
	*fakeRedis
	calls int
}

func (e *evalNilRedis) Eval(_ context.Context, script string, keys []string, args ...any) (any, error) {
	e.fakeRedis.mu.Lock()
	defer e.fakeRedis.mu.Unlock()
	e.calls++
	e.fakeRedis.evalCalls = append(e.fakeRedis.evalCalls, evalCall{script: script, keys: append([]string(nil), keys...), args: append([]any(nil), args...)})
	return []interface{}{int64(0), nil}, nil // {0, Lua nil} — the D4.1 corner.
}

// ---------------------------------------------------------------------------
// Fake Metrics / Logger / Clock+Ticker.
// ---------------------------------------------------------------------------

type fakeMetrics struct {
	mu       sync.Mutex
	lookups  []string
	fallback int
}

func (m *fakeMetrics) Lookup(result string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lookups = append(m.lookups, result)
}

func (m *fakeMetrics) Fallback() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.fallback++
}

func (m *fakeMetrics) lookupCount(result string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, l := range m.lookups {
		if l == result {
			n++
		}
	}
	return n
}

func (m *fakeMetrics) totalLookups() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.lookups)
}

func (m *fakeMetrics) fallbackCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.fallback
}

type logLine struct {
	level string // "WARN" | "ERROR"
	msg   string
}

type fakeLogger struct {
	mu    sync.Mutex
	lines []logLine
}

func (l *fakeLogger) Warn(_ context.Context, msg string, _ ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lines = append(l.lines, logLine{"WARN", msg})
}

func (l *fakeLogger) Error(_ context.Context, msg string, _ ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lines = append(l.lines, logLine{"ERROR", msg})
}

func (l *fakeLogger) count(level string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	n := 0
	for _, ln := range l.lines {
		if ln.level == level {
			n++
		}
	}
	return n
}

func (l *fakeLogger) has(level, substr string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, ln := range l.lines {
		if ln.level == level && strings.Contains(ln.msg, substr) {
			return true
		}
	}
	return false
}

// manualTicker is a test-driven Ticker (D7/D11): a "tick" is an explicit
// channel send the test controls — -race clean, zero wall-clock waits.
type manualTicker struct {
	ch      chan time.Time
	stopped chan struct{}
	once    sync.Once
}

func newManualTicker() *manualTicker {
	return &manualTicker{ch: make(chan time.Time, 1), stopped: make(chan struct{})}
}

func (mt *manualTicker) C() <-chan time.Time { return mt.ch }

func (mt *manualTicker) Stop() {
	mt.once.Do(func() { close(mt.stopped) })
}

func (mt *manualTicker) tick() { mt.ch <- time.Unix(0, 0) }

// manualClock returns the single manualTicker it was built with so the test
// can drive ticks deterministically.
type manualClock struct {
	ticker *manualTicker
}

func (mc manualClock) NewTicker(time.Duration) Ticker { return mc.ticker }

// ---------------------------------------------------------------------------
// Helpers.
// ---------------------------------------------------------------------------

func newGuard(t *testing.T, r RedisSeam, cfg Config, m Metrics, c Clock, lg Logger) *Guard {
	t.Helper()
	g, err := NewGuard(r, cfg, Deps{Metrics: m, Clock: c, Logger: lg})
	if err != nil {
		t.Fatalf("NewGuard: unexpected error: %v", err)
	}
	return g
}

func okCfg() Config { return Config{HeartbeatInterval: 30 * time.Second} }

const procTTL = 150 * time.Second

// ===========================================================================
// PART C #4 + #17 — frozen port satisfied; lookup constants match SSOT.
// ===========================================================================

// TestGuardSatisfiesFrozenPort is the in-package compile-time + runtime
// assertion that *Guard satisfies port.IdempotencyStorePort byte-for-byte
// (PART C #4, build-spec D13). The package-level var _ in guard.go is the
// compile-time half; this pins it in the test binary too.
func TestGuardSatisfiesFrozenPort(t *testing.T) {
	var _ port.IdempotencyStorePort = (*Guard)(nil)
}

func TestLookupConstantsMatchSSOT(t *testing.T) {
	// Asserted WITHOUT importing the forbidden metrics package (D8/D10 —
	// the consumer D18 precedent). Values are == labels.go:117-120.
	if lookupNew != "new" {
		t.Errorf("lookupNew = %q, want %q", lookupNew, "new")
	}
	if lookupInProgress != "in_progress" {
		t.Errorf("lookupInProgress = %q, want %q", lookupInProgress, "in_progress")
	}
	if lookupCompleted != "completed" {
		t.Errorf("lookupCompleted = %q, want %q", lookupCompleted, "completed")
	}
	if lookupFallbackDB != "fallback_db" {
		t.Errorf("lookupFallbackDB = %q, want %q", lookupFallbackDB, "fallback_db")
	}
}

// ===========================================================================
// PART C #16 — constructor fail-fast (D2).
// ===========================================================================

func TestNewGuardFailFast(t *testing.T) {
	clk := newTestTime()

	t.Run("nil redis", func(t *testing.T) {
		g, err := NewGuard(nil, okCfg(), Deps{})
		if g != nil {
			t.Fatalf("expected nil *Guard, got %v", g)
		}
		if err == nil || !strings.Contains(err.Error(), "redis") {
			t.Fatalf("expected joined error mentioning redis, got %v", err)
		}
	})

	t.Run("HeartbeatInterval zero", func(t *testing.T) {
		g, err := NewGuard(newFakeRedis(clk), Config{HeartbeatInterval: 0}, Deps{})
		if g != nil {
			t.Fatalf("expected nil *Guard, got %v", g)
		}
		if err == nil || !strings.Contains(err.Error(), "HeartbeatInterval") {
			t.Fatalf("expected joined error mentioning HeartbeatInterval, got %v", err)
		}
	})

	t.Run("both invalid — joined error mentions each arg", func(t *testing.T) {
		g, err := NewGuard(nil, Config{HeartbeatInterval: -1}, Deps{})
		if g != nil {
			t.Fatalf("expected nil *Guard, got %v", g)
		}
		if err == nil {
			t.Fatal("expected joined error")
		}
		if !strings.Contains(err.Error(), "redis") || !strings.Contains(err.Error(), "HeartbeatInterval") {
			t.Fatalf("joined error must mention each failing arg, got %v", err)
		}
	})

	t.Run("valid — nil Deps fields degrade to noop", func(t *testing.T) {
		g, err := NewGuard(newFakeRedis(clk), okCfg(), Deps{})
		if err != nil || g == nil {
			t.Fatalf("expected ok, got g=%v err=%v", g, err)
		}
	})
}

// ===========================================================================
// PART C #6 — named test_step tests (exact names required).
// ===========================================================================

func TestGuard_SetNX_AbsentAcquiresProcessing(t *testing.T) {
	clk := newTestTime()
	r := newFakeRedis(clk)
	m := &fakeMetrics{}
	g := newGuard(t, r, okCfg(), m, systemClock{}, noopLogger{})

	status, err := g.SetNX(context.Background(), "k1", procTTL)
	if status != port.IdempotencyAbsent || err != nil {
		t.Fatalf("absent SetNX = (%q, %v), want (Absent, nil)", status, err)
	}
	got, gerr := r.Get(context.Background(), "k1")
	if gerr != nil || got != string(port.IdempotencyProcessing) {
		t.Fatalf("key after acquire = (%q, %v), want PROCESSING", got, gerr)
	}
	if m.lookupCount(lookupNew) != 1 || m.totalLookups() != 1 {
		t.Fatalf("metric: want exactly one {new}, got %v", m.lookups)
	}
}

func TestGuard_SetNX_RepeatReturnsInProgress(t *testing.T) {
	clk := newTestTime()
	r := newFakeRedis(clk)
	m := &fakeMetrics{}
	g := newGuard(t, r, okCfg(), m, systemClock{}, noopLogger{})

	if _, err := g.SetNX(context.Background(), "k", procTTL); err != nil {
		t.Fatalf("first SetNX: %v", err)
	}
	status, err := g.SetNX(context.Background(), "k", procTTL)
	if status != port.IdempotencyProcessing {
		t.Fatalf("repeat SetNX status = %q, want PROCESSING", status)
	}
	if !errors.Is(err, port.ErrIdempotencyKeyExists) {
		t.Fatalf("repeat SetNX err = %v, want ErrIdempotencyKeyExists", err)
	}
	if m.lookupCount(lookupInProgress) != 1 {
		t.Fatalf("metric: want one {in_progress}, got %v", m.lookups)
	}
}

func TestGuard_SetNX_PresentCompleted(t *testing.T) {
	clk := newTestTime()
	r := newFakeRedis(clk)
	m := &fakeMetrics{}
	r.seed("k", string(port.IdempotencyCompleted), 24*time.Hour)
	g := newGuard(t, r, okCfg(), m, systemClock{}, noopLogger{})

	status, err := g.SetNX(context.Background(), "k", procTTL)
	if status != port.IdempotencyCompleted {
		t.Fatalf("status = %q, want COMPLETED", status)
	}
	if !errors.Is(err, port.ErrIdempotencyKeyExists) {
		t.Fatalf("err = %v, want ErrIdempotencyKeyExists", err)
	}
	if m.lookupCount(lookupCompleted) != 1 {
		t.Fatalf("metric: want one {completed}, got %v", m.lookups)
	}
}

func TestGuard_SetNX_PresentPaused(t *testing.T) {
	clk := newTestTime()
	r := newFakeRedis(clk)
	m := &fakeMetrics{}
	r.seed("k", string(port.IdempotencyPaused), 25*time.Hour)
	g := newGuard(t, r, okCfg(), m, systemClock{}, noopLogger{})

	status, err := g.SetNX(context.Background(), "k", procTTL)
	if status != port.IdempotencyPaused {
		t.Fatalf("status = %q, want PAUSED", status)
	}
	if !errors.Is(err, port.ErrIdempotencyKeyExists) {
		t.Fatalf("err = %v, want ErrIdempotencyKeyExists", err)
	}
	// PAUSED maps to in_progress (D8 — no "paused" metric value).
	if m.lookupCount(lookupInProgress) != 1 || m.lookupCount("paused") != 0 {
		t.Fatalf("metric: PAUSED must map to {in_progress}, got %v", m.lookups)
	}
}

func TestGuard_Heartbeat_ExtendsTTL(t *testing.T) {
	clk := newTestTime()
	r := newFakeRedis(clk)
	mt := newManualTicker()
	g := newGuard(t, r, okCfg(), &fakeMetrics{}, manualClock{ticker: mt}, &fakeLogger{})

	// Acquire so the key holds PROCESSING with a deadline.
	if _, err := g.SetNX(context.Background(), "hb", procTTL); err != nil {
		t.Fatalf("SetNX: %v", err)
	}
	r.mu.Lock()
	before := r.store["hb"].deadline
	r.mu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stop := g.StartHeartbeat(ctx, "hb", procTTL)
	defer stop()

	// Advance the clock so the post-tick deadline is strictly later, then
	// drive exactly one tick and wait until Expire is recorded.
	clk.Advance(10 * time.Second)
	mt.tick()
	waitFor(t, func() bool {
		r.mu.Lock()
		defer r.mu.Unlock()
		return len(r.expireCalls) == 1
	}, "Expire recorded after one tick")

	r.mu.Lock()
	gotCall := r.expireCalls[0]
	after := r.store["hb"].deadline
	r.mu.Unlock()
	if gotCall.key != "hb" || gotCall.ttl != procTTL {
		t.Fatalf("Expire call = %+v, want {hb, %v}", gotCall, procTTL)
	}
	if !after.After(before) {
		t.Fatalf("deadline not advanced: before=%v after=%v", before, after)
	}
}

func TestGuard_Heartbeat_StopThenTTLExpiresNaturally(t *testing.T) {
	clk := newTestTime()
	r := newFakeRedis(clk)
	mt := newManualTicker()
	g := newGuard(t, r, okCfg(), &fakeMetrics{}, manualClock{ticker: mt}, &fakeLogger{})

	if _, err := g.SetNX(context.Background(), "hb", procTTL); err != nil {
		t.Fatalf("SetNX: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stop := g.StartHeartbeat(ctx, "hb", procTTL)

	// Stop the heartbeat; the ticker.Stop() must be observed (goroutine
	// exited). Then advance the fake clock past the original deadline:
	// without the heartbeat refreshing it, the key expires naturally.
	stop()
	waitFor(t, func() bool {
		select {
		case <-mt.stopped:
			return true
		default:
			return false
		}
	}, "heartbeat goroutine stopped its ticker")

	clk.Advance(procTTL + time.Second)
	status, err := g.Get(context.Background(), "hb")
	if status != port.IdempotencyAbsent || err != nil {
		t.Fatalf("after stop + TTL expiry: Get = (%q, %v), want (Absent, nil)", status, err)
	}
}

// ===========================================================================
// PART C #7 — atomic Lua, no TOCTOU (D4).
// ===========================================================================

func TestGuardAtomicLuaSingleEvalNoTOCTOU(t *testing.T) {
	clk := newTestTime()

	t.Run("acquire issues exactly one Eval with exact KEYS/ARGV, no Get", func(t *testing.T) {
		r := newFakeRedis(clk)
		g := newGuard(t, r, okCfg(), &fakeMetrics{}, systemClock{}, noopLogger{})
		if _, err := g.SetNX(context.Background(), "kx", procTTL); err != nil {
			t.Fatalf("SetNX: %v", err)
		}
		if len(r.evalCalls) != 1 {
			t.Fatalf("want exactly 1 Eval, got %d", len(r.evalCalls))
		}
		c := r.evalCalls[0]
		if c.script != luaSetNXOrGet {
			t.Fatalf("Eval script mismatch")
		}
		if len(c.keys) != 1 || c.keys[0] != "kx" {
			t.Fatalf("Eval keys = %v, want [kx]", c.keys)
		}
		if len(c.args) != 2 || c.args[0] != string(port.IdempotencyProcessing) || c.args[1] != 150 {
			t.Fatalf("Eval args = %v, want [PROCESSING 150]", c.args)
		}
	})

	t.Run("present path issues no separate Get round-trip", func(t *testing.T) {
		r := newFakeRedis(clk)
		r.seed("kp", string(port.IdempotencyProcessing), procTTL)
		// Spy Get via an injected error: if Guard.SetNX did a Get it would
		// surface; the present path must NOT call Get at all.
		r.getErr = errors.New("Get must not be called on the SetNX present path")
		g := newGuard(t, r, okCfg(), &fakeMetrics{}, systemClock{}, noopLogger{})
		status, err := g.SetNX(context.Background(), "kp", procTTL)
		if status != port.IdempotencyProcessing || !errors.Is(err, port.ErrIdempotencyKeyExists) {
			t.Fatalf("present SetNX = (%q, %v)", status, err)
		}
		if len(r.evalCalls) != 1 {
			t.Fatalf("present path: want exactly 1 Eval, got %d", len(r.evalCalls))
		}
	})

	t.Run("decoder: {1,\"\"} acquired, {0,\"PAUSED\"} present/PAUSED", func(t *testing.T) {
		s, acq, retry, err := decodeEvalResult([]interface{}{int64(1), ""})
		if err != nil || !acq || retry || s != port.IdempotencyAbsent {
			t.Fatalf("{1,\"\"} decode = (%q,%v,%v,%v)", s, acq, retry, err)
		}
		s, acq, retry, err = decodeEvalResult([]interface{}{int64(0), "PAUSED"})
		if err != nil || acq || retry || s != port.IdempotencyPaused {
			t.Fatalf("{0,PAUSED} decode = (%q,%v,%v,%v)", s, acq, retry, err)
		}
	})

	t.Run("unexpected shape / Lua-nil ⇒ errEvalShape (transport-class)", func(t *testing.T) {
		for _, bad := range []any{nil, "scalar", []interface{}{int64(1)}, []interface{}{"x", "y"}, []interface{}{int64(2), "z"}} {
			_, _, _, err := decodeEvalResult(bad)
			if !errors.Is(err, errEvalShape) {
				t.Fatalf("decodeEvalResult(%v) err = %v, want errEvalShape", bad, err)
			}
		}
	})

	t.Run("errEvalShape surfaces as a SetNX transport error", func(t *testing.T) {
		base := newFakeRedis(clk)
		// Force an unexpected shape via a stub wrapping *fakeRedis:
		g := newGuard(t, &shapeFaultRedis{fakeRedis: base}, okCfg(), &fakeMetrics{}, systemClock{}, noopLogger{})
		status, err := g.SetNX(context.Background(), "k", procTTL)
		if status != port.IdempotencyAbsent {
			t.Fatalf("status = %q, want Absent", status)
		}
		if !errors.Is(err, errEvalShape) {
			t.Fatalf("err = %v, want errEvalShape", err)
		}
	})
}

// shapeFaultRedis returns a structurally-unexpected Eval result every call so
// the errEvalShape transport-class path is deterministic. It embeds
// *fakeRedis (a pointer — no mutex copy) so it satisfies RedisSeam.
type shapeFaultRedis struct{ *fakeRedis }

func (shapeFaultRedis) Eval(context.Context, string, []string, ...any) (any, error) {
	return "totally-wrong-shape", nil
}

// ===========================================================================
// PART C #8 — heartbeat lifecycle: all 3 stop conditions + transient + no leak.
// ===========================================================================

func TestGuardHeartbeatLifecycle(t *testing.T) {
	t.Run("ctx-cancel stops the goroutine and the ticker", func(t *testing.T) {
		clk := newTestTime()
		r := newFakeRedis(clk)
		mt := newManualTicker()
		g := newGuard(t, r, okCfg(), &fakeMetrics{}, manualClock{ticker: mt}, &fakeLogger{})
		ctx, cancel := context.WithCancel(context.Background())
		stop := g.StartHeartbeat(ctx, "k", procTTL)
		defer stop()
		cancel()
		waitFor(t, func() bool {
			select {
			case <-mt.stopped:
				return true
			default:
				return false
			}
		}, "goroutine returned on ctx.Done (ticker stopped)")
	})

	t.Run("stop() stops the goroutine; calling it twice does not panic", func(t *testing.T) {
		clk := newTestTime()
		r := newFakeRedis(clk)
		mt := newManualTicker()
		g := newGuard(t, r, okCfg(), &fakeMetrics{}, manualClock{ticker: mt}, &fakeLogger{})
		stop := g.StartHeartbeat(context.Background(), "k", procTTL)
		stop()
		stop() // sync.Once — must not panic / double-close.
		waitFor(t, func() bool {
			select {
			case <-mt.stopped:
				return true
			default:
				return false
			}
		}, "goroutine returned on stop()")
	})

	t.Run("ExtendTTL→ErrIdempotencyKeyVanished ⇒ WARN + goroutine returns", func(t *testing.T) {
		clk := newTestTime()
		r := newFakeRedis(clk)
		// Key absent ⇒ Expire returns (false,nil) ⇒ ExtendTTL ⇒ vanished.
		mt := newManualTicker()
		lg := &fakeLogger{}
		g := newGuard(t, r, okCfg(), &fakeMetrics{}, manualClock{ticker: mt}, lg)
		stop := g.StartHeartbeat(context.Background(), "gone", procTTL)
		defer stop()
		mt.tick()
		waitFor(t, func() bool {
			select {
			case <-mt.stopped:
				return true
			default:
				return false
			}
		}, "goroutine returned on key-vanished")
		if !lg.has("WARN", "key vanished") {
			t.Fatalf("expected WARN 'key vanished', got %v", lg.lines)
		}
	})

	t.Run("transient ExtendTTL error ⇒ WARN + goroutine continues", func(t *testing.T) {
		clk := newTestTime()
		r := newFakeRedis(clk)
		r.seed("live", string(port.IdempotencyProcessing), procTTL)
		r.expireErr = &kvstore.RedisError{Op: "Expire", Retryable: true, Cause: errors.New("blip")}
		mt := newManualTicker()
		lg := &fakeLogger{}
		g := newGuard(t, r, okCfg(), &fakeMetrics{}, manualClock{ticker: mt}, lg)
		stop := g.StartHeartbeat(context.Background(), "live", procTTL)
		defer stop()

		mt.tick()
		waitFor(t, func() bool {
			r.mu.Lock()
			defer r.mu.Unlock()
			return len(r.expireCalls) >= 1
		}, "first tick attempted Expire")
		// Goroutine must NOT have stopped — drive a second tick to prove it.
		select {
		case <-mt.stopped:
			t.Fatal("goroutine stopped on a transient error (must continue)")
		default:
		}
		// Clear the injected error; the next tick succeeds.
		r.mu.Lock()
		r.expireErr = nil
		r.mu.Unlock()
		mt.tick()
		waitFor(t, func() bool {
			r.mu.Lock()
			defer r.mu.Unlock()
			return len(r.expireCalls) >= 2
		}, "second tick still fired (loop continued)")
		if lg.count("WARN") < 1 || !lg.has("WARN", "transient error") {
			t.Fatalf("expected a WARN 'transient error', got %v", lg.lines)
		}
	})
}

// ===========================================================================
// PART C #9 — defensive unknown value (D5).
// ===========================================================================

func TestGuardDefensiveUnknownValue(t *testing.T) {
	clk := newTestTime()

	t.Run("parseStatus garbage ⇒ Processing, empty ⇒ Processing (never Absent)", func(t *testing.T) {
		if got := parseStatus("GARBAGE"); got != port.IdempotencyProcessing {
			t.Fatalf("parseStatus(GARBAGE) = %q, want PROCESSING", got)
		}
		if got := parseStatus("processing"); got != port.IdempotencyProcessing {
			t.Fatalf("parseStatus(lowercase) = %q, want PROCESSING (defensive)", got)
		}
	})

	t.Run("SetNX on a garbage-valued key ⇒ Processing + ErrIdempotencyKeyExists", func(t *testing.T) {
		r := newFakeRedis(clk)
		r.seed("g", "GARBAGE", procTTL)
		g := newGuard(t, r, okCfg(), &fakeMetrics{}, systemClock{}, noopLogger{})
		status, err := g.SetNX(context.Background(), "g", procTTL)
		if status != port.IdempotencyProcessing {
			t.Fatalf("status = %q, want PROCESSING (never Absent)", status)
		}
		if !errors.Is(err, port.ErrIdempotencyKeyExists) {
			t.Fatalf("err = %v, want ErrIdempotencyKeyExists", err)
		}
	})

	t.Run("CheckAndAcquire on a garbage-valued key ⇒ Processing, alreadyExists", func(t *testing.T) {
		r := newFakeRedis(clk)
		r.seed("g2", "GARBAGE", procTTL)
		g := newGuard(t, r, okCfg(), &fakeMetrics{}, systemClock{}, noopLogger{})
		status, exists, err := g.CheckAndAcquire(context.Background(), "g2", procTTL)
		if status != port.IdempotencyProcessing || !exists || err != nil {
			t.Fatalf("CheckAndAcquire = (%q, %v, %v), want (PROCESSING, true, nil)", status, exists, err)
		}
	})
}

// ===========================================================================
// PART C #10 — CheckAndAcquire mapping table (D3.2).
// ===========================================================================

func TestGuardCheckAndAcquireMappingTable(t *testing.T) {
	cases := []struct {
		name       string
		seedVal    string // "" ⇒ absent
		wantStatus port.IdempotencyStatus
		wantExists bool
		wantMetric string
	}{
		{"absent ⇒ new", "", port.IdempotencyAbsent, false, lookupNew},
		{"PROCESSING ⇒ in_progress", string(port.IdempotencyProcessing), port.IdempotencyProcessing, true, lookupInProgress},
		{"PAUSED ⇒ in_progress", string(port.IdempotencyPaused), port.IdempotencyPaused, true, lookupInProgress},
		{"COMPLETED ⇒ completed", string(port.IdempotencyCompleted), port.IdempotencyCompleted, true, lookupCompleted},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			clk := newTestTime()
			r := newFakeRedis(clk)
			if tc.seedVal != "" {
				r.seed("k", tc.seedVal, procTTL)
			}
			m := &fakeMetrics{}
			g := newGuard(t, r, okCfg(), m, systemClock{}, noopLogger{})
			status, exists, err := g.CheckAndAcquire(context.Background(), "k", procTTL)
			if status != tc.wantStatus || exists != tc.wantExists {
				t.Fatalf("= (%q,%v), want (%q,%v)", status, exists, tc.wantStatus, tc.wantExists)
			}
			// Binding D3.2 split: present ⇒ err == nil (NOT
			// ErrIdempotencyKeyExists — unlike SetNX).
			if err != nil {
				t.Fatalf("CheckAndAcquire err = %v, want nil (D3.2 — NOT ErrIdempotencyKeyExists)", err)
			}
			if errors.Is(err, port.ErrIdempotencyKeyExists) {
				t.Fatalf("CheckAndAcquire must NOT return ErrIdempotencyKeyExists (D3.2 split)")
			}
			if m.lookupCount(tc.wantMetric) != 1 || m.totalLookups() != 1 {
				t.Fatalf("metric: want one {%s}, got %v", tc.wantMetric, m.lookups)
			}
		})
	}
}

// ===========================================================================
// PART C #11 — fallback split (R1).
// ===========================================================================

func TestGuardFallbackSplitR1(t *testing.T) {
	transport := &kvstore.RedisError{Op: "Eval", Retryable: true, Cause: errors.New("dial tcp: connection refused")}

	for _, fb := range []bool{true, false} {
		t.Run("SetNX ALWAYS errors verbatim regardless of FallbackEnabled", func(t *testing.T) {
			clk := newTestTime()
			r := newFakeRedis(clk)
			r.evalErr = transport
			m := &fakeMetrics{}
			g := newGuard(t, r, Config{HeartbeatInterval: 30 * time.Second, FallbackEnabled: fb}, m, systemClock{}, &fakeLogger{})
			status, err := g.SetNX(context.Background(), "k", procTTL)
			if status != port.IdempotencyAbsent {
				t.Fatalf("status = %q, want Absent", status)
			}
			if !errors.Is(err, transport) {
				t.Fatalf("err = %v, want the transport error verbatim", err)
			}
			if errors.Is(err, port.ErrIdempotencyKeyExists) {
				t.Fatalf("SetNX transport error must NOT be ErrIdempotencyKeyExists (R1)")
			}
			if m.lookupCount(lookupFallbackDB) != 0 || m.fallbackCount() != 0 {
				t.Fatalf("SetNX must NOT emit fallback_db/Fallback (R1), got %v fb=%d", m.lookups, m.fallbackCount())
			}
		})
	}

	t.Run("CheckAndAcquire + FallbackEnabled=true ⇒ (Absent,false,nil)+fallback_db+Fallback+ERROR", func(t *testing.T) {
		clk := newTestTime()
		r := newFakeRedis(clk)
		r.evalErr = transport
		m := &fakeMetrics{}
		lg := &fakeLogger{}
		g := newGuard(t, r, Config{HeartbeatInterval: 30 * time.Second, FallbackEnabled: true}, m, systemClock{}, lg)
		status, exists, err := g.CheckAndAcquire(context.Background(), "k", procTTL)
		if status != port.IdempotencyAbsent || exists || err != nil {
			t.Fatalf("= (%q,%v,%v), want (Absent,false,nil)", status, exists, err)
		}
		if m.lookupCount(lookupFallbackDB) != 1 || m.fallbackCount() != 1 {
			t.Fatalf("want one {fallback_db} + one Fallback(), got %v fb=%d", m.lookups, m.fallbackCount())
		}
		if lg.count("ERROR") != 1 {
			t.Fatalf("want exactly one ERROR log (the alert), got %v", lg.lines)
		}
	})

	t.Run("CheckAndAcquire + FallbackEnabled=false ⇒ (Absent,false,errVerbatim)+WARN, no fallback metric", func(t *testing.T) {
		clk := newTestTime()
		r := newFakeRedis(clk)
		r.evalErr = transport
		m := &fakeMetrics{}
		lg := &fakeLogger{}
		g := newGuard(t, r, Config{HeartbeatInterval: 30 * time.Second, FallbackEnabled: false}, m, systemClock{}, lg)
		status, exists, err := g.CheckAndAcquire(context.Background(), "k", procTTL)
		if status != port.IdempotencyAbsent || exists {
			t.Fatalf("= (%q,%v), want (Absent,false)", status, exists)
		}
		if !errors.Is(err, transport) {
			t.Fatalf("err = %v, want the transport error verbatim", err)
		}
		if m.lookupCount(lookupFallbackDB) != 0 || m.fallbackCount() != 0 {
			t.Fatalf("must NOT emit fallback_db/Fallback when disabled, got %v fb=%d", m.lookups, m.fallbackCount())
		}
		if lg.count("WARN") != 1 {
			t.Fatalf("want exactly one WARN log, got %v", lg.lines)
		}
	})

	t.Run("context error: fallback-disabled CheckAndAcquire returns it RAW", func(t *testing.T) {
		clk := newTestTime()
		r := newFakeRedis(clk)
		r.evalErr = context.DeadlineExceeded
		g := newGuard(t, r, Config{HeartbeatInterval: 30 * time.Second, FallbackEnabled: false}, &fakeMetrics{}, systemClock{}, &fakeLogger{})
		_, _, err := g.CheckAndAcquire(context.Background(), "k", procTTL)
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("err = %v, want context.DeadlineExceeded RAW (R1 — never wrapped)", err)
		}
		if err != context.DeadlineExceeded { //nolint:errorlint // RAW identity is the contract (R1).
			t.Fatalf("context error must be RAW identity, got %v", err)
		}
	})
}

// ===========================================================================
// PART C #12 — Get contract (R5).
// ===========================================================================

func TestGuardGetContractR5(t *testing.T) {
	clk := newTestTime()

	t.Run("miss ⇒ (Absent, nil) + NO metric", func(t *testing.T) {
		r := newFakeRedis(clk)
		m := &fakeMetrics{}
		g := newGuard(t, r, okCfg(), m, systemClock{}, noopLogger{})
		status, err := g.Get(context.Background(), "absent")
		if status != port.IdempotencyAbsent || err != nil {
			t.Fatalf("miss Get = (%q, %v), want (Absent, nil)", status, err)
		}
		if m.totalLookups() != 0 {
			t.Fatalf("Get must emit NO metric, got %v", m.lookups)
		}
	})

	t.Run("present ⇒ (parseStatus, nil)", func(t *testing.T) {
		r := newFakeRedis(clk)
		r.seed("k", string(port.IdempotencyCompleted), 24*time.Hour)
		g := newGuard(t, r, okCfg(), &fakeMetrics{}, systemClock{}, noopLogger{})
		status, err := g.Get(context.Background(), "k")
		if status != port.IdempotencyCompleted || err != nil {
			t.Fatalf("present Get = (%q, %v)", status, err)
		}
	})

	t.Run("transport error ⇒ (Absent, errVerbatim) + NO metric + NO fallback", func(t *testing.T) {
		r := newFakeRedis(clk)
		boom := &kvstore.RedisError{Op: "Get", Retryable: true, Cause: errors.New("boom")}
		r.getErr = boom
		m := &fakeMetrics{}
		// FallbackEnabled=true must NOT change Get behaviour (R1/R5).
		g := newGuard(t, r, Config{HeartbeatInterval: 30 * time.Second, FallbackEnabled: true}, m, systemClock{}, noopLogger{})
		status, err := g.Get(context.Background(), "k")
		if status != port.IdempotencyAbsent || !errors.Is(err, boom) {
			t.Fatalf("transport Get = (%q, %v), want (Absent, boom verbatim)", status, err)
		}
		if m.totalLookups() != 0 || m.fallbackCount() != 0 {
			t.Fatalf("Get must not consult fallback nor emit metric, got %v fb=%d", m.lookups, m.fallbackCount())
		}
	})
}

// ===========================================================================
// PART C #13 — SetCompleted / SetPaused (D12).
// ===========================================================================

func TestGuardSetCompletedSetPaused(t *testing.T) {
	clk := newTestTime()

	t.Run("SetCompleted overwrites PROCESSING with COMPLETED + per-call ttl", func(t *testing.T) {
		r := newFakeRedis(clk)
		r.seed("k", string(port.IdempotencyProcessing), procTTL)
		g := newGuard(t, r, okCfg(), &fakeMetrics{}, systemClock{}, noopLogger{})
		ttl := 24 * time.Hour
		if err := g.SetCompleted(context.Background(), "k", ttl); err != nil {
			t.Fatalf("SetCompleted: %v", err)
		}
		if len(r.setCalls) != 1 {
			t.Fatalf("want exactly one Set, got %d", len(r.setCalls))
		}
		c := r.setCalls[0]
		if c.key != "k" || c.value != string(port.IdempotencyCompleted) || c.ttl != ttl {
			t.Fatalf("Set = %+v, want {k, COMPLETED, 24h} (per-call ttl, NOT hardcoded — R3)", c)
		}
		got, _ := r.Get(context.Background(), "k")
		if got != string(port.IdempotencyCompleted) {
			t.Fatalf("value after SetCompleted = %q, want COMPLETED", got)
		}
	})

	t.Run("SetPaused overwrites PROCESSING with PAUSED + per-call ttl", func(t *testing.T) {
		r := newFakeRedis(clk)
		r.seed("k", string(port.IdempotencyProcessing), procTTL)
		g := newGuard(t, r, okCfg(), &fakeMetrics{}, systemClock{}, noopLogger{})
		ttl := 25 * time.Hour
		if err := g.SetPaused(context.Background(), "k", ttl); err != nil {
			t.Fatalf("SetPaused: %v", err)
		}
		c := r.setCalls[0]
		if c.key != "k" || c.value != string(port.IdempotencyPaused) || c.ttl != ttl {
			t.Fatalf("Set = %+v, want {k, PAUSED, 25h}", c)
		}
	})

	t.Run("Set error returned verbatim (NOT a model code — R4)", func(t *testing.T) {
		r := newFakeRedis(clk)
		boom := &kvstore.RedisError{Op: "Set", Retryable: true, Cause: errors.New("set boom")}
		r.setErr = boom
		g := newGuard(t, r, okCfg(), &fakeMetrics{}, systemClock{}, noopLogger{})
		if err := g.SetCompleted(context.Background(), "k", time.Hour); !errors.Is(err, boom) {
			t.Fatalf("SetCompleted err = %v, want boom verbatim", err)
		}
		if err := g.SetPaused(context.Background(), "k", time.Hour); !errors.Is(err, boom) {
			t.Fatalf("SetPaused err = %v, want boom verbatim", err)
		}
	})
}

// ===========================================================================
// PART C #14 — ExtendTTL (D6.1).
// ===========================================================================

func TestGuardExtendTTL(t *testing.T) {
	clk := newTestTime()

	t.Run("Expire→(true,nil) ⇒ nil", func(t *testing.T) {
		r := newFakeRedis(clk)
		r.seed("k", string(port.IdempotencyProcessing), procTTL)
		g := newGuard(t, r, okCfg(), &fakeMetrics{}, systemClock{}, noopLogger{})
		if err := g.ExtendTTL(context.Background(), "k", procTTL); err != nil {
			t.Fatalf("ExtendTTL on present key = %v, want nil", err)
		}
	})

	t.Run("Expire→(false,nil) ⇒ ErrIdempotencyKeyVanished", func(t *testing.T) {
		r := newFakeRedis(clk) // key absent ⇒ Expire (false,nil).
		g := newGuard(t, r, okCfg(), &fakeMetrics{}, systemClock{}, noopLogger{})
		err := g.ExtendTTL(context.Background(), "gone", procTTL)
		if !errors.Is(err, ErrIdempotencyKeyVanished) {
			t.Fatalf("ExtendTTL on absent key = %v, want ErrIdempotencyKeyVanished", err)
		}
	})

	t.Run("Expire transport error ⇒ verbatim", func(t *testing.T) {
		r := newFakeRedis(clk)
		boom := &kvstore.RedisError{Op: "Expire", Retryable: true, Cause: errors.New("expire boom")}
		r.expireErr = boom
		g := newGuard(t, r, okCfg(), &fakeMetrics{}, systemClock{}, noopLogger{})
		if err := g.ExtendTTL(context.Background(), "k", procTTL); !errors.Is(err, boom) {
			t.Fatalf("ExtendTTL transport err = %v, want boom verbatim", err)
		}
	})
}

// ===========================================================================
// PART C #15 — no hardcoded TTL (R3): the ttl passed to Eval/Set/Expire is
// EXACTLY the caller's per-call value, never a constant.
// ===========================================================================

func TestGuardNoHardcodedTTL(t *testing.T) {
	clk := newTestTime()
	r := newFakeRedis(clk)
	g := newGuard(t, r, okCfg(), &fakeMetrics{}, systemClock{}, noopLogger{})

	// An arbitrary non-production TTL must reach the seam unchanged.
	custom := 73 * time.Second
	if _, err := g.SetNX(context.Background(), "k", custom); err != nil {
		t.Fatalf("SetNX: %v", err)
	}
	if got := r.evalCalls[0].args[1]; got != 73 {
		t.Fatalf("Eval ttl arg = %v, want 73 (the per-call value, NOT a hardcoded 150/24h/25h — R3)", got)
	}
	if err := g.SetCompleted(context.Background(), "k", 999*time.Hour); err != nil {
		t.Fatalf("SetCompleted: %v", err)
	}
	if r.setCalls[0].ttl != 999*time.Hour {
		t.Fatalf("Set ttl = %v, want 999h (per-call — R3)", r.setCalls[0].ttl)
	}
}

// ===========================================================================
// PART C #18 (partial) — errEvalShape / ErrIdempotencyKeyVanished are plain
// errors.New (not model codes); D4.1 bounded retry.
// ===========================================================================

func TestGuardSentinelsArePlainAndBoundedRetry(t *testing.T) {
	if errEvalShape == nil || ErrIdempotencyKeyVanished == nil {
		t.Fatal("sentinels must be non-nil")
	}
	if !strings.Contains(errEvalShape.Error(), "idempotency:") {
		t.Fatalf("errEvalShape = %q", errEvalShape.Error())
	}
	if !strings.Contains(ErrIdempotencyKeyVanished.Error(), "idempotency:") {
		t.Fatalf("ErrIdempotencyKeyVanished = %q", ErrIdempotencyKeyVanished.Error())
	}

	t.Run("D4.1: {0,nil} keyspace-eviction corner ⇒ bounded single retry then PROCESSING-exists", func(t *testing.T) {
		clk := newTestTime()
		base := newFakeRedis(clk)
		en := &evalNilRedis{fakeRedis: base}
		g := newGuard(t, en, okCfg(), &fakeMetrics{}, systemClock{}, noopLogger{})
		// D4.1: after the bounded single retry the corner persists ⇒ fall
		// through to D5's defensive treat-as-PROCESSING-EXISTS. For the
		// FROZEN SetNX that is the present-key contract (D3.1.5):
		// (IdempotencyProcessing, port.ErrIdempotencyKeyExists) — NEVER a
		// re-run path, NEVER IdempotencyAbsent.
		status, err := g.SetNX(context.Background(), "evict", procTTL)
		if status != port.IdempotencyProcessing {
			t.Fatalf("status = %q, want PROCESSING (D5 defensive after bounded retry)", status)
		}
		if !errors.Is(err, port.ErrIdempotencyKeyExists) {
			t.Fatalf("err = %v, want ErrIdempotencyKeyExists (D3.1.5 present — treat-as-PROCESSING-exists, D4.1)", err)
		}
		// Bounded: exactly 2 attempts (1 initial + 1 retry — D4.1), never
		// unbounded.
		if en.calls != 2 {
			t.Fatalf("Eval attempts = %d, want exactly 2 (bounded single retry — D4.1)", en.calls)
		}
	})

	t.Run("D4.1: CheckAndAcquire over the persistent eviction corner ⇒ (Processing,true,nil)", func(t *testing.T) {
		clk := newTestTime()
		base := newFakeRedis(clk)
		en := &evalNilRedis{fakeRedis: base}
		g := newGuard(t, en, okCfg(), &fakeMetrics{}, systemClock{}, noopLogger{})
		status, exists, err := g.CheckAndAcquire(context.Background(), "evict", procTTL)
		if status != port.IdempotencyProcessing || !exists || err != nil {
			t.Fatalf("CheckAndAcquire = (%q,%v,%v), want (PROCESSING,true,nil) — D5/D3.2 after D4.1 bound", status, exists, err)
		}
		if en.calls != 2 {
			t.Fatalf("Eval attempts = %d, want exactly 2 (bounded single retry — D4.1)", en.calls)
		}
	})
}

// ===========================================================================
// PART C #19 — fakeRedis faithfulness (D11).
// ===========================================================================

func TestFakeRedisFaithfulness(t *testing.T) {
	clk := newTestTime()
	r := newFakeRedis(clk)
	ctx := context.Background()

	// GET miss ⇒ kvstore.ErrKeyNotFound (ops.go:24).
	if _, err := r.Get(ctx, "nope"); !errors.Is(err, kvstore.ErrKeyNotFound) {
		t.Fatalf("Get miss = %v, want kvstore.ErrKeyNotFound", err)
	}
	// EXPIRE absent ⇒ (false,nil) (ops.go:74-78).
	if ok, err := r.Expire(ctx, "nope", time.Hour); ok || err != nil {
		t.Fatalf("Expire absent = (%v,%v), want (false,nil)", ok, err)
	}
	// Eval SET-NX-EX-or-return-existing + exact shape.
	res, err := r.Eval(ctx, luaSetNXOrGet, []string{"k"}, "PROCESSING", 150)
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	arr, ok := res.([]interface{})
	if !ok || len(arr) != 2 || arr[0] != int64(1) || arr[1] != "" {
		t.Fatalf("Eval acquire shape = %#v, want []interface{}{int64(1), \"\"}", res)
	}
	res2, _ := r.Eval(ctx, luaSetNXOrGet, []string{"k"}, "PROCESSING", 150)
	arr2 := res2.([]interface{})
	if arr2[0] != int64(0) || arr2[1] != "PROCESSING" {
		t.Fatalf("Eval present shape = %#v, want []interface{}{int64(0), \"PROCESSING\"}", res2)
	}
	// Lazy expiry driven by the injected clock (no time.Sleep).
	r.seed("ttlk", "PROCESSING", 10*time.Second)
	clk.Advance(11 * time.Second)
	if _, gerr := r.Get(ctx, "ttlk"); !errors.Is(gerr, kvstore.ErrKeyNotFound) {
		t.Fatalf("after clock advance past deadline, Get = %v, want ErrKeyNotFound", gerr)
	}
}

// ---------------------------------------------------------------------------
// waitFor polls cond until true or the test deadline (a bounded, -race-clean
// busy-wait — NO time.Sleep anywhere in the suite, PART C #8/#19). It uses a
// time.After deadline only as a failure backstop (never a success-path wait).
// ---------------------------------------------------------------------------

func waitFor(t *testing.T, cond func() bool, what string) {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		if cond() {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for: %s", what)
		default:
			// Tight poll; yield to the scheduler without sleeping.
			runtime.Gosched()
		}
	}
}
