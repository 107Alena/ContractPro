package permissions

import (
	"context"
	"sync"
	"testing"

	"contractpro/api-orchestrator/internal/ingress/middleware/auth"
)

// fakeDeleter records every Delete call for assertion.
type fakeDeleter struct {
	mu      sync.Mutex
	deleted []string
	err     error
}

func (f *fakeDeleter) Delete(_ context.Context, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deleted = append(f.deleted, key)
	return f.err
}

func (f *fakeDeleter) snapshot() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.deleted))
	copy(out, f.deleted)
	return out
}

// TestInvalidator_InvalidateOrg_DeletesAllRoleKeys verifies the core
// invalidation loop independently of the Pub/Sub plumbing.
func TestInvalidator_InvalidateOrg_DeletesAllRoleKeys(t *testing.T) {
	kv := &fakeDeleter{}
	roles := []auth.Role{auth.RoleLawyer, auth.RoleBusinessUser, auth.RoleOrgAdmin}
	inv := NewCacheInvalidator(kv, nil, testLogger(), roles)
	t.Cleanup(func() { inv.cancel() })

	inv.invalidateOrg("org-1")

	got := kv.snapshot()
	want := []string{
		CacheKey("org-1", auth.RoleLawyer),
		CacheKey("org-1", auth.RoleBusinessUser),
		CacheKey("org-1", auth.RoleOrgAdmin),
	}
	if len(got) != len(want) {
		t.Fatalf("deleted keys = %v, want %v", got, want)
	}
	for i, k := range want {
		if got[i] != k {
			t.Errorf("deleted[%d] = %q, want %q", i, got[i], k)
		}
	}
}

// TestInvalidator_HandleMessage_ParsesOrgID verifies that the channel-name
// parser extracts the orgID correctly.
func TestInvalidator_HandleMessage_ParsesOrgID(t *testing.T) {
	kv := &fakeDeleter{}
	inv := NewCacheInvalidator(kv, nil, testLogger(), nil)
	t.Cleanup(func() { inv.cancel() })

	inv.handleMessage(InvalidateChannel("org-abc"))
	got := kv.snapshot()
	if len(got) != len(KnownRoles) {
		t.Errorf("deleted %d keys, want %d", len(got), len(KnownRoles))
	}
	for _, k := range got {
		if k[:len("permissions:org-abc:")] != "permissions:org-abc:" {
			t.Errorf("unexpected key: %q", k)
		}
	}
}

// TestInvalidator_HandleMessage_IgnoresMalformedChannel verifies robustness.
func TestInvalidator_HandleMessage_IgnoresMalformedChannel(t *testing.T) {
	kv := &fakeDeleter{}
	inv := NewCacheInvalidator(kv, nil, testLogger(), nil)
	t.Cleanup(func() { inv.cancel() })

	inv.handleMessage("wrong-channel")
	if len(kv.snapshot()) != 0 {
		t.Errorf("unexpected deletes on malformed channel")
	}

	// Bare prefix with no orgID after it.
	inv.handleMessage(invalidateChanPref)
	if len(kv.snapshot()) != 0 {
		t.Errorf("unexpected deletes on bare prefix")
	}
}

// TestInvalidator_InvalidateOrg_ContinuesOnDeleteError verifies that one
// failing DEL does not abort the rest of the invalidation.
func TestInvalidator_InvalidateOrg_ContinuesOnDeleteError(t *testing.T) {
	errorOnlyDeleter := &fakeDeleter{err: assertingError("boom")}
	inv := NewCacheInvalidator(errorOnlyDeleter, nil, testLogger(), KnownRoles)
	t.Cleanup(func() { inv.cancel() })

	inv.invalidateOrg("org-1")

	// All deletes attempted, all recorded, despite errors.
	if len(errorOnlyDeleter.snapshot()) != len(KnownRoles) {
		t.Errorf("attempted deletes = %d, want %d",
			len(errorOnlyDeleter.snapshot()), len(KnownRoles))
	}
}

type assertingError string

func (e assertingError) Error() string { return string(e) }

// TestInvalidationPublisher_ComposesChannelName verifies the InvalidateOrg
// method delegates to the Pub/Sub client with the right channel.
func TestInvalidationPublisher_InvalidateOrg(t *testing.T) {
	var gotChannel, gotMessage string
	pub := &fakePublisher{
		publishFn: func(_ context.Context, channel, message string) error {
			gotChannel = channel
			gotMessage = message
			return nil
		},
	}
	p := NewInvalidationPublisher(pub)
	if err := p.InvalidateOrg(context.Background(), "org-1"); err != nil {
		t.Fatalf("InvalidateOrg: %v", err)
	}
	if gotChannel != "permissions:invalidate:org-1" {
		t.Errorf("channel = %q, want permissions:invalidate:org-1", gotChannel)
	}
	if gotMessage != "" {
		t.Errorf("message = %q, want empty", gotMessage)
	}
}

type fakePublisher struct {
	publishFn func(ctx context.Context, channel, message string) error
}

func (f *fakePublisher) Publish(ctx context.Context, channel string, message string) error {
	return f.publishFn(ctx, channel, message)
}
