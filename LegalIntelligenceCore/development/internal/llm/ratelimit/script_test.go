package ratelimit

import (
	"errors"
	"strings"
	"testing"
	"time"
)

const usPerSec = int64(time.Second / time.Microsecond)

func TestComputeBucket_ColdKeyStartsFull(t *testing.T) {
	// Cold key (prev==nil): full burst, first request allowed, NOT refilled
	// from a zero timestamp (that would overflow Lua double precision).
	st, res := computeBucket(nil, 10, 20, 1, 5*usPerSec)
	if !res.allowed {
		t.Fatalf("cold key must allow the first request, got denied")
	}
	if want := int64(19) * microScale; st.tokensMicro != want {
		t.Fatalf("tokensMicro after first take = %d, want %d", st.tokensMicro, want)
	}
	if st.tsUS != 5*usPerSec {
		t.Fatalf("ts = %d, want %d (now, not 0)", st.tsUS, 5*usPerSec)
	}
}

func TestComputeBucket_DrainThenDeny(t *testing.T) {
	rps, burst := 10.0, 3
	var prev *bucketState
	now := int64(0)
	for i := 0; i < burst; i++ {
		st, res := computeBucket(prev, rps, burst, 1, now)
		if !res.allowed {
			t.Fatalf("request %d within burst must be allowed", i+1)
		}
		cp := st
		prev = &cp
	}
	// Burst exhausted, no time elapsed → denied with a positive retry-after.
	st, res := computeBucket(prev, rps, burst, 1, now)
	if res.allowed {
		t.Fatalf("request beyond burst with no refill must be denied")
	}
	if res.retryAfterMS < minRetryAfterMS {
		t.Fatalf("retryAfterMS = %d, want >= %d", res.retryAfterMS, minRetryAfterMS)
	}
	// 1 token at rps=10 ⇒ 100ms.
	if res.retryAfterMS != 100 {
		t.Fatalf("retryAfterMS = %d, want 100 (1 token / 10rps)", res.retryAfterMS)
	}
	// Denied state is still persisted (ts advanced).
	if st.tsUS != now {
		t.Fatalf("denied path must persist ts; got %d want %d", st.tsUS, now)
	}
}

func TestComputeBucket_RefillCappedAtBurst(t *testing.T) {
	rps, burst := 10.0, 5
	// Drain one then idle far longer than the bucket window.
	st, _ := computeBucket(nil, rps, burst, 1, 0)
	cp := st
	st2, res := computeBucket(&cp, rps, burst, 1, 3600*usPerSec) // 1h later
	if !res.allowed {
		t.Fatalf("request after long idle must be allowed")
	}
	// Refill is capped at burst, then 1 taken ⇒ exactly (burst-1) micro-tokens.
	if want := int64(burst-1) * microScale; st2.tokensMicro != want {
		t.Fatalf("tokensMicro = %d, want %d (capped at burst)", st2.tokensMicro, want)
	}
}

func TestComputeBucket_SustainsRPS(t *testing.T) {
	// Drain burst, then 1 request every 1/rps with virtual time advancing:
	// every request must be allowed for a full simulated second (test_step 2).
	rps, burst := 10.0, 20
	var prev *bucketState
	now := int64(0)
	for i := 0; i < burst; i++ {
		st, res := computeBucket(prev, rps, burst, 1, now)
		if !res.allowed {
			t.Fatalf("burst request %d must be allowed", i+1)
		}
		cp := st
		prev = &cp
	}
	step := usPerSec / int64(rps) // 100ms in microseconds
	for i := 0; i < int(rps); i++ {
		now += step
		st, res := computeBucket(prev, rps, burst, 1, now)
		if !res.allowed {
			t.Fatalf("sustained request %d at %d rps must be allowed", i+1, int(rps))
		}
		cp := st
		prev = &cp
	}
}

func TestComputeBucket_ClockBackwardsClampsElapsed(t *testing.T) {
	st, _ := computeBucket(nil, 10, 5, 1, 1_000_000)
	cp := st
	// now < prev.ts (clock skew): elapsed clamped to 0, no negative refill.
	st2, res := computeBucket(&cp, 10, 5, 1, 500_000)
	if !res.allowed {
		t.Fatalf("backwards clock must not deny a still-funded bucket")
	}
	if st2.tokensMicro > cp.tokensMicro {
		t.Fatalf("backwards clock must not add tokens: %d > %d", st2.tokensMicro, cp.tokensMicro)
	}
}

func TestComputeBucket_FractionalRPS(t *testing.T) {
	// rps=0.5, burst=1: after draining, ~2s to refill one token.
	st, res := computeBucket(nil, 0.5, 1, 1, 0)
	if !res.allowed {
		t.Fatalf("cold fractional bucket must allow first request")
	}
	cp := st
	_, res = computeBucket(&cp, 0.5, 1, 1, 0)
	if res.allowed {
		t.Fatalf("second immediate request must be denied")
	}
	if res.retryAfterMS != 2000 {
		t.Fatalf("retryAfterMS = %d, want 2000 (1 token / 0.5rps)", res.retryAfterMS)
	}
}

func TestDecodeResult(t *testing.T) {
	tests := []struct {
		name    string
		in      any
		wantOK  bool
		allowed bool
		retry   time.Duration
	}{
		{"allowed", []any{int64(1), int64(0)}, true, true, 0},
		{"denied", []any{int64(0), int64(5)}, true, false, 5 * time.Millisecond},
		{"int-elements", []any{1, 250}, true, true, 250 * time.Millisecond},
		{"string-numbers", []any{"0", "12"}, true, false, 12 * time.Millisecond},
		{"negative-retry-clamped", []any{int64(0), int64(-9)}, true, false, 0},
		{"nil", nil, false, false, 0},
		{"wrong-len", []any{int64(1)}, false, false, 0},
		{"bad-elem", []any{struct{}{}, int64(0)}, false, false, 0},
		{"not-a-slice", "OK", false, false, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res, err := decodeResult(tc.in)
			if tc.wantOK && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !tc.wantOK {
				if !errors.Is(err, errScriptAnomaly) {
					t.Fatalf("err = %v, want errScriptAnomaly", err)
				}
				return
			}
			if res.allowed != tc.allowed {
				t.Fatalf("allowed = %v, want %v", res.allowed, tc.allowed)
			}
			if got := msToDuration(res.retryAfterMS); got != tc.retry {
				t.Fatalf("retry = %v, want %v", got, tc.retry)
			}
		})
	}
}

// TestLuaScriptInvariants pins the structural contract of the Lua source so a
// future edit cannot silently break determinism-replication, the clock
// source, dual-path persistence, TTL or the never-nil return (the offline
// environment cannot run a Lua VM; the //go:build redis_integration test
// validates execution when a live Redis is available).
func TestLuaScriptInvariants(t *testing.T) {
	s := tokenBucketScript

	firstLine := strings.TrimSpace(strings.SplitN(s, "\n", 2)[0])
	if firstLine != "redis.replicate_commands()" {
		t.Fatalf("first statement must be redis.replicate_commands() (MF-4), got %q", firstLine)
	}
	if idx := strings.Index(s, "redis.replicate_commands()"); idx > strings.Index(s, "redis.call") {
		t.Fatalf("redis.replicate_commands() must precede any redis.call")
	}

	for _, frag := range []string{
		"redis.call('TIME')",        // multi-instance clock (OQ-E)
		"redis.call('HMGET'",        // read state
		"redis.call('HSET'",         // persist (both allow & deny)
		"redis.call('EXPIRE'",       // TTL max(window,60s)
		"return {allowed, retry_after_ms}", // never nil
		"local SCALE     = 1000000", // integer micro-tokens
	} {
		if !strings.Contains(s, frag) {
			t.Fatalf("Lua source missing required fragment: %q", frag)
		}
	}
	// HSET must appear after the allow/deny branch (single persistence point
	// covering both outcomes), not inside the `if tokens >= req` block only.
	if strings.Count(s, "redis.call('HSET'") != 1 {
		t.Fatalf("exactly one HSET expected (single dual-path persistence point)")
	}
}

func TestMicroScaleConstant(t *testing.T) {
	// The Go SSOT and the Lua SCALE must agree or HSET round-trips drift.
	if !strings.Contains(tokenBucketScript, "1000000") {
		t.Fatalf("Lua SCALE literal absent")
	}
	if microScale != 1_000_000 {
		t.Fatalf("microScale = %d, want 1_000_000 (must match Lua SCALE)", microScale)
	}
}
