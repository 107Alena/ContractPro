package outbox

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"time"

	"contractpro/document-management/internal/domain/port"
)

// OutboxWriter inserts events into the outbox_events table within an existing
// database transaction managed by the caller.
//
// Usage in an application service:
//
//	err := transactor.WithTransaction(ctx, func(txCtx context.Context) error {
//	    if err := versionRepo.Insert(txCtx, version); err != nil { return err }
//	    if err := auditRepo.Insert(txCtx, record); err != nil { return err }
//	    return outboxWriter.Write(txCtx, version.ID, topic, event)
//	})
//
// The txCtx carries the pgx.Tx; OutboxRepository.Insert uses ConnFromCtx
// to pick it up. The caller (Transactor) commits or rolls back.
type OutboxWriter struct {
	repo port.OutboxRepository
}

// NewOutboxWriter creates an OutboxWriter backed by the given repository.
// Panics if repo is nil (programmer error at startup wiring).
func NewOutboxWriter(repo port.OutboxRepository) *OutboxWriter {
	if repo == nil {
		panic("outbox: repository must not be nil")
	}
	return &OutboxWriter{repo: repo}
}

// Write serializes event to JSON and inserts a single PENDING outbox entry
// within the current transaction.
//
// aggregateID is the FIFO partition key (= version_id per REV-010).
// An empty aggregateID is allowed for events that do not need ordering.
//
// topic is the RabbitMQ routing key where the poller will publish the event.
// Must not be empty.
//
// event is any JSON-serializable Go struct. A marshal failure returns a
// non-retryable DomainError (deterministic, not transient).
func (w *OutboxWriter) Write(ctx context.Context, aggregateID, topic string, event any) error {
	if topic == "" {
		return port.NewValidationError("outbox: topic must not be empty")
	}

	payload, err := json.Marshal(event)
	if err != nil {
		return &port.DomainError{
			Code:      port.ErrCodeValidation,
			Message:   fmt.Sprintf("outbox: marshal event for topic %s: %v", topic, err),
			Retryable: false,
			Cause:     err,
		}
	}

	entry := port.OutboxEntry{
		ID:          newUUID(),
		AggregateID: aggregateID,
		Topic:       topic,
		Payload:     payload,
		Status:      StatusPending,
		CreatedAt:   time.Now().UTC(),
	}

	return w.repo.Insert(ctx, entry)
}

// WriteMultiple inserts multiple outbox entries atomically within the current
// transaction. All entries share the same aggregateID for FIFO ordering.
// This is useful when an application service needs to emit both a confirmation
// and a notification event in the same transaction.
func (w *OutboxWriter) WriteMultiple(ctx context.Context, aggregateID string, items []TopicEvent) error {
	if len(items) == 0 {
		return nil
	}

	entries := make([]port.OutboxEntry, 0, len(items))
	now := time.Now().UTC()
	for _, item := range items {
		if item.Topic == "" {
			return port.NewValidationError("outbox: topic must not be empty")
		}
		payload, err := json.Marshal(item.Event)
		if err != nil {
			return &port.DomainError{
				Code:      port.ErrCodeValidation,
				Message:   fmt.Sprintf("outbox: marshal event for topic %s: %v", item.Topic, err),
				Retryable: false,
				Cause:     err,
			}
		}
		entries = append(entries, port.OutboxEntry{
			ID:          newUUID(),
			AggregateID: aggregateID,
			Topic:       item.Topic,
			Payload:     payload,
			Status:      StatusPending,
			CreatedAt:   now,
		})
	}

	return w.repo.Insert(ctx, entries...)
}

// TopicEvent pairs a broker topic with a JSON-serializable event.
type TopicEvent struct {
	Topic string
	Event any
}

// newUUID generates a UUID v4 string using crypto/rand.
// Panics if crypto/rand fails — this indicates a broken system CSPRNG,
// which is a fatal condition that cannot be recovered from.
func newUUID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("outbox: crypto/rand failed: " + err.Error())
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
