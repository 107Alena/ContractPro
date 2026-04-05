package integration

import (
	"context"
	"fmt"
	"io"
	"sync"
	"testing"
	"time"

	"contractpro/document-management/internal/application/ingestion"
	appVersion "contractpro/document-management/internal/application/version"
	"contractpro/document-management/internal/config"
	"contractpro/document-management/internal/domain/model"
	"contractpro/document-management/internal/domain/port"
	"contractpro/document-management/internal/egress/outbox"
	"contractpro/document-management/internal/ingress/idempotency"
)

// =========================================================================
// Scenario 1: Object Storage fail on 4th artifact -> compensation -> retry
// =========================================================================

func TestErrorScenario_ObjectStorageFailOnFourthArtifact_CompensationAndRetry(t *testing.T) {
	const (
		orgID         = "org-err-001"
		docID         = "doc-err-001"
		versionID     = "ver-err-001"
		jobID         = "job-err-001"
		correlationID = "corr-err-001"
	)

	// Build custom harness with failingObjectStorage.
	transactor := newMemoryTransactor()
	versionRepo := newMemoryVersionRepository()
	artifactRepo := newMemoryArtifactRepository()
	auditRepo := newMemoryAuditRepository()
	outboxRepo := newMemoryOutboxRepository()
	fallback := newMemoryFallbackResolver()
	logger := newRecordingLogger()

	innerStorage := newMemoryObjectStorage()
	// Fail on the 4th PutObject call (SEMANTIC_TREE, order: OCR_RAW, EXTRACTED_TEXT, DOCUMENT_STRUCTURE, SEMANTIC_TREE).
	failStorage := newFailingObjectStorage(innerStorage, 4)

	docRepo := newMemoryDocumentRepository()
	outboxWriter := outbox.NewOutboxWriter(outboxRepo)
	orphanInserter := newRecordingOrphanInserter()
	ingestionSvc := ingestion.NewArtifactIngestionService(
		transactor, versionRepo, artifactRepo, auditRepo,
		failStorage, outboxWriter, fallback, &noopFallbackMetrics{},
		docRepo, &noopTenantMetrics{}, orphanInserter, logger,
		10*1024*1024, 100*1024*1024,
	)

	// Seed document (for tenant ownership check) and version in PENDING status.
	docRepo.store(defaultDocument(orgID, docID))
	ver := defaultVersion(orgID, docID, versionID)
	versionRepo.store(ver)

	event := defaultDPEvent(orgID, docID, versionID, jobID, correlationID)

	// --- First call: expect failure on 4th artifact ---
	err := ingestionSvc.HandleDPArtifacts(context.Background(), event)
	if err == nil {
		t.Fatal("expected error when object storage fails on 4th artifact, got nil")
	}

	// Assert: blobs 1-3 were compensated (deleted from object storage).
	if got := innerStorage.blobCount(); got != 0 {
		t.Errorf("expected 0 blobs after compensation, got %d", got)
	}

	// Assert: no artifact descriptors saved (transaction never started or rolled back).
	if got := len(artifactRepo.allArtifacts()); got != 0 {
		t.Errorf("expected 0 artifact descriptors after failure, got %d", got)
	}

	// Assert: no outbox entries.
	if got := len(outboxRepo.allEntries()); got != 0 {
		t.Errorf("expected 0 outbox entries after failure, got %d", got)
	}

	// Assert: version status unchanged (still PENDING).
	verAfter, findErr := versionRepo.FindByID(context.Background(), orgID, docID, versionID)
	if findErr != nil {
		t.Fatalf("FindByID after failure: %v", findErr)
	}
	assertEqual(t, "version.ArtifactStatus after failure",
		string(model.ArtifactStatusPending), string(verAfter.ArtifactStatus))

	// --- Second call: retry with storage working ---
	failStorage.disableFailure()

	err = ingestionSvc.HandleDPArtifacts(context.Background(), event)
	if err != nil {
		t.Fatalf("HandleDPArtifacts retry returned error: %v", err)
	}

	// Assert: all 4 blobs saved successfully.
	if got := innerStorage.blobCount(); got != 4 {
		t.Errorf("expected 4 blobs after retry, got %d", got)
	}

	// Assert: 4 artifact descriptors created.
	if got := len(artifactRepo.allArtifacts()); got != 4 {
		t.Errorf("expected 4 artifact descriptors after retry, got %d", got)
	}

	// Assert: version status transitioned to PROCESSING_ARTIFACTS_RECEIVED.
	verRetry, findErr := versionRepo.FindByID(context.Background(), orgID, docID, versionID)
	if findErr != nil {
		t.Fatalf("FindByID after retry: %v", findErr)
	}
	assertEqual(t, "version.ArtifactStatus after retry",
		string(model.ArtifactStatusProcessingArtifactsReceived), string(verRetry.ArtifactStatus))

	// Assert: 2 outbox entries (confirmation + notification).
	if got := len(outboxRepo.allEntries()); got != 2 {
		t.Errorf("expected 2 outbox entries after retry, got %d", got)
	}
}

// =========================================================================
// Scenario 2: Concurrent version creation -> both succeed with different
//             version_numbers (1 and 2)
// =========================================================================

func TestErrorScenario_ConcurrentVersionCreation_BothSucceed(t *testing.T) {
	const (
		orgID = "org-err-002"
		docID = "doc-err-002"
	)

	// Build VersionManagementService with conflicting version repo.
	transactor := newMemoryTransactor()
	docRepo := newMemoryDocumentRepository()
	innerVersionRepo := newMemoryVersionRepository()
	conflictRepo := newConflictingVersionRepository(innerVersionRepo)
	auditRepo := newMemoryAuditRepository()
	outboxRepo := newMemoryOutboxRepository()
	outboxWriter := outbox.NewOutboxWriter(outboxRepo)
	logger := newRecordingLogger()

	versionSvc := appVersion.NewVersionManagementService(
		transactor, docRepo, conflictRepo, auditRepo, outboxWriter, logger,
	)

	// Seed an ACTIVE document.
	doc := defaultDocument(orgID, docID)
	docRepo.store(doc)

	// Create version params for two concurrent requests.
	params1 := port.CreateVersionParams{
		OrganizationID:     orgID,
		DocumentID:         docID,
		OriginType:         model.OriginTypeUpload,
		SourceFileKey:      "source/file1.pdf",
		SourceFileName:     "contract_v1.pdf",
		SourceFileSize:     10000,
		SourceFileChecksum: "sha256-aaa",
		CreatedByUserID:    "user-001",
	}

	params2 := port.CreateVersionParams{
		OrganizationID:     orgID,
		DocumentID:         docID,
		OriginType:         model.OriginTypeReUpload,
		SourceFileKey:      "source/file2.pdf",
		SourceFileName:     "contract_v2.pdf",
		SourceFileSize:     20000,
		SourceFileChecksum: "sha256-bbb",
		CreatedByUserID:    "user-002",
	}

	// Run concurrently with a sync barrier.
	var (
		wg   sync.WaitGroup
		ver1 *model.DocumentVersion
		ver2 *model.DocumentVersion
		err1 error
		err2 error
	)

	wg.Add(2)
	start := make(chan struct{})

	go func() {
		defer wg.Done()
		<-start
		ver1, err1 = versionSvc.CreateVersion(context.Background(), params1)
	}()

	go func() {
		defer wg.Done()
		<-start
		ver2, err2 = versionSvc.CreateVersion(context.Background(), params2)
	}()

	close(start)
	wg.Wait()

	// Both must succeed.
	if err1 != nil {
		t.Fatalf("CreateVersion #1 failed: %v", err1)
	}
	if err2 != nil {
		t.Fatalf("CreateVersion #2 failed: %v", err2)
	}

	// Verify 2 versions exist.
	versions, total, listErr := innerVersionRepo.List(context.Background(), orgID, docID, 1, 10)
	if listErr != nil {
		t.Fatalf("List versions: %v", listErr)
	}
	assertIntEqual(t, "total versions", 2, total)
	assertIntEqual(t, "listed versions", 2, len(versions))

	// Verify version numbers are {1, 2} (order may vary).
	versionNumbers := map[int]bool{
		ver1.VersionNumber: true,
		ver2.VersionNumber: true,
	}
	if !versionNumbers[1] || !versionNumbers[2] {
		t.Errorf("expected version_numbers {1, 2}, got {%d, %d}",
			ver1.VersionNumber, ver2.VersionNumber)
	}

	// Verify the two version IDs are different.
	if ver1.VersionID == ver2.VersionID {
		t.Error("expected different version IDs, got identical")
	}

	// Verify audit records: 2 VERSION_CREATED.
	auditRecords := auditRepo.allRecords()
	versionCreatedCount := 0
	for _, rec := range auditRecords {
		if rec.Action == model.AuditActionVersionCreated {
			versionCreatedCount++
		}
	}
	assertIntEqual(t, "VERSION_CREATED audit records", 2, versionCreatedCount)

	// Verify outbox: 2 entries for dm.events.version-created.
	outboxEntries := outboxRepo.allEntries()
	versionCreatedOutbox := 0
	for _, entry := range outboxEntries {
		if entry.Topic == model.TopicDMEventsVersionCreated {
			versionCreatedOutbox++
		}
	}
	assertIntEqual(t, "version-created outbox entries", 2, versionCreatedOutbox)
}

// =========================================================================
// Scenario 3: Document not found -> error, NO blobs, NO descriptors, NO outbox
// =========================================================================

func TestErrorScenario_DocumentNotFound_NoBlobsNoDescriptors(t *testing.T) {
	const (
		orgID     = "org-err-003"
		docID     = "doc-err-003-missing"
		versionID = "ver-err-003"
		jobID     = "job-err-003"
	)

	h := newTestHarness(t)
	// Neither document nor version is seeded — simulating non-existent document.
	// With tenant isolation (BRE-015), the ownership check fires first and
	// rejects the event with TENANT_MISMATCH because the document does not
	// exist under the claimed organization.

	event := defaultDPEvent(orgID, docID, versionID, jobID, "corr-err-003")

	err := h.ingestion.HandleDPArtifacts(context.Background(), event)
	if err == nil {
		t.Fatal("expected error for non-existent document/version, got nil")
	}

	// With tenant isolation the ownership check rejects the event before
	// the version lookup, so the error code is TENANT_MISMATCH.
	if code := port.ErrorCode(err); code != port.ErrCodeTenantMismatch {
		t.Errorf("expected error code %s, got %s (err: %v)", port.ErrCodeTenantMismatch, code, err)
	}

	// Assert: NO blobs in object storage (compensation ran).
	// The ingestion service saves blobs BEFORE the transaction, then the
	// transaction fails at FindByIDForUpdate -> compensation deletes the blobs.
	if got := h.objectStorage.blobCount(); got != 0 {
		t.Errorf("expected 0 blobs after compensation, got %d", got)
	}

	// Assert: NO artifact descriptors.
	if got := len(h.artifactRepo.allArtifacts()); got != 0 {
		t.Errorf("expected 0 artifact descriptors, got %d", got)
	}

	// Assert: NO outbox entries.
	if got := len(h.outboxRepo.allEntries()); got != 0 {
		t.Errorf("expected 0 outbox entries, got %d", got)
	}

	// Assert: NO audit records.
	if got := len(h.auditRepo.allRecords()); got != 0 {
		t.Errorf("expected 0 audit records, got %d", got)
	}
}

// =========================================================================
// Scenario 4: Redis unavailable -> fallback to DB -> success
// =========================================================================

func TestErrorScenario_RedisUnavailable_FallbackToDB_Success(t *testing.T) {
	const (
		orgID         = "org-err-004"
		docID         = "doc-err-004"
		versionID     = "ver-err-004"
		jobID         = "job-err-004"
		correlationID = "corr-err-004"
	)

	h := newTestHarness(t)
	h.seedDocument(defaultDocument(orgID, docID))
	h.seedVersion(defaultVersion(orgID, docID, versionID))

	// Create idempotency guard with a failing store (simulates Redis down).
	failStore := &failingIdempotencyStore{}
	failGuard := idempotency.NewIdempotencyGuard(
		failStore,
		config.IdempotencyConfig{
			TTL:            24 * time.Hour,
			ProcessingTTL:  120 * time.Second,
			StuckThreshold: 240 * time.Second,
		},
		&noopIdempotencyMetrics{},
		h.logger,
	)

	// Create a FallbackChecker that queries the artifact repo for existing DP
	// artifacts. Since no artifacts have been ingested yet, it returns false
	// (not processed) -> the guard returns ResultProcess.
	fallbackChecker := idempotency.ArtifactFallback(
		h.artifactRepo, orgID, docID, versionID, jobID, model.ProducerDomainDP,
	)

	idemKey := idempotency.KeyForDPArtifacts(jobID)
	result, err := failGuard.Check(
		context.Background(), idemKey, model.TopicDPArtifactsProcessingReady, fallbackChecker,
	)
	if err != nil {
		t.Fatalf("idempotency Check with failing Redis returned error: %v", err)
	}
	if result != idempotency.ResultProcess {
		t.Fatalf("expected ResultProcess on Redis failure with DB fallback, got %v", result)
	}

	// Process the event (ingestion service uses in-memory fakes, not Redis).
	event := defaultDPEvent(orgID, docID, versionID, jobID, correlationID)
	if err := h.ingestion.HandleDPArtifacts(context.Background(), event); err != nil {
		t.Fatalf("HandleDPArtifacts after fallback returned error: %v", err)
	}

	// Assert: artifacts saved correctly.
	assertIntEqual(t, "artifacts", 4, len(h.artifactRepo.allArtifacts()))
	assertIntEqual(t, "blobs", 4, h.objectStorage.blobCount())

	// Assert: version status transitioned.
	ver, findErr := h.versionRepo.FindByID(context.Background(), orgID, docID, versionID)
	if findErr != nil {
		t.Fatalf("FindByID: %v", findErr)
	}
	assertEqual(t, "version.ArtifactStatus",
		string(model.ArtifactStatusProcessingArtifactsReceived), string(ver.ArtifactStatus))

	// Assert: 2 outbox entries (confirmation + notification).
	assertIntEqual(t, "outbox entries", 2, len(h.outboxRepo.allEntries()))

	// Assert: logger recorded the Redis fallback warning.
	if !h.logger.hasMessage("redis unavailable") {
		t.Error("expected log message about Redis unavailability (fallback)")
	}
}

// =========================================================================
// Scenario 5: Terminal status -> status transition error -> compensation
// =========================================================================

func TestErrorScenario_TerminalStatus_StatusTransitionError_CompensationRuns(t *testing.T) {
	const (
		orgID     = "org-err-005"
		docID     = "doc-err-005"
		versionID = "ver-err-005"
		jobID     = "job-err-005"
	)

	h := newTestHarness(t)
	h.seedDocument(defaultDocument(orgID, docID))

	// Seed version in FULLY_READY (terminal) status — no valid transitions.
	ver := defaultVersion(orgID, docID, versionID)
	ver.ArtifactStatus = model.ArtifactStatusFullyReady
	h.seedVersion(ver)

	event := defaultDPEvent(orgID, docID, versionID, jobID, "corr-err-005")

	err := h.ingestion.HandleDPArtifacts(context.Background(), event)
	if err == nil {
		t.Fatal("expected error for terminal status transition, got nil")
	}

	// Assert: error code is STATUS_TRANSITION.
	if code := port.ErrorCode(err); code != port.ErrCodeStatusTransition {
		t.Errorf("expected error code %s, got %s", port.ErrCodeStatusTransition, code)
	}

	// Note: the ingestion service sets Retryable=true for ALL status transition
	// errors. This is by design — the transition may become valid after a prior
	// stage completes (e.g., LIC arrives before DP commits). For a terminal
	// status like FULLY_READY, retrying will never succeed, but the error type
	// is the same. The dispatcher layer would need additional logic (e.g., a
	// max-retry counter) to route terminal failures to the DLQ.
	if !port.IsRetryable(err) {
		t.Error("expected retryable status transition error (by design)")
	}

	// Assert: blobs compensated — all uploaded blobs deleted.
	if got := h.objectStorage.blobCount(); got != 0 {
		t.Errorf("expected 0 blobs after compensation, got %d", got)
	}

	// Assert: no artifact descriptors saved.
	if got := len(h.artifactRepo.allArtifacts()); got != 0 {
		t.Errorf("expected 0 artifact descriptors, got %d", got)
	}

	// Assert: no outbox entries.
	if got := len(h.outboxRepo.allEntries()); got != 0 {
		t.Errorf("expected 0 outbox entries, got %d", got)
	}

	// Assert: version status unchanged (still FULLY_READY).
	verAfter, findErr := h.versionRepo.FindByID(context.Background(), orgID, docID, versionID)
	if findErr != nil {
		t.Fatalf("FindByID after error: %v", findErr)
	}
	assertEqual(t, "version.ArtifactStatus unchanged",
		string(model.ArtifactStatusFullyReady), string(verAfter.ArtifactStatus))
}

// =========================================================================
// Test-local fakes
// =========================================================================

// ---------------------------------------------------------------------------
// failingObjectStorage — wraps memoryObjectStorage and fails on the Nth
// PutObject call (1-indexed). After failure, all other methods succeed.
// Used by Scenario 1 to simulate partial upload failure + compensation.
// ---------------------------------------------------------------------------

type failingObjectStorage struct {
	inner        *memoryObjectStorage
	mu           sync.Mutex
	putCallCount int
	failOnPut    int // fail when putCallCount reaches this value (0 = never fail)
}

func newFailingObjectStorage(inner *memoryObjectStorage, failOnPut int) *failingObjectStorage {
	return &failingObjectStorage{inner: inner, failOnPut: failOnPut}
}

func (s *failingObjectStorage) PutObject(ctx context.Context, key string, data io.Reader, contentType string) error {
	s.mu.Lock()
	s.putCallCount++
	shouldFail := s.failOnPut > 0 && s.putCallCount == s.failOnPut
	s.mu.Unlock()

	if shouldFail {
		return port.NewStorageError("simulated storage failure on PutObject", fmt.Errorf("I/O error"))
	}
	return s.inner.PutObject(ctx, key, data, contentType)
}

func (s *failingObjectStorage) GetObject(ctx context.Context, key string) (io.ReadCloser, error) {
	return s.inner.GetObject(ctx, key)
}

func (s *failingObjectStorage) DeleteObject(ctx context.Context, key string) error {
	return s.inner.DeleteObject(ctx, key)
}

func (s *failingObjectStorage) HeadObject(ctx context.Context, key string) (int64, bool, error) {
	return s.inner.HeadObject(ctx, key)
}

func (s *failingObjectStorage) GeneratePresignedURL(ctx context.Context, key string, expiry time.Duration) (string, error) {
	return s.inner.GeneratePresignedURL(ctx, key, expiry)
}

func (s *failingObjectStorage) DeleteByPrefix(ctx context.Context, prefix string) error {
	return s.inner.DeleteByPrefix(ctx, prefix)
}

// disableFailure resets the failure counter so subsequent calls succeed.
func (s *failingObjectStorage) disableFailure() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failOnPut = 0
	s.putCallCount = 0
}

var _ port.ObjectStoragePort = (*failingObjectStorage)(nil)

// ---------------------------------------------------------------------------
// failingIdempotencyStore — all operations return an error, simulating
// Redis unavailability. Used by Scenario 4.
// ---------------------------------------------------------------------------

type failingIdempotencyStore struct{}

func (s *failingIdempotencyStore) Get(_ context.Context, _ string) (*model.IdempotencyRecord, error) {
	return nil, fmt.Errorf("redis connection refused")
}

func (s *failingIdempotencyStore) Set(_ context.Context, _ *model.IdempotencyRecord, _ time.Duration) error {
	return fmt.Errorf("redis connection refused")
}

func (s *failingIdempotencyStore) SetNX(_ context.Context, _ *model.IdempotencyRecord, _ time.Duration) (bool, error) {
	return false, fmt.Errorf("redis connection refused")
}

func (s *failingIdempotencyStore) Delete(_ context.Context, _ string) error {
	return fmt.Errorf("redis connection refused")
}

var _ port.IdempotencyStorePort = (*failingIdempotencyStore)(nil)

// ---------------------------------------------------------------------------
// conflictingVersionRepository — wraps memoryVersionRepository and returns
// a stale version_number on the first NextVersionNumber call, forcing
// a unique constraint violation on Insert. The retry loop in
// VersionManagementService.CreateVersion handles this by re-reading.
// Used by Scenario 2.
// ---------------------------------------------------------------------------

type conflictingVersionRepository struct {
	*memoryVersionRepository
	mu              sync.Mutex
	conflictOnFirst bool
	conflicted      bool
}

func newConflictingVersionRepository(inner *memoryVersionRepository) *conflictingVersionRepository {
	return &conflictingVersionRepository{
		memoryVersionRepository: inner,
		conflictOnFirst:         true,
	}
}

// NextVersionNumber returns a stale (conflicting) number on the first call,
// forcing a unique constraint violation when Insert is called. Subsequent
// calls delegate normally.
func (r *conflictingVersionRepository) NextVersionNumber(ctx context.Context, orgID, docID string) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.conflictOnFirst && !r.conflicted {
		r.conflicted = true
		// Return 1 even if a version with number 1 already exists,
		// causing the subsequent Insert to fail with ErrCodeVersionAlreadyExists.
		// The retry loop will call NextVersionNumber again and get the correct number.
		return 1, nil
	}
	return r.memoryVersionRepository.NextVersionNumber(ctx, orgID, docID)
}

var _ port.VersionRepository = (*conflictingVersionRepository)(nil)
