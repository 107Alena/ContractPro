// Package ratelimit provides per-organization rate limiting middleware
// for the API/Backend Orchestrator.
//
// The middleware uses a Redis-backed fixed-window counter with separate
// read/write RPS limits. Read operations (GET, HEAD) consume from the
// read bucket; all mutating methods (POST, PUT, DELETE, PATCH) consume
// from the write bucket. Both buckets use a 1-second window.
//
// When Redis is unavailable the middleware degrades gracefully: requests
// are allowed through with a WARN log, preventing a Redis outage from
// blocking the entire API.
package ratelimit

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// RateLimiterStore abstracts the atomic "increment-and-check" operation
// used by the rate limiter. Implementations must guarantee atomicity:
// the counter increment and limit check happen in a single step.
//
// This is a consumer-side interface (defined here, not in the infra
// layer) following the project's hexagonal architecture pattern.
type RateLimiterStore interface {
	// Allow atomically increments the counter for key and returns true
	// if the new count is within limit. The counter auto-expires after
	// window elapses. If the key does not exist, it is created with
	// count=1 and TTL=window.
	//
	// Returns (true, nil) if allowed, (false, nil) if limit exceeded.
	// Returns (false, error) on infrastructure failure.
	Allow(ctx context.Context, key string, limit int, window time.Duration) (bool, error)
}

// luaAllowScript is a Redis Lua script that atomically increments a
// counter and checks it against a limit.
//
//	KEYS[1] = rate limit key (e.g., "rl:{org_id}:read")
//	ARGV[1] = limit (e.g., "200")
//	ARGV[2] = window in milliseconds (e.g., "1000")
//
// Returns 1 if allowed, 0 if rejected.
//
// INCR on a non-existent key creates it with value 1. PEXPIRE is set
// only on the first request in the window (current == 1) to prevent
// TTL resets on subsequent requests. The script executes atomically.
const luaAllowScript = `
local current = redis.call('INCR', KEYS[1])
if current == 1 then
    redis.call('PEXPIRE', KEYS[1], ARGV[2])
end
if current > tonumber(ARGV[1]) then
    return 0
end
return 1`

// evalFunc is a function type for executing a Lua script and returning
// an int64 result. This indirection makes RedisStore testable without
// requiring a full redis.Cmdable mock.
type evalFunc func(ctx context.Context, script string, keys []string, args ...interface{}) (int64, error)

// RedisStore implements RateLimiterStore using a Redis Lua script for
// atomic increment-and-check.
type RedisStore struct {
	eval evalFunc
}

// Compile-time interface check.
var _ RateLimiterStore = (*RedisStore)(nil)

// NewRedisStore creates a RedisStore backed by the given redis.Cmdable.
// In production this is typically the *redis.Client obtained from
// kvstore.Client.RawRedis().
func NewRedisStore(rdb redis.Cmdable) *RedisStore {
	return &RedisStore{
		eval: func(ctx context.Context, script string, keys []string, args ...interface{}) (int64, error) {
			return rdb.Eval(ctx, script, keys, args...).Int64()
		},
	}
}

// newRedisStoreWithEval creates a RedisStore with a custom eval function.
// Used in tests to inject mock behaviour without a real Redis connection.
func newRedisStoreWithEval(fn evalFunc) *RedisStore {
	return &RedisStore{eval: fn}
}

// Allow atomically increments the counter for key and checks against limit.
func (s *RedisStore) Allow(ctx context.Context, key string, limit int, window time.Duration) (bool, error) {
	windowMs := window.Milliseconds()
	result, err := s.eval(ctx, luaAllowScript, []string{key}, limit, windowMs)
	if err != nil {
		return false, fmt.Errorf("rate limiter eval: %w", err)
	}
	return result == 1, nil
}
