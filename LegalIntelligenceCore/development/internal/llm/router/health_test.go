package router

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"contractpro/legal-intelligence-core/internal/domain/port"
)

// runHealthChecks is the deterministic unit of the background loop; test it
// directly (the loop's only extra logic — the ticker — is covered by
// TestStartStop).
func TestRunHealthChecks_UpdatesRegistry(t *testing.T) {
	t.Parallel()
	provs, claude, openai, gemini := threeProviders()

	// claude: healthy. openai: typed auth failure → permanent.
	// gemini: transport failure → transient after threshold.
	openai.healthFn = func(context.Context, int) (*port.LLMProviderError, error) {
		return port.NewLLMProviderError(port.LLMErrorInvalidAPIKey, nil), nil
	}
	gemini.healthFn = func(context.Context, int) (*port.LLMProviderError, error) {
		return nil, errors.New("connection refused")
	}
	r := newTestRouter(t, provs, defaultTestConfig(), Deps{})

	for i := 0; i < transientUnhealthyThreshold; i++ {
		r.runHealthChecks(context.Background())
	}

	if !r.registry.isHealthy(port.ProviderClaude) {
		t.Fatal("claude probes succeed → healthy")
	}
	if h, _ := r.registry.snapshot(port.ProviderOpenAI); !h.permanent {
		t.Fatal("openai auth failure → permanent")
	}
	if r.registry.isHealthy(port.ProviderGemini) {
		t.Fatal("gemini transport failures past threshold → transient unhealthy")
	}
	// Healthy claude was probed every sweep; permanent openai is probed
	// only until it flips permanent (then shouldProbe skips it).
	if claude.healthCount() != transientUnhealthyThreshold {
		t.Fatalf("claude probes = %d, want %d", claude.healthCount(), transientUnhealthyThreshold)
	}
	if openai.healthCount() != 1 {
		t.Fatalf("openai probes = %d, want 1 (auth-permanent skipped after first)", openai.healthCount())
	}
}

func TestRunHealthChecks_TypedNonAuthIsTransient(t *testing.T) {
	t.Parallel()
	provs, claude, _, _ := threeProviders()
	claude.healthFn = func(context.Context, int) (*port.LLMProviderError, error) {
		return port.NewLLMProviderError(port.LLMErrorServerError, nil), nil
	}
	r := newTestRouter(t, provs, defaultTestConfig(), Deps{})
	for i := 0; i < transientUnhealthyThreshold; i++ {
		r.runHealthChecks(context.Background())
	}
	h, _ := r.registry.snapshot(port.ProviderClaude)
	if h.permanent {
		t.Fatal("a 5xx healthcheck must be transient, never permanent")
	}
	if h.healthy {
		t.Fatal("3 consecutive 5xx healthchecks → transient unhealthy")
	}
}

func TestRunHealthChecks_CtxCancelStopsSweep(t *testing.T) {
	t.Parallel()
	provs, claude, _, _ := threeProviders()
	r := newTestRouter(t, provs, defaultTestConfig(), Deps{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r.runHealthChecks(ctx)
	if claude.healthCount() != 0 {
		t.Fatal("a cancelled ctx must short-circuit the sweep before any probe")
	}
}

// Start/Stop lifecycle: the loop runs at least one sweep on its ticker and
// Stop blocks until the goroutine has fully exited (no leak under -race).
func TestStartStop_RunsAndStopsCleanly(t *testing.T) {
	t.Parallel()
	provs, claude, _, _ := threeProviders()
	probed := make(chan struct{}, 16)
	claude.healthFn = func(context.Context, int) (*port.LLMProviderError, error) {
		select {
		case probed <- struct{}{}:
		default:
		}
		return nil, nil
	}
	cfg := defaultTestConfig()
	cfg.HealthCheckInterval = 5 * time.Millisecond
	cfg.HealthCheckTimeout = 50 * time.Millisecond
	r := newTestRouter(t, provs, cfg, Deps{})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r.Start(ctx)
	r.Start(ctx) // idempotent — must not start a second goroutine

	select {
	case <-probed:
	case <-time.After(2 * time.Second):
		t.Fatal("background loop did not probe within 2s")
	}

	r.Stop()
	r.Stop() // idempotent

	// After Stop the loop is gone; no further probes accrue.
	n := claude.healthCount()
	time.Sleep(20 * time.Millisecond)
	if claude.healthCount() != n {
		t.Fatal("probes continued after Stop — goroutine leak")
	}
}

// S-1: concurrent Start/Stop from many goroutines must be data-race-free
// (two sync.Once established no happens-before with each other; replaced by
// lifeMu). Run under -race; the loop must end up stopped regardless of who
// won the race.
func TestStartStop_ConcurrentRaceClean(t *testing.T) {
	t.Parallel()
	provs, _, _, _ := threeProviders()
	cfg := defaultTestConfig()
	cfg.HealthCheckInterval = time.Millisecond
	r := newTestRouter(t, provs, cfg, Deps{})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(2)
		go func() { defer wg.Done(); r.Start(ctx) }()
		go func() { defer wg.Done(); r.Stop() }()
	}
	wg.Wait()
	// Whoever won, force-stop and confirm a clean, leak-free exit.
	r.Stop()
}

func TestStop_WithoutStartIsNoop(t *testing.T) {
	t.Parallel()
	provs, _, _, _ := threeProviders()
	r := newTestRouter(t, provs, defaultTestConfig(), Deps{})
	r.Stop() // must not panic / must not block
}
