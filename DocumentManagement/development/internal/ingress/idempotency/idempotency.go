package idempotency

import (
	"context"
	"fmt"

	"contractpro/document-management/internal/config"
	"contractpro/document-management/internal/domain/model"
	"contractpro/document-management/internal/domain/port"
)

// CheckStatus is the discrete decision returned by IdempotencyGuard.Check.
type CheckStatus int

const (
	// ResultProcess means no prior record exists; the caller should proceed.
	// The guard has atomically claimed a PROCESSING record with ProcessingTTL.
	ResultProcess CheckStatus = iota

	// ResultSkip means the event is already COMPLETED or another worker is
	// currently processing it. The caller should ACK without re-processing.
	ResultSkip

	// ResultReprocess means a stuck PROCESSING record was detected
	// (age >= StuckThreshold) and overwritten. The caller should proceed
	// as with ResultProcess.
	ResultReprocess
)

// String returns a human-readable label for the check status.
func (s CheckStatus) String() string {
	switch s {
	case ResultProcess:
		return "process"
	case ResultSkip:
		return "skip"
	case ResultReprocess:
		return "reprocess"
	default:
		return "unknown"
	}
}

// CheckResult bundles the idempotency decision (Status) with the persisted
// confirmation snapshot (DM-TASK-058).
//
// StoredSnapshot is non-nil only when Status == ResultSkip and the prior
// COMPLETED record carried a non-empty ResultSnapshot. Callers must re-publish
// the snapshot's confirmation payload to its topic when a duplicate is
// detected, closing the producer-crash-in-acknowledgment-window race for the
// 4 producer→DM confirmation flows (DP/LIC/RE artifacts + DP diff).
type CheckResult struct {
	Status         CheckStatus
	StoredSnapshot *string
}

// FallbackChecker is a function that checks the database for evidence that an
// event was already processed. Called only when Redis is unavailable.
// Returns true if the event was already processed (caller should skip).
// A nil FallbackChecker means "always process on Redis failure" (safe for
// read-only/idempotent query events).
type FallbackChecker func(ctx context.Context) (alreadyProcessed bool, err error)

// MetricsCollector is a consumer-side interface for the metrics the
// IdempotencyGuard emits. Keeps the dependency inverted.
type MetricsCollector interface {
	// IncFallbackTotal increments dm_idempotency_fallback_total counter.
	IncFallbackTotal(topic string)
	// IncCheckTotal increments dm_idempotency_check_total counter with the result label.
	IncCheckTotal(result string)
}

// Logger is a consumer-side interface for structured logging.
type Logger interface {
	Warn(msg string, keysAndValues ...any)
	Info(msg string, keysAndValues ...any)
}

// IdempotencyGuard provides event deduplication via Redis with DB fallback.
// It is an ingress-layer component used by the event consumer/dispatcher.
type IdempotencyGuard struct {
	store   port.IdempotencyStorePort
	cfg     config.IdempotencyConfig
	metrics MetricsCollector
	logger  Logger
}

// NewIdempotencyGuard creates a new IdempotencyGuard.
// Panics on nil dependencies (programming error caught at startup).
func NewIdempotencyGuard(
	store port.IdempotencyStorePort,
	cfg config.IdempotencyConfig,
	metrics MetricsCollector,
	logger Logger,
) *IdempotencyGuard {
	if store == nil {
		panic("idempotency: store must not be nil")
	}
	if metrics == nil {
		panic("idempotency: metrics must not be nil")
	}
	if logger == nil {
		panic("idempotency: logger must not be nil")
	}
	return &IdempotencyGuard{
		store:   store,
		cfg:     cfg,
		metrics: metrics,
		logger:  logger,
	}
}

// Check evaluates the idempotency state for the given key.
//
// Decision matrix:
//   - Key not found           → atomic SETNX PROCESSING (ProcessingTTL) → ResultProcess
//   - SETNX fails (claimed)   → re-read → evaluate COMPLETED / PROCESSING
//   - COMPLETED               → ResultSkip (StoredSnapshot set when record carries one)
//   - PROCESSING, age < stuck → ResultSkip (another worker is handling it)
//   - PROCESSING, age ≥ stuck → overwrite with SET → ResultReprocess
//   - Redis error             → DB fallback via checker → ResultProcess or ResultSkip
//   - Context cancelled       → error propagated to caller
//
// The topic parameter is used only for metrics labeling on the fallback path.
// The fallback checker is optional; nil means "always process on Redis failure".
//
// CheckResult.StoredSnapshot is populated only on the COMPLETED→Skip path and
// only when the prior record persisted a confirmation snapshot (DM-TASK-058).
// All other paths return StoredSnapshot=nil for backward compatibility.
func (g *IdempotencyGuard) Check(ctx context.Context, key string, topic string, fallback FallbackChecker) (CheckResult, error) {
	if err := ctx.Err(); err != nil {
		return CheckResult{Status: ResultSkip}, fmt.Errorf("idempotency check: %w", err)
	}

	// Attempt atomic claim via SETNX.
	newRecord := model.NewIdempotencyRecord(key)
	acquired, err := g.store.SetNX(ctx, newRecord, g.cfg.ProcessingTTL)
	if err != nil {
		return g.handleRedisFailure(ctx, key, topic, fallback)
	}
	if acquired {
		g.metrics.IncCheckTotal("process")
		return CheckResult{Status: ResultProcess}, nil
	}

	// Key already exists — read the existing record.
	record, err := g.store.Get(ctx, key)
	if err != nil {
		return g.handleRedisFailure(ctx, key, topic, fallback)
	}

	// Key expired between SETNX and GET — treat as new.
	if record == nil {
		g.metrics.IncCheckTotal("process")
		return CheckResult{Status: ResultProcess}, nil
	}

	if record.Status == model.IdempotencyStatusCompleted {
		g.metrics.IncCheckTotal("skip")
		result := CheckResult{Status: ResultSkip}
		if record.ResultSnapshot != "" {
			snapshot := record.ResultSnapshot
			result.StoredSnapshot = &snapshot
		}
		return result, nil
	}

	// Status == PROCESSING
	if record.IsStuck(g.cfg.StuckThreshold) {
		g.logger.Warn("stuck PROCESSING record detected, re-processing",
			"key", key, "age_seconds", record.Age().Seconds())
		// Overwrite the stuck record with a fresh PROCESSING record (single SET, not delete+set).
		freshRecord := model.NewIdempotencyRecord(key)
		if setErr := g.store.Set(ctx, freshRecord, g.cfg.ProcessingTTL); setErr != nil {
			g.logger.Warn("failed to overwrite stuck record",
				"key", key, "error", setErr)
		}
		g.metrics.IncCheckTotal("reprocess")
		return CheckResult{Status: ResultReprocess}, nil
	}

	// PROCESSING and not stuck — another worker is handling it
	g.metrics.IncCheckTotal("skip")
	return CheckResult{Status: ResultSkip}, nil
}

// MarkCompleted transitions the key to COMPLETED status with the configured
// TTL (24h). Called after the handler returns success.
//
// resultSnapshot is the optional confirmation envelope to persist alongside
// the COMPLETED record so that future duplicate deliveries can re-publish the
// same confirmation payload (DM-TASK-058). Pass "" for events that do not
// produce a direct response confirmation (query topics, downstream-only flows).
//
// Errors are logged but not propagated because the business transaction has
// already committed via outbox.
func (g *IdempotencyGuard) MarkCompleted(ctx context.Context, key string, resultSnapshot string) error {
	record := model.NewIdempotencyRecord(key)
	record.MarkCompleted(resultSnapshot)
	if err := g.store.Set(ctx, record, g.cfg.TTL); err != nil {
		g.logger.Warn("failed to mark idempotency record as COMPLETED",
			"key", key, "error", err)
		return err
	}
	return nil
}

// Cleanup removes the PROCESSING record for the given key.
// Called when the handler returns a non-retryable error so a future
// redelivery can re-enter the pipeline.
func (g *IdempotencyGuard) Cleanup(ctx context.Context, key string) error {
	if err := g.store.Delete(ctx, key); err != nil {
		g.logger.Warn("failed to cleanup idempotency record",
			"key", key, "error", err)
		return err
	}
	return nil
}

// handleRedisFailure falls back to DB check when Redis is unavailable.
func (g *IdempotencyGuard) handleRedisFailure(ctx context.Context, key, topic string, fallback FallbackChecker) (CheckResult, error) {
	g.metrics.IncFallbackTotal(topic)
	g.logger.Warn("redis unavailable, falling back to DB check",
		"key", key, "topic", topic, "degraded", true)

	if fallback == nil {
		g.metrics.IncCheckTotal("fallback_process")
		return CheckResult{Status: ResultProcess}, nil
	}

	alreadyProcessed, err := fallback(ctx)
	if err != nil {
		g.logger.Warn("DB fallback check failed, allowing processing",
			"key", key, "error", err)
		g.metrics.IncCheckTotal("fallback_process")
		return CheckResult{Status: ResultProcess}, nil
	}

	if alreadyProcessed {
		g.metrics.IncCheckTotal("fallback_skip")
		return CheckResult{Status: ResultSkip}, nil
	}

	g.metrics.IncCheckTotal("fallback_process")
	return CheckResult{Status: ResultProcess}, nil
}
