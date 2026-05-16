package router

import (
	"context"
	"fmt"

	"contractpro/legal-intelligence-core/internal/domain/port"
)

// CompleteRepair re-issues the agent call STICKY on usedProvider — the
// provider that served the original successful Complete (OQ-10 / §2.1).
// req is the repair request fully built by the Schema Validator
// (LIC-TASK-023, internal/agents/schemavalidator): req.PriorTurns carries
// [..origPriors, {User, origUser}, {Assistant, invalid_response}] and
// req.User carries the §5.2 repair prompt (the adapter appends req.User as
// the final user turn, so the wire conversation ends user→assistant→user;
// see port.Turn godoc for the full reconciliation). Switching providers here
// would break conversation continuity, so there is NO fallback and NO
// same-provider retry — it is a single shot (§2.1 CompleteRepair pseudocode
// + §5.4 hard repair limit of 1; code-architect confirmed).
//
// Cost telemetry: per metrics/llm.go SSOT ("CallsTotal — `repair` increments
// on every CompleteRepair invocation regardless of its sub-outcome") EVERY
// invocation records lic_llm_calls_total{outcome=repair} via
// UsageTracker.ObserveCall — success included. It deliberately does NOT call
// ObserveSuccess: that method also emits {outcome=success} (wrong label for
// a repair) and the cost package models repair calls as non-usage
// (cost.go ObserveCall godoc). The minor cost undercount of a *successful*
// repair's tokens is a pre-existing, documented property of the cost
// package (LIC-TASK-018), not introduced here — recorded in CLAUDE.md.
func (r *ProviderRouter) CompleteRepair(
	ctx context.Context,
	req port.CompletionRequest,
	usedProvider port.LLMProviderID,
) (port.CompletionResponse, error) {
	provider, ok := r.providers[usedProvider]
	// metrics/llm.go SSOT: lic_llm_calls_total{repair} increments on EVERY
	// CompleteRepair invocation regardless of sub-outcome — including the
	// two pre-wire escalations below (golang-pro N-1). usedProvider is
	// always a registered, typed LLMProviderID in correct wiring, so the
	// {provider} label stays bounded even on the unregistered-bug path.
	r.usage.ObserveCall(usedProvider, req.Model, req.AgentID, OutcomeRepair)

	if !ok {
		// The caller passed back a provider the router never registered —
		// a wiring/caller bug, not a provider fault. Literal struct: the
		// MALFORMED_REQUEST catalog row is already {false,false}, but we
		// keep it explicit and symmetric with the unhealthy escalation.
		return port.CompletionResponse{}, &port.LLMProviderError{
			Code:             port.LLMErrorMalformedRequest,
			Retryable:        false,
			FallbackEligible: false,
			Wrapped:          fmt.Errorf("router: repair on unregistered provider %q", usedProvider),
		}
	}

	if !r.registry.isHealthy(usedProvider) {
		// The provider that succeeded seconds ago is suddenly unhealthy.
		// Escalate AGENT_OUTPUT_INVALID immediately — sticky semantics
		// forbid a fallback hop here (§2.1). LITERAL struct, NOT
		// NewLLMProviderError: the SERVER_ERROR catalog row is
		// {retryable:true, fallbackEligible:true} (llm_errors.go) — the
		// opposite of what the sticky escalation requires (code-architect
		// MF-4; the documented llm_errors.go literal-override path).
		return port.CompletionResponse{}, &port.LLMProviderError{
			Code:             port.LLMErrorServerError,
			Retryable:        false,
			FallbackEligible: false,
			Wrapped:          fmt.Errorf("router: repair provider %q is unhealthy", usedProvider),
		}
	}

	if err := r.rl.Wait(ctx, usedProvider); err != nil {
		// §2.1 returns the rate-limit error as-is; it is already a typed
		// *LLMProviderError (RATE_LIMIT wrapping ctx.Err, or MALFORMED for
		// an unknown provider — impossible here, we resolved it above).
		// The repair call was already recorded at the top of the method.
		return port.CompletionResponse{}, err
	}

	resp, err := provider.Complete(ctx, req) // single shot — no retry, no fallback
	if err != nil {
		if pe, ok := port.AsLLMProviderError(err); ok {
			r.mx.ProviderFailed(usedProvider, pe.Code)
			r.registry.recordFailure(usedProvider, pe, nil)
		} else {
			r.mx.ProviderFailed(usedProvider, unknownCode)
			r.registry.recordFailure(usedProvider, nil, err)
		}
		return port.CompletionResponse{}, err
	}
	return resp, nil
}
