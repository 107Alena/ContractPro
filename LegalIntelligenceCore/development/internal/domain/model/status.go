// Package model contains pure domain types for the Legal Intelligence Core
// service. The package has no dependencies on infra, agents, llm or any other
// outer layer — it is the inner-most ring of the hexagonal architecture.
package model

// ExternalStatus is the high-level lifecycle status published in
// lic.events.status-changed (only three values per high-architecture.md
// §2.1.3 — QUEUED / COMPLETED_WITH_WARNINGS / TIMED_OUT / REJECTED of the
// system-wide enum are NOT published by LIC v1).
type ExternalStatus string

const (
	StatusInProgress ExternalStatus = "IN_PROGRESS"
	StatusCompleted  ExternalStatus = "COMPLETED"
	StatusFailed     ExternalStatus = "FAILED"
)

// IsValid reports whether s is one of the three published external statuses.
func (s ExternalStatus) IsValid() bool {
	switch s {
	case StatusInProgress, StatusCompleted, StatusFailed:
		return true
	}
	return false
}

// String returns the wire representation of the status.
func (s ExternalStatus) String() string { return string(s) }

// AllExternalStatuses returns a fresh slice with every defined ExternalStatus
// in the order in which they were declared. Callers may mutate the returned
// slice without affecting subsequent calls.
func AllExternalStatuses() []ExternalStatus {
	return []ExternalStatus{StatusInProgress, StatusCompleted, StatusFailed}
}

// Stage is the internal pipeline stage used for structured logs, Prometheus
// labels and OpenTelemetry span attributes. Stages are not exposed in the
// external status enum (see high-architecture.md §2.1.3 — внешний контракт
// минимален; стадии видны только в логах / Prometheus / OTel).
type Stage string

// Receive / artifacts stages.
const (
	StageReceived            Stage = "STAGE_RECEIVED"
	StageRequestingArtifacts Stage = "STAGE_REQUESTING_ARTIFACTS"
	StageArtifactsReceived   Stage = "STAGE_ARTIFACTS_RECEIVED"
)

// Agent stages — one per agent in the 9-agent pipeline.
const (
	StageAgentTypeClassifier      Stage = "STAGE_AGENT_TYPE_CLASSIFIER"
	StageAgentKeyParams           Stage = "STAGE_AGENT_KEY_PARAMS"
	StageAwaitingUserConfirmation Stage = "STAGE_AWAITING_USER_CONFIRMATION"
	StageAgentPartyConsistency    Stage = "STAGE_AGENT_PARTY_CONSISTENCY"
	StageAgentMandatoryConditions Stage = "STAGE_AGENT_MANDATORY_CONDITIONS"
	StageAgentRiskDetection       Stage = "STAGE_AGENT_RISK_DETECTION"
	StageAgentRecommendation      Stage = "STAGE_AGENT_RECOMMENDATION"
	StageAgentSummary             Stage = "STAGE_AGENT_SUMMARY"
	StageAgentDetailedReport      Stage = "STAGE_AGENT_DETAILED_REPORT"
	StageAgentRiskDelta           Stage = "STAGE_AGENT_RISK_DELTA"
)

// Deterministic calculation stages (no LLM calls).
const (
	StageRiskProfileCalc    Stage = "STAGE_RISK_PROFILE_CALC"
	StageAggregateScoreCalc Stage = "STAGE_AGGREGATE_SCORE_CALC"
)

// Publish / DM-confirmation / terminal stages.
const (
	StagePublishingArtifacts    Stage = "STAGE_PUBLISHING_ARTIFACTS"
	StageAwaitingDMConfirmation Stage = "STAGE_AWAITING_DM_CONFIRMATION"
	StageDone                   Stage = "STAGE_DONE"
)

// String returns the wire representation of the stage.
func (s Stage) String() string { return string(s) }

// IsValid reports whether s is a known pipeline stage.
func (s Stage) IsValid() bool {
	_, ok := stageSet[s]
	return ok
}

// stageSet is the lookup table backing Stage.IsValid. It is populated in
// init() from AllStages so the source of truth lives in a single slice.
var stageSet map[Stage]struct{}

// AllStages returns a fresh slice with every defined Stage in pipeline order
// (top-down through the orchestrator flow). Callers may mutate the returned
// slice freely.
func AllStages() []Stage {
	return []Stage{
		StageReceived,
		StageRequestingArtifacts,
		StageArtifactsReceived,
		StageAgentTypeClassifier,
		StageAgentKeyParams,
		StageAwaitingUserConfirmation,
		StageAgentPartyConsistency,
		StageAgentMandatoryConditions,
		StageAgentRiskDetection,
		StageAgentRecommendation,
		StageAgentSummary,
		StageAgentDetailedReport,
		StageAgentRiskDelta,
		StageRiskProfileCalc,
		StageAggregateScoreCalc,
		StagePublishingArtifacts,
		StageAwaitingDMConfirmation,
		StageDone,
	}
}

func init() {
	all := AllStages()
	stageSet = make(map[Stage]struct{}, len(all))
	for _, s := range all {
		stageSet[s] = struct{}{}
	}
}
