package retention

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// --- mocks ---

type mockPartitionManager struct {
	ensureCalls    int
	ensureMonths   int
	ensureErr      error
	dropCutoff     time.Time
	dropResult     int
	dropErr        error
	mu             sync.Mutex
}

func (m *mockPartitionManager) EnsurePartitions(_ context.Context, monthsAhead int) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ensureCalls++
	m.ensureMonths = monthsAhead
	if m.ensureErr != nil {
		return 0, m.ensureErr
	}
	return monthsAhead + 1, nil
}

func (m *mockPartitionManager) DropPartitionsOlderThan(_ context.Context, cutoff time.Time) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.dropCutoff = cutoff
	return m.dropResult, m.dropErr
}

type mockAuditPartitionMetrics struct {
	createdTotal int64
	droppedTotal int64
	mu           sync.Mutex
}

func (m *mockAuditPartitionMetrics) IncRetentionAuditPartitionsCreatedTotal(count int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.createdTotal += int64(count)
}

func (m *mockAuditPartitionMetrics) IncRetentionAuditPartitionsDroppedTotal(count int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.droppedTotal += int64(count)
}

// --- tests ---

func TestAuditPartition_ConstructorPanics(t *testing.T) {
	cfg := testRetentionConfig()
	logger := testLogger()
	pm := &mockPartitionManager{}
	metrics := &mockAuditPartitionMetrics{}

	tests := []struct {
		name string
		fn   func()
	}{
		{"nil partition mgr", func() { NewAuditPartitionJob(nil, metrics, logger, cfg) }},
		{"nil metrics", func() { NewAuditPartitionJob(pm, nil, logger, cfg) }},
		{"nil logger", func() { NewAuditPartitionJob(pm, metrics, nil, cfg) }},
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

func TestAuditPartition_Constructor(t *testing.T) {
	job := NewAuditPartitionJob(
		&mockPartitionManager{},
		&mockAuditPartitionMetrics{},
		testLogger(),
		testRetentionConfig(),
	)
	if job == nil {
		t.Fatal("expected non-nil job")
	}
}

func TestAuditPartition_Scan_HappyPath(t *testing.T) {
	pm := &mockPartitionManager{dropResult: 2}
	metrics := &mockAuditPartitionMetrics{}
	cfg := testRetentionConfig()
	cfg.AuditMonthsAhead = 3

	job := NewAuditPartitionJob(pm, metrics, testLogger(), cfg)
	job.scan()

	if pm.ensureCalls != 1 {
		t.Errorf("expected 1 ensure call, got %d", pm.ensureCalls)
	}
	if pm.ensureMonths != 3 {
		t.Errorf("expected 3 months ahead, got %d", pm.ensureMonths)
	}
	// createdTotal = monthsAhead + 1 = 4
	if metrics.createdTotal != 4 {
		t.Errorf("expected created total 4, got %d", metrics.createdTotal)
	}
	if metrics.droppedTotal != 2 {
		t.Errorf("expected dropped total 2, got %d", metrics.droppedTotal)
	}
}

func TestAuditPartition_Scan_NothingDropped(t *testing.T) {
	pm := &mockPartitionManager{dropResult: 0}
	metrics := &mockAuditPartitionMetrics{}

	job := NewAuditPartitionJob(pm, metrics, testLogger(), testRetentionConfig())
	job.scan()

	if metrics.droppedTotal != 0 {
		t.Errorf("expected 0 dropped, got %d", metrics.droppedTotal)
	}
}

func TestAuditPartition_Scan_EnsureError(t *testing.T) {
	pm := &mockPartitionManager{ensureErr: errors.New("partition error")}
	metrics := &mockAuditPartitionMetrics{}

	job := NewAuditPartitionJob(pm, metrics, testLogger(), testRetentionConfig())
	job.scan()

	// Drop should NOT be called if ensure fails.
	if metrics.droppedTotal != 0 {
		t.Errorf("expected 0 dropped on ensure failure, got %d", metrics.droppedTotal)
	}
	if metrics.createdTotal != 0 {
		t.Errorf("expected 0 created on ensure failure, got %d", metrics.createdTotal)
	}
}

func TestAuditPartition_Scan_DropError(t *testing.T) {
	pm := &mockPartitionManager{dropErr: errors.New("drop error")}
	metrics := &mockAuditPartitionMetrics{}

	job := NewAuditPartitionJob(pm, metrics, testLogger(), testRetentionConfig())
	job.scan()

	// Ensure should still report success.
	if metrics.createdTotal == 0 {
		t.Error("expected created total > 0 even on drop failure")
	}
	// Drop total should be 0 on error.
	if metrics.droppedTotal != 0 {
		t.Errorf("expected 0 dropped on error, got %d", metrics.droppedTotal)
	}
}

func TestAuditPartition_Scan_DropCutoff(t *testing.T) {
	pm := &mockPartitionManager{dropResult: 0}
	metrics := &mockAuditPartitionMetrics{}
	cfg := testRetentionConfig()
	cfg.AuditDays = 1095 // ~3 years

	job := NewAuditPartitionJob(pm, metrics, testLogger(), cfg)
	job.scan()

	// Verify cutoff is approximately 1095 days ago.
	expected := time.Now().UTC().AddDate(0, 0, -1095)
	diff := pm.dropCutoff.Sub(expected)
	if diff < -5*time.Second || diff > 5*time.Second {
		t.Errorf("cutoff %v too far from expected %v (diff: %v)", pm.dropCutoff, expected, diff)
	}
}

func TestAuditPartition_StartStop(t *testing.T) {
	pm := &mockPartitionManager{}
	metrics := &mockAuditPartitionMetrics{}
	cfg := testRetentionConfig()
	cfg.AuditScanInterval = 10 * time.Millisecond

	job := NewAuditPartitionJob(pm, metrics, testLogger(), cfg)
	job.Start()

	time.Sleep(50 * time.Millisecond)
	job.Stop()

	select {
	case <-job.Done():
	case <-time.After(1 * time.Second):
		t.Fatal("job did not stop")
	}
}

func TestAuditPartition_DoubleStop(t *testing.T) {
	pm := &mockPartitionManager{}
	metrics := &mockAuditPartitionMetrics{}

	job := NewAuditPartitionJob(pm, metrics, testLogger(), testRetentionConfig())
	job.Start()
	job.Stop()
	job.Stop() // should not panic
	<-job.Done()
}
