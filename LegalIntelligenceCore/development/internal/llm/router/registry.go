package router

import (
	"sync"
	"time"

	"contractpro/legal-intelligence-core/internal/domain/port"
)

// transientUnhealthyThreshold is the §2.3 rule: a provider is marked
// transient-unhealthy only after >= 3 consecutive retryable/transport
// failures (a single 5xx blip must not divert the whole pipeline).
const transientUnhealthyThreshold = 3

// quotaRecheckAfter is the §2.3 quota auto-recovery window: QUOTA_EXCEEDED
// pins the provider permanent-unhealthy for 24h (quota typically resets
// daily), after which the background loop re-probes.
const quotaRecheckAfter = 24 * time.Hour

// providerHealth is one provider's row in the in-memory registry
// (acceptance criterion 5; §2.3 "{healthy, permanent, last_check_at,
// consecutive_failures}"). quotaUntil distinguishes the two permanent
// flavours: zero ⇒ auth (INVALID_API_KEY — never auto-recovers, needs
// SIGHUP/restart); non-zero ⇒ quota (re-probe allowed at/after quotaUntil).
type providerHealth struct {
	healthy             bool
	permanent           bool
	consecutiveFailures int
	lastCheckAt         time.Time
	quotaUntil          time.Time
}

// state derives the lic_llm_provider_health_status label for this row.
// permanent dominates; otherwise healthy/unhealthy.
func (h providerHealth) state() HealthState {
	switch {
	case h.permanent:
		return HealthPermanent
	case h.healthy:
		return HealthHealthy
	default:
		return HealthUnhealthy
	}
}

// healthRegistry is the mutex-guarded in-memory health map shared by the
// Complete path and the background health loop. It is the SINGLE owner of
// every health state transition (code-architect MF-3): both a Complete
// failure and a HealthCheck failure funnel through recordFailure, so the
// two producers cannot define divergent "unhealthy" semantics.
//
// lic_llm_provider_health_status is emitted exactly once per *transition*
// (per-count exactness), but the emit happens AFTER mu.Unlock() (never
// call a seam holding a mutex). Two interleaved transitions can therefore
// emit gauges out of final-state order — an eventual-consistency window of
// at most one 30s health-loop tick, which self-heals on the next sweep
// (golang-pro N-2; acceptable for v1, the gauge is for alerting trends not
// exact instantaneous state).
type healthRegistry struct {
	mu      sync.Mutex
	entries map[port.LLMProviderID]*providerHealth
	metrics Metrics
	now     func() time.Time
}

// newHealthRegistry seeds every known provider as healthy (optimistic
// start, §2.3: the registry begins healthy and the background loop
// maintains it) and emits the initial healthy gauge so dashboards have a
// series from t0.
func newHealthRegistry(providerIDs []port.LLMProviderID, m Metrics, now func() time.Time) *healthRegistry {
	r := &healthRegistry{
		entries: make(map[port.LLMProviderID]*providerHealth, len(providerIDs)),
		metrics: m,
		now:     now,
	}
	for _, id := range providerIDs {
		r.entries[id] = &providerHealth{healthy: true}
		r.metrics.ProviderHealthState(id, HealthHealthy)
	}
	return r
}

// isHealthy is the gate Complete/CompleteRepair consult before a wire call
// (§2.1 "if !r.healthy(providerID): skip"). A permanent entry is never
// healthy; a quota-permanent entry whose window has elapsed is still "not
// healthy" here — only a successful probe (recordSuccess) clears permanent.
// Treating the elapsed-window state as healthy here would let live traffic
// hammer a still-throttled provider; the background loop owns recovery.
func (r *healthRegistry) isHealthy(id port.LLMProviderID) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	h, ok := r.entries[id]
	if !ok {
		return false
	}
	return h.healthy && !h.permanent
}

// shouldProbe reports whether the background loop should issue a HealthCheck
// for id (§2.3): auth-permanent (quotaUntil zero) is skipped forever
// (pinger must not waste quota / flap on every ping until restart);
// quota-permanent is skipped until its 24h window elapses, then probed for
// auto-recovery; everything else is always probed.
func (r *healthRegistry) shouldProbe(id port.LLMProviderID) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	h, ok := r.entries[id]
	if !ok {
		return false
	}
	if !h.permanent {
		return true
	}
	if h.quotaUntil.IsZero() {
		return false // auth-permanent: wait for operator (SIGHUP/restart)
	}
	return !r.now().Before(h.quotaUntil) // quota-permanent: probe once window elapsed
}

// recordSuccess is the single success transition (HealthCheck success, §2.3
// row 1): reset consecutive_failures, clear transient AND permanent (a
// successful probe is the quota auto-recovery signal). Emits the healthy
// gauge only when the state actually changed, so a steady-state healthy
// provider does not re-emit every 30s.
func (r *healthRegistry) recordSuccess(id port.LLMProviderID) {
	r.mu.Lock()
	h, ok := r.entries[id]
	if !ok {
		r.mu.Unlock()
		return
	}
	changed := !h.healthy || h.permanent
	h.healthy = true
	h.permanent = false
	h.consecutiveFailures = 0
	h.quotaUntil = time.Time{}
	h.lastCheckAt = r.now()
	r.mu.Unlock()

	if changed {
		r.metrics.ProviderHealthState(id, HealthHealthy)
	}
}

// recordFailure is the single failure transition shared by the Complete
// path and the background loop (code-architect MF-3). It applies the §2.3
// table purely as a state side-effect — it returns nothing and never
// influences control flow (code-architect MF-2). Exactly one of pe /
// transportErr is meaningful:
//
//   - pe.IsAuthError()             → permanent (auth), never auto-recovers
//   - pe.Code == QUOTA_EXCEEDED    → permanent (quota), re-probe after 24h
//   - non-retryable & non-fallback (CONTEXT_TOO_LONG / MALFORMED_REQUEST)
//     → NO health change: these are LIC-side bugs (bad payload / un-truncated
//     input), not provider ill-health. §2.3 classifies only Retryable /
//     transport as transient; counting them would let a recurring LIC
//     request-builder defect flip a perfectly healthy provider unhealthy
//     across requests and mask the real bug (code-reviewer LOW-1).
//   - any other retryable / transport / unclassified failure
//     → consecutive_failures++, transient-unhealthy at >= 3
func (r *healthRegistry) recordFailure(id port.LLMProviderID, pe *port.LLMProviderError, transportErr error) {
	r.mu.Lock()
	h, ok := r.entries[id]
	if !ok {
		r.mu.Unlock()
		return
	}
	prev := h.state()
	h.lastCheckAt = r.now()

	switch {
	case pe != nil && pe.IsAuthError():
		h.permanent = true
		h.healthy = false
		h.quotaUntil = time.Time{}
	case pe != nil && pe.Code == port.LLMErrorQuotaExceeded:
		h.permanent = true
		h.healthy = false
		h.quotaUntil = r.now().Add(quotaRecheckAfter)
	case pe != nil && !pe.Retryable && !pe.FallbackEligible:
		// CONTEXT_TOO_LONG / MALFORMED_REQUEST — LIC bug, not provider
		// ill-health: no-op for health state (LOW-1).
	default:
		h.consecutiveFailures++
		if h.consecutiveFailures >= transientUnhealthyThreshold {
			h.healthy = false
		}
	}
	next := h.state()
	r.mu.Unlock()

	if next != prev {
		r.metrics.ProviderHealthState(id, next)
	}
}

// snapshot returns a copy of one provider's row for tests/diagnostics
// (never a pointer into the live map). ok=false for an unknown provider.
func (r *healthRegistry) snapshot(id port.LLMProviderID) (providerHealth, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	h, ok := r.entries[id]
	if !ok {
		return providerHealth{}, false
	}
	return *h, true
}
