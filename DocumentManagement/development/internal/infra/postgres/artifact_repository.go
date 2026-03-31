package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"contractpro/document-management/internal/domain/model"
	"contractpro/document-management/internal/domain/port"
)

// Compile-time proof that ArtifactRepository satisfies port.ArtifactRepository.
var _ port.ArtifactRepository = (*ArtifactRepository)(nil)

// ArtifactRepository implements port.ArtifactRepository backed by PostgreSQL.
type ArtifactRepository struct{}

// NewArtifactRepository creates a new ArtifactRepository.
func NewArtifactRepository() *ArtifactRepository {
	return &ArtifactRepository{}
}

// Insert creates a new artifact descriptor.
func (r *ArtifactRepository) Insert(ctx context.Context, descriptor *model.ArtifactDescriptor) error {
	conn := ConnFromCtx(ctx)

	_, err := conn.Exec(ctx,
		`INSERT INTO artifact_descriptors
			(artifact_id, version_id, document_id, organization_id, artifact_type,
			 producer_domain, storage_key, size_bytes, content_hash, schema_version,
			 job_id, correlation_id, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)`,
		descriptor.ArtifactID,
		descriptor.VersionID,
		descriptor.DocumentID,
		descriptor.OrganizationID,
		string(descriptor.ArtifactType),
		string(descriptor.ProducerDomain),
		descriptor.StorageKey,
		descriptor.SizeBytes,
		descriptor.ContentHash,
		descriptor.SchemaVersion,
		descriptor.JobID,
		descriptor.CorrelationID,
		descriptor.CreatedAt,
	)
	if err != nil {
		if isPgUniqueViolation(err) {
			return port.NewArtifactAlreadyExistsError(descriptor.VersionID, string(descriptor.ArtifactType))
		}
		if isPgFKViolation(err) {
			return port.NewVersionNotFoundError(descriptor.VersionID)
		}
		return port.NewDatabaseError("insert artifact descriptor", err)
	}
	return nil
}

// FindByVersionAndType retrieves an artifact descriptor by version and type.
func (r *ArtifactRepository) FindByVersionAndType(
	ctx context.Context, organizationID, documentID, versionID string, artifactType model.ArtifactType,
) (*model.ArtifactDescriptor, error) {
	conn := ConnFromCtx(ctx)

	row := conn.QueryRow(ctx,
		`SELECT artifact_id, version_id, document_id, organization_id, artifact_type,
				producer_domain, storage_key, size_bytes, content_hash, schema_version,
				job_id, correlation_id, created_at
		FROM artifact_descriptors
		WHERE version_id = $1 AND artifact_type = $2 AND organization_id = $3 AND document_id = $4`,
		versionID, string(artifactType), organizationID, documentID,
	)

	a, err := scanArtifact(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, port.NewArtifactNotFoundError(versionID, string(artifactType))
		}
		return nil, port.NewDatabaseError("find artifact by version and type", err)
	}
	return a, nil
}

// ListByVersion returns all artifact descriptors for a document version.
func (r *ArtifactRepository) ListByVersion(ctx context.Context, organizationID, documentID, versionID string) ([]*model.ArtifactDescriptor, error) {
	conn := ConnFromCtx(ctx)

	rows, err := conn.Query(ctx,
		`SELECT artifact_id, version_id, document_id, organization_id, artifact_type,
				producer_domain, storage_key, size_bytes, content_hash, schema_version,
				job_id, correlation_id, created_at
		FROM artifact_descriptors
		WHERE version_id = $1 AND organization_id = $2 AND document_id = $3
		ORDER BY created_at ASC`,
		versionID, organizationID, documentID,
	)
	if err != nil {
		return nil, port.NewDatabaseError("list artifacts by version", err)
	}
	defer rows.Close()

	return collectArtifacts(rows)
}

// ListByVersionAndTypes returns artifact descriptors for the specified types.
func (r *ArtifactRepository) ListByVersionAndTypes(
	ctx context.Context, organizationID, documentID, versionID string, artifactTypes []model.ArtifactType,
) ([]*model.ArtifactDescriptor, error) {
	conn := ConnFromCtx(ctx)

	// Convert []ArtifactType to []string for pgx array binding.
	typeStrings := make([]string, len(artifactTypes))
	for i, t := range artifactTypes {
		typeStrings[i] = string(t)
	}

	rows, err := conn.Query(ctx,
		`SELECT artifact_id, version_id, document_id, organization_id, artifact_type,
				producer_domain, storage_key, size_bytes, content_hash, schema_version,
				job_id, correlation_id, created_at
		FROM artifact_descriptors
		WHERE version_id = $1 AND organization_id = $2 AND document_id = $3
			AND artifact_type = ANY($4)
		ORDER BY created_at ASC`,
		versionID, organizationID, documentID, typeStrings,
	)
	if err != nil {
		return nil, port.NewDatabaseError("list artifacts by version and types", err)
	}
	defer rows.Close()

	return collectArtifacts(rows)
}

// DeleteByVersion removes all artifact descriptors for a version.
func (r *ArtifactRepository) DeleteByVersion(ctx context.Context, organizationID, documentID, versionID string) error {
	conn := ConnFromCtx(ctx)

	_, err := conn.Exec(ctx,
		`DELETE FROM artifact_descriptors
		WHERE version_id = $1 AND organization_id = $2 AND document_id = $3`,
		versionID, organizationID, documentID,
	)
	if err != nil {
		return port.NewDatabaseError("delete artifacts by version", err)
	}
	return nil
}

// scanArtifact scans a single artifact descriptor row.
func scanArtifact(row pgx.Row) (*model.ArtifactDescriptor, error) {
	var (
		a              model.ArtifactDescriptor
		artifactType   string
		producerDomain string
	)
	err := row.Scan(
		&a.ArtifactID, &a.VersionID, &a.DocumentID, &a.OrganizationID, &artifactType,
		&producerDomain, &a.StorageKey, &a.SizeBytes, &a.ContentHash, &a.SchemaVersion,
		&a.JobID, &a.CorrelationID, &a.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	a.ArtifactType = model.ArtifactType(artifactType)
	a.ProducerDomain = model.ProducerDomain(producerDomain)
	return &a, nil
}

// collectArtifacts scans all rows into a slice of artifact descriptors.
func collectArtifacts(rows pgx.Rows) ([]*model.ArtifactDescriptor, error) {
	var artifacts []*model.ArtifactDescriptor
	for rows.Next() {
		var (
			a              model.ArtifactDescriptor
			artifactType   string
			producerDomain string
		)
		if err := rows.Scan(
			&a.ArtifactID, &a.VersionID, &a.DocumentID, &a.OrganizationID, &artifactType,
			&producerDomain, &a.StorageKey, &a.SizeBytes, &a.ContentHash, &a.SchemaVersion,
			&a.JobID, &a.CorrelationID, &a.CreatedAt,
		); err != nil {
			return nil, port.NewDatabaseError("scan artifact row", err)
		}
		a.ArtifactType = model.ArtifactType(artifactType)
		a.ProducerDomain = model.ProducerDomain(producerDomain)
		artifacts = append(artifacts, &a)
	}
	if err := rows.Err(); err != nil {
		return nil, port.NewDatabaseError("iterate artifact rows", err)
	}
	if artifacts == nil {
		artifacts = []*model.ArtifactDescriptor{}
	}
	return artifacts, nil
}
