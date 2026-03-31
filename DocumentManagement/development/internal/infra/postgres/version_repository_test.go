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

func TestNewVersionRepository(t *testing.T) {
	repo := NewVersionRepository()
	assert.NotNil(t, repo)
}

func TestVersionRepository_Insert_Success(t *testing.T) {
	mock := &mockTx{
		execFn: func(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			assert.Contains(t, sql, "INSERT INTO document_versions")
			assert.Len(t, args, 14)
			return pgconn.NewCommandTag("INSERT 0 1"), nil
		},
	}
	ctx := ctxWithMockTx(mock)

	v := model.NewDocumentVersion("v-1", "doc-1", "org-1", 1,
		model.OriginTypeUpload, "key", "file.pdf", 1024, "sha256", "user-1")
	err := NewVersionRepository().Insert(ctx, v)
	assert.NoError(t, err)
}

func TestVersionRepository_Insert_UniqueViolation(t *testing.T) {
	mock := &mockTx{
		execFn: func(_ context.Context, _ string, _ ...any) (pgconn.CommandTag, error) {
			return pgconn.NewCommandTag(""), &pgconn.PgError{Code: "23505"}
		},
	}
	ctx := ctxWithMockTx(mock)

	v := model.NewDocumentVersion("v-1", "doc-1", "org-1", 1,
		model.OriginTypeUpload, "key", "file.pdf", 1024, "sha256", "user-1")
	err := NewVersionRepository().Insert(ctx, v)

	require.Error(t, err)
	assert.Equal(t, port.ErrCodeVersionAlreadyExists, port.ErrorCode(err))
}

func TestVersionRepository_Insert_FKViolation(t *testing.T) {
	mock := &mockTx{
		execFn: func(_ context.Context, _ string, _ ...any) (pgconn.CommandTag, error) {
			return pgconn.NewCommandTag(""), &pgconn.PgError{Code: "23503"}
		},
	}
	ctx := ctxWithMockTx(mock)

	v := model.NewDocumentVersion("v-1", "doc-1", "org-1", 1,
		model.OriginTypeUpload, "key", "file.pdf", 1024, "sha256", "user-1")
	err := NewVersionRepository().Insert(ctx, v)

	require.Error(t, err)
	assert.Equal(t, port.ErrCodeDocumentNotFound, port.ErrorCode(err))
}

func TestVersionRepository_FindByID_Success(t *testing.T) {
	now := time.Now().UTC()
	mock := &mockTx{
		queryRowFn: func(_ context.Context, sql string, args ...any) pgx.Row {
			assert.Contains(t, sql, "organization_id = $3")
			assert.Equal(t, "v-1", args[0])
			assert.Equal(t, "doc-1", args[1])
			assert.Equal(t, "org-1", args[2])
			return &mockRow{
				scanFn: func(dest ...any) error {
					*dest[0].(*string) = "v-1"
					*dest[1].(*string) = "doc-1"
					*dest[2].(*string) = "org-1"
					*dest[3].(*int) = 1
					*dest[4].(**string) = nil // no parent
					*dest[5].(*string) = "UPLOAD"
					*dest[6].(**string) = nil // no description
					*dest[7].(*string) = "key"
					*dest[8].(*string) = "file.pdf"
					*dest[9].(*int64) = 1024
					*dest[10].(*string) = "sha256"
					*dest[11].(*string) = "PENDING"
					*dest[12].(*string) = "user-1"
					*dest[13].(*time.Time) = now
					return nil
				},
			}
		},
	}
	ctx := ctxWithMockTx(mock)

	v, err := NewVersionRepository().FindByID(ctx, "org-1", "doc-1", "v-1")
	require.NoError(t, err)
	assert.Equal(t, "v-1", v.VersionID)
	assert.Equal(t, model.OriginTypeUpload, v.OriginType)
	assert.Equal(t, model.ArtifactStatusPending, v.ArtifactStatus)
	assert.Empty(t, v.ParentVersionID)
}

func TestVersionRepository_FindByID_NotFound(t *testing.T) {
	mock := &mockTx{
		queryRowFn: func(_ context.Context, _ string, _ ...any) pgx.Row {
			return &mockRow{err: pgx.ErrNoRows}
		},
	}
	ctx := ctxWithMockTx(mock)

	_, err := NewVersionRepository().FindByID(ctx, "org-1", "doc-1", "v-1")
	require.Error(t, err)
	assert.Equal(t, port.ErrCodeVersionNotFound, port.ErrorCode(err))
}

func TestVersionRepository_List_Empty(t *testing.T) {
	mock := &mockTx{
		queryFn: func(_ context.Context, sql string, _ ...any) (pgx.Rows, error) {
			assert.Contains(t, sql, "ORDER BY version_number DESC")
			assert.Contains(t, sql, "organization_id = $2")
			return &mockRows{}, nil
		},
	}
	ctx := ctxWithMockTx(mock)

	versions, total, err := NewVersionRepository().List(ctx, "org-1", "doc-1", 1, 10)
	require.NoError(t, err)
	assert.NotNil(t, versions)
	assert.Empty(t, versions)
	assert.Equal(t, 0, total)
}

func TestVersionRepository_Update_Success(t *testing.T) {
	mock := &mockTx{
		execFn: func(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			assert.Contains(t, sql, "UPDATE document_versions")
			assert.Contains(t, sql, "artifact_status = $1")
			assert.Contains(t, sql, "organization_id = $3")
			assert.Contains(t, sql, "document_id = $4")
			assert.Len(t, args, 4)
			assert.Equal(t, "PROCESSING_ARTIFACTS_RECEIVED", args[0])
			return pgconn.NewCommandTag("UPDATE 1"), nil
		},
	}
	ctx := ctxWithMockTx(mock)

	v := &model.DocumentVersion{
		VersionID:      "v-1",
		DocumentID:     "doc-1",
		OrganizationID: "org-1",
		ArtifactStatus: model.ArtifactStatusProcessingArtifactsReceived,
	}
	err := NewVersionRepository().Update(ctx, v)
	assert.NoError(t, err)
}

func TestVersionRepository_Update_NotFound(t *testing.T) {
	mock := &mockTx{
		execFn: func(_ context.Context, _ string, _ ...any) (pgconn.CommandTag, error) {
			return pgconn.NewCommandTag("UPDATE 0"), nil
		},
	}
	ctx := ctxWithMockTx(mock)

	v := &model.DocumentVersion{VersionID: "v-1", DocumentID: "doc-1", OrganizationID: "org-1"}
	err := NewVersionRepository().Update(ctx, v)

	require.Error(t, err)
	assert.Equal(t, port.ErrCodeVersionNotFound, port.ErrorCode(err))
}

func TestVersionRepository_NextVersionNumber_FirstVersion(t *testing.T) {
	mock := &mockTx{
		queryRowFn: func(_ context.Context, sql string, args ...any) pgx.Row {
			assert.Contains(t, sql, "COALESCE(MAX(version_number), 0) + 1")
			assert.Contains(t, sql, "organization_id = $2")
			return &mockRow{
				scanFn: func(dest ...any) error {
					*dest[0].(*int) = 1
					return nil
				},
			}
		},
	}
	ctx := ctxWithMockTx(mock)

	next, err := NewVersionRepository().NextVersionNumber(ctx, "org-1", "doc-1")
	require.NoError(t, err)
	assert.Equal(t, 1, next)
}

func TestVersionRepository_NextVersionNumber_Subsequent(t *testing.T) {
	mock := &mockTx{
		queryRowFn: func(_ context.Context, _ string, _ ...any) pgx.Row {
			return &mockRow{
				scanFn: func(dest ...any) error {
					*dest[0].(*int) = 5
					return nil
				},
			}
		},
	}
	ctx := ctxWithMockTx(mock)

	next, err := NewVersionRepository().NextVersionNumber(ctx, "org-1", "doc-1")
	require.NoError(t, err)
	assert.Equal(t, 5, next)
}

func TestVersionRepository_AllQueriesHaveOrgFilter(t *testing.T) {
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
			return &mockRow{err: pgx.ErrNoRows}
		},
	}
	ctx := ctxWithMockTx(mock)
	repo := NewVersionRepository()

	v := model.NewDocumentVersion("v", "d", "o", 1, model.OriginTypeUpload, "k", "f", 100, "h", "u")
	_ = repo.Insert(ctx, v)
	_, _ = repo.FindByID(ctx, "o", "d", "v")
	_, _, _ = repo.List(ctx, "o", "d", 1, 10)
	_ = repo.Update(ctx, v)
	_, _ = repo.NextVersionNumber(ctx, "o", "d")

	for i, sql := range sqlStatements {
		assert.True(t, strings.Contains(sql, "organization_id"),
			"SQL statement %d does not contain organization_id: %s", i, sql)
	}
}
