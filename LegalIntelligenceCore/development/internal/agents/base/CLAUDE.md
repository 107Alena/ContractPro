# base Package — CLAUDE.md

**BaseAgent runner** — the uniform `Run()` workflow shared by all 9 LIC AI
agents (LIC-TASK-024, `high-architecture.md` §6.6/§6.8,
`ai-agents-pipeline.md`, `observability.md` §3.3/§4.2/§4.3,
`error-handling.md` §5). Implements `port.Agent`. The 9 per-agent packages
(LIC-TASK-025..033) embed `*BaseAgent` and supply only a `Spec` + a `Config`.

Constructor: `NewBaseAgent(cfg, spec, deps) (*BaseAgent, error)` —
stutter-free `NewTypeName` per the codebase-wide convention /
`feedback_constructors.md`; fail-fast like `NewProviderRouter` /
`NewRepairLoop` / `NewBuilder`.

## Files

- **base.go** — package doc, `Config`, `Spec`, `Deps`, `canonicalStage`
  table, `BaseAgent`, `NewBaseAgent`, `ID`, `Run` (the §6.6 loop),
  `correlationFrom`, `classifyCompleteError`, `classifyRepairError`.
- **seams.go** — `Outcome` (local mirror of
  `metrics.AgentInvocationOutcome`), the `Metrics` / `TokenEstimator` /
  `Tracer`+`AgentSpan`+`LLMSpan` seams + zero-dependency noop/passthrough
  defaults, the span in/out plain containers.
- **CLAUDE.md** — this file.

## API

- `NewBaseAgent(Config, Spec, Deps) (*BaseAgent, error)`.
- `(*BaseAgent) ID() model.AgentID`; `(*BaseAgent) Run(ctx, model.AgentInput)
  (port.AgentResult, error)` — satisfies `port.Agent`.
- `Spec{ Parts(*promptbuilder.Builder, model.AgentInput) ([]promptbuilder.Part,
  error); Decode([]byte) (port.AgentResult, error) }` — the only per-agent
  variation; implementations MUST be stateless / concurrent-safe.

## The Run() loop (§6.6, steps map 1:1)

1. start parent `lic.agent.<name>` span (Tracer seam) + start clock.
2. `spec.Parts` → `builder.Build(AgentID, System, parts)` (step 1).
3. set `Model/MaxTokens/Temperature/JSONSchema/JSONMode` on the request
   (promptbuilder sets only `AgentID/System/User` per its contract).
4. `estimator.Fit(req)` (step 2) — estimate only, **never mutates** `req`.
5. child `lic.llm.call` span + `context.WithTimeout(ctx, cfg.Timeout)`.
6. `router.Complete` (step 3).
7. `validator.Validate(primary)`: `nil` ⇒ **success** (no RepairLoop call);
   else ⇒ `repair.Run(...)` (steps 4–6, the schemavalidator SSOT).
8. `spec.Decode(finalResp)` → typed `port.AgentResult`.
9. single deferred site: 4 metrics + finish child-then-parent span.

## Conventions & deliberate decisions

- **Hermetic (the universal `internal/agents|llm/*` invariant).** Imports
  ONLY stdlib + `internal/domain/{model,port}` + the sibling leaf agents
  packages it composes (`promptbuilder`, `schemavalidator`). Telemetry —
  the four agent metrics, the OTel span tree, and the (LIC-TASK-021) token
  estimator — is inverted behind the `Metrics`/`Tracer`/`TokenEstimator`
  seams with zero-dependency defaults; the concrete adapters over
  `*metrics.AgentMetrics` / `*tracer.Tracer` / the real estimator are wired
  in **LIC-TASK-047** (mirrors `router`/`cost`/`promptbuilder`/
  `schemavalidator`). `TestHermeticImports` pins this against an exact
  allowlist. `schemavalidator` transitively pulls `gojsonschema` but
  re-exposes it only via `Validate(...) error`, so `base` gains **no**
  third-party surface (`schemavalidator/CLAUDE.md` "single-exception
  confinement"; `TestSingleThirdPartyImport` there pins schemavalidator's
  own imports, not consumers — code-architect Q1).
- **RepairLoop is reused, NOT re-implemented (code-architect Q2).**
  `schemavalidator.RepairLoop.Run` owns the §5.2 byte-exact repair prompt,
  the unexported `buildRepairRequest`, and the pinned MF-1..4 invariants.
  `base` constructs the RepairLoop with the **same router instance** (so
  the sticky `used_provider` OQ-10 holds) and calls it for the unhappy
  path only.
- **MF-1 — success vs repair_success is a load-bearing INVARIANT, not a
  guess.** `base`'s pre-check (`validator.Validate`) and `RepairLoop`'s
  internal first validate use the **same `*Validator` semantics and the
  same `cfg.Schema` bytes**, so the happy/unhappy fork is deterministic
  and `RepairLoop.Run`'s `(resp,nil)` happy arm is **statically
  unreachable** from `base` (we only call it after `verr != nil`).
  Therefore a `nil` repair error PROVES exactly one repair turn occurred ⇒
  `repair_success`. The one redundant primary validate on the rare unhappy
  path is the price of reusing the schemavalidator SSOT instead of
  duplicating the §5.2 prompt — and is what makes the outcome attribution
  provable rather than heuristic.
- **C1 — `repair_attempts` is derived from GROUND TRUTH, never a pre-call
  guess.** It is set from the actual `repair.Run` outcome
  (`classifyRepairError`'s `turnIssued` / a `(resp,nil)` return), not from
  `base` predicting what `RepairLoop` will do. A turn was issued ⇒
  `repaired_ok | repair_failed | repair_provider_error` ⇒ `=1`; an
  `INTERNAL_ERROR` (broken schema / Validate shape-drift, RepairLoop
  returns before any `CompleteRepair`) ⇒ `=0`. This guarantees the
  `lic.agent.repair_attempts` span attribute can NEVER disagree with
  schemavalidator's `lic_agent_repair_attempts_total`, even on the
  defence-in-depth shape-drift branch. Pinned by
  `TestRun_Repair{Success,Failed_InvalidOutput,ProviderError}`.
- **MF-2 — `invalid_output` is a documented LOSSY PROJECTION; the truth
  is never lost.** `observability.md` §3.3 is the closed SSOT
  (`success|repair_success|invalid_output|provider_error|timeout`, 9×5=45
  series). LIC build defects (broken embedded schema → `INTERNAL_ERROR`, a
  `spec.Decode` schema/struct mismatch, a `promptbuilder.Build`/
  `spec.Parts` fail-fast, an empty-chain `MALFORMED_REQUEST`) have **no**
  dedicated outcome value; they project onto `invalid_output` as the
  closest closed-enum value. The un-lossy truth is preserved on the OTel
  agent span (`Finish(out, err)` records `status=Error` carrying the real
  `*model.DomainError` with `Code=INTERNAL_ERROR`, Stage, Cause) and in
  the returned error to the Orchestrator. Pinned by
  `TestRun_BuildDefect_*`. Same class of named, deliberate lossy telemetry
  property as the router's documented `cost` undercount.
- **MF-2 — the two `AGENT_OUTPUT_INVALID` sub-cases are discriminated
  EXPLICITLY, never by `else`.** `RepairLoop` returns
  `AGENT_OUTPUT_INVALID` for **both** `repair_failed`
  (`Cause=*SchemaViolation`) and `repair_provider_error`
  (`Cause=*LLMProviderError`). `classifyRepairError` probes
  `schemavalidator.AsSchemaViolation` first (→ `invalid_output`) then
  `port.AsLLMProviderError` (→ `provider_error`); the two are strictly
  disjoint (schemavalidator MF-2). `INTERNAL_ERROR`/anything-else →
  `invalid_output` (MF-2 projection).
- **MF-3 — `TokenEstimator.Fit` is shaped so it CANNOT corrupt the
  envelope.** It returns `(estInputTokens, overBudget)` only — **no
  request to mutate**. `promptbuilder.Build` produces one escaped
  `<input>…</input>` blob; truncating it post-Build would slice an escaped
  XML entity and defeat prompt-injection defence layer 2. The §6.7 /
  ASSUMPTION-LIC-12 head-60/tail-40 truncation of `EXTRACTED_TEXT` is
  **per-artifact, upstream of `spec.Parts`/Build** (LIC-TASK-021's job on
  `model.AgentInput`). v1 default `passthroughEstimator` = ⌈runes/4⌉,
  `overBudget=false` → the size verdict is delegated to the provider's
  `CONTEXT_TOO_LONG`, which `classifyCompleteError` maps to
  `AGENT_INPUT_TOO_LARGE` (non-retryable). When the real LIC-TASK-021
  estimator is wired and a request is unsalvageable it returns
  `overBudget=true` and `Run` fails fast with the **identical**
  `AGENT_INPUT_TOO_LARGE` code without burning an LLM call. There is NO
  §6.6 deviation: §6.6 lists Prompt Builder as step 1 and Token Estimator
  as step 2, i.e. estimate **after** Build — exactly this order
  (code-architect MF-3 corrected the earlier mis-statement).
- **MF-4 — happy path issues NO repair turn ⇒ ZERO repair metrics.** A
  valid primary returns without calling `RepairLoop`, so neither
  `lic_agent_repair_attempts_total` nor `lic_agent_repair_outcome_total`
  is touched. `TestRun_Success` pins this.
- **MF-5 — `lic_agent_invocations_total` fires EXACTLY once.** `outcome`
  is initialised to the build-defect sentinel (`invalid_output`) so even a
  pre-call `spec.Parts`/`Build` failure carries a valid closed-enum label;
  every fork overwrites it; emission happens at exactly ONE deferred site
  (no inline `Invocation`). `Duration` is always recorded; `InputTokens`
  only once `Fit` ran (`fitRan` guard — no `observe(0)` on a pre-Fit
  defect); `OutputTokens` only on `success|repair_success`. The single
  defer also finishes the child `lic.llm.call` span **before** the parent
  `lic.agent` span (§4.2 tree). `TestRun_BuildDefect_PartsError` pins the
  no-pre-Fit-input-token rule.
- **MF-6 — `builder.Build` / `spec.Parts` errors are NEVER swallowed.**
  Both are LIC programming defects `promptbuilder` deliberately surfaces
  (fail-fast, never panic). They become `INTERNAL_ERROR` DomainErrors with
  `outcome=invalid_output`, not a half-built request sent to the LLM.
- **MF-4 (span) — TWO spans, not one.** `observability.md` §4.2 mandates
  parent `lic.agent.<name>` ⊃ child `lic.llm.call`; §4.3 splits the
  attribute families (`tracer/attrs.go` encodes the split). Agent span:
  `lic.agent.id` + correlation IDs + `lic.agent.outcome` +
  `lic.agent.repair_attempts`. Child span: `lic.llm.provider`
  (`UsedProvider`) / `model` / `input_tokens` / `output_tokens` /
  `latency_ms`. `lic.llm.cost_usd`, `lic.llm.cached_tokens` and
  `lic.llm.fallback_used` are **forward-deferred**: the Router owns
  provider selection and does not expose the agent→primary map (so
  `fallback_used` is not derivable here), and cost-USD span attribution is
  a documented forward note for the span owner (`router/CLAUDE.md` "Out of
  scope"). They are set by a later task.
- **S2 deviation (deliberate, overrides the design-review S2).**
  code-architect S2 suggested `base.New`; this is **rejected** because
  `feedback_constructors.md` (user-recorded feedback) explicitly mandates
  `NewTypeName`, never bare `New`, and the entire codebase follows it
  (`NewProviderRouter`/`NewRepairLoop`/`NewBuilder`/`NewValidator`/
  `NewTracker`/`NewLimiter`). `NewBaseAgent` is the convention-correct
  name; the project feedback wins over the generic stutter heuristic.
- **S4 — timeout discrimination: `cctx.Err()==DeadlineExceeded` is the
  decisive FIRST check.** The Router wraps ctx errors (a ctx-cancelled
  rate-limiter `Wait` surfaces as `*LLMProviderError{RATE_LIMIT}` wrapping
  `ctx.Err()`), so an `errors.Is(cerr, DeadlineExceeded)` first would
  mis-tag a rate-limit as a timeout. Pinned by
  `TestClassifyCompleteError_DeadlineDecisiveOverWrappedRateLimit` AND
  end-to-end through a real `context.WithTimeout` by
  `TestRun_Timeout_WrappedProviderError` (router returns
  `ALL_PROVIDERS_FAILED` wrapping `ctx.Err()` after the deadline fires).
- **H2 — parent-cancel → `AGENT_TIMEOUT` is a documented deliberate lossy
  decision.** The §3.3 closed enum has no "cancelled" value. An untyped
  parent-context cancel (shutdown / job abort — rare, the real router
  wraps ctx errors as typed `*LLMProviderError` ⇒ `provider_error`)
  projects onto `AGENT_TIMEOUT`/`timeout`. The `retryable=true` side
  effect is INTENTIONAL and correct: per ASSUMPTION-LIC-19 LIC has no
  pipeline-level retry, so `is_retryable=true` only lets the Orchestrator
  create a fresh RE_CHECK version — the right product behaviour for an
  analysis interrupted by shutdown. Same class of named lossy projection
  as MF-2 (not an oversight; flagged & accepted in code-review H2).
- **N3 — `canonicalStage` cross-check.** `NewBaseAgent` rejects a
  `cfg.Stage` that is not the agent's canonical `STAGE_AGENT_*`, so a
  per-agent task that wires a mismatched pair fails at construction, not
  via a mislabeled `status-changed` in production. Explicit enumerated
  table (house style; cf. `prompts.basenames`).
- **N1 — every base-constructed `DomainError` carries
  `agent_id`.** Consistent with `schemavalidator.RepairLoop`'s
  `WithAttribute("agent_id", …)` and the `DomainError.Attributes`
  structured-error contract.
- **Immutable / concurrency-safe.** `*BaseAgent` is fixed after
  construction and all per-call state is stack-local, so one instance is
  shared by the parallel errgroup pipeline without locking — provided
  `Spec` impls are stateless (stated on the `Spec` godoc).
  `TestRun_ConcurrentRaceClean` (`-race`, 32 goroutines, shared
  `*BaseAgent` + shared mutex-guarded `*fakeMetrics`) pins the production
  immutability + stack-locality. The concurrency contract of the
  `Tracer`/`Metrics` seam ADAPTERS is the LIC-TASK-047 adapter's
  responsibility, not pinned here (base only invokes interface methods).
- **gofmt self-check.** The sandbox blocks `gofmt`/`go fmt`;
  `TestGofmtClean` asserts canonical formatting in-process via
  `go/format` (same approach as `schemavalidator`).

## Forward requirements for LIC-TASK-025..033 (the 9 per-agent packages)

1. Implement `Spec`: `Parts` builds the agent's exact envelope (block
   ORDER matches the agent's system prompt; user content via
   `promptbuilder.Content`; agent 3 inserts `b.ValidationFacts` between
   `<party_roles>` and `<party_details_block>`); `Decode` unmarshals into
   the agent's concrete `model.*` result. Keep the `Spec` STATELESS.
2. Build `Config`: `System = prompts.LoadPrompt(AgentID)`,
   `Schema = schemas.LoadSchema(AgentID)`, `Model/MaxTokens/Temperature`
   from `ai-agents-pipeline.md` "Бюджеты и параметры LLM",
   `Timeout = config.AgentsConfig.Timeouts[AgentID]`,
   `Stage = canonicalStage[AgentID]`.
3. Embed `*BaseAgent`; the package's exported agent type then satisfies
   `port.Agent` for the Stage Executor (LIC-TASK-034) for free.

## Forward requirements for LIC-TASK-047 (app-wiring)

1. `Metrics` adapter over `*metrics.AgentMetrics`: `Invocation` →
   `InvocationsTotal.WithLabelValues(agent, outcome).Inc()` (`outcome`
   from the exported `Outcome` constants only — pinned by
   `TestOutcome_WireStringsPinned` against `metrics.AgentInvocationOutcome`);
   `Duration`/`InputTokens`/`OutputTokens` → the matching
   `*HistogramVec.WithLabelValues(agent).Observe(…)`.
2. `Tracer` adapter over `*tracer.Tracer`: `StartAgent` →
   `StartSpanWithFields(ctx, "lic.agent."+basename(agentID),
   SpanFields{…}, tracer.AttrAgentID.String(agentID))`; `StartLLMCall` →
   a child span `"lic.llm.call"`; `LLMSpan.Finish` sets
   `AttrLLMProvider/Model/InputTokens/OutputTokens/LatencyMS`;
   `AgentSpan.Finish` sets `AttrAgentOutcome` + `AttrAgentRepairAttempts`,
   calls `tracer.RecordError(span, err)` (carries the un-lossy
   `INTERNAL_ERROR` truth — MF-2) then `span.End()`.
3. `RepairMetrics` (`schemavalidator.Metrics`) adapter as documented in
   `schemavalidator/CLAUDE.md` forward-req #1. Assert the router satisfies
   the seam in wiring.
4. The real `TokenEstimator` (LIC-TASK-021) is injected via `Deps.Estimator`;
   it performs per-artifact head/tail truncation **upstream** in the
   per-agent `Spec.Parts`/the artifact bundle, and `Fit` here only reports
   the estimate + over-budget verdict.
