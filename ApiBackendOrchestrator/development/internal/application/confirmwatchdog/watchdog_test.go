package confirmwatchdog

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/redis/go-redis/v9"

	"contractpro/api-orchestrator/internal/application/statustracker"
	"contractpro/api-orchestrator/internal/infra/kvstore"
	"contractpro/api-orchestrator/internal/infra/observability/logger"
)

// --- Mocks ---

type mockTracker struct {
	mu     sync.Mutex
	calls  []timeoutCall
	err    error
	callFn func(orgID, docID, verID string) error
}

type timeoutCall struct {
	OrgID string
	DocID string
	VerID string
}

func (m *mockTracker) TimeoutAwaitingInput(_ context.Context, orgID, docID, verID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, timeoutCall{orgID, docID, verID})
	if m.callFn != nil {
		return m.callFn(orgID, docID, verID)
	}
	return m.err
}

func (m *mockTracker) getCalls() []timeoutCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]timeoutCall, len(m.calls))
	copy(out, m.calls)
	return out
}

type mockKV struct {
	mu      sync.Mutex
	data    map[string]string
	getErr  error
	delErr  error
	deleted []string
}

func newMockKV() *mockKV {
	return &mockKV{data: make(map[string]string)}
}

func (m *mockKV) Get(_ context.Context, key string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.getErr != nil {
		return "", m.getErr
	}
	v, ok := m.data[key]
	if !ok {
		return "", kvstore.ErrKeyNotFound
	}
	return v, nil
}

func (m *mockKV) Delete(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.deleted = append(m.deleted, key)
	delete(m.data, key)
	return m.delErr
}

func (m *mockKV) set(key, value string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data[key] = value
}

func (m *mockKV) getDeleted() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, len(m.deleted))
	copy(out, m.deleted)
	return out
}

// mockRedisSubscriber simulates Redis for the watchdog.
type mockRedisSubscriber struct {
	configSetErr error
	scanKeys     []string
	scanErr      error
	pttlResults  map[string]time.Duration
	pttlErr      error
}

func newMockRedisSubscriber() *mockRedisSubscriber {
	return &mockRedisSubscriber{
		pttlResults: make(map[string]time.Duration),
	}
}

func (m *mockRedisSubscriber) ConfigSet(_ context.Context, _, _ string) *redis.StatusCmd {
	cmd := redis.NewStatusCmd(context.Background())
	if m.configSetErr != nil {
		cmd.SetErr(m.configSetErr)
	} else {
		cmd.SetVal("OK")
	}
	return cmd
}

func (m *mockRedisSubscriber) PSubscribe(ctx context.Context, _ ...string) *redis.PubSub {
	return redis.NewClient(&redis.Options{Addr: "localhost:0"}).PSubscribe(ctx, "__dummy__")
}

func (m *mockRedisSubscriber) Scan(_ context.Context, _ uint64, _ string, _ int64) *redis.ScanCmd {
	cmd := redis.NewScanCmd(context.Background(), nil, "", 0, "", 0)
	if m.scanErr != nil {
		cmd.SetErr(m.scanErr)
	} else {
		cmd.SetVal(m.scanKeys, 0)
	}
	return cmd
}

func (m *mockRedisSubscriber) PTTL(_ context.Context, key string) *redis.DurationCmd {
	cmd := redis.NewDurationCmd(context.Background(), 0)
	if m.pttlErr != nil {
		cmd.SetErr(m.pttlErr)
	} else if ttl, ok := m.pttlResults[key]; ok {
		cmd.SetVal(ttl)
	} else {
		cmd.SetVal(-2) // key not found (expired)
	}
	return cmd
}

// --- Helpers ---

func testLogger() *logger.Logger {
	return logger.NewLogger("error")
}

func newTestCounter() prometheus.Counter {
	return prometheus.NewCounter(prometheus.CounterOpts{
		Name: "test_watchdog_timeouts_total",
		Help: "Test counter.",
	})
}

func seedMeta(kv *mockKV, verID, orgID, docID, jobID string) {
	meta := confirmationMeta{
		OrganizationID: orgID,
		DocumentID:     docID,
		VersionID:      verID,
		JobID:          jobID,
	}
	data, _ := json.Marshal(meta)
	kv.set(statustracker.ConfirmationMetaKey(verID), string(data))
}

// validUUID is a UUID v4 used in tests.
const validUUID = "a1b2c3d4-e5f6-4a7b-8c9d-0e1f2a3b4c5d"

func validKey(verID string) string {
	return confirmationWaitPrefix + verID
}

// --- handleExpiredKey Tests ---

func TestHandleExpiredKey_HappyPath(t *testing.T) {
	tracker := &mockTracker{}
	kv := newMockKV()
	counter := newTestCounter()

	w := NewWatchdog(tracker, kv, newMockRedisSubscriber(), testLogger(), counter, time.Minute)

	seedMeta(kv, validUUID, "org-1", "doc-1", "job-1")
	w.handleExpiredKey(validKey(validUUID))

	calls := tracker.getCalls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	if calls[0].OrgID != "org-1" {
		t.Errorf("expected org_id=org-1, got %s", calls[0].OrgID)
	}
	if calls[0].DocID != "doc-1" {
		t.Errorf("expected doc_id=doc-1, got %s", calls[0].DocID)
	}
	if calls[0].VerID != validUUID {
		t.Errorf("expected ver_id=%s, got %s", validUUID, calls[0].VerID)
	}

	if v := testutil.ToFloat64(counter); v != 1 {
		t.Errorf("expected counter=1, got %f", v)
	}
}

func TestHandleExpiredKey_DeletesMetaAfterSuccess(t *testing.T) {
	tracker := &mockTracker{}
	kv := newMockKV()

	seedMeta(kv, validUUID, "org-1", "doc-1", "job-1")

	w := NewWatchdog(tracker, kv, newMockRedisSubscriber(), testLogger(), newTestCounter(), time.Minute)

	w.handleExpiredKey(validKey(validUUID))

	deleted := kv.getDeleted()
	expectedKey := statustracker.ConfirmationMetaKey(validUUID)
	if len(deleted) != 1 || deleted[0] != expectedKey {
		t.Errorf("expected meta key %q deleted, got %v", expectedKey, deleted)
	}
}

func TestHandleExpiredKey_MetaDeleteFailureNonCritical(t *testing.T) {
	tracker := &mockTracker{}
	kv := newMockKV()
	kv.delErr = errors.New("delete failed")

	seedMeta(kv, validUUID, "org-1", "doc-1", "job-1")
	counter := newTestCounter()

	w := NewWatchdog(tracker, kv, newMockRedisSubscriber(), testLogger(), counter, time.Minute)

	w.handleExpiredKey(validKey(validUUID))

	if v := testutil.ToFloat64(counter); v != 1 {
		t.Errorf("expected counter=1 even with delete failure, got %f", v)
	}
}

func TestHandleExpiredKey_IgnoresNonConfirmationKeys(t *testing.T) {
	tracker := &mockTracker{}
	w := NewWatchdog(tracker, newMockKV(), newMockRedisSubscriber(), testLogger(), newTestCounter(), time.Minute)

	w.handleExpiredKey("some:other:key")
	w.handleExpiredKey("status:org:doc:ver")
	w.handleExpiredKey("")

	if len(tracker.getCalls()) != 0 {
		t.Error("expected no calls for non-confirmation keys")
	}
}

func TestHandleExpiredKey_EmptyVersionID(t *testing.T) {
	tracker := &mockTracker{}
	w := NewWatchdog(tracker, newMockKV(), newMockRedisSubscriber(), testLogger(), newTestCounter(), time.Minute)

	w.handleExpiredKey("confirmation:wait:")

	if len(tracker.getCalls()) != 0 {
		t.Error("expected no calls for empty version_id")
	}
}

func TestHandleExpiredKey_InvalidUUID(t *testing.T) {
	tracker := &mockTracker{}
	w := NewWatchdog(tracker, newMockKV(), newMockRedisSubscriber(), testLogger(), newTestCounter(), time.Minute)

	w.handleExpiredKey("confirmation:wait:not-a-uuid")
	w.handleExpiredKey("confirmation:wait:../../admin-key")
	w.handleExpiredKey("confirmation:wait:12345")

	if len(tracker.getCalls()) != 0 {
		t.Error("expected no calls for invalid UUID version_ids")
	}
}

func TestHandleExpiredKey_MetaNotFound(t *testing.T) {
	tracker := &mockTracker{}
	w := NewWatchdog(tracker, newMockKV(), newMockRedisSubscriber(), testLogger(), newTestCounter(), time.Minute)

	w.handleExpiredKey(validKey(validUUID))

	if len(tracker.getCalls()) != 0 {
		t.Error("expected no calls when meta not found")
	}
}

func TestHandleExpiredKey_MetaReadError(t *testing.T) {
	tracker := &mockTracker{}
	kv := newMockKV()
	kv.getErr = errors.New("redis connection error")

	w := NewWatchdog(tracker, kv, newMockRedisSubscriber(), testLogger(), newTestCounter(), time.Minute)

	w.handleExpiredKey(validKey(validUUID))

	if len(tracker.getCalls()) != 0 {
		t.Error("expected no calls on meta read error")
	}
}

func TestHandleExpiredKey_CorruptMeta(t *testing.T) {
	tracker := &mockTracker{}
	kv := newMockKV()
	kv.set(statustracker.ConfirmationMetaKey(validUUID), "not-json")

	w := NewWatchdog(tracker, kv, newMockRedisSubscriber(), testLogger(), newTestCounter(), time.Minute)

	w.handleExpiredKey(validKey(validUUID))

	if len(tracker.getCalls()) != 0 {
		t.Error("expected no calls on corrupt meta")
	}
}

func TestHandleExpiredKey_MissingIdentityFields(t *testing.T) {
	tracker := &mockTracker{}
	kv := newMockKV()

	meta := confirmationMeta{OrganizationID: "", DocumentID: "", VersionID: validUUID}
	data, _ := json.Marshal(meta)
	kv.set(statustracker.ConfirmationMetaKey(validUUID), string(data))

	w := NewWatchdog(tracker, kv, newMockRedisSubscriber(), testLogger(), newTestCounter(), time.Minute)

	w.handleExpiredKey(validKey(validUUID))

	if len(tracker.getCalls()) != 0 {
		t.Error("expected no calls when identity fields missing")
	}
}

func TestHandleExpiredKey_InvalidTransition_GracefulSkip(t *testing.T) {
	tracker := &mockTracker{err: statustracker.ErrInvalidTransition}
	kv := newMockKV()
	counter := newTestCounter()

	seedMeta(kv, validUUID, "org-1", "doc-1", "job-1")

	w := NewWatchdog(tracker, kv, newMockRedisSubscriber(), testLogger(), counter, time.Minute)

	w.handleExpiredKey(validKey(validUUID))

	calls := tracker.getCalls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 call (attempt), got %d", len(calls))
	}

	if v := testutil.ToFloat64(counter); v != 0 {
		t.Errorf("expected counter=0 on invalid transition, got %f", v)
	}
}

func TestHandleExpiredKey_TransientError_RetriesOnce(t *testing.T) {
	var attempt atomic.Int64
	tracker := &mockTracker{
		callFn: func(_, _, _ string) error {
			n := attempt.Add(1)
			if n == 1 {
				return errors.New("redis timeout")
			}
			return nil
		},
	}
	kv := newMockKV()
	counter := newTestCounter()

	seedMeta(kv, validUUID, "org-1", "doc-1", "job-1")

	w := NewWatchdog(tracker, kv, newMockRedisSubscriber(), testLogger(), counter, time.Minute)

	w.handleExpiredKey(validKey(validUUID))

	calls := tracker.getCalls()
	if len(calls) != 2 {
		t.Fatalf("expected 2 calls (initial + retry), got %d", len(calls))
	}

	if v := testutil.ToFloat64(counter); v != 1 {
		t.Errorf("expected counter=1 after successful retry, got %f", v)
	}
}

func TestHandleExpiredKey_TransientError_BothFail(t *testing.T) {
	tracker := &mockTracker{err: errors.New("redis unavailable")}
	kv := newMockKV()
	counter := newTestCounter()

	seedMeta(kv, validUUID, "org-1", "doc-1", "job-1")

	w := NewWatchdog(tracker, kv, newMockRedisSubscriber(), testLogger(), counter, time.Minute)

	w.handleExpiredKey(validKey(validUUID))

	calls := tracker.getCalls()
	if len(calls) != 2 {
		t.Fatalf("expected 2 calls (initial + retry), got %d", len(calls))
	}

	if v := testutil.ToFloat64(counter); v != 0 {
		t.Errorf("expected counter=0 when both attempts fail, got %f", v)
	}
}

func TestHandleExpiredKey_RetryGetsInvalidTransition(t *testing.T) {
	var attempt atomic.Int64
	tracker := &mockTracker{
		callFn: func(_, _, _ string) error {
			n := attempt.Add(1)
			if n == 1 {
				return errors.New("redis timeout")
			}
			return statustracker.ErrInvalidTransition
		},
	}
	kv := newMockKV()
	counter := newTestCounter()

	seedMeta(kv, validUUID, "org-1", "doc-1", "job-1")

	w := NewWatchdog(tracker, kv, newMockRedisSubscriber(), testLogger(), counter, time.Minute)

	w.handleExpiredKey(validKey(validUUID))

	if v := testutil.ToFloat64(counter); v != 0 {
		t.Errorf("expected counter=0 when retry gets invalid transition, got %f", v)
	}
}

func TestHandleExpiredKey_MultipleKeys_MetricsIncremented(t *testing.T) {
	tracker := &mockTracker{}
	kv := newMockKV()
	counter := newTestCounter()

	uuid1 := "a1b2c3d4-e5f6-4a7b-8c9d-000000000001"
	uuid2 := "a1b2c3d4-e5f6-4a7b-8c9d-000000000002"

	seedMeta(kv, uuid1, "org-1", "doc-1", "job-1")
	seedMeta(kv, uuid2, "org-1", "doc-2", "job-2")

	w := NewWatchdog(tracker, kv, newMockRedisSubscriber(), testLogger(), counter, time.Minute)

	w.handleExpiredKey(validKey(uuid1))
	w.handleExpiredKey(validKey(uuid2))

	if v := testutil.ToFloat64(counter); v != 2 {
		t.Errorf("expected counter=2, got %f", v)
	}
}

// --- ConfigSet Tests ---

func TestTryEnableKeyspaceNotifications_Success(t *testing.T) {
	rdb := newMockRedisSubscriber()
	w := NewWatchdog(&mockTracker{}, newMockKV(), rdb, testLogger(), newTestCounter(), time.Minute)

	if !w.tryEnableKeyspaceNotifications() {
		t.Error("expected success when ConfigSet succeeds")
	}
}

func TestTryEnableKeyspaceNotifications_Failure(t *testing.T) {
	rdb := newMockRedisSubscriber()
	rdb.configSetErr = errors.New("ERR Unsupported CONFIG parameter")

	w := NewWatchdog(&mockTracker{}, newMockKV(), rdb, testLogger(), newTestCounter(), time.Minute)

	if w.tryEnableKeyspaceNotifications() {
		t.Error("expected failure when ConfigSet fails")
	}
}

// --- Scan Tests ---

func TestScanExpiredKeys_PTTLExpired(t *testing.T) {
	tracker := &mockTracker{}
	kv := newMockKV()
	rdb := newMockRedisSubscriber()

	uuid1 := "a1b2c3d4-e5f6-4a7b-8c9d-000000000001"
	uuid2 := "a1b2c3d4-e5f6-4a7b-8c9d-000000000002"
	key1 := validKey(uuid1)
	key2 := validKey(uuid2)

	rdb.scanKeys = []string{key1, key2}
	rdb.pttlResults[key1] = -2            // already expired
	rdb.pttlResults[key2] = 30 * time.Second // within scanInterval

	seedMeta(kv, uuid1, "org-1", "doc-1", "job-1")
	seedMeta(kv, uuid2, "org-2", "doc-2", "job-2")

	w := NewWatchdog(tracker, kv, rdb, testLogger(), newTestCounter(), time.Minute)

	w.scanExpiredKeys()

	calls := tracker.getCalls()
	if len(calls) != 2 {
		t.Fatalf("expected 2 calls (both within threshold), got %d", len(calls))
	}
}

func TestScanExpiredKeys_PTTLBeyondInterval_Skipped(t *testing.T) {
	tracker := &mockTracker{}
	rdb := newMockRedisSubscriber()

	key := validKey(validUUID)
	rdb.scanKeys = []string{key}
	rdb.pttlResults[key] = 2 * time.Hour // well beyond scanInterval

	w := NewWatchdog(tracker, newMockKV(), rdb, testLogger(), newTestCounter(), time.Minute)

	w.scanExpiredKeys()

	if len(tracker.getCalls()) != 0 {
		t.Error("expected no calls for key with long TTL")
	}
}

func TestScanExpiredKeys_PTTLError(t *testing.T) {
	tracker := &mockTracker{}
	rdb := newMockRedisSubscriber()

	key := validKey(validUUID)
	rdb.scanKeys = []string{key}
	rdb.pttlErr = errors.New("redis error")

	w := NewWatchdog(tracker, newMockKV(), rdb, testLogger(), newTestCounter(), time.Minute)

	w.scanExpiredKeys()

	if len(tracker.getCalls()) != 0 {
		t.Error("expected no calls on PTTL error")
	}
}

func TestScanExpiredKeys_ScanError(t *testing.T) {
	tracker := &mockTracker{}
	rdb := newMockRedisSubscriber()
	rdb.scanErr = errors.New("redis error")

	w := NewWatchdog(tracker, newMockKV(), rdb, testLogger(), newTestCounter(), time.Minute)

	w.scanExpiredKeys()

	if len(tracker.getCalls()) != 0 {
		t.Error("expected no calls on scan error")
	}
}

func TestScanExpiredKeys_EmptyResults(t *testing.T) {
	tracker := &mockTracker{}
	rdb := newMockRedisSubscriber()
	rdb.scanKeys = []string{}

	w := NewWatchdog(tracker, newMockKV(), rdb, testLogger(), newTestCounter(), time.Minute)

	w.scanExpiredKeys()

	if len(tracker.getCalls()) != 0 {
		t.Error("expected no calls on empty scan")
	}
}

// --- Lifecycle Tests ---

func TestScanLoop_GracefulShutdown(t *testing.T) {
	rdb := newMockRedisSubscriber()
	rdb.configSetErr = errors.New("no config set")

	w := NewWatchdog(&mockTracker{}, newMockKV(), rdb, testLogger(), newTestCounter(), 50*time.Millisecond)

	if err := w.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	done := make(chan struct{})
	go func() {
		w.Shutdown()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Shutdown did not complete in time")
	}
}

func TestStart_Idempotent(t *testing.T) {
	rdb := newMockRedisSubscriber()
	rdb.configSetErr = errors.New("no config set")

	w := NewWatchdog(&mockTracker{}, newMockKV(), rdb, testLogger(), newTestCounter(), time.Minute)

	if err := w.Start(); err != nil {
		t.Fatalf("first Start failed: %v", err)
	}
	if err := w.Start(); err != nil {
		t.Fatalf("second Start failed: %v", err)
	}

	w.Shutdown()
}

func TestScanLoop_ProcessesKeys(t *testing.T) {
	tracker := &mockTracker{}
	kv := newMockKV()
	rdb := newMockRedisSubscriber()
	rdb.configSetErr = errors.New("no config set")
	rdb.scanKeys = []string{validKey(validUUID)}
	// Default PTTL mock returns -2 (expired) for unknown keys

	seedMeta(kv, validUUID, "org-1", "doc-1", "job-1")

	w := NewWatchdog(tracker, kv, rdb, testLogger(), newTestCounter(), 50*time.Millisecond)

	if err := w.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	time.Sleep(200 * time.Millisecond)

	w.Shutdown()

	calls := tracker.getCalls()
	if len(calls) == 0 {
		t.Error("expected at least one timeout call from scan loop")
	}
}

func TestConcurrentHandleExpiredKey(t *testing.T) {
	var count atomic.Int64
	tracker := &mockTracker{
		callFn: func(_, _, _ string) error {
			count.Add(1)
			return nil
		},
	}
	kv := newMockKV()

	uuids := make([]string, 10)
	for i := 0; i < 10; i++ {
		uuids[i] = "a1b2c3d4-e5f6-4a7b-8c9d-00000000" + string(rune('0'+i/10)) + string(rune('0'+i%10)) + "00"
	}
	// Use proper UUIDs
	uuids = []string{
		"a1b2c3d4-e5f6-4a7b-8c9d-000000000001",
		"a1b2c3d4-e5f6-4a7b-8c9d-000000000002",
		"a1b2c3d4-e5f6-4a7b-8c9d-000000000003",
		"a1b2c3d4-e5f6-4a7b-8c9d-000000000004",
		"a1b2c3d4-e5f6-4a7b-8c9d-000000000005",
		"a1b2c3d4-e5f6-4a7b-8c9d-000000000006",
		"a1b2c3d4-e5f6-4a7b-8c9d-000000000007",
		"a1b2c3d4-e5f6-4a7b-8c9d-000000000008",
		"a1b2c3d4-e5f6-4a7b-8c9d-000000000009",
		"a1b2c3d4-e5f6-4a7b-8c9d-000000000010",
	}

	for _, uid := range uuids {
		seedMeta(kv, uid, "org-1", "doc-1", "job-1")
	}

	w := NewWatchdog(tracker, kv, newMockRedisSubscriber(), testLogger(), newTestCounter(), time.Minute)

	var wg sync.WaitGroup
	for _, uid := range uuids {
		wg.Add(1)
		go func(u string) {
			defer wg.Done()
			w.handleExpiredKey(validKey(u))
		}(uid)
	}
	wg.Wait()

	if count.Load() != 10 {
		t.Errorf("expected 10 calls, got %d", count.Load())
	}
}

func TestNewWatchdog_ComponentLogger(t *testing.T) {
	w := NewWatchdog(&mockTracker{}, newMockKV(), newMockRedisSubscriber(), testLogger(), newTestCounter(), time.Minute)
	if w.log == nil {
		t.Error("expected non-nil logger")
	}
}

func TestInterfaceCompliance(t *testing.T) {
	var _ StatusTracker = (*mockTracker)(nil)
	var _ KVStore = (*mockKV)(nil)
}
