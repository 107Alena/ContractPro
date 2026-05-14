package model

// AgentID identifies one of the 9 AI agents in the LIC pipeline (see
// ai-agents-pipeline.md §1–9). The wire format is upper-snake-case with the
// "AGENT_" prefix and is used in structured logs, Prometheus labels, OTel
// span attributes and DLQ envelopes.
type AgentID string

const (
	AgentTypeClassifier      AgentID = "AGENT_TYPE_CLASSIFIER"
	AgentKeyParams           AgentID = "AGENT_KEY_PARAMS"
	AgentPartyConsistency    AgentID = "AGENT_PARTY_CONSISTENCY"
	AgentMandatoryConditions AgentID = "AGENT_MANDATORY_CONDITIONS"
	AgentRiskDetection       AgentID = "AGENT_RISK_DETECTION"
	AgentRecommendation      AgentID = "AGENT_RECOMMENDATION"
	AgentSummary             AgentID = "AGENT_SUMMARY"
	AgentDetailedReport      AgentID = "AGENT_DETAILED_REPORT"
	AgentRiskDelta           AgentID = "AGENT_RISK_DELTA"
)

// String returns the wire representation of the agent identifier.
func (a AgentID) String() string { return string(a) }

// IsValid reports whether a is one of the 9 defined agent identifiers.
func (a AgentID) IsValid() bool {
	_, ok := agentIDSet[a]
	return ok
}

var agentIDSet map[AgentID]struct{}

// AllAgentIDs returns a fresh slice with every defined AgentID in pipeline
// execution order (Stage 1 → Stage 6). Callers may mutate the returned slice.
func AllAgentIDs() []AgentID {
	return []AgentID{
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
}

func init() {
	all := AllAgentIDs()
	agentIDSet = make(map[AgentID]struct{}, len(all))
	for _, a := range all {
		agentIDSet[a] = struct{}{}
	}
}
