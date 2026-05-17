// Package base is the BaseAgent runner — the uniform Run() workflow shared by
// all 9 LIC AI agents (LIC-TASK-024, high-architecture.md §6.6/§6.8,
// ai-agents-pipeline.md, observability.md §3.3/§4.2/§4.3).
//
// It implements port.Agent. The 9 per-agent packages (LIC-TASK-025..033)
// supply only a Spec (envelope assembly + typed decode) and a per-agent
// Config (loaded system prompt, embedded schema, model/params/timeout); the
// invariant-heavy loop — Prompt Builder → Token Estimator → primary LLM call
// → Schema Validator → sticky 1-shot Repair Loop → typed decode, with
// span-per-agent and the four invocation metrics — lives here exactly once.
//
// Hermeticity. Like every internal/agents|llm/* sibling, this package imports
// ONLY stdlib + internal/domain/{model,port} + the sibling leaf agents
// packages it composes (promptbuilder, schemavalidator). Telemetry — the
// agent metrics, the OTel span tree, and the (LIC-TASK-021) token estimator —
// is inverted behind the Metrics / Tracer / TokenEstimator seams in seams.go,
// each with a zero-dependency default, so no internal/infra/observability or
// go.opentelemetry.io import enters here; the concrete adapters are wired in
// LIC-TASK-047 (mirrors router / cost / promptbuilder / schemavalidator).
// schemavalidator transitively pulls gojsonschema but re-exposes it only via
// Validate(...) error, so base gains no third-party surface
// (schemavalidator/CLAUDE.md "single-exception confinement").
//
// Concurrency. *BaseAgent is immutable after NewBaseAgent (Config value;
// Spec/seam interfaces; the stateless *schemavalidator.Validator; the
// immutable *schemavalidator.RepairLoop and *promptbuilder.Builder; the
// router immutable except its mutex-guarded health registry), so one
// *BaseAgent is shared by the parallel errgroup pipeline without locking —
// PROVIDED Spec implementations are themselves stateless / concurrent-safe
// (see the Spec godoc; pinned by TestRun_ConcurrentRaceClean, -race).
package base

import (
	"context"
	"errors"
	"fmt"
	"time"

	"contractpro/legal-intelligence-core/internal/agents/promptbuilder"
	"contractpro/legal-intelligence-core/internal/agents/schemavalidator"
	"contractpro/legal-intelligence-core/internal/domain/model"
	"contractpro/legal-intelligence-core/internal/domain/port"
)

// errInputOverBudget is the cause stamped when TokenEstimator.Fit reports the
// assembled prompt cannot be salvaged by upstream truncation (LIC-TASK-021
// real estimator only; the default passthrough never reports over-budget).
var errInputOverBudget = errors.New("base: assembled prompt exceeds model input budget after upstream truncation")

// Config is the static, per-agent configuration. The per-agent package
// (LIC-TASK-025..033) populates it: System = prompts.LoadPrompt(AgentID),
// Schema = schemas.LoadSchema(AgentID), Model/MaxTokens/Temperature from the
// ai-agents-pipeline.md "Бюджеты и параметры LLM" table, Timeout from
// config.AgentsConfig.Timeouts[AgentID]. It is a value (copied into the
// immutable *BaseAgent), never mutated at runtime.
type Config struct {
	AgentID     model.AgentID
	Stage       model.Stage
	System      string
	Schema      []byte
	Model       string
	MaxTokens   int
	Temperature float64
	Timeout     time.Duration
}

// Spec is the per-agent strategy: the only two pieces that genuinely differ
// across the 9 agents. The 9 packages each implement it; BaseAgent owns
// everything else.
//
// Implementations MUST be stateless / safe for concurrent use: one
// *BaseAgent (hence one Spec) is shared by the parallel errgroup stages, so
// Parts and Decode may run concurrently for different contracts. Hold no
// per-call mutable state on the Spec receiver.
type Spec interface {
	// Parts builds the ordered envelope blocks for this agent from input.
	// b is the shared immutable Builder — agent 3 uses b.ValidationFacts to
	// mint its <validation_facts> block; all user-controlled content goes
	// through promptbuilder.Content (escaped). BaseAgent passes the result to
	// b.Build(AgentID, System, parts). A returned error is a LIC programming
	// defect (it is surfaced as INTERNAL_ERROR, never sent to the LLM).
	Parts(b *promptbuilder.Builder, in model.AgentInput) ([]promptbuilder.Part, error)

	// Decode unmarshals the schema-validated response JSON into the agent's
	// concrete typed result (e.g. *model.ClassificationResult). The returned
	// value is the port.AgentResult the Stage Executor narrows by AgentID. A
	// decode failure means the schema and the Go struct disagree — a LIC
	// build defect, surfaced as INTERNAL_ERROR.
	Decode(content []byte) (port.AgentResult, error)
}

// Deps bundles the injectable collaborators. Every nil field degrades to its
// zero-dependency default (noop seams, default Builder, real clock) so the
// common production path passes only Router and the three 047 adapters, and
// tests override exactly what they need (router.Deps.withDefaults pattern).
type Deps struct {
	Router        port.ProviderRouterPort
	Builder       *promptbuilder.Builder
	Metrics       Metrics
	RepairMetrics schemavalidator.Metrics
	Tracer        Tracer
	Estimator     TokenEstimator

	// now is injected only by tests for deterministic duration. Production
	// leaves it nil → time.Now.
	now func() time.Time
}

func (d Deps) withDefaults() Deps {
	if d.Builder == nil {
		d.Builder = promptbuilder.NewBuilder(nil)
	}
	if d.Metrics == nil {
		d.Metrics = noopMetrics{}
	}
	if d.Tracer == nil {
		d.Tracer = noopTracer{}
	}
	if d.Estimator == nil {
		d.Estimator = passthroughEstimator{}
	}
	if d.now == nil {
		d.now = time.Now
	}
	// RepairMetrics intentionally left as-is: schemavalidator.NewRepairLoop
	// already maps nil → noop (its documented contract).
	return d
}

// canonicalStage is the AgentID → STAGE_AGENT_* bijection (status.go /
// agent.go). NewBaseAgent cross-checks cfg.Stage against it so a per-agent
// task that wires a mismatched pair fails at construction, not via a
// mislabeled lic.events.status-changed in production (code-architect N3).
// Explicit enumerated table per the house style (cf. prompts.basenames).
var canonicalStage = map[model.AgentID]model.Stage{
	model.AgentTypeClassifier:      model.StageAgentTypeClassifier,
	model.AgentKeyParams:           model.StageAgentKeyParams,
	model.AgentPartyConsistency:    model.StageAgentPartyConsistency,
	model.AgentMandatoryConditions: model.StageAgentMandatoryConditions,
	model.AgentRiskDetection:       model.StageAgentRiskDetection,
	model.AgentRecommendation:      model.StageAgentRecommendation,
	model.AgentSummary:             model.StageAgentSummary,
	model.AgentDetailedReport:      model.StageAgentDetailedReport,
	model.AgentRiskDelta:           model.StageAgentRiskDelta,
}

// BaseAgent is the shared runner. Immutable after NewBaseAgent.
type BaseAgent struct {
	cfg       Config
	spec      Spec
	builder   *promptbuilder.Builder
	router    port.ProviderRouterPort
	repair    *schemavalidator.RepairLoop
	validator *schemavalidator.Validator
	metrics   Metrics
	tracer    Tracer
	estimator TokenEstimator
	now       func() time.Time
}

// Compile-time assertion that BaseAgent satisfies the port contract so the 9
// per-agent packages can embed it directly.
var _ port.Agent = (*BaseAgent)(nil)

// NewBaseAgent validates the wiring and assembles the runner. It fails fast
// (like NewProviderRouter / NewRepairLoop / NewBuilder — stutter-free
// NewTypeName per the codebase-wide convention / feedback_constructors.md;
// the constructor name deliberately keeps the type name, NOT bare New) so a
// misconfiguration is a startup error, not a first-contract runtime surprise.
func NewBaseAgent(cfg Config, spec Spec, deps Deps) (*BaseAgent, error) {
	if spec == nil {
		return nil, errors.New("base: spec must not be nil")
	}
	if deps.Router == nil {
		return nil, errors.New("base: Deps.Router must not be nil")
	}
	if !cfg.AgentID.IsValid() {
		return nil, fmt.Errorf("base: invalid agent id %q", cfg.AgentID)
	}
	if !cfg.Stage.IsValid() {
		return nil, fmt.Errorf("base: agent %s: invalid stage %q", cfg.AgentID, cfg.Stage)
	}
	if want, ok := canonicalStage[cfg.AgentID]; !ok || cfg.Stage != want {
		return nil, fmt.Errorf("base: agent %s requires stage %q, got %q", cfg.AgentID, want, cfg.Stage)
	}
	if cfg.System == "" {
		return nil, fmt.Errorf("base: agent %s: empty system prompt", cfg.AgentID)
	}
	if len(cfg.Schema) == 0 {
		return nil, fmt.Errorf("base: agent %s: empty schema", cfg.AgentID)
	}
	if cfg.Model == "" {
		return nil, fmt.Errorf("base: agent %s: empty model id", cfg.AgentID)
	}
	if cfg.MaxTokens <= 0 {
		return nil, fmt.Errorf("base: agent %s: MaxTokens must be > 0, got %d", cfg.AgentID, cfg.MaxTokens)
	}
	if cfg.Temperature < 0 || cfg.Temperature > 1 {
		return nil, fmt.Errorf("base: agent %s: Temperature must be in [0,1], got %v", cfg.AgentID, cfg.Temperature)
	}
	if cfg.Timeout <= 0 {
		return nil, fmt.Errorf("base: agent %s: Timeout must be > 0, got %s", cfg.AgentID, cfg.Timeout)
	}

	d := deps.withDefaults()
	// RepairLoop is built with the SAME router instance so the sticky repair
	// (OQ-10) re-issues on exactly the provider that served the primary call
	// (S1: propagate the constructor error, never discard it).
	repair, err := schemavalidator.NewRepairLoop(d.Router, d.RepairMetrics)
	if err != nil {
		return nil, fmt.Errorf("base: agent %s: build repair loop: %w", cfg.AgentID, err)
	}

	return &BaseAgent{
		cfg:       cfg,
		spec:      spec,
		builder:   d.Builder,
		router:    d.Router,
		repair:    repair,
		validator: schemavalidator.NewValidator(),
		metrics:   d.Metrics,
		tracer:    d.Tracer,
		estimator: d.Estimator,
		now:       d.now,
	}, nil
}

// ID returns the stable AgentID (port.Agent).
func (a *BaseAgent) ID() model.AgentID { return a.cfg.AgentID }

// Run executes one agent invocation: the §6.6 loop. It returns the per-agent
// typed result on success, or a *model.DomainError the Pipeline Orchestrator
// maps to lic.events.status-changed (Code / Retryable / UserMessage / Stage).
//
// Telemetry is emitted from a SINGLE deferred site (code-architect MF-5):
// lic_agent_invocations_total fires exactly once with the terminal outcome
// (initialised to the build-defect sentinel so even a pre-call failure
// carries a valid closed-enum label); lic_agent_duration_seconds always;
// lic_agent_input_tokens only once the estimate was computed;
// lic_agent_output_tokens only on success|repair_success. The child
// lic.llm.call span is finished before the parent lic.agent span (§4.2 tree).
func (a *BaseAgent) Run(ctx context.Context, input model.AgentInput) (result port.AgentResult, err error) {
	agentLabel := a.cfg.AgentID.String()
	started := a.now()

	// MF-5 sentinel: any return before a fork sets it is a LIC build defect,
	// which projects onto invalid_output (MF-2 lossy projection).
	outcome := OutcomeInvalidOutput
	repairAttempts := 0
	est := 0
	fitRan := false
	llmStarted := false
	var lspan LLMSpan
	var llmOut LLMSpanOutput

	sctx, aspan := a.tracer.StartAgent(ctx, AgentSpanInput{
		AgentID:     agentLabel,
		Correlation: correlationFrom(input),
	})

	// Single terminal telemetry site. Registered first ⇒ runs LAST (LIFO),
	// after StartLLMCall's lspan has been finished here too — yielding the
	// §4.2 child-before-parent close order, exactly once, on every path.
	defer func() {
		a.metrics.Invocation(agentLabel, outcome.String())
		a.metrics.Duration(agentLabel, a.now().Sub(started).Seconds())
		if fitRan {
			a.metrics.InputTokens(agentLabel, est)
		}
		if outcome == OutcomeSuccess || outcome == OutcomeRepairSuccess {
			a.metrics.OutputTokens(agentLabel, llmOut.OutputTokens)
		}
		if llmStarted {
			lspan.Finish(llmOut)
		}
		aspan.Finish(AgentSpanOutput{Outcome: outcome.String(), RepairAttempts: repairAttempts}, err)
	}()

	// Step 2: per-agent envelope assembly + Build. promptbuilder.Build's
	// deterministic fail-fast error (empty system / no parts / bad|dup tag /
	// empty minted block) is a LIC programming defect — never swallowed
	// (code-architect MF-6).
	parts, perr := a.spec.Parts(a.builder, input)
	if perr != nil {
		outcome = OutcomeInvalidOutput
		err = model.NewDomainError(model.ErrCodeInternal, a.cfg.Stage).
			WithCause(perr).WithAttribute("agent_id", agentLabel)
		return
	}
	req, berr := a.builder.Build(a.cfg.AgentID, a.cfg.System, parts)
	if berr != nil {
		outcome = OutcomeInvalidOutput
		err = model.NewDomainError(model.ErrCodeInternal, a.cfg.Stage).
			WithCause(berr).WithAttribute("agent_id", agentLabel)
		return
	}

	// Step 3: agent/router-layer fields (promptbuilder sets only
	// AgentID/System/User per its contract).
	req.Model = a.cfg.Model
	req.MaxTokens = a.cfg.MaxTokens
	req.Temperature = a.cfg.Temperature
	req.JSONSchema = a.cfg.Schema
	// Redundant per port/llm.go (JSONSchema implies JSONMode) but set
	// explicitly as defence-in-depth for any adapter that branches on
	// JSONMode without first checking JSONSchema (N2).
	req.JSONMode = true

	// Step 4: token estimate. Fit MUST NOT mutate req (MF-3) — per-artifact
	// head/tail truncation is LIC-TASK-021's job upstream of spec.Parts.
	estTokens, overBudget := a.estimator.Fit(req)
	est = estTokens
	fitRan = true
	if overBudget {
		// Real LIC-TASK-021 estimator only: unsalvageable. Fail fast without
		// burning an LLM call — the same code the provider's CONTEXT_TOO_LONG
		// path produces (MF-3). AGENT_INPUT_TOO_LARGE has no dedicated
		// invocation outcome; projected to provider_error for parity with the
		// CONTEXT_TOO_LONG mapping in classifyCompleteError (MF-2).
		outcome = OutcomeProviderError
		err = model.NewDomainError(model.ErrCodeAgentInputTooLarge, a.cfg.Stage).
			WithCause(errInputOverBudget).WithAttribute("agent_id", agentLabel)
		return
	}
	llmOut.InputTokens = est

	// Step 5/6: child lic.llm.call span, per-agent timeout, primary call.
	lctx, ls := aspan.StartLLMCall(sctx)
	lspan = ls
	llmStarted = true
	cctx, cancel := context.WithTimeout(lctx, a.cfg.Timeout)
	defer cancel()

	primary, cerr := a.router.Complete(cctx, req)
	if cerr != nil {
		code, oc := classifyCompleteError(cctx, cerr)
		outcome = oc
		err = model.NewDomainError(code, a.cfg.Stage).
			WithCause(cerr).WithAttribute("agent_id", agentLabel)
		return
	}
	llmOut.Provider = primary.UsedProvider.String()
	llmOut.Model = primary.Response.Model
	llmOut.OutputTokens = primary.Response.OutputTokens
	llmOut.LatencyMs = primary.Response.LatencyMs

	// Step 7: schema validation, then the sticky 1-shot repair ONLY if the
	// primary failed (MF-4: a valid primary issues no repair turn ⇒ zero
	// repair metrics). MF-1 load-bearing invariant: this pre-check and
	// RepairLoop's internal first validate share the SAME *Validator
	// semantics and the SAME a.cfg.Schema bytes, so the fork is deterministic
	// and RepairLoop's (resp,nil) happy arm is statically unreachable from
	// here — therefore a nil repair error PROVES a repair turn occurred
	// (repair_success); the attribution is an invariant, not a guess.
	var finalResp port.CompletionResponse
	switch verr := a.validator.Validate(a.cfg.Schema, []byte(primary.Response.Content)); {
	case verr == nil:
		outcome = OutcomeSuccess
		finalResp = primary.Response

	default:
		// repair_attempts is derived from the ACTUAL outcome of repair.Run
		// (ground truth), never a pre-call guess: this guarantees the
		// lic.agent.repair_attempts span attribute can never disagree with
		// schemavalidator's lic_agent_repair_attempts_total, even on
		// RepairLoop's defence-in-depth shape-drift branch (code-reviewer C1).
		rResp, rerr := a.repair.Run(cctx, a.cfg.AgentID, a.cfg.Stage, a.cfg.Schema, req, primary)
		if rerr != nil {
			oc, turnIssued := classifyRepairError(rerr)
			outcome = oc
			if turnIssued {
				repairAttempts = 1
			}
			err = rerr
			return
		}
		repairAttempts = 1 // (resp,nil) ⇒ repaired_ok ⇒ exactly one repair turn
		outcome = OutcomeRepairSuccess
		finalResp = rResp
		llmOut.OutputTokens = finalResp.OutputTokens
		llmOut.LatencyMs = finalResp.LatencyMs
	}

	// Step 8: per-agent typed decode. A failure is a schema/struct
	// disagreement = LIC build defect → INTERNAL_ERROR, projected to
	// invalid_output (MF-2; the returned error + span status carry the truth).
	res, derr := a.spec.Decode([]byte(finalResp.Content))
	if derr != nil {
		outcome = OutcomeInvalidOutput
		err = model.NewDomainError(model.ErrCodeInternal, a.cfg.Stage).
			WithCause(derr).WithAttribute("agent_id", agentLabel)
		return
	}

	result = res
	return
}

// correlationFrom lifts the correlation IDs off AgentInput onto the span
// container (observability.md §4.3).
func correlationFrom(in model.AgentInput) Correlation {
	return Correlation{
		CorrelationID:   in.CorrelationID,
		JobID:           in.JobID,
		VersionID:       in.VersionID,
		DocumentID:      in.DocumentID,
		OrganizationID:  in.OrganizationID,
		CreatedByUserID: in.CreatedByUserID,
	}
}

// classifyCompleteError maps a router.Complete failure to a domain ErrorCode
// and an invocation Outcome.
//
// S4: the per-agent deadline firing is the DECISIVE FIRST discriminant. The
// Router wraps ctx errors (a ctx-cancelled rate-limiter Wait surfaces as
// *port.LLMProviderError{RATE_LIMIT} wrapping ctx.Err(); ALL_PROVIDERS_FAILED
// wraps the last error), so an errors.Is(cerr, DeadlineExceeded) test first
// would mis-tag a rate-limit-during-shutdown as a timeout. cctx.Err()
// reflects only whether OUR derived per-agent deadline actually fired.
func classifyCompleteError(cctx context.Context, cerr error) (model.ErrorCode, Outcome) {
	if cctx.Err() == context.DeadlineExceeded {
		return model.ErrCodeAgentTimeout, OutcomeTimeout
	}
	if pe, ok := port.AsLLMProviderError(cerr); ok {
		switch pe.Code {
		case port.LLMErrorContextTooLong:
			return model.ErrCodeAgentInputTooLarge, OutcomeProviderError
		case port.LLMErrorQuotaExceeded:
			return model.ErrCodeLLMQuotaExceeded, OutcomeProviderError
		case port.LLMErrorContentPolicy:
			return model.ErrCodeLLMContentPolicyViolation, OutcomeProviderError
		case port.LLMErrorMalformedRequest:
			// Empty provider chain = LIC wiring/build defect → INTERNAL_ERROR
			// projected to invalid_output (MF-2; span carries the truth).
			return model.ErrCodeInternal, OutcomeInvalidOutput
		default:
			// ALL_PROVIDERS_FAILED and any raw transient surfaced directly.
			return model.ErrCodeLLMAllProvidersFailed, OutcomeProviderError
		}
	}
	if cctx.Err() == context.Canceled {
		// Parent cancel (shutdown / job abort) with NO typed error from the
		// router — rare in production, because a real router wraps a
		// ctx-cancel into a typed *LLMProviderError (handled above as
		// provider_error); this is the untyped fallback. Mapped to
		// AGENT_TIMEOUT/timeout DELIBERATELY (code-reviewer H2): the §3.3
		// closed enum has no "cancelled" value, and AGENT_TIMEOUT's
		// retryable=true is the CORRECT product behaviour here — an
		// analysis interrupted by shutdown should be re-runnable, and per
		// ASSUMPTION-LIC-19 LIC has no pipeline-level retry; is_retryable=true
		// merely lets the Orchestrator create a new (RE_CHECK) version. This
		// is a named, intentional lossy projection (see CLAUDE.md), not an
		// oversight.
		return model.ErrCodeAgentTimeout, OutcomeTimeout
	}
	// Untyped, non-ctx error from the router = adapter-invariant breach
	// (router/CLAUDE.md MF-1). Internal defect → invalid_output projection.
	return model.ErrCodeInternal, OutcomeInvalidOutput
}

// classifyRepairError maps a *model.DomainError returned by
// schemavalidator.RepairLoop.Run to an invocation Outcome AND reports whether
// a repair turn was actually issued (ground truth for the
// lic.agent.repair_attempts span attribute — code-reviewer C1; never a
// pre-call guess).
//
// MF-2 EXPLICIT discriminator (never a bare "else"): RepairLoop returns
// AGENT_OUTPUT_INVALID for BOTH repair_failed (Cause = *SchemaViolation) and
// repair_provider_error (Cause = *port.LLMProviderError) — in BOTH a turn was
// issued — and INTERNAL_ERROR for a (defence-in-depth) *SchemaCompileError /
// Validate shape-drift, where RepairLoop returns BEFORE issuing a turn. The
// SchemaViolation/LLMProviderError causes are STRICTLY disjoint
// (schemavalidator MF-2), so As-probing in this order is exact.
func classifyRepairError(rerr error) (outcome Outcome, turnIssued bool) {
	if _, ok := schemavalidator.AsSchemaViolation(rerr); ok {
		return OutcomeInvalidOutput, true // repair_failed: a turn WAS issued
	}
	if _, ok := port.AsLLMProviderError(rerr); ok {
		return OutcomeProviderError, true // repair_provider_error: turn issued
	}
	// INTERNAL_ERROR (broken embedded schema / shape-drift): RepairLoop
	// returned before any CompleteRepair → invalid_output projection (rerr +
	// span carry the real code), and NO repair turn was issued.
	return OutcomeInvalidOutput, false
}
