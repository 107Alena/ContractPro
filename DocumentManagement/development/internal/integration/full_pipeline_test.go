package integration

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"contractpro/document-management/internal/domain/model"
)

// ---------------------------------------------------------------------------
// Test: Full pipeline DP → LIC → RE → artifact_status = FULLY_READY
// Verifies notifications, audit records, and artifact accumulation.
// ---------------------------------------------------------------------------

func TestFullPipeline_DPtoLICtoRE_FullyReady(t *testing.T) {
	const (
		orgID         = "org-fp-001"
		docID         = "doc-fp-001"
		versionID     = "ver-fp-001"
		dpJobID       = "job-dp-fp-001"
		licJobID      = "job-lic-fp-001"
		reJobID       = "job-re-fp-001"
		correlationID = "corr-fp-001"
	)

	h := newTestHarness(t)
	h.seedDocument(defaultDocument(orgID, docID))
	h.seedVersion(defaultVersion(orgID, docID, versionID))

	// ========== Stage 1: DP Artifacts ==========
	dpEvent := defaultDPEvent(orgID, docID, versionID, dpJobID, correlationID)
	if err := h.ingestion.HandleDPArtifacts(context.Background(), dpEvent); err != nil {
		t.Fatalf("HandleDPArtifacts failed: %v", err)
	}

	// Verify status after DP.
	ver, err := h.versionRepo.FindByID(context.Background(), orgID, docID, versionID)
	if err != nil {
		t.Fatalf("FindByID after DP: %v", err)
	}
	assertEqual(t, "status after DP",
		string(model.ArtifactStatusProcessingArtifactsReceived), string(ver.ArtifactStatus))
	assertIntEqual(t, "artifacts after DP", 4, len(h.artifactRepo.allArtifacts()))
	assertIntEqual(t, "outbox after DP", 2, len(h.outboxRepo.allEntries()))
	assertIntEqual(t, "audit after DP", 2, len(h.auditRepo.allRecords()))

	// ========== Stage 2: LIC Artifacts ==========
	licEvent := defaultLICEvent(orgID, docID, versionID, licJobID, correlationID)
	if err := h.ingestion.HandleLICArtifacts(context.Background(), licEvent); err != nil {
		t.Fatalf("HandleLICArtifacts failed: %v", err)
	}

	ver, err = h.versionRepo.FindByID(context.Background(), orgID, docID, versionID)
	if err != nil {
		t.Fatalf("FindByID after LIC: %v", err)
	}
	assertEqual(t, "status after LIC",
		string(model.ArtifactStatusAnalysisArtifactsReceived), string(ver.ArtifactStatus))
	assertIntEqual(t, "artifacts after LIC", 12, len(h.artifactRepo.allArtifacts()))
	assertIntEqual(t, "outbox after LIC", 4, len(h.outboxRepo.allEntries()))
	assertIntEqual(t, "audit after LIC", 4, len(h.auditRepo.allRecords()))

	// ========== Stage 3: RE Artifacts ==========
	reEvent := defaultREEvent(h, orgID, docID, versionID, reJobID, correlationID)
	if err := h.ingestion.HandleREArtifacts(context.Background(), reEvent); err != nil {
		t.Fatalf("HandleREArtifacts failed: %v", err)
	}

	ver, err = h.versionRepo.FindByID(context.Background(), orgID, docID, versionID)
	if err != nil {
		t.Fatalf("FindByID after RE: %v", err)
	}
	assertEqual(t, "status after RE (FULLY_READY)",
		string(model.ArtifactStatusFullyReady), string(ver.ArtifactStatus))
	assertIntEqual(t, "artifacts after RE", 14, len(h.artifactRepo.allArtifacts()))
	assertIntEqual(t, "outbox after RE", 6, len(h.outboxRepo.allEntries()))
	assertIntEqual(t, "audit after RE", 6, len(h.auditRepo.allRecords()))

	// ========== Verify outbox topics order ==========
	outboxEntries := h.outboxRepo.allEntries()
	expectedTopics := []string{
		model.TopicDMResponsesArtifactsPersisted,
		model.TopicDMEventsVersionArtifactsReady,
		model.TopicDMResponsesLICArtifactsPersisted,
		model.TopicDMEventsVersionAnalysisReady,
		model.TopicDMResponsesREReportsPersisted,
		model.TopicDMEventsVersionReportsReady,
	}
	for i, expected := range expectedTopics {
		if i >= len(outboxEntries) {
			t.Fatalf("missing outbox entry at index %d, expected topic %s", i, expected)
		}
		assertEqual(t, "outbox["+expected+"]", expected, outboxEntries[i].Topic)
	}

	// All outbox entries have aggregate_id = versionID.
	for _, entry := range outboxEntries {
		assertEqual(t, "outbox.AggregateID", versionID, entry.AggregateID)
	}

	// ========== Verify correlation_id propagation ==========
	for _, entry := range outboxEntries {
		var meta struct {
			CorrelationID string `json:"correlation_id"`
		}
		if err := json.Unmarshal(entry.Payload, &meta); err == nil {
			assertEqual(t, "outbox.CorrelationID for "+entry.Topic, correlationID, meta.CorrelationID)
		}
	}
}

// ---------------------------------------------------------------------------
// Test: ListArtifacts returns progressively more artifacts at each stage
// ---------------------------------------------------------------------------

func TestFullPipeline_ListArtifactsAtEachStage(t *testing.T) {
	const (
		orgID     = "org-fp-002"
		docID     = "doc-fp-002"
		versionID = "ver-fp-002"
	)

	h := newTestHarness(t)
	h.seedDocument(defaultDocument(orgID, docID))
	h.seedVersion(defaultVersion(orgID, docID, versionID))

	// Before any ingestion: 0 artifacts.
	artifacts, err := h.query.ListArtifacts(context.Background(), orgID, docID, versionID)
	if err != nil {
		t.Fatalf("ListArtifacts before ingestion: %v", err)
	}
	assertIntEqual(t, "artifacts before ingestion", 0, len(artifacts))

	// After DP: 4 artifacts (DP types).
	dpEvent := defaultDPEvent(orgID, docID, versionID, "job-dp-002", "corr-002")
	if err := h.ingestion.HandleDPArtifacts(context.Background(), dpEvent); err != nil {
		t.Fatalf("HandleDPArtifacts: %v", err)
	}

	artifacts, err = h.query.ListArtifacts(context.Background(), orgID, docID, versionID)
	if err != nil {
		t.Fatalf("ListArtifacts after DP: %v", err)
	}
	assertIntEqual(t, "artifacts after DP", 4, len(artifacts))
	verifyProducerDomains(t, artifacts, model.ProducerDomainDP, 4)

	// After LIC: 12 artifacts (4 DP + 8 LIC).
	licEvent := defaultLICEvent(orgID, docID, versionID, "job-lic-002", "corr-002")
	if err := h.ingestion.HandleLICArtifacts(context.Background(), licEvent); err != nil {
		t.Fatalf("HandleLICArtifacts: %v", err)
	}

	artifacts, err = h.query.ListArtifacts(context.Background(), orgID, docID, versionID)
	if err != nil {
		t.Fatalf("ListArtifacts after LIC: %v", err)
	}
	assertIntEqual(t, "artifacts after LIC", 12, len(artifacts))
	verifyProducerDomains(t, artifacts, model.ProducerDomainDP, 4)
	verifyProducerDomains(t, artifacts, model.ProducerDomainLIC, 8)

	// After RE: 14 artifacts (4 DP + 8 LIC + 2 RE).
	reEvent := defaultREEvent(h, orgID, docID, versionID, "job-re-002", "corr-002")
	if err := h.ingestion.HandleREArtifacts(context.Background(), reEvent); err != nil {
		t.Fatalf("HandleREArtifacts: %v", err)
	}

	artifacts, err = h.query.ListArtifacts(context.Background(), orgID, docID, versionID)
	if err != nil {
		t.Fatalf("ListArtifacts after RE: %v", err)
	}
	assertIntEqual(t, "artifacts after RE", 14, len(artifacts))
	verifyProducerDomains(t, artifacts, model.ProducerDomainDP, 4)
	verifyProducerDomains(t, artifacts, model.ProducerDomainLIC, 8)
	verifyProducerDomains(t, artifacts, model.ProducerDomainRE, 2)
}

// ---------------------------------------------------------------------------
// Test: Audit trail integrity across full pipeline
// ---------------------------------------------------------------------------

func TestFullPipeline_AuditTrailIntegrity(t *testing.T) {
	const (
		orgID     = "org-fp-003"
		docID     = "doc-fp-003"
		versionID = "ver-fp-003"
	)

	h := newTestHarness(t)
	h.seedDocument(defaultDocument(orgID, docID))
	h.seedVersion(defaultVersion(orgID, docID, versionID))

	// Run full pipeline.
	_ = h.ingestion.HandleDPArtifacts(context.Background(),
		defaultDPEvent(orgID, docID, versionID, "job-dp-003", "corr-003"))
	_ = h.ingestion.HandleLICArtifacts(context.Background(),
		defaultLICEvent(orgID, docID, versionID, "job-lic-003", "corr-003"))
	_ = h.ingestion.HandleREArtifacts(context.Background(),
		defaultREEvent(h, orgID, docID, versionID, "job-re-003", "corr-003"))

	auditRecords := h.auditRepo.allRecords()
	assertIntEqual(t, "total audit records", 6, len(auditRecords))

	// Verify status transitions in order.
	type transitionDetail struct {
		From string `json:"from"`
		To   string `json:"to"`
	}

	expectedTransitions := []transitionDetail{
		{From: "PENDING", To: "PROCESSING_ARTIFACTS_RECEIVED"},
		{From: "PROCESSING_ARTIFACTS_RECEIVED", To: "ANALYSIS_ARTIFACTS_RECEIVED"},
		{From: "ANALYSIS_ARTIFACTS_RECEIVED", To: "FULLY_READY"},
	}
	expectedProducers := []string{"DP", "LIC", "RE"}

	transitionIdx := 0
	for _, rec := range auditRecords {
		if rec.Action == model.AuditActionArtifactStatusChanged {
			if transitionIdx >= len(expectedTransitions) {
				t.Fatalf("unexpected extra ARTIFACT_STATUS_CHANGED record")
			}
			var detail transitionDetail
			if err := json.Unmarshal(rec.Details, &detail); err != nil {
				t.Fatalf("unmarshal status change detail: %v", err)
			}
			assertEqual(t, "transition.from", expectedTransitions[transitionIdx].From, detail.From)
			assertEqual(t, "transition.to", expectedTransitions[transitionIdx].To, detail.To)
			assertEqual(t, "actor_id", expectedProducers[transitionIdx], rec.ActorID)
			transitionIdx++
		}
		if rec.Action == model.AuditActionArtifactSaved {
			assertEqual(t, "audit.ActorType", string(model.ActorTypeDomain), string(rec.ActorType))
		}
	}
	assertIntEqual(t, "status transitions count", 3, transitionIdx)
}

// ---------------------------------------------------------------------------
// Test: Out-of-order LIC before DP fails (state machine enforcement)
// ---------------------------------------------------------------------------

func TestFullPipeline_OutOfOrder_LICBeforeDP_Fails(t *testing.T) {
	const (
		orgID     = "org-fp-004"
		docID     = "doc-fp-004"
		versionID = "ver-fp-004"
	)

	h := newTestHarness(t)
	h.seedDocument(defaultDocument(orgID, docID))
	h.seedVersion(defaultVersion(orgID, docID, versionID))

	// LIC before DP: invalid transition PENDING → ANALYSIS_ARTIFACTS_RECEIVED.
	licEvent := defaultLICEvent(orgID, docID, versionID, "job-lic-004", "corr-004")
	err := h.ingestion.HandleLICArtifacts(context.Background(), licEvent)
	if err == nil {
		t.Fatal("expected error for out-of-order LIC before DP, got nil")
	}

	// Verify no artifacts, no outbox, blobs compensated.
	assertIntEqual(t, "artifacts", 0, len(h.artifactRepo.allArtifacts()))
	assertIntEqual(t, "outbox", 0, len(h.outboxRepo.allEntries()))
	assertIntEqual(t, "blobs compensated", 0, h.objectStorage.blobCount())
}

// ---------------------------------------------------------------------------
// Test: Duplicate DP after LIC is rejected
// ---------------------------------------------------------------------------

func TestFullPipeline_DuplicateDP_AfterLIC_Fails(t *testing.T) {
	const (
		orgID     = "org-fp-005"
		docID     = "doc-fp-005"
		versionID = "ver-fp-005"
	)

	h := newTestHarness(t)
	h.seedDocument(defaultDocument(orgID, docID))
	h.seedVersion(defaultVersion(orgID, docID, versionID))

	// Normal pipeline: DP then LIC.
	_ = h.ingestion.HandleDPArtifacts(context.Background(),
		defaultDPEvent(orgID, docID, versionID, "job-dp-005", "corr-005"))
	_ = h.ingestion.HandleLICArtifacts(context.Background(),
		defaultLICEvent(orgID, docID, versionID, "job-lic-005", "corr-005"))

	snapshotArtifacts := len(h.artifactRepo.allArtifacts())
	snapshotOutbox := len(h.outboxRepo.allEntries())

	// Re-submit DP: ANALYSIS_ARTIFACTS_RECEIVED → PROCESSING_ARTIFACTS_RECEIVED is invalid.
	err := h.ingestion.HandleDPArtifacts(context.Background(),
		defaultDPEvent(orgID, docID, versionID, "job-dp-005-dup", "corr-005"))
	if err == nil {
		t.Fatal("expected error for duplicate DP after LIC, got nil")
	}

	// Artifact and outbox counts unchanged.
	assertIntEqual(t, "artifacts unchanged", snapshotArtifacts, len(h.artifactRepo.allArtifacts()))
	assertIntEqual(t, "outbox unchanged", snapshotOutbox, len(h.outboxRepo.allEntries()))
}

// ---------------------------------------------------------------------------
// Test: GetSemanticTreeRequest → SemanticTreeProvided (happy path)
// ---------------------------------------------------------------------------

func TestGetSemanticTree_HappyPath(t *testing.T) {
	const (
		orgID         = "org-fp-006"
		docID         = "doc-fp-006"
		versionID     = "ver-fp-006"
		dpJobID       = "job-dp-006"
		queryJobID    = "job-query-006"
		correlationID = "corr-006"
	)

	h, recPublisher := newTestHarnessWithRecordingPublisher(t)
	h.seedDocument(defaultDocument(orgID, docID))
	h.seedVersion(defaultVersion(orgID, docID, versionID))

	// Seed DP artifacts (includes SEMANTIC_TREE).
	dpEvent := defaultDPEvent(orgID, docID, versionID, dpJobID, correlationID)
	if err := h.ingestion.HandleDPArtifacts(context.Background(), dpEvent); err != nil {
		t.Fatalf("HandleDPArtifacts: %v", err)
	}

	// Query for semantic tree.
	request := model.GetSemanticTreeRequest{
		EventMeta:  model.EventMeta{CorrelationID: correlationID, Timestamp: time.Now().UTC()},
		JobID:      queryJobID,
		DocumentID: docID,
		VersionID:  versionID,
		OrgID:      orgID,
	}
	if err := h.query.HandleGetSemanticTree(context.Background(), request); err != nil {
		t.Fatalf("HandleGetSemanticTree: %v", err)
	}

	// Verify SemanticTreeProvided was published.
	published := recPublisher.getSemanticTreeProvided()
	if len(published) != 1 {
		t.Fatalf("expected 1 SemanticTreeProvided, got %d", len(published))
	}

	resp := published[0]
	assertEqual(t, "resp.JobID", queryJobID, resp.JobID)
	assertEqual(t, "resp.DocumentID", docID, resp.DocumentID)
	assertEqual(t, "resp.VersionID", versionID, resp.VersionID)
	assertEqual(t, "resp.CorrelationID", correlationID, resp.CorrelationID)

	// Error fields should be empty on success.
	if resp.ErrorCode != "" {
		t.Errorf("expected empty error_code, got %q", resp.ErrorCode)
	}
	if resp.ErrorMessage != "" {
		t.Errorf("expected empty error_message, got %q", resp.ErrorMessage)
	}

	// Verify content matches original DP event semantic tree.
	expectedTree := `{"nodes": [{"id": "root"}]}`
	if string(resp.SemanticTree) != expectedTree {
		t.Errorf("semantic tree content mismatch: expected %s, got %s", expectedTree, string(resp.SemanticTree))
	}

	// Verify audit ARTIFACT_READ record (async — give it a moment).
	time.Sleep(100 * time.Millisecond)
	var foundArtifactRead bool
	for _, rec := range h.auditRepo.allRecords() {
		if rec.Action == model.AuditActionArtifactRead {
			foundArtifactRead = true
			assertEqual(t, "audit.ActorType", string(model.ActorTypeDomain), string(rec.ActorType))
			assertEqual(t, "audit.ActorID", "DP", rec.ActorID)
		}
	}
	if !foundArtifactRead {
		t.Error("missing ARTIFACT_READ audit record for semantic tree query")
	}
}

// ---------------------------------------------------------------------------
// Test: GetSemanticTreeRequest → SemanticTreeProvided with error (not found)
// ---------------------------------------------------------------------------

func TestGetSemanticTree_NotFound(t *testing.T) {
	const (
		orgID         = "org-fp-007"
		docID         = "doc-fp-007"
		versionID     = "ver-fp-007"
		queryJobID    = "job-query-007"
		correlationID = "corr-007"
	)

	h, recPublisher := newTestHarnessWithRecordingPublisher(t)
	h.seedDocument(defaultDocument(orgID, docID))
	h.seedVersion(defaultVersion(orgID, docID, versionID))
	// NOTE: no DP artifacts ingested — semantic tree does not exist.

	request := model.GetSemanticTreeRequest{
		EventMeta:  model.EventMeta{CorrelationID: correlationID, Timestamp: time.Now().UTC()},
		JobID:      queryJobID,
		DocumentID: docID,
		VersionID:  versionID,
		OrgID:      orgID,
	}
	if err := h.query.HandleGetSemanticTree(context.Background(), request); err != nil {
		t.Fatalf("HandleGetSemanticTree should not return error on not-found: %v", err)
	}

	published := recPublisher.getSemanticTreeProvided()
	if len(published) != 1 {
		t.Fatalf("expected 1 SemanticTreeProvided, got %d", len(published))
	}

	resp := published[0]
	if resp.ErrorCode == "" {
		t.Error("expected non-empty error_code for not-found case")
	}
	if resp.ErrorMessage == "" {
		t.Error("expected non-empty error_message for not-found case")
	}
	if resp.SemanticTree != nil {
		t.Error("expected nil semantic_tree for not-found case")
	}
}

// ---------------------------------------------------------------------------
// Test: GetArtifactsRequest → ArtifactsProvided (all found)
// ---------------------------------------------------------------------------

func TestGetArtifacts_HappyPath_AllFound(t *testing.T) {
	const (
		orgID         = "org-fp-008"
		docID         = "doc-fp-008"
		versionID     = "ver-fp-008"
		correlationID = "corr-008"
	)

	h, recPublisher := newTestHarnessWithRecordingPublisher(t)
	h.seedDocument(defaultDocument(orgID, docID))
	h.seedVersion(defaultVersion(orgID, docID, versionID))

	// Ingest DP and LIC.
	_ = h.ingestion.HandleDPArtifacts(context.Background(),
		defaultDPEvent(orgID, docID, versionID, "job-dp-008", correlationID))
	_ = h.ingestion.HandleLICArtifacts(context.Background(),
		defaultLICEvent(orgID, docID, versionID, "job-lic-008", correlationID))

	// Request two existing artifacts: one DP and one LIC.
	request := model.GetArtifactsRequest{
		EventMeta:     model.EventMeta{CorrelationID: correlationID, Timestamp: time.Now().UTC()},
		JobID:         "job-query-008",
		DocumentID:    docID,
		VersionID:     versionID,
		OrgID:         orgID,
		ArtifactTypes: []model.ArtifactType{model.ArtifactTypeSemanticTree, model.ArtifactTypeRiskAnalysis},
	}
	if err := h.query.HandleGetArtifacts(context.Background(), request); err != nil {
		t.Fatalf("HandleGetArtifacts: %v", err)
	}

	published := recPublisher.getArtifactsProvided()
	if len(published) != 1 {
		t.Fatalf("expected 1 ArtifactsProvided, got %d", len(published))
	}

	resp := published[0]
	assertIntEqual(t, "artifacts found", 2, len(resp.Artifacts))
	assertIntEqual(t, "missing types", 0, len(resp.MissingTypes))
	assertEqual(t, "resp.JobID", "job-query-008", resp.JobID)
	assertEqual(t, "resp.CorrelationID", correlationID, resp.CorrelationID)

	// Verify content matches.
	stContent := string(resp.Artifacts[model.ArtifactTypeSemanticTree])
	if stContent != `{"nodes": [{"id": "root"}]}` {
		t.Errorf("semantic tree content mismatch: %s", stContent)
	}
	raContent := string(resp.Artifacts[model.ArtifactTypeRiskAnalysis])
	if raContent != `{"risks": [{"id": "R1", "severity": "HIGH"}]}` {
		t.Errorf("risk analysis content mismatch: %s", raContent)
	}

	// Verify audit ARTIFACT_READ (async).
	time.Sleep(100 * time.Millisecond)
	var foundArtifactRead bool
	for _, rec := range h.auditRepo.allRecords() {
		if rec.Action == model.AuditActionArtifactRead {
			foundArtifactRead = true
		}
	}
	if !foundArtifactRead {
		t.Error("missing ARTIFACT_READ audit record for GetArtifacts query")
	}
}

// ---------------------------------------------------------------------------
// Test: GetArtifactsRequest → partial response (some types missing)
// ---------------------------------------------------------------------------

func TestGetArtifacts_PartialResponse(t *testing.T) {
	const (
		orgID         = "org-fp-009"
		docID         = "doc-fp-009"
		versionID     = "ver-fp-009"
		correlationID = "corr-009"
	)

	h, recPublisher := newTestHarnessWithRecordingPublisher(t)
	h.seedDocument(defaultDocument(orgID, docID))
	h.seedVersion(defaultVersion(orgID, docID, versionID))

	// Ingest only DP (no LIC).
	_ = h.ingestion.HandleDPArtifacts(context.Background(),
		defaultDPEvent(orgID, docID, versionID, "job-dp-009", correlationID))

	// Request both DP and LIC artifacts: SEMANTIC_TREE exists, RISK_ANALYSIS and SUMMARY don't.
	request := model.GetArtifactsRequest{
		EventMeta:     model.EventMeta{CorrelationID: correlationID, Timestamp: time.Now().UTC()},
		JobID:         "job-query-009",
		DocumentID:    docID,
		VersionID:     versionID,
		OrgID:         orgID,
		ArtifactTypes: []model.ArtifactType{model.ArtifactTypeSemanticTree, model.ArtifactTypeRiskAnalysis, model.ArtifactTypeSummary},
	}
	if err := h.query.HandleGetArtifacts(context.Background(), request); err != nil {
		t.Fatalf("HandleGetArtifacts: %v", err)
	}

	published := recPublisher.getArtifactsProvided()
	if len(published) != 1 {
		t.Fatalf("expected 1 ArtifactsProvided, got %d", len(published))
	}

	resp := published[0]
	assertIntEqual(t, "artifacts found", 1, len(resp.Artifacts))
	assertIntEqual(t, "missing types", 2, len(resp.MissingTypes))

	// SEMANTIC_TREE should be in artifacts.
	if _, ok := resp.Artifacts[model.ArtifactTypeSemanticTree]; !ok {
		t.Error("expected SEMANTIC_TREE in response artifacts")
	}

	// RISK_ANALYSIS and SUMMARY should be in missing types.
	missingSet := make(map[model.ArtifactType]bool)
	for _, mt := range resp.MissingTypes {
		missingSet[mt] = true
	}
	if !missingSet[model.ArtifactTypeRiskAnalysis] {
		t.Error("expected RISK_ANALYSIS in missing types")
	}
	if !missingSet[model.ArtifactTypeSummary] {
		t.Error("expected SUMMARY in missing types")
	}
}

// ---------------------------------------------------------------------------
// Test: GetArtifactsRequest → all types missing (no artifacts ingested)
// ---------------------------------------------------------------------------

func TestGetArtifacts_AllMissing(t *testing.T) {
	const (
		orgID     = "org-fp-010"
		docID     = "doc-fp-010"
		versionID = "ver-fp-010"
	)

	h, recPublisher := newTestHarnessWithRecordingPublisher(t)
	h.seedDocument(defaultDocument(orgID, docID))
	h.seedVersion(defaultVersion(orgID, docID, versionID))
	// No artifacts ingested.

	request := model.GetArtifactsRequest{
		EventMeta:     model.EventMeta{CorrelationID: "corr-010", Timestamp: time.Now().UTC()},
		JobID:         "job-query-010",
		DocumentID:    docID,
		VersionID:     versionID,
		OrgID:         orgID,
		ArtifactTypes: []model.ArtifactType{model.ArtifactTypeRiskAnalysis, model.ArtifactTypeSummary},
	}
	if err := h.query.HandleGetArtifacts(context.Background(), request); err != nil {
		t.Fatalf("HandleGetArtifacts: %v", err)
	}

	published := recPublisher.getArtifactsProvided()
	if len(published) != 1 {
		t.Fatalf("expected 1 ArtifactsProvided, got %d", len(published))
	}

	resp := published[0]
	assertIntEqual(t, "artifacts found", 0, len(resp.Artifacts))
	assertIntEqual(t, "missing types", 2, len(resp.MissingTypes))
}

// ---------------------------------------------------------------------------
// Helper: verify producer domain counts in artifact list
// ---------------------------------------------------------------------------

func verifyProducerDomains(t *testing.T, artifacts []*model.ArtifactDescriptor, producer model.ProducerDomain, expectedCount int) {
	t.Helper()
	count := 0
	for _, a := range artifacts {
		if a.ProducerDomain == producer {
			count++
		}
	}
	if count != expectedCount {
		t.Errorf("expected %d artifacts from %s, got %d", expectedCount, producer, count)
	}
}
