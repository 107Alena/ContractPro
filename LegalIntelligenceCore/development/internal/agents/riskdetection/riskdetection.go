// Package riskdetection is Agent 5 — Risk Detection & Severity Scoring
// (LIC-TASK-029, ai-agents-pipeline.md §5, high-architecture.md §6.6/§6.7.2).
// It is the CENTRAL agent: it scans the contract for risky constructions and
// assigns each a severity (high|medium|low), forming the primary
// RISK_ANALYSIS.risks[] array. The Result Aggregator (LIC-TASK-035) later
// folds Agent 3 (party-consistency) findings as R-PNNN and Agent 4
// (mandatory-conditions) findings as R-MNNN INTO this same risks[] slice
// (high-architecture.md §6.11.1); Agent 5 itself emits only the bare R-NNN
// namespace.
//
// The agent is a THIN wrapper over the shared BaseAgent runner
// (internal/agents/base, LIC-TASK-024): Detector embeds *base.BaseAgent, so
// it satisfies port.Agent (ID + Run) for free. The only per-agent code is the
// Spec in spec.go — the envelope assembly (detectorSpec.Parts) and the typed
// decode (detectorSpec.Decode). Everything invariant-heavy — Prompt Builder,
// Token Estimator, the primary LLM call, schema validation, the sticky 1-shot
// repair loop, the span tree and the four invocation metrics — lives in base,
// exactly once, shared by all 9 agents.
//
// Hermeticity. Like every internal/agents|llm/* sibling, this package imports
// ONLY stdlib + internal/domain/{model,port} + the sibling agent packages it
// composes (base, promptbuilder, prompts, schemas) + the shared DP-faithful
// EXTRACTED_TEXT decoder (internal/agents/artifacts — the ratified reuse rule
// for every EXTRACTED_TEXT consumer; artifacts/CLAUDE.md "Consumers"). Like
// Agents 1/2/4 (and UNLIKE Agent 3) it imports promptbuilder ONLY for
// Content: Agent 5 mints NO structural block — <validation_facts> is solely
// Agent 3's role. It does NOT import internal/config (resolved per-agent
// values are constructor parameters; the config→value mapping is app-wiring's
// job, LIC-TASK-047 — the hermetic router.RouterConfig precedent) and it does
// NOT import the DocumentProcessing module: BOTH SEMANTIC_TREE and the
// optional PROCESSING_WARNINGS artifact are byte-faithful passthroughs (Agent
// 5 never structurally decodes them — code-architect CC-4), and the artifacts
// package owns the DP-faithful EXTRACTED_TEXT decoder. The deliberate ABSENCE
// of a local processingWarnings mirror struct (cf. Agent 3's local
// documentStructure) is the SEMANTIC_TREE-passthrough class, not the
// DOCUMENT_STRUCTURE-local-decode class — not an omission. TestHermeticImports
// pins the exact allowlist (byte-identical to mandatoryconditions').
package riskdetection

import (
	"fmt"
	"time"

	"contractpro/legal-intelligence-core/internal/agents/base"
	"contractpro/legal-intelligence-core/internal/agents/prompts"
	"contractpro/legal-intelligence-core/internal/agents/schemas"
	"contractpro/legal-intelligence-core/internal/domain/model"
	"contractpro/legal-intelligence-core/internal/domain/port"
)

// Agent-5 LLM budget, copied verbatim from the ai-agents-pipeline.md §5
// "Бюджеты и параметры LLM" table (the binding per-agent SSOT): Claude
// (sonnet) primary, deterministic temperature, 3 500 max output tokens, 12 s
// timeout. The timeout is supplied by app-wiring from
// config.AgentsConfig.Timeouts[AgentRiskDetection]
// (LIC_AGENT_RISK_DETECTION_TIMEOUT, default 12s — configuration.md;
// config.AgentRiskDetection), not hard-coded here. The configured primary
// provider is LIC_AGENT_RISK_DETECTION_PROVIDER (default Claude per
// ADR-LIC-03), resolved by wiring into modelID — never a literal here.
const (
	maxOutputTokens = 3500 // §5 "Max output tokens"
	temperature     = 0.0  // §5 "Temperature" — deterministic analysis
)

// Detector is Agent 5. It embeds *base.BaseAgent; the embedded ID() and Run()
// make it a port.Agent the Stage Executor (LIC-TASK-034) wires by AgentID.
// Immutable after NewDetector (it adds no mutable state of its own; the
// BaseAgent is itself immutable and concurrency-safe).
type Detector struct {
	*base.BaseAgent
}

// Compile-time proof that Detector satisfies the uniform agent contract
// (codebase-wide `var _ Port = (*Impl)(nil)` house style; mirrors base.go /
// typeclassifier / keyparams / partyconsistency / mandatoryconditions).
var _ port.Agent = (*Detector)(nil)

// NewDetector assembles Agent 5. It loads the embedded system prompt and
// output schema (a missing/empty/invalid asset is a fatal startup error per
// prompts/schemas package contracts — propagated, never swallowed), builds
// the §5 Config, and delegates the wiring validation to base.NewBaseAgent
// (fail-fast: empty model id / non-positive timeout / nil router / a
// Stage≠canonicalStage[AgentRiskDetection] mismatch are rejected at
// construction, not on the first contract).
//
// modelID is the resolved wire model of Agent 5's CONFIGURED PRIMARY provider
// (ADR-LIC-03 default = claude ⇒ LIC_CLAUDE_MODEL, default claude-sonnet-4-6),
// supplied by app-wiring (LIC-TASK-047) — never a literal here.
//
// KNOWN LIMITATION (forward note 1, owner: LIC-TASK-024 / router /
// LIC-TASK-047 — identical to typeclassifier #1 / keyparams /
// partyconsistency / mandatoryconditions). base.Run unconditionally sets
// req.Model = cfg.Model and the router forwards the request UNCHANGED to every
// provider in the fallback chain; each provider's chooseModel lets a non-empty
// req.Model OVERRIDE that provider's env-pinned default. So on a fallback to a
// non-primary provider the primary's model id is sent verbatim (invalid for
// that vendor → that fallback hop fails), which contradicts ADR-LIC-03 /
// llm-provider-abstraction.md §1.3. Passing the primary provider's resolved
// default here is the least-bad choice that is correct on the primary path
// and does not modify base; the proper fix is a base/router change out of
// scope for this task.
//
// Constructor name is the stutter-free NewTypeName (feedback_constructors.md /
// the codebase-wide convention — NewClassifier / NewExtractor / NewChecker /
// NewDetector; never bare New). deps is base.Deps (the established injection
// seam shared by base and router) rather than a parallel functional-Option
// API: it already bundles the required Router plus the optional
// Metrics/Tracer/Estimator/RepairMetrics seams whose concrete adapters
// LIC-TASK-047 supplies; every nil field degrades to its zero-dependency
// default inside base.
func NewDetector(modelID string, timeout time.Duration, deps base.Deps) (*Detector, error) {
	system, err := prompts.LoadPrompt(model.AgentRiskDetection)
	if err != nil {
		return nil, fmt.Errorf("riskdetection: load system prompt: %w", err)
	}
	schema, err := schemas.LoadSchema(model.AgentRiskDetection)
	if err != nil {
		return nil, fmt.Errorf("riskdetection: load output schema: %w", err)
	}

	cfg := base.Config{
		AgentID:     model.AgentRiskDetection,
		Stage:       model.StageAgentRiskDetection,
		System:      system,
		Schema:      schema,
		Model:       modelID,
		MaxTokens:   maxOutputTokens,
		Temperature: temperature,
		Timeout:     timeout,
	}

	ba, err := base.NewBaseAgent(cfg, detectorSpec{}, deps)
	if err != nil {
		return nil, fmt.Errorf("riskdetection: %w", err)
	}
	return &Detector{BaseAgent: ba}, nil
}
