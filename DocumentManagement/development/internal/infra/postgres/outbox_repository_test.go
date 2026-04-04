package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"contractpro/document-management/internal/domain/port"
)

func TestNewOutboxRepository(t *testing.T) {
	repo := NewOutboxRepository()
	assert.NotNil(t, repo)
}

func TestOutboxRepository_Insert_NoEntries(t *testing.T) {
	mock := &mockTx{}
	ctx := ctxWithMockTx(mock)

	err := NewOutboxRepository().Insert(ctx)
	assert.NoError(t, err)
	assert.Empty(t, mock.getCalls(), "no DB call when zero entries")
}

func TestOutboxRepository_Insert_SingleEntry(t *testing.T) {
	mock := &mockTx{
		execFn: func(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			assert.Contains(t, sql, "INSERT INTO outbox_events")
			assert.Contains(t, sql, "($1, $2, $3, $4, $5, $6)")
			assert.Len(t, args, 6)
			return pgconn.NewCommandTag("INSERT 0 1"), nil
		},
	}
	ctx := ctxWithMockTx(mock)

	entry := port.OutboxEntry{
		ID:          "evt-1",
		AggregateID: "ver-1",
		Topic:       "dm.events.test",
		Payload:     []byte(`{"key":"value"}`),
		Status:      "PENDING",
		CreatedAt:   time.Now().UTC(),
	}
	err := NewOutboxRepository().Insert(ctx, entry)
	assert.NoError(t, err)
}

func TestOutboxRepository_Insert_MultipleEntries(t *testing.T) {
	mock := &mockTx{
		execFn: func(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			assert.Contains(t, sql, "INSERT INTO outbox_events")
			// Two entries → two value groups.
			assert.Contains(t, sql, "($1, $2, $3, $4, $5, $6)")
			assert.Contains(t, sql, "($7, $8, $9, $10, $11, $12)")
			assert.Len(t, args, 12)
			return pgconn.NewCommandTag("INSERT 0 2"), nil
		},
	}
	ctx := ctxWithMockTx(mock)

	entries := []port.OutboxEntry{
		{ID: "e1", AggregateID: "v1", Topic: "t1", Payload: []byte(`{}`), Status: "PENDING", CreatedAt: time.Now().UTC()},
		{ID: "e2", AggregateID: "v1", Topic: "t2", Payload: []byte(`{}`), Status: "PENDING", CreatedAt: time.Now().UTC()},
	}
	err := NewOutboxRepository().Insert(ctx, entries...)
	assert.NoError(t, err)
}

func TestOutboxRepository_Insert_DatabaseError(t *testing.T) {
	mock := &mockTx{
		execFn: func(_ context.Context, _ string, _ ...any) (pgconn.CommandTag, error) {
			return pgconn.NewCommandTag(""), errors.New("disk full")
		},
	}
	ctx := ctxWithMockTx(mock)

	entry := port.OutboxEntry{ID: "e1", Topic: "t", Payload: []byte(`{}`), Status: "PENDING", CreatedAt: time.Now().UTC()}
	err := NewOutboxRepository().Insert(ctx, entry)

	require.Error(t, err)
	assert.Equal(t, port.ErrCodeDatabaseFailed, port.ErrorCode(err))
}

func TestOutboxRepository_FetchUnpublished_Empty(t *testing.T) {
	mock := &mockTx{
		queryFn: func(_ context.Context, sql string, args ...any) (pgx.Rows, error) {
			assert.Contains(t, sql, "FOR UPDATE SKIP LOCKED")
			assert.Contains(t, sql, "ORDER BY aggregate_id NULLS FIRST, created_at")
			assert.Contains(t, sql, "status = 'PENDING'")
			assert.Equal(t, 50, args[0])
			return &mockRows{}, nil
		},
	}
	ctx := ctxWithMockTx(mock)

	entries, err := NewOutboxRepository().FetchUnpublished(ctx, 50)
	require.NoError(t, err)
	assert.NotNil(t, entries)
	assert.Empty(t, entries)
}

func TestOutboxRepository_FetchUnpublished_WithResults(t *testing.T) {
	now := time.Now().UTC()
	mock := &mockTx{
		queryFn: func(_ context.Context, _ string, _ ...any) (pgx.Rows, error) {
			return &mockRows{
				scanFns: []func(dest ...any) error{
					func(dest ...any) error {
						*dest[0].(*string) = "evt-1"
						*dest[1].(**string) = strPtr("ver-1")
						*dest[2].(*string) = "dm.events.test"
						*dest[3].(*[]byte) = []byte(`{"key":"value"}`)
						*dest[4].(*string) = "PENDING"
						*dest[5].(*time.Time) = now
						*dest[6].(**time.Time) = nil
						return nil
					},
				},
			}, nil
		},
	}
	ctx := ctxWithMockTx(mock)

	entries, err := NewOutboxRepository().FetchUnpublished(ctx, 10)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, "evt-1", entries[0].ID)
	assert.Equal(t, "ver-1", entries[0].AggregateID)
	assert.Equal(t, "dm.events.test", entries[0].Topic)
	assert.Equal(t, "PENDING", entries[0].Status)
	assert.True(t, entries[0].PublishedAt.IsZero())
}

func TestOutboxRepository_FetchUnpublished_DatabaseError(t *testing.T) {
	mock := &mockTx{
		queryFn: func(_ context.Context, _ string, _ ...any) (pgx.Rows, error) {
			return nil, errors.New("connection timeout")
		},
	}
	ctx := ctxWithMockTx(mock)

	_, err := NewOutboxRepository().FetchUnpublished(ctx, 10)
	require.Error(t, err)
	assert.Equal(t, port.ErrCodeDatabaseFailed, port.ErrorCode(err))
}

func TestOutboxRepository_MarkPublished_NoIDs(t *testing.T) {
	mock := &mockTx{}
	ctx := ctxWithMockTx(mock)

	err := NewOutboxRepository().MarkPublished(ctx, []string{})
	assert.NoError(t, err)
	assert.Empty(t, mock.getCalls(), "no DB call when empty IDs")
}

func TestOutboxRepository_MarkPublished_Success(t *testing.T) {
	mock := &mockTx{
		execFn: func(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			assert.Contains(t, sql, "UPDATE outbox_events")
			assert.Contains(t, sql, "status = 'CONFIRMED'")
			assert.Contains(t, sql, "published_at = now()")
			assert.Contains(t, sql, "ANY($1)")
			assert.Len(t, args, 1)
			ids, ok := args[0].([]string)
			assert.True(t, ok)
			assert.ElementsMatch(t, []string{"e1", "e2"}, ids)
			return pgconn.NewCommandTag("UPDATE 2"), nil
		},
	}
	ctx := ctxWithMockTx(mock)

	err := NewOutboxRepository().MarkPublished(ctx, []string{"e1", "e2"})
	assert.NoError(t, err)
}

func TestOutboxRepository_DeletePublished_NoLimit(t *testing.T) {
	mock := &mockTx{
		execFn: func(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			assert.Contains(t, sql, "DELETE FROM outbox_events")
			assert.Contains(t, sql, "status = 'CONFIRMED'")
			assert.Contains(t, sql, "published_at < $1")
			assert.NotContains(t, sql, "LIMIT")
			assert.Len(t, args, 1, "no limit param when limit=0")
			return pgconn.NewCommandTag("DELETE 42"), nil
		},
	}
	ctx := ctxWithMockTx(mock)

	count, err := NewOutboxRepository().DeletePublished(ctx, time.Now().Add(-48*time.Hour), 0)
	require.NoError(t, err)
	assert.Equal(t, int64(42), count)
}

func TestOutboxRepository_DeletePublished_WithLimit(t *testing.T) {
	mock := &mockTx{
		execFn: func(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			assert.Contains(t, sql, "DELETE FROM outbox_events")
			assert.Contains(t, sql, "LIMIT $2")
			assert.Len(t, args, 2)
			assert.Equal(t, 1000, args[1])
			return pgconn.NewCommandTag("DELETE 500"), nil
		},
	}
	ctx := ctxWithMockTx(mock)

	count, err := NewOutboxRepository().DeletePublished(ctx, time.Now().Add(-48*time.Hour), 1000)
	require.NoError(t, err)
	assert.Equal(t, int64(500), count)
}

func TestOutboxRepository_DeletePublished_DatabaseError(t *testing.T) {
	mock := &mockTx{
		execFn: func(_ context.Context, _ string, _ ...any) (pgconn.CommandTag, error) {
			return pgconn.NewCommandTag(""), errors.New("i/o error")
		},
	}
	ctx := ctxWithMockTx(mock)

	_, err := NewOutboxRepository().DeletePublished(ctx, time.Now(), 0)
	require.Error(t, err)
	assert.Equal(t, port.ErrCodeDatabaseFailed, port.ErrorCode(err))
}

func TestOutboxRepository_PendingStats_WithResults(t *testing.T) {
	mock := &mockTx{
		queryRowFn: func(_ context.Context, sql string, _ ...any) pgx.Row {
			assert.Contains(t, sql, "COUNT(*)")
			assert.Contains(t, sql, "EXTRACT(EPOCH FROM")
			assert.Contains(t, sql, "status = 'PENDING'")
			return &mockRow{
				scanFn: func(dest ...any) error {
					*dest[0].(*int64) = 15
					*dest[1].(*float64) = 120.5
					return nil
				},
			}
		},
	}
	ctx := ctxWithMockTx(mock)

	count, age, err := NewOutboxRepository().PendingStats(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(15), count)
	assert.InDelta(t, 120.5, age, 0.001)
}

func TestOutboxRepository_PendingStats_Empty(t *testing.T) {
	mock := &mockTx{
		queryRowFn: func(_ context.Context, _ string, _ ...any) pgx.Row {
			return &mockRow{
				scanFn: func(dest ...any) error {
					*dest[0].(*int64) = 0
					*dest[1].(*float64) = 0
					return nil
				},
			}
		},
	}
	ctx := ctxWithMockTx(mock)

	count, age, err := NewOutboxRepository().PendingStats(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(0), count)
	assert.Equal(t, float64(0), age)
}

func TestOutboxRepository_PendingStats_DatabaseError(t *testing.T) {
	mock := &mockTx{
		queryRowFn: func(_ context.Context, _ string, _ ...any) pgx.Row {
			return &mockRow{err: errors.New("connection lost")}
		},
	}
	ctx := ctxWithMockTx(mock)

	_, _, err := NewOutboxRepository().PendingStats(ctx)
	require.Error(t, err)
	assert.Equal(t, port.ErrCodeDatabaseFailed, port.ErrorCode(err))
}

func TestOutboxRepository_Insert_EmptyAggregateID(t *testing.T) {
	var capturedArgs []any
	mock := &mockTx{
		execFn: func(_ context.Context, _ string, args ...any) (pgconn.CommandTag, error) {
			capturedArgs = args
			return pgconn.NewCommandTag("INSERT 0 1"), nil
		},
	}
	ctx := ctxWithMockTx(mock)

	entry := port.OutboxEntry{
		ID:      "e1",
		Topic:   "t",
		Payload: []byte(`{}`),
		Status:  "PENDING",
		CreatedAt: time.Now().UTC(),
	}
	err := NewOutboxRepository().Insert(ctx, entry)
	require.NoError(t, err)
	// AggregateID is empty → should be nil (SQL NULL).
	assert.Nil(t, capturedArgs[1], "empty AggregateID should become SQL NULL")
}
