package postgres

import (
	"context"
	"encoding/json"
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

func TestNewAuditRepository(t *testing.T) {
	repo := NewAuditRepository()
	assert.NotNil(t, repo)
}

func TestAuditRepository_Insert_Success(t *testing.T) {
	mock := &mockTx{
		execFn: func(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			assert.Contains(t, sql, "INSERT INTO audit_records")
			assert.Len(t, args, 11)
			return pgconn.NewCommandTag("INSERT 0 1"), nil
		},
	}
	ctx := ctxWithMockTx(mock)

	record := model.NewAuditRecord("aud-1", "org-1", model.AuditActionDocumentCreated, model.ActorTypeUser, "user-1").
		WithDocument("doc-1").
		WithDetails(json.RawMessage(`{"key":"value"}`))
	err := NewAuditRepository().Insert(ctx, record)
	assert.NoError(t, err)
}

func TestAuditRepository_Insert_NullableFields(t *testing.T) {
	var capturedArgs []any
	mock := &mockTx{
		execFn: func(_ context.Context, _ string, args ...any) (pgconn.CommandTag, error) {
			capturedArgs = args
			return pgconn.NewCommandTag("INSERT 0 1"), nil
		},
	}
	ctx := ctxWithMockTx(mock)

	// Record without document_id, version_id, job_id, details.
	record := model.NewAuditRecord("aud-1", "org-1", model.AuditActionDocumentCreated, model.ActorTypeSystem, "system")
	err := NewAuditRepository().Insert(ctx, record)
	require.NoError(t, err)

	// Verify nullable fields are nil (SQL NULL).
	assert.Nil(t, capturedArgs[2], "document_id should be nil for empty string")
	assert.Nil(t, capturedArgs[3], "version_id should be nil for empty string")
	assert.Nil(t, capturedArgs[7], "job_id should be nil for empty string")
	assert.Nil(t, capturedArgs[8], "correlation_id should be nil for empty string")
	assert.Nil(t, capturedArgs[9], "details should be nil for empty RawMessage")
}

func TestAuditRepository_Insert_DatabaseError(t *testing.T) {
	mock := &mockTx{
		execFn: func(_ context.Context, _ string, _ ...any) (pgconn.CommandTag, error) {
			return pgconn.NewCommandTag(""), errors.New("connection lost")
		},
	}
	ctx := ctxWithMockTx(mock)

	record := model.NewAuditRecord("aud-1", "org-1", model.AuditActionDocumentCreated, model.ActorTypeUser, "user-1")
	err := NewAuditRepository().Insert(ctx, record)

	require.Error(t, err)
	assert.Equal(t, port.ErrCodeDatabaseFailed, port.ErrorCode(err))
}

func TestAuditRepository_List_AllFilters(t *testing.T) {
	mock := &mockTx{
		queryFn: func(_ context.Context, sql string, args ...any) (pgx.Rows, error) {
			// Verify all filter conditions are present.
			assert.Contains(t, sql, "organization_id = $1")
			assert.Contains(t, sql, "document_id = $2")
			assert.Contains(t, sql, "version_id = $3")
			assert.Contains(t, sql, "action = $4")
			assert.Contains(t, sql, "actor_type = $5")
			assert.Contains(t, sql, "created_at >= $6")
			assert.Contains(t, sql, "created_at <= $7")
			assert.Contains(t, sql, "LIMIT $8 OFFSET $9")
			assert.Len(t, args, 9)
			return &mockRows{}, nil
		},
	}
	ctx := ctxWithMockTx(mock)

	action := model.AuditActionDocumentCreated
	actorType := model.ActorTypeUser
	since := time.Now().Add(-24 * time.Hour)
	until := time.Now()
	params := port.AuditListParams{
		OrganizationID: "org-1",
		DocumentID:     "doc-1",
		VersionID:      "v-1",
		Action:         &action,
		ActorType:      &actorType,
		Since:          &since,
		Until:          &until,
		Page:           1,
		PageSize:       20,
	}

	records, total, err := NewAuditRepository().List(ctx, params)
	require.NoError(t, err)
	assert.NotNil(t, records)
	assert.Empty(t, records)
	assert.Equal(t, 0, total)
}

func TestAuditRepository_List_MinimalFilters(t *testing.T) {
	mock := &mockTx{
		queryFn: func(_ context.Context, sql string, args ...any) (pgx.Rows, error) {
			assert.Contains(t, sql, "organization_id = $1")
			// Verify WHERE clause does NOT contain optional filters.
			// Note: column names appear in SELECT, so we check for "AND document_id" pattern.
			assert.NotContains(t, sql, "AND document_id")
			assert.NotContains(t, sql, "AND version_id")
			assert.NotContains(t, sql, "AND action =")
			assert.Contains(t, sql, "LIMIT $2 OFFSET $3")
			assert.Len(t, args, 3)
			return &mockRows{}, nil
		},
	}
	ctx := ctxWithMockTx(mock)

	params := port.AuditListParams{
		OrganizationID: "org-1",
		Page:           1,
		PageSize:       10,
	}

	records, total, err := NewAuditRepository().List(ctx, params)
	require.NoError(t, err)
	assert.NotNil(t, records)
	assert.Equal(t, 0, total)
}

func TestAuditRepository_List_Pagination(t *testing.T) {
	mock := &mockTx{
		queryFn: func(_ context.Context, sql string, args ...any) (pgx.Rows, error) {
			assert.Contains(t, sql, "LIMIT $2 OFFSET $3")
			// Page 3, page size 20 → offset = 40.
			assert.Equal(t, 20, args[1])
			assert.Equal(t, 40, args[2])
			return &mockRows{}, nil
		},
	}
	ctx := ctxWithMockTx(mock)

	params := port.AuditListParams{
		OrganizationID: "org-1",
		Page:           3,
		PageSize:       20,
	}

	_, _, err := NewAuditRepository().List(ctx, params)
	assert.NoError(t, err)
}

func TestAuditRepository_List_QueryError(t *testing.T) {
	mock := &mockTx{
		queryFn: func(_ context.Context, _ string, _ ...any) (pgx.Rows, error) {
			return nil, errors.New("database down")
		},
	}
	ctx := ctxWithMockTx(mock)

	params := port.AuditListParams{OrganizationID: "org-1", Page: 1, PageSize: 10}
	_, _, err := NewAuditRepository().List(ctx, params)

	require.Error(t, err)
	assert.Equal(t, port.ErrCodeDatabaseFailed, port.ErrorCode(err))
}

func TestAuditRepository_AllQueriesHaveOrgFilter(t *testing.T) {
	sqlStatements := []string{}
	mock := &mockTx{
		execFn: func(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
			sqlStatements = append(sqlStatements, sql)
			return pgconn.NewCommandTag("INSERT 0 1"), nil
		},
		queryFn: func(_ context.Context, sql string, _ ...any) (pgx.Rows, error) {
			sqlStatements = append(sqlStatements, sql)
			return &mockRows{}, nil
		},
	}
	ctx := ctxWithMockTx(mock)
	repo := NewAuditRepository()

	record := model.NewAuditRecord("aud", "o", model.AuditActionDocumentCreated, model.ActorTypeUser, "u")
	_ = repo.Insert(ctx, record)
	_, _, _ = repo.List(ctx, port.AuditListParams{OrganizationID: "o", Page: 1, PageSize: 10})

	for i, sql := range sqlStatements {
		assert.True(t, strings.Contains(sql, "organization_id"),
			"SQL statement %d does not contain organization_id: %s", i, sql)
	}
}
