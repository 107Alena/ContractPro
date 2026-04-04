package retention

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"contractpro/document-management/internal/domain/model"
)

// --- mocks ---

type mockMetaDocRepo struct {
	docs             []*model.Document
	findErr          error
	updateCalls      int
	deleteByIDCalls  []string
	deleteByIDErr    error
	mu               sync.Mutex
}

func (m *mockMetaDocRepo) FindDeletedOlderThan(_ context.Context, _ time.Time, _ int) ([]*model.Document, error) {
	return m.docs, m.findErr
}

func (m *mockMetaDocRepo) DeleteByID(_ context.Context, documentID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.deleteByIDCalls = append(m.deleteByIDCalls, documentID)
	return m.deleteByIDErr
}

func (m *mockMetaDocRepo) Update(_ context.Context, _ *model.Document) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.updateCalls++
	return nil
}

type mockMetaVersionRepo struct {
	versions         map[string][]*model.DocumentVersion // documentID → versions
	deleteDocCalls   []string
	deleteDocErr     error
	mu               sync.Mutex
}

func (m *mockMetaVersionRepo) ListByDocument(_ context.Context, documentID string) ([]*model.DocumentVersion, error) {
	if m.versions != nil {
		if v, ok := m.versions[documentID]; ok {
			return v, nil
		}
	}
	return []*model.DocumentVersion{}, nil
}

func (m *mockMetaVersionRepo) DeleteByDocument(_ context.Context, documentID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.deleteDocCalls = append(m.deleteDocCalls, documentID)
	return m.deleteDocErr
}

type mockMetaArtifactRepo struct {
	deleteVersionCalls []string
	deleteVersionErr   error
	mu                 sync.Mutex
}

func (m *mockMetaArtifactRepo) DeleteByVersion(_ context.Context, _, _, versionID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.deleteVersionCalls = append(m.deleteVersionCalls, versionID)
	return m.deleteVersionErr
}

type mockMetaDiffRepo struct {
	deleteDocCalls []string
	deleteDocErr   error
	mu             sync.Mutex
}

func (m *mockMetaDiffRepo) DeleteByDocument(_ context.Context, _, documentID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.deleteDocCalls = append(m.deleteDocCalls, documentID)
	return m.deleteDocErr
}

type mockMetaAuditRepo struct {
	deleteDocCalls []string
	deleteDocErr   error
	mu             sync.Mutex
}

func (m *mockMetaAuditRepo) DeleteByDocument(_ context.Context, documentID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.deleteDocCalls = append(m.deleteDocCalls, documentID)
	return m.deleteDocErr
}

type mockMetaMetrics struct {
	metaDeletedTotal   int64
	metaScanDocsCount  int64
	mu                 sync.Mutex
}

func (m *mockMetaMetrics) IncRetentionMetaDeletedTotal(count int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.metaDeletedTotal += int64(count)
}

func (m *mockMetaMetrics) SetRetentionMetaScanDocsCount(count float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.metaScanDocsCount = int64(count)
}

type noopTransactor struct{}

func (t *noopTransactor) WithTransaction(_ context.Context, fn func(context.Context) error) error {
	return fn(context.Background())
}

// --- tests ---

func TestMetaCleanup_ConstructorPanics(t *testing.T) {
	cfg := testRetentionConfig()
	logger := testLogger()
	tx := &noopTransactor{}
	docRepo := &mockMetaDocRepo{}
	vRepo := &mockMetaVersionRepo{}
	aRepo := &mockMetaArtifactRepo{}
	dRepo := &mockMetaDiffRepo{}
	auditRepo := &mockMetaAuditRepo{}
	metrics := &mockMetaMetrics{}

	tests := []struct {
		name string
		fn   func()
	}{
		{"nil transactor", func() { NewDeletedMetaCleanupJob(nil, docRepo, vRepo, aRepo, dRepo, auditRepo, metrics, logger, cfg) }},
		{"nil doc repo", func() { NewDeletedMetaCleanupJob(tx, nil, vRepo, aRepo, dRepo, auditRepo, metrics, logger, cfg) }},
		{"nil version repo", func() { NewDeletedMetaCleanupJob(tx, docRepo, nil, aRepo, dRepo, auditRepo, metrics, logger, cfg) }},
		{"nil artifact repo", func() { NewDeletedMetaCleanupJob(tx, docRepo, vRepo, nil, dRepo, auditRepo, metrics, logger, cfg) }},
		{"nil diff repo", func() { NewDeletedMetaCleanupJob(tx, docRepo, vRepo, aRepo, nil, auditRepo, metrics, logger, cfg) }},
		{"nil audit repo", func() { NewDeletedMetaCleanupJob(tx, docRepo, vRepo, aRepo, dRepo, nil, metrics, logger, cfg) }},
		{"nil metrics", func() { NewDeletedMetaCleanupJob(tx, docRepo, vRepo, aRepo, dRepo, auditRepo, nil, logger, cfg) }},
		{"nil logger", func() { NewDeletedMetaCleanupJob(tx, docRepo, vRepo, aRepo, dRepo, auditRepo, metrics, nil, cfg) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r == nil {
					t.Fatal("expected panic")
				}
			}()
			tt.fn()
		})
	}
}

func TestMetaCleanup_Scan_HappyPath(t *testing.T) {
	doc := deletedDoc("doc-1", "org-A")
	versions := []*model.DocumentVersion{
		{VersionID: "v-1", DocumentID: "doc-1", OrganizationID: "org-A"},
		{VersionID: "v-2", DocumentID: "doc-1", OrganizationID: "org-A"},
	}

	docRepo := &mockMetaDocRepo{docs: []*model.Document{doc}}
	versionRepo := &mockMetaVersionRepo{
		versions: map[string][]*model.DocumentVersion{"doc-1": versions},
	}
	artifactRepo := &mockMetaArtifactRepo{}
	diffRepo := &mockMetaDiffRepo{}
	auditRepo := &mockMetaAuditRepo{}
	metrics := &mockMetaMetrics{}

	job := NewDeletedMetaCleanupJob(
		&noopTransactor{}, docRepo, versionRepo, artifactRepo,
		diffRepo, auditRepo, metrics, testLogger(), testRetentionConfig(),
	)
	job.scan()

	// Verify: Update called to clear current_version_id.
	if docRepo.updateCalls != 1 {
		t.Errorf("expected 1 update call, got %d", docRepo.updateCalls)
	}

	// Verify: Artifacts deleted per version.
	if len(artifactRepo.deleteVersionCalls) != 2 {
		t.Fatalf("expected 2 artifact delete calls, got %d", len(artifactRepo.deleteVersionCalls))
	}

	// Verify: Diffs deleted for document.
	if len(diffRepo.deleteDocCalls) != 1 || diffRepo.deleteDocCalls[0] != "doc-1" {
		t.Errorf("expected diff delete for doc-1, got %v", diffRepo.deleteDocCalls)
	}

	// Verify: Audit deleted for document.
	if len(auditRepo.deleteDocCalls) != 1 || auditRepo.deleteDocCalls[0] != "doc-1" {
		t.Errorf("expected audit delete for doc-1, got %v", auditRepo.deleteDocCalls)
	}

	// Verify: Versions deleted for document.
	if len(versionRepo.deleteDocCalls) != 1 || versionRepo.deleteDocCalls[0] != "doc-1" {
		t.Errorf("expected version delete for doc-1, got %v", versionRepo.deleteDocCalls)
	}

	// Verify: Document hard-deleted.
	if len(docRepo.deleteByIDCalls) != 1 || docRepo.deleteByIDCalls[0] != "doc-1" {
		t.Errorf("expected doc delete for doc-1, got %v", docRepo.deleteByIDCalls)
	}

	// Verify: Metrics.
	if metrics.metaDeletedTotal != 1 {
		t.Errorf("expected meta deleted total 1, got %d", metrics.metaDeletedTotal)
	}
	if metrics.metaScanDocsCount != 1 {
		t.Errorf("expected scan docs count 1, got %d", metrics.metaScanDocsCount)
	}
}

func TestMetaCleanup_Scan_MultipleDocs(t *testing.T) {
	docs := []*model.Document{
		deletedDoc("doc-1", "org-A"),
		deletedDoc("doc-2", "org-B"),
	}
	docRepo := &mockMetaDocRepo{docs: docs}
	versionRepo := &mockMetaVersionRepo{
		versions: map[string][]*model.DocumentVersion{
			"doc-1": {{VersionID: "v-1", DocumentID: "doc-1", OrganizationID: "org-A"}},
			"doc-2": {{VersionID: "v-2", DocumentID: "doc-2", OrganizationID: "org-B"}},
		},
	}
	artifactRepo := &mockMetaArtifactRepo{}
	diffRepo := &mockMetaDiffRepo{}
	auditRepo := &mockMetaAuditRepo{}
	metrics := &mockMetaMetrics{}

	job := NewDeletedMetaCleanupJob(
		&noopTransactor{}, docRepo, versionRepo, artifactRepo,
		diffRepo, auditRepo, metrics, testLogger(), testRetentionConfig(),
	)
	job.scan()

	if len(docRepo.deleteByIDCalls) != 2 {
		t.Fatalf("expected 2 doc deletes, got %d", len(docRepo.deleteByIDCalls))
	}
	if metrics.metaDeletedTotal != 2 {
		t.Errorf("expected meta deleted total 2, got %d", metrics.metaDeletedTotal)
	}
}

func TestMetaCleanup_Scan_NoDocs(t *testing.T) {
	docRepo := &mockMetaDocRepo{docs: []*model.Document{}}
	metrics := &mockMetaMetrics{}

	job := NewDeletedMetaCleanupJob(
		&noopTransactor{}, docRepo, &mockMetaVersionRepo{},
		&mockMetaArtifactRepo{}, &mockMetaDiffRepo{}, &mockMetaAuditRepo{},
		metrics, testLogger(), testRetentionConfig(),
	)
	job.scan()

	if metrics.metaDeletedTotal != 0 {
		t.Errorf("expected 0, got %d", metrics.metaDeletedTotal)
	}
}

func TestMetaCleanup_Scan_FinderError(t *testing.T) {
	docRepo := &mockMetaDocRepo{findErr: errors.New("db error")}
	metrics := &mockMetaMetrics{}

	job := NewDeletedMetaCleanupJob(
		&noopTransactor{}, docRepo, &mockMetaVersionRepo{},
		&mockMetaArtifactRepo{}, &mockMetaDiffRepo{}, &mockMetaAuditRepo{},
		metrics, testLogger(), testRetentionConfig(),
	)
	job.scan()

	if metrics.metaScanDocsCount != 0 {
		t.Errorf("expected 0, got %d", metrics.metaScanDocsCount)
	}
}

func TestMetaCleanup_Scan_ArtifactDeleteError(t *testing.T) {
	doc := deletedDoc("doc-1", "org-A")
	versions := []*model.DocumentVersion{
		{VersionID: "v-1", DocumentID: "doc-1", OrganizationID: "org-A"},
	}
	docRepo := &mockMetaDocRepo{docs: []*model.Document{doc}}
	versionRepo := &mockMetaVersionRepo{
		versions: map[string][]*model.DocumentVersion{"doc-1": versions},
	}
	artifactRepo := &mockMetaArtifactRepo{deleteVersionErr: errors.New("artifact error")}
	metrics := &mockMetaMetrics{}

	job := NewDeletedMetaCleanupJob(
		&noopTransactor{}, docRepo, versionRepo, artifactRepo,
		&mockMetaDiffRepo{}, &mockMetaAuditRepo{},
		metrics, testLogger(), testRetentionConfig(),
	)
	job.scan()

	// Document should not be counted as deleted.
	if metrics.metaDeletedTotal != 0 {
		t.Errorf("expected 0 on failure, got %d", metrics.metaDeletedTotal)
	}
}

func TestMetaCleanup_Scan_DiffDeleteError(t *testing.T) {
	doc := deletedDoc("doc-1", "org-A")
	docRepo := &mockMetaDocRepo{docs: []*model.Document{doc}}
	versionRepo := &mockMetaVersionRepo{}
	diffRepo := &mockMetaDiffRepo{deleteDocErr: errors.New("diff error")}
	metrics := &mockMetaMetrics{}

	job := NewDeletedMetaCleanupJob(
		&noopTransactor{}, docRepo, versionRepo, &mockMetaArtifactRepo{},
		diffRepo, &mockMetaAuditRepo{},
		metrics, testLogger(), testRetentionConfig(),
	)
	job.scan()

	if metrics.metaDeletedTotal != 0 {
		t.Errorf("expected 0 on failure, got %d", metrics.metaDeletedTotal)
	}
}

func TestMetaCleanup_Scan_DocNoVersions(t *testing.T) {
	doc := deletedDoc("doc-1", "org-A")
	docRepo := &mockMetaDocRepo{docs: []*model.Document{doc}}
	versionRepo := &mockMetaVersionRepo{}
	artifactRepo := &mockMetaArtifactRepo{}
	diffRepo := &mockMetaDiffRepo{}
	auditRepo := &mockMetaAuditRepo{}
	metrics := &mockMetaMetrics{}

	job := NewDeletedMetaCleanupJob(
		&noopTransactor{}, docRepo, versionRepo, artifactRepo,
		diffRepo, auditRepo, metrics, testLogger(), testRetentionConfig(),
	)
	job.scan()

	// Should succeed even with no versions.
	if len(docRepo.deleteByIDCalls) != 1 {
		t.Fatalf("expected 1 doc delete, got %d", len(docRepo.deleteByIDCalls))
	}
	if metrics.metaDeletedTotal != 1 {
		t.Errorf("expected 1, got %d", metrics.metaDeletedTotal)
	}
}

func TestMetaCleanup_StartStop(t *testing.T) {
	docRepo := &mockMetaDocRepo{docs: []*model.Document{}}
	cfg := testRetentionConfig()
	cfg.MetaScanInterval = 10 * time.Millisecond

	job := NewDeletedMetaCleanupJob(
		&noopTransactor{}, docRepo, &mockMetaVersionRepo{},
		&mockMetaArtifactRepo{}, &mockMetaDiffRepo{}, &mockMetaAuditRepo{},
		&mockMetaMetrics{}, testLogger(), cfg,
	)
	job.Start()

	time.Sleep(50 * time.Millisecond)
	job.Stop()

	select {
	case <-job.Done():
	case <-time.After(1 * time.Second):
		t.Fatal("job did not stop")
	}
}

func TestMetaCleanup_DoubleStop(t *testing.T) {
	docRepo := &mockMetaDocRepo{docs: []*model.Document{}}
	job := NewDeletedMetaCleanupJob(
		&noopTransactor{}, docRepo, &mockMetaVersionRepo{},
		&mockMetaArtifactRepo{}, &mockMetaDiffRepo{}, &mockMetaAuditRepo{},
		&mockMetaMetrics{}, testLogger(), testRetentionConfig(),
	)
	job.Start()
	job.Stop()
	job.Stop() // should not panic
	<-job.Done()
}

func TestMetaCleanup_DeleteOrder(t *testing.T) {
	doc := deletedDoc("doc-1", "org-A")
	versions := []*model.DocumentVersion{
		{VersionID: "v-1", DocumentID: "doc-1", OrganizationID: "org-A"},
	}

	// Track call order.
	var order []string
	var mu sync.Mutex

	docRepo := &orderTrackingDocRepo{
		docs:  []*model.Document{doc},
		order: &order,
		mu:    &mu,
	}
	versionRepo := &orderTrackingVersionRepo{
		versions: map[string][]*model.DocumentVersion{"doc-1": versions},
		order:    &order,
		mu:       &mu,
	}
	artifactRepo := &orderTrackingArtifactRepo{order: &order, mu: &mu}
	diffRepo := &orderTrackingDiffRepo{order: &order, mu: &mu}
	auditRepo := &orderTrackingAuditRepo{order: &order, mu: &mu}

	job := NewDeletedMetaCleanupJob(
		&noopTransactor{}, docRepo, versionRepo, artifactRepo,
		diffRepo, auditRepo, &mockMetaMetrics{}, testLogger(), testRetentionConfig(),
	)
	job.scan()

	expected := []string{
		"update_doc",
		"list_versions",
		"delete_artifacts_v-1",
		"delete_diffs",
		"delete_audit",
		"delete_versions",
		"delete_doc",
	}

	if len(order) != len(expected) {
		t.Fatalf("expected %d operations, got %d: %v", len(expected), len(order), order)
	}
	for i, exp := range expected {
		if order[i] != exp {
			t.Errorf("step %d: expected %s, got %s", i, exp, order[i])
		}
	}
}

// --- order-tracking mocks ---

type orderTrackingDocRepo struct {
	docs  []*model.Document
	order *[]string
	mu    *sync.Mutex
}

func (m *orderTrackingDocRepo) FindDeletedOlderThan(_ context.Context, _ time.Time, _ int) ([]*model.Document, error) {
	return m.docs, nil
}

func (m *orderTrackingDocRepo) Update(_ context.Context, _ *model.Document) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	*m.order = append(*m.order, "update_doc")
	return nil
}

func (m *orderTrackingDocRepo) DeleteByID(_ context.Context, _ string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	*m.order = append(*m.order, "delete_doc")
	return nil
}

type orderTrackingVersionRepo struct {
	versions map[string][]*model.DocumentVersion
	order    *[]string
	mu       *sync.Mutex
}

func (m *orderTrackingVersionRepo) ListByDocument(_ context.Context, documentID string) ([]*model.DocumentVersion, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	*m.order = append(*m.order, "list_versions")
	if v, ok := m.versions[documentID]; ok {
		return v, nil
	}
	return []*model.DocumentVersion{}, nil
}

func (m *orderTrackingVersionRepo) DeleteByDocument(_ context.Context, _ string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	*m.order = append(*m.order, "delete_versions")
	return nil
}

type orderTrackingArtifactRepo struct {
	order *[]string
	mu    *sync.Mutex
}

func (m *orderTrackingArtifactRepo) DeleteByVersion(_ context.Context, _, _, versionID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	*m.order = append(*m.order, "delete_artifacts_"+versionID)
	return nil
}

type orderTrackingDiffRepo struct {
	order *[]string
	mu    *sync.Mutex
}

func (m *orderTrackingDiffRepo) DeleteByDocument(_ context.Context, _, _ string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	*m.order = append(*m.order, "delete_diffs")
	return nil
}

type orderTrackingAuditRepo struct {
	order *[]string
	mu    *sync.Mutex
}

func (m *orderTrackingAuditRepo) DeleteByDocument(_ context.Context, _ string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	*m.order = append(*m.order, "delete_audit")
	return nil
}
