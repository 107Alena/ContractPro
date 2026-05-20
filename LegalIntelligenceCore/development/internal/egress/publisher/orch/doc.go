// Package orch implements the LIC outbound Status Event Publisher for the
// Orchestrator-facing wire topic lic.events.status-changed (LIC
// event-catalog.md §1.1, observability.md §3.9, high-architecture.md §6.13,
// LIC-TASK-044).
//
// One exported type satisfying ONE structural role:
//
//   - StatusPublisher — port.StatusPublisherPort. Publishes a single
//     LICStatusChangedEvent envelope per call, on the FROZEN topic
//     lic.events.status-changed. Used by the pipeline orchestrator
//     (LIC-TASK-036) for every external-status transition: IN_PROGRESS
//     (start, also stage=STAGE_AWAITING_USER_CONFIRMATION at the
//     classification-uncertain pause), COMPLETED (terminal-success), FAILED
//     (terminal-failure with publishable ErrorCode + ErrorMessage +
//     IsRetryable). Each call produces EXACTLY ONE wire message; no fan-out,
//     no retry loop (those concerns belong to the orchestrator at
//     LIC-TASK-036). The Orchestrator deduplicates by
//     `lic-status:{job_id}:{status}` (LIC event-catalog §1.1), so safe
//     re-publication on crash-recovery is supported by design.
//
// The type satisfies its domain port (compile-time `var _` assertion in
// publisher.go) and is concurrency-safe across distinct correlation_ids —
// stateless after construction.
//
// This package is the sibling of internal/egress/publisher/dm but
// intentionally has NO source-level dependency on it. Both publishers share
// the same hermetic boundary and the same seam SHAPE (Publisher / Metrics /
// Clock / Logger) but each owns its own copy of those interfaces and its
// own local PublishOutcome mirror so future Orchestrator-facing publishers
// (LIC-TASK-045 ClassificationUncertain, LIC-TASK-046 DLQ) can live in this
// same package without re-importing dm.
//
// Hermetic boundary (enforced by internal_test.go.TestHermeticImports):
// non-test source imports only stdlib plus EXACTLY these three first-party
// paths:
//
//   - contractpro/legal-intelligence-core/internal/domain/model
//     (model.ExternalStatus + IsValid, model.Stage + IsValid,
//     model.ErrorCode + IsPublishableToOrchestrator, model.LookupErrorSpec
//     for the ErrorMessage rewrite from the catalog SSOT)
//   - contractpro/legal-intelligence-core/internal/domain/port
//     (port.StatusPublisherPort, port.LICStatusChangedEvent)
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
// No internal/config import (PublisherConfig is a local value, injected by
// the LIC-TASK-036 wiring), no
// internal/infra/observability/metrics import in production source (a
// concrete prometheus transitive would break hermeticity — Metrics is
// seamed away), no internal/egress/publisher/dm import (sibling-not-parent
// — actively forbidden in internal_test.go), no third-party path (broker
// AMQP, prometheus, otel are all behind seams or out of scope here).
//
// Notable differences from the sibling dm publisher (deliberate, documented
// in code):
//
//   - OrganizationID is REQUIRED here (`organization_id` carries no
//     `omitempty` on port.LICStatusChangedEvent — every Orchestrator-bound
//     status event has a known organization). The sibling dm.GetArtifactsRequest
//     keeps OrganizationID optional.
//   - The Metrics seam carries ONLY IncPublish — there is no published-size
//     histogram for the status envelope (small, fixed shape; observability.md
//     §3.5 size histogram is specific to the lic.artifacts.analysis-ready
//     terminal payload).
//   - Status-conditional validation (FAILED requires ErrorCode +
//     IsRetryable; non-FAILED forbids any of ErrorCode/ErrorMessage/
//     IsRetryable — stale-data-leak guard).
//   - ErrorMessage rewrite from model.LookupErrorSpec(code).UserMessage on
//     FAILED — the catalog is the single source of truth for the RU
//     user-facing rendering (NFR-5.2).
//
// Design adjudicated by subagent code-architect:
//   - StatusPublisher (LIC-TASK-044): decisions D1..D13, reconciliations
//     R1..R6 — same hermetic contract as dm; differences listed above.
//
// Implementation by subagent golang-pro. The authoritative reconciliations
// are recorded in this package's CLAUDE.md.
package orch
