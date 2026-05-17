package base

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"contractpro/legal-intelligence-core/internal/agents/promptbuilder"
	"contractpro/legal-intelligence-core/internal/domain/model"
	"contractpro/legal-intelligence-core/internal/domain/port"
)

// --- shared test fixtures ---------------------------------------------------

const testSchema = `{"$schema":"http://json-schema.org/draft-07/schema#","title":"t","type":"object","required":["k"],"properties":{"k":{"type":"string"}},"additionalProperties":false}`

const validJSON = `{"k":"ok"}`
const invalidJSON = `{"x":1}` // missing required "k" + additionalProperties:false

type testResult struct {
	K string `json:"k"`
}

// fakeSpec is a stateless Spec: one escaped content block, strict decode.
type fakeSpec struct {
	partsErr  error
	decodeErr error
}

func (s fakeSpec) Parts(b *promptbuilder.Builder, _ model.AgentInput) ([]promptbuilder.Part, error) {
	if s.partsErr != nil {
		return nil, s.partsErr
	}
	return []promptbuilder.Part{promptbuilder.Content("doc", "body")}, nil
}

func (s fakeSpec) Decode(content []byte) (port.AgentResult, error) {
	if s.decodeErr != nil {
		return nil, s.decodeErr
	}
	var r testResult
	if err := json.Unmarshal(content, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// fakeRouter implements port.ProviderRouterPort.
type fakeRouter struct {
	complete func(ctx context.Context, req port.CompletionRequest) (port.PrimaryCallResult, error)
	repair   func(ctx context.Context, req port.CompletionRequest, used port.LLMProviderID) (port.CompletionResponse, error)
}

func (r fakeRouter) Complete(ctx context.Context, req port.CompletionRequest) (port.PrimaryCallResult, error) {
	return r.complete(ctx, req)
}

func (r fakeRouter) CompleteRepair(ctx context.Context, req port.CompletionRequest, used port.LLMProviderID) (port.CompletionResponse, error) {
	return r.repair(ctx, req, used)
}

var _ port.ProviderRouterPort = fakeRouter{}

func primaryOK(content string) func(context.Context, port.CompletionRequest) (port.PrimaryCallResult, error) {
	return func(_ context.Context, _ port.CompletionRequest) (port.PrimaryCallResult, error) {
		return port.PrimaryCallResult{
			Response: port.CompletionResponse{
				Content:      content,
				InputTokens:  100,
				OutputTokens: 42,
				LatencyMs:    7,
				ProviderID:   port.ProviderClaude,
				Model:        "claude-test",
			},
			UsedProvider: port.ProviderClaude,
		}, nil
	}
}

// fakeMetrics records the four BaseAgent-owned signals.
type fakeMetrics struct {
	mu          sync.Mutex
	invocations [][2]string
	durations   []float64
	inputTok    []int
	outputTok   []int
}

func (m *fakeMetrics) Invocation(a, o string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.invocations = append(m.invocations, [2]string{a, o})
}
func (m *fakeMetrics) Duration(_ string, s float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.durations = append(m.durations, s)
}
func (m *fakeMetrics) InputTokens(_ string, n int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.inputTok = append(m.inputTok, n)
}
func (m *fakeMetrics) OutputTokens(_ string, n int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.outputTok = append(m.outputTok, n)
}

// fakeRepairMetrics implements schemavalidator.Metrics.
type fakeRepairMetrics struct {
	mu       sync.Mutex
	attempts int
	outcomes []string
}

func (m *fakeRepairMetrics) RepairAttempt(string, string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.attempts++
}
func (m *fakeRepairMetrics) RepairOutcome(_, _, outcome string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.outcomes = append(m.outcomes, outcome)
}

// fakeTracer records the two-span structure.
type fakeTracer struct{ span *fakeAgentSpan }

func (t *fakeTracer) StartAgent(ctx context.Context, in AgentSpanInput) (context.Context, AgentSpan) {
	t.span = &fakeAgentSpan{in: in}
	return ctx, t.span
}

type fakeAgentSpan struct {
	in        AgentSpanInput
	out       AgentSpanOutput
	err       error
	finished  bool
	llm       *fakeLLMSpan
	llmBefore bool // llm span finished before this one
}

func (s *fakeAgentSpan) StartLLMCall(ctx context.Context) (context.Context, LLMSpan) {
	s.llm = &fakeLLMSpan{parent: s}
	return ctx, s.llm
}
func (s *fakeAgentSpan) Finish(out AgentSpanOutput, err error) {
	s.finished = true
	s.out = out
	s.err = err
	if s.llm != nil && s.llm.finished {
		s.llmBefore = true
	}
}

type fakeLLMSpan struct {
	parent   *fakeAgentSpan
	out      LLMSpanOutput
	finished bool
}

func (s *fakeLLMSpan) Finish(out LLMSpanOutput) {
	s.finished = true
	s.out = out
}

func goodConfig() Config {
	return Config{
		AgentID:     model.AgentTypeClassifier,
		Stage:       model.StageAgentTypeClassifier,
		System:      "you are a classifier",
		Schema:      []byte(testSchema),
		Model:       "claude-test",
		MaxTokens:   400,
		Temperature: 0,
		Timeout:     2 * time.Second,
	}
}

func newAgent(t *testing.T, spec Spec, deps Deps) *BaseAgent {
	t.Helper()
	a, err := NewBaseAgent(goodConfig(), spec, deps)
	if err != nil {
		t.Fatalf("NewBaseAgent: %v", err)
	}
	return a
}

// --- tests ------------------------------------------------------------------

// Шаг 2: success path → AgentResult returned, metrics updated.
func TestRun_Success(t *testing.T) {
	mx := &fakeMetrics{}
	rmx := &fakeRepairMetrics{}
	tr := &fakeTracer{}
	a := newAgent(t, fakeSpec{}, Deps{
		Router:        fakeRouter{complete: primaryOK(validJSON)},
		Metrics:       mx,
		RepairMetrics: rmx,
		Tracer:        tr,
	})

	in := model.AgentInput{
		CorrelationID:   "c1",
		JobID:           "j1",
		VersionID:       "v1",
		DocumentID:      "d1",
		OrganizationID:  "o1",
		CreatedByUserID: "u1",
	}
	res, err := a.Run(context.Background(), in)
	if err != nil {
		t.Fatalf("Run: unexpected error: %v", err)
	}
	tr2, ok := res.(*testResult)
	if !ok || tr2.K != "ok" {
		t.Fatalf("decoded result = %#v, want *testResult{K:ok}", res)
	}
	if len(mx.invocations) != 1 || mx.invocations[0] != [2]string{model.AgentTypeClassifier.String(), "success"} {
		t.Fatalf("invocations = %v, want one {AGENT_TYPE_CLASSIFIER,success}", mx.invocations)
	}
	if len(mx.durations) != 1 {
		t.Fatalf("durations recorded = %d, want 1", len(mx.durations))
	}
	if len(mx.inputTok) != 1 || mx.inputTok[0] <= 0 {
		t.Fatalf("inputTok = %v, want one positive estimate", mx.inputTok)
	}
	if len(mx.outputTok) != 1 || mx.outputTok[0] != 42 {
		t.Fatalf("outputTok = %v, want [42]", mx.outputTok)
	}
	// MF-4: no repair metrics on the happy path.
	if rmx.attempts != 0 || len(rmx.outcomes) != 0 {
		t.Fatalf("repair metrics fired on happy path: attempts=%d outcomes=%v", rmx.attempts, rmx.outcomes)
	}
	// Span structure: agent span finished, outcome=success, repair_attempts=0,
	// child llm span finished BEFORE the parent (§4.2).
	if !tr.span.finished || tr.span.out.Outcome != "success" || tr.span.out.RepairAttempts != 0 {
		t.Fatalf("agent span = %+v", tr.span.out)
	}
	if tr.span.err != nil {
		t.Fatalf("agent span err = %v, want nil", tr.span.err)
	}
	if tr.span.llm == nil || !tr.span.llm.finished || !tr.span.llmBefore {
		t.Fatalf("llm span not finished before agent span: %+v", tr.span)
	}
	// Acceptance criterion 3: provider, model, input_tokens, output_tokens,
	// latency_ms all pinned on the child llm span.
	wantLLM := LLMSpanOutput{Provider: "claude", Model: "claude-test", InputTokens: tr.span.llm.out.InputTokens, OutputTokens: 42, LatencyMs: 7}
	if tr.span.llm.out != wantLLM || tr.span.llm.out.InputTokens <= 0 {
		t.Fatalf("llm span out = %+v, want provider/model/output=42/latency=7 + positive input", tr.span.llm.out)
	}
	// L1: every correlation field is propagated verbatim (observability §4.3).
	wantCorr := Correlation{CorrelationID: "c1", JobID: "j1", VersionID: "v1", DocumentID: "d1", OrganizationID: "o1", CreatedByUserID: "u1"}
	if tr.span.in.AgentID != model.AgentTypeClassifier.String() || tr.span.in.Correlation != wantCorr {
		t.Fatalf("agent span in = %+v, want corr %+v", tr.span.in, wantCorr)
	}
}

// Шаг 3: timeout → AGENT_TIMEOUT error, outcome=timeout.
func TestRun_Timeout(t *testing.T) {
	mx := &fakeMetrics{}
	cfg := goodConfig()
	cfg.Timeout = 20 * time.Millisecond
	a, err := NewBaseAgent(cfg, fakeSpec{}, Deps{
		Metrics: mx,
		Router: fakeRouter{complete: func(ctx context.Context, _ port.CompletionRequest) (port.PrimaryCallResult, error) {
			<-ctx.Done()
			return port.PrimaryCallResult{}, ctx.Err()
		}},
	})
	if err != nil {
		t.Fatalf("NewBaseAgent: %v", err)
	}

	_, rerr := a.Run(context.Background(), model.AgentInput{})
	de, ok := model.AsDomainError(rerr)
	if !ok || de.Code != model.ErrCodeAgentTimeout {
		t.Fatalf("err = %v, want *DomainError AGENT_TIMEOUT", rerr)
	}
	if got := mx.invocations; len(got) != 1 || got[0][1] != "timeout" {
		t.Fatalf("invocations = %v, want one timeout", got)
	}
	if de.Stage != model.StageAgentTypeClassifier {
		t.Fatalf("stage = %q, want STAGE_AGENT_TYPE_CLASSIFIER", de.Stage)
	}
}

// H3a: realistic S4 path — the router returns AFTER the per-agent deadline
// fired, wrapping ctx.Err() in a typed *LLMProviderError (exactly what the
// real router does on a ctx-expired chain). cctx.Err()==DeadlineExceeded must
// be decisive over the typed-error probe ⇒ AGENT_TIMEOUT/timeout, NOT
// provider_error. Pins S4 end-to-end through a real context.WithTimeout.
func TestRun_Timeout_WrappedProviderError(t *testing.T) {
	mx := &fakeMetrics{}
	cfg := goodConfig()
	cfg.Timeout = 20 * time.Millisecond
	a, err := NewBaseAgent(cfg, fakeSpec{}, Deps{
		Metrics: mx,
		Router: fakeRouter{complete: func(ctx context.Context, _ port.CompletionRequest) (port.PrimaryCallResult, error) {
			<-ctx.Done() // let OUR per-agent deadline fire
			return port.PrimaryCallResult{}, port.NewLLMProviderError(port.LLMErrorAllProvidersFailed, ctx.Err())
		}},
	})
	if err != nil {
		t.Fatalf("NewBaseAgent: %v", err)
	}
	_, rerr := a.Run(context.Background(), model.AgentInput{})
	if de, ok := model.AsDomainError(rerr); !ok || de.Code != model.ErrCodeAgentTimeout {
		t.Fatalf("err = %v, want AGENT_TIMEOUT (deadline decisive over wrapped ALL_PROVIDERS_FAILED)", rerr)
	}
	if got := mx.invocations; len(got) != 1 || got[0][1] != "timeout" {
		t.Fatalf("invocations = %v, want timeout", got)
	}
}

// Шаг 4: repair_success path → correct metric + span repair_attempts=1.
func TestRun_RepairSuccess(t *testing.T) {
	mx := &fakeMetrics{}
	rmx := &fakeRepairMetrics{}
	tr := &fakeTracer{}
	a := newAgent(t, fakeSpec{}, Deps{
		Metrics:       mx,
		RepairMetrics: rmx,
		Tracer:        tr,
		Router: fakeRouter{
			complete: primaryOK(invalidJSON),
			repair: func(_ context.Context, _ port.CompletionRequest, _ port.LLMProviderID) (port.CompletionResponse, error) {
				return port.CompletionResponse{Content: validJSON, OutputTokens: 11, LatencyMs: 3}, nil
			},
		},
	})

	res, err := a.Run(context.Background(), model.AgentInput{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if r, ok := res.(*testResult); !ok || r.K != "ok" {
		t.Fatalf("res = %#v", res)
	}
	if got := mx.invocations; len(got) != 1 || got[0][1] != "repair_success" {
		t.Fatalf("invocations = %v, want one repair_success", got)
	}
	if mx.outputTok[0] != 11 {
		t.Fatalf("outputTok = %v, want repaired-response 11", mx.outputTok)
	}
	if tr.span.out.RepairAttempts != 1 {
		t.Fatalf("span repair_attempts = %d, want 1", tr.span.out.RepairAttempts)
	}
	if rmx.attempts != 1 || len(rmx.outcomes) != 1 || rmx.outcomes[0] != "repaired_ok" {
		t.Fatalf("repair metrics = attempts:%d outcomes:%v, want 1 / [repaired_ok]", rmx.attempts, rmx.outcomes)
	}
}

func TestRun_RepairFailed_InvalidOutput(t *testing.T) {
	mx := &fakeMetrics{}
	tr := &fakeTracer{}
	a := newAgent(t, fakeSpec{}, Deps{
		Metrics: mx,
		Tracer:  tr,
		Router: fakeRouter{
			complete: primaryOK(invalidJSON),
			repair: func(_ context.Context, _ port.CompletionRequest, _ port.LLMProviderID) (port.CompletionResponse, error) {
				return port.CompletionResponse{Content: invalidJSON}, nil // still invalid
			},
		},
	})

	_, err := a.Run(context.Background(), model.AgentInput{})
	de, ok := model.AsDomainError(err)
	if !ok || de.Code != model.ErrCodeAgentOutputInvalid {
		t.Fatalf("err = %v, want AGENT_OUTPUT_INVALID", err)
	}
	if got := mx.invocations; len(got) != 1 || got[0][1] != "invalid_output" {
		t.Fatalf("invocations = %v, want invalid_output", got)
	}
	if len(mx.outputTok) != 0 {
		t.Fatalf("outputTok recorded on failure: %v", mx.outputTok)
	}
	// C1: a repair turn WAS issued (repair_failed) → span repair_attempts=1,
	// matching schemavalidator's lic_agent_repair_attempts_total.
	if tr.span.out.RepairAttempts != 1 {
		t.Fatalf("span repair_attempts = %d, want 1 (turn issued)", tr.span.out.RepairAttempts)
	}
}

func TestRun_RepairProviderError(t *testing.T) {
	mx := &fakeMetrics{}
	tr := &fakeTracer{}
	a := newAgent(t, fakeSpec{}, Deps{
		Metrics: mx,
		Tracer:  tr,
		Router: fakeRouter{
			complete: primaryOK(invalidJSON),
			repair: func(_ context.Context, _ port.CompletionRequest, _ port.LLMProviderID) (port.CompletionResponse, error) {
				return port.CompletionResponse{}, port.NewLLMProviderError(port.LLMErrorServerError, errors.New("boom"))
			},
		},
	})

	_, err := a.Run(context.Background(), model.AgentInput{})
	de, ok := model.AsDomainError(err)
	if !ok || de.Code != model.ErrCodeAgentOutputInvalid {
		t.Fatalf("err = %v, want AGENT_OUTPUT_INVALID(provider)", err)
	}
	if got := mx.invocations; len(got) != 1 || got[0][1] != "provider_error" {
		t.Fatalf("invocations = %v, want provider_error", got)
	}
	// C1: a repair turn WAS issued (repair_provider_error) → span
	// repair_attempts=1. H1: the child llm span carries the PRIMARY call's
	// figures (the failed repair produced no usable response — documented
	// one-logical-span behaviour) and the parent span carries the error.
	if tr.span.out.RepairAttempts != 1 {
		t.Fatalf("span repair_attempts = %d, want 1 (turn issued)", tr.span.out.RepairAttempts)
	}
	if tr.span.llm == nil || tr.span.llm.out.Provider != "claude" || tr.span.llm.out.OutputTokens != 42 {
		t.Fatalf("llm span (primary figures) = %+v", tr.span.llm)
	}
	if de2, _ := model.AsDomainError(tr.span.err); de2 == nil || de2.Code != model.ErrCodeAgentOutputInvalid {
		t.Fatalf("agent span err = %v, want AGENT_OUTPUT_INVALID on the span", tr.span.err)
	}
}

func TestRun_PrimaryAllProvidersFailed(t *testing.T) {
	mx := &fakeMetrics{}
	a := newAgent(t, fakeSpec{}, Deps{
		Metrics: mx,
		Router: fakeRouter{complete: func(context.Context, port.CompletionRequest) (port.PrimaryCallResult, error) {
			return port.PrimaryCallResult{}, port.NewLLMProviderError(port.LLMErrorAllProvidersFailed, errors.New("chain exhausted"))
		}},
	})

	_, err := a.Run(context.Background(), model.AgentInput{})
	de, ok := model.AsDomainError(err)
	if !ok || de.Code != model.ErrCodeLLMAllProvidersFailed {
		t.Fatalf("err = %v, want LLM_ALL_PROVIDERS_FAILED", err)
	}
	if got := mx.invocations; len(got) != 1 || got[0][1] != "provider_error" {
		t.Fatalf("invocations = %v, want provider_error", got)
	}
}

func TestRun_PrimaryContextTooLong(t *testing.T) {
	a := newAgent(t, fakeSpec{}, Deps{
		Router: fakeRouter{complete: func(context.Context, port.CompletionRequest) (port.PrimaryCallResult, error) {
			return port.PrimaryCallResult{}, port.NewLLMProviderError(port.LLMErrorContextTooLong, nil)
		}},
	})
	_, err := a.Run(context.Background(), model.AgentInput{})
	if de, ok := model.AsDomainError(err); !ok || de.Code != model.ErrCodeAgentInputTooLarge {
		t.Fatalf("err = %v, want AGENT_INPUT_TOO_LARGE", err)
	}
}

func TestRun_BuildDefect_PartsError(t *testing.T) {
	mx := &fakeMetrics{}
	a := newAgent(t, fakeSpec{partsErr: errors.New("bad parts")}, Deps{
		Metrics: mx,
		Router:  fakeRouter{complete: primaryOK(validJSON)},
	})
	_, err := a.Run(context.Background(), model.AgentInput{})
	if de, ok := model.AsDomainError(err); !ok || de.Code != model.ErrCodeInternal {
		t.Fatalf("err = %v, want INTERNAL_ERROR", err)
	}
	if got := mx.invocations; len(got) != 1 || got[0][1] != "invalid_output" {
		t.Fatalf("invocations = %v, want invalid_output (lossy projection)", got)
	}
	// No Fit ran ⇒ no input-token observation (MF-5).
	if len(mx.inputTok) != 0 {
		t.Fatalf("inputTok recorded before Fit: %v", mx.inputTok)
	}
}

func TestRun_BuildDefect_DecodeError(t *testing.T) {
	mx := &fakeMetrics{}
	tr := &fakeTracer{}
	a := newAgent(t, fakeSpec{decodeErr: errors.New("schema/struct mismatch")}, Deps{
		Metrics: mx,
		Tracer:  tr,
		Router:  fakeRouter{complete: primaryOK(validJSON)},
	})
	_, err := a.Run(context.Background(), model.AgentInput{})
	if de, ok := model.AsDomainError(err); !ok || de.Code != model.ErrCodeInternal {
		t.Fatalf("err = %v, want INTERNAL_ERROR", err)
	}
	if got := mx.invocations; len(got) != 1 || got[0][1] != "invalid_output" {
		t.Fatalf("invocations = %v, want invalid_output", got)
	}
	// MF-2 truth-preservation: the metric label is the lossy invalid_output
	// projection, but the un-lossy INTERNAL_ERROR truth survives on the span.
	if de, _ := model.AsDomainError(tr.span.err); de == nil || de.Code != model.ErrCodeInternal {
		t.Fatalf("agent span err = %v, want INTERNAL_ERROR (un-lossy truth)", tr.span.err)
	}
}

func TestRun_OverBudget(t *testing.T) {
	mx := &fakeMetrics{}
	a := newAgent(t, fakeSpec{}, Deps{
		Metrics:   mx,
		Router:    fakeRouter{complete: primaryOK(validJSON)},
		Estimator: overBudgetEstimator{},
	})
	_, err := a.Run(context.Background(), model.AgentInput{})
	if de, ok := model.AsDomainError(err); !ok || de.Code != model.ErrCodeAgentInputTooLarge {
		t.Fatalf("err = %v, want AGENT_INPUT_TOO_LARGE", err)
	}
	// M3: documented lossy projection of the pre-call over-budget path.
	if got := mx.invocations; len(got) != 1 || got[0][1] != "provider_error" {
		t.Fatalf("invocations = %v, want provider_error (over-budget projection)", got)
	}
}

type overBudgetEstimator struct{}

func (overBudgetEstimator) Fit(port.CompletionRequest) (int, bool) { return 999999, true }

func TestNewBaseAgent_FailFast(t *testing.T) {
	bad := func(mut func(*Config)) Config {
		c := goodConfig()
		mut(&c)
		return c
	}
	cases := []struct {
		name string
		cfg  Config
		spec Spec
		deps Deps
	}{
		{"nil spec", goodConfig(), nil, Deps{Router: fakeRouter{}}},
		{"nil router", goodConfig(), fakeSpec{}, Deps{}},
		{"bad agent", bad(func(c *Config) { c.AgentID = "NOPE" }), fakeSpec{}, Deps{Router: fakeRouter{}}},
		{"stage mismatch", bad(func(c *Config) { c.Stage = model.StageAgentKeyParams }), fakeSpec{}, Deps{Router: fakeRouter{}}},
		{"empty system", bad(func(c *Config) { c.System = "" }), fakeSpec{}, Deps{Router: fakeRouter{}}},
		{"empty schema", bad(func(c *Config) { c.Schema = nil }), fakeSpec{}, Deps{Router: fakeRouter{}}},
		{"empty model", bad(func(c *Config) { c.Model = "" }), fakeSpec{}, Deps{Router: fakeRouter{}}},
		{"bad maxtokens", bad(func(c *Config) { c.MaxTokens = 0 }), fakeSpec{}, Deps{Router: fakeRouter{}}},
		{"bad temp", bad(func(c *Config) { c.Temperature = 2 }), fakeSpec{}, Deps{Router: fakeRouter{}}},
		{"bad timeout", bad(func(c *Config) { c.Timeout = 0 }), fakeSpec{}, Deps{Router: fakeRouter{}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := NewBaseAgent(tc.cfg, tc.spec, tc.deps); err == nil {
				t.Fatalf("NewBaseAgent(%s): want error, got nil", tc.name)
			}
		})
	}
}

func TestNewBaseAgent_OK_AllAgents(t *testing.T) {
	for id, stage := range canonicalStage {
		c := goodConfig()
		c.AgentID = id
		c.Stage = stage
		if _, err := NewBaseAgent(c, fakeSpec{}, Deps{Router: fakeRouter{}}); err != nil {
			t.Fatalf("NewBaseAgent(%s): %v", id, err)
		}
	}
}

func TestBaseAgent_ImplementsPortAgent(t *testing.T) {
	var _ port.Agent = (*BaseAgent)(nil)
	a := newAgent(t, fakeSpec{}, Deps{Router: fakeRouter{complete: primaryOK(validJSON)}})
	if a.ID() != model.AgentTypeClassifier {
		t.Fatalf("ID() = %q", a.ID())
	}
}

// S5: one *BaseAgent shared by the parallel pipeline, -race clean.
func TestRun_ConcurrentRaceClean(t *testing.T) {
	a := newAgent(t, fakeSpec{}, Deps{
		Metrics: &fakeMetrics{},
		Router: fakeRouter{
			complete: primaryOK(invalidJSON),
			repair: func(_ context.Context, _ port.CompletionRequest, _ port.LLMProviderID) (port.CompletionResponse, error) {
				return port.CompletionResponse{Content: validJSON}, nil
			},
		},
	})
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := a.Run(context.Background(), model.AgentInput{CorrelationID: "x"}); err != nil {
				t.Errorf("concurrent Run: %v", err)
			}
		}()
	}
	wg.Wait()
}

func TestClassifyCompleteError_DeadlineDecisiveOverWrappedRateLimit(t *testing.T) {
	// S4: a RATE_LIMIT error wrapping a deadline must still classify as
	// timeout because OUR derived ctx deadline fired (cctx.Err() decisive).
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	wrapped := port.NewLLMProviderError(port.LLMErrorRateLimit, context.DeadlineExceeded)
	code, oc := classifyCompleteError(ctx, wrapped)
	if code != model.ErrCodeAgentTimeout || oc != OutcomeTimeout {
		t.Fatalf("classify = (%s,%s), want (AGENT_TIMEOUT,timeout)", code, oc)
	}
}

func TestRun_DomainErrorCarriesAgentID(t *testing.T) {
	a := newAgent(t, fakeSpec{partsErr: errors.New("x")}, Deps{Router: fakeRouter{}})
	_, err := a.Run(context.Background(), model.AgentInput{})
	de, _ := model.AsDomainError(err)
	if de == nil || de.Attributes["agent_id"] != model.AgentTypeClassifier.String() {
		t.Fatalf("agent_id attribute missing: %+v", de)
	}
	if !strings.Contains(de.Error(), "INTERNAL_ERROR") {
		t.Fatalf("DomainError.Error()=%q lacks code", de.Error())
	}
}
