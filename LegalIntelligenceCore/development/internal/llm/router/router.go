package router

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"contractpro/legal-intelligence-core/internal/domain/model"
	"contractpro/legal-intelligence-core/internal/domain/port"
)

// Same-provider retry backoff schedule (error-handling.md §4.3). Applied
// once, before the single same-provider retry of a Retryable error. 429
// uses the provider's Retry-After when present, else rateLimitDefaultBackoff.
const (
	// transientBackoff is the 5xx / network / timeout wait.
	transientBackoff = 200 * time.Millisecond
	// overloadedBackoff is the Anthropic 529 wait.
	overloadedBackoff = 500 * time.Millisecond
	// rateLimitDefaultBackoff is the 429 wait when no Retry-After header.
	rateLimitDefaultBackoff = 1 * time.Second
)

// unknownCode labels lic_llm_provider_failed_total when an adapter violates
// the port.LLMProviderPort invariant and returns a non-typed error. It is a
// fixed sentinel (bounded cardinality) that makes the invariant breach
// visible on dashboards rather than silently dropped (code-architect MF-1:
// a missing classification degrades to "fail this provider, no fallback").
const unknownCode port.LLMErrorCode = "UNKNOWN"

// ProviderRouter implements port.ProviderRouterPort. It is immutable after
// NewProviderRouter except for the mutex-guarded healthRegistry, so the
// parallel errgroup agent pipeline can share one instance without locking
// the hot path (the providers/config maps are read-only).
type ProviderRouter struct {
	providers map[port.LLMProviderID]port.LLMProviderPort
	cfg       RouterConfig
	registry  *healthRegistry

	rl    RateLimiter
	usage UsageTracker
	mx    Metrics

	now   func() time.Time
	sleep func(ctx context.Context, d time.Duration) error

	// orderedProviderIDs is a stable (sorted) iteration order for the
	// background sweep so logs/tests are deterministic (Go map order is
	// randomised). Read-only after construction.
	orderedProviderIDs []port.LLMProviderID

	// Background health-loop lifecycle (see health.go). lifeMu guards the
	// started/stopped/stopFn trio so concurrent Start/Stop from different
	// goroutines is data-race-free (two sync.Once establish no
	// happens-before with each other — golang-pro S-1); wg lets Stop block
	// until the goroutine has fully exited (no leak under -race).
	lifeMu  sync.Mutex
	started bool
	stopped bool
	stopFn  context.CancelFunc
	wg      sync.WaitGroup
}

// Compile-time assertion that ProviderRouter satisfies the port contract.
var _ port.ProviderRouterPort = (*ProviderRouter)(nil)

// NewProviderRouter validates the wiring and assembles the router. It
// fails fast (like NewLimiter / NewTracker / kvstore.NewClient) so a
// misconfiguration is a startup error, not a runtime surprise on the first
// contract that happens to route through the broken provider:
//
//   - providers must be non-empty;
//   - FallbackOrder must be non-empty and every entry must be a registered
//     provider with no duplicates;
//   - AgentPrimary must cover every model.AllAgentIDs() and each value must
//     be a registered provider.
//
// deps with nil seams degrade to the zero-dependency noop defaults so the
// router is usable before LIC-TASK-047 wires the concrete adapters.
func NewProviderRouter(
	providers map[port.LLMProviderID]port.LLMProviderPort,
	cfg RouterConfig,
	deps Deps,
) (*ProviderRouter, error) {
	if len(providers) == 0 {
		return nil, errors.New("router: providers must contain at least one provider")
	}
	for id, p := range providers {
		if id == "" {
			return nil, errors.New("router: empty provider id in providers map")
		}
		if p == nil {
			return nil, fmt.Errorf("router: provider %q is nil", id)
		}
	}

	if len(cfg.FallbackOrder) == 0 {
		return nil, errors.New("router: RouterConfig.FallbackOrder must contain at least one provider")
	}
	seen := make(map[port.LLMProviderID]struct{}, len(cfg.FallbackOrder))
	for _, id := range cfg.FallbackOrder {
		if _, ok := providers[id]; !ok {
			return nil, fmt.Errorf("router: FallbackOrder references unregistered provider %q", id)
		}
		if _, dup := seen[id]; dup {
			return nil, fmt.Errorf("router: FallbackOrder contains duplicate provider %q", id)
		}
		seen[id] = struct{}{}
	}

	if len(cfg.AgentPrimary) == 0 {
		return nil, errors.New("router: RouterConfig.AgentPrimary must be set for every agent")
	}
	for _, agent := range model.AllAgentIDs() {
		primary, ok := cfg.AgentPrimary[agent]
		if !ok {
			return nil, fmt.Errorf("router: RouterConfig.AgentPrimary missing agent %q", agent)
		}
		if _, ok := providers[primary]; !ok {
			return nil, fmt.Errorf("router: AgentPrimary[%q]=%q is not a registered provider", agent, primary)
		}
	}

	d := deps.withDefaults()

	ids := make([]port.LLMProviderID, 0, len(providers))
	for id := range providers {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })

	return &ProviderRouter{
		providers:          providers,
		cfg:                cfg,
		registry:           newHealthRegistry(ids, d.Metrics, d.now),
		rl:                 d.RateLimiter,
		usage:              d.UsageTracker,
		mx:                 d.Metrics,
		now:                d.now,
		sleep:              d.sleep,
		orderedProviderIDs: ids,
	}, nil
}

// chainFor builds the effective provider chain for an agent: the agent's
// primary first, then the global fallback order with the primary
// deduplicated (llm-provider-abstraction.md §2.1). A request for an agent
// absent from AgentPrimary (NewProviderRouter rejects that, so this is a
// defensive belt-and-braces path) yields an empty chain → Complete returns
// MALFORMED_REQUEST.
func (r *ProviderRouter) chainFor(agent model.AgentID) (primary port.LLMProviderID, chain []port.LLMProviderID) {
	primary, ok := r.cfg.AgentPrimary[agent]
	if !ok {
		return "", nil
	}
	chain = make([]port.LLMProviderID, 0, len(r.cfg.FallbackOrder)+1)
	chain = append(chain, primary)
	for _, id := range r.cfg.FallbackOrder {
		if id != primary {
			chain = append(chain, id)
		}
	}
	return primary, chain
}

// Complete walks the per-agent chain primary → fallback (§2.1). For each
// provider: skip if the registry says unhealthy, block on the rate limiter,
// then attempt the call with one same-provider retry on a Retryable error.
// On success it returns the winning provider in PrimaryCallResult (the
// caller stashes UsedProvider for the sticky repair, OQ-10). On a
// FallbackEligible error it advances; on a non-fallback error
// (CONTEXT_TOO_LONG / MALFORMED_REQUEST, or an unclassified error per
// MF-1) it returns immediately; on chain exhaustion it returns
// ALL_PROVIDERS_FAILED wrapping the last error.
func (r *ProviderRouter) Complete(ctx context.Context, req port.CompletionRequest) (port.PrimaryCallResult, error) {
	primary, chain := r.chainFor(req.AgentID)
	if len(chain) == 0 {
		return port.PrimaryCallResult{}, port.NewLLMProviderError(port.LLMErrorMalformedRequest,
			fmt.Errorf("router: no provider chain for agent %q", req.AgentID))
	}

	var lastErr error
	for i, id := range chain {
		if !r.registry.isHealthy(id) {
			r.mx.ProviderSkippedUnhealthy(id) // MF-3: counted ONLY here
			// Do NOT overwrite lastErr: the §2.1 / §6 pseudocode is
			// "skip + metric" only. Preserving the prior real provider
			// error keeps ALL_PROVIDERS_FAILED.Wrapped pointed at the
			// genuine root cause instead of a synthetic skip marker
			// (code-reviewer MEDIUM-1). Only seed it if nothing failed
			// before this skip, so chain exhaustion still wraps something.
			if lastErr == nil {
				lastErr = port.NewLLMProviderError(port.LLMErrorServerError,
					fmt.Errorf("router: provider %q skipped (unhealthy)", id))
			}
			continue
		}

		// Rate limit. Any ctx-derived Wait error aborts the WHOLE chain
		// (code-architect MF-1): a dead parent ctx must not burn the rest
		// of the chain and mask the cancellation as ALL_PROVIDERS_FAILED.
		if err := r.rl.Wait(ctx, id); err != nil {
			if ctx.Err() != nil {
				return port.PrimaryCallResult{}, port.NewLLMProviderError(port.LLMErrorRateLimit, ctx.Err())
			}
			// Non-ctx Wait failure == unconfigured-provider MALFORMED
			// (a wiring bug for this provider only): skip it, keep going.
			// recordFailure is deliberately NOT called — a missing bucket
			// is a LIC wiring bug, not provider ill-health, so it must not
			// poison the health registry (documented exclusion in
			// CLAUDE.md). But ObserveCall(fail) IS emitted so the
			// LICAllProvidersFailing alert (keys on outcome="fail") still
			// fires on a persistently mis-wired primary (code-reviewer
			// MEDIUM-2 — keeps the MF-5 "every failed attempt → fail"
			// invariant true).
			lastErr = err
			if pe, ok := port.AsLLMProviderError(err); ok {
				r.mx.ProviderFailed(id, pe.Code)
			}
			r.usage.ObserveCall(id, req.Model, req.AgentID, OutcomeFail)
			continue
		}

		resp, err := r.attempt(ctx, id, req)
		if err == nil {
			if i > 0 {
				r.mx.ProviderFallback(primary, id, req.AgentID)
			}
			r.usage.ObserveSuccess(id, resp.Model, req.AgentID, resp)
			return port.PrimaryCallResult{Response: resp, UsedProvider: id}, nil
		}

		pe, ok := port.AsLLMProviderError(err)
		if !ok {
			// Adapter invariant breach: a non-typed error. Degrade to
			// "fail this provider, no fallback" (code-architect MF-1) —
			// without a classification we cannot safely assume it is
			// fallback-eligible.
			r.mx.ProviderFailed(id, unknownCode)
			r.registry.recordFailure(id, nil, err)
			r.usage.ObserveCall(id, req.Model, req.AgentID, OutcomeFail)
			return port.PrimaryCallResult{}, err
		}

		// (1) metric, (2) registry side-effect, (3) flag-driven decision —
		// in that order; side-effects never alter control flow (MF-2).
		r.mx.ProviderFailed(id, pe.Code)
		r.registry.recordFailure(id, pe, nil)
		lastErr = pe

		if !pe.FallbackEligible {
			// CONTEXT_TOO_LONG / MALFORMED_REQUEST — fatal, no fallback.
			r.usage.ObserveCall(id, req.Model, req.AgentID, OutcomeFail)
			return port.PrimaryCallResult{}, pe
		}
		// FallbackEligible (incl. Retryable=false: INVALID_API_KEY,
		// QUOTA_EXCEEDED, CONTENT_POLICY) → advance to the next provider.
		r.usage.ObserveCall(id, req.Model, req.AgentID, OutcomeFail)
	}

	return port.PrimaryCallResult{}, port.NewLLMProviderError(port.LLMErrorAllProvidersFailed, lastErr)
}

// attempt issues one provider.Complete and, on a Retryable typed error,
// performs exactly ONE same-provider retry after the §4.3 backoff
// (acceptance criterion 3 "retry на same provider 1 раз"). The adapters
// deliberately do NOT retry (see claude/provider.go godoc) — level-1 retry
// is the Router's job (error-handling.md §4.1). Metrics, registry and cost
// are recorded by the caller ONCE per provider chain-iteration on the
// terminal error; the retry is an internal HTTP-resilience detail and must
// not double-count (code-architect resolution).
func (r *ProviderRouter) attempt(ctx context.Context, id port.LLMProviderID, req port.CompletionRequest) (port.CompletionResponse, error) {
	provider := r.providers[id]

	resp, err := provider.Complete(ctx, req)
	if err == nil {
		return resp, nil
	}
	pe, ok := port.AsLLMProviderError(err)
	if !ok || !pe.Retryable {
		return port.CompletionResponse{}, err
	}

	// One backoff, ctx-aware: a cancelled/expired ctx during backoff
	// short-circuits the retry and returns the ctx error so Complete's
	// MF-1 ctx guard aborts the chain instead of looping a dead context.
	if serr := r.sleep(ctx, backoffFor(pe)); serr != nil {
		return port.CompletionResponse{}, port.NewLLMProviderError(port.LLMErrorTimeout, serr)
	}

	resp, err = provider.Complete(ctx, req)
	if err == nil {
		return resp, nil
	}
	return port.CompletionResponse{}, err
}

// backoffFor maps a Retryable error to its §4.3 wait. RATE_LIMIT honours
// the provider's Retry-After when present (capped defensively at the
// default to avoid a hostile header parking the agent), else 1s; 529 →
// 500ms; everything else (5xx / network / timeout) → 200ms.
func backoffFor(pe *port.LLMProviderError) time.Duration {
	switch pe.Code {
	case port.LLMErrorRateLimit:
		if pe.RetryAfter != nil && *pe.RetryAfter > 0 {
			return *pe.RetryAfter
		}
		return rateLimitDefaultBackoff
	case port.LLMErrorOverloaded:
		return overloadedBackoff
	default:
		return transientBackoff
	}
}
