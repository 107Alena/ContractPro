// Package riskdelta is Agent 9 — Risk Delta (LIC-TASK-033,
// ai-agents-pipeline.md §9, high-architecture.md §6.6/§8.7). From the parent
// (base) version's MERGED risk analysis and the current (target) version's
// MERGED risk analysis — plus the two version UUIDs — it produces ONE
// model.RiskDelta: the risks added / removed / level-changed since the parent
// version, plus the before/after profile counts and a short Russian summary.
// It is the LAST of the 9 agents and runs ONLY in RE_CHECK mode (Stage 6),
// gated by the §8.7 RE_CHECK gate (parent_version_id != null AND the parent
// RISK_ANALYSIS was retrieved from DM).
//
// Agent 9 is NON-CRITICAL (error-handling.md groups agents 3 & 9: a timeout /
// failure does NOT fail the pipeline — it continues with risk_delta=null and a
// RE_CHECK_PARENT_ANALYSIS_MISSING warning). That graceful degradation and the
// run/skip gate are owned by the Stage Executor (LIC-TASK-034) and the
// Pipeline Orchestrator (LIC-TASK-036), NOT by this package: a nil parent
// input reaching Parts means the gate failed to skip Agent 9 — a wiring defect
// surfaced as INTERNAL_ERROR (forward note 2).
//
// The agent is a THIN wrapper over the shared BaseAgent runner
// (internal/agents/base, LIC-TASK-024): RiskDeltaComparator embeds
// *base.BaseAgent, so it satisfies port.Agent (ID + Run) for free. The only
// per-agent code is the Spec in spec.go — the §9 envelope assembly
// (riskDeltaComparatorSpec.Parts) and the typed decode
// (riskDeltaComparatorSpec.Decode). Everything invariant-heavy — Prompt
// Builder, Token Estimator, the primary LLM call, schema validation, the
// sticky 1-shot repair loop, the span tree and the four invocation metrics —
// lives in base, exactly once, shared by all 9 agents.
//
// Hermeticity (6-entry, artifacts-FREE — the PUREST non-DM-artifact agent of
// the 9; code-architect D7). Like every internal/agents|llm/* sibling, this
// package imports ONLY stdlib + internal/domain/{model,port} + the sibling
// agent packages it composes (base, promptbuilder, prompts, schemas). Agent 9
// consumes ZERO DM artifacts (§9 "Зависимости", ai-agents-pipeline.md:1510-1511
// — input is only the two RiskAnalysis structs + the two version ids; there is
// no SEMANTIC_TREE / EXTRACTED_TEXT / PROCESSING_WARNINGS), so — even more than
// Agent 8 — internal/agents/artifacts is dead weight here and is DELIBERATELY
// absent. Dropping artifacts is the deliberate "non-artifact-consumer" class,
// NOT an omission: a future reviewer must NOT re-add it (the riskdetection
// "deliberate absence is a class, not an omission" house style). Like Agents
// 1/2/4/5/6/8 (and UNLIKE Agent 3) it imports promptbuilder ONLY for Content:
// Agent 9 mints NO structural block — <validation_facts> is solely Agent 3's
// role. It does NOT import internal/config (resolved per-agent values are
// constructor parameters; the config→value mapping is app-wiring's job,
// LIC-TASK-047 — the hermetic router.RouterConfig precedent), nor the
// DocumentProcessing module (no contract tree is consumed). TestHermeticImports
// pins the exact (6-entry, artifacts-free) allowlist.
package riskdelta

import (
	"fmt"
	"time"

	"contractpro/legal-intelligence-core/internal/agents/base"
	"contractpro/legal-intelligence-core/internal/agents/prompts"
	"contractpro/legal-intelligence-core/internal/agents/schemas"
	"contractpro/legal-intelligence-core/internal/domain/model"
	"contractpro/legal-intelligence-core/internal/domain/port"
)

// Agent-9 LLM budget, copied verbatim from the ai-agents-pipeline.md §9
// "Бюджеты и параметры LLM" table (the binding per-agent SSOT): Claude
// (sonnet) primary, 1 500 max output tokens, 8 s timeout, temperature 0.0.
// The timeout is supplied by app-wiring from
// config.AgentsConfig.Timeouts[config.AgentRiskDelta]
// (LIC_AGENT_RISK_DELTA_TIMEOUT, default 8s — config/agents.go:59, mirroring
// configuration.md §2.11), not hard-coded here. KNOWN DOC CONFLICT (forward
// note 5): ai-agents-pipeline.md §9 table says 8s but high-architecture.md
// §4.3.x cites LIC_AGENT_RISK_DELTA_TIMEOUT=10s — the value is a constructor
// parameter so the code is correct against whatever config.AgentRiskDelta
// actually resolves to (default 8s); the §9-vs-§4.3.x discrepancy is recorded
// for a doc-reconciliation task, NOT silently chosen here. The configured
// primary provider is LIC_AGENT_RISK_DELTA_PROVIDER (default Claude per
// ADR-LIC-03), resolved by wiring into modelID — never a literal here.
const (
	maxOutputTokens = 1500 // §9 "Max output tokens"

	// temperature is §9's "Temperature | 0.0". Agent 9 is a deterministic
	// comparison/diff pass (matching base↔target risks, exact profile counts),
	// NOT a phrasing-variety task — so §9 specifies 0.0, like Agents 1-5 and 8
	// and UNLIKE the wording-variety Agents 6 (0.2) and 7 (0.3). A future
	// reviewer must NOT "carry forward" a non-zero temperature from a
	// temperature-bearing agent: 0.0 is the binding §9 SSOT table value (the
	// same doc-lock discipline as Agent 8's D8). base.NewBaseAgent validates
	// Temperature ∈ [0,1] (base.go), so 0.0 is accepted.
	temperature = 0.0
)

// RiskDeltaComparator is Agent 9. It embeds *base.BaseAgent; the embedded ID()
// and Run() make it a port.Agent the Stage Executor (LIC-TASK-034) wires by
// AgentID. Immutable after NewRiskDeltaComparator (it adds no mutable state of
// its own; the BaseAgent is itself immutable and concurrency-safe).
type RiskDeltaComparator struct {
	*base.BaseAgent
}

// Compile-time proof that RiskDeltaComparator satisfies the uniform agent
// contract (codebase-wide `var _ Port = (*Impl)(nil)` house style; mirrors
// base.go / typeclassifier / keyparams / partyconsistency /
// mandatoryconditions / riskdetection / recommendation / summary /
// detailedreport).
var _ port.Agent = (*RiskDeltaComparator)(nil)

// NewRiskDeltaComparator assembles Agent 9. It loads the embedded system
// prompt and output schema (a missing/empty/invalid asset is a fatal startup
// error per prompts/schemas package contracts — propagated, never swallowed),
// builds the §9 Config, and delegates the wiring validation to
// base.NewBaseAgent (fail-fast: empty model id / non-positive timeout / nil
// router / a Stage≠canonicalStage[AgentRiskDelta] mismatch are rejected at
// construction, not on the first contract).
//
// modelID is the resolved wire model of Agent 9's CONFIGURED PRIMARY provider
// (ADR-LIC-03 default = claude ⇒ LIC_CLAUDE_MODEL, default claude-sonnet-4-6),
// supplied by app-wiring (LIC-TASK-047) — never a literal here.
//
// KNOWN LIMITATION (forward note 1, owner: LIC-TASK-024 / router /
// LIC-TASK-047 — identical to the prior 8 packages). base.Run unconditionally
// sets req.Model = cfg.Model and the router forwards the request UNCHANGED to
// every provider in the fallback chain; each provider's chooseModel lets a
// non-empty req.Model OVERRIDE that provider's env-pinned default. So on a
// fallback to a non-primary provider the primary's model id is sent verbatim
// (invalid for that vendor → that fallback hop fails), which contradicts
// ADR-LIC-03 / llm-provider-abstraction.md §1.3. Passing the primary
// provider's resolved default here is the least-bad choice that is correct on
// the primary path and does not modify base; the proper fix is a base/router
// change out of scope for this task.
//
// Constructor name is the stutter-free NewTypeName (feedback_constructors.md /
// the codebase-wide convention — NewClassifier / NewExtractor / NewChecker /
// NewDetector / NewRecommender / NewSummarizer / NewDetailedReporter /
// NewRiskDeltaComparator; never bare New, never the package-and-model-stuttering
// NewRiskDelta). deps is base.Deps (the established injection seam shared by
// base and router) rather than a parallel functional-Option API: it already
// bundles the required Router plus the optional Metrics/Tracer/Estimator/
// RepairMetrics seams whose concrete adapters LIC-TASK-047 supplies; every nil
// field degrades to its zero-dependency default inside base.
func NewRiskDeltaComparator(modelID string, timeout time.Duration, deps base.Deps) (*RiskDeltaComparator, error) {
	system, err := prompts.LoadPrompt(model.AgentRiskDelta)
	if err != nil {
		return nil, fmt.Errorf("riskdelta: load system prompt: %w", err)
	}
	schema, err := schemas.LoadSchema(model.AgentRiskDelta)
	if err != nil {
		return nil, fmt.Errorf("riskdelta: load output schema: %w", err)
	}

	cfg := base.Config{
		AgentID:     model.AgentRiskDelta,
		Stage:       model.StageAgentRiskDelta,
		System:      system,
		Schema:      schema,
		Model:       modelID,
		MaxTokens:   maxOutputTokens,
		Temperature: temperature,
		Timeout:     timeout,
	}

	ba, err := base.NewBaseAgent(cfg, riskDeltaComparatorSpec{}, deps)
	if err != nil {
		return nil, fmt.Errorf("riskdelta: %w", err)
	}
	return &RiskDeltaComparator{BaseAgent: ba}, nil
}
