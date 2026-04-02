package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"contractpro/document-management/internal/domain/port"
)

// Compile-time proof that FallbackResolver satisfies port.DocumentFallbackResolver.
var _ port.DocumentFallbackResolver = (*FallbackResolver)(nil)

// FallbackResolver provides cross-tenant document lookup for backward
// compatibility with producer domains that don't include organization_id
// or version_id in events (REV-001/REV-002).
//
// TEMPORARY: remove when DP TASK-056 and TASK-057 are completed.
type FallbackResolver struct{}

// NewFallbackResolver creates a new FallbackResolver.
func NewFallbackResolver() *FallbackResolver {
	return &FallbackResolver{}
}

// ResolveByDocumentID retrieves organization_id and current_version_id for a
// document by its document_id alone (no tenant filter).
//
// This intentionally bypasses tenant isolation for a narrow, read-only
// lookup used only during event ingestion when the producer domain omits
// these fields. The query returns only non-sensitive metadata.
func (r *FallbackResolver) ResolveByDocumentID(ctx context.Context, documentID string) (string, string, error) {
	conn := ConnFromCtx(ctx)

	var orgID string
	var currentVersionID *string

	err := conn.QueryRow(ctx,
		// TEMPORARY: cross-tenant lookup for REV-001/REV-002 fallback.
		// No WHERE organization_id because the caller doesn't have it yet.
		`SELECT organization_id, current_version_id
		 FROM documents
		 WHERE document_id = $1`,
		documentID,
	).Scan(&orgID, &currentVersionID)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", "", &port.DomainError{
				Code:      port.ErrCodeDocumentNotFound,
				Message:   "document " + documentID + " not found (fallback lookup)",
				Retryable: false,
			}
		}
		return "", "", &port.DomainError{
			Code:      port.ErrCodeDatabaseFailed,
			Message:   "fallback resolve for document " + documentID,
			Retryable: true,
			Cause:     err,
		}
	}

	versionID := ""
	if currentVersionID != nil {
		versionID = *currentVersionID
	}

	return orgID, versionID, nil
}
