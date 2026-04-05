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
			assert.Contains(t, sql, "actor_id = $5")
			assert.Contains(t, sql, "actor_type = $6")
			assert.Contains(t, sql, "created_at >= $7")
			assert.Contains(t, sql, "created_at <= $8")
			assert.Contains(t, sql, "LIMIT $9 OFFSET $10")
			assert.Len(t, args, 10)
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
		ActorID:        "user-1",
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

// ---------------------------------------------------------------------------
// DeleteByDocument tests (BRE-016: retention override for append-only trigger)
// ---------------------------------------------------------------------------

func TestAuditRepository_DeleteByDocument_SetsRetentionOverride(t *testing.T) {
	var execCalls []string
	mock := &mockTx{
		execFn: func(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
			execCalls = append(execCalls, sql)
			return pgconn.NewCommandTag("DELETE 3"), nil
		},
	}
	ctx := ctxWithMockTx(mock)

	err := NewAuditRepository().DeleteByDocument(ctx, "doc-1")
	require.NoError(t, err)

	// Verify SET LOCAL is called BEFORE DELETE.
	require.Len(t, execCalls, 2, "expected 2 exec calls: SET LOCAL + DELETE")
	assert.Contains(t, execCalls[0], "SET LOCAL app.retention_override")
	assert.Contains(t, execCalls[1], "DELETE FROM audit_records")
}

func TestAuditRepository_DeleteByDocument_SetLocalError(t *testing.T) {
	callCount := 0
	mock := &mockTx{
		execFn: func(_ context.Context, _ string, _ ...any) (pgconn.CommandTag, error) {
			callCount++
			if callCount == 1 {
				return pgconn.NewCommandTag(""), errors.New("permission denied")
			}
			return pgconn.NewCommandTag("DELETE 0"), nil
		},
	}
	ctx := ctxWithMockTx(mock)

	err := NewAuditRepository().DeleteByDocument(ctx, "doc-1")

	require.Error(t, err)
	assert.Equal(t, port.ErrCodeDatabaseFailed, port.ErrorCode(err))
	// Verify DELETE was never called.
	assert.Equal(t, 1, callCount, "DELETE should not be called when SET LOCAL fails")
}

func TestAuditRepository_DeleteByDocument_DeleteError(t *testing.T) {
	callCount := 0
	mock := &mockTx{
		execFn: func(_ context.Context, _ string, _ ...any) (pgconn.CommandTag, error) {
			callCount++
			if callCount == 2 {
				return pgconn.NewCommandTag(""), errors.New("trigger violation")
			}
			return pgconn.NewCommandTag("SET"), nil
		},
	}
	ctx := ctxWithMockTx(mock)

	err := NewAuditRepository().DeleteByDocument(ctx, "doc-1")

	require.Error(t, err)
	assert.Equal(t, port.ErrCodeDatabaseFailed, port.ErrorCode(err))
	assert.Equal(t, 2, callCount, "both SET LOCAL and DELETE should be called")
}

func TestAuditRepository_DeleteByDocument_Success(t *testing.T) {
	var capturedDocID string
	mock := &mockTx{
		execFn: func(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			if strings.Contains(sql, "DELETE") {
				capturedDocID = args[0].(string)
			}
			return pgconn.NewCommandTag("DELETE 5"), nil
		},
	}
	ctx := ctxWithMockTx(mock)

	err := NewAuditRepository().DeleteByDocument(ctx, "doc-42")
	require.NoError(t, err)
	assert.Equal(t, "doc-42", capturedDocID)
}

// ---------------------------------------------------------------------------
// Migration file embedding tests
// ---------------------------------------------------------------------------

func TestAuditProtectionMigration_FilesExist(t *testing.T) {
	// Verify the migration files are embedded in the binary.
	upContent, err := migrationsFS.ReadFile("migrations/000005_audit_protection.up.sql")
	require.NoError(t, err, "up migration must be embedded")
	assert.Contains(t, string(upContent), "fn_audit_no_update_delete")
	assert.Contains(t, string(upContent), "no_update_delete_audit")
	assert.Contains(t, string(upContent), "fn_audit_no_truncate")
	assert.Contains(t, string(upContent), "dm_audit_writer")

	downContent, err := migrationsFS.ReadFile("migrations/000005_audit_protection.down.sql")
	require.NoError(t, err, "down migration must be embedded")
	assert.Contains(t, string(downContent), "DROP TRIGGER")
	assert.Contains(t, string(downContent), "DROP FUNCTION")
}

func TestAuditProtectionMigration_TriggerBlocksUpdate(t *testing.T) {
	// Verify the trigger function raises exception on UPDATE.
	upContent, err := migrationsFS.ReadFile("migrations/000005_audit_protection.up.sql")
	require.NoError(t, err)

	content := string(upContent)
	assert.Contains(t, content, "UPDATE operations are prohibited")
	assert.Contains(t, content, "DELETE operations require retention override")
	assert.Contains(t, content, "TRUNCATE operations are prohibited")
}

func TestAuditProtectionMigration_RoleIsNOLOGIN(t *testing.T) {
	upContent, err := migrationsFS.ReadFile("migrations/000005_audit_protection.up.sql")
	require.NoError(t, err)

	content := string(upContent)
	assert.Contains(t, content, "NOLOGIN")
	assert.Contains(t, content, "GRANT INSERT, SELECT ON audit_records TO dm_audit_writer")
	// Must NOT grant UPDATE, DELETE, or TRUNCATE.
	assert.NotContains(t, content, "GRANT UPDATE")
	assert.NotContains(t, content, "GRANT DELETE")
	assert.NotContains(t, content, "GRANT TRUNCATE")
}

func TestAuditProtectionMigration_DownDoesNotDropRole(t *testing.T) {
	downContent, err := migrationsFS.ReadFile("migrations/000005_audit_protection.down.sql")
	require.NoError(t, err)

	content := string(downContent)
	assert.NotContains(t, content, "DROP ROLE")
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
