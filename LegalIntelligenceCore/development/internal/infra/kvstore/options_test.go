package kvstore

import (
	"crypto/tls"
	"errors"
	"testing"
	"time"

	"contractpro/legal-intelligence-core/internal/config"
)

func baseCfg() config.RedisConfig {
	return config.RedisConfig{
		URL:         "redis://localhost:6379",
		DB:          3,
		Password:    "from-cfg",
		TLS:         false,
		PoolSize:    7,
		DialTimeout: 4 * time.Second,
	}
}

func TestBuildOptions_PlaintextOverrides(t *testing.T) {
	opt, err := buildOptions(baseCfg())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if opt.Addr != "localhost:6379" {
		t.Errorf("Addr = %q", opt.Addr)
	}
	if opt.DB != 3 {
		t.Errorf("DB = %d, want 3 (LIC_REDIS_DB knob wins)", opt.DB)
	}
	if opt.Password != "from-cfg" {
		t.Errorf("Password = %q, want cfg override", opt.Password)
	}
	if opt.PoolSize != 7 {
		t.Errorf("PoolSize = %d, want 7", opt.PoolSize)
	}
	if opt.DialTimeout != 4*time.Second || opt.ReadTimeout != 4*time.Second || opt.WriteTimeout != 4*time.Second {
		t.Errorf("timeouts = dial:%v read:%v write:%v, want all 4s",
			opt.DialTimeout, opt.ReadTimeout, opt.WriteTimeout)
	}
	if opt.TLSConfig != nil {
		t.Error("redis:// with TLS=false must not configure TLS")
	}
}

func TestBuildOptions_InlinePasswordPreservedWhenCfgEmpty(t *testing.T) {
	cfg := baseCfg()
	cfg.URL = "redis://:inline-pw@localhost:6379"
	cfg.Password = ""

	opt, err := buildOptions(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if opt.Password != "inline-pw" {
		t.Errorf("Password = %q, want inline-pw preserved when LIC_REDIS_PASSWORD empty", opt.Password)
	}
}

func TestBuildOptions_CfgPasswordOverridesInline(t *testing.T) {
	cfg := baseCfg()
	cfg.URL = "redis://:inline-pw@localhost:6379"
	cfg.Password = "explicit"

	opt, err := buildOptions(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if opt.Password != "explicit" {
		t.Errorf("Password = %q, want explicit cfg override", opt.Password)
	}
}

func TestBuildOptions_RedissHardenedTLS(t *testing.T) {
	cfg := baseCfg()
	cfg.URL = "rediss://redis.example.com:6380"

	opt, err := buildOptions(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if opt.TLSConfig == nil {
		t.Fatal("rediss:// must configure TLS")
	}
	if opt.TLSConfig.MinVersion < tls.VersionTLS12 {
		t.Errorf("MinVersion = %x, want >= TLS1.2", opt.TLSConfig.MinVersion)
	}
	if opt.TLSConfig.ServerName != "redis.example.com" {
		t.Errorf("ServerName = %q, want host", opt.TLSConfig.ServerName)
	}
	if opt.TLSConfig.InsecureSkipVerify {
		t.Error("InsecureSkipVerify must never be set")
	}
}

func TestBuildOptions_ForcedTLSOverPlaintextScheme(t *testing.T) {
	cfg := baseCfg()
	cfg.URL = "redis://redis.example.com:6379" // plaintext scheme
	cfg.TLS = true                              // LIC_REDIS_TLS=true forces TLS

	opt, err := buildOptions(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if opt.TLSConfig == nil {
		t.Fatal("LIC_REDIS_TLS=true must force TLS even on redis:// scheme")
	}
	if opt.TLSConfig.MinVersion < tls.VersionTLS12 {
		t.Errorf("MinVersion = %x, want >= TLS1.2", opt.TLSConfig.MinVersion)
	}
	if opt.TLSConfig.ServerName != "redis.example.com" {
		t.Errorf("ServerName = %q, want host", opt.TLSConfig.ServerName)
	}
}

func TestBuildOptions_InvalidURL(t *testing.T) {
	cfg := baseCfg()
	cfg.URL = "http://not-redis"

	_, err := buildOptions(cfg)
	if err == nil {
		t.Fatal("expected error for non-redis scheme")
	}
	var re *RedisError
	if !errors.As(err, &re) {
		t.Fatalf("want *RedisError, got %T: %v", err, err)
	}
	if re.Op != "ParseURL" || re.Retryable {
		t.Errorf("want non-retryable RedisError{Op:ParseURL}, got %+v", re)
	}
}
