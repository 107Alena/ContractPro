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

func TestNewArtifactRepository(t *testing.T) {
	repo := NewArtifactRepository()
	assert.NotNil(t, repo)
}

func TestArtifactRepository_Insert_Success(t *testing.T) {
	mock := &mockTx{
		execFn: func(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			assert.Contains(t, sql, "INSERT INTO artifact_descriptors")
			assert.Len(t, args, 13)
			return pgconn.NewCommandTag("INSERT 0 1"), nil
		},
	}
	ctx := ctxWithMockTx(mock)

	a := model.NewArtifactDescriptor(
		"art-1", "v-1", "doc-1", "org-1",
		model.ArtifactTypeSemanticTree, model.ProducerDomainDP,
		"key", 1024, "sha256", "1.0", "job-1", "corr-1",
	)
	err := NewArtifactRepository().Insert(ctx, a)
	assert.NoError(t, err)
}

func TestArtifactRepository_Insert_UniqueViolation(t *testing.T) {
	mock := &mockTx{
		execFn: func(_ context.Context, _ string, _ ...any) (pgconn.CommandTag, error) {
			return pgconn.NewCommandTag(""), &pgconn.PgError{Code: "23505"}
		},
	}
	ctx := ctxWithMockTx(mock)

	a := model.NewArtifactDescriptor(
		"art-1", "v-1", "doc-1", "org-1",
		model.ArtifactTypeSemanticTree, model.ProducerDomainDP,
		"key", 1024, "sha256", "1.0", "job-1", "corr-1",
	)
	err := NewArtifactRepository().Insert(ctx, a)

	require.Error(t, err)
	assert.Equal(t, port.ErrCodeArtifactAlreadyExists, port.ErrorCode(err))
}

func TestArtifactRepository_Insert_FKViolation(t *testing.T) {
	mock := &mockTx{
		execFn: func(_ context.Context, _ string, _ ...any) (pgconn.CommandTag, error) {
			return pgconn.NewCommandTag(""), &pgconn.PgError{Code: "23503"}
		},
	}
	ctx := ctxWithMockTx(mock)

	a := model.NewArtifactDescriptor(
		"art-1", "v-1", "doc-1", "org-1",
		model.ArtifactTypeSemanticTree, model.ProducerDomainDP,
		"key", 1024, "sha256", "1.0", "job-1", "corr-1",
	)
	err := NewArtifactRepository().Insert(ctx, a)

	require.Error(t, err)
	assert.Equal(t, port.ErrCodeVersionNotFound, port.ErrorCode(err))
}

func TestArtifactRepository_FindByVersionAndType_Success(t *testing.T) {
	now := time.Now().UTC()
	mock := &mockTx{
		queryRowFn: func(_ context.Context, sql string, args ...any) pgx.Row {
			assert.Contains(t, sql, "organization_id = $3")
			assert.Equal(t, "v-1", args[0])
			assert.Equal(t, "SEMANTIC_TREE", args[1])
			assert.Equal(t, "org-1", args[2])
			assert.Equal(t, "doc-1", args[3])
			return &mockRow{
				scanFn: func(dest ...any) error {
					*dest[0].(*string) = "art-1"
					*dest[1].(*string) = "v-1"
					*dest[2].(*string) = "doc-1"
					*dest[3].(*string) = "org-1"
					*dest[4].(*string) = "SEMANTIC_TREE"
					*dest[5].(*string) = "DP"
					*dest[6].(*string) = "key"
					*dest[7].(*int64) = 1024
					*dest[8].(*string) = "sha256"
					*dest[9].(*string) = "1.0"
					*dest[10].(*string) = "job-1"
					*dest[11].(*string) = "corr-1"
					*dest[12].(*time.Time) = now
					return nil
				},
			}
		},
	}
	ctx := ctxWithMockTx(mock)

	a, err := NewArtifactRepository().FindByVersionAndType(ctx, "org-1", "doc-1", "v-1", model.ArtifactTypeSemanticTree)
	require.NoError(t, err)
	assert.Equal(t, "art-1", a.ArtifactID)
	assert.Equal(t, model.ArtifactTypeSemanticTree, a.ArtifactType)
	assert.Equal(t, model.ProducerDomainDP, a.ProducerDomain)
}

func TestArtifactRepository_FindByVersionAndType_NotFound(t *testing.T) {
	mock := &mockTx{
		queryRowFn: func(_ context.Context, _ string, _ ...any) pgx.Row {
			return &mockRow{err: pgx.ErrNoRows}
		},
	}
	ctx := ctxWithMockTx(mock)

	_, err := NewArtifactRepository().FindByVersionAndType(ctx, "org-1", "doc-1", "v-1", model.ArtifactTypeSemanticTree)
	require.Error(t, err)
	assert.Equal(t, port.ErrCodeArtifactNotFound, port.ErrorCode(err))
}

func TestArtifactRepository_ListByVersion_Empty(t *testing.T) {
	mock := &mockTx{
		queryFn: func(_ context.Context, sql string, _ ...any) (pgx.Rows, error) {
			assert.Contains(t, sql, "organization_id = $2")
			return &mockRows{}, nil
		},
	}
	ctx := ctxWithMockTx(mock)

	artifacts, err := NewArtifactRepository().ListByVersion(ctx, "org-1", "doc-1", "v-1")
	require.NoError(t, err)
	assert.NotNil(t, artifacts)
	assert.Empty(t, artifacts)
}

func TestArtifactRepository_ListByVersionAndTypes_UsesANY(t *testing.T) {
	mock := &mockTx{
		queryFn: func(_ context.Context, sql string, args ...any) (pgx.Rows, error) {
			assert.Contains(t, sql, "ANY($4)")
			assert.Len(t, args, 4)
			types, ok := args[3].([]string)
			assert.True(t, ok)
			assert.Contains(t, types, "SEMANTIC_TREE")
			assert.Contains(t, types, "EXTRACTED_TEXT")
			return &mockRows{}, nil
		},
	}
	ctx := ctxWithMockTx(mock)

	_, err := NewArtifactRepository().ListByVersionAndTypes(ctx, "org-1", "doc-1", "v-1",
		[]model.ArtifactType{model.ArtifactTypeSemanticTree, model.ArtifactTypeExtractedText})
	assert.NoError(t, err)
}

func TestArtifactRepository_DeleteByVersion_Success(t *testing.T) {
	mock := &mockTx{
		execFn: func(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			assert.Contains(t, sql, "DELETE FROM artifact_descriptors")
			assert.Contains(t, sql, "organization_id = $2")
			return pgconn.NewCommandTag("DELETE 5"), nil
		},
	}
	ctx := ctxWithMockTx(mock)

	err := NewArtifactRepository().DeleteByVersion(ctx, "org-1", "doc-1", "v-1")
	assert.NoError(t, err)
}

func TestArtifactRepository_DeleteByVersion_NoRows(t *testing.T) {
	mock := &mockTx{
		execFn: func(_ context.Context, _ string, _ ...any) (pgconn.CommandTag, error) {
			return pgconn.NewCommandTag("DELETE 0"), nil
		},
	}
	ctx := ctxWithMockTx(mock)

	err := NewArtifactRepository().DeleteByVersion(ctx, "org-1", "doc-1", "v-1")
	assert.NoError(t, err, "delete with zero affected rows should not error")
}

func TestArtifactRepository_AllQueriesHaveOrgFilter(t *testing.T) {
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
	repo := NewArtifactRepository()

	a := model.NewArtifactDescriptor("a", "v", "d", "o",
		model.ArtifactTypeSemanticTree, model.ProducerDomainDP,
		"k", 100, "h", "1.0", "j", "c")
	_ = repo.Insert(ctx, a)
	_, _ = repo.FindByVersionAndType(ctx, "o", "d", "v", model.ArtifactTypeSemanticTree)
	_, _ = repo.ListByVersion(ctx, "o", "d", "v")
	_, _ = repo.ListByVersionAndTypes(ctx, "o", "d", "v", []model.ArtifactType{model.ArtifactTypeSemanticTree})
	_ = repo.DeleteByVersion(ctx, "o", "d", "v")

	for i, sql := range sqlStatements {
		assert.True(t, strings.Contains(sql, "organization_id"),
			"SQL statement %d does not contain organization_id: %s", i, sql)
	}
}

func TestArtifactRepository_DatabaseError(t *testing.T) {
	mock := &mockTx{
		execFn: func(_ context.Context, _ string, _ ...any) (pgconn.CommandTag, error) {
			return pgconn.NewCommandTag(""), errors.New("timeout")
		},
	}
	ctx := ctxWithMockTx(mock)

	a := model.NewArtifactDescriptor("a", "v", "d", "o",
		model.ArtifactTypeSemanticTree, model.ProducerDomainDP,
		"k", 100, "h", "1.0", "j", "c")
	err := NewArtifactRepository().Insert(ctx, a)

	require.Error(t, err)
	assert.Equal(t, port.ErrCodeDatabaseFailed, port.ErrorCode(err))
	assert.True(t, port.IsRetryable(err))
}
