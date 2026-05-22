// Package app is the application-wiring layer (LIC-TASK-047).
//
// adapters.go declares the thin seam adapters that bridge the central
// observability collaborators (*logger.Logger, *metrics.Metrics,
// *tracer.Tracer) onto the narrower seam interfaces each downstream
// package declares (`Metrics`, `Logger`, `Clock`, `Tracer` and similar).
//
// Every adapter is deliberately one method per seam method, doing the
// minimum translation needed. They are NOT exported — wiring assembles
// them once in New().
package app

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"contractpro/legal-intelligence-core/internal/application/dmawaiter"
	"contractpro/legal-intelligence-core/internal/application/pendingconfirmation"
	"contractpro/legal-intelligence-core/internal/application/pipeline"
	"contractpro/legal-intelligence-core/internal/application/pipeline/stages"
	"contractpro/legal-intelligence-core/internal/domain/model"
	"contractpro/legal-intelligence-core/internal/domain/port"
	"contractpro/legal-intelligence-core/internal/egress/dlq"
	dmpub "contractpro/legal-intelligence-core/internal/egress/publisher/dm"
	"contractpro/legal-intelligence-core/internal/egress/publisher/orch"
	"contractpro/legal-intelligence-core/internal/ingress/consumer"
	"contractpro/legal-intelligence-core/internal/ingress/idempotency"
	ingressrouter "contractpro/legal-intelligence-core/internal/ingress/router"
	"contractpro/legal-intelligence-core/internal/infra/health"
	"contractpro/legal-intelligence-core/internal/infra/observability/logger"
	"contractpro/legal-intelligence-core/internal/infra/observability/metrics"
	"contractpro/legal-intelligence-core/internal/llm/cost"
	"contractpro/legal-intelligence-core/internal/llm/router"
	"contractpro/legal-intelligence-core/internal/agents/base"
	"contractpro/legal-intelligence-core/internal/agents/schemavalidator"
	"contractpro/legal-intelligence-core/internal/application/aggregator"
)

// -----------------------------------------------------------------------------
// Logger adapters.
// -----------------------------------------------------------------------------

// loggerKV is a small helper that converts a free-form key/value slice
// (used by most seam loggers) into slog.Attr values the central logger
// understands.
func loggerKV(kv []any) []slog.Attr {
	if len(kv) == 0 {
		return nil
	}
	attrs := make([]slog.Attr, 0, len(kv)/2+1)
	for i := 0; i < len(kv); i += 2 {
		key, ok := kv[i].(string)
		if !ok {
			key = "field"
		}
		var val any
		if i+1 < len(kv) {
			val = kv[i+1]
		}
		attrs = append(attrs, slog.Any(key, val))
	}
	return attrs
}

// stdLogger adapts *logger.Logger to a Warn/Error/Info-only seam.
type stdLogger struct{ l *logger.Logger }

func (s stdLogger) Info(ctx context.Context, msg string, kv ...any) {
	s.l.Info(ctx, msg, loggerKV(kv)...)
}
func (s stdLogger) Warn(ctx context.Context, msg string, kv ...any) {
	s.l.Warn(ctx, msg, loggerKV(kv)...)
}
func (s stdLogger) Error(ctx context.Context, msg string, kv ...any) {
	s.l.Error(ctx, msg, loggerKV(kv)...)
}

// consumerLogger adapts *logger.Logger to consumer.Logger which also
// requires the WithRequestContext hook.
type consumerLogger struct{ l *logger.Logger }

func (c consumerLogger) Info(ctx context.Context, msg string, kv ...any) {
	c.l.Info(ctx, msg, loggerKV(kv)...)
}
func (c consumerLogger) Warn(ctx context.Context, msg string, kv ...any) {
	c.l.Warn(ctx, msg, loggerKV(kv)...)
}
func (c consumerLogger) Error(ctx context.Context, msg string, kv ...any) {
	c.l.Error(ctx, msg, loggerKV(kv)...)
}

// WithRequestContext attaches per-delivery correlation IDs onto ctx via
// logger.WithRequestContext.
func (c consumerLogger) WithRequestContext(ctx context.Context, ids consumer.RequestIDs) context.Context {
	return logger.WithRequestContext(ctx, logger.RequestContext{
		CorrelationID:   ids.CorrelationID,
		JobID:           ids.JobID,
		DocumentID:      ids.DocumentID,
		VersionID:       ids.VersionID,
		OrganizationID:  ids.OrganizationID,
		CreatedByUserID: ids.CreatedByUserID,
	})
}

// Compile-time interface satisfaction.
var (
	_ pipeline.Logger             = stdLogger{}
	_ pendingconfirmation.Logger  = stdLogger{}
	_ dmawaiter.Logger            = stdLogger{}
	_ ingressrouter.Logger        = stdLogger{}
	_ idempotency.Logger          = stdLogger{}
	_ dmpub.Logger                = stdLogger{}
	_ orch.Logger                 = stdLogger{}
	_ dlq.Logger                  = stdLogger{}
	_ consumer.Logger             = consumerLogger{}
)

// -----------------------------------------------------------------------------
// Clock adapters.
// -----------------------------------------------------------------------------

// sysClock is the production clock seam — UTC wall time and Since().
type sysClock struct{}

func (sysClock) Now() time.Time                  { return time.Now().UTC() }
func (sysClock) Since(t time.Time) time.Duration { return time.Since(t) }

var (
	_ pipeline.Clock            = sysClock{}
	_ pendingconfirmation.Clock = sysClock{}
	_ dmawaiter.Clock           = sysClock{}
	_ ingressrouter.Clock       = sysClock{}
	_ consumer.Clock            = sysClock{}
	_ dmpub.Clock               = sysClock{}
	_ orch.Clock                = sysClock{}
	_ dlq.Clock                 = sysClock{}
)

// -----------------------------------------------------------------------------
// Pipeline metrics adapter.
// -----------------------------------------------------------------------------

// pipelineMetrics adapts *metrics.PipelineMetrics onto pipeline.PipelineMetrics.
type pipelineMetrics struct{ p *metrics.PipelineMetrics }

func (m pipelineMetrics) PipelineStarted(mode string) {
	m.p.StartedTotal.WithLabelValues(mode).Inc()
}
func (m pipelineMetrics) PipelineFinished(mode, outcome string, seconds float64) {
	m.p.TotalDurationSeconds.WithLabelValues(mode, outcome).Observe(seconds)
}
func (m pipelineMetrics) PipelineOutcome(mode, outcome, errorCode string) {
	m.p.OutcomeTotal.WithLabelValues(mode, outcome, errorCode).Inc()
}

var _ pipeline.PipelineMetrics = pipelineMetrics{}

// -----------------------------------------------------------------------------
// Stage metrics + tracer adapters (stages.Executor).
// -----------------------------------------------------------------------------

// stageMetrics adapts *metrics.PipelineMetrics onto stages.StageMetrics.
type stageMetrics struct{ p *metrics.PipelineMetrics }

func (m stageMetrics) StageDuration(stage string, seconds float64) {
	m.p.StageDurationSeconds.WithLabelValues(stage).Observe(seconds)
}

var _ stages.StageMetrics = stageMetrics{}

// -----------------------------------------------------------------------------
// Agent metrics adapter (base.Metrics).
// -----------------------------------------------------------------------------

// agentMetrics adapts *metrics.AgentMetrics onto base.Metrics.
type agentMetrics struct{ a *metrics.AgentMetrics }

func (m agentMetrics) Invocation(agent, outcome string) {
	m.a.InvocationsTotal.WithLabelValues(agent, outcome).Inc()
}
func (m agentMetrics) Duration(agent string, seconds float64) {
	m.a.DurationSeconds.WithLabelValues(agent).Observe(seconds)
}
func (m agentMetrics) InputTokens(agent string, tokens int) {
	m.a.InputTokens.WithLabelValues(agent).Observe(float64(tokens))
}
func (m agentMetrics) OutputTokens(agent string, tokens int) {
	m.a.OutputTokens.WithLabelValues(agent).Observe(float64(tokens))
}

var _ base.Metrics = agentMetrics{}

// -----------------------------------------------------------------------------
// schemavalidator.Metrics adapter (repair-loop counters).
// -----------------------------------------------------------------------------

// repairMetrics is a noop today — the metrics package does not expose a
// dedicated lic_agent_repair_* family yet. We keep the seam wired so adding
// the family later requires no API change to base/schemavalidator.
type repairMetrics struct{}

func (repairMetrics) RepairAttempt(agent, provider string)                  {}
func (repairMetrics) RepairOutcome(agent, provider, outcome string)         {}

var _ schemavalidator.Metrics = repairMetrics{}

// -----------------------------------------------------------------------------
// Aggregator metrics adapter.
// -----------------------------------------------------------------------------

// aggregatorMetrics adapts *metrics.CrossCutMetrics onto aggregator.Metrics.
type aggregatorMetrics struct{ c *metrics.CrossCutMetrics }

func (m aggregatorMetrics) PromptInjectionDetected(agent string) {
	m.c.PromptInjectionDetectedTotal.WithLabelValues(agent).Inc()
}

var _ aggregator.Metrics = aggregatorMetrics{}

// -----------------------------------------------------------------------------
// LLM router seams adapter.
// -----------------------------------------------------------------------------

// routerMetrics adapts *metrics.LLMMetrics onto router.Metrics.
type routerMetrics struct{ l *metrics.LLMMetrics }

func (m routerMetrics) ProviderFallback(from, to port.LLMProviderID, agent model.AgentID) {
	m.l.ProviderFallbackTotal.WithLabelValues(from.String(), to.String(), string(agent)).Inc()
}
func (m routerMetrics) ProviderSkippedUnhealthy(provider port.LLMProviderID) {
	m.l.ProviderSkippedUnhealthyTotal.WithLabelValues(provider.String()).Inc()
}
func (m routerMetrics) ProviderFailed(provider port.LLMProviderID, code port.LLMErrorCode) {
	m.l.ProviderFailedTotal.WithLabelValues(provider.String(), string(code)).Inc()
}
func (m routerMetrics) ProviderHealthState(provider port.LLMProviderID, state router.HealthState) {
	p := provider.String()
	// Exactly one of the three states is active; flip the active gauge to 1
	// and the other two to 0 so dashboards always show a single hot series.
	for _, s := range []router.HealthState{router.HealthHealthy, router.HealthUnhealthy, router.HealthPermanent} {
		v := 0.0
		if s == state {
			v = 1.0
		}
		m.l.ProviderHealthStatus.WithLabelValues(p, string(s)).Set(v)
	}
}

var _ router.Metrics = routerMetrics{}

// usageTracker bridges *cost.Tracker onto router.UsageTracker.
type usageTracker struct{ t *cost.Tracker }

func (u usageTracker) ObserveSuccess(provider port.LLMProviderID, mdl string, agent model.AgentID, resp port.CompletionResponse) {
	u.t.ObserveSuccess(cost.Usage{
		Provider:          provider,
		Model:             resp.Model,
		Agent:             agent,
		InputTokens:       resp.InputTokens,
		CachedInputTokens: resp.CachedInputTokens,
		OutputTokens:      resp.OutputTokens,
		Latency:           time.Duration(resp.LatencyMs) * time.Millisecond,
	})
}
func (u usageTracker) ObserveCall(provider port.LLMProviderID, mdl string, agent model.AgentID, outcome router.CallOutcome) {
	u.t.ObserveCall(provider, mdl, agent, cost.Outcome(outcome))
}

var _ router.UsageTracker = usageTracker{}

// -----------------------------------------------------------------------------
// cost.Recorder adapter — bridges to *metrics.LLMMetrics.
// -----------------------------------------------------------------------------

// costRecorder bridges *metrics.LLMMetrics onto cost.Recorder.
type costRecorder struct{ l *metrics.LLMMetrics }

func (r costRecorder) RecordUsage(provider, mdl, agent string, input, cached, output int, costUSD float64, latency time.Duration) {
	r.l.InputTokensTotal.WithLabelValues(provider, mdl, agent).Add(float64(input))
	r.l.CachedTokensTotal.WithLabelValues(provider, mdl, agent).Add(float64(cached))
	r.l.OutputTokensTotal.WithLabelValues(provider, mdl, agent).Add(float64(output))
	r.l.CostUSDTotal.WithLabelValues(provider, mdl, agent).Add(costUSD)
	r.l.LatencySeconds.WithLabelValues(provider, mdl, agent).Observe(latency.Seconds())
}
func (r costRecorder) RecordCall(provider, mdl, agent, outcome string) {
	r.l.CallsTotal.WithLabelValues(provider, mdl, agent, outcome).Inc()
}
func (r costRecorder) UnknownModel(provider, mdl string) {
	// v1: no dedicated unknown-model counter; the missed-pricing surfaces
	// only via cost-USD running flat. A future telemetry pass should add a
	// provider-labelled counter — recorded so the seam is wired through.
}

var _ cost.Recorder = costRecorder{}

// -----------------------------------------------------------------------------
// dmawaiter.Metrics adapter.
// -----------------------------------------------------------------------------

// dmAwaiterMetrics adapts *metrics.DMMetrics onto dmawaiter.Metrics.
type dmAwaiterMetrics struct{ d *metrics.DMMetrics }

func (m dmAwaiterMetrics) RecordOutcome(op, outcome string, seconds float64) {
	m.d.RequestDurationSeconds.WithLabelValues(op).Observe(seconds)
	m.d.RequestOutcomeTotal.WithLabelValues(op, outcome).Inc()
}

var _ dmawaiter.Metrics = dmAwaiterMetrics{}

// -----------------------------------------------------------------------------
// pendingconfirmation.Metrics adapter.
// -----------------------------------------------------------------------------

// pendingMetrics adapts *metrics.PendingMetrics onto pendingconfirmation.Metrics.
type pendingMetrics struct{ p *metrics.PendingMetrics }

func (m pendingMetrics) PendingStateInc()                       { m.p.StateCount.Inc() }
func (m pendingMetrics) PendingStateDec()                       { m.p.StateCount.Dec() }
func (m pendingMetrics) PendingStateAgeMaxSeconds(s float64)    { m.p.StateAgeSecondsMax.Set(s) }
func (m pendingMetrics) UserConfirmation(outcome string) {
	m.p.UserConfirmationReceivedTotal.WithLabelValues(outcome).Inc()
}

var _ pendingconfirmation.Metrics = pendingMetrics{}

// -----------------------------------------------------------------------------
// Idempotency seam adapters.
// -----------------------------------------------------------------------------

// idempotencyMetrics adapts *metrics.IdempotencyMetrics onto idempotency.Metrics.
type idempotencyMetrics struct{ i *metrics.IdempotencyMetrics }

func (m idempotencyMetrics) Lookup(result string) {
	m.i.LookupsTotal.WithLabelValues(result).Inc()
}
func (m idempotencyMetrics) Fallback() { m.i.FallbackTotal.Inc() }

var _ idempotency.Metrics = idempotencyMetrics{}

// -----------------------------------------------------------------------------
// Consumer / Publisher metrics adapters.
// -----------------------------------------------------------------------------

// consumerMetrics adapts *metrics.CrossCutMetrics onto consumer.Metrics.
type consumerMetrics struct{ c *metrics.CrossCutMetrics }

func (m consumerMetrics) ConsumerMessage(topic, outcome string) {
	m.c.ConsumerMessagesTotal.WithLabelValues(topic, outcome).Inc()
}

var _ consumer.Metrics = consumerMetrics{}

// dmPubMetrics adapts *metrics.CrossCutMetrics + *metrics.DMMetrics onto
// dmpub.Metrics (the publisher seam carries BOTH the publisher-outcome counter
// and the analysis-ready size histogram).
type dmPubMetrics struct {
	c *metrics.CrossCutMetrics
	d *metrics.DMMetrics
}

func (m dmPubMetrics) IncPublish(topic string, outcome dmpub.PublishOutcome) {
	m.c.PublisherMessagesTotal.WithLabelValues(topic, string(outcome)).Inc()
}
func (m dmPubMetrics) ObservePublishedSize(bytes int) {
	m.d.ArtifactsPublishedSizeBytes.Observe(float64(bytes))
}

var _ dmpub.Metrics = dmPubMetrics{}

// orchPubMetrics adapts *metrics.CrossCutMetrics onto orch.Metrics.
type orchPubMetrics struct{ c *metrics.CrossCutMetrics }

func (m orchPubMetrics) IncPublish(topic string, outcome orch.PublishOutcome) {
	m.c.PublisherMessagesTotal.WithLabelValues(topic, string(outcome)).Inc()
}

var _ orch.Metrics = orchPubMetrics{}

// dlqPubMetrics adapts *metrics.CrossCutMetrics + *metrics.DLQMetrics onto
// dlq.Metrics.
type dlqPubMetrics struct {
	c *metrics.CrossCutMetrics
	d *metrics.DLQMetrics
}

func (m dlqPubMetrics) IncPublish(topic string, outcome dlq.PublishOutcome) {
	m.c.PublisherMessagesTotal.WithLabelValues(topic, string(outcome)).Inc()
}
func (m dlqPubMetrics) IncDLQPublished(topic, reason string) {
	m.d.PublishedTotal.WithLabelValues(topic, reason).Inc()
}

var _ dlq.Metrics = dlqPubMetrics{}

// -----------------------------------------------------------------------------
// Health checker adapters.
// -----------------------------------------------------------------------------

// pingFunc is the minimal Ping contract every infra client satisfies.
type pingFunc func(ctx context.Context) error

// healthChecker is a generic Checker wrapper used for redis / broker probes.
type healthChecker struct {
	name string
	ping pingFunc
}

func (h healthChecker) Name() string                       { return h.name }
func (h healthChecker) Check(ctx context.Context) error    { return h.ping(ctx) }

var _ health.Checker = healthChecker{}

// -----------------------------------------------------------------------------
// HTTP plumbing helpers.
// -----------------------------------------------------------------------------

// noopHandler is used when there is no metrics handler attached (tests).
type noopHandler struct{}

func (noopHandler) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusNotImplemented)
}

var _ http.Handler = noopHandler{}
