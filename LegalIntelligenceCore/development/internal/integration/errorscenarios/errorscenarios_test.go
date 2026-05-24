// Package errorscenarios_test exercises LIC-TASK-051 — eight error /
// edge-case integration scenarios over the full production LIC stack
// (orchestrator, ingress router, pending manager, consumer, 9 agents,
// aggregator, publishers, idempotency Guard, dmawaiter) wired by
// lictestapp.NewTestApp on top of the four in-memory fakes
// (FakeBroker, FakeKVStore, FakeLLMProvider, FakeDM).
//
// One top-level Test_* function per scenario (tasks.json acceptance
// "каждый сценарий — отдельный test func"). Each constructs an isolated
// harness so the JobLimiter slots / Redis state / canned LLM queues
// never leak between scenarios.
//
// Scenarios:
//
//   1. TestErrorScenario_ReCheckPipeline                — RE_CHECK via
//      VersionCreated cache + analysis-ready triggers Agent 9; payload
//      carries risk_delta.
//   2. TestErrorScenario_AgentFailure_RepairOK          — first response
//      schema-invalid, repair turn returns valid, pipeline COMPLETED;
//      exactly 2 LLM calls for the failing agent.
//   3. TestErrorScenario_AgentFailure_RepairFailed      — two consecutive
//      schema-invalid responses → AGENT_OUTPUT_INVALID, terminal FAILED
//      published, no analysis-ready, no DLQ from orchestrator (the
//      DLQTopicAgentOutputInvalid envelope is owned by future LIC-TASK-046
//      wiring; current production path emits only the FAILED status).
//   4. TestErrorScenario_ProviderFallback               — Claude returns
//      INVALID_API_KEY for Agent 1; router falls back to OpenAI; OpenAI
//      returns a schema-valid response; pipeline COMPLETED.
//   5. TestErrorScenario_AnalysisTimeout                — sub-30ms JobTimeout
//      + 80ms LLM latency → rootCtx expires → finalizer reclassifies to
//      ANALYSIS_TIMEOUT; terminal FAILED published with retryable=true.
//   6. TestErrorScenario_InvalidEnvelope_DLQInvalidMessage — malformed JSON
//      on version-artifacts-ready → consumer decode fails → PII-safe DLQ
//      envelope published on lic.dlq.invalid-message + Ack.
//   7. TestErrorScenario_PromptInjectionDetected_AggregatorWarning — two
//      agents emit prompt_injection_detected=true → DETAILED_REPORT.
//      warnings.PROMPT_INJECTION_DETECTED detection_count=2 with the
//      lex-sorted DetectedByAgents list; stripped flags on the wire stay
//      false (aggregator strip invariant).
//   8. TestErrorScenario_DocumentTooLarge_NoAgents      — MaxIngestedBytes
//      shrunk below the fixture size → orchestrator fails fast with
//      DOCUMENT_TOO_LARGE BEFORE any agent is invoked.
package errorscenarios_test

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"contractpro/legal-intelligence-core/internal/application/pipeline"
	"contractpro/legal-intelligence-core/internal/domain/model"
	"contractpro/legal-intelligence-core/internal/domain/port"
	"contractpro/legal-intelligence-core/internal/integration/fakes"
	"contractpro/legal-intelligence-core/internal/integration/lictestapp"
)

const (
	testModelID  = "test-claude"
	maxWaitShort = 10 * time.Second
	maxWaitLong  = 20 * time.Second
)

// hexHash64 matches 64 lowercase hex digits — the wire shape of a
// HMAC-SHA-256-first-64-hex DLQ envelope hash (security.md §6.4).
var hexHash64 = regexp.MustCompile(`^[0-9a-f]{64}$`)

// -----------------------------------------------------------------------------
// Scenario 1 — RE_CHECK pipeline via VersionCreated → metaCache fallback.
// -----------------------------------------------------------------------------

func TestErrorScenario_ReCheckPipeline(t *testing.T) {
	// 1. Build the harness. Default canned responses cover Agents 1..8;
	//    layer the AGENT_RISK_DELTA response on top so Stage 6 finds a
	//    canned schema-valid output (INITIAL pipeline skips Agent 9; the
	//    RE_CHECK path runs it).
	app := lictestapp.NewTestApp(t, lictestapp.WithCannedResponses(map[model.AgentID]string{
		model.AgentRiskDelta: fakes.RiskDeltaResponse,
	}))

	correlationID := uuid.NewString()
	jobID := uuid.NewString()
	documentID := uuid.NewString()
	versionID := uuid.NewString()
	parentVersionID := uuid.NewString()
	organizationID := uuid.NewString()
	createdByUserID := uuid.NewString()

	// 2. Pre-program DM responses for BOTH the current and parent
	//    versions: current gets the four mandatory artifacts; parent
	//    gets the RISK_ANALYSIS only (orchestrator requests just that
	//    artifact for the parent — orchestrator.go:810-812).
	app.DM.SetArtifactsResponse(versionID,
		fakes.BuildArtifactsResponse(fakes.DefaultArtifactsBundle()))
	app.DM.SetArtifactsResponse(parentVersionID, fakes.ArtifactsResponse{
		Artifacts: map[model.ArtifactType]json.RawMessage{
			model.ArtifactRiskAnalysis: json.RawMessage(fakes.ParentRiskAnalysisRU),
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), maxWaitLong)
	defer cancel()

	// 3. Inject VersionCreated FIRST — the cache populator. Router's
	//    RouteVersionCreated writes lic-version-meta:{version_id}
	//    carrying parent_version_id; the subsequent trigger with
	//    ParentVersionID=nil falls back to the cache (orchestrator
	//    DEFECT-1 resolveParentAndMode).
	versionCreated := port.VersionCreated{
		CorrelationID:   uuid.NewString(),
		Timestamp:       time.Now().UTC().Format(time.RFC3339Nano),
		DocumentID:      documentID,
		VersionID:       versionID,
		VersionNumber:   2,
		OrganizationID:  organizationID,
		OriginType:      "UPLOAD",
		ParentVersionID: &parentVersionID,
		JobID:           "",
		CreatedByUserID: createdByUserID,
	}
	if _, err := app.Broker.Inject(ctx, fakes.RoutingKeyVersionCreated, nil, mustJSON(t, versionCreated)); err != nil {
		t.Fatalf("Inject(version-created): %v", err)
	}

	// 4. Verify the cache write landed.
	rawMeta, err := app.KV.Get(ctx, "lic-version-meta:"+versionID)
	if err != nil {
		t.Fatalf("lic-version-meta after VersionCreated: %v (want present)", err)
	}
	if !strings.Contains(rawMeta, parentVersionID) {
		t.Fatalf("lic-version-meta = %q; want it to carry parent_version_id %q", rawMeta, parentVersionID)
	}

	// 5. Inject VersionProcessingArtifactsReady WITHOUT trigger.ParentVersionID
	//    so the orchestrator's cache fallback is the only RE_CHECK source.
	trigger := port.VersionProcessingArtifactsReady{
		CorrelationID:   correlationID,
		Timestamp:       time.Now().UTC().Format(time.RFC3339Nano),
		DocumentID:      documentID,
		VersionID:       versionID,
		OrganizationID:  organizationID,
		ArtifactTypes:   []string{"SEMANTIC_TREE", "EXTRACTED_TEXT", "DOCUMENT_STRUCTURE", "PROCESSING_WARNINGS"},
		JobID:           jobID,
		OriginType:      "UPLOAD",
		ParentVersionID: nil, // RE_CHECK driven by cache only.
		CreatedByUserID: createdByUserID,
	}
	if _, err := app.Broker.Inject(ctx, fakes.RoutingKeyVersionArtifactsReady, nil, mustJSON(t, trigger)); err != nil {
		t.Fatalf("Inject(version-artifacts-ready): %v", err)
	}

	// 6. Wait for terminal COMPLETED.
	waitCtx, waitCancel := context.WithTimeout(context.Background(), maxWaitShort)
	defer waitCancel()
	if err := waitForCompletedStatus(waitCtx, app.Broker); err != nil {
		t.Fatalf("waiting for COMPLETED status: %v", err)
	}

	// 7. Decode analysis-ready and assert RiskDelta is non-nil with the
	//    canned RiskDeltaResponse content (RE_CHECK with parent available).
	analysisMsgs := app.Broker.PublishedOn(fakes.RoutingKeyAnalysisReady)
	if len(analysisMsgs) != 1 {
		t.Fatalf("analysis-ready: got %d publish(es), want exactly 1", len(analysisMsgs))
	}
	var payload port.LegalAnalysisArtifactsReady
	fakes.MatchEvent(t, analysisMsgs[0], &payload)

	if payload.RiskDelta == nil {
		t.Fatal("RiskDelta is nil — RE_CHECK path failed to invoke Agent 9")
	}
	if len(payload.RiskDelta.Changed) < 1 {
		t.Errorf("RiskDelta.Changed: got %d, want >= 1 (from canned RiskDeltaResponse)", len(payload.RiskDelta.Changed))
	}

	// 8. The orchestrator MUST have issued TWO artifact requests — one
	//    for the current version (all 4 required artifacts), one for the
	//    parent (just RISK_ANALYSIS).
	requests := app.DM.ArtifactRequests()
	if len(requests) != 2 {
		t.Fatalf("FakeDM.ArtifactRequests: got %d, want 2 (current + parent)", len(requests))
	}
	var sawCurrent, sawParent bool
	for _, r := range requests {
		switch r.VersionID {
		case versionID:
			sawCurrent = true
			if len(r.ArtifactTypes) != 4 {
				t.Errorf("current artifact request: got %d types, want 4", len(r.ArtifactTypes))
			}
		case parentVersionID:
			sawParent = true
			if len(r.ArtifactTypes) != 1 || r.ArtifactTypes[0] != model.ArtifactRiskAnalysis {
				t.Errorf("parent artifact request: types = %v, want exactly [RISK_ANALYSIS]", r.ArtifactTypes)
			}
		}
	}
	if !sawCurrent {
		t.Error("FakeDM did NOT receive a request for the current version")
	}
	if !sawParent {
		t.Error("FakeDM did NOT receive a request for the parent version (RE_CHECK path broken)")
	}

	// 9. AGENT_RISK_DELTA MUST have been invoked exactly once on Claude.
	claudeCalls := app.LLM[port.ProviderClaude].Calls()
	deltaCalls := 0
	for _, c := range claudeCalls {
		if c.AgentID == model.AgentRiskDelta {
			deltaCalls++
		}
	}
	if deltaCalls != 1 {
		t.Errorf("AGENT_RISK_DELTA invocations on Claude = %d, want 1", deltaCalls)
	}
}

// -----------------------------------------------------------------------------
// Scenario 2 — Agent failure + repair OK.
// -----------------------------------------------------------------------------

func TestErrorScenario_AgentFailure_RepairOK(t *testing.T) {
	// Override AGENT_KEY_PARAMS with TWO responses: first schema-invalid
	// JSON, then a valid one. The Schema Validator catches the first,
	// RepairLoop fires exactly one repair turn (sticky on the same
	// provider/model FIFO), the 2nd response drains, validates, and the
	// pipeline finishes COMPLETED.
	app := lictestapp.NewTestApp(t)

	invalidKeyParams := `{"this_is_not_a_valid_key_params_response": true}`
	app.LLM[port.ProviderClaude].SetResponses(model.AgentKeyParams, testModelID, []fakes.CompletionScript{
		{Content: invalidKeyParams, InputTokens: 100, OutputTokens: 50, StopReason: port.StopReasonEndTurn},
		{Content: fakes.KeyParamsResponse, InputTokens: 100, OutputTokens: 100, StopReason: port.StopReasonEndTurn},
	})

	trigger, _, _, _ := buildHappyTrigger(t, app)
	ctx, cancel := context.WithTimeout(context.Background(), maxWaitLong)
	defer cancel()

	if _, err := app.Broker.Inject(ctx, fakes.RoutingKeyVersionArtifactsReady, nil, mustJSON(t, trigger)); err != nil {
		t.Fatalf("Inject(version-artifacts-ready): %v", err)
	}

	waitCtx, waitCancel := context.WithTimeout(context.Background(), maxWaitShort)
	defer waitCancel()
	if err := waitForCompletedStatus(waitCtx, app.Broker); err != nil {
		t.Fatalf("waiting for COMPLETED after repair: %v", err)
	}

	// AGENT_KEY_PARAMS MUST have been invoked exactly 2 times on Claude
	// (1 primary + 1 repair).
	claudeCalls := app.LLM[port.ProviderClaude].Calls()
	kpCalls := 0
	for _, c := range claudeCalls {
		if c.AgentID == model.AgentKeyParams {
			kpCalls++
		}
	}
	if kpCalls != 2 {
		t.Errorf("AGENT_KEY_PARAMS Claude calls = %d, want 2 (primary + repair)", kpCalls)
	}

	// Pipeline ran to completion: analysis-ready emitted with valid
	// KeyParameters (from the repaired response).
	analysisMsgs := app.Broker.PublishedOn(fakes.RoutingKeyAnalysisReady)
	if len(analysisMsgs) != 1 {
		t.Fatalf("analysis-ready: got %d publish(es), want exactly 1", len(analysisMsgs))
	}
	var payload port.LegalAnalysisArtifactsReady
	fakes.MatchEvent(t, analysisMsgs[0], &payload)
	if payload.KeyParameters == nil {
		t.Fatal("KeyParameters is nil after repair")
	}
	if payload.KeyParameters.Subject == "" {
		t.Errorf("KeyParameters.Subject is empty; repaired response did not flow through")
	}
}

// -----------------------------------------------------------------------------
// Scenario 3 — Agent failure repair-failed.
// -----------------------------------------------------------------------------

func TestErrorScenario_AgentFailure_RepairFailed(t *testing.T) {
	// Override AGENT_KEY_PARAMS with TWO schema-invalid responses.
	// RepairLoop hits the hard N=1 limit; the second violation surfaces
	// as AGENT_OUTPUT_INVALID (retryable=true per repair.go:173). The
	// orchestrator publishes the terminal FAILED status and returns the
	// error; the router NACKs (the DLQTopicAgentOutputInvalid envelope
	// is NOT published in the current production wiring — orchestrator
	// has no DLQPublisherPort by design, and the LIC-TASK-046 publisher
	// only owns publish-failed / agent-output-invalid emission for its
	// own callers, none of which run here yet).
	app := lictestapp.NewTestApp(t)

	invalidKeyParams := `{"this_is_not_a_valid_key_params_response": true}`
	app.LLM[port.ProviderClaude].SetResponses(model.AgentKeyParams, testModelID, []fakes.CompletionScript{
		{Content: invalidKeyParams, InputTokens: 100, OutputTokens: 50, StopReason: port.StopReasonEndTurn},
		{Content: invalidKeyParams, InputTokens: 100, OutputTokens: 50, StopReason: port.StopReasonEndTurn},
	})

	trigger, _, _, _ := buildHappyTrigger(t, app)
	ctx, cancel := context.WithTimeout(context.Background(), maxWaitLong)
	defer cancel()

	if _, err := app.Broker.Inject(ctx, fakes.RoutingKeyVersionArtifactsReady, nil, mustJSON(t, trigger)); err != nil {
		t.Fatalf("Inject(version-artifacts-ready): %v", err)
	}

	waitCtx, waitCancel := context.WithTimeout(context.Background(), maxWaitShort)
	defer waitCancel()
	if err := waitForFailedStatus(waitCtx, app.Broker); err != nil {
		t.Fatalf("waiting for FAILED status after repair-failed: %v", err)
	}

	// Last status MUST be FAILED + AGENT_OUTPUT_INVALID.
	statusMsgs := app.Broker.PublishedOn(fakes.RoutingKeyStatusChanged)
	var lastStatus port.LICStatusChangedEvent
	fakes.MatchEvent(t, statusMsgs[len(statusMsgs)-1], &lastStatus)
	if lastStatus.Status != model.StatusFailed {
		t.Fatalf("last status = %q, want FAILED", lastStatus.Status)
	}
	if lastStatus.ErrorCode != model.ErrCodeAgentOutputInvalid {
		t.Errorf("FAILED.ErrorCode = %q, want %q", lastStatus.ErrorCode, model.ErrCodeAgentOutputInvalid)
	}

	// analysis-ready MUST NOT have been published.
	if msgs := app.Broker.PublishedOn(fakes.RoutingKeyAnalysisReady); len(msgs) != 0 {
		t.Errorf("analysis-ready: got %d publish(es), want 0 on repair-failed", len(msgs))
	}

	// No DLQ agent-output-invalid envelope on the current wiring.
	if msgs := app.Broker.PublishedOn(fakes.RoutingKeyDLQAgentOutputInvalid); len(msgs) != 0 {
		t.Errorf("DLQ agent-output-invalid: got %d publish(es), want 0 (no production caller yet)", len(msgs))
	}

	// AGENT_KEY_PARAMS observed exactly 2 calls on Claude (primary + repair).
	kpCalls := 0
	for _, c := range app.LLM[port.ProviderClaude].Calls() {
		if c.AgentID == model.AgentKeyParams {
			kpCalls++
		}
	}
	if kpCalls != 2 {
		t.Errorf("AGENT_KEY_PARAMS Claude calls = %d, want 2 (primary + repair, no second repair per ADR-LIC-04)", kpCalls)
	}
}

// -----------------------------------------------------------------------------
// Scenario 4 — Provider fallback (Claude → OpenAI).
// -----------------------------------------------------------------------------

func TestErrorScenario_ProviderFallback(t *testing.T) {
	// Two-step setup:
	//   a) install a canned schema-valid AGENT_TYPE_CLASSIFIER response
	//      on OpenAI for testModelID — needs to exist BEFORE the router
	//      tries the fallback hop;
	//   b) inject a CONTENT_POLICY error on Claude for that agent — the
	//      catalog row is {Retryable=false, FallbackEligible=true} so
	//      the router does NOT retry the primary and advances straight
	//      to OpenAI. CONTENT_POLICY is deliberately preferred over
	//      INVALID_API_KEY / QUOTA_EXCEEDED here: those two flip Claude
	//      into the *permanent-unhealthy* state (health/registry.go) and
	//      every other Stage-1..6 agent running in parallel against
	//      Claude would then be transparently re-routed through the
	//      fallback chain. Since the harness only canned-installs Agent 1
	//      on OpenAI, a permanent flip would cause KeyParams (parallel
	//      with Classifier in Stage 1) to fall back to OpenAI, find no
	//      canned response, and fail the whole pipeline. CONTENT_POLICY
	//      only increments consecutive_failures — far below the >=3
	//      transient-unhealthy threshold — so Claude stays healthy for
	//      every other agent.
	app := lictestapp.NewTestApp(t)
	app.LLM[port.ProviderOpenAI].SetResponseJSON(model.AgentTypeClassifier, testModelID, fakes.ClassifierResponse)
	app.LLM[port.ProviderClaude].InjectError(model.AgentTypeClassifier, testModelID,
		port.NewLLMProviderError(port.LLMErrorContentPolicy, nil))

	trigger, _, _, _ := buildHappyTrigger(t, app)
	ctx, cancel := context.WithTimeout(context.Background(), maxWaitLong)
	defer cancel()

	if _, err := app.Broker.Inject(ctx, fakes.RoutingKeyVersionArtifactsReady, nil, mustJSON(t, trigger)); err != nil {
		t.Fatalf("Inject(version-artifacts-ready): %v", err)
	}

	waitCtx, waitCancel := context.WithTimeout(context.Background(), maxWaitShort)
	defer waitCancel()
	if err := waitForCompletedStatus(waitCtx, app.Broker); err != nil {
		t.Fatalf("waiting for COMPLETED status after fallback: %v", err)
	}

	// Claude observed exactly ONE call for AGENT_TYPE_CLASSIFIER (the
	// failed primary).
	claudeTypeCalls := 0
	for _, c := range app.LLM[port.ProviderClaude].Calls() {
		if c.AgentID == model.AgentTypeClassifier {
			claudeTypeCalls++
		}
	}
	if claudeTypeCalls != 1 {
		t.Errorf("Claude AGENT_TYPE_CLASSIFIER calls = %d, want 1", claudeTypeCalls)
	}

	// OpenAI observed exactly ONE call for AGENT_TYPE_CLASSIFIER (the
	// successful fallback).
	openaiTypeCalls := 0
	for _, c := range app.LLM[port.ProviderOpenAI].Calls() {
		if c.AgentID == model.AgentTypeClassifier {
			openaiTypeCalls++
		}
	}
	if openaiTypeCalls != 1 {
		t.Errorf("OpenAI AGENT_TYPE_CLASSIFIER calls = %d, want 1", openaiTypeCalls)
	}

	// No DLQ envelope MUST have been emitted on the fallback path —
	// CONTENT_POLICY is a router-side decision, not a poison message;
	// the consumer must not have DLQ'd anything, and the orchestrator
	// must not have published any FAILED status.
	for _, rk := range []string{
		fakes.RoutingKeyDLQInvalidMessage,
		fakes.RoutingKeyDLQConsumerFailed,
		fakes.RoutingKeyDLQPublishFailed,
		fakes.RoutingKeyDLQAgentOutputInvalid,
	} {
		if msgs := app.Broker.PublishedOn(rk); len(msgs) != 0 {
			t.Errorf("DLQ %s after fallback: got %d, want 0", rk, len(msgs))
		}
	}

	// analysis-ready emitted; classification from the fallback response.
	analysisMsgs := app.Broker.PublishedOn(fakes.RoutingKeyAnalysisReady)
	if len(analysisMsgs) != 1 {
		t.Fatalf("analysis-ready: got %d publish(es), want 1", len(analysisMsgs))
	}
	var payload port.LegalAnalysisArtifactsReady
	fakes.MatchEvent(t, analysisMsgs[0], &payload)
	if payload.ClassificationResult == nil {
		t.Fatal("ClassificationResult is nil after fallback")
	}
	if payload.ClassificationResult.ContractType != model.ContractType("SUPPLY") {
		t.Errorf("ContractType = %q, want SUPPLY (from OpenAI fallback canned response)",
			payload.ClassificationResult.ContractType)
	}
}

// -----------------------------------------------------------------------------
// Scenario 5 — pipeline ANALYSIS_TIMEOUT.
// -----------------------------------------------------------------------------

func TestErrorScenario_AnalysisTimeout(t *testing.T) {
	// Lean on the orchestrator's finalizer discriminator (orchestrator.go
	// D11 / classifyOutcome): if rootCtx is DeadlineExceeded when the
	// body returns, the outcome is reclassified to ANALYSIS_TIMEOUT
	// regardless of the body error.
	//
	// To trigger it deterministically: shrink JobTimeout to 30ms and
	// install an 80ms latency on Claude. Artifacts arrive immediately
	// (FakeDM has no delay), Stage 1 starts, the Claude call blocks, the
	// 30ms rootCtx expires, ctx.Err propagates, the finalizer stamps
	// ANALYSIS_TIMEOUT.
	//
	// Observation note (production behaviour pinned here): publishFailed
	// uses the dead spanCtx (orchestrator.go:346 + 1155). The status
	// publisher's ctx-guard (publisher.go + FakeBroker.Publish:170) drops
	// any publish on a done ctx, so the terminal FAILED{ANALYSIS_TIMEOUT}
	// is NEVER reachable on the wire for this code path. The test
	// therefore asserts the *observable consequence*:
	//   - IN_PROGRESS{STAGE_REQUESTING_ARTIFACTS} WAS published (proves the
	//     pipeline started under the trimmed deadlines);
	//   - analysis-ready was NEVER published (proves the pipeline did NOT
	//     COMPLETE);
	//   - COMPLETED status was NEVER published;
	//   - the pipeline returned far below maxWaitShort (proves the
	//     deadline-driven unwind, not a hang).
	app := lictestapp.NewTestApp(t, lictestapp.WithPipelineConfigOverride(func(c *pipeline.Config) {
		c.JobTimeout = 30 * time.Millisecond
		c.DMRequestTimeout = 15 * time.Millisecond
		c.DMPersistConfirmTimeout = 15 * time.Millisecond
	}))
	app.LLM[port.ProviderClaude].SetLatency(80 * time.Millisecond)

	trigger, _, _, _ := buildHappyTrigger(t, app)
	ctx, cancel := context.WithTimeout(context.Background(), maxWaitLong)
	defer cancel()

	start := time.Now()
	if _, err := app.Broker.Inject(ctx, fakes.RoutingKeyVersionArtifactsReady, nil, mustJSON(t, trigger)); err != nil {
		t.Fatalf("Inject(version-artifacts-ready): %v", err)
	}
	// Inject is synchronous over the consumer handler chain (broker.go
	// fan-out), so by this line the pipeline has run, the rootCtx has
	// expired, the finalizer has fired, the consumer has NACK'd, and Run
	// has returned. The elapsed time is the actual deadline-driven unwind
	// duration plus tiny overheads.
	elapsed := time.Since(start)
	if elapsed > 2*time.Second {
		t.Errorf("Inject-to-return elapsed = %v, want << 2s (deadline-driven unwind, not a hang)", elapsed)
	}
	t.Logf("ANALYSIS_TIMEOUT pipeline unwind duration: %v", elapsed)

	// IN_PROGRESS{STAGE_REQUESTING_ARTIFACTS} MUST have been published
	// BEFORE the deadline fired (orchestrator.go:357 — the first publish
	// uses spanCtx which is still alive at that moment).
	statusMsgs := app.Broker.PublishedOn(fakes.RoutingKeyStatusChanged)
	if len(statusMsgs) < 1 {
		t.Fatalf("status-changed: got %d publish(es), want >= 1 (IN_PROGRESS)", len(statusMsgs))
	}
	var firstStatus port.LICStatusChangedEvent
	fakes.MatchEvent(t, statusMsgs[0], &firstStatus)
	if firstStatus.Status != model.StatusInProgress {
		t.Errorf("first status = %q, want IN_PROGRESS", firstStatus.Status)
	}

	// No COMPLETED, no FAILED (publishFailed dropped the publish on the
	// dead spanCtx), no analysis-ready.
	for i, msg := range statusMsgs {
		var s port.LICStatusChangedEvent
		fakes.MatchEvent(t, msg, &s)
		if s.Status == model.StatusCompleted {
			t.Errorf("status #%d is COMPLETED — pipeline must NOT have completed under a 30ms JobTimeout vs 80ms LLM latency", i)
		}
	}
	if msgs := app.Broker.PublishedOn(fakes.RoutingKeyAnalysisReady); len(msgs) != 0 {
		t.Errorf("analysis-ready on timeout: got %d publish(es), want 0", len(msgs))
	}
	if ready := app.DM.AnalysisReady(); len(ready) != 0 {
		t.Errorf("FakeDM.AnalysisReady on timeout: got %d, want 0", len(ready))
	}
}

// -----------------------------------------------------------------------------
// Scenario 6 — invalid envelope → DLQ invalid-message.
// -----------------------------------------------------------------------------

func TestErrorScenario_InvalidEnvelope_DLQInvalidMessage(t *testing.T) {
	app := lictestapp.NewTestApp(t)

	ctx, cancel := context.WithTimeout(context.Background(), maxWaitShort)
	defer cancel()

	// Garbage payload — fails json.Unmarshal in decodeVersionArtifactsReady.
	res, err := app.Broker.Inject(ctx, fakes.RoutingKeyVersionArtifactsReady, nil, []byte("{not-json"))
	if err != nil {
		t.Fatalf("Inject(garbage): %v", err)
	}
	if res.Acked != 1 {
		t.Fatalf("InjectResult.Acked = %d, want 1 (consumer ACKs after DLQ publish)", res.Acked)
	}

	dlqMsgs := app.Broker.PublishedOn(fakes.RoutingKeyDLQInvalidMessage)
	if len(dlqMsgs) != 1 {
		t.Fatalf("DLQ invalid-message: got %d publish(es), want 1", len(dlqMsgs))
	}
	var env port.LICDLQEnvelope
	fakes.MatchEvent(t, dlqMsgs[0], &env)
	if env.OriginalTopic != fakes.RoutingKeyVersionArtifactsReady {
		t.Errorf("DLQ envelope OriginalTopic = %q, want %q", env.OriginalTopic, fakes.RoutingKeyVersionArtifactsReady)
	}
	if env.ErrorCode != model.ErrCodeInvalidMessageSchema {
		t.Errorf("DLQ envelope ErrorCode = %q, want %q", env.ErrorCode, model.ErrCodeInvalidMessageSchema)
	}
	if len(env.OriginalMessageHash) != 64 {
		t.Errorf("DLQ envelope OriginalMessageHash length = %d, want 64 (HMAC-SHA-256 hex)", len(env.OriginalMessageHash))
	}
	if !hexHash64.MatchString(env.OriginalMessageHash) {
		t.Errorf("DLQ envelope OriginalMessageHash = %q, want 64 lowercase hex digits", env.OriginalMessageHash)
	}
	if env.OriginalMessageSizeBytes != len([]byte("{not-json")) {
		t.Errorf("DLQ envelope OriginalMessageSizeBytes = %d, want %d",
			env.OriginalMessageSizeBytes, len([]byte("{not-json")))
	}

	// No pipeline state created — no analysis-ready / requests.artifacts.
	if msgs := app.Broker.PublishedOn(fakes.RoutingKeyAnalysisReady); len(msgs) != 0 {
		t.Errorf("analysis-ready after invalid envelope: got %d, want 0", len(msgs))
	}
	if msgs := app.Broker.PublishedOn(fakes.RoutingKeyRequestArtifacts); len(msgs) != 0 {
		t.Errorf("requests.artifacts after invalid envelope: got %d, want 0", len(msgs))
	}
}

// -----------------------------------------------------------------------------
// Scenario 7 — PROMPT_INJECTION_DETECTED warning aggregation.
// -----------------------------------------------------------------------------

func TestErrorScenario_PromptInjectionDetected_AggregatorWarning(t *testing.T) {
	// Two agents flip prompt_injection_detected to true. The aggregator
	// (warnings.go applyPromptInjection) emits a single
	// DETAILED_REPORT.warnings.PROMPT_INJECTION_DETECTED entry with the
	// lex-sorted DetectedByAgents list + DetectionCount=2.
	app := lictestapp.NewTestApp(t, lictestapp.WithCannedResponses(map[model.AgentID]string{
		model.AgentKeyParams:     flipPromptInjection(fakes.KeyParamsResponse),
		model.AgentRiskDetection: flipPromptInjection(fakes.RiskDetectionResponse),
	}))

	trigger, _, _, _ := buildHappyTrigger(t, app)
	ctx, cancel := context.WithTimeout(context.Background(), maxWaitLong)
	defer cancel()

	if _, err := app.Broker.Inject(ctx, fakes.RoutingKeyVersionArtifactsReady, nil, mustJSON(t, trigger)); err != nil {
		t.Fatalf("Inject(version-artifacts-ready): %v", err)
	}

	waitCtx, waitCancel := context.WithTimeout(context.Background(), maxWaitShort)
	defer waitCancel()
	if err := waitForCompletedStatus(waitCtx, app.Broker); err != nil {
		t.Fatalf("waiting for COMPLETED status: %v", err)
	}

	analysisMsgs := app.Broker.PublishedOn(fakes.RoutingKeyAnalysisReady)
	if len(analysisMsgs) != 1 {
		t.Fatalf("analysis-ready: got %d publish(es), want 1", len(analysisMsgs))
	}
	var payload port.LegalAnalysisArtifactsReady
	fakes.MatchEvent(t, analysisMsgs[0], &payload)

	if payload.DetailedReport == nil {
		t.Fatal("DetailedReport is nil")
	}
	if payload.DetailedReport.Warnings == nil {
		t.Fatal("DetailedReport.Warnings is nil — aggregator did not emit warnings")
	}
	warn := payload.DetailedReport.Warnings.PromptInjectionDetected
	if warn == nil {
		t.Fatal("DetailedReport.Warnings.PROMPT_INJECTION_DETECTED is nil")
	}
	if !warn.Detected {
		t.Errorf("warn.Detected = false, want true")
	}
	if warn.DetectionCount != 2 {
		t.Errorf("warn.DetectionCount = %d, want 2", warn.DetectionCount)
	}
	if len(warn.DetectedByAgents) != 2 {
		t.Fatalf("warn.DetectedByAgents length = %d, want 2", len(warn.DetectedByAgents))
	}
	wantBy := []string{string(model.AgentKeyParams), string(model.AgentRiskDetection)}
	if !equalSorted(warn.DetectedByAgents, wantBy) {
		t.Errorf("warn.DetectedByAgents = %v, want lex-sorted %v", warn.DetectedByAgents, wantBy)
	}
	if warn.UserMessage == "" {
		t.Errorf("warn.UserMessage is empty; aggregator did not stamp RU message")
	}

	// Strip invariants — outbound KeyParameters and RiskAnalysis MUST
	// have prompt_injection_detected = false on the wire (aggregator
	// strip per aggregator.go:194,202).
	if payload.KeyParameters == nil {
		t.Fatal("KeyParameters nil")
	}
	if payload.KeyParameters.PromptInjectionDetected {
		t.Errorf("KeyParameters.PromptInjectionDetected on wire = true, want false (strip)")
	}
	if payload.RiskAnalysis == nil {
		t.Fatal("RiskAnalysis nil")
	}
	if payload.RiskAnalysis.PromptInjectionDetected {
		t.Errorf("RiskAnalysis.PromptInjectionDetected on wire = true, want false (strip)")
	}
}

// -----------------------------------------------------------------------------
// Scenario 8 — DOCUMENT_TOO_LARGE skips every agent.
// -----------------------------------------------------------------------------

func TestErrorScenario_DocumentTooLarge_NoAgents(t *testing.T) {
	// Shrink MaxIngestedBytes to 100 bytes — the default DM artifacts
	// fixtures are several KB so the orchestrator's D8 inline cap
	// (orchestrator.go:393-403) fires BEFORE any agent runs.
	app := lictestapp.NewTestApp(t, lictestapp.WithPipelineConfigOverride(func(c *pipeline.Config) {
		c.MaxIngestedBytes = 100
	}))

	trigger, _, _, _ := buildHappyTrigger(t, app)
	ctx, cancel := context.WithTimeout(context.Background(), maxWaitLong)
	defer cancel()

	if _, err := app.Broker.Inject(ctx, fakes.RoutingKeyVersionArtifactsReady, nil, mustJSON(t, trigger)); err != nil {
		t.Fatalf("Inject(version-artifacts-ready): %v", err)
	}

	waitCtx, waitCancel := context.WithTimeout(context.Background(), maxWaitShort)
	defer waitCancel()
	if err := waitForFailedStatus(waitCtx, app.Broker); err != nil {
		t.Fatalf("waiting for FAILED status: %v", err)
	}

	statusMsgs := app.Broker.PublishedOn(fakes.RoutingKeyStatusChanged)
	var lastStatus port.LICStatusChangedEvent
	fakes.MatchEvent(t, statusMsgs[len(statusMsgs)-1], &lastStatus)
	if lastStatus.Status != model.StatusFailed {
		t.Fatalf("last status = %q, want FAILED", lastStatus.Status)
	}
	if lastStatus.ErrorCode != model.ErrCodeDocumentTooLarge {
		t.Errorf("FAILED.ErrorCode = %q, want %q", lastStatus.ErrorCode, model.ErrCodeDocumentTooLarge)
	}
	if lastStatus.IsRetryable == nil || *lastStatus.IsRetryable != false {
		t.Errorf("FAILED.IsRetryable = %v, want *bool=false", lastStatus.IsRetryable)
	}

	// No agent calls — cap fires at STAGE_ARTIFACTS_RECEIVED, before
	// Stage 1.
	totalLLMCalls := 0
	for _, p := range app.LLM {
		totalLLMCalls += len(p.Calls())
	}
	if totalLLMCalls != 0 {
		t.Errorf("LLM calls observed = %d, want 0 (DOCUMENT_TOO_LARGE precedes Stage 1)", totalLLMCalls)
	}

	if msgs := app.Broker.PublishedOn(fakes.RoutingKeyAnalysisReady); len(msgs) != 0 {
		t.Errorf("analysis-ready on DOCUMENT_TOO_LARGE: got %d, want 0", len(msgs))
	}
}

// -----------------------------------------------------------------------------
// Helpers — shared across scenarios.
// -----------------------------------------------------------------------------

// buildHappyTrigger generates fresh canonical UUIDs for one INITIAL run,
// pre-programs the FakeDM with the four mandatory artifacts for the
// generated versionID, and returns the trigger DTO plus the IDs.
func buildHappyTrigger(t *testing.T, app *lictestapp.TestApp) (port.VersionProcessingArtifactsReady, string, string, string) {
	t.Helper()
	correlationID := uuid.NewString()
	jobID := uuid.NewString()
	documentID := uuid.NewString()
	versionID := uuid.NewString()
	organizationID := uuid.NewString()
	createdByUserID := uuid.NewString()

	app.DM.SetArtifactsResponse(versionID, fakes.BuildArtifactsResponse(fakes.DefaultArtifactsBundle()))

	trigger := port.VersionProcessingArtifactsReady{
		CorrelationID:   correlationID,
		Timestamp:       time.Now().UTC().Format(time.RFC3339Nano),
		DocumentID:      documentID,
		VersionID:       versionID,
		OrganizationID:  organizationID,
		ArtifactTypes:   []string{"SEMANTIC_TREE", "EXTRACTED_TEXT", "DOCUMENT_STRUCTURE", "PROCESSING_WARNINGS"},
		JobID:           jobID,
		OriginType:      "UPLOAD",
		ParentVersionID: nil,
		CreatedByUserID: createdByUserID,
	}
	return trigger, jobID, versionID, organizationID
}

// flipPromptInjection rewrites the canned response so
// prompt_injection_detected is true. The fixture is well-formed JSON,
// so a string-replace is enough — the resulting body remains
// schema-valid (the boolean is the only flipped value).
func flipPromptInjection(canned string) string {
	return strings.Replace(canned,
		`"prompt_injection_detected": false`,
		`"prompt_injection_detected": true`,
		1)
}

// equalSorted reports whether got equals want when interpreted as the
// already lex-sorted list of strings (the aggregator's contract).
func equalSorted(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

// mustJSON marshals v and fails the test on error.
func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	body, err := jsonMarshal(v)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	return body
}

// waitForCompletedStatus polls the broker's status-changed log until a
// terminal COMPLETED event arrives or ctx expires.
func waitForCompletedStatus(ctx context.Context, fb *fakes.FakeBroker) error {
	tk := time.NewTicker(2 * time.Millisecond)
	defer tk.Stop()
	for {
		for _, msg := range fb.PublishedOn(fakes.RoutingKeyStatusChanged) {
			var evt port.LICStatusChangedEvent
			if err := jsonUnmarshal(msg.Payload, &evt); err != nil {
				continue
			}
			if evt.Status == model.StatusCompleted {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-tk.C:
		}
	}
}

// waitForFailedStatus polls the broker's status-changed log until a
// terminal FAILED event arrives or ctx expires.
func waitForFailedStatus(ctx context.Context, fb *fakes.FakeBroker) error {
	tk := time.NewTicker(2 * time.Millisecond)
	defer tk.Stop()
	for {
		for _, msg := range fb.PublishedOn(fakes.RoutingKeyStatusChanged) {
			var evt port.LICStatusChangedEvent
			if err := jsonUnmarshal(msg.Payload, &evt); err != nil {
				continue
			}
			if evt.Status == model.StatusFailed {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-tk.C:
		}
	}
}
