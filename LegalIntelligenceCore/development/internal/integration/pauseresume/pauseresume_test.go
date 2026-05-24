// Package pauseresume_test exercises LIC-TASK-050 — low-confidence
// pause + resume INITIAL pipeline integration test.
//
// Mirrors the happypath_test composition: the full production stack
// (real orchestrator, ingress router, pending manager, consumer, 9
// agents, aggregator, publishers, idempotency Guard, dmawaiter) is
// wired by lictestapp.NewTestApp over the four in-memory fakes
// (FakeBroker, FakeKVStore, FakeLLMProvider, FakeDM). Agent 1
// (AGENT_TYPE_CLASSIFIER) is reconfigured to return a low-confidence
// (0.55 < 0.75 threshold) response, so:
//
//  1. INITIAL artifacts-ready trigger ⇒ Stage 1 runs ⇒ low confidence
//     triggers pendingconfirmation.Manager.Pause (high-arch §6.5):
//
//       a. lic-pending-state:{vid} = encoded PendingTypeConfirmation;
//       b. publish ClassificationUncertain on
//          lic.events.classification-uncertain;
//       c. publish IN_PROGRESS{STAGE_AWAITING_USER_CONFIRMATION} on
//          lic.events.status-changed;
//       d. lic-trigger:{vid} = "PAUSED";
//       e. pipeline returns pipeline.ErrPipelinePaused, the
//          consumer ACKs.
//
//  2. Subtest A — valid resume: inject UserConfirmedType with
//     ContractType="SUPPLY" (whitelisted). Manager validates → SETNX
//     lic-user-confirmed → Load pending → tenant check → restore
//     state → override classification (confidence=1.0) → drive
//     pipeline.ResumeAfterConfirmation (Stage 2..5 + aggregator +
//     analysis-ready publish + DM persist confirmation + COMPLETED).
//     §6.10-step-9 cleanup: lic-pending-state Deleted; lic-trigger
//     and lic-user-confirmed switched to "COMPLETED" (24h TTL).
//
//  3. Subtest B — invalid contract type: inject UserConfirmedType
//     with ContractType="INVALID_TYPE_NOT_IN_WHITELIST" (passes the
//     ^[A-Z_]{1,32}$ regex but is not in the 12-value whitelist;
//     auditRejectedWhitelist branch — manager.go:311-322). Manager
//     publishes a terminal FAILED{INVALID_CONTRACT_TYPE} status,
//     sets lic-user-confirmed to "COMPLETED" and ACKs. The pipeline
//     never resumes (analysis-ready never published, pending-state
//     still present).
//
//     The lic.dlq.invalid-message envelope is asserted to be present
//     with a non-empty OriginalMessageHash (LIC-TASK-050 fix: the
//     Manager now re-marshals cmd and hashes it via the DLQHashKey
//     wired from cfg.Security.DLQHashKey, mirroring the consumer's
//     hmacFirst64 contract — see manager.go:publishInvalidDLQ).
package pauseresume_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"contractpro/legal-intelligence-core/internal/domain/model"
	"contractpro/legal-intelligence-core/internal/domain/port"
	"contractpro/legal-intelligence-core/internal/integration/fakes"
	"contractpro/legal-intelligence-core/internal/integration/lictestapp"
)

// TestPauseResume_INITIAL_Pipeline covers the two pause+resume
// branches: a happy resume after a valid UserConfirmedType, and a
// security-guard rejection of an invalid contract_type.
func TestPauseResume_INITIAL_Pipeline(t *testing.T) {
	t.Run("valid_contract_type_resumes_to_completed", testValidResume)
	t.Run("invalid_contract_type_dlqs_and_fails", testInvalidResume)
}

// ---------------------------------------------------------------------------
// Subtest A — valid contract type: pause → resume → COMPLETED.
// ---------------------------------------------------------------------------

func testValidResume(t *testing.T) {
	// 1. Build the harness with Agent 1 (AGENT_TYPE_CLASSIFIER)
	//    reconfigured to return confidence=0.55 (below the default
	//    0.75 threshold) — drives the pause path on Stage 1.
	//    lictestapp.WithCannedResponses REPLACES the per-agent FIFO
	//    (lictestapp.go installs the override via
	//    FakeLLMProvider.SetResponses), so the high-confidence default
	//    for AGENT_TYPE_CLASSIFIER is discarded while every other
	//    agent keeps its canned default for Stage 2..5.
	app := lictestapp.NewTestApp(t, lictestapp.WithCannedResponses(map[model.AgentID]string{
		model.AgentTypeClassifier: fakes.ClassifierLowConfidenceResponse,
	}))

	// 2. Canonical UUIDs for the INITIAL trigger (parent_version_id
	//    stays nil — INITIAL mode).
	correlationID := uuid.NewString()
	jobID := uuid.NewString()
	documentID := uuid.NewString()
	versionID := uuid.NewString()
	organizationID := uuid.NewString()
	createdByUserID := uuid.NewString()

	// 3. Pre-program the FakeDM artifacts response with the four
	//    mandatory bundle artifacts (semantic tree, extracted text,
	//    document structure, processing warnings).
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

	// 4. Inject the trigger. Inject is synchronous over the handler
	//    chain; FakeDM responds via a goroutine, so we still poll
	//    for the classification-uncertain event below.
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	startPause := time.Now()
	res, err := app.Broker.Inject(ctx, fakes.RoutingKeyVersionArtifactsReady, nil, mustJSON(t, trigger))
	if err != nil {
		t.Fatalf("Inject(version-artifacts-ready): %v", err)
	}
	if res.Acked != 1 {
		t.Fatalf("pause trigger InjectResult.Acked = %d, want 1 (Nacked=%d)", res.Acked, res.Nacked)
	}

	// 5. Wait for the pause signal — the Manager publishes
	//    ClassificationUncertain immediately after persisting
	//    lic-pending-state (Pause step 2 per manager.go:232).
	pauseWaitCtx, pauseWaitCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer pauseWaitCancel()
	if err := waitForClassificationUncertain(pauseWaitCtx, app.Broker); err != nil {
		t.Fatalf("waiting for classification-uncertain pause signal: %v", err)
	}
	t.Logf("pause reached after %v", time.Since(startPause))

	// 6. Assert the pause publish triplet.
	//
	// 6.a — classification-uncertain: exactly one publication; payload
	//       carries the low confidence, the configured threshold,
	//       the suggested OTHER type and the correlation ids.
	uncertainMsgs := app.Broker.PublishedOn(fakes.RoutingKeyClassificationUncertain)
	if len(uncertainMsgs) != 1 {
		t.Fatalf("classification-uncertain: got %d publish(es), want exactly 1", len(uncertainMsgs))
	}
	var uncertain port.ClassificationUncertain
	fakes.MatchEvent(t, uncertainMsgs[0], &uncertain)
	if uncertain.Confidence != 0.55 {
		t.Errorf("uncertain.Confidence = %v, want 0.55", uncertain.Confidence)
	}
	if uncertain.Threshold != 0.75 {
		t.Errorf("uncertain.Threshold = %v, want 0.75 (default)", uncertain.Threshold)
	}
	if uncertain.SuggestedType != model.ContractType("OTHER") {
		t.Errorf("uncertain.SuggestedType = %q, want OTHER", uncertain.SuggestedType)
	}
	if uncertain.OrganizationID != organizationID {
		t.Errorf("uncertain.OrganizationID = %q, want %q", uncertain.OrganizationID, organizationID)
	}
	if uncertain.VersionID != versionID {
		t.Errorf("uncertain.VersionID = %q, want %q", uncertain.VersionID, versionID)
	}
	if uncertain.JobID != jobID {
		t.Errorf("uncertain.JobID = %q, want %q", uncertain.JobID, jobID)
	}
	if uncertain.CorrelationID != correlationID {
		t.Errorf("uncertain.CorrelationID = %q, want %q", uncertain.CorrelationID, correlationID)
	}

	// 6.b — status-changed: at least 2 (the first IN_PROGRESS
	//       {STAGE_REQUESTING_ARTIFACTS} from Stage 0, then the
	//       IN_PROGRESS{STAGE_AWAITING_USER_CONFIRMATION} from Pause
	//       step 3). Sanity-check the first; locate the
	//       AWAITING_USER_CONFIRMATION publication explicitly.
	statusMsgs := app.Broker.PublishedOn(fakes.RoutingKeyStatusChanged)
	if len(statusMsgs) < 2 {
		t.Fatalf("status-changed at pause: got %d publish(es), want >= 2", len(statusMsgs))
	}
	var firstStatus port.LICStatusChangedEvent
	fakes.MatchEvent(t, statusMsgs[0], &firstStatus)
	if firstStatus.Status != model.StatusInProgress {
		t.Errorf("first status: status = %q, want IN_PROGRESS", firstStatus.Status)
	}
	if firstStatus.Stage != model.StageRequestingArtifacts {
		t.Errorf("first status: stage = %q, want STAGE_REQUESTING_ARTIFACTS", firstStatus.Stage)
	}

	awaitingIdx := -1
	for i, msg := range statusMsgs {
		var s port.LICStatusChangedEvent
		if err := jsonUnmarshal(msg.Payload, &s); err != nil {
			continue
		}
		if s.Status == model.StatusInProgress && s.Stage == model.StageAwaitingUserConfirmation {
			awaitingIdx = i
			break
		}
	}
	if awaitingIdx < 0 {
		t.Fatalf("status-changed: no IN_PROGRESS{STAGE_AWAITING_USER_CONFIRMATION} observed before resume")
	}

	// 6.c — Pause publish order (manager.go:209-263, sequence-diagrams
	//       §2.1): classification-uncertain published BEFORE the
	//       AWAITING_USER_CONFIRMATION status.
	if !uncertainMsgs[0].At.Before(statusMsgs[awaitingIdx].At) {
		t.Errorf("expected classification-uncertain (%v) before IN_PROGRESS{AWAITING} (%v)",
			uncertainMsgs[0].At, statusMsgs[awaitingIdx].At)
	}

	// 6.d — analysis-ready MUST NOT have been published yet
	//       (Stage 2-5 did not run because the pipeline paused after
	//       Stage 1).
	if msgs := app.Broker.PublishedOn(fakes.RoutingKeyAnalysisReady); len(msgs) != 0 {
		t.Fatalf("analysis-ready pre-resume: got %d publish(es), want 0", len(msgs))
	}

	// 6.e — Redis pending state MUST be persisted (Pause step 1).
	pendingRaw, err := app.KV.Get(ctx, "lic-pending-state:"+versionID)
	if err != nil {
		t.Fatalf("lic-pending-state Get: %v", err)
	}
	if pendingRaw == "" {
		t.Fatal("lic-pending-state is present but value is empty")
	}

	// 6.f — Redis lic-trigger MUST be "PAUSED" (Pause step 4 — the
	//       string constant comes from port.IdempotencyPaused).
	triggerVal, err := app.KV.Get(ctx, "lic-trigger:"+versionID)
	if err != nil {
		t.Fatalf("lic-trigger Get during pause: %v", err)
	}
	if triggerVal != string(port.IdempotencyPaused) {
		t.Errorf("lic-trigger during pause = %q, want %q", triggerVal, string(port.IdempotencyPaused))
	}

	// 7. Inject the UserConfirmedType resume command with a
	//    whitelisted contract type ("SUPPLY" is one of the 12).
	resume := port.UserConfirmedType{
		CorrelationID:  correlationID,
		Timestamp:      time.Now().UTC().Format(time.RFC3339Nano),
		JobID:          jobID,
		DocumentID:     documentID,
		VersionID:      versionID,
		OrganizationID: organizationID,
		ContractType:   "SUPPLY",
		UserID:         createdByUserID,
	}
	startResume := time.Now()
	res2, err := app.Broker.Inject(ctx, fakes.RoutingKeyUserConfirmedType, nil, mustJSON(t, resume))
	if err != nil {
		t.Fatalf("Inject(user-confirmed-type): %v", err)
	}
	if res2.Acked != 1 {
		t.Fatalf("resume InjectResult.Acked = %d, want 1 (Nacked=%d)", res2.Acked, res2.Nacked)
	}

	// 8. Wait for the terminal COMPLETED status — the resumed pipeline
	//    runs Stage 2..5, publishes analysis-ready, FakeDM responds
	//    Persisted, the orchestrator publishes COMPLETED.
	completeWaitCtx, completeWaitCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer completeWaitCancel()
	if err := waitForCompletedStatus(completeWaitCtx, app.Broker); err != nil {
		t.Fatalf("waiting for COMPLETED status after resume: %v", err)
	}
	t.Logf("resume → COMPLETED took %v", time.Since(startResume))

	// 9. Post-resume assertions.
	//
	// 9.a — analysis-ready: exactly one publication; the overridden
	//       contract_type is SUPPLY (manager.go:403) with
	//       confidence=1.0 (manager.go:404); RiskDelta is nil
	//       (INITIAL).
	analysisMsgs := app.Broker.PublishedOn(fakes.RoutingKeyAnalysisReady)
	if len(analysisMsgs) != 1 {
		t.Fatalf("analysis-ready post-resume: got %d publish(es), want exactly 1", len(analysisMsgs))
	}
	var analysis port.LegalAnalysisArtifactsReady
	fakes.MatchEvent(t, analysisMsgs[0], &analysis)
	if analysis.ClassificationResult == nil {
		t.Fatal("analysis.ClassificationResult is nil")
	}
	if analysis.ClassificationResult.ContractType != model.ContractType("SUPPLY") {
		t.Errorf("analysis.ClassificationResult.ContractType = %q, want SUPPLY (overridden by resume)",
			analysis.ClassificationResult.ContractType)
	}
	if analysis.ClassificationResult.Confidence != 1.0 {
		t.Errorf("analysis.ClassificationResult.Confidence = %v, want 1.0 (overridden by resume)",
			analysis.ClassificationResult.Confidence)
	}
	if analysis.RiskAnalysis == nil {
		t.Error("analysis.RiskAnalysis is nil (Stage 2 did not run after resume)")
	}
	if analysis.RiskProfile == nil {
		t.Error("analysis.RiskProfile is nil (aggregator did not derive after resume)")
	}
	if len(analysis.Recommendations) < 1 {
		t.Errorf("analysis.Recommendations = %d, want >= 1", len(analysis.Recommendations))
	}
	if analysis.Summary == nil {
		t.Error("analysis.Summary is nil after resume")
	}
	if analysis.DetailedReport == nil {
		t.Error("analysis.DetailedReport is nil after resume")
	}
	if analysis.AggregateScore == nil {
		t.Error("analysis.AggregateScore is nil after resume")
	}
	if analysis.KeyParameters == nil {
		t.Error("analysis.KeyParameters is nil after resume")
	}
	if analysis.RiskDelta != nil {
		t.Errorf("analysis.RiskDelta should be nil for INITIAL, got %+v", analysis.RiskDelta)
	}

	// 9.b — last status-changed publication is COMPLETED.
	statusMsgs = app.Broker.PublishedOn(fakes.RoutingKeyStatusChanged)
	var lastStatus port.LICStatusChangedEvent
	fakes.MatchEvent(t, statusMsgs[len(statusMsgs)-1], &lastStatus)
	if lastStatus.Status != model.StatusCompleted {
		t.Errorf("last status post-resume = %q, want COMPLETED", lastStatus.Status)
	}

	// 9.c — §6.10-step-9 cleanup: lic-pending-state MUST be deleted
	//       (manager.go:434), lic-trigger and lic-user-confirmed are
	//       both moved to "COMPLETED" (manager.go:438,442).
	if _, err := app.KV.Get(ctx, "lic-pending-state:"+versionID); !errors.Is(err, fakes.ErrKeyNotFound) {
		t.Errorf("lic-pending-state after resume: err = %v, want fakes.ErrKeyNotFound (deleted on cleanup)", err)
	}
	finalTrigger, err := app.KV.Get(ctx, "lic-trigger:"+versionID)
	if err != nil {
		t.Fatalf("lic-trigger Get after resume: %v", err)
	}
	if finalTrigger != string(port.IdempotencyCompleted) {
		t.Errorf("lic-trigger after resume = %q, want %q", finalTrigger, string(port.IdempotencyCompleted))
	}
	finalUserConfirmed, err := app.KV.Get(ctx, "lic-user-confirmed:"+versionID)
	if err != nil {
		t.Fatalf("lic-user-confirmed Get after resume: %v", err)
	}
	if finalUserConfirmed != string(port.IdempotencyCompleted) {
		t.Errorf("lic-user-confirmed after resume = %q, want %q", finalUserConfirmed, string(port.IdempotencyCompleted))
	}

	// 9.d — DLQ MUST be empty on the happy resume path.
	if msgs := app.Broker.PublishedOn(fakes.RoutingKeyDLQInvalidMessage); len(msgs) != 0 {
		t.Errorf("DLQ invalid-message: got %d publish(es) on happy resume, want 0", len(msgs))
	}
}

// ---------------------------------------------------------------------------
// Subtest B — invalid contract type: rejected_whitelist branch.
// ---------------------------------------------------------------------------

func testInvalidResume(t *testing.T) {
	// 1. Fresh harness — the subtest B state must not leak from A.
	app := lictestapp.NewTestApp(t, lictestapp.WithCannedResponses(map[model.AgentID]string{
		model.AgentTypeClassifier: fakes.ClassifierLowConfidenceResponse,
	}))

	// 2. Same INITIAL trigger build as subtest A.
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

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	res, err := app.Broker.Inject(ctx, fakes.RoutingKeyVersionArtifactsReady, nil, mustJSON(t, trigger))
	if err != nil {
		t.Fatalf("Inject(version-artifacts-ready): %v", err)
	}
	if res.Acked != 1 {
		t.Fatalf("pause trigger InjectResult.Acked = %d, want 1", res.Acked)
	}

	// 3. Wait until pause has reached (no need to re-assert the full
	//    triplet — subtest A pinned the pause invariants).
	pauseWaitCtx, pauseWaitCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer pauseWaitCancel()
	if err := waitForClassificationUncertain(pauseWaitCtx, app.Broker); err != nil {
		t.Fatalf("waiting for classification-uncertain pause signal: %v", err)
	}

	// 4. Inject UserConfirmedType with a value that PASSES the regex
	//    ^[A-Z_]{1,32}$ but is NOT in the 12-value whitelist — drives
	//    the auditRejectedWhitelist branch (manager.go:311-322).
	resume := port.UserConfirmedType{
		CorrelationID:  correlationID,
		Timestamp:      time.Now().UTC().Format(time.RFC3339Nano),
		JobID:          jobID,
		DocumentID:     documentID,
		VersionID:      versionID,
		OrganizationID: organizationID,
		ContractType:   "INVALID_TYPE_NOT_IN_WHITELIST",
		UserID:         createdByUserID,
	}
	res2, err := app.Broker.Inject(ctx, fakes.RoutingKeyUserConfirmedType, nil, mustJSON(t, resume))
	if err != nil {
		t.Fatalf("Inject(user-confirmed-type): %v", err)
	}
	// Manager.HandleUserConfirmedType returns nil on this branch
	// (§6.10:776 — ACK), so InjectResult.Acked == 1.
	if res2.Acked != 1 {
		t.Fatalf("invalid resume InjectResult.Acked = %d, want 1 (Nacked=%d)", res2.Acked, res2.Nacked)
	}

	// 5. Wait for the terminal FAILED status — Manager publishes
	//    FAILED{INVALID_CONTRACT_TYPE} synchronously inside
	//    HandleUserConfirmedType (manager.go:318), so it MUST be
	//    already recorded by the time Inject returns — but we poll
	//    defensively via waitForFailedStatus to avoid any goroutine
	//    surprise.
	failedWaitCtx, failedWaitCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer failedWaitCancel()
	if err := waitForFailedStatus(failedWaitCtx, app.Broker); err != nil {
		t.Fatalf("waiting for FAILED status after invalid resume: %v", err)
	}

	// 6. Assert the last status-changed is FAILED with the catalog
	//    code INVALID_CONTRACT_TYPE and is_retryable=false (per
	//    model.LookupErrorSpec).
	statusMsgs := app.Broker.PublishedOn(fakes.RoutingKeyStatusChanged)
	var lastStatus port.LICStatusChangedEvent
	fakes.MatchEvent(t, statusMsgs[len(statusMsgs)-1], &lastStatus)
	if lastStatus.Status != model.StatusFailed {
		t.Errorf("last status = %q, want FAILED", lastStatus.Status)
	}
	if lastStatus.ErrorCode != model.ErrCodeInvalidContractType {
		t.Errorf("last status ErrorCode = %q, want %q", lastStatus.ErrorCode, model.ErrCodeInvalidContractType)
	}
	if lastStatus.IsRetryable == nil {
		t.Fatal("last status IsRetryable is nil; want *bool=false")
	}
	if *lastStatus.IsRetryable != false {
		t.Errorf("last status *IsRetryable = %v, want false", *lastStatus.IsRetryable)
	}
	// Cross-check the catalog row is non-retryable (defensive — if
	// somebody flips the spec the assertion above still holds).
	if spec, ok := model.LookupErrorSpec(model.ErrCodeInvalidContractType); !ok || spec.Retryable {
		t.Errorf("LookupErrorSpec(INVALID_CONTRACT_TYPE) = %+v ok=%v, want retryable=false", spec, ok)
	}

	// 7. DLQ invalid-message envelope — Manager.publishInvalidDLQ
	//    (manager.go) re-marshals cmd, computes HMAC-SHA-256 keyed by
	//    cfg.DLQHashKey, and ships the envelope via DLQPublisher. The
	//    envelope MUST carry the canonical OriginalTopic + ErrorCode +
	//    non-empty HMAC + best-effort correlation IDs.
	dlqMsgs := app.Broker.PublishedOn(fakes.RoutingKeyDLQInvalidMessage)
	if len(dlqMsgs) != 1 {
		t.Fatalf("DLQ invalid-message: got %d publish(es), want exactly 1", len(dlqMsgs))
	}
	var dlqEnv port.LICDLQEnvelope
	fakes.MatchEvent(t, dlqMsgs[0], &dlqEnv)
	if dlqEnv.OriginalTopic != "orch.commands.user-confirmed-type" {
		t.Errorf("DLQ envelope OriginalTopic = %q, want %q",
			dlqEnv.OriginalTopic, "orch.commands.user-confirmed-type")
	}
	if dlqEnv.ErrorCode != model.ErrCodeInvalidContractType {
		t.Errorf("DLQ envelope ErrorCode = %q, want %q",
			dlqEnv.ErrorCode, model.ErrCodeInvalidContractType)
	}
	if dlqEnv.OriginalMessageHash == "" {
		t.Error("DLQ envelope OriginalMessageHash is empty (would be rejected by dlq.DLQPublisher)")
	}
	if len(dlqEnv.OriginalMessageHash) != 64 {
		t.Errorf("DLQ envelope hash length = %d, want 64 (SHA-256 hex)", len(dlqEnv.OriginalMessageHash))
	}
	if dlqEnv.OriginalMessageSizeBytes <= 0 {
		t.Errorf("DLQ envelope OriginalMessageSizeBytes = %d, want > 0", dlqEnv.OriginalMessageSizeBytes)
	}
	if dlqEnv.VersionID != versionID {
		t.Errorf("DLQ envelope VersionID = %q, want %q", dlqEnv.VersionID, versionID)
	}
	if dlqEnv.OrganizationID != organizationID {
		t.Errorf("DLQ envelope OrganizationID = %q, want %q", dlqEnv.OrganizationID, organizationID)
	}
	if dlqEnv.JobID != jobID {
		t.Errorf("DLQ envelope JobID = %q, want %q", dlqEnv.JobID, jobID)
	}
	if dlqEnv.CorrelationID != correlationID {
		t.Errorf("DLQ envelope CorrelationID = %q, want %q", dlqEnv.CorrelationID, correlationID)
	}

	// 8. lic-user-confirmed:{vid} MUST be "COMPLETED" — manager.go:319
	//    runs setUserConfirmedCompleted before returning, even on the
	//    invalid branch (so an immediate redelivery hits the
	//    COMPLETED ACK fast path).
	userConfirmed, err := app.KV.Get(ctx, "lic-user-confirmed:"+versionID)
	if err != nil {
		t.Fatalf("lic-user-confirmed Get after invalid resume: %v", err)
	}
	if userConfirmed != string(port.IdempotencyCompleted) {
		t.Errorf("lic-user-confirmed after invalid resume = %q, want %q",
			userConfirmed, string(port.IdempotencyCompleted))
	}

	// 9. lic-pending-state:{vid} MUST STILL be present — the invalid
	//    branch returns BEFORE the resume path that Deletes the
	//    pending state (manager.go:319 vs manager.go:434). The
	//    pending state is consumed by the Delete only on a successful
	//    resume.
	pendingRaw, err := app.KV.Get(ctx, "lic-pending-state:"+versionID)
	if err != nil {
		t.Fatalf("lic-pending-state Get after invalid resume: %v (want present)", err)
	}
	if pendingRaw == "" {
		t.Error("lic-pending-state is empty after invalid resume (should still be persisted)")
	}

	// 10. analysis-ready MUST NOT have been published — Stage 2-5
	//     never ran on the invalid branch.
	if msgs := app.Broker.PublishedOn(fakes.RoutingKeyAnalysisReady); len(msgs) != 0 {
		t.Errorf("analysis-ready on invalid resume: got %d publish(es), want 0", len(msgs))
	}
}

// ---------------------------------------------------------------------------
// Helpers.
// ---------------------------------------------------------------------------

// mustJSON marshals v and fails the test on error.
func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	body, err := jsonMarshal(v)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	return body
}

// waitForClassificationUncertain polls the broker's classification-
// uncertain log until at least one message arrives or ctx expires.
func waitForClassificationUncertain(ctx context.Context, fb *fakes.FakeBroker) error {
	tk := time.NewTicker(2 * time.Millisecond)
	defer tk.Stop()
	for {
		if msgs := fb.PublishedOn(fakes.RoutingKeyClassificationUncertain); len(msgs) > 0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-tk.C:
		}
	}
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
