# tenantisolation Package — CLAUDE.md

**Integration tests for tenant isolation (LIC-TASK-054).** Pins
organization_id propagation through outgoing events, cross-event
matching on UserConfirmedType, LLM-call wire purity, OTel correlation-
attr key, Prometheus label hygiene. Tests run against the production
stack wired over the four in-memory fakes via `lictestapp.NewTestApp`
(same harness as `internal/integration/happypath` and
`internal/integration/pauseresume`).

## Files

- **tenantisolation_test.go** — six external-package tests, plus
  `mustJSON` + `waitForCompletedStatus` + `waitForClassificationUncertain`
  helpers copied verbatim from `pauseresume_test.go`.
- **json_helpers_test.go** — `jsonMarshal` / `jsonUnmarshal` thin
  wrappers around `encoding/json`. Mirrors the `happypath` and
  `pauseresume` helpers.
- **CLAUDE.md** — this file.

## Tests

| # | Test | What it pins |
|---|------|--------------|
| 1 | `TestTenantIsolation_OrgID_PropagationToOutgoing_INITIAL` | org-id flows into every outgoing event of the happy-path INITIAL pipeline (`requests.artifacts`, `status-changed`, `analysis-ready`). |
| 2 | `TestTenantIsolation_OrgID_PropagationToOutgoing_PauseResume` | org-id propagation across pause + resume (every `status-changed`, the `classification-uncertain` pause signal, the persisted `lic-pending-state` blob, post-resume `analysis-ready`). |
| 3 | `TestTenantIsolation_UserConfirmedType_OrgIDMismatch_DLQ` | forged-cmd org-id mismatch ⇒ DLQ`INVALID_ORG_ID_MISMATCH` + pending-state NOT consumed + ACK + no `analysis-ready` + `lic-user-confirmed` stays at SETNX-written `PROCESSING`. |
| 4 | `TestTenantIsolation_LLMRequest_NoOrgIDLeakIntoMessages` | no org-id substring in `FakeLLMCall.System`/`.User`; reflection-confirms `port.CompletionRequest` has no org-id field. |
| 5 | `TestTenantIsolation_OTelAttributeKey_OrganizationID_Pin` | `tracer.AttrOrganizationID == "organization_id"`; `SpanFields{OrganizationID:"ORG-TEST"}.AsKeyValues()` includes the kv. |
| 6 | `TestTenantIsolation_PrometheusMetrics_NoOrganizationIDLabel` | every Prometheus metric family on a fresh registry is org-id-label-free (case-/spelling-variants `organizationid`, `org_id`, `orgid`, `tenant_id`, `tenantid` also rejected). |

## Reconciliations

The brief lists three intentional deviations from the literal task
spec that are pinned in test comments — recording them here so the
next reviewer sees the reasoning.

**R1 — ArtifactsProvided has no OrganizationID field.**
`port.ArtifactsProvided` (events.go:81-91, frozen event-catalog §2.1)
deliberately omits an `OrganizationID` field. Therefore cross-event
matching for ArtifactsProvided cannot happen at JSON-payload level. The
only org-id cross-event check that EXISTS in code is
`pendingconfirmation.Manager.HandleUserConfirmedType` (manager.go:396)
comparing `cmd.OrganizationID` to `ptc.OrganizationID` loaded from the
persisted pending state. Test #3 covers that — NOT a fabricated
ArtifactsProvided org check.

**R2 — OTel key is literally "organization_id".**
The constant `tracer.AttrOrganizationID` in
`internal/infra/observability/tracer/attrs.go:22` is literally
`"organization_id"` (correlation-level wire key), NOT
`"lic.pipeline.organization_id"` (that `lic.pipeline.*` namespace is
reserved for pipeline-mode/outcome attributes; see the §"Pipeline-
level keys" block at attrs.go:31-38). Test #5 pins the constant value.

**R3 — `port.CompletionRequest` has no org-id field by design.**
`port.CompletionRequest` (`internal/domain/port/llm.go:137`)
intentionally has no `OrganizationID` field — the godoc explicitly
states: *"Correlation identifiers (correlation_id, job_id, version_id,
organization_id, created_by_user_id) are propagated via context, not
fields, so the wire envelope stays minimal and PII-free"*
(llm.go:134-136). Therefore the security model's "organization_id
передаётся в Metadata LLM-вызова" requirement is satisfied
STRUCTURALLY — there is no field on the wire DTO to carry it. Test #4
asserts both surfaces:
  - `System`, `User`, of every `FakeLLMCall` do NOT contain the test's
    `organization_id` substring;
  - via `reflect.TypeOf(port.CompletionRequest{})`, NO field named
    `OrganizationID` / `TenantID` / `OrgID` exists.

## Hermeticity

External test package (`package tenantisolation_test`). Allowed imports:

- stdlib (`context`, `reflect`, `strings`, `testing`, `time`,
  `encoding/json`);
- `internal/domain/{model,port}`;
- `internal/infra/observability/{tracer,metrics}` (test 5/6 only);
- `internal/integration/{fakes,lictestapp}`;
- `github.com/google/uuid` (canonical UUIDs);
- `github.com/prometheus/client_model/go` as `dto` (test 6 walks
  `*dto.LabelPair`).

No new `go.mod` additions: `prometheus/client_model` is already a
transitive dependency (used by `internal/infra/observability/metrics`
test code) — verified by `go build ./...` passing without changes.

## Forward notes

1. **FakeLLMCall does not expose PriorTurns content.** Test #4
   asserts only `System` and `User` for the absence of the org-id
   substring. If a future agent leaks `organization_id` into a
   `PriorTurn` (the repair-loop conversation history,
   `port.Turn.Content`), the test will not catch it. Extend
   `fakes/llm.go` `FakeLLMCall` to carry `PriorTurns []port.Turn` and
   re-run this test to close that gap.
