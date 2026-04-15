package statustracker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"

	"contractpro/api-orchestrator/internal/egress/ssebroadcast"
)

// ConfirmationStore provides atomic Redis operations for the user type
// confirmation flow (FR-2.1.3). All methods execute atomically via Redis
// Lua scripts to prevent race conditions between concurrent HTTP requests
// and event consumers.
type ConfirmationStore interface {
	// SetAwaitingInput atomically verifies that the current status is
	// expectedStatus, sets it to newStatusJSON, and creates a watchdog key
	// with watchdogTTL. Returns ErrInvalidTransition if the current status
	// does not match.
	SetAwaitingInput(ctx context.Context, statusKey, expectedStatus, newStatusJSON string, statusTTL time.Duration, watchdogKey string, watchdogTTL time.Duration) error

	// ConfirmInput atomically verifies that the current status is
	// AWAITING_USER_INPUT, sets it to newStatusJSON, and deletes the watchdog
	// key. Returns ErrNotAwaitingInput if the status does not match.
	ConfirmInput(ctx context.Context, statusKey, newStatusJSON string, statusTTL time.Duration, watchdogKey string) error

	// TimeoutInput atomically verifies that the current status is
	// AWAITING_USER_INPUT and sets it to newStatusJSON (FAILED).
	// Returns ErrInvalidTransition if the status has already changed.
	TimeoutInput(ctx context.Context, statusKey, newStatusJSON string, statusTTL time.Duration) error
}

// --- Redis Lua scripts ---
//
// All scripts use cjson.decode to parse the status JSON record, avoiding
// fragile string matching. cjson is built into Redis since 2.6.

// luaSetAwaitingInput atomically:
//  1. Gets current status, decodes JSON, checks status == ARGV[1]
//  2. Sets status key to ARGV[2] with PX = ARGV[3] (milliseconds)
//  3. Sets watchdog key with PX = ARGV[4] (milliseconds)
//
// KEYS[1] = status key, KEYS[2] = watchdog key
// ARGV[1] = expected status, ARGV[2] = new status JSON, ARGV[3] = status TTL ms, ARGV[4] = watchdog TTL ms
const luaSetAwaitingInput = `
local data = redis.call('GET', KEYS[1])
if not data then
    return redis.error_reply('INVALID_TRANSITION')
end
local rec = cjson.decode(data)
if rec.status ~= ARGV[1] then
    return redis.error_reply('INVALID_TRANSITION')
end
redis.call('SET', KEYS[1], ARGV[2], 'PX', ARGV[3])
redis.call('SET', KEYS[2], '1', 'PX', ARGV[4])
return 'OK'`

// luaConfirmInput atomically:
//  1. Gets current status, decodes JSON, checks status == "AWAITING_USER_INPUT"
//  2. Sets status key to ARGV[1] with PX = ARGV[2] (milliseconds)
//  3. Deletes watchdog key
//
// KEYS[1] = status key, KEYS[2] = watchdog key
// ARGV[1] = new status JSON, ARGV[2] = status TTL ms
const luaConfirmInput = `
local data = redis.call('GET', KEYS[1])
if not data then
    return redis.error_reply('NOT_AWAITING_INPUT')
end
local rec = cjson.decode(data)
if rec.status ~= 'AWAITING_USER_INPUT' then
    return redis.error_reply('NOT_AWAITING_INPUT')
end
redis.call('SET', KEYS[1], ARGV[1], 'PX', ARGV[2])
redis.call('DEL', KEYS[2])
return 'OK'`

// luaTimeoutInput atomically:
//  1. Gets current status, decodes JSON, checks status == "AWAITING_USER_INPUT"
//  2. Sets status key to ARGV[1] with PX = ARGV[2] (milliseconds)
//
// KEYS[1] = status key
// ARGV[1] = new status JSON, ARGV[2] = status TTL ms
const luaTimeoutInput = `
local data = redis.call('GET', KEYS[1])
if not data then
    return redis.error_reply('INVALID_TRANSITION')
end
local rec = cjson.decode(data)
if rec.status ~= 'AWAITING_USER_INPUT' then
    return redis.error_reply('INVALID_TRANSITION')
end
redis.call('SET', KEYS[1], ARGV[1], 'PX', ARGV[2])
return 'OK'`

// --- evalFunc indirection (same pattern as ratelimit/store.go) ---

type evalAnyFunc func(ctx context.Context, script string, keys []string, args ...any) (any, error)

// RedisConfirmationStore implements ConfirmationStore using Redis Lua scripts.
type RedisConfirmationStore struct {
	eval evalAnyFunc
}

var _ ConfirmationStore = (*RedisConfirmationStore)(nil)

// NewRedisConfirmationStore creates a ConfirmationStore backed by Redis Lua
// scripts. In production, rdb is obtained via kvstore.Client.RawRedis().
func NewRedisConfirmationStore(rdb redis.Cmdable) *RedisConfirmationStore {
	return &RedisConfirmationStore{
		eval: func(ctx context.Context, script string, keys []string, args ...any) (any, error) {
			return rdb.Eval(ctx, script, keys, args...).Result()
		},
	}
}

// newRedisConfirmationStoreWithEval creates a store with a custom eval
// function (for tests).
func newRedisConfirmationStoreWithEval(fn evalAnyFunc) *RedisConfirmationStore {
	return &RedisConfirmationStore{eval: fn}
}

func (s *RedisConfirmationStore) SetAwaitingInput(
	ctx context.Context,
	statusKey, expectedStatus, newStatusJSON string,
	statusTTL time.Duration,
	watchdogKey string,
	watchdogTTL time.Duration,
) error {
	_, err := s.eval(ctx, luaSetAwaitingInput,
		[]string{statusKey, watchdogKey},
		expectedStatus,
		newStatusJSON,
		statusTTL.Milliseconds(),
		watchdogTTL.Milliseconds(),
	)
	return mapLuaError(err)
}

func (s *RedisConfirmationStore) ConfirmInput(
	ctx context.Context,
	statusKey, newStatusJSON string,
	statusTTL time.Duration,
	watchdogKey string,
) error {
	_, err := s.eval(ctx, luaConfirmInput,
		[]string{statusKey, watchdogKey},
		newStatusJSON,
		statusTTL.Milliseconds(),
	)
	return mapLuaError(err)
}

func (s *RedisConfirmationStore) TimeoutInput(
	ctx context.Context,
	statusKey, newStatusJSON string,
	statusTTL time.Duration,
) error {
	_, err := s.eval(ctx, luaTimeoutInput,
		[]string{statusKey},
		newStatusJSON,
		statusTTL.Milliseconds(),
	)
	return mapLuaError(err)
}

func mapLuaError(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	if strings.Contains(msg, "INVALID_TRANSITION") {
		return ErrInvalidTransition
	}
	if strings.Contains(msg, "NOT_AWAITING_INPUT") {
		return ErrNotAwaitingInput
	}
	return fmt.Errorf("confirmation store: %w", err)
}

// --- Tracker methods for the confirmation flow ---

// WithConfirmation configures the Tracker with the type confirmation flow
// dependencies. Must be called before using SetAwaitingUserInput, ConfirmType,
// or TimeoutAwaitingInput.
func (t *Tracker) WithConfirmation(cs ConfirmationStore, timeout time.Duration) *Tracker {
	t.confirmStore = cs
	t.confirmationTimeout = timeout
	return t
}

// SetAwaitingUserInput atomically transitions a version from ANALYZING to
// AWAITING_USER_INPUT and creates a watchdog key with the configured timeout.
// Called by the ClassificationUncertainHandler (ORCH-TASK-040) when LIC
// reports low classification confidence.
func (t *Tracker) SetAwaitingUserInput(ctx context.Context, orgID, docID, verID string) error {
	if t.confirmStore == nil {
		return fmt.Errorf("statustracker: confirmation store not configured")
	}
	if orgID == "" || docID == "" || verID == "" {
		return fmt.Errorf("statustracker: missing identity fields")
	}

	sKey := statusKey(orgID, docID, verID)
	wKey := confirmationKey(verID)

	rec := statusRecord{
		Status:    string(StatusAwaitingUserInput),
		UpdatedAt: t.now().UTC().Format(time.RFC3339),
	}
	data, _ := json.Marshal(rec)

	err := t.confirmStore.SetAwaitingInput(
		ctx,
		sKey,
		string(StatusAnalyzing),
		string(data),
		statusTTL,
		wKey,
		t.confirmationTimeout,
	)
	if err != nil {
		t.log.Error(ctx, "SetAwaitingUserInput failed",
			"status_key", sKey,
			"error", err.Error())
		return err
	}

	t.log.Info(ctx, "version moved to AWAITING_USER_INPUT",
		"status_key", sKey,
		"watchdog_key", wKey,
		"watchdog_ttl", t.confirmationTimeout.String())

	sseEvent := t.buildStatusUpdateEvent(docID, verID, "", StatusAwaitingUserInput)
	_ = t.broadcaster.Broadcast(ctx, orgID, sseEvent)
	return nil
}

// ConfirmType atomically transitions a version from AWAITING_USER_INPUT back
// to ANALYZING and removes the watchdog key. Called by the POST /confirm-type
// HTTP handler (ORCH-TASK-041). Returns ErrNotAwaitingInput if the version is
// not in AWAITING_USER_INPUT status (maps to HTTP 409).
func (t *Tracker) ConfirmType(ctx context.Context, orgID, docID, verID string) error {
	if t.confirmStore == nil {
		return fmt.Errorf("statustracker: confirmation store not configured")
	}
	if orgID == "" || docID == "" || verID == "" {
		return fmt.Errorf("statustracker: missing identity fields")
	}

	sKey := statusKey(orgID, docID, verID)
	wKey := confirmationKey(verID)

	rec := statusRecord{
		Status:    string(StatusAnalyzing),
		UpdatedAt: t.now().UTC().Format(time.RFC3339),
	}
	data, _ := json.Marshal(rec)

	err := t.confirmStore.ConfirmInput(ctx, sKey, string(data), statusTTL, wKey)
	if err != nil {
		if errors.Is(err, ErrNotAwaitingInput) {
			t.log.Warn(ctx, "ConfirmType: version not awaiting input",
				"status_key", sKey)
		} else {
			t.log.Error(ctx, "ConfirmType failed",
				"status_key", sKey,
				"error", err.Error())
		}
		return err
	}

	t.log.Info(ctx, "type confirmed, version resumed to ANALYZING",
		"status_key", sKey)

	sseEvent := t.buildStatusUpdateEvent(docID, verID, "", StatusAnalyzing)
	sseEvent.Message = "Анализ возобновлён"
	_ = t.broadcaster.Broadcast(ctx, orgID, sseEvent)
	return nil
}

// TimeoutAwaitingInput transitions a version from AWAITING_USER_INPUT to FAILED
// when the watchdog timer expires. Called by the ConfirmationWatchdog
// (ORCH-TASK-042). Returns ErrInvalidTransition if the version status has
// already changed (user confirmed just in time — race resolved gracefully).
func (t *Tracker) TimeoutAwaitingInput(ctx context.Context, orgID, docID, verID string) error {
	if t.confirmStore == nil {
		return fmt.Errorf("statustracker: confirmation store not configured")
	}
	if orgID == "" || docID == "" || verID == "" {
		return fmt.Errorf("statustracker: missing identity fields")
	}

	sKey := statusKey(orgID, docID, verID)

	rec := statusRecord{
		Status:    string(StatusFailed),
		UpdatedAt: t.now().UTC().Format(time.RFC3339),
	}
	data, _ := json.Marshal(rec)

	err := t.confirmStore.TimeoutInput(ctx, sKey, string(data), statusTTL)
	if err != nil {
		if errors.Is(err, ErrInvalidTransition) {
			t.log.Info(ctx, "TimeoutAwaitingInput: status already changed, skipping",
				"status_key", sKey)
		} else {
			t.log.Error(ctx, "TimeoutAwaitingInput failed",
				"status_key", sKey,
				"error", err.Error())
		}
		return err
	}

	t.log.Info(ctx, "version timed out waiting for user input",
		"status_key", sKey)

	sseEvent := t.buildStatusUpdateEvent(docID, verID, "", StatusFailed)
	sseEvent.ErrorCode = "USER_CONFIRMATION_TIMEOUT"
	sseEvent.ErrorMessage = "Время на подтверждение типа договора истекло"
	_ = t.broadcaster.Broadcast(ctx, orgID, sseEvent)
	return nil
}

// ConfirmationTimeout returns the configured confirmation timeout.
// Exported for use by downstream components (e.g., watchdog).
func (t *Tracker) ConfirmationTimeout() time.Duration {
	return t.confirmationTimeout
}

// StatusUpdateEvent is the SSE payload for the type confirmation flow.
// Exported for use by downstream components that need to build custom SSE events.
func (t *Tracker) BuildSSEEvent(docID, verID, jobID string, status UserStatus) ssebroadcast.Event {
	return t.buildStatusUpdateEvent(docID, verID, jobID, status)
}
