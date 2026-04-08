package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

// Config is the root configuration for the API/Backend Orchestrator.
// Loaded from environment variables with the ORCH_ prefix at startup.
type Config struct {
	HTTP           HTTPConfig
	Broker         BrokerConfig
	Storage        StorageConfig
	Redis          RedisConfig
	Upload         UploadConfig
	DMClient       DMClientConfig
	OPMClient      OPMClientConfig
	UOMClient      UOMClientConfig
	JWT            JWTConfig
	SSE            SSEConfig
	RateLimit      RateLimitConfig
	CircuitBreaker CircuitBreakerConfig
	CORS           CORSConfig
	Observability  ObservabilityConfig
}

// Load reads configuration from environment variables, applies defaults,
// and validates required fields. Returns an aggregated error listing
// all problems found.
//
// If a .env file exists in the working directory, it is loaded first.
// Already set environment variables take precedence over .env values.
func Load() (*Config, error) {
	_ = godotenv.Load()

	cfg := &Config{
		HTTP:           loadHTTPConfig(),
		Broker:         loadBrokerConfig(),
		Storage:        loadStorageConfig(),
		Redis:          loadRedisConfig(),
		Upload:         loadUploadConfig(),
		DMClient:       loadDMClientConfig(),
		OPMClient:      loadOPMClientConfig(),
		UOMClient:      loadUOMClientConfig(),
		JWT:            loadJWTConfig(),
		SSE:            loadSSEConfig(),
		RateLimit:      loadRateLimitConfig(),
		CircuitBreaker: loadCircuitBreakerConfig(),
		CORS:           loadCORSConfig(),
		Observability:  loadObservabilityConfig(),
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// Validate checks that all required configuration fields are present and
// constraints are satisfied. Returns an error listing all problems at once.
func (c *Config) Validate() error {
	var problems []string

	// Required fields.
	if c.DMClient.BaseURL == "" {
		problems = append(problems, "ORCH_DM_BASE_URL is required")
	}
	if c.Broker.Address == "" {
		problems = append(problems, "ORCH_BROKER_ADDRESS is required")
	}
	if c.Storage.Endpoint == "" {
		problems = append(problems, "ORCH_STORAGE_ENDPOINT is required")
	}
	if c.Storage.Bucket == "" {
		problems = append(problems, "ORCH_STORAGE_BUCKET is required")
	}
	if c.Storage.AccessKey == "" {
		problems = append(problems, "ORCH_STORAGE_ACCESS_KEY is required")
	}
	if c.Storage.SecretKey == "" {
		problems = append(problems, "ORCH_STORAGE_SECRET_KEY is required")
	}
	if c.Redis.Address == "" {
		problems = append(problems, "ORCH_REDIS_ADDRESS is required")
	}
	if c.JWT.PublicKeyPath == "" {
		problems = append(problems, "ORCH_JWT_PUBLIC_KEY_PATH is required")
	} else {
		if _, err := os.Stat(c.JWT.PublicKeyPath); err != nil {
			problems = append(problems, fmt.Sprintf("ORCH_JWT_PUBLIC_KEY_PATH: file not accessible: %v", err))
		}
	}

	// Port collision.
	if c.HTTP.Port == c.HTTP.MetricsPort {
		problems = append(problems, "ORCH_HTTP_PORT and ORCH_METRICS_PORT must differ")
	}

	// Broker prefetch.
	if c.Broker.Prefetch < 1 {
		problems = append(problems, "ORCH_BROKER_PREFETCH must be >= 1")
	}

	// Positive durations.
	if c.HTTP.RequestTimeout <= 0 {
		problems = append(problems, "ORCH_REQUEST_TIMEOUT must be > 0")
	}
	if c.HTTP.UploadTimeout <= 0 {
		problems = append(problems, "ORCH_UPLOAD_TIMEOUT must be > 0")
	}
	if c.HTTP.ShutdownTimeout <= 0 {
		problems = append(problems, "ORCH_SHUTDOWN_TIMEOUT must be > 0")
	}
	if c.Storage.UploadTimeout <= 0 {
		problems = append(problems, "ORCH_STORAGE_UPLOAD_TIMEOUT must be > 0")
	}
	if c.Redis.Timeout <= 0 {
		problems = append(problems, "ORCH_REDIS_TIMEOUT must be > 0")
	}
	if c.DMClient.TimeoutRead <= 0 {
		problems = append(problems, "ORCH_DM_TIMEOUT_READ must be > 0")
	}
	if c.DMClient.TimeoutWrite <= 0 {
		problems = append(problems, "ORCH_DM_TIMEOUT_WRITE must be > 0")
	}
	if c.DMClient.RetryBackoff <= 0 {
		problems = append(problems, "ORCH_DM_RETRY_BACKOFF must be > 0")
	}
	if c.SSE.HeartbeatInterval <= 0 {
		problems = append(problems, "ORCH_SSE_HEARTBEAT_INTERVAL must be > 0")
	}
	if c.SSE.MaxConnectionAge <= 0 {
		problems = append(problems, "ORCH_SSE_MAX_CONNECTION_AGE must be > 0")
	}
	if c.CircuitBreaker.Timeout <= 0 {
		problems = append(problems, "ORCH_CB_TIMEOUT must be > 0")
	}

	// Upload max size.
	if c.Upload.MaxSize <= 0 {
		problems = append(problems, "ORCH_UPLOAD_MAX_SIZE must be > 0")
	}

	// Circuit breaker.
	if c.CircuitBreaker.FailureThreshold < 1 {
		problems = append(problems, "ORCH_CB_FAILURE_THRESHOLD must be >= 1")
	}
	if c.CircuitBreaker.MaxRequests < 1 {
		problems = append(problems, "ORCH_CB_MAX_REQUESTS must be >= 1")
	}

	// Redis pool size.
	if c.Redis.PoolSize < 1 {
		problems = append(problems, "ORCH_REDIS_POOL_SIZE must be >= 1")
	}

	// Log level.
	switch c.Observability.LogLevel {
	case "debug", "info", "warn", "error":
		// valid
	default:
		problems = append(problems, "ORCH_LOG_LEVEL must be one of: debug, info, warn, error")
	}

	// Rate limiting conditional validation.
	if c.RateLimit.Enabled {
		if c.RateLimit.ReadRPS <= 0 {
			problems = append(problems, "ORCH_RATELIMIT_READ_RPS must be > 0 when rate limiting is enabled")
		}
		if c.RateLimit.WriteRPS <= 0 {
			problems = append(problems, "ORCH_RATELIMIT_WRITE_RPS must be > 0 when rate limiting is enabled")
		}
	}

	if len(problems) > 0 {
		return fmt.Errorf("config: invalid configuration:\n  - %s", strings.Join(problems, "\n  - "))
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

// envStringSlice splits a comma-separated env var into a trimmed slice.
// Returns nil if the env var is empty or unset.
func envStringSlice(key string) []string {
	v := os.Getenv(key)
	if v == "" {
		return nil
	}
	parts := strings.Split(v, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		trimmed := strings.TrimSpace(p)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}
