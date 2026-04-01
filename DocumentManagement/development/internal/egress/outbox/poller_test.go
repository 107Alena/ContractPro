package outbox

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"contractpro/document-management/internal/config"
	"contractpro/document-management/internal/domain/port"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func testOutboxConfig() config.OutboxConfig {
	return config.OutboxConfig{
		PollInterval: 50 * time.Millisecond,
		BatchSize:    10,
		LockTimeout:  2 * time.Second,
		CleanupHours: 48,
	}
}

// ---------------------------------------------------------------------------
// NewOutboxPoller constructor tests
// ---------------------------------------------------------------------------

func TestNewOutboxPoller_NilRepoPanics(t *testing.T) {
	assert.Panics(t, func() {
		NewOutboxPoller(nil, &mockTransactor{}, &mockBroker{}, &mockMetrics{}, discardLogger(), testOutboxConfig())
	})
}

func TestNewOutboxPoller_NilTransactorPanics(t *testing.T) {
	assert.Panics(t, func() {
		NewOutboxPoller(&pollerMockRepo{}, nil, &mockBroker{}, &mockMetrics{}, discardLogger(), testOutboxConfig())
	})
}

func TestNewOutboxPoller_NilBrokerPanics(t *testing.T) {
	assert.Panics(t, func() {
		NewOutboxPoller(&pollerMockRepo{}, &mockTransactor{}, nil, &mockMetrics{}, discardLogger(), testOutboxConfig())
	})
}

func TestNewOutboxPoller_NilMetricsPanics(t *testing.T) {
	assert.Panics(t, func() {
		NewOutboxPoller(&pollerMockRepo{}, &mockTransactor{}, &mockBroker{}, nil, discardLogger(), testOutboxConfig())
	})
}

func TestNewOutboxPoller_NilLoggerPanics(t *testing.T) {
	assert.Panics(t, func() {
		NewOutboxPoller(&pollerMockRepo{}, &mockTransactor{}, &mockBroker{}, &mockMetrics{}, nil, testOutboxConfig())
	})
}

// ---------------------------------------------------------------------------
// poll() tests
// ---------------------------------------------------------------------------

func TestOutboxPoller_Poll_HappyPath(t *testing.T) {
	entries := []port.OutboxEntry{
		{ID: "e1", Topic: "t1", Payload: []byte(`{"a":1}`)},
		{ID: "e2", Topic: "t2", Payload: []byte(`{"b":2}`)},
		{ID: "e3", Topic: "t1", Payload: []byte(`{"c":3}`)},
	}

	repo := &pollerMockRepo{
		fetchFn: func(_ context.Context, limit int) ([]port.OutboxEntry, error) {
			assert.Equal(t, 10, limit)
			return entries, nil
		},
	}
	broker := &mockBroker{}
	metrics := &mockMetrics{}

	p := NewOutboxPoller(repo, &mockTransactor{}, broker, metrics, discardLogger(), testOutboxConfig())
	p.poll()

	// All 3 published.
	require.Len(t, broker.publishCalls, 3)
	assert.Equal(t, "t1", broker.publishCalls[0].topic)
	assert.Equal(t, "t2", broker.publishCalls[1].topic)
	assert.Equal(t, "t1", broker.publishCalls[2].topic)

	// All 3 marked as published.
	require.Len(t, repo.markCalls, 1)
	assert.ElementsMatch(t, []string{"e1", "e2", "e3"}, repo.markCalls[0])

	// Metrics.
	assert.Equal(t, 3, metrics.publishedCount())
	assert.Equal(t, 0, metrics.publishFailedCount())
}

func TestOutboxPoller_Poll_EmptyBatch(t *testing.T) {
	repo := &pollerMockRepo{
		fetchFn: func(_ context.Context, _ int) ([]port.OutboxEntry, error) {
			return []port.OutboxEntry{}, nil
		},
	}
	broker := &mockBroker{}
	metrics := &mockMetrics{}

	p := NewOutboxPoller(repo, &mockTransactor{}, broker, metrics, discardLogger(), testOutboxConfig())
	p.poll()

	assert.Empty(t, broker.publishCalls)
	assert.Empty(t, repo.markCalls)
}

func TestOutboxPoller_Poll_PartialPublishFailure(t *testing.T) {
	entries := []port.OutboxEntry{
		{ID: "e1", Topic: "t1", Payload: []byte(`{}`)},
		{ID: "e2", Topic: "t2", Payload: []byte(`{}`)},
		{ID: "e3", Topic: "t3", Payload: []byte(`{}`)},
	}

	callCount := 0
	repo := &pollerMockRepo{
		fetchFn: func(_ context.Context, _ int) ([]port.OutboxEntry, error) {
			return entries, nil
		},
	}
	broker := &mockBroker{
		publishFn: func(_ context.Context, topic string, _ []byte) error {
			callCount++
			if topic == "t2" {
				return errors.New("broker unavailable")
			}
			return nil
		},
	}
	metrics := &mockMetrics{}

	p := NewOutboxPoller(repo, &mockTransactor{}, broker, metrics, discardLogger(), testOutboxConfig())
	p.poll()

	// Only e1 and e3 should be marked.
	require.Len(t, repo.markCalls, 1)
	assert.ElementsMatch(t, []string{"e1", "e3"}, repo.markCalls[0])

	assert.Equal(t, 2, metrics.publishedCount())
	assert.Equal(t, 1, metrics.publishFailedCount())
}

func TestOutboxPoller_Poll_AllPublishFail(t *testing.T) {
	entries := []port.OutboxEntry{
		{ID: "e1", Topic: "t1", Payload: []byte(`{}`)},
		{ID: "e2", Topic: "t2", Payload: []byte(`{}`)},
	}

	repo := &pollerMockRepo{
		fetchFn: func(_ context.Context, _ int) ([]port.OutboxEntry, error) {
			return entries, nil
		},
	}
	broker := &mockBroker{
		publishFn: func(_ context.Context, _ string, _ []byte) error {
			return errors.New("broker down")
		},
	}
	metrics := &mockMetrics{}

	p := NewOutboxPoller(repo, &mockTransactor{}, broker, metrics, discardLogger(), testOutboxConfig())
	p.poll()

	// No entries should be marked.
	assert.Empty(t, repo.markCalls)
	assert.Equal(t, 0, metrics.publishedCount())
	assert.Equal(t, 2, metrics.publishFailedCount())
}

func TestOutboxPoller_Poll_FetchError(t *testing.T) {
	repo := &pollerMockRepo{
		fetchFn: func(_ context.Context, _ int) ([]port.OutboxEntry, error) {
			return nil, errors.New("db timeout")
		},
	}
	broker := &mockBroker{}

	p := NewOutboxPoller(repo, &mockTransactor{}, broker, &mockMetrics{}, discardLogger(), testOutboxConfig())
	// Should not panic; error is logged.
	p.poll()

	assert.Empty(t, broker.publishCalls)
}

func TestOutboxPoller_Poll_MarkPublishedError(t *testing.T) {
	entries := []port.OutboxEntry{
		{ID: "e1", Topic: "t1", Payload: []byte(`{}`)},
	}

	repo := &pollerMockRepo{
		fetchFn: func(_ context.Context, _ int) ([]port.OutboxEntry, error) {
			return entries, nil
		},
		markFn: func(_ context.Context, _ []string) error {
			return errors.New("serialization failure")
		},
	}
	broker := &mockBroker{}

	p := NewOutboxPoller(repo, &mockTransactor{}, broker, &mockMetrics{}, discardLogger(), testOutboxConfig())
	// Should not panic; error is logged and entry will be re-fetched on next cycle.
	p.poll()

	assert.Len(t, broker.publishCalls, 1)
}

// ---------------------------------------------------------------------------
// cleanup() tests
// ---------------------------------------------------------------------------

func TestOutboxPoller_Cleanup_SingleBatch(t *testing.T) {
	repo := &pollerMockRepo{
		deleteFn: func(_ context.Context, olderThan time.Time, limit int) (int64, error) {
			assert.Equal(t, 1000, limit)
			// Verify threshold is approximately 48 hours ago.
			expectedThreshold := time.Now().UTC().Add(-48 * time.Hour)
			assert.WithinDuration(t, expectedThreshold, olderThan, 5*time.Second)
			return 500, nil // Less than batch limit → done.
		},
	}
	metrics := &mockMetrics{}

	p := NewOutboxPoller(repo, &mockTransactor{}, &mockBroker{}, metrics, discardLogger(), testOutboxConfig())
	p.cleanup()

	assert.Equal(t, 1, repo.deleteCallCount)
	assert.Equal(t, int64(500), metrics.cleanedUpTotal())
}

func TestOutboxPoller_Cleanup_MultipleBatches(t *testing.T) {
	callNum := 0
	repo := &pollerMockRepo{
		deleteFn: func(_ context.Context, _ time.Time, _ int) (int64, error) {
			callNum++
			switch callNum {
			case 1:
				return 1000, nil // Full batch → continue.
			case 2:
				return 1000, nil // Full batch → continue.
			default:
				return 300, nil // Partial → done.
			}
		},
	}
	metrics := &mockMetrics{}

	p := NewOutboxPoller(repo, &mockTransactor{}, &mockBroker{}, metrics, discardLogger(), testOutboxConfig())
	p.cleanup()

	assert.Equal(t, 3, repo.deleteCallCount)
	assert.Equal(t, int64(2300), metrics.cleanedUpTotal())
}

func TestOutboxPoller_Cleanup_Error(t *testing.T) {
	repo := &pollerMockRepo{
		deleteFn: func(_ context.Context, _ time.Time, _ int) (int64, error) {
			return 0, errors.New("disk error")
		},
	}

	p := NewOutboxPoller(repo, &mockTransactor{}, &mockBroker{}, &mockMetrics{}, discardLogger(), testOutboxConfig())
	// Should not panic.
	p.cleanup()

	assert.Equal(t, 1, repo.deleteCallCount)
}

// ---------------------------------------------------------------------------
// Start/Stop lifecycle test
// ---------------------------------------------------------------------------

func TestOutboxPoller_StartStop(t *testing.T) {
	repo := &pollerMockRepo{
		fetchFn: func(_ context.Context, _ int) ([]port.OutboxEntry, error) {
			return []port.OutboxEntry{}, nil
		},
	}

	p := NewOutboxPoller(repo, &mockTransactor{}, &mockBroker{}, &mockMetrics{}, discardLogger(), testOutboxConfig())
	p.Start()

	// Give it a moment to run a poll cycle.
	time.Sleep(100 * time.Millisecond)

	p.Stop()

	// Wait for the goroutine to actually finish.
	select {
	case <-p.Done():
		// OK — goroutine has exited.
	case <-time.After(2 * time.Second):
		t.Fatal("poller goroutine did not exit within timeout")
	}

	// Stop should be idempotent.
	p.Stop()
}

// ---------------------------------------------------------------------------
// Mock types
// ---------------------------------------------------------------------------

// pollerMockRepo — mock for poller tests.
var _ port.OutboxRepository = (*pollerMockRepo)(nil)

type pollerMockRepo struct {
	fetchFn  func(ctx context.Context, limit int) ([]port.OutboxEntry, error)
	markFn   func(ctx context.Context, ids []string) error
	deleteFn func(ctx context.Context, olderThan time.Time, limit int) (int64, error)
	statsFn  func(ctx context.Context) (int64, float64, error)

	markCalls       [][]string
	deleteCallCount int
}

func (m *pollerMockRepo) Insert(_ context.Context, _ ...port.OutboxEntry) error {
	return nil
}

func (m *pollerMockRepo) FetchUnpublished(ctx context.Context, limit int) ([]port.OutboxEntry, error) {
	if m.fetchFn != nil {
		return m.fetchFn(ctx, limit)
	}
	return nil, nil
}

func (m *pollerMockRepo) MarkPublished(ctx context.Context, ids []string) error {
	m.markCalls = append(m.markCalls, ids)
	if m.markFn != nil {
		return m.markFn(ctx, ids)
	}
	return nil
}

func (m *pollerMockRepo) DeletePublished(ctx context.Context, olderThan time.Time, limit int) (int64, error) {
	m.deleteCallCount++
	if m.deleteFn != nil {
		return m.deleteFn(ctx, olderThan, limit)
	}
	return 0, nil
}

func (m *pollerMockRepo) PendingStats(ctx context.Context) (int64, float64, error) {
	if m.statsFn != nil {
		return m.statsFn(ctx)
	}
	return 0, 0, nil
}

// mockTransactor — executes fn directly (no real DB transaction), propagating context.
type mockTransactor struct{}

func (t *mockTransactor) WithTransaction(ctx context.Context, fn func(ctx context.Context) error) error {
	return fn(ctx)
}

// mockBroker — records publish calls.
type mockBroker struct {
	publishFn    func(ctx context.Context, topic string, payload []byte) error
	publishCalls []publishCall
}

type publishCall struct {
	topic   string
	payload []byte
}

func (b *mockBroker) Publish(ctx context.Context, topic string, payload []byte) error {
	b.publishCalls = append(b.publishCalls, publishCall{topic: topic, payload: payload})
	if b.publishFn != nil {
		return b.publishFn(ctx, topic, payload)
	}
	return nil
}

// mockMetrics — records metric updates.
type mockMetrics struct {
	mu              sync.Mutex
	published       []string
	publishFailed   []string
	cleanedUp       []int64
	pendingCount    float64
	oldestAge       float64
}

func (m *mockMetrics) SetPendingCount(count float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pendingCount = count
}

func (m *mockMetrics) SetOldestPendingAge(ageSeconds float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.oldestAge = ageSeconds
}

func (m *mockMetrics) IncPublished(topic string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.published = append(m.published, topic)
}

func (m *mockMetrics) IncPublishFailed(topic string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.publishFailed = append(m.publishFailed, topic)
}

func (m *mockMetrics) IncCleanedUp(count int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cleanedUp = append(m.cleanedUp, count)
}

func (m *mockMetrics) publishedCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.published)
}

func (m *mockMetrics) publishFailedCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.publishFailed)
}

func (m *mockMetrics) cleanedUpTotal() int64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	var total int64
	for _, c := range m.cleanedUp {
		total += c
	}
	return total
}
