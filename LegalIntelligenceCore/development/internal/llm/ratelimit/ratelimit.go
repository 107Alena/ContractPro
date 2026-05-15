// Package ratelimit is the per-provider token-bucket rate limiter for the
// Legal Intelligence Core (LIC-TASK-017, llm-provider-abstraction.md §3.1–§3.2).
//
// The Provider Router (LIC-TASK-019) calls Limiter.Wait(ctx, providerID) as
// its r.rateLimit(ctx, providerID) hook before every wire call: Wait blocks
// until a token is available within the caller's ctx deadline (normal
// backpressure — NOT a fallback trigger) and returns nil. It only returns an
// error when ctx expires first. The bucket itself is an atomic Lua script in
// Redis (script.go); exponential backoff between providers is the Router's
// concern (§3.2), deliberately not implemented here.
//
// Hermeticity: like every internal/llm/* sibling this package imports only the
// standard library and internal/domain/port. Redis access is inverted behind
// the LuaEvaluator seam (satisfied by *kvstore.Client) and observability
// behind the Observer seam; both are injected by app-wiring (LIC-TASK-047),
// which also owns the compile-time `var _ LuaEvaluator = (*kvstore.Client)(nil)`
// assertion and the Prometheus/logger Observer adapter. No prometheus / kvstore
// import here, by design (code-architect MF-3 / note 2).
package ratelimit

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"time"

	"contractpro/legal-intelligence-core/internal/domain/port"
)

// LuaEvaluator is the consumer-side Redis seam: the single primitive the
// token bucket needs. *kvstore.Client.Eval satisfies it exactly. Declared
// here (not imported from kvstore) so the package stays hermetic and unit
// tests run against an in-memory evaluator with no live Redis. The name is
// deliberately domain-narrow rather than "ScriptRunner", which would shadow
// redis.Scripter conceptually (code-architect MF-3).
type LuaEvaluator interface {
	Eval(ctx context.Context, script string, keys []string, args ...any) (any, error)
}

// Observer receives the limiter's three observable signals. They go to
// different sinks (a Prometheus counter, a sampled WARN log, a distinct
// anomaly counter) but are grouped into one seam so app-wiring injects a
// single adapter and tests need one fake (code-architect note 3 — choice
// recorded here per the kvstore CLAUDE.md convention).
//
// Sampling/log-rate policy lives in the wiring adapter (LIC-TASK-047), not
// here: the limiter calls these unconditionally on every occurrence.
type Observer interface {
	// RateLimited fires once per denied evaluation → lic_llm_rate_limited_total
	// {provider}. Strictly denied-only; fail-open and anomalies must NOT
	// increment it or the metric lies (acceptance criteria).
	RateLimited(provider string)

	// FailOpen fires when an Eval transport call failed (Redis unreachable)
	// and the limiter therefore allowed the request through. err is the raw
	// Eval error for a sampled WARN.
	FailOpen(provider string, err error)

	// ScriptAnomaly fires when the script returned nil / a malformed reply
	// (a script/data bug, not an outage). Also fail-open, but a distinct
	// signal so dashboards/alerts don't conflate it with Redis outages.
	ScriptAnomaly(provider string, err error)
}

// noopObserver is the zero-dependency default so the limiter is usable (e.g.
// in tests and before LIC-TASK-047 wires Prometheus) without a nil check on
// every call.
type noopObserver struct{}

func (noopObserver) RateLimited(string)            {}
func (noopObserver) FailOpen(string, error)        {}
func (noopObserver) ScriptAnomaly(string, error)   {}

var _ Observer = noopObserver{}

// ProviderLimit is one provider's bucket parameters. RPS and Burst originate
// from config.LLMConfig (LIC_<PROVIDER>_RPS / LIC_<PROVIDER>_BURST — the
// configuration.md §2 SSOT, already loaded by config/llm.go). This package
// never reads env; the env→param mapping is LIC-TASK-047's job.
//
// DOC NOTE: architecture §3.2 and the LIC-TASK-017 acceptance text spell these
// LIC_LLM_RPS_<PROVIDER> / LIC_LLM_BURST_<PROVIDER>, which do NOT exist — the
// implemented SSOT is LIC_<PROVIDER>_RPS / LIC_<PROVIDER>_BURST. LIC-TASK-047
// MUST map from cfg.LLM.<Provider>.RPS/Burst, not the §3.2 spelling. Recorded
// in progress.md as a known doc discrepancy.
type ProviderLimit struct {
	RPS   float64
	Burst int
}

// Config is the per-provider parameter set the Limiter is built from.
type Config struct {
	Providers map[port.LLMProviderID]ProviderLimit
}

// Limiter is the aggregate the Router holds: it owns one TokenBucket per
// configured provider and exposes the blocking Wait the Router calls as
// r.rateLimit(ctx, providerID). Immutable after construction → safe for
// concurrent use by the parallel agent pipeline (errgroup).
type Limiter struct {
	buckets  map[port.LLMProviderID]*TokenBucket
	obs      Observer
	maxSleep time.Duration
	// randF is the jitter source. Contract: returns a value in [0,1) and MUST
	// be safe for concurrent use (the parallel agent pipeline shares one
	// Limiter). Default rand.Float64 (math/rand/v2 global, lock-free &
	// concurrency-safe); injectable for deterministic tests.
	randF func() float64
}

// minRPS is the smallest RPS NewLimiter accepts. Below it the derived
// retry-after / minSleep arithmetic could exceed int64 nanoseconds and wrap a
// time.Duration negative (golang-pro MF-1). 1e-6 (one request per ~11.6 days)
// is far below any real provider quota — config.validateProviderShape only
// guards rps>0, so this is the defensive backstop.
const minRPS = 1e-6

// maxSleepDefault caps a single denied-wait so a low-RPS misconfiguration
// (e.g. RPS=0.01 → retry_after ≈ 100s) cannot park a goroutine in one opaque
// sleep: the loop wakes, re-checks the bucket and ctx, and continues
// (code-architect MF-5). It does not change total wait semantics.
const maxSleepDefault = 2 * time.Second

// NewLimiter validates cfg and assembles one bucket per provider. It
// fails-fast (like kvstore.NewClient) on an empty config, an RPS below minRPS
// or a burst < 1 so a wiring bug is a startup error, not a runtime surprise.
// eval is required; a nil obs degrades to a no-op.
func NewLimiter(cfg Config, eval LuaEvaluator, obs Observer) (*Limiter, error) {
	if eval == nil {
		return nil, errors.New("ratelimit: LuaEvaluator is required")
	}
	if len(cfg.Providers) == 0 {
		return nil, errors.New("ratelimit: Config.Providers must contain at least one provider")
	}
	if obs == nil {
		obs = noopObserver{}
	}

	buckets := make(map[port.LLMProviderID]*TokenBucket, len(cfg.Providers))
	for id, pl := range cfg.Providers {
		if id == "" {
			return nil, errors.New("ratelimit: empty provider id in Config.Providers")
		}
		if pl.RPS < minRPS {
			return nil, fmt.Errorf("ratelimit: provider %q RPS must be >= %v, got %v", id, minRPS, pl.RPS)
		}
		if pl.Burst < 1 {
			return nil, fmt.Errorf("ratelimit: provider %q Burst must be >= 1, got %d", id, pl.Burst)
		}
		buckets[id] = NewTokenBucket(id, pl.RPS, pl.Burst, eval)
	}

	return &Limiter{
		buckets:  buckets,
		obs:      obs,
		maxSleep: maxSleepDefault,
		randF:    rand.Float64,
	}, nil
}

// Wait blocks until a token for providerID is available, returning nil. It is
// the Router's r.rateLimit(ctx, providerID) (llm-provider-abstraction.md §2.1):
//
//   - token available           → nil immediately
//   - denied                    → RateLimited(provider); sleep (retry-after +
//     full jitter, floored, capped, clamped to ctx) then retry
//   - ctx expires while waiting  → *port.LLMProviderError{Code:RATE_LIMIT,
//     Wrapped:ctx.Err()}. This is BOTH a *LLMProviderError (the §2.1 router's
//     non-deadline `err.(*LLMProviderError)` path is panic-safe) AND
//     errors.Is(_, context.DeadlineExceeded) via Unwrap (the §2.1 router's
//     deadline branch fires; acceptance "error wrapping context.DeadlineExceeded")
//   - unknown providerID         → *port.LLMProviderError{Code:MALFORMED}
//     (not retryable/fallback — a config bug; never a panic, so the router's
//     type assertion is safe)
//   - Redis unreachable / script anomaly → fail-OPEN: nil, after notifying the
//     Observer on the matching distinct signal (rate limiting must not be a
//     single point of failure for the pipeline)
func (l *Limiter) Wait(ctx context.Context, providerID port.LLMProviderID) error {
	bucket, ok := l.buckets[providerID]
	if !ok {
		return port.NewLLMProviderError(port.LLMErrorMalformedRequest,
			fmt.Errorf("ratelimit: no rate-limit bucket configured for provider %q", providerID))
	}

	prov := providerID.String()

	// One reusable timer for the denied-wait (Go 1.23+ Reset/Stop guarantee no
	// stale fire, so no manual channel drain — go.mod is go 1.26.1). Created
	// lazily so the common allowed-immediately path allocates nothing.
	var timer *time.Timer
	defer func() {
		if timer != nil {
			timer.Stop()
		}
	}()

	for {
		if err := ctx.Err(); err != nil {
			return port.NewLLMProviderError(port.LLMErrorRateLimit, err)
		}

		out, retryAfter, cause := bucket.allow(ctx)
		switch out {
		case outcomeAllowed:
			return nil
		case outcomeInfraError:
			// ctx can expire in the window between the loop-top ctx.Err()
			// check and the Eval round-trip: a real Redis client then returns
			// a transport error here. That is NOT a Redis outage — it is the
			// caller's deadline elapsing, and the §2.1 contract requires a
			// RATE_LIMIT error wrapping ctx.Err(), not a fail-open that would
			// silently mask the timeout as success (code-reviewer H1).
			if ctxErr := ctx.Err(); ctxErr != nil {
				return port.NewLLMProviderError(port.LLMErrorRateLimit, ctxErr)
			}
			l.obs.FailOpen(prov, cause)
			return nil
		case outcomeScriptAnomaly:
			l.obs.ScriptAnomaly(prov, cause)
			return nil
		case outcomeDenied:
			l.obs.RateLimited(prov)
			sleep := clampToDeadline(ctx, l.computeSleep(retryAfter, bucket.rps))
			// Less than the anti-busy-spin floor left before the deadline:
			// don't spin out the tail re-Evaling every few µs — give up now
			// (ctx is about to expire anyway) (golang-pro SF-1).
			if sleep < time.Millisecond {
				return port.NewLLMProviderError(port.LLMErrorRateLimit, deadlineCause(ctx))
			}
			if timer == nil {
				timer = time.NewTimer(sleep)
			} else {
				timer.Reset(sleep)
			}
			select {
			case <-ctx.Done():
				timer.Stop()
				return port.NewLLMProviderError(port.LLMErrorRateLimit, ctx.Err())
			case <-timer.C:
				// Re-evaluate on the next iteration. If ctx.Done() and timer.C
				// were both ready, Go may pick this branch; the loop-top
				// ctx.Err() check turns that into the RATE_LIMIT return on the
				// very next iteration (one extra bucket.allow at worst, which
				// the H1 infra-path guard also converts to RATE_LIMIT).
			}
		}
	}
}

// computeSleep turns the script's retry-after into the actual sleep:
// floored at minSleep (no busy-spin when retry_after rounds tiny), capped at
// maxSleep (MF-5), then full-jittered within [minSleep, upper] to break the
// thundering herd of parallel agents sharing one provider bucket. No
// exponential backoff — that is the Router's §3.2 concern.
func (l *Limiter) computeSleep(retryAfter time.Duration, rps float64) time.Duration {
	minSleep := time.Millisecond
	if half := time.Duration(float64(time.Second) / (2 * rps)); half > minSleep {
		minSleep = half
	}
	// The maxSleep cap (MF-5) wins even over the anti-busy-spin floor: a
	// pathologically low RPS (operator misconfig, e.g. 0.01 → minSleep 50s)
	// must still wake within maxSleep to re-check the bucket and ctx.
	if minSleep > l.maxSleep {
		minSleep = l.maxSleep
	}

	base := retryAfter
	// base < 0 only if a pathological retry-after overflowed int64 ns;
	// minRPS bounds that out, but treat it defensively as "very large".
	if base < minSleep || base < 0 {
		base = minSleep
	}
	if base > l.maxSleep {
		base = l.maxSleep
	}

	upper := time.Duration(float64(base) * 1.2)
	if upper > l.maxSleep {
		upper = l.maxSleep
	}
	if upper < minSleep {
		upper = minSleep
	}

	span := upper - minSleep
	if span <= 0 {
		// Degenerate only when minSleep is itself pinned to maxSleep, i.e. a
		// misconfigured sub-0.25 RPS: jitter collapses to a synchronous
		// maxSleep tick for all waiters. Acceptable — real provider RPS is
		// ≥10 (configuration.md), so this is an operator-misconfig corner,
		// not a steady-state thundering-herd path (code-reviewer L1).
		return minSleep
	}
	return minSleep + time.Duration(l.randF()*float64(span))
}

// clampToDeadline shrinks sleep so the limiter never sleeps past ctx's
// deadline (it would wake only to find ctx expired). No deadline → unchanged:
// the Router always passes a deadline'd ctx, so the no-deadline branch is a
// defensive fallback (an unbounded blocking wait, broken only by the maxSleep
// re-check loop), not a normal path.
func clampToDeadline(ctx context.Context, sleep time.Duration) time.Duration {
	dl, ok := ctx.Deadline()
	if !ok {
		return sleep
	}
	if remaining := time.Until(dl); remaining < sleep {
		return remaining
	}
	return sleep
}

// deadlineCause returns the most specific context error available; if ctx is
// not yet reporting one (deadline shrank sleep to <=0 a hair early), fall back
// to DeadlineExceeded so the router's errors.Is branch still fires.
func deadlineCause(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return context.DeadlineExceeded
}
