package config

import "time"

// BrokerConfig holds message broker connection and topic settings.
type BrokerConfig struct {
	Address string // DP_BROKER_ADDRESS (required)

	// DP inbound command topics (consumed by Command Consumer)
	TopicProcessDocument string // DP_BROKER_TOPIC_PROCESS_DOCUMENT
	TopicCompareVersions string // DP_BROKER_TOPIC_COMPARE_VERSIONS

	// DP -> DM topics (published by DM Outbound Adapter)
	TopicArtifactsReady    string // DP_BROKER_TOPIC_ARTIFACTS_READY
	TopicSemanticTreeReq   string // DP_BROKER_TOPIC_SEMANTIC_TREE_REQUEST
	TopicDiffReady         string // DP_BROKER_TOPIC_DIFF_READY

	// DM -> DP response topics (consumed by DM Inbound Adapter)
	TopicDMArtifactsPersisted    string // DP_BROKER_TOPIC_DM_ARTIFACTS_PERSISTED
	TopicDMArtifactsPersistFailed string // DP_BROKER_TOPIC_DM_ARTIFACTS_PERSIST_FAILED
	TopicDMSemanticTreeProvided  string // DP_BROKER_TOPIC_DM_SEMANTIC_TREE_PROVIDED
	TopicDMDiffPersisted         string // DP_BROKER_TOPIC_DM_DIFF_PERSISTED
	TopicDMDiffPersistFailed     string // DP_BROKER_TOPIC_DM_DIFF_PERSIST_FAILED

	// DP -> external event topics (published by Event Publisher)
	TopicStatusChanged        string // DP_BROKER_TOPIC_STATUS_CHANGED
	TopicProcessingCompleted  string // DP_BROKER_TOPIC_PROCESSING_COMPLETED
	TopicProcessingFailed     string // DP_BROKER_TOPIC_PROCESSING_FAILED
	TopicComparisonCompleted  string // DP_BROKER_TOPIC_COMPARISON_COMPLETED
	TopicComparisonFailed     string // DP_BROKER_TOPIC_COMPARISON_FAILED
}

func loadBrokerConfig() BrokerConfig {
	return BrokerConfig{
		Address: envString("DP_BROKER_ADDRESS", ""),

		TopicProcessDocument: envString("DP_BROKER_TOPIC_PROCESS_DOCUMENT", "dp.commands.process-document"),
		TopicCompareVersions: envString("DP_BROKER_TOPIC_COMPARE_VERSIONS", "dp.commands.compare-versions"),

		TopicArtifactsReady:  envString("DP_BROKER_TOPIC_ARTIFACTS_READY", "dp.artifacts.processing-ready"),
		TopicSemanticTreeReq: envString("DP_BROKER_TOPIC_SEMANTIC_TREE_REQUEST", "dp.requests.semantic-tree"),
		TopicDiffReady:       envString("DP_BROKER_TOPIC_DIFF_READY", "dp.artifacts.diff-ready"),

		TopicDMArtifactsPersisted:    envString("DP_BROKER_TOPIC_DM_ARTIFACTS_PERSISTED", "dm.responses.artifacts-persisted"),
		TopicDMArtifactsPersistFailed: envString("DP_BROKER_TOPIC_DM_ARTIFACTS_PERSIST_FAILED", "dm.responses.artifacts-persist-failed"),
		TopicDMSemanticTreeProvided:  envString("DP_BROKER_TOPIC_DM_SEMANTIC_TREE_PROVIDED", "dm.responses.semantic-tree-provided"),
		TopicDMDiffPersisted:         envString("DP_BROKER_TOPIC_DM_DIFF_PERSISTED", "dm.responses.diff-persisted"),
		TopicDMDiffPersistFailed:     envString("DP_BROKER_TOPIC_DM_DIFF_PERSIST_FAILED", "dm.responses.diff-persist-failed"),

		TopicStatusChanged:       envString("DP_BROKER_TOPIC_STATUS_CHANGED", "dp.events.status-changed"),
		TopicProcessingCompleted: envString("DP_BROKER_TOPIC_PROCESSING_COMPLETED", "dp.events.processing-completed"),
		TopicProcessingFailed:    envString("DP_BROKER_TOPIC_PROCESSING_FAILED", "dp.events.processing-failed"),
		TopicComparisonCompleted: envString("DP_BROKER_TOPIC_COMPARISON_COMPLETED", "dp.events.comparison-completed"),
		TopicComparisonFailed:    envString("DP_BROKER_TOPIC_COMPARISON_FAILED", "dp.events.comparison-failed"),
	}
}

// StorageConfig holds Yandex Object Storage connection settings.
type StorageConfig struct {
	Endpoint  string // DP_STORAGE_ENDPOINT (required)
	Bucket    string // DP_STORAGE_BUCKET (required)
	AccessKey string // DP_STORAGE_ACCESS_KEY (required)
	SecretKey string // DP_STORAGE_SECRET_KEY (required)
	Region    string // DP_STORAGE_REGION (default: "ru-central1")
}

func loadStorageConfig() StorageConfig {
	return StorageConfig{
		Endpoint:  envString("DP_STORAGE_ENDPOINT", ""),
		Bucket:    envString("DP_STORAGE_BUCKET", ""),
		AccessKey: envString("DP_STORAGE_ACCESS_KEY", ""),
		SecretKey: envString("DP_STORAGE_SECRET_KEY", ""),
		Region:    envString("DP_STORAGE_REGION", "ru-central1"),
	}
}

// OCRConfig holds Yandex Cloud Vision OCR integration settings.
type OCRConfig struct {
	Endpoint string // DP_OCR_ENDPOINT (required)
	APIKey   string // DP_OCR_API_KEY (required)
	FolderID string // DP_OCR_FOLDER_ID (required)
	RPSLimit int    // DP_OCR_RPS_LIMIT (default: 10)
}

func loadOCRConfig() OCRConfig {
	return OCRConfig{
		Endpoint: envString("DP_OCR_ENDPOINT", ""),
		APIKey:   envString("DP_OCR_API_KEY", ""),
		FolderID: envString("DP_OCR_FOLDER_ID", ""),
		RPSLimit: envInt("DP_OCR_RPS_LIMIT", 10),
	}
}

// LimitsConfig holds processing limits for documents.
type LimitsConfig struct {
	MaxFileSize int64         // DP_LIMITS_MAX_FILE_SIZE in bytes (default: 20971520 = 20 MB)
	MaxPages    int           // DP_LIMITS_MAX_PAGES (default: 100)
	JobTimeout  time.Duration // DP_LIMITS_JOB_TIMEOUT (default: 120s)
}

func loadLimitsConfig() LimitsConfig {
	return LimitsConfig{
		MaxFileSize: envInt64("DP_LIMITS_MAX_FILE_SIZE", 20971520),
		MaxPages:    envInt("DP_LIMITS_MAX_PAGES", 100),
		JobTimeout:  envDuration("DP_LIMITS_JOB_TIMEOUT", 120*time.Second),
	}
}

// ConcurrencyConfig holds concurrency limits per DP instance.
type ConcurrencyConfig struct {
	MaxConcurrentJobs int // DP_CONCURRENCY_MAX_JOBS (default: 5)
}

func loadConcurrencyConfig() ConcurrencyConfig {
	return ConcurrencyConfig{
		MaxConcurrentJobs: envInt("DP_CONCURRENCY_MAX_JOBS", 5),
	}
}

// IdempotencyConfig holds settings for the Idempotency Guard KV-store.
type IdempotencyConfig struct {
	TTL time.Duration // DP_IDEMPOTENCY_TTL (default: 24h)
}

func loadIdempotencyConfig() IdempotencyConfig {
	return IdempotencyConfig{
		TTL: envDuration("DP_IDEMPOTENCY_TTL", 24*time.Hour),
	}
}

// KVStoreConfig holds Redis connection settings for the KV-store.
type KVStoreConfig struct {
	Address  string        // DP_KVSTORE_ADDRESS (required)
	Password string        // DP_KVSTORE_PASSWORD (default: "")
	DB       int           // DP_KVSTORE_DB (default: 0)
	PoolSize int           // DP_KVSTORE_POOL_SIZE (default: 10)
	Timeout  time.Duration // DP_KVSTORE_TIMEOUT (default: 5s)
}

func loadKVStoreConfig() KVStoreConfig {
	return KVStoreConfig{
		Address:  envString("DP_KVSTORE_ADDRESS", ""),
		Password: envString("DP_KVSTORE_PASSWORD", ""),
		DB:       envInt("DP_KVSTORE_DB", 0),
		PoolSize: envInt("DP_KVSTORE_POOL_SIZE", 10),
		Timeout:  envDuration("DP_KVSTORE_TIMEOUT", 5*time.Second),
	}
}

// ObservabilityConfig holds logging, metrics, and tracing settings.
type ObservabilityConfig struct {
	LogLevel         string // DP_LOG_LEVEL (default: "info")
	MetricsPort      int    // DP_METRICS_PORT (default: 9090)
	TracingEnabled   bool   // DP_TRACING_ENABLED (default: false)
	TracingEndpoint  string // DP_TRACING_ENDPOINT (default: "")
	TracingInsecure  bool   // DP_TRACING_INSECURE (default: false)
}

func loadObservabilityConfig() ObservabilityConfig {
	return ObservabilityConfig{
		LogLevel:         envString("DP_LOG_LEVEL", "info"),
		MetricsPort:      envInt("DP_METRICS_PORT", 9090),
		TracingEnabled:   envBool("DP_TRACING_ENABLED", false),
		TracingEndpoint:  envString("DP_TRACING_ENDPOINT", ""),
		TracingInsecure:  envBool("DP_TRACING_INSECURE", false),
	}
}

// HTTPConfig holds HTTP server settings for health/readiness probes.
type HTTPConfig struct {
	Port int // DP_HTTP_PORT (default: 8080)
}

func loadHTTPConfig() HTTPConfig {
	return HTTPConfig{
		Port: envInt("DP_HTTP_PORT", 8080),
	}
}

// RetryConfig holds retry policy settings for retryable errors.
type RetryConfig struct {
	MaxAttempts int           // DP_RETRY_MAX_ATTEMPTS (default: 3)
	BackoffBase time.Duration // DP_RETRY_BACKOFF_BASE (default: 1s)
}

func loadRetryConfig() RetryConfig {
	return RetryConfig{
		MaxAttempts: envInt("DP_RETRY_MAX_ATTEMPTS", 3),
		BackoffBase: envDuration("DP_RETRY_BACKOFF_BASE", 1*time.Second),
	}
}
