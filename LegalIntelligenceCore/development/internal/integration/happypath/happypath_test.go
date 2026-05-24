// Package happypath_test exercises LIC-TASK-049 — happy-path INITIAL
// pipeline integration test.
//
// The test composes the full production stack via lictestapp.NewTestApp
// (real orchestrator, ingress router, pending manager, consumer, 9
// agents, aggregator, publishers, idempotency Guard, dmawaiter) over
// the four in-memory fakes (FakeBroker, FakeKVStore, FakeLLMProvider,
// FakeDM), then:
//
//  1. injects a VersionProcessingArtifactsReady trigger on
//     dm.events.version-artifacts-ready with parent_version_id=nil
//     (INITIAL mode);
//  2. waits for the orchestrator to publish COMPLETED on
//     lic.events.status-changed;
//  3. asserts the publish order: IN_PROGRESS{STAGE_REQUESTING_ARTIFACTS}
//     → lic.requests.artifacts → lic.artifacts.analysis-ready →
//     COMPLETED{STAGE_DONE};
//  4. asserts the analysis-ready payload carries every artifact slot
//     non-nil and with the canned schema-valid content;
//  5. asserts the trigger delivery was Ack'd exactly once
//     (InjectResult.Acked == 1).
package happypath_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"contractpro/legal-intelligence-core/internal/domain/model"
	"contractpro/legal-intelligence-core/internal/domain/port"
	"contractpro/legal-intelligence-core/internal/integration/fakes"
	"contractpro/legal-intelligence-core/internal/integration/lictestapp"
)

// TestHappyPath_INITIAL_Pipeline drives one end-to-end INITIAL contract
// analysis through the real production code.
func TestHappyPath_INITIAL_Pipeline(t *testing.T) {
	// 1. Build the harness. Default options install canned schema-valid
	//    responses for AGENT_TYPE_CLASSIFIER..AGENT_DETAILED_REPORT;
	//    AGENT_RISK_DELTA stays unconfigured (INITIAL skips it).
	app := lictestapp.NewTestApp(t)

	// 2. Build a trigger with canonical UUIDs. parent_version_id stays
	//    nil — INITIAL mode.
	correlationID := uuid.NewString()
	jobID := uuid.NewString()
	documentID := uuid.NewString()
	versionID := uuid.NewString()
	organizationID := uuid.NewString()
	createdByUserID := uuid.NewString()

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

	// 3. Pre-program the FakeDM response for THIS version: the 4
	//    mandatory artifacts via fakes.DefaultArtifactsBundle.
	app.DM.SetArtifactsResponse(versionID, fakes.BuildArtifactsResponse(fakes.DefaultArtifactsBundle()))
	// Persist outcome: default = success.

	// 4. Inject the trigger. The FakeBroker's Inject is synchronous over
	//    the handler chain but the FakeDM responds via a goroutine, so we
	//    still need WaitForPublish to observe the downstream events.
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	start := time.Now()
	body := mustJSON(t, trigger)
	res, err := app.Broker.Inject(ctx, fakes.RoutingKeyVersionArtifactsReady, nil, body)
	if err != nil {
		t.Fatalf("Inject(version-artifacts-ready): %v", err)
	}

	// 5. Wait for the terminal COMPLETED event on status-changed (the
	//    last publish in the chain) before asserting the publish log.
	waitCtx, waitCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer waitCancel()
	if err := waitForCompletedStatus(waitCtx, app.Broker); err != nil {
		t.Fatalf("waiting for COMPLETED status: %v", err)
	}

	elapsed := time.Since(start)
	t.Logf("happy-path INITIAL pipeline duration: %v", elapsed)

	// 6. Inject ACK assertion — the trigger delivery was Ack'd exactly
	//    once (success → consumer.handle returns nil → handle Acks).
	if res.Acked != 1 {
		t.Fatalf("InjectResult.Acked = %d, want 1 (Nacked=%d)", res.Acked, res.Nacked)
	}

	// 7. Outbound publishes — assert presence + order.
	statusMsgs := app.Broker.PublishedOn(fakes.RoutingKeyStatusChanged)
	if len(statusMsgs) < 2 {
		t.Fatalf("status-changed: got %d publish(es), want >= 2", len(statusMsgs))
	}

	analysisMsgs := app.Broker.PublishedOn(fakes.RoutingKeyAnalysisReady)
	if len(analysisMsgs) != 1 {
		t.Fatalf("analysis-ready: got %d publish(es), want exactly 1", len(analysisMsgs))
	}

	requestMsgs := app.Broker.PublishedOn(fakes.RoutingKeyRequestArtifacts)
	if len(requestMsgs) != 1 {
		t.Fatalf("requests.artifacts: got %d publish(es), want exactly 1", len(requestMsgs))
	}

	// 8. Order assertion. The orchestrator publishes
	//    IN_PROGRESS{STAGE_REQUESTING_ARTIFACTS} before the artifact
	//    request, and COMPLETED{STAGE_DONE} after the analysis-ready
	//    publish + persist confirmation.
	//
	// Inspect the first two status-changed events.
	var firstStatus port.LICStatusChangedEvent
	fakes.MatchEvent(t, statusMsgs[0], &firstStatus)
	if firstStatus.Status != model.StatusInProgress {
		t.Fatalf("first status-changed: status = %q, want IN_PROGRESS", firstStatus.Status)
	}
	if firstStatus.Stage != model.StageRequestingArtifacts {
		t.Fatalf("first status-changed: stage = %q, want STAGE_REQUESTING_ARTIFACTS", firstStatus.Stage)
	}

	// The last status-changed event must be the terminal COMPLETED.
	// Per LICStatusChangedEvent (events.go:206-208) Stage is omitempty
	// on the wire and the orchestrator deliberately leaves it zero for
	// COMPLETED (orchestrator.go:1119); only IN_PROGRESS / FAILED carry
	// a Stage. So we only assert Status here.
	lastStatus := statusMsgs[len(statusMsgs)-1]
	var lastStatusEvt port.LICStatusChangedEvent
	fakes.MatchEvent(t, lastStatus, &lastStatusEvt)
	if lastStatusEvt.Status != model.StatusCompleted {
		t.Fatalf("last status-changed: status = %q, want COMPLETED", lastStatusEvt.Status)
	}

	// Chronological order: IN_PROGRESS < requests.artifacts <
	// analysis-ready < COMPLETED.
	if !statusMsgs[0].At.Before(requestMsgs[0].At) {
		t.Errorf("expected IN_PROGRESS (%v) before requests.artifacts (%v)", statusMsgs[0].At, requestMsgs[0].At)
	}
	if !requestMsgs[0].At.Before(analysisMsgs[0].At) {
		t.Errorf("expected requests.artifacts (%v) before analysis-ready (%v)", requestMsgs[0].At, analysisMsgs[0].At)
	}
	if !analysisMsgs[0].At.Before(lastStatus.At) {
		t.Errorf("expected analysis-ready (%v) before COMPLETED (%v)", analysisMsgs[0].At, lastStatus.At)
	}

	// 9. Decode the analysis-ready payload and assert every artifact slot.
	var payload port.LegalAnalysisArtifactsReady
	fakes.MatchEvent(t, analysisMsgs[0], &payload)

	if payload.CorrelationID != correlationID {
		t.Errorf("payload.CorrelationID = %q, want %q", payload.CorrelationID, correlationID)
	}
	if payload.JobID != jobID {
		t.Errorf("payload.JobID = %q, want %q", payload.JobID, jobID)
	}
	if payload.VersionID != versionID {
		t.Errorf("payload.VersionID = %q, want %q", payload.VersionID, versionID)
	}

	// Agent 1 — Classification.
	if payload.ClassificationResult == nil {
		t.Fatal("ClassificationResult is nil")
	}
	if payload.ClassificationResult.ContractType != model.ContractType("SUPPLY") {
		t.Errorf("ClassificationResult.ContractType = %q, want SUPPLY", payload.ClassificationResult.ContractType)
	}
	if payload.ClassificationResult.Confidence < 0.9 {
		t.Errorf("ClassificationResult.Confidence = %v, want >= 0.9", payload.ClassificationResult.Confidence)
	}

	// Agent 2 — KeyParameters. Stripped (internal_extras must be nil).
	if payload.KeyParameters == nil {
		t.Fatal("KeyParameters is nil")
	}
	if payload.KeyParameters.InternalExtras != nil {
		t.Errorf("KeyParameters.InternalExtras should be nil after aggregator strip, got %+v", payload.KeyParameters.InternalExtras)
	}
	if payload.KeyParameters.PromptInjectionDetected {
		t.Errorf("KeyParameters.PromptInjectionDetected should be false after strip")
	}

	// Agent 5 (merged) — MergedRiskAnalysis on the wire is RiskAnalysis.
	if payload.RiskAnalysis == nil {
		t.Fatal("RiskAnalysis (merged) is nil")
	}
	if len(payload.RiskAnalysis.Risks) < 1 {
		t.Errorf("RiskAnalysis.Risks: got %d, want >= 1", len(payload.RiskAnalysis.Risks))
	}

	// Derived — RiskProfile + AggregateScore.
	if payload.RiskProfile == nil {
		t.Fatal("RiskProfile is nil — aggregator failed to derive")
	}
	if payload.AggregateScore == nil {
		t.Fatal("AggregateScore is nil — aggregator failed to derive")
	}

	// Agent 6 — Recommendations.
	if len(payload.Recommendations) < 1 {
		t.Errorf("Recommendations: got %d, want >= 1", len(payload.Recommendations))
	}

	// Agent 7 — Summary.
	if payload.Summary == nil {
		t.Fatal("Summary is nil")
	}
	if payload.Summary.Text == "" {
		t.Errorf("Summary.Text empty")
	}

	// Agent 8 — DetailedReport.
	if payload.DetailedReport == nil {
		t.Fatal("DetailedReport is nil")
	}
	if len(payload.DetailedReport.Sections) < 1 {
		t.Errorf("DetailedReport.Sections: got %d, want >= 1", len(payload.DetailedReport.Sections))
	}

	// Agent 9 — RiskDelta MUST be omitted (INITIAL).
	if payload.RiskDelta != nil {
		t.Errorf("RiskDelta should be nil for INITIAL mode, got %+v", payload.RiskDelta)
	}

	// 10. FakeDM observed exactly ONE artifact request + ONE
	//     analysis-ready publish for this version.
	requests := app.DM.ArtifactRequests()
	if len(requests) != 1 {
		t.Fatalf("FakeDM.ArtifactRequests: got %d, want 1", len(requests))
	}
	if requests[0].VersionID != versionID {
		t.Errorf("DM artifact request: versionID = %q, want %q", requests[0].VersionID, versionID)
	}

	ready := app.DM.AnalysisReady()
	if len(ready) != 1 {
		t.Fatalf("FakeDM.AnalysisReady: got %d, want 1", len(ready))
	}
	if ready[0].VersionID != versionID {
		t.Errorf("DM analysis-ready: versionID = %q, want %q", ready[0].VersionID, versionID)
	}

	// 11. AGENT_RISK_DELTA must NOT have been invoked (no canned response
	//     installed). Inspect FakeLLMProvider call log.
	claudeCalls := app.LLM[port.ProviderClaude].Calls()
	for _, c := range claudeCalls {
		if c.AgentID == model.AgentRiskDelta {
			t.Errorf("AGENT_RISK_DELTA was invoked %d call(s) — should be skipped for INITIAL pipeline", 1)
		}
	}
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
