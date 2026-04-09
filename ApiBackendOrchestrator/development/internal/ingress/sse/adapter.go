package sse

import (
	"context"
	"time"

	"contractpro/api-orchestrator/internal/infra/kvstore"
)

// Compile-time check that *KVStoreAdapter satisfies KVStore.
var _ KVStore = (*KVStoreAdapter)(nil)

// KVStoreAdapter wraps *kvstore.Client to satisfy the sse.KVStore interface.
//
// The adapter is necessary because kvstore.Client.Subscribe returns the
// concrete *kvstore.Subscription, while sse.KVStore.Subscribe returns the
// sse.Subscription interface. Go's type system requires exact return type
// matching for interface satisfaction, so this thin adapter bridges the gap.
//
// All other methods (Set, Delete) are delegated directly.
type KVStoreAdapter struct {
	client *kvstore.Client
}

// NewKVStoreAdapter creates a KVStoreAdapter wrapping the given kvstore.Client.
func NewKVStoreAdapter(client *kvstore.Client) *KVStoreAdapter {
	return &KVStoreAdapter{client: client}
}

// Subscribe delegates to kvstore.Client.Subscribe and wraps the returned
// *kvstore.Subscription as an sse.Subscription interface.
func (a *KVStoreAdapter) Subscribe(ctx context.Context, channel string, handler func(msg string)) (Subscription, error) {
	return a.client.Subscribe(ctx, channel, handler)
}

// Set delegates to kvstore.Client.Set.
func (a *KVStoreAdapter) Set(ctx context.Context, key string, value string, ttl time.Duration) error {
	return a.client.Set(ctx, key, value, ttl)
}

// Delete delegates to kvstore.Client.Delete.
func (a *KVStoreAdapter) Delete(ctx context.Context, key string) error {
	return a.client.Delete(ctx, key)
}
