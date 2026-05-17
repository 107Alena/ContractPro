// Package summary is Agent 7 — Business Summary (LIC-TASK-031,
// ai-agents-pipeline.md §7, high-architecture.md §6.6/§6.7.2). From the
// classification, the key parameters, the (RAW Agent-5) risk analysis, the
// mandatory-conditions report and a head/tail-compacted slice of the contract
// text it produces ONE plain-language summary (model.Summary{text}, 200..3000
// chars) aimed at a non-legal business reader: what the contract is, its key
// terms, what to watch out for (high/medium risks + missing important
// conditions), and an overall verdict — no legal jargon, no article
// references. Agent 7 is Stage 5, running in PARALLEL with Agent 8 (detailed
// report), after Stage-1 (Agents 1/2) and Stage-3 (Agents 4/5).
//
// The agent is a THIN wrapper over the shared BaseAgent runner
// (internal/agents/base, LIC-TASK-024): Summarizer embeds *base.BaseAgent, so
// it satisfies port.Agent (ID + Run) for free. The only per-agent code is the
// Spec in spec.go — the envelope assembly (summarizerSpec.Parts) and the typed
// decode (summarizerSpec.Decode). Everything invariant-heavy — Prompt Builder,
// Token Estimator, the primary LLM call, schema validation, the sticky 1-shot
// repair loop, the span tree and the four invocation metrics — lives in base,
// exactly once, shared by all 9 agents.
//
// Hermeticity (7-entry, artifacts-PRESENT — the Agent-4/5 EXTRACTED_TEXT-
// consumer CLASS). Like every internal/agents|llm/* sibling, this package
// imports ONLY stdlib + internal/domain/{model,port} + the sibling agent
// packages it composes (base, promptbuilder, prompts, schemas) + the shared
// DP-faithful EXTRACTED_TEXT decoder internal/agents/artifacts. Agent 7
// RE-ADDS artifacts (the deliberate Agent-6 DROP is reversed here — §7
// "Зависимости" lists EXTRACTED_TEXT and the §7 envelope has a
// <contract_document> block): this is NOT a regression toward Agents 4/5, it
// is §7 putting Agent 7 back in the EXTRACTED_TEXT-consumer class (the
// artifacts/CLAUDE.md "Consumers" rule names "agents 4,5 — and 7 with its own
// §7 compaction" as a reuse consumer). Like Agents 1/2/4/5 (and UNLIKE Agent
// 3) it imports promptbuilder ONLY for Content: Agent 7 mints NO structural
// block — <validation_facts> is solely Agent 3's role. It does NOT import
// internal/config (resolved per-agent values are constructor parameters; the
// config→value mapping is app-wiring's job, LIC-TASK-047 — the hermetic
// router.RouterConfig precedent) and it does NOT import the DocumentProcessing
// module (the artifacts package owns the DP-faithful EXTRACTED_TEXT decoder;
// no other artifact is structurally decoded here). TestHermeticImports pins
// the exact 7-entry allowlist (artifacts PRESENT — distinct from Agent 6's
// 6-entry artifacts-free set).
package summary

import (
	"fmt"
	"time"

	"contractpro/legal-intelligence-core/internal/agents/base"
	"contractpro/legal-intelligence-core/internal/agents/prompts"
	"contractpro/legal-intelligence-core/internal/agents/schemas"
	"contractpro/legal-intelligence-core/internal/domain/model"
	"contractpro/legal-intelligence-core/internal/domain/port"
)

// Agent-7 LLM budget, copied verbatim from the ai-agents-pipeline.md §7
// "Бюджеты и параметры LLM" table (the binding per-agent SSOT): Claude
// (sonnet) primary, 1 000 max output tokens, 6 s timeout, and a NON-ZERO
// temperature. The timeout is supplied by app-wiring from
// config.AgentsConfig.Timeouts[AgentSummary] (LIC_AGENT_SUMMARY_TIMEOUT,
// default 6s — configuration.md; config.AgentSummary), not hard-coded here.
// The configured primary provider is LIC_AGENT_SUMMARY_PROVIDER (default
// Claude per ADR-LIC-03), resolved by wiring into modelID — never a literal
// here.
const (
	maxOutputTokens = 1000 // §7 "Max output tokens"

	// temperature is §7's "Temperature | 0.3". Agent 7 is the SECOND
	// non-zero-temperature agent (after Agent 6's 0.2) and the highest so
	// far. Unlike §6 ("0.2 — немного выше 0 для разнообразия формулировок")
	// the §7 budget table gives NO inline rationale: 0.3 is the SSOT table
	// value, full stop. base.NewBaseAgent validates Temperature ∈ [0,1]
	// (base.go), so 0.3 is accepted; a future reviewer must NOT "normalise"
	// this to 0.0 to match Agents 1–5 — the non-zero value is a binding §7
	// requirement, not drift (the Agent-6 MF-D5.1 doc-lock precedent).
	temperature = 0.3
)

// Summarizer is Agent 7. It embeds *base.BaseAgent; the embedded ID() and
// Run() make it a port.Agent the Stage Executor (LIC-TASK-034) wires by
// AgentID. Immutable after NewSummarizer (it adds no mutable state of its own;
// the BaseAgent is itself immutable and concurrency-safe).
type Summarizer struct {
	*base.BaseAgent
}

// Compile-time proof that Summarizer satisfies the uniform agent contract
// (codebase-wide `var _ Port = (*Impl)(nil)` house style; mirrors base.go /
// typeclassifier / keyparams / partyconsistency / mandatoryconditions /
// riskdetection / recommendation).
var _ port.Agent = (*Summarizer)(nil)

// NewSummarizer assembles Agent 7. It loads the embedded system prompt and
// output schema (a missing/empty/invalid asset is a fatal startup error per
// prompts/schemas package contracts — propagated, never swallowed), builds
// the §7 Config, and delegates the wiring validation to base.NewBaseAgent
// (fail-fast: empty model id / non-positive timeout / nil router / a
// Stage≠canonicalStage[AgentSummary] mismatch are rejected at construction,
// not on the first contract).
//
// modelID is the resolved wire model of Agent 7's CONFIGURED PRIMARY provider
// (ADR-LIC-03 default = claude ⇒ LIC_CLAUDE_MODEL, default claude-sonnet-4-6),
// supplied by app-wiring (LIC-TASK-047) — never a literal here.
//
// KNOWN LIMITATION (forward note 1, owner: LIC-TASK-024 / router /
// LIC-TASK-047 — identical to typeclassifier #1 / keyparams /
// partyconsistency / mandatoryconditions / riskdetection / recommendation).
// base.Run unconditionally sets req.Model = cfg.Model and the router forwards
// the request UNCHANGED to every provider in the fallback chain; each
// provider's chooseModel lets a non-empty req.Model OVERRIDE that provider's
// env-pinned default. So on a fallback to a non-primary provider the primary's
// model id is sent verbatim (invalid for that vendor → that fallback hop
// fails), which contradicts ADR-LIC-03 / llm-provider-abstraction.md §1.3.
// Passing the primary provider's resolved default here is the least-bad
// choice that is correct on the primary path and does not modify base; the
// proper fix is a base/router change out of scope for this task.
//
// Constructor name is the stutter-free NewTypeName (feedback_constructors.md /
// the codebase-wide convention — NewClassifier / NewExtractor / NewChecker /
// NewDetector / NewRecommender / NewSummarizer; never bare New, never the
// package-stuttering NewSummary). deps is base.Deps (the established injection
// seam shared by base and router) rather than a parallel functional-Option
// API: it already bundles the required Router plus the optional
// Metrics/Tracer/Estimator/RepairMetrics seams whose concrete adapters
// LIC-TASK-047 supplies; every nil field degrades to its zero-dependency
// default inside base.
func NewSummarizer(modelID string, timeout time.Duration, deps base.Deps) (*Summarizer, error) {
	system, err := prompts.LoadPrompt(model.AgentSummary)
	if err != nil {
		return nil, fmt.Errorf("summary: load system prompt: %w", err)
	}
	schema, err := schemas.LoadSchema(model.AgentSummary)
	if err != nil {
		return nil, fmt.Errorf("summary: load output schema: %w", err)
	}

	cfg := base.Config{
		AgentID:     model.AgentSummary,
		Stage:       model.StageAgentSummary,
		System:      system,
		Schema:      schema,
		Model:       modelID,
		MaxTokens:   maxOutputTokens,
		Temperature: temperature,
		Timeout:     timeout,
	}

	ba, err := base.NewBaseAgent(cfg, summarizerSpec{}, deps)
	if err != nil {
		return nil, fmt.Errorf("summary: %w", err)
	}
	return &Summarizer{BaseAgent: ba}, nil
}
