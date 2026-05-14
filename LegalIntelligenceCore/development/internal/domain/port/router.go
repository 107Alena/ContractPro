package port

import "context"

// PrimaryCallResult is the typed return of ProviderRouterPort.Complete — the
// successful response paired with the provider that actually served it
// (llm-provider-abstraction.md §2.1). The caller (BaseAgent) MUST stash
// `UsedProvider` and pass it back into CompleteRepair so the repair-loop is
// sticky (OQ-10): switching providers mid-conversation would break the
// PriorTurns contract.
type PrimaryCallResult struct {
	Response     CompletionResponse
	UsedProvider LLMProviderID
}

// ProviderRouterPort is the LIC-internal LLM-router contract: builds the
// per-agent chain (primary + fallbacks), enforces per-provider rate limits
// in-process before the wire call, retries on the same provider, falls back
// on FallbackEligible errors, and surfaces ALL_PROVIDERS_FAILED when the
// chain is exhausted (llm-provider-abstraction.md §2.1, LIC-TASK-019).
//
// Two methods, two semantics:
//
//   - Complete: walks the chain primary → fallback. On success, returns the
//     winning provider in PrimaryCallResult.
//   - CompleteRepair: sticky on `usedProvider`. NO fallback — if the same
//     provider fails again the orchestrator escalates AGENT_OUTPUT_INVALID
//     immediately (high-architecture.md §6.8).
//
// Implementations also drive the background healthcheck goroutine and
// maintain the in-memory healthy registry described in §1.3; that registry
// is not part of this interface — it is an implementation detail.
type ProviderRouterPort interface {
	Complete(ctx context.Context, req CompletionRequest) (PrimaryCallResult, error)
	CompleteRepair(ctx context.Context, req CompletionRequest, usedProvider LLMProviderID) (CompletionResponse, error)
}
