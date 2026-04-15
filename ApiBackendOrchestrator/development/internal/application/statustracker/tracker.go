package statustracker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"contractpro/api-orchestrator/internal/egress/ssebroadcast"
	"contractpro/api-orchestrator/internal/infra/kvstore"
	"contractpro/api-orchestrator/internal/infra/observability/logger"
	"contractpro/api-orchestrator/internal/ingress/consumer"
)

// Compile-time check that *Tracker satisfies consumer.EventHandler.
var _ consumer.EventHandler = (*Tracker)(nil)

// Tracker implements consumer.EventHandler. It receives deserialized events,
// maps them to user-facing statuses, enforces monotonic ordering via Redis,
// and broadcasts SSE events via Redis Pub/Sub.
//
// Tracker is safe for concurrent use from multiple goroutines. All mutable
// state lives in Redis. The tryTransition method performs a non-atomic
// GET-check-SET sequence; this is safe because events for the same version
// are processed sequentially (one RabbitMQ consumer per queue). If the
// deployment changes to parallel consumers per version, tryTransition must
// be upgraded to a Redis Lua script or WATCH/MULTI/EXEC.
type Tracker struct {
	kv                  KVStore
	confirmStore        ConfirmationStore
	broadcaster         ssebroadcast.Broadcaster
	log                 *logger.Logger
	now                 func() time.Time
	confirmationTimeout time.Duration
}

// NewTracker creates a Tracker with the given dependencies.
func NewTracker(kv KVStore, bc ssebroadcast.Broadcaster, log *logger.Logger) *Tracker {
	return &Tracker{
		kv:          kv,
		broadcaster: bc,
		log:         log.With("component", "status-tracker"),
		now:         time.Now,
	}
}

// HandleEvent dispatches an inbound event to the appropriate per-event handler
// based on eventType and the concrete type of event.
//
// Returns nil on success or on silently-ignored events (backward transition,
// terminal already set, unrecognized event type).
// Returns a non-nil error only on transient infrastructure failure (Redis
// unavailable), signaling the consumer to NACK and requeue.
func (t *Tracker) HandleEvent(ctx context.Context, eventType consumer.EventType, event any) error {
	switch eventType {
	case consumer.EventDPStatusChanged:
		e, ok := event.(*consumer.DPStatusChangedEvent)
		if !ok {
			t.log.Warn(ctx, "unexpected event type for dp.status-changed",
				"got_type", fmt.Sprintf("%T", event))
			return nil
		}
		return t.handleDPStatusChanged(ctx, e)

	case consumer.EventDPProcessingCompleted:
		e, ok := event.(*consumer.DPProcessingCompletedEvent)
		if !ok {
			t.log.Warn(ctx, "unexpected event type for dp.processing-completed",
				"got_type", fmt.Sprintf("%T", event))
			return nil
		}
		return t.handleDPProcessingCompleted(ctx, e)

	case consumer.EventDPProcessingFailed:
		e, ok := event.(*consumer.DPProcessingFailedEvent)
		if !ok {
			t.log.Warn(ctx, "unexpected event type for dp.processing-failed",
				"got_type", fmt.Sprintf("%T", event))
			return nil
		}
		return t.handleDPProcessingFailed(ctx, e)

	case consumer.EventDPComparisonCompleted:
		e, ok := event.(*consumer.DPComparisonCompletedEvent)
		if !ok {
			t.log.Warn(ctx, "unexpected event type for dp.comparison-completed",
				"got_type", fmt.Sprintf("%T", event))
			return nil
		}
		return t.handleDPComparisonCompleted(ctx, e)

	case consumer.EventDPComparisonFailed:
		e, ok := event.(*consumer.DPComparisonFailedEvent)
		if !ok {
			t.log.Warn(ctx, "unexpected event type for dp.comparison-failed",
				"got_type", fmt.Sprintf("%T", event))
			return nil
		}
		return t.handleDPComparisonFailed(ctx, e)

	case consumer.EventLICStatusChanged:
		e, ok := event.(*consumer.LICStatusChangedEvent)
		if !ok {
			t.log.Warn(ctx, "unexpected event type for lic.status-changed",
				"got_type", fmt.Sprintf("%T", event))
			return nil
		}
		return t.handleLICStatusChanged(ctx, e)

	case consumer.EventLICClassificationUncertain:
		e, ok := event.(*consumer.LICClassificationUncertainEvent)
		if !ok {
			t.log.Warn(ctx, "unexpected event type for lic.classification-uncertain",
				"got_type", fmt.Sprintf("%T", event))
			return nil
		}
		return t.handleLICClassificationUncertain(ctx, e)

	case consumer.EventREStatusChanged:
		e, ok := event.(*consumer.REStatusChangedEvent)
		if !ok {
			t.log.Warn(ctx, "unexpected event type for re.status-changed",
				"got_type", fmt.Sprintf("%T", event))
			return nil
		}
		return t.handleREStatusChanged(ctx, e)

	case consumer.EventDMVersionArtifactsReady:
		e, ok := event.(*consumer.DMVersionArtifactsReadyEvent)
		if !ok {
			t.log.Warn(ctx, "unexpected event type for dm.version-artifacts-ready",
				"got_type", fmt.Sprintf("%T", event))
			return nil
		}
		return t.handleDMVersionArtifactsReady(ctx, e)

	case consumer.EventDMVersionAnalysisReady:
		e, ok := event.(*consumer.DMVersionAnalysisReadyEvent)
		if !ok {
			t.log.Warn(ctx, "unexpected event type for dm.version-analysis-ready",
				"got_type", fmt.Sprintf("%T", event))
			return nil
		}
		return t.handleDMVersionAnalysisReady(ctx, e)

	case consumer.EventDMVersionReportsReady:
		e, ok := event.(*consumer.DMVersionReportsReadyEvent)
		if !ok {
			t.log.Warn(ctx, "unexpected event type for dm.version-reports-ready",
				"got_type", fmt.Sprintf("%T", event))
			return nil
		}
		return t.handleDMVersionReportsReady(ctx, e)

	case consumer.EventDMVersionPartiallyAvail:
		e, ok := event.(*consumer.DMVersionPartiallyAvailableEvent)
		if !ok {
			t.log.Warn(ctx, "unexpected event type for dm.version-partially-available",
				"got_type", fmt.Sprintf("%T", event))
			return nil
		}
		return t.handleDMVersionPartiallyAvailable(ctx, e)

	case consumer.EventDMVersionCreated:
		e, ok := event.(*consumer.DMVersionCreatedEvent)
		if !ok {
			t.log.Warn(ctx, "unexpected event type for dm.version-created",
				"got_type", fmt.Sprintf("%T", event))
			return nil
		}
		return t.handleDMVersionCreated(ctx, e)

	default:
		t.log.Warn(ctx, "unrecognized event type, ignoring",
			"event_type", string(eventType))
		return nil
	}
}

// --- DP event handlers ---

// handleDPStatusChanged processes dp.events.status-changed.
//
// Mapping:
//   - "IN_PROGRESS" → PROCESSING
//   - "REJECTED"    → REJECTED
//   - "TIMED_OUT"   → FAILED (timeout is a failure variant)
//   - Others (QUEUED, COMPLETED, etc.) → informational, no transition
func (t *Tracker) handleDPStatusChanged(ctx context.Context, e *consumer.DPStatusChangedEvent) error {
	if e.OrganizationID == "" {
		t.log.Warn(ctx, "dp status-changed missing organization_id, skipping")
		return nil
	}

	var newStatus UserStatus
	var sseEvent ssebroadcast.Event

	switch e.Status {
	case "IN_PROGRESS":
		newStatus = StatusProcessing
	case "REJECTED":
		newStatus = StatusRejected
	case "TIMED_OUT":
		newStatus = StatusFailed
	default:
		t.log.Debug(ctx, "dp status-changed not mapped, ignoring",
			"dp_status", e.Status)
		return nil
	}

	transitioned, err := t.tryTransition(ctx, e.OrganizationID, e.DocumentID, e.VersionID, newStatus)
	if err != nil {
		return err
	}
	if !transitioned {
		return nil
	}

	sseEvent = t.buildStatusUpdateEvent(e.DocumentID, e.VersionID, e.JobID, newStatus)

	if e.Status == "TIMED_OUT" {
		sseEvent.ErrorCode = "TIMED_OUT"
		sseEvent.ErrorMessage = e.Message
		if sseEvent.ErrorMessage == "" {
			sseEvent.ErrorMessage = "Превышено время обработки"
		}
		sseEvent.IsRetryable = true
	} else if e.Status == "REJECTED" {
		sseEvent.ErrorCode = "REJECTED"
		sseEvent.ErrorMessage = e.Message
		if sseEvent.ErrorMessage == "" {
			sseEvent.ErrorMessage = "Файл отклонён"
		}
	}

	_ = t.broadcaster.Broadcast(ctx, e.OrganizationID, sseEvent)
	return nil
}

// handleDPProcessingCompleted processes dp.events.processing-completed.
// Informational only — the actual PROCESSING → ANALYZING transition is
// triggered by dm.version-artifacts-ready.
func (t *Tracker) handleDPProcessingCompleted(ctx context.Context, e *consumer.DPProcessingCompletedEvent) error {
	if len(e.Warnings) > 0 {
		t.log.Info(ctx, "dp processing completed with warnings",
			"warning_count", len(e.Warnings))
	} else {
		t.log.Debug(ctx, "dp processing completed")
	}
	return nil
}

// handleDPProcessingFailed processes dp.events.processing-failed.
// Maps to FAILED status.
func (t *Tracker) handleDPProcessingFailed(ctx context.Context, e *consumer.DPProcessingFailedEvent) error {
	if e.OrganizationID == "" {
		t.log.Warn(ctx, "dp processing-failed missing organization_id, skipping")
		return nil
	}

	transitioned, err := t.tryTransition(ctx, e.OrganizationID, e.DocumentID, e.VersionID, StatusFailed)
	if err != nil {
		return err
	}
	if !transitioned {
		return nil
	}

	sseEvent := t.buildStatusUpdateEvent(e.DocumentID, e.VersionID, e.JobID, StatusFailed)
	sseEvent.ErrorCode = e.ErrorCode
	sseEvent.ErrorMessage = e.ErrorMessage
	sseEvent.IsRetryable = e.IsRetryable

	_ = t.broadcaster.Broadcast(ctx, e.OrganizationID, sseEvent)
	return nil
}

// --- DP comparison event handlers ---

// handleDPComparisonCompleted processes dp.events.comparison-completed.
// Broadcasts a comparison_update SSE event. No version status transition.
func (t *Tracker) handleDPComparisonCompleted(ctx context.Context, e *consumer.DPComparisonCompletedEvent) error {
	if e.OrganizationID == "" {
		t.log.Warn(ctx, "dp comparison-completed missing organization_id, skipping")
		return nil
	}

	sseEvent := ssebroadcast.Event{
		EventType:       "comparison_update",
		DocumentID:      e.DocumentID,
		JobID:           e.JobID,
		Status:          "COMPARISON_COMPLETED",
		Message:         "Сравнение версий завершено",
		Timestamp:       t.now().UTC().Format(time.RFC3339),
		BaseVersionID:   e.BaseVersionID,
		TargetVersionID: e.TargetVersionID,
	}

	_ = t.broadcaster.Broadcast(ctx, e.OrganizationID, sseEvent)
	return nil
}

// handleDPComparisonFailed processes dp.events.comparison-failed.
// Broadcasts a comparison_update SSE event with error details.
func (t *Tracker) handleDPComparisonFailed(ctx context.Context, e *consumer.DPComparisonFailedEvent) error {
	if e.OrganizationID == "" {
		t.log.Warn(ctx, "dp comparison-failed missing organization_id, skipping")
		return nil
	}

	sseEvent := ssebroadcast.Event{
		EventType:       "comparison_update",
		DocumentID:      e.DocumentID,
		JobID:           e.JobID,
		Status:          "COMPARISON_FAILED",
		Message:         "Ошибка сравнения версий",
		Timestamp:       t.now().UTC().Format(time.RFC3339),
		IsRetryable:     e.IsRetryable,
		ErrorCode:       e.ErrorCode,
		ErrorMessage:    e.ErrorMessage,
		BaseVersionID:   e.BaseVersionID,
		TargetVersionID: e.TargetVersionID,
	}

	_ = t.broadcaster.Broadcast(ctx, e.OrganizationID, sseEvent)
	return nil
}

// --- LIC / RE event handlers ---

// handleLICStatusChanged processes lic.events.status-changed.
//
// Mapping:
//   - "IN_PROGRESS" → ANALYZING
//   - "COMPLETED"   → no transition (wait for dm.version-analysis-ready)
//   - "FAILED"      → ANALYSIS_FAILED (immediate failure, ASSUMPTION-ORCH-13)
func (t *Tracker) handleLICStatusChanged(ctx context.Context, e *consumer.LICStatusChangedEvent) error {
	var newStatus UserStatus

	switch e.Status {
	case "IN_PROGRESS":
		newStatus = StatusAnalyzing
	case "COMPLETED":
		t.log.Debug(ctx, "lic status-changed COMPLETED, awaiting dm.version-analysis-ready")
		return nil
	case "FAILED":
		newStatus = StatusAnalysisFailed
	default:
		t.log.Warn(ctx, "lic status-changed with unknown status, ignoring",
			"lic_status", e.Status)
		return nil
	}

	transitioned, err := t.tryTransition(ctx, e.OrganizationID, e.DocumentID, e.VersionID, newStatus)
	if err != nil {
		return err
	}
	if !transitioned {
		return nil
	}

	sseEvent := t.buildStatusUpdateEvent(e.DocumentID, e.VersionID, e.JobID, newStatus)

	if e.Status == "FAILED" {
		sseEvent.ErrorCode = e.ErrorCode
		sseEvent.ErrorMessage = e.ErrorMessage
		sseEvent.IsRetryable = derefBool(e.IsRetryable)
	}

	_ = t.broadcaster.Broadcast(ctx, e.OrganizationID, sseEvent)
	return nil
}

// handleREStatusChanged processes re.events.status-changed.
//
// Mapping:
//   - "IN_PROGRESS" → GENERATING_REPORTS
//   - "COMPLETED"   → no transition (wait for dm.version-reports-ready)
//   - "FAILED"      → REPORTS_FAILED (immediate failure)
func (t *Tracker) handleREStatusChanged(ctx context.Context, e *consumer.REStatusChangedEvent) error {
	var newStatus UserStatus

	switch e.Status {
	case "IN_PROGRESS":
		newStatus = StatusGeneratingReports
	case "COMPLETED":
		t.log.Debug(ctx, "re status-changed COMPLETED, awaiting dm.version-reports-ready")
		return nil
	case "FAILED":
		newStatus = StatusReportsFailed
	default:
		t.log.Warn(ctx, "re status-changed with unknown status, ignoring",
			"re_status", e.Status)
		return nil
	}

	transitioned, err := t.tryTransition(ctx, e.OrganizationID, e.DocumentID, e.VersionID, newStatus)
	if err != nil {
		return err
	}
	if !transitioned {
		return nil
	}

	sseEvent := t.buildStatusUpdateEvent(e.DocumentID, e.VersionID, e.JobID, newStatus)

	if e.Status == "FAILED" {
		sseEvent.ErrorCode = e.ErrorCode
		sseEvent.ErrorMessage = e.ErrorMessage
		sseEvent.IsRetryable = derefBool(e.IsRetryable)
	}

	_ = t.broadcaster.Broadcast(ctx, e.OrganizationID, sseEvent)
	return nil
}

// --- DM event handlers ---

// handleDMVersionArtifactsReady processes dm.version-artifacts-ready.
// Transitions to ANALYZING (DP artifacts persisted, LIC can begin).
func (t *Tracker) handleDMVersionArtifactsReady(ctx context.Context, e *consumer.DMVersionArtifactsReadyEvent) error {
	return t.handleDMStatusEvent(ctx, e.OrganizationID, e.DocumentID, e.VersionID, StatusAnalyzing)
}

// handleDMVersionAnalysisReady processes dm.version-analysis-ready.
// Transitions to GENERATING_REPORTS (LIC analysis persisted, RE can begin).
func (t *Tracker) handleDMVersionAnalysisReady(ctx context.Context, e *consumer.DMVersionAnalysisReadyEvent) error {
	return t.handleDMStatusEvent(ctx, e.OrganizationID, e.DocumentID, e.VersionID, StatusGeneratingReports)
}

// handleDMVersionReportsReady processes dm.version-reports-ready.
// Transitions to READY (terminal happy path).
func (t *Tracker) handleDMVersionReportsReady(ctx context.Context, e *consumer.DMVersionReportsReadyEvent) error {
	return t.handleDMStatusEvent(ctx, e.OrganizationID, e.DocumentID, e.VersionID, StatusReady)
}

// handleDMVersionPartiallyAvailable processes dm.version-partially-available.
// Transitions to PARTIALLY_FAILED (terminal).
func (t *Tracker) handleDMVersionPartiallyAvailable(ctx context.Context, e *consumer.DMVersionPartiallyAvailableEvent) error {
	transitioned, err := t.tryTransition(ctx, e.OrganizationID, e.DocumentID, e.VersionID, StatusPartiallyFailed)
	if err != nil {
		return err
	}
	if !transitioned {
		return nil
	}

	sseEvent := t.buildStatusUpdateEvent(e.DocumentID, e.VersionID, "", StatusPartiallyFailed)
	sseEvent.ErrorCode = e.FailedStage
	sseEvent.ErrorMessage = e.ErrorMessage

	_ = t.broadcaster.Broadcast(ctx, e.OrganizationID, sseEvent)
	return nil
}

// handleDMVersionCreated processes dm.version-created.
// No version status transition. For RE_CHECK origin, broadcasts a
// version_created SSE event so the frontend can update its UI.
func (t *Tracker) handleDMVersionCreated(ctx context.Context, e *consumer.DMVersionCreatedEvent) error {
	if e.OrganizationID == "" {
		t.log.Warn(ctx, "dm version-created missing organization_id, skipping")
		return nil
	}

	if e.OriginType == "RE_CHECK" {
		sseEvent := ssebroadcast.Event{
			EventType:  "version_created",
			DocumentID: e.DocumentID,
			VersionID:  e.VersionID,
			Status:     "VERSION_CREATED",
			Message:    "Создана новая версия для повторной проверки",
			Timestamp:  t.now().UTC().Format(time.RFC3339),
		}
		_ = t.broadcaster.Broadcast(ctx, e.OrganizationID, sseEvent)
	} else {
		t.log.Debug(ctx, "dm version-created, no broadcast needed",
			"origin_type", e.OriginType)
	}
	return nil
}

// --- Internal helpers ---

// handleDMStatusEvent is a shared helper for DM events that trigger a simple
// status transition with no error fields. DM events do not carry job_id.
func (t *Tracker) handleDMStatusEvent(
	ctx context.Context,
	orgID, docID, verID string,
	newStatus UserStatus,
) error {
	transitioned, err := t.tryTransition(ctx, orgID, docID, verID, newStatus)
	if err != nil {
		return err
	}
	if !transitioned {
		return nil
	}

	sseEvent := t.buildStatusUpdateEvent(docID, verID, "", newStatus)
	_ = t.broadcaster.Broadcast(ctx, orgID, sseEvent)
	return nil
}

// buildStatusUpdateEvent creates an ssebroadcast.Event with event_type "status_update"
// and the Russian status message.
func (t *Tracker) buildStatusUpdateEvent(docID, verID, jobID string, status UserStatus) ssebroadcast.Event {
	msg := statusMessages[status]
	if msg == "" {
		msg = string(status)
	}
	return ssebroadcast.Event{
		EventType:  "status_update",
		DocumentID: docID,
		VersionID:  verID,
		JobID:      jobID,
		Status:     string(status),
		Message:    msg,
		Timestamp:  t.now().UTC().Format(time.RFC3339),
	}
}

// tryTransition attempts to transition the version's status to newStatus.
//
// Returns (true, nil) if the transition was applied.
// Returns (false, nil) if silently skipped (backward/terminal/missing IDs).
// Returns (false, error) if Redis is unavailable (transient).
//
// Note on atomicity: this is not a Redis transaction (WATCH/MULTI/EXEC).
// In the current architecture, events for the same version are processed
// sequentially by one consumer (RabbitMQ delivers to a single queue with
// one consumer goroutine per topic, and events for the same version arrive
// in order within each topic). If horizontal scaling introduces parallel
// consumers for the same version, this must be upgraded to a Lua script
// or WATCH/MULTI/EXEC.
func (t *Tracker) tryTransition(
	ctx context.Context,
	orgID, docID, verID string,
	newStatus UserStatus,
) (bool, error) {
	if orgID == "" || docID == "" || verID == "" {
		t.log.Warn(ctx, "missing identity field, skipping transition",
			"org_id", orgID, "doc_id", docID, "ver_id", verID)
		return false, nil
	}

	key := statusKey(orgID, docID, verID)

	raw, err := t.kv.Get(ctx, key)
	if err != nil && !errors.Is(err, kvstore.ErrKeyNotFound) {
		t.log.Error(ctx, "failed to read status from Redis",
			logger.ErrorAttr(err),
			"key", key)
		return false, err
	}

	// If the key exists, check monotonic ordering.
	if err == nil {
		var rec statusRecord
		if jsonErr := json.Unmarshal([]byte(raw), &rec); jsonErr != nil {
			t.log.Warn(ctx, "corrupt status record in Redis, overwriting",
				"key", key,
				"raw", raw,
				"new_status", string(newStatus),
				logger.ErrorAttr(jsonErr))
			// Treat as no existing status — allow the write.
		} else {
			current := UserStatus(rec.Status)
			if !canTransition(current, newStatus) {
				t.log.Debug(ctx, "status transition skipped",
					"key", key,
					"current", string(current),
					"requested", string(newStatus))
				return false, nil
			}
		}
	}

	// Write the new status.
	rec := statusRecord{
		Status:    string(newStatus),
		UpdatedAt: t.now().UTC().Format(time.RFC3339),
	}
	data, _ := json.Marshal(rec) // statusRecord marshalling cannot fail.

	if err := t.kv.Set(ctx, key, string(data), statusTTL); err != nil {
		t.log.Error(ctx, "failed to write status to Redis",
			logger.ErrorAttr(err),
			"key", key,
			"new_status", string(newStatus))
		return false, err
	}

	t.log.Info(ctx, "status transition applied",
		"key", key,
		"new_status", string(newStatus))
	return true, nil
}

