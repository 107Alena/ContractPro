package kvstore

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"

	"contractpro/document-processing/internal/config"
)

// RedisAPI is a consumer-side interface covering the subset of redis.Cmdable
// methods used by this client. Defined here (consumer-side) to keep the
// dependency inverted and enable unit testing with a mock.
type RedisAPI interface {
	Set(ctx context.Context, key string, value interface{}, expiration time.Duration) *redis.StatusCmd
	Get(ctx context.Context, key string) *redis.StringCmd
	Exists(ctx context.Context, keys ...string) *redis.IntCmd
	Close() error
	Ping(ctx context.Context) *redis.StatusCmd
}

// Client is a Redis-backed KV-store client that provides Set/Get/Exists
// operations with connection pooling and graceful shutdown.
//
// Client does not implement any domain port directly — it is used by
// higher-level adapters (ingress/idempotency) that implement IdempotencyStorePort.
type Client struct {
	rdb  RedisAPI
	mu   sync.Mutex
	done chan struct{}
}

// NewClient creates a Client configured for the given KVStoreConfig.
// It dials Redis, pings to verify connectivity, and returns the ready client.
// Returns a DomainError on connection failure.
func NewClient(cfg config.KVStoreConfig) (*Client, error) {
	rdb := redis.NewClient(&redis.Options{
		Addr:         cfg.Address,
		Password:     cfg.Password,
		DB:           cfg.DB,
		PoolSize:     cfg.PoolSize,
		DialTimeout:  cfg.Timeout,
		ReadTimeout:  cfg.Timeout,
		WriteTimeout: cfg.Timeout,
	})

	ctx, cancel := context.WithTimeout(context.Background(), cfg.Timeout)
	defer cancel()

	if err := rdb.Ping(ctx).Err(); err != nil {
		_ = rdb.Close()
		return nil, mapError(err, "Ping")
	}

	return &Client{
		rdb:  rdb,
		done: make(chan struct{}),
	}, nil
}

// newClientWithRedis creates a Client with an injected RedisAPI (for testing).
func newClientWithRedis(api RedisAPI) *Client {
	return &Client{
		rdb:  api,
		done: make(chan struct{}),
	}
}

// Set stores a key-value pair with a TTL. If TTL is 0, the key does not expire.
func (c *Client) Set(ctx context.Context, key string, value string, ttl time.Duration) error {
	if c.isClosed() {
		return errClientClosed("Set")
	}
	err := c.rdb.Set(ctx, key, value, ttl).Err()
	if err != nil {
		return mapError(err, "Set")
	}
	return nil
}

// Get retrieves the value for a key. Returns ("", ErrKeyNotFound) if the key
// does not exist.
func (c *Client) Get(ctx context.Context, key string) (string, error) {
	if c.isClosed() {
		return "", errClientClosed("Get")
	}
	val, err := c.rdb.Get(ctx, key).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return "", ErrKeyNotFound
		}
		return "", mapError(err, "Get")
	}
	return val, nil
}

// Exists returns true if the key exists in the store.
func (c *Client) Exists(ctx context.Context, key string) (bool, error) {
	if c.isClosed() {
		return false, errClientClosed("Exists")
	}
	n, err := c.rdb.Exists(ctx, key).Result()
	if err != nil {
		return false, mapError(err, "Exists")
	}
	return n > 0, nil
}

// isClosed returns true if Close has been called.
func (c *Client) isClosed() bool {
	select {
	case <-c.done:
		return true
	default:
		return false
	}
}

// Close performs a graceful shutdown of the Redis connection pool.
// Safe to call multiple times.
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	select {
	case <-c.done:
		return nil // already closed
	default:
	}

	close(c.done)

	if c.rdb != nil {
		if err := c.rdb.Close(); err != nil {
			return mapError(err, "Close")
		}
	}
	return nil
}
