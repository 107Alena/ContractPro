package dispatcher

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"contractpro/document-processing/internal/domain/model"
	"contractpro/document-processing/internal/domain/port"
	"contractpro/document-processing/internal/ingress/consumer"
	"contractpro/document-processing/internal/infra/observability"
)

// Compile-time check: Dispatcher satisfies consumer.CommandDispatcher (W-1).
var _ consumer.CommandDispatcher = (*Dispatcher)(nil)

// --- mocks ---

type mockIdempotency struct {
	registerErr      error
	markCompletedErr error
	registerCalled   bool
	completedCalled  bool
	lastJobID        string
}

func (m *mockIdempotency) Check(_ context.Context, _ string) (port.IdempotencyStatus, error) {
	return port.IdempotencyStatusNew, nil
}

func (m *mockIdempotency) Register(_ context.Context, jobID string) error {
	m.registerCalled = true
	m.lastJobID = jobID
	return m.registerErr
}

func (m *mockIdempotency) MarkCompleted(_ context.Context, jobID string) error {
	m.completedCalled = true
	m.lastJobID = jobID
	return m.markCompletedErr
}

var _ port.IdempotencyStorePort = (*mockIdempotency)(nil)

type mockLimiter struct {
	mu         sync.Mutex
	acquireErr error
	acquired   bool
	released   bool
}

func (m *mockLimiter) Acquire(_ context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.acquireErr != nil {
		return m.acquireErr
	}
	m.acquired = true
	return nil
}

func (m *mockLimiter) Release() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.released = true
}

var _ port.ConcurrencyLimiterPort = (*mockLimiter)(nil)

type mockProcessingHandler struct {
	mu      sync.Mutex
	called  bool
	lastCmd model.ProcessDocumentCommand
	err     error
}

func (m *mockProcessingHandler) HandleProcessDocument(_ context.Context, cmd model.ProcessDocumentCommand) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.called = true
	m.lastCmd = cmd
	return m.err
}

var _ port.ProcessingCommandHandler = (*mockProcessingHandler)(nil)

type mockComparisonHandler struct {
	mu      sync.Mutex
	called  bool
	lastCmd model.CompareVersionsCommand
	err     error
}

func (m *mockComparisonHandler) HandleCompareVersions(_ context.Context, cmd model.CompareVersionsCommand) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.called = true
	m.lastCmd = cmd
	return m.err
}

var _ port.ComparisonCommandHandler = (*mockComparisonHandler)(nil)

// --- helpers ---

func testLogger() *observability.Logger {
	return observability.NewLogger("error") // suppress info/warn/debug in tests
}

func validProcessCmd() model.ProcessDocumentCommand {
	return model.ProcessDocumentCommand{
		JobID:      "job-123",
		DocumentID: "doc-456",
		FileURL:    "https://storage.example.com/files/contract.pdf",
		FileName:   "contract.pdf",
		FileSize:   1024,
		MimeType:   "application/pdf",
	}
}

func validCompareCmd() model.CompareVersionsCommand {
	return model.CompareVersionsCommand{
		JobID:           "job-789",
		DocumentID:      "doc-456",
		BaseVersionID:   "v1",
		TargetVersionID: "v2",
	}
}

func newDispatcher(
	idem *mockIdempotency,
	lim *mockLimiter,
	proc *mockProcessingHandler,
	comp *mockComparisonHandler,
) *Dispatcher {
	return NewDispatcher(idem, lim, proc, comp, testLogger())
}

// --- Constructor panic tests ---

func TestNewDispatcher_PanicsOnNilIdempotency(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for nil idempotency")
		}
	}()
	NewDispatcher(nil, &mockLimiter{}, &mockProcessingHandler{}, &mockComparisonHandler{}, testLogger())
}

func TestNewDispatcher_PanicsOnNilLimiter(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for nil limiter")
		}
	}()
	NewDispatcher(&mockIdempotency{}, nil, &mockProcessingHandler{}, &mockComparisonHandler{}, testLogger())
}

func TestNewDispatcher_PanicsOnNilProcessing(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for nil processing handler")
		}
	}()
	NewDispatcher(&mockIdempotency{}, &mockLimiter{}, nil, &mockComparisonHandler{}, testLogger())
}

func TestNewDispatcher_PanicsOnNilComparison(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for nil comparison handler")
		}
	}()
	NewDispatcher(&mockIdempotency{}, &mockLimiter{}, &mockProcessingHandler{}, nil, testLogger())
}

func TestNewDispatcher_PanicsOnNilLogger(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for nil logger")
		}
	}()
	NewDispatcher(&mockIdempotency{}, &mockLimiter{}, &mockProcessingHandler{}, &mockComparisonHandler{}, nil)
}

// --- DispatchProcessDocument tests ---

func TestDispatchProcessDocument_HappyPath(t *testing.T) {
	idem := &mockIdempotency{}
	lim := &mockLimiter{}
	proc := &mockProcessingHandler{}
	comp := &mockComparisonHandler{}
	d := newDispatcher(idem, lim, proc, comp)

	cmd := validProcessCmd()
	err := d.DispatchProcessDocument(context.Background(), cmd)
	if err != nil {
		t.Fatalf("expected nil, got: %v", err)
	}

	if !idem.registerCalled {
		t.Error("Register was not called")
	}
	if idem.lastJobID != cmd.JobID {
		t.Errorf("Register job_id = %q, want %q", idem.lastJobID, cmd.JobID)
	}
	if !lim.acquired {
		t.Error("Acquire was not called")
	}
	if !proc.called {
		t.Error("processing handler was not called")
	}
	if proc.lastCmd.JobID != cmd.JobID {
		t.Errorf("handler received job_id = %q, want %q", proc.lastCmd.JobID, cmd.JobID)
	}
	if !idem.completedCalled {
		t.Error("MarkCompleted was not called")
	}
	if !lim.released {
		t.Error("Release was not called")
	}
}

func TestDispatchProcessDocument_DuplicateJob(t *testing.T) {
	idem := &mockIdempotency{registerErr: port.NewDuplicateJobError("job-123")}
	lim := &mockLimiter{}
	proc := &mockProcessingHandler{}
	d := newDispatcher(idem, lim, proc, &mockComparisonHandler{})

	err := d.DispatchProcessDocument(context.Background(), validProcessCmd())
	if err != nil {
		t.Fatalf("expected nil, got: %v", err)
	}

	if proc.called {
		t.Error("handler should NOT be called for duplicate job")
	}
	if lim.acquired {
		t.Error("Acquire should NOT be called for duplicate job")
	}
	if lim.released {
		t.Error("Release should NOT be called for duplicate job")
	}
}

func TestDispatchProcessDocument_RegisterStorageError(t *testing.T) {
	idem := &mockIdempotency{registerErr: errors.New("redis connection refused")}
	lim := &mockLimiter{}
	proc := &mockProcessingHandler{}
	d := newDispatcher(idem, lim, proc, &mockComparisonHandler{})

	err := d.DispatchProcessDocument(context.Background(), validProcessCmd())
	if err != nil {
		t.Fatalf("expected nil, got: %v", err)
	}

	if proc.called {
		t.Error("handler should NOT be called on register error")
	}
	if lim.acquired {
		t.Error("Acquire should NOT be called on register error")
	}
}

func TestDispatchProcessDocument_AcquireTimeout(t *testing.T) {
	idem := &mockIdempotency{}
	lim := &mockLimiter{acquireErr: context.DeadlineExceeded}
	proc := &mockProcessingHandler{}
	d := newDispatcher(idem, lim, proc, &mockComparisonHandler{})

	err := d.DispatchProcessDocument(context.Background(), validProcessCmd())
	if err != nil {
		t.Fatalf("expected nil, got: %v", err)
	}

	if proc.called {
		t.Error("handler should NOT be called when Acquire fails")
	}
	if lim.released {
		t.Error("Release should NOT be called when Acquire fails")
	}
	// C-1 fix: MarkCompleted should be called to release idempotency lock
	if !idem.completedCalled {
		t.Error("MarkCompleted should be called on Acquire failure to release idempotency lock")
	}
}

func TestDispatchProcessDocument_AcquireCancelled(t *testing.T) {
	idem := &mockIdempotency{}
	lim := &mockLimiter{acquireErr: context.Canceled}
	proc := &mockProcessingHandler{}
	d := newDispatcher(idem, lim, proc, &mockComparisonHandler{})

	err := d.DispatchProcessDocument(context.Background(), validProcessCmd())
	if err != nil {
		t.Fatalf("expected nil, got: %v", err)
	}

	if proc.called {
		t.Error("handler should NOT be called when Acquire is cancelled")
	}
	if lim.released {
		t.Error("Release should NOT be called when Acquire is cancelled")
	}
	// C-1 fix: MarkCompleted should be called to release idempotency lock
	if !idem.completedCalled {
		t.Error("MarkCompleted should be called on Acquire cancellation to release idempotency lock")
	}
}

func TestDispatchProcessDocument_HandlerError_StillCompletesAndReleases(t *testing.T) {
	idem := &mockIdempotency{}
	lim := &mockLimiter{}
	proc := &mockProcessingHandler{err: errors.New("handler exploded")}
	d := newDispatcher(idem, lim, proc, &mockComparisonHandler{})

	err := d.DispatchProcessDocument(context.Background(), validProcessCmd())
	if err != nil {
		t.Fatalf("expected nil, got: %v", err)
	}

	if !proc.called {
		t.Error("handler should have been called")
	}
	if !idem.completedCalled {
		t.Error("MarkCompleted should still be called after handler error")
	}
	if !lim.released {
		t.Error("Release should still be called after handler error")
	}
}

func TestDispatchProcessDocument_MarkCompletedError_NonFatal(t *testing.T) {
	idem := &mockIdempotency{markCompletedErr: errors.New("redis write failed")}
	lim := &mockLimiter{}
	proc := &mockProcessingHandler{}
	d := newDispatcher(idem, lim, proc, &mockComparisonHandler{})

	err := d.DispatchProcessDocument(context.Background(), validProcessCmd())
	if err != nil {
		t.Fatalf("expected nil (non-fatal), got: %v", err)
	}

	if !proc.called {
		t.Error("handler should have been called")
	}
	if !idem.completedCalled {
		t.Error("MarkCompleted should have been attempted")
	}
}

func TestDispatchProcessDocument_AllFieldsPassedThrough(t *testing.T) {
	proc := &mockProcessingHandler{}
	d := newDispatcher(&mockIdempotency{}, &mockLimiter{}, proc, &mockComparisonHandler{})

	cmd := model.ProcessDocumentCommand{
		JobID:      "j1",
		DocumentID: "d1",
		FileURL:    "https://example.com/f.pdf",
		OrgID:      "org-1",
		UserID:     "user-1",
		FileName:   "contract.pdf",
		FileSize:   2048,
		MimeType:   "application/pdf",
		Checksum:   "sha256:abc",
	}

	_ = d.DispatchProcessDocument(context.Background(), cmd)

	got := proc.lastCmd
	if got.JobID != cmd.JobID {
		t.Errorf("JobID = %q, want %q", got.JobID, cmd.JobID)
	}
	if got.DocumentID != cmd.DocumentID {
		t.Errorf("DocumentID = %q, want %q", got.DocumentID, cmd.DocumentID)
	}
	if got.FileURL != cmd.FileURL {
		t.Errorf("FileURL = %q, want %q", got.FileURL, cmd.FileURL)
	}
	if got.OrgID != cmd.OrgID {
		t.Errorf("OrgID = %q, want %q", got.OrgID, cmd.OrgID)
	}
	if got.UserID != cmd.UserID {
		t.Errorf("UserID = %q, want %q", got.UserID, cmd.UserID)
	}
	if got.FileName != cmd.FileName {
		t.Errorf("FileName = %q, want %q", got.FileName, cmd.FileName)
	}
	if got.FileSize != cmd.FileSize {
		t.Errorf("FileSize = %d, want %d", got.FileSize, cmd.FileSize)
	}
	if got.MimeType != cmd.MimeType {
		t.Errorf("MimeType = %q, want %q", got.MimeType, cmd.MimeType)
	}
	if got.Checksum != cmd.Checksum {
		t.Errorf("Checksum = %q, want %q", got.Checksum, cmd.Checksum)
	}
}

// --- DispatchCompareVersions tests ---

func TestDispatchCompareVersions_HappyPath(t *testing.T) {
	idem := &mockIdempotency{}
	lim := &mockLimiter{}
	comp := &mockComparisonHandler{}
	d := newDispatcher(idem, lim, &mockProcessingHandler{}, comp)

	cmd := validCompareCmd()
	err := d.DispatchCompareVersions(context.Background(), cmd)
	if err != nil {
		t.Fatalf("expected nil, got: %v", err)
	}

	if !idem.registerCalled {
		t.Error("Register was not called")
	}
	if !lim.acquired {
		t.Error("Acquire was not called")
	}
	if !comp.called {
		t.Error("comparison handler was not called")
	}
	if comp.lastCmd.JobID != cmd.JobID {
		t.Errorf("handler received job_id = %q, want %q", comp.lastCmd.JobID, cmd.JobID)
	}
	if comp.lastCmd.BaseVersionID != cmd.BaseVersionID {
		t.Errorf("handler received base_version_id = %q, want %q", comp.lastCmd.BaseVersionID, cmd.BaseVersionID)
	}
	if comp.lastCmd.TargetVersionID != cmd.TargetVersionID {
		t.Errorf("handler received target_version_id = %q, want %q", comp.lastCmd.TargetVersionID, cmd.TargetVersionID)
	}
	if !idem.completedCalled {
		t.Error("MarkCompleted was not called")
	}
	if !lim.released {
		t.Error("Release was not called")
	}
}

func TestDispatchCompareVersions_DuplicateJob(t *testing.T) {
	idem := &mockIdempotency{registerErr: port.NewDuplicateJobError("job-789")}
	lim := &mockLimiter{}
	comp := &mockComparisonHandler{}
	d := newDispatcher(idem, lim, &mockProcessingHandler{}, comp)

	err := d.DispatchCompareVersions(context.Background(), validCompareCmd())
	if err != nil {
		t.Fatalf("expected nil, got: %v", err)
	}

	if comp.called {
		t.Error("handler should NOT be called for duplicate job")
	}
	if lim.acquired {
		t.Error("Acquire should NOT be called for duplicate job")
	}
}

func TestDispatchCompareVersions_RegisterStorageError(t *testing.T) {
	idem := &mockIdempotency{registerErr: errors.New("redis connection refused")}
	comp := &mockComparisonHandler{}
	d := newDispatcher(idem, &mockLimiter{}, &mockProcessingHandler{}, comp)

	err := d.DispatchCompareVersions(context.Background(), validCompareCmd())
	if err != nil {
		t.Fatalf("expected nil, got: %v", err)
	}

	if comp.called {
		t.Error("handler should NOT be called on register error")
	}
}

func TestDispatchCompareVersions_AcquireTimeout(t *testing.T) {
	idem := &mockIdempotency{}
	lim := &mockLimiter{acquireErr: context.DeadlineExceeded}
	comp := &mockComparisonHandler{}
	d := newDispatcher(idem, lim, &mockProcessingHandler{}, comp)

	err := d.DispatchCompareVersions(context.Background(), validCompareCmd())
	if err != nil {
		t.Fatalf("expected nil, got: %v", err)
	}

	if comp.called {
		t.Error("handler should NOT be called when Acquire fails")
	}
	if lim.released {
		t.Error("Release should NOT be called when Acquire fails")
	}
	// C-1 fix: MarkCompleted should be called to release idempotency lock
	if !idem.completedCalled {
		t.Error("MarkCompleted should be called on Acquire failure to release idempotency lock")
	}
}

func TestDispatchCompareVersions_HandlerError_StillCompletesAndReleases(t *testing.T) {
	idem := &mockIdempotency{}
	lim := &mockLimiter{}
	comp := &mockComparisonHandler{err: errors.New("comparison exploded")}
	d := newDispatcher(idem, lim, &mockProcessingHandler{}, comp)

	err := d.DispatchCompareVersions(context.Background(), validCompareCmd())
	if err != nil {
		t.Fatalf("expected nil, got: %v", err)
	}

	if !comp.called {
		t.Error("handler should have been called")
	}
	if !idem.completedCalled {
		t.Error("MarkCompleted should still be called after handler error")
	}
	if !lim.released {
		t.Error("Release should still be called after handler error")
	}
}

func TestDispatchCompareVersions_MarkCompletedError_NonFatal(t *testing.T) {
	idem := &mockIdempotency{markCompletedErr: errors.New("redis write failed")}
	comp := &mockComparisonHandler{}
	d := newDispatcher(idem, &mockLimiter{}, &mockProcessingHandler{}, comp)

	err := d.DispatchCompareVersions(context.Background(), validCompareCmd())
	if err != nil {
		t.Fatalf("expected nil (non-fatal), got: %v", err)
	}

	if !comp.called {
		t.Error("handler should have been called")
	}
}

func TestDispatchCompareVersions_AllFieldsPassedThrough(t *testing.T) {
	comp := &mockComparisonHandler{}
	d := newDispatcher(&mockIdempotency{}, &mockLimiter{}, &mockProcessingHandler{}, comp)

	cmd := model.CompareVersionsCommand{
		JobID:           "j1",
		DocumentID:      "d1",
		BaseVersionID:   "v1",
		TargetVersionID: "v2",
		OrgID:           "org-1",
		UserID:          "user-1",
	}

	_ = d.DispatchCompareVersions(context.Background(), cmd)

	got := comp.lastCmd
	if got.JobID != cmd.JobID {
		t.Errorf("JobID = %q, want %q", got.JobID, cmd.JobID)
	}
	if got.DocumentID != cmd.DocumentID {
		t.Errorf("DocumentID = %q, want %q", got.DocumentID, cmd.DocumentID)
	}
	if got.BaseVersionID != cmd.BaseVersionID {
		t.Errorf("BaseVersionID = %q, want %q", got.BaseVersionID, cmd.BaseVersionID)
	}
	if got.TargetVersionID != cmd.TargetVersionID {
		t.Errorf("TargetVersionID = %q, want %q", got.TargetVersionID, cmd.TargetVersionID)
	}
	if got.OrgID != cmd.OrgID {
		t.Errorf("OrgID = %q, want %q", got.OrgID, cmd.OrgID)
	}
	if got.UserID != cmd.UserID {
		t.Errorf("UserID = %q, want %q", got.UserID, cmd.UserID)
	}
}

// --- Release behavior ---

func TestDispatchProcessDocument_ReleaseCalledOnSuccess(t *testing.T) {
	lim := &mockLimiter{}
	d := newDispatcher(&mockIdempotency{}, lim, &mockProcessingHandler{}, &mockComparisonHandler{})

	_ = d.DispatchProcessDocument(context.Background(), validProcessCmd())

	if !lim.released {
		t.Error("Release should be called on success")
	}
}

func TestDispatchProcessDocument_ReleaseNotCalledOnAcquireFailure(t *testing.T) {
	lim := &mockLimiter{acquireErr: context.Canceled}
	d := newDispatcher(&mockIdempotency{}, lim, &mockProcessingHandler{}, &mockComparisonHandler{})

	_ = d.DispatchProcessDocument(context.Background(), validProcessCmd())

	if lim.released {
		t.Error("Release should NOT be called when Acquire fails")
	}
}

func TestDispatchCompareVersions_ReleaseCalledOnSuccess(t *testing.T) {
	lim := &mockLimiter{}
	d := newDispatcher(&mockIdempotency{}, lim, &mockProcessingHandler{}, &mockComparisonHandler{})

	_ = d.DispatchCompareVersions(context.Background(), validCompareCmd())

	if !lim.released {
		t.Error("Release should be called on success")
	}
}

func TestDispatchCompareVersions_ReleaseNotCalledOnAcquireFailure(t *testing.T) {
	lim := &mockLimiter{acquireErr: context.DeadlineExceeded}
	d := newDispatcher(&mockIdempotency{}, lim, &mockProcessingHandler{}, &mockComparisonHandler{})

	_ = d.DispatchCompareVersions(context.Background(), validCompareCmd())

	if lim.released {
		t.Error("Release should NOT be called when Acquire fails")
	}
}

// --- Acquire failure cleanup error (C-1) ---

func TestDispatchProcessDocument_AcquireFailure_CleanupError_NonFatal(t *testing.T) {
	idem := &mockIdempotency{markCompletedErr: errors.New("redis write failed")}
	lim := &mockLimiter{acquireErr: context.DeadlineExceeded}
	proc := &mockProcessingHandler{}
	d := newDispatcher(idem, lim, proc, &mockComparisonHandler{})

	err := d.DispatchProcessDocument(context.Background(), validProcessCmd())
	if err != nil {
		t.Fatalf("expected nil (cleanup error is non-fatal), got: %v", err)
	}

	if proc.called {
		t.Error("handler should NOT be called")
	}
	if !idem.completedCalled {
		t.Error("MarkCompleted cleanup should have been attempted")
	}
}

// --- Context propagation (S-1) ---

type ctxCapturingIdempotency struct {
	mockIdempotency
	mu          sync.Mutex
	registerCtx context.Context
}

func (m *ctxCapturingIdempotency) Register(ctx context.Context, jobID string) error {
	m.mu.Lock()
	m.registerCtx = ctx
	m.mu.Unlock()
	return m.mockIdempotency.Register(ctx, jobID)
}

type ctxCapturingLimiter struct {
	mockLimiter
	mu         sync.Mutex
	acquireCtx context.Context
}

func (m *ctxCapturingLimiter) Acquire(ctx context.Context) error {
	m.mu.Lock()
	m.acquireCtx = ctx
	m.mu.Unlock()
	return m.mockLimiter.Acquire(ctx)
}

type ctxCapturingProcessing struct {
	mockProcessingHandler
	mu         sync.Mutex
	handlerCtx context.Context
}

func (m *ctxCapturingProcessing) HandleProcessDocument(ctx context.Context, cmd model.ProcessDocumentCommand) error {
	m.mu.Lock()
	m.handlerCtx = ctx
	m.mu.Unlock()
	return m.mockProcessingHandler.HandleProcessDocument(ctx, cmd)
}

func TestDispatchProcessDocument_ContextPropagation(t *testing.T) {
	type ctxKey struct{}
	ctx := context.WithValue(context.Background(), ctxKey{}, "test-value")

	idem := &ctxCapturingIdempotency{}
	lim := &ctxCapturingLimiter{}
	proc := &ctxCapturingProcessing{}
	d := NewDispatcher(idem, lim, proc, &mockComparisonHandler{}, testLogger())

	_ = d.DispatchProcessDocument(ctx, validProcessCmd())

	idem.mu.Lock()
	regCtx := idem.registerCtx
	idem.mu.Unlock()
	if regCtx.Value(ctxKey{}) != "test-value" {
		t.Error("Register did not receive the original context")
	}

	lim.mu.Lock()
	acqCtx := lim.acquireCtx
	lim.mu.Unlock()
	if acqCtx.Value(ctxKey{}) != "test-value" {
		t.Error("Acquire did not receive the original context")
	}

	proc.mu.Lock()
	hCtx := proc.handlerCtx
	proc.mu.Unlock()
	if hCtx.Value(ctxKey{}) != "test-value" {
		t.Error("Handler did not receive the original context")
	}
}

// --- Concurrent dispatch (S-2) ---

func TestDispatchProcessDocument_ConcurrentSafety(t *testing.T) {
	var registerCount atomic.Int32
	var completedCount atomic.Int32

	idem := &atomicCountingIdempotency{registerCount: &registerCount, completedCount: &completedCount}
	lim := &mockLimiter{}
	proc := &mockProcessingHandler{}
	d := NewDispatcher(idem, lim, proc, &mockComparisonHandler{}, testLogger())

	const goroutines = 50
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := range goroutines {
		go func(idx int) {
			defer wg.Done()
			cmd := validProcessCmd()
			cmd.JobID = "job-" + string(rune('A'+idx%26))
			_ = d.DispatchProcessDocument(context.Background(), cmd)
		}(i)
	}
	wg.Wait()

	if registerCount.Load() != goroutines {
		t.Errorf("expected %d Register calls, got %d", goroutines, registerCount.Load())
	}
	if completedCount.Load() != goroutines {
		t.Errorf("expected %d MarkCompleted calls, got %d", goroutines, completedCount.Load())
	}
}

type atomicCountingIdempotency struct {
	registerCount  *atomic.Int32
	completedCount *atomic.Int32
}

func (m *atomicCountingIdempotency) Check(_ context.Context, _ string) (port.IdempotencyStatus, error) {
	return port.IdempotencyStatusNew, nil
}

func (m *atomicCountingIdempotency) Register(_ context.Context, _ string) error {
	m.registerCount.Add(1)
	return nil
}

func (m *atomicCountingIdempotency) MarkCompleted(_ context.Context, _ string) error {
	m.completedCount.Add(1)
	return nil
}
