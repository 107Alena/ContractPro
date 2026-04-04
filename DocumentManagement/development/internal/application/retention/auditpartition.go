package retention

import (
	"context"
	"log/slog"
	"time"

	"contractpro/document-management/internal/config"
)

// AuditPartitionMetrics is the consumer-side interface for audit partition metrics.
// Implemented by observability.Metrics.
type AuditPartitionMetrics interface {
	IncRetentionAuditPartitionsCreatedTotal(count int)
	IncRetentionAuditPartitionsDroppedTotal(count int)
}

// PartitionManager manages monthly audit_records partitions.
// Implemented by postgres.AuditPartitionManager (via pool-injecting wrapper).
type PartitionManager interface {
	EnsurePartitions(ctx context.Context, monthsAhead int) (int, error)
	DropPartitionsOlderThan(ctx context.Context, cutoff time.Time) (int, error)
}

// AuditPartitionJob is a background job that creates future monthly
// partitions for audit_records and drops partitions older than
// DM_RETENTION_AUDIT_DAYS (REV-027).
//
// Processing flow per scan:
//  1. EnsurePartitions — create next N months of partitions
//  2. DropPartitionsOlderThan — drop partitions expired per retention policy
//  3. Update metrics and log summary
type AuditPartitionJob struct {
	partitionMgr PartitionManager
	metrics      AuditPartitionMetrics
	logger       *slog.Logger
	cfg          config.RetentionConfig

	stop chan struct{}
	done chan struct{}
}

// NewAuditPartitionJob creates a new audit partition job.
// Panics if any required dependency is nil (programmer error at startup).
func NewAuditPartitionJob(
	partitionMgr PartitionManager,
	metrics AuditPartitionMetrics,
	logger *slog.Logger,
	cfg config.RetentionConfig,
) *AuditPartitionJob {
	if partitionMgr == nil {
		panic("retention: partition manager must not be nil")
	}
	if metrics == nil {
		panic("retention: metrics must not be nil")
	}
	if logger == nil {
		panic("retention: logger must not be nil")
	}

	return &AuditPartitionJob{
		partitionMgr: partitionMgr,
		metrics:      metrics,
		logger:       logger,
		cfg:          cfg,
		stop:         make(chan struct{}),
		done:         make(chan struct{}),
	}
}

// Start launches the scanning loop in a background goroutine.
func (j *AuditPartitionJob) Start() {
	go j.run()
}

// Stop signals the scanning loop to stop. Safe to call multiple times.
func (j *AuditPartitionJob) Stop() {
	select {
	case <-j.stop:
	default:
		close(j.stop)
	}
}

// Done returns a channel that is closed when the job goroutine has exited.
func (j *AuditPartitionJob) Done() <-chan struct{} {
	return j.done
}

func (j *AuditPartitionJob) run() {
	defer close(j.done)

	ticker := time.NewTicker(j.cfg.AuditScanInterval)
	defer ticker.Stop()

	for {
		select {
		case <-j.stop:
			j.logger.Info("retention audit partition: shutting down")
			return
		case <-ticker.C:
			j.scan()
		}
	}
}

func (j *AuditPartitionJob) scan() {
	ctx, cancel := context.WithTimeout(context.Background(), j.cfg.ScanTimeout)
	defer cancel()

	// Create future partitions.
	created, err := j.partitionMgr.EnsurePartitions(ctx, j.cfg.AuditMonthsAhead)
	if err != nil {
		j.logger.Error("retention audit partition: ensure partitions failed", "error", err)
		return
	}
	if created > 0 {
		j.metrics.IncRetentionAuditPartitionsCreatedTotal(created)
	}

	// Drop expired partitions.
	cutoff := time.Now().UTC().AddDate(0, 0, -j.cfg.AuditDays)
	dropped, err := j.partitionMgr.DropPartitionsOlderThan(ctx, cutoff)
	if err != nil {
		j.logger.Error("retention audit partition: drop partitions failed", "error", err)
		return
	}

	if dropped > 0 {
		j.metrics.IncRetentionAuditPartitionsDroppedTotal(dropped)
	}

	j.logger.Info("retention audit partition: scan completed",
		"months_ahead", j.cfg.AuditMonthsAhead,
		"dropped", dropped,
		"audit_retention_days", j.cfg.AuditDays,
	)
}
