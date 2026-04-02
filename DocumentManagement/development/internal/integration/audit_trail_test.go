package integration

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"contractpro/document-management/internal/domain/model"
)

// waitForAuditRecord polls the audit repo until a record matching action and jobID appears.
func waitForAuditRecord(t *testing.T, repo *memoryAuditRepository, action model.AuditAction, jobID string) *model.AuditRecord {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		for _, rec := range repo.allRecords() {
			if rec.Action == action && (jobID == "" || rec.JobID == jobID) {
				return rec
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for audit action %q (jobID=%q)", action, jobID)
	return nil
}

// ---------------------------------------------------------------------------
// Test: All action types produced by DP → LIC → RE pipeline + diff
// Verifies each action type has correct ActorType, ActorID, and details.
// ---------------------------------------------------------------------------

func TestAuditTrail_AllActionTypes(t *testing.T) {
	const (
		orgID         = "org-audit-001"
		docID         = "doc-audit-001"
		versionID     = "ver-audit-001"
		versionID2    = "ver-audit-002"
		dpJobID       = "job-audit-dp"
		licJobID      = "job-audit-lic"
		reJobID       = "job-audit-re"
		diffJobID     = "job-audit-diff"
		correlationDP = "corr-audit-dp"
		correlationRE = "corr-audit-re"
	)

	h := newTestHarness(t)
	h.seedDocument(defaultDocument(orgID, docID))
	h.seedVersion(defaultVersion(orgID, docID, versionID))
	h.seedVersion(defaultVersion(orgID, docID, versionID2))

	// DP artifacts → ARTIFACT_SAVED + ARTIFACT_STATUS_CHANGED.
	if err := h.ingestion.HandleDPArtifacts(context.Background(),
		defaultDPEvent(orgID, docID, versionID, dpJobID, correlationDP)); err != nil {
		t.Fatalf("HandleDPArtifacts: %v", err)
	}

	// LIC artifacts → ARTIFACT_SAVED + ARTIFACT_STATUS_CHANGED.
	if err := h.ingestion.HandleLICArtifacts(context.Background(),
		defaultLICEvent(orgID, docID, versionID, licJobID, "corr-lic")); err != nil {
		t.Fatalf("HandleLICArtifacts: %v", err)
	}

	// RE artifacts → ARTIFACT_SAVED + ARTIFACT_STATUS_CHANGED.
	if err := h.ingestion.HandleREArtifacts(context.Background(),
		defaultREEvent(h, orgID, docID, versionID, reJobID, correlationRE)); err != nil {
		t.Fatalf("HandleREArtifacts: %v", err)
	}

	// Diff → DIFF_SAVED.
	diffEvent := model.DocumentVersionDiffReady{
		EventMeta: model.EventMeta{
			CorrelationID: "corr-diff",
			Timestamp:     time.Now().UTC(),
		},
		JobID:           diffJobID,
		DocumentID:      docID,
		BaseVersionID:   versionID,
		TargetVersionID: versionID2,
		OrgID:           orgID,
		TextDiffs:       json.RawMessage(`[{"path":"1.1","old":"a","new":"b"}]`),
		StructuralDiffs: json.RawMessage(`[{"type":"added","path":"2"}]`),
	}
	if err := h.diffService.HandleDiffReady(context.Background(), diffEvent); err != nil {
		t.Fatalf("HandleDiffReady: %v", err)
	}

	records := h.auditRepo.allRecords()

	// Expect: 3×ARTIFACT_SAVED + 3×ARTIFACT_STATUS_CHANGED + 1×DIFF_SAVED = 7.
	assertIntEqual(t, "total audit records", 7, len(records))

	// Count each action type.
	actionCounts := make(map[model.AuditAction]int)
	for _, rec := range records {
		actionCounts[rec.Action]++
	}
	assertIntEqual(t, "ARTIFACT_SAVED count", 3, actionCounts[model.AuditActionArtifactSaved])
	assertIntEqual(t, "ARTIFACT_STATUS_CHANGED count", 3, actionCounts[model.AuditActionArtifactStatusChanged])
	assertIntEqual(t, "DIFF_SAVED count", 1, actionCounts[model.AuditActionDiffSaved])

	// Verify ARTIFACT_SAVED records have ActorType=DOMAIN.
	for _, rec := range records {
		if rec.Action == model.AuditActionArtifactSaved {
			assertEqual(t, "ARTIFACT_SAVED.ActorType", string(model.ActorTypeDomain), string(rec.ActorType))
			if rec.ActorID != "DP" && rec.ActorID != "LIC" && rec.ActorID != "RE" {
				t.Errorf("ARTIFACT_SAVED.ActorID = %q, want DP/LIC/RE", rec.ActorID)
			}
			if rec.OrganizationID != orgID {
				t.Errorf("ARTIFACT_SAVED.OrganizationID = %q, want %q", rec.OrganizationID, orgID)
			}
		}
	}

	// Verify DIFF_SAVED has ActorType=DOMAIN, ActorID=DP.
	for _, rec := range records {
		if rec.Action == model.AuditActionDiffSaved {
			assertEqual(t, "DIFF_SAVED.ActorType", string(model.ActorTypeDomain), string(rec.ActorType))
			assertEqual(t, "DIFF_SAVED.ActorID", "DP", rec.ActorID)
			assertEqual(t, "DIFF_SAVED.DocumentID", docID, rec.DocumentID)
			if rec.AuditID == "" {
				t.Error("DIFF_SAVED.AuditID is empty")
			}
			if rec.CreatedAt.IsZero() {
				t.Error("DIFF_SAVED.CreatedAt is zero")
			}
		}
	}

	// Verify all records have non-empty AuditID and non-zero CreatedAt.
	for i, rec := range records {
		if rec.AuditID == "" {
			t.Errorf("record[%d].AuditID is empty", i)
		}
		if rec.CreatedAt.IsZero() {
			t.Errorf("record[%d].CreatedAt is zero", i)
		}
		if rec.OrganizationID == "" {
			t.Errorf("record[%d].OrganizationID is empty", i)
		}
	}
}

// ---------------------------------------------------------------------------
// Test: ARTIFACT_SAVED details contain producer, artifact_types, artifact_count
// ---------------------------------------------------------------------------

func TestAuditTrail_ArtifactSavedDetails(t *testing.T) {
	const (
		orgID     = "org-audit-002"
		docID     = "doc-audit-002"
		versionID = "ver-audit-002"
	)

	h := newTestHarness(t)
	h.seedDocument(defaultDocument(orgID, docID))
	h.seedVersion(defaultVersion(orgID, docID, versionID))

	if err := h.ingestion.HandleDPArtifacts(context.Background(),
		defaultDPEvent(orgID, docID, versionID, "job-002", "corr-002")); err != nil {
		t.Fatalf("HandleDPArtifacts: %v", err)
	}

	for _, rec := range h.auditRepo.allRecords() {
		if rec.Action != model.AuditActionArtifactSaved {
			continue
		}
		if rec.Details == nil {
			t.Fatal("ARTIFACT_SAVED details is nil")
		}

		var detail struct {
			Producer      string   `json:"producer"`
			ArtifactTypes []string `json:"artifact_types"`
			ArtifactCount int      `json:"artifact_count"`
		}
		if err := json.Unmarshal(rec.Details, &detail); err != nil {
			t.Fatalf("unmarshal ARTIFACT_SAVED details: %v", err)
		}
		assertEqual(t, "detail.Producer", "DP", detail.Producer)
		if detail.ArtifactCount == 0 {
			t.Error("detail.ArtifactCount is 0")
		}
		if len(detail.ArtifactTypes) == 0 {
			t.Error("detail.ArtifactTypes is empty")
		}
		return // Found and verified.
	}
	t.Error("no ARTIFACT_SAVED record found")
}

// ---------------------------------------------------------------------------
// Test: ARTIFACT_STATUS_CHANGED details contain from/to
// ---------------------------------------------------------------------------

func TestAuditTrail_StatusChangedDetails(t *testing.T) {
	const (
		orgID     = "org-audit-003"
		docID     = "doc-audit-003"
		versionID = "ver-audit-003"
	)

	h := newTestHarness(t)
	h.seedDocument(defaultDocument(orgID, docID))
	h.seedVersion(defaultVersion(orgID, docID, versionID))

	if err := h.ingestion.HandleDPArtifacts(context.Background(),
		defaultDPEvent(orgID, docID, versionID, "job-003", "corr-003")); err != nil {
		t.Fatalf("HandleDPArtifacts: %v", err)
	}

	for _, rec := range h.auditRepo.allRecords() {
		if rec.Action != model.AuditActionArtifactStatusChanged {
			continue
		}
		var detail struct {
			From string `json:"from"`
			To   string `json:"to"`
		}
		if err := json.Unmarshal(rec.Details, &detail); err != nil {
			t.Fatalf("unmarshal ARTIFACT_STATUS_CHANGED details: %v", err)
		}
		assertEqual(t, "detail.From", "PENDING", detail.From)
		assertEqual(t, "detail.To", "PROCESSING_ARTIFACTS_RECEIVED", detail.To)
		return // Found and verified.
	}
	t.Error("no ARTIFACT_STATUS_CHANGED record found")
}

// ---------------------------------------------------------------------------
// Test: Async ARTIFACT_READ audit for HandleGetSemanticTree
// ---------------------------------------------------------------------------

func TestAuditTrail_AsyncArtifactRead_SemanticTree(t *testing.T) {
	const (
		orgID         = "org-audit-004"
		docID         = "doc-audit-004"
		versionID     = "ver-audit-004"
		correlationID = "corr-audit-004"
	)

	h, _ := newTestHarnessWithRecordingPublisher(t)
	h.seedDocument(defaultDocument(orgID, docID))
	h.seedVersion(defaultVersion(orgID, docID, versionID))

	// Ingest DP artifacts first (so semantic tree exists).
	if err := h.ingestion.HandleDPArtifacts(context.Background(),
		defaultDPEvent(orgID, docID, versionID, "job-dp-004", "corr-dp-004")); err != nil {
		t.Fatalf("HandleDPArtifacts: %v", err)
	}

	// Query semantic tree.
	request := model.GetSemanticTreeRequest{
		EventMeta:  model.EventMeta{CorrelationID: correlationID, Timestamp: time.Now().UTC()},
		JobID:      "job-query-004",
		DocumentID: docID,
		VersionID:  versionID,
		OrgID:      orgID,
	}
	if err := h.query.HandleGetSemanticTree(context.Background(), request); err != nil {
		t.Fatalf("HandleGetSemanticTree: %v", err)
	}

	// Poll for async audit record (goroutine writes with 5s timeout).
	rec := waitForAuditRecord(t, h.auditRepo, model.AuditActionArtifactRead, "job-query-004")
	assertEqual(t, "ARTIFACT_READ.ActorType", string(model.ActorTypeDomain), string(rec.ActorType))
	assertEqual(t, "ARTIFACT_READ.ActorID", "DP", rec.ActorID)
	assertEqual(t, "ARTIFACT_READ.DocumentID", docID, rec.DocumentID)
	assertEqual(t, "ARTIFACT_READ.VersionID", versionID, rec.VersionID)
}

// ---------------------------------------------------------------------------
// Test: Async ARTIFACT_READ audit for HandleGetArtifacts (LIC → requester=RE)
// ---------------------------------------------------------------------------

func TestAuditTrail_AsyncArtifactRead_GetArtifacts_LIC(t *testing.T) {
	const (
		orgID         = "org-audit-005"
		docID         = "doc-audit-005"
		versionID     = "ver-audit-005"
		correlationID = "corr-audit-005"
	)

	h, _ := newTestHarnessWithRecordingPublisher(t)
	h.seedDocument(defaultDocument(orgID, docID))
	h.seedVersion(defaultVersion(orgID, docID, versionID))

	// Ingest DP then LIC artifacts.
	if err := h.ingestion.HandleDPArtifacts(context.Background(),
		defaultDPEvent(orgID, docID, versionID, "job-dp-005", "corr-dp-005")); err != nil {
		t.Fatalf("HandleDPArtifacts: %v", err)
	}
	if err := h.ingestion.HandleLICArtifacts(context.Background(),
		defaultLICEvent(orgID, docID, versionID, "job-lic-005", "corr-lic-005")); err != nil {
		t.Fatalf("HandleLICArtifacts: %v", err)
	}

	// Query LIC artifacts (requester should be inferred as "RE").
	request := model.GetArtifactsRequest{
		EventMeta:  model.EventMeta{CorrelationID: correlationID, Timestamp: time.Now().UTC()},
		JobID:      "job-query-005",
		DocumentID: docID,
		VersionID:  versionID,
		OrgID:      orgID,
		ArtifactTypes: []model.ArtifactType{
			model.ArtifactTypeClassificationResult,
			model.ArtifactTypeRiskAnalysis,
		},
	}
	if err := h.query.HandleGetArtifacts(context.Background(), request); err != nil {
		t.Fatalf("HandleGetArtifacts: %v", err)
	}

	// Poll for async audit record (goroutine writes with 5s timeout).
	rec := waitForAuditRecord(t, h.auditRepo, model.AuditActionArtifactRead, "job-query-005")
	assertEqual(t, "ARTIFACT_READ.ActorType", string(model.ActorTypeDomain), string(rec.ActorType))
	// LIC artifacts → requester is RE.
	assertEqual(t, "ARTIFACT_READ.ActorID", "RE", rec.ActorID)
}

// ---------------------------------------------------------------------------
// Test: DIFF_SAVED details contain base/target version IDs and counts
// ---------------------------------------------------------------------------

func TestAuditTrail_DiffSavedDetails(t *testing.T) {
	const (
		orgID      = "org-audit-006"
		docID      = "doc-audit-006"
		versionID1 = "ver-audit-006a"
		versionID2 = "ver-audit-006b"
	)

	h := newTestHarness(t)
	h.seedDocument(defaultDocument(orgID, docID))
	h.seedVersion(defaultVersion(orgID, docID, versionID1))
	h.seedVersion(defaultVersion(orgID, docID, versionID2))

	diffEvent := model.DocumentVersionDiffReady{
		EventMeta: model.EventMeta{
			CorrelationID: "corr-diff-006",
			Timestamp:     time.Now().UTC(),
		},
		JobID:           "job-diff-006",
		DocumentID:      docID,
		BaseVersionID:   versionID1,
		TargetVersionID: versionID2,
		OrgID:           orgID,
		TextDiffs:       json.RawMessage(`[{"path":"1","old":"x","new":"y"}]`),
		StructuralDiffs: json.RawMessage(`[]`),
	}
	if err := h.diffService.HandleDiffReady(context.Background(), diffEvent); err != nil {
		t.Fatalf("HandleDiffReady: %v", err)
	}

	for _, rec := range h.auditRepo.allRecords() {
		if rec.Action != model.AuditActionDiffSaved {
			continue
		}
		var detail struct {
			BaseVersionID   string `json:"base_version_id"`
			TargetVersionID string `json:"target_version_id"`
			StorageKey      string `json:"storage_key"`
			ContentHash     string `json:"content_hash"`
			SizeBytes       int    `json:"size_bytes"`
		}
		if err := json.Unmarshal(rec.Details, &detail); err != nil {
			t.Fatalf("unmarshal DIFF_SAVED details: %v", err)
		}
		assertEqual(t, "detail.BaseVersionID", versionID1, detail.BaseVersionID)
		assertEqual(t, "detail.TargetVersionID", versionID2, detail.TargetVersionID)
		if detail.StorageKey == "" {
			t.Error("detail.StorageKey is empty")
		}
		if detail.ContentHash == "" {
			t.Error("detail.ContentHash is empty")
		}
		if detail.SizeBytes == 0 {
			t.Error("detail.SizeBytes is 0")
		}
		return
	}
	t.Error("no DIFF_SAVED record found")
}

// ---------------------------------------------------------------------------
// Test: Append-only — audit repo only exposes Insert and List
// (This is a compile-time + behavioral verification)
// ---------------------------------------------------------------------------

func TestAuditTrail_AppendOnly_NoUpdateOrDelete(t *testing.T) {
	h := newTestHarness(t)

	rec := model.NewAuditRecord("a-append-1", "org-append", model.AuditActionDocumentCreated, model.ActorTypeUser, "user-1")
	if err := h.auditRepo.Insert(context.Background(), rec); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	// Verify record exists.
	records := h.auditRepo.allRecords()
	assertIntEqual(t, "records count", 1, len(records))
	assertEqual(t, "action", string(model.AuditActionDocumentCreated), string(records[0].Action))

	// The AuditRepository interface only has Insert and List — no Update, Delete,
	// or any mutation method. This is enforced by the port definition.
	// The compile-time check in testinfra.go (var _ port.AuditRepository = ...)
	// ensures the in-memory implementation satisfies the port.
}
