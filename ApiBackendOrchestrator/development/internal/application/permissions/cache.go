package permissions

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"contractpro/api-orchestrator/internal/infra/kvstore"
	"contractpro/api-orchestrator/internal/ingress/middleware/auth"
)

// RedisCache is the production CacheStore backed by kvstore.Client.
// Values are JSON-serialized UserPermissions with a TTL.
type RedisCache struct {
	kv  KVClient
	ttl time.Duration
}

// KVClient is the subset of kvstore operations the cache needs.
type KVClient interface {
	Get(ctx context.Context, key string) (string, error)
	Set(ctx context.Context, key string, value string, ttl time.Duration) error
}

// Compile-time interface checks.
var _ CacheStore = (*RedisCache)(nil)
var _ KVClient = (*kvstore.Client)(nil)

// NewRedisCache creates a RedisCache with the given TTL for new entries.
func NewRedisCache(kv KVClient, ttl time.Duration) *RedisCache {
	return &RedisCache{kv: kv, ttl: ttl}
}

// Get returns cached UserPermissions for (orgID, role). A key-not-found
// condition returns (zero, false, nil) — callers MUST treat !ok as a miss
// without inspecting err.
func (c *RedisCache) Get(ctx context.Context, orgID string, role auth.Role) (UserPermissions, bool, error) {
	var zero UserPermissions
	raw, err := c.kv.Get(ctx, CacheKey(orgID, role))
	if err != nil {
		if errors.Is(err, kvstore.ErrKeyNotFound) {
			return zero, false, nil
		}
		return zero, false, err
	}
	var perms UserPermissions
	if uerr := json.Unmarshal([]byte(raw), &perms); uerr != nil {
		return zero, false, uerr
	}
	return perms, true, nil
}

// Set stores UserPermissions for (orgID, role) with the configured TTL.
func (c *RedisCache) Set(ctx context.Context, orgID string, role auth.Role, perms UserPermissions) error {
	data, err := json.Marshal(perms)
	if err != nil {
		return err
	}
	return c.kv.Set(ctx, CacheKey(orgID, role), string(data), c.ttl)
}
