package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	"contractpro/legal-intelligence-core/internal/domain/model"
	"contractpro/legal-intelligence-core/internal/domain/port"
	"contractpro/legal-intelligence-core/internal/infra/kvstore"
)

// keyPrefixPendingState is the Redis key namespace for the
// gzip+base64-encoded PendingTypeConfirmation blob (high-architecture.md §6.10).
const keyPrefixPendingState = "lic-pending-state:"

// pendingStateStore is the Redis-backed implementation of
// port.PendingStatePort. It is intentionally a thin pass-through:
// the model encode/decode logic owns the gzip+base64 framing.
type pendingStateStore struct {
	kv *kvstore.Client
}

// newPendingStateStore wires a *kvstore.Client into port.PendingStatePort.
func newPendingStateStore(kv *kvstore.Client) *pendingStateStore {
	return &pendingStateStore{kv: kv}
}

// Save serializes pts and persists it with the given TTL.
func (s *pendingStateStore) Save(ctx context.Context, versionID string, pts *model.PendingTypeConfirmation, ttl time.Duration) error {
	if pts == nil {
		return errors.New("app/pending-state: state must not be nil")
	}
	if versionID == "" {
		return errors.New("app/pending-state: versionID must not be empty")
	}
	payload, err := pts.Encode()
	if err != nil {
		return fmt.Errorf("app/pending-state: encode: %w", err)
	}
	return s.kv.Set(ctx, keyPrefixPendingState+versionID, string(payload), ttl)
}

// Load fetches and decodes the persisted PendingTypeConfirmation.
// Returns port.ErrPendingStateNotFound when the key is missing.
func (s *pendingStateStore) Load(ctx context.Context, versionID string) (*model.PendingTypeConfirmation, error) {
	if versionID == "" {
		return nil, errors.New("app/pending-state: versionID must not be empty")
	}
	raw, err := s.kv.Get(ctx, keyPrefixPendingState+versionID)
	if err != nil {
		if errors.Is(err, kvstore.ErrKeyNotFound) {
			return nil, port.ErrPendingStateNotFound
		}
		return nil, fmt.Errorf("app/pending-state: get: %w", err)
	}
	pts, err := model.DecodePendingTypeConfirmation([]byte(raw))
	if err != nil {
		return nil, fmt.Errorf("app/pending-state: decode: %w", err)
	}
	return pts, nil
}

// Delete removes the persisted state. A missing key is not an error.
func (s *pendingStateStore) Delete(ctx context.Context, versionID string) error {
	if versionID == "" {
		return errors.New("app/pending-state: versionID must not be empty")
	}
	_, err := s.kv.Delete(ctx, keyPrefixPendingState+versionID)
	if err != nil {
		return fmt.Errorf("app/pending-state: delete: %w", err)
	}
	return nil
}

var _ port.PendingStatePort = (*pendingStateStore)(nil)
