package retention

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"contractpro/document-management/internal/config"
	"contractpro/document-management/internal/domain/model"
)

// --- mocks ---

type mockDeletedDocFinder struct {
	docs []*model.Document
	err  error
}

func (m *mockDeletedDocFinder) FindDeletedOlderThan(_ context.Context, _ time.Time, _ int) ([]*model.Document, error) {
	return m.docs, m.err
}

type mockBlobDeleter struct {
	deletedPrefixes []string
	errOnPrefix     string
	err             error
}

func (m *mockBlobDeleter) DeleteByPrefix(_ context.Context, prefix string) error {
	if m.errOnPrefix != "" && prefix == m.errOnPrefix {
		return m.err
	}
	m.deletedPrefixes = append(m.deletedPrefixes, prefix)
	return nil
}

type mockBlobMetrics struct {
	blobDeletedTotal   atomic.Int64
	blobScanDocsCount  atomic.Int64
}

func (m *mockBlobMetrics) IncRetentionBlobDeletedTotal(count int) {
	m.blobDeletedTotal.Add(int64(count))
}

func (m *mockBlobMetrics) SetRetentionBlobScanDocsCount(count float64) {
	m.blobScanDocsCount.Store(int64(count))
}

func testRetentionConfig() config.RetentionConfig {
	return config.RetentionConfig{
		DeletedBlobDays:   30,
		DeletedMetaDays:   365,
		AuditDays:         1095,
		BlobScanInterval:  1 * time.Hour,
		MetaScanInterval:  24 * time.Hour,
		AuditScanInterval: 24 * time.Hour,
		BatchSize:         50,
		ScanTimeout:       5 * time.Second,
		AuditMonthsAhead:  3,
	}
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func deletedDoc(id, orgID string) *model.Document {
	deletedAt := time.Now().UTC().AddDate(0, 0, -60)
	return &model.Document{
		DocumentID:     id,
		OrganizationID: orgID,
		Status:         model.DocumentStatusDeleted,
		DeletedAt:      &deletedAt,
	}
}

// --- tests ---

func TestBlobCleanup_ConstructorPanics(t *testing.T) {
	cfg := testRetentionConfig()
	logger := testLogger()
	finder := &mockDeletedDocFinder{}
	storage := &mockBlobDeleter{}
	metrics := &mockBlobMetrics{}

	tests := []struct {
		name string
		fn   func()
	}{
		{"nil finder", func() { NewDeletedBlobCleanupJob(nil, storage, metrics, logger, cfg) }},
		{"nil storage", func() { NewDeletedBlobCleanupJob(finder, nil, metrics, logger, cfg) }},
		{"nil metrics", func() { NewDeletedBlobCleanupJob(finder, storage, nil, logger, cfg) }},
		{"nil logger", func() { NewDeletedBlobCleanupJob(finder, storage, metrics, nil, cfg) }},
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

func TestBlobCleanup_Constructor(t *testing.T) {
	job := NewDeletedBlobCleanupJob(
		&mockDeletedDocFinder{},
		&mockBlobDeleter{},
		&mockBlobMetrics{},
		testLogger(),
		testRetentionConfig(),
	)
	if job == nil {
		t.Fatal("expected non-nil job")
	}
}

func TestBlobCleanup_Scan_HappyPath(t *testing.T) {
	docs := []*model.Document{
		deletedDoc("doc-1", "org-A"),
		deletedDoc("doc-2", "org-B"),
	}
	finder := &mockDeletedDocFinder{docs: docs}
	storage := &mockBlobDeleter{}
	metrics := &mockBlobMetrics{}

	job := NewDeletedBlobCleanupJob(finder, storage, metrics, testLogger(), testRetentionConfig())
	job.scan()

	if len(storage.deletedPrefixes) != 2 {
		t.Fatalf("expected 2 deleted prefixes, got %d", len(storage.deletedPrefixes))
	}
	if storage.deletedPrefixes[0] != "org-A/doc-1/" {
		t.Errorf("expected prefix org-A/doc-1/, got %s", storage.deletedPrefixes[0])
	}
	if storage.deletedPrefixes[1] != "org-B/doc-2/" {
		t.Errorf("expected prefix org-B/doc-2/, got %s", storage.deletedPrefixes[1])
	}
	if metrics.blobDeletedTotal.Load() != 2 {
		t.Errorf("expected blob deleted total 2, got %d", metrics.blobDeletedTotal.Load())
	}
	if metrics.blobScanDocsCount.Load() != 2 {
		t.Errorf("expected scan docs count 2, got %d", metrics.blobScanDocsCount.Load())
	}
}

func TestBlobCleanup_Scan_NoDocs(t *testing.T) {
	finder := &mockDeletedDocFinder{docs: []*model.Document{}}
	storage := &mockBlobDeleter{}
	metrics := &mockBlobMetrics{}

	job := NewDeletedBlobCleanupJob(finder, storage, metrics, testLogger(), testRetentionConfig())
	job.scan()

	if len(storage.deletedPrefixes) != 0 {
		t.Fatalf("expected 0 deleted prefixes, got %d", len(storage.deletedPrefixes))
	}
	if metrics.blobDeletedTotal.Load() != 0 {
		t.Errorf("expected blob deleted total 0, got %d", metrics.blobDeletedTotal.Load())
	}
}

func TestBlobCleanup_Scan_FinderError(t *testing.T) {
	finder := &mockDeletedDocFinder{err: errors.New("db error")}
	storage := &mockBlobDeleter{}
	metrics := &mockBlobMetrics{}

	job := NewDeletedBlobCleanupJob(finder, storage, metrics, testLogger(), testRetentionConfig())
	job.scan()

	if len(storage.deletedPrefixes) != 0 {
		t.Fatalf("expected 0 deleted prefixes, got %d", len(storage.deletedPrefixes))
	}
}

func TestBlobCleanup_Scan_PartialStorageFailure(t *testing.T) {
	docs := []*model.Document{
		deletedDoc("doc-1", "org-A"),
		deletedDoc("doc-2", "org-B"),
		deletedDoc("doc-3", "org-C"),
	}
	finder := &mockDeletedDocFinder{docs: docs}
	storage := &mockBlobDeleter{
		errOnPrefix: "org-B/doc-2/",
		err:         errors.New("s3 error"),
	}
	metrics := &mockBlobMetrics{}

	job := NewDeletedBlobCleanupJob(finder, storage, metrics, testLogger(), testRetentionConfig())
	job.scan()

	if len(storage.deletedPrefixes) != 2 {
		t.Fatalf("expected 2 deleted prefixes, got %d", len(storage.deletedPrefixes))
	}
	if metrics.blobDeletedTotal.Load() != 2 {
		t.Errorf("expected blob deleted total 2, got %d", metrics.blobDeletedTotal.Load())
	}
}

func TestBlobCleanup_Scan_ContextCancelled(t *testing.T) {
	docs := []*model.Document{
		deletedDoc("doc-1", "org-A"),
		deletedDoc("doc-2", "org-B"),
	}
	finder := &mockDeletedDocFinder{docs: docs}
	// Use a storage that cancels context on first call.
	cancelStorage := &cancellingBlobDeleter{}
	metrics := &mockBlobMetrics{}

	cfg := testRetentionConfig()
	cfg.ScanTimeout = 1 * time.Millisecond // very short timeout

	job := NewDeletedBlobCleanupJob(finder, cancelStorage, metrics, testLogger(), cfg)
	// Scan will timeout quickly and break the loop.
	time.Sleep(5 * time.Millisecond) // let timeout expire
	job.scan()
}

type cancellingBlobDeleter struct{}

func (m *cancellingBlobDeleter) DeleteByPrefix(ctx context.Context, _ string) error {
	// Simulate slow operation.
	time.Sleep(10 * time.Millisecond)
	return ctx.Err()
}

func TestBlobCleanup_StartStop(t *testing.T) {
	finder := &mockDeletedDocFinder{docs: []*model.Document{}}
	storage := &mockBlobDeleter{}
	metrics := &mockBlobMetrics{}

	cfg := testRetentionConfig()
	cfg.BlobScanInterval = 10 * time.Millisecond

	job := NewDeletedBlobCleanupJob(finder, storage, metrics, testLogger(), cfg)
	job.Start()

	time.Sleep(50 * time.Millisecond)

	job.Stop()
	select {
	case <-job.Done():
	case <-time.After(1 * time.Second):
		t.Fatal("job did not stop within timeout")
	}
}

func TestBlobCleanup_DoubleStop(t *testing.T) {
	finder := &mockDeletedDocFinder{docs: []*model.Document{}}
	storage := &mockBlobDeleter{}
	metrics := &mockBlobMetrics{}

	job := NewDeletedBlobCleanupJob(finder, storage, metrics, testLogger(), testRetentionConfig())
	job.Start()
	job.Stop()
	job.Stop() // second stop should not panic
	<-job.Done()
}
