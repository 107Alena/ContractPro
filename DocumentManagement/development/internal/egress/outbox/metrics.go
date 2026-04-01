package outbox

import (
	"context"
	"log/slog"
	"time"

	"contractpro/document-management/internal/domain/port"
)

// OutboxMetricsCollector periodically queries the database to refresh the
// outbox gauge metrics: dm_outbox_pending_count and
// dm_outbox_oldest_pending_age_seconds.
//
// These gauges cannot be maintained purely by increment/decrement because
// the outbox writer and poller run in different transactions. A periodic
// query is the standard approach for outbox monitoring (REV-022).
type OutboxMetricsCollector struct {
	repo     port.OutboxRepository
	metrics  OutboxMetrics
	logger   *slog.Logger
	interval time.Duration

	stop chan struct{} // closed by Stop() to signal shutdown
	done chan struct{} // closed by run() when goroutine exits
}

// NewOutboxMetricsCollector creates a collector that refreshes gauges
// at the given interval.
func NewOutboxMetricsCollector(
	repo port.OutboxRepository,
	metrics OutboxMetrics,
	logger *slog.Logger,
	interval time.Duration,
) *OutboxMetricsCollector {
	if repo == nil {
		panic("outbox: metrics collector repository must not be nil")
	}
	if metrics == nil {
		panic("outbox: metrics collector metrics must not be nil")
	}
	if logger == nil {
		panic("outbox: metrics collector logger must not be nil")
	}
	if interval <= 0 {
		interval = 5 * time.Second
	}

	return &OutboxMetricsCollector{
		repo:     repo,
		metrics:  metrics,
		logger:   logger,
		interval: interval,
		stop:     make(chan struct{}),
		done:     make(chan struct{}),
	}
}

// Start launches the metrics collection loop in a background goroutine.
func (c *OutboxMetricsCollector) Start() {
	go c.run()
}

// Stop signals the collection loop to stop. Safe to call multiple times.
func (c *OutboxMetricsCollector) Stop() {
	select {
	case <-c.stop:
	default:
		close(c.stop)
	}
}

// Done returns a channel that is closed when the collector goroutine has exited.
func (c *OutboxMetricsCollector) Done() <-chan struct{} {
	return c.done
}

func (c *OutboxMetricsCollector) run() {
	defer close(c.done)

	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()

	// Run once immediately on start.
	c.collect()

	for {
		select {
		case <-c.stop:
			return
		case <-ticker.C:
			c.collect()
		}
	}
}

func (c *OutboxMetricsCollector) collect() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	count, oldestAge, err := c.repo.PendingStats(ctx)
	if err != nil {
		c.logger.Error("outbox metrics: failed to query pending stats", "error", err)
		return
	}

	c.metrics.SetPendingCount(float64(count))
	c.metrics.SetOldestPendingAge(oldestAge)
}
