package postgres

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"

	"contractpro/document-management/internal/domain/model"
	"contractpro/document-management/internal/domain/port"
)

// Compile-time proof that AuditRepository satisfies port.AuditRepository.
var _ port.AuditRepository = (*AuditRepository)(nil)

// AuditRepository implements port.AuditRepository backed by PostgreSQL.
// Audit records are append-only: Insert only, no Update/Delete.
type AuditRepository struct{}

// NewAuditRepository creates a new AuditRepository.
func NewAuditRepository() *AuditRepository {
	return &AuditRepository{}
}

// Insert creates a new audit record.
func (r *AuditRepository) Insert(ctx context.Context, record *model.AuditRecord) error {
	conn := ConnFromCtx(ctx)

	_, err := conn.Exec(ctx,
		`INSERT INTO audit_records
			(audit_id, organization_id, document_id, version_id, action, actor_type,
			 actor_id, job_id, correlation_id, details, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
		record.AuditID,
		record.OrganizationID,
		nullableString(record.DocumentID),
		nullableString(record.VersionID),
		string(record.Action),
		string(record.ActorType),
		record.ActorID,
		nullableString(record.JobID),
		nullableString(record.CorrelationID),
		nullableJSON(record.Details),
		record.CreatedAt,
	)
	if err != nil {
		return port.NewDatabaseError("insert audit record", err)
	}
	return nil
}

// List returns audit records matching the given filter.
func (r *AuditRepository) List(ctx context.Context, params port.AuditListParams) ([]*model.AuditRecord, int, error) {
	conn := ConnFromCtx(ctx)

	// Build dynamic WHERE clause.
	argIdx := 1
	where := fmt.Sprintf("organization_id = $%d", argIdx)
	args := []any{params.OrganizationID}
	argIdx++

	if params.DocumentID != "" {
		where += fmt.Sprintf(" AND document_id = $%d", argIdx)
		args = append(args, params.DocumentID)
		argIdx++
	}
	if params.VersionID != "" {
		where += fmt.Sprintf(" AND version_id = $%d", argIdx)
		args = append(args, params.VersionID)
		argIdx++
	}
	if params.Action != nil {
		where += fmt.Sprintf(" AND action = $%d", argIdx)
		args = append(args, string(*params.Action))
		argIdx++
	}
	if params.ActorType != nil {
		where += fmt.Sprintf(" AND actor_type = $%d", argIdx)
		args = append(args, string(*params.ActorType))
		argIdx++
	}
	if params.Since != nil {
		where += fmt.Sprintf(" AND created_at >= $%d", argIdx)
		args = append(args, *params.Since)
		argIdx++
	}
	if params.Until != nil {
		where += fmt.Sprintf(" AND created_at <= $%d", argIdx)
		args = append(args, *params.Until)
		argIdx++
	}

	offset := (params.Page - 1) * params.PageSize
	query := fmt.Sprintf(
		`SELECT audit_id, organization_id, document_id, version_id, action, actor_type,
				actor_id, job_id, correlation_id, details, created_at,
				COUNT(*) OVER() AS total_count
		FROM audit_records
		WHERE %s
		ORDER BY created_at DESC
		LIMIT $%d OFFSET $%d`,
		where, argIdx, argIdx+1,
	)
	args = append(args, params.PageSize, offset)

	rows, err := conn.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, port.NewDatabaseError("list audit records", err)
	}
	defer rows.Close()

	var records []*model.AuditRecord
	var totalCount int

	for rows.Next() {
		rec, count, err := scanAuditWithTotal(rows)
		if err != nil {
			return nil, 0, port.NewDatabaseError("scan audit row", err)
		}
		totalCount = count
		records = append(records, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, port.NewDatabaseError("iterate audit rows", err)
	}

	if records == nil {
		records = []*model.AuditRecord{}
	}
	return records, totalCount, nil
}

// scanAuditWithTotal scans an audit record row plus a total_count column from Rows.
func scanAuditWithTotal(rows pgx.Rows) (*model.AuditRecord, int, error) {
	var (
		rec           model.AuditRecord
		documentID    *string
		versionID     *string
		action        string
		actorType     string
		jobID         *string
		correlationID *string
		details       []byte
		totalCount    int
	)
	err := rows.Scan(
		&rec.AuditID, &rec.OrganizationID, &documentID, &versionID,
		&action, &actorType, &rec.ActorID,
		&jobID, &correlationID, &details, &rec.CreatedAt,
		&totalCount,
	)
	if err != nil {
		return nil, 0, err
	}
	rec.DocumentID = fromNullableString(documentID)
	rec.VersionID = fromNullableString(versionID)
	rec.Action = model.AuditAction(action)
	rec.ActorType = model.ActorType(actorType)
	rec.JobID = fromNullableString(jobID)
	rec.CorrelationID = fromNullableString(correlationID)
	if details != nil {
		rec.Details = json.RawMessage(details)
	}
	return &rec, totalCount, nil
}

// nullableJSON converts a json.RawMessage to a value suitable for pgx.
// nil or empty RawMessage → nil (SQL NULL).
func nullableJSON(data json.RawMessage) any {
	if len(data) == 0 {
		return nil
	}
	return []byte(data)
}
