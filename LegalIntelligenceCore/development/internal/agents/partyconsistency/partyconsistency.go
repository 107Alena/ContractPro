// Package partyconsistency is Agent 3 — Party Data Consistency
// (LIC-TASK-027, ai-agents-pipeline.md §3, high-architecture.md §6.6/§6.7.2).
// It checks the consistency and completeness of the contracting parties'
// details: requisites, names, ИНН/ОГРН (formal validation — length, control
// digits, mutual consistency), addresses, signatory authority, and
// divergences across different parts of the document. The deterministic
// ИНН/ОГРН control-digit validation is performed PRE-LLM, LLM-free, by the
// Prompt Builder and handed to the agent as the <validation_facts> ground-truth
// block — Agent 3 is the sole v1 consumer of
// promptbuilder.Builder.ValidationFacts (base.Spec godoc,
// promptbuilder/CLAUDE.md).
//
// The agent is a THIN wrapper over the shared BaseAgent runner
// (internal/agents/base, LIC-TASK-024): Checker embeds *base.BaseAgent, so it
// satisfies port.Agent (ID + Run) for free. The only per-agent code is the
// Spec in spec.go — the envelope assembly (checkerSpec.Parts, which mints the
// <validation_facts> block) and the typed decode (checkerSpec.Decode).
// Everything invariant-heavy — Prompt Builder, Token Estimator, the primary
// LLM call, schema validation, the sticky 1-shot repair loop, the span tree
// and the four invocation metrics — lives in base, exactly once, shared by all
// 9 agents.
//
// Hermeticity. Like every internal/agents|llm/* sibling, this package imports
// ONLY stdlib + internal/domain/{model,port} + the sibling agent packages it
// composes (base, promptbuilder, prompts, schemas) + the shared DP-faithful
// EXTRACTED_TEXT decoder (internal/agents/artifacts — the ratified reuse rule
// for every EXTRACTED_TEXT consumer; artifacts/CLAUDE.md "Consumers"). Unlike
// Agents 1/2, this package imports promptbuilder for a STRUCTURAL purpose
// (b.ValidationFacts + promptbuilder.Party), not merely Content — that is the
// mandated §3 difference, anticipated by base.go's Spec godoc. It does NOT
// import internal/config (resolved per-agent values are constructor parameters;
// the config→value mapping is app-wiring's job, LIC-TASK-047 — the hermetic
// router.RouterConfig precedent) and it does NOT import the DocumentProcessing
// module (the local minimal documentStructure mirror in spec.go owns the
// DP-faithful DOCUMENT_STRUCTURE.party_details shape — the exact analogue of
// Agent 1's local documentStructure projection; artifacts deliberately
// centralises ONLY EXTRACTED_TEXT). TestHermeticImports pins the exact
// allowlist.
package partyconsistency

import (
	"fmt"
	"time"

	"contractpro/legal-intelligence-core/internal/agents/base"
	"contractpro/legal-intelligence-core/internal/agents/prompts"
	"contractpro/legal-intelligence-core/internal/agents/schemas"
	"contractpro/legal-intelligence-core/internal/domain/model"
	"contractpro/legal-intelligence-core/internal/domain/port"
)

// Agent-3 LLM budget, copied verbatim from the ai-agents-pipeline.md §3
// "Бюджеты и параметры LLM" table (the binding per-agent SSOT): Claude
// (sonnet) primary, deterministic temperature, 1 000 max output tokens,
// 6 s timeout. The timeout is supplied by app-wiring from
// config.AgentsConfig.Timeouts[AgentPartyConsistency]
// (LIC_AGENT_PARTY_CONSISTENCY_TIMEOUT, default 6s — configuration.md), not
// hard-coded here. The configured primary provider is
// LIC_AGENT_PARTY_CONSISTENCY_PROVIDER (default Claude per ADR-LIC-03),
// resolved by wiring into modelID — never a literal here.
const (
	maxOutputTokens = 1000 // §3 "Max output tokens"
	temperature     = 0.0  // §3 "Temperature" — deterministic analysis
)

// Checker is Agent 3. It embeds *base.BaseAgent; the embedded ID() and Run()
// make it a port.Agent the Stage Executor (LIC-TASK-034) wires by AgentID.
// Immutable after NewChecker (it adds no mutable state of its own; the
// BaseAgent is itself immutable and concurrency-safe).
type Checker struct {
	*base.BaseAgent
}

// Compile-time proof that Checker satisfies the uniform agent contract
// (codebase-wide `var _ Port = (*Impl)(nil)` house style; mirrors base.go /
// typeclassifier / keyparams).
var _ port.Agent = (*Checker)(nil)

// NewChecker assembles Agent 3. It loads the embedded system prompt and output
// schema (a missing/empty/invalid asset is a fatal startup error per
// prompts/schemas package contracts — propagated, never swallowed), builds the
// §3 Config, and delegates the wiring validation to base.NewBaseAgent
// (fail-fast: empty model id / non-positive timeout / nil router / a
// Stage≠canonicalStage[AgentPartyConsistency] mismatch are rejected at
// construction, not on the first contract).
//
// modelID is the resolved wire model of Agent 3's CONFIGURED PRIMARY provider
// (ADR-LIC-03 default = claude ⇒ LIC_CLAUDE_MODEL, default claude-sonnet-4-6),
// supplied by app-wiring (LIC-TASK-047) — never a literal here.
//
// KNOWN LIMITATION (forward note, owner: LIC-TASK-024 / router / LIC-TASK-047
// — identical to typeclassifier forward-note #1 / keyparams). base.Run
// unconditionally sets req.Model = cfg.Model and the router forwards the
// request UNCHANGED to every provider in the fallback chain; each provider's
// chooseModel lets a non-empty req.Model OVERRIDE that provider's env-pinned
// default (LIC_OPENAI_MODEL / LIC_GEMINI_MODEL). So on a fallback to a
// non-primary provider the primary's model id is sent verbatim (invalid for
// that vendor → that fallback hop fails), which contradicts ADR-LIC-03 /
// llm-provider-abstraction.md §1.3. Passing the primary provider's resolved
// default here is the least-bad choice that is correct on the primary path and
// does not modify base; the proper fix (base leaving req.Model=="" so each
// adapter's chooseModel falls through to its own env-pinned default, or the
// router rewriting req.Model per provider) is a base/router change out of
// scope for this task.
//
// Constructor name is the stutter-free NewTypeName (feedback_constructors.md /
// the codebase-wide convention — NewClassifier / NewExtractor / NewBuilder /
// NewValidator; never bare New). deps is base.Deps (the established injection
// seam shared by base and router) rather than a parallel functional-Option
// API: it already bundles the required Router plus the optional
// Metrics/Tracer/Estimator/RepairMetrics seams whose concrete adapters
// LIC-TASK-047 supplies; every nil field degrades to its zero-dependency
// default inside base.
func NewChecker(modelID string, timeout time.Duration, deps base.Deps) (*Checker, error) {
	system, err := prompts.LoadPrompt(model.AgentPartyConsistency)
	if err != nil {
		return nil, fmt.Errorf("partyconsistency: load system prompt: %w", err)
	}
	schema, err := schemas.LoadSchema(model.AgentPartyConsistency)
	if err != nil {
		return nil, fmt.Errorf("partyconsistency: load output schema: %w", err)
	}

	cfg := base.Config{
		AgentID:     model.AgentPartyConsistency,
		Stage:       model.StageAgentPartyConsistency,
		System:      system,
		Schema:      schema,
		Model:       modelID,
		MaxTokens:   maxOutputTokens,
		Temperature: temperature,
		Timeout:     timeout,
	}

	ba, err := base.NewBaseAgent(cfg, checkerSpec{}, deps)
	if err != nil {
		return nil, fmt.Errorf("partyconsistency: %w", err)
	}
	return &Checker{BaseAgent: ba}, nil
}
