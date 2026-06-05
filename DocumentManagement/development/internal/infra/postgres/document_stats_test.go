package postgres

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"contractpro/document-management/internal/domain/model"
	"contractpro/document-management/internal/domain/port"
)

// scanStatusCount returns a mockRows scan function for one (status, count) row.
// A nil status models a document with no current version (the NULL group).
func scanStatusCount(status *string, cnt int) func(dest ...any) error {
	return func(dest ...any) error {
		*(dest[0].(**string)) = status
		*(dest[1].(*int)) = cnt
		return nil
	}
}

func TestDocumentRepository_CountByArtifactStatus_Grouping(t *testing.T) {
	pending := "PENDING"
	fully := "FULLY_READY"

	var capturedSQL string
	var capturedArgs []any
	mock := &mockTx{
		queryFn: func(_ context.Context, sql string, args ...any) (pgx.Rows, error) {
			capturedSQL = sql
			capturedArgs = args
			return &mockRows{scanFns: []func(dest ...any) error{
				scanStatusCount(&pending, 3),
				scanStatusCount(&fully, 5),
				scanStatusCount(nil, 2), // no current version → not_started
			}}, nil
		},
	}
	ctx := ctxWithMockTx(mock)

	stats, err := NewDocumentRepository().CountCurrentVersionsByArtifactStatus(ctx, "org-1", false)
	require.NoError(t, err)

	// Single query, no N+1.
	calls := mock.getCalls()
	require.Len(t, calls, 1)
	assert.Equal(t, "Query", calls[0].Method)

	// JOIN + GROUP BY shape and tenant scoping.
	assert.Contains(t, capturedSQL, "LEFT JOIN document_versions")
	assert.Contains(t, capturedSQL, "GROUP BY v.artifact_status")
	assert.Contains(t, capturedSQL, "d.organization_id = $1")
	assert.Contains(t, capturedSQL, "d.status = ANY($2)")
	require.Len(t, capturedArgs, 2)
	assert.Equal(t, "org-1", capturedArgs[0])
	// ACTIVE-only by default.
	assert.Equal(t, []string{"ACTIVE"}, capturedArgs[1])

	assert.Equal(t, 3, stats.ByArtifactStatus[model.ArtifactStatusPending])
	assert.Equal(t, 5, stats.ByArtifactStatus[model.ArtifactStatusFullyReady])
	assert.Equal(t, 2, stats.NotStarted)
	assert.Equal(t, 10, stats.Total)
}

func TestDocumentRepository_CountByArtifactStatus_IncludeArchived(t *testing.T) {
	var capturedArgs []any
	mock := &mockTx{
		queryFn: func(_ context.Context, _ string, args ...any) (pgx.Rows, error) {
			capturedArgs = args
			return &mockRows{}, nil
		},
	}
	ctx := ctxWithMockTx(mock)

	stats, err := NewDocumentRepository().CountCurrentVersionsByArtifactStatus(ctx, "org-1", true)
	require.NoError(t, err)
	require.Len(t, capturedArgs, 2)
	// ARCHIVED added; DELETED never present.
	assert.Equal(t, []string{"ACTIVE", "ARCHIVED"}, capturedArgs[1])
	assert.NotContains(t, capturedArgs[1], "DELETED")
	// Empty result → non-nil map, zero totals.
	assert.NotNil(t, stats.ByArtifactStatus)
	assert.Equal(t, 0, stats.Total)
	assert.Equal(t, 0, stats.NotStarted)
}

func TestDocumentRepository_CountByArtifactStatus_QueryError(t *testing.T) {
	mock := &mockTx{
		queryFn: func(_ context.Context, _ string, _ ...any) (pgx.Rows, error) {
			return nil, errors.New("connection refused")
		},
	}
	ctx := ctxWithMockTx(mock)

	_, err := NewDocumentRepository().CountCurrentVersionsByArtifactStatus(ctx, "org-1", false)
	require.Error(t, err)
	assert.Equal(t, port.ErrCodeDatabaseFailed, port.ErrorCode(err))
	assert.True(t, port.IsRetryable(err))
}

func TestDocumentRepository_CountByArtifactStatus_ScanError(t *testing.T) {
	mock := &mockTx{
		queryFn: func(_ context.Context, _ string, _ ...any) (pgx.Rows, error) {
			return &mockRows{scanFns: []func(dest ...any) error{
				func(_ ...any) error { return errors.New("scan boom") },
			}}, nil
		},
	}
	ctx := ctxWithMockTx(mock)

	_, err := NewDocumentRepository().CountCurrentVersionsByArtifactStatus(ctx, "org-1", false)
	require.Error(t, err)
	assert.Equal(t, port.ErrCodeDatabaseFailed, port.ErrorCode(err))
}

func TestDocumentRepository_CountByArtifactStatus_RowsErr(t *testing.T) {
	mock := &mockTx{
		queryFn: func(_ context.Context, _ string, _ ...any) (pgx.Rows, error) {
			return &mockRows{errFn: func() error { return errors.New("iteration failed") }}, nil
		},
	}
	ctx := ctxWithMockTx(mock)

	_, err := NewDocumentRepository().CountCurrentVersionsByArtifactStatus(ctx, "org-1", false)
	require.Error(t, err)
	assert.Equal(t, port.ErrCodeDatabaseFailed, port.ErrorCode(err))
}
