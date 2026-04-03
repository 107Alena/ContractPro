package integration

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	appLifecycle "contractpro/document-management/internal/application/lifecycle"
	"contractpro/document-management/internal/domain/model"
	"contractpro/document-management/internal/domain/port"
	"contractpro/document-management/internal/infra/objectstorage"
)

// ---------------------------------------------------------------------------
// Test 1: DP Artifacts event with wrong org_id is rejected.
// ---------------------------------------------------------------------------

func TestTenantIsolation_DPArtifacts_WrongOrg_Rejected(t *testing.T) {
	const (
		orgA      = "org-tenant-A"
		orgB      = "org-tenant-B"
		docID     = "doc-tenant-001"
		versionID = "ver-tenant-001"
		jobID     = "job-tenant-001"
	)

	h := newTestHarness(t)

	// Seed document and version for org-A.
	h.seedDocument(defaultDocument(orgA, docID))
	h.seedVersion(defaultVersion(orgA, docID, versionID))

	// Send DP event claiming org-B.
	event := defaultDPEvent(orgB, docID, versionID, jobID, "corr-tenant-001")

	err := h.ingestion.HandleDPArtifacts(context.Background(), event)
	if err == nil {
		t.Fatal("expected TENANT_MISMATCH error, got nil")
	}
	if code := port.ErrorCode(err); code != port.ErrCodeTenantMismatch {
		t.Errorf("expected error code %s, got %s (err: %v)", port.ErrCodeTenantMismatch, code, err)
	}

	// Verify no artifacts stored.
	if got := len(h.artifactRepo.allArtifacts()); got != 0 {
		t.Errorf("expected 0 artifact descriptors, got %d", got)
	}

	// Verify no outbox events.
	if got := len(h.outboxRepo.allEntries()); got != 0 {
		t.Errorf("expected 0 outbox entries, got %d", got)
	}

	// Verify no blobs in object storage.
	if got := h.objectStorage.blobCount(); got != 0 {
		t.Errorf("expected 0 blobs in storage, got %d", got)
	}
}

// ---------------------------------------------------------------------------
// Test 2: LIC Artifacts event with wrong org_id is rejected.
// ---------------------------------------------------------------------------

func TestTenantIsolation_LICArtifacts_WrongOrg_Rejected(t *testing.T) {
	const (
		orgA      = "org-tenant-A"
		orgB      = "org-tenant-B"
		docID     = "doc-tenant-002"
		versionID = "ver-tenant-002"
		jobID     = "job-tenant-002"
	)

	h := newTestHarness(t)

	// Seed document and version for org-A (DP already received).
	h.seedDocument(defaultDocument(orgA, docID))
	ver := defaultVersion(orgA, docID, versionID)
	ver.ArtifactStatus = model.ArtifactStatusProcessingArtifactsReceived
	h.seedVersion(ver)

	// Send LIC event claiming org-B.
	event := defaultLICEvent(orgB, docID, versionID, jobID, "corr-tenant-002")

	err := h.ingestion.HandleLICArtifacts(context.Background(), event)
	if err == nil {
		t.Fatal("expected TENANT_MISMATCH error, got nil")
	}
	if code := port.ErrorCode(err); code != port.ErrCodeTenantMismatch {
		t.Errorf("expected error code %s, got %s (err: %v)", port.ErrCodeTenantMismatch, code, err)
	}

	// Verify no artifacts stored.
	if got := len(h.artifactRepo.allArtifacts()); got != 0 {
		t.Errorf("expected 0 artifact descriptors, got %d", got)
	}

	// Verify no outbox events.
	if got := len(h.outboxRepo.allEntries()); got != 0 {
		t.Errorf("expected 0 outbox entries, got %d", got)
	}
}

// ---------------------------------------------------------------------------
// Test 3: DiffReady event with wrong org_id is rejected.
// ---------------------------------------------------------------------------

func TestTenantIsolation_DiffReady_WrongOrg_Rejected(t *testing.T) {
	const (
		orgA        = "org-tenant-A"
		orgB        = "org-tenant-B"
		docID       = "doc-tenant-003"
		versionID1  = "ver-tenant-003a"
		versionID2  = "ver-tenant-003b"
		jobID       = "job-tenant-003"
	)

	h := newTestHarness(t)

	// Seed document and two versions for org-A.
	h.seedDocument(defaultDocument(orgA, docID))
	h.seedVersion(defaultVersion(orgA, docID, versionID1))
	ver2 := defaultVersion(orgA, docID, versionID2)
	ver2.VersionNumber = 2
	h.seedVersion(ver2)

	// Send diff event claiming org-B.
	diffEvent := model.DocumentVersionDiffReady{
		EventMeta: model.EventMeta{
			CorrelationID: "corr-tenant-003",
			Timestamp:     time.Now().UTC(),
		},
		JobID:           jobID,
		DocumentID:      docID,
		BaseVersionID:   versionID1,
		TargetVersionID: versionID2,
		OrgID:           orgB,
		TextDiffs:       json.RawMessage(`[{"from":"a","to":"b"}]`),
		StructuralDiffs: json.RawMessage(`[{"type":"added"}]`),
	}

	err := h.diffService.HandleDiffReady(context.Background(), diffEvent)
	if err == nil {
		t.Fatal("expected TENANT_MISMATCH error, got nil")
	}
	if code := port.ErrorCode(err); code != port.ErrCodeTenantMismatch {
		t.Errorf("expected error code %s, got %s (err: %v)", port.ErrCodeTenantMismatch, code, err)
	}

	// Verify no diffs stored.
	diffs, _ := h.diffRepo.ListByDocument(context.Background(), orgA, docID)
	if len(diffs) != 0 {
		t.Errorf("expected 0 diffs, got %d", len(diffs))
	}
	diffsB, _ := h.diffRepo.ListByDocument(context.Background(), orgB, docID)
	if len(diffsB) != 0 {
		t.Errorf("expected 0 diffs for org-B, got %d", len(diffsB))
	}
}

// ---------------------------------------------------------------------------
// Test 4: GetSemanticTree request with wrong org_id is rejected.
// ---------------------------------------------------------------------------

func TestTenantIsolation_GetSemanticTree_WrongOrg_Rejected(t *testing.T) {
	const (
		orgA      = "org-tenant-A"
		orgB      = "org-tenant-B"
		docID     = "doc-tenant-004"
		versionID = "ver-tenant-004"
	)

	h, recPublisher := newTestHarnessWithRecordingPublisher(t)

	// Seed document and version (PENDING) for org-A.
	h.seedDocument(defaultDocument(orgA, docID))
	h.seedVersion(defaultVersion(orgA, docID, versionID))

	// Ingest DP artifacts so that a semantic tree is stored.
	dpEvent := defaultDPEvent(orgA, docID, versionID, "job-dp-004", "corr-dp-004")
	if err := h.ingestion.HandleDPArtifacts(context.Background(), dpEvent); err != nil {
		t.Fatalf("HandleDPArtifacts setup: %v", err)
	}

	// Now request the semantic tree claiming org-B.
	request := model.GetSemanticTreeRequest{
		EventMeta:  model.EventMeta{CorrelationID: "corr-tenant-004", Timestamp: time.Now().UTC()},
		JobID:      "job-tenant-004",
		DocumentID: docID,
		VersionID:  versionID,
		OrgID:      orgB,
	}

	err := h.query.HandleGetSemanticTree(context.Background(), request)
	if err == nil {
		t.Fatal("expected TENANT_MISMATCH error, got nil")
	}
	if code := port.ErrorCode(err); code != port.ErrCodeTenantMismatch {
		t.Errorf("expected error code %s, got %s (err: %v)", port.ErrCodeTenantMismatch, code, err)
	}

	// Verify no SemanticTreeProvided events published.
	if got := len(recPublisher.getSemanticTreeProvided()); got != 0 {
		t.Errorf("expected 0 published semantic tree events, got %d", got)
	}
}

// ---------------------------------------------------------------------------
// Test 5: GetArtifacts request with wrong org_id is rejected.
// ---------------------------------------------------------------------------

func TestTenantIsolation_GetArtifacts_WrongOrg_Rejected(t *testing.T) {
	const (
		orgA      = "org-tenant-A"
		orgB      = "org-tenant-B"
		docID     = "doc-tenant-005"
		versionID = "ver-tenant-005"
	)

	h, recPublisher := newTestHarnessWithRecordingPublisher(t)

	// Seed document, version, and artifacts for org-A.
	h.seedDocument(defaultDocument(orgA, docID))
	h.seedVersion(defaultVersion(orgA, docID, versionID))

	dpEvent := defaultDPEvent(orgA, docID, versionID, "job-dp-005", "corr-dp-005")
	if err := h.ingestion.HandleDPArtifacts(context.Background(), dpEvent); err != nil {
		t.Fatalf("HandleDPArtifacts setup: %v", err)
	}

	// Request artifacts claiming org-B.
	request := model.GetArtifactsRequest{
		EventMeta:     model.EventMeta{CorrelationID: "corr-tenant-005", Timestamp: time.Now().UTC()},
		JobID:         "job-tenant-005",
		DocumentID:    docID,
		VersionID:     versionID,
		OrgID:         orgB,
		ArtifactTypes: []model.ArtifactType{model.ArtifactTypeSemanticTree},
	}

	err := h.query.HandleGetArtifacts(context.Background(), request)
	if err == nil {
		t.Fatal("expected TENANT_MISMATCH error, got nil")
	}
	if code := port.ErrorCode(err); code != port.ErrCodeTenantMismatch {
		t.Errorf("expected error code %s, got %s (err: %v)", port.ErrCodeTenantMismatch, code, err)
	}

	// Verify no ArtifactsProvided events published.
	if got := len(recPublisher.getArtifactsProvided()); got != 0 {
		t.Errorf("expected 0 published artifact events, got %d", got)
	}
}

// ---------------------------------------------------------------------------
// Test 6: Correct org succeeds (positive control).
// ---------------------------------------------------------------------------

func TestTenantIsolation_CorrectOrg_Succeeds(t *testing.T) {
	const (
		orgA      = "org-tenant-A"
		docID     = "doc-tenant-006"
		versionID = "ver-tenant-006"
		jobID     = "job-tenant-006"
	)

	h := newTestHarness(t)

	h.seedDocument(defaultDocument(orgA, docID))
	h.seedVersion(defaultVersion(orgA, docID, versionID))

	event := defaultDPEvent(orgA, docID, versionID, jobID, "corr-tenant-006")

	err := h.ingestion.HandleDPArtifacts(context.Background(), event)
	if err != nil {
		t.Fatalf("expected success for correct org, got error: %v", err)
	}

	// Verify artifacts stored.
	expectedTypes := []model.ArtifactType{
		model.ArtifactTypeOCRRaw,
		model.ArtifactTypeExtractedText,
		model.ArtifactTypeDocumentStructure,
		model.ArtifactTypeSemanticTree,
	}
	for _, at := range expectedTypes {
		key := objectstorage.ArtifactKey(orgA, docID, versionID, at)
		if !h.objectStorage.hasKey(key) {
			t.Errorf("expected blob at key %s, but not found", key)
		}
	}
	if got := len(h.artifactRepo.allArtifacts()); got != 4 {
		t.Errorf("expected 4 artifact descriptors, got %d", got)
	}
}

// ---------------------------------------------------------------------------
// Test 7: Empty org_id bypasses tenant check (fallback path).
// ---------------------------------------------------------------------------

func TestTenantIsolation_EmptyOrg_FallbackBypass(t *testing.T) {
	const (
		orgA      = "org-tenant-A"
		docID     = "doc-tenant-007"
		versionID = "ver-tenant-007"
		jobID     = "job-tenant-007"
	)

	h := newTestHarness(t)

	h.seedDocument(defaultDocument(orgA, docID))
	h.seedVersion(defaultVersion(orgA, docID, versionID))

	// Register fallback so the resolver can fill in the correct org.
	h.fallback.RegisterDocument(docID, orgA, versionID)

	// Send event with empty org_id — should bypass tenant check and succeed
	// via the fallback resolver.
	event := defaultDPEvent("", docID, "", jobID, "corr-tenant-007")

	err := h.ingestion.HandleDPArtifacts(context.Background(), event)
	if err != nil {
		t.Fatalf("expected success with empty org (fallback path), got error: %v", err)
	}

	// Verify artifacts stored.
	if got := len(h.artifactRepo.allArtifacts()); got != 4 {
		t.Errorf("expected 4 artifact descriptors, got %d", got)
	}

	// Verify the version was updated with the correct org from the fallback.
	ver, findErr := h.versionRepo.FindByID(context.Background(), orgA, docID, versionID)
	if findErr != nil {
		t.Fatalf("FindByID: %v", findErr)
	}
	assertEqual(t, "version.ArtifactStatus",
		string(model.ArtifactStatusProcessingArtifactsReceived), string(ver.ArtifactStatus))
}

// ---------------------------------------------------------------------------
// Test 8: Sync API — org-A cannot read org-B's document.
// ---------------------------------------------------------------------------

func TestTenantIsolation_SyncAPI_OrgA_CannotRead_OrgB_Document(t *testing.T) {
	const (
		orgA = "org-tenant-A"
		orgB = "org-tenant-B"
	)

	h := newTestHarness(t)

	// Create a lifecycle service wired to the harness in-memory repos.
	lifecycleSvc := appLifecycle.NewDocumentLifecycleService(
		h.transactor, h.docRepo, h.auditRepo, h.logger,
	)

	// Create document for org-B — use the returned document's ID.
	docB, err := lifecycleSvc.CreateDocument(context.Background(), port.CreateDocumentParams{
		OrganizationID:  orgB,
		Title:           "Org-B Secret Contract",
		CreatedByUserID: "user-B",
	})
	if err != nil {
		t.Fatalf("CreateDocument for org-B: %v", err)
	}

	// Try to get org-B's document with org-A credentials — should fail.
	_, err = lifecycleSvc.GetDocument(context.Background(), orgA, docB.DocumentID)
	if err == nil {
		t.Fatal("expected error when org-A reads org-B document, got nil")
	}
	if code := port.ErrorCode(err); code != port.ErrCodeDocumentNotFound {
		t.Errorf("expected error code %s, got %s (err: %v)", port.ErrCodeDocumentNotFound, code, err)
	}

	// Verify org-B CAN read its own document.
	_, err = lifecycleSvc.GetDocument(context.Background(), orgB, docB.DocumentID)
	if err != nil {
		t.Fatalf("expected org-B to read its own document, got error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Test 9: RE Artifacts event with wrong org_id is rejected.
// ---------------------------------------------------------------------------

func TestTenantIsolation_REArtifacts_WrongOrg_Rejected(t *testing.T) {
	const (
		orgA      = "org-tenant-A"
		orgB      = "org-tenant-B"
		docID     = "doc-tenant-009"
		versionID = "ver-tenant-009"
		jobID     = "job-tenant-009"
	)

	h := newTestHarness(t)

	// Seed document and version for org-A (DP + LIC already received).
	h.seedDocument(defaultDocument(orgA, docID))
	ver := defaultVersion(orgA, docID, versionID)
	ver.ArtifactStatus = model.ArtifactStatusAnalysisArtifactsReceived
	h.seedVersion(ver)

	// Build RE event claiming org-B. Pre-seed blobs under org-B's keys.
	event := defaultREEvent(h, orgB, docID, versionID, jobID, "corr-tenant-009")

	err := h.ingestion.HandleREArtifacts(context.Background(), event)
	if err == nil {
		t.Fatal("expected TENANT_MISMATCH error, got nil")
	}
	if code := port.ErrorCode(err); code != port.ErrCodeTenantMismatch {
		t.Errorf("expected error code %s, got %s (err: %v)", port.ErrCodeTenantMismatch, code, err)
	}

	// Verify no artifact descriptors created.
	if got := len(h.artifactRepo.allArtifacts()); got != 0 {
		t.Errorf("expected 0 artifact descriptors, got %d", got)
	}

	// Verify no outbox events.
	if got := len(h.outboxRepo.allEntries()); got != 0 {
		t.Errorf("expected 0 outbox entries, got %d", got)
	}
}

// ---------------------------------------------------------------------------
// Test 10: Sync API — ListDocuments returns only org-scoped docs.
// ---------------------------------------------------------------------------

func TestTenantIsolation_SyncAPI_ListDocuments_OrgIsolation(t *testing.T) {
	const (
		orgA = "org-tenant-A"
		orgB = "org-tenant-B"
	)

	h := newTestHarness(t)

	lifecycleSvc := appLifecycle.NewDocumentLifecycleService(
		h.transactor, h.docRepo, h.auditRepo, h.logger,
	)

	// Create 2 docs for org-A and 1 doc for org-B.
	for _, title := range []string{"Contract A1", "Contract A2"} {
		_, err := lifecycleSvc.CreateDocument(context.Background(), port.CreateDocumentParams{
			OrganizationID:  orgA,
			Title:           title,
			CreatedByUserID: "user-A",
		})
		if err != nil {
			t.Fatalf("CreateDocument org-A %q: %v", title, err)
		}
	}
	_, err := lifecycleSvc.CreateDocument(context.Background(), port.CreateDocumentParams{
		OrganizationID:  orgB,
		Title:           "Contract B1",
		CreatedByUserID: "user-B",
	})
	if err != nil {
		t.Fatalf("CreateDocument org-B: %v", err)
	}

	// List for org-A — should return exactly 2 documents.
	resultA, err := lifecycleSvc.ListDocuments(context.Background(), port.ListDocumentsParams{
		OrganizationID: orgA,
		Page:           1,
		PageSize:       10,
	})
	if err != nil {
		t.Fatalf("ListDocuments org-A: %v", err)
	}
	if resultA.TotalCount != 2 {
		t.Errorf("expected 2 docs for org-A, got %d", resultA.TotalCount)
	}
	for _, doc := range resultA.Items {
		if doc.OrganizationID != orgA {
			t.Errorf("org-A list returned doc with org %s", doc.OrganizationID)
		}
	}

	// List for org-B — should return exactly 1 document.
	resultB, err := lifecycleSvc.ListDocuments(context.Background(), port.ListDocumentsParams{
		OrganizationID: orgB,
		Page:           1,
		PageSize:       10,
	})
	if err != nil {
		t.Fatalf("ListDocuments org-B: %v", err)
	}
	if resultB.TotalCount != 1 {
		t.Errorf("expected 1 doc for org-B, got %d", resultB.TotalCount)
	}
	for _, doc := range resultB.Items {
		if doc.OrganizationID != orgB {
			t.Errorf("org-B list returned doc with org %s", doc.OrganizationID)
		}
	}
}
