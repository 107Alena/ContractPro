package router

import (
	"context"
	"sync"
	"time"

	"contractpro/legal-intelligence-core/internal/domain/model"
	"contractpro/legal-intelligence-core/internal/domain/port"
)

// fakeProvider is a programmable port.LLMProviderPort. completeFn / healthFn
// are swappable so each test scripts the exact sequence it needs; calls are
// counted (atomic under -race via the mutex) for assertions.
type fakeProvider struct {
	id port.LLMProviderID

	mu            sync.Mutex
	completeCalls int
	healthCalls   int

	completeFn func(ctx context.Context, req port.CompletionRequest, call int) (port.CompletionResponse, error)
	healthFn   func(ctx context.Context, call int) (*port.LLMProviderError, error)
}

func newFakeProvider(id port.LLMProviderID) *fakeProvider {
	return &fakeProvider{id: id}
}

func (f *fakeProvider) ID() port.LLMProviderID { return f.id }

func (f *fakeProvider) Complete(ctx context.Context, req port.CompletionRequest) (port.CompletionResponse, error) {
	f.mu.Lock()
	f.completeCalls++
	n := f.completeCalls
	fn := f.completeFn
	f.mu.Unlock()
	if fn == nil {
		return port.CompletionResponse{ProviderID: f.id, Model: req.Model}, nil
	}
	return fn(ctx, req, n)
}

func (f *fakeProvider) HealthCheck(ctx context.Context) (*port.LLMProviderError, error) {
	f.mu.Lock()
	f.healthCalls++
	n := f.healthCalls
	fn := f.healthFn
	f.mu.Unlock()
	if fn == nil {
		return nil, nil
	}
	return fn(ctx, n)
}

func (f *fakeProvider) completeCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.completeCalls
}

func (f *fakeProvider) healthCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.healthCalls
}

// okResponse is the canonical success the default success-fns return.
func okResponse(id port.LLMProviderID, model string) port.CompletionResponse {
	return port.CompletionResponse{
		Content:      `{"ok":true}`,
		InputTokens:  100,
		OutputTokens: 50,
		StopReason:   port.StopReasonEndTurn,
		LatencyMs:    42,
		ProviderID:   id,
		Model:        model,
	}
}

// fakeRateLimiter records Wait calls and can be scripted to fail. waitErr,
// when set, is returned for every Wait (used for the ctx-abort / skip
// tests); nil means "token always available immediately".
type fakeRateLimiter struct {
	mu      sync.Mutex
	calls   []port.LLMProviderID
	waitErr error
	waitFn  func(ctx context.Context, id port.LLMProviderID) error
}

func (f *fakeRateLimiter) Wait(ctx context.Context, id port.LLMProviderID) error {
	f.mu.Lock()
	f.calls = append(f.calls, id)
	fn := f.waitFn
	err := f.waitErr
	f.mu.Unlock()
	if fn != nil {
		return fn(ctx, id)
	}
	return err
}

func (f *fakeRateLimiter) waitCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

var _ RateLimiter = (*fakeRateLimiter)(nil)

// usageEvent captures one ObserveSuccess / ObserveCall invocation for
// assertion (the success-volume undercount and the repair-outcome rules
// are subtle enough to pin explicitly).
type usageEvent struct {
	kind     string // "success" | "call"
	provider port.LLMProviderID
	model    string
	agent    model.AgentID
	outcome  CallOutcome
}

type recordingUsage struct {
	mu     sync.Mutex
	events []usageEvent
}

func (u *recordingUsage) ObserveSuccess(p port.LLMProviderID, m string, a model.AgentID, _ port.CompletionResponse) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.events = append(u.events, usageEvent{kind: "success", provider: p, model: m, agent: a, outcome: OutcomeSuccess})
}

func (u *recordingUsage) ObserveCall(p port.LLMProviderID, m string, a model.AgentID, o CallOutcome) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.events = append(u.events, usageEvent{kind: "call", provider: p, model: m, agent: a, outcome: o})
}

func (u *recordingUsage) snapshot() []usageEvent {
	u.mu.Lock()
	defer u.mu.Unlock()
	out := make([]usageEvent, len(u.events))
	copy(out, u.events)
	return out
}

func (u *recordingUsage) count(kind string, o CallOutcome) int {
	u.mu.Lock()
	defer u.mu.Unlock()
	n := 0
	for _, e := range u.events {
		if e.kind == kind && e.outcome == o {
			n++
		}
	}
	return n
}

var _ UsageTracker = (*recordingUsage)(nil)

// recordingMetrics counts every router metric signal so tests assert the
// acceptance-criterion-8 emissions precisely.
// fallback rows are {from,to,agent}; failed rows are {provider,code};
// health rows are {provider,state} — all stored stringified for easy assert.
type recordingMetrics struct {
	mu               sync.Mutex
	fallback         [][3]string
	skippedUnhealthy []port.LLMProviderID
	failed           [][2]string
	health           [][2]string
}

func (m *recordingMetrics) ProviderFallback(from, to port.LLMProviderID, agent model.AgentID) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.fallback = append(m.fallback, [3]string{from.String(), to.String(), agent.String()})
}

func (m *recordingMetrics) ProviderSkippedUnhealthy(p port.LLMProviderID) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.skippedUnhealthy = append(m.skippedUnhealthy, p)
}

func (m *recordingMetrics) ProviderFailed(p port.LLMProviderID, code port.LLMErrorCode) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.failed = append(m.failed, [2]string{p.String(), code.String()})
}

func (m *recordingMetrics) ProviderHealthState(p port.LLMProviderID, s HealthState) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.health = append(m.health, [2]string{p.String(), string(s)})
}

func (m *recordingMetrics) fallbackCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.fallback)
}

func (m *recordingMetrics) skippedCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.skippedUnhealthy)
}

func (m *recordingMetrics) failedCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.failed)
}

func (m *recordingMetrics) lastHealth(p port.LLMProviderID) (string, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := len(m.health) - 1; i >= 0; i-- {
		if m.health[i][0] == p.String() {
			return m.health[i][1], true
		}
	}
	return "", false
}

var _ Metrics = (*recordingMetrics)(nil)

// allClaudePrimary maps every agent to claude (the v1 ADR-LIC-03 default),
// the per-agent primary map every router test starts from.
func allClaudePrimary() map[model.AgentID]port.LLMProviderID {
	m := make(map[model.AgentID]port.LLMProviderID, 9)
	for _, a := range model.AllAgentIDs() {
		m[a] = port.ProviderClaude
	}
	return m
}

// threeProviders returns the canonical claude/openai/gemini fake set.
func threeProviders() (map[port.LLMProviderID]port.LLMProviderPort, *fakeProvider, *fakeProvider, *fakeProvider) {
	c := newFakeProvider(port.ProviderClaude)
	o := newFakeProvider(port.ProviderOpenAI)
	g := newFakeProvider(port.ProviderGemini)
	return map[port.LLMProviderID]port.LLMProviderPort{
		port.ProviderClaude: c,
		port.ProviderOpenAI: o,
		port.ProviderGemini: g,
	}, c, o, g
}

func defaultTestConfig() RouterConfig {
	return RouterConfig{
		AgentPrimary:  allClaudePrimary(),
		FallbackOrder: []port.LLMProviderID{port.ProviderClaude, port.ProviderOpenAI, port.ProviderGemini},
	}
}

// instantSleep replaces the ctx-aware backoff with a zero-wait that still
// honours ctx cancellation, so retry tests are fast and deterministic
// without losing the "ctx dead during backoff aborts" coverage.
func instantSleep(ctx context.Context, _ time.Duration) error {
	return ctx.Err()
}

func newTestRouter(t testingTB, providers map[port.LLMProviderID]port.LLMProviderPort, cfg RouterConfig, d Deps) *ProviderRouter {
	t.Helper()
	r, err := NewProviderRouter(providers, cfg, d)
	if err != nil {
		t.Fatalf("NewProviderRouter: %v", err)
	}
	return r
}

// testingTB is the minimal subset of *testing.T newTestRouter needs (keeps
// the helper usable from sub-benchmarks too).
type testingTB interface {
	Helper()
	Fatalf(format string, args ...any)
}
