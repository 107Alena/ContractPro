package config

import "time"

// DatabaseConfig holds PostgreSQL connection settings.
type DatabaseConfig struct {
	DSN          string        // DM_DB_DSN (required)
	MaxConns     int           // DM_DB_MAX_CONNS (default: 25)
	MinConns     int           // DM_DB_MIN_CONNS (default: 5)
	QueryTimeout time.Duration // DM_DB_QUERY_TIMEOUT (default: 10s)
}

func loadDatabaseConfig() DatabaseConfig {
	return DatabaseConfig{
		DSN:          envString("DM_DB_DSN", ""),
		MaxConns:     envInt("DM_DB_MAX_CONNS", 25),
		MinConns:     envInt("DM_DB_MIN_CONNS", 5),
		QueryTimeout: envDuration("DM_DB_QUERY_TIMEOUT", 10*time.Second),
	}
}

// BrokerConfig holds message broker connection and topic settings.
type BrokerConfig struct {
	Address string // DM_BROKER_ADDRESS (required)
	TLS     bool   // DM_BROKER_TLS (default: false)

	// Incoming topics — DM subscribes to these queues.
	TopicDPArtifactsProcessingReady string // DM_BROKER_TOPIC_DP_ARTIFACTS_PROCESSING_READY
	TopicDPRequestsSemanticTree     string // DM_BROKER_TOPIC_DP_REQUESTS_SEMANTIC_TREE
	TopicDPArtifactsDiffReady       string // DM_BROKER_TOPIC_DP_ARTIFACTS_DIFF_READY
	TopicLICArtifactsAnalysisReady  string // DM_BROKER_TOPIC_LIC_ARTIFACTS_ANALYSIS_READY
	TopicLICRequestsArtifacts       string // DM_BROKER_TOPIC_LIC_REQUESTS_ARTIFACTS
	TopicREArtifactsReportsReady    string // DM_BROKER_TOPIC_RE_ARTIFACTS_REPORTS_READY
	TopicRERequestsArtifacts        string // DM_BROKER_TOPIC_RE_REQUESTS_ARTIFACTS

	// Outgoing confirmation topics — DM publishes responses to senders.
	TopicDMResponsesArtifactsPersisted        string // DM_BROKER_TOPIC_DM_RESPONSES_ARTIFACTS_PERSISTED
	TopicDMResponsesArtifactsPersistFailed    string // DM_BROKER_TOPIC_DM_RESPONSES_ARTIFACTS_PERSIST_FAILED
	TopicDMResponsesSemanticTreeProvided      string // DM_BROKER_TOPIC_DM_RESPONSES_SEMANTIC_TREE_PROVIDED
	TopicDMResponsesArtifactsProvided         string // DM_BROKER_TOPIC_DM_RESPONSES_ARTIFACTS_PROVIDED
	TopicDMResponsesDiffPersisted             string // DM_BROKER_TOPIC_DM_RESPONSES_DIFF_PERSISTED
	TopicDMResponsesDiffPersistFailed         string // DM_BROKER_TOPIC_DM_RESPONSES_DIFF_PERSIST_FAILED
	TopicDMResponsesLICArtifactsPersisted     string // DM_BROKER_TOPIC_DM_RESPONSES_LIC_ARTIFACTS_PERSISTED
	TopicDMResponsesLICArtifactsPersistFailed string // DM_BROKER_TOPIC_DM_RESPONSES_LIC_ARTIFACTS_PERSIST_FAILED
	TopicDMResponsesREReportsPersisted        string // DM_BROKER_TOPIC_DM_RESPONSES_RE_REPORTS_PERSISTED
	TopicDMResponsesREReportsPersistFailed    string // DM_BROKER_TOPIC_DM_RESPONSES_RE_REPORTS_PERSIST_FAILED

	// Outgoing notification topics — DM publishes events for downstream domains.
	TopicDMEventsVersionArtifactsReady     string // DM_BROKER_TOPIC_DM_EVENTS_VERSION_ARTIFACTS_READY
	TopicDMEventsVersionAnalysisReady      string // DM_BROKER_TOPIC_DM_EVENTS_VERSION_ANALYSIS_READY
	TopicDMEventsVersionReportsReady       string // DM_BROKER_TOPIC_DM_EVENTS_VERSION_REPORTS_READY
	TopicDMEventsVersionCreated            string // DM_BROKER_TOPIC_DM_EVENTS_VERSION_CREATED
	TopicDMEventsVersionPartiallyAvailable string // DM_BROKER_TOPIC_DM_EVENTS_VERSION_PARTIALLY_AVAILABLE

	// DLQ topics — failed messages for post-mortem analysis.
	TopicDMDLQIngestionFailed string // DM_BROKER_TOPIC_DM_DLQ_INGESTION_FAILED
	TopicDMDLQQueryFailed     string // DM_BROKER_TOPIC_DM_DLQ_QUERY_FAILED
	TopicDMDLQInvalidMessage  string // DM_BROKER_TOPIC_DM_DLQ_INVALID_MESSAGE
}

func loadBrokerConfig() BrokerConfig {
	return BrokerConfig{
		Address: envString("DM_BROKER_ADDRESS", ""),
		TLS:     envBool("DM_BROKER_TLS", false),

		// Incoming topics
		TopicDPArtifactsProcessingReady: envString("DM_BROKER_TOPIC_DP_ARTIFACTS_PROCESSING_READY", "dp.artifacts.processing-ready"),
		TopicDPRequestsSemanticTree:     envString("DM_BROKER_TOPIC_DP_REQUESTS_SEMANTIC_TREE", "dp.requests.semantic-tree"),
		TopicDPArtifactsDiffReady:       envString("DM_BROKER_TOPIC_DP_ARTIFACTS_DIFF_READY", "dp.artifacts.diff-ready"),
		TopicLICArtifactsAnalysisReady:  envString("DM_BROKER_TOPIC_LIC_ARTIFACTS_ANALYSIS_READY", "lic.artifacts.analysis-ready"),
		TopicLICRequestsArtifacts:       envString("DM_BROKER_TOPIC_LIC_REQUESTS_ARTIFACTS", "lic.requests.artifacts"),
		TopicREArtifactsReportsReady:    envString("DM_BROKER_TOPIC_RE_ARTIFACTS_REPORTS_READY", "re.artifacts.reports-ready"),
		TopicRERequestsArtifacts:        envString("DM_BROKER_TOPIC_RE_REQUESTS_ARTIFACTS", "re.requests.artifacts"),

		// Outgoing confirmation topics
		TopicDMResponsesArtifactsPersisted:        envString("DM_BROKER_TOPIC_DM_RESPONSES_ARTIFACTS_PERSISTED", "dm.responses.artifacts-persisted"),
		TopicDMResponsesArtifactsPersistFailed:    envString("DM_BROKER_TOPIC_DM_RESPONSES_ARTIFACTS_PERSIST_FAILED", "dm.responses.artifacts-persist-failed"),
		TopicDMResponsesSemanticTreeProvided:      envString("DM_BROKER_TOPIC_DM_RESPONSES_SEMANTIC_TREE_PROVIDED", "dm.responses.semantic-tree-provided"),
		TopicDMResponsesArtifactsProvided:         envString("DM_BROKER_TOPIC_DM_RESPONSES_ARTIFACTS_PROVIDED", "dm.responses.artifacts-provided"),
		TopicDMResponsesDiffPersisted:             envString("DM_BROKER_TOPIC_DM_RESPONSES_DIFF_PERSISTED", "dm.responses.diff-persisted"),
		TopicDMResponsesDiffPersistFailed:         envString("DM_BROKER_TOPIC_DM_RESPONSES_DIFF_PERSIST_FAILED", "dm.responses.diff-persist-failed"),
		TopicDMResponsesLICArtifactsPersisted:     envString("DM_BROKER_TOPIC_DM_RESPONSES_LIC_ARTIFACTS_PERSISTED", "dm.responses.lic-artifacts-persisted"),
		TopicDMResponsesLICArtifactsPersistFailed: envString("DM_BROKER_TOPIC_DM_RESPONSES_LIC_ARTIFACTS_PERSIST_FAILED", "dm.responses.lic-artifacts-persist-failed"),
		TopicDMResponsesREReportsPersisted:        envString("DM_BROKER_TOPIC_DM_RESPONSES_RE_REPORTS_PERSISTED", "dm.responses.re-reports-persisted"),
		TopicDMResponsesREReportsPersistFailed:    envString("DM_BROKER_TOPIC_DM_RESPONSES_RE_REPORTS_PERSIST_FAILED", "dm.responses.re-reports-persist-failed"),

		// Outgoing notification topics
		TopicDMEventsVersionArtifactsReady:     envString("DM_BROKER_TOPIC_DM_EVENTS_VERSION_ARTIFACTS_READY", "dm.events.version-artifacts-ready"),
		TopicDMEventsVersionAnalysisReady:      envString("DM_BROKER_TOPIC_DM_EVENTS_VERSION_ANALYSIS_READY", "dm.events.version-analysis-ready"),
		TopicDMEventsVersionReportsReady:       envString("DM_BROKER_TOPIC_DM_EVENTS_VERSION_REPORTS_READY", "dm.events.version-reports-ready"),
		TopicDMEventsVersionCreated:            envString("DM_BROKER_TOPIC_DM_EVENTS_VERSION_CREATED", "dm.events.version-created"),
		TopicDMEventsVersionPartiallyAvailable: envString("DM_BROKER_TOPIC_DM_EVENTS_VERSION_PARTIALLY_AVAILABLE", "dm.events.version-partially-available"),

		// DLQ topics
		TopicDMDLQIngestionFailed: envString("DM_BROKER_TOPIC_DM_DLQ_INGESTION_FAILED", "dm.dlq.ingestion-failed"),
		TopicDMDLQQueryFailed:     envString("DM_BROKER_TOPIC_DM_DLQ_QUERY_FAILED", "dm.dlq.query-failed"),
		TopicDMDLQInvalidMessage:  envString("DM_BROKER_TOPIC_DM_DLQ_INVALID_MESSAGE", "dm.dlq.invalid-message"),
	}
}

// StorageConfig holds Yandex Object Storage connection settings.
type StorageConfig struct {
	Endpoint      string        // DM_STORAGE_ENDPOINT (required)
	Bucket        string        // DM_STORAGE_BUCKET (required)
	AccessKey     string        // DM_STORAGE_ACCESS_KEY (required)
	SecretKey     string        // DM_STORAGE_SECRET_KEY (required)
	Region        string        // DM_STORAGE_REGION (default: "ru-central1")
	PresignedURLTTL time.Duration // DM_STORAGE_PRESIGNED_TTL (default: 5m)
}

func loadStorageConfig() StorageConfig {
	return StorageConfig{
		Endpoint:        envString("DM_STORAGE_ENDPOINT", ""),
		Bucket:          envString("DM_STORAGE_BUCKET", ""),
		AccessKey:       envString("DM_STORAGE_ACCESS_KEY", ""),
		SecretKey:       envString("DM_STORAGE_SECRET_KEY", ""),
		Region:          envString("DM_STORAGE_REGION", "ru-central1"),
		PresignedURLTTL: envDuration("DM_STORAGE_PRESIGNED_TTL", 5*time.Minute),
	}
}

// KVStoreConfig holds Redis connection settings for the KV-store.
type KVStoreConfig struct {
	Address  string        // DM_KVSTORE_ADDRESS (required)
	Password string        // DM_KVSTORE_PASSWORD (default: "")
	DB       int           // DM_KVSTORE_DB (default: 0)
	PoolSize int           // DM_KVSTORE_POOL_SIZE (default: 10)
	Timeout  time.Duration // DM_KVSTORE_TIMEOUT (default: 2s)
}

func loadKVStoreConfig() KVStoreConfig {
	return KVStoreConfig{
		Address:  envString("DM_KVSTORE_ADDRESS", ""),
		Password: envString("DM_KVSTORE_PASSWORD", ""),
		DB:       envInt("DM_KVSTORE_DB", 0),
		PoolSize: envInt("DM_KVSTORE_POOL_SIZE", 10),
		Timeout:  envDuration("DM_KVSTORE_TIMEOUT", 2*time.Second),
	}
}

// HTTPConfig holds HTTP API server settings.
type HTTPConfig struct {
	Port int // DM_HTTP_PORT (default: 8080)
}

func loadHTTPConfig() HTTPConfig {
	return HTTPConfig{
		Port: envInt("DM_HTTP_PORT", 8080),
	}
}

// ConsumerConfig holds message consumer settings.
type ConsumerConfig struct {
	Prefetch    int // DM_CONSUMER_PREFETCH (default: 10)
	Concurrency int // DM_CONSUMER_CONCURRENCY (default: 5)
}

func loadConsumerConfig() ConsumerConfig {
	return ConsumerConfig{
		Prefetch:    envInt("DM_CONSUMER_PREFETCH", 10),
		Concurrency: envInt("DM_CONSUMER_CONCURRENCY", 5),
	}
}

// IdempotencyConfig holds settings for the idempotency guard KV-store.
type IdempotencyConfig struct {
	TTL            time.Duration // DM_IDEMPOTENCY_TTL (default: 24h)
	ProcessingTTL  time.Duration // DM_IDEMPOTENCY_PROCESSING_TTL (default: 120s)
	StuckThreshold time.Duration // DM_IDEMPOTENCY_STUCK_THRESHOLD (default: 240s)
}

func loadIdempotencyConfig() IdempotencyConfig {
	return IdempotencyConfig{
		TTL:            envDuration("DM_IDEMPOTENCY_TTL", 24*time.Hour),
		ProcessingTTL:  envDuration("DM_IDEMPOTENCY_PROCESSING_TTL", 120*time.Second),
		StuckThreshold: envDuration("DM_IDEMPOTENCY_STUCK_THRESHOLD", 240*time.Second),
	}
}

// OutboxConfig holds transactional outbox settings.
type OutboxConfig struct {
	PollInterval time.Duration // DM_OUTBOX_POLL_INTERVAL (default: 200ms)
	BatchSize    int           // DM_OUTBOX_BATCH_SIZE (default: 50)
	LockTimeout  time.Duration // DM_OUTBOX_LOCK_TIMEOUT (default: 5s)
	CleanupHours int           // DM_OUTBOX_CLEANUP_HOURS (default: 48)
}

func loadOutboxConfig() OutboxConfig {
	return OutboxConfig{
		PollInterval: envDuration("DM_OUTBOX_POLL_INTERVAL", 200*time.Millisecond),
		BatchSize:    envInt("DM_OUTBOX_BATCH_SIZE", 50),
		LockTimeout:  envDuration("DM_OUTBOX_LOCK_TIMEOUT", 5*time.Second),
		CleanupHours: envInt("DM_OUTBOX_CLEANUP_HOURS", 48),
	}
}

// RetentionConfig holds data retention policy settings.
type RetentionConfig struct {
	ArchiveDays     int // DM_RETENTION_ARCHIVE_DAYS (default: 90)
	DeletedBlobDays int // DM_RETENTION_DELETED_BLOB_DAYS (default: 30)
	DeletedMetaDays int // DM_RETENTION_DELETED_META_DAYS (default: 365)
	AuditDays       int // DM_RETENTION_AUDIT_DAYS (default: 1095)
}

func loadRetentionConfig() RetentionConfig {
	return RetentionConfig{
		ArchiveDays:     envInt("DM_RETENTION_ARCHIVE_DAYS", 90),
		DeletedBlobDays: envInt("DM_RETENTION_DELETED_BLOB_DAYS", 30),
		DeletedMetaDays: envInt("DM_RETENTION_DELETED_META_DAYS", 365),
		AuditDays:       envInt("DM_RETENTION_AUDIT_DAYS", 1095),
	}
}

// RetryConfig holds retry policy settings for retryable errors.
type RetryConfig struct {
	MaxAttempts int           // DM_RETRY_MAX_ATTEMPTS (default: 3)
	BackoffBase time.Duration // DM_RETRY_BACKOFF_BASE (default: 1s)
}

func loadRetryConfig() RetryConfig {
	return RetryConfig{
		MaxAttempts: envInt("DM_RETRY_MAX_ATTEMPTS", 3),
		BackoffBase: envDuration("DM_RETRY_BACKOFF_BASE", 1*time.Second),
	}
}

// ObservabilityConfig holds logging, metrics, and tracing settings.
type ObservabilityConfig struct {
	LogLevel        string // DM_LOG_LEVEL (default: "info")
	MetricsPort     int    // DM_METRICS_PORT (default: 9090)
	TracingEnabled  bool   // DM_TRACING_ENABLED (default: false)
	TracingEndpoint string // DM_TRACING_ENDPOINT (default: "")
}

func loadObservabilityConfig() ObservabilityConfig {
	return ObservabilityConfig{
		LogLevel:        envString("DM_LOG_LEVEL", "info"),
		MetricsPort:     envInt("DM_METRICS_PORT", 9090),
		TracingEnabled:  envBool("DM_TRACING_ENABLED", false),
		TracingEndpoint: envString("DM_TRACING_ENDPOINT", ""),
	}
}

// TimeoutConfig holds timeout settings for various operations.
type TimeoutConfig struct {
	StoragePut      time.Duration // DM_TIMEOUT_STORAGE_PUT (default: 30s)
	StorageGet      time.Duration // DM_TIMEOUT_STORAGE_GET (default: 15s)
	EventProcessing time.Duration // DM_TIMEOUT_EVENT_PROCESSING (default: 60s)
	BrokerPublish   time.Duration // DM_TIMEOUT_BROKER_PUBLISH (default: 10s)
	StaleVersion    time.Duration // DM_STALE_VERSION_TIMEOUT (default: 30m)
	Shutdown        time.Duration // DM_SHUTDOWN_TIMEOUT (default: 30s)
}

func loadTimeoutConfig() TimeoutConfig {
	return TimeoutConfig{
		StoragePut:      envDuration("DM_TIMEOUT_STORAGE_PUT", 30*time.Second),
		StorageGet:      envDuration("DM_TIMEOUT_STORAGE_GET", 15*time.Second),
		EventProcessing: envDuration("DM_TIMEOUT_EVENT_PROCESSING", 60*time.Second),
		BrokerPublish:   envDuration("DM_TIMEOUT_BROKER_PUBLISH", 10*time.Second),
		StaleVersion:    envDuration("DM_STALE_VERSION_TIMEOUT", 30*time.Minute),
		Shutdown:        envDuration("DM_SHUTDOWN_TIMEOUT", 30*time.Second),
	}
}
