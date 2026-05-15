package kvstore

import (
	"context"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"
)

// Get returns the value for key. Returns ("", ErrKeyNotFound) when the key
// does not exist — callers use errors.Is(err, ErrKeyNotFound).
func (c *Client) Get(ctx context.Context, key string) (string, error) {
	if c.isClosed() {
		return "", errClientClosed("Get")
	}
	val, err := c.rdb.Get(ctx, key).Result()
	if err != nil {
		// go-redis never co-emits redis.Nil with a context error (a
		// cancelled command yields the context error, never a nil reply),
		// so short-circuiting here cannot mask context passthrough; this
		// mirrors mapError's defensive redis.Nil guard (code-reviewer M1).
		if errors.Is(err, redis.Nil) {
			return "", ErrKeyNotFound
		}
		return "", mapError(err, "Get")
	}
	return val, nil
}

// Set stores key=value with a TTL. ttl<=0 means no expiration (Redis SET
// without EX). Used by the pause-flow (SET lic-pending-state … EX 25h) and
// status writes.
func (c *Client) Set(ctx context.Context, key, value string, ttl time.Duration) error {
	if c.isClosed() {
		return errClientClosed("Set")
	}
	if err := c.rdb.Set(ctx, key, value, ttl).Err(); err != nil {
		return mapError(err, "Set")
	}
	return nil
}

// SetNX atomically sets key=value with TTL only if key does not already
// exist. Returns (true, nil) when the key was set (first writer wins),
// (false, nil) when it already existed. This is the atomic primitive the
// idempotency guard (LIC-TASK-038) builds the SETNX→PROCESSING transition on.
func (c *Client) SetNX(ctx context.Context, key, value string, ttl time.Duration) (bool, error) {
	if c.isClosed() {
		return false, errClientClosed("SetNX")
	}
	ok, err := c.rdb.SetNX(ctx, key, value, ttl).Result()
	if err != nil {
		return false, mapError(err, "SetNX")
	}
	return ok, nil
}

// Delete removes one or more keys and returns the number actually removed
// (missing keys are not an error — Redis DEL semantics). Used by §6.10
// cleanup (DELETE lic-pending-state:{version_id}); variadic so the
// idempotency adapter can batch and ignore the count.
func (c *Client) Delete(ctx context.Context, keys ...string) (int64, error) {
	if c.isClosed() {
		return 0, errClientClosed("Delete")
	}
	n, err := c.rdb.Del(ctx, keys...).Result()
	if err != nil {
		return 0, mapError(err, "Delete")
	}
	return n, nil
}

// Expire sets / refreshes key's TTL. Returns (true, nil) when the key existed
// and the TTL was applied, (false, nil) when the key is gone — the §6.3
// heartbeat (EXPIRE lic-trigger 150s every 30s) uses the false result as the
// signal that the idempotency key vanished and the heartbeat goroutine should
// stop.
func (c *Client) Expire(ctx context.Context, key string, ttl time.Duration) (bool, error) {
	if c.isClosed() {
		return false, errClientClosed("Expire")
	}
	ok, err := c.rdb.Expire(ctx, key, ttl).Result()
	if err != nil {
		return false, mapError(err, "Expire")
	}
	return ok, nil
}

// Eval runs a Lua script with the given KEYS and ARGV and returns its decoded
// result. It uses redis.Script.Run, which optimistically EVALSHA's and
// transparently falls back to EVAL on NOSCRIPT; the *redis.Script (and its
// precomputed SHA1) is cached per source so the token-bucket hot path
// (LIC-TASK-017, run per LLM call) does not rebuild it every call
// (code-architect Q3). A script returning Lua nil surfaces as (nil, nil):
// redis.Nil here means "script returned nil", not "key not found", so it is
// intentionally NOT mapped to ErrKeyNotFound.
func (c *Client) Eval(ctx context.Context, script string, keys []string, args ...any) (any, error) {
	if c.isClosed() {
		return nil, errClientClosed("Eval")
	}
	res, err := c.scriptFor(script).Run(ctx, c.rdb, keys, args...).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, nil
		}
		return nil, mapError(err, "Eval")
	}
	return res, nil
}

// scriptFor returns the cached *redis.Script for src, creating and caching it
// on first use. Concurrency-safe via sync.Map + LoadOrStore (a duplicate
// NewScript lost on a race is harmless and cheap — SHA1 of the source).
func (c *Client) scriptFor(src string) *redis.Script {
	if v, ok := c.scripts.Load(src); ok {
		return v.(*redis.Script)
	}
	s := redis.NewScript(src)
	actual, _ := c.scripts.LoadOrStore(src, s)
	return actual.(*redis.Script)
}
