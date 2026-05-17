package base

import (
	"context"

	"contractpro/legal-intelligence-core/internal/domain/port"
)

// Outcome is the lic_agent_invocations_total{outcome} label value
// (observability.md §3.3 — the authoritative enum SSOT). It is a LOCAL MIRROR
// of metrics.AgentInvocationOutcome — declared here, not imported, so this
// package stays hermetic (no internal/infra/observability/metrics import
// before LIC-TASK-047), exactly like router.CallOutcome / cost.Outcome /
// schemavalidator.RepairOutcome. seams_test.go pins the five wire strings
// against the shipped metrics/labels.go SSOT so the mirror cannot drift.
//
// The five values are a CLOSED set (observability.md §3.3 / cardinality
// budget §3.10: 9 agents × 5 outcomes = 45 series). LIC build defects
// (a broken embedded schema → INTERNAL_ERROR, a spec.Decode mismatch, a
// promptbuilder.Build / spec.Parts fail-fast) have NO dedicated value; they
// are projected onto OutcomeInvalidOutput as a DELIBERATE, documented lossy
// projection (code-architect MF-2). The un-lossy truth is never discarded:
// the *model.DomainError (Code=INTERNAL_ERROR, Stage, Cause) is returned to
// the caller AND recorded on the OTel agent span as status=Error, so the
// build-defect signal survives on traces even though the metric label is the
// closest closed-enum approximation. See base.go classifyRepairError /
// CLAUDE.md "Lossy outcome projection".
type Outcome string

const (
	// OutcomeSuccess — the primary LLM response passed schema validation
	// with NO repair turn issued (MF-4: zero repair metrics on this path).
	OutcomeSuccess Outcome = "success"
	// OutcomeRepairSuccess — the primary response failed schema validation,
	// exactly one sticky repair turn was issued, and the repaired response
	// validated.
	OutcomeRepairSuccess Outcome = "repair_success"
	// OutcomeInvalidOutput — repair produced output that still failed schema
	// validation (repair_failed), OR a LIC build defect (broken embedded
	// schema, spec.Decode/Parts mismatch, promptbuilder.Build fail-fast)
	// projected here as the closest closed-enum value (MF-2 lossy projection;
	// the returned DomainError + span status carry the real INTERNAL_ERROR).
	OutcomeInvalidOutput Outcome = "invalid_output"
	// OutcomeProviderError — the primary call exhausted the provider chain,
	// or the sticky repair call itself failed at the provider. Also the
	// projection target for the pre-call AGENT_INPUT_TOO_LARGE over-budget
	// path (no provider was called) — chosen for parity with the
	// CONTEXT_TOO_LONG→provider_error mapping so both unsalvageable-input
	// routes carry one outcome (a documented lossy projection, MF-2/MF-3).
	OutcomeProviderError Outcome = "provider_error"
	// OutcomeTimeout — the per-agent context deadline (cfg.Timeout) fired.
	OutcomeTimeout Outcome = "timeout"
)

// String returns the wire representation of the outcome.
func (o Outcome) String() string { return string(o) }

// IsValid reports whether o is one of the five declared invocation outcomes.
func (o Outcome) IsValid() bool {
	switch o {
	case OutcomeSuccess, OutcomeRepairSuccess, OutcomeInvalidOutput,
		OutcomeProviderError, OutcomeTimeout:
		return true
	default:
		return false
	}
}

// Metrics is the telemetry seam for the four BaseAgent-owned Prometheus vecs
// declared centrally in metrics/agent.go (observability.md §3.3). The repair
// pair (lic_agent_repair_attempts_total / lic_agent_repair_outcome_total) is
// NOT here — it is owned by schemavalidator.RepairLoop's own Metrics seam
// (Deps.RepairMetrics), so the two metric families stay with their producers.
//
// The concrete adapter over *metrics.AgentMetrics is wired in LIC-TASK-047;
// an internal/infra/observability/metrics import here would break the
// hermeticity invariant every internal/agents|llm/* package upholds before
// app-wiring (mirrors router.Metrics / cost.Recorder / schemavalidator.Metrics).
//
// agent / outcome label values MUST come from typed sources only
// (model.AgentID.String() / Outcome.String()) so {agent,outcome} cardinality
// stays the metrics/agent.go-budgeted 9 × 5 = 45 series.
type Metrics interface {
	// Invocation increments lic_agent_invocations_total{agent,outcome}.
	// Called EXACTLY once per Run() with the terminal outcome (code-architect
	// MF-5: single emission site, every path assigns outcome first).
	Invocation(agent, outcome string)
	// Duration observes lic_agent_duration_seconds{agent}. Always recorded
	// (success and every failure path), including repair time.
	Duration(agent string, seconds float64)
	// InputTokens observes lic_agent_input_tokens{agent}. Recorded ONLY when
	// the token estimate was actually computed (TokenEstimator.Fit ran) —
	// never with a zero placeholder on a pre-Fit build-defect path (MF-5).
	InputTokens(agent string, tokens int)
	// OutputTokens observes lic_agent_output_tokens{agent}. Recorded ONLY on
	// success | repair_success (a response was produced); provider-reported
	// (CompletionResponse.OutputTokens) (MF-5).
	OutputTokens(agent string, tokens int)
}

// noopMetrics is the zero-dependency default so BaseAgent is usable in tests
// and before LIC-TASK-047 wires Prometheus, without a per-call nil check
// (mirrors router.noopMetrics / cost.noopRecorder / schemavalidator.noopMetrics).
type noopMetrics struct{}

func (noopMetrics) Invocation(string, string) {}
func (noopMetrics) Duration(string, float64)  {}
func (noopMetrics) InputTokens(string, int)   {}
func (noopMetrics) OutputTokens(string, int)  {}

var _ Metrics = noopMetrics{}

// TokenEstimator is the seam over the Token Estimator (LIC-TASK-021, not yet
// implemented and NOT a dependency of this task). Step 2 of the §6.6 agent
// loop ("проверить input fits in model context").
//
// CONTRACT (code-architect MF-3): Fit estimates the input-token count of the
// ALREADY-ASSEMBLED request for its target model and reports whether it
// exceeds the model budget. Fit MUST NOT mutate req. The §6.7 /
// ASSUMPTION-LIC-12 head-60/tail-40 truncation of EXTRACTED_TEXT is
// per-artifact and happens UPSTREAM of spec.Parts/promptbuilder.Build (it is
// LIC-TASK-021's job operating on model.AgentInput artifacts) — truncating
// the assembled <input>…</input> envelope here would slice an escaped XML
// entity and defeat prompt-injection defence layer 2, so the seam is shaped
// to make envelope corruption a type-level impossibility (no request is
// returned to mutate).
//
// v1 behaviour: the default passthroughEstimator estimates ⌈runes/4⌉ and
// always returns overBudget=false, so the size verdict is delegated to the
// provider's CONTEXT_TOO_LONG, which Run maps to AGENT_INPUT_TOO_LARGE
// (non-retryable). When LIC-TASK-021's real estimator is wired and a request
// cannot be salvaged by upstream truncation it returns overBudget=true and
// Run fails fast with AGENT_INPUT_TOO_LARGE WITHOUT burning an LLM call —
// both paths reach the identical error code.
type TokenEstimator interface {
	Fit(req port.CompletionRequest) (estInputTokens int, overBudget bool)
}

// passthroughEstimator is the zero-dependency default. ⌈runes/4⌉ over the
// System + User + PriorTurns content is the well-known rough heuristic; it is
// deliberately a placeholder (LIC-TASK-021 owns the real per-model
// tokeniser). It never reports over-budget — see the TokenEstimator contract.
type passthroughEstimator struct{}

func (passthroughEstimator) Fit(req port.CompletionRequest) (int, bool) {
	runes := len([]rune(req.System)) + len([]rune(req.User))
	for _, t := range req.PriorTurns {
		runes += len([]rune(t.Content))
	}
	est := (runes + 3) / 4 // ⌈runes/4⌉
	return est, false
}

var _ TokenEstimator = passthroughEstimator{}

// Correlation is the per-run correlation-ID set lifted verbatim from
// model.AgentInput onto the agent span (observability.md §4.3: every span
// carries correlation_id/job_id/version_id/document_id/organization_id/
// created_by_user_id). It is a plain container so the package stays free of a
// tracer import; the LIC-TASK-047 adapter maps it onto tracer.SpanFields.
type Correlation struct {
	CorrelationID   string
	JobID           string
	VersionID       string
	DocumentID      string
	OrganizationID  string
	CreatedByUserID string
}

// AgentSpanInput is the attribute set known at agent-span start: the agent id
// (→ lic.agent.id and the lic.agent.<name> span name, composed by the 047
// adapter) plus the correlation IDs.
type AgentSpanInput struct {
	AgentID     string
	Correlation Correlation
}

// AgentSpanOutput is the attribute set known at agent-span finish
// (observability.md §4.3 agent-span keys).
type AgentSpanOutput struct {
	Outcome        string
	RepairAttempts int
}

// LLMSpanOutput is the attribute set for the child lic.llm.call span
// (observability.md §4.3 llm-span keys). The child span wraps the primary
// call AND the sticky repair as ONE logical LLM-call span; on a
// repair_provider_error the response-derived fields (Provider/Model/
// OutputTokens/LatencyMs) therefore reflect the PRIMARY call (the failed
// repair produced no usable response), while the parent agent span carries
// the error + outcome. cost_usd, cached_tokens and fallback_used are
// intentionally absent: the Router owns provider selection and does not
// expose the agent→primary map, so fallback_used is not derivable here, and
// cost-USD span attribution is a forward note for the span owner
// (router/CLAUDE.md "Out of scope"). They are set by a later task.
type LLMSpanOutput struct {
	Provider     string
	Model        string
	InputTokens  int
	OutputTokens int
	LatencyMs    int64
}

// Tracer is the OTel seam. The §4.2 span tree mandates a parent
// lic.agent.<name> span with a child lic.llm.call span; a single flattened
// span would violate the pinned span schema (code-architect MF-4). The
// concrete adapter over *tracer.Tracer is wired in LIC-TASK-047 — a
// go.opentelemetry.io / internal/infra/observability/tracer import here would
// break hermeticity (no internal/agents|llm/* package imports tracer before
// app-wiring; router/CLAUDE.md defers the lic.llm span to "the agent task" =
// THIS task, behind a seam exactly like every other telemetry collaborator).
type Tracer interface {
	// StartAgent opens the parent lic.agent.<name> span. The returned ctx
	// carries the span so the child llm-call span (and any downstream router
	// spans) nest correctly.
	StartAgent(ctx context.Context, in AgentSpanInput) (context.Context, AgentSpan)
}

// AgentSpan is the parent-span handle. Finish is called EXACTLY once
// (deferred) with the terminal attributes; a non-nil err is recorded as
// span status=Error carrying the REAL error (incl. the un-lossy
// INTERNAL_ERROR truth behind a lossy invalid_output metric label — MF-2).
type AgentSpan interface {
	// StartLLMCall opens the child lic.llm.call span around the
	// router.Complete (+ sticky repair) calls.
	StartLLMCall(ctx context.Context) (context.Context, LLMSpan)
	Finish(out AgentSpanOutput, err error)
}

// LLMSpan is the child-span handle. Finish is called EXACTLY once (deferred)
// with whatever is known — on a primary-call failure Provider/Model/tokens
// may be zero-valued; the span still closes.
type LLMSpan interface {
	Finish(out LLMSpanOutput)
}

// noopTracer / noopAgentSpan / noopLLMSpan are the zero-dependency defaults
// (mirrors every other seam's noop). StartAgent / StartLLMCall return the ctx
// unchanged so trace context propagation still works when tracing is off.
type noopTracer struct{}

func (noopTracer) StartAgent(ctx context.Context, _ AgentSpanInput) (context.Context, AgentSpan) {
	return ctx, noopAgentSpan{}
}

type noopAgentSpan struct{}

func (noopAgentSpan) StartLLMCall(ctx context.Context) (context.Context, LLMSpan) {
	return ctx, noopLLMSpan{}
}
func (noopAgentSpan) Finish(AgentSpanOutput, error) {}

type noopLLMSpan struct{}

func (noopLLMSpan) Finish(LLMSpanOutput) {}

var (
	_ Tracer    = noopTracer{}
	_ AgentSpan = noopAgentSpan{}
	_ LLMSpan   = noopLLMSpan{}
)
