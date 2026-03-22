package lifecycle

import (
	"context"
	"log"
	"time"

	"contractpro/document-processing/internal/domain/model"
	"contractpro/document-processing/internal/domain/port"
)

// ManagedJob is implemented by ProcessingJob and ComparisonJob.
// It provides the LifecycleManager access to job metadata without
// knowing the concrete job type.
type ManagedJob interface {
	GetJobMeta() *model.JobMeta
	GetDocumentID() string
	GetStage() string
}

// CleanupFunc is called on terminal status transitions.
// It receives the jobID so a single function can serve all jobs.
type CleanupFunc func(ctx context.Context, jobID string) error

// LifecycleManager handles the side effects of every job status transition:
//   - publishes StatusChangedEvent via EventPublisherPort
//   - calls cleanup on terminal statuses
//   - marks idempotency as completed on terminal statuses
type LifecycleManager struct {
	publisher   port.EventPublisherPort
	idempotency port.IdempotencyStorePort
	jobTimeout  time.Duration
	cleanup     CleanupFunc
}

// NewLifecycleManager creates a LifecycleManager.
// Panics if publisher or idempotency is nil. cleanup may be nil if no cleanup is needed.
func NewLifecycleManager(
	publisher port.EventPublisherPort,
	idempotency port.IdempotencyStorePort,
	jobTimeout time.Duration,
	cleanup CleanupFunc,
) *LifecycleManager {
	if publisher == nil {
		panic("lifecycle: publisher must not be nil")
	}
	if idempotency == nil {
		panic("lifecycle: idempotency store must not be nil")
	}
	return &LifecycleManager{
		publisher:   publisher,
		idempotency: idempotency,
		jobTimeout:  jobTimeout,
		cleanup:     cleanup,
	}
}

// TransitionJob validates and performs a status transition, then:
//  1. Publishes a StatusChangedEvent.
//  2. On terminal status: runs cleanup (best-effort) and marks idempotency as completed.
func (m *LifecycleManager) TransitionJob(ctx context.Context, job ManagedJob, newStatus model.JobStatus) error {
	meta := job.GetJobMeta()
	oldStatus := meta.Status

	if err := meta.TransitionTo(newStatus); err != nil {
		return err
	}

	event := model.StatusChangedEvent{
		EventMeta: model.EventMeta{
			CorrelationID: meta.JobID,
			Timestamp:     time.Now().UTC(),
		},
		JobID:      meta.JobID,
		DocumentID: job.GetDocumentID(),
		OldStatus:  oldStatus,
		NewStatus:  newStatus,
		Stage:      job.GetStage(),
	}

	if err := m.publisher.PublishStatusChanged(ctx, event); err != nil {
		return err
	}

	if newStatus.IsTerminal() {
		if m.cleanup != nil {
			if err := m.cleanup(ctx, meta.JobID); err != nil {
				log.Printf("lifecycle: cleanup error for job %s: %v", meta.JobID, err)
			}
		}
		if err := m.idempotency.MarkCompleted(ctx, meta.JobID); err != nil {
			return err
		}
	}

	return nil
}

// NewJobContext creates a child context with the configured job timeout.
func (m *LifecycleManager) NewJobContext(parent context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(parent, m.jobTimeout)
}
