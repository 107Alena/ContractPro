package ratelimit

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"contractpro/legal-intelligence-core/internal/domain/port"
)

// outcome classifies a single token-bucket evaluation.
type outcome int

const (
	outcomeAllowed       outcome = iota // token granted
	outcomeDenied                       // bucket empty; retryAfter is meaningful
	outcomeInfraError                   // Eval transport failure (Redis down) → fail-open
	outcomeScriptAnomaly                // nil / malformed reply → fail-open, distinct metric
)

// TokenBucket is the per-provider Redis token bucket (LIC-TASK-017,
// llm-provider-abstraction.md §3.1). It owns no goroutine and holds no mutable
// state — all state lives in Redis under key lic:rate:{provider}, mutated
// atomically by tokenBucketScript. Safe for concurrent use.
//
// The shard suffix from §3.1 (lic:rate:{provider}:{shard}, shard=org_id_hash%4)
// is intentionally omitted: §3.1 marks it "(опц.)", the LIC-TASK-017 acceptance
// criteria freeze the key as lic:rate:{provider}, a single global per-provider
// bucket tracks the provider's aggregate RPS quota more accurately than a 4×
// split, and keeping org_id (even hashed) out of the key is 152-ФЗ PII
// minimisation (code-architect OQ-F).
type TokenBucket struct {
	provider port.LLMProviderID
	key      string
	rps      float64
	burst    int
	eval     LuaEvaluator
}

// NewTokenBucket builds a bucket for one provider. rps must be > 0 and burst
// >= 1 (validated by the caller, NewLimiter); the constructor does not
// re-validate — it is unexported-intent infrastructure assembled from an
// already-validated Config.
func NewTokenBucket(provider port.LLMProviderID, rps float64, burst int, eval LuaEvaluator) *TokenBucket {
	return &TokenBucket{
		provider: provider,
		key:      "lic:rate:" + provider.String(),
		rps:      rps,
		burst:    burst,
		eval:     eval,
	}
}

// allow runs the atomic script once and classifies the result. It never
// blocks (the blocking wait/retry loop is Limiter.Wait's job) and never
// returns a Go error: an Eval failure or anomalous reply is reported via the
// outcome so the caller can fail-open deterministically.
func (b *TokenBucket) allow(ctx context.Context) (out outcome, retryAfter time.Duration, cause error) {
	raw, err := b.eval.Eval(ctx, tokenBucketScript,
		[]string{b.key},
		formatFloat(b.rps),
		strconv.Itoa(b.burst),
		"1",
	)
	if err != nil {
		// Eval transport failure (Redis unreachable / ctx). The rate limiter
		// must not be a single point of failure for the whole LLM pipeline:
		// briefly over-calling a provider is less bad than a full analysis
		// outage (code-architect OQ-A; error-handling.md graceful degradation).
		return outcomeInfraError, 0, err
	}
	res, derr := decodeResult(raw)
	if derr != nil {
		// Script is contractually "never nil"; a nil/garbage reply is a
		// script/data bug, NOT "Redis down" — fail-open but account it on a
		// separate signal so it is not conflated with infra outages or with
		// lic_llm_rate_limited_total (code-architect OQ-A).
		return outcomeScriptAnomaly, 0, derr
	}
	if res.allowed {
		return outcomeAllowed, 0, nil
	}
	return outcomeDenied, msToDuration(res.retryAfterMS), nil
}

// formatFloat renders rps for ARGV with enough precision for fractional RPS
// (e.g. 0.5) without scientific notation, which Lua's tonumber accepts.
func formatFloat(f float64) string {
	return strconv.FormatFloat(f, 'f', -1, 64)
}

// String returns a stable identifier for logs/tests.
func (b *TokenBucket) String() string {
	return fmt.Sprintf("TokenBucket{provider=%s rps=%g burst=%d}", b.provider, b.rps, b.burst)
}
