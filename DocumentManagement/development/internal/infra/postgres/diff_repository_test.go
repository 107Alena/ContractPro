package postgres

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"contractpro/document-management/internal/domain/model"
	"contractpro/document-management/internal/domain/port"
)

func TestNewDiffRepository(t *testing.T) {
	repo := NewDiffRepository()
	assert.NotNil(t, repo)
}

func TestDiffRepository_Insert_Success(t *testing.T) {
	mock := &mockTx{
		execFn: func(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			assert.Contains(t, sql, "INSERT INTO version_diff_references")
			assert.Len(t, args, 11)
			return pgconn.NewCommandTag("INSERT 0 1"), nil
		},
	}
	ctx := ctxWithMockTx(mock)

	ref := model.NewVersionDiffReference("d-1", "doc-1", "org-1", "v-1", "v-2",
		"key", 5, 2, "job-1", "corr-1")
	err := NewDiffRepository().Insert(ctx, ref)
	assert.NoError(t, err)
}

func TestDiffRepository_Insert_UniqueViolation(t *testing.T) {
	mock := &mockTx{
		execFn: func(_ context.Context, _ string, _ ...any) (pgconn.CommandTag, error) {
			return pgconn.NewCommandTag(""), &pgconn.PgError{Code: "23505"}
		},
	}
	ctx := ctxWithMockTx(mock)

	ref := model.NewVersionDiffReference("d-1", "doc-1", "org-1", "v-1", "v-2",
		"key", 5, 2, "job-1", "corr-1")
	err := NewDiffRepository().Insert(ctx, ref)

	require.Error(t, err)
	assert.Equal(t, port.ErrCodeDiffAlreadyExists, port.ErrorCode(err))
}

func TestDiffRepository_Insert_FKViolation(t *testing.T) {
	mock := &mockTx{
		execFn: func(_ context.Context, _ string, _ ...any) (pgconn.CommandTag, error) {
			return pgconn.NewCommandTag(""), &pgconn.PgError{Code: "23503"}
		},
	}
	ctx := ctxWithMockTx(mock)

	ref := model.NewVersionDiffReference("d-1", "doc-1", "org-1", "v-1", "v-2",
		"key", 5, 2, "job-1", "corr-1")
	err := NewDiffRepository().Insert(ctx, ref)

	require.Error(t, err)
	assert.Equal(t, port.ErrCodeDatabaseFailed, port.ErrorCode(err))
	assert.True(t, port.IsRetryable(err))
}

func TestDiffRepository_FindByVersionPair_Success(t *testing.T) {
	now := time.Now().UTC()
	mock := &mockTx{
		queryRowFn: func(_ context.Context, sql string, args ...any) pgx.Row {
			assert.Contains(t, sql, "organization_id = $3")
			assert.Equal(t, "v-1", args[0])
			assert.Equal(t, "v-2", args[1])
			return &mockRow{
				scanFn: func(dest ...any) error {
					*dest[0].(*string) = "d-1"
					*dest[1].(*string) = "doc-1"
					*dest[2].(*string) = "org-1"
					*dest[3].(*string) = "v-1"
					*dest[4].(*string) = "v-2"
					*dest[5].(*string) = "key"
					*dest[6].(*int) = 5
					*dest[7].(*int) = 2
					*dest[8].(*string) = "job-1"
					*dest[9].(*string) = "corr-1"
					*dest[10].(*time.Time) = now
					return nil
				},
			}
		},
	}
	ctx := ctxWithMockTx(mock)

	ref, err := NewDiffRepository().FindByVersionPair(ctx, "org-1", "doc-1", "v-1", "v-2")
	require.NoError(t, err)
	assert.Equal(t, "d-1", ref.DiffID)
	assert.Equal(t, 5, ref.TextDiffCount)
	assert.Equal(t, 2, ref.StructuralDiffCount)
}

func TestDiffRepository_FindByVersionPair_NotFound(t *testing.T) {
	mock := &mockTx{
		queryRowFn: func(_ context.Context, _ string, _ ...any) pgx.Row {
			return &mockRow{err: pgx.ErrNoRows}
		},
	}
	ctx := ctxWithMockTx(mock)

	_, err := NewDiffRepository().FindByVersionPair(ctx, "org-1", "doc-1", "v-1", "v-2")
	require.Error(t, err)
	assert.Equal(t, port.ErrCodeDiffNotFound, port.ErrorCode(err))
}

func TestDiffRepository_ListByDocument_Empty(t *testing.T) {
	mock := &mockTx{
		queryFn: func(_ context.Context, sql string, _ ...any) (pgx.Rows, error) {
			assert.Contains(t, sql, "organization_id = $2")
			return &mockRows{}, nil
		},
	}
	ctx := ctxWithMockTx(mock)

	diffs, err := NewDiffRepository().ListByDocument(ctx, "org-1", "doc-1")
	require.NoError(t, err)
	assert.NotNil(t, diffs)
	assert.Empty(t, diffs)
}

func TestDiffRepository_DeleteByDocument_Success(t *testing.T) {
	mock := &mockTx{
		execFn: func(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
			assert.Contains(t, sql, "DELETE FROM version_diff_references")
			assert.Contains(t, sql, "organization_id = $2")
			return pgconn.NewCommandTag("DELETE 3"), nil
		},
	}
	ctx := ctxWithMockTx(mock)

	err := NewDiffRepository().DeleteByDocument(ctx, "org-1", "doc-1")
	assert.NoError(t, err)
}

func TestDiffRepository_AllQueriesHaveOrgFilter(t *testing.T) {
	sqlStatements := []string{}
	mock := &mockTx{
		execFn: func(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
			sqlStatements = append(sqlStatements, sql)
			return pgconn.NewCommandTag("DELETE 0"), nil
		},
		queryFn: func(_ context.Context, sql string, _ ...any) (pgx.Rows, error) {
			sqlStatements = append(sqlStatements, sql)
			return &mockRows{}, nil
		},
		queryRowFn: func(_ context.Context, sql string, _ ...any) pgx.Row {
			sqlStatements = append(sqlStatements, sql)
			return &mockRow{err: pgx.ErrNoRows}
		},
	}
	ctx := ctxWithMockTx(mock)
	repo := NewDiffRepository()

	ref := model.NewVersionDiffReference("d", "doc", "o", "v1", "v2", "k", 1, 1, "j", "c")
	_ = repo.Insert(ctx, ref)
	_, _ = repo.FindByVersionPair(ctx, "o", "doc", "v1", "v2")
	_, _ = repo.ListByDocument(ctx, "o", "doc")
	_ = repo.DeleteByDocument(ctx, "o", "doc")

	for i, sql := range sqlStatements {
		assert.True(t, strings.Contains(sql, "organization_id"),
			"SQL statement %d does not contain organization_id: %s", i, sql)
	}
}
