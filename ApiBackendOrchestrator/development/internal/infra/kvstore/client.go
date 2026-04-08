package kvstore

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"

	"contractpro/api-orchestrator/internal/config"
)

// RedisAPI is a consumer-side interface covering the subset of redis.Cmdable
// methods used by this client. Defined here (consumer-side) to keep the
// dependency inverted and enable unit testing with a mock.
type RedisAPI interface {
	Get(ctx context.Context, key string) *redis.StringCmd
	Set(ctx context.Context, key string, value interface{}, expiration time.Duration) *redis.StatusCmd
	Del(ctx context.Context, keys ...string) *redis.IntCmd
	Publish(ctx context.Context, channel string, message interface{}) *redis.IntCmd
	Subscribe(ctx context.Context, channels ...string) *redis.PubSub
	Ping(ctx context.Context) *redis.StatusCmd
	Close() error
}

// Client is a Redis-backed key-value store and Pub/Sub client for the
// API Orchestrator. It provides key-value operations (Get, Set, Delete),
// Redis Pub/Sub (Publish, Subscribe), and health checking (Ping).
type Client struct {
	rdb  RedisAPI
	mu   sync.Mutex
	done chan struct{}
}

// NewClient creates a Client configured for the given RedisConfig.
// It dials Redis, pings to verify connectivity, and returns the ready client.
// Returns an error on connection failure.
func NewClient(cfg config.RedisConfig) (*Client, error) {
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

// Set stores a key-value pair with a TTL. If TTL is 0, the key does not expire.
func (c *Client) Set(ctx context.Context, key string, value string, ttl time.Duration) error {
	if c.isClosed() {
		return errClientClosed("Set")
	}
	if err := c.rdb.Set(ctx, key, value, ttl).Err(); err != nil {
		return mapError(err, "Set")
	}
	return nil
}

// Delete removes a key. No error if the key does not exist (idempotent).
func (c *Client) Delete(ctx context.Context, key string) error {
	if c.isClosed() {
		return errClientClosed("Delete")
	}
	if err := c.rdb.Del(ctx, key).Err(); err != nil {
		return mapError(err, "Delete")
	}
	return nil
}

// Publish sends a message to a Redis Pub/Sub channel.
func (c *Client) Publish(ctx context.Context, channel string, message string) error {
	if c.isClosed() {
		return errClientClosed("Publish")
	}
	if err := c.rdb.Publish(ctx, channel, message).Err(); err != nil {
		return mapError(err, "Publish")
	}
	return nil
}

// Subscribe creates a Pub/Sub subscription on the given channel and spawns
// a goroutine that delivers messages to handler. The goroutine exits when
// the returned Subscription is closed or ctx is cancelled.
func (c *Client) Subscribe(ctx context.Context, channel string, handler func(msg string)) (*Subscription, error) {
	if c.isClosed() {
		return nil, errClientClosed("Subscribe")
	}

	pubsub := c.rdb.Subscribe(ctx, channel)

	// Verify subscription succeeded by reading the confirmation message.
	_, err := pubsub.Receive(ctx)
	if err != nil {
		_ = pubsub.Close()
		return nil, mapError(err, "Subscribe")
	}

	subCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})

	sub := &Subscription{
		pubsub: pubsub,
		cancel: cancel,
		done:   done,
	}

	go func() {
		defer close(done)
		ch := pubsub.Channel()
		for {
			select {
			case msg, ok := <-ch:
				if !ok {
					return
				}
				handler(msg.Payload)
			case <-subCtx.Done():
				return
			}
		}
	}()

	return sub, nil
}

// Ping verifies Redis connectivity. Used by the readiness probe.
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
