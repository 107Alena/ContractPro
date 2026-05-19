package idempotency

import (
	"context"
	"errors"
	"time"

	"contractpro/legal-intelligence-core/internal/domain/port"
)

// luaSetNXOrGet is the atomic SET-NX-EX-or-return-existing primitive (build-
// spec D4). A naive kvstore.SetNX(false)→kvstore.Get has a TOCTOU: the key may
// expire (or transition PROCESSING→COMPLETED) in the gap, yielding a stale or
// miss reply that could cause a double-analysis or a wrong NACK. This single
// Lua script via kvstore.Eval (ops.go:98, kvstore/CLAUDE.md:71-79 — the
// adapter owns its Lua) is one round-trip with zero TOCTOU.
//
//	KEYS[1] = idempotency key
//	ARGV[1] = "PROCESSING"     (the value to set if absent)
//	ARGV[2] = ttl seconds (integer string)
//	Returns: {1, ""}        when the key was set (acquired; absent before)
//	         {0, <value>}   when the key already existed (its current value)
//
// SET ... NX EX returns a Lua status-or-nil; the `if` is truthy ONLY on a
// successful set, so there is no separate EXISTS. The GET in the else branch
// is inside the same script ⇒ atomic with the failed SET NX (Redis Lua is
// atomic): zero TOCTOU. A vanished key between the failed SET NX and GET is
// impossible inside one Lua call; defensively, if GET surfaces Lua nil (the
// keyspace-eviction maxmemory corner), the decoder + the bounded single retry
// (D4.1) handle it — never an unbounded loop.
const luaSetNXOrGet = `if redis.call('SET', KEYS[1], ARGV[1], 'NX', 'EX', ARGV[2]) then
  return {1, ''}
else
  return {0, redis.call('GET', KEYS[1])}
end`

// errEvalShape is the package-local decode-shape fault (build-spec D4): the
// Eval returned something structurally unexpected (a Lua nil — the
// ops.go:95-97 case — or any non {int64,string} shape). It is NOT a
// model.ErrorCode (R4); the caller (SetNX/CheckAndAcquire) treats it as a
// transport-class error (R1) — an infra anomaly, not a business state.
var errEvalShape = errors.New("idempotency: unexpected EVAL result shape")

// luaArgs builds the KEYS/ARGV passed to RedisSeam.Eval for luaSetNXOrGet.
// TTL is converted to integer seconds; if ttl < time.Second use 1 (Redis EX
// minimum) — defensive, production TTLs are 150s/24h/25h. The value comes from
// the method's ttl param: the adapter NEVER passes a hardcoded TTL (R3 — TTLs
// are per-call).
func luaArgs(key string, ttl time.Duration) (keys []string, args []any) {
	secs := int(ttl / time.Second)
	if secs < 1 {
		secs = 1
	}
	return []string{key}, []any{string(port.IdempotencyProcessing), secs}
}

// decodeEvalResult decodes the kvstore.Eval result of luaSetNXOrGet (build-
// spec D4). kvstore.Eval returns any (ops.go:98); a Lua table {n, v} surfaces
// (via go-redis) as []interface{}{int64, string}.
//
//   - []interface{} of length 2 with [0] an int64:
//     [0]==1 ⇒ (IdempotencyAbsent, true, nil)        — acquired.
//     [0]==0 ⇒ (parseStatus([1] string), false, nil) — present (D5).
//   - nil (the ops.go:95-97 Lua-nil case) OR any other shape ⇒
//     (IdempotencyAbsent, false, errEvalShape) — a decode-shape fault the
//     caller treats as transport-class (R1).
//
// The [0]==0-with-nil/empty-and-unparseable corner (keyspace eviction) is
// surfaced to the caller as the second return distinguishing acquired vs
// present plus a needsRetry flag the bounded single-retry helper consumes
// (D4.1) — decodeEvalResult itself never loops.
func decodeEvalResult(res any) (existing port.IdempotencyStatus, acquired bool, needsRetry bool, err error) {
	arr, ok := res.([]interface{})
	if !ok || len(arr) != 2 {
		return port.IdempotencyAbsent, false, false, errEvalShape
	}
	code, ok := arr[0].(int64)
	if !ok {
		return port.IdempotencyAbsent, false, false, errEvalShape
	}
	switch code {
	case 1:
		return port.IdempotencyAbsent, true, false, nil
	case 0:
		raw, isStr := arr[1].(string)
		if !isStr {
			// {0, nil}: GET surfaced Lua nil (the keyspace-eviction
			// corner). Signal a bounded single retry (D4.1) — a vanished
			// key means "absent now", the new-event path.
			return port.IdempotencyAbsent, false, true, nil
		}
		if raw == "" {
			// {0, ""}: the stored value is the empty string — never a
			// valid status (the empty string is the absent sentinel and is
			// never STORED, D5). Treat as the eviction corner: bounded
			// single retry (D4.1).
			return port.IdempotencyAbsent, false, true, nil
		}
		return parseStatus(raw), false, false, nil
	default:
		return port.IdempotencyAbsent, false, false, errEvalShape
	}
}

// evalSetNXOrGet runs luaSetNXOrGet once, decodes it, and applies the D4.1
// bounded single retry exactly ONCE on the keyspace-eviction Lua-nil corner
// ({0, nil}/{0, ""}). If the second attempt is STILL the eviction corner it
// falls through to D5's defensive treat-as-PROCESSING-exists (never a re-run
// path). The retry bound is the constant 1 (no config knob — YAGNI, D4.1).
// This helper is local to SetNX/CheckAndAcquire; it is NOT the heartbeat.
func (g *Guard) evalSetNXOrGet(
	ctx context.Context, key string, ttl time.Duration,
) (existing port.IdempotencyStatus, acquired bool, err error) {
	const maxAttempts = 2 // 1 initial + 1 bounded retry (D4.1).
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		keys, args := luaArgs(key, ttl)
		res, evalErr := g.redis.Eval(ctx, luaSetNXOrGet, keys, args...)
		if evalErr != nil {
			// Transport error from Eval (Redis unreachable, context
			// error, etc.) — surface verbatim (R1/R4); never retry a
			// transport fault here (the heartbeat is the only retry loop).
			return port.IdempotencyAbsent, false, evalErr
		}
		status, ok, needsRetry, decErr := decodeEvalResult(res)
		if decErr != nil {
			return port.IdempotencyAbsent, false, decErr
		}
		if !needsRetry {
			return status, ok, nil
		}
		// needsRetry: the keyspace-eviction corner. Retry exactly once;
		// if the LAST attempt is still the corner, fall through to D5's
		// defensive treat-as-PROCESSING-exists (the key existed but its
		// value is unusable ⇒ "something owns this slot").
		if attempt == maxAttempts {
			return port.IdempotencyProcessing, false, nil
		}
	}
	// Unreachable: the loop always returns. Defensive belt-and-suspenders.
	return port.IdempotencyProcessing, false, nil
}
