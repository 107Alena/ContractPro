package port

import (
	"context"
	"errors"
	"time"

	"contractpro/legal-intelligence-core/internal/domain/model"
)

// ErrPendingStateNotFound is returned by Load when no pending state exists
// under the given version_id (high-architecture.md §6.10 Resume step 4 —
// miss leads to USER_CONFIRMATION_EXPIRED).
var ErrPendingStateNotFound = errors.New("pending state not found")

// PendingStatePort persists the gzip+base64-encoded PendingTypeConfirmation
// blob in Redis under lic-pending-state:{version_id} with TTL 25h
// (high-architecture.md §6.10).
//
// The store sees opaque bytes — encoding and decoding are the responsibility
// of model.PendingTypeConfirmation.Encode / DecodePendingTypeConfirmation,
// so callers always pass / receive the typed struct.
//
// Implementations are Redis-backed (LIC-TASK-037).
type PendingStatePort interface {
	// Save serializes and stores the pending state for `versionID` with
	// the given TTL. Used at the pause step of §6.10 (between SET
	// pending-state and publishing classification-uncertain).
	Save(ctx context.Context, versionID string, state *model.PendingTypeConfirmation, ttl time.Duration) error

	// Load fetches and decodes the pending state for `versionID`.
	// Returns ErrPendingStateNotFound when the key is missing — callers
	// translate that into a FAILED with error_code=USER_CONFIRMATION_EXPIRED.
	Load(ctx context.Context, versionID string) (*model.PendingTypeConfirmation, error)

	// Delete drops the pending state — called after the resumed pipeline
	// reaches COMPLETED so memory budgeted at ~5 GB for 1000 concurrent
	// pauses (high-architecture.md §6.14) stays bounded.
	Delete(ctx context.Context, versionID string) error
}
