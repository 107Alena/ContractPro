// Package keyparams is Agent 2 — Key Parameters Extractor (LIC-TASK-026,
// ai-agents-pipeline.md §2, high-architecture.md §6.6). It extracts the
// contract's key parameters — parties, subject, price, duration, penalties,
// jurisdiction — plus the LIC-internal extras consumed by downstream agents
// 3/4/8 (applicable law, termination, acceptance procedure, party roles with
// raw INN/OGRN, key dates) from the FULL semantic tree and the extracted
// contract text.
//
// The agent is a THIN wrapper over the shared BaseAgent runner
// (internal/agents/base, LIC-TASK-024): Extractor embeds *base.BaseAgent, so
// it satisfies port.Agent (ID + Run) for free. The only per-agent code is the
// Spec in spec.go — the envelope assembly (extractorSpec.Parts) and the typed
// decode (extractorSpec.Decode). Everything invariant-heavy — Prompt Builder,
// Token Estimator, the primary LLM call, schema validation, the sticky 1-shot
// repair loop, the span tree and the four invocation metrics — lives in base,
// exactly once, shared by all 9 agents.
//
// Hermeticity. Like every internal/agents|llm/* sibling, this package imports
// ONLY stdlib + internal/domain/{model,port} + the sibling agent packages it
// composes (base, promptbuilder, prompts, schemas) + the shared DP-faithful
// artifacts decoder (internal/agents/artifacts, the LIC-TASK-026 steward
// decision per typeclassifier/CLAUDE.md forward-note #3). It does NOT import
// internal/config (resolved per-agent values are constructor parameters; the
// config→value mapping is app-wiring's job, LIC-TASK-047 — the hermetic
// router.RouterConfig precedent) and it does NOT import the
// DocumentProcessing module (the artifacts package owns the local minimal
// DP-faithful structs). TestHermeticImports pins the exact allowlist.
package keyparams

import (
	"fmt"
	"time"

	"contractpro/legal-intelligence-core/internal/agents/base"
	"contractpro/legal-intelligence-core/internal/agents/prompts"
	"contractpro/legal-intelligence-core/internal/agents/schemas"
	"contractpro/legal-intelligence-core/internal/domain/model"
	"contractpro/legal-intelligence-core/internal/domain/port"
)

// Agent-2 LLM budget, copied verbatim from the ai-agents-pipeline.md §2
// "Бюджеты и параметры LLM" table (the binding per-agent SSOT): Claude
// (sonnet) primary, deterministic temperature, 2 000 max output tokens,
// 8 s timeout. The timeout is supplied by app-wiring from
// config.AgentsConfig.Timeouts[AgentKeyParams] (LIC_AGENT_KEY_PARAMS_TIMEOUT,
// default 8s — configuration.md), not hard-coded here.
const (
	maxOutputTokens = 2000 // §2 "Max output tokens"
	temperature     = 0.0  // §2 "Temperature" — deterministic extraction
)

// Extractor is Agent 2. It embeds *base.BaseAgent; the embedded ID() and
// Run() make it a port.Agent the Stage Executor (LIC-TASK-034) wires by
// AgentID. Immutable after NewExtractor (it adds no mutable state of its own;
// the BaseAgent is itself immutable and concurrency-safe).
type Extractor struct {
	*base.BaseAgent
}

// Compile-time proof that Extractor satisfies the uniform agent contract
// (codebase-wide `var _ Port = (*Impl)(nil)` house style; mirrors base.go /
// typeclassifier).
var _ port.Agent = (*Extractor)(nil)

// NewExtractor assembles Agent 2. It loads the embedded system prompt and
// output schema (a missing/empty/invalid asset is a fatal startup error per
// prompts/schemas package contracts — propagated, never swallowed), builds the
// §2 Config, and delegates the wiring validation to base.NewBaseAgent
// (fail-fast: empty model id / non-positive timeout / nil router / a
// Stage≠canonicalStage[AgentKeyParams] mismatch are rejected at construction,
// not on the first contract).
//
// modelID is the resolved wire model of Agent 2's CONFIGURED PRIMARY provider
// (ADR-LIC-03 default = claude ⇒ LIC_CLAUDE_MODEL, default claude-sonnet-4-6),
// supplied by app-wiring (LIC-TASK-047) — never a literal here.
//
// KNOWN LIMITATION (forward note, owner: LIC-TASK-024 / router / LIC-TASK-047
// — identical to typeclassifier forward-note #1). base.Run unconditionally
// sets req.Model = cfg.Model and the router forwards the request UNCHANGED to
// every provider in the fallback chain; each provider's chooseModel lets a
// non-empty req.Model OVERRIDE that provider's env-pinned default
// (LIC_OPENAI_MODEL / LIC_GEMINI_MODEL). So on a fallback to a non-primary
// provider the primary's model id is sent verbatim (invalid for that vendor →
// that fallback hop fails), which contradicts ADR-LIC-03 /
// llm-provider-abstraction.md §1.3. Passing the primary provider's resolved
// default here is the least-bad choice that is correct on the primary path
// and does not modify base; the proper fix (base leaving req.Model=="" so
// each adapter's chooseModel falls through to its own env-pinned default, or
// the router rewriting req.Model per provider) is a base/router change out of
// scope for this task.
//
// Constructor name is the stutter-free NewTypeName (feedback_constructors.md /
// the codebase-wide convention — NewClassifier / NewProviderRouter /
// NewBuilder / NewValidator; never bare New). deps is base.Deps (the
// established injection seam shared by base and router) rather than a parallel
// functional-Option API: it already bundles the required Router plus the
// optional Metrics/Tracer/Estimator/RepairMetrics seams whose concrete
// adapters LIC-TASK-047 supplies; every nil field degrades to its
// zero-dependency default inside base.
func NewExtractor(modelID string, timeout time.Duration, deps base.Deps) (*Extractor, error) {
	system, err := prompts.LoadPrompt(model.AgentKeyParams)
	if err != nil {
		return nil, fmt.Errorf("keyparams: load system prompt: %w", err)
	}
	schema, err := schemas.LoadSchema(model.AgentKeyParams)
	if err != nil {
		return nil, fmt.Errorf("keyparams: load output schema: %w", err)
	}

	cfg := base.Config{
		AgentID:     model.AgentKeyParams,
		Stage:       model.StageAgentKeyParams,
		System:      system,
		Schema:      schema,
		Model:       modelID,
		MaxTokens:   maxOutputTokens,
		Temperature: temperature,
		Timeout:     timeout,
	}

	ba, err := base.NewBaseAgent(cfg, extractorSpec{}, deps)
	if err != nil {
		return nil, fmt.Errorf("keyparams: %w", err)
	}
	return &Extractor{BaseAgent: ba}, nil
}
