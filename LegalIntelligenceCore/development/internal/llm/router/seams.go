// Package router is the LIC-internal LLM Provider Router (LIC-TASK-019,
// llm-provider-abstraction.md §2, error-handling.md §4–§6). It implements
// port.ProviderRouterPort: it builds the per-agent provider chain
// (primary + global fallback, deduplicated), enforces the per-provider
// in-process rate limit before the wire call, retries once on the same
// provider for retryable errors, falls back on FallbackEligible errors,
// and surfaces ALL_PROVIDERS_FAILED when the chain is exhausted. It also
// owns the in-memory healthy registry and the background health-check
// goroutine (§2.3).
//
// Hermeticity: like every internal/llm/* sibling (claude/openai/gemini/
// ratelimit/cost) this package imports ONLY the standard library and
// internal/domain/{port,model}. The three external collaborators — the
// rate limiter, the cost & usage tracker, and the Prometheus metric vecs —
// are inverted behind the consumer-side seams declared in this file
// (RateLimiter, UsageTracker, Metrics), each with a zero-dependency noop
// default so the Router is usable in unit tests and before LIC-TASK-047
// wires the concrete *ratelimit.Limiter / *cost.Tracker / *metrics.LLMMetrics
// adapters. A prometheus / ratelimit / cost / tracer import here would break
// the invariant that no internal/llm/* package imports its concrete infra
// before app-wiring (mirrors ratelimit's Observer and cost's Recorder).
package router

import (
	"context"
	"time"

	"contractpro/legal-intelligence-core/internal/domain/model"
	"contractpro/legal-intelligence-core/internal/domain/port"
)

// RateLimiter is the consumer-side seam over the per-provider token bucket.
// *ratelimit.Limiter.Wait satisfies it exactly; the
// `var _ RateLimiter = (*ratelimit.Limiter)(nil)` assertion and the concrete
// injection belong to app-wiring (LIC-TASK-047), NOT here — adding a
// ratelimit import would break hermeticity (same rule ratelimit applies to
// its own LuaEvaluator seam).
//
// Contract (llm-provider-abstraction.md §2.1 / ratelimit.go godoc): Wait
// blocks until a token is available within ctx and returns nil; it returns
// a *port.LLMProviderError{Code:RATE_LIMIT} wrapping ctx.Err() when ctx
// expires/cancels first, and a *port.LLMProviderError{Code:MALFORMED_REQUEST}
// for an unconfigured provider. The Router treats any ctx-derived Wait error
// as a chain-aborting condition (code-architect MF-1), never a fallback
// trigger.
type RateLimiter interface {
	Wait(ctx context.Context, providerID port.LLMProviderID) error
}

// noopRateLimiter never blocks — a usable default before LIC-TASK-047 wires
// the Redis-backed *ratelimit.Limiter (mirrors ratelimit.noopObserver /
// cost.noopRecorder). With it the Router degrades to "no in-process rate
// limiting", which is fail-open by the same rationale ratelimit itself uses.
type noopRateLimiter struct{}

func (noopRateLimiter) Wait(context.Context, port.LLMProviderID) error { return nil }

var _ RateLimiter = noopRateLimiter{}

// CallOutcome is the lic_llm_calls_total{outcome} label value. It is a local
// mirror of cost.Outcome (itself a mirror of metrics.LLMCallOutcome,
// observability.md §3.4) — declared here, not imported, so the package stays
// hermetic; seams_test.go pins the four wire strings so the mirror cannot
// silently drift from the SSOT (identical pattern to cost.Outcome).
type CallOutcome string

const (
	OutcomeSuccess  CallOutcome = "success"
	OutcomeRepair   CallOutcome = "repair"
	OutcomeFail     CallOutcome = "fail"
	OutcomeFallback CallOutcome = "fallback"
)

// UsageTracker is the consumer-side seam over the Cost & Usage Tracker
// (cost.Tracker). It mirrors the Tracker's ObserveSuccess / ObserveCall
// split (code-architect MF-5): a successful Complete MUST go through
// ObserveSuccess (which records the five usage families AND
// lic_llm_calls_total{success} — omitting it undercounts calls_total by the
// entire success volume) while every non-usage terminal outcome goes
// through ObserveCall.
//
// The signatures are cost-free (port/model types + the local CallOutcome,
// never cost.Usage / cost.Outcome) so Router unit tests need no real
// *cost.Tracker and no pricing table. The LIC-TASK-047 adapter maps
// ObserveSuccess → cost.Tracker.ObserveSuccess (building cost.Usage with the
// off-by-1000-critical Latency = time.Duration(resp.LatencyMs)*time.Millisecond
// conversion — see cost/CLAUDE.md forward-req #3) and ObserveCall →
// cost.Tracker.ObserveCall(..., cost.Outcome(outcome)).
type UsageTracker interface {
	// ObserveSuccess records one successful Complete: the five usage
	// families + lic_llm_calls_total{outcome=success}. resp carries the
	// token counts, model and latency the 047 adapter forwards verbatim.
	ObserveSuccess(provider port.LLMProviderID, mdl string, agent model.AgentID, resp port.CompletionResponse)

	// ObserveCall records a non-usage terminal outcome
	// (fail|fallback|repair) into lic_llm_calls_total only.
	ObserveCall(provider port.LLMProviderID, mdl string, agent model.AgentID, outcome CallOutcome)
}

// noopUsageTracker is the zero-dependency default (mirrors
// ratelimit.noopObserver / cost.noopRecorder).
type noopUsageTracker struct{}

func (noopUsageTracker) ObserveSuccess(port.LLMProviderID, string, model.AgentID, port.CompletionResponse) {
}
func (noopUsageTracker) ObserveCall(port.LLMProviderID, string, model.AgentID, CallOutcome) {}

var _ UsageTracker = noopUsageTracker{}

// HealthState is the lic_llm_provider_health_status{state} label value
// (llm-provider-abstraction.md §2.3): exactly one of the three is "active"
// (gauge=1) per provider at any time, the other two are 0.
type HealthState string

const (
	HealthHealthy   HealthState = "healthy"
	HealthUnhealthy HealthState = "unhealthy"
	HealthPermanent HealthState = "permanent"
)

// Metrics is the seam over the four router-owned Prometheus vecs declared
// centrally in metrics/llm.go (acceptance criteria 8). Typed parameters
// (never raw caller strings) keep the {from,to,agent} / {provider,code} /
// {provider,state} cardinality bounded exactly as observability.md §3.10
// budgets — the 047 adapter only .String()s them. Mirrors the
// "one seam, grouped signals" choice in ratelimit.Observer / cost.Recorder.
type Metrics interface {
	// ProviderFallback → lic_llm_provider_fallback_total{from,to,agent},
	// incremented once when a non-primary provider serves the request.
	ProviderFallback(from, to port.LLMProviderID, agent model.AgentID)

	// ProviderSkippedUnhealthy → lic_llm_provider_skipped_unhealthy_total
	// {provider}, incremented once per skip at the top of a chain iteration
	// (code-architect MF-3: never as a side-effect of a later failure).
	ProviderSkippedUnhealthy(provider port.LLMProviderID)

	// ProviderFailed → lic_llm_provider_failed_total{provider,code},
	// incremented once per terminal provider failure in a chain iteration.
	ProviderFailed(provider port.LLMProviderID, code port.LLMErrorCode)

	// ProviderHealthState → lic_llm_provider_health_status{provider,state}:
	// the 047 adapter sets the gauge for `state` to 1 and the other two
	// states for the same provider to 0. Called on every registry state
	// transition.
	ProviderHealthState(provider port.LLMProviderID, state HealthState)
}

// noopMetrics is the zero-dependency default (mirrors
// ratelimit.noopObserver / cost.noopRecorder).
type noopMetrics struct{}

func (noopMetrics) ProviderFallback(port.LLMProviderID, port.LLMProviderID, model.AgentID) {}
func (noopMetrics) ProviderSkippedUnhealthy(port.LLMProviderID)                             {}
func (noopMetrics) ProviderFailed(port.LLMProviderID, port.LLMErrorCode)                    {}
func (noopMetrics) ProviderHealthState(port.LLMProviderID, HealthState)                     {}

var _ Metrics = noopMetrics{}

// Deps bundles the three injectable seams + the optional test clocks so
// NewProviderRouter has one optional-deps parameter instead of five. Any
// nil field degrades to its zero-dependency default (noop seams, real
// time/sleep) so the common production path passes only the three adapters
// and tests override exactly what they need.
type Deps struct {
	RateLimiter  RateLimiter
	UsageTracker UsageTracker
	Metrics      Metrics

	// now and sleep are injected only by tests for deterministic, -race
	// clean backoff and registry-timestamp behaviour. Production leaves
	// them nil → time.Now / a ctx-aware time.Timer sleep.
	now   func() time.Time
	sleep func(ctx context.Context, d time.Duration) error
}

func (d Deps) withDefaults() Deps {
	if d.RateLimiter == nil {
		d.RateLimiter = noopRateLimiter{}
	}
	if d.UsageTracker == nil {
		d.UsageTracker = noopUsageTracker{}
	}
	if d.Metrics == nil {
		d.Metrics = noopMetrics{}
	}
	if d.now == nil {
		d.now = time.Now
	}
	if d.sleep == nil {
		d.sleep = sleepCtx
	}
	return d
}

// sleepCtx blocks for d or until ctx is done, whichever is first. It returns
// ctx.Err() if the context fired before the timer (so the Router aborts the
// chain on a dead ctx instead of waiting out a useless backoff), nil
// otherwise. A reusable single-shot timer with Go 1.23+ Stop semantics
// (go.mod go 1.26.1 → no manual channel drain), mirroring the ctx-aware
// sleep pattern in ratelimit.go.
func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
