// Package dlq implements the LIC dead-letter-queue publisher for the four
// PII-safe DLQ topics (LIC-TASK-046). Failures across the consumer / agent /
// publisher boundaries are routed here so DLQ depth is the SSOT post-mortem
// signal (error-handling.md §9, integration-contracts.md §10,
// event-catalog.md §3, observability.md §3.8, security.md §6.4-6.5).
//
// One exported type — DLQPublisher — satisfies port.DLQPublisherPort
// (compile-time var _ assertion in publisher.go). PublishDLQ is goroutine-
// safe across distinct envelopes; the only shared state is the broker
// Publisher seam, which serializes publishes internally on its pubMu.
//
// The envelope is PII-safe by construction: callers compute
// OriginalMessageHash via HashPayload (HMAC-SHA-256 over the FULL raw
// payload keyed by LIC_DLQ_HASH_KEY) and the optional RawLLMResponseHash
// via HashRawLLMResponse (HMAC-SHA-256 over the first 1024 bytes per
// security.md §6.4) BEFORE invoking PublishDLQ. The publisher never sees
// raw payloads — its only inputs are the topic and the envelope.
//
// For lic.dlq.publish-failed, the failing payload (LegalAnalysisArtifactsReady
// with real-tenant PII) is logged by the CALLER as a structured warning
// BEFORE invoking PublishDLQ (task acceptance criteria; integration-contracts
// §10.2 — v1 object-storage retention is optional). The DLQ publisher
// itself only handles the envelope; it never logs payloads it cannot see.
//
// Hermetic boundary (enforced by internal_test.go.TestHermeticImports):
// non-test source imports only stdlib plus EXACTLY these three first-party
// paths:
//
//   - contractpro/legal-intelligence-core/internal/domain/model
//     (model.ErrorCode, model.AgentID, model.Stage referenced via the
//     envelope)
//   - contractpro/legal-intelligence-core/internal/domain/port
//     (DLQPublisherPort, LICDLQEnvelope, DLQTopic)
//   - contractpro/legal-intelligence-core/internal/infra/broker
//     (TWO sentinel errors ONLY — ErrPublishNack and ErrConfirmTimeout —
//     for the broker-outcome classifier; the concrete *broker.Client is
//     behind the Publisher seam — same R2 exception as the sibling dm /
//     orch publishers.)
//
// The metrics.PublishOutcome typed enum used by the publisher counter is
// MIRRORED locally in seams.go as PublishOutcome (the universal
// base.Outcome / router.CallOutcome / cost / schemavalidator local-mirror
// precedent — keeps the production source hermetic). The metrics-SSOT pin
// lives in seams_test.go.
//
// No internal/config import (Config is a local value, injected by the
// LIC-TASK-047 wiring), no internal/infra/observability/metrics import in
// production source (a concrete prometheus transitive would break
// hermeticity — Metrics is seamed away), no third-party path.
//
// Design adjudicated by subagent code-architect (Q1..Q6 reconciliations):
// Q1 hash helpers co-located in this package (uncapped HashPayload +
// 1024-byte-capped HashRawLLMResponse); Q2 reason label is the 1:1 topic
// derivation (4 values, sub-budget within §3.10); Q3 minimal required-set
// validation (best-effort correlation IDs per integration-contracts §10.1);
// Q4 caller logs raw payload before PublishDLQ — publisher never sees PII;
// Q5 Logger reserved-for-future-use (mirror dm/orch); Q6 FailedAt
// auto-stamped only if empty (caller-set values preserved — semantic
// difference vs the dm/orch Timestamp-always-overwrite).
package dlq
