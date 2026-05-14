package port

import (
	"context"

	"contractpro/legal-intelligence-core/internal/domain/model"
)

// Agent is the uniform contract every one of the 9 LIC AI agents implements
// (high-architecture.md §6.6, ai-agents-pipeline.md). It hides per-agent
// concrete logic — Prompt Builder, Token Estimator, primary call, schema
// validation, repair loop — behind a single Run() method.
//
// Implementations live in internal/agents/{typeclassifier, keyparams, ...}
// and are wired into the Stage Executor (LIC-TASK-034) by AgentID.
type Agent interface {
	// ID returns the stable AgentID used in metrics, OTel attributes,
	// DLQ envelopes and per-agent env overrides (LIC_AGENT_*_PROVIDER,
	// LIC_AGENT_*_TIMEOUT).
	ID() model.AgentID

	// Run executes one invocation of the agent: builds the prompt from
	// `input`, calls the LLM via the Provider Router, validates the
	// response against the agent's JSON schema (with one repair attempt
	// on failure) and returns the typed result.
	//
	// The returned AgentResult is the agent's strongly-typed output
	// struct (ClassificationResult, KeyParameters, ...). Callers narrow
	// it with a type assertion driven by ID(); the Stage Executor does
	// this through a per-AgentID dispatch table so no centralised switch
	// is needed.
	//
	// Errors are returned as *model.DomainError so the Pipeline
	// Orchestrator can map them directly to LICStatusChangedEvent.FAILED
	// fields (Retryable, ErrorCode, UserMessage).
	Run(ctx context.Context, input model.AgentInput) (AgentResult, error)
}

// AgentResult is the type-erased return of Agent.Run. v1 uses `any` because
// every agent has its own concrete result type and the 9-agent dispatch is
// per-AgentID; a marker interface or generic Agent[R] would either force a
// touch to every concrete type in model/ or break the heterogeneous registry
// the Stage Executor needs.
//
// Concrete runtime types are: *model.ClassificationResult, *model.KeyParameters,
// *model.PartyConsistencyFindings, *model.MandatoryConditionsReport,
// *model.RiskAnalysis, model.Recommendations, *model.Summary,
// *model.DetailedReport, *model.RiskDelta. Tests in the agent packages
// (LIC-TASK-025..033) assert their respective concrete types.
type AgentResult = any
