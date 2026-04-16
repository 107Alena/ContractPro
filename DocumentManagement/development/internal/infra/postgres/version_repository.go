package postgres

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"contractpro/document-management/internal/domain/model"
	"contractpro/document-management/internal/domain/port"
)

// Compile-time proof that VersionRepository satisfies port.VersionRepository.
var _ port.VersionRepository = (*VersionRepository)(nil)

// VersionRepository implements port.VersionRepository backed by PostgreSQL.
type VersionRepository struct{}

// NewVersionRepository creates a new VersionRepository.
func NewVersionRepository() *VersionRepository {
	return &VersionRepository{}
}

// Insert creates a new version record.
func (r *VersionRepository) Insert(ctx context.Context, version *model.DocumentVersion) error {
	conn := ConnFromCtx(ctx)

	_, err := conn.Exec(ctx,
		`INSERT INTO document_versions
			(version_id, document_id, organization_id, version_number, parent_version_id,
			 origin_type, origin_description, source_file_key, source_file_name,
			 source_file_size, source_file_checksum, artifact_status, created_by_user_id, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)`,
		version.VersionID,
		version.DocumentID,
		version.OrganizationID,
		version.VersionNumber,
		nullableString(version.ParentVersionID),
		string(version.OriginType),
		nullableString(version.OriginDescription),
		version.SourceFileKey,
		version.SourceFileName,
		version.SourceFileSize,
		version.SourceFileChecksum,
		string(version.ArtifactStatus),
		version.CreatedByUserID,
		version.CreatedAt,
	)
	if err != nil {
		if isPgUniqueViolation(err) {
			return port.NewVersionAlreadyExistsError(version.VersionID)
		}
		if isPgFKViolation(err) {
			return port.NewDocumentNotFoundError(version.OrganizationID, version.DocumentID)
		}
		return port.NewDatabaseError("insert version", err)
	}
	return nil
}

// FindByID retrieves a version by organization, document, and version ID.
func (r *VersionRepository) FindByID(ctx context.Context, organizationID, documentID, versionID string) (*model.DocumentVersion, error) {
	conn := ConnFromCtx(ctx)

	row := conn.QueryRow(ctx,
		`SELECT version_id, document_id, organization_id, version_number, parent_version_id,
				origin_type, origin_description, source_file_key, source_file_name,
				source_file_size, source_file_checksum, artifact_status, created_by_user_id, created_at
		FROM document_versions
		WHERE version_id = $1 AND document_id = $2 AND organization_id = $3`,
		versionID, documentID, organizationID,
	)

	v, err := scanVersion(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, port.NewVersionNotFoundError(versionID)
		}
		return nil, port.NewDatabaseError("find version by id", err)
	}
	return v, nil
}

// FindByIDForUpdate retrieves a version with a row-level exclusive lock
// (SELECT ... FOR UPDATE). Must be called within a transaction.
// Serializes concurrent artifact_status transitions (BRE-001).
func (r *VersionRepository) FindByIDForUpdate(ctx context.Context, organizationID, documentID, versionID string) (*model.DocumentVersion, error) {
	conn := ConnFromCtx(ctx)

	row := conn.QueryRow(ctx,
		`SELECT version_id, document_id, organization_id, version_number, parent_version_id,
				origin_type, origin_description, source_file_key, source_file_name,
				source_file_size, source_file_checksum, artifact_status, created_by_user_id, created_at
		FROM document_versions
		WHERE version_id = $1 AND document_id = $2 AND organization_id = $3
		FOR UPDATE`,
		versionID, documentID, organizationID,
	)

	v, err := scanVersion(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, port.NewVersionNotFoundError(versionID)
		}
		return nil, port.NewDatabaseError("find version by id for update", err)
	}
	return v, nil
}

// List returns a paginated list of versions for a document.
func (r *VersionRepository) List(ctx context.Context, organizationID, documentID string, page, pageSize int) ([]*model.DocumentVersion, int, error) {
	conn := ConnFromCtx(ctx)

	offset := (page - 1) * pageSize

	rows, err := conn.Query(ctx,
		`SELECT version_id, document_id, organization_id, version_number, parent_version_id,
				origin_type, origin_description, source_file_key, source_file_name,
				source_file_size, source_file_checksum, artifact_status, created_by_user_id, created_at,
				COUNT(*) OVER() AS total_count
		FROM document_versions
		WHERE document_id = $1 AND organization_id = $2
		ORDER BY version_number DESC
		LIMIT $3 OFFSET $4`,
		documentID, organizationID, pageSize, offset,
	)
	if err != nil {
		return nil, 0, port.NewDatabaseError("list versions", err)
	}
	defer rows.Close()

	var versions []*model.DocumentVersion
	var totalCount int

	for rows.Next() {
		var (
			v               model.DocumentVersion
			parentVersionID *string
			originDesc      *string
			originType      string
			artifactStatus  string
		)
		if err := rows.Scan(
			&v.VersionID, &v.DocumentID, &v.OrganizationID, &v.VersionNumber, &parentVersionID,
			&originType, &originDesc, &v.SourceFileKey, &v.SourceFileName,
			&v.SourceFileSize, &v.SourceFileChecksum, &artifactStatus, &v.CreatedByUserID, &v.CreatedAt,
			&totalCount,
		); err != nil {
			return nil, 0, port.NewDatabaseError("scan version row", err)
		}
		v.ParentVersionID = fromNullableString(parentVersionID)
		v.OriginDescription = fromNullableString(originDesc)
		v.OriginType = model.OriginType(originType)
		v.ArtifactStatus = model.ArtifactStatus(artifactStatus)
		versions = append(versions, &v)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, port.NewDatabaseError("iterate version rows", err)
	}

	if versions == nil {
		versions = []*model.DocumentVersion{}
	}
	return versions, totalCount, nil
}

// Update persists changes to a version (artifact_status transitions).
func (r *VersionRepository) Update(ctx context.Context, version *model.DocumentVersion) error {
	conn := ConnFromCtx(ctx)

	tag, err := conn.Exec(ctx,
		`UPDATE document_versions
		SET artifact_status = $1
		WHERE version_id = $2 AND organization_id = $3 AND document_id = $4`,
		string(version.ArtifactStatus),
		version.VersionID,
		version.OrganizationID,
		version.DocumentID,
	)
	if err != nil {
		return port.NewDatabaseError("update version", err)
	}
	if tag.RowsAffected() == 0 {
		return port.NewVersionNotFoundError(version.VersionID)
	}
	return nil
}

// NextVersionNumber returns the next sequential version number for a document.
//
// Race safety: this method is intentionally non-locking. The caller must
// invoke it within a WithTransaction that also performs the Insert.
// The UNIQUE(document_id, version_number) constraint is the final arbiter:
// on conflict, the caller should retry with a fresh number.
func (r *VersionRepository) NextVersionNumber(ctx context.Context, organizationID, documentID string) (int, error) {
	conn := ConnFromCtx(ctx)

	var next int
	err := conn.QueryRow(ctx,
		`SELECT COALESCE(MAX(version_number), 0) + 1
		FROM document_versions
		WHERE document_id = $1 AND organization_id = $2`,
		documentID, organizationID,
	).Scan(&next)
	if err != nil {
		return 0, port.NewDatabaseError("next version number", err)
	}
	return next, nil
}

// FindStaleInIntermediateStatus returns versions stuck in non-terminal states
// whose created_at is older than the per-stage cutoff for their status
// (DM-TASK-053). No row locking — the caller handles locking per-version via
// FindByIDForUpdate in separate transactions.
//
// Only statuses present in cutoffs are scanned. Missing keys (e.g. an operator
// disabled a stage) are silently skipped. If the map is empty, no query is run
// and an empty slice is returned.
func (r *VersionRepository) FindStaleInIntermediateStatus(ctx context.Context, cutoffs map[model.ArtifactStatus]time.Time, limit int) ([]*model.DocumentVersion, error) {
	if len(cutoffs) == 0 {
		return []*model.DocumentVersion{}, nil
	}

	// Build disjunction: (status = $i AND created_at < $i+1) OR ...
	// Deterministic order — iterate over the 4 known intermediate statuses
	// so args line up with placeholders reproducibly across calls.
	knownOrder := []model.ArtifactStatus{
		model.ArtifactStatusPending,
		model.ArtifactStatusProcessingArtifactsReceived,
		model.ArtifactStatusAnalysisArtifactsReceived,
		model.ArtifactStatusReportsReady,
	}

	var (
		conditions []string
		args       []any
	)
	for _, status := range knownOrder {
		cutoff, ok := cutoffs[status]
		if !ok {
			continue
		}
		args = append(args, string(status), cutoff)
		conditions = append(conditions,
			fmt.Sprintf("(artifact_status = $%d AND created_at < $%d)", len(args)-1, len(args)))
	}
	if len(conditions) == 0 {
		return []*model.DocumentVersion{}, nil
	}
	args = append(args, limit)

	query := `SELECT version_id, document_id, organization_id, version_number, parent_version_id,
			origin_type, origin_description, source_file_key, source_file_name,
			source_file_size, source_file_checksum, artifact_status, created_by_user_id, created_at
		FROM document_versions
		WHERE ` + strings.Join(conditions, " OR ") + `
		ORDER BY created_at ASC
		LIMIT $` + strconv.Itoa(len(args))

	conn := ConnFromCtx(ctx)
	rows, err := conn.Query(ctx, query, args...)
	if err != nil {
		return nil, port.NewDatabaseError("find stale versions", err)
	}
	defer rows.Close()

	var versions []*model.DocumentVersion
	for rows.Next() {
		v, err := scanVersion(rows)
		if err != nil {
			return nil, port.NewDatabaseError("scan stale version row", err)
		}
		versions = append(versions, v)
	}
	if err := rows.Err(); err != nil {
		return nil, port.NewDatabaseError("iterate stale version rows", err)
	}
	if versions == nil {
		versions = []*model.DocumentVersion{}
	}
	return versions, nil
}

// DeleteByDocument removes all version rows for a document.
// Cross-tenant system-level query for retention metadata cleanup.
// Must be called after document.current_version_id is cleared and
// after all artifacts for each version are deleted.
func (r *VersionRepository) DeleteByDocument(ctx context.Context, documentID string) error {
	conn := ConnFromCtx(ctx)

	_, err := conn.Exec(ctx,
		`DELETE FROM document_versions WHERE document_id = $1`,
		documentID,
	)
	if err != nil {
		return port.NewDatabaseError("delete versions by document", err)
	}
	return nil
}

// ListByDocument returns all versions for a document (no org filter).
// Cross-tenant system-level query for retention metadata cleanup.
func (r *VersionRepository) ListByDocument(ctx context.Context, documentID string) ([]*model.DocumentVersion, error) {
	conn := ConnFromCtx(ctx)

	rows, err := conn.Query(ctx,
		`SELECT version_id, document_id, organization_id, version_number, parent_version_id,
				origin_type, origin_description, source_file_key, source_file_name,
				source_file_size, source_file_checksum, artifact_status, created_by_user_id, created_at
		FROM document_versions
		WHERE document_id = $1
		ORDER BY version_number ASC`,
		documentID,
	)
	if err != nil {
		return nil, port.NewDatabaseError("list versions by document", err)
	}
	defer rows.Close()

	var versions []*model.DocumentVersion
	for rows.Next() {
		v, err := scanVersion(rows)
		if err != nil {
			return nil, port.NewDatabaseError("scan version row", err)
		}
		versions = append(versions, v)
	}
	if err := rows.Err(); err != nil {
		return nil, port.NewDatabaseError("iterate version rows", err)
	}
	if versions == nil {
		versions = []*model.DocumentVersion{}
	}
	return versions, nil
}

// scanVersion scans a single version row.
func scanVersion(row pgx.Row) (*model.DocumentVersion, error) {
	var (
		v               model.DocumentVersion
		parentVersionID *string
		originDesc      *string
		originType      string
		artifactStatus  string
	)
	err := row.Scan(
		&v.VersionID, &v.DocumentID, &v.OrganizationID, &v.VersionNumber, &parentVersionID,
		&originType, &originDesc, &v.SourceFileKey, &v.SourceFileName,
		&v.SourceFileSize, &v.SourceFileChecksum, &artifactStatus, &v.CreatedByUserID, &v.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	v.ParentVersionID = fromNullableString(parentVersionID)
	v.OriginDescription = fromNullableString(originDesc)
	v.OriginType = model.OriginType(originType)
	v.ArtifactStatus = model.ArtifactStatus(artifactStatus)
	return &v, nil
}
