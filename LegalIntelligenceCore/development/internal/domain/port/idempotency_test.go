package port

import (
	"context"
	"testing"
	"time"
)

type fakeIdempotencyStore struct{}

func (fakeIdempotencyStore) SetNX(context.Context, string, time.Duration) (IdempotencyStatus, error) {
	return IdempotencyAbsent, nil
}
func (fakeIdempotencyStore) Get(context.Context, string) (IdempotencyStatus, error) {
	return IdempotencyAbsent, nil
}
func (fakeIdempotencyStore) ExtendTTL(context.Context, string, time.Duration) error { return nil }
func (fakeIdempotencyStore) SetCompleted(context.Context, string, time.Duration) error {
	return nil
}
func (fakeIdempotencyStore) SetPaused(context.Context, string, time.Duration) error { return nil }

var _ IdempotencyStorePort = (*fakeIdempotencyStore)(nil)

func TestIdempotencyStatus_IsTerminal(t *testing.T) {
	t.Parallel()
	cases := map[IdempotencyStatus]bool{
		IdempotencyAbsent:     false,
		IdempotencyProcessing: false,
		IdempotencyPaused:     false,
		IdempotencyCompleted:  true,
	}
	for in, want := range cases {
		if got := in.IsTerminal(); got != want {
			t.Errorf("%q.IsTerminal() = %v, want %v", in, got, want)
		}
	}
}
