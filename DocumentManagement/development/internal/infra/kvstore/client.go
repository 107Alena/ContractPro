package kvstore

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"

	"contractpro/document-management/internal/config"
	"contractpro/document-management/internal/domain/model"
	"contractpro/document-management/internal/domain/port"
)

// RedisAPI is a consumer-side interface covering the subset of *redis.Client
// methods used by this client. Defined here to keep the dependency inverted
// and enable unit testing with a mock.
type RedisAPI interface {
	Set(ctx context.Context, key string, value interface{}, expiration time.Duration) *redis.StatusCmd
	Get(ctx context.Context, key string) *redis.StringCmd
	Del(ctx context.Context, keys ...string) *redis.IntCmd
	Ping(ctx context.Context) *redis.StatusCmd
	Close() error
}

// Compile-time interface check.
var _ port.IdempotencyStorePort = (*Client)(nil)

// Client is a Redis-backed implementation of port.IdempotencyStorePort.
// It stores IdempotencyRecord as JSON values with configurable TTL.
type Client struct {
	rdb  RedisAPI
	mu   sync.Mutex
	done chan struct{}
}

// NewClient creates a Client configured for the given KVStoreConfig.
// It dials Redis, pings to verify connectivity, and returns the ready client.
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

// Get retrieves the idempotency record for the given key.
// Returns (nil, nil) if the key does not exist — per port contract.
func (c *Client) Get(ctx context.Context, key string) (*model.IdempotencyRecord, error) {
	if c.isClosed() {
		return nil, errClientClosed("Get")
	}
	if key == "" {
		return nil, port.NewValidationError("kvstore: Get: empty key")
	}

	val, err := c.rdb.Get(ctx, key).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, nil
		}
		return nil, mapError(err, "Get")
	}

	var record model.IdempotencyRecord
	if err := json.Unmarshal([]byte(val), &record); err != nil {
		return nil, port.NewStorageError("kvstore: Get: unmarshal", err)
	}

	return &record, nil
}

// Set creates or updates an idempotency record with a TTL.
// The record is serialized to JSON and stored under record.Key.
func (c *Client) Set(ctx context.Context, record *model.IdempotencyRecord, ttl time.Duration) error {
	if c.isClosed() {
		return errClientClosed("Set")
	}
	if record == nil {
		return port.NewValidationError("kvstore: Set: record must not be nil")
	}

	data, err := json.Marshal(record)
	if err != nil {
		return port.NewStorageError("kvstore: Set: marshal", err)
	}

	if err := c.rdb.Set(ctx, record.Key, data, ttl).Err(); err != nil {
		return mapError(err, "Set")
	}

	return nil
}

// Delete removes an idempotency record by key.
// Deleting a nonexistent key is not an error.
func (c *Client) Delete(ctx context.Context, key string) error {
	if c.isClosed() {
		return errClientClosed("Delete")
	}
	if key == "" {
		return port.NewValidationError("kvstore: Delete: empty key")
	}

	if err := c.rdb.Del(ctx, key).Err(); err != nil {
		return mapError(err, "Delete")
	}

	return nil
}

// Ping checks Redis connectivity. Used by health check handler.
func (c *Client) Ping(ctx context.Context) error {
	if c.isClosed() {
		return errClientClosed("Ping")
	}

	if err := c.rdb.Ping(ctx).Err(); err != nil {
		return mapError(err, "Ping")
	}

	return nil
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

// isClosed returns true if Close has been called.
func (c *Client) isClosed() bool {
	select {
	case <-c.done:
		return true
	default:
		return false
	}
}
