// Package dm implements the LIC DM Artifact Requester
// (LIC-TASK-042, high-architecture.md §6.5 step 1, integration-contracts.md
// §2 + §6.1, DocumentManagement/architecture/event-catalog.md §1.4). It is
// the OUTBOUND publisher for the lic.requests.artifacts topic — the only
// way LIC asks Document Management for the DP-side artifacts of a given
// version. Each call produces exactly ONE wire message (no fan-out, no
// retry loop, no version-meta caching — those concerns belong to the
// pipeline orchestrator at LIC-TASK-036, build-spec D10/D15).
//
// The exported type ArtifactRequester satisfies port.ArtifactRequesterPort
// (var _ assertion in requester.go) and is concurrency-safe across distinct
// correlation_ids — it is stateless after construction (build-spec D12).
//
// Hermetic boundary (build-spec D13, enforced by internal_test.go.
// TestHermeticImports): non-test source imports only stdlib plus EXACTLY
// these three first-party paths:
//
//   - contractpro/legal-intelligence-core/internal/domain/model
//     (model.ArtifactType + IsValid for pre-publish validation)
//   - contractpro/legal-intelligence-core/internal/domain/port
//     (port.ArtifactRequesterPort, port.GetArtifactsRequest)
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
// No internal/config import (Config is a local value, injected by the
// LIC-TASK-036 wiring), no internal/infra/observability/metrics import in
// production source (a concrete prometheus transitive would break
// hermeticity — Metrics is seamed away), no third-party path (broker
// AMQP, prometheus, otel are all behind seams or out of scope here).
//
// Design adjudicated by subagent code-architect (build-spec — decisions
// D1..D15, reconciliations R1..R3); implemented by subagent golang-pro.
// The authoritative reconciliations are recorded in this package's
// CLAUDE.md.
package dm
