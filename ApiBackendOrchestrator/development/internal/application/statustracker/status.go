// Package statustracker tracks the processing status of document versions by
// consuming events from DP, LIC, RE, and DM domains, enforcing monotonic
// status ordering, persisting state in Redis, and broadcasting SSE events
// via Redis Pub/Sub.
package statustracker

import (
	"context"
	"time"
)

// UserStatus represents a user-facing processing status for a document version.
type UserStatus string

const (
	StatusUploaded          UserStatus = "UPLOADED"
	StatusQueued            UserStatus = "QUEUED"
	StatusProcessing        UserStatus = "PROCESSING"
	StatusAnalyzing         UserStatus = "ANALYZING"
	StatusGeneratingReports UserStatus = "GENERATING_REPORTS"
	StatusReady             UserStatus = "READY"
	StatusFailed            UserStatus = "FAILED"
	StatusAnalysisFailed    UserStatus = "ANALYSIS_FAILED"
	StatusReportsFailed     UserStatus = "REPORTS_FAILED"
	StatusPartiallyFailed   UserStatus = "PARTIALLY_FAILED"
	StatusRejected          UserStatus = "REJECTED"
)

// statusOrder defines the monotonic ordering for happy-path statuses.
// Higher index = more advanced. Only forward transitions are allowed.
// Failure statuses are not in this map — they are handled separately.
var statusOrder = map[UserStatus]int{
	StatusUploaded:          0,
	StatusQueued:            1,
	StatusProcessing:        2,
	StatusAnalyzing:         3,
	StatusGeneratingReports: 4,
	StatusReady:             5,
}

// terminalStatuses contains all statuses that cannot be overwritten.
var terminalStatuses = map[UserStatus]struct{}{
	StatusReady:           {},
	StatusFailed:          {},
	StatusAnalysisFailed:  {},
	StatusReportsFailed:   {},
	StatusPartiallyFailed: {},
	StatusRejected:        {},
}

// statusMessages maps each UserStatus to a Russian user-facing description
// for NFR-5.2 compliance.
var statusMessages = map[UserStatus]string{
	StatusUploaded:          "Договор загружен",
	StatusQueued:            "В очереди на обработку",
	StatusProcessing:        "Извлечение текста и структуры",
	StatusAnalyzing:         "Юридический анализ",
	StatusGeneratingReports: "Формирование отчётов",
	StatusReady:             "Анализ завершён",
	StatusFailed:            "Ошибка обработки",
	StatusAnalysisFailed:    "Ошибка юридического анализа",
	StatusReportsFailed:     "Ошибка формирования отчётов",
	StatusPartiallyFailed:   "Частично доступно (есть ошибки)",
	StatusRejected:          "Файл отклонён (формат/размер)",
}

// SSEEvent is the JSON payload published to Redis Pub/Sub and delivered to
// SSE clients.
type SSEEvent struct {
	EventType       string `json:"event_type"`
	DocumentID      string `json:"document_id"`
	VersionID       string `json:"version_id,omitempty"`
	JobID           string `json:"job_id,omitempty"`
	Status          string `json:"status"`
	Message         string `json:"message"`
	Timestamp       string `json:"timestamp"`
	IsRetryable     bool   `json:"is_retryable"`
	ErrorCode       string `json:"error_code,omitempty"`
	ErrorMessage    string `json:"error_message,omitempty"`
	BaseVersionID   string `json:"base_version_id,omitempty"`
	TargetVersionID string `json:"target_version_id,omitempty"`
}

// statusRecord is the JSON value stored in Redis for each version's current
// processing status.
type statusRecord struct {
	Status    string `json:"status"`
	UpdatedAt string `json:"updated_at"`
}

// KVStore provides key-value and pub/sub operations for status persistence
// and SSE broadcasting.
//
// Satisfied by: *kvstore.Client
type KVStore interface {
	Get(ctx context.Context, key string) (string, error)
	Set(ctx context.Context, key string, value string, ttl time.Duration) error
	Publish(ctx context.Context, channel string, message string) error
}

const (
	statusKeyPrefix  = "status"
	sseChannelPrefix = "sse:broadcast"
	statusTTL        = 24 * time.Hour
)

// statusKey builds the Redis key for a version's processing status.
func statusKey(orgID, docID, verID string) string {
	return statusKeyPrefix + ":" + orgID + ":" + docID + ":" + verID
}

// sseChannel builds the Redis Pub/Sub channel for an organization's SSE broadcasts.
func sseChannel(orgID string) string {
	return sseChannelPrefix + ":" + orgID
}

// isTerminal returns true if the status cannot be overwritten.
func isTerminal(s UserStatus) bool {
	_, ok := terminalStatuses[s]
	return ok
}

// isForwardTransition returns true if transitioning from current to next is a
// valid forward move on the happy path.
func isForwardTransition(current, next UserStatus) bool {
	curIdx, curOK := statusOrder[current]
	nextIdx, nextOK := statusOrder[next]
	if !curOK || !nextOK {
		return false
	}
	return nextIdx > curIdx
}

// canTransition decides whether a transition from current to next is allowed.
//
// Rules:
//  1. If current is terminal, no transition is allowed.
//  2. If next is a failure/terminal status, transition is always allowed
//     from any non-terminal current status.
//  3. If next is a happy-path status, it must be strictly forward.
func canTransition(current, next UserStatus) bool {
	if isTerminal(current) {
		return false
	}
	if isTerminal(next) {
		return true
	}
	return isForwardTransition(current, next)
}

// derefBool returns the value of a *bool, or false if nil.
func derefBool(b *bool) bool {
	if b == nil {
		return false
	}
	return *b
}
