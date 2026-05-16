package schemavalidator

import (
	"context"
	"fmt"
	"slices"

	"contractpro/legal-intelligence-core/internal/domain/model"
	"contractpro/legal-intelligence-core/internal/domain/port"
)

// repairPromptTemplate is the repair prompt, BYTE-EXACT from
// error-handling.md §5.2 (the verbatim SSOT — NOT the tasks.json paraphrase,
// NOT the high-architecture.md §6.8 one-liner). The single placeholder
// %s is the {validation_errors_pretty_printed} slot (SchemaViolation.Pretty).
//
// The hard line break inside "без объяснений и\npreamble" is present in §5.2
// and preserved verbatim: deviating from a frozen prompt SSOT is a larger
// risk than the mid-sentence wrap (code-architect item 7). seams/repair
// tests pin this string against §5.2.
const repairPromptTemplate = `Твой предыдущий ответ не прошёл валидацию по схеме.

Ошибки валидации:
%s

Исправь ответ. Возвращай ТОЛЬКО валидный JSON по исходной схеме, без объяснений и
preamble. Не добавляй markdown. Не цитируй ошибки в ответе.`

// Repairer is the narrow seam RepairLoop needs from the Provider Router: just
// the sticky single-shot CompleteRepair (no fallback, no same-provider retry —
// the router owns that contract, LIC-TASK-019). Declaring the minimum the
// consumer needs (not depending on the full port.ProviderRouterPort) mirrors
// cost.Recorder / ratelimit.Observer; the
// `var _ Repairer = port.ProviderRouterPort(nil)` assertion belongs in
// app-wiring (LIC-TASK-047), not here, so this package stays free of an
// internal/llm/router import.
type Repairer interface {
	CompleteRepair(ctx context.Context, req port.CompletionRequest, usedProvider port.LLMProviderID) (port.CompletionResponse, error)
}

// RepairLoop wires a Validator, a Repairer (the sticky router) and the
// telemetry seam into the §6.8 one-shot repair flow. Immutable after
// construction → safe for concurrent use across the parallel agent pipeline.
type RepairLoop struct {
	validator *Validator
	repairer  Repairer
	metrics   Metrics
}

// NewRepairLoop fails fast on a nil repairer — a repair loop with no router
// is a wiring bug that must not start silently (mirrors cost.NewTracker /
// router.NewProviderRouter fail-fast). A nil metrics seam degrades to a
// no-op so RepairLoop is usable before LIC-TASK-047 wires Prometheus.
func NewRepairLoop(repairer Repairer, metrics Metrics) (*RepairLoop, error) {
	if repairer == nil {
		return nil, fmt.Errorf("schemavalidator: repairer must not be nil")
	}
	if metrics == nil {
		metrics = noopMetrics{}
	}
	return &RepairLoop{
		validator: NewValidator(),
		repairer:  repairer,
		metrics:   metrics,
	}, nil
}

// Run validates primary.Response.Content against schema and, on a
// SchemaViolation, performs the §6.8 one-shot repair.
//
// Outcomes:
//   - primary valid → (primary.Response, nil); NO repair turn issued, so
//     NO repair metric is touched (code-architect MF-4).
//   - schema is a broken embedded document → INTERNAL_ERROR DomainError
//     (is_retryable=true), wrapping the *SchemaCompileError; NO repair turn
//     (code-architect MF-3).
//   - primary SchemaViolation → one sticky CompleteRepair on
//     primary.UsedProvider:
//   - CompleteRepair err != nil → repair_provider_error;
//     AGENT_OUTPUT_INVALID (is_retryable=true) wrapping the provider error,
//     NO fallback (§6.8; the router already enforces sticky/no-fallback).
//   - CompleteRepair err == nil and repaired content valid → repaired_ok;
//     (repairedResponse, nil).
//   - CompleteRepair err == nil and repaired content still invalid →
//     repair_failed; AGENT_OUTPUT_INVALID (is_retryable=true). Hard limit
//     N=1 — no second repair (§5.4 / ADR-LIC-04).
//
// stage is the agent's STAGE_AGENT_* (the caller — BaseAgent, LIC-TASK-024 —
// owns its stage); it is stamped on every DomainError so the Orchestrator
// can publish lic.events.status-changed correctly.
func (rl *RepairLoop) Run(
	ctx context.Context,
	agentID model.AgentID,
	stage model.Stage,
	schema []byte,
	originalReq port.CompletionRequest,
	primary port.PrimaryCallResult,
) (port.CompletionResponse, error) {
	agentLabel := agentID.String()
	provLabel := primary.UsedProvider.String()

	switch err := rl.validator.Validate(schema, []byte(primary.Response.Content)); {
	case err == nil:
		// Happy path: the primary response already conformed. No repair
		// turn is issued ⇒ no repair_attempts / repair_outcome series.
		return primary.Response, nil

	case isCompileError(err):
		// Broken embedded schema — a LIC build defect, not a model
		// mistake. Never repair; escalate as INTERNAL_ERROR (MF-3).
		return port.CompletionResponse{}, model.
			NewDomainError(model.ErrCodeInternal, stage).
			WithRetryable(true).
			WithCause(err).
			WithAttribute("agent_id", agentLabel)

	default:
		// SchemaViolation — the only repair trigger. err is the FIRST
		// violation; its Pretty() fills the §5.2 prompt placeholder.
		// Validate's contract is exhaustive (nil | *SchemaCompileError |
		// *SchemaViolation) and the earlier arms consumed the first two, so
		// this is always a *SchemaViolation. The !ok guard is defence in
		// depth against a future Validate return-shape drift: surface it as
		// INTERNAL_ERROR rather than nil-deref viol.Pretty() (code-reviewer
		// LOW-3 / golang-pro nit 3).
		viol, ok := AsSchemaViolation(err)
		if !ok {
			return port.CompletionResponse{}, model.
				NewDomainError(model.ErrCodeInternal, stage).
				WithRetryable(true).
				WithCause(err).
				WithAttribute("agent_id", agentLabel)
		}
		repairReq := buildRepairRequest(originalReq, primary.Response.Content, viol.Pretty())

		// A repair turn IS being issued → attempts++ exactly here (MF-4).
		rl.metrics.RepairAttempt(agentLabel, provLabel)

		resp, rerr := rl.repairer.CompleteRepair(ctx, repairReq, primary.UsedProvider)
		if rerr != nil {
			// err != nil ⇒ the repair call failed at the provider.
			// Strictly distinct from a second SchemaViolation (MF-2).
			rl.metrics.RepairOutcome(agentLabel, provLabel, OutcomeProviderError.String())
			return port.CompletionResponse{}, model.
				NewDomainError(model.ErrCodeAgentOutputInvalid, stage).
				WithRetryable(true).
				WithCause(rerr).
				WithAttribute("agent_id", agentLabel).
				WithAttribute("used_provider", provLabel)
		}

		// err == nil ⇒ validate the repaired content (second & final).
		switch verr := rl.validator.Validate(schema, []byte(resp.Content)); {
		case verr == nil:
			rl.metrics.RepairOutcome(agentLabel, provLabel, OutcomeRepairedOK.String())
			return resp, nil

		case isCompileError(verr):
			// Defence in depth: impossible in practice (the identical
			// schema compiled on the first call, gojsonschema is
			// deterministic), but if it ever happens it is still a LIC
			// schema defect, not a repair failure → INTERNAL_ERROR.
			return port.CompletionResponse{}, model.
				NewDomainError(model.ErrCodeInternal, stage).
				WithRetryable(true).
				WithCause(verr).
				WithAttribute("agent_id", agentLabel)

		default:
			// Second SchemaViolation — hard limit N=1 reached.
			rl.metrics.RepairOutcome(agentLabel, provLabel, OutcomeRepairFailed.String())
			return port.CompletionResponse{}, model.
				NewDomainError(model.ErrCodeAgentOutputInvalid, stage).
				WithRetryable(true).
				WithCause(verr).
				WithAttribute("agent_id", agentLabel).
				WithAttribute("used_provider", provLabel)
		}
	}
}

// isCompileError reports whether err is (or wraps) a *SchemaCompileError.
func isCompileError(err error) bool {
	_, ok := AsSchemaCompileError(err)
	return ok
}

// buildRepairRequest constructs the §5.2 / §6.8 repair CompletionRequest.
//
// SSOT reconciliation (code-architect MF-1). high-architecture.md §6.8 and
// port.Turn / router.repair.go godoc give a lossy 2-element shorthand
// "PriorTurns = [{Assistant,invalid},{User,repair_prompt}]". The shipped
// adapters (claude/openai/gemini payload.go) ALL build the wire conversation
// as [System?] + PriorTurns... + a final appended {user, req.User}. To
// produce a valid, provider-portable, alternating sequence that ALSO honours
// error-handling.md §5.2 ("User message предыдущего вызова — сохраняется.
// Добавляется assistant message с raw response, затем user message с текстом
// repair-prompt"), the only correct construction is:
//
//	PriorTurns = origPriorTurns + [{User, origUser}, {Assistant, invalid}]
//	User       = repair_prompt   (adapters append this as the final user turn)
//
// → wire: [..origPriors, user:origUser, assistant:invalid, user:repair].
// §5.2 prose wins on substance (the model needs the original contract data
// to repair against). The 2-element godoc/ §6.8 forms are documented as
// lossy in CLAUDE.md and corrected in their source godocs by this task.
//
// §5.3: same model, same MaxTokens, same StopSequences/JSONMode/JSONSchema/
// System; Temperature is forced to 0.0 for determinism. The returned request
// differs from originalReq in EXACTLY {PriorTurns, User, Temperature}.
func buildRepairRequest(originalReq port.CompletionRequest, invalidResponse, validationErrors string) port.CompletionRequest {
	repairReq := originalReq // value copy

	prior := slices.Clone(originalReq.PriorTurns)
	prior = append(prior,
		port.Turn{Role: port.RoleUser, Content: originalReq.User},
		port.Turn{Role: port.RoleAssistant, Content: invalidResponse},
	)
	repairReq.PriorTurns = prior
	repairReq.User = fmt.Sprintf(repairPromptTemplate, validationErrors)
	repairReq.Temperature = 0.0

	return repairReq
}
