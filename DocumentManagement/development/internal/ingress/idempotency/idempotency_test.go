package idempotency

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"contractpro/document-management/internal/config"
	"contractpro/document-management/internal/domain/model"
	"contractpro/document-management/internal/domain/port"
)

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

type mockStore struct {
	mu      sync.Mutex
	records map[string]*storeEntry
	getErr  error
	setErr  error
	setNXErr error
	delErr  error
}

type storeEntry struct {
	record *model.IdempotencyRecord
	ttl    time.Duration
}

func newMockStore() *mockStore {
	return &mockStore{records: make(map[string]*storeEntry)}
}

func (m *mockStore) Get(_ context.Context, key string) (*model.IdempotencyRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.getErr != nil {
		return nil, m.getErr
	}
	e, ok := m.records[key]
	if !ok {
		return nil, nil
	}
	cp := *e.record
	return &cp, nil
}

func (m *mockStore) Set(_ context.Context, record *model.IdempotencyRecord, ttl time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.setErr != nil {
		return m.setErr
	}
	cp := *record
	m.records[record.Key] = &storeEntry{record: &cp, ttl: ttl}
	return nil
}

func (m *mockStore) SetNX(_ context.Context, record *model.IdempotencyRecord, ttl time.Duration) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.setNXErr != nil {
		return false, m.setNXErr
	}
	if _, exists := m.records[record.Key]; exists {
		return false, nil
	}
	cp := *record
	m.records[record.Key] = &storeEntry{record: &cp, ttl: ttl}
	return true, nil
}

func (m *mockStore) Delete(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.delErr != nil {
		return m.delErr
	}
	delete(m.records, key)
	return nil
}

func (m *mockStore) getRecord(key string) *model.IdempotencyRecord {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.records[key]
	if !ok {
		return nil
	}
	return e.record
}

func (m *mockStore) getTTL(key string) time.Duration {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.records[key]
	if !ok {
		return 0
	}
	return e.ttl
}

// seedRecord injects a record into the store (for test setup).
func (m *mockStore) seedRecord(key string, rec *model.IdempotencyRecord, ttl time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.records[key] = &storeEntry{record: rec, ttl: ttl}
}

type mockMetrics struct {
	mu            sync.Mutex
	fallbackCalls map[string]int
	checkCalls    map[string]int
}

func newMockMetrics() *mockMetrics {
	return &mockMetrics{
		fallbackCalls: make(map[string]int),
		checkCalls:    make(map[string]int),
	}
}

func (m *mockMetrics) IncFallbackTotal(topic string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.fallbackCalls[topic]++
}

func (m *mockMetrics) IncCheckTotal(result string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.checkCalls[result]++
}

func (m *mockMetrics) getFallbackCount(topic string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.fallbackCalls[topic]
}

func (m *mockMetrics) getCheckCount(result string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.checkCalls[result]
}

type mockLogger struct {
	mu   sync.Mutex
	logs []string
}

func newMockLogger() *mockLogger { return &mockLogger{} }

func (m *mockLogger) Warn(msg string, _ ...any) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.logs = append(m.logs, "WARN: "+msg)
}

func (m *mockLogger) Info(msg string, _ ...any) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.logs = append(m.logs, "INFO: "+msg)
}

func (m *mockLogger) hasLog(substr string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, l := range m.logs {
		if strings.Contains(l, substr) {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func defaultCfg() config.IdempotencyConfig {
	return config.IdempotencyConfig{
		TTL:            24 * time.Hour,
		ProcessingTTL:  120 * time.Second,
		StuckThreshold: 240 * time.Second,
	}
}

func newGuard(store port.IdempotencyStorePort, metrics MetricsCollector, logger Logger) *IdempotencyGuard {
	return NewIdempotencyGuard(store, defaultCfg(), metrics, logger)
}

// ---------------------------------------------------------------------------
// Constructor Tests
// ---------------------------------------------------------------------------

func TestNewIdempotencyGuard_PanicOnNilStore(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic on nil store")
		}
	}()
	NewIdempotencyGuard(nil, defaultCfg(), newMockMetrics(), newMockLogger())
}

func TestNewIdempotencyGuard_PanicOnNilMetrics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic on nil metrics")
		}
	}()
	NewIdempotencyGuard(newMockStore(), defaultCfg(), nil, newMockLogger())
}

func TestNewIdempotencyGuard_PanicOnNilLogger(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic on nil logger")
		}
	}()
	NewIdempotencyGuard(newMockStore(), defaultCfg(), newMockMetrics(), nil)
}

func TestNewIdempotencyGuard_Success(t *testing.T) {
	g := NewIdempotencyGuard(newMockStore(), defaultCfg(), newMockMetrics(), newMockLogger())
	if g == nil {
		t.Error("expected non-nil guard")
	}
}

// ---------------------------------------------------------------------------
// Check: Key Not Found — atomic SETNX claims PROCESSING
// ---------------------------------------------------------------------------

func TestCheck_KeyNotFound_AtomicClaimAndReturnsProcess(t *testing.T) {
	store := newMockStore()
	metrics := newMockMetrics()
	g := newGuard(store, metrics, newMockLogger())

	key := "dm:idem:dp-art:job-123"
	result, err := g.Check(context.Background(), key, model.TopicDPArtifactsProcessingReady, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != ResultProcess {
		t.Errorf("result = %v, want ResultProcess", result)
	}

	rec := store.getRecord(key)
	if rec == nil {
		t.Fatal("expected PROCESSING record in store")
	}
	if rec.Status != model.IdempotencyStatusProcessing {
		t.Errorf("status = %s, want PROCESSING", rec.Status)
	}
	if store.getTTL(key) != 120*time.Second {
		t.Errorf("TTL = %v, want 120s", store.getTTL(key))
	}
	if metrics.getCheckCount("process") != 1 {
		t.Errorf("check_total(process) = %d, want 1", metrics.getCheckCount("process"))
	}
}

// ---------------------------------------------------------------------------
// Check: SETNX fails (key already claimed) + existing COMPLETED
// ---------------------------------------------------------------------------

func TestCheck_Completed_ReturnsSkip(t *testing.T) {
	store := newMockStore()
	metrics := newMockMetrics()
	g := newGuard(store, metrics, newMockLogger())

	key := "dm:idem:dp-art:job-456"
	rec := model.NewIdempotencyRecord(key)
	rec.MarkCompleted("persisted")
	store.seedRecord(key, rec, 24*time.Hour)

	result, err := g.Check(context.Background(), key, model.TopicDPArtifactsProcessingReady, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != ResultSkip {
		t.Errorf("result = %v, want ResultSkip", result)
	}
	if metrics.getCheckCount("skip") != 1 {
		t.Errorf("check_total(skip) = %d, want 1", metrics.getCheckCount("skip"))
	}
}

// ---------------------------------------------------------------------------
// Check: SETNX fails + existing PROCESSING (fresh — another worker)
// ---------------------------------------------------------------------------

func TestCheck_ProcessingFresh_ReturnsSkip(t *testing.T) {
	store := newMockStore()
	metrics := newMockMetrics()
	g := newGuard(store, metrics, newMockLogger())

	key := "dm:idem:dp-art:job-789"
	rec := model.NewIdempotencyRecord(key)
	store.seedRecord(key, rec, 120*time.Second)

	result, err := g.Check(context.Background(), key, model.TopicDPArtifactsProcessingReady, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != ResultSkip {
		t.Errorf("result = %v, want ResultSkip", result)
	}
}

// ---------------------------------------------------------------------------
// Check: PROCESSING (stuck — age >= threshold) → overwrite → ResultReprocess
// ---------------------------------------------------------------------------

func TestCheck_ProcessingStuck_ReturnsReprocess(t *testing.T) {
	store := newMockStore()
	metrics := newMockMetrics()
	logger := newMockLogger()
	g := newGuard(store, metrics, logger)

	key := "dm:idem:dp-art:job-stuck"
	rec := model.NewIdempotencyRecord(key)
	rec.CreatedAt = time.Now().UTC().Add(-5 * time.Minute)
	store.seedRecord(key, rec, 120*time.Second)

	result, err := g.Check(context.Background(), key, model.TopicDPArtifactsProcessingReady, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != ResultReprocess {
		t.Errorf("result = %v, want ResultReprocess", result)
	}

	newRec := store.getRecord(key)
	if newRec == nil {
		t.Fatal("expected new PROCESSING record after stuck overwrite")
	}
	if newRec.Status != model.IdempotencyStatusProcessing {
		t.Errorf("status = %s, want PROCESSING", newRec.Status)
	}
	if metrics.getCheckCount("reprocess") != 1 {
		t.Errorf("check_total(reprocess) = %d, want 1", metrics.getCheckCount("reprocess"))
	}
	if !logger.hasLog("stuck PROCESSING") {
		t.Error("expected WARN log about stuck PROCESSING")
	}
}

// ---------------------------------------------------------------------------
// Check: Concurrent SETNX — second caller gets ResultSkip
// ---------------------------------------------------------------------------

func TestCheck_ConcurrentSETNX_SecondCallerGetsSkip(t *testing.T) {
	store := newMockStore()
	metrics := newMockMetrics()
	g := newGuard(store, metrics, newMockLogger())

	key := "dm:idem:dp-art:job-concurrent"

	// First call claims the key
	r1, err := g.Check(context.Background(), key, model.TopicDPArtifactsProcessingReady, nil)
	if err != nil {
		t.Fatalf("first Check error: %v", err)
	}
	if r1 != ResultProcess {
		t.Errorf("first Check = %v, want ResultProcess", r1)
	}

	// Second call — SETNX fails, sees PROCESSING (fresh) → skip
	r2, err := g.Check(context.Background(), key, model.TopicDPArtifactsProcessingReady, nil)
	if err != nil {
		t.Fatalf("second Check error: %v", err)
	}
	if r2 != ResultSkip {
		t.Errorf("second Check = %v, want ResultSkip", r2)
	}
}

// ---------------------------------------------------------------------------
// Check: Redis failure — fallback with nil checker (always process)
// ---------------------------------------------------------------------------

func TestCheck_RedisDown_NilFallback_ReturnsProcess(t *testing.T) {
	store := newMockStore()
	store.setNXErr = port.NewStorageError("redis down", errors.New("connection refused"))
	metrics := newMockMetrics()
	logger := newMockLogger()
	g := newGuard(store, metrics, logger)

	key := "dm:idem:dp-tree:job-123:ver-456"
	topic := model.TopicDPRequestsSemanticTree
	result, err := g.Check(context.Background(), key, topic, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != ResultProcess {
		t.Errorf("result = %v, want ResultProcess", result)
	}
	if metrics.getFallbackCount(topic) != 1 {
		t.Errorf("fallback_total(%s) = %d, want 1", topic, metrics.getFallbackCount(topic))
	}
	if metrics.getCheckCount("fallback_process") != 1 {
		t.Errorf("check_total(fallback_process) = %d, want 1", metrics.getCheckCount("fallback_process"))
	}
	if !logger.hasLog("redis unavailable") {
		t.Error("expected WARN log about redis unavailable")
	}
}

// ---------------------------------------------------------------------------
// Check: Redis failure — fallback returns alreadyProcessed=true
// ---------------------------------------------------------------------------

func TestCheck_RedisDown_FallbackAlreadyProcessed_ReturnsSkip(t *testing.T) {
	store := newMockStore()
	store.setNXErr = port.NewStorageError("redis down", errors.New("timeout"))
	metrics := newMockMetrics()
	g := newGuard(store, metrics, newMockLogger())

	fallback := func(_ context.Context) (bool, error) {
		return true, nil
	}

	key := "dm:idem:dp-art:job-dup"
	topic := model.TopicDPArtifactsProcessingReady
	result, err := g.Check(context.Background(), key, topic, fallback)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != ResultSkip {
		t.Errorf("result = %v, want ResultSkip", result)
	}
	if metrics.getCheckCount("fallback_skip") != 1 {
		t.Errorf("check_total(fallback_skip) = %d, want 1", metrics.getCheckCount("fallback_skip"))
	}
}

// ---------------------------------------------------------------------------
// Check: Redis failure — fallback returns not processed
// ---------------------------------------------------------------------------

func TestCheck_RedisDown_FallbackNotProcessed_ReturnsProcess(t *testing.T) {
	store := newMockStore()
	store.setNXErr = port.NewStorageError("redis down", errors.New("connection refused"))
	metrics := newMockMetrics()
	g := newGuard(store, metrics, newMockLogger())

	fallback := func(_ context.Context) (bool, error) {
		return false, nil
	}

	key := "dm:idem:dp-art:job-new"
	topic := model.TopicDPArtifactsProcessingReady
	result, err := g.Check(context.Background(), key, topic, fallback)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != ResultProcess {
		t.Errorf("result = %v, want ResultProcess", result)
	}
	if metrics.getCheckCount("fallback_process") != 1 {
		t.Errorf("check_total(fallback_process) = %d, want 1", metrics.getCheckCount("fallback_process"))
	}
}

// ---------------------------------------------------------------------------
// Check: Redis failure — fallback also fails
// ---------------------------------------------------------------------------

func TestCheck_RedisDown_FallbackError_ReturnsProcess(t *testing.T) {
	store := newMockStore()
	store.setNXErr = port.NewStorageError("redis down", errors.New("timeout"))
	metrics := newMockMetrics()
	logger := newMockLogger()
	g := newGuard(store, metrics, logger)

	fallback := func(_ context.Context) (bool, error) {
		return false, errors.New("db connection failed")
	}

	key := "dm:idem:dp-art:job-fail"
	topic := model.TopicDPArtifactsProcessingReady
	result, err := g.Check(context.Background(), key, topic, fallback)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != ResultProcess {
		t.Errorf("result = %v, want ResultProcess", result)
	}
	if !logger.hasLog("DB fallback check failed") {
		t.Error("expected WARN log about DB fallback failure")
	}
}

// ---------------------------------------------------------------------------
// Check: Redis GET fails after SETNX succeeded — fallback
// ---------------------------------------------------------------------------

func TestCheck_GetFailsAfterSETNXFalse_FallsBack(t *testing.T) {
	store := newMockStore()
	metrics := newMockMetrics()
	g := newGuard(store, metrics, newMockLogger())

	key := "dm:idem:dp-art:job-get-fail"
	// Seed a record so SETNX returns false
	rec := model.NewIdempotencyRecord(key)
	rec.MarkCompleted("")
	store.seedRecord(key, rec, 24*time.Hour)

	// Now make GET fail
	store.getErr = port.NewStorageError("redis read error", errors.New("timeout"))

	topic := model.TopicDPArtifactsProcessingReady
	result, err := g.Check(context.Background(), key, topic, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != ResultProcess {
		t.Errorf("result = %v, want ResultProcess (fallback on GET error)", result)
	}
	if metrics.getFallbackCount(topic) != 1 {
		t.Error("expected fallback metric to be incremented")
	}
}

// ---------------------------------------------------------------------------
// Check: Stuck overwrite fails (best-effort)
// ---------------------------------------------------------------------------

func TestCheck_StuckOverwriteFails_StillReturnsReprocess(t *testing.T) {
	store := newMockStore()
	store.setErr = port.NewStorageError("set failed", errors.New("timeout"))
	metrics := newMockMetrics()
	logger := newMockLogger()
	g := newGuard(store, metrics, logger)

	key := "dm:idem:dp-art:job-stuck-set-fail"
	rec := model.NewIdempotencyRecord(key)
	rec.CreatedAt = time.Now().UTC().Add(-10 * time.Minute)
	store.seedRecord(key, rec, 120*time.Second)

	result, err := g.Check(context.Background(), key, model.TopicDPArtifactsProcessingReady, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != ResultReprocess {
		t.Errorf("result = %v, want ResultReprocess", result)
	}
	if !logger.hasLog("failed to overwrite stuck") {
		t.Error("expected WARN log about failed overwrite")
	}
}

// ---------------------------------------------------------------------------
// Check: Context cancelled → error returned
// ---------------------------------------------------------------------------

func TestCheck_ContextCancelled_ReturnsError(t *testing.T) {
	store := newMockStore()
	g := newGuard(store, newMockMetrics(), newMockLogger())

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result, err := g.Check(ctx, "test-key", model.TopicDPArtifactsProcessingReady, nil)
	if err == nil {
		t.Fatal("expected error on cancelled context")
	}
	if result != ResultSkip {
		t.Errorf("result = %v, want ResultSkip on cancelled context", result)
	}
	if !strings.Contains(err.Error(), "context canceled") {
		t.Errorf("error = %q, want context canceled", err.Error())
	}
}

// ---------------------------------------------------------------------------
// MarkCompleted
// ---------------------------------------------------------------------------

func TestMarkCompleted_Success(t *testing.T) {
	store := newMockStore()
	g := newGuard(store, newMockMetrics(), newMockLogger())

	key := "dm:idem:dp-art:job-complete"
	err := g.MarkCompleted(context.Background(), key)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	rec := store.getRecord(key)
	if rec == nil {
		t.Fatal("expected COMPLETED record in store")
	}
	if rec.Status != model.IdempotencyStatusCompleted {
		t.Errorf("status = %s, want COMPLETED", rec.Status)
	}
	if store.getTTL(key) != 24*time.Hour {
		t.Errorf("TTL = %v, want 24h", store.getTTL(key))
	}
}

func TestMarkCompleted_StoreError_ReturnsError(t *testing.T) {
	store := newMockStore()
	store.setErr = port.NewStorageError("redis failed", errors.New("down"))
	logger := newMockLogger()
	g := newGuard(store, newMockMetrics(), logger)

	key := "dm:idem:dp-art:job-mark-fail"
	err := g.MarkCompleted(context.Background(), key)
	if err == nil {
		t.Error("expected error from MarkCompleted when store fails")
	}
	if !logger.hasLog("failed to mark idempotency record as COMPLETED") {
		t.Error("expected WARN log")
	}
}

// ---------------------------------------------------------------------------
// Cleanup
// ---------------------------------------------------------------------------

func TestCleanup_Success(t *testing.T) {
	store := newMockStore()
	g := newGuard(store, newMockMetrics(), newMockLogger())

	key := "dm:idem:dp-art:job-cleanup"
	rec := model.NewIdempotencyRecord(key)
	store.seedRecord(key, rec, 120*time.Second)

	err := g.Cleanup(context.Background(), key)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if store.getRecord(key) != nil {
		t.Error("expected record to be deleted")
	}
}

func TestCleanup_StoreError_ReturnsError(t *testing.T) {
	store := newMockStore()
	store.delErr = port.NewStorageError("redis failed", errors.New("down"))
	logger := newMockLogger()
	g := newGuard(store, newMockMetrics(), logger)

	key := "dm:idem:dp-art:job-cleanup-fail"
	err := g.Cleanup(context.Background(), key)
	if err == nil {
		t.Error("expected error from Cleanup when store fails")
	}
	if !logger.hasLog("failed to cleanup") {
		t.Error("expected WARN log")
	}
}

// ---------------------------------------------------------------------------
// CheckResult String
// ---------------------------------------------------------------------------

func TestCheckResult_String(t *testing.T) {
	tests := []struct {
		result CheckResult
		want   string
	}{
		{ResultProcess, "process"},
		{ResultSkip, "skip"},
		{ResultReprocess, "reprocess"},
		{CheckResult(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.result.String(); got != tt.want {
			t.Errorf("CheckResult(%d).String() = %q, want %q", tt.result, got, tt.want)
		}
	}
}

// ---------------------------------------------------------------------------
// Full lifecycle: Check → Process → MarkCompleted → Check again → Skip
// ---------------------------------------------------------------------------

func TestFullLifecycle_ProcessThenSkipOnRedelivery(t *testing.T) {
	store := newMockStore()
	metrics := newMockMetrics()
	g := newGuard(store, metrics, newMockLogger())

	key := KeyForDPArtifacts("job-lifecycle-123")

	// First delivery: should process
	result, err := g.Check(context.Background(), key, model.TopicDPArtifactsProcessingReady, nil)
	if err != nil {
		t.Fatalf("first Check error: %v", err)
	}
	if result != ResultProcess {
		t.Errorf("first Check = %v, want ResultProcess", result)
	}

	// Mark completed
	if err := g.MarkCompleted(context.Background(), key); err != nil {
		t.Fatalf("MarkCompleted error: %v", err)
	}

	// Redelivery: should skip
	result, err = g.Check(context.Background(), key, model.TopicDPArtifactsProcessingReady, nil)
	if err != nil {
		t.Fatalf("second Check error: %v", err)
	}
	if result != ResultSkip {
		t.Errorf("second Check = %v, want ResultSkip", result)
	}
}

// ---------------------------------------------------------------------------
// Full lifecycle: Check → Fail → Cleanup → Check again → Process
// ---------------------------------------------------------------------------

func TestFullLifecycle_FailCleanupThenReprocess(t *testing.T) {
	store := newMockStore()
	g := newGuard(store, newMockMetrics(), newMockLogger())

	key := KeyForLICArtifacts("job-fail-lifecycle")

	// First delivery: should process
	result, err := g.Check(context.Background(), key, model.TopicLICArtifactsAnalysisReady, nil)
	if err != nil {
		t.Fatalf("first Check error: %v", err)
	}
	if result != ResultProcess {
		t.Errorf("first Check = %v, want ResultProcess", result)
	}

	// Handler fails → cleanup
	if err := g.Cleanup(context.Background(), key); err != nil {
		t.Fatalf("Cleanup error: %v", err)
	}

	// Redelivery: should process again (key was cleaned up)
	result, err = g.Check(context.Background(), key, model.TopicLICArtifactsAnalysisReady, nil)
	if err != nil {
		t.Fatalf("second Check error: %v", err)
	}
	if result != ResultProcess {
		t.Errorf("second Check = %v, want ResultProcess (after cleanup)", result)
	}
}
