// Package kvstore is the Redis adapter for the Legal Intelligence Core.
//
// It provides the raw Redis primitives LIC needs — Get, Set/SetNX with TTL,
// Delete, Expire and Lua Eval — plus a Ping for /readyz and an idempotent
// graceful Close. Configuration comes from config.RedisConfig (LIC_REDIS_*),
// including TLS (redis:// vs rediss:// and the LIC_REDIS_TLS override).
//
// Layering: this package is pure infrastructure and implements NO domain port
// (mirrors internal/infra/broker; LIC has no kvstore domain port). The
// IdempotencyStorePort (LIC-TASK-038) and PendingStatePort (LIC-TASK-037)
// adapters build on these primitives — Lua scripts, EX/EXPIRE semantics, the
// four idempotency statuses and gzip+base64 pending blobs are their concern,
// not this client's. The token-bucket rate limiter (LIC-TASK-017) uses Eval.
//
// go-redis command types are decoded behind the RedisAPI seam: RedisAPI
// returns the raw *redis.StringCmd/StatusCmd/BoolCmd/IntCmd/Cmd and ops.go
// decodes (value, error). Unlike the broker (whose amqp091 Connection.Channel
// returns a concrete type and needs wrapper structs), *redis.Client satisfies
// RedisAPI directly, so there are intentionally NO wrapper types here
// (code-architect Q2 — recorded so a future "consistency with broker" change
// does not add pointless wrappers).
package kvstore

import (
	"context"
	"io"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"

	"contractpro/legal-intelligence-core/internal/config"
)

// RedisAPI is the consumer-side interface covering the subset of *redis.Client
// this package uses. Declaring it here keeps the dependency inverted and lets
// tests run against in-memory fakes without a live Redis. It embeds
// redis.Scripter so a *redis.Script (used by Eval for EVALSHA→EVAL) can run
// against it, and io.Closer for graceful shutdown.
type RedisAPI interface {
	Get(ctx context.Context, key string) *redis.StringCmd
	Set(ctx context.Context, key string, value any, expiration time.Duration) *redis.StatusCmd
	SetNX(ctx context.Context, key string, value any, expiration time.Duration) *redis.BoolCmd
	Del(ctx context.Context, keys ...string) *redis.IntCmd
	Expire(ctx context.Context, key string, expiration time.Duration) *redis.BoolCmd
	Ping(ctx context.Context) *redis.StatusCmd
	redis.Scripter
	io.Closer
}

// Compile-time assertion that the real client satisfies the seam (no wrappers
// needed, unlike the broker).
var _ RedisAPI = (*redis.Client)(nil)

// Client is the LIC Redis client. Safe for concurrent use: the underlying
// go-redis client is concurrency-safe and pooled; mu only guards the
// idempotent Close transition, and scripts is a sync.Map.
type Client struct {
	rdb RedisAPI

	scripts sync.Map // src string -> *redis.Script (EVALSHA cache, Eval hot path)

	mu   sync.Mutex
	done chan struct{}
}

// NewClient builds options from cfg, dials Redis, and Pings to verify
// connectivity before returning. Failing fast at construction (like the
// broker asserting topology) turns a bad URL / credentials / unreachable
// Redis into a startup error rather than a first-operation surprise. Dial /
// Ping errors are credential-redacted (152-ФЗ).
func NewClient(cfg config.RedisConfig) (*Client, error) {
	opt, err := buildOptions(cfg)
	if err != nil {
		return nil, err
	}

	rdb := redis.NewClient(opt)

	ctx, cancel := context.WithTimeout(context.Background(), cfg.DialTimeout)
	defer cancel()

	if err := rdb.Ping(ctx).Err(); err != nil {
		_ = rdb.Close()
		return nil, &RedisError{
			Op:        "Ping",
			Retryable: true,
			Cause:     redactURLCredentials(err, cfg.URL),
		}
	}

	return &Client{
		rdb:  rdb,
		done: make(chan struct{}),
	}, nil
}

// newClientWithRedis builds a Client around an injected RedisAPI for tests.
// It does NOT dial or Ping — tests drive behaviour through the fake (mirrors
// the DP kvstore and LIC broker test seams).
func newClientWithRedis(api RedisAPI) *Client {
	return &Client{
		rdb:  api,
		done: make(chan struct{}),
	}
}

// Ping verifies Redis liveness for /readyz. It honours ctx (the /readyz probe
// has a deadline): ctx is checked first, then passed to the pooled command.
// go-redis already aborts the dial/read on ctx cancellation, so the broker's
// off-goroutine half-open-TCP workaround is intentionally NOT needed here
// (code-architect must-fix 3 — recorded so it is not "fixed" to match broker
// later).
func (c *Client) Ping(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if c.isClosed() {
		return errClientClosed("Ping")
	}
	if err := c.rdb.Ping(ctx).Err(); err != nil {
		return mapError(err, "Ping")
	}
	return nil
}

func (c *Client) isClosed() bool {
	select {
	case <-c.done:
		return true
	default:
		return false
	}
}

// Close performs an idempotent graceful shutdown of the connection pool.
// Safe to call multiple times and concurrently.
func (c *Client) Close() error {
	c.mu.Lock()
	select {
	case <-c.done:
		c.mu.Unlock()
		return nil
	default:
	}
	close(c.done)
	c.mu.Unlock()

	if c.rdb != nil {
		if err := c.rdb.Close(); err != nil {
			return mapError(err, "Close")
		}
	}
	return nil
}
