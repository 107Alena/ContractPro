package port

import (
	"context"
	"encoding/json"

	"contractpro/legal-intelligence-core/internal/domain/model"
)

// LLMProviderPort is the single contract every LLM adapter (Claude, OpenAI,
// Gemini) implements (llm-provider-abstraction.md §1.1). Agents talk only to
// this interface; swapping a provider is purely a deployment-time decision.
//
// Compile-time checks (var _ LLMProviderPort = (*claudeProvider)(nil)) belong
// in each adapter package — port itself only declares the contract.
//
// **Adapter invariant** (enforced by the Router and asserted by adapter
// contract tests): every non-nil error returned from `Complete` or
// `HealthCheck` MUST unwrap to a *LLMProviderError via errors.As. Adapters
// MUST NOT return bare network / SDK errors — the Router uses the typed
// fields (Retryable, FallbackEligible, RetryAfter) to drive its decisions
// and a missing classification degrades silently to "fail this provider, no
// fallback". `AsLLMProviderError` is the canonical extraction helper.
type LLMProviderPort interface {
	// ID returns the stable LLMProviderID under which the adapter is
	// registered in the router and routed through env LIC_AGENT_*_PROVIDER.
	ID() LLMProviderID

	// Complete performs one LLM round-trip and returns the parsed response.
	// On error, the returned value MUST be (or wrap) a *LLMProviderError so
	// the Router can branch on Retryable / FallbackEligible — never a bare
	// network error (see llm-provider-abstraction.md §1.2 and the adapter
	// invariant in this interface's godoc).
	Complete(ctx context.Context, req CompletionRequest) (CompletionResponse, error)

	// HealthCheck issues a minimal probe (max_tokens=10) to confirm the
	// provider responds. The dual return distinguishes two failure modes:
	//   - return (typedErr, nil) when the wire call completed but reported
	//     an LLM-specific failure (401, 429, content policy). The Router
	//     uses typedErr to mark the provider permanently unhealthy in the
	//     auth/quota case (§1.3).
	//   - return (nil, err) when the probe never reached the provider
	//     (DNS, TLS, transport). The Router treats this as transient.
	//   - return (nil, nil) on success.
	HealthCheck(ctx context.Context) (*LLMProviderError, error)
}

// LLMProviderID is the typed wire identifier for an LLM provider (used in
// metric labels, OTel attributes, env keys, agent-primary maps).
type LLMProviderID string

// The three providers shipped in v1. Adding a new adapter = new const + new
// package; the abstraction itself does not change.
const (
	ProviderClaude LLMProviderID = "claude"
	ProviderOpenAI LLMProviderID = "openai"
	ProviderGemini LLMProviderID = "gemini"
)

// String returns the wire representation of the provider identifier.
func (p LLMProviderID) String() string { return string(p) }

// IsKnown reports whether p is one of the three providers declared in v1.
// Adapters may register additional providers in the future; callers that
// rely on this helper for validation MUST be updated alongside any new
// const declaration.
func (p LLMProviderID) IsKnown() bool {
	switch p {
	case ProviderClaude, ProviderOpenAI, ProviderGemini:
		return true
	default:
		return false
	}
}

// Role identifies the speaker of a Turn in a multi-turn conversation. System
// prompts do NOT use this type — they are carried separately in
// CompletionRequest.System (llm-provider-abstraction.md §1.1).
type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

// IsValid reports whether r is one of the two declared roles.
func (r Role) IsValid() bool {
	switch r {
	case RoleUser, RoleAssistant:
		return true
	default:
		return false
	}
}

// Turn is one exchange in a multi-turn conversation, used solely for the
// repair-loop (llm-provider-abstraction.md §1.1): when a primary call returns
// JSON that fails schema validation, the Schema Validator (LIC-TASK-023)
// builds the repair request and re-issues it via `CompleteRepair` on the
// same provider.
//
// Repair conversation shape (authoritative — see
// internal/agents/schemavalidator). Adapters build the wire conversation as
// [System?] + PriorTurns... + a final appended {user, req.User}, and
// error-handling.md §5.2 requires the previous user message to be preserved.
// The Schema Validator therefore sets, on the repair request:
//
//	PriorTurns = origPriorTurns + [{User, origUser}, {Assistant, invalid_response}]
//	User       = repair_prompt   (the adapter appends it as the final user turn)
//
// yielding wire = [..origPriors, user:origUser, assistant:invalid, user:repair]
// — a valid alternating sequence that starts with a user turn on all three
// adapters. (Earlier revisions of this godoc and high-architecture.md §6.8
// showed a lossy 2-element shorthand [{Assistant,invalid},{User,repair}];
// that omitted the preserved original user turn and is corrected here —
// schemavalidator/CLAUDE.md records the reconciliation.)
//
// Role=System is NOT permitted; system prompts are non-turn-scoped and travel
// in CompletionRequest.System.
type Turn struct {
	Role    Role
	Content string
}

// CompletionRequest is the provider-agnostic request shape every adapter
// translates into its native chat format (Anthropic Messages, OpenAI
// Responses, Gemini generateContent — llm-provider-abstraction.md §1.1).
//
// JSONSchema, when set, enables strict structured outputs at the provider
// (tool_use for Claude, response_format=json_schema for OpenAI, responseSchema
// for Gemini). Without it the adapter falls back to free-text or json_object
// mode and validation remains the responsibility of Schema Validator.
//
// Correlation identifiers (correlation_id, job_id, version_id, organization_id,
// created_by_user_id) are propagated via context, not fields, so the wire
// envelope stays minimal and PII-free (llm-provider-abstraction.md §1.1).
type CompletionRequest struct {
	AgentID model.AgentID // for metrics and OTel attributes; never sent to the provider
	Model   string        // concrete model id, e.g. claude-sonnet-4-6

	System     string // baked-in agent system prompt
	User       string // current-turn user content (XML-enveloped data)
	PriorTurns []Turn // optional history; empty on primary calls, set in repair

	MaxTokens     int      // upper bound on output tokens
	Temperature   float64  // 0..1
	StopSequences []string // optional

	// JSONMode signals "return JSON" without a schema. If JSONSchema is set,
	// JSONMode is implied true and this flag is redundant.
	JSONMode   bool
	JSONSchema json.RawMessage // optional JSON Schema draft-07 for strict structured outputs
}

// CompletionResponse is the typed reply from an adapter.
//
// CachedInputTokens is broken out from InputTokens so the Cost & Usage Tracker
// can bill at the 10×-discounted prompt-cache rate on Anthropic
// (llm-provider-abstraction.md §4.1). v1 OpenAI / Gemini adapters always
// report zero here.
type CompletionResponse struct {
	Content           string
	InputTokens       int
	CachedInputTokens int
	OutputTokens      int
	StopReason        StopReason
	LatencyMs         int64
	ProviderID        LLMProviderID
	Model             string
}

// StopReason is the typed reason the provider stopped generating
// (llm-provider-abstraction.md §1.1).
type StopReason string

const (
	StopReasonEndTurn       StopReason = "end_turn"
	StopReasonMaxTokens     StopReason = "max_tokens"
	StopReasonStopSequence  StopReason = "stop_sequence"
	StopReasonContentFilter StopReason = "content_filter" // OpenAI/Gemini-specific; treated by router as LLMErrorContentPolicy
)

// IsValid reports whether s is one of the four declared stop reasons.
func (s StopReason) IsValid() bool {
	switch s {
	case StopReasonEndTurn, StopReasonMaxTokens,
		StopReasonStopSequence, StopReasonContentFilter:
		return true
	default:
		return false
	}
}

// LLMProviderError and its taxonomy live in llm_errors.go.
