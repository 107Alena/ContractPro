# fakes Package — CLAUDE.md

**Integration test framework — in-memory fakes** for the Legal Intelligence
Core (LIC-TASK-048, `high-architecture.md` §6, `integration-contracts.md`
§6, `ai-agents-pipeline.md` §1–§9). Production code is **unchanged** —
this is a test-support package consumed by LIC-TASK-049 / 050 / 051 / 054
(happy-path INITIAL pipeline, low-confidence pause + resume, RE_CHECK /
agent failure / provider fallback / DLQ / prompt injection, tenant
isolation).

Imports stdlib + `internal/domain/{model,port}` + `internal/infra/broker`
(types only — `broker.MessageHandler` / `broker.Delivery` /
`broker.XDeathEntry`, identical to the consumer adapter's
`infra/broker`-types-only import) + `internal/llm/ratelimit` (the
`LuaEvaluator` seam type assertion is co-located with this package's own
implementation). NO RabbitMQ, NO Redis, NO Prometheus, NO concrete
`*kvstore.Client` / `*broker.Client` / `*llm.*` provider — the test
helper is the substitute for those production types behind the seams the
037 / 038 / 042 / 043 / 044 / 045 / 046 adapters already established.

## Files

- **doc.go** — package documentation: type roster, helper surfaces,
  hermetic statement, the BuildTestApp deferral note (see below).
- **broker.go** — `FakeBroker`, `FakeDelivery`, LIC topology bindings
  (`LICTopologyBindings()` + `NewFakeBrokerWithLICTopology()`),
  `PublishedMessage`, `InjectResult`, `XDeathHeader` helper. Both
  Publish (outbound LIC → broker) and Inject (inbound broker → LIC
  queue) record into one log so verifier helpers (`PublishedOn`,
  `WaitForPublish`) work in both directions.
- **kvstore.go** — `FakeKVStore` (real-time TTL, lazy expiry), Lua
  dispatch by substring marker for the two LIC scripts
  (`luaSetNXOrGet`, `tokenBucketScript`), Ping FIFO failure queue,
  Close, Reset, Size/TTL helpers. ErrKeyNotFound mirrors the kvstore
  sentinel by name only (see "Sentinel adapter" below).
- **llm.go** — `FakeLLMProvider` (port.LLMProviderPort), FIFO response
  queue per (AgentID, Model), per-call error injection, latency
  simulation, JSONSchema-set passthrough on the recorded call,
  HealthCheck dual-failure-mode plumbing.
- **dm.go** — `FakeDM`, `ArtifactsResponse`, `PersistOutcome`,
  `ObservedArtifactRequest` / `ObservedAnalysisReady`. Subscribes via
  `FakeBroker.OnPublish` to LIC's two outbound DM wires; responds
  through `FakeBroker.Inject` so the production consumer code path
  sees a real delivery. Per-version programmable outcomes; default
  echo for unconfigured versions.
- **fixtures.go** — Russian-language SEMANTIC_TREE / EXTRACTED_TEXT /
  DOCUMENT_STRUCTURE / PROCESSING_WARNINGS / parent RISK_ANALYSIS
  blobs + nine canned agent responses (one per `model.AgentID`,
  schema-valid against `internal/agents/schemas`) +
  `CannedResponseFor`, `DefaultArtifactsBundle`, `ReCheckArtifactsBundle`,
  `BuildAgentInput`, `BuildArtifactsResponse`.
- **verifier.go** — `Tb` minimal interface, `AssertPublished` /
  `AssertNotPublished` / `WaitForPublish[After]` / `MatchEvent` /
  `AssertPayloadField` / `AssertNoErrors` / `IsTimeout`. The recorded
  `recordingTb` in verifier_test.go uses `runtime.Goexit` to mirror
  `*testing.T.Fatalf` semantics.
- **rig.go** — `TestRig` and `NewTestRig(t)` — bundles the four fakes
  with the LIC topology preset and `t.Cleanup` registered.
  `InstallCannedAgentResponses` is a convenience for per-agent
  provider/model mapping.
- **broker_test.go** — 20 tests (topology preset, Subscribe, Publish,
  Inject fan-out, FakeDelivery Ack/Nack/Reject + XDeath, OnPublish,
  ResetPublished, Close idempotency, race-clean 16×32 concurrent
  publish + listener).
- **kvstore_test.go** — 19 tests (miss → ErrKeyNotFound, Set/Get
  round-trip, TTL=0 no expiry, SetNX first-writer-wins +
  expired-key-allows-reset, Expire true/false, Delete count, Ping
  FIFO failure, Close, Eval unknown / idempotency acquired /
  idempotency present / idempotency TTL re-acquire, token-bucket
  allow-then-deny / hash persistence, Eval ctx, Reset, string-TTL
  arg, race-clean 32-goroutine SetNX fairness).
- **llm_test.go** — 14 tests (Complete no-response→typed-malformed,
  FIFO drain, error wins over response, bare→typed wrap, typed
  passthrough, call recording with JSONSchemaSet, ctx cancel,
  latency, HealthCheck three modes, SetResponses replace, default
  response, race-clean 16×32 concurrent Complete).
- **dm_test.go** — 8 tests (default echo, per-version override, Drop
  → no publish, persist success default, persist failure
  per-version, response delay drives timeout, observed-logs,
  Stop drains in-flight).
- **fixtures_test.go** — 6 tests (every fixture is valid JSON,
  every agent has a schema-valid canned response, default bundle has
  the four mandatory artifacts + NOT RISK_ANALYSIS, RE_CHECK bundle
  has RISK_ANALYSIS, BuildAgentInput copies the artifacts map,
  BuildArtifactsResponse wrapping, unknown agent panics).
- **verifier_test.go** — 11 tests covering all helpers + the
  recordingTb / Goexit pattern.
- **rig_test.go** — 5 tests (rig wires every fake, topology preset,
  cleanup safe, InstallCannedAgentResponses happy path + skip-unmapped).
- **seams_check_test.go** — 7 `var _ Iface = (*Impl)(nil)` cross-
  package satisfaction assertions:
  - `consumer.BrokerSubscriber  = (*FakeBroker)(nil)`
  - `dmpub.Publisher            = (*FakeBroker)(nil)`
  - `orchpub.Publisher          = (*FakeBroker)(nil)`
  - `dlqpub.Publisher           = (*FakeBroker)(nil)`
  - `idempotency.RedisSeam      = (*FakeKVStore)(nil)`
  - `ratelimit.LuaEvaluator     = (*FakeKVStore)(nil)`
  - `port.LLMProviderPort       = (*FakeLLMProvider)(nil)`
- **CLAUDE.md** — this file.

## API

### FakeBroker

- `NewFakeBroker()` / `NewFakeBrokerWithLICTopology()`.
- `Bind(queue, routingKey)` — register a binding (idempotent).
- `Subscribe(queue, broker.MessageHandler) error` — satisfies
  `consumer.BrokerSubscriber`.
- `Publish(ctx, exchange, routingKey, payload) error` — satisfies
  `dm/orch/dlq.Publisher`; records into the same log Inject uses.
- `Inject(ctx, routingKey, headers, body) (InjectResult, error)` —
  fans out to every queue bound to routingKey; synchronous (waits
  for handler return + delivery terminate); records into the log.
- `OnPublish(routingKeyOrEmpty, PublishListener)` — fires after each
  matching record; FakeDM uses this for its DM-response loop.
- `InjectPublishError(err)` — FIFO error queue consumed by Publish.
- `Published()` / `PublishedOn(rk)` / `ResetPublished()`.
- `Close()` / `Closed()`.

### FakeDelivery (broker.Delivery)

Body / Header / Headers (shallow-copy) / XDeath (typed slice OR
wire-faithful `[]any`-of-tables) / Ack / Nack(requeue) /
Reject(requeue) / Acked / Nacked / Rejected / Terminated. The second
Ack/Nack/Reject returns `ErrAlreadyTerminated`.

### FakeKVStore

- `NewFakeKVStore()`.
- `Get/Set/SetNX/Delete/Expire/Ping/Close` — mirrors the production
  `*kvstore.Client.<op>` shape byte-for-byte.
- `Eval(ctx, script, keys, args...) (any, error)` — dispatches to
  the LIC Lua handlers by substring marker; unknown script returns
  `ErrUnknownLuaScript`.
- `InjectPingError(err)` — FIFO Ping failure queue.
- `Reset()` / `Size()` / `TTL(key)`.
- Sentinel: `ErrKeyNotFound` (own value — see "Sentinel adapter").
- Sentinel: `ErrUnknownLuaScript`.

### FakeLLMProvider (port.LLMProviderPort)

- `NewFakeLLMProvider(id)`.
- `SetResponse / SetResponses / SetResponseJSON / SetDefaultResponse`.
- `InjectError(agent, model, err)` — FIFO; wins over response.
- `SetLatency(d)`.
- `SetHealth(typedFailure, transportFailure)`.
- `Complete / HealthCheck / ID` — port.LLMProviderPort.
- `Calls()` / `Pending(agent, model)` / `Reset()`.

### FakeDM

- `NewFakeDM(broker)` + `Start()` + `Stop()`.
- `SetArtifactsResponse(versionID, ArtifactsResponse)` +
  `SetDefaultArtifactsResponse`.
- `SetPersistOutcome(versionID, PersistOutcome)` +
  `SetDefaultPersistOutcome`.
- `SetResponseDelay(d)`.
- `ArtifactRequests()` / `AnalysisReady()`.

### TestRig

- `NewTestRig(t) *TestRig` — bundles `FakeBroker` (LIC topology
  preset) + `FakeKVStore` + per-LLMProviderID `FakeLLMProvider` +
  `FakeDM` (Start-ed), with `t.Cleanup` registered.
- `InstallCannedAgentResponses(providerByAgent, modelByAgent, agents)`.

### Helpers (verifier.go)

- `Tb` — narrow Helper / Fatalf interface.
- `WaitForPublish(ctx, fb, rk)` / `WaitForPublishAfter(ctx, fb, rk, since)`.
- `AssertPublished(t, fb, rk) PublishedMessage`.
- `AssertNotPublished(t, fb, rk)`.
- `MatchEvent(t, msg, *out)` / `AssertPayloadField(t, msg, key, want)`.
- `AssertNoErrors(t, errs...)`.
- `IsTimeout(err)`.

## Conventions & deliberate decisions

- **Test-support package, NOT production.** Lives under
  `internal/integration/fakes/` next to the LIC-TASK-053 prompt-
  injection harness. The four fakes are imported by consumer
  integration tests (`internal/integration/*_test.go` in 049+); they
  must NOT appear in production transitive imports — verified by the
  `internal/infra/broker` types-only constraint and the seam-side
  hermeticity tests already shipped in the consumer / idempotency /
  egress packages (no production file imports
  `internal/integration/fakes`).

- **Inject AND Publish both record.** A FakeBroker is the entire
  broker fabric, not "LIC's view of the broker". Inject simulates a
  message arriving from outside (DM-side publish); Publish simulates
  LIC publishing. Both are observable broker activity and tests
  benefit from one unified log. The semantic distinction (direction)
  is recoverable from the routing-key (lic.* vs dm.* vs orch.*).

- **Inject is synchronous.** Inject returns AFTER every subscriber
  handler returns AND every delivery is terminated (Ack/Nack/
  Reject). Tests can immediately inspect downstream state without
  polling. Asynchronous response paths (FakeDM's reply goroutines)
  remain — and `WaitForPublish` covers those.

- **REAL-TIME TTL for FakeKVStore.** Acceptance criterion "real
  time, не frozen". No injectable clock — the production
  `idempotency.Guard`, `pendingconfirmation.Manager`, and the
  token-bucket all already accept their own deterministic clocks via
  seams; the FAKE doesn't need to drive their time. Tests that need
  TTL boundaries use `time.Sleep` against small durations
  (10-50ms range — the bucket-test pattern).

- **Lua dispatch by substring marker, NOT script-source equality.**
  The two LIC scripts are private to their owning packages. Hard-
  coding the full source here would require an out-of-band update on
  every script tweak; substring markers chosen for uniqueness inside
  the LIC Lua corpus survive whitespace/comment edits. Unknown
  script returns `ErrUnknownLuaScript` so a future Lua addition
  surfaces as a clear "register me" defect, not as a silent
  miss-dispatch.

- **Token-bucket math co-located, mirrored from
  `ratelimit.computeBucket`.** Tests in
  `ratelimit/script_test.go` pin the production Go SSOT; the fake
  duplicates the same arithmetic verbatim in `computeBucketSlim`
  because the production helper is private. Drift between the two
  would surface in LIC-TASK-049+ as test failures comparing
  actual-fake rates against expected rates; both implementations
  trace back to the script source. (Note: importing
  `internal/llm/ratelimit` is permitted in this test-helper package.)

- **JSONSchema strict mode = passthrough + recording.** The fake
  returns the installed canned `Content` verbatim. The downstream
  Schema Validator (LIC-TASK-023) gates the bytes the same way it
  gates a real provider, so the integration test path is faithful.
  The fake records `JSONSchemaSet=true` on the call so a test
  asserting "router asked for structured outputs" can do so against
  observation, not against schema validation outcome.

- **NewTypeName constructors.** `NewFakeBroker`, `NewFakeKVStore`,
  `NewFakeLLMProvider`, `NewFakeDM`, `NewTestRig` — per
  `feedback_constructors.md`. No stuttering `New + package` form.

- **Goroutine-safe, race-clean.** Every fake protects internal state
  with `sync.Mutex` and operates on snapshot/copy values where
  applicable. Concurrent-access tests cover broker (16×32 publish +
  listener), kvstore (32-goroutine SetNX fairness), llm (16×32
  Complete), exercised under `-race`.

- **PublishedMessage payload is COPIED.** A mutation of the
  caller's `[]byte` after Publish/Inject MUST NOT affect the
  recorded payload — TestPublish_RecordsAndReturnsCopy pins this.

- **FakeDelivery enforces "exactly one terminate".** The second
  Ack/Nack/Reject returns `ErrAlreadyTerminated` so a handler bug
  surfaces in tests. Matches the real broker's "handler OWNS the
  delivery lifecycle" contract (`broker/subscribe.go:34`).

- **FakeDM publishes responses via Inject, not via the broker
  Publisher.** The production DM sits on a different (DM-owned)
  broker connection; in tests we collapse the two onto one
  FakeBroker. Inject routes the DM-side response into LIC's
  consumer queue, the same code path the real consumer adapter
  takes for an inbound delivery. (Tests asserting "DM responded"
  can still find the message in `fb.PublishedOn(dm.responses.*)`
  because Inject records.)

- **TestRig cleanup.** `NewTestRig(t)` registers `t.Cleanup`:
  FakeDM.Stop (drains in-flight response goroutines), FakeBroker.Close,
  FakeKVStore.Close. The test doesn't need defer; abnormal exits via
  t.Fatal still trigger the cleanup chain.

## Sentinel adapter (forward note for LIC-TASK-049+)

`fakes.ErrKeyNotFound` is **not** the same value as
`kvstore.ErrKeyNotFound`: they have identical messages but distinct
sentinel identities. Consumers that branch on `errors.Is(err,
kvstore.ErrKeyNotFound)` (e.g.
`pendingconfirmation.PendingStatePort` callers) would FAIL against the
fake. There are two options:

1. The consumer test wraps FakeKVStore's Get in a small adapter that
   translates the fake sentinel to the kvstore sentinel at the call
   boundary. This is the recommended pattern — keeps the fake
   hermetic.
2. The consumer test asserts on the message string, not the sentinel.
   Less ergonomic but viable.

Recorded so 049+ picks a pattern without re-deriving the trade-off.

## BuildTestApp deferral (RECORDED)

Acceptance criteria mention "Helper BuildTestApp(t): wires fakes в App".
The `app.New()` constructor in `internal/app/app.go` is **983 lines**
of concrete wiring that consumes `*kvstore.Client` AND `*broker.Client`
directly for two collaborators that do NOT have a seam yet:

- `pendingStateStore` (app/pending_state.go) constructs over
  `*kvstore.Client`.
- `versionMetaCache` (app/version_meta_cache.go) — same.

Replacing these requires either:

- (a) Refactor `app.New` into factory-injected form so the test rig
  can inject `*FakeKVStore` where the production code passes
  `*kvstore.Client`. This is its own LIC-TASK and was deemed
  out-of-scope for 048.
- (b) Build a parallel orchestrator-only wiring that bypasses
  `pendingStateStore` and `versionMetaCache` when the pipeline path
  under test does not exercise pending-state / version-meta cache.

Neither is in 048's scope; both are correctly the consumer task's
concern (049 / 050 / 051 / 054). 048 ships **TestRig** — a lightweight
"bundle the four fakes" — instead of a full app wiring. The four
fakes ARE the substitutes the consumer task plugs into the production
orchestrator / router / agents / awaiters; the app-scope wiring is
the consumer task's choice of strategy.

## Hermeticity

Test-support package, not production code. Allowed imports:
`stdlib + internal/domain/{model,port} + internal/infra/broker (types
only) + internal/llm/ratelimit (LuaEvaluator typed-assert)`. Forbidden
in production source (this package): RabbitMQ (amqp091), Redis
(go-redis, miniredis), Prometheus, OTel, `internal/config`,
`internal/infra/kvstore` (would force the production client into the
transitive import graph), every `internal/application/*` (the rig is
NOT a parallel orchestrator), every `internal/agents/*` (the rig
ships canned agent responses, not agent runtime), every
`internal/ingress/*` / `internal/egress/*` (the seams already inverted
these and the rig satisfies them externally). Verified at compile
time by the import set in each .go file.

The seams_check_test.go file is the ONLY place imports of
`internal/ingress/consumer`, `internal/ingress/idempotency`,
`internal/egress/dlq`, `internal/egress/publisher/dm`,
`internal/egress/publisher/orch` appear in this package — `_test.go`
scope, satisfaction assertions only, does NOT affect production
hermeticity.

## Forward notes (recorded, owners elsewhere)

1. **LIC-TASK-049 (happy path INITIAL pipeline).** Will compose the
   real orchestrator, idempotency Guard, dmawaiter and publishers
   over a `NewTestRig(t)`. The full sequence: publish
   VersionProcessingArtifactsReady → orchestrator routes →
   GetArtifactsRequest published → FakeDM responds → agents run via
   FakeLLMProvider → analysis-ready published → FakeDM responds
   Persisted. Use `InstallCannedAgentResponses` with
   `model.AllAgentIDs()[:8]` (skip Risk Delta).

2. **LIC-TASK-050 (low-confidence pause + resume).** Use
   `ClassifierLowConfidenceResponse` from fixtures.go for Agent 1;
   assert `RoutingKeyClassificationUncertain` published; inject
   `UserConfirmedType` via `fb.Inject(RoutingKeyUserConfirmedType,
   nil, ...)`; assert pipeline resumes.

3. **LIC-TASK-051 (RE_CHECK / agent failure / provider fallback /
   timeout / DLQ / prompt injection).** Use `ReCheckArtifactsBundle`,
   `FakeLLMProvider.InjectError` (+ second provider as fallback
   target), `FakeDM.SetResponseDelay` to drive awaiter timeouts,
   `FakeBroker.InjectPublishError` to drive publish-failure path.

4. **LIC-TASK-054 (tenant isolation).** Use distinct
   `organization_id` per InitialInject; assert all downstream
   publishes carry the matching org id via `AssertPayloadField`.

5. **Sentinel adapter — see "Sentinel adapter" above.** 049+ picks
   wrapping vs message-compare.

6. **No `go.mod` change.** All imports already in the module
   dependency graph.
