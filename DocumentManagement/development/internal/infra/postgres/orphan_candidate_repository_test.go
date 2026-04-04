package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"contractpro/document-management/internal/domain/port"
)

func TestNewOrphanCandidateRepository(t *testing.T) {
	repo := NewOrphanCandidateRepository()
	assert.NotNil(t, repo)
}

func TestOrphanCandidateRepository_FindOlderThan_Success(t *testing.T) {
	now := time.Now().UTC()
	createdAt1 := now.Add(-2 * time.Hour)
	createdAt2 := now.Add(-3 * time.Hour)
	v1 := "v1"

	rows := &mockRows{
		scanFns: []func(dest ...any) error{
			func(dest ...any) error {
				*(dest[0].(*string)) = "org/doc/v1/ocr_text"
				*(dest[1].(**string)) = &v1
				*(dest[2].(*time.Time)) = createdAt1
				return nil
			},
			func(dest ...any) error {
				*(dest[0].(*string)) = "org/doc/v2/tree"
				*(dest[1].(**string)) = nil
				*(dest[2].(*time.Time)) = createdAt2
				return nil
			},
		},
	}
	mock := &mockTx{
		queryFn: func(_ context.Context, sql string, args ...any) (pgx.Rows, error) {
			assert.Contains(t, sql, "FROM orphan_candidates")
			assert.Contains(t, sql, "WHERE created_at < $1")
			assert.Contains(t, sql, "ORDER BY created_at ASC")
			assert.Contains(t, sql, "LIMIT $2")
			assert.Len(t, args, 2)
			return rows, nil
		},
	}
	ctx := ctxWithMockTx(mock)

	result, err := NewOrphanCandidateRepository().FindOlderThan(ctx, now, 100)
	require.NoError(t, err)
	require.Len(t, result, 2)
	assert.Equal(t, "org/doc/v1/ocr_text", result[0].StorageKey)
	assert.Equal(t, "v1", result[0].VersionID)
	assert.Equal(t, "org/doc/v2/tree", result[1].StorageKey)
	assert.Empty(t, result[1].VersionID) // nil version_id → ""
}

func TestOrphanCandidateRepository_FindOlderThan_Empty(t *testing.T) {
	rows := &mockRows{scanFns: nil}
	mock := &mockTx{
		queryFn: func(_ context.Context, _ string, _ ...any) (pgx.Rows, error) {
			return rows, nil
		},
	}
	ctx := ctxWithMockTx(mock)

	result, err := NewOrphanCandidateRepository().FindOlderThan(ctx, time.Now(), 100)
	require.NoError(t, err)
	require.NotNil(t, result) // Empty slice, not nil.
	assert.Len(t, result, 0)
}

func TestOrphanCandidateRepository_FindOlderThan_DBError(t *testing.T) {
	mock := &mockTx{
		queryFn: func(_ context.Context, _ string, _ ...any) (pgx.Rows, error) {
			return nil, errors.New("connection reset")
		},
	}
	ctx := ctxWithMockTx(mock)

	_, err := NewOrphanCandidateRepository().FindOlderThan(ctx, time.Now(), 100)
	require.Error(t, err)
	assert.True(t, port.IsRetryable(err))
}

func TestOrphanCandidateRepository_ExistsByStorageKey_Exists(t *testing.T) {
	mock := &mockTx{
		queryRowFn: func(_ context.Context, sql string, args ...any) pgx.Row {
			assert.Contains(t, sql, "SELECT EXISTS")
			assert.Contains(t, sql, "FROM artifact_descriptors")
			assert.Contains(t, sql, "WHERE storage_key = $1")
			assert.Len(t, args, 1)
			assert.Equal(t, "org/doc/v1/ocr_text", args[0])
			return &mockRow{scanFn: func(dest ...any) error {
				*(dest[0].(*bool)) = true
				return nil
			}}
		},
	}
	ctx := ctxWithMockTx(mock)

	exists, err := NewOrphanCandidateRepository().ExistsByStorageKey(ctx, "org/doc/v1/ocr_text")
	require.NoError(t, err)
	assert.True(t, exists)
}

func TestOrphanCandidateRepository_ExistsByStorageKey_NotExists(t *testing.T) {
	mock := &mockTx{
		queryRowFn: func(_ context.Context, _ string, _ ...any) pgx.Row {
			return &mockRow{scanFn: func(dest ...any) error {
				*(dest[0].(*bool)) = false
				return nil
			}}
		},
	}
	ctx := ctxWithMockTx(mock)

	exists, err := NewOrphanCandidateRepository().ExistsByStorageKey(ctx, "orphan-key")
	require.NoError(t, err)
	assert.False(t, exists)
}

func TestOrphanCandidateRepository_ExistsByStorageKey_DBError(t *testing.T) {
	mock := &mockTx{
		queryRowFn: func(_ context.Context, _ string, _ ...any) pgx.Row {
			return &mockRow{scanFn: func(_ ...any) error {
				return errors.New("db error")
			}}
		},
	}
	ctx := ctxWithMockTx(mock)

	_, err := NewOrphanCandidateRepository().ExistsByStorageKey(ctx, "key")
	require.Error(t, err)
	assert.True(t, port.IsRetryable(err))
}

func TestOrphanCandidateRepository_DeleteByKeys_Success(t *testing.T) {
	mock := &mockTx{
		execFn: func(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			assert.Contains(t, sql, "DELETE FROM orphan_candidates")
			assert.Contains(t, sql, "WHERE storage_key = ANY($1)")
			assert.Len(t, args, 1)
			keys := args[0].([]string)
			assert.Len(t, keys, 2)
			return pgconn.NewCommandTag("DELETE 2"), nil
		},
	}
	ctx := ctxWithMockTx(mock)

	err := NewOrphanCandidateRepository().DeleteByKeys(ctx, []string{"key1", "key2"})
	assert.NoError(t, err)
}

func TestOrphanCandidateRepository_DeleteByKeys_EmptySlice(t *testing.T) {
	mock := &mockTx{
		execFn: func(_ context.Context, _ string, _ ...any) (pgconn.CommandTag, error) {
			t.Fatal("should not be called for empty slice")
			return pgconn.CommandTag{}, nil
		},
	}
	ctx := ctxWithMockTx(mock)

	err := NewOrphanCandidateRepository().DeleteByKeys(ctx, []string{})
	assert.NoError(t, err)
}

func TestOrphanCandidateRepository_DeleteByKeys_DBError(t *testing.T) {
	mock := &mockTx{
		execFn: func(_ context.Context, _ string, _ ...any) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, errors.New("db error")
		},
	}
	ctx := ctxWithMockTx(mock)

	err := NewOrphanCandidateRepository().DeleteByKeys(ctx, []string{"key1"})
	require.Error(t, err)
	assert.True(t, port.IsRetryable(err))
}

func TestOrphanCandidateRepository_Insert_Success(t *testing.T) {
	mock := &mockTx{
		execFn: func(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			assert.Contains(t, sql, "INSERT INTO orphan_candidates")
			assert.Contains(t, sql, "ON CONFLICT (storage_key) DO NOTHING")
			assert.Len(t, args, 3)
			assert.Equal(t, "org/doc/v1/ocr_text", args[0])
			return pgconn.NewCommandTag("INSERT 0 1"), nil
		},
	}
	ctx := ctxWithMockTx(mock)

	err := NewOrphanCandidateRepository().Insert(ctx, port.OrphanCandidate{
		StorageKey: "org/doc/v1/ocr_text",
		VersionID:  "v1",
		CreatedAt:  time.Now().UTC(),
	})
	assert.NoError(t, err)
}

func TestOrphanCandidateRepository_Insert_EmptyVersionID(t *testing.T) {
	mock := &mockTx{
		execFn: func(_ context.Context, _ string, args ...any) (pgconn.CommandTag, error) {
			// version_id should be nil (SQL NULL) for empty string.
			assert.Nil(t, args[1])
			return pgconn.NewCommandTag("INSERT 0 1"), nil
		},
	}
	ctx := ctxWithMockTx(mock)

	err := NewOrphanCandidateRepository().Insert(ctx, port.OrphanCandidate{
		StorageKey: "key",
		VersionID:  "",
		CreatedAt:  time.Now().UTC(),
	})
	assert.NoError(t, err)
}

func TestOrphanCandidateRepository_Insert_DBError(t *testing.T) {
	mock := &mockTx{
		execFn: func(_ context.Context, _ string, _ ...any) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, errors.New("db error")
		},
	}
	ctx := ctxWithMockTx(mock)

	err := NewOrphanCandidateRepository().Insert(ctx, port.OrphanCandidate{
		StorageKey: "key",
		CreatedAt:  time.Now().UTC(),
	})
	require.Error(t, err)
	assert.True(t, port.IsRetryable(err))
}

func TestOrphanCandidateRepository_ExistsByStorageKey_CrossTenant(t *testing.T) {
	// Verify the query does NOT contain organization_id — cross-tenant.
	mock := &mockTx{
		queryRowFn: func(_ context.Context, sql string, _ ...any) pgx.Row {
			assert.False(t, strings.Contains(sql, "organization_id"),
				"ExistsByStorageKey must be cross-tenant (no organization_id filter)")
			return &mockRow{scanFn: func(dest ...any) error {
				*(dest[0].(*bool)) = false
				return nil
			}}
		},
	}
	ctx := ctxWithMockTx(mock)

	_, err := NewOrphanCandidateRepository().ExistsByStorageKey(ctx, "key")
	assert.NoError(t, err)
}
