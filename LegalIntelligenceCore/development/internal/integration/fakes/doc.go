// Package fakes is the in-memory integration-test harness for the Legal
// Intelligence Core (LIC-TASK-048, high-architecture.md §6, integration-
// contracts.md §6, ai-agents-pipeline.md §1–§9).
//
// Production code is unchanged. This is a test-support package: it ships
// the in-memory doubles that LIC-TASK-049/050/051 (integration tests for
// happy path, low-confidence pause+resume, RE_CHECK / agent failure /
// provider fallback / DLQ / prompt injection) plug into orchestrator,
// router, agents, idempotency guard, awaiter and publisher components,
// REPLACING the real RabbitMQ / Redis / LLM / DM dependencies at the seams
// where those tasks already inverted them.
//
// Exported types:
//
//   - FakeBroker      — satisfies consumer.BrokerSubscriber AND every egress
//                       Publisher seam (dm/orch/dlq). Records publishes,
//                       routes Inject(routingKey, ...) to all queues bound
//                       on that key, owns manual-ACK semantics through
//                       FakeDelivery. Ships a frozen LIC-topology preset
//                       (the six §6.1 queue→routing-key bindings) so tests
//                       just call NewFakeBrokerWithLICTopology().
//   - FakeDelivery    — broker.Delivery implementation that records exactly
//                       one of Ack/Nack/Reject per delivery and surfaces a
//                       lifecycle error on double-terminate.
//   - FakeKVStore     — in-memory Redis double with REAL-TIME TTL (not
//                       frozen). Satisfies idempotency.RedisSeam +
//                       ratelimit.LuaEvaluator + the general kvstore op
//                       surface (Get/Set/SetNX/Delete/Expire/Eval +
//                       Ping/Close). Recognises the two LIC Lua scripts
//                       (luaSetNXOrGet, tokenBucketScript) by source-string
//                       identity and executes their observable semantics
//                       in pure Go.
//   - FakeLLMProvider — satisfies port.LLMProviderPort. FIFO per-(AgentID,
//                       Model) response queue + per-call error injection +
//                       passthrough of JSONSchema strict mode (the canned
//                       Content is returned verbatim; downstream Schema
//                       Validator gates it the same way it gates real
//                       providers).
//   - FakeDM          — listens to FakeBroker for LIC's two outbound DM
//                       wires (lic.requests.artifacts,
//                       lic.artifacts.analysis-ready) and publishes the DM-
//                       side responses back through FakeBroker.Inject. Per-
//                       version programmable outcomes:
//                       ArtifactsProvided{success, missing, error}; persist
//                       outcomes Persisted/PersistFailed; configurable
//                       per-call delay.
//   - TestRig         — bundles FakeBroker, FakeKVStore, FakeLLMProvider and
//                       FakeDM (the four collaborators every higher-level
//                       integration test composes). NewTestRig(t) returns
//                       a ready-to-use rig with the LIC topology preset
//                       and the FakeDM listener wired.
//
// Helper surfaces:
//
//   - fixtures.go     — realistic Russian-language artifact blobs
//                       (SemanticTreeRU, ExtractedTextRU, DocumentStructureRU)
//                       and nine canned agent responses (one per AGENT_*),
//                       all schema-valid against internal/agents/schemas.
//                       Builders for ArtifactsProvided and AgentInput so
//                       tests construct envelopes by name, not by hand.
//   - verifier.go     — AssertPublished / WaitForPublish / MatchEvent over
//                       FakeBroker.Published(); a Tb interface so the same
//                       helper drives a *testing.T and (in this package's
//                       own tests) a recording-Tb double.
//
// Hermeticity: imports stdlib + internal/domain/{model,port} +
// internal/infra/broker (types only: broker.MessageHandler / broker.Delivery
// / broker.XDeathEntry — the same types-only import the consumer adapter
// uses). NO RabbitMQ, NO Redis, NO Prometheus, NO concrete *kvstore.Client /
// *broker.Client / *llm.* provider. This is enforced structurally by the
// var-_-pinning in seams_check_test.go.
//
// BuildTestApp deferral (RECORDED). Acceptance criteria mention "Helper
// BuildTestApp(t): wires fakes в App". app.New() (internal/app/app.go) is a
// 983-line concrete wiring that consumes *kvstore.Client AND *broker.Client
// directly for two collaborators (pendingStateStore, versionMetaCache —
// app/pending_state.go, app/version_meta_cache.go). A faithful BuildTestApp
// either (a) refactors app.New() into factory-injected form, or (b) builds
// a parallel orchestrator-only wiring that bypasses those two collaborators
// when the pipeline path under test does not exercise pending-state /
// version-meta. Neither is in 048's scope; both are correctly the concern
// of the consumer tests (049 happy path, 050 pause+resume, 051 RE_CHECK +
// agent failure + provider fallback + DLQ). 048 ships the rig fakes; the
// consuming task picks the strategy that fits the path it tests.
package fakes
