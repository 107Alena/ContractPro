package outbox

import (
	"context"
	"log/slog"
	"time"

	"contractpro/document-management/internal/config"
	"contractpro/document-management/internal/domain/port"
)

// Outbox entry status constants.
const (
	StatusPending   = "PENDING"
	StatusConfirmed = "CONFIRMED"
)

// BrokerPublisher is a consumer-side interface covering the broker's Publish
// method. The broker.Client satisfies this via synchronous publisher confirms.
type BrokerPublisher interface {
	Publish(ctx context.Context, topic string, payload []byte) error
}

// OutboxMetrics is the interface for outbox-specific metrics.
// Implemented by the observability layer; stubbed in tests.
type OutboxMetrics interface {
	// SetPendingCount sets the dm_outbox_pending_count gauge.
	SetPendingCount(count float64)

	// SetOldestPendingAge sets the dm_outbox_oldest_pending_age_seconds gauge.
	SetOldestPendingAge(ageSeconds float64)

	// IncPublished increments the count of successfully published events.
	IncPublished(topic string)

	// IncPublishFailed increments the count of failed publish attempts.
	IncPublishFailed(topic string)

	// IncCleanedUp increments the count of cleaned-up CONFIRMED entries.
	IncCleanedUp(count int64)
}

// OutboxPoller is a background relay that reads PENDING events from the
// outbox_events table, publishes them to RabbitMQ, marks them CONFIRMED,
// and periodically cleans up old entries.
//
// Delivery guarantee: at-least-once. If the broker accepts a message but the
// subsequent DB commit fails, the entry stays PENDING and will be re-published
// on the next poll cycle. Downstream consumers must be idempotent.
//
// The entire fetch-publish-mark cycle runs within a single database
// transaction so that FOR UPDATE SKIP LOCKED row locks are held until
// MarkPublished completes. This prevents other poller instances from
// picking up the same rows.
type OutboxPoller struct {
	repo       port.OutboxRepository
	transactor port.Transactor
	broker     BrokerPublisher
	metrics    OutboxMetrics
	logger     *slog.Logger
	cfg        config.OutboxConfig

	stop chan struct{} // closed by Stop() to signal shutdown
	done chan struct{} // closed by run() when goroutine exits
}

// NewOutboxPoller creates an OutboxPoller with the given dependencies.
// Panics if any required dependency is nil (programmer error at startup).
func NewOutboxPoller(
	repo port.OutboxRepository,
	transactor port.Transactor,
	broker BrokerPublisher,
	metrics OutboxMetrics,
	logger *slog.Logger,
	cfg config.OutboxConfig,
) *OutboxPoller {
	if repo == nil {
		panic("outbox: poller repository must not be nil")
	}
	if transactor == nil {
		panic("outbox: poller transactor must not be nil")
	}
	if broker == nil {
		panic("outbox: poller broker must not be nil")
	}
	if metrics == nil {
		panic("outbox: poller metrics must not be nil")
	}
	if logger == nil {
		panic("outbox: poller logger must not be nil")
	}

	return &OutboxPoller{
		repo:       repo,
		transactor: transactor,
		broker:     broker,
		metrics:    metrics,
		logger:     logger,
		cfg:        cfg,
		stop:       make(chan struct{}),
		done:       make(chan struct{}),
	}
}

// Start launches the polling loop in a background goroutine.
// Call Stop() for graceful shutdown.
func (p *OutboxPoller) Start() {
	go p.run()
}

// Stop signals the polling loop to stop. Safe to call multiple times.
func (p *OutboxPoller) Stop() {
	select {
	case <-p.stop:
	default:
		close(p.stop)
	}
}

// Done returns a channel that is closed when the poller goroutine has exited.
// Use this to wait for graceful completion: p.Stop(); <-p.Done()
func (p *OutboxPoller) Done() <-chan struct{} {
	return p.done
}

// run is the main polling loop.
func (p *OutboxPoller) run() {
	defer close(p.done)

	pollTicker := time.NewTicker(p.cfg.PollInterval)
	defer pollTicker.Stop()

	// Cleanup runs less frequently: once per minute.
	cleanupTicker := time.NewTicker(1 * time.Minute)
	defer cleanupTicker.Stop()

	for {
		select {
		case <-p.stop:
			p.logger.Info("outbox poller: shutting down")
			return
		case <-pollTicker.C:
			p.poll()
		case <-cleanupTicker.C:
			p.cleanup()
		}
	}
}

// poll fetches a batch of PENDING entries, publishes each to the broker,
// and marks successfully published entries as CONFIRMED.
//
// FIFO guarantee (BRE-006): entries are fetched ORDER BY aggregate_id, created_at.
// When publishing fails for an entry, all subsequent entries with the same
// aggregate_id are skipped in the current batch. They remain PENDING and will
// be retried on the next poll cycle, preserving per-aggregate event ordering.
// Entries with empty AggregateID have no ordering constraint and are published
// independently — a failure on one does not block others.
//
// Note: BrokerPublisher.Publish must be synchronous (blocking until the broker
// confirms delivery). If Publish returns nil, the message is guaranteed to be
// in the broker. This is required for the at-least-once guarantee.
func (p *OutboxPoller) poll() {
	ctx, cancel := context.WithTimeout(context.Background(), p.cfg.LockTimeout)
	defer cancel()

	err := p.transactor.WithTransaction(ctx, func(txCtx context.Context) error {
		entries, err := p.repo.FetchUnpublished(txCtx, p.cfg.BatchSize)
		if err != nil {
			return err
		}
		if len(entries) == 0 {
			return nil
		}

		// failedAggs tracks aggregate IDs that had a publish failure in this batch.
		// Subsequent entries for the same aggregate are skipped to preserve FIFO.
		failedAggs := make(map[string]struct{})
		publishedIDs := make([]string, 0, len(entries))

		for _, entry := range entries {
			// BRE-006: skip entries whose aggregate already had a failure in this batch.
			if entry.AggregateID != "" {
				if _, blocked := failedAggs[entry.AggregateID]; blocked {
					p.logger.Warn("outbox poller: skipping entry — prior failure in same aggregate",
						"entry_id", entry.ID,
						"aggregate_id", entry.AggregateID,
						"topic", entry.Topic,
					)
					continue
				}
			}

			if pubErr := p.broker.Publish(txCtx, entry.Topic, entry.Payload); pubErr != nil {
				p.logger.Error("outbox poller: publish failed",
					"entry_id", entry.ID,
					"topic", entry.Topic,
					"error", pubErr,
				)
				p.metrics.IncPublishFailed(entry.Topic)
				if entry.AggregateID != "" {
					failedAggs[entry.AggregateID] = struct{}{}
				}
				continue
			}
			publishedIDs = append(publishedIDs, entry.ID)
			p.metrics.IncPublished(entry.Topic)
		}

		if len(publishedIDs) > 0 {
			return p.repo.MarkPublished(txCtx, publishedIDs)
		}
		return nil
	})

	if err != nil {
		p.logger.Error("outbox poller: poll cycle failed", "error", err)
	}
}

// cleanup deletes CONFIRMED entries older than DM_OUTBOX_CLEANUP_HOURS.
// Deletes in batches of 1000 to avoid large single-statement deletes (BRE-018).
// Runs outside a transaction: each DELETE is auto-committed.
func (p *OutboxPoller) cleanup() {
	threshold := time.Now().UTC().Add(-time.Duration(p.cfg.CleanupHours) * time.Hour)
	const batchLimit = 1000

	for {
		select {
		case <-p.stop:
			return
		default:
		}

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		deleted, err := p.repo.DeletePublished(ctx, threshold, batchLimit)
		cancel()

		if err != nil {
			p.logger.Error("outbox poller: cleanup failed", "error", err)
			return
		}

		if deleted > 0 {
			p.metrics.IncCleanedUp(deleted)
			p.logger.Debug("outbox poller: cleaned up entries", "count", deleted)
		}

		if deleted < batchLimit {
			return
		}
	}
}
