// Package orch implements the LIC outbound Orchestrator-facing publishers
// for two FROZEN wire topics (LIC event-catalog.md §1.1 + §1.2,
// observability.md §3.9, high-architecture.md §6.13). LIC-TASK-046 DLQ
// will land as a third sibling here without growing the allowlist.
//
// Two exported types, each satisfying ONE structural role:
//
//   - StatusPublisher — port.StatusPublisherPort (LIC-TASK-044). Publishes a
//     single LICStatusChangedEvent envelope per call on the FROZEN topic
//     lic.events.status-changed. Used by the pipeline orchestrator
//     (LIC-TASK-036) for every external-status transition: IN_PROGRESS
//     (start, also stage=STAGE_AWAITING_USER_CONFIRMATION at the
//     classification-uncertain pause), COMPLETED (terminal-success), FAILED
//     (terminal-failure with publishable ErrorCode + ErrorMessage +
//     IsRetryable). The Orchestrator deduplicates by
//     `lic-status:{job_id}:{status}` (LIC event-catalog §1.1).
//   - UncertaintyPublisher — port.UncertaintyPublisherPort (LIC-TASK-045).
//     Publishes a single ClassificationUncertain envelope per call on the
//     FROZEN topic lic.events.classification-uncertain. Used by the
//     pendingconfirmation Manager (LIC-TASK-037) once per version when
//     Agent 1 returns confidence < LIC_CONFIDENCE_THRESHOLD. The
//     Orchestrator deduplicates by `lic-uncertain:{version_id}` (LIC
//     event-catalog §1.2).
//
// Each call on either publisher produces EXACTLY ONE wire message; no
// fan-out, no retry loop, no DLQ routing (those concerns belong to the
// caller — orchestrator at LIC-TASK-036 and pendingconfirmation Manager at
// LIC-TASK-037). Both types satisfy their respective domain ports
// (compile-time `var _` assertions in publisher.go and uncertainty.go) and
// are concurrency-safe across distinct correlation_ids — stateless after
// construction. Safe re-publication on crash-recovery is supported by
// design (Orchestrator-side dedup keys).
//
// This package is the sibling of internal/egress/publisher/dm but
// intentionally has NO source-level dependency on it. Both publishers
// share the same hermetic boundary and the same seam SHAPE (Publisher /
// Metrics / Clock / Logger) — all defined in seams.go and SHARED across
// both 044 and 045 — but each owns its own Config + Deps + topic constant
// + marshal seam so future Orchestrator-facing publishers (LIC-TASK-046
// DLQ) can live in this same package without re-importing dm.
//
// Hermetic boundary (enforced by internal_test.go.TestHermeticImports):
// non-test source imports only stdlib plus EXACTLY these three first-party
// paths:
//
//   - contractpro/legal-intelligence-core/internal/domain/model
//     (model.ExternalStatus + IsValid, model.Stage + IsValid,
//     model.ErrorCode + IsPublishableToOrchestrator, model.LookupErrorSpec
//     for the ErrorMessage rewrite from the catalog SSOT — StatusPublisher;
//     model.ContractType + IsValid 12-whitelist,
//     model.ClassificationAlternative — UncertaintyPublisher)
//   - contractpro/legal-intelligence-core/internal/domain/port
//     (port.StatusPublisherPort + port.LICStatusChangedEvent;
//     port.UncertaintyPublisherPort + port.ClassificationUncertain)
//   - contractpro/legal-intelligence-core/internal/infra/broker
//     (TWO sentinel errors ONLY — ErrPublishNack and ErrConfirmTimeout —
//     for the broker-outcome classifier. The broker package is a
//     compile-time dependency only on a Publisher seam (see seams.go.
//     Publisher); the seam keeps the concrete *broker.Client out of this
//     package. Documented exception to the otherwise infra-free egress
//     allowlist — same R2 rationale as the sibling dm publisher.)
//
// The metrics.PublishOutcome typed enum used by the metrics seam is
// MIRRORED locally in seams.go as PublishOutcome (the universal
// base.Outcome / router.CallOutcome / cost / schemavalidator / dm
// local-mirror precedent — keeps the production source hermetic). The
// metrics-SSOT pin lives in seams_test.go.
//
// No internal/config import (StatusPublisherConfig and
// UncertaintyPublisherConfig are local values, injected by the
// LIC-TASK-036 / TASK-047 wiring), no
// internal/infra/observability/metrics import in production source (a
// concrete prometheus transitive would break hermeticity — Metrics is
// seamed away), no internal/egress/publisher/dm import (sibling-not-parent
// — actively forbidden in internal_test.go), no third-party path (broker
// AMQP, prometheus, otel are all behind seams or out of scope here).
//
// Notable differences from the sibling dm publisher (deliberate,
// documented in code):
//
//   - OrganizationID is REQUIRED in BOTH 044 and 045 (no `omitempty` on
//     port.LICStatusChangedEvent.OrganizationID or
//     port.ClassificationUncertain.OrganizationID — every
//     Orchestrator-bound event has a known organization). The sibling
//     dm.GetArtifactsRequest keeps OrganizationID optional.
//   - The Metrics seam carries ONLY IncPublish — there is no published-size
//     histogram for either envelope (both are small with a fixed shape;
//     observability.md §3.5 size histogram is specific to the
//     lic.artifacts.analysis-ready terminal payload).
//   - StatusPublisher (044) has status-conditional validation (FAILED
//     requires ErrorCode + IsRetryable; non-FAILED forbids any of
//     ErrorCode/ErrorMessage/IsRetryable — stale-data-leak guard) +
//     ErrorMessage rewrite from model.LookupErrorSpec(code).UserMessage on
//     FAILED (RU NFR-5.2 — the catalog is the SSOT).
//   - UncertaintyPublisher (045) validates SuggestedType against the
//     12-whitelist, Confidence and Threshold each in [0, 1] with EXPLICIT
//     NaN-handling (math.IsNaN guard — Go float comparisons against NaN
//     return false), and (when non-empty) every alternative item the same
//     way. There is no ErrorMessage rewrite — the envelope has no error
//     field. The only post-validation rewrite is Timestamp via the Clock
//     seam to RFC3339Nano UTC.
//
// Design adjudicated by subagent code-architect:
//   - StatusPublisher (LIC-TASK-044): decisions D1..D13, reconciliations
//     R1..R6 — same hermetic contract as dm; differences listed above.
//   - UncertaintyPublisher (LIC-TASK-045): build-spec D1..D18 (symmetric
//     re-use of seam stack, broker classifier and PublishError;
//     dedicated Config/Deps/topic/marshal/reasons), reconciliations
//     R5-NaN-explicit / R6-AltNilEmpty / R7-NoErrorMessageRewrite.
//
// Implementation by subagent golang-pro. The authoritative reconciliations
// are recorded in this package's CLAUDE.md.
package orch
