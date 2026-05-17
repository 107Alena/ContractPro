package stages

import "context"

// StageMetrics is the telemetry seam for the single Stage-Executor-owned
// Prometheus vec declared centrally in metrics/pipeline.go
// (lic_pipeline_stage_duration_seconds{stage}, observability.md §3.2). The
// concrete adapter over *metrics.PipelineMetrics is wired in LIC-TASK-047; an
// internal/infra/observability/metrics import here would break the
// hermeticity invariant every internal/application|agents|llm/* package
// upholds before app-wiring (the base.Metrics / aggregator.Metrics /
// cost.Recorder precedent).
//
// The stage label value MUST come from Stage.String() only (the closed
// 6-value set, code-architect D5) so {stage} cardinality stays bounded.
// LIC-TASK-047's adapter maps THIS 6-value set, NOT model.Stage STAGE_* —
// the metrics/pipeline.go:24-25 comment is stale-on-conflict (CLAUDE.md
// "errgroup/Stage SSOT reconciliation").
type StageMetrics interface {
	// StageDuration observes lic_pipeline_stage_duration_seconds{stage}.
	// Recorded once per executed stage; a gated-out Stage 6 (INITIAL run)
	// records NOTHING (code-architect D7 — no zero-duration pollution).
	StageDuration(stage string, seconds float64)
}

// noopStageMetrics is the zero-dependency default so the Executor is usable
// in tests and before LIC-TASK-047 wires Prometheus, without a per-call nil
// check (mirrors base.noopMetrics / aggregator.noopMetrics).
type noopStageMetrics struct{}

func (noopStageMetrics) StageDuration(string, float64) {}

var _ StageMetrics = noopStageMetrics{}

// Tracer is the OTel seam. observability.md §4.2 mandates a per-stage span
// lic.stage.<suffix> that is the PARENT of the per-agent lic.agent.<name>
// spans (the agent + llm spans are opened INSIDE base.BaseAgent.Run via its
// own Tracer seam). The Stage Executor opens ONLY the stage span and passes
// its ctx to agent.Run so the agent span nests correctly. The concrete
// adapter over *tracer.Tracer is wired in LIC-TASK-047 — a
// go.opentelemetry.io / internal/infra/observability/tracer import here would
// break hermeticity (the base.Tracer / aggregator-seam precedent).
type Tracer interface {
	// StartStage opens the parent lic.stage.<name> span (name == the
	// Stage.String() suffix). The returned ctx carries the span so the
	// agent spans (and downstream router spans) nest beneath it.
	StartStage(ctx context.Context, name string) (context.Context, StageSpan)
}

// StageSpan is the stage-span handle. Finish is called EXACTLY once
// (deferred) with the terminal error; a non-nil err is recorded as span
// status=Error carrying the real *model.DomainError.
type StageSpan interface {
	// Degraded records a non-fatal non-critical-agent skip (Agent 3 in
	// Stage 2 / Agent 9 in Stage 6, on a per-agent AGENT_TIMEOUT) as a span
	// event. The stage still completes successfully (the pipeline
	// continues, err==nil at Finish); the un-lossy degradation truth
	// survives on the trace (the base MF-2 "truth survives on traces"
	// pattern — there is no closed-set model.Warnings code for an
	// agent-skip in v1; code-architect D6).
	Degraded(agentID, reason string)
	// Finish closes the stage span. err != nil ⇒ status=Error.
	Finish(err error)
}

// noopTracer / noopStageSpan are the zero-dependency defaults (mirrors every
// other seam's noop). StartStage returns the ctx unchanged so W3C trace
// context propagation still works when tracing is off.
type noopTracer struct{}

func (noopTracer) StartStage(ctx context.Context, _ string) (context.Context, StageSpan) {
	return ctx, noopStageSpan{}
}

type noopStageSpan struct{}

func (noopStageSpan) Degraded(string, string) {}
func (noopStageSpan) Finish(error)            {}

var (
	_ Tracer    = noopTracer{}
	_ StageSpan = noopStageSpan{}
)
