package outbox

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"contractpro/document-management/internal/domain/port"
)

// ---------------------------------------------------------------------------
// Constructor tests
// ---------------------------------------------------------------------------

func TestNewOutboxMetricsCollector_NilRepoPanics(t *testing.T) {
	assert.Panics(t, func() {
		NewOutboxMetricsCollector(nil, &mockMetrics{}, discardLogger(), 5*time.Second)
	})
}

func TestNewOutboxMetricsCollector_NilMetricsPanics(t *testing.T) {
	assert.Panics(t, func() {
		NewOutboxMetricsCollector(&metricsMockRepo{}, nil, discardLogger(), 5*time.Second)
	})
}

func TestNewOutboxMetricsCollector_NilLoggerPanics(t *testing.T) {
	assert.Panics(t, func() {
		NewOutboxMetricsCollector(&metricsMockRepo{}, &mockMetrics{}, nil, 5*time.Second)
	})
}

func TestNewOutboxMetricsCollector_ZeroIntervalDefaultsTo5s(t *testing.T) {
	c := NewOutboxMetricsCollector(&metricsMockRepo{}, &mockMetrics{}, discardLogger(), 0)
	assert.Equal(t, 5*time.Second, c.interval)
}

func TestNewOutboxMetricsCollector_NegativeIntervalDefaultsTo5s(t *testing.T) {
	c := NewOutboxMetricsCollector(&metricsMockRepo{}, &mockMetrics{}, discardLogger(), -1*time.Second)
	assert.Equal(t, 5*time.Second, c.interval)
}

// ---------------------------------------------------------------------------
// collect() tests
// ---------------------------------------------------------------------------

func TestOutboxMetricsCollector_Collect_HappyPath(t *testing.T) {
	repo := &metricsMockRepo{
		statsFn: func(_ context.Context) (int64, float64, error) {
			return 15, 120.5, nil
		},
	}
	metrics := &mockMetrics{}

	c := NewOutboxMetricsCollector(repo, metrics, discardLogger(), 5*time.Second)
	c.collect()

	metrics.mu.Lock()
	defer metrics.mu.Unlock()
	assert.Equal(t, float64(15), metrics.pendingCount)
	assert.InDelta(t, 120.5, metrics.oldestAge, 0.001)
}

func TestOutboxMetricsCollector_Collect_NoPending(t *testing.T) {
	repo := &metricsMockRepo{
		statsFn: func(_ context.Context) (int64, float64, error) {
			return 0, 0, nil
		},
	}
	metrics := &mockMetrics{}

	c := NewOutboxMetricsCollector(repo, metrics, discardLogger(), 5*time.Second)
	c.collect()

	metrics.mu.Lock()
	defer metrics.mu.Unlock()
	assert.Equal(t, float64(0), metrics.pendingCount)
	assert.Equal(t, float64(0), metrics.oldestAge)
}

func TestOutboxMetricsCollector_Collect_Error(t *testing.T) {
	repo := &metricsMockRepo{
		statsFn: func(_ context.Context) (int64, float64, error) {
			return 0, 0, errors.New("connection lost")
		},
	}
	metrics := &mockMetrics{}
	// Set initial values to verify they're NOT changed on error.
	metrics.pendingCount = 42
	metrics.oldestAge = 99.9

	c := NewOutboxMetricsCollector(repo, metrics,
		slog.New(slog.NewTextHandler(io.Discard, nil)), 5*time.Second)
	c.collect()

	metrics.mu.Lock()
	defer metrics.mu.Unlock()
	assert.Equal(t, float64(42), metrics.pendingCount, "should preserve old value on error")
	assert.InDelta(t, 99.9, metrics.oldestAge, 0.001, "should preserve old value on error")
}

// ---------------------------------------------------------------------------
// Start/Stop lifecycle test
// ---------------------------------------------------------------------------

func TestOutboxMetricsCollector_StartStop(t *testing.T) {
	var callCount atomic.Int32
	repo := &metricsMockRepo{
		statsFn: func(_ context.Context) (int64, float64, error) {
			callCount.Add(1)
			return 0, 0, nil
		},
	}

	c := NewOutboxMetricsCollector(repo, &mockMetrics{}, discardLogger(), 50*time.Millisecond)
	c.Start()

	// Wait for at least the initial + one tick collection.
	time.Sleep(120 * time.Millisecond)

	c.Stop()

	// Wait for goroutine to actually exit.
	select {
	case <-c.Done():
		// OK — goroutine has exited.
	case <-time.After(2 * time.Second):
		t.Fatal("metrics collector goroutine did not exit within timeout")
	}

	// Stop should be idempotent.
	c.Stop()

	assert.GreaterOrEqual(t, callCount.Load(), int32(1), "should have collected at least once")
}

// ---------------------------------------------------------------------------
// metricsMockRepo — mock for metrics collector tests.
// ---------------------------------------------------------------------------

var _ port.OutboxRepository = (*metricsMockRepo)(nil)

type metricsMockRepo struct {
	statsFn func(ctx context.Context) (int64, float64, error)
}

func (m *metricsMockRepo) Insert(_ context.Context, _ ...port.OutboxEntry) error {
	return nil
}

func (m *metricsMockRepo) FetchUnpublished(context.Context, int) ([]port.OutboxEntry, error) {
	return nil, nil
}

func (m *metricsMockRepo) MarkPublished(context.Context, []string) error {
	return nil
}

func (m *metricsMockRepo) DeletePublished(_ context.Context, _ time.Time, _ int) (int64, error) {
	return 0, nil
}

func (m *metricsMockRepo) PendingStats(ctx context.Context) (int64, float64, error) {
	if m.statsFn != nil {
		return m.statsFn(ctx)
	}
	return 0, 0, nil
}
