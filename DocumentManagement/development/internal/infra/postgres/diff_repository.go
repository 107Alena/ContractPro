package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"contractpro/document-management/internal/domain/model"
	"contractpro/document-management/internal/domain/port"
)

// Compile-time proof that DiffRepository satisfies port.DiffRepository.
var _ port.DiffRepository = (*DiffRepository)(nil)

// DiffRepository implements port.DiffRepository backed by PostgreSQL.
type DiffRepository struct{}

// NewDiffRepository creates a new DiffRepository.
func NewDiffRepository() *DiffRepository {
	return &DiffRepository{}
}

// Insert creates a new diff reference.
func (r *DiffRepository) Insert(ctx context.Context, ref *model.VersionDiffReference) error {
	conn := ConnFromCtx(ctx)

	_, err := conn.Exec(ctx,
		`INSERT INTO version_diff_references
			(diff_id, document_id, organization_id, base_version_id, target_version_id,
			 storage_key, text_diff_count, structural_diff_count, job_id, correlation_id, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
		ref.DiffID,
		ref.DocumentID,
		ref.OrganizationID,
		ref.BaseVersionID,
		ref.TargetVersionID,
		ref.StorageKey,
		ref.TextDiffCount,
		ref.StructuralDiffCount,
		ref.JobID,
		ref.CorrelationID,
		ref.CreatedAt,
	)
	if err != nil {
		if isPgUniqueViolation(err) {
			return port.NewDiffAlreadyExistsError(ref.BaseVersionID, ref.TargetVersionID)
		}
		if isPgFKViolation(err) {
			return port.NewDatabaseError("insert diff reference: FK violation", err)
		}
		return port.NewDatabaseError("insert diff reference", err)
	}
	return nil
}

// FindByVersionPair retrieves a diff reference by base and target versions.
func (r *DiffRepository) FindByVersionPair(
	ctx context.Context, organizationID, documentID, baseVersionID, targetVersionID string,
) (*model.VersionDiffReference, error) {
	conn := ConnFromCtx(ctx)

	row := conn.QueryRow(ctx,
		`SELECT diff_id, document_id, organization_id, base_version_id, target_version_id,
				storage_key, text_diff_count, structural_diff_count, job_id, correlation_id, created_at
		FROM version_diff_references
		WHERE base_version_id = $1 AND target_version_id = $2
			AND organization_id = $3 AND document_id = $4`,
		baseVersionID, targetVersionID, organizationID, documentID,
	)

	ref, err := scanDiff(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, port.NewDiffNotFoundError(baseVersionID, targetVersionID)
		}
		return nil, port.NewDatabaseError("find diff by version pair", err)
	}
	return ref, nil
}

// ListByDocument returns all diff references for a document.
func (r *DiffRepository) ListByDocument(ctx context.Context, organizationID, documentID string) ([]*model.VersionDiffReference, error) {
	conn := ConnFromCtx(ctx)

	rows, err := conn.Query(ctx,
		`SELECT diff_id, document_id, organization_id, base_version_id, target_version_id,
				storage_key, text_diff_count, structural_diff_count, job_id, correlation_id, created_at
		FROM version_diff_references
		WHERE document_id = $1 AND organization_id = $2
		ORDER BY created_at DESC`,
		documentID, organizationID,
	)
	if err != nil {
		return nil, port.NewDatabaseError("list diffs by document", err)
	}
	defer rows.Close()

	var diffs []*model.VersionDiffReference
	for rows.Next() {
		var ref model.VersionDiffReference
		if err := rows.Scan(
			&ref.DiffID, &ref.DocumentID, &ref.OrganizationID,
			&ref.BaseVersionID, &ref.TargetVersionID,
			&ref.StorageKey, &ref.TextDiffCount, &ref.StructuralDiffCount,
			&ref.JobID, &ref.CorrelationID, &ref.CreatedAt,
		); err != nil {
			return nil, port.NewDatabaseError("scan diff row", err)
		}
		diffs = append(diffs, &ref)
	}
	if err := rows.Err(); err != nil {
		return nil, port.NewDatabaseError("iterate diff rows", err)
	}

	if diffs == nil {
		diffs = []*model.VersionDiffReference{}
	}
	return diffs, nil
}

// DeleteByDocument removes all diff references for a document.
func (r *DiffRepository) DeleteByDocument(ctx context.Context, organizationID, documentID string) error {
	conn := ConnFromCtx(ctx)

	_, err := conn.Exec(ctx,
		`DELETE FROM version_diff_references
		WHERE document_id = $1 AND organization_id = $2`,
		documentID, organizationID,
	)
	if err != nil {
		return port.NewDatabaseError("delete diffs by document", err)
	}
	return nil
}

// scanDiff scans a single diff reference row.
func scanDiff(row pgx.Row) (*model.VersionDiffReference, error) {
	var ref model.VersionDiffReference
	err := row.Scan(
		&ref.DiffID, &ref.DocumentID, &ref.OrganizationID,
		&ref.BaseVersionID, &ref.TargetVersionID,
		&ref.StorageKey, &ref.TextDiffCount, &ref.StructuralDiffCount,
		&ref.JobID, &ref.CorrelationID, &ref.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &ref, nil
}
