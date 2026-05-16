// Package cost is the Cost & Usage Tracker for the Legal Intelligence Core
// (LIC-TASK-018, llm-provider-abstraction.md §4, observability.md §3.4).
//
// The Provider Router (LIC-TASK-019) calls the Tracker after every
// provider.Complete(): ObserveSuccess on a successful round-trip (records
// the five usage families AND a success call) and ObserveCall on a
// repair/fail/fallback outcome (call counter only — a failed call has no
// billable usage). ObserveSuccess returns the computed USD so the Router can
// attach it to the lic.llm OTel span via tracer.AttrLLMCostUSD; the Tracker
// never touches the span itself — the Router owns it, and keeping cost out
// avoids double-ownership of lic.llm.cost_usd and keeps this package
// hermetic (code-architect OQ-2).
//
// Hermeticity: like every internal/llm/* sibling (claude/openai/gemini/
// ratelimit) this package imports only the standard library, the domain
// (port/model) and its sibling internal/llm/pricing. The Prometheus metric
// vecs are inverted behind the Recorder seam (satisfied by an adapter over
// *metrics.LLMMetrics) and that adapter is wired in LIC-TASK-047 — a
// prometheus import here would break the invariant that no internal/*
// package imports internal/infra/observability/metrics before app-wiring
// (code-architect OQ-1, mirrors ratelimit's Observer seam).
package cost

import (
	"fmt"
	"time"

	"contractpro/legal-intelligence-core/internal/domain/model"
	"contractpro/legal-intelligence-core/internal/domain/port"
	"contractpro/legal-intelligence-core/internal/llm/pricing"
)

// Outcome is the lic_llm_calls_total{outcome} label value. It is a local
// mirror of metrics.LLMCallOutcome (observability.md §3.4) — declared here,
// not imported, so the package stays hermetic; cost_test.go pins the four
// wire strings so the mirror cannot silently drift from the SSOT.
type Outcome string

const (
	OutcomeSuccess  Outcome = "success"
	OutcomeRepair   Outcome = "repair"
	OutcomeFail     Outcome = "fail"
	OutcomeFallback Outcome = "fallback"
)

// IsValid reports whether o is one of the four declared outcomes. The
// Tracker never errors on the telemetry path, so this is for callers/tests,
// not enforced by ObserveCall.
func (o Outcome) IsValid() bool {
	switch o {
	case OutcomeSuccess, OutcomeRepair, OutcomeFail, OutcomeFallback:
		return true
	default:
		return false
	}
}

// Recorder is the metrics seam. A single batched RecordUsage keeps the
// success-atomicity invariant (the five usage families fire together for
// one successful call) inside the Tracker instead of spread across the
// wiring adapter and every test fake (code-architect MF-1; mirrors the
// "one seam, grouped signals" choice in ratelimit's CLAUDE.md). The
// concrete adapter binding this to *metrics.LLMMetrics is LIC-TASK-047.
type Recorder interface {
	// RecordUsage fires once per successful Complete: it increments
	// lic_llm_{input,cached,output}_tokens_total and lic_llm_cost_usd_total
	// {provider,model,agent} and observes lic_llm_latency_seconds
	// {provider,model,agent}. It does NOT touch lic_llm_calls_total (see
	// RecordCall) — calls are counted on every outcome with an extra
	// {outcome} label, a different lifecycle.
	RecordUsage(provider, model, agent string, input, cached, output int, costUSD float64, latency time.Duration)

	// RecordCall increments lic_llm_calls_total{provider,model,agent,outcome}.
	RecordCall(provider, model, agent, outcome string)

	// UnknownModel fires when a recorded model is absent from the pricing
	// table (its cost contribution was 0). model is for LOGGING only: the
	// LIC-TASK-047 adapter MUST aggregate it into a provider-labelled
	// counter (e.g. lic_llm_pricing_unknown_model_total{provider}) and MUST
	// NOT place the raw model string in a Prometheus label — an arbitrary
	// model string is an unbounded-cardinality vector, the same class
	// observability.md §3.10 closes for organization_id (code-architect
	// MF-3).
	UnknownModel(provider, model string)
}

// noopRecorder is the zero-dependency default so the Tracker is usable in
// tests and before LIC-TASK-047 wires Prometheus, without a nil check on
// every call (mirrors ratelimit.noopObserver).
type noopRecorder struct{}

func (noopRecorder) RecordUsage(string, string, string, int, int, int, float64, time.Duration) {}
func (noopRecorder) RecordCall(string, string, string, string)                                 {}
func (noopRecorder) UnknownModel(string, string)                                               {}

var _ Recorder = noopRecorder{}

// Usage is everything the Tracker needs from one successful Complete.
//
// There is deliberately NO organization_id field: per observability.md
// §3.10 / §4.2 per-tenant cost is attributed via the OTel span (the Router
// sets lic.pipeline.organization_id alongside the cost returned by
// ObserveSuccess), never a Prometheus label. Omitting org id here makes
// "no org id in any cost metric" a structural guarantee, not a discipline
// (code-architect OQ-2).
//
// Latency is a time.Duration; its source is CompletionResponse.LatencyMs
// (int64 millis) which the Router converts via
// time.Duration(resp.LatencyMs)*time.Millisecond. The Recorder observes
// Latency.Seconds() into lic_llm_latency_seconds (llmLatencyBuckets is
// seconds-based) — an off-by-1000 here would silently corrupt every
// latency panel.
type Usage struct {
	Provider          port.LLMProviderID
	Model             string
	Agent             model.AgentID
	InputTokens       int
	CachedInputTokens int
	OutputTokens      int
	Latency           time.Duration
}

// Tracker computes LLM cost from the pricing table and records the usage &
// call metrics. It is immutable after construction (table is read-only, rec
// is a fixed seam) → safe for concurrent use by the parallel errgroup agent
// pipeline without locking: the pricing.Table map is never written and the
// Prometheus vecs behind Recorder are themselves concurrency-safe.
type Tracker struct {
	table pricing.Table
	rec   Recorder
}

// NewTracker fails fast on a nil/empty pricing table — a silently empty
// table bills every call at $0 and hides every spike from LICCostSpike,
// which is worse than not starting (code-architect OQ-6; mirrors
// ratelimit.NewLimiter / kvstore.NewClient fail-fast). A nil rec degrades
// to a no-op so the Tracker is usable before LIC-TASK-047 wiring.
func NewTracker(table pricing.Table, rec Recorder) (*Tracker, error) {
	if len(table) == 0 {
		return nil, fmt.Errorf("cost: pricing table must contain at least one model")
	}
	if rec == nil {
		rec = noopRecorder{}
	}
	return &Tracker{table: table, rec: rec}, nil
}

// ObserveSuccess records one successful Complete and returns the computed
// USD cost. The cost is ALWAYS returned (including 0.0 for an unknown
// model) so the Router can set the OTel span attribute deterministically.
//
// It emits BOTH the five usage families (RecordUsage) AND
// lic_llm_calls_total{outcome=success} (RecordCall): a success is a call
// too, and omitting it would undercount lic_llm_calls_total by the entire
// success volume (code-architect MF-2). On an unknown model it still
// records usage (cost 0) and fires UnknownModel — a missing price never
// drops telemetry. Negative token counts are clamped to 0 (defensively at
// this boundary and inside pricing.CostUSD): a buggy adapter must never
// panic prometheus.Counter.Add and crash the pipeline.
func (t *Tracker) ObserveSuccess(u Usage) float64 {
	prov := u.Provider.String()
	agent := string(u.Agent)

	usd, known := t.table.CostUSD(u.Model, u.InputTokens, u.CachedInputTokens, u.OutputTokens)
	if !known {
		t.rec.UnknownModel(prov, u.Model)
	}
	t.rec.RecordUsage(prov, u.Model, agent,
		nonNeg(u.InputTokens), nonNeg(u.CachedInputTokens), nonNeg(u.OutputTokens),
		usd, u.Latency)
	t.rec.RecordCall(prov, u.Model, agent, string(OutcomeSuccess))
	return usd
}

// ObserveCall records a non-usage terminal outcome (repair|fail|fallback —
// such calls carry no billable usage). Provider and model are still known
// (the request targeted a concrete model). Success must go through
// ObserveSuccess; ObserveCall records whatever outcome it is given verbatim
// (the Tracker never errors on the telemetry path — a caller passing a bad
// outcome is the caller's bug, surfaced as an odd label, not a crash).
func (t *Tracker) ObserveCall(provider port.LLMProviderID, mdl string, agent model.AgentID, outcome Outcome) {
	t.rec.RecordCall(provider.String(), mdl, string(agent), string(outcome))
}

// nonNeg clamps a token count to >= 0 before it reaches RecordUsage —
// prometheus.Counter.Add(<0) panics and must never crash the agent
// pipeline. Deliberately duplicated, not shared, with pricing.nonNeg (see
// the rationale on that copy); the two MUST stay behaviourally identical
// (golang-pro D1/N1).
func nonNeg(n int) int {
	if n < 0 {
		return 0
	}
	return n
}
