package integration

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"contractpro/api-orchestrator/internal/egress/dmclient"
	"contractpro/api-orchestrator/internal/ingress/consumer"
	"contractpro/api-orchestrator/internal/ingress/middleware/auth"
)

// fakePDF is a minimal byte sequence that starts with the PDF magic bytes
// (%PDF-) and is large enough to pass the upload handler's size checks.
var fakePDF = append([]byte("%PDF-1.4 fake content"), make([]byte, 100)...)

// test constants reused across scenarios.
var (
	testUserID = uuid.New().String()
	testOrgID  = uuid.New().String()
)

// mustJSON marshals v to JSON or panics.
func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("mustJSON: %v", err)
	}
	return data
}

// readBody reads and closes the response body.
func readBody(t *testing.T, resp *http.Response) []byte {
	t.Helper()
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("readBody: %v", err)
	}
	return data
}

// ---------------------------------------------------------------------------
// Test 1: Upload → ProcessDocumentRequested published → StatusChanged → SSE push
// ---------------------------------------------------------------------------

func TestUploadToSSEPipeline(t *testing.T) {
	env := newTestEnv(t)

	// 1. Connect SSE client BEFORE upload (simulates browser already connected).
	token := env.jwtSigner.SignToken(testUserID, testOrgID, auth.RoleLawyer)
	sse, err := connectSSE(env.server.URL, token)
	if err != nil {
		t.Fatalf("connectSSE: %v", err)
	}
	defer sse.Close()

	if err := sse.WaitForConnected(3 * time.Second); err != nil {
		t.Fatalf("WaitForConnected: %v", err)
	}

	// 2. Upload contract.
	resp := env.UploadContract("Test Contract", fakePDF, testUserID, testOrgID)
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", resp.StatusCode, body)
	}

	var uploadResp struct {
		ContractID    string `json:"contract_id"`
		VersionID     string `json:"version_id"`
		VersionNumber int    `json:"version_number"`
		JobID         string `json:"job_id"`
		Status        string `json:"status"`
	}
	if err := json.Unmarshal(body, &uploadResp); err != nil {
		t.Fatalf("unmarshal upload response: %v", err)
	}

	if uploadResp.ContractID == "" {
		t.Fatal("contract_id is empty")
	}
	if uploadResp.VersionID == "" {
		t.Fatal("version_id is empty")
	}
	if uploadResp.JobID == "" {
		t.Fatal("job_id is empty")
	}
	if uploadResp.Status != "UPLOADED" {
		t.Fatalf("expected status UPLOADED, got %s", uploadResp.Status)
	}

	// 3. Assert ProcessDocumentRequested was published to the broker.
	msgs := env.brokerFake.PublishedMessages()
	if len(msgs) == 0 {
		t.Fatal("expected at least one published message, got 0")
	}
	found := false
	for _, m := range msgs {
		if m.Topic == "dp.commands.process-document" {
			found = true
			var cmd map[string]any
			if err := json.Unmarshal(m.Payload, &cmd); err != nil {
				t.Fatalf("unmarshal published command: %v", err)
			}
			if cmd["document_id"] != uploadResp.ContractID {
				t.Errorf("published document_id = %v, want %s", cmd["document_id"], uploadResp.ContractID)
			}
			if cmd["version_id"] != uploadResp.VersionID {
				t.Errorf("published version_id = %v, want %s", cmd["version_id"], uploadResp.VersionID)
			}
			break
		}
	}
	if !found {
		t.Fatal("ProcessDocumentRequested not found in published messages")
	}

	// 4. Assert S3 object was uploaded.
	s3Has := false
	for key := range env.s3Fake.objects {
		s3Has = true
		_ = key
		break
	}
	if !s3Has {
		t.Fatal("expected at least one S3 object, got 0")
	}

	// 5. Inject DP status-changed IN_PROGRESS event (simulates DP processing).
	dpEvent := consumer.DPStatusChangedEvent{
		CorrelationID:  uuid.New().String(),
		Timestamp:      time.Now().UTC().Format(time.RFC3339),
		JobID:          uploadResp.JobID,
		DocumentID:     uploadResp.ContractID,
		VersionID:      uploadResp.VersionID,
		OrganizationID: testOrgID,
		Status:         "IN_PROGRESS",
	}
	if err := env.InjectEvent("dp.events.status-changed", mustJSON(t, dpEvent)); err != nil {
		t.Fatalf("InjectEvent: %v", err)
	}

	// Small delay to let Pub/Sub deliver the event to the SSE handler.
	time.Sleep(100 * time.Millisecond)

	// 6. Read SSE event.
	sseEvt, err := sse.NextEvent(3 * time.Second)
	if err != nil {
		t.Fatalf("NextEvent: %v", err)
	}
	if sseEvt.EventType != "status_update" {
		t.Errorf("SSE event_type = %q, want status_update", sseEvt.EventType)
	}

	var sseData map[string]any
	if err := json.Unmarshal([]byte(sseEvt.Data), &sseData); err != nil {
		t.Fatalf("unmarshal SSE data: %v", err)
	}
	if sseData["status"] != "PROCESSING" {
		t.Errorf("SSE status = %v, want PROCESSING", sseData["status"])
	}
	if sseData["document_id"] != uploadResp.ContractID {
		t.Errorf("SSE document_id = %v, want %s", sseData["document_id"], uploadResp.ContractID)
	}

	// 7. Verify Redis status.
	statusKey := "status:" + testOrgID + ":" + uploadResp.ContractID + ":" + uploadResp.VersionID
	raw, err := env.kvStore.Get(nil, statusKey)
	if err != nil {
		t.Fatalf("kvStore.Get status: %v", err)
	}
	var statusRec struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal([]byte(raw), &statusRec); err != nil {
		t.Fatalf("unmarshal status record: %v", err)
	}
	if statusRec.Status != "PROCESSING" {
		t.Errorf("Redis status = %s, want PROCESSING", statusRec.Status)
	}
}

// ---------------------------------------------------------------------------
// Test 2: Recheck → new version created → command published
// ---------------------------------------------------------------------------

func TestRecheckPipeline(t *testing.T) {
	env := newTestEnv(t)

	docID := uuid.New().String()
	verID := uuid.New().String()

	// Seed a document and version in the fake DM.
	env.dmFake.SeedDocument(&dmclient.Document{
		DocumentID:     docID,
		OrganizationID: testOrgID,
		Title:          "Existing Contract",
		Status:         "ACTIVE",
		CreatedAt:      time.Now().UTC(),
		UpdatedAt:      time.Now().UTC(),
	})
	env.dmFake.SeedVersion(docID, &dmclient.DocumentVersionWithArtifacts{
		DocumentVersion: dmclient.DocumentVersion{
			VersionID:          verID,
			DocumentID:         docID,
			VersionNumber:      1,
			OriginType:         "UPLOAD",
			SourceFileKey:      "uploads/org/job/file.pdf",
			SourceFileName:     "contract.pdf",
			SourceFileSize:     12345,
			SourceFileChecksum: "abc123",
			ArtifactStatus:     "FULLY_READY",
			CreatedAt:          time.Now().UTC(),
		},
		Artifacts: []dmclient.ArtifactDescriptor{},
	})

	// POST /api/v1/contracts/{id}/versions/{vid}/recheck
	resp := env.DoRequest(
		http.MethodPost,
		"/api/v1/contracts/"+docID+"/versions/"+verID+"/recheck",
		nil,
		testUserID, testOrgID, auth.RoleLawyer,
	)
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", resp.StatusCode, body)
	}

	var recheckResp struct {
		ContractID    string `json:"contract_id"`
		VersionID     string `json:"version_id"`
		VersionNumber int    `json:"version_number"`
		JobID         string `json:"job_id"`
		Status        string `json:"status"`
	}
	if err := json.Unmarshal(body, &recheckResp); err != nil {
		t.Fatalf("unmarshal recheck response: %v", err)
	}

	if recheckResp.ContractID != docID {
		t.Errorf("contract_id = %s, want %s", recheckResp.ContractID, docID)
	}
	if recheckResp.VersionID == "" || recheckResp.VersionID == verID {
		t.Errorf("expected NEW version_id, got %s (original: %s)", recheckResp.VersionID, verID)
	}
	if recheckResp.Status != "UPLOADED" {
		t.Errorf("status = %s, want UPLOADED", recheckResp.Status)
	}

	// Assert ProcessDocumentRequested was published.
	msgs := env.brokerFake.PublishedMessages()
	found := false
	for _, m := range msgs {
		if m.Topic == "dp.commands.process-document" {
			found = true
			var cmd map[string]any
			if err := json.Unmarshal(m.Payload, &cmd); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if cmd["version_id"] != recheckResp.VersionID {
				t.Errorf("published version_id = %v, want %s", cmd["version_id"], recheckResp.VersionID)
			}
			break
		}
	}
	if !found {
		t.Fatal("ProcessDocumentRequested not published after recheck")
	}
}

// ---------------------------------------------------------------------------
// Test 3: Compare → CompareDocumentVersionsRequested published
// ---------------------------------------------------------------------------

func TestComparePipeline(t *testing.T) {
	env := newTestEnv(t)

	docID := uuid.New().String()
	baseVerID := uuid.New().String()
	targetVerID := uuid.New().String()

	// Seed document and two versions.
	env.dmFake.SeedDocument(&dmclient.Document{
		DocumentID:     docID,
		OrganizationID: testOrgID,
		Title:          "Comparison Contract",
		Status:         "ACTIVE",
		CreatedAt:      time.Now().UTC(),
		UpdatedAt:      time.Now().UTC(),
	})

	seedVersion := func(verID string, num int) {
		env.dmFake.SeedVersion(docID, &dmclient.DocumentVersionWithArtifacts{
			DocumentVersion: dmclient.DocumentVersion{
				VersionID:      verID,
				DocumentID:     docID,
				VersionNumber:  num,
				ArtifactStatus: "FULLY_READY",
				CreatedAt:      time.Now().UTC(),
			},
			Artifacts: []dmclient.ArtifactDescriptor{},
		})
	}
	seedVersion(baseVerID, 1)
	seedVersion(targetVerID, 2)

	// POST /api/v1/contracts/{id}/compare
	compareBody := mustJSON(t, map[string]string{
		"base_version_id":   baseVerID,
		"target_version_id": targetVerID,
	})

	resp := env.DoRequest(
		http.MethodPost,
		"/api/v1/contracts/"+docID+"/compare",
		jsonReader(compareBody),
		testUserID, testOrgID, auth.RoleLawyer,
	)
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", resp.StatusCode, body)
	}

	var compareResp struct {
		ContractID      string `json:"contract_id"`
		JobID           string `json:"job_id"`
		BaseVersionID   string `json:"base_version_id"`
		TargetVersionID string `json:"target_version_id"`
		Status          string `json:"status"`
	}
	if err := json.Unmarshal(body, &compareResp); err != nil {
		t.Fatalf("unmarshal compare response: %v", err)
	}

	if compareResp.Status != "COMPARISON_QUEUED" {
		t.Errorf("status = %s, want COMPARISON_QUEUED", compareResp.Status)
	}
	if compareResp.BaseVersionID != baseVerID {
		t.Errorf("base_version_id = %s, want %s", compareResp.BaseVersionID, baseVerID)
	}
	if compareResp.TargetVersionID != targetVerID {
		t.Errorf("target_version_id = %s, want %s", compareResp.TargetVersionID, targetVerID)
	}

	// Assert CompareDocumentVersionsRequested was published.
	msgs := env.brokerFake.PublishedMessages()
	found := false
	for _, m := range msgs {
		if m.Topic == "dp.commands.compare-versions" {
			found = true
			var cmd map[string]any
			if err := json.Unmarshal(m.Payload, &cmd); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if cmd["base_version_id"] != baseVerID {
				t.Errorf("published base_version_id = %v, want %s", cmd["base_version_id"], baseVerID)
			}
			if cmd["target_version_id"] != targetVerID {
				t.Errorf("published target_version_id = %v, want %s", cmd["target_version_id"], targetVerID)
			}
			break
		}
	}
	if !found {
		t.Fatal("CompareDocumentVersionsRequested not published")
	}
}

// ---------------------------------------------------------------------------
// Test 4: LIC failure event → SSE push ANALYSIS_FAILED
// ---------------------------------------------------------------------------

func TestLICFailureToSSE(t *testing.T) {
	env := newTestEnv(t)

	docID := uuid.New().String()
	verID := uuid.New().String()
	jobID := uuid.New().String()

	// Pre-seed status to ANALYZING so the ANALYSIS_FAILED transition is valid.
	env.SeedStatus(testOrgID, docID, verID, "ANALYZING")

	// Connect SSE client.
	token := env.jwtSigner.SignToken(testUserID, testOrgID, auth.RoleLawyer)
	sse, err := connectSSE(env.server.URL, token)
	if err != nil {
		t.Fatalf("connectSSE: %v", err)
	}
	defer sse.Close()

	if err := sse.WaitForConnected(3 * time.Second); err != nil {
		t.Fatalf("WaitForConnected: %v", err)
	}

	// Inject LIC status-changed FAILED event.
	isRetryable := true
	licEvent := consumer.LICStatusChangedEvent{
		CorrelationID:  uuid.New().String(),
		Timestamp:      time.Now().UTC().Format(time.RFC3339),
		JobID:          jobID,
		DocumentID:     docID,
		VersionID:      verID,
		OrganizationID: testOrgID,
		Status:         "FAILED",
		ErrorCode:      "LIC_INTERNAL_ERROR",
		ErrorMessage:   "Model inference failed",
		IsRetryable:    &isRetryable,
	}
	if err := env.InjectEvent("lic.events.status-changed", mustJSON(t, licEvent)); err != nil {
		t.Fatalf("InjectEvent: %v", err)
	}

	// Small delay for Pub/Sub delivery.
	time.Sleep(100 * time.Millisecond)

	// Read SSE event.
	sseEvt, err := sse.NextEvent(3 * time.Second)
	if err != nil {
		t.Fatalf("NextEvent: %v", err)
	}
	if sseEvt.EventType != "status_update" {
		t.Errorf("SSE event_type = %q, want status_update", sseEvt.EventType)
	}

	var sseData map[string]any
	if err := json.Unmarshal([]byte(sseEvt.Data), &sseData); err != nil {
		t.Fatalf("unmarshal SSE data: %v", err)
	}
	if sseData["status"] != "ANALYSIS_FAILED" {
		t.Errorf("SSE status = %v, want ANALYSIS_FAILED", sseData["status"])
	}
	if sseData["error_code"] != "LIC_INTERNAL_ERROR" {
		t.Errorf("SSE error_code = %v, want LIC_INTERNAL_ERROR", sseData["error_code"])
	}
	if sseData["is_retryable"] != true {
		t.Errorf("SSE is_retryable = %v, want true", sseData["is_retryable"])
	}

	// Verify Redis status.
	statusKey := "status:" + testOrgID + ":" + docID + ":" + verID
	raw, err := env.kvStore.Get(nil, statusKey)
	if err != nil {
		t.Fatalf("kvStore.Get status: %v", err)
	}
	var statusRec struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal([]byte(raw), &statusRec); err != nil {
		t.Fatalf("unmarshal status record: %v", err)
	}
	if statusRec.Status != "ANALYSIS_FAILED" {
		t.Errorf("Redis status = %s, want ANALYSIS_FAILED", statusRec.Status)
	}
}

// ---------------------------------------------------------------------------
// Test 5: DM version-reports-ready → SSE push READY → GET /results → data
// ---------------------------------------------------------------------------

func TestReportsReadyToResults(t *testing.T) {
	env := newTestEnv(t)

	docID := uuid.New().String()
	verID := uuid.New().String()

	// Pre-seed status to GENERATING_REPORTS.
	env.SeedStatus(testOrgID, docID, verID, "GENERATING_REPORTS")

	// Seed DM document, version, and artifacts for the results endpoint.
	env.dmFake.SeedDocument(&dmclient.Document{
		DocumentID:     docID,
		OrganizationID: testOrgID,
		Title:          "Ready Contract",
		Status:         "ACTIVE",
		CreatedAt:      time.Now().UTC(),
		UpdatedAt:      time.Now().UTC(),
	})
	env.dmFake.SeedVersion(docID, &dmclient.DocumentVersionWithArtifacts{
		DocumentVersion: dmclient.DocumentVersion{
			VersionID:      verID,
			DocumentID:     docID,
			VersionNumber:  1,
			ArtifactStatus: "FULLY_READY",
			CreatedAt:      time.Now().UTC(),
		},
		Artifacts: []dmclient.ArtifactDescriptor{},
	})

	// Seed artifacts that the results handler will fetch.
	riskAnalysis := json.RawMessage(`{"risks":[{"level":"high","description":"Неустойка не ограничена"}]}`)
	summary := json.RawMessage(`{"summary":"Договор содержит повышенные риски"}`)
	aggregateScore := json.RawMessage(`{"score":35,"grade":"C"}`)

	env.dmFake.SeedArtifact(docID, verID, "RISK_ANALYSIS", riskAnalysis)
	env.dmFake.SeedArtifact(docID, verID, "SUMMARY", summary)
	env.dmFake.SeedArtifact(docID, verID, "AGGREGATE_SCORE", aggregateScore)

	// Connect SSE client.
	token := env.jwtSigner.SignToken(testUserID, testOrgID, auth.RoleLawyer)
	sse, err := connectSSE(env.server.URL, token)
	if err != nil {
		t.Fatalf("connectSSE: %v", err)
	}
	defer sse.Close()

	if err := sse.WaitForConnected(3 * time.Second); err != nil {
		t.Fatalf("WaitForConnected: %v", err)
	}

	// Inject DM version-reports-ready event.
	dmEvent := consumer.DMVersionReportsReadyEvent{
		CorrelationID:  uuid.New().String(),
		Timestamp:      time.Now().UTC().Format(time.RFC3339),
		DocumentID:     docID,
		VersionID:      verID,
		OrganizationID: testOrgID,
		ArtifactTypes:  []string{"EXPORT_PDF", "EXPORT_DOCX"},
	}
	if err := env.InjectEvent("dm.events.version-reports-ready", mustJSON(t, dmEvent)); err != nil {
		t.Fatalf("InjectEvent: %v", err)
	}

	// Small delay for Pub/Sub delivery.
	time.Sleep(100 * time.Millisecond)

	// Read SSE event — should be READY.
	sseEvt, err := sse.NextEvent(3 * time.Second)
	if err != nil {
		t.Fatalf("NextEvent: %v", err)
	}
	if sseEvt.EventType != "status_update" {
		t.Errorf("SSE event_type = %q, want status_update", sseEvt.EventType)
	}

	var sseData map[string]any
	if err := json.Unmarshal([]byte(sseEvt.Data), &sseData); err != nil {
		t.Fatalf("unmarshal SSE data: %v", err)
	}
	if sseData["status"] != "READY" {
		t.Errorf("SSE status = %v, want READY", sseData["status"])
	}

	// GET /results — should return aggregated data from DM.
	resp := env.DoRequest(
		http.MethodGet,
		"/api/v1/contracts/"+docID+"/versions/"+verID+"/results",
		nil,
		testUserID, testOrgID, auth.RoleLawyer,
	)
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /results: expected 200, got %d: %s", resp.StatusCode, body)
	}

	var resultsResp map[string]any
	if err := json.Unmarshal(body, &resultsResp); err != nil {
		t.Fatalf("unmarshal results response: %v", err)
	}

	// Check that the status is READY (not PROCESSING).
	if resultsResp["status"] != "READY" {
		t.Errorf("results status = %v, want READY", resultsResp["status"])
	}

	// Check that risks is present (fetched from DM as RISK_ANALYSIS → JSON "risks").
	if resultsResp["risks"] == nil {
		t.Error("risks is nil, expected fetched artifact data")
	}

	// Check that summary is present.
	if resultsResp["summary"] == nil {
		t.Error("summary is nil, expected fetched artifact data")
	}

	// Check that aggregate_score is present.
	if resultsResp["aggregate_score"] == nil {
		t.Error("aggregate_score is nil, expected fetched artifact data")
	}

	// Verify Redis status is READY.
	statusKey := "status:" + testOrgID + ":" + docID + ":" + verID
	raw, err := env.kvStore.Get(nil, statusKey)
	if err != nil {
		t.Fatalf("kvStore.Get status: %v", err)
	}
	var statusRec struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal([]byte(raw), &statusRec); err != nil {
		t.Fatalf("unmarshal status record: %v", err)
	}
	if statusRec.Status != "READY" {
		t.Errorf("Redis status = %s, want READY", statusRec.Status)
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// jsonReader creates an io.Reader from a JSON byte slice.
func jsonReader(data []byte) io.Reader {
	return strings.NewReader(string(data))
}
