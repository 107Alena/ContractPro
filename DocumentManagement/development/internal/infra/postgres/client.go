package postgres

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"contractpro/document-management/internal/config"
	"contractpro/document-management/internal/domain/port"
)

// Client is a PostgreSQL connection pool wrapper.
//
// It owns the *pgxpool.Pool lifetime and provides:
//   - Ping() for health/readiness checks.
//   - Close() for graceful shutdown.
//   - Pool() for code that needs the raw pool (e.g. Migrator, Transactor).
//
// Client does not implement port.Transactor; use Transactor for that.
type Client struct {
	pool *pgxpool.Pool
	mu   sync.Mutex
	done chan struct{}
}

// NewPostgresClient creates a Client from the given DatabaseConfig.
//
// It parses the DSN, applies pool settings (MaxConns, MinConns), connects to
// PostgreSQL, and pings to verify connectivity. Returns a *port.DomainError
// (DATABASE_FAILED, retryable) on any failure.
func NewPostgresClient(ctx context.Context, cfg config.DatabaseConfig) (*Client, error) {
	poolCfg, err := pgxpool.ParseConfig(cfg.DSN)
	if err != nil {
		return nil, port.NewDatabaseError("parse DSN", err)
	}

	if cfg.MaxConns > 0 {
		poolCfg.MaxConns = int32(cfg.MaxConns)
	}
	if cfg.MinConns > 0 {
		poolCfg.MinConns = int32(cfg.MinConns)
	}

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, port.NewDatabaseError("create connection pool", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, pingTimeout(cfg))
	defer cancel()

	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, port.NewDatabaseError("ping database", err)
	}

	return &Client{
		pool: pool,
		done: make(chan struct{}),
	}, nil
}

// pingTimeout derives a reasonable timeout for the initial health-check ping.
// Uses QueryTimeout if set; otherwise falls back to 5 seconds.
func pingTimeout(cfg config.DatabaseConfig) time.Duration {
	if cfg.QueryTimeout > 0 {
		return cfg.QueryTimeout
	}
	return 5 * time.Second
}

// Pool returns the underlying *pgxpool.Pool.
// Use this to create a Transactor or Migrator, or to call InjectPool.
func (c *Client) Pool() *pgxpool.Pool {
	return c.pool
}

// Ping verifies that the database connection is alive.
// Suitable for /healthz and /readyz probes.
func (c *Client) Ping(ctx context.Context) error {
	if c.isClosed() {
		return port.NewDatabaseError("ping: client closed", nil)
	}
	if err := c.pool.Ping(ctx); err != nil {
		return port.NewDatabaseError("ping database", err)
	}
	return nil
}

// Close releases all pool connections. Safe to call multiple times.
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	select {
	case <-c.done:
		return nil // already closed
	default:
	}

	close(c.done)
	c.pool.Close()
	return nil
}

// isClosed returns true after Close has been called.
func (c *Client) isClosed() bool {
	select {
	case <-c.done:
		return true
	default:
		return false
	}
}

// String implements fmt.Stringer for logging/diagnostics.
func (c *Client) String() string {
	stat := c.pool.Stat()
	return fmt.Sprintf(
		"postgres.Client{conns: total=%d idle=%d acquired=%d}",
		stat.TotalConns(), stat.IdleConns(), stat.AcquiredConns(),
	)
}
