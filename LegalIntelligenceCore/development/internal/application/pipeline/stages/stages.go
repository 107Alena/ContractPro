// Package stages implements the LIC Stage Executor (LIC-TASK-034,
// high-architecture.md §4.3.1 / §8.1, ai-agents-pipeline.md §0.1 / §"errgroup
// для параллельных стадий", error-handling.md §7.3 / §8, observability.md
// §3.2 / §4.2). It owns the six pipeline stages that orchestrate the nine
// LIC AI agents:
//
//	RunStage1  Type Classifier ‖ Key Parameters Extractor   (parallel, both must succeed)
//	RunStage2  Party Data Consistency                        (sequential, NON-CRITICAL)
//	RunStage3  Mandatory Conditions ‖ Risk Detection         (parallel, both must succeed)
//	RunStage4  Recommendation                                (sequential)
//	RunStage5  Business Summary ‖ Detailed Report            (parallel, both must succeed)
//	RunStage6  Risk Delta                                    (sequential, RE_CHECK only, NON-CRITICAL)
//
// The Executor is a thin orchestrator: per stage it projects PipelineState
// into a read-only model.AgentInput snapshot (buildInput), runs the stage's
// agent(s) — parallel stages via the stdlib parallel() errgroup-equivalent —
// and writes each typed result back into its OWN disjoint PipelineState field
// (assign). It does NOT call the Result Aggregator: the Pipeline Orchestrator
// (LIC-TASK-036) runs aggregator.Aggregate between RunStage3 and RunStage4
// and assigns Output.MergedRiskAnalysis→state.MergedRiskAnalysis before
// calling RunStage4 (code-architect D2; tasks.json LIC-TASK-036 acceptance;
// aggregator/CLAUDE.md FN-1).
//
// Hermetic: stdlib + internal/domain/{model,port} only. Telemetry is inverted
// behind the StageMetrics / Tracer seams (zero-dependency noop defaults);
// concrete adapters are wired in LIC-TASK-047 (the base / aggregator seam
// precedent). The parallel() helper is a deliberate stdlib-only
// errgroup.WithContext equivalent forced by the offline-build constraint —
// see parallel.go and the CLAUDE.md "errgroup SSOT reconciliation". Design
// adjudicated by subagent code-architect (decisions D1..D8, binding
// constraints B-1..B-5 — see CLAUDE.md).
package stages

import (
	"context"
	"errors"
	"fmt"
	"time"

	"contractpro/legal-intelligence-core/internal/domain/model"
	"contractpro/legal-intelligence-core/internal/domain/port"
)

// canonicalStage is the model.AgentID → model.Stage bijection used SOLELY for
// *model.DomainError.Stage (the lic.events.status-changed SSOT). It is the
// base.canonicalStage N3 precedent. NOT the local Stage type — the two stage
// enums have disjoint SSOTs (code-architect D4/D5; see stage.go godoc).
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

// Deps bundles the injectable telemetry collaborators. A nil field degrades
// to its zero-dependency default (noop seams) so the common production path
// passes only the two LIC-TASK-047 adapters and tests override exactly what
// they need (the base.Deps.withDefaults pattern).
type Deps struct {
	Metrics StageMetrics
	Tracer  Tracer

	// now is injected only by tests for deterministic stage duration.
	// Production leaves it nil → time.Now.
	now func() time.Time
}

func (d Deps) withDefaults() Deps {
	if d.Metrics == nil {
		d.Metrics = noopStageMetrics{}
	}
	if d.Tracer == nil {
		d.Tracer = noopTracer{}
	}
	if d.now == nil {
		d.now = time.Now
	}
	return d
}

// Executor runs the six pipeline stages. Immutable after NewExecutor; one
// instance is shared across concurrent jobs (the agents it holds are
// themselves immutable/concurrent-safe — base/CLAUDE.md).
type Executor struct {
	agents  map[model.AgentID]port.Agent
	metrics StageMetrics
	tracer  Tracer
	now     func() time.Time
}

// NewExecutor validates the wiring and assembles the Executor. It fails fast
// (NewTypeName per feedback_constructors.md; the base.NewBaseAgent /
// NewAggregator precedent) so a LIC-TASK-047 wiring defect — a missing agent,
// a nil agent, or an agent registered under the wrong AgentID — is a startup
// error, not a first-job nil-map-lookup panic or a mislabeled production run
// (the base.canonicalStage N3 cross-check class).
func NewExecutor(agents map[model.AgentID]port.Agent, deps Deps) (*Executor, error) {
	if len(agents) == 0 {
		return nil, errors.New("stages: agents map must not be empty")
	}
	for _, id := range model.AllAgentIDs() {
		ag, ok := agents[id]
		if !ok {
			return nil, fmt.Errorf("stages: missing agent %q (all 9 agents must be registered)", id)
		}
		if ag == nil {
			return nil, fmt.Errorf("stages: agent %q is nil", id)
		}
		if ag.ID() != id {
			return nil, fmt.Errorf("stages: agent registered under key %q reports ID %q (key/ID mismatch)", id, ag.ID())
		}
	}
	if len(agents) != len(model.AllAgentIDs()) {
		return nil, fmt.Errorf("stages: agents map has %d entries, expected exactly %d", len(agents), len(model.AllAgentIDs()))
	}

	d := deps.withDefaults()
	// Defensive copy: the Executor owns its registry; a caller mutating the
	// passed map post-construction must not affect a running pipeline.
	reg := make(map[model.AgentID]port.Agent, len(agents))
	for k, v := range agents {
		reg[k] = v
	}
	return &Executor{
		agents:  reg,
		metrics: d.Metrics,
		tracer:  d.Tracer,
		now:     d.now,
	}, nil
}

// buildInput projects the current PipelineState into a read-only
// model.AgentInput snapshot (code-architect D3). Called ONCE at the top of
// each RunStageN, BEFORE launching any goroutine: the snapshot is an
// immutable value shared read-only across the parallel goroutines, each of
// which writes only its OWN disjoint state.* field. It is a SHALLOW snapshot
// — agents treat AgentInput strictly read-only (the base immutability
// invariant; every Spec is stateless; aggregator D2/D5 produces distinct
// allocations nobody mutates), so a deep copy would be dead work that falsely
// implies mutation. ParentVersionID/ParentRiskAnalysis are copied
// unconditionally (riskdelta D3/FN-4 — Agent 9's hard-required base_version_id
// has no other source); the RunStage6 gate (not buildInput) decides whether
// Agent 9 runs. Note the field-name asymmetry: PipelineState.InputArtifacts →
// AgentInput.Artifacts.
func buildInput(state *model.PipelineState) model.AgentInput {
	return model.AgentInput{
		CorrelationID:   state.CorrelationID,
		JobID:           state.JobID,
		DocumentID:      state.DocumentID,
		VersionID:       state.VersionID,
		OrganizationID:  state.OrganizationID,
		CreatedByUserID: state.CreatedByUserID,

		Artifacts: state.InputArtifacts,

		Classification:      state.Classification,
		KeyParameters:       state.KeyParameters,
		PartyConsistency:    state.PartyConsistency,
		MandatoryConditions: state.MandatoryConditions,
		RiskAnalysis:        state.RiskAnalysis,
		MergedRiskAnalysis:  state.MergedRiskAnalysis,
		Recommendations:     state.Recommendations,

		ParentRiskAnalysis: state.ParentRiskAnalysis,
		ParentVersionID:    state.ParentVersionID,
	}
}

// assign narrows the type-erased port.AgentResult to the agent's concrete
// type (port/agents.go:45-49) keyed by AgentID and writes it into the
// matching PipelineState field (code-architect D4 — the per-AgentID dispatch
// table port/agents.go:30 mandates instead of a centralised switch in the
// caller). AgentRecommendation's result is the VALUE type
// model.Recommendations (a slice), not a pointer — the lone asymmetry. A type
// mismatch is structurally impossible if the agent's Decode is correct (each
// is pinned by its own package test); it is guarded defensively as a LIC
// build defect → INTERNAL_ERROR carrying the agent's canonical model.Stage
// (NOT the local Stage type) and the agent_id attribute (the base MF-2/N1
// "build defect, never swallow" precedent).
func assign(state *model.PipelineState, id model.AgentID, res port.AgentResult) error {
	switch id {
	case model.AgentTypeClassifier:
		v, ok := res.(*model.ClassificationResult)
		if !ok {
			return resultMismatch(id, res)
		}
		state.Classification = v
	case model.AgentKeyParams:
		v, ok := res.(*model.KeyParameters)
		if !ok {
			return resultMismatch(id, res)
		}
		state.KeyParameters = v
	case model.AgentPartyConsistency:
		v, ok := res.(*model.PartyConsistencyFindings)
		if !ok {
			return resultMismatch(id, res)
		}
		state.PartyConsistency = v
	case model.AgentMandatoryConditions:
		v, ok := res.(*model.MandatoryConditionsReport)
		if !ok {
			return resultMismatch(id, res)
		}
		state.MandatoryConditions = v
	case model.AgentRiskDetection:
		v, ok := res.(*model.RiskAnalysis)
		if !ok {
			return resultMismatch(id, res)
		}
		state.RiskAnalysis = v
	case model.AgentRecommendation:
		v, ok := res.(model.Recommendations) // VALUE type — the lone asymmetry
		if !ok {
			return resultMismatch(id, res)
		}
		state.Recommendations = v
	case model.AgentSummary:
		v, ok := res.(*model.Summary)
		if !ok {
			return resultMismatch(id, res)
		}
		state.Summary = v
	case model.AgentDetailedReport:
		v, ok := res.(*model.DetailedReport)
		if !ok {
			return resultMismatch(id, res)
		}
		state.DetailedReport = v
	case model.AgentRiskDelta:
		v, ok := res.(*model.RiskDelta)
		if !ok {
			return resultMismatch(id, res)
		}
		state.RiskDelta = v
	default:
		return resultMismatch(id, res)
	}
	return nil
}

// resultMismatch builds the INTERNAL_ERROR for an agent whose result type (or
// AgentID) does not match the dispatch table. canonicalStage[id] is the
// offending agent's model.Stage; an unknown id yields the empty model.Stage,
// which DomainError.Error() renders as STAGE_UNSPECIFIED (acceptable — this
// path is unreachable when NewExecutor's validation held).
func resultMismatch(id model.AgentID, res port.AgentResult) error {
	return model.NewDomainError(model.ErrCodeInternal, canonicalStage[id]).
		WithCause(fmt.Errorf("stages: agent %s returned unexpected result type %T", id, res)).
		WithAttribute("agent_id", id.String())
}

// runAgent runs one agent and, on success, assigns its typed result into the
// agent's OWN disjoint PipelineState field. The returned error is the agent's
// verbatim *model.DomainError (or assign's INTERNAL_ERROR) — propagated
// unwrapped so the Orchestrator's errors.As(*model.DomainError) survives,
// including through the parallel() join (code-architect D4 /
// additional-finding-3). The agent is guaranteed present (NewExecutor pins
// the full 9-agent registry).
func (e *Executor) runAgent(ctx context.Context, state *model.PipelineState, in model.AgentInput, id model.AgentID) error {
	res, err := e.agents[id].Run(ctx, in)
	if err != nil {
		return err
	}
	return assign(state, id, res)
}

// runParallel opens the stage span + duration clock, builds the per-stage
// input snapshot, and runs the stage's agents concurrently via parallel()
// (errgroup.WithContext semantics: first error wins, siblings cancelled). All
// listed agents are must-succeed (Stages 1/3/5).
func (e *Executor) runParallel(ctx context.Context, state *model.PipelineState, stage Stage, ids ...model.AgentID) (err error) {
	sctx, span := e.tracer.StartStage(ctx, stage.String())
	started := e.now()
	defer func() {
		e.metrics.StageDuration(stage.String(), e.now().Sub(started).Seconds())
		span.Finish(err)
	}()

	in := buildInput(state)
	fns := make([]func(context.Context) error, 0, len(ids))
	for _, id := range ids {
		id := id
		fns = append(fns, func(c context.Context) error {
			return e.runAgent(c, state, in, id)
		})
	}
	err = parallel(sctx, fns...)
	return
}

// runSequentialCritical opens the stage span + duration clock and runs a
// single must-succeed agent (Stage 4). Any agent error propagates verbatim
// (pipeline-fatal).
func (e *Executor) runSequentialCritical(ctx context.Context, state *model.PipelineState, stage Stage, id model.AgentID) (err error) {
	sctx, span := e.tracer.StartStage(ctx, stage.String())
	started := e.now()
	defer func() {
		e.metrics.StageDuration(stage.String(), e.now().Sub(started).Seconds())
		span.Finish(err)
	}()

	in := buildInput(state)
	err = e.runAgent(sctx, state, in, id)
	return
}

// runNonCritical runs a single NON-CRITICAL agent (Agent 3 in Stage 2 /
// Agent 9 in Stage 6). Degradation is gated on the per-agent AGENT_TIMEOUT
// ONLY (code-architect D6, the decisive reversal): error-handling.md:304 and
// ai-agents-pipeline.md:1665 gate the non-critical skip on "timed out", and
// the schemavalidator 1-shot repair inside base.BaseAgent already absorbs
// transient invalid output — a post-repair AGENT_OUTPUT_INVALID (or any
// non-timeout error) is a genuine retryable failure that MUST fail the
// pipeline (ai-agents-pipeline.md:1673 — for contract analytics a low-quality
// result is worse than a retryable error; error-handling.md:311 "degradation
// does not fill with fake data"). On a timeout the agent's result field stays
// nil (skip), a span Degraded event records the un-lossy truth (there is no
// closed-set model.Warnings code for an agent-skip in v1 — the
// RE_CHECK_PARENT_ANALYSIS_MISSING warning + outbound risk_delta=null are the
// Aggregator/Orchestrator's job per aggregator D4 / riskdelta FN-2, NOT this
// task), and the pipeline continues (err==nil).
func (e *Executor) runNonCritical(ctx context.Context, state *model.PipelineState, stage Stage, id model.AgentID) (err error) {
	sctx, span := e.tracer.StartStage(ctx, stage.String())
	started := e.now()
	defer func() {
		e.metrics.StageDuration(stage.String(), e.now().Sub(started).Seconds())
		span.Finish(err)
	}()

	in := buildInput(state)
	if runErr := e.runAgent(sctx, state, in, id); runErr != nil {
		if isAgentTimeout(runErr) {
			span.Degraded(id.String(), runErr.Error())
			return nil
		}
		err = runErr
	}
	return
}

// isAgentTimeout reports whether err is (or wraps) a *model.DomainError with
// code AGENT_TIMEOUT — the code base.BaseAgent stamps when the per-agent
// context deadline fires (base/seams.go OutcomeTimeout; base S4/H2). The
// timeout-vs-fatal discrimination mirrors base's own decisive S4 check; it is
// a typed code comparison, never a string match.
func isAgentTimeout(err error) bool {
	if de, ok := model.AsDomainError(err); ok {
		return de.Code == model.ErrCodeAgentTimeout
	}
	return false
}

// RunStage1 runs Type Classifier ‖ Key Parameters Extractor in parallel; both
// must succeed (Critical Agent 1 / Tier-2 Agent 2 — a failure fails the
// pipeline).
func (e *Executor) RunStage1(ctx context.Context, state *model.PipelineState) error {
	return e.runParallel(ctx, state, Stage1, model.AgentTypeClassifier, model.AgentKeyParams)
}

// RunStage2 runs Party Data Consistency sequentially. Agent 3 is NON-CRITICAL:
// a per-agent timeout is a graceful skip (warning event + continue); any
// other failure is pipeline-fatal (code-architect D6).
func (e *Executor) RunStage2(ctx context.Context, state *model.PipelineState) error {
	return e.runNonCritical(ctx, state, Stage2, model.AgentPartyConsistency)
}

// RunStage3 runs Mandatory Conditions ‖ Risk Detection in parallel; both must
// succeed (Tier-2 Agent 4 / Critical Agent 5).
func (e *Executor) RunStage3(ctx context.Context, state *model.PipelineState) error {
	return e.runParallel(ctx, state, Stage3, model.AgentMandatoryConditions, model.AgentRiskDetection)
}

// RunStage4 runs Recommendation sequentially (Tier-2 Agent 6 — must succeed).
// Agent 6 reads state.MergedRiskAnalysis, which the Orchestrator (036)
// populates from aggregator.Aggregate AFTER RunStage3 and BEFORE RunStage4
// (code-architect D2 — this Executor never calls the aggregator). If 036
// violates that contract Agent 6 will (correctly) fail with INTERNAL_ERROR —
// its pinned pipeline-ordering breach, not a Stage Executor defect.
func (e *Executor) RunStage4(ctx context.Context, state *model.PipelineState) error {
	return e.runSequentialCritical(ctx, state, Stage4, model.AgentRecommendation)
}

// RunStage5 runs Business Summary ‖ Detailed Report in parallel; both must
// succeed (Tier-2 Agent 7 / Critical Agent 8).
func (e *Executor) RunStage5(ctx context.Context, state *model.PipelineState) error {
	return e.runParallel(ctx, state, Stage5, model.AgentSummary, model.AgentDetailedReport)
}

// RunStage6 runs Risk Delta sequentially, ONLY when the §8.7 RE_CHECK gate
// holds: state.Mode==RE_CHECK AND a parent RISK_ANALYSIS was retrieved
// (state.ParentRiskAnalysis != nil). When gated out it returns nil
// immediately and emits NOTHING — no span, no stage-duration sample — so the
// {stage=s6.risk_delta} series is not polluted with a meaningless
// zero-duration on every INITIAL run (code-architect D7; observability.md
// §4.2 marks s6 "(опц.)"). When it runs, Agent 9 is NON-CRITICAL: a per-agent
// timeout is a graceful skip; any other failure is pipeline-fatal (the same
// D6 discrimination). The RE_CHECK_PARENT_ANALYSIS_MISSING warning and
// outbound risk_delta=null are owned by the Aggregator/Orchestrator
// (LIC-TASK-035 D4 / riskdelta FN-2), NOT this task.
func (e *Executor) RunStage6(ctx context.Context, state *model.PipelineState) error {
	if state.Mode != model.PipelineModeReCheck || state.ParentRiskAnalysis == nil {
		return nil // gated out: no Agent 9 call, no span, no metric.
	}
	return e.runNonCritical(ctx, state, Stage6, model.AgentRiskDelta)
}
