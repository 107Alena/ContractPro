package router

import (
	"time"

	"contractpro/legal-intelligence-core/internal/domain/model"
	"contractpro/legal-intelligence-core/internal/domain/port"
)

// defaultHealthCheckInterval is the §2.3 production cadence (every 30s; on
// staging the operator may relax to 60s via config). Used when
// RouterConfig.HealthCheckInterval is zero so a partially-populated config
// still produces a working background loop.
const defaultHealthCheckInterval = 30 * time.Second

// defaultHealthCheckTimeout bounds a single provider HealthCheck probe so
// one hung provider cannot stall the whole sweep. Comfortably above any
// real provider's ping latency and well below the 30s interval.
const defaultHealthCheckTimeout = 10 * time.Second

// RouterConfig is the per-agent routing policy + global fallback order the
// Router is built from (acceptance criterion 2; llm-provider-abstraction.md
// §2.1–§2.3). It is assembled by app-wiring (LIC-TASK-047) from
// config.AgentsConfig.Providers / config.LLMConfig.ProviderFallbackOrder —
// this package reads no env (hermetic, like every internal/llm/* sibling).
type RouterConfig struct {
	// AgentPrimary maps each of the 9 agents to its primary provider
	// (ADR-LIC-03 default: claude for all). NewProviderRouter fails fast
	// unless this covers every model.AllAgentIDs() and every value is a
	// registered provider.
	AgentPrimary map[model.AgentID]port.LLMProviderID

	// FallbackOrder is the global fallback chain (LIC_PROVIDER_FALLBACK_ORDER,
	// e.g. claude→openai→gemini). The effective per-agent chain is
	// [primary, FallbackOrder...] with the primary deduplicated.
	FallbackOrder []port.LLMProviderID

	// HealthCheckInterval is the background health-loop cadence (§2.3).
	// Zero → defaultHealthCheckInterval.
	HealthCheckInterval time.Duration

	// HealthCheckTimeout bounds one provider probe. Zero →
	// defaultHealthCheckTimeout.
	HealthCheckTimeout time.Duration
}

func (c RouterConfig) healthCheckInterval() time.Duration {
	if c.HealthCheckInterval <= 0 {
		return defaultHealthCheckInterval
	}
	return c.HealthCheckInterval
}

func (c RouterConfig) healthCheckTimeout() time.Duration {
	if c.HealthCheckTimeout <= 0 {
		return defaultHealthCheckTimeout
	}
	return c.HealthCheckTimeout
}
