// Package typeclassifier is Agent 1 — Contract Type Classifier (LIC-TASK-025,
// ai-agents-pipeline.md §1, high-architecture.md §6.6). It determines the
// Russian-civil-law contract type (one of the 12-value whitelist) and a
// confidence score from a compacted slice of the contract text plus the
// document's section titles.
//
// The agent is a THIN wrapper over the shared BaseAgent runner
// (internal/agents/base, LIC-TASK-024): Classifier embeds *base.BaseAgent, so
// it satisfies port.Agent (ID + Run) for free. The only per-agent code is the
// Spec in spec.go — the envelope assembly (classifierSpec.Parts) and the typed
// decode (classifierSpec.Decode). Everything invariant-heavy — Prompt Builder,
// Token Estimator, the primary LLM call, schema validation, the sticky 1-shot
// repair loop, the span tree and the four invocation metrics — lives in base,
// exactly once, shared by all 9 agents.
//
// Hermeticity. Like every internal/agents|llm/* sibling, this package imports
// ONLY stdlib + internal/domain/{model,port} + the sibling agent packages it
// composes (base, promptbuilder, prompts, schemas). It does NOT import
// internal/config: resolved per-agent values (the primary-provider model id,
// the timeout) are constructor parameters and the config→value mapping is
// app-wiring's job (LIC-TASK-047), mirroring how router.RouterConfig is
// assembled by wiring and the router package itself reads no env.
// TestHermeticImports pins the exact allowlist.
package typeclassifier

import (
	"fmt"
	"time"

	"contractpro/legal-intelligence-core/internal/agents/base"
	"contractpro/legal-intelligence-core/internal/agents/prompts"
	"contractpro/legal-intelligence-core/internal/agents/schemas"
	"contractpro/legal-intelligence-core/internal/domain/model"
	"contractpro/legal-intelligence-core/internal/domain/port"
)

// Agent-1 LLM budget, copied from the ai-agents-pipeline.md §1 "Бюджеты и
// параметры LLM" table (the binding per-agent SSOT). NOTE: §0.6's aggregate
// token-budget summary lists 200 output tokens for Agent 1 — that figure
// disagrees with the §1 per-agent table; the §1 table wins (a docs-side
// reconciliation note, not a code concern).
const (
	maxOutputTokens = 400 // §1 "Max output tokens"
	temperature     = 0.0 // §1 "Temperature" — deterministic classification
)

// Classifier is Agent 1. It embeds *base.BaseAgent; the embedded ID() and
// Run() make it a port.Agent the Stage Executor (LIC-TASK-034) wires by
// AgentID. Immutable after NewClassifier (it adds no mutable state of its own;
// the BaseAgent is itself immutable and concurrency-safe).
type Classifier struct {
	*base.BaseAgent
}

// Compile-time proof that Classifier satisfies the uniform agent contract
// (codebase-wide `var _ Port = (*Impl)(nil)` house style; mirrors base.go).
var _ port.Agent = (*Classifier)(nil)

// NewClassifier assembles Agent 1. It loads the embedded system prompt and
// output schema (a missing/empty/invalid asset is a fatal startup error per
// prompts/schemas package contracts — propagated, never swallowed), builds the
// §1 Config, and delegates the wiring validation to base.NewBaseAgent
// (fail-fast: empty model id / non-positive timeout / nil router are rejected
// at construction, not on the first contract).
//
// modelID is the resolved wire model of Agent 1's CONFIGURED PRIMARY provider
// (ADR-LIC-03 default = claude ⇒ LIC_CLAUDE_MODEL, default claude-sonnet-4-6),
// supplied by app-wiring (LIC-TASK-047) — never a literal here.
//
// KNOWN LIMITATION (forward note, owner: LIC-TASK-024 / router / LIC-TASK-047).
// base.Run unconditionally sets req.Model = cfg.Model and the router forwards
// the request UNCHANGED to every provider in the fallback chain; each
// provider's chooseModel lets a non-empty req.Model OVERRIDE that provider's
// own env-pinned default (LIC_OPENAI_MODEL / LIC_GEMINI_MODEL). So on a
// fallback to a non-primary provider the primary's model id is sent verbatim
// (invalid for that vendor → that fallback hop fails), which contradicts
// ADR-LIC-03 / llm-provider-abstraction.md §1.3. Passing the primary
// provider's resolved default here is the least-bad choice that is correct on
// the primary path and does not modify base; the proper fix (base leaving
// req.Model=="" so each adapter's chooseModel falls through to its own
// env-pinned default, or the router rewriting req.Model per provider) is a
// base/router change out of scope for this task.
//
// Constructor name is the stutter-free NewTypeName (feedback_constructors.md /
// the codebase-wide convention — NewProviderRouter / NewBuilder / NewValidator;
// never bare New). deps is base.Deps (the established injection seam shared by
// base and router) rather than a parallel functional-Option API: it already
// bundles the required Router plus the optional Metrics/Tracer/Estimator/
// RepairMetrics seams whose concrete adapters LIC-TASK-047 supplies; every nil
// field degrades to its zero-dependency default inside base.
func NewClassifier(modelID string, timeout time.Duration, deps base.Deps) (*Classifier, error) {
	system, err := prompts.LoadPrompt(model.AgentTypeClassifier)
	if err != nil {
		return nil, fmt.Errorf("typeclassifier: load system prompt: %w", err)
	}
	schema, err := schemas.LoadSchema(model.AgentTypeClassifier)
	if err != nil {
		return nil, fmt.Errorf("typeclassifier: load output schema: %w", err)
	}

	cfg := base.Config{
		AgentID:     model.AgentTypeClassifier,
		Stage:       model.StageAgentTypeClassifier,
		System:      system,
		Schema:      schema,
		Model:       modelID,
		MaxTokens:   maxOutputTokens,
		Temperature: temperature,
		Timeout:     timeout,
	}

	ba, err := base.NewBaseAgent(cfg, classifierSpec{}, deps)
	if err != nil {
		return nil, fmt.Errorf("typeclassifier: %w", err)
	}
	return &Classifier{BaseAgent: ba}, nil
}
