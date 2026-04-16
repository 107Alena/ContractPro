package permissions

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"contractpro/api-orchestrator/internal/infra/kvstore"
	"contractpro/api-orchestrator/internal/ingress/middleware/auth"
)

// ---------------------------------------------------------------------------
// In-memory KV store that returns kvstore.ErrKeyNotFound on missing keys.
// ---------------------------------------------------------------------------

type fakeKV struct {
	mu      sync.Mutex
	entries map[string]string
}

func newFakeKV() *fakeKV {
	return &fakeKV{entries: map[string]string{}}
}

func (f *fakeKV) Get(_ context.Context, key string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	v, ok := f.entries[key]
	if !ok {
		return "", kvstore.ErrKeyNotFound
	}
	return v, nil
}

func (f *fakeKV) Set(_ context.Context, key, value string, _ time.Duration) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.entries[key] = value
	return nil
}

func TestRedisCache_SetThenGet(t *testing.T) {
	kv := newFakeKV()
	c := NewRedisCache(kv, 5*time.Minute)

	if err := c.Set(context.Background(), "org-1", auth.RoleBusinessUser,
		UserPermissions{ExportEnabled: true}); err != nil {
		t.Fatalf("Set: %v", err)
	}
	got, ok, err := c.Get(context.Background(), "org-1", auth.RoleBusinessUser)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !ok {
		t.Fatalf("Get returned ok=false after Set")
	}
	if !got.ExportEnabled {
		t.Errorf("ExportEnabled = false, want true")
	}
}

func TestRedisCache_Get_MissReturnsNotOk(t *testing.T) {
	kv := newFakeKV()
	c := NewRedisCache(kv, 5*time.Minute)

	_, ok, err := c.Get(context.Background(), "org-1", auth.RoleBusinessUser)
	if err != nil {
		t.Errorf("Get: unexpected error %v", err)
	}
	if ok {
		t.Error("Get returned ok=true for missing key")
	}
}

type errKV struct{}

func (errKV) Get(context.Context, string) (string, error) {
	return "", errors.New("backend error")
}
func (errKV) Set(context.Context, string, string, time.Duration) error {
	return errors.New("backend error")
}

func TestRedisCache_Get_BackendErrorPropagated(t *testing.T) {
	c := NewRedisCache(errKV{}, 5*time.Minute)
	_, ok, err := c.Get(context.Background(), "org-1", auth.RoleBusinessUser)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if ok {
		t.Errorf("ok = true on backend error")
	}
}

func TestRedisCache_Get_CorruptedJSONReturnsError(t *testing.T) {
	kv := newFakeKV()
	_ = kv.Set(context.Background(), CacheKey("org-1", auth.RoleBusinessUser),
		"not json", 0)

	c := NewRedisCache(kv, 5*time.Minute)
	_, ok, err := c.Get(context.Background(), "org-1", auth.RoleBusinessUser)
	if err == nil {
		t.Fatal("expected JSON decode error, got nil")
	}
	if ok {
		t.Errorf("ok = true on malformed JSON")
	}
}
