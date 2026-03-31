package lifecycle

import (
	"context"
	"errors"
	"testing"
	"time"

	"contractpro/document-processing/internal/domain/model"
	"contractpro/document-processing/internal/domain/port"
	"contractpro/document-processing/internal/infra/observability"
)

// --- Mocks ---

type publishedEvent struct {
	statusChanged        []model.StatusChangedEvent
	processingCompleted  []model.ProcessingCompletedEvent
	processingFailed     []model.ProcessingFailedEvent
	comparisonCompleted  []model.ComparisonCompletedEvent
	comparisonFailed     []model.ComparisonFailedEvent
}

type mockPublisher struct {
	events publishedEvent
	err    error // if set, all Publish* methods return this error
}

func (m *mockPublisher) PublishStatusChanged(ctx context.Context, event model.StatusChangedEvent) error {
	if m.err != nil {
		return m.err
	}
	m.events.statusChanged = append(m.events.statusChanged, event)
	return nil
}

func (m *mockPublisher) PublishProcessingCompleted(ctx context.Context, event model.ProcessingCompletedEvent) error {
	if m.err != nil {
		return m.err
	}
	m.events.processingCompleted = append(m.events.processingCompleted, event)
	return nil
}

func (m *mockPublisher) PublishProcessingFailed(ctx context.Context, event model.ProcessingFailedEvent) error {
	if m.err != nil {
		return m.err
	}
	m.events.processingFailed = append(m.events.processingFailed, event)
	return nil
}

func (m *mockPublisher) PublishComparisonCompleted(ctx context.Context, event model.ComparisonCompletedEvent) error {
	if m.err != nil {
		return m.err
	}
	m.events.comparisonCompleted = append(m.events.comparisonCompleted, event)
	return nil
}

func (m *mockPublisher) PublishComparisonFailed(ctx context.Context, event model.ComparisonFailedEvent) error {
	if m.err != nil {
		return m.err
	}
	m.events.comparisonFailed = append(m.events.comparisonFailed, event)
	return nil
}

type mockIdempotency struct {
	completed []string // job IDs passed to MarkCompleted
	err       error    // if set, MarkCompleted returns this error
}

func (m *mockIdempotency) Check(_ context.Context, _ string) (port.IdempotencyStatus, error) {
	return port.IdempotencyStatusNew, nil
}

func (m *mockIdempotency) Register(_ context.Context, _ string) error {
	return nil
}

func (m *mockIdempotency) MarkCompleted(_ context.Context, jobID string) error {
	if m.err != nil {
		return m.err
	}
	m.completed = append(m.completed, jobID)
	return nil
}

// --- Helpers ---

func nopLogger() *observability.Logger { return observability.NewLogger("error") }

func newProcessingJob() *model.ProcessingJob {
	return model.NewProcessingJob("job-1", "doc-1", "https://example.com/file.pdf")
}

func newComparisonJob() *model.ComparisonJob {
	return model.NewComparisonJob("job-2", "doc-2", "v1", "v2")
}

func newManager(pub *mockPublisher, idem *mockIdempotency, cleanup CleanupFunc) *LifecycleManager {
	return NewLifecycleManager(pub, idem, 120*time.Second, cleanup, nopLogger())
}

// --- Tests ---

func TestTransitionToInProgress(t *testing.T) {
	pub := &mockPublisher{}
	idem := &mockIdempotency{}
	cleanupCalled := false
	cleanup := func(_ context.Context, _ string) error {
		cleanupCalled = true
		return nil
	}
	mgr := newManager(pub, idem, cleanup)

	job := newProcessingJob()
	job.Stage = model.ProcessingStageValidatingInput

	err := mgr.TransitionJob(context.Background(), job, model.StatusInProgress)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if job.Status != model.StatusInProgress {
		t.Errorf("expected status IN_PROGRESS, got %s", job.Status)
	}
	if len(pub.events.statusChanged) != 1 {
		t.Fatalf("expected 1 StatusChangedEvent, got %d", len(pub.events.statusChanged))
	}
	evt := pub.events.statusChanged[0]
	if evt.OldStatus != model.StatusQueued {
		t.Errorf("expected old status QUEUED, got %s", evt.OldStatus)
	}
	if evt.NewStatus != model.StatusInProgress {
		t.Errorf("expected new status IN_PROGRESS, got %s", evt.NewStatus)
	}
	if cleanupCalled {
		t.Error("cleanup should not be called for non-terminal status")
	}
	if len(idem.completed) != 0 {
		t.Error("idempotency should not be marked for non-terminal status")
	}
}

func TestTransitionToCompleted(t *testing.T) {
	pub := &mockPublisher{}
	idem := &mockIdempotency{}
	var cleanupJobID string
	cleanup := func(_ context.Context, jobID string) error {
		cleanupJobID = jobID
		return nil
	}
	mgr := newManager(pub, idem, cleanup)

	job := newProcessingJob()
	// Move to IN_PROGRESS first.
	_ = mgr.TransitionJob(context.Background(), job, model.StatusInProgress)

	err := mgr.TransitionJob(context.Background(), job, model.StatusCompleted)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if job.Status != model.StatusCompleted {
		t.Errorf("expected status COMPLETED, got %s", job.Status)
	}
	if cleanupJobID != "job-1" {
		t.Errorf("expected cleanup called with job-1, got %q", cleanupJobID)
	}
	if len(idem.completed) != 1 || idem.completed[0] != "job-1" {
		t.Errorf("expected idempotency marked for job-1, got %v", idem.completed)
	}
}

func TestTransitionToFailed(t *testing.T) {
	pub := &mockPublisher{}
	idem := &mockIdempotency{}
	cleanupCalled := false
	cleanup := func(_ context.Context, _ string) error {
		cleanupCalled = true
		return nil
	}
	mgr := newManager(pub, idem, cleanup)

	job := newProcessingJob()
	_ = mgr.TransitionJob(context.Background(), job, model.StatusInProgress)

	err := mgr.TransitionJob(context.Background(), job, model.StatusFailed)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if job.Status != model.StatusFailed {
		t.Errorf("expected status FAILED, got %s", job.Status)
	}
	if !cleanupCalled {
		t.Error("cleanup should be called on terminal status FAILED")
	}
	if len(idem.completed) != 1 {
		t.Error("idempotency should be marked on terminal status FAILED")
	}
}

func TestTransitionToTimedOut(t *testing.T) {
	pub := &mockPublisher{}
	idem := &mockIdempotency{}
	cleanupCalled := false
	cleanup := func(_ context.Context, _ string) error {
		cleanupCalled = true
		return nil
	}
	mgr := newManager(pub, idem, cleanup)

	job := newProcessingJob()
	_ = mgr.TransitionJob(context.Background(), job, model.StatusInProgress)

	err := mgr.TransitionJob(context.Background(), job, model.StatusTimedOut)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if job.Status != model.StatusTimedOut {
		t.Errorf("expected status TIMED_OUT, got %s", job.Status)
	}
	if !cleanupCalled {
		t.Error("cleanup should be called on terminal status TIMED_OUT")
	}
	if len(idem.completed) != 1 {
		t.Error("idempotency should be marked on terminal status TIMED_OUT")
	}
}

func TestTransitionToRejected(t *testing.T) {
	pub := &mockPublisher{}
	idem := &mockIdempotency{}
	cleanupCalled := false
	cleanup := func(_ context.Context, _ string) error {
		cleanupCalled = true
		return nil
	}
	mgr := newManager(pub, idem, cleanup)

	job := newProcessingJob()

	err := mgr.TransitionJob(context.Background(), job, model.StatusRejected)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if job.Status != model.StatusRejected {
		t.Errorf("expected status REJECTED, got %s", job.Status)
	}
	if !cleanupCalled {
		t.Error("cleanup should be called on terminal status REJECTED")
	}
	if len(idem.completed) != 1 {
		t.Error("idempotency should be marked on terminal status REJECTED")
	}
}

func TestInvalidTransition(t *testing.T) {
	pub := &mockPublisher{}
	idem := &mockIdempotency{}
	mgr := newManager(pub, idem, nil)

	job := newProcessingJob()
	_ = mgr.TransitionJob(context.Background(), job, model.StatusInProgress)
	_ = mgr.TransitionJob(context.Background(), job, model.StatusCompleted)

	// COMPLETED is terminal — transition should fail.
	err := mgr.TransitionJob(context.Background(), job, model.StatusInProgress)
	if err == nil {
		t.Fatal("expected error for invalid transition from COMPLETED to IN_PROGRESS")
	}

	// No new event should be published for the invalid transition.
	// We expect 2 events from the setup (QUEUED→IN_PROGRESS, IN_PROGRESS→COMPLETED).
	if len(pub.events.statusChanged) != 2 {
		t.Errorf("expected 2 events (from setup), got %d", len(pub.events.statusChanged))
	}
}

func TestCleanupErrorIgnored(t *testing.T) {
	pub := &mockPublisher{}
	idem := &mockIdempotency{}
	cleanup := func(_ context.Context, _ string) error {
		return errors.New("cleanup failed")
	}
	mgr := newManager(pub, idem, cleanup)

	job := newProcessingJob()
	_ = mgr.TransitionJob(context.Background(), job, model.StatusInProgress)

	// Cleanup error should be logged but not returned.
	err := mgr.TransitionJob(context.Background(), job, model.StatusCompleted)
	if err != nil {
		t.Fatalf("cleanup error should not propagate, got: %v", err)
	}

	if job.Status != model.StatusCompleted {
		t.Errorf("expected COMPLETED, got %s", job.Status)
	}
	if len(idem.completed) != 1 {
		t.Error("idempotency should still be marked despite cleanup error")
	}
}

func TestNilCleanup(t *testing.T) {
	pub := &mockPublisher{}
	idem := &mockIdempotency{}
	mgr := newManager(pub, idem, nil)

	job := newProcessingJob()
	_ = mgr.TransitionJob(context.Background(), job, model.StatusInProgress)

	err := mgr.TransitionJob(context.Background(), job, model.StatusCompleted)
	if err != nil {
		t.Fatalf("unexpected error with nil cleanup: %v", err)
	}

	if job.Status != model.StatusCompleted {
		t.Errorf("expected COMPLETED, got %s", job.Status)
	}
}

func TestPublishError(t *testing.T) {
	pubErr := errors.New("broker unavailable")
	pub := &mockPublisher{err: pubErr}
	idem := &mockIdempotency{}
	cleanupCalled := false
	cleanup := func(_ context.Context, _ string) error {
		cleanupCalled = true
		return nil
	}
	mgr := newManager(pub, idem, cleanup)

	job := newProcessingJob()

	err := mgr.TransitionJob(context.Background(), job, model.StatusInProgress)
	if !errors.Is(err, pubErr) {
		t.Fatalf("expected publish error, got: %v", err)
	}

	// Transition already happened on the object.
	if job.Status != model.StatusInProgress {
		t.Errorf("status should be IN_PROGRESS (transition happened), got %s", job.Status)
	}
	// Cleanup should NOT be called because publish failed before terminal check.
	if cleanupCalled {
		t.Error("cleanup should not be called when publish fails")
	}
}

func TestIdempotencyError(t *testing.T) {
	pub := &mockPublisher{}
	idemErr := errors.New("kv-store unavailable")
	idem := &mockIdempotency{err: idemErr}
	mgr := newManager(pub, idem, nil)

	job := newProcessingJob()
	_ = mgr.TransitionJob(context.Background(), job, model.StatusInProgress)

	err := mgr.TransitionJob(context.Background(), job, model.StatusCompleted)
	if !errors.Is(err, idemErr) {
		t.Fatalf("expected idempotency error, got: %v", err)
	}
}

func TestNewJobContext(t *testing.T) {
	pub := &mockPublisher{}
	idem := &mockIdempotency{}
	mgr := NewLifecycleManager(pub, idem, 50*time.Millisecond, nil, nopLogger())

	ctx, cancel := mgr.NewJobContext(context.Background())
	defer cancel()

	select {
	case <-ctx.Done():
		if !errors.Is(ctx.Err(), context.DeadlineExceeded) {
			t.Errorf("expected DeadlineExceeded, got %v", ctx.Err())
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("context should have expired within 50ms")
	}
}

func TestEventFieldsCorrect(t *testing.T) {
	pub := &mockPublisher{}
	idem := &mockIdempotency{}
	mgr := newManager(pub, idem, nil)

	job := newProcessingJob()
	job.Stage = model.ProcessingStageFetchingSourceFile

	before := time.Now().UTC()
	err := mgr.TransitionJob(context.Background(), job, model.StatusInProgress)
	after := time.Now().UTC()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	evt := pub.events.statusChanged[0]
	if evt.JobID != "job-1" {
		t.Errorf("expected JobID job-1, got %s", evt.JobID)
	}
	if evt.DocumentID != "doc-1" {
		t.Errorf("expected DocumentID doc-1, got %s", evt.DocumentID)
	}
	if evt.OldStatus != model.StatusQueued {
		t.Errorf("expected OldStatus QUEUED, got %s", evt.OldStatus)
	}
	if evt.NewStatus != model.StatusInProgress {
		t.Errorf("expected NewStatus IN_PROGRESS, got %s", evt.NewStatus)
	}
	if evt.Stage != string(model.ProcessingStageFetchingSourceFile) {
		t.Errorf("expected Stage FETCHING_SOURCE_FILE, got %s", evt.Stage)
	}
	if evt.CorrelationID != "job-1" {
		t.Errorf("expected CorrelationID job-1, got %s", evt.CorrelationID)
	}
	if evt.Timestamp.Before(before) || evt.Timestamp.After(after) {
		t.Errorf("timestamp %v outside expected range [%v, %v]", evt.Timestamp, before, after)
	}
}

func TestComparisonJob(t *testing.T) {
	pub := &mockPublisher{}
	idem := &mockIdempotency{}
	cleanupCalled := false
	cleanup := func(_ context.Context, _ string) error {
		cleanupCalled = true
		return nil
	}
	mgr := newManager(pub, idem, cleanup)

	job := newComparisonJob()
	job.Stage = model.ComparisonStageRequestingTrees

	err := mgr.TransitionJob(context.Background(), job, model.StatusInProgress)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	evt := pub.events.statusChanged[0]
	if evt.JobID != "job-2" {
		t.Errorf("expected JobID job-2, got %s", evt.JobID)
	}
	if evt.DocumentID != "doc-2" {
		t.Errorf("expected DocumentID doc-2, got %s", evt.DocumentID)
	}
	if evt.Stage != string(model.ComparisonStageRequestingTrees) {
		t.Errorf("expected Stage REQUESTING_SEMANTIC_TREES, got %s", evt.Stage)
	}

	err = mgr.TransitionJob(context.Background(), job, model.StatusCompleted)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !cleanupCalled {
		t.Error("cleanup should be called on terminal status for ComparisonJob")
	}
	if len(idem.completed) != 1 || idem.completed[0] != "job-2" {
		t.Errorf("expected idempotency marked for job-2, got %v", idem.completed)
	}
}

func TestOrgID_PropagatedInStatusChangedEvent(t *testing.T) {
	t.Run("ProcessingJob", func(t *testing.T) {
		pub := &mockPublisher{}
		idem := &mockIdempotency{}
		mgr := newManager(pub, idem, nil)

		job := newProcessingJob()
		job.OrgID = "org-alpha-1"
		job.Stage = model.ProcessingStageValidatingInput

		err := mgr.TransitionJob(context.Background(), job, model.StatusInProgress)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		evt := pub.events.statusChanged[0]
		if evt.OrgID != "org-alpha-1" {
			t.Errorf("OrgID = %q, want %q", evt.OrgID, "org-alpha-1")
		}
	})

	t.Run("ComparisonJob", func(t *testing.T) {
		pub := &mockPublisher{}
		idem := &mockIdempotency{}
		mgr := newManager(pub, idem, nil)

		job := newComparisonJob()
		job.OrgID = "org-beta-2"
		job.Stage = model.ComparisonStageRequestingTrees

		err := mgr.TransitionJob(context.Background(), job, model.StatusInProgress)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		evt := pub.events.statusChanged[0]
		if evt.OrgID != "org-beta-2" {
			t.Errorf("OrgID = %q, want %q", evt.OrgID, "org-beta-2")
		}
	})

	t.Run("EmptyOrgID", func(t *testing.T) {
		pub := &mockPublisher{}
		idem := &mockIdempotency{}
		mgr := newManager(pub, idem, nil)

		job := newProcessingJob()
		// OrgID intentionally not set — backward compatibility.

		err := mgr.TransitionJob(context.Background(), job, model.StatusInProgress)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		evt := pub.events.statusChanged[0]
		if evt.OrgID != "" {
			t.Errorf("OrgID = %q, want empty (backward compat)", evt.OrgID)
		}
	})
}
