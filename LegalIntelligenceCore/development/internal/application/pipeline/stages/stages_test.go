package stages

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"contractpro/legal-intelligence-core/internal/domain/model"
	"contractpro/legal-intelligence-core/internal/domain/port"
)

// --- fakes -----------------------------------------------------------------

type fakeAgent struct {
	id  model.AgentID
	run func(ctx context.Context, in model.AgentInput) (port.AgentResult, error)
}

func (f fakeAgent) ID() model.AgentID { return f.id }
func (f fakeAgent) Run(ctx context.Context, in model.AgentInput) (port.AgentResult, error) {
	return f.run(ctx, in)
}

var _ port.Agent = fakeAgent{}

// defaultResult returns a non-nil concrete result of the type the dispatch
// table (assign) expects for id — exactly port/agents.go:45-49.
func defaultResult(id model.AgentID) port.AgentResult {
	switch id {
	case model.AgentTypeClassifier:
		return &model.ClassificationResult{}
	case model.AgentKeyParams:
		return &model.KeyParameters{}
	case model.AgentPartyConsistency:
		return &model.PartyConsistencyFindings{}
	case model.AgentMandatoryConditions:
		return &model.MandatoryConditionsReport{}
	case model.AgentRiskDetection:
		return &model.RiskAnalysis{}
	case model.AgentRecommendation:
		return model.Recommendations{} // VALUE type
	case model.AgentSummary:
		return &model.Summary{}
	case model.AgentDetailedReport:
		return &model.DetailedReport{}
	case model.AgentRiskDelta:
		return &model.RiskDelta{}
	default:
		return nil
	}
}

func okAgent(id model.AgentID) fakeAgent {
	return fakeAgent{id: id, run: func(context.Context, model.AgentInput) (port.AgentResult, error) {
		return defaultResult(id), nil
	}}
}

// fullRegistry builds a registry of all 9 succeeding agents; overrides
// replace specific agents.
func fullRegistry(overrides ...fakeAgent) map[model.AgentID]port.Agent {
	reg := make(map[model.AgentID]port.Agent, 9)
	for _, id := range model.AllAgentIDs() {
		reg[id] = okAgent(id)
	}
	for _, o := range overrides {
		reg[o.id] = o
	}
	return reg
}

type recMetrics struct {
	mu  sync.Mutex
	obs map[string]float64 // stage suffix -> last observed seconds
	n   map[string]int     // stage suffix -> count
}

func newRecMetrics() *recMetrics {
	return &recMetrics{obs: map[string]float64{}, n: map[string]int{}}
}
func (m *recMetrics) StageDuration(stage string, s float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.obs[stage] = s
	m.n[stage]++
}
func (m *recMetrics) count(stage string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.n[stage]
}

type recTracer struct {
	mu       sync.Mutex
	started  []string
	degraded map[string]string // agentID -> reason
}

func newRecTracer() *recTracer {
	return &recTracer{degraded: map[string]string{}}
}
func (t *recTracer) StartStage(ctx context.Context, name string) (context.Context, StageSpan) {
	t.mu.Lock()
	t.started = append(t.started, name)
	t.mu.Unlock()
	return ctx, &recSpan{t: t}
}
func (t *recTracer) startedCount() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.started)
}

type recSpan struct{ t *recTracer }

func (s *recSpan) Degraded(agentID, reason string) {
	s.t.mu.Lock()
	defer s.t.mu.Unlock()
	s.t.degraded[agentID] = reason
}
func (s *recSpan) Finish(error) {}

func testDeps() (Deps, *recMetrics, *recTracer) {
	m, tr := newRecMetrics(), newRecTracer()
	return Deps{Metrics: m, Tracer: tr}, m, tr
}

func newState() *model.PipelineState {
	return model.NewPipelineState("corr", "job", "doc", "ver", "org")
}

// --- NewExecutor -----------------------------------------------------------

func TestNewExecutor_OK(t *testing.T) {
	deps, _, _ := testDeps()
	if _, err := NewExecutor(fullRegistry(), deps); err != nil {
		t.Fatalf("NewExecutor: %v", err)
	}
}

func TestNewExecutor_FailFast(t *testing.T) {
	missing := fullRegistry()
	delete(missing, model.AgentSummary)

	nilAgent := fullRegistry()
	nilAgent[model.AgentRiskDelta] = nil

	wrongKey := fullRegistry()
	wrongKey[model.AgentSummary] = okAgent(model.AgentRiskDelta) // ID != key

	extra := fullRegistry()
	extra["AGENT_BOGUS"] = okAgent(model.AgentSummary)

	cases := []struct {
		name string
		reg  map[model.AgentID]port.Agent
	}{
		{"empty", map[model.AgentID]port.Agent{}},
		{"missing agent", missing},
		{"nil agent", nilAgent},
		{"key/ID mismatch", wrongKey},
		{"extra entry", extra},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := NewExecutor(c.reg, Deps{}); err == nil {
				t.Fatalf("want fail-fast error for %s", c.name)
			}
		})
	}
}

func TestNewExecutor_DefensiveRegistryCopy(t *testing.T) {
	reg := fullRegistry()
	deps, _, _ := testDeps()
	e, err := NewExecutor(reg, deps)
	if err != nil {
		t.Fatalf("NewExecutor: %v", err)
	}
	// Mutating the caller's map after construction must not affect the run.
	delete(reg, model.AgentTypeClassifier)
	if err := e.RunStage1(context.Background(), newState()); err != nil {
		t.Fatalf("post-mutation RunStage1 must still work: %v", err)
	}
}

// --- Stage 1 / 3 / 5 (parallel, both must succeed) -------------------------

func TestRunStage1_ParallelBothSucceed(t *testing.T) {
	deps, m, tr := testDeps()
	e, _ := NewExecutor(fullRegistry(), deps)
	st := newState()
	if err := e.RunStage1(context.Background(), st); err != nil {
		t.Fatalf("RunStage1: %v", err)
	}
	if st.Classification == nil || st.KeyParameters == nil {
		t.Fatalf("both results must be assigned: cls=%v kp=%v", st.Classification, st.KeyParameters)
	}
	if m.count(Stage1.String()) != 1 {
		t.Fatalf("stage-duration metric must fire once for %s", Stage1)
	}
	if tr.startedCount() != 1 || tr.started[0] != Stage1.String() {
		t.Fatalf("span must open with suffix %q, got %v", Stage1, tr.started)
	}
}

// Шаг 2 (test_steps): Stage 1 parallelises 2 agents — elapsed < sum of the
// two agent durations.
func TestRunStage1_IsParallel(t *testing.T) {
	const d = 60 * time.Millisecond
	slow := func(id model.AgentID) fakeAgent {
		return fakeAgent{id: id, run: func(ctx context.Context, _ model.AgentInput) (port.AgentResult, error) {
			select {
			case <-time.After(d):
				return defaultResult(id), nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}}
	}
	deps, _, _ := testDeps()
	e, _ := NewExecutor(fullRegistry(slow(model.AgentTypeClassifier), slow(model.AgentKeyParams)), deps)

	start := time.Now()
	if err := e.RunStage1(context.Background(), newState()); err != nil {
		t.Fatalf("RunStage1: %v", err)
	}
	elapsed := time.Since(start)
	if elapsed >= 2*d {
		t.Fatalf("parallel stage took %v, must be < 2*%v (sequential would)", elapsed, d)
	}
}

// Шаг 3 (test_steps): a fail in one agent cancels the sibling.
func TestRunStage1_FailCancelsSibling(t *testing.T) {
	boom := model.NewDomainError(model.ErrCodeAgentOutputInvalid, model.StageAgentTypeClassifier)
	var siblingCancelled = make(chan struct{}, 1)

	failing := fakeAgent{id: model.AgentTypeClassifier, run: func(context.Context, model.AgentInput) (port.AgentResult, error) {
		return nil, boom
	}}
	sibling := fakeAgent{id: model.AgentKeyParams, run: func(ctx context.Context, _ model.AgentInput) (port.AgentResult, error) {
		<-ctx.Done() // only unblocks when the failing agent cancels the group
		siblingCancelled <- struct{}{}
		return nil, ctx.Err()
	}}
	deps, _, _ := testDeps()
	e, _ := NewExecutor(fullRegistry(failing, sibling), deps)

	err := e.RunStage1(context.Background(), newState())
	if err == nil {
		t.Fatal("RunStage1 must propagate the failure")
	}
	var de *model.DomainError
	if !errors.As(err, &de) || de != boom {
		t.Fatalf("must propagate the *model.DomainError verbatim, got %v", err)
	}
	select {
	case <-siblingCancelled:
	case <-time.After(time.Second):
		t.Fatal("sibling was not cancelled by the failing agent")
	}
}

func TestRunStage3_And5_ParallelBothSucceed(t *testing.T) {
	deps, _, _ := testDeps()
	e, _ := NewExecutor(fullRegistry(), deps)
	st := newState()
	st.MergedRiskAnalysis = &model.RiskAnalysis{} // 036 would have set this
	if err := e.RunStage3(context.Background(), st); err != nil {
		t.Fatalf("RunStage3: %v", err)
	}
	if st.MandatoryConditions == nil || st.RiskAnalysis == nil {
		t.Fatal("Stage 3 must assign MandatoryConditions + RiskAnalysis")
	}
	if err := e.RunStage5(context.Background(), st); err != nil {
		t.Fatalf("RunStage5: %v", err)
	}
	if st.Summary == nil || st.DetailedReport == nil {
		t.Fatal("Stage 5 must assign Summary + DetailedReport")
	}
}

// --- Stage 4 (sequential, must succeed) ------------------------------------

func TestRunStage4_Recommendation(t *testing.T) {
	deps, m, tr := testDeps()
	var sawMerged bool
	rec := fakeAgent{id: model.AgentRecommendation, run: func(_ context.Context, in model.AgentInput) (port.AgentResult, error) {
		sawMerged = in.MergedRiskAnalysis != nil
		return model.Recommendations{{}}, nil
	}}
	e, _ := NewExecutor(fullRegistry(rec), deps)
	st := newState()
	st.MergedRiskAnalysis = &model.RiskAnalysis{} // populated by Orchestrator 036 before Stage 4

	if err := e.RunStage4(context.Background(), st); err != nil {
		t.Fatalf("RunStage4: %v", err)
	}
	if !sawMerged {
		t.Fatal("Agent 6 must observe state.MergedRiskAnalysis via buildInput")
	}
	if len(st.Recommendations) != 1 {
		t.Fatalf("value-type Recommendations must be assigned, got %v", st.Recommendations)
	}
	if m.count(Stage4.String()) != 1 || tr.started[0] != Stage4.String() {
		t.Fatal("Stage 4 metric/span must fire with s4.recommendation")
	}
}

// --- Stage 2 (non-critical) ------------------------------------------------

func TestRunStage2_Success(t *testing.T) {
	deps, _, _ := testDeps()
	e, _ := NewExecutor(fullRegistry(), deps)
	st := newState()
	if err := e.RunStage2(context.Background(), st); err != nil {
		t.Fatalf("RunStage2: %v", err)
	}
	if st.PartyConsistency == nil {
		t.Fatal("Agent 3 success must assign PartyConsistency")
	}
}

func TestRunStage2_Timeout_Degrades(t *testing.T) {
	deps, _, tr := testDeps()
	to := fakeAgent{id: model.AgentPartyConsistency, run: func(context.Context, model.AgentInput) (port.AgentResult, error) {
		return nil, model.NewDomainError(model.ErrCodeAgentTimeout, model.StageAgentPartyConsistency)
	}}
	e, _ := NewExecutor(fullRegistry(to), deps)
	st := newState()

	if err := e.RunStage2(context.Background(), st); err != nil {
		t.Fatalf("timeout on non-critical Agent 3 must NOT fail the pipeline, got %v", err)
	}
	if st.PartyConsistency != nil {
		t.Fatal("degraded Agent 3 must leave PartyConsistency nil (skip)")
	}
	if _, ok := tr.degraded[model.AgentPartyConsistency.String()]; !ok {
		t.Fatal("a span Degraded event must record the skip")
	}
}

func TestRunStage2_NonTimeout_Propagates(t *testing.T) {
	cases := []struct {
		name string
		err  error
	}{
		{"output invalid (post-repair)", model.NewDomainError(model.ErrCodeAgentOutputInvalid, model.StageAgentPartyConsistency)},
		{"internal error", model.NewDomainError(model.ErrCodeInternal, model.StageAgentPartyConsistency)},
		{"provider error", model.NewDomainError(model.ErrCodeLLMAllProvidersFailed, model.StageAgentPartyConsistency)},
		{"plain non-domain error", errors.New("transport blew up")},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			deps, _, _ := testDeps()
			bad := fakeAgent{id: model.AgentPartyConsistency, run: func(context.Context, model.AgentInput) (port.AgentResult, error) {
				return nil, c.err
			}}
			e, _ := NewExecutor(fullRegistry(bad), deps)
			if err := e.RunStage2(context.Background(), newState()); err == nil {
				t.Fatalf("non-timeout Agent 3 failure (%s) must be pipeline-fatal", c.name)
			}
		})
	}
}

// --- Stage 6 (RE_CHECK only, non-critical) ---------------------------------

func TestRunStage6_GatedOut_EmitsNothing(t *testing.T) {
	cases := []struct {
		name  string
		setup func(*model.PipelineState)
	}{
		{"INITIAL mode", func(s *model.PipelineState) { s.Mode = model.PipelineModeInitial }},
		{"RE_CHECK but no parent analysis", func(s *model.PipelineState) {
			s.Mode = model.PipelineModeReCheck
			s.ParentRiskAnalysis = nil
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			deps, m, tr := testDeps()
			// An Agent 9 that would fail the test if ever invoked.
			trip := fakeAgent{id: model.AgentRiskDelta, run: func(context.Context, model.AgentInput) (port.AgentResult, error) {
				t.Fatal("Agent 9 must NOT run when gated out")
				return nil, nil
			}}
			e, _ := NewExecutor(fullRegistry(trip), deps)
			st := newState()
			c.setup(st)
			if err := e.RunStage6(context.Background(), st); err != nil {
				t.Fatalf("gated-out RunStage6 must return nil, got %v", err)
			}
			if tr.startedCount() != 0 {
				t.Fatal("gated-out RunStage6 must open NO span")
			}
			if m.count(Stage6.String()) != 0 {
				t.Fatal("gated-out RunStage6 must emit NO stage-duration sample")
			}
		})
	}
}

func TestRunStage6_RunsWhenGateMet(t *testing.T) {
	deps, m, _ := testDeps()
	e, _ := NewExecutor(fullRegistry(), deps)
	st := newState()
	st.Mode = model.PipelineModeReCheck
	st.ParentRiskAnalysis = &model.RiskAnalysis{}
	st.MergedRiskAnalysis = &model.RiskAnalysis{}
	pv := "parent-ver"
	st.ParentVersionID = &pv

	if err := e.RunStage6(context.Background(), st); err != nil {
		t.Fatalf("RunStage6: %v", err)
	}
	if st.RiskDelta == nil {
		t.Fatal("Agent 9 success must assign RiskDelta")
	}
	if m.count(Stage6.String()) != 1 {
		t.Fatal("a met-gate RunStage6 must emit the stage-duration sample")
	}
}

func TestRunStage6_Timeout_Degrades(t *testing.T) {
	deps, _, tr := testDeps()
	to := fakeAgent{id: model.AgentRiskDelta, run: func(context.Context, model.AgentInput) (port.AgentResult, error) {
		return nil, model.NewDomainError(model.ErrCodeAgentTimeout, model.StageAgentRiskDelta)
	}}
	e, _ := NewExecutor(fullRegistry(to), deps)
	st := newState()
	st.Mode = model.PipelineModeReCheck
	st.ParentRiskAnalysis = &model.RiskAnalysis{}

	if err := e.RunStage6(context.Background(), st); err != nil {
		t.Fatalf("timeout on non-critical Agent 9 must NOT fail the pipeline, got %v", err)
	}
	if st.RiskDelta != nil {
		t.Fatal("degraded Agent 9 must leave RiskDelta nil")
	}
	if _, ok := tr.degraded[model.AgentRiskDelta.String()]; !ok {
		t.Fatal("a span Degraded event must record the Agent 9 skip")
	}
}

func TestRunStage6_NonTimeout_Propagates(t *testing.T) {
	deps, _, _ := testDeps()
	bad := fakeAgent{id: model.AgentRiskDelta, run: func(context.Context, model.AgentInput) (port.AgentResult, error) {
		return nil, model.NewDomainError(model.ErrCodeInternal, model.StageAgentRiskDelta)
	}}
	e, _ := NewExecutor(fullRegistry(bad), deps)
	st := newState()
	st.Mode = model.PipelineModeReCheck
	st.ParentRiskAnalysis = &model.RiskAnalysis{}
	if err := e.RunStage6(context.Background(), st); err == nil {
		t.Fatal("non-timeout Agent 9 failure must be pipeline-fatal")
	}
}

// --- dispatch / projection -------------------------------------------------

func TestAssign_TypeMismatch_InternalError(t *testing.T) {
	deps, _, _ := testDeps()
	wrong := fakeAgent{id: model.AgentSummary, run: func(context.Context, model.AgentInput) (port.AgentResult, error) {
		return &model.RiskAnalysis{}, nil // wrong concrete type for AGENT_SUMMARY
	}}
	e, _ := NewExecutor(fullRegistry(wrong), deps)
	st := newState()
	st.MergedRiskAnalysis = &model.RiskAnalysis{}

	err := e.RunStage5(context.Background(), st)
	if err == nil {
		t.Fatal("a result/type mismatch must be a build-defect error")
	}
	de, ok := model.AsDomainError(err)
	if !ok {
		t.Fatalf("want *model.DomainError, got %T", err)
	}
	if de.Code != model.ErrCodeInternal {
		t.Fatalf("want INTERNAL_ERROR, got %s", de.Code)
	}
	if de.Stage != model.StageAgentSummary {
		t.Fatalf("DomainError.Stage must be the agent's canonical model.Stage, got %s", de.Stage)
	}
	if de.Attributes["agent_id"] != model.AgentSummary.String() {
		t.Fatalf("agent_id attribute must be set, got %v", de.Attributes)
	}
}

func TestBuildInput_CopiesAllFields(t *testing.T) {
	st := newState()
	st.Mode = model.PipelineModeReCheck
	st.InputArtifacts = model.InputArtifactsCompact{model.ArtifactExtractedText: []byte(`"x"`)}
	st.Classification = &model.ClassificationResult{}
	st.KeyParameters = &model.KeyParameters{}
	st.PartyConsistency = &model.PartyConsistencyFindings{}
	st.MandatoryConditions = &model.MandatoryConditionsReport{}
	st.RiskAnalysis = &model.RiskAnalysis{}
	st.MergedRiskAnalysis = &model.RiskAnalysis{}
	st.Recommendations = model.Recommendations{{}}
	st.ParentRiskAnalysis = &model.RiskAnalysis{}
	pv := "parent-ver-id"
	st.ParentVersionID = &pv

	in := buildInput(st)

	if in.CorrelationID != st.CorrelationID || in.JobID != st.JobID ||
		in.DocumentID != st.DocumentID || in.VersionID != st.VersionID ||
		in.OrganizationID != st.OrganizationID {
		t.Fatal("correlation IDs not copied")
	}
	if !in.Artifacts.Has(model.ArtifactExtractedText) {
		t.Fatal("InputArtifacts → Artifacts not copied (field-name asymmetry)")
	}
	if in.Classification != st.Classification || in.KeyParameters != st.KeyParameters ||
		in.PartyConsistency != st.PartyConsistency || in.MandatoryConditions != st.MandatoryConditions ||
		in.RiskAnalysis != st.RiskAnalysis || in.MergedRiskAnalysis != st.MergedRiskAnalysis ||
		in.ParentRiskAnalysis != st.ParentRiskAnalysis {
		t.Fatal("upstream result pointers not copied verbatim (shallow snapshot)")
	}
	if in.ParentVersionID != st.ParentVersionID || *in.ParentVersionID != pv {
		t.Fatal("ParentVersionID must be copied unconditionally (riskdelta D3/FN-4)")
	}
	if len(in.Recommendations) != 1 {
		t.Fatal("Recommendations value slice not copied")
	}
}

// --- Stage enum ------------------------------------------------------------

// D5 binding constraint #3: the six strings are pinned byte-exact to the
// observability.md §4.2 span-tree suffixes.
func TestStageStrings_PinnedToObservabilitySpanTree(t *testing.T) {
	want := map[Stage]string{
		Stage1: "s1.parallel",
		Stage2: "s2.party_consistency",
		Stage3: "s3.parallel",
		Stage4: "s4.recommendation",
		Stage5: "s5.parallel",
		Stage6: "s6.risk_delta",
	}
	for s, w := range want {
		if s.String() != w {
			t.Fatalf("Stage %v string drifted: got %q want %q", s, s.String(), w)
		}
		if !s.IsValid() {
			t.Fatalf("Stage %q must be valid", s)
		}
	}
	if got := len(AllStages()); got != 6 {
		t.Fatalf("AllStages must list exactly 6 stages, got %d", got)
	}
	if Stage("bogus").IsValid() {
		t.Fatal("unknown stage must be invalid")
	}
}

// --- concurrency -----------------------------------------------------------

// The pipeline_state.go invariant ("parallel stages mutate disjoint result
// fields via errgroup, synchronisation at stage boundaries") is empirically
// pinned, not assumed (code-architect additional-finding-1).
func TestRunStages_ConcurrentRaceClean(t *testing.T) {
	deps, _, _ := testDeps()
	e, _ := NewExecutor(fullRegistry(), deps)

	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			st := newState()
			st.MergedRiskAnalysis = &model.RiskAnalysis{}
			ctx := context.Background()
			if err := e.RunStage1(ctx, st); err != nil {
				t.Errorf("RunStage1: %v", err)
			}
			if err := e.RunStage3(ctx, st); err != nil {
				t.Errorf("RunStage3: %v", err)
			}
			if err := e.RunStage5(ctx, st); err != nil {
				t.Errorf("RunStage5: %v", err)
			}
			if st.Classification == nil || st.KeyParameters == nil ||
				st.MandatoryConditions == nil || st.RiskAnalysis == nil ||
				st.Summary == nil || st.DetailedReport == nil {
				t.Error("all parallel-stage results must be populated")
			}
		}()
	}
	wg.Wait()
}
