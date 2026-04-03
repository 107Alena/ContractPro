package tenant

import (
	"context"
	"fmt"

	"contractpro/document-management/internal/domain/port"
)

// Metrics is the consumer-side interface for tenant isolation metrics.
type Metrics interface {
	IncTenantMismatch()
}

// Logger is the minimal structured logging interface.
type Logger interface {
	Warn(msg string, keysAndValues ...any)
}

// VerifyTenantOwnership checks that the document referenced in an async
// event actually belongs to the claimed organization_id (BRE-015).
//
// When the event carries a non-empty organization_id, this function
// performs a DocumentRepository.ExistsByID lookup. If the document does
// not exist under that organization, it returns a non-retryable
// TENANT_MISMATCH DomainError, increments the alert metric, and logs
// a WARN.
//
// When organization_id is empty (fallback path — REV-001/REV-002),
// verification is skipped: the fallback resolver will set the correct
// org_id from the database.
func VerifyTenantOwnership(
	ctx context.Context,
	docRepo DocumentExistenceChecker,
	metrics Metrics,
	logger Logger,
	organizationID string,
	documentID string,
) error {
	// Skip verification when org_id was not provided in the event.
	// The fallback resolver handles resolution in that case.
	if organizationID == "" {
		return nil
	}

	exists, err := docRepo.ExistsByID(ctx, organizationID, documentID)
	if err != nil {
		return err
	}

	if !exists {
		logger.Warn("BRE-015: tenant ownership verification failed",
			"organization_id", organizationID,
			"document_id", documentID,
		)
		metrics.IncTenantMismatch()
		return &port.DomainError{
			Code:      port.ErrCodeTenantMismatch,
			Message:   fmt.Sprintf("document %s does not belong to organization %s", documentID, organizationID),
			Retryable: false,
		}
	}

	return nil
}

// DocumentExistenceChecker is the minimal interface required by
// VerifyTenantOwnership. Satisfied by port.DocumentRepository.
type DocumentExistenceChecker interface {
	ExistsByID(ctx context.Context, organizationID, documentID string) (bool, error)
}
