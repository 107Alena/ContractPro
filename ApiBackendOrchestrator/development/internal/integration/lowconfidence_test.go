package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"contractpro/api-orchestrator/internal/ingress/consumer"
	"contractpro/api-orchestrator/internal/ingress/middleware/auth"
)

// ---------------------------------------------------------------------------
// Test 1: Happy path — ClassificationUncertain → SSE type_confirmation_required
//         → POST /confirm-type → UserConfirmedType published → ANALYZING
// ---------------------------------------------------------------------------

func TestLowConfidenceTypeConfirmation(t *testing.T) {
	env := newTestEnv(t)

	docID := uuid.New().String()
	verID := uuid.New().String()
	jobID := uuid.New().String()

	// Pre-seed status to ANALYZING (precondition for classification-uncertain).
	env.SeedStatus(testOrgID, docID, verID, "ANALYZING")

	// Connect SSE client.
	token := env.jwtSigner.SignToken(testUserID, testOrgID, auth.RoleLawyer)
	sseClient, err := connectSSE(env.server.URL, token)
	if err != nil {
		t.Fatalf("connectSSE: %v", err)
	}
	defer sseClient.Close()

	if err := sseClient.WaitForConnected(3 * time.Second); err != nil {
		t.Fatalf("WaitForConnected: %v", err)
	}

	// Inject lic.events.classification-uncertain.
	uncertain := consumer.LICClassificationUncertainEvent{
		CorrelationID:  uuid.New().String(),
		Timestamp:      time.Now().UTC().Format(time.RFC3339),
		JobID:          jobID,
		DocumentID:     docID,
		VersionID:      verID,
		OrganizationID: testOrgID,
		SuggestedType:  "услуги",
		Confidence:     0.62,
		Threshold:      0.75,
		Alternatives: []consumer.ClassificationAlternative{
			{ContractType: "подряд", Confidence: 0.31},
		},
	}
	if err := env.InjectEvent("lic.events.classification-uncertain", mustJSON(t, uncertain)); err != nil {
		t.Fatalf("InjectEvent classification-uncertain: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	// SSE event 1: status_update AWAITING_USER_INPUT (from SetAwaitingUserInput).
	sseEvt1, err := sseClient.NextEvent(3 * time.Second)
	if err != nil {
		t.Fatalf("NextEvent (status_update AWAITING): %v", err)
	}
	if sseEvt1.EventType != "status_update" {
		t.Errorf("SSE event 1 type = %q, want status_update", sseEvt1.EventType)
	}
	var evt1Data map[string]any
	if err := json.Unmarshal([]byte(sseEvt1.Data), &evt1Data); err != nil {
		t.Fatalf("unmarshal SSE event 1: %v", err)
	}
	if evt1Data["status"] != "AWAITING_USER_INPUT" {
		t.Errorf("SSE event 1 status = %v, want AWAITING_USER_INPUT", evt1Data["status"])
	}

	// SSE event 2: type_confirmation_required with classification payload.
	sseEvt2, err := sseClient.NextEvent(3 * time.Second)
	if err != nil {
		t.Fatalf("NextEvent (type_confirmation_required): %v", err)
	}
	if sseEvt2.EventType != "type_confirmation_required" {
		t.Errorf("SSE event 2 type = %q, want type_confirmation_required", sseEvt2.EventType)
	}
	var evt2Data map[string]any
	if err := json.Unmarshal([]byte(sseEvt2.Data), &evt2Data); err != nil {
		t.Fatalf("unmarshal SSE event 2: %v", err)
	}
	if evt2Data["suggested_type"] != "услуги" {
		t.Errorf("SSE suggested_type = %v, want услуги", evt2Data["suggested_type"])
	}
	if evt2Data["confidence"] != 0.62 {
		t.Errorf("SSE confidence = %v, want 0.62", evt2Data["confidence"])
	}
	if evt2Data["threshold"] != 0.75 {
		t.Errorf("SSE threshold = %v, want 0.75", evt2Data["threshold"])
	}

	// Verify Redis: status = AWAITING_USER_INPUT.
	statusKey := "status:" + testOrgID + ":" + docID + ":" + verID
	raw, err := env.kvStore.Get(context.Background(), statusKey)
	if err != nil {
		t.Fatalf("kvStore.Get status: %v", err)
	}
	var statusRec struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal([]byte(raw), &statusRec); err != nil {
		t.Fatalf("unmarshal status: %v", err)
	}
	if statusRec.Status != "AWAITING_USER_INPUT" {
		t.Errorf("Redis status = %s, want AWAITING_USER_INPUT", statusRec.Status)
	}

	// Verify confirmation:wait:{verID} exists.
	if _, err := env.kvStore.Get(context.Background(), "confirmation:wait:"+verID); err != nil {
		t.Errorf("confirmation:wait key not found: %v", err)
	}

	// Verify confirmation:meta:{verID} exists with correct fields.
	metaRaw, err := env.kvStore.Get(context.Background(), "confirmation:meta:"+verID)
	if err != nil {
		t.Fatalf("confirmation:meta key not found: %v", err)
	}
	var meta struct {
		OrganizationID string `json:"organization_id"`
		DocumentID     string `json:"document_id"`
		VersionID      string `json:"version_id"`
		JobID          string `json:"job_id"`
	}
	if err := json.Unmarshal([]byte(metaRaw), &meta); err != nil {
		t.Fatalf("unmarshal meta: %v", err)
	}
	if meta.OrganizationID != testOrgID {
		t.Errorf("meta.OrganizationID = %s, want %s", meta.OrganizationID, testOrgID)
	}
	if meta.DocumentID != docID {
		t.Errorf("meta.DocumentID = %s, want %s", meta.DocumentID, docID)
	}
	if meta.JobID != jobID {
		t.Errorf("meta.JobID = %s, want %s", meta.JobID, jobID)
	}

	// POST /confirm-type.
	confirmBody := mustJSON(t, map[string]any{
		"contract_type":    "услуги",
		"confirmed_by_user": true,
	})
	resp := env.DoRequest(
		http.MethodPost,
		"/api/v1/contracts/"+docID+"/versions/"+verID+"/confirm-type",
		jsonReader(confirmBody),
		testUserID, testOrgID, auth.RoleLawyer,
	)
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("POST /confirm-type: expected 202, got %d: %s", resp.StatusCode, body)
	}

	var confirmResp struct {
		ContractID string `json:"contract_id"`
		VersionID  string `json:"version_id"`
		Status     string `json:"status"`
		Message    string `json:"message"`
	}
	if err := json.Unmarshal(body, &confirmResp); err != nil {
		t.Fatalf("unmarshal confirm response: %v", err)
	}
	if confirmResp.Status != "ANALYZING" {
		t.Errorf("confirm response status = %s, want ANALYZING", confirmResp.Status)
	}
	if confirmResp.ContractID != docID {
		t.Errorf("confirm response contract_id = %s, want %s", confirmResp.ContractID, docID)
	}

	// Verify orch.commands.user-confirmed-type published.
	msgs := env.brokerFake.PublishedMessages()
	found := false
	for _, m := range msgs {
		if m.Topic == "orch.commands.user-confirmed-type" {
			found = true
			var cmd map[string]any
			if err := json.Unmarshal(m.Payload, &cmd); err != nil {
				t.Fatalf("unmarshal published UserConfirmedType: %v", err)
			}
			if cmd["document_id"] != docID {
				t.Errorf("published document_id = %v, want %s", cmd["document_id"], docID)
			}
			if cmd["version_id"] != verID {
				t.Errorf("published version_id = %v, want %s", cmd["version_id"], verID)
			}
			if cmd["contract_type"] != "услуги" {
				t.Errorf("published contract_type = %v, want услуги", cmd["contract_type"])
			}
			if cmd["confirmed_by_user_id"] != testUserID {
				t.Errorf("published confirmed_by_user_id = %v, want %s", cmd["confirmed_by_user_id"], testUserID)
			}
			if cmd["organization_id"] != testOrgID {
				t.Errorf("published organization_id = %v, want %s", cmd["organization_id"], testOrgID)
			}
			break
		}
	}
	if !found {
		t.Fatal("UserConfirmedType not found in published messages")
	}

	// Verify Redis status = ANALYZING after confirmation.
	raw, err = env.kvStore.Get(context.Background(), statusKey)
	if err != nil {
		t.Fatalf("kvStore.Get status after confirm: %v", err)
	}
	if err := json.Unmarshal([]byte(raw), &statusRec); err != nil {
		t.Fatalf("unmarshal status after confirm: %v", err)
	}
	if statusRec.Status != "ANALYZING" {
		t.Errorf("Redis status after confirm = %s, want ANALYZING", statusRec.Status)
	}

	// Verify confirmation:wait:{verID} deleted.
	if _, err := env.kvStore.Get(context.Background(), "confirmation:wait:"+verID); err == nil {
		t.Error("confirmation:wait key should be deleted after confirm")
	}

	// SSE event 3: status_update ANALYZING (from ConfirmType).
	time.Sleep(100 * time.Millisecond)
	sseEvt3, err := sseClient.NextEvent(3 * time.Second)
	if err != nil {
		t.Fatalf("NextEvent (status_update ANALYZING): %v", err)
	}
	if sseEvt3.EventType != "status_update" {
		t.Errorf("SSE event 3 type = %q, want status_update", sseEvt3.EventType)
	}
	var evt3Data map[string]any
	if err := json.Unmarshal([]byte(sseEvt3.Data), &evt3Data); err != nil {
		t.Fatalf("unmarshal SSE event 3: %v", err)
	}
	if evt3Data["status"] != "ANALYZING" {
		t.Errorf("SSE event 3 status = %v, want ANALYZING", evt3Data["status"])
	}

	// Verify idempotency: second POST /confirm-type returns 202 without
	// publishing a duplicate command.
	msgCountBefore := len(env.brokerFake.PublishedMessages())
	resp2 := env.DoRequest(
		http.MethodPost,
		"/api/v1/contracts/"+docID+"/versions/"+verID+"/confirm-type",
		jsonReader(confirmBody),
		testUserID, testOrgID, auth.RoleLawyer,
	)
	body2 := readBody(t, resp2)
	if resp2.StatusCode != http.StatusAccepted {
		t.Fatalf("idempotent POST /confirm-type: expected 202, got %d: %s", resp2.StatusCode, body2)
	}
	msgCountAfter := len(env.brokerFake.PublishedMessages())
	if msgCountAfter != msgCountBefore {
		t.Errorf("idempotent POST should not publish: before=%d, after=%d", msgCountBefore, msgCountAfter)
	}
}

// ---------------------------------------------------------------------------
// Test 2: Wrong state — POST /confirm-type for version not in AWAITING_USER_INPUT
// ---------------------------------------------------------------------------

func TestLowConfidenceWrongState(t *testing.T) {
	env := newTestEnv(t)

	docID := uuid.New().String()
	verID := uuid.New().String()

	// Seed status to ANALYZING (not AWAITING_USER_INPUT).
	env.SeedStatus(testOrgID, docID, verID, "ANALYZING")

	// Seed confirmation:meta so the handler passes the metadata check and
	// reaches the ConfirmType call where the status mismatch triggers 409.
	meta := map[string]string{
		"organization_id": testOrgID,
		"document_id":     docID,
		"version_id":      verID,
		"job_id":          uuid.New().String(),
	}
	metaJSON, _ := json.Marshal(meta)
	env.kvStore.SetDirect("confirmation:meta:"+verID, string(metaJSON))

	confirmBody := mustJSON(t, map[string]any{
		"contract_type":    "услуги",
		"confirmed_by_user": true,
	})
	resp := env.DoRequest(
		http.MethodPost,
		"/api/v1/contracts/"+docID+"/versions/"+verID+"/confirm-type",
		jsonReader(confirmBody),
		testUserID, testOrgID, auth.RoleLawyer,
	)
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", resp.StatusCode, body)
	}

	var errResp struct {
		ErrorCode string `json:"error_code"`
	}
	if err := json.Unmarshal(body, &errResp); err != nil {
		t.Fatalf("unmarshal error response: %v", err)
	}
	if errResp.ErrorCode != "VERSION_NOT_AWAITING_INPUT" {
		t.Errorf("error_code = %s, want VERSION_NOT_AWAITING_INPUT", errResp.ErrorCode)
	}

	// Verify no command published.
	msgs := env.brokerFake.PublishedMessages()
	for _, m := range msgs {
		if m.Topic == "orch.commands.user-confirmed-type" {
			t.Fatal("UserConfirmedType should NOT be published for wrong state")
		}
	}

	// Verify status unchanged after rejected confirmation.
	statusKey := "status:" + testOrgID + ":" + docID + ":" + verID
	raw, err := env.kvStore.Get(context.Background(), statusKey)
	if err != nil {
		t.Fatalf("kvStore.Get status: %v", err)
	}
	var statusRec struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal([]byte(raw), &statusRec); err != nil {
		t.Fatalf("unmarshal status: %v", err)
	}
	if statusRec.Status != "ANALYZING" {
		t.Errorf("status after 409 = %s, want ANALYZING (unchanged)", statusRec.Status)
	}
}

// ---------------------------------------------------------------------------
// Test 3: Timeout — version stays in AWAITING_USER_INPUT, watchdog fires FAILED
// ---------------------------------------------------------------------------

func TestLowConfidenceTimeout(t *testing.T) {
	env := newTestEnv(t)

	docID := uuid.New().String()
	verID := uuid.New().String()
	jobID := uuid.New().String()

	// Pre-seed status to ANALYZING.
	env.SeedStatus(testOrgID, docID, verID, "ANALYZING")

	// Inject classification-uncertain event → AWAITING_USER_INPUT.
	uncertain := consumer.LICClassificationUncertainEvent{
		CorrelationID:  uuid.New().String(),
		Timestamp:      time.Now().UTC().Format(time.RFC3339),
		JobID:          jobID,
		DocumentID:     docID,
		VersionID:      verID,
		OrganizationID: testOrgID,
		SuggestedType:  "аренда",
		Confidence:     0.55,
		Threshold:      0.75,
	}
	if err := env.InjectEvent("lic.events.classification-uncertain", mustJSON(t, uncertain)); err != nil {
		t.Fatalf("InjectEvent: %v", err)
	}

	// Verify status = AWAITING_USER_INPUT.
	statusKey := "status:" + testOrgID + ":" + docID + ":" + verID
	raw, err := env.kvStore.Get(context.Background(), statusKey)
	if err != nil {
		t.Fatalf("kvStore.Get status: %v", err)
	}
	var statusRec struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal([]byte(raw), &statusRec); err != nil {
		t.Fatalf("unmarshal status: %v", err)
	}
	if statusRec.Status != "AWAITING_USER_INPUT" {
		t.Fatalf("status = %s, want AWAITING_USER_INPUT", statusRec.Status)
	}

	// Connect SSE client (after injection, before timeout).
	token := env.jwtSigner.SignToken(testUserID, testOrgID, auth.RoleLawyer)
	sseClient, err := connectSSE(env.server.URL, token)
	if err != nil {
		t.Fatalf("connectSSE: %v", err)
	}
	defer sseClient.Close()

	if err := sseClient.WaitForConnected(3 * time.Second); err != nil {
		t.Fatalf("WaitForConnected: %v", err)
	}

	// Simulate watchdog timeout by directly calling TimeoutAwaitingInput.
	// In production, the watchdog detects the expired confirmation:wait key and
	// calls this method. We test the full timeout-to-SSE path here; the
	// watchdog's key expiration detection is covered by its unit tests.
	if err := env.tracker.TimeoutAwaitingInput(context.Background(), testOrgID, docID, verID); err != nil {
		t.Fatalf("TimeoutAwaitingInput: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	// SSE event: status_update FAILED with USER_CONFIRMATION_TIMEOUT.
	sseEvt, err := sseClient.NextEvent(3 * time.Second)
	if err != nil {
		t.Fatalf("NextEvent: %v", err)
	}
	if sseEvt.EventType != "status_update" {
		t.Errorf("SSE event type = %q, want status_update", sseEvt.EventType)
	}

	var sseData map[string]any
	if err := json.Unmarshal([]byte(sseEvt.Data), &sseData); err != nil {
		t.Fatalf("unmarshal SSE data: %v", err)
	}
	if sseData["status"] != "FAILED" {
		t.Errorf("SSE status = %v, want FAILED", sseData["status"])
	}
	if sseData["error_code"] != "USER_CONFIRMATION_TIMEOUT" {
		t.Errorf("SSE error_code = %v, want USER_CONFIRMATION_TIMEOUT", sseData["error_code"])
	}

	// Verify Redis status = FAILED.
	raw, err = env.kvStore.Get(context.Background(), statusKey)
	if err != nil {
		t.Fatalf("kvStore.Get status after timeout: %v", err)
	}
	if err := json.Unmarshal([]byte(raw), &statusRec); err != nil {
		t.Fatalf("unmarshal status after timeout: %v", err)
	}
	if statusRec.Status != "FAILED" {
		t.Errorf("Redis status after timeout = %s, want FAILED", statusRec.Status)
	}
}

// ---------------------------------------------------------------------------
// Test 4: RBAC — BUSINESS_USER cannot confirm type
// ---------------------------------------------------------------------------

func TestLowConfidenceRBAC(t *testing.T) {
	env := newTestEnv(t)

	docID := uuid.New().String()
	verID := uuid.New().String()

	// No status/metadata seeding: RBAC middleware rejects before handler executes.
	confirmBody := mustJSON(t, map[string]any{
		"contract_type":    "услуги",
		"confirmed_by_user": true,
	})
	resp := env.DoRequest(
		http.MethodPost,
		"/api/v1/contracts/"+docID+"/versions/"+verID+"/confirm-type",
		jsonReader(confirmBody),
		testUserID, testOrgID, auth.RoleBusinessUser,
	)
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", resp.StatusCode, body)
	}

	var errResp struct {
		ErrorCode string `json:"error_code"`
	}
	if err := json.Unmarshal(body, &errResp); err != nil {
		t.Fatalf("unmarshal error response: %v", err)
	}
	if errResp.ErrorCode != "PERMISSION_DENIED" {
		t.Errorf("error_code = %s, want PERMISSION_DENIED", errResp.ErrorCode)
	}

	// Verify no command published.
	msgs := env.brokerFake.PublishedMessages()
	for _, m := range msgs {
		if m.Topic == "orch.commands.user-confirmed-type" {
			t.Fatal("UserConfirmedType should NOT be published for BUSINESS_USER")
		}
	}
}
