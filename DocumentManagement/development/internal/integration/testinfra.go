package integration

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"contractpro/document-management/internal/config"
	"contractpro/document-management/internal/domain/model"
	"contractpro/document-management/internal/domain/port"
	"contractpro/document-management/internal/egress/outbox"
	"contractpro/document-management/internal/ingress/idempotency"

	appIngestion "contractpro/document-management/internal/application/ingestion"
	appQuery "contractpro/document-management/internal/application/query"
	appDiff "contractpro/document-management/internal/application/diff"
)

// ---------------------------------------------------------------------------
// Test harness — wires real application services with in-memory fakes.
// ---------------------------------------------------------------------------

type testHarness struct {
	// In-memory stores.
	transactor    *memoryTransactor
	docRepo       *memoryDocumentRepository
	versionRepo   *memoryVersionRepository
	artifactRepo  *memoryArtifactRepository
	auditRepo     *memoryAuditRepository
	outboxRepo    *memoryOutboxRepository
	objectStorage *memoryObjectStorage
	idemStore     *memoryIdempotencyStore
	dlqPort       *recordingDLQPort
	diffRepo      *memoryDiffRepository
	fallback      *memoryFallbackResolver

	// Real application services.
	ingestion       *appIngestion.ArtifactIngestionService
	query           *appQuery.ArtifactQueryService
	diffService     *appDiff.DiffStorageService
	outboxWriter    *outbox.OutboxWriter
	idempotencyGuard *idempotency.IdempotencyGuard

	// Consumer-level helpers.
	broker *captureBroker
	logger *recordingLogger
}

func newTestHarness(t *testing.T) *testHarness {
	t.Helper()
	return newTestHarnessCore(t, &noopConfirmationPublisher{})
}

// newTestHarnessCore is the shared initialization for all harness variants.
func newTestHarnessCore(t *testing.T, confirmationPublisher port.ConfirmationPublisherPort) *testHarness {
	t.Helper()

	h := &testHarness{
		transactor:    newMemoryTransactor(),
		docRepo:       newMemoryDocumentRepository(),
		versionRepo:   newMemoryVersionRepository(),
		artifactRepo:  newMemoryArtifactRepository(),
		auditRepo:     newMemoryAuditRepository(),
		outboxRepo:    newMemoryOutboxRepository(),
		objectStorage: newMemoryObjectStorage(),
		idemStore:     newMemoryIdempotencyStore(),
		dlqPort:       newRecordingDLQPort(),
		diffRepo:      newMemoryDiffRepository(),
		fallback:      newMemoryFallbackResolver(),
		broker:        newCaptureBroker(),
		logger:        newRecordingLogger(),
	}

	h.outboxWriter = outbox.NewOutboxWriter(h.outboxRepo)

	// Wire ingestion service.
	h.ingestion = appIngestion.NewArtifactIngestionService(
		h.transactor,
		h.versionRepo,
		h.artifactRepo,
		h.auditRepo,
		h.objectStorage,
		h.outboxWriter,
		h.fallback,
		&noopFallbackMetrics{},
		h.docRepo,
		&noopTenantMetrics{},
		h.logger,
	)

	// Wire query service.
	h.query = appQuery.NewArtifactQueryService(
		h.artifactRepo,
		h.objectStorage,
		confirmationPublisher,
		h.auditRepo,
		h.fallback,
		h.docRepo,
		&noopTenantMetrics{},
		h.logger,
	)

	// Wire diff service.
	h.diffService = appDiff.NewDiffStorageService(
		h.transactor,
		h.versionRepo,
		h.diffRepo,
		h.auditRepo,
		h.objectStorage,
		h.outboxWriter,
		h.fallback,
		h.docRepo,
		&noopTenantMetrics{},
		h.logger,
	)

	// Wire idempotency guard.
	h.idempotencyGuard = idempotency.NewIdempotencyGuard(
		h.idemStore,
		config.IdempotencyConfig{
			TTL:            24 * time.Hour,
			ProcessingTTL:  120 * time.Second,
			StuckThreshold: 240 * time.Second,
		},
		&noopIdempotencyMetrics{},
		h.logger,
	)

	return h
}

// seedDocument creates a pre-existing document in the in-memory store.
func (h *testHarness) seedDocument(doc *model.Document) {
	h.docRepo.store(doc)
}

// seedVersion creates a pre-existing document version in the in-memory store.
func (h *testHarness) seedVersion(ver *model.DocumentVersion) {
	h.versionRepo.store(ver)
}

// ---------------------------------------------------------------------------
// In-memory Transactor
// ---------------------------------------------------------------------------

type memoryTransactor struct {
	mu        sync.Mutex
	callCount int
	// txMu serializes transaction execution, simulating database-level
	// row locking (FOR UPDATE). Without this, concurrent transactions
	// would operate on shared state without isolation.
	txMu sync.Mutex
}

func newMemoryTransactor() *memoryTransactor { return &memoryTransactor{} }

func (t *memoryTransactor) WithTransaction(ctx context.Context, fn func(ctx context.Context) error) error {
	t.mu.Lock()
	t.callCount++
	t.mu.Unlock()

	// Serialize transaction execution to simulate DB-level isolation.
	t.txMu.Lock()
	defer t.txMu.Unlock()
	return fn(ctx)
}

// txCallCount returns the number of WithTransaction calls (thread-safe).
func (t *memoryTransactor) txCallCount() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.callCount
}

var _ port.Transactor = (*memoryTransactor)(nil)

// ---------------------------------------------------------------------------
// In-memory Document Repository
// ---------------------------------------------------------------------------

type memoryDocumentRepository struct {
	mu   sync.RWMutex
	docs map[string]*model.Document // key: orgID/docID
}

func newMemoryDocumentRepository() *memoryDocumentRepository {
	return &memoryDocumentRepository{docs: make(map[string]*model.Document)}
}

func (r *memoryDocumentRepository) key(orgID, docID string) string {
	return orgID + "/" + docID
}

func (r *memoryDocumentRepository) store(doc *model.Document) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.docs[r.key(doc.OrganizationID, doc.DocumentID)] = doc
}

func (r *memoryDocumentRepository) Insert(ctx context.Context, doc *model.Document) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	k := r.key(doc.OrganizationID, doc.DocumentID)
	if _, ok := r.docs[k]; ok {
		return port.NewDocumentAlreadyExistsError(doc.DocumentID)
	}
	r.docs[k] = doc
	return nil
}

func (r *memoryDocumentRepository) FindByID(ctx context.Context, orgID, docID string) (*model.Document, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	doc, ok := r.docs[r.key(orgID, docID)]
	if !ok {
		return nil, port.NewDocumentNotFoundError(orgID, docID)
	}
	return doc, nil
}

func (r *memoryDocumentRepository) FindByIDForUpdate(ctx context.Context, orgID, docID string) (*model.Document, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	doc, ok := r.docs[r.key(orgID, docID)]
	if !ok {
		return nil, port.NewDocumentNotFoundError(orgID, docID)
	}
	// Return a copy to prevent concurrent mutation of the shared object,
	// simulating the isolation provided by SELECT ... FOR UPDATE.
	cp := *doc
	return &cp, nil
}

func (r *memoryDocumentRepository) List(ctx context.Context, orgID string, statusFilter *model.DocumentStatus, page, pageSize int) ([]*model.Document, int, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var result []*model.Document
	for _, doc := range r.docs {
		if doc.OrganizationID != orgID {
			continue
		}
		if statusFilter != nil && doc.Status != *statusFilter {
			continue
		}
		result = append(result, doc)
	}
	total := len(result)
	start := (page - 1) * pageSize
	if start >= total {
		return nil, total, nil
	}
	end := start + pageSize
	if end > total {
		end = total
	}
	return result[start:end], total, nil
}

func (r *memoryDocumentRepository) Update(ctx context.Context, doc *model.Document) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	k := r.key(doc.OrganizationID, doc.DocumentID)
	if _, ok := r.docs[k]; !ok {
		return port.NewDocumentNotFoundError(doc.OrganizationID, doc.DocumentID)
	}
	r.docs[k] = doc
	return nil
}

func (r *memoryDocumentRepository) ExistsByID(ctx context.Context, orgID, docID string) (bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.docs[r.key(orgID, docID)]
	return ok, nil
}

var _ port.DocumentRepository = (*memoryDocumentRepository)(nil)

// ---------------------------------------------------------------------------
// In-memory Version Repository
// ---------------------------------------------------------------------------

type memoryVersionRepository struct {
	mu       sync.RWMutex
	versions map[string]*model.DocumentVersion // key: orgID/docID/versionID
}

func newMemoryVersionRepository() *memoryVersionRepository {
	return &memoryVersionRepository{versions: make(map[string]*model.DocumentVersion)}
}

func (r *memoryVersionRepository) vkey(orgID, docID, versionID string) string {
	return orgID + "/" + docID + "/" + versionID
}

func (r *memoryVersionRepository) store(ver *model.DocumentVersion) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.versions[r.vkey(ver.OrganizationID, ver.DocumentID, ver.VersionID)] = ver
}

func (r *memoryVersionRepository) Insert(ctx context.Context, version *model.DocumentVersion) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	k := r.vkey(version.OrganizationID, version.DocumentID, version.VersionID)
	if _, ok := r.versions[k]; ok {
		return port.NewVersionAlreadyExistsError(version.VersionID)
	}
	// Simulate DB unique constraint on (org_id, doc_id, version_number).
	for _, v := range r.versions {
		if v.OrganizationID == version.OrganizationID &&
			v.DocumentID == version.DocumentID &&
			v.VersionNumber == version.VersionNumber {
			return port.NewVersionAlreadyExistsError(version.VersionID)
		}
	}
	r.versions[k] = version
	return nil
}

func (r *memoryVersionRepository) FindByID(ctx context.Context, orgID, docID, versionID string) (*model.DocumentVersion, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	ver, ok := r.versions[r.vkey(orgID, docID, versionID)]
	if !ok {
		return nil, port.NewVersionNotFoundError(versionID)
	}
	return ver, nil
}

func (r *memoryVersionRepository) FindByIDForUpdate(ctx context.Context, orgID, docID, versionID string) (*model.DocumentVersion, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	ver, ok := r.versions[r.vkey(orgID, docID, versionID)]
	if !ok {
		return nil, port.NewVersionNotFoundError(versionID)
	}
	// Return a copy to prevent concurrent mutation of the shared object,
	// simulating the isolation provided by SELECT ... FOR UPDATE.
	cp := *ver
	return &cp, nil
}

func (r *memoryVersionRepository) List(ctx context.Context, orgID, docID string, page, pageSize int) ([]*model.DocumentVersion, int, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var result []*model.DocumentVersion
	for _, ver := range r.versions {
		if ver.OrganizationID == orgID && ver.DocumentID == docID {
			result = append(result, ver)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].VersionNumber > result[j].VersionNumber
	})
	total := len(result)
	start := (page - 1) * pageSize
	if start >= total {
		return nil, total, nil
	}
	end := start + pageSize
	if end > total {
		end = total
	}
	return result[start:end], total, nil
}

func (r *memoryVersionRepository) Update(ctx context.Context, version *model.DocumentVersion) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	k := r.vkey(version.OrganizationID, version.DocumentID, version.VersionID)
	if _, ok := r.versions[k]; !ok {
		return port.NewVersionNotFoundError(version.VersionID)
	}
	r.versions[k] = version
	return nil
}

func (r *memoryVersionRepository) NextVersionNumber(ctx context.Context, orgID, docID string) (int, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	maxNum := 0
	for _, ver := range r.versions {
		if ver.OrganizationID == orgID && ver.DocumentID == docID && ver.VersionNumber > maxNum {
			maxNum = ver.VersionNumber
		}
	}
	return maxNum + 1, nil
}

var _ port.VersionRepository = (*memoryVersionRepository)(nil)

// ---------------------------------------------------------------------------
// In-memory Artifact Repository
// ---------------------------------------------------------------------------

type memoryArtifactRepository struct {
	mu        sync.RWMutex
	artifacts []*model.ArtifactDescriptor
}

func newMemoryArtifactRepository() *memoryArtifactRepository {
	return &memoryArtifactRepository{}
}

func (r *memoryArtifactRepository) Insert(ctx context.Context, descriptor *model.ArtifactDescriptor) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, a := range r.artifacts {
		if a.VersionID == descriptor.VersionID && a.ArtifactType == descriptor.ArtifactType {
			return port.NewArtifactAlreadyExistsError(descriptor.VersionID, string(descriptor.ArtifactType))
		}
	}
	r.artifacts = append(r.artifacts, descriptor)
	return nil
}

func (r *memoryArtifactRepository) FindByVersionAndType(ctx context.Context, orgID, docID, versionID string, artifactType model.ArtifactType) (*model.ArtifactDescriptor, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, a := range r.artifacts {
		if a.OrganizationID == orgID && a.DocumentID == docID && a.VersionID == versionID && a.ArtifactType == artifactType {
			return a, nil
		}
	}
	return nil, port.NewArtifactNotFoundError(versionID, string(artifactType))
}

func (r *memoryArtifactRepository) ListByVersion(ctx context.Context, orgID, docID, versionID string) ([]*model.ArtifactDescriptor, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var result []*model.ArtifactDescriptor
	for _, a := range r.artifacts {
		if a.OrganizationID == orgID && a.DocumentID == docID && a.VersionID == versionID {
			result = append(result, a)
		}
	}
	if result == nil {
		result = []*model.ArtifactDescriptor{}
	}
	return result, nil
}

func (r *memoryArtifactRepository) ListByVersionAndTypes(ctx context.Context, orgID, docID, versionID string, types []model.ArtifactType) ([]*model.ArtifactDescriptor, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	typeSet := make(map[model.ArtifactType]bool, len(types))
	for _, t := range types {
		typeSet[t] = true
	}
	var result []*model.ArtifactDescriptor
	for _, a := range r.artifacts {
		if a.OrganizationID == orgID && a.DocumentID == docID && a.VersionID == versionID && typeSet[a.ArtifactType] {
			result = append(result, a)
		}
	}
	if result == nil {
		result = []*model.ArtifactDescriptor{}
	}
	return result, nil
}

func (r *memoryArtifactRepository) DeleteByVersion(ctx context.Context, orgID, docID, versionID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	filtered := make([]*model.ArtifactDescriptor, 0, len(r.artifacts))
	for _, a := range r.artifacts {
		if !(a.OrganizationID == orgID && a.DocumentID == docID && a.VersionID == versionID) {
			filtered = append(filtered, a)
		}
	}
	r.artifacts = filtered
	return nil
}

// allArtifacts returns a snapshot of all stored artifact descriptors.
func (r *memoryArtifactRepository) allArtifacts() []*model.ArtifactDescriptor {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]*model.ArtifactDescriptor, len(r.artifacts))
	copy(result, r.artifacts)
	return result
}

var _ port.ArtifactRepository = (*memoryArtifactRepository)(nil)

// ---------------------------------------------------------------------------
// In-memory Diff Repository
// ---------------------------------------------------------------------------

type memoryDiffRepository struct {
	mu    sync.RWMutex
	diffs []*model.VersionDiffReference
}

func newMemoryDiffRepository() *memoryDiffRepository {
	return &memoryDiffRepository{}
}

func (r *memoryDiffRepository) Insert(ctx context.Context, ref *model.VersionDiffReference) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, d := range r.diffs {
		if d.BaseVersionID == ref.BaseVersionID && d.TargetVersionID == ref.TargetVersionID {
			return port.NewDiffAlreadyExistsError(ref.BaseVersionID, ref.TargetVersionID)
		}
	}
	r.diffs = append(r.diffs, ref)
	return nil
}

func (r *memoryDiffRepository) FindByVersionPair(ctx context.Context, orgID, docID, baseVersionID, targetVersionID string) (*model.VersionDiffReference, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, d := range r.diffs {
		if d.OrganizationID == orgID && d.DocumentID == docID &&
			d.BaseVersionID == baseVersionID && d.TargetVersionID == targetVersionID {
			return d, nil
		}
	}
	return nil, port.NewDiffNotFoundError(baseVersionID, targetVersionID)
}

func (r *memoryDiffRepository) ListByDocument(ctx context.Context, orgID, docID string) ([]*model.VersionDiffReference, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var result []*model.VersionDiffReference
	for _, d := range r.diffs {
		if d.OrganizationID == orgID && d.DocumentID == docID {
			result = append(result, d)
		}
	}
	return result, nil
}

func (r *memoryDiffRepository) DeleteByDocument(ctx context.Context, orgID, docID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	filtered := make([]*model.VersionDiffReference, 0, len(r.diffs))
	for _, d := range r.diffs {
		if !(d.OrganizationID == orgID && d.DocumentID == docID) {
			filtered = append(filtered, d)
		}
	}
	r.diffs = filtered
	return nil
}

var _ port.DiffRepository = (*memoryDiffRepository)(nil)

// ---------------------------------------------------------------------------
// In-memory Audit Repository
// ---------------------------------------------------------------------------

type memoryAuditRepository struct {
	mu      sync.RWMutex
	records []*model.AuditRecord
}

func newMemoryAuditRepository() *memoryAuditRepository {
	return &memoryAuditRepository{}
}

func (r *memoryAuditRepository) Insert(ctx context.Context, record *model.AuditRecord) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.records = append(r.records, record)
	return nil
}

func (r *memoryAuditRepository) List(ctx context.Context, params port.AuditListParams) ([]*model.AuditRecord, int, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var result []*model.AuditRecord
	for _, rec := range r.records {
		if rec.OrganizationID != params.OrganizationID {
			continue
		}
		if params.DocumentID != "" && rec.DocumentID != params.DocumentID {
			continue
		}
		if params.VersionID != "" && rec.VersionID != params.VersionID {
			continue
		}
		if params.Action != nil && rec.Action != *params.Action {
			continue
		}
		result = append(result, rec)
	}
	total := len(result)
	return result, total, nil
}

// allRecords returns a snapshot of all audit records.
func (r *memoryAuditRepository) allRecords() []*model.AuditRecord {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]*model.AuditRecord, len(r.records))
	copy(result, r.records)
	return result
}

var _ port.AuditRepository = (*memoryAuditRepository)(nil)

// ---------------------------------------------------------------------------
// In-memory Outbox Repository
// ---------------------------------------------------------------------------

type memoryOutboxRepository struct {
	mu      sync.RWMutex
	entries []port.OutboxEntry
}

func newMemoryOutboxRepository() *memoryOutboxRepository {
	return &memoryOutboxRepository{}
}

func (r *memoryOutboxRepository) Insert(ctx context.Context, entries ...port.OutboxEntry) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.entries = append(r.entries, entries...)
	return nil
}

func (r *memoryOutboxRepository) FetchUnpublished(ctx context.Context, limit int) ([]port.OutboxEntry, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var result []port.OutboxEntry
	for _, e := range r.entries {
		if e.Status == "PENDING" {
			result = append(result, e)
			if len(result) >= limit {
				break
			}
		}
	}
	return result, nil
}

func (r *memoryOutboxRepository) MarkPublished(ctx context.Context, ids []string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	idSet := make(map[string]bool, len(ids))
	for _, id := range ids {
		idSet[id] = true
	}
	now := time.Now().UTC()
	for i := range r.entries {
		if idSet[r.entries[i].ID] {
			r.entries[i].Status = "CONFIRMED"
			r.entries[i].PublishedAt = now
		}
	}
	return nil
}

func (r *memoryOutboxRepository) DeletePublished(ctx context.Context, olderThan time.Time, limit int) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var count int64
	filtered := r.entries[:0]
	for _, e := range r.entries {
		if e.Status == "CONFIRMED" && e.PublishedAt.Before(olderThan) && (limit == 0 || count < int64(limit)) {
			count++
			continue
		}
		filtered = append(filtered, e)
	}
	r.entries = filtered
	return count, nil
}

func (r *memoryOutboxRepository) PendingStats(ctx context.Context) (int64, float64, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var count int64
	var oldest time.Time
	for _, e := range r.entries {
		if e.Status == "PENDING" {
			count++
			if oldest.IsZero() || e.CreatedAt.Before(oldest) {
				oldest = e.CreatedAt
			}
		}
	}
	if count == 0 {
		return 0, 0, nil
	}
	return count, time.Since(oldest).Seconds(), nil
}

// allEntries returns a snapshot of all outbox entries.
func (r *memoryOutboxRepository) allEntries() []port.OutboxEntry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]port.OutboxEntry, len(r.entries))
	copy(result, r.entries)
	return result
}

// pendingEntries returns entries with status PENDING.
func (r *memoryOutboxRepository) pendingEntries() []port.OutboxEntry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var result []port.OutboxEntry
	for _, e := range r.entries {
		if e.Status == "PENDING" {
			result = append(result, e)
		}
	}
	return result
}

var _ port.OutboxRepository = (*memoryOutboxRepository)(nil)

// ---------------------------------------------------------------------------
// In-memory Object Storage
// ---------------------------------------------------------------------------

type memoryObjectStorage struct {
	mu    sync.RWMutex
	blobs map[string][]byte
	types map[string]string // key → content type
}

func newMemoryObjectStorage() *memoryObjectStorage {
	return &memoryObjectStorage{
		blobs: make(map[string][]byte),
		types: make(map[string]string),
	}
}

func (s *memoryObjectStorage) PutObject(ctx context.Context, key string, data io.Reader, contentType string) error {
	b, err := io.ReadAll(data)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.blobs[key] = b
	s.types[key] = contentType
	return nil
}

func (s *memoryObjectStorage) GetObject(ctx context.Context, key string) (io.ReadCloser, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	data, ok := s.blobs[key]
	if !ok {
		return nil, port.NewStorageError("object not found: "+key, nil)
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func (s *memoryObjectStorage) DeleteObject(ctx context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.blobs, key)
	delete(s.types, key)
	return nil
}

func (s *memoryObjectStorage) HeadObject(ctx context.Context, key string) (int64, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	data, ok := s.blobs[key]
	if !ok {
		return 0, false, nil
	}
	return int64(len(data)), true, nil
}

func (s *memoryObjectStorage) GeneratePresignedURL(ctx context.Context, key string, expiry time.Duration) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, ok := s.blobs[key]; !ok {
		return "", port.NewStorageError("object not found: "+key, nil)
	}
	return "https://presigned.example.com/" + key, nil
}

func (s *memoryObjectStorage) DeleteByPrefix(ctx context.Context, prefix string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for key := range s.blobs {
		if len(key) >= len(prefix) && key[:len(prefix)] == prefix {
			delete(s.blobs, key)
			delete(s.types, key)
		}
	}
	return nil
}

// hasKey returns true if the object storage contains the given key.
func (s *memoryObjectStorage) hasKey(key string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.blobs[key]
	return ok
}

// getBlob returns a copy of the stored data for a key, or nil if not found.
func (s *memoryObjectStorage) getBlob(key string) []byte {
	s.mu.RLock()
	defer s.mu.RUnlock()
	data, ok := s.blobs[key]
	if !ok {
		return nil
	}
	cp := make([]byte, len(data))
	copy(cp, data)
	return cp
}

// blobCount returns the number of stored blobs.
func (s *memoryObjectStorage) blobCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.blobs)
}

var _ port.ObjectStoragePort = (*memoryObjectStorage)(nil)

// ---------------------------------------------------------------------------
// In-memory Idempotency Store
// ---------------------------------------------------------------------------

type memoryIdempotencyStore struct {
	mu      sync.RWMutex
	records map[string]*model.IdempotencyRecord
}

func newMemoryIdempotencyStore() *memoryIdempotencyStore {
	return &memoryIdempotencyStore{records: make(map[string]*model.IdempotencyRecord)}
}

func (s *memoryIdempotencyStore) Get(ctx context.Context, key string) (*model.IdempotencyRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rec, ok := s.records[key]
	if !ok {
		return nil, nil
	}
	return rec, nil
}

func (s *memoryIdempotencyStore) Set(ctx context.Context, record *model.IdempotencyRecord, ttl time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records[record.Key] = record
	return nil
}

func (s *memoryIdempotencyStore) SetNX(ctx context.Context, record *model.IdempotencyRecord, ttl time.Duration) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.records[record.Key]; ok {
		return false, nil
	}
	s.records[record.Key] = record
	return true, nil
}

func (s *memoryIdempotencyStore) Delete(ctx context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.records, key)
	return nil
}

// getRecord returns the idempotency record for the given key (thread-safe).
func (s *memoryIdempotencyStore) getRecord(key string) *model.IdempotencyRecord {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.records[key]
}

var _ port.IdempotencyStorePort = (*memoryIdempotencyStore)(nil)

// ---------------------------------------------------------------------------
// Recording DLQ Port
// ---------------------------------------------------------------------------

type recordingDLQPort struct {
	mu      sync.Mutex
	records []model.DLQRecord
}

func newRecordingDLQPort() *recordingDLQPort {
	return &recordingDLQPort{}
}

func (p *recordingDLQPort) SendToDLQ(ctx context.Context, record model.DLQRecord) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.records = append(p.records, record)
	return nil
}

// allRecords returns a snapshot of all DLQ records (thread-safe).
func (p *recordingDLQPort) allRecords() []model.DLQRecord {
	p.mu.Lock()
	defer p.mu.Unlock()
	result := make([]model.DLQRecord, len(p.records))
	copy(result, p.records)
	return result
}

var _ port.DLQPort = (*recordingDLQPort)(nil)

// ---------------------------------------------------------------------------
// In-memory Fallback Resolver
// ---------------------------------------------------------------------------

type memoryFallbackResolver struct {
	mu   sync.RWMutex
	data map[string]fallbackData // key: documentID
}

type fallbackData struct {
	orgID            string
	currentVersionID string
}

func newMemoryFallbackResolver() *memoryFallbackResolver {
	return &memoryFallbackResolver{data: make(map[string]fallbackData)}
}

func (r *memoryFallbackResolver) RegisterDocument(docID, orgID, currentVersionID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.data[docID] = fallbackData{orgID: orgID, currentVersionID: currentVersionID}
}

func (r *memoryFallbackResolver) ResolveByDocumentID(ctx context.Context, documentID string) (string, string, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	d, ok := r.data[documentID]
	if !ok {
		return "", "", port.NewDocumentNotFoundError("", documentID)
	}
	return d.orgID, d.currentVersionID, nil
}

var _ port.DocumentFallbackResolver = (*memoryFallbackResolver)(nil)

// ---------------------------------------------------------------------------
// Capture Broker — captures subscribe handlers for test delivery.
// ---------------------------------------------------------------------------

type captureBroker struct {
	mu       sync.RWMutex
	handlers map[string]func(ctx context.Context, body []byte) error
}

func newCaptureBroker() *captureBroker {
	return &captureBroker{handlers: make(map[string]func(ctx context.Context, body []byte) error)}
}

func (b *captureBroker) Subscribe(topic string, handler func(ctx context.Context, body []byte) error) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.handlers[topic] = handler
	return nil
}

func (b *captureBroker) Publish(ctx context.Context, topic string, payload []byte) error {
	return nil
}

// deliverToTopic invokes the registered handler for the given topic synchronously.
func (b *captureBroker) deliverToTopic(ctx context.Context, topic string, body []byte) error {
	b.mu.RLock()
	handler, ok := b.handlers[topic]
	b.mu.RUnlock()
	if !ok {
		return nil
	}
	return handler(ctx, body)
}

// ---------------------------------------------------------------------------
// Recording Confirmation Publisher (for query service integration tests)
// ---------------------------------------------------------------------------

type recordingConfirmationPublisher struct {
	mu                   sync.Mutex
	semanticTreeProvided []model.SemanticTreeProvided
	artifactsProvided    []model.ArtifactsProvided
}

func newRecordingConfirmationPublisher() *recordingConfirmationPublisher {
	return &recordingConfirmationPublisher{}
}

func (p *recordingConfirmationPublisher) PublishDPArtifactsPersisted(_ context.Context, _ model.DocumentProcessingArtifactsPersisted) error {
	return nil
}
func (p *recordingConfirmationPublisher) PublishDPArtifactsPersistFailed(_ context.Context, _ model.DocumentProcessingArtifactsPersistFailed) error {
	return nil
}
func (p *recordingConfirmationPublisher) PublishSemanticTreeProvided(_ context.Context, event model.SemanticTreeProvided) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.semanticTreeProvided = append(p.semanticTreeProvided, event)
	return nil
}
func (p *recordingConfirmationPublisher) PublishArtifactsProvided(_ context.Context, event model.ArtifactsProvided) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.artifactsProvided = append(p.artifactsProvided, event)
	return nil
}
func (p *recordingConfirmationPublisher) PublishDiffPersisted(_ context.Context, _ model.DocumentVersionDiffPersisted) error {
	return nil
}
func (p *recordingConfirmationPublisher) PublishDiffPersistFailed(_ context.Context, _ model.DocumentVersionDiffPersistFailed) error {
	return nil
}
func (p *recordingConfirmationPublisher) PublishLICArtifactsPersisted(_ context.Context, _ model.LegalAnalysisArtifactsPersisted) error {
	return nil
}
func (p *recordingConfirmationPublisher) PublishLICArtifactsPersistFailed(_ context.Context, _ model.LegalAnalysisArtifactsPersistFailed) error {
	return nil
}
func (p *recordingConfirmationPublisher) PublishREReportsPersisted(_ context.Context, _ model.ReportsArtifactsPersisted) error {
	return nil
}
func (p *recordingConfirmationPublisher) PublishREReportsPersistFailed(_ context.Context, _ model.ReportsArtifactsPersistFailed) error {
	return nil
}

func (p *recordingConfirmationPublisher) getSemanticTreeProvided() []model.SemanticTreeProvided {
	p.mu.Lock()
	defer p.mu.Unlock()
	result := make([]model.SemanticTreeProvided, len(p.semanticTreeProvided))
	copy(result, p.semanticTreeProvided)
	return result
}

func (p *recordingConfirmationPublisher) getArtifactsProvided() []model.ArtifactsProvided {
	p.mu.Lock()
	defer p.mu.Unlock()
	result := make([]model.ArtifactsProvided, len(p.artifactsProvided))
	copy(result, p.artifactsProvided)
	return result
}

var _ port.ConfirmationPublisherPort = (*recordingConfirmationPublisher)(nil)

// ---------------------------------------------------------------------------
// Noop Confirmation Publisher (for query service)
// ---------------------------------------------------------------------------

type noopConfirmationPublisher struct{}

func (p *noopConfirmationPublisher) PublishDPArtifactsPersisted(ctx context.Context, event model.DocumentProcessingArtifactsPersisted) error {
	return nil
}
func (p *noopConfirmationPublisher) PublishDPArtifactsPersistFailed(ctx context.Context, event model.DocumentProcessingArtifactsPersistFailed) error {
	return nil
}
func (p *noopConfirmationPublisher) PublishSemanticTreeProvided(ctx context.Context, event model.SemanticTreeProvided) error {
	return nil
}
func (p *noopConfirmationPublisher) PublishArtifactsProvided(ctx context.Context, event model.ArtifactsProvided) error {
	return nil
}
func (p *noopConfirmationPublisher) PublishDiffPersisted(ctx context.Context, event model.DocumentVersionDiffPersisted) error {
	return nil
}
func (p *noopConfirmationPublisher) PublishDiffPersistFailed(ctx context.Context, event model.DocumentVersionDiffPersistFailed) error {
	return nil
}
func (p *noopConfirmationPublisher) PublishLICArtifactsPersisted(ctx context.Context, event model.LegalAnalysisArtifactsPersisted) error {
	return nil
}
func (p *noopConfirmationPublisher) PublishLICArtifactsPersistFailed(ctx context.Context, event model.LegalAnalysisArtifactsPersistFailed) error {
	return nil
}
func (p *noopConfirmationPublisher) PublishREReportsPersisted(ctx context.Context, event model.ReportsArtifactsPersisted) error {
	return nil
}
func (p *noopConfirmationPublisher) PublishREReportsPersistFailed(ctx context.Context, event model.ReportsArtifactsPersistFailed) error {
	return nil
}

var _ port.ConfirmationPublisherPort = (*noopConfirmationPublisher)(nil)

// ---------------------------------------------------------------------------
// Recording Logger
// ---------------------------------------------------------------------------

type logEntry struct {
	level string
	msg   string
	args  []any
}

type recordingLogger struct {
	mu      sync.Mutex
	entries []logEntry
}

func newRecordingLogger() *recordingLogger {
	return &recordingLogger{}
}

func (l *recordingLogger) Info(msg string, keysAndValues ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.entries = append(l.entries, logEntry{level: "INFO", msg: msg, args: keysAndValues})
}

func (l *recordingLogger) Warn(msg string, keysAndValues ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.entries = append(l.entries, logEntry{level: "WARN", msg: msg, args: keysAndValues})
}

func (l *recordingLogger) Error(msg string, keysAndValues ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.entries = append(l.entries, logEntry{level: "ERROR", msg: msg, args: keysAndValues})
}

func (l *recordingLogger) Debug(msg string, keysAndValues ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.entries = append(l.entries, logEntry{level: "DEBUG", msg: msg, args: keysAndValues})
}

// hasMessage returns true if a log entry with the given message substring exists.
func (l *recordingLogger) hasMessage(substr string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, e := range l.entries {
		if strings.Contains(e.msg, substr) {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Noop metrics stubs
// ---------------------------------------------------------------------------

type noopFallbackMetrics struct{}

func (m *noopFallbackMetrics) IncMissingVersionID() {}

type noopTenantMetrics struct{}

func (m *noopTenantMetrics) IncTenantMismatch() {}

type noopIdempotencyMetrics struct{}

func (m *noopIdempotencyMetrics) IncFallbackTotal(topic string) {}
func (m *noopIdempotencyMetrics) IncCheckTotal(result string)   {}

// ---------------------------------------------------------------------------
// Shared test helpers
// ---------------------------------------------------------------------------

// sha256HexHelper computes SHA-256 of data and returns it as a hex string.
func sha256HexHelper(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

// ---------------------------------------------------------------------------
// Test data helpers
// ---------------------------------------------------------------------------

func defaultDocument(orgID, docID string) *model.Document {
	return model.NewDocument(docID, orgID, "Test Contract", "user-001")
}

func defaultVersion(orgID, docID, versionID string) *model.DocumentVersion {
	return model.NewDocumentVersion(
		versionID, docID, orgID,
		1,
		model.OriginTypeUpload,
		"source/test.pdf", "test.pdf",
		12345, "abc123sha", "user-001",
	)
}

func defaultDPEvent(orgID, docID, versionID, jobID, correlationID string) model.DocumentProcessingArtifactsReady {
	return model.DocumentProcessingArtifactsReady{
		EventMeta: model.EventMeta{
			CorrelationID: correlationID,
			Timestamp:     time.Now().UTC(),
		},
		JobID:        jobID,
		DocumentID:   docID,
		VersionID:    versionID,
		OrgID:        orgID,
		OCRRaw:       json.RawMessage(`{"pages": [{"text": "sample OCR"}]}`),
		Text:         json.RawMessage(`{"content": "extracted text"}`),
		Structure:    json.RawMessage(`{"sections": [{"title": "Section 1"}]}`),
		SemanticTree: json.RawMessage(`{"nodes": [{"id": "root"}]}`),
	}
}

func defaultLICEvent(orgID, docID, versionID, jobID, correlationID string) model.LegalAnalysisArtifactsReady {
	return model.LegalAnalysisArtifactsReady{
		EventMeta: model.EventMeta{
			CorrelationID: correlationID,
			Timestamp:     time.Now().UTC(),
		},
		JobID:                jobID,
		DocumentID:           docID,
		VersionID:            versionID,
		OrgID:                orgID,
		ClassificationResult: json.RawMessage(`{"type": "supply_agreement"}`),
		KeyParameters:        json.RawMessage(`{"parties": ["Company A", "Company B"]}`),
		RiskAnalysis:         json.RawMessage(`{"risks": [{"id": "R1", "severity": "HIGH"}]}`),
		RiskProfile:          json.RawMessage(`{"overall_risk": "MEDIUM"}`),
		Recommendations:      json.RawMessage(`{"items": [{"text": "Add force majeure clause"}]}`),
		Summary:              json.RawMessage(`{"text": "Supply agreement between A and B"}`),
		DetailedReport:       json.RawMessage(`{"sections": [{"title": "Risk Overview"}]}`),
		AggregateScore:       json.RawMessage(`{"score": 72}`),
	}
}

// defaultREEvent creates a RE event with pre-seeded blobs in object storage.
// The blobs must exist before calling HandleREArtifacts (claim-check pattern).
func defaultREEvent(h *testHarness, orgID, docID, versionID, jobID, correlationID string) model.ReportsArtifactsReady {
	pdfContent := []byte("%PDF-1.4 fake PDF content for testing")
	docxContent := []byte("PK\x03\x04 fake DOCX content for testing")

	pdfKey := orgID + "/" + docID + "/" + versionID + "/" + "EXPORT_PDF"
	docxKey := orgID + "/" + docID + "/" + versionID + "/" + "EXPORT_DOCX"

	// Pre-seed blobs in object storage (RE uploads before sending event).
	_ = h.objectStorage.PutObject(context.Background(), pdfKey, bytes.NewReader(pdfContent), "application/pdf")
	_ = h.objectStorage.PutObject(context.Background(), docxKey, bytes.NewReader(docxContent), "application/vnd.openxmlformats-officedocument.wordprocessingml.document")

	pdfHash := sha256HexHelper(pdfContent)
	docxHash := sha256HexHelper(docxContent)

	return model.ReportsArtifactsReady{
		EventMeta: model.EventMeta{
			CorrelationID: correlationID,
			Timestamp:     time.Now().UTC(),
		},
		JobID:      jobID,
		DocumentID: docID,
		VersionID:  versionID,
		OrgID:      orgID,
		ExportPDF: &model.BlobReference{
			StorageKey:  pdfKey,
			FileName:    "contract_report.pdf",
			SizeBytes:   int64(len(pdfContent)),
			ContentHash: pdfHash,
		},
		ExportDOCX: &model.BlobReference{
			StorageKey:  docxKey,
			FileName:    "contract_report.docx",
			SizeBytes:   int64(len(docxContent)),
			ContentHash: docxHash,
		},
	}
}

// newTestHarnessWithRecordingPublisher creates a harness with a recording
// confirmation publisher (for query service integration tests that verify
// published SemanticTreeProvided / ArtifactsProvided events).
func newTestHarnessWithRecordingPublisher(t *testing.T) (*testHarness, *recordingConfirmationPublisher) {
	t.Helper()
	recPublisher := newRecordingConfirmationPublisher()
	return newTestHarnessCore(t, recPublisher), recPublisher
}
