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

	"contractpro/document-management/internal/domain/model"
	"contractpro/document-management/internal/domain/port"
)

func TestNewDocumentRepository(t *testing.T) {
	repo := NewDocumentRepository()
	assert.NotNil(t, repo)
}

func TestDocumentRepository_Insert_Success(t *testing.T) {
	mock := &mockTx{
		execFn: func(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			assert.Contains(t, sql, "INSERT INTO documents")
			assert.Len(t, args, 9)
			return pgconn.NewCommandTag("INSERT 0 1"), nil
		},
	}
	ctx := ctxWithMockTx(mock)

	doc := model.NewDocument("doc-1", "org-1", "Test Contract", "user-1")
	err := NewDocumentRepository().Insert(ctx, doc)
	assert.NoError(t, err)

	calls := mock.getCalls()
	require.Len(t, calls, 1)
	assert.Equal(t, "Exec", calls[0].Method)
}

func TestDocumentRepository_Insert_UniqueViolation(t *testing.T) {
	mock := &mockTx{
		execFn: func(_ context.Context, _ string, _ ...any) (pgconn.CommandTag, error) {
			return pgconn.NewCommandTag(""), &pgconn.PgError{Code: "23505", ConstraintName: "documents_pkey"}
		},
	}
	ctx := ctxWithMockTx(mock)

	doc := model.NewDocument("doc-1", "org-1", "Test", "user-1")
	err := NewDocumentRepository().Insert(ctx, doc)

	require.Error(t, err)
	assert.Equal(t, port.ErrCodeDocumentAlreadyExists, port.ErrorCode(err))
	assert.False(t, port.IsRetryable(err))
}

func TestDocumentRepository_Insert_DatabaseError(t *testing.T) {
	mock := &mockTx{
		execFn: func(_ context.Context, _ string, _ ...any) (pgconn.CommandTag, error) {
			return pgconn.NewCommandTag(""), errors.New("connection reset")
		},
	}
	ctx := ctxWithMockTx(mock)

	doc := model.NewDocument("doc-1", "org-1", "Test", "user-1")
	err := NewDocumentRepository().Insert(ctx, doc)

	require.Error(t, err)
	assert.Equal(t, port.ErrCodeDatabaseFailed, port.ErrorCode(err))
	assert.True(t, port.IsRetryable(err))
}

func TestDocumentRepository_FindByID_NotFound(t *testing.T) {
	mock := &mockTx{
		queryRowFn: func(_ context.Context, sql string, args ...any) pgx.Row {
			assert.Contains(t, sql, "organization_id = $2")
			assert.Equal(t, "doc-1", args[0])
			assert.Equal(t, "org-1", args[1])
			return &mockRow{err: pgx.ErrNoRows}
		},
	}
	ctx := ctxWithMockTx(mock)

	_, err := NewDocumentRepository().FindByID(ctx, "org-1", "doc-1")
	require.Error(t, err)
	assert.Equal(t, port.ErrCodeDocumentNotFound, port.ErrorCode(err))
}

func TestDocumentRepository_FindByID_Success(t *testing.T) {
	now := time.Now().UTC()
	mock := &mockTx{
		queryRowFn: func(_ context.Context, sql string, _ ...any) pgx.Row {
			assert.Contains(t, sql, "organization_id = $2")
			return &mockRow{
				scanFn: func(dest ...any) error {
					*dest[0].(*string) = "doc-1"
					*dest[1].(*string) = "org-1"
					*dest[2].(*string) = "Title"
					*dest[3].(**string) = strPtr("ver-1")
					*dest[4].(*string) = "ACTIVE"
					*dest[5].(*string) = "user-1"
					*dest[6].(*time.Time) = now
					*dest[7].(*time.Time) = now
					*dest[8].(**time.Time) = nil
					return nil
				},
			}
		},
	}
	ctx := ctxWithMockTx(mock)

	doc, err := NewDocumentRepository().FindByID(ctx, "org-1", "doc-1")
	require.NoError(t, err)
	assert.Equal(t, "doc-1", doc.DocumentID)
	assert.Equal(t, "org-1", doc.OrganizationID)
	assert.Equal(t, "Title", doc.Title)
	assert.Equal(t, "ver-1", doc.CurrentVersionID)
	assert.Equal(t, model.DocumentStatusActive, doc.Status)
	assert.Nil(t, doc.DeletedAt)
}

func TestDocumentRepository_FindByIDForUpdate_Success(t *testing.T) {
	now := time.Now().UTC()
	mock := &mockTx{
		queryRowFn: func(_ context.Context, sql string, args ...any) pgx.Row {
			assert.Contains(t, sql, "organization_id = $2")
			assert.Contains(t, sql, "FOR UPDATE", "query must use FOR UPDATE clause (BRE-005)")
			assert.Equal(t, "doc-1", args[0])
			assert.Equal(t, "org-1", args[1])
			return &mockRow{
				scanFn: func(dest ...any) error {
					*dest[0].(*string) = "doc-1"
					*dest[1].(*string) = "org-1"
					*dest[2].(*string) = "Title"
					*dest[3].(**string) = strPtr("ver-1")
					*dest[4].(*string) = "ACTIVE"
					*dest[5].(*string) = "user-1"
					*dest[6].(*time.Time) = now
					*dest[7].(*time.Time) = now
					*dest[8].(**time.Time) = nil
					return nil
				},
			}
		},
	}
	ctx := ctxWithMockTx(mock)

	doc, err := NewDocumentRepository().FindByIDForUpdate(ctx, "org-1", "doc-1")
	require.NoError(t, err)
	assert.Equal(t, "doc-1", doc.DocumentID)
	assert.Equal(t, "org-1", doc.OrganizationID)
	assert.Equal(t, model.DocumentStatusActive, doc.Status)
}

func TestDocumentRepository_FindByIDForUpdate_NotFound(t *testing.T) {
	mock := &mockTx{
		queryRowFn: func(_ context.Context, sql string, _ ...any) pgx.Row {
			assert.Contains(t, sql, "FOR UPDATE")
			return &mockRow{err: pgx.ErrNoRows}
		},
	}
	ctx := ctxWithMockTx(mock)

	_, err := NewDocumentRepository().FindByIDForUpdate(ctx, "org-1", "doc-1")
	require.Error(t, err)
	assert.Equal(t, port.ErrCodeDocumentNotFound, port.ErrorCode(err))
}

func TestDocumentRepository_FindByIDForUpdate_DBError(t *testing.T) {
	mock := &mockTx{
		queryRowFn: func(_ context.Context, sql string, _ ...any) pgx.Row {
			assert.Contains(t, sql, "FOR UPDATE")
			return &mockRow{err: errors.New("connection reset")}
		},
	}
	ctx := ctxWithMockTx(mock)

	_, err := NewDocumentRepository().FindByIDForUpdate(ctx, "org-1", "doc-1")
	require.Error(t, err)
	assert.Equal(t, port.ErrCodeDatabaseFailed, port.ErrorCode(err))
	assert.True(t, port.IsRetryable(err))
}

func TestDocumentRepository_List_Empty(t *testing.T) {
	mock := &mockTx{
		queryFn: func(_ context.Context, sql string, args ...any) (pgx.Rows, error) {
			assert.Contains(t, sql, "organization_id = $1")
			assert.Contains(t, sql, "COUNT(*) OVER()")
			return &mockRows{}, nil
		},
	}
	ctx := ctxWithMockTx(mock)

	docs, total, err := NewDocumentRepository().List(ctx, "org-1", nil, 1, 10)
	require.NoError(t, err)
	assert.NotNil(t, docs, "empty list must be non-nil slice")
	assert.Empty(t, docs)
	assert.Equal(t, 0, total)
}

func TestDocumentRepository_List_WithStatusFilter(t *testing.T) {
	mock := &mockTx{
		queryFn: func(_ context.Context, sql string, args ...any) (pgx.Rows, error) {
			assert.Contains(t, sql, "status = $2")
			assert.Equal(t, "org-1", args[0])
			assert.Equal(t, "ACTIVE", args[1])
			return &mockRows{}, nil
		},
	}
	ctx := ctxWithMockTx(mock)

	status := model.DocumentStatusActive
	_, _, err := NewDocumentRepository().List(ctx, "org-1", &status, 1, 10)
	assert.NoError(t, err)
}

func TestDocumentRepository_List_QueryError(t *testing.T) {
	mock := &mockTx{
		queryFn: func(_ context.Context, _ string, _ ...any) (pgx.Rows, error) {
			return nil, errors.New("connection refused")
		},
	}
	ctx := ctxWithMockTx(mock)

	_, _, err := NewDocumentRepository().List(ctx, "org-1", nil, 1, 10)
	require.Error(t, err)
	assert.Equal(t, port.ErrCodeDatabaseFailed, port.ErrorCode(err))
}

func TestDocumentRepository_Update_Success(t *testing.T) {
	mock := &mockTx{
		execFn: func(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			assert.Contains(t, sql, "UPDATE documents")
			assert.Contains(t, sql, "organization_id = $7")
			assert.Len(t, args, 7)
			return pgconn.NewCommandTag("UPDATE 1"), nil
		},
	}
	ctx := ctxWithMockTx(mock)

	doc := model.NewDocument("doc-1", "org-1", "Updated Title", "user-1")
	err := NewDocumentRepository().Update(ctx, doc)
	assert.NoError(t, err)
}

func TestDocumentRepository_Update_NotFound(t *testing.T) {
	mock := &mockTx{
		execFn: func(_ context.Context, _ string, _ ...any) (pgconn.CommandTag, error) {
			return pgconn.NewCommandTag("UPDATE 0"), nil
		},
	}
	ctx := ctxWithMockTx(mock)

	doc := model.NewDocument("doc-1", "org-1", "Title", "user-1")
	err := NewDocumentRepository().Update(ctx, doc)

	require.Error(t, err)
	assert.Equal(t, port.ErrCodeDocumentNotFound, port.ErrorCode(err))
}

func TestDocumentRepository_ExistsByID_True(t *testing.T) {
	mock := &mockTx{
		queryRowFn: func(_ context.Context, sql string, args ...any) pgx.Row {
			assert.Contains(t, sql, "SELECT EXISTS")
			assert.Contains(t, sql, "organization_id = $2")
			return &mockRow{
				scanFn: func(dest ...any) error {
					*dest[0].(*bool) = true
					return nil
				},
			}
		},
	}
	ctx := ctxWithMockTx(mock)

	exists, err := NewDocumentRepository().ExistsByID(ctx, "org-1", "doc-1")
	require.NoError(t, err)
	assert.True(t, exists)
}

func TestDocumentRepository_ExistsByID_False(t *testing.T) {
	mock := &mockTx{
		queryRowFn: func(_ context.Context, _ string, _ ...any) pgx.Row {
			return &mockRow{
				scanFn: func(dest ...any) error {
					*dest[0].(*bool) = false
					return nil
				},
			}
		},
	}
	ctx := ctxWithMockTx(mock)

	exists, err := NewDocumentRepository().ExistsByID(ctx, "org-1", "doc-1")
	require.NoError(t, err)
	assert.False(t, exists)
}

func TestDocumentRepository_AllQueriesHaveOrgFilter(t *testing.T) {
	// Verify that every SQL query contains organization_id for tenant isolation.
	sqlStatements := []string{}
	mock := &mockTx{
		execFn: func(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
			sqlStatements = append(sqlStatements, sql)
			return pgconn.NewCommandTag("UPDATE 1"), nil
		},
		queryFn: func(_ context.Context, sql string, _ ...any) (pgx.Rows, error) {
			sqlStatements = append(sqlStatements, sql)
			return &mockRows{}, nil
		},
		queryRowFn: func(_ context.Context, sql string, _ ...any) pgx.Row {
			sqlStatements = append(sqlStatements, sql)
			// Return ErrNoRows — the test only cares about SQL content, not results.
			return &mockRow{err: pgx.ErrNoRows}
		},
	}
	ctx := ctxWithMockTx(mock)
	repo := NewDocumentRepository()

	doc := model.NewDocument("d", "o", "t", "u")
	_ = repo.Insert(ctx, doc)
	_, _ = repo.FindByID(ctx, "o", "d")   // returns not-found, that's OK
	_, _, _ = repo.List(ctx, "o", nil, 1, 10)
	_ = repo.Update(ctx, doc)
	_, _ = repo.ExistsByID(ctx, "o", "d")  // will fail scan but SQL is captured

	for i, sql := range sqlStatements {
		assert.True(t, strings.Contains(sql, "organization_id"),
			"SQL statement %d does not contain organization_id: %s", i, sql)
	}
}
