package port

import (
	"context"
	"errors"
	"testing"
	"time"

	"contractpro/legal-intelligence-core/internal/domain/model"
)

type fakePendingStore struct{}

func (fakePendingStore) Save(context.Context, string, *model.PendingTypeConfirmation, time.Duration) error {
	return nil
}
func (fakePendingStore) Load(context.Context, string) (*model.PendingTypeConfirmation, error) {
	return nil, ErrPendingStateNotFound
}
func (fakePendingStore) Delete(context.Context, string) error { return nil }

var _ PendingStatePort = (*fakePendingStore)(nil)

func TestPendingStore_ErrPendingStateNotFoundIsErrorsIsMatchable(t *testing.T) {
	t.Parallel()
	var p PendingStatePort = fakePendingStore{}
	_, err := p.Load(context.Background(), "v1")
	if err == nil {
		t.Fatal("expected ErrPendingStateNotFound, got nil")
	}
	if !errors.Is(err, ErrPendingStateNotFound) {
		t.Fatalf("errors.Is(err, ErrPendingStateNotFound) = false; err=%v", err)
	}
}
