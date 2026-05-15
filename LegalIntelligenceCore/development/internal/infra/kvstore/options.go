package kvstore

import (
	"crypto/tls"
	"net"

	"github.com/redis/go-redis/v9"

	"contractpro/legal-intelligence-core/internal/config"
)

// buildOptions converts a config.RedisConfig into *redis.Options.
//
// The URL is the base: redis.ParseURL handles scheme, host, an optional
// inline user:password and an optional /db path, and for rediss:// pre-builds
// a hardened *tls.Config{ServerName, MinVersion: TLS1.2}. Explicit LIC_REDIS_*
// knobs then override — DB, PoolSize and DialTimeout are always applied (they
// have config defaults and config.validate() has already range-checked them);
// Password overrides only when set, so an inline URL password survives when
// LIC_REDIS_PASSWORD is empty (configuration.md §2.3: "если не в URL").
//
// ReadTimeout / WriteTimeout reuse DialTimeout: configuration.md §2.3 freezes
// the Redis var set to URL / DB / PASSWORD / TLS / POOL_SIZE / DIAL_TIMEOUT —
// there is no LIC_REDIS_READ/WRITE_TIMEOUT and inventing one is out of scope
// (code-architect must-fix 2). Reusing the dial timeout for the (sub-second,
// local-cluster) command timeouts mirrors the DP kvstore precedent.
// PoolTimeout is left at the go-redis default (ReadTimeout + 1s).
func buildOptions(cfg config.RedisConfig) (*redis.Options, error) {
	opt, err := redis.ParseURL(cfg.URL)
	if err != nil {
		return nil, &RedisError{
			Op:        "ParseURL",
			Retryable: false,
			Cause:     redactURLCredentials(err, cfg.URL),
		}
	}

	opt.DB = cfg.DB
	if cfg.Password != "" {
		opt.Password = cfg.Password
	}
	opt.PoolSize = cfg.PoolSize
	opt.DialTimeout = cfg.DialTimeout
	opt.ReadTimeout = cfg.DialTimeout
	opt.WriteTimeout = cfg.DialTimeout

	// The TLS decision is the config layer's SSOT (config.RedisConfig.UsesTLS,
	// also driven by the production TLS-everywhere enforcement in
	// config/tls.go); kvstore only honours it, never re-enforces
	// (code-architect Q4). ParseURL already builds a hardened config for
	// rediss://; when TLS is forced via LIC_REDIS_TLS=true over a redis://
	// URL, ParseURL leaves TLSConfig nil and we must build it. Either way the
	// floor is TLS 1.2 with ServerName pinned to the dialled host. Plaintext
	// is never silently used in production because config.enforceTLS fails
	// startup first.
	if cfg.UsesTLS() {
		host, _, splitErr := net.SplitHostPort(opt.Addr)
		if splitErr != nil {
			host = opt.Addr
		}
		if opt.TLSConfig == nil {
			opt.TLSConfig = &tls.Config{
				ServerName: host,
				MinVersion: tls.VersionTLS12,
			}
		} else {
			if opt.TLSConfig.ServerName == "" {
				opt.TLSConfig.ServerName = host
			}
			if opt.TLSConfig.MinVersion < tls.VersionTLS12 {
				opt.TLSConfig.MinVersion = tls.VersionTLS12
			}
		}
	}

	return opt, nil
}
