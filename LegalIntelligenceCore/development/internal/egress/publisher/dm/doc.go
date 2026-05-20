// Package dm implements the LIC outbound publishers for the two DM-facing
// wire topics (high-architecture.md §6.5 steps 1 and 8;
// integration-contracts.md §2 + §6.1; DocumentManagement/architecture/
// event-catalog.md §1.4 + §1.5; LIC event-catalog.md §2; ADR-LIC-05).
//
// Two exported types, one per topic — each a separate side of the same
// hermetic boundary:
//
//   - ArtifactRequester (LIC-TASK-042) — publishes lic.requests.artifacts,
//     the OUTBOUND request used to ask Document Management for the DP-side
//     artifacts of a given version. Each call produces exactly ONE wire
//     message (no fan-out, no retry loop — those concerns belong to the
//     pipeline orchestrator at LIC-TASK-036, build-spec D10/D15).
//
//   - AnalysisArtifactsPublisher (LIC-TASK-043) — publishes
//     lic.artifacts.analysis-ready, the TERMINAL publication carrying the
//     consolidated payload of all eight mandatory artifacts plus the
//     optional v1.1 risk_delta extension. Each call produces exactly ONE
//     wire message; DM persists, then emits the persist-confirmation that
//     PersistConfirmationAwaiterPort awaits (high-architecture.md §6.5
//     steps 8-10).
//
// Both types satisfy their respective domain ports (compile-time var _
// assertions in requester.go / publisher.go) and are concurrency-safe
// across distinct correlation_ids — stateless after construction
// (build-spec D12).
//
// The two publishers share the seam stack — Publisher (broker), Metrics,
// Clock, Logger — and the same hermeticity contract. The Metrics seam
// carries TWO methods: IncPublish (both publishers; the §3.9 counter) and
// ObservePublishedSize (analysis-ready only; the §3.5 histogram is
// specific to the terminal payload).
//
// Hermetic boundary (build-spec D13, enforced by internal_test.go.
// TestHermeticImports): non-test source imports only stdlib plus EXACTLY
// these three first-party paths:
//
//   - contractpro/legal-intelligence-core/internal/domain/model
//     (model.ArtifactType + IsValid for pre-publish validation;
//     model.RiskDelta and the eight artifact types are referenced
//     transitively via port.LegalAnalysisArtifactsReady)
//   - contractpro/legal-intelligence-core/internal/domain/port
//     (port.ArtifactRequesterPort, port.GetArtifactsRequest,
//     port.AnalysisArtifactsPublisherPort, port.LegalAnalysisArtifactsReady)
//   - contractpro/legal-intelligence-core/internal/infra/broker
//     (TWO sentinel errors ONLY — ErrPublishNack and ErrConfirmTimeout —
//     for the broker-outcome classifier. The broker package is a
//     compile-time dependency only on a Publisher seam (see
//     seams.go.Publisher); the seam keeps the concrete *broker.Client
//     out of this package. This is the documented R2 exception to the
//     otherwise infra-free egress allowlist.)
//
// The labels.PublishOutcome typed enum used by the metrics seam is
// MIRRORED locally in seams.go as PublishOutcome (the universal
// base.Outcome / router.CallOutcome / cost / schemavalidator local-mirror
// precedent — keeps the production source hermetic). The metrics-SSOT
// pin lives in seams_test.go.
//
// No internal/config import (RequesterConfig / PublisherConfig are local
// values, injected by the LIC-TASK-036 wiring), no
// internal/infra/observability/metrics import in production source (a
// concrete prometheus transitive would break hermeticity — Metrics is
// seamed away), no third-party path (broker AMQP, prometheus, otel are
// all behind seams or out of scope here).
//
// Design adjudicated by subagent code-architect:
//   - ArtifactRequester (LIC-TASK-042): decisions D1..D15, reconciliations
//     R1..R3.
//   - AnalysisArtifactsPublisher (LIC-TASK-043): decisions D1..D18,
//     reconciliations R1..R5 — same hermetic contract; extends the
//     Metrics seam with ObservePublishedSize (§3.5 histogram) and adds
//     the eight artifact-pointer required-field validation branches plus
//     the timestamp-rewrite-via-value-receiver contract (build-spec D5).
//
// Both implementations by subagent golang-pro. The authoritative
// reconciliations are recorded in this package's CLAUDE.md.
package dm
