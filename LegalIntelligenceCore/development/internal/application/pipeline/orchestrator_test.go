package pipeline

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"contractpro/legal-intelligence-core/internal/application/aggregator"
	"contractpro/legal-intelligence-core/internal/application/pipeline/stages"
	"contractpro/legal-intelligence-core/internal/domain/model"
	"contractpro/legal-intelligence-core/internal/domain/port"
)

// ============================================================================
// fakes — every seam AND every port
// ============================================================================

// --- port.Agent (the stages_test.go pattern, reused per the build-spec
// "construct a REAL stages.Executor with fake agents" instruction) ---------

type fakeAgent struct {
	id  model.AgentID
	run func(ctx context.Context, in model.AgentInput) (port.AgentResult, error)
}

func (f fakeAgent) ID() model.AgentID { return f.id }
func (f fakeAgent) Run(ctx context.Context, in model.AgentInput) (port.AgentResult, error) {
	return f.run(ctx, in)
}

var _ port.Agent = fakeAgent{}

// defaultResult returns a non-nil concrete result of the type the stages
// dispatch table expects (port/agents.go:45-49). Agent 5 returns a non-nil
// *model.RiskAnalysis so the REAL aggregator.Aggregate succeeds (its only
// error is ErrNilRiskAnalysis).
func defaultResult(id model.AgentID) port.AgentResult {
	switch id {
	case model.AgentTypeClassifier:
		return &model.ClassificationResult{ContractType: "SERVICE", Confidence: 0.99}
	case model.AgentKeyParams:
		return &model.KeyParameters{}
	case model.AgentPartyConsistency:
		return &model.PartyConsistencyFindings{}
	case model.AgentMandatoryConditions:
		return &model.MandatoryConditionsReport{}
	case model.AgentRiskDetection:
		return &model.RiskAnalysis{}
	case model.AgentRecommendation:
		return model.Recommendations{}
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

func newExecutor(t *testing.T, overrides ...fakeAgent) *stages.Executor {
	t.Helper()
	ex, err := stages.NewExecutor(fullRegistry(overrides...), stages.Deps{})
	if err != nil {
		t.Fatalf("stages.NewExecutor: %v", err)
	}
	return ex
}

func newAggregator(t *testing.T) *aggregator.Aggregator {
	t.Helper()
	ag, err := aggregator.NewAggregator(aggregator.Config{
		WeightHigh:               25,
		WeightMedium:             10,
		WeightLow:                3,
		WeightMissingMandatory:   15,
		WeightAmbiguousMandatory: 5,
		LabelLowThreshold:        0.75,
		LabelMediumThreshold:     0.45,
	}, nil)
	if err != nil {
		t.Fatalf("aggregator.NewAggregator: %v", err)
	}
	return ag
}

// --- seam: JobLimiter -------------------------------------------------------

type fakeLimiter struct {
	mu          sync.Mutex
	acquires    int
	releases    int
	acquireErr  error
	acquireWait time.Duration
}

func (l *fakeLimiter) Acquire(ctx context.Context) error {
	l.mu.Lock()
	l.acquires++
	wait, fixedErr := l.acquireWait, l.acquireErr
	l.mu.Unlock()
	if fixedErr != nil {
		return fixedErr
	}
	if wait > 0 {
		select {
		case <-time.After(wait):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

func (l *fakeLimiter) Release() {
	l.mu.Lock()
	l.releases++
	l.mu.Unlock()
}

func (l *fakeLimiter) counts() (int, int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.acquires, l.releases
}

// --- seam: PipelineMetrics --------------------------------------------------

type recMetrics struct {
	mu             sync.Mutex
	started        []string
	finished       [][3]any // mode, outcome, seconds
	outcomes       [][3]string
	concurrentSeen bool // DEFECT-2: must stay false (no ConcurrentJobs method)
}

func newRecMetrics() *recMetrics { return &recMetrics{} }

func (m *recMetrics) PipelineStarted(mode string) {
	m.mu.Lock()
	m.started = append(m.started, mode)
	m.mu.Unlock()
}
func (m *recMetrics) PipelineFinished(mode, outcome string, seconds float64) {
	m.mu.Lock()
	m.finished = append(m.finished, [3]any{mode, outcome, seconds})
	m.mu.Unlock()
}
func (m *recMetrics) PipelineOutcome(mode, outcome, errorCode string) {
	m.mu.Lock()
	m.outcomes = append(m.outcomes, [3]string{mode, outcome, errorCode})
	m.mu.Unlock()
}

// --- seam: Tracer / PipelineSpan -------------------------------------------

type recTracer struct {
	mu       sync.Mutex
	roots    int
	children []string
}

func (t *recTracer) StartPipeline(ctx context.Context, _ PipelineSpanAttrs) (context.Context, PipelineSpan) {
	t.mu.Lock()
	t.roots++
	t.mu.Unlock()
	return ctx, &recSpan{t: t}
}

type recSpan struct{ t *recTracer }

func (s *recSpan) StartChild(ctx context.Context, name string) (context.Context, PipelineSpan) {
	s.t.mu.Lock()
	s.t.children = append(s.t.children, name)
	s.t.mu.Unlock()
	return ctx, s
}
func (s *recSpan) Finish(error) {}

// --- seam: Clock ------------------------------------------------------------

type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func newFakeClock() *fakeClock { return &fakeClock{now: time.Unix(1_700_000_000, 0).UTC()} }
func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}
func (c *fakeClock) Since(t time.Time) time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now.Sub(t)
}

// --- seam: Logger -----------------------------------------------------------

type recLogger struct {
	mu    sync.Mutex
	warns int
	errs  int
}

func (l *recLogger) Warn(context.Context, string, ...any) {
	l.mu.Lock()
	l.warns++
	l.mu.Unlock()
}
func (l *recLogger) Error(context.Context, string, ...any) {
	l.mu.Lock()
	l.errs++
	l.mu.Unlock()
}
func (l *recLogger) warnCount() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.warns
}

// --- seam: VersionMetaCache -------------------------------------------------

type fakeMetaCache struct {
	mu     sync.Mutex
	ret    *string
	err    error
	called int
}

func (c *fakeMetaCache) GetParentVersionID(context.Context, string) (*string, error) {
	c.mu.Lock()
	c.called++
	r, e := c.ret, c.err
	c.mu.Unlock()
	return r, e
}
func (c *fakeMetaCache) callCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.called
}

// --- seam: PauseController --------------------------------------------------

type recPause struct {
	mu     sync.Mutex
	called int
	ret    error
}

func (p *recPause) Pause(_ context.Context, _ *model.PipelineState) error {
	p.mu.Lock()
	p.called++
	r := p.ret
	p.mu.Unlock()
	return r
}

// --- port: ArtifactRequesterPort -------------------------------------------

type recRequester struct {
	mu    sync.Mutex
	calls []reqCall
	errOn string // correlationID suffix to fail on ("" = none)
}

type reqCall struct {
	corr      string
	versionID string
	types     []model.ArtifactType
}

func (r *recRequester) RequestArtifacts(_ context.Context, corr, _, _, versionID, _ string, types []model.ArtifactType) error {
	r.mu.Lock()
	r.calls = append(r.calls, reqCall{corr: corr, versionID: versionID, types: append([]model.ArtifactType(nil), types...)})
	failOn := r.errOn
	r.mu.Unlock()
	if failOn != "" && hasSuffix(corr, failOn) {
		return errors.New("publish failed")
	}
	return nil
}
func (r *recRequester) callsFor(suffix string) []reqCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []reqCall
	for _, c := range r.calls {
		if hasSuffix(c.corr, suffix) {
			out = append(out, c)
		}
	}
	return out
}
func hasSuffix(s, suf string) bool { return len(s) >= len(suf) && s[len(s)-len(suf):] == suf }

// --- port: ArtifactsAwaiterPort --------------------------------------------

type fakeArtAwaiter struct {
	mu        sync.Mutex
	provided  map[string]port.ArtifactsProvided // keyed by corr suffix (":current"/":parent")
	awaitErr  map[string]error                  // keyed by corr suffix
	registers map[string]int
	cancels   map[string]int
	dupOn     string
}

func newArtAwaiter() *fakeArtAwaiter {
	return &fakeArtAwaiter{
		provided:  map[string]port.ArtifactsProvided{},
		awaitErr:  map[string]error{},
		registers: map[string]int{},
		cancels:   map[string]int{},
	}
}

func suffixOf(corr string) string {
	if hasSuffix(corr, ":current") {
		return ":current"
	}
	if hasSuffix(corr, ":parent") {
		return ":parent"
	}
	return corr
}

func (a *fakeArtAwaiter) Register(corr string) (<-chan port.ArtifactsProvided, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	suf := suffixOf(corr)
	a.registers[suf]++
	if a.dupOn != "" && suf == a.dupOn {
		return nil, port.ErrDuplicateRegistration
	}
	ch := make(chan port.ArtifactsProvided, 1)
	return ch, nil
}
func (a *fakeArtAwaiter) Await(ctx context.Context, corr string) (port.ArtifactsProvided, error) {
	a.mu.Lock()
	suf := suffixOf(corr)
	err := a.awaitErr[suf]
	prov, ok := a.provided[suf]
	a.mu.Unlock()
	if err != nil {
		return port.ArtifactsProvided{}, err
	}
	if !ok {
		// Block until ctx cancels — models a never-arriving response.
		<-ctx.Done()
		return port.ArtifactsProvided{}, ctx.Err()
	}
	return prov, nil
}
func (a *fakeArtAwaiter) Cancel(corr string) {
	a.mu.Lock()
	a.cancels[suffixOf(corr)]++
	a.mu.Unlock()
}
func (a *fakeArtAwaiter) registerCount(suf string) int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.registers[suf]
}
func (a *fakeArtAwaiter) cancelCount(suf string) int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.cancels[suf]
}

// goodCurrent is a complete current-version ArtifactsProvided.
func goodCurrent() port.ArtifactsProvided {
	return port.ArtifactsProvided{
		Artifacts: map[model.ArtifactType]json.RawMessage{
			model.ArtifactSemanticTree:       json.RawMessage(`{"t":1}`),
			model.ArtifactExtractedText:      json.RawMessage(`{"x":1}`),
			model.ArtifactDocumentStructure:  json.RawMessage(`{"s":1}`),
			model.ArtifactProcessingWarnings: json.RawMessage(`{"w":1}`),
		},
	}
}

func goodParent() port.ArtifactsProvided {
	return port.ArtifactsProvided{
		Artifacts: map[model.ArtifactType]json.RawMessage{
			model.ArtifactRiskAnalysis: json.RawMessage(`{"risks":[],"prompt_injection_detected":false}`),
		},
	}
}

// --- port: AnalysisArtifactsPublisherPort ----------------------------------

type recAnalysisPub struct {
	mu       sync.Mutex
	payloads []port.LegalAnalysisArtifactsReady
	err      error
}

func (p *recAnalysisPub) Publish(_ context.Context, payload port.LegalAnalysisArtifactsReady) error {
	p.mu.Lock()
	p.payloads = append(p.payloads, payload)
	e := p.err
	p.mu.Unlock()
	return e
}
func (p *recAnalysisPub) last() (port.LegalAnalysisArtifactsReady, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.payloads) == 0 {
		return port.LegalAnalysisArtifactsReady{}, false
	}
	return p.payloads[len(p.payloads)-1], true
}

// --- port: PersistConfirmationAwaiterPort ----------------------------------

type fakePersistAwaiter struct {
	mu        sync.Mutex
	conf      port.PersistConfirmation
	hasConf   bool
	err       error
	registers int
	cancels   int
}

func (p *fakePersistAwaiter) Register(string) (<-chan port.PersistConfirmation, error) {
	p.mu.Lock()
	p.registers++
	p.mu.Unlock()
	return make(chan port.PersistConfirmation, 1), nil
}
func (p *fakePersistAwaiter) Await(ctx context.Context, _ string) (port.PersistConfirmation, error) {
	p.mu.Lock()
	e, hc, c := p.err, p.hasConf, p.conf
	p.mu.Unlock()
	if e != nil {
		return port.PersistConfirmation{}, e
	}
	if !hc {
		<-ctx.Done()
		return port.PersistConfirmation{}, ctx.Err()
	}
	return c, nil
}
func (p *fakePersistAwaiter) Cancel(string) {
	p.mu.Lock()
	p.cancels++
	p.mu.Unlock()
}

func successConf() port.PersistConfirmation {
	return port.NewPersistConfirmationSuccess(&port.LegalAnalysisArtifactsPersisted{JobID: "job"})
}

// --- port: StatusPublisherPort ---------------------------------------------

type recStatusPub struct {
	mu     sync.Mutex
	events []port.LICStatusChangedEvent
	err    error
}

func (p *recStatusPub) PublishStatus(_ context.Context, evt port.LICStatusChangedEvent) error {
	p.mu.Lock()
	p.events = append(p.events, evt)
	e := p.err
	p.mu.Unlock()
	return e
}
func (p *recStatusPub) byStatus(s model.ExternalStatus) []port.LICStatusChangedEvent {
	p.mu.Lock()
	defer p.mu.Unlock()
	var out []port.LICStatusChangedEvent
	for _, e := range p.events {
		if e.Status == s {
			out = append(out, e)
		}
	}
	return out
}

// --- port: UncertaintyPublisherPort ----------------------------------------

type noopUncertainPub struct{}

func (noopUncertainPub) PublishClassificationUncertain(context.Context, port.ClassificationUncertain) error {
	return nil
}

// ============================================================================
// harness
// ============================================================================

type harness struct {
	t         *testing.T
	cfg       Config
	limiter   *fakeLimiter
	metrics   *recMetrics
	tracer    *recTracer
	clock     *fakeClock
	log       *recLogger
	meta      *fakeMetaCache
	pause     *recPause
	requester *recRequester
	artAwait  *fakeArtAwaiter
	analysis  *recAnalysisPub
	persist   *fakePersistAwaiter
	status    *recStatusPub
	exec      *stages.Executor
	agg       *aggregator.Aggregator
}

func newHarness(t *testing.T, agentOverrides ...fakeAgent) *harness {
	h := &harness{
		t: t,
		cfg: Config{
			JobTimeout:              90 * time.Second,
			DMRequestTimeout:        30 * time.Second,
			DMPersistConfirmTimeout: 30 * time.Second,
			ConfidenceThreshold:     0.7,
			MaxIngestedBytes:        1 << 20,
		},
		limiter:   &fakeLimiter{},
		metrics:   newRecMetrics(),
		tracer:    &recTracer{},
		clock:     newFakeClock(),
		log:       &recLogger{},
		meta:      &fakeMetaCache{},
		pause:     &recPause{},
		requester: &recRequester{},
		artAwait:  newArtAwaiter(),
		analysis:  &recAnalysisPub{},
		persist:   &fakePersistAwaiter{},
		status:    &recStatusPub{},
		exec:      newExecutor(t, agentOverrides...),
		agg:       newAggregator(t),
	}
	// Happy defaults: current artifacts present, persist succeeds.
	h.artAwait.provided[":current"] = goodCurrent()
	h.persist.conf = successConf()
	h.persist.hasConf = true
	return h
}

func (h *harness) orch() *Orchestrator {
	o, err := NewOrchestrator(
		h.cfg, h.exec, h.agg,
		h.requester, h.artAwait, h.analysis, h.persist, h.status, noopUncertainPub{},
		Deps{
			JobLimiter:       h.limiter,
			Metrics:          h.metrics,
			Tracer:           h.tracer,
			Clock:            h.clock,
			Logger:           h.log,
			VersionMetaCache: h.meta,
			PauseController:  h.pause,
		},
	)
	if err != nil {
		h.t.Fatalf("NewOrchestrator: %v", err)
	}
	return o
}

func initialTrigger() port.VersionProcessingArtifactsReady {
	return port.VersionProcessingArtifactsReady{
		CorrelationID:  "corr-1",
		JobID:          "job",
		DocumentID:     "doc",
		VersionID:      "ver",
		OrganizationID: "org",
		OriginType:     "UPLOAD",
	}
}

func reCheckTrigger(parent string) port.VersionProcessingArtifactsReady {
	tr := initialTrigger()
	tr.ParentVersionID = &parent
	return tr
}

// ============================================================================
// pins
// ============================================================================

// Pin 1 — happy INITIAL.
func TestRun_HappyInitial(t *testing.T) {
	h := newHarness(t)
	o := h.orch()

	if err := o.Run(context.Background(), initialTrigger()); err != nil {
		t.Fatalf("Run: unexpected error %v", err)
	}

	if got := len(h.requester.callsFor(":current")); got != 1 {
		t.Fatalf("current RequestArtifacts: want 1, got %d", got)
	}
	if got := len(h.requester.callsFor(":parent")); got != 0 {
		t.Fatalf("INITIAL must NOT request parent, got %d calls", got)
	}
	cur := h.requester.callsFor(":current")[0]
	if len(cur.types) != 4 {
		t.Fatalf("current request must carry 4 artifact types, got %d", len(cur.types))
	}
	pay, ok := h.analysis.last()
	if !ok {
		t.Fatal("analysis-ready not published")
	}
	if pay.RiskDelta != nil {
		t.Fatalf("INITIAL payload RiskDelta must be nil, got %+v", pay.RiskDelta)
	}
	if got := len(h.status.byStatus(model.StatusCompleted)); got != 1 {
		t.Fatalf("COMPLETED publishes: want 1, got %d", got)
	}
	if got := len(h.status.byStatus(model.StatusInProgress)); got != 1 {
		t.Fatalf("IN_PROGRESS publishes: want 1, got %d", got)
	}
	// Register precedes request (both registered before any await; the
	// requester ran after Register since Run registers first).
	if h.artAwait.registerCount(":current") != 1 {
		t.Fatalf("current awaiter Register: want 1, got %d", h.artAwait.registerCount(":current"))
	}
	if h.persist.registers != 1 {
		t.Fatalf("persist Register: want 1, got %d", h.persist.registers)
	}
}

// Pin 2 — happy RE_CHECK via trigger.ParentVersionID (DEFECT-1 primary
// source: VersionMetaCache NOT consulted).
func TestRun_HappyReCheck_TriggerPointer(t *testing.T) {
	h := newHarness(t)
	h.artAwait.provided[":parent"] = goodParent()
	o := h.orch()

	if err := o.Run(context.Background(), reCheckTrigger("parent-ver")); err != nil {
		t.Fatalf("Run: %v", err)
	}

	parCalls := h.requester.callsFor(":parent")
	if len(parCalls) != 1 {
		t.Fatalf("RE_CHECK must request parent exactly once, got %d", len(parCalls))
	}
	if parCalls[0].versionID != "parent-ver" {
		t.Fatalf("parent request subject must be parent version_id, got %q", parCalls[0].versionID)
	}
	if len(parCalls[0].types) != 1 || parCalls[0].types[0] != model.ArtifactRiskAnalysis {
		t.Fatalf("parent request must ask for RISK_ANALYSIS only, got %v", parCalls[0].types)
	}
	pay, _ := h.analysis.last()
	if pay.RiskDelta == nil {
		t.Fatal("RE_CHECK with parent present: payload RiskDelta must be non-nil (Agent 9 ran)")
	}
	if h.meta.callCount() != 0 {
		t.Fatalf("DEFECT-1: trigger carried the pointer; VersionMetaCache must NOT be consulted, got %d calls", h.meta.callCount())
	}
}

// Pin 3 — RE_CHECK via cache fallback.
func TestRun_ReCheck_CacheFallback(t *testing.T) {
	h := newHarness(t)
	parent := "cached-parent"
	h.meta.ret = &parent
	h.artAwait.provided[":parent"] = goodParent()
	o := h.orch()

	if err := o.Run(context.Background(), initialTrigger()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if h.meta.callCount() != 1 {
		t.Fatalf("cache fallback: VersionMetaCache must be consulted exactly once, got %d", h.meta.callCount())
	}
	if len(h.requester.callsFor(":parent")) != 1 {
		t.Fatal("cache-derived RE_CHECK must request parent")
	}
}

// Pin 4 — RE_CHECK parent DEGRADE (parent await times out): success, no fail.
func TestRun_ReCheck_ParentDegrade(t *testing.T) {
	h := newHarness(t)
	h.artAwait.awaitErr[":parent"] = port.ErrAwaitTimeout
	o := h.orch()

	if err := o.Run(context.Background(), reCheckTrigger("p")); err != nil {
		t.Fatalf("parent-degrade must NOT fail the pipeline, got %v", err)
	}
	if h.log.warnCount() == 0 {
		t.Fatal("parent degradation must WARN-log")
	}
	pay, _ := h.analysis.last()
	if pay.RiskDelta != nil {
		t.Fatal("degraded parent ⇒ outbound RiskDelta must be nil (§8.7 step 4)")
	}
	if got := len(h.status.byStatus(model.StatusCompleted)); got != 1 {
		t.Fatalf("degraded RE_CHECK still COMPLETES, got %d COMPLETED", got)
	}
}

// Pin 5 — job timeout: finalizer reclassifies to timeout (D11). A stage
// (Stage 1 Classifier) blocks on its ctx; the only deadline on the
// stage ctx is the job timeout (stages add no sub-timeout — stages D8), so
// rootCtx.Err()==DeadlineExceeded at finalize time even though the blocked
// agent returns a ctx-cancelled error.
func TestRun_JobTimeout_Reclassify(t *testing.T) {
	blockAgent := fakeAgent{id: model.AgentTypeClassifier, run: func(ctx context.Context, _ model.AgentInput) (port.AgentResult, error) {
		<-ctx.Done()
		return nil, model.NewDomainError(model.ErrCodeInternal, model.StageAgentTypeClassifier).WithCause(ctx.Err())
	}}
	h := newHarness(t, blockAgent)
	h.cfg.JobTimeout = 30 * time.Millisecond
	h.cfg.DMRequestTimeout = 25 * time.Millisecond
	o := h.orch()

	err := o.Run(context.Background(), initialTrigger())
	if err == nil {
		t.Fatal("expected timeout error")
	}
	de, ok := model.AsDomainError(err)
	if !ok {
		t.Fatalf("want *model.DomainError, got %T", err)
	}
	if de.Code != model.ErrCodeAnalysisTimeout {
		t.Fatalf("D11: want ANALYSIS_TIMEOUT, got %s", de.Code)
	}
	if !de.Retryable {
		t.Fatal("ANALYSIS_TIMEOUT must be retryable")
	}
	failed := h.status.byStatus(model.StatusFailed)
	if len(failed) != 1 || failed[0].ErrorCode != model.ErrCodeAnalysisTimeout {
		t.Fatalf("single FAILED with ANALYSIS_TIMEOUT expected, got %+v", failed)
	}
	// metrics outcome label = timeout, error_code empty.
	if len(h.metrics.outcomes) != 1 || h.metrics.outcomes[0][1] != outcomeTimeout || h.metrics.outcomes[0][2] != "" {
		t.Fatalf("outcome metric: want {*,timeout,\"\"}, got %v", h.metrics.outcomes)
	}
}

// Pin 6 — merge-early ordering (D3): a fake Agent 6 asserting
// MergedRiskAnalysis != nil at RunStage4 time passes.
func TestRun_MergeEarly_Ordering(t *testing.T) {
	var sawMerged bool
	agent6 := fakeAgent{id: model.AgentRecommendation, run: func(_ context.Context, in model.AgentInput) (port.AgentResult, error) {
		if in.MergedRiskAnalysis != nil {
			sawMerged = true
		}
		return model.Recommendations{}, nil
	}}
	h := newHarness(t, agent6)
	o := h.orch()

	if err := o.Run(context.Background(), initialTrigger()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !sawMerged {
		t.Fatal("D3 regression: Agent 6 (Stage 4) did not observe a merged risk analysis — aggregator must run merge-early BEFORE Stage 4")
	}
}

// Pin 7 — aggregator called exactly twice (merge-early + finalize-late),
// idempotent over the fixed shape.
func TestRun_AggregatorCalledTwice(t *testing.T) {
	// Count Aggregate calls indirectly: each call produces a span child
	// named aggregate.merge / aggregate.finalize.
	h := newHarness(t)
	o := h.orch()
	if err := o.Run(context.Background(), initialTrigger()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	var merge, finalize int
	for _, c := range h.tracer.children {
		switch c {
		case "aggregate.merge":
			merge++
		case "aggregate.finalize":
			finalize++
		}
	}
	if merge != 1 || finalize != 1 {
		t.Fatalf("aggregator must run exactly twice: merge=%d finalize=%d", merge, finalize)
	}
	pay, _ := h.analysis.last()
	if pay.RiskAnalysis == nil {
		t.Fatal("finalize output (merged) must populate payload.RiskAnalysis")
	}
}

// Pin 8 — DOCUMENT_TOO_LARGE (D8): non-retryable, STAGE_ARTIFACTS_RECEIVED.
func TestRun_DocumentTooLarge(t *testing.T) {
	h := newHarness(t)
	h.cfg.MaxIngestedBytes = 4 // current artifacts sum to far more than 4 bytes
	o := h.orch()

	err := o.Run(context.Background(), initialTrigger())
	de, ok := model.AsDomainError(err)
	if !ok {
		t.Fatalf("want *model.DomainError, got %T (%v)", err, err)
	}
	if de.Code != model.ErrCodeDocumentTooLarge {
		t.Fatalf("want DOCUMENT_TOO_LARGE, got %s", de.Code)
	}
	if de.Retryable {
		t.Fatal("DOCUMENT_TOO_LARGE must be non-retryable")
	}
	if de.Stage != model.StageArtifactsReceived {
		t.Fatalf("want STAGE_ARTIFACTS_RECEIVED, got %s", de.Stage)
	}
}

// Pin 9 — low-confidence + noop PauseController: non-retryable
// INTERNAL_ERROR at STAGE_AWAITING_USER_CONFIRMATION (DEFECT-3).
func TestRun_LowConfidence_NoopPause_NonRetryable(t *testing.T) {
	lowConf := fakeAgent{id: model.AgentTypeClassifier, run: func(context.Context, model.AgentInput) (port.AgentResult, error) {
		return &model.ClassificationResult{ContractType: "SERVICE", Confidence: 0.10}, nil
	}}
	h := newHarness(t, lowConf)
	// Use the real noop pause (not the recPause): construct directly.
	o, err := NewOrchestrator(h.cfg, h.exec, h.agg,
		h.requester, h.artAwait, h.analysis, h.persist, h.status, noopUncertainPub{},
		Deps{JobLimiter: h.limiter, Metrics: h.metrics, Tracer: h.tracer, Clock: h.clock, Logger: h.log, VersionMetaCache: h.meta})
	if err != nil {
		t.Fatalf("NewOrchestrator: %v", err)
	}

	rerr := o.Run(context.Background(), initialTrigger())
	de, ok := model.AsDomainError(rerr)
	if !ok {
		t.Fatalf("want *model.DomainError, got %T", rerr)
	}
	if de.Code != model.ErrCodeInternal {
		t.Fatalf("want INTERNAL_ERROR, got %s", de.Code)
	}
	if de.Retryable {
		t.Fatal("DEFECT-3: noop pause stub MUST be non-retryable")
	}
	if de.Stage != model.StageAwaitingUserConfirmation {
		t.Fatalf("want STAGE_AWAITING_USER_CONFIRMATION, got %s", de.Stage)
	}
}

// Pin 10 — verbatim DomainError propagation from a stage.
func TestRun_VerbatimDomainErrorPropagation(t *testing.T) {
	want := model.NewDomainError(model.ErrCodeLLMAllProvidersFailed, model.StageAgentRiskDetection)
	badRisk := fakeAgent{id: model.AgentRiskDetection, run: func(context.Context, model.AgentInput) (port.AgentResult, error) {
		return nil, want
	}}
	h := newHarness(t, badRisk)
	o := h.orch()

	err := o.Run(context.Background(), initialTrigger())
	de, ok := model.AsDomainError(err)
	if !ok {
		t.Fatalf("want *model.DomainError, got %T", err)
	}
	if de.Code != model.ErrCodeLLMAllProvidersFailed || de.Stage != model.StageAgentRiskDetection {
		t.Fatalf("propagation must be verbatim: got code=%s stage=%s", de.Code, de.Stage)
	}
	failed := h.status.byStatus(model.StatusFailed)
	if len(failed) != 1 || failed[0].ErrorCode != model.ErrCodeLLMAllProvidersFailed {
		t.Fatalf("FAILED must carry the verbatim code, got %+v", failed)
	}
}

// Pin 11 — single FAILED publish (no double from finalizer + inline).
func TestRun_SingleFailedPublish(t *testing.T) {
	h := newHarness(t)
	h.artAwait.awaitErr[":current"] = port.ErrAwaitTimeout
	o := h.orch()

	_ = o.Run(context.Background(), initialTrigger())
	if got := len(h.status.byStatus(model.StatusFailed)); got != 1 {
		t.Fatalf("exactly one FAILED publish expected, got %d", got)
	}
	if got := len(h.status.byStatus(model.StatusCompleted)); got != 0 {
		t.Fatalf("no COMPLETED on failure, got %d", got)
	}
}

// Pin 12 — semaphore lifecycle: 1 Acquire, 1 Release, Release after the
// terminal publish; a panic in the body still Releases and still FAILs.
func TestRun_SemaphoreLifecycle(t *testing.T) {
	h := newHarness(t)
	o := h.orch()
	if err := o.Run(context.Background(), initialTrigger()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	acq, rel := h.limiter.counts()
	if acq != 1 || rel != 1 {
		t.Fatalf("want 1 Acquire / 1 Release, got %d / %d", acq, rel)
	}

	// Panic path: a fake agent in a SEQUENTIAL stage (Recommendation /
	// Stage 4 runs in the Run goroutine, not a parallel() child) panics;
	// the deferred Release must still happen on unwind and Run re-panics
	// (it does not recover — Release runs via defer regardless). Note: a
	// panic from a parallel stage would crash the test process (parallel()
	// deliberately does not recover — its CLAUDE.md), so the sequential
	// Stage 4 is the correct, race-clean probe of the defer guarantee.
	panicAgent := fakeAgent{id: model.AgentRecommendation, run: func(context.Context, model.AgentInput) (port.AgentResult, error) {
		panic("boom")
	}}
	h2 := newHarness(t, panicAgent)
	o2 := h2.orch()
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("expected panic to propagate")
			}
		}()
		_ = o2.Run(context.Background(), initialTrigger())
	}()
	a2, r2 := h2.limiter.counts()
	if a2 != 1 || r2 != 1 {
		t.Fatalf("panic path must still Release exactly once: Acquire=%d Release=%d", a2, r2)
	}
}

// Pin 13 — awaiter cleanup on early failure (registry stays bounded).
func TestRun_AwaiterCleanup(t *testing.T) {
	h := newHarness(t)
	h.artAwait.awaitErr[":current"] = port.ErrAwaitTimeout
	o := h.orch()

	_ = o.Run(context.Background(), reCheckTrigger("p"))
	if h.artAwait.cancelCount(":current") != 1 {
		t.Fatalf("curCorr Cancel must run on early failure, got %d", h.artAwait.cancelCount(":current"))
	}
	if h.artAwait.cancelCount(":parent") != 1 {
		t.Fatalf("parCorr Cancel must run on early failure, got %d", h.artAwait.cancelCount(":parent"))
	}
}

// Pin 14 — persist IsFailure non-retryable (§8.8 mirror).
func TestRun_PersistFailure_NonRetryable(t *testing.T) {
	h := newHarness(t)
	h.persist.conf = port.NewPersistConfirmationFailure(&port.LegalAnalysisArtifactsPersistFailed{
		JobID: "job", ErrorMessage: "DOCUMENT_NOT_FOUND", IsRetryable: false,
	})
	h.persist.hasConf = true
	o := h.orch()

	err := o.Run(context.Background(), initialTrigger())
	de, ok := model.AsDomainError(err)
	if !ok {
		t.Fatalf("want *model.DomainError, got %T", err)
	}
	if de.Code != model.ErrCodeDMPersistFailed {
		t.Fatalf("want DM_PERSIST_FAILED, got %s", de.Code)
	}
	if de.Retryable {
		t.Fatal("§8.8: must mirror DM is_retryable=false ⇒ non-retryable")
	}
}

// Pin 15 — Acquire-before-WithTimeout: a limiter that delays > JobTimeout
// still gives the pipeline a full budget (the timeout clock starts AFTER
// Acquire). Fast pipeline work ⇒ no ANALYSIS_TIMEOUT.
func TestRun_AcquireBeforeWithTimeout_BudgetIntact(t *testing.T) {
	h := newHarness(t)
	h.cfg.JobTimeout = 200 * time.Millisecond
	h.cfg.DMRequestTimeout = 150 * time.Millisecond
	h.cfg.DMPersistConfirmTimeout = 150 * time.Millisecond
	h.limiter.acquireWait = 400 * time.Millisecond // exceeds JobTimeout alone
	o := h.orch()

	if err := o.Run(context.Background(), initialTrigger()); err != nil {
		t.Fatalf("limiter delay alone must not consume the job budget; got %v", err)
	}
	if got := len(h.status.byStatus(model.StatusCompleted)); got != 1 {
		t.Fatalf("pipeline should COMPLETE: the 90s-equivalent budget starts after Acquire, got %d COMPLETED", got)
	}
}

// Pin 16 — metrics calls (DEFECT-2: no ConcurrentJobs method on the seam).
func TestRun_Metrics(t *testing.T) {
	h := newHarness(t)
	o := h.orch()
	if err := o.Run(context.Background(), initialTrigger()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(h.metrics.started) != 1 || h.metrics.started[0] != string(model.PipelineModeInitial) {
		t.Fatalf("PipelineStarted once with INITIAL, got %v", h.metrics.started)
	}
	if len(h.metrics.finished) != 1 {
		t.Fatalf("PipelineFinished once, got %d", len(h.metrics.finished))
	}
	if len(h.metrics.outcomes) != 1 || h.metrics.outcomes[0][1] != outcomeSuccess || h.metrics.outcomes[0][2] != "" {
		t.Fatalf("outcome metric want {INITIAL,success,\"\"}, got %v", h.metrics.outcomes)
	}
	if h.metrics.concurrentSeen {
		t.Fatal("DEFECT-2: PipelineMetrics must expose NO ConcurrentJobs method")
	}
}

// Pin (constructor) — fail-fast nil deps surface ALL defects via errors.Join.
func TestNewOrchestrator_FailFast(t *testing.T) {
	_, err := NewOrchestrator(Config{}, nil, nil, nil, nil, nil, nil, nil, nil, Deps{})
	if err == nil {
		t.Fatal("expected fail-fast error for nil required deps + invalid config")
	}
	// All ten messages joined (config + 8 ports/engines + uncertainPub).
	msg := err.Error()
	for _, want := range []string{"JobTimeout", "exec", "agg", "artReq", "artAwait", "analysisPub", "persistAwait", "statusPub", "uncertainPub"} {
		if !contains(msg, want) {
			t.Errorf("joined error must mention %q; got: %s", want, msg)
		}
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// Pin (acquire failure) — pre-pipeline Acquire timeout still publishes FAILED.
func TestRun_AcquireTimeout_PublishesFailed(t *testing.T) {
	h := newHarness(t)
	h.limiter.acquireErr = context.DeadlineExceeded
	o := h.orch()

	err := o.Run(context.Background(), initialTrigger())
	de, ok := model.AsDomainError(err)
	if !ok {
		t.Fatalf("want *model.DomainError, got %T", err)
	}
	if de.Code != model.ErrCodeAnalysisTimeout {
		t.Fatalf("Acquire DeadlineExceeded ⇒ ANALYSIS_TIMEOUT, got %s", de.Code)
	}
	if got := len(h.status.byStatus(model.StatusFailed)); got != 1 {
		t.Fatalf("pre-pipeline failure must still publish FAILED once, got %d", got)
	}
	a, r := h.limiter.counts()
	if a != 1 || r != 0 {
		t.Fatalf("failed Acquire consumes no slot ⇒ no Release: Acquire=%d Release=%d", a, r)
	}
}

// Pin (concurrency) — shared Orchestrator, many concurrent Runs, -race.
func TestRun_ConcurrentRaceClean(t *testing.T) {
	h := newHarness(t)
	o := h.orch()
	const n = 16
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			if err := o.Run(context.Background(), initialTrigger()); err != nil {
				t.Errorf("concurrent Run: %v", err)
			}
		}()
	}
	wg.Wait()
	a, r := h.limiter.counts()
	if a != n || r != n {
		t.Fatalf("each Run = 1 Acquire/1 Release: want %d/%d, got %d/%d", n, n, a, r)
	}
}

// ============================================================================
// SHOULD-FIX coverage closures (review feedback — test-only; production code
// is correct and was NOT touched). Each pin exercises a §6-table row that the
// existing suite left unexercised because the corresponding fake knob
// (recRequester.errOn / fakeArtAwaiter.dupOn) was never set, plus a strict
// payload-stripping assertion proving buildPayload sources
// aggregator.Output.StrippedKeyParameters and never the raw state copy.
// ============================================================================

// assertRequestingArtifactsFailure is the shared §6-row-5/row-8 oracle: the
// run must end as a single non-retryable-classification-independent
// INTERNAL_ERROR at STAGE_REQUESTING_ARTIFACTS, having published EXACTLY one
// FAILED status (the build-spec §5 single-publish path) carrying that verbatim
// code/stage, with NO analysis-ready and NO COMPLETED, and the semaphore slot
// reclaimed (1 Acquire / 1 Release — the defer-LIFO Release still runs because
// Acquire succeeded before the request/Register step failed). wantRetryable
// pins de.Retryable (INTERNAL_ERROR's catalog default is retryable=true —
// error_codes.go:221-225 — and neither the publish-failure nor the duplicate-
// Register path chains .WithRetryable(false), so both rows are retryable).
func assertRequestingArtifactsFailure(t *testing.T, h *harness, err error, wantRetryable bool) {
	t.Helper()
	de, ok := model.AsDomainError(err)
	if !ok {
		t.Fatalf("want *model.DomainError, got %T (%v)", err, err)
	}
	if de.Code != model.ErrCodeInternal {
		t.Fatalf("want INTERNAL_ERROR, got %s", de.Code)
	}
	if de.Stage != model.StageRequestingArtifacts {
		t.Fatalf("want STAGE_REQUESTING_ARTIFACTS, got %s", de.Stage)
	}
	if de.Retryable != wantRetryable {
		t.Fatalf("IsRetryable: want %v, got %v", wantRetryable, de.Retryable)
	}
	failed := h.status.byStatus(model.StatusFailed)
	if len(failed) != 1 {
		t.Fatalf("exactly one FAILED publish expected, got %d", len(failed))
	}
	if failed[0].ErrorCode != model.ErrCodeInternal || failed[0].Stage != model.StageRequestingArtifacts {
		t.Fatalf("FAILED event must carry the verbatim code/stage, got code=%s stage=%s",
			failed[0].ErrorCode, failed[0].Stage)
	}
	if failed[0].IsRetryable == nil || *failed[0].IsRetryable != wantRetryable {
		t.Fatalf("FAILED event IsRetryable: want %v, got %v", wantRetryable, failed[0].IsRetryable)
	}
	if got := len(h.status.byStatus(model.StatusCompleted)); got != 0 {
		t.Fatalf("no COMPLETED on a STAGE_REQUESTING_ARTIFACTS failure, got %d", got)
	}
	if _, published := h.analysis.last(); published {
		t.Fatal("analysis-ready must NOT be published when artifact requesting fails")
	}
	if acq, rel := h.limiter.counts(); acq != 1 || rel != 1 {
		t.Fatalf("slot must be acquired then released exactly once: Acquire=%d Release=%d", acq, rel)
	}
}

// Pin (§6 row 5, current) — recRequester.errOn=":current": the RequestArtifacts
// publish for the CURRENT artifacts fails. requestAndAwaitCurrent maps the
// broker/publish failure to INTERNAL_ERROR / STAGE_REQUESTING_ARTIFACTS
// (retryable — orchestrator.go:556). This closes the dead recRequester.errOn
// knob for the INITIAL path.
func TestRun_RequesterPublishFails_Current(t *testing.T) {
	h := newHarness(t)
	h.requester.errOn = ":current"
	o := h.orch()

	err := o.Run(context.Background(), initialTrigger())
	assertRequestingArtifactsFailure(t, h, err, true)
	// The await must never be reached: the failure is at the publish step,
	// before any Await on the current correlation.
	if h.artAwait.cancelCount(":current") != 1 {
		t.Fatalf("curCorr Cancel must still run on the deferred cleanup, got %d",
			h.artAwait.cancelCount(":current"))
	}
}

// Pin (§6 row 5, parent) — RE_CHECK, recRequester.errOn=":parent": the parent
// RISK_ANALYSIS request publish fails. Per spec §6 row 5 a broker/publish
// failure of the parent request is INTERNAL_ERROR / STAGE_REQUESTING_ARTIFACTS
// (orchestrator.go:566) — it is NOT the degrade path. Degrade (§5 step 10) is
// reserved for the parent AWAIT timing out / missing / parse-failing, which is
// already pinned by TestRun_ReCheck_ParentDegrade. The current request
// succeeds (errOn only matches the ":parent" suffix), so the parent-request
// publish is the sole failure.
func TestRun_RequesterPublishFails_Parent(t *testing.T) {
	h := newHarness(t)
	h.artAwait.provided[":parent"] = goodParent()
	h.requester.errOn = ":parent"
	o := h.orch()

	err := o.Run(context.Background(), reCheckTrigger("parent-ver"))
	assertRequestingArtifactsFailure(t, h, err, true)
	// The current request was published (only the parent suffix fails) but
	// the run aborted before awaiting either correlation; both deferred
	// Cancels must still fire (registry stays bounded — build-spec §5).
	if len(h.requester.callsFor(":current")) != 1 {
		t.Fatalf("current request must have been published once before the parent failed, got %d",
			len(h.requester.callsFor(":current")))
	}
	if h.artAwait.cancelCount(":current") != 1 || h.artAwait.cancelCount(":parent") != 1 {
		t.Fatalf("both awaiter Cancels must run on early failure: current=%d parent=%d",
			h.artAwait.cancelCount(":current"), h.artAwait.cancelCount(":parent"))
	}
}

// Pin (§6 row 8, current) — fakeArtAwaiter.dupOn=":current": the awaiter
// Register for the CURRENT correlation returns port.ErrDuplicateRegistration
// (a LIC-TASK-040 routing/idempotency defect). Run maps it to INTERNAL_ERROR /
// STAGE_REQUESTING_ARTIFACTS at step 7 BEFORE any request is published
// (orchestrator.go:342-345). This closes the dead fakeArtAwaiter.dupOn knob.
func TestRun_AwaiterDuplicateRegistration_Current(t *testing.T) {
	h := newHarness(t)
	h.artAwait.dupOn = ":current"
	o := h.orch()

	err := o.Run(context.Background(), initialTrigger())
	assertRequestingArtifactsFailure(t, h, err, true)
	// Register failed for :current ⇒ Run returns before its deferred Cancel
	// was armed and before any request fired (the defer is registered only
	// AFTER a successful Register — orchestrator.go:346).
	if h.artAwait.cancelCount(":current") != 0 {
		t.Fatalf("a failed Register arms no Cancel, got %d", h.artAwait.cancelCount(":current"))
	}
	if len(h.requester.callsFor(":current")) != 0 {
		t.Fatalf("no request may be published after a failed current Register, got %d",
			len(h.requester.callsFor(":current")))
	}
}

// Pin (§6 row 8, parent) — RE_CHECK, fakeArtAwaiter.dupOn=":parent": the
// CURRENT Register succeeds, then the PARENT Register returns
// port.ErrDuplicateRegistration. Run maps it to INTERNAL_ERROR /
// STAGE_REQUESTING_ARTIFACTS (orchestrator.go:351-354), and the already-armed
// curCorr Cancel still fires on unwind while the parCorr Cancel was never
// armed (its Register failed).
func TestRun_AwaiterDuplicateRegistration_Parent(t *testing.T) {
	h := newHarness(t)
	h.artAwait.provided[":parent"] = goodParent()
	h.artAwait.dupOn = ":parent"
	o := h.orch()

	err := o.Run(context.Background(), reCheckTrigger("parent-ver"))
	assertRequestingArtifactsFailure(t, h, err, true)
	if h.artAwait.cancelCount(":current") != 1 {
		t.Fatalf("curCorr Cancel must run (its Register succeeded), got %d",
			h.artAwait.cancelCount(":current"))
	}
	if h.artAwait.cancelCount(":parent") != 0 {
		t.Fatalf("parCorr Cancel must NOT run (its Register failed), got %d",
			h.artAwait.cancelCount(":parent"))
	}
	if len(h.requester.callsFor(":parent")) != 0 {
		t.Fatalf("parent request must NOT be published after a failed parent Register, got %d",
			len(h.requester.callsFor(":parent")))
	}
}

// Pin (payload stripping) — buildPayload publishes
// aggregator.Output.StrippedKeyParameters, NEVER the raw state.KeyParameters.
// Agent 2 (AgentKeyParams) is overridden to return a *model.KeyParameters
// carrying BOTH LIC-internal signals the aggregator's single stripping site
// drops (strip.go:18-26): a non-nil InternalExtras and
// PromptInjectionDetected=true, alongside a legitimate FROZEN DM-surface field
// (Subject). The real aggregator.Aggregator runs (no aggregator fake — the
// harness wires the real one), so the assertion proves the END-TO-END
// contract: the published key_parameters has the two internal fields cleared
// while Subject survives. If buildPayload had sourced the raw
// state.KeyParameters instead, InternalExtras / PromptInjectionDetected would
// still be set — the test would fail. Deterministic: fixed, content-free
// inputs; no clock/ordering sensitivity.
func TestRun_Payload_KeyParametersStripped(t *testing.T) {
	const subject = "поставка оборудования"
	raw := &model.KeyParameters{
		Subject:                 subject,
		InternalExtras:          &model.KeyParametersInternalExtras{},
		PromptInjectionDetected: true,
	}
	kpAgent := fakeAgent{id: model.AgentKeyParams, run: func(context.Context, model.AgentInput) (port.AgentResult, error) {
		return raw, nil
	}}
	h := newHarness(t, kpAgent)
	o := h.orch()

	if err := o.Run(context.Background(), initialTrigger()); err != nil {
		t.Fatalf("Run: unexpected error %v", err)
	}
	pay, ok := h.analysis.last()
	if !ok {
		t.Fatal("analysis-ready not published")
	}
	if pay.KeyParameters == nil {
		t.Fatal("payload KeyParameters must be non-nil (Agent 2 returned a value)")
	}
	if pay.KeyParameters == raw {
		t.Fatal("payload must carry aggregator Output.StrippedKeyParameters, " +
			"NOT the raw state.KeyParameters (distinct allocation — aggregator D5)")
	}
	if pay.KeyParameters.InternalExtras != nil {
		t.Fatalf("internal_extras must be stripped, got %+v", pay.KeyParameters.InternalExtras)
	}
	if pay.KeyParameters.PromptInjectionDetected {
		t.Fatal("prompt_injection_detected must be stripped (surfaced only via the aggregated warning)")
	}
	if pay.KeyParameters.Subject != subject {
		t.Fatalf("the FROZEN DM-surface field must survive stripping: want %q, got %q",
			subject, pay.KeyParameters.Subject)
	}
	// Stripping must not have mutated the raw Agent-2 output (aggregator D5
	// purity — the orchestrator never aliases the raw into the payload).
	if raw.InternalExtras == nil || !raw.PromptInjectionDetected {
		t.Fatal("aggregator must not mutate the raw KeyParameters in place")
	}
}

// ============================================================================
// LIC-TASK-037 ADDITIVE pins (build-spec D.B 18..23). The 24 functions above
// are UNTOUCHED (Pin 16/D.B reviewer gate). These exercise the paused
// sentinel + ResumeAfterConfirmation + the continueFromStage2 extraction.
// ============================================================================

// classifiedState builds a post-Stage-1, user-confirmed PipelineState as the
// pendingconfirmation.Manager hands to ResumeAfterConfirmation: Stage 1 done,
// Classification non-nil & overridden.
func classifiedState() *model.PipelineState {
	st := model.NewPipelineState("corr-1", "job", "doc", "ver", "org")
	st.OriginType = "UPLOAD"
	st.Classification = &model.ClassificationResult{ContractType: "SUPPLY", Confidence: 1.0}
	return st
}

func reCheckClassifiedState(parent string) *model.PipelineState {
	st := classifiedState()
	st.Mode = model.PipelineModeReCheck
	st.ParentVersionID = &parent
	return st
}

// D.B-18 — TestRun_LowConfidence_RealPause_Sentinel: a fake PauseController
// returning ErrPipelinePaused ⇒ Run returns the sentinel, IsPaused==true, NO
// FAILED, NO COMPLETED, span finished without error, PipelineOutcome
// "paused"/"".
func TestRun_LowConfidence_RealPause_Sentinel(t *testing.T) {
	lowConf := fakeAgent{id: model.AgentTypeClassifier, run: func(context.Context, model.AgentInput) (port.AgentResult, error) {
		return &model.ClassificationResult{ContractType: "SERVICE", Confidence: 0.10}, nil
	}}
	h := newHarness(t, lowConf)
	h.pause.ret = ErrPipelinePaused
	o := h.orch()

	err := o.Run(context.Background(), initialTrigger())
	if err != ErrPipelinePaused {
		t.Fatalf("Run must return ErrPipelinePaused, got %v", err)
	}
	if !IsPaused(err) {
		t.Fatal("IsPaused(err) must be true")
	}
	if got := len(h.status.byStatus(model.StatusFailed)); got != 0 {
		t.Fatalf("paused run must NOT publish FAILED, got %d", got)
	}
	if got := len(h.status.byStatus(model.StatusCompleted)); got != 0 {
		t.Fatalf("paused run must NOT publish COMPLETED, got %d", got)
	}
	if len(h.metrics.outcomes) != 1 || h.metrics.outcomes[0][1] != outcomePaused || h.metrics.outcomes[0][2] != "" {
		t.Fatalf("outcome metric want {*,paused,\"\"}, got %v", h.metrics.outcomes)
	}
}

// D.B-19 — TestResumeAfterConfirmation_Happy: real Executor+Aggregator,
// classified INITIAL state ⇒ IN_PROGRESS{STAGE_AGENT_PARTY_CONSISTENCY} →
// Stage2..6 → analysis-ready → persist → COMPLETED; nil; 1 Acquire/1 Release.
func TestResumeAfterConfirmation_Happy(t *testing.T) {
	h := newHarness(t)
	o := h.orch()

	if err := o.ResumeAfterConfirmation(context.Background(), classifiedState()); err != nil {
		t.Fatalf("resume happy must return nil, got %v", err)
	}
	ip := h.status.byStatus(model.StatusInProgress)
	if len(ip) != 1 || ip[0].Stage != model.StageAgentPartyConsistency {
		t.Fatalf("resume IN_PROGRESS must be STAGE_AGENT_PARTY_CONSISTENCY once, got %+v", ip)
	}
	if got := len(h.status.byStatus(model.StatusCompleted)); got != 1 {
		t.Fatalf("resume must COMPLETE once, got %d", got)
	}
	if _, ok := h.analysis.last(); !ok {
		t.Fatal("resume must publish analysis-ready")
	}
	if acq, rel := h.limiter.counts(); acq != 1 || rel != 1 {
		t.Fatalf("resume re-acquires a slot exactly once: Acquire=%d Release=%d", acq, rel)
	}
	if h.persist.registers != 1 {
		t.Fatalf("persist Register once, got %d", h.persist.registers)
	}
	// No artifact request for the CURRENT version on resume (Stage 1 already
	// consumed the restored InputArtifacts).
	if got := len(h.requester.callsFor(":current")); got != 0 {
		t.Fatalf("resume must NOT re-request current artifacts, got %d", got)
	}
}

// D.B-20 — TestResumeAfterConfirmation_ReCheckParentRefetch.
func TestResumeAfterConfirmation_ReCheckParentRefetch(t *testing.T) {
	// Sub-1: parent await success ⇒ ParentRiskAnalysis set, risk_delta in
	// payload; RequestArtifacts called with :parent:resume + [RISK_ANALYSIS]
	// + *state.ParentVersionID.
	t.Run("ParentPresent", func(t *testing.T) {
		h := newHarness(t)
		h.artAwait.provided["corr-1:parent:resume"] = goodParent()
		o := h.orch()

		if err := o.ResumeAfterConfirmation(context.Background(), reCheckClassifiedState("parent-ver")); err != nil {
			t.Fatalf("resume RE_CHECK must COMPLETE, got %v", err)
		}
		par := h.requester.callsFor(":parent:resume")
		if len(par) != 1 {
			t.Fatalf("parent re-fetch must use the :parent:resume suffix exactly once, got %d", len(par))
		}
		if par[0].versionID != "parent-ver" {
			t.Fatalf("parent request subject must be *state.ParentVersionID, got %q", par[0].versionID)
		}
		if len(par[0].types) != 1 || par[0].types[0] != model.ArtifactRiskAnalysis {
			t.Fatalf("parent request must ask RISK_ANALYSIS only, got %v", par[0].types)
		}
		pay, _ := h.analysis.last()
		if pay.RiskDelta == nil {
			t.Fatal("parent present ⇒ payload risk_delta must be non-nil (Agent 9 ran)")
		}
	})

	// Sub-2: parent await error ⇒ degrade (parentMissing), Stage 6 self-skips,
	// payload risk_delta==nil, pipeline still COMPLETED (degrade-never-fail).
	t.Run("ParentDegrade", func(t *testing.T) {
		h := newHarness(t)
		h.artAwait.awaitErr["corr-1:parent:resume"] = port.ErrAwaitTimeout
		o := h.orch()

		if err := o.ResumeAfterConfirmation(context.Background(), reCheckClassifiedState("parent-ver")); err != nil {
			t.Fatalf("parent degrade must NOT fail the resumed pipeline, got %v", err)
		}
		if h.log.warnCount() == 0 {
			t.Fatal("parent degradation must WARN-log")
		}
		pay, _ := h.analysis.last()
		if pay.RiskDelta != nil {
			t.Fatal("degraded parent ⇒ outbound risk_delta must be nil (§8.7 step 4)")
		}
		if got := len(h.status.byStatus(model.StatusCompleted)); got != 1 {
			t.Fatalf("degraded RE_CHECK resume still COMPLETES, got %d", got)
		}
	})
}

// D.B-21 — TestResumeAfterConfirmation_SingleFinalizer + _JobTimeout.
func TestResumeAfterConfirmation_SingleFinalizer(t *testing.T) {
	want := model.NewDomainError(model.ErrCodeLLMAllProvidersFailed, model.StageAgentMandatoryConditions)
	badStage3 := fakeAgent{id: model.AgentMandatoryConditions, run: func(context.Context, model.AgentInput) (port.AgentResult, error) {
		return nil, want
	}}
	h := newHarness(t, badStage3)
	o := h.orch()

	err := o.ResumeAfterConfirmation(context.Background(), classifiedState())
	de, ok := model.AsDomainError(err)
	if !ok || de.Code != model.ErrCodeLLMAllProvidersFailed {
		t.Fatalf("resume body error must propagate verbatim, got %v", err)
	}
	if got := len(h.status.byStatus(model.StatusFailed)); got != 1 {
		t.Fatalf("exactly one FAILED publish on the resume path, got %d", got)
	}
	if got := len(h.status.byStatus(model.StatusCompleted)); got != 0 {
		t.Fatalf("no COMPLETED on a failed resume, got %d", got)
	}
	if acq, rel := h.limiter.counts(); acq != 1 || rel != 1 {
		t.Fatalf("slot reclaimed exactly once on a failed resume: Acquire=%d Release=%d", acq, rel)
	}
}

func TestResumeAfterConfirmation_JobTimeout(t *testing.T) {
	blockAgent := fakeAgent{id: model.AgentPartyConsistency, run: func(ctx context.Context, _ model.AgentInput) (port.AgentResult, error) {
		<-ctx.Done()
		return nil, model.NewDomainError(model.ErrCodeInternal, model.StageAgentPartyConsistency).WithCause(ctx.Err())
	}}
	h := newHarness(t, blockAgent)
	h.cfg.JobTimeout = 30 * time.Millisecond
	h.cfg.DMRequestTimeout = 25 * time.Millisecond
	h.cfg.DMPersistConfirmTimeout = 25 * time.Millisecond
	o := h.orch()

	err := o.ResumeAfterConfirmation(context.Background(), classifiedState())
	de, ok := model.AsDomainError(err)
	if !ok {
		t.Fatalf("want *model.DomainError, got %T", err)
	}
	if de.Code != model.ErrCodeAnalysisTimeout || !de.Retryable {
		t.Fatalf("fresh-budget overrun ⇒ retryable ANALYSIS_TIMEOUT (classifyOutcome reused), got code=%s retry=%v", de.Code, de.Retryable)
	}
}

// D.B-22 — TestResumeAfterConfirmation_AcquireBeforeWithTimeout: Acquire on
// the raw ctx; a queued resume is not mis-timed.
func TestResumeAfterConfirmation_AcquireBeforeWithTimeout(t *testing.T) {
	h := newHarness(t)
	h.cfg.JobTimeout = 200 * time.Millisecond
	h.cfg.DMRequestTimeout = 150 * time.Millisecond
	h.cfg.DMPersistConfirmTimeout = 150 * time.Millisecond
	h.limiter.acquireWait = 400 * time.Millisecond // exceeds JobTimeout alone
	o := h.orch()

	if err := o.ResumeAfterConfirmation(context.Background(), classifiedState()); err != nil {
		t.Fatalf("limiter delay alone must not consume the resume budget; got %v", err)
	}
	if got := len(h.status.byStatus(model.StatusCompleted)); got != 1 {
		t.Fatalf("resume should COMPLETE: the budget starts after Acquire, got %d", got)
	}
}

// D.B-23 — TestContinueFromStage2_SharedBody: Run (post-gate) and
// ResumeAfterConfirmation produce byte-identical LegalAnalysisArtifactsReady
// for the same post-Stage-1 state (proves the extraction is
// behavior-preserving — the load-bearing refactor pin).
func TestContinueFromStage2_SharedBody(t *testing.T) {
	// Path A: Run an INITIAL pipeline to COMPLETED and capture the payload.
	hRun := newHarness(t)
	oRun := hRun.orch()
	if err := oRun.Run(context.Background(), initialTrigger()); err != nil {
		t.Fatalf("Run path: %v", err)
	}
	runPay, ok := hRun.analysis.last()
	if !ok {
		t.Fatal("Run path published no analysis-ready")
	}

	// Path B: ResumeAfterConfirmation over the equivalent post-Stage-1 state
	// (same identity, same Agent-1 ContractType=SERVICE/Confidence as the
	// Run-path Stage 1 produced via defaultResult).
	hRes := newHarness(t)
	oRes := hRes.orch()
	st := model.NewPipelineState("corr-1", "job", "doc", "ver", "org")
	st.OriginType = "UPLOAD"
	// Post-Stage-1 state as the Manager restores it from the pending blob:
	// Stage 1 ran Agent 1 (Classification) AND Agent 2 (KeyParams). The Run
	// path's fake Agent 2 returns &model.KeyParameters{} (defaultResult), so
	// the equivalent resume state carries the same — proving the SHARED body
	// is behavior-preserving over identical post-Stage-1 input.
	st.Classification = &model.ClassificationResult{ContractType: "SERVICE", Confidence: 0.99}
	st.KeyParameters = &model.KeyParameters{}
	if err := oRes.ResumeAfterConfirmation(context.Background(), st); err != nil {
		t.Fatalf("Resume path: %v", err)
	}
	resPay, ok := hRes.analysis.last()
	if !ok {
		t.Fatal("Resume path published no analysis-ready")
	}

	a, err := json.Marshal(runPay)
	if err != nil {
		t.Fatalf("marshal run payload: %v", err)
	}
	b, err := json.Marshal(resPay)
	if err != nil {
		t.Fatalf("marshal resume payload: %v", err)
	}
	if string(a) != string(b) {
		t.Fatalf("continueFromStage2 must be behavior-preserving: Run and Resume payloads differ\n run=%s\n res=%s", a, b)
	}
}
