# schemavalidator Package — CLAUDE.md

JSON **Schema Validator + 1-shot Repair Loop** for the LIC agent pipeline
(LIC-TASK-023, `high-architecture.md` §6.6/§6.8, `error-handling.md` §5).
Consumed by the BaseAgent runner (LIC-TASK-024): step 4 (validate) + step
5–6 (repair / escalate).

## Files

- **validator.go** — package doc + `Validator` / `NewValidator` /
  `Validate(schema, content) []byte→error` (the only `gojsonschema`
  call-site).
- **errors.go** — `SchemaViolation` (repair trigger; `Pretty()` for the
  prompt; sorted + de-duped + deterministic) and `SchemaCompileError`
  (LIC build defect; never repaired) + `AsSchemaViolation` /
  `AsSchemaCompileError`.
- **repair.go** — `Repairer` seam, `RepairLoop` / `NewRepairLoop` /
  `Run`, `buildRepairRequest`, `repairPromptTemplate` (§5.2 byte-exact).
- **seams.go** — `Metrics` seam + `noopMetrics`; `RepairOutcome` local
  mirror of `metrics.AgentRepairOutcome`.
- **CLAUDE.md** — this file.

## API

- `NewValidator() *Validator`; `(*Validator) Validate(schema, content
  []byte) error` → `nil` | `*SchemaCompileError` | `*SchemaViolation`.
- `NewRepairLoop(repairer Repairer, metrics Metrics) (*RepairLoop,
  error)` — fail-fast on nil `repairer`; nil `metrics` ⇒ no-op.
- `(*RepairLoop) Run(ctx, agentID, stage, schema, originalReq, primary)
  (port.CompletionResponse, error)`.

## Conventions & deliberate decisions

- **THE ONE non-hermetic `internal/*` package.** Every other
  `internal/llm/*` / `internal/agents/*` imports only stdlib +
  `internal/domain`. Real draft-07 validation needs a real library and
  `schemas/CLAUDE.md` explicitly defers "the real JSON-Schema library
  (kaptinlin/jsonschema or xeipuuv/gojsonschema)" to **this** task.
  `github.com/xeipuuv/gojsonschema v1.2.0` is confined to `validator.go`
  and re-exposed only via `Validate(...) error`, so no other package
  gains the dependency. `validator_internal_test.go`
  (`TestSingleThirdPartyImport`) pins this single-exception confinement
  (code-architect Q4). Telemetry stays inverted behind the `Metrics`
  seam — no `internal/infra/observability/metrics` import here
  (LIC-TASK-047 wires the adapter), exactly like `cost`/`router`.
- **PriorTurns SSOT reconciliation (code-architect MF-1).**
  `high-architecture.md` §6.8 and the (now-corrected) `port.Turn` /
  `router/repair.go` godocs gave a lossy 2-element shorthand
  `[{Assistant,invalid},{User,repair}]`. The shipped adapters
  (`claude`/`openai`/`gemini` `payload.go`) ALL build the wire
  conversation as `[System?] + PriorTurns... + final {user, req.User}`.
  The only construction that is provider-portable, alternating,
  user-first AND honours `error-handling.md` §5.2 ("User message
  предыдущего вызова — сохраняется") is:
  `PriorTurns = origPriorTurns + [{User,origUser},{Assistant,invalid}]`,
  `User = repair_prompt`. §5.2 prose wins on substance (the model needs
  the original contract data to repair against). The two stale godocs
  were corrected in this task (`port/llm.go`, `router/repair.go`); this
  file is the authoritative record of the reconciliation. Same class of
  SSOT-resolution as LIC-TASK-019 (§6.1-vs-catalog) and LIC-TASK-022
  (validation_facts form).
- **Repair prompt is `error-handling.md` §5.2 BYTE-EXACT.** Not the
  `tasks.json` paraphrase, not the §6.8 one-liner. The hard line break
  inside "без объяснений и\npreamble" is from §5.2 and preserved
  verbatim — deviating from a frozen prompt SSOT is the larger risk
  (code-architect item 7). `TestRepairPrompt_VerbatimSSOT` pins it.
- **`SchemaCompileError` ⇒ `INTERNAL_ERROR`, never repaired
  (code-architect MF-3 / Q2).** A broken embedded schema is a LIC build
  defect that `schemas.Validate()` should have caught fatally at
  startup; reaching `Run` is defence-in-depth. Escalated as
  `model.NewDomainError(ErrCodeInternal, stage).WithRetryable(true).
  WithCause(...)` — retryable (a redeploy with a fixed schema makes a
  retry succeed) and it must NOT trigger a repair turn (re-prompting the
  model cannot fix our schema). A raw error would deny the Orchestrator
  a code/stage/RU-message; never return raw.
- **Provider-error vs. second-violation are STRICTLY disjoint
  (code-architect MF-2).** `CompleteRepair` returns `(resp, err)`.
  `err != nil` ⇒ `repair_provider_error` → `AGENT_OUTPUT_INVALID`
  (is_retryable=true) wrapping the provider error, NO fallback (the
  router already enforces sticky/no-fallback; §6.8). `err == nil` +
  repaired content still a `SchemaViolation` ⇒ `repair_failed` →
  `AGENT_OUTPUT_INVALID` (is_retryable=true). The two branches cannot
  overlap — a validation error object is never classified as a provider
  error.
- **Happy path issues NO repair turn ⇒ ZERO repair metrics
  (code-architect MF-4).** `lic_agent_repair_attempts_total` is
  incremented EXACTLY once, immediately before `CompleteRepair`
  (`agent.go` SSOT: "incremented each time we issue a repair turn").
  A primary response that already validates touches neither
  `repair_attempts` nor `repair_outcome`.
  `TestRun_PrimaryValid_NoRepairMetrics` pins this.
- **Hard limit N=1 (ADR-LIC-04 / §5.4).** Exactly one `CompleteRepair`;
  no loop, no second repair on a second failure.
- **Repair request delta is EXACTLY {PriorTurns, User, Temperature}
  (§5.3).** Same `Model`, `MaxTokens`, `StopSequences`, `JSONMode`,
  `JSONSchema`, `System`; `Temperature` forced to `0.0` for determinism.
  `TestBuildRepairRequest_DeltaOnly` pins this.
- **Deterministic violation list.** `gojsonschema` reports errors in
  map-iteration order; `newSchemaViolation` sorts + de-dupes so the
  `{validation_errors_pretty_printed}` slot (and the whole repair
  prompt) is byte-stable run-to-run.
- **Narrow `Repairer` seam.** 1 method = the exact
  `port.ProviderRouterPort.CompleteRepair` signature; the
  `var _ Repairer = (port.ProviderRouterPort)(nil)` assertion lives in
  app-wiring (LIC-TASK-047), NOT here — keeps this package free of an
  `internal/llm/router` import (mirrors `cost.Recorder`).
- **`RepairOutcome` local mirror.** Declared here, not imported, to keep
  telemetry hermetic; `seams_test.go` (`TestRepairOutcome_WireStringsPinned`)
  pins the three strings against the shipped `metrics/labels.go`
  `AgentRepairOutcome` SSOT (`observability.md` §3.3).
- **Immutable / concurrency-safe.** `Validator` is stateless;
  `RepairLoop` is fixed after construction → safe for the parallel
  errgroup pipeline (`-race`).

## Forward requirements for LIC-TASK-047 (app-wiring)

1. Implement the `Metrics` adapter over `*metrics.AgentMetrics`:
   `RepairAttempt` → `RepairAttemptsTotal.WithLabelValues(agent,
   provider).Inc()`; `RepairOutcome` →
   `RepairOutcomeTotal.WithLabelValues(agent, provider, outcome).Inc()`.
   `outcome` MUST be one of the exported `RepairOutcome` constants
   (`.String()`), never a raw conversion.
2. Assert the router satisfies the seam in wiring:
   `var _ Repairer = (port.ProviderRouterPort)(nil)` (or the concrete
   `*router.ProviderRouter`).
3. `schemas.Validate()` MUST be a fatal startup check (already a
   `schemas/CLAUDE.md` forward requirement) so `SchemaCompileError`
   never reaches `Run` in production — the in-`Run` handling is
   defence-in-depth only.

## Forward requirement for LIC-TASK-024 (BaseAgent)

`Run` is steps 4–6 of the agent loop. The caller owns: the agent's
`stage` (`STAGE_AGENT_*`), the `schema` (`schemas.LoadSchema(agentID)`),
the `originalReq` (built by the Prompt Builder, LIC-TASK-022) and the
`primary` `PrimaryCallResult` (from `router.Complete`). On a `nil` error
`Run` returns the response to validate-and-aggregate; on a `*DomainError`
the BaseAgent propagates it (errgroup) — `AGENT_OUTPUT_INVALID` and
`INTERNAL_ERROR` are both `is_retryable=true`.
