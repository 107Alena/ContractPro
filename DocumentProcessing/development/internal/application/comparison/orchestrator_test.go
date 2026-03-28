package comparison

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"contractpro/document-processing/internal/application/lifecycle"
	"contractpro/document-processing/internal/domain/model"
	"contractpro/document-processing/internal/domain/port"
	"contractpro/document-processing/internal/infra/observability"
)

// --- Mocks ---

// mockPublisher records all published events.
type mockPublisher struct {
	statusChanged       []model.StatusChangedEvent
	processingCompleted []model.ProcessingCompletedEvent
	processingFailed    []model.ProcessingFailedEvent
	comparisonCompleted []model.ComparisonCompletedEvent
	comparisonFailed    []model.ComparisonFailedEvent
	err                 error

	// completedErr allows failing only on PublishComparisonCompleted.
	completedErr error
}

func (m *mockPublisher) PublishStatusChanged(_ context.Context, event model.StatusChangedEvent) error {
	if m.err != nil {
		return m.err
	}
	m.statusChanged = append(m.statusChanged, event)
	return nil
}

func (m *mockPublisher) PublishProcessingCompleted(_ context.Context, event model.ProcessingCompletedEvent) error {
	if m.err != nil {
		return m.err
	}
	m.processingCompleted = append(m.processingCompleted, event)
	return nil
}

func (m *mockPublisher) PublishProcessingFailed(_ context.Context, event model.ProcessingFailedEvent) error {
	if m.err != nil {
		return m.err
	}
	m.processingFailed = append(m.processingFailed, event)
	return nil
}

func (m *mockPublisher) PublishComparisonCompleted(_ context.Context, event model.ComparisonCompletedEvent) error {
	if m.completedErr != nil {
		return m.completedErr
	}
	if m.err != nil {
		return m.err
	}
	m.comparisonCompleted = append(m.comparisonCompleted, event)
	return nil
}

func (m *mockPublisher) PublishComparisonFailed(_ context.Context, event model.ComparisonFailedEvent) error {
	if m.err != nil {
		return m.err
	}
	m.comparisonFailed = append(m.comparisonFailed, event)
	return nil
}

// mockIdempotency is a no-op idempotency store for lifecycle manager construction.
type mockIdempotency struct {
	completed []string
	err       error
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

// mockTreeRequester records RequestSemanticTree calls.
type mockTreeRequester struct {
	mu        sync.Mutex
	requests  []model.GetSemanticTreeRequest
	callCount int
	err       error
}

func (m *mockTreeRequester) RequestSemanticTree(_ context.Context, req model.GetSemanticTreeRequest) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.callCount++
	if m.err != nil {
		return m.err
	}
	m.requests = append(m.requests, req)
	return nil
}

// mockDMSender records SendDiffResult calls.
type mockDMSender struct {
	mu             sync.Mutex
	sentArtifacts  []model.DocumentProcessingArtifactsReady
	sentDiffResult []model.DocumentVersionDiffReady
	err            error
}

func (m *mockDMSender) SendArtifacts(_ context.Context, event model.DocumentProcessingArtifactsReady) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return m.err
	}
	m.sentArtifacts = append(m.sentArtifacts, event)
	return nil
}

func (m *mockDMSender) SendDiffResult(_ context.Context, event model.DocumentVersionDiffReady) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return m.err
	}
	m.sentDiffResult = append(m.sentDiffResult, event)
	return nil
}

// mockRegistry implements port.PendingResponseRegistryPort.
// It tracks Register, AwaitAll, and Cancel calls.
// AwaitAll returns pre-configured responses in order of calls.
type mockRegistry struct {
	mu sync.Mutex

	// registered tracks Register calls: map[jobID][]correlationIDs.
	registered map[string][]string
	// registerOrder records the order in which Register was called (jobID per call).
	registerOrder []string

	// awaitResponses holds the responses returned by successive AwaitAll calls.
	awaitResponses [][]port.PendingResponse
	awaitCallCount int

	// cancelled tracks Cancel calls.
	cancelled []string

	// Configurable errors.
	registerErr error
	awaitErr    error
}

func newMockRegistry() *mockRegistry {
	return &mockRegistry{
		registered: make(map[string][]string),
	}
}

func (m *mockRegistry) Register(jobID string, correlationIDs []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.registerErr != nil {
		return m.registerErr
	}
	m.registered[jobID] = append(m.registered[jobID], correlationIDs...)
	m.registerOrder = append(m.registerOrder, jobID)
	return nil
}

func (m *mockRegistry) AwaitAll(_ context.Context, _ string) ([]port.PendingResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.awaitErr != nil {
		return nil, m.awaitErr
	}
	idx := m.awaitCallCount
	m.awaitCallCount++
	if idx < len(m.awaitResponses) {
		return m.awaitResponses[idx], nil
	}
	return nil, nil
}

func (m *mockRegistry) Receive(_ string, _ model.SemanticTree) error {
	return nil
}

func (m *mockRegistry) ReceiveError(_ string, _ error) error {
	return nil
}

func (m *mockRegistry) Cancel(jobID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cancelled = append(m.cancelled, jobID)
}

// mockComparer implements port.VersionComparisonPort.
type mockComparer struct {
	result *model.VersionDiffResult
	err    error
}

func (m *mockComparer) Compare(_ context.Context, _, _ *model.SemanticTree) (*model.VersionDiffResult, error) {
	return m.result, m.err
}

// mockDLQ implements port.DLQPort.
type mockDLQ struct {
	mu       sync.Mutex
	messages []model.DLQMessage
	err      error
}

func (m *mockDLQ) SendToDLQ(_ context.Context, msg model.DLQMessage) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return m.err
	}
	m.messages = append(m.messages, msg)
	return nil
}

// --- Helpers ---

func defaultCompareCmd() model.CompareVersionsCommand {
	return model.CompareVersionsCommand{
		JobID:           "job-cmp-1",
		DocumentID:      "doc-1",
		BaseVersionID:   "v1",
		TargetVersionID: "v2",
		OrgID:           "org-1",
		UserID:          "user-1",
	}
}

func defaultBaseTree() *model.SemanticTree {
	return &model.SemanticTree{
		DocumentID: "doc-1",
		Root: &model.SemanticNode{
			ID:   "root",
			Type: model.NodeTypeRoot,
			Children: []*model.SemanticNode{
				{ID: "s1", Type: model.NodeTypeSection, Content: "Предмет договора (v1)"},
			},
		},
	}
}

func defaultTargetTree() *model.SemanticTree {
	return &model.SemanticTree{
		DocumentID: "doc-1",
		Root: &model.SemanticNode{
			ID:   "root",
			Type: model.NodeTypeRoot,
			Children: []*model.SemanticNode{
				{ID: "s1", Type: model.NodeTypeSection, Content: "Предмет договора (v2)"},
				{ID: "s2", Type: model.NodeTypeSection, Content: "Условия оплаты"},
			},
		},
	}
}

func defaultDiffResult() *model.VersionDiffResult {
	return &model.VersionDiffResult{
		DocumentID:    "doc-1",
		BaseVersionID: "v1",
		TargetVersionID: "v2",
		TextDiffs: []model.TextDiffEntry{
			{Type: model.DiffTypeModified, Path: "s1", OldContent: "v1", NewContent: "v2"},
		},
		StructuralDiffs: []model.StructuralDiffEntry{
			{Type: model.DiffTypeAdded, NodeType: model.NodeTypeSection, NodeID: "s2", Path: "root/s2"},
		},
	}
}

// testDeps holds all mock dependencies for constructing a comparison Orchestrator.
type testDeps struct {
	publisher   *mockPublisher
	idempotency *mockIdempotency
	treeReq     *mockTreeRequester
	dmSender    *mockDMSender
	registry    *mockRegistry
	comparer    *mockComparer
	dlq         *mockDLQ
}

// newTestDeps creates testDeps pre-configured for a successful happy-path comparison pipeline.
func newTestDeps() *testDeps {
	cmd := defaultCompareCmd()
	baseCorrID := fmt.Sprintf("%s:base:%s", cmd.JobID, cmd.BaseVersionID)
	targetCorrID := fmt.Sprintf("%s:target:%s", cmd.JobID, cmd.TargetVersionID)
	confirmCorrID := cmd.JobID + ":diff-confirm"

	reg := newMockRegistry()
	// First AwaitAll: returns tree responses (base + target).
	reg.awaitResponses = [][]port.PendingResponse{
		{
			{CorrelationID: baseCorrID, Tree: defaultBaseTree()},
			{CorrelationID: targetCorrID, Tree: defaultTargetTree()},
		},
		// Second AwaitAll: returns confirmation (no error).
		{
			{CorrelationID: confirmCorrID},
		},
	}

	return &testDeps{
		publisher:   &mockPublisher{},
		idempotency: &mockIdempotency{},
		treeReq:     &mockTreeRequester{},
		dmSender:    &mockDMSender{},
		registry:    reg,
		comparer:    &mockComparer{result: defaultDiffResult()},
		dlq:         &mockDLQ{},
	}
}

func nopLogger() *observability.Logger { return observability.NewLogger("error") }

func (d *testDeps) build() *Orchestrator {
	lm := lifecycle.NewLifecycleManager(d.publisher, d.idempotency, 120*time.Second, nil, nopLogger())
	return NewOrchestrator(
		lm,
		d.treeReq,
		d.dmSender,
		d.registry,
		d.comparer,
		d.publisher,
		d.dlq,
		nopLogger(),
		1,
		time.Millisecond,
	)
}

func (d *testDeps) buildWithRetry(maxRetries int, backoff time.Duration) *Orchestrator {
	lm := lifecycle.NewLifecycleManager(d.publisher, d.idempotency, 120*time.Second, nil, nopLogger())
	return NewOrchestrator(
		lm,
		d.treeReq,
		d.dmSender,
		d.registry,
		d.comparer,
		d.publisher,
		d.dlq,
		nopLogger(),
		maxRetries,
		backoff,
	)
}

// --- Tests: Constructor ---

func TestNewOrchestrator_PanicsOnNilDeps(t *testing.T) {
	pub := &mockPublisher{}
	idem := &mockIdempotency{}
	lm := lifecycle.NewLifecycleManager(pub, idem, 120*time.Second, nil, nopLogger())

	validArgs := []interface{}{
		lm,
		&mockTreeRequester{},
		&mockDMSender{},
		newMockRegistry(),
		&mockComparer{result: defaultDiffResult()},
		pub,
		&mockDLQ{},
		nopLogger(),
	}

	depNames := []string{
		"lifecycle", "treeRequester", "dmSender",
		"registry", "comparer", "publisher", "dlq", "logger",
	}

	for i, name := range depNames {
		t.Run("nil_"+name, func(t *testing.T) {
			defer func() {
				if r := recover(); r == nil {
					t.Errorf("expected panic for nil %s", name)
				}
			}()

			args := make([]interface{}, len(validArgs))
			copy(args, validArgs)
			args[i] = nil

			NewOrchestrator(
				asLifecycle(args[0]),
				asTreeRequester(args[1]),
				asDMSender(args[2]),
				asRegistry(args[3]),
				asComparer(args[4]),
				asPublisher(args[5]),
				asDLQ(args[6]),
				asLogger(args[7]),
				1,
				time.Millisecond,
			)
		})
	}
}

func TestNewOrchestrator_Defaults(t *testing.T) {
	deps := newTestDeps()
	pub := deps.publisher
	idem := deps.idempotency
	lm := lifecycle.NewLifecycleManager(pub, idem, 120*time.Second, nil, nopLogger())

	// maxRetries < 1 defaults to 1, backoffBase <= 0 defaults to time.Second.
	orch := NewOrchestrator(
		lm,
		deps.treeReq,
		deps.dmSender,
		deps.registry,
		deps.comparer,
		pub,
		&mockDLQ{},
		nopLogger(),
		0,
		0,
	)

	if orch.maxRetries != 1 {
		t.Errorf("expected maxRetries default 1, got %d", orch.maxRetries)
	}
	if orch.backoffBase != time.Second {
		t.Errorf("expected backoffBase default 1s, got %v", orch.backoffBase)
	}
}

// Type assertion helpers for the nil-panic test.
func asLifecycle(v interface{}) *lifecycle.LifecycleManager {
	if v == nil {
		return nil
	}
	return v.(*lifecycle.LifecycleManager)
}

func asTreeRequester(v interface{}) port.DMTreeRequesterPort {
	if v == nil {
		return nil
	}
	return v.(port.DMTreeRequesterPort)
}

func asDMSender(v interface{}) port.DMArtifactSenderPort {
	if v == nil {
		return nil
	}
	return v.(port.DMArtifactSenderPort)
}

func asRegistry(v interface{}) port.PendingResponseRegistryPort {
	if v == nil {
		return nil
	}
	return v.(port.PendingResponseRegistryPort)
}

func asComparer(v interface{}) port.VersionComparisonPort {
	if v == nil {
		return nil
	}
	return v.(port.VersionComparisonPort)
}

func asPublisher(v interface{}) port.EventPublisherPort {
	if v == nil {
		return nil
	}
	return v.(port.EventPublisherPort)
}

func asDLQ(v interface{}) port.DLQPort {
	if v == nil {
		return nil
	}
	return v.(port.DLQPort)
}

func asLogger(v interface{}) *observability.Logger {
	if v == nil {
		return nil
	}
	return v.(*observability.Logger)
}

// --- Tests: classifyError ---

func TestClassifyError_Table(t *testing.T) {
	tests := []struct {
		name              string
		err               error
		expectedStatus    model.JobStatus
		expectedRetryable bool
	}{
		{
			name:              "DeadlineExceeded",
			err:               context.DeadlineExceeded,
			expectedStatus:    model.StatusTimedOut,
			expectedRetryable: true,
		},
		{
			name:              "ValidationError",
			err:               port.NewValidationError("bad input"),
			expectedStatus:    model.StatusRejected,
			expectedRetryable: false,
		},
		{
			name:              "DMVersionNotFound",
			err:               port.NewDMVersionNotFoundError("v1", nil),
			expectedStatus:    model.StatusRejected,
			expectedRetryable: false,
		},
		{
			name:              "BrokerFailed",
			err:               port.NewBrokerError("broker down", errors.New("err")),
			expectedStatus:    model.StatusFailed,
			expectedRetryable: true,
		},
		{
			name:              "GenericError",
			err:               errors.New("unknown error"),
			expectedStatus:    model.StatusFailed,
			expectedRetryable: false,
		},
		{
			name:              "ContextCanceled",
			err:               context.Canceled,
			expectedStatus:    model.StatusTimedOut,
			expectedRetryable: true,
		},
		{
			name:              "WrappedContextCanceled",
			err:               fmt.Errorf("wrapped: %w", context.Canceled),
			expectedStatus:    model.StatusTimedOut,
			expectedRetryable: true,
		},
		{
			name:              "WrappedDeadlineExceeded",
			err:               port.NewTimeoutError("timed out", context.DeadlineExceeded),
			expectedStatus:    model.StatusTimedOut,
			expectedRetryable: true,
		},
		{
			name:              "StorageFailed_Retryable",
			err:               port.NewStorageError("storage down", errors.New("err")),
			expectedStatus:    model.StatusFailed,
			expectedRetryable: true,
		},
		{
			name:              "DiffPersistFailed_Retryable",
			err:               port.NewDMDiffPersistFailedError("DM failed to persist", true, nil),
			expectedStatus:    model.StatusFailed,
			expectedRetryable: true,
		},
		{
			name:              "DiffPersistFailed_NonRetryable",
			err:               port.NewDMDiffPersistFailedError("DM failed to persist", false, nil),
			expectedStatus:    model.StatusFailed,
			expectedRetryable: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, retryable := classifyError(tt.err)
			if status != tt.expectedStatus {
				t.Errorf("expected status %s, got %s", tt.expectedStatus, status)
			}
			if retryable != tt.expectedRetryable {
				t.Errorf("expected retryable=%v, got %v", tt.expectedRetryable, retryable)
			}
		})
	}
}

// --- Tests: Happy Path ---

func TestHappyPath_Completed(t *testing.T) {
	deps := newTestDeps()
	orch := deps.build()
	cmd := defaultCompareCmd()

	err := orch.HandleCompareVersions(context.Background(), cmd)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	// Verify status transitions: QUEUED -> IN_PROGRESS, then IN_PROGRESS -> COMPLETED.
	if len(deps.publisher.statusChanged) != 2 {
		t.Fatalf("expected 2 StatusChangedEvents, got %d", len(deps.publisher.statusChanged))
	}

	first := deps.publisher.statusChanged[0]
	if first.OldStatus != model.StatusQueued || first.NewStatus != model.StatusInProgress {
		t.Errorf("first transition: expected QUEUED->IN_PROGRESS, got %s->%s", first.OldStatus, first.NewStatus)
	}

	second := deps.publisher.statusChanged[1]
	if second.OldStatus != model.StatusInProgress || second.NewStatus != model.StatusCompleted {
		t.Errorf("second transition: expected IN_PROGRESS->COMPLETED, got %s->%s", second.OldStatus, second.NewStatus)
	}

	// Verify ComparisonCompletedEvent.
	if len(deps.publisher.comparisonCompleted) != 1 {
		t.Fatalf("expected 1 ComparisonCompletedEvent, got %d", len(deps.publisher.comparisonCompleted))
	}
	completed := deps.publisher.comparisonCompleted[0]
	if completed.JobID != "job-cmp-1" {
		t.Errorf("expected JobID job-cmp-1, got %s", completed.JobID)
	}
	if completed.DocumentID != "doc-1" {
		t.Errorf("expected DocumentID doc-1, got %s", completed.DocumentID)
	}
	if completed.Status != model.StatusCompleted {
		t.Errorf("expected status COMPLETED, got %s", completed.Status)
	}
	if completed.BaseVersionID != "v1" {
		t.Errorf("expected BaseVersionID v1, got %s", completed.BaseVersionID)
	}
	if completed.TargetVersionID != "v2" {
		t.Errorf("expected TargetVersionID v2, got %s", completed.TargetVersionID)
	}
	if completed.TextDiffCount != 1 {
		t.Errorf("expected TextDiffCount=1, got %d", completed.TextDiffCount)
	}
	if completed.StructuralDiffCount != 1 {
		t.Errorf("expected StructuralDiffCount=1, got %d", completed.StructuralDiffCount)
	}
	if completed.CorrelationID != "job-cmp-1" {
		t.Errorf("expected CorrelationID job-cmp-1, got %s", completed.CorrelationID)
	}

	// Verify no failure events.
	if len(deps.publisher.comparisonFailed) != 0 {
		t.Error("no ComparisonFailedEvent should be published on success")
	}

	// Verify idempotency was marked.
	if len(deps.idempotency.completed) != 1 || deps.idempotency.completed[0] != "job-cmp-1" {
		t.Errorf("expected idempotency marked for job-cmp-1, got %v", deps.idempotency.completed)
	}
}

func TestHappyPath_NoWarnings(t *testing.T) {
	// The comparison pipeline does not currently produce warnings.
	// Verify that the pipeline always completes with COMPLETED status.
	deps := newTestDeps()
	orch := deps.build()
	cmd := defaultCompareCmd()

	err := orch.HandleCompareVersions(context.Background(), cmd)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	// Verify terminal status is COMPLETED.
	if len(deps.publisher.statusChanged) != 2 {
		t.Fatalf("expected 2 StatusChangedEvents, got %d", len(deps.publisher.statusChanged))
	}
	second := deps.publisher.statusChanged[1]
	if second.NewStatus != model.StatusCompleted {
		t.Errorf("expected COMPLETED, got %s", second.NewStatus)
	}

	// Verify ComparisonCompletedEvent reflects COMPLETED.
	if len(deps.publisher.comparisonCompleted) != 1 {
		t.Fatalf("expected 1 ComparisonCompletedEvent, got %d", len(deps.publisher.comparisonCompleted))
	}
	if deps.publisher.comparisonCompleted[0].Status != model.StatusCompleted {
		t.Errorf("expected status COMPLETED, got %s", deps.publisher.comparisonCompleted[0].Status)
	}
}

func TestHappyPath_CorrelationIDFormat(t *testing.T) {
	deps := newTestDeps()
	orch := deps.build()
	cmd := defaultCompareCmd()

	err := orch.HandleCompareVersions(context.Background(), cmd)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	// Verify the tree requester received correct correlation IDs.
	if len(deps.treeReq.requests) != 2 {
		t.Fatalf("expected 2 tree requests, got %d", len(deps.treeReq.requests))
	}

	baseCorrID := fmt.Sprintf("%s:base:%s", cmd.JobID, cmd.BaseVersionID)
	targetCorrID := fmt.Sprintf("%s:target:%s", cmd.JobID, cmd.TargetVersionID)

	gotBase := deps.treeReq.requests[0].CorrelationID
	gotTarget := deps.treeReq.requests[1].CorrelationID

	if gotBase != baseCorrID {
		t.Errorf("base correlation ID: expected %s, got %s", baseCorrID, gotBase)
	}
	if gotTarget != targetCorrID {
		t.Errorf("target correlation ID: expected %s, got %s", targetCorrID, gotTarget)
	}
}

func TestHappyPath_RegisterBeforeRequestSemanticTree(t *testing.T) {
	// Verify that Register is called before RequestSemanticTree.
	// We use a custom tree requester that checks the registry state.
	deps := newTestDeps()

	// Override tree requester to check that registry has been called.
	checkingReq := &orderCheckingTreeRequester{
		registry: deps.registry,
	}

	pub := deps.publisher
	idem := deps.idempotency
	lm := lifecycle.NewLifecycleManager(pub, idem, 120*time.Second, nil, nopLogger())
	orch := NewOrchestrator(
		lm,
		checkingReq,
		deps.dmSender,
		deps.registry,
		deps.comparer,
		pub,
		&mockDLQ{},
		nopLogger(),
		1,
		time.Millisecond,
	)

	cmd := defaultCompareCmd()
	err := orch.HandleCompareVersions(context.Background(), cmd)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if !checkingReq.registeredBeforeRequest {
		t.Error("expected Register to be called before RequestSemanticTree")
	}
}

// orderCheckingTreeRequester verifies that registry.Register was called before RequestSemanticTree.
type orderCheckingTreeRequester struct {
	registry                *mockRegistry
	registeredBeforeRequest bool
	checked                 bool
}

func (m *orderCheckingTreeRequester) RequestSemanticTree(_ context.Context, req model.GetSemanticTreeRequest) error {
	if !m.checked {
		m.checked = true
		m.registry.mu.Lock()
		m.registeredBeforeRequest = len(m.registry.registerOrder) > 0
		m.registry.mu.Unlock()
	}
	return nil
}

func TestHappyPath_TreesRoutedToComparer(t *testing.T) {
	// Verify that the base and target trees from DM responses are passed to the comparer.
	deps := newTestDeps()

	// Use a capturing comparer — build orchestrator manually.
	cc := &capturingComparer{result: defaultDiffResult()}
	pub := deps.publisher
	idem := deps.idempotency
	lm := lifecycle.NewLifecycleManager(pub, idem, 120*time.Second, nil, nopLogger())
	orch := NewOrchestrator(
		lm,
		deps.treeReq,
		deps.dmSender,
		deps.registry,
		cc,
		pub,
		&mockDLQ{},
		nopLogger(),
		1,
		time.Millisecond,
	)
	cmd := defaultCompareCmd()

	err := orch.HandleCompareVersions(context.Background(), cmd)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if cc.baseTree == nil {
		t.Fatal("base tree was nil")
	}
	if cc.targetTree == nil {
		t.Fatal("target tree was nil")
	}
	// Verify the correct trees were routed based on content.
	if cc.baseTree.Root.Children[0].Content != "Предмет договора (v1)" {
		t.Errorf("base tree content mismatch: %s", cc.baseTree.Root.Children[0].Content)
	}
	if cc.targetTree.Root.Children[0].Content != "Предмет договора (v2)" {
		t.Errorf("target tree content mismatch: %s", cc.targetTree.Root.Children[0].Content)
	}
}

type capturingComparer struct {
	baseTree   *model.SemanticTree
	targetTree *model.SemanticTree
	result     *model.VersionDiffResult
}

func (m *capturingComparer) Compare(_ context.Context, baseTree, targetTree *model.SemanticTree) (*model.VersionDiffResult, error) {
	m.baseTree = baseTree
	m.targetTree = targetTree
	return m.result, nil
}

func TestHappyPath_DiffResultSentToDM(t *testing.T) {
	deps := newTestDeps()
	orch := deps.build()
	cmd := defaultCompareCmd()

	err := orch.HandleCompareVersions(context.Background(), cmd)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	// Verify SendDiffResult was called with correct content.
	if len(deps.dmSender.sentDiffResult) != 1 {
		t.Fatalf("expected 1 SendDiffResult call, got %d", len(deps.dmSender.sentDiffResult))
	}
	diffEvent := deps.dmSender.sentDiffResult[0]
	if diffEvent.JobID != "job-cmp-1" {
		t.Errorf("expected JobID job-cmp-1, got %s", diffEvent.JobID)
	}
	if diffEvent.DocumentID != "doc-1" {
		t.Errorf("expected DocumentID doc-1, got %s", diffEvent.DocumentID)
	}
	if diffEvent.BaseVersionID != "v1" {
		t.Errorf("expected BaseVersionID v1, got %s", diffEvent.BaseVersionID)
	}
	if diffEvent.TargetVersionID != "v2" {
		t.Errorf("expected TargetVersionID v2, got %s", diffEvent.TargetVersionID)
	}
	if len(diffEvent.TextDiffs) != 1 {
		t.Errorf("expected 1 text diff, got %d", len(diffEvent.TextDiffs))
	}
	if len(diffEvent.StructuralDiffs) != 1 {
		t.Errorf("expected 1 structural diff, got %d", len(diffEvent.StructuralDiffs))
	}
	if diffEvent.TextDiffCount != 1 {
		t.Errorf("expected TextDiffCount=1, got %d", diffEvent.TextDiffCount)
	}
	if diffEvent.StructuralDiffCount != 1 {
		t.Errorf("expected StructuralDiffCount=1, got %d", diffEvent.StructuralDiffCount)
	}

	// Verify the correlation ID follows confirmCorrID format.
	expectedConfirmCorrID := cmd.JobID + ":diff-confirm"
	if diffEvent.CorrelationID != expectedConfirmCorrID {
		t.Errorf("expected confirm correlation ID %s, got %s", expectedConfirmCorrID, diffEvent.CorrelationID)
	}
}

func TestHappyPath_CommandFieldsCopiedToJob(t *testing.T) {
	deps := newTestDeps()
	orch := deps.build()

	cmd := model.CompareVersionsCommand{
		JobID:           "job-cmp-42",
		DocumentID:      "doc-99",
		BaseVersionID:   "v10",
		TargetVersionID: "v20",
		OrgID:           "org-5",
		UserID:          "user-7",
	}

	// Reconfigure registry for this command's correlation IDs.
	baseCorrID := fmt.Sprintf("%s:base:%s", cmd.JobID, cmd.BaseVersionID)
	targetCorrID := fmt.Sprintf("%s:target:%s", cmd.JobID, cmd.TargetVersionID)
	confirmCorrID := cmd.JobID + ":diff-confirm"
	deps.registry.awaitResponses = [][]port.PendingResponse{
		{
			{CorrelationID: baseCorrID, Tree: defaultBaseTree()},
			{CorrelationID: targetCorrID, Tree: defaultTargetTree()},
		},
		{
			{CorrelationID: confirmCorrID},
		},
	}
	deps.registry.awaitCallCount = 0

	err := orch.HandleCompareVersions(context.Background(), cmd)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	// Verify job ID propagated to status events.
	if len(deps.publisher.statusChanged) < 1 {
		t.Fatal("expected at least 1 StatusChangedEvent")
	}
	if deps.publisher.statusChanged[0].JobID != "job-cmp-42" {
		t.Errorf("expected JobID job-cmp-42, got %s", deps.publisher.statusChanged[0].JobID)
	}
	if deps.publisher.statusChanged[0].DocumentID != "doc-99" {
		t.Errorf("expected DocumentID doc-99, got %s", deps.publisher.statusChanged[0].DocumentID)
	}
}

func TestHappyPath_SecondRegisterAwaitForDiffConfirmation(t *testing.T) {
	deps := newTestDeps()
	orch := deps.build()
	cmd := defaultCompareCmd()

	err := orch.HandleCompareVersions(context.Background(), cmd)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	// Verify Register was called twice (once for trees, once for confirmation).
	deps.registry.mu.Lock()
	registerCount := len(deps.registry.registerOrder)
	deps.registry.mu.Unlock()

	if registerCount != 2 {
		t.Errorf("expected 2 Register calls, got %d", registerCount)
	}

	// Verify AwaitAll was called twice.
	deps.registry.mu.Lock()
	awaitCount := deps.registry.awaitCallCount
	deps.registry.mu.Unlock()

	if awaitCount != 2 {
		t.Errorf("expected 2 AwaitAll calls, got %d", awaitCount)
	}

	// Verify the second Register used the confirmCorrID.
	deps.registry.mu.Lock()
	allRegistered := deps.registry.registered[cmd.JobID]
	deps.registry.mu.Unlock()

	confirmCorrID := cmd.JobID + ":diff-confirm"
	found := false
	for _, id := range allRegistered {
		if id == confirmCorrID {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected confirmCorrID %s in registered IDs, got %v", confirmCorrID, allRegistered)
	}
}

// --- Tests: Error Handling ---

func TestError_BaseTreeRequestError(t *testing.T) {
	deps := newTestDeps()
	deps.treeReq.err = port.NewBrokerError("broker down", errors.New("connection refused"))

	orch := deps.build()
	cmd := defaultCompareCmd()

	err := orch.HandleCompareVersions(context.Background(), cmd)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	var domErr *port.DomainError
	if !errors.As(err, &domErr) {
		t.Fatalf("expected DomainError, got %T: %v", err, err)
	}

	// Verify ComparisonFailedEvent was published.
	if len(deps.publisher.comparisonFailed) != 1 {
		t.Fatalf("expected 1 ComparisonFailedEvent, got %d", len(deps.publisher.comparisonFailed))
	}
	failed := deps.publisher.comparisonFailed[0]
	if failed.Status != model.StatusFailed {
		t.Errorf("expected FAILED, got %s", failed.Status)
	}

	// Verify registry was cancelled.
	deps.registry.mu.Lock()
	cancelCount := len(deps.registry.cancelled)
	deps.registry.mu.Unlock()

	// cancelled is called in runPipeline (base tree error) + handlePipelineError
	if cancelCount < 1 {
		t.Error("expected registry Cancel to be called")
	}
}

func TestError_TargetTreeRequestError(t *testing.T) {
	deps := newTestDeps()

	// Make the tree requester fail on second call only.
	failOnSecond := &callCountTreeRequester{
		failOnCall: 2,
		err:        port.NewBrokerError("broker down", errors.New("connection refused")),
	}

	pub := deps.publisher
	idem := deps.idempotency
	lm := lifecycle.NewLifecycleManager(pub, idem, 120*time.Second, nil, nopLogger())
	orch := NewOrchestrator(
		lm,
		failOnSecond,
		deps.dmSender,
		deps.registry,
		deps.comparer,
		pub,
		&mockDLQ{},
		nopLogger(),
		1,
		time.Millisecond,
	)
	cmd := defaultCompareCmd()
	err := orch.HandleCompareVersions(context.Background(), cmd)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	// Verify registry was cancelled (in runPipeline for target tree error + handlePipelineError).
	deps.registry.mu.Lock()
	cancelCount := len(deps.registry.cancelled)
	deps.registry.mu.Unlock()

	if cancelCount < 1 {
		t.Error("expected registry Cancel to be called on target tree request error")
	}
}

// callCountTreeRequester fails on a specific call number.
type callCountTreeRequester struct {
	mu         sync.Mutex
	callCount  int
	failOnCall int
	err        error
}

func (m *callCountTreeRequester) RequestSemanticTree(_ context.Context, _ model.GetSemanticTreeRequest) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.callCount++
	if m.callCount == m.failOnCall {
		return m.err
	}
	return nil
}

func TestError_AwaitAllTimeout(t *testing.T) {
	deps := newTestDeps()
	deps.registry.awaitErr = context.DeadlineExceeded

	orch := deps.build()
	cmd := defaultCompareCmd()

	err := orch.HandleCompareVersions(context.Background(), cmd)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("expected DeadlineExceeded, got: %v", err)
	}

	// Verify ComparisonFailedEvent with TIMED_OUT.
	if len(deps.publisher.comparisonFailed) != 1 {
		t.Fatalf("expected 1 ComparisonFailedEvent, got %d", len(deps.publisher.comparisonFailed))
	}
	failed := deps.publisher.comparisonFailed[0]
	if failed.Status != model.StatusTimedOut {
		t.Errorf("expected TIMED_OUT, got %s", failed.Status)
	}
	if !failed.IsRetryable {
		t.Error("expected IsRetryable=true for TIMED_OUT")
	}
}

func TestError_TreeResponseWithDMVersionNotFound(t *testing.T) {
	deps := newTestDeps()
	cmd := defaultCompareCmd()
	baseCorrID := fmt.Sprintf("%s:base:%s", cmd.JobID, cmd.BaseVersionID)
	targetCorrID := fmt.Sprintf("%s:target:%s", cmd.JobID, cmd.TargetVersionID)

	// First AwaitAll returns one response with DM_VERSION_NOT_FOUND error.
	deps.registry.awaitResponses = [][]port.PendingResponse{
		{
			{CorrelationID: baseCorrID, Err: port.NewDMVersionNotFoundError("v1", nil)},
			{CorrelationID: targetCorrID, Tree: defaultTargetTree()},
		},
	}
	deps.registry.awaitCallCount = 0

	orch := deps.build()
	err := orch.HandleCompareVersions(context.Background(), cmd)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	// Verify ComparisonFailedEvent with REJECTED.
	if len(deps.publisher.comparisonFailed) != 1 {
		t.Fatalf("expected 1 ComparisonFailedEvent, got %d", len(deps.publisher.comparisonFailed))
	}
	failed := deps.publisher.comparisonFailed[0]
	if failed.Status != model.StatusRejected {
		t.Errorf("expected REJECTED for DM_VERSION_NOT_FOUND, got %s", failed.Status)
	}
	if failed.IsRetryable {
		t.Error("expected IsRetryable=false for DM_VERSION_NOT_FOUND")
	}
}

func TestError_ComparerError(t *testing.T) {
	deps := newTestDeps()
	deps.comparer.result = nil
	deps.comparer.err = port.NewExtractionError("comparison failed", errors.New("tree mismatch"))

	orch := deps.build()
	cmd := defaultCompareCmd()

	err := orch.HandleCompareVersions(context.Background(), cmd)
	if err == nil {
		t.Fatal("expected comparer error, got nil")
	}

	var domErr *port.DomainError
	if !errors.As(err, &domErr) {
		t.Fatalf("expected DomainError, got %T: %v", err, err)
	}

	// Verify ComparisonFailedEvent with FAILED.
	if len(deps.publisher.comparisonFailed) != 1 {
		t.Fatalf("expected 1 ComparisonFailedEvent, got %d", len(deps.publisher.comparisonFailed))
	}
	if deps.publisher.comparisonFailed[0].Status != model.StatusFailed {
		t.Errorf("expected FAILED, got %s", deps.publisher.comparisonFailed[0].Status)
	}

	// Verify failed at stage.
	if deps.publisher.comparisonFailed[0].FailedAtStage != string(model.ComparisonStageExecutingDiff) {
		t.Errorf("expected FailedAtStage=%s, got %s", model.ComparisonStageExecutingDiff, deps.publisher.comparisonFailed[0].FailedAtStage)
	}
}

func TestError_SendDiffResultError(t *testing.T) {
	deps := newTestDeps()
	deps.dmSender.err = port.NewBrokerError("broker unavailable", errors.New("connection refused"))

	orch := deps.build()
	cmd := defaultCompareCmd()

	err := orch.HandleCompareVersions(context.Background(), cmd)
	if err == nil {
		t.Fatal("expected SendDiffResult error, got nil")
	}

	var domErr *port.DomainError
	if !errors.As(err, &domErr) {
		t.Fatalf("expected DomainError, got %T: %v", err, err)
	}

	// Verify ComparisonFailedEvent with FAILED.
	if len(deps.publisher.comparisonFailed) != 1 {
		t.Fatalf("expected 1 ComparisonFailedEvent, got %d", len(deps.publisher.comparisonFailed))
	}
	if deps.publisher.comparisonFailed[0].Status != model.StatusFailed {
		t.Errorf("expected FAILED, got %s", deps.publisher.comparisonFailed[0].Status)
	}

	// Verify failed at stage.
	if deps.publisher.comparisonFailed[0].FailedAtStage != string(model.ComparisonStageSavingResult) {
		t.Errorf("expected FailedAtStage=%s, got %s", model.ComparisonStageSavingResult, deps.publisher.comparisonFailed[0].FailedAtStage)
	}
}

func TestError_ConfirmationResponseError(t *testing.T) {
	deps := newTestDeps()
	cmd := defaultCompareCmd()
	baseCorrID := fmt.Sprintf("%s:base:%s", cmd.JobID, cmd.BaseVersionID)
	targetCorrID := fmt.Sprintf("%s:target:%s", cmd.JobID, cmd.TargetVersionID)
	confirmCorrID := cmd.JobID + ":diff-confirm"

	// First AwaitAll returns trees successfully, second returns confirmation with error.
	deps.registry.awaitResponses = [][]port.PendingResponse{
		{
			{CorrelationID: baseCorrID, Tree: defaultBaseTree()},
			{CorrelationID: targetCorrID, Tree: defaultTargetTree()},
		},
		{
			{CorrelationID: confirmCorrID, Err: port.NewBrokerError("DM failed to persist diff", errors.New("storage error"))},
		},
	}
	deps.registry.awaitCallCount = 0

	orch := deps.build()
	err := orch.HandleCompareVersions(context.Background(), cmd)
	if err == nil {
		t.Fatal("expected confirmation error, got nil")
	}

	// Verify ComparisonFailedEvent with FAILED.
	if len(deps.publisher.comparisonFailed) != 1 {
		t.Fatalf("expected 1 ComparisonFailedEvent, got %d", len(deps.publisher.comparisonFailed))
	}
	if deps.publisher.comparisonFailed[0].Status != model.StatusFailed {
		t.Errorf("expected FAILED, got %s", deps.publisher.comparisonFailed[0].Status)
	}
}

func TestError_MissingTreeNil(t *testing.T) {
	deps := newTestDeps()
	cmd := defaultCompareCmd()
	baseCorrID := fmt.Sprintf("%s:base:%s", cmd.JobID, cmd.BaseVersionID)
	targetCorrID := fmt.Sprintf("%s:target:%s", cmd.JobID, cmd.TargetVersionID)

	// Return responses where base tree is nil (no error, but Tree is nil).
	deps.registry.awaitResponses = [][]port.PendingResponse{
		{
			{CorrelationID: baseCorrID, Tree: nil},
			{CorrelationID: targetCorrID, Tree: defaultTargetTree()},
		},
	}
	deps.registry.awaitCallCount = 0

	orch := deps.build()
	err := orch.HandleCompareVersions(context.Background(), cmd)
	if err == nil {
		t.Fatal("expected validation error for missing tree, got nil")
	}

	// Verify it was classified as REJECTED (validation error).
	if len(deps.publisher.comparisonFailed) != 1 {
		t.Fatalf("expected 1 ComparisonFailedEvent, got %d", len(deps.publisher.comparisonFailed))
	}
	failed := deps.publisher.comparisonFailed[0]
	if failed.Status != model.StatusRejected {
		t.Errorf("expected REJECTED for missing tree, got %s", failed.Status)
	}
	if failed.ErrorCode != port.ErrCodeValidation {
		t.Errorf("expected error code %s, got %s", port.ErrCodeValidation, failed.ErrorCode)
	}
}

func TestError_RegisterError(t *testing.T) {
	deps := newTestDeps()
	deps.registry.registerErr = errors.New("registry full")

	orch := deps.build()
	cmd := defaultCompareCmd()

	err := orch.HandleCompareVersions(context.Background(), cmd)
	if err == nil {
		t.Fatal("expected register error, got nil")
	}

	// Pipeline should abort; ComparisonFailedEvent should be published.
	if len(deps.publisher.comparisonFailed) != 1 {
		t.Fatalf("expected 1 ComparisonFailedEvent, got %d", len(deps.publisher.comparisonFailed))
	}
}

func TestError_TransitionErrorAtStart(t *testing.T) {
	// Simulate a transition error by making the publisher fail on PublishStatusChanged.
	deps := newTestDeps()
	deps.publisher.err = errors.New("publish status failed")

	orch := deps.build()
	cmd := defaultCompareCmd()

	err := orch.HandleCompareVersions(context.Background(), cmd)
	if err == nil {
		t.Fatal("expected transition error, got nil")
	}

	// No tree requests should have been made.
	deps.treeReq.mu.Lock()
	reqCount := len(deps.treeReq.requests)
	deps.treeReq.mu.Unlock()

	if reqCount != 0 {
		t.Errorf("expected 0 tree requests on transition error, got %d", reqCount)
	}
}

func TestError_PublishCompletedError(t *testing.T) {
	deps := newTestDeps()
	deps.publisher.completedErr = errors.New("completion publish failed")

	orch := deps.build()
	cmd := defaultCompareCmd()

	err := orch.HandleCompareVersions(context.Background(), cmd)
	if err == nil {
		t.Fatal("expected publish completed error, got nil")
	}

	if err.Error() != "completion publish failed" {
		t.Errorf("expected 'completion publish failed', got: %v", err)
	}
}

// --- Tests: handlePipelineError ---

func TestHandlePipelineError_FieldCompleteness(t *testing.T) {
	deps := newTestDeps()
	deps.comparer.result = nil
	deps.comparer.err = port.NewExtractionError("comparison engine failure", errors.New("internal"))

	orch := deps.build()
	cmd := defaultCompareCmd()

	before := time.Now().UTC()
	_ = orch.HandleCompareVersions(context.Background(), cmd)
	after := time.Now().UTC()

	if len(deps.publisher.comparisonFailed) != 1 {
		t.Fatalf("expected 1 ComparisonFailedEvent, got %d", len(deps.publisher.comparisonFailed))
	}
	f := deps.publisher.comparisonFailed[0]

	// 1. CorrelationID
	if f.CorrelationID != "job-cmp-1" {
		t.Errorf("CorrelationID: expected job-cmp-1, got %s", f.CorrelationID)
	}
	// 2. Timestamp
	if f.Timestamp.Before(before) || f.Timestamp.After(after) {
		t.Errorf("Timestamp %v outside range [%v, %v]", f.Timestamp, before, after)
	}
	// 3. JobID
	if f.JobID != "job-cmp-1" {
		t.Errorf("JobID: expected job-cmp-1, got %s", f.JobID)
	}
	// 4. DocumentID
	if f.DocumentID != "doc-1" {
		t.Errorf("DocumentID: expected doc-1, got %s", f.DocumentID)
	}
	// 5. Status
	if f.Status != model.StatusFailed {
		t.Errorf("Status: expected FAILED, got %s", f.Status)
	}
	// 6. ErrorCode
	if f.ErrorCode != port.ErrCodeExtractionFailed {
		t.Errorf("ErrorCode: expected %s, got %s", port.ErrCodeExtractionFailed, f.ErrorCode)
	}
	// 7. ErrorMessage
	if f.ErrorMessage == "" {
		t.Error("ErrorMessage should not be empty")
	}
	// 8. FailedAtStage
	if f.FailedAtStage != string(model.ComparisonStageExecutingDiff) {
		t.Errorf("FailedAtStage: expected %s, got %s", model.ComparisonStageExecutingDiff, f.FailedAtStage)
	}
	// 9. IsRetryable
	if f.IsRetryable {
		t.Error("expected IsRetryable=false for extraction error")
	}
}

func TestHandlePipelineError_RegistryCancelledOnFailure(t *testing.T) {
	deps := newTestDeps()
	deps.comparer.result = nil
	deps.comparer.err = port.NewExtractionError("failed", errors.New("err"))

	orch := deps.build()
	cmd := defaultCompareCmd()

	_ = orch.HandleCompareVersions(context.Background(), cmd)

	// handlePipelineError should cancel the registry.
	deps.registry.mu.Lock()
	cancelCount := len(deps.registry.cancelled)
	deps.registry.mu.Unlock()

	if cancelCount < 1 {
		t.Error("expected registry Cancel to be called in handlePipelineError")
	}
}

// --- Tests: retryStep ---

func TestRetryStep_SuccessOnFirstAttempt(t *testing.T) {
	deps := newTestDeps()
	orch := deps.buildWithRetry(3, time.Millisecond)

	callCount := 0
	err := orch.retryStep(context.Background(), func() error {
		callCount++
		return nil
	})

	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if callCount != 1 {
		t.Errorf("expected 1 call, got %d", callCount)
	}
}

func TestRetryStep_NonRetryableError_ReturnsImmediately(t *testing.T) {
	deps := newTestDeps()
	orch := deps.buildWithRetry(3, time.Millisecond)

	nonRetryableErr := port.NewValidationError("bad input")
	callCount := 0
	err := orch.retryStep(context.Background(), func() error {
		callCount++
		return nonRetryableErr
	})

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if callCount != 1 {
		t.Errorf("expected 1 call (no retry for non-retryable), got %d", callCount)
	}
}

func TestRetryStep_ContextCancelledStopsRetry(t *testing.T) {
	deps := newTestDeps()
	orch := deps.buildWithRetry(5, 50*time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())

	retryableErr := port.NewBrokerError("broker down", errors.New("err"))
	callCount := 0
	err := orch.retryStep(ctx, func() error {
		callCount++
		if callCount == 1 {
			// Cancel context after first attempt so backoff select picks up cancellation.
			cancel()
		}
		return retryableErr
	})

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	// Should stop after context cancellation during backoff.
	if errors.Is(err, context.Canceled) {
		// Good: context cancellation was returned.
	} else if callCount <= 1 {
		// Also acceptable: cancelled before next attempt.
	}
	// Should NOT have exhausted all 5 retries.
	if callCount >= 5 {
		t.Errorf("expected early stop due to context cancellation, but got %d calls", callCount)
	}
}

func TestRetryStep_RetryableError_ExhaustsRetries(t *testing.T) {
	deps := newTestDeps()
	orch := deps.buildWithRetry(3, time.Millisecond)

	retryableErr := port.NewBrokerError("broker down", errors.New("err"))
	callCount := 0
	err := orch.retryStep(context.Background(), func() error {
		callCount++
		return retryableErr
	})

	if err == nil {
		t.Fatal("expected error after exhausted retries, got nil")
	}
	if callCount != 3 {
		t.Errorf("expected 3 calls (maxRetries=3), got %d", callCount)
	}
}

func TestRetryStep_RetryableError_ThenSuccess(t *testing.T) {
	deps := newTestDeps()
	orch := deps.buildWithRetry(3, time.Millisecond)

	retryableErr := port.NewBrokerError("broker down", errors.New("err"))
	callCount := 0
	err := orch.retryStep(context.Background(), func() error {
		callCount++
		if callCount < 2 {
			return retryableErr
		}
		return nil
	})

	if err != nil {
		t.Fatalf("expected success after retry, got: %v", err)
	}
	if callCount != 2 {
		t.Errorf("expected 2 calls (1 fail + 1 success), got %d", callCount)
	}
}

// --- Tests: Stage progression ---

func TestStageProgression(t *testing.T) {
	deps := newTestDeps()
	orch := deps.build()
	cmd := defaultCompareCmd()

	err := orch.HandleCompareVersions(context.Background(), cmd)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	// First transition should be at VALIDATING_INPUT stage.
	if len(deps.publisher.statusChanged) < 1 {
		t.Fatal("expected at least 1 StatusChangedEvent")
	}
	firstStage := deps.publisher.statusChanged[0].Stage
	if firstStage != string(model.ComparisonStageValidatingInput) {
		t.Errorf("first event stage: expected %s, got %s", model.ComparisonStageValidatingInput, firstStage)
	}

	// The final transition should have WAITING_DM_CONFIRMATION stage
	// (set before the terminal transition call).
	lastStage := deps.publisher.statusChanged[len(deps.publisher.statusChanged)-1].Stage
	if lastStage != string(model.ComparisonStageWaitingConfirm) {
		t.Errorf("last event stage: expected %s, got %s", model.ComparisonStageWaitingConfirm, lastStage)
	}
}

// --- Tests: Integration-style ---

func TestTreeRequestContainsCorrectVersionIDs(t *testing.T) {
	deps := newTestDeps()
	orch := deps.build()
	cmd := defaultCompareCmd()

	err := orch.HandleCompareVersions(context.Background(), cmd)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if len(deps.treeReq.requests) != 2 {
		t.Fatalf("expected 2 tree requests, got %d", len(deps.treeReq.requests))
	}

	baseReq := deps.treeReq.requests[0]
	if baseReq.VersionID != "v1" {
		t.Errorf("base request VersionID: expected v1, got %s", baseReq.VersionID)
	}
	if baseReq.DocumentID != "doc-1" {
		t.Errorf("base request DocumentID: expected doc-1, got %s", baseReq.DocumentID)
	}
	if baseReq.JobID != "job-cmp-1" {
		t.Errorf("base request JobID: expected job-cmp-1, got %s", baseReq.JobID)
	}

	targetReq := deps.treeReq.requests[1]
	if targetReq.VersionID != "v2" {
		t.Errorf("target request VersionID: expected v2, got %s", targetReq.VersionID)
	}
}

func TestNoCompletionEventOnError(t *testing.T) {
	deps := newTestDeps()
	deps.comparer.result = nil
	deps.comparer.err = port.NewExtractionError("failed", errors.New("err"))

	orch := deps.build()
	cmd := defaultCompareCmd()

	_ = orch.HandleCompareVersions(context.Background(), cmd)

	if len(deps.publisher.comparisonCompleted) != 0 {
		t.Error("no ComparisonCompletedEvent should be published on error")
	}
}

// --- Tests: Input Validation ---

func TestValidateCompareCommand_EmptyFields(t *testing.T) {
	tests := []struct {
		name string
		cmd  model.CompareVersionsCommand
	}{
		{
			name: "empty_job_id",
			cmd:  model.CompareVersionsCommand{DocumentID: "d", BaseVersionID: "v1", TargetVersionID: "v2"},
		},
		{
			name: "empty_document_id",
			cmd:  model.CompareVersionsCommand{JobID: "j", BaseVersionID: "v1", TargetVersionID: "v2"},
		},
		{
			name: "empty_base_version_id",
			cmd:  model.CompareVersionsCommand{JobID: "j", DocumentID: "d", TargetVersionID: "v2"},
		},
		{
			name: "empty_target_version_id",
			cmd:  model.CompareVersionsCommand{JobID: "j", DocumentID: "d", BaseVersionID: "v1"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateCompareCommand(tt.cmd)
			if err == nil {
				t.Error("expected validation error for empty field")
			}
			if port.ErrorCode(err) != port.ErrCodeValidation {
				t.Errorf("expected VALIDATION_ERROR code, got %s", port.ErrorCode(err))
			}
		})
	}
}

func TestValidateCompareCommand_SameVersionIDs(t *testing.T) {
	cmd := model.CompareVersionsCommand{
		JobID:           "j",
		DocumentID:      "d",
		BaseVersionID:   "v1",
		TargetVersionID: "v1",
	}
	err := validateCompareCommand(cmd)
	if err == nil {
		t.Error("expected validation error for same version IDs")
	}
	if port.ErrorCode(err) != port.ErrCodeValidation {
		t.Errorf("expected VALIDATION_ERROR code, got %s", port.ErrorCode(err))
	}
}

func TestValidateCompareCommand_Valid(t *testing.T) {
	cmd := defaultCompareCmd()
	if err := validateCompareCommand(cmd); err != nil {
		t.Errorf("expected no error for valid command, got: %v", err)
	}
}

func TestHandleCompareVersions_ValidationRejectsEmptyFields(t *testing.T) {
	deps := newTestDeps()
	orch := deps.build()

	// Command with empty BaseVersionID should be rejected.
	cmd := model.CompareVersionsCommand{
		JobID:           "job-val-1",
		DocumentID:      "doc-1",
		BaseVersionID:   "",
		TargetVersionID: "v2",
	}
	err := orch.HandleCompareVersions(context.Background(), cmd)
	if err == nil {
		t.Fatal("expected validation error, got nil")
	}

	// Should be classified as REJECTED.
	if len(deps.publisher.comparisonFailed) != 1 {
		t.Fatalf("expected 1 ComparisonFailedEvent, got %d", len(deps.publisher.comparisonFailed))
	}
	if deps.publisher.comparisonFailed[0].Status != model.StatusRejected {
		t.Errorf("expected REJECTED, got %s", deps.publisher.comparisonFailed[0].Status)
	}
}

func TestHandleCompareVersions_ValidationRejectsSameVersions(t *testing.T) {
	deps := newTestDeps()
	orch := deps.build()

	cmd := model.CompareVersionsCommand{
		JobID:           "job-val-2",
		DocumentID:      "doc-1",
		BaseVersionID:   "v1",
		TargetVersionID: "v1",
	}
	err := orch.HandleCompareVersions(context.Background(), cmd)
	if err == nil {
		t.Fatal("expected validation error, got nil")
	}

	if len(deps.publisher.comparisonFailed) != 1 {
		t.Fatalf("expected 1 ComparisonFailedEvent, got %d", len(deps.publisher.comparisonFailed))
	}
	if deps.publisher.comparisonFailed[0].Status != model.StatusRejected {
		t.Errorf("expected REJECTED, got %s", deps.publisher.comparisonFailed[0].Status)
	}
}

// --- Tests: Terminal status guard in handlePipelineError ---

func TestHandlePipelineError_SkipsTransitionWhenAlreadyTerminal(t *testing.T) {
	// Simulate: job reaches COMPLETED, then PublishComparisonCompleted fails.
	// handlePipelineError should NOT attempt to transition COMPLETED→FAILED.
	deps := newTestDeps()
	deps.publisher.completedErr = errors.New("publish failed")

	orch := deps.build()
	cmd := defaultCompareCmd()

	err := orch.HandleCompareVersions(context.Background(), cmd)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	// The job should have reached COMPLETED (2 status changes: QUEUED→IN_PROGRESS, IN_PROGRESS→COMPLETED).
	// handlePipelineError should skip the COMPLETED→FAILED transition.
	// So we should still see only 2 status changed events (not 3).
	if len(deps.publisher.statusChanged) != 2 {
		t.Errorf("expected 2 StatusChangedEvents (no extra FAILED transition), got %d", len(deps.publisher.statusChanged))
	}

	// The last status should be COMPLETED, not FAILED.
	last := deps.publisher.statusChanged[len(deps.publisher.statusChanged)-1]
	if last.NewStatus != model.StatusCompleted {
		t.Errorf("expected last transition to COMPLETED, got %s", last.NewStatus)
	}
}

// --- Helper types: TASK-038 ---

// blockingRegistry embeds mockRegistry but overrides AwaitAll to block until
// the context is cancelled, simulating a real timeout scenario.
type blockingRegistry struct {
	*mockRegistry
}

func (m *blockingRegistry) AwaitAll(ctx context.Context, jobID string) ([]port.PendingResponse, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

// cleanupRecorder records cleanup invocations in a thread-safe manner.
type cleanupRecorder struct {
	mu     sync.Mutex
	called bool
	jobID  string
}

func (r *cleanupRecorder) cleanup(_ context.Context, jobID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.called = true
	r.jobID = jobID
	return nil
}

func (r *cleanupRecorder) wasCalled() (bool, string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.called, r.jobID
}

// --- Tests: TASK-038 Error Handling and Timeouts ---

func TestError_DiffPersistFailed_RetryablePassthrough(t *testing.T) {
	deps := newTestDeps()
	cmd := defaultCompareCmd()
	baseCorrID := fmt.Sprintf("%s:base:%s", cmd.JobID, cmd.BaseVersionID)
	targetCorrID := fmt.Sprintf("%s:target:%s", cmd.JobID, cmd.TargetVersionID)
	confirmCorrID := cmd.JobID + ":diff-confirm"

	// First AwaitAll: trees returned successfully.
	// Second AwaitAll: confirmation returns DiffPersistFailed with retryable=true.
	deps.registry.awaitResponses = [][]port.PendingResponse{
		{
			{CorrelationID: baseCorrID, Tree: defaultBaseTree()},
			{CorrelationID: targetCorrID, Tree: defaultTargetTree()},
		},
		{
			{CorrelationID: confirmCorrID, Err: port.NewDMDiffPersistFailedError("DM storage failed", true, nil)},
		},
	}
	deps.registry.awaitCallCount = 0

	orch := deps.build()
	err := orch.HandleCompareVersions(context.Background(), cmd)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	// Verify ComparisonFailedEvent.
	if len(deps.publisher.comparisonFailed) != 1 {
		t.Fatalf("expected 1 ComparisonFailedEvent, got %d", len(deps.publisher.comparisonFailed))
	}
	failed := deps.publisher.comparisonFailed[0]
	if failed.Status != model.StatusFailed {
		t.Errorf("expected FAILED, got %s", failed.Status)
	}
	if !failed.IsRetryable {
		t.Error("expected IsRetryable=true for retryable DiffPersistFailed")
	}
	if failed.FailedAtStage != string(model.ComparisonStageWaitingConfirm) {
		t.Errorf("expected FailedAtStage=%s, got %s", model.ComparisonStageWaitingConfirm, failed.FailedAtStage)
	}
}

func TestError_DiffPersistFailed_NonRetryablePassthrough(t *testing.T) {
	deps := newTestDeps()
	cmd := defaultCompareCmd()
	baseCorrID := fmt.Sprintf("%s:base:%s", cmd.JobID, cmd.BaseVersionID)
	targetCorrID := fmt.Sprintf("%s:target:%s", cmd.JobID, cmd.TargetVersionID)
	confirmCorrID := cmd.JobID + ":diff-confirm"

	// First AwaitAll: trees returned successfully.
	// Second AwaitAll: confirmation returns DiffPersistFailed with retryable=false.
	deps.registry.awaitResponses = [][]port.PendingResponse{
		{
			{CorrelationID: baseCorrID, Tree: defaultBaseTree()},
			{CorrelationID: targetCorrID, Tree: defaultTargetTree()},
		},
		{
			{CorrelationID: confirmCorrID, Err: port.NewDMDiffPersistFailedError("permanent DM failure", false, nil)},
		},
	}
	deps.registry.awaitCallCount = 0

	orch := deps.build()
	err := orch.HandleCompareVersions(context.Background(), cmd)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	// Verify ComparisonFailedEvent.
	if len(deps.publisher.comparisonFailed) != 1 {
		t.Fatalf("expected 1 ComparisonFailedEvent, got %d", len(deps.publisher.comparisonFailed))
	}
	failed := deps.publisher.comparisonFailed[0]
	if failed.Status != model.StatusFailed {
		t.Errorf("expected FAILED, got %s", failed.Status)
	}
	if failed.IsRetryable {
		t.Error("expected IsRetryable=false for non-retryable DiffPersistFailed")
	}
}

func TestError_BrokerError_RetryableAfterExhaustion(t *testing.T) {
	deps := newTestDeps()
	deps.treeReq.err = port.NewBrokerError("broker down", errors.New("err"))

	orch := deps.buildWithRetry(2, time.Millisecond)
	cmd := defaultCompareCmd()

	err := orch.HandleCompareVersions(context.Background(), cmd)
	if err == nil {
		t.Fatal("expected error after retry exhaustion, got nil")
	}

	// Verify ComparisonFailedEvent.
	if len(deps.publisher.comparisonFailed) != 1 {
		t.Fatalf("expected 1 ComparisonFailedEvent, got %d", len(deps.publisher.comparisonFailed))
	}
	failed := deps.publisher.comparisonFailed[0]
	if failed.Status != model.StatusFailed {
		t.Errorf("expected FAILED, got %s", failed.Status)
	}
	if !failed.IsRetryable {
		t.Error("expected IsRetryable=true for retryable BrokerError after exhaustion")
	}

	// Verify the tree requester was called exactly 2 times (2 retries exhausted).
	deps.treeReq.mu.Lock()
	callCount := deps.treeReq.callCount
	deps.treeReq.mu.Unlock()

	if callCount != 2 {
		t.Errorf("expected 2 calls (maxRetries=2), got %d", callCount)
	}

	var domErr *port.DomainError
	if !errors.As(err, &domErr) {
		t.Fatalf("expected DomainError, got %T: %v", err, err)
	}
	if domErr.Code != port.ErrCodeBrokerFailed {
		t.Errorf("expected error code %s, got %s", port.ErrCodeBrokerFailed, domErr.Code)
	}
}

func TestError_JobContextTimeout(t *testing.T) {
	deps := newTestDeps()

	// Use a blocking registry that blocks until context cancellation.
	blocking := &blockingRegistry{mockRegistry: deps.registry}

	// Create lifecycle manager with very short timeout (5ms).
	lm := lifecycle.NewLifecycleManager(deps.publisher, deps.idempotency, 5*time.Millisecond, nil, nopLogger())
	orch := NewOrchestrator(
		lm,
		deps.treeReq,
		deps.dmSender,
		blocking,
		deps.comparer,
		deps.publisher,
		&mockDLQ{},
		nopLogger(),
		1,
		time.Millisecond,
	)
	cmd := defaultCompareCmd()

	err := orch.HandleCompareVersions(context.Background(), cmd)
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}

	// Verify ComparisonFailedEvent with TIMED_OUT.
	if len(deps.publisher.comparisonFailed) != 1 {
		t.Fatalf("expected 1 ComparisonFailedEvent, got %d", len(deps.publisher.comparisonFailed))
	}
	failed := deps.publisher.comparisonFailed[0]
	if failed.Status != model.StatusTimedOut {
		t.Errorf("expected TIMED_OUT, got %s", failed.Status)
	}
	if !failed.IsRetryable {
		t.Error("expected IsRetryable=true for TIMED_OUT")
	}
}

func TestError_CleanupCalledOnFailure(t *testing.T) {
	deps := newTestDeps()
	deps.comparer.result = nil
	deps.comparer.err = port.NewExtractionError("comparison failed", errors.New("tree mismatch"))

	recorder := &cleanupRecorder{}
	lm := lifecycle.NewLifecycleManager(deps.publisher, deps.idempotency, 120*time.Second, recorder.cleanup, nopLogger())
	orch := NewOrchestrator(
		lm,
		deps.treeReq,
		deps.dmSender,
		deps.registry,
		deps.comparer,
		deps.publisher,
		&mockDLQ{},
		nopLogger(),
		1,
		time.Millisecond,
	)
	cmd := defaultCompareCmd()

	err := orch.HandleCompareVersions(context.Background(), cmd)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	// Verify cleanup was called with the correct jobID.
	called, jobID := recorder.wasCalled()
	if !called {
		t.Error("expected cleanup to be called on pipeline failure")
	}
	if jobID != cmd.JobID {
		t.Errorf("expected cleanup jobID=%s, got %s", cmd.JobID, jobID)
	}
}

func TestError_CleanupCalledOnTimeout(t *testing.T) {
	deps := newTestDeps()

	blocking := &blockingRegistry{mockRegistry: deps.registry}

	recorder := &cleanupRecorder{}
	// Short timeout to trigger TIMED_OUT.
	lm := lifecycle.NewLifecycleManager(deps.publisher, deps.idempotency, 5*time.Millisecond, recorder.cleanup, nopLogger())
	orch := NewOrchestrator(
		lm,
		deps.treeReq,
		deps.dmSender,
		blocking,
		deps.comparer,
		deps.publisher,
		&mockDLQ{},
		nopLogger(),
		1,
		time.Millisecond,
	)
	cmd := defaultCompareCmd()

	err := orch.HandleCompareVersions(context.Background(), cmd)
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}

	// Verify cleanup was called.
	called, jobID := recorder.wasCalled()
	if !called {
		t.Error("expected cleanup to be called on timeout")
	}
	if jobID != cmd.JobID {
		t.Errorf("expected cleanup jobID=%s, got %s", cmd.JobID, jobID)
	}
}

func TestError_FailedEventFieldsForDiffPersistFailed(t *testing.T) {
	deps := newTestDeps()
	cmd := defaultCompareCmd()
	baseCorrID := fmt.Sprintf("%s:base:%s", cmd.JobID, cmd.BaseVersionID)
	targetCorrID := fmt.Sprintf("%s:target:%s", cmd.JobID, cmd.TargetVersionID)
	confirmCorrID := cmd.JobID + ":diff-confirm"

	// First AwaitAll: trees returned successfully.
	// Second AwaitAll: confirmation returns DiffPersistFailed.
	deps.registry.awaitResponses = [][]port.PendingResponse{
		{
			{CorrelationID: baseCorrID, Tree: defaultBaseTree()},
			{CorrelationID: targetCorrID, Tree: defaultTargetTree()},
		},
		{
			{CorrelationID: confirmCorrID, Err: port.NewDMDiffPersistFailedError("DM persist error", true, nil)},
		},
	}
	deps.registry.awaitCallCount = 0

	orch := deps.build()

	before := time.Now().UTC()
	err := orch.HandleCompareVersions(context.Background(), cmd)
	after := time.Now().UTC()

	if err == nil {
		t.Fatal("expected error, got nil")
	}

	if len(deps.publisher.comparisonFailed) != 1 {
		t.Fatalf("expected 1 ComparisonFailedEvent, got %d", len(deps.publisher.comparisonFailed))
	}
	f := deps.publisher.comparisonFailed[0]

	// 1. CorrelationID
	if f.CorrelationID != cmd.JobID {
		t.Errorf("CorrelationID: expected %s, got %s", cmd.JobID, f.CorrelationID)
	}
	// 2. Timestamp
	if f.Timestamp.Before(before) || f.Timestamp.After(after) {
		t.Errorf("Timestamp %v outside range [%v, %v]", f.Timestamp, before, after)
	}
	// 3. JobID
	if f.JobID != cmd.JobID {
		t.Errorf("JobID: expected %s, got %s", cmd.JobID, f.JobID)
	}
	// 4. DocumentID
	if f.DocumentID != cmd.DocumentID {
		t.Errorf("DocumentID: expected %s, got %s", cmd.DocumentID, f.DocumentID)
	}
	// 5. Status
	if f.Status != model.StatusFailed {
		t.Errorf("Status: expected FAILED, got %s", f.Status)
	}
	// 6. ErrorCode
	if f.ErrorCode != port.ErrCodeDMDiffPersistFailed {
		t.Errorf("ErrorCode: expected %s, got %s", port.ErrCodeDMDiffPersistFailed, f.ErrorCode)
	}
	// 7. ErrorMessage
	if f.ErrorMessage == "" {
		t.Error("ErrorMessage should not be empty")
	}
	// 8. FailedAtStage
	if f.FailedAtStage != string(model.ComparisonStageWaitingConfirm) {
		t.Errorf("FailedAtStage: expected %s, got %s", model.ComparisonStageWaitingConfirm, f.FailedAtStage)
	}
	// 9. IsRetryable
	if !f.IsRetryable {
		t.Error("expected IsRetryable=true for retryable DiffPersistFailed")
	}
}

// --- Tests: DLQ ---

func TestDLQ_SentOnFailed(t *testing.T) {
	// Trigger FAILED by using a retryable broker error from tree requester
	// that exhausts retries (maxRetries=1).
	deps := newTestDeps()
	deps.treeReq.err = port.NewBrokerError("broker down", errors.New("connection refused"))

	orch := deps.buildWithRetry(1, time.Millisecond)
	cmd := defaultCompareCmd()

	err := orch.HandleCompareVersions(context.Background(), cmd)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	// Verify DLQ message was sent.
	deps.dlq.mu.Lock()
	dlqMessages := make([]model.DLQMessage, len(deps.dlq.messages))
	copy(dlqMessages, deps.dlq.messages)
	deps.dlq.mu.Unlock()

	if len(dlqMessages) != 1 {
		t.Fatalf("expected 1 DLQ message, got %d", len(dlqMessages))
	}

	dlqMsg := dlqMessages[0]

	// Verify DLQ message fields.
	if dlqMsg.JobID != "job-cmp-1" {
		t.Errorf("DLQ JobID: expected job-cmp-1, got %s", dlqMsg.JobID)
	}
	if dlqMsg.DocumentID != "doc-1" {
		t.Errorf("DLQ DocumentID: expected doc-1, got %s", dlqMsg.DocumentID)
	}
	if dlqMsg.ErrorCode != port.ErrCodeBrokerFailed {
		t.Errorf("DLQ ErrorCode: expected %s, got %s", port.ErrCodeBrokerFailed, dlqMsg.ErrorCode)
	}
	if dlqMsg.FailedAtStage == "" {
		t.Error("DLQ FailedAtStage should not be empty")
	}
	if dlqMsg.PipelineType != "comparison" {
		t.Errorf("DLQ PipelineType: expected comparison, got %s", dlqMsg.PipelineType)
	}

	// Verify OriginalCommand contains the command JSON.
	if len(dlqMsg.OriginalCommand) == 0 {
		t.Error("DLQ OriginalCommand should not be empty")
	}
	var cmdFromDLQ map[string]interface{}
	if err := json.Unmarshal(dlqMsg.OriginalCommand, &cmdFromDLQ); err != nil {
		t.Fatalf("failed to unmarshal DLQ OriginalCommand: %v", err)
	}
	if cmdFromDLQ["job_id"] != "job-cmp-1" {
		t.Errorf("DLQ OriginalCommand job_id: expected job-cmp-1, got %v", cmdFromDLQ["job_id"])
	}
}

func TestDLQ_NotSentOnRejected(t *testing.T) {
	// Trigger REJECTED using same base_version_id and target_version_id
	// (validation error, non-retryable).
	deps := newTestDeps()
	orch := deps.build()

	cmd := model.CompareVersionsCommand{
		JobID:           "job-rej-1",
		DocumentID:      "doc-1",
		BaseVersionID:   "v1",
		TargetVersionID: "v1", // same as base -> REJECTED
		OrgID:           "org-1",
		UserID:          "user-1",
	}

	_ = orch.HandleCompareVersions(context.Background(), cmd)

	// Verify no DLQ message was sent.
	deps.dlq.mu.Lock()
	dlqCount := len(deps.dlq.messages)
	deps.dlq.mu.Unlock()

	if dlqCount != 0 {
		t.Errorf("expected 0 DLQ messages for REJECTED, got %d", dlqCount)
	}
}

func TestDLQ_NotSentOnTimedOut(t *testing.T) {
	// Trigger TIMED_OUT using context.DeadlineExceeded from registry.
	deps := newTestDeps()
	deps.registry.awaitErr = context.DeadlineExceeded

	orch := deps.build()
	cmd := defaultCompareCmd()

	_ = orch.HandleCompareVersions(context.Background(), cmd)

	// Verify no DLQ message was sent.
	deps.dlq.mu.Lock()
	dlqCount := len(deps.dlq.messages)
	deps.dlq.mu.Unlock()

	if dlqCount != 0 {
		t.Errorf("expected 0 DLQ messages for TIMED_OUT, got %d", dlqCount)
	}
}

func TestDLQ_ErrorIsLogged_NotPropagated(t *testing.T) {
	// Set mockDLQ to return an error, then trigger a FAILED pipeline error.
	// Verify that HandleCompareVersions returns the original pipeline error,
	// not the DLQ error.
	deps := newTestDeps()
	deps.dlq.err = errors.New("DLQ broker down")
	deps.treeReq.err = port.NewBrokerError("broker down", errors.New("connection refused"))

	orch := deps.buildWithRetry(1, time.Millisecond)
	cmd := defaultCompareCmd()

	err := orch.HandleCompareVersions(context.Background(), cmd)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	// The returned error should be the original pipeline error, not the DLQ error.
	var domErr *port.DomainError
	if !errors.As(err, &domErr) {
		t.Fatalf("expected DomainError, got %T: %v", err, err)
	}
	if domErr.Code != port.ErrCodeBrokerFailed {
		t.Errorf("expected original error code %s, got %s", port.ErrCodeBrokerFailed, domErr.Code)
	}

	// Verify the error is NOT the DLQ error.
	if err.Error() == "DLQ broker down" {
		t.Error("returned error should be the pipeline error, not the DLQ error")
	}
}
