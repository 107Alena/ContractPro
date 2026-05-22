package app

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"contractpro/legal-intelligence-core/internal/config"
	"contractpro/legal-intelligence-core/internal/domain/model"
	"contractpro/legal-intelligence-core/internal/infra/observability/logger"
)

// Test_New_NilConfig verifies fail-fast on nil config (no panic, typed error).
func Test_New_NilConfig(t *testing.T) {
	_, err := New(context.Background(), nil, BuildInfo{})
	if err == nil {
		t.Fatalf("expected error for nil config, got nil")
	}
	if !strings.Contains(err.Error(), "config must not be nil") {
		t.Fatalf("expected nil-config error, got %v", err)
	}
}

// fakeResumer captures invocations for lazyResumer forwarding tests.
type fakeResumer struct {
	mu     sync.Mutex
	calls  int
	state  *model.PipelineState
	retErr error
}

func (f *fakeResumer) ResumeAfterConfirmation(_ context.Context, s *model.PipelineState) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.state = s
	return f.retErr
}

// Test_LazyResumer_BeforeSet asserts a pre-wire call returns a clear error.
func Test_LazyResumer_BeforeSet(t *testing.T) {
	l := &lazyResumer{}
	err := l.ResumeAfterConfirmation(context.Background(), &model.PipelineState{})
	if err == nil {
		t.Fatalf("expected error from unwired resumer")
	}
	if !strings.Contains(err.Error(), "not yet wired") {
		t.Fatalf("expected 'not yet wired' marker in error, got %v", err)
	}
}

// Test_LazyResumer_ForwardsAfterSet verifies forwarding of state and error.
func Test_LazyResumer_ForwardsAfterSet(t *testing.T) {
	target := &fakeResumer{retErr: errors.New("downstream failure")}
	l := &lazyResumer{}
	l.set(target)

	state := &model.PipelineState{JobID: "job-1", VersionID: "v-1"}
	err := l.ResumeAfterConfirmation(context.Background(), state)
	if !errors.Is(err, target.retErr) {
		t.Fatalf("expected downstream error propagated, got %v", err)
	}
	if target.calls != 1 {
		t.Fatalf("expected 1 forwarded call, got %d", target.calls)
	}
	if target.state != state {
		t.Fatalf("state pointer not propagated through lazyResumer")
	}
}

// Test_LazyResumer_ConcurrentSetAndCall — race-safety smoke test.
// Run under `go test -race` (CI default).
func Test_LazyResumer_ConcurrentSetAndCall(t *testing.T) {
	l := &lazyResumer{}
	target := &fakeResumer{}

	const goroutines = 64
	var wg sync.WaitGroup
	wg.Add(goroutines * 2)

	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			l.set(target)
		}()
		go func() {
			defer wg.Done()
			_ = l.ResumeAfterConfirmation(context.Background(), &model.PipelineState{})
		}()
	}
	wg.Wait()

	if err := l.ResumeAfterConfirmation(context.Background(), &model.PipelineState{}); err != nil {
		t.Fatalf("post-race forward failed: %v", err)
	}
}

// Test_MapAgentID covers the full 9-agent mapping plus the unknown fallthrough.
func Test_MapAgentID(t *testing.T) {
	cases := []struct {
		in   string
		want model.AgentID
	}{
		{config.AgentTypeClassifier, model.AgentTypeClassifier},
		{config.AgentKeyParams, model.AgentKeyParams},
		{config.AgentPartyConsistency, model.AgentPartyConsistency},
		{config.AgentMandatoryConditions, model.AgentMandatoryConditions},
		{config.AgentRiskDetection, model.AgentRiskDetection},
		{config.AgentRecommendation, model.AgentRecommendation},
		{config.AgentSummary, model.AgentSummary},
		{config.AgentDetailedReport, model.AgentDetailedReport},
		{config.AgentRiskDelta, model.AgentRiskDelta},
		{"UNKNOWN_AGENT", model.AgentID("UNKNOWN_AGENT")},
	}
	for _, tc := range cases {
		if got := mapAgentID(tc.in); got != tc.want {
			t.Errorf("mapAgentID(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// Test_AgentModelForProvider verifies the agent → model resolution.
func Test_AgentModelForProvider(t *testing.T) {
	a := &App{cfg: &config.Config{
		LLM: config.LLMConfig{
			Claude: config.ClaudeProviderConfig{Model: "claude-sonnet-4-6"},
			OpenAI: config.OpenAIProviderConfig{Model: "gpt-4.1"},
			Gemini: config.GeminiProviderConfig{Model: "gemini-2.5"},
		},
		Agents: config.AgentsConfig{
			Providers: map[string]string{
				config.AgentTypeClassifier: config.ProviderClaude,
				config.AgentRiskDetection:  config.ProviderOpenAI,
				config.AgentSummary:        config.ProviderGemini,
				config.AgentRiskDelta:      "weird",
			},
		},
	}}

	tests := []struct {
		agent string
		want  string
	}{
		{config.AgentTypeClassifier, "claude-sonnet-4-6"},
		{config.AgentRiskDetection, "gpt-4.1"},
		{config.AgentSummary, "gemini-2.5"},
		{config.AgentRiskDelta, "claude-sonnet-4-6"}, // unknown provider → claude default
	}
	for _, tc := range tests {
		if got := a.agentModelForProvider(tc.agent); got != tc.want {
			t.Errorf("agentModelForProvider(%q) = %q, want %q", tc.agent, got, tc.want)
		}
	}
}

// Test_ReloadSecrets_Reentrancy asserts the reload guard rejects concurrent
// invocations so a flood of SIGHUPs cannot interleave rotations.
func Test_ReloadSecrets_Reentrancy(t *testing.T) {
	reloadGuard.Store(false)
	defer reloadGuard.Store(false)

	a := newAppForLoggerTest(t)

	// Simulate an in-flight rotation by setting the guard manually; the next
	// call must reject.
	if !reloadGuard.CompareAndSwap(false, true) {
		t.Fatalf("guard precondition failed")
	}
	err := a.ReloadSecrets(context.Background())
	if err == nil {
		t.Fatalf("expected reentrancy error while guard held, got nil")
	}
	if !strings.Contains(err.Error(), "already in progress") {
		t.Fatalf("expected 'already in progress' marker, got %v", err)
	}

	reloadGuard.Store(false)
	if err := a.ReloadSecrets(context.Background()); err != nil {
		t.Fatalf("post-release ReloadSecrets failed: %v", err)
	}
}

// Test_ReloadSecrets_GuardReleasedOnReturn confirms the guard returns to
// `false` after each call so subsequent rotations can proceed.
func Test_ReloadSecrets_GuardReleasedOnReturn(t *testing.T) {
	reloadGuard.Store(false)
	defer reloadGuard.Store(false)
	a := newAppForLoggerTest(t)

	for i := 0; i < 5; i++ {
		if err := a.ReloadSecrets(context.Background()); err != nil {
			t.Fatalf("iter %d: ReloadSecrets failed: %v", i, err)
		}
	}
	if reloadGuard.Load() {
		t.Fatalf("reload guard not released on return")
	}
}

// Test_VersionMetaPayload_JSON verifies parent_version_id round-trips through
// the wire shape used by both the ingress writer and the pipeline reader.
func Test_VersionMetaPayload_JSON(t *testing.T) {
	parent := "parent-v-42"
	for _, p := range []*string{&parent, nil} {
		payload := versionMetaPayload{ParentVersionID: p, OriginType: "supply-contract"}
		raw, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		var back versionMetaPayload
		if err := json.Unmarshal(raw, &back); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if (back.ParentVersionID == nil) != (p == nil) {
			t.Fatalf("parent_version_id nil-ness changed: got %v", back.ParentVersionID)
		}
		if p != nil && *back.ParentVersionID != *p {
			t.Fatalf("parent_version_id mismatch: got %q want %q", *back.ParentVersionID, *p)
		}
		if back.OriginType != payload.OriginType {
			t.Fatalf("origin_type mismatch: got %q want %q", back.OriginType, payload.OriginType)
		}
	}
}

// Test_PendingStateStore_EmptyArgs covers the input-validation arms that do
// not touch Redis — Save with nil, Save/Load/Delete with empty versionID.
func Test_PendingStateStore_EmptyArgs(t *testing.T) {
	s := &pendingStateStore{kv: nil}
	ctx := context.Background()

	if err := s.Save(ctx, "v1", nil, 0); err == nil || !strings.Contains(err.Error(), "must not be nil") {
		t.Fatalf("Save(nil state): expected nil-state error, got %v", err)
	}
	if err := s.Save(ctx, "", &model.PendingTypeConfirmation{}, 0); err == nil ||
		!strings.Contains(err.Error(), "must not be empty") {
		t.Fatalf("Save(empty versionID): expected empty-versionID error, got %v", err)
	}
	if _, err := s.Load(ctx, ""); err == nil || !strings.Contains(err.Error(), "must not be empty") {
		t.Fatalf("Load(empty versionID): expected error, got %v", err)
	}
	if err := s.Delete(ctx, ""); err == nil || !strings.Contains(err.Error(), "must not be empty") {
		t.Fatalf("Delete(empty versionID): expected error, got %v", err)
	}
}

// Test_VersionMetaCache_EmptyID covers the input-validation arms.
func Test_VersionMetaCache_EmptyID(t *testing.T) {
	c := &versionMetaCache{kv: nil}
	ctx := context.Background()

	if err := c.Set(ctx, "", []byte("{}"), 0); err == nil ||
		!strings.Contains(err.Error(), "must not be empty") {
		t.Fatalf("Set(empty versionID): expected error, got %v", err)
	}
	// GetParentVersionID with empty versionID is a quiet (nil, nil) per §8.3.
	ptr, err := c.GetParentVersionID(ctx, "")
	if err != nil || ptr != nil {
		t.Fatalf("GetParentVersionID(empty): expected (nil, nil), got (%v, %v)", ptr, err)
	}
}

// newAppForLoggerTest builds the minimum *App needed by ReloadSecrets — a real
// *logger.Logger from logger.New with a benign config (no env-var dependence).
func newAppForLoggerTest(t *testing.T) *App {
	t.Helper()
	lg, err := logger.New(config.AppConfig{LogLevel: "info", Env: config.EnvLocal})
	if err != nil {
		t.Fatalf("logger.New: %v", err)
	}
	return &App{cfg: &config.Config{}, log: lg.With("test")}
}
