package router

import (
	"context"
	"time"

	"contractpro/legal-intelligence-core/internal/domain/port"
)

// Start launches the background health-check goroutine (acceptance
// criterion 7; §2.3): every HealthCheckInterval it probes each provider
// (skipping permanent-auth and not-yet-expired quota-permanent entries,
// §2.3) and feeds the result through the registry's single state-transition
// path. The goroutine is NOT started by the constructor so app-wiring
// (LIC-TASK-047) controls its lifecycle and tests can drive single sweeps
// (runHealthChecks) deterministically. Start is idempotent (repeated calls
// are no-ops); the loop stops when ctx is cancelled OR Stop() is called.
func (r *ProviderRouter) Start(ctx context.Context) {
	r.lifeMu.Lock()
	defer r.lifeMu.Unlock()
	if r.started || r.stopped {
		return // already running, or Stop won the race → never start
	}
	r.started = true
	loopCtx, cancel := context.WithCancel(ctx)
	r.stopFn = cancel
	r.wg.Add(1) // under lifeMu, so it happens-before Stop's wg.Wait
	go func() {
		defer r.wg.Done()
		r.healthLoop(loopCtx)
	}()
}

// Stop cancels the background loop and blocks until it has exited. Safe
// without a prior Start (no-op) and idempotent (stopOnce). Called by
// graceful shutdown (LIC-TASK-047) before the rest of teardown so the
// sweep cannot race a closing rate limiter / metrics registry.
func (r *ProviderRouter) Stop() {
	r.lifeMu.Lock()
	alreadyStopped := r.stopped
	r.stopped = true
	stop := r.stopFn
	r.lifeMu.Unlock()

	if !alreadyStopped && stop != nil {
		stop()
	}
	// Safe even if Start never ran (wg counter 0) or Stop is called twice
	// (wg.Wait is idempotent); the wg.Add is ordered before this via lifeMu.
	r.wg.Wait()
}

func (r *ProviderRouter) healthLoop(ctx context.Context) {
	ticker := time.NewTicker(r.cfg.healthCheckInterval())
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.runHealthChecks(ctx)
		}
	}
}

// runHealthChecks probes every eligible provider once and routes the dual
// HealthCheck return through the registry's single transition path
// (code-architect MF-3):
//
//   - (nil, nil)             → recordSuccess (resets transient, clears the
//     quota-permanent flag on the §2.3 24h auto-recovery probe)
//   - (*LLMProviderError, _) → recordFailure(pe): auth→permanent,
//     quota→permanent-24h, retryable→consecutive++ (§2.3 rows 2–4)
//   - (nil, transportErr)    → recordFailure(transport): transient (the
//     probe never reached the provider — §2.3 "DNS/TLS/transport")
//
// Probes are sequential (3 providers, sub-second each); each is bounded by
// HealthCheckTimeout so one hung provider cannot stall the sweep past the
// next tick. Exported indirectly via the loop; called directly by tests.
func (r *ProviderRouter) runHealthChecks(ctx context.Context) {
	for _, id := range r.orderedProviderIDs {
		if ctx.Err() != nil {
			return
		}
		if !r.registry.shouldProbe(id) {
			continue
		}
		r.probeOne(ctx, id, r.providers[id])
	}
}

func (r *ProviderRouter) probeOne(ctx context.Context, id port.LLMProviderID, p port.LLMProviderPort) {
	pctx, cancel := context.WithTimeout(ctx, r.cfg.healthCheckTimeout())
	defer cancel()

	typedErr, transportErr := p.HealthCheck(pctx)
	switch {
	case typedErr == nil && transportErr == nil:
		r.registry.recordSuccess(id)
	case typedErr != nil:
		r.registry.recordFailure(id, typedErr, nil)
	default:
		r.registry.recordFailure(id, nil, transportErr)
	}
}
