package orphancleanup

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"

	"contractpro/document-management/internal/config"
	"contractpro/document-management/internal/domain/port"
)

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

type mockOrphanCandidateRepo struct {
	mu sync.Mutex

	findResult []port.OrphanCandidate
	findErr    error

	existsResults map[string]bool
	existsErr     error

	deletedKeys []string
	deleteErr   error

	inserted []port.OrphanCandidate
	insertErr error
}

func (m *mockOrphanCandidateRepo) FindOlderThan(_ context.Context, _ time.Time, _ int) ([]port.OrphanCandidate, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.findErr != nil {
		return nil, m.findErr
	}
	return m.findResult, nil
}

func (m *mockOrphanCandidateRepo) ExistsByStorageKey(_ context.Context, key string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.existsErr != nil {
		return false, m.existsErr
	}
	return m.existsResults[key], nil
}

func (m *mockOrphanCandidateRepo) DeleteByKeys(_ context.Context, keys []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.deletedKeys = append(m.deletedKeys, keys...)
	return m.deleteErr
}

func (m *mockOrphanCandidateRepo) Insert(_ context.Context, c port.OrphanCandidate) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.inserted = append(m.inserted, c)
	return m.insertErr
}

func (m *mockOrphanCandidateRepo) getDeletedKeys() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]string, len(m.deletedKeys))
	copy(cp, m.deletedKeys)
	return cp
}

type mockObjectStorage struct {
	mu         sync.Mutex
	deleteErr  error
	deletedKeys []string
	// failOn maps a specific key to an error — simulates per-key failure.
	failOn map[string]error
}

func (m *mockObjectStorage) DeleteObject(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if e, ok := m.failOn[key]; ok {
		return e
	}
	if m.deleteErr != nil {
		return m.deleteErr
	}
	m.deletedKeys = append(m.deletedKeys, key)
	return nil
}

func (m *mockObjectStorage) getDeletedKeys() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]string, len(m.deletedKeys))
	copy(cp, m.deletedKeys)
	return cp
}

type mockOrphanMetrics struct {
	mu                  sync.Mutex
	deletedTotal        int
	candidateGaugeValues []float64
}

func (m *mockOrphanMetrics) IncOrphansDeletedTotal(count int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.deletedTotal += count
}

func (m *mockOrphanMetrics) SetOrphanCandidatesCount(count float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.candidateGaugeValues = append(m.candidateGaugeValues, count)
}

func (m *mockOrphanMetrics) getDeletedTotal() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.deletedTotal
}

func (m *mockOrphanMetrics) getLastGaugeValue() float64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.candidateGaugeValues) == 0 {
		return -1
	}
	return m.candidateGaugeValues[len(m.candidateGaugeValues)-1]
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func testConfig() config.OrphanCleanupConfig {
	return config.OrphanCleanupConfig{
		ScanInterval: 1 * time.Hour,
		BatchSize:    100,
		GracePeriod:  1 * time.Hour,
		ScanTimeout:  5 * time.Second,
	}
}

func makeCandidate(key string, age time.Duration) port.OrphanCandidate {
	return port.OrphanCandidate{
		StorageKey: key,
		VersionID:  "v1",
		CreatedAt:  time.Now().UTC().Add(-age),
	}
}

// ---------------------------------------------------------------------------
// Constructor panic tests
// ---------------------------------------------------------------------------

func TestNewOrphanCleanupJob_PanicOnNilRepo(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for nil candidate repo")
		}
	}()
	NewOrphanCleanupJob(nil, &mockObjectStorage{}, &mockOrphanMetrics{}, testLogger(), testConfig())
}

func TestNewOrphanCleanupJob_PanicOnNilStorage(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for nil storage")
		}
	}()
	NewOrphanCleanupJob(&mockOrphanCandidateRepo{}, nil, &mockOrphanMetrics{}, testLogger(), testConfig())
}

func TestNewOrphanCleanupJob_PanicOnNilMetrics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for nil metrics")
		}
	}()
	NewOrphanCleanupJob(&mockOrphanCandidateRepo{}, &mockObjectStorage{}, nil, testLogger(), testConfig())
}

func TestNewOrphanCleanupJob_PanicOnNilLogger(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for nil logger")
		}
	}()
	NewOrphanCleanupJob(&mockOrphanCandidateRepo{}, &mockObjectStorage{}, &mockOrphanMetrics{}, nil, testConfig())
}

func TestNewOrphanCleanupJob_Success(t *testing.T) {
	job := NewOrphanCleanupJob(
		&mockOrphanCandidateRepo{}, &mockObjectStorage{}, &mockOrphanMetrics{},
		testLogger(), testConfig(),
	)
	if job == nil {
		t.Fatal("expected non-nil job")
	}
}

// ---------------------------------------------------------------------------
// scan() tests
// ---------------------------------------------------------------------------

func TestScan_NoCandidates(t *testing.T) {
	repo := &mockOrphanCandidateRepo{findResult: []port.OrphanCandidate{}}
	storage := &mockObjectStorage{}
	metrics := &mockOrphanMetrics{}

	job := NewOrphanCleanupJob(repo, storage, metrics, testLogger(), testConfig())
	job.scan()

	if got := metrics.getLastGaugeValue(); got != 0 {
		t.Errorf("gauge: want 0, got %v", got)
	}
	if got := metrics.getDeletedTotal(); got != 0 {
		t.Errorf("deleted total: want 0, got %d", got)
	}
	if got := len(storage.getDeletedKeys()); got != 0 {
		t.Errorf("S3 deletes: want 0, got %d", got)
	}
}

func TestScan_AllOrphans(t *testing.T) {
	candidates := []port.OrphanCandidate{
		makeCandidate("org/doc/v1/ocr_text", 2*time.Hour),
		makeCandidate("org/doc/v1/semantic_tree", 2*time.Hour),
		makeCandidate("org/doc/v1/extracted_text", 2*time.Hour),
	}
	repo := &mockOrphanCandidateRepo{
		findResult:    candidates,
		existsResults: map[string]bool{}, // all false
	}
	storage := &mockObjectStorage{}
	metrics := &mockOrphanMetrics{}

	job := NewOrphanCleanupJob(repo, storage, metrics, testLogger(), testConfig())
	job.scan()

	if got := metrics.getLastGaugeValue(); got != 3 {
		t.Errorf("gauge: want 3, got %v", got)
	}
	if got := metrics.getDeletedTotal(); got != 3 {
		t.Errorf("deleted total: want 3, got %d", got)
	}
	if got := len(storage.getDeletedKeys()); got != 3 {
		t.Errorf("S3 deletes: want 3, got %d", got)
	}
	if got := len(repo.getDeletedKeys()); got != 3 {
		t.Errorf("candidate row deletes: want 3, got %d", got)
	}
}

func TestScan_AllFalsePositives(t *testing.T) {
	candidates := []port.OrphanCandidate{
		makeCandidate("org/doc/v1/ocr_text", 2*time.Hour),
		makeCandidate("org/doc/v1/semantic_tree", 2*time.Hour),
	}
	repo := &mockOrphanCandidateRepo{
		findResult: candidates,
		existsResults: map[string]bool{
			"org/doc/v1/ocr_text":      true,
			"org/doc/v1/semantic_tree": true,
		},
	}
	storage := &mockObjectStorage{}
	metrics := &mockOrphanMetrics{}

	job := NewOrphanCleanupJob(repo, storage, metrics, testLogger(), testConfig())
	job.scan()

	// No S3 deletes for false positives.
	if got := len(storage.getDeletedKeys()); got != 0 {
		t.Errorf("S3 deletes: want 0, got %d", got)
	}
	// Counter not incremented for false positives.
	if got := metrics.getDeletedTotal(); got != 0 {
		t.Errorf("deleted total: want 0, got %d", got)
	}
	// But candidate rows are removed.
	if got := len(repo.getDeletedKeys()); got != 2 {
		t.Errorf("candidate row deletes: want 2, got %d", got)
	}
}

func TestScan_MixedOrphansAndFalsePositives(t *testing.T) {
	candidates := []port.OrphanCandidate{
		makeCandidate("org/doc/v1/ocr_text", 2*time.Hour),      // false positive
		makeCandidate("org/doc/v1/semantic_tree", 2*time.Hour),  // orphan
		makeCandidate("org/doc/v1/extracted_text", 2*time.Hour), // orphan
	}
	repo := &mockOrphanCandidateRepo{
		findResult: candidates,
		existsResults: map[string]bool{
			"org/doc/v1/ocr_text": true, // exists → false positive
		},
	}
	storage := &mockObjectStorage{}
	metrics := &mockOrphanMetrics{}

	job := NewOrphanCleanupJob(repo, storage, metrics, testLogger(), testConfig())
	job.scan()

	if got := metrics.getDeletedTotal(); got != 2 {
		t.Errorf("deleted total: want 2, got %d", got)
	}
	if got := len(storage.getDeletedKeys()); got != 2 {
		t.Errorf("S3 deletes: want 2, got %d", got)
	}
	// All 3 candidate rows removed (2 orphans + 1 false positive).
	if got := len(repo.getDeletedKeys()); got != 3 {
		t.Errorf("candidate row deletes: want 3, got %d", got)
	}
}

func TestScan_FindOlderThanError(t *testing.T) {
	repo := &mockOrphanCandidateRepo{
		findErr: fmt.Errorf("db connection lost"),
	}
	storage := &mockObjectStorage{}
	metrics := &mockOrphanMetrics{}

	job := NewOrphanCleanupJob(repo, storage, metrics, testLogger(), testConfig())
	job.scan()

	// No S3 calls when find fails.
	if got := len(storage.getDeletedKeys()); got != 0 {
		t.Errorf("S3 deletes: want 0, got %d", got)
	}
	if got := metrics.getDeletedTotal(); got != 0 {
		t.Errorf("deleted total: want 0, got %d", got)
	}
}

func TestScan_ExistsByStorageKeyError(t *testing.T) {
	candidates := []port.OrphanCandidate{
		makeCandidate("org/doc/v1/ocr_text", 2*time.Hour),     // will fail
		makeCandidate("org/doc/v1/semantic_tree", 2*time.Hour), // orphan, will succeed
	}
	repo := &mockOrphanCandidateRepo{
		findResult:    candidates,
		existsResults: map[string]bool{},
	}
	storage := &mockObjectStorage{}
	metrics := &mockOrphanMetrics{}

	// Wrapper to simulate per-key DB failure on ExistsByStorageKey.
	failingRepo := &failingExistsRepo{
		inner:   repo,
		failKey: "org/doc/v1/ocr_text",
		failErr: fmt.Errorf("db timeout"),
	}

	job := NewOrphanCleanupJob(failingRepo, storage, metrics, testLogger(), testConfig())
	job.scan()

	// First candidate skipped, second is an orphan — deleted.
	if got := metrics.getDeletedTotal(); got != 1 {
		t.Errorf("deleted total: want 1, got %d", got)
	}
	if got := len(storage.getDeletedKeys()); got != 1 {
		t.Errorf("S3 deletes: want 1, got %d", got)
	}
	// Only second candidate removed from table.
	deletedKeys := failingRepo.inner.getDeletedKeys()
	if got := len(deletedKeys); got != 1 {
		t.Errorf("candidate row deletes: want 1, got %d", got)
	}
}

func TestScan_DeleteObjectError(t *testing.T) {
	candidates := []port.OrphanCandidate{
		makeCandidate("org/doc/v1/ocr_text", 2*time.Hour),     // S3 fail
		makeCandidate("org/doc/v1/semantic_tree", 2*time.Hour), // success
	}
	repo := &mockOrphanCandidateRepo{
		findResult:    candidates,
		existsResults: map[string]bool{},
	}
	storage := &mockObjectStorage{
		failOn: map[string]error{
			"org/doc/v1/ocr_text": fmt.Errorf("s3 timeout"),
		},
	}
	metrics := &mockOrphanMetrics{}

	job := NewOrphanCleanupJob(repo, storage, metrics, testLogger(), testConfig())
	job.scan()

	// Only second blob deleted.
	if got := metrics.getDeletedTotal(); got != 1 {
		t.Errorf("deleted total: want 1, got %d", got)
	}
	if got := len(storage.getDeletedKeys()); got != 1 {
		t.Errorf("S3 deletes: want 1, got %d", got)
	}
	// Only second candidate removed (first stays for retry next scan).
	if got := len(repo.getDeletedKeys()); got != 1 {
		t.Errorf("candidate row deletes: want 1, got %d", got)
	}
}

func TestScan_DeleteByKeysError(t *testing.T) {
	candidates := []port.OrphanCandidate{
		makeCandidate("org/doc/v1/ocr_text", 2*time.Hour),
	}
	repo := &mockOrphanCandidateRepo{
		findResult:    candidates,
		existsResults: map[string]bool{},
		deleteErr:     fmt.Errorf("db error on delete"),
	}
	storage := &mockObjectStorage{}
	metrics := &mockOrphanMetrics{}

	job := NewOrphanCleanupJob(repo, storage, metrics, testLogger(), testConfig())
	job.scan()

	// S3 delete still succeeded even though DB delete failed.
	if got := len(storage.getDeletedKeys()); got != 1 {
		t.Errorf("S3 deletes: want 1, got %d", got)
	}
	// Counter still incremented (orphan was deleted from S3).
	if got := metrics.getDeletedTotal(); got != 1 {
		t.Errorf("deleted total: want 1, got %d", got)
	}
}

func TestScan_ContextCancellation(t *testing.T) {
	// Create many candidates to increase chance of context check.
	candidates := make([]port.OrphanCandidate, 50)
	for i := range candidates {
		candidates[i] = makeCandidate(fmt.Sprintf("org/doc/v1/type_%d", i), 2*time.Hour)
	}

	repo := &mockOrphanCandidateRepo{
		findResult:    candidates,
		existsResults: map[string]bool{},
	}
	storage := &mockObjectStorage{}
	metrics := &mockOrphanMetrics{}

	// Use very short scan timeout to trigger context cancellation.
	cfg := testConfig()
	cfg.ScanTimeout = 1 * time.Nanosecond

	job := NewOrphanCleanupJob(repo, storage, metrics, testLogger(), cfg)
	job.scan()

	// Some candidates may be processed, but not necessarily all 50.
	// Just verify the job didn't panic and metrics are reasonable.
	if got := metrics.getDeletedTotal(); got > 50 {
		t.Errorf("deleted total: want <= 50, got %d", got)
	}
}

func TestStartStop_GracefulShutdown(t *testing.T) {
	repo := &mockOrphanCandidateRepo{findResult: []port.OrphanCandidate{}}
	storage := &mockObjectStorage{}
	metrics := &mockOrphanMetrics{}

	cfg := testConfig()
	cfg.ScanInterval = 100 * time.Millisecond // fast ticker for test

	job := NewOrphanCleanupJob(repo, storage, metrics, testLogger(), cfg)
	job.Start()

	// Give it a moment to start.
	time.Sleep(50 * time.Millisecond)

	job.Stop()

	select {
	case <-job.Done():
		// OK — shut down cleanly.
	case <-time.After(2 * time.Second):
		t.Fatal("job did not shut down within timeout")
	}
}

func TestStop_DoubleStopSafe(t *testing.T) {
	repo := &mockOrphanCandidateRepo{findResult: []port.OrphanCandidate{}}
	storage := &mockObjectStorage{}
	metrics := &mockOrphanMetrics{}

	job := NewOrphanCleanupJob(repo, storage, metrics, testLogger(), testConfig())
	job.Start()

	// Double stop should not panic.
	job.Stop()
	job.Stop()

	select {
	case <-job.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("job did not shut down within timeout")
	}
}

func TestScan_GaugeSetToZeroOnEmpty(t *testing.T) {
	repo := &mockOrphanCandidateRepo{findResult: []port.OrphanCandidate{}}
	metrics := &mockOrphanMetrics{}

	job := NewOrphanCleanupJob(repo, &mockObjectStorage{}, metrics, testLogger(), testConfig())
	job.scan()

	if got := metrics.getLastGaugeValue(); got != 0 {
		t.Errorf("gauge on empty: want 0, got %v", got)
	}
}

func TestScan_MetricsNotIncrementedForZeroOrphans(t *testing.T) {
	// All false positives — counter should not be incremented.
	candidates := []port.OrphanCandidate{
		makeCandidate("key1", 2*time.Hour),
	}
	repo := &mockOrphanCandidateRepo{
		findResult:    candidates,
		existsResults: map[string]bool{"key1": true},
	}
	metrics := &mockOrphanMetrics{}

	job := NewOrphanCleanupJob(repo, &mockObjectStorage{}, metrics, testLogger(), testConfig())
	job.scan()

	if got := metrics.getDeletedTotal(); got != 0 {
		t.Errorf("deleted total: want 0, got %d", got)
	}
}

// ---------------------------------------------------------------------------
// Helper: failingExistsRepo wraps mockOrphanCandidateRepo to fail on a
// specific key for ExistsByStorageKey.
// ---------------------------------------------------------------------------

type failingExistsRepo struct {
	inner   *mockOrphanCandidateRepo
	failKey string
	failErr error
}

func (r *failingExistsRepo) FindOlderThan(ctx context.Context, cutoff time.Time, limit int) ([]port.OrphanCandidate, error) {
	return r.inner.FindOlderThan(ctx, cutoff, limit)
}

func (r *failingExistsRepo) ExistsByStorageKey(ctx context.Context, key string) (bool, error) {
	if key == r.failKey {
		return false, r.failErr
	}
	return r.inner.ExistsByStorageKey(ctx, key)
}

func (r *failingExistsRepo) DeleteByKeys(ctx context.Context, keys []string) error {
	return r.inner.DeleteByKeys(ctx, keys)
}

func (r *failingExistsRepo) Insert(ctx context.Context, c port.OrphanCandidate) error {
	return r.inner.Insert(ctx, c)
}
