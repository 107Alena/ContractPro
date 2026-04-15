package statustracker

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"contractpro/api-orchestrator/internal/infra/observability/logger"
)

// --- Mock ConfirmationStore ---
//
// Uses a shared in-memory store with mutex for atomicity, simulating the
// behaviour of the Redis Lua scripts at the Go level.

type mockConfirmationStore struct {
	mu    sync.Mutex
	store map[string]mockEntry
	err   error // injected infrastructure error
}

type mockEntry struct {
	value string
	ttl   time.Duration
}

func newMockConfirmationStore() *mockConfirmationStore {
	return &mockConfirmationStore{store: make(map[string]mockEntry)}
}

func (m *mockConfirmationStore) seedStatus(key, statusJSON string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.store[key] = mockEntry{value: statusJSON}
}

func (m *mockConfirmationStore) getEntry(key string) (mockEntry, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.store[key]
	return e, ok
}

func (m *mockConfirmationStore) getStatus(key string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.store[key]
	if !ok {
		return ""
	}
	var rec statusRecord
	if err := json.Unmarshal([]byte(e.value), &rec); err != nil {
		return ""
	}
	return rec.Status
}

func (m *mockConfirmationStore) SetAwaitingInput(
	ctx context.Context,
	statusKey, expectedStatus, newStatusJSON string,
	statusTTL time.Duration,
	watchdogKey string,
	watchdogTTL time.Duration,
) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.err != nil {
		return m.err
	}

	entry, ok := m.store[statusKey]
	if !ok {
		return ErrInvalidTransition
	}
	var rec statusRecord
	if err := json.Unmarshal([]byte(entry.value), &rec); err != nil {
		return ErrInvalidTransition
	}
	if rec.Status != expectedStatus {
		return ErrInvalidTransition
	}

	m.store[statusKey] = mockEntry{value: newStatusJSON, ttl: statusTTL}
	m.store[watchdogKey] = mockEntry{value: "1", ttl: watchdogTTL}
	return nil
}

func (m *mockConfirmationStore) ConfirmInput(
	ctx context.Context,
	statusKey, newStatusJSON string,
	statusTTL time.Duration,
	watchdogKey string,
) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.err != nil {
		return m.err
	}

	entry, ok := m.store[statusKey]
	if !ok {
		return ErrNotAwaitingInput
	}
	var rec statusRecord
	if err := json.Unmarshal([]byte(entry.value), &rec); err != nil {
		return ErrNotAwaitingInput
	}
	if rec.Status != string(StatusAwaitingUserInput) {
		return ErrNotAwaitingInput
	}

	m.store[statusKey] = mockEntry{value: newStatusJSON, ttl: statusTTL}
	delete(m.store, watchdogKey)
	return nil
}

func (m *mockConfirmationStore) TimeoutInput(
	ctx context.Context,
	statusKey, newStatusJSON string,
	statusTTL time.Duration,
) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.err != nil {
		return m.err
	}

	entry, ok := m.store[statusKey]
	if !ok {
		return ErrInvalidTransition
	}
	var rec statusRecord
	if err := json.Unmarshal([]byte(entry.value), &rec); err != nil {
		return ErrInvalidTransition
	}
	if rec.Status != string(StatusAwaitingUserInput) {
		return ErrInvalidTransition
	}

	m.store[statusKey] = mockEntry{value: newStatusJSON, ttl: statusTTL}
	return nil
}

// --- Test helpers ---

const (
	testOrgID = "org-1"
	testDocID = "doc-1"
	testVerID = "ver-1"
)

var testConfirmTimeout = 24 * time.Hour

func newTestConfirmTracker(cs *mockConfirmationStore, bc *mockBroadcaster) *Tracker {
	kv := newMockKVStore()
	t := NewTracker(kv, bc, logger.NewLogger("error"))
	t.now = fixedNow
	t.WithConfirmation(cs, testConfirmTimeout)
	return t
}

func seedAnalyzing(cs *mockConfirmationStore) {
	rec := statusRecord{
		Status:    string(StatusAnalyzing),
		UpdatedAt: fixedTime.Format(time.RFC3339),
	}
	data, _ := json.Marshal(rec)
	cs.seedStatus(statusKey(testOrgID, testDocID, testVerID), string(data))
}

func seedAwaitingInput(cs *mockConfirmationStore) {
	rec := statusRecord{
		Status:    string(StatusAwaitingUserInput),
		UpdatedAt: fixedTime.Format(time.RFC3339),
	}
	data, _ := json.Marshal(rec)
	sKey := statusKey(testOrgID, testDocID, testVerID)
	wKey := confirmationKey(testVerID)
	cs.seedStatus(sKey, string(data))
	cs.mu.Lock()
	cs.store[wKey] = mockEntry{value: "1", ttl: testConfirmTimeout}
	cs.mu.Unlock()
}

// --- Transition logic tests ---

func TestAwaitingUserInput_TransitionRules(t *testing.T) {
	tests := []struct {
		name     string
		current  UserStatus
		next     UserStatus
		expected bool
	}{
		{"ANALYZING → AWAITING_USER_INPUT", StatusAnalyzing, StatusAwaitingUserInput, true},
		{"AWAITING_USER_INPUT → ANALYZING", StatusAwaitingUserInput, StatusAnalyzing, true},
		{"AWAITING_USER_INPUT → FAILED", StatusAwaitingUserInput, StatusFailed, true},
		{"AWAITING_USER_INPUT → READY", StatusAwaitingUserInput, StatusReady, false},
		{"AWAITING_USER_INPUT → PROCESSING", StatusAwaitingUserInput, StatusProcessing, false},
		{"AWAITING_USER_INPUT → GENERATING_REPORTS", StatusAwaitingUserInput, StatusGeneratingReports, false},
		{"AWAITING_USER_INPUT → ANALYSIS_FAILED", StatusAwaitingUserInput, StatusAnalysisFailed, false},
		{"AWAITING_USER_INPUT → REJECTED", StatusAwaitingUserInput, StatusRejected, false},
		{"AWAITING_USER_INPUT → QUEUED", StatusAwaitingUserInput, StatusQueued, false},
		{"PROCESSING → AWAITING_USER_INPUT", StatusProcessing, StatusAwaitingUserInput, false},
		{"UPLOADED → AWAITING_USER_INPUT", StatusUploaded, StatusAwaitingUserInput, false},
		{"QUEUED → AWAITING_USER_INPUT", StatusQueued, StatusAwaitingUserInput, false},
		{"GENERATING_REPORTS → AWAITING_USER_INPUT", StatusGeneratingReports, StatusAwaitingUserInput, false},
		{"FAILED → AWAITING_USER_INPUT", StatusFailed, StatusAwaitingUserInput, false},
		{"READY → AWAITING_USER_INPUT", StatusReady, StatusAwaitingUserInput, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := canTransition(tt.current, tt.next)
			if got != tt.expected {
				t.Errorf("canTransition(%s, %s) = %v, want %v",
					tt.current, tt.next, got, tt.expected)
			}
		})
	}
}

func TestAwaitingUserInput_StatusMessage(t *testing.T) {
	msg, ok := statusMessages[StatusAwaitingUserInput]
	if !ok {
		t.Fatal("StatusAwaitingUserInput missing from statusMessages")
	}
	if msg != "Ожидание подтверждения типа договора" {
		t.Errorf("unexpected message: %q", msg)
	}
}

func TestAwaitingUserInput_NotTerminal(t *testing.T) {
	if isTerminal(StatusAwaitingUserInput) {
		t.Error("AWAITING_USER_INPUT should not be terminal")
	}
}

// --- SetAwaitingUserInput tests ---

func TestSetAwaitingUserInput_HappyPath(t *testing.T) {
	cs := newMockConfirmationStore()
	bc := newMockBroadcaster()
	tr := newTestConfirmTracker(cs, bc)

	seedAnalyzing(cs)

	err := tr.SetAwaitingUserInput(context.Background(), testOrgID, testDocID, testVerID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify status changed.
	sKey := statusKey(testOrgID, testDocID, testVerID)
	if got := cs.getStatus(sKey); got != string(StatusAwaitingUserInput) {
		t.Errorf("status = %q, want %q", got, StatusAwaitingUserInput)
	}

	// Verify watchdog key created.
	wKey := confirmationKey(testVerID)
	entry, ok := cs.getEntry(wKey)
	if !ok {
		t.Fatal("watchdog key not created")
	}
	if entry.ttl != testConfirmTimeout {
		t.Errorf("watchdog TTL = %v, want %v", entry.ttl, testConfirmTimeout)
	}

	// Verify SSE event broadcast.
	events := bc.broadcastedEvents()
	if len(events) != 1 {
		t.Fatalf("expected 1 SSE event, got %d", len(events))
	}
	if events[0].event.Status != string(StatusAwaitingUserInput) {
		t.Errorf("SSE status = %q, want %q", events[0].event.Status, StatusAwaitingUserInput)
	}
	if events[0].orgID != testOrgID {
		t.Errorf("SSE orgID = %q, want %q", events[0].orgID, testOrgID)
	}
}

func TestSetAwaitingUserInput_NotAnalyzing(t *testing.T) {
	cs := newMockConfirmationStore()
	bc := newMockBroadcaster()
	tr := newTestConfirmTracker(cs, bc)

	// Seed status as PROCESSING (not ANALYZING).
	rec := statusRecord{Status: string(StatusProcessing), UpdatedAt: fixedTime.Format(time.RFC3339)}
	data, _ := json.Marshal(rec)
	cs.seedStatus(statusKey(testOrgID, testDocID, testVerID), string(data))

	err := tr.SetAwaitingUserInput(context.Background(), testOrgID, testDocID, testVerID)
	if !errors.Is(err, ErrInvalidTransition) {
		t.Errorf("expected ErrInvalidTransition, got: %v", err)
	}

	// No SSE event on error.
	if len(bc.broadcastedEvents()) != 0 {
		t.Error("expected no SSE events on failure")
	}
}

func TestSetAwaitingUserInput_NoExistingStatus(t *testing.T) {
	cs := newMockConfirmationStore()
	bc := newMockBroadcaster()
	tr := newTestConfirmTracker(cs, bc)

	err := tr.SetAwaitingUserInput(context.Background(), testOrgID, testDocID, testVerID)
	if !errors.Is(err, ErrInvalidTransition) {
		t.Errorf("expected ErrInvalidTransition, got: %v", err)
	}
}

func TestSetAwaitingUserInput_MissingIDs(t *testing.T) {
	cs := newMockConfirmationStore()
	bc := newMockBroadcaster()
	tr := newTestConfirmTracker(cs, bc)

	tests := []struct {
		name          string
		org, doc, ver string
	}{
		{"empty org", "", testDocID, testVerID},
		{"empty doc", testOrgID, "", testVerID},
		{"empty ver", testOrgID, testDocID, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tr.SetAwaitingUserInput(context.Background(), tt.org, tt.doc, tt.ver)
			if err == nil {
				t.Error("expected error for missing ID")
			}
		})
	}
}

func TestSetAwaitingUserInput_NoConfirmationStore(t *testing.T) {
	kv := newMockKVStore()
	bc := newMockBroadcaster()
	tr := NewTracker(kv, bc, logger.NewLogger("error"))

	err := tr.SetAwaitingUserInput(context.Background(), testOrgID, testDocID, testVerID)
	if err == nil {
		t.Error("expected error when confirmation store not configured")
	}
}

func TestSetAwaitingUserInput_InfraError(t *testing.T) {
	cs := newMockConfirmationStore()
	cs.err = errors.New("redis down")
	bc := newMockBroadcaster()
	tr := newTestConfirmTracker(cs, bc)

	seedAnalyzing(cs)

	err := tr.SetAwaitingUserInput(context.Background(), testOrgID, testDocID, testVerID)
	if err == nil {
		t.Error("expected error")
	}
}

// --- ConfirmType tests ---

func TestConfirmType_HappyPath(t *testing.T) {
	cs := newMockConfirmationStore()
	bc := newMockBroadcaster()
	tr := newTestConfirmTracker(cs, bc)

	seedAwaitingInput(cs)

	err := tr.ConfirmType(context.Background(), testOrgID, testDocID, testVerID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Status back to ANALYZING.
	sKey := statusKey(testOrgID, testDocID, testVerID)
	if got := cs.getStatus(sKey); got != string(StatusAnalyzing) {
		t.Errorf("status = %q, want %q", got, StatusAnalyzing)
	}

	// Watchdog key deleted.
	wKey := confirmationKey(testVerID)
	if _, ok := cs.getEntry(wKey); ok {
		t.Error("watchdog key should be deleted")
	}

	// SSE event with custom message.
	events := bc.broadcastedEvents()
	if len(events) != 1 {
		t.Fatalf("expected 1 SSE event, got %d", len(events))
	}
	if events[0].event.Status != string(StatusAnalyzing) {
		t.Errorf("SSE status = %q, want %q", events[0].event.Status, StatusAnalyzing)
	}
	if events[0].event.Message != "Анализ возобновлён" {
		t.Errorf("SSE message = %q, want %q", events[0].event.Message, "Анализ возобновлён")
	}
}

func TestConfirmType_NotAwaitingInput(t *testing.T) {
	cs := newMockConfirmationStore()
	bc := newMockBroadcaster()
	tr := newTestConfirmTracker(cs, bc)

	seedAnalyzing(cs)

	err := tr.ConfirmType(context.Background(), testOrgID, testDocID, testVerID)
	if !errors.Is(err, ErrNotAwaitingInput) {
		t.Errorf("expected ErrNotAwaitingInput, got: %v", err)
	}
}

func TestConfirmType_NoExistingStatus(t *testing.T) {
	cs := newMockConfirmationStore()
	bc := newMockBroadcaster()
	tr := newTestConfirmTracker(cs, bc)

	err := tr.ConfirmType(context.Background(), testOrgID, testDocID, testVerID)
	if !errors.Is(err, ErrNotAwaitingInput) {
		t.Errorf("expected ErrNotAwaitingInput, got: %v", err)
	}
}

func TestConfirmType_ConcurrentRace(t *testing.T) {
	cs := newMockConfirmationStore()
	bc := newMockBroadcaster()
	tr := newTestConfirmTracker(cs, bc)

	seedAwaitingInput(cs)

	var wg sync.WaitGroup
	var successCount atomic.Int32
	var notAwaitingCount atomic.Int32

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := tr.ConfirmType(context.Background(), testOrgID, testDocID, testVerID)
			if err == nil {
				successCount.Add(1)
			} else if errors.Is(err, ErrNotAwaitingInput) {
				notAwaitingCount.Add(1)
			}
		}()
	}
	wg.Wait()

	if got := successCount.Load(); got != 1 {
		t.Errorf("expected exactly 1 success, got %d", got)
	}
	if got := notAwaitingCount.Load(); got != 9 {
		t.Errorf("expected 9 ErrNotAwaitingInput, got %d", got)
	}
}

func TestConfirmType_MissingIDs(t *testing.T) {
	cs := newMockConfirmationStore()
	bc := newMockBroadcaster()
	tr := newTestConfirmTracker(cs, bc)

	err := tr.ConfirmType(context.Background(), "", testDocID, testVerID)
	if err == nil {
		t.Error("expected error for missing org ID")
	}
}

// --- TimeoutAwaitingInput tests ---

func TestTimeoutAwaitingInput_HappyPath(t *testing.T) {
	cs := newMockConfirmationStore()
	bc := newMockBroadcaster()
	tr := newTestConfirmTracker(cs, bc)

	seedAwaitingInput(cs)

	err := tr.TimeoutAwaitingInput(context.Background(), testOrgID, testDocID, testVerID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Status set to FAILED.
	sKey := statusKey(testOrgID, testDocID, testVerID)
	if got := cs.getStatus(sKey); got != string(StatusFailed) {
		t.Errorf("status = %q, want %q", got, StatusFailed)
	}

	// SSE event with error details.
	events := bc.broadcastedEvents()
	if len(events) != 1 {
		t.Fatalf("expected 1 SSE event, got %d", len(events))
	}
	if events[0].event.ErrorCode != "USER_CONFIRMATION_TIMEOUT" {
		t.Errorf("SSE error_code = %q, want %q", events[0].event.ErrorCode, "USER_CONFIRMATION_TIMEOUT")
	}
	if events[0].event.Status != string(StatusFailed) {
		t.Errorf("SSE status = %q, want %q", events[0].event.Status, StatusFailed)
	}
}

func TestTimeoutAwaitingInput_AlreadyConfirmed(t *testing.T) {
	cs := newMockConfirmationStore()
	bc := newMockBroadcaster()
	tr := newTestConfirmTracker(cs, bc)

	// Seed as ANALYZING (user already confirmed).
	seedAnalyzing(cs)

	err := tr.TimeoutAwaitingInput(context.Background(), testOrgID, testDocID, testVerID)
	if !errors.Is(err, ErrInvalidTransition) {
		t.Errorf("expected ErrInvalidTransition, got: %v", err)
	}

	// No SSE event on graceful skip.
	if len(bc.broadcastedEvents()) != 0 {
		t.Error("expected no SSE events when timeout races with confirmation")
	}
}

func TestTimeoutAwaitingInput_NoExistingStatus(t *testing.T) {
	cs := newMockConfirmationStore()
	bc := newMockBroadcaster()
	tr := newTestConfirmTracker(cs, bc)

	err := tr.TimeoutAwaitingInput(context.Background(), testOrgID, testDocID, testVerID)
	if !errors.Is(err, ErrInvalidTransition) {
		t.Errorf("expected ErrInvalidTransition, got: %v", err)
	}
}

func TestTimeoutAwaitingInput_MissingIDs(t *testing.T) {
	cs := newMockConfirmationStore()
	bc := newMockBroadcaster()
	tr := newTestConfirmTracker(cs, bc)

	err := tr.TimeoutAwaitingInput(context.Background(), testOrgID, "", testVerID)
	if err == nil {
		t.Error("expected error for missing doc ID")
	}
}

// --- ConfirmationStore Redis implementation tests ---

func TestRedisConfirmationStore_SetAwaitingInput_HappyPath(t *testing.T) {
	var capturedScript string
	var capturedKeys []string
	var capturedArgs []any

	store := newRedisConfirmationStoreWithEval(
		func(_ context.Context, script string, keys []string, args ...any) (any, error) {
			capturedScript = script
			capturedKeys = keys
			capturedArgs = args
			return "OK", nil
		})

	err := store.SetAwaitingInput(
		context.Background(),
		"status:org:doc:ver",
		"ANALYZING",
		`{"status":"AWAITING_USER_INPUT"}`,
		24*time.Hour,
		"confirmation:wait:ver",
		24*time.Hour,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedScript != luaSetAwaitingInput {
		t.Error("wrong Lua script used")
	}
	if len(capturedKeys) != 2 || capturedKeys[0] != "status:org:doc:ver" || capturedKeys[1] != "confirmation:wait:ver" {
		t.Errorf("unexpected keys: %v", capturedKeys)
	}
	if len(capturedArgs) != 4 {
		t.Fatalf("expected 4 args, got %d", len(capturedArgs))
	}
	if capturedArgs[0] != "ANALYZING" {
		t.Errorf("expected status arg = ANALYZING, got %v", capturedArgs[0])
	}
}

func TestRedisConfirmationStore_SetAwaitingInput_InvalidTransition(t *testing.T) {
	store := newRedisConfirmationStoreWithEval(
		func(_ context.Context, _ string, _ []string, _ ...any) (any, error) {
			return nil, errors.New("INVALID_TRANSITION")
		})

	err := store.SetAwaitingInput(context.Background(), "k", "s", "v", time.Hour, "w", time.Hour)
	if !errors.Is(err, ErrInvalidTransition) {
		t.Errorf("expected ErrInvalidTransition, got: %v", err)
	}
}

func TestRedisConfirmationStore_ConfirmInput_HappyPath(t *testing.T) {
	store := newRedisConfirmationStoreWithEval(
		func(_ context.Context, script string, keys []string, _ ...any) (any, error) {
			if script != luaConfirmInput {
				t.Error("wrong Lua script")
			}
			if len(keys) != 2 {
				t.Errorf("expected 2 keys, got %d", len(keys))
			}
			return "OK", nil
		})

	err := store.ConfirmInput(context.Background(), "status:key", `{"status":"ANALYZING"}`, 24*time.Hour, "watchdog:key")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRedisConfirmationStore_ConfirmInput_NotAwaitingInput(t *testing.T) {
	store := newRedisConfirmationStoreWithEval(
		func(_ context.Context, _ string, _ []string, _ ...any) (any, error) {
			return nil, errors.New("NOT_AWAITING_INPUT")
		})

	err := store.ConfirmInput(context.Background(), "k", "v", time.Hour, "w")
	if !errors.Is(err, ErrNotAwaitingInput) {
		t.Errorf("expected ErrNotAwaitingInput, got: %v", err)
	}
}

func TestRedisConfirmationStore_TimeoutInput_HappyPath(t *testing.T) {
	store := newRedisConfirmationStoreWithEval(
		func(_ context.Context, script string, keys []string, _ ...any) (any, error) {
			if script != luaTimeoutInput {
				t.Error("wrong Lua script")
			}
			if len(keys) != 1 {
				t.Errorf("expected 1 key, got %d", len(keys))
			}
			return "OK", nil
		})

	err := store.TimeoutInput(context.Background(), "status:key", `{"status":"FAILED"}`, 24*time.Hour)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRedisConfirmationStore_TimeoutInput_InvalidTransition(t *testing.T) {
	store := newRedisConfirmationStoreWithEval(
		func(_ context.Context, _ string, _ []string, _ ...any) (any, error) {
			return nil, errors.New("INVALID_TRANSITION")
		})

	err := store.TimeoutInput(context.Background(), "k", "v", time.Hour)
	if !errors.Is(err, ErrInvalidTransition) {
		t.Errorf("expected ErrInvalidTransition, got: %v", err)
	}
}

func TestRedisConfirmationStore_InfraError(t *testing.T) {
	infraErr := errors.New("connection refused")
	store := newRedisConfirmationStoreWithEval(
		func(_ context.Context, _ string, _ []string, _ ...any) (any, error) {
			return nil, infraErr
		})

	err := store.SetAwaitingInput(context.Background(), "k", "s", "v", time.Hour, "w", time.Hour)
	if err == nil {
		t.Fatal("expected error")
	}
	if errors.Is(err, ErrInvalidTransition) || errors.Is(err, ErrNotAwaitingInput) {
		t.Error("infrastructure error should not be mapped to a domain error")
	}
}

// --- Full cycle integration test ---

func TestConfirmation_FullCycle_HappyPath(t *testing.T) {
	cs := newMockConfirmationStore()
	bc := newMockBroadcaster()
	tr := newTestConfirmTracker(cs, bc)

	seedAnalyzing(cs)
	sKey := statusKey(testOrgID, testDocID, testVerID)
	wKey := confirmationKey(testVerID)

	// Step 1: Set awaiting input.
	if err := tr.SetAwaitingUserInput(context.Background(), testOrgID, testDocID, testVerID); err != nil {
		t.Fatalf("SetAwaitingUserInput: %v", err)
	}
	if got := cs.getStatus(sKey); got != string(StatusAwaitingUserInput) {
		t.Fatalf("after SetAwaitingUserInput: status = %q, want AWAITING_USER_INPUT", got)
	}
	if _, ok := cs.getEntry(wKey); !ok {
		t.Fatal("watchdog key should exist after SetAwaitingUserInput")
	}

	// Step 2: Confirm type.
	if err := tr.ConfirmType(context.Background(), testOrgID, testDocID, testVerID); err != nil {
		t.Fatalf("ConfirmType: %v", err)
	}
	if got := cs.getStatus(sKey); got != string(StatusAnalyzing) {
		t.Fatalf("after ConfirmType: status = %q, want ANALYZING", got)
	}
	if _, ok := cs.getEntry(wKey); ok {
		t.Fatal("watchdog key should be deleted after ConfirmType")
	}

	// Verify SSE events: AWAITING_USER_INPUT + ANALYZING.
	events := bc.broadcastedEvents()
	if len(events) != 2 {
		t.Fatalf("expected 2 SSE events, got %d", len(events))
	}
	if events[0].event.Status != string(StatusAwaitingUserInput) {
		t.Errorf("first SSE: status = %q, want AWAITING_USER_INPUT", events[0].event.Status)
	}
	if events[1].event.Status != string(StatusAnalyzing) {
		t.Errorf("second SSE: status = %q, want ANALYZING", events[1].event.Status)
	}
}

func TestConfirmation_FullCycle_Timeout(t *testing.T) {
	cs := newMockConfirmationStore()
	bc := newMockBroadcaster()
	tr := newTestConfirmTracker(cs, bc)

	seedAnalyzing(cs)
	sKey := statusKey(testOrgID, testDocID, testVerID)

	// Step 1: Set awaiting input.
	if err := tr.SetAwaitingUserInput(context.Background(), testOrgID, testDocID, testVerID); err != nil {
		t.Fatalf("SetAwaitingUserInput: %v", err)
	}

	// Step 2: Timeout (no confirmation).
	if err := tr.TimeoutAwaitingInput(context.Background(), testOrgID, testDocID, testVerID); err != nil {
		t.Fatalf("TimeoutAwaitingInput: %v", err)
	}
	if got := cs.getStatus(sKey); got != string(StatusFailed) {
		t.Errorf("after timeout: status = %q, want FAILED", got)
	}

	// Verify SSE events: AWAITING_USER_INPUT + FAILED with error code.
	events := bc.broadcastedEvents()
	if len(events) != 2 {
		t.Fatalf("expected 2 SSE events, got %d", len(events))
	}
	if events[1].event.ErrorCode != "USER_CONFIRMATION_TIMEOUT" {
		t.Errorf("timeout SSE error_code = %q, want USER_CONFIRMATION_TIMEOUT", events[1].event.ErrorCode)
	}
}

// --- Config validation test ---

func TestConfirmationTimeout_Config_Default(t *testing.T) {
	tr := &Tracker{}
	tr.WithConfirmation(newMockConfirmationStore(), 24*time.Hour)
	if got := tr.ConfirmationTimeout(); got != 24*time.Hour {
		t.Errorf("ConfirmationTimeout() = %v, want 24h", got)
	}
}

// --- Key helper tests ---

func TestConfirmationKey(t *testing.T) {
	key := confirmationKey("ver-123")
	if key != "confirmation:wait:ver-123" {
		t.Errorf("confirmationKey = %q, want %q", key, "confirmation:wait:ver-123")
	}
}

func TestConfirmationMetaKey(t *testing.T) {
	key := ConfirmationMetaKey("ver-123")
	if key != "confirmation:meta:ver-123" {
		t.Errorf("ConfirmationMetaKey = %q, want %q", key, "confirmation:meta:ver-123")
	}
}

// --- Error sentinel tests ---

func TestSentinelErrors_Distinct(t *testing.T) {
	if errors.Is(ErrNotAwaitingInput, ErrInvalidTransition) {
		t.Error("ErrNotAwaitingInput should not be ErrInvalidTransition")
	}
	if errors.Is(ErrInvalidTransition, ErrNotAwaitingInput) {
		t.Error("ErrInvalidTransition should not be ErrNotAwaitingInput")
	}
}

func TestMapLuaError_Infrastructure(t *testing.T) {
	err := mapLuaError(errors.New("NOSCRIPT No matching script"))
	if errors.Is(err, ErrInvalidTransition) || errors.Is(err, ErrNotAwaitingInput) {
		t.Error("generic Redis error should not map to a domain error")
	}
	if err == nil {
		t.Error("expected non-nil error")
	}
}

func TestMapLuaError_Nil(t *testing.T) {
	if err := mapLuaError(nil); err != nil {
		t.Errorf("expected nil, got: %v", err)
	}
}
