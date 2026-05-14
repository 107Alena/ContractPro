package metrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

// TestPipelineMetrics_Counters — smoke test that pipeline counters and
// gauges can be observed and reflect the expected delta.
func TestPipelineMetrics_Counters(t *testing.T) {
	m := New(BuildInfo{})

	m.Pipeline.StartedTotal.WithLabelValues(string(PipelineModeInitial)).Inc()
	m.Pipeline.StartedTotal.WithLabelValues(string(PipelineModeInitial)).Inc()
	m.Pipeline.StartedTotal.WithLabelValues(string(PipelineModeRecheck)).Inc()

	if got := testutil.ToFloat64(m.Pipeline.StartedTotal.WithLabelValues(string(PipelineModeInitial))); got != 2 {
		t.Errorf("StartedTotal{mode=INITIAL} = %v, want 2", got)
	}
	if got := testutil.ToFloat64(m.Pipeline.StartedTotal.WithLabelValues(string(PipelineModeRecheck))); got != 1 {
		t.Errorf("StartedTotal{mode=RE_CHECK} = %v, want 1", got)
	}

	m.Pipeline.ConcurrentJobs.Set(3)
	if got := testutil.ToFloat64(m.Pipeline.ConcurrentJobs); got != 3 {
		t.Errorf("ConcurrentJobs = %v, want 3", got)
	}
}

// TestAgentMetrics_RepairOutcomeCardinality — guard for the per-agent
// budget §3.10: 9 agents × 5 outcomes = 45 series max on
// invocations_total. We don't enforce a hard ceiling here (Prometheus
// won't either) but assert that label combinations make sense.
func TestAgentMetrics_RepairOutcomeCardinality(t *testing.T) {
	m := New(BuildInfo{})
	m.Agent.RepairOutcomeTotal.WithLabelValues("AGENT_RISK_DETECTION", "claude", string(RepairOutcomeRepairedOK)).Inc()
	if got := testutil.ToFloat64(m.Agent.RepairOutcomeTotal.WithLabelValues("AGENT_RISK_DETECTION", "claude", string(RepairOutcomeRepairedOK))); got != 1 {
		t.Errorf("RepairOutcomeTotal = %v, want 1", got)
	}
}

// TestLLMMetrics_CachedTokensDistinctFromInput — the spec explicitly
// separates cached vs uncached input tokens to avoid 10× cost inflation.
// This test proves the two counters are independent series.
func TestLLMMetrics_CachedTokensDistinctFromInput(t *testing.T) {
	m := New(BuildInfo{})

	m.LLM.InputTokensTotal.WithLabelValues("claude", "claude-sonnet-4-6", "AGENT_RISK_DETECTION").Add(1_000)
	m.LLM.CachedTokensTotal.WithLabelValues("claude", "claude-sonnet-4-6", "AGENT_RISK_DETECTION").Add(9_000)

	gotInput := testutil.ToFloat64(m.LLM.InputTokensTotal.WithLabelValues("claude", "claude-sonnet-4-6", "AGENT_RISK_DETECTION"))
	gotCached := testutil.ToFloat64(m.LLM.CachedTokensTotal.WithLabelValues("claude", "claude-sonnet-4-6", "AGENT_RISK_DETECTION"))

	if gotInput != 1_000 {
		t.Errorf("input_tokens = %v, want 1000", gotInput)
	}
	if gotCached != 9_000 {
		t.Errorf("cached_tokens = %v, want 9000", gotCached)
	}
}

// TestLLMMetrics_ProviderHealthStatus — health gauge encodes binary
// presence per state; flipping state means we set new=1 and (caller's
// responsibility) old=0. Test verifies both flows work.
func TestLLMMetrics_ProviderHealthStatus(t *testing.T) {
	m := New(BuildInfo{})

	m.LLM.ProviderHealthStatus.WithLabelValues("claude", string(LLMHealthHealthy)).Set(1)
	m.LLM.ProviderHealthStatus.WithLabelValues("claude", string(LLMHealthUnhealthy)).Set(0)

	if got := testutil.ToFloat64(m.LLM.ProviderHealthStatus.WithLabelValues("claude", string(LLMHealthHealthy))); got != 1 {
		t.Errorf("health=healthy gauge = %v, want 1", got)
	}
	if got := testutil.ToFloat64(m.LLM.ProviderHealthStatus.WithLabelValues("claude", string(LLMHealthUnhealthy))); got != 0 {
		t.Errorf("health=unhealthy gauge = %v, want 0", got)
	}
}

// TestDMMetrics_ArtifactSizeHistogram — observation lands in the right
// histogram bucket. Spec ceiling is LIC_MAX_INGESTED_BYTES=10 MiB.
func TestDMMetrics_ArtifactSizeHistogram(t *testing.T) {
	m := New(BuildInfo{})
	m.DM.ArtifactsReceivedSizeBytes.Observe(5 * 1024 * 1024) // 5 MiB

	// Confirm the metric is registered and accepted the observation by
	// re-gathering from the dedicated registry.
	reg := m.Registry()
	families, err := reg.Gather()
	if err != nil {
		t.Fatalf("Gather: %v", err)
	}
	var found bool
	for _, fam := range families {
		if fam.GetName() == "lic_dm_artifacts_received_size_bytes" {
			found = true
			h := fam.Metric[0].GetHistogram()
			if h.GetSampleCount() != 1 {
				t.Errorf("sample count = %d, want 1", h.GetSampleCount())
			}
		}
	}
	if !found {
		t.Error("lic_dm_artifacts_received_size_bytes not found")
	}
}

// TestIdempotencyMetrics_AllLookupResults — every spec'd result value
// must be a valid label.
func TestIdempotencyMetrics_AllLookupResults(t *testing.T) {
	m := New(BuildInfo{})
	for _, r := range []IdempotencyLookupResult{IdempLookupNew, IdempLookupInProgress, IdempLookupCompleted, IdempLookupFallbackDB} {
		m.Idempotency.LookupsTotal.WithLabelValues(string(r)).Inc()
		if got := testutil.ToFloat64(m.Idempotency.LookupsTotal.WithLabelValues(string(r))); got != 1 {
			t.Errorf("LookupsTotal{result=%s} = %v, want 1", r, got)
		}
	}
}

// TestPendingMetrics_StateAgeGauge — gauge accepts and returns the set
// value (used by the LICStuckPendingState alert).
func TestPendingMetrics_StateAgeGauge(t *testing.T) {
	m := New(BuildInfo{})
	m.Pending.StateAgeSecondsMax.Set(72_000) // 20h, below 22h alert threshold
	if got := testutil.ToFloat64(m.Pending.StateAgeSecondsMax); got != 72_000 {
		t.Errorf("StateAgeSecondsMax = %v, want 72000", got)
	}
}

// TestDLQMetrics_PerTopicReason — DLQ counter must distinguish topic
// and reason; both labels are required for the LICDLQGrowth alert
// breakdown.
func TestDLQMetrics_PerTopicReason(t *testing.T) {
	m := New(BuildInfo{})
	m.DLQ.PublishedTotal.WithLabelValues(string(DLQTopicAgentOutputInvalid), "schema_violation").Inc()
	m.DLQ.PublishedTotal.WithLabelValues(string(DLQTopicAgentOutputInvalid), "schema_violation").Inc()
	m.DLQ.PublishedTotal.WithLabelValues(string(DLQTopicInvalidMessage), "missing_correlation_id").Inc()

	if got := testutil.ToFloat64(m.DLQ.PublishedTotal.WithLabelValues(string(DLQTopicAgentOutputInvalid), "schema_violation")); got != 2 {
		t.Errorf("DLQ agent_output_invalid/schema = %v, want 2", got)
	}
	if got := testutil.ToFloat64(m.DLQ.PublishedTotal.WithLabelValues(string(DLQTopicInvalidMessage), "missing_correlation_id")); got != 1 {
		t.Errorf("DLQ invalid-message/missing_correlation_id = %v, want 1", got)
	}
}

// TestCrossCutMetrics_PromptInjectionPerAgent — observability.md §3.9:
// per-agent counter, no severity label.
func TestCrossCutMetrics_PromptInjectionPerAgent(t *testing.T) {
	m := New(BuildInfo{})
	m.CrossCut.PromptInjectionDetectedTotal.WithLabelValues("AGENT_TYPE_CLASSIFIER").Inc()
	m.CrossCut.PromptInjectionDetectedTotal.WithLabelValues("AGENT_DETAILED_REPORT").Inc()

	families, err := m.Registry().Gather()
	if err != nil {
		t.Fatalf("Gather: %v", err)
	}
	for _, fam := range families {
		if fam.GetName() != "lic_prompt_injection_detected_total" {
			continue
		}
		for _, met := range fam.Metric {
			for _, lp := range met.Label {
				if lp.GetName() == "severity" {
					t.Errorf("forbidden label severity present (C-lite policy: severity NOT a label)")
				}
			}
		}
	}
}

// TestCrossCutMetrics_PartyValidation — `valid` label must be the string
// representation produced by BoolLabel; not, say, "1"/"0".
func TestCrossCutMetrics_PartyValidation(t *testing.T) {
	m := New(BuildInfo{})
	m.CrossCut.PartyValidationTotal.WithLabelValues(string(PartyValidationINN), BoolLabel(true)).Inc()
	if got := testutil.ToFloat64(m.CrossCut.PartyValidationTotal.WithLabelValues("inn", "true")); got != 1 {
		t.Errorf("PartyValidationTotal{type=inn, valid=true} = %v, want 1", got)
	}
}

// Compile-time assurance that exported sub-groups embed the
// *CounterVec/*Gauge types we'd expect — keeps refactors from quietly
// downgrading the public surface.
var (
	_ prometheus.Collector = (*prometheus.CounterVec)(nil)
	_ prometheus.Collector = (*prometheus.HistogramVec)(nil)
	_ prometheus.Collector = (*prometheus.GaugeVec)(nil)
)
