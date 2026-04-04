package postgres

import (
	"context"
	"time"

	"contractpro/document-management/internal/domain/port"
)

// Compile-time interface check.
var _ port.OrphanCandidateRepository = (*OrphanCandidateRepository)(nil)

// OrphanCandidateRepository implements port.OrphanCandidateRepository using
// PostgreSQL. All queries are system-level (cross-tenant) — orphan_candidates
// has no organization_id column and is excluded from RLS.
type OrphanCandidateRepository struct{}

// NewOrphanCandidateRepository creates a new OrphanCandidateRepository.
func NewOrphanCandidateRepository() *OrphanCandidateRepository {
	return &OrphanCandidateRepository{}
}

// FindOlderThan returns orphan candidates whose created_at is older than
// the given cutoff, up to limit results ordered by created_at ASC.
func (r *OrphanCandidateRepository) FindOlderThan(ctx context.Context, cutoff time.Time, limit int) ([]port.OrphanCandidate, error) {
	conn := ConnFromCtx(ctx)

	const q = `SELECT storage_key, version_id, created_at
		FROM orphan_candidates
		WHERE created_at < $1
		ORDER BY created_at ASC
		LIMIT $2`

	rows, err := conn.Query(ctx, q, cutoff, limit)
	if err != nil {
		return nil, port.NewDatabaseError("find orphan candidates", err)
	}
	defer rows.Close()

	var result []port.OrphanCandidate
	for rows.Next() {
		var c port.OrphanCandidate
		var versionID *string
		if err := rows.Scan(&c.StorageKey, &versionID, &c.CreatedAt); err != nil {
			return nil, port.NewDatabaseError("scan orphan candidate", err)
		}
		c.VersionID = fromNullableString(versionID)
		result = append(result, c)
	}
	if err := rows.Err(); err != nil {
		return nil, port.NewDatabaseError("iterate orphan candidates", err)
	}

	if result == nil {
		result = []port.OrphanCandidate{}
	}
	return result, nil
}

// ExistsByStorageKey checks whether an ArtifactDescriptor row exists for the
// given storage_key. Cross-tenant lookup against artifact_descriptors using
// the idx_artifacts_storage_key index.
func (r *OrphanCandidateRepository) ExistsByStorageKey(ctx context.Context, storageKey string) (bool, error) {
	conn := ConnFromCtx(ctx)

	const q = `SELECT EXISTS(SELECT 1 FROM artifact_descriptors WHERE storage_key = $1)`

	var exists bool
	if err := conn.QueryRow(ctx, q, storageKey).Scan(&exists); err != nil {
		return false, port.NewDatabaseError("check storage key existence", err)
	}
	return exists, nil
}

// DeleteByKeys removes orphan_candidates rows for the specified storage keys.
// Idempotent: keys that don't exist are silently ignored.
func (r *OrphanCandidateRepository) DeleteByKeys(ctx context.Context, storageKeys []string) error {
	if len(storageKeys) == 0 {
		return nil
	}
	conn := ConnFromCtx(ctx)

	const q = `DELETE FROM orphan_candidates WHERE storage_key = ANY($1)`

	if _, err := conn.Exec(ctx, q, storageKeys); err != nil {
		return port.NewDatabaseError("delete orphan candidates", err)
	}
	return nil
}

// Insert records a new orphan candidate. Idempotent via ON CONFLICT DO NOTHING.
func (r *OrphanCandidateRepository) Insert(ctx context.Context, candidate port.OrphanCandidate) error {
	conn := ConnFromCtx(ctx)

	const q = `INSERT INTO orphan_candidates (storage_key, version_id, created_at)
		VALUES ($1, $2, $3)
		ON CONFLICT (storage_key) DO NOTHING`

	if _, err := conn.Exec(ctx, q, candidate.StorageKey, nullableString(candidate.VersionID), candidate.CreatedAt); err != nil {
		return port.NewDatabaseError("insert orphan candidate", err)
	}
	return nil
}
