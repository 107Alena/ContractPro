package ratelimit

import (
	"errors"
	"math"
	"strconv"
	"time"
)

// microScale is the fixed-point factor used to store fractional token balances
// as integers. A float HSET round-trips lossily (Redis stores the value as a
// string and "%.17g" / re-parse drifts); storing tokens × 1e6 keeps every
// write lossless while still resolving sub-token refills at any sane RPS
// (code-reviewer B1 from the prior reviewed design; code-architect OQ-E).
const microScale = 1_000_000

// minRetryAfterMS is the floor the Lua script applies to retry_after_ms so a
// denied call never reports 0 (which would let the caller busy-spin).
const minRetryAfterMS = 1

// tokenBucketScript is the atomic token-bucket evaluated server-side. Its
// arithmetic is the exact integer mirror of computeBucket below — the two are
// kept in lock-step (script_test.go pins the structural invariants; the
// //go:build redis_integration test in script_integration_test.go runs the
// real script against a live Redis and asserts it agrees with computeBucket,
// closing the Lua/Go drift gap, prior H3/M1).
//
// Determinism: the script reads the server clock via redis.TIME, so it is
// classified non-deterministic. redis.replicate_commands() MUST be the first
// statement — on Redis < 5 / script-replication configs the first write after
// redis.TIME would otherwise be rejected; on effects-replication it is a
// harmless no-op (code-architect MF-4). Minimum supported Redis: 5.0.
//
// KEYS[1] = lic:rate:{provider}
// ARGV[1] = rps   (float, tokens/sec)
// ARGV[2] = burst (int, bucket capacity in whole tokens)
// ARGV[3] = requested (int, tokens to take — always 1 in v1)
//
// Returns {allowed (1|0), retry_after_ms (>=1 when denied, else 0)}. The
// script NEVER returns nil — a nil/garbage reply on the Go side therefore
// signals a script/Redis-data anomaly, handled distinctly from "Redis down".
const tokenBucketScript = `redis.replicate_commands()

local key       = KEYS[1]
local rps       = tonumber(ARGV[1])
local burst     = tonumber(ARGV[2])
local requested = tonumber(ARGV[3])

local SCALE     = 1000000
local cap_micro = burst * SCALE
local req_micro = requested * SCALE

local t      = redis.call('TIME')
local now_us = (tonumber(t[1]) * 1000000) + tonumber(t[2])

local data   = redis.call('HMGET', key, 'tokens', 'ts')
local tokens = tonumber(data[1])
local ts     = tonumber(data[2])

if tokens == nil or ts == nil then
  -- Cold key: start full. Never refill from a zero ts (now_us - 0 would be
  -- ~1.7e15 and overflow Lua double precision once multiplied by rps).
  tokens = cap_micro
  ts     = now_us
else
  local elapsed_us = now_us - ts
  if elapsed_us < 0 then elapsed_us = 0 end          -- clock skew / backwards
  tokens = tokens + (elapsed_us * rps)               -- micro-tokens accrued
  if tokens > cap_micro then tokens = cap_micro end
  ts = now_us
end

local allowed        = 0
local retry_after_ms = 0

if tokens >= req_micro then
  tokens  = tokens - req_micro
  allowed = 1
else
  local deficit  = req_micro - tokens
  retry_after_ms = math.ceil(deficit / (rps * 1000.0))
  if retry_after_ms < 1 then retry_after_ms = 1 end
end

redis.call('HSET', key, 'tokens', math.floor(tokens), 'ts', ts)

local window_s = math.ceil(burst / rps)
local ttl_s    = window_s
if ttl_s < 60 then ttl_s = 60 end
redis.call('EXPIRE', key, ttl_s)

return {allowed, retry_after_ms}
`

// bucketState is the persisted per-provider balance: micro-tokens and the
// microsecond server timestamp of the last refill.
type bucketState struct {
	tokensMicro int64
	tsUS        int64
}

// bucketResult is the decoded outcome of one token-bucket evaluation.
type bucketResult struct {
	allowed      bool
	retryAfterMS int64
}

// computeBucket is the single Go source of truth for token-bucket arithmetic.
// It is the exact mirror of tokenBucketScript and is used by the in-memory
// test evaluator so the Lua and Go implementations cannot drift (the offline
// module cache has no miniredis and no Lua VM is available — true EVALSHA→EVAL
// dispatch is already proven by LIC-TASK-007's kvstore tests; here we verify
// bucket *behaviour*, code-architect MF-1/MF-2).
//
// prev==nil models a cold key. nowUS is the (injected, in tests) server clock
// in microseconds. It returns the new state to persist plus the result; the
// caller persists on BOTH allow and deny, exactly like the script.
func computeBucket(prev *bucketState, rps float64, burst, requested int, nowUS int64) (bucketState, bucketResult) {
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

	res := bucketResult{}
	if tokens >= float64(reqMicro) {
		tokens -= float64(reqMicro)
		res.allowed = true
	} else {
		deficit := float64(reqMicro) - tokens
		// ceil mirrors the Lua math.ceil exactly; rps >= minRPS (validated by
		// NewLimiter) keeps this within int64 range.
		ms := int64(math.Ceil(deficit / (rps * 1000.0)))
		if ms < minRetryAfterMS {
			ms = minRetryAfterMS
		}
		res.retryAfterMS = ms
	}

	// Mirror Lua's math.floor before persisting (lossless integer HSET).
	// tokens is finite and in [0, capMicro] here, so the int64 conversion is
	// exact (math is stdlib — importing it does not affect hermeticity, which
	// only forbids infra/3rd-party deps).
	return bucketState{tokensMicro: int64(math.Floor(tokens)), tsUS: ts}, res
}

// errScriptAnomaly marks a nil / wrong-shape reply from a script that is
// contractually "never nil". It is distinct from an Eval transport error so
// the Limiter can fail-open but account it separately (code-architect OQ-A).
var errScriptAnomaly = errors.New("ratelimit: token-bucket script returned nil or malformed reply")

// decodeResult defensively decodes the {allowed, retry_after_ms} reply.
// go-redis decodes Lua integers as int64; we also accept int and numeric
// strings so a future client decoder change cannot silently break this.
// Any other shape → errScriptAnomaly (handled as fail-open + anomaly metric).
func decodeResult(v any) (bucketResult, error) {
	arr, ok := v.([]any)
	if !ok || len(arr) != 2 {
		return bucketResult{}, errScriptAnomaly
	}
	allowed, ok1 := toInt64(arr[0])
	retry, ok2 := toInt64(arr[1])
	if !ok1 || !ok2 {
		return bucketResult{}, errScriptAnomaly
	}
	if retry < 0 {
		retry = 0
	}
	return bucketResult{allowed: allowed == 1, retryAfterMS: retry}, nil
}

func toInt64(v any) (int64, bool) {
	switch n := v.(type) {
	case int64:
		return n, true
	case int:
		return int64(n), true
	case float64:
		// RESP3 / a future client decoder may surface a Lua integer as a
		// float64; accept it only if it is integral (a fractional value is a
		// genuine anomaly, not a benign type difference).
		if n == math.Trunc(n) {
			return int64(n), true
		}
		return 0, false
	case string:
		i, err := strconv.ParseInt(n, 10, 64)
		return i, err == nil
	default:
		return 0, false
	}
}

// msToDuration converts a non-negative millisecond count to a Duration.
func msToDuration(ms int64) time.Duration {
	if ms < 0 {
		ms = 0
	}
	return time.Duration(ms) * time.Millisecond
}
