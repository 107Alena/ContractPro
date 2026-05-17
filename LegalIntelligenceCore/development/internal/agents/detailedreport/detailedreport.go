// Package detailedreport is Agent 8 — Detailed Report (LIC-TASK-032,
// ai-agents-pipeline.md §8, high-architecture.md §6.6/§6.7.2). From the
// classification, the key parameters, the party-consistency findings, the
// mandatory-conditions report, the MERGED risk analysis, the recommendations
// and the semantic tree it produces ONE structured legal report
// (model.DetailedReport) for a fellow lawyer: seven fixed-order sections
// (OVERVIEW, KEY_PARAMETERS, PARTY_DATA, MANDATORY_CONDITIONS, RISKS,
// RECOMMENDATIONS_SUMMARY, WARNINGS), each with items carrying a clause
// locator (clause_ref), a legal basis and links to the underlying risk /
// recommendation. Unlike Agent 7 (Business Summary, plain language) the §8
// register is professional legal prose. Agent 8 is Stage 5, running in
// PARALLEL with Agent 7 (business summary), after the Result Aggregator
// (LIC-TASK-035) merge.
//
// The agent is a THIN wrapper over the shared BaseAgent runner
// (internal/agents/base, LIC-TASK-024): DetailedReporter embeds
// *base.BaseAgent, so it satisfies port.Agent (ID + Run) for free. The only
// per-agent code is the Spec in spec.go — the envelope assembly
// (detailedReporterSpec.Parts) and the typed decode
// (detailedReporterSpec.Decode). Everything invariant-heavy — Prompt Builder,
// Token Estimator, the primary LLM call, schema validation, the sticky 1-shot
// repair loop, the span tree and the four invocation metrics — lives in base,
// exactly once, shared by all 9 agents.
//
// Hermeticity (6-entry, artifacts-FREE — the Agent-6 non-EXTRACTED_TEXT
// consumer CLASS, code-architect D10). Like every internal/agents|llm/*
// sibling, this package imports ONLY stdlib + internal/domain/{model,port} +
// the sibling agent packages it composes (base, promptbuilder, prompts,
// schemas). Like Agents 1/2/4/5/6 (and UNLIKE Agent 3) it imports
// promptbuilder ONLY for Content: Agent 8 mints NO structural block —
// <validation_facts> is solely Agent 3's role. It does NOT import
// internal/config (resolved per-agent values are constructor parameters; the
// config→value mapping is app-wiring's job, LIC-TASK-047 — the hermetic
// router.RouterConfig precedent). CRUCIALLY it does NOT import
// internal/agents/artifacts: the §8 envelope (detailed_report.txt:33-43) has
// NO <contract_document> block and §8 "Зависимости"
// (ai-agents-pipeline.md:1280) lists only SEMANTIC_TREE from DM — Agent 8
// consumes NO EXTRACTED_TEXT, so the shared DP-faithful ExtractedText decoder
// is dead weight here. Dropping artifacts is the deliberate Agent-6
// "non-EXTRACTED_TEXT consumer" class, NOT a regression toward Agents 4/5/7: a
// future reviewer must NOT re-add it (the riskdetection "deliberate absence is
// a class, not an omission" house style). SEMANTIC_TREE is a byte-faithful
// passthrough so the DocumentProcessing module is not imported either.
// TestHermeticImports pins the exact (6-entry, artifacts-free) allowlist.
package detailedreport

import (
	"fmt"
	"time"

	"contractpro/legal-intelligence-core/internal/agents/base"
	"contractpro/legal-intelligence-core/internal/agents/prompts"
	"contractpro/legal-intelligence-core/internal/agents/schemas"
	"contractpro/legal-intelligence-core/internal/domain/model"
	"contractpro/legal-intelligence-core/internal/domain/port"
)

// Agent-8 LLM budget, copied verbatim from the ai-agents-pipeline.md §8
// "Бюджеты и параметры LLM" table (the binding per-agent SSOT): Claude
// (sonnet) primary, 5 000 max output tokens, 12 s timeout, temperature 0.0.
// The timeout is supplied by app-wiring from
// config.AgentsConfig.Timeouts[AgentDetailedReport]
// (LIC_AGENT_DETAILED_REPORT_TIMEOUT, default 12s — configuration.md;
// config.AgentDetailedReport), not hard-coded here. The configured primary
// provider is LIC_AGENT_DETAILED_REPORT_PROVIDER (default Claude per
// ADR-LIC-03), resolved by wiring into modelID — never a literal here.
const (
	maxOutputTokens = 5000 // §8 "Max output tokens"

	// temperature is §8's "Temperature | 0.0". Agent 8 RETURNS to a
	// deterministic 0.0 — the INVERSE of the immediately-preceding Agents 6
	// (0.2) and 7 (0.3), whose non-zero values served clause-wording /
	// summary-phrasing variety. A detailed legal report is a deterministic
	// aggregation-and-formatting pass, so §8 specifies 0.0. A future reviewer
	// must NOT "carry forward" 0.3 from Agent 7 to match the immediate
	// predecessor — 0.0 is the binding §8 SSOT table value (the mirror image
	// of the Agent-6/7 MF-D5.1 doc-lock: there the lock was "don't normalise
	// to 0.0"; here it is "don't carry forward 0.3"). base.NewBaseAgent
	// validates Temperature ∈ [0,1] (base.go), so 0.0 is accepted.
	temperature = 0.0
)

// DetailedReporter is Agent 8. It embeds *base.BaseAgent; the embedded ID()
// and Run() make it a port.Agent the Stage Executor (LIC-TASK-034) wires by
// AgentID. Immutable after NewDetailedReporter (it adds no mutable state of
// its own; the BaseAgent is itself immutable and concurrency-safe).
type DetailedReporter struct {
	*base.BaseAgent
}

// Compile-time proof that DetailedReporter satisfies the uniform agent
// contract (codebase-wide `var _ Port = (*Impl)(nil)` house style; mirrors
// base.go / typeclassifier / keyparams / partyconsistency /
// mandatoryconditions / riskdetection / recommendation / summary).
var _ port.Agent = (*DetailedReporter)(nil)

// NewDetailedReporter assembles Agent 8. It loads the embedded system prompt
// and output schema (a missing/empty/invalid asset is a fatal startup error
// per prompts/schemas package contracts — propagated, never swallowed),
// builds the §8 Config, and delegates the wiring validation to
// base.NewBaseAgent (fail-fast: empty model id / non-positive timeout / nil
// router / a Stage≠canonicalStage[AgentDetailedReport] mismatch are rejected
// at construction, not on the first contract).
//
// modelID is the resolved wire model of Agent 8's CONFIGURED PRIMARY provider
// (ADR-LIC-03 default = claude ⇒ LIC_CLAUDE_MODEL, default claude-sonnet-4-6),
// supplied by app-wiring (LIC-TASK-047) — never a literal here.
//
// KNOWN LIMITATION (forward note 1, owner: LIC-TASK-024 / router /
// LIC-TASK-047 — identical to typeclassifier #1 / keyparams /
// partyconsistency / mandatoryconditions / riskdetection / recommendation /
// summary). base.Run unconditionally sets req.Model = cfg.Model and the
// router forwards the request UNCHANGED to every provider in the fallback
// chain; each provider's chooseModel lets a non-empty req.Model OVERRIDE that
// provider's env-pinned default. So on a fallback to a non-primary provider
// the primary's model id is sent verbatim (invalid for that vendor → that
// fallback hop fails), which contradicts ADR-LIC-03 /
// llm-provider-abstraction.md §1.3. Passing the primary provider's resolved
// default here is the least-bad choice that is correct on the primary path
// and does not modify base; the proper fix is a base/router change out of
// scope for this task.
//
// Constructor name is the stutter-free NewTypeName (feedback_constructors.md /
// the codebase-wide convention — NewClassifier / NewExtractor / NewChecker /
// NewDetector / NewRecommender / NewSummarizer / NewDetailedReporter; never
// bare New, never the package-stuttering NewDetailedReport). deps is base.Deps
// (the established injection seam shared by base and router) rather than a
// parallel functional-Option API: it already bundles the required Router plus
// the optional Metrics/Tracer/Estimator/RepairMetrics seams whose concrete
// adapters LIC-TASK-047 supplies; every nil field degrades to its
// zero-dependency default inside base.
func NewDetailedReporter(modelID string, timeout time.Duration, deps base.Deps) (*DetailedReporter, error) {
	system, err := prompts.LoadPrompt(model.AgentDetailedReport)
	if err != nil {
		return nil, fmt.Errorf("detailedreport: load system prompt: %w", err)
	}
	schema, err := schemas.LoadSchema(model.AgentDetailedReport)
	if err != nil {
		return nil, fmt.Errorf("detailedreport: load output schema: %w", err)
	}

	cfg := base.Config{
		AgentID:     model.AgentDetailedReport,
		Stage:       model.StageAgentDetailedReport,
		System:      system,
		Schema:      schema,
		Model:       modelID,
		MaxTokens:   maxOutputTokens,
		Temperature: temperature,
		Timeout:     timeout,
	}

	ba, err := base.NewBaseAgent(cfg, detailedReporterSpec{}, deps)
	if err != nil {
		return nil, fmt.Errorf("detailedreport: %w", err)
	}
	return &DetailedReporter{BaseAgent: ba}, nil
}
