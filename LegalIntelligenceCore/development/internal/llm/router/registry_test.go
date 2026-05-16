package router

import (
	"sync"
	"testing"
	"time"

	"contractpro/legal-intelligence-core/internal/domain/port"
)

func newTestRegistry(now func() time.Time) (*healthRegistry, *recordingMetrics) {
	mx := &recordingMetrics{}
	reg := newHealthRegistry([]port.LLMProviderID{port.ProviderClaude, port.ProviderOpenAI}, mx, now)
	return reg, mx
}

func TestRegistry_SeedsHealthy(t *testing.T) {
	t.Parallel()
	reg, mx := newTestRegistry(time.Now)
	if !reg.isHealthy(port.ProviderClaude) {
		t.Fatal("providers must start healthy (optimistic, §2.3)")
	}
	// initial healthy gauge emitted for every seeded provider.
	if len(mx.health) != 2 {
		t.Fatalf("seed health emissions = %d, want 2", len(mx.health))
	}
}

func TestRegistry_TransientThreshold(t *testing.T) {
	t.Parallel()
	reg, mx := newTestRegistry(time.Now)
	srv := port.NewLLMProviderError(port.LLMErrorServerError, nil)

	// Below threshold: still healthy.
	reg.recordFailure(port.ProviderClaude, srv, nil)
	reg.recordFailure(port.ProviderClaude, srv, nil)
	if !reg.isHealthy(port.ProviderClaude) {
		t.Fatal("2 failures < threshold; provider must stay healthy")
	}
	// 3rd consecutive failure → transient unhealthy.
	reg.recordFailure(port.ProviderClaude, srv, nil)
	if reg.isHealthy(port.ProviderClaude) {
		t.Fatal("3 consecutive failures must mark transient unhealthy")
	}
	if st, _ := mx.lastHealth(port.ProviderClaude); st != string(HealthUnhealthy) {
		t.Fatalf("health metric = %q, want unhealthy", st)
	}
	// Success resets and recovers.
	reg.recordSuccess(port.ProviderClaude)
	if !reg.isHealthy(port.ProviderClaude) {
		t.Fatal("a successful probe must recover a transient-unhealthy provider")
	}
	h, _ := reg.snapshot(port.ProviderClaude)
	if h.consecutiveFailures != 0 {
		t.Fatalf("consecutiveFailures = %d, want 0 after success", h.consecutiveFailures)
	}
}

func TestRegistry_TransportErrorIsTransient(t *testing.T) {
	t.Parallel()
	reg, _ := newTestRegistry(time.Now)
	for i := 0; i < transientUnhealthyThreshold; i++ {
		reg.recordFailure(port.ProviderClaude, nil, errDNS)
	}
	if reg.isHealthy(port.ProviderClaude) {
		t.Fatal("transport failures must accumulate toward transient-unhealthy")
	}
	h, _ := reg.snapshot(port.ProviderClaude)
	if h.permanent {
		t.Fatal("transport error must never be permanent")
	}
}

func TestRegistry_AuthIsPermanentNeverProbed(t *testing.T) {
	t.Parallel()
	reg, _ := newTestRegistry(time.Now)
	reg.recordFailure(port.ProviderClaude, port.NewLLMProviderError(port.LLMErrorInvalidAPIKey, nil), nil)

	h, _ := reg.snapshot(port.ProviderClaude)
	if !h.permanent || h.healthy || !h.quotaUntil.IsZero() {
		t.Fatalf("auth failure → permanent, !healthy, zero quotaUntil; got %+v", h)
	}
	if reg.shouldProbe(port.ProviderClaude) {
		t.Fatal("auth-permanent provider must never be probed (waits for restart)")
	}
}

func TestRegistry_QuotaPermanentAutoRecoversAfter24h(t *testing.T) {
	t.Parallel()
	base := time.Date(2026, 5, 16, 12, 0, 0, 0, time.UTC)
	cur := base
	var mu sync.Mutex
	now := func() time.Time { mu.Lock(); defer mu.Unlock(); return cur }
	reg, _ := newTestRegistry(now)

	reg.recordFailure(port.ProviderClaude, port.NewLLMProviderError(port.LLMErrorQuotaExceeded, nil), nil)
	if reg.shouldProbe(port.ProviderClaude) {
		t.Fatal("quota-permanent must not be probed within the 24h window")
	}
	// 23h59m later — still skipped.
	mu.Lock()
	cur = base.Add(quotaRecheckAfter - time.Minute)
	mu.Unlock()
	if reg.shouldProbe(port.ProviderClaude) {
		t.Fatal("quota-permanent must remain skipped until the 24h window elapses")
	}
	// 24h later — probe allowed for auto-recovery.
	mu.Lock()
	cur = base.Add(quotaRecheckAfter)
	mu.Unlock()
	if !reg.shouldProbe(port.ProviderClaude) {
		t.Fatal("quota-permanent must be re-probed once the 24h window elapses")
	}
	// isHealthy still false until a successful probe clears permanent.
	if reg.isHealthy(port.ProviderClaude) {
		t.Fatal("an elapsed quota window must not make the provider healthy without a successful probe")
	}
	reg.recordSuccess(port.ProviderClaude)
	if !reg.isHealthy(port.ProviderClaude) {
		t.Fatal("a successful probe must clear quota-permanent")
	}
}

func TestRegistry_HealthMetricOnlyOnChange(t *testing.T) {
	t.Parallel()
	reg, mx := newTestRegistry(time.Now)
	mx.mu.Lock()
	seed := len(mx.health)
	mx.mu.Unlock()

	// Repeated successes on an already-healthy provider must not re-emit.
	reg.recordSuccess(port.ProviderClaude)
	reg.recordSuccess(port.ProviderClaude)
	mx.mu.Lock()
	got := len(mx.health)
	mx.mu.Unlock()
	if got != seed {
		t.Fatalf("steady-state healthy must not re-emit gauge: emissions %d → %d", seed, got)
	}
}

// LOW-1: CONTEXT_TOO_LONG / MALFORMED_REQUEST are LIC bugs, not provider
// ill-health — they must NOT accumulate toward transient-unhealthy even
// across many requests.
func TestRegistry_NonFallbackFatalDoesNotDegradeHealth(t *testing.T) {
	t.Parallel()
	reg, mx := newTestRegistry(time.Now)
	for _, code := range []port.LLMErrorCode{port.LLMErrorMalformedRequest, port.LLMErrorContextTooLong} {
		for i := 0; i < transientUnhealthyThreshold+2; i++ {
			reg.recordFailure(port.ProviderClaude, port.NewLLMProviderError(code, nil), nil)
		}
	}
	if !reg.isHealthy(port.ProviderClaude) {
		t.Fatal("non-retryable & non-fallback codes must not flip a provider unhealthy")
	}
	h, _ := reg.snapshot(port.ProviderClaude)
	if h.consecutiveFailures != 0 {
		t.Fatalf("consecutiveFailures = %d, want 0 (LIC-bug codes are a health no-op)", h.consecutiveFailures)
	}
	mx.mu.Lock()
	emissions := len(mx.health)
	mx.mu.Unlock()
	if emissions != 2 { // only the two seed emissions, no transitions
		t.Fatalf("health emissions = %d, want 2 (seed only, no transition)", emissions)
	}
}

func TestRegistry_UnknownProviderIsNotHealthy(t *testing.T) {
	t.Parallel()
	reg, _ := newTestRegistry(time.Now)
	if reg.isHealthy(port.ProviderGemini) {
		t.Fatal("a provider absent from the registry must not be considered healthy")
	}
	if reg.shouldProbe(port.ProviderGemini) {
		t.Fatal("an unknown provider must not be probed")
	}
}

var errDNS = &dnsError{}

type dnsError struct{}

func (*dnsError) Error() string { return "dns: no such host" }
