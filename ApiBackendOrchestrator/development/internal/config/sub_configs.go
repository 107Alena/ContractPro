package config

import "time"

// HTTPConfig holds HTTP server settings.
type HTTPConfig struct {
	Port            int           // ORCH_HTTP_PORT (default: 8080)
	MetricsPort     int           // ORCH_METRICS_PORT (default: 9090)
	RequestTimeout  time.Duration // ORCH_REQUEST_TIMEOUT (default: 30s)
	UploadTimeout   time.Duration // ORCH_UPLOAD_TIMEOUT (default: 60s)
	ShutdownTimeout time.Duration // ORCH_SHUTDOWN_TIMEOUT (default: 30s)
}

func loadHTTPConfig() HTTPConfig {
	return HTTPConfig{
		Port:            envInt("ORCH_HTTP_PORT", 8080),
		MetricsPort:     envInt("ORCH_METRICS_PORT", 9090),
		RequestTimeout:  envDuration("ORCH_REQUEST_TIMEOUT", 30*time.Second),
		UploadTimeout:   envDuration("ORCH_UPLOAD_TIMEOUT", 60*time.Second),
		ShutdownTimeout: envDuration("ORCH_SHUTDOWN_TIMEOUT", 30*time.Second),
	}
}

// BrokerConfig holds RabbitMQ connection and topic settings.
type BrokerConfig struct {
	Address  string // ORCH_BROKER_ADDRESS (required)
	TLS      bool   // ORCH_BROKER_TLS (default: false)
	Prefetch int    // ORCH_BROKER_PREFETCH (default: 10)

	// Outgoing commands (Orchestrator → DP / LIC).
	TopicProcessDocument   string // ORCH_BROKER_TOPIC_PROCESS_DOCUMENT
	TopicCompareVersions   string // ORCH_BROKER_TOPIC_COMPARE_VERSIONS
	TopicUserConfirmedType string // ORCH_BROKER_TOPIC_USER_CONFIRMED_TYPE

	// Incoming events from DP.
	TopicDPStatusChanged        string // ORCH_BROKER_TOPIC_DP_STATUS_CHANGED
	TopicDPProcessingCompleted  string // ORCH_BROKER_TOPIC_DP_PROCESSING_COMPLETED
	TopicDPProcessingFailed     string // ORCH_BROKER_TOPIC_DP_PROCESSING_FAILED
	TopicDPComparisonCompleted  string // ORCH_BROKER_TOPIC_DP_COMPARISON_COMPLETED
	TopicDPComparisonFailed     string // ORCH_BROKER_TOPIC_DP_COMPARISON_FAILED

	// Incoming events from LIC and RE.
	TopicLICStatusChanged           string // ORCH_BROKER_TOPIC_LIC_STATUS_CHANGED
	TopicLICClassificationUncertain string // ORCH_BROKER_TOPIC_LIC_CLASSIFICATION_UNCERTAIN
	TopicREStatusChanged            string // ORCH_BROKER_TOPIC_RE_STATUS_CHANGED

	// Incoming events from DM.
	TopicDMVersionArtifactsReady    string // ORCH_BROKER_TOPIC_DM_VERSION_ARTIFACTS_READY
	TopicDMVersionAnalysisReady     string // ORCH_BROKER_TOPIC_DM_VERSION_ANALYSIS_READY
	TopicDMVersionReportsReady      string // ORCH_BROKER_TOPIC_DM_VERSION_REPORTS_READY
	TopicDMVersionPartiallyAvail    string // ORCH_BROKER_TOPIC_DM_VERSION_PARTIALLY_AVAILABLE
	TopicDMVersionCreated           string // ORCH_BROKER_TOPIC_DM_VERSION_CREATED
}

func loadBrokerConfig() BrokerConfig {
	return BrokerConfig{
		Address:  envString("ORCH_BROKER_ADDRESS", ""),
		TLS:      envBool("ORCH_BROKER_TLS", false),
		Prefetch: envInt("ORCH_BROKER_PREFETCH", 10),

		TopicProcessDocument:   envString("ORCH_BROKER_TOPIC_PROCESS_DOCUMENT", "dp.commands.process-document"),
		TopicCompareVersions:   envString("ORCH_BROKER_TOPIC_COMPARE_VERSIONS", "dp.commands.compare-versions"),
		TopicUserConfirmedType: envString("ORCH_BROKER_TOPIC_USER_CONFIRMED_TYPE", "orch.commands.user-confirmed-type"),

		TopicDPStatusChanged:       envString("ORCH_BROKER_TOPIC_DP_STATUS_CHANGED", "dp.events.status-changed"),
		TopicDPProcessingCompleted: envString("ORCH_BROKER_TOPIC_DP_PROCESSING_COMPLETED", "dp.events.processing-completed"),
		TopicDPProcessingFailed:    envString("ORCH_BROKER_TOPIC_DP_PROCESSING_FAILED", "dp.events.processing-failed"),
		TopicDPComparisonCompleted: envString("ORCH_BROKER_TOPIC_DP_COMPARISON_COMPLETED", "dp.events.comparison-completed"),
		TopicDPComparisonFailed:    envString("ORCH_BROKER_TOPIC_DP_COMPARISON_FAILED", "dp.events.comparison-failed"),

		TopicLICStatusChanged:           envString("ORCH_BROKER_TOPIC_LIC_STATUS_CHANGED", "lic.events.status-changed"),
		TopicLICClassificationUncertain: envString("ORCH_BROKER_TOPIC_LIC_CLASSIFICATION_UNCERTAIN", "lic.events.classification-uncertain"),
		TopicREStatusChanged:            envString("ORCH_BROKER_TOPIC_RE_STATUS_CHANGED", "re.events.status-changed"),

		TopicDMVersionArtifactsReady: envString("ORCH_BROKER_TOPIC_DM_VERSION_ARTIFACTS_READY", "dm.events.version-artifacts-ready"),
		TopicDMVersionAnalysisReady:  envString("ORCH_BROKER_TOPIC_DM_VERSION_ANALYSIS_READY", "dm.events.version-analysis-ready"),
		TopicDMVersionReportsReady:   envString("ORCH_BROKER_TOPIC_DM_VERSION_REPORTS_READY", "dm.events.version-reports-ready"),
		TopicDMVersionPartiallyAvail: envString("ORCH_BROKER_TOPIC_DM_VERSION_PARTIALLY_AVAILABLE", "dm.events.version-partially-available"),
		TopicDMVersionCreated:        envString("ORCH_BROKER_TOPIC_DM_VERSION_CREATED", "dm.events.version-created"),
	}
}

// StorageConfig holds S3-compatible Object Storage settings.
type StorageConfig struct {
	Endpoint      string        // ORCH_STORAGE_ENDPOINT (required)
	Bucket        string        // ORCH_STORAGE_BUCKET (required)
	AccessKey     string        // ORCH_STORAGE_ACCESS_KEY (required)
	SecretKey     string        // ORCH_STORAGE_SECRET_KEY (required)
	Region        string        // ORCH_STORAGE_REGION (default: "ru-central1")
	UploadTimeout time.Duration // ORCH_STORAGE_UPLOAD_TIMEOUT (default: 30s)
}

func loadStorageConfig() StorageConfig {
	return StorageConfig{
		Endpoint:      envString("ORCH_STORAGE_ENDPOINT", ""),
		Bucket:        envString("ORCH_STORAGE_BUCKET", ""),
		AccessKey:     envString("ORCH_STORAGE_ACCESS_KEY", ""),
		SecretKey:     envString("ORCH_STORAGE_SECRET_KEY", ""),
		Region:        envString("ORCH_STORAGE_REGION", "ru-central1"),
		UploadTimeout: envDuration("ORCH_STORAGE_UPLOAD_TIMEOUT", 30*time.Second),
	}
}

// RedisConfig holds Redis connection settings.
type RedisConfig struct {
	Address  string        // ORCH_REDIS_ADDRESS (required)
	Password string        // ORCH_REDIS_PASSWORD (default: "")
	DB       int           // ORCH_REDIS_DB (default: 0)
	PoolSize int           // ORCH_REDIS_POOL_SIZE (default: 20)
	Timeout  time.Duration // ORCH_REDIS_TIMEOUT (default: 2s)
}

func loadRedisConfig() RedisConfig {
	return RedisConfig{
		Address:  envString("ORCH_REDIS_ADDRESS", ""),
		Password: envString("ORCH_REDIS_PASSWORD", ""),
		DB:       envInt("ORCH_REDIS_DB", 0),
		PoolSize: envInt("ORCH_REDIS_POOL_SIZE", 20),
		Timeout:  envDuration("ORCH_REDIS_TIMEOUT", 2*time.Second),
	}
}

// UploadConfig holds file upload settings.
type UploadConfig struct {
	MaxSize       int64    // ORCH_UPLOAD_MAX_SIZE in bytes (default: 20971520 = 20 MB)
	AllowedTypes  []string // ORCH_UPLOAD_ALLOWED_TYPES comma-separated (default: ["application/pdf"])
	MaxConcurrent int      // ORCH_UPLOAD_MAX_CONCURRENT per organization (default: 10)
}

func loadUploadConfig() UploadConfig {
	allowedTypes := envStringSlice("ORCH_UPLOAD_ALLOWED_TYPES")
	if allowedTypes == nil {
		allowedTypes = []string{"application/pdf"}
	}
	return UploadConfig{
		MaxSize:       envInt64("ORCH_UPLOAD_MAX_SIZE", 20971520),
		AllowedTypes:  allowedTypes,
		MaxConcurrent: envInt("ORCH_UPLOAD_MAX_CONCURRENT", 10),
	}
}

// DMClientConfig holds DM REST API client settings.
type DMClientConfig struct {
	BaseURL      string        // ORCH_DM_BASE_URL (required)
	TimeoutRead  time.Duration // ORCH_DM_TIMEOUT_READ (default: 5s)
	TimeoutWrite time.Duration // ORCH_DM_TIMEOUT_WRITE (default: 10s)
	RetryMax     int           // ORCH_DM_RETRY_MAX (default: 3) — total attempts, not retry count
	RetryBackoff time.Duration // ORCH_DM_RETRY_BACKOFF (default: 200ms)
}

func loadDMClientConfig() DMClientConfig {
	return DMClientConfig{
		BaseURL:      envString("ORCH_DM_BASE_URL", ""),
		TimeoutRead:  envDuration("ORCH_DM_TIMEOUT_READ", 5*time.Second),
		TimeoutWrite: envDuration("ORCH_DM_TIMEOUT_WRITE", 10*time.Second),
		RetryMax:     envInt("ORCH_DM_RETRY_MAX", 3),
		RetryBackoff: envDuration("ORCH_DM_RETRY_BACKOFF", 200*time.Millisecond),
	}
}

// OPMClientConfig holds OPM REST API client settings.
// OPM is optional — empty BaseURL disables the proxy.
type OPMClientConfig struct {
	BaseURL string        // ORCH_OPM_BASE_URL (default: "" — OPM disabled)
	Timeout time.Duration // ORCH_OPM_TIMEOUT (default: 5s)
}

func loadOPMClientConfig() OPMClientConfig {
	return OPMClientConfig{
		BaseURL: envString("ORCH_OPM_BASE_URL", ""),
		Timeout: envDuration("ORCH_OPM_TIMEOUT", 5*time.Second),
	}
}

// UOMClientConfig holds UOM REST API client settings.
// UOM is optional — empty BaseURL disables the auth proxy.
type UOMClientConfig struct {
	BaseURL string        // ORCH_UOM_BASE_URL (default: "" — auth proxy disabled)
	Timeout time.Duration // ORCH_UOM_TIMEOUT (default: 5s)
}

func loadUOMClientConfig() UOMClientConfig {
	return UOMClientConfig{
		BaseURL: envString("ORCH_UOM_BASE_URL", ""),
		Timeout: envDuration("ORCH_UOM_TIMEOUT", 5*time.Second),
	}
}

// JWTConfig holds JWT authentication settings.
type JWTConfig struct {
	PublicKeyPath string // ORCH_JWT_PUBLIC_KEY_PATH (required)
}

func loadJWTConfig() JWTConfig {
	return JWTConfig{
		PublicKeyPath: envString("ORCH_JWT_PUBLIC_KEY_PATH", ""),
	}
}

// SSEConfig holds Server-Sent Events settings.
type SSEConfig struct {
	HeartbeatInterval time.Duration // ORCH_SSE_HEARTBEAT_INTERVAL (default: 15s)
	MaxConnectionAge  time.Duration // ORCH_SSE_MAX_CONNECTION_AGE (default: 24h)
}

func loadSSEConfig() SSEConfig {
	return SSEConfig{
		HeartbeatInterval: envDuration("ORCH_SSE_HEARTBEAT_INTERVAL", 15*time.Second),
		MaxConnectionAge:  envDuration("ORCH_SSE_MAX_CONNECTION_AGE", 24*time.Hour),
	}
}

// RateLimitConfig holds per-organization rate limiting settings.
type RateLimitConfig struct {
	Enabled  bool // ORCH_RATELIMIT_ENABLED (default: true)
	ReadRPS  int  // ORCH_RATELIMIT_READ_RPS (default: 200)
	WriteRPS int  // ORCH_RATELIMIT_WRITE_RPS (default: 50)
}

func loadRateLimitConfig() RateLimitConfig {
	return RateLimitConfig{
		Enabled:  envBool("ORCH_RATELIMIT_ENABLED", true),
		ReadRPS:  envInt("ORCH_RATELIMIT_READ_RPS", 200),
		WriteRPS: envInt("ORCH_RATELIMIT_WRITE_RPS", 50),
	}
}

// CircuitBreakerConfig holds circuit breaker settings for downstream calls.
type CircuitBreakerConfig struct {
	FailureThreshold int           // ORCH_CB_FAILURE_THRESHOLD (default: 5)
	Timeout          time.Duration // ORCH_CB_TIMEOUT (default: 30s)
	MaxRequests      int           // ORCH_CB_MAX_REQUESTS (default: 3)
}

func loadCircuitBreakerConfig() CircuitBreakerConfig {
	return CircuitBreakerConfig{
		FailureThreshold: envInt("ORCH_CB_FAILURE_THRESHOLD", 5),
		Timeout:          envDuration("ORCH_CB_TIMEOUT", 30*time.Second),
		MaxRequests:      envInt("ORCH_CB_MAX_REQUESTS", 3),
	}
}

// CORSConfig holds Cross-Origin Resource Sharing settings.
type CORSConfig struct {
	AllowedOrigins []string // ORCH_CORS_ALLOWED_ORIGINS comma-separated (default: nil — same-origin only)
	MaxAge         int      // ORCH_CORS_MAX_AGE in seconds (default: 3600)
}

func loadCORSConfig() CORSConfig {
	return CORSConfig{
		AllowedOrigins: envStringSlice("ORCH_CORS_ALLOWED_ORIGINS"),
		MaxAge:         envInt("ORCH_CORS_MAX_AGE", 3600),
	}
}

// TypeConfirmationConfig holds settings for the user type confirmation flow
// (FR-2.1.3). When LIC classification confidence is low, the user is asked
// to confirm the contract type. If no confirmation arrives within the timeout,
// the version is moved to FAILED with error_code USER_CONFIRMATION_TIMEOUT.
type TypeConfirmationConfig struct {
	ConfirmationTimeout   time.Duration // ORCH_USER_CONFIRMATION_TIMEOUT (default: 24h)
	IdempotencyTTL        time.Duration // ORCH_USER_CONFIRMATION_IDEMPOTENCY_TTL (default: 60s)
	WatchdogScanInterval  time.Duration // ORCH_WATCHDOG_SCAN_INTERVAL (default: 1m)
	ContractTypeWhitelist []string      // ORCH_CONTRACT_TYPE_WHITELIST (default: static v1 list)
}

// defaultContractTypeWhitelist is the v1 static whitelist of contract types.
var defaultContractTypeWhitelist = []string{"услуги", "поставка", "подряд", "аренда", "NDA"}

func loadTypeConfirmationConfig() TypeConfirmationConfig {
	whitelist := envStringSlice("ORCH_CONTRACT_TYPE_WHITELIST")
	if len(whitelist) == 0 {
		whitelist = defaultContractTypeWhitelist
	}
	return TypeConfirmationConfig{
		ConfirmationTimeout:   envDuration("ORCH_USER_CONFIRMATION_TIMEOUT", 24*time.Hour),
		IdempotencyTTL:        envDuration("ORCH_USER_CONFIRMATION_IDEMPOTENCY_TTL", 60*time.Second),
		WatchdogScanInterval:  envDuration("ORCH_WATCHDOG_SCAN_INTERVAL", 1*time.Minute),
		ContractTypeWhitelist: whitelist,
	}
}

// PermissionsConfig holds settings for the Permissions Resolver (UR-10).
// Computes user permissions (e.g. export_enabled) from role, OPM policies,
// and environment fallbacks for the GET /users/me endpoint.
type PermissionsConfig struct {
	CacheTTL                      time.Duration // ORCH_PERMISSIONS_CACHE_TTL (default: 5m)
	OPMFallbackBusinessUserExport bool          // ORCH_OPM_FALLBACK_BUSINESS_USER_EXPORT (default: false)
	OPMTimeout                    time.Duration // ORCH_OPM_PERMISSIONS_TIMEOUT (default: 2s, <= 10s)
}

func loadPermissionsConfig() PermissionsConfig {
	return PermissionsConfig{
		CacheTTL:                      envDuration("ORCH_PERMISSIONS_CACHE_TTL", 5*time.Minute),
		OPMFallbackBusinessUserExport: envBool("ORCH_OPM_FALLBACK_BUSINESS_USER_EXPORT", false),
		OPMTimeout:                    envDuration("ORCH_OPM_PERMISSIONS_TIMEOUT", 2*time.Second),
	}
}

// ObservabilityConfig holds logging, metrics, and tracing settings.
type ObservabilityConfig struct {
	LogLevel        string // ORCH_LOG_LEVEL (default: "info")
	TracingEnabled  bool   // ORCH_TRACING_ENABLED (default: false)
	TracingEndpoint string // ORCH_TRACING_ENDPOINT (default: "")
	TracingInsecure bool   // ORCH_TRACING_INSECURE (default: false)
}

func loadObservabilityConfig() ObservabilityConfig {
	return ObservabilityConfig{
		LogLevel:        envString("ORCH_LOG_LEVEL", "info"),
		TracingEnabled:  envBool("ORCH_TRACING_ENABLED", false),
		TracingEndpoint: envString("ORCH_TRACING_ENDPOINT", ""),
		TracingInsecure: envBool("ORCH_TRACING_INSECURE", false),
	}
}
