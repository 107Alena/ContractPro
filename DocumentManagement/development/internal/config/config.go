package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

// Config is the root configuration for the Document Management service.
// Loaded from environment variables with the DM_ prefix at startup.
type Config struct {
	Database      DatabaseConfig
	Broker        BrokerConfig
	Storage       StorageConfig
	KVStore       KVStoreConfig
	HTTP          HTTPConfig
	Consumer      ConsumerConfig
	Idempotency   IdempotencyConfig
	Outbox        OutboxConfig
	Retention     RetentionConfig
	Retry         RetryConfig
	DLQ           DLQConfig
	Observability ObservabilityConfig
	Timeout       TimeoutConfig
}

// Load reads configuration from environment variables, applies defaults,
// and validates required fields. Returns an aggregated error listing
// all missing required fields.
//
// If a .env file exists in the working directory, it is loaded first.
// Already set environment variables take precedence over .env values.
func Load() (*Config, error) {
	// Load .env file if it exists; ignore error if file is absent.
	_ = godotenv.Load()

	cfg := &Config{
		Database:      loadDatabaseConfig(),
		Broker:        loadBrokerConfig(),
		Storage:       loadStorageConfig(),
		KVStore:       loadKVStoreConfig(),
		HTTP:          loadHTTPConfig(),
		Consumer:      loadConsumerConfig(),
		Idempotency:   loadIdempotencyConfig(),
		Outbox:        loadOutboxConfig(),
		Retention:     loadRetentionConfig(),
		Retry:         loadRetryConfig(),
		DLQ:           loadDLQConfig(),
		Observability: loadObservabilityConfig(),
		Timeout:       loadTimeoutConfig(),
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// Validate checks that all required configuration fields are present and
// constraint invariants hold. Returns an error listing all problems at once.
func (c *Config) Validate() error {
	var missing []string
	var invalid []string

	if c.Database.DSN == "" {
		missing = append(missing, "DM_DB_DSN")
	}
	if c.Broker.Address == "" {
		missing = append(missing, "DM_BROKER_ADDRESS")
	}
	if c.Storage.Endpoint == "" {
		missing = append(missing, "DM_STORAGE_ENDPOINT")
	}
	if c.Storage.Bucket == "" {
		missing = append(missing, "DM_STORAGE_BUCKET")
	}
	if c.Storage.AccessKey == "" {
		missing = append(missing, "DM_STORAGE_ACCESS_KEY")
	}
	if c.Storage.SecretKey == "" {
		missing = append(missing, "DM_STORAGE_SECRET_KEY")
	}
	if c.KVStore.Address == "" {
		missing = append(missing, "DM_KVSTORE_ADDRESS")
	}

	if c.HTTP.Port == c.Observability.MetricsPort {
		invalid = append(invalid, "DM_HTTP_PORT and DM_METRICS_PORT must differ")
	}

	var parts []string
	if len(missing) > 0 {
		parts = append(parts, "missing required: "+strings.Join(missing, ", "))
	}
	if len(invalid) > 0 {
		parts = append(parts, "invalid: "+strings.Join(invalid, "; "))
	}
	if len(parts) > 0 {
		return fmt.Errorf("config: %s", strings.Join(parts, "; "))
	}
	return nil
}

// --- environment variable helpers ---

func envString(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}

func envInt(key string, defaultVal int) int {
	v := os.Getenv(key)
	if v == "" {
		return defaultVal
	}
	parsed, err := strconv.Atoi(v)
	if err != nil {
		return defaultVal
	}
	return parsed
}

func envDuration(key string, defaultVal time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return defaultVal
	}
	parsed, err := time.ParseDuration(v)
	if err != nil {
		return defaultVal
	}
	return parsed
}

func envBool(key string, defaultVal bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return defaultVal
	}
	parsed, err := strconv.ParseBool(v)
	if err != nil {
		return defaultVal
	}
	return parsed
}
