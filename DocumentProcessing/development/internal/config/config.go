package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

// Config is the root configuration for the Document Processing service.
// Loaded from environment variables with the DP_ prefix at startup.
type Config struct {
	Broker        BrokerConfig
	Storage       StorageConfig
	OCR           OCRConfig
	Limits        LimitsConfig
	Concurrency   ConcurrencyConfig
	Idempotency   IdempotencyConfig
	KVStore       KVStoreConfig
	Observability ObservabilityConfig
	HTTP          HTTPConfig
	Retry         RetryConfig
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
		Broker:        loadBrokerConfig(),
		Storage:       loadStorageConfig(),
		OCR:           loadOCRConfig(),
		Limits:        loadLimitsConfig(),
		Concurrency:   loadConcurrencyConfig(),
		Idempotency:   loadIdempotencyConfig(),
		KVStore:       loadKVStoreConfig(),
		Observability: loadObservabilityConfig(),
		HTTP:          loadHTTPConfig(),
		Retry:         loadRetryConfig(),
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// Validate checks that all required configuration fields are present.
// Returns an error listing all missing fields at once.
func (c *Config) Validate() error {
	var missing []string

	if c.Broker.Address == "" {
		missing = append(missing, "DP_BROKER_ADDRESS")
	}
	if c.Storage.Endpoint == "" {
		missing = append(missing, "DP_STORAGE_ENDPOINT")
	}
	if c.Storage.Bucket == "" {
		missing = append(missing, "DP_STORAGE_BUCKET")
	}
	if c.Storage.AccessKey == "" {
		missing = append(missing, "DP_STORAGE_ACCESS_KEY")
	}
	if c.Storage.SecretKey == "" {
		missing = append(missing, "DP_STORAGE_SECRET_KEY")
	}
	if c.OCR.Endpoint == "" {
		missing = append(missing, "DP_OCR_ENDPOINT")
	}
	if c.OCR.APIKey == "" {
		missing = append(missing, "DP_OCR_API_KEY")
	}
	if c.OCR.FolderID == "" {
		missing = append(missing, "DP_OCR_FOLDER_ID")
	}
	if c.KVStore.Address == "" {
		missing = append(missing, "DP_KVSTORE_ADDRESS")
	}

	if len(missing) > 0 {
		return fmt.Errorf("config: missing required environment variables: %s", strings.Join(missing, ", "))
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

func envInt64(key string, defaultVal int64) int64 {
	v := os.Getenv(key)
	if v == "" {
		return defaultVal
	}
	parsed, err := strconv.ParseInt(v, 10, 64)
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
