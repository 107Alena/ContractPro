package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"contractpro/document-management/internal/domain/model"
	"contractpro/document-management/internal/domain/port"
)

// Compile-time proof that DocumentRepository satisfies port.DocumentRepository.
var _ port.DocumentRepository = (*DocumentRepository)(nil)

// DocumentRepository implements port.DocumentRepository backed by PostgreSQL.
type DocumentRepository struct{}

// NewDocumentRepository creates a new DocumentRepository.
func NewDocumentRepository() *DocumentRepository {
	return &DocumentRepository{}
}

// Insert creates a new document record.
func (r *DocumentRepository) Insert(ctx context.Context, doc *model.Document) error {
	conn := ConnFromCtx(ctx)

	_, err := conn.Exec(ctx,
		`INSERT INTO documents
			(document_id, organization_id, title, current_version_id, status,
			 created_by_user_id, created_at, updated_at, deleted_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		doc.DocumentID,
		doc.OrganizationID,
		doc.Title,
		nullableString(doc.CurrentVersionID),
		string(doc.Status),
		doc.CreatedByUserID,
		doc.CreatedAt,
		doc.UpdatedAt,
		doc.DeletedAt,
	)
	if err != nil {
		if isPgUniqueViolation(err) {
			return port.NewDocumentAlreadyExistsError(doc.DocumentID)
		}
		if isPgFKViolation(err) {
			return port.NewVersionNotFoundError(doc.CurrentVersionID)
		}
		return port.NewDatabaseError("insert document", err)
	}
	return nil
}

// FindByID retrieves a document by organization and document ID.
func (r *DocumentRepository) FindByID(ctx context.Context, organizationID, documentID string) (*model.Document, error) {
	conn := ConnFromCtx(ctx)

	row := conn.QueryRow(ctx,
		`SELECT document_id, organization_id, title, current_version_id, status,
				created_by_user_id, created_at, updated_at, deleted_at
		FROM documents
		WHERE document_id = $1 AND organization_id = $2`,
		documentID, organizationID,
	)

	doc, err := scanDocument(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, port.NewDocumentNotFoundError(organizationID, documentID)
		}
		return nil, port.NewDatabaseError("find document by id", err)
	}
	return doc, nil
}

// FindByIDForUpdate retrieves a document with a row-level exclusive lock
// (SELECT ... FOR UPDATE). Must be called within a transaction.
// Serializes concurrent version creation on the same document (BRE-005).
func (r *DocumentRepository) FindByIDForUpdate(ctx context.Context, organizationID, documentID string) (*model.Document, error) {
	conn := ConnFromCtx(ctx)

	row := conn.QueryRow(ctx,
		`SELECT document_id, organization_id, title, current_version_id, status,
				created_by_user_id, created_at, updated_at, deleted_at
		FROM documents
		WHERE document_id = $1 AND organization_id = $2
		FOR UPDATE`,
		documentID, organizationID,
	)

	doc, err := scanDocument(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, port.NewDocumentNotFoundError(organizationID, documentID)
		}
		return nil, port.NewDatabaseError("find document by id for update", err)
	}
	return doc, nil
}

// List returns a paginated list of documents for the organization.
func (r *DocumentRepository) List(ctx context.Context, organizationID string, statusFilter *model.DocumentStatus, page, pageSize int) ([]*model.Document, int, error) {
	conn := ConnFromCtx(ctx)

	offset := (page - 1) * pageSize

	var query string
	var args []any

	if statusFilter != nil {
		query = `SELECT document_id, organization_id, title, current_version_id, status,
					created_by_user_id, created_at, updated_at, deleted_at,
					COUNT(*) OVER() AS total_count
				FROM documents
				WHERE organization_id = $1 AND status = $2
				ORDER BY created_at DESC
				LIMIT $3 OFFSET $4`
		args = []any{organizationID, string(*statusFilter), pageSize, offset}
	} else {
		query = `SELECT document_id, organization_id, title, current_version_id, status,
					created_by_user_id, created_at, updated_at, deleted_at,
					COUNT(*) OVER() AS total_count
				FROM documents
				WHERE organization_id = $1
				ORDER BY created_at DESC
				LIMIT $2 OFFSET $3`
		args = []any{organizationID, pageSize, offset}
	}

	rows, err := conn.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, port.NewDatabaseError("list documents", err)
	}
	defer rows.Close()

	var docs []*model.Document
	var totalCount int

	for rows.Next() {
		var (
			doc              model.Document
			currentVersionID *string
			status           string
			deletedAt        *time.Time
		)
		if err := rows.Scan(
			&doc.DocumentID, &doc.OrganizationID, &doc.Title, &currentVersionID,
			&status, &doc.CreatedByUserID, &doc.CreatedAt, &doc.UpdatedAt, &deletedAt,
			&totalCount,
		); err != nil {
			return nil, 0, port.NewDatabaseError("scan document row", err)
		}
		doc.CurrentVersionID = fromNullableString(currentVersionID)
		doc.Status = model.DocumentStatus(status)
		doc.DeletedAt = deletedAt
		docs = append(docs, &doc)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, port.NewDatabaseError("iterate document rows", err)
	}

	if docs == nil {
		docs = []*model.Document{}
	}
	return docs, totalCount, nil
}

// Update persists changes to an existing document.
func (r *DocumentRepository) Update(ctx context.Context, doc *model.Document) error {
	conn := ConnFromCtx(ctx)

	tag, err := conn.Exec(ctx,
		`UPDATE documents
		SET title = $1, current_version_id = $2, status = $3, updated_at = $4, deleted_at = $5
		WHERE document_id = $6 AND organization_id = $7`,
		doc.Title,
		nullableString(doc.CurrentVersionID),
		string(doc.Status),
		doc.UpdatedAt,
		doc.DeletedAt,
		doc.DocumentID,
		doc.OrganizationID,
	)
	if err != nil {
		if isPgFKViolation(err) {
			return port.NewVersionNotFoundError(doc.CurrentVersionID)
		}
		return port.NewDatabaseError("update document", err)
	}
	if tag.RowsAffected() == 0 {
		return port.NewDocumentNotFoundError(doc.OrganizationID, doc.DocumentID)
	}
	return nil
}

// ExistsByID returns true if a document exists for the given organization.
func (r *DocumentRepository) ExistsByID(ctx context.Context, organizationID, documentID string) (bool, error) {
	conn := ConnFromCtx(ctx)

	var exists bool
	err := conn.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM documents WHERE document_id = $1 AND organization_id = $2)`,
		documentID, organizationID,
	).Scan(&exists)
	if err != nil {
		return false, port.NewDatabaseError("check document exists", err)
	}
	return exists, nil
}

// FindDeletedOlderThan returns documents with status=DELETED whose
// deleted_at is older than cutoff, up to limit results ordered by deleted_at ASC.
// Cross-tenant system-level query (no org filter) for retention cleanup.
func (r *DocumentRepository) FindDeletedOlderThan(ctx context.Context, cutoff time.Time, limit int) ([]*model.Document, error) {
	conn := ConnFromCtx(ctx)

	rows, err := conn.Query(ctx,
		`SELECT document_id, organization_id, title, current_version_id, status,
				created_by_user_id, created_at, updated_at, deleted_at
		FROM documents
		WHERE status = 'DELETED' AND deleted_at IS NOT NULL AND deleted_at < $1
		ORDER BY deleted_at ASC
		LIMIT $2`,
		cutoff, limit,
	)
	if err != nil {
		return nil, port.NewDatabaseError("find deleted documents older than", err)
	}
	defer rows.Close()

	var docs []*model.Document
	for rows.Next() {
		doc, err := scanDocument(rows)
		if err != nil {
			return nil, port.NewDatabaseError("scan deleted document row", err)
		}
		docs = append(docs, doc)
	}
	if err := rows.Err(); err != nil {
		return nil, port.NewDatabaseError("iterate deleted document rows", err)
	}
	if docs == nil {
		docs = []*model.Document{}
	}
	return docs, nil
}

// DeleteByID hard-deletes a document row by document_id.
// Cross-tenant system-level query. Idempotent: returns nil if row absent.
// Used by metadata retention after all dependent rows are removed.
func (r *DocumentRepository) DeleteByID(ctx context.Context, documentID string) error {
	conn := ConnFromCtx(ctx)

	_, err := conn.Exec(ctx,
		`DELETE FROM documents WHERE document_id = $1`,
		documentID,
	)
	if err != nil {
		return port.NewDatabaseError("delete document by id", err)
	}
	return nil
}

// scanDocument scans a single document row.
func scanDocument(row pgx.Row) (*model.Document, error) {
	var (
		doc              model.Document
		currentVersionID *string
		status           string
		deletedAt        *time.Time
	)
	err := row.Scan(
		&doc.DocumentID, &doc.OrganizationID, &doc.Title, &currentVersionID,
		&status, &doc.CreatedByUserID, &doc.CreatedAt, &doc.UpdatedAt, &deletedAt,
	)
	if err != nil {
		return nil, err
	}
	doc.CurrentVersionID = fromNullableString(currentVersionID)
	doc.Status = model.DocumentStatus(status)
	doc.DeletedAt = deletedAt
	return &doc, nil
}


