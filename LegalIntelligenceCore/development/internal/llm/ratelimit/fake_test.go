package ratelimit

import (
	"context"
	"strconv"
	"sync"
	"time"
)

// anomalyMode forces fakeEvaluator to return a contractually-impossible reply
// so the script-anomaly fail-open path is exercised.
type anomalyMode int

const (
	anomalyNone anomalyMode = iota
	anomalyNilReply
	anomalyBadShape
	anomalyBadElem
)

// fakeEvaluator is a faithful in-memory LuaEvaluator. It executes the exact
// token-bucket semantics through the shared computeBucket so the Go and Lua
// implementations cannot drift, driven by an injectable virtual microsecond
// clock so the RPS-sustain / burst / refill tests are deterministic and
// -race clean (code-architect MF-1/MF-2). miniredis is unavailable offline and
// no Lua VM exists offline; the real EVALSHA→EVAL dispatch is already proven
// by LIC-TASK-007 — here we verify bucket *behaviour*.
type fakeEvaluator struct {
	mu sync.Mutex

	states     map[string]*bucketState
	useVirtual bool
	virtualUS  int64

	forcedErr error // infra-error injection (Redis down)
	anomaly   anomalyMode

	calls      int
	lastScript string
	lastKeys   []string
	lastArgs   []any
}

func newFakeEvaluator() *fakeEvaluator {
	return &fakeEvaluator{states: make(map[string]*bucketState)}
}

// withVirtualClock pins the server clock to a fixed start (microseconds) so
// tests advance time explicitly via advance().
func (f *fakeEvaluator) withVirtualClock(start time.Duration) *fakeEvaluator {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.useVirtual = true
	f.virtualUS = start.Microseconds()
	return f
}

func (f *fakeEvaluator) advance(d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.virtualUS += d.Microseconds()
}

func (f *fakeEvaluator) setForcedErr(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.forcedErr = err
}

func (f *fakeEvaluator) setAnomaly(m anomalyMode) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.anomaly = m
}

func (f *fakeEvaluator) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// Eval mirrors what bucket.allow sends: keys=[lic:rate:{provider}],
// args=[rpsStr, burstStr, "1"], and the script's HMGET→computeBucket→HSET.
func (f *fakeEvaluator) Eval(ctx context.Context, script string, keys []string, args ...any) (any, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.calls++
	f.lastScript = script
	f.lastKeys = keys
	f.lastArgs = args

	if f.forcedErr != nil {
		return nil, f.forcedErr
	}
	switch f.anomaly {
	case anomalyNilReply:
		return nil, nil
	case anomalyBadShape:
		return []any{int64(1)}, nil // wrong length
	case anomalyBadElem:
		return []any{struct{}{}, int64(0)}, nil // undecodable element
	}

	rps, _ := strconv.ParseFloat(args[0].(string), 64)
	burst, _ := strconv.Atoi(args[1].(string))
	req, _ := strconv.Atoi(args[2].(string))
	key := keys[0]

	var now int64
	if f.useVirtual {
		now = f.virtualUS
	} else {
		now = time.Now().UnixMicro()
	}

	ns, res := computeBucket(f.states[key], rps, burst, req, now)
	cp := ns
	f.states[key] = &cp

	allowed := int64(0)
	if res.allowed {
		allowed = 1
	}
	return []any{allowed, res.retryAfterMS}, nil
}

// recordingObserver counts the three signals so tests can assert that denied
// → RateLimited only, fail-open → FailOpen only, anomaly → ScriptAnomaly only.
type recordingObserver struct {
	mu          sync.Mutex
	rateLimited int
	failOpen    int
	anomaly     int
	lastErr     error

	// firstDenied (when non-nil) receives exactly once, on the first
	// RateLimited, so timing tests can synchronise on a *proven* denial
	// instead of a fragile time.Sleep (golang-pro MF-2 / code-reviewer M2).
	firstDenied chan struct{}
}

func (o *recordingObserver) RateLimited(string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.rateLimited++
	if o.firstDenied != nil {
		select {
		case o.firstDenied <- struct{}{}:
		default:
		}
	}
}

func (o *recordingObserver) FailOpen(_ string, err error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.failOpen++
	o.lastErr = err
}

func (o *recordingObserver) ScriptAnomaly(_ string, err error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.anomaly++
	o.lastErr = err
}

func (o *recordingObserver) snapshot() (rl, fo, an int) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.rateLimited, o.failOpen, o.anomaly
}

var _ Observer = (*recordingObserver)(nil)
var _ LuaEvaluator = (*fakeEvaluator)(nil)
