# promptinjection Package — CLAUDE.md

**End-to-end verification of the LIC 5-layer prompt-injection defense**
(LIC-TASK-053, ADR-LIC-07, `security.md` §4 / §6.4,
`high-architecture.md` §6.7.1 / §6.11 step 6, `ai-agents-pipeline.md`
§0.2 / §0.3 / §8 warnings schema). **Test-only leaf package** —
production code is unchanged. Per-layer unit tests live alongside each
owning package; this harness wires the layers together and asserts the
contract HOLDS ACROSS THE SEAMS.

## Files

- **promptinjection_test.go** — the single `_test.go` file; package is
  `promptinjection_test` (external test package). Holds all 7 layer
  checks plus the audit-hash primitive contract and the small local
  helpers (`loadSchemaAsMap`, `mustMap`, `equalStrings`, `min`/`max`).
- **CLAUDE.md** — this file.

## Layers covered (security.md §4.1)

- **L1** — every one of the 9 agent system prompts ships the
  "ЗАЩИТА ОТ ИНСТРУКЦИЙ" section (case-insensitive match — a stylistic
  re-cap survives; a removal fails).
- **L2** — `promptbuilder.Build` escapes user-controlled content
  verbatim (`&lt;/contract_document&gt;` ×N) while the Builder-emitted
  structural envelope tags (`<input>`, `<contract_document>`) stay
  un-escaped — pinned by `strings.Count` equalling 1 for every
  structural form and `N` for every escaped form.
- **L3** — `schemas.LoadSchema(id)` returns non-empty, well-formed JSON
  for all 9 agents and the bytes are assignable to
  `port.CompletionRequest.JSONSchema` (`json.RawMessage`) — enabling
  strict structured outputs at the LLM adapter
  (`llm-provider-abstraction.md` §1.1).
- **L4** — schema-level prompt-injection contract:
  - top-level `prompt_injection_detected: boolean` PRESENT in the 5
    raw-text-consuming agents (1..5);
  - ABSENT from the 4 downstream-only agents (6/7/8/9 — Aggregator D3
    model-SSOT pin; defense-in-depth against a future "helpful" widen);
  - Agent 8 `warnings.PROMPT_INJECTION_DETECTED.required` =
    `{detected, detected_by_agents, detection_count, user_message}` —
    the wire contract the Aggregator fills in L5.
- **L5** — `aggregator.Aggregate` folds raw flags into one merged
  warning: `DetectionCount`, lexicographically-sorted
  `DetectedByAgents`, RU `UserMessage`; metric incremented exactly once
  per detecting agent (`lic_prompt_injection_detected_total{agent}`,
  observability.md §3.9). Verified via an in-test `recordingMetrics`
  fake — NO Prometheus import. Also pins the D6 stripping invariant
  (raw flags do not echo onto `MergedRiskAnalysis` /
  `StrippedKeyParameters`).
- **Audit hash** (`security.md` §6.4) — `logger.HashContent` is
  HMAC-SHA-256-hex(64) and returns `""` on an empty/nil key
  (misconfig signal); deterministic; content-sensitive.

## API

None — this package exports nothing. It only contains `*_test.go` code.

## Conventions & deliberate decisions

- **External test package (`promptinjection_test`).** Imports only
  through the public APIs of the leaf packages it integrates — no
  privileged access to internal-test types. This is what makes the
  harness a faithful integration view.
- **Hermetic.** stdlib + `internal/agents/{prompts,schemas,promptbuilder}`,
  `internal/application/aggregator`, `internal/domain/{model,port}`,
  `internal/infra/observability/logger`. NO Prometheus, NO RabbitMQ, NO
  Redis. The aggregator's `Metrics` seam is satisfied by an in-file
  `recordingMetrics` (mirrors `aggregator.spyMetrics` —
  `feedback_constructors.md` test-fake pattern, kept LOCAL).
- **Schema inspection via `map[string]any`.** Decoding the verbatim
  bytes through `encoding/json` keeps the harness leaf-pure and free of
  a JSON-Schema library (that dependency is owned by LIC-TASK-023).
  Sufficient for the property/required-set assertions this layer needs.
- **No emojis, no exported helpers, gofmt-clean.** Single file by
  preference; the helpers stay below the tests so the file reads
  top-down.
- **Counts, not first-occurrence.** L2 uses `strings.Count` so a
  hostile payload that double-plants a delimiter cannot pass a
  "contains an escape" check while still leaking a second un-escaped
  copy.

## Forward requirement for LIC-TASK-048..051

Once the per-agent prompt-injection BaseAgent + detection plumbing
lands, L4's flag-presence contract will be EXERCISED end-to-end by
real agent `Run()` calls against canned hostile fixtures. Today only
the schemas + Aggregator are wired, because the agent Runs themselves
are not yet integrated; this harness pins the wire contracts the
runtime path will fill.
