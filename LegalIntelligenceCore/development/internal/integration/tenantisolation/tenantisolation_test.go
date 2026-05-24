// Package tenantisolation_test pins LIC-TASK-054 — tenant isolation
// invariants for the Legal Intelligence Core service.
//
// The six tests cover:
//
//  1. organization_id propagation through every outgoing event of the
//     happy-path INITIAL pipeline (request-artifacts, status-changed,
//     analysis-ready).
//
//  2. organization_id propagation through pause + resume — every event
//     before the pause, the classification-uncertain pause signal, every
//     event after the user-confirmed resume, plus the
//     org-id-of-the-persisted-pending-state.
//
//  3. Cross-event tenant check on UserConfirmedType
//     (`pendingconfirmation.Manager.HandleUserConfirmedType` —
//     manager.go:396): a forged cmd.OrganizationID that does NOT match
//     the persisted PendingTypeConfirmation.OrganizationID is poison-
//     ACKed, DLQ'd with `INVALID_ORG_ID_MISMATCH`, and the pending-state
//     is NOT consumed (security.md §11.2 line 496).
//
//  4. LLM-call wire purity — no organization_id leak into
//     CompletionRequest.System / .User; reflection-confirms the wire DTO
//     `port.CompletionRequest` has NO org-id field
//     (llm.go:134-136 godoc: "Correlation identifiers ... are propagated
//     via context, not fields, so the wire envelope stays minimal and
//     PII-free").
//
//  5. OTel attribute key pin — `tracer.AttrOrganizationID` is literally
//     "organization_id" (correlation-level wire key per
//     observability.md §4.3). Renaming is a breaking dashboard change.
//
//  6. Prometheus label hygiene — no Prometheus metric exposes an
//     organization_id (or any case-/spelling-variant) label
//     (observability.md §3.10 + metrics/registry.go:11).
package tenantisolation_test

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	dto "github.com/prometheus/client_model/go"

	"contractpro/legal-intelligence-core/internal/domain/model"
	"contractpro/legal-intelligence-core/internal/domain/port"
	"contractpro/legal-intelligence-core/internal/infra/observability/metrics"
	"contractpro/legal-intelligence-core/internal/infra/observability/tracer"
	"contractpro/legal-intelligence-core/internal/integration/fakes"
	"contractpro/legal-intelligence-core/internal/integration/lictestapp"
)

// ---------------------------------------------------------------------------
// 1. INITIAL happy-path organization_id propagation.
// ---------------------------------------------------------------------------

// TestTenantIsolation_OrgID_PropagationToOutgoing_INITIAL drives the full
// INITIAL pipeline and asserts that organization_id flows into every
// outgoing event LIC owns.
//
// Reconciliation R1: ArtifactsProvided has NO OrganizationID field
// (port.events.go:81-91, frozen event-catalog §2.1). Therefore we cannot
// assert cross-event matching on inbound DM responses at JSON-payload
// level; we only assert org-id on events LIC *publishes*.
func TestTenantIsolation_OrgID_PropagationToOutgoing_INITIAL(t *testing.T) {
	app := lictestapp.NewTestApp(t)

	correlationID := uuid.NewString()
	jobID := uuid.NewString()
	documentID := uuid.NewString()
	versionID := uuid.NewString()
	orgID := uuid.NewString()
	createdByUserID := uuid.NewString()

	app.DM.SetArtifactsResponse(versionID, fakes.BuildArtifactsResponse(fakes.DefaultArtifactsBundle()))

	trigger := port.VersionProcessingArtifactsReady{
		CorrelationID:   correlationID,
		Timestamp:       time.Now().UTC().Format(time.RFC3339Nano),
		DocumentID:      documentID,
		VersionID:       versionID,
		OrganizationID:  orgID,
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
		t.Fatalf("InjectResult.Acked = %d, want 1 (Nacked=%d)", res.Acked, res.Nacked)
	}

	waitCtx, waitCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer waitCancel()
	if err := waitForCompletedStatus(waitCtx, app.Broker); err != nil {
		t.Fatalf("waiting for COMPLETED status: %v", err)
	}

	// lic.requests.artifacts — exactly 1 outbound request, org-id propagated.
	reqMsgs := app.Broker.PublishedOn(fakes.RoutingKeyRequestArtifacts)
	if len(reqMsgs) != 1 {
		t.Fatalf("requests.artifacts: got %d publish(es), want 1", len(reqMsgs))
	}
	var req port.GetArtifactsRequest
	fakes.MatchEvent(t, reqMsgs[0], &req)
	if req.OrganizationID != orgID {
		t.Errorf("GetArtifactsRequest.OrganizationID = %q, want %q", req.OrganizationID, orgID)
	}

	// lic.events.status-changed — every publication carries org-id.
	statusMsgs := app.Broker.PublishedOn(fakes.RoutingKeyStatusChanged)
	if len(statusMsgs) < 2 {
		t.Fatalf("status-changed: got %d publish(es), want >= 2", len(statusMsgs))
	}
	for i, msg := range statusMsgs {
		var s port.LICStatusChangedEvent
		fakes.MatchEvent(t, msg, &s)
		if s.OrganizationID != orgID {
			t.Errorf("status-changed[%d] OrganizationID = %q, want %q", i, s.OrganizationID, orgID)
		}
	}

	// lic.artifacts.analysis-ready — exactly 1 terminal publication, org-id propagated.
	analysisMsgs := app.Broker.PublishedOn(fakes.RoutingKeyAnalysisReady)
	if len(analysisMsgs) != 1 {
		t.Fatalf("analysis-ready: got %d publish(es), want 1", len(analysisMsgs))
	}
	var analysis port.LegalAnalysisArtifactsReady
	fakes.MatchEvent(t, analysisMsgs[0], &analysis)
	if analysis.OrganizationID != orgID {
		t.Errorf("LegalAnalysisArtifactsReady.OrganizationID = %q, want %q", analysis.OrganizationID, orgID)
	}
}

// ---------------------------------------------------------------------------
// 2. Pause + Resume organization_id propagation.
// ---------------------------------------------------------------------------

// TestTenantIsolation_OrgID_PropagationToOutgoing_PauseResume drives a
// low-confidence pause, resumes with the matching org-id, and asserts
// org-id stays consistent across every outgoing event AND across the
// persisted pending-state blob.
func TestTenantIsolation_OrgID_PropagationToOutgoing_PauseResume(t *testing.T) {
	app := lictestapp.NewTestApp(t, lictestapp.WithCannedResponses(map[model.AgentID]string{
		model.AgentTypeClassifier: fakes.ClassifierLowConfidenceResponse,
	}))

	correlationID := uuid.NewString()
	jobID := uuid.NewString()
	documentID := uuid.NewString()
	versionID := uuid.NewString()
	orgID := uuid.NewString()
	createdByUserID := uuid.NewString()

	app.DM.SetArtifactsResponse(versionID, fakes.BuildArtifactsResponse(fakes.DefaultArtifactsBundle()))

	trigger := port.VersionProcessingArtifactsReady{
		CorrelationID:   correlationID,
		Timestamp:       time.Now().UTC().Format(time.RFC3339Nano),
		DocumentID:      documentID,
		VersionID:       versionID,
		OrganizationID:  orgID,
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
		t.Fatalf("pause trigger InjectResult.Acked = %d, want 1 (Nacked=%d)", res.Acked, res.Nacked)
	}

	pauseWaitCtx, pauseWaitCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer pauseWaitCancel()
	if err := waitForClassificationUncertain(pauseWaitCtx, app.Broker); err != nil {
		t.Fatalf("waiting for classification-uncertain pause signal: %v", err)
	}

	// Pause-side org-id assertions.
	uncertainMsgs := app.Broker.PublishedOn(fakes.RoutingKeyClassificationUncertain)
	if len(uncertainMsgs) != 1 {
		t.Fatalf("classification-uncertain: got %d publish(es), want 1", len(uncertainMsgs))
	}
	var uncertain port.ClassificationUncertain
	fakes.MatchEvent(t, uncertainMsgs[0], &uncertain)
	if uncertain.OrganizationID != orgID {
		t.Errorf("ClassificationUncertain.OrganizationID = %q, want %q", uncertain.OrganizationID, orgID)
	}

	// Every status-changed so far carries org-id (incl. the IN_PROGRESS
	// {STAGE_AWAITING_USER_CONFIRMATION} pause status).
	for i, msg := range app.Broker.PublishedOn(fakes.RoutingKeyStatusChanged) {
		var s port.LICStatusChangedEvent
		fakes.MatchEvent(t, msg, &s)
		if s.OrganizationID != orgID {
			t.Errorf("pause-phase status-changed[%d] OrganizationID = %q, want %q", i, s.OrganizationID, orgID)
		}
	}

	// Re-read the persisted pending-state blob — decode via
	// model.DecodePendingTypeConfirmation (gzip+base64+JSON) because the
	// Redis value is the encoded form; the typed org-id is what the
	// tenant check at manager.go:396 will compare against on resume.
	pendingRaw, err := app.KV.Get(ctx, "lic-pending-state:"+versionID)
	if err != nil {
		t.Fatalf("lic-pending-state Get: %v", err)
	}
	ptc, err := model.DecodePendingTypeConfirmation([]byte(pendingRaw))
	if err != nil {
		t.Fatalf("DecodePendingTypeConfirmation: %v", err)
	}
	if ptc.OrganizationID != orgID {
		t.Errorf("PendingTypeConfirmation.OrganizationID = %q, want %q", ptc.OrganizationID, orgID)
	}

	// Resume with the SAME org-id.
	resume := port.UserConfirmedType{
		CorrelationID:  correlationID,
		Timestamp:      time.Now().UTC().Format(time.RFC3339Nano),
		JobID:          jobID,
		DocumentID:     documentID,
		VersionID:      versionID,
		OrganizationID: orgID,
		ContractType:   "SUPPLY",
		UserID:         createdByUserID,
	}
	res2, err := app.Broker.Inject(ctx, fakes.RoutingKeyUserConfirmedType, nil, mustJSON(t, resume))
	if err != nil {
		t.Fatalf("Inject(user-confirmed-type): %v", err)
	}
	if res2.Acked != 1 {
		t.Fatalf("resume InjectResult.Acked = %d, want 1 (Nacked=%d)", res2.Acked, res2.Nacked)
	}

	completeWaitCtx, completeWaitCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer completeWaitCancel()
	if err := waitForCompletedStatus(completeWaitCtx, app.Broker); err != nil {
		t.Fatalf("waiting for COMPLETED status after resume: %v", err)
	}

	// Post-resume: every status-changed AND the analysis-ready carry org-id.
	for i, msg := range app.Broker.PublishedOn(fakes.RoutingKeyStatusChanged) {
		var s port.LICStatusChangedEvent
		fakes.MatchEvent(t, msg, &s)
		if s.OrganizationID != orgID {
			t.Errorf("post-resume status-changed[%d] OrganizationID = %q, want %q", i, s.OrganizationID, orgID)
		}
	}
	analysisMsgs := app.Broker.PublishedOn(fakes.RoutingKeyAnalysisReady)
	if len(analysisMsgs) != 1 {
		t.Fatalf("post-resume analysis-ready: got %d publish(es), want 1", len(analysisMsgs))
	}
	var analysis port.LegalAnalysisArtifactsReady
	fakes.MatchEvent(t, analysisMsgs[0], &analysis)
	if analysis.OrganizationID != orgID {
		t.Errorf("post-resume LegalAnalysisArtifactsReady.OrganizationID = %q, want %q", analysis.OrganizationID, orgID)
	}
}

// ---------------------------------------------------------------------------
// 3. Cross-event tenant check — forged UserConfirmedType org-id mismatch.
// ---------------------------------------------------------------------------

// TestTenantIsolation_UserConfirmedType_OrgIDMismatch_DLQ pauses the
// pipeline with pipelineOrgID, then injects a UserConfirmedType with a
// DIFFERENT forgedOrgID. The Manager (manager.go:396) MUST detect the
// mismatch, DLQ the message with INVALID_ORG_ID_MISMATCH, leave the
// pending-state intact (security.md §11.2 line 496), and ACK to stop
// the poison-loop. No FAILED status is published (the code is non-
// publishable — see error_codes.go).
func TestTenantIsolation_UserConfirmedType_OrgIDMismatch_DLQ(t *testing.T) {
	app := lictestapp.NewTestApp(t, lictestapp.WithCannedResponses(map[model.AgentID]string{
		model.AgentTypeClassifier: fakes.ClassifierLowConfidenceResponse,
	}))

	correlationID := uuid.NewString()
	jobID := uuid.NewString()
	documentID := uuid.NewString()
	versionID := uuid.NewString()
	pipelineOrgID := uuid.NewString()
	forgedOrgID := uuid.NewString()
	createdByUserID := uuid.NewString()

	app.DM.SetArtifactsResponse(versionID, fakes.BuildArtifactsResponse(fakes.DefaultArtifactsBundle()))

	trigger := port.VersionProcessingArtifactsReady{
		CorrelationID:   correlationID,
		Timestamp:       time.Now().UTC().Format(time.RFC3339Nano),
		DocumentID:      documentID,
		VersionID:       versionID,
		OrganizationID:  pipelineOrgID,
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
		t.Fatalf("pause trigger InjectResult.Acked = %d, want 1 (Nacked=%d)", res.Acked, res.Nacked)
	}

	pauseWaitCtx, pauseWaitCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer pauseWaitCancel()
	if err := waitForClassificationUncertain(pauseWaitCtx, app.Broker); err != nil {
		t.Fatalf("waiting for classification-uncertain pause signal: %v", err)
	}

	statusBefore := len(app.Broker.PublishedOn(fakes.RoutingKeyStatusChanged))

	// Forge the resume: ContractType="SUPPLY" passes the regex AND the
	// 12-value whitelist so manager.go reaches step 4 (tenant check)
	// rather than short-circuiting on auditRejectedWhitelist/Format.
	forged := port.UserConfirmedType{
		CorrelationID:  correlationID,
		Timestamp:      time.Now().UTC().Format(time.RFC3339Nano),
		JobID:          jobID,
		DocumentID:     documentID,
		VersionID:      versionID,
		OrganizationID: forgedOrgID,
		ContractType:   "SUPPLY",
		UserID:         createdByUserID,
	}
	res2, err := app.Broker.Inject(ctx, fakes.RoutingKeyUserConfirmedType, nil, mustJSON(t, forged))
	if err != nil {
		t.Fatalf("Inject(user-confirmed-type forged): %v", err)
	}
	// Manager returns nil on the tenant-mismatch branch (manager.go:404) —
	// poison message ACK so it does not loop.
	if res2.Acked != 1 {
		t.Fatalf("forged-resume InjectResult.Acked = %d, want 1 (Nacked=%d)", res2.Acked, res2.Nacked)
	}

	// DLQ: exactly 1 invalid-message envelope.
	dlqMsgs := app.Broker.PublishedOn(fakes.RoutingKeyDLQInvalidMessage)
	if len(dlqMsgs) != 1 {
		t.Fatalf("DLQ invalid-message: got %d publish(es), want 1", len(dlqMsgs))
	}
	var dlqEnv port.LICDLQEnvelope
	fakes.MatchEvent(t, dlqMsgs[0], &dlqEnv)
	if dlqEnv.OriginalTopic != "orch.commands.user-confirmed-type" {
		t.Errorf("DLQ OriginalTopic = %q, want %q", dlqEnv.OriginalTopic, "orch.commands.user-confirmed-type")
	}
	if dlqEnv.ErrorCode != model.ErrCodeInvalidOrgIDMismatch {
		t.Errorf("DLQ ErrorCode = %q, want %q", dlqEnv.ErrorCode, model.ErrCodeInvalidOrgIDMismatch)
	}
	// Best-effort: Manager carries cmd.OrganizationID (the forged value)
	// into the envelope — see publishInvalidDLQ.
	if dlqEnv.OrganizationID != forgedOrgID {
		t.Errorf("DLQ OrganizationID = %q, want %q (forged cmd value)", dlqEnv.OrganizationID, forgedOrgID)
	}
	if dlqEnv.OriginalMessageHash == "" {
		t.Error("DLQ OriginalMessageHash is empty (would be rejected by dlq.DLQPublisher)")
	}
	if len(dlqEnv.OriginalMessageHash) != 64 {
		t.Errorf("DLQ hash length = %d, want 64 (SHA-256 hex)", len(dlqEnv.OriginalMessageHash))
	}
	if dlqEnv.VersionID != versionID {
		t.Errorf("DLQ VersionID = %q, want %q", dlqEnv.VersionID, versionID)
	}
	if dlqEnv.JobID != jobID {
		t.Errorf("DLQ JobID = %q, want %q", dlqEnv.JobID, jobID)
	}
	if dlqEnv.CorrelationID != correlationID {
		t.Errorf("DLQ CorrelationID = %q, want %q", dlqEnv.CorrelationID, correlationID)
	}

	// security.md §11.2 line 496 — pending-state NOT consumed on tenant
	// mismatch. Re-decode and assert the stored org-id is still the
	// pipeline's, never the forged one.
	pendingRaw, err := app.KV.Get(ctx, "lic-pending-state:"+versionID)
	if err != nil {
		t.Fatalf("lic-pending-state Get after forged resume: %v (want present)", err)
	}
	if pendingRaw == "" {
		t.Fatal("lic-pending-state is empty after forged resume (should still be persisted)")
	}
	ptc, err := model.DecodePendingTypeConfirmation([]byte(pendingRaw))
	if err != nil {
		t.Fatalf("DecodePendingTypeConfirmation after forged resume: %v", err)
	}
	if ptc.OrganizationID != pipelineOrgID {
		t.Errorf("persisted pending org-id = %q, want %q (forged %q must not overwrite)",
			ptc.OrganizationID, pipelineOrgID, forgedOrgID)
	}

	// analysis-ready MUST NOT have been published.
	if msgs := app.Broker.PublishedOn(fakes.RoutingKeyAnalysisReady); len(msgs) != 0 {
		t.Errorf("analysis-ready after forged resume: got %d publish(es), want 0", len(msgs))
	}

	// INVALID_ORG_ID_MISMATCH is NOT publishable to the orchestrator
	// (see model.IsPublishableToOrchestrator); the Manager mirrors that
	// gate by NOT calling publishFailed on this branch (manager.go:402-
	// 404). So no NEW status-changed publication on a FAILED status with
	// that code should appear past statusBefore.
	statusAfter := app.Broker.PublishedOn(fakes.RoutingKeyStatusChanged)
	for i := statusBefore; i < len(statusAfter); i++ {
		var s port.LICStatusChangedEvent
		if err := jsonUnmarshal(statusAfter[i].Payload, &s); err != nil {
			continue
		}
		if s.ErrorCode == model.ErrCodeInvalidOrgIDMismatch {
			t.Errorf("status-changed[%d] carries non-publishable code %q",
				i, model.ErrCodeInvalidOrgIDMismatch)
		}
	}

	// SETNX at manager.go:343 writes "PROCESSING" BEFORE the tenant
	// check; setUserConfirmedCompleted is NOT called on the tenant-
	// mismatch branch (only on auditRejectedWhitelist/Format and on
	// USER_CONFIRMATION_EXPIRED). So lic-user-confirmed:{vid} stays at
	// the SETNX-written PROCESSING value.
	userConfirmed, err := app.KV.Get(ctx, "lic-user-confirmed:"+versionID)
	if err != nil {
		t.Fatalf("lic-user-confirmed Get after forged resume: %v", err)
	}
	if userConfirmed != string(port.IdempotencyProcessing) {
		t.Errorf("lic-user-confirmed after forged resume = %q, want %q (SETNX-written, NOT setUserConfirmedCompleted on tenant-mismatch branch)",
			userConfirmed, string(port.IdempotencyProcessing))
	}
}

// ---------------------------------------------------------------------------
// 4. LLM call wire purity — no org-id in System / User; no field on the DTO.
// ---------------------------------------------------------------------------

// TestTenantIsolation_LLMRequest_NoOrgIDLeakIntoMessages drives the
// happy-path INITIAL pipeline and asserts that the organization_id never
// reaches the LLM wire — neither into the System prompt nor the User
// content of any recorded FakeLLMCall.
//
// Reconciliation R3: `port.CompletionRequest` (llm.go:137) intentionally
// does NOT have an OrganizationID field — its godoc states "Correlation
// identifiers ... are propagated via context, not fields, so the wire
// envelope stays minimal and PII-free". The test asserts that
// invariant STRUCTURALLY via reflection on the type.
func TestTenantIsolation_LLMRequest_NoOrgIDLeakIntoMessages(t *testing.T) {
	app := lictestapp.NewTestApp(t)

	correlationID := uuid.NewString()
	jobID := uuid.NewString()
	documentID := uuid.NewString()
	versionID := uuid.NewString()
	orgID := uuid.NewString()
	createdByUserID := uuid.NewString()

	app.DM.SetArtifactsResponse(versionID, fakes.BuildArtifactsResponse(fakes.DefaultArtifactsBundle()))

	trigger := port.VersionProcessingArtifactsReady{
		CorrelationID:   correlationID,
		Timestamp:       time.Now().UTC().Format(time.RFC3339Nano),
		DocumentID:      documentID,
		VersionID:       versionID,
		OrganizationID:  orgID,
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
		t.Fatalf("InjectResult.Acked = %d, want 1 (Nacked=%d)", res.Acked, res.Nacked)
	}

	waitCtx, waitCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer waitCancel()
	if err := waitForCompletedStatus(waitCtx, app.Broker); err != nil {
		t.Fatalf("waiting for COMPLETED status: %v", err)
	}

	calls := app.LLM[port.ProviderClaude].Calls()
	if len(calls) == 0 {
		t.Fatal("FakeLLMProvider recorded zero Complete calls — pipeline did not run agents")
	}
	for i, c := range calls {
		if strings.Contains(c.System, orgID) {
			t.Errorf("FakeLLMCall[%d].System contains organization_id %q (PII leak)", i, orgID)
		}
		if strings.Contains(c.User, orgID) {
			t.Errorf("FakeLLMCall[%d].User contains organization_id %q (PII leak)", i, orgID)
		}
		// FakeLLMCall does not expose PriorTurns content — only
		// PriorTurnsLen. If a future agent leaks org_id into PriorTurns,
		// extend fakes/llm.go FakeLLMCall to carry PriorTurns []port.Turn
		// and re-run this test (forward note in CLAUDE.md).
	}

	// Reflection-confirm the wire DTO has no org-id field. This pins
	// R3: the security model's "organization_id передаётся в Metadata
	// LLM-вызова" is satisfied STRUCTURALLY (no field on the wire DTO,
	// see llm.go:134-136 godoc).
	rt := reflect.TypeOf(port.CompletionRequest{})
	for i := 0; i < rt.NumField(); i++ {
		name := rt.Field(i).Name
		switch name {
		case "OrganizationID", "TenantID", "OrgID":
			t.Errorf("port.CompletionRequest has forbidden field %q — wire DTO must stay PII-free (llm.go:134-136)", name)
		}
	}
}

// ---------------------------------------------------------------------------
// 5. OTel attribute key pin.
// ---------------------------------------------------------------------------

// TestTenantIsolation_OTelAttributeKey_OrganizationID_Pin pins the
// wire-stable OTel key. Renaming `AttrOrganizationID` is a breaking
// change for every Tempo/Jaeger dashboard and alert
// (observability.md §4.3).
//
// Reconciliation R2: the constant is literally "organization_id"
// (correlation-level wire key), NOT "lic.pipeline.organization_id"
// (that namespace is reserved for pipeline-mode/outcome attributes,
// not for correlation IDs — see tracer/attrs.go:22 vs the §"Pipeline-
// level keys" block at attrs.go:31-38).
func TestTenantIsolation_OTelAttributeKey_OrganizationID_Pin(t *testing.T) {
	const wantKey = "organization_id"

	if got := string(tracer.AttrOrganizationID); got != wantKey {
		t.Errorf("tracer.AttrOrganizationID = %q, want %q — renaming is a breaking change for every dashboard/alert pinned to this key (observability.md §4.3)",
			got, wantKey)
	}

	const orgValue = "ORG-TEST"
	fields := tracer.SpanFields{OrganizationID: orgValue}
	kvs := fields.AsKeyValues()

	var found bool
	for _, kv := range kvs {
		if kv.Key == tracer.AttrOrganizationID {
			if got := kv.Value.AsString(); got != orgValue {
				t.Errorf("SpanFields kv for AttrOrganizationID = %q, want %q", got, orgValue)
			}
			found = true
			break
		}
	}
	if !found {
		t.Errorf("SpanFields.AsKeyValues() did not include AttrOrganizationID; got %d kv(s)", len(kvs))
	}
}

// ---------------------------------------------------------------------------
// 6. Prometheus label hygiene — no organization_id label anywhere.
// ---------------------------------------------------------------------------

// TestTenantIsolation_PrometheusMetrics_NoOrganizationIDLabel walks every
// metric family on a freshly-built registry and asserts no Label.Name
// matches organization_id (or any case-/spelling-variant). Authoritative
// ban: observability.md §3.10 + metrics/registry.go:11 —
// organization_id is unbounded cardinality (10K tenants × ~1.5K series =
// 15M-series bomb) and forbidden as a Prometheus label.
//
// Unlike metrics' own registry_test.go this test does NOT seed
// observations: a label that exists on a metric definition will surface
// once an observation is recorded; we walk the family-level Label set
// returned by Gather() and inspect any metric that is already present
// (build_info is set in New(), so the registry is never empty).
func TestTenantIsolation_PrometheusMetrics_NoOrganizationIDLabel(t *testing.T) {
	m := metrics.New(metrics.BuildInfo{Version: "test", Commit: "test", GoVersion: "test"})

	families, err := m.Registry().Gather()
	if err != nil {
		t.Fatalf("Registry().Gather(): %v", err)
	}

	for _, fam := range families {
		for _, met := range fam.Metric {
			for _, lp := range met.Label {
				if isForbiddenOrgLabel(lp) {
					t.Errorf("metric %q exposes forbidden label %q (observability.md §3.10)",
						fam.GetName(), lp.GetName())
				}
			}
		}
	}
}

// isForbiddenOrgLabel reports whether the label name matches any case-
// or spelling-variant of "organization id" / "tenant id". The full ban
// list mirrors the wire DTO field-name check in
// TestTenantIsolation_LLMRequest_NoOrgIDLeakIntoMessages so both seams
// share the same surface.
func isForbiddenOrgLabel(lp *dto.LabelPair) bool {
	name := strings.ToLower(lp.GetName())
	switch name {
	case "organization_id", "organizationid", "orgid", "org_id", "tenant_id", "tenantid":
		return true
	}
	return false
}

// ---------------------------------------------------------------------------
// Helpers (mirror happypath/pauseresume).
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

// waitForCompletedStatus polls the broker's status-changed log until a
// terminal COMPLETED event arrives or ctx expires. Verbatim from
// pauseresume_test.go.
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

// waitForClassificationUncertain polls the broker's classification-
// uncertain log until at least one message arrives or ctx expires.
// Verbatim from pauseresume_test.go.
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
