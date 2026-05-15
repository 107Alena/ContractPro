package config

import (
	"fmt"
	"net/url"
	"strings"
	"time"
)

// RedisConfig holds Redis connection settings.
type RedisConfig struct {
	URL         string        // LIC_REDIS_URL (required) — redis:// or rediss://
	DB          int           // LIC_REDIS_DB (0..15)
	Password    string        // LIC_REDIS_PASSWORD
	TLS         bool          // LIC_REDIS_TLS — forced true in staging/prod
	PoolSize    int           // LIC_REDIS_POOL_SIZE
	DialTimeout time.Duration // LIC_REDIS_DIAL_TIMEOUT
}

func loadRedisConfig() RedisConfig {
	return RedisConfig{
		URL:         envString("LIC_REDIS_URL", ""),
		DB:          envInt("LIC_REDIS_DB", 0),
		Password:    envString("LIC_REDIS_PASSWORD", ""),
		TLS:         envBool("LIC_REDIS_TLS", false),
		PoolSize:    envInt("LIC_REDIS_POOL_SIZE", 10),
		DialTimeout: envDuration("LIC_REDIS_DIAL_TIMEOUT", 2*time.Second),
	}
}

func (r RedisConfig) validate() error {
	if r.URL == "" {
		return missingVarErr("LIC_REDIS_URL")
	}
	u, err := url.Parse(r.URL)
	if err != nil {
		return fmt.Errorf("config: LIC_REDIS_URL is not a valid URL: %w", err)
	}
	if scheme := strings.ToLower(u.Scheme); scheme != "redis" && scheme != "rediss" {
		return fmt.Errorf("config: LIC_REDIS_URL must use redis:// or rediss://, got %q", u.Scheme)
	}
	if r.DB < 0 || r.DB > 15 {
		return fmt.Errorf("config: LIC_REDIS_DB must be in [0,15], got %d", r.DB)
	}
	if r.PoolSize < 1 {
		return fmt.Errorf("config: LIC_REDIS_POOL_SIZE must be >= 1, got %d", r.PoolSize)
	}
	if r.DialTimeout <= 0 {
		return fmt.Errorf("config: LIC_REDIS_DIAL_TIMEOUT must be > 0, got %s", r.DialTimeout)
	}
	return nil
}

// UsesTLS returns true when either LIC_REDIS_TLS=true or the URL scheme is
// rediss://. It is the single source of truth for the Redis TLS decision: the
// production TLS-everywhere enforcement (enforceTLS, configuration.md §3 rule
// 10) and the kvstore adapter (internal/infra/kvstore, LIC-TASK-007) both
// consult it, so the rule cannot drift between the two.
func (r RedisConfig) UsesTLS() bool {
	if r.TLS {
		return true
	}
	u, err := url.Parse(r.URL)
	if err != nil {
		return false
	}
	return strings.EqualFold(u.Scheme, "rediss")
}
