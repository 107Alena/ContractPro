package metrics

import "github.com/prometheus/client_golang/prometheus"

// LLMMetrics — observability.md §3.4.
//
// The {provider, model, agent} triple is the canonical cardinality unit
// for LLM cost & usage attribution; observability.md §3.10 budgets ~405
// series here (3 providers × 3 models × 9 agents × 5 outcomes), within
// the per-instance ceiling.
type LLMMetrics struct {
	// CallsTotal — call count by outcome. `repair` increments on every
	// CompleteRepair invocation regardless of its sub-outcome
	// (granularity is in RepairOutcomeTotal on AgentMetrics).
	CallsTotal *prometheus.CounterVec

	// LatencySeconds — wall-clock LLM call latency.
	LatencySeconds *prometheus.HistogramVec

	// InputTokensTotal — billable, *uncached* input tokens.
	InputTokensTotal *prometheus.CounterVec

	// CachedTokensTotal — tokens served from the provider's prompt cache.
	// Critical for cost accuracy: omitting this label inflates Anthropic
	// cost by up to 10× on cache-hit requests
	// (llm-provider-abstraction.md §4.1). For OpenAI/Gemini in v1 this
	// counter stays at 0 (implicit cache not tracked).
	CachedTokensTotal *prometheus.CounterVec

	// OutputTokensTotal — generated tokens billed as output.
	OutputTokensTotal *prometheus.CounterVec

	// CostUSDTotal — running cost in USD per (provider, model, agent).
	CostUSDTotal *prometheus.CounterVec

	// ProviderFallbackTotal — incremented each time the router falls back
	// from `from` to `to` for a given agent.
	ProviderFallbackTotal *prometheus.CounterVec

	// ProviderSkippedUnhealthyTotal — counts skips due to unhealthy/permanent
	// state in the provider registry.
	ProviderSkippedUnhealthyTotal *prometheus.CounterVec

	// ProviderFailedTotal — per-provider failures labelled by LLMProviderError.Code.
	ProviderFailedTotal *prometheus.CounterVec

	// ProviderHealthStatus — current state from the health registry
	// (one series per provider, per state); always 0 or 1.
	ProviderHealthStatus *prometheus.GaugeVec

	// ProviderCircuitState — 0=closed, 1=half_open, 2=open.
	ProviderCircuitState *prometheus.GaugeVec

	// RateLimitedTotal — increments when the token-bucket denies a call.
	RateLimitedTotal *prometheus.CounterVec
}

func newLLMMetrics() *LLMMetrics {
	return &LLMMetrics{
		CallsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "lic_llm_calls_total",
			Help: "Total LLM calls by provider, model, agent, and outcome.",
		}, []string{"provider", "model", "agent", "outcome"}),

		LatencySeconds: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "lic_llm_latency_seconds",
			Help:    "LLM call latency in seconds.",
			Buckets: llmLatencyBuckets(),
		}, []string{"provider", "model", "agent"}),

		InputTokensTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "lic_llm_input_tokens_total",
			Help: "Billable uncached input tokens.",
		}, []string{"provider", "model", "agent"}),

		CachedTokensTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "lic_llm_cached_tokens_total",
			Help: "Tokens served from the provider's prompt cache (0 for OpenAI/Gemini in v1).",
		}, []string{"provider", "model", "agent"}),

		OutputTokensTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "lic_llm_output_tokens_total",
			Help: "LLM output tokens generated.",
		}, []string{"provider", "model", "agent"}),

		CostUSDTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "lic_llm_cost_usd_total",
			Help: "Accumulated LLM cost in USD.",
		}, []string{"provider", "model", "agent"}),

		ProviderFallbackTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "lic_llm_provider_fallback_total",
			Help: "Number of provider fallbacks from `from` to `to` per agent.",
		}, []string{"from", "to", "agent"}),

		ProviderSkippedUnhealthyTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "lic_llm_provider_skipped_unhealthy_total",
			Help: "Number of times a provider was skipped due to unhealthy/permanent state.",
		}, []string{"provider"}),

		ProviderFailedTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "lic_llm_provider_failed_total",
			Help: "Number of provider failures by LLMProviderError code.",
		}, []string{"provider", "code"}),

		ProviderHealthStatus: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "lic_llm_provider_health_status",
			Help: "Current provider health state (healthy|unhealthy|permanent).",
		}, []string{"provider", "state"}),

		ProviderCircuitState: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "lic_llm_provider_circuit_state",
			Help: "Provider circuit breaker state: 0=closed, 1=half_open, 2=open.",
		}, []string{"provider"}),

		RateLimitedTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "lic_llm_rate_limited_total",
			Help: "Number of LLM calls denied by the token-bucket rate limiter.",
		}, []string{"provider"}),
	}
}

func (l *LLMMetrics) mustRegister(reg *prometheus.Registry) {
	reg.MustRegister(
		l.CallsTotal,
		l.LatencySeconds,
		l.InputTokensTotal,
		l.CachedTokensTotal,
		l.OutputTokensTotal,
		l.CostUSDTotal,
		l.ProviderFallbackTotal,
		l.ProviderSkippedUnhealthyTotal,
		l.ProviderFailedTotal,
		l.ProviderHealthStatus,
		l.ProviderCircuitState,
		l.RateLimitedTotal,
	)
}
