package fakes

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"contractpro/legal-intelligence-core/internal/domain/model"
	"contractpro/legal-intelligence-core/internal/domain/port"
)

// FakeLLMProvider is the test double for port.LLMProviderPort. It is
// keyed by provider ID + every Complete call FIFO-drains the response
// queue installed for (AgentID, Model). The fake supports deterministic
// errors (per-call, FIFO), latency simulation, and a passthrough of
// JSONSchema strict mode — when req.JSONSchema is non-empty the fake
// records that fact on the recorded call so tests can assert "router
// requested structured outputs" without inspecting raw bytes.
//
// Goroutine-safe. The error and response queues are independent: an
// installed error wins over a queued response (the queued response is
// NOT consumed). Use ResponseSetSequence or SetResponseJSON to install
// canned outputs.
//
// NOTE on JSONSchema simulation. Real providers (Claude tool_use, OpenAI
// response_format=json_schema, Gemini responseSchema) gate the WIRE
// output against the schema. The fake does NOT validate: it returns
// req.User contents in CompletionResponse.Content verbatim from the
// installed canned response. The Schema Validator (LIC-TASK-023) gates
// the fake's output the same way it gates a real provider — which is
// exactly the contract the integration tests want to exercise.
type FakeLLMProvider struct {
	id port.LLMProviderID

	mu        sync.Mutex
	responses map[fakeKey][]CompletionScript // FIFO per key
	errors    map[fakeKey][]error            // FIFO per key
	calls     []FakeLLMCall                  // append-only record
	healthErr error                          // current HealthCheck result; nil ⇒ healthy
	healthTyped *port.LLMProviderError      // for typed-error variant
	latency   time.Duration                  // optional, applied per Complete

	// Default response if no script is installed for the key. nil ⇒
	// Complete returns an LLMErrorMalformedRequest "no canned response".
	defaultResponse *CompletionScript
}

type fakeKey struct {
	id    model.AgentID
	model string
}

// CompletionScript is one canned response. Set Content (raw string) and,
// optionally, the token-usage numbers + StopReason. Latency is per-call
// (added to the provider-wide latency).
type CompletionScript struct {
	Content           string
	InputTokens       int
	CachedInputTokens int
	OutputTokens      int
	StopReason        port.StopReason
	Latency           time.Duration // optional per-call latency on top of provider-wide
}

// FakeLLMCall captures one observed Complete invocation. Used by tests
// asserting "router routed agent X to provider Y with the right
// (model, jsonSchemaSet) pair".
type FakeLLMCall struct {
	AgentID       model.AgentID
	Model         string
	System        string
	User          string
	PriorTurnsLen int
	MaxTokens     int
	Temperature   float64
	JSONMode      bool
	JSONSchemaSet bool
	At            time.Time
}

// NewFakeLLMProvider builds a fake adapter for one provider ID
// (Claude / OpenAI / Gemini). The provider ID is what flows into
// router/agents wiring; the agent/model pair is the FIFO key for
// installed responses.
func NewFakeLLMProvider(id port.LLMProviderID) *FakeLLMProvider {
	return &FakeLLMProvider{
		id:        id,
		responses: make(map[fakeKey][]CompletionScript),
		errors:    make(map[fakeKey][]error),
	}
}

// ID returns the configured provider id.
func (p *FakeLLMProvider) ID() port.LLMProviderID { return p.id }

// SetResponse installs one canned response for the (agentID, model) pair.
// FIFO: a subsequent SetResponse appends after; SetResponses replaces.
func (p *FakeLLMProvider) SetResponse(agent model.AgentID, modelID string, r CompletionScript) {
	p.mu.Lock()
	defer p.mu.Unlock()
	k := fakeKey{id: agent, model: modelID}
	p.responses[k] = append(p.responses[k], r)
}

// SetResponseJSON is a convenience wrapper that installs a strict-JSON
// content body with reasonable usage defaults (end_turn, small token
// counts). The caller passes a pre-rendered JSON string — typically a
// fixture from fixtures.go.
func (p *FakeLLMProvider) SetResponseJSON(agent model.AgentID, modelID, jsonContent string) {
	p.SetResponse(agent, modelID, CompletionScript{
		Content:      jsonContent,
		InputTokens:  100,
		OutputTokens: len(jsonContent) / 4, // rough estimate; tests assert ranges, not exact
		StopReason:   port.StopReasonEndTurn,
	})
}

// SetResponses replaces the FIFO queue for (agent, model) with rs.
func (p *FakeLLMProvider) SetResponses(agent model.AgentID, modelID string, rs []CompletionScript) {
	p.mu.Lock()
	defer p.mu.Unlock()
	k := fakeKey{id: agent, model: modelID}
	cp := make([]CompletionScript, len(rs))
	copy(cp, rs)
	p.responses[k] = cp
}

// SetDefaultResponse installs the response Complete returns when no
// per-(agent, model) script is queued. Useful for tests that exercise
// the router without caring about per-agent output (the schema validator
// then rejects it as expected, or the test mocks downstream).
func (p *FakeLLMProvider) SetDefaultResponse(r CompletionScript) {
	p.mu.Lock()
	defer p.mu.Unlock()
	cp := r
	p.defaultResponse = &cp
}

// InjectError queues an error for the next Complete call against
// (agent, model). Errors win over responses; the queued response is NOT
// consumed. FIFO.
func (p *FakeLLMProvider) InjectError(agent model.AgentID, modelID string, err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	k := fakeKey{id: agent, model: modelID}
	p.errors[k] = append(p.errors[k], err)
}

// SetLatency configures provider-wide per-call latency. Each Complete
// blocks for this duration BEFORE returning. Use small values (≤10ms)
// to avoid flakiness; tests that need precise timing should drive
// scenarios through the router's own ctx deadlines.
func (p *FakeLLMProvider) SetLatency(d time.Duration) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.latency = d
}

// SetHealth installs the next HealthCheck result. typed!=nil simulates
// a wire-call-completed-but-provider-reported-failure (401/429/content
// policy); transport!=nil simulates a never-reached-the-provider error
// (DNS/TLS/transport).
//
// Pass (nil, nil) to make HealthCheck return (nil, nil) (healthy). The
// state persists across calls until overwritten.
func (p *FakeLLMProvider) SetHealth(typed *port.LLMProviderError, transport error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.healthTyped = typed
	p.healthErr = transport
}

// Calls returns a snapshot of every recorded Complete invocation.
func (p *FakeLLMProvider) Calls() []FakeLLMCall {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]FakeLLMCall, len(p.calls))
	copy(out, p.calls)
	return out
}

// Reset clears queued responses, queued errors, and the call record.
// Health and latency are unchanged.
func (p *FakeLLMProvider) Reset() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.responses = make(map[fakeKey][]CompletionScript)
	p.errors = make(map[fakeKey][]error)
	p.calls = nil
}

// Complete is the port.LLMProviderPort implementation.
//
// Order of operations:
//  1. Honor ctx (raw return on already-done).
//  2. Apply provider-wide latency (sleep, ctx-aware).
//  3. Record the call.
//  4. Dequeue an error (wins over response) ⇒ return it.
//  5. Dequeue a response (FIFO per (agent, model)) ⇒ return it.
//  6. Use default response if set.
//  7. Otherwise return *LLMProviderError{LLMErrorMalformedRequest} so
//     a misconfigured test fails clearly, not with a zero response.
func (p *FakeLLMProvider) Complete(ctx context.Context, req port.CompletionRequest) (port.CompletionResponse, error) {
	if err := ctx.Err(); err != nil {
		return port.CompletionResponse{}, err
	}

	p.mu.Lock()
	latency := p.latency
	p.mu.Unlock()

	if latency > 0 {
		select {
		case <-ctx.Done():
			return port.CompletionResponse{}, ctx.Err()
		case <-time.After(latency):
		}
	}

	call := FakeLLMCall{
		AgentID:       req.AgentID,
		Model:         req.Model,
		System:        req.System,
		User:          req.User,
		PriorTurnsLen: len(req.PriorTurns),
		MaxTokens:     req.MaxTokens,
		Temperature:   req.Temperature,
		JSONMode:      req.JSONMode,
		JSONSchemaSet: len(req.JSONSchema) > 0,
		At:            time.Now(),
	}

	p.mu.Lock()
	p.calls = append(p.calls, call)
	k := fakeKey{id: req.AgentID, model: req.Model}

	// 4. Error wins.
	if errs := p.errors[k]; len(errs) > 0 {
		err := errs[0]
		p.errors[k] = errs[1:]
		p.mu.Unlock()
		// Wrap a non-typed error so the Router's typed-error invariant
		// holds (every non-nil error MUST unwrap to *LLMProviderError).
		return port.CompletionResponse{}, p.ensureTyped(err)
	}

	// 5. FIFO canned response.
	if rs := p.responses[k]; len(rs) > 0 {
		r := rs[0]
		p.responses[k] = rs[1:]
		p.mu.Unlock()
		// Honour per-call latency, ctx-aware.
		if r.Latency > 0 {
			select {
			case <-ctx.Done():
				return port.CompletionResponse{}, ctx.Err()
			case <-time.After(r.Latency):
			}
		}
		return p.buildResponse(r, req), nil
	}

	// 6. Default.
	if p.defaultResponse != nil {
		r := *p.defaultResponse
		p.mu.Unlock()
		return p.buildResponse(r, req), nil
	}

	p.mu.Unlock()
	return port.CompletionResponse{}, port.NewLLMProviderError(
		port.LLMErrorMalformedRequest,
		fmt.Errorf("fakes: no canned response for agent=%s model=%s",
			req.AgentID, req.Model),
	)
}

func (p *FakeLLMProvider) buildResponse(r CompletionScript, req port.CompletionRequest) port.CompletionResponse {
	sr := r.StopReason
	if sr == "" {
		sr = port.StopReasonEndTurn
	}
	return port.CompletionResponse{
		Content:           r.Content,
		InputTokens:       r.InputTokens,
		CachedInputTokens: r.CachedInputTokens,
		OutputTokens:      r.OutputTokens,
		StopReason:        sr,
		LatencyMs:         r.Latency.Milliseconds(),
		ProviderID:        p.id,
		Model:             req.Model,
	}
}

// ensureTyped guarantees the returned error is (or wraps) a
// *port.LLMProviderError so the Router's invariant holds.
func (p *FakeLLMProvider) ensureTyped(err error) error {
	if err == nil {
		return nil
	}
	var lerr *port.LLMProviderError
	if errors.As(err, &lerr) {
		return err
	}
	return port.NewLLMProviderError(port.LLMErrorNetwork, err)
}

// HealthCheck returns the current health state.
//
//   - (typed, nil)        — wire call completed; provider reported a
//                            classified failure (router marks unhealthy
//                            per §1.3).
//   - (nil, transport)    — never reached the provider (DNS/TLS).
//   - (nil, nil)          — healthy.
func (p *FakeLLMProvider) HealthCheck(ctx context.Context) (*port.LLMProviderError, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.healthTyped, p.healthErr
}

// Pending returns the queued response count for (agent, model). Test
// helper, used to assert the FIFO drained.
func (p *FakeLLMProvider) Pending(agent model.AgentID, modelID string) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.responses[fakeKey{id: agent, model: modelID}])
}

// Compile-time assertion against the port. The cross-package check is in
// seams_check_test.go.
var _ port.LLMProviderPort = (*FakeLLMProvider)(nil)
