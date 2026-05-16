package cost

import (
	"math"
	"sync"
	"testing"
	"time"

	"contractpro/legal-intelligence-core/internal/domain/model"
	"contractpro/legal-intelligence-core/internal/domain/port"
	"contractpro/legal-intelligence-core/internal/llm/pricing"
)

// usageCall / callCall / unknownCall capture one Recorder invocation so
// tests can assert exact arguments. recordingRecorder is mutex-guarded so
// the concurrency test is -race clean (the real adapter wraps
// concurrency-safe Prometheus vecs; the fake must not introduce its own
// race).
type usageCall struct {
	provider string
	model    string
	agent    string
	input    int
	cached   int
	output   int
	costUSD  float64
	latency  time.Duration
}
type callCall struct{ provider, model, agent, outcome string }
type unknownCall struct{ provider, model string }

type recordingRecorder struct {
	mu      sync.Mutex
	usage   []usageCall
	calls   []callCall
	unknown []unknownCall
}

func (r *recordingRecorder) RecordUsage(p, m, a string, in, ca, out int, usd float64, lat time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.usage = append(r.usage, usageCall{p, m, a, in, ca, out, usd, lat})
}

func (r *recordingRecorder) RecordCall(p, m, a, o string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, callCall{p, m, a, o})
}

func (r *recordingRecorder) UnknownModel(p, m string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.unknown = append(r.unknown, unknownCall{p, m})
}

var _ Recorder = (*recordingRecorder)(nil)

func claudeTable() pricing.Table {
	return pricing.Table{
		"claude-sonnet-4-6": {InputPerMTokenUSD: 3.00, CachedInputPerMTokenUSD: 0.30, OutputPerMTokenUSD: 15.00},
	}
}

func TestNewTracker_Validation(t *testing.T) {
	if _, err := NewTracker(nil, nil); err == nil {
		t.Fatal("NewTracker(nil table) = nil error, want fail-fast error")
	}
	if _, err := NewTracker(pricing.Table{}, nil); err == nil {
		t.Fatal("NewTracker(empty table) = nil error, want fail-fast error")
	}
	// nil rec must degrade to no-op (usable before LIC-TASK-047 wiring).
	tr, err := NewTracker(claudeTable(), nil)
	if err != nil {
		t.Fatalf("NewTracker(valid, nil rec): %v", err)
	}
	if got := tr.ObserveSuccess(Usage{Provider: port.ProviderClaude, Model: "claude-sonnet-4-6"}); got != 0 {
		t.Fatalf("noop tracker cost = %v, want 0 (no tokens)", got)
	}
}

func TestObserveSuccess_RecordsAllFamiliesAndSuccessCall(t *testing.T) {
	rec := &recordingRecorder{}
	tr, err := NewTracker(claudeTable(), rec)
	if err != nil {
		t.Fatalf("NewTracker: %v", err)
	}

	u := Usage{
		Provider:          port.ProviderClaude,
		Model:             "claude-sonnet-4-6",
		Agent:             model.AgentRiskDetection,
		InputTokens:       10_000,
		CachedInputTokens: 0,
		OutputTokens:      1_000,
		Latency:           1234 * time.Millisecond,
	}
	got := tr.ObserveSuccess(u)

	// Acceptance step 2: (10000*3 + 1000*15)/1e6 = 0.045.
	if math.Abs(got-0.045) > 1e-12 {
		t.Fatalf("ObserveSuccess cost = %v, want 0.045", got)
	}
	if len(rec.usage) != 1 {
		t.Fatalf("RecordUsage calls = %d, want 1", len(rec.usage))
	}
	uc := rec.usage[0]
	if uc.provider != "claude" || uc.model != "claude-sonnet-4-6" || uc.agent != "AGENT_RISK_DETECTION" {
		t.Fatalf("usage labels = %+v, want claude/claude-sonnet-4-6/AGENT_RISK_DETECTION", uc)
	}
	if uc.input != 10_000 || uc.cached != 0 || uc.output != 1_000 {
		t.Fatalf("usage tokens = (%d,%d,%d), want (10000,0,1000)", uc.input, uc.cached, uc.output)
	}
	if math.Abs(uc.costUSD-0.045) > 1e-12 {
		t.Fatalf("RecordUsage costUSD = %v, want 0.045", uc.costUSD)
	}
	if uc.latency != 1234*time.Millisecond {
		t.Fatalf("RecordUsage latency = %v, want 1.234s", uc.latency)
	}
	// MF-2: success must also increment lic_llm_calls_total{outcome=success}.
	if len(rec.calls) != 1 || rec.calls[0].outcome != "success" {
		t.Fatalf("calls = %+v, want exactly one {outcome=success}", rec.calls)
	}
	if len(rec.unknown) != 0 {
		t.Fatalf("UnknownModel fired %d times for a known model, want 0", len(rec.unknown))
	}
}

func TestObserveSuccess_UnknownModel(t *testing.T) {
	rec := &recordingRecorder{}
	tr, _ := NewTracker(claudeTable(), rec)

	got := tr.ObserveSuccess(Usage{
		Provider:    port.ProviderOpenAI,
		Model:       "gpt-does-not-exist",
		Agent:       model.AgentSummary,
		InputTokens: 500,
	})
	if got != 0 {
		t.Fatalf("unknown-model cost = %v, want 0.0 (deterministic for span attr)", got)
	}
	if len(rec.unknown) != 1 || rec.unknown[0] != (unknownCall{"openai", "gpt-does-not-exist"}) {
		t.Fatalf("UnknownModel = %+v, want one {openai, gpt-does-not-exist}", rec.unknown)
	}
	// Telemetry is still recorded even with no price (MF-3).
	if len(rec.usage) != 1 || len(rec.calls) != 1 || rec.calls[0].outcome != "success" {
		t.Fatalf("unknown model must still record usage+success call; usage=%d calls=%+v", len(rec.usage), rec.calls)
	}
}

func TestObserveSuccess_NegativeTokensClamped(t *testing.T) {
	rec := &recordingRecorder{}
	tr, _ := NewTracker(claudeTable(), rec)

	got := tr.ObserveSuccess(Usage{
		Provider:          port.ProviderClaude,
		Model:             "claude-sonnet-4-6",
		InputTokens:       -10,
		CachedInputTokens: -10,
		OutputTokens:      -10,
	})
	if got != 0 {
		t.Fatalf("all-negative cost = %v, want 0 (clamped, no panic)", got)
	}
	uc := rec.usage[0]
	if uc.input != 0 || uc.cached != 0 || uc.output != 0 {
		t.Fatalf("RecordUsage got negative tokens %+v, want all clamped to 0 "+
			"(prometheus Counter.Add(<0) panics)", uc)
	}
}

func TestObserveCall_RecordsCallOnly(t *testing.T) {
	rec := &recordingRecorder{}
	tr, _ := NewTracker(claudeTable(), rec)

	tr.ObserveCall(port.ProviderGemini, "gemini-2.5-pro", model.AgentKeyParams, OutcomeFail)

	if len(rec.usage) != 0 || len(rec.unknown) != 0 {
		t.Fatalf("ObserveCall must not record usage/unknown; usage=%d unknown=%d",
			len(rec.usage), len(rec.unknown))
	}
	if len(rec.calls) != 1 {
		t.Fatalf("calls = %d, want 1", len(rec.calls))
	}
	c := rec.calls[0]
	if c != (callCall{"gemini", "gemini-2.5-pro", "AGENT_KEY_PARAMS", "fail"}) {
		t.Fatalf("call = %+v, want {gemini, gemini-2.5-pro, AGENT_KEY_PARAMS, fail}", c)
	}
}

// TestOutcome_WireStringsPinned guards the local mirror against drift from
// the metrics.LLMCallOutcome SSOT (observability.md §3.4).
func TestOutcome_WireStringsPinned(t *testing.T) {
	for o, want := range map[Outcome]string{
		OutcomeSuccess:  "success",
		OutcomeRepair:   "repair",
		OutcomeFail:     "fail",
		OutcomeFallback: "fallback",
	} {
		if string(o) != want {
			t.Fatalf("Outcome %q != %q (SSOT drift vs metrics.LLMCallOutcome)", string(o), want)
		}
		if !o.IsValid() {
			t.Fatalf("%q.IsValid() = false, want true", string(o))
		}
	}
	if Outcome("bogus").IsValid() {
		t.Fatal(`Outcome("bogus").IsValid() = true, want false`)
	}
}

// TestObserveSuccess_Concurrent asserts the shared Tracker is race-free
// under the parallel errgroup agent pipeline (run with -race).
func TestObserveSuccess_Concurrent(t *testing.T) {
	rec := &recordingRecorder{}
	tr, _ := NewTracker(claudeTable(), rec)

	const goroutines, iters = 16, 64
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		go func() {
			defer wg.Done()
			for i := 0; i < iters; i++ {
				tr.ObserveSuccess(Usage{
					Provider:    port.ProviderClaude,
					Model:       "claude-sonnet-4-6",
					Agent:       model.AgentRiskDetection,
					InputTokens: 1000,
				})
				tr.ObserveCall(port.ProviderClaude, "claude-sonnet-4-6", model.AgentRiskDetection, OutcomeRepair)
			}
		}()
	}
	wg.Wait()

	if want := goroutines * iters; len(rec.usage) != want {
		t.Fatalf("RecordUsage count = %d, want %d (lost updates under concurrency)", len(rec.usage), want)
	}
	if want := goroutines * iters * 2; len(rec.calls) != want {
		t.Fatalf("RecordCall count = %d, want %d", len(rec.calls), want)
	}
}
