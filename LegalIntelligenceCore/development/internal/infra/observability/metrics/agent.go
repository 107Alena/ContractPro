package metrics

import "github.com/prometheus/client_golang/prometheus"

// AgentMetrics — observability.md §3.3.
//
// `agent` label takes one of the 9 AgentID values; `provider` (on repair-
// related metrics) takes one of the 3 LLMProvider IDs. Combined cardinality
// is bounded by 9 × 3 × 3 = 81 series for the worst case, well within budget.
type AgentMetrics struct {
	// InvocationsTotal — every agent.Run() lands here exactly once,
	// with the final outcome label.
	InvocationsTotal *prometheus.CounterVec

	// DurationSeconds — wall-clock duration of agent.Run(), including
	// repair retries.
	DurationSeconds *prometheus.HistogramVec

	// InputTokens — estimated input tokens fed to the agent's prompt
	// (after truncation, before any provider-side rephrasing).
	InputTokens *prometheus.HistogramVec

	// OutputTokens — generated output tokens, as reported by the provider.
	OutputTokens *prometheus.HistogramVec

	// RepairAttemptsTotal — incremented each time we issue a repair turn
	// (max 1 per agent invocation per spec).
	RepairAttemptsTotal *prometheus.CounterVec

	// RepairOutcomeTotal — final repair outcome for this agent on the
	// "used provider" (sticky per OQ-10).
	RepairOutcomeTotal *prometheus.CounterVec
}

func newAgentMetrics() *AgentMetrics {
	return &AgentMetrics{
		InvocationsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "lic_agent_invocations_total",
			Help: "Number of agent invocations by outcome.",
		}, []string{"agent", "outcome"}),

		DurationSeconds: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "lic_agent_duration_seconds",
			Help:    "Duration of an agent invocation (including repair).",
			Buckets: agentDurationBuckets(),
		}, []string{"agent"}),

		InputTokens: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "lic_agent_input_tokens",
			Help:    "Estimated agent prompt size in tokens (post-truncation).",
			Buckets: agentInputTokensBuckets(),
		}, []string{"agent"}),

		OutputTokens: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "lic_agent_output_tokens",
			Help:    "Generated output tokens reported by the LLM provider.",
			Buckets: agentOutputTokensBuckets(),
		}, []string{"agent"}),

		RepairAttemptsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "lic_agent_repair_attempts_total",
			Help: "Number of repair attempts issued, by agent and used provider.",
		}, []string{"agent", "provider"}),

		RepairOutcomeTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "lic_agent_repair_outcome_total",
			Help: "Outcome of repair attempts, by agent, used provider, and outcome.",
		}, []string{"agent", "provider", "outcome"}),
	}
}

func (a *AgentMetrics) mustRegister(reg *prometheus.Registry) {
	reg.MustRegister(
		a.InvocationsTotal,
		a.DurationSeconds,
		a.InputTokens,
		a.OutputTokens,
		a.RepairAttemptsTotal,
		a.RepairOutcomeTotal,
	)
}
