package integration

import (
	"context"
	"encoding/json"
	"testing"

	"contractpro/document-management/internal/domain/model"
	"contractpro/document-management/internal/domain/port"
	"contractpro/document-management/internal/infra/objectstorage"
	"contractpro/document-management/internal/ingress/idempotency"
)

// ---------------------------------------------------------------------------
// Test: DP Artifacts → DM saves artifacts → outbox has confirmation + notification
// ---------------------------------------------------------------------------

func TestDPIngestion_HappyPath(t *testing.T) {
	const (
		orgID         = "org-001"
		docID         = "doc-001"
		versionID     = "ver-001"
		jobID         = "job-dp-001"
		correlationID = "corr-dp-001"
	)

	h := newTestHarness(t)

	// Pre-seed document and version in PENDING status.
	h.seedDocument(defaultDocument(orgID, docID))
	h.seedVersion(defaultVersion(orgID, docID, versionID))

	// Create DP event with 4 artifacts (no warnings).
	event := defaultDPEvent(orgID, docID, versionID, jobID, correlationID)

	// --- Act: process the DP artifacts event ---
	err := h.ingestion.HandleDPArtifacts(context.Background(), event)
	if err != nil {
		t.Fatalf("HandleDPArtifacts returned error: %v", err)
	}

	// --- Assert: blobs stored in Object Storage ---
	expectedTypes := []model.ArtifactType{
		model.ArtifactTypeOCRRaw,
		model.ArtifactTypeExtractedText,
		model.ArtifactTypeDocumentStructure,
		model.ArtifactTypeSemanticTree,
	}

	for _, at := range expectedTypes {
		key := objectstorage.ArtifactKey(orgID, docID, versionID, at)
		if !h.objectStorage.hasKey(key) {
			t.Errorf("expected blob at key %s, but not found", key)
		}
	}

	if got := h.objectStorage.blobCount(); got != 4 {
		t.Errorf("expected 4 blobs in object storage, got %d", got)
	}

	// --- Assert: ArtifactDescriptor records created ---
	artifacts := h.artifactRepo.allArtifacts()
	if len(artifacts) != 4 {
		t.Fatalf("expected 4 artifact descriptors, got %d", len(artifacts))
	}

	artifactTypeSet := make(map[model.ArtifactType]bool)
	for _, a := range artifacts {
		artifactTypeSet[a.ArtifactType] = true
		assertEqual(t, "artifact.OrganizationID", orgID, a.OrganizationID)
		assertEqual(t, "artifact.DocumentID", docID, a.DocumentID)
		assertEqual(t, "artifact.VersionID", versionID, a.VersionID)
		assertEqual(t, "artifact.ProducerDomain", string(model.ProducerDomainDP), string(a.ProducerDomain))
		assertEqual(t, "artifact.JobID", jobID, a.JobID)
		assertEqual(t, "artifact.CorrelationID", correlationID, a.CorrelationID)
		if a.SizeBytes <= 0 {
			t.Errorf("expected positive size_bytes for %s, got %d", a.ArtifactType, a.SizeBytes)
		}
		if a.ContentHash == "" {
			t.Errorf("expected non-empty content_hash for %s", a.ArtifactType)
		}
		if a.StorageKey == "" {
			t.Errorf("expected non-empty storage_key for %s", a.ArtifactType)
		}
	}

	for _, at := range expectedTypes {
		if !artifactTypeSet[at] {
			t.Errorf("missing artifact descriptor for type %s", at)
		}
	}

	// --- Assert: version artifact_status transitioned to PROCESSING_ARTIFACTS_RECEIVED ---
	version, err := h.versionRepo.FindByID(context.Background(), orgID, docID, versionID)
	if err != nil {
		t.Fatalf("FindByID version failed: %v", err)
	}
	assertEqual(t, "version.ArtifactStatus",
		string(model.ArtifactStatusProcessingArtifactsReceived),
		string(version.ArtifactStatus))

	// --- Assert: audit records created (2: ARTIFACT_SAVED + ARTIFACT_STATUS_CHANGED) ---
	auditRecords := h.auditRepo.allRecords()
	if len(auditRecords) != 2 {
		t.Fatalf("expected 2 audit records, got %d", len(auditRecords))
	}

	auditActions := make(map[model.AuditAction]bool)
	for _, rec := range auditRecords {
		auditActions[rec.Action] = true
		assertEqual(t, "audit.OrganizationID", orgID, rec.OrganizationID)
		assertEqual(t, "audit.DocumentID", docID, rec.DocumentID)
		assertEqual(t, "audit.VersionID", versionID, rec.VersionID)
		assertEqual(t, "audit.JobID", jobID, rec.JobID)
		assertEqual(t, "audit.CorrelationID", correlationID, rec.CorrelationID)
		assertEqual(t, "audit.ActorType", string(model.ActorTypeDomain), string(rec.ActorType))
		assertEqual(t, "audit.ActorID", string(model.ProducerDomainDP), rec.ActorID)
	}

	if !auditActions[model.AuditActionArtifactSaved] {
		t.Error("missing audit record with action ARTIFACT_SAVED")
	}
	if !auditActions[model.AuditActionArtifactStatusChanged] {
		t.Error("missing audit record with action ARTIFACT_STATUS_CHANGED")
	}

	// --- Assert: outbox entries created (2: confirmation + notification) ---
	outboxEntries := h.outboxRepo.allEntries()
	if len(outboxEntries) != 2 {
		t.Fatalf("expected 2 outbox entries, got %d", len(outboxEntries))
	}

	outboxTopics := make(map[string]port.OutboxEntry)
	for _, entry := range outboxEntries {
		outboxTopics[entry.Topic] = entry
		assertEqual(t, "outbox.AggregateID", versionID, entry.AggregateID)
		assertEqual(t, "outbox.Status", "PENDING", entry.Status)
		if len(entry.Payload) == 0 {
			t.Errorf("outbox entry for topic %s has empty payload", entry.Topic)
		}
	}

	// Check confirmation event (ArtifactsPersisted).
	confirmEntry, ok := outboxTopics[model.TopicDMResponsesArtifactsPersisted]
	if !ok {
		t.Fatal("missing outbox entry for topic dm.responses.artifacts-persisted")
	}

	var confirmEvent model.DocumentProcessingArtifactsPersisted
	if err := json.Unmarshal(confirmEntry.Payload, &confirmEvent); err != nil {
		t.Fatalf("failed to unmarshal confirmation event: %v", err)
	}
	assertEqual(t, "confirm.JobID", jobID, confirmEvent.JobID)
	assertEqual(t, "confirm.DocumentID", docID, confirmEvent.DocumentID)
	assertEqual(t, "confirm.CorrelationID", correlationID, confirmEvent.CorrelationID)

	// Check notification event (VersionProcessingArtifactsReady).
	notifEntry, ok := outboxTopics[model.TopicDMEventsVersionArtifactsReady]
	if !ok {
		t.Fatal("missing outbox entry for topic dm.events.version-artifacts-ready")
	}

	var notifEvent model.VersionProcessingArtifactsReady
	if err := json.Unmarshal(notifEntry.Payload, &notifEvent); err != nil {
		t.Fatalf("failed to unmarshal notification event: %v", err)
	}
	assertEqual(t, "notif.DocumentID", docID, notifEvent.DocumentID)
	assertEqual(t, "notif.VersionID", versionID, notifEvent.VersionID)
	assertEqual(t, "notif.OrgID", orgID, notifEvent.OrgID)
	assertEqual(t, "notif.CorrelationID", correlationID, notifEvent.CorrelationID)
	if len(notifEvent.ArtifactTypes) != 4 {
		t.Errorf("expected 4 artifact types in notification, got %d", len(notifEvent.ArtifactTypes))
	}
}

// ---------------------------------------------------------------------------
// Test: DP Artifacts with 5 artifacts (including warnings)
// ---------------------------------------------------------------------------

func TestDPIngestion_WithWarnings(t *testing.T) {
	const (
		orgID     = "org-002"
		docID     = "doc-002"
		versionID = "ver-002"
		jobID     = "job-dp-002"
	)

	h := newTestHarness(t)
	h.seedDocument(defaultDocument(orgID, docID))
	h.seedVersion(defaultVersion(orgID, docID, versionID))

	event := defaultDPEvent(orgID, docID, versionID, jobID, "corr-002")
	event.Warnings = json.RawMessage(`[{"code": "W001", "message": "low quality scan"}]`)

	err := h.ingestion.HandleDPArtifacts(context.Background(), event)
	if err != nil {
		t.Fatalf("HandleDPArtifacts returned error: %v", err)
	}

	// 5 artifacts (4 + warnings).
	if got := h.objectStorage.blobCount(); got != 5 {
		t.Errorf("expected 5 blobs, got %d", got)
	}
	if got := len(h.artifactRepo.allArtifacts()); got != 5 {
		t.Errorf("expected 5 artifact descriptors, got %d", got)
	}
}

// ---------------------------------------------------------------------------
// Test: content hash matches SHA-256 of stored blob
// ---------------------------------------------------------------------------

func TestDPIngestion_ContentHashIntegrity(t *testing.T) {
	const (
		orgID     = "org-003"
		docID     = "doc-003"
		versionID = "ver-003"
		jobID     = "job-dp-003"
	)

	h := newTestHarness(t)
	h.seedDocument(defaultDocument(orgID, docID))
	h.seedVersion(defaultVersion(orgID, docID, versionID))

	event := defaultDPEvent(orgID, docID, versionID, jobID, "corr-003")
	err := h.ingestion.HandleDPArtifacts(context.Background(), event)
	if err != nil {
		t.Fatalf("HandleDPArtifacts returned error: %v", err)
	}

	// Verify content hash for the SEMANTIC_TREE artifact.
	stKey := objectstorage.ArtifactKey(orgID, docID, versionID, model.ArtifactTypeSemanticTree)
	blob := h.objectStorage.getBlob(stKey)
	if blob == nil {
		t.Fatal("semantic tree blob not found in object storage")
	}

	expectedHash := sha256HexHelper(blob)

	descriptor, err := h.artifactRepo.FindByVersionAndType(
		context.Background(), orgID, docID, versionID, model.ArtifactTypeSemanticTree)
	if err != nil {
		t.Fatalf("FindByVersionAndType failed: %v", err)
	}

	assertEqual(t, "content_hash", expectedHash, descriptor.ContentHash)
	assertIntEqual(t, "size_bytes", len(blob), int(descriptor.SizeBytes))
}

// ---------------------------------------------------------------------------
// Test: duplicate event delivery → idempotency hit → no duplicate artifacts
// ---------------------------------------------------------------------------

func TestDPIngestion_IdempotencyDedup(t *testing.T) {
	const (
		orgID     = "org-004"
		docID     = "doc-004"
		versionID = "ver-004"
		jobID     = "job-dp-004"
	)

	h := newTestHarness(t)
	h.seedDocument(defaultDocument(orgID, docID))
	h.seedVersion(defaultVersion(orgID, docID, versionID))

	event := defaultDPEvent(orgID, docID, versionID, jobID, "corr-004")

	// --- First delivery: process normally ---
	idemKey := idempotency.KeyForDPArtifacts(jobID)

	result, err := h.idempotencyGuard.Check(context.Background(), idemKey, model.TopicDPArtifactsProcessingReady, nil)
	if err != nil {
		t.Fatalf("idempotency Check #1 failed: %v", err)
	}
	if result != idempotency.ResultProcess {
		t.Fatalf("expected ResultProcess, got %v", result)
	}

	err = h.ingestion.HandleDPArtifacts(context.Background(), event)
	if err != nil {
		t.Fatalf("HandleDPArtifacts #1 returned error: %v", err)
	}

	err = h.idempotencyGuard.MarkCompleted(context.Background(), idemKey)
	if err != nil {
		t.Fatalf("MarkCompleted failed: %v", err)
	}

	// Verify first delivery state.
	firstArtifactCount := len(h.artifactRepo.allArtifacts())
	firstOutboxCount := len(h.outboxRepo.allEntries())
	firstAuditCount := len(h.auditRepo.allRecords())
	assertIntEqual(t, "first artifacts", 4, firstArtifactCount)
	assertIntEqual(t, "first outbox", 2, firstOutboxCount)
	assertIntEqual(t, "first audit", 2, firstAuditCount)

	// --- Second delivery: same event → idempotency skip ---
	result2, err := h.idempotencyGuard.Check(context.Background(), idemKey, model.TopicDPArtifactsProcessingReady, nil)
	if err != nil {
		t.Fatalf("idempotency Check #2 failed: %v", err)
	}
	if result2 != idempotency.ResultSkip {
		t.Fatalf("expected ResultSkip on duplicate, got %v", result2)
	}

	// Verify: no additional artifacts, outbox entries, or audit records.
	assertIntEqual(t, "artifacts after dedup", firstArtifactCount, len(h.artifactRepo.allArtifacts()))
	assertIntEqual(t, "outbox after dedup", firstOutboxCount, len(h.outboxRepo.allEntries()))
	assertIntEqual(t, "audit after dedup", firstAuditCount, len(h.auditRepo.allRecords()))

	// Verify idempotency record state.
	idemRecord := h.idemStore.getRecord(idemKey)
	if idemRecord == nil {
		t.Fatal("idempotency record not found")
	}
	assertEqual(t, "idem.Status", string(model.IdempotencyStatusCompleted), string(idemRecord.Status))
}

// ---------------------------------------------------------------------------
// Test: version not found → error returned
// ---------------------------------------------------------------------------

func TestDPIngestion_VersionNotFound(t *testing.T) {
	const (
		orgID     = "org-005"
		docID     = "doc-005"
		versionID = "ver-005-missing"
		jobID     = "job-dp-005"
	)

	h := newTestHarness(t)
	h.seedDocument(defaultDocument(orgID, docID))
	// Note: version NOT seeded — simulating missing version.

	event := defaultDPEvent(orgID, docID, versionID, jobID, "corr-005")

	err := h.ingestion.HandleDPArtifacts(context.Background(), event)
	if err == nil {
		t.Fatal("expected error for missing version, got nil")
	}

	// Should be a VERSION_NOT_FOUND error.
	if !port.IsNotFound(err) {
		t.Errorf("expected not-found error, got: %v", err)
	}

	// Verify: no artifacts stored (compensation should have cleaned blobs).
	assertIntEqual(t, "artifacts after error", 0, len(h.artifactRepo.allArtifacts()))
	assertIntEqual(t, "outbox after error", 0, len(h.outboxRepo.allEntries()))
}

// ---------------------------------------------------------------------------
// Test: version_id missing in event → REV-001 fallback resolves from DB
// ---------------------------------------------------------------------------

func TestDPIngestion_FallbackVersionID(t *testing.T) {
	const (
		orgID     = "org-006"
		docID     = "doc-006"
		versionID = "ver-006"
		jobID     = "job-dp-006"
	)

	h := newTestHarness(t)
	h.seedDocument(defaultDocument(orgID, docID))
	h.seedVersion(defaultVersion(orgID, docID, versionID))

	// Register fallback data for the document.
	h.fallback.RegisterDocument(docID, orgID, versionID)

	// Create event WITHOUT version_id and org_id (REV-001 + REV-002).
	event := defaultDPEvent("", docID, "", jobID, "corr-006")

	err := h.ingestion.HandleDPArtifacts(context.Background(), event)
	if err != nil {
		t.Fatalf("HandleDPArtifacts with fallback returned error: %v", err)
	}

	// Verify: artifacts are stored correctly despite missing fields in event.
	assertIntEqual(t, "artifacts", 4, len(h.artifactRepo.allArtifacts()))

	version, err := h.versionRepo.FindByID(context.Background(), orgID, docID, versionID)
	if err != nil {
		t.Fatalf("FindByID version failed: %v", err)
	}
	assertEqual(t, "version.ArtifactStatus",
		string(model.ArtifactStatusProcessingArtifactsReceived),
		string(version.ArtifactStatus))

	// Verify fallback warning was logged.
	if !h.logger.hasMessage("REV-001 fallback") && !h.logger.hasMessage("REV-002 fallback") {
		t.Error("expected fallback warning log message")
	}
}

// ---------------------------------------------------------------------------
// Test: outbox entry aggregate_id is version_id for FIFO ordering (REV-010)
// ---------------------------------------------------------------------------

func TestDPIngestion_OutboxAggregateID(t *testing.T) {
	const (
		orgID     = "org-007"
		docID     = "doc-007"
		versionID = "ver-007"
		jobID     = "job-dp-007"
	)

	h := newTestHarness(t)
	h.seedDocument(defaultDocument(orgID, docID))
	h.seedVersion(defaultVersion(orgID, docID, versionID))

	event := defaultDPEvent(orgID, docID, versionID, jobID, "corr-007")
	err := h.ingestion.HandleDPArtifacts(context.Background(), event)
	if err != nil {
		t.Fatalf("HandleDPArtifacts returned error: %v", err)
	}

	for _, entry := range h.outboxRepo.allEntries() {
		if entry.AggregateID != versionID {
			t.Errorf("outbox entry for topic %s has aggregate_id=%s, expected %s",
				entry.Topic, entry.AggregateID, versionID)
		}
	}
}

// ---------------------------------------------------------------------------
// Test: object storage blob compensation on transaction failure
// ---------------------------------------------------------------------------

func TestDPIngestion_CompensationOnTxFailure(t *testing.T) {
	const (
		orgID     = "org-008"
		docID     = "doc-008"
		versionID = "ver-008"
		jobID     = "job-dp-008"
	)

	h := newTestHarness(t)
	h.seedDocument(defaultDocument(orgID, docID))

	// Seed version in FULLY_READY status (terminal — no valid transition).
	ver := defaultVersion(orgID, docID, versionID)
	ver.ArtifactStatus = model.ArtifactStatusFullyReady
	h.seedVersion(ver)

	event := defaultDPEvent(orgID, docID, versionID, jobID, "corr-008")
	err := h.ingestion.HandleDPArtifacts(context.Background(), event)
	if err == nil {
		t.Fatal("expected error for invalid status transition, got nil")
	}

	// Blobs should have been compensated (deleted from object storage).
	// Compensation is async with context.Background() + 30s timeout,
	// but in our sync in-memory implementation it completes immediately.
	if got := h.objectStorage.blobCount(); got != 0 {
		t.Errorf("expected 0 blobs after compensation, got %d (compensation may not have run)", got)
	}
}

// ---------------------------------------------------------------------------
// Test: context cancellation before ingestion
// ---------------------------------------------------------------------------

func TestDPIngestion_ContextCancelled(t *testing.T) {
	const (
		orgID     = "org-009"
		docID     = "doc-009"
		versionID = "ver-009"
		jobID     = "job-dp-009"
	)

	h := newTestHarness(t)
	h.seedDocument(defaultDocument(orgID, docID))
	h.seedVersion(defaultVersion(orgID, docID, versionID))

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Immediately cancel the context.

	event := defaultDPEvent(orgID, docID, versionID, jobID, "corr-009")
	err := h.ingestion.HandleDPArtifacts(ctx, event)
	if err == nil {
		t.Fatal("expected error for cancelled context, got nil")
	}

	// W-5: verify no side effects on cancelled context.
	assertIntEqual(t, "artifacts after cancel", 0, len(h.artifactRepo.allArtifacts()))
	assertIntEqual(t, "outbox after cancel", 0, len(h.outboxRepo.allEntries()))
	assertIntEqual(t, "audit after cancel", 0, len(h.auditRepo.allRecords()))
}

// ---------------------------------------------------------------------------
// Test: audit details contain correct metadata
// ---------------------------------------------------------------------------

func TestDPIngestion_AuditDetails(t *testing.T) {
	const (
		orgID     = "org-010"
		docID     = "doc-010"
		versionID = "ver-010"
		jobID     = "job-dp-010"
	)

	h := newTestHarness(t)
	h.seedDocument(defaultDocument(orgID, docID))
	h.seedVersion(defaultVersion(orgID, docID, versionID))

	event := defaultDPEvent(orgID, docID, versionID, jobID, "corr-010")
	err := h.ingestion.HandleDPArtifacts(context.Background(), event)
	if err != nil {
		t.Fatalf("HandleDPArtifacts returned error: %v", err)
	}

	for _, rec := range h.auditRepo.allRecords() {
		if rec.Action == model.AuditActionArtifactSaved {
			var details map[string]any
			if err := json.Unmarshal(rec.Details, &details); err != nil {
				t.Fatalf("failed to unmarshal ARTIFACT_SAVED details: %v", err)
			}
			assertEqual(t, "details.producer", "DP", details["producer"].(string))
			count := details["artifact_count"].(float64) // JSON numbers are float64
			assertIntEqual(t, "details.artifact_count", 4, int(count))
		}
		if rec.Action == model.AuditActionArtifactStatusChanged {
			var details map[string]any
			if err := json.Unmarshal(rec.Details, &details); err != nil {
				t.Fatalf("failed to unmarshal ARTIFACT_STATUS_CHANGED details: %v", err)
			}
			assertEqual(t, "details.from", "PENDING", details["from"].(string))
			assertEqual(t, "details.to", "PROCESSING_ARTIFACTS_RECEIVED", details["to"].(string))
		}
	}
}

// ---------------------------------------------------------------------------
// Test: storage keys follow naming convention
// ---------------------------------------------------------------------------

func TestDPIngestion_StorageKeyConvention(t *testing.T) {
	const (
		orgID     = "org-011"
		docID     = "doc-011"
		versionID = "ver-011"
		jobID     = "job-dp-011"
	)

	h := newTestHarness(t)
	h.seedDocument(defaultDocument(orgID, docID))
	h.seedVersion(defaultVersion(orgID, docID, versionID))

	event := defaultDPEvent(orgID, docID, versionID, jobID, "corr-011")
	err := h.ingestion.HandleDPArtifacts(context.Background(), event)
	if err != nil {
		t.Fatalf("HandleDPArtifacts returned error: %v", err)
	}

	for _, a := range h.artifactRepo.allArtifacts() {
		expectedKey := objectstorage.ArtifactKey(orgID, docID, versionID, a.ArtifactType)
		assertEqual(t, "storage_key for "+string(a.ArtifactType), expectedKey, a.StorageKey)
	}
}

// ---------------------------------------------------------------------------
// Test: document not found → error returned
// ---------------------------------------------------------------------------

func TestDPIngestion_FallbackDocumentNotFound(t *testing.T) {
	const (
		docID   = "doc-012-missing"
		jobID   = "job-dp-012"
	)

	h := newTestHarness(t)
	// Document NOT registered in fallback resolver.
	// Event has no org_id → triggers fallback → document not found.

	event := defaultDPEvent("", docID, "ver-012", jobID, "corr-012")
	event.OrgID = "" // Force fallback.

	err := h.ingestion.HandleDPArtifacts(context.Background(), event)
	if err == nil {
		t.Fatal("expected error when fallback resolver cannot find document, got nil")
	}

	if !port.IsNotFound(err) {
		t.Errorf("expected not-found error, got: %v", err)
	}

	// No side effects.
	assertIntEqual(t, "artifacts", 0, len(h.artifactRepo.allArtifacts()))
	assertIntEqual(t, "outbox", 0, len(h.outboxRepo.allEntries()))
}

// ---------------------------------------------------------------------------
// Test: transactional intent — WithTransaction called exactly once
// ---------------------------------------------------------------------------

func TestDPIngestion_TransactionalIntent(t *testing.T) {
	const (
		orgID     = "org-013"
		docID     = "doc-013"
		versionID = "ver-013"
		jobID     = "job-dp-013"
	)

	h := newTestHarness(t)
	h.seedDocument(defaultDocument(orgID, docID))
	h.seedVersion(defaultVersion(orgID, docID, versionID))

	event := defaultDPEvent(orgID, docID, versionID, jobID, "corr-013")
	err := h.ingestion.HandleDPArtifacts(context.Background(), event)
	if err != nil {
		t.Fatalf("HandleDPArtifacts returned error: %v", err)
	}

	// B-1: verify transactional intent.
	if got := h.transactor.txCallCount(); got != 1 {
		t.Errorf("expected 1 WithTransaction call, got %d", got)
	}
}

// ---------------------------------------------------------------------------
// Test: end-to-end idempotency — second HandleDPArtifacts call after completed
// ---------------------------------------------------------------------------

func TestDPIngestion_EndToEndIdempotency(t *testing.T) {
	const (
		orgID     = "org-014"
		docID     = "doc-014"
		versionID = "ver-014"
		jobID     = "job-dp-014"
	)

	h := newTestHarness(t)
	h.seedDocument(defaultDocument(orgID, docID))
	h.seedVersion(defaultVersion(orgID, docID, versionID))

	event := defaultDPEvent(orgID, docID, versionID, jobID, "corr-014")

	// First delivery — through idempotency guard + handler.
	idemKey := idempotency.KeyForDPArtifacts(jobID)

	result, err := h.idempotencyGuard.Check(context.Background(), idemKey, model.TopicDPArtifactsProcessingReady, nil)
	if err != nil {
		t.Fatalf("Check #1: %v", err)
	}
	if result != idempotency.ResultProcess {
		t.Fatalf("expected ResultProcess, got %d", result)
	}

	err = h.ingestion.HandleDPArtifacts(context.Background(), event)
	if err != nil {
		t.Fatalf("HandleDPArtifacts #1: %v", err)
	}
	_ = h.idempotencyGuard.MarkCompleted(context.Background(), idemKey)

	snapshotArtifacts := len(h.artifactRepo.allArtifacts())
	snapshotOutbox := len(h.outboxRepo.allEntries())
	snapshotAudit := len(h.auditRepo.allRecords())

	// Second delivery — guard returns Skip; handler NOT called.
	result2, err := h.idempotencyGuard.Check(context.Background(), idemKey, model.TopicDPArtifactsProcessingReady, nil)
	if err != nil {
		t.Fatalf("Check #2: %v", err)
	}
	if result2 != idempotency.ResultSkip {
		t.Fatalf("expected ResultSkip on duplicate, got %d", result2)
	}

	// B-3: Verify no side effects after dedup.
	assertIntEqual(t, "artifacts unchanged", snapshotArtifacts, len(h.artifactRepo.allArtifacts()))
	assertIntEqual(t, "outbox unchanged", snapshotOutbox, len(h.outboxRepo.allEntries()))
	assertIntEqual(t, "audit unchanged", snapshotAudit, len(h.auditRepo.allRecords()))
}

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

func assertEqual(t *testing.T, field, expected, actual string) {
	t.Helper()
	if expected != actual {
		t.Errorf("%s: expected %q, got %q", field, expected, actual)
	}
}

func assertIntEqual(t *testing.T, field string, expected, actual int) {
	t.Helper()
	if expected != actual {
		t.Errorf("%s: expected %d, got %d", field, expected, actual)
	}
}

