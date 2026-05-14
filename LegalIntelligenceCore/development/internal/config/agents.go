package config

import (
	"errors"
	"fmt"
	"time"
)

// Agent IDs are kept as local string constants until LIC-TASK-011 introduces
// the typed `domain.AgentID`. After that task lands, callers can migrate to
// the typed key and these constants can be removed.
//
// The string value of each constant IS the env-var suffix
// (e.g. `LIC_AGENT_TYPE_CLASSIFIER_PROVIDER` for AgentTypeClassifier),
// so we use the constant directly when building env-var names.
const (
	AgentTypeClassifier      = "TYPE_CLASSIFIER"
	AgentKeyParams           = "KEY_PARAMS"
	AgentPartyConsistency    = "PARTY_CONSISTENCY"
	AgentMandatoryConditions = "MANDATORY_CONDITIONS"
	AgentRiskDetection       = "RISK_DETECTION"
	AgentRecommendation      = "RECOMMENDATION"
	AgentSummary             = "SUMMARY"
	AgentDetailedReport      = "DETAILED_REPORT"
	AgentRiskDelta           = "RISK_DELTA"
)

// AllAgentIDs is the canonical ordering for the 9 agents (used by validators,
// metrics initialisation, and golden tests). Order matches ai-agents-pipeline.md.
var AllAgentIDs = []string{
	AgentTypeClassifier,
	AgentKeyParams,
	AgentPartyConsistency,
	AgentMandatoryConditions,
	AgentRiskDetection,
	AgentRecommendation,
	AgentSummary,
	AgentDetailedReport,
	AgentRiskDelta,
}

// AgentsConfig holds the per-agent primary-provider override map and the
// per-agent timeout map. Default provider is claude for every agent (ADR-LIC-03).
type AgentsConfig struct {
	Providers map[string]string        // agentID → providerID (claude|openai|gemini)
	Timeouts  map[string]time.Duration // agentID → timeout
}

// defaultAgentTimeouts mirrors configuration.md §2.11.
var defaultAgentTimeouts = map[string]time.Duration{
	AgentTypeClassifier:      5 * time.Second,
	AgentKeyParams:           8 * time.Second,
	AgentPartyConsistency:    6 * time.Second,
	AgentMandatoryConditions: 8 * time.Second,
	AgentRiskDetection:       12 * time.Second,
	AgentRecommendation:      10 * time.Second,
	AgentSummary:             6 * time.Second,
	AgentDetailedReport:      12 * time.Second,
	AgentRiskDelta:           8 * time.Second,
}

func loadAgentsConfig() AgentsConfig {
	providers := make(map[string]string, len(AllAgentIDs))
	timeouts := make(map[string]time.Duration, len(AllAgentIDs))
	for _, id := range AllAgentIDs {
		providers[id] = envString("LIC_AGENT_"+id+"_PROVIDER", ProviderClaude)
		timeouts[id] = envDuration("LIC_AGENT_"+id+"_TIMEOUT", defaultAgentTimeouts[id])
	}
	return AgentsConfig{Providers: providers, Timeouts: timeouts}
}

// validate checks per-agent overrides:
//   - provider value is a known provider AND appears in fallbackOrder (so the
//     router can actually route through it);
//   - timeout > 0;
//   - no spurious extra agent IDs in the maps (catches direct struct injection).
//
// All findings are aggregated via errors.Join so the operator sees every
// misconfigured agent in a single fail-fast message.
func (a AgentsConfig) validate(fallbackOrder []string) error {
	inOrder := make(map[string]struct{}, len(fallbackOrder))
	for _, p := range fallbackOrder {
		inOrder[p] = struct{}{}
	}
	known := make(map[string]struct{}, len(AllAgentIDs))
	for _, id := range AllAgentIDs {
		known[id] = struct{}{}
	}

	var errs []error
	for _, id := range AllAgentIDs {
		provider, ok := a.Providers[id]
		if !ok {
			errs = append(errs, fmt.Errorf("config: AgentsConfig.Providers missing agent %q", id))
			continue
		}
		if !IsKnownProvider(provider) {
			errs = append(errs, fmt.Errorf("config: LIC_AGENT_%s_PROVIDER is %q, expected claude|openai|gemini", id, provider))
		} else if _, present := inOrder[provider]; !present {
			errs = append(errs, fmt.Errorf("config: LIC_AGENT_%s_PROVIDER=%q is not in LIC_PROVIDER_FALLBACK_ORDER=%v", id, provider, fallbackOrder))
		}
		if a.Timeouts[id] <= 0 {
			errs = append(errs, fmt.Errorf("config: LIC_AGENT_%s_TIMEOUT must be > 0, got %s", id, a.Timeouts[id]))
		}
	}
	// Spurious extra keys in either map => silent agent IDs that never run.
	for id := range a.Providers {
		if _, ok := known[id]; !ok {
			errs = append(errs, fmt.Errorf("config: AgentsConfig.Providers has unknown agent %q", id))
		}
	}
	for id := range a.Timeouts {
		if _, ok := known[id]; !ok {
			errs = append(errs, fmt.Errorf("config: AgentsConfig.Timeouts has unknown agent %q", id))
		}
	}
	return errors.Join(errs...)
}
