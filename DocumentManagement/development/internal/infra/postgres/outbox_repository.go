package postgres

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"contractpro/document-management/internal/domain/port"
)

// Compile-time proof that OutboxRepository satisfies port.OutboxRepository.
var _ port.OutboxRepository = (*OutboxRepository)(nil)

// OutboxRepository implements port.OutboxRepository backed by PostgreSQL.
//
// The outbox relay operates at the infrastructure level: it publishes events
// for all tenants. Tenant isolation is enforced by application services that
// write entries, not by the relay that publishes them.
type OutboxRepository struct{}

// NewOutboxRepository creates a new OutboxRepository.
func NewOutboxRepository() *OutboxRepository {
	return &OutboxRepository{}
}

// Insert writes one or more outbox entries within the current transaction.
func (r *OutboxRepository) Insert(ctx context.Context, entries ...port.OutboxEntry) error {
	if len(entries) == 0 {
		return nil
	}

	conn := ConnFromCtx(ctx)

	// Build multi-row INSERT for atomicity and efficiency.
	const colsPerRow = 6
	var sb strings.Builder
	sb.WriteString(`INSERT INTO outbox_events (event_id, aggregate_id, topic, payload, status, created_at) VALUES `)

	args := make([]any, 0, len(entries)*colsPerRow)
	for i, e := range entries {
		if i > 0 {
			sb.WriteString(", ")
		}
		base := i * colsPerRow
		sb.WriteString(fmt.Sprintf("($%d, $%d, $%d, $%d, $%d, $%d)",
			base+1, base+2, base+3, base+4, base+5, base+6))
		args = append(args, e.ID, nullableString(e.AggregateID), e.Topic, e.Payload, e.Status, e.CreatedAt)
	}

	_, err := conn.Exec(ctx, sb.String(), args...)
	if err != nil {
		return port.NewDatabaseError("insert outbox entries", err)
	}
	return nil
}

// FetchUnpublished retrieves up to limit outbox entries that have not been
// published yet, using FOR UPDATE SKIP LOCKED for concurrent poller safety.
//
// FIFO caveat: SKIP LOCKED operates at the row level. When multiple pollers
// run concurrently, poller B may skip rows locked by poller A and pick up
// later entries for the same aggregate, breaking strict per-aggregate FIFO.
// For strict ordering, use a single poller or per-aggregate partitioning.
func (r *OutboxRepository) FetchUnpublished(ctx context.Context, limit int) ([]port.OutboxEntry, error) {
	conn := ConnFromCtx(ctx)

	rows, err := conn.Query(ctx,
		`SELECT event_id, aggregate_id, topic, payload, status, created_at, published_at
		FROM outbox_events
		WHERE status = 'PENDING'
		ORDER BY aggregate_id, created_at
		LIMIT $1
		FOR UPDATE SKIP LOCKED`,
		limit,
	)
	if err != nil {
		return nil, port.NewDatabaseError("fetch unpublished outbox entries", err)
	}
	defer rows.Close()

	var entries []port.OutboxEntry
	for rows.Next() {
		var (
			e           port.OutboxEntry
			aggregateID *string
			publishedAt *time.Time
		)
		if err := rows.Scan(
			&e.ID, &aggregateID, &e.Topic, &e.Payload, &e.Status, &e.CreatedAt, &publishedAt,
		); err != nil {
			return nil, port.NewDatabaseError("scan outbox entry", err)
		}
		e.AggregateID = fromNullableString(aggregateID)
		if publishedAt != nil {
			e.PublishedAt = *publishedAt
		}
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return nil, port.NewDatabaseError("iterate outbox rows", err)
	}

	if entries == nil {
		entries = []port.OutboxEntry{}
	}
	return entries, nil
}

// MarkPublished marks the specified outbox entries as published (CONFIRMED).
func (r *OutboxRepository) MarkPublished(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}

	conn := ConnFromCtx(ctx)

	_, err := conn.Exec(ctx,
		`UPDATE outbox_events
		SET status = 'CONFIRMED', published_at = now()
		WHERE event_id = ANY($1) AND status = 'PENDING'`,
		ids,
	)
	if err != nil {
		return port.NewDatabaseError("mark outbox entries published", err)
	}
	return nil
}

// DeletePublished removes up to limit entries marked as published that are
// older than the given threshold. A limit of 0 means delete all matching
// entries (no limit). Batched deletion avoids long-running transactions and
// excessive lock contention (BRE-018).
func (r *OutboxRepository) DeletePublished(ctx context.Context, olderThan time.Time, limit int) (int64, error) {
	conn := ConnFromCtx(ctx)

	var (
		tag pgconn.CommandTag
		err error
	)

	if limit > 0 {
		tag, err = conn.Exec(ctx,
			`DELETE FROM outbox_events
			WHERE event_id IN (
				SELECT event_id FROM outbox_events
				WHERE status = 'CONFIRMED' AND published_at < $1
				ORDER BY published_at
				LIMIT $2
			)`,
			olderThan, limit,
		)
	} else {
		tag, err = conn.Exec(ctx,
			`DELETE FROM outbox_events
			WHERE status = 'CONFIRMED' AND published_at < $1`,
			olderThan,
		)
	}

	if err != nil {
		return 0, port.NewDatabaseError("delete published outbox entries", err)
	}
	return tag.RowsAffected(), nil
}

// PendingStats returns the count of PENDING entries and the age in seconds
// of the oldest PENDING entry. Returns (0, 0, nil) if there are no pending
// entries. Uses the idx_outbox_pending partial index for efficiency (REV-022).
func (r *OutboxRepository) PendingStats(ctx context.Context) (int64, float64, error) {
	conn := ConnFromCtx(ctx)

	var (
		count    int64
		ageSecs  float64
	)

	err := conn.QueryRow(ctx,
		`SELECT
			COUNT(*),
			COALESCE(EXTRACT(EPOCH FROM (now() - MIN(created_at))), 0)
		FROM outbox_events
		WHERE status = 'PENDING'`,
	).Scan(&count, &ageSecs)

	if err != nil {
		return 0, 0, port.NewDatabaseError("query outbox pending stats", err)
	}
	return count, ageSecs, nil
}
