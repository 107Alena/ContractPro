package schemavalidator_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"contractpro/legal-intelligence-core/internal/agents/schemavalidator"
	"contractpro/legal-intelligence-core/internal/domain/model"
	"contractpro/legal-intelligence-core/internal/domain/port"
)

// fakeRepairer records calls and returns scripted responses/errors.
type fakeRepairer struct {
	mu      sync.Mutex
	calls   int
	gotReq  port.CompletionRequest
	gotProv port.LLMProviderID
	resp    port.CompletionResponse
	err     error
}

func (f *fakeRepairer) CompleteRepair(_ context.Context, req port.CompletionRequest, prov port.LLMProviderID) (port.CompletionResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.gotReq = req
	f.gotProv = prov
	return f.resp, f.err
}

// fakeMetrics records the seam calls.
type fakeMetrics struct {
	mu       sync.Mutex
	attempts [][2]string
	outcomes [][3]string
}

func (m *fakeMetrics) RepairAttempt(agent, prov string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.attempts = append(m.attempts, [2]string{agent, prov})
}

func (m *fakeMetrics) RepairOutcome(agent, prov, outcome string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.outcomes = append(m.outcomes, [3]string{agent, prov, outcome})
}

const objSchema = `{
  "$schema": "http://json-schema.org/draft-07/schema#",
  "title": "T",
  "type": "object",
  "additionalProperties": false,
  "required": ["ok"],
  "properties": {"ok": {"type": "boolean"}}
}`

func newReq() port.CompletionRequest {
	return port.CompletionRequest{
		AgentID:     model.AgentTypeClassifier,
		Model:       "claude-sonnet-4-6",
		System:      "SYSTEM PROMPT",
		User:        "<input>original data</input>",
		PriorTurns:  nil,
		MaxTokens:   2048,
		Temperature: 0.7,
		JSONMode:    true,
	}
}

func primaryWith(content string) port.PrimaryCallResult {
	return port.PrimaryCallResult{
		Response:     port.CompletionResponse{Content: content, ProviderID: port.ProviderClaude},
		UsedProvider: port.ProviderClaude,
	}
}

func TestRun_PrimaryValid_NoRepairMetrics(t *testing.T) {
	rep := &fakeRepairer{}
	mx := &fakeMetrics{}
	rl, err := schemavalidator.NewRepairLoop(rep, mx)
	if err != nil {
		t.Fatalf("NewRepairLoop: %v", err)
	}
	got, err := rl.Run(context.Background(), model.AgentTypeClassifier,
		model.StageAgentTypeClassifier, []byte(objSchema), newReq(),
		primaryWith(`{"ok":true}`))
	if err != nil {
		t.Fatalf("Run() = %v, want nil", err)
	}
	if got.Content != `{"ok":true}` {
		t.Fatalf("Run() returned %q, want primary response unchanged", got.Content)
	}
	if rep.calls != 0 {
		t.Fatalf("CompleteRepair called %d times on a valid primary, want 0", rep.calls)
	}
	// MF-4: a passing primary issues NO repair turn ⇒ ZERO repair metrics.
	if len(mx.attempts) != 0 || len(mx.outcomes) != 0 {
		t.Fatalf("repair metrics emitted on happy path: attempts=%v outcomes=%v", mx.attempts, mx.outcomes)
	}
}

func TestRun_RepairSucceeds_RepairedOK(t *testing.T) {
	rep := &fakeRepairer{resp: port.CompletionResponse{Content: `{"ok":true}`, ProviderID: port.ProviderClaude}}
	mx := &fakeMetrics{}
	rl, _ := schemavalidator.NewRepairLoop(rep, mx)

	got, err := rl.Run(context.Background(), model.AgentTypeClassifier,
		model.StageAgentTypeClassifier, []byte(objSchema), newReq(),
		primaryWith(`{"ok":"yes"}`)) // type mismatch → repair
	if err != nil {
		t.Fatalf("Run() = %v, want nil (repaired)", err)
	}
	if got.Content != `{"ok":true}` {
		t.Fatalf("Run() = %q, want repaired content", got.Content)
	}
	if rep.calls != 1 {
		t.Fatalf("CompleteRepair calls = %d, want exactly 1 (hard limit N=1)", rep.calls)
	}
	wantAttempt := [2]string{"AGENT_TYPE_CLASSIFIER", "claude"}
	if len(mx.attempts) != 1 || mx.attempts[0] != wantAttempt {
		t.Fatalf("attempts = %v, want [%v]", mx.attempts, wantAttempt)
	}
	wantOutcome := [3]string{"AGENT_TYPE_CLASSIFIER", "claude", "repaired_ok"}
	if len(mx.outcomes) != 1 || mx.outcomes[0] != wantOutcome {
		t.Fatalf("outcomes = %v, want [%v]", mx.outcomes, wantOutcome)
	}
}

func TestRun_SecondInvalid_AgentOutputInvalid(t *testing.T) {
	// Repair returns still-invalid JSON → repair_failed, AGENT_OUTPUT_INVALID.
	rep := &fakeRepairer{resp: port.CompletionResponse{Content: `{"ok":42}`}}
	mx := &fakeMetrics{}
	rl, _ := schemavalidator.NewRepairLoop(rep, mx)

	_, err := rl.Run(context.Background(), model.AgentRiskDetection,
		model.StageAgentRiskDetection, []byte(objSchema), newReq(),
		primaryWith(`{"ok":"no"}`))
	if err == nil {
		t.Fatal("Run() = nil, want AGENT_OUTPUT_INVALID")
	}
	de, ok := model.AsDomainError(err)
	if !ok {
		t.Fatalf("Run() error = %T, want *model.DomainError", err)
	}
	if de.Code != model.ErrCodeAgentOutputInvalid {
		t.Fatalf("code = %s, want AGENT_OUTPUT_INVALID", de.Code)
	}
	if !de.Retryable {
		t.Fatal("AGENT_OUTPUT_INVALID must be is_retryable=true")
	}
	if de.Stage != model.StageAgentRiskDetection {
		t.Fatalf("stage = %s, want STAGE_AGENT_RISK_DETECTION", de.Stage)
	}
	if rep.calls != 1 {
		t.Fatalf("CompleteRepair calls = %d, want 1 (no second repair)", rep.calls)
	}
	if len(mx.outcomes) != 1 || mx.outcomes[0][2] != "repair_failed" {
		t.Fatalf("outcomes = %v, want one repair_failed", mx.outcomes)
	}
}

func TestRun_RepairProviderError_Escalates(t *testing.T) {
	provErr := &port.LLMProviderError{Code: port.LLMErrorServerError, Retryable: false, FallbackEligible: false}
	rep := &fakeRepairer{err: provErr}
	mx := &fakeMetrics{}
	rl, _ := schemavalidator.NewRepairLoop(rep, mx)

	_, err := rl.Run(context.Background(), model.AgentSummary,
		model.StageAgentSummary, []byte(objSchema), newReq(),
		primaryWith(`{"ok":"bad"}`))
	de, ok := model.AsDomainError(err)
	if !ok {
		t.Fatalf("Run() error = %T, want *model.DomainError", err)
	}
	if de.Code != model.ErrCodeAgentOutputInvalid || !de.Retryable {
		t.Fatalf("got code=%s retryable=%v, want AGENT_OUTPUT_INVALID/true", de.Code, de.Retryable)
	}
	// MF-2: provider error ⇒ repair_provider_error (NOT repair_failed),
	// and the provider error is preserved in the cause chain.
	if !errors.Is(err, provErr) {
		t.Fatal("provider error must be wrapped as the DomainError cause")
	}
	if len(mx.outcomes) != 1 || mx.outcomes[0][2] != "repair_provider_error" {
		t.Fatalf("outcomes = %v, want one repair_provider_error", mx.outcomes)
	}
}

func TestRun_BrokenSchema_InternalError_NoRepair(t *testing.T) {
	rep := &fakeRepairer{}
	mx := &fakeMetrics{}
	rl, _ := schemavalidator.NewRepairLoop(rep, mx)

	broken := []byte(`{"$schema":"http://json-schema.org/draft-07/schema#","type":"banana"}`)
	_, err := rl.Run(context.Background(), model.AgentKeyParams,
		model.StageAgentKeyParams, broken, newReq(), primaryWith(`{"x":1}`))
	de, ok := model.AsDomainError(err)
	if !ok {
		t.Fatalf("Run() error = %T, want *model.DomainError", err)
	}
	// MF-3: a broken embedded schema is a LIC defect → INTERNAL_ERROR,
	// retryable, and NEVER repaired.
	if de.Code != model.ErrCodeInternal {
		t.Fatalf("code = %s, want INTERNAL_ERROR", de.Code)
	}
	if !de.Retryable {
		t.Fatal("INTERNAL_ERROR from a broken schema must be is_retryable=true")
	}
	if rep.calls != 0 {
		t.Fatalf("CompleteRepair called %d times on a broken schema, want 0", rep.calls)
	}
	if len(mx.attempts) != 0 || len(mx.outcomes) != 0 {
		t.Fatalf("a broken schema must touch no repair metrics: %v %v", mx.attempts, mx.outcomes)
	}
}

// TestRun_RepairRequest_PriorTurnsAndDelta pins the MF-1 SSOT reconciliation:
// the repair request carries [..origPriors, {User,origUser},
// {Assistant,invalid}], User=repair_prompt, Temperature=0.0, and differs from
// the original in EXACTLY those three fields (§5.3).
func TestRun_RepairRequest_PriorTurnsAndDelta(t *testing.T) {
	rep := &fakeRepairer{resp: port.CompletionResponse{Content: `{"ok":true}`}}
	rl, _ := schemavalidator.NewRepairLoop(rep, &fakeMetrics{})

	orig := newReq()
	orig.PriorTurns = []port.Turn{{Role: port.RoleUser, Content: "earlier"}}
	invalid := `{"ok":"not-bool"}`

	if _, err := rl.Run(context.Background(), model.AgentTypeClassifier,
		model.StageAgentTypeClassifier, []byte(objSchema), orig, primaryWith(invalid)); err != nil {
		t.Fatalf("Run() = %v", err)
	}

	got := rep.gotReq
	wantPrior := []port.Turn{
		{Role: port.RoleUser, Content: "earlier"},
		{Role: port.RoleUser, Content: orig.User},
		{Role: port.RoleAssistant, Content: invalid},
	}
	if len(got.PriorTurns) != len(wantPrior) {
		t.Fatalf("PriorTurns len = %d, want %d (%v)", len(got.PriorTurns), len(wantPrior), got.PriorTurns)
	}
	for i := range wantPrior {
		if got.PriorTurns[i] != wantPrior[i] {
			t.Fatalf("PriorTurns[%d] = %+v, want %+v", i, got.PriorTurns[i], wantPrior[i])
		}
	}
	if got.Temperature != 0.0 {
		t.Fatalf("repair Temperature = %v, want 0.0 (§5.3 determinism)", got.Temperature)
	}
	// Delta check: everything except PriorTurns/User/Temperature is unchanged.
	normalized := got
	normalized.PriorTurns = orig.PriorTurns
	normalized.User = orig.User
	normalized.Temperature = orig.Temperature
	if normalized.AgentID != orig.AgentID || normalized.Model != orig.Model ||
		normalized.System != orig.System || normalized.MaxTokens != orig.MaxTokens ||
		normalized.JSONMode != orig.JSONMode {
		t.Fatalf("repair request changed fields outside {PriorTurns,User,Temperature}:\n got=%+v\norig=%+v", got, orig)
	}
	if rep.gotProv != port.ProviderClaude {
		t.Fatalf("repair issued on %q, want sticky %q", rep.gotProv, port.ProviderClaude)
	}
}

func TestNewRepairLoop_NilRepairer_FailsFast(t *testing.T) {
	if _, err := schemavalidator.NewRepairLoop(nil, &fakeMetrics{}); err == nil {
		t.Fatal("NewRepairLoop(nil, …) = nil error, want fail-fast")
	}
}

func TestNewRepairLoop_NilMetrics_NoOp(t *testing.T) {
	rep := &fakeRepairer{resp: port.CompletionResponse{Content: `{"ok":true}`}}
	rl, err := schemavalidator.NewRepairLoop(rep, nil) // nil metrics ⇒ noop
	if err != nil {
		t.Fatalf("NewRepairLoop(_, nil) = %v, want nil (noop metrics)", err)
	}
	if _, err := rl.Run(context.Background(), model.AgentTypeClassifier,
		model.StageAgentTypeClassifier, []byte(objSchema), newReq(),
		primaryWith(`{"ok":"x"}`)); err != nil {
		t.Fatalf("Run with noop metrics = %v, want nil", err)
	}
}

// TestRun_Concurrent — one RepairLoop shared by the parallel errgroup
// pipeline (-race).
func TestRun_Concurrent(t *testing.T) {
	rep := &fakeRepairer{resp: port.CompletionResponse{Content: `{"ok":true}`}}
	rl, _ := schemavalidator.NewRepairLoop(rep, &fakeMetrics{})
	const g = 16
	done := make(chan struct{})
	for i := 0; i < g; i++ {
		go func() {
			defer func() { done <- struct{}{} }()
			for j := 0; j < 32; j++ {
				_, _ = rl.Run(context.Background(), model.AgentTypeClassifier,
					model.StageAgentTypeClassifier, []byte(objSchema), newReq(),
					primaryWith(`{"ok":true}`))
				_, _ = rl.Run(context.Background(), model.AgentTypeClassifier,
					model.StageAgentTypeClassifier, []byte(objSchema), newReq(),
					primaryWith(`{"ok":"bad"}`))
			}
		}()
	}
	for i := 0; i < g; i++ {
		<-done
	}
}
