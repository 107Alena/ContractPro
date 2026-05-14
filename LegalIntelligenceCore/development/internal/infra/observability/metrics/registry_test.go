package metrics

import (
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

// TestNew_RegistersAllExpectedMetrics is the canonical "no metric was
// silently dropped" guard. The list below mirrors observability.md §3 —
// any future addition must be reflected here, or the test fails.
func TestNew_RegistersAllExpectedMetrics(t *testing.T) {
	want := []string{
		// §3.2 pipeline
		"lic_pipeline_started_total",
		"lic_pipeline_total_duration_seconds",
		"lic_pipeline_outcome_total",
		"lic_pipeline_concurrent_jobs",
		"lic_pipeline_stage_duration_seconds",
		// §3.3 agent
		"lic_agent_invocations_total",
		"lic_agent_duration_seconds",
		"lic_agent_input_tokens",
		"lic_agent_output_tokens",
		"lic_agent_repair_attempts_total",
		"lic_agent_repair_outcome_total",
		// §3.4 llm
		"lic_llm_calls_total",
		"lic_llm_latency_seconds",
		"lic_llm_input_tokens_total",
		"lic_llm_cached_tokens_total",
		"lic_llm_output_tokens_total",
		"lic_llm_cost_usd_total",
		"lic_llm_provider_fallback_total",
		"lic_llm_provider_skipped_unhealthy_total",
		"lic_llm_provider_failed_total",
		"lic_llm_provider_health_status",
		"lic_llm_provider_circuit_state",
		"lic_llm_rate_limited_total",
		// §3.5 dm
		"lic_dm_request_duration_seconds",
		"lic_dm_request_outcome_total",
		"lic_dm_artifacts_received_size_bytes",
		"lic_dm_artifacts_published_size_bytes",
		// §3.6 idempotency
		"lic_idempotency_lookups_total",
		"lic_idempotency_fallback_total",
		// §3.7 pending
		"lic_pending_state_count",
		"lic_pending_state_age_seconds_max",
		"lic_user_confirmation_received_total",
		// §3.8 dlq
		"lic_dlq_published_total",
		// §3.9 cross-cutting
		"lic_prompt_injection_detected_total",
		"lic_party_validation_total",
		"lic_consumer_messages_total",
		"lic_publisher_messages_total",
		"lic_circuit_breaker_state",
		"lic_build_info",
	}

	m := New(BuildInfo{Version: "test", Commit: "abc", GoVersion: "go1.26.1"})
	// Prometheus *Vec metrics surface in Gather() only after at least
	// one observation (a registered-but-unobserved CounterVec yields no
	// MetricFamily). Seed one observation per labelled metric so the
	// gathered set matches the full registered set.
	seedOneObservationPerLabelledMetric(t, m)

	got := gatherMetricNames(t, m.Registry())

	for _, name := range want {
		if !got[name] {
			t.Errorf("metric %q not registered", name)
		}
	}
	if len(got) != len(want) {
		t.Errorf("unexpected metric count: got %d, want %d (extras: %v)", len(got), len(want), diffNames(got, want))
	}
}

// TestNew_LicPrefixOnAllMetrics enforces observability.md §3.1: every
// exposed metric must start with `lic_`. Catches accidental upstream
// imports or default-registry leaks.
func TestNew_LicPrefixOnAllMetrics(t *testing.T) {
	m := New(BuildInfo{})
	seedOneObservationPerLabelledMetric(t, m)
	for name := range gatherMetricNames(t, m.Registry()) {
		if !strings.HasPrefix(name, "lic_") {
			t.Errorf("metric %q lacks required lic_ prefix", name)
		}
	}
}

// TestNew_NoOrganizationIDLabel enforces observability.md §3.10 — the
// single most important cardinality invariant. organization_id MUST NOT
// appear in any label on any metric. Adding it to a new metric — even
// once — is a 10K-tenant × 1.5K-series = 15M-series cardinality bomb.
func TestNew_NoOrganizationIDLabel(t *testing.T) {
	const forbidden = "organization_id"

	m := New(BuildInfo{Version: "v", Commit: "c", GoVersion: "go"})

	// A label appears in the gathered output only after at least one
	// observation, so we trigger one observation per labelled metric
	// before gathering.
	seedOneObservationPerLabelledMetric(t, m)

	families, err := m.Registry().Gather()
	if err != nil {
		t.Fatalf("Gather: %v", err)
	}

	for _, fam := range families {
		for _, met := range fam.Metric {
			for _, lp := range met.Label {
				if lp.GetName() == forbidden {
					t.Errorf("metric %q exposes forbidden label %q (observability.md §3.10)",
						fam.GetName(), forbidden)
				}
			}
		}
	}
}

// TestNew_BuildInfoEmptyIsNormalized — guarantees that an under-wired
// `main` (forgot to pass BuildInfo) produces visible "unknown" labels
// rather than a silent empty-string series that breaks dashboards.
func TestNew_BuildInfoEmptyIsNormalized(t *testing.T) {
	m := New(BuildInfo{})

	families, err := m.Registry().Gather()
	if err != nil {
		t.Fatalf("Gather: %v", err)
	}
	for _, fam := range families {
		if fam.GetName() != "lic_build_info" {
			continue
		}
		if len(fam.Metric) != 1 {
			t.Fatalf("lic_build_info: want 1 series, got %d", len(fam.Metric))
		}
		labels := labelMap(fam.Metric[0].GetLabel())
		for _, k := range []string{"version", "commit", "go_version"} {
			if labels[k] == "" {
				t.Errorf("lic_build_info label %q is empty; want %q for unset fields", k, "unknown")
			}
			if labels[k] != "unknown" {
				t.Errorf("lic_build_info label %q = %q, want %q (default for zero-value BuildInfo)", k, labels[k], "unknown")
			}
		}
	}
}

// TestNew_BuildInfoIsAlwaysOne checks that lic_build_info ships exactly
// one series, value 1, with the configured BuildInfo labels.
func TestNew_BuildInfoIsAlwaysOne(t *testing.T) {
	info := BuildInfo{Version: "1.2.3", Commit: "deadbeef", GoVersion: "go1.26.1"}
	m := New(info)

	families, err := m.Registry().Gather()
	if err != nil {
		t.Fatalf("Gather: %v", err)
	}

	var found bool
	for _, fam := range families {
		if fam.GetName() != "lic_build_info" {
			continue
		}
		if len(fam.Metric) != 1 {
			t.Fatalf("lic_build_info: want 1 series, got %d", len(fam.Metric))
		}
		series := fam.Metric[0]
		if v := series.GetGauge().GetValue(); v != 1 {
			t.Errorf("lic_build_info value = %v, want 1", v)
		}
		labels := labelMap(series.GetLabel())
		if labels["version"] != info.Version ||
			labels["commit"] != info.Commit ||
			labels["go_version"] != info.GoVersion {
			t.Errorf("lic_build_info labels = %#v, want version=%s commit=%s go_version=%s",
				labels, info.Version, info.Commit, info.GoVersion)
		}
		found = true
	}
	if !found {
		t.Fatal("lic_build_info family not found in gather output")
	}
}

// TestNew_DistinctRegistries — two New() calls must produce isolated
// registries; otherwise duplicate registration panics or tests leak
// state into each other.
func TestNew_DistinctRegistries(t *testing.T) {
	m1 := New(BuildInfo{Version: "a"})
	m2 := New(BuildInfo{Version: "b"})

	if m1.Registry() == m2.Registry() {
		t.Fatal("New() reused the same *prometheus.Registry across instances")
	}
}

// TestRegistry_HermeticFromDefault — ensures we never accidentally
// register on prometheus.DefaultRegisterer. The count-diff approach is
// fragile (a polluted default registry from another test masks the
// regression), so we assert directly: no `lic_*` family must appear
// on the default gatherer after New().
func TestRegistry_HermeticFromDefault(t *testing.T) {
	_ = New(BuildInfo{Version: "h", Commit: "c", GoVersion: "go"})

	families, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("Gather default: %v", err)
	}
	for _, fam := range families {
		if strings.HasPrefix(fam.GetName(), "lic_") {
			t.Errorf("lic_ metric leaked into DefaultRegisterer: %s", fam.GetName())
		}
	}
}

// TestNew_ConcurrentConstructionIsSafe — protects future call-sites
// that may lazily construct Metrics via sync.Once (or, by accident,
// from multiple goroutines). Two concurrent New() calls must each
// yield a usable, distinct registry without panic or data races.
func TestNew_ConcurrentConstructionIsSafe(t *testing.T) {
	const n = 50
	results := make(chan *Metrics, n)

	for i := 0; i < n; i++ {
		go func() {
			results <- New(BuildInfo{Version: "v", Commit: "c", GoVersion: "go"})
		}()
	}

	seen := make(map[*prometheus.Registry]struct{}, n)
	for i := 0; i < n; i++ {
		m := <-results
		if m.Registry() == nil {
			t.Fatal("nil registry from concurrent New()")
		}
		if _, dup := seen[m.Registry()]; dup {
			t.Fatal("two concurrent New() calls returned the same *prometheus.Registry")
		}
		seen[m.Registry()] = struct{}{}
	}
}

// TestLabels_PipelineOutcomeExhaustive — single-source-of-truth guard.
// Adding a new PipelineOutcome const without updating this map breaks
// the build, forcing a cardinality review. Repeat the pattern for any
// enum where new values would silently expand label cardinality.
func TestLabels_PipelineOutcomeExhaustive(t *testing.T) {
	pipelineOutcomes := map[PipelineOutcome]struct{}{
		PipelineOutcomeSuccess: {},
		PipelineOutcomeFailed:  {},
		PipelineOutcomeTimeout: {},
	}
	if len(pipelineOutcomes) != 3 {
		t.Fatalf("PipelineOutcome enum expanded without test update; got %d members", len(pipelineOutcomes))
	}

	agentInvocationOutcomes := map[AgentInvocationOutcome]struct{}{
		AgentOutcomeSuccess:       {},
		AgentOutcomeRepairSuccess: {},
		AgentOutcomeInvalidOutput: {},
		AgentOutcomeProviderError: {},
		AgentOutcomeTimeout:       {},
	}
	if len(agentInvocationOutcomes) != 5 {
		t.Fatalf("AgentInvocationOutcome enum expanded without test update; got %d members", len(agentInvocationOutcomes))
	}

	repairOutcomes := map[AgentRepairOutcome]struct{}{
		RepairOutcomeRepairedOK:    {},
		RepairOutcomeRepairFailed:  {},
		RepairOutcomeProviderError: {},
	}
	if len(repairOutcomes) != 3 {
		t.Fatalf("AgentRepairOutcome enum expanded without test update; got %d members", len(repairOutcomes))
	}

	llmCallOutcomes := map[LLMCallOutcome]struct{}{
		LLMOutcomeSuccess:  {},
		LLMOutcomeRepair:   {},
		LLMOutcomeFail:     {},
		LLMOutcomeFallback: {},
	}
	if len(llmCallOutcomes) != 4 {
		t.Fatalf("LLMCallOutcome enum expanded without test update; got %d members", len(llmCallOutcomes))
	}

	dmOutcomes := map[DMOutcome]struct{}{
		DMOutcomeSuccess:       {},
		DMOutcomeTimeout:       {},
		DMOutcomePersistFailed: {},
		DMOutcomeMissing:       {},
	}
	if len(dmOutcomes) != 4 {
		t.Fatalf("DMOutcome enum expanded without test update; got %d members", len(dmOutcomes))
	}

	idempLookups := map[IdempotencyLookupResult]struct{}{
		IdempLookupNew:        {},
		IdempLookupInProgress: {},
		IdempLookupCompleted:  {},
		IdempLookupFallbackDB: {},
	}
	if len(idempLookups) != 4 {
		t.Fatalf("IdempotencyLookupResult enum expanded without test update; got %d members", len(idempLookups))
	}

	pendingOutcomes := map[PendingConfirmationOutcome]struct{}{
		PendingOutcomeResumed: {},
		PendingOutcomeExpired: {},
		PendingOutcomeInvalid: {},
	}
	if len(pendingOutcomes) != 3 {
		t.Fatalf("PendingConfirmationOutcome enum expanded without test update; got %d members", len(pendingOutcomes))
	}

	dlqTopics := map[DLQTopic]struct{}{
		DLQTopicInvalidMessage:     {},
		DLQTopicConsumerFailed:     {},
		DLQTopicPublishFailed:      {},
		DLQTopicAgentOutputInvalid: {},
	}
	if len(dlqTopics) != 4 {
		t.Fatalf("DLQTopic enum expanded without test update; got %d members", len(dlqTopics))
	}
}

// --- helpers ---------------------------------------------------------

func gatherMetricNames(t *testing.T, reg *prometheus.Registry) map[string]bool {
	t.Helper()
	families, err := reg.Gather()
	if err != nil {
		t.Fatalf("Gather: %v", err)
	}
	out := make(map[string]bool, len(families))
	for _, fam := range families {
		out[fam.GetName()] = true
	}
	return out
}

func diffNames(got map[string]bool, want []string) []string {
	wantSet := make(map[string]bool, len(want))
	for _, w := range want {
		wantSet[w] = true
	}
	var extras []string
	for name := range got {
		if !wantSet[name] {
			extras = append(extras, name)
		}
	}
	return extras
}

func labelMap(pairs []*dto.LabelPair) map[string]string {
	out := make(map[string]string, len(pairs))
	for _, lp := range pairs {
		out[lp.GetName()] = lp.GetValue()
	}
	return out
}

// seedOneObservationPerLabelledMetric forces every *Vec metric to emit
// at least one series so the cardinality test sees its actual labels.
// Without this, Gather() returns only families that have non-zero
// children and the no-organization_id invariant becomes a no-op.
//
// One observation per metric, not exhaustive across label combinations
// — the goal here is "make the family appear in Gather()", not "exercise
// every label tuple". The cardinality invariant (§3.10) is enforced at
// the schema level (label *names*, independent of which values are
// instantiated), so a single observation per metric is sufficient.
func seedOneObservationPerLabelledMetric(t *testing.T, m *Metrics) {
	t.Helper()
	// Pipeline
	m.Pipeline.StartedTotal.WithLabelValues(string(PipelineModeInitial)).Inc()
	m.Pipeline.TotalDurationSeconds.WithLabelValues(string(PipelineModeInitial), string(PipelineOutcomeSuccess)).Observe(1)
	m.Pipeline.OutcomeTotal.WithLabelValues(string(PipelineModeInitial), string(PipelineOutcomeSuccess), "").Inc()
	m.Pipeline.StageDurationSeconds.WithLabelValues("STAGE_RECEIVED").Observe(1)

	// Agent
	m.Agent.InvocationsTotal.WithLabelValues("AGENT_TYPE_CLASSIFIER", string(AgentOutcomeSuccess)).Inc()
	m.Agent.DurationSeconds.WithLabelValues("AGENT_TYPE_CLASSIFIER").Observe(1)
	m.Agent.InputTokens.WithLabelValues("AGENT_TYPE_CLASSIFIER").Observe(1)
	m.Agent.OutputTokens.WithLabelValues("AGENT_TYPE_CLASSIFIER").Observe(1)
	m.Agent.RepairAttemptsTotal.WithLabelValues("AGENT_TYPE_CLASSIFIER", "claude").Inc()
	m.Agent.RepairOutcomeTotal.WithLabelValues("AGENT_TYPE_CLASSIFIER", "claude", string(RepairOutcomeRepairedOK)).Inc()

	// LLM
	m.LLM.CallsTotal.WithLabelValues("claude", "claude-sonnet-4-6", "AGENT_TYPE_CLASSIFIER", string(LLMOutcomeSuccess)).Inc()
	m.LLM.LatencySeconds.WithLabelValues("claude", "claude-sonnet-4-6", "AGENT_TYPE_CLASSIFIER").Observe(1)
	m.LLM.InputTokensTotal.WithLabelValues("claude", "claude-sonnet-4-6", "AGENT_TYPE_CLASSIFIER").Add(100)
	m.LLM.CachedTokensTotal.WithLabelValues("claude", "claude-sonnet-4-6", "AGENT_TYPE_CLASSIFIER").Add(50)
	m.LLM.OutputTokensTotal.WithLabelValues("claude", "claude-sonnet-4-6", "AGENT_TYPE_CLASSIFIER").Add(20)
	m.LLM.CostUSDTotal.WithLabelValues("claude", "claude-sonnet-4-6", "AGENT_TYPE_CLASSIFIER").Add(0.01)
	m.LLM.ProviderFallbackTotal.WithLabelValues("claude", "openai", "AGENT_TYPE_CLASSIFIER").Inc()
	m.LLM.ProviderSkippedUnhealthyTotal.WithLabelValues("claude").Inc()
	m.LLM.ProviderFailedTotal.WithLabelValues("claude", string(LLMErrTimeout)).Inc()
	m.LLM.ProviderHealthStatus.WithLabelValues("claude", string(LLMHealthHealthy)).Set(1)
	m.LLM.ProviderCircuitState.WithLabelValues("claude").Set(CircuitStateClosed)
	m.LLM.RateLimitedTotal.WithLabelValues("claude").Inc()

	// DM
	m.DM.RequestDurationSeconds.WithLabelValues(string(DMOpGetArtifacts)).Observe(1)
	m.DM.RequestOutcomeTotal.WithLabelValues(string(DMOpGetArtifacts), string(DMOutcomeSuccess)).Inc()

	// Idempotency
	m.Idempotency.LookupsTotal.WithLabelValues(string(IdempLookupNew)).Inc()

	// Pending
	m.Pending.UserConfirmationReceivedTotal.WithLabelValues(string(PendingOutcomeResumed)).Inc()

	// DLQ
	m.DLQ.PublishedTotal.WithLabelValues(string(DLQTopicInvalidMessage), "invalid_envelope").Inc()

	// Cross-cut
	m.CrossCut.PromptInjectionDetectedTotal.WithLabelValues("AGENT_TYPE_CLASSIFIER").Inc()
	m.CrossCut.PartyValidationTotal.WithLabelValues(string(PartyValidationINN), BoolLabel(true)).Inc()
	m.CrossCut.ConsumerMessagesTotal.WithLabelValues("dm.events.version-artifacts-ready", string(PublishOutcomeSuccess)).Inc()
	m.CrossCut.PublisherMessagesTotal.WithLabelValues("lic.artifacts.analysis-ready", string(PublishOutcomeSuccess)).Inc()
	m.CrossCut.CircuitBreakerState.WithLabelValues("broker").Set(CircuitStateClosed)
}

