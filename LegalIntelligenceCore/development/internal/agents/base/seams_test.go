package base

import (
	"context"
	"testing"

	"contractpro/legal-intelligence-core/internal/domain/port"
	"contractpro/legal-intelligence-core/internal/infra/observability/metrics"
)

// TestOutcome_WireStringsPinned pins the local Outcome mirror against the
// metrics.AgentInvocationOutcome SSOT (observability.md §3.3) so the hermetic
// mirror cannot silently drift from the value the LIC-TASK-047 adapter feeds
// to lic_agent_invocations_total — identical guard to router.CallOutcome /
// schemavalidator.RepairOutcome (code-architect S3). This is the ONLY file
// that imports metrics, and it is a _test file, so package hermeticity holds.
func TestOutcome_WireStringsPinned(t *testing.T) {
	pairs := []struct {
		local Outcome
		ssot  metrics.AgentInvocationOutcome
	}{
		{OutcomeSuccess, metrics.AgentOutcomeSuccess},
		{OutcomeRepairSuccess, metrics.AgentOutcomeRepairSuccess},
		{OutcomeInvalidOutput, metrics.AgentOutcomeInvalidOutput},
		{OutcomeProviderError, metrics.AgentOutcomeProviderError},
		{OutcomeTimeout, metrics.AgentOutcomeTimeout},
	}
	for _, p := range pairs {
		if p.local.String() != string(p.ssot) {
			t.Fatalf("Outcome %q drifted from metrics SSOT %q", p.local, p.ssot)
		}
		if !p.local.IsValid() {
			t.Fatalf("Outcome %q not IsValid", p.local)
		}
	}
	if Outcome("nope").IsValid() {
		t.Fatalf("unknown outcome reported valid")
	}
}

func TestPassthroughEstimator(t *testing.T) {
	var e passthroughEstimator
	// System(2) + User(4) + PriorTurns content(2) = 8 runes ⇒ ⌈8/4⌉ = 2.
	got, over := e.Fit(port.CompletionRequest{
		System:     "ab",
		User:       "cdef",
		PriorTurns: []port.Turn{{Role: port.RoleUser, Content: "gh"}},
	})
	if got != 2 || over {
		t.Fatalf("Fit = (%d,%v), want (2,false)", got, over)
	}
	// Contract: never reports over-budget (size verdict delegated to provider).
	if _, ob := e.Fit(port.CompletionRequest{User: string(make([]rune, 1_000_000))}); ob {
		t.Fatalf("passthrough must never report over-budget")
	}
}

func TestNoopSeams_DoNotPanic(t *testing.T) {
	var m Metrics = noopMetrics{}
	m.Invocation("a", "success")
	m.Duration("a", 1)
	m.InputTokens("a", 1)
	m.OutputTokens("a", 1)

	var tr Tracer = noopTracer{}
	ctx, as := tr.StartAgent(context.Background(), AgentSpanInput{AgentID: "a"})
	if ctx == nil || as == nil {
		t.Fatal("noop StartAgent returned nil")
	}
	lctx, ls := as.StartLLMCall(ctx)
	if lctx == nil || ls == nil {
		t.Fatal("noop StartLLMCall returned nil")
	}
	ls.Finish(LLMSpanOutput{})
	as.Finish(AgentSpanOutput{}, nil)
}
