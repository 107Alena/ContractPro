package dispatcher

import (
	"context"

	"contractpro/document-processing/internal/domain/model"
	"contractpro/document-processing/internal/domain/port"
	"contractpro/document-processing/internal/infra/observability"
)

// Dispatcher coordinates the ingress pipeline: idempotency guard, concurrency
// limiter, and handler dispatch. Every public method always returns nil to
// prevent poison-pill requeue loops; errors are logged and acknowledged.
type Dispatcher struct {
	idempotency port.IdempotencyStorePort
	limiter     port.ConcurrencyLimiterPort
	processing  port.ProcessingCommandHandler
	comparison  port.ComparisonCommandHandler
	logger      *observability.Logger
}

// NewDispatcher creates a Dispatcher wired to the given dependencies.
// Panics if any required dependency is nil (programmer error at startup).
func NewDispatcher(
	idempotency port.IdempotencyStorePort,
	limiter port.ConcurrencyLimiterPort,
	processing port.ProcessingCommandHandler,
	comparison port.ComparisonCommandHandler,
	logger *observability.Logger,
) *Dispatcher {
	if idempotency == nil {
		panic("dispatcher: idempotency store must not be nil")
	}
	if limiter == nil {
		panic("dispatcher: concurrency limiter must not be nil")
	}
	if processing == nil {
		panic("dispatcher: processing handler must not be nil")
	}
	if comparison == nil {
		panic("dispatcher: comparison handler must not be nil")
	}
	if logger == nil {
		panic("dispatcher: logger must not be nil")
	}
	return &Dispatcher{
		idempotency: idempotency,
		limiter:     limiter,
		processing:  processing,
		comparison:  comparison,
		logger:      logger.With("component", "dispatcher"),
	}
}

// DispatchProcessDocument runs the ingress pipeline for a process-document
// command: idempotency check, concurrency slot acquisition, handler dispatch,
// and idempotency completion marking.
//
// Always returns nil to prevent poison-pill requeue loops.
func (d *Dispatcher) DispatchProcessDocument(ctx context.Context, cmd model.ProcessDocumentCommand) error {
	return d.runPipeline(ctx, cmd.JobID, func() error {
		return d.processing.HandleProcessDocument(ctx, cmd)
	})
}

// DispatchCompareVersions runs the ingress pipeline for a compare-versions
// command: idempotency check, concurrency slot acquisition, handler dispatch,
// and idempotency completion marking.
//
// Always returns nil to prevent poison-pill requeue loops.
func (d *Dispatcher) DispatchCompareVersions(ctx context.Context, cmd model.CompareVersionsCommand) error {
	return d.runPipeline(ctx, cmd.JobID, func() error {
		return d.comparison.HandleCompareVersions(ctx, cmd)
	})
}

// runPipeline is the common ingress pipeline shared by both dispatch methods:
//  1. Idempotency guard — atomic Register (SetNX); duplicate jobs are skipped.
//  2. Concurrency limiter — Acquire blocks until a slot is available or ctx is done.
//     On failure, MarkCompleted is called to release the idempotency lock so the
//     job can be retried on a future delivery instead of being blocked for TTL.
//  3. Handler dispatch — orchestrator handles failure semantics internally.
//  4. Mark completed — best-effort; errors are logged but do not affect the result.
//
// Always returns nil.
func (d *Dispatcher) runPipeline(ctx context.Context, jobID string, handler func() error) error {
	// 1. Idempotency guard.
	if err := d.idempotency.Register(ctx, jobID); err != nil {
		if port.ErrorCode(err) == port.ErrCodeDuplicateJob {
			d.logger.Info(ctx, "duplicate job, skipping", "job_id", jobID)
			return nil
		}
		d.logger.Error(ctx, "idempotency register failed", "job_id", jobID, "error", err)
		return nil
	}

	// 2. Concurrency limiter.
	if err := d.limiter.Acquire(ctx); err != nil {
		d.logger.Warn(ctx, "concurrency slot unavailable", "job_id", jobID, "error", err)
		// Best-effort cleanup: mark completed so the job is not blocked for the
		// full idempotency TTL (24h). Future redelivery will be accepted.
		if cleanErr := d.idempotency.MarkCompleted(ctx, jobID); cleanErr != nil {
			d.logger.Error(ctx, "failed to clean up idempotency after acquire failure",
				"job_id", jobID, "error", cleanErr)
		}
		return nil
	}
	defer d.limiter.Release()

	// 3. Handler dispatch.
	if err := handler(); err != nil {
		d.logger.Warn(ctx, "handler returned error", "job_id", jobID, "error", err)
	}

	// 4. Mark completed (best-effort).
	if err := d.idempotency.MarkCompleted(ctx, jobID); err != nil {
		d.logger.Error(ctx, "idempotency mark completed failed", "job_id", jobID, "error", err)
	}

	return nil
}
