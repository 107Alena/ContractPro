package outbox

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"contractpro/document-management/internal/domain/port"
)

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestNewOutboxWriter_NilRepoPanics(t *testing.T) {
	assert.Panics(t, func() {
		NewOutboxWriter(nil)
	})
}

func TestOutboxWriter_Write_HappyPath(t *testing.T) {
	repo := &writerMockRepo{}
	w := NewOutboxWriter(repo)

	type testEvent struct {
		Key string `json:"key"`
	}

	err := w.Write(context.Background(), "ver-123", "dm.events.test", testEvent{Key: "value"})
	require.NoError(t, err)
	require.Len(t, repo.insertCalls, 1)
	require.Len(t, repo.insertCalls[0], 1)

	entry := repo.insertCalls[0][0]
	assert.NotEmpty(t, entry.ID, "UUID should be generated")
	assert.Equal(t, "ver-123", entry.AggregateID)
	assert.Equal(t, "dm.events.test", entry.Topic)
	assert.Equal(t, "PENDING", entry.Status)
	assert.False(t, entry.CreatedAt.IsZero())

	// Verify JSON payload.
	var decoded testEvent
	require.NoError(t, json.Unmarshal(entry.Payload, &decoded))
	assert.Equal(t, "value", decoded.Key)
}

func TestOutboxWriter_Write_EmptyAggregateID(t *testing.T) {
	repo := &writerMockRepo{}
	w := NewOutboxWriter(repo)

	err := w.Write(context.Background(), "", "dm.events.test", map[string]string{"a": "b"})
	require.NoError(t, err)
	require.Len(t, repo.insertCalls, 1)
	assert.Empty(t, repo.insertCalls[0][0].AggregateID)
}

func TestOutboxWriter_Write_MarshalError(t *testing.T) {
	repo := &writerMockRepo{}
	w := NewOutboxWriter(repo)

	// Channels cannot be marshaled to JSON.
	err := w.Write(context.Background(), "ver-1", "topic", make(chan int))
	require.Error(t, err)
	assert.Equal(t, port.ErrCodeValidation, port.ErrorCode(err))
	assert.False(t, port.IsRetryable(err))
	assert.Empty(t, repo.insertCalls, "repo should not be called on marshal error")
}

func TestOutboxWriter_Write_RepositoryError(t *testing.T) {
	dbErr := port.NewDatabaseError("insert failed", errors.New("disk full"))
	repo := &writerMockRepo{
		insertFn: func(_ context.Context, _ ...port.OutboxEntry) error {
			return dbErr
		},
	}
	w := NewOutboxWriter(repo)

	err := w.Write(context.Background(), "ver-1", "topic", "event")
	require.Error(t, err)
	assert.Equal(t, port.ErrCodeDatabaseFailed, port.ErrorCode(err))
}

func TestOutboxWriter_WriteMultiple_HappyPath(t *testing.T) {
	repo := &writerMockRepo{}
	w := NewOutboxWriter(repo)

	items := []TopicEvent{
		{Topic: "dm.responses.artifacts-persisted", Event: map[string]string{"job_id": "j1"}},
		{Topic: "dm.events.version-artifacts-ready", Event: map[string]string{"version_id": "v1"}},
		{Topic: "dm.events.version-created", Event: map[string]string{"doc_id": "d1"}},
	}

	err := w.WriteMultiple(context.Background(), "ver-1", items)
	require.NoError(t, err)
	require.Len(t, repo.insertCalls, 1)
	require.Len(t, repo.insertCalls[0], 3)

	for i, entry := range repo.insertCalls[0] {
		assert.NotEmpty(t, entry.ID)
		assert.Equal(t, "ver-1", entry.AggregateID)
		assert.Equal(t, items[i].Topic, entry.Topic)
		assert.Equal(t, "PENDING", entry.Status)
	}

	// All entries should share the same CreatedAt (batched).
	assert.Equal(t, repo.insertCalls[0][0].CreatedAt, repo.insertCalls[0][1].CreatedAt)
	assert.Equal(t, repo.insertCalls[0][0].CreatedAt, repo.insertCalls[0][2].CreatedAt)
}

func TestOutboxWriter_WriteMultiple_EmptyItems(t *testing.T) {
	repo := &writerMockRepo{}
	w := NewOutboxWriter(repo)

	err := w.WriteMultiple(context.Background(), "ver-1", nil)
	require.NoError(t, err)
	assert.Empty(t, repo.insertCalls, "repo should not be called for empty items")
}

func TestOutboxWriter_WriteMultiple_MarshalError(t *testing.T) {
	repo := &writerMockRepo{}
	w := NewOutboxWriter(repo)

	items := []TopicEvent{
		{Topic: "topic-ok", Event: "valid"},
		{Topic: "topic-bad", Event: make(chan int)},
	}

	err := w.WriteMultiple(context.Background(), "ver-1", items)
	require.Error(t, err)
	assert.Equal(t, port.ErrCodeValidation, port.ErrorCode(err))
	assert.Empty(t, repo.insertCalls, "repo should not be called on marshal error")
}

func TestOutboxWriter_Write_EmptyTopicError(t *testing.T) {
	repo := &writerMockRepo{}
	w := NewOutboxWriter(repo)

	err := w.Write(context.Background(), "ver-1", "", "event")
	require.Error(t, err)
	assert.Equal(t, port.ErrCodeValidation, port.ErrorCode(err))
	assert.Empty(t, repo.insertCalls)
}

func TestOutboxWriter_WriteMultiple_EmptyTopicError(t *testing.T) {
	repo := &writerMockRepo{}
	w := NewOutboxWriter(repo)

	items := []TopicEvent{
		{Topic: "valid-topic", Event: "ok"},
		{Topic: "", Event: "bad"},
	}

	err := w.WriteMultiple(context.Background(), "ver-1", items)
	require.Error(t, err)
	assert.Equal(t, port.ErrCodeValidation, port.ErrorCode(err))
	assert.Empty(t, repo.insertCalls)
}

func TestOutboxWriter_Write_UUIDUniqueness(t *testing.T) {
	repo := &writerMockRepo{}
	w := NewOutboxWriter(repo)

	for i := 0; i < 10; i++ {
		_ = w.Write(context.Background(), "", "topic", "event")
	}

	ids := make(map[string]bool)
	for _, call := range repo.insertCalls {
		for _, entry := range call {
			assert.False(t, ids[entry.ID], "duplicate UUID detected: %s", entry.ID)
			ids[entry.ID] = true
		}
	}
	assert.Len(t, ids, 10)
}

// ---------------------------------------------------------------------------
// writerMockRepo — minimal mock for writer tests.
// ---------------------------------------------------------------------------

var _ port.OutboxRepository = (*writerMockRepo)(nil)

type writerMockRepo struct {
	insertFn    func(ctx context.Context, entries ...port.OutboxEntry) error
	insertCalls [][]port.OutboxEntry
}

func (m *writerMockRepo) Insert(ctx context.Context, entries ...port.OutboxEntry) error {
	m.insertCalls = append(m.insertCalls, entries)
	if m.insertFn != nil {
		return m.insertFn(ctx, entries...)
	}
	return nil
}

func (m *writerMockRepo) FetchUnpublished(context.Context, int) ([]port.OutboxEntry, error) {
	return nil, nil
}

func (m *writerMockRepo) MarkPublished(context.Context, []string) error {
	return nil
}

func (m *writerMockRepo) DeletePublished(_ context.Context, _ time.Time, _ int) (int64, error) {
	return 0, nil
}

func (m *writerMockRepo) PendingStats(context.Context) (int64, float64, error) {
	return 0, 0, nil
}
