package metrics

// Single source of truth for Prometheus label values exposed by lic-service.
// observability.md §3 explicitly states this file's role: any divergence in
// other docs is overridden by these constants. Typed string constants keep
// call-sites honest — a typo in `OutcomeSuccss` is a compile error.

// PipelineMode is the binary mode label for pipeline metrics.
type PipelineMode string

const (
	// PipelineModeInitial — first analysis for a version (no parent_version_id).
	PipelineModeInitial PipelineMode = "INITIAL"
	// PipelineModeRecheck — re-analysis triggered by a new version derived
	// from an existing parent (parent_version_id != null).
	PipelineModeRecheck PipelineMode = "RE_CHECK"
)

// PipelineOutcome is the terminal outcome of a pipeline run.
type PipelineOutcome string

const (
	PipelineOutcomeSuccess PipelineOutcome = "success"
	PipelineOutcomeFailed  PipelineOutcome = "failed"
	PipelineOutcomeTimeout PipelineOutcome = "timeout"
)

// AgentInvocationOutcome is the result of one agent.Run() call.
type AgentInvocationOutcome string

const (
	AgentOutcomeSuccess        AgentInvocationOutcome = "success"
	AgentOutcomeRepairSuccess  AgentInvocationOutcome = "repair_success"
	AgentOutcomeInvalidOutput  AgentInvocationOutcome = "invalid_output"
	AgentOutcomeProviderError  AgentInvocationOutcome = "provider_error"
	AgentOutcomeTimeout        AgentInvocationOutcome = "timeout"
)

// AgentRepairOutcome is the result of a single repair loop attempt.
type AgentRepairOutcome string

const (
	RepairOutcomeRepairedOK    AgentRepairOutcome = "repaired_ok"
	RepairOutcomeRepairFailed  AgentRepairOutcome = "repair_failed"
	RepairOutcomeProviderError AgentRepairOutcome = "repair_provider_error"
)

// LLMCallOutcome — observability.md §3.4: "repair" увеличивается для каждого
// CompleteRepair-вызова независимо от его дальнейшего исхода.
type LLMCallOutcome string

const (
	LLMOutcomeSuccess  LLMCallOutcome = "success"
	LLMOutcomeRepair   LLMCallOutcome = "repair"
	LLMOutcomeFail     LLMCallOutcome = "fail"
	LLMOutcomeFallback LLMCallOutcome = "fallback"
)

// LLMErrorCode matches LLMProviderError.Code from llm-provider-abstraction.md §1.2.
type LLMErrorCode string

const (
	LLMErrTimeout         LLMErrorCode = "TIMEOUT"
	LLMErrRateLimit       LLMErrorCode = "RATE_LIMIT"
	LLMErrServerError     LLMErrorCode = "SERVER_ERROR"
	LLMErrNetwork         LLMErrorCode = "NETWORK"
	LLMErrOverloaded      LLMErrorCode = "OVERLOADED"
	LLMErrInvalidAPIKey   LLMErrorCode = "INVALID_API_KEY"
	LLMErrQuotaExceeded   LLMErrorCode = "QUOTA_EXCEEDED"
	LLMErrContentPolicy   LLMErrorCode = "CONTENT_POLICY"
	LLMErrContextTooLong  LLMErrorCode = "CONTEXT_TOO_LONG"
	LLMErrMalformedReq    LLMErrorCode = "MALFORMED_REQUEST"
)

// LLMHealthState matches the provider registry state from
// llm-provider-abstraction.md §2.3.
type LLMHealthState string

const (
	LLMHealthHealthy   LLMHealthState = "healthy"
	LLMHealthUnhealthy LLMHealthState = "unhealthy"
	LLMHealthPermanent LLMHealthState = "permanent"
)

// CircuitState — numeric gauge encoding per observability.md §3.4.
//   0 — closed (normal operation)
//   1 — half_open (probing recovery)
//   2 — open (calls short-circuit)
const (
	CircuitStateClosed   float64 = 0
	CircuitStateHalfOpen float64 = 1
	CircuitStateOpen     float64 = 2
)

// DMOperation — get_artifacts | persist_artifacts (observability.md §3.5).
type DMOperation string

const (
	DMOpGetArtifacts     DMOperation = "get_artifacts"
	DMOpPersistArtifacts DMOperation = "persist_artifacts"
)

// DMOutcome — outcome of a DM request.
type DMOutcome string

const (
	DMOutcomeSuccess       DMOutcome = "success"
	DMOutcomeTimeout       DMOutcome = "timeout"
	DMOutcomePersistFailed DMOutcome = "persist_failed"
	DMOutcomeMissing       DMOutcome = "missing"
)

// IdempotencyLookupResult — observability.md §3.6.
type IdempotencyLookupResult string

const (
	IdempLookupNew         IdempotencyLookupResult = "new"
	IdempLookupInProgress  IdempotencyLookupResult = "in_progress"
	IdempLookupCompleted   IdempotencyLookupResult = "completed"
	IdempLookupFallbackDB  IdempotencyLookupResult = "fallback_db"
)

// PendingConfirmationOutcome — observability.md §3.7.
type PendingConfirmationOutcome string

const (
	PendingOutcomeResumed PendingConfirmationOutcome = "resumed"
	PendingOutcomeExpired PendingConfirmationOutcome = "expired"
	PendingOutcomeInvalid PendingConfirmationOutcome = "invalid"
)

// DLQTopic enumerates the four LIC DLQ destinations (integration-contracts.md §6.2).
type DLQTopic string

const (
	DLQTopicInvalidMessage     DLQTopic = "lic.dlq.invalid-message"
	DLQTopicConsumerFailed     DLQTopic = "lic.dlq.consumer-failed"
	DLQTopicPublishFailed      DLQTopic = "lic.dlq.publish-failed"
	DLQTopicAgentOutputInvalid DLQTopic = "lic.dlq.agent-output-invalid"
)

// PartyValidationType — high-architecture.md §6.7.2.
type PartyValidationType string

const (
	PartyValidationINN  PartyValidationType = "inn"
	PartyValidationOGRN PartyValidationType = "ogrn"
)

// BoolLabel converts a Go bool to the canonical Prometheus label value
// ("true"|"false") used for the `valid` label on lic_party_validation_total
// and similar counters. Centralising the conversion avoids divergent
// representations across call-sites.
func BoolLabel(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// PublishOutcome — generic success|failure outcome for consumer/publisher
// counters. Kept narrow on purpose: cardinality budget §3.10 forbids
// per-error-code fan-out at the broker layer.
//
// NOTE: observability.md §3.9 names lic_consumer_messages_total and
// lic_publisher_messages_total with an `outcome` label but does not
// enumerate the values. The four constants below are package-defined
// and the de-facto contract for LIC; coordinate any addition with the
// broker/consumer maintainer and reflect it in §3.9.
type PublishOutcome string

const (
	PublishOutcomeSuccess PublishOutcome = "success"
	PublishOutcomeFailure PublishOutcome = "failure"
	PublishOutcomeNacked  PublishOutcome = "nacked"
	PublishOutcomeInvalid PublishOutcome = "invalid"
)
